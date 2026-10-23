package main

import (
	"agentvault.local/agentvault/internal/vault"
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
)

func runMCP(pipe string) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 512<<10)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
		var result any
		switch msg.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "agent-vault", "version": vault.Version}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": mcpTools()}
		case "tools/call":
			var call struct {
				Name      string `json:"name"`
				Arguments struct {
					Target     string `json:"target"`
					SQL        string `json:"sql"`
					Parameters []any  `json:"parameters"`
					Session    string `json:"session"`
					Ref        string `json:"ref"`
				} `json:"arguments"`
			}
			if json.Unmarshal(msg.Params, &call) != nil {
				response["error"] = map[string]any{"code": -32602, "message": "Invalid parameters"}
				break
			}
			op := map[string]string{"vault_status": "status", "vault_targets": "targets", "vault_open": "ui.open", "vault_ssh_config": "ssh.config", "vault_db_test": "db.test", "vault_db_schema": "db.schema", "vault_db_query": "db.query", "vault_browser_login": "browser.login", "vault_browser_snapshot": "browser.snapshot", "vault_browser_click": "browser.click", "vault_browser_close": "browser.close"}[call.Name]
			var res vault.Response
			if op == "" {
				res.Error = vault.Fail("UNSUPPORTED_OPERATION", "工具不存在。")
			} else {
				p, _ := json.Marshal(vault.BrowserParams{Session: call.Arguments.Session, Ref: call.Arguments.Ref})
				req := vault.Request{Op: op, Target: call.Arguments.Target, Query: vault.QueryInput{SQL: call.Arguments.SQL, Parameters: call.Arguments.Parameters}, Params: p}
				var err error
				res, err = vault.Call(context.Background(), pipe, req)
				if err != nil {
					res.Error = vault.SafeError(err)
				}
			}
			data, _ := json.Marshal(res)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}, "isError": !res.OK}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "Method not found"}
		}
		if result != nil {
			response["result"] = result
		}
		if err := enc.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func mcpTools() []map[string]any {
	out := []map[string]any{}
	for _, item := range [][2]string{{"vault_status", "检查本机保险库及 SSH 接入状态。"}, {"vault_targets", "列出已保存连接的公开信息，不返回凭据。"}, {"vault_open", "打开本机管理窗口，由用户录入凭据或解锁；不返回秘密。"}, {"vault_ssh_config", "获取 SSH 公开连接信息和原生客户端路径；远程命令仍由宿主执行。"}, {"vault_db_test", "使用本机已保存凭据测试数据库连接。"}, {"vault_db_schema", "读取数据库结构信息。"}, {"vault_db_query", "通过本机连接器执行 SQL，参数仅包含目标、SQL 与查询参数。"}, {"vault_browser_login", "在可见托管浏览器中使用本机凭据登录网站；区分填写、提交与成功标记。"}, {"vault_browser_snapshot", "查看已由保险库登录的托管浏览器会话。"}, {"vault_browser_click", "点击托管浏览器快照返回的元素引用。"}, {"vault_browser_close", "关闭托管浏览器会话。"}} {
		props := map[string]any{}
		required := []string{}
		if strings.Contains(item[0], "ssh") || strings.HasPrefix(item[0], "vault_db_") || item[0] == "vault_browser_login" {
			props["target"] = map[string]string{"type": "string"}
			required = append(required, "target")
		}
		if item[0] == "vault_db_query" {
			props["sql"] = map[string]string{"type": "string"}
			props["parameters"] = map[string]any{"type": "array", "items": map[string]any{}}
			required = append(required, "sql")
		}
		if strings.HasPrefix(item[0], "vault_browser_") && item[0] != "vault_browser_login" {
			props["session"] = map[string]string{"type": "string"}
			required = append(required, "session")
		}
		if item[0] == "vault_browser_click" {
			props["ref"] = map[string]string{"type": "string"}
			required = append(required, "ref")
		}
		out = append(out, map[string]any{"name": item[0], "description": item[1], "inputSchema": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}})
	}
	return out
}
