#!/bin/bash
# L018-1 fix-up pass — redo misfired sub-requests INSIDE existing arms (no new
# arms): A pkgmeta (empty q -> 400 on A, pid chain broke), A single-rev delete
# re-discriminator (8s settle vs async no-op), cross-side artifact fetch
# (conanfile.py / conanmanifest.txt / conan_package.tgz sha256) + B re-upload
# (its recipe was deleted in v2-11 + repo torn down).
# Usage: fixup.sh <a|b>   Env: ARTI_AUTH / BF_AUTH
set -u
HERE=/Users/lzw/dev-center/tools/difftest/l018
FIX=$HERE/fixtures
WIRE=/Users/lzw/dev-center/reports/compatibility/l018-wire
CONAN2=${CONAN2:-/tmp/l018venv/bin/conan}
SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then HOSTPORT=8082; PREFIX='artifactory/api/conan'; AUTHVAR=ARTI_AUTH; ADMINAPI=http://localhost:8082/artifactory/api
else HOSTPORT=8083; PREFIX='binflow'; AUTHVAR=BF_AUTH; ADMINAPI=http://localhost:8083/binflow/api; fi
AUTH=${!AUTHVAR:-}; [ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
USER=${AUTH%%:*}; PW=${AUTH#*:}
BASE=http://localhost:$HOSTPORT; CBASE=$BASE/$PREFIX/l018-conan
OUT=$WIRE/$SIDE; WS=$HERE/ws-$SIDE
CURL=(curl -sS --noproxy '*' -u "$AUTH")
req() { local id=$1 method=$2 url=$3; shift 3
  "${CURL[@]}" -X "$method" -D "$OUT/conan/$id.hdr" -o "$OUT/conan/$id.body" -w "$id %{http_code} %{size_download}B\n" "$url" "$@"; }

if [ "$SIDE" = b ]; then  # repo was torn down in the main pass — recreate
  printf '{"rclass":"local","packageType":"conan"}' > "$OUT/setup/local.json"
  req fix-b-repo-put PUT "$ADMINAPI/repositories/l018-conan" -H 'Content-Type: application/json' -T "$OUT/setup/local.json"
  sleep 2
fi

# re-upload r1 via conan 2 CLI (fresh proxy leg so the wire stays complete)
mkdir -p "$OUT/client/wire"; : > "$OUT/client/wire/fix-$SIDE-upload.wire"
UPSTREAM="localhost:$HOSTPORT" python3 "$HERE/wireproxy.py" 19018 "$OUT/client/wire" "fix-$SIDE-upload" &
px=$!; sleep 1
CONAN_HOME="$WS-conanhome" PROXYURL="http://127.0.0.1:19018/$PREFIX/l018-conan" \
  sh -c 'C="'"$CONAN2"'"; cd "'"$FIX"'" && "$C" create . --name=hello18 --version=1.0 --user=l018 --channel=stable && "$C" upload "hello18/1.0@l018/stable" -r l018 --confirm' \
  > "$OUT/client/fix-$SIDE-upload.log" 2>&1
echo "exit=$?" >> "$OUT/client/fix-$SIDE-upload.log"
kill $px 2>/dev/null; wait $px 2>/dev/null
sed -i '' -e "s/$PW/REDACTED-PW/g" "$OUT/client/fix-$SIDE-upload.log"
sleep 2

RREV=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["revision"])' "$OUT/conan/fix-$SIDE-latest.body" 2>/dev/null)
req "fix-$SIDE-latest" GET "$CBASE/v2/conans/hello18/1.0/l018/stable/latest"
RREV=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["revision"])' "$OUT/conan/fix-$SIDE-latest.body")

# pkgmeta q variants — A rejected empty q (400 "Unexpected query syntax:")
req "fix-$SIDE-pkgmeta-qref" GET "$CBASE/v2/conans/hello18/1.0/l018/stable/search?q=hello18/1.0@l018/stable"
req "fix-$SIDE-pkgmeta-qstar" GET "$CBASE/v2/conans/hello18/1.0/l018/stable/search?q=*"

PID=82339cc4d6db7990c1830d274cd12e7c91ab18a1   # client-computed, cross-instance (proven)
req "fix-$SIDE-pkg-latest"    GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/packages/$PID/latest"
req "fix-$SIDE-pkg-revisions" GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/packages/$PID/revisions"
PREV=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["revisions"][0]["revision"])' "$OUT/conan/fix-$SIDE-pkg-revisions.body" 2>/dev/null)
req "fix-$SIDE-recipe-files"  GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/files"
req "fix-$SIDE-conanfile"     GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/files/conanfile.py"
req "fix-$SIDE-manifest"      GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/files/conanmanifest.txt"
req "fix-$SIDE-pkg-files"     GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/packages/$PID/revisions/$PREV/files"
req "fix-$SIDE-pkg-tgz"       GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/packages/$PID/revisions/$PREV/files/conan_package.tgz"
sha256sum() { shasum -a 256 "$1" | cut -d' ' -f1; }
echo "[$SIDE] conanfile.py  $(sha256sum "$OUT/conan/fix-$SIDE-conanfile.body")" | tee -a "$WIRE/fix-sha256.txt"
echo "[$SIDE] manifest      $(sha256sum "$OUT/conan/fix-$SIDE-manifest.body")" | tee -a "$WIRE/fix-sha256.txt"
echo "[$SIDE] pkg.tgz       $(sha256sum "$OUT/conan/fix-$SIDE-pkg-tgz.body")" | tee -a "$WIRE/fix-sha256.txt"
if [ "$SIDE" = a ]; then
  echo "[$SIDE] manifest content:"; cat "$OUT/conan/fix-$SIDE-manifest.body" | tee -a "$WIRE/fix-sha256.txt"
fi

if [ "$SIDE" = a ]; then  # single-rev delete re-discriminator: 8s settle
  req fix-a-del-single DELETE "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/91eaead8ee2bd3f4c9f88021aa40bd82"
  echo "  sleeping 8s (async-index discriminator)"; sleep 8
  req fix-a-rev-after-8s GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions"
fi

# leave namespaces empty: delete recipe, then teardown repos on both sides
req "fix-$SIDE-del-recipe" DELETE "$CBASE/v2/conans/hello18/1.0/l018/stable"
req "fix-$SIDE-teardown-virt"  DELETE "$ADMINAPI/repositories/l018-conan-virt?deleteContent=true"
req "fix-$SIDE-teardown-local" DELETE "$ADMINAPI/repositories/l018-conan?deleteContent=true"
echo "fixup done side=$SIDE"
