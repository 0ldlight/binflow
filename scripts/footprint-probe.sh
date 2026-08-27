#!/bin/sh
# BinFlow D-8 footprint probe (T-326, PRD milestone-11 §102.3 / AC3).
#
# WHAT THIS MEASURES — the REAL D-8 ruling is runtime memory, not artifact
# size (the registration chain: T-297 L30/D-9 observation "fresh-empty boot
# vmmap footprint 138.4MB > 100MB 文面线" -> BOARD "D-8 boot footprint 138MB
# (M11 候选)" -> PRD §102.3 "空载 RSS 138MB -> <=100MB 恢复"). This probe boots
# a THROWAWAY fresh instance (fresh data dir, ephemeral port — the m7-resume
# probe's process-management idiom) and records:
#
#   cold-start   spawn -> first 200 "OK" on /binflow/api/system/ping (ms;
#                PRD AC3 pairs this with the RSS line: 冷启动 <2s 维持)
#   rss@ready    resident set the moment ping answers (the fresh-boot reading
#                the 138.4MB registration was taken at)
#   rss@idle     resident set after --idle-secs of zero traffic (NFR-P4's
#                exact protocol: 启动完成、无请求 60s 后 RSS)
#   footprint    darwin only: vmmap "Physical footprint" at ready + idle —
#                the T-147/T-297 methodology, so the numbers are comparable
#                with the 138.4MB registration (vmmap footprint >= ps RSS:
#                it counts compressed + dirty pages the OS attributes to the
#                process). On Linux the ps RSS IS the verdict metric.
#
# VERDICT — observational by default (records, exits 0 unless infra fails);
# --expect turns it into the PRD gate: cold-start < 2000ms AND the
# fresh-boot metric <= 100MB. The gate is expected RED until the boot-time
# allocation slimming (lazy embed touches — internal/ dev-go-core surface)
# lands; PRD §102.3's escape clause (不达须差异归因 + 改善 >=20% 并 BOARD 留痕)
# is the conductor's call on the evidence this probe produces.
#
# Exit codes:
#   0  green (or observational record taken)
#   1  RED — --expect verdict failed (budget exceeded)
#   2  probe infrastructure error (binary missing, server never booted)
#
# Usage: scripts/footprint-probe.sh [--idle-secs 60] [--expect] [binary]
#
# POSIX sh (macOS bash-3.2-as-sh / dash / busybox ash); macOS + Linux.

set -eu

# `CDPATH= cd --` is the POSIX empty-assignment prefix idiom this repo's
# probes use (m7-resume-probe.sh); not an assignment.
# shellcheck disable=SC1007
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

IDLE_SECS=60
EXPECT=0
BIN=""
while [ $# -gt 0 ]; do
    case $1 in
    --idle-secs)
        [ $# -ge 2 ] || { echo "footprint-probe: --idle-secs needs a value" >&2; exit 2; }
        IDLE_SECS=$2
        shift 2
        ;;
    --idle-secs=*)
        IDLE_SECS=${1#--idle-secs=}
        shift
        ;;
    --expect)
        EXPECT=1
        shift
        ;;
    -h|--help)
        sed -n '2,40p' "$0"
        exit 0
        ;;
    -*)
        echo "footprint-probe: unknown option $1" >&2
        exit 2
        ;;
    *)
        BIN=$1
        shift
        ;;
    esac
done

case $IDLE_SECS in
''|*[!0-9]*) echo "footprint-probe: --idle-secs must be a non-negative integer" >&2; exit 2 ;;
esac

fail_infra() {
    echo "footprint-probe: INFRA FAIL: $1" >&2
    [ -f "$SERVER_LOG" ] && tail -20 "$SERVER_LOG" >&2
    exit 2
}

step() {
    echo "---- $1"
}

# ---- build (or reuse a FRESH binary) — the m7-resume-probe discipline -----

