#!/usr/bin/env bash
# wave1-rerun13c — provision the deterministic schema2 smoke image into the
# A-side (JFrog 7.161.26) LOCAL docker repo `difftest-docker` (image `smoke`,
# tags t1/t2). Byte-for-byte the same fixture as the upstream-push segment of
# docker_wave_setup.sh (layer 64x'A' + config + manifest; manifest sha256
# 95301964… must reproduce — asserted below).
#
# Credentials enter ONLY via the gitignored .env.ref file (parsed in-shell,
# never printed, never written anywhere).
#
# OCI DELETE on this face may 405 — cleanup is out of scope; residue in
# difftest-docker is declared test residue (conductor-approved).
set -eu

A_HOST=http://192.168.120.36:32082
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref
A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
[ -n "$A_USER" ] && [ -n "$A_PASSWORD" ] || { echo "ENV MISSING from .env.ref"; exit 2; }

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT

# ---- fixture bytes: verbatim from docker_wave_setup.sh upstream segment ----
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
MANIFEST_SHA="$(shasum -a 256 "$W/manifest.json" | cut -d' ' -f1)"

echo "local fixture digests:"
echo "  layer    sha256:$LAYER_SHA"
echo "  config   sha256:$CFG_SHA"
echo "  manifest sha256:$MANIFEST_SHA"
case "$LAYER_SHA" in  d53eda7a*) ;; *) echo "MISMATCH: layer digest != d53eda7a…"; exit 3;; esac
case "$CFG_SHA" in    481acf9f*) ;; *) echo "MISMATCH: config digest != 481acf9f…"; exit 3;; esac
case "$MANIFEST_SHA" in 95301964901114259e985357c982f41e97657f38b36e0b5c65de8b3c6be4b558) ;;
  *) echo "MISMATCH: manifest digest != 9530196490…"; exit 3;; esac
echo "digest table matches wave-1 fixture (d53eda7a / 481acf9f / 95301964…) OK"

# ---- push into A local repo difftest-docker, image smoke (admin Basic) ----
# %2F form first (repo=image first segment encoded); fall back to slash form.
BASEV2_TRY=("/v2/difftest-docker%2Fsmoke" "/v2/difftest-docker/smoke")
V2=""
for cand in "${BASEV2_TRY[@]}"; do
  code="$(curl -s -o /dev/null -w '%{http_code}' -u "$A_USER:$A_PASSWORD" \
    -X POST "$A_HOST$cand/blobs/uploads/")"
  echo "POST $cand/blobs/uploads/ -> $code"
  if [ "$code" = "202" ] || [ "$code" = "200" ]; then V2="$cand"; break; fi
done
[ -n "$V2" ] || { echo "no working v2 base path on A for $IMG"; exit 4; }
echo "using base path: $V2"

upload_blob() { # file digest
  loc="$(curl -s -D - -o /dev/null -u "$A_USER:$A_PASSWORD" \
    -X POST "$A_HOST$V2/blobs/uploads/" \
    | tr -d '\r' | grep -i '^location:' | sed 's/^[Ll]ocation: *//')"
  [ -n "$loc" ] || { echo "no Location header on blob upload POST"; exit 5; }
  case "$loc" in
    http*) url="$loc" ;;
    /*)    url="$A_HOST$loc" ;;
    *)     url="$A_HOST/$loc" ;;
  esac
  case "$url" in
    *\?*) url="$url&digest=$2" ;;
    *)    url="$url?digest=$2" ;;
  esac
  code="$(curl -s -o /dev/null -w '%{http_code}' -u "$A_USER:$A_PASSWORD" \
    -X PUT -H 'Content-Type: application/octet-stream' --data-binary "@$1" "$url")"
  echo "blob $2 -> $code"
  [ "$code" = "201" ] || { echo "blob PUT not 201"; exit 6; }
}
upload_blob "$W/layer.bin"   "sha256:$LAYER_SHA"
upload_blob "$W/config.json" "sha256:$CFG_SHA"

for tag in t1 t2; do
  code="$(curl -s -o /dev/null -w '%{http_code}' -u "$A_USER:$A_PASSWORD" \
    -X PUT -H 'Content-Type: application/vnd.docker.distribution.manifest.v2+json' \
    --data-binary "@$W/manifest.json" "$A_HOST$V2/manifests/$tag")"
  echo "manifest $tag -> $code"
  [ "$code" = "201" ] || { echo "manifest PUT $tag not 201"; exit 7; }
done

# ---- post-push assertions: HEAD manifest by tag (t1/t2) and by digest ----
for tag in t1 t2; do
  out="$(curl -s -I -u "$A_USER:$A_PASSWORD" \
    -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
    "$A_HOST$V2/manifests/$tag")"
  code="$(printf '%s' "$out" | head -1 | awk '{print $2}')"
  dcd="$(printf '%s' "$out" | tr -d '\r' | grep -i '^docker-content-digest:' | awk '{print $2}')"
  echo "HEAD manifest $tag -> $code dcd=$dcd"
  [ "$code" = "200" ] || { echo "ASSERT FAIL: HEAD manifest $tag != 200"; exit 8; }
  [ "$dcd" = "sha256:$MANIFEST_SHA" ] || { echo "ASSERT FAIL: dcd mismatch"; exit 9; }
done
hd="$(curl -s -o /dev/null -w '%{http_code}' -I -u "$A_USER:$A_PASSWORD" \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
  "$A_HOST$V2/manifests/sha256:$MANIFEST_SHA")"
echo "HEAD manifest by digest -> $hd"
[ "$hd" = "200" ] || { echo "ASSERT FAIL: HEAD by digest != 200"; exit 10; }

echo "PROVISION DONE: A difftest-docker/smoke tags t1,t2 verified (200 + digest match)"
