#!/usr/bin/env bash
# API-face fix rerun (2026-10-29) — the 28 maven/npm/rest/conan/generic/demo
# cases that were BLOCKED on the hardcoded bare /api face. Dual-send, six
# env vars only, bare host roots (the scenario the ctx-prefix probe fixes).
# Values never printed.
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
OUT=tools/difftest/v2/run/apiwave-fix1

python3 -I tools/difftest/v2/runner.py --out "$OUT/results.json" \
  --case demo-ping \
  --case maven-auto-materialize-probe \
  --case maven-deploy-201-location-header \
  --case maven-deploy-created-envelope \
  --case maven-deploy-gav-mismatch \
  --case maven-local-handle-walk-skip \
  --case maven-member-snapshot-sidecar-304 \
  --case maven-module-handle-seat-probe \
  --case maven-plainsnap-nonunique-resolve \
  --case maven-resolve-remote-cache \
  --case maven-sidecar-crosssec-stamp \
  --case maven-snapshot-versions-merge \
  --case maven-unique-plain-resolve-forensics \
  --case maven-virtual-delete-passthrough \
  --case maven-virtual-deploy \
  --case maven-virtual-metadata-merge \
  --case maven-virtual-walk-selection \
  --case maven-walk-sameface-mvn \
  --case maven-walk-selection-tiebreak \
  --case maven-walk-trigger-matrix \
  --case npm-cache-member-layout \
  --case npm-cli-tarball-fidelity \
  --case npm-virtual-aggregate-delete-entry \
  --case npm-virtual-packument-merge \
  --case rest-kcache-deploy-404-wording \
  --case rest-nonempty-repo-delete-cascade \
  --case conan-v1-pkgdel-virtual \
  --case generic-mime-ownership
