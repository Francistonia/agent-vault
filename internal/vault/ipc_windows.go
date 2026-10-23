//go:build windows

package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Microsoft/go-winio"
	"io"
	"net"
	"strings"
	"time"
)

func PipeNames(dir string) (string, string, error) {
	sid, err := CurrentSID()
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(sid + strings.ToLower(dir)))
	id := hex.EncodeToString(sum[:8])
	base := `\\.\pipe\agent-vault-` + id
	return base, base + "-ssh", nil
}
func ListenPipe(path string) (net.Listener, error) {
	sid, err := CurrentSID()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(path, &winio.PipeConfig{SecurityDescriptor: "D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;" + sid + ")", InputBufferSize: 65536, OutputBufferSize: 65536})
}

type Request struct {
	Version int             `json:"version"`
	Op      string          `json:"op"`
	Target  string          `json:"target,omitempty"`
	Query   QueryInput      `json:"query,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type Response struct {
	OK    bool     `json:"ok"`
	Data  any      `json:"data,omitempty"`
	Error *Failure `json:"error,omitempty"`
}

func Call(ctx context.Context, pipe string, req Request) (Response, error) {
	c, err := winio.DialPipeContext(ctx, pipe)
	if err != nil {
		return Response{}, Fail("VAULT_NOT_RUNNING", "本地 Vault 未运行。请先启动 Agent Vault。")
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(90 * time.Second))
	req.Version = 1
	if err = json.NewEncoder(c).Encode(req); err != nil {
		return Response{}, Fail("IPC_ERROR", "无法发送本地请求。")
	}
	var out Response
	if err = json.NewDecoder(io.LimitReader(c, 3<<20)).Decode(&out); err != nil {
		return out, Fail("IPC_ERROR", "本地响应无效。")
	}
	return out, nil
}
