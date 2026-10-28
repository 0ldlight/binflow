#!/usr/bin/env bash
# wave1-rerun13c — replay the 8 dcr cases (console-root-path excluded; already
# PASS and conductor-verified). Six diff env vars injected from gitignored env
# files; values never printed. Run from tools/difftest/v2 (runner cwd).
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

cd "$HERE"
python3 runner.py --out run/wave1-rerun13c-2 \
  --case dcr-blob-conditional \
  --case dcr-blob-get \
  --case dcr-manifest-conditional \
  --case dcr-manifest-get \
  --case dcr-manifest-unknown-404 \
  --case dcr-pull-sequence \
  --case dcr-push-reject \
  --case dcr-tags-catalog
echo "---- score ----"
bash score.sh run/wave1-rerun13c-2/results.json
