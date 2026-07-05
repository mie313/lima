$logfile = "C:\Users\{{.User}}\lima-setup.log"

# Record logs
Start-Transcript -Path $logfile -Append

# We need to change password because the current password is specified in autounattend.xml, so all users/processes can see it.
## Generate a random 16 character password.
## Avoid special characters to minimize potential keyboard layout issue.
$chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
$newPassword = -join ((1..16) | ForEach-Object { $chars[(Get-Random -Maximum $chars.Length)] })

## Store the password under the user directory so that user can know/change it.
$newPassword | Out-File -FilePath "C:\Users\{{.User}}\password.txt" -Encoding utf8 -NoNewline

## Change the password
$username = $env:USERNAME
$newSecurePassword = ConvertTo-SecureString $newPassword -AsPlainText -Force
Set-LocalUser -Name $username -Password $newSecurePassword

# Install per-boot provision runner and task scheduler entries.
$cidataRoot = Split-Path -Path $PSScriptRoot -Qualifier
powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$cidataRoot\install_scheduler.ps1" -CIDATARoot "$cidataRoot\"

# Execute provision phases for the current boot so scripts don't have to wait for the next reboot.
$runnerPath = "C:\ProgramData\Lima\provision\runner.ps1"
powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$runnerPath" -Phase startup -CIDATARoot "$cidataRoot\"
powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$runnerPath" -Phase user -CIDATARoot "$cidataRoot\"

# Finish recording logs
Stop-Transcript
