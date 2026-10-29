#!/usr/bin/env bash
# L043-wave2 ruling-material probe — read-only verification of the seven
# divergence candidates from reports/compatibility/L043-wave2-report.md.
#
# Arms (ALL GET — zero writes, zero residue):
#   C   registry-level GET /v2/_catalog (wave2 candidate 3: A 200 empty body
#       zero CT vs B 200 JSON repositories)
#   T   remote repo unknown-image tags/list (candidate 4: A 404 NAME_UNKNOWN
#       vs B 200 {"name","tags":null})
#   S   manifest GET with schema1-only Accept (candidate 2: A online
#       conversion CT=prettyjws vs B schema2 as-is)
#   Br  tampered bearer on manifest GET (candidate 1: status SAME 401, shape
#       A classified message + Basic challenge vs B generic + Bearer challenge)
#   F   tags/list freshness: upstream live set vs A remote view vs B remote
#       view (candidate: A non-live cached view missing upstream nc-* tags)
#   N   tags/list with a manifest Accept header (unverified 406 arm: A 406?
#       vs B 200?)
#
# Credentials arrive via env (gitignored .env.ref / .env.lzw, line-anchored
# grep exactly like the wave2 wrappers), never written to disk, never echoed.
# No user creation, no config change, no data mutation on either side.
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
E_REF=/Users/lzw/vscodeProjects/binflow/.env.ref
E_LZW=/Users/lzw/vscodeProjects/binflow/.env.lzw

