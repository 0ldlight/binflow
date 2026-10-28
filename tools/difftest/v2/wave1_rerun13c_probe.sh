#!/usr/bin/env bash
# wave1-rerun13c probe — ground-truth check of both sides BEFORE fixture push.
# Prints statuses / repo lists only; credentials never echoed.
set -u

E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref
E_LZW=/Users/lzw/vscodeProjects/binflow/.env.lzw

A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
B_USER="$(grep -E '^BINFLOW_LZW_USER=' "$E_LZW" | head -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
B_PASSWORD="$(grep -E '^BINFLOW_LZW_ADMIN_PASSWORD=' "$E_LZW" | head -1 | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"

for v in A_USER A_PASSWORD B_USER B_PASSWORD; do
  val="${!v}"
  [ -n "$val" ] || { echo "ENV MISSING: $v"; exit 2; }
done
echo "env loaded (values not printed)"

A_HOST=http://192.168.120.36:32082
B_HOST=http://192.168.120.36:32083

echo "== A version =="
curl -s -u "$A_USER:$A_PASSWORD" "$A_HOST/artifactory/api/system/version" | head -c 200; echo
echo "== B version =="
curl -s -u "$B_USER:$B_PASSWORD" "$B_HOST/binflow/api/system/version" | head -c 200; echo

echo "== A repo list (keys only) =="
curl -s -u "$A_USER:$A_PASSWORD" "$A_HOST/artifactory/api/repositories" \
  | python3 -c 'import json,sys; print(sorted(r["key"] for r in json.load(sys.stdin)))'
echo "== B repo list (keys only) =="
curl -s -u "$B_USER:$B_PASSWORD" "$B_HOST/binflow/api/repositories" \
  | python3 -c 'import json,sys; print(sorted(r["key"] for r in json.load(sys.stdin)))'

probe() { # label user pass url
  code="$(curl -s -o /tmp/dcr_probe_body.$$ -w '%{http_code}' -u "$2:$3" -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' -I "$4")"
  echo "$1 -> $code"
  rm -f /tmp/dcr_probe_body.$$
}
echo "== docker face probes (HEAD) =="
probe "A /v2/difftest-docker%2Fsmoke/manifests/t1  (local-repo form)" "$A_USER" "$A_PASSWORD" "$A_HOST/v2/difftest-docker%2Fsmoke/manifests/t1"
probe "A /v2/difftest-docker/smoke/manifests/t1    (slash form)"     "$A_USER" "$A_PASSWORD" "$A_HOST/v2/difftest-docker/smoke/manifests/t1"
probe "A /v2/diff-docker-remote/difftest-docker/smoke/manifests/t2   (wave1 remote form)" "$A_USER" "$A_PASSWORD" "$A_HOST/v2/diff-docker-remote/difftest-docker/smoke/manifests/t2"
probe "B /v2/difftest-docker%2Fsmoke/manifests/t1  (local-repo form)" "$B_USER" "$B_PASSWORD" "$B_HOST/v2/difftest-docker%2Fsmoke/manifests/t1"
probe "B /v2/difftest-docker/smoke/manifests/t1    (slash form)"     "$B_USER" "$B_PASSWORD" "$B_HOST/v2/difftest-docker/smoke/manifests/t1"
probe "B /v2/diff-docker-remote/difftest-docker/smoke/manifests/t2   (wave1 remote form)" "$B_USER" "$B_PASSWORD" "$B_HOST/v2/diff-docker-remote/difftest-docker/smoke/manifests/t2"
