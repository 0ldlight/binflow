#!/usr/bin/env bash
# T-630 closure probe (Z-accounting batch, 2026-10-28) — BinFlow console root-face
# three arms after the bare-root homepage alignment (f3bf1b78) went live on the
# k8s test instance (ns lzw). Wave-1 case #0 console-root_path: FAIL -> PASS.
#
# Arms (ALL read-only GET; zero state change, zero breaker arming):
#   P0   liveness: GET /binflow/api/system/version (authed)        -> 200
#   P1   bare root (authed): GET /                                 -> 200 text/html shell
#   P2   UI direct (authed): GET /binflow/ui/                      -> 200; sha256 == P1 body (byte-identical)
#   P3   context root: GET /binflow/ and GET /binflow              -> 302 + ABSOLUTE Location .../binflow/ui/
#   P4   anonymous: GET / (no credentials)                         -> posture record (no assertion;
#                                                                   default-anonymous posture is ledgered
#                                                                   under rest/anonymous-read-default-policy)
#   P5   rejected credentials: GET / + bad Basic (round-unique
#        probe username, 1 attempt << breaker threshold 5)        -> 401 (auth gate precedes root branch)
#
# Credentials arrive via env (gitignored .env.lzw: BINFLOW_LZW_URL/_USER/_ADMIN_PASSWORD),
# never written to disk, never echoed. Output is status/header/sha256-prefixed only.
set -u
. "$(dirname "$0")/../../../../.env.lzw"
B_BASE="${BINFLOW_LZW_URL%/}"
BAD_USER="diff-probe-t630z-$(date +%s)"

hdr() { printf '\n===== %s =====\n' "$*"; }
sha() { shasum -a 256 | cut -c1-16; }

hdr "P0 liveness GET /binflow/api/system/version (authed)"
curl -sS -u "$BINFLOW_LZW_USER:$BINFLOW_LZW_ADMIN_PASSWORD" -w '\n---HTTP %{http_code}---\n' "$B_BASE/binflow/api/system/version"

hdr "P1 bare root GET / (authed): status + headers + body sha256"
curl -sS -u "$BINFLOW_LZW_USER:$BINFLOW_LZW_ADMIN_PASSWORD" -D /tmp/t630z-p1.h -o /tmp/t630z-p1.body -w '---HTTP %{http_code}---\n' "$B_BASE/"
grep -i -E '^(HTTP/|content-type|cache-control|content-length):|^---HTTP' /tmp/t630z-p1.h
printf 'body sha256[:16] = '; sha < /tmp/t630z-p1.body

hdr "P2 UI direct GET /binflow/ui/ (authed): status + body sha256 (compare P1)"
curl -sS -u "$BINFLOW_LZW_USER:$BINFLOW_LZW_ADMIN_PASSWORD" -D /tmp/t630z-p2.h -o /tmp/t630z-p2.body -w '---HTTP %{http_code}---\n' "$B_BASE/binflow/ui/"
grep -i -E '^(HTTP/|content-type|content-length):|^---HTTP' /tmp/t630z-p2.h
printf 'body sha256[:16] = '; sha < /tmp/t630z-p2.body
if cmp -s /tmp/t630z-p1.body /tmp/t630z-p2.body; then
  echo 'BYTE-IDENTICAL: bare-root shell == /binflow/ui/ shell'
else
  echo 'BYTE-DIFFER: bare-root shell != /binflow/ui/ shell'
fi

hdr "P3 context root GET /binflow/ and GET /binflow (authed): 302 + Location"
for p in /binflow/ /binflow; do
  curl -sS -u "$BINFLOW_LZW_USER:$BINFLOW_LZW_ADMIN_PASSWORD" -D - -o /dev/null -w '---HTTP %{http_code}---\n' "$B_BASE$p" | grep -i -E '^(HTTP/|location):|^---HTTP'
done

hdr "P4 anonymous GET / (no credentials): posture record"
curl -sS -D - -o /dev/null -w '---HTTP %{http_code}---\n' "$B_BASE/" | grep -i -E '^(HTTP/|location|www-authenticate):|^---HTTP'

hdr "P5 rejected credentials GET / (bad Basic, round-unique user): 401 gate ordering"
curl -sS -u "$BAD_USER:wrong-password" -D - -o /dev/null -w '---HTTP %{http_code}---\n' "$B_BASE/" | grep -i -E '^(HTTP/|www-authenticate):|^---HTTP'

rm -f /tmp/t630z-p1.h /tmp/t630z-p1.body /tmp/t630z-p2.h /tmp/t630z-p2.body
echo; echo 'probe done (read-only; no repo/breaker state touched)'
