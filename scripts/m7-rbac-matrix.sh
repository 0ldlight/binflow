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
#   admin            bootstrap administrator (exists today)
#   user             plain non-admin fixture user (exists today)
#   read-only-admin  provisioned by POSTing adminRole=read-only-admin; if the
#                    build does not carry the role field (pre-T-215 main) the
#                    whole column is SKIPped with a note — the field's absence
#                    IS the finding, not a failure of the run.
#
# Columns execute non-admin roles first (their change-face legs must be 403
# with zero side effects) and admin last (its change-face legs really execute
# on the throwaway instance). A zero-side-effect guard snapshots one entity
# before/after the non-admin columns.
#
# Output: the matrix on stdout (markdown table; 404 on a read row is
# annotated with a dagger = route absent on this build). Default mode is
# OBSERVATIONAL (exit 0 once the run completes — the archived baseline);
# --expect switches to verdict mode against the PRD M7 target table
# (admin: reads 200 / changes non-403; user: management face 403;
# read-only-admin: reads 200 / changes 403) and exits 1 on any deviation —
# flip it on once T-215 lands.
#
# Usage: scripts/m7-rbac-matrix.sh [--roles admin,user,read-only-admin]
#                                  [--expect] [path-to-binary]
#          (add --base URL --admin-pw PW to run against an already-running
#           instance instead of booting one; the script then never stops it)
#
# POSIX sh (macOS bash-3.2-as-sh / dash / busybox ash); macOS + Linux.

set -eu
# -f: the W04 sample body carries an includePatterns "**" which MUST reach
# curl verbatim; unquoted word-splitting of $extra would otherwise expand it
# against the working directory's files.
set -f

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

ROLES="read-only-admin,user,admin"
EXPECT=0
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
        sed -n '2,42p' "$0"
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

FIX_REPO="m7-matrix"
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
    # write-probe repo (W01 target)
    http PUT admin "$ADMIN_PW" "api/repositories/m7-matrix-w" "$WORK/fx1" \
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
    read-only-admin)
        # Provision with the FR-64 wire field; support is detected by the
        # field coming back on the user read. Absent field => SKIP (T-215).
        http PUT admin "$ADMIN_PW" "api/security/users/$FIX_ROA" "$WORK/roa.body" \
            -H 'Content-Type: application/json' \
            -d "{\"name\":\"$FIX_ROA\",\"email\":\"$FIX_ROA@t.io\",\"password\":\"$FIX_ROA_PW\",\"adminRole\":\"read-only-admin\"}" >/dev/null
        http GET admin "$ADMIN_PW" "api/security/users/$FIX_ROA" "$WORK/roa.get" >/dev/null
        if grep -q 'adminRole' "$WORK/roa.get" 2>/dev/null; then
            CRED_USER="$FIX_ROA"
            CRED_PW="$FIX_ROA_PW"
            return 0
        fi
        return 1
        ;;
    *)
        echo "m7-rbac-matrix: unknown role '$1' (known: admin, user, read-only-admin)" >&2
        exit 2
        ;;
    esac
}

# ---- the endpoint lists (PRD FR-64 §4.1) ------------------------------------

READ_ROWS="R01|api/repositories
R02|api/repositories/$FIX_REPO
R03|api/v1/stats
R04|api/v1/health
R05|api/security/users
R06|api/security/users/$FIX_USER
R07|api/security/groups
R08|api/v1/permissions
R09|api/v1/audit
R10|api/v1/replication/status
R11|api/v1/storage/stats
R12|api/v1/storage/migration
R13|api/security/token"

WRITE_ROWS="W01|PUT|api/repositories/m7-matrix-w
W02|PUT|api/security/users/$FIX_BOB
W03|DELETE|api/security/groups/m7-sac-grp
W04|POST|api/v1/permissions
W05|POST|api/security/token/revoke
W06|POST|api/v1/system/gc"

# ---- run one role column ----------------------------------------------------

run_column() {
    role=$1
    ensure_fixtures
    if ! declare_role "$role"; then
        printf 'role %s: SKIP (adminRole wire field absent on this build — pending T-215)\n' "$role"
        : > "$WORK/$role.read"
        : > "$WORK/$role.write"
        n=0
        while [ "$n" -lt 13 ]; do
            echo "SKIP" >> "$WORK/$role.read"
            n=$((n + 1))
        done
        n=0
        while [ "$n" -lt 6 ]; do
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
            extra=""
            ;;
        esac
        # shellcheck:disable=SC2086 — extra is a curated word list per row
        code=$(http "$wmethod" "$CRED_USER" "$CRED_PW" "$wpath" "$WORK/cell.body" $extra)
        echo "$code" >> "$WORK/$role.write"
    done
}

# ---- zero-side-effect guard around the non-admin columns ---------------------

GUARD=""
http GET admin "$ADMIN_PW" "api/security/users/$FIX_BOB" "$WORK/guard.before" >/dev/null
cp "$WORK/guard.before" "$WORK/guard.snapshot"

step "run columns (execution order:$RUN_ORDER)"
for role in $RUN_ORDER; do
    case "$role" in
    admin) break ;; # guard closes before the admin column mutates fixtures
    esac
    run_column "$role"
done
http GET admin "$ADMIN_PW" "api/security/users/$FIX_BOB" "$WORK/guard.after" >/dev/null
if [ "$EXTERNAL" = "0" ]; then
    if cmp -s "$WORK/guard.snapshot" "$WORK/guard.after"; then
        GUARD="zero-side-effect guard: $FIX_BOB unchanged across non-admin columns — OK"
    else
        GUARD="zero-side-effect guard: $FIX_BOB CHANGED across non-admin columns — VIOLATION"
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
echo "† = 404 on a governance read: the route is absent on this build (E-26 envelope), both roles see it — feeds T-215's route inventory."
echo "$GUARD"

# ---- verdict mode ------------------------------------------------------------

if [ "$EXPECT" = "1" ]; then
    step "verdict (--expect: PRD M7 target table)"
    dev=0
    check_col() {
        crole=$1
        cface=$2
        [ -f "$WORK/$crole.$cface" ] || return 0
        line_no=0
        while read -r code; do
            line_no=$((line_no + 1))
            if [ "$code" = "SKIP" ]; then
                continue
            fi
            case "$crole:$cface" in
            admin:read)
                [ "$code" = "200" ] || { echo "  DEV $crole $cface #$line_no: want 200, got $code"; dev=$((dev + 1)); }
                ;;
            admin:write)
                case "$code" in
                403) echo "  DEV $crole $cface #$line_no: admin change-face must not be 403, got $code"; dev=$((dev + 1)) ;;
                esac
                ;;
            user:*)
                [ "$code" = "403" ] || { echo "  DEV $crole $cface #$line_no: want 403, got $code"; dev=$((dev + 1)); }
                ;;
            read-only-admin:read)
                [ "$code" = "200" ] || { echo "  DEV $crole $cface #$line_no: want 200, got $code"; dev=$((dev + 1)); }
                ;;
            read-only-admin:write)
                [ "$code" = "403" ] || { echo "  DEV $crole $cface #$line_no: want 403, got $code"; dev=$((dev + 1)); }
                ;;
            esac
        done < "$WORK/$crole.$cface"
    }
    for c in $COLS; do
        check_col "$c" read
        check_col "$c" write
    done
    if [ "$dev" -gt 0 ]; then
        echo "m7-rbac-matrix: $dev deviation(s) from the M7 target table"
        exit 1
    fi
    echo "m7-rbac-matrix: 0 deviations (SKIP columns excluded)"
fi

echo "m7-rbac-matrix: run complete"
