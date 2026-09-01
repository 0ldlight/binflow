#!/usr/bin/env bash
# m15-aql-perf.sh — re-run the T-411 AQL performance legs (AC3, NFR-P67
# pre-leg: item domain P95 <= 500ms, property join P95 <= 800ms over the
# 12k-node corpus, local SSD + WAL).
#
# The corpus is built inside the test (deterministic: 6 repos x 2000 file
# nodes across depths 0..4, 330 folder markers, 1500 shared blobs, 24k
# property rows; ~2.5s to seed) — see
# internal/metadata/aqlperf_internal_test.go.
#
# Flags after the script reach go test verbatim (e.g. -count=5).
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./internal/metadata -run 'TestT411Perf(ItemDomain|PropertyJoin)' -count=1 -v "$@"
