#!/bin/sh
# BinFlow M11 MPU REST probe (T-289 born, T-332 wire-flipped per ADR-0039 /
# the 2026-08-28 user ruling item 2; the S3 real leg of the
# /api/v1/uploads six-endpoint plane on the Artifactory shape).
#
# Boots TWO scratch binflow-server instances against a running MinIO
# (started outside the probe — see the header of the S3 leg):
#
#   FILESTORE instance (default disk backend): the honesty matrix — the
#   five data endpoints (create/urlPart/status/complete/abort) plus the
#   urlPart PUT target answer 501 text/plain "not supported on this
#   backend", never a 404 disguise (FR-90-AC3); GET /config — the probe —
#   answers 200 {"supported": false} (a probe that could not say "no"
#   would be no probe).
#
#   S3 instance (storage.backend=s3), the flipped wire end to end:
#     config probe: the jfrog-cli version gate (2.50.0 -> false,
#               2.62.2/2.63.1 -> true, 2.62 -> false [shorter], dev -> true
#               [the catch arm]) + plain-agent true
#     create: POST + QueryParam (repoKey/repoPath/partSizeMB) -> 200
#               {"token": ...} — the capability credential
#     the T-304 section 1.2-B flip: create on a VIRTUAL repo resolves its
#               defaultDeploymentRepo (was the retired 400)
#     urlPart: POST ?partNumber=2 with the token -> {"url": ...} — the
#               URL is presigned-shaped (?token= capability)
#     3 part PUTs (5MiB + 5MiB + 1MiB short final), 200 the S3 PutObject
#               shape, status -> PARTS/0
#     complete?sha1= (THE ALGORITHM FLIP): 202 Accepted, then the async
#               task: status polls to FINISHED/100 carrying the
#               checksum-deploy token; the artifact is NOT a node yet —
#               the client's X-Checksum-Deploy PUT (Bearer the 5-minute
#               token) lands it; GET reconciles sha256 + bytes
#     wrong sha1: 202 then status NON_RETRYABLE_ERROR (the async gate; was
#               sync 409)
#     abort: mid-upload session discarded, status 404, artifact 404
#     kill -9 + restart: the SAME token drives the restarted process
#               (T-323R on the new wire: the binding rides the engine's
#               persisted row) — status 200 PARTS, parts 2+3,
#               complete, FINISHED, checksum-deploy, byte-for-byte GET
#     docker push of a 20MiB image (> the 16MiB default part size, so the
#               layer lands as a real multipart upload) + pull roundtrip
#
#   B5 assertions (T-289 review): with --mc-cmd set (e.g. "docker exec
#   binflow-mpu-minio mc"), the probe asserts S3-SIDE multipart
#   reclamation through `mc ls --incomplete`. The wrong-sha complete and
#   the abort legs must leave ZERO in-progress upload for their session
#   id, and at end of run the bucket holds NO in-progress upload at all.
#   Run against a FRESH bucket: pre-existing orphans from older builds
#   fail the end-of-run assertion honestly.
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

# ---- MinIO must already be up (the bucket is part of the fixture) -----------
step "MinIO endpoint $ENDPOINT bucket $BUCKET"
curl -sf "$ENDPOINT/minio/health/live" >/dev/null 2>&1 \
    || fail_infra "MinIO is not answering $ENDPOINT/minio/health/live (start it per the usage header)"

# ---- B5 helpers: S3-side in-progress MPU visibility via mc ------------------
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

# ---- filestore instance: FR-90-AC3 honesty + the config probe ----------------
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

