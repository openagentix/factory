.PHONY: test lint build nocgo

test:
	go test ./... -race

lint:
	go vet ./...

build:
	go build -o bin/oxf ./cmd/oxf

# The store uses a pure Go SQLite driver. This proves no C toolchain is needed.
nocgo:
	CGO_ENABLED=0 go build ./...
	CGO_ENABLED=0 go test ./internal/store/... -count=1
