# PRD — M6 企业就绪与生态扩展（S3 存储后端 / OIDC+LDAP / 复制联邦 / Prometheus 指标 / bf CLI / 迁移工具）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-6.md` |
| 里程碑 | M6 — 企业就绪与生态扩展（对应 ROADMAP.md「M6+ — 展望」全部条目） |
| 状态 | **v1.2**（v1.0 初版：FR-48~FR-63 六域 16 条功能需求，端点矩阵 S3/OD/LD/RE/PM/CL/MG 七域 28 条，九项开放问题 Q1~Q9；v1.1 勘误：FR-50 迁移端点契约对齐实现 + Q6/Q7 暂行口径回写 + Q10 增补，T-181；v1.2 规格裁决：Q11 定案 token 端点权限走 A 路径开放，FR-54-AC3 维持原文，T-188） |
| 上游依据 | PRODUCT.md（愿景与 Non-goals：M6 起 OIDC/LDAP 解禁——PRODUCT 只标注「第一版不做」，M6 起进入企业就绪期）、ROADMAP.md M6+ 展望（S3 存储后端 / 复制联邦 / OIDC+LDAP / Prometheus 指标 / `bf` CLI / 迁移工具）、DECISIONS.md 全部 ADR（ADR-0004 部署矩阵六产物、ADR-0005 零 CGO 依赖基线、ADR-0006 blob 存储布局与存量兼容升级、ADR-0007 元数据迁移机制、ADR-0008 路由前缀 `/binflow`、ADR-0009 匿名读默认开、ADR-0010 docker /v2 根级例外、ADR-0012 remote 代理基线、ADR-0013 virtual 解析顺序、ADR-0014 console 基线与 session 认证、ADR-0015 治理面、ADR-0016 目录实体化、ADR-0017 镜像供应链）、M5 交付基线（milestone-5.md v1.2 @m5-done）、docs/reverse/rest-api.md / auth-model.md / repo-semantics.md（行为参考） |
| 下游消费者 | tech-lead（拆票）、architect（ADR-0018~ADR-0023 新决策面）、dev-go-core（S3 adapter / replication / OIDC/LDAP 认证臂）、dev-go-storage（S3 后端）、devops-engineer（Prometheus / bf CLI scaffold）、release-engineer（迁移工具与 bf CLI 发布）、qa-engineer（H 序列验收）、tech-writer（OIDC/LDAP 接入指南、bf CLI 手册、迁移指南） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-21 | 初版：M6 范围、FR-48~FR-63（S3 六条 / OIDC-LDAP 三条 / 复制联邦四条 / Prometheus 一条 / bf CLI 一条 / 迁移工具一条）、端点矩阵 28 条、H01~H42 验收命令草案、九项开放问题 Q1~Q9、与既有 ADR 的冲突/补充标注 |
| v1.1 | 2026-08-22 | 勘误（T-181）：① migration 契约对齐实现——FR-50 迁移进度端点响应字段改为 `running/done/total/migrated/skipped/failed`（+`error`/`started_at`/`finished_at` 三者 omitempty），未配置/未启用双写时迁移两端点 501（以 architecture.md §7.1 T-176 回写版与 `internal/storage/migration.go` json tag 为准；旧拟名 `total_blobs`/`in_progress`/`completed` 作废——配置键 `migration.completed` 不受影响）；② Q6/Q7 暂行口径按 T-162 报告回写，标注「暂行已实现，待终裁」并注明切换位；增补 Q10（私有复制目标默认放行） |
| v1.2 | 2026-08-22 | 规格裁决（T-188，§7 Q11）：T-174 D3 冲突裁定走 **A 路径（开放）**——`POST /api/security/token` 权限模型对齐 Artifactory（auth-model.md §3.1）：admin 全量；**非 admin 已认证用户（本地/OIDC/LDAP 三臂同权）可为本人发 Token**。FR-54-AC3 **维持原文不勘误**；T-174 D3 定性从「PRD 冲突」改判「实现未达 PRD」（H26 维持部分过定格，普通 OIDC 用户腿转实现票）。四条护栏：① 非 admin 主体仅限本人（指定他人 → 403）；② 非 admin 强制有限 TTL（`expires_in>0` 且 ≤ 上限，默认 365d，K9）；③ 验证期校验主体用户行存在且未禁用（禁用 → Token 401）；④ 非 admin 铸 Token 记 `token.issue` 审计（M5 G31a 既有面）。列表/吊销维持 admin-only。FR-54 Token 兼容条、FR-54-AC3、FR-55 映射条、OD-04、H26、K9 同步回写；实现票草案 AC 见 Q11 |

---

## 1. 背景与目标

### 1.1 背景

M1~M5 交付了一个**功能完整的单实例制品仓库**：五协议、三仓型、控制台、治理四件、备份恢复、六平台发布矩阵、Docusaurus 文档中心——94/98 QA PASS，零 HIGH/CRITICAL 安全漏洞，GA 就绪。

M5 的行为面冻结标志着 BinFlow 从「可用」进入「可运营」阶段。M6 的用户价值重心从「单点功能」转向「企业级能力」：对象存储降本、SSO 集成免维护、跨地域高可用、可观测性接入、CLI 降低运维门槛、迁移工具缩短从 Artifactory 到 BinFlow 的最后一步。

**PRODUCT.md 的 Non-goals 中「第一版不做 LDAP / SAML / OIDC」在 M6 起解禁**——M6 是 BinFlow 的企业就绪起点。

### 1.2 M6 目标

> 一句话：交付 S3 存储后端、OIDC/LDAP 企业认证、仓库复制（push 单向）、Prometheus 指标端点、`bf` CLI、Artifactory 迁移工具——让 BinFlow 从单实例工具升级为企业级制品平台。

量化门槛（未达即里程碑不完成）：

| 指标 | M6 门槛 | 来源 |
|---|---|---|
| S3 存储 | 所有既存 QA 序列（M1 C + M2 D + M3 M + M4 W + M5 G）在 S3 后端下复跑全绿；本地 filestore → S3 在线迁移零丢数据 | 本 PRD FR-48 |
| OIDC/LDAP 登录 | OIDC（含 Google/Keycloak/Azure AD 任一）或 LDAP 绑定成功 + 控制台 session 可用 + 授权实时生效 | 本 PRD FR-51/FR-52 |
| 复制 | push 单向：源实例 → 目标实例，docker 镜像 + generic 制品 + maven/npm/pypi 全协议覆盖，增量复制延迟 < 60s（P50） | 本 PRD FR-54 |
| Prometheus | `/metrics` 端点 200，含 HTTP 请求、存储、认证、复制四类指标，无 HIGH/CARDINALITY 标签 | 本 PRD FR-58 |
| bf CLI | `bf repo create`、`bf artifact upload/download`、`bf user create`、`bf token create` 四个子命令可跑通；`bf --version` 输出版本号 | 本 PRD FR-59 |
| 迁移工具 | 从 Artifactory 实例迁移 1 个 generic local 仓库（含 100+ 制品 + 用户 + token）到 BinFlow，制品 sha256 一致、用户可登录 | 本 PRD FR-60 |
| 既有零回归 | M1~M5 全部 P0 序列在本地 filestore 下复跑全绿；S3 后端下同序列全绿 | GA 后首个里程碑的回归硬门槛 |

### 1.3 上游依赖与并行关系

- **架构依赖（architect）**：M6 新增至少 6 条 ADR（S3 适配器接口、S3 升级迁移、OIDC 提供者模型、LDAP 绑定与搜索、复制事件模型与冲突策略、Prometheus 指标命名与标签规范）。本 PRD 只约束可观察行为，实现细节归 ADR。
- **存储引擎升级（dev-go-storage）**：S3 后端是 M6 最大的基础设施变更。ADR-0006 的 blob 布局（`blobs/<sha256[0:2]>/<sha256>`）必须在不改布局的前提下适配 S3 的 object key 模型。本地 filestore 与 S3 后端的**热切换**（运行时切换后端）是 M6 的关键架构决策（Q1）。
- **认证模型升级（dev-go-core）**：OIDC/LDAP 是 ADR-0009（匿名读默认开）之后最大的认证模型变更。需新增至少一个认证臂（OIDC Bearer/LDAP bind），并与既有 Basic/Token/session 四臂共存。
- **逆向参考（reverse-engineer）**：Artifactory 的 replication、OIDC/LDAP 集成、Prometheus 端点有逆向规格需求（Q9 待定）。无逆向规格的能力以公开协议文档为准（OIDC = OpenID Connect Core 1.0、LDAP = RFC 4511、Prometheus = OpenMetrics 兼容）。
- **QA 并行面**：M6 的 QA 面显著大于 M1~M5 任何单里程碑——S3 后端下需复跑全部既存序列；OIDC/LDAP 需外部 IdP；复制需至少两个 BinFlow 实例。QA 策略分阶段：先本地 filestore 回归全绿 → 再 S3 后端全序列 → 再 OIDC/LDAP/复制/迁移工具增量。

---

## 2. 范围

### 2.1 In scope（与 ROADMAP M6+ 展望条目一一对应）

| # | ROADMAP 条目 | 本 PRD 功能需求 | 优先级 |
|---|---|---|---|
| 1 | S3 存储后端 | FR-48（S3 adapter 接口与本地等价性）+ FR-49（S3 配置与健康检查）+ FR-50（本地→S3 在线迁移）+ FR-51（S3 下 checksum 去重与 GC）+ FR-52（S3 下性能基线）+ FR-53（S3 兼容性列表） | P0 |
| 2 | OIDC/LDAP | FR-54（OIDC 提供者集成）+ FR-55（LDAP 绑定与搜索）+ FR-56（认证臂优先级与 fallback） | P0 |
| 3 | 复制/联邦 | FR-57（push 单向复制）+ FR-58（复制事件与增量）+ FR-59（复制监控与冲突处理）+ FR-60（多协议覆盖） | P1 |
| 4 | Prometheus 指标 | FR-61（`/metrics` 端点与四类指标） | P1 |
| 5 | `bf` CLI | FR-62（`bf` CLI 四个子命令 + 配置管理） | P2 |
| 6 | Artifactory 迁移工具 | FR-63（`bf-migrate` 仓库+用户+token 迁移） | P2 |

### 2.2 Non-goals — M6 明确不做

**产品级 Non-goals（继承 PRODUCT.md，M6 起 OIDC/LDAP 解禁——其余不变）**：不做 HA 集群（active-active 多写）、不做 Xray 式漏洞扫描 / 许可证合规、不做 SAML（M6 仅 OIDC + LDAP；SAML 归 M7+）、不做 Artifactory 全量 REST 兼容、不做 UI 高级分析与洞察报表。

**M6 里程碑级 Non-goals**：

| 不做项 | 归属 | M6 的隔离边界 |
|---|---|---|
| HA 集群（active-active 多写） | M7+ | 复制是 push 单向（源→目标），不是双向同步；replica 节点只读不写；无冲突合并（last-write-wins 或手动） |
| SAML 认证 | M7+ | M6 仅 OIDC + LDAP；SAML 的元数据交换、SP-initiated/IdP-initiated 双流、签名证书管理复杂度单独里程碑 |
| pull 复制（目标拉取源） | M7+ | M6 仅 push 单向复制；pull 复制的事件订阅、拉取调度、网络分区恢复归 M7+ |
| 多源复制（星型/网状拓扑） | M7+ | M6 仅单源→单目标；多源拓扑的冲突向量时钟/CRDT 归 M7+ |
| LDAP 分页（VLV/Simple Paged Results） | M7+ | M6 LDAP 搜索仅默认 LDAP 服务端单页上限（通常 1000 条目）；超大目录（>1000 用户/组）需分页或预先过滤 |
| OIDC 动态注册（Dynamic Client Registration） | M7+ | OIDC client 静态配置（client_id/client_secret/issuer），不支持 `/.well-known/openid-configuration` 的 registration_endpoint |
| 多 IdP 联合（同时多个 OIDC provider） | M7+ | M6 单 OIDC provider + 单 LDAP server；多 provider 的域名路由/claims 映射归 M7+ |
| Prometheus Pushgateway / Grafana 仪表板预置 | M7+ | M6 仅 `/metrics` 端点 + 指标文档；Grafana 仪表板 JSON 与告警规则归 M7+ |
| `bf` CLI 完整命令集（plugins/lifecycle/stats/audit） | M7+ | M6 仅四个子命令（repo/user/token/artifact）；命令补全、插件系统归 M7+ |
| 迁移工具覆盖 docker/maven/npm/pypi 全协议 | M7+ | M6 迁移工具仅 generic 本地仓库 + 用户 + token；多协议迁移归 M7+ |
| 迁移工具在线不停机迁移 | M7+ | M6 迁移工具离线运行（源 Artifactory 可在线，目标 BinFlow 需空实例）；增量同步归 M7+ |
| LDAP 组的 OIDC claims 映射 | M7+ | M6 的 OIDC 与 LDAP 是两条独立认证臂；LDAP 组的 OIDC claims 桥接（如 OIDC token 中带 LDAP 组成员）归 M7+ |
| S3 对象锁定（Object Lock / WORM） | M7+ | M6 S3 仅标准读写；不可变存储的合规持有期归 M7+ |
| S3 智能分层 / 生命周期策略自动管理 | 不排期 | BinFlow 不管理 S3 bucket 生命周期；由运维在 S3 侧（AWS Console/CLI）自行配置 |

---

## 3. 用户与场景（M6 视角）

