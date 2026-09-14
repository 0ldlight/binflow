#!/usr/bin/env bash
# L021-1 helmoci expansion — classic chart-repo differential probe (dual mode).
# A = Artifactory ref 7.161.20 :8082 ; B = BinFlow UAT :8083.
# Fixture: /tmp/l021ws (l021chart 0.1.0 signed + 0.2.0 + 0.10.0).
# Serial discipline; arm budget <=12 (setup/teardown not counted).
# Wire: reports/compatibility/l021-wire/{a,b}/helm/*.hdr|body + client/*.log|.wire
set -uo pipefail

WS=/tmp/l021ws
WIRE=/Users/lzw/dev-center/reports/compatibility/l021-wire
REPO=l021-helm
CHART=l021chart
REF=http://localhost:8082
UAT=http://localhost:8083
REF_USER="${REF_USER:-admin}"
REF_PASS="${REF_PASS:?set REF_PASS (Artifactory ref admin password)}"
UAT_PASS="$(grep '^BINFLOW_ADMIN_PASSWORD=' /Users/lzw/dev-center/deploy/compose/.env.uat | cut -d= -f2-)"
[ -n "$UAT_PASS" ] || { echo "FATAL: no UAT password"; exit 2; }

mkdir -p "$WIRE/a/helm" "$WIRE/b/helm" "$WIRE/a/client" "$WIRE/b/client" "$WS/pull"

log(){ printf '\n=== %s ===\n' "$*" | tee -a "$WIRE/arms.txt"; }

# cap <end> <name> <curl args...>  — capture response hdr+body, print status
cap(){
  local end=$1 name=$2; shift 2
  curl -sS -D "$WIRE/$end/helm/$name.hdr" -o "$WIRE/$end/helm/$name.body" \
    -w "%{http_code}" "$@" 2>"$WIRE/$end/helm/$name.err"
}

# wait_entry <end> <dl-face-base> <user> <pass> <chart-version> — poll index until entry visible (A async indexer)
wait_entry(){
  local end=$1 base=$2 user=$3 pass=$4 want=$5 i
  for i in $(seq 1 10); do
    if cap "$end" "poll-$want" -m 5 -u "$user:$pass" "$base/index.yaml" | grep -q 200 \
       && grep -q "version: $want" "$WIRE/$end/helm/poll-$want.body" 2>/dev/null; then
      echo "$end entry $want visible after ${i}x1s"; return 0
    fi
    sleep 1
  done
  echo "$end entry $want NOT visible after 10s"; return 1
}

A_AUTH=(-u "$REF_USER:$REF_PASS")
B_AUTH=(-u "admin:$UAT_PASS")

# ── setup: create HELM local repo on both ends ─────────────────────────────
log "setup: create $REPO (helm local) on A and B"
sa=$(cap a setup-create-repo -X PUT -H 'Content-Type: application/json' \
     ${A_AUTH[@]} -d '{"rclass":"local","packageType":"helm"}' "$REF/artifactory/api/repositories/$REPO")
sb=$(cap b setup-create-repo -X PUT -H 'Content-Type: application/json' \
     ${B_AUTH[@]} -d '{"rclass":"local","packageType":"helm"}' "$UAT/binflow/api/repositories/$REPO")
echo "create repo: A=$sa B=$sb (expect 200/201 both)"
[ "$sa" = 200 ] || [ "$sa" = 201 ] || echo "WARN: A create=$sa: $(cat $WIRE/a/helm/setup-create-repo.body)"
[ "$sb" = 200 ] || [ "$sb" = 201 ] || echo "WARN: B create=$sb: $(cat $WIRE/b/helm/setup-create-repo.body)"

# ── a01 upload chart 0.1.0 (signed fixture tgz) → status face ──────────────
log "a01 upload $CHART-0.1.0.tgz"
ua=$(cap a a01-put-tgz -X PUT ${A_AUTH[@]} --data-binary "@$WS/$CHART-0.1.0.tgz" \
     -H 'Content-Type: application/x-gzip' "$REF/artifactory/$REPO/$CHART-0.1.0.tgz")
ub=$(cap b a01-put-tgz -X PUT ${B_AUTH[@]} --data-binary "@$WS/$CHART-0.1.0.tgz" \
     -H 'Content-Type: application/x-gzip' "$UAT/binflow/$REPO/$CHART-0.1.0.tgz")
