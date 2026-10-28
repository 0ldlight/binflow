#!/bin/bash
# relay-sync.sh — package the repo tip + tools and sync the Artifactory
# relay (generic-local/mirror) that the lzw CI Jobs fetch from. Replaces the
# /tmp/bf_relay.sh scratch script; runs from anywhere inside the repo.
#
# Fixes vs the scratch version:
#   - checksums go into ONE manifest mirror-checksums.txt (sha256sum -c
#     format). Artifactory treats PUT <name>.sha256 as a checksum-deploy
#     directive for <name>, which 404s ("Target file to set checksum on
#     doesn't exist") and is why the old helm.sha256 upload never landed.
#   - every PUT is 201-checked.
# Requires .env.ref (ARTIFACTORY_REF_URL/USER/PASSWORD) in the repo root.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
. ./.env.ref

SHA=$(git rev-parse --short HEAD)
echo "packaging source at $SHA"
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
git archive --format=tar.gz -o "$STAGE/binflow-src-$SHA.tar.gz" HEAD
cp "$STAGE/binflow-src-$SHA.tar.gz" "$STAGE/binflow-src-latest.tar.gz"
echo "$SHA" > "$STAGE/VERSION"

if [ ! -s /tmp/helm-linux-amd64 ]; then
  echo "fetching helm v3.16.4"
  curl -sSLo /tmp/helm.tgz https://get.helm.sh/helm-v3.16.4-linux-amd64.tar.gz
  tar -xzf /tmp/helm.tgz -C /tmp linux-amd64/helm
  mv /tmp/linux-amd64/helm /tmp/helm-linux-amd64 && rm -f /tmp/helm.tgz
fi
cp /tmp/helm-linux-amd64 "$STAGE/helm-linux-amd64"

# job templates ride the same sync so the relay can never drift from the repo
cp deploy/ci/k8s/buildah-job.yaml deploy/ci/k8s/helm-deploy-job.yaml "$STAGE/"

(cd "$STAGE" && sha256sum VERSION binflow-src-latest.tar.gz helm-linux-amd64 \
  buildah-job.yaml helm-deploy-job.yaml > mirror-checksums.txt)

BASE="$ARTIFACTORY_REF_URL/artifactory/generic-local/mirror"
for f in binflow-src-$SHA.tar.gz binflow-src-latest.tar.gz VERSION \
         helm-linux-amd64 buildah-job.yaml helm-deploy-job.yaml \
         mirror-checksums.txt; do
  code=$(curl -sS -m 600 -u "$ARTIFACTORY_REF_USER:$ARTIFACTORY_REF_PASSWORD" \
    -X PUT -H "Content-Type: application/octet-stream" --data-binary @"$STAGE/$f" \
    -o /dev/null -w '%{http_code}' "$BASE/$f")
  echo "$f -> $code"
  [ "$code" = "201" ]
done
echo "relay synced at $SHA"
