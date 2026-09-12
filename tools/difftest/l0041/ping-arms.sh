#!/usr/bin/env bash
# L004-1 differential replay: /v2/ ping refused-credential message arms.
# bad-basic / unknown-bearer / expired-bearer (product API minted,
# expires_in=5) / revoked-bearer (mint then revoke) + valid + anonymous
# controls. Verbatim header+body capture per arm.
#   HOST=... BASIC='admin:***' APIBASE=/binflow (or /artifactory) OUT=... ./ping-arms.sh
set -euo pipefail
: "${HOST:?}" "${BASIC:?}" "${APIBASE:?}" "${OUT:?}"
mkdir -p "$OUT"

arm() { # id desc [curl-args...]
  local id="$1" desc="$2"; shift 2
  local code
  code=$(curl -s --noproxy '*' -m 20 -D "$OUT/$id.h" -o "$OUT/$id.b" \
    -w '%{http_code}' "$@" "$HOST/v2/") || code=CURL_ERR
  {
    echo "arm: $id  ($desc)"
    echo "  status: $code"
    echo "  www-authenticate: $(awk 'tolower($1)=="www-authenticate:"{sub(/\r$/,"");print substr($0,0,120)}' "$OUT/$id.h")"
    echo "  content-type: $(awk 'tolower($1)=="content-type:"{sub(/\r$/,"");print $2}' "$OUT/$id.h")"
    echo "  api-version-hdr: $(awk 'tolower($1)=="docker-distribution-api-version:"{sub(/\r$/,"");print $2}' "$OUT/$id.h")"
    echo "  body:"
    sed 's/^/    /' "$OUT/$id.b"
  } | tee -a "$OUT/ping.txt"
}

mint() { # expires_in -> prints token (product security token API; the
         # reference refuses a mint without an explicit scope — "Insufficient
         # scope: ''" — so the canonical user scope rides every mint)
  local exp="$1"
  curl -s --noproxy '*' -m 20 -u "$BASIC" \
    -d "grant_type=client_credentials&username=${BASIC%%:*}&scope=applied-permissions/user&expires_in=$exp" \
    "$HOST$APIBASE/api/security/token" \
    | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("access_token") or d.get("token") or "")'
}
revoke() { # token -> http code
  curl -s --noproxy '*' -m 20 -u "$BASIC" -d "token=$1" \
    -o /dev/null -w '%{http_code}' "$HOST$APIBASE/api/security/token/revoke"
}

# docker-issued token (the registry's own /v2/token endpoint) for the valid control
DTOK=$(curl -s --noproxy '*' -m 20 -u "$BASIC" \
  "$HOST/v2/token?service=${HOST#http://}&scope=repository:nonexistent/x:pull&account=${BASIC%%:*}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))')

arm P_anon "anonymous ping (challenge control)"
arm P0d "valid docker-issued bearer" -H "Authorization: Bearer $DTOK"
STOK=$(mint 3600)
arm P0s "valid security-API bearer" -H "Authorization: Bearer $STOK"
arm P1 "bad basic" -u "${BASIC%%:*}:definitely-wrong-password"
arm P2 "unknown bearer (garbage)" -H "Authorization: Bearer lzw-no-such-token-0123456789abcdef"
ETOK=$(mint 5)
echo "minted expiring token (expires_in=5), waiting 7s..." >&2
sleep 7
arm P3 "expired bearer" -H "Authorization: Bearer $ETOK"
RTOK=$(mint 86400)   # the reference only revokes tokens with >6h of life left
arm P4a "revocation control (valid)" -H "Authorization: Bearer $RTOK"
RC=$(revoke "$RTOK")
echo "revoke call: $RC" | tee -a "$OUT/ping.txt"
arm P4b "revoked bearer" -H "Authorization: Bearer $RTOK"
echo "ping arms complete"