step "filestore: five data endpoints + part target answer the honest 501"
for LEG in "POST create?repoKey=r&repoPath=a.bin" "POST urlPart?partNumber=1" "POST status" "POST complete?sha1=0000000000000000000000000000000000000000" "POST abort" "PUT part/some-id/1"; do
    METHOD=${LEG%% *}; PATHPART=${LEG#* }
    CODE=$(curl -sS -o "$WORK/501.body" -w '%{http_code}' \
        -u "admin:$ADMIN_PW" -X "$METHOD" \
        -H 'Content-Type: application/octet-stream' \
        --data-binary "probe" \
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

step "filestore: GET /config answers 200 supported:false (the probe's whole point)"
CODE=$(curl -sS -o "$WORK/cfg.body" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" "$DISK_BASE/binflow/api/v1/uploads/config" || true)
[ "$CODE" = "200" ] || red "filestore config = $CODE, want 200: $(cat "$WORK/cfg.body")"
grep -q '"supported": false' "$WORK/cfg.body" \
    || red "filestore config body = $(cat "$WORK/cfg.body"), want supported:false"
echo "  config -> 200 {\"supported\": false}"
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
WHOLE_SHA1=$(python3 -c 'import hashlib,sys; print(hashlib.sha1(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/whole.bin")
echo "expected sha256: $WHOLE_SHA / sha1: $WHOLE_SHA1"

# mpu_create: POST + QueryParam -> 200 {"token": ...} (the flipped create).
mpu_create() { # $1 = path, $2 = partSizeMB, $3 = repo (default $REPO) -> echoes the token
    MREPO=${3:-$REPO}
    CODE=$(curl -sS -o "$WORK/create.body" -w '%{http_code}' \
        -u "admin:$ADMIN_PW" -X POST \
        "$BASE/binflow/api/v1/uploads/create?repoKey=$MREPO&repoPath=$1&partSizeMB=$2" || true)
    [ "$CODE" = "200" ] || red "create $1 = $CODE: $(cat "$WORK/create.body")"
    python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["token"])' "$WORK/create.body"
}

# mpu_sid: the session id — the token's public half (S3-side keying).
mpu_sid() { # $1 = token
    printf '%s' "$1" | sed 's/.*\.//'
}

# put_part: the relay target on the TOKEN lane (the client's one credential).
put_part() { # $1 = token, $2 = part number, $3 = file
    SID=$(mpu_sid "$1")
    CODE=$(curl -sS -o "$WORK/part.body" -w '%{http_code}' \
        -X PUT -H "Authorization: Bearer $1" \
        -H 'Content-Type: application/octet-stream' \
        --data-binary @"$3" \
        "$BASE/binflow/api/v1/uploads/part/$SID/$2" || true)
    [ "$CODE" = "200" ] || red "part $2 PUT = $CODE: $(cat "$WORK/part.body")"
}

# mpu_status: POST + the token -> the task body's status field.
mpu_status() { # $1 = token
    curl -sS -X POST -H "Authorization: Bearer $1" \
        "$BASE/binflow/api/v1/uploads/status" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])'
}

# mpu_wait_status: poll the task to a terminal state (the async model).
mpu_wait_status() { # $1 = token, $2 = want ("FINISHED"|"NON_RETRYABLE_ERROR")
    n=0
    while [ "$n" -lt 300 ]; do
        GOT=$(mpu_status "$1" || true)
        [ "$GOT" = "$2" ] && { echo "$GOT"; return 0; }
        case "$GOT" in
        NON_RETRYABLE_ERROR|FINISHED) red "task reached $GOT, wanted $2" ;;
        esac
        n=$((n + 1)); sleep 0.1
    done
    red "status never reached $2 (last: $GOT)"
}

# --- leg 0: the config probe + the jfrog-cli version gate --------------------
step "config probe: version gate + backend verdict"
cfg_probe() { # $1 = user agent -> "true"/"false"
    curl -sS -u "admin:$ADMIN_PW" -A "$1" \
        "$BASE/binflow/api/v1/uploads/config" \
        | python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin)["supported"]))'
}
for UA in "jfrog-cli-go/2.50.0:false" "jfrog-cli-go/2.62:false" \
          "jfrog-cli-go/2.62.2:true" "jfrog-cli-go/2.63.1:true" \
          "jfrog-cli-go/dev:true" "curl/8.0:true"; do
    U=${UA%%:*}; WANT=${UA##*:}
    GOT=$(cfg_probe "$U")
    [ "$GOT" = "$WANT" ] || red "config gate for $U = $GOT, want $WANT"
done
echo "  version gate + backend verdict all as ruled"

# --- leg 0b: the T-304 section 1.2-B flip — virtual defaultDeploymentRepo -----
step "create on a virtual repo resolves defaultDeploymentRepo (was the retired 400)"
CODE=$(curl -sS -o "$WORK/vm.body" -w '%{http_code}' \
    -u "admin:$ADMIN_PW" -X PUT "$BASE/binflow/api/repositories/mpu-virtual" \
    -H 'Content-Type: application/json' \
    -d "{\"rclass\":\"virtual\",\"packageType\":\"generic\",\"repositories\":[\"$REPO\"],\"defaultDeploymentRepo\":\"$REPO\"}" || true)
[ "$CODE" = "200" ] || fail_infra "create virtual repo: want 200, got $CODE: $(cat "$WORK/vm.body")"
VTOK=$(mpu_create "via-virtual-probe.bin" 5 "mpu-virtual")
[ -n "$VTOK" ] || red "virtual create carried no token"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST -H "Authorization: Bearer $VTOK" \
    "$BASE/binflow/api/v1/uploads/abort" || true)