- **场景 A（降本：从块存储到对象存储）**：运维团队在 AWS 上部署 BinFlow，EBS 卷成本高且备份窗口长。切换 S3 后端后，制品存储成本降 60%，备份只需导出 SQLite 快照（blob 已在 S3 上，无需 tar）。
- **场景 B（企业 SSO 接入）**：平台团队已有 Keycloak/Okta/Azure AD 作为统一身份源。BinFlow 接入 OIDC 后，开发者用公司账号登录控制台，CI 用 LDAP 服务账号绑 Token——不再维护 BinFlow 本地用户密码。
- **场景 C（跨地域制品分发）**：总部 CI 在深圳 build 出的 Docker 镜像，通过 push 复制自动同步到新加坡的 BinFlow 实例——当地开发团队 `docker pull` 延迟从 300ms 降到 5ms，且不依赖总部网络。
- **场景 D（Prometheus 告警）**：SRE 把 BinFlow `/metrics` 接入 Prometheus + Grafana，配置了「复制延迟 > 5min」告警和「存储剩余容量 < 10%」预警——从「黑盒」变成「可观测」。
- **场景 E（CLI 运维脚本）**：运维用 `bf repo create` 批量建仓、`bf user create` 批量建账号——无需手写 curl JSON body，脚本可读性提升一个数量级。
- **场景 F（Artifactory 迁移最后一公里）**：从 Artifactory 迁出的团队运行 `bf-migrate`，指定源 Artifactory URL 与目标 BinFlow 实例，工具自动拉取仓库列表、制品、用户、token——迁移从「手工比对手册」变成「一条命令 + 一份报告」。

---

## 4. 功能需求

约定：`BASE=http://localhost:8080`、`admin`/`$ADMIN_PW` 沿用；`S3_BUCKET`/`S3_REGION`/`S3_ENDPOINT` 等为 S3 配置变量。优先级 P0/P1/P2 沿用 M1 定义。H 序列命令见 §5.4。

### 4.1 S3 存储后端（P0）

#### FR-48 S3 adapter 接口与本地等价性（dev-go-storage）

**用户故事**：作为运维，我把 BinFlow 的 `storage.backend` 从 `local` 改为 `s3`（配好 bucket/region/credentials），之后所有制品上传下载行为与本地 filestore 完全一致——我的 CI 脚本、docker 客户端、maven/npm/pip 全部零改动。

行为规格：

- **S3 adapter 实现 `storage.Store` 接口**（ADR-0006 定义的 blob 寻址与落盘契约）：`Put(ctx, sha256, reader) → error` / `Get(ctx, sha256) → io.ReadCloser` / `Exists(ctx, sha256) → bool` / `Delete(ctx, sha256) → error` / `Stat(ctx, sha256) → FileInfo`。
- **Object key 映射**：`<bucket_prefix>/blobs/<sha256[0:2]>/<sha256>`（与 ADR-0006 本地布局同构，S3 上无 sessions 目录——上传会话用 S3 multipart upload 的临时 parts 或本地临时文件缓冲）。
- **上传协议**：大文件（> 5MB 可配阈值）走 S3 multipart upload（分段上传 + CompleteMultipartUpload 原子完成）；小文件走 PutObject。上传过程边写边算 sha256/sha1/md5，与本地协议一致。
- **下载协议**：流式 GetObject（支持 Range 请求透传——S3 原生 GetObject 的 Range 参数）。
- **并发同一 blob**：利用 S3 的 object 原子性——PutObject 在 S3 侧是原子的（同 key 后写覆盖、先写先可见）；BinFlow 侧仍用 singleflight 互斥减少重复上传（与 ADR-0006 决策 4 一致）。
- **元数据存储**：元数据库（SQLite/Postgres）**不受 S3 后端影响**——元数据仍走既有迁移栈与连接池，只有 blob 数据面切到 S3。
- **sessions（上传会话）**：S3 后端下，上传会话数据不落本地 `sessions/` 目录（无本地数据目录可写或写 S3 临时对象）。multipart upload 的中间 parts 由 S3 管理（AbortMultipartUpload 清理未完成上传）；小文件上传缓冲在内存（上限 5MB，超限走 multipart）。会话状态（state.json）落元数据库（SQLite 新增 `upload_sessions` 表）而非本地文件——与 ADR-0006 决策 2 冲突，见 §6.1。

> **与 ADR-0006 冲突/补充标注**：ADR-0006 决策 2「会话 = `<data>/sessions/<uuid>/{data,state.json}`，纯磁盘状态，不入 DB」在 S3 后端下不可行——S3 无本地数据目录。M6 需新增 `upload_sessions` 表（sha256 / state / created_at / expires_at），S3 后端下会话状态入 DB；本地 filestore 下保持 ADR-0006 的磁盘会话不变（或统一为 DB 会话——Q2 定案）。本 PRD 建议：**本地 filestore 也统一为 DB 会话**（降低双路径维护成本），需 architect 在 ADR-0018 裁决。

- **验收矩阵**：S3 后端下复跑 M1 C 序列 P0（C01~C29 全部）、M2 D 序列 P0、M3 M 序列 P0、M4 W 序列 P0、M5 G 序列 P0——全绿等价于 S3 adapter 与本地 filestore 行为等价。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-48-AC1 | H01：MinIO（或 S3 兼容存储）建 bucket → 配置 BinFlow `storage.backend=s3` → 起服 → M1 C 序列 P0 全绿（generic 上传/下载/校验/去重/删除 全路径）——含 stats blob 计数与去重断言（C07/C12） | P0 |
| FR-48-AC2 | H02：M2 D 序列 P0 全绿（docker push/pull、manifest/tag、catalog、Helm OCI）——证明 S3 后端下 docker 协议等价 | P0 |
| FR-48-AC3 | H03：M3 M 序列 P0 全绿（maven deploy/resolve、npm publish/install、pypi upload、remote pull-through、virtual 聚合） | P0 |
| FR-48-AC4 | H04：M4 W 序列 P0 全绿（控制台（session 登录/建仓/树/搜索/上传）+ 权限完整 + 审计 + GC + 配额 + 备份恢复）——S3 后端下 `export` 只导出元数据（blob 已在 S3），`import` 恢复后 blob 可达（S3 侧未动） | P0 |
| FR-48-AC5 | H05：M5 G 序列 P0 全绿（版本注入/镜像/部署/文档/安全/性能/债务收编）——S3 后端下压缩产物 check-size 仍成立（S3 依赖不进二进制） | P0 |
| FR-48-AC6 | H06：上传 1GB 文件（FR-2-AC6 同口径）→ S3 后端下 RSS 增量 < 256MB（内存缓冲 + 流式 multipart） | P0 |

#### FR-49 S3 配置与健康检查（dev-go-storage）

**用户故事**：作为运维，我在 `binflow.yaml` 里写清楚 S3 的 bucket/region/endpoint/credentials 后启动，`/health` 端点告诉我 S3 连通性是否正常——而不是等到第一次上传才报错。

行为规格：

- **配置键**（`storage` 段下）：
  - `backend`: `"local"`（默认）| `"s3"`
  - `s3.bucket`: S3 bucket 名称（必填）
  - `s3.region`: AWS 区域（如 `us-east-1`），对 S3 兼容服务（MinIO/Ceph）可为 `us-east-1` 占位
  - `s3.endpoint`: 自定义 endpoint（空 = 默认 AWS S3；MinIO 用 `http://minio:9000`）
  - `s3.access_key_id` / `s3.secret_access_key`: 静态凭据（可选——也支持 AWS SDK 默认凭据链：env/AWS profile/EC2 instance role）
  - `s3.use_path_style`: 对 MinIO 等需 path-style 访问的服务设为 `true`（默认 `false` = virtual-hosted-style）
  - `s3.upload_part_size`: multipart 分片大小（默认 16MB，最小 5MB）
  - `s3.upload_concurrency`: 并发分片数（默认 4）
- **环境变量覆盖**：`BINFLOW_STORAGE_BACKEND`、`BINFLOW_S3_BUCKET`、`BINFLOW_S3_REGION`、`BINFLOW_S3_ENDPOINT`、`BINFLOW_S3_ACCESS_KEY_ID`、`BINFLOW_S3_SECRET_ACCESS_KEY`（12-factor 友好，秘密不入 YAML）。
- **健康检查（/health）**：`storage` 子系统状态在 S3 后端下含义变为「S3 bucket 可达且可写」——启动时做一次 HeadBucket + PutObject（哨兵校验）+ DeleteObject 三步；失败时 `/health` 的 storage 状态为 `"degraded"` 或 `"unhealthy"`。
- **启动时 fail-fast**：`backend=s3` 但 `bucket` 未配 → 启动失败（退出码非 0 + 日志）；S3 凭据缺失（无静态凭据 + 无默认凭据链）→ 启动失败。
- **CGO 约束**：S3 依赖用 AWS SDK Go v2（`github.com/aws/aws-sdk-go-v2`），纯 Go 实现、零 CGO——满足 ADR-0005 依赖准入。该库需经 architect 批准加入白名单。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-49-AC1 | H07：`backend=s3` + `bucket` 已配 + 凭据有效 → 起服成功 → `/health` 含 `storage: "ok"`；`/readyz` 200 | P0 |
| FR-49-AC2 | H08：`backend=s3` 但 `bucket` 不存在 → 启动失败，日志含 S3 错误码（NoSuchBucket / 403）；`/health` 不暴露（进程未启动）或 storage=`unhealthy` | P0 |
| FR-49-AC3 | H09：`backend=s3` 但凭据无效 → 启动失败，日志含 S3 认证错误（InvalidAccessKeyId / SignatureDoesNotMatch） | P0 |
| FR-49-AC4 | H10：`BINFLOW_STORAGE_BACKEND=s3` + `BINFLOW_S3_BUCKET=x` 环境变量覆盖 → 起服成功，S3 后端生效（与 YAML 配置等价） | P1 |
| FR-49-AC5 | H11：MinIO 实例上 `endpoint=http://localhost:9000` + `use_path_style=true` + `region=us-east-1` → 起服成功，上传下载正常 | P0 |

#### FR-50 本地→S3 在线迁移（dev-go-storage + dev-go-core）

**用户故事**：作为运维，我已有 200GB 本地 filestore 的制品。我改配置 `backend=s3` 后重启，BinFlow 自动把本地 blob 迁移到 S3（零停机 = 迁移期间继续服务 read，write 双写）——我不需要手动跑 `aws s3 sync` 或 `rclone`。

行为规格：

- **迁移模式**：`storage.migration.enabled: true`（默认 `false`）时，启动后进入「双写 + 后台迁移」模式：
  - 读：先查 S3，miss 则回退本地 → 命中后异步推入 S3（lazy migration）
  - 写：新上传同时写本地 + S3（双写），本地成功后异步写 S3（S3 写失败不阻塞请求，仅记录错误日志 + 重试队列）
  - 后台迁移：启动后扫描本地 `blobs/` 目录，逐 blob 上传 S3（SkipIfExists），迁移进度可查询
- **迁移完成**：全部本地 blob 已在 S3 后，`storage.migration.completed` 标记为 `true`——后续可切为纯 S3 模式（`backend=s3` + `migration.enabled=false`），本地 blob 可删除（由运维手动执行，BinFlow 不自动删本地文件）。
- **迁移进度查询**（v1.1 契约，以实现为准）：`GET /binflow/api/v1/storage/migration`（admin）→ 200 JSON：`{"running": bool, "done": bool, "total": N, "migrated": N, "skipped": N, "failed": N, "error"?: string, "started_at"?: RFC3339, "finished_at"?: RFC3339}`——`total` = 启动盘点时待迁移 blob 数（在本地盘且不在 S3 的）；`skipped` = 盘点时 S3 已有（幂等跳过不重拷）；`migrated`/`failed` = 已拷贝/拷贝失败计数；`error`（首错文本）/`started_at`/`finished_at` 三者 omitempty（未发生即不出键）；从未启动过迁移时返回全 0 基线快照。旧拟名 `total_blobs`/`in_progress`/`completed` 作废（v1.0 拟名未按实现落地，T-160 核验发现，T-176 回写）。
- **迁移触发**：`POST /binflow/api/v1/storage/migration/start`（admin）——幂等：迁移已运行中时重复调用返回 202 + 上述状态体；启动被拒（如已完成）→ 409。
- **501 语义（v1.1）**：未处于双写装配（`backend≠s3`、或 `migration.enabled≠true`、或已置 `migration.completed=true`——端点仅在 dual-write 启动时接线）时，上述两端点返回 **501**「migration is not configured」（非 404）。
- **迁移中断恢复**：重启后从断点继续（已存在的 S3 object 跳过，SkipIfExists 幂等）；迁移期间 `storage.migration.enabled=true` 不可逆（设为 false 后不再读本地）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-50-AC1 | H12：本地 filestore 存有 100+ blob → 配置 `backend=s3` + `migration.enabled=true` → 起服 → `GET /api/v1/storage/migration` 显示进度 → 迁移完成后 `done=true`（`running=false`）；所有制品 GET 200（经 S3 返回） | P0 |
| FR-50-AC2 | H13：迁移期间上传新制品 → 201 成功 → 本地 + S3 均有 blob；迁移完成后 `done=true` + 新制品 S3 上可达 | P0 |
| FR-50-AC3 | H14：迁移期间重启 → 继续从断点迁移（已完成 blob 不重复上传）；日志无重复上传错误 | P1 |
| FR-50-AC4 | H15：`migration.enabled=false` 后切换纯 S3 模式 → 本地 blob 目录可删（运维手动）→ 所有制品仍可达（S3 only） | P1 |

#### FR-51 S3 下 checksum 去重与 GC（dev-go-storage + dev-go-core）

