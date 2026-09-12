# 契约扩面启动评估——maven/npm 协议面（LOOP 011 评估票，2026-09-12）

- 票：LOOP 011 评估票（只评估不开工）。产出供 conductor 排程 LOOP 012+。
- 方法：照 docker（docker-remote.yaml 25 条目 + L000-L010 九轮差分）与 storage（storage-admin.yaml 10 条目 + L003/L005 两轮）两域已跑通的「证据→契约→差分→翻绿→matrix 行微分」模式，评估复用到 maven/npm 的排程与证据厚薄。
- 位置裁量：落 docs/compatibility/（compatibility-engineer owns；conductor 指令允许自选位置）。

## ① maven/npm 现有规格与差分证据盘点

### 规格（docs/reverse/）

| 协议 | 规格 | 厚薄评估 |
|---|---|---|
| npm 认证/会话族 | `npm.md`（87 行，K60 定案） | **最厚**。三源纪律示范级：官方=npm 10.9.8 客户端实物源码逐文件核对（npm 无 server-side 规范——客户端源码即规范）+ live 抓包（真客户端 × BinFlow，2026-08-31）+ DE 反编译补服务端盲区；置信度逐条分级（高=双证）。K60-6 不变量（login 永不 409）等定案可直接入契约。 |
| npm 协议主面 | `maven-npm-pypi.md` §2（约 90 行） | **中厚**。官方 [NPM-API] 锚：端点表（publish/packument/tarball/dist-tags/unpublish）、存储布局、PUT publish 校验顺序即错误顺序、metadata GET、remote tarball URL 重写、virtual 合并。置信度高（OSS/DE+官方双证）但无 live 层——**差分证据零**。§5 待验证两项挂账（unpublish rev-dance 隐式副作用、scoped %2f/%2F 等价性）。 |
| Maven 2 | `maven-npm-pypi.md` §1（约 100 行） | **中厚**。官方 [MVN-MD] 锚 + OSS 源码 + 一手 config 模板（RepoLayout 六字段非反编译）：端点表（传输共用存储命名空间 + calculateMetadata/generatePom/索引触发三 REST 端点，错误文案逐字）、snapshotVersionBehavior 三态、maven-metadata.xml 服务端计算（对官方规范的关键补充——§1.4 是本域最难资产）、checksum 伴随、virtual 合并（摘要级，正文在 repo-semantics §8.3）。§5-5 挂「unique snapshot buildNumber 条件分支建议 mvn deploy 对拍」。 |
| 其余协议规格 | helm/conan/cargo/debian/rpm/nuget/goproxy 等 | 后续波次素材，本票不评。 |

### matrix（D12 域相关行）

| 行 | 态 | 与扩面的关系 |
|---|---|---|
| D12-R03 npm 认证族 | ✅ high | K60 已实现已验（npm.md live 双向）——首份 npm 契约的天然第一群条目 |
| D12-R04 npm 包面（packument GET/PUT、unpublish） | ✅ high | 实现在案但**无差分背书**——契约化后差分可发现深水问题（§5 待验证两项正落此面） |
| D12-R05 npm dist-tags 族 | ◐ medium | **首票自然落点**：显式端点族缺位（部分语义经 packument PUT 旁路）；inv-2 §1.J 计 42 op 全集；V-7 挂「npm CLI 真实客户端依赖度未拍」 |
| D12-R08 Maven 元数据计算 | ◐ high | 次票落点：calculateMetadata/generatePom/索引触发——错误分支规格已逐字 |
| （maven 传输/布局/snapshot 主面） | 无独立行 | 制品传输共用存储命名空间（隐含在 D01-R09/R10/R11 generic 面内）——若契约化需**新行提案** |

### 差分证据

**零 formal 差分**：reports/compatibility/ 无 maven/npm 差分报告。现有证据形态为规格内嵌 live 取证（npm.md K60）与历代迭代 e2e（非双端对拍、无归一判定）。这是与 docker 域起点（L000 前夜）完全同构的位置——模式复用的前提成立。

## ② 两域模式复用的适配差异（协议面 vs REST 面）

