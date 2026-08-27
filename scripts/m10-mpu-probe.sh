#!/bin/sh
# BinFlow M10 MPU REST probe (T-289, PRD FR-90 / architecture section 15.4;
# the S3 real leg of the /api/v1/uploads six-endpoint plane).
#
# Boots TWO scratch binflow-server instances against a running MinIO
# (started outside the probe — see the header of the S3 leg):
#
#   FILESTORE instance (default disk backend): the honesty matrix — every
#   one of the six endpoints (create/config/urlPart/status/complete/abort)
#   plus the urlPart PUT target answers 501 text/plain
#   "not supported on this backend", never a 404 disguise (FR-90-AC3).
#
#   S3 instance (storage.backend=s3): the full chain (FR-90-AC1 shape) —
#     create (partSizeMB=5, clamped-echo assertions)
#     urlPart -> PUT 3 parts (5MiB + 5MiB + 1MiB short final)
#     status progress after every part + the bare list form
#     complete: wrong sha256 -> 409 (checksum gate), right sha256 -> 201,
#               artifact GET sha256-reconciled byte for byte
#     abort: mid-upload session discarded, status 404, artifact 404
#     kill -9 + restart: status 404 — the REST-plane registry posture.
#               The §11.31 ENGINE debt is PAID (T-323: upload ids land in
#               upload_sessions and S3Engine.ResumeSession rebuilds via
#               ListParts); what still 404s here is the /api/v1/uploads
#               plane's in-process mpuRegistry (protocol state — repo/path/
#               part accounting — has no persisted home yet, the T-209
#               N6/O-2-class visibility gap). Flipping THIS leg needs the
#               httpapi lazy-rebuild + the assembler's Sessions wiring; see
#               reports/agents/T-323.md's intersection register.
#     docker push of a 20MiB image (> the 16MiB default part size, so the
#               layer lands as a real multipart upload) + pull roundtrip
#
#   B5 assertions (T-289 review): with --mc-cmd set (e.g. "docker exec
#   binflow-mpu-minio mc"), the probe asserts S3-SIDE multipart
#   reclamation through `mc ls --incomplete` — an object listing cannot
#   see in-progress MPUs, the review's finding against the first round's
#   evidence. The wrong-sha complete and the abort legs must leave ZERO
#   in-progress upload for their session id, and at end of run the ONLY
#   in-progress upload in the bucket is the kill -9 leg's documented
#   orphan (reclaimed by the engine's TTL sweep after restart). Run
#   against a FRESH bucket: pre-existing orphans from older builds fail
#   the end-of-run assertion honestly.
#
# Exit codes:
#   0  GREEN — every leg passed
#   1  RED   — a behavior observation failed (the field this probe pins)
#   2  INFRA — probe infrastructure error (binary, boot, MinIO, tools)
#
# Usage: scripts/m10-mpu-probe.sh [--endpoint URL] [--access-key ID]
#                                 [--secret KEY] [--bucket NAME]
#                                 [--mc-cmd "docker exec NAME mc"]
#                                 [--mc-endpoint URL] [--skip-docker]
#                                 [path-to-binary]
# Defaults match a MinIO started with:
#   docker run -d --rm --name binflow-mpu-minio -p 127.0.0.1:29000:9000 \
#     -e MINIO_ROOT_USER=mpuadmin -e MINIO_ROOT_PASSWORD=mpuadmin-secret \
#     minio/minio:latest server /data
#   docker exec binflow-mpu-minio sh -c \
#     'mc alias set local http://127.0.0.1:9000 mpuadmin mpuadmin-secret && \
#      mc mb local/binflow-mpu'
#
# POSIX sh (macOS bash-3.2-as-sh / dash / busybox ash); macOS + Linux.

set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)

