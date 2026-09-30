# L040 — maven virtual/remote sidecar 面 + C2/C5/C7 裁定输入 + generic 守卫限值 + 白名单复跑（T-586 / BIN-68）

- 日期：2026-09-30；模式 **dual**（A 活体双轮 + B 双面各双轮）
- A 面：JFrog Artifactory **7.161.26** Enterprise+ @ http://192.168.120.38:8082（当日 `/api/system/version` 复验 `7.161.26 / 86126900`，与 L037/L038/L039 同锚）；凭据经 `/tmp/r3-difftest.env` source 注入（值零落盘零打印不进本报告）
- **B 面双跑（票面 SHA 与载荷账实不符，本批最大发现，先立此存照）**：
  - **B1 = origin/develop `d420bbc7`**（票面钉住的 SHA）。**该树不含 R10 载荷**：`git show d420bbc7:internal/adapter/maven/put.go | grep -c SetClientChecksums` = **0**；`git merge-base --is-ancestor b248be9d d420bbc7` → **非祖先**；d420bbc7（PR #185 merge）第二父 = d9bae6b4（R9 战报 docs）。R10 载荷码 = `3254c887`（BIN-60/T-578 SPI seam）+ `b248be9d`（BIN-61/T-579 nuget），在分支 claude/r6-payload（tip `885b22e5`），**未进 d420bbc7**。R10 战报「PR #185 merged d420bbc7」与 git 图不符，**待 conductor 对账**（网络注：本批两次 `git fetch origin develop` 均失败〔HTTP2 framing / connect timeout〕，远端 develop 是否已越过 d420bbc7 无法复验，B1=本地 tracking ref）。
  - **B2 = `885b22e5`**（R10 载荷 committed 树=票面意图所指「含 R10 载荷」面；非共享树在途改动，detached worktree 固定 SHA）。
  - 两面同规构建：`git worktree add --detach` → `go build -tags dev` → `/tmp/l040/bin/b{1,2}-server`；scratch `BINFLOW_HOME=/tmp/l040/homes/{b1,b2}-r{1,2}`，`BINFLOW_SERVER__LISTEN`=127.0.0.1:**18150/18152**（B1 r1/r2）与 **18153/18154**（B2 r1/r2），`BINFLOW_DEV_TIER=pro`（license `licensed:false, tier:pro` 四实例一致读数）；fixture 上游服务器 0.0.0.0:**18155**（/tmp/l040/upstream.py，静态字节+全请求日志）
- 命名空间：双端统一 `difftest-l040-*`；A 实例配置零触碰；**A 收尾独立复验前缀计数=0**（实例上另有非本批的 `difftest-generic-local`，l040 命名空间外未动）
- **双轮**：A r1→r2 漂移 1 腿（`c7-get-read-control`：A remote 离线态机——r1 `Failed retrieving resource … Connect timed out` → r2 `is assumed offline`；状态性 A 行为非 fixture 挥发，单列注记）；**B1 r1≡r2 漂移 0、B2 r1≡r2 漂移 0**（normalize 口径同 L039：host:port/ISO-TS/UUID/epoch-ms；nupkg zip mtime 已钉死 2026-01-01）
- 探针资产（/tmp 留存备查）：`probe.py`（111 腿/轮：四工作包+setup/teardown/residual）+ `addendum_szb.py`（守卫边界 1024/1025 双腿）+ `reach.py`（A↔fixture 上游连通性预检）+ `upstream.py` + `round.sh` + `compare.py`；raw：`probe-{a,b1,b2}-r{1,2}.json` ×6 + `add-szb-a-r{1,2}.json` + `upstream.log` + 四 home server.log；shakedown 轮 2 次（探针客户端 bug 两处：GAV 命名不合 layout / reach.py 上下文路径双拼——均修复后才入证据轮，证据轮零客户端错误 status=0）
- 前序权威：L039（C1-C7 候选与白名单九腿标尺）、R10 台账批（known-divergence 新七条）、R10 审 A 范围外发现①（maven putSidecar 非 local factsKey 不走 svc.Put）

---

## Arm 1（WP1）— maven virtual/remote 面 sidecar PUT（审 A 范围外①取证）

