<#
Build script for Windows (PowerShell).

Usage:
  .\build-windows.ps1             # build s3async.exe (amd64)
  .\build-windows.ps1 -Out my.exe -Release

Requirements:
  - Go installed and in PATH
  - No C toolchain required (pure-Go SQLite, CGO disabled)
#>
param(
    [string]$Out = "s3async",
    [switch]$Release
)

Write-Host "Building for Windows (GOOS=windows GOARCH=amd64)..."

$DistDir = "dist"
if (-not (Test-Path $DistDir)) {
    New-Item -ItemType Directory -Path $DistDir | Out-Null
    Write-Host "Created directory: $DistDir"
}

$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

$exeName = if ($Out.ToLower().EndsWith('.exe')) { $Out } else { "$Out.exe" }
$exePath = Join-Path $DistDir $exeName

Write-Host "Building executable to: $exePath"
go build -ldflags "-s -w" -o $exePath .
if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
Write-Host "Built: $exePath"

if ($Release) {
    $zipName = "${Out}-windows-amd64.zip"
    $zipPath = Join-Path $DistDir $zipName
    Write-Host "Creating release zip: $zipPath"
    Compress-Archive -Path $exePath -DestinationPath $zipPath -Force
    Write-Host "Release package: $zipPath"
}