ENDPOINT="http://127.0.0.1:29000"
ACCESS_KEY="mpuadmin"
SECRET="mpuadmin-secret"
BUCKET="binflow-mpu"
MC=""
MC_ENDPOINT="http://127.0.0.1:9000"
SKIP_DOCKER=0
BIN=""
while [ $# -gt 0 ]; do
    case "$1" in
    --endpoint)  ENDPOINT=$2; shift 2 ;;
    --endpoint=*) ENDPOINT=${1#--endpoint=}; shift ;;
    --access-key) ACCESS_KEY=$2; shift 2 ;;
    --access-key=*) ACCESS_KEY=${1#--access-key=}; shift ;;
    --secret)    SECRET=$2; shift 2 ;;
    --secret=*)  SECRET=${1#--secret=}; shift ;;
    --bucket)    BUCKET=$2; shift 2 ;;
    --bucket=*)  BUCKET=${1#--bucket=}; shift ;;
    --mc-cmd)    MC=$2; shift 2 ;;
    --mc-cmd=*)  MC=${1#--mc-cmd=}; shift ;;
    --mc-endpoint) MC_ENDPOINT=$2; shift 2 ;;
    --mc-endpoint=*) MC_ENDPOINT=${1#--mc-endpoint=}; shift ;;
    --skip-docker) SKIP_DOCKER=1; shift ;;
    -h|--help)   sed -n '2,40p' "$0"; exit 0 ;;
    -*) echo "m10-mpu-probe: unknown option $1" >&2; exit 2 ;;
    *)  BIN=$1; shift ;;
    esac
done

REPO="mpu-probe"
ADMIN_PW="mpu-probe-admin-pw"
PART_MB=5
PART_BYTES=$((PART_MB * 1024 * 1024))
TOTAL_BYTES=$((11 * 1024 * 1024)) # 5 + 5 + 1 MiB

fail_infra() {
    echo "m10-mpu-probe: INFRA FAIL: $1" >&2
    [ -f "$S3_LOG" ] && tail -20 "$S3_LOG" >&2
    exit 2
}
red() {
    echo ""
    echo "m10-mpu-probe: RED — $1" >&2
    exit 1
}
step() { echo "---- $1"; }

for tool in curl python3; do
    command -v "$tool" >/dev/null 2>&1 || fail_infra "$tool is required"
done

# ---- build (or reuse a FRESH binary; the m7-resume-probe discipline) ----
BUILD_INPUTS="$ROOT/cmd $ROOT/internal $ROOT/go.mod $ROOT/go.sum $ROOT/tools.go"
if [ "$BIN" = "" ]; then
    BIN="$ROOT/bin/binflow-server"
fi
NEEDS_BUILD=0
if [ ! -x "$BIN" ]; then
    NEEDS_BUILD=1
elif [ "${BIN#"$ROOT"/}" != "$BIN" ]; then
    STALE_SRC=$(find $BUILD_INPUTS -type f -newer "$BIN" -print 2>/dev/null | head -n 1)
    [ -n "$STALE_SRC" ] && NEEDS_BUILD=1
fi
if [ "$NEEDS_BUILD" = "1" ]; then
    step "building binflow-server (make build)"
    (cd "$ROOT" && make build) || fail_infra "make build"
fi
[ -x "$BIN" ] || fail_infra "binary $BIN missing after build"

# ---- workspace --------------------------------------------------------------
WORK=$(mktemp -d "${TMPDIR:-/tmp}/binflow-m10-mpu.XXXXXX")
S3_LOG="$WORK/s3-serve.log"
DISK_LOG="$WORK/disk-serve.log"
S3_PID=""; DISK_PID=""

