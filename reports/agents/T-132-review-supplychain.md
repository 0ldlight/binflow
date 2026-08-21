# T-132 Supply-Chain / Configuration Review

**Reviewer**: supply-chain reviewer
**Date**: 2026-08-21
**Files reviewed**:
- `deploy/release/Dockerfile.alpine`
- `deploy/release/Dockerfile.distroless`
- `deploy/release/build-release.sh`
- `deploy/dev/Dockerfile` (zero-change verification)
- `docs-site/docusaurus.config.js`
- `internal/console/console.go`
- `internal/docs/docs.go`
- `Makefile`

---

## Verdict: APPROVE (with medium recommendations)

---

## Findings

### 1. [Medium] No explicit docker buildx `--provenance` or `--sbom` flags for SLSA provenance

**What**: The `docker buildx build` invocation in both `build-release.sh` and the Dockerfile headers does not set `--attest type=provenance` or `--attest type=sbom`. These are GA release images for a production artifact repository — provenance attestation is valuable for supply-chain integrity.

**Why**: Without attestation, consumers cannot verify which Dockerfile and which commit produced the image. `docker buildx` supports provenance attestation natively (buildkit `--attest type=provenance`). The current images are bare-bit-identical but carry no chain-of-custody metadata.

**Fix recommendation**: Add `--attest type=provenance,mode=min` to the `docker buildx build` invocations in `build-release.sh` (lines 60-67 and 75-82). The `min` mode records the build command and source commit without full SBOM overhead. If full SBOM is desired, add `--attest type=sbom` for production releases. Note: `--load` currently conflicts with attestations on some buildx versions — this may need `--set "*.attest=..."` or switching to `--output type=image` with `--push`.

---

### 2. [Medium] Distroless `HEALTHCHECK` probe is compiled statically but has no build cache mount

**What**: The `healthcheck-builder` stage in `Dockerfile.distroless` (line 132) runs `go build` without `--mount=type=cache,target=/go/pkg/mod` or `--mount=type=cache,target=/root/.cache/go-build`. The main `builder` stage in both Dockerfiles correctly uses both cache mounts.

**Why**: This is a very small self-contained module (inline `go.mod` + `main.go`, no dependencies), so the build time impact is negligible. The Go standard library is also small here. However, for principle of consistency and for an incremental rebuild scenario, the cache mount would still be beneficial.

**Fix recommendation**: Add cache mounts to the healthcheck build, matching the builder stage pattern:
```
RUN --mount=type=cache,target=/go/pkg/mod \
    go build -trimpath -ldflags="-s -w" -o /healthcheck/probe
```

---

### 3. [Medium] `build-release.sh` does not validate that the Docker CLI and buildx are actually multi-arch capable before building

**What**: The script checks if the `binflow-builder` builder exists and creates/uses it, but does not validate that QEMU user-mode emulation is installed or that the builder can actually target both `linux/amd64` and `linux/arm64`.

**Why**: If a user runs the script without QEMU installed (`docker run --privileged --rm tonistiigi/binfmt --install all` not yet performed), `docker buildx build` will silently fail or produce broken arm64 images (the build may appear to succeed but arm64 layers may be broken). The script currently trusts the environment is set up without validation.

**Fix recommendation**: Add a pre-flight validation step in `build-release.sh` after the builder bootstrap:
```bash
# Validate multi-arch capability
echo "--- validating multi-arch support..."
docker buildx inspect "$BUILDER" | grep -q "linux/arm64" || {
  echo "WARNING: builder may not support linux/arm64."
  echo "Install QEMU: docker run --privileged --rm tonistiigi/binfmt --install all"
}
```
This should be a warning, not a hard failure — the user may have intentionally set `PLATFORMS` to a single platform.

---

### 4. [Medium] Version resolution fallback from git is not validated as a semver-conforming string

**What**: `build-release.sh` line 37 uses `git rev-parse --short HEAD` for `REV`, which produces a hex hash, and defaults `VER` to `v1.0.0` from the Makefile. Both are valid defaults. However, the positional argument `$1` for `VER` is not validated — any string, including empty, gets assigned.

