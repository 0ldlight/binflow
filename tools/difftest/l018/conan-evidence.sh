#!/bin/bash
# L018-1 conan wire-forensics probe — dual-system differential, serial arms.
# Differential-qa-engineer, LOOP 018 track L018-1 (conan expansion, first ticket).
#
# Faces: v2 handshake/rev-chain/upload-download/search/delete via conan 2.31.2
#        real CLI legs (through tools/difftest/l018/wireproxy.py -> raw wire)
#        + curl direct legs; v1 classic endpoints via conan 1.66.0 real CLI +
#        curl (upload_urls/download_urls absolute-URL forms, snapshot, digest,
#        search, whole-tree delete, v1-on-virtual gate).
# Arm budget: 22 (11 v2 + 11 v1). Serial; settle 2s; 503/migration gate on ref
# (503 or "not yet migrated" -> wait 15s, retry x5).
#
# Usage: conan-evidence.sh <a|b>   a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env:  ARTI_AUTH / BF_AUTH = "user:password" (env only; redacted in artifacts)
#       CONAN2 / CONAN1 = client binaries (default /tmp/l018venv{,1}/bin/conan)
# Out:  reports/compatibility/l018-wire/<side>/{conan,client,setup}/...
#
# Cleanup contract: namespace l018-conan + l018-conan-virt deleted (deleteContent)
# at the end of side b; re-run of a side is idempotent (repo PUT overwrites cfg).
set -u
HERE=/Users/lzw/dev-center/tools/difftest/l018
FIX=$HERE/fixtures
WIRE=/Users/lzw/dev-center/reports/compatibility/l018-wire
CONAN2=${CONAN2:-/tmp/l018venv/bin/conan}
CONAN1=${CONAN1:-/tmp/l018venv1/bin/conan}

SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then
  HOSTPORT=8082; PREFIX='artifactory/api/conan'; AUTHVAR=ARTI_AUTH
else
  HOSTPORT=8083; PREFIX='binflow';              AUTHVAR=BF_AUTH
