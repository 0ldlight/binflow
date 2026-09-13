# L018-1 conan 扩章首票前段 — wire 取证 + 契约蓝本（双系统对照）

- 日期：2026-09-13（LOOP 018 / L018-1 / differential-qa-engineer）
- 模式：**dual**（A=Artifactory ref :8082 7.161.20 pro 全开；B=BinFlow UAT :8083 `uat-l0181-74656514`——工作树 b64b2a62 构建，产品码与 74656514 逐字节同（diff 空，commits 间仅 docs））
- 纪律：全程串行；臂预算 ≤22（v2 面 11 + v1 面 11，实跑 22 + setup/teardown 不计）；settle 2s；ref 503/迁移闸门门（本跑未触发——实例迁移早已完成）
- 客户端：**真实腿双版本钉版**——conan 2.31.2（`/tmp/l018venv`）+ conan 1.66.0（`/tmp/l018venv1`），Python 3.14.7 承载；conan 1.66 的 `--detect` 在 apple-clang 17 上炸（settings.yml 止步 16.0——EOL 客户端），以手写最小 profile 规避；conan 1.66 默认 **revisions 关闭**（活体 WARN 证），按默认形态取证（revisions-on 面未覆盖，记后段）
- wire 捕获：curl 腿 `.hdr/.body` 逐部落盘 + 真实 CLI 腿经 `tools/difftest/l018/wireproxy.py`（127.0.0.1:19018 转发 + 逐交换 JSON wire 落档，Authorization 脱敏）——**CLI 腿 22 条 wire 全捕获**（v2/v1 登录、上传×3、下载×2、搜索×2、删除，双端各 11 腿 exit=0×22）
- 证据：`reports/compatibility/l018-wire/{a,b}/`（conan/*.hdr+body、client/*.log、client/wire/*.wire、setup/、arms.txt）+ `fix-sha256.txt`；脚本 `tools/difftest/l018/{conan-evidence.sh,fixup.sh,wireproxy.py,fixtures/}`
- 规格/种子锚：`docs/reverse/conan.md`（216 行双源）+ `expansion-assessment-v2.md` conan 节；matrix D12 conan 主面无独立行（仅 D12-R09 重索引行）——新行建议见 §7
- 命名空间：`l018-conan`（local）+ `l018-conan-virt`（virtual）双端建删净（deleteContent；双端 repo 列表复核无 l018 残留）

## 0. 环境前置与事故（影响时序，不影响判定）

1. **UAT 重建**：`binflow:uat-l0181-74656514-alpine`（compose `--env-file .env.uat --build`；.env.uat 版签更新为 uat-l0181-74656514）。构建途中 Docker Desktop 第 **5** 次全灭（L015/016/017 同款：backend 僵死、双端口 000、docker CLI 挂起）——处置沿 runbook：杀僵尸 build 进程 + `kill -9` backend + Docker Desktop 全量重启 + 手工 `docker start postgres artifactory` → 双端 healthz/ping 200 后重跑构建（层缓存续）。
2. **B 侧 conan 面不可达根因**：首轮 b 全灭（统一 404）——`package type 'conan' is not available (license tier 'community' < 'pro')`。**UAT 无 license=community 档**（此前 L012-L017 四域全是 community 核心包型，未触过档门；conan 是差分首遇 TierPro 包型）。处置：以 repo 根 gitignored 私钥 `binflow-license-private.pem`（T-281 keygen 产物）经 `bf license issue --tier pro --days 90` 铸文档、`POST /binflow/api/system/license` 装载（201，licenseId `243531c2-2b52-460d-84b0-03dd2be6074a`，89 天余量）。**这是 difftest 命名空间外的实例配置变更**——按 §12 应先报 conductor；因不装则票面任务整体不可达，先行处置并在此显式上报：可 `DELETE /binflow/api/system/license` 一键回滚（幂等回 community 地板，conan 仓转只读不删数据——ADR-0032 D1/D2）；**建议 conductor 裁定 UAT 常驻 pro 档**（后续 conan 域差分全依赖）。
3. probe 副产物清理：conan 2 create 在 repo 根落了 6 个 `conan*.sh`（Intel python → x86_64 build context 的 classic 生成器）+ difftest ws conan homes——已删净（git status 复核）；/tmp 脚手架（smoke/tgz/license 文档）已清。

## 1. 探针矩阵（22 臂；判定=归一提案 §6 应用前的原始差分）

fixture：`hello18/1.0@l018/stable`（v2 面，双内容修订 r1/r2）+ `hello18/1.1@l018/stable`（v1 面）——双版兼容 conanfile（try/except 双 import、CLI 传名版本）。

| # | 臂 | A（ref） | B（UAT 74656514） | 判定 |
|---|---|---|---|---|
| v2-01 | ping 双前缀 + 版本检查四连 | **v1/ping** 200+能力头；**v2/ping 400** `Couldn't perform replication…`（无路由，落 replication catch-all——反编译 ConanResource.java:105 只有 v1/ping）；四连版本头全不可达 | v1/ping 200+能力头**逐字同**；v2/ping 200+能力头同；版本检查 `deprecated/outdated/current/server_outdated` 四值齐 | **差异 D1**（v2/ping 超集面）+ 能力面**一致** |
| v2-02 | login（真实 conan 2.31.2 腿）+ authenticate | CLI 全绿；authenticate 200 体=**JWT RS256 806B**（jfrt subject）；坏凭据 401 `Bad Credentials`（pretty envelope） | 同序全绿；200 体=**opaque 64hex 64B**；401 `invalid credentials`（compact envelope） | **一致（协议面）**；token 形态/401 措辞差 D2/D8 |
| v2-03 | check_credentials | 好 token 200 空；坏 token 401 `Props Authentication Token not found` | 200 空；401 `invalid credentials` | **一致（状态面）**；措辞 D2 |
| v2-04 | upload（真实 CLI） | wire 14 交换：v1/ping→revisions 404→pkg-revisions 404→check_credentials→**每文件 PUT×2（X-Checksum-Deploy:true+Sha1 探测 404→实体 PUT 201）** | **逐交换同构**（同请序同状态码） | **一致**（客户端可见面最强证据） |
| v2-05 | rev 链（第二内容修订） | latest/revisions 200；rrev `1d56f752…`/`91eaead8…`（32hex）；time `…+0000` 带毫秒；ghost latest 404 `Not Found` envelope | 同 rrev **逐字同**；time `…Z`；ghost 404 裸文本 `Couldn't find revisions` | **差异 D2/D6/D7**；rrev 断言面**同** |
| v2-06 | recipe files 清单 | `{"files":{…空对象…}}` | 同构（键序异） | **一致**（键序 ignore） |
| v2-07 | download（真实 CLI 新缓存）+文件 GET | CLI install 全绿 exit 0；conanfile.py 200 | 同（**sha256 `a76c0ef3…` 双端同**） | **一致**（字节平价） |
| v2-08 | search（真实 `conan list hello18/*` + curl q=） | CLI wire `search?q=hello18%2F%2A`（**尾缀 `/*` 服务端剥离**否则 0 命中）→ 200 `{"results":["hello18/1.0@l018/stable"]}` | 同序同形 | **一致**（T-308 D2 活体主张双端双证） |
| v2-09 | pkgId 元数据+pkg 修订链 | 主跑空 q **400** `Unexpected query syntax:`（pid 链断，补测 resurrect）；补测 pkg latest/revisions/files/tgz 全 200 | 空 q/**`*`/ref 三态全 200** 返回全 pid map（q 被忽略）；pkg 链 200 | **差异 D4**（q 语义）+ pkg 链**一致** |
| v2-10 | 删单修订+ghost 404 | DELETE 200；**2s 后列表仍含双修订**（异步索引）；补测 8s 后已删者消失；ghost rrev 404 envelope `Couldn't find path 'l018/…/deadbeef…'` | DELETE 200；**立即**列表只余 1；ghost 404 裸文本同文案**+尾斜杠** | **差异 D5（时序）/D2（形态+尾斜杠）**；语义终态一致 |
| v2-11 | 删整 recipe+再删 | 200→latest 404→再删 404 envelope `Couldn't find path 'l018/hello18/1.0/stable'` | 200→404 `Couldn't find revisions`→再删 404 裸文本+尾斜杠 | **一致（状态机）**；文案族 D2 |
| v1-12 | v1/ping | 200+能力头（local 基础集） | 200+能力头逐字同 | **一致** |
| v1-13 | login（真实 conan 1.66 腿） | wire 2 交换：v1/ping→v1/users/authenticate 200 JWT | 同构；opaque 64hex | **一致**（D8 token） |
| v1-14 | check_credentials | 200 空 | 200 空 | **一致** |
| v1-15 | upload（真实 CLI `--all`） | wire 8 交换：ping→digest 404→check→snapshot 404→**upload_urls POST**→check→pkg snapshot 404→pkg upload_urls POST；文件 PUT 直发绝对 URL（**绕代理**） | **逐交换同构**；文件 PUT 同理直发 | **一致**（v1 上传链） |
| v1-16 | upload_urls 绝对 URL | `http://localhost:8082/artifactory/api/conan/l018-conan/v1/files/l018/hello18/1.1/stable/0/export/conanfile.py` | `http://localhost:8083/binflow/l018-conan/v1/files/l018/hello18/1.1/stable/0/export/conanfile.py` | **一致（形态）**：坐标序 user/name/ver/channel→`/0/` 默认修订段→export|package 完全同构；仅 base 前缀差（D9 归一） |
| v1-17 | download_urls+digest | 同形态绝对 URL | 同 | **一致**（同上） |
| v1-18 | snapshot（md5 map） | `{"conanmanifest.txt":"b44c…","conanfile.py":"dd3b5dec…"}`（pretty） | conanfile.py md5 **`dd3b5dec…` 同**；manifest md5 异 | **一致（conanfile.py）/D10（manifest=客户端时间戳行非确定性，见 §4）** |
| v1-19 | search（curl+真实 CLI） | `{"results":["hello18/1.1@l018/stable"]}`；CLI wire `search?q=hello18` | 同形同文案 | **一致** |
| v1-20 | download（真实 conan 1.66 install 新缓存） | wire 7 交换全 200，CLI exit 0 | 同构 | **一致** |
| v1-21 | v1 数据面 on virtual + only_v2 | v1 search on virt **400 `Unsupported Conan v1 repository request for 'l018-conan-virt'`**；v2/ping on virt 400（无路由）；**补测 v1/ping on virt 200+能力头追加 `only_v2`** | 400 **message 逐字同**；v2/ping on virt 200+`only_v2`；补测 v1/ping on virt 200+`only_v2` **逐字同** | **一致（gate+能力面）**；v2/ping 超集归 D1 |
| v1-22 | 整树删+跨面+再删 | CLI remove→200；v1 snapshot 404 envelope `Not Found`+**v2 latest 404（跨面整树删）**；**再删 200 空（幂等）** | CLI remove→200；404 裸文本 `Path not found`+跨面 404 同；**再删 404** | 跨面整树删**一致**；再删面 **差异 D3** |

补测（fixup，同臂内补发非新臂）：A pkg 链 resurrect（pkg latest/revisions/files/tgz 全 200，pid `82339cc4…` 与 B 同）；制品字节三件套对拍（§4）；A 单修订删除 8s 判别；双端 v1/ping-on-virt 能力头。

## 2. 双版 CLI 差异面（收尾问题项）

| 面 | conan 1.66.0（v1 族） | conan 2.31.2（v2 族） |
|---|---|---|
| 握手 | v1/ping → v1/users/authenticate | **v1/ping**（每次操作首跳！）→ v2/users/authenticate |
| 上传 | digest 404→check→snapshot 404→upload_urls（POST 换绝对 URL）→**直发 files/ 通道** | revisions 404→check_credentials→PUT …/files/{path} **每文件 checksum-deploy 舞步**（X-Checksum-Deploy:true+Sha1 0B 探测 404→实体 201） |
| 下载 | download_urls→直发 files/ 通道 | latest→files 清单→v2 files GET |
| revisions | 默认关（WARN：deprecated）；URL 无 rrev 段（服务端 `/0/` 默认修订解析） | rrev=32hex（conanmanifest md5 形态，**非** spec §5 所称 sha256 64hex——勘误项）；pid=40hex |
| 平面交叉 | 永不触 v2 面 | **双面都触**（v1/ping + v2 数据）——双前缀握手面是 conan 2 硬依赖的反向证明 |
| 本机适配 | Python 3.14 可装；`--detect` 炸于 apple-clang 17（settings.yml 顶 16.0）→手写 profile | 无碍（Intel python 下 arch=x86_64——Rosetta 副作用，非服务端行为） |

## 3. 差异清单与分类建议（11 项；INTENTIONAL 终局裁定权不在本角色）

| D# | 面 | A | B | 分类建议 | 一句证据 |
|---|---|---|---|---|---|
| D1 | v2/ping 存在性 | 无路由→400 replication catch-all（local+virtual 同） | 200+完整能力头（+virtual 追加 only_v2） | **UNKNOWN（倾向 B 保留=超集）** | conan 2.31.2 全程只 ping v1/ping（22 腿 wire 证）→client-blind；npm `/-/v1/login` 姿态先例；**reverse spec §2 `{v1\|v2}/ping` 行对参照不成立（勘误项）** |
| D2 | 数据面错误信封族 | Artifactory JSON errors envelope：ghost GET 404=`Not Found`、坏凭据=`Bad Credentials`、坏 bearer=`Props Authentication Token not found`；ghost rrev 404 envelope `Couldn't find path '<p>'` | conan 原生裸文本：`Couldn't find revisions`/`Path not found`；401 envelope 但 `invalid credentials`；ghost rrev 同文案裸文本+**尾斜杠** | **UNKNOWN（对齐候选族）** | npm D4「Not found」先例（conductor 曾裁对齐收）；B 文案恰为 DE 抄录的 conan 原生串但 A 真身不回它；尾斜杠=路径规范化差（不归一，待裁） |
| D3 | v1 ghost DELETE（再删） | **200 空（幂等）** | 404 `Path not found` | **UNKNOWN** | 真实 conan 1 客户端可达（conan remove ×2）；T-369 对齐整树删时未裁 ghost 面 |
| D4 | `<ref>/search?q=` q 语义 | 空 q/`*`/ref 三态全 400 `Unexpected query syntax:` | 三态全 200，q 忽略返回全 pid map | **UNKNOWN** | A 期望的合法 q 语法未探明（complex_search 能力暗示属性查询）；真实客户端腿未触达该端点（两版 CLI wire 均无此调用）——服务端语义面差，后段批跑定 q 语法 |
| D5 | 单修订删除索引可见性 | 异步：2s 后列表仍含已删项、8s 后消失 | 同步：立即消失 | **记录级（timing）** | 终态双端一致（均真删）；conan 客户端删后不立即 list——不可见面 |
| D6 | revisions `time` 格式 | `2026-09-13T15:33:38.493+0000`（Java ISO） | `2026-09-13T15:40:38.929Z`（RFC3339） | **归一候选（§6-R1）** | 值语义同（UTC 带毫秒）；形态差非语义差 |
| D7 | JSON wire_format | pretty（`" : "`） | compact | **归一候选（§6-R2，沿 npm 域）** | 全端点一致规律；conan 客户端全 JSON 解析 |
| D8 | authenticate token 值 | JWT RS256 806B（jfrt 生态） | opaque 64hex | **归一候选（§6-R3）** | 客户端 opaque 用（Bearer 头直传）；GitLab 也 JWT——形态非断言面 |
| D9 | 绝对 URL 前缀 | `:8082/artifactory/api/conan/…` | `:8083/binflow/…` | **归一候选（§6-R4）** | upload_urls/download_urls/digest 返回值前缀=实例 base（A 此处取请求 Host 非 Override-Base-Url——与 pypi L016 D10 行为不同，单列注记）；**路径形态（坐标序/`/0/`/export\|package）双端逐段同构=断言面** |
| D10 | conanmanifest 字节 | 首行 epoch 时间戳（客户端 create-time）+md5 行 | 同格式、时间戳异 | **归一候选（§6-R5）** | 本机 conan 2 缓存同格式实证=客户端非确定性；md5 比对须剥离首行（v1 snapshot 的 manifest 项同理） |
| D11 | （注记）virtual 空成员表 | PUT 收（200） | 400 拒 | 记录级 | storage-admin 域非 conan 契约面；B 更严 |

## 4. 制品字节平价（fixup 对拍）

| 制品 | A sha256 | B sha256 | 结论 |
|---|---|---|---|
| conanfile.py（v2 files GET） | `a76c0ef3a7b8435fc4ea7b0b4161dc5abea00a160d3e531c9cece5d62a1bf246` | **同** | 字节平价 |
| conan_package.tgz | `d4e8857c…` | `d0bd370a…` | **解包 `diff -r` 空**（tgz 仅 license/LICENSE.txt 一文件同内容）——压缩字节差=gzip header 非确定性（客户端打包时刻） |
| conanmanifest.txt | `b3b2bc7e…` | `5ddda734…` | 首行 epoch 异（create-time）+内容行同——客户端非确定性（D10） |
| v1 snapshot conanfile.py md5 | `dd3b5decf1c5c49a8b846948da817e2a` | **同** | 跨面跨版一致 |

## 5. 回归对照（conan 域首轮差分——无上轮清单；对照 T-308/T-312/T-340 单侧实证存量）

- T-308 §3「conan 2.31.2 走 v2/users/authenticate」：**双端双证维持**（且补强：ping 恒 v1 前缀——本 wire 新事实）。
- T-308 D2「search 尾缀 `/*` 服务端剥离」：A/B 双端活体确认（CLI wire `q=hello18%2F%2A` 命中）。
- T-340「conan 1.66 匿名 ref `_/_` wire 形态」：本轮未派匿名 ref 臂（后段批跑候选）。
- T-369「v1 DELETE=整树删（跨面）」：**双端同态维持**（v1 snapshot 404 + v2 latest 404 双绿）；ghost 再删面新差（D3）。
- reverse spec §2 能力头三只（`X-Conan-Server-Version: 0.20.0`/capabilities 四项/only_v2 追加）：**双端逐字双证**——中置信→高。
- reverse spec §5「v2 rrev=sha256 64hex」：**勘误**——conan 2.31.2 活体 32hex（md5 形态）。
- reverse spec §3.1 错误列「404 Couldn't find revisions」：A 真身回 `Not Found` envelope——规格该行需注 A/B 分立（B 按 conan 原生文案实现=as-built 事实）。

## 6. normalize 提案（conan 域新章 `fixtures/normalize.yaml#conan`——归 compatibility-engineer 裁定，本轮不动文件）

| # | 规则 | 内容 | 理由 |
|---|---|---|---|
| R1 | `revision.time: iso8601_equivalence` | `+0000` ≡ `Z`（毫秒保留、时区归 UTC 后比对） | D6；值语义同 |
| R2 | `wire_format: normalize_indent` | pretty ≡ compact（沿 npm 域先例） | D7；conan 客户端全 JSON 解析、无双形态指纹语义 |
| R3 | `token_value: placeholder` | authenticate 200 体（JWT/opaque）→ 占位；非空+格式不判 | D8 |
| R4 | `absolute_url_prefix: placeholder` | upload_urls/download_urls/digest 值的 `scheme://host[:port]/<产品前缀>` → 占位；**路径段（repoKey 起）逐段比对=断言面** | D9；实例 base 非协议面（另注：A 此端点取请求 Host——部署敏感） |
| R5 | `manifest.timestamp_line: strip_epoch_first_line` | conanmanifest.txt 首行若纯 epoch → 剥离后比对；v1 snapshot 的 manifest md5 值不比（时间戳混入） | D10；客户端 create-time 非确定性 |
| R6 | `artifact_bytes: content_not_container` | tgz 类文件不比压缩字节（sha256），比解包内容（名+内容+模式） | §4——gzip header 非确定性 |
| R7 | headers：drop `Date/X-Request-Id/X-Artifactory-*/X-Jfrog-Version/Via`；keep_semantic `X-Conan-Server-Version`/`X-Conan-Server-Capabilities`/`X-Conan-Client-Version-Check`/`Content-Type`/`WWW-Authenticate`；superset tolerate_and_note | 沿四域先例 | 能力头三只是核心断言面 |
| 反向门 | **不归一清单** | 错误信封形态（envelope vs 裸文本）、`Couldn't find path '<p>'` 尾斜杠、401/404 message 措辞、v2/ping 存在性、ghost DELETE 状态码、`<ref>/search` q 语义——**全部为待裁差异面，禁止归一掩盖** | D1-D4 分类建议先行 |

