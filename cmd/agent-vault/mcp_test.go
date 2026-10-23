package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPPublicToolSurface(t *testing.T) {
	exe, _ := filepath.Abs("../../dist/agent-vault.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Skip("build executable first")
	}
	cmd := exec.Command(exe, "--data-dir", t.TempDir(), "mcp")
	cmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	count := 0
	for scanner.Scan() {
		var r map[string]any
		if json.Unmarshal(scanner.Bytes(), &r) != nil || r["result"] == nil {
			t.Fatal("invalid JSON RPC response")
		}
		count++
	}
	if count != 2 {
		t.Fatal("notification generated an unexpected response")
	}
	if strings.Contains(string(out), "secrets.get") || strings.Contains(string(out), "credential.resolve") {
		t.Fatal("private credential tool exposed")
	}
}
