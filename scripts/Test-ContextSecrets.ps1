[CmdletBinding()]
param([string[]]$Paths=@(),[string[]]$PrivateKeyPath=@(),[switch]$SkipPasswordPrompt,[switch]$RequireRequestCapture)
$ErrorActionPreference='Stop'

function New-SecretPatterns {
    param([string[]]$Values)
    $patterns=[System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach($value in $Values){
        if([string]::IsNullOrEmpty($value)){continue}
        [void]$patterns.Add($value)
        [void]$patterns.Add([Uri]::EscapeDataString($value))
        [void]$patterns.Add([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($value)))
        $encoded=$value
        for($layer=0;$layer -lt 2;$layer++){
            $encoded=ConvertTo-Json -InputObject $encoded -Compress
            [void]$patterns.Add($encoded.Substring(1,$encoded.Length-2))
        }
    }
    return ,$patterns
}

function Test-ScanText {
    param([string]$Text,$Patterns)
    foreach($pattern in $Patterns){if($Text.IndexOf($pattern,[StringComparison]::Ordinal) -ge 0){return $true}}
    # Catch private-key material even when an imported encrypted key has been
    # reserialized by a tool. Public keys and fingerprints are not matches.
    return $Text -match '-----BEGIN (?:OPENSSH |RSA |EC |DSA |ENCRYPTED )?PRIVATE KEY-----'
}

function Test-JsonValue {
    param($Value,$Patterns,[int]$Depth=0)
    if($Depth -gt 64){throw 'JSON nesting exceeds scanner limit'}
    if($null -eq $Value){return $false}
    if($Value -is [string]){return Test-ScanText $Value $Patterns}
    if($Value -is [System.Collections.IDictionary]){
        foreach($key in $Value.Keys){if((Test-ScanText ([string]$key) $Patterns) -or (Test-JsonValue $Value[$key] $Patterns ($Depth+1))){return $true}}
    }elseif($Value -is [System.Collections.IEnumerable]){
        foreach($item in $Value){if(Test-JsonValue $item $Patterns ($Depth+1)){return $true}}
    }elseif($Value -is [PSCustomObject]){
        foreach($property in $Value.PSObject.Properties){if((Test-ScanText $property.Name $Patterns) -or (Test-JsonValue $property.Value $Patterns ($Depth+1))){return $true}}
    }
    return $false
}

function Invoke-ContextSecretScan {
    param([string[]]$ScanPaths,$Patterns,[switch]$RequireRequests)
    $files=[System.Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    $errorsCount=0;$skipped=0;$matched=0;$scanned=0;$requestFiles=0
    foreach($scanPath in $ScanPaths){
        try{
            $item=Get-Item -LiteralPath $scanPath -Force -ErrorAction Stop
            if($item.PSIsContainer){$items=@(Get-ChildItem -LiteralPath $item.FullName -Recurse -File -Force -ErrorAction Stop)}else{$items=@($item)}
            foreach($file in $items){[void]$files.Add($file.FullName)}
        }catch{$errorsCount++}
    }
    foreach($file in $files){
        $hit=$false
        try{
            $item=Get-Item -LiteralPath $file -Force
            if($item.Attributes -band [IO.FileAttributes]::ReparsePoint){$skipped++;continue}
            if($item.Length -gt 32MB){$skipped++;continue}
            if($item.Extension -notin @('.json','.jsonl','.txt','.log','.md','.ps1','.sh','.yaml','.yml','.toml','.js','.ts','.py')){$skipped++;continue}
            $text=[IO.File]::ReadAllText($file)
            $scanned++
            if($item.Name.EndsWith('.request.json',[StringComparison]::OrdinalIgnoreCase)){$requestFiles++}
            $hit=Test-ScanText $text $Patterns
            if($item.Extension -eq '.json'){
                $decoded=ConvertFrom-Json -InputObject $text -ErrorAction Stop
                if(Test-JsonValue $decoded $Patterns){$hit=$true}
            }elseif($item.Extension -eq '.jsonl'){
                foreach($line in ($text -split '\r?\n')){
                    if([string]::IsNullOrWhiteSpace($line)){continue}
                    $decoded=ConvertFrom-Json -InputObject $line -ErrorAction Stop
                    if(Test-JsonValue $decoded $Patterns){$hit=$true}
                }
            }
            if($hit){$matched++}
        }catch{
            # Never include raw exception text, a matching line or secret.
            $errorsCount++
            if($hit){$matched++}
        }
    }
    $conclusion='NO_MATCH_IN_SCANNED_TEXT'
    if($scanned -eq 0 -or $errorsCount -gt 0 -or $skipped -gt 0){$conclusion='INCOMPLETE'}
    if($RequireRequests -and $requestFiles -eq 0){$conclusion='INCOMPLETE'}
    if($matched -gt 0){$conclusion='MATCH_FOUND'}
    return [PSCustomObject]@{conclusion=$conclusion;scanned_files=$scanned;request_files=$requestFiles;matching_files=$matched;unreadable_or_invalid_files=$errorsCount;skipped_files=$skipped;secret_patterns=$Patterns.Count;private_key_header_check=$true;scope='Selected local text files; not an all-path guarantee'}
}

# Tests may dot-source the matcher without invoking an interactive prompt.
if($MyInvocation.InvocationName -eq '.'){return}
if($Paths.Count -eq 0){Write-Output 'Specify -Paths with only the capture or transcript files for this test.';exit 2}
try{
    $values=[System.Collections.Generic.List[string]]::new()
    if(-not $SkipPasswordPrompt){
        $secret=Read-Host 'Enter the TEST password or passphrase locally (hidden; never put it in command arguments)' -AsSecureString
        $pointer=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($secret)
        try{$plain=[Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)}finally{[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer);$secret.Dispose()}
        if($plain.Length -lt 12){throw 'Use a random test value of at least 12 characters'}
        $values.Add($plain)
        $plain=$null
    }
    foreach($keyPath in $PrivateKeyPath){
        $key=[IO.File]::ReadAllText((Resolve-Path -LiteralPath $keyPath).Path)
        $values.Add($key)
        foreach($line in ($key -split '\r?\n')){if($line.Length -ge 32 -and -not $line.StartsWith('-----')){$values.Add($line)}}
        $key=$null
    }
    $patterns=New-SecretPatterns $values.ToArray()
    $result=Invoke-ContextSecretScan $Paths $patterns -RequireRequests:$RequireRequestCapture
    $values.Clear();$patterns.Clear()
    $result | ConvertTo-Json -Compress
    if($result.conclusion -eq 'MATCH_FOUND'){exit 1}
    if($result.conclusion -eq 'INCOMPLETE'){exit 2}
    exit 0
}catch{Write-Output 'Scanner could not complete. Check the selected paths, file permissions and test input locally.';exit 2}
