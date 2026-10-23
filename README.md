# Agent Vault · Windows

让 Agent 使用本机保存的凭据登录 SSH、数据库和网站，避免在支持的认证流程中把真实密码或私钥交给模型。

支持 SSH 私钥认证、PostgreSQL / MySQL 连接，以及网站账号登录。凭据由本机组件读取和使用，Agent 通过已授权的连接执行任务。具体保护范围与验证情况见本文末尾说明。

## 启动

解压完整的 `agent-vault-windows-x64.zip`，双击 `Start-AgentVault.cmd`。

也可以运行：

```powershell
.\agent-vault.exe serve
```

程序在当前用户下运行，通过独立 Edge/Chrome 应用窗口显示本机管理界面。需要已安装 Edge 或 Chrome。首次点击“创建保险库”，随后添加连接。界面跟随系统浅色或深色主题。

默认数据目录：`%LOCALAPPDATA%\AgentVault`。开发测试可以指定 `--data-dir PATH`，选项放在命令之前。不把凭据文件放到项目仓库或聊天中。

解锁使用当前 Windows 用户的 DPAPI 保护；没有额外的主密码或 Windows Hello 验证，不提供同用户应用之间的强隔离。

## 三种登录

### SSH

1. 在本机界面导入 Ed25519 私钥。加密私钥的口令仅在导入时输入。
2. 将详情页的**公钥**加入服务器对应账号的允许登录列表，例如 `authorized_keys`。
3. 点击“启用 SSH 客户端接入”。程序尝试占用标准 OpenSSH Agent 管道；若有其他代理，明确报冲突，不会修改或停止它。
4. 使用正常 Windows OpenSSH 命令连接服务器，保留主机密钥校验。

同一按钮可在“启用 / 禁用 SSH 客户端接入”间切换，连接详情和“设置”都可以操作。禁用会阻止标准和专用管道上的新签名、关闭本产品的本地 Agent 连接并释放标准管道；数据库和网站功能继续可用。已建立的远端 SSH 会话需在 SSH 客户端中自行退出。每次程序启动默认禁用 SSH 接入。只停用某个目标，可编辑连接并取消“允许 Agent 使用此连接”。

私钥不导出；Vault 只对 SSH 用户认证数据签名。不是随意字符串签名，也不意味着远端命令受到本产品白名单控制。

当前授权方式是“本机解锁 + 连接已启用”，满足条件即自动签名，**没有逐次签名确认弹窗或待批准队列**。`agent refused operation` 表示操作失败，不能解释成等待批准。0.1.1 已支持 OpenSSH 主机绑定公钥认证，并通过 `status` 的 `ssh_last_attempt` 返回最近一次签名结果及不含凭据的失败原因。`signed` 仅表示签名完成，不代表服务器已接受登录。

使用 `ssh-config` 返回的 Windows 原生 `client_executable`，不要依赖 Git Bash 中的 `ssh`。非交互命令设置 `BatchMode=yes`、禁用密码回退，避免失败后挂起。

```powershell
.\agent-vault.exe targets
.\agent-vault.exe ssh-config --target TARGET_ID
ssh username@host -p 22
```

### 数据库

界面保存 PostgreSQL 或 MySQL 目标与密码。远程连接默认校验 TLS；关闭 TLS 只允许明确的本机测试地址。

```powershell
.\agent-vault.exe db test --target TARGET_ID
.\agent-vault.exe db schema --target TARGET_ID
.\agent-vault.exe db query --target TARGET_ID --input query.json
```

`query.json` 只包含 SQL 与参数：

```json
{"sql":"SELECT COUNT(*) AS count FROM orders WHERE created_at >= $1","parameters":["2026-10-22T00:00:00Z"]}
```

示例使用 PostgreSQL 占位符。MySQL 使用 `?`。每次调用创建和关闭连接，最多返回 1,000 行，响应数据上限约 1 MiB，超时 30 秒。不提供跨调用事务或额外数据库管理功能。读写权限来自数据库账号；程序不以 SQL 前缀假装强制只读。

### 网站

随完整分发包附带固定版本 agent-browser；需要已安装 Chrome。网站凭据插件由本机登录过程通过单次、短时内部授权调用。直接运行插件不会返回密码。

在本机界面选择登录方式并录入凭据：

- **账号和密码**：原有标准表单，通过 agent-browser 的私有凭据插件认证。
- **仅密码表单**：不需要账号，由本地认证适配器填写可见密码输入框。多个输入框时需指定密码选择器；没有唯一提交按钮时只填写，不自动提交。
- **浏览器密码弹窗**：不需要账号，由本地认证适配器响应该网站的原生 JavaScript `prompt`。自动出现的弹窗无需配置；点击按钮后出现的弹窗，在高级选项填写“打开登录入口”的 CSS 选择器。此模式不包含 HTTP Basic 认证或操作系统弹窗。

