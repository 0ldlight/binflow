# L012-1 — npm dist-tags 真实 wire 取证（LOOP 012 首票前段）

- 票：LOOP 012 / L012-1（differential-qa-engineer；D12-R05 首票前段=probe，契约与实现留后段拆票）
- 模式：**dual**（参照 + BinFlow 双端活体，无降级）
- 双端：
  - A = Artifactory 7.161.20（:8082，admin；本机 http_proxy=127.0.0.1:7897 → 全部探测 `--noproxy '*'`）
  - B = BinFlow **uat-l0121-d301cf31**（:8083；本票由 uat-l0111-293a754c-wip2 重建：`git worktree d301cf31` → `docker build deploy/release/Dockerfile.alpine` → compose 滚动，数据卷保留）
- 客户端：**npm 10.9.8**（评估票钉的版本锚；本机 node v26.8.1 承载，CLI 逻辑与 npm 11.19.0 逐行 diff 仅 cosmetic 重命名）
- 取证方法：`tools/difftest/l0121/wiretap.py`——一次性线程化反向代理（监听 127.0.0.1:1908x → 转发真实端点），全量记录 method/path/headers/body 与响应；**Authorization 落盘前一律 redacted**；真实凭据仅存于 wiretap 进程 env（CLI 侧只带假 `_authToken` 过 publish 预检，上游由 wiretap 替换为真 Basic——npm publish 的 ENEEDAUTH 预检只认 nerfed registry-scoped 键，`getCredentialsByURI` 不读全局 `_auth`）
- 原始 wire：`reports/compatibility/l0121-wire/`（pass 2，最终态）与 `l0121-wire-pass1/`（pass 1，复跑门对照）；curl 矩阵 `l0121-wire{,-pass1}/curl-matrix-{a,b}.txt`

## 0. 环境注记（复现前提）

| # | 事实 |
|---|---|
| E0-1 | npm 的 nerfed 凭据键对 registry URL **尾斜杠敏感**：`nerfDart('http://h/p/kg')` 掉最后一段（`new URL('.',…)` 语义）→ registry 必须带尾斜杠才能命中 `//h/p/kg/:_authToken` 键。npm.md §5「端口也参与匹配」的姊妹坑，本票实证第三形态 |
| E0-2 | npm 10.9.8 `dist-tag.js` fetchTags 传 `'prefer-online': true`（kebab-case），而 `npm-registry-fetch#getCacheMode` 只读 camelCase `preferOnline`——**该"prefer-online"意图为死代码**，dist-tags GET 实际走 default HTTP 缓存（新鲜期内零网络请求，live 实证：无 `--prefer-online` 时 ls2/rm/notag 全部命中缓存不发包）。取证 rig 以 CLI `--prefer-online` 顶掉（no-cache 模式，且 Artifactory dist-tags GET 响应**无 ETag** → 重验证退化为无条件 GET） |
| E0-3 | npm CLI 对 scoped 包名做 `escapedName = name.replace('/', '%2f')`——**小写 f、@ 不编码**（wire 亲证 `@l0121-scope%2fprobe-pkg`） |
| E0-4 | 环境流量：npm publish 前固定发 `GET <registry>/npm` ×2（自更新横幅探测）→ 双端均 404。K60 §5-1 双端复核一致 |
| E0-5 | UAT 容器在本票收尾阶段被并行轨（L012-3）换至 a81cd053-wip（14:08:29Z）；本票判别探针已在**一次性 d301cf31 容器**（127.0.0.1:18083，用后即焚）复判，且 `git diff d301cf31..a81cd053 -- internal/adapter/npm internal/httpapi` 为空（零产品码漂移）——证据基线仍纯 |

## 1. wire 序列结论（CLI 腿，npm 10.9.8 真客户端）

**总判定：npm dist-tag 全族走 `/-/package/<escapedName>/dist-tags(-/<tag>)` 显式端点族，零 packument 旁路、零 etag/rev 冲突重试。** 序列逐命令如下（A/B 双端线序完全一致，仅 host 前缀不同）：

