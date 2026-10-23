package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// OpenSSH 8.9+ prefers this format when the server advertises host-bound
// public-key authentication. A plain RFC 4252 fixture does not exercise it.
func TestSSHOpenSSHHostboundSignature(t *testing.T) {
	s := testStore(t)
	private, signer := testKey(t)
	_, host := testKey(t)
	_, err := s.Save(SaveInput{Target: Target{Kind: "ssh", Name: "hostbound", Host: "127.0.0.1", Port: 22, Username: "tester", Enabled: true}, PrivateKey: private})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	go func() { defer b.Close(); _ = ServeSSHAgent(s, b) }()
	client := agent.NewClient(a)
	request := struct {
		Session                   []byte
		Message                   byte
		Username, Service, Method string
		Signed                    bool
		Algorithm                 string
		Key, HostKey              []byte
	}{bytes.Repeat([]byte{42}, 32), 50, "tester", "ssh-connection", "publickey-hostbound-v00@openssh.com", true, signer.PublicKey().Type(), signer.PublicKey().Marshal(), host.PublicKey().Marshal()}
	data := ssh.Marshal(request)
	sig, err := client.Sign(signer.PublicKey(), data)
	if err != nil {
		t.Fatalf("valid OpenSSH host-bound authentication was rejected: %v", err)
	}
	if err := signer.PublicKey().Verify(data, sig); err != nil {
		t.Fatal("signature did not cover the complete host-bound request")
	}
	request.HostKey = nil
	if _, err = client.Sign(signer.PublicKey(), ssh.Marshal(request)); err == nil {
		t.Fatal("empty host key accepted")
	}
	request.HostKey = []byte("invalid host key")
	if _, err = client.Sign(signer.PublicKey(), ssh.Marshal(request)); err == nil {
		t.Fatal("invalid host key accepted")
	}
	request.HostKey = host.PublicKey().Marshal()
	request.Method = "publickey"
	if _, err = client.Sign(signer.PublicKey(), ssh.Marshal(request)); err == nil {
		t.Fatal("unexpected trailing host key on ordinary authentication accepted")
	}
	request.Method = "publickey-hostbound-v00@openssh.com"
	request.Username = "different-user"
	if _, err = client.Sign(signer.PublicKey(), ssh.Marshal(request)); err == nil {
		t.Fatal("wrong username accepted")
	}
}

func TestSSHFailureDiagnosticsDoNotExposeAuthenticationData(t *testing.T) {
	s := testStore(t)
	private, signer := testKey(t)
	target, err := s.Save(SaveInput{Target: Target{Kind: "ssh", Name: "diagnostic", Host: "127.0.0.1", Port: 22, Username: "tester", Enabled: true}, PrivateKey: private})
	if err != nil {
		t.Fatal(err)
	}
	a := &SSHAgent{Store: s}
	auth := sshAuthData{Session: []byte(RandomID("private-session-")), Message: 50, Username: "tester", Service: "ssh-connection", Method: "publickey", Signed: true, Algorithm: signer.PublicKey().Type(), Key: signer.PublicKey().Marshal()}
	data := ssh.Marshal(auth)
	if _, err = a.Sign(signer.PublicKey(), data); err != nil {
		t.Fatal(err)
	}
	if last := s.SSHLastAttempt(); last == nil || last.Status != "signed" {
		t.Fatal("successful signature diagnostic missing")
	}
	s.Lock()
	if _, err = a.Sign(signer.PublicKey(), data); err == nil {
		t.Fatal("locked signing accepted")
	}
	if last := s.SSHLastAttempt(); last.ErrorCode != "VAULT_LOCKED" || last.Status != "denied" {
		t.Fatal("locked signing diagnostic incorrect")
	}
	if err = s.Unlock(); err != nil {
		t.Fatal(err)
	}
	target.Enabled = false
	if _, err = s.Save(SaveInput{Target: target}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Sign(signer.PublicKey(), data); err == nil {
		t.Fatal("disabled signing accepted")
	}
	if last := s.SSHLastAttempt(); last.ErrorCode != "CREDENTIAL_USE_DENIED" {
		t.Fatal("disabled target diagnostic incorrect")
	}
	auth.Method = RandomID("untrusted-method-")
	if _, err = a.Sign(signer.PublicKey(), ssh.Marshal(auth)); err == nil {
		t.Fatal("unknown method accepted")
	}
	encoded, _ := json.Marshal(s.SSHLastAttempt())
	for _, forbidden := range []string{private, string(auth.Session), auth.Method} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("authentication data leaked into diagnostic")
		}
	}
	if last := s.SSHLastAttempt(); last.ErrorCode != "SSH_AUTH_METHOD_UNSUPPORTED" || last.Method != "" {
		t.Fatal("unknown method diagnostic incorrect")
	}
}

