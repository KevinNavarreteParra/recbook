# Everything CI would run. There is no GitHub workflow: run `make check` before pushing.

.PHONY: build test check fmt

build:
	go build -o bin/recbook ./cmd/recbook

test:
	go test ./...

fmt:
	gofmt -w .

check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	go vet ./...
	go test ./...
