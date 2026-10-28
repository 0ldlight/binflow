#!/usr/bin/env bash
# T-631 ruling-material probe (round 2) — A-face (JFrog Artifactory reference 7.161.26)
# semantics of allowPrivateUpstream on the remote repository configuration surface.
#
# Round-1 lesson: A's PUT /api/repositories/<key> is create-only — an existing key fails
# with "Repository key already exists" BEFORE any field validation. So field handling must
# be probed with distinct keys (create arms) and via POST (the update verb).
#
# Arms:
#   P0  reference liveness + version
#   P1  PUT create WITHOUT allowPrivateUpstream (private RFC1918 upstream) → GET full config
#       (field-absence form + full returned field set)
#   P2  PUT create a DIFFERENT key WITH "allowPrivateUpstream":true → accepted / error?
#   P2b GET that config — was the field persisted (echoed)?
#   P2c POST update (partial patch) on repo P1 with {"allowPrivateUpstream":true} → status?
#   P2d GET again — echoed after update?
#   P3  PUT create a THIRD key with a deliberately bogus field (unknown-field policy control)
#   P3b GET — bogus echoed?
#   P4  private-upstream pull-through through A's docker v2 face (default posture)
#   P5  cleanup: DELETE all three diff- repos, verify zero residue (trap-guarded)
#
# Credentials arrive via env (gitignored .env.ref: ARTIFACTORY_REF_URL/_USER/_PASSWORD),
# never written to disk, never echoed. Writes only diff- prefixed test repos.
set -u
A_BASE="${ARTIFACTORY_REF_URL%/}"
R1="diff-privup-t631"     # baseline (no field)
R2="diff-privup-t631-b"   # create with allowPrivateUpstream:true
R3="diff-privup-t631-c"   # bogus-field control
UPSTREAM="http://192.168.120.36:32100"  # in-cluster relay registry (RFC1918 private)

hdr() { printf '\n===== %s =====\n' "$*"; }
req() { # req METHOD PATH [JSON-BODY]
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -u "$ARTIFACTORY_REF_USER:$ARTIFACTORY_REF_PASSWORD" -X "$method" \
      -H 'Content-Type: application/json' -d "$body" \
      -w '\n---HTTP %{http_code}---\n' "$A_BASE$path"
  else
    curl -sS -u "$ARTIFACTORY_REF_USER:$ARTIFACTORY_REF_PASSWORD" -X "$method" \
      -w '\n---HTTP %{http_code}---\n' "$A_BASE$path"
  fi
}
pl() { # payload for a repo key, with optional extra field (pass the raw JSON fragment)
  local key="$1" extra="${2:-}"
  local base='{"key":"'$key'","rclass":"remote","packageType":"docker","url":"'$UPSTREAM'","dockerApiVersion":"V2"'
  if [ -n "$extra" ]; then printf '%s,%s}' "$base" "$extra"; else printf '%s}' "$base"; fi
}
cleanup() {
  for r in "$R1" "$R2" "$R3"; do
    hdr "P5 cleanup DELETE $r"
    req DELETE "/artifactory/api/repositories/$r"
  done
  printf 'residue check (expect zero):\n'
  curl -sS -u "$ARTIFACTORY_REF_USER:$ARTIFACTORY_REF_PASSWORD" \
    "$A_BASE/artifactory/api/repositories" | grep -o '"key"  *:  *"diff-privup-t631[^"]*"' || printf '  zero residue\n'
}
trap cleanup EXIT

hdr "P0 reference version"
req GET "/artifactory/api/system/version" | grep -E '"version"|"revision"'

hdr "P1 PUT create $R1 without allowPrivateUpstream (upstream=$UPSTREAM RFC1918)"
req PUT "/artifactory/api/repositories/$R1" "$(pl "$R1")"

hdr "P1b GET config full text of $R1 (field-absence form; grep for private-ish keys)"
req GET "/artifactory/api/repositories/$R1" | grep -in "private\|loopback\|internal" || printf '  zero private/loopback/internal keys in full config echo\n'

hdr "P2 PUT create $R2 WITH allowPrivateUpstream:true — accepted / ignored / error?"
req PUT "/artifactory/api/repositories/$R2" "$(pl "$R2" '"allowPrivateUpstream":true')"

hdr "P2b GET config of $R2 — is allowPrivateUpstream echoed (persisted)?"
req GET "/artifactory/api/repositories/$R2" | grep -in "private" || printf '  zero *private* keys echoed\n'

hdr "P2c POST update (partial patch) $R1 with {allowPrivateUpstream:true}"
req POST "/artifactory/api/repositories/$R1" '{"allowPrivateUpstream":true}'

hdr "P2d GET config of $R1 after update — echoed?"
req GET "/artifactory/api/repositories/$R1" | grep -in "private" || printf '  zero *private* keys echoed after POST update\n'

hdr "P3 PUT create $R3 with bogus field t631DiffProbeField:true (unknown-field policy control)"
req PUT "/artifactory/api/repositories/$R3" "$(pl "$R3" '"t631DiffProbeField":true')"

hdr "P3b GET config of $R3 — bogus field echoed?"
req GET "/artifactory/api/repositories/$R3" | grep -in "t631DiffProbeField" || printf '  bogus field not echoed\n'

hdr "P4a v2 ping with credentials (docker v2 face, root path)"
req GET "/v2/"

hdr "P4b manifest t1 pull-through through remote repo $R1 (default posture, private upstream)"
curl -sS -u "$ARTIFACTORY_REF_USER:$ARTIFACTORY_REF_PASSWORD" \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json' \
  -w '\n---HTTP %{http_code}---\n' \
  "$A_BASE/v2/$R1/difftest-docker/smoke/manifests/t1"
