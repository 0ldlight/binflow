#!/bin/sh
# BinFlow M7 RBAC role x endpoint matrix (T-211, PRD FR-64 read face + change
# face samples; the QA gate itself is V02/V03 -> T-221).
#
# Boots an ephemeral binflow-server (throwaway data dir), provisions fixtures,
# then runs every PRD FR-64 §4.1 governance READ endpoint plus a sample of
# CHANGE-face endpoints under each requested role, and prints the endpoint x
# status-code matrix.
#
# Roles:
#   admin           bootstrap administrator (exists today)
#   user            plain non-admin fixture user (exists today)
#   readonly_admin  provisioned by PUTting adminRole=readonly_admin (T-215
#                   wire spelling, snake per ADR-0026 decision 6 — the kebab
#                   spelling is a 400); if the build does not carry the role
#                   field (pre-T-215 main) the whole column is SKIPped with a
#                   note — the field's absence IS the finding, not a failure
#                   of the run.
#
# Columns execute non-admin roles first (their change-face legs must be 403
# with zero side effects) and admin last (its change-face legs really execute
# on the throwaway instance). A zero-side-effect guard snapshots the user,
# repository-config and group entities before/after the non-admin columns,
# plus the absence of the create-arm repository.
#
# Output: the matrix on stdout (markdown table; 404 on a read row is
# annotated with a dagger = route absent on this build). Default mode is
# OBSERVATIONAL (exit 0 once the run completes — the archived baseline);
# --expect switches to verdict mode against the PRD M7 target table
# (admin: reads 200-or-pass-gate-501 / changes non-403; user: management
# face 403; readonly_admin: reads 200-or-pass-gate-501 / changes 403) and
# exits 1 on any deviation — the "pass-gate 501" rows are storage/migration
# (no dual-write on the throwaway config) and the replication pair when the
# instance carries no replication store: the capability gate PASSED and the
# feature is honestly unwired (architecture 7.1; a plain user collects 403
# on the same rows, which is what tells the two apart).
#
# [T-250] M9 CONTRACT-BASELINE MODE (ADR-0030 "additive only", architecture
# 14.5-1): --expect additionally diffs every observed (role, face, row,
# status) cell against the frozen baseline file scripts/m7-rbac-matrix.baseline
# (the M8 tail state, recorded via --record from a clean ephemeral instance)
# and consults scripts/m7-rbac-matrix.whitelist — the explicit deviation
# registry, EMPTY when M9 opens. Any status flip on an existing cell that is
# not whitelisted fails the run: that is the machine gate of "new endpoints
# must not break existing ones". Registering a whitelist entry requires a
# ticket/ADR reference in the entry's comment; rewriting the baseline file
# itself requires an ADR first (per-milestone evolution), and --record refuses
# to overwrite without --force. A whitelist entry whose deviation no longer
# happens is reported WL-STALE and still fails — the registry stays honest.
# The PRD-table verdict (verdict A, the semantic floor) is kept below the
# baseline diff (verdict B): it also catches a corrupt re-record of the
# baseline from a broken build. Baseline verdicts are only meaningful in the
# default clean-instance boot; with --base the caller owns the instance state.
#
# Usage: scripts/m7-rbac-matrix.sh [--roles admin,user,readonly_admin]
#                                  [--expect] [--record] [--force]
#                                  [--baseline FILE] [--whitelist FILE]
#                                  [path-to-binary]
#          (--record and --expect are mutually exclusive; add --base URL
#           --admin-pw PW to run against an already-running instance instead
#           of booting one; the script then never stops it)
#
# POSIX sh (macOS bash-3.2-as-sh / dash / busybox ash); macOS + Linux.

set -eu
# -f: the W04 sample body carries an includePatterns "**" which MUST reach
# curl verbatim; unquoted word-splitting of $extra would otherwise expand it
# against the working directory's files.
set -f

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

