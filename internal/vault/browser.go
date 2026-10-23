package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type BrowserManager struct {
	Store      *Store
	Exe        string
	DataDir    string
	Pipe       string
	RuntimeDir string
	Headless   bool // Test harness only; the desktop product uses a visible window.
	mu         sync.Mutex
	grants     map[string]browserGrant
	sessions   map[string]string
}
type browserGrant struct {
	Target  string
	Origin  string
	Expires time.Time
}
type BrowserParams struct {
	Session string `json:"session,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Grant   string `json:"grant,omitempty"`
	URL     string `json:"url,omitempty"`
}

func NewBrowser(s *Store, exe, dir, pipe, runtimeDir string) *BrowserManager {
	return &BrowserManager{Store: s, Exe: exe, DataDir: dir, Pipe: pipe, RuntimeDir: runtimeDir, grants: map[string]browserGrant{}, sessions: map[string]string{}}
}
func (b *BrowserManager) Binary() string {
	for _, p := range []string{filepath.Join(b.RuntimeDir, "agent-browser.exe"), filepath.Join(b.RuntimeDir, "node_modules", "agent-browser", "bin", "agent-browser-win32-x64.exe"), filepath.Join(b.RuntimeDir, "node_modules", "agent-browser", "bin", "agent-browser.exe")} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
func (b *BrowserManager) grant(e Entry) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	token := RandomID("bg-")
	origin, _ := Origin(e.Target.LoginURL)
	b.grants[token] = browserGrant{e.Target.ID, origin, time.Now().Add(45 * time.Second)}
	return token
}
func (b *BrowserManager) Resolve(p BrowserParams) (any, error) {
	b.mu.Lock()
	g, ok := b.grants[p.Grant]
	if ok {
		delete(b.grants, p.Grant)
	}
	b.mu.Unlock()
	if !ok || time.Now().After(g.Expires) {
		return nil, Fail("CREDENTIAL_USE_DENIED", "凭据插件只能由本机登录流程使用。")
	}
	if p.URL != "" {
		origin, err := Origin(p.URL)
		if err != nil || origin != g.Origin {
			return nil, Fail("CREDENTIAL_USE_DENIED", "登录网站与保存的目标不匹配。")
		}
	}
	e, err := b.Store.Use(g.Target, "web")
	if err != nil {
		return nil, err
	}
	if e.Target.LoginMode == "password_only" || e.Target.LoginMode == "password_prompt" {
		return nil, Fail("CREDENTIAL_USE_DENIED", "此登录方式使用本机认证适配器。")
	}
	if err = b.Store.Record(e.Target, "browser.credential", "allowed", ""); err != nil {
		return nil, err
	}
	credential := map[string]any{"username": e.Target.Username, "password": e.Secret.Password, "url": e.Target.LoginURL}
	for key, value := range map[string]string{"usernameSelector": e.Target.UsernameSelector, "passwordSelector": e.Target.PasswordSelector, "submitSelector": e.Target.SubmitSelector} {
		if value != "" {
			credential[key] = value
		}
	}
	return credential, nil
}

type cappedBuffer struct {
	bytes.Buffer
	Limit    int
	Overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remain := b.Limit - b.Len()
	if remain > 0 {
		if len(p) > remain {
			b.Buffer.Write(p[:remain])
		} else {
			b.Buffer.Write(p)
		}
	}
	if n > remain {
		b.Overflow = true
	}
	return n, nil
}
func (b *BrowserManager) command(ctx context.Context, session, grant string, args ...string) (map[string]any, error) {
	bin := b.Binary()
	if bin == "" {
		return nil, Fail("BROWSER_NOT_INSTALLED", "浏览器组件未安装，请运行项目的浏览器安装步骤。")
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	base := []string{"--json", "--session", session}
	base = append(base, args...)
	cmd := exec.CommandContext(ctx, bin, base...)
	// A native browser daemon may inherit stdio handles after the CLI exits.
	// Bound pipe-drain waiting so a successful command cannot hang forever.
	cmd.WaitDelay = 2 * time.Second
	registry, _ := json.Marshal([]map[string]any{{"name": "vault", "command": b.Exe, "args": []string{"browser-provider"}, "capabilities": []string{"credential.read"}}})
	// These values are scoped to the managed child, never inherited by the model's shell.
	env := []string{}
	for _, v := range os.Environ() {
		name := strings.ToUpper(strings.SplitN(v, "=", 2)[0])
		if strings.HasPrefix(name, "AGENT_BROWSER_") || strings.HasPrefix(name, "AV_INTERNAL_") {
			continue
		}
		env = append(env, v)
	}
	headed := "true"
	if b.Headless {
		headed = "false"
	}
	env = append(env, "AGENT_BROWSER_PLUGINS="+string(registry), "AV_INTERNAL_PIPE="+b.Pipe, "AV_INTERNAL_GRANT="+grant, "AGENT_BROWSER_HEADED="+headed)
	cmd.Env = env
	cmd.Dir = b.RuntimeDir
	hideProcess(cmd)
	out := &cappedBuffer{Limit: 1 << 20}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, Fail("TIMEOUT", "浏览器操作超时，请在本机查看页面。")
	}
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return nil, Fail("BROWSER_OPERATION_FAILED", "浏览器操作未完成。请检查页面和组件状态，原始错误未输出。")
	}
	if out.Overflow {
		return nil, Fail("OUTPUT_LIMIT", "页面返回超出上限，请缩小查看范围。")
	}
	var value map[string]any
	if json.Unmarshal(out.Bytes(), &value) != nil {
		return nil, Fail("BROWSER_PROTOCOL_ERROR", "浏览器返回格式不受支持。")
	}
	if value["success"] == false {
		return nil, Fail("BROWSER_OPERATION_FAILED", "浏览器未完成操作，请在可见窗口检查页面。")
	}
	return value, nil
}
func (b *BrowserManager) Login(ctx context.Context, id string) (any, error) {
	e, err := b.Store.Use(id, "web")
	if err != nil {
		return nil, err
	}
	if b.Binary() == "" {
		return nil, Fail("BROWSER_NOT_INSTALLED", "浏览器组件未安装，请先完成组件安装。")
	}
	if err = b.Store.Record(e.Target, "browser.login", "started", ""); err != nil {
		return nil, err
	}
	// Each new login owns a fresh daemon and its one-use provider grant.
	// Reusing an older daemon could retain a revoked grant in its environment.
	session := RandomID("av-")
	state := "submitted"
	if e.Target.LoginMode == "password_only" || e.Target.LoginMode == "password_prompt" {
		state, err = b.loginPassword(ctx, session, e)
	} else {
		grant := b.grant(e)
		defer func() { b.mu.Lock(); delete(b.grants, grant); b.mu.Unlock() }()
		args := []string{"auth", "login", id, "--credential-provider", "vault", "--item", id, "--url", e.Target.LoginURL}
		for _, p := range [][2]string{{"--username-selector", e.Target.UsernameSelector}, {"--password-selector", e.Target.PasswordSelector}, {"--submit-selector", e.Target.SubmitSelector}} {
			if p[1] != "" {
				args = append(args, p[0], p[1])
			}
		}
		_, err = b.command(ctx, session, grant, args...)
	}
	if err != nil {
		_, _ = b.command(context.Background(), session, "", "close")
		_ = b.Store.Record(e.Target, "browser.login", "failed", SafeError(err).Code)
		return nil, err
	}
	b.mu.Lock()
	b.sessions[session] = id
	b.mu.Unlock()
	message := "登录表单已提交，请在浏览器中确认；系统未据此断言登录成功。"
	if state == "filled" {
		message = "密码已填写，未自动提交。请在本机浏览器中完成后续操作。"
	}
	if e.Target.SuccessSelector != "" {
		if _, err = b.command(ctx, session, "", "wait", e.Target.SuccessSelector, "--timeout", "10000"); err == nil {
			state = "authenticated"
			message = "已出现配置的登录成功标记。"
		}
	}
	_ = b.Store.Record(e.Target, "browser.login", state, "")
	return map[string]any{"session": session, "status": state, "message": message}, nil
}
func (b *BrowserManager) Operate(ctx context.Context, action string, p BrowserParams) (any, error) {
	b.mu.Lock()
	id, ok := b.sessions[p.Session]
	b.mu.Unlock()
	if !ok {
		return nil, Fail("SESSION_NOT_FOUND", "此浏览器会话不属于当前 Vault 登录流程。")
	}
	e, err := b.Store.Use(id, "web")
	if err != nil {
		return nil, err
	}
	args := []string{}
	switch action {
	case "snapshot":
		args = []string{"snapshot", "-i"}
	case "click":
		if len(p.Ref) > 30 || !strings.HasPrefix(p.Ref, "@e") {
			return nil, Fail("INVALID_ELEMENT", "请使用页面快照返回的 @e 元素引用。")
		}
		args = []string{"click", p.Ref}
	case "close":
		args = []string{"close"}
	default:
		return nil, Fail("UNSUPPORTED_OPERATION", "支持 snapshot、click、close。")
	}
	value, err := b.command(ctx, p.Session, "", args...)
	if err != nil {
		return nil, err
	}
	if action == "close" {
		b.mu.Lock()
		delete(b.sessions, p.Session)
		b.mu.Unlock()
	}
	return map[string]any{"session": p.Session, "result": redactBrowserValue(value, e.Secret.Password)}, nil
}
func ProviderMain(in io.Reader, out io.Writer) error {
	var req struct {
		Protocol string `json:"protocol"`
		Type     string `json:"type"`
		Request  struct {
			URL         string `json:"url"`
			ProfileName string `json:"profileName"`
			ItemRef     string `json:"itemRef"`
		} `json:"request"`
	}
	if json.NewDecoder(io.LimitReader(in, 64<<10)).Decode(&req) != nil {
		return json.NewEncoder(out).Encode(map[string]any{"protocol": "agent-browser.plugin.v1", "success": false, "error": "invalid request"})
	}
	reply := map[string]any{"protocol": "agent-browser.plugin.v1", "success": false, "error": "credential use denied"}
	if req.Protocol == "agent-browser.plugin.v1" && req.Type == "plugin.manifest" {
		reply = map[string]any{"protocol": req.Protocol, "success": true, "manifest": map[string]any{"name": "vault", "capabilities": []string{"credential.read"}, "description": "Agent Vault local credential provider"}}
	} else if req.Protocol == "agent-browser.plugin.v1" && req.Type == "credential.resolve" && os.Getenv("AV_INTERNAL_GRANT") != "" {
		p, _ := json.Marshal(BrowserParams{Grant: os.Getenv("AV_INTERNAL_GRANT"), URL: req.Request.URL})
		res, err := Call(context.Background(), os.Getenv("AV_INTERNAL_PIPE"), Request{Op: "browser.resolve", Params: p})
		if err == nil && res.OK {
			reply = map[string]any{"protocol": req.Protocol, "success": true, "credential": res.Data}
		}
	}
	return json.NewEncoder(out).Encode(reply)
}
func ValidBrowserURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.User == nil && (u.Scheme == "https" || u.Scheme == "http")
}
