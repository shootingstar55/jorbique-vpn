# Local Windows release build (frontend + go-winres + Jorbique-VPN-Extended.exe).
# sing-box is NOT embedded — the client downloads it from the update server on
# first launch (with a progress bar). Requires: Go, Node/npm, PowerShell 5.1+.
param(
    [string]$AppVersion = $env:APP_VERSION
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $root

# The version comes from scripts/app-version.sh — the same resolver the Unix
# build scripts use. A second implementation here would be one more thing to
# keep in step (its resolution order, its validation), and the two would drift
# the first time either changed. Git for Windows installs bash next to git, so
# it is there wherever this repository was cloned.
function Resolve-AppVersion {
    param([string]$Current)
    if (-not [string]::IsNullOrWhiteSpace($Current)) { $env:APP_VERSION = $Current.Trim() }

    $bash = Get-Command bash -ErrorAction SilentlyContinue
    if (-not $bash) {
        Write-Warning 'bash not found (install Git for Windows) — building as a dev version'
        return '0.0.0-dev'
    }
    # Forward slashes: bash from Git for Windows resolves them, backslashes it
    # would read as escapes.
    $resolver = (Join-Path $root 'scripts\app-version.sh') -replace '\\', '/'
    $version = & $bash.Source $resolver
    if ($LASTEXITCODE -ne 0) {
        throw "app-version.sh failed (exit $LASTEXITCODE)"
    }
    return (@($version) | Select-Object -First 1).ToString().Trim()
}

$appVersion = Resolve-AppVersion -Current $AppVersion
Write-Host "==> Jorbique VPN Extended Windows build (version: $appVersion)"

Write-Host '==> frontend'
$frontendDir = Join-Path $root 'frontend'
Push-Location $frontendDir
npm ci
npm run build
Pop-Location

# sing-box is not bundled/embedded: the client downloads it from the update server
# on first launch (with a progress bar).

Write-Host '==> version metadata'
$env:APP_VERSION = $appVersion
node (Join-Path $root 'scripts\bump-windows-version.mjs')

Write-Host '==> Windows resources'
$iconIco = Join-Path $root 'winres\icon.ico'
if (Test-Path $iconIco) {
    Copy-Item $iconIco (Join-Path $root 'internal\tray\tray_icon.ico') -Force
}
go run github.com/tc-hib/go-winres@v0.3.3 make --out cmd/jorbiquevpn/rsrc

$outDir = Join-Path $root 'release\binaries'
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$outExe = Join-Path $outDir 'Jorbique-VPN-Extended.exe'
$outZip = Join-Path $outDir 'Jorbique-VPN-Extended-windows.zip'
Write-Host '==> go build'
# 'production' is required by Wails itself (dev vs production runtime); it no
# longer embeds sing-box (that file was removed) — the client downloads it.
go build -tags desktop,production `
    -ldflags "-w -s -H windowsgui -X jorbiquevpn/internal/constants.AppVersion=$appVersion" `
    -o $outExe ./cmd/jorbiquevpn
if (Test-Path $outZip) { Remove-Item $outZip -Force }
Compress-Archive -Path $outExe -DestinationPath $outZip -Force
Write-Host "Built $outZip ($outExe inside)"
