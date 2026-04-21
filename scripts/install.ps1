# Requires RunAsAdministrator
$ErrorActionPreference = "Stop"

$VERSION="0.8.2-alpha"

$ESC = [char]27

Write-Host "$ESC[38;2;156;39;176m   __                __ $ESC[0m`n$ESC[38;2;117;66;166m  / /_  __  ______  / /_$ESC[0m`n$ESC[38;2;78;93;156m / __ \/ / / / __ \/ __/$ESC[0m`n$ESC[38;2;39;120;146m/ / / / /_/ / / / / /_  $ESC[0m`n$ESC[38;2;0;150;136m/_/ /_/\__,_/_/ /_/\__/ $ESC[0m`n" -NoNewline
Write-Host " $ESC[38;2;200;200;200mDAEMON INSTALLER | v$VERSION$ESC[0m`n"

# Configuration
$Url = "https://github.com/MonteChristo46/fs-ingest-daemon/raw/main/build/hunt-windows-amd64.exe"
$BinName = "hunt.exe"
$InstallDir = "C:\ProgramData\hunt"
$PathScope = "Machine"

# 1. Check Privileges
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
$IsAdmin = $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

if (-not $IsAdmin) {
    Write-Host "[ERROR] This script must be run as an Administrator."
    exit 1
}

Write-Host "[SYSTEM] Running as ADMINISTRATOR"

# 2. Create Directory
Write-Host "[CONFIG] Target Directory: $InstallDir"
if (-not (Test-Path -Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}

# Fix Permissions: Ensure users can read/execute in this directory
try {
    $Acl = Get-Acl $InstallDir
    $Ar = New-Object System.Security.AccessControl.FileSystemAccessRule("Users", "ReadAndExecute", "ContainerInherit,ObjectInherit", "None", "Allow")
    $Acl.SetAccessRule($Ar)
    Set-Acl $InstallDir $Acl
} catch {
    Write-Warning "[CONFIG] Could not explicitly set directory permissions. You might need to adjust them manually."
}

# 3. Download Binary
$Target = Join-Path $InstallDir $BinName
Write-Host "[STATUS] Downloading Hunt daemon..."
Invoke-WebRequest -Uri $Url -OutFile $Target

# Unblock the file (Fix for "Access Denied" / Mark of the Web)
Unblock-File -Path $Target

# 4. Update PATH (Persistent)
$CurrentPath = [Environment]::GetEnvironmentVariable("Path", $PathScope)
if ($CurrentPath -notlike "*$InstallDir*") {
    Write-Host "[CONFIG] Adding $InstallDir to $PathScope PATH..."
    [Environment]::SetEnvironmentVariable("Path", "$CurrentPath;$InstallDir", $PathScope)
    $env:Path += ";$InstallDir" # Update current session
} else {
    Write-Host "[CONFIG] PATH already configured."
}

# 5. Run Install
Write-Host "[STATUS] Running hunt install..."
Write-Host "--------------------------------------------------"
& $Target install

Write-Host "--------------------------------------------------"
Write-Host "[SUCCESS] Installation wrapper complete. You can now use 'hunt'."
Write-Host "[INFO] You may need to restart your terminal for PATH changes to take effect."