ROLES="readonly_admin,user,admin"
EXPECT=0
RECORD=0
FORCE=0
BASELINE=""
WHITELIST=""
BIN=""
BASE=""
ADMIN_PW=""
while [ $# -gt 0 ]; do
    case "$1" in
    --roles)
        [ $# -ge 2 ] || { echo "m7-rbac-matrix: --roles needs a value" >&2; exit 2; }
        ROLES=$2
        shift 2
        ;;
    --roles=*)
        ROLES=${1#--roles=}
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
        [ $# -ge 2 ] || { echo "m7-rbac-matrix: --baseline needs a value" >&2; exit 2; }
        BASELINE=$2
        shift 2
        ;;
    --baseline=*)
        BASELINE=${1#--baseline=}
        shift
        ;;
    --whitelist)
        [ $# -ge 2 ] || { echo "m7-rbac-matrix: --whitelist needs a value" >&2; exit 2; }
        WHITELIST=$2
        shift 2
        ;;
    --whitelist=*)
        WHITELIST=${1#--whitelist=}
        shift
        ;;
    --base)
        [ $# -ge 2 ] || { echo "m7-rbac-matrix: --base needs a value" >&2; exit 2; }
        BASE=$2
        shift 2
        ;;
    --base=*)
        BASE=${1#--base=}
        shift
        ;;
    --admin-pw)
        [ $# -ge 2 ] || { echo "m7-rbac-matrix: --admin-pw needs a value" >&2; exit 2; }
        ADMIN_PW=$2
        shift 2
        ;;
    --admin-pw=*)
        ADMIN_PW=${1#--admin-pw=}
        shift
        ;;
    -h|--help)
        sed -n '2,61p' "$0"
        exit 0
        ;;
    -*)
        echo "m7-rbac-matrix: unknown option $1" >&2
        exit 2
        ;;
    *)
        BIN=$1
        shift
        ;;
    esac
done

[ -n "$ROLES" ] || { echo "m7-rbac-matrix: --roles is empty" >&2; exit 2; }
[ "$EXPECT" = "0" ] || [ "$RECORD" = "0" ] || {
    echo "m7-rbac-matrix: --record and --expect are mutually exclusive" >&2
    exit 2
}
[ -n "$BASELINE" ] || BASELINE="$ROOT/scripts/m7-rbac-matrix.baseline"
[ -n "$WHITELIST" ] || WHITELIST="$ROOT/scripts/m7-rbac-matrix.whitelist"

FIX_REPO="m7-matrix"
FIX_WREPO="m7-matrix-w"
FIX_NEWREPO="m7-matrix-create"
FIX_USER="m7user"
FIX_USER_PW="m7user-pw"
FIX_BOB="m7bob"
FIX_ROA="m7roa"
FIX_ROA_PW="m7roa-pw"

fail() {
    echo "m7-rbac-matrix: FAIL: $1" >&2
    [ -n "$SERVER_LOG" ] && [ -f "$SERVER_LOG" ] && tail -20 "$SERVER_LOG" >&2
    exit 2
}

step() {
    echo "---- $1"
}

# http METHOD USER PW PATH OUT_FILE [curl extras...] — prints the status code.
http() {
    _method=$1
    _user=$2
    _pw=$3
    _path=$4
    _out=$5
    shift 5
    _code=$(curl -sS -o "$_out" -w '%{http_code}' \
        -u "$_user:$_pw" -X "$_method" "$BASE/binflow/$_path" "$@" || true)
    printf '%s' "$_code"
}

for tool in curl python3; do
    command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
done

# ---- server lifecycle -------------------------------------------------------

WORK=$(mktemp -d "${TMPDIR:-/tmp}/binflow-m7-rbac.XXXXXX")
SERVER_LOG=""
SERVE_PID=""
EXTERNAL=0

