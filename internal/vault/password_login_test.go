package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPasswordOnlyTargetValidation(t *testing.T) {
	s := testStore(t)
	for _, mode := range []string{"password_only", "password_prompt"} {
		target, err := s.Save(SaveInput{Target: Target{Kind: "web", Name: mode, LoginURL: "https://example.com/login", LoginMode: mode, Username: "old-account", UsernameSelector: "#old", Enabled: true}, Password: RandomID("test-")})
		if err != nil {
			t.Fatal(err)
		}
		if target.Username != "" || target.UsernameSelector != "" {
			t.Fatal("password-only target retained a username")
		}
	}
	for _, target := range []Target{
		{Kind: "web", Name: "account-required", LoginURL: "https://example.com/login"},
		{Kind: "web", Name: "invalid-mode", LoginURL: "https://example.com/login", LoginMode: "unknown"},
		{Kind: "web", Name: "http-no-consent", LoginURL: "http://example.com/login", LoginMode: "password_only"},
		{Kind: "web", Name: "file-rejected", LoginURL: "file:///local.html", LoginMode: "password_only", AllowHTTP: true},
		{Kind: "web", Name: "embedded-credential", LoginURL: "https://name:secret@example.com/login", LoginMode: "password_only"},
	} {
		if _, err := s.Save(SaveInput{Target: target, Password: RandomID("test-")}); err == nil {
			t.Fatal("invalid login target accepted")
		}
	}
	if _, err := s.Save(SaveInput{Target: Target{Kind: "web", Name: "http-opt-in", LoginURL: "http://example.com/login", LoginMode: "password_only", AllowHTTP: true}, Password: RandomID("test-")}); err != nil {
		t.Fatal("explicit HTTP choice rejected")
	}
}
func TestLocalAuthEndpointRestrictions(t *testing.T) {
	for _, address := range []string{"ws://192.0.2.1:9222/devtools/browser/example", "ws://localhost:9222/devtools/browser/example", "ws://127.0.0.1/devtools/browser/example", "ws://user@127.0.0.1:9222/", "https://127.0.0.1:9222/", "file:///local"} {
		if c, err := newAuthCDP(context.Background(), address); err == nil {
			c.conn.Close()
			t.Fatal("invalid private authentication endpoint accepted")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"webSocketDebuggerUrl":"ws://192.0.2.1:9222/devtools/browser/example"}`)
	}))
	defer server.Close()
	if c, err := newAuthCDP(context.Background(), server.URL); err == nil {
		c.conn.Close()
		t.Fatal("discovered remote endpoint accepted")
	}
}
func TestBrowserStructuredPasswordRedaction(t *testing.T) {
	password := RandomID("canary-") + `"\中文<&`
	value := map[string]any{"outer": []any{map[string]any{password: "prefix " + password + " suffix"}}}
	safe := redactBrowserValue(value, password)
	encoded, _ := json.Marshal(safe)
	quoted, _ := json.Marshal(password)
	if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), string(quoted[1:len(quoted)-1])) {
		t.Fatal("special-character password was not redacted")
	}
	if !strings.Contains(string(encoded), "REDACTED") {
		t.Fatal("redaction missing")
	}
}

func TestPasswordModesRealBrowser(t *testing.T) {
	if os.Getenv("AV_BROWSER_E2E") != "1" {
		t.Skip("opt-in: real agent-browser and Chrome, local fixtures only")
	}
	password := RandomID("browser-canary-") + `"\中文<&`
	cookie := RandomID("session-")
	var wrongOriginPosts atomic.Int32
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			wrongOriginPosts.Add(1)
		}
		io.WriteString(w, `<!doctype html><script>prompt('Untrusted fixture');</script>`)
	}))
	defer bad.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/form", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<!doctype html><form method="post" action="/auth"><input id="code" type="password" name="code"><button id="submit" type="submit">Unlock</button></form>`)
	})
	mux.HandleFunc("/prompt", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<!doctype html><script>const code=prompt('Fixture access code');fetch('/auth',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams({code})}).then(r=>{if(r.ok)location.href='/dashboard'});</script>`)
	})
	mux.HandleFunc("/trigger", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<!doctype html><button id="unlock" onclick="const code=prompt('Fixture access code');fetch('/auth',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams({code})}).then(r=>{if(r.ok)location.href='/dashboard'})">Unlock</button>`)
	})
	mux.HandleFunc("/wrong-origin", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, bad.URL, 302) })
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.FormValue("code") != password {
			http.Error(w, "wrong code", 403)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "fixture", Value: cookie, HttpOnly: true, Path: "/"})
		http.Redirect(w, r, "/dashboard", 303)
	})
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("fixture")
		if err != nil || c.Value != cookie {
			http.Error(w, "not signed in", 403)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><h1 id="dashboard">Signed in</h1><button>%s</button>`, html.EscapeString(password))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	runtimeDir, _ := filepath.Abs("../../runtime")
	exe, _ := filepath.Abs("../../dist/agent-vault.exe")
	h, err := NewHost(t.TempDir(), runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	h.Exe = exe
	h.Browser.Exe = exe
	h.Browser.Headless = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx, false) }()
	t.Cleanup(func() { cancel(); <-done })
	for i := 0; i < 100; i++ {
		if _, err := Call(ctx, h.Pipe, Request{Op: "status"}); err == nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err = h.Store.Unlock(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, mode, path, trigger string }{
		{"password form", "password_only", "/form", ""},
		{"load prompt", "password_prompt", "/prompt", ""},
		{"triggered prompt", "password_prompt", "/trigger", "#unlock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, err := h.Store.Save(SaveInput{Target: Target{Kind: "web", Name: tc.name, LoginURL: server.URL + tc.path, LoginMode: tc.mode, LoginTriggerSelector: tc.trigger, PasswordSelector: "#code", SubmitSelector: "#submit", SuccessSelector: "#dashboard", Enabled: true}, Password: password})
			if err != nil {
				t.Fatal(err)
			}
			result, err := h.Browser.Login(ctx, target.ID)
			if err != nil {
				t.Fatal(err)
			}
			value := result.(map[string]any)
			session := fmt.Sprint(value["session"])
			defer h.Browser.Operate(context.Background(), "close", BrowserParams{Session: session})
			if value["status"] != "authenticated" {
				t.Fatal("local password login was not confirmed")
			}
			snapshot, err := h.Browser.Operate(ctx, "snapshot", BrowserParams{Session: session})
			if err != nil {
				t.Fatal(err)
			}
			for _, out := range []any{result, snapshot} {
				encoded, _ := json.Marshal(out)
				quoted, _ := json.Marshal(password)
				if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), string(quoted[1:len(quoted)-1])) || strings.Contains(string(encoded), cookie) || strings.Contains(string(encoded), "devtools/browser/") {
					t.Fatal("private authentication material in public result")
				}
			}
		})
	}
	t.Run("cross origin prompt refused", func(t *testing.T) {
		target, err := h.Store.Save(SaveInput{Target: Target{Kind: "web", Name: "origin fixture", LoginURL: server.URL + "/wrong-origin", LoginMode: "password_prompt", Enabled: true}, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.Browser.Login(ctx, target.ID); err == nil || SafeError(err).Code != "CREDENTIAL_USE_DENIED" {
			t.Fatal("cross-origin dialog did not fail closed")
		}
		if wrongOriginPosts.Load() != 0 {
			t.Fatal("untrusted origin received a submission")
		}
	})
}
