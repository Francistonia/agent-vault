package vault

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir(), WindowsProtector{})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Unlock(); err != nil {
		t.Fatal(err)
	}
	return s
}
func testKey(t *testing.T) (string, ssh.Signer) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(key)
	return string(pem.EncodeToMemory(block)), signer
}
func dbTarget() Target {
	return Target{Kind: "db", Name: "test-db", Host: "127.0.0.1", Port: 5432, Username: "reader", Database: "test", Engine: "postgres", TLSMode: "disable", Enabled: true}
}
func TestEncryptedPersistenceAndLock(t *testing.T) {
	s := testStore(t)
	password := RandomID("credential-")
	target, err := s.Save(SaveInput{Target: dbTarget(), Password: password})
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, []byte(password)) || bytes.Contains(disk, []byte(target.Username)) {
		t.Fatal("plaintext credential or account in encrypted storage")
	}
	s.Lock()
	if _, err = s.Use(target.ID, "db"); err == nil {
		t.Fatal("locked vault allowed use")
	}
	s2, err := NewStore(filepath.Dir(s.path), WindowsProtector{})
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Locked() {
		t.Fatal("startup must be locked")
	}
	if err = s2.Unlock(); err != nil {
		t.Fatal(err)
	}
	entry, err := s2.Use(target.ID, "db")
	if err != nil || entry.Secret.Password != password {
		t.Fatal("DPAPI persistence round-trip failed")
	}
	target.Enabled = false
	if _, err = s2.Save(SaveInput{Target: target}); err != nil {
		t.Fatal(err)
	}
	if _, err = s2.Use(target.ID, "db"); err == nil {
		t.Fatal("disabled target allowed use")
	}
}
func TestTamperedVaultFailsClosed(t *testing.T) {
	s := testStore(t)
	data, _ := os.ReadFile(s.path)
	data = bytes.Replace(data, []byte(`"ciphertext":"`), []byte(`"ciphertext":"AAAA`), 1)
	s.Lock()
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Unlock(); err == nil {
		t.Fatal("tampered store accepted")
	}
	if !s.Locked() {
		t.Fatal("tampered store unlocked")
	}
}
func TestTargetValidation(t *testing.T) {
	for _, url := range []string{"http://example.com/login", "https://user:password@example.com", "file:///C:/secret"} {
		s := testStore(t)
		_, err := s.Save(SaveInput{Target: Target{Kind: "web", Name: "site", LoginURL: url, Username: "user", Enabled: true}, Password: "test-password"})
		if err == nil {
			t.Fatalf("accepted insecure URL: %s", url)
		}
	}
	s := testStore(t)
	target := dbTarget()
	target.Host = "db.example.com"
	if _, err := s.Save(SaveInput{Target: target, Password: "test"}); err == nil {
		t.Fatal("disabled TLS accepted for remote host")
	}
}
func TestPrivateKeyNeverInPublicView(t *testing.T) {
	s := testStore(t)
	key, _ := testKey(t)
	target, err := s.Save(SaveInput{Target: Target{Kind: "ssh", Name: "server", Host: "127.0.0.1", Port: 22, Username: "tester", Enabled: true}, PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if target.PublicKey == "" || !strings.HasPrefix(target.Fingerprint, "SHA256:") {
		t.Fatal("missing public metadata")
	}
	if strings.Contains(target.PublicKey, "PRIVATE") {
		t.Fatal("private key in public view")
	}
	disk, _ := os.ReadFile(s.path)
	if bytes.Contains(disk, []byte("PRIVATE KEY")) {
		t.Fatal("private key on disk")
	}
}
