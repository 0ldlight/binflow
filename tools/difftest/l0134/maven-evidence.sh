#!/bin/bash
# L013-4 maven metadata computation forensics — dual-system wire matrix.
# Spec anchor: docs/reverse/maven-npm-pypi.md §1 (maven-metadata server calc,
# snapshot arithmetic, checksum policy) + [MVN-MD].
# Usage: maven-evidence.sh <a|b>   a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env: ARTI_AUTH / BF_AUTH. Serial arms; polls spaced 2s to stay under the
# reference's recurrent-failure blocker (no rapid 404 streams).
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
OUT=/Users/lzw/dev-center/reports/compatibility/l0134-wire/$SIDE/maven
mkdir -p "$OUT"
G=com/l0134; M=probe-app   # group path / module

mkrepo() { # mkrepo <key> <extra-json>
  local key=$1 extra=$2 try
  printf '{"rclass":"local","packageType":"maven","repoLayoutRef":"maven-2-default"%s}' "$extra" > "$OUT/repo-$key.json"
  for try in 1 2 3; do
    "${CURL[@]}" -X PUT -H 'Content-Type: application/json' -T "$OUT/repo-$key.json" \
      -o "$OUT/repo-$key.out" -w "mkrepo $key %{http_code}\n" "$API/repositories/$key"
    if ! grep -q 'recurrent request failures' "$OUT/repo-$key.out" 2>/dev/null; then break; fi
    printf 'mkrepo %s blocked, retry %s\n' "$key" "$try"; sleep 9
  done
  sleep 2
}
put() { # put <file> <url> -> records <id>.{hdr,body}
  local id=$1 file=$2 url=$3 try
  for try in 1 2 3; do
    "${CURL[@]}" -T "$file" -D "$OUT/$id.hdr" -o "$OUT/$id.body" -w "$id %{http_code} " "$url"
    if ! grep -q 'recurrent request failures' "$OUT/$id.body" 2>/dev/null; then break; fi
    printf '(blocked, retry %s) ' "$try"; sleep 9
  done
  grep -i '^location:' "$OUT/$id.hdr" | tr -d '\r' | sed 's/^/  /'
  sleep 2
}
get() { # get <id> <url>
  local id=$1 url=$2 try
  for try in 1 2 3; do
    "${CURL[@]}" -D "$OUT/$id.hdr" -o "$OUT/$id.body" -w "$id %{http_code} %{size_download}B\n" "$url"
    if ! grep -q 'recurrent request failures' "$OUT/$id.body" 2>/dev/null; then break; fi
    printf '(blocked, retry %s)\n' "$try"; sleep 9
  done
  sleep 2
}
pom() { # pom <version> -> prints pom content (minimal valid pom)
  local v=$1
  printf '<?xml version="1.0" encoding="UTF-8"?>\n<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><groupId>com.l0134</groupId><artifactId>%s</artifactId><version>%s</version></project>\n' "$M" "$v"
}
jarbyte() { printf 'PK\x03\x04l0134-%s-dummy-jar-bytes' "$(date +%s%N | tail -c 7)"; }

wait_meta() { # wait_meta <repo> <dir> <max-tries> — poll folder listing (200) until maven-metadata.xml child appears
  local repo=$1 dir=$2 tries=${3:-8} i listing
  for ((i=1; i<=tries; i++)); do
    listing=$("${CURL[@]}" "$API/storage/$repo/$dir" 2>/dev/null)
    if printf '%s' "$listing" | grep -q '"uri" : "/maven-metadata.xml"\|"uri":"/maven-metadata.xml"\|maven-metadata.xml'; then return 0; fi
    sleep 5
  done
  return 1
}

# ---------------- repos ----------------
mkrepo l0134-mvn-d  ''                                                       # defaults
mkrepo l0134-mvn-u  ',"snapshotVersionBehavior":"unique"'
mkrepo l0134-mvn-cc ',"checksumPolicyType":"client-checksums"'
mkrepo l0134-mvn-sg ',"checksumPolicyType":"server-generated-checksums"'

# ---------------- A. metadata GET shape (l0134-mvn-d) ----------------
pom 1.0.0 > "$OUT/pom-100.xml"; pom 1.1.0 > "$OUT/pom-110.xml"; pom 2.0.0-SNAPSHOT > "$OUT/pom-200sn.xml"
put a1-put-pom-100 "$OUT/pom-100.xml" "$BASE/l0134-mvn-d/$G/$M/1.0.0/$M-1.0.0.pom"
wait_meta l0134-mvn-d "$G/$M" && get a1-get-groupmeta "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml"
put a2-put-pom-110 "$OUT/pom-110.xml" "$BASE/l0134-mvn-d/$G/$M/1.1.0/$M-1.1.0.pom"
sleep 4; get a2-get-groupmeta "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml"
put a3-put-pom-200sn "$OUT/pom-200sn.xml" "$BASE/l0134-mvn-d/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.pom"
sleep 4
get a3-get-groupmeta "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml"
get a3b-get-snapdir-meta "$BASE/l0134-mvn-d/$G/$M/2.0.0-SNAPSHOT/maven-metadata.xml"
get a4-get-meta-sha1 "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml.sha1"
get a4b-get-meta-md5  "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml.md5"
# cross-check: sha1 served == sha1 of served metadata body
if command -v shasum >/dev/null; then
  { echo "served-sha1: $(cat "$OUT/a4-get-meta-sha1.body" | tr -d ' \r\n')"; echo "actual-sha1: $(shasum -a 1 < "$OUT/a3-get-groupmeta.body" | cut -d' ' -f1)"; } > "$OUT/a4-sha1-check.txt"
  cat "$OUT/a4-sha1-check.txt"
