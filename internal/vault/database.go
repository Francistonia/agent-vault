package vault

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
)

type QueryInput struct {
	SQL        string `json:"sql"`
	Parameters []any  `json:"parameters,omitempty"`
}
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type QueryResult struct {
	Columns   []Column `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"`
	RowCount  int      `json:"row_count"`
	RequestID string   `json:"request_id"`
}

func databaseConnection(e Entry) (*sql.DB, error) {
	t := e.Target
	if t.Engine == "postgres" {
		// Explicit configuration avoids reading ambient PGPASSWORD/PGSERVICE files.
		cfg, err := pgx.ParseConfig("postgres://localhost/postgres?sslmode=disable")
		if err != nil {
			return nil, err
		}
		cfg.Host = t.Host
		cfg.Port = uint16(t.Port)
		cfg.User = t.Username
		cfg.Password = e.Secret.Password
		cfg.Database = t.Database
		cfg.ConnectTimeout = 8 * time.Second
		cfg.Fallbacks = nil
		cfg.RuntimeParams = map[string]string{"application_name": "agent-vault"}
		if t.TLSMode == "verify-full" {
			cfg.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: t.Host}
		} else {
			cfg.TLSConfig = nil
		}
		return sql.OpenDB(stdlib.GetConnector(*cfg)), nil
	}
	cfg := mysql.NewConfig()
	cfg.User = t.Username
	cfg.Passwd = e.Secret.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	cfg.DBName = t.Database
	cfg.Timeout = 8 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	cfg.MultiStatements = false
	cfg.ParseTime = true
	cfg.AllowNativePasswords = true
	if t.TLSMode == "verify-full" {
		cfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: t.Host}
	}
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, Fail("DB_CONFIG_ERROR", "数据库连接配置无效。")
	}
	return sql.OpenDB(conn), nil
}
func classifyDB(err error) *Failure {
	if errors.Is(err, context.DeadlineExceeded) {
		return Fail("TIMEOUT", "数据库操作超时。")
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "password") || strings.Contains(s, "authentication") || strings.Contains(s, "access denied"):
		return Fail("AUTH_FAILED", "数据库认证失败，请在本机检查账号和密码。")
	case strings.Contains(s, "certificate") || strings.Contains(s, "tls"):
		return Fail("TLS_VALIDATION_FAILED", "数据库 TLS 校验失败，请检查证书和目标地址。")
	default:
		return Fail("DB_OPERATION_FAILED", "数据库操作失败，请检查连接、SQL 和账号权限。原始驱动错误未输出。")
	}
}
func DBOperation(ctx context.Context, s *Store, id, action string, q QueryInput) (any, error) {
	e, err := s.Use(id, "db")
	if err != nil {
		return nil, err
	}
	if err = s.Record(e.Target, "db."+action, "started", ""); err != nil {
		return nil, err
	}
	if action != "test" && action != "schema" && action != "query" {
		return nil, Fail("UNSUPPORTED_OPERATION", "不支持的数据库操作。")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := databaseConnection(e)
	if err != nil {
		return nil, SafeError(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	if err = db.PingContext(ctx); err != nil {
		f := classifyDB(err)
		_ = s.Record(e.Target, "db."+action, "failed", f.Code)
		return nil, f
	}
	if action == "test" {
		_ = s.Record(e.Target, "db.test", "success", "")
		return map[string]any{"connected": true}, nil
	}
	if action == "schema" {
		q.SQL = "SELECT table_schema, table_name, column_name, data_type FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog','information_schema') ORDER BY table_schema,table_name,ordinal_position"
		if e.Target.Engine == "mysql" {
			q.SQL = "SELECT table_schema, table_name, column_name, data_type FROM information_schema.columns WHERE table_schema = DATABASE() ORDER BY table_name,ordinal_position"
		}
		q.Parameters = nil
	}
	if strings.TrimSpace(q.SQL) == "" || len(q.SQL) > 128<<10 {
		return nil, Fail("INVALID_QUERY", "请提供不超过 128 KiB 的 SQL。")
	}
	parameters := make([]any, len(q.Parameters))
	for i, v := range q.Parameters {
		switch n := v.(type) {
		case json.Number:
			if integer, err := n.Int64(); err == nil {
				parameters[i] = integer
			} else {
				parameters[i] = string(n)
			}
		case float64:
			if math.Abs(n) <= 9007199254740991 && math.Trunc(n) == n {
				parameters[i] = int64(n)
			} else {
				parameters[i] = n
			}
		default:
			parameters[i] = v
		}
	}
	rows, err := db.QueryContext(ctx, q.SQL, parameters...)
	if err != nil {
		f := classifyDB(err)
		_ = s.Record(e.Target, "db."+action, "failed", f.Code)
		return nil, f
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, Fail("DB_RESULT_ERROR", "无法读取查询字段。")
	}
	result := QueryResult{Columns: []Column{}, Rows: [][]any{}, RequestID: RandomID("req-")}
	for _, c := range types {
		result.Columns = append(result.Columns, Column{Redact(c.Name(), e.Secret.Password), c.DatabaseTypeName()})
	}
	bytes := 0
	for rows.Next() {
		if len(result.Rows) >= 1000 {
			result.Truncated = true
			break
		}
		vals := make([]any, len(types))
		ptrs := make([]any, len(types))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return nil, Fail("DB_RESULT_ERROR", "无法转换查询结果。")
		}
		for i, v := range vals {
			switch x := v.(type) {
			case []byte:
				if strings.Contains(strings.ToUpper(types[i].DatabaseTypeName()), "BLOB") || strings.EqualFold(types[i].DatabaseTypeName(), "BYTEA") {
					vals[i] = map[string]string{"base64": base64.StdEncoding.EncodeToString(x)}
				} else {
					vals[i] = Redact(string(x), e.Secret.Password)
				}
			case string:
				vals[i] = Redact(x, e.Secret.Password)
			case int64:
				if x > 9007199254740991 || x < -9007199254740991 {
					vals[i] = strconv.FormatInt(x, 10)
				}
			case uint64:
				if x > 9007199254740991 {
					vals[i] = strconv.FormatUint(x, 10)
				}
			case time.Time:
				vals[i] = x.Format(time.RFC3339Nano)
			}
		}
		encoded, _ := json.Marshal(vals)
		bytes += len(encoded)
		if bytes > 1<<20 {
			result.Truncated = true
			break
		}
		result.Rows = append(result.Rows, vals)
	}
	if err = rows.Err(); err != nil {
		return nil, classifyDB(err)
	}
	result.RowCount = len(result.Rows)
	_ = s.Record(e.Target, "db."+action, "success", "")
	return result, nil
}
