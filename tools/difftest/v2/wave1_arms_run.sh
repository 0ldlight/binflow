#!/usr/bin/env bash
# wave1-arms — L042 unknown-arm replay batch (six committed arms + anonymous
# catalog supplement), evidence to run/wave1-arms/. Follow-up batch to the
# wave-2 (L043) first execution; the +6min negative-cache leg runs separately:
#   python3 wave1_arms_ttl6m_probe.py            # waits 360s itself
# Run twice back-to-back for the re-run gate.
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
export DIFFTEST_DOCKER_UPSTREAM=http://192.168.120.36:32100

OUT="${1:-run/wave1-arms}"

cd "$HERE"

# fixture: remote repo diff-docker-remote + upstream smoke tags (idempotent)
bash docker_wave_setup.sh

python3 runner.py --out "$OUT" \
  --case dcr-negative-cache-ttl \
  --case dcr-catalog-empty-degrade \
  --case dcr-catalog-anonymous \
  --case dcr-star-inm \
  --case binflow-v2-prefix-face \
  --case dcr-schema1-pull-block \
  --case dcr-bearer-tamper-arms
echo "---- score ----"
bash score.sh "$OUT/results.json"
echo "---- next ----"
echo "+6min leg: python3 wave1_arms_ttl6m_probe.py --out $OUT/ttl6m_probe.json"