echo "PUT tgz: A=$ua B=$ub (expect 201 both)"

# ── a02 index.yaml after upload (generation plane: form/digest/urls/created) ─
log "a02 index.yaml after first upload (settle-aware)"
wait_entry a "$REF/artifactory/api/helm/$REPO" "$REF_USER" "$REF_PASS" 0.1.0
wait_entry b "$UAT/binflow/$REPO" admin "$UAT_PASS" 0.1.0
cap a a02-index -m 5 ${A_AUTH[@]} "$REF/artifactory/api/helm/$REPO/index.yaml" >/dev/null
cap b a02-index -m 5 ${B_AUTH[@]} "$UAT/binflow/$REPO/index.yaml" >/dev/null
# content-face twin for B + alias face probe
cap b a02-index-alias -m 5 ${B_AUTH[@]} "$UAT/binflow/api/helm/$REPO/index.yaml" >/dev/null
echo "index bodies captured: a02-index{,-alias}"

# ── a03 version sorting (upload 0.2.0 + 0.10.0 → SemVer desc expected) ─────
log "a03 upload 0.2.0 + 0.10.0, index ordering"
for v in 0.2.0 0.10.0; do
  ca=$(cap a "a03-put-$v" -X PUT ${A_AUTH[@]} --data-binary "@$WS/$CHART-$v.tgz" \
       -H 'Content-Type: application/x-gzip' "$REF/artifactory/$REPO/$CHART-$v.tgz")
  cb=$(cap b "a03-put-$v" -X PUT ${B_AUTH[@]} --data-binary "@$WS/$CHART-$v.tgz" \
       -H 'Content-Type: application/x-gzip' "$UAT/binflow/$REPO/$CHART-$v.tgz")
  echo "PUT $v: A=$ca B=$cb"
done
wait_entry a "$REF/artifactory/api/helm/$REPO" "$REF_USER" "$REF_PASS" 0.10.0
wait_entry b "$UAT/binflow/$REPO" admin "$UAT_PASS" 0.10.0
cap a a03-index -m 5 ${A_AUTH[@]} "$REF/artifactory/api/helm/$REPO/index.yaml" >/dev/null
cap b a03-index -m 5 ${B_AUTH[@]} "$UAT/binflow/$REPO/index.yaml" >/dev/null
echo "ordering (version lines, first=expected newest):"
grep -n "version:" "$WIRE/a/helm/a03-index.body" | head -5
echo "---"
grep -n "version:" "$WIRE/b/helm/a03-index.body" | head -5

# ── a04 provenance file (PUT .prov → GET bytes parity; not in index) ───────
log "a04 provenance PUT/GET"
pa=$(cap a a04-put-prov -X PUT ${A_AUTH[@]} --data-binary "@$WS/$CHART-0.1.0.tgz.prov" \
     -H 'Content-Type: application/pgp-signature' "$REF/artifactory/$REPO/$CHART-0.1.0.tgz.prov")
pb=$(cap b a04-put-prov -X PUT ${B_AUTH[@]} --data-binary "@$WS/$CHART-0.1.0.tgz.prov" \
     -H 'Content-Type: application/pgp-signature' "$UAT/binflow/$REPO/$CHART-0.1.0.tgz.prov")
echo "PUT prov: A=$pa B=$pb"
cap a a04-get-prov -m 10 ${A_AUTH[@]} "$REF/artifactory/api/helm/$REPO/$CHART-0.1.0.tgz.prov" >/dev/null
cap b a04-get-prov -m 10 ${B_AUTH[@]} "$UAT/binflow/$REPO/$CHART-0.1.0.tgz.prov" >/dev/null
shasum -a 256 "$WS/$CHART-0.1.0.tgz.prov" "$WIRE/a/helm/a04-get-prov.body" "$WIRE/b/helm/a04-get-prov.body"

