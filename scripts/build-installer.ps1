param([string]$DistDir,[string]$OutputDir,[string]$CompilerPath)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
if (-not $DistDir) { $DistDir = Join-Path $taskRoot 'dist' }
if (-not $OutputDir) { $OutputDir = Join-Path $taskRoot 'output/installers' }
$taskDist = (Resolve-Path -LiteralPath $DistDir).Path
$taskOutput = [System.IO.Path]::GetFullPath($OutputDir)
$taskVersionMatch = [regex]::Match((Get-Content -LiteralPath (Join-Path $taskRoot 'internal/vault/model.go') -Raw), 'const Version = "([0-9]+\.[0-9]+\.[0-9]+)"')
if (-not $taskVersionMatch.Success) { throw 'Cannot determine application version' }
$taskVersion = $taskVersionMatch.Groups[1].Value

$taskExpected = @('agent-vault.exe','AgentVault.exe','agent-vault.ico','Start-AgentVault.cmd','Start-AgentVault.ps1','README.md','LICENSE','build-dependencies.txt','SHA256SUMS.txt','runtime/agent-browser.exe','runtime/agent-browser-LICENSE.txt')
$taskExpected += @('implementation-status.md','windows-agent-vault-design.md','windows-agent-vault-constraints.md','context-verification.md','mcp-integration.md','privacy.md','code-signing.md','windows-installation.md') | ForEach-Object { 'docs/' + $_ }
$taskExpected += @('Start-ClaudeContextAudit.ps1','Test-ContextSecrets.ps1','Test-ContextScanner.ps1') | ForEach-Object { 'tools/' + $_ }
$taskExpected += @('filippo.io_edwards25519','github.com_Microsoft_go-winio','github.com_go-sql-driver_mysql','github.com_gorilla_websocket','github.com_jackc_pgpassfile','github.com_jackc_pgservicefile','github.com_jackc_pgx_v5','github.com_jackc_puddle_v2','golang.org_x_crypto','golang.org_x_sync','golang.org_x_sys','golang.org_x_text') | ForEach-Object { 'licenses/' + $_ + '-LICENSE.txt' }
$taskFiles = @(Get-ChildItem -LiteralPath $taskDist -Recurse -File)
if (Get-ChildItem -LiteralPath $taskDist -Recurse -Force | Where-Object { $_.Attributes -band [System.IO.FileAttributes]::ReparsePoint }) { throw 'Distribution may not contain links or reparse points' }
$taskActual = @($taskFiles | ForEach-Object { $_.FullName.Substring($taskDist.Length + 1).Replace('\','/') })
if (Compare-Object -ReferenceObject $taskExpected -DifferenceObject $taskActual) { throw 'Unexpected or missing distribution files; installer creation refused' }
$taskManifest = @{}
foreach ($taskLine in Get-Content -LiteralPath (Join-Path $taskDist 'SHA256SUMS.txt')) {
    if ($taskLine -notmatch '^([a-f0-9]{64})  (.+)$') { throw 'Invalid distribution checksum manifest' }
    if ($taskManifest.ContainsKey($Matches[2])) { throw 'Duplicate checksum entry' }
    $taskManifest[$Matches[2]] = $Matches[1]
}
if (Compare-Object -ReferenceObject @($taskExpected | Where-Object { $_ -ne 'SHA256SUMS.txt' }) -DifferenceObject @($taskManifest.Keys)) { throw 'Checksum manifest does not cover the distribution' }
foreach ($taskName in $taskManifest.Keys) {
    if ((Get-FileHash -LiteralPath (Join-Path $taskDist $taskName) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskManifest[$taskName]) { throw ('Distribution checksum mismatch: ' + $taskName) }
}

if (-not $CompilerPath) {
    $taskCompilerCommand = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($taskCompilerCommand) { $CompilerPath = $taskCompilerCommand.Source }
    else {
        foreach ($taskCandidate in @((Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6/ISCC.exe'),(Join-Path $env:LOCALAPPDATA 'Programs/Inno Setup 6/ISCC.exe'))) {
            if (Test-Path -LiteralPath $taskCandidate) { $CompilerPath = $taskCandidate; break }
        }
    }
}
if (-not $CompilerPath -or -not (Test-Path -LiteralPath $CompilerPath)) { throw 'Inno Setup 6.6 or later is required; GitHub Windows runners include Inno Setup' }
New-Item -ItemType Directory -Path $taskOutput -Force | Out-Null
& $CompilerPath '/Qp' ('/DAVVersion=' + $taskVersion) ('/DAVSourceDir=' + $taskDist) ('/DAVOutputDir=' + $taskOutput) (Join-Path $taskRoot 'installer/agent-vault.iss')
if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed' }
$taskInstaller = Join-Path $taskOutput ('agent-vault-v' + $taskVersion + '-windows-x64-setup.exe')
if (-not (Test-Path -LiteralPath $taskInstaller)) { throw 'Expected installer was not created' }
Write-Output $taskInstaller