fi
AUTH=${!AUTHVAR:-}
[ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
USER=${AUTH%%:*}; PW=${AUTH#*:}

BASE=http://localhost:$HOSTPORT
CBASE=$BASE/$PREFIX/l018-conan          # conan endpoints (v1|v2 under here)
if [ "$SIDE" = a ]; then ADMINAPI=$BASE/artifactory/api; else ADMINAPI=$BASE/binflow/api; fi
VIRT=$BASE/$PREFIX/l018-conan-virt

OUT=$WIRE/$SIDE
WS=$HERE/ws-$SIDE
rm -rf "$OUT" "$WS"; mkdir -p "$OUT/conan" "$OUT/client" "$OUT/setup" "$WS/wire"
CURL=(curl -sS --noproxy '*' -u "$AUTH")
ARMS=$OUT/arms.txt; : > "$ARMS"
arm()  { echo "[$(date +%H:%M:%S)] arm $*" | tee -a "$ARMS"; }
settle() { sleep 2; }
RED()  { sed -e "s/$PW/REDACTED-PW/g" -e "s/$(printf %s "$USER:$PW" | base64)/REDACTED-B64/g"; }

req() { # req <id> <method> <url> [curl args...] -> $OUT/conan/<id>.{hdr,body}
  local id=$1 method=$2 url=$3; shift 3; local try code
  for try in 1 2 3 4 5; do
    "${CURL[@]}" -X "$method" -D "$OUT/conan/$id.hdr" -o "$OUT/conan/$id.body" \
      -w "$id %{http_code} %{size_download}B\n" "$url" "$@"
    code=$(awk 'NR==1{sub(/\r/,"");print $2}' "$OUT/conan/$id.hdr")
    if [ "$code" != 503 ] && ! grep -q 'not yet migrated' "$OUT/conan/$id.body" 2>/dev/null; then
      return 0
    fi
    echo "  [gate] ref unhealthy ($code / migration) — waiting 15s"; sleep 15
  done
  echo "  [gate] ref still unhealthy after 5 tries — ABORT $id" >&2
}

cliproxy() { # cliproxy <leg> <client-cmd...>  (cmd reads $PROXYURL)
  local leg=$1; shift
  mkdir -p "$OUT/client/wire"; : > "$OUT/client/wire/$leg.wire"
  UPSTREAM="localhost:$HOSTPORT" python3 "$HERE/wireproxy.py" 19018 "$OUT/client/wire" "$leg" &
  local px=$!
  sleep 1
  PROXYURL="http://127.0.0.1:19018/$PREFIX/l018-conan" "$@" > "$OUT/client/$leg.log" 2>&1
  echo "exit=$?" >> "$OUT/client/$leg.log"
  kill $px 2>/dev/null; wait $px 2>/dev/null
}

# ══ setup (not arm-budgeted) ══════════════════════════════════════════════════
arm "0-setup repos l018-conan(local) + l018-conan-virt(virtual)"
printf '{"rclass":"local","packageType":"conan"}' > "$OUT/setup/local.json"
printf '{"rclass":"virtual","packageType":"conan","repositories":["l018-conan"]}' > "$OUT/setup/virt.json"
req setup-repo-local PUT  "$ADMINAPI/repositories/l018-conan"      -H 'Content-Type: application/json' -T "$OUT/setup/local.json"
req setup-repo-virt  PUT  "$ADMINAPI/repositories/l018-conan-virt" -H 'Content-Type: application/json' -T "$OUT/setup/virt.json"
settle

# ══ V2 FACE (conan 2.31.2) ═══════════════════════════════════════════════════
arm "v2-01 ping anon + authed + client-version-check triptych"
req v2-01a-ping-anon   GET  "$CBASE/v2/ping"
req v2-01b-ping-authed GET  "$CBASE/v2/ping"
for v in 0.15.0 0.17.0 0.20.0 0.25.0; do
  req "v2-01c-vcv-$v" GET "$CBASE/v2/ping" -H "X-Conan-Client-Version: $v"
done
settle

arm "v2-02 login (conan 2.31.2 CLI via wireproxy) + curl authenticate"
cliproxy v2-02-login sh -c '
  export CONAN_HOME="$0-conanhome"; C="'"$CONAN2"'"
  rm -rf "$CONAN_HOME"; "$C" remote add l018 "$PROXYURL" --force 2>&1
  "$C" remote login l018 '"$USER"' -p "'"$PW"'" 2>&1' "$WS"
req v2-02b-authenticate GET "$CBASE/v2/users/authenticate"
req v2-02c-auth-badcred GET "$CBASE/v2/users/authenticate" -u "$USER:wrongpass"
settle

arm "v2-03 check_credentials (bearer from 02) + bad token"
TOKEN=$(cat "$OUT/conan/v2-02b-authenticate.body")
req v2-03a-check-ok   GET "$CBASE/v2/users/check_credentials" -H "Authorization: Bearer $TOKEN"
req v2-03b-check-bad  GET "$CBASE/v2/users/check_credentials" -H "Authorization: Bearer not-a-token"
settle

arm "v2-04 upload recipe+package (conan 2.31.2 CLI)"
cliproxy v2-04-upload sh -c '
  export CONAN_HOME="$0-conanhome"; C="'"$CONAN2"'"
  "$C" profile detect --force 2>&1
  cd "'"$FIX"'" && "$C" create . --name=hello18 --version=1.0 --user=l018 --channel=stable 2>&1
  "$C" upload "hello18/1.0@l018/stable" -r l018 --confirm 2>&1' "$WS"
settle

arm "v2-05 rev-chain: second revision (content change) + latest/revisions wire"
cliproxy v2-05-upload-r2 sh -c '
  export CONAN_HOME="$0-conanhome"; C="'"$CONAN2"'"
  rm -rf "$0-r2"; mkdir -p "$0-r2"; cp "'"$FIX"'/conanfile-r2.py" "$0-r2/conanfile.py"
  cd "$0-r2" && "$C" create . --name=hello18 --version=1.0 --user=l018 --channel=stable 2>&1
  "$C" upload "hello18/1.0@l018/stable" -r l018 --confirm 2>&1' "$WS"
settle
req v2-05b-latest     GET  "$CBASE/v2/conans/hello18/1.0/l018/stable/latest"
req v2-05c-revisions  GET  "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions"
req v2-05d-latest404  GET  "$CBASE/v2/conans/hello18/9.9/l018/stable/latest"
settle

arm "v2-06 recipe files list (rrev from 05b latest)"
RREV=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["revision"])' "$OUT/conan/v2-05b-latest.body" 2>/dev/null || true)
arm "    (rrev=$RREV)"
req v2-06a-recipe-files GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/files"
settle

arm "v2-07 download (conan 2.31.2 CLI fresh cache) + curl file fetch"
cliproxy v2-07-download sh -c '
  export CONAN_HOME="$0-conanhome-dl"; C="'"$CONAN2"'"
  rm -rf "$CONAN_HOME"; "$C" profile detect --force 2>&1
  "$C" remote add l018 "$PROXYURL" --force 2>&1
  "$C" remote login l018 '"$USER"' -p "'"$PW"'" 2>&1
  "$C" install --requires=hello18/1.0@l018/stable -r l018 2>&1' "$WS"
req v2-07b-get-conanfile GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/files/conanfile.py"
settle

arm "v2-08 search (conan list hello18/* — revision-wildcard suffix) + curl q="
cliproxy v2-08-search sh -c '
  export CONAN_HOME="$0-conanhome"; C="'"$CONAN2"'"
  "$C" list "hello18/*" -r l018 2>&1' "$WS"
req v2-08b-search-q GET "$CBASE/v2/conans/search?q=hello18/*"
req v2-08c-search-star GET "$CBASE/v2/conans/search?q=hello*"
settle

arm "v2-09 packageId metadata (settings/options/recipe_hash=rrev)"
req v2-09a-pkgmeta GET "$CBASE/v2/conans/hello18/1.0/l018/stable/search?q="
PID=$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print(next(iter(d)))' "$OUT/conan/v2-09a-pkgmeta.body" 2>/dev/null || echo NOPID)
arm "    (pid=$PID)"
req v2-09b-pkg-latest    GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/packages/$PID/latest"
req v2-09c-pkg-revisions GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/packages/$PID/revisions"
PREV=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["revisions"][0]["revision"])' "$OUT/conan/v2-09c-pkg-revisions.body" 2>/dev/null || true)
req v2-09d-pkg-files GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/packages/$PID/revisions/$PREV/files"
req v2-09e-pkg-tgz  GET "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$RREV/packages/$PID/revisions/$PREV/files/conan_package.tgz"
settle

