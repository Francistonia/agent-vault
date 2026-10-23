package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Protector interface {
	Protect([]byte) ([]byte, error)
	Unprotect([]byte) ([]byte, error)
}
type envelope struct {
	Format     int    `json:"format"`
	WrappedKey string `json:"wrapped_key"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}
type Store struct {
	mu             sync.RWMutex
	path           string
	protector      Protector
	doc            *Document
	key            []byte
	view           []Target
	sshLastAttempt *SSHAttempt
}

func NewStore(dir string, p Protector) (*Store, error) {
	if err := secureDir(dir); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(dir, "vault.enc.json"), protector: p}, nil
}
func (s *Store) Exists() bool { _, err := os.Stat(s.path); return err == nil }
func (s *Store) Locked() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.doc == nil }
func (s *Store) Unlock() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc != nil {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.key = make([]byte, 32)
		if _, err = rand.Read(s.key); err != nil {
			return err
		}
		s.doc = &Document{Format: 1, Entries: []Entry{}, Records: []Record{}}
		if err = s.persist(); err != nil {
			s.doc = nil
			wipe(s.key)
			s.key = nil
		}
		return err
	}
	if err != nil {
		return Fail("STORAGE_ERROR", "无法读取保险库文件。")
	}
	if len(data) > 16<<20 {
		return Fail("STORAGE_ERROR", "保险库文件超出限制。")
	}
	var e envelope
	if json.Unmarshal(data, &e) != nil || e.Format != 1 {
		return Fail("STORAGE_ERROR", "保险库格式不受支持或文件损坏。")
	}
	wrapped, err := base64.StdEncoding.DecodeString(e.WrappedKey)
	if err != nil {
		return Fail("STORAGE_ERROR", "保险库文件损坏。")
	}
	key, err := s.protector.Unprotect(wrapped)
	if err != nil || len(key) != 32 {
		return Fail("UNLOCK_FAILED", "无法解锁。请使用创建保险库的 Windows 用户和电脑。")
	}
	defer func() {
		if s.key == nil {
			wipe(key)
		}
	}()
	blk, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(blk)
	nonce, _ := base64.StdEncoding.DecodeString(e.Nonce)
	ct, _ := base64.StdEncoding.DecodeString(e.Ciphertext)
	if len(nonce) != gcm.NonceSize() {
		return Fail("STORAGE_ERROR", "保险库文件损坏。")
	}
	plain, err := gcm.Open(nil, nonce, ct, []byte("agent-vault/document/v1"))
	if err != nil {
		return Fail("STORAGE_ERROR", "保险库完整性校验失败。")
	}
	defer wipe(plain)
	var doc Document
	if json.Unmarshal(plain, &doc) != nil || doc.Format != 1 {
		return Fail("STORAGE_ERROR", "保险库内容不受支持。")
	}
	s.key = key
	s.doc = &doc
	s.refreshView()
	return nil
}
func (s *Store) Lock() { s.mu.Lock(); defer s.mu.Unlock(); s.doc = nil; wipe(s.key); s.key = nil }
func wipe(v []byte) {
	for i := range v {
		v[i] = 0
	}
}
func (s *Store) refreshView() {
	s.view = make([]Target, 0, len(s.doc.Entries))
	for _, e := range s.doc.Entries {
		s.view = append(s.view, e.Target)
	}
}
func (s *Store) Targets() []Target {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Target{}, s.view...)
}
func (s *Store) Use(id string, kind string) (Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.doc == nil {
		return Entry{}, Fail("VAULT_LOCKED", "请在本机窗口解锁保险库。")
	}
	for _, e := range s.doc.Entries {
		if e.Target.ID == id {
			if !e.Target.Enabled {
				return Entry{}, Fail("CREDENTIAL_USE_DENIED", "此连接已停用。")
			}
			if kind != "" && e.Target.Kind != kind {
				return Entry{}, Fail("INVALID_TARGET", "目标类型不匹配。")
			}
			return e, nil
		}
	}
	return Entry{}, Fail("TARGET_NOT_FOUND", "没有找到此连接，请在本机添加。")
}
func (s *Store) Save(in SaveInput) (Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return Target{}, Fail("VAULT_LOCKED", "请先解锁保险库。")
	}
	t := in.Target
	if err := validateTarget(&t); err != nil {
		return Target{}, err
	}
	index := -1
	var old Entry
	if t.ID != "" {
		for i, e := range s.doc.Entries {
			if e.Target.ID == t.ID {
				index = i
				old = e
				break
			}
		}
		if index < 0 {
			return Target{}, Fail("TARGET_NOT_FOUND", "目标不存在。")
		}
		if old.Target.Kind != t.Kind {
			return Target{}, Fail("INVALID_TARGET", "编辑时不能改变连接类型。")
		}
	}
	secret := old.Secret
	if t.Kind == "ssh" {
		if in.PrivateKey != "" {
			var raw any
			var err error
			if in.Passphrase != "" {
				raw, err = ssh.ParseRawPrivateKeyWithPassphrase([]byte(in.PrivateKey), []byte(in.Passphrase))
			} else {
				raw, err = ssh.ParseRawPrivateKey([]byte(in.PrivateKey))
			}
			if err != nil {
				return Target{}, Fail("INVALID_PRIVATE_KEY", "无法解析私钥，请检查格式和解锁口令。")
			}
			signer, err := ssh.NewSignerFromKey(raw)
			if err != nil {
				return Target{}, Fail("INVALID_PRIVATE_KEY", "不支持此私钥类型。")
			}
			if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
				return Target{}, Fail("UNSUPPORTED_KEY", "请选择 Ed25519 类型的私钥文件。")
			}
			block, err := ssh.MarshalPrivateKey(raw, "")
			if err != nil {
				return Target{}, Fail("INVALID_PRIVATE_KEY", "无法导入私钥。")
			}
			secret = Secret{PrivateKey: string(pem.EncodeToMemory(block))}
			t.Fingerprint = ssh.FingerprintSHA256(signer.PublicKey())
			t.PublicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
		} else {
			t.Fingerprint = old.Target.Fingerprint
			t.PublicKey = old.Target.PublicKey
		}
		if secret.PrivateKey == "" {
			return Target{}, Fail("CREDENTIAL_REQUIRED", "请在本机导入私钥。")
		}
	} else {
		if in.Password != "" {
			secret = Secret{Password: in.Password}
		}
		if secret.Password == "" {
			return Target{}, Fail("CREDENTIAL_REQUIRED", "请在本机填写密码。")
		}
		if len(secret.Password) > 64<<10 {
			return Target{}, Fail("INVALID_CREDENTIAL", "密码超出长度限制。")
		}
	}
	if t.ID == "" {
		t.ID = RandomID(t.Kind + "-")
	}
	entry := Entry{t, secret}
	if index >= 0 {
		s.doc.Entries[index] = entry
	} else {
		s.doc.Entries = append(s.doc.Entries, entry)
	}
	if err := s.persist(); err != nil {
		if index >= 0 {
			s.doc.Entries[index] = old
		} else {
			s.doc.Entries = s.doc.Entries[:len(s.doc.Entries)-1]
		}
		return Target{}, err
	}
	s.refreshView()
	return t, nil
}
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return Fail("VAULT_LOCKED", "请先解锁保险库。")
	}
	for i, e := range s.doc.Entries {
		if e.Target.ID == id {
			old := append([]Entry{}, s.doc.Entries...)
			s.doc.Entries = append(s.doc.Entries[:i], s.doc.Entries[i+1:]...)
			if err := s.persist(); err != nil {
				s.doc.Entries = old
				return err
			}
			s.refreshView()
			return nil
		}
	}
	return Fail("TARGET_NOT_FOUND", "目标不存在。")
}
func (s *Store) Records() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.doc == nil {
		return []Record{}
	}
	return append([]Record{}, s.doc.Records...)
}
func (s *Store) Record(t Target, action, status, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return Fail("VAULT_LOCKED", "保险库已锁定。")
	}
	r := Record{RandomID("use-"), time.Now().UTC().Format(time.RFC3339), t.ID, t.Name, t.Kind, action, status, code}
	s.doc.Records = append([]Record{r}, s.doc.Records...)
	if len(s.doc.Records) > 200 {
		s.doc.Records = s.doc.Records[:200]
	}
	return s.persist()
}
func (s *Store) persist() error {
	plain, err := json.Marshal(s.doc)
	if err != nil {
		return Fail("STORAGE_ERROR", "无法保存保险库。")
	}
	defer wipe(plain)
	blk, _ := aes.NewCipher(s.key)
	gcm, _ := cipher.NewGCM(blk)
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	ct := gcm.Seal(nil, nonce, plain, []byte("agent-vault/document/v1"))
	wrapped, err := s.protector.Protect(s.key)
	if err != nil {
		return Fail("STORAGE_ERROR", "Windows 凭据保护失败。")
	}
	e := envelope{1, base64.StdEncoding.EncodeToString(wrapped), base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(ct)}
	data, _ := json.Marshal(e)
	tmp := s.path + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return Fail("STORAGE_ERROR", "无法写入保险库。")
	}
	if err = secureFile(tmp); err != nil {
		return Fail("STORAGE_ERROR", "无法设置保险库访问权限。")
	}
	if err = replaceFile(tmp, s.path); err != nil {
		return Fail("STORAGE_ERROR", "无法原子保存保险库。")
	}
	return nil
}
func Redact(text string, secrets ...string) string {
	for _, s := range secrets {
		if s != "" {
			text = strings.ReplaceAll(text, s, "[REDACTED]")
		}
	}
	return text
}
func ConstantEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
