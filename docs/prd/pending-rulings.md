# PRD — 用户/产品待裁清单（pending rulings）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/pending-rulings.md` |
| 版本 | v2.0（L008-4a 整编：新增 R-10 三臂 / R-11 / R-12 / R-13 / R-14 + 既有 9 项复核〔§2.0〕+ §4 批量批复式；v1.0=L002-4 首建 9 项） |
| 维护者 | product-manager（唯写）；裁定结果回写本表 + 各关联账目，不另建副本 |
| 裁定通道 | 「用户」类经 conductor 转用户终裁；「产品」类 PM 域内可裁、报 conductor 备案；「ADR」类走 DECISIONS.md 流程 |

## 0. 纪律与图例

- **建议立场 = product-manager 的专业建议，不构成裁定**。每项待对应 authority 终裁后方可回写关联账目（known-divergence / matrix / gap 总账 / 契约）。
- 证据等级：E1=反编译走读（B=7.161.20 partial 源）/ E4=运行时实测（参照实例 Artifactory-pro 7.161.20 rev 86120900，:8082）/ E5=双系统差分（BinFlow UAT :8083；L001-1 复验基线 uat-l0011-f80c46aa，源 rev 59f33ab5；L007-1 独立验证实例 binflow-l0071-verify / uat-l0071-c1193f5f）。
- authority 类型：**用户**（范围/安全/排程终裁）/ **产品**（PM 域内）/ **ADR**（架构记录，需先入 DECISIONS.md）。
- 来源账目：`docs/compatibility/known-divergence.yaml`、`docs/ai-engineering/loop-state.yaml`、`docs/ai-engineering/artifactory-binflow-gap.yaml`、`docs/design/storage-v2.md` §1/§5、`reports/compatibility/L000-docker-remote-*.md`、`reports/agents/T-L001-4.md`；v2 新增素材：`docs/prd/ruling-invalid-value-family.md`（R-10/R-11① 素材，architect 两案对比）、`docs/design/repo-update-merge.md`（ADR-0050 候选稿）、`reports/compatibility/L007-residuals-users-diff.md`（R-12 邻面 / R-13 / R-14）、`reports/compatibility/L003-s3-chain-evidence.md`（R-6 现状）。
- 批量批复式（v2）：见 §4——用户可逐项批，或整包「按建议执行」。

## 1. 总览表

