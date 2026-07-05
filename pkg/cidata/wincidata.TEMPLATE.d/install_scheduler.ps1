param(
    [string]$CIDATARoot = "F:\"
)

$ErrorActionPreference = "Stop"

$provisionDir = "C:\ProgramData\Lima\provision"
New-Item -ItemType Directory -Path $provisionDir -Force | Out-Null

$runnerSource = Join-Path $CIDATARoot "provision_runner.ps1"
$runnerTarget = Join-Path $provisionDir "runner.ps1"
Copy-Item -LiteralPath $runnerSource -Destination $runnerTarget -Force

$startupAction = New-ScheduledTaskAction -Execute "powershell.exe" -Argument ("-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"{0}`" -Phase startup" -f $runnerTarget)
$startupTrigger = New-ScheduledTaskTrigger -AtStartup
$startupPrincipal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName "LimaProvisionStartup" -TaskPath "\Lima\" -Action $startupAction -Trigger $startupTrigger -Principal $startupPrincipal -Force | Out-Null

$userAction = New-ScheduledTaskAction -Execute "powershell.exe" -Argument ("-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"{0}`" -Phase user" -f $runnerTarget)
$userTrigger = New-ScheduledTaskTrigger -AtLogOn -User "{{.User}}"
$userPrincipal = New-ScheduledTaskPrincipal -UserId "{{.User}}" -LogonType InteractiveToken -RunLevel Highest
Register-ScheduledTask -TaskName "LimaProvisionUser" -TaskPath "\Lima\" -Action $userAction -Trigger $userTrigger -Principal $userPrincipal -Force | Out-Null
