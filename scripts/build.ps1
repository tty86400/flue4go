$ErrorActionPreference = "Stop"

$OutDir = if ($args.Count -gt 0) { $args[0] } else { "dist" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; Ext = ".exe" },
    @{ GOOS = "linux"; GOARCH = "amd64"; Ext = "" },
    @{ GOOS = "linux"; GOARCH = "arm64"; Ext = "" },
    @{ GOOS = "darwin"; GOARCH = "amd64"; Ext = "" },
    @{ GOOS = "darwin"; GOARCH = "arm64"; Ext = "" }
)

foreach ($target in $targets) {
    $env:GOOS = $target.GOOS
    $env:GOARCH = $target.GOARCH
    $env:CGO_ENABLED = "0"
    $output = Join-Path $OutDir ("fluego-" + $target.GOOS + "-" + $target.GOARCH + $target.Ext)
    Write-Host "==> $output"
    go build -trimpath -ldflags="-s -w" -o $output ./cmd/fluego
}

Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