cleanup() {
    if [ -n "$SERVE_PID" ]; then
        kill -TERM "$SERVE_PID" 2>/dev/null || true
        wait "$SERVE_PID" 2>/dev/null || true
    fi
    rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

if [ -n "$BASE" ]; then
    [ -n "$ADMIN_PW" ] || fail "--base needs --admin-pw"
    EXTERNAL=1
    step "external instance mode: $BASE (the server is NOT managed here)"
else
    if [ "$BIN" = "" ]; then
        BIN="$ROOT/bin/binflow-server"
    fi
    if [ ! -x "$BIN" ]; then
        step "binary $BIN not found; building via make build"
        (cd "$ROOT" && make build) || fail "make build"
    fi
    [ -x "$BIN" ] || fail "binary $BIN missing after build"

    SERVER_LOG="$WORK/serve.log"
    DATA_DIR="$WORK/data"
    CFG="$WORK/binflow.yaml"
    PORT=$(python3 -c 'import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()') || fail "cannot pick an ephemeral port"
    BASE="http://127.0.0.1:$PORT"
    ADMIN_PW="m7-matrix-admin-pw"
    cat > "$CFG" <<EOF
server:
  listen: 127.0.0.1:$PORT
storage:
  data_dir: $DATA_DIR
EOF
    step "starting binflow-server on $BASE (data: $DATA_DIR)"
    BINFLOW_ADMIN_PASSWORD="$ADMIN_PW" \
        "$BIN" serve -c "$CFG" >"$SERVER_LOG" 2>&1 &
    SERVE_PID=$!
    n=0
    while [ "$n" -lt 200 ]; do
        if BODY=$(curl -sf "$BASE/binflow/api/system/ping" 2>/dev/null) \
            && [ "$BODY" = "OK" ]; then
            break
        fi
        kill -0 "$SERVE_PID" 2>/dev/null || fail "server exited during boot"
        n=$((n + 1))
        sleep 0.1
    done
    [ "$n" -lt 200 ] || fail "server never answered ping on $BASE"
fi

# ---- fixtures (as admin; idempotent, re-run before every column) -----------

ensure_fixtures() {
    # write-probe repo (config/quota snapshot target)
    http PUT admin "$ADMIN_PW" "api/repositories/$FIX_WREPO" "$WORK/fx1" \
        -H 'Content-Type: application/json' \
        -d '{"rclass":"local","packageType":"generic"}' >/dev/null
    # read-fixture repo
    http PUT admin "$ADMIN_PW" "api/repositories/$FIX_REPO" "$WORK/fx2" \
        -H 'Content-Type: application/json' \
        -d '{"rclass":"local","packageType":"generic"}' >/dev/null
    # plain user + write-target user
    http PUT admin "$ADMIN_PW" "api/security/users/$FIX_USER" "$WORK/fx3" \
        -H 'Content-Type: application/json' \
        -d "{\"name\":\"$FIX_USER\",\"email\":\"$FIX_USER@t.io\",\"password\":\"$FIX_USER_PW\",\"admin\":false}" >/dev/null
    http PUT admin "$ADMIN_PW" "api/security/users/$FIX_BOB" "$WORK/fx4" \
        -H 'Content-Type: application/json' \
        -d "{\"name\":\"$FIX_BOB\",\"email\":\"$FIX_BOB@t.io\",\"password\":\"bob-pw-123\",\"admin\":false}" >/dev/null
    # sacrificial group (W03 target)
    http PUT admin "$ADMIN_PW" "api/security/groups/m7-sac-grp" "$WORK/fx5" \
        -H 'Content-Type: application/json' \
        -d '{"name":"m7-sac-grp","description":"m7 matrix sacrificial group"}' >/dev/null
    return 0
}

step "provision fixtures (as admin, idempotent)"
ensure_fixtures

# ---- role resolution --------------------------------------------------------

# Column execution order: non-admin roles first (their writes must be pure
# 403s), admin last (its writes execute). Order within each group follows
# --roles.
RUN_ORDER=""
for r in $(echo "$ROLES" | tr ',' ' '); do
    case "$r" in
    admin) ;;
    *) RUN_ORDER="$RUN_ORDER $r" ;;
    esac
done
case ",$ROLES," in
*,admin,*) RUN_ORDER="$RUN_ORDER admin" ;;
esac

