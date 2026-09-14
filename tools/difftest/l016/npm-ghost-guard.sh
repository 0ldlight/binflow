#!/bin/bash
# L016-2 npm ghost-publish guard deep probe — mismatch taxonomy, <=6 arms.
# Evidence: L015 N4/N4' (reports/compatibility/L015-expansion-probe.md) pinned
# ONE ghost form (versions 9.9.9 + attachment -1.0.0.tgz -> ref 403 vs BinFlow
# 201+latest hijack). This probe subdivides the mismatch space, each arm on a
# FRESH package whose 1.0.0 is a REAL npm publish (projection guaranteed), so
# readbacks separate "guard fired" from "crafted-doc non-materialization":
#   G1 attachment name not in versions + tarball path conflict (N4 repl)
#   G2 versions carries a pre-existing version + a new one, attachment=new
#   G3 both all wrong (attachment version neither in versions nor stored)
#   G4 control: crafted clean 2.0.0 (materialization discriminator)
# Serial, settle 2s, ref 503 gate (crawl discipline).
#
# Usage: npm-ghost-guard.sh <a|b>   a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env:  ARTI_AUTH / BF_AUTH = "user:password" (never on disk).
# Out:  reports/compatibility/l016-probe-wire/<side>/npm-ghost/...
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
REPO=l016-ghost-npm
NPM_API=$BASE/api/npm/$REPO
OUT=/Users/lzw/dev-center/reports/compatibility/l016-probe-wire/$SIDE/npm-ghost
WS=/tmp/l016/ghost-ws-$SIDE
NPM=/tmp/l0121/npm10/node_modules/.bin/npm
TAP=/Users/lzw/dev-center/tools/difftest/l0121/wiretap.py
[ -x "$NPM" ] || NPM=$(command -v npm)
rm -rf "$OUT"; mkdir -p "$OUT" "$WS"
CURL=(curl -sS --noproxy '*' -m 30 -u "$AUTH")

# --- ref health gate: 503 or recurrent-failure body -> wait and retry ---------
req() { # req <id> <method> <url> [curl args...]
  local id=$1 method=$2 url=$3; shift 3; local try code
  for try in 1 2 3 4 5; do
    "${CURL[@]}" -X "$method" -D "$OUT/$id.hdr" -o "$OUT/$id.body" \
      -w "$id %{http_code} %{size_download}B\n" "$url" "$@"
    code=$(awk 'NR==1{sub(/\r/,"");print $2}' "$OUT/$id.hdr")
    if [ "$code" != 503 ] && ! grep -q 'recurrent request failures' "$OUT/$id.body" 2>/dev/null; then
      return 0
    fi
    echo "  [gate] ref unhealthy ($code) — waiting 15s"; sleep 15
  done
  echo "  [gate] ref still unhealthy after 5 tries"
}
settle() { sleep 2; }

# --- repo ---------------------------------------------------------------------
printf '{"rclass":"local","packageType":"npm"}' > "$OUT/repo.json"
req repo PUT "$BASE/api/repositories/$REPO" -H 'Content-Type: application/json' -T "$OUT/repo.json"
settle

NPMFLAGS=(--userconfig /tmp/l016/npmrc-fake-$SIDE --cache /tmp/l016/npm-cache-$SIDE
          --registry "http://127.0.0.1:$TAPPORT/$PREFIX/api/npm/$REPO/"
          --prefer-online --no-fund --no-audit --loglevel warn
          --fetch-retries 0 --fetch-timeout 30000)
taprun() { # taprun <label> <cmd...> — real client via wiretap (Basic from env)
  printf '//127.0.0.1:%s/%s/api/npm/%s/:_authToken=l016-fake-client-token\n' \
    "$TAPPORT" "$PREFIX" "$REPO" > /tmp/l016/npmrc-fake-$SIDE
  ( cd "$WS" && TAP_CWD=$WS python3 "$TAP" --target "http://localhost:$HOSTPORT" \
      --port "$TAPPORT" --log "$OUT/$1.log" --auth-env "$AUTHVAR" -- "${@:2}" )
}
setup100() { # setup100 <pkg> — real npm publish 1.0.0 (materializes projection)
  printf '{"name":"%s","version":"1.0.0","description":"l016 ghost guard probe"}' "$1" > "$WS/package.json"
  rm -rf "$WS/node_modules"
  taprun "setup-$1" "$NPM" "${NPMFLAGS[@]}" publish 2>&1 | tail -2
  settle
}
# ghost <file> <pkg> <json> — write crafted doc then PUT it
ghost() { # ghost <id> <pkg>
  req "$1" PUT "$NPM_API/$2" -H 'Content-Type: application/json' -T "$WS/$1.json"
  settle
}
pydoc() { python3 - "$@"; }

