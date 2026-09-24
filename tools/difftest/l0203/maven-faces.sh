#!/bin/bash
# L020-3 Task2 — maven 三小面差分（differential-qa-engineer）
# ①手 PUT snapshot maven-metadata.xml 受理码（L019-1+2 Risk③：B 201 vs A 202）
# ②手 PUT 后 GET 回读 XML 渲染形态（Risk④：modelVersion 属性/元素序）
# ③文件 404 措辞（Risk⑤：A 'File not found.; Path:' vs B 'Failed to find...'）
# 每面 ≤3 臂；A=:8082 /artifactory/<repo>；B=:8083 /binflow/<repo>（直存通道）。
set -u
W=/tmp/l0203/wire
REPO=l0203-mvn-local
G=l0203/test; AID=artver; V=1.0-SNAPSHOT
AAUTH="${ARTI_AUTH:?set ARTI_AUTH admin:password}"
source /Users/lzw/dev-center/deploy/compose/.env.uat
BAUTH="admin:${BINFLOW_ADMIN_PASSWORD:?BINFLOW_ADMIN_PASSWORD unset}"
METADIR="$W/maven-metadata.xml"
cat > "$METADIR" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<metadata modelVersion="1.1.0">
  <groupId>l0203.test</groupId>
  <artifactId>artver</artifactId>
  <version>1.0-SNAPSHOT</version>
  <versioning>
    <snapshot>
      <timestamp>20260914.000001</timestamp>
      <buildNumber>7</buildNumber>
    </snapshot>
    <lastUpdated>20260914000001</lastUpdated>
    <snapshotVersions>
      <snapshotVersion>
        <extension>pom</extension>
        <value>1.0-20260914.000001-7</value>
        <updated>20260914000001</updated>
      </snapshotVersion>
    </snapshotVersions>
  </versioning>
</metadata>
XML
POM="$W/artver-1.0-SNAPSHOT.pom"
cat > "$POM" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>l0203.test</groupId>
  <artifactId>artver</artifactId>
  <version>1.0-SNAPSHOT</version>
</project>
XML

arm() { # side label method path [file]
  local s=$1 label=$2 method=$3 path=$4; local file=${5:-} base auth
  if [ "$s" = A ]; then base="http://localhost:8082/artifactory/$REPO"; auth=$AAUTH; else base="http://127.0.0.1:8083/binflow/$REPO"; auth=$BAUTH; fi
  local args=(-s -o "$W/${s}-${label}.body" -D "$W/${s}-${label}.hdr" -w '%{http_code}' --noproxy '*' -u "$auth" "$base$path")
  [ -n "$file" ] && args+=(-X PUT -T "$file") || args+=(-X "$method")
  local code; code=$(curl "${args[@]}")
  local ct; ct=$(grep -i '^content-type:' "$W/${s}-${label}.hdr" | tr -d '\r' | head -1)
  printf '%s %-14s -> %s | %s | body[%dB]: %s\n' "$s" "$label" "$code" "$ct" \
    "$(wc -c < "$W/${s}-${label}.body" | tr -d ' ')" "$(head -c 260 "$W/${s}-${label}.body" | tr '\n' ' ')"
}

mkA() { curl -s -o /dev/null -w "repo-create A: %{http_code}\n" -X PUT --noproxy '*' -u "$AAUTH" -H 'Content-Type: application/json' "http://localhost:8082/artifactory/api/repositories/$REPO" -d '{"rclass":"local","packageType":"maven"}'; }
mkB() { curl -s -o /dev/null -w "repo-create B: %{http_code}\n" -X PUT --noproxy '*' -u "$BAUTH" -H 'Content-Type: application/json' "http://127.0.0.1:8083/binflow/api/repositories/$REPO" -d '{"rclass":"local","packageType":"maven"}'; }
rmA() { curl -s -o /dev/null -w "repo-delete A: %{http_code}\n" -X DELETE --noproxy '*' -u "$AAUTH" "http://localhost:8082/artifactory/api/repositories/$REPO"; }
rmB() { curl -s -o /dev/null -w "repo-delete B: %{http_code}\n" -X DELETE --noproxy '*' -u "$BAUTH" "http://127.0.0.1:8083/binflow/api/repositories/$REPO?deleteContent=true"; }

echo "== setup"; mkA; mkB
echo "== s1 播种 pom"
arm A s1-put-pom PUT "/$G/$AID/$V/$AID-$V.pom" "$POM"
arm B s1-put-pom PUT "/$G/$AID/$V/$AID-$V.pom" "$POM"
sleep 3
echo "== f2a 基线：GET metadata（服务端生成形）"
arm A f2a-meta-baseline GET "/$G/$AID/$V/maven-metadata.xml"
arm B f2a-meta-baseline GET "/$G/$AID/$V/maven-metadata.xml"
echo "== f1 手 PUT maven-metadata.xml（受理码面）"
arm A f1-meta-put PUT "/$G/$AID/$V/maven-metadata.xml" "$METADIR"
arm B f1-meta-put PUT "/$G/$AID/$V/maven-metadata.xml" "$METADIR"
sleep 3
echo "== f2b 手 PUT 后 GET 回读（渲染形态面）"
arm A f2b-meta-readback GET "/$G/$AID/$V/maven-metadata.xml"
arm B f2b-meta-readback GET "/$G/$AID/$V/maven-metadata.xml"
echo "== f3 文件 404 措辞（三臂）"
arm A f3a-ghost-jar GET "/$G/$AID/$V/$AID-$V.jar"
arm B f3a-ghost-jar GET "/$G/$AID/$V/$AID-$V.jar"
arm A f3b-ghost-dir GET "/$G/nosuch/$V/nosuch-$V.jar"
arm B f3b-ghost-dir GET "/$G/nosuch/$V/nosuch-$V.jar"
arm A f3c-ghost-meta GET "/$G/$AID/2.0-SNAPSHOT/maven-metadata.xml"
arm B f3c-ghost-meta GET "/$G/$AID/2.0-SNAPSHOT/maven-metadata.xml"
echo "== cleanup"; rmA; rmB
for s in A B; do
  if [ "$s" = A ]; then u="http://localhost:8082/artifactory/api/repositories"; a=$AAUTH; else u="http://127.0.0.1:8083/binflow/api/repositories"; a=$BAUTH; fi
  echo "residue $s: $(curl -s --noproxy '*' -u "$a" "$u" | grep -c l0203 || true)"
done
echo "== f2b 回读体逐字节"
for s in A B; do echo "--- $s f2b $(wc -c < "$W/${s}-f2b-meta-readback.body")B sha256=$(shasum -a 256 "$W/${s}-f2b-meta-readback.body" | cut -c1-16)"; done
diff "$W/A-f2b-meta-readback.body" "$W/B-f2b-meta-readback.body" >/dev/null 2>&1 && echo "f2b: byte-identical" || echo "f2b: differs (see bodies)"