declare_role() {
    # Sets CRED_USER / CRED_PW for the role, or returns 1 to SKIP the column.
    case "$1" in
    admin)
        CRED_USER="admin"
        CRED_PW="$ADMIN_PW"
        return 0
        ;;
    user)
        CRED_USER="$FIX_USER"
        CRED_PW="$FIX_USER_PW"
        return 0
        ;;
    readonly_admin)
        # Provision with the FR-64 wire field (snake spelling, T-215);
        # support is detected by the field coming back on the user read.
        # Absent field => SKIP (pre-T-215 build).
        http PUT admin "$ADMIN_PW" "api/security/users/$FIX_ROA" "$WORK/roa.body" \
            -H 'Content-Type: application/json' \
            -d "{\"name\":\"$FIX_ROA\",\"email\":\"$FIX_ROA@t.io\",\"password\":\"$FIX_ROA_PW\",\"adminRole\":\"readonly_admin\"}" >/dev/null
        http GET admin "$ADMIN_PW" "api/security/users/$FIX_ROA" "$WORK/roa.get" >/dev/null
        if grep -q '"adminRole": "readonly_admin"' "$WORK/roa.get" 2>/dev/null; then
            CRED_USER="$FIX_ROA"
            CRED_PW="$FIX_ROA_PW"
            return 0
        fi
        return 1
        ;;
    *)
        echo "m7-rbac-matrix: unknown role '$1' (known: admin, user, readonly_admin)" >&2
        exit 2
        ;;
    esac
}

# ---- the endpoint lists (PRD FR-64 §4.1 v1.1) -------------------------------
#
# Read face = the T-214① final 11-endpoint list plus the single-user read
# (both sit on security:read): repositories list/detail, health, users
# list/detail, groups, permissions, audit, replication status, replication
# configs, storage stats, storage migration. The v1.0 rows api/v1/stats
# (route never existed; the real endpoint is storage/stats) and
# api/security/token (M1 E-17 deliberately has no token list) are GONE —
# T-214 P2's erratum. Rows whose PASS-GATE posture is 501 on this build:
# R12 storage/migration (no dual-write in the throwaway config).

READ_ROWS="R01|api/repositories
R02|api/repositories/$FIX_REPO
R03|api/v1/health
R04|api/security/users
R05|api/security/users/$FIX_USER
R06|api/security/groups
R07|api/v1/permissions
R08|api/v1/audit
R09|api/v1/replications
R10|api/v1/replication/status
R11|api/v1/storage/stats
R12|api/v1/storage/migration"

READ_ROWS_N=12

# Change face samples per the PRD: repo creation (the PUT create arm — the
# router has no POST collection route, T-214 P8), user write, group delete,
# permission write, token revoke, GC (dry-run included) and the quota write
# (POST partial update, family 7).

WRITE_ROWS="W01|PUT|api/repositories/$FIX_NEWREPO
W02|PUT|api/security/users/$FIX_BOB
W03|DELETE|api/security/groups/m7-sac-grp
W04|POST|api/v1/permissions
W05|POST|api/security/token/revoke
W06|POST|api/v1/system/gc
W07|POST|api/repositories/$FIX_WREPO"

WRITE_ROWS_N=7

# ---- run one role column ----------------------------------------------------

