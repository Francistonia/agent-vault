[CmdletBinding()]
param([string]$OutputDirectory='')
$ErrorActionPreference='Stop'
if(-not $OutputDirectory){$OutputDirectory=Join-Path $env:LOCALAPPDATA ('AgentVault-Audit\'+(Get-Date -Format 'yyyyMMdd-HHmmss')+'-'+[Guid]::NewGuid().ToString('N').Substring(0,8))}
$taskDirectory=New-Item -ItemType Directory -Path $OutputDirectory -ErrorAction Stop
$taskSid=[Security.Principal.WindowsIdentity]::GetCurrent().User
$taskAcl=[Security.AccessControl.DirectorySecurity]::new()
$taskAcl.SetAccessRuleProtection($true,$false)
$taskAcl.SetOwner($taskSid)
foreach($taskIdentity in @($taskSid,[Security.Principal.SecurityIdentifier]::new('S-1-5-18'))){
    $taskRule=[Security.AccessControl.FileSystemAccessRule]::new($taskIdentity,'FullControl','ContainerInherit,ObjectInherit','None','Allow')
    $taskAcl.AddAccessRule($taskRule)
}
Set-Acl -LiteralPath $taskDirectory.FullName -AclObject $taskAcl
$taskChanges=@{
    CLAUDE_CODE_ENABLE_TELEMETRY='1'
    OTEL_METRICS_EXPORTER='none'
    OTEL_TRACES_EXPORTER='none'
    OTEL_LOGS_EXPORTER='console'
    OTEL_LOG_RAW_API_BODIES=('file:'+$taskDirectory.FullName)
}
$taskPrevious=@{}
try{
    foreach($taskName in $taskChanges.Keys){$taskPrevious[$taskName]=[Environment]::GetEnvironmentVariable($taskName,'Process');[Environment]::SetEnvironmentVariable($taskName,$taskChanges[$taskName],'Process')}
    Write-Host ('Local capture directory: '+$taskDirectory.FullName)
    Write-Host 'Use a NEW conversation with dedicated TEST credentials. Do not paste any passwords into the conversation.'
    Write-Host 'Request and response bodies will remain on this computer. Exit Claude when the test finishes.'
    & claude
}finally{
    foreach($taskName in $taskPrevious.Keys){
        if($null -eq $taskPrevious[$taskName]){[Environment]::SetEnvironmentVariable($taskName,[NullString]::Value,'Process')}
        else{[Environment]::SetEnvironmentVariable($taskName,$taskPrevious[$taskName],'Process')}
    }
    $taskCount=@(Get-ChildItem -LiteralPath $taskDirectory.FullName -Filter '*.request.json' -File -ErrorAction SilentlyContinue).Count
    Write-Host ('Captured request files: '+$taskCount)
    if($taskCount -eq 0){Write-Host 'INCOMPLETE: no request files captured; this run provides no request-body evidence.'}
    Write-Host ('Scan locally with Test-ContextSecrets.ps1 -Paths "'+$taskDirectory.FullName+'"')
}
