#!/usr/bin/env bash
# docker-remote replay wave — fixture provisioning (idempotent, dual-path API).
#
# Provisions on BOTH sides:
#   - remote repo  diff-docker-remote  (upstream = DIFFTEST_DOCKER_UPSTREAM,
#     private HTTP; B side needs allowPrivateUpstream=true — A 7.161.26 has no
#     such field and accepts private HTTP upstreams by default; BinFlow's
#     runtime refuses pull-through with 400 "private or suppressed upstream"
#     until the flag is set. Config-face divergence recorded in the wave report.)
#   - schema2 image difftest-docker/smoke tags t1,t2 on the upstream registry
#     (deterministic bytes; blobs/manifest pushed anonymously).
# Plus, on the A side only (JFrog reference; conductor-mandated 2026-10-28):
#   - local repo difftest-docker (docker rclass local; created ONLY when absent)
#   - the same smoke image pushed with admin Basic auth against the A-side
#     /v2/difftest-docker/ face, then HEAD-asserted 200 with exact
#     Docker-Content-Digest. B-side local provisioning is deliberately out of
#     scope until a local-face replay batch needs it.
#
# API-face dual compatibility (2026-10-28 fix; root cause of the 404 in the
# previous revision): the repo-config face moved — JFrog 7.161.26 answers 404
# on the root alias /api/* and only serves /artifactory/api/*, while BinFlow
# serves /binflow/api/*. BASE may be either the bare host root or already
# carry the context prefix, so the working API prefix is PROBED per side:
# candidates "", /artifactory, /binflow against GET <cand>/api/repositories,
# first 200 wins, 404 -> next candidate, anything else -> abort loudly.
# No hardcoded ports or hosts.
#
# Idempotency: existing repos are detected (GET config 200 -> skip create);
# blob uploads HEAD-check the digest first (200 -> skip); manifest PUTs are
# naturally repeatable (200/201 accepted); every push is followed by an
# explicit HEAD verification. Re-running the script end-to-end is all-green.
#
# Credentials and endpoints enter ONLY via the six diff env vars
# (A_BASE/B_BASE/A_USER/A_PASSWORD/B_USER/B_PASSWORD) + DIFFTEST_DOCKER_UPSTREAM.
# Nothing here prints secret values.
set -eu

: "${A_BASE:?}" "${B_BASE:?}" "${A_USER:?}" "${A_PASSWORD:?}" "${B_USER:?}" "${B_PASSWORD:?}"
: "${DIFFTEST_DOCKER_UPSTREAM:?}"   # e.g. http://192.168.120.36:32100

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT

# ---------------------------------------------------------------- helpers

# auth-aware curl: injects `-u user:pass` only when USER is non-empty.
# usage: acurl USER PASS [curl args...]
acurl() {
  local u="$1" p="$2"; shift 2
  if [ -n "$u" ]; then curl -s -u "$u:$p" "$@"; else curl -s "$@"; fi
}

# host_root BASE -> scheme://host:port (strips any context path).
host_root() { printf '%s' "$1" | sed -E 's#^(https?://[^/]+).*#\1#'; }

# resolve_api LABEL BASE USER PASS -> echoes the working API base
# ("<base>/[ctx]/api"). Probe trail goes to stderr; secrets never printed.
resolve_api() {
  local label="$1" base="$2" user="$3" pass="$4" ctx cand code
  base="${base%/}"
  for ctx in "" /artifactory /binflow; do
    case "$base" in
      *"$ctx") [ -n "$ctx" ] && continue ;;   # BASE already carries this ctx
    esac
    cand="$base$ctx/api"
    code="$(curl -s -o /dev/null -w '%{http_code}' -u "$user:$pass" "$cand/repositories")"
    echo "$label probe $cand/repositories -> $code" >&2
    if [ "$code" = "200" ]; then printf '%s' "$cand"; return 0; fi
    if [ "$code" != "404" ]; then
      echo "$label FATAL: $cand/repositories -> $code (not 404; auth/server trouble)" >&2
      return 1
    fi
  done
  echo "$label FATAL: no working API face under $base (tried '', /artifactory, /binflow)" >&2
  return 1
}

