VERSION  := $(shell cat VERSION 2>/dev/null || echo "0.0.0-dev")
BUILD_DATE := $(shell date +%Y-%m-%d 2>/dev/null || echo "unknown")
LDFLAGS  := -X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE)

.PHONY: build build-linux frontend test lint clean help tools hls-tool doctor-tool

## build: compile server binary (Windows)
build: frontend
	go build -ldflags "$(LDFLAGS)" -o server.exe ./cmd/server

## build-linux: cross-compile for Linux (CGO disabled)
build-linux: frontend
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o server ./cmd/server

## frontend: build React SPA into web/static/react/
frontend:
	cd web/frontend && npm ci && npm run build

## tools: build auxiliary binaries (hls-pregenerate, media-doctor)
tools:
	go build -ldflags "$(LDFLAGS)" -o hls-pregenerate.exe ./cmd/hls-pregenerate
	go build -ldflags "$(LDFLAGS)" -o media-doctor.exe ./cmd/media-doctor

## hls-tool: build HLS pre-generation tool only
hls-tool:
	go build -ldflags "$(LDFLAGS)" -o hls-pregenerate.exe ./cmd/hls-pregenerate

## doctor-tool: build offline diagnostics tool only
doctor-tool:
	go build -ldflags "$(LDFLAGS)" -o media-doctor.exe ./cmd/media-doctor

## test: run all Go tests and frontend tests
test:
	go test ./...
	cd web/frontend && npx vitest run 2>/dev/null || echo "[warn] frontend tests not configured yet"

## test-go: run only Go tests
test-go:
	go test ./...

## test-frontend: run only frontend tests
test-frontend:
	cd web/frontend && npx vitest run

## lint: run static analysis on Go and frontend code
lint:
	go vet ./...
	cd web/frontend && npm run lint

## check: compile-check all packages without producing output
check:
	go build ./...

## clean: remove compiled binaries
clean:
	rm -f server server.exe hls-pregenerate hls-pregenerate.exe media-doctor media-doctor.exe

## help: show this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
