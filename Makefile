BINARY := drun
PKG    := github.com/flowm/drun/cmd/drun
BIN    := bin/$(BINARY)
PREFIX ?= $(HOME)/.local

GO         ?= go
GOFLAGS    ?=
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf none)
DATE       ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || printf unknown)
LDFLAGS    ?= -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: all build install uninstall run fmt vet tidy test clean help

all: build

## build: Compile the drun binary to ./bin/drun
build:
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

## install: Install drun to $(PREFIX)/bin (default ~/.local/bin)
install: build
	@mkdir -p $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BINARY)

## uninstall: Remove drun from $(PREFIX)/bin
uninstall:
	rm -f $(PREFIX)/bin/$(BINARY)

## run: Build and run (pass args with ARGS="...")
run: build
	./$(BIN) $(ARGS)

## fmt: Format all Go sources
fmt:
	$(GO) fmt ./...

## vet: Run go vet
vet:
	$(GO) vet ./...

## tidy: Ensure go.mod/go.sum are tidy
tidy:
	$(GO) mod tidy

## test: Run tests
test:
	$(GO) test ./...

## check: fmt + vet + test
check: fmt vet test

## clean: Remove build artifacts
clean:
	rm -rf bin

## help: Show this help
help:
	@awk 'BEGIN {FS = ":.*## "} /^## / {sub(/^## /, "", $$0); print $$0; next} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
