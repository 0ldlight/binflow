#!/usr/bin/env bash
# wave2 — B-side breaker spot-check (BinFlow dev-cfa5da05, T-632/T-633 wired).
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
E_LZW=/Users/lzw/vscodeProjects/binflow/.env.lzw

strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
B_USER="$(grep -E '^BINFLOW_LZW_USER=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
B_PASSWORD="$(grep -E '^BINFLOW_LZW_ADMIN_PASSWORD=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"

export B_BASE=http://192.168.120.36:32083
export B_USER B_PASSWORD

python3 "$HERE/wave2_bside_breaker_probe.py"
