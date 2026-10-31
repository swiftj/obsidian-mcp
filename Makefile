SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

# The version comes from the Desktop extension manifest, so the binary, the
# extension and the plugin always agree.
MANIFEST := plugin/mcpb/manifest.json
VERSION  ?= $(shell sed -n 's/^  "version": "\(.*\)",$$/\1/p' $(MANIFEST))
MCPB     := npx -y @anthropic-ai/mcpb@2.1.2
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

verify-static: build mcpb
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; bin=$(DIST)/obs-mcp-$$os-$$arch; \
		go version -m $$bin | grep -q 'CGO_ENABLED=0' || { echo "$$bin: CGO_ENABLED is not 0"; exit 1; }; \
		if [ $$os = linux ]; then \
			file $$bin | grep -q 'statically linked' || { echo "$$bin: not statically linked"; exit 1; }; \
		fi; \
		echo "ok $$bin"; \
	done
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
		unzip -q $(DIST)/obs-mcp-darwin-arm64.mcpb -d "$$tmp"; bin="$$tmp/server/obs-mcp"; \
		go version -m "$$bin" | grep -q 'CGO_ENABLED=0' || { echo "mcpb: CGO_ENABLED is not 0"; exit 1; }; \
		file "$$bin" | grep -q 'Mach-O 64-bit executable arm64' || { echo "mcpb: not a darwin/arm64 binary"; exit 1; }; \
		cmp -s "$$bin" $(DIST)/obs-mcp-darwin-arm64 || { echo "mcpb: binary differs from the build"; exit 1; }; \
		[ -x "$$bin" ] || { echo "mcpb: binary is not executable"; exit 1; }; \
		echo "ok $(DIST)/obs-mcp-darwin-arm64.mcpb"

# The Desktop extension: macOS on Apple silicon only (X-PLUG-09).
mcpb: build
	@rm -rf $(DIST)/mcpb && mkdir -p $(DIST)/mcpb/server
	cp $(MANIFEST) $(DIST)/mcpb/manifest.json
	cp $(DIST)/obs-mcp-darwin-arm64 $(DIST)/mcpb/server/obs-mcp
	$(MCPB) validate $(DIST)/mcpb/manifest.json
	$(MCPB) pack $(DIST)/mcpb $(DIST)/obs-mcp-darwin-arm64.mcpb
	$(MCPB) info $(DIST)/obs-mcp-darwin-arm64.mcpb

# The Cowork / Claude Code plugin: skills only, no server and no bin/.
plugin:
	@rm -f $(DIST)/obsidian-vaults-plugin.zip && mkdir -p $(DIST)
	cd plugin/cowork && zip -qrX ../../$(DIST)/obsidian-vaults-plugin.zip . -x '.DS_Store' '*/.DS_Store'
	@echo "ok $(DIST)/obsidian-vaults-plugin.zip"

clean:
	rm -rf $(DIST)