默认要求 HTTPS。网站只提供 HTTP 时，需要在本机编辑连接并勾选“允许此连接使用 HTTP”。此选择意味着该连接的网络传输不加密；不会自动降级或关闭证书验证，本机回环测试地址除外。

密码适配器通过受管理 Chrome 的本机私有调试连接注入凭据，不把密码放进公共工具参数、命令行、普通环境变量或明文临时脚本。填写前检查保存的 origin；跨站密码弹窗会被拒绝。此内部连接不是新增的公开调试工具，也不提供同用户进程强隔离。

高级选项可配置登录成功标记。返回状态分别是 `filled`（已填写但未提交）、`submitted`（已提交，需确认结果）和 `authenticated`（出现配置的成功标记），不把填写或提交等同于认证成功。

```powershell
.\agent-vault.exe browser login --target TARGET_ID
.\agent-vault.exe browser snapshot --session SESSION_ID
.\agent-vault.exe browser click --session SESSION_ID --ref @e1
.\agent-vault.exe browser close --session SESSION_ID
```

默认打开独立可见 Chrome 会话。MFA、验证码可在本机接管；产品未提供自动解决保证。公开浏览器接口不提供任意 JavaScript、Cookie 导出或原始 CDP 地址。

## Agent 接入

在“Agent 集成”界面安装 skill：

- Codex：`~/.agents/skills/agent-vault/SKILL.md`
- Claude Code：`~/.claude/skills/agent-vault/SKILL.md`
- OpenClaw：`~/.openclaw/skills/agent-vault/SKILL.md`

产品只写入自己的目录。遇到非本产品的同名 skill 会拒绝覆盖；更新前保存备份。安装不意味着当前宿主已经加载或兼容，需在宿主中确认发现情况。

也可配置 stdio MCP，命令为完整的 `agent-vault.exe` 路径，参数为 `mcp`。若使用非默认数据目录，参数为 `--data-dir`、目录、`mcp`。MCP 不提供秘密读取工具。

“Agent 集成”页面提供可复制的 Codex TOML 与通用 JSON 配置。详细注册命令、11 个工具、执行链路与 SSH 限制见 [MCP 接入与实现](docs/mcp-integration.md)。产品生成的 Skill 使用中文，已有集成可点击“更新”后在新会话中加载。

## 构建与测试

需要 Go 1.26+。开发环境可在项目 `.tools/go/bin/go.exe` 放置 Go 工具链，不要求修改系统 Go。

```powershell
.\scripts\setup-browser.ps1
.\scripts\build.ps1 -Package
```

测试应以普通 Windows 用户权限运行。部分受限沙箱会在应用 ACL 后失去访问权，不能将这种失败误判为正常产品结果。

```powershell
go test ./... -count=1
# 可选真实浏览器测试：需先构建 dist/agent-vault.exe、安装浏览器组件
$env:AV_BROWSER_E2E='1'
go test ./internal/vault -run 'TestBrowserRealLogin|TestPasswordModesRealBrowser' -count=1 -timeout 150s -v
```

测试只生成专用随机凭据并使用本机测试服务。数据库协议 fixture 验证真实驱动的认证和结果处理，不是完整的 PostgreSQL/MySQL 引擎。详见 [实际实现与验证状态](docs/implementation-status.md)。

## 验证上下文没有密码

见 [本地模型请求与会话扫描步骤](docs/context-verification.md)。完整包 `tools/` 提供 `Start-ClaudeContextAudit.ps1`、`Test-ContextSecrets.ps1` 和扫描器自检；测试密码只在你自己的本机终端隐藏输入，不放进命令参数或报告。

登录成功不等于已完成防泄露验收；本版不会声称已经扫描用户的全部模型请求。

## 边界

- 本产品不接受“把密码发到聊天里”的回退。
- 本机保存使用 DPAPI 保护的密钥与 AES-256-GCM 加密数据。DPAPI 不能保证同一用户下只有本程序可以解密。
- 工具收到凭据后的未知行为、全部截图或转换后的泄露、管理员/恶意同用户程序不作绝对保证。
- 浏览器内部单次授权值只存在受管理的子进程环境中；它不是生产密码，但受同用户进程检查威胁，不能宣称完全防御本机恶意程序。
- 锁定阻止新的取密和签名，不能撤回已交付凭据，也不会自动撤销已建立的远端会话。
- 删除本产品保存的目标不会删除原私钥文件、服务器公钥授权或网站会话。
- 卸载可删除分发目录；数据与 skill 目录需由用户按需保留或删除。没有自动删除原始凭据副本的行为。

## 设计与许可

[详细设计](docs/windows-agent-vault-design.md) · [约束清单](docs/windows-agent-vault-constraints.md)

agent-browser 为 Apache-2.0 第三方组件。Go 模块版本记录在 `go.mod` / `go.sum` 与分发包的 `build-dependencies.txt`，第三方许可证随包保留。产品未上传到软件商店、未签名，当前分发包用于本地开发验证。