| 编号 | 事项 | 当前 BinFlow 行为 | Artifactory 参照行为 | 建议立场（PM） | authority | 状态/限期 |
|---|---|---|---|---|---|---|
| R-1 | docker `/v2/token` 签发 TTL 与形态 | `{token(不透明 hex), access_token, expires_in:2592000, issued_at, scope}`（30 天超集） | `{token(JWT), expires_in:9000}`（2.5h，无 issued_at/access_token/scope 键） | **对齐 expires_in=9000**（灭 DIVERGENT 主因 + 安全收紧）；token 内形保留不透明（客户端不解码，吊销链不动），超集键保留 | 产品（TTL/键集）+ 用户（30 天→2.5h 的安全口径确认） | known-divergence UNKNOWN，LOOP 002 限期**已过未升级**——随 v2 呈批；契约 `docker/remote-token-issue` DIVERGENT 待翻 |
| R-2 | remote 面匿名默认姿态（`/v2/token` 无凭据） | 匿名开：200 签发匿名 pull scope token | 参照实例匿名关：401 `{"errors":[{"status":401,"message":"Authentication is required"}]}`（pretty） | **维持默认开 + 全局 `anonymous_access` 键管辖 remote 面**（与 local 面 v1.3/C6 同键同语义）；对拍以配置态收敛（参照实例切 anonymous 双态取证后锚契约），wire 默认差异登记 INTENTIONAL（部署策略差异非协议语义） | 产品 | known-divergence UNKNOWN，LOOP 002 限期**已过未升级**——随 v2 呈批；契约 `docker/remote-token-anonymous` DIVERGENT 待翻 |
| R-3 | SSRF 私网上游闸（allowPrivateUpstream） | 默认拒私网上游（per-repo 显式键放行，对拍环境即用此键） | 无此闸：remote 仓 url 可指私网 | **维持更严立场 → INTENTIONAL_DIFFERENCE**：安全（SSRF/横向移动面）优先于行为兼容；保留显式配置键（默认 false）服务内网级联场景；登记 ADR 为 authority | 用户（安全 vs 兼容终裁；建议已明确） | known-divergence UNKNOWN，LOOP 003 限期**已过未升级**——随 v2 呈批 |
| R-4 | HA 链范围 | 无（单节点；servelock，ADR-0002/0004） | HA 心跳/主选举/分布式锁/集群拓扑/任务单节点守卫（wire 协议 H1 UNKNOWN） | **两步走**：① HA-ready 无状态化审查近期做（不锁死、低成本）；② HA 本体最小集（锁/心跳/任务守卫）**进目标但自有形态**（NEW-BUILD 绿地、无 wire 对拍义务——HA 是部署形态非客户端协议），维持 PRODUCT M18+ 候排程，请用户确认排程与最小集边界 | 用户（范围+排程；PRODUCT 2026-09-06 翻案已把 HA 本体放进路线） | gap 总账 `ha-cluster` NEW-BUILD 候选；storage-v2 §1 #17 |
| R-5 | GCS/Azure 云 provider 是否进目标 | 无（S3 已立；binstore.yaml 保留名 `azure`/`gs` 拒启 + Backend 缝已埋，ADR-0036/0019） | GCS/Azure 模板族（google-storage-v2 / azure-blob-storage{,-v2,-archive} 等 8 模板） | **不进目标（维持 UNSUPPORTED 留缝）**：S3 协议是事实标准对象存储兼容面（GCS/Azure 均有 S3 兼容层）；单二进制 <40MB 成功标准与双 SDK 维护成本不支持；Backend 缝保留，强需求再启 | 用户（终裁；建议已明确） | storage-v2 §1 #15（UNSUPPORTED 留缝）；binary-provider-chain §3.2 |
| R-6 | 云重定向/预签名行为 | S3 读全代理（无重定向） | ≥200KB（`cloud.binary.provider.redirect.threshold.in.bytes` 默认 204800）且 provider 支持且 `enableSignedUrlRedirect=true` 时下载 302 预签名 URL；URL 有效期 UNKNOWN | **证据依赖，暂不可裁**（维持 evidence-blocked）：L003 三次换链取证均 BLOCKED（TLS 开关真形态未定位，三候选在案，见 §2-R6 增补）；静态面已收（E1 字节码：redirect 默认关 + signedUrlExpirySeconds 键族 + 阈值 204800）；取证前盲裁 = 违反 ADR-0001 | 产品（取证后裁）；当前 evidence-blocked | storage-v2 §1 #18 UNKNOWN 挂账；unknown.yaml U-STG-15/U-STG-09；L003-3b-final 终态 BLOCKED |
| R-7 | C05 remote 缓存树布局 | digest 寻址（`manifests/<hex>` + `blobs/<hex>`），无 tag 目录/marker/sha256__ 命名 | tag 目录 + `sha256__<digest>` 命名 + marker 文件驱动缓存 + `library/` 归一 | **INTENTIONAL 定谳**：客户端协议面零差异（缓存树是服务端内部布局）；BinFlow digest 寻址为存储契约（去重/数据完整性语义更优）；ADR-0047 已拒 marker 逐行翻译（docker_refs 账本等价能力）；唯一可见通道 = REST storage 浏览面（ListFolder），该面若有对拍需求另立票 | 产品（登记）+ 用户确认（仅当有 REST 浏览缓存树的使用场景） | known-divergence `docker/remote-cache-layout` 仍 BUG 待按裁定拆分转 INTENTIONAL；library/ 归一行为臂随 L001-1 已收敛（C12），docker.io 上游前缀行为 LOOP 002 契约重放补证 |
| R-8 | xray-curation REMOVE 候选 | 无（matrix D14 xray 行 ⛔ 登记口径） | Xray curation/apptrust 策略族（Xray addon 联动下游） | **REMOVE 定谳**：Xray 式扫描是 PRODUCT 2026-09-06 用户终裁唯一排除项；curation/apptrust 无 Xray 引擎即无语义，同判归 D14 ⛔ 族；正式登 known-divergence INTENTIONAL_DIFFERENCE（authority=PRODUCT 终裁 + 本裁定票），对应 REST 端点按 DE-16 式边界处理 | 用户（确认 curation/apptrust 归入 Xray 排除族——PRODUCT 终裁字面只点名「Xray 式扫描」，族边界需确认） | gap 总账 `xray-curation` REMOVE 候选；first_loop=开 INTENTIONAL 裁定票（即本项） |
| R-9 | plugins/worker SPI 是否入范围 | 无（matrix D10 ❌1；webhook outbox M13 已交付） | Groovy 用户插件（execution/steps/webhook 类型，进程内执行）+ worker TS serverless 扩展（116 java 模块）+ REST plugin/workers 资源 | **DEPRECATE（不进目标）**：进程内执行用户 Groovy = 供应链攻击面，与 BinFlow 单二进制/最小面立场冲突；等价扩展能力走 webhook + REST API 组合（M13 已交付）；登记 UNSUPPORTED_FEATURE；未来强迁移需求再评估受限插件沙箱另行立项 | 用户（终裁；建议已明确） | gap 总账 `plugins-worker-spi` DEPRECATE 候选；first_loop=开产品裁定票（即本项） |
| R-10a | 仓配置非法值·mistyped 数字（`maxUniqueSnapshots:"seven"`） | 400（decode 报文带字段名——全域统一 decode 姿态，非本域特例） | 500 `Error converting from 'String' to 'Integer' For input string: "seven"`（Java 转换器逐字） | **维持 400，登记 INTENTIONAL**：500 系参照异常泄漏非契约设计；复刻 500 诱发规范客户端重试确定性错误（RFC 9110 §15.5/15.6），逐字文案耦合 JDK 实现细节（clean-room 边缘） | 产品（+ADR 登记） | known-divergence `rest/repo-config-invalid-value-family` UNKNOWN，LOOP 008 限期（本票呈批） |
| R-10b | 仓配置非法值·宽容布尔（`blackedOut:"yes"`） | 400（严格 bool） | 接受并转换（commons-lang 真假值词表 {true,on,yes,y,t,1}/{false,off,no,n,f,0} 落库回显；不可转 "maybe" 400） | **维持 400，登记 INTENTIONAL**：宽容转换静默吞 typo 反噬可调试性；受害脚本形态（字符串填 bool 席位）罕见；折衷备选臂（仅收 "true"/"false" 字符串）列备选不预裁 | 产品（+ADR 登记） | 同 R-10a |
| R-10c | 仓配置非法值·未知布局名（`repoLayoutRef:"no-such-layout"`） | 接受并存读（无布局注册表；K73 已定谳布局 presentation-only） | 400 `Unable to find repository layout by the name: <n>`（create/update 双面逐字） | **对齐 400**：按官方 xsd/文档 26 内置布局名冻结名单做存在性校验（名单源 clean-room，实现票内冻结）；灭一个真实客户端可见分歧（typo 布局名在 BinFlow 静默存成无效配置）；自定义布局名迁移臂二选一归 R-11① | 产品（裁对齐则开实现票） | 同 R-10a；matrix D02 行 3/4 note 联动 |
| R-11 | update-merge 邻臂两件：① 自定义布局名迁移臂 ② PUT=更新超集回撤确认 | ① 未定（随 R-10c 联裁） ② PUT-on-existing=更新 200（BinFlow 自有超集行为） | ① 自定义布局名有效（Artifactory 支持自定义布局，迁来 configJSON 可引用） ② PUT-on-existing=400 create-only（参照更新拼写只有 POST） | ① **名单外一律 400**（BinFlow 无自定义布局能力，白名单放行即造无效配置；「白名单放行+WARN」为备选臂） ② **确认回撤**（ADR-0050 案 A 定谳方向：POST=merge 三列矩阵+PUT=create-only；BinFlow UAT 前夜无外部存量承诺，breaking 窗口现在最便宜；release note breaking changes 首条+无双轨期） | ① 产品 ② 用户（breaking 窗口确认）+ ADR-0050（随 L008-1 入册） | known-divergence `rest/repo-config-update-merge-semantics` BUG；ADR 候选稿 docs/design/repo-update-merge.md |
| R-12 | 经典 security 读族 GET 管理面门（users/groups/permissions 列表与详情） | readonly_admin 可读（CapSecurityRead：admin ∨ readonly_admin） | 参照实例级无 read-only admin（经典读族 admin-only；最接近物=project 域 Viewer） | **维持 CapSecurityRead 超集 → INTENTIONAL**：角色本身系 ADR-0026 决策 1 既有架构位（readonly_admin 管理面含 security:read），终裁时补引 ADR-0026 即转正；参照无此主体，参照存在主体面（admin 200/user 403/匿名 401）零差；收窄 = 翻转既有 readonly_admin 200→403 组合，冲 M9「新增不破坏」基调并伤 console 只读视图 | 产品（补引 ADR-0026 决策 1）+ 用户确认 | known-divergence `rest/security-read-family-readonly-admin-gate` UNKNOWN，LOOP 008 限期（本票呈批） |
| R-13 | 建用户自动入默认组 readers（建模级） | 无此语义（groups:[]） | 建用户自动入组（回显 groups:["readers"]；组权限绑定随实例预置） | **对齐（引入默认组语义）**：参照生态默认权限面是真实迁移依赖（缺位 = 迁移用户静默丢默认权限心智）；实施留建模票（预置组行+建用户入组+回显；组的权限绑定默认零授权，与参照预置绑定的差异随建模票取证定） | 产品（裁对齐则开建模+实现票） | known-divergence `rest/user-create-default-group-readers` UNKNOWN，LOOP 008 限期（本票呈批） |
| R-14 | anonymous 用户行（GET /api/security/users/anonymous，建模级） | 无该行（404） | 有（200；profileUpdatable=false、internalPasswordDisabled=true，字段集同 L007-1 §2 三形态活体取证） | **对齐（只读虚拟行最小面）**：渲染层固定行（不进 DB、不可编辑、不可作为认证身份）；写面（PUT/DELETE 该行）参照行为未取证——实现票内先补取证或按只读拒写登记未取证臂 | 产品（裁对齐则开建模+实现票） | known-divergence `rest/anonymous-user-row` UNKNOWN，LOOP 008 限期（本票呈批） |

