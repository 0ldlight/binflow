#!/bin/bash
# L019-3 conan PUT 补臂 — v1 files 直传通道 A 侧 wire 取证（≤4 臂，A=:8082 only）。
# 背景：L018 v1-15 真实 CLI 腿 wire 止于 upload_urls POST——conan 1.66 对返回的绝对 URL
#       直发 PUT（绕过 wireproxy），故「PUT files/<path>」请求/响应形态在 A 侧从未落 wire。
#       本脚本以 curl 模拟客户端直发，补齐 A 形态（成功码/头/体+索引副作用）。
# 契约候选：conan/v1-url-family side_effects「upload_urls 返回的 URL 即 v1 直传通道」——
#       unobserved（直发 PUT wire）解除素材；goproxy/put-upload-triplet 同票另臂（s1）。
# Usage:   conan-put-wire.sh            # 需 env ARTI_AUTH=user:password
# Out:     reports/compatibility/l019-conan-put/a/{conan,setup}/…
# Cleanup: repo l019-conan-put 删净（deleteContent）+ 残留复核。
set -u
HERE=/Users/lzw/dev-center/tools/difftest/l019
WIRE=/Users/lzw/dev-center/reports/compatibility/l019-conan-put
AUTH=${ARTI_AUTH:?env ARTI_AUTH=user:password required}
BASE=http://localhost:8082/artifactory
CBASE=$BASE/api/conan/l019-conan-put
REF='hello19/1.0/l019/stable'   # fresh ref（A 侧从未上传）
OUT=$WIRE/a; rm -rf "$OUT"; mkdir -p "$OUT/conan" "$OUT/setup"
ARMS=$OUT/arms.txt; : > "$ARMS"
arm() { echo "[$(date +%H:%M:%S)] arm $*" | tee -a "$ARMS"; }
req() { # req <id> <method> <url> [curl args...]
  local id=$1 method=$2 url=$3; shift 3
  curl -sS --noproxy '*' -u "$AUTH" -X "$method" -D "$OUT/conan/$id.hdr" \
    -o "$OUT/conan/$id.body" -w "$id %{http_code} %{size_download}B ct=%{content_type}\n" "$url" "$@"
}

# ── setup（不计臂）──
arm "0-setup repo l019-conan-put(local conan)"
printf '{"rclass":"local","packageType":"conan"}' > "$OUT/setup/local.json"
req setup-repo-put PUT "$BASE/api/repositories/l019-conan-put" -H 'Content-Type: application/json' -T "$OUT/setup/local.json"
sleep 2

# fixture 文件（内容锚可复算 md5/sha1）
mkdir -p /tmp/l019cp
cat > /tmp/l019cp/conanfile.py <<'PY'
from conan import ConanFile
class hello19Recipe(ConanFile):
    settings = "os"
PY
printf 'hello19 manifest line1\n' > /tmp/l019cp/conanmanifest.txt

# ── cp-01 upload_urls 换取绝对 URL（形态再锚 + URL 供直发）──
arm "cp-01 upload_urls(recipe face)"
SZ_PY=$(wc -c < /tmp/l019cp/conanfile.py | tr -d ' ')
SZ_MF=$(wc -c < /tmp/l019cp/conanmanifest.txt | tr -d ' ')
printf '{"conanfile.py":%s,"conanmanifest.txt":%s}' "$SZ_PY" "$SZ_MF" > "$OUT/conan/cp-01.reqbody"
req cp-01-upload-urls POST "$CBASE/v1/conans/$REF/upload_urls" \
  -H 'Content-Type: application/json' -T "$OUT/conan/cp-01.reqbody"
URL_PY=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["conanfile.py"])' "$OUT/conan/cp-01-upload-urls.body" 2>/dev/null || true)
URL_MF=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["conanmanifest.txt"])' "$OUT/conan/cp-01-upload-urls.body" 2>/dev/null || true)
arm "    (url_py=$URL_PY)"
echo "url_py=$URL_PY" >> "$ARMS"; echo "url_mf=$URL_MF" >> "$ARMS"
sleep 2

# ── cp-02 裸直发 PUT（无 checksum 头——最小客户端形态）──
arm "cp-02 direct PUT conanfile.py (no checksum headers)"
req cp-02-put-plain PUT "$URL_PY" -H 'Content-Type: application/octet-stream' -T /tmp/l019cp/conanfile.py
sleep 2

# ── cp-03 带 X-Checksum-Sha1 直发 PUT（conan 1.x 实发候选形态）──
arm "cp-03 direct PUT conanmanifest.txt (X-Checksum-Sha1)"
SHA1_MF=$(shasum -a 1 /tmp/l019cp/conanmanifest.txt | cut -d' ' -f1)
req cp-03-put-sha1 PUT "$URL_MF" -H "X-Checksum-Sha1: $SHA1_MF" -T /tmp/l019cp/conanmanifest.txt
sleep 2

# ── cp-04 副作用与回读：files GET 字节平价 + v1 snapshot md5 登记面 ──
arm "cp-04 readback: files GET + v1 snapshot"
req cp-04a-get-file GET "$URL_PY"
req cp-04b-snapshot GET "$CBASE/v1/conans/$REF"
cmp -s /tmp/l019cp/conanfile.py "$OUT/conan/cp-04a-get-file.body" \
  && echo "cp-04a byte-parity: SAME" | tee -a "$ARMS" \
  || echo "cp-04a byte-parity: DIFF" | tee -a "$ARMS"
sleep 2

# ── teardown ──
arm "T-teardown"
req teardown-repo DELETE "$BASE/api/repositories/l019-conan-put?deleteContent=true"
N=$(curl -s --noproxy '*' -u "$AUTH" "$BASE/api/repositories" | grep -c l019-conan-put || true)
echo "residue A: $N" | tee -a "$ARMS"
rm -rf /tmp/l019cp
echo "done arms=$(grep -c '^\[.*\] arm [c0T]' "$ARMS")"