arm "v2-10 delete single revision (old rrev) + ghost rrev 404"
OLDRREV=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["revisions"][-1]["revision"])' "$OUT/conan/v2-05c-revisions.body" 2>/dev/null || true)
req v2-10a-del-rev    DELETE "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/$OLDRREV"
req v2-10b-rev-after  GET    "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions"
req v2-10c-del-ghost  DELETE "$CBASE/v2/conans/hello18/1.0/l018/stable/revisions/deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
settle

arm "v2-11 delete whole recipe + latest 404 + re-delete"
req v2-11a-del-recipe DELETE "$CBASE/v2/conans/hello18/1.0/l018/stable"
req v2-11b-latest-404 GET    "$CBASE/v2/conans/hello18/1.0/l018/stable/latest"
req v2-11c-del-again  DELETE "$CBASE/v2/conans/hello18/1.0/l018/stable"
settle

# ══ V1 FACE (conan 1.66.0) ═══════════════════════════════════════════════════
arm "v1-12 ping (anon + authed)"
req v1-12a-ping-anon   GET "$CBASE/v1/ping"
req v1-12b-ping-authed GET "$CBASE/v1/ping"
settle

arm "v1-13 login (conan 1.66.0 CLI via wireproxy) + curl authenticate"
cliproxy v1-13-login sh -c '
  export CONAN_USER_HOME="$0-conan1home"; C="'"$CONAN1"'"
  rm -rf "$CONAN_USER_HOME"
  echo "revisions=$(  "$C" config get general.revisions 2>&1 || true)"
  "$C" remote add l018 "$PROXYURL" False 2>&1
  "$C" user '"$USER"' -p "'"$PW"'" -r l018 2>&1' "$WS"
req v1-13b-authenticate GET "$CBASE/v1/users/authenticate"
settle

arm "v1-14 check_credentials (bearer from 13)"
TOKEN1=$(cat "$OUT/conan/v1-13b-authenticate.body")
req v1-14-check GET "$CBASE/v1/users/check_credentials" -H "Authorization: Bearer $TOKEN1"
settle

arm "v1-15 upload recipe+package (conan 1.66.0 CLI)"
cliproxy v1-15-upload sh -c '
  export CONAN_USER_HOME="$0-conan1home"; C="'"$CONAN1"'"
  # apple-clang 17 not in conan 1.66 settings.yml -> manual minimal profile
  mkdir -p "$CONAN_USER_HOME/.conan/profiles"
  printf "[settings]\nos=Macos\nos_build=Macos\narch=armv8\narch_build=armv8\ncompiler=apple-clang\ncompiler.version=16.0\ncompiler.libcxx=libc++\n[build_requires]\n" \
    > "$CONAN_USER_HOME/.conan/profiles/default"
  cd "'"$FIX"'" && "$C" create . hello18/1.1@l018/stable 2>&1
  "$C" upload hello18/1.1@l018/stable -r l018 --all --confirm 2>&1' "$WS"
