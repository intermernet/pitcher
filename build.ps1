#Requires -Version 5.1
<#
.SYNOPSIS
    Build script for Pitcher.
.DESCRIPTION
    Checks prerequisites, generates Wails JS bindings, builds the frontend,
    and compiles the final pitcher.exe binary.
    Prerequisites: Go 1.25+, npm.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Fail([string]$msg) { Write-Error $msg; exit 1 }
function Info([string]$msg) { Write-Host "==> $msg" -ForegroundColor Cyan }

# --- Go ---
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail "Go is not installed. See https://go.dev/dl/"
}
$goVer = (go version) -replace '.*go(\d+\.\d+)[\.\d]*.*', '$1'
$parts = $goVer.Split('.')
if ([int]$parts[0] -lt 1 -or ([int]$parts[0] -eq 1 -and [int]$parts[1] -lt 25)) {
    Fail "Go 1.25 or later required (found $goVer). See https://go.dev/dl/"
}
Info "Go $goVer"

# --- npm ---
if (-not (Get-Command npm -ErrorAction SilentlyContinue)) {
    Fail "npm is not installed. See https://nodejs.org/"
}
Info "npm $(npm --version)"

# --- wails3 ---
if (-not (Get-Command wails3 -ErrorAction SilentlyContinue)) {
    Info "wails3 not found - installing via go install..."
    go install github.com/wailsapp/wails/v3/cmd/wails3@latest
    if ($LASTEXITCODE -ne 0) { Fail "go install wails3 failed" }

    # Refresh PATH so the newly installed binary is visible in this session
    $goBin = (go env GOPATH) + '\bin'
    if ($env:PATH -notlike "*$goBin*") {
        $env:PATH = $goBin + ';' + $env:PATH
    }
    if (-not (Get-Command wails3 -ErrorAction SilentlyContinue)) {
        $goBin2 = go env GOPATH
        Fail "wails3 installed but not in PATH. Add '$goBin2\bin' to your PATH and re-run."
    }
}
Info "wails3 ready"

# --- Generate Wails JS/TS bindings ---
Info "Generating Wails bindings..."
wails3 generate bindings
if ($LASTEXITCODE -ne 0) { Fail "wails3 generate bindings failed" }

# --- Frontend ---
Push-Location frontend
try {
    Info "Installing frontend dependencies..."
    npm install
    if ($LASTEXITCODE -ne 0) { Fail "npm install failed" }

    Info "Building frontend..."
    npm run build
    if ($LASTEXITCODE -ne 0) { Fail "npm run build failed" }
} finally {
    Pop-Location
}

# --- Go binary ---
Info "Building pitcher..."
go build -ldflags "-s -w" -o pitcher.exe .
if ($LASTEXITCODE -ne 0) { Fail "go build failed" }

Info "Build complete - binary: .\pitcher.exe"
Write-Host "Run 'pitcher.exe --help' for usage, or 'pitcher.exe --gui' for the graphical interface." -ForegroundColor Green
