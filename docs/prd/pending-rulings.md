# PRD — 用户/产品待裁清单（pending rulings）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/pending-rulings.md` |
| 版本 | v1.0（L002-4 首建：9 项收编——LOOP 001 差分/设计票上交 7 项 + gap 总账 2 项） |
| 维护者 | product-manager（唯写）；裁定结果回写本表 + 各关联账目，不另建副本 |
| 裁定通道 | 「用户」类经 conductor 转用户终裁；「产品」类 PM 域内可裁、报 conductor 备案；「ADR」类走 DECISIONS.md 流程 |

## 0. 纪律与图例

- **建议立场 = product-manager 的专业建议，不构成裁定**。每项待对应 authority 终裁后方可回写关联账目（known-divergence / matrix / gap 总账 / 契约）。
- 证据等级：E1=反编译走读（B=7.161.20 partial 源）/ E4=运行时实测（参照实例 Artifactory-pro 7.161.20 rev 86120900，:8082）/ E5=双系统差分（BinFlow UAT :8083；L001-1 复验基线 uat-l0011-f80c46aa，源 rev 59f33ab5）。
- authority 类型：**用户**（范围/安全/排程终裁）/ **产品**（PM 域内）/ **ADR**（架构记录，需先入 DECISIONS.md）。
- 来源账目：`docs/compatibility/known-divergence.yaml`、`docs/ai-engineering/loop-state.yaml`、`docs/ai-engineering/artifactory-binflow-gap.yaml`、`docs/design/storage-v2.md` §1/§5、`reports/compatibility/L000-docker-remote-*.md`、`reports/agents/T-L001-4.md`。

## 1. 总览表

