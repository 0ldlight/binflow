#!/usr/bin/env bash
# docker-remote replay wave — fixture provisioning (idempotent-ish).
#
# Provisions on BOTH sides:
#   - remote repo  diff-docker-remote  (upstream = DIFFTEST_DOCKER_UPSTREAM,
#     private HTTP; B side needs allowPrivateUpstream=true — A 7.161.26 has no
#     such field and accepts private HTTP upstreams by default; BinFlow's
#     runtime refuses pull-through with 400 "private or suppressed upstream"
#     until the flag is set. Config-face divergence recorded in the wave report.)
#   - schema2 image difftest-docker/smoke tags t1,t2 on the upstream registry
#     (deterministic bytes; blobs/manifest pushed anonymously).
#
# Credentials and endpoints enter ONLY via the six diff env vars
# (A_BASE/B_BASE/A_USER/A_PASSWORD/B_USER/B_PASSWORD) + DIFFTEST_DOCKER_UPSTREAM.
# Nothing here prints secret values.
set -eu

: "${A_BASE:?}" "${B_BASE:?}" "${A_USER:?}" "${A_PASSWORD:?}" "${B_USER:?}" "${B_PASSWORD:?}"
: "${DIFFTEST_DOCKER_UPSTREAM:?}"   # e.g. http://192.168.120.36:32100

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT

repo_config='{"key":"diff-docker-remote","rclass":"remote","packageType":"docker","url":"'"$DIFFTEST_DOCKER_UPSTREAM"'","storeArtifactsLocally":true}'
for side in a b; do
  if [ "$side" = a ]; then BASE="$A_BASE"; U="$A_USER"; P="$A_PASSWORD"; cfg="$repo_config"; else
    BASE="$B_BASE"; U="$B_USER"; P="$B_PASSWORD"
    cfg="${repo_config%?},\"allowPrivateUpstream\":true}"
  fi
  code="$(curl -s -u "$U:$P" -o "$W/$side.create" -w '%{http_code}' \
    -X PUT "$BASE/api/repositories/diff-docker-remote" \
    -H 'Content-Type: application/json' -d "$cfg")"
  echo "$side create repo: $code $(head -c 120 "$W/$side.create")"
done

# deterministic schema2 fixture image on the upstream
IMG=difftest-docker/smoke
printf 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' > "$W/layer.bin"
LAYER_SHA="$(shasum -a 256 "$W/layer.bin" | cut -d' ' -f1)"
LAYER_SIZE="$(wc -c < "$W/layer.bin" | tr -d ' ')"
printf '{"architecture":"amd64","os":"linux","created":"2026-10-28T00:00:00Z","config":{},"rootfs":{"type":"layers","diff_ids":["sha256:%s"]},"history":[{"created":"2026-10-28T00:00:00Z","created_by":"difftest wave"}]}' \
  "$LAYER_SHA" > "$W/config.json"
CFG_SHA="$(shasum -a 256 "$W/config.json" | cut -d' ' -f1)"
CFG_SIZE="$(wc -c < "$W/config.json" | tr -d ' ')"
printf '{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json","config":{"mediaType":"application/vnd.docker.container.image.v1+json","size":%s,"digest":"sha256:%s"},"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":%s,"digest":"sha256:%s"}]}' \
  "$CFG_SIZE" "$CFG_SHA" "$LAYER_SIZE" "$LAYER_SHA" > "$W/manifest.json"

upload_blob() {
  loc="$(curl -s -D - -o /dev/null -X POST "$DIFFTEST_DOCKER_UPSTREAM/v2/$IMG/blobs/uploads/" \
    | grep -i '^location:' | tr -d '\r' | sed 's/^[Ll]ocation: *//')"
  case "$loc" in
    *\?*) url="$loc&digest=$2" ;;
    *)    url="$loc?digest=$2" ;;
  esac
  curl -s -o /dev/null -w "blob $2 -> %{http_code}\n" \
    -X PUT -H 'Content-Type: application/octet-stream' --data-binary "@$1" "$url"
}
upload_blob "$W/layer.bin"   "sha256:$LAYER_SHA"
upload_blob "$W/config.json" "sha256:$CFG_SHA"
for tag in t1 t2; do
  curl -s -o /dev/null -w "manifest $tag -> %{http_code}\n" \
    -X PUT -H 'Content-Type: application/vnd.docker.distribution.manifest.v2+json' \
    --data-binary "@$W/manifest.json" "$DIFFTEST_DOCKER_UPSTREAM/v2/$IMG/manifests/$tag"
done
echo "fixture manifest sha256: $(shasum -a 256 "$W/manifest.json" | cut -d' ' -f1)"
echo "SETUP DONE"
