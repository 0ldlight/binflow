# BinFlow — engineering entrypoint (T-7).
# The Makefile is the single door into every build/test/lint command, for both
# humans and CI (.github/workflows/ci.yml invokes these same targets).

BINARY      := bin/binflow-server
GO          ?= go
# The Go package list excludes web/: it is the console's own build domain
# (npm toolchain, eslint/tsc gates) and node_modules vendors third-party Go
# files (e.g. flatted's Go port) that must never enter the Go gate —
# `make console` recreates them on every machine including CI. Defined after
# GO so the shell expansion sees the toolchain variable.
PKG         := $(shell $(GO) list ./... | grep -v '/binflow/web/')
GOPROXY     ?= https://goproxy.cn,direct
GOTOOLCHAIN ?= local
# Version-pinned lint toolchain (CI installs the same one, see ci.yml).
GOLANGCI_VERSION := v2.12.2
# Prefer a golangci-lint on PATH, else the one `make lint` installs into GOPATH/bin.
GOLANGCI := $(or $(shell command -v golangci-lint 2>/dev/null),$(shell $(GO) env GOPATH 2>/dev/null)/bin/golangci-lint)

export GOPROXY
export GOTOOLCHAIN

.PHONY: all build test lint fmt vet tidy run dev clean tools check-size docs docs-size console console-size \
	goreleaser-check release-snapshot release release-verify help

all: build

## build: compile bin/binflow-server with CGO disabled (ADR-0005 zero-CGo baseline).
## Node-free by design: the console rides the committed placeholder shell until
## `make console` has run in this checkout (T-89 placeholder strategy).
build:
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BINARY) ./cmd/binflow-server
	@$(MAKE) --no-print-directory check-size

## console: build the web console (npm ci — lockfile-pinned, ADR-0014
## decision 4) and copy it into internal/console/dist for go:embed. Requires
## node >= 20; reports the gzip'd SPA payload against the 5MB budget (W37).
## Rebuild the binary afterwards to embed the fresh bundle.
console:
	cd web && npm ci && npm run build
	rm -rf internal/console/dist/assets
	cp -R web/dist/. internal/console/dist/
	@$(MAKE) --no-print-directory console-size

