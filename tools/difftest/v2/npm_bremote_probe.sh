#!/usr/bin/env bash
# Probe (2026-10-29): does BinFlow's npm remote still request
# <name>/packument.json (T-538 drift A) or the standard /<name>?
# Method: seed src on A (real npm publish), point a B remote DIRECTLY at the
# A router-face npm URL (allowPrivateUpstream), GET the packument through the
# B remote. 200 -> drift gone (direct connection viable); 404 -> drift stands.
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

curl -s -o /dev/null -X DELETE -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-npm"
curl -s -o /dev/null -X PUT -u "$A_USER:$A_PASSWORD" "$A/artifactory/api/repositories/diff-probe-npm" \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"npm"}'
mkdir -p "$W/pkg"
printf '{"name":"diff-probe-pkg","version":"1.0.0","description":"probe seed"}\n' > "$W/pkg/package.json"
echo "module.exports='probe';" > "$W/pkg/index.js"
NPM_AUTH="$(printf '%s:%s' "$A_USER" "$A_PASSWORD" | base64)"
printf '//192.168.120.36:32082/artifactory/api/npm/diff-probe-npm/:_auth=%s\n' "$NPM_AUTH" > "$W/npmrc"
(cd "$W/pkg" && npm publish --registry "$A/artifactory/api/npm/diff-probe-npm/" \
  --userconfig "$W/npmrc" --loglevel error >/dev/null 2>&1) && echo "seed publish ok"
echo "A src face packument -> $(codea "$A/artifactory/api/npm/diff-probe-npm/diff-probe-pkg")"

curl -s -o /dev/null -X DELETE -u "$B_USER:$B_PASSWORD" "$B/binflow/api/repositories/diff-probe-brem"
rc="$(codeb -X PUT "$B/binflow/api/repositories/diff-probe-brem" -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"npm","url":"'"$A"'/artifactory/api/npm/diff-probe-npm","allowPrivateUpstream":true}')"
echo "B remote PUT (direct A url, no creds) -> $rc"
echo "B remote packument GET -> $(codeb "$B/binflow/api/npm/diff-probe-brem/diff-probe-pkg")"
echo "  body head: $(curl -s -u "$B_USER:$B_PASSWORD" "$B/binflow/api/npm/diff-probe-brem/diff-probe-pkg" | head -c 200)"

echo "== cleanup =="
echo "B DELETE brem -> $(codeb -X DELETE "$B/binflow/api/repositories/diff-probe-brem")"
echo "A DELETE npm  -> $(codea -X DELETE "$A/artifactory/api/repositories/diff-probe-npm")"