[ "$CODE" = "204" ] || red "virtual-leg abort = $CODE, want 204 (every opened session terminates)"
echo "  virtual -> default member resolution OK (token minted, session aborted)"

# --- leg 1: the async checksum gate — wrong sha1 -> 202 then task Failed ------
step "wrong-sha1 complete: 202, then status Failed (the async gate; S3 reclaimed)"
GATE_TOKEN=$(mpu_create "big/gate.bin" 5)
GATE_ID=$(mpu_sid "$GATE_TOKEN")
put_part "$GATE_TOKEN" 1 "$WORK/p3.bin" # short part closes the stream
assert_incomplete_has "$GATE_ID" "wrong-sha leg before complete"
CODE=$(curl -sS -o "$WORK/gate.body" -w '%{http_code}' \
        -X POST -H "Authorization: Bearer $GATE_TOKEN" \
        "$BASE/binflow/api/v1/uploads/complete?sha1=$(python3 -c 'print("0"*40)')" || true)
[ "$CODE" = "202" ] || red "wrong-sha1 complete = $CODE, want 202: $(cat "$WORK/gate.body")"
GOT=$(mpu_wait_status "$GATE_TOKEN" "NON_RETRYABLE_ERROR")
echo "task -> $GOT"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST -H "Authorization: Bearer $GATE_TOKEN" \
    "$BASE/binflow/api/v1/uploads/status" || true)
[ "$CODE" = "200" ] || red "status after failed task = $CODE, want 200 (observable)"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/$REPO/big/gate.bin" || true)
[ "$CODE" = "404" ] || red "failed artifact GET = $CODE, want 404"
assert_incomplete_gone "$GATE_ID" "wrong-sha1 finish (B5: the failed Commit reclaims)"
echo "202 + task NON_RETRYABLE_ERROR + no artifact confirmed"

# --- leg 2: the full good chain ----------------------------------------------
step "create -> urlPart -> 3 token-lane parts -> complete?sha1= 202 -> Finished -> client checksum-deploy"
TOKEN=$(mpu_create "big/blob.bin" 5)
SID=$(mpu_sid "$TOKEN")

CODE=$(curl -sS -o "$WORK/urlpart.body" -w '%{http_code}' \
    -X POST -H "Authorization: Bearer $TOKEN" \
    "$BASE/binflow/api/v1/uploads/urlPart?partNumber=2" || true)
[ "$CODE" = "200" ] || red "urlPart = $CODE: $(cat "$WORK/urlpart.body")"
URL_ECHO=$(python3 -c 'import json; print(json.load(open("'"$WORK/urlpart.body"'"))["url"])' 2>/dev/null || true)
# The URL is presigned-shaped: the relay path AND the capability query.
case "$URL_ECHO" in
*/api/v1/uploads/part/$SID/2?token=*) ;;
"") ;;
*) red "urlPart handed out an unexpected target: $URL_ECHO" ;;
esac
case "$URL_ECHO" in
*"token="*) ;;
*) red "urlPart URL carries no capability token: $URL_ECHO" ;;
esac
echo "urlPart(2) -> $URL_ECHO"

put_part "$TOKEN" 1 "$WORK/p1.bin"
GOT=$(mpu_status "$TOKEN")
[ "$GOT" = "PARTS" ] || red "status after part 1 = $GOT, want PARTS"
put_part "$TOKEN" 2 "$WORK/p2.bin"
put_part "$TOKEN" 3 "$WORK/p3.bin"

