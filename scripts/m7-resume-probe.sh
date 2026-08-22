#!/bin/sh
# BinFlow M7 resume-across-restart probe (T-211, PRD FR-67 / DU-01/DU-02/DU-03).
#
# Full docker blob-upload chain with a server stop in the middle, exactly the
# V15/V16 shape:
#
#   POST  /v2/<repo>/<img>/blobs/uploads/   -> 202 + Location (session grant)
#   PATCH 512KiB  (Content-Range: 0-524287) -> 202 + Range: 0-524287
#   GET   <upload URL>                      -> 204 + Range  (pre-restart sanity)
#   --- stop the server (--stop kill|sigterm) and restart it on the SAME
#       data_dir (direct process management; SIGTERM is the same signal
#       `docker compose restart`/stop delivers, so the two arms cover the
#       compose口径 without a Docker dependency) ---
#   GET   <upload URL>                      -> 204 + Range: 0-524287  (resume)
#   PATCH 64KiB   (Content-Range: 524288-)  -> 202
#   PUT   <upload URL>?digest=sha256:...    -> 201
#   GET   /v2/<repo>/<img>/blobs/<digest>   -> 200, bitwise-identical body
#
# RED-LIGHT-FIRST contract (T-211): on pre-T-216 main the upload-session
# registry is process memory, so after EITHER stop style the restart answers
# 404 BLOB_UPLOAD_UNKNOWN on the upload URL and the probe FAILS with exit 1.
# The archived output is the "before" baseline for T-216 (FR-67) and FR-70's
# Close-semantics change (O-1 / ADR-0028).
#
# Exit codes:
#   0  GREEN — every step passed, resume works end to end (post-T-216 state)
#   1  RED   — the resume observation failed (404 BLOB_UPLOAD_UNKNOWN, wrong
#              Range, broken续块/收尾/逐位校验); baseline on main, fix target
#   2  probe infrastructure error (server never booted, PATCH before the stop
#      failed, missing tools) — not a verdict on resume semantics
#
# Usage: scripts/m7-resume-probe.sh --stop kill|sigterm [path-to-binary]
#
# POSIX sh (macOS bash-3.2-as-sh / dash / busybox ash); macOS + Linux.

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

STOP=""
BIN=""
while [ $# -gt 0 ]; do
    case "$1" in
    --stop)
        [ $# -ge 2 ] || { echo "m7-resume-probe: --stop needs a value" >&2; exit 2; }
        STOP=$2
        shift 2
        ;;
    --stop=*)
        STOP=${1#--stop=}
        shift
        ;;
    -h|--help)
        sed -n '2,32p' "$0"
        exit 0
        ;;
    -*)
        echo "m7-resume-probe: unknown option $1" >&2
        exit 2
        ;;
    *)
        BIN=$1
        shift
        ;;
    esac
done

case "$STOP" in
kill|sigterm) ;;
"")
    echo "usage: scripts/m7-resume-probe.sh --stop kill|sigterm [path-to-binary]" >&2
    exit 2
    ;;
*)
    echo "m7-resume-probe: --stop must be kill or sigterm, got '$STOP'" >&2
    exit 2
    ;;
esac

REPO="m7-resume"
IMAGE="probe-img"
CHUNK1_KIB=512
CHUNK2_KIB=64
CHUNK1_BYTES=$((CHUNK1_KIB * 1024))
CHUNK2_BYTES=$((CHUNK2_KIB * 1024))
C2_START=$CHUNK1_BYTES
C2_END=$((CHUNK1_BYTES + CHUNK2_BYTES - 1))
WANT_RANGE="0-$((CHUNK1_BYTES - 1))"

fail_infra() {
    echo "m7-resume-probe: INFRA FAIL: $1" >&2
    [ -f "$SERVER_LOG" ] && tail -20 "$SERVER_LOG" >&2
    exit 2
}

step() {
    echo "---- $1"
}

# ---- build (or reuse) ------------------------------------------------------

if [ "$BIN" = "" ]; then
    BIN="$ROOT/bin/binflow-server"
fi
if [ ! -x "$BIN" ]; then
    step "binary $BIN not found; building via make build"
    (cd "$ROOT" && make build) || fail_infra "make build"
fi
[ -x "$BIN" ] || fail_infra "binary $BIN missing after build"

for tool in curl python3; do
    command -v "$tool" >/dev/null 2>&1 || fail_infra "$tool is required"
done

# ---- workspace: ONE data dir survives the restart --------------------------

WORK=$(mktemp -d "${TMPDIR:-/tmp}/binflow-m7-resume.XXXXXX")
SERVER_LOG="$WORK/serve.log"
DATA_DIR="$WORK/data"
CFG="$WORK/binflow.yaml"
SERVE_PID=""