**B1≡B2 本臂全腿逐字**（29 腿差异面全在 generic local/nuget/sz 域，mv/mr 域零差异——B1↔B2 差异腿清单在案）。A 探针无上游可达（fixture 连通性预检：A→192.168.1.70:18155 Connect timed out、upstream.log 零 /a/ 命中）→ A 面「是否触发回源」以**错误外显形态**判别（A remote GET 会匿名回源并把上游错误包进 404 文案，r1 实证）。

### 1a. 无 defaultDeploymentRepo 的 maven virtual（成员=同前缀 local）

| 腿 | A（双轮） | B1=B2（双轮） | 判定 |
|---|---|---|---|
| `PUT mvn-virt/…/4.0.0/w1-4.0.0.jar`（普通部署） | **405** `No local repository was configured as local deployment repository for the (difftest-l040-mvn-virt) virtual repository.` | **405** 同文案**逐字** | 一致（与 L039 arm1a generic 面同形） |
| `PUT …/3.0.0/w1-3.0.0.jar.sha1`（源在场·值对） | **405** 同文案——**sidecar 终缀不豁免部署路由拒绝** | **201** CL0 空体、Location→**虚拟仓**源路径 | **差异（审 A 假说坐实：假成功 201）** |
| `PUT …/5.0.0/w1-5.0.0.jar.sha1`（源缺） | **405** 同文案 | **404** `Target file to set checksum on doesn't exist: difftest-l040-mvn-virt:…`（repo 段=**虚拟 key**） | 差异（绕 405 出拦截文案） |
| `PUT …/3.0.0/w1-3.0.0.jar.md5`（值错） | **405** 同文案 | **409** `Checksum error for '…jar.md5': received '000…0' but actual is '…'`（relPath 无 repo 前缀） | 差异（绕 405 出 409） |
| 落地复验（成员 FileInfo oc / GET .sha1） | oc 仅 sha256 计算值（拒绝零副作用） | oc=计算 triple（**零落地**：409 未写穿、201 未注册）；`GET mvn-virt/…jar.sha1` = **200 计算值**（按需回显） | A 零副作用 vs B 零落地+读面按需 |
| `GET mvn-virt/…/3.0.0/w1-3.0.0.jar.sha1`（PUT 后） | **404** `Checksum not found for com/diff/l040/w1/3.0.0/w1-3.0.0.jar` | **200** cb8681…（=sha1(JAR3) 计算值） | **差异（面反转：B 算 A 不算）** |

**结论**：审 A 范围外①在 maven **未路由 virtual** 面**坐实且两面（B1/B2）同形**——putSidecar 的 svc.Get 读探使 sidecar PUT **绕过普通部署的 405**，对未路由 virtual 渲染 201/404/409 三态而**零落地**；普通 PUT 的 405 与 sidecar 的越权渲染并存=同 key 同路由双语义。A 权威：未路由 virtual 一切写（终缀与否）= 405 部署路由拒绝，无例外。

### 1b. 带 defaultDeploymentRepo 的 maven virtual

| 腿 | A（双轮） | B1=B2（双轮） | 判定 |
|---|---|---|---|
| `PUT mvn-virt2/…/1.0.0/w1-1.0.0.jar`（种子） | **201** envelope **repo=成员 key（difftest-l040-maven）**、Location→成员路径 | **201** envelope repo/Location=**虚拟仓**（存储实落成员，成员 FileInfo 200 在案） | 差异（渲染目标族=L039 arm1b generic 同形） |
| `PUT …/2.0.0/w1-2.0.0.jar.sha1`（源缺） | **404** `Target file … doesn't exist: difftest-l040-`**`maven`**`:…`（repo 段=**成员 key**——拦截穿透 virtual） | **404** 同文案但 repo 段=**difftest-l040-mvn-virt2**（虚拟 key） | 差异（同族异 repo 段） |
| `PUT …/1.0.0/w1-1.0.0.jar.sha1`（值对） | **201** CL0 空体、Location→**成员**源工件 | **201** CL0 空体、Location→**虚拟仓**源路径 | 差异（Location 目标） |
| `PUT …/1.0.0/w1-1.0.0.jar.md5`（值错） | **409** `Checksum error for '…jar.md5': …`（relPath 无 repo 前缀） | **409** 同文案**逐字** | **一致（status+文案）** |
| 409 后 `GET …jar.md5` | **200 = 000…0（写穿回显）** | **200 = 计算值** c4ffd…（**未写穿**） | **差异（写穿半面）** |
| `GET …jar.sha1`（值对后）/ FileInfo oc | 200=client 值；oc.sha1=client 值 | 200=计算值（值对腿 declared≡computed 不可分，写穿腿已证未注册）；oc=计算 | 差异（注册未达成员） |

