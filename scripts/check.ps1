$ErrorActionPreference = "Stop"

Write-Host "==> gofmt"
$changed = gofmt -l .
if ($changed) {
    Write-Host "The following files need gofmt:"
    Write-Host $changed
    exit 1
}

Write-Host "==> go test"
go test -count=1 ./...

Write-Host "==> go vet"
go vet ./...

Write-Host "==> go build"
go build ./...

Write-Host "==> CLI smoke"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("flue4go-" + [guid]::NewGuid().ToString("N"))
go run ./cmd/fluego init --workspace $tmp
go run ./cmd/fluego inspect --workspace $tmp

Write-Host "All checks passed."
