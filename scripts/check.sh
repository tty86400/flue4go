#!/usr/bin/env sh
set -eu

echo "==> gofmt"
changed="$(gofmt -l .)"
if [ -n "$changed" ]; then
  echo "The following files need gofmt:"
  echo "$changed"
  exit 1
fi

echo "==> go test"
go test -count=1 ./...

echo "==> go vet"
go vet ./...

echo "==> go build"
go build ./...

echo "==> CLI smoke"
tmp="$(mktemp -d)"
go run ./cmd/fluego init --workspace "$tmp"
go run ./cmd/fluego inspect --workspace "$tmp"

echo "All checks passed."