**结论**：maven 已路由 virtual 面 B 的 putSidecar 走「成员 facts 探测+虚拟 key 渲染」：409 文案已逐字对齐 A，但 **SET 注册门（origLocal）未在虚拟寻址下放行**→写穿/注册半面全缺、404/201 的 repo 段指虚拟 key。= **generic/checksum-put-virtual-plane（C1）的 maven 平面实例**，且与 seam 的 origLocal 门同缝。

### 1c. maven remote 面

| 腿 | A（双轮） | B1=B2（双轮） | 判定 |
|---|---|---|---|
| `PUT mvn-remote/…/jar`（普通，对照） | **404** `Could not find a local repository named difftest-l040-mvn-remote to deploy to.` | **404** 同文案**逐字**（CT 差：A `application/json;charset=ISO-8859-1` vs B `application/json`） | **一致（文案族）** |
| `PUT mvn-remote/…/1.0.0/l040-1.0.0.jar.sha1`（未缓存写动词） | **404** 同文案（**无上游接触**：upstream.log 零命中；拒绝先于终缀解释与回源） | **404** 同文案（同样零上游接触） | **一致** |
| `PUT …/2.0.0/…jar.sha1`（GET 控制腿之后） | 404 同文案 | 404 同文案 | 一致 |
| `GET mvn-remote/…/2.0.0/l040-2.0.0.jar`（读控制） | 404，文案**包上游错误**（r1 `Failed retrieving resource from http://192.168.1.70:18155/a/gav/…: Connect timed out`；r2 `is assumed offline`——A 匿名回源+错误外显+离线态机） | **400** `Cannot fetch '…': upstream target refused — private or suppressed upstream`（私网上游守卫，预连接拒） | 差异（读面，已知族邻面） |
| `GET mvn-remote/…/1.0.0/l040-1.0.0.jar.sha1`（sidecar 读） | 404，文案包**对 .sha1 的上游拉取错误**（A 对 remote 面 sidecar GET **会回源取**） | **404** `Checksums are not downloadable.`（maven/handler.go:215 专门拦截，a-priori 拒绝） | **差异（新 facet）** |

**结论**：**审 A「remote 面读探=写动词触回源同族窗」在 maven 面被证伪**——B 的写拒绝（httpapi 上传引擎 `deployNoLocalRepoMessage`，T-553 码锚 router.go:2422）先于 putSidecar 读探，双端同形且零上游接触；「写动词拉穿」窗口在当前 develop 与载荷树上都不存在。副产：B 的 maven remote PUT 拒绝文案与 A **逐字一致**（见 Arm 3 C7 分解）。

---

## Arm 2（WP2）— C2/C5/C7 UNKNOWN 裁定输入

### C2 generic/checksum-get-virtual-ondemand — **A 模型修订（算法分面）**

| 腿 | A（双轮） | B1=B2（双轮） |
|---|---|---|
| `GET virt/c2/src.bin.sha256`（源在场·未设） | **200** a7366c…（**按需计算**） | 404 `Failed to find the requested resource '…src.bin.sha256'.` |
| `GET virt/c2/src.bin.sha1` / `.md5`（源在场·未设） | **404** `Checksum not found for c2/src.bin`（**不按需计算**） | 404 Failed-to-find（指终缀路径） |
| `GET virt/c2/never.txt.sha1`（源缺） | **404** `Could not find resource; Path: 'difftest-l040-virt:c2/never.txt'`（指源） | 404 Failed-to-find |
| 值对设后 GET（V 与 V2 两视图） | 200 = client 值 | 200 = 文件内容（sidecar 落实体文件后经 virtual 读面服务）；**成员面 GET 同路径 404**（B2 local GET 拦截剥缀查 overlay 未果→`File not found.; Path: '<repo>:c2/src.bin'`——实体 .sha1 文件被 GET 拦截遮蔽，B 自身两面不一致） |