**用户故事**：作为运维，S3 后端下 checksum 去重和 GC 的行为与本地 filestore 一致——我不需要学两套治理模型。

行为规格：

- **去重**：S3 上同一 sha256 的 object key 全局唯一（与本地布局同构），跨仓共享 blob 无需 S3 侧复制。
- **GC**：mark-sweep 逻辑不变（ADR-0006 决策 5 + ADR-0015 勘误）——sweep 时 `DeleteObject` 而非 `os.Remove`；grace 基准 = blob 的 `LastModified`（S3 object metadata，等价于本地 mtime——需在上传时通过 S3 `Metadata` 头显式记录 `blob-created-at` 时间戳）。
- **GC 安全**：S3 后端下 `gc --apply` 执行 DeleteObject——需额外确认（Q3 定案：是否加 `--confirm-s3` 标志或复用 `--apply` 语义）。

> **与 ADR-0006 冲突/补充标注**：ADR-0006 勘误「grace 基准 = blob 文件 mtime」在 S3 后端下需改为「blob 的 S3 object 自定义元数据 `blob-created-at`」——S3 的 LastModified 是上传时间、非原始 blob 创建时间。需在 PutObject 时设置 `Metadata: {"blob-created-at": "<RFC3339>"}`，GC 时读取该元数据。若 S3 object 无该元数据（老迁移 blob），fallback 到 S3 LastModified。此变更不推翻 ADR-0006 原则——grace 基准仍是「blob 创建时间」，只是 S3 上载体不同。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-51-AC1 | H16：S3 后端下 C12 去重断言——同内容两条路径 PUT 后 stats blob 数不变；跨仓同 blob 存一份（S3 上 object count 不变） | P0 |
| FR-51-AC2 | H17：S3 后端下 GC dry-run 报告可回收 blob 数正确 → `gc --apply` 后对应 S3 object 被删除 → 已删除 blob 的路径 GET 404 | P0 |
| FR-51-AC3 | H18：S3 上传的 blob 携带 `blob-created-at` 元数据；GC grace 基于该字段（非 S3 LastModified） | P1 |

#### FR-52 S3 下性能基线（dev-go-storage + qa-engineer）

**用户故事**：作为容量规划师，我有 S3 后端下的性能数据（1000 并发拉取、冷启动、上传吞吐），用于对比本地 filestore 做后端选型。

行为规格：

- **复跑 M5 NFR-P21/P22/P23**：1000 并发拉取零错误（S3 后端下）、冷启动 < 2s（含 S3 连通性检查）、空载 RSS < 100MB、1GB 流式 RSS < 256MB。
- **S3 特有指标**：上传吞吐（MB/s，分小文件 1MB 与 大文件 1GB）、下载吞吐（同上）、S3 API 调用延迟（P50/P95）。
- **记录项**（不设硬门）：与本地 filestore 对比（同机型、同网络），S3 后端的延迟增加与吞吐变化——供 M6+ 决策。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-52-AC1 | H19：S3 后端下 1000 并发拉取零错误（G27 同口径，含 docker pull 混合） | P0 |
| FR-52-AC2 | H20：S3 后端下冷启动 < 2s（G28 同口径）；空载 RSS < 100MB | P0 |
| FR-52-AC3 | H21：S3 后端下上传/下载吞吐基准记录（1MB×100 与 1GB×1 两种场景）；S3 API 调用 P50/P95 延迟记录 | P1 |

#### FR-53 S3 兼容性列表（dev-go-storage + release-engineer）

**用户故事**：作为运维，我知道 BinFlow 支持哪些 S3 兼容存储——而不是猜「MinIO 能不能用」。

行为规格：

- **明确支持矩阵**（经 QA 验证）：
  - AWS S3（标准）
  - MinIO（RELEASE.2024+ 或最新稳定版）
  - 兼容 S3 API 的存储（Ceph RGW，需用户提供环境）
- **文档化**：S3 配置指南（tech-writer，FR-63 联动）含各服务商的 endpoint/region/path-style 配置示例。
- **不支持**：GCS S3 兼容模式（XML API 差异，用户反馈后再评估）、Azure Blob S3 兼容模式（同上）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-53-AC1 | H22：AWS S3（或 MinIO）上 M1~M5 全部 P0 序列复跑全绿（QA 报告注明 S3 服务商与版本） | P0 |
| FR-53-AC2 | H23：MinIO 上 M1~M5 全部 P0 序列复跑全绿（`use_path_style=true`） | P0 |
| FR-53-AC3 | 文档：S3 配置指南含 AWS S3 / MinIO 配置示例（endpoint/region/凭据注入方式） | P1 |

---

### 4.2 OIDC + LDAP 企业认证（P0）

#### FR-54 OIDC 提供者集成（dev-go-core）

**用户故事**：作为平台管理员，我配置 BinFlow 接入公司 Keycloak/Okta/Azure AD 后，开发者在控制台点「使用 SSO 登录」→ 跳转 IdP 登录页 → 回调后获得 BinFlow session——全程无需在 BinFlow 侧创建本地用户。

行为规格：

- **OIDC 提供者配置**（`auth.oidc` 段）：
  - `enabled`: `true` | `false`（默认 `false`）
  - `issuer`: OIDC provider 的 issuer URL（如 `https://keycloak.example.com/realms/myorg`），用于发现 `.well-known/openid-configuration`
  - `client_id`: BinFlow 在 IdP 注册的 client ID
  - `client_secret`: client secret（通过 env `BINFLOW_AUTH_OIDC_CLIENT_SECRET` 注入，不入 YAML）
  - `scopes`: 额外 scopes（默认 `openid profile email`）
  - `username_claim`: 从 ID token / userinfo 提取用户名的 claim 名（默认 `preferred_username`，fallback `sub`）
  - `groups_claim`: 从 ID token / userinfo 提取组名的 claim 名（可选；如 `groups`）
  - `admin_group`: 该组成员自动获得 BinFlow admin 角色（可选；如 `binflow-admins`）
  - `redirect_uri`: 回调地址（自动生成为 `<base_url>/binflow/api/v1/oidc/callback`，用户仅需在 IdP 侧注册）
- **OIDC 登录流**（Authorization Code Grant + PKCE）：
  1. 控制台「SSO 登录」按钮 → 后端生成 PKCE `code_verifier` + `code_challenge`（S256）→ 302 到 IdP `/authorize`（response_type=code, scope, code_challenge, redirect_uri）
  2. IdP 认证后回调 `/binflow/api/v1/oidc/callback?code=...&state=...`
  3. 后端用 code + code_verifier 换 token（POST IdP `/token`）
  4. 验证 ID token（签名 + iss + aud + exp + nonce）
  5. 从 ID token / userinfo 提取 username + groups
  6. 创建或更新 BinFlow 本地用户映射（`oidc_user_mappings` 表：`oidc_sub` → `username` + `groups`）
  7. 生成 BinFlow session（`binflow_session` cookie，与 ADR-0014 决策 2 同机制）
  8. 302 到 `/binflow/ui/`
- **OIDC 用户映射**：
  - 首次登录：自动创建 BinFlow 本地用户（`username` = claim 提取值，`source=oidc`，无本地口令——仅 OIDC 登录）
  - `admin_group` 成员：自动加入 `admins` 组（或直接标记 admin——Q4 定案）
  - `groups_claim` 成员：自动同步到 BinFlow 组（`source=oidc`），每次登录刷新组成员；被移除的组在刷新后权限即时失效
- **授权**：OIDC 用户使用 BinFlow 既有权限模型（permission targets × repos × patterns）——与本地用户同权。
- **Token 兼容**：OIDC 用户仍可创建 BinFlow API Token（`POST /api/security/token`），用于 CI 脚本——Token 不依赖 OIDC session。（v1.2 Q11 裁决定案：端点权限模型对齐 Artifactory auth-model.md §3.1——admin 全量 / 非 admin 已认证仅限本人主体 + 强制有限 TTL；实现现状 admin-only 属未达 PRD，见 §7 Q11）
- **控制台 UI**：登录页新增「使用 SSO 登录」按钮（仅 `oidc.enabled=true` 时显示）；现有用户名/口令登录保留并存。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-54-AC1 | H24：Keycloak（或兼容 OIDC provider）配置 BinFlow client → 设置 `oidc.enabled=true` → 控制台「SSO 登录」按钮可见 → 点击跳转 Keycloak 登录页 → 成功后回调 → 获得 BinFlow session（Playwright 断言：`/binflow/ui/` 200 + whoami 返回 username） | P0 |
| FR-54-AC2 | H25：OIDC 用户首次登录后 `GET /api/security/users` 可见该用户（`source=oidc`）；`GET /api/v1/session` whoami 返回 username 与 groups | P0 |
| FR-54-AC3 | H26：OIDC 用户（非 admin）创建 API Token（`POST /api/security/token`，主体=本人）→ 200 → curl 用 Token 访问有权限仓库成功；负面腿：`username` 指定他人 → 403、`expires_in=0`（永不过期）→ 401 `invalid_request`（Q11 裁决护栏，非 admin 强制有限 TTL） | P0 |
| FR-54-AC4 | H27：`admin_group` 成员登录后自动获得 admin 权限（`GET /api/repositories` 200）；非 admin 成员的 OIDC 用户访问管理 API → 401/403 | P0 |
| FR-54-AC5 | H28：`groups_claim` 成员同步——IdP 侧把用户从组移除 → 用户重新登录后权限即时失效（原组授权的仓库 403） | P1 |
| FR-54-AC6 | H29：`oidc.enabled=false` 时控制台无 SSO 按钮，OIDC 回调端点 404 | P1 |

#### FR-55 LDAP 绑定与搜索（dev-go-core）

**用户故事**：作为企业 IT 管理员，我配置 BinFlow 接入公司 LDAP（Active Directory / OpenLDAP）后，用户用公司 AD 账号密码直接登录控制台——BinFlow 不做本地密码存储，密码验证全走 LDAP bind。

行为规格：

- **LDAP 配置**（`auth.ldap` 段）：
  - `enabled`: `true` | `false`（默认 `false`）
  - `url`: LDAP server URL（如 `ldaps://ad.example.com:636`）
  - `bind_dn`: 搜索用的绑定 DN（如 `CN=binflow-bind,CN=Users,DC=example,DC=com`）
  - `bind_password`: 绑定密码（通过 env `BINFLOW_AUTH_LDAP_BIND_PASSWORD` 注入，不入 YAML）
  - `base_dn`: 用户搜索基 DN（如 `DC=example,DC=com`）
  - `user_filter`: 用户搜索过滤器（默认 `(&(objectClass=user)(sAMAccountName={0}))`，`{0}` 替换为用户名）
  - `group_base_dn`: 组搜索基 DN（可选；如 `CN=Users,DC=example,DC=com`）
  - `group_filter`: 组搜索过滤器（默认 `(&(objectClass=group)(member={0}))`，`{0}` 替换为用户 DN）
  - `group_name_attr`: 组名属性（默认 `cn`）
  - `admin_group_dn`: 该组成员自动获得 BinFlow admin 角色（可选；如 `CN=BinFlowAdmins,CN=Users,DC=example,DC=com`）
  - `start_tls`: 是否使用 StartTLS（默认 `false`；`ldaps://` 时忽略）
  - `skip_tls_verify`: 是否跳过 TLS 证书验证（默认 `false`；**仅限评估环境，生产必须 false**）
- **LDAP 登录流**：
  1. 控制台用户名/密码登录提交 → 后端先查本地用户（`source=local`），命中则走本地密码验证
  2. 本地未命中 → 若 `ldap.enabled=true`，用 `bind_dn` + `bind_password` 绑定 LDAP
  3. 搜索用户：`base_dn` + `user_filter`（替换 `{0}` → 用户名）→ 唯一结果 → 获得用户 DN
  4. 用户 DN + 输入密码再次绑定 LDAP（验证密码）→ 失败则 401
  5. 搜索组：`group_base_dn` + `group_filter`（替换 `{0}` → 用户 DN）→ 列出所有 `group_name_attr` 值
  6. 创建或更新 BinFlow 本地用户映射（`ldap_user_mappings` 表：`ldap_dn` → `username` + `groups`）
  7. 生成 BinFlow session（同 OIDC 流）
- **LDAP 用户映射**：同 OIDC 模型——首次登录自动创建本地用户（`source=ldap`，无本地口令）；每次登录刷新组成员；`admin_group_dn` 成员自动获得 admin 角色。（v1.2 Q11：Token 自助面三臂同权——非 admin LDAP 用户同样可为本人铸 Token，同 FR-54「Token 兼容」条）
- **LDAP 连接池**：长连接池（默认 5 连接），避免每次登录重新 bind；连接健康检查（定期 search 基 DN）。
- **控制台 UI**：用户名/密码登录框对 LDAP 用户透明——用户输入 AD 账号密码即可登录（与本地用户同一表单，后端自动路由：先本地、后 LDAP）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-55-AC1 | H30：OpenLDAP（或 ApacheDS / AD）搭建测试目录 → 配置 `ldap.enabled=true` → 控制台用 LDAP 用户名密码登录 → 200（session 创建）+ whoami 返回 username | P0 |
| FR-55-AC2 | H31：LDAP 用户首次登录后 `GET /api/security/users` 可见该用户（`source=ldap`）；gruops 字段含 LDAP 组成员 | P0 |
| FR-55-AC3 | H32：LDAP 用户密码错误 → 401（控制台登录失败提示）；LDAP 用户无本地口令（`POST /api/security/password` 改密 → 400——LDAP 用户密码由 AD 管理） | P0 |
| FR-55-AC4 | H33：`admin_group_dn` 成员登录后自动获得 admin 权限 | P0 |
| FR-55-AC5 | H34：LDAP 连接断开（停止 LDAP 服务）→ 新登录尝试超时（可配 timeout，默认 5s）→ 明确错误提示（非 hang 或 500）；已有 session 不受影响（session 验证不走 LDAP） | P1 |
| FR-55-AC6 | H35：LDAP start_tls 成功（`ldap://` + `start_tls=true`）或 `ldaps://` 成功；`skip_tls_verify=true` 时自签名证书可用（评估场景），`false` 时自签名证书拒绝连接 | P1 |

