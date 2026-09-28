#!/usr/bin/env bash
# T-526: Fern en-pages ratchet gate — the English docs surface (T-521) may
# only grow, never shrink. Counts the page files Fern actually renders under
# fern/translations/en/pages/ (both .md and .mdx — Fern's page extensions;
# the tree today is .mdx-only, verified at T-526 landing) against the
# committed baseline fern/en-pages.count.
#
# Fail-loud on drift in EITHER direction (AC2 decision, settled in-ticket):
#   current < baseline — en coverage regressed (page deleted / migration
#                        damage): restore the page(s). A deliberate removal is
#                        a product decision: route it through a ticket and
#                        lower the baseline in that same, justified change.
#   current > baseline — growth: lock in the new floor by bumping the
#                        baseline (`make fern-en-baseline`) and committing it
#                        WITH the added pages. CI never edits tracked files:
#                        both CI faces are main-push-only (no PR checkout to
#                        commit back to; a CI-side write would need a
#                        credential'd push surface), and a committed baseline
#                        bump keeps every ratchet move an auditable diff. A
#                        stale baseline after growth would also re-open the
#                        shrink window (10 -> 12, then silent drop to 11).
#
# Count-only by design (AC4): content quality / bilingual parity is T-521's
# follow-up acceptance gate, out of scope here. Counting files, not
# docs.yml nav entries: the en nav mirrors zh (41 entries) with field-level
# fallback for untranslated pages, so nav counting would always read 41.
#
# Usage:
#   scripts/fern-en-ratchet.sh           # gate (exit 1 on any drift)
#   scripts/fern-en-ratchet.sh --write   # regenerate fern/en-pages.count

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PAGES_DIR="$ROOT/fern/translations/en/pages"
BASELINE_FILE="$ROOT/fern/en-pages.count"

count_pages() {
    find "$PAGES_DIR" -type f \( -name '*.md' -o -name '*.mdx' \) | wc -l | tr -d '[:space:]'
}

if [ "${1:-}" = "--write" ]; then
    if [ ! -d "$PAGES_DIR" ]; then
        echo "fern-en-ratchet: $PAGES_DIR not found — refusing to write an empty baseline" >&2
        exit 1
    fi
    printf '%s\n' "$(count_pages)" > "$BASELINE_FILE"
    echo "fern-en-baseline: wrote $(cat "$BASELINE_FILE") -> $BASELINE_FILE"
    exit 0
fi

if [ ! -d "$PAGES_DIR" ]; then
    {
        echo "=== FAIL: Fern en tree missing (T-526 ratchet) ==="
        echo "$PAGES_DIR does not exist — the English translation surface is gone entirely."
        echo "fix: restore fern/translations/en/pages/ (git log --oneline -- fern/translations/en/pages/)"
    } >&2
    exit 1
fi
if [ ! -f "$BASELINE_FILE" ]; then
    {
        echo "=== FAIL: baseline file missing (T-526 ratchet) ==="
        echo "fern/en-pages.count not found; regenerate it from the tree:"
        echo "fix: make fern-en-baseline && git add fern/en-pages.count"
    } >&2
    exit 1
fi

CURRENT="$(count_pages)"
BASELINE="$(tr -dc '0-9' < "$BASELINE_FILE")"
if [ -z "$BASELINE" ]; then
    echo "=== FAIL: fern/en-pages.count does not contain a number (T-526) ===" >&2
    exit 1
fi

echo "fern en pages: current=$CURRENT baseline=$BASELINE (fern/translations/en/pages/**/*.{md,mdx})"

if [ "$CURRENT" -eq "$BASELINE" ]; then
    echo "fern-en-ratchet: PASS (en page count holds at $CURRENT)"
    exit 0
fi

if [ "$CURRENT" -lt "$BASELINE" ]; then
    {
        echo "=== FAIL: Fern en page count DROPPED (T-526 ratchet: en coverage may only grow) ==="
        echo "current=$CURRENT baseline=$BASELINE delta=$((CURRENT - BASELINE))"
        echo "fix: restore the removed page(s): git log --oneline -- fern/translations/en/pages/"
        echo "     a deliberate removal is a product decision: route it through a ticket and"
        echo "     lower fern/en-pages.count in that same change, with the justification."
    } >&2
    exit 1
fi

{
    echo "=== FAIL: Fern en page count GREW — baseline must be ratcheted up (T-526) ==="
    echo "current=$CURRENT baseline=$BASELINE delta=+$((CURRENT - BASELINE))"
    echo "fix: make fern-en-baseline && git add fern/en-pages.count"
    echo "     commit the baseline bump together with the added pages, so the new floor is"
    echo "     locked in — a later silent drop back below $CURRENT must fail this gate."
} >&2
exit 1
