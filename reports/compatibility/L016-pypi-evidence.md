# L016-1 pypi simple 扩章首票前段 — 契约化取证（真实客户端腿补全）

- 日期：2026-09-13（LOOP 016 / L016-1 / differential-qa-engineer）
- 模式：**dual**（A=Artifactory ref :8082 7.161.20；B=BinFlow UAT :8083 `uat-l015-ebdb8fb6`——HEAD=cd9cb770 领先 5 commit，其间 `internal/adapter/pypi`、`internal/httpapi` 零改动（仅 metadata likePrefix fix），pypi 面行为基线纯）
- 纪律：全程串行；每臂 settle 2s；ref 503 门（首轮 repo-create 触发 3 次 503 门、等待后过——门生效；环境事故一次见 §5）
- 客户端：**真实腿**——twine 7.0.0 / pip 26.2.1（python 3.14.7，venv 内钉版）；twine 原始 multipart wire 离线 sink 落档（`l016-wire/client/twine-raw.req`，已脱敏）；无效 wheel 三态为 curl 复刻（twine 7 对 badmeta/badver 客户端拦截——见 §3 前言）
- 证据：`reports/compatibility/l016-wire/{a,b}/`（pypi/*.hdr+body、client/*.log、arms.txt）+ `l016-wire/client/twine-raw.req`；脚本 `tools/difftest/l016/{pypi-evidence.sh,make-fixtures.py}`
- 规格/种子锚：`docs/reverse/maven-npm-pypi.md` §3.1-3.7 + `reports/compatibility/L015-expansion-probe.md` §2
- 命名空间：`l016-pypi` repo 双端建删净（deleteContent，双端复核 404/400-Bad-Request[Artifactory 删除键位形态]）

## 1. 探针矩阵（19 臂）

fixture：真实 sdist/wheel（zip 含 dist-info METADATA、tar 含 PKG-INFO，twine check PASS）——`hello16-1.0.0{.tar.gz,-py3-none-any.whl}`（METADATA/PKG-INFO 含 `Requires-Python: >=3.8`）、`hello16-1.0.1.tar.gz`（yanked 臂）、三态坏 wheel 见 §3。

| # | 臂 | A（ref） | B（UAT） | 判定 |
|---|---|---|---|---|
| 1 | 空根 `GET /simple/` | 200 120B（头模板逐字同） | 200 121B（唯一差=尾部 `\n`） | **一致**（尾换行记 normalize） |
| 2 | **真实 twine 7 sdist 上传** | 200 空体，exit 0 | 200 空体，exit 0 | **一致** |
| 3 | 根索引即时性（t0≈上传后 2-3s） | t0 **已含** hello16：`<a href="hello16" data-requires-python="&gt;=3.8" rel="internal">hello16</a>` | t0 已含：`<a href="hello16/">hello16</a>` | **差异（条目形态 ×3）**；即时性双端均 ≤3s（L015 P2b 异步空窗未复现——见 §4-R1） |
| 4 | **真实 twine 7 wheel 上传**（METADATA Requires-Python: >=3.8） | 200 空体，exit 0 | 200 空体，exit 0 | **一致** |
| 5 | 根即时性复核（wheel 后） | 197B 同形态 | 152B 同形态 | 同臂 3 |
| 6 | 包页 `GET /simple/hello16/` | `../../hello16/1.0.0/<file>#sha256=<hash>` + `data-requires-python="&gt;=3.8"` + `rel="internal"`；whl→tar.gz 文件名排序 | `../../packages/hello16/1.0.0/<file>#sha256=<hash>`，**无任何属性**；同排序 | **差异（×3）**：链接模板（packages/ 段）/data-requires-python 丢失/rel 丢失；`#sha256=` fragment 逐字同 |
| 7 | ETag 304 | 200→**304**；`Etag: 883205695`（无引号十进制折迭哈希）；`Cache-Control: max-age=60`；`Content-Type: text/html` | 200→**304**；`Etag: "7cf8…e1b6"`（带引号 sha256hex）；无 CC；`Content-Type: text/html; charset=utf-8`；`Vary: Accept` | **一致（304 语义）** + 头面差 4 项（CC 缺失=对齐候选，同 npm D7 先例；etag 算法 opaque；CT charset；Vary） |
| 8 | 无尾斜杠 302 | 302；`Location: http://localhost:8081/artifactory/…/simple/hello16/`（**绝对 URL 且 :8081=Override-Base-Url 配置派生**） | 302；`Location: hello16/`（**相对**） | **差异（形态；A 侧部署敏感）** |
| 9 | 锚点链路（resolve 本端包页 href→GET -L） | 裸形态 URL 200 **直达 0 跳**，848B/360B，sha256=fixture 逐字 | packages/ URL 200 直达 0 跳，**同 sha256** | **一致**（链路+内容哈希） |
| 9x | 跨形态路由矩阵 | `packages/…` 200 **且** 裸路径 200（双形态都服务） | 同（双形态都服务） | **一致——路由对称**（L015 P3 的「结构差」实为页面 href 模板差，非路由差） |
| 10 | **真实 pip download**（默认 wheel 优先） | exit 0；848B wheel 落盘（sha256 过） | exit 0；同 | **一致** |
| 10n | PEP 691 协商（pip 字面 Accept：json q=0.9 优先） | 200 **`Content-Type: text/html`**（JSON simple 默认关——§3.2 规格活体证实） | 200 **`application/vnd.pypi.simple.v1+json; charset=utf-8`** 749B（PEP 691 JSON 全形：meta.api-version 2.0/files[].hashes） | **差异（协商面）——新发现**：B 做 JSON、A 不做（超集 vs 参照）→ UNKNOWN 升裁 |
| 11 | **真实 pip download sdist**（`--no-binary :all: --no-build-isolation`） | `Downloading hello16-1.0.0.tar.gz (360 bytes)` 下载+sha256 校验过后，本地 metadata prep 失败（fixture setup.py NameError——客户端侧） | **同阶段同错**（日志逐字同，仅 tmp 路径异） | **一致（下载+哈希校验面）**；fixture 尾巴见 §6 |
| 12 | **真实 pip install --target** | exit 0（dist-info 落 target） | exit 0 | **一致** |
| 13 | 坏名 wheel（文件 `notl016-1.0.0-…whl` vs 表单 name=hello16；METADATA 自洽） | 200 入库；**索引挂在 `/simple/notl016/`**（200，303B——索引键=文件名派生名）；hello16 页不变 | 200 入库；**索引挂在 `/simple/hello16/`**（632B 第三条）；`/simple/notl016/` 404 | **差异（索引键来源：文件名 vs 表单名）** |
| 14 | 坏元数据 wheel（zip 有效、无 METADATA 文件；文件名 nol016 避开路径冲突） | 200 入库；**不入任何索引**（hello16 页 530B 不变；/simple/nol016/ 404） | 200 入库；**入 hello16 索引**（807B） | **差异（无效元数据索引策略：A 防污染 / B 放行）** |
| 15 | 坏版本 wheel（文件 `hello16-abc-…whl` vs 表单 version=1.0.0） | 200 入库且入 hello16 索引（709B） | 同（980B，累计） | **一致**（双端都收且都入索引；attr/模板差仍适用） |
| 14b | 重复上传（同路径同文件名再传 good wheel） | **200 静默接受**（重部署允许） | **400** `file '…' already exists … overwriting is not allowed` | **差异（重部署策略）——真实 twine 可达**（重复 upload 是常见误操作） |
| 16 | yanked 表单字段（sdist 1.0.1 + `yanked=true`，curl） | 200；锚点 **无 data-yanked**（属性面双端同忽略）；但锚点带 `data-requires-python="&gt;=3.8"`——**上传未发 requires_python 表单字段，值只能派生自 sdist PKG-INFO（服务端解析）** | 200；无 data-yanked；无任何属性 | **差异（=已知 drps 坑的新源证据：A 三源派生——表单/wheel METADATA/sdist PKG-INFO；B 三源全丢）** |
| 17 | legacy JSON `/pypi/hello16/json` | **200** warehouse 全形（1180B）：info{name/version=1.0.1[最高版]/requires_python/yanked:false…}+urls[]{digests{sha256,md5},requires_python,upload_time…}+vulnerabilities+last_serial | **404**（152B） | **差异（UNSUPPORTED 候选）**——A 全文落档为裁定素材 |
| 18 | legacy JSON `/pypi/hello16/1.0.0/json` | 200（1765B；urls[0] 14 字段族） | 404 | 同上 |

### 1.1 真实 twine 7 上传 wire（离线 sink 落档，`l016-wire/client/twine-raw.req`）

表单字段全集（sdist）：`metadata_version=2.1`、`summary`、`requires_python`（源自 PKG-INFO）、`name`、`version`、`pyversion=source`、`filetype=sdist`、`sha256_digest`、`blake2_256_digest`、`:action=file_upload`、`protocol_version`、`content`。
**无 `md5_digest`**——twine ≥6.2 弃发 md5 的规格点（§3.3）在 twine 7.0.0 活体证实；双端上传 200 = 服务端自算校验和并接受，`md5_digest` 可缺失规格点双绿。
twine 7 客户端校验边界：坏元数据 wheel、坏版本 wheel 在**客户端即被拒**（`InvalidDistribution`），永不上发——三态矩阵服务端行为只能 curl 复刻取证（本身即证据：真实发布者不可达面）。

## 2. 差异清单与分类建议（10 项；INTENTIONAL 终局裁定权不在本角色）

| D# | 面 | A | B | 分类建议 | 一句证据 |
|---|---|---|---|---|---|
| D1 | 锚点 `data-requires-python` | 三源派生（表单/wheel METADATA/sdist PKG-INFO）全渲染 | 三源全丢 | **BUG** | 真实 twine 表单腿（t2）+ PKG-INFO 派生腿（y1）双证；pip 按 requires-python 过滤版本，客户端可见 |
| D2 | 锚点 `rel="internal"` | 全条目渲染 | 无 | **UNKNOWN**（倾向对齐） | pip 不消费 rel（pip-blind），字节/金样面差；对齐低风险 |
| D3 | 包页 href 模板 | `../../{name}/{ver}/{file}`（裸） | `../../packages/{name}/{ver}/{file}` | **UNKNOWN** | c1x 双端路由对称（packages/ 与裸路径双端都 200）——仅页面模板差，pip 等价；金样面需裁 |
| D4 | 根索引条目形态 | 裸名 href + drp + rel | 尾斜杠 href、无属性 | **UNKNOWN**（=D1/D2 在根级的投影 + href 尾斜杠差） | i1 双端体逐字对照 |
| D5 | PEP 691 JSON 协商 | Accept JSON→仍 text/html（默认关） | Accept JSON→`vnd.pypi.simple.v1+json` 全形 | **UNKNOWN（超集能力）** | pd1-accept691 双端 CT 对照；B 为 wider-than-reference，裁「对齐关/保留注记」 |
| D6 | 坏名 wheel 索引键 | 文件名派生名（/simple/notl016/） | 表单名（混入 hello16 页） | **UNKNOWN** | b1 三点对照（hello16 页/notl016 页/404）；真实 twine 不可达（表单与元数据恒一致） |
| D7 | 坏元数据 wheel 索引策略 | 入库不入索引 | 入库且入索引 | **BUG 候选（对齐 A：防索引污染）** | b2 对照；无元数据文件可经 curl 造出，污染 hello16 页 |
| D8 | 同路径重复上传 | 200 静默接受 | 400 overwrite not allowed | **UNKNOWN（真实客户端可达的硬差异）** | b2b 臂；`twine upload` 误重发常见场景，A 幂等/B 拒绝 |
| D9 | legacy JSON API | 200 全功能 | 404 | **UNSUPPORTED 候选（做/裁二选一升 conductor）** | l1/l2 全文落档（A 侧金样候选）；§3.1 规格高置信 |
| D10 | 302 Location 形态 | 绝对 URL（:8081=Override-Base-Url 派生，部署敏感） | 相对 `hello16/` | **UNKNOWN（倾向 B 保留相对+注记）** | r1 头对照；RFC 7231 相对 Location 合法，pip 双吃 |
| — | ETag 算法/CT charset/Vary | 折迭哈希整数/text/html/Vary 无 | sha256hex 带引号/;charset=utf-8/Vary: Accept | 记录级（normalize 收） | opaque 面，304 语义双绿 |
| — | `Cache-Control: max-age=60` | 有 | 无 | **对齐候选（BUG，同 npm D7 先例）** | e1 头对照；npm 域已裁定对齐 max-age=60 |

## 3. 无效 wheel 三态矩阵结论

前提：twine 7 对坏元数据/坏版本**客户端拦截**（`twine check` ERROR 实证），三态仅 curl 可达——服务端防御面取证。

| 态 | A（ref） | B（UAT） | 结论 |
|---|---|---|---|
| 坏名（filename↔表单名错位，METADATA 自洽） | 200 入库；索引键=**文件名派生名**（/simple/notl016/ 200；存储路径仍按表单 hello16/1.0.0/） | 200 入库；索引键=**表单名**（hello16 页混入 notl016 文件条目；/simple/notl016/ 404） | 索引键来源分叉（D6）；A 形态对 pip 更自洽（页内文件名与包名一致），B 页内异名文件会被 pip 忽略 |
| 坏元数据（zip 有效、无 METADATA） | 200 入库、**不入索引**（防污染） | 200 入库、**入索引** | 策略分叉（D7）；A 更严 |
| 坏版本（version=abc 非法 PEP 440） | 200 入库且入索引 | 200 入库且入索引 | **双端同放行**——双端对非法版本字符串零校验（twine 客户端挡住了真实链路，服务端均不设防；记录级） |
| （附）同路径重复上传 | 200 幂等接受 | 400 拒绝 | D8 |

## 4. 回归对照（vs L015 §2 pypi 12 臂）

| L015 项 | 本轮状态 | 说明 |
|---|---|---|
| P2b 根即时性（异步 vs 即时） | **软化** | 本轮双端 t0（≈2-3s 窗口）均含条目；A 异步空窗未复现——真实客户端粒度无可观察差；如需钉死须亚秒级探针（可选后段票） |
| P2b' 根条目形态 | **仍在** | i1 逐字对照；且新证据：A 根条目也带 data-requires-python（L015 fixture 无 requires-python 字段故未显） |
| P3 链接 packages/ 段 | **仍在，性质澄清** | c1x：双端对两种路由形态都 200——差的是页面 href 模板（D3），非路由能力 |
| P3' rel 缺失 | **仍在** | p2 全条目 |
| P4 无效 wheel（伪字节）永不入索引 vs 入索引 | **仍在（换态复证）** | 本轮坏元数据态（b2）：A 不入/B 入——同族策略差 |
| P4c data-requires-python 丢失 | **仍在，证据升格** | 真实 twine 表单腿（t2）+ sdist PKG-INFO 服务端派生腿（y1）——从 curl 单源升为真实客户端双源三证 |
| P5 302 形态 | **仍在** | r1（绝对 :8081 vs 相对） |
| P8 gzip | 未重测 | L015 证据（双端均不 gzip）维持，本轮未派臂 |
| P9 ETag 304 | **仍在=绿** | 304 双端复证 |
| P10 legacy JSON 404 | **仍在** | l1/l2 + A 侧全文金样落档 |

新增差异（本轮首证）：D5 PEP 691 JSON 协商、D6 坏名索引键、D8 重复上传策略、根条目属性投影（D4）；新增一致面：真实 pip 下载/安装 happy path 全绿（L015 缺的真实客户端腿补齐）、双路由形态对称、twine 上传 200 族。

## 5. 环境事故记录（影响时序，不影响判定）

1. B 侧首轮跑到 arm 1 时 Docker Desktop 整体僵死（docker CLI 假性 000、端口监听但后端无响应、ref 一度 503）——19:50→20:15 全灭 25 分钟，20:04 短暂恢复后又灭。按 L015 §4 runbook 处置：等待自愈失败 → Docker Desktop 全量重启 → 手工 `docker start artifactory` → 双端 healthz/ping 200 复核后重跑。B 首轮残留 repo `l016-pypi` 已 deleteContent 清除后重跑。与 L015 同款主机级事故（第三次记录在案）。
2. 会话中段 `/tmp/l016` 工作区被外因部分清空（venv/fixtures 消失）——工作区迁至 `tools/difftest/l016/ws/`（owns 域）后重建复跑，双端全量重跑取证（终版证据=修复 fixture 后的完整双跑）。
3. A 侧 503 门首轮触发 3 次（repo-create），等待后全绿——门纪律生效记录。

## 6. fixture 勘误（诚实记录）

- v1 wheel 的 dist-info 目录名误用整文件名 stem（`hello16-1.0.0-py3-none-any.dist-info`），致 pip install 后置摘要崩溃（InvalidVersion '1.0.0-py3-none-any'）——双端同崩故不影响差分判定；v2 已改 `{name}-{version}.dist-info`，pi1 双端 exit 0。
- sdist `setup.py` 内 `pkg_dir` 变量未求值（字面写入），pip sdist 腿在**下载+sha256 校验完成之后**的本地 metadata prep 失败——下载/哈希面证据完整，build 面为客户端侧 fixture 尾巴，不影响服务端结论。
- 坏元数据态 v1 与 good wheel 同名（B 400=路径冲突非校验拒）——v2 改独立文件名 `nol016-…` 复测，并将「同路径重复上传」升为独立臂 14b。

## 7. 回流清单

- **契约**：`docs/compatibility/contracts/pypi.yaml`（10 条目，evidence 挂本轮 wire）——待 compatibility-engineer 评审入册。
- **normalize 提案**：`docs/compatibility/fixtures/normalize.yaml#pypi`（尾换行/etag opaque/Location host 占位/CT charset；**packages/ 段与锚点属性为断言目标不归一**）。
- **提金候选**（A 侧逐字落档 l016-wire/a）：空根+根条目模板（含 drp HTML 转义形）、包页锚点模板（双文件排序+sha256 fragment）、302 Location 形态、ETag 304 头族、legacy JSON 两端点全文（l1 1180B/l2 1765B——warehouse 兼容形态的唯一样本）、坏名/坏元数据索引策略行为。
- **状态机反馈**：L015 P2b「异步索引」建议降级为「快异步（≤3s 窗口不可分辨）」；maven-npm-pypi.md §3.3「twine ≥6.2 不发 md5_digest」在 twine 7.0.0 活体证实可标 live 双绿；§3.2「PEP 691 默认 false」A 侧活体证实（B 侧相反——正好是待裁差异 D5）。
- **UNKNOWN 升 conductor**：D5（PEP 691 做/裁）、D6（索引键来源）、D8（重复上传策略——真实客户端可达）、D9（legacy JSON 做/裁）、D3/D4/D10（模板/形态对齐项）。
