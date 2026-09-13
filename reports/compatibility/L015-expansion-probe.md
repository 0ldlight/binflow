# L015-4 扩章第三票候选取证 — npm publish 族 vs pypi simple 索引面

- 日期：2026-09-13（LOOP 015 / L015-4 / differential-qa-engineer）
- 模式：**dual**（A=Artifactory ref :8082 7.161.20；B=BinFlow UAT :8083 `uat-l015-ebdb8fb6`，HEAD=ebdb8fb6 工作树构建）
- 纪律：全程串行；每臂 settle 2s；ref 503 门（本次未触发 503，gate 全绿；环境事故两次见 §4）
- 客户端：npm 10.9.8（评估锚 /tmp/l0121/npm10，wiretap 注入 Basic）+ curl 复刻（pypi 面无本地 twine，任务授权 curl 复刻）
- 证据：`reports/compatibility/l015-probe-wire/{a,b}/{npm,pypi}/`（响应头+体+wire 日志）；脚本 `tools/difftest/l015/expansion-probe.sh`
- 复核附录：ghost 干净复刻（g2/g3/g4）、真 wheel（p4c）、ETag 复核、显式 gzip——均在正文标注

## 1. npm publish 族探针矩阵（12 臂）

规格锚：`docs/reverse/maven-npm-pypi.md` §2.1-2.7 + §5-2/§5-3；`docs/reverse/npm.md`（认证族）。

| # | 臂 | A（ref） | B（UAT） | 判定 |
|---|---|---|---|---|
| N1 | npm publish 1.0.0 wire | `GET /npm`×2 → 单 `PUT /{name}`（tarball 内联 `_attachments`）→ 201 | 同形（wire 逐 hop 一致） | **一致** |
| N2 | packument 读面 | 200；`ETag`=`X-Checksum-Sha1`=sha1hex；304 重验证 ✓ | 同款（ETag=sha1hex ✓、304 ✓）；929B vs 742B 投影差 | **差异（投影族）** |
| N3 | republish 同版本 | 403 `Cannot modify pre-existing version '1.0.0', aborting upload for: 'l015-probe-pkg'` | 403 **逐字同消息** | **一致** |
| N4 | 幽灵版本 publish（versions 9.9.9 + attachment 名 -1.0.0.tgz） | 403（守卫=**tarball 路径已存在**，消息引 9.9.9） | **201 `{"success":true}`**（守卫=version 存在性） | **差异（BUG 候选）** |
| N4' | ghost 干净复刻 g2（全新包：真 1.0.0 → ghost 9.9.9 → 读回） | ghost 403；且 crafted 1.0.0 的 packument **404**（tarball 已入库但投影不可见） | ghost 201；packument 200 含 `[1.0.0, 9.9.9]`，**latest 被劫持到 9.9.9** | **差异（双面）** |
| N4'' | crafted 201→404 对照 g3/g4（补 shasum+integrity） | 201→404 维持（最小文档不物化投影；缺场字段待票内二分） | —（g2 已示 201→200） | **差异（A 侧行为）** |
| N5 | 非法 semver `1.0` | 400 `Invalid Version: '1.0', aborting upload for: 'l015-probe-pkg'` | 400 `Invalid Version: '1.0'`（**缺后缀**） | **差异（消息形）** |
| N6 | 单版本 unpublish wire | `GET ?write=true`×2 → `PUT /{name}/-rev/1-0` → GET → `DELETE …/-/…tgz/-rev/1-0` | 同 hop 集；rev 值形态 `3-16a35e6bb94d1592`（A=`1-0`，均 opaque 客户端回传） | **一致（hop）** |
| N7 | 整包 unpublish wire | `GET ?write=true`×2 → `DELETE /{name}/-rev/1-0`（**无** PUT -rev 跳） | 同形 | **一致** |
| N8 | stale-rev 直发 `PUT /{name}/-rev/000-bogus-rev` | 200 `{"ok": "updated package"}` 假成功；packument 无形变 | 200 同款假成功；无形变 | **一致**（§5-2 隐式副作用疑虑**清偿**） |
| N9 | scoped publish | `PUT @l015-scope%2fprobe-pkg`（**小写 %2f**）→ 201 | 同形（§5-3 等价性**清偿**） | **一致** |

### 1.1 npm 投影形态细目（N2 差异展开）