cleanup() {
    for pid in "$S3_PID" "$DISK_PID"; do
        [ -n "$pid" ] && kill -9 "$pid" 2>/dev/null || true
        [ -n "$pid" ] && wait "$pid" 2>/dev/null || true
    done
    rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

free_port() {
    python3 -c 'import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

start_server() { # $1 = config file, $2 = log file; sets PID via stdout
    PORT=$(free_port) || fail_infra "cannot pick an ephemeral port"
    BINFLOW_ADMIN_PASSWORD="$ADMIN_PW" \
        "$BIN" serve -c "$1" >>"$2" 2>&1 &
    PID=$!
    n=0
    BASE="http://127.0.0.1:$PORT"
    while [ "$n" -lt 200 ]; do
        if BODY=$(curl -sf "$BASE/binflow/api/system/ping" 2>/dev/null) && [ "$BODY" = "OK" ]; then
            return 0
        fi
        kill -0 "$PID" 2>/dev/null || fail_infra "server exited during boot (see $2)"
        n=$((n + 1)); sleep 0.1
    done
    fail_infra "server never answered ping on $BASE (see $2)"
}

# ---- MinIO must already be up (the bucket is part of the fixture) -----------
step "MinIO endpoint $ENDPOINT bucket $BUCKET"
curl -sf "$ENDPOINT/minio/health/live" >/dev/null 2>&1 \
    || fail_infra "MinIO is not answering $ENDPOINT/minio/health/live (start it per the usage header)"

# ---- B5 helpers: S3-side in-progress MPU visibility via mc ------------------
# An object listing (mc ls) cannot see in-progress multipart uploads — the
# T-289 review's finding against round 1's evidence. --incomplete lists
# them; the assertions grep the session id out of the object key
# (sessions/<uuid>/data). Without --mc-cmd these are no-ops (noted once).
if [ -n "$MC" ]; then
    $MC alias set local "$MC_ENDPOINT" "$ACCESS_KEY" "$SECRET" >/dev/null 2>&1 \
        || fail_infra "mc alias set failed (is --mc-endpoint reachable from wherever mc runs?)"
    echo "mc wired: in-progress MPU assertions active"
else
    echo "mc not wired (--mc-cmd empty): S3-side MPU assertions SKIPPED"
fi

incomplete_list() {
    [ -n "$MC" ] || return 1
    $MC ls --incomplete "local/$BUCKET" 2>/dev/null || true
}

assert_incomplete_has() { # $1 = sessionId, $2 = leg — positive control
    [ -n "$MC" ] || return 0
    OUT=$(incomplete_list)
    case "$OUT" in
    *"$1"*) echo "  in-progress MPU visible for $2 ($1)" ;;
    *) red "$2: expected an in-progress multipart upload for session $1, mc sees none — the assertion itself is broken or the upload never opened" ;;
    esac
}

assert_incomplete_gone() { # $1 = sessionId, $2 = leg — THE B5 assertion
    [ -n "$MC" ] || return 0
    OUT=$(incomplete_list)
    case "$OUT" in
    *"$1"*) red "$2: session $1 left its S3 multipart upload in progress: $OUT" ;;
    *) echo "  zero in-progress MPU for $2 ($1) — reclaimed" ;;
    esac
}

# ---- filestore instance: the FR-90-AC3 honesty matrix -----------------------
step "boot filestore instance (the 501 matrix leg)"
DISK_PORT=$(free_port) || fail_infra "port"
DISK_CFG="$WORK/disk.yaml"
cat > "$DISK_CFG" <<EOF
server:
  listen: 127.0.0.1:$DISK_PORT
storage:
  data_dir: $WORK/disk-data
EOF
BINFLOW_ADMIN_PASSWORD="$ADMIN_PW" "$BIN" serve -c "$DISK_CFG" >>"$DISK_LOG" 2>&1 &
DISK_PID=$!
DISK_BASE="http://127.0.0.1:$DISK_PORT"
n=0; while [ "$n" -lt 200 ]; do
    curl -sf "$DISK_BASE/binflow/api/system/ping" >/dev/null 2>&1 && break
    kill -0 "$DISK_PID" 2>/dev/null || fail_infra "filestore server exited during boot"
    n=$((n + 1)); sleep 0.1
done
[ "$n" -lt 200 ] || fail_infra "filestore server never answered ping"

step "filestore: all six endpoints + part target answer the honest 501"
for LEG in "POST create" "POST config" "GET urlPart/some-id/1" "GET status/some-id" "GET status" "POST complete/some-id" "POST abort/some-id" "PUT part/some-id/1"; do
    METHOD=${LEG%% *}; PATHPART=${LEG#* }
    CODE=$(curl -sS -o "$WORK/501.body" -w '%{http_code}' \
        -u "admin:$ADMIN_PW" -X "$METHOD" \
        -H 'Content-Type: application/json' \
        -d '{"repoKey":"r","path":"a.bin"}' \
        "$DISK_BASE/binflow/api/v1/uploads/$PATHPART" || true)
    BODY=$(cat "$WORK/501.body")
    [ "$CODE" = "501" ] || red "filestore $METHOD /uploads/$PATHPART = $CODE, want 501 (body: $BODY)"
    case "$BODY" in
    *"not supported on this backend"*) ;;
    *) red "filestore $METHOD /uploads/$PATHPART body lacks the honest refusal: $BODY" ;;
    esac
    case "$BODY" in
    *S3*) ;;
    *) red "filestore $METHOD /uploads/$PATHPART body does not name S3: $BODY" ;;
    esac
    echo "  $METHOD /uploads/$PATHPART -> 501 (plain text, honest)"