run_column() {
    role=$1
    ensure_fixtures
    if ! declare_role "$role"; then
        printf 'role %s: SKIP (adminRole wire field absent on this build — pending T-215)\n' "$role"
        : > "$WORK/$role.read"
        : > "$WORK/$role.write"
        n=0
        while [ "$n" -lt "$READ_ROWS_N" ]; do
            echo "SKIP" >> "$WORK/$role.read"
            n=$((n + 1))
        done
        n=0
        while [ "$n" -lt "$WRITE_ROWS_N" ]; do
            echo "SKIP" >> "$WORK/$role.write"
            n=$((n + 1))
        done
        return 0
    fi
    printf 'role %s: running as %s\n' "$role" "$CRED_USER"
    : > "$WORK/$role.read"
    printf '%s\n' "$READ_ROWS" | while IFS='|' read -r rid rpath; do
        code=$(http GET "$CRED_USER" "$CRED_PW" "$rpath" "$WORK/cell.body")
        echo "$code" >> "$WORK/$role.read"
    done
    : > "$WORK/$role.write"
    printf '%s\n' "$WRITE_ROWS" | while IFS='|' read -r wid wmethod wpath; do
        extra=""
        case "$wid" in
        W01)
            extra="-H Content-Type:application/json -d {\"rclass\":\"local\",\"packageType\":\"generic\"}"
            ;;
        W02)
            # PUT is create-or-replace: password is mandatory in the body,
            # otherwise admin legitimately collects a 400 instead of a 200.
            extra="-H Content-Type:application/json -d {\"email\":\"$FIX_BOB@changed.invalid\",\"password\":\"bob-pw-123\"}"
            ;;
        W04)
            # fresh target name per column: the admin column really creates it
            extra="-H Content-Type:application/json -d {\"name\":\"t-m7-$role\",\"repos\":[\"$FIX_REPO\"],\"includePatterns\":[\"**\"],\"principals\":{\"users\":{},\"groups\":{}}}"
            ;;
        W05)
            extra="-d token=sha256-0000000000000000000000000000000000000000000000000000000000000000-nonexistent"
            ;;
        W06)
            # dry-run arm on purpose (T-214①: readonly_admin is 403 here too)
            extra="-H Content-Type:application/json -d {\"apply\":false}"
            ;;
        W07)
            # quota write (family 7 partial update): the repository-config
            # snapshot catches any leak
            extra="-H Content-Type:application/json -d {\"quotaBytes\":424242}"
            ;;
        esac
        # shellcheck:disable=SC2086 — extra is a curated word list per row
        code=$(http "$wmethod" "$CRED_USER" "$CRED_PW" "$wpath" "$WORK/cell.body" $extra)
        echo "$code" >> "$WORK/$role.write"
    done
}

# ---- zero-side-effect guard around the non-admin columns ---------------------
#
# Snapshots three entities a denied write must leave byte-identical (the
# user, the repository config — quota included — and the group), plus the
# ABSENCE of the create-arm repository: if any non-admin column minted it,
# the probe finds a 200 where the 404 belongs.

GUARD=""
guard_entity() {
    # guard_entity PATH OUTFILE — snapshot one entity's current body.
    http GET admin "$ADMIN_PW" "$1" "$2" >/dev/null
}
guard_entity "api/security/users/$FIX_BOB" "$WORK/guard.bob.before"
guard_entity "api/repositories/$FIX_WREPO" "$WORK/guard.repo.before"
guard_entity "api/security/groups/m7-sac-grp" "$WORK/guard.group.before"

step "run columns (execution order:$RUN_ORDER)"
for role in $RUN_ORDER; do
    case "$role" in
    admin) break ;; # guard closes before the admin column mutates fixtures
    esac
    run_column "$role"
done
if [ "$EXTERNAL" = "0" ]; then
    GUARD="zero-side-effect guard:"
    guard_entity "api/security/users/$FIX_BOB" "$WORK/guard.bob.after"
    if cmp -s "$WORK/guard.bob.before" "$WORK/guard.bob.after"; then
        GUARD="$GUARD $FIX_BOB unchanged"
    else
        GUARD="$GUARD $FIX_BOB CHANGED (VIOLATION)"
    fi
    guard_entity "api/repositories/$FIX_WREPO" "$WORK/guard.repo.after"
    if cmp -s "$WORK/guard.repo.before" "$WORK/guard.repo.after"; then
        GUARD="$GUARD, repo config unchanged"
    else
        GUARD="$GUARD, repo config CHANGED (VIOLATION)"
    fi
    guard_entity "api/security/groups/m7-sac-grp" "$WORK/guard.group.after"
    if cmp -s "$WORK/guard.group.before" "$WORK/guard.group.after"; then
        GUARD="$GUARD, group unchanged"
    else
        GUARD="$GUARD, group CHANGED (VIOLATION)"
    fi
    crecode=$(http GET admin "$ADMIN_PW" "api/repositories/$FIX_NEWREPO" "$WORK/guard.create.body")
    if [ "$crecode" = "404" ]; then
        GUARD="$GUARD, create-arm repo still absent — OK"
    else
        GUARD="$GUARD, create-arm repo EXISTS after non-admin columns (VIOLATION, got $crecode)"
    fi