# ── a05 chart tgz download (bytes parity + checksum headers) ───────────────
log "a05 tgz download via helm face"
cap a a05-get-tgz -m 10 ${A_AUTH[@]} "$REF/artifactory/api/helm/$REPO/$CHART-0.1.0.tgz" >/dev/null
cap b a05-get-tgz -m 10 ${B_AUTH[@]} "$UAT/binflow/$REPO/$CHART-0.1.0.tgz" >/dev/null
cap b a05-get-tgz-alias -m 10 ${B_AUTH[@]} "$UAT/binflow/api/helm/$REPO/$CHART-0.1.0.tgz" >/dev/null
shasum -a 256 "$WS/$CHART-0.1.0.tgz" "$WIRE/a/helm/a05-get-tgz.body" "$WIRE/b/helm/a05-get-tgz.body" "$WIRE/b/helm/a05-get-tgz-alias.body"
grep -iE "^(X-Checksum|Content-Type|Content-Length)" "$WIRE/a/helm/a05-get-tgz.hdr"
echo "---"
grep -iE "^(X-Checksum|Content-Type|Content-Length)" "$WIRE/b/helm/a05-get-tgz.hdr"

# ── a06 HEAD index.yaml + index content-type ───────────────────────────────
log "a06 HEAD index + CT"
cap a a06-head-index -I -m 5 ${A_AUTH[@]} "$REF/artifactory/api/helm/$REPO/index.yaml" >/dev/null
cap b a06-head-index -I -m 5 ${B_AUTH[@]} "$UAT/binflow/$REPO/index.yaml" >/dev/null
head -1 "$WIRE/a/helm/a06-head-index.hdr"; grep -i "^content-type" "$WIRE/a/helm/a06-head-index.hdr"
echo "---"
head -1 "$WIRE/b/helm/a06-head-index.hdr"; grep -i "^content-type" "$WIRE/b/helm/a06-head-index.hdr"

# ── a07-a09 helm CLI legs (repo add/update/show/pull/verify via wireproxy) ─
log "a07-a09 helm CLI legs (wireproxy: 19021->8082, 19022->8083)"
UPSTREAM=localhost:8082 python3 /Users/lzw/dev-center/tools/difftest/l0211/wireproxy.py 19021 "$WIRE/a/client" cli-a &
PA=$!
UPSTREAM=localhost:8083 python3 /Users/lzw/dev-center/tools/difftest/l0211/wireproxy.py 19022 "$WIRE/b/client" cli-b &
PB=$!
sleep 1
HELM_A=http://127.0.0.1:19021/artifactory/api/helm/$REPO
HELM_B=http://127.0.0.1:19022/binflow/$REPO

# a07 repo add + update (A)
helm repo remove l021a >/dev/null 2>&1
helm repo add l021a "$HELM_A" --username "$REF_USER" --password "$REF_PASS" \
  >"$WIRE/a/client/a07-repo-add.log" 2>&1; echo "A repo add exit=$?"
UPSTREAM=localhost:8082 helm repo update l021a >"$WIRE/a/client/a07-repo-update.log" 2>&1; echo "A repo update exit=$?"
# a07 repo add + update (B)
helm repo remove l021b >/dev/null 2>&1
helm repo add l021b "$HELM_B" --username admin --password "$UAT_PASS" \
  >"$WIRE/b/client/a07-repo-add.log" 2>&1; echo "B repo add exit=$?"
UPSTREAM=localhost:8083 helm repo update l021b >"$WIRE/b/client/a07-repo-update.log" 2>&1; echo "B repo update exit=$?"

# a08 show + pull (A)
helm show chart l021a/$CHART --version 0.1.0 >"$WIRE/a/client/a08-show.log" 2>&1; echo "A show exit=$?"
rm -f "$WS/pull/a-"*; helm pull l021a/$CHART --version 0.1.0 --destination "$WS/pull/a" \
  >"$WIRE/a/client/a08-pull.log" 2>&1; echo "A pull exit=$?"
shasum -a 256 "$WS/pull/a/"*.tgz 2>/dev/null
# a08 show + pull (B)
helm show chart l021b/$CHART --version 0.1.0 >"$WIRE/b/client/a08-show.log" 2>&1; echo "B show exit=$?"
rm -f "$WS/pull/b-"*; helm pull l021b/$CHART --version 0.1.0 --destination "$WS/pull/b" \
  >"$WIRE/b/client/a08-pull.log" 2>&1; echo "B pull exit=$?"
shasum -a 256 "$WS/pull/b/"*.tgz 2>/dev/null

