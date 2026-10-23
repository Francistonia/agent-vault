package vault

import (
	"crypto/rand"
	"errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"io"
	"sync"
	"time"
)

// A read lease covers each operation. Disabling waits for any signature
// already in progress, then prevents new signatures on all agent pipes.
type SSHAccess struct {
	mu      sync.RWMutex
	enabled bool
}

func (a *SSHAccess) set(enabled bool) { a.mu.Lock(); a.enabled = enabled; a.mu.Unlock() }
func (a *SSHAccess) Enabled() bool    { a.mu.RLock(); defer a.mu.RUnlock(); return a.enabled }
func (a *SSHAccess) acquire() bool {
	if a == nil {
		return true
	}
	a.mu.RLock()
	if !a.enabled {
		a.mu.RUnlock()
		return false
	}
	return true
}
func (a *SSHAccess) release() {
	if a != nil {
		a.mu.RUnlock()
	}
}

type SSHAgent struct {
	Store  *Store
	Access *SSHAccess
}

func (a *SSHAgent) List() ([]*agent.Key, error) {
	if !a.Access.acquire() {
		return []*agent.Key{}, nil
	}
	defer a.Access.release()
	if a.Store.Locked() {
		return nil, nil
	}
	keys := []*agent.Key{}
	for _, t := range a.Store.Targets() {
		if t.Kind != "ssh" || !t.Enabled {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(t.PublicKey))
		if err == nil {
			keys = append(keys, &agent.Key{Format: key.Type(), Blob: key.Marshal(), Comment: t.Name})
		}
	}
	return keys, nil
}

type sshAuthData struct {
	Session   []byte
	Message   byte
	Username  string
	Service   string
	Method    string
	Signed    bool
	Algorithm string
	Key       []byte
	Rest      []byte `ssh:"rest"`
}

const sshHostboundMethod = "publickey-hostbound-v00@openssh.com"

type SSHAttempt struct {
	Time      string `json:"time"`
	Status    string `json:"status"`
	Method    string `json:"method,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Keep diagnostics in memory and report only known method names and safe error
// codes. Never retain the signed data, session identifier, or signature.
func (s *Store) setSSHAttempt(method string, err error) {
	a := SSHAttempt{Time: time.Now().UTC().Format(time.RFC3339), Status: "signed"}
	if method == "publickey" || method == sshHostboundMethod {
		a.Method = method
	}
	if err != nil {
		a.Status = "denied"
		f := SafeError(err)
		a.ErrorCode, a.Message = f.Code, f.Message
	}
	s.mu.Lock()
	s.sshLastAttempt = &a
	s.mu.Unlock()
}

func (s *Store) SSHLastAttempt() *SSHAttempt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sshLastAttempt == nil {
		return nil
	}
	a := *s.sshLastAttempt
	return &a
}

func (a *SSHAgent) Sign(key ssh.PublicKey, data []byte) (sig *ssh.Signature, err error) {
	var auth sshAuthData
	defer func() { a.Store.setSSHAttempt(auth.Method, err) }()
	if !a.Access.acquire() {
		return nil, Fail("SSH_CLIENT_ACCESS_DISABLED", "SSH 客户端接入已禁用。请在本机窗口手动启用后重试。")
	}
	defer a.Access.release()
	if key == nil || len(data) > 128<<10 || ssh.Unmarshal(data, &auth) != nil || len(auth.Session) < 16 || auth.Message != 50 || auth.Service != "ssh-connection" || !auth.Signed || string(auth.Key) != string(key.Marshal()) || auth.Algorithm != key.Type() {
		return nil, Fail("SSH_AUTH_FORMAT_REJECTED", "仅支持有效的 SSH 用户公钥认证签名；此拒绝不表示等待人工批准。")
	}
	switch auth.Method {
	case "publickey":
		if len(auth.Rest) != 0 {
			return nil, Fail("SSH_AUTH_FORMAT_REJECTED", "普通 SSH 公钥认证包含不支持的附加数据。")
		}
	case sshHostboundMethod:
		// OpenSSH PROTOCOL 3.1 adds exactly one SSH string containing the
		// server host key. Sign the original bytes including this binding.
		var host struct{ Key []byte }
		if ssh.Unmarshal(auth.Rest, &host) != nil {
			return nil, Fail("SSH_AUTH_FORMAT_REJECTED", "SSH 主机绑定认证缺少有效的服务器公钥。")
		}
		if _, e := ssh.ParsePublicKey(host.Key); e != nil {
			return nil, Fail("SSH_AUTH_FORMAT_REJECTED", "SSH 主机绑定认证的服务器公钥无效。")
		}
	default:
		return nil, Fail("SSH_AUTH_METHOD_UNSUPPORTED", "不支持此 SSH 认证方法；此拒绝不表示等待人工批准。")
	}
	if a.Store.Locked() {
		return nil, Fail("VAULT_LOCKED", "请在本机窗口解锁保险库；当前没有逐次签名批准弹窗。")
	}
	for _, t := range a.Store.Targets() {
		if t.Kind != "ssh" || t.Username != auth.Username {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(t.PublicKey))
		if err != nil || string(pub.Marshal()) != string(key.Marshal()) {
			continue
		}
		e, err := a.Store.Use(t.ID, "ssh")
		if err != nil {
			return nil, err
		}
		if err = a.Store.Record(t, "ssh.sign", "allowed", ""); err != nil {
			return nil, err
		}
		signer, err := ssh.ParsePrivateKey([]byte(e.Secret.PrivateKey))
		if err != nil {
			return nil, Fail("SSH_KEY_UNAVAILABLE", "无法使用已保存的 SSH 密钥。")
		}
		sig, err = signer.Sign(rand.Reader, data)
		if err != nil {
			return nil, Fail("SSH_SIGN_FAILED", "SSH 认证签名未完成。")
		}
		return sig, nil
	}
	return nil, Fail("SSH_KEY_USER_MISMATCH", "没有与此公钥和登录用户名匹配的 SSH 连接；请检查目标账号。")
}
func (a *SSHAgent) SignWithFlags(k ssh.PublicKey, d []byte, f agent.SignatureFlags) (*ssh.Signature, error) {
	if f != 0 {
		err := Fail("SSH_SIGNATURE_FLAGS_UNSUPPORTED", "不支持此 SSH 签名标志。")
		a.Store.setSSHAttempt("", err)
		return nil, err
	}
	return a.Sign(k, d)
}
func (a *SSHAgent) Signers() ([]ssh.Signer, error) { return nil, errors.New("signer export disabled") }
func (a *SSHAgent) Add(agent.AddedKey) error       { return errors.New("manage keys in the local window") }
func (a *SSHAgent) Remove(ssh.PublicKey) error     { return errors.New("manage keys in the local window") }
func (a *SSHAgent) RemoveAll() error               { return errors.New("manage keys in the local window") }
func (a *SSHAgent) Lock([]byte) error              { return errors.New("manage locking in the local window") }
func (a *SSHAgent) Unlock([]byte) error            { return errors.New("manage unlocking in the local window") }
func (a *SSHAgent) Extension(string, []byte) ([]byte, error) {
	return nil, agent.ErrExtensionUnsupported
}
func ServeSSHAgent(s *Store, c io.ReadWriter) error { return agent.ServeAgent(&SSHAgent{Store: s}, c) }
