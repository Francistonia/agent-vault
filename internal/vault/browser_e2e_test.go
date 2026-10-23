package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrowserRealLogin(t *testing.T) {
	if os.Getenv("AV_BROWSER_E2E") != "1" {
		t.Skip("opt-in: AV_BROWSER_E2E=1; requires built executable and installed agent-browser")
	}
	password := RandomID("browser-test-")
	cookie := RandomID("test-session-")
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<!doctype html><title>Fixture login</title><form method="post" action="/auth"><label>Username<input id="username" name="username" autocomplete="username"></label><label>Password<input id="password" type="password" name="password" autocomplete="current-password"></label><button id="submit" type="submit">Sign in</button></form>`)
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.FormValue("username") != "tester" || r.FormValue("password") != password {
			http.Error(w, "authentication failed", 403)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "fixture_session", Value: cookie, HttpOnly: true, Path: "/"})
		http.Redirect(w, r, "/dashboard", 303)
	})
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("fixture_session")
		if err != nil || c.Value != cookie {
			http.Error(w, "not signed in", 403)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<!doctype html><title>Dashboard</title><h1 id="dashboard">Signed in</h1><a href="/dashboard">Service status</a>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	runtimeDir, _ := filepath.Abs("../../runtime")
	exe, _ := filepath.Abs("../../dist/agent-vault.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Fatal("build dist/agent-vault.exe first")
	}
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
		time.Sleep(50 * time.Millisecond)
	}
	if err = h.Store.Unlock(); err != nil {
		t.Fatal(err)
	}
	target, err := h.Store.Save(SaveInput{Target: Target{Kind: "web", Name: "Browser fixture", LoginURL: server.URL + "/login", Username: "tester", UsernameSelector: "#username", PasswordSelector: "#password", SubmitSelector: "#submit", SuccessSelector: "#dashboard", Enabled: true}, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.Browser.Login(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), password) || strings.Contains(string(data), cookie) {
		t.Fatal("login response leaked credential")
	}
	value := result.(map[string]any)
	session := fmt.Sprint(value["session"])
	defer h.Browser.Operate(context.Background(), "close", BrowserParams{Session: session})
	if value["status"] != "authenticated" {
		t.Fatalf("login was not confirmed: %s", data)
	}
	result, err = h.Browser.Operate(ctx, "snapshot", BrowserParams{Session: session})
	if err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(result)
	if !strings.Contains(string(data), "Service status") {
		t.Fatal("authenticated session not usable")
	}
	if strings.Contains(string(data), password) || strings.Contains(string(data), cookie) {
		t.Fatal("snapshot leaked credential")
	}
}
