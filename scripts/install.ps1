# Requires RunAsAdministrator

$ErrorActionPreference = "Stop"

# Configuration
# URL to the raw executable on GitHub
$Url = "https://github.com/MonteChristo46/fs-ingest-daemon/raw/main/fsd.exe"
$InstallDir = "C:\ProgramData\fsd"
$BinName = "fsd.exe"

# 1. Admin Check
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "Please run this script as Administrator."
    exit 1
}

# 2. Create Directory
if (-not (Test-Path -Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
    Write-Host "Created directory $InstallDir"
}

# 3. Download Binary
$Target = Join-Path $InstallDir $BinName

Write-Host "Downloading $Url..."
Invoke-WebRequest -Uri $Url -OutFile $Target


# 4. Update PATH (Persistent)
$CurrentPath = [Environment]::GetEnvironmentVariable("Path", "Machine")
if ($CurrentPath -notlike "*$InstallDir*") {
    Write-Host "Adding $InstallDir to System PATH..."
    [Environment]::SetEnvironmentVariable("Path", "$CurrentPath;$InstallDir", "Machine")
    $env:Path += ";$InstallDir" # Update current session too
} else {
    Write-Host "PATH already configured."
}

# 5. Run Install
Write-Host "Running fsd install..."
& $Target install

Write-Host "`n✅ Installation wrapper complete."
Write-Host "You may need to restart your terminal for PATH changes to take effect."