done
kill -9 "$DISK_PID" 2>/dev/null || true; wait "$DISK_PID" 2>/dev/null || true; DISK_PID=""

# ---- S3 instance ------------------------------------------------------------
step "boot S3 instance (backend=s3 -> $ENDPOINT/$BUCKET)"
S3_PORT=$(free_port) || fail_infra "port"
S3_CFG="$WORK/s3.yaml"
cat > "$S3_CFG" <<EOF
server:
  listen: 127.0.0.1:$S3_PORT
storage:
  data_dir: $WORK/s3-data
  backend: s3
  s3:
    bucket: $BUCKET
    region: us-east-1
    endpoint: $ENDPOINT
    access_key_id: $ACCESS_KEY
    use_path_style: true
EOF
BINFLOW_ADMIN_PASSWORD="$ADMIN_PW" \
BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY="$SECRET" \
    "$BIN" serve -c "$S3_CFG" >>"$S3_LOG" 2>&1 &
S3_PID=$!
BASE="http://127.0.0.1:$S3_PORT"
n=0; while [ "$n" -lt 300 ]; do
    curl -sf "$BASE/binflow/api/system/ping" >/dev/null 2>&1 && break
    kill -0 "$S3_PID" 2>/dev/null || fail_infra "S3 server exited during boot (bucket present?)"
    n=$((n + 1)); sleep 0.1
done
[ "$n" -lt 300 ] || fail_infra "S3 server never answered ping"
echo "S3 instance pid $S3_PID on $BASE"

step "fixture: generic repository '$REPO'"
CODE=$(curl -sS -o "$WORK/repo.body" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X PUT "$BASE/binflow/api/repositories/$REPO" \
    -H 'Content-Type: application/json' \
    -d '{"rclass":"local","packageType":"generic"}' || true)
[ "$CODE" = "200" ] || fail_infra "create generic repo: want 200, got $CODE: $(cat "$WORK/repo.body")"

step "generate the 11MiB payload (parts: 5MiB + 5MiB + 1MiB short final)"
python3 - "$WORK/p1.bin" "$WORK/p2.bin" "$WORK/p3.bin" "$WORK/whole.bin" <<'PY' || fail_infra "payload generation"
import os, sys
p1, p2, p3, whole = sys.argv[1:5]
a, b, c = os.urandom(5 << 20), os.urandom(5 << 20), os.urandom(1 << 20)
open(p1, "wb").write(a); open(p2, "wb").write(b); open(p3, "wb").write(c)
open(whole, "wb").write(a + b + c)
PY
WHOLE_SHA=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/whole.bin")
echo "expected sha256: $WHOLE_SHA"

mpu_create() { # $1 = path, $2 = partSizeMB -> echoes sessionId
    CODE=$(curl -sS -o "$WORK/create.body" -w '%{http_code}' \
        -u "admin:$ADMIN_PW" -X POST "$BASE/binflow/api/v1/uploads/create" \
        -H 'Content-Type: application/json' \
        -d "{\"repoKey\":\"$REPO\",\"path\":\"$1\",\"partSizeMB\":$2}" || true)
    [ "$CODE" = "201" ] || red "create $1 = $CODE: $(cat "$WORK/create.body")"
    python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["sessionId"])' "$WORK/create.body"
}

put_part() { # $1 = sessionId, $2 = part number, $3 = file
    CODE=$(curl -sS -o "$WORK/part.body" -w '%{http_code}' \
        -u "admin:$ADMIN_PW" -X PUT \
        -H 'Content-Type: application/octet-stream' \
        --data-binary @"$3" \
        "$BASE/binflow/api/v1/uploads/part/$1/$2" || true)
    [ "$CODE" = "202" ] || red "part $2 PUT = $CODE: $(cat "$WORK/part.body")"
}

status_field() { # $1 = sessionId, $2 = json key
    curl -sS -u "admin:$ADMIN_PW" "$BASE/binflow/api/v1/uploads/status/$1" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$2"
}