**裁定输入**：A 的 virtual GET 按需计算**仅 sha256**（存储主摘要），sha1/md5 未设=404 双文案（源在场 `Checksum not found for <src>` / 源缺 `Could not find resource; Path:`）——L039 登记面「任意未设算法」**过宽，需修订**。B 实现成本下探：sha256 按需=blob 主摘要回显（无新计算链），主要成本在 miss 双文案与 virtual 读路由。分类建议维持 **UNKNOWN**（plane 语义+成本归 conductor）。

### C5 maven/metadata-checksum-dedicated-route — **A 模型扩展（范围钉死）**

| 腿 | A（双轮） | B1 | B2 |
|---|---|---|---|
| maven 仓 `PUT …/mdg/maven-metadata.xml.{sha1,md5}`（group 级，源在场，值对/值错） | **200** 空体（无 CT 无 Location） | 404 Target-file（源在场也 404） | 404 同 B1 |
| maven 仓 version 级（`mdg/1.0.0/…`）源在场/`mdg/2.0.0/…` 源缺 | **200** 空体（源缺=no-op 零副作用） | 404 / 404 | 404 / 404 |
| maven 仓 **根级** `maven-metadata.xml.sha1`（源在场）与 `maven-metadata.md5`（**非 .xml** 控制） | 根级 **200** 空体；`maven-metadata.md5` = **404** Target-file（**非 .xml 不入专用路由**） | 404 / 404 | 404 / 404 |
| **generic 仓** `PUT c5/maven-metadata.xml.sha1`（maven-metadata.xml 已作文件种子） | **200** 空体（**专用路由跨 packageType，按文件名键控**）；GET .sha1=200 计算值 | **201** envelope 普通部署（sidecar 落文件） | **201** CL0+Location→**源**（generic local 拦截臂把它当工件 sidecar 注册）——**三端三形** |
| maven 仓 GET 任意算法（含从未 PUT 的 .sha256）/ FileInfo 源 | GET=200 计算值；FileInfo=200 | GET=404 File-not-found；FileInfo=404 node not found | 同 B1 |

**裁定输入**：A 专用路由=**文件名键控（maven-metadata.xml.{sha1,md5,sha256}）、层级无关（根/group/version 同形）、源存在性无关（缺亦 no-op）、校验豁免、client 值零持久化、跨 packageType（generic 仓同样命中）**；非 .xml 的 `maven-metadata.*` 不入。B 的修复取舍=双 adapter 文件名豁免 + metadata 可见性 facet（FileInfo/拦截臂看不见 metadata 存储）——**架构裁定输入齐**。分类建议维持 **UNKNOWN**。

### C7 generic/remote-deploy-refusal-form — **族分解**

| 面 | A（双轮） | B1=B2（双轮） |
|---|---|---|
| generic 仓 PUT（普通/终缀，登记条目主面） | **404** `Could not find a local repository named <key> to deploy to.` | **405** `Remote repository '<key>' is a read-only proxy cache; …`（RE-05）——**条目 live 不变** |
| **maven 仓 PUT**（Arm 1c） | 404 同上文案 | **404 同文案逐字**（T-553 上传引擎拒绝）——**B 内部：maven 链 404-A 形 vs generic/docker/cargo/deb/rpm 链 405 read-only，两种拒绝并存** |
| DELETE（generic remote） | **404** `Artifact deletion error: Item difftest-l040-gen-remote/c7/r.txt does not exist` | **404** `Could not locate artifact. Path: '…/c7/r.txt'.`（借 maven .sha512 miss 文案族） |
| sidecar GET（generic remote，新 facet） | 回源拉 .sha1（错误外显族） | （maven 面实证 `Checksums are not downloadable.`；generic 面未单测——Risks） |
| 读控制 | 匿名回源+错误外显+离线态机（`is assumed offline`） | 私网上游守卫 400 |

**裁定输入**：C7 应**分面裁定**——(a) generic PUT 面 405-vs-404（登记条目，对齐=改用上传引擎的 `deployNoLocalRepoMessage`，目标文案 B 已有现成实现）；(b) maven PUT 面**已逐字一致**（无需动作，登记为已对齐面）；(c) DELETE 文案 facet（新）；(d) sidecar-GET-through-remote facet（新，A 回源 vs B 拒绝）。分类建议：登记条目维持 **UNKNOWN 待裁**，新 facet 待立案归并。

