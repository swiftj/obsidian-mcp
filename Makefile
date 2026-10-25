SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Private build cache, in case other sessions clean the shared one.
export GOCACHE ?= $(CURDIR)/.gocache

PKG     := ./...
CMD     := ./cmd/obs-mcp
DIST    := dist
TARGETS := darwin/arm64 linux/amd64 linux/arm64
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: lint test test-race test-obsidian build verify-static mcpb plugin clean

lint:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	CGO_ENABLED=0 go vet $(PKG)
	CGO_ENABLED=0 staticcheck $(PKG)
	CGO_ENABLED=0 golangci-lint run $(PKG)
	CGO_ENABLED=0 govulncheck $(PKG)

test:
	CGO_ENABLED=0 go test $(PKG)

# Race detector needs cgo; this build is never shipped.
test-race:
	CGO_ENABLED=1 go test -race $(PKG)

# Opt-in probes against a real Obsidian install; tests skip when the CLI is missing.
test-obsidian:
	CGO_ENABLED=0 go test -tags obsidian $(PKG)

build:
	@mkdir -p $(DIST)
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		echo "build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o $(DIST)/obs-mcp-$$os-$$arch $(CMD); \
	done

verify-static: build
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; bin=$(DIST)/obs-mcp-$$os-$$arch; \
		go version -m $$bin | grep -q 'CGO_ENABLED=0' || { echo "$$bin: CGO_ENABLED is not 0"; exit 1; }; \
		if [ $$os = linux ]; then \
			file $$bin | grep -q 'statically linked' || { echo "$$bin: not statically linked"; exit 1; }; \
		fi; \
		echo "ok $$bin"; \
	done

mcpb plugin:
	@echo "$@: not implemented until Phase 7"; exit 1

clean:
	rm -rf $(DIST)
