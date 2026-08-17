# BinFlow — engineering entrypoint (T-7).
# The Makefile is the single door into every build/test/lint command, for both
# humans and CI (.github/workflows/ci.yml invokes these same targets).

BINARY      := bin/binflow-server
PKG         := ./...
GO          ?= go
GOPROXY     ?= https://goproxy.cn,direct
GOTOOLCHAIN ?= local
# Version-pinned lint toolchain (CI installs the same one, see ci.yml).
GOLANGCI_VERSION := v2.12.2
# Prefer a golangci-lint on PATH, else the one `make lint` installs into GOPATH/bin.
GOLANGCI := $(or $(shell command -v golangci-lint 2>/dev/null),$(shell $(GO) env GOPATH 2>/dev/null)/bin/golangci-lint)

export GOPROXY
export GOTOOLCHAIN

.PHONY: all build test lint fmt vet tidy run dev clean tools check-size help

all: build

## build: compile bin/binflow-server with CGO disabled (ADR-0005 zero-CGo baseline).
build:
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BINARY) ./cmd/binflow-server
	@$(MAKE) --no-print-directory check-size

## test: run all tests with the race detector (default test target).
test:
	CGO_ENABLED=1 $(GO) test -race -count=1 $(PKG)

## test-cov: run tests with a coverage profile (not part of `all`).
test-cov:
	CGO_ENABLED=1 $(GO) test -race -count=1 -coverprofile=coverage.out -covermode=atomic $(PKG)
	@$(GO) tool cover -func=coverage.out | tail -1

## lint: golangci-lint over the whole module (config in .golangci.yml).
lint:
	@test -x "$(GOLANGCI)" || { \
		echo "golangci-lint not found; installing $(GOLANGCI_VERSION) into $$($(GO) env GOPATH)/bin (pinned in .tool-versions)."; \
		GOBIN=$$($(GO) env GOPATH)/bin $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION); \
	}
	"$(GOLANGCI)" run

## fmt: gofmt the whole tree.
fmt:
	$(GO) fmt ./...

## vet: go vet, kept as a separate escape hatch from golangci-lint.
vet:
	$(GO) vet ./...

## tidy: sync go.mod/go.sum; run alongside `git diff --stat go.mod go.sum` to verify no drift.
tidy:
	$(GO) mod tidy

## run: build then start the server locally (serve wiring lands in T-16).
run: build
	./$(BINARY) serve

## dev: local development loop — vet+lint+test+build, the pre-push sanity gate.
dev: vet lint test build

## clean: remove build artifacts and coverage output.
clean:
	rm -rf bin coverage.out coverage.html

## tools: print toolchain versions (Go, golangci-lint) for reproducibility.
tools:
	@echo "go:              $$($(GO) version)"
	@echo "golangci-lint:   $$("$(GOLANGCI)" --version 2>/dev/null || echo 'not installed (run: make lint)')"

## check-size: print the binary size, warn (non-blocking) above 40MB (PRODUCT budget).
check-size:
	@size=$$(wc -c < $(BINARY) | tr -d ' '); \
	mb=$$(awk -v b="$$size" 'BEGIN { printf "%.2f", b/1048576 }'); \
	echo "bin size: $$mb MB ($(BINARY))"; \
	if [ "$$size" -gt 41943040 ]; then \
		echo "WARNING: binary exceeds the 40MB product budget"; \
	fi

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | sed 's/:/ ::/'