# a09 pull --verify (provenance chain, keyring = L021 DiffTest pubring)
rm -f "$WS/pull/av-"* "$WS/pull/bv-"*
helm pull l021a/$CHART --version 0.1.0 --verify --keyring "$WS/pubring.gpg" \
  --destination "$WS/pull/av" >"$WIRE/a/client/a09-pull-verify.log" 2>&1; echo "A pull --verify exit=$?"
helm pull l021b/$CHART --version 0.1.0 --verify --keyring "$WS/pubring.gpg" \
  --destination "$WS/pull/bv" >"$WIRE/b/client/a09-pull-verify.log" 2>&1; echo "B pull --verify exit=$?"
tail -2 "$WIRE/a/client/a09-pull-verify.log"; echo "---"; tail -2 "$WIRE/b/client/a09-pull-verify.log"
kill $PA $PB 2>/dev/null; wait $PA $PB 2>/dev/null

# ── a10 auth gate (anonymous + bad creds on index face) ────────────────────
log "a10 auth gate"
aa=$(cap a a10-anon-index -m 5 "$REF/artifactory/api/helm/$REPO/index.yaml")
ba=$(cap b a10-anon-index -m 5 "$UAT/binflow/$REPO/index.yaml")
echo "anon GET index: A=$aa B=$ba"
ab=$(cap a a10-badcred-index -m 5 -u "admin:wrongpass" "$REF/artifactory/api/helm/$REPO/index.yaml")
bb=$(cap b a10-badcred-index -m 5 -u "admin:wrongpass" "$UAT/binflow/$REPO/index.yaml")
echo "badcred GET index: A=$ab B=$bb"
head -1 "$WIRE/a/helm/a10-anon-index.hdr"; grep -i "content-type" "$WIRE/a/helm/a10-anon-index.hdr" | head -1
echo "---"
head -1 "$WIRE/b/helm/a10-anon-index.hdr"; grep -i "content-type" "$WIRE/b/helm/a10-anon-index.hdr" | head -1

# ── a11 reindex (admin plane, index preserved) ─────────────────────────────
log "a11 reindex"
ra=$(cap a a11-reindex -X POST -m 20 ${A_AUTH[@]} "$REF/artifactory/api/helm/$REPO/reindex")
rb=$(cap b a11-reindex -X POST -m 20 ${B_AUTH[@]} "$UAT/binflow/api/helm/$REPO/reindex")
echo "reindex: A=$ra B=rb"
sleep 6   # A async full-repo reindex; settle before reading index
cap a a11-index-after -m 5 ${A_AUTH[@]} "$REF/artifactory/api/helm/$REPO/index.yaml" >/dev/null
cap b a11-index-after -m 5 ${B_AUTH[@]} "$UAT/binflow/$REPO/index.yaml" >/dev/null
grep -c "version:" "$WIRE/a/helm/a11-index-after.body" | xargs echo "A entries after reindex:"
grep -c "version:" "$WIRE/b/helm/a11-index-after.body" | xargs echo "B entries after reindex:"

# ── a12 DELETE tgz → index entry removal (async settle on A) ───────────────
log "a12 delete 0.10.0 → index entry removal"
da=$(cap a a12-del-tgz -X DELETE -m 10 ${A_AUTH[@]} "$REF/artifactory/$REPO/$CHART-0.10.0.tgz")
db=$(cap b a12-del-tgz -X DELETE -m 10 ${B_AUTH[@]} "$UAT/binflow/$REPO/$CHART-0.10.0.tgz")
echo "DELETE tgz: A=$da B=$db"
for i in $(seq 1 12); do
  cap a a12-index-after -m 5 ${A_AUTH[@]} "$REF/artifactory/api/helm/$REPO/index.yaml" >/dev/null
  grep -q "version: 0.10.0" "$WIRE/a/helm/a12-index-after.body" || { echo "A removed 0.10.0 after ${i}x2s"; break; }
  sleep 2
done
cap b a12-index-after -m 5 ${B_AUTH[@]} "$UAT/binflow/$REPO/index.yaml" >/dev/null
grep -q "version: 0.10.0" "$WIRE/b/helm/a12-index-after.body" && echo "B STILL has 0.10.0" || echo "B removed 0.10.0"

log "probe run complete"