BUILD_INPUTS="$ROOT/cmd $ROOT/internal $ROOT/go.mod $ROOT/go.sum $ROOT/tools.go"

if [ "$BIN" = "" ]; then
    BIN="$ROOT/bin/binflow-server"
fi
NEEDS_BUILD=0
if [ ! -x "$BIN" ]; then
    step "binary $BIN not found; building via make build"
    NEEDS_BUILD=1
elif [ "${BIN#"$ROOT"/}" != "$BIN" ]; then
    # BUILD_INPUTS is a deliberate space-separated word list.
    # shellcheck disable=SC2086
    STALE_SRC=$(find $BUILD_INPUTS -type f -newer "$BIN" -print 2>/dev/null | head -n 1)
    if [ -n "$STALE_SRC" ]; then
        step "stale binary: '$STALE_SRC' is newer than $BIN; rebuilding via make build"
        NEEDS_BUILD=1
    fi
else
    echo "note: explicit binary outside the repo ($BIN); freshness NOT checked"
fi
if [ "$NEEDS_BUILD" = "1" ]; then
    (cd "$ROOT" && make build) || fail_infra "make build"
fi
[ -x "$BIN" ] || fail_infra "binary $BIN missing after build"

for tool in python3 curl; do
    command -v "$tool" >/dev/null 2>&1 || fail_infra "$tool is required"
done

# ---- which embed face is staged (the 138.4M registration rode the real
# assets; a placeholder-only checkout reads lower — record, don't guess) -----

if [ -d "$ROOT/internal/console/dist/assets" ]; then CONSOLE_FACE="real SPA staged"; else CONSOLE_FACE="placeholder only"; fi
if [ -d "$ROOT/internal/docs/dist/assets" ]; then DOCS_FACE="real docs staged"; else DOCS_FACE="placeholder only"; fi
step "embed face: console=${CONSOLE_FACE}, docs=${DOCS_FACE}"

# ---- workspace ---------------------------------------------------------------

WORK=$(mktemp -d "${TMPDIR:-/tmp}/binflow-footprint.XXXXXX")
SERVER_LOG="$WORK/serve.log"
DATA_DIR="$WORK/data"
CFG="$WORK/binflow.yaml"
SERVE_PID=""

# cleanup() is invoked by the EXIT/INT/TERM trap below.
# shellcheck disable=SC2329
cleanup() {
    if [ -n "$SERVE_PID" ]; then
        kill "$SERVE_PID" 2>/dev/null || true
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

cat > "$CFG" <<EOF
server:
  listen: 127.0.0.1:$PORT
storage:
  data_dir: $DATA_DIR
EOF

# ---- boot + cold-start timing (single python3 poller, 50ms quantization) ----

step "booting fresh instance on $BASE (data: $DATA_DIR)"
BINFLOW_ADMIN_PASSWORD="footprint-probe-pw" \
    "$BIN" serve -c "$CFG" >>"$SERVER_LOG" 2>&1 &
SERVE_PID=$!

COLD_MS=$(python3 - "$SERVE_PID" "$BASE" 30 <<'PY' || fail_infra "server never answered ping (see log above)"
import os, sys, time, urllib.request
pid, base, timeout = int(sys.argv[1]), sys.argv[2], float(sys.argv[3])
start = time.monotonic()
while time.monotonic() - start < timeout:
    try:
        with urllib.request.urlopen(base + "/binflow/api/system/ping", timeout=1) as r:
            if r.read().decode().strip() == "OK":
                print(int((time.monotonic() - start) * 1000))
                sys.exit(0)
    except Exception:
        pass
    try:
        os.kill(pid, 0)
    except OSError:
        sys.exit(1)
    time.sleep(0.05)
sys.exit(1)
PY
)
echo "cold-start (spawn -> ping OK): ${COLD_MS}ms"

rss_kb() {
    ps -o rss= -p "$1" 2>/dev/null | tr -d ' '
}

# vmmap "Physical footprint" in MB (darwin supplementary; empty elsewhere).
footprint_mb() {
    if command -v vmmap >/dev/null 2>&1; then
        FP=$(vmmap --summary "$1" 2>/dev/null | grep 'Physical footprint:' | head -n 1 | sed 's/.*Physical footprint:[[:space:]]*//')
        [ -n "$FP" ] || return 0
        awk -v v="$FP" 'BEGIN {
            u = substr(v, length(v), 1); n = substr(v, 1, length(v) - 1) + 0
            if (u == "G") printf "%.1f", n * 1024
            else if (u == "M") printf "%.1f", n
            else if (u == "K") printf "%.1f", n / 1024
            else printf "%.1f", n / 1048576
        }'
    fi
}

