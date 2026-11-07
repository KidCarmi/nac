$ErrorActionPreference = 'Stop'
if (-not (Test-Path 'C:\NACLab\probe.ps1')) { throw 'probe script not staged' }
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File C:\NACLab\probe.ps1'
$trigger = New-ScheduledTaskTrigger -AtStartup
Register-ScheduledTask -TaskName 'NACLabWiredProbe' -Action $action -Trigger $trigger -User 'SYSTEM' -RunLevel Highest -Force | Out-Null
Write-Output 'SCHEDULED_STARTUP_PROBE_NIC_ONLY'
Write-Output ((Get-ScheduledTask -TaskName 'NACLabWiredProbe').State)