# ensure_repo LABEL API_BASE KEY USER PASS CONFIG_JSON
# GET config first: 200 -> skip create (existing repo untouched); 404 ->
# PUT create (200/201); anything else -> abort. Re-GET must end 200.
ensure_repo() {
  local label="$1" api="$2" key="$3" user="$4" pass="$5" cfg="$6" code ccode vcode
  code="$(curl -s -o "$W/$label.$key.cfg" -w '%{http_code}' -u "$user:$pass" "$api/repositories/$key")"
  if [ "$code" = "200" ]; then
    echo "$label repo $key: exists (GET 200) -> skip create"
  elif [ "$code" = "404" ]; then
    ccode="$(curl -s -o "$W/$label.$key.create" -w '%{http_code}' -u "$user:$pass" \
      -X PUT "$api/repositories/$key" -H 'Content-Type: application/json' -d "$cfg")"
    echo "$label create repo $key: PUT -> $ccode $(head -c 120 "$W/$label.$key.create")"
    case "$ccode" in 200|201) ;; *) echo "$label FATAL: repo create $key not 200/201"; return 1;; esac
  else
    echo "$label FATAL: GET repo $key -> $code"; return 1
  fi
  vcode="$(curl -s -o /dev/null -w '%{http_code}' -u "$user:$pass" "$api/repositories/$key")"
  echo "$label repo $key: verify GET -> $vcode"
  [ "$vcode" = "200" ] || { echo "$label FATAL: repo $key missing after ensure"; return 1; }
}