RSS_READY=$(rss_kb "$SERVE_PID")
[ -n "$RSS_READY" ] || fail_infra "ps returned no rss for $SERVE_PID"
FP_READY=$(footprint_mb "$SERVE_PID")
step "RSS @ ready: $((RSS_READY / 1024))MB (ps rss ${RSS_READY}KB)${FP_READY:+ / vmmap footprint: ${FP_READY}MB}"

step "idling ${IDLE_SECS}s (NFR-P4 protocol: no traffic)"
sleep "$IDLE_SECS"
curl -sf "$BASE/binflow/api/system/ping" >/dev/null 2>&1 || fail_infra "server died during idle window"

RSS_IDLE=$(rss_kb "$SERVE_PID")
FP_IDLE=$(footprint_mb "$SERVE_PID")
step "RSS @ idle+${IDLE_SECS}s: $((RSS_IDLE / 1024))MB (ps rss ${RSS_IDLE}KB)${FP_IDLE:+ / vmmap footprint: ${FP_IDLE}MB}"

# ---- verdict ------------------------------------------------------------------
# Metric selection: darwin gates on vmmap Physical footprint (the T-147/T-297
# methodology behind the 138.4MB registration and the PRD line); elsewhere ps
# RSS at ready is the verdict. The idle readings are NFR-P4 evidence, never
# the gate — D-8's registered shape is the FRESH-BOOT reading.

COLD_BUDGET_MS=2000
RSS_BUDGET_MB=100

if command -v vmmap >/dev/null 2>&1 && [ -n "${FP_READY:-}" ]; then
    VERDICT_METRIC="vmmap-physical-footprint@ready"
    VERDICT_MB=$FP_READY
else
    VERDICT_METRIC="ps-rss@ready"
    VERDICT_MB=$((RSS_READY / 1024))
fi

echo ""
echo "footprint-probe: verdict metric ${VERDICT_METRIC} = ${VERDICT_MB}MB (budget ${RSS_BUDGET_MB}MB); cold-start ${COLD_MS}ms (budget ${COLD_BUDGET_MS}ms)"

if [ "$EXPECT" != "1" ]; then
    echo "footprint-probe: observational (pass --expect for the PRD gate verdict)"
    exit 0
fi

RC=0
if [ "$COLD_MS" -ge "$COLD_BUDGET_MS" ]; then
    echo "footprint-probe: RED — cold-start ${COLD_MS}ms >= ${COLD_BUDGET_MS}ms" >&2
    RC=1
fi
OVER=$(awk -v v="$VERDICT_MB" -v b="$RSS_BUDGET_MB" 'BEGIN { print (v > b) ? 1 : 0 }')
if [ "$OVER" = "1" ]; then
    echo "footprint-probe: RED — ${VERDICT_METRIC} ${VERDICT_MB}MB > ${RSS_BUDGET_MB}MB (PRD §102.3; escape clause: 差异归因 + 改善>=20% + BOARD 留痕)" >&2
    RC=1
fi
[ "$RC" -eq 0 ] && echo "footprint-probe: GREEN — AC3 footprint face within budget"
exit "$RC"