func TestSSHAccessDisableRevokesExistingAgentClient(t *testing.T) {
	s := testStore(t)
	private, signer := testKey(t)
	_, err := s.Save(SaveInput{Target: Target{Kind: "ssh", Name: "switch-test", Host: "127.0.0.1", Port: 22, Username: "tester", Enabled: true}, PrivateKey: private})
	if err != nil {
		t.Fatal(err)
	}
	access := &SSHAccess{}
	a, b := net.Pipe()
	defer a.Close()
	go func() { defer b.Close(); _ = agent.ServeAgent(&SSHAgent{Store: s, Access: access}, b) }()
	client := agent.NewClient(a)
	auth := sshAuthData{Session: bytes.Repeat([]byte{9}, 32), Message: 50, Username: "tester", Service: "ssh-connection", Method: "publickey", Signed: true, Algorithm: signer.PublicKey().Type(), Key: signer.PublicKey().Marshal()}
	data := ssh.Marshal(auth)
	if keys, err := client.List(); err != nil || len(keys) != 0 {
		t.Fatal("access must start disabled")
	}
	if _, err := client.Sign(signer.PublicKey(), data); err == nil {
		t.Fatal("disabled agent signed")
	}
	access.set(true)
	if _, err := client.Sign(signer.PublicKey(), data); err != nil {
		t.Fatal(err)
	}
	access.set(false)
	if _, err := client.Sign(signer.PublicKey(), data); err == nil {
		t.Fatal("existing client bypassed disabled access")
	}
	if s.SSHLastAttempt().ErrorCode != "SSH_CLIENT_ACCESS_DISABLED" {
		t.Fatal("disabled diagnostic missing")
	}
	if keys, err := client.List(); err != nil || len(keys) != 0 {
		t.Fatal("disabled agent exposed keys")
	}
	access.set(true)
	if _, err := client.Sign(signer.PublicKey(), data); err != nil {
		t.Fatal("re-enabled access failed")
	}
	if !access.acquire() {
		t.Fatal("lease unavailable")
	}
	done := make(chan struct{})
	go func() { access.set(false); close(done) }()
	select {
	case <-done:
		t.Fatal("disable returned while an operation was in progress")
	case <-time.After(30 * time.Millisecond):
	}
	access.release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disable did not finish")
	}
}

func TestSSHDisableClosesLocalAgentConnections(t *testing.T) {
	h, err := NewHost(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Store.Unlock(); err != nil {
		t.Fatal(err)
	}
	private, _ := testKey(t)
	_, err = h.Store.Save(SaveInput{Target: Target{Kind: "ssh", Name: "pipe-fixture", Host: "127.0.0.1", Port: 22, Username: "tester", Enabled: true}, PrivateKey: private})
	if err != nil {
		t.Fatal(err)
	}
	db, err := h.Store.Save(SaveInput{Target: dbTarget(), Password: RandomID("test-password-")})
	if err != nil {
		t.Fatal(err)
	}
	l, err := ListenPipe(h.SSHPipe)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	h.sshAccess.set(true)
	go h.acceptSSH(context.Background(), l)
	c, err := winio.DialPipe(h.SSHPipe, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	client := agent.NewClient(c)
	if keys, err := client.List(); err != nil || len(keys) != 1 {
		t.Fatal("fixture agent unavailable")
	}
	h.DisableSSH()
	if _, err = client.List(); err == nil {
		t.Fatal("existing pipe connection was not closed")
	}
	if h.sshAccess.Enabled() {
		t.Fatal("signing gate remained enabled")
	}
	if _, err = h.Store.Use(db.ID, "db"); err != nil {
		t.Fatal("SSH disable affected database credentials")
	}
	if res := h.Dispatch(context.Background(), Request{Version: 1, Op: "ssh.enable"}); res.OK {
		t.Fatal("public tool unexpectedly enabled SSH")
	}
}

func TestSSHLoginThroughAgentWithoutKeyExport(t *testing.T) {
	s := testStore(t)
	key, signer := testKey(t)
	_, err := s.Save(SaveInput{Target: Target{Kind: "ssh", Name: "server", Host: "127.0.0.1", Port: 22, Username: "tester", Enabled: true}, PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, p ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == "tester" && string(p.Marshal()) == string(signer.PublicKey().Marshal()) {
			return nil, nil
		}
		return nil, Fail("DENIED", "denied")
	}}
	_, hostKey := testKey(t)
	serverConfig.AddHostKey(hostKey)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		conn, ch, req, err := ssh.NewServerConn(c, serverConfig)
		if err != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(req)
		for channel := range ch {
			if channel.ChannelType() != "session" {
				channel.Reject(ssh.UnknownChannelType, "unsupported")
				continue
			}
			stream, requests, err := channel.Accept()
			if err != nil {
				continue
			}
			go func() {
				defer stream.Close()
				for r := range requests {
					if r.Type == "exec" {
						r.Reply(true, nil)
						io.WriteString(stream, "ssh-agent-login-ok")
						_, _ = stream.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						return
					}
					r.Reply(false, nil)
				}
			}()
		}
	}()
	a, b := net.Pipe()
	defer a.Close()
	go func() { defer b.Close(); _ = ServeSSHAgent(s, b) }()
	client := agent.NewClient(a)
	sshClient, err := ssh.Dial("tcp", l.Addr().String(), &ssh.ClientConfig{User: "tester", Auth: []ssh.AuthMethod{ssh.PublicKeysCallback(client.Signers)}, HostKeyCallback: ssh.FixedHostKey(hostKey.PublicKey()), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer sshClient.Close()
	session, err := sshClient.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.Output("echo test")
	if err != nil || string(result) != "ssh-agent-login-ok" {
		t.Fatalf("SSH command failed: %v", err)
	}
	if _, err = client.Sign(signer.PublicKey(), []byte("arbitrary string")); err == nil {
		t.Fatal("arbitrary signing accepted")
	}
	if err = client.RemoveAll(); err == nil {
		t.Fatal("remote key removal accepted")
	}
	s.Lock()
	keys, err := client.List()
	if err != nil || len(keys) != 0 {
		t.Fatal("keys available while locked")
	}
}