echo "======== side=$SIDE npm ghost-guard taxonomy (4 arms + readbacks) ========"

# --- setups: real npm publish 1.0.0 per arm package ---------------------------
for p in l016-gh1-pkg l016-gh2-pkg l016-gh3-pkg l016-gh4-pkg; do setup100 "$p"; done

# --- G1: attachment name not in versions + path conflict (N4 repl) -----------
pydoc "$WS" l016-gh1-pkg <<'EOF'
import base64, json, sys, os
ws, pkg = sys.argv[1], sys.argv[2]
tar = base64.b64encode(f"{pkg} ghost 9.9.9 bytes".encode()).decode()
doc = {"_id": pkg, "name": pkg, "dist-tags": {"latest": "9.9.9"},
       "versions": {"9.9.9": {"name": pkg, "version": "9.9.9",
         "dist": {"tarball": f"http://x/{pkg}-9.9.9.tgz"}}},
       "_attachments": {f"{pkg}-1.0.0.tgz": {"content_type": "application/octet-stream",
         "data": tar, "length": len(base64.b64decode(tar))}}}
open(os.path.join(ws, "g1.json"), "w").write(json.dumps(doc))
EOF
ghost g1 l016-gh1-pkg
req g1-packument GET "$NPM_API/l016-gh1-pkg"

# --- G2: versions carries pre-existing 1.0.0 + new 2.0.0, attachment = new ---
pydoc "$WS" l016-gh2-pkg <<'EOF'
import base64, json, sys, os
ws, pkg = sys.argv[1], sys.argv[2]
tar = base64.b64encode(f"{pkg} mixed 2.0.0 bytes".encode()).decode()
doc = {"_id": pkg, "name": pkg, "dist-tags": {"latest": "2.0.0"},
       "versions": {
         "1.0.0": {"name": pkg, "version": "1.0.0",
           "dist": {"tarball": f"http://x/{pkg}-1.0.0.tgz"}},
         "2.0.0": {"name": pkg, "version": "2.0.0",
           "dist": {"tarball": f"http://x/{pkg}-2.0.0.tgz"}}},
       "_attachments": {f"{pkg}-2.0.0.tgz": {"content_type": "application/octet-stream",
         "data": tar, "length": len(base64.b64decode(tar))}}}
open(os.path.join(ws, "g2.json"), "w").write(json.dumps(doc))
EOF
ghost g2 l016-gh2-pkg
req g2-packument GET "$NPM_API/l016-gh2-pkg"

# --- G3: both all wrong (7.7.7 neither in versions nor stored) ---------------
pydoc "$WS" l016-gh3-pkg <<'EOF'
import base64, json, sys, os
ws, pkg = sys.argv[1], sys.argv[2]
tar = base64.b64encode(f"{pkg} wild 7.7.7 bytes".encode()).decode()
doc = {"_id": pkg, "name": pkg, "dist-tags": {"latest": "9.9.9"},
       "versions": {"9.9.9": {"name": pkg, "version": "9.9.9",
         "dist": {"tarball": f"http://x/{pkg}-9.9.9.tgz"}}},
       "_attachments": {f"{pkg}-7.7.7.tgz": {"content_type": "application/octet-stream",
         "data": tar, "length": len(base64.b64decode(tar))}}}
open(os.path.join(ws, "g3.json"), "w").write(json.dumps(doc))
EOF
ghost g3 l016-gh3-pkg
req g3-packument GET "$NPM_API/l016-gh3-pkg"
req g3-tarball-777 GET "$NPM_API/l016-gh3-pkg/-/l016-gh3-pkg-7.7.7.tgz"
req g3-tarball-999 GET "$NPM_API/l016-gh3-pkg/-/l016-gh3-pkg-9.9.9.tgz"

# --- G4 control: crafted clean 2.0.0 (discriminates doc-form vs mismatch) -----
pydoc "$WS" l016-gh4-pkg <<'EOF'
import base64, json, sys, os
ws, pkg = sys.argv[1], sys.argv[2]
tar = base64.b64encode(f"{pkg} clean 2.0.0 bytes".encode()).decode()
doc = {"_id": pkg, "name": pkg, "dist-tags": {"latest": "2.0.0"},
       "versions": {"2.0.0": {"name": pkg, "version": "2.0.0",
         "dist": {"tarball": f"http://x/{pkg}-2.0.0.tgz"}}},
       "_attachments": {f"{pkg}-2.0.0.tgz": {"content_type": "application/octet-stream",
         "data": tar, "length": len(base64.b64decode(tar))}}}
open(os.path.join(ws, "g4.json"), "w").write(json.dumps(doc))
EOF
ghost g4 l016-gh4-pkg
req g4-packument GET "$NPM_API/l016-gh4-pkg"

echo "======== side=$SIDE done — evidence in $OUT"
