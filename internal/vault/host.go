package vault

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/ssh/agent"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed ui/*
var uiFiles embed.FS

//go:embed integration-skill.md
var skillTemplate string

type Host struct {
	Store          *Store
	Browser        *BrowserManager
	Dir            string
	Exe            string
	Pipe           string
	SSHPipe        string
	mu             sync.Mutex
	httpAddress    string
	boots          map[string]time.Time
	sessions       map[string]time.Time
	standardSSH    net.Listener
	sshAccess      *SSHAccess
	sshConnections map[net.Conn]bool
	cancel         context.CancelFunc
}

func NewHost(dir, runtimeDir string) (*Host, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	s, err := NewStore(dir, WindowsProtector{})
	if err != nil {
		return nil, err
	}
	pipe, sp, err := PipeNames(dir)
	if err != nil {
		return nil, err
	}
	exe, _ := os.Executable()
	h := &Host{Store: s, Dir: dir, Exe: exe, Pipe: pipe, SSHPipe: sp, boots: map[string]time.Time{}, sessions: map[string]time.Time{}, sshAccess: &SSHAccess{}, sshConnections: map[net.Conn]bool{}}
	h.Browser = NewBrowser(s, exe, dir, pipe, runtimeDir)
	return h, nil
}
func (h *Host) Run(ctx context.Context, open bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	h.cancel = cancel
	pub, err := ListenPipe(h.Pipe)
	if err != nil {
		return Fail("ALREADY_RUNNING", "无法启动本地通信。可能已有一个 Vault 正在运行。")
	}
	defer pub.Close()
	sshListener, err := ListenPipe(h.SSHPipe)
	if err != nil {
		return err
	}
	defer sshListener.Close()
	web, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	h.httpAddress = web.Addr().String()
	server := &http.Server{Handler: h.uiHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go h.acceptPublic(ctx, pub)
	go h.acceptSSH(ctx, sshListener)
	go server.Serve(web)
	if open {
		if err = h.OpenUI(); err != nil {
			fmt.Fprintln(os.Stderr, "本机管理窗口未打开，可稍后运行 agent-vault open。")
		}
	}
	<-ctx.Done()
	h.DisableSSH()
	h.Store.Lock()
	shutdown, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	_ = server.Shutdown(shutdown)
	return nil
}
func (h *Host) acceptPublic(ctx context.Context, l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(90 * time.Second))
			var req Request
			dec := json.NewDecoder(io.LimitReader(c, 256<<10))
			dec.UseNumber()
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil {
				json.NewEncoder(c).Encode(Response{Error: Fail("INVALID_REQUEST", "请求格式无效或超过上限。")})
				return
			}
			res := h.Dispatch(ctx, req)
			_ = json.NewEncoder(c).Encode(res)
		}()
	}
}
func (h *Host) acceptSSH(ctx context.Context, l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		h.mu.Lock()
		if ctx.Err() != nil || !h.sshAccess.Enabled() {
			h.mu.Unlock()
			c.Close()
			continue
		}
		h.sshConnections[c] = true
		h.mu.Unlock()
		go func() {
			defer func() { c.Close(); h.mu.Lock(); delete(h.sshConnections, c); h.mu.Unlock() }()
			_ = c.SetDeadline(time.Now().Add(2 * time.Minute))
			_ = agent.ServeAgent(&SSHAgent{Store: h.Store, Access: h.sshAccess}, c)
		}()
	}
}
func (h *Host) Dispatch(ctx context.Context, r Request) Response {
	if r.Version != 1 {
		return Response{Error: Fail("UNSUPPORTED_PROTOCOL", "本地协议版本不受支持。")}
	}
	var data any
	var err error
	switch r.Op {
	case "status":
		data = h.Status()
	case "targets":
		if h.Store.Locked() {
			err = Fail("VAULT_LOCKED", "请先在本机解锁保险库。")
		} else {
			data = h.Store.Targets()
		}
	case "ui.open", "target.setup":
		err = h.OpenUI()
		data = map[string]any{"opened": err == nil}
	case "db.test", "db.query", "db.schema":
		data, err = DBOperation(ctx, h.Store, r.Target, strings.TrimPrefix(r.Op, "db."), r.Query)
	case "browser.login":
		data, err = h.Browser.Login(ctx, r.Target)
	case "browser.snapshot", "browser.click", "browser.close":
		var p BrowserParams
		if json.Unmarshal(r.Params, &p) != nil {
			err = Fail("INVALID_REQUEST", "浏览器参数无效。")
		} else {
			data, err = h.Browser.Operate(ctx, strings.TrimPrefix(r.Op, "browser."), p)
		}
	case "browser.resolve":
		var p BrowserParams
		if json.Unmarshal(r.Params, &p) != nil {
			err = Fail("INVALID_REQUEST", "凭据请求无效。")
		} else {
			data, err = h.Browser.Resolve(p)
		}
	case "ssh.config":
		var e Entry
		e, err = h.Store.Use(r.Target, "ssh")
		if err == nil {
			data = map[string]any{"host": e.Target.Host, "port": e.Target.Port, "username": e.Target.Username, "public_key": e.Target.PublicKey, "fingerprint": e.Target.Fingerprint, "agent_pipe": h.SSHPipe, "standard_agent_pipe": `\\.\pipe\openssh-ssh-agent`, "client_executable": filepath.Join(os.Getenv("WINDIR"), "System32", "OpenSSH", "ssh.exe"), "confirmation_supported": false, "note": "使用 Windows 原生 OpenSSH。解锁且目标启用时自动签名，没有逐次确认弹窗；签名拒绝请查询 status 的 ssh_last_attempt，不能解释为等待批准。"}
		}
	default:
		err = Fail("UNSUPPORTED_OPERATION", "没有此公共操作；不提供原始凭据读取接口。")
	}
	if err != nil {
		return Response{Error: SafeError(err)}
	}
	return Response{OK: true, Data: data}
}
func (h *Host) Status() map[string]any {
	h.mu.Lock()
	standard := h.standardSSH != nil
	enabled := h.sshAccess != nil && h.sshAccess.Enabled()
	h.mu.Unlock()
	return map[string]any{"version": Version, "locked": h.Store.Locked(), "initialized": h.Store.Exists(), "browser_installed": h.Browser.Binary() != "", "ssh_client_enabled": enabled, "ssh_standard_enabled": standard, "ssh_pipe": h.SSHPipe, "ssh_confirmation_supported": false, "ssh_approval_mode": "enabled-targets", "ssh_last_attempt": h.Store.SSHLastAttempt(), "platform": "Windows", "storage": "DPAPI + AES-256-GCM", "scope": "本机用户；不保证隔离同用户任意代码"}
}
func (h *Host) EnableStandardSSH(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.standardSSH != nil {
		return nil
	}
	l, err := ListenPipe(`\\.\pipe\openssh-ssh-agent`)
	if err != nil {
		return Fail("SSH_AGENT_CONFLICT", "标准 SSH Agent 管道已被其他程序占用。未修改系统服务，请继续使用原有代理或手动选择自定义管道。")
	}
	h.standardSSH = l
	h.sshAccess.set(true)
	go h.acceptSSH(ctx, l)
	return nil
}

