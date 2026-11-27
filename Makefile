.PHONY: test lint build nocgo web build-web

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

# The web app (record 0004). Needs Node 22 and pnpm. Installs from the lockfile with
# install scripts off, checks, tests and builds into web/dist-app, then tests the built files.
web:
	cd web && pnpm install --frozen-lockfile --ignore-scripts
	cd web && pnpm check
	cd web && pnpm test
	cd web && pnpm build
	go test -tags webapp ./web/... -count=1

# bin/oxf with the web app inside. Run make web first.
build-web:
	go build -tags webapp -o bin/oxf ./cmd/oxf