**Why**: If someone runs `./deploy/release/build-release.sh ""` (empty string argument), the script will tag images as `binflow:-alpine` and `binflow:-distroless`, which are syntactically invalid Docker tags. `VER` would be empty because `$1` overrides the default.

**Fix recommendation**: Add a simple validation after version resolution (around line 38):
```bash
if [ -z "$VER" ]; then
  echo "ERROR: VER is empty. Provide a version (e.g., v1.0.0)." >&2
  exit 1
fi
```
Optionally add a semver-ish regex check: `[[ "$VER" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+ ]] || { echo "WARNING: VER does not look like semver"; }`.

---

### 5. [Low] `web/package-lock.json` and `docs-site/package-lock.json` are copied but npm dedupe/hoisting behavior is not explicitly locked

**What**: Both the console and docs stages use `npm ci` (which is correct — it uses the lockfile exactly). The `package-lock.json` is COPY'd before `npm ci`. This is correct practice.

**Observation**: No issue here. `npm ci` with a lockfile is the recommended approach for reproducible builds. The stages correctly COPY the lockfile before running npm. Filing as a positive note kept as a finding for documentation: **this is correctly implemented**.

---

### 6. [Low] Docusaurus path fix is verified correct

**What**: `docusaurus.config.js` line 71 sets `path: '../docs/user'`, which resolves relative to the `docs-site/` directory. The Dockerfiles perform `COPY docs/ /docs/` at the filesystem root (line 52 in both Dockerfiles). Inside the Docusaurus container, `/docs/` is the absolute mount; the config's relative `../docs/user` resolves to `/docs/user` relative to `/docs-site` -> `/docs/user`.

**Verification**: Correct. `docs-site/docusaurus.config.js` is at `/docs-site/docusaurus.config.js` (WORKDIR). `../docs/user` from there is `/docs/user`, and the `COPY docs/ /docs/` places the content tree at `/docs/`. The Docusaurus build will find the markdown source at `/docs/user/`.

**Status**: Confirmed working as documented in T-132 report.

---

### 7. [Low] Dev image zero-change verified

**What**: `deploy/dev/Dockerfile` was verified via `git diff HEAD~1..HEAD -- deploy/dev/Dockerfile` — no changes. The latest commit (`679dcc9` for T-132) touched only `deploy/release/Dockerfile.alpine`, `deploy/release/Dockerfile.distroless`, and `deploy/release/build-release.sh`.

**Status**: PASS — zero modifications to dev image. T-132 report AC G08 is confirmed.

---

### 8. [Low] Image size claims are credible

**Analysis**:
- **Binary**: The Go binary (`bin/binflow-server`) is roughly 22MB (per the `check-size` target in Makefile and the removed binary size). With `-trimpath -ldflags="-s -w"`, stripping debug info, the binary is ~22MB.
- **Alpine base**: `alpine:3.24` is approximately 7MB.
- **ca-certificates + tzdata**: Small, ~3MB.
- **Console SPA**: Vite build output, likely 100-500KB gzip'd for JS/CSS assets.
- **Docs site**: `Makefile` docs-size target caps at 15MB. The actual docs build from the workspace appears to be ~2-3MB.
- **Sum**: ~22MB (binary) + 7MB (alpine) + 3MB (certs/tzdata) + ~3MB (static assets) ≈ 35MB, with the alpine overhead pushing to ~38MB (reported 38.2MB). Credible.
- **Distroless**: Binary is the same ~22MB. `gcr.io/distroless/static-debian13:nonroot` is approximately 2-5MB. No apk layers. Reported 958 36.8MB is credibly ~14MB overhead for the binary + assets + ca-cert bundle.

**Status**: Size claims (38.2MB alpine, 36.8MB distroless) are reasonable and consistent with the stage composition. Both are well under the 60MB/40MB thresholds.

---

### 9. [Informational] Multi-arch readiness is correct but untested for arm64

**What**: Both Dockerfiles use `FROM` with architecture-independent base images (`alpine:3.24`, `gcr.io/distroless/static-debian13:nonroot`, `golang:1.26-alpine`, `node:22-alpine`). These support both `linux/amd64` and `linux/arm64` natively on Docker Hub. `CGO_ENABLED=0` in the builder stage ensures pure Go cross-compilation works for both architectures. The `--platform linux/amd64,linux/arm64` in the build commands is correct.

