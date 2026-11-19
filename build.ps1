$ErrorActionPreference = "Stop"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"

if (-not (Test-Path "rsrc_windows_amd64.syso")) {
    $compressed = [Convert]::FromBase64String((Get-Content "assets/rsrc_windows_amd64.syso.gz.b64" -Raw).Trim())
    $source = [IO.MemoryStream]::new($compressed)
    $gzip = [IO.Compression.GZipStream]::new($source, [IO.Compression.CompressionMode]::Decompress)
    $target = [IO.File]::Create("rsrc_windows_amd64.syso")
    try { $gzip.CopyTo($target) } finally { $target.Dispose(); $gzip.Dispose(); $source.Dispose() }
}

go test ./...
go build -buildvcs=false -trimpath -ldflags="-s -w -H=windowsgui" -o Win11Customizer.exe .

Write-Host "Готово: Win11Customizer.exe" -ForegroundColor Green