fi

# ---------------- B. snapshot arithmetic (l0134-mvn-u, unique) ----------------
jarbyte > "$OUT/bytes-jar.bin"
put b1-put-jar-snap-1 "$OUT/bytes-jar.bin" "$BASE/l0134-mvn-u/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.jar"
put b2-put-pom-snap   "$OUT/pom-200sn.xml" "$BASE/l0134-mvn-u/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.pom"
sleep 3
put b3-put-jar-snap-2 "$OUT/bytes-jar.bin" "$BASE/l0134-mvn-u/$G/$M/2.0.0-SNAPSHOT/$M-2.0.0-SNAPSHOT.jar"
sleep 3
get b4-get-snapdir-meta "$BASE/l0134-mvn-u/$G/$M/2.0.0-SNAPSHOT/maven-metadata.xml"
get b5-list-snapdir     "$API/storage/l0134-mvn-u/$G/$M/2.0.0-SNAPSHOT"

# ---------------- C. PUT merge semantics / parallel conflict ----------------
# c1: client-authored group metadata with bogus version -> accepted? overwritten by recompute?
cat > "$OUT/hand-meta.xml" <<EOF
<metadata>
  <groupId>com.l0134</groupId>
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
put c1-put-hand-meta "$OUT/hand-meta.xml" "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml"
sleep 1; get c1b-get-after-put "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml"
pom 3.0.0 > "$OUT/pom-300.xml"
put c1c-put-pom-300 "$OUT/pom-300.xml" "$BASE/l0134-mvn-d/$G/$M/3.0.0/$M-3.0.0.pom"
sleep 5
get c1d-get-after-deploy "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml"

# c2: parallel deploy, different versions -> merge or lose one?
pom 4.0.0 > "$OUT/pom-400.xml"; pom 4.1.0 > "$OUT/pom-410.xml"
put c2a-put-pom-400 "$OUT/pom-400.xml" "$BASE/l0134-mvn-d/$G/$M/4.0.0/$M-4.0.0.pom" &
put c2b-put-pom-410 "$OUT/pom-410.xml" "$BASE/l0134-mvn-d/$G/$M/4.1.0/$M-4.1.0.pom" &
wait
sleep 6
get c2c-get-groupmeta "$BASE/l0134-mvn-d/$G/$M/maven-metadata.xml"

# c3: parallel unique snapshot same version -> buildNumber collision? (distinct bytes so
# a same-name overwrite is observable as content loss, not a no-op)
jarbyte > "$OUT/bytes-race1.bin"; sleep 0.2; jarbyte > "$OUT/bytes-race2.bin"
put c3a-put-jar-race1 "$OUT/bytes-race1.bin" "$BASE/l0134-mvn-u/$G/$M/3.0.0-SNAPSHOT/$M-3.0.0-SNAPSHOT.jar" &
put c3b-put-jar-race2 "$OUT/bytes-race2.bin" "$BASE/l0134-mvn-u/$G/$M/3.0.0-SNAPSHOT/$M-3.0.0-SNAPSHOT.jar" &
wait
sleep 6
get c3c-get-snapdir-meta "$BASE/l0134-mvn-u/$G/$M/3.0.0-SNAPSHOT/maven-metadata.xml"
get c3d-list-snapdir "$API/storage/l0134-mvn-u/$G/$M/3.0.0-SNAPSHOT"

# ---------------- D. checksum policy tri-state ----------------
for R in l0134-mvn-cc l0134-mvn-sg; do
  t=$(echo "$R" | sed 's/l0134-mvn-//')
  jarbyte > "$OUT/bytes-$t.bin"
  put d1-$t-put-file "$OUT/bytes-$t.bin" "$BASE/$R/$G/$M/1.0.0/$M-1.0.0.jar"
  real=$(shasum -a 1 < "$OUT/bytes-$t.bin" | cut -d' ' -f1)
  printf '%s\n' "$real" > "$OUT/cs-$t-ok.sha1"
  printf '%040d\n' 0 | tr '0' 'f' > "$OUT/cs-$t-bad.sha1"   # 40 hex chars, guaranteed wrong
  put d2-$t-sha1-ok   "$OUT/cs-$t-ok.sha1"  "$BASE/$R/$G/$M/1.0.0/$M-1.0.0.jar.sha1"
  sleep 2
  put d3-$t-sha1-bad  "$OUT/cs-$t-bad.sha1" "$BASE/$R/$G/$M/1.0.0/$M-1.0.0.jar.sha1"
  sleep 2
done
# d4: oversize sidecar (>1024B) on defaults repo
head -c 1200 /dev/zero | tr '\0' 'x' > "$OUT/cs-huge.sha1"
put d4-huge-sidecar "$OUT/cs-huge.sha1" "$BASE/l0134-mvn-d/$G/$M/1.0.0/$M-1.0.0.jar.sha1"

echo "---- wire files: $OUT ----"; ls "$OUT"
