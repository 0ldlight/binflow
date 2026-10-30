#!/usr/bin/env bash
# Face-topology probe round 2 (2026-10-29):
#   - B maven/npm repo content-face prefix (root vs /binflow)
#   - A/B npm-face on an npm-type repo
#   - A remote self-reference pull-through via router face (the old-env
#     direct-port workaround is dead: no :32081 listener)
# Test namespace diff-probe-* only; all created repos deleted at the end.
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
ACURL="curl -s -u $A_USER:$A_PASSWORD"
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
codea() { code -u "$A_USER:$A_PASSWORD" "$@"; }
codeb() { code -u "$B_USER:$B_PASSWORD" "$@"; }

echo "== B maven repo content face =="
curl -s -o /dev/null -X DELETE -u "$B_USER:$B_PASSWORD" "$B/binflow/api/repositories/diff-probe-face"
echo "B PUT /binflow/api/repositories/diff-probe-face (maven local) -> $(codeb -X PUT "$B/binflow/api/repositories/diff-probe-face" -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"maven"}')"
echo "B GET root   /diff-probe-face/    -> $(codeb "$B/diff-probe-face/")"
echo "B GET ctx    /binflow/diff-probe-face/ -> $(codeb "$B/binflow/diff-probe-face/")"

echo "== A npm-type repo npm face =="
curl -s -o /dev/null -X DELETE -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-npm"
echo "A PUT diff-probe-npm (npm local) -> $(codea -X PUT "$A/artifactory/api/repositories/diff-probe-npm" -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"npm"}')"
echo "A GET /artifactory/api/npm/diff-probe-face/ (maven repo, npm face) -> $(codea "$A/artifactory/api/npm/diff-probe-face/")"
echo "A GET /artifactory/api/npm/diff-probe-npm/  (npm repo, npm face)   -> $(codea "$A/artifactory/api/npm/diff-probe-npm/")"
echo "A GET /api/npm/diff-probe-npm/              (root alias)           -> $(codea "$A/api/npm/diff-probe-npm/")"

echo "== B npm-type repo npm face =="
curl -s -o /dev/null -X DELETE -u "$B_USER:$B_PASSWORD" "$B/binflow/api/repositories/diff-probe-npm"
echo "B PUT diff-probe-npm (npm local) -> $(codeb -X PUT "$B/binflow/api/repositories/diff-probe-npm" -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"npm"}')"
echo "B GET /binflow/api/npm/diff-probe-npm/ -> $(codeb "$B/binflow/api/npm/diff-probe-npm/")"

echo "== A self-reference remote pull-through (router face) =="
# seed a packument into diff-probe-npm via the npm publish wire (curl PUT)
PKT='{"_id":"diff-probe-pkg","name":"diff-probe-pkg","versions":{"1.0.0":{"name":"diff-probe-pkg","version":"1.0.0","dist":{"tarball":"diff-probe-pkg/-/diff-probe-pkg-1.0.0.tgz","shasum":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}}},"dist-tags":{"latest":"1.0.0"}}'
echo "A PUT packument via npm face -> $(codea -X PUT "$A/artifactory/api/npm/diff-probe-npm/diff-probe-pkg" -H 'Content-Type: application/json' -d "$PKT")"
curl -s -o /dev/null -X DELETE -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-rem"
echo "A PUT diff-probe-rem (npm remote, url=router face) -> $(codea -X PUT "$A/artifactory/api/repositories/diff-probe-rem" -H 'Content-Type: application/json' -d '{"rclass":"remote","packageType":"npm","url":"'"$A"'/artifactory/api/npm/diff-probe-npm","username":"'"$A_USER"'","password":"'"$A_PASSWORD"'"}')"
echo "A GET packument via remote (pull-through) -> $(codea "$A/artifactory/api/npm/diff-probe-rem/diff-probe-pkg")"
echo "-- body head: $(curl -s -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/npm/diff-probe-rem/diff-probe-pkg" | head -c 160)"

echo "== cleanup =="
for r in diff-probe-rem diff-probe-npm; do
  echo "A DELETE $r -> $(codea -X DELETE "$A/artifactory/api/repositories/$r")"
done
echo "B DELETE diff-probe-face -> $(codeb -X DELETE "$B/binflow/api/repositories/diff-probe-face")"
echo "B DELETE diff-probe-npm  -> $(codeb -X DELETE "$B/binflow/api/repositories/diff-probe-npm")"
sleep 2
echo "A verify diff-probe-rem -> $(codea "$A/artifactory/api/repositories/diff-probe-rem")"
echo "B verify diff-probe-face -> $(codeb "$B/binflow/api/repositories/diff-probe-face")"
