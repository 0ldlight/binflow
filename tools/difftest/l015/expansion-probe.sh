#!/bin/bash
# L015-4 expansion candidate probe — npm publish family vs pypi simple index.
# Differential-qa-engineer, LOOP 015 track L015-4. Evidence for third-ticket
# selection: per-domain quick probes (<=10 arms each), serial, settle 2s,
# 503 gate on the ref (crawl discipline: on 503/recurrent-failure -> wait, retry).
#
# Usage: expansion-probe.sh <a|b>   a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env:  ARTI_AUTH / BF_AUTH = "user:password" (never on disk).
# Out:  reports/compatibility/l015-probe-wire/<side>/{npm,pypi}/...
set -u
SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then
  HOSTPORT=8082; PREFIX=artifactory; AUTHVAR=ARTI_AUTH; TAPPORT=19082
else
  HOSTPORT=8083; PREFIX=binflow;   AUTHVAR=BF_AUTH;  TAPPORT=19083
fi
AUTH=${!AUTHVAR:-}
[ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }

BASE=http://localhost:$HOSTPORT/$PREFIX
NPM_API=$BASE/api/npm/l015-npm
PYPI_API=$BASE/api/pypi/l015-pypi
OUT=/Users/lzw/dev-center/reports/compatibility/l015-probe-wire/$SIDE
WS=/tmp/l015/ws-$SIDE
NPM=/tmp/l0121/npm10/node_modules/.bin/npm
TAP=/Users/lzw/dev-center/tools/difftest/l0121/wiretap.py
[ -x "$NPM" ] || NPM=$(command -v npm)
rm -rf "$OUT"; mkdir -p "$OUT/npm" "$OUT/pypi" "$WS"
CURL=(curl -sS --noproxy '*' -u "$AUTH")

# --- ref health gate: 503 or recurrent-failure body -> wait and retry ---------
req() { # req <id> <dir> <method> <url> [curl args...]
  local id=$1 dir=$2 method=$3 url=$4; shift 4; local try
  mkdir -p "$OUT/$dir"
  for try in 1 2 3 4 5; do
    "${CURL[@]}" -X "$method" -D "$OUT/$dir/$id.hdr" -o "$OUT/$dir/$id.body" \
      -w "$id %{http_code} %{size_download}B\n" "$url" "$@"
    local code=$(awk 'NR==1{sub(/\r/,"");print $2}' "$OUT/$dir/$id.hdr")
    if [ "$code" != 503 ] && ! grep -q 'recurrent request failures' "$OUT/$dir/$id.body" 2>/dev/null; then
      return 0
    fi
    echo "  [gate] ref unhealthy ($code) — waiting 15s"; sleep 15
  done
  echo "  [gate] ref still unhealthy after 5 tries"
}
settle() { sleep 2; }

