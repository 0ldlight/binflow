#!/usr/bin/env bash
# build-release.sh — build multi-arch GA images for BinFlow (T-132, PB-03).
#
# Builds both alpine and distroless variants for linux/amd64 + linux/arm64
# via `docker buildx`. The script tags images locally only; pushing to ghcr.io
# is a conductor + user-confirmed action (PRD M5 DoD 7, Q1) — credentials
# must NOT be committed or stored in CI.
#
# Usage:
#   ./deploy/release/build-release.sh           # snapshot build (dev version)
#   ./deploy/release/build-release.sh v1.0.0    # nominal GA build
#   VER=v1.0.0 REV=a1b2c3d ./deploy/release/build-release.sh
#
# Pre-requisites:
#   - docker buildx with a multi-arch builder (e.g., `docker buildx create --use`)
#   - qemu user-mode emulation for arm64 if running on amd64 host (or vice versa)
#   - repository root as working directory
#
# Output:
#   - binflow:<VER>-alpine       (manifest list: linux/amd64, linux/arm64)
#   - binflow:<VER>-distroless   (manifest list: linux/amd64, linux/arm64)
#   - docker buildx imagetools inspect report on stdout

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# ---- version resolution -----------------------------------------------------
# VER defaults from the env or Makefile (Q4: v1.0.0 baseline). A positional
# argument overrides the env.
# REV defaults to git short sha or the env.
VER="${VER:-v1.0.0}"
if [ $# -ge 1 ]; then
  VER="$1"
fi
REV="${REV:-$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")}"

# ---- platform selection -----------------------------------------------------
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

echo "=== BinFlow release image build ==="
echo "VER:       $VER"
echo "REVISION:  $REV"
echo "PLATFORMS: $PLATFORMS"
echo ""

# ---- ensure buildx builder --------------------------------------------------
BUILDER="${BUILDER:-binflow-builder}"
if ! docker buildx inspect "$BUILDER" &>/dev/null; then
  echo "--- creating buildx builder: $BUILDER"
  docker buildx create --name "$BUILDER" --use --bootstrap
else
  docker buildx use "$BUILDER"
fi

# ---- alpine variant ---------------------------------------------------------
echo ""
echo "=== building alpine variant ==="
docker buildx build \
  --platform "$PLATFORMS" \
  -f deploy/release/Dockerfile.alpine \
  -t "binflow:${VER}-alpine" \
  --build-arg BINFLOW_VER="${VER}" \
  --build-arg BINFLOW_REVISION="${REV}" \
  --load \
  .

echo "--- alpine imagetools inspect ---"
docker buildx imagetools inspect "binflow:${VER}-alpine" || true

# ---- distroless variant -----------------------------------------------------
echo ""
echo "=== building distroless variant ==="
docker buildx build \
  --platform "$PLATFORMS" \
  -f deploy/release/Dockerfile.distroless \
  -t "binflow:${VER}-distroless" \
  --build-arg BINFLOW_VER="${VER}" \
  --build-arg BINFLOW_REVISION="${REV}" \
  --load \
  .

echo "--- distroless imagetools inspect ---"
docker buildx imagetools inspect "binflow:${VER}-distroless" || true

# ---- summary -----------------------------------------------------------------
echo ""
echo "=== build complete ==="
echo "Images:"
echo "  binflow:${VER}-alpine"
echo "  binflow:${VER}-distroless"
echo ""
echo "Smoke test (alpine, amd64):"
echo "  docker run --rm -d --name bf-alpine -p 18080:8080 binflow:${VER}-alpine"
echo "  curl -s http://127.0.0.1:18080/readyz"
echo "  docker stop bf-alpine"
echo ""
echo "Smoke test (distroless, amd64):"
echo "  docker run --rm -d --name bf-distroless -p 18081:8080 binflow:${VER}-distroless"
echo "  curl -s http://127.0.0.1:18081/readyz"
echo "  docker stop bf-distroless"