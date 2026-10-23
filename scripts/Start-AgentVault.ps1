$ErrorActionPreference = 'Stop'
$taskExe = Join-Path $PSScriptRoot 'agent-vault.exe'
if (-not (Test-Path -LiteralPath $taskExe)) { throw 'agent-vault.exe was not found. Extract the complete package first.' }
Start-Process -FilePath $taskExe -ArgumentList 'serve' -WorkingDirectory $PSScriptRoot -WindowStyle Hidden
