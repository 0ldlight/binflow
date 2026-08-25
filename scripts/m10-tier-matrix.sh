#!/bin/sh
# BinFlow M10 tier x addon-gate posture matrix (T-277, PRD milestone-10 §5.2 /
# §5.5 L04+L06 posture legs; ADR-0032/0033; architecture §15.6).
#
# Boots one EPHEMERAL binflow-server per FORM — five orchestrated instance
# shapes, the QA "多档位实例编排" the PRD §1.3 QA-parallel面 demands:
#
#   community   no license at all (Q2: tier=community, everything M9 stays)
#   pro         + pro license document POSTed after boot
#   enterprise  + enterprise license document POSTed after boot
#   expired     + past-grace license document POSTed after boot (degraded)
#   disabled    config carries addons.disabled: npm (the circuit breaker)
#
# License documents are INJECTED, never generated here: pass --license-dir DIR
# holding pro.lic / enterprise.lic / expired.lic (or env
# BINFLOW_M10_LICENSE_DIR). The mount point is reserved for the T-281 keygen
# toolchain (`bf license keygen/issue`, test key pair). Without a document the
# license-carrying forms run in their UN-INSTALLED posture and say so — an
# honest recording, never a fabricated tier.
#
# Rows (config-plane, deterministic, admin credentials; per-form unique repo
# keys so every PUT is the create arm). Wire paths follow the ADR (winner of
# the PRD<->ADR divergence ruling routed to T-293): /api/system/license.
#
#   T01  GET  api/system/license                  license visibility plane
#   T02  GET  api/v1/addons                       addon registry plane
#   T03  PUT  api/repositories/<form>-generic     five-core floor (must stay 2xx)
#   T04  PUT  api/repositories/<form>-npm         five-core floor / breaker row
#   T05  PUT  api/repositories/<form>-nuget       gated pilot slot
#   T06  PUT  api/repositories/<form>-go          gated pilot slot
#
# Output: the posture matrix on stdout (markdown). Default mode is
# OBSERVATIONAL (exit 0 once the run completes). --expect adds two verdicts:
#
#   verdict A (semantic floor — the license invariants that hold TODAY and
#   must hold forever): every bootable form answers T03 generic 2xx and T04
#   npm 2xx — EXCEPT the disabled form, where npm is intentionally broken
#   (non-2xx) and generic must stay 2xx; the community form additionally
#   must NOT unlock the pilot slots (T05/T06 non-2xx — locked-or-unknown:
#   400 today, 400/403 once FR-86 lands) and, once T01 answers 200, must
#   report tier "community" (Q2: no license never locks the five core
#   package types, and never inflates the tier either). A form whose boot
#   fails records BOOTFAIL cells: excluded from verdict A with a note (the
#   absence IS the finding — the m7 SKIP posture), still diffed by verdict B.
#
#   verdict B (M10 contract baseline + whitelist — the T-250 discipline):
#   every observed (form, row, status) cell is diffed against the frozen
#   baseline scripts/m10-tier-matrix.baseline (recorded --record from the
#   m9-done binary, the M10-opening posture); a mismatch — or a cell with no
#   baseline entry — is a DEViation unless the exact tuple is registered in
#   scripts/m10-tier-matrix.whitelist (entry needs a ticket/ADR comment).
#   A registered deviation that stops happening is WL-STALE and still fails.
#   Rewriting the baseline itself requires an ADR first; --record refuses to
#   overwrite without --force.
#
# Usage: scripts/m10-tier-matrix.sh [--forms community,pro,enterprise,expired,disabled]
#                                  [--expect] [--record] [--force]
#                                  [--baseline FILE] [--whitelist FILE]
#                                  [--license-dir DIR]
#          (--record and --expect are mutually exclusive)
#
# POSIX sh (macOS bash-3.2-as-sh / dash / busybox ash); macOS + Linux.

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