else
    GUARD="zero-side-effect guard: skipped (external instance)"
fi

for role in $RUN_ORDER; do
    case "$role" in
    admin)
        run_column "$role"
        break
        ;;
    esac
done

# ---- render the matrix -------------------------------------------------------

step "matrix (BASE=$BASE)"

COLS=$(echo "$ROLES" | tr ',' ' ')

printf '| endpoint |'
for c in $COLS; do
    printf ' %s |' "$c"
done
printf '\n'
printf '%s' '|---|'
for c in $COLS; do
    printf '%s' '---|'
done
printf '\n'

render_face() {
    face=$1 # read | write
    rows=$2
    n=0
    printf '%s\n' "$rows" | while IFS='|' read -r f1 f2 f3; do
        n=$((n + 1))
        if [ "$face" = "write" ]; then
            label="$f1 $f2 $f3"
        else
            label="$f1 GET $f2"
        fi
        line="| $label |"
        for c in $COLS; do
            code=$(sed -n "${n}p" "$WORK/$c.$face")
            cell=$code
            if [ "$code" = "404" ] && [ "$face" = "read" ]; then
                cell="404†"
            fi
            line="$line $cell |"
        done
        printf '%s\n' "$line"
    done
}

render_face read "$READ_ROWS"
render_face write "$WRITE_ROWS"
echo "† = 404 on a governance read: the route is absent on this build (E-26 envelope) — with the T-214① v1.1 row set no read row should carry one."
echo "$GUARD"

# ---- record mode (T-250: freeze the contract baseline) -----------------------
#
# Writes the observed (role|face|row|status) table to the baseline file. The
# baseline must be recorded from a CLEAN ephemeral instance (the default boot
# mode) with the default role set; --force is required to overwrite an
# existing file, and per architecture 14.5-1 an ADR must register the flip
# before a re-record ever happens.

if [ "$RECORD" = "1" ]; then
    step "record baseline -> $BASELINE"
    if [ -e "$BASELINE" ] && [ "$FORCE" != "1" ]; then
        fail "baseline $BASELINE already exists — re-recording needs --force AND an ADR registering the flip (architecture 14.5-1)"
    fi
    GIT_SHA=$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
    ORIGIN="clean ephemeral instance (the script's default boot mode)"
    [ "$EXTERNAL" = "0" ] || ORIGIN="EXTERNAL instance via --base (caller-owned state — not the frozen posture)"
    {
        echo "# BinFlow RBAC matrix contract baseline (T-250, ADR-0030 / architecture 14.5-1)."
        echo "# Frozen: commit $GIT_SHA, $(date -u '+%Y-%m-%dT%H:%M:%SZ'), from a $ORIGIN."
        echo "# Recorded by: scripts/m7-rbac-matrix.sh --record [--force] (default role set)."
        echo "#"
        echo "# Line format: role|face|row|status   (face = read|write; SKIP = column skipped)"
        echo "# Consumed by: scripts/m7-rbac-matrix.sh --expect (verdict B — deviations must be"
        echo "# registered in scripts/m7-rbac-matrix.whitelist to pass; rewriting this file needs"
        echo "# an ADR first)."
        for c in $COLS; do
            for face in read write; do
                rows=$READ_ROWS
                [ "$face" = "write" ] && rows=$WRITE_ROWS
                n=0
                printf '%s\n' "$rows" | while IFS='|' read -r row_id _rest; do
                    n=$((n + 1))
                    code=$(sed -n "${n}p" "$WORK/$c.$face")
                    echo "$c|$face|$row_id|$code"
                done
            done
        done
    } > "$BASELINE"
    cells=$(grep -c -v '^#' "$BASELINE")
    echo "m7-rbac-matrix: baseline recorded ($cells cells) -> $BASELINE"
    echo "m7-rbac-matrix: run complete"
    exit 0
