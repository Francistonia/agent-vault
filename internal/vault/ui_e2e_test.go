package vault

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUIRealManagementFlow(t *testing.T) {
	if os.Getenv("AV_UI_E2E") != "1" {
		t.Skip("opt-in: AV_UI_E2E=1, AV_PLAYWRIGHT_MODULE points to an installed Playwright module")
	}
	module := os.Getenv("AV_PLAYWRIGHT_MODULE")
	if module == "" {
		t.Fatal("AV_PLAYWRIGHT_MODULE is required")
	}
	runtimeDir, _ := filepath.Abs("../../runtime")
	h, err := NewHost(t.TempDir(), runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	// Give this isolated UI fixture an owned listener, never the real standard
	// pipe. The disable button must close it and change the actual gate.
	fixtureListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer fixtureListener.Close()
	h.standardSSH = fixtureListener
	h.sshAccess.set(true)
	server := httptest.NewServer(h.uiHandler())
	defer server.Close()
	h.httpAddress = strings.TrimPrefix(server.URL, "http://")
	token := RandomID("test-ui-")
	h.boots[token] = time.Now().Add(time.Minute)
	key, _ := testKey(t)
	script, _ := filepath.Abs("../../scripts/ui-test.cjs")
	outDir, _ := filepath.Abs("../../.test-data/screenshots")
	cfg, _ := json.Marshal(map[string]any{"url": server.URL + "/activate/" + token, "key": key, "playwright": module, "chrome": `C:\Program Files\Google\Chrome\Application\chrome.exe`, "output": outDir})
	cmd := exec.Command("node", script)
	cmd.Stdin = bytes.NewReader(cfg)
	cmd.WaitDelay = 3 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("UI flow failed: %s", Redact(string(out), key))
	}
	t.Log(strings.TrimSpace(string(out)))
}
