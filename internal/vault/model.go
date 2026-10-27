package vault

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const Version = "0.1.8"

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Failure) Error() string         { return e.Code + ": " + e.Message }
func Fail(code, message string) *Failure { return &Failure{code, message} }
func SafeError(err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	return Fail("INTERNAL_ERROR", "操作未完成，请查看组件状态后重试。")
}

type Target struct {
	ID                   string `json:"id"`
	Kind                 string `json:"kind"`
	Name                 string `json:"name"`
	Host                 string `json:"host"`
	Port                 int    `json:"port,omitempty"`
	Username             string `json:"username"`
	Database             string `json:"database,omitempty"`
	Engine               string `json:"engine,omitempty"`
	TLSMode              string `json:"tls_mode,omitempty"`
	LoginURL             string `json:"login_url,omitempty"`
	LoginMode            string `json:"login_mode,omitempty"`
	LoginTriggerSelector string `json:"login_trigger_selector,omitempty"`
	AllowHTTP            bool   `json:"allow_http,omitempty"`
	UsernameSelector     string `json:"username_selector,omitempty"`
	PasswordSelector     string `json:"password_selector,omitempty"`
	SubmitSelector       string `json:"submit_selector,omitempty"`
	SuccessSelector      string `json:"success_selector,omitempty"`
	Enabled              bool   `json:"enabled"`
	Fingerprint          string `json:"fingerprint,omitempty"`
	PublicKey            string `json:"public_key,omitempty"`
	HostKey              string `json:"host_key,omitempty"`
	UpdatedAt            string `json:"updated_at"`
}
type Secret struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
}
type Entry struct {
	Target Target `json:"target"`
	Secret Secret `json:"secret"`
}
type SaveInput struct {
	Target     Target `json:"target"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}
type Record struct {
	ID         string `json:"id"`
	Time       string `json:"time"`
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name"`
	Kind       string `json:"kind"`
	Action     string `json:"action"`
	Status     string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
}
type Document struct {
	Format  int      `json:"format"`
	Entries []Entry  `json:"entries"`
	Records []Record `json:"records"`
}

func RandomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure randomness unavailable")
	}
	return prefix + hex.EncodeToString(b[:])
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func validateTarget(t *Target) error {
	t.Name = strings.TrimSpace(t.Name)
	t.Username = strings.TrimSpace(t.Username)
	t.Host = strings.TrimSpace(t.Host)
	if t.Name == "" || len(t.Name) > 100 || len(t.Username) > 256 || strings.ContainsAny(t.Username, "\x00\r\n") {
		return Fail("INVALID_TARGET", "请填写有效的连接名称和账号。")
	}
	if t.ID != "" && !safeID.MatchString(t.ID) {
		return Fail("INVALID_TARGET", "连接标识无效。")
	}
	switch t.Kind {
	case "ssh", "db":
		if t.Host == "" || strings.ContainsAny(t.Host, "/\\@?# \t\r\n\x00") || len(t.Host) > 253 || t.Username == "" {
			return Fail("INVALID_TARGET", "请填写有效的服务器地址和用户名。")
		}
		if strings.Contains(t.Host, ":") && net.ParseIP(strings.Trim(t.Host, "[]")) == nil {
			return Fail("INVALID_TARGET", "地址与端口必须分开填写。")
		}
		if t.Port < 1 || t.Port > 65535 {
			return Fail("INVALID_TARGET", "端口必须在 1–65535 之间。")
		}
		if t.Kind == "db" {
			if t.Engine != "postgres" && t.Engine != "mysql" {
				return Fail("UNSUPPORTED_ENGINE", "当前支持 PostgreSQL 和 MySQL。")
			}
			if t.Database == "" || len(t.Database) > 128 {
				return Fail("INVALID_TARGET", "请填写数据库名称。")
			}
			if t.TLSMode != "verify-full" && t.TLSMode != "disable" {
				return Fail("INVALID_TARGET", "请选择校验证书的 TLS，或明确的本地测试模式。")
			}
			if t.TLSMode == "disable" && t.Host != "localhost" && net.ParseIP(t.Host) != nil && !net.ParseIP(t.Host).IsLoopback() {
				return Fail("TLS_REQUIRED", "不加密模式仅允许本机回环地址。")
			}
			if t.TLSMode == "disable" && t.Host != "localhost" && net.ParseIP(t.Host) == nil {
				return Fail("TLS_REQUIRED", "不加密模式仅允许本机回环地址。")
			}
		}
	case "web":
		if t.LoginMode == "" {
			t.LoginMode = "account_password"
		}
		switch t.LoginMode {
		case "account_password":
			if t.Username == "" {
				return Fail("USERNAME_REQUIRED", "账号密码登录需要填写账号；无账号的网站请选择仅密码或密码弹窗。")
			}
		case "password_only", "password_prompt":
			t.Username = ""
			t.UsernameSelector = ""
		default:
			return Fail("INVALID_LOGIN_MODE", "请选择支持的网站登录方式。")
		}
		u, err := url.Parse(t.LoginURL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return Fail("INVALID_TARGET", "请填写不含账号密码的完整登录网址。")
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && (t.AllowHTTP || u.Hostname() == "localhost" || net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback())) {
			return Fail("TLS_REQUIRED", "此网站使用未加密的 HTTP。请使用 HTTPS，或在本机编辑连接并明确允许 HTTP。")
		}
		t.Host = u.Hostname()
		if len(t.LoginURL) > 2048 {
			return Fail("INVALID_TARGET", "登录网址过长。")
		}
		for _, v := range []string{t.UsernameSelector, t.PasswordSelector, t.SubmitSelector, t.SuccessSelector, t.LoginTriggerSelector} {
			if len(v) > 500 {
				return Fail("INVALID_TARGET", "页面字段配置过长。")
			}
		}
	default:
		return Fail("INVALID_TARGET", "不支持的连接类型。")
	}
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return nil
}
func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid origin")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else if u.Scheme == "http" {
			port = "80"
		} else {
			return "", fmt.Errorf("invalid scheme")
		}
	}
	return strings.ToLower(u.Scheme) + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port), nil
}
