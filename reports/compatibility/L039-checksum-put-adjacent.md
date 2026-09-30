# L039 — checksum-PUT 相邻面 A 权威探针批（T-580 / BIN-62）

- 日期：2026-09-30；模式 **dual**（A 活体双轮 + B 干净树活体双轮）
- A 面：JFrog Artifactory **7.161.26** Enterprise+ @ http://192.168.120.38:8082（当日 `/api/system/version` 复验 `"version":"7.161.26"`，revision 86126900——与 L037/L038 同锚）；凭据经 `/tmp/r3-difftest.env` source 注入（本批 `set -a` 导出，值零落盘零打印不进本报告）
- B 面：**origin/develop 6d10fd34** 干净树（worktree 并行在途改动隔离：`git archive origin/develop | tar -x -C /tmp/l039-src` 全新构建）；`go build -tags dev` → `/tmp/l039-b/binflow-server-dev`（L038 先例：dev tier 读数覆盖解锁 nuget 面，`GET /binflow/api/system/license` = `{"licensed":false,"tier":"pro"}` 双实例复验）；scratch `BINFLOW_HOME=/tmp/l039/home-r{1,2}`，listen 127.0.0.1:**18111**（r1）/ **18112**（r2）
- 命名空间：双端统一 `difftest-l039-*`（仓与路径均带前缀）；A 实例配置零触碰
- **双轮**：A r1≡r2（漂移 3 项全为 fixture 挥发：envelope 时间戳 ×1 + nupkg zip 内嵌 mtime→哈希 ×2，结构零漂移）；B r1≡r2（host:port/TS/HEX/UUID 归一后漂移 **0**）
- 探针资产（/tmp 留存备查）：`/tmp/l039/probe.py`（97 腿/轮：八臂主集+setup/teardown/residual）+ `/tmp/l039/addendum5.py`（臂5 语义收口 6 腿/轮）；raw：`/tmp/l039/probe-{a,b}-r{1,2}.json` + `/tmp/l039/add-{a,b}-r{1,2}.json`；shakedown 轮 `probe-a-r0-shake.json`（脚本定型前，仅取证 A 面 maven-none 405-empty 观察）
- 前序权威：L037 Arm 1（本地仓拦截族全谱=基线口径，本批零重复取证）；T-574（NOT_RUN 清单=臂4/5/6/7 目标）；L038（白名单九腿定义=臂8 标尺）

---

## Arm 1 — virtual 面 checksum PUT（R9 审 A NB-1）：PASS（A 权威全谱钉住；B 现状对照在案）

### 1a. 无 defaultDeploymentRepo 的 virtual（成员=同前缀 local）——双端逐字一致（PASS）

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `PUT virt/arm1/src.bin`（普通部署） | **405** `No local repository was configured as local deployment repository for the (difftest-l039-virt) virtual repository.` | **405** 同文案**逐字** | **一致**（B 的 C5/M52 措辞钉 live 证实） |
| `PUT virt/arm1/miss.txt.{sha1,md5,sha256}` | **405** 同文案（终缀不豁免部署路由失败） | **405** 同文案 | 一致（×3） |
| `GET virt/arm1/src.bin.sha1`（无源） | **404** `Could not find resource; Path: 'difftest-l039-virt:arm1/src.bin'`（**指源**） | **404** `Failed to find the requested resource 'difftest-l039-virt/arm1/src.bin.sha1'.`（指 .sha1 路径） | 差异（→C2 措辞facet） |

