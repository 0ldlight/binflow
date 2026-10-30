#!/usr/bin/env bash
# wave3 Task-1 wrapper — A-side breaker-ledger scope probe (one-time username).
# Optional $1 = username suffix override (re-run gate uses a fresh default).
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref
strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
export A_BASE=http://192.168.120.36:32082
export A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
export A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
if [ -n "${1:-}" ]; then export SCOPE_USER="$1"; fi
cd "$HERE"
python3 wave3_a_scope_probe.py