#### FR-56 认证臂优先级与 fallback（dev-go-core）

**用户故事**：作为管理员，当 OIDC 和 LDAP 同时启用时，我有明确的认证优先级——用户不会因为「走了错误的认证臂」而登录失败。

行为规格：

- **认证臂优先级（从高到低）**：
  1. **Bearer Token**（API Token / OIDC access token / docker token）——显式凭据，最高优先级
  2. **Session Cookie**（`binflow_session`）——控制台会话
  3. **HTTP Basic**（用户名+密码）——本地用户优先，fallback 到 LDAP bind
  4. **OIDC 登录流**——仅 `/api/v1/oidc/*` 端点，不参与常规请求认证
  5. **匿名**——无凭据时的默认身份（ADR-0009 匿名读）
- **Basic 认证的 fallback 链**：用户名+密码 → 先查本地用户（`source=local`）→ 命中则密码验证（bcrypt/argon2id）→ 未命中 → 若 `ldap.enabled=true`，走 LDAP bind 验证 → 仍未命中 → 401。
- **OIDC 与 LDAP 用户名冲突**：同一用户名同时存在于本地/LDAP/OIDC → 本地优先（显式创建）；LDAP 与 OIDC 用户名冲突由 `source` 字段区分（`source=ldap` 与 `source=oidc` 可共存同一 username——Q5 定案是否允许）。
- **认证失败审计**：所有认证失败记审计事件（`auth.failed`），含 actor、method（basic/bearer/session/oidc/ldap）、reason（bad_credentials / user_not_found / provider_error）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-56-AC1 | H36：同时启用 OIDC + LDAP + 本地用户 → admin 本地登录成功 → OIDC 用户 SSO 登录成功 → LDAP 用户 Basic 登录成功 → 三种用户 session whoami 各返回正确的 username 与 source | P0 |
| FR-56-AC2 | H37：LDAP 用户用错误密码登录 → 401（不 fallback 到本地验证——本地无该用户或密码不同） | P0 |
| FR-56-AC3 | H38：认证失败事件审计可查（`GET /api/v1/audit?action=auth.failed`）——含 method 与 reason 字段 | P1 |

---

### 4.3 复制/联邦（P1）

#### FR-57 push 单向复制（dev-go-core）

**用户故事**：作为运维，我配置 BinFlow A 把 `docker-release` 和 `maven-release` 仓库的内容推送到 BinFlow B——A 上的每次上传自动同步到 B，B 上这些仓库是只读的（不能直接上传）。

行为规格：

- **复制配置**（`replication` 段）：
  - `enabled`: `true` | `false`（默认 `false`）
  - `targets`: 目标列表，每项：
    - `url`: 目标 BinFlow 实例的 base URL（如 `https://registry-sg.example.com`）
    - `username` / `password`: 目标实例的认证凭据（password 通过 env 注入）
    - `repos`: 要复制的仓库列表（`["docker-release", "maven-release"]`）
    - `cron`: 定时增量复制（可选；如 `*/5 * * * *` 每 5 分钟；不配则仅事件驱动）
    - `enabled`: 该目标是否启用
- **push 语义**：源实例上的制品上传事件（PUT 成功）→ 异步推送到目标实例（非阻塞，源上传先返回 201 给客户端）→ 目标实例接收后本地落盘。
- **事件驱动复制**：本地上传成功后，将事件写入 `replication_events` 表（repo_key + path + sha256 + timestamp + status=pending）→ 后台 worker 消费事件 → 对目标执行：checksum 存在性检查（HEAD 或 Stat）→ 若目标已存在（幂等），标记 done；若不存在，传输 blob + node 元数据 → 标记 done。
- **定时增量复制**（可选）：cron 触发时，扫描 `replication_events` 表中上次 cron 之后的所有上传事件——兜底事件丢失（进程 crash 时未持久化的事件）。
- **目标端行为**：replica 仓库标记为 `rclass=local` + `replica=true`（或 `rclass=replica` 新类型——Q6 定案），**只读**（PUT/DELETE → 405 + Allow: GET, HEAD）。BinFlow B 的本地用户不能直接上传到 replica 仓库。（v1.1 注：Q6 暂行按「local backing + virtual 只读门面」实现，backing local 仍可被目标实例直写——终裁前本条为目标态，见 §7 Q6）

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-57-AC1 | H39：源实例 A 配置 push 复制到目标 B → 源 A 上传 generic 制品 → 目标 B 的 replica 仓库 GET 同路径 200（sha256 一致） | P1 |
| FR-57-AC2 | H40：目标 B 的 replica 仓库 PUT → 405（只读）；DELETE → 405 | P1 |
| FR-57-AC3 | H41：源 A 上传后立即重启（未完成复制）→ 重启后 `replication_events` 中的 pending 事件被重新消费 → 目标 B 最终一致（60s 窗口内制品可达） | P1 |
| FR-57-AC4 | H42：复制目标不可达（目标 B 宕机）→ 源 A 上传正常（201）→ `replication_events` 中事件 status=error → 目标 B 恢复后，定时 cron 增量复制兜底补齐 | P1 |

#### FR-58 复制事件与增量（dev-go-core）

**用户故事**：作为运维，我知道复制是「增量」的——只有新上传的制品会被复制，已有的不会重复传输。

行为规格：

- **事件持久化**：`replication_events` 表（repo_key / path / sha256 / size / timestamp / status / target_url / retries / last_error），SQLite 与 Postgres 同 schema。
- **增量判断**：push 前先对目标执行 HEAD 请求（`/binflow/{repo}/{path}`）或 sha256 存在性检查——若目标已存在且 sha256 一致，跳过（标记 done）。
- **重试策略**：失败事件指数退避重试（1s → 2s → 4s → 8s → 16s，最多 5 次），失败后标记 error；定时 cron 扫描 error 事件重新入队。
- **事件清理**：成功事件保留 7 天（可配 `replication.event_retention_days`），过期后自动清理（M6 不做自动清理，仅记录 doc 建议——入 M7+ 的周期性清扫）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-58-AC1 | H43：源 A 上传同一制品两次（同 sha256）→ 目标 B 只收到一次 blob 传输（第二次 push 时 sha256 已存在，跳过 blob + 标记 done） | P1 |
| FR-58-AC2 | H44：`replication_events` 表可查询——`GET /api/v1/replication/events?status=pending` 返回待复制事件列表 | P1 |

#### FR-59 复制监控与冲突处理（dev-go-core + httpapi）

**用户故事**：作为运维，我在控制台能看到复制状态——哪些仓库在复制、上次成功时间、有无失败事件——而不是去数据库里查。

行为规格：

- **复制状态查询**：`GET /binflow/api/v1/replication/status` → 200 JSON：`{"targets": [{"url": "...", "repos": [...], "last_success": "ISO8601", "last_error": "ISO8601", "pending_events": N, "error_events": N}]}`。
- **控制台 UI**：管理页新增「复制」面板（P1）——显示目标列表、仓库、状态、上次成功时间；失败事件列表（最近 10 条）。
- **冲突处理**：push 复制是单向的——源是唯一事实来源。目标端若存在同路径但不同 sha256 的制品（手误上传到 replica），复制时**跳过**该路径并记录 `conflict` 事件（不覆盖目标端数据——Q7 定案是否覆盖）。（v1.1 注：Q7 暂行实现为「checksum 一致 → 幂等成功；不一致 → 任务终态 failed、不动目标」，009 任务状态闭集暂无 conflict——见 §7 Q7）
- **审计**：复制事件记审计（`replication.push` + `replication.error`）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-59-AC1 | H45：`GET /api/v1/replication/status` → 200，含目标列表与状态 | P1 |
| FR-59-AC2 | H46：控制台「复制」面板可见（目标/仓库/状态/上次成功） | P1 |
| FR-59-AC3 | H47：目标端存在冲突路径（同路径不同 sha256，手动放入）→ 源 push 时跳过，`replication_events` 中 status=conflict，不覆盖目标 | P1 |

#### FR-60 多协议覆盖（dev-go-core + 各 adapter）

**用户故事**：作为运维，复制不仅覆盖 generic 制品——docker 镜像（manifest + blob）、maven 构件（jar + pom + metadata）、npm tarball + packument、pypi 包——全部自动同步。

行为规格：

- **docker 复制**：manifest 与 blob 按事件逐条复制；docker 仓的 tag 节点（`docker_refs` 表）同步到目标端（`docker_tag` 事件）。目标端 docker 仓 `GET /v2/<repo>/tags/list` 返回与源一致的 tag 列表。
- **maven 复制**：jar/pom/war + `maven-metadata.xml` 同步；目标端 `GET /binflow/<maven-repo>/<path>` 200 且 sha256 一致。
- **npm 复制**：tarball + packument 同步；目标端 `npm install` 可用。
- **pypi 复制**：wheel/tar.gz + simple index 同步；目标端 `pip install` 可用。
- **virtual 仓库**：M6 不复制 virtual 仓库定义（virtual 成员的 local/remote 仓需在目标端独立配置）；仅复制内容（制品本身）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-60-AC1 | H48：源 A docker push 镜像 → 目标 B docker pull 成功（tag 一致 + sha256 一致） | P1 |
| FR-60-AC2 | H49：源 A maven deploy → 目标 B maven resolve 成功（`mvn dependency:get` 指向目标 B） | P1 |
| FR-60-AC3 | H50：源 A npm publish → 目标 B npm install 成功（`npm install --registry=http://B/binflow/npm-repo/`） | P1 |
| FR-60-AC4 | H51：源 A pip upload → 目标 B pip install 成功（`pip install --index-url=http://B/binflow/pypi-repo/simple`） | P1 |

---

### 4.4 Prometheus 指标（P1）

#### FR-61 `/metrics` 端点与四类指标（dev-go-core + httpapi）

**用户故事**：作为 SRE，我 `curl /metrics` 拿到 Prometheus 格式的指标——HTTP 请求（QPS/延迟/错误）、存储（blob 数/字节）、认证（登录/失败）、复制（事件数/延迟）——直接接入现有 Prometheus 栈，零额外 agent。

行为规格：

- **端点**：`GET /metrics`（根路径，与 `/healthz` 同级——Prometheus 抓取端点惯例不挂子路径前缀；不与 ADR-0008 冲突——`/metrics` 与 `/healthz` 同属「探针/抓取基础端点」豁免类）。
- **格式**：Prometheus text format（`Content-Type: text/plain; version=0.0.4`），兼容 OpenMetrics 规范。
- **四类指标**（命名规范待 architect ADR 定案，PRD 给最低要求）：
  1. **HTTP 指标**：
     - `binflow_http_requests_total{method, path, status}` — 请求计数（counter）
     - `binflow_http_request_duration_seconds{method, path, quantile}` — 请求延迟直方图（histogram，默认 buckets: .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10）
     - `binflow_http_requests_in_flight` — 当前处理中请求数（gauge）
     - **标签约束**：`path` 标签归一化（`/binflow/api/storage/:repo/:path` 而非原始路径），防 HIGH CARDINALITY
  2. **存储指标**：
     - `binflow_storage_blobs_total` — blob 总数（gauge）
     - `binflow_storage_blob_bytes_total` — blob 物理字节数（gauge）
     - `binflow_storage_logical_bytes_total{repo}` — 逻辑字节数（gauge）
  3. **认证指标**：
     - `binflow_auth_logins_total{method, status}` — 登录计数（counter，method=[basic, token, oidc, ldap, session]）
     - `binflow_auth_sessions_active` — 活跃 session 数（gauge）
  4. **复制指标**（`replication.enabled=true` 时）：
     - `binflow_replication_events_total{target, status}` — 复制事件计数（counter，status=[pending, done, error, conflict]）
     - `binflow_replication_latency_seconds{target, quantile}` — 复制延迟（histogram，从源上传到目标可见的秒数）
- **匿名访问**：`/metrics` 匿名可访问（与 `/healthz` 同级产品自描述面），但可通过配置 `metrics.require_auth: true` 限制（需认证）。
- **性能影响**：指标收集不阻塞请求路径（内存原子操作）；histogram 采样率可配（默认 100% 即全量，可降到 10%——`metrics.sample_rate`）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-61-AC1 | H52：`curl -s $BASE/metrics` → 200，body 含 `binflow_http_requests_total` + `binflow_storage_blobs_total` + `binflow_auth_logins_total` + TYPE/HELP 行 | P1 |
| FR-61-AC2 | H53：Prometheus server 抓取 `/metrics` 成功（`promtool check metrics` 或 `promtool test rules` 通过）；无 HIGH CARDINALITY 告警（path 标签归一化后基数 < 100） | P1 |
| FR-61-AC3 | H54：`metrics.require_auth=true` 时匿名 curl `/metrics` → 401；认证后 curl 200 | P1 |
| FR-61-AC4 | H55：`replication.enabled=true` 时 `/metrics` 含 `binflow_replication_events_total` + `binflow_replication_latency_seconds`；`replication.enabled=false` 时不含（不暴露零值） | P1 |

---

