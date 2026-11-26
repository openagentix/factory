.PHONY: test lint build

test:
	go test ./... -race

lint:
	go vet ./...

build:
	go build -o bin/oxf ./cmd/oxf
