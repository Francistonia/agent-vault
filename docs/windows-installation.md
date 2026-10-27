# Windows 安装与卸载

## 安装版

在 [GitHub Releases](https://github.com/Francistonia/agent-vault/releases) 下载
`agent-vault-v0.1.8-windows-x64-setup.exe`。

安装版适用于 Windows 10 / 11 x64，默认安装到
`%LOCALAPPDATA%\Programs\AgentVault`，仅为当前 Windows 用户安装，无需管理员权限。
安装向导提供简体中文和英文，随系统语言选择。开始菜单会添加 Agent Vault 入口，
桌面快捷方式可按需勾选。可在安装完成时选择启动应用。

本机需要已安装 Edge 或 Chrome 以显示管理窗口；网站登录需要 Chrome。
浏览器组件 agent-browser 随包提供，安装时不下载浏览器或登录生产环境。
安装版没有设置自动开机启动，也不会自动启用 SSH 接入。

保险库仍位于 `%LOCALAPPDATA%\AgentVault`，与程序目录独立。
从便携版迁移时，默认数据目录中的原有保险库可以继续使用。
先退出原有 Agent Vault，再启动安装版；关闭管理窗口不一定代表后台服务已退出。
若原来使用了自定义数据目录，需要继续通过 CLI 的 `--data-dir` 参数指定。

## Agent 接入

桌面入口为安装目录中的 `AgentVault.exe`，不会显示命令行窗口。
Skill、命令行和 MCP 继续使用 `agent-vault.exe`。安装程序不修改 PATH。
默认 MCP 程序路径为：

```text
%LOCALAPPDATA%\Programs\AgentVault\agent-vault.exe
```

从其他程序填写 MCP 路径时，需要将 `%LOCALAPPDATA%` 展开为当前用户的实际路径。
如果原来的 Skill 或 MCP 配置引用便携版路径，在“Agent 集成”中更新 Skill，
并将宿主的 MCP 配置改为安装目录中的 CLI 路径。

## 升级与卸载

升级前请退出正在运行的 Agent Vault。向导在发现安装目录中的程序仍被占用时，
可能询问是否关闭这些程序；请先结束需要保留的操作和会话。

在 Windows“设置 → 应用 → 已安装的应用”中找到 Agent Vault，选择卸载。
卸载删除本安装包写入的程序、开始菜单入口和所选桌面快捷方式，
不会删除 `%LOCALAPPDATA%\AgentVault` 中的保险库，也不会移除已经安装的 Skill
或修改宿主 MCP 配置。需要完整移除时，请在确认不再需要数据后自行处理这些目录和配置。

## 签名与 Windows 拦截

安装包、应用程序和 agent-browser 当前均未获得可信代码签名。
SignPath Foundation 申请因项目现有认可度不足而未获批准。
安装版改善安装与卸载体验，不承诺消除 SmartScreen 或智能应用控制拦截，
也不会修改 Windows 安全设置、清除互联网来源标记或添加防护例外。
如系统阻止运行，请保留拦截信息并查看
[代码签名政策](code-signing.md)，不要将“安装成功”当作所有功能均已放行的证明。

## 构建

需要 Go 1.26+、固定版本的 agent-browser，以及 Inno Setup 6.6+。
GitHub 的 Windows 构建环境已经包含 Inno Setup。执行：

```powershell
.\scripts\setup-browser.ps1
.\scripts\build.ps1 -SkipTests
go test ./... -count=1
.\scripts\build-installer.ps1
```

安装脚本在打包前检查完整文件清单及 SHA-256，拒绝包含额外文件的分发目录。
输出默认位于 `output/installers/`。保险库、凭据、浏览器配置和本地测试数据不参与打包。
