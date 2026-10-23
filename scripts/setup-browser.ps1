$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
Push-Location (Join-Path $taskRoot 'runtime')
try {
    npm.cmd ci --ignore-scripts --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { throw 'Browser dependency installation failed' }
    $taskBinary = Join-Path $taskRoot 'runtime/node_modules/agent-browser/bin/agent-browser-win32-x64.exe'
    if (-not (Test-Path -LiteralPath $taskBinary)) { throw 'Pinned native browser binary not present in package; no unverified download fallback is enabled.' }
    & $taskBinary --version
    Write-Output 'agent-browser is installed. An existing Chrome installation is used; install Chrome separately if it is absent.'
} finally { Pop-Location }
