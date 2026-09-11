#!/usr/bin/env bash
# L004-1 differential replay: the EXPIRED-window arms (repo with
# retrievalCachePeriodSecs=60). Seeding fetch, one TTL sleep per arm so each
# arm observes its own expired window. §8-C's upstream-304 passthrough (the
# revalidation serve) plus the client-conditional-on-expired-copy arms.
#   HOST=... BASIC='admin:***' REPO=l0041-b-remote IMAGE=l0041/busybox TAG=t1 \
#   CFGDIGEST=sha256:... OUT=/... ./expired-matrix.sh
set -euo pipefail
: "${HOST:?}" "${BASIC:?}" "${REPO:?}" "${IMAGE:?}" "${TAG:?}" "${CFGDIGEST:?}" "${OUT:?}"
mkdir -p "$OUT"
ACCEPT='Accept: application/vnd.oci.image.index.v1+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.docker.distribution.manifest.v2+json'

up_access() { docker logs l0041-upstream 2>&1 | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ - - \['; }
up_count() { up_access | wc -l | tr -d ' '; }
norm_headers() {
  grep -viE '^(date|x-artifactory-id|x-artifactory-node-id|x-jfrog-version|x-binflow-[a-z-]*|x-request-id):' "$1" \
    | tr -d '\r' | sed 's/: */: /' | sort
}
TOK=$(curl -s --noproxy '*' -m 20 -u "$BASIC" \
  "$HOST/v2/token?service=${HOST#http://}&scope=repository:$REPO/$IMAGE:pull&account=${BASIC%%:*}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
AUTH="Authorization: Bearer $TOK"
MF="/v2/$REPO/$IMAGE/manifests/$TAG"
BL="/v2/$REPO/$IMAGE/blobs/$CFGDIGEST"

arm() { # id path [curl-args...]
  local id="$1" path="$2"; shift 2
  local before after delta code
  before=$(up_count)
  code=$(curl -s --noproxy '*' -m 30 -D "$OUT/$id.h" -o "$OUT/$id.b" \
    -w '%{http_code}' -H "$AUTH" -H "$ACCEPT" "$@" "$HOST$path") || code=CURL_ERR
  after=$(up_count)
  delta=$((after - before))
  {
    echo "arm: $id"
    echo "  status: $code  body_bytes: $(wc -c < "$OUT/$id.b" | tr -d ' ')"
    echo "  upstream_delta: $delta"
    echo "  headers_normalized:"; norm_headers "$OUT/$id.h" | sed 's/^/    /'
    if [ "$delta" -gt 0 ]; then
      echo "  upstream_lines:"; up_access | tail -"$delta" | sed 's/^/    /'
    fi
  } | tee -a "$OUT/expired.txt"
}

echo "== seed (fresh fetch of tag+config through repo B)" | tee -a "$OUT/expired.txt"
arm SEED "$MF"
arm SEEDB "$BL"
ETAG_M=$(awk 'tolower($1)=="etag:"{sub(/\r$/,"");print $2}' "$OUT/SEED.h" | tr -d '"')
ETAG_B=$(awk 'tolower($1)=="etag:"{sub(/\r$/,"");print $2}' "$OUT/SEEDB.h" | tr -d '"')
echo "client validators: manifest etag=$ETAG_M | blob etag=$ETAG_B" | tee -a "$OUT/expired.txt"

echo "== sleep 70s (TTL=60 expired)" >&2; sleep 70
arm X1 "$MF"                    # no client conditionals: the plain revalidation serve
echo "== sleep 70s" >&2; sleep 70
arm X2 "$MF" -H "If-None-Match: \"$ETAG_M\""   # client conditional ON the revalidation serve
echo "== sleep 70s" >&2; sleep 70
arm X3 "$BL" -H "If-None-Match: \"$ETAG_B\""   # expired blob + client conditional
echo "== sleep 70s" >&2; sleep 70
arm X4 "$BL"                    # expired blob plain re-serve
echo "expired matrix complete"
