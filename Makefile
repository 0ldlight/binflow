# BinFlow — engineering entrypoint (T-7).
# The Makefile is the single door into every build/test/lint command, for both
# humans and CI (.github/workflows/ci.yml invokes these same targets).

BINARY          := bin/binflow-server
BF              := bin/bf
BF_MIGRATE      := bin/bf-migrate
BINARIES        := $(BINARY) $(BF) $(BF_MIGRATE)
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
	check-deps goreleaser-check release-snapshot release release-verify \
	test-m7-resume test-m7-resume-sigterm test-m7-rbac-matrix lint-baseline help

all: build

## build: compile bin/binflow-server + bin/bf + bin/bf-migrate with CGO disabled
## (ADR-0005 zero-CGo baseline). Node-free by design: the console rides the
## committed placeholder shell until `make console` has run in this checkout
## (T-89 placeholder strategy).
build:
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BINARY) ./cmd/binflow-server
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BF) ./cmd/bf
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BF_MIGRATE) ./cmd/bf-migrate
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

# ---- M7 acceptance scaffolding (T-211) ---------------------------------------
# RED-LIGHT-FIRST: the resume probe encodes PRD FR-67 (V15/V16 shape) and on
# pre-T-216 main FAILS by design (exit 1, 404 BLOB_UPLOAD_UNKNOWN after the
# restart — the in-memory upload-session registry). It goes GREEN when T-216
# lands; the archived baseline lives in reports/agents/T-211.md. Deliberately
# NOT wired into CI until then (ci.yml stays green on main).

## test-m7-resume: FR-67 resume probe, kill -9 arm (RED on pre-T-216 main).
test-m7-resume:
	scripts/m7-resume-probe.sh --stop kill

## test-m7-resume-sigterm: FR-67 resume probe, graceful-restart arm (the
## `docker compose restart` signal 口径; RED until T-216 + ADR-0028 Close
## semantics).
test-m7-resume-sigterm:
	scripts/m7-resume-probe.sh --stop sigterm

## test-m7-rbac-matrix: FR-64 role x endpoint status matrix on a throwaway
## instance (roles admin/user run; read-only-admin SKIPs until T-215 wires
## the role field). Observational by default — pass EXPECT=1 for the PRD M7
## target-table verdict (exit 1 on deviations; flip on after T-215).
test-m7-rbac-matrix:
	scripts/m7-rbac-matrix.sh $(if $(EXPECT),--expect)

## lint-baseline: per-package golangci-lint issue counts — the archived
## before-picture for FR-70/V31 (internal/auth is the tracked debt line).
## Archive, not gate: exit 0 with issues; `make lint` stays the gate.
lint-baseline:
	scripts/lint-baseline.sh

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

## check-deps: verify zero CGo baseline (ADR-0005) — list all CGo-referencing
## deps and fail if any found. Run after every `go get` or go.mod edit.
check-deps:
	@cgo_deps=$$(CGO_ENABLED=0 $(GO) list -deps -f '{{if .CgoFiles}}{{.ImportPath}}{{end}}' ./...); \
	if [ -n "$$cgo_deps" ]; then \
		echo "ERROR: CGo dependencies found (ADR-0005 violation):"; \
		echo "$$cgo_deps"; \
		exit 1; \
	fi; \
	echo "check-deps: zero CGo baseline — OK (ADR-0005)"

