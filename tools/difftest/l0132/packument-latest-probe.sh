#!/bin/bash
# l0132 packument-latest probe — 判别探针（设计方 compatibility-engineer L013-2；执行归
# differential-qa-engineer，本脚本未被 L013-2 执行过）。
#
# 裁定对象：known-divergence.yaml#npm/packument-latest-recompute（UNKNOWN，gate=LOOP 013）——
#   dist-tags 端点族 DELETE latest 读时重算已落（L012-2b D2）；packument 面（GET /:name）的
#   dist-tags.latest 是否同步读时重算未裁定（BinFlow 以 TestDistTagRecomputeIsReadTimeOnly
#   钉住「packument 投影与端点族同源重算」现状，参照侧该臂未单独取证）。
#
# 序列（每侧）：publish 1.0.0+1.1.0 → pin latest=1.0.0 → DELETE latest → GET packument（判别）
#   → GET dist-tags（跨面对照）→ 追加 2.0.0+1.0.2 → 再 pin/delete → GET packument（重算基准判别）。
#
# 判定标准（diff 模式实现，F1 为 gate）：
#   F1 packument-latest-recompute（A=p5 latest vs B=p5 latest，GET /:name 删 latest 后）：
#     A 缺席 && B 缺席  → NO_DIVERGENCE —— 建议 ledger resolved（无差异：BinFlow 现状即参照
#                         姿态；TestDistTagRecomputeIsReadTimeOnly 断言维持）。
#     A 在(1.1.0) && B 缺席 → DIVERGENT —— 建议 classification=BUG + 实现票（packument GET
#                         读时重算，与 dist-tags GET 同源同款 D2）；翻 TestDistTagRecomputeIsReadTimeOnly。
#     A 缺席 && B 在   → BinFlow 超集 —— UNKNOWN 升级（裁收窄或保留，报 conductor）。
#     双在：normalize 后同值 → ALIGNED（B 已同源重算，ledger resolved）；异值 → UNKNOWN
#                         升级（以 F3 重算基准输出为判据报 conductor）。
#   F2 versions 投影形态（p1 基线，informational——packument 契约票素材，不 gate）：
#     顶层键集 / versions 键集 / 单版本字段名并集 / dist.tarball URL 形态（剥 origin 后比对）。
#   F3 重算基准（p8，判别 semver-max vs 最近发布）：发布序 1.0.0,1.1.0,2.0.0,1.0.2（末发=低版）
#     后删 latest，latest 回 2.0.0 → semver-max；回 1.0.2 → 最近发布；缺席 → 不重算。
#     产出回填 contracts/npm.yaml#npm/dist-tags-latest-recompute 的 unobserved 注记。
#
# Usage:
#   packument-latest-probe.sh <a|b>     跑单侧序列（写 obs/*.json + 阶段日志）
#   packument-latest-probe.sh diff      双侧对拍出判定表（需两侧 obs/ 已备）
# Env: ARTI_AUTH / BF_AUTH = "user:password"（发布腿经 wiretap 注入；读腿 curl -u，沿 l0121 惯例）。
# 前置（执行方备）：双侧各建一次性 local npm 仓 l0132-npm（跑后可整仓删除）。
#   A: PUT :8082/artifactory/api/repositories/l0132-npm {"rclass":"local","packageType":"npm"}
#   B: PUT :8083/binflow/api/repositories/l0132-npm {…}（B 侧创建体见 docs/reverse/repo-operations.md）
# 客户端：优先复用 /tmp/l0121/npm10（评估锚 npm 10.9.8），缺则回退系统 npm（记录版本）。
set -u
REPO=l0132-npm
PKG=l0132-pkdoc-pkg
OUT=/Users/lzw/dev-center/reports/compatibility/l0132-packument
TAP=/Users/lzw/dev-center/tools/difftest/l0121/wiretap.py
NPM=/tmp/l0121/npm10/node_modules/.bin/npm
[ -x "$NPM" ] || NPM=$(command -v npm)

side_base() { # side_base <a|b> → BASE DIRECT_REG AUTHVAR
  if [ "$1" = a ]; then
    echo "http://localhost:8082/artifactory/api/npm/$REPO|http://localhost:8082/artifactory/api/npm/$REPO|ARTI_AUTH|8082|artifactory"
  else
    echo "http://localhost:8083/binflow/api/npm/$REPO|http://localhost:8083/binflow/api/npm/$REPO|BF_AUTH|8083|binflow"
  fi
}

