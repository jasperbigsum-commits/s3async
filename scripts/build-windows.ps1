<#
Build script for Windows (PowerShell).

Usage:
  .\build-windows.ps1             # build s3async.exe (amd64)
  .\build-windows.ps1 -Out my.exe -Release

Requirements:
  - Go installed and in PATH
  - If using sqlite3 (CGO), have a C toolchain (MSYS2/mingw-w64) available
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

$env:CGO_ENABLED = "1"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

if (-not $env:CC) {
    $compilerCandidates = @(
        'gcc',
        'x86_64-w64-mingw32-gcc',
        'x86_64-pc-windows-gnu-gcc',
        'clang'
    )
    foreach ($candidate in $compilerCandidates) {
        if (Get-Command $candidate -ErrorAction SilentlyContinue) {
            $env:CC = $candidate
            Write-Host "Using C compiler: $candidate"
            break
        }
    }
    if (-not $env:CC) {
        throw "CGO requires a C compiler on PATH. Install MSYS2/mingw-w64 and add its mingw64\bin to PATH, for example: pacman -S mingw-w64-x86_64-gcc"
    }
} else {
    Write-Host "Using configured C compiler: $env:CC"
}

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