## 2. 逐项详述

### 2.0 既有 9 项复核记录（v2，L008-4a）

- **台账态核对（known-divergence 现值）**：R-1/R-2/R-3 对应条目仍 UNKNOWN（LOOP 002/002/003 限期均过未升级）；R-7 `docker/remote-cache-layout` 仍 BUG（待终裁后拆分转 INTENTIONAL）；R-4/R-5/R-8/R-9 关联 gap 总账候选行不变。逾期事实不改变建议立场，随 v2 一并呈批。
- **D2（L004 分歧号 2，known-divergence `docker/remote-v2-ping-revoked-arm-unreachable`）：仍未裁**——UNKNOWN，authority=pending（token 吊销模型：删行 vs 吊销态可验）。P1~P3 三臂消息已逐字节一致（L004-1 分型落地+差分复验），分歧仅 revoked 第四臂：BinFlow revoke=删行模型下该臂不可达。PM 建议：与 R-1 同属 token 模型面，**随 R-1 终裁联裁**（R-1 建议立场「吊销链不动」即 D2 的 INTENTIONAL 候选形态——删行模型维持，第四臂按模型级 INTENTIONAL 登记）；呈批编排归 conductor。
- **D3（L004 分歧号 3，known-divergence `docker/remote-blob-expired-revalidation`）：已终裁**——conductor LOOP 007 终裁（2026-09-12）**BUG 对齐收**（已验证 blob 豁免检索窗重取；参照语义=manifest 可变恒重验/blob 内容寻址豁免），L008-2 小票执行中。非用户裁定项，本表不收编，仅留状态注记。
- **R-6 取证现状更新**：见 §2 R-6 末「v2 增补」段（L003 三次换链 BLOCKED + TLS 开关三候选）。