| 维度 | A | B |
|---|---|---|
| 顶层独有键 | `users` | — |
| 版本对象独有键 | `lastModified`、`scripts`（服务端注入） | `_nodeVersion`、`_npmVersion`、`readme`（客户端原样保留） |
| `time` | 含 `unpublished: null` + 毫秒精度 | 秒精度、无 `unpublished` 键 |
| `dist` | tarball 重写为本基址 + shasum/integrity | 同款（各自基址） |

分类建议：投影族差异大概率 **npm-blind**（npm install/publish 主链不消费 users/scripts 注入差异），但 ghost 201 与 latest 劫持（N4'）是**客户端可见硬差异**（`npm install pkg@9.9.9` 将拉到 1.0.0 的 tarball 字节）→ BUG 候选。

## 2. pypi simple 索引面探针矩阵（12 臂）

规格锚：`docs/reverse/maven-npm-pypi.md` §3.1-3.7。

| # | 臂 | A（ref） | B（UAT） | 判定 |
|---|---|---|---|---|
| P1 | 空根 `GET /simple/` | 头模板逐字同（DOCTYPE+title+`api-version=2`）；120B | 121B（同头） | **一致** |
| P2 | sdist 上传（:action=file_upload+md5_digest） | 200 空体 | 200 空体 | **一致** |
| P2b | 上传后根索引 | **仍空**（+2s）；后补物化（**异步索引**） | **立即**含条目 | **差异（时序）** |
| P2b' | 根条目形态 | `<a href="hello15" rel="internal">hello15</a>`（裸名+rel） | `<a href="hello15/">hello15</a>`（**尾斜杠、无 rel**） | **差异（×2）** |
| P3 | 包页链接形态 | `../../hello15/1.0.0/hello15-1.0.0.tar.gz#sha256=…`（**无 packages/ 段**） | `../../packages/hello15/1.0.0/…#sha256=…`（**多 packages/ 段**） | **差异（结构）** |
| P3' | `rel` 属性 | `rel="internal"` 全条目 | **无** | **差异** |
| P4 | 无效 wheel（伪字节）上传 | 200 入库但**永不入索引**（存储 2 子项、索引 1 条） | 200 入库**且入索引** | **差异（策略）** |
| P4c | 真 wheel（zip+METADATA+Requires-Python） | 入索引；`data-requires-python="&gt;=3.8"`（HTML 转义） | 入索引；**丢 data-requires-python**（表单字段+METADATA 双源均未生效） | **差异（BUG 候选——pip 按 requires-python 过滤，客户端可见）** |
| P4s | 文件名排序 | tar.gz（单条目） | whl→tar.gz→whl 字典序正确 | 一致（A 未同版双文件对照，票内补） |
| P5 | 无尾斜杠 302 | `Location: http://localhost:8081/artifactory/…`（**绝对 URL 且指向 8081=Override-Base-Url 配置**） | `Location: hello15/`（相对） | **差异（形态+部署敏感）** |
| P6 | `/simple/{name}/{version}` | 404（74B §0 信封） | 404（159B BinFlow 消息体） | 状态一致，消息体差异（记录级） |
| P7 | 坏 :action（缺元数据） | 400 `Metadata is not complete, missing name or version`（**先查元数据**） | 400 `unknown action 'submit'`（**先查 action**） | **差异（验证顺序）** |
| P7b | 坏 :action（全元数据） | 400 `unknown action 'SUBMIT'`（**大写化**） | —（B 已在 P7 出 action 错） | 差异（大小写处理） |
| P8 | gzip 协商（显式 `AE: gzip`） | **无 CE、无 Vary**（与 §3.2 高置信规格相悖——spec-gap 或条件未满足，票内复核） | 无 CE、`Vary: Accept`（非 AE） | **一致（双端均未 gzip）** + 双端对规格各有一缺口 |
| P9 | ETag 304 | 304 ✓（etag=`-1539234185` 带符号十进制、无引号） | 304 ✓（etag=`"sha256hex"` 带引号）；首测 200 系页面变更致回传 etag 过期，复核 304 ✓ | **一致**（算法形态差异，opaque 合规） |
| P10 | legacy JSON `/pypi/{name}/json` | 200 1156B（warehouse 兼容） | **404 not implemented** | **差异（UNSUPPORTED 候选）** |

## 3. 三轴打分与择优