### 4.5 `bf` CLI（P2）

#### FR-62 `bf` CLI 四个子命令与配置管理（devops-engineer）

**用户故事**：作为运维，我 `brew install binflow`（或下载 `bf` 二进制）后，`bf repo create` 建仓、`bf artifact upload` 上传、`bf user create` 建用户、`bf token create` 发 Token——每条命令的参数不超过 5 个，--help 输出清晰，配置文件支持 profile 切换。

行为规格：

- **CLI 形态**：独立二进制 `bf`（非 `binflow-server` 子命令）——源仓库 `cmd/bf/`，与 `cmd/binflow-server/` 同级。`bf --version` 输出版本号（与 `binflow-server` 同源 ldflags 注入）。
- **四个子命令**：
  1. `bf repo create <key> --type local --package-type generic [--description "..." ]` → 建仓成功输出 `Repository '<key>' created.`
  2. `bf artifact upload <file> --repo <repo> --path <path>` → 上传后输出 sha256 + download URI
  3. `bf user create <username> --password <pw> --email <email> [--admin]` → 建用户成功输出 `User '<username>' created.`
  4. `bf token create [--username <user>] [--expires-in <seconds>]` → 返回 token 值（仅此一次可见）
- **配置管理**：
  - `bf` 读取 `~/.bf/config.yaml`（或 `BF_CONFIG` 环境变量指定的路径）：
    - `profiles`: 多 profile 配置（`default` + 自定义 profile）
    - 每个 profile：`base_url`（BinFlow 实例地址）、`username` / `password`（或 `token`）、`skip_tls_verify`（默认 false）
  - `bf --profile <name>` 切换 profile；`bf config set <key> <value>` 修改配置
  - 环境变量覆盖：`BF_BASE_URL` / `BF_USERNAME` / `BF_PASSWORD` / `BF_TOKEN`
- **发布**：`bf` 随 `binflow-server` 同版发布——goreleaser 六平台二进制 + checksums 含 `bf`；Docker 镜像不含 `bf`（CLI 工具不在容器内使用）。
- **错误处理**：非 2xx 响应 → 退出码非 0，stderr 输出错误信息（从 BinFlow 错误体 `errors[].message` 提取）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-62-AC1 | H56：`bf --version` 输出版本号（非 `dev`）；`bf --help` 含四个子命令 | P2 |
| FR-62-AC2 | H57：`bf repo create test-repo --type local --package-type generic` → 成功 → `curl $BASE/binflow/api/repositories/test-repo` 200 | P2 |
| FR-62-AC3 | H58：`bf artifact upload README.md --repo test-repo --path doc/readme.md` → 成功（sha256 + URI）→ `curl $BASE/binflow/test-repo/doc/readme.md` 200 且 sha256 一致 | P2 |
| FR-62-AC4 | H59：`bf user create ci-user --password pw123 --email ci@test.com` → 成功 → `curl -su ci-user:pw123 $BASE/binflow/api/repositories` 200（非 admin → 401/403） | P2 |
| FR-62-AC5 | H60：`bf token create` → 返回 token 字符串 → `curl -su admin:$TOKEN $BASE/binflow/api/repositories` 200 | P2 |
| FR-62-AC6 | H61：`bf --profile staging repo create ...` → 使用 `~/.bf/config.yaml` 中 staging profile 的 base_url 与凭据 | P2 |

---

### 4.6 Artifactory 迁移工具（P2）

#### FR-63 `bf-migrate` 仓库+用户+token 迁移（release-engineer）

**用户故事**：作为从 Artifactory 迁出的团队，我运行 `bf-migrate` 指定源 Artifactory URL 与目标 BinFlow 实例——工具自动拉取仓库列表、用户、token、制品，迁移报告告诉我哪些成功、哪些跳过、哪些失败——而不是手工比对手册。

行为规格：

- **工具形态**：独立二进制 `bf-migrate`（`cmd/bf-migrate/`），与 `bf` 同级。`bf-migrate --version` 输出版本号。
- **迁移范围（M6 子集）**：
  - **仓库**：generic local 仓库（含制品与目录结构）——全量拉取并上传到目标 BinFlow
  - **用户**：Artifactory 本地用户（用户名 + email + admin 标志）——密码不可迁移（Artifactory 哈希无法跨平台验证），迁移后用户设随机口令 + 标记 `require_password_change=true`（或 BinFlow 等效机制）
  - **Token**：API Token（仅 token 值——迁移到目标 BinFlow 后立即可用）
  - **不做**：LDAP/OIDC 配置（手工重新配置）、权限 targets（手工重建）、remote/virtual 仓库（手工重建）、docker/maven/npm/pypi 仓库（M7+）
- **迁移流程**：
  1. `bf-migrate migrate --source <artifactory_url> --target <binflow_url> --source-user <user> --source-password <pw> --target-user <user> --target-password <pw>`（或 `--target-token <token>`）
  2. 连接源 Artifactory → 验证连通性（`GET /artifactory/api/system/ping`）
  3. 连接目标 BinFlow → 验证连通性（`GET /binflow/api/system/ping`）→ 校验目标 BinFlow 为空实例（零仓库——FR-63-AC1）
  4. 列出源仓库（`GET /artifactory/api/repositories`）→ 筛选 generic local 仓库
  5. 逐仓库迁移制品：遍历源仓库文件列表（`?list&deep=1`）→ 逐文件下载（GET）+ 上传到目标 BinFlow（PUT）→ 校验 sha256 一致
  6. 迁移用户：列出源用户（`GET /artifactory/api/security/users`）→ 逐用户在目标 BinFlow 创建（`POST /binflow/api/security/users`）
  7. 迁移 Token：列出源 Token（需 Artifactory admin 权限）→ 逐 Token 在目标 BinFlow 创建（`POST /binflow/api/security/token`）
  8. 输出迁移报告：`migration_report.json`（制品数/成功/失败/跳过 + 用户数/成功/失败 + Token 数/成功/失败 + 失败明细）
- **错误处理**：单制品迁移失败不中断整体流程（记录失败，继续下一制品）；网络中断 → 断点续传（`--resume` 标志，从上次报告中的最后成功制品继续）。
- **限速与并发**：`--concurrency N`（默认 4 并发）；`--rate-limit`（每秒请求数上限，默认 10，避免打爆源 Artifactory）。
- **dry-run**：`--dry-run` 模式仅统计制品数量、用户数量、Token 数量，不执行实际迁移。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-63-AC1 | H62：目标 BinFlow 非空实例时 `bf-migrate` 退出码非 0 + 错误提示（仅空实例才可迁移） | P2 |
| FR-63-AC2 | H63：源 Artifactory 1 个 generic local 仓库（100+ 制品）→ `bf-migrate` 迁移 → 目标 BinFlow 仓库 200 + 制品 sha256 逐条一致（抽样 10 条） | P2 |
| FR-63-AC3 | H64：迁移报告 `migration_report.json` 产出——制品迁移成功率 100%（或含失败明细） | P2 |
| FR-63-AC4 | H65：迁移的用户在目标 BinFlow 可登录（随机口令 + 首次登录强制改密——或 admin 手工设密码） | P2 |
| FR-63-AC5 | H66：迁移的 Token 在目标 BinFlow 可用（`curl -H "X-JFrog-Art-Api: <token>"` 200） | P2 |
| FR-63-AC6 | H67：`--dry-run` 模式零制品迁移（目标 BinFlow 仓库存空），报告统计数与源 Artifactory 一致 | P2 |

---

## 5. 兼容性矩阵（M6 核心）

### 5.1 层级定义（沿用 M1 §5.1 四层，M6 新增第五层）

| 层级 | 含义 | 判定标准 |
|---|---|---|
| **兼容** | 路径 = `/binflow` + Artifactory 相应路径去掉 `/artifactory`；方法/参数/成功状态码/响应关键字段与 Artifactory 一致 | 真实客户端脚本**仅改 base path** 即可通过 |
| **兼容（子集）** | 同上，但字段/参数只实现高频子集；未实现字段不返回（不返回错误值） | 命令通过 + 响应为请求字段的超集/子集明示清单 |
| **语义等同但路径不同（/binflow/api/v1）** | 能力对应 Artifactory 某端点，但 BinFlow 用自有路径与更干净的 schema | 仅 `/binflow/api/v1` 文档化的命令通过 |
| **有意不兼容** | 明确决定不做的行为，返回确定性错误 | 返回码与错误体符合本 PRD 规定 |
| **BinFlow 自有（无 Artifactory 对应）** | 端点/产物为 BinFlow 原生能力，Artifactory 无对应物 | 不涉兼容性承诺 |

### 5.2 M6 端点/产物矩阵

「置信度」：高 = 公开协议规范（OIDC Core 1.0 / RFC 4511 / Prometheus text format）或官方文档双证；中 = PRD 暂行待逆向规格校准；低 = 推断待验证。编号前缀：S3 = S3 存储面，OD = OIDC，LD = LDAP，RE = 复制，PM = Prometheus，CL = CLI，MG = 迁移工具。

| # | 端点 / 产物 | 行为要点 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| S3-01 | `storage.backend=s3` 配置 + 全量行为等价 | 本地 filestore 与 S3 后端下 M1~M5 全部 P0 序列全绿；blob 布局同构（`blobs/<sha256[0:2]>/<sha256>`→ S3 object key）；上传会话入 DB（`upload_sessions` 表） | 自有（BinFlow 基础设施） | P0 | 中（待 ADR-0018 定案） | H01~H06 |
| S3-02 | `/health` storage 子系统 S3 连通性 | HeadBucket + PutObject(哨兵) + DeleteObject 三步检测；不可达 → `"degraded"` 或 `"unhealthy"` | 自有 | P0 | — | H07~H08 |
| S3-03 | `storage.migration.enabled=true` 本地→S3 迁移 | 双写+后台迁移；读 S3 miss→ 本地；迁移进度查询 `GET /api/v1/storage/migration`（running/done/total/migrated/skipped/failed，v1.1）；未配双写时迁移两端点 501（非 404）；中断恢复幂等 | 自有 | P0 | 中（待 ADR-0018 定案） | H12~H15 |
| S3-04 | S3 下 GC mark-sweep（`blob-created-at` 元数据） | grace 基准 = S3 object 自定义元数据 `blob-created-at`（非 S3 LastModified）；sweep = DeleteObject；GC 安全确认标志（Q3） | 自有（ADR-0006 补充） | P0 | 中（待 ADR-0019 定案） | H16~H18 |
| S3-05 | S3 兼容性列表（AWS S3 / MinIO） | 两个 S3 实现上 M1~M5 全序列全绿；文档含配置示例 | 自有 | P0 | — | H22~H23 |
| OD-01 | `GET /binflow/api/v1/oidc/login`（302 → IdP） | Authorization Code Grant + PKCE（S256）；scope=openid+profile+email | 自有（OIDC Core 1.0 公开协议） | P0 | 高（OIDC 规范） | H24~H25 |
| OD-02 | `GET /binflow/api/v1/oidc/callback`（code→token→session） | 验证 ID token（签名/iss/aud/exp/nonce）→ 提取 username+groups → 创建/更新映射 → 生成 session → 302 `/binflow/ui/` | 自有 | P0 | 高 | H24 |
| OD-03 | OIDC 用户映射（`oidc_user_mappings` 表） | `oidc_sub` → `username`+`groups`；首次登录自动创建本地用户（`source=oidc`）；`admin_group` 映射 | 自有 | P0 | — | H25/H27 |
| OD-04 | OIDC 用户创建 API Token | `POST /api/security/token` 可用（与本地用户同权）；v1.2 Q11 裁决：权限模型对齐 Artifactory §3.1——admin 全量 / 非 admin 已认证仅限本人主体 + 强制有限 TTL（`expires_in>0` 且 ≤ 上限，K9），匿名拒绝；列表（GET）与吊销（revoke）维持 admin-only（§3.3/§3.4） | 自有 | P0 | 高（auth-model.md §3.1） | H26 |
| OD-05 | OIDC groups 同步（`groups_claim`） | 每次登录刷新组成员；被移除的组权限即时失效 | 自有 | P1 | 中（待 OIDC provider 行为验证） | H28 |
| LD-01 | LDAP 认证（Basic → LDAP bind） | 本地用户优先 → 未命中 → LDAP 搜索用户 DN → 用户 DN bind 验证密码 → 搜索组 → 创建/更新映射 → session | 自有（RFC 4511 公开协议） | P0 | 高（LDAP RFC） | H30~H32 |
| LD-02 | LDAP 用户映射（`ldap_user_mappings` 表） | `ldap_dn` → `username`+`groups`；`admin_group_dn` 映射；`source=ldap` | 自有 | P0 | — | H31/H33 |
| LD-03 | LDAP 连接池与健康检查 | 长连接池（默认 5）；定期搜索基 DN 检查连接；连接断开 → 新登录超时（可配 timeout） | 自有 | P1 | — | H34 |
| LD-04 | LDAP TLS（`ldaps://` + `start_tls`） | 支持 `ldaps://`（隐式 TLS）与 `ldap://` + `start_tls=true`（显式 TLS）；`skip_tls_verify` 开关 | 自有 | P1 | — | H35 |
| RE-01 | `POST /binflow/api/v1/replication/targets`（创建复制目标） | 配置目标 URL + 凭据 + 仓库列表 + cron | 自有（/api/v1） | P1 | 中（待逆向规格） | H39 |
| RE-02 | `GET /binflow/api/v1/replication/status`（复制状态） | 目标列表 / 仓库 / 上次成功 / pending 事件数 / error 事件数 | 自有 | P1 | — | H45 |
| RE-03 | `GET /binflow/api/v1/replication/events?status=pending`（事件列表） | 待复制事件列表（repo_key / path / sha256 / timestamp） | 自有 | P1 | — | H44 |
| RE-04 | 目标端 replica 仓库 PUT/DELETE → 405 | `Allow: GET, HEAD`；replica 仓库只读 | 自有（有意不兼容——Artifactory 的 push replication 目标端行为待逆向校准） | P1 | 中 | H40 |
| RE-05 | push 复制事件驱动（上传事件 → `replication_events` → worker 消费 → 目标 push） | 异步非阻塞；幂等（sha256 存在性检查）；失败重试 + 定时 cron 兜底 | 自有 | P1 | — | H41~H43 |
| RE-06 | 多协议复制（docker + maven + npm + pypi） | docker（manifest+blob+tag）、maven（jar+pom+metadata）、npm（tarball+packument）、pypi（wheel+tar.gz+simple index） | 自有 | P1 | — | H48~H51 |
| PM-01 | `GET /metrics`（Prometheus text format） | HTTP/存储/认证/复制四类指标；path 标签归一化；匿名可访问（可配认证） | 自有（/metrics 属探针/抓取基础端点，与 /healthz 同豁免类） | P1 | 高（Prometheus text format 公开规范） | H52~H55 |
| CL-01 | `bf` 二进制（goreleaser 六平台） | 独立二进制 `bf`；`bf --version` 输出版本号；四个子命令（repo create / artifact upload / user create / token create） | 自有 | P2 | — | H56~H61 |
| CL-02 | `bf` 配置管理（`~/.bf/config.yaml` + profile） | 多 profile 支持；环境变量覆盖；`bf config set` 修改 | 自有 | P2 | — | H61 |
| MG-01 | `bf-migrate` 二进制（goreleaser 六平台） | 独立二进制 `bf-migrate`；source Artifactory → target BinFlow；generic local 仓库 + 用户 + token | 自有 | P2 | 中（待逆向规格） | H62~H67 |
| MG-02 | `bf-migrate --dry-run` + 迁移报告 | 统计不迁移；`migration_report.json` 产出 | 自有 | P2 | — | H66/H67 |

