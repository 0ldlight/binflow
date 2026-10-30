#!/usr/bin/env bash
# Face-topology probe round 3 (2026-10-29):
#   - real npm publish seed on A npm face, then router-face self-reference
#     pull-through verdict (the old-env direct-port workaround is dead)
#   - GET repo config for a NEVER-EXISTED repo on A and B (404 or 400?)
#   - GET content face root for never-existed repo (probe semantics)
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
codea() { code -u "$A_USER:$A_PASSWORD" "$@"; }
codeb() { code -u "$B_USER:$B_PASSWORD" "$@"; }
W="$(mktemp -d)"; trap 'rm -rf "$W"' EXIT

echo "== seed via real npm client on A npm face =="
curl -s -o /dev/null -X DELETE -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-npm"
curl -s -o /dev/null -X PUT -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-npm" \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"npm"}'
mkdir -p "$W/pkg"
cat > "$W/pkg/package.json" <<'EOF'
{"name":"diff-probe-pkg","version":"1.0.0","description":"probe seed"}
EOF
echo "module.exports='probe';" > "$W/pkg/index.js"
NPM_AUTH="$(printf '%s:%s' "$A_USER" "$A_PASSWORD" | base64)"
echo "//192.168.120.36:32082/:_auth=$NPM_AUTH" > "$W/npmrc"
echo "always-auth=true" >> "$W/npmrc"
(cd "$W/pkg" && npm publish --registry "$A/artifactory/api/npm/diff-probe-npm/" \
  --userconfig "$W/npmrc" --loglevel error >/dev/null 2>&1) && echo "npm publish -> ok" || echo "npm publish -> FAILED"
echo "A GET packument src face -> $(codea "$A/artifactory/api/npm/diff-probe-npm/diff-probe-pkg")"

echo "== A self-reference remote pull-through verdict =="
curl -s -o /dev/null -X DELETE -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-rem"
curl -s -o /dev/null -X PUT -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-rem" \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"npm","url":"'"$A"'/artifactory/api/npm/diff-probe-npm","username":"'"$A_USER"'","password":"'"$A_PASSWORD"'"}'
st="$(codea "$A/artifactory/api/npm/diff-probe-rem/diff-probe-pkg")"
echo "A GET packument via remote (pull-through) -> $st"
echo "-- body head: $(curl -s -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/npm/diff-probe-rem/diff-probe-pkg" | head -c 200)"

echo "== never-existed repo GET status =="
echo "A GET /artifactory/api/repositories/diff-never-existed -> $(codea "$A/artifactory/api/repositories/diff-never-existed")"
echo "A GET /artifactory/api/storage/diff-never-existed -> $(codea "$A/artifactory/api/storage/diff-never-existed")"
echo "A GET /artifactory/diff-never-existed/ (content root) -> $(codea "$A/artifactory/diff-never-existed/")"
echo "B GET /binflow/api/repositories/diff-never-existed -> $(codeb "$B/binflow/api/repositories/diff-never-existed")"
echo "B GET /binflow/api/storage/diff-never-existed -> $(codeb "$B/binflow/api/storage/diff-never-existed")"
echo "B GET /binflow/diff-never-existed/ (content root) -> $(codeb "$B/binflow/diff-never-existed/")"

echo "== cleanup =="
echo "A DELETE diff-probe-rem -> $(codea -X DELETE "$A/artifactory/api/repositories/diff-probe-rem")"
echo "A DELETE diff-probe-npm -> $(codea -X DELETE "$A/artifactory/api/repositories/diff-probe-npm")"
