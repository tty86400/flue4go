.PHONY: fmt check test vet build cli-smoke release-build

fmt:
	gofmt -w .

check:
	./scripts/check.sh

test:
	go test -count=1 ./...

vet:
	go vet ./...

build:
	go build ./...

cli-smoke:
	tmp="$$(mktemp -d)" && go run ./cmd/fluego init --workspace "$$tmp" && go run ./cmd/fluego inspect --workspace "$$tmp"

release-build:
	./scripts/build.sh
