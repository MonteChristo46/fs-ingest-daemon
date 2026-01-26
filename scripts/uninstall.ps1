# Requires RunAsAdministrator

$ErrorActionPreference = "Stop"
$InstallDir = "C:\ProgramData\fsd"
$BinName = "fsd.exe"

# 1. Admin Check
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "Please run this script as Administrator."
    exit 1
}

# 2. Uninstall Service
$Target = Join-Path $InstallDir $BinName
if (Test-Path $Target) {
    Write-Host "Stopping and uninstalling service..."
    try {
        & $Target uninstall
    } catch {
        Write-Warning "Service uninstall failed or already removed."
    }
}

# 3. Remove from PATH
$OldPath = [Environment]::GetEnvironmentVariable("Path", "Machine")
if ($OldPath -like "*$InstallDir*") {
    Write-Host "Removing $InstallDir from System PATH..."
    # Remove with trailing semicolon or without
    $NewPath = $OldPath.Replace(";$InstallDir", "").Replace("$InstallDir;", "").Replace($InstallDir, "")
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "Machine")
}

# 4. Remove Directory
if (Test-Path $InstallDir) {
    Write-Host "Removing installation directory..."
    Remove-Item -Path $InstallDir -Recurse -Force
}

Write-Host "✅ Cleanup complete."