## console-size: report the embedded SPA's gzipped js+css payload (the wire
## cost a cold browser pays; PRD W37 target <= 5MB).
console-size:
	@total=0; for f in internal/console/dist/assets/*.js internal/console/dist/assets/*.css; do \
		[ -e "$$f" ] || continue; \
		sz=$$(gzip -c "$$f" | wc -c); total=$$((total+sz)); \
	done; \
	echo "console SPA payload (gzip, js+css): $$total bytes"; \
	if [ "$$total" -gt 5242880 ]; then \
		echo "WARNING: SPA payload exceeds the 5MB budget (PRD W37)"; \
	fi
## docs: build the help documentation site (Docusaurus 3, npm ci —
## lockfile-pinned) and copy it into internal/docs/dist for go:embed.
## Requires node >= 20 (engines gate in package.json). The site is built
## from docs/user/*.md (ADR-0011: tech-writers never touch docs-site/).
## Rebuild the binary afterwards to embed the fresh build.
docs:
	cd docs-site && npm ci && npm run build
	rm -rf internal/docs/dist/assets
	rm -rf internal/docs/dist/search
	cp -R docs-site/build/. internal/docs/dist/
	@$(MAKE) --no-print-directory docs-size

## docs-size: report the embedded docs site size (raw total, not gzip — the
## site is served from the same disk, not over the wire; PRD T-129 budget
## <= 15MB). WARNs at 15MB; the deploy gate (T-130 K1 fallback) requires
## user confirmation above that threshold.
docs-size:
	@bytes=0; for f in $$(find internal/docs/dist -type f); do \
		[ -e "$$f" ] || continue; \
		sz=$$(wc -c < "$$f" | tr -d ' '); bytes=$$((bytes+sz)); \
	done; \
	mb=$$(awk -v b="$$bytes" 'BEGIN { printf "%.2f", b/1048576 }'); \
	echo "docs site size (raw total): $$mb MB ($$bytes bytes)"; \
	if [ "$$bytes" -gt 15728640 ]; then \
		echo "WARNING: docs site exceeds the 15MB budget — user confirmation required (T-130 K1 fallback)"; \
	fi


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
	$(GO) vet $(PKG)

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

## tools: print toolchain versions (Go, golangci-lint, goreleaser) for reproducibility.
tools:
	@echo "go:              $$($(GO) version)"
	@echo "golangci-lint:   $$("$(GOLANGCI)" --version 2>/dev/null || echo 'not installed (run: make lint)')"
	@echo "goreleaser:      $$("$(GORELEASER)" --version 2>/dev/null | grep -m1 GitVersion || echo 'not installed (run: make release-snapshot)')"

## check-size: product budget gate (PRODUCT <40MB), two faces (T-127, G03):
## the raw bin/binflow-server (WARN only, unchanged since T-7) and every
## compressed dist/ release artifact (exit 1 when any platform archive is
## over budget; CHECK_SIZE_WARN=1 downgrades that face to a warning). No
## dist/ artifacts — the common `make build` case — leaves just the report.
check-size:
	@size=$$(wc -c < $(BINARY) | tr -d ' '); \
	mb=$$(awk -v b="$$size" 'BEGIN { printf "%.2f", b/1048576 }'); \
	echo "bin size: $$mb MB ($(BINARY))"; \
	if [ "$$size" -gt 41943040 ]; then \
		echo "WARNING: binary exceeds the 40MB product budget"; \
	fi; \
	over=0; \
	for f in dist/binflow_*.tar.gz dist/binflow_*.zip; do \
		[ -e "$$f" ] || continue; \
		sz=$$(wc -c < "$$f" | tr -d ' '); \
		mbz=$$(awk -v b="$$sz" 'BEGIN { printf "%.2f", b/1048576 }'); \
		echo "archive size: $$mbz MB ($$f)"; \
		if [ "$$sz" -gt 41943040 ]; then \
			over=1; \
			if [ "$(CHECK_SIZE_WARN)" = "1" ]; then \
				echo "WARNING: $$f exceeds the 40MB product budget"; \
			else \
				echo "ERROR: $$f exceeds the 40MB product budget (CHECK_SIZE_WARN=1 downgrades to a warning)"; \
			fi; \
		fi; \
	done; \
	[ "$$over" -eq 0 ] || [ "$(CHECK_SIZE_WARN)" = "1" ] || exit 1

# ---- release face (T-127, FR-34 / PB-01 / PB-02) --------------------------
# Q4 baseline: VER defaults to the GA version. The conductor cuts the real
# `v1.0.0` git tag at M5 close (DoD 7); until then the Makefile owns the
# version and hands it to goreleaser via BINFLOW_VER (see the snapshot
# template in .goreleaser.yaml). Bare `make build` stays dev-stamped — only
# the release faces inject. goreleaser is a build-side tool and never enters
# go.mod (NFR-S31, ADR-0005); pinned here and in .tool-versions, installed
# on demand exactly like golangci-lint.
VER ?= v1.0.0
GORELEASER_VERSION := v2.17.1
GORELEASER := $(or $(shell command -v goreleaser 2>/dev/null),$(shell $(GO) env GOPATH 2>/dev/null)/bin/goreleaser)
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null)

define ensure-goreleaser
@test -x "$(GORELEASER)" || { \
	echo "goreleaser not found; installing $(GORELEASER_VERSION) into $$($(GO) env GOPATH)/bin (pinned in .tool-versions)."; \
	GOBIN=$$($(GO) env GOPATH)/bin $(GO) install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION); \
}
endef

## goreleaser-check: validate .goreleaser.yaml against the pinned schema.
goreleaser-check:
	$(ensure-goreleaser)
	"$(GORELEASER)" check

## release-snapshot: six-platform snapshot build (linux/darwin/windows x
## amd64/arm64, zero CGo) with checksums — the CI/verification face. Version
## is explicitly snapshot-marked so these bits are never confusable with
## release bits. Embeds whatever console assets are staged in
## internal/console/dist (run `make console` first for the real SPA — T-89
## placeholder strategy; add `make docs` once T-129 lands).
release-snapshot:
	$(ensure-goreleaser)
	BINFLOW_VER="$(VER)-snapshot.$(GIT_SHA)" "$(GORELEASER)" release --snapshot --clean
	@$(MAKE) --no-print-directory release-verify RELVER="$(VER)-snapshot.$(GIT_SHA)"

## release: final-form LOCAL archive (dist/ + checksums named
## binflow_$(VER)_*, no snapshot marker) — the pre-release rehearsal face
## (PRD FR-34: local archiving is the pre-publish verification step).
## Publishing to GitHub Releases is NOT here: Q1/DoD 7 keep the push as a
## conductor + user-confirmed action with user-injected credentials.
release:
	$(ensure-goreleaser)
	BINFLOW_VER="$(VER)" "$(GORELEASER)" release --snapshot --clean
	@$(MAKE) --no-print-directory release-verify RELVER="$(VER)"

## release-verify: post-build gate (QA G01/G03 consume) — call with RELVER.
## Asserts all six platform archives plus the checksums file exist, verifies
## every sha256 (`shasum -a 256 -c`), then runs the check-size budget gate.
release-verify:
	@set -e; \
	n=$$(ls dist/binflow_$(RELVER)_linux_amd64.tar.gz \
		dist/binflow_$(RELVER)_linux_arm64.tar.gz \
		dist/binflow_$(RELVER)_darwin_amd64.tar.gz \
		dist/binflow_$(RELVER)_darwin_arm64.tar.gz \
		dist/binflow_$(RELVER)_windows_amd64.zip \
		dist/binflow_$(RELVER)_windows_arm64.zip 2>/dev/null | wc -l | tr -d ' '); \
	[ "$$n" -eq 6 ] || { echo "release-verify: expected 6 platform archives for $(RELVER), found $$n"; ls -l dist; exit 1; }; \
	test -f dist/binflow_$(RELVER)_checksums.txt || { echo "release-verify: checksums file missing for $(RELVER)"; exit 1; }; \
	echo "release-verify: 6/6 platform archives + checksums present for $(RELVER)"; \
	(cd dist && shasum -a 256 -c binflow_$(RELVER)_checksums.txt); \
	$(MAKE) --no-print-directory check-size

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | sed 's/:/ ::/'
