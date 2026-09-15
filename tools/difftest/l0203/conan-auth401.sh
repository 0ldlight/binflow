#!/bin/bash
# L020-3 Task1 — conan auth401 措辞族双端对照（differential-qa-engineer）
# v1+v2 各抽 2（坏凭据 + 匿名）；文案/CT/信封逐字 + WWW-Authenticate 头。
# A=:8082 Artifactory 7.161.20（/artifactory/api/conan/<repo>）
# B=:8083 BinFlow uat-l0203-f94d6b1e（/binflow/<repo>）
# 凭据运行时 source；difftest 命名空间 l0203-conan-auth；用后删净。
set -u
W=/tmp/l0203/wire
REPO=l0203-conan-auth
AAUTH="${ARTI_AUTH:-admin:JFrog@2026}"
source /Users/lzw/dev-center/deploy/compose/.env.uat
BAUTH="admin:${BINFLOW_ADMIN_PASSWORD:?BINFLOW_ADMIN_PASSWORD unset}"

side() { # side -> base auth
  if [ "$1" = A ]; then echo "http://localhost:8082/artifactory/api/conan/$REPO|$AAUTH"
  else echo "http://127.0.0.1:8083/binflow/$REPO|$BAUTH"; fi
}

mkA() { curl -s -o /dev/null -w "repo-create A: %{http_code}\n" -X PUT --noproxy '*' -u "$AAUTH" -H 'Content-Type: application/json' "http://localhost:8082/artifactory/api/repositories/$REPO" -d '{"rclass":"local","packageType":"conan"}'; }
mkB() { curl -s -o /dev/null -w "repo-create B: %{http_code}\n" -X PUT --noproxy '*' -u "$BAUTH" -H 'Content-Type: application/json' "http://127.0.0.1:8083/binflow/api/repositories/$REPO" -d '{"rclass":"local","packageType":"conan"}'; }

arm() { # side label curl-extra... path
  local s=$1 label=$2 path=$3; shift 3
  local base auth
  IFS='|' read -r base auth <<< "$(side $s)"
  local code ct
  code=$(curl -s -o "$W/${s}-${label}.body" -D "$W/${s}-${label}.hdr" -w '%{http_code}' --noproxy '*' "$@" "${base}${path}")
  ct=$(grep -i '^content-type:' "$W/${s}-${label}.hdr" | tr -d '\r' | head -1)
  local wa; wa=$(grep -i '^www-authenticate:' "$W/${s}-${label}.hdr" | tr -d '\r' | head -1)
  printf '%s %-16s -> %s | %s | %s | body[%dB]: %s\n' "$s" "$label" "$code" "$ct" "${wa:-no-WWW-Auth}" \
    "$(wc -c < "$W/${s}-${label}.body" | tr -d ' ')" "$(head -c 200 "$W/${s}-${label}.body" | tr '\n' ' ')"
}

rmA() { curl -s -o /dev/null -w "repo-delete A: %{http_code}\n" -X DELETE --noproxy '*' -u "$AAUTH" "http://localhost:8082/artifactory/api/repositories/$REPO"; }
rmB() { curl -s -o /dev/null -w "repo-delete B: %{http_code}\n" -X DELETE --noproxy '*' -u "$BAUTH" "http://127.0.0.1:8083/binflow/api/repositories/$REPO?deleteContent=true"; }

echo "== setup"; mkA; mkB
echo "== c1 v2-auth-badpw (GET v2/users/authenticate, Basic admin:WRONG)"
arm A c1-v2auth-badpw /v2/users/authenticate -u "admin:WRONGpw-l0203"
arm B c1-v2auth-badpw /v2/users/authenticate -u "admin:WRONGpw-l0203"
echo "== c2 v2-data-anon (GET v2 search, no auth)"
arm A c2-v2search-anon '/v2/conans/search?q=nope%2F%2A'
arm B c2-v2search-anon '/v2/conans/search?q=nope%2F%2A'
echo "== c3 v1-auth-badpw (GET v1/users/authenticate, Basic admin:WRONG)"
arm A c3-v1auth-badpw /v1/users/authenticate -u "admin:WRONGpw-l0203"
arm B c3-v1auth-badpw /v1/users/authenticate -u "admin:WRONGpw-l0203"
echo "== c4 v1-data-anon (GET v1 recipe ref, no auth)"
arm A c4-v1recipe-anon '/v1/conans/nope/1.0/nope/stable'
arm B c4-v1recipe-anon '/v1/conans/nope/1.0/nope/stable'
echo "== cleanup"; rmA; rmB
for s in A B; do
  if [ "$s" = A ]; then u="http://localhost:8082/artifactory/api/repositories"; a=$AAUTH; else u="http://127.0.0.1:8083/binflow/api/repositories"; a=$BAUTH; fi
  echo "residue $s: $(curl -s --noproxy '*' -u "$a" "$u" | grep -c l0203 || true)"
done