cleanup() {
    if [ -n "$SERVE_PID" ]; then
        kill -9 "$SERVE_PID" 2>/dev/null || true
        wait "$SERVE_PID" 2>/dev/null || true
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

PORT=$(python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()') || fail_infra "cannot pick an ephemeral port"
BASE="http://127.0.0.1:$PORT"
ADMIN_PW="m7-resume-admin-pw"

cat > "$CFG" <<EOF
server:
  listen: 127.0.0.1:$PORT
storage:
  data_dir: $DATA_DIR
EOF

# start_server: boots on the SAME config/data dir; sets SERVE_PID.
start_server() {
    BINFLOW_ADMIN_PASSWORD="$ADMIN_PW" \
        "$BIN" serve -c "$CFG" >>"$SERVER_LOG" 2>&1 &
    SERVE_PID=$!
    n=0
    while [ "$n" -lt 200 ]; do
        if BODY=$(curl -sf "$BASE/binflow/api/system/ping" 2>/dev/null) \
            && [ "$BODY" = "OK" ]; then
            return 0
        fi
        kill -0 "$SERVE_PID" 2>/dev/null || fail_infra "server exited during boot (see $SERVER_LOG)"
        n=$((n + 1))
        sleep 0.1
    done
    fail_infra "server never answered ping on $BASE"
}

red() {
    echo ""
    echo "m7-resume-probe: RED — resume observation failed at: $1" >&2
    echo "  This is the pre-T-216 baseline behavior (in-memory session" >&2
    echo "  registry); T-216 / FR-67 makes this probe GREEN." >&2
    exit 1
}

step "boot #1 on $BASE (data: $DATA_DIR, stop style: $STOP)"
start_server
echo "server pid: $SERVE_PID"

step "fixture: docker repository '$REPO'"
CODE=$(curl -sS -o "$WORK/repo.body" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X PUT "$BASE/binflow/api/repositories/$REPO" \
    -H 'Content-Type: application/json' \
    -d '{"rclass":"local","packageType":"docker","description":"m7 resume probe"}' || true)
[ "$CODE" = "200" ] || fail_infra "create docker repo: want 200, got $CODE: $(cat "$WORK/repo.body")"
echo "PUT /api/repositories/$REPO: 200"

step "POST /v2/$REPO/$IMAGE/blobs/uploads/ (session grant)"
CODE=$(curl -sS -o /dev/null -D "$WORK/post.hdr" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X POST "$BASE/v2/$REPO/$IMAGE/blobs/uploads/" || true)
[ "$CODE" = "202" ] || fail_infra "initiating POST: want 202, got $CODE"
LOC=$(awk -F': ' 'tolower($1)=="location"{sub(/\r/,"",$2); print $2}' "$WORK/post.hdr")
UUID=$(awk -F': ' 'tolower($1)=="docker-upload-uuid"{sub(/\r/,"",$2); print $2}' "$WORK/post.hdr")
[ -n "$LOC" ] || fail_infra "202 without a Location header"
echo "202 Location: $LOC"
echo "Docker-Upload-UUID: $UUID"

step "generate chunks and digest (chunk1=${CHUNK1_KIB}KiB chunk2=${CHUNK2_KIB}KiB)"
CHUNK1="$WORK/chunk1.bin"
CHUNK2="$WORK/chunk2.bin"
WHOLE="$WORK/whole.bin"
python3 - "$CHUNK1" "$CHUNK2" "$WHOLE" "$CHUNK1_BYTES" "$CHUNK2_BYTES" <<'PY' || fail_infra "chunk generation"
import os, sys
c1, c2, whole, n1, n2 = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4]), int(sys.argv[5])
a, b = os.urandom(n1), os.urandom(n2)
open(c1, "wb").write(a)
open(c2, "wb").write(b)
open(whole, "wb").write(a + b)
PY
DIG="sha256:$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WHOLE")"
echo "expected digest: $DIG"

step "PATCH chunk1 (Content-Range: 0-$((CHUNK1_BYTES - 1))) — pre-restart anchor"
CODE=$(curl -sS -o /dev/null -D "$WORK/patch1.hdr" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X PATCH \
    -H 'Content-Type: application/octet-stream' \
    -H "Content-Range: 0-$((CHUNK1_BYTES - 1))" \
    --data-binary @"$CHUNK1" "$BASE$LOC" || true)
RANGE1=$(awk -F': ' 'tolower($1)=="range"{sub(/\r/,"",$2); print $2}' "$WORK/patch1.hdr")
[ "$CODE" = "202" ] || fail_infra "PATCH chunk1: want 202, got $CODE"
echo "PATCH chunk1: 202, Range: $RANGE1"
[ "$RANGE1" = "$WANT_RANGE" ] || fail_infra "PATCH chunk1 Range: want $WANT_RANGE, got $RANGE1"

