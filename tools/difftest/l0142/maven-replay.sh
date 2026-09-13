#!/bin/bash
# L014-2 maven matrix replay — the L013-4 18-arm wire matrix re-run against
# both sides after the unique-snapshot rewrite + registration-only sidecar
# landed (internal/adapter/maven). Same arms, fresh namespace l0142-mvn-*,
# listings captured WITH names (the L013 collector stripped them; the E2
# convergence count needs the item set).
# Usage: maven-replay.sh <a|b>   a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env: ARTI_AUTH / BF_AUTH. Judge: maven-judge.py reads both wire dirs.
set -u
SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then
  BASE=http://localhost:8082/artifactory; API=$BASE/api; AUTHVAR=ARTI_AUTH
else
  BASE=http://localhost:8083/binflow;   API=$BASE/api; AUTHVAR=BF_AUTH
fi
AUTH=${!AUTHVAR:-}
[ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
CURL=(curl -sS --noproxy '*' -u "$AUTH")
OUT=/Users/lzw/dev-center/reports/compatibility/l0142-wire/$SIDE/maven
rm -rf "$OUT"; mkdir -p "$OUT"
G=com/l0142; M=probe-app

mkrepo() {
  local key=$1 extra=$2 try
  printf '{"rclass":"local","packageType":"maven","repoLayoutRef":"maven-2-default"%s}' "$extra" > "$OUT/repo-$key.json"
  for try in 1 2 3; do
    "${CURL[@]}" -X PUT -H 'Content-Type: application/json' -T "$OUT/repo-$key.json" \
      -o "$OUT/repo-$key.out" -w "mkrepo $key %{http_code}\n" "$API/repositories/$key"
    if ! grep -q 'recurrent request failures' "$OUT/repo-$key.out" 2>/dev/null; then break; fi
    sleep 9
  done
  sleep 2
}
put() { # put <id> <file> <url> [extra curl args...]
  local id=$1 file=$2 url=$3; shift 3; local try
  for try in 1 2 3; do
    "${CURL[@]}" "$@" -T "$file" -D "$OUT/$id.hdr" -o "$OUT/$id.body" -w "$id %{http_code} " "$url"
    if ! grep -q 'recurrent request failures' "$OUT/$id.body" 2>/dev/null; then break; fi
    sleep 9
  done
  grep -i '^location' "$OUT/$id.hdr" | tr -d '\r' | sed 's/^/  /'
  sleep 2
}
get() {
  local id=$1 url=$2 try
  for try in 1 2 3; do
    "${CURL[@]}" -D "$OUT/$id.hdr" -o "$OUT/$id.body" -w "$id %{http_code} %{size_download}B\n" "$url"
    if ! grep -q 'recurrent request failures' "$OUT/$id.body" 2>/dev/null; then break; fi
    sleep 9
  done
  sleep 2
}
listnames() { # listnames <id> <repo> <dir> — storage listing, names extracted
  local id=$1 repo=$2 dir=$3
  "${CURL[@]}" -o "$OUT/$id.json" -w "$id %{http_code}\n" "$API/storage/$repo/$dir"
  python3 -c 'import json,sys;d=json.load(open("'"$OUT/$id.json"'"));print("\n".join(sorted(c["uri"] for c in d.get("children",[]))))' > "$OUT/$id.names" 2>/dev/null || true
  sleep 2
}
pom() { printf '<?xml version="1.0" encoding="UTF-8"?>\n<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><groupId>com.l0142</groupId><artifactId>%s</artifactId><version>%s</version></project>\n' "$M" "$1"; }
jarbyte() { printf 'PK\x03\x04l0142-%s-dummy-jar-bytes' "$1"; }

mkrepo l0142-mvn-d  ''
mkrepo l0142-mvn-u  ',"snapshotVersionBehavior":"unique"'
mkrepo l0142-mvn-cc ',"checksumPolicyType":"client-checksums"'
mkrepo l0142-mvn-sg ',"checksumPolicyType":"server-generated-checksums"'

# ---- A: metadata GET shape (l0142-mvn-d) ----
pom 1.0.0 > "$OUT/pom-100.xml"; pom 1.1.0 > "$OUT/pom-110.xml"; pom 2.0.0-SNAPSHOT > "$OUT/pom-200sn.xml"
put a1-put-pom-100 "$OUT/pom-100.xml" "$BASE/l0142-mvn-d/$G/$M/1.0.0/$M-1.0.0.pom"
sleep 6; get a1-get-groupmeta "$BASE/l0142-mvn-d/$G/$M/maven-metadata.xml"
put a2-put-pom-110 "$OUT/pom-110.xml" "$BASE/l0142-mvn-d/$G/$M/1.1.0/$M-1.1.0.pom"
sleep 6; get a2-get-groupmeta "$BASE/l0142-mvn-d/$G/$M/maven-metadata.xml"
put a3-put-pom-200sn "$OUT/pom-200sn.xml" "$BASE/l0142-mvn-d/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.pom"
sleep 6
get a3-get-groupmeta "$BASE/l0142-mvn-d/$G/$M/maven-metadata.xml"
get a3b-get-snapdir-meta "$BASE/l0142-mvn-d/$G/$M/2.0.0-SNAPSHOT/maven-metadata.xml"
get a4-get-meta-sha1 "$BASE/l0142-mvn-d/$G/$M/maven-metadata.xml.sha1"
listnames a6-list-snapdir l0142-mvn-d "$G/$M/2.0.0-SNAPSHOT"

# ---- B: snapshot arithmetic (l0142-mvn-u, unique) ----
jarbyte b1 > "$OUT/bytes-jar1.bin"; jarbyte b2 > "$OUT/bytes-jar2.bin"; jarbyte b3 > "$OUT/bytes-jar3.bin"
put b1-put-jar-snap-1 "$OUT/bytes-jar1.bin" "$BASE/l0142-mvn-u/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.jar"
put b2-put-pom-snap "$OUT/pom-200sn.xml" "$BASE/l0142-mvn-u/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.pom"
sleep 4
put b3-put-jar-snap-2 "$OUT/bytes-jar3.bin" "$BASE/l0142-mvn-u/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.jar"
sleep 4
get b4-get-snapdir-meta "$BASE/l0142-mvn-u/$G/$M/2.0.0-SNAPSHOT/maven-metadata.xml"
listnames b5-list-snapdir l0142-mvn-u "$G/$M/2.0.0-SNAPSHOT"
# B3: already-unique client name never rewrites
put b6-put-unique-name "$OUT/bytes-jar1.bin" "$BASE/l0142-mvn-u/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-20240819.101500-7.jar"
# sidecar of a -SNAPSHOT artifact (registration target follows the rewrite)
printf '%s\n' "$(shasum -a 1 < "$OUT/bytes-jar1.bin" | cut -d' ' -f1)" > "$OUT/b7.sha1"
put b7-put-snap-sidecar "$OUT/b7.sha1" "$BASE/l0142-mvn-u/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.jar.sha1"
listnames b8-list-after-sidecar l0142-mvn-u "$G/$M/2.0.0-SNAPSHOT"

# ---- C: merge semantics / parallel conflict (l0142-mvn-d / u) ----
cat > "$OUT/hand-meta.xml" <<EOF
<metadata>
  <groupId>com.l0142</groupId>
  <artifactId>probe-app</artifactId>
  <version>2.0.0-SNAPSHOT</version>
  <versioning>
    <latest>9.9.9-bogus</latest>
    <release>9.9.9-bogus</release>
    <versions><version>9.9.9-bogus</version></versions>
    <lastUpdated>19700101000000</lastUpdated>
  </versioning>
</metadata>
EOF
put c1-put-hand-meta "$OUT/hand-meta.xml" "$BASE/l0142-mvn-d/$G/$M/maven-metadata.xml"
sleep 2; get c1b-get-after-put "$BASE/l0142-mvn-d/$G/$M/maven-metadata.xml"
pom 3.0.0 > "$OUT/pom-300.xml"
put c1c-put-pom-300 "$OUT/pom-300.xml" "$BASE/l0142-mvn-d/$G/$M/3.0.0/$M-3.0.0.pom"
sleep 6; get c1d-get-after-deploy "$BASE/l0142-mvn-d/$G/$M/maven-metadata.xml"

pom 4.0.0 > "$OUT/pom-400.xml"; pom 4.1.0 > "$OUT/pom-410.xml"
put c2a-put-pom-400 "$OUT/pom-400.xml" "$BASE/l0142-mvn-d/$G/$M/4.0.0/$M-4.0.0.pom" &
put c2b-put-pom-410 "$OUT/pom-410.xml" "$BASE/l0142-mvn-d/$G/$M/4.1.0/$M-4.1.0.pom" &
wait; sleep 8
get c2c-get-groupmeta "$BASE/l0142-mvn-d/$G/$M/maven-metadata.xml"

jarbyte r1 > "$OUT/bytes-race1.bin"; sleep 0.3; jarbyte r2 > "$OUT/bytes-race2.bin"
put c3a-put-jar-race1 "$OUT/bytes-race1.bin" "$BASE/l0142-mvn-u/$G/$M/3.0.0-SNAPSHOT/$M-3.0.0-SNAPSHOT.jar" &
put c3b-put-jar-race2 "$OUT/bytes-race2.bin" "$BASE/l0142-mvn-u/$G/$M/3.0.0-SNAPSHOT/$M-3.0.0-SNAPSHOT.jar" &
wait; sleep 8
get c3c-get-snapdir-meta "$BASE/l0142-mvn-u/$G/$M/3.0.0-SNAPSHOT/maven-metadata.xml"
listnames c3d-list-snapdir l0142-mvn-u "$G/$M/3.0.0-SNAPSHOT"

# ---- D: checksum policy tri-state ----
for R in l0142-mvn-cc l0142-mvn-sg; do
  t=$(echo "$R" | sed 's/l0142-mvn-//')
  jarbyte "d$t" > "$OUT/bytes-$t.bin"
  put d1-$t-put-file "$OUT/bytes-$t.bin" "$BASE/$R/$G/$M/1.0.0/$M-1.0.0.jar"
  printf '%s\n' "$(shasum -a 1 < "$OUT/bytes-$t.bin" | cut -d' ' -f1)" > "$OUT/cs-$t-ok.sha1"
  printf '%040d\n' 0 | tr '0' 'f' > "$OUT/cs-$t-bad.sha1"
  put d2-$t-sha1-ok "$OUT/cs-$t-ok.sha1" "$BASE/$R/$G/$M/1.0.0/$M-1.0.0.jar.sha1"
  put d3-$t-sha1-bad "$OUT/cs-$t-bad.sha1" "$BASE/$R/$G/$M/1.0.0/$M-1.0.0.jar.sha1"
done
head -c 1200 /dev/zero | tr '\0' 'x' > "$OUT/cs-huge.sha1"
put d4-huge-sidecar "$OUT/cs-huge.sha1" "$BASE/l0142-mvn-d/$G/$M/1.0.0/$M-1.0.0.jar.sha1"
# sidecar materialization face (BUG 2): what the 1.0.0 dir holds after the
# client-sidecar PUTs above
listnames d5-list-cc-dir l0142-mvn-cc "$G/$M/1.0.0"

# ---- E: real mvn CLI leg (l0142-mvn-u) ----
W=/tmp/l0142/mvn-leg/$SIDE; rm -rf "$W"; mkdir -p "$W/src/main/java/com/l0142"
cat > "$W/pom.xml" <<EOF
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.l0142</groupId>
  <artifactId>probe-cli</artifactId>
  <version>1.0.0-SNAPSHOT</version>
  <packaging>jar</packaging>
  <distributionManagement>
    <snapshotRepository><id>r</id><url>$BASE/l0142-mvn-u</url></snapshotRepository>
  </distributionManagement>
</project>
EOF
echo 'package com.l0142; public class Probe { public static void main(String[] a){ System.out.println("l0142"); } }' \
  > "$W/src/main/java/com/l0142/Probe.java"
MVN_USER=${AUTH%%:*}; export L0142_MVN_PASS=${AUTH#*:}
cat > "$W/settings.xml" <<EOF
<settings><servers><server><id>r</id><username>$MVN_USER</username><password>\${env.L0142_MVN_PASS}</password></server></servers></settings>
EOF
( cd "$W" && mvn -s settings.xml -Dmaven.wagon.http.retryHandler.count=1 -Dmaven.resolver.transport=wagon deploy \
  > "$OUT/mvn-deploy-$SIDE.log" 2>&1; echo "mvn exit=$?" | tee "$OUT/mvn-exit.txt" )
unset L0142_MVN_PASS
sleep 4
listnames e2-snapdir-list l0142-mvn-u "com/l0142/probe-cli/1.0.0-SNAPSHOT"
get e2-snapdir-meta "$BASE/l0142-mvn-u/com/l0142/probe-cli/1.0.0-SNAPSHOT/maven-metadata.xml"
get e2b-group-meta "$BASE/l0142-mvn-u/com/l0142/probe-cli/maven-metadata.xml"

echo "---- wire files: $OUT ----"; ls "$OUT" | wc -l
