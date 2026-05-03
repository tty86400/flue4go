#!/usr/bin/env sh
set -eu

out_dir="${1:-dist}"
mkdir -p "$out_dir"

build_one() {
  goos="$1"
  goarch="$2"
  ext="$3"
  output="$out_dir/fluego-$goos-$goarch$ext"
  echo "==> $output"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "$output" ./cmd/fluego
}

build_one windows amd64 .exe
build_one linux amd64 ""
build_one linux arm64 ""
build_one darwin amd64 ""
build_one darwin arm64 ""
