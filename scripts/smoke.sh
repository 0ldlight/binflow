#!/bin/sh
# BinFlow bare-binary smoke test (T-16, PRD C01/C03/C07/C08 minimal chain).
#
# Builds (or reuses) bin/binflow-server, boots it on an ephemeral port with
# a throwaway data directory, then runs the four-step happy chain every
# downstream ticket (T-17/T-18) can reuse:
#
#   C01  GET  /binflow/api/system/ping          -> 200 "OK"
#   C03  PUT  /binflow/api/repositories/{key}   -> 200 plain text
#   C07  PUT  /binflow/{repo}/{path}            -> 201 + sha256 echo
#   C08  GET  /binflow/{repo}/{path}            -> 200 + sha256 match
#
# Everything is cleaned up by the trap (server process + temp dir). Exit
# code 0 = smoke passed; any step failing aborts with the failing command.
#
# Usage: scripts/smoke.sh [path-to-binary]
#   The binary argument lets CI reuse an artifact it already built;
#   without it the script builds via `make build` when bin/ is stale.

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BIN=${1:-"$ROOT/bin/binflow-server"}

fail() {
    echo "smoke: FAIL: $1" >&2
    exit 1
}

step() {
    echo "---- $1"
}

# ---- build (or reuse) ------------------------------------------------------

if [ ! -x "$BIN" ]; then
    step "binary $BIN not found; building via make build"
    (cd "$ROOT" && make build) || fail "make build"
fi
[ -x "$BIN" ] || fail "binary $BIN missing after build"

for tool in curl python3; do
    command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
done

# ---- fixtures --------------------------------------------------------------

TMPDIR_SMOKE=$(mktemp -d "${TMPDIR:-/tmp}/binflow-smoke.XXXXXX")
SERVER_LOG="$TMPDIR_SMOKE/serve.log"
DATA_DIR="$TMPDIR_SMOKE/data"
SERVE_PID=""

cleanup() {
    if [ -n "$SERVE_PID" ]; then
        kill -TERM "$SERVE_PID" 2>/dev/null || true
        # Wait for the graceful drain (30s worst case; smoke is idle, so the
        # real wait is milliseconds).
        wait "$SERVE_PID" 2>/dev/null || true
    fi
    rm -rf "$TMPDIR_SMOKE"
}
trap cleanup EXIT INT TERM

# ---- ephemeral port --------------------------------------------------------

PORT=$(python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()') || fail "cannot pick an ephemeral port"
BASE="http://127.0.0.1:$PORT"
ADMIN_PW="smoke-admin-pw" # evaluation instances must not run the default

# ---- boot ------------------------------------------------------------------

step "starting binflow-server on $BASE (data: $DATA_DIR)"
cat > "$TMPDIR_SMOKE/binflow.yaml" <<EOF
server:
  listen: 127.0.0.1:$PORT
storage:
  data_dir: $DATA_DIR
EOF

BINFLOW_ADMIN_PASSWORD="$ADMIN_PW" \
    "$BIN" serve -c "$TMPDIR_SMOKE/binflow.yaml" >"$SERVER_LOG" 2>&1 &
SERVE_PID=$!

# ---- C01: ping, with cold-start timing --------------------------------------

START=$(python3 -c 'import time; print(time.time())')
OK=""
i=0
while [ "$i" -lt 200 ]; do
    if BODY=$(curl -sf "$BASE/binflow/api/system/ping" 2>/dev/null) && [ "$BODY" = "OK" ]; then
        OK=1
        break
    fi
    i=$((i + 1))
    sleep 0.05
done
[ -n "$OK" ] || { cat "$SERVER_LOG" >&2; fail "C01: server never answered ping on $BASE"; }
ELAPSED=$(python3 -c "print(f'{__import__(\"time\").time() - float($START):.3f}')")
echo "C01 ping: OK (cold start exec -> ping OK: ${ELAPSED}s, NFR-P1 budget 2s)"

# NFR-P1: the smoke run doubles as the cold-start gate.
python3 - "$ELAPSED" <<'PY' || fail "NFR-P1: cold start exceeded 2s"
import sys
if float(sys.argv[1]) >= 2.0:
    sys.exit(1)
PY

step "C03 create repository"
CODE=$(curl -su "admin:$ADMIN_PW" -o "$TMPDIR_SMOKE/c03.body" -w '%{http_code}' \
    -X PUT "$BASE/binflow/api/repositories/generic-local" \
    -H 'Content-Type: application/json' \
    -d '{"rclass":"local","packageType":"generic","description":"smoke"}')
[ "$CODE" = "200" ] || { cat "$TMPDIR_SMOKE/c03.body" >&2; fail "C03: want 200, got $CODE"; }
echo "C03 create repository: 200 ($(head -c 60 "$TMPDIR_SMOKE/c03.body"))"

step "C07 upload with sha256 reconciliation"
PAYLOAD="$TMPDIR_SMOKE/artifact.bin"
python3 -c 'import os,sys; sys.stdout.buffer.write(os.urandom(64*1024))' > "$PAYLOAD"
WANT_SHA=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$PAYLOAD")
CODE=$(curl -su "admin:$ADMIN_PW" -o "$TMPDIR_SMOKE/c07.body" -w '%{http_code}' \
    -T "$PAYLOAD" "$BASE/binflow/generic-local/acme/artifact.bin")
[ "$CODE" = "201" ] || { cat "$TMPDIR_SMOKE/c07.body" >&2; fail "C07: want 201, got $CODE"; }
GOT_SHA=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["checksums"]["sha256"])' "$TMPDIR_SMOKE/c07.body")
[ "$GOT_SHA" = "$WANT_SHA" ] || fail "C07: server sha256 $GOT_SHA != local $WANT_SHA"
echo "C07 upload: 201, checksums.sha256 matches local sha256 ($WANT_SHA)"

step "C08 download and verify"
DL="$TMPDIR_SMOKE/download.bin"
CODE=$(curl -s -o "$DL" -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/generic-local/acme/artifact.bin")
[ "$CODE" = "200" ] || fail "C08: want 200, got $CODE"
DL_SHA=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$DL")
[ "$DL_SHA" = "$WANT_SHA" ] || fail "C08: downloaded sha256 $DL_SHA != source $WANT_SHA"
echo "C08 download: 200, sha256 identical to source"

step "anonymous read (default on, ADR-0009)"
CODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/binflow/generic-local/acme/artifact.bin")
[ "$CODE" = "200" ] || fail "anonymous GET: want 200, got $CODE"
echo "anonymous GET: 200"

# ---- teardown proof ---------------------------------------------------------

step "graceful shutdown"
kill -TERM "$SERVE_PID"
wait "$SERVE_PID"
EXIT_CODE=$?
SERVE_PID=""
[ "$EXIT_CODE" = "0" ] || fail "server exit code $EXIT_CODE after SIGTERM, want 0"
grep -q '"msg":"binflow stopped"' "$SERVER_LOG" || fail "shutdown log line missing"
echo "SIGTERM -> exit 0"

echo "smoke: PASS (C01/C03/C07/C08 + shutdown)"
