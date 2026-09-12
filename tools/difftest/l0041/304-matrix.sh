#!/usr/bin/env bash
# L004-1 differential replay: client-conditional 304 matrix, fresh-window
# face (repo with default 21600s retrieval TTL — the copy is IN window the
# whole run). One endpoint per invocation; run serially for :8082 then :8083
# and diff the two result files. Credentials arrive via env, never persisted.
#
#   HOST=http://localhost:8082 BASIC='admin:***' REPO=l0041-a-remote \
#   IMAGE=l0041/busybox TAG=t1 CFGDIGEST=sha256:c6348fa8... \
#   OUT=/tmp/l0041/mA-art ./304-matrix.sh
#
# Upstream round-trip counting: the registry:3 access log line count before
# vs after each arm (docker logs l0041-upstream) — the run is serial so the
# delta attributes to the arm.
set -euo pipefail

: "${HOST:?}" "${BASIC:?}" "${REPO:?}" "${IMAGE:?}" "${TAG:?}" "${CFGDIGEST:?}" "${OUT:?}"
mkdir -p "$OUT"
ACCEPT='Accept: application/vnd.oci.image.index.v1+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json'

up_count() { docker logs l0041-upstream 2>&1 | grep -cE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ - - \[' || true; }

# token dance (Bearer, the real client's shape; scope carries the repo-path image)
TOK=$(curl -s --noproxy '*' -m 20 -u "$BASIC" \
  "$HOST/v2/token?service=${HOST#http://}&scope=repository:$REPO/$IMAGE:pull&account=${BASIC%%:*}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
AUTH="Authorization: Bearer $TOK"
echo "token: acquired (${#TOK} chars)" >&2

# norm_headers: normalized header fingerprint (docker-remote normalize.yaml
# rules: Date / instance-id / version headers dropped; X-Binflow-* noted as
# tolerated superset rather than compared; everything else kept verbatim)
norm_headers() {
  grep -viE '^(date|x-artifactory-id|x-artifactory-node-id|x-jfrog-version|x-binflow-[a-z-]*):' "$1" \
    | tr -d '\r' | sed 's/: */: /' | sort
}

# arm <id> <method> <path> [curl-header-args...]
arm() {
  local id="$1" method="$2" path="$3"; shift 3
  local before after delta code
  if [ "$method" = HEAD ]; then
    before=$(up_count)
    code=$(curl -s --noproxy '*' -m 30 --head -D "$OUT/$id.h" -o "$OUT/$id.b" \
      -w '%{http_code}' -H "$AUTH" -H "$ACCEPT" "$@" "$HOST$path") || code=CURL_ERR
  else
    before=$(up_count)
    code=$(curl -s --noproxy '*' -m 30 -X "$method" -D "$OUT/$id.h" -o "$OUT/$id.b" \
      -w '%{http_code}' -H "$AUTH" -H "$ACCEPT" "$@" "$HOST$path") || code=CURL_ERR
  fi
  after=$(up_count)
  delta=$((after - before))
  {
    echo "arm: $id"
    echo "  method: $method  path: $path"
    echo "  status: $code"
    echo "  body_bytes: $(wc -c < "$OUT/$id.b" | tr -d ' ')  body_sha256: $(shasum -a 256 "$OUT/$id.b" | cut -d' ' -f1)"
    echo "  upstream_delta: $delta"
    echo "  headers_normalized:"
    norm_headers "$OUT/$id.h" | sed 's/^/    /'
    echo "  binflow_superset_headers: $(grep -ciE '^x-binflow-' "$OUT/$id.h" || true)"
    if [ "$delta" -gt 0 ]; then
      echo "  upstream_lines:"
      docker logs l0041-upstream 2>&1 | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ - - \[' | tail -"$delta" | sed 's/^/    /'
    fi
  } | tee -a "$OUT/matrix.txt"
}

MF="/v2/$REPO/$IMAGE/manifests/$TAG"
BL="/v2/$REPO/$IMAGE/blobs/$CFGDIGEST"

# -- control GETs first: harvest the served validators (etag / last-modified)
arm M8 GET "$MF"
ETAG_M=$(awk 'tolower($1)=="etag:"{sub(/\r$/,"");print $2}' "$OUT/M8.h" | tr -d '"')
LMD_M=$(awk 'tolower($1)=="last-modified:"{sub(/^\r$/,"");sub(/\r$/,"");print substr($0,16)}' "$OUT/M8.h")
arm B7 GET "$BL"
ETAG_B=$(awk 'tolower($1)=="etag:"{sub(/\r$/,"");print $2}' "$OUT/B7.h" | tr -d '"')
LMD_B=$(awk 'tolower($1)=="last-modified:"{sub(/^\r$/,"");sub(/\r$/,"");print substr($0,16)}' "$OUT/B7.h")
echo "validators: manifest etag=$ETAG_M lm=$LMD_M | blob etag=$ETAG_B lm=$LMD_B" | tee -a "$OUT/matrix.txt"

# -- manifest face arms (client conditionals vs the fresh cached copy)
arm M1 GET "$MF" -H "If-None-Match: \"$ETAG_M\""                       # quoted, matching
arm M2 GET "$MF" -H "If-None-Match: $ETAG_M"                           # unquoted, matching
arm M3 GET "$MF" -H "If-Modified-Since: $LMD_M"                        # date arm, exact LM
arm M4 GET "$MF" -H "If-None-Match: \"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\"" # quoted, non-matching
arm M4b GET "$MF" -H "If-None-Match: \"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\"" -H "If-Modified-Since: $LMD_M" # quoted non-match vs date arm
arm M5 GET "$MF" -H "If-None-Match: \"$ETAG_M\"" -H "If-Modified-Since: Thu, 01 Jan 1970 00:00:00 GMT" # etag wins over stale date
arm M6 GET "$MF" -H "If-None-Match: $ETAG_M" -H "If-Modified-Since: $LMD_M"       # unquoted spelling + date arm
arm M2b GET "$MF" -H "If-None-Match: deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" # unquoted, non-matching
arm M2c GET "$MF" -H "If-None-Match: deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" -H "If-Modified-Since: $LMD_M" # unquoted non-match vs date arm
arm M7 HEAD "$MF" -H "If-None-Match: \"$ETAG_M\""                      # HEAD never conditional
MFD="/v2/$REPO/$IMAGE/manifests/sha256:1cfa4e2b09e127b9c4ed43578d3f3c18e7d44ea47b9ea98475c0cbe9086525f8"
arm M1d GET "$MFD" -H "If-None-Match: \"$ETAG_M\""                     # digest path, quoted matching
arm M2d GET "$MFD" -H "If-None-Match: $ETAG_M"                         # digest path, unquoted matching

# -- blob face arms (config blob, cached by the real pull)
arm B1 GET "$BL" -H "If-None-Match: \"$ETAG_B\""
arm B2 GET "$BL" -H "If-None-Match: $ETAG_B"
arm B3 GET "$BL" -H "If-Modified-Since: $LMD_B"
arm B4 GET "$BL" -H "If-None-Match: \"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\""
arm B5 GET "$BL" -H "If-None-Match: deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" -H "If-Modified-Since: $LMD_B" # unquoted non-match + date arm
arm B6 HEAD "$BL" -H "If-None-Match: \"$ETAG_B\""

echo "matrix complete: $OUT/matrix.txt"