step "GET upload URL (pre-restart sanity: 204 + Range $WANT_RANGE)"
CODE=$(curl -sS -o /dev/null -D "$WORK/get0.hdr" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" "$BASE$LOC" || true)
RANGE0=$(awk -F': ' 'tolower($1)=="range"{sub(/\r/,"",$2); print $2}' "$WORK/get0.hdr")
echo "GET (pre-restart): $CODE, Range: $RANGE0"
if [ "$CODE" != "204" ] || [ "$RANGE0" != "$WANT_RANGE" ]; then
    echo "  NOTE: offset-query leg already broken BEFORE the restart (DU-01)."
fi

step "stop the server: $STOP"
if [ "$STOP" = "kill" ]; then
    kill -9 "$SERVE_PID" 2>/dev/null || true
    wait "$SERVE_PID" 2>/dev/null || true
    echo "kill -9 delivered (crash semantics)"
else
    kill -TERM "$SERVE_PID" 2>/dev/null || true
    wait "$SERVE_PID" 2>/dev/null || true
    echo "SIGTERM delivered; graceful exit (compose restart 口径)"
fi
SERVE_PID=""

step "boot #2 on the SAME data dir ($DATA_DIR)"
start_server
echo "server pid: $SERVE_PID (restarted)"

step "GET upload URL after restart — THE resume observation (want 204 + Range: $WANT_RANGE)"
CODE=$(curl -sS -o "$WORK/get1.body" -D "$WORK/get1.hdr" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" "$BASE$LOC" || true)
RANGE1=$(awk -F': ' 'tolower($1)=="range"{sub(/\r/,"",$2); print $2}' "$WORK/get1.hdr")
echo "GET (post-restart): $CODE, Range: ${RANGE1:-<none>}"
echo "body: $(head -c 200 "$WORK/get1.body")"
if [ "$CODE" != "204" ] || [ "$RANGE1" != "$WANT_RANGE" ]; then
    # Diagnostic: show what a resumed PATCH would see on this build (not a
    # verdict of its own — the GET above already failed the spec's resume
    # premise).
    PCODE=$(curl -sS -o "$WORK/pdiag.body" -w '%{http_code}' \
        -u "admin:$ADMIN_PW" -X PATCH \
        -H 'Content-Type: application/octet-stream' \
        -H "Content-Range: $C2_START-$C2_END" \
        --data-binary @"$CHUNK2" "$BASE$LOC" || true)
    echo "diagnostic PATCH (Content-Range: $C2_START-$C2_END): $PCODE"
    echo "body: $(head -c 200 "$WORK/pdiag.body")"
    red "GET upload URL after restart: want 204 + Range: $WANT_RANGE, got $CODE (Range: ${RANGE1:-<none>})"
fi
echo "resume offset authoritative: Range: $RANGE1"

step "PATCH chunk2 (Content-Range: $C2_START-$C2_END)"
CODE=$(curl -sS -o /dev/null -D "$WORK/patch2.hdr" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X PATCH \
    -H 'Content-Type: application/octet-stream' \
    -H "Content-Range: $C2_START-$C2_END" \
    --data-binary @"$CHUNK2" "$BASE$LOC" || true)
RANGE2=$(awk -F': ' 'tolower($1)=="range"{sub(/\r/,"",$2); print $2}' "$WORK/patch2.hdr")
echo "PATCH chunk2: $CODE, Range: $RANGE2"
if [ "$CODE" != "202" ] || [ "$RANGE2" != "0-$C2_END" ]; then
    red "PATCH chunk2: want 202 + Range: 0-$C2_END, got $CODE (Range: $RANGE2)"
fi

step "PUT ?digest= finalize"
CODE=$(curl -sS -o "$WORK/put.body" -D "$WORK/put.hdr" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X PUT "$BASE$LOC?digest=$DIG" || true)
echo "PUT finalize: $CODE"
[ "$CODE" = "201" ] || red "PUT ?digest=: want 201, got $CODE: $(head -c 200 "$WORK/put.body")"

step "GET blob + bitwise verification"
DL="$WORK/download.bin"
CODE=$(curl -sS -o "$DL" -D "$WORK/getblob.hdr" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" "$BASE/v2/$REPO/$IMAGE/blobs/$DIG" || true)
echo "GET blob: $CODE"
[ "$CODE" = "200" ] || red "GET blob: want 200, got $CODE"
if cmp -s "$DL" "$WHOLE"; then
    DL_SHA=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$DL")
    echo "bitwise: downloaded blob == chunk1+chunk2 ($DL_SHA)"
else
    red "downloaded blob differs bitwise from chunk1+chunk2"
fi

echo ""
echo "m7-resume-probe: GREEN — $STOP restart resume works end to end (FR-67 V15/V16 shape)"
