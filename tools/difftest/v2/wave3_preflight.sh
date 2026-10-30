#!/usr/bin/env bash
# wave3 preflight — instance health + fixture liveness on both sides.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref
E_LZW=/Users/lzw/vscodeProjects/binflow/.env.lzw
strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
B_PASSWORD="$(grep -E '^BINFLOW_LZW_ADMIN_PASSWORD=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
B_USER="$(grep -E '^BINFLOW_LZW_USER=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"

echo "== A REST ping =="
curl -sS -o /dev/null -w '%{http_code}\n' -u "$A_USER:$A_PASSWORD" http://192.168.120.36:32082/artifactory/api/system/ping
echo "== B REST ping =="
curl -sS -o /dev/null -w '%{http_code}\n' -u "$B_USER:$B_PASSWORD" http://192.168.120.36:32083/binflow/api/system/ping
echo "== fixture: remote manifest t2 via A (Accept schema2) =="
curl -sS -o /dev/null -w '%{http_code}\n' -u "$A_USER:$A_PASSWORD" \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
  http://192.168.120.36:32082/v2/diff-docker-remote/difftest-docker/smoke/manifests/t2
echo "== fixture: remote manifest t2 via B =="
curl -sS -o /dev/null -w '%{http_code}\n' -u "$B_USER:$B_PASSWORD" \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
  http://192.168.120.36:32083/v2/diff-docker-remote/difftest-docker/smoke/manifests/t2
echo "== upstream relay t2 =="
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
  http://192.168.120.36:32100/v2/difftest-docker/smoke/manifests/t2
echo "== B version =="
curl -sS -u "$B_USER:$B_PASSWORD" http://192.168.120.36:32083/binflow/api/system/version 2>/dev/null | head -c 300; echo