### R-1 docker token TTL/形态（C01b）

- **现状对比**：BinFlow `/v2/token` 200 body `{token, access_token, expires_in:2592000, issued_at, scope}`，token 为不透明 hex（M1 token 表 + 吊销链，M2 Q3 暂行 30 天）；Artifactory `{token, expires_in:9000}`，token 为 JWT（前缀 `eyJ2ZXIiOiIy` 即 access token JWT 形态），无 `issued_at` 键。
- **影响面**：客户端兼容低风险——docker CLI 实测登录/拉取均兼容（超集字段不破坏客户端，E5 C01b）；差分 DIVERGENT 的断言驱动 = `expires_in` 值与 `issued_at` 键缺席；TTL 30 天 vs 2.5h 是安全暴露面差异（bearer token 泄露窗口 ×288）与协商频率差异（9000s → daemon 周期性重协商，Artifactory 实况如此）。
- **建议分臂**：TTL 对齐 9000（消灭契约 DIVERGENT 主因，同时安全收紧）；token 内形保留不透明（客户端不解码 token 内容，BinFlow 吊销链 FR-11-AC6 是自有安全增益，无对照面）；`issued_at`/`access_token`/`scope` 超集键保留（无 Artifactory 断言排除它们，CLI 兼容实测）。
- **邻臂联动**：D2（revoked 第四臂）建议随本项联裁，见 §2.0。
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