### 1b. 带 defaultDeploymentRepo=成员 local 的 virtual——A 权威口径（R9 审问正面）

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `PUT virt2/arm1/src.bin`（种子） | **201** envelope **repo=成员 key**，`Location`→**成员仓路径** | **201** envelope uri/`Location`→**虚拟仓路径**（存储实落成员：成员 FileInfo 200 在案） | 差异（→C1 渲染 facet） |
| `PUT virt2/arm1/miss.txt.{sha1,md5,sha256}`（源缺） | **404** `Target file to set checksum on doesn't exist: difftest-l039-generic:arm1/miss.txt`——**拦截穿 virtual，文案指成员仓 key** | **201** envelope（**普通部署**：sidecar 落实体文件节点） | **差异（→C1）** |
| `PUT virt2/arm1/src.bin.sha1`（值对） | **201** 空体 CT 无、`Location`→**成员仓源工件** | **201** envelope、`Location`→虚拟 .sha1 路径 | 差异（形态=已登记 generic 值对 family 的 virtual 面实例，→C1） |
| `PUT virt2/arm1/src.bin.md5`（值错） | **409** `Checksum error for 'arm1/src.bin.md5': received '000…0' but actual is '4a35…'`（路径无 repo 前缀，同本地面） | **201**（无校验，普通部署） | **差异（→C1，形态=值错 family）** |
| `GET virt2/arm1/src.bin.sha1`（已设） | **200** x-checksum，body=client 值 | **200** x-checksum，body=同字节（B=文件内容恰同值） | wire 一致（机制不同，注记） |
| `GET virt2/arm1/src.bin.sha256`（**未设**） | **200** x-checksum body=**计算值**——virtual 面**按需计算回显** | **404** `Failed to find the requested resource '…src.bin.sha256'.` | **差异（→C2）** |
| FileInfo（virt2 与成员两视图） | oc：sha1=client 值 / **md5=000…0（写穿）** / sha256=计算 | oc：全计算 triple（md5=4a35…） | 差异（=已登记 oc 写穿 family 的 virtual 面实例） |

**A 权威口径**：拦截族**穿透 virtual**——存在性检查、值校验、写穿全部按**解析后的部署目标成员仓**执行（404 文案 repo 段=成员 key；Location/envelope repo=成员）；GET sidecar 在 virtual 面**按需计算**未设算法（本地面 404 不生成，L037 Arm5 附注口径不变——面分 plane）。

**B 现状（码锚）**：`internal/adapter/generic/handler.go:126-139` 拦截臂以**被寻址 repoKey** 的 `Type == TypeLocal` 为门（virtual 不通过）→ 原链 `svc.Put`；`internal/repo/virtual.go resolveWriteRepo` 虚拟写路由到成员后跑**目标仓普通部署语义**（FR-21-AC4）→ sidecar 以**实体文件**落成员仓。remote/virtual 恒走原链的 T-574 勘误后门现状 live 证实。

---

## Arm 2 — 终缀大小写：A 面 PASS（B 对照差异在案）

| 腿 | A（双轮） | B（双轮） |
|---|---|---|
| `PUT arm2/lone.txt.SHA1 / .Md5 / .SHA256`（源缺，内容合法 hex） | **404** `Target file to set checksum on doesn't exist: difftest-l039-generic:arm2/lone.txt`——**大小写不敏感拦截**，×3 同文案 | **201** envelope 普通部署 ×3（大小写敏感家族） |
| `PUT arm2/lone.txt.sha1`（小写对照） | 404 同文案 | 404 同文案（逐字） |

**A 权威**：拦截族终缀匹配**大小写不敏感**（.SHA1/.Md5/.SHA256 全入族；源引用为原路径）。**B**：`strings.HasSuffix` 精确小写三键（handler.go terminalChecksumSuffixes）→ 大写变体漏拦截。**候选 C3（BUG 形）**。

---

## Arm 3 — 拦截排序/复合终缀：PASS（路由语义双端逐字吻合）

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| `PUT arm3/pkg.tar.sha1`（源 pkg.tar 缺） | **404** `Target file…doesn't exist: difftest-l039-generic:arm3/pkg.tar` | **404** 同文案**逐字** | **一致**（剥一缀到源，穿透 .tar 复合终缀） |
| 源在场+值对后 `GET arm3/pkg.tar.sha1` | 200 x-checksum body=client 值 | 200 x-checksum body=**同字节** | 一致（值面） |
| `PUT arm3/pkg.tar.sha1`（源在场，值对） | 201 空体、Location→**源** pkg.tar | 201 envelope、Location→pkg.tar.sha1 自身 | 差异（=已登记 generic 值对 201 形态 family，白名单确认不另立） |
| `PUT arm3/a..sha1`（双点，源=`arm3/a.`） | **404** 文案指 `arm3/a.`（尾点源名原样） | **404** 同文案**逐字** | **一致** |
| `PUT arm3/x.sha1/y`（目录段含终缀） | **201** envelope 普通部署 | **201** envelope 普通部署 | **一致**（仅**末段**终缀判定，目录段不触发） |

