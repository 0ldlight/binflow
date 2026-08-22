#!/bin/sh
# BinFlow golangci-lint per-package baseline (T-211; feeds FR-70/V31 —
# "全仓 0 issues (含 internal/auth 清零)" — by archiving TODAY's counts as the
# before-picture the debt ticket is diffed against).
#
# Runs the same pinned golangci-lint the Makefile gate uses (PATH first, then
# GOPATH/bin — mirrors make lint) over the whole module and reduces the issue
# list to:
#   1. a per-package count table (sorted, descending),
#   2. a per-linter count table,
#   3. the grand total + tool version.
#
# This is an ARCHIVE, not a gate: it always exits 0 when the lint run itself
# succeeded (issues are the payload, not a failure). `make lint` stays the
# zero-warning gate. Nonzero exit means the tool could not run at all.
#
# Usage: scripts/lint-baseline.sh [path-to-golangci-lint]
#
# POSIX sh (macOS bash-3.2-as-sh / dash / busybox ash); macOS + Linux.

set -eu
set -f

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

GOLANGCI=${1:-}
case "$GOLANGCI" in
-h|--help)
    sed -n '2,20p' "$0"
    exit 0
    ;;
esac
if [ "$GOLANGCI" = "" ]; then
    GOLANGCI=$(command -v golangci-lint 2>/dev/null || true)
fi
if [ "$GOLANGCI" = "" ] && [ -n "${GOPATH:-}" ] && [ -x "$GOPATH/bin/golangci-lint" ]; then
    GOLANGCI="$GOPATH/bin/golangci-lint"
fi
if [ "$GOLANGCI" = "" ]; then
    GO_BIN=$(go env GOPATH 2>/dev/null || true)
    if [ -n "$GO_BIN" ] && [ -x "$GO_BIN/bin/golangci-lint" ]; then
        GOLANGCI="$GO_BIN/bin/golangci-lint"
    fi
fi
if [ "$GOLANGCI" = "" ] || [ ! -x "$GOLANGCI" ]; then
    echo "lint-baseline: golangci-lint not found; run 'make lint' once to install the pinned version" >&2
    exit 1
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/binflow-lint-base.XXXXXX")
trap 'rm -rf "$WORK"' EXIT INT TERM

cd "$ROOT"

echo "tool:   $($GOLANGCI --version)"
echo "config: .golangci.yml (repo root)"
echo ""

# --show-stats=false: v2 appends a stats block we do not want to count.
# The tool's exit code is swallowed on purpose (1 = issues found, and issues
# are the payload here, not a failure).
"$GOLANGCI" run ./... --show-stats=false >"$WORK/out.txt" 2>"$WORK/err.txt" || true
if [ -s "$WORK/err.txt" ]; then
    echo "--- stderr (context) ---"
    cat "$WORK/err.txt"
    echo "------------------------"
    echo ""
fi

# Issue line shape: <file>:<line>:<col>: <message> (<linter>). golangci-lint
# also prints an indented code-context block (source line + caret) under each
# issue when run non-interactively; those lines start with whitespace and are
# skipped so they never inflate the counts.

echo "## issues per package"
awk '
/^[[:space:]]/ { next } # code-context block
!/^[^:]+:[0-9]+:[0-9]+: / { next } # not an issue line
{
    file = $1
    sub(/:$/, "", file)
    sub(/^\.\//, "", file)
    if (file ~ /\//) {
        dir = file
        sub(/\/[^\/]*$/, "", dir)
    } else {
        dir = "(root)"
    }
    print dir
}
' "$WORK/out.txt" | sort | uniq -c | sort -rn | awk '
BEGIN { printf "| package | issues |\n|---|---|\n"; total = 0 }
{ total += $1; printf "| %s | %s |\n", $2, $1 }
END { printf "| **total** | **%d** |\n", total }
'

echo ""
echo "## issues per linter"
awk '
/^[[:space:]]/ { next }
!/^[^:]+:[0-9]+:[0-9]+: / { next }
{
    line = $0
    if (match(line, /\([^)]+\)[[:space:]]*$/)) {
        l = substr(line, RSTART + 1, RLENGTH - 1)
        sub(/\)[[:space:]]*$/, "", l)
        print l
    } else {
        print "(unparsed)"
    }
}
' "$WORK/out.txt" | sort | uniq -c | sort -rn | awk '
BEGIN { printf "| linter | issues |\n|---|---|\n" }
{ printf "| %s | %s |\n", $2, $1 }
'

echo ""
TOTAL=$(awk '
/^[[:space:]]/ { next }
!/^[^:]+:[0-9]+:[0-9]+: / { next }
{ n++ }
END { print n + 0 }
' "$WORK/out.txt")
echo "golangci-lint baseline: $TOTAL issues total (FR-70/V31 target = 0; internal/auth is the tracked debt line)"
