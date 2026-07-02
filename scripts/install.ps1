# Requires RunAsAdministrator
$ErrorActionPreference = "Stop"

$VERSION="0.9.5-alpha"

$ESC = [char]27

Write-Host "$ESC[38;2;156;39;176m██╗  ██╗██╗   ██╗███╗   ██╗████████╗    ██████╗  █████╗ ███████╗███╗   ███╗ ██████╗ ███╗   ██╗$ESC[0m`n$ESC[38;2;125;61;168m██║  ██║██║   ██║████╗  ██║╚══██╔══╝    ██╔══██╗██╔══██╗██╔════╝████╗ ████║██╔═══██╗████╗  ██║$ESC[0m`n$ESC[38;2;94;83;160m███████║██║   ██║██╔██╗ ██║   ██║       ██║  ██║███████║█████╗  ██╔████╔██║██║   ██║██╔██╗ ██║$ESC[0m`n$ESC[38;2;63;105;152m██╔══██║██║   ██║██║╚██╗██║   ██║       ██║  ██║██╔══██║██╔══╝  ██║╚██╔╝██║██║   ██║██║╚██╗██║$ESC[0m`n$ESC[38;2;32;127;144m██║  ██║╚██████╔╝██║ ╚████║   ██║       ██████╔╝██║  ██║███████╗██║ ╚═╝ ██║╚██████╔╝██║ ╚████║$ESC[0m`n$ESC[38;2;0;150;136m╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝   ╚═╝       ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═══╝$ESC[0m`n" -NoNewline
Write-Host " $ESC[38;2;200;200;200mDAEMON INSTALLER | v$VERSION$ESC[0m"
Write-Host ""
Write-Host "╔══════════════════════════════════════════════════════╗"
Write-Host "║  Glitch Hunt — Edge Daemon Installer                ║"
Write-Host "║                                                      ║"
Write-Host "║  What this will do:                                 ║"
Write-Host "║  • Download the daemon binary                       ║"
Write-Host "║  • Install it as a background service               ║"
Write-Host "║  • Guide you through setup (press Enter for default)║"
Write-Host "╚══════════════════════════════════════════════════════╝"
Write-Host ""
$null = Read-Host "Press Enter to continue or Ctrl+C to cancel"
Write-Host ""

# Configuration
$Url = "https://github.com/MonteChristo46/fs-ingest-daemon/raw/main/build/hunt-windows-amd64.exe"
$BinName = "hunt.exe"
$InstallDir = "C:\ProgramData\hunt"
$PathScope = "Machine"

# 1. Check Privileges
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
$IsAdmin = $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

if (-not $IsAdmin) {
    Write-Host "  ✖ Requires Administrator privileges."
    Write-Host "  Run PowerShell as Administrator and try again."
    exit 1
}

Write-Host "  ✓ Running as Administrator"

# 2. Create Directory
Write-Host "  ⚙ Install directory: $InstallDir"
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
    Write-Warning "  ⚠ Could not set directory permissions. Adjust manually if needed."
}

# 3. Download Binary
$Target = Join-Path $InstallDir $BinName
Write-Host "  ↓ Downloading daemon..."
$ProgressPreference = 'SilentlyContinue'
Invoke-WebRequest -Uri $Url -OutFile $Target
$ProgressPreference = 'Continue'

# Unblock the file (Fix for "Access Denied" / Mark of the Web)
Unblock-File -Path $Target

# 4. Update PATH (Persistent)
$CurrentPath = [Environment]::GetEnvironmentVariable("Path", $PathScope)
if ($CurrentPath -notlike "*$InstallDir*") {
    Write-Host "  🔗 Adding $InstallDir to system PATH..."
    [Environment]::SetEnvironmentVariable("Path", "$CurrentPath;$InstallDir", $PathScope)
    $env:Path += ";$InstallDir"
} else {
    Write-Host "  🔗 PATH already configured"
}

# 5. Run Install
Write-Host ""
Write-Host "  ── Configuration ──"
Write-Host "  (Press Enter to accept each [default])"
Write-Host ""
& $Target install

Write-Host ""
Write-Host "  Done. Use 'hunt' to manage the daemon."
Write-Host "  (You may need to restart your terminal for PATH changes.)"