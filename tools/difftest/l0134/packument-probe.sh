#!/bin/bash
# L013-4/5 packument probe — known-divergence npm/packument-latest-recompute
# (LOOP 013 diff leg: dual npm repo, DELETE dist-tags/latest -> GET packument,
#  is dist-tags.latest read-time recomputed on both sides?)
# Usage: packument-probe.sh <a|b>   a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env: ARTI_AUTH / BF_AUTH (npm auth via .npmrc ${...} env interpolation —
# creds never on argv; /tmp npmrc removed at end).
set -u
SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then
  API=http://localhost:8082/artifactory/api; NPMREG=http://localhost:8082/artifactory/api/npm/l0134-npm; AUTHVAR=ARTI_AUTH; NPMHOST=localhost:8082/artifactory
else
  API=http://localhost:8083/binflow/api;   NPMREG=http://localhost:8083/binflow/api/npm/l0134-npm; AUTHVAR=BF_AUTH;   NPMHOST=localhost:8083/binflow
fi
AUTH=${!AUTHVAR:-}
[ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
CURL=(curl -sS --noproxy '*' -u "$AUTH")
export L0134_NPM_AUTH=$(printf '%s' "$AUTH" | base64)
OUT=/Users/lzw/dev-center/reports/compatibility/l0134-wire/$SIDE/npm
W=/tmp/l0134/npm/$SIDE
mkdir -p "$OUT" "$W"
PKG=l0134-probe-pkg

# repo
printf '{"rclass":"local","packageType":"npm"}' > "$OUT/repo.json"
"${CURL[@]}" -X PUT -H 'Content-Type: application/json' -T "$OUT/repo.json" -o "$OUT/repo.out" -w "mkrepo %{http_code}\n" "$API/repositories/l0134-npm"

# npmrc (env-interpolated auth), pkg workdir
printf '//%s/api/npm/l0134-npm/:_auth=${L0134_NPM_AUTH}\nregistry=%s/\n' "$NPMHOST" "$NPMREG" > "$W/.npmrc"
mkdir -p "$W/$PKG"
ver() { printf '{"name":"%s","version":"%s","description":"l0134 packument probe"}' "$PKG" "$1" > "$W/$PKG/package.json"; }

ver 1.0.0
(cd "$W/$PKG" && npm publish --tag l0134base --userconfig "$W/.npmrc" --no-fund --no-audit --loglevel warn > "$OUT/publish-100.log" 2>&1; echo "publish 1.0.0 exit=$?")
ver 1.1.0
(cd "$W/$PKG" && npm publish --userconfig "$W/.npmrc" --no-fund --no-audit --loglevel warn > "$OUT/publish-110.log" 2>&1; echo "publish 1.1.0 exit=$?")

arm() { # arm <id> <method> <url> [data]
  local id=$1 m=$2 url=$3 data=${4:-}
  if [ -n "$data" ]; then
    "${CURL[@]}" -X "$m" -H 'Content-Type: application/json' -d "$data" -D "$OUT/$id.hdr" -o "$OUT/$id.body" -w "$id %{http_code}\n" "$url"
  else
    "${CURL[@]}" -X "$m" -D "$OUT/$id.hdr" -o "$OUT/$id.body" -w "$id %{http_code}\n" "$url"
  fi
}
tagface() { python3 -c "
import json,sys
try: d=json.load(open('$OUT/$1.body')); print('  dist-tags:', json.dumps(d.get('dist-tags', d)))
except Exception as e: print('  parse-fail:', e)"; }

arm n1-get-packument-before GET "$NPMREG/$PKG"; tagface n1-get-packument-before
arm n2-get-disttags-endpoint GET "$NPMREG/-/package/$PKG/dist-tags"; tagface n2-get-disttags-endpoint
arm n3-delete-latest DELETE "$NPMREG/-/package/$PKG/dist-tags/latest"
arm n4-get-packument-after GET "$NPMREG/$PKG"; tagface n4-get-packument-after
arm n5-get-disttags-endpoint-after GET "$NPMREG/-/package/$PKG/dist-tags"; tagface n5-get-disttags-endpoint-after
arm n6-put-latest-restore PUT "$NPMREG/-/package/$PKG/dist-tags/latest" '"1.1.0"'
arm n7-get-packument-restored GET "$NPMREG/$PKG"; tagface n7-get-packument-restored

rm -f "$W/.npmrc"; unset L0134_NPM_AUTH
echo "---- wire: $OUT ----"; ls "$OUT"
