# Pinix installer for Windows
# Usage: irm dl.pinixai.com/install.ps1 | iex

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$BaseUrl = "https://dl.pinixai.com/releases/latest"
$BunMirror = "https://registry.npmmirror.com/-/binary/bun"
$InstallDir = Join-Path $env:USERPROFILE ".pinix\bin"

# Detect architecture
$Arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
} else {
    Write-Host "Unsupported: 32-bit Windows is not supported." -ForegroundColor Red
    exit 1
}

Write-Host "Installing Pinix (windows/$Arch)..." -ForegroundColor Cyan

# Create install directory
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# Download pinix.exe
$PinixUrl = "$BaseUrl/pinix-windows-$Arch.exe"
$PinixPath = Join-Path $InstallDir "pinix.exe"
Write-Host "  Downloading pinix.exe..."
try {
    Invoke-WebRequest -Uri $PinixUrl -OutFile $PinixPath -UseBasicParsing
} catch {
    Write-Host "Failed to download pinix.exe from $PinixUrl" -ForegroundColor Red
    Write-Host "  $_" -ForegroundColor Red
    exit 1
}

# Download pinixd.exe
$PinixdUrl = "$BaseUrl/pinixd-windows-$Arch.exe"
$PinixdPath = Join-Path $InstallDir "pinixd.exe"
Write-Host "  Downloading pinixd.exe..."
try {
    Invoke-WebRequest -Uri $PinixdUrl -OutFile $PinixdPath -UseBasicParsing
} catch {
    Write-Host "Failed to download pinixd.exe from $PinixdUrl" -ForegroundColor Red
    Write-Host "  $_" -ForegroundColor Red
    exit 1
}

# Add to user PATH if not already there
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    $env:Path = "$env:Path;$InstallDir"
    Write-Host "  Added $InstallDir to PATH"
}

# Verify installation
$Version = & $PinixPath --version 2>&1
Write-Host "Pinix installed: $Version" -ForegroundColor Green

# --- Install Bun (required for running Clips) ---

$BunExe = Join-Path $env:USERPROFILE ".bun\bin\bun.exe"
$HasBun = (Get-Command bun -ErrorAction SilentlyContinue) -or (Test-Path $BunExe)

if (-not $HasBun) {
    Write-Host ""
    Write-Host "Installing Bun (required for Clips)..." -ForegroundColor Cyan

    # Find latest bun version from npm registry
    $BunLatest = try {
        (Invoke-RestMethod "https://registry.npmmirror.com/bun/latest" -UseBasicParsing).version
    } catch { "1.3.14" }  # fallback

    $BunZipUrl = "$BunMirror/bun-v$BunLatest/bun-windows-x64.zip"
    $BunZip = Join-Path $env:TEMP "bun-windows.zip"
    $BunDir = Join-Path $env:USERPROFILE ".bun"
    $BunBinDir = Join-Path $BunDir "bin"

    Write-Host "  Downloading Bun v$BunLatest..."
    try {
        Invoke-WebRequest -Uri $BunZipUrl -OutFile $BunZip -UseBasicParsing

        # Extract
        $BunTmp = Join-Path $env:TEMP "bun-extract"
        if (Test-Path $BunTmp) { Remove-Item $BunTmp -Recurse -Force }
        Expand-Archive -Path $BunZip -DestinationPath $BunTmp -Force

        # Move to ~/.bun/bin/
        if (-not (Test-Path $BunBinDir)) {
            New-Item -ItemType Directory -Path $BunBinDir -Force | Out-Null
        }
        Get-ChildItem (Join-Path $BunTmp "bun-windows-x64") | Move-Item -Destination $BunBinDir -Force

        # Cleanup
        Remove-Item $BunZip -Force -ErrorAction SilentlyContinue
        Remove-Item $BunTmp -Recurse -Force -ErrorAction SilentlyContinue

        # Add bun to PATH
        $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if ($UserPath -notlike "*$BunBinDir*") {
            [Environment]::SetEnvironmentVariable("Path", "$UserPath;$BunBinDir", "User")
            $env:Path = "$env:Path;$BunBinDir"
        }

        $BunVersion = & $BunExe --version 2>&1
        Write-Host "Bun installed: $BunVersion" -ForegroundColor Green
    } catch {
        Write-Host "Warning: Failed to install Bun automatically." -ForegroundColor Yellow
        Write-Host "  Install manually: irm bun.sh/install.ps1 | iex"
    }
}

Write-Host ""
Write-Host "Get started:" -ForegroundColor Cyan
Write-Host "  pinix start                        start Pinix"
Write-Host "  pinix login                        log in to Pinix"
Write-Host "  pinix hub add @pinix/todo          install your first Clip"
Write-Host "  pinix invoke todo list             use a Clip"
Write-Host "  Start-Process http://localhost:9000 open Console"
