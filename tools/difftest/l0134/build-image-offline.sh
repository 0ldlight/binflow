#!/bin/bash
# L013-4 offline image assembly — binflow:uat-l0134-bebd92b1-alpine
# DaoCloud alpine mirror unreachable (manifest inspect EOF, build-release phase 3);
# faithful replay per tools/difftest/l0072/build-image-offline.sh with fresh assets
# image from the failed run's completed phase 2 (binflow-release-assets:uat-l0134-bebd92b1).
# Tree note: Go code at cc55428d == bebd92b1 (git diff bebd92b1..cc55428d -- cmd internal = empty).
set -euo pipefail
cd "$(dirname "$0")/../../.."

VER=uat-l0134-bebd92b1
ARCH=amd64
W=/tmp/l0134-img
ASSETS_IMG=binflow-release-assets:uat-l0134-bebd92b1
ALPINE_DGST=sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ALPINE_REF=docker.m.daocloud.io/library/alpine:3.24

rm -rf "$W"; mkdir -p "$W/ctx"

# phase 1 — goreleaser binary (built beforehand: make release VER=$VER)
tar -xzf "dist/binflow_${VER}_linux_${ARCH}.tar.gz" -C "$W"
install -m 0755 "$W/binflow_${VER}_linux_${ARCH}/binflow-server" "$W/ctx/binflow-server"

# phase 2 — rootfs from cached assets image + binflow user append (verbatim)
ROOTFS="$W/ctx/rootfs"
mkdir -p "$ROOTFS/etc/ssl/certs" "$ROOTFS/usr/share" "$ROOTFS/var/lib/binflow" "$ROOTFS/etc/binflow"
cid="$(docker create "$ASSETS_IMG")"
trap 'docker rm "$cid" >/dev/null 2>&1 || true' EXIT
docker cp "$cid":/etc/passwd "$ROOTFS/etc/passwd"
docker cp "$cid":/etc/group "$ROOTFS/etc/group"
docker cp "$cid":/etc/ssl/certs/ca-certificates.crt "$ROOTFS/etc/ssl/certs/ca-certificates.crt"
docker cp "$cid":/usr/share/zoneinfo "$ROOTFS/usr/share/zoneinfo"
docker rm "$cid" >/dev/null; trap - EXIT
printf 'binflow:x:10001:10001:BinFlow:/var/lib/binflow:/sbin/nologin\n' >> "$ROOTFS/etc/passwd"
printf 'binflow:x:10001:\n' >> "$ROOTFS/etc/group"
touch "$ROOTFS/var/lib/binflow/.keep" "$ROOTFS/etc/binflow/.keep"

# phase 3+4 — classic builder, digest-pinned base resolved from local store
cp deploy/release/Dockerfile.prebuilt-alpine "$W/ctx/Dockerfile"
DOCKER_BUILDKIT=0 docker build \
  --build-arg "ALPINE_BASE=${ALPINE_REF}@${ALPINE_DGST}" \
  -t "binflow:${VER}-alpine-${ARCH}" "$W/ctx" >/dev/null
docker tag "binflow:${VER}-alpine-${ARCH}" "binflow:${VER}-alpine"

# verify (arch gate + embedded revision label)
f="$(docker image inspect "binflow:${VER}-alpine" --format '{{.Os}}/{{.Architecture}}')"
[ "$f" = "linux/${ARCH}" ] || { echo "ERROR: platform $f"; exit 1; }
strings -a "$W/ctx/binflow-server" | grep -m1 "uat-l0134-bebd92b1" >/dev/null && echo "label OK"
echo "OK binflow:${VER}-alpine ($f)"
