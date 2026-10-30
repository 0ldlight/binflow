#!/usr/bin/env bash
# wave5 — L043 five-candidate live probes batch (catalog membership / schema1
# phase boundary / bearer full-face matrix / tags liveness / accept strictness
# / schema1 mutation heal). Six diff env vars + DIFFTEST_UPSTREAM_BASE.
# Output: run/l043-probe (first pass) / run/l043-probe-2 (re-run gate). Run twice.
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

OUT="${1:-run/l043-probe}"

cd "$HERE"
python3 runner.py --out "$OUT" \
  --case dcr-catalog-membership-matrix \
  --case dcr-schema1-phase-boundary \
  --case dcr-bearer-face-full-matrix \
  --case dcr-tags-list-liveness \
  --case dcr-tags-list-accept-strictness \
  --case dcr-schema1-tag-mutation-heal
echo "---- score ----"
bash score.sh "$OUT/results.json"