func (h *Host) DisableSSH() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sshAccess != nil {
		h.sshAccess.set(false)
	}
	if h.standardSSH != nil {
		h.standardSSH.Close()
		h.standardSSH = nil
	}
	// These are local agent protocol connections, not remote SSH sessions.
	for c := range h.sshConnections {
		c.Close()
		delete(h.sshConnections, c)
	}
}
func (h *Host) OpenUI() error {
	h.mu.Lock()
	if h.httpAddress == "" {
		h.mu.Unlock()
		return Fail("UI_NOT_READY", "管理窗口尚未就绪。")
	}
	nonce := RandomID("ui-")
	h.boots[nonce] = time.Now().Add(time.Minute)
	u := "http://" + h.httpAddress + "/activate/" + nonce
	h.mu.Unlock()
	for _, browser := range []string{filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Microsoft", "Edge", "Application", "msedge.exe"), filepath.Join(os.Getenv("PROGRAMFILES"), "Google", "Chrome", "Application", "chrome.exe")} {
		if _, err := os.Stat(browser); err == nil {
			cmd := exec.Command(browser, "--app="+u, "--user-data-dir="+filepath.Join(h.Dir, "manager-profile"), "--no-first-run", "--disable-extensions")
			if err = cmd.Start(); err == nil {
				go cmd.Wait()
				return nil
			}
		}
	}
	return Fail("UI_NOT_AVAILABLE", "未找到可显示管理窗口的 Edge 或 Chrome。")
}
func (h *Host) uiHandler() http.Handler {
	sub, _ := fs.Sub(uiFiles, "ui")
	static := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Host != h.httpAddress {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/activate/") {
			if r.Method != "GET" {
				http.Error(w, "method", 405)
				return
			}
			token := strings.TrimPrefix(r.URL.Path, "/activate/")
			h.mu.Lock()
			expires, ok := h.boots[token]
			delete(h.boots, token)
			if ok && time.Now().Before(expires) {
				session := RandomID("us-")
				h.sessions[session] = time.Now().Add(12 * time.Hour)
				http.SetCookie(w, &http.Cookie{Name: "av_session", Value: session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
				h.mu.Unlock()
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			h.mu.Unlock()
			http.Error(w, "activation expired; reopen the local manager", 403)
			return
		}
		cookie, err := r.Cookie("av_session")
		if err != nil {
			http.Error(w, "open the manager from Agent Vault", 403)
			return
		}
		h.mu.Lock()
		until, ok := h.sessions[cookie.Value]
		h.mu.Unlock()
		if !ok || time.Now().After(until) {
			http.Error(w, "session expired", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Header.Get("X-Agent-Vault") != "local-ui" {
				http.Error(w, "missing local header", 403)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+h.httpAddress {
				http.Error(w, "origin denied", 403)
				return
			}
			if r.Method != "GET" && r.Header.Get("Origin") != "http://"+h.httpAddress {
				http.Error(w, "origin required", 403)
				return
			}
			h.uiAPI(w, r)
			return
		}
		if r.Method != "GET" {
			http.Error(w, "method", 405)
			return
		}
		static.ServeHTTP(w, r)
	})
}
func (h *Host) uiAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	reply := func(data any, err error) {
		if err != nil {
			_ = json.NewEncoder(w).Encode(Response{Error: SafeError(err)})
		} else {
			_ = json.NewEncoder(w).Encode(Response{OK: true, Data: data})
		}
	}
	decode := func(v any) error {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		d := json.NewDecoder(r.Body)
		d.UseNumber()
		d.DisallowUnknownFields()
		if d.Decode(v) != nil {
			return Fail("INVALID_REQUEST", "表单内容无效或过长。")
		}
		return nil
	}
	if r.Method == "GET" && r.URL.Path == "/api/state" {
		reply(map[string]any{"status": h.Status(), "targets": h.Store.Targets(), "history": h.Store.Records(), "integrations": h.IntegrationStatus(), "mcp_config": map[string]any{"command": h.Exe, "args": []string{"--data-dir", h.Dir, "mcp"}}}, nil)
		return
	}
	if r.Method != "POST" {
		reply(nil, Fail("UNSUPPORTED_OPERATION", "不支持的操作。"))
		return
	}
	switch r.URL.Path {
	case "/api/unlock":
		reply(nil, h.Store.Unlock())
	case "/api/lock":
		h.Store.Lock()
		reply(nil, nil)
	case "/api/targets/save":
		var in SaveInput
		if err := decode(&in); err != nil {
			reply(nil, err)
			return
		}
		t, err := h.Store.Save(in)
		reply(t, err)
	case "/api/targets/delete":
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(&p); err != nil {
			reply(nil, err)
			return
		}
		reply(nil, h.Store.Delete(p.ID))
	case "/api/operation":
		var req Request
		if err := decode(&req); err != nil {
			reply(nil, err)
			return
		}
		if req.Op == "browser.resolve" {
			reply(nil, Fail("UNSUPPORTED_OPERATION", "此接口不支持取密。"))
			return
		}
		req.Version = 1
		res := h.Dispatch(r.Context(), req)
		_ = json.NewEncoder(w).Encode(res)
	case "/api/ssh/enable":
		reply(nil, h.EnableStandardSSH(context.Background()))
	case "/api/ssh/disable":
		h.DisableSSH()
		reply(map[string]any{"message": "SSH 客户端接入已禁用，新的签名请求将被拒绝。已建立的远程会话需在客户端自行退出。"}, nil)
	case "/api/integrations/install":
		var p struct {
			Agent string `json:"agent"`
		}
		if err := decode(&p); err != nil {
			reply(nil, err)
			return
		}
		path, err := h.InstallSkill(p.Agent)
		reply(map[string]any{"path": path}, err)
	case "/api/shutdown":
		reply(map[string]bool{"stopping": true}, nil)
		go func() { time.Sleep(200 * time.Millisecond); h.cancel() }()
	default:
		reply(nil, Fail("UNSUPPORTED_OPERATION", "未找到此操作。"))
	}
}
func (h *Host) skillPath(agentName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch agentName {
	case "codex":
		return filepath.Join(home, ".agents", "skills", "agent-vault", "SKILL.md"), nil
	case "claude":
		return filepath.Join(home, ".claude", "skills", "agent-vault", "SKILL.md"), nil
	case "openclaw":
		return filepath.Join(home, ".openclaw", "skills", "agent-vault", "SKILL.md"), nil
	}
	return "", Fail("UNSUPPORTED_AGENT", "请选择支持的 Agent。")
}
func (h *Host) IntegrationStatus() []map[string]any {
	out := []map[string]any{}
	for _, a := range []string{"codex", "claude", "openclaw"} {
		p, _ := h.skillPath(a)
		data, err := os.ReadFile(p)
		out = append(out, map[string]any{"agent": a, "installed": err == nil && strings.Contains(string(data), "Generated by Agent Vault"), "path": p, "verified_runtime": false})
	}
	return out
}
func (h *Host) InstallSkill(agentName string) (string, error) {
	p, err := h.skillPath(agentName)
	if err != nil {
		return "", err
	}
	if old, err := os.ReadFile(p); err == nil {
		if !strings.Contains(string(old), "Generated by Agent Vault") {
			return "", Fail("INTEGRATION_CONFLICT", "已有同名 skill，不会覆盖。请先处理原有文件。")
		}
		sum := sha256.Sum256(old)
		backup := p + ".backup-" + hex.EncodeToString(sum[:4])
		if err = os.WriteFile(backup, old, 0600); err != nil {
			return "", Fail("INSTALL_FAILED", "无法备份现有集成。")
		}
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return "", Fail("INSTALL_FAILED", "无法创建 Agent 集成目录。")
	}
	text := strings.ReplaceAll(skillTemplate, "{{EXECUTABLE}}", h.Exe)
	text = strings.ReplaceAll(text, "{{DATA_DIR}}", h.Dir)
	if err = os.WriteFile(p, []byte(text), 0600); err != nil {
		return "", Fail("INSTALL_FAILED", "无法写入 Agent 集成。")
	}
	return p, nil
}
