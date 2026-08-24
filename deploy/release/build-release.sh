#!/usr/bin/env bash
# build-release.sh — prebuilt multi-arch GA images for BinFlow (T-270,
# FR-83-AC2 / M9 Q6 final ruling).
#
# T-132 delivered the two image variants (alpine + distroless) as full
# source builds (console/docs/Go compiled INSIDE buildx — arm64 means qemu,
# infeasible on the 2C CI VM). T-270 rewrote the release chain to PREBUILT
# INJECTION:
#
#   1. `make release VER=<VER>` runs goreleaser, which natively
#      cross-compiles six platforms on the build host (CGO_ENABLED=0,
#      ADR-0005) — linux/amd64 AND linux/arm64 binaries land in dist/ with
#      zero emulation cost.
#   2. This script injects each arch's binflow-server verbatim into a
#      runtime Dockerfile whose final stage has ZERO RUN instructions
#      (deploy/release/Dockerfile.prebuilt-*), so the arm64 image assembles
#      on an amd64-only host with no buildx/qemu/binfmt — verified on the CI
#      VM's docker 29 legacy builder.
#   3. Per-arch tags are pushed, then stitched into one multi-arch tag via
#      docker manifest create/annotate/push.
#
# Destination is the LOCAL BinFlow instance's docker-local repo (dogfood,
# T-248 precedent) — never a public registry. Public publishing remains a
# conductor + user-confirmed action with user-injected credentials
# (PRD M5 DoD 7, Q1).
#
# Usage:
#   make release VER=v1.0.0                 # step 1 (six-platform archives)
#   ./deploy/release/build-release.sh v1.0.0
#   REGISTRY= PUSH=0 ./deploy/release/build-release.sh v1.0.0   # build only
#
# Environment knobs (all optional):
#   VER / $1         release version (default v1.0.0; must match make release)
#   REGISTRY         push destination, host[:port]/repoPath.
#                    Default 172.16.58.129:8080/docker-local (the local
#                    BinFlow). Empty string = local build only, no push and
#                    no manifest assembly (local field checks still run).
#   IMG_NAME         image name inside the registry (default binflow)
#   ARCHES           default "amd64 arm64"
#   VARIANTS         default "alpine distroless"
#   ALPINE_SRC       alpine base ref (default: DaoCloud mirror of alpine:3.24;
#                    registry-1.docker.io is unreachable from the CI networks)
#   DISTROLESS_SRC   distroless base ref (default: DaoCloud mirror of
#                    gcr.io/distroless/static-debian13:nonroot, ADR-0017 K2)
#   APK_MIRROR       apk mirror for the assets stage (the CI VM needs
#                    mirrors.tuna.tsinghua.edu.cn; empty = dl-cdn)
#   PUSH             1 (default) push per-arch tags + manifest list
#   DOCKER_USER/
#   DOCKER_PASS      optional credentials for `docker login "$REG_HOST"`
#   ALSO_LATEST      0/1 (default 0) also push the manifest as :latest
#   VERIFY           1 (default) manifest + image config architecture checks
#   SMOKE            1 (default) run the amd64 image -> /readyz + version;
#                    then probe arm64: run it if qemu-user is available on
#                    this host, otherwise record that the arm64 leg carries
#                    on the manifest/config architecture fields (M9 PRD
#                    FR-83-AC2 DoD relaxation)
#   SMOKE_ADDR       where published smoke ports answer (default 127.0.0.1;
#                    inside the Jenkins agent container set it to the docker
#                    host IP — T-248 pitfall #7)
#   SMOKE_PORT       default 18090 (arm64 leg uses SMOKE_PORT+1)
#   PULL_BACK        0/1, default = PUSH: pull the pushed manifest back under
#                    the canonical local name binflow:<VER>-<variant> (the
#                    offline-bundle face consumes those names)
#
# Output:
#   local:   binflow:<VER>-<variant>-<arch>          (per-arch images)
#   pushed:  <REGISTRY>/binflow:<VER>-<variant>      (multi-arch manifest)
#            <REGISTRY>/binflow:<VER>-<variant>-<arch>  (per-arch children)
#            <REGISTRY>/binflow:latest-<variant>     (when ALSO_LATEST=1)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# ---- knobs ------------------------------------------------------------------

