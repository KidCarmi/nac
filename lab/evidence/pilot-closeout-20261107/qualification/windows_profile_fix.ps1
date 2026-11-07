$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$root = 'C:\NACLab'
Start-Transcript -Path "$root\profile-fix.log" -Force | Out-Null
try {
    $caThumb = '45FEA4D2DC3BFE8A3358139E46478D3C7589ADFE'
    $ca = @(Get-ChildItem Cert:\LocalMachine\Root | Where-Object Thumbprint -eq $caThumb)
    if ($ca.Count -ne 1) {
        $ca = @(Import-Certificate -FilePath "$root\ca.crt" -CertStoreLocation Cert:\LocalMachine\Root)
    }
    if ($ca.Count -ne 1 -or $ca[0].Thumbprint -ne $caThumb) { throw 'Lab CA not trusted in machine root store' }
    $client = @(Get-ChildItem Cert:\LocalMachine\My | Where-Object Subject -eq 'CN=wired-endpoint-01')
    if ($client.Count -ne 1 -or -not $client[0].HasPrivateKey) { throw 'Client certificate or private key missing' }
    Write-Output "MACHINE_CA_THUMBPRINT $caThumb"
    Write-Output "CLIENT_CERT_SUBJECT $($client[0].Subject) HAS_PRIVATE_KEY $($client[0].HasPrivateKey)"

    $probe = "$root\probe.ps1"
    if (-not (Test-Path $probe)) { throw 'Bounded endpoint probe missing' }
    $action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File C:\NACLab\probe.ps1'
    $trigger = New-ScheduledTaskTrigger -AtStartup
    Register-ScheduledTask -TaskName 'NACLabWiredProbe' -Action $action -Trigger $trigger -User 'SYSTEM' -RunLevel Highest -Force | Out-Null
    Write-Output 'STARTUP_PROBE_REGISTERED'

    $path = "$root\wired.xml"
    $xml = [System.IO.File]::ReadAllText($path)
    if (-not $xml.Contains('http://www.microsoft.com/networking/LAN/profile/v1')) { throw 'Unexpected wired XML namespace' }
    if (-not $xml.Contains('<eapTls:TrustedRootCA>')) {
        $xml = $xml.Replace('</eapTls:ServerValidation>',
            "<eapTls:TrustedRootCA>$caThumb</eapTls:TrustedRootCA></eapTls:ServerValidation>")
    }
    [System.IO.File]::WriteAllText($path,$xml,[System.Text.Encoding]::ASCII)
    $deleted = & netsh.exe lan delete profile 2>&1
    Write-Output "DELETE_PROFILE $deleted"
    $added = & netsh.exe lan add profile "filename=$path" 2>&1
    if ($LASTEXITCODE -ne 0) { throw "Import with pinned CA failed: $added" }
    Write-Output "PINNED_CA_PROFILE_IMPORTED $added"
    Write-Output (& netsh.exe lan show profiles 2>&1)
    Start-ScheduledTask -TaskName 'NACLabWiredProbe'
    Write-Output 'BOUNDED_PROBE_STARTED'
} catch {
    Write-Output "PROFILE_FIX_ERROR $($_.Exception.Message)"
    throw
} finally {
    Stop-Transcript | Out-Null
}