# --- repos (difftest namespace l015-*) ----------------------------------------
mkdir -p "$OUT/setup"
for pair in "npm:l015-npm" "pypi:l015-pypi"; do
  pt=${pair%%:*}; key=${pair##*:}
  printf '{"rclass":"local","packageType":"%s"}' "$pt" > "$OUT/setup/repo-$key.json"
  req "repo-$key" setup PUT "$BASE/api/repositories/$key" \
      -H 'Content-Type: application/json' -T "$OUT/setup/repo-$key.json"
  settle
done

PKG=l015-probe-pkg
NPMFLAGS=(--userconfig /tmp/l015/npmrc-fake-$SIDE --cache /tmp/l015/npm-cache-$SIDE
          --registry "http://127.0.0.1:$TAPPORT/$PREFIX/api/npm/l015-npm/"
          --prefer-online --no-fund --no-audit --loglevel warn
          --fetch-retries 0 --fetch-timeout 30000)
taprun() { # taprun <case-id> <cmd...> — real client via wiretap (Basic from env)
  printf '//127.0.0.1:%s/%s/api/npm/l015-npm/:_authToken=l015-fake-client-token\n' \
    "$TAPPORT" "$PREFIX" > /tmp/l015/npmrc-fake-$SIDE
  ( cd "$WS" && TAP_CWD=$WS python3 "$TAP" --target "http://localhost:$HOSTPORT" \
      --port "$TAPPORT" --log "$OUT/npm/$1.log" --auth-env "$AUTHVAR" -- "${@:2}" )
}
setver() { mkdir -p "$WS"; printf '{"name":"%s","version":"%s","description":"l015 expansion probe"}' "$PKG" "$1" > "$WS/package.json"; }

echo "======== side=$SIDE npm publish family ========"

# N1 real npm publish 1.0.0 (wire: full PUT publish sequence)
setver 1.0.0
taprun n1-publish-100 "$NPM" "${NPMFLAGS[@]}" publish 2>&1 | tail -3
settle

# N2 packument read face (ETag/X-Checksum-Sha1, tarball rewrite, dist-tags)
req n2-packument npm GET "$NPM_API/$PKG"
req n2-packument-304 npm GET "$NPM_API/$PKG" -H "If-None-Match: $(awk 'tolower($1)=="etag:"{gsub(/\r/,"");print $2}' "$OUT/npm/n2-packument.hdr" | head -1)"
settle

# N3 rep-publish same version via real npm (expect 403 family + message)
taprun n3-republish-100 "$NPM" "${NPMFLAGS[@]}" publish 2>&1 | tail -4
settle

# N4 ghost version: versions has 9.9.9, attachment named -1.0.0.tgz (curl craft)
python3 - "$WS" "$PKG" <<'EOF'
import base64, json, sys, os
ws, pkg = sys.argv[1], sys.argv[2]
tar = base64.b64encode(f"{pkg} ghost tarball bytes".encode()).decode()
doc = {"_id": pkg, "name": pkg, "dist-tags": {"latest": "9.9.9"},
       "versions": {"9.9.9": {"name": pkg, "version": "9.9.9",
         "dist": {"tarball": f"http://localhost/{pkg}-9.9.9.tgz"}}},
       "_attachments": {f"{pkg}-1.0.0.tgz": {"content_type": "application/octet-stream",
         "data": tar, "length": len(base64.b64decode(tar))}}}
open(os.path.join(ws, "ghost.json"), "w").write(json.dumps(doc))
EOF
req n4-ghost-publish npm PUT "$NPM_API/$PKG" -H 'Content-Type: application/json' -T "$WS/ghost.json"
settle

# N5 invalid semver "1.0" with matching attachment (curl craft)
python3 - "$WS" "$PKG" <<'EOF'
import base64, json, sys, os
ws, pkg = sys.argv[1], sys.argv[2]
tar = base64.b64encode(f"{pkg} badsemver tarball bytes".encode()).decode()
doc = {"_id": pkg, "name": pkg, "dist-tags": {"latest": "1.0"},
       "versions": {"1.0": {"name": pkg, "version": "1.0",
         "dist": {"tarball": f"http://localhost/{pkg}-1.0.tgz"}}},
       "_attachments": {f"{pkg}-1.0.tgz": {"content_type": "application/octet-stream",
         "data": tar, "length": len(base64.b64decode(tar))}}}
open(os.path.join(ws, "badsemver.json"), "w").write(json.dumps(doc))
EOF
req n5-badsemver npm PUT "$NPM_API/$PKG" -H 'Content-Type: application/json' -T "$WS/badsemver.json"
settle

# N6 publish 1.1.0 (clean second version -> latest move), then single-version unpublish
setver 1.1.0
taprun n6a-publish-110 "$NPM" "${NPMFLAGS[@]}" publish 2>&1 | tail -2
settle
taprun n6b-unpublish-100 "$NPM" "${NPMFLAGS[@]}" unpublish "$PKG@1.0.0" 2>&1 | tail -4
settle

# N7 whole-package unpublish (PUT -rev rev-dance + DELETE -rev)
taprun n7-unpublish-all "$NPM" "${NPMFLAGS[@]}" unpublish --force "$PKG" 2>&1 | tail -4
settle

# N8 stale-rev PUT: re-publish 2.0.0 then PUT -rev with bogus rev (fake-200 probe)
setver 2.0.0
taprun n8a-publish-200 "$NPM" "${NPMFLAGS[@]}" publish 2>&1 | tail -2
settle
req n8b-put-rev-stale npm PUT "$NPM_API/$PKG/-rev/000-bogus-rev" -H 'Content-Type: application/json' -d '{}'
req n8c-packument-after npm GET "$NPM_API/$PKG"
settle

# N9 scoped publish (%2f path form) via real npm
mkdir -p "$WS-scope"
printf '{"name":"@l015-scope/probe-pkg","version":"1.0.0","description":"l015 scoped probe"}' > "$WS-scope/package.json"
( cd "$WS-scope" && TAP_CWD=$WS-scope python3 "$TAP" --target "http://localhost:$HOSTPORT" \
    --port "$TAPPORT" --log "$OUT/npm/n9-scoped-publish.log" --auth-env "$AUTHVAR" -- \
    "$NPM" "${NPMFLAGS[@]}" publish --access public 2>&1 | tail -3 )
settle

echo "======== side=$SIDE pypi simple index ========"

# fixture: minimal sdist + wheel bytes (content arbitrary; metadata rides form fields)
SDIST=$WS/hello15-1.0.0.tar.gz; WHEEL=$WS/hello15-1.0.0-py3-none-any.whl
mkdir -p "$WS/hello15-1.0.0"
printf 'Metadata-Version: 2.1\nName: hello15\nVersion: 1.0.0\nSummary: l015 probe\n' > "$WS/hello15-1.0.0/PKG-INFO"
tar -czf "$SDIST" -C "$WS" hello15-1.0.0
printf 'PK\x03\x04l015-dummy-wheel-bytes' > "$WHEEL"
md5_of() { md5 -q "$1"; }

# P1 simple root empty (head form)
req p1-simple-root pypi GET "$PYPI_API/simple/"
settle

# P2 upload sdist with md5_digest (twine classic form)
req p2-upload-sdist pypi POST "$PYPI_API/" \
    -F ':action=file_upload' -F "content=@$SDIST" -F "md5_digest=$(md5_of "$SDIST")" \
    -F 'filetype=sdist' -F 'protocol_version=1' -F 'name=hello15' -F 'version=1.0.0'
req p2b-simple-root-after pypi GET "$PYPI_API/simple/"
settle

# P3 package page (anchor form, #sha256=, rel=, ../../ links)
req p3-simple-pkg pypi GET "$PYPI_API/simple/hello15/"
settle

# P4 upload wheel with requires_python + no md5_digest (twine >=6.2 form)
req p4-upload-wheel pypi POST "$PYPI_API/" \
    -F ':action=file_upload' -F "content=@$WHEEL" -F 'filetype=bdist_wheel' \
    -F 'protocol_version=1' -F 'name=hello15' -F 'version=1.0.0' -F 'requires_python=>=3.8'
req p4b-simple-pkg-after pypi GET "$PYPI_API/simple/hello15/"
settle

# P5 no trailing slash -> 302
req p5-no-slash pypi GET "$PYPI_API/simple/hello15"
settle

# P6 /simple/{name}/{version} -> 404 reserved endpoint
req p6-version-path pypi GET "$PYPI_API/simple/hello15/1.0.0"
settle

# P7 wrong :action -> 400
req p7-bad-action pypi POST "$PYPI_API/" \
    -F ':action=submit' -F "content=@$SDIST" -F 'filetype=sdist'
settle

# P8 gzip negotiation
req p8-gzip pypi GET "$PYPI_API/simple/hello15/" -H 'Accept-Encoding: gzip' --compressed -o "$OUT/pypi/p8-gzip.body"
settle

# P9 ETag revalidation (304) — reuse p3 etag
req p9-etag-304 pypi GET "$PYPI_API/simple/hello15/" -H "If-None-Match: $(awk 'tolower($1)=="etag:"{gsub(/\r/,"");print $2}' "$OUT/pypi/p3-simple-pkg.hdr" | head -1)"
settle

# P10 legacy JSON API
req p10-legacy-json pypi GET "$PYPI_API/pypi/hello15/json"
settle

echo "======== side=$SIDE done — evidence in $OUT"
