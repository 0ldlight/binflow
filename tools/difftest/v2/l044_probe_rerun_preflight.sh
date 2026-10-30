#!/usr/bin/env bash
# L043 probe rerun preflight wrapper (credentials via gitignored root env files).
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref
E_LZW=/Users/lzw/vscodeProjects/binflow/.env.lzw

strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
B_USER="$(grep -E '^BINFLOW_LZW_USER=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
B_PASSWORD="$(grep -E '^BINFLOW_LZW_ADMIN_PASSWORD=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"

export A_BASE=http://192.168.120.36:32082
export B_BASE=http://192.168.120.36:32083
export A_USER A_PASSWORD B_USER B_PASSWORD
export DIFFTEST_UPSTREAM_BASE=http://192.168.120.36:32100

cd "$HERE"
python3 -I l044_probe_rerun_preflight.py