fi

# ---- verdict mode ------------------------------------------------------------
#
# Verdict A (PRD M7 target table — the semantic floor): row-aware, every code
# line maps to its row id, because the pass-gate posture is per row — R12
# storage/migration answers 501 once the gate passed on a build without
# dual-write (a plain user's 403 on the same row is what proves the gate
# decided). The zero-side-effect guard's verdicts count as deviations too.
# The floor also catches a corrupt re-record of the baseline from a broken
# build (e.g. a user column suddenly answering 200 on a write row).
#
# Verdict B (M9 contract baseline + whitelist — T-250, the machine gate):
# every observed cell is diffed against the frozen baseline; a mismatch (or a
# cell with no baseline entry) is a DEViation unless the exact tuple is
# registered in the whitelist, in which case it is reported WL. A whitelist
# entry whose deviation no longer happens (observed == baseline again, or the
# observed status never materializes) is WL-STALE and also fails — the
# registry must not accumulate dead entries.

if [ "$EXPECT" = "1" ]; then
    step "verdict A (--expect: PRD M7 target table — semantic floor)"
    dev=0
    check_col() {
        crole=$1
        cface=$2
        [ -f "$WORK/$crole.$cface" ] || return 0
        rows=$READ_ROWS
        [ "$cface" = "write" ] && rows=$WRITE_ROWS
        line_no=0
        row_id=""
        while IFS='|' read -r row_a row_b row_c; do
            line_no=$((line_no + 1))
            if [ "$cface" = "write" ]; then
                row_id="$row_a"
            else
                row_id="$row_a"
            fi
            code=$(sed -n "${line_no}p" "$WORK/$crole.$cface")
            [ "$code" = "SKIP" ] && continue
            case "$crole:$cface" in
            admin:read|readonly_admin:read)
                want="200"
                [ "$row_id" = "R12" ] && want="200-or-501"
                ok=0
                [ "$code" = "200" ] && ok=1
                if [ "$want" = "200-or-501" ] && [ "$code" = "501" ]; then
                    ok=1
                fi
                [ "$ok" = "1" ] || { echo "  DEV $crole $cface $row_id: want $want, got $code"; dev=$((dev + 1)); }
                ;;
            admin:write)
                case "$code" in
                403) echo "  DEV $crole $cface $row_id: admin change-face must not be 403, got $code"; dev=$((dev + 1)) ;;
                esac
                ;;
            user:*)
                [ "$code" = "403" ] || { echo "  DEV $crole $cface $row_id: want 403, got $code"; dev=$((dev + 1)); }
                ;;
            readonly_admin:write)
                [ "$code" = "403" ] || { echo "  DEV $crole $cface $row_id: want 403, got $code"; dev=$((dev + 1)); }
                ;;
            esac
        done <<EOF
