# Pinix installer for Windows
# Usage: irm dl.pinixai.com/install.ps1 | iex
#
# Installs: pinix + pinixd + Bun + Node.js + bb-browser + bb-viewer (stream)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

$BaseUrl       = "https://dl.pinixai.com/releases/latest"
$ViewerBaseUrl = "https://dl.pinixai.com/releases/bb-viewer/latest"
$BunMirror     = "https://registry.npmmirror.com/-/binary/bun"
$NodeMirror    = "https://registry.npmmirror.com/-/binary/node"
$NpmRegistry   = "https://registry.npmmirror.com"

$PinixBin  = Join-Path $env:USERPROFILE ".pinix\bin"
$BunBin    = Join-Path $env:USERPROFILE ".bun\bin"
$NodeDir   = Join-Path $env:USERPROFILE ".node"
$ViewerDir = Join-Path $env:USERPROFILE ".bb-browser\bin"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

function Add-ToUserPath($dir) {
    $p = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($p -notlike "*$dir*") {
        [Environment]::SetEnvironmentVariable("Path", "$p;$dir", "User")
        $env:Path = "$env:Path;$dir"
    }
}

function Download($url, $dest, $label) {
    Write-Host "  $label" -NoNewline
    try {
        Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing
        Write-Host " done" -ForegroundColor Green
    } catch {
        Write-Host " FAILED" -ForegroundColor Red
        throw "Download failed: $url`n  $_"
    }
}

