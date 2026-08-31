BINARY  := rtz
PKG     := github.com/runtimez-com/runtimez-cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

.PHONY: build test lint fmt vet tidy install clean snapshot

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/rtz

install:
	CGO_ENABLED=0 go install -trimpath -ldflags '$(LDFLAGS)' ./cmd/rtz

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	golangci-lint run

tidy:
	go mod tidy

# Builds every release target locally without publishing — the same check CI runs on a PR.
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist
