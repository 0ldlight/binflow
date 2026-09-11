# L004-1 — §8-C 304 透传 + ping 三消息臂 差分复验报告（U-PROTO 面）

- Ticket: L004-1（differential-qa-engineer 差分出证腿；实现码 = conductor 收编的工作树，未提交，本报告只出证不改码）
- 日期: 2026-09-11/12
- 模式: **dual**（BinFlow UAT http://localhost:8083 vs 参照 :8082 Artifactory-pro 7.161.20）
- BinFlow 基线: **uat-l0041-worktree**（c81119b7 + 工作树；按 L003-3 指南重建，console/docs 产物未变仅重建 release；镜像纯净性：`go version -m` vcs.revision=c81119b7cd6f…、vcs.modified=true，且新消息字面量 `Props Authentication Token not found` / `Token failed verification: expired` 经 strings 在镜像二进制内在——工作树代码确在镜像内；version/healthz/readyz/ui 200 smoke 通过）
- 上游: 本地 registry:3（l0041-upstream，127.0.0.1:5591，前 agent 遗留容器复用，种子 l0041/busybox:{latest,t1}，manifest sha256:1cfa4e2b…、config c6348fa8…；判据 = 上游 access 行计数[combined-log 行，排除 registry:3 周期性 trace 噪声])
- 测试仓: 双端各 2 个——l0041-a-remote（默认检索窗 21600s，新窗口矩阵）、l0041-b-remote（retrievalCachePeriodSecs=**60** 双端配置键回读验证；过期窗矩阵）。**用后已删净**
- 客户端: 真实 docker CLI 27.5.1（一次性 dind difftest-dind，--insecure-registry 8082/8083/5591，login+pull 种缓存）+ curl 8.7.1（--noproxy；矩阵臂用 Bearer=各端 /v2/token 自签）
- 复跑门: 新窗口 304 矩阵双端各连跑两轮，逐臂 status/upstream_delta **完全稳定**（STABLE×2）
- 凭据: 命令行内使用，本报告脱敏；dind 随容器销毁；/tmp 内前 agent 遗留 token 文件已销毁

## 0. 结论速览

| 问题 | 答案 |
|---|---|
| E3-2「If-None-Match 恒不被消费、永不见 304」 | **推翻**（勘误成立）：活体参照对新窗口内副本，quoted/unquoted 匹配 etag 与 IMS 三种拼写均回 **304 本地应答（上游零往返）**；E3-2 的存活子集只有 {非匹配 INM、HEAD、过期窗重验证服务} 三族（§1） |
| §8-C 上游 304 透传（收编实现的核心声明） | **活体验证通过**：BinFlow 过期 tag 重验证以条件 GET（If-None-Match=digest etag）对上游 → 上游回 304（0 字节）→ 窗口滑动 → 客户端 200 全量。参照同臂为上游 HEAD 探查——客户端可见面 SAME，上游往返形状差异登记为 note（§3） |
| 新窗口 304 矩阵（18 臂） | **SAME 15 / DIVERGENT 3**：三个差异臂全部是 manifest 面 **unquoted INM** 拼写（M2/M2c/M2d）——参照按值不区分引号匹配（304），BinFlow 丢弃 unquoted 落日期臂（200/304 反转）（§2） |
| 过期窗矩阵（4 臂） | 客户端可见 **SAME 4/4**；上游计数 DIVERGENT 2 臂（X3/X4 blob：BinFlow 按 TTL 回源重取 vs 参照对过期 blob 本地应答零回源）（§3） |
| ping 三消息臂 | **三臂消息串逐字节一致**（Bad Credentials / Props Authentication Token not found / Token failed verification: expired），挑战头 `Basic realm="Artifactory Realm"` + charset CT 一致；第四臂 revoked：参照 `Token failed verification: revoked` vs BinFlow `Props Authentication Token not found`——**模型级差异坐实**（revoke=删行使 revoked 验定为 unknown）（§4） |

## 1. E3-2 勘误（双证并排）

### 1.1 原文（L000-docker-remote-evidence.md §3）

> **E3-2 | 客户端条件请求不被消费**：`If-None-Match`（带/不带引号、GET/HEAD）一律 `200` 全量回，永不见 304 | E4

（条件：docker.io 上游 hello-world 已缓存副本，2026-09-10 会话；E3-3 同节补充「304 只在上游回源时透传……客户端的 INM 头与该判定无关」。）

### 1.2 新捕获（:8082，2026-09-11，l0041-a-remote，registry:3 上游，副本在新窗口内；judged by 上游 access 计数）

| 臂 | 请求 | E3-2 预期 | 活体实测（:8082） | 上游往返 |
|---|---|---|---|---|
| M1 | GET tag + `If-None-Match: "<sha1>"`（带引号匹配） | 200 全量 | **304**（裸头：仅 Docker-Distribution-Api-Version） | **0** |
| M2 | GET tag + `If-None-Match: <sha1>`（不带引号匹配） | 200 全量 | **304**（裸头同上） | **0** |
| M1d/M2d | GET by digest 同两拼写 | 200 全量 | **304 / 304** | 0 |
| M3 | GET tag + `If-Modified-Since: <LM>` | 200 全量 | **304**（裸头） | 0 |
| M4/M4b | GET + 带引号**非匹配** etag（无/有 IMS） | 200 | 200 / 200（INM 在场即封死日期臂） | 0 |
| M2b/M2c | GET + 不带引号**非匹配** etag（无/有 IMS） | 200 | 200 / 200（unquoted 在场同样封死日期臂——**不是被丢弃**） | 0 |
| M5/M6 | 匹配 INM + 陈旧 IMS / unquoted 匹配 + IMS | 200 | 304 / 304 | 0 |
| M7 | HEAD + 带引号匹配 INM | 200 | **200**（HEAD 永不 304） | 0 |
| X1/X2 | 过期窗（TTL 60s 到期）tag GET（无/有客户端 INM） | —（E3-3 域） | 200 全量 + 上游 HEAD 探查 ×1（客户端条件不被消费） | 1（HEAD） |

**E3-2 勘误结论**：「恒不被消费/永不见 304」为伪——三种拼写（quoted 匹配、unquoted 匹配、IMS）在新窗口内均触发本地 304（上游零往返直证本地判定）。E3-2 的真子集仅为：**非匹配** INM（quoted/unquoted）→ 200、**HEAD** → 200、**过期窗重验证服务** → 200 全量。

### 1.3 为何 E3-2 当时的测法没观察到（条件差异解释）

E3-2 原始命令未随报告保存，无法回放确证；按本矩阵的臂间差异排序三种可行机理（按可能性）：

1. **校验值错配（最可能）**：E3-2 场景是 docker.io hello-world——tag 路径存 `list.manifest.json` 而 digest 路径存平台 `manifest.json`（E2-2/E2-3 已记），两文件 sha1（=Etag 值）不同；若回放时用了 digest 面捕获的 etag 打 tag 面（或用了上游 registry-1.docker.io 自己的 `"sha256:…"` etag 拼写，而 Artifactory 客户可见 Etag 是裸 sha1），则 INM 永远非匹配 → 恒 200——与本矩阵 M4/M2b（非匹配族）的 200 完全同象。
2. **实际探针窄于报告句**：报告句覆盖「带/不带引号 × GET/HEAD」，但若实测臂只覆盖了非匹配值与 HEAD（如用上游 etag 值或截断的 etag），quoted-匹配-GET 这一唯一 304 臂恰好缺席。
3. **过期窗服务**：若部分探针落在 6h 检索窗外（重验证服务臂，X1/X2 同象 200 全量）。

另：**E3-3 同时半勘误**——「304 只经上游回源透传」与活体不符：客户端 304 是本地判定（M1 上游零往返）；过期窗重验证的活体形状是**上游 HEAD 探查**（X1），反编译的 returnResponseGettingManifest 条件 GET→304 路径在活体流中未被走及（与收编实现 remote.go 注释的判断一致）。

### 1.4 过程缺口（为何错误存活到 VERIFIED）

L000-F 的 C08 复验**只测了 BinFlow 腿**（当时 BinFlow 未实现条件消费，恒 200），判定 SAME 锚在 E3-2 原文上而非参照新捕获——契约 `docker/remote-manifest-conditional-get` 以 VERIFIED/SAME 落账。本轮双端活体后该契约的 feature/expect/verdict 需重写（§5）。

## 2. 新窗口 304 矩阵（l0041-a-remote，18 臂，双端对照）

判据维度：status / 归一头集（normalize.yaml docker-remote 域：drop Date/instance/version；superset tolerate_and_note）/ body 指纹 / 上游 access 计数。两轮连跑稳定。

| 臂 | 条件 | 参照 :8082 | BinFlow :8083 | 判定 |
|---|---|---|---|---|
| M8 | GET 无条件（对照/取校验值） | 200/610B，头集=完整 artifact 面 | 200/610B，**body sha256 逐字节一致**，头集同构（+X-Binflow-Cache 超集） | SAME |
| M1 | GET + INM 带引号匹配 | 304 裸头 | 304 裸头（+X-Binflow-Cache: HIT 超集） | SAME |
| **M2** | GET + INM 不带引号匹配 | **304** | **200 全量** | **DIVERGENT** |
| M3 | GET + IMS(=LM) | 304 | 304 | SAME |
| M4 | GET + 带引号非匹配 | 200 | 200 | SAME |
| M4b | 带引号非匹配 + IMS 匹配 | 200（INM 封死日期臂） | 200 | SAME |
| M5 | 带引号匹配 + IMS 1970 | 304（etag 优先） | 304 | SAME |
| M6 | 不带引号匹配 + IMS 匹配 | 304 | 304（内部经丢弃→日期臂，**结果偶合**） | SAME |
| M2b | 不带引号非匹配 | 200 | 200（结果偶合） | SAME |
| **M2c** | 不带引号非匹配 + IMS 匹配 | **200**（unquoted 在场封死日期臂） | **304**（丢弃→日期臂判定） | **DIVERGENT** |
| M7 | HEAD + 带引号匹配 | 200（无体） | 200（无体） | SAME |
| M1d | by digest + 带引号匹配 | 304 | 304 | SAME |
| **M2d** | by digest + 不带引号匹配 | **304** | **200** | **DIVERGENT** |
| B7 | blob GET 对照（config 459B） | 200，完整 artifact 面 | 200，同构 | SAME |
| B1 | blob + INM 带引号匹配 | 304 **完整 artifact 面**（Etag/LM/校验和族/文件名对/Docker-Content-Digest/Accept-Ranges；无 CT/CL） | 304 同头集（无 CT——与服务端 304 语义一致） | SAME |
| B2 | blob + INM 不带引号匹配 | 304 | 304（quote 不敏感，双端同） | SAME |
| B3 | blob + IMS | 304 | 304 | SAME |
| B4 | blob + 带引号非匹配 | 200 | 200 | SAME |
| B5 | blob + 不带引号非匹配 + IMS | 200（INM 封死日期臂） | 200 | SAME |
| B6 | blob HEAD + 带引号匹配 | 200 | 200 | SAME |

**上游计数**：新窗口 18 臂双端全部 **0 往返**（304/200 均本地应答）。

**差异机理**（M2/M2c/M2d 三臂同根）：收编实现 `manifestClientNotModified` 只解析**带引号** entity-tag，unquoted 拼写解析为零标签被**丢弃**落日期臂；活体参照 manifest 面与 blob 面同构——按值**不区分引号**匹配，且在场 INM（任意拼写、匹配与否）封死日期臂。真实 docker/containerd/oras 客户端发送的是带引号 etag（RFC 9110 形态），故实际客户端影响面低，但矩阵口径为可观察面分歧。

## 3. 过期窗矩阵（l0041-b-remote，retrievalCachePeriodSecs=60 双端回读确认）

| 臂 | 场景 | 参照 :8082 | BinFlow :8083 | 客户端可见 | 上游计数 |
|---|---|---|---|---|---|
| SEED | tag 首取 | 200/610B；上游 **HEAD+GET ×2 次** | 200/610B；上游 GET ×1 | SAME | 2 vs 1（形状 note） |
| SEEDB | config blob 首取 | 200/459B；上游 **GET ×2 次**（双取） | 200/459B；GET ×1 | SAME | 2 vs 1（N2） |
| X1 | 过期 tag GET（无条件） | 200 全量；上游 **HEAD 探查 ×1**（HEAD 200 即续窗） | 200 全量；上游**条件 GET ×1 → 304（0 字节）**→窗口滑动重落 | **SAME** | 1 vs 1（形状 N1） |
| X2 | 过期 tag GET + 客户端 quoted INM | 200 全量（重验证服务不消费客户端条件） | 200 全量（同姿：cacheState≠HIT 不评估客户端条件） | **SAME** | 1 vs 1 |
| X3 | 过期 blob GET + 客户端 INM | **304 本地，上游 0**（过期 blob 不回源） | **304**，但**上游 GET 200/459B ×1**（TTL 到期重取后再 304） | **SAME**（304） | **0 vs 1 DIVERGENT** |
| X4 | 过期 blob GET（无条件） | 200 本地，上游 0 | 200，上游 GET ×1 | SAME（200） | **0 vs 1 DIVERGENT** |

- **§8-C 透传活体直证**：BinFlow X1/X2 上游 access 行 `"GET /v2/l0041/busybox/manifests/t1 HTTP/1.1" **304** 0`（UA binflow-remote/1.0）——上游条件 GET 304 → 窗口滑动 → 客户端 200 全量，与收编实现 `serveRevalidatedManifest` 声明一致；客户端条件在该臂不被消费（X2），与参照一致。
- **N1（形状 note，客户端不可见）**：过期 tag 重验证——BinFlow 条件 GET（etag=quoted digest）vs 参照 HEAD 探查；同为 1 往返 0 字节，tag 变更时 BinFlow 单往返拿到新内容（参照 HEAD 察觉变更后需二次 GET）。
- **X3/X4 差异**：BinFlow 对 blob 检索窗到期后按自身 retrievalCachePeriodSecs 回源重取；活体参照对已缓存 blob **不按该窗回源**（本地 200/304 应答，内容寻址不可变语义）。客户端可见面全同，上游行为分歧——对齐方向待裁（D3）。

## 4. ping 消息臂（/v2/ 根 ping，逐字对照）

| 臂 | 参照 :8082 | BinFlow :8083 | 判定 |
|---|---|---|---|
| P_anon 匿名（对照） | 401 + `WWW-Authenticate: Bearer realm="http://localhost:8082/v2/token",service="localhost:8082"` + compact UNAUTHORIZED + `application/json;charset=ISO-8859-1` + api-version 头 | 同构（realm/service 回显 8083） | SAME |
| P0d 有效 docker token | 200，**0 字节体，无 CT** | 200，`{}`（2B）+ CT application/json（超集 note N4） | SAME（status） |
| P0s 有效 security-API token | 200 | 200 `{}` | SAME |
| **P1 坏 Basic** | 401 + `Basic realm="Artifactory Realm"` + pretty `Bad Credentials` + charset CT | 401 + 同挑战头 + 同消息串 + 同 CT（+api-version 超集，L003 已 tolerated） | **SAME** |
| **P2 unknown bearer** | 401 + `Basic realm="Artifactory Realm"` + `Props Authentication Token not found` | 同头面 + **消息串逐字节一致** | **SAME** |
| **P3 expired bearer** | 401 + `Token failed verification: expired` | **消息串逐字节一致** | **SAME** |
| P4a 吊销前对照（有效） | 200 | 200 `{}` | SAME |
| **P4b revoked bearer** | 401 + `Basic realm="Artifactory Realm"` + **`Token failed verification: revoked`** | 401 + 同头面 + **`Props Authentication Token not found`** | **DIVERGENT（模型级，D2）** |

三臂消息串 byte-diff：`Bad Credentials` / `Props Authentication Token not found` / `Token failed verification: expired` **逐字节一致**；差异仅在 pretty JSON 方言（Jackson `"errors" : [ {` 折叠式 vs Go Encoder 展开式，80B vs 90B）——同一 writer 早于本票即此形态（VERIFIED 契约 docker/remote-v2-ping-bad-credentials 已接受该方言，N3 note）。

**D2 模型级差异登记（revoked 不可达）**：参照 token 吊销保留吊销态可验（第四臂消息独立）；BinFlow 吊销=删行， revoked 验定走 unknown 臂 → `Token failed verification: revoked` 在 BinFlow 模型下**不可达**（client 永远看到 Props 消息）。本轮以产品自身 revoke API（POST /api/security/token/revoke → 200）活体坐实：吊销前 200 → 吊销后 401 `Props Authentication Token not found`。

**参照侧取证副产品**（复现要点，供后人）：① /artifactory/api/security/token mint **必须带 scope**（`scope=applied-permissions/user`），否则 `invalid_request: Insufficient scope: ''`（空 bearer 会以 403 Forbidden 假象污染臂）；② 参照**只允许吊销剩余寿命 >6h（21600000ms）的 token**（`Token not revocable… revocableExpiryThresholdMillis`），过期臂用 expires_in=5、吊销臂用 86400；③ 吊销生效在 +0s 观察到一次 200 竞态（手工复核 +0s/+15s/+30s/+60s 四点均 401 revoked 稳定；报告以手工链为准并标注竞态一次）。

## 5. 差异分类建议 + 契约条目更新建议（归 L004-3 消费）

### 5.1 差异登记建议（known-divergence 候选；INTENTIONAL 终裁不在我权）

| id | 内容 | 建议 | 证据 |
|---|---|---|---|
| D1 `docker/remote-manifest-inm-unquoted` | manifest 面 unquoted INM：参照按值不区分引号匹配→304 且封死日期臂；BinFlow 丢弃落日期臂（M2/M2c/M2d 三臂反转） | **BUG 候选**（实现缺口：quotedEntityTags 只收带引号；真实客户端影响低——均发 quoted） | §2 |
| D2 `docker/remote-v2-ping-revoked-arm-unreachable` | 参照第四臂 `Token failed verification: revoked`；BinFlow revoke=删行 → 恒走 Props 消息 | **INTENTIONAL 候选（模型级）**，待裁；若裁对齐参照需引入吊销态可验模型（表结构+验定链改动） | §4 P4b |
| D3 `docker/remote-blob-expired-revalidation` | 过期 blob：BinFlow 按 TTL 回源重取（X3/X4 上游 +1）vs 参照本地应答零回源（内容寻址不可变语义） | **UNKNOWN 待裁**（客户端不可见；上游行为对齐方向需裁定：BinFlow 现状=兑现自家 retrievalCachePeriodSecs 承诺，参照=不回源） | §3 X3/X4 |

notes（不立差异）：N1 过期 tag 重验证上游形状（条件 GET→304 vs HEAD 探查；BinFlow 单往返优势）；N2 参照 blob 冷取双 GET（每 miss 2 次上游 GET）；N3 ping pretty 方言（既有 accepted 指纹）；N4 参照有效 ping 200 为 0 字节裸应答（反编译 E1-2 的 `200 {}` 不上线），BinFlow `{}`+CT 为超集。

### 5.2 契约条目建议（docs/compatibility/contracts/docker-remote.yaml，L004-3 执行）

1. **`docker/remote-manifest-conditional-get` 重写**（现 VERIFIED/SAME 建立于已证伪的 E3-2 前提）：feature 改「manifest 条件请求（新窗口：quoted/unquoted 匹配→304 裸头；IMS→304；非匹配 INM→200 且封死日期臂；HEAD 恒 200）」；expect 按 §2 矩阵；binflow_state → **DIVERGENT**（M2/M2c/M2d unquoted 三臂，detail 指向本报告 + D1）；status VERIFIED→IMPLEMENTED（待 D1 裁定/修复后复验翻绿）。
2. **新立 `docker/remote-blob-conditional-get`**：blob 面矩阵（quoted/unquoted→304 完整 artifact 面；IMS→304；非匹配封日期臂；HEAD 恒 200）——本轮双端全 SAME，建议 **VERIFIED**（evidence=本报告 §2）。
3. **新立 `docker/remote-manifest-revalidation-serve`**（过期 tag 臂）：上游往返 ×1 + 客户端 200 全量 + 客户端条件不被消费——双端 SAME（N1 形状 note 入 detail）；§8-C 透传的契约落点。
4. **新立或扩展 ping 消息契约**（`docker/remote-v2-ping-bad-credentials` → 泛化为 `docker/remote-v2-ping-refusal-messages`）：四臂消息矩阵（Basic/unknown/expired SAME；revoked=D2 模型级差异挂 known-divergence）。
5. **evidence 勘误指针**：L000-docker-remote-evidence.md E3-2/E3-3 加 errata 注记（superseded by 本报告 §1）——文本权归 compatibility-engineer。

## 6. 回归对照（对前轮结论）

| 前轮结论 | 本轮 | 处置 |
|---|---|---|
| E3-2「INM 恒 200 永不见 304」 | 推翻（三拼写 304 活体直证） | 勘误 §1；契约 5.2-1 重写 |
| E3-3「304 仅上游回源透传」 | 半推翻（客户端 304=本地判定；活体重验证=HEAD 探查非条件 GET） | 随 E3-2 勘误注记 |
| L000-F C08 复验「双端恒 200 SAME→VERIFIED」 | BinFlow 腿当时确为 200（未实现），参照腿未重测——账实缺口 | 契约翻 DIVERGENT（5.2-1） |
| L001-1 C02 / L003 ping Basic realm 对齐 | 复验维持（P1 逐字） | 无回归 |
| L004-2 负缓存各族（M-a/M-b/B1/B2） | 未触碰本票面（不同 repo/臂） | 无回归、无扩张 |
| 收编实现自测门（conductor 报全绿） | 活体双端 15/18+4/4+3/3 与其声明一致，唯 unquoted 三臂与 revoked 第四臂为已知模型面 | D1/D2 登记 |

## 7. 复现骨架（凭据脱敏；脚本=tools/difftest/l0041/）

```bash
# 上游：docker run -d --name l0041-upstream -p 5591:5000 registry:3（种子 l0041/busybox:t1）
# 仓：双端 PUT l0041-a-remote{url:http://host.docker.internal:5591} / l0041-b-remote{+retrievalCachePeriodSecs:60}
#     （Artifactory 键拼写=retrievalCachePeriodSecs；…Seconds 会被静默忽略）
# 种缓存：dind docker pull <host>:<port>/l0041-a-remote/l0041/busybox:t1（双端）
# 新窗口矩阵：tools/difftest/l0041/304-matrix.sh  HOST=… BASIC=… REPO=l0041-a-remote …
# 过期窗矩阵：tools/difftest/l0041/expired-matrix.sh REPO=l0041-b-remote …（内嵌 TTL sleep）
# ping 臂：tools/difftest/l0041/ping-arms.sh HOST=… APIBASE=/binflow|/artifactory …
# 上游计数：docker logs l0041-upstream | grep -E '^[0-9.]+ - - \['
```

### 环境清理记录（已执行）

- 仓：双端 l0041-a-remote / l0041-b-remote DELETE 各 200；回读列表零残留（含前 agent 遗留 audit-l0041-docker-remote 亦已删，6 artifacts）。
- 容器：difftest-dind、l0041-upstream 已 rm -f；docker ps -a 无 difftest/l0041 残留。
- 凭据落盘清理：/tmp/l0041/{a_tok,exp_tok,rev_tok,exp_tok_full.json,a_tok_*.h}（前 agent 遗留 token 材料）已销毁；本会话 token 仅存 shell 变量。
- UAT :8083：保留 uat-l0041-worktree 运行（后续差分可直接消费）；.env.uat 版本键已指向该镜像（gitignored）。