**排序结论（A 权威）**：终缀判定只看**路径末段**的尾部拼写；命中即走 client-checksum 写入解释（先于普通部署），未命中（含目录段拼写、`.sha1.bak` 类非终缀）即普通部署。maven 域「终缀路由先于 layout 400」已由 T-574 钉住，本臂不重复。

---

## Arm 4 — maven GAV `.sha512` PUT A 形直证（T-574 NOT_RUN①）：PASS（A 直证 + B 对照差异在案）

| 腿 | A（双轮） | B（双轮） |
|---|---|---|
| `PUT com/diff/l039/1.0.0/l037-1.0.0.jar.sha512`（GAV 合法、源 jar 缺、128-hex 体） | **201** ItemCreated envelope **普通部署**（mimeType octet-stream） | **404** `Could not locate artifact. Path: 'difftest-l039-maven/com/diff/l039/1.0.0/l037-1.0.0.jar'.`（**旧文案族 sidecar 源缺臂**——B 把 GAV .sha512 当 sidecar 写） |
| GET 同路径 | 200 octet-stream 128 字节原文 | 404 `File not found.; Path: '<repo>:….sha512'` |
| FileInfo 同路径 | 200（oc=server sha256） | 404 node not found |

**A 权威**：GAV `.sha512` 与非 GAV 同形——**不入拦截族，普通部署 201**（.sha512 域外性在 GAV 腿上同样成立）。**B**：GAV `.sha512` 走 putSidecar 旧文案臂（T-574 仅改 {sha1,md5,sha256} 措辞时按票面保留 .sha512 族外）。**候选 C4：既有条目 `maven/sha512-put-non-layout-deploy` 扩展提案**（非 GAV 腿 A=201/B=400 之外补 GAV 腿 A=201/B=404 旧文案）。

---

## Arm 5 — `maven-metadata.xml` 目标 sidecar（T-574 NOT_RUN②）：PASS（A 全模型钉住；B 对照差异在案）

A 面（generic-maven `difftest-l039-maven`，双轮 + addendum 双轮四腿收口）：

| 腿 | A（7.161.26 live，双轮恒等） |
|---|---|
| `PUT …/mdg/maven-metadata.xml`（种子 XML） | **201** envelope（普通部署入 metadata 通道；FileInfo 200 可见） |
| `PUT …/mdg/maven-metadata.xml.sha1`（源在场，值对） | **200**（**非 201**）空体无 CT——专用路由 |
| `PUT …/mdg/maven-metadata.xml.md5`（源在场，**值错** 000…0） | **200** 空体——**无 409 校验** |
| `PUT …/mdg2/maven-metadata.xml.sha1`（**源缺席**，值错） | **200** 空体；addendum 收口：随后 GET .sha1 / GET xml / FileInfo / FolderInfo **全 404**——**纯 no-op，零存储效果**（不建文件/节点/目录） |
| `GET …/mdg/maven-metadata.xml.sha1 / .md5`（PUT 后） | **200** x-checksum，body=**计算值**（sha1=9a9f…、md5=9397… 与本地计算逐一相符；错值 PUT 的 md5 也回计算值——**无写穿**） |
| `GET …/mdg/maven-metadata.xml.sha256`（**从未 PUT**） | **200** 计算值——**任意算法按需计算** |
| FileInfo `…/maven-metadata.xml` | 200；`originalChecksums` 仅 server sha256——**sidecar PUT 的 client 值不落 oc**（全 no-op 或仅回执） |

**A 权威模型**：metadata 目标的 checksum 终缀 PUT = 专用 metadata 路由——恒 200 空体、无源存在性检查、无值校验、无 client 值持久化、源缺亦零副作用；GET 侧恒按需计算。**与工件目标（Arm L037 全模型：404/201/409+写穿）完全两套语义。**

**B 现状**：三 PUT 腿全 **404** `Target file to set checksum on doesn't exist: difftest-l039-maven:…/maven-metadata.xml`——**源在场也 404**（B 的 metadata 存于 metadata 仓、拦截臂节点查询看不见 → 误报源缺）；GET sidecar 404 `File not found.; Path: '<repo>:…maven-metadata.xml.sha1'`（指终缀路径）；FileInfo metadata 源 **404 node not found**（PUT 201 却无文件节点）。**候选 C5（UNKNOWN，双 facet：专用路由缺失 + metadata 节点对 FileInfo/拦截臂不可见）**。

