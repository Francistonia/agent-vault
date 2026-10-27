param([switch]$SkipTests,[switch]$Package,[string]$OutputDir)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutputDir) { $OutputDir = Join-Path $taskRoot 'dist' }
$taskDist = [System.IO.Path]::GetFullPath($OutputDir)
$taskGo = Join-Path $taskRoot '.tools/go/bin/go.exe'
if (-not (Test-Path -LiteralPath $taskGo)) { $taskGo = (Get-Command go -ErrorAction Stop).Source }
$env:GOMODCACHE = Join-Path $taskRoot '.tools/gomodcache'
$env:GOCACHE = Join-Path $taskRoot '.tools/gocache'
Push-Location $taskRoot
try {
    if (-not $SkipTests) {
        & $taskGo test ./... -count=1
        if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
    }
    New-Item -ItemType Directory -Path (Join-Path $taskDist 'runtime') -Force | Out-Null
    & $taskGo build -trimpath -o (Join-Path $taskDist 'agent-vault.exe') ./cmd/agent-vault
    if ($LASTEXITCODE -ne 0) { throw 'Build failed. Go 1.26 or later is required.' }
    & $taskGo build -trimpath -ldflags '-H=windowsgui' -o (Join-Path $taskDist 'AgentVault.exe') ./cmd/agent-vault-desktop
    if ($LASTEXITCODE -ne 0) { throw 'Desktop launcher build failed' }
    Copy-Item -LiteralPath (Join-Path $taskRoot 'installer/assets/agent-vault.ico') -Destination (Join-Path $taskDist 'agent-vault.ico') -Force
    $taskBrowser = Join-Path $taskRoot 'runtime/node_modules/agent-browser/bin/agent-browser-win32-x64.exe'
    if (Test-Path -LiteralPath $taskBrowser) {
        $taskBrowserDestination = Join-Path $taskDist 'runtime/agent-browser.exe'
        $taskCopyBrowser = $true
        if (Test-Path -LiteralPath $taskBrowserDestination) {
            $taskCopyBrowser = (Get-FileHash -LiteralPath $taskBrowser -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $taskBrowserDestination -Algorithm SHA256).Hash
        }
        if ($taskCopyBrowser) { Copy-Item -LiteralPath $taskBrowser -Destination $taskBrowserDestination -Force }
        Copy-Item -LiteralPath (Join-Path $taskRoot 'runtime/node_modules/agent-browser/LICENSE') -Destination (Join-Path $taskDist 'runtime/agent-browser-LICENSE.txt') -Force
    }
    Copy-Item -LiteralPath (Join-Path $taskRoot 'scripts/Start-AgentVault.cmd') -Destination (Join-Path $taskDist 'Start-AgentVault.cmd') -Force
    Copy-Item -LiteralPath (Join-Path $taskRoot 'scripts/Start-AgentVault.ps1') -Destination (Join-Path $taskDist 'Start-AgentVault.ps1') -Force
    Copy-Item -LiteralPath (Join-Path $taskRoot 'README.md') -Destination (Join-Path $taskDist 'README.md') -Force
    Copy-Item -LiteralPath (Join-Path $taskRoot 'LICENSE') -Destination (Join-Path $taskDist 'LICENSE') -Force
    New-Item -ItemType Directory -Path (Join-Path $taskDist 'docs') -Force | Out-Null
    foreach ($taskDoc in @('implementation-status.md','windows-agent-vault-design.md','windows-agent-vault-constraints.md','context-verification.md','mcp-integration.md','privacy.md','code-signing.md','windows-installation.md')) {
        Copy-Item -LiteralPath (Join-Path $taskRoot ('docs/' + $taskDoc)) -Destination (Join-Path $taskDist ('docs/' + $taskDoc)) -Force
    }
    New-Item -ItemType Directory -Path (Join-Path $taskDist 'tools') -Force | Out-Null
    foreach ($taskTool in @('Start-ClaudeContextAudit.ps1','Test-ContextSecrets.ps1','Test-ContextScanner.ps1')) {
        Copy-Item -LiteralPath (Join-Path $taskRoot ('scripts/' + $taskTool)) -Destination (Join-Path $taskDist ('tools/' + $taskTool)) -Force
    }
    New-Item -ItemType Directory -Path (Join-Path $taskDist 'licenses') -Force | Out-Null
    $taskModuleList = & $taskGo list -deps -f '{{with .Module}}{{.Path}}|{{.Dir}}{{end}}' ./cmd/agent-vault
    if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate shipped module licenses' }
    $taskModuleList | Where-Object { $_ } | Sort-Object -Unique | ForEach-Object {
        $taskModule = $_.Split('|',2)
        if ($taskModule.Length -eq 2 -and $taskModule[0] -ne 'agentvault.local/agentvault' -and $taskModule[1]) {
            $taskLicense = Join-Path $taskModule[1] 'LICENSE'
            if (Test-Path -LiteralPath $taskLicense) {
                $taskLicenseName = $taskModule[0].Replace('/','_') + '-LICENSE.txt'
                Copy-Item -LiteralPath $taskLicense -Destination (Join-Path $taskDist ('licenses/' + $taskLicenseName)) -Force
            }
        }
    }
    & $taskGo version -m (Join-Path $taskDist 'agent-vault.exe') | Set-Content -LiteralPath (Join-Path $taskDist 'build-dependencies.txt') -Encoding utf8
    Get-ChildItem -LiteralPath $taskDist -Recurse -File | Where-Object { $_.Name -ne 'SHA256SUMS.txt' -and $_.Extension -ne '.zip' } | ForEach-Object {
        $taskRelative = $_.FullName.Substring($taskDist.Length + 1).Replace('\','/')
        '{0}  {1}' -f (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(),$taskRelative
    } | Set-Content -LiteralPath (Join-Path $taskDist 'SHA256SUMS.txt') -Encoding ascii
    if ($Package) {
        $taskZip = Join-Path $taskRoot 'agent-vault-windows-x64.zip'
        Compress-Archive -Path (Join-Path $taskDist '*') -DestinationPath $taskZip -Force
        Write-Output $taskZip
    }
    Write-Output (Join-Path $taskDist 'agent-vault.exe')
} finally { Pop-Location }