> 计数：**28 条**。S3 面 5 条（S3-01~S3-05）、OIDC 面 5 条（OD-01~OD-05）、LDAP 面 4 条（LD-01~LD-04）、复制面 6 条（RE-01~RE-06）、Prometheus 面 1 条（PM-01）、CLI 面 2 条（CL-01~CL-02）、迁移面 2 条（MG-01~MG-02）。全部为 BinFlow 自有端点（无 Artifactory 兼容端点——M6 全部能力为 BinFlow 原生或基于公开协议标准）。有意不兼容 **1** 条（RE-04：replica 仓库只读）。M6 无 Artifactory REST 兼容端点新增。

### 5.3 客户端与部署形态分级矩阵（M6 判定标准）

「全过」定义：所列操作退出码 0 且服务端日志无 5xx；「降级」定义同 M5 §7 Q3。

| 成员 | 必测面 | 分级 |
|---|---|---|
| curl + docker + mvn + npm + pip（回归基线） | M1~M5 全部 P0 序列在本地 filestore 下复跑全绿 | **P0 必须全过（M6 回归硬门槛）** |
| curl + docker + mvn + npm + pip（S3 后端等价） | M1~M5 全部 P0 序列在 S3 后端下复跑全绿 | **P0 必须全过** |
| curl + Prometheus（指标端点） | H52~H55 | **P1 必须全过** |
| Playwright（控制台 OIDC/LDAP 登录 + 复制面板） | H24/H25/H30/H46 | **P0（OIDC/LDAP 登录）** / P1（复制面板） |
| Keycloak / OpenLDAP（外部 IdP） | H24~H29（OIDC）/ H30~H35（LDAP） | **P0**（需外部 IdP 环境——QA 提供 Docker compose 或云实例） |
| MinIO / AWS S3（S3 后端） | H01~H06 + H22~H23 | **P0**（MinIO 本地容器可达；AWS S3 需用户提供 bucket——Q8 定案） |
| bf CLI（四个子命令） | H56~H61 | P2 |
| bf-migrate（迁移工具） | H62~H67 | P2（需 Artifactory 实例——Q9 定案） |
| 两个 BinFlow 实例（复制） | H39~H51 | P1（Docker compose 可起两个实例） |

### 5.4 M6 核心验收命令（H 序列，QA 直接引用）

> `$BASE/$ADMIN_PW` 沿用；`$BASE2` 为目标 BinFlow 实例（复制场景）；S3/MinIO 容器名与凭据在 QA 剧本中注明。以下为关键命令骨架，完整 H 序列由 QA 扩展。

```bash
# ---- 环境准备 ----
export BASE=http://localhost:8080
export BASE2=http://localhost:8081      # 复制场景用
export ADMIN_PW=password

# ========== S3 存储后端（FR-48~FR-53） ==========
# H01 S3 后端下 M1 C 序列全绿（S3-01）
#   前置：MinIO 容器（docker run minio/minio）或 AWS S3 bucket
#   binflow.yaml 配置 storage.backend=s3 + s3.bucket + s3.region + s3.endpoint + s3.access_key_id + s3.secret_access_key
#   起服 → 执行 M1 C01~C29 全部（§5.3 的 C 序列）→ 全绿
# H02 S3 后端下 M2 D 序列全绿（docker push/pull/manifest/tag）
# H03 S3 后端下 M3 M 序列全绿（maven/npm/pypi/remote/virtual）
# H04 S3 后端下 M4 W 序列全绿（控制台/权限/审计/GC/配额/备份恢复）
# H05 S3 后端下 M5 G 序列全绿（版本注入/镜像/部署/文档/安全/性能/债务）
# H06 S3 后端下 1GB 流式上传 RSS < 256MB（FR-2-AC6 同口径）

# H07~H11 S3 配置与健康检查（S3-02~S3-03）
# H07 backend=s3 + bucket 有效 → 起服 /health storage=ok
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/health | jq -r .storage   # "ok"
# H08 bucket 不存在 → 启动失败（日志含 NoSuchBucket）
# H09 凭据无效 → 启动失败（日志含 InvalidAccessKeyId）
# H10 环境变量覆盖（BINFLOW_STORAGE_BACKEND=s3 ...）→ 起服成功
# H11 MinIO + use_path_style=true → 起服成功

# H12~H15 本地→S3 迁移（S3-03）
# H12 本地 100+ blob + migration.enabled=true → 起服 → 查进度（v1.1 契约：running/done/total/migrated/skipped/failed）
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/migration | jq -r '.done'   # true（迁移完成后）
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/migration -o /dev/null -w '%{http_code}\n'   # 501（对照腿：未配置双写——backend=local 或 migration.enabled≠true 时，非 404）
# H13 迁移期间上传新制品 → 本地+S3 均有
# H14 迁移期间重启 → 断点续传
# H15 纯 S3 模式（migration.enabled=false）→ 本地 blob 可删

# H16~H18 S3 下 GC 与去重（S3-04）
# H16 去重断言（C12 同口径）
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/stats | jq '.blobs'   # 同内容去重后不变
# H17 GC --apply 后 S3 object 删除（S3 ListObjectsV2 验证）
# H18 blob-created-at 元数据存在（aws s3api head-object --bucket <b> --key <k> | jq '.Metadata."blob-created-at"'）

# H19~H21 S3 下性能基线（S3-05）
# H19 1000 并发拉取零错误（G27 同口径）
# H20 冷启动 < 2s + RSS < 100MB（G28 同口径）
# H21 上传/下载吞吐记录（1MB×100 + 1GB×1；S3 API P50/P95）

# H22~H23 S3 兼容性（S3-05）
# H22 MinIO 上 M1~M5 全序列全绿
# H23 AWS S3（条件腿：用户提供 bucket）上 M1~M5 全序列全绿

# ========== OIDC + LDAP 认证（FR-54~FR-56） ==========
# H24 OIDC 登录流（OD-01/OD-02）
#   前置：Keycloak（或兼容 OIDC provider）运行，已注册 BinFlow client
#   控制台 SSO 登录 → Playwright 断言：/binflow/ui/ 200 + whoami 返回 username
# H25 OIDC 用户映射（OD-03）
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users | jq '.[] | select(.name=="<oidc_user>") | .source'   # "oidc"
# H26 OIDC 用户创建 Token（OD-04；v1.2 Q11 裁决：非 admin 仅限本人 + 有限 TTL）
#   控制台登录 OIDC 用户（非 admin）→ POST /api/security/token（无 username 或 username=本人）→ 200
#   → curl -H "X-JFrog-Art-Api: <token>" 访问有权限仓库可用；无权限路径 → 403
#   负面腿：username=<他人> → 403 OAuth 形；expires_in=0 → 401 invalid_request；匿名 POST → 401
# H27 admin_group 映射（OD-03）
#   OIDC admin_group 成员登录 → GET /api/repositories 200
# H28 groups_claim 同步（OD-05）
#   IdP 移出组 → 重新登录 → 原组授权仓库 403
# H29 oidc.enabled=false → 无 SSO 按钮 + callback 404

# H30 LDAP 登录流（LD-01）
#   前置：OpenLDAP（或 ApacheDS）运行，有测试用户
#   控制台用户名密码登录 → session 创建 → whoami 返回 username
# H31 LDAP 用户映射（LD-02）
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users | jq '.[] | select(.name=="<ldap_user>") | .source'   # "ldap"
# H32 LDAP 用户密码错误 → 401
# H33 admin_group_dn 映射 → admin 权限
# H34 LDAP 连接断开 → 新登录超时（5s 内返回错误，非 hang）
# H35 LDAP TLS（ldaps:// 或 start_tls）

# H36~H38 认证臂优先级（FR-56）
# H36 三种用户（本地/OIDC/LDAP）同时登录 → 各返回正确 source
# H37 LDAP 用户错误密码 → 401（不 fallback 本地）
# H38 认证失败审计
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=auth.failed" | jq '.events | length'

# ========== 复制/联邦（FR-57~FR-60） ==========
# H39 push 单向复制（RE-01）
#   前置：两个 BinFlow 实例（Docker compose 起两实例）
#   源 A 配置 push 复制到目标 B → 源 A 上传制品 → 目标 B GET 同路径 200（sha256 一致）
# H40 目标 B replica 仓库 PUT/DELETE → 405（RE-04）
curl -su admin:$ADMIN_PW -X PUT -T t.bin $BASE2/binflow/replica-repo/x -o /dev/null -w '%{http_code}\n'   # 405
# H41 源重启后最终一致（RE-05）
# H42 目标不可达 → 源上传正常 → 目标恢复后 cron 兜底补齐
# H43 幂等（同 sha256 跳过 blob 传输）
# H44 复制事件查询（RE-03）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/replication/events?status=pending" | jq '.events | length'
# H45 复制状态（RE-02）
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/replication/status | jq '.targets'
# H46 控制台复制面板（Playwright 断言）
# H47 冲突处理（目标端同路径不同 sha256 → 跳过 + conflict；v1.1 暂行口径：任务 status=failed + conflict 文案，见 §7 Q7）
# H48~H51 多协议复制（docker/maven/npm/pypi 各一腿）

# ========== Prometheus 指标（FR-61） ==========
# H52 /metrics 端点（PM-01）
curl -s $BASE/metrics | grep -c 'binflow_http_requests_total'   # ≥1
curl -s $BASE/metrics | grep -c 'TYPE\|HELP'                     # 含 TYPE/HELP 行
# H53 Prometheus 抓取（promtool check metrics 或 promtool test rules 通过）
# H54 metrics.require_auth=true → 匿名 401
# H55 replication.enabled=true 时含复制指标

# ========== bf CLI（FR-62） ==========
# H56 bf --version + --help（CL-01）
bf --version               # 输出版本号（非 dev）
bf --help                  # 含四个子命令
# H57 bf repo create（CL-01）
bf repo create test-repo --type local --package-type generic --profile default
curl -su admin:$ADMIN_PW $BASE/binflow/api/repositories/test-repo -o /dev/null -w '%{http_code}\n'   # 200
# H58 bf artifact upload（CL-01）
bf artifact upload README.md --repo test-repo --path doc/readme.md
curl -s $BASE/binflow/test-repo/doc/readme.md -o /dev/null -w '%{http_code}\n'   # 200
# H59 bf user create（CL-01）
bf user create ci-user --password pw123 --email ci@test.com
# H60 bf token create（CL-01）
bf token create | grep -oE '^[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+'   # 返回 token 值
# H61 bf --profile staging（CL-02）
bf --profile staging repo create staging-repo --type local --package-type generic

# ========== 迁移工具（FR-63） ==========
# H62 目标 BinFlow 非空 → 拒绝（MG-01）
bf-migrate migrate --source http://artifactory:8081/artifactory --target $BASE --source-user admin --source-password pw --target-user admin --target-password $ADMIN_PW
#   期望：退出码非 0 + 错误提示「目标 BinFlow 非空」
# H63 1 个 generic 仓库 100+ 制品迁移（MG-01）
#   前置：Artifactory 实例有 generic-local 仓库（100+ 制品）
#   bf-migrate 迁移 → 目标 BinFlow 仓库 200 + sha256 抽样一致
# H64 迁移报告产出（MG-02）
cat migration_report.json | jq '.artifacts.migrated'   # ≥100
# H65 用户迁移后登录（MG-01）
#   迁移的用户在目标 BinFlow 可登录（随机口令，admin 手工改密或首次登录强制改密）
# H66 迁移 Token 可用（MG-01）
#   bf-migrate 迁移的 Token → curl -H "X-JFrog-Art-Api: <token>" 目标 BinFlow 200
# H67 --dry-run 零迁移（MG-02）
bf-migrate migrate --dry-run ...  # 退出码 0，migration_report.json 统计数与源一致，目标 BinFlow 无制品

# ---- 回归（M6 硬门槛） ----
# H68 本地 filestore 下 M1~M5 全部 P0 序列复跑全绿
# H69 S3 后端下 M1~M5 全部 P0 序列复跑全绿（H01~H05 的汇总断言）
```