## check-size: product budget gate (T-148), three faces:
## 1. Raw binaries: bin/bf (<15MB T-148 AC2), bin/bf-migrate (<15MB),
##    bin/binflow-server (<40MB PRODUCT, unchanged since T-7).
##    WARN only — `make build` never fails on size.
## 2. Every compressed dist/ release archive (exit 1 when any platform
##    archive is over 40MB; CHECK_SIZE_WARN=1 downgrades to a warning).
## 3. No dist/ artifacts — the common `make build` case — leaves just the
##    per-binary report.
check-size:
	@echo "--- raw binaries ---"; \
	for pair in "bin/bf:15728640" "bin/bf-migrate:15728640" "$(BINARY):41943040"; do \
		f=$${pair%%:*}; \
		limit=$${pair#*:}; \
		[ -f "$$f" ] || continue; \
		sz=$$(wc -c < "$$f" | tr -d ' '); \
		mb=$$(awk -v b="$$sz" 'BEGIN { printf "%.2f", b/1048576 }'); \
		limit_mb=$$(awk -v b="$$limit" 'BEGIN { printf "%.0f", b/1048576 }'); \
		echo "  $$f: $$mb MB (budget $$limit_mb MB)"; \
		if [ "$$sz" -gt $$limit ]; then \
			echo "  WARNING: $$f exceeds the $$limit_mb MB budget"; \
		fi; \
	done; \
	echo "--- dist archives ---"; \
	over=0; \
	for f in dist/binflow_*.tar.gz dist/binflow_*.zip; do \
		[ -e "$$f" ] || continue; \
		sz=$$(wc -c < "$$f" | tr -d ' '); \
		mbz=$$(awk -v b="$$sz" 'BEGIN { printf "%.2f", b/1048576 }'); \
		echo "  $$mbz MB ($$f)"; \
		if [ "$$sz" -gt 41943040 ]; then \
			over=1; \
			if [ "$(CHECK_SIZE_WARN)" = "1" ]; then \
				echo "  WARNING: $$f exceeds the 40MB product budget"; \
			else \
				echo "  ERROR: $$f exceeds the 40MB product budget (CHECK_SIZE_WARN=1 downgrades to a warning)"; \
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

# ---- offline bundle (T-139, FR-40 / PB-08) ----------------------------------
# Builds the offline installation bundle: binflow_offline_<VER>.tar.gz in
# deploy/offline/.  Pre-requisites:
#   - make release (or release-snapshot) — dist/ has the platform binaries
#   - Docker images tagged binflow:<VER>-alpine and binflow:<VER>-distroless
#     (run deploy/release/build-release.sh)
#   - helm chart packaged at charts/binflow/binflow-<VER>.tgz
#     (run `helm package charts/binflow` from repo root)
OFFLINE_DIR := deploy/offline
OFFLINE_BUNDLE := $(OFFLINE_DIR)/binflow_offline_$(VER).tar.gz

## offline-bundle: build the offline installation bundle (binflow_offline_<VER>.tar.gz).
offline-bundle: ensure-offline-prereqs
	@set -e; \
	REPO_ROOT=$$(pwd); \
	BUNDLE="$(OFFLINE_BUNDLE)"; \
	WORK="$$(mktemp -d)"; \
	mkdir -p "$$WORK/images" "$$WORK/charts" "$$WORK/k8s" "$$WORK/binaries" "$$WORK/compose"; \
	echo "=== Offline bundle: $(VER) ==="; \
	\
	# 1. Docker image tars (alpine + distroless). \
	echo "--- Saving Docker images…"; \
	docker save "binflow:$(VER)-alpine"    -o "$$WORK/images/binflow-$(VER)-alpine.tar"; \
	docker save "binflow:$(VER)-distroless" -o "$$WORK/images/binflow-$(VER)-distroless.tar"; \
	\
	# 2. Helm chart. \
	echo "--- Packaging Helm chart…"; \
	cd charts/binflow && helm package -d "$$WORK/charts" . > /dev/null; \
	cd "$$REPO_ROOT"; \
	\
	# 3. K8s manifests. \
	echo "--- Copying K8s manifests…"; \
	cp deploy/k8s/*.yaml "$$WORK/k8s/"; \
	\
	# 4. Compose files (needed by install-offline.sh --compose mode). \
	echo "--- Copying compose files…"; \
	cp deploy/compose/docker-compose.yml "$$WORK/compose/"; \
	cp deploy/compose/.env.example "$$WORK/compose/"; \
	cp deploy/compose/nginx.conf "$$WORK/compose/"; \
	\
	# 5. Linux binaries. \
	echo "--- Copying linux binaries…"; \
	cp "dist/binflow_$(VER)_linux_amd64/binflow-server" "$$WORK/binaries/binflow-$(VER)-linux-amd64"; \
	cp "dist/binflow_$(VER)_linux_arm64/binflow-server" "$$WORK/binaries/binflow-$(VER)-linux-arm64"; \
	echo "--- Computing binary checksums…"; \
	(cd "$$WORK/binaries" && shasum -a 256 binflow-$(VER)-linux-* > checksums.txt); \
	\
	# 5. Copy install script, README. \
	cp "$(OFFLINE_DIR)/install-offline.sh" "$$WORK/"; \
	chmod +x "$$WORK/install-offline.sh"; \
	cp "$(OFFLINE_DIR)/README-offline.md" "$$WORK/"; \
	\
	# 6. Generate SHA256SUMS for the bundle contents. \
	echo "--- Computing bundle SHA256SUMS…"; \
	(cd "$$WORK" && find . -type f | sort | xargs shasum -a 256 > SHA256SUMS); \
	\
	# 7. Create tar. \
	echo "--- Creating bundle: $$BUNDLE"; \
	mkdir -p "$(OFFLINE_DIR)"; \
	tar -czf "$$BUNDLE" -C "$$WORK" .; \
	\
	# 8. Record outer tar checksum. \
	(cd "$(OFFLINE_DIR)" && shasum -a 256 "binflow_offline_$(VER).tar.gz" > "binflow_offline_$(VER).tar.gz.sha256"); \
	\
	# 9. Report. \
	echo ""; \
	echo "=== Offline bundle complete ==="; \
	echo "  Bundle: $$BUNDLE"; \
	echo "  Size:   $$(wc -c < "$$BUNDLE" | tr -d ' ') bytes"; \
	echo "  SHA256: $$(cat "$(OFFLINE_DIR)/binflow_offline_$(VER).tar.gz.sha256")"; \
	\
	rm -rf "$$WORK"; \
	echo ""

## ensure-offline-prereqs: verify pre-requisites for offline-bundle target.
ensure-offline-prereqs:
	@set -e; \
	errors=0; \
	\
	if ! docker image inspect "binflow:$(VER)-alpine" &>/dev/null; then \
		echo "ERROR: Docker image 'binflow:$(VER)-alpine' not found. Run deploy/release/build-release.sh first."; \
		errors=$$((errors+1)); \
	fi; \
	if ! docker image inspect "binflow:$(VER)-distroless" &>/dev/null; then \
		echo "ERROR: Docker image 'binflow:$(VER)-distroless' not found. Run deploy/release/build-release.sh first."; \
		errors=$$((errors+1)); \
	fi; \
	if [ ! -f "dist/binflow_$(VER)_linux_amd64/binflow-server" ]; then \
		echo "ERROR: linux/amd64 binary not found at dist/binflow_$(VER)_linux_amd64/binflow-server. Run 'make release' first."; \
		errors=$$((errors+1)); \
	fi; \
	if [ ! -f "dist/binflow_$(VER)_linux_arm64/binflow-server" ]; then \
		echo "ERROR: linux/arm64 binary not found at dist/binflow_$(VER)_linux_arm64/binflow-server. Run 'make release' first."; \
		errors=$$((errors+1)); \
	fi; \
	if ! command -v helm &>/dev/null; then \
		echo "ERROR: helm is required but not found."; \
		errors=$$((errors+1)); \
	fi; \
	if [ "$$errors" -gt 0 ]; then \
		echo ""; \
		echo "Run these steps first:"; \
		echo "  1. make release      # build platform archives (dist/)"; \
		echo "  2. deploy/release/build-release.sh  # build Docker images"; \
		echo ""; \
		exit 1; \
	fi

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | sed 's/:/ ::/'
