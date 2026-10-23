package vault

import (
	"context"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsOpenSSHNativeLogin(t *testing.T) {
	if os.Getenv("AV_NATIVE_SSH_E2E") != "1" {
		t.Skip("opt-in: AV_NATIVE_SSH_E2E=1; invokes the Windows SSH client against a local fixture")
	}
	s := testStore(t)
	key, signer := testKey(t)
	_, err := s.Save(SaveInput{Target: Target{Kind: "ssh", Name: "native-test", Host: "127.0.0.1", Port: 22, Username: "tester", Enabled: true}, PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	// The bundled Windows OpenSSH may ignore IdentityAgent overrides.
	// Exercise the same standard pipe enabled by the product UI.
	h, err := NewHost(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.Store = s
	if err = h.EnableStandardSSH(context.Background()); err != nil {
		t.Skip("standard SSH pipe is already owned; test will not replace another agent")
	}
	defer h.DisableSSH()
	_, hostKey := testKey(t)
	config := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, p ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == "tester" && string(p.Marshal()) == string(signer.PublicKey().Marshal()) {
			return nil, nil
		}
		return nil, Fail("DENIED", "denied")
	}}
	config.AddHostKey(hostKey)
	server, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		for {
			c, err := server.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				conn, channels, requests, err := ssh.NewServerConn(c, config)
				if err != nil {
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						channel.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					stream, reqs, err := channel.Accept()
					if err != nil {
						return
					}
					go func() {
						defer stream.Close()
						for r := range reqs {
							if r.Type == "exec" {
								r.Reply(true, nil)
								io.WriteString(stream, "native-ssh-ok")
								_, _ = stream.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
								return
							}
							r.Reply(false, nil)
						}
					}()
				}
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(server.Addr().String())
	known := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{"[" + host + "]:" + port}, hostKey.PublicKey()) + "\n"
	if err = os.WriteFile(known, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	client := filepath.Join(os.Getenv("WINDIR"), "System32", "OpenSSH", "ssh.exe")
	runClient := func() ([]byte, error) {
		cmd := exec.Command(client, "-F", "NUL", "-p", port, "-o", "BatchMode=yes", "-o", "IdentityFile=none", "-o", "UserKnownHostsFile="+known, "-o", "StrictHostKeyChecking=yes", "-o", "PasswordAuthentication=no", "-o", "ForwardAgent=no", "-o", "ConnectTimeout=8", "tester@"+host, "echo", "test")
		cmd.WaitDelay = 2 * time.Second
		return cmd.CombinedOutput()
	}
	out, err := runClient()
	if err != nil {
		t.Fatalf("native SSH failed: %s", Redact(string(out), key))
	}
	if !strings.Contains(string(out), "native-ssh-ok") {
		t.Fatal("missing command result")
	}
	h.DisableSSH()
	if out, err := runClient(); err == nil || strings.Contains(string(out), "native-ssh-ok") {
		t.Fatal("native SSH authenticated after access was disabled")
	}
	probe, err := ListenPipe(`\\.\pipe\openssh-ssh-agent`)
	if err != nil {
		t.Fatal("standard pipe not released after disable")
	}
	probe.Close()
	if err := h.EnableStandardSSH(context.Background()); err != nil {
		t.Fatal("cannot re-enable SSH access")
	}
	if out, err := runClient(); err != nil || !strings.Contains(string(out), "native-ssh-ok") {
		t.Fatal("native SSH failed after re-enable")
	}
}
