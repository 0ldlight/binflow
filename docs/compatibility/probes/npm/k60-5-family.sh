#!/bin/bash
# k60-5-family probe — K60-4/-5 补格取证（LOOP 014 / L014-4，compatibility-engineer 设计+实跑）
#
# 对象：
#   m21  POST /-/v1/login（K60-4 web-login 姿态——A 侧头面捕获，T-L013-2 Next 差分批跑补格①）
#   m22  GET  /-/npm/v1/user（K60-5）
#   m23  GET  /-/npm/v1/tokens（K60-5）
#   m24  POST /-/npm/v1/tokens + DELETE /-/npm/v1/tokens/token/<key>（K60-5）
#   L1   PUT /-/user/org.couchdb.user:<name> body 缺 name/password（④-① login 缺字段臂：DE 400 vs B ?）
#   L2   PUT /-/user/org.couchdb.user:<name>/-rev/<rev>（④-② id+rev 臂：A 无路由 404 vs B ?）
#
# 前置：A=http://localhost:8082（admin/JFrog@2026）；B=http://127.0.0.1:8083（BINFLOW_ADMIN_PASSWORD
#   取自 deploy/compose/.env，运行时 source，不落日志/输出）。双侧各建一次性 npm local 仓，用后删净。
# 用法：bash docs/compatibility/probes/npm/k60-5-family.sh   （输出逐臂 status/CT/body 摘要）
set -u
REPO=l0144-k605-local
A=http://localhost:8082
B=http://127.0.0.1:8083/binflow   # BinFlow 挂 /binflow 前缀（npm.md 挂载前缀；A 为 /artifactory）
source /Users/lzw/dev-center/deploy/compose/.env.uat 2>/dev/null || source /Users/lzw/dev-center/deploy/compose/.env
AAUTH="admin:JFrog@2026"
BAUTH="admin:${BINFLOW_ADMIN_PASSWORD:-}"
NAME="l0144probe"
OUT=/tmp/l0144; mkdir -p "$OUT"

req() { # side label method path auth body
  local side=$1 label=$2 method=$3 path=$4 auth=$5 body=$6 base
  [ "$side" = A ] && base=$A || base=$B
  local args=(-s -o "$OUT/${side}-${label}.body" -w '%{http_code} %{content_type}' -X "$method" --noproxy '*' \
    "$([ "$side" = A ] && echo "$base/artifactory" || echo "$base")/api/npm/$REPO$path" -H 'Accept: application/json')
  [ -n "$auth" ] && args+=(-u "$auth")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
  local res; res=$(curl "${args[@]}")
  printf '%-6s %-34s -> %s | %s\n' "$side" "$label" "$res" "$(head -c 160 "$OUT/${side}-${label}.body" | tr '\n' ' ')"
}

mkrepo() { # side
  local side=$1 base auth
  [ "$side" = A ] && { base=$A; auth=$AAUTH; } || { base=$B; auth=$BAUTH; }
  curl -s -o /dev/null -w "repo-create $side: %{http_code}\n" -X PUT --noproxy '*' \
    -u "$auth" -H 'Content-Type: application/json' \
    "$([ "$side" = A ] && echo "$base/artifactory" || echo "$base")/api/repositories/$REPO" -d '{"rclass":"local","packageType":"npm"}'
}
rmrepo() {
  local side=$1 base auth extra
  [ "$side" = A ] && { base=$A; auth=$AAUTH; } || { base=$B; auth=$BAUTH; extra=""; }
  [ "$side" = B ] && extra="?deleteContent=true"
  curl -s -o /dev/null -w "repo-delete $side: %{http_code}\n" -X DELETE --noproxy '*' -u "$auth" \
    "$([ "$side" = A ] && echo "$base/artifactory" || echo "$base")/api/repositories/$REPO${extra:-}"
}

echo "== setup"; mkrepo A; mkrepo B
echo "== m21 /-/v1/login"
req A m21-anon POST /-/v1/login '' '{}'
req B m21-anon POST /-/v1/login '' '{}'
req A m21-admin POST /-/v1/login "$AAUTH" '{}'
req B m21-admin POST /-/v1/login "$BAUTH" '{}'
echo "== m22-m24 /-/npm/v1 家族"
req A m22-user-anon GET /-/npm/v1/user '' ''
req B m22-user-anon GET /-/npm/v1/user '' ''
req A m22-user GET /-/npm/v1/user "$AAUTH" ''
req B m22-user GET /-/npm/v1/user "$BAUTH" ''
req A m23-tokens GET /-/npm/v1/tokens "$AAUTH" ''
req B m23-tokens GET /-/npm/v1/tokens "$BAUTH" ''
req A m24-post POST /-/npm/v1/tokens "$AAUTH" '{"password":"x"}'
req B m24-post POST /-/npm/v1/tokens "$BAUTH" '{"password":"x"}'
req A m24-del DELETE /-/npm/v1/tokens/token/xyz "$AAUTH" ''
req B m24-del DELETE /-/npm/v1/tokens/token/xyz "$BAUTH" ''
echo "== L1 login 缺字段臂（body 无 name/password）"
req A L1-missing PUT "/-/user/org.couchdb.user:$NAME" '' "{\"_id\":\"org.couchdb.user:$NAME\",\"type\":\"user\",\"roles\":[],\"date\":\"2026-09-13T00:00:00.000Z\"}"
req B L1-missing PUT "/-/user/org.couchdb.user:$NAME" '' "{\"_id\":\"org.couchdb.user:$NAME\",\"type\":\"user\",\"roles\":[],\"date\":\"2026-09-13T00:00:00.000Z\"}"
echo "== L2 id+rev 臂"
req A L2-idrev PUT "/-/user/org.couchdb.user:$NAME/-rev/1-abc" "$AAUTH" '{"name":"x","password":"y"}'
req B L2-idrev PUT "/-/user/org.couchdb.user:$NAME/-rev/1-abc" "$BAUTH" '{"name":"x","password":"y"}'
echo "== cleanup"; rmrepo A; rmrepo B
for s in A B; do
  n=$(curl -s --noproxy '*' -u "$([ $s = A ] && echo "$AAUTH" || echo "$BAUTH")" "$([ $s = A ] && echo "$A/artifactory" || echo "$B")/api/repositories" | grep -c l0144 || true)
  echo "residue $s: $n"
done