- **现状对比**：BinFlow S3 读全代理（无重定向）；Artifactory 二进制 ≥200KB 且云 provider 支持且 `enableSignedUrlRedirect=true` 时下载可 302 重定向至预签名 URL（常量 `cloud.binary.provider.redirect.threshold.in.bytes` 默认 204800）——**URL 有效期与降级直读形态 UNKNOWN**（运行时未取证，参照实例未配云后端）。
- **硬依赖**：取证票 = 参照实例 binarystore.xml 换 `s3-storage-v3` 链指向本机 MinIO，PUT >200KB 对象后 `curl -v` GET 抓 302 Location 形态/签名参数/有效期 + 阈值上下行为（unknown.yaml **U-STG-15**（云链运行时 redirect/预签名，P1）/ **U-STG-09**（预签名 URL 生成规则，P2）、binary-provider-chain §8-4）。**取证前不裁**——盲实现违反 ADR-0001 clean-room 铁律（禁止猜测补齐兼容行为）。
- **建议**（取证后执行）：若取证证实 302/预签名形态，实现面 = S3 Backend presign GET + 200KB 阈值键 + `enableSignedUrlRedirect` 语义对齐；若取证显示形态不可稳定复刻，按 INTENTIONAL 登记（全代理是等价能力）。
- **v2 增补（R-6 取证现状，L003-3/L003-3b-final，E1+E4）**：三次换链尝试均 init 期确定性失败并三度完整恢复（sha256 字节级一致 ×3，conductor 复验）；根因链收敛 = path-style（property 形态无效 → 子元素 `<enablePathStyleAccess>true</enablePathStyleAccess>` 有效，尝试 2/3 过 DNS 证明）→ **TLS 强制**（`<httpsOnly>false</httpsOnly>` 子元素形态**不生效**——仍握手层被断，真开关未定位）。**TLS 开关三候选**（evidence §5，未验证假说按可能性排序）：① JVM 系统属性 `-Dbinary.provider.s3.https.only=false`（BinaryStoreProperties$Key 键表收录 dotted/camel 对键；注入面=JAVA_OPTIONS/setenv）；② `<property name="httpsOnly" value="false"/>` 属性形态（未单独验证）；③ `BinaryStoreConstValues` 层面出厂默认（可能仅系统属性可覆盖）。替代路径（零换链风险）= MinIO 侧配 TLS（自签证书 + artifactory truststore 注入）。静态面已收（E1 字节码）：redirect 默认关（`enableSignedUrlRedirect` 未设 true 即不重定向——字符串常量实证）、`signedUrlExpirySeconds`/`s3SignedUrlExpirySeconds` 键族、阈值常量 204800。**维持 evidence-blocked 不裁**；若三候选配方的新窗口仍失败，备选=按字节码已证静态面实现（默认直读+redirect 显式开启+有效期可配），redirect 运行时面留 UNKNOWN——该备选属产品裁定权，届时单独立票呈批（evidence §6）。
- **证据**：E1 常量+docs（binary-provider-chain §4.4-C2，中置信，有效期 UNKNOWN）；storage-v2 §1 #18；reports/compatibility/L003-s3-chain-evidence.md（§2 三次失败-恢复 / §5 三候选 / §6 回填建议）。

### R-7 C05 remote 缓存树布局

- **现状对比**：BinFlow remote 缓存 = digest 寻址（`<image>/manifests/<hex>` + `<image>/blobs/<hex>`，行带 sha1/sha2）；Artifactory = tag 目录（`library/hello-world/latest/list.manifest.json`）+ `sha256__<digest>` 文件命名 + marker 文件驱动缓存（manifest 下载预写 marker → blob 首取替换）。
- **已裁/已收敛的邻臂**：marker **门控语义**已由 ADR-0047 DigestChainGate 等价实现（docker_refs 账本 + node 短路，L002-1 落地）——本项只剩**树布局形态**；`library/` 归一**行为臂**已随 L001-1 错误形态票收敛（C12 detail 键/静态文案 SAME），docker.io 上游的 library/ 前缀行为待 LOOP 002 契约重放补证（L002-2）。
- **影响面**：客户端协议面**零差异**（缓存树是服务端内部布局，docker/oci 客户端不感知）；唯一可见通道 = Artifactory 兼容 REST 的 storage 浏览面（ListFolder/item 路径形态）——若用户有「用 REST 浏览 remote 缓存树」的使用场景则该面有对拍需求，否则无。
- **建议**：INTENTIONAL 定谳（BinFlow digest 寻址为存储契约——去重/数据完整性语义更优，ADR-0047 拒逐行翻译的立场延伸）；known-divergence `docker/remote-cache-layout` 按「布局=INTENTIONAL（本裁定）/门控=ADR-0047/归一=行为票已收敛」拆分收口。
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

### R-10 仓配置标量非法值三臂（R-10a/R-10b/R-10c）

素材：`docs/prd/ruling-invalid-value-family.md`（L007-3 architect 整理的两案对比+建议立场，本节折入并复核）；证据 E4 活体逐字（reports/compatibility/L007-3-update-merge-evidence.md §6，2026-09-12 :8082）+ E5（L006-a-b-diff.md §1.3）。共同背景：三臂同属仓配置写面（PUT/POST `/api/repositories/{key}`）的输入合法性问题；**参照自身姿态不均**（String→Integer 撞 500、String→Boolean 不可转拒 400、未知布局名拒 400、宽容布尔可落库）——「对齐参照」≠ 统一错误模型，而是复刻其不一致。该面是**管理面**（admin 凭据），真实消费方是 curl/CI 脚本与 terraform provider 类工具；400 vs 500 的差异主要在**重试语义**（规范 HTTP 客户端对 5xx 重试、对 4xx 不重试——RFC 9110 §15.5/15.6，协议语义推理，无 live 工具对拍记录）。