FORMS="community,pro,enterprise,expired,disabled"
EXPECT=0
RECORD=0
FORCE=0
BASELINE=""
WHITELIST=""
LICENSE_DIR="${BINFLOW_M10_LICENSE_DIR:-}"
# Sanitize the child environment: binflow-server fail-closes on UNKNOWN
# BINFLOW_* env keys (config env hygiene), so the mount-point variable must
# never leak into the booted forms — its value is already captured above.
unset BINFLOW_M10_LICENSE_DIR || true
BIN=""
while [ $# -gt 0 ]; do
    case "$1" in
    --forms)
        [ $# -ge 2 ] || { echo "m10-tier-matrix: --forms needs a value" >&2; exit 2; }
        FORMS=$2
        shift 2
        ;;
    --forms=*)
        FORMS=${1#--forms=}
        shift
        ;;
    --expect)
        EXPECT=1
        shift
        ;;
    --record)
        RECORD=1
        shift
        ;;
    --force)
        FORCE=1
        shift
        ;;
    --baseline)
        [ $# -ge 2 ] || { echo "m10-tier-matrix: --baseline needs a value" >&2; exit 2; }
        BASELINE=$2
        shift 2
        ;;
    --baseline=*)
        BASELINE=${1#--baseline=}
        shift
        ;;
    --whitelist)
        [ $# -ge 2 ] || { echo "m10-tier-matrix: --whitelist needs a value" >&2; exit 2; }
        WHITELIST=$2
        shift 2
        ;;
    --whitelist=*)
        WHITELIST=${1#--whitelist=}
        shift
        ;;
    --license-dir)
        [ $# -ge 2 ] || { echo "m10-tier-matrix: --license-dir needs a value" >&2; exit 2; }
        LICENSE_DIR=$2
        shift 2
        ;;
    --license-dir=*)
        LICENSE_DIR=${1#--license-dir=}
        shift
        ;;
    -h|--help)
        sed -n '2,63p' "$0"
        exit 0
        ;;
    -*)
        echo "m10-tier-matrix: unknown option $1" >&2
        exit 2
        ;;
    *)
        BIN=$1
        shift
        ;;
    esac
done

[ -n "$FORMS" ] || { echo "m10-tier-matrix: --forms is empty" >&2; exit 2; }
[ "$EXPECT" = "0" ] || [ "$RECORD" = "0" ] || {
    echo "m10-tier-matrix: --record and --expect are mutually exclusive" >&2
    exit 2
}
[ -n "$BASELINE" ] || BASELINE="$ROOT/scripts/m10-tier-matrix.baseline"
[ -n "$WHITELIST" ] || WHITELIST="$ROOT/scripts/m10-tier-matrix.whitelist"

fail() {
    echo "m10-tier-matrix: FAIL: $1" >&2
    exit 2
}

step() {
    echo "---- $1"
}

for tool in curl python3; do
    command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
done

# ---- row definitions ---------------------------------------------------------

ROWS="T01 T02 T03 T04 T05 T06"
ROWS_N=6

row_label() {
    case "$1" in
    T01) echo "T01 GET api/system/license" ;;
    T02) echo "T02 GET api/v1/addons" ;;
    T03) echo "T03 PUT repo packageType=generic" ;;
    T04) echo "T04 PUT repo packageType=npm" ;;
    T05) echo "T05 PUT repo packageType=nuget" ;;
    T06) echo "T06 PUT repo packageType=go" ;;
    *) fail "unknown row id $1" ;;
    esac
}

# ---- one form ----------------------------------------------------------------

