$ErrorActionPreference = 'Stop'
$env:CGO_ENABLED = '1'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
go build -o dist/s3async-windows-amd64.exe ./...