---

## Arm 6 — checksumPolicyType × sidecar 值错交互（T-574 NOT_RUN③）：PASS（A 全谱钉住）

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| 建仓 `checksumPolicyType:"none"` | **400** `No checksum policy type found for type: none`——**"none" 非合法枚举** | **200** Successfully created | **差异（→C6）** |
| 建仓 `"client-checksums"`（=未声明默认，cfg-read 复验 echo） | 200 | 200（cfg-read echo 同值） | 一致 |
| 建仓 `"server-generated-checksums"` | 200 | 200（cfg-read echo 同值） | 一致 |
| client-checksums 值错 `.md5` | **409** `Checksum error for 'com/diff/l039/pol/1.0.0/pol-1.0.0.jar.md5': received '000…0' but actual is 'b675…'` | **409** 同文案**逐字**（路径无 repo 前缀） | **一致**（T-574 修复面复绿） |
| client-checksums 409 后 GET `.md5` | 200 = **000…0（写穿回显）** | 200 = 计算值 b675… | 差异（已登记写穿 family 实例） |
| client-checksums FileInfo oc | md5=000…0（client 写穿） | md5=计算值 | 差异（同 family） |
| **server-generated-checksums** 值错 `.md5` | **201**（**免校验**；空体 Location→源） | **201**（免校验） | **一致（状态族）**——B 实现了策略门翻转 |
| srvgen GET `.md5` | 200 = **计算值** b675… | 200 = 计算值 b675… | **一致** |
| srvgen FileInfo oc | md5=**000…0（client 宣称值仍入 oc）**+sha256 计算（无 sha1 键） | 全计算 triple | 差异（oc 渲染 family + BIN-60 SPI 缝） |

**A 权威**：策略枚举={client-checksums（默认）, server-generated-checksums}，REST 面 `none` 拒收；client-checksums=校验+写穿；server-generated-checksums=**sidecar 免校验 201**，但 client 宣称值仍记入 originalChecksums，GET 面恒服务计算值。**B**：策略翻转（409→201）与 GET 面已对齐；oc 面随 standing family。**候选 C6（BUG 形）**：B 接受非法枚举 `none` 建仓（B 存储值未读回——Risks 注记）。

---

## Arm 7 — generic remote 仓 checksum PUT（T-574 NOT_RUN④）：PASS（A 权威钉住；B 对照差异在案）

A 面 remote `difftest-l039-remote`（url=self-loop → A 自身 difftest-l039-generic，建仓 200——A 允许自指 remote 建仓）：

| 腿 | A（双轮） | B（双轮，url=self-loop 指向 B 自身 local） |
|---|---|---|
| `PUT remote/arm7/r.txt.sha1`（终缀腿） | **404** `Could not find a local repository named difftest-l039-remote to deploy to.`——**无拦截文案**（终缀路由不先于 remote 部署拒绝） | **405** `Remote repository 'difftest-l039-remote' is a read-only proxy cache; deployments to remote repositories are not accepted.` |
| `PUT remote/arm7/r.txt`（普通对照） | **404** 同文案 | **405** 同 B 文案 |
| GET remote/arm1/src-direct.bin（fetch 控制） | 404 `Unauthorized; Path: 'difftest-l039-remote:arm1/src-direct.bin'`（自指匿名回源撞 auth 墙） | 400 `Cannot fetch …: upstream target refused — private or suppressed upstream`（B 私网上游守卫） |

**A 权威**：remote 面 PUT（终缀与否）= **404** `Could not find a local repository named <key> to deploy to.`——部署拒绝先于/压过一切终缀解释，**拦截族不在 remote 面运行**。**B**：405 read-only 语义（RE-05 码锚 `refuseNonLocalWrite`）——语义同向（都拒绝、都无拦截文案），**status+文案族分歧**。**候选 C7（UNKNOWN）**。B 的 TypeLocal 门在 remote 面零拦截文案 = T-574 勘误负测的 live 证实。fetch 控制腿两端因不同原因不可达（A=匿名 auth 墙 / B=私网守卫）——控制腿注记，不立候选（A 侧带凭据回源未测，凭据入 remote 配置触红线）。

