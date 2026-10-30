#!/usr/bin/env bash
# Rerun (2026-10-29, wave 2) — the cases affected by the harness fixes after
# the first apiface batch: 3 npm layout cases (B-local generic twin upstream,
# retired layout proxy), aggregate entry (patch alias fix; expects BLOCKED on
# DIFFTEST_B_DB), generic-mime (curl content-face base).
set -eu
WT=/Users/lzw/vscodeProjects/binflow/.claude/worktrees/wave-docker-remote
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

cd "$WT"

python3 -I tools/difftest/v2/runner.py \
  --out "tools/difftest/v2/run/apiwave-fix1/${1:-rerun2}/results.json" \
  --case npm-cache-member-layout \
  --case npm-cli-tarball-fidelity \
  --case npm-virtual-packument-merge \
  --case npm-virtual-aggregate-delete-entry \
  --case generic-mime-ownership