| 编号 | 事项 | 当前 BinFlow 行为 | Artifactory 参照行为 | 建议立场（PM） | authority | 状态/限期 |
|---|---|---|---|---|---|---|
| R-1 | docker `/v2/token` 签发 TTL 与形态 | `{token(不透明 hex), access_token, expires_in:2592000, issued_at, scope}`（30 天超集） | `{token(JWT), expires_in:9000}`（2.5h，无 issued_at/access_token/scope 键） | **对齐 expires_in=9000**（灭 DIVERGENT 主因 + 安全收紧）；token 内形保留不透明（客户端不解码，吊销链不动），超集键保留 | 产品（TTL/键集）+ 用户（30 天→2.5h 的安全口径确认） | known-divergence UNKNOWN，LOOP 002 限期；契约 `docker/remote-token-issue` DIVERGENT 待翻 |
| R-2 | remote 面匿名默认姿态（`/v2/token` 无凭据） | 匿名开：200 签发匿名 pull scope token | 参照实例匿名关：401 `{"errors":[{"status":401,"message":"Authentication is required"}]}`（pretty） | **维持默认开 + 全局 `anonymous_access` 键管辖 remote 面**（与 local 面 v1.3/C6 同键同语义）；对拍以配置态收敛（参照实例切 anonymous 双态取证后锚契约），wire 默认差异登记 INTENTIONAL（部署策略差异非协议语义） | 产品 | known-divergence UNKNOWN，LOOP 002 限期；契约 `docker/remote-token-anonymous` DIVERGENT 待翻 |
| R-3 | SSRF 私网上游闸（allowPrivateUpstream） | 默认拒私网上游（per-repo 显式键放行，对拍环境即用此键） | 无此闸：remote 仓 url 可指私网 | **维持更严立场 → INTENTIONAL_DIFFERENCE**：安全（SSRF/横向移动面）优先于行为兼容；保留显式配置键（默认 false）服务内网级联场景；登记 ADR 为 authority | 用户（安全 vs 兼容终裁；建议已明确） | known-divergence UNKNOWN，LOOP 003 限期升级 |
| R-4 | HA 链范围 | 无（单节点；servelock，ADR-0002/0004） | HA 心跳/主选举/分布式锁/集群拓扑/任务单节点守卫（wire 协议 H1 UNKNOWN） | **两步走**：① HA-ready 无状态化审查近期做（不锁死、低成本）；② HA 本体最小集（锁/心跳/任务守卫）**进目标但自有形态**（NEW-BUILD 绿地、无 wire 对拍义务——HA 是部署形态非客户端协议），维持 PRODUCT M18+ 候排程，请用户确认排程与最小集边界 | 用户（范围+排程；PRODUCT 2026-09-06 翻案已把 HA 本体放进路线） | gap 总账 `ha-cluster` NEW-BUILD 候选；storage-v2 §1 #17 |
| R-5 | GCS/Azure 云 provider 是否进目标 | 无（S3 已立；binstore.yaml 保留名 `azure`/`gs` 拒启 + Backend 缝已埋，ADR-0036/0019） | GCS/Azure 模板族（google-storage-v2 / azure-blob-storage{,-v2,-archive} 等 8 模板） | **不进目标（维持 UNSUPPORTED 留缝）**：S3 协议是事实标准对象存储兼容面（GCS/Azure 均有 S3 兼容层）；单二进制 <40MB 成功标准与双 SDK 维护成本不支持；Backend 缝保留，强需求再启 | 用户（终裁；建议已明确） | storage-v2 §1 #15（UNSUPPORTED 留缝）；binary-provider-chain §3.2 |
| R-6 | 云重定向/预签名行为 | S3 读全代理（无重定向） | ≥200KB（`cloud.binary.provider.redirect.threshold.in.bytes` 默认 204800）且 provider 支持时下载 302 预签名 URL（`enableSignedUrlRedirect`）；URL 有效期 UNKNOWN | **证据依赖，暂不可裁**：MinIO 后端换 s3-storage-v3 链实测（PUT >200KB 对象 + curl -v GET 抓 302 Location/签名参数/有效期）后再裁——盲实现 = 猜测兼容行为（ADR-0001 红线）；若实现则 S3 Backend 加 presign GET + 阈值键 | 产品（取证后裁）；当前 evidence-blocked | storage-v2 §1 #18 UNKNOWN 挂账；unknown.yaml U-STG-15/U-STG-09 |
| R-7 | C05 remote 缓存树布局 | digest 寻址（`manifests/<hex>` + `blobs/<hex>`），无 tag 目录/marker/sha256__ 命名 | tag 目录 + `sha256__<digest>` 命名 + marker 文件驱动缓存 + `library/` 归一 | **INTENTIONAL 定谳**：客户端协议面零差异（缓存树是服务端内部布局）；BinFlow digest 寻址为存储契约（去重/数据完整性语义更优）；ADR-0047 已拒 marker 逐行翻译（docker_refs 账本等价能力）；唯一可见通道 = REST storage 浏览面（ListFolder），该面若有对拍需求另立票 | 产品（登记）+ 用户确认（仅当有 REST 浏览缓存树的使用场景） | known-divergence `docker/remote-cache-layout` 现 BUG，待按本裁定拆分转 INTENTIONAL；library/ 归一行为臂随 L001-1 已收敛（C12），docker.io 上游前缀行为 LOOP 002 契约重放补证 |
| R-8 | xray-curation REMOVE 候选 | 无（matrix D14 xray 行 ⛔ 登记口径） | Xray curation/apptrust 策略族（Xray addon 联动下游） | **REMOVE 定谳**：Xray 式扫描是 PRODUCT 2026-09-06 用户终裁唯一排除项；curation/apptrust 无 Xray 引擎即无语义，同判归 D14 ⛔ 族；正式登 known-divergence INTENTIONAL_DIFFERENCE（authority=PRODUCT 终裁 + 本裁定票），对应 REST 端点按 DE-16 式边界处理 | 用户（确认 curation/apptrust 归入 Xray 排除族——PRODUCT 终裁字面只点名「Xray 式扫描」，族边界需确认） | gap 总账 `xray-curation` REMOVE 候选；first_loop=开 INTENTIONAL 裁定票（即本项） |
| R-9 | plugins/worker SPI 是否入范围 | 无（matrix D10 ❌1；webhook outbox M13 已交付） | Groovy 用户插件（execution/steps/webhook 类型，进程内执行）+ worker TS serverless 扩展（116 java 模块）+ REST plugin/workers 资源 | **DEPRECATE（不进目标）**：进程内执行用户 Groovy = 供应链攻击面，与 BinFlow 单二进制/最小面立场冲突；等价扩展能力走 webhook + REST API 组合（M13 已交付）；登记 UNSUPPORTED_FEATURE；未来强迁移需求再评估受限插件沙箱另行立项 | 用户（终裁；建议已明确） | gap 总账 `plugins-worker-spi` DEPRECATE 候选；first_loop=开产品裁定票（即本项） |

## 2. 逐项详述

### R-1 docker token TTL/形态（C01b）

- **现状对比**：BinFlow `/v2/token` 200 body `{token, access_token, expires_in:2592000, issued_at, scope}`，token 为不透明 hex（M1 token 表 + 吊销链，M2 Q3 暂行 30 天）；Artifactory `{token, expires_in:9000}`，token 为 JWT（前缀 `eyJ2ZXIiOiIy` 即 access token JWT 形态），无 `issued_at` 键。
- **影响面**：客户端兼容低风险——docker CLI 实测登录/拉取均兼容（超集字段不破坏客户端，E5 C01b）；差分 DIVERGENT 的断言驱动 = `expires_in` 值与 `issued_at` 键缺席；TTL 30 天 vs 2.5h 是安全暴露面差异（bearer token 泄露窗口 ×288）与协商频率差异（9000s → daemon 周期性重协商，Artifactory 实况如此）。
- **建议分臂**：TTL 对齐 9000（消灭契约 DIVERGENT 主因，同时安全收紧）；token 内形保留不透明（客户端不解码 token 内容，BinFlow 吊销链 FR-11-AC6 是自有安全增益，无对照面）；`issued_at`/`access_token`/`scope` 超集键保留（无 Artifactory 断言排除它们，CLI 兼容实测）。
- **证据**：E4（L000-docker-remote-evidence.md E1-3）+ E5（L000-docker-remote-diff.md C01b，DIVERGENT/UNKNOWN）；known-divergence `docker/remote-token-response-shape`。

### R-2 remote 面匿名默认姿态（C03）

- **现状对比**：BinFlow remote 仓 `/v2/token` 无凭据请求 → 200 签发匿名 pull scope token（匿名开）；Artifactory 参照实例 → 401 `{"errors":[{"status":401,"message":"Authentication is required"}]}`（匿名关）。
- **定性**：**部署策略默认值差异，非协议语义差异**——Artifactory 支持匿名（全局配置项），本地面 BinFlow 已有定案（v1.3/C6：ping 恒挑战，`anonymous_access` 管匿名 token 是否发 pull scope）；待裁的是 remote 面是否同键管辖 + 默认值。
- **影响面**：匿名开 = remote 仓任何人可借上行带宽代理公网镜像（拉取放大面）；匿名关 = 匿名 pull 体验受损、偏离 Docker Hub/GHCR/Harbor「匿名可 pull」真实世界心智（M2 DE-01 同构论证）。
- **证据**：E4（E1-4）/ E5（C03）；known-divergence `docker/remote-anonymous-token-policy`（LOOP 002 限期）。

### R-3 SSRF 私网上游（allowPrivateUpstream）

- **现状对比**：BinFlow remote 仓上游 URL 指私网默认拒（per-repo `allowPrivateUpstream:true` 显式放行——L000-F/L001-1 对拍拓扑即用此键，键已存在且默认严）；Artifactory 无此闸（私网上游直接放行）。
- **影响面**：安全（SSRF：remote 仓 url 是服务端发起请求的输入，指向内网元数据/存储端点 = 横向移动面）vs 兼容（内网 Artifactory/MinIO 级联是正当场景——已由显式键覆盖，成本=配置一行）。
- **建议**：维持严立场，登记 INTENTIONAL_DIFFERENCE（authority=ADR）；对拍矩阵在 private-upstream case 标注配置前置，不视作 wire 差异。
- **证据**：E5（L000-docker-remote-diff.md 复验拓扑 `allowPrivateUpstream:true` 实证）；known-divergence `docker/ssrf-private-upstream-stricter`（LOOP 003 限期）。

### R-4 HA 链范围

- **现状对比**：BinFlow 单节点（servelock 单实例，ADR-0002/0004；元数据 SQLite/Postgres 双栈，Postgres 路径是 HA 前提）；Artifactory HA = 心跳/主选举/分布式锁（db/locks）/集群拓扑/混合许可降级/任务单节点执行守卫，wire 协议 H1 UNKNOWN（需双节点实验环境）。
- **范围基线**：PRODUCT.md 2026-09-06 终裁翻案已把「HA 本体」放进产品路线（M18+ 候排程）——本项**不是范围翻案票，是排程与最小集边界确认票**。
- **影响面**：架构级——文件存储层单写假设、GC/prune 任务 singleton、token/session 状态；storage-v2 §1 #17（GC cluster singleton 调度）随本项联裁。
- **建议**：HA 本体进目标、**自有形态**（HA 是部署形态非客户端协议，无 wire 对拍义务；管理 REST 面兼容义务已由矩阵覆盖）；最小集=分布式锁/心跳/任务单点守卫；前置=HA-ready 无状态化审查（架构票先行）；排程维持 M18+ 由用户确认。
- **证据**：gap 总账 `ha-cluster`（NEW-BUILD 候选）；storage-v2 §1 #17/§5；feature-catalog n134。

### R-5 GCS/Azure 云 provider

- **现状对比**：BinFlow S3 Backend 已立（含 MPU 新 wire ADR-0039），GCS/Azure 零实现但缝已埋（ADR-0019 Backend 接口 + ADR-0036 保留名 `azure`/`gs` 出现即拒启、文案点名保留位）；Artifactory 模板族含 google-storage-v2、azure-blob-storage{,-v2,-archive}、cluster 变体等 8 模板（binary-provider-chain §3.2，置信中）。
- **影响面**：部署矩阵广度（云原生目标用户的心智覆盖）vs 单二进制 <40MB（PRODUCT 成功标准；双 SDK 依赖链显著增重）vs 维护成本（两套 provider 行为面取证+契约）；S3 兼容层（GCS/Azure 均提供）覆盖大部分实际部署。
- **建议**：不进目标，维持 UNSUPPORTED 留缝；用户强需求信号出现再启（Backend 缝保证引擎零改）。
- **证据**：E1 静态（binary-provider-chain §3.2，jar 模板 + 官方 docs 互证，中置信）；storage-v2 §1 #15/§5。

### R-6 云重定向/预签名（证据依赖——暂不可裁）

- **现状对比**：BinFlow S3 读全代理（无重定向）；Artifactory 二进制 ≥200KB 且云 provider 支持时下载可 302 重定向至预签名 URL（`enableSignedUrlRedirect`；常量 `cloud.binary.provider.redirect.threshold.in.bytes` 默认 204800）——**URL 有效期与降级直读形态 UNKNOWN**（运行时未取证，参照实例未配云后端）。
- **硬依赖**：取证票 = 参照实例 binarystore.xml 换 `s3-storage-v3` 链指向本机 MinIO，PUT >200KB 对象后 `curl -v` GET 抓 302 Location 形态/签名参数/有效期 + 阈值上下行为（unknown.yaml **U-STG-15**（云链运行时 redirect/预签名，P1）/ **U-STG-09**（预签名 URL 生成规则，P2）、binary-provider-chain §8-4）。**取证前不裁**——盲实现违反 ADR-0001 clean-room 铁律（禁止猜测补齐兼容行为）。
- **建议**（取证后执行）：若取证证实 302/预签名形态，实现面 = S3 Backend presign GET + 200KB 阈值键 + `enableSignedUrlRedirect` 语义对齐；若取证显示形态不可稳定复刻，按 INTENTIONAL 登记（全代理是等价能力）。
- **证据**：E1 常量+docs（binary-provider-chain §4.4-C2，中置信，有效期 UNKNOWN）；storage-v2 §1 #18。

### R-7 C05 remote 缓存树布局

- **现状对比**：BinFlow remote 缓存 = digest 寻址（`<image>/manifests/<hex>` + `<image>/blobs/<hex>`，行带 sha1/sha2）；Artifactory = tag 目录（`library/hello-world/latest/list.manifest.json`）+ `sha256__<digest>` 文件命名 + marker 文件驱动缓存（manifest 下载预写 marker → blob 首取替换）。
- **已裁/已收敛的邻臂**：marker **门控语义**已由 ADR-0047 DigestChainGate 等价实现（docker_refs 账本 + node 短路，L002-1 实现中）——本项只剩**树布局形态**；`library/` 归一**行为臂**已随 L001-1 错误形态票收敛（C12 detail 键/静态文案 SAME），docker.io 上游的 library/ 前缀行为待 LOOP 002 契约重放补证（L002-2）。
- **影响面**：客户端协议面**零差异**（缓存树是服务端内部布局，docker/oci 客户端不感知）；唯一可见通道 = Artifactory 兼容 REST 的 storage 浏览面（ListFolder/item 路径形态）——若用户有「用 REST 浏览 remote 缓存树」的使用场景则该面有对拍需求，否则无。
- **建议**：INTENTIONAL 定谳（BinFlow digest 寻址为存储契约——去重/数据完整性语义更优，ADR-00047 拒逐行翻译的立场延伸）；known-divergence `docker/remote-cache-layout` 按「布局=INTENTIONAL（本裁定）/门控=ADR-0047/归一=行为票已收敛」拆分收口。
- **证据**：E5（L000-docker-remote-diff.md C05 DIVERGENT——布局异构，非阻断）+ E4/E1（evidence E2-2/E4-3）；L001-4 Compatibility 拆解留痕。

### R-8 xray-curation REMOVE 候选

- **现状对比**：BinFlow 无 Xray/curation/apptrust 任何面（matrix D14 xray 行 ⛔ 登记口径）；Artifactory curation 策略端点族与 apptrust 属 Xray addon 联动下游能力。
- **范围基线**：PRODUCT.md 2026-09-06 用户终裁「不做 Xray 式漏洞扫描/许可证合规平台（唯一维持排除项）」——**字面只点名 Xray 式扫描**；curation/apptrust 是否归入该排除族是本票要确认的族边界（gap 总账 first_loop 即「开 INTENTIONAL 裁定票」）。
- **影响面**：matrix D14 ⛔ 族行从缺口账移除/维持非目标标注；对应 REST 端点（curation 策略 CRUD 等）按 DE-16 式边界（404 + 域内错误体）处理，不实现。
- **建议**：REMOVE 定谳——curation/apptrust 无 Xray 引擎即无语义（策略执行依赖扫描结果），单独实现是空壳端点；登记 known-divergence INTENTIONAL_DIFFERENCE，authority=PRODUCT 终裁 + 本裁定。
- **证据**：gap 总账 `xray-curation`（REMOVE 候选，待产品终裁）；matrix D14 ⛔ 登记口径。

### R-9 plugins/worker SPI 范围

- **现状对比**：BinFlow 无用户插件/worker 面（matrix D10 ❌1）；扩展等价能力 = webhook outbox/事件总线（M13 FR-114/115 已交付）+ REST API。Artifactory = Groovy 用户插件（execution/steps/webhook 三类型，进程内执行）+ worker TS serverless 扩展（116 java 模块）+ REST plugin/workers 资源。
- **影响面**：迁移故事（重度定制 Artifactory 用户带 Groovy 插件资产迁移）vs 安全（仓库进程内执行用户代码 = 供应链攻击面，安全模型需先行）vs 成本（116 模块 worker 面整域绿地 + Groovy 运行时维护）。
- **建议**：DEPRECATE——不进目标；登记 UNSUPPORTED_FEATURE；等价扩展路径（webhook + REST）已交付；未来出现强迁移需求时评估受限 DSL/插件沙箱另行立项（届时新裁，不预留半成品缝）。
- **证据**：gap 总账 `plugins-worker-spi`（DEPRECATE 候选——安全与维护成本）；feature-catalog n139。

## 3. 裁定后回写路径（约定）

| 裁定项 | 回写目标 |
|---|---|
| R-1/R-2/R-3/R-7 | known-divergence.yaml 对应条目分类/authority + contracts/docker-remote.yaml 条目状态 + 本表状态列 |
| R-4/R-5/R-6 | gap 总账对应域 migration_verdict + storage-v2 §1 行（经 architect）+ 本表 |
| R-8/R-9 | known-divergence 新 INTENTIONAL/UNSUPPORTED 条目 + matrix D14/D10 族标注（经 compatibility-engineer）+ PRODUCT.md 范围演进记录（如需字面扩界）+ 本表 |

> 本表不替任何 authority 拍板；每项终裁后由 product-manager 在本表更新状态列并按上表路径派发回写票。