run_side() {
  local SIDE=${1:?side a|b}
  IFS='|' read -r TAPPED DIRECT AUTHVAR PORT PREFIX <<< "$(side_base "$SIDE")"
  [ -n "${!AUTHVAR:-}" ] || { echo "\$$AUTHVAR not set" >&2; exit 2; }
  local OBS="$OUT/$SIDE/obs"; mkdir -p "$OBS"
  local WS=/tmp/l0132/ws/$SIDE; mkdir -p "$WS"
  local AUTH="${!AUTHVAR}"
  echo "======== side=$SIDE direct=$DIRECT npm=$($NPM --version)"

  # -- 发布腿：真客户端经 wiretap（假 token userconfig，真 Basic 只在 wiretap env）----------
  printf '//127.0.0.1:%s/%s/api/npm/%s/:_authToken=l0132-fake-client-token\n' "$PORT" "$PREFIX" "$REPO" > /tmp/l0132/npmrc-fake-$SIDE
  local NPMFLAGS=(--userconfig /tmp/l0132/npmrc-fake-$SIDE --cache /tmp/l0132/npm-cache-$SIDE
                  --registry "http://127.0.0.1:$PORT/$PREFIX/api/npm/$REPO/"
                  --prefer-online --no-fund --no-audit --loglevel warn
                  --fetch-retries 0 --fetch-timeout 30000)
  setver() { printf '{"name":"%s","version":"%s","description":"l0132 packument latest probe"}' "$PKG" "$2" > "$WS/package.json"; }
  pub() { # pub <case-id> <version>
    setver "$WS" "$2"
    echo "== $1 publish $2"
    TAP_CWD=$WS python3 "$TAP" --target "http://localhost:$PORT" --port "$PORT" \
      --log "$OUT/$SIDE/$1.log" --auth-env "$AUTHVAR" -- \
      "$NPM" "${NPMFLAGS[@]}" publish 2>&1 | tail -2
  }
  pub p0a-publish-100 1.0.0
  pub p0b-publish-110 1.1.0

  # -- 读/改腿：直发 curl（body 存档为 obs；-D 存响应头面）--------------------------------
  snap() { # snap <obs-name> <method> <path> [curl-extra...]
    local name=$1 method=$2 path=$3; shift 3
    local code
    code=$(curl -sS --noproxy '*' -u "$AUTH" -X "$method" -H 'Content-Type: application/json' \
      -m 15 -D "$OBS/$name.headers" -o "$OBS/$name.json" -w '%{http_code}' "$DIRECT$path" "$@")
    echo "snap $name -> $method:$code $path"
  }
  snap p1-baseline        GET    "/$PKG"
  snap p2-pin-low         PUT    "/-/package/$PKG/dist-tags/latest" -d '"1.0.0"'
  snap p3-pinned          GET    "/$PKG"
  snap p4-delete-latest   DELETE "/-/package/$PKG/dist-tags/latest"
  snap p5-after-delete    GET    "/$PKG"                       # ← F1 判别本体
  snap p6-disttags-face   GET    "/-/package/$PKG/dist-tags"   # ← 跨面对照（D2 已裁）

  pub p7a-publish-200 2.0.0
  pub p7b-publish-102 1.0.2
  snap p7c-pin-low        PUT    "/-/package/$PKG/dist-tags/latest" -d '"1.0.0"'
  snap p7d-delete-latest  DELETE "/-/package/$PKG/dist-tags/latest"
  snap p8-basis           GET    "/$PKG"                       # ← F3 判别本体
  snap p9-disttags-basis  GET    "/-/package/$PKG/dist-tags"

  # -- 收尾：latest 复位到 semver 最高（留仓 canonical；整仓删除由执行方裁量）--------------
  curl -sS --noproxy '*' -u "$AUTH" -X PUT -H 'Content-Type: application/json' \
    -d '"2.0.0"' -o /dev/null -w "cleanup restore latest: %{http_code}\n" \
    "$DIRECT/-/package/$PKG/dist-tags/latest"
  echo "---- side=$SIDE done: obs in $OBS"
}