settle

arm "v1-16 upload_urls (absolute URL form — server self-knowledge)"
SZ=$(wc -c < "$FIX/conanfile.py" | tr -d ' ')
printf '{"conanfile.py":%s}' "$SZ" > "$OUT/conan/v1-16-upload_urls.reqbody"
req v1-16a-upload-urls POST "$CBASE/v1/conans/hello18/1.1/l018/stable/upload_urls" \
  -H 'Content-Type: application/json' -T "$OUT/conan/v1-16-upload_urls.reqbody"
req v1-16b-upload-urls-pkg POST "$CBASE/v1/conans/hello18/1.1/l018/stable/packages/5ab5d68f01a5f6e2f9f3ad6d2d1d9b6d3f0e6c1d/upload_urls" \
  -H 'Content-Type: application/json' -T "$OUT/conan/v1-16-upload_urls.reqbody"
settle

arm "v1-17 download_urls + digest (absolute URL form)"
req v1-17a-dl-urls GET "$CBASE/v1/conans/hello18/1.1/l018/stable/download_urls"
req v1-17b-digest  GET "$CBASE/v1/conans/hello18/1.1/l018/stable/digest"
settle

arm "v1-18 snapshot (recipe md5 map + package)"
req v1-18a-snapshot GET "$CBASE/v1/conans/hello18/1.1/l018/stable"
settle

arm "v1-19 search (curl q= + conan 1.66 CLI)"
req v1-19a-search GET "$CBASE/v1/conans/search?q=hello*"
cliproxy v1-19b-search-cli sh -c '
  export CONAN_USER_HOME="$0-conan1home"; C="'"$CONAN1"'"
  "$C" search hello18 -r l018 2>&1' "$WS"
settle

arm "v1-20 download (conan 1.66 CLI fresh cache)"
cliproxy v1-20-install sh -c '
  export CONAN_USER_HOME="$0-conan1home-dl"; C="'"$CONAN1"'"
  rm -rf "$CONAN_USER_HOME"
  mkdir -p "$CONAN_USER_HOME/.conan/profiles"
  printf "[settings]\nos=Macos\nos_build=Macos\narch=armv8\narch_build=armv8\ncompiler=apple-clang\ncompiler.version=16.0\ncompiler.libcxx=libc++\n[build_requires]\n" \
    > "$CONAN_USER_HOME/.conan/profiles/default"
  "$C" remote add l018 "$PROXYURL" False 2>&1
  "$C" user '"$USER"' -p "'"$PW"'" -r l018 2>&1
  "$C" install hello18/1.1@l018/stable -r l018 2>&1' "$WS"
settle

arm "v1-21 v1-data-plane-on-virtual (gate) + only_v2 capability"
req v1-21a-v1-on-virt GET "$VIRT/v1/conans/search?q=hello*"
req v1-21b-v2-ping-virt GET "$VIRT/v2/ping"
settle

arm "v1-22 delete whole tree (CLI remove) + cross-plane v2 404 + re-delete"
cliproxy v1-22-remove sh -c '
  export CONAN_USER_HOME="$0-conan1home"; C="'"$CONAN1"'"
  "$C" remove hello18/1.1@l018/stable -r l018 -f 2>&1' "$WS"
req v1-22b-snapshot-404 GET "$CBASE/v1/conans/hello18/1.1/l018/stable"
req v1-22c-v2-latest-404 GET "$CBASE/v2/conans/hello18/1.1/l018/stable/latest"
req v1-22d-del-again DELETE "$CBASE/v1/conans/hello18/1.1/l018/stable"
settle

# ══ teardown (side b only; leave side a for triage until b is green) ═════════
arm "T-teardown"
if [ "$SIDE" = b ]; then
  req teardown-repo-virt  DELETE "$ADMINAPI/repositories/l018-conan-virt?deleteContent=true"
  req teardown-repo-local DELETE "$ADMINAPI/repositories/l018-conan?deleteContent=true"
else
  echo "side a: repos kept for triage; run teardown-a.sh after b is green" | tee -a "$ARMS"
fi

# redact any accidental credential echoes in client logs
for f in "$OUT"/client/*.log; do [ -f "$f" ] && RED < "$f" > "$f.r" && mv "$f.r" "$f"; done
echo "done side=$SIDE arms=$(grep -c '^\[.*\] arm [v0T]' "$ARMS")"