step "complete?sha1= -> 202 (the algorithm flip: sha1, async)"
CODE=$(curl -sS -o "$WORK/complete.body" -w '%{http_code}' \
    -X POST -H "Authorization: Bearer $TOKEN" \
    "$BASE/binflow/api/v1/uploads/complete?sha1=$WHOLE_SHA1" || true)
[ "$CODE" = "202" ] || red "complete = $CODE, want 202: $(cat "$WORK/complete.body")"
GOT=$(mpu_wait_status "$TOKEN" "FINISHED")
echo "task -> $GOT"
curl -sS -X POST -H "Authorization: Bearer $TOKEN" \
    "$BASE/binflow/api/v1/uploads/status" > "$WORK/finished.body"
PROG=$(python3 -c 'import json; print(json.load(open("'"$WORK/finished.body"'"))["progress"])')
[ "$PROG" = "100" ] || red "Finished progress = $PROG, want 100"
DEP_TOK=$(python3 -c 'import json; print(json.load(open("'"$WORK/finished.body"'")).get("checksumToken") or "")')
[ -n "$DEP_TOK" ] || red "Finished status carries no checksumToken: $(cat "$WORK/finished.body")"

step "the node is the CLIENT's landing: 404 before, 201 after the checksum-deploy PUT"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/$REPO/big/blob.bin" || true)
[ "$CODE" = "404" ] || red "artifact before checksum-deploy = $CODE, want 404"
CODE=$(curl -sS -o "$WORK/dep.body" -w '%{http_code}' \
    -X PUT -H "Authorization: Bearer $DEP_TOK" \
    -H "X-Checksum-Deploy: true" -H "X-Checksum-Sha1: $WHOLE_SHA1" \
    "$BASE/binflow/$REPO/big/blob.bin" || true)
[ "$CODE" = "201" ] || red "checksum-deploy PUT = $CODE, want 201: $(cat "$WORK/dep.body")"

step "artifact GET: sha256 reconciliation, byte for byte"
curl -sS -u "admin:$ADMIN_PW" -o "$WORK/download.bin" \
    "$BASE/binflow/$REPO/big/blob.bin" || red "artifact GET failed"
DL_SHA=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/download.bin")
[ "$DL_SHA" = "$WHOLE_SHA" ] || red "downloaded sha256 $DL_SHA != uploaded $WHOLE_SHA"
cmp -s "$WORK/download.bin" "$WORK/whole.bin" || red "downloaded bytes differ from the upload"
echo "GET $REPO/big/blob.bin: 200, sha256 + bytes reconciled"

# --- leg 3: abort discards ----------------------------------------------------
step "abort: session gone, artifact never lands (S3 MPU reclaimed)"
AB_TOKEN=$(mpu_create "big/gone.bin" 5)
AB_ID=$(mpu_sid "$AB_TOKEN")
put_part "$AB_TOKEN" 1 "$WORK/p1.bin"
assert_incomplete_has "$AB_ID" "abort leg before abort"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST -H "Authorization: Bearer $AB_TOKEN" \
    "$BASE/binflow/api/v1/uploads/abort" || true)
[ "$CODE" = "204" ] || red "abort = $CODE, want 204"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST -H "Authorization: Bearer $AB_TOKEN" \
    "$BASE/binflow/api/v1/uploads/status" || true)
[ "$CODE" = "404" ] || red "status after abort = $CODE, want 404"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -u "admin:$ADMIN_PW" \
    "$BASE/binflow/$REPO/big/gone.bin" || true)
[ "$CODE" = "404" ] || red "aborted artifact GET = $CODE, want 404 (blob invisible)"
assert_incomplete_gone "$AB_ID" "abort (B5: Session.Abort reclaims)"
echo "abort -> 204, status 404, artifact 404"

# --- leg 4: kill -9 restart — the SAME token drives the restarted process -----
step "kill -9 + restart: token honored, parts 2+3, complete, Finished, checksum-deploy, byte reconciliation"
RST_TOKEN=$(mpu_create "big/restart.bin" 5)
RST_ID=$(mpu_sid "$RST_TOKEN")
put_part "$RST_TOKEN" 1 "$WORK/p1.bin"
assert_incomplete_has "$RST_ID" "kill -9 leg before the crash"
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