WORK=$(mktemp -d "${TMPDIR:-/tmp}/binflow-m10-tier.XXXXXX")
cleanup() {
    rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# http METHOD PATH OUT_FILE [curl extras...] against $BASE/$ADMIN_PW — prints
# the status code.
http() {
    _method=$1
    _path=$2
    _out=$3
    shift 3
    _code=$(curl -sS -o "$_out" -w '%{http_code}' \
        -u "admin:$ADMIN_PW" -X "$_method" "$BASE/binflow/$_path" "$@" || true)
    printf '%s' "$_code"
}

license_doc_for() {
    # form -> file name inside $LICENSE_DIR (empty when unavailable). Always
    # returns 0: a missing document is a posture note, never a script abort
    # (set -e would otherwise eat the assignment).
    [ -n "$LICENSE_DIR" ] || return 0
    case "$1" in
    pro) [ -f "$LICENSE_DIR/pro.lic" ] && printf '%s' "$LICENSE_DIR/pro.lic" ;;
    enterprise) [ -f "$LICENSE_DIR/enterprise.lic" ] && printf '%s' "$LICENSE_DIR/enterprise.lic" ;;
    expired) [ -f "$LICENSE_DIR/expired.lic" ] && printf '%s' "$LICENSE_DIR/expired.lic" ;;
    *) ;;
    esac
    return 0
}

# run_form FORM — boots the instance, runs the six rows, leaves the cells in
# $WORK/$FORM.cells (one status per line, ROWS order) plus $WORK/$FORM.t01.body.
run_form() {
    form=$1
    if [ "$BIN" = "" ]; then
        BIN="$ROOT/bin/binflow-server"
    fi
    if [ ! -x "$BIN" ]; then
        step "binary $BIN not found; building via make build"
        (cd "$ROOT" && make build) || fail "make build"
    fi

    FDIR="$WORK/$form"
    mkdir -p "$FDIR"
    DATA_DIR="$FDIR/data"
    CFG="$FDIR/binflow.yaml"
    SERVER_LOG="$FDIR/serve.log"
    ADMIN_PW="m10-tier-admin-pw"

    pick_port() {
        PORT=$(python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()') || fail "cannot pick an ephemeral port"
        BASE="http://127.0.0.1:$PORT"
    }

    write_cfg() {
        cat > "$CFG" <<EOF
server:
  listen: 127.0.0.1:$PORT
storage:
  data_dir: $DATA_DIR
EOF
        # The circuit-breaker form rides the config key (LC-03):
        # addons.disabled CSV, restart-effective. Pre-T-28x builds reject the
        # unknown section (strict YAML, KnownFields) — the boot fails and the
        # form records its BOOTFAIL posture honestly instead of pretending.
        if [ "$form" = "disabled" ]; then
            cat >> "$CFG" <<EOF
addons:
  disabled: npm
EOF
        fi
    }

    pick_port
    write_cfg
    step "form $form: starting binflow-server on $BASE (data: $DATA_DIR)"
    # One bounded boot retry (fresh port): absorbs the rare bind(0)-close
    # port self-collision with a boot-probe curl's source port. A REAL boot
    # failure — e.g. the disabled form's config rejected pre-T-28x — fails
    # every attempt, and the final log tail says why; BOOTFAIL stays the
    # honest recorded posture.
    attempt=0
    boot_ok=0
    while [ "$attempt" -lt 2 ]; do
        attempt=$((attempt + 1))
        if [ "$attempt" -gt 1 ]; then
            pick_port
            write_cfg
            echo "form $form: boot attempt 2 on $BASE (attempt 1 failed; the tail below names the reason if this one fails too)"
        fi
        BINFLOW_ADMIN_PASSWORD="$ADMIN_PW" \
            "$BIN" serve -c "$CFG" >"$SERVER_LOG" 2>&1 &
        SERVE_PID=$!
        n=0
        while [ "$n" -lt 200 ]; do
            if BODY=$(curl -sf "$BASE/binflow/api/system/ping" 2>/dev/null) \
                && [ "$BODY" = "OK" ]; then
                break
            fi
            if ! kill -0 "$SERVE_PID" 2>/dev/null; then
                break
            fi
            n=$((n + 1))
            sleep 0.1
        done
        if [ "$n" -lt 200 ] && kill -0 "$SERVE_PID" 2>/dev/null; then
            boot_ok=1
            break
        fi
        kill -TERM "$SERVE_PID" 2>/dev/null || true
        wait "$SERVE_PID" 2>/dev/null || true
        SERVE_PID=""
    done
    if [ "$boot_ok" != "1" ]; then
        echo "form $form: BOOT FAIL — server exited during boot (config rejected on this build?). Log tail:"
        tail -5 "$SERVER_LOG" | sed 's/^/    /'
    fi

    : > "$WORK/$form.cells"
    if [ "$boot_ok" != "1" ]; then
        # Boot never completed: record the posture, do not fabricate cells.
        i=0
        while [ "$i" -lt "$ROWS_N" ]; do
            echo "BOOTFAIL" >> "$WORK/$form.cells"
            i=$((i + 1))
        done
        [ -z "$SERVE_PID" ] || {
            kill -TERM "$SERVE_PID" 2>/dev/null || true
            wait "$SERVE_PID" 2>/dev/null || true
            SERVE_PID=""
        }
        return 0
    fi

    # License injection (mount point for the T-281 keygen output). The POST
    # status is an INFO line, not a matrix cell: T01's GET then observes the
    # resulting tier state.
    DOC=$(license_doc_for "$form")
    if [ -n "$DOC" ]; then
        code=$(http POST "api/system/license" "$FDIR/lic.body" \
            -H 'Content-Type: text/plain' --data-binary @"$DOC")
        echo "form $form: license install POST $DOC -> $code"
    elif [ "$form" = "pro" ] || [ "$form" = "enterprise" ] || [ "$form" = "expired" ]; then
        echo "form $form: NO license document (--license-dir / BINFLOW_M10_LICENSE_DIR; mount point reserved for T-281 keygen) — recording the UN-INSTALLED posture"
    fi

    for row in $ROWS; do
        case "$row" in
        T01)
            code=$(http GET "api/system/license" "$WORK/$form.t01.body")
            ;;
        T02)
            code=$(http GET "api/v1/addons" "$FDIR/addons.body")
            ;;
        T03)
            code=$(http PUT "api/repositories/m10-mx-$form-generic" "$FDIR/r3.body" \
                -H 'Content-Type: application/json' \
                -d '{"rclass":"local","packageType":"generic"}')
            ;;
        T04)
            code=$(http PUT "api/repositories/m10-mx-$form-npm" "$FDIR/r4.body" \
                -H 'Content-Type: application/json' \
                -d '{"rclass":"local","packageType":"npm"}')
            ;;
        T05)
            code=$(http PUT "api/repositories/m10-mx-$form-nuget" "$FDIR/r5.body" \
                -H 'Content-Type: application/json' \
                -d '{"rclass":"local","packageType":"nuget"}')
            ;;
        T06)
            code=$(http PUT "api/repositories/m10-mx-$form-go" "$FDIR/r6.body" \
                -H 'Content-Type: application/json' \
                -d '{"rclass":"local","packageType":"go"}')
            ;;
        esac
        echo "$code" >> "$WORK/$form.cells"
    done

    kill -TERM "$SERVE_PID" 2>/dev/null || true
    wait "$SERVE_PID" 2>/dev/null || true
    SERVE_PID=""
}

