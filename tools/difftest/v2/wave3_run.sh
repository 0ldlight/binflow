#!/usr/bin/env bash
# wave3 — L043 candidate deep-dive batch (catalog topology / schema1 cache-phase /
# bearer cross-face matrix). Six diff env vars + DIFFTEST_UPSTREAM_BASE.
# Output: run/wave3 (first pass) / run/wave3-2 (re-run gate). Run twice.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref
E_LZW=/Users/lzw/vscodeProjects/binflow/.env.lzw

strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
B_USER="$(grep -E '^BINFLOW_LZW_USER=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
B_PASSWORD="$(grep -E '^BINFLOW_LZW_ADMIN_PASSWORD=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"

export A_BASE=http://192.168.120.36:32082   # A docker v2 face = bare host root
export B_BASE=http://192.168.120.36:32083   # B docker v2 face = bare host root
export A_USER A_PASSWORD B_USER B_PASSWORD
export DIFFTEST_UPSTREAM_BASE=http://192.168.120.36:32100

OUT="${1:-run/wave3}"

cd "$HERE"
python3 runner.py --out "$OUT" \
  --case dcr-catalog-bare-root-topology \
  --case dcr-schema1-cache-phase \
  --case dcr-bearer-crossface-challenge
echo "---- score ----"
bash score.sh "$OUT/results.json"