# --- leg 1: the checksum gate consumes the session on a wrong sha256 --------
step "wrong-sha256 complete: 409 and the session is consumed (S3 MPU reclaimed)"
GATE_ID=$(mpu_create "big/gate.bin" 5)
put_part "$GATE_ID" 1 "$WORK/p3.bin" # short part closes the stream
assert_incomplete_has "$GATE_ID" "wrong-sha leg before complete"
CODE=$(curl -sS -o "$WORK/gate.body" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X POST "$BASE/binflow/api/v1/uploads/complete/$GATE_ID" \
    -H 'Content-Type: application/json' \
    -d '{"sha256":"'$(python3 -c 'print("0"*64)')'"}' || true)
[ "$CODE" = "409" ] || red "wrong-sha complete = $CODE, want 409: $(cat "$WORK/gate.body")"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/api/v1/uploads/status/$GATE_ID" || true)
[ "$CODE" = "404" ] || red "status after failed complete = $CODE, want 404"
assert_incomplete_gone "$GATE_ID" "wrong-sha complete (B5: failLocked reclaims)"
echo "409 + status 404 confirmed"

# --- leg 2: the full good chain ----------------------------------------------
step "create -> urlPart -> 3 parts -> progress -> complete -> byte reconciliation"
SID=$(mpu_create "big/blob.bin" 5)
PART_SIZE_ECHO=$(python3 -c 'import json; print(json.load(open("'"$WORK/create.body"'"))["partSizeBytes"])')
[ "$PART_SIZE_ECHO" = "$PART_BYTES" ] || red "partSizeBytes echo = $PART_SIZE_ECHO, want $PART_BYTES"

CODE=$(curl -sS -o "$WORK/urlpart.body" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" "$BASE/binflow/api/v1/uploads/urlPart/$SID/2" || true)
[ "$CODE" = "200" ] || red "urlPart = $CODE: $(cat "$WORK/urlpart.body")"
URL_ECHO=$(python3 -c 'import json; print(json.load(open("'"$WORK/urlpart.body"'"))["url"])' 2>/dev/null || true)
case "$URL_ECHO" in
*/api/v1/uploads/part/$SID/2) ;;
"") ;;
*) red "urlPart handed out an unexpected target: $URL_ECHO" ;;
esac
echo "urlPart(2) -> $URL_ECHO"

put_part "$SID" 1 "$WORK/p1.bin"
GOT=$(status_field "$SID" receivedBytes)
[ "$GOT" = "$PART_BYTES" ] || red "progress after part 1: receivedBytes = $GOT, want $PART_BYTES"
put_part "$SID" 2 "$WORK/p2.bin"
GOT=$(status_field "$SID" receivedBytes)
[ "$GOT" = "$((PART_BYTES * 2))" ] || red "progress after part 2: receivedBytes = $GOT, want $((PART_BYTES * 2))"
put_part "$SID" 3 "$WORK/p3.bin"
GOT=$(status_field "$SID" receivedBytes)
[ "$GOT" = "$TOTAL_BYTES" ] || red "progress after part 3: receivedBytes = $GOT, want $TOTAL_BYTES"
GOT=$(status_field "$SID" state)
[ "$GOT" = "awaiting-complete" ] || red "state after the short final part = $GOT, want awaiting-complete"
echo "progress: $PART_BYTES -> $((PART_BYTES * 2)) -> $TOTAL_BYTES bytes"

step "bare status list form"
curl -sS -u "admin:$ADMIN_PW" "$BASE/binflow/api/v1/uploads/status" > "$WORK/list.body"
python3 - "$WORK/list.body" "$SID" <<'PY' || red "bare status did not carry the live session: $(cat "$WORK/list.body")"
import json, sys
lst = json.load(open(sys.argv[1]))
assert any(s.get("sessionId") == sys.argv[2] for s in lst), lst
PY
echo "list form carries the live session"

step "complete (sha256 gate) -> 201"
CODE=$(curl -sS -o "$WORK/complete.body" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X POST "$BASE/binflow/api/v1/uploads/complete/$SID" \
    -H 'Content-Type: application/json' \
    -d '{"sha256":"'"$WHOLE_SHA"'"}' || true)
[ "$CODE" = "201" ] || red "complete = $CODE: $(cat "$WORK/complete.body")"
SIZE_ECHO=$(python3 -c 'import json; print(json.load(open("'"$WORK/complete.body"'"))["size"])' 2>/dev/null || echo "?")
[ "$SIZE_ECHO" = "$TOTAL_BYTES" ] || red "complete size echo = $SIZE_ECHO, want $TOTAL_BYTES"