SERVE_PID=""
COLS=$(echo "$FORMS" | tr ',' ' ')
step "run forms:$COLS"
for form in $COLS; do
    case "$form" in
    community|pro|enterprise|expired|disabled) ;;
    *) fail "unknown form '$form' (known: community, pro, enterprise, expired, disabled)" ;;
    esac
    run_form "$form"
done

# ---- render the matrix -------------------------------------------------------

step "posture matrix (rows x forms)"

printf '| row |'
for c in $COLS; do
    printf ' %s |' "$c"
done
printf '\n'
printf '%s' '|---|'
for c in $COLS; do
    printf '%s' '---|'
done
printf '\n'
row_no=0
for row in $ROWS; do
    row_no=$((row_no + 1))
    line="| $(row_label "$row") |"
    for c in $COLS; do
        cell=$(sed -n "${row_no}p" "$WORK/$c.cells")
        line="$line $cell |"
    done
    printf '%s\n' "$line"
done

# ---- record mode (freeze the contract baseline — T-250 discipline) -----------

if [ "$RECORD" = "1" ]; then
    step "record baseline -> $BASELINE"
    if [ -e "$BASELINE" ] && [ "$FORCE" != "1" ]; then
        fail "baseline $BASELINE already exists — re-recording needs --force AND an ADR registering the flip (the T-250 discipline)"
    fi
    GIT_SHA=$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
    {
        echo "# BinFlow M10 tier matrix contract baseline (T-277, ADR-0032/0033 / architecture 15.6)."
        echo "# Frozen: commit $GIT_SHA, $(date -u '+%Y-%m-%dT%H:%M:%SZ'), from ephemeral instances"
        echo "# (the script's default boot mode; license forms recorded in their un-installed"
        echo "# posture until --license-dir carries T-281 keygen output)."
        echo "# Recorded by: scripts/m10-tier-matrix.sh --record [--force] (default form set)."
        echo "#"
        echo "# Line format: form|row|status   (BOOTFAIL = the form's config cannot boot on this"
        echo "# build — an honest posture, not a skip)"
        echo "# Consumed by: scripts/m10-tier-matrix.sh --expect (verdict B — deviations must be"
        echo "# registered in scripts/m10-tier-matrix.whitelist to pass; rewriting this file needs"
        echo "# an ADR first)."
        for c in $COLS; do
            row_no=0
            for row in $ROWS; do
                row_no=$((row_no + 1))
                code=$(sed -n "${row_no}p" "$WORK/$c.cells")
                echo "$c|$row|$code"
            done
        done
    } > "$BASELINE"
    cells=$(grep -c -v '^#' "$BASELINE")
    echo "m10-tier-matrix: baseline recorded ($cells cells) -> $BASELINE"
    echo "m10-tier-matrix: run complete"
    exit 0
