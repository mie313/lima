param(
    [ValidateSet("startup", "user")]
    [string]$Phase = "startup",
    [string]$CIDATARoot = ""
)

$ErrorActionPreference = "Stop"

function Write-LimaInfo {
    param([string]$Message)
    Write-Host ("LIMA {0}| {1}" -f (Get-Date -Format o), $Message)
}

function Write-LimaWarning {
    param([string]$Message)
    Write-Warning ("LIMA {0}| {1}" -f (Get-Date -Format o), $Message)
}

function Resolve-CIDataRoot {
    param([string]$ExplicitRoot)

    if ($ExplicitRoot) {
        return $ExplicitRoot.TrimEnd("\")
    }

    foreach ($drive in (Get-PSDrive -PSProvider FileSystem | Sort-Object Name)) {
        $root = $drive.Root.TrimEnd("\")
        if ((Test-Path -LiteralPath (Join-Path $root "autounattend.xml")) -and (Test-Path -LiteralPath (Join-Path $root "lima.env"))) {
            return $root
        }
    }

    throw "Could not locate CIDATA drive. Expected files: autounattend.xml and lima.env"
}

function Import-EnvFile {
    param([string]$Path)

    if (-not (Test-Path -LiteralPath $Path)) {
        return
    }

    foreach ($line in Get-Content -LiteralPath $Path) {
        if ([string]::IsNullOrWhiteSpace($line)) {
            continue
        }
        if ($line.StartsWith("#")) {
            continue
        }
        $idx = $line.IndexOf("=")
        if ($idx -le 0) {
            continue
        }
        $name = $line.Substring(0, $idx)
        $value = $line.Substring($idx + 1)
        [Environment]::SetEnvironmentVariable($name, $value, "Process")
    }
}

function Get-CIDataVar {
    param([string]$Name)
    return [Environment]::GetEnvironmentVariable($Name, "Process")
}

function Get-ProvisionFiles {
    param(
        [string]$Root,
        [string]$Mode
    )
    $dir = Join-Path $Root ("provision.{0}" -f $Mode)
    if (-not (Test-Path -LiteralPath $dir)) {
        return @()
    }
    return @(Get-ChildItem -LiteralPath $dir -File | Sort-Object Name)
}

function Invoke-ProvisionScriptFile {
    param(
        [System.IO.FileInfo]$File,
        [string]$Mode
    )

    Write-LimaInfo ("Executing {0} ({1})" -f $File.FullName, $Mode)
    $tempFile = Join-Path ([System.IO.Path]::GetTempPath()) ("lima-provision-{0}.ps1" -f [System.IO.Path]::GetRandomFileName())
    try {
        $content = Get-Content -LiteralPath $File.FullName -Raw
        Set-Content -LiteralPath $tempFile -Value $content -Encoding UTF8
        & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $tempFile
        if ($LASTEXITCODE -ne 0) {
            throw ("Provision script exited with code {0}: {1}" -f $LASTEXITCODE, $File.FullName)
        }
    }
    finally {
        if (Test-Path -LiteralPath $tempFile) {
            Remove-Item -LiteralPath $tempFile -Force
        }
    }
}

function Invoke-DataProvisionFile {
    param(
        [System.IO.FileInfo]$File
    )

    $id = $File.Name
    $path = Get-CIDataVar ("LIMA_CIDATA_DATAFILE_{0}_PATH" -f $id)
    $overwrite = (Get-CIDataVar ("LIMA_CIDATA_DATAFILE_{0}_OVERWRITE" -f $id))
    if (-not $path) {
        throw ("missing data provision target path for {0}" -f $File.FullName)
    }
    if ((Test-Path -LiteralPath $path) -and ($overwrite -eq "false")) {
        Write-LimaInfo ("Not overwriting {0}" -f $path)
        return
    }

    $dir = Split-Path -Path $path -Parent
    if ($dir) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
    [System.IO.File]::WriteAllBytes($path, [System.IO.File]::ReadAllBytes($File.FullName))
    Write-LimaInfo ("Copied {0} to {1}" -f $File.FullName, $path)
}

function Invoke-YQProvisionFile {
    param(
        [System.IO.FileInfo]$File
    )

    throw ("mode 'yq' is not supported on Windows guest yet ({0})" -f $File.FullName)
}

function Invoke-DefaultDependencyProvision {
    param([string]$Root)

    Write-LimaInfo "Applying default dependency provisioning for Windows guest"

    Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
    Start-Service sshd
    Set-Service -Name sshd -StartupType Automatic

    Remove-NetFirewallRule -Name "OpenSSH-Server-In-TCP" -ErrorAction Ignore
    New-NetFirewallRule -Name "OpenSSH-Server-In-TCP" -DisplayName "OpenSSH Server (sshd)" -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22

    $pubkeyPath = Join-Path $Root "ssh_authorized_keys"
    if (Test-Path -LiteralPath $pubkeyPath) {
        $pubkeyLocation = "C:\ProgramData\ssh\administrators_authorized_keys"
        $keys = Get-Content -LiteralPath $pubkeyPath
        Set-Content -LiteralPath $pubkeyLocation -Value $keys -Encoding ASCII
        icacls $pubkeyLocation /inheritance:r
        icacls $pubkeyLocation /grant "SYSTEM:F"
        icacls $pubkeyLocation /grant "Administrators:F"
    }

    if (-not (Test-Path -LiteralPath "C:\ProgramData\chocolatey\choco.exe")) {
        Set-ExecutionPolicy Bypass -Scope Process -Force
        [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
        iex ((New-Object System.Net.WebClient).DownloadString("https://community.chocolatey.org/install.ps1"))
    }

    & C:\ProgramData\chocolatey\choco.exe install winfsp -y --pre
    if ($LASTEXITCODE -ne 0) {
        throw ("failed to install WinFSP with choco (exit code {0})" -f $LASTEXITCODE)
    }

    $virtioCandidates = @(
        "E:\viofs\2k25\amd64\virtiofs.exe",
        "E:\viofs\w11\amd64\virtiofs.exe"
    )
    $virtioBinary = $virtioCandidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    if ($virtioBinary) {
        if (-not (Get-Service -Name "VirtioFsSvc" -ErrorAction Ignore)) {
            New-Service -Name "VirtioFsSvc" -BinaryPathName $virtioBinary -DisplayName "VirtioFsSvc" -StartupType Automatic
        }
        Start-Service -Name "VirtioFsSvc" -ErrorAction Ignore
    }
}

$baseDir = "C:\ProgramData\Lima"
$stateDir = Join-Path $baseDir "state"
$logDir = Join-Path $baseDir "logs"
New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
New-Item -ItemType Directory -Path $logDir -Force | Out-Null

$logPath = Join-Path $logDir ("provision-{0}.log" -f $Phase)
Start-Transcript -Path $logPath -Append
try {
    $resolvedCIDATA = Resolve-CIDataRoot -ExplicitRoot $CIDATARoot
    Import-EnvFile -Path (Join-Path $resolvedCIDATA "lima.env")
    Import-EnvFile -Path (Join-Path $resolvedCIDATA "param.env")

    $iid = Get-CIDataVar "LIMA_CIDATA_IID"
    if (-not $iid) {
        throw "LIMA_CIDATA_IID is not set in lima.env"
    }

    $stateFile = Join-Path $stateDir ("last-{0}-iid.txt" -f $Phase)
    if (Test-Path -LiteralPath $stateFile) {
        $lastIID = Get-Content -LiteralPath $stateFile -Raw
        if ($lastIID.Trim() -eq $iid) {
            Write-LimaInfo ("Phase '{0}' already ran for IID {1}, skipping" -f $Phase, $iid)
            exit 0
        }
    }

    if ($Phase -eq "startup") {
        foreach ($f in (Get-ProvisionFiles -Root $resolvedCIDATA -Mode "boot")) {
            Invoke-ProvisionScriptFile -File $f -Mode "boot"
        }
        foreach ($f in (Get-ProvisionFiles -Root $resolvedCIDATA -Mode "dependency")) {
            Invoke-ProvisionScriptFile -File $f -Mode "dependency"
        }
        if ((Get-CIDataVar "LIMA_CIDATA_SKIP_DEFAULT_DEPENDENCY_RESOLUTION") -ne "1") {
            Invoke-DefaultDependencyProvision -Root $resolvedCIDATA
        }
        foreach ($f in (Get-ProvisionFiles -Root $resolvedCIDATA -Mode "data")) {
            Invoke-DataProvisionFile -File $f
        }
        foreach ($f in (Get-ProvisionFiles -Root $resolvedCIDATA -Mode "yq")) {
            Invoke-YQProvisionFile -File $f
        }
        foreach ($f in (Get-ProvisionFiles -Root $resolvedCIDATA -Mode "system")) {
            Invoke-ProvisionScriptFile -File $f -Mode "system"
        }
    }
    else {
        foreach ($f in (Get-ProvisionFiles -Root $resolvedCIDATA -Mode "user")) {
            Invoke-ProvisionScriptFile -File $f -Mode "user"
        }
    }

    Set-Content -LiteralPath $stateFile -Value $iid -NoNewline -Encoding ASCII
    Write-LimaInfo ("Completed phase '{0}' for IID {1}" -f $Phase, $iid)
}
catch {
    Write-LimaWarning ("Provision phase '{0}' failed: {1}" -f $Phase, $_)
    exit 1
}
finally {
    Stop-Transcript
}
