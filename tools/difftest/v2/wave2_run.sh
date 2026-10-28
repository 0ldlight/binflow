#!/usr/bin/env bash
# wave2 — six L042-unknown arms + breaker contamination guard.
# Six diff env vars + DIFFTEST_UPSTREAM_BASE (fixture relay, case-local need).
# Output: run/wave2/. Run twice back-to-back for the re-run gate.
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

OUT="${1:-run/wave2}"

cd "$HERE"
python3 runner.py --out "$OUT" \
  --case dcr-negative-cache-ttl \
  --case dcr-catalog-empty-degrade \
  --case dcr-star-inm \
  --case binflow-v2-prefix-face \
  --case dcr-schema1-pull-block \
  --case dcr-bearer-tamper-arms
echo "---- score ----"
bash score.sh "$OUT/results.json"
