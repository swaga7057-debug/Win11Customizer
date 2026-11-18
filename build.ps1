$ErrorActionPreference = "Stop"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

go test ./...
go build -buildvcs=false -trimpath -ldflags="-s -w -H=windowsgui" -o Win11Customizer.exe .

Write-Host "Готово: Win11Customizer.exe" -ForegroundColor Green