step "artifact GET: sha256 reconciliation, byte for byte"
curl -sS -u "admin:$ADMIN_PW" -o "$WORK/download.bin" \
    "$BASE/binflow/$REPO/big/blob.bin" || red "artifact GET failed"
DL_SHA=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/download.bin")
[ "$DL_SHA" = "$WHOLE_SHA" ] || red "downloaded sha256 $DL_SHA != uploaded $WHOLE_SHA"
cmp -s "$WORK/download.bin" "$WORK/whole.bin" || red "downloaded bytes differ from the upload"
echo "GET $REPO/big/blob.bin: 200, sha256 + bytes reconciled"

# --- leg 3: abort discards ----------------------------------------------------
step "abort: session gone, artifact never lands (S3 MPU reclaimed)"
AB_ID=$(mpu_create "big/gone.bin" 5)
put_part "$AB_ID" 1 "$WORK/p1.bin"
assert_incomplete_has "$AB_ID" "abort leg before abort"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    -X POST "$BASE/binflow/api/v1/uploads/abort/$AB_ID" || true)
[ "$CODE" = "204" ] || red "abort = $CODE, want 204"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/api/v1/uploads/status/$AB_ID" || true)
[ "$CODE" = "404" ] || red "status after abort = $CODE, want 404"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/$REPO/big/gone.bin" || true)
[ "$CODE" = "404" ] || red "aborted artifact GET = $CODE, want 404 (blob invisible)"
assert_incomplete_gone "$AB_ID" "abort (B5: Session.Abort reclaims)"
echo "abort -> 204, status 404, artifact 404"

# --- leg 4: kill -9 restart — the REST registry posture (see header) ---------
step "kill -9 + restart: status 404 (REST registry is process state; engine-side resume landed with T-323)"
RST_ID=$(mpu_create "big/restart.bin" 5)
put_part "$RST_ID" 1 "$WORK/p1.bin"
kill -9 "$S3_PID" 2>/dev/null || true; wait "$S3_PID" 2>/dev/null || true; S3_PID=""
BINFLOW_ADMIN_PASSWORD="$ADMIN_PW" \
BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY="$SECRET" \
    "$BIN" serve -c "$S3_CFG" >>"$S3_LOG" 2>&1 &
S3_PID=$!
n=0; while [ "$n" -lt 300 ]; do
    curl -sf "$BASE/binflow/api/system/ping" >/dev/null 2>&1 && break
    kill -0 "$S3_PID" 2>/dev/null || fail_infra "S3 server exited during restart boot"
    n=$((n + 1)); sleep 0.1
done
[ "$n" -lt 300 ] || fail_infra "S3 server never answered ping after restart"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/api/v1/uploads/status/$RST_ID" || true)
# The assertion direction is DELIBERATE and still honest post-T-323: the
# engine-level §11.31 debt is paid (upload ids in upload_sessions +
# ResumeSession via ListParts — proven by the kill -9 leg of the storage
# suite), but THIS plane's mpuRegistry is process state and its protocol
# coordinates (repoKey/path/part accounting) have no persisted home. The
# 404 flips when the httpapi lazy-rebuild + assembler Sessions wiring land
# (the registered T-323 intersection).
[ "$CODE" = "404" ] || red "post-restart status = $CODE, want the REST-registry 404 (see T-323 intersection register)"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/$REPO/big/blob.bin" || true)
[ "$CODE" = "200" ] || red "committed artifact missing after restart: $CODE"
echo "post-restart: session 404 (REST registry posture), committed artifact still 200"

# End-of-run B5 audit: with the kill -9 orphan documented, the ONLY
# in-progress multipart upload left in the bucket is that restarted
# session's — every other leg (wrong-sha complete, abort, complete) must
# have reclaimed its MPU. Pre-existing orphans from older builds fail
# here honestly: run against a fresh bucket.
if [ -n "$MC" ]; then
    step "end-of-run audit: only the kill -9 leg's orphan MPU remains"
    OUT=$(incomplete_list)
    COUNT=$(printf '%s\n' "$OUT" | grep -c . || true)
    if [ "$COUNT" = "0" ]; then
        red "end-of-run audit: expected the kill -9 leg's orphan MPU, found none — the orphan premise itself broke"
    fi
    LEAK=$(printf '%s\n' "$OUT" | grep -v "$RST_ID" | grep -c . || true)
    if [ "$LEAK" != "0" ]; then
        red "end-of-run audit: $LEAK in-progress MPU(s) beyond the kill -9 orphan: $OUT"
    fi
    echo "in-progress MPUs at end of run: $COUNT (the documented restart orphan $RST_ID only)"