### 5.5 待校准项（M6 落地后回写）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K1 | S3 adapter 接口定义（`storage.Store` 接口的 S3 实现）；S3 sessions 是否统一为 DB 会话（Q2 定案） | 见 FR-48 行为规格；sessions 入 DB（`upload_sessions` 表），本地 filestore 保持磁盘会话 | ADR-0018（architect） |
| K2 | S3 下 GC grace 基准（`blob-created-at` 元数据 vs S3 LastModified）；GC `--apply` 的 S3 安全确认 | grace 基准 = S3 自定义元数据 `blob-created-at`；fallback 到 S3 LastModified | ADR-0019（architect） |
| K3 | OIDC claims 映射（username_claim / groups_claim 的默认值与 fallback 链） | `preferred_username` → fallback `sub`；`groups` → fallback 无 | ADR-0020（architect） |
| K4 | LDAP 搜索过滤器（user_filter / group_filter 的默认值）与 AD/OpenLDAP 兼容性 | `(&(objectClass=user)(sAMAccountName={0}))` / `(&(objectClass=group)(member={0}))` | ADR-0021（architect） |
| K5 | 复制事件模型（`replication_events` 表 schema）与复制冲突策略（跳过 vs 覆盖） | 跳过冲突（不覆盖目标端）；事件含 sha256 + timestamp + status + retries | ADR-0022（architect） |
| K6 | Prometheus 指标命名与标签规范（`binflow_*` 前缀、path 归一化、histogram buckets） | 见 FR-61；path 归一化 = 动态段替换为 `:param` 占位符 | ADR-0023（architect） |
| K7 | `bf` CLI 配置文件格式（`~/.bf/config.yaml`）与 profile 切换 | 见 FR-62；YAML 格式 + `--profile` flag + 环境变量覆盖 | 实现票细化 |
| K8 | `bf-migrate` 迁移范围（M6 子集：generic 仓库 + 用户 + token）与断点续传 | 见 FR-63；`--resume` 从 migration_report.json 恢复 | 实现票细化 |
| K9 | 非 admin 发 Token 的 TTL 上限默认值与配置键形态；非 admin 指定他人 `username` 的错误语义 | 暂行：上限默认 365d（31536000s，对齐 Artifactory `access.token.non.admin.max.expires.in`）；配置键建议 `auth.token_nonadmin_max_ttl`（env `BINFLOW_AUTH_TOKEN_NONADMIN_MAX_TTL`，秘密不入 YAML 原则不适用——非秘密）；指定他人 → 403 OAuth 形（沿用现有 `administrator privileges required` 文案） | Q11 裁决（T-188）+ ADR-0020 附带确认（architect 定键名） |

### 5.6 回归基线反转表（M6 起生效，qa 更新既有断言）

| 既有断言 | 来源 | M6 起的期望 |
|---|---|---|
| 本地 filestore 独有行为（sessions 磁盘目录、mtime 为 grace 基准） | ADR-0006 | S3 后端下 sessions 入 DB（`upload_sessions` 表）；grace 基准 = S3 `blob-created-at` 元数据（本地 filestore 下不变） |
| 本地用户认证为唯一认证方式 | M1 FR-5 / M4 FR-23 | OIDC/LDAP 用户并存（`source=oidc|ldap`）；认证臂优先级：Bearer > Session > Basic（本地→LDAP） |
| `/metrics` 端点不存在 → 404 | 现状 | `/metrics` 200（Prometheus text format） |
| binflow-server 为唯一二进制 | M5 FR-34 | `bf` + `bf-migrate` 两个新二进制，goreleaser 三二进制同版发布 |
| M1~M5 全部 P0 序列仅本地 filestore | 各里程碑 QA 基线 | 全部 P0 序列在本地 filestore 与 S3 后端下**双份复跑全绿**（M6 回归硬门槛） |

---

## 6. 非功能需求（NFR）与已有 ADR 冲突/补充

### 6.1 与已有 ADR 的冲突/补充标注

| ADR | 冲突/补充点 | 本 PRD 立场 | 所需 ADR |
|---|---|---|---|
| ADR-0005（零 CGO 依赖基线） | S3 SDK（`aws-sdk-go-v2`）为纯 Go 实现、零 CGO——符合依赖准入原则；需经 architect 批准加入白名单 | 补充：新增白名单条目 `github.com/aws/aws-sdk-go-v2`（及其子包 `config` / `credentials` / `service/s3`） | ADR-0018 附带 |
| ADR-0005（零 CGO 依赖基线） | OIDC 依赖 `github.com/coreos/go-oidc/v3`（纯 Go，零 CGO）；LDAP 依赖 `github.com/go-ldap/ldap/v3`（纯 Go，零 CGO）；Prometheus 依赖 `github.com/prometheus/client_golang`（纯 Go，零 CGO） | 补充：新增白名单条目三条 | ADR-0020/0021/0023 附带 |
| ADR-0006（blob 存储布局）决策 2 | S3 后端无本地 `sessions/` 目录——会话状态需入 DB（`upload_sessions` 表）或 S3 临时对象。本 PRD 建议本地 filestore 也统一为 DB 会话（降低双路径维护成本） | 冲突：需修订 ADR-0006 决策 2，或为 S3 后端新增例外 | ADR-0018 |
| ADR-0006（blob 存储布局）勘误 | grace 基准 = blob 文件 mtime。S3 后端下需改为 S3 自定义元数据 `blob-created-at`（S3 LastModified 是上传时间非创建时间） | 补充：S3 后端下 PutObject 时设置 `Metadata: {"blob-created-at": "<RFC3339>"}`；GC 时读取该元数据 | ADR-0019 |
| ADR-0008（路由前缀 `/binflow`） | `/metrics` 端点挂根路径（与 `/healthz` 同级），属「探针/抓取基础端点」豁免类——不违背 ADR-0008 的例外规则 | 补充：路由表新增 `/metrics` 豁免端点 | 实现层 |
| ADR-0009（匿名读默认开） | `/metrics` 端点默认匿名可访问（与 `/healthz` 同级），可配 `metrics.require_auth=true` 限制 | 补充：不冲突 | — |
| ADR-0014（console 基线与 session 认证） | OIDC/LDAP 登录后生成同一 `binflow_session` cookie（与 ADR-0014 决策 2 同机制），Authenticator 增 OIDC/LDAP 臂 | 补充：不冲突 | — |
| ADR-0015（治理面）决策 2（配额） | S3 后端下配额计数逻辑不变（`repo_usage` 计数器与 node 增删同事务）；物理字节统计（`blob_bytes`）在 S3 后端下含义为「S3 上 blob 存储量」 | 补充：不冲突 | — |
| ADR-0017（镜像供应链） | M6 起 `bf` + `bf-migrate` 随 `binflow-server` 同版发布（goreleaser 三二进制 + checksums） | 补充：不冲突；goreleaser 配置扩展 | 实现层 |

### 6.2 性能（M6 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P26 S3 后端下 1000 并发拉取 | 与本地 filestore 同口径（G27），零错误、零 5xx | P0 |
| NFR-P27 S3 后端下冷启动 / 内存 | 冷启动 < 2s（含 S3 连通性检查）；空载 RSS < 100MB；1GB 流式 < 256MB | P0 |
| NFR-P28 S3 上传/下载吞吐 | 1MB 小文件上传吞吐 ≥ 本地 filestore 的 70%（网络延迟不可消除）；1GB 大文件上传吞吐 ≥ 本地 filestore 的 90%（S3 multipart 并发补偿） | P1 |
| NFR-P29 OIDC/LDAP 登录延迟 | 登录（含 IdP/LDAP 往返）< 3s（P95）；session 验证 < 5ms（本地查库，不走 IdP） | P1 |
| NFR-P30 复制延迟 | push 延迟（源上传到目标可见）< 60s（P50）；< 5min（P95） | P1 |
| NFR-P31 bf CLI 冷启动 | `bf --help` < 0.5s（不含网络请求） | P2 |
| NFR-P32 二进制体积 | `bf` 二进制压缩产物 < 15MB（六平台）；`bf-migrate` < 15MB；`binflow-server` 体积增量 < 2MB（S3/OIDC/LDAP/Prometheus 依赖） | P2 |

### 6.3 安全底线（M6 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S32 S3 凭据安全 | `access_key_id` / `secret_access_key` 通过 env 注入（`BINFLOW_S3_*`），不入 YAML；日志脱敏（不记录 S3 凭据） | 日志 grep + YAML grep |
| NFR-S33 OIDC client_secret 安全 | `BINFLOW_AUTH_OIDC_CLIENT_SECRET` 环境变量注入，不入 YAML；日志脱敏 | 同上 |
| NFR-S34 LDAP bind_password 安全 | `BINFLOW_AUTH_LDAP_BIND_PASSWORD` 环境变量注入，不入 YAML；日志脱敏 | 同上 |
| NFR-S35 OIDC PKCE + state 防护 | Authorization Code Grant + PKCE（S256）；state 参数（随机生成 + session 校验）防 CSRF | QA 验证 PKCE code_verifier 长度 ≥ 43 字符；state 校验失败拒绝回调 |
| NFR-S36 LDAP TLS 强制 | 默认 `skip_tls_verify=false`；`true` 时文档标注「仅限评估」 | H35 |
| NFR-S37 复制凭据安全 | 目标实例的 username/password 加密存储（AES-256-GCM，与 ADR-0012 remote 凭据同机制）；env 密钥 `BINFLOW_REPLICATION_CREDENTIALS_KEY` | 元数据库 grep 不到明文 |
| NFR-S38 S3 传输加密 | S3 请求强制 HTTPS（`endpoint` 以 `https://` 开头时）；MinIO 本地测试可 `http://` | H11 配置验证 |
| NFR-S39 迁移工具凭据安全 | `bf-migrate` 的 source/target 凭据仅通过命令行参数或环境变量输入，不落盘（不写入配置文件） | 迁移后检查 `~/.bf/` 无凭据残留 |

### 6.4 可观测性（M6 增量）

- **结构化日志**：保持 M1~M5 字段集（time/level/method/path/status/duration_ms/remote_addr/user）；新增字段：
  - `storage_backend`: `local` | `s3`（启动日志）
  - `auth_method`: `basic` | `bearer` | `session` | `oidc` | `ldap`（认证日志）
  - `replication_event`: `push` | `error` | `conflict`（复制日志）
- **Prometheus `/metrics`**：FR-61 定义的四类指标（HTTP/存储/认证/复制）。
- **健康检查**：`/health` 的 `storage` 子系统在 S3 后端下反映 S3 连通性；新增 `auth` 子系统（OIDC provider 发现可用 / LDAP 连接可用）。
- **审计日志**（`audit_events` 表）新增事件类型：
  - `auth.login.oidc` / `auth.login.ldap` / `auth.login.local`（登录成功）
  - `auth.failed`（认证失败，含 method 与 reason）
  - `replication.push` / `replication.error` / `replication.conflict`
  - `storage.migration.start` / `storage.migration.complete` / `storage.migration.error`
  - `storage.backend_switch`（backend 切换事件）

---

## 7. 开放问题（待用户定案）

