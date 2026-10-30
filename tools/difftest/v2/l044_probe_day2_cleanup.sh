#!/usr/bin/env bash
# Day-2 post-batch: B catalog cleanup check + upstream GC ledger enumeration.
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
echo "== B catalog after day-2 batch (expect only fixture entry, no diff-cat5 residue) =="
curl -s -u "$B_USER:$B_PASSWORD" "$B_BASE/v2/_catalog"
echo "== A-side temp repo check (diff-cat5-* should be gone) =="
curl -s -u "$A_USER:$A_PASSWORD" "$A_BASE/artifactory/api/repositories" | python3 -I -c "
import json,sys
repos=[r['key'] for r in json.load(sys.stdin)]
res=[k for k in repos if k.startswith('diff-cat')]
print('diff-cat repos on A:', res if res else 'none', '| total repos:', len(repos))"
echo "== upstream wave5 tag ledger =="
python3 -I l044_probe_upstream_gc_ledger.py