---

## Arm 3（WP3）— generic 面 sidecar 注册体超限（审 A NB-①，A 权威探限）

body=合法 hex 文本（'a'×N），每档单发；A 面 PUT 总量 1,000+1,024+1,025+4,000+64,000+1,000,000 = **1,070,249 B ≈ 1.02MB ≤ 2MB 预算**。

| 档 | A（双轮恒等） | B1 | B2 |
|---|---|---|---|
| 1000 B | **409** `Checksum error for 'sz/src.bin.sha1': received 'aaa…' but actual is '…'`（守卫放行→**参与比对**）；409 后 GET .sha1=**200 'aaa…'×1000**、FileInfo oc.sha1=**声明值全文**（**写穿**） | **201** envelope（全量落文件，GET=全文） | **409** Checksum error（比对；无守卫） |
| **1024 B**（边界腿，addendum） | **409 Checksum error**（守卫**放行**） | — | — |
| **1025 B**（边界腿，addendum） | **409** `Suspicious checksum file, content length of 1025 bytes is bigger than allowed.`（**守卫触发**，N=精确长度） | — | — |
| 4000 B / 64000 B | **409 Suspicious checksum file, content length of N bytes is bigger than allowed.` | 201（落文件） | 409 Checksum error（**读全量再比对，无守卫**） |
| 1,000,000 B | **传输层断连**（客户端 Broken pipe；服务端守卫拒绝后**中途断开连接**，Tomcat 式 post-error abort，响应不可观测） | 201（落 1MB 文件） | 409 Checksum error（读全量比对） |

**实证限值（修复票直接输入，非猜测）**：A generic 面 sidecar 注册体守卫=**1024 B 含端点**（≤1024 放行入比对、>1024 拒 409 Suspicious 文案与 B maven `maxSidecarBytes=1024`（put.go:503）**同界同文案族**）；**B generic 面在 develop 与载荷树均无守卫**（任意尺寸读全+比对/落文件）。修复=B generic 拦截臂补 1024B 守卫+Suspicious 文案（现成范式 maven 面）。A 1MB 档的断连形态如实记录（客户端视角无响应）。

---

## Arm 4（WP4）— 白名单复跑（L038 九腿 + srvgen-oc 第十面；B1/B2 双面）

| # | 面（L038/L039 编号） | A（双轮） | **B1 = d420bbc7**（双轮） | **B2 = 885b22e5 载荷树**（双轮） | B2 判定 |
|---|---|---|---|---|---|
| 1 | generic 值对 201 形态 | 201 CL0 无 CT、Location→**源** | 201 envelope+Location→.sha1 自身（**旧形仍在**） | 201 CL0 无 CT、Location→**源** | **collapse（逐字）** |
| 2 | generic 值错 409 | 409 Checksum-error | **201**（旧形仍在） | 409 同文案**逐字**（含 actual 值） | **collapse** |
| 3 | del3 计数 | deletedArtifactsCount=**3** | **4**（旧形仍在） | **3** | **collapse** |
| 4 | FileInfo oc 写穿 | oc.md5=**000…0** | 计算值（旧形仍在） | oc.md5=**000…0** | **collapse** |
| 5 | maven 409 后 GET 回显 | =**000…0** | =计算值（旧形仍在） | =**000…0**（409 文案两 B 面均已逐字） | **collapse** |
| 6 | nuget bare 201 CT+body envelope | CT `ItemCreated+json;charset=UTF-8` 全 envelope | CT 无 CL=0 纯头 201（旧形仍在） | CT `ItemCreated+json;charset=UTF-8` 全 envelope（结构与 A 一致；created 时间格式 +08:00 vs Z 属归一域） | **collapse** |
| 7 | nuget 409 文案族 | `Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected…ChecksumsInfo{…}`，CT `application/json;charset=ISO-8859-1` | `Checksum error for '<repo>/<path>': commit upload <session-id>…`，CT text/plain（**session 外泄旧形仍在**） | 与 A **逐字**（ChecksumsInfo 全族）+CT charset 同 | **collapse（含 L039 CT 微facet）** |
| 8a | （任务编号 #8）srvgen-oc | oc={md5:**000…0**, sha256:计算}（**无 sha1 键**） | 全计算 triple | 全计算 triple | **仍在（待 BIN-66）✓符合预期** |
| 8b | （L039 编号 #8）maven 非 GAV .sha512 | **201** 普通部署 | 400 layout | 400 layout | **仍在 ✓（C4 扩展提案悬置中）** |
| 9a | generic GET 未设值·源在场 | `Checksum not found for wl/other.txt` | `Failed to find…` | `File not found.; Path: '<repo>:wl/other.txt'`（指**源**了，但文案仍异） | **半 collapse**（状态+指向收齐，P8 双文案半面在） |
| 9b | （任务编号 #9 对应的保持项见 8b；本行为 L039 #9 源缺腿）generic GET 未设值·源缺 | `File not found.; Path: '<repo>:wl/never-seeded.txt'` | `Failed to find…` | `File not found.; Path: '<repo>:wl/never-seeded.txt'` **逐字** | **collapse**（get-unset 条目主形态腿收齐） |

**B1 结论**：**十面零 collapse**——全部旧形 live（因 R10 载荷不在 d420bbc7，见头部账实不符）。这不是逐腿回归，而是**票面前提被证伪**：票面预期「#1-#7 collapse」在字面钉住的 SHA 上不可能成立。
**B2 结论**：任务预期**全数兑现**——#1-#7 collapse（六腿逐字/结构一致）、#8 srvgen-oc 仍在（BIN-66）、保持项仍在；附加发现：get-unset 条目源缺腿已在 B2 收齐（该条目或可随 P8 双文案模型一并翻态，供 ledger 批参考）。
**编号注记**：任务书「#8 srvgen-oc / #9 保持」与 L038/L039 字面编号（#8=sha512 / #9=get-unset）不一致——上表双编号并列覆盖两种读法，无漏腿。

---

## 新差异候选 / 修订提案（分类建议全待 conductor/compatibility-engineer 裁定；INTENTIONAL 恒不越权）

| # | 候选面 | A 形（权威） | B 形（B1≡B2 除注明） | 建议分类 | raw 锚 |
|---|---|---|---|---|---|
| N1 | **maven 未路由 virtual 面 sidecar PUT 绕 405 假成功**（审 A 范围外①正面坐实） | 一切写=405 部署路由拒绝，sidecar 不豁免 | 普通 PUT 405 但 sidecar PUT 渲染 201（Location→虚拟源）/404（repo 段=虚拟 key）/409，**零落地零注册**；GET 读面按需计算 200（A 反而 404 Checksum-not-found） | **BUG 形**（C1=generic/checksum-put-virtual-plane 的 **maven 平面扩展提案**：拦截臂需先过部署路由门；已路由面=repo 段+写穿半面同缝） | `mv-unrouted-*` `mv2-*` |
| N2 | **remote 拒绝族分面**（C7 裁定输入） | 全 remote PUT=404 Could-not-find-local-repo；DELETE=`Artifact deletion error: Item … does not exist`；sidecar GET 会回源 | generic PUT=405 read-only（RE-05）；**maven PUT=404 与 A 逐字**（T-553 引擎文案）→B 内部双拒绝形并存；DELETE=`Could not locate artifact.`（借 maven miss 族）；sidecar GET=`Checksums are not downloadable.` | UNKNOWN（分面裁：generic PUT 对齐可复用现成 `deployNoLocalRepoMessage`；maven 面已一致；DELETE/sidecar-GET 两 facet 待立案） | `c7-*` `mr-*` |
| N3 | **C2 模型修订** | virtual GET 按需计算**仅 sha256**；sha1/md5 未设=404 `Checksum not found for <src>`；源缺=404 `Could not find resource; Path:` | 恒 404 Failed-to-find（两面同） | 修订既有条目 A 模型（登记面「任意算法」过宽）；成本下探=sha256 主摘要回显 | `c2-v-get-*` |
| N4 | **C5 范围模型** | 专用路由=文件名键控/层级无关/源无关/校验豁免/零持久化/**跨 packageType**；非 .xml 不入 | maven 仓全 404（metadata 不可见）；generic 仓 B1=普通部署、**B2=工件拦截注册**（三端三形） | 修订既有条目 A 模型（修复=文件名豁免+可见性 facet，架构裁定输入齐） | `c5-*` |
| N5 | **generic sidecar 守卫缺失**（审 A NB-①） | 1024B 含端点守卫：>1024 → 409 `Suspicious checksum file, content length of N bytes…`；≤1024 入比对（409+写穿）；1MB=断连 | generic 面**无守卫**（两面：B1 落文件/B2 读全量比对） | **BUG 形**（修复票输入：限值 1024 已实证钉死，勿猜） | `sz-*` `szb-*` |
| N6 | （注记）B2 local GET 拦截遮蔽实体 .sha1 文件 | — | virtual 普通部署落的 `x.sha1` 实体文件：virtual 读面 200 服务、**成员 local 面 GET 404**（剥缀查 overlay 未果）——同仓两读面不一致 | 注记（归 C1/C5 修复设计考量） | `c2-member-get-sha1` vs `c2-v-get-after-set` |
| N7 | （注记）A remote 离线态机 | 连续上游失败后 GET 文案切 `is assumed offline, '<repo>:<path>' is not found…`（r1 Connect-timed-out → r2 offline，状态性） | B 无对应态机（恒守卫 400） | 注记（A 行为规格面，供 remote 错误族条目参考；本批双轮漂移唯一来源） | `c7-get-read-control` r1/r2 |

**账实不符上报（最高优先）**：`origin/develop d420bbc7 ≠ R10 载荷`（证据三连：树内无 SetClientChecksums / b248be9d 非祖先 / merge 第二父=d9bae6b4 R9 战报）；R10 战报「PR #185 merged d420bbc7」与 git 图矛盾。白名单 #1-#7 的 collapse 只在 **B2（分支 tip 载荷树）**成立——ledger 翻态与「develop 已含修复」的任何表述须以此为条件，直至载荷真正合入 develop（或远端 develop 已前进而本地 ref 陈旧——本批网络不可达无法判别，**留 conductor 对账**）。

## NOT_RUN / BLOCKED 如实清单

- **A 侧上游计数**：A→fixture 上游不可达（Connect timed out，预检在案）——「remote 写动词是否回源」以 A 错误外显+upstream.log 零 /a/ 命中（双证据）替代判别；结论=拒绝先于回源，非 NOT_RUN 项；**但「A 侧成功回源的上游计数」形态未取样**（无法取样）。
- **GitHub 远端 develop 复验**：两次 fetch 均网络失败——B1=本地 tracking ref；远端是否已含载荷未知。
- **generic remote 面 sidecar GET**（C7 分面 d）：maven 面已单测，generic 面腿未设（Arm 4 预算外）——C7 裁定时若需 generic 面该腿，补探。
- **C3（终缀大小写）/ C6（policy 枚举）复验**：不在本批臂集（L039 已钉 A 形，B1/B2 相关域零改动推断不变——未实证，如实标注）。
- **mvn/wagon 真实客户端腿**：沿用 T-574/L039 缓期口径（SPI 缝+路由面落地后随全模型验证）。
- **B2 c2 成员 .sha1 文件节点 FileInfo 直查**：落地由 virtual GET 200 + local GET 遮蔽推断，直查腿未设。
- **shakedown 期观察**（不入定版腿集双轮）：reach.py 首跑上下文双拼撞「PUT 405 空 message」——与 L039 shakedown 同形，维持不立案。

## 环境处置

- A：`difftest-l040-*` 每轮脚本 teardown DELETE + 六轮 residual=[] + addendum 双轮 residual=[] + 收尾**独立**复验前缀计数=**0**（实例存量 `difftest-generic-local` 为前批遗留、l040 命名空间外未动）；A 配置零触碰；1MB 断连腿未留残片（404/409 家族均无落地）。
- B：四实例（18150/18152/18153/18154）每轮 SIGTERM graceful（`"binflow stopped"` ×4 在案）+端口释放复验（healthz 不可达 + lsof LISTEN 空）+pgrep 无残留；上游 fixture 服务器 SIGTERM + 18155 释放复验；两 detached worktree `git worktree remove` 完毕（worktree list 零 l040 项）。
- /tmp/l040：探针脚本+raw JSON×8+upstream.log+四 home（server.log）留存备查；dev 构建产物与 worktree 树已清（bin/ 二进制留存于 /tmp/l040/bin 备复跑）；凭据零落盘（全产物 grep 双口令零命中，退出码复核）。
