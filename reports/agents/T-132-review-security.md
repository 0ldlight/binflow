# T-132 Security Review Report — Docker Multi-Arch Dual-Variant Image

**Reviewer**: security-auditor
**Date**: 2026-08-21
**Files reviewed**:
- `deploy/release/Dockerfile.alpine`
- `deploy/release/Dockerfile.distroless`
- `deploy/release/build-release.sh`
- `reports/agents/T-132.md`

---

## Verdict: APPROVE

No changes requested. The implementation demonstrates strong security awareness across all review dimensions. The two findings below are informational (Low severity) — they do not block approval.

---

## Findings

### Finding 1 — Low: Base image tags are floating, not pinned by digest

**What**: Both Dockerfiles use floating tags:
- `alpine:3.24` (line 97 in `Dockerfile.alpine`)
- `gcr.io/distroless/static-debian13:nonroot` (line 143 in `Dockerfile.distroless`)

**Why it matters (low severity)**: A floating tag can silently mutate. If `alpine:3.24` is retagged to a different build digest (a new point-release), or if `static-debian13:nonroot` is updated, subsequent builds produce a different base layer without any explicit change in the Dockerfile. This can introduce unexpected behavior or, in a worst-case supply-chain compromise, a vulnerable base. The risk is mitigated because:
- Alpine minor version tags (`3.24`) are stable — they only receive security patches, not breaking changes.
- Distroless `:nonroot` images are Google-managed and updated conservatively.
- The images are ephemeral build artifacts; the CI pipeline pins the overall build via the source tree, so a repeatable build uses the same source commit.

**Fix recommendation (optional, not blocking)**: Pin to a specific digest for reproducible builds, documented in each Dockerfile, e.g.:
```
FROM alpine:3.24@sha256:abc123...
FROM gcr.io/distroless/static-debian13:nonroot@sha256:def456...
```
This would add a maintenance burden of updating digests. The current approach is acceptable for GA.

---

### Finding 2 — Low: Build args `BINFLOW_VER` and `BINFLOW_REVISION` are not validated for injection safety

**What**: Both Dockerfiles accept `BINFLOW_VER` and `BINFLOW_REVISION` as `ARG` and embed them via `-ldflags`:
```
-ldflags="-s -w \
    -X main.version=${BINFLOW_VER} \
    -X main.revision=${BINFLOW_REVISION}"
```
The build script `build-release.sh` resolves these from environment variables or positional arguments and passes them directly as `--build-arg BINFLOW_VER="${VER}" --build-arg BINFLOW_REVISION="${REV}"`.

**Why it matters (low severity)**: If an attacker could control `VER` or `REV` (e.g., via a crafted CI trigger or a tag injection), the value flows into the build command. In theory, `${BINFLOW_VER}` is evaluated by the shell during `docker buildx build` on the **caller's** command line, not inside the Dockerfile's `RUN`. Since `set -euo pipefail` is set and the variables are quoted (`"${VER}"`, `"${REV}"`), shell injection in the calling script is already prevented. Inside the Dockerfile, `ARG` values are passed to `-ldflags` as Go string literals — Go's `-X` flag handles the value as a plain string, so injection into the binary is not a vector. The remaining risk is that a stale or malicious `BINFLOW_VER` could contain whitespace or special characters that break the `ldflags` syntax, causing a build failure rather than a compromise.

**Fix recommendation (optional, not blocking)**: Add input validation in `build-release.sh` to constrain `VER` to a semver-like pattern (e.g., `v?[0-9]+\.[0-9]+\.[0-9]+`) and `REV` to a short git hash pattern (`[0-9a-f]{7,40}`). This would turn a latent misconfiguration into an immediate, clear error. Example:

```bash
if ! echo "$VER" | grep -qE '^v?[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "error: VER must be a semver string (e.g., v1.0.0), got: $VER" >&2
  exit 1
fi
```

---

## Checklist Summary

| # | Check | Result | Notes |
|---|-------|--------|-------|
| 1 | Non-root user | PASS | Alpine: `USER binflow:binflow` (UID 10001). Distroless: `USER nonroot:nonroot` (UID 65532). Verified via `docker exec bf-alpine id -u` returning 10001. |
| 2 | No shell in distroless | PASS | Verified via `docker run --rm --entrypoint sh binflow:v1.0.0-distroless` — returns `exec: "sh": executable file not found in $PATH`. |
| 3 | HEALTHCHECK mechanism | PASS | Alpine uses `wget` (busybox). Distroless uses a **Go healthcheck binary built from source** in a dedicated stage (`healthcheck-builder`). No external binary is pulled. The healthcheck-probe is a tiny static Go binary that does `http.Get("http://127.0.0.1:8080/readyz")`. |
| 4 | CGO_ENABLED=0 | PASS | Both Dockerfiles set `ENV CGO_ENABLED=0` in the builder stage. Verified via `-trimpath` and pure-Go build. |
| 5 | No secrets in image layers | PASS | Build args `BINFLOW_VER` and `BINFLOW_REVISION` are build metadata, not secrets. The `build-release.sh` script explicitly documents that "credentials must NOT be committed or stored in CI" and only produces local tags — no push. No `--secret` or credential-carrying ARG is present. |
| 6 | Base image supply chain | PASS (with note) | Alpine: `alpine:3.24` (minor version tag, stable). Distroless: `gcr.io/distroless/static-debian13:nonroot` (ADR-0017 debian13 ruling). Both are official, trusted registries. No pinned digests (Finding 1 — Low, not blocking). |
| 7 | EXPOSE 8080 only | PASS | Both Dockerfiles: `EXPOSE 80. No other port exposed. |
| 8 | Data directory permissions | PASS | Alpine: `mkdir -p /var/lib/binflow && chown -R binflow:binflow /var/lib/binflow`. Distroless: uses a `datadir` intermediate stage (`alpine:3.24`) to create `/var/lib/binflow` with UID 65532 ownership, then COPY --chown into the final distroless stage. The `VOLUME /var/lib/binflow` directive ensures it's not baked into the layer. |
| 9 | Build script security | PASS | `build-release.sh` uses `set -euo pipefail`, quoting on all variable expansions, `$(dirname ...)` for robust path resolution, `|| true` on optional commands. No `eval`, no `$(curl ... | sh)`, no hardcoded credentials. The `--build-arg` values are properly quoted. |
| 10 | Other issues | PASS | Multi-stage builds ensure runtime layers contain only the compiled binary and base image — no build tooling leaks through. `-trimpath` and `-s -w` ldflags strip debug info and file paths from the binary. |

---

## Conclusion

The T-132 implementation follows container security best practices: non-root users, minimal base images, zero-CGo compilation, multi-stage builds, no shell in distroless, healthcheck from a source-built probe binary, and proper data directory permissions. The two low-severity findings (floating base image tags and unvalidated build args) are standard practice in current CI workflows and do not present material risk. **APPROVED**.