function Ensure-Dir($dir) {
    if (-not (Test-Path $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
}

# Detect architecture
$Arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
} else {
    Write-Host "32-bit Windows is not supported." -ForegroundColor Red; exit 1
}

# ===========================================================================
# 1. Pinix (pinix.exe + pinixd.exe)
# ===========================================================================

Write-Host ""
Write-Host "[1/5] Pinix CLI" -ForegroundColor Cyan
Ensure-Dir $PinixBin

Download "$BaseUrl/pinix-windows-$Arch.exe"  (Join-Path $PinixBin "pinix.exe")  "pinix.exe..."
Download "$BaseUrl/pinixd-windows-$Arch.exe" (Join-Path $PinixBin "pinixd.exe") "pinixd.exe..."
Add-ToUserPath $PinixBin

$v = & (Join-Path $PinixBin "pinix.exe") --version 2>&1
Write-Host "  $v" -ForegroundColor Green

# ===========================================================================
# 2. Bun (required for Clips)
# ===========================================================================

Write-Host ""
Write-Host "[2/5] Bun" -ForegroundColor Cyan

$BunExe = Join-Path $BunBin "bun.exe"
if ((Get-Command bun -ErrorAction SilentlyContinue) -or (Test-Path $BunExe)) {
    $bv = if (Test-Path $BunExe) { & $BunExe --version 2>&1 } else { & bun --version 2>&1 }
    Write-Host "  already installed: $bv" -ForegroundColor Green
} else {
    $BunVer = try { (Invoke-RestMethod "$NpmRegistry/bun/latest" -UseBasicParsing).version } catch { "1.3.14" }
    $BunZip = Join-Path $env:TEMP "bun-win.zip"
    Download "$BunMirror/bun-v$BunVer/bun-windows-x64.zip" $BunZip "Bun v$BunVer..."

    $tmp = Join-Path $env:TEMP "bun-ext"
    if (Test-Path $tmp) { Remove-Item $tmp -Recurse -Force }
    Expand-Archive -Path $BunZip -DestinationPath $tmp -Force
    Ensure-Dir $BunBin
    Get-ChildItem (Join-Path $tmp "bun-windows-x64") | Move-Item -Destination $BunBin -Force
    Remove-Item $BunZip, $tmp -Recurse -Force -ErrorAction SilentlyContinue
    Add-ToUserPath $BunBin

    Write-Host "  Bun $(& $BunExe --version 2>&1)" -ForegroundColor Green
}

# ===========================================================================
# 3. Node.js (required for bb-browser)
# ===========================================================================

Write-Host ""
Write-Host "[3/5] Node.js" -ForegroundColor Cyan

$NodeExe = Join-Path $NodeDir "node.exe"
if ((Get-Command node -ErrorAction SilentlyContinue) -or (Test-Path $NodeExe)) {
    $nv = if (Test-Path $NodeExe) { & $NodeExe --version 2>&1 } else { & node --version 2>&1 }
    Write-Host "  already installed: $nv" -ForegroundColor Green
} else {
    # Get latest LTS version
    $NodeVer = try {
        $listing = Invoke-RestMethod "$NodeMirror/" -UseBasicParsing
        ($listing | Where-Object { $_.name -match '^v22\.' -and $_.type -eq 'dir' } |
         Sort-Object { [version]($_.name -replace '^v','') } -Descending |
         Select-Object -First 1).name -replace '/$',''
    } catch { "v22.16.0" }

    $NodeZip = Join-Path $env:TEMP "node-win.zip"
    $NodeFolder = "node-$NodeVer-win-x64"
    Download "$NodeMirror/$NodeVer/$NodeFolder.zip" $NodeZip "Node.js $NodeVer..."

    $tmp = Join-Path $env:TEMP "node-ext"
    if (Test-Path $tmp) { Remove-Item $tmp -Recurse -Force }
    Expand-Archive -Path $NodeZip -DestinationPath $tmp -Force
    if (Test-Path $NodeDir) { Remove-Item $NodeDir -Recurse -Force }
    Move-Item (Join-Path $tmp $NodeFolder) $NodeDir -Force
    Remove-Item $NodeZip, $tmp -Recurse -Force -ErrorAction SilentlyContinue
    Add-ToUserPath $NodeDir

    Write-Host "  Node.js $(& $NodeExe --version 2>&1)" -ForegroundColor Green
}

# ===========================================================================
# 4. bb-browser (npm package)
# ===========================================================================

Write-Host ""
Write-Host "[4/5] bb-browser" -ForegroundColor Cyan

# Resolve npm path
$NpmCmd = if (Test-Path (Join-Path $NodeDir "npm.cmd")) {
    Join-Path $NodeDir "npm.cmd"
} elseif (Get-Command npm -ErrorAction SilentlyContinue) {
    "npm"
} else { $null }

if (-not $NpmCmd) {
    Write-Host "  skipped: npm not found" -ForegroundColor Yellow
} else {
    $bbVer = try { & $NpmCmd list -g bb-browser --depth=0 2>&1 | Select-String "bb-browser@" } catch { $null }
    if ($bbVer) {
        Write-Host "  already installed: $($bbVer.ToString().Trim())" -ForegroundColor Green
    } else {
        Write-Host "  installing via npm..."
        & $NpmCmd install -g bb-browser --registry $NpmRegistry 2>&1 | Out-Null
        $bbv = try { & (Join-Path $NodeDir "bb-browser.cmd") --version 2>&1 } catch { "installed" }
        Write-Host "  bb-browser $bbv" -ForegroundColor Green
    }
}

# ===========================================================================
# 5. bb-viewer / stream (pre-built binary + DLLs)
# ===========================================================================

Write-Host ""
Write-Host "[5/5] bb-viewer (stream)" -ForegroundColor Cyan

Ensure-Dir $ViewerDir

$viewerExe = Join-Path $ViewerDir "bb-viewer.exe"
if (Test-Path $viewerExe) {
    Write-Host "  already installed" -ForegroundColor Green
} else {
    $files = @(
        @{ name = "bb-viewer-windows-amd64.exe"; dest = "bb-viewer.exe" },
        @{ name = "bb-viewer-windows-amd64.exe"; dest = "bb-viewer" },  # alias without .exe for Node spawn
        @{ name = "libturbojpeg.dll";            dest = "libturbojpeg.dll" },
        @{ name = "libvpx-1.dll";                dest = "libvpx-1.dll" },
        @{ name = "libgcc_s_seh-1.dll";          dest = "libgcc_s_seh-1.dll" },
        @{ name = "libwinpthread-1.dll";         dest = "libwinpthread-1.dll" }
    )
    foreach ($f in $files) {
        Download "$ViewerBaseUrl/$($f.name)" (Join-Path $ViewerDir $f.dest) "$($f.dest)..."
    }

    # DLLs also need to be findable by the system loader when Node spawns bb-viewer
    $sysDlls = @("libturbojpeg.dll", "libvpx-1.dll", "libgcc_s_seh-1.dll", "libwinpthread-1.dll")
    foreach ($dll in $sysDlls) {
        $src = Join-Path $ViewerDir $dll
        $dst = Join-Path $env:SystemRoot "System32\$dll"
        if (-not (Test-Path $dst)) {
            try { Copy-Item $src $dst -Force } catch {
                # Not admin — add viewer dir to PATH instead
                Add-ToUserPath $ViewerDir
                break
            }
        }
    }

    Write-Host "  bb-viewer installed" -ForegroundColor Green
}

# ===========================================================================
# Done
# ===========================================================================

Write-Host ""
Write-Host "Pinix installed successfully!" -ForegroundColor Green
Write-Host ""
Write-Host "Get started:" -ForegroundColor Cyan
Write-Host "  pinix start                        start the daemon"
Write-Host "  pinix login                        log in to Pinix Cloud"
Write-Host "  pinix hub add @pinix/todo          install your first Clip"
Write-Host "  pinix invoke todo list             use a Clip"
Write-Host ""
Write-Host "Note: restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