run_diff() {
  python3 - "$OUT" <<'PY'
import json, sys, os, re
out = sys.argv[1]
def load(side, name):
    p = os.path.join(out, side, 'obs', name + '.json')
    try:
        return json.load(open(p))
    except Exception as e:
        return {'__error__': f'{e}'}
def latest(doc):
    dt = doc.get('dist-tags', {}) if isinstance(doc, dict) else {}
    return dt.get('latest', '__absent__')
def versions_keys(doc):
    return sorted(doc.get('versions', {}).keys()) if isinstance(doc, dict) else []
def per_version_fields(doc):
    vs = doc.get('versions', {}) if isinstance(doc, dict) else {}
    u = set()
    for v in vs.values(): u |= set(v.keys())
    return sorted(u)
def tarball_shape(doc):
    # dist.tarball: 剥 origin（host:port/prefix），只留仓后路径形态
    vs = doc.get('versions', {}) if isinstance(doc, dict) else {}
    shapes = set()
    for v in vs.values():
        t = (v.get('dist') or {}).get('tarball', '')
        shapes.add(re.sub(r'^https?://[^/]+', '', t))
    return sorted(shapes)
def f3_label(val, hisemver='2.0.0', lastpub='1.0.2'):
    if val == '__absent__': return 'no-recompute'
    if val == hisemver: return 'semver-max'
    if val == lastpub: return 'most-recent-publish'
    return f'other({val})'

rows = []
a5, b5 = load('a','p5-after-delete'), load('b','p5-after-delete')
la, lb = latest(a5), latest(b5)
if la == '__absent__' and lb == '__absent__': f1 = 'NO_DIVERGENCE（双端不重算——ledger 建议 resolved：BinFlow 现状即参照姿态，TestDistTagRecomputeIsReadTimeOnly 维持）'
elif la != '__absent__' and lb == '__absent__': f1 = 'DIVERGENT（A 重算、B 不重算——ledger 建议 BUG + packument GET 读时重算实现票（D2 同款）；翻 TestDistTagRecomputeIsReadTimeOnly）'
elif la == '__absent__' and lb != '__absent__': f1 = 'UNKNOWN 升级（B 超集重算——裁收窄或保留，报 conductor）'
elif la == lb: f1 = f'ALIGNED（双端重算同值 {la}——ledger 建议 resolved；B 已同源重算）'
else: f1 = f'UNKNOWN 升级（双端重算但异值 A={la} B={lb}——以 F3 基准输出为判据报 conductor）'
rows.append(('F1 packument-latest-recompute (p5)', f'A={la} B={lb}', f1))

a8, b8 = load('a','p8-basis'), load('b','p8-basis')
fa, fb = f3_label(latest(a8)), f3_label(latest(b8))
rows.append(('F3 重算基准 (p8)', f'A={fa} B={fb}', '回填 dist-tags-latest-recompute unobserved 注记；F1 异值时的判据'))

a1, b1 = load('a','p1-baseline'), load('b','p1-baseline')
rows.append(('F2 顶层键集', f'A={sorted(a1.keys()) if isinstance(a1,dict) else a1}', f'B={sorted(b1.keys()) if isinstance(b1,dict) else b1}'))
rows.append(('F2 versions 键集', f'A={versions_keys(a1)}', f'B={versions_keys(b1)}'))
rows.append(('F2 单版本字段并集', f'A={per_version_fields(a1)}', f'B={per_version_fields(b1)}'))
rows.append(('F2 dist.tarball 形态(剥 origin)', f'A={tarball_shape(a1)}', f'B={tarball_shape(b1)}'))
a3 = load('a','p3-pinned'); b3 = load('b','p3-pinned')
rows.append(('p3 显式钉可见性（sanity）', f'A.latest={latest(a3)}', f'B.latest={latest(b3)}（钉 1.0.0 后 packument 应显 1.0.0；不显=钉面分叉，记 drift）'))

print('======== l0132 packument-latest 判定表')
print(f'{"字段":<34} {"A":<46} {"B":<46}')
for name, va, vb in rows: print(f'{name:<36} {str(va):<48} {str(vb):<48}')
print('---- GATE（F1）:', f1.split('（')[0])
print('（完整裁定映射见脚本头「判定标准」；F2 informational=packument 契约票素材不 gate）')
PY
}

case "${1:-}" in
  a|b) run_side "$1" ;;
  diff) run_diff ;;
  *) echo "usage: $0 <a|b|diff>" >&2; exit 2 ;;
esac
