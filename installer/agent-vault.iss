#ifndef AVVersion
  #error AVVersion must be supplied by scripts/build-installer.ps1
#endif
#ifndef AVSourceDir
  #error AVSourceDir must be supplied by scripts/build-installer.ps1
#endif
#ifndef AVOutputDir
  #error AVOutputDir must be supplied by scripts/build-installer.ps1
#endif

[Setup]
AppId={{2E596CFA-58F4-4FC9-926F-4CD2219DD0A8}
AppName=Agent Vault
AppVersion={#AVVersion}
AppPublisher=Francistonia
AppPublisherURL=https://github.com/Francistonia/agent-vault
AppSupportURL=https://github.com/Francistonia/agent-vault/issues
AppUpdatesURL=https://github.com/Francistonia/agent-vault/releases
DefaultDirName={localappdata}\Programs\AgentVault
DefaultGroupName=Agent Vault
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible and not arm64
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
DisableProgramGroupPage=yes
DisableWelcomePage=no
ShowLanguageDialog=no
UsePreviousAppDir=yes
LicenseFile={#AVSourceDir}\LICENSE
OutputDir={#AVOutputDir}
OutputBaseFilename=agent-vault-v{#AVVersion}-windows-x64-setup
SetupIconFile=assets\agent-vault.ico
UninstallDisplayIcon={app}\agent-vault.ico
UninstallDisplayName=Agent Vault
Compression=lzma2
SolidCompression=yes
WizardStyle=modern dynamic
CloseApplications=yes
RestartApplications=no
SetupLogging=yes
VersionInfoVersion={#AVVersion}.0
VersionInfoProductName=Agent Vault
VersionInfoDescription=Agent Vault Windows Installer

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
; build-installer.ps1 verifies the complete allowlist and hashes before compilation.
Source: "{#AVSourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\Agent Vault"; Filename: "{app}\AgentVault.exe"; WorkingDir: "{app}"; IconFilename: "{app}\agent-vault.ico"
Name: "{autodesktop}\Agent Vault"; Filename: "{app}\AgentVault.exe"; WorkingDir: "{app}"; IconFilename: "{app}\agent-vault.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\AgentVault.exe"; Description: "{cm:LaunchProgram,Agent Vault}"; Flags: nowait postinstall skipifsilent

[Messages]
chinesesimplified.WelcomeLabel2=让 Agent 使用本机保存的凭据登录服务器、数据库和网站。%n%n安装程序将为当前 Windows 用户安装 Agent Vault，并创建开始菜单入口。保险库数据独立保存，卸载时会保留。
