# VMM Makefile for local development builds
# Injects version information via ldflags

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build build-web clean install test

# Build the vmm binary with version info
build:
	go build -ldflags "$(LDFLAGS)" -o vmm ./cmd/vmm/

# Build the vmm-web binary with version info
build-web:
	go build -ldflags "$(LDFLAGS)" -o vmm-web ./cmd/vmm-web/

# Build all binaries
build-all: build build-web

# Build without version info (faster, for quick iteration)
build-quick:
	go build -o vmm ./cmd/vmm/
	go build -o vmm-web ./cmd/vmm-web/

# Install to /usr/local/bin (requires sudo). Copies to a temp name and
# renames so a running vmm-web keeps its old inode instead of failing
# with "Text file busy" (same approach as scripts/install.sh).
install: build build-web
	sudo cp vmm /usr/local/bin/vmm.new
	sudo chmod 755 /usr/local/bin/vmm.new
	sudo mv -f /usr/local/bin/vmm.new /usr/local/bin/vmm
	sudo cp vmm-web /usr/local/bin/vmm-web.new
	sudo chmod 755 /usr/local/bin/vmm-web.new
	sudo mv -f /usr/local/bin/vmm-web.new /usr/local/bin/vmm-web

# Clean build artifacts
clean:
	rm -f vmm vmm-web

# Run tests
test:
	go test -v ./...

# Show version info that will be embedded
version-info:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Date:    $(DATE)"