**R-10a mistyped 数字（`maxUniqueSnapshots:"seven"`）**

- **两案对比**：对齐案 = 复刻 500 + Java 转换器逐字报文（差分断言逐字同；但仓配置族获得与全域其余面相反的错误码姿态、500 诱发规范客户端重试确定性错误、逐字文案耦合 JDK `NumberFormatException` 实现细节——脆弱且 clean-room 边缘〔库癖性泄漏〕）；维持案 = 400 + 字段名报文，登记 INTENTIONAL_DIFFERENCE（错误模型一致、可调试性好、无重试误导；差分断言需 normalize 层——tools/difftest 已有 400/500 归一先例可挂）。
- **影响面**：差分矩阵 D02 行 3/4 note；contracts 仓配置契约错误臂冻结。
- **PM 建议立场**：**维持 400，INTENTIONAL**（采纳 architect 建议立场：500 是参照的异常泄漏而非契约设计，两案客户端实际后果等价〔均失败〕，唯重试语义 400 更正确）。

**R-10b 宽容布尔（`blackedOut:"yes"`）**

- **两案对比**：对齐案 = bool 席位接受 commons-lang 真假值词表、静默转换（照抄坏数据的脚本不炸；但静默改写输入〔typo 被吞——数据完整性气味〕、词表逐词维护〔含 'y'/'t' 单字母〕、全域 bool 席位要么跟改要么双姿态）；维持案 = 400，INTENTIONAL（Java 库癖性非有意契约）。
- **影响面**：同 R-10a。
- **PM 建议立场**：**维持 400，INTENTIONAL**（受害脚本形态在 JSON API 消费方中属罕见病；宽容转换的静默性反噬可调试性）。备选折衷臂（仅接受字符串 "true"/"false"——JSON 惯用双拼写，比词表窄比严格宽）：若用户判断「照抄迁移脚本」场景真实存在可选，不预裁。

**R-10c 未知布局名（`repoLayoutRef:"no-such-layout"`）**

- **两案对比**：对齐案 = 按**内置布局名冻结清单**（官方 xsd/文档 26 内置布局名，matrix D02 行 12 已载）做存在性校验，未知名 400 逐字（灭一个真实客户端可见分歧——typo 布局名在 BinFlow 静默存成无效配置、在参照被即刻拦截；成本=维护一份闭集冻结名单〔低频变更〕+ 自定义布局名迁移臂需随裁明确→R-11①）；维持案 = 接受并存读（超集姿态；零实现零维护，但静默无效配置长期在账）。
- **影响面**：实现票一枚（校验+400 逐字+差分臂）；matrix D02 行 3/4；K73 presentation-only 定谳不受影响（本臂只裁**名字存在性**，不实现布局引擎）。
- **PM 建议立场**：**对齐 400（按冻结名单校验）**（采纳 architect 建议立场：名单来源必须是官方 xsd/文档〔clean-room〕，实现票内冻结进规格票；自定义布局名迁移臂二选一归 R-11① 定）。

### R-11 update-merge 邻臂两件

素材：`docs/prd/ruling-invalid-value-family.md` §3（①）+ `docs/design/repo-update-merge.md`（ADR-0050 候选稿）+ `docs/ai-engineering/loop-state.yaml` L008-1（案 A 定谳方向）；证据 E4 活体（reports/compatibility/L007-3-update-merge-evidence.md + L006-a-b-diff.md §1.4，双仓四臂实测）。

**① 自定义布局名迁移臂（随 R-10c 联裁）**

- **两案对比**：**拒案**（名单外一律 400）= 与参照对未知名的拦截一致；BinFlow 无自定义布局能力，放行即造无效配置（错名配置静默存库长期在账）。**放行案**（白名单/登记放行 + WARN）= 迁来 configJSON 引用自定义布局名的仓不拒建（迁移顺滑）；但被放行的名字在 BinFlow 无语义（presentation-only 存读），WARN 易被 CI 脚本忽略——实质是「有条件接受无效配置」。
- **影响面**：R-10c 实现票的校验分支；迁移工具（bf-migrate 类）行为；known-divergence `rest/repo-config-invalid-value-family` 臂三分类。
- **PM 建议立场**：**拒案（名单外一律 400）**——采纳 architect 建议立场；迁移含自定义布局名的仓属显式失败（用户可感知、可改名单内布局名重迁），优于静默无效配置。

