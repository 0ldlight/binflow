#!/bin/bash
# L013-4 R-15 pre-authorized probe (pending-rulings v2.1 §2 R-15 / §4-4).
#   R-15c/d : GET /api/repositories?project= tolerance semantics -> four-quadrant verdict
#   R-15a   : ?propertiesXml attribution face (api face vs file face) + 404-no-props arm
# Usage: r15-probe.sh <a|b>   a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env: ARTI_AUTH / BF_AUTH = "user:password" (never argv, never in wire logs).
set -u
SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then
  BASE=http://localhost:8082/artifactory; API=$BASE/api; AUTHVAR=ARTI_AUTH; CTX=artifactory
else
  BASE=http://localhost:8083/binflow;   API=$BASE/api; AUTHVAR=BF_AUTH;   CTX=binflow
fi
AUTH=${!AUTHVAR:-}
[ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
CURL=(curl -sS --noproxy '*' -u "$AUTH")
OUT=/Users/lzw/dev-center/reports/compatibility/l0134-wire/$SIDE/r15
mkdir -p "$OUT"

# ---- R-15c/d: repositories?project= arms ----
arm() { # arm <id> <path-and-query>
  local id=$1 url="$API/repositories$2"
  "${CURL[@]}" -D "$OUT/$id.hdr" -o "$OUT/$id.body" -w "$id %{http_code} %{size_download}B\n" "$url"
}

arm c0-baseline           ""
arm c1-project-unknown    "?project=l0134-nonexistent"
arm c2-project-empty      "?project="
arm c3-unknown-type-combo "?project=l0134-nonexistent&type=local"
# family precedent control (documented same-row behavior, sanity anchor)
arm c4-badtype-ctrl       "?type=l0134-nosuchtype"

# quadrant evidence: does unknown-project arm equal baseline (ignore) or empty set (filter)?
base_n=$("${CURL[@]}" "$API/repositories" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')
unk_body=$(cat "$OUT/c1-project-unknown.body")
echo "baseline-count=$base_n unknown-project-body=$unk_body"

# ---- R-15a: propertiesXml attribution faces ----
# setup: difftest repo + artifact with one property (PUT ?properties=k=v), and a prop-less file
GEN=l0134-r15a-gen
cat > "$OUT/r15a-repo.json" <<EOF
{"rclass":"local","packageType":"generic","repoLayoutRef":"simple-default"}
EOF
"${CURL[@]}" -X PUT -H 'Content-Type: application/json' -T "$OUT/r15a-repo.json" \
  -o "$OUT/setup-repo.body" -w "setup repo %{http_code}\n" "$API/repositories/$GEN"
echo "l0134 wire fixture: difftest namespace $GEN" > "$OUT/fixture.txt"
"${CURL[@]}" -T "$OUT/fixture.txt" -o "$OUT/setup-f1.body" -w "setup f1 %{http_code}\n" "$BASE/$GEN/l0134/with-props.txt"
"${CURL[@]}" -T "$OUT/fixture.txt" -o "$OUT/setup-f2.body" -w "setup f2 %{http_code}\n" "$BASE/$GEN/l0134/no-props.txt"
# property attach lives on the /api/storage face (file-face PUT ?properties= is a
# plain re-upload with the query ignored -- observed 201 + zero props, ref 7.161)
"${CURL[@]}" -X PUT -o "$OUT/setup-props.body" -w "set props %{http_code}\n" \
  "$API/storage/$GEN/l0134/with-props.txt?properties=l0134k=l0134v"

pa() { # pa <id> <url>
  local id=$1 url=$2
  "${CURL[@]}" -D "$OUT/$id.hdr" -o "$OUT/$id.body" -w "$id %{http_code} %{size_download}B ct:%{content_type}\n" "$url"
}
pa p1-api-face-props     "$API/storage/$GEN/l0134/with-props.txt?propertiesXml"
pa p2-file-face-props    "$BASE/$GEN/l0134/with-props.txt?propertiesXml"
pa p3-api-face-noprops   "$API/storage/$GEN/l0134/no-props.txt?propertiesXml"
pa p4-file-face-noprops  "$BASE/$GEN/l0134/no-props.txt?propertiesXml"
pa p5-json-twin-control  "$API/storage/$GEN/l0134/with-props.txt?properties"

# cleanup: drop difftest repo (assets deleted at source)
"${CURL[@]}" -X DELETE -o "$OUT/cleanup-repo.body" -w "cleanup repo %{http_code}\n" "$API/repositories/$GEN"

echo "---- wire files in $OUT ----"; ls "$OUT"