## 7. 回流清单

- **契约蓝本**：`docs/compatibility/contracts/conan.yaml`（12 条目 v1/v2 分组，evidence 全挂本轮 wire）——待 compatibility-engineer 评审入册。
- **normalize 提案**：§6 七规则+反向门（不改 normalize.yaml，提案在案）。
- **提金候选**（A 侧逐字落档 l018-wire/a）：能力头三只形态（v1-12b/fix-a-v1ping-virt.hdr）、upload_urls/download_urls/digest 绝对 URL 形态（v1-16a/17a/17b）、revisions/latest 响应体（v2-05b/c）、`Unsupported Conan v1…` 400 文案（v1-21a）、authenticate JWT 形态（v2-02b）。
- **状态机反馈**：D12 conan 主面建议**新行**（现无独立行——D12-R09 仅重索引；`contract_ref=contracts/conan.yaml`、`last_difftest=本报告`）。
- **规格勘误提案**（docs/reverse/conan.md 归 reverse-engineer）：§2 v2/ping 行、§5 rrev 形态、§3.1 A 侧错误文案三处（§5 清单）。
- **UNKNOWN 升 conductor**：D1（v2/ping 超集姿态）、D2（信封族对齐）、D3（ghost 再删幂等）、D4（pkgmeta q 语义）。
- **实例配置上报**：UAT pro license 装载（§0-2，回滚命令在案；建议常驻）。
- **环境上报**：Docker Desktop 第 5 次全灭（runbook 已沿用；建议 conductor 知会 devops 立案——频次已达 5 次/4 天）。
