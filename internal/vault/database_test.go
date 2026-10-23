package vault

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgproto3"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Protocol fixtures exercise the real production drivers against local peers.
// They do not pretend to be full PostgreSQL/MySQL engines or validate all SQL.
func startPGFixture(t *testing.T, password string) (string, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				b := pgproto3.NewBackend(c, c)
				if _, err := b.ReceiveStartupMessage(); err != nil {
					return
				}
				b.Send(&pgproto3.AuthenticationCleartextPassword{})
				_ = b.Flush()
				msg, err := b.Receive()
				if err != nil {
					return
				}
				p, ok := msg.(*pgproto3.PasswordMessage)
				if !ok || subtle.ConstantTimeCompare([]byte(p.Password), []byte(password)) != 1 {
					b.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: "password rejected: " + p.Password})
					_ = b.Flush()
					return
				}
				b.Send(&pgproto3.AuthenticationOk{})
				b.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.0"})
				b.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
				b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
				_ = b.Flush()
				statement := ""
				rowDescription := func() {
					b.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte("count"), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1}}})
				}
				for {
					m, err := b.Receive()
					if err != nil {
						return
					}
					switch v := m.(type) {
					case *pgproto3.Parse:
						statement = v.Query
						b.Send(&pgproto3.ParseComplete{})
						_ = b.Flush()
					case *pgproto3.Describe:
						if v.ObjectType == 'S' {
							b.Send(&pgproto3.ParameterDescription{ParameterOIDs: []uint32{}})
						}
						rowDescription()
						_ = b.Flush()
					case *pgproto3.Bind:
						b.Send(&pgproto3.BindComplete{})
						_ = b.Flush()
					case *pgproto3.Execute:
						val := "120"
						if strings.Contains(statement, "fixture_secret") {
							val = password
						}
						b.Send(&pgproto3.DataRow{Values: [][]byte{[]byte(val)}})
						b.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
						_ = b.Flush()
					case *pgproto3.Sync:
						b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
						_ = b.Flush()
					case *pgproto3.Close:
						b.Send(&pgproto3.CloseComplete{})
						_ = b.Flush()
					case *pgproto3.Query:
						val := "120"
						if strings.Contains(v.String, "fixture_secret") {
							val = password
						}
						b.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte("count"), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1}}})
						b.Send(&pgproto3.DataRow{Values: [][]byte{[]byte(val)}})
						b.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
						b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
						_ = b.Flush()
					case *pgproto3.Terminate:
						return
					default:
						b.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "0A000", Message: "fixture supports simple query only"})
						b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
						_ = b.Flush()
					}
				}
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(l.Addr().String())
	n, _ := strconv.Atoi(port)
	return host, n
}
func TestPostgresDriverAuthenticationAndRedaction(t *testing.T) {
	password := RandomID("pg-test-")
	host, port := startPGFixture(t, password)
	s := testStore(t)
	target := dbTarget()
	target.Host = host
	target.Port = port
	target, err := s.Save(SaveInput{Target: target, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DBOperation(context.Background(), s, target.ID, "test", QueryInput{}); err != nil {
		t.Fatal(err)
	}
	// The fixture handles both simple queries and the driver's extended protocol.
	e, _ := s.Use(target.ID, "db")
	db, err := databaseConnection(e)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	res, err := DBOperation(context.Background(), s, target.ID, "query", QueryInput{SQL: "SELECT 120"})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(QueryResult)
	if len(r.Rows) != 1 || fmt.Sprint(r.Rows[0][0]) != "120" {
		t.Fatal("unexpected result")
	}
	res, err = DBOperation(context.Background(), s, target.ID, "query", QueryInput{SQL: "SELECT fixture_secret"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(res)
	if bytes.Contains(encoded, []byte(password)) {
		t.Fatal("database result leaked password")
	}
	_, err = s.Save(SaveInput{Target: target, Password: RandomID("wrong-")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = DBOperation(context.Background(), s, target.ID, "test", QueryInput{})
	if err == nil || strings.Contains(err.Error(), password) {
		t.Fatal("authentication error must be safe")
	}
}

func mysqlPacket(w io.Writer, seq byte, data []byte) error {
	header := []byte{byte(len(data)), byte(len(data) >> 8), byte(len(data) >> 16), seq}
	_, err := w.Write(append(header, data...))
	return err
}
func mysqlRead(r io.Reader) ([]byte, error) {
	h := make([]byte, 4)
	if _, err := io.ReadFull(r, h); err != nil {
		return nil, err
	}
	n := int(h[0]) | int(h[1])<<8 | int(h[2])<<16
	if n > 1<<20 {
		return nil, fmt.Errorf("packet too large")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}
func mysqlNative(password string, salt []byte) []byte {
	a := sha1.Sum([]byte(password))
	b := sha1.Sum(a[:])
	h := sha1.New()
	h.Write(salt)
	h.Write(b[:])
	c := h.Sum(nil)
	for i := range c {
		c[i] ^= a[i]
	}
	return c
}
func startMySQLFixture(t *testing.T, password string) (string, int) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	salt := []byte("12345678901234567890")
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				caps := uint32(1 | 4 | 8 | 512 | 8192 | 32768 | 131072 | 524288)
				var hello bytes.Buffer
				hello.WriteByte(10)
				hello.WriteString("8.0.36-fixture\x00")
				binary.Write(&hello, binary.LittleEndian, uint32(1))
				hello.Write(salt[:8])
				hello.WriteByte(0)
				binary.Write(&hello, binary.LittleEndian, uint16(caps))
				hello.WriteByte(45)
				binary.Write(&hello, binary.LittleEndian, uint16(2))
				binary.Write(&hello, binary.LittleEndian, uint16(caps>>16))
				hello.WriteByte(21)
				hello.Write(make([]byte, 10))
				hello.Write(salt[8:])
				hello.WriteByte(0)
				hello.WriteString("mysql_native_password\x00")
				_ = mysqlPacket(c, 0, hello.Bytes())
				auth, err := mysqlRead(c)
				if err != nil || len(auth) < 34 {
					return
				}
				idx := 32
				for idx < len(auth) && auth[idx] != 0 {
					idx++
				}
				idx++
				if idx >= len(auth) {
					return
				}
				size := int(auth[idx])
				idx++
				if idx+size > len(auth) {
					return
				}
				if subtle.ConstantTimeCompare(auth[idx:idx+size], mysqlNative(password, salt)) != 1 {
					_ = mysqlPacket(c, 2, append([]byte{0xff, 0x15, 0x04, '#', '2', '8', '0', '0', '0'}, []byte("Access denied "+password)...))
					return
				}
				_ = mysqlPacket(c, 2, []byte{0, 0, 0, 2, 0, 0, 0})
				for {
					cmd, err := mysqlRead(c)
					if err != nil || len(cmd) == 0 {
						return
					}
					switch cmd[0] {
					case 1:
						return
					case 14:
						_ = mysqlPacket(c, 1, []byte{0, 0, 0, 2, 0, 0, 0})
					case 3:
						val := "120"
						if strings.Contains(string(cmd[1:]), "fixture_secret") {
							val = password
						}
						_ = mysqlPacket(c, 1, []byte{1})
						column := []byte{}
						for _, v := range []string{"def", "test", "", "", "count", ""} {
							column = append(column, byte(len(v)))
							column = append(column, []byte(v)...)
						}
						column = append(column, 12, 45, 0, 255, 0, 0, 0, 253, 0, 0, 0, 0, 0)
						_ = mysqlPacket(c, 2, column)
						_ = mysqlPacket(c, 3, []byte{0xfe, 0, 0, 2, 0})
						row := append([]byte{byte(len(val))}, []byte(val)...)
						_ = mysqlPacket(c, 4, row)
						_ = mysqlPacket(c, 5, []byte{0xfe, 0, 0, 2, 0})
					default:
						_ = mysqlPacket(c, 1, []byte{0, 0, 0, 2, 0, 0, 0})
					}
				}
			}()
		}
	}()
	host, p, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(p)
	return host, port
}
func TestMySQLDriverAuthenticationAndRedaction(t *testing.T) {
	password := RandomID("my-test-")
	host, port := startMySQLFixture(t, password)
	s := testStore(t)
	target := dbTarget()
	target.Engine = "mysql"
	target.Host = host
	target.Port = port
	target, err := s.Save(SaveInput{Target: target, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DBOperation(context.Background(), s, target.ID, "test", QueryInput{}); err != nil {
		t.Fatal(err)
	}
	res, err := DBOperation(context.Background(), s, target.ID, "query", QueryInput{SQL: "SELECT fixture_secret"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(res)
	if bytes.Contains(data, []byte(password)) {
		t.Fatal("MySQL output leaked password")
	}
}
