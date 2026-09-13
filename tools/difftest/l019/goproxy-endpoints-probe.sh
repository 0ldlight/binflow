#!/bin/bash
# goproxy endpoints probe — L018-2 轻差分腿（compatibility-engineer 设计+实跑，2026-09-13）
# L019-3 复跑版：上游 set -u 下 $5 未绑定 bug 修（GET 臂 4 参调用崩）——diff 仅 file=${5:-} 一行，
# 已提案回灌 docs/compatibility/probes/goproxy/endpoints-probe.sh（归 compatibility-engineer）。
# 六臂：s1 PUT 三件套播种 / g1 @v/list / g2 .info 逐字节 / g3 @latest / g4 .zip CT+校验头 /
#       g5 sumdb supported（404 姿态判别）/ g6 .mod 逐字节
# 前置：A=:8082（/artifactory/api/go/<repo>）；B=:8083（/binflow/<repo>——ADR-0008 前缀）。
#       双侧各建一次性 go local 仓，用后删净。凭据运行时 source，不落日志。
set -u
REPO=l0182-go-local
MOD=example.com/l018mod
VER=v1.0.0
AAUTH="${ARTI_AUTH:-admin:JFrog@2026}"
source /Users/lzw/dev-center/deploy/compose/.env.uat 2>/dev/null || source /Users/lzw/dev-center/deploy/compose/.env
BAUTH="admin:${BINFLOW_ADMIN_PASSWORD:-}"
W=/tmp/l0182; rm -rf "$W"; mkdir -p "$W/pkg"
python3 - "$W" "$MOD" "$VER" <<'EOF'
import sys, zipfile, json, os
w, mod, ver = sys.argv[1], sys.argv[2], sys.argv[3]
prefix = f"{mod}@{ver}"
os.makedirs(f"{w}/pkg", exist_ok=True)
gomod = f"module {mod}\n\ngo 1.21\n"
with zipfile.ZipFile(f"{w}/{ver}.zip", "w") as z:
    z.writestr(f"{prefix}/go.mod", gomod)
    z.writestr(f"{prefix}/hello.txt", "l0182\n")
open(f"{w}/{ver}.mod", "w").write(gomod)
json.dump({"Version": ver, "Time": "2026-09-13T00:00:00Z"}, open(f"{w}/{ver}.info", "w"))
EOF
req() { # side label method path [file]
  local side=$1 label=$2 method=$3 path=$4; local file=${5:-} base auth
  if [ "$side" = A ]; then base="http://localhost:8082/artifactory/api/go/$REPO"; auth=$AAUTH; else base="http://127.0.0.1:8083/binflow/$REPO"; auth=$BAUTH; fi
  local args=(-s -o "$W/${side}-${label}.body" -w '%{http_code} %{content_type}' --noproxy '*' -u "$auth" "$base$path")
  [ -n "$file" ] && args+=(-X PUT -T "$file") || args+=(-X "$method")
  local res; res=$(curl "${args[@]}")
  printf '%-2s %-14s -> %s | %s\n' "$side" "$label" "$res" "$(head -c 120 "$W/${side}-${label}.body" | tr '\n' ' ')"
}
mk() { curl -s -o /dev/null -w "repo-create $1: %{http_code}\n" -X PUT --noproxy '*' -u "$([ $1 = A ] && echo "$AAUTH" || echo "$BAUTH")" -H 'Content-Type: application/json' "http://$([ $1 = A ] && echo 'localhost:8082/artifactory' || echo '127.0.0.1:8083/binflow')/api/repositories/$REPO" -d '{"rclass":"local","packageType":"go"}'; }
rmr() { curl -s -o /dev/null -w "repo-delete $1: %{http_code}\n" -X DELETE --noproxy '*' -u "$([ $1 = A ] && echo "$AAUTH" || echo "$BAUTH")" "http://$([ $1 = A ] && echo 'localhost:8082/artifactory' || echo '127.0.0.1:8083/binflow')/api/repositories/$REPO$([ $1 = B ] && echo '?deleteContent=true')"; }
echo "== setup"; mk A; mk B
echo "== s1 播种 PUT 三件套"; req A s1-put-zip PUT "/$MOD/@v/$VER.zip" "$W/$VER.zip"; req B s1-put-zip PUT "/$MOD/@v/$VER.zip" "$W/$VER.zip"
req A s1-put-mod PUT "/$MOD/@v/$VER.mod" "$W/$VER.mod"; req B s1-put-mod PUT "/$MOD/@v/$VER.mod" "$W/$VER.mod"
req A s1-put-info PUT "/$MOD/@v/$VER.info" "$W/$VER.info"; req B s1-put-info PUT "/$MOD/@v/$VER.info" "$W/$VER.info"
echo "== g1-g6"
req A g1-list GET "/$MOD/@v/list"; req B g1-list GET "/$MOD/@v/list"
req A g2-info GET "/$MOD/@v/$VER.info"; req B g2-info GET "/$MOD/@v/$VER.info"
req A g3-latest GET "/$MOD/@latest"; req B g3-latest GET "/$MOD/@latest"
req A g4-zip GET "/$MOD/@v/$VER.zip"; req B g4-zip GET "/$MOD/@v/$VER.zip"
req A g5-sumdb GET "/sumdb/sum.golang.org/supported"; req B g5-sumdb GET "/sumdb/sum.golang.org/supported"
req A g6-mod GET "/$MOD/@v/$VER.mod"; req B g6-mod GET "/$MOD/@v/$VER.mod"
echo "== checksum headers (g4)"
for s in A B; do
  if [ "$s" = A ]; then url="http://localhost:8082/artifactory/api/go/$REPO"; auth=$AAUTH; else url="http://127.0.0.1:8083/binflow/$REPO"; auth=$BAUTH; fi
  curl -s -I --noproxy '*' -u "$auth" "$url/$MOD/@v/$VER.zip" | grep -i -E 'content-type|x-checksum' | sed "s/^/$s /"
done
echo "== cleanup"; rmr A; rmr B
for s in A B; do
  n=$(curl -s --noproxy '*' -u "$([ $s = A ] && echo "$AAUTH" || echo "$BAUTH")" "http://$([ $s = A ] && echo 'localhost:8082/artifactory' || echo '127.0.0.1:8083/binflow')/api/repositories" | grep -c l0182 || true)
  echo "residue $s: $n"
done

# 残留清理注记（L018-2，2026-09-13）
# goproxy 差分腿因双实例不可达（docker daemon 被并行镜像构建占死，A/B healthz/ping 均 000）中止：
#   - A 侧残留仓 l0182-go-local（repo-create 200 后脚本被杀，PUT 未发生——仓为空）——下轮差分腿开跑前先
#     DELETE http://localhost:8082/artifactory/api/repositories/l0182-go-local 复用同仓（或删后重建）。
#   - B 侧无残留（repo-create 400 未建仓）。400 根因已诊（2026-09-13 复查）：license tier community < pro——
#     packageType 'go' 系 pro 门控面（errors 信封原文明示）。复跑前置=UAT 换 pro 档 license（归
#     conductor/release-engineer，非脚本可修）；契约 binflow_tested_instance 锚 T-285 窗实例（go 面可用）。
# 复跑入口：bash docs/compatibility/probes/goproxy/endpoints-probe.sh（六臂；A PUT 挂起根因同 docker 占死）。
