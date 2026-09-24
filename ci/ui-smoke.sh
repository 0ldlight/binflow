#!/usr/bin/env bash
# ui-smoke.sh — nightly UI smoke against a STANDING BinFlow instance (T-522).
#
# The nightly window's missing UI face (flagged by the 2026-09-25 verification
# report): race_full covers race depth and the protocol matrix covers client
# protocols against the standing UAT, but nothing watched the console/docs
# faces overnight. This smoke answers ONE question — "is the standing UI face
# alive" — not "is every e2e spec green" (that is the e2e job's domain; its
# known-red spec debt family is parameter-gated off since L024-2/L026-4). A
# red here means deploy/config/asset rot on the instance, not spec debt.
#
# Checks (all must pass; legs never mask each other):
#   1. GET /healthz                      -> 200
#   2. GET /binflow                      -> 301 with Location /binflow/ui/
#      (the CE-01 mount-redirect contract)
#   3. GET /binflow/ui/                  -> 200 HTML carrying the SPA mount
#      id="root" (the same marker the e2e smoke spec keys on, W01)
#   4. the shell's first module script   -> 200 (the fingerprinted bundle at
#      /binflow/assets/ actually resolves — catches the broken-embed /
#      asset-rot class a bare index-200 cannot)
#   5. GET /binflow/docs/                -> 200 (the embedded docs face)
#   6. GET /binflow/api/system/version   -> 200 (management plane answers;
#      the version STRING is informational here — drift in it is the deploy
#      log's business, the smoke only proves the plane is up)
#
# Base resolution mirrors ci/protocol-matrix.sh's transition probe: an
# explicit BINFLOW_BASE (or UAT_MATRIX_BASE) wins; else probe https://
# $UAT_DOMAIN and fall back to the plain-HTTP IP form until DNS/TLS are
# live (the 5c36c80e lesson).
#
# Usage: bash ci/ui-smoke.sh    (no arguments; env-driven like the matrix)
set -euo pipefail

BASE="${BINFLOW_BASE:-${UAT_MATRIX_BASE:-}}"
if [ -z "$BASE" ]; then
  DOMAIN="${UAT_DOMAIN:-uat.binflow.org}"
  if curl -fsS -m 8 "https://${DOMAIN}/healthz" >/dev/null 2>&1; then
    BASE="https://${DOMAIN}"
  else
    echo "WARN: https://${DOMAIN} unreachable (DNS/TLS not live) — smoke runs against the plain-HTTP transition base" >&2
    BASE="http://${UAT_HOST:-52.79.109.153}:8080"
  fi
fi
echo "ui-smoke base: ${BASE}"

fail=0
body="$(mktemp)"
trap 'rm -f "$body"' EXIT

http_code() { # url -> prints the HTTP status code (000 on connect failure)
  local c
  c="$(curl -sS -m 15 -o "$body" -w '%{http_code}' "$1" 2>/dev/null)" || c="000"
  printf '%s' "$c"
}

expect_200() { # name url
  local code
  code="$(http_code "$2")"
  if [ "$code" = "200" ]; then
    echo "OK   $1: 200"
  else
    echo "FAIL $1: HTTP $code ($2)"
    fail=1
  fi
}

# 1. healthz
expect_200 "healthz" "${BASE}/healthz"

# 2. console mount redirect (CE-01) — one request, headers + status together
hdrs="$(curl -sS -m 15 -o /dev/null -D - "${BASE}/binflow" 2>/dev/null)" || hdrs=""
code="$(printf '%s\n' "$hdrs" | head -1 | awk '{print $2}')"
loc="$(printf '%s\n' "$hdrs" | tr -d '\r' | awk 'tolower($1)=="location:"{print $2; exit}')"
if [ "$code" = "301" ] && printf '%s' "$loc" | grep -q '/binflow/ui/'; then
  echo "OK   console mount redirect: 301 -> ${loc}"
else
  echo "FAIL console mount redirect: HTTP ${code:-none} Location '${loc}' (${BASE}/binflow)"
  fail=1
fi

# 3. console shell serves with the SPA mount marker
code="$(http_code "${BASE}/binflow/ui/")"
if [ "$code" = "200" ] && grep -q 'id="root"' "$body"; then
  echo "OK   console shell: 200 + SPA mount (id=root)"
elif [ "$code" = "200" ]; then
  echo "FAIL console shell: 200 but no id=\"root\" mount — placeholder/rotten shell (${BASE}/binflow/ui/)"
  fail=1
else
  echo "FAIL console shell: HTTP $code (${BASE}/binflow/ui/)"
  fail=1
fi

# 4. the shell's first module script actually resolves — THE discriminator
#    between the real console and the committed placeholder shell (the
#    placeholder carries id="root" too, by design: "the e2e smoke spec
#    passes against either shell"); only the real build has a bundle.
#    (grep -m1, not | head -1: head closing the pipe early SIGPIPEs grep
#    under pipefail; || true guards the no-match exit on the placeholder.)
asset="$(grep -o -m1 '<script[^>]*src="[^"]*"' "$body" | sed 's/.*src="//;s/"$//' || true)"
if [ -z "$asset" ]; then
  echo "FAIL console bundle: no module script in the shell (broken build/embed)"
  fail=1
else
  expect_200 "console bundle (${asset##*/})" "${BASE}${asset}"
fi

# 5. embedded docs face
expect_200 "docs face" "${BASE}/binflow/docs/"

# 6. management plane version endpoint
expect_200 "version endpoint" "${BASE}/binflow/api/system/version"

if [ "$fail" -ne 0 ]; then
  echo "ui-smoke: FAILED (see FAIL lines above)"
  exit 1
fi
echo "ui-smoke: GREEN (6/6 checks)"