$rows
EOF
    }
    for c in $COLS; do
        check_col "$c" read
        check_col "$c" write
    done
    dev_table=$dev

    step "verdict B (--expect: M9 contract baseline $BASELINE + whitelist $WHITELIST)"
    [ -f "$BASELINE" ] || fail "baseline $BASELINE missing — record one first: scripts/m7-rbac-matrix.sh --record"
    [ -f "$WHITELIST" ] || fail "whitelist $WHITELIST missing — restore it (an absent registry must never pass the gate)"
    if [ -n "$(grep -v '^#' "$BASELINE" | grep -v '^$' | awk -F'|' 'NF != 4')" ]; then
        fail "baseline $BASELINE has malformed lines (want role|face|row|status)"
    fi

    # Whitelist pre-pass: strip trailing comments/blanks so both the grep
    # lookup and the staleness walk see bare tuples.
    WL_STRIPPED="$WORK/whitelist.stripped"
    sed 's/#.*//' "$WHITELIST" | sed 's/[[:space:]]*$//' | grep -v '^$' > "$WL_STRIPPED" || true

    whitelisted() {
        # whitelisted ROLE FACE ROW STATUS — 0 iff the tuple is registered.
        grep -q "^$1|$2|$3|$4\$" "$WL_STRIPPED"
    }

    dev_base=0
    wl_hits=0
    diff_col() {
        crole=$1
        cface=$2
        [ -f "$WORK/$crole.$cface" ] || return 0
        rows=$READ_ROWS
        [ "$cface" = "write" ] && rows=$WRITE_ROWS
        line_no=0
        while IFS='|' read -r row_a row_b row_c; do
            line_no=$((line_no + 1))
            base=$(grep -m1 "^$crole|$cface|$row_a|" "$BASELINE" | cut -d'|' -f4)
            code=$(sed -n "${line_no}p" "$WORK/$crole.$cface")
            if [ -z "$base" ]; then
                if whitelisted "$crole" "$cface" "$row_a" "$code"; then
                    echo "  WL $crole $cface $row_a: no baseline entry, observed $code (whitelisted)"
                    wl_hits=$((wl_hits + 1))
                else
                    echo "  DEV $crole $cface $row_a: no baseline entry (row added to the matrix? re-record with an ADR), observed $code"
                    dev_base=$((dev_base + 1))
                fi
                continue
            fi
            [ "$code" = "$base" ] && continue
            if whitelisted "$crole" "$cface" "$row_a" "$code"; then
                echo "  WL $crole $cface $row_a: baseline $base -> observed $code (whitelisted deviation)"
                wl_hits=$((wl_hits + 1))
            else
                echo "  DEV $crole $cface $row_a: baseline $base, got $code"
                dev_base=$((dev_base + 1))
            fi
        done <<EOF
$rows
EOF
    }
    for c in $COLS; do
        diff_col "$c" read
        diff_col "$c" write
    done

    # Staleness walk: every registry entry must correspond to a live deviation
    # in THIS run (observed == registered status, differing from baseline).
    while IFS='|' read -r w_role w_face w_row w_status; do
        [ -n "$w_role" ] || continue
        case " $COLS " in
        *" $w_role "*) ;;
        *) continue ;; # entry for a role not in this run — not evaluable
        esac
        case "$w_face" in
        read|write) ;;
        *)
            echo "  WL-STALE $w_role $w_face $w_row: malformed face in whitelist entry"
            dev_base=$((dev_base + 1))
            continue
            ;;
        esac
        rows=$READ_ROWS
        [ "$w_face" = "write" ] && rows=$WRITE_ROWS
        w_ln=$(printf '%s\n' "$rows" | awk -F'|' -v r="$w_row" '$1 == r { print NR; exit }')
        if [ -z "$w_ln" ]; then
            echo "  WL-STALE $w_role $w_face $w_row: unknown row id in whitelist entry"
            dev_base=$((dev_base + 1))
            continue
        fi
        obs=$(sed -n "${w_ln}p" "$WORK/$w_role.$w_face")
        [ "$obs" = "SKIP" ] && continue # SKIP already counted as DEV above
        base=$(grep -m1 "^$w_role|$w_face|$w_row|" "$BASELINE" | cut -d'|' -f4)
        if [ "$obs" = "$w_status" ] && [ "$obs" != "$base" ]; then
            : # live deviation — reported as WL in the diff pass above
        else
            echo "  WL-STALE $w_role $w_face $w_row: entry expects $w_status, observed ${obs:-none} (baseline ${base:-none}) — delete the dead entry"
            dev_base=$((dev_base + 1))
        fi
    done < "$WL_STRIPPED"

    dev_guard=0
    case "$GUARD" in
    *VIOLATION*)
        echo "  DEV zero-side-effect guard: $GUARD"
        dev_guard=1
        ;;
    esac

    dev=$((dev_table + dev_base + dev_guard))
    if [ "$dev" -gt 0 ]; then
        echo "m7-rbac-matrix: $dev deviation(s) (verdict A table: $dev_table, verdict B baseline: $dev_base, guard: $dev_guard)"
        exit 1
    fi
    echo "m7-rbac-matrix: 0 deviations — PRD table clean, contract baseline clean ($wl_hits whitelisted)"
fi

echo "m7-rbac-matrix: run complete"