| # | 问题 | 影响面 | 暂行假设（v1.0；v1.1 起标注实现状态） |
|---|---|---|---|
| Q1 | **S3 后端热切换 vs 冷切换**：运行时切换 `storage.backend` 需要重启？还是支持热切换（SIGHUP 重载配置）？ | FR-48/FR-50；运维体验 | **冷切换**（需重启）——M6 暂不引入 SIGHUP 重载，降低复杂度。重启后 `migration.enabled=true` 自动进入双写+迁移模式 |
| Q2 | **本地 filestore 的 sessions 是否统一为 DB 会话**：ADR-0006 决策 2 定为磁盘会话，S3 后端下必须入 DB。是否趁 M6 统一为 DB 会话？ | FR-48；ADR-0006 修订；双路径维护成本 | **建议统一为 DB 会话**（`upload_sessions` 表），本地 filestore 下也入 DB——降低双路径维护成本，状态恢复更可靠。需用户确认——**这是对 ADR-0006 的修订**
| Q3 | **S3 下 GC `--apply` 的安全确认**：S3 后端下 DeleteObject 不可逆（无回收站），是否需额外确认标志？ | FR-51；S3 数据安全 | **复用 `--apply` 标志**（与本地 filestore 一致），但 S3 后端下日志额外 WARN「S3 backend: blob deletion is irreversible」。不加 `--confirm-s3` 新标志，保持运维界面一致
| Q4 | **OIDC `admin_group` 映射的角色粒度**：`admin_group` 成员是获得完整 admin 权限，还是可配「只读 admin」？ | FR-54；权限模型 | **完整 admin 权限**（与本地 admin 用户同权）。更细粒度的角色（read-only admin / repo admin）归 M7+ 的 RBAC 里程碑
| Q5 | **OIDC 与 LDAP 用户名冲突处理**：同一 username 同时存在于 OIDC 和 LDAP 时，哪个优先？ | FR-56；用户映射 | **不允许冲突**——`username` 全局唯一。若 OIDC 用户与 LDAP 用户 username 相同，第二个登录的返回 409「username already exists with different source」。用户需在 IdP 侧或 LDAP 侧调整 username 避免冲突
| Q6 | **replica 仓库的仓库类型**：目标端 replica 仓库是 `rclass=local` + `replica=true` 标记，还是新增 `rclass=replica`？ | FR-57；仓库模型 | **暂行已实现，待终裁**（T-162 口径，2026-08-22 回写）：落法 = `target_repo` 指向**可写 local 仓**（backing，如 `replica-local`）+ 未配置写路由的 **virtual 只读门面**（members=[backing]）——「replica 只读 PUT/DELETE 405」复用 M3 virtual 既有行为，零仓库模型改动。**注意**：暂行下 backing local 本身仍可被目标实例上的用户直写（只有门面只读）。v1.0 暂行假设（`rclass=local` + `replica=true` 标记，不新增 rclass 枚举）**未实施**；终裁若选标记/新 rclass 路线，切换位在 `pushOnce` 的推送落点寻址（`PUT /binflow/{targetRepo}/{path}`，internal/replication/engine.go），引擎骨架不动 |
| Q7 | **复制冲突策略**：目标端存在同路径但不同 sha256 的制品时，覆盖还是跳过？ | FR-59；复制语义 | **暂行已实现，待终裁**（T-162 口径，2026-08-22 回写）：目标同路径已存在且 **checksum 一致 → 幂等成功**（零传输、不打开源 blob）；**不一致 → 任务终态 `status=failed`**（attempts 记满防 cron 复活、completed_at 置位）且**不动目标**。与 ADR-0021 决策 4 字面（比较 updated_at、first-write-wins、记 `skipped`）及本 PRD H47 的 `status=conflict` 词汇不一致——009 任务状态闭集（T-161）无 `conflict`，暂行取 failed + conflict 文案。终裁若选 skipped/覆盖，切换位在 `pushOnce` 的 HEAD 分支（internal/replication/engine.go），引擎骨架不动 |
| Q8 | **AWS S3 验收环境**：QA 需要 AWS 账号与 S3 bucket 用于验收——由谁提供？ | FR-53；QA 条件腿 | **优先 MinIO 本地容器**（P0 可自足）；AWS S3 验收为条件腿（用户提供 bucket 与凭据——拆票标注 `dep:用户环境`），到位前 MinIO 全序列 PASS 视为等价。与 M5 Q3 同口径 |
| Q9 | **Artifactory 迁移工具验收环境**：QA 需要 Artifactory 实例（含制品）用于验收——由谁提供？ | FR-63；QA 条件腿 | **用户提供 Artifactory 实例**（或 QA 用 Docker 自建 Artifactory OSS 容器 + 脚本填充 100+ 制品）。Docker 自建可覆盖 P2 验收；若需真实企业版 Artifactory 实例，拆票标注 `dep:用户环境`。与 M5 Q3 同口径 |
| Q10 | **复制目标私网地址默认放行**：复制目标几乎必然是内网/同主机 BinFlow 实例——`DenyPrivateTargets` 默认拒绝会使功能在所有现实部署不可用；但「默认放行私网目标」是否需要 config 显式开关与文档警示？ | FR-57；SSRF 面（NFR-S13） | **暂行已实现，待终裁**（T-162 口径，2026-08-22 增补）：`DenyPrivateTargets` 默认 `false`（私网目标放行，等价 Guard 的 AllowPrivateUpstream=true）——scheme/host 校验、逐跳重检、DNS-rebinding pinning 仍然生效。建议 config 桥接票增 `replication.allow_private_target`（默认 `true`）落到该选项（T-162 建议，未实施） |
| Q11 | **Token 端点权限 vs FR-54-AC3**（T-174 D3）：`POST /api/security/token` 实现现状 admin-only（router.go routeAuth，M1 子集决策的延续），普通 OIDC/LDAP 映射用户发 Token → 403，与 FR-54-AC3「OIDC 用户创建 Token → 200」（P0）冲突。裁决 A（开放，非 admin 可为本人发）还是 B（维持 admin-only + FR-54-AC3 勘误）？ | FR-54-AC3/OD-04/H26；internal/httpapi security 面 + 审计；M2 FR-11 双入口注记与 M1 E-17 子集注记（历史文档，随实现票收口） | **已裁决：A（开放），T-188，2026-08-22，PM 规格裁决**（FR-54-AC3 为已定 PRD 基线，A = 维持基线原文、纠正实现偏差，属 PM 权限内；用户明示推翻则转 B 路径勘误）。**依据链**：① Artifactory 对标（决定性）——auth-model.md §3.1（高置信度，代码+官方文档双证）：真实权限模型 = 「admin 全量；**非 admin 已认证用户可为自己发 token**；匿名拒绝」，且非 admin 受「只能填自己 username」与 TTL 上限（365d，`access.token.non.admin.max.expires.in`）双约束；B 路径前提「Artifactory 同样限制」与逆向规格事实相反，走 B 须把 M1 E-17 该端点改标「有意不兼容」，违背 PRODUCT.md 架构对齐原则与场景 F 迁移平移。② 威胁模型——开放不构成提权：非 admin Token 主体=本人、权限=本人动态权限（identity token 语义，与 ADR-0010 第 4/5 条 docker token 同构），能过 session 做的事才能过 Token 做，新增仅是既有能力的持久化；真实风险为「IdP 侧停用用户后 Token 存活至过期」与「session 被盗 → 铸 Token 持久化立足点」，以护栏 ②③④ 对冲；M7+ 可选加固（SSO session 铸 Token 需二次认证）不入 M6。③ M1/M2 既有决策——admin-only 非安全红线而是 M1「username 参数未实现」的子集简化（M1 E-17 明文「admin 无参创建即可」：无 username 参数即无法区分为自己/为他人，admin-only 是当时唯一安全落点）；且 M2 起 `/v2/token` 已允许任意有效用户铸有限 TTL Token（FR-11 v1.1 双入口设计，T-43 QA 2.12 PASS）——「SSO 用户不应持有 Token」在本产品从非不变量，管理面收紧并不缩小凭据面，只砍掉自助服务与统一审计入口。④ 产品场景——§3 场景 B 明文「CI 用 LDAP 服务账号绑 Token」（服务账号通常非 admin），B 路径下每个 CI Token 须 admin 代铸，违背 M6「企业就绪/SSO 免维护」目标。**裁决细则（A 落地口径）**：admin 全量（可代任何主体、可永不过期，现状不变）；非 admin 已认证（本地/OIDC/LDAP 三臂同权）可为**本人**铸 Token——缺省 `username`=本人，指定他人 → 403 OAuth 形；`expires_in` 必须 >0 且 ≤ 上限（K9，默认 365d），0/负值/超限 → 401 `invalid_request`（文案对齐 Artifactory「can only create user token with expires in larger than 0 and smaller than <max> seconds」）；匿名拒绝（现状不变）；列表 `GET /api/security/token` 与 `revoke` 维持 admin-only（§3.3/§3.4，现状不变）；**验证期护栏**——Token 验证须校验主体用户行存在且未禁用，否则 401；审计——非 admin 铸 Token 同样记 `token.issue`（M5 G31a 既有 actor/fingerprint/TTL 面）。**实现票草案 AC**（conductor 分配编号；建议 `role:dev-go-core` `area:internal/httpapi + internal/auth`，改 router.go token 路由 routeAuth + security.go 主体判定 + 验证链用户行检查）：(1) ssouser（OIDC session，非 admin）`POST $BASE/binflow/api/security/token -d 'grant_type=client_credentials'` → 200（access_token/token_type/scope/token_id，与 T-174 H26 admin 腿同构）→ `curl -H "X-JFrog-Art-Api: $T" -X PUT $BASE/binflow/<有权repo>/t.bin` 201、GET 200、无权限路径 403；(2) jdoe（LDAP，非 admin）同 (1) 全链；(3) 本地非 admin（ci-bot）同 (1)——三臂同权 + M1 C21a/C21c admin 腿回归不破；(4) 负面腿：非 admin 带 `username=<他人>` → 403、`expires_in=0` → 401 invalid_request、`expires_in` 超上限 → 401、匿名 POST → 401；(5) 禁用即失效：admin 禁用该 OIDC 用户行后其既有 Token 全部 → 401；(6) 审计：非 admin 铸 Token 后 `GET /api/v1/audit?action=token.issue` 可见该事件（actor=用户名，含 TTL/fingerprint）；(7) revoke 回归：admin 吊销任意 Token 幂等语义不变（C21c）。**T-174 H26 定格**：维持「部分过」——admin_group 腿 ✅，普通 OIDC 用户腿按本裁决从「规格冲突悬置」转「实现票待修 FAIL 项」；D3 分级从 P0(规格冲突) 降为 P1(实现差距)。**推翻出口**：用户如明示选 B，本条转勘误记录——FR-54-AC3 改为「admin_group 映射用户可发 Token（H26 已验证），普通 OIDC/LDAP 用户 403 属预期」，OD-04 补「有意不兼容（权限模型收紧于 Artifactory）」标注，M1 E-17 兼容层级同步加注 |

---

## 8. M6 验收剧本（QA 总纲）

1. **回归基线（本地 filestore）**：M1~M5 全部 P0 序列复跑全绿（H68——M6 回归硬门槛）。
2. **S3 存储后端**：H01~H06（S3 下全序列等价）→ H07~H11（配置与健康检查）→ H12~H15（本地→S3 迁移）→ H16~H18（去重与 GC）→ H19~H21（性能基线）→ H22~H23（兼容性列表）。
3. **OIDC 认证**：H24~H25（登录流 + 用户映射）→ H26（Token 创建）→ H27（admin 映射）→ H28（groups 同步）→ H29（开关）。
4. **LDAP 认证**：H30~H31（登录流 + 用户映射）→ H32（密码错误）→ H33（admin 映射）→ H34（连接断开）→ H35（TLS）。
5. **认证臂优先级**：H36~H38（三种用户并存 + fallback 链 + 审计）。
6. **复制/联邦**：H39~H42（push 复制 + 只读 + 最终一致）→ H43（幂等）→ H44~H45（事件查询 + 状态）→ H46（控制台面板）→ H47（冲突处理）→ H48~H51（多协议覆盖）。
7. **Prometheus 指标**：H52~H53（`/metrics` 端点 + 格式）→ H54（认证）→ H55（复制指标）。
8. **bf CLI**：H56（--version + --help）→ H57~H60（四个子命令）→ H61（profile 切换）。
9. **迁移工具**：H62（非空拒绝）→ H63（仓库迁移）→ H64（报告）→ H65（用户迁移）→ H66（Token 迁移）→ H67（dry-run）。
10. **S3 后端下全量回归**：H69（M1~M5 全部 P0 序列在 S3 后端下复跑全绿）。
11. **文档**：tech-writer 产出 OIDC 配置指南（含 Keycloak/Okta/Azure AD 示例）、LDAP 配置指南（含 AD/OpenLDAP 示例）、S3 配置指南（含 AWS S3/MinIO 示例）、bf CLI 手册、迁移指南、Prometheus 指标参考。

---

## 9. M6 DoD

1. §4 全部 P0 AC 经 qa 验证全绿；P1 除「条件腿」外全绿；P2 至少 `bf` CLI 四个子命令与迁移工具 1 仓库迁移通过（P2 延后须在 BOARD 记录）；
2. §8 剧本全绿，§5.3 分级矩阵 curl+docker+mvn+npm+pip 回归基线（本地+S3）全绿；Playwright OIDC/LDAP 登录全绿；MinIO S3 全序列全绿；复制 P1 全绿；Prometheus 指标 P1 全绿；
3. **条件腿处置**（AWS S3 / 迁移工具）：Q8/Q9 用户环境到位 → 实测记录；未到位 → 按 M5 Q3 降级口径归档（MinIO 等价 + 静态验证 + 用户确认），环境到位后补跑；
4. tech-writer 文档 5 类齐备（OIDC 配置指南 / LDAP 配置指南 / S3 配置指南 / bf CLI 手册 / 迁移指南）；
5. architect 完成 ADR-0018（S3 adapter 与 sessions 统一）、ADR-0019（S3 下 GC 语义）、ADR-0020（OIDC 提供者模型）、ADR-0021（LDAP 绑定与搜索）、ADR-0022（复制事件模型）、ADR-0023（Prometheus 指标规范）——共 6 条新 ADR；
6. 安全审计：S3 凭据 + OIDC client_secret + LDAP bind_password + 复制凭据 四类凭据全部 env 注入、不入 YAML、日志脱敏；PKCE + state 防护、LDAP TLS 强制、S3 传输加密全面验证；
7. 主会话完成 `m6-done` tag。

---

*本 PRD v1.0 由 product-manager（T-148）依据 PRODUCT.md、ROADMAP.md M6+ 展望、M1~M5 交付基线、DECISIONS.md 全部 ADR 撰写；开放问题 Q1~Q9（v1.1 增补 Q10）待用户定案后回写。与既有 ADR 的冲突/补充点见 §6.1 标注表。v1.1 勘误由 product-manager（T-181）回写：FR-50 迁移端点契约对齐实现（architecture.md §7.1 T-176 回写版）+ Q6/Q7/Q10 暂行口径（reports/agents/T-162.md）。v1.2 规格裁决由 product-manager（T-188）回写：Q11（token 端点权限 vs FR-54-AC3，T-174 D3）裁 A 路径开放 + 四条护栏 + 实现票草案 AC，依据 reports/agents/T-174.md §3 与 docs/reverse/auth-model.md §3.1。*