---

## Arm 8 — L038 白名单九腿零回归复验：9/9 全部 live 确认（双轮同形，零回归）

| # | 白名单条目（L038 定义） | 本批 A（双轮） | 本批 B（6d10fd34 干净树，双轮） | 判定 |
|---|---|---|---|---|
| 1 | generic 值对 201 形态 | 201 空体（CT 无）+Location→**源** | 201 envelope（CT ItemCreated+json）+Location→**.sha1 自身** | 白名单确认（仍在） |
| 2 | generic 值错 409 | **409** `Checksum error for 'arm8/src.bin.md5'…`（无 repo 前缀） | **201** | 白名单确认 |
| 3 | del3 计数 3-vs-4 | deletedArtifactsCount=**3** | **4** | 白名单确认 |
| 4 | FileInfo originalChecksums 写穿 | oc.md5=**000…0** | oc.md5=计算值 4a35… | 白名单确认 |
| 5 | maven 409 后 GET 回显 | =**000…0**（40 hex） | =计算值 b675…（32 hex） | 白名单确认（BIN-60 SPI 缝） |
| 6 | nuget bare 201 CT+body envelope | CT `ItemCreated+json;charset=UTF-8` 全 envelope | **CT 无 CL=0 纯头 201** | 白名单确认 |
| 7 | nuget 409 文案族 | `Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected…ChecksumsInfo{…}`（CT application/json;charset=ISO-8859-1） | `Checksum error for '<repo>/<path>': commit upload <session-id>…checksum mismatch: sha256 received 000…0, actual …`（CT text/plain;charset=utf-8；session id r1≠r2 归一后同形） | 白名单确认（CT 微facet注记） |
| 8 | maven 非 GAV .sha512 201-vs-400 | **201** 普通部署 | **400** `maven layout: "arm8/foo/bar.txt": artifacts need <groupId path>/…` | 白名单确认 |
| 9 | generic GET 未设值 404 文案（源缺腿） | `File not found.; Path: 'difftest-l039-generic:arm8/never-seeded.txt'`（指源） | `Failed to find the requested resource 'difftest-l039-generic/arm8/never-seeded.txt.sha1'.`（指终缀路径） | 白名单确认 |

**附加 refinement（P8，供既有条目模型修订）**：源**在场**未设值时 A 有**第二种文案** `Checksum not found for arm8/other.txt`（无 repo 前缀、无 Path 结构）vs B 仍单一 `Failed to find…`——既有 `generic/checksum-get-unset-404-wording` 条目的 A 模型应改双态（按源存在性分文案）。

**R9 修复面复绿**（白名单外的 PASS 证据）：maven/generic 本地拦截族 404 文案（a2-ctrl、a3-两腿逐字）、maven 409 去 repo 前缀（w5/a6 逐字）、maven 非本地路由序（a3 复合终缀双端逐字）——全部按 T-574 宣称形态 live 复现，零回归。

---

## 新差异候选（本批提案，分类建议全待 conductor/compatibility-engineer 裁定；INTENTIONAL 恒不越权）