| 命令 | HTTP 序列 | 备注 |
|---|---|---|
| `npm publish`（前置） | `GET /npm` ×2（404，环境流量）→ `PUT /<name>` 全量 packument → 201 | publish 面单 PUT；A 响应带 `Location`+`X-Checksum-Sha256`+Content-Type `…ItemCreated+json`，B 仅 `X-Request-Id`+`application/json`（publish 深水面，后段票素材） |
| `npm dist-tag ls <pkg>` | `GET /-/package/<esc>/dist-tags` → 200 map | 无条件 GET（E0-2）；A 带 `Cache-Control: max-age=60`，B 无 Cache-Control |
| `npm dist-tag add <pkg>@<v> <tag>` | `GET dist-tags`（客户端预检）→ `PUT /-/package/<esc>/dist-tags/<tag>` body=`"1.0.0"`（7 字节 JSON string）→ 201 `{"ok":"created new tag"}` | 客户端先查当前值；**etag 冲突重试形态不存在**（请求无 If-Match/If-None-Match，响应无 ETag） |
| add 同值（幂等） | 仅 `GET`，**无 PUT** | 客户端短路（`npm warn … already set`） |
| `npm dist-tag rm <pkg> <tag>` | `GET dist-tags` → `DELETE /-…-/dist-tags/<tag>` → 200 空 body | |
| rm 不存在 tag | 仅 `GET`，无 DELETE | 客户端短路报错 `<tag> is not a dist-tag on <pkg>` |
| ls 幽灵包 | `GET dist-tags` → 404 | 客户端 E404 |
| add 到不存在版本 | `GET` 200 → `PUT …/canary` body=`"9.9.9"` → **404** | 客户端不预检版本存在性，服务端拒绝；双端均未创建 tag（事后 GET 验证） |
| ls 无 tag 包（latest 被删后） | `GET dist-tags` → A：200 `{"latest":"1.1.0"}`；B：200 `{}` | **本票最深差分**，见 §3-D2 |

CLI 头面（dist-tag 族请求）：`npm-command: dist-tag|publish`、`npm-auth-type: web`（token 形态）、`Accept: */*`、`user-agent: npm/10.9.8 node/v26.8.1 …`；K60-6 不变量面：dist-tag 族零 `?write=true`、零 409 rev-dance，与 npm.md 定案一致。

## 2. curl 直发端点族矩阵（20 格双端对照）

base：A `http://localhost:8082/artifactory/api/npm/l0121-npm` vs B `http://localhost:8083/binflow/api/npm/l0121-npm`。原始输出：`l0121-wire/curl-matrix-{a,b}.txt`。

| # | 探针 | A（Artifactory） | B（BinFlow d301cf31） | 判定 |
|---|---|---|---|---|
| m01 | GET dist-tags（认证） | 200 `{"latest":"1.1.0"}`（pretty） | 200 `{"latest":"1.1.0"}`（compact） | 一致（格式归一） |
| m02 | GET dist-tags 匿名 | **401** `Authentication is required` | **200** | 差异=实例配置（A 匿名访问全局关 / B `ANONYMOUS_ACCESS=true`），非协议面；差分用认证臂 |
| m03 | GET scoped `%2f`（小写，= npm CLI 实际形态） | 200 | 200 | 一致 |
| m04 | GET scoped `%2F`（大写，lu 等价性） | **200** | **200** | **双端 %2f/%2F 等价——maven-npm-pypi.md §5 挂账项闭合** |
| m05 | GET 幽灵包 | 404 `Not found` | 404 `Package 'l0121-ghost-pkg' not found` | status+信封一致，message 措辞异（D4） |
| m06 | PUT tag happy | 201 `{"ok": "created new tag"}` | 201 `{"ok":"created new tag"}` | 一致（空格归一） |
| m07 | PUT tag 不存在版本 | 404 `…name:l0121-probe-pkg, and version:9.9.9` | 404 `…name:l0121-probe-pkg, and tag:novers` | status 一致+双端均拒建；message 版本/标签位错位（D3） |
| m08 | PUT body 非 JSON string（`not-a-json-string`） | **500** `Internal server error` | **400** `invalid dist-tag body: invalid character 'o' in literal null…` | 双双偏离：A 500 内部错误；B 400 但 message 泄漏 Go unmarshal 内幕（D5） |
| m09 | PUT body JSON object | **500** | **400** `json: cannot unmarshal object into Go value of type string` | 同 D5 |
| m10 | PUT tag 匿名 | 401（无 WWW-Authenticate） | 401 `authentication required` | status 一致；message 大小写为 BinFlow errors[] 既有风格（K60 §6 已裁定） |
| m11 | DELETE tag happy | 200 空 | 200 空 | 一致 |
| m12 | DELETE tag 再删 | 404 `…and tag:staging` | 404 `…and tag:staging` | **逐字节一致**（BinFlow 此臂文案精确对齐） |
| m13 | PUT 集合（bulk map） | **405** Method Not Allowed | **201** `{"ok":"created new tag"}`（curltag 实建，m19 删得动） | BinFlow 实现了参照没有的 bulk 面（D6） |
| m14 | POST 集合 | **405** | **201** 同上（posttag 实建） | 同 D6 |
| m15 | POST 单 tag 子路径 | **405** | **201** 同上 | 同 D6 |
| m16 | GET `/-/ping` | 200 `{}` | 200 `{}` | 一致（K60-3 ✓） |
| m17 | GET `/-/whoami` 认证 | 200 `{"username":"admin"}` | 200 `{"username":"admin"}` | 一致（K60-2 ✓） |
| m18 | GET `/-/whoami` 匿名 | 401 + `WWW-Authenticate: Basic realm="Artifactory Realm"` | 401 + `WWW-Authenticate: Basic realm="BinFlow Realm"` | 形态一致（realm 产品名差异，K60 已记） |

