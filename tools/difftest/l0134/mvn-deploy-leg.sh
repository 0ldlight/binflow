#!/bin/bash
# L013-4 real-client leg: one `mvn deploy` per side into the unique-snapshot repo.
# Usage: mvn-deploy-leg.sh <a|b>   a=Artifactory ref :8082, b=BinFlow UAT :8083
# Env: ARTI_AUTH / BF_AUTH (parsed into settings.xml via ${env.*} — password
# never on argv, never in the pom/settings file).
set -u
SIDE=${1:?side a|b}
if [ "$SIDE" = a ]; then
  REPO_URL=http://localhost:8082/artifactory/l0134-mvn-u; AUTHVAR=ARTI_AUTH
else
  REPO_URL=http://localhost:8083/binflow/l0134-mvn-u; AUTHVAR=BF_AUTH
fi
AUTH=${!AUTHVAR:-}
[ -n "$AUTH" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
MVN_USER=${AUTH%%:*}; export L0134_MVN_PASS=${AUTH#*:}
W=/tmp/l0134/mvn-leg/$SIDE
OUT=/Users/lzw/dev-center/reports/compatibility/l0134-wire/$SIDE/maven
mkdir -p "$W/src/main/java/com/l0134" "$OUT"

cat > "$W/pom.xml" <<EOF
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.l0134</groupId>
  <artifactId>probe-cli</artifactId>
  <version>1.0.0-SNAPSHOT</version>
  <packaging>jar</packaging>
  <distributionManagement>
    <snapshotRepository><id>l0134-repo</id><url>$REPO_URL</url></snapshotRepository>
  </distributionManagement>
</project>
EOF
echo 'package com.l0134; public class Probe { public static void main(String[] a){ System.out.println("l0134"); } }' \
  > "$W/src/main/java/com/l0134/Probe.java"
cat > "$W/settings.xml" <<EOF
<settings><servers><server>
  <id>l0134-repo</id><username>$MVN_USER</username><password>\${env.L0134_MVN_PASS}</password>
</server></servers></settings>
EOF

cd "$W"
mvn -s settings.xml -Dmaven.wagon.http.retryHandler.count=1 -Dmaven.resolver.transport=wagon deploy \
  > "$OUT/mvn-deploy-$SIDE.log" 2>&1
echo "mvn deploy exit=$?"
tail -5 "$OUT/mvn-deploy-$SIDE.log" | sed 's/^/  | /'
unset L0134_MVN_PASS

# post-state: what filenames landed, what metadata says
sleep 3
curl -sS --noproxy '*' -u "$AUTH" -o "$OUT/mvn-leg-snapdir-list.json" -w "snapdir-list %{http_code}\n" \
  "$([ "$SIDE" = a ] && echo http://localhost:8082/artifactory/api/storage || echo http://localhost:8083/binflow/api/storage)/l0134-mvn-u/com/l0134/probe-cli/1.0.0-SNAPSHOT"
curl -sS --noproxy '*' -u "$AUTH" -o "$OUT/mvn-leg-snapdir-meta.xml" -w "snapdir-meta %{http_code}\n" \
  "$REPO_URL/com/l0134/probe-cli/1.0.0-SNAPSHOT/maven-metadata.xml"
curl -sS --noproxy '*' -u "$AUTH" -o "$OUT/mvn-leg-groupmeta.xml" -w "group-meta %{http_code}\n" \
  "$REPO_URL/com/l0134/probe-cli/maven-metadata.xml"