fi

# --- leg 5: docker push of a >part-size image (multipart regression) ---------
# The T-103/T-104 pattern: the host's Docker Desktop daemon lives in a VM
# whose 127.0.0.1 is not the host's loopback, so the docker CLI rides a
# docker:dind container with --insecure-registry=host.docker.internal:<port>
# — zero changes to the host daemon's configuration.
if [ "$SKIP_DOCKER" = "1" ]; then
    echo "---- docker leg SKIPPED (--skip-docker)"
else
    command -v docker >/dev/null 2>&1 || { echo "---- docker leg SKIPPED (no docker CLI)"; exit 0; }
    step "docker push 20MiB image (layer > 16MiB part size: a real multipart upload)"
    CODE=$(curl -sS -o "$WORK/drepo.body" -w '%{http_code}' \
        -u "admin:$ADMIN_PW" -X PUT "$BASE/binflow/api/repositories/mpu-docker" \
        -H 'Content-Type: application/json' \
        -d '{"rclass":"local","packageType":"docker"}' || true)
    [ "$CODE" = "200" ] || fail_infra "create docker repo: want 200, got $CODE: $(cat "$WORK/drepo.body")"
    python3 - "$WORK/img-root" <<'PY' || fail_infra "image rootfs generation"
import os, sys
d = sys.argv[1]
os.makedirs(d, exist_ok=True)
open(os.path.join(d, "payload.bin"), "wb").write(os.urandom(20 << 20))
PY
    (cd "$WORK/img-root" && tar -cf "$WORK/img.tar" .) || fail_infra "image tar"
    DIND="binflow-mpu-dind"
    REG="host.docker.internal:$S3_PORT"
    docker rm -f "$DIND" >/dev/null 2>&1 || true
    docker run -d --rm --privileged --name "$DIND" \
        docker:dind --insecure-registry="$REG" >/dev/null 2>&1 || fail_infra "dind start"
    n=0; while [ "$n" -lt 60 ]; do
        docker exec "$DIND" docker info >/dev/null 2>&1 && break
        n=$((n + 1)); sleep 0.5
    done
    [ "$n" -lt 60 ] || fail_infra "dind never became ready"
    dind() { docker exec "$DIND" docker "$@"; }
    cat "$WORK/img.tar" | docker exec -i "$DIND" docker import - "$REG/mpu-docker/big:1" >/dev/null 2>&1 \
        || fail_infra "docker import (in dind)"
    dind login "$REG" -u admin -p "$ADMIN_PW" >/dev/null 2>&1 \
        || fail_infra "docker login $REG (in dind)"
    PUSH_OUT=$(dind push "$REG/mpu-docker/big:1" 2>&1) || red "docker push failed: $PUSH_OUT"
    echo "$PUSH_OUT" | tail -1
    PUSHED_SHA=$(echo "$PUSH_OUT" | sed -n 's/.*digest: sha256:\([0-9a-f]*\).*/\1/p' | tail -1)
    [ -n "$PUSHED_SHA" ] || fail_infra "could not parse the pushed digest"
    dind rmi "$REG/mpu-docker/big:1" >/dev/null 2>&1 || fail_infra "docker rmi"
    PULL_OUT=$(dind pull "$REG/mpu-docker/big:1" 2>&1) || red "docker pull after push failed: $PULL_OUT"
    PULLED_SHA=$(dind inspect --format '{{index .RepoDigests 0}}' "$REG/mpu-docker/big:1" | sed 's/.*sha256://')
    [ "$PUSHED_SHA" = "$PULLED_SHA" ] || red "push/pull digest mismatch: $PUSHED_SHA vs $PULLED_SHA"
    echo "push -> rmi -> pull roundtrip on digest sha256:$PUSHED_SHA (dind -> $REG)"
    docker rm -f "$DIND" >/dev/null 2>&1 || true
fi

echo ""
echo "m10-mpu-probe: GREEN — filestore 501 matrix + S3 full chain + abort + restart-posture + docker legs all passed (FR-90-AC1/AC3)"