fi

# ---- verdict mode ------------------------------------------------------------

if [ "$EXPECT" = "1" ]; then
    step "verdict A (--expect: semantic floor — the license invariants)"
    dev=0
    for c in $COLS; do
        bootfail=$(grep -c '^BOOTFAIL$' "$WORK/$c.cells" || true)
        if [ "$bootfail" -gt 0 ]; then
            echo "  NOTE form $c: BOOTFAIL posture ($bootfail/6 cells) — excluded from the floor (the config key landing is the ticket that flips it; verdict B still owns the cells)"
            continue
        fi
        t03=$(sed -n 3p "$WORK/$c.cells")
        t04=$(sed -n 4p "$WORK/$c.cells")
        case "$t03" in
        2*) ;;
        *) echo "  DEV form $c T03: five-core floor broken — generic repo PUT want 2xx, got $t03"; dev=$((dev + 1)) ;;
        esac
        if [ "$c" = "disabled" ]; then
            case "$t04" in
            2*) echo "  DEV form $c T04: circuit breaker inert — npm repo PUT want non-2xx, got $t04"; dev=$((dev + 1)) ;;
            esac
        else
            case "$t04" in
            2*) ;;
            *) echo "  DEV form $c T04: five-core floor broken — npm repo PUT want 2xx, got $t04"; dev=$((dev + 1)) ;;
            esac
        fi
        if [ "$c" = "community" ]; then
            t05=$(sed -n 5p "$WORK/$c.cells")
            t06=$(sed -n 6p "$WORK/$c.cells")
            case "$t05" in
            2*) echo "  DEV form $c T05: community unlocked a gated pilot slot — nuget PUT want non-2xx, got $t05"; dev=$((dev + 1)) ;;
            esac
            case "$t06" in
            2*) echo "  DEV form $c T06: community unlocked a gated pilot slot — go PUT want non-2xx, got $t06"; dev=$((dev + 1)) ;;
            esac
            t01=$(sed -n 1p "$WORK/$c.cells")
            if [ "$t01" = "200" ]; then
                if ! grep -q '"tier"[[:space:]]*:[[:space:]]*"community"' "$WORK/$c.t01.body" 2>/dev/null; then
                    echo "  DEV form $c T01: license GET answered 200 but not tier community (Q2: no-license must read community)"
                    dev=$((dev + 1))
                fi
            fi
        fi
    done
    dev_floor=$dev

    step "verdict B (--expect: M10 contract baseline $BASELINE + whitelist $WHITELIST)"
    [ -f "$BASELINE" ] || fail "baseline $BASELINE missing — record one first: scripts/m10-tier-matrix.sh --record"
    [ -f "$WHITELIST" ] || fail "whitelist $WHITELIST missing — restore it (an absent registry must never pass the gate)"
    if [ -n "$(grep -v '^#' "$BASELINE" | grep -v '^$' | awk -F'|' 'NF != 3')" ]; then
        fail "baseline $BASELINE has malformed lines (want form|row|status)"
    fi

    WL_STRIPPED="$WORK/whitelist.stripped"
    sed 's/#.*//' "$WHITELIST" | sed 's/[[:space:]]*$//' | grep -v '^$' > "$WL_STRIPPED" || true

    whitelisted() {
        grep -q "^$1|$2|$3\$" "$WL_STRIPPED"
    }

    dev_base=0
    wl_hits=0
    for c in $COLS; do
        row_no=0
        for row in $ROWS; do
            row_no=$((row_no + 1))
            base=$(grep -m1 "^$c|$row|" "$BASELINE" | cut -d'|' -f3)
            code=$(sed -n "${row_no}p" "$WORK/$c.cells")
            if [ -z "$base" ]; then
                if whitelisted "$c" "$row" "$code"; then
                    echo "  WL $c $row: no baseline entry, observed $code (whitelisted)"
                    wl_hits=$((wl_hits + 1))
                else
                    echo "  DEV $c $row: no baseline entry (form/row added to the matrix? re-record with an ADR), observed $code"
                    dev_base=$((dev_base + 1))
                fi
                continue
            fi
            [ "$code" = "$base" ] && continue
            if whitelisted "$c" "$row" "$code"; then
                echo "  WL $c $row: baseline $base -> observed $code (whitelisted deviation)"
                wl_hits=$((wl_hits + 1))
            else
                echo "  DEV $c $row: baseline $base, got $code"
                dev_base=$((dev_base + 1))
            fi
        done
    done

    # Staleness walk: every registry entry must correspond to a live deviation
    # in THIS run (observed == registered status, differing from baseline).
    while IFS='|' read -r w_form w_row w_status; do
        [ -n "$w_form" ] || continue
        case " $COLS " in
        *" $w_form "*) ;;
        *) continue ;; # entry for a form not in this run — not evaluable
        esac
        w_no=$(printf '%s\n' $ROWS | awk -v r="$w_row" '$1 == r { print NR; exit }')
        if [ -z "$w_no" ]; then
            echo "  WL-STALE $w_form $w_row: unknown row id in whitelist entry"
            dev_base=$((dev_base + 1))
            continue
        fi
        obs=$(sed -n "${w_no}p" "$WORK/$w_form.cells")
        base=$(grep -m1 "^$w_form|$w_row|" "$BASELINE" | cut -d'|' -f3)
        if [ "$obs" = "$w_status" ] && [ "$obs" != "$base" ]; then
            : # live deviation — reported as WL in the diff pass above
        else
            echo "  WL-STALE $w_form $w_row: entry expects $w_status, observed ${obs:-none} (baseline ${base:-none}) — delete the dead entry"
            dev_base=$((dev_base + 1))
        fi
    done < "$WL_STRIPPED"

    dev=$((dev_floor + dev_base))
    if [ "$dev" -gt 0 ]; then
        echo "m10-tier-matrix: $dev deviation(s) (verdict A floor: $dev_floor, verdict B baseline: $dev_base)"
        exit 1
    fi
    echo "m10-tier-matrix: 0 deviations — semantic floor clean, contract baseline clean ($wl_hits whitelisted)"
fi

echo "m10-tier-matrix: run complete"