## 3. 差异清单与分类建议（裁定权在 compatibility-engineer / conductor）

| # | 差异 | 证据 | 分类建议 |
|---|---|---|---|
| D2 | **DELETE `latest` 后读时语义**：A 删 latest 后 GET 重算回 `{"latest":"1.1.0"}`（判别探针：set latest=1.0.0→201，DELETE→200，GET 回 **1.1.0**——非 no-op、是读时按最高版本重算）；B 同序列 GET 回 `{}`（真删、不重算）→ 客户端可见分叉：`npm dist-tag ls` 在 B 报 `No dist-tags found`（exit 1），在 A 打印 latest | `l0121-wire*/[ab]/notag-ls.log` + §2 判别探针输出（B 已在纯 d301cf31 一次性容器复判） | **UNKNOWN**（协议面最重差异；npm CLI 无立场——两形态都不违客户端；Artifactory「latest 永生」是便利语义，BinFlow 更朴素。需契约定夺：契约条目应至少钉住一种，另一形态记 known-divergence） |
| D3 | PUT 不存在版本 404 message：A 用 `and version:9.9.9`（真版本值），B 用 `and tag:novers`（错位复用 DELETE 臂文案） | m07 | UNKNOWN（cosmetic 倾向，npm 状态码归类不敏感；若对齐则 B 补版本位文案） |
| D4 | 幽灵包 404 message：A `Not found` vs B `Package '<n>' not found` | m05 | UNKNOWN（cosmetic；B 文案信息量更大） |
| D5 | 非法 PUT body：A **500**（Artifactory 对坏输入 500，参照自身瑕疵）vs B **400 但泄漏 Go unmarshal 文案** | m08/m09 | UNKNOWN——A 形态不建议追齐（500 坏味道）；B 的 400 是更好姿态但 message 应换成中性文案（硬面建议随契约附「400 + 中性 message」） |
| D6 | 集合 bulk PUT/POST 与 POST 单 tag：A 405 全族拒，B 201 全族收（实建 tag） | m13-m15 | UNKNOWN（BinFlow 超 Referenct 面=wider-than-reference；npm 10.9.8 现客户端零依赖（§4），保留无害；若裁定对齐 Artifactory 则应收窄为 405，若裁定超集则记 INTENTIONAL+known-divergence） |
| D7 | dist-tags GET 缓存头：A `Cache-Control: max-age=60`，B 无 | wire 响应头 | UNKNOWN（对 npm 默认缓存行为有可观察影响：A 允许 60s 陈旧，B 无指示→客户端启发式；建议契约钉住其一） |
| — | m02 匿名读（A 401 / B 200） | m02 | 实例配置差（非协议），差分以认证臂为准，入 normalize 注记 |
| — | body pretty/compact、`{"ok": "…"}` 空格、401 message 大小写 | 多格 | 归一化面（建议进 normalize 规则组提案，非差异本体） |

提金候选（golden-capture，交 compatibility-engineer 评审）：npm CLI 腿 happy 序列（ls/add/rm + scoped 形态 + 201/200 空体）、m12 再删 404（双端逐字一致）、m04 %2F 等价双绿、`GET /-/ping` `{}`。

## 4. V-7 清偿：npm CLI 对显式端点族的依赖度

**结论（高置信，源码+活体双证）：npm 10.9.8 dist-tag 全族（ls/add/rm 及别名）100% 依赖显式端点族**——

