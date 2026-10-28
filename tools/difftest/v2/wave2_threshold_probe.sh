#!/usr/bin/env bash
# wave2 Task-1 — A-side recurrent-failure breaker precise counting.
# Credentials injected from gitignored env file (line-anchored grep); values
# never printed. Run from anywhere; output lands in tools/difftest/v2/run/wave2/.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref

strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"

export A_BASE=http://192.168.120.36:32082
export A_USER A_PASSWORD

python3 "$HERE/wave2_threshold_probe.py"