# push_blob FILE DIGEST V2BASE USER PASS  (USER empty -> anonymous)
# HEAD by digest first (200 -> skip); else POST/PUT upload; then HEAD must 200.
push_blob() {
  local file="$1" digest="$2" v2="$3" user="$4" pass="$5"
  local hcode loc url pcode vcode host
  host="$(host_root "$v2")"
  hcode="$(acurl "$user" "$pass" -o /dev/null -w '%{http_code}' -I "$v2/blobs/sha256:$digest")"
  if [ "$hcode" = "200" ]; then
    echo "blob sha256:$digest -> SKIP (HEAD 200, already present)"
    return 0
  fi
  loc="$(acurl "$user" "$pass" -D - -o /dev/null -X POST "$v2/blobs/uploads/" \
    | tr -d '\r' | grep -i '^location:' | sed 's/^[Ll]ocation: *//' || true)"
  [ -n "$loc" ] || { echo "FATAL: no Location header on blob POST $v2/blobs/uploads/"; return 1; }
  case "$loc" in
    http*) url="$loc" ;;
    /*)    url="$host$loc" ;;
    *)     url="$host/$loc" ;;
  esac
  case "$url" in
    *\?*) url="$url&digest=sha256:$digest" ;;
    *)    url="$url?digest=sha256:$digest" ;;
  esac
  pcode="$(acurl "$user" "$pass" -o /dev/null -w '%{http_code}' \
    -X PUT -H 'Content-Type: application/octet-stream' --data-binary "@$file" "$url")"
  echo "blob sha256:$digest -> PUT $pcode"
  case "$pcode" in 200|201) ;; *) echo "FATAL: blob PUT sha256:$digest not 200/201"; return 1;; esac
  vcode="$(acurl "$user" "$pass" -o /dev/null -w '%{http_code}' -I "$v2/blobs/sha256:$digest")"
  echo "blob sha256:$digest -> verify HEAD $vcode"
  [ "$vcode" = "200" ] || { echo "FATAL: blob HEAD after push != 200 (sha256:$digest)"; return 1; }
}

# push_manifest FILE TAG V2BASE USER PASS  (200/201 accepted -> repeatable)
push_manifest() {
  local file="$1" tag="$2" v2="$3" user="$4" pass="$5" mcode
  mcode="$(acurl "$user" "$pass" -o /dev/null -w '%{http_code}' \
    -X PUT -H 'Content-Type: application/vnd.docker.distribution.manifest.v2+json' \
    --data-binary "@$file" "$v2/manifests/$tag")"
  echo "manifest $tag -> PUT $mcode"
  case "$mcode" in 200|201) ;; *) echo "FATAL: manifest PUT $tag not 200/201"; return 1;; esac
}

# assert_manifest_head TAG V2BASE USER PASS LABEL REQUIRE_DCD(0|1)
# HEAD with manifest Accept; 200 required; when REQUIRE_DCD=1 the
# Docker-Content-Digest must equal sha256:WANT_DIGEST (global).
assert_manifest_head() {
  local tag="$1" v2="$2" user="$3" pass="$4" label="$5" req_dcd="$6" out code dcd
  out="$(acurl "$user" "$pass" -I -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
    "$v2/manifests/$tag")"
  code="$(printf '%s' "$out" | head -1 | awk '{print $2}')"
  dcd="$(printf '%s' "$out" | tr -d '\r' | grep -i '^docker-content-digest:' | awk '{print $2}')"
  echo "$label HEAD manifest $tag -> $code dcd=${dcd:-<none>}"
  [ "$code" = "200" ] || { echo "FATAL: $label HEAD manifest $tag != 200"; return 1; }
  if [ "$req_dcd" = "1" ]; then
    [ -n "$dcd" ] || { echo "FATAL: $label HEAD manifest $tag missing Docker-Content-Digest"; return 1; }
    [ "$dcd" = "sha256:$WANT_MANIFEST_SHA" ] || \
      { echo "FATAL: $label digest mismatch: $dcd != sha256:$WANT_MANIFEST_SHA"; return 1; }
  fi
}

# ------------------------------------------------- fixture bytes (deterministic)
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
WANT_MANIFEST_SHA="$MANIFEST_SHA"

echo "local fixture digests:"
echo "  layer    sha256:$LAYER_SHA"
echo "  config   sha256:$CFG_SHA"
echo "  manifest sha256:$MANIFEST_SHA"
case "$LAYER_SHA" in d53eda7a*) ;; *) echo "MISMATCH: layer digest != d53eda7a…"; exit 3;; esac
case "$CFG_SHA" in   481acf9f*) ;; *) echo "MISMATCH: config digest != 481acf9f…"; exit 3;; esac
case "$MANIFEST_SHA" in
  95301964901114259e985357c982f41e97657f38b36e0b5c65de8b3c6be4b558) ;;
  *) echo "MISMATCH: manifest digest != 9530196490…"; exit 3;;
esac
echo "fixture digest table matches wave-1 (d53eda7a / 481acf9f / 9530196490…) OK"

# ---------------------------------------------- resolve API faces (adaptive)
A_API="$(resolve_api A "$A_BASE" "$A_USER" "$A_PASSWORD")"
B_API="$(resolve_api B "$B_BASE" "$B_USER" "$B_PASSWORD")"
echo "A api base: $A_API"
echo "B api base: $B_API"

# ------------------------------- remote repo diff-docker-remote on both sides
repo_config='{"key":"diff-docker-remote","rclass":"remote","packageType":"docker","url":"'"$DIFFTEST_DOCKER_UPSTREAM"'","storeArtifactsLocally":true}'
b_repo_config="${repo_config%?},\"allowPrivateUpstream\":true}"
ensure_repo A "$A_API" diff-docker-remote "$A_USER" "$A_PASSWORD" "$repo_config"
ensure_repo B "$B_API" diff-docker-remote "$B_USER" "$B_PASSWORD" "$b_repo_config"

# --------------------- fixture image on the upstream registry (anonymous push)
UP="$DIFFTEST_DOCKER_UPSTREAM/v2/$IMG"
push_blob "$W/layer.bin"   "$LAYER_SHA" "$UP" "" ""
push_blob "$W/config.json" "$CFG_SHA"   "$UP" "" ""
for tag in t1 t2; do push_manifest "$W/manifest.json" "$tag" "$UP" "" ""; done
assert_manifest_head t1 "$UP" "" "" upstream 0
assert_manifest_head t2 "$UP" "" "" upstream 0

# -------------- A-side local difftest-docker repo + smoke image (admin Basic)
# Repo face: created only when absent; an existing (conductor-created) repo is
# left untouched. Image face: %2F form probed first, slash form fallback —
# both are probed against tags/list, first 200 wins (no hardcoded host/port).
ensure_repo A "$A_API" difftest-docker "$A_USER" "$A_PASSWORD" \
  '{"key":"difftest-docker","rclass":"local","packageType":"docker"}'
A_HOST="$(host_root "$A_BASE")"
AV2=""
for form in "%2F" "/"; do
  cand="$A_HOST/v2/difftest-docker${form}smoke"
  code="$(acurl "$A_USER" "$A_PASSWORD" -o /dev/null -w '%{http_code}' "$cand/tags/list")"
  echo "A v2 probe $cand/tags/list -> $code"
  if [ "$code" = "200" ]; then AV2="$cand"; break; fi
done
[ -n "$AV2" ] || { echo "FATAL: no working A-side v2 face for $IMG"; exit 4; }
echo "A v2 image face: $AV2"
push_blob "$W/layer.bin"   "$LAYER_SHA" "$AV2" "$A_USER" "$A_PASSWORD"
push_blob "$W/config.json" "$CFG_SHA"   "$AV2" "$A_USER" "$A_PASSWORD"
for tag in t1 t2; do push_manifest "$W/manifest.json" "$tag" "$AV2" "$A_USER" "$A_PASSWORD"; done
assert_manifest_head t1 "$AV2" "$A_USER" "$A_PASSWORD" A-local 1
assert_manifest_head t2 "$AV2" "$A_USER" "$A_PASSWORD" A-local 1
hd="$(acurl "$A_USER" "$A_PASSWORD" -o /dev/null -w '%{http_code}' -I \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
  "$AV2/manifests/sha256:$MANIFEST_SHA")"
echo "A-local HEAD manifest by digest -> $hd"
[ "$hd" = "200" ] || { echo "FATAL: A-local HEAD by digest != 200"; exit 5; }

echo "SETUP DONE (repos + fixture verified on upstream and A-side local face)"
