#!/usr/bin/env bash
# Face-topology probe for the maven/npm/rest un-block fix (2026-10-29).
# Answers, with no secrets printed:
#   A root /api alias vs /artifactory/api; repo content face at root vs
#   /artifactory/<repo>; direct-port candidate :32081; B /binflow/api.
# Test repo namespace: diff-probe-face (deleted at the end).
set -eu
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref
E_LZW=/Users/lzw/vscodeProjects/binflow/.env.lzw
strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
B_USER="$(grep -E '^BINFLOW_LZW_USER=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
B_PASSWORD="$(grep -E '^BINFLOW_LZW_ADMIN_PASSWORD=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
A=http://192.168.120.36:32082
B=http://192.168.120.36:32083
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

echo "== A root alias /api =="
echo "A  /api/system/ping            -> $(code -u "$A_USER:$A_PASSWORD" "$A/api/system/ping")"
echo "A  /artifactory/api/system/ping -> $(code -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/system/ping")"
echo "A  /artifactory/api/repositories -> $(code -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories")"
echo "A  /api/repositories            -> $(code -u "$A_USER:$A_PASSWORD" "$A/api/repositories")"

echo "== A direct-port candidate :32081 =="
echo "A  :32081/artifactory/api/system/ping -> $(code -m 4 -u "$A_USER:$A_PASSWORD" "http://192.168.120.36:32081/artifactory/api/system/ping" || echo conn-fail)"

echo "== A repo content face (create diff-probe-face local maven) =="
curl -s -o /dev/null -X DELETE -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-face"
put=$(code -u "$A_USER:$A_PASSWORD" -X PUT "$A/artifactory/api/repositories/diff-probe-face" \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"maven"}')
echo "A  PUT /artifactory/api/repositories/diff-probe-face -> $put"
echo "A  GET /artifactory/api/repositories/diff-probe-face -> $(code -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-face")"
echo "A  root content /diff-probe-face/               -> $(code -u "$A_USER:$A_PASSWORD" "$A/diff-probe-face/")"
echo "A  ctx content /artifactory/diff-probe-face/    -> $(code -u "$A_USER:$A_PASSWORD" "$A/artifactory/diff-probe-face/")"
echo "A  npm face /artifactory/api/npm/diff-probe-face/ -> $(code -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/npm/diff-probe-face/")"
echo "A  npm face root /api/npm/diff-probe-face/        -> $(code -u "$A_USER:$A_PASSWORD" "$A/api/npm/diff-probe-face/")"
echo "A  storage /artifactory/api/storage/diff-probe-face -> $(code -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/storage/diff-probe-face?list&deep=0")"

echo "== B /binflow faces =="
echo "B  /binflow/api/system/ping    -> $(code -u "$B_USER:$B_PASSWORD" "$B/binflow/api/system/ping")"
echo "B  /binflow/api/repositories   -> $(code -u "$B_USER:$B_PASSWORD" "$B/binflow/api/repositories")"
echo "B  /binflow/api/npm/x/         -> $(code -u "$B_USER:$B_PASSWORD" "$B/binflow/api/npm/x/")"
echo "B  /binflow/api/storage/x      -> $(code -u "$B_USER:$B_PASSWORD" "$B/binflow/api/storage/x")"
echo "B  root /api/system/ping       -> $(code -u "$B_USER:$B_PASSWORD" "$B/api/system/ping")"

echo "== cleanup =="
del=$(code -u "$A_USER:$A_PASSWORD" -X DELETE "$A/artifactory/api/repositories/diff-probe-face")
echo "A  DELETE diff-probe-face -> $del"
echo "A  verify GET -> $(code -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-face")"
