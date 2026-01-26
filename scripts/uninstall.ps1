# Requires RunAsAdministrator

$ErrorActionPreference = "Stop"
$InstallDir = "C:\ProgramData\fsd"
$BinName = "fsd.exe"

# 1. Check Privileges
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
$IsAdmin = $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

if ($IsAdmin) {
    Write-Host "Uninstalling System Service (ADMIN)..."
    $InstallDir = "C:\ProgramData\fsd"
    $PathScope = "Machine"
} else {
    Write-Host "Uninstalling User Service..."
    $InstallDir = Join-Path $env:LOCALAPPDATA "fsd"
    $PathScope = "User"
}

$BinName = "fsd.exe"

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
$OldPath = [Environment]::GetEnvironmentVariable("Path", $PathScope)
if ($OldPath -like "*$InstallDir*") {
    Write-Host "Removing $InstallDir from $PathScope PATH..."
    # Clean removal handling potential semicolon variations
    $NewPath = $OldPath.Replace(";$InstallDir", "").Replace("$InstallDir;", "").Replace($InstallDir, "")
    [Environment]::SetEnvironmentVariable("Path", $NewPath, $PathScope)
}

# 4. Remove Directory
if (Test-Path $InstallDir) {
    Write-Host "Removing installation directory: $InstallDir"
    Remove-Item -Path $InstallDir -Recurse -Force
}

Write-Host "✅ Cleanup complete."