**② PUT=更新超集回撤确认（ADR-0050 案 A 呈批件）**

- **现状对比**：BinFlow PUT-on-existing=更新 200（自有超集行为）；参照=400 `error when validating repository name: <key> : Repository key already exists`（errors envelope 逐字，create-only——参照更新拼写只有 POST）。
- **已定谳方向**：LOOP 008 L008-1「update-merge **ADR-0050** 流程（**案 A 定谳**：POST=merge 三列矩阵+PUT=create-only——PUT=更新超集回撤随 ADR）」——ADR-0050 随 L008-1 入 DECISIONS.md（现册顶=ADR-0049）。**本臂待用户确认的仅剩 breaking 窗口可接受性**：案 A 下 BinFlow 自有「PUT 改仓」脚本断（改用 POST 或接受 400）、依赖「全量替换」清配置的脚本须改显式 null；release note breaking changes 首条、无双轨期（参照无 escape hatch，双轨=自造差异）。
- **影响面**：BinFlow 尚在 UAT 前夜、无外部存量承诺（回撤窗口现在最便宜）；console 前端已按 PUT=create/POST=update 分工（零破坏）；已知差分面=matrix D02 行注记复核（L007-3 发现「BinFlow PUT=更新系自有超集行为」）。
- **PM 建议立场**：**确认回撤（案 A）**——超集行为制造永久登记差与双更新入口漂移风险；对齐参照单更新入口语义最简。

### R-12 经典 security 读族 readonly_admin 姿态

素材：known-divergence `rest/security-read-family-readonly-admin-gate`（L006-1 Review B 范围外上报→LOOP 008 立账）；证据 E1（internal/httpapi/router.go 实测：security/permissions GET 族、security/users、security/groups 列表族 routeAuth 挂 CapSecurityRead）+ E1 规格（docs/reverse/rbac-model.md #2 高置信：参照实例级无 read-only admin，经典读族 admin-only）；E5 局限注记：参照无此主体，差分不可达该轴（L006-1 以 admin 凭据执行未触）。

- **两案对比**：**维持案**（CapSecurityRead 超集：admin ∨ readonly_admin 可读）= 角色是 ADR-0026 决策 1 既有架构位（readonly_admin 管理面 = {system:read, security:read, repo:read}）；对参照存在的调用者类（admin 200/普通 user 403/匿名 401）双端一致——分歧仅在 BinFlow 自有 readonly_admin 轴显现，参照存在主体面零差；console 只读视图（审计/运维角色）持续可用。**收窄案**（经典读族 admin-only）= 对齐参照；但翻转既有 readonly_admin 200→403 组合（M9「新增不破坏」基调冲突），影响面=readonly_admin 全部管理面读消费（console 只读视图首当）。
- **同款先例**：与 D2（revoked 第四臂）同为「参照主体缺位」款——参照无此角色/状态，无法差分，只能建模层裁。
- **PM 建议立场**：**维持超集，登记 INTENTIONAL**；终裁时补引 ADR-0026 决策 1 为 authority 即转正（台账 review_gate 已预留此路径）。

### R-13 建用户自动入默认组 readers（建模）

素材：known-divergence `rest/user-create-default-group-readers`；证据 E5（reports/compatibility/L007-residuals-users-diff.md §3-4，2026-09-12 双端活体：参照建用户回显 groups:["readers"]，BinFlow groups:[]）。

- **两案对比**：**对齐案**（引入默认组语义）= 预置 readers 组行 + 建用户自动入组 + 回显对齐；参照生态默认权限面（Artifactory 新用户默认可读的权限心智）对迁移用户/脚本真实存在，缺位 = 迁移后用户静默丢默认权限心智（差分可见 + 行为面损失）。**缺位案**（INTENTIONAL 建模缺位登记）= 零建模零实现；但每次建用户差分恒分歧，且迁移工具（bf-migrate users 阶段）对组面语义不对称。
- **影响面**：建模票（预置组行/建用户事务/回显）+ 权限语义：组的权限绑定建议默认零授权（BinFlow 自有权限模型下 readers 为空权限组——语义无害），与参照实例预置绑定的差异随建模票取证定，不在本票预裁。
- **PM 建议立场**：**对齐（引入默认组语义）**——静默权限心智损失劣于一张小建模票；实施细节归建模票。

### R-14 anonymous 用户行（建模）

素材：known-divergence `rest/anonymous-user-row`；证据 E5（reports/compatibility/L007-residuals-users-diff.md §2/§3-4：参照 GET /api/security/users/anonymous 200——profileUpdatable=false、internalPasswordDisabled=true，字段集同 D04-R02 全集；BinFlow 404）。

