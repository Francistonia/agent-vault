package vault

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// This private channel is used only by local authentication adapters. Neither
// its address, session identifier, JavaScript source nor params are exported.
type authCDPMessage struct {
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Session string          `json:"sessionId"`
	Result  json.RawMessage `json:"result"`
	Params  json.RawMessage `json:"params"`
	Error   json.RawMessage `json:"error"`
}
type authCDP struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	writeMu sync.Mutex
	next    int
	pending map[int]chan authCDPMessage
	dialogs chan authCDPMessage
	closed  chan struct{}
	session string
}

func authBrowserError() error {
	return Fail("BROWSER_AUTH_FAILED", "本机浏览器认证未完成。请检查登录方式、页面状态和高级登录选项。")
}
func newAuthCDP(ctx context.Context, address string) (*authCDP, error) {
	u, err := url.Parse(address)
	if err != nil || u.User != nil || u.Port() == "" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return nil, Fail("BROWSER_AUTH_ENDPOINT", "本机浏览器认证端点无效。")
	}
	if u.Scheme == "http" {
		transport := &http.Transport{Proxy: nil}
		defer transport.CloseIdleConnections()
		client := &http.Client{Timeout: 3 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		request, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(address, "/")+"/json/version", nil)
		if err != nil {
			return nil, authBrowserError()
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, authBrowserError()
		}
		defer response.Body.Close()
		var version struct {
			WebSocket string `json:"webSocketDebuggerUrl"`
		}
		if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&version) != nil {
			return nil, authBrowserError()
		}
		ws, err := url.Parse(version.WebSocket)
		if err != nil || ws.Scheme != "ws" || ws.Host != u.Host || ws.User != nil {
			return nil, authBrowserError()
		}
		address = version.WebSocket
		u = ws
	}
	if u.Scheme != "ws" {
		return nil, Fail("BROWSER_AUTH_ENDPOINT", "本机浏览器认证端点协议不受支持。")
	}
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.DialContext(ctx, address, nil)
	if err != nil {
		return nil, Fail("BROWSER_AUTH_CONNECT", "无法连接本机浏览器的认证通道。")
	}
	c := &authCDP{conn: conn, pending: map[int]chan authCDPMessage{}, dialogs: make(chan authCDPMessage, 8), closed: make(chan struct{})}
	conn.SetReadLimit(2 << 20)
	go func() {
		defer close(c.closed)
		for {
			var m authCDPMessage
			if conn.ReadJSON(&m) != nil {
				return
			}
			if m.ID != 0 {
				c.mu.Lock()
				reply := c.pending[m.ID]
				c.mu.Unlock()
				if reply != nil {
					select {
					case reply <- m:
					default:
					}
				}
			} else if m.Method == "Page.javascriptDialogOpening" {
				select {
				case c.dialogs <- m:
				default:
				}
			}
		}
	}()
	return c, nil
}
func (c *authCDP) call(ctx context.Context, method string, params any, session string) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	reply := make(chan authCDPMessage, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	c.writeMu.Lock()
	deadline := time.Now().Add(8 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.conn.SetWriteDeadline(deadline)
	request := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		request["sessionId"] = session
	}
	err := c.conn.WriteJSON(request)
	c.writeMu.Unlock()
	if err != nil {
		return nil, authBrowserError()
	}
	select {
	case m := <-reply:
		if len(m.Error) > 0 {
			return nil, Fail("BROWSER_CDP_"+strings.ReplaceAll(strings.ToUpper(method), ".", "_"), "本机浏览器认证接口未完成操作，请重试登录。")
		}
		return m.Result, nil
	case <-ctx.Done():
		return nil, Fail("BROWSER_AUTH_TIMEOUT", "登录等待超时，请检查页面中的登录入口。")
	case <-c.closed:
		return nil, authBrowserError()
	}
}
func (c *authCDP) eval(ctx context.Context, expression string, world int) (json.RawMessage, error) {
	params := map[string]any{"expression": expression, "returnByValue": true}
	if world != 0 {
		params["contextId"] = world
	}
	result, err := c.call(ctx, "Runtime.evaluate", params, c.session)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if json.Unmarshal(result, &decoded) != nil || len(decoded.Exception) > 0 {
		return nil, authBrowserError()
	}
	return decoded.Result.Value, nil
}
func (c *authCDP) attach(ctx context.Context) error {
	data, err := c.call(ctx, "Target.getTargets", map[string]any{}, "")
	if err != nil {
		return err
	}
	var targets struct {
		Targets []struct {
			ID   string `json:"targetId"`
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"targetInfos"`
	}
	if json.Unmarshal(data, &targets) != nil {
		return authBrowserError()
	}
	id := ""
	for _, t := range targets.Targets {
		if t.Type == "page" {
			if t.URL != "about:blank" {
				if strings.HasPrefix(t.URL, "chrome://") {
					continue
				}
				return authBrowserError()
			}
			if id != "" {
				return authBrowserError()
			}
			id = t.ID
		}
	}
	if id == "" {
		return authBrowserError()
	}
	data, err = c.call(ctx, "Target.attachToTarget", map[string]any{"targetId": id, "flatten": true}, "")
	if err != nil {
		return err
	}
	var attached struct {
		Session string `json:"sessionId"`
	}
	if json.Unmarshal(data, &attached) != nil || attached.Session == "" {
		return authBrowserError()
	}
	c.session = attached.Session
	_, err = c.call(ctx, "Page.enable", map[string]any{}, c.session)
	return err
}
func (c *authCDP) world(ctx context.Context, frame, origin string) (int, error) {
	for {
		value, err := c.eval(ctx, `({origin:location.origin,ready:document.readyState!=='loading'})`, 0)
		if err != nil {
			return 0, err
		}
		var state struct {
			Origin string `json:"origin"`
			Ready  bool   `json:"ready"`
		}
		if json.Unmarshal(value, &state) != nil {
			return 0, authBrowserError()
		}
		if state.Origin != "null" && state.Origin != origin {
			return 0, Fail("CREDENTIAL_USE_DENIED", "当前页面与保存的登录网站不匹配，未填写密码。")
		}
		if state.Origin == origin && state.Ready {
			break
		}
		select {
		case <-ctx.Done():
			return 0, authBrowserError()
		case <-time.After(100 * time.Millisecond):
		}
	}
	data, err := c.call(ctx, "Page.createIsolatedWorld", map[string]any{"frameId": frame, "worldName": "agent-vault-auth"}, c.session)
	if err != nil {
		return 0, err
	}
	var result struct {
		Context int `json:"executionContextId"`
	}
	if json.Unmarshal(data, &result) != nil || result.Context == 0 {
		return 0, authBrowserError()
	}
	return result.Context, nil
}

//go:embed password-form.js
var passwordFormScript string

func (c *authCDP) fillPassword(ctx context.Context, world int, e Entry) error {
	// location.origin omits default ports; URL provides the browser's canonical
	// origin while the Go-side Origin comparisons retain explicit port binding.
	config, _ := json.Marshal(map[string]string{"origin": browserOrigin(e.Target.LoginURL), "selector": e.Target.PasswordSelector, "password": e.Secret.Password})
	for {
		value, err := c.eval(ctx, passwordFormScript+"("+string(config)+")", world)
		if err != nil {
			return err
		}
		var result struct {
			OK   bool   `json:"ok"`
			Code string `json:"code"`
		}
		if json.Unmarshal(value, &result) != nil {
			return authBrowserError()
		}
		if result.OK {
			return nil
		}
		if result.Code != "WAITING_FOR_FIELD" {
			return Fail("PASSWORD_FIELD_REJECTED", "无法唯一确定可填写的密码输入框，或页面已变化。请配置密码输入框选择器后重试。")
		}
		select {
		case <-ctx.Done():
			return Fail("PASSWORD_FIELD_NOT_FOUND", "没有找到密码输入框，请检查登录入口和密码输入框选择器。")
		case <-time.After(150 * time.Millisecond):
		}
	}
}
func (c *authCDP) click(ctx context.Context, world int, selector, origin string) error {
	cfg, _ := json.Marshal(map[string]string{"selector": selector, "origin": origin})
	script := `(function(c){try{if(location.origin!==c.origin)return false;const es=[...document.querySelectorAll(c.selector)].filter(e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e);return r.width>0&&r.height>0&&!e.disabled&&s.visibility!=='hidden'&&s.display!=='none'});if(es.length!==1)return false;es[0].click();return true}catch(_){return false}})(` + string(cfg) + `)`
	value, err := c.eval(ctx, script, world)
	if err != nil {
		return err
	}
	if string(value) != "true" {
		return Fail("LOGIN_TRIGGER_NOT_FOUND", "没有找到唯一的登录入口，请检查高级选项中的选择器。")
	}
	return nil
}
func (b *BrowserManager) loginPassword(ctx context.Context, session string, e Entry) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := b.command(ctx, session, "", "open"); err != nil {
		return "", err
	}
	metadata, err := b.command(ctx, session, "", "get", "cdp-url")
	if err != nil {
		return "", err
	}
	data, ok := metadata["data"].(map[string]any)
	if !ok {
		return "", authBrowserError()
	}
	address, ok := data["cdpUrl"].(string)
	if !ok {
		return "", authBrowserError()
	}
	c, err := newAuthCDP(ctx, address)
	if err != nil {
		return "", err
	}
	defer c.conn.Close()
	if err = c.attach(ctx); err != nil {
		return "", err
	}
	navigated, err := c.call(ctx, "Page.navigate", map[string]any{"url": e.Target.LoginURL}, c.session)
	if err != nil {
		return "", err
	}
	var navigation struct {
		Frame string `json:"frameId"`
		Error string `json:"errorText"`
	}
	if json.Unmarshal(navigated, &navigation) != nil || navigation.Frame == "" || navigation.Error != "" {
		return "", authBrowserError()
	}
	expected, _ := Origin(e.Target.LoginURL)
	jsOrigin := browserOrigin(e.Target.LoginURL)
	if e.Target.LoginMode == "password_prompt" {
		triggerResult := make(chan error, 1)
		if e.Target.LoginTriggerSelector != "" {
			world, err := c.world(ctx, navigation.Frame, jsOrigin)
			if err != nil {
				return "", err
			}
			go func() { triggerResult <- c.click(ctx, world, e.Target.LoginTriggerSelector, jsOrigin) }()
		}
		for {
			select {
			case err := <-triggerResult:
				if err != nil {
					return "", err
				}
			case event := <-c.dialogs:
				if event.Session != c.session {
					continue
				}
				var dialog struct {
					URL  string `json:"url"`
					Type string `json:"type"`
				}
				if json.Unmarshal(event.Params, &dialog) != nil {
					return "", authBrowserError()
				}
				actual, err := Origin(dialog.URL)
				if err != nil || actual != expected || dialog.Type != "prompt" {
					return "", Fail("CREDENTIAL_USE_DENIED", "密码弹窗的网站或类型不匹配，未提交密码。")
				}
				if _, err = c.call(ctx, "Page.handleJavaScriptDialog", map[string]any{"accept": true, "promptText": e.Secret.Password}, c.session); err != nil {
					return "", err
				}
				return "submitted", nil
			case <-ctx.Done():
				return "", Fail("PASSWORD_PROMPT_NOT_FOUND", "没有收到此网站的密码输入弹窗，请配置打开弹窗的登录入口。")
			}
		}
	}
	world, err := c.world(ctx, navigation.Frame, jsOrigin)
	if err != nil {
		return "", err
	}
	if e.Target.LoginTriggerSelector != "" {
		if err = c.click(ctx, world, e.Target.LoginTriggerSelector, jsOrigin); err != nil {
			return "", err
		}
	}
	if err = c.fillPassword(ctx, world, e); err != nil {
		return "", err
	}
	submit := e.Target.SubmitSelector
	if submit == "" {
		submit = "button[type=submit], input[type=submit]"
		count, err := c.eval(ctx, `[...document.querySelectorAll('button[type=submit],input[type=submit]')].filter(e=>!e.disabled&&e.getClientRects().length>0).length`, world)
		if err != nil || string(count) != "1" {
			return "filled", nil
		}
	}
	// The native browser owns ordinary clicks and navigation. No secret is in
	// this command's arguments or returned output.
	if _, err = b.command(ctx, session, "", "click", submit); err != nil {
		return "filled", nil
	}
	return "submitted", nil
}

func redactBrowserValue(value any, password string) any {
	switch v := value.(type) {
	case string:
		return Redact(v, password)
	case []any:
		for i, item := range v {
			v[i] = redactBrowserValue(item, password)
		}
		return v
	case map[string]any:
		safe := map[string]any{}
		for k, item := range v {
			safe[Redact(k, password)] = redactBrowserValue(item, password)
		}
		return safe
	default:
		return value
	}
}

func browserOrigin(raw string) string {
	u, _ := url.Parse(raw)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port != "" && !((u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80")) {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return strings.ToLower(u.Scheme) + "://" + host
}
