package vault

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManagementAuthenticationAndCSRF(t *testing.T) {
	s := testStore(t)
	h := &Host{Store: s, httpAddress: "127.0.0.1:9911", boots: map[string]time.Time{}, sessions: map[string]time.Time{}, Browser: NewBrowser(s, "", t.TempDir(), "", t.TempDir())}
	handler := h.uiHandler()
	request := func(method, path, host, origin, cookie, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		r.Host = host
		r.Header.Set("X-Agent-Vault", "local-ui")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "av_session", Value: cookie})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/state", h.httpAddress, "", "", ""); w.Code != 403 {
		t.Fatal("unauthenticated management access")
	}
	h.sessions["test-session"] = time.Now().Add(time.Minute)
	if w := request("GET", "/api/state", "evil.example", "", "test-session", ""); w.Code != 403 {
		t.Fatal("untrusted Host accepted")
	}
	if w := request("POST", "/api/lock", h.httpAddress, "https://evil.example", "test-session", "{}"); w.Code != 403 {
		t.Fatal("cross-origin state change accepted")
	}
	if w := request("POST", "/api/lock", h.httpAddress, "", "test-session", "{}"); w.Code != 403 {
		t.Fatal("originless state change accepted")
	}
	if w := request("POST", "/api/ssh/disable", h.httpAddress, "http://"+h.httpAddress, "", "{}"); w.Code != 403 {
		t.Fatal("unauthenticated SSH disable accepted")
	}
	if w := request("POST", "/api/ssh/disable", h.httpAddress, "https://evil.example", "test-session", "{}"); w.Code != 403 {
		t.Fatal("cross-origin SSH disable accepted")
	}
	h.sshAccess = &SSHAccess{}
	h.sshAccess.set(true)
	wDisable := request("POST", "/api/ssh/disable", h.httpAddress, "http://"+h.httpAddress, "test-session", "{}")
	var disabled Response
	if json.Unmarshal(wDisable.Body.Bytes(), &disabled) != nil || !disabled.OK || h.sshAccess.Enabled() {
		t.Fatal("authenticated disable failed")
	}
	w := request("POST", "/api/operation", h.httpAddress, "http://"+h.httpAddress, "test-session", `{"version":1,"op":"browser.resolve"}`)
	var res Response
	if json.Unmarshal(w.Body.Bytes(), &res) != nil || res.OK {
		t.Fatal("management resolve endpoint accepted")
	}
	h.boots["once"] = time.Now().Add(time.Minute)
	if w := request("GET", "/activate/once", h.httpAddress, "", "", ""); w.Code != 303 {
		t.Fatal("activation failed")
	}
	if w := request("GET", "/activate/once", h.httpAddress, "", "", ""); w.Code != 403 {
		t.Fatal("activation replay accepted")
	}
}