- **两案对比**：**对齐案**（只读虚拟行）= 渲染层固定行（不进 DB、不可编辑、不作为可认证身份），GET 命中 `anonymous` 返回取证形态；迁移/盘点类脚本对用户清单的差分即消。**缺位案**（INTENTIONAL 登记）= 维持 404；但「参照有此行」是迁移工具与 UI 对齐的可见面（参照 UI 用户列表含 anonymous）。
- **影响面**：建模票（handler 特判固定行）；**写面未取证**——PUT/DELETE 对该行的参照行为无活体证据（clean-room：不猜），实现票内先补取证或按只读拒写（405/400 形态届时随取证定）并登记未取证臂。
- **PM 建议立场**：**对齐（只读虚拟行最小面）**——最小实现换一个建模级差分清零；写面留取证。

## 3. 裁定后回写路径（约定）

| 裁定项 | 回写目标 |
|---|---|
| R-1/R-2/R-3/R-7 | known-divergence.yaml 对应条目分类/authority + contracts/docker-remote.yaml 条目状态 + 本表状态列 |
| R-4/R-5/R-6 | gap 总账对应域 migration_verdict + storage-v2 §1 行（经 architect）+ 本表 |
| R-8/R-9 | known-divergence 新 INTENTIONAL/UNSUPPORTED 条目 + matrix D14/D10 族标注（经 compatibility-engineer）+ PRODUCT.md 范围演进记录（如需字面扩界）+ 本表 |
| R-10a/R-10b | known-divergence `rest/repo-config-invalid-value-family` 拆臂定 INTENTIONAL（authority=本裁定 + ADR 登记）+ contracts 仓配置契约错误臂冻结 400 + matrix D02 行 3/4 note + 本表 |
| R-10c + R-11① | 同上拆臂 + 布局名实现票（冻结名单 + 400 逐字 + 差分臂；自定义迁移臂按裁定分支）+ 本表 |
| R-11② | ADR-0050 入册（architect/conductor，L008-1 承载）+ update-merge 实现票 PUT 臂 + release note breaking changes 首条 + 本表 |
| R-12 | known-divergence `rest/security-read-family-readonly-admin-gate` → INTENTIONAL（authority 补引 ADR-0026 决策 1）+ 本表 |
| R-13/R-14 | 裁对齐 → 建模+实现票（一票两臂或两票）+ 台账翻 BUG 修复路径；裁缺位 → INTENTIONAL 登记（authority=本裁定）+ 本表 |
| D2（若随 R-1 联裁） | known-divergence `docker/remote-v2-ping-revoked-arm-unreachable` → INTENTIONAL（authority=R-1 终裁 + 模型级登记）或开吊销态可验实现票 + 本表 §2.0 注记更新 |

> 本表不替任何 authority 拍板；每项终裁后由 product-manager 在本表更新状态列并按上表路径派发回写票。

## 4. 批量批复式（v2 新增）

用户可任选其一：

1. **逐项批**：按席位给裁决（例：「R-10a 维持 / R-10c 对齐 / R-11② 确认」；R-1~R-9 同理；备选臂可直接点名，如「R-10b 折衷臂」）。
2. **整包批「按建议执行」**：= 采纳全部 PM 建议立场，展开为——
   - R-1 对齐 expires_in=9000（token 不透明 + 超集键保留）；D2 随联裁按 INTENTIONAL 登记
   - R-2 维持默认开（`anonymous_access` 键管辖 remote 面）
   - R-3 维持严立场 → INTENTIONAL（ADR 登记）
   - R-4 两步走（HA 本体进目标自有形态，M18+ 候排程）
   - R-5 不进目标（UNSUPPORTED 留缝）
   - **R-6 不在整包内**——evidence-blocked 维持不裁直至 MinIO 取证（TLS 三候选配方窗口或 MinIO 侧 TLS 替代路径）；整包批复亦不可代裁（ADR-0001 红线：禁止猜测补齐兼容行为）
   - R-7 INTENTIONAL 定谳（布局拆分收口）
   - R-8 REMOVE / R-9 DEPRECATE
   - R-10a 维持 400 / R-10b 维持 400 / R-10c 对齐 400（冻结名单）
   - R-11① 名单外一律 400（拒自定义名）/ R-11② 确认 PUT 超集回撤（ADR-0050 案 A breaking 窗口背书）
   - R-12 维持 CapSecurityRead 超集 → INTENTIONAL（补引 ADR-0026）
   - R-13 对齐（默认组语义）/ R-14 对齐（只读虚拟行）
3. **生效路径**：裁决回执经 conductor 落地——PM 回写本表状态列 + 按 §3 派发回写票；INTENTIONAL 项的 ADR 登记与契约冻结随票；实现/建模类裁定转 tech-lead 拆票。
