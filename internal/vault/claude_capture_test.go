package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Check the installed Claude's file capture using a local fake model endpoint.
// No real API key, production credential, or paid model request is involved.
func TestClaudeLocalRequestCapture(t *testing.T) {
	if os.Getenv("AV_CLAUDE_CAPTURE_E2E") != "1" {
		t.Skip("opt-in: installed Claude Code, local fake endpoint only")
	}
	cli := filepath.Join(os.Getenv("USERPROFILE"), ".local", "bin", "claude.exe")
	if _, err := os.Stat(cli); err != nil {
		t.Fatal("Claude executable not found")
	}
	const probe = "capture-probe-safe-marker"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages/count_tokens") {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"input_tokens":8}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			http.Error(w, "not found", 404)
			return
		}
		var request map[string]any
		if json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&request) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		message := map[string]any{"id": "msg_fixture_capture", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 8, "output_tokens": 0}}
		if request["stream"] != true {
			message["content"] = []any{map[string]any{"type": "text", "text": "capture-probe-ok"}}
			message["stop_reason"] = "end_turn"
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(message)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		event := func(name string, value any) {
			encoded, _ := json.Marshal(value)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, encoded)
			w.(http.Flusher).Flush()
		}
		event("message_start", map[string]any{"type": "message_start", "message": message})
		event("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		event("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "capture-probe-ok"}})
		event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 8}})
		event("message_stop", map[string]string{"type": "message_stop"})
	}))
	defer server.Close()
	root := t.TempDir()
	capture := filepath.Join(root, "capture")
	if err := os.Mkdir(capture, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, "-p", "Reply with capture-probe-ok. "+probe, "--model", "claude-sonnet-4-5", "--max-turns", "1", "--no-session-persistence")
	cmd.Dir = root
	cmd.WaitDelay = 2 * time.Second
	for _, env := range os.Environ() {
		name := strings.ToUpper(strings.SplitN(env, "=", 2)[0])
		if strings.HasPrefix(name, "ANTHROPIC_") || strings.HasPrefix(name, "CLAUDE_") || strings.HasPrefix(name, "OTEL_") {
			continue
		}
		cmd.Env = append(cmd.Env, env)
	}
	cmd.Env = append(cmd.Env, "ANTHROPIC_BASE_URL="+server.URL, "ANTHROPIC_API_KEY=fixture-only-key", "CLAUDE_CONFIG_DIR="+filepath.Join(root, "config"), "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_CODE_ENABLE_TELEMETRY=1", "OTEL_METRICS_EXPORTER=none", "OTEL_TRACES_EXPORTER=none", "OTEL_LOGS_EXPORTER=console", "OTEL_LOG_RAW_API_BODIES=file:"+capture)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatalf("local capture probe did not finish: %v (raw output omitted)", err)
	}
	files, _ := filepath.Glob(filepath.Join(capture, "*.request.json"))
	if len(files) == 0 {
		t.Fatal("installed Claude did not capture request files")
	}
	found := false
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal("capture unreadable")
		}
		var value any
		if json.Unmarshal(data, &value) != nil {
			t.Fatal("capture is not JSON")
		}
		if strings.Contains(string(data), probe) {
			found = true
		}
	}
	if !found {
		t.Fatal("captured requests did not include the known harmless probe")
	}
	t.Logf("Installed Claude captured %d request file(s) against the local fixture; no real model request.", len(files))
}