| 轴 | npm publish 族 | pypi simple 索引面 |
|---|---|---|
| 证据厚薄 | **厚**：真客户端 wire 全链（publish/unpublish/rev-dance/scoped）双端落档；§2.3 校验序 live 对照 4 步；§5-2/§5-3 两笔挂账顺带清偿 | 中厚：curl 复刻 12 臂全落地；**缺真实 pip/twine 腿**（本地无 twine） |
| 差分密度 | 6 处（硬差异 1：ghost 201+latest 劫持；其余投影族/消息后缀/rev 形态，npm-blind 概率高） | **10 处**（硬差异 ≥4：requires-python 丢失、链接 packages/ 段、P5 302 形态、legacy JSON 404；另有无效 wheel 索引策略、根即时性、rel 属性） |
| 实现缺口 | **小**：主链 B 已全绿（wire hop 逐字同形），缺口集中在守卫谓词单点 + 投影细节 | **中**：attr 集缺失（rel、data-requires-python）+ legacy API 未实现 + 链接模板分歧（需 authority 裁定 INTENTIONAL 与否） |

**推荐第三票：pypi simple 索引面。**
理由：差分密度最高且多 pip 可见（`data-requires-python` 直接影响 pip 的 python 版本过滤；`packages/` 段与 302 形态影响下载重定向链）；实现缺口清晰可收敛（补属性→裁链接形态→决 legacy API）；本域此前零差分记录，契约化+提金空间最大。npm publish 族主链已绿，剩余 ghost 守卫单点深坑建议并入后续 npm 深水票（④），不必占第三票。

### 首票拆法（四段闭环）

1. **probe**（半程已完成）：补真实 pip 腿（`pip download --index-url --no-deps` 对拍哈希/属性消费）与 twine 腿（真表单字段全集）；无效 wheel 策略矩阵（伪 zip/坏 METADATA/好 wheel 三态 × 双端）。
2. **契约**：`docs/compatibility/contracts/pypi.yaml` simple 索引面——头模板/锚点属性集（rel、data-requires-python、data-yanked）/链接模板（packages/ 段裁定）/排序/ETag/302/legacy JSON API 决策项；normalize 提案（sha256 随内容、时间戳、etag 剥引号）。
3. **实现**：B 补 `data-requires-python`（双源：表单 requires_python + wheel METADATA）、`rel="internal"`、P5 Location 形态裁定、invalid-wheel 索引策略对齐、legacy JSON API 做/裁决策。
4. **批跑**：`tools/difftest/l015x/` 批跑入口（本 `expansion-probe.sh` 为种子）+ wire 落档 + 回归对照（fixed/仍在/新增）。

## 4. 环境事故记录（影响时序，不影响判定）

1. UAT 重建 `uat-l015-ebdb8fb6` 期间 Docker Desktop 两次整体僵死（多阶段 node/docs/go 构建 + 同 VM 内 Artifactory/penpot 栈并发）：ref+UAT 同时 000。处置=杀构建→Docker 全量重启→手工 `docker start postgres artifactory`→**临时停 penpot 栈腾 VM 资源**→第 4 次构建成功（buildkit 缓存热）→penpot 已恢复。教训：此主机上 in-Docker 全量构建期间 docker CLI/端点探测会假性 000，判死前先看构建日志是否推进。
2. B 侧 repo 删除需 `?deleteContent=true`（非空仓 400 拒删）；difftest 命名空间 `l015-*` 双端已删净，`/tmp/l015` 已清。

## 5. 回流清单

- **状态机反馈**：`docs/reverse/maven-npm-pypi.md` §5-2（`-rev` PUT 隐式副作用）与 §5-3（%2f 等价）可标记 live 双绿清偿（compatibility-engineer 裁定改写）；§3.2 gzip 行为与 live 观测相悖，建议复核（可能条件化）。
- **提金候选**（A 侧逐字落档于 l015-probe-wire/a）：pypi 头模板+锚点模板（含 rel/data-requires-python 转义形）、npm republish 403 / badsemver 400 消息全文、`{"ok": "updated package"}` 假成功体、P7/P7b 400 消息族。
- **UNKNOWN 升 conductor**：B `packages/` 链接段与 302 相对形态是否 INTENTIONAL（无 authority 不裁）；A crafted-201→packument-404 的最小文档门槛字段。
