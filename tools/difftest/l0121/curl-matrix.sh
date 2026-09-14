#!/bin/bash
# l0121 curl endpoint-family matrix — direct wire (no npm client) on both sides.
# Usage: curl-matrix.sh <a|b>   (writes stdout; caller tees into the report dir)
# Env: ARTI_AUTH / BF_AUTH = user:password.
set -u
SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then
  BASE=http://localhost:8082/artifactory/api/npm/l0121-npm; AUTH=$ARTI_AUTH
else
  BASE=http://localhost:8083/binflow/api/npm/l0121-npm; AUTH=$BF_AUTH
fi
U=l0121-probe-pkg
S='@l0121-scope%2fprobe-pkg'
SU='@l0121-scope%2Fprobe-pkg'   # uppercase %2F variant (lu equivalence cell)

cell() { # cell <id> <desc> <method> <path> [curl-extra...]
  local id=$1 desc=$2 method=$3 path=$4; shift 4
  local out
  out=$(curl -sS --noproxy '*' -u "$AUTH" -X "$method" -H 'Content-Type: application/json' \
        -m 10 -w $'\n%{http_code}' "$BASE$path" "$@" 2>&1)
  local code=$(printf '%s\n' "$out" | tail -1)
  local body=$(printf '%s\n' "$out" | sed '$d' | tr -d '\n' | cut -c1-160)
  printf '%-4s %-42s %-9s %s\n     %s\n' "$id" "$desc" "${method}:${code}" "$path" "$body"
}

cellanon() { # cellanon <id> <desc> <method> <path> [curl-extra...] — no credentials
  local id=$1 desc=$2 method=$3 path=$4; shift 4
  local out
  out=$(curl -sS --noproxy '*' -X "$method" -H 'Content-Type: application/json' \
        -m 10 -w $'\n%{http_code}' "$BASE$path" "$@" 2>&1)
  local code=$(printf '%s\n' "$out" | tail -1)
  local body=$(printf '%s\n' "$out" | sed '$d' | tr -d '\n' | cut -c1-160)
  printf '%-4s %-42s %-9s %s\n     %s\n' "$id" "$desc" "${method}:${code}" "$path" "$body"
}

echo "======== curl matrix side=$SIDE base=$BASE"
cell m01 "GET dist-tags (authed)"            GET    "/-/package/$U/dist-tags"
cellanon m02 "GET dist-tags (ANON)"          GET    "/-/package/$U/dist-tags"
cell m03 "GET scoped %2f lowercase"          GET    "/-/package/$S/dist-tags"
cell m04 "GET scoped %2F UPPERCASE"          GET    "/-/package/$SU/dist-tags"
cell m05 "GET ghost package"                 GET    "/-/package/l0121-ghost-pkg/dist-tags"
cell m06 "PUT tag happy (1.1.0)"             PUT    "/-/package/$U/dist-tags/staging" -d '"1.1.0"'
cell m07 "PUT tag nonexistent version"       PUT    "/-/package/$U/dist-tags/novers"  -d '"9.9.9"'
cell m08 "PUT tag malformed body"            PUT    "/-/package/$U/dist-tags/badbody" -d 'not-a-json-string'
cell m09 "PUT tag JSON-object body"          PUT    "/-/package/$U/dist-tags/badobj"  -d '{"v":"1.0.0"}'
cellanon m10 "PUT tag ANON (no creds)"       PUT    "/-/package/$U/dist-tags/anonw"  -d '"1.0.0"'
cell m11 "DELETE tag happy"                  DELETE "/-/package/$U/dist-tags/staging"
cell m12 "DELETE tag again (gone)"           DELETE "/-/package/$U/dist-tags/staging"
cell m13 "PUT collection (bulk map)"         PUT    "/-/package/$U/dist-tags"        -d '{"latest":"1.1.0","curltag":"1.0.0"}'
cell m14 "POST collection (legacy bulk)"     POST   "/-/package/$U/dist-tags"        -d '{"posttag":"1.0.0"}'
cell m15 "POST single tag subpath"           POST   "/-/package/$U/dist-tags/posttag" -d '"1.0.0"'
echo "     -- K60 cross-checks:"
cell m16 "GET /-/ping"                       GET    "/-/ping"
cell m17 "GET /-/whoami (authed)"            GET    "/-/whoami"
cellanon m18 "GET /-/whoami (ANON)"          GET    "/-/whoami"
echo "     -- cleanup probe tags:"
cell m19 "DELETE curltag (from m13)"         DELETE "/-/package/$U/dist-tags/curltag"
cell m20 "DELETE posttag (if m14/15 made)"   DELETE "/-/package/$U/dist-tags/posttag"
