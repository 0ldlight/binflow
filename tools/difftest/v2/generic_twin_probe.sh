#!/usr/bin/env bash
# Mini-probe (2026-10-29): B remote -> B-local generic twin (anonymous
# content read). Validates the retired-proxy replacement end to end.
set -eu
E_LZW=/Users/lzw/vscodeProjects/binflow/.env.lzw
strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
B_USER="$(grep -E '^BINFLOW_LZW_USER=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
B_PASSWORD="$(grep -E '^BINFLOW_LZW_ADMIN_PASSWORD=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
B=http://192.168.120.36:32083
put()  { curl -s -o /dev/null -w '%{http_code}' "$@"; }
codeb() { put -u "$B_USER:$B_PASSWORD" "$@"; }
G=difftest-probe-gsrc
W="$(mktemp -d)"; trap 'rm -rf "$W"' EXIT

mkdir -p "$W/pkg/package"
printf '{"name":"diff-twin-pkg","version":"1.0.0"}\n' > "$W/pkg/package/package.json"
echo "module.exports='twin';" > "$W/pkg/package/index.js"
(cd "$W/pkg" && tar -cf ../p.tar package && gzip -n -9 -c ../p.tar > ../p.tgz)
TGZ="$W/p.tgz"
SHA1="$(shasum -a 1 "$TGZ" | cut -d' ' -f1)"
TARBALL="$B/binflow/$G/diff-twin-pkg/-/diff-twin-pkg-1.0.0.tgz"
printf '{"_id":"diff-twin-pkg","name":"diff-twin-pkg","description":"twin probe","dist-tags":{"latest":"1.0.0"},"versions":{"1.0.0":{"_id":"diff-twin-pkg@1.0.0","name":"diff-twin-pkg","version":"1.0.0","description":"twin probe","dist":{"shasum":"%s","tarball":"%s"}}}}' \
  "$SHA1" "$TARBALL" > "$W/packument.json"

echo "seed twin ON B:"
echo "  create generic repo  -> $(codeb -X PUT "$B/binflow/api/repositories/$G" -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"generic"}')"
echo "  PUT packument.json   -> $(codeb -X PUT "$B/binflow/$G/diff-twin-pkg/packument.json" -H 'Content-Type: application/json' --data-binary "@$W/packument.json")"
echo "  PUT tarball          -> $(codeb -X PUT "$B/binflow/$G/diff-twin-pkg/-/diff-twin-pkg-1.0.0.tgz" -H 'Content-Type: application/octet-stream' --data-binary "@$TGZ")"
echo "  twin packument GET   -> $(codeb "$B/binflow/$G/diff-twin-pkg/packument.json")"

echo "B remote -> B-local twin:"
echo "  create B remote      -> $(codeb -X PUT "$B/binflow/api/repositories/diff-probe-brem" -H 'Content-Type: application/json' -d '{"rclass":"remote","packageType":"npm","url":"'"$B"'/binflow/'"$G"'","allowPrivateUpstream":true}')"
echo "  B packument via rem  -> $(codeb "$B/binflow/api/npm/diff-probe-brem/diff-twin-pkg")"
echo "    body head: $(curl -s -u "$B_USER:$B_PASSWORD" "$B/binflow/api/npm/diff-probe-brem/diff-twin-pkg" | head -c 220)"
echo "  B tarball via rem    -> $(codeb "$B/binflow/api/npm/diff-probe-brem/diff-twin-pkg/-/diff-twin-pkg-1.0.0.tgz")"

echo "cleanup:"
echo "  B DELETE rem  -> $(codeb -X DELETE "$B/binflow/api/repositories/diff-probe-brem")"
echo "  B DELETE twin -> $(codeb -X DELETE "$B/binflow/api/repositories/$G")"