# The flipped assertion: the restarted process honors the SAME token — the
# lazy re-materialization keys off the binding persisted in the engine's
# upload_sessions row (T-323R under the new wire).
GOT=$(mpu_status "$RST_TOKEN" || true)
[ "$GOT" = "PARTS" ] || red "post-restart status = $GOT, want PARTS (the token re-materializes the session)"
echo "post-restart: token honored, session re-materialized"

put_part "$RST_TOKEN" 2 "$WORK/p2.bin"
put_part "$RST_TOKEN" 3 "$WORK/p3.bin"
CODE=$(curl -sS -o "$WORK/rst-complete.body" -w '%{http_code}' \
    -X POST -H "Authorization: Bearer $RST_TOKEN" \
    "$BASE/binflow/api/v1/uploads/complete?sha1=$WHOLE_SHA1" || true)
[ "$CODE" = "202" ] || red "post-restart complete = $CODE, want 202: $(cat "$WORK/rst-complete.body")"
mpu_wait_status "$RST_TOKEN" "FINISHED" >/dev/null
curl -sS -X POST -H "Authorization: Bearer $RST_TOKEN" \
    "$BASE/binflow/api/v1/uploads/status" > "$WORK/rst-finished.body"
RST_DEP=$(python3 -c 'import json; print(json.load(open("'"$WORK/rst-finished.body"'")).get("checksumToken") or "")')
[ -n "$RST_DEP" ] || red "post-restart Finished carries no checksumToken: $(cat "$WORK/rst-finished.body")"
CODE=$(curl -sS -o "$WORK/rst-dep.body" -w '%{http_code}' \
    -X PUT -H "Authorization: Bearer $RST_DEP" \
    -H "X-Checksum-Deploy: true" -H "X-Checksum-Sha1: $WHOLE_SHA1" \
    "$BASE/binflow/$REPO/big/restart.bin" || true)
[ "$CODE" = "201" ] || red "post-restart checksum-deploy = $CODE, want 201: $(cat "$WORK/rst-dep.body")"
curl -sS -u "admin:$ADMIN_PW" -o "$WORK/rst-download.bin" \
    "$BASE/binflow/$REPO/big/restart.bin" || red "post-restart artifact GET failed"
cmp -s "$WORK/rst-download.bin" "$WORK/whole.bin" \
    || red "post-restart downloaded bytes differ from the 11MiB upload"
DL_SHA=$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/rst-download.bin")
[ "$DL_SHA" = "$WHOLE_SHA" ] || red "post-restart downloaded sha256 $DL_SHA != uploaded $WHOLE_SHA"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST -H "Authorization: Bearer $RST_TOKEN" \
    "$BASE/binflow/api/v1/uploads/complete?sha1=$WHOLE_SHA1" || true)
[ "$CODE" = "404" ] || red "complete after Finished = $CODE, want 404 (session consumed)"
assert_incomplete_gone "$RST_ID" "kill -9 leg (B5: the resumed finish reclaims)"
echo "resumed upload finished on the restarted process: checksum-deploy 201 + bytes reconciled"

# End-of-run B5 audit: NOTHING in-progress may remain. Pre-existing orphans
# from older builds fail here honestly: run against a fresh bucket.
if [ -n "$MC" ]; then
    step "end-of-run audit: zero in-progress MPUs, zero sessions/ residue"
    OUT=$(incomplete_list)
    COUNT=$(printf '%s\n' "$OUT" | grep -c . || true)
    if [ "$COUNT" != "0" ]; then
        red "end-of-run audit: $COUNT in-progress MPU(s) survived: $OUT"
    fi
    SESS=$($MC ls --recursive "local/$BUCKET" 2>/dev/null | grep -c "sessions/" || true)
    if [ "$SESS" != "0" ]; then
        red "end-of-run audit: sessions/ temp objects survived: $($MC ls --recursive "local/$BUCKET" | grep "sessions/")"
    fi
    echo "in-progress MPUs at end of run: 0; sessions/ residue: 0"
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
echo "m10-mpu-probe: GREEN — filestore 501+probe matrix + the flipped S3 chain (token lane, 202+async task, client checksum-deploy) + abort + kill-9 token resume + docker legs all passed (T-332/ADR-0039, T-323R)"
