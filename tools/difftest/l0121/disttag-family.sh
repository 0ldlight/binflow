#!/bin/bash
# l0121 dist-tag family — real npm CLI (10.9.8) through wiretap.py, one log per case.
# LOOP 012 / L012-1 forensics entry point. Serial; no background jobs.
#
# Usage: disttag-family.sh <a|b>
#   a = Artifactory reference  :8082 (forward via 127.0.0.1:19082)
#   b = BinFlow UAT d301cf31   :8083 (forward via 127.0.0.1:19083)
# Env: ARTI_AUTH / BF_AUTH = "user:password" — injected by wiretap into forwarded
# requests only; never on argv, never in logs (wiretap redacts).
set -u
SIDE=${1:?side a|b}
NPM=/tmp/l0121/npm10/node_modules/.bin/npm
TAP=/Users/lzw/dev-center/tools/difftest/l0121/wiretap.py
LOGS=/Users/lzw/dev-center/reports/compatibility/l0121-wire/$SIDE
WS=/tmp/l0121/ws/$SIDE
mkdir -p "$LOGS"
SUMMARY="$LOGS/_summary.txt"
: > "$SUMMARY"

if [ "$SIDE" = a ]; then
  PORT=19082; TARGET=http://localhost:8082
  REG=http://127.0.0.1:19082/artifactory/api/npm/l0121-npm/
  DIRECT_REG=http://localhost:8082/artifactory/api/npm/l0121-npm
  AUTHVAR=ARTI_AUTH; NCMD=(curl -sS --noproxy '*' -u "$ARTI_AUTH")
else
  PORT=19083; TARGET=http://localhost:8083
  REG=http://127.0.0.1:19083/binflow/api/npm/l0121-npm/
  DIRECT_REG=http://localhost:8083/binflow/api/npm/l0121-npm
  AUTHVAR=BF_AUTH; NCMD=(curl -sS --noproxy '*' -u "$BF_AUTH")
fi
if [ -z "${!AUTHVAR:-}" ]; then echo "\$$AUTHVAR not set" >&2; exit 2; fi

# npm publish pre-checks registry-scoped credentials client-side (ENEEDAUTH,
# zero HTTP). The CLI carries a FAKE registry-scoped token (not a credential,
# safe on disk); wiretap replaces the Authorization header with the real Basic
# upstream, so real creds never leave the wiretap process env.
printf '//127.0.0.1:%s/%s/api/npm/l0121-npm/:_authToken=l0121-fake-client-token\n' \
  "$PORT" "$([ "$SIDE" = a ] && echo artifactory || echo binflow)" > /tmp/l0121/npmrc-fake-$SIDE

NPMFLAGS=(--userconfig /tmp/l0121/npmrc-fake-$SIDE --cache /tmp/l0121/npm-cache-$SIDE
          --registry "$REG" --prefer-online --no-fund --no-audit --loglevel warn
          --fetch-retries 0 --fetch-timeout 30000)

run_case() { # run_case <case-id> <workdir> <npm-args...>
  local id=$1 wd=$2; shift 2
  echo "== case $id" | tee -a "$SUMMARY"
  local out rc
  out=$(TAP_CWD=$wd python3 "$TAP" --target "$TARGET" --port "$PORT" \
        --log "$LOGS/$id.log" --auth-env "$AUTHVAR" -- \
        "$NPM" "${NPMFLAGS[@]}" "$@" 2>&1)
  rc=$?
  printf '%s\n' "$out" | tail -5 | sed 's/^/   | /'
  echo "case $id exit=$rc" >> "$SUMMARY"
}

# ---- preconditions outside the tap: version bumps (no registry traffic) ----
setver() { # setver <dir> <name> <version>
  printf '{"name":"%s","version":"%s","description":"l0121 dist-tag wire probe"}' "$2" "$3" > "$1/package.json"
}
setver "$WS/pkg-unscoped" l0121-probe-pkg 1.0.0
setver "$WS/pkg-scoped" '@l0121-scope/probe-pkg' 1.0.0

# ---- publish preconditions (wire captured: p1..p4) ----
run_case p1-publish-unscoped-100 "$WS/pkg-unscoped" publish
setver "$WS/pkg-unscoped" l0121-probe-pkg 1.1.0
run_case p2-publish-unscoped-110 "$WS/pkg-unscoped" publish
run_case p3-publish-scoped-100 "$WS/pkg-scoped" publish
setver "$WS/pkg-scoped" '@l0121-scope/probe-pkg' 1.1.0
run_case p4-publish-scoped-110 "$WS/pkg-scoped" publish

# ---- dist-tag family ----
run_case ls1-basic "$WS" dist-tag ls l0121-probe-pkg
run_case add1-beta "$WS" dist-tag add l0121-probe-pkg@1.0.0 beta
run_case add2-idempotent "$WS" dist-tag add l0121-probe-pkg@1.0.0 beta
run_case ls2-after-add "$WS" dist-tag ls l0121-probe-pkg
run_case rm1-beta "$WS" dist-tag rm l0121-probe-pkg beta
run_case rm2-nonexistent "$WS" dist-tag rm l0121-probe-pkg nosuchtag
run_case ghost-ls "$WS" dist-tag ls l0121-ghost-pkg
run_case badver-add "$WS" dist-tag add l0121-probe-pkg@9.9.9 canary

# ---- scoped variants ----
run_case scope-ls "$WS" dist-tag ls @l0121-scope/probe-pkg
run_case scope-add "$WS" dist-tag add @l0121-scope/probe-pkg@1.0.0 stable
run_case scope-rm "$WS" dist-tag rm @l0121-scope/probe-pkg stable

# ---- no-tag package: strip both tags via the direct API, then ls ----
echo "== notag-prep (direct curl, not tapped)" | tee -a "$SUMMARY"
"${NCMD[@]}" -o /dev/null -w "prep del latest: %{http_code}\n" -X DELETE "$DIRECT_REG/-/package/@l0121-scope%2fprobe-pkg/dist-tags/latest" | tee -a "$SUMMARY"
run_case notag-ls "$WS" dist-tag ls @l0121-scope/probe-pkg
# restore: put latest back so later curl-matrix legs see the canonical shape
"${NCMD[@]}" -o /dev/null -w "prep put latest: %{http_code}\n" -X PUT -H 'Content-Type: application/json' \
  -d '"1.1.0"' "$DIRECT_REG/-/package/@l0121-scope%2fprobe-pkg/dist-tags/latest" | tee -a "$SUMMARY"

echo "---- summary ($SIDE) ----"
cat "$SUMMARY"