1. **真实客户端腿是新增判据维度**。REST 域 curl 直发即可；协议域必须加 CLI 腿（docker 域 dind + docker 27.5.1 先例照搬）：npm=真 npm CLI（publish/unpublish/dist-tag/tag/ls——同时回答 V-7 依赖度问题）；maven=mvn deploy/dependency:get（snapshot 改写与 metadata 计算只有真实 resolver 腿能暴露）。**「客户端源码即规范」源纪律**：npm 无 server-side 规范，npm.md 已示范逐文件核对客户端源码的路径，契约断言可引客户端行为为锚。
2. **版本矩阵负担**：mvn 3.8/3.9 resolver 行为差、npm 10.x（legacy vs web login 已在 K60-4 定案不实现）——差分计划需钉版本（docker 域钉 27.5.1 同款）。
3. **参照写腿保护更重**：npm publish / mvn deploy 会落参照 filestore——沿 L003-1 纪律（专用一次性 repo + 用后 DELETE 200 清净复核），禁碰参照既有仓。
4. **归一化面更大**：maven metadata XML（lastUpdated 毫秒、buildNumber 随部署漂、checksum 伴随文件）与 npm packument（时间戳/etag/tarball URL 主机前缀、dist-tags 顺序）——fixtures/normalize.yaml 需扩协议规则组（docker 域 ETag/时间戳/随机 ID 先例可抄）。
5. **matrix 行微分照搬无碍**：以行态翻绿为闭环出口（R05/R08 两行 ◐ 是现成靶）；maven 主面需新行提案报 conductor（冻结纪律）。

## ③ 首票拆法建议（最小可闭环切片）

**首票 = npm dist-tags 显式端点族（D12-R05）**，四段闭环：

1. probe：参照 :8082 + BinFlow 双端拍 `npm dist-tag ls/add/rm` 真实 wire（npm CLI 源码锚 + 活体）——同时清偿 V-7（客户端是否走显式端点族）；
2. 契约：`contracts/npm-remote.yaml`（或 npm.yaml）首文件 4-6 条目（GET/PUT `/-/package/:pkg/dist-tags` + 版本 CRUD 子集；错误臂沿 errors 信封族）+ normalize 规则组；
3. 差分：curl 矩阵 + npm CLI 腿（双腿判定，docker L004 304 矩阵模式照搬）；
4. 翻绿：D12-R05 ◐→✅/收窄注记；D12-R03/R04 的既有面顺带契约化补链（K60 群直接从 npm.md 转写——最厚规格零补证成本）。

**次票 = Maven 元数据计算（D12-R08）**：纯 REST 面（POST /api/maven/...）无客户端腿负担、三错误文案已逐字——最快闭环，作为 maven 域契约首付与模式第二验证点。

**后续序（深水区递进）**：③ maven deploy/snapshotVersionBehavior（重客户端腿 + §1.3/§1.4 最难资产，mvn deploy 对拍）→ ④ npm publish/unpublish 深水区（§5 待验证 rev-dance 项在此清偿）→ ⑤ 两域 virtual 合并面（repo-semantics §8.3 摘要级规格需先升级）。

## ④ 排程建议（LOOP 012+ 与 R-10 批复的关系）

- **与 R-10 批复正交可并行**：R-10（invalid-value 等 product authority 包）走 PM/ADR 流程，maven/npm 扩面走差分工程线——域、owner、资源（参照实例读腿 vs 呈批流程）均不冲突。扩面不必等批复。
- **节奏**：LOOP 012 起**一 LOOP 一票**（首票 npm dist-tags；次票 maven 元数据计算可与 LOOP 012 池内既有序并存——评估为 P1 面不挤 P0 残余）。参照 docker 域十轮一域的密度经验，两域合计预估 6-8 LOOP 达到 docker 域同等契约覆盖。
- **前置依赖三项**（首票开工条件）：① conductor 派 probe 票授权参照写腿（一次性 npm repo + 清净复核纪律）；② normalize.yaml 协议规则组扩写（随首票）；③ maven 主面若契约化需新行提案（LOOP 013+ 再报）。
- **不依赖**：UAT 双端实例模式（:8082/:8083）现成；differential-qa-engineer 消费契约+fixtures 的接口不变。

---
*评估依据：docs/reverse/{npm.md, maven-npm-pypi.md, rest-compat-matrix.md §11.1/V-7}、docs/compatibility/{matrix.yaml D12 域, contracts/ 现有两域样本}、reports/compatibility/ 差分目录清点。本文件为评估产物非账目——无行态/条目变动。*
