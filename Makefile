.PHONY: all lint test test-integration ui build docker package-offline verify-offline

VERSION := $(shell tr -d '[:space:]' < VERSION)
COMMIT := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/hkjang/mmcp/internal/buildinfo.Version=v$(VERSION) -X github.com/hkjang/mmcp/internal/buildinfo.Commit=$(COMMIT) -X github.com/hkjang/mmcp/internal/buildinfo.BuildTime=$(BUILD_TIME)

all: lint test build

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	./scripts/verify-version.sh

test:
	go test ./...
	cd web && node node_modules/vitest/vitest.mjs run

# Needs PostgreSQL: TEST_POSTGRES_DSN=postgres://... make test-integration
test-integration:
	go test -race -count=1 ./...

ui:
	./scripts/build-ui.sh

build: ui
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/mmcp ./cmd/mmcp

docker:
	docker build --build-arg VERSION=v$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t mmcp:v$(VERSION) .

package-offline:
	./scripts/package-offline.sh

verify-offline:
	./scripts/verify-offline.sh