**Verification**: The multi-arch approach is architecturally sound. The `--platform` flags are correctly placed in the `docker buildx build` invocation. ARM64 is untested locally (expected — docker driver does not support multi-platform `--load`), but would work in CI with a container driver and QEMU.

**Status**: Correctly implemented. Awaiting CI verification for arm64.

---

### 10. [Informational] GOPROXY configuration for reproducible builds

**What**: Both Dockerfiles set `GOPROXY=https://goproxy.cn,direct` in the builder stage (line 65 of both). This uses a known proxy with `direct` fallback.

**Verification**: This matches the project's standard GOPROXY configuration from the Makefile. The `go.sum` file provides checksum verification regardless of proxy. The module download uses cache mounts (`--mount=type=cache,target=/go/pkg/mod`). No vendor directory exists, so module download from the proxy is the expected path. This is consistent with the rest of the project.

**Status**: Correct. Build reproducibility is ensured by `go.sum` + lockfile (`package-lock.json`) + `npm ci`.

---

### 11. [Informational] Tagging strategy is minimal but appropriate for local builds

**What**: `build-release.sh` tags images as `binflow:${VER}-alpine` and `binflow:${VER}-distroless`. There are no floating tags (e.g., `:latest`, `:stable`). The script explicitly says "only local tag."

**Observation**: For local pre-release verification, this is appropriate. The T-132 report correctly notes that push to a registry is outside scope (Q1/DoD 7). When pushing does happen, a tagging convention (e.g., `ghcr.io/binflow/binflow:v1.0.0-alpine`, `ghcr.io/binflow/binflow:v1.0.0-distroless`, with optional `:latest-alpine` floating tag for convenience) should be established. Recommend adding `:latest-alpine` and `:latest-distroless` floating tags at push time for user convenience.

**Status**: Acceptable for current scope. No changes requested.

---

### 12. [Informational] Layer optimization is good

**What**: Both Dockerfiles use a textbook multi-stage pattern:
1. Lockfile copied first (`package.json` + `package-lock.json` / `go.mod` + `go.sum`), then `npm ci` / `go mod download` — this layer is cached as long as dependencies don't change.
2. Source code copied after dependency layer.
3. Build output replaces gitignored local artifacts (the `rm -rf ... COPY --from=...` pattern).
4. Runtime stage is minimal — only the binary and necessary config.

**Verification**: The layering is well-optimized. The dependency-cache-then-source pattern means a source-code-only change reuses cached dependency layers. Cache mounts on `go build` further accelerate rebuilds. The `rm -rf` + `COPY --from=` pattern correctly handles the gitignored dist directories. No unnecessary layers are present.

**One observation**: The docs stage copies `docs-site/` after `npm ci` but could split into two COPY instructions (config files first, then source) for better caching of config-only changes. This is a minor optimization and not required.

**Status**: Good as-is.

---

## Summary

| # | Severity | Finding | Recommendation |
|---|---|---|---|
| 1 | Medium | No SLSA provenance attestation | Add `--attest type=provenance,mode=min` to build commands |
| 2 | Medium | Healthcheck probe build lacks cache mounts | Add cache mounts for consistency |
| 3 | Medium | No QEMU/multi-arch pre-flight validation | Add builder capability check after bootstrap |
| 4 | Medium | No version format validation | Guard against empty or malformed VER |
| 5 | Low | npm lockfile handling is correct (positive) | No action |
| 6 | Low | Docusaurus path fix verified correct | No action |
| 7 | Low | Dev image zero-change verified | No action |
| 8 | Low | Image size claims are credible | No action |
| 9 | Info | Multi-arch design is correct | Await CI verification for arm64 |
| 10 | Info | GOPROXY / reproducible build is correct | No action |
| 11 | Info | Tagging strategy is appropriate | Consider floating tags at push time |
| 12 | Info | Layer optimization is good | Consider minor COPY split for docs stage |

**Four medium recommendations** are for hardening, not correctness — the images build, run, and pass smoke tests as demonstrated in the T-132 report. None block approval.