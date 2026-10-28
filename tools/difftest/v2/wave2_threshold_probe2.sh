#!/usr/bin/env bash
# wave2 Task-1 probe #2 — window semantics under silence + re-arm law.
# Same credential discipline as wave2_threshold_probe.sh.
set -eu

HERE="$(cd "$(dirname "$0")" && pwd)"
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref

strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"

export A_BASE=http://192.168.120.36:32082
export A_USER A_PASSWORD

python3 "$HERE/wave2_threshold_probe2.py"