VER="${VER:-v1.0.0}"
if [ $# -ge 1 ]; then
  VER="$1"
fi
REV="${REV:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
# ${REGISTRY-...} (not :-) so an explicitly EMPTY REGISTRY means local-only.
REGISTRY="${REGISTRY-172.16.58.129:8080/docker-local}"
IMG_NAME="${IMG_NAME:-binflow}"
ARCHES="${ARCHES:-amd64 arm64}"
VARIANTS="${VARIANTS:-alpine distroless}"
ALPINE_SRC="${ALPINE_SRC:-docker.m.daocloud.io/library/alpine:3.24}"
DISTROLESS_SRC="${DISTROLESS_SRC:-gcr.m.daocloud.io/distroless/static-debian13:nonroot}"
APK_MIRROR="${APK_MIRROR:-}"
PUSH="${PUSH:-1}"
ALSO_LATEST="${ALSO_LATEST:-0}"
VERIFY="${VERIFY:-1}"
SMOKE="${SMOKE:-1}"
SMOKE_ADDR="${SMOKE_ADDR:-127.0.0.1}"
SMOKE_PORT="${SMOKE_PORT:-18090}"
PULL_BACK="${PULL_BACK:-$PUSH}"
REG_SCHEME="${REG_SCHEME:-http}"   # the local BinFlow speaks plain HTTP
WORK="${WORK:-$(mktemp -d -t binflow-release.XXXXXX)}"

if [ -n "$REGISTRY" ]; then
  REG_HOST="${REGISTRY%%/*}"
  REG_REPO_PATH="${REGISTRY#*/}"
  [ "$REG_REPO_PATH" = "$REGISTRY" ] && REG_REPO_PATH=""
  IMG_PUSH_PREFIX="${REGISTRY}/${IMG_NAME}"
else
  REG_HOST=""; REG_REPO_PATH=""; IMG_PUSH_PREFIX=""
  PUSH=0; PULL_BACK=0
fi

T0="$SECONDS"
log() { printf '\n=== %s\n' "$*"; }
info() { printf '    %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "required tool not found: $1"; }
need docker; need python3; need tar; need curl
case " $VARIANTS " in
  *" distroless "*) need go ;;
esac

# reg_flags <ref> — docker-manifest subcommands talk to the registry directly
# (not through the daemon), so a plain-HTTP registry needs --insecure.
reg_flags() {
  [ -n "$REG_HOST" ] || return 0
  case "$1" in
    "$REG_HOST"/*) echo "--insecure" ;;
  esac
}

# py_digest_for <arch> <index.json> — print the linux/<arch> child digest.
# Takes a FILE path (not stdin: the heredoc below already owns stdin).
# Bash 3.2 compatible (macOS): no associative arrays anywhere.
py_digest_for() {
  python3 - "$1" "$2" <<'PY'
import json, sys
want, path = sys.argv[1], sys.argv[2]
idx = json.load(open(path))
for m in idx.get("manifests", []):
    p = m.get("platform") or {}
    if p.get("os") == "linux" and p.get("architecture") == want:
        print(m["digest"])
        sys.exit(0)
sys.exit(1)
PY
}

# py_config_digest <manifest.json> — print the config descriptor digest.
py_config_digest() {
  python3 - "$1" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
print(m["config"]["digest"])
PY
}

mb() { awk -v b="$1" 'BEGIN { printf "%.1f MB", b / 1048576 }'; }

log "BinFlow multi-arch release image build (prebuilt injection, T-270)"
info "VER:       $VER"
info "REVISION:  $REV"
info "ARCHES:    $ARCHES"
info "VARIANTS:  $VARIANTS"
info "REGISTRY:  ${REGISTRY:-<local only>}"
info "WORKDIR:   $WORK"

# ---- phase 1: goreleaser archives -------------------------------------------

log "phase 1/6: goreleaser archives (make release VER=$VER)"
for arch in $ARCHES; do
  f="dist/binflow_${VER}_linux_${arch}.tar.gz"
  [ -f "$f" ] || die "archive missing: $f — run 'make release VER=$VER' first"
  mkdir -p "$WORK/extract-$arch"
  tar -xzf "$f" -C "$WORK/extract-$arch"
  bin="$WORK/extract-$arch/binflow_${VER}_linux_${arch}/binflow-server"
  [ -x "$bin" ] || die "extracted binary missing/not executable: $bin"
  install -m 0755 "$bin" "$WORK/binflow-server-$arch"
  info "$(printf 'linux/%s binflow-server: %s' "$arch" "$(mb "$(stat -f%z "$WORK/binflow-server-$arch" 2>/dev/null || stat -c%s "$WORK/binflow-server-$arch")")")"
done

# ---- phase 2: arch-independent assets (native build, no emulation) -----------

log "phase 2/6: native assets stage (ca-certificates, tzdata, passwd/group)"
ASSETS_IMG="binflow-release-assets:${VER}"
# Empty context — Dockerfile.assets COPYs nothing; sending the repo root as
# context would tar up the whole working tree (docs-site build products are
# gigabytes) for no reason.
mkdir -p "$WORK/assets-ctx"
docker build -q -t "$ASSETS_IMG" \
  --build-arg ALPINE_SRC="$ALPINE_SRC" \
  --build-arg APK_MIRROR="$APK_MIRROR" \
  -f deploy/release/Dockerfile.assets "$WORK/assets-ctx" >/dev/null
ROOTFS="$WORK/rootfs"
mkdir -p "$ROOTFS/etc/ssl/certs" "$ROOTFS/usr/share" "$ROOTFS/var/lib/binflow" "$ROOTFS/etc/binflow"
cid="$(docker create "$ASSETS_IMG")"
docker cp "$cid":/etc/passwd "$ROOTFS/etc/passwd"
docker cp "$cid":/etc/group "$ROOTFS/etc/group"
docker cp "$cid":/etc/ssl/certs/ca-certificates.crt "$ROOTFS/etc/ssl/certs/ca-certificates.crt"
docker cp "$cid":/usr/share/zoneinfo "$ROOTFS/usr/share/zoneinfo"
docker rm "$cid" >/dev/null
printf 'binflow:x:10001:10001:BinFlow:/var/lib/binflow:/sbin/nologin\n' >> "$ROOTFS/etc/passwd"
printf 'binflow:x:10001:\n' >> "$ROOTFS/etc/group"
touch "$ROOTFS/var/lib/binflow/.keep" "$ROOTFS/etc/binflow/.keep"
info "assets staged: $(du -sh "$ROOTFS" | awk '{print $1}') (passwd/group + CA bundle + tzdata)"

# ---- phase 3: base digests ---------------------------------------------------

log "phase 3/6: per-arch base digests"
fetch_index() { # $1 = base ref, $2 = outfile — the mirrors are slow at times
  local attempt
  for attempt in 1 2 3; do
    if docker manifest inspect "$1" > "$2" 2> "$WORK/idx.err"; then return 0; fi
    info "manifest inspect $1 failed (attempt $attempt/3): $(tail -1 "$WORK/idx.err" 2>/dev/null)"
    sleep 3
  done
  die "manifest inspect failed after 3 attempts: $1"
}
fetch_index "$ALPINE_SRC" "$WORK/index-alpine.json"
fetch_index "$DISTROLESS_SRC" "$WORK/index-distroless.json"
for arch in $ARCHES; do
  py_digest_for "$arch" "$WORK/index-alpine.json"     > "$WORK/dgst-alpine-$arch" \
    || die "no linux/$arch manifest in base index: $ALPINE_SRC"
  py_digest_for "$arch" "$WORK/index-distroless.json" > "$WORK/dgst-distroless-$arch" \
    || die "no linux/$arch manifest in base index: $DISTROLESS_SRC"
  info "linux/$arch: alpine $(cat "$WORK/dgst-alpine-$arch") / distroless $(cat "$WORK/dgst-distroless-$arch")"
done

# ---- phase 4: per-arch image builds (zero-RUN runtime stages) ----------------

log "phase 4/6: per-arch image builds"
HAVE_BUILDX=0
if docker buildx version >/dev/null 2>&1; then HAVE_BUILDX=1; fi
info "builder: $([ "$HAVE_BUILDX" = 1 ] && echo 'docker buildx (buildkit)' || echo 'plain docker build (legacy builder, digest-pinned bases)')"

build_image() { # $1 variant, $2 arch
  local variant="$1" arch="$2" ctx="$WORK/ctx-$1-$2"
  local local_tag="binflow:${VER}-${variant}-${arch}"
  local push_tag=""
  [ -n "$IMG_PUSH_PREFIX" ] && push_tag="${IMG_PUSH_PREFIX}:${VER}-${variant}-${arch}"
  rm -rf "$ctx"; mkdir -p "$ctx"
  cp "deploy/release/Dockerfile.prebuilt-${variant}" "$ctx/Dockerfile"
  cp -R "$ROOTFS" "$ctx/rootfs"
  install -m 0755 "$WORK/binflow-server-$arch" "$ctx/binflow-server"
  local base_arg
  if [ "$variant" = alpine ]; then
    base_arg="--build-arg ALPINE_BASE=${ALPINE_SRC}@$(cat "$WORK/dgst-alpine-$arch")"
  else
    base_arg="--build-arg DISTROLESS_BASE=${DISTROLESS_SRC}@$(cat "$WORK/dgst-distroless-$arch")"
    install -m 0755 "$WORK/probe-$arch" "$ctx/healthcheck-probe"
  fi
  local t="$SECONDS"
  local tags=""
  [ -n "$push_tag" ] && tags="-t $push_tag"
  # shellcheck disable=SC2086 # base_arg/tags word-split by design
  if [ "$HAVE_BUILDX" = 1 ]; then
    # --platform aligns the build with the digest-pinned base (silences the
    # platform-mismatch lint); --provenance/--sbom=false keeps each child a
    # plain single manifest (attestation children would pollute the list).
    docker buildx build \
      --platform "linux/$arch" --provenance=false --sbom=false \
      $base_arg $tags -t "$local_tag" --load "$ctx" >/dev/null
  else
    docker build $base_arg $tags -t "$local_tag" "$ctx" >/dev/null
  fi
  local fields
  fields="$(docker image inspect "$local_tag" --format '{{.Os}}/{{.Architecture}}')"
  [ "$fields" = "linux/$arch" ] \
    || die "image $local_tag reports platform $fields, expected linux/$arch"
  info "$(printf 'binflow:%s-%s-%s  %s  %s  (%ss)' \
    "$VER" "$variant" "$arch" "$fields" \
    "$(mb "$(docker image inspect "$local_tag" --format '{{.Size}}')")" \
    "$((SECONDS - t))")"
}

# The distroless HEALTHCHECK agent, cross-compiled per arch (deploy/release/
# healthcheck-probe — distroless has no shell, T-132 AC G07).
case " $VARIANTS " in
  *" distroless "*)
    for arch in $ARCHES; do
      CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
        go build -trimpath -ldflags "-s -w" \
        -o "$WORK/probe-$arch" ./deploy/release/healthcheck-probe
      info "healthcheck-probe linux/$arch: $(mb "$(stat -f%z "$WORK/probe-$arch" 2>/dev/null || stat -c%s "$WORK/probe-$arch")")"
    done
    ;;
esac
for variant in $VARIANTS; do
  for arch in $ARCHES; do
    build_image "$variant" "$arch"
  done
done

# ---- phase 5: push per-arch + manifest assembly ------------------------------

push_manifest_tag() { # $1 = manifest list ref, rest = already-pushed child refs
  local mref="$1"; shift
  local flags; flags="$(reg_flags "$mref")"
  # shellcheck disable=SC2086 # flags word-splits by design
  docker manifest rm "$mref" >/dev/null 2>&1 || true
  # shellcheck disable=SC2086
  docker manifest create $flags "$mref" "$@" >/dev/null
  local child
  for child in "$@"; do
    local a="${child##*-}"
    docker manifest annotate "$mref" "$child" --os linux --arch "$a"
  done
  # shellcheck disable=SC2086
  docker manifest push $flags "$mref"
}

if [ "$PUSH" = 1 ]; then
  log "phase 5/6: push per-arch tags + manifest assembly -> $REGISTRY"
  if [ -n "${DOCKER_USER:-}" ]; then
    echo "$DOCKER_PASS" | docker login -u "$DOCKER_USER" --password-stdin "$REG_HOST" >/dev/null
    info "docker login $REG_HOST (credentials from environment)"
  fi
  for variant in $VARIANTS; do
    for arch in $ARCHES; do
      docker push "${IMG_PUSH_PREFIX}:${VER}-${variant}-${arch}" >/dev/null
    done
  done
  for variant in $VARIANTS; do
    children=""
    for arch in $ARCHES; do
      children="$children ${IMG_PUSH_PREFIX}:${VER}-${variant}-${arch}"
    done
    # shellcheck disable=SC2086
    push_manifest_tag "${IMG_PUSH_PREFIX}:${VER}-${variant}" $children
    info "manifest pushed: ${IMG_PUSH_PREFIX}:${VER}-${variant}"
    if [ "$ALSO_LATEST" = 1 ]; then
      # same children as the versioned list — per-arch tags are not re-pushed
      # shellcheck disable=SC2086
      push_manifest_tag "${IMG_PUSH_PREFIX}:latest-${variant}" $children
      info "manifest pushed: ${IMG_PUSH_PREFIX}:latest-${variant}"
    fi
  done
  docker logout "$REG_HOST" >/dev/null 2>&1 || true
else
  log "phase 5/6: push skipped (PUSH=0 / REGISTRY empty) — local tags only"
fi

# ---- phase 6: verification + smoke -------------------------------------------

if [ "$VERIFY" = 1 ]; then
  log "phase 6/6: verification"
  # (a) local image config architecture fields — always available.
  for variant in $VARIANTS; do
    for arch in $ARCHES; do
      info "local config field: binflow:${VER}-${variant}-${arch} -> $(docker image inspect "binflow:${VER}-${variant}-${arch}" --format 'os={{.Os}} architecture={{.Architecture}}')"
    done
  done
  if [ "$PUSH" = 1 ]; then
    # (b) registry manifest list carries every linux/<arch> entry.
    for variant in $VARIANTS; do
      mref="${IMG_PUSH_PREFIX}:${VER}-${variant}"
      flags="$(reg_flags "$mref")"
      # shellcheck disable=SC2086 # flags word-splits by design
      docker manifest inspect $flags "$mref" > "$WORK/manifest-$variant.json"
      info "docker manifest inspect ${mref}:"
      # shellcheck disable=SC2086
      python3 - "$WORK/manifest-$variant.json" $ARCHES <<'PY' | sed 's/^/    /'
import json, sys
idx = json.load(open(sys.argv[1]))
want = sys.argv[2:]
got = {}
for m in idx.get("manifests", []):
    p = m.get("platform") or {}
    if p.get("os") == "linux" and p.get("architecture") in want:
        got[p["architecture"]] = m["digest"]
missing = [a for a in want if a not in got]
if missing:
    print("FAIL: manifest lacks " + ", ".join("linux/" + a for a in missing))
    sys.exit(1)
for a in want:
    print("PASS: linux/%s -> %s" % (a, got[a]))
PY
      # (c) each per-arch child's image config architecture field, read from
      # the registry blob plane (the AC's field-level assertion).
      for arch in $ARCHES; do
        cref="${IMG_PUSH_PREFIX}:${VER}-${variant}-${arch}"
        cflags="$(reg_flags "$cref")"
        # shellcheck disable=SC2086 # cflags word-splits by design
        docker manifest inspect $cflags "$cref" > "$WORK/child-$variant-$arch.json"
        cdgst="$(py_config_digest "$WORK/child-$variant-$arch.json")"
        name="${REG_REPO_PATH:+$REG_REPO_PATH/}$IMG_NAME"
        auth=()
        if [ -n "${DOCKER_USER:-}" ]; then auth=(-u "$DOCKER_USER:$DOCKER_PASS"); fi
        # shellcheck disable=SC2068,SC2086
        got_fields="$(curl -fsS ${auth[@]+"${auth[@]}"} "$REG_SCHEME://$REG_HOST/v2/$name/blobs/$cdgst" \
          | python3 -c 'import json,sys; c=json.load(sys.stdin); print("os=%s architecture=%s" % (c.get("os"), c.get("architecture")))')" \
          || die "config blob fetch/parse failed for $cref"
        if [ "$got_fields" = "os=linux architecture=$arch" ]; then
          info "registry config field: $cref -> $got_fields"
        else
          die "config field mismatch for $cref: $got_fields (expected linux/$arch)"
        fi
      done
    done
  fi
fi

if [ "$SMOKE" = 1 ]; then
  log "smoke: amd64 run + arm64 qemu probe"
  run_smoke() { # $1 image, $2 platform, $3 port, $4 container name
    local img="$1" plat="$2" port="$3" name="$4" ok=0 _
    docker rm -f "$name" >/dev/null 2>&1 || true
    docker run -d --name "$name" --platform "$plat" -p "$port:8080" "$img" >/dev/null
    for _ in $(seq 1 30); do
      if curl -sf "http://$SMOKE_ADDR:$port/readyz" >/dev/null 2>&1; then ok=1; break; fi
      sleep 2
    done
    if [ "$ok" != 1 ]; then
      docker logs "$name" 2>&1 | tail -5 || true
      docker rm -f "$name" >/dev/null 2>&1 || true
      return 1
    fi
    info "readyz:  $(curl -s "http://$SMOKE_ADDR:$port/readyz")"
    info "version: $(curl -s "http://$SMOKE_ADDR:$port/binflow/api/system/version")"
    docker rm -f "$name" >/dev/null
  }
  for variant in $VARIANTS; do
    info "amd64 leg: binflow:${VER}-${variant}-amd64 (native)"
    run_smoke "binflow:${VER}-${variant}-amd64" linux/amd64 "$SMOKE_PORT" "t270-smoke-$variant" \
      || die "amd64 smoke failed for $variant"
    if docker run --platform linux/arm64 --rm "binflow:${VER}-${variant}-arm64" --version \
        > "$WORK/qemu-$variant.out" 2> "$WORK/qemu-$variant.err"; then
      info "arm64 qemu-user FEASIBLE on this host: $(cat "$WORK/qemu-$variant.out")"
      run_smoke "binflow:${VER}-${variant}-arm64" linux/arm64 "$((SMOKE_PORT + 1))" "t270-smoke-$variant-arm" \
        || die "arm64 qemu smoke failed for $variant (probe passed, run leg failed)"
    else
      info "arm64 qemu-user NOT feasible on this host: $(tail -1 "$WORK/qemu-$variant.err")"
      info "arm64 verification carries on the manifest + config architecture fields (M9 PRD FR-83-AC2)."
    fi
  done
fi

if [ "$PULL_BACK" = 1 ]; then
  log "pull-back: canonical local names for the offline-bundle face"
  for variant in $VARIANTS; do
    if docker pull "${IMG_PUSH_PREFIX}:${VER}-${variant}" >/dev/null 2>&1; then
      docker tag "${IMG_PUSH_PREFIX}:${VER}-${variant}" "binflow:${VER}-${variant}"
      info "binflow:${VER}-${variant} <- ${IMG_PUSH_PREFIX}:${VER}-${variant}"
    else
      info "pull-back of ${IMG_PUSH_PREFIX}:${VER}-${variant} failed (classic image store? amd64 fallback below)"
      docker pull --platform linux/amd64 "${IMG_PUSH_PREFIX}:${VER}-${variant}-amd64" >/dev/null 2>&1 \
        && docker tag "${IMG_PUSH_PREFIX}:${VER}-${variant}-amd64" "binflow:${VER}-${variant}" || true
    fi
  done
fi

log "build complete in $((SECONDS - T0))s"
info "local tags:  binflow:${VER}-{alpine,distroless}-{amd64,arm64}"
[ -n "$IMG_PUSH_PREFIX" ] && info "manifests:   ${IMG_PUSH_PREFIX}:${VER}-{alpine,distroless}"
info "workdir kept for evidence: $WORK"