| # | 候选面 | A 形（权威） | B 形（6d10fd34 现状） | 建议分类 | raw 锚 |
|---|---|---|---|---|---|
| C1 | **virtual 面终缀拦截缺失+渲染目标**（R9 审 NB-1 正面） | 拦截穿 virtual：源缺 404 文案**指成员 key**；值对 201 空体 Location→成员源；值错 409；部署 envelope repo=**成员** | TypeLocal 门按被寻址 key 关闭→原链普通部署：源缺 201（sidecar 落实体文件）、值错 201、envelope/Location 指**虚拟仓**（存储实落成员，FR-21-AC4） | **BUG 形**（generic/checksum-terminal-suffix-put-routing 的 virtual 半面，同 SPI 缝叠加） | probe `a1b-v-*` |
| C2 | virtual GET sidecar 按需计算 | 未设算法 **200 计算值**；无可解析源 404 `Could not find resource; Path: '<virt>:<src>'` | 404 `Failed to find the requested resource '<virt>/<path>.sha256'.` | UNKNOWN（A-only 按需面；本地面 L037 已钉 A 也不生成——面分 plane，实现成本需裁定） | probe `a1b-v-get-unset-sha256` / `a1-v-get-sha1` |
| C3 | 终缀大小写不敏感 | .SHA1/.Md5/.SHA256 全入拦截族（404 同文案） | 精确小写三键，大写变体 201 普通部署 | **BUG 形**（拦截族成员拼写漏） | probe `a2-*` |
| C4 | maven GAV `.sha512`（既有条目扩展提案） | 201 普通部署（GET 200/FileInfo 200） | 404 旧文案 `Could not locate artifact. Path: '<repo>/<src>'.` | **BUG 形**（`maven/sha512-put-non-layout-deploy` 增 GAV 腿 A=201/B=404） | probe `a4-*` |
| C5 | metadata 目标 sidecar 专用路由 | 恒 200 空体/无校验/无写穿/源缺零副作用；GET 按需计算任意算法；FileInfo 200 | 404 拦截文案（源在场也 404）；GET 404；FileInfo metadata 源 404 node not found | UNKNOWN（A 专用 metadata 路由 vs B metadata 存储 invisible——新建路由或豁免拦截需架构裁定） | probe `a5-*` + add `x-*` |
| C6 | checksumPolicyType 枚举校验 | `none` 400 `No checksum policy type found for type: none` | `none` 建仓 200 | **BUG 形**（配置校验缺口） | probe `setup-create-difftest-l039-maven-none` |
| C7 | remote 面部署拒绝形 | **404** `Could not find a local repository named <key> to deploy to.`（终缀/普通同形，无拦截文案） | **405** read-only proxy cache 文案 | UNKNOWN（语义同向、status+文案族分歧；B 的 RE-05 为 as-built 声明面） | probe `a7-remote-*` |


**refinement/注记（不独立立案）**：P8=generic GET 未设值 A 双文案（源在场 `Checksum not found for <src>` / 源缺 `File not found.; Path:`）——修订既有条目 A 模型；w7 nuget 409 的 CT 面（A application/json;charset=ISO-8859-1 vs B text/plain;charset=utf-8）并入既有 nuget 409 文案族条目；FileInfo miss 措辞族（A 简短 vs B 带 node 细节）为横切已知风格面，本批不立案。

## NOT_RUN / BLOCKED 如实清单

- **mvn/wagon 真实客户端腿：NOT_RUN**（全臂为存储/API 面，python3 urllib 探针 curl 级覆盖；与 T-574 同一缓期口径——SPI 缝落地后随全模型验证）。
- **GAV `.sha512` 源在场腿：NOT_RUN**（臂问题「A 形」已由源缺腿+GET/FileInfo 钉住 201 普通部署；源在场值对/值错交互未取样）。
- **remote GET 回源控制腿：两端不可判**（A 匿名自指回源撞 auth 墙 404 Unauthorized；B 私网上游守卫 400）——控制腿注记，A 带凭据回源未测（凭据写入 remote 配置触红线）。
- **B `checksumPolicyType:"none"` 存储值读回：NOT_RUN**（脚本未在删除前 cfg-read 该仓——建仓 200 已足立案 C6，存储 echo 留待修复票复验）。
- **A PUT 不存在仓 key 形态**（405 空 message）：shakedown 轮单边观察（probe-a-r0-shake.json `a6-none-*`），未入定版腿集双轮复验——如实标注，不立案。
- cargo/五族 envelope 面：不在本批臂集（L038 候选⑤维持原状）。

## 环境处置

- A：全部 `difftest-l039-*` 仓逐轮脚本 teardown DELETE + 轮内 residual=[]（四主轮+四 addendum 轮）+ 收尾独立复验 `/api/repositories` 前缀计数=**0**；实例配置零触碰。
- B：18111/18112 双实例同规格清理（各轮 residual=[]）；SIGTERM graceful stop（两日志尾行 `"binflow stopped"` 在案）；端口释放复验（healthz=**000**×2，lsof LISTEN=空）；无残留进程（pgrep 空）。
- /tmp/l039：探针脚本+raw JSON+两 home（server.log/数据目录）留存备查；dev 构建产物未出 /tmp；凭据零落盘（全部产物无凭据字节；env 经 `set -a` 导出仅进进程环境）。