strip_q() { sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
A_USER="$(grep -E '^ARTIFACTORY_REF_USER=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
A_PASSWORD="$(grep -E '^ARTIFACTORY_REF_PASSWORD=' "$E_REF" | head -1 | cut -d= -f2- | strip_q)"
B_USER="$(grep -E '^BINFLOW_LZW_USER=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"
B_PASSWORD="$(grep -E '^BINFLOW_LZW_ADMIN_PASSWORD=' "$E_LZW" | head -1 | cut -d= -f2- | strip_q)"

A_BASE="${ARTIFACTORY_REF_URL:-http://192.168.120.36:32082}"
B_BASE="${BINFLOW_LZW_URL:-http://192.168.120.36:32083}"
UPSTREAM="${DIFFTEST_DOCKER_UPSTREAM:-http://192.168.120.36:32100}"

REPO="diff-docker-remote"
IMG="difftest-docker/smoke"
TAG="t1"
NONCE="$(date +%s)"
UNKNOWN_IMG="difftest-nonexistent-$NONCE"

hdr() { printf '\n===== %s =====\n' "$*"; }

ag() { curl -sS -u "$A_USER:$A_PASSWORD" "$@"; }   # A-side GET (Basic)
bg() { curl -sS -u "$B_USER:$B_PASSWORD" "$@"; }   # B-side GET (Basic)

status_of() { awk 'NR==1{print $1}'; }             # first token of "---HTTP nnn---" marker line

req_meta() { # req_meta CURL_OUTPUT -> "status | CT | body-first-160B"
  printf '%s' "$1" | LC_ALL=C tr -d '\000'
}

hdr "A0/B0 liveness (docker v2 root, unauthenticated challenge expected)"
printf 'A /v2/ -> '; curl -sS -o /dev/null -w '%{http_code}\n' "$A_BASE/v2/"
printf 'B /v2/ -> '; curl -sS -o /dev/null -w '%{http_code}\n' "$B_BASE/v2/"

hdr "C: registry-level GET /v2/_catalog"
for side in a b; do
  if [ "$side" = a ]; then
    out="$(ag -w '\n---HTTP %{http_code}--- CT:%{content_type}---' "$A_BASE/v2/_catalog")"
  else
    out="$(bg -w '\n---HTTP %{http_code}--- CT:%{content_type}---' "$B_BASE/v2/_catalog")"
  fi
  printf '%s: %s\n' "$side" "$(printf '%s' "$out" | tail -1)"
  printf '%s body(300B): %s\n' "$side" "$(printf '%s' "$out" | head -c 300)"
done

hdr "T: remote repo unknown-image tags/list (/v2/$REPO/$UNKNOWN_IMG/tags/list)"
for side in a b; do
  if [ "$side" = a ]; then
    out="$(ag -w '\n---HTTP %{http_code}--- CT:%{content_type}---' "$A_BASE/v2/$REPO/$UNKNOWN_IMG/tags/list")"
  else
    out="$(bg -w '\n---HTTP %{http_code}--- CT:%{content_type}---' "$B_BASE/v2/$REPO/$UNKNOWN_IMG/tags/list")"
  fi
  printf '%s: %s\n' "$side" "$(printf '%s' "$out" | tail -1)"
  printf '%s body(240B): %s\n' "$side" "$(printf '%s' "$out" | head -c 240)"
done

hdr "S: manifest GET schema1-only Accept (/v2/$REPO/$IMG/manifests/$TAG)"
S1_ACCEPT='application/vnd.docker.distribution.manifest.v1+prettyjws'
for side in a b; do
  if [ "$side" = a ]; then
    out="$(ag -H "Accept: $S1_ACCEPT" -w '\n---HTTP %{http_code}--- CT:%{content_type}---' "$A_BASE/v2/$REPO/$IMG/manifests/$TAG")"
  else
    out="$(bg -H "Accept: $S1_ACCEPT" -w '\n---HTTP %{http_code}--- CT:%{content_type}---' "$B_BASE/v2/$REPO/$IMG/manifests/$TAG")"
  fi
  printf '%s: %s\n' "$side" "$(printf '%s' "$out" | tail -1)"
  printf '%s body head(120B): %s\n' "$side" "$(printf '%s' "$out" | head -c 120)"
done

hdr "Br: tampered bearer arms on the same manifest face (t-garbage / t3-alg-none)"
GARBAGE="Bearer w2probe-garbage-$NONCE"
ALGNONE="Bearer eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJ3MnByb2JlIn0.$NONCE"
for arm in GARBAGE ALGNONE; do
  val="$(eval echo "\$$arm")"
  for side in a b; do
    if [ "$side" = a ]; then
      out="$(ag -H "Authorization: $val" -w '\n---HTTP %{http_code}--- WWW:%{header_json}---' "$A_BASE/v2/$REPO/$IMG/manifests/$TAG")"
    else
      out="$(bg -H "Authorization: $val" -w '\n---HTTP %{http_code}--- WWW:%{header_json}---' "$B_BASE/v2/$REPO/$IMG/manifests/$TAG")"
    fi
    printf '%s/%s status+shape: %s\n' "$arm" "$side" "$(printf '%s' "$out" | tail -1 | head -c 60)"
    printf '%s/%s body(240B): %s\n' "$arm" "$side" "$(printf '%s' "$out" | head -c 240)"
    printf '%s/%s WWW-Authenticate: %s\n' "$arm" "$side" \
      "$(printf '%s' "$out" | grep -o '"www-authenticate":\[[^]]*\]' | head -c 200)"
  done
done

hdr "F: tags freshness — upstream live vs A remote view vs B remote view"
printf 'upstream tags: %s\n' \
  "$(curl -sS "$UPSTREAM/v2/$IMG/tags/list" | head -c 300)"
printf 'A remote tags: %s\n' \
  "$(ag -w ' [HTTP %{http_code}]' "$A_BASE/v2/$REPO/$IMG/tags/list" | head -c 340)"
printf 'B remote tags: %s\n' \
  "$(bg -w ' [HTTP %{http_code}]' "$B_BASE/v2/$REPO/$IMG/tags/list" | head -c 340)"

hdr "N: tags/list WITH a manifest Accept header (unverified 406 arm)"
for side in a b; do
  if [ "$side" = a ]; then
    out="$(ag -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' -w '\n---HTTP %{http_code}--- CT:%{content_type}---' "$A_BASE/v2/$REPO/$IMG/tags/list")"
  else
    out="$(bg -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' -w '\n---HTTP %{http_code}--- CT:%{content_type}---' "$B_BASE/v2/$REPO/$IMG/tags/list")"
  fi
  printf '%s: %s\n' "$side" "$(printf '%s' "$out" | tail -1)"
  printf '%s body(160B): %s\n' "$side" "$(printf '%s' "$out" | head -c 160)"
done

hdr "probe done (read-only; no residue possible)"