1. `lib/commands/dist-tag.js`（npm 10.9.8 实物源码逐行核对，与 npm 11.19.0 仅 cosmetic diff）：`fetchTags → npm-registry-fetch.json('/-/package/'+spec.escapedName+'/dist-tags')`；add → `PUT …/dist-tags/<tag>`（body=JSON string 版本号）；rm → `DELETE …/dist-tags/<tag>`。**无任何 packument GET→PUT 全量回写路径、无回退分支**（老 npm-registry-client 的 packument 旁路在当代 npm 已不存在）。
2. 活体 wire 15 case 双端：dist-tag 操作的全部 HTTP 落在端点族 + 环境流量 `GET /npm`，零 `PUT /<name>`、零 `/-rev`、零 `?write=true`、零条件请求。
3. 客户端预检位置：add 的同值短路、rm 的存在性短路均在客户端（GET 后判），服务端仍需兜底（badver 案例证明服务端版本校验必须存在）。

→ **D12-R05 的「显式端点族缺位」问题就此闭合：契约化对象=该端点族本体**；「部分语义经 packument PUT 旁路」的说法对 npm ≥9 客户端不成立（旁路仅历史客户端）。

## 5. K60 族零补证抽核（3/3 一致）

| K60 条目 | 本次 wire | 判 |
|---|---|---|
| K60-2 whoami | B：认证 200 `{"username":"admin"}`；匿名 401 + `WWW-Authenticate: Basic realm="BinFlow Realm"`（m17/m18 + 头亲证） | ✓ 与 npm.md §3 一致 |
| K60-3 ping | B：200 `{}`（m16） | ✓ |
| §5-1 环境流量 | 双端 npm 命令前置 `GET /npm` ×2 → 404（p1 REQUEST 1-2，两侧同形） | ✓（且首次在 **Artifactory 侧**同形复证） |

## 6. 复跑门与清理

- **复跑门**：全量重置（双端仓删重建 + npm 缓存清空）后二次完整跑（family a+b + matrix a+b）——family 退出码向量逐 case 相同（`diff _summary` 零差）、curl 矩阵 byte-identical。B 侧仓删除需 `?deleteContent=true`（非空仓 400 拒删——BinFlow 删仓语义与 Artifactory 不同，本次顺带观察，非本票对象）。
- **清理**：A 仓 `l0121-npm` DELETE 200；B 仓（现 a81cd053-wip 实例内）`?deleteContent=true` DELETE 200 + 复核 404；一次性 d301cf31 容器连卷 `docker rm -f -v`；`git worktree remove`；`/tmp/l0121` 清空。凭据零落盘（wiretap env + redacted 日志复核）。
- 遗留：本地镜像 `binflow:uat-l0121-d301cf31-alpine` 保留（回滚锚）；`.env.uat` 现值 `uat-l0123-a81cd053-wip` 为并行轨 L012-3 所写，非本票状态，未动。

## 7. 后段拆票建议（供 conductor 排程）

1. **契约票**（compatibility-engineer）：`docs/compatibility/contracts/npm.yaml` 首文件，6± 条目锚本票 wire：GET dist-tags（200 map / 404 幽灵）、PUT 单 tag（201 `{"ok":"created new tag"}` / 404 版本不存在——**文案版本位对齐 A 与否 = D3 裁定输入**）、DELETE 单 tag（200 空 / 404 `and tag:<t>` 逐字）、`%2f/%2F` 等价、匿名/认证臂、（裁定后）DELETE latest 读时语义=**D2 必须先裁**、bulk PUT/POST 面存废=**D6 必须先裁**。normalize 提案：JSON pretty/compact、`{"ok": "…"}` 空格、错误信封 message 措辞（若走宽松）。
2. **实现票**（dev-*）：按 D2/D3/D5/D6/D7 裁定结果改 `internal/adapter/npm/disttag` 面——候选：latest 读时重算（或声明不重算）、PUT 404 版本位文案、400 中性文案、bulk 面收窄或保留声明、Cache-Control 钉值。
3. **差分票**（本角色）：契约落地后双腿（curl 矩阵 + npm CLI 10.9.8）正式批跑翻绿 D12-R05 ◐→✅；D12-R03（K60 群）可同批从 npm.md 转写零补证。

---
*证据索引：`tools/difftest/l0121/{wiretap.py,disttag-family.sh,curl-matrix.sh}`；wire 原始日志 `reports/compatibility/l0121-wire{,-pass1}/{a,b}/*.log`（每请求含时戳、头、body；Authorization 红acted）。*
