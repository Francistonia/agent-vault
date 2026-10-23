package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBrowserGrantBoundOriginAndSingleUse(t *testing.T) {
	s := testStore(t)
	target, err := s.Save(SaveInput{Target: Target{Kind: "web", Name: "site", LoginURL: "https://example.com/login", Username: "user", Enabled: true}, Password: RandomID("test-secret-")})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := s.Use(target.ID, "web")
	b := NewBrowser(s, "unused", t.TempDir(), "unused", t.TempDir())
	if _, err = b.Resolve(BrowserParams{}); err == nil {
		t.Fatal("ungated resolve accepted")
	}
	grant := b.grant(e)
	if _, err = b.Resolve(BrowserParams{Grant: grant, URL: "https://evil.example/login"}); err == nil {
		t.Fatal("cross-origin credential access")
	}
	if _, err = b.Resolve(BrowserParams{Grant: grant, URL: e.Target.LoginURL}); err == nil {
		t.Fatal("grant reused after denied attempt")
	}
	grant = b.grant(e)
	if _, err = b.Resolve(BrowserParams{Grant: grant, URL: e.Target.LoginURL}); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Resolve(BrowserParams{Grant: grant}); err == nil {
		t.Fatal("grant replay accepted")
	}
}
func TestProviderDirectInvocationDoesNotRevealSecrets(t *testing.T) {
	t.Setenv("AV_INTERNAL_GRANT", "")
	var out bytes.Buffer
	err := ProviderMain(strings.NewReader(`{"protocol":"agent-browser.plugin.v1","type":"credential.resolve","request":{"profileName":"site"}}`), &out)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if json.Unmarshal(out.Bytes(), &response) != nil || response["success"] != false || response["credential"] != nil {
		t.Fatal("direct provider call returned a credential")
	}
}
func TestPublicDispatchNoCredentialExport(t *testing.T) {
	s := testStore(t)
	h := &Host{Store: s}
	for _, op := range []string{"secrets.get", "password", "private_key.export", "cookies.export"} {
		res := h.Dispatch(context.Background(), Request{Version: 1, Op: op})
		if res.OK || res.Error == nil {
			t.Fatalf("accepted secret export: %s", op)
		}
	}
}
func TestExpiredGrant(t *testing.T) {
	s := testStore(t)
	b := NewBrowser(s, "", t.TempDir(), "", t.TempDir())
	b.grants["expired"] = browserGrant{Target: "missing", Expires: time.Now().Add(-time.Second)}
	if _, err := b.Resolve(BrowserParams{Grant: "expired"}); err == nil {
		t.Fatal("expired grant accepted")
	}
}
