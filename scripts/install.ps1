# Requires RunAsAdministrator

$ErrorActionPreference = "Stop"

# Configuration
# URL to the raw executable on GitHub
$Url = "https://github.com/MonteChristo46/fs-ingest-daemon/raw/main/fsd.exe"
$InstallDir = "C:\ProgramData\fsd"
$BinName = "fsd.exe"

# 1. Check Privileges

$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())

$IsAdmin = $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)



if ($IsAdmin) {

    Write-Host "Running as ADMINISTRATOR (System Install)"

    $InstallDir = "C:\ProgramData\fsd"

    $PathScope = "Machine"

} else {

    Write-Host "Running as USER (User Install)"

    $InstallDir = Join-Path $env:LOCALAPPDATA "fsd"

    $PathScope = "User"

}



# 2. Create Directory



if (-not (Test-Path -Path $InstallDir)) {



    New-Item -ItemType Directory -Path $InstallDir | Out-Null



    Write-Host "Created directory $InstallDir"



}







# Fix Permissions: Ensure users can read/execute in this directory



# (Important for ProgramData installs so non-admins can at least run 'fsd version' or see logs if allowed)



try {



    $Acl = Get-Acl $InstallDir



    $Ar = New-Object System.Security.AccessControl.FileSystemAccessRule("Users", "ReadAndExecute", "ContainerInherit,ObjectInherit", "None", "Allow")



    $Acl.SetAccessRule($Ar)



    Set-Acl $InstallDir $Acl



} catch {



    Write-Warning "Could not explicitly set directory permissions. You might need to adjust them manually."



}







# 3. Download Binary

$Target = Join-Path $InstallDir $BinName



Write-Host "Downloading $Url..."



Invoke-WebRequest -Uri $Url -OutFile $Target







# Unblock the file (Fix for "Access Denied" / Mark of the Web)



Unblock-File -Path $Target







# 4. Update PATH (Persistent)

$CurrentPath = [Environment]::GetEnvironmentVariable("Path", $PathScope)

if ($CurrentPath -notlike "*$InstallDir*") {

    Write-Host "Adding $InstallDir to $PathScope PATH..."

    [Environment]::SetEnvironmentVariable("Path", "$CurrentPath;$InstallDir", $PathScope)

    $env:Path += ";$InstallDir" # Update current session

} else {

    Write-Host "PATH already configured."

}



# 5. Run Install
Write-Host "Running fsd install..."
& $Target install

Write-Host "`n✅ Installation wrapper complete."
Write-Host "You may need to restart your terminal for PATH changes to take effect."
