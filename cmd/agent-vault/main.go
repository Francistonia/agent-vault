package main

import (
	"agentvault.local/agentvault/internal/vault"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
)

func output(res vault.Response) { _ = json.NewEncoder(os.Stdout).Encode(res) }
func main() {
	if err := run(os.Args[1:]); err != nil {
		output(vault.Response{Error: vault.SafeError(err)})
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) > 0 && args[0] == "browser-provider" {
		return vault.ProviderMain(os.Stdin, os.Stdout)
	}
	dir := vault.DefaultDir()
	runtimeDir := ""
	exe, _ := os.Executable()
	runtimeDir = filepath.Join(filepath.Dir(exe), "runtime")
	if st, err := os.Stat(runtimeDir); err != nil || !st.IsDir() {
		runtimeDir = filepath.Join(filepath.Dir(exe), "..", "runtime")
	}
	for len(args) >= 2 && (args[0] == "--data-dir" || args[0] == "--runtime-dir") {
		if args[0] == "--data-dir" {
			dir = args[1]
		} else {
			runtimeDir = args[1]
		}
		args = args[2:]
	}
	dir, _ = filepath.Abs(dir)
	runtimeDir, _ = filepath.Abs(runtimeDir)
	if len(args) == 0 {
		args = []string{"serve"}
	}
	if args[0] == "help" || args[0] == "--help" {
		fmt.Println("Agent Vault " + vault.Version + "\nserve [--no-browser] | open | status | targets | ssh-config --target ID\ndb test|schema|query --target ID [--input query.json]\nbrowser login --target ID | snapshot|click|close --session ID [--ref @eN]\nmcp\nGlobal options before command: --data-dir PATH --runtime-dir PATH\nNo raw credential export is provided.")
		return nil
	}
	if args[0] == "serve" {
		if !contains(args, "--no-browser") {
			if p, _, err := vault.PipeNames(dir); err == nil {
				if res, err := vault.Call(context.Background(), p, vault.Request{Op: "ui.open"}); err == nil && res.OK {
					return nil
				}
			}
		}
		h, err := vault.NewHost(dir, runtimeDir)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		fmt.Fprintln(os.Stderr, "Agent Vault 本地服务启动；凭据仅在本机管理窗口录入。")
		return h.Run(ctx, !contains(args, "--no-browser"))
	}
	pipe, _, err := vault.PipeNames(dir)
	if err != nil {
		return err
	}
	if args[0] == "mcp" {
		return runMCP(pipe)
	}
	req := vault.Request{}
	switch args[0] {
	case "status", "targets":
		req.Op = args[0]
	case "open":
		req.Op = "ui.open"
	case "ssh-config":
		req.Op = "ssh.config"
		req.Target = option(args, "--target")
	case "db":
		if len(args) < 2 {
			return vault.Fail("INVALID_COMMAND", "请提供 test、schema 或 query。")
		}
		req.Op = "db." + args[1]
		req.Target = option(args, "--target")
		if args[1] == "query" {
			p := option(args, "--input")
			if p == "" {
				return vault.Fail("INVALID_COMMAND", "请用 --input 指定仅包含 SQL 和参数的 JSON 文件。")
			}
			f, err := os.Open(p)
			if err != nil {
				return vault.Fail("QUERY_FILE_ERROR", "无法读取查询文件。")
			}
			defer f.Close()
			decoder := json.NewDecoder(io.LimitReader(f, 256<<10))
			decoder.UseNumber()
			if decoder.Decode(&req.Query) != nil {
				return vault.Fail("INVALID_QUERY", "查询 JSON 格式无效。")
			}
		}
	case "browser":
		if len(args) < 2 {
			return vault.Fail("INVALID_COMMAND", "请提供 login、snapshot、click 或 close。")
		}
		req.Op = "browser." + args[1]
		req.Target = option(args, "--target")
		req.Params, _ = json.Marshal(vault.BrowserParams{Session: option(args, "--session"), Ref: option(args, "--ref")})
	default:
		return vault.Fail("INVALID_COMMAND", "未知命令。运行 help 查看用法。")
	}
	res, err := vault.Call(context.Background(), pipe, req)
	if err != nil {
		return err
	}
	output(res)
	if !res.OK {
		os.Exit(1)
	}
	return nil
}
func contains(args []string, v string) bool {
	for _, a := range args {
		if a == v {
			return true
		}
	}
	return false
}
func option(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
