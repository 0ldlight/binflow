# 架构决策记录（ADR）

> architect 维护。每条决策一条 ADR，按序号递增，只追加不删改（推翻旧决策时新增一条并标记旧条 Superseded）。

## 模板

```
## ADR-<序号>: <决策标题>
- 状态: Proposed | Accepted | Superseded by ADR-<n>
- 日期: YYYY-MM-DD
- 背景: 遇到什么问题、什么约束
- 候选方案: A / B / C 各自优劣
- 决策: 选了什么
- 理由: 为什么赢出
- 后果: 带来的影响、需要遵守的约定
```

---

## ADR-0000: 采用「AI 研发团队 + 看板单写者 + area 分区」工作流
- 状态: Accepted
- 日期: 2026-08-17
- 背景: 多个 AI agent 并行开发同一代码库，需要防冲突、可恢复、可审计的流程。
- 候选方案: A) 单 agent 串行完成所有事；B) 多 agent 自由协作；C) 主会话统一编排 + 角色分工 + 状态落盘。
- 决策: 选 C。
- 理由: 保留并行效率，同时用「BOARD.md 单写者」「ticket area 不重叠」「全部状态写文件」三条约束消除写冲突与状态丢失。
- 后果: 每轮迭代必须更新看板与报告；agent 间不直接通信，全部通过主会话中转。

## ADR-0001: 逆向采用 clean-room 流程（规格与实现分离）
- 状态: Accepted（用户指定基线）
- 日期: 2026-08-17
- 背景: BinFlow 需要对齐 Artifactory 行为，参考材料是 `reverse-src/` 下的反编译 Java 代码。直接翻译/复制反编译代码有版权与许可风险，且会把 Java 的结构包袱带进 Go 实现。
- 候选方案: A) 逐模块翻译反编译代码；B) 只看公开文档实现；C) clean-room：反编译仅用于产出行为规格，规格与实现分离。
- 决策: 选 C。reverse-engineer 产出 `docs/reverse/*.md` 行为规格（端点表、存储布局、语义流程），实现者只依据规格与官方协议文档编码；有公开规范的能力（Docker Registry v2 / Maven 2 / npm / PyPI）一律以官方规范为准。
- 后果: 每个协议/领域先有规格再有实现 ticket；规格必须标注置信度（高=代码+公开文档双证 / 中=仅代码 / 低=推测）；实现不得与反编译代码逐行对应；`reverse-src/` 永不入库。

## ADR-0002: Go 模块化单体，单二进制交付
- 状态: Accepted（用户指定基线，architect 细化）
- 日期: 2026-08-17
- 背景: 对标 Artifactory（Java/Tomcat/JVM，部署重、启动慢、内存高）。BinFlow 的差异化之一是交付形态。
- 候选方案: A) Go 微服务；B) Go 模块化单体；C) Java 同栈重写。
- 决策: 选 B。`cmd/binflow-server` 单入口；`internal/` 分包（config / storage / metadata / repo / adapter / auth / audit / httpapi / console）；包边界即并行开发的 area 边界；Web 控制台构建产物用 go:embed 打入二进制。
- 理由: 单二进制是产品差异化（零依赖、冷启动快、内存小一个数量级）；此规模上微服务是过度设计；沿用 Java 违背选 Go 的初衷。
- 后果: 模块间只通过接口依赖，禁止跨包摸内部结构；未来如需拆分保留可能（ADR 可推翻）。

## ADR-0003: 概念模型与 Artifactory 一一对齐
- 状态: Accepted（用户指定基线）
- 日期: 2026-08-17
- 背景: 产品要求架构与 Artifactory 一致，便于迁移与文档类推。
- 决策: 仓库三型 local / remote / virtual；checksum 寻址的文件存储（sha256 主键，sha1/md5 附属校验）；元数据库内嵌 SQLite（默认、零依赖）+ Postgres（可选）；users/groups/permissions/tokens 权限体系；REST 兼容 Artifactory 高频子集 + 自有 `/api/v1`。
- 后果: API 与配置命名沿用 Artifactory 术语（repo key、node、checksum…）；元数据 schema 需在 M1 由 architect 定稿。

## ADR-0004: 部署矩阵（多元化部署）
- 状态: Accepted（用户指定基线，release-engineer 落地）
- 日期: 2026-08-17
- 背景: 用户明确要求多元化部署方式；目标环境含 K8s、compose、裸机、离线网络。
- 决策: GA（M5）必须交付：① 单二进制（linux/darwin/windows × amd64/arm64，goreleaser + 校验和）② Docker multi-arch 镜像（distroless / alpine）③ docker-compose ④ Helm Chart（PVC/ingress/HPA/values）⑤ 原生 K8s 清单 ⑥ systemd 单元 + 安装脚本；另产出离线安装包（镜像 tar + Chart + 脚本 + 校验和）。
- 后果: release-engineer 常设；`deploy/` 与 `charts/` 目录纳管；M2 起每个里程碑包含部署烟测票；对外推送镜像/Chart/二进制必须经用户确认。

## ADR-0005: 零 CGo 纯 Go 依赖基线（driver / YAML / 路由选型）
- 状态: Accepted
- 日期: 2026-08-17
- 背景: ADR-0004 要求 6 平台交叉编译单二进制（linux/darwin/windows × amd64/arm64）。CGo 依赖（如 mattn/go-sqlite3）会让每种目标平台都需要本地 C 工具链，破坏 goreleaser 交叉编译与离线构建的可复制性。同时首个外部依赖需要确立「依赖准入原则」，避免依赖树失控。
- 候选方案:
  - SQLite 驱动：A) mattn/go-sqlite3（CGo，最快但破坏交叉编译）；B) modernc.org/sqlite（纯 Go 转译，无 CGo，写路径略慢）；C) ncruces/go-sqlite（WASM，性能介于两者但运行时更重）。
  - YAML：A) gopkg.in/yaml.v3（**已归档不维护**，CVE-2022-28948 无上游修复）；B) go.yaml.in/yaml/v3（YAML 官方组织 fork，drop-in 继任，活跃维护）；C) goccy/go-yaml（重写实现，功能多但 API 面大）。
  - HTTP 路由：A) Go 1.22+ stdlib `net/http.ServeMux`（方法匹配 + `{path}` 通配已内建）+ 自写约 30 行 middleware chain；B) go-chi/chi（仍维护，中间件/子路由更强）；C) gorilla/mux（历史包袱）。
- 决策: 全部选纯 Go 路线——`modernc.org/sqlite`（已验证 `go get` 解析到 v1.56.0）、`go.yaml.in/yaml/v3`（已验证 v3.0.5）、stdlib ServeMux + httpapi 包内极简 middleware chain。**依赖准入原则**：默认 stdlib；引入任何新外部库必须由 architect 记录（追加到本 ADR 附属清单或新 ADR）；CGo 一律禁止。首批依赖白名单：`modernc.org/sqlite`、`go.yaml.in/yaml/v3`、`golang.org/x/crypto`（argon2id 密码哈希）。
	- **M6 准入（T-149，2026-08-21）**：以下三个依赖由 ADR-0018/ADR-0020 批准，`go get` 验证通过（零 CGo，`CGO_ENABLED=0 go build ./...` 通过）：
	  - `github.com/minio/minio-go/v7`（纯 Go S3 SDK，ADR-0018 选型）
	  - `github.com/coreos/go-oidc/v3`（纯 Go OIDC 客户端，ADR-0020 选型）
	  - `github.com/go-ldap/ldap/v3`（纯 Go LDAP 客户端，ADR-0020 选型）
	  - 新依赖白名单完整列表：`modernc.org/sqlite`、`go.yaml.in/yaml/v3`、`golang.org/x/crypto`、`minio-go/v7`、`go-oidc/v3`、`go-ldap/ldap/v3`
	- **M11 准入（T-301，2026-08-26）**：以下依赖由 ADR-0038 批准，`go get` 解析 + `CGO_ENABLED=0` 三平台（darwin/arm64、linux/amd64、windows/amd64）构建验证通过（goproxy.cn 镜像，2026-08-26 实测）：
	  - `github.com/ProtonMail/go-crypto`（纯 Go OpenPGP 实现，ADR-0038 选型；`golang.org/x/crypto/openpgp` 已弃用〔Go proposal #44226〕的活跃继任）
	  - 随行传递依赖一并准入：`github.com/cloudflare/circl`（纯 Go，Ed448/X448/Curve25519 互操作）；`golang.org/x/crypto`、`golang.org/x/sys` 已在树
	  - 白名单现行全集（截至 M11 规划期）：`modernc.org/sqlite`、`go.yaml.in/yaml/v3`、`golang.org/x/crypto`、`minio-go/v7`、`go-oidc/v3`、`go-ldap/ldap/v3`、`ProtonMail/go-crypto`（+ circl）
- 理由: 6 平台矩阵下无 CGo 是硬收益；BinFlow 元数据负载是小事务 OLTP 而非分析查询，modernc 的写性能折损可接受；YAML 选官方继任 fork 迁移成本最低且持续收安全修复；M1 路由需求是「前缀挂载 + 少量 REST 模式」，1.22+ ServeMux 足够，少一个依赖就少一分供应链风险。
- 后果: 所有构建环境无 C 工具链要求；受限网络下需配置 `GOPROXY` 镜像（验证时 goproxy.cn 可用、proxy.golang.org 超时——devops-engineer 需在 CI 与文档中体现）；若 M3 npm/Maven 出现 ServeMux 表达不了的匹配需求，再评估引入路由库（届时新 ADR）；性能基准（M5）若显示 SQLite 写瓶颈再评估驱动替换。

## ADR-0006: blob 存储磁盘布局与上传落盘协议（布局即兼容契约）
- 状态: Accepted
- 日期: 2026-08-17
- 背景: checksum 寻址去重是 ADR-0003 定下的核心能力。磁盘布局一经发布就是事实兼容承诺——后续版本的升级、GC、备份/恢复工具（M4）都依赖它，M1 必须定稿。
- 候选方案:
  - blob 目录：A) 全平铺 `blobs/<sha256>`（单目录百万级文件，多数文件系统退化）；B) 二级分片 `blobs/<2-hex>/<sha256>`（256 目录，均衡）；C) 按仓库分目录 + 硬链接去重（Artifactory 旧式 filestore，跨仓去重依赖链接、备份语义复杂）。
  - 会话状态：A) DB 表（每次 append 都写库，崩溃恢复依赖库可用）；B) 磁盘 `sessions/<id>/state.json`（存储自包含，重启即恢复）。
  - 引用计数：A) `blobs.ref_count` 列实时维护（每次 node 增删都写同热行，锁竞争）；B) 无计数列，GC 时与 `nodes` 实时 JOIN（mark-sweep）。
- 决策:
  1. 布局：`<data>/blobs/<sha256[0:2]>/<sha256>`，blob 文件内容不可变、全局唯一、无 sidecar 属性文件（附属 sha1/md5/size 存元数据 `blobs` 表，元数据库是唯一事实源）。
  2. 会话：`<data>/uploads/<uuid>/data`（瞬态数据）+ 元数据库 `upload_sessions` 表（状态），磁盘 `sessions/<uuid>/{data,state.json}` 纯磁盘方案废止（2026-08-22，T-209 勘误）；启动时扫描，过期（ttl，默认 24h）即清理。
  3. 落盘协议：session 追加写 `data`（边写边算 sha256/sha1/md5）→ fsync(data) → 若目标 blob 不存在则 `rename` 到 blobs 路径 → fsync(目标目录) → 提交元数据。同一文件系统内 rename 原子，崩溃只会留下 session 残渣或已完整 blob，无半写文件。
  4. 并发同一 blob：进程内 per-checksum singleflight 互斥；后到者发现目标已存在 → 丢弃自己的会话数据、返回已有 blob（幂等）。
  5. 引用与 GC：不设 ref_count 列；GC 为离线 CLI（`binflow-server gc`，默认 dry-run，`--apply` 才删），mark-sweep：未出现在 `nodes` 中且 `created_at < now - grace_period`（默认 24h，防与在途上传竞态）的 blob 才可删。
- 理由: 分片布局消除单目录规模问题且推导简单；全局内容寻址让跨仓去重、零拷贝移动/重命名（只改 nodes 行）免费获得；会话留磁盘使存储引擎可独立于元数据库测试与恢复；mark-sweep + grace period 用时间换锁，避免为 M1 引入分布式锁或引用计数热点。
- 后果: 备份 = `blobs/` 目录 + SQLite 文件的一致性快照（M4 交付工具）；blob 物理删除只有 GC 一个入口（运行时 DELETE 制品只删 node 引用，安全底线友好）；`blobs/` 是版本兼容承诺，改名需新 ADR；磁盘 session 目录（`sessions/`）**不是**版本兼容承诺——session 状态 DB 化为迁移边界（2026-08-22，T-209 勘误，见决策 2）；「Artifactory 是否有 blob sidecar 属性文件」待逆向规格 docs/reverse/storage-layout.md 确认（若其有 properties 文件，BinFlow 也不跟进——以本决策为准，差异记入架构文档对齐表）。
- 勘误（2026-08-17，T-25 依 T-9 review 落地，不推翻决策本体）: ① 决策 5 的 grace 基准明确为 **blob 文件 mtime**（非 `blobs.created_at` 列——storage 不读 DB 的必然选择，方向安全：mtime 被推新只会多保留）；mark 形态升级为**引用集合回调**（调用方一次 `SELECT DISTINCT sha256 FROM nodes`）取代逐条反连接；grace/ttl 零值 = 默认 24h。② **备份/恢复硬约束**：必须保留 blob 文件 mtime（`tar` / `rsync -a` 默认保留），否则恢复后宽限期时钟重置、全部历史 blob 立即可回收——M4 备份票与 ops 文档必须遵守。③ 会话瞬态文件 state.json 的形状由架构 §4.1 契约定稿（`version` 字段 + 演进规则）；session 目录命名仍是兼容承诺，state.json 内容不是（此句已被 2026-08-22 T-209 勘误修订：磁盘 session 目录不再兼容承诺，状态 DB 化——见决策 2）。④ 停机语义（2026-08-23，ADR-0028）：`Engine.Close` 不再清除未过期上传会话（`upload_sessions` 行 + `uploads/<id>/` 数据文件保留，回收唯一路径 = 启动 sweep + TTL）——「跨重启续传」不再限定异常中断；本决策的布局/落盘协议/sweep 语义本体不变。

## ADR-0007: 元数据迁移机制与 SQLite 并发策略
- 状态: Accepted
- 日期: 2026-08-17
- 背景: ADR-0003 要求 SQLite（默认）/ Postgres（可选）双栈；schema 演进必须保证跨里程碑升级不丢数据。验收标准含 1000 并发拉取，SQLite 并发策略需先定。
- 候选方案:
  - 迁移工具：A) golang-migrate / B) goose（均为成熟库，但 embedded + 双方言场景下仍需自建约定，且引入 CLI 依赖）；C) 自写约 100 行：embedded SQL 按序号在事务中应用并记录版本。
  - 并发：A) 单连接全串行（正确但读吞吐受限）；B) WAL + busy_timeout + 单连接池（读并发，写靠超时重试）；C) 读写双池（读池 N 连接 + 写池 1 连接，最优但 M1 复杂度不成比例）。
- 决策: 自写迁移器：`internal/metadata/migrations/sqlite/NNN_*.sql` embedded FS，逐版本事务应用，记录进 `schema_migrations` 表；方言目录 `migrations/postgres/` 必须同版本号同步演进（M1 仅交付 sqlite 方言 + postgres 占位说明），逻辑 schema 以 docs/design/architecture.md 为契约。SQLite 连接统一 PRAGMA：`journal_mode=WAL`、`foreign_keys=ON`、`busy_timeout=5000`；连接池 `MaxOpenConns = NumCPU`（M1 不拆读写池）。时间戳一律 RFC3339 UTC 文本列（SQLite/Postgres 同构）。
- 理由: 迁移需求就是「顺序执行 SQL 并记账」，三方库的迁移即代码/多驱动能力用不上；WAL 下现代c 驱动支持多读并发，写冲突是小事务 + busy_timeout 可吸收的；双池留作已识别的优化缝，不过早付费。
- 后果: 迁移文件成为受 architect review 的工件（每次 schema 变更两方言同步提交）；1000 并发验收若出现 `SQLITE_BUSY` 热点，升级到读写双池属允许的实现内优化（不改本决策）；Postgres 接入前，任何 SQLite 专有特性（如 `AUTOINCREMENT`、`strftime`）不得进入迁移 SQL，两方言共同子集为准。
- 附注（2026-08-17，用户定案 Q5 补充记录）: 元数据嵌入式选型时用户曾考虑 Derby / H2——两者均为 Java 系嵌入式库，无 Go 绑定、无法嵌入 Go 进程，**结构上不可行**，予以排除（非优劣权衡而是硬约束淘汰）。CGo 版 mattn/go-sqlite3 因破坏 goreleaser 六平台交叉编译（每种目标平台需本地 C 工具链）同样排除。最终维持本 ADR 决策：`modernc.org/sqlite` 纯 Go 转译实现，已实测解析 v1.56.0。
- 勘误（2026-08-17，T-25 依 T-10 review B1/B2 落地，决策方向不变、机制表述升级）: ① 决策中「SQLite 连接统一 PRAGMA」的原表述不完备——`journal_mode=WAL` 是**库级**，而 `foreign_keys=ON`、`busy_timeout=5000` 是**每连接**设置；per-connection PRAGMA **必须经 DSN `_pragma=...` 形态下发**（modernc 驱动在池内每条新连接上重放），用 `db.ExecContext` 打 PRAGMA 只会配置池中第一条连接，扩池后 FK 约束在其余连接上**静默失效**（T-10 review 实测发现）。现行为：`file:...?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=case_sensitive_like(ON)`，池 `MaxOpenConns=NumCPU`。② 新增第三条 per-connection PRAGMA `case_sensitive_like=ON`（T-10 review B1）：SQLite `LIKE` 默认对 ASCII 大小写不敏感，曾致 `ListByPrefix`/`DeleteByPrefix` 跨大小写误匹配（前缀删除静默多删行，实测证据）；开启后 LIKE 臂与 `=` 精确臂同为二进制比较，与制品 path 大小写敏感语义一致。③ M1 事务边界裁定（review M1，采纳 a 案）：blob link 与 node 两语句、blob-first 顺序、FK 兜底，不设 Txn 接口方法。④ 迁移文件体内禁止自带 BEGIN/COMMIT（迁移器已包事务，嵌套即错）。

## ADR-0008: 统一 `/binflow` 路由前缀与 Go module 路径
- 状态: Accepted（用户定案）
- 日期: 2026-08-17
- 背景: 需要为「内容路径 + Artifactory 兼容层 + 自有 API」定顶层命名空间；Go module 路径须在脚手架票开工前定值，否则事后全局 rename。
- 候选方案:
  - 前缀: A) `/artifactory`（最大化与迁移方文档类推，但品牌错位且暗示全量兼容）；B) 自有前缀 `/binflow`（兼容端点 `/binflow/api/...`、内容 `/binflow/<repo>/<path>`、自有 `/binflow/api/v1/...`）；C) 无前缀挂根（`/api`、`/v2`、`/<repo>` 混布，与其他服务同域共存时路由冲突）。
  - module 路径: A) `github.com/lzwzzy/binflow`；B) 自有域（如 `binflow.dev/binflow`，需购域维护）。
- 决策: 前缀选 B，module 路径选 A（`go.mod: module github.com/lzwzzy/binflow`）。所有产品端点统一 `/binflow` 前缀；**不用 `/artifactory` 前缀，不做根路径镜像**。探针/抓取基础端点（`/healthz` `/readyz` `/metrics`）不带前缀。repo key 保留字：`api`、`v2`（建仓校验拒绝）。
- 理由: 自有品牌命名空间在同域反代/子路径部署下无冲突；不做根镜像避免双份路由表与歧义；兼容性靠端点行为对齐（`/binflow/api/...` 上的兼容子集）而非前缀伪装。module 路径即刻定值，脚手架票直接使用。
- 后果: 文档与客户端示例统一 `/binflow`（例 `curl http://host:8080/binflow/<repo>/path`）；`server.base_url` 非空时必须含 `/binflow`；**[M2 风险预告]** docker 客户端固定向 `/v2/...` 发请求、无法自定义前缀，届时二选一：反代 rewrite 到 `/binflow/v2` 或为 `/v2` 开根级例外——属实现层路由例外，不推翻本 ADR，M2 出细化票据时定；控制台相对路径以 `/binflow` 为基。（repo key 长度上限后经 T-22 回写为 `{1,62}`，见 PRD FR-3-AC4。）（增补 2026-08-20，T-108：repo key 保留字并集定稿为 **{api, v2, docs, console, ui}**——api/v2 本 ADR、docs 为 ADR-0011、console/ui 为 ADR-0014 勘误后挂载段；存量库已有保留字同名仓时启动 WARN、路由仍占用，详见 M4 PRD §3。）（再增补 2026-08-20，T-110：并集增 **`assets`** → {api, v2, docs, console, ui, assets}——`assets` 与 `ui` 同构被 `/binflow/assets/**` SPA 指纹资产前缀遮蔽，不保留则建仓成功但内容永不可达（T-91 架构 review 遗留①，安全 review 同判）；代码侧 reservedRepoKeys 由 T-91 修复轮同步。）

## ADR-0009: 匿名读默认开启 + admin 首启口令引导
- 状态: Accepted（用户定案）
- 日期: 2026-08-17
- 背景: 两件事：① 内网/迁移场景的默认访问姿态（Artifactory 传统匿名读开）；② admin 首启口令需兼顾「部署矩阵 15 分钟跑通」与不引入静默弱口令。
- 候选方案:
  - 匿名策略: A) 匿名读默认开——仅内容路径 GET/HEAD，管理 API 永远认证，配置可关；B) 默认全拒绝（最安全，迁移体验降级）；C) 全端点匿名（不可接受，直接排除）。
  - admin 引导: A) 无缺省、env 未设则启动失败（最安全，评估/文档流程多一步）；B) env `BINFLOW_ADMIN_PASSWORD` 优先，未设置用**文档化缺省值 `password`**（文档标注仅限评估）；C) 首启随机口令打日志（K8s/compose 自动化不友好）。
- 决策: 匿名选 A（`auth.anonymous_read` 默认 `true`，只放行内容路径 GET/HEAD；写操作与 `/binflow/api/**` 不受开关豁免，一律认证）；admin 引导选 B（env 优先 → 缺省 `password`；仅当 admin 用户不存在时生效；检测到缺省值时启动日志打 WARN）。
- 理由: 匿名读默认对齐 Artifactory 迁移场景的主流姿态，且暴露面被精确限定在只读内容；缺省口令保住「按文档 15 分钟跑通」的产品成功标准，env 覆盖给生产，WARN + 文档标注兜底提醒。
- 后果: qa 必须覆盖匿名矩阵（匿名 GET 内容 200 / 匿名 PUT 401 / 匿名调 API 401 / `anonymous_read:false` 后 401）；审计事件 actor 记 `anonymous`；tech-writer 安装文档必须标注缺省口令仅限评估；`BINFLOW_ADMIN_PASSWORD` 只走 env 不入 YAML（秘密不入配置文件原则，见 architecture.md §8）。（后经 T-22 回写：配置键用户可见形态统一为 `security.anonymous_access` / env `BINFLOW_SECURITY_ANONYMOUS_ACCESS`，`auth.anonymous_read` 降为兼容别名，config 包双键等价且两键同给不一致时启动报错。）（T-14 review 终判补充：repo 行查询位于授权门之后，RepoLookup 用 metadata.Get 是有意为之的匿名读前置缝——无权者在查询前即被拦截，安全面成立。）

## ADR-0010: docker /v2 根级例外挂载与 token 认证流（ADR-0008 风险预告定案）
- 状态: Accepted
- 日期: 2026-08-18
- 背景: ADR-0008 预告的 M2 风险到期定案。docker 客户端（及 podman/crane/skopeo/oras）按 Registry spec 硬编码向 `/v2/...` 发请求且无法配置前缀；BinFlow 全部产品端点统一 `/binflow` 前缀。两者必须调和，且 `docker login` 的 Bearer 协商地址取自 `Www-Authenticate` 响应头（realm=...），任何路径改写都必须同步考虑该头。
- 候选方案:
  - a) 根级例外：httpapi 为 `/v2/**` 开一条不剥前缀的路由特例，docker handler 直接挂根。
  - b) 反代 rewrite：部署层（nginx/traefik）把 `/v2/` 转 `/binflow/v2/`，应用内只有统一前缀；裸部署（无反代）时 docker 不可用。
  - c) 双挂载：应用内同时挂 `/v2/**` 与 `/binflow/v2/**` 两套路由。
- 决策: 选 **a) 根级例外**（应用内实现，非部署层依赖）。具体：
  1. `/v2/**` 在 httpapi 是与 `/binflow` 平行的**根级例外路由**（与 `/healthz` 同类，属「客户端协议硬编码」豁免类），进入同一 middleware 链（logging/recover/auth），但**不剥 `/binflow` 前缀**。路由表层面：`/v2/` 首段即 docker adapter 专属，repo key 保留字 `v2` 维持（ADR-0008 不变）。
  2. `/binflow/v2/**` **不再提供**（不选 c：双挂载 = 双份 Location/Www-Authenticate 头生成逻辑与双份 QA 面，且 `/binflow/v2` 本就无客户端会走——docker 不认、curl 用户走 `/binflow/api`）。ADR-0008 的「统一前缀」表述由本 ADR 修订为「统一前缀 + `/v2` 协议硬编码例外」，性质同 `/healthz`。
  3. **repository name 即 repo key（默认）**：docker 镜像名 `<host>/<repo-key>/<image...>` 的**首段**映射 BinFlow repo key（如 `registry.example.com/binflow-dev/ubuntu` → repo `binflow-dev`）。Layout 解析：`/v2/<name>/manifests/<ref>` 与 `/v2/<name>/blobs/<digest>` 的 name 按 `/` 切分，首段为 repoKey，余段为镜像相对名；name 为单段（无 `/`）时 M2 返回 404（不支持顶层裸名， Artifactory 亦要求 repo 前缀；待逆向规格 T-31 校准）。
  4. **token 端点 `/v2/token`（Adapter 自有，非 RFC 标准）**：docker login 流为——客户端匿名打 `/v2/` → 401 + `Www-Authenticate: Bearer realm="<base_url>/v2/token",service="binflow",scope="<repo>:pull,push"` → 客户端携 Basic 凭据 GET realm（带 service/scope 参数）→ 返回 `{"token":"...","expires_in":..}`（兼容字段 `access_token` 同值）→ 后续请求 `Authorization: Bearer <token>`。BinFlow 的 token 签发**复用 auth.TokenRegistry**（不平行一套）：docker token = 有限时 TTL 的 BinFlow token + scope 声明，Verify 时解出 scope 参与授权。因 realm 指向的是 BinFlow 自身路径 `/v2/token`（无 rewrite），`Www-Authenticate` 头**无需重写**——这是选 a 而非 b 的决定性附带收益。
  5. scope → Authorizer.Can 映射：`pull` → action `r`、`push` → action `w`（manifest DELETE → `d`）。scope 的 subject 是 `<repoKey>/<image>`，映射时 repoKey 段交给 Authorizer 的 repoKey 参数、余段并入 path。无 scope 的 token（纯 `docker login`）只证明身份，授权仍逐请求判定。
  6. 反代部署（b 的部署层形态）**不禁止但非必需**：用户已有 nginx 前置时可 `proxy_pass` 直通 `/v2/`（不 rewrite），compose 产物默认不加反代组件（见 §9 影响）。
- 理由: docker 生态是 M2 的旗舰验收面（真实客户端 conformance 全过），可用性必须在**裸单二进制**形态下成立——b 会让「单二进制直接 docker push」不可用，直接违背产品核心场景；c 的双路由面在 Location 头、Www-Authenticate realm、catalog 分页链接三处都要双份生成与双份测试，纯成本。a 的「例外」有精确边界（仅 `/v2` 首段、仅 docker adapter、进同一 middleware 链），不侵蚀统一前缀的其余承诺。
- 后果: httpapi 路由表新增根级例外段（实现层）；`server.base_url` 语义不变（realm 生成用它）；文档（tech-writer）需写明 docker 客户端用 `docker login <host>` 直连、curl 用户统一 `/binflow`；QA conformance 面只测 `/v2`（无 `/binflow/v2` 面）；ADR-0008 的统一前缀表述以本 ADR 为准修订（不推翻，例外化）。

## ADR-0011: 帮助文档中心采用 Docusaurus
- 状态: Accepted（用户定案）
- 日期: 2026-08-19
- 背景: M5 GA 要求交付帮助文档中心（docs/user/，PRODUCT 核心能力 7 的文档维度：安装指南、协议接入、管理、API 参考、FAQ）。文档从 M1 起就以 Markdown 累积（docs/user/ 已有导航结构与篇目规划），M5 需要把它变成可导航、可搜索、随版本演进的站点；同时要裁决文档站的架构接点（交付形态与源文件工作流），避免与「单二进制差异化」冲突。
- 候选方案:
  - 静态站点生成器：A) **Docusaurus**（React/MDX、内建版本化文档与 i18n、文档站事实标准）；B) Hugo/Jekyll（更轻、构建快，但无 React 生态、版本化文档要自建、MDX 组件不可用）；C) 纯 Markdown 目录（零构建，GitHub/GitLab 直接渲染，但无侧边栏导航/全文搜索/版本切换，非产品级形态）。
  - 交付形态：a) 独立静态站点部署（docs.example.com，Vercel/Netlify/nginx 静态托管）；b) **build 产物 go:embed 进 binflow-server，服务自带 `/docs` 路由**；c) 双轨（embed + 独立站）。
  - 源文件位置：a) docs/user/ 保持纯 Markdown 源 + Docusaurus 配置目录分离；b) 源整体迁入 Docusaurus 站点目录。
- 决策: 生成器选 A（Docusaurus）；交付形态选 **b（go:embed 自带 `/docs`）为主，独立站点部署为可选输出**（同一份 build 产物，用户要对外挂独立域名时自行托管，BinFlow 不承诺维护双轨）；源文件选 a（docs/user/ 保持 Markdown 源，站点配置在 `docs-site/` 聚合构建）。
  - **交付形态细则**：`docs-site/` build 产出静态资产 → 构建步骤复制进 `internal/docs/`（与 console 的 web/dist → internal/console 同构）→ `go:embed` 打入 binflow-server → httpapi 挂 `/binflow/docs/**`（统一前缀内，不占根级；控制台「帮助」入口链到它）。二进制体积预算影响：文档站 build 产物典型 5~15MB（gzip 后 embed 更小），相对 40MB 上限可控；若 M5 实测超预算，fallback 是 docs 站产物走独立 tar 附带而非 embed——以 M5 check-size 实测为准，不预先复杂化。
  - **工作流细则**：tech-writer 继续按文件地图只写 `docs/user/*.md`（frontmatter 仅用 Docusaurus 兼容子集：title/description/sidebar_position），不碰 `docs-site/`（docusaurus.config.js、sidebars、i18n 配置归 architect/release-engineer）；`docs-site/` 以 docs/user 为内容源聚合。CI（或 Makefile 目标）负责 build + 复制进 embed 目录，writer 的 DoD 不含「构建站点」。
- 理由: 与 M4 控制台同栈 React（组件/构建链/技能复用，i18n 方案可共享）；Artifactory 官方文档站即同类形态（迁移用户心智连续）；**版本化文档（随 BinFlow 版本切 docs/v1.x）与 i18n（中英双语）是产品级需求**，Hugo/Jekyll 要自建、纯 Markdown 没有——Docusaurus 内建。交付形态选 embed：PRODUCT 成功标准「部署矩阵每种方式 15 分钟跑通」——离线安装包/air-gapped 场景下文档随二进制走是差异化（出问题当场可查），独立站点在受限网络恰恰不可达；「单二进制含文档」与「单二进制含控制台」是同一哲学。
- 后果: `docs-site/` 目录纳管（属 architect/release-engineer，非 tech-writer area）；M5 部署矩阵新增一项「docs 站点产物」（embed 形态下即二进制本身，独立托管为可选自办）；Makefile 增 `docs` 目标（build + embed 复制）；**M4/M5 票面影响**：T-46（docker 接入文档）等已产出的 docs/user Markdown 即 Docusaurus 首批页面（frontmatter 兼容子集需回查一遍）；M5 文档矩阵票前必须先有 **Docusaurus 脚手架票**（类似 T-7 工程脚手架先行：node 依赖、build 链、embed 复制、/binflow/docs 路由、check-size 实测）；`internal/docs` 包（embed + Handler）进入包结构；40MB 二进制预算在 M5 check-size 对 docs 增量实测，超限走 fallback（独立 tar）。
- 增补（2026-08-21，T-130 依 M5 PRD §5.5 K1 终裁；交付形态〔embed 主交付 + 独立 tar 可选〕与工作流细则不变，构建形态四项定案）:
  ① **搜索索引 = 外挂本地全文索引 `@easyops-cn/docusaurus-search-local`（zh 分词另装 `nodejieba`）**，T-129 暂行「Docusaurus 内建方案」就此修正——事实核查：Docusaurus 内建搜索只索引 title/description/keywords/tags **元数据**（非正文全文），G18 要求的「配额」「离线」类正文关键词在内建方案下结构性不可达；官方搜索文档给出的本地全文方案即此类插件（v0.55.x 支持 Docusaurus v3、zh 分词专项优化、Snyk 零已知漏洞、活跃维护）。Algolia/Typesense 等外网搜索服务被 NFR-S29 零外网断言排除。**索引形态 = build 期生成静态索引文件随 embed 走、浏览器本地检索**（零请求外发、离线可搜）。构建链注记：nodejieba 是原生 C++ addon，仅存在于 docs-site node 工具链（构建期）——不进 Go 二进制、不进运行时镜像（ADR-0005 构建链/运行时隔离，NFR-S31；docker 构建的 node 阶段如需编译工具链属正常成本）。对 T-129 的影响收敛在配置层：docusaurus.config.js 增 plugin/theme 块、package.json 增 devDependencies——embed/挂载/`make docs` 链路零改动。
  ② **self-host 资产落盘布局 = build 产物原样进 `internal/docs/dist`，零后处理**：Docusaurus 默认全量打包 JS/CSS/字体（Infima 自带、无外链），baseUrl `/binflow/docs/` 原生完成资产路径前缀化——console 的 relink-assets 手法在此**不需要**（T-126 预计零改成立，本增补定案）。红线：不引自定义字体/统计/评论/反馈等任何外链组件（PRD Non-goal 钉死）；G18 的 route-abort 断言是该红线的常设验证面。
  ③ **版本化 = `v1.x` 目录起步（Q4），单版本期默认路径无版本段**：`lastVersion: 'v1.x'`——baseUrl 根即当前版本（URL 稳定，G22 版本下拉含 v1.x）；`defaultLocale: 'zh'`（zh 唯一，i18n 骨架就位、en 目录不建——PRD Non-goal）。v2 出现时的默认版本切换策略（显式版本段 vs 根跳最新）届时裁决，不在本增补承诺。
  ④ **fallback 触发线（docs embed 增量 gzip > 15MB）处置步骤定案**：(a) `make docs-size` 报告归因——图片/未压缩资产优先内容减重（tech-writer 内容票路径，不算 fallback）；(b) 减重后仍 >15MB → 触发 fallback：`internal/docs/dist` 置占位、二进制重建不含 docs 资产，`docs-static_<VER>.tar.gz` 升格为 docs 唯一交付渠道并附 Release；(c) 触发与解除均**须经用户确认**（Q1 联动，不静默降级），QA 报告记录形态（该形态下 G04/G19 的 docs 200 断言按占位形态改写）；(d) 六平台压缩产物 >40MB 而 docs ≤15MB → **不触发本 fallback**（归因 console/二进制本体，另案处置）。
  ⑤ **docs-static tar 内容 = 同一 build 产物原样**：tar 根即站点根（无版本/外层目录嵌套），与 embed 消费同一 `internal/docs/dist`（不二次构建）；Release 附 sha256；自托管说明（任意静态服务器 alias 到站点根 + nginx 一页示例）归 T-141 文档票。P1 可选输出定位维持（FR-41-AC7）。

## ADR-0012: remote 仓库代理基线——pull-through 缓存、SSRF 防护、凭据加密、stdlib-only HTTP
- 状态: Accepted
- 日期: 2026-08-19
- 背景: M3 最大架构增量是 remote 仓库（pull-through 代理缓存）。三个必须先定的面：缓存/失效/降级语义；SSRF 防护（上游 URL 是管理员配置的，但 BinFlow 部署在内网——被攻陷的 admin 配置恶意 URL 可打元数据服务/内网，安全底线要求设计期就防）；上游凭据静态加密（ADR-0003 遗留：remote_configs.password M1 起明文占位，技术债 #5 承诺 M3 前定案）。
- 候选方案:
  - 缓存失效: A) 纯 TTL（miss 才回源，命中期内永不再验）；B) TTL + 条件再验证（ETag/Last-Modified，304 刷新时钟零 body）；C) 永久缓存 + 手动失效。
  - 上游故障: A) 硬失败（上游 5xx/超时 → 客户端直接 502）；B) stale-while-error（缓存过期但上游不可达时回吐过期副本 + Warning 头）；C) 黑名单自动熔断。
  - 凭据: A) 明文列（现状占位）；B) AES-256-GCM + env 主键；C) 外部 secret manager（vault）。
  - HTTP client: A) net/http + 自写 transport 装饰（超时/重试/限速）；B) hashicorp/go-retryablehttp；C) resty 等框架。
- 决策:
  1. **缓存语义 = B（TTL + 条件再验证）**：miss → 回源 fetch → 流式落盘 local blob + node（与本地上传同一落盘协议，checksum 一致性同源）→ 响应；命中且未过期 → 直接回；命中已过期 → 条件请求（If-None-Match/If-Modified-Since，验证器元数据存 003 新表 remote_cache），304 刷新时钟、200 全量替换（同 sha256 幂等覆盖）。**metadata 类响应（maven-metadata.xml、npm packument、simple index）与 artifact 分流**：前者默认短 TTL 且可按 Content-Type 重新生成，后者默认长 TTL（制品不可变原则，checksum 命中即永不再验）。
  2. **降级 = B（stale-while-error）+ 手动 blacked_out 开关**（复用 remote_configs.unreachable_mask 列语义改名）：上游 5xx/超时/连接拒绝且本地有过期副本 → 回吐副本 + `Warning: 111 binflow "revalidation failed, stale content"`；无副本 → 502 spec 信封。不自动熔断（M3 复杂度不成比例；blacked_out 手动遮蔽走已有列）。
  3. **SSRF 防护（配置时校验 + 请求时双检）**：建/改 remote 仓时解析 URL——scheme 仅 https/http；host 解析出的全部 IP（含 CNAME 展开）不得落入回环/私网（RFC1918）/链路本地（169.254/64:ff9b::/fc00::/7）/唯一本地（fd00::/8）/组播/0.0.0.0，违者 400（可配 `allow_private_upstream: true` 显式豁免，审计记事件）；**请求时重验**（IP 可能随 DNS 漂移，自定义 net.Dialer 的 Control 钩子在连接前对实际 IP 复查同一清单——防 DNS rebinding）；**IPv6 过渡格式拆解递归过表（T-65 review 勘误，commit 0dc17a2——修正原文仅列 NAT64 前缀、未写内嵌 v4 拆解语义的缺口）**：64:ff9b::/96（NAT64）与 2002::/16（6to4）取内嵌 IPv4 过全清单（包裹公网 v4 放行，保 DNS64），2001:0::/32（Teredo）直接拒，::a.b.c.d（IPv4-compatible）同构收编，zone 标识剥离后判定；重定向不自动跟随（registry/maven 上游重定向罕见，手动跟随且每跳重过 SSRF 校验，最多 3 跳）；超时（连接 10s/响应头 30s 可配）与响应体上限（默认 8GB，防解压炸弹——Content-Length 预检 + 流式计数双保险）。
  4. **凭据 = B**：AES-256-GCM，随机 12B nonce，密文 `enc:v1:<base64(nonce+ciphertext)>` 前缀标识；密钥经 env `BINFLOW_REMOTE_CREDENTIALS_KEY`（base64 32B）注入，不入 YAML；有 remote_configs 行而无密钥 → 启动 fail-fast；无前缀的旧明文值在 003 迁移中一次性加密（需 env 密钥在场）。密钥轮换不做（M3 单密钥，轮换 = 重新录入凭据）。
  5. **HTTP client = A（stdlib-only）**：net/http + 自写 `internal/remote` 的 transport 装饰（超时/重试仅幂等 GET、指数退避 2 次）。不引 retryablehttp/resty——重试逻辑约 40 行，不值得为此破 ADR-0005 白名单（准入原则：默认 stdlib，引入须 architect 记录）。
- 理由: 代理缓存的正确性核心是「制品不可变 + metadata 可变」二分，B 案精确表达且条件再验证是上游友好标准做法；stale-while-error 换取内网 CI 在上游抖动时的可用性（这是 pull-through 代理的存在意义）；SSRF 双检（配置时 + 连接时）是因为单点校验挡不住 DNS rebinding——这是安全底线的设计期义务；凭据加密选对称 + env 密钥是「单二进制零外部依赖」约束下唯一务实解；stdlib client 符合依赖准入与供应链最小面。
- 后果: 新增 `internal/remote` 包（fetcher/cache-state/ssrf-guard/credentials，依赖 config+storage+metadata）；003 迁移：remote_configs 加列（content_ttl_seconds/metadata_ttl_seconds/allow_private_upstream/blocked_out 改名）、新表 remote_cache（验证器元数据）、旧明文凭据一次性加密；`X-BinFlow-Cache: HIT/MISS/STALE/REVALIDATED` 响应头供 QA 断言与用户排障；SSRF 清单与豁免逻辑属安全面，变更须 security 意识 review；上游响应的 checksum 校验：上游给 digest 头（Docker-Content-Digest/X-Checksum-*）则强校验，否则信任 TLS + 落盘时自算摘要记账。
- 勘误一（2026-08-19，T-79 依 PRD v1.1/v1.2 定案回写，决策方向不变、故障语义对齐）: **决策 2 的「无副本 → 502 spec 信封」作废**。PRD 定案（T-60，FR-20-AC5/M44，repo-semantics §7.6 高置信度）：上游 5xx/超时/连接失败 → 仓标记 **assumed-offline**（静默期 `assumedOfflinePeriodSecs` 默认 300s，期内零上游流量——取代本 ADR「不自动熔断」表述，静默期即轻量熔断）；有缓存（**含过期**）→ 服务缓存 + 附 `X-Binflow-Upstream-Error: <摘要>` 头（取代原 `Warning: 111` 方案，网关可观测性头 + 客户端等价感知）；无缓存 → **404**（E-01，message 含 offline/assumed offline 状态）；仅 `hardFail: true`（默认 false）时改 **502**。另补 PRD 定案的负缓存语义：上游 404 → 写负缓存（missedRetrievalCachePeriodSecs）+ 若本地有过期副本仍回发（"expired but serving"）。
- 勘误二（2026-08-19，T-79）：**决策 3/5 的参数面实现以 PRD v1.2 §6 NFR-S13 为准**，本 ADR 正文数字为设计草案：① 重定向上限 **5 跳**（本文 3 跳作废），每跳完整重过校验链、拒绝即 400；② 超时统一为仓配置 `socketTimeoutSecs` 默认 **15s**（本文连接 10s/响应头 30s 双值作废）；③ 体量上限分型：**非 streaming 缓冲型响应（packument/simple/metadata）64MB**、超限截断报 502（本文「8GB 全局响应体上限」表述作废，artifact 流式落盘不受 64MB 限）；④ 缓存 TTL 默认对齐 PRD C4：`retrievalCachePeriodSecs` **7200** / `missedRetrievalCachePeriodSecs` **1800**（本文 content_ttl 86400/metadata_ttl 600 的默认值列名与数值以 003 迁移实现票为准对齐 PRD 字段名）；⑤ **建仓时只做 scheme/格式校验**（PRD FR-15-AC3：私网 URL 建仓成功，IP 校验全部在**请求时**执行——DNS/网络可变，本文「配置时全 IP 解析拒绝违者 400」弱化为请求时双检的唯一防线；`allowPrivateUpstream` 豁免语义不变，仅 admin 可设 + 审计）。凭据加密（决策 4）经 PRD v1.2 Q1 正式采纳关闭，双方一致无需勘误。

## ADR-0013: virtual 仓库解析顺序与 M3 写路由边界
- 状态: Accepted
- 日期: 2026-08-19
- 背景: M3 落地 virtual 仓库（聚合 local + remote 成员）。两个必须定的行为：解析顺序（Artifactory 对齐——repo-semantics §7.2 的 `priorityResolution` 与成员序语义待逆向补齐，M3 先定 BinFlow 默认）；virtual 是否可写（Artifactory 语义：virtual 可指定 local deployment repo 承接写，M1 的 §5.1 已按 repo-semantics §1「virtual 未配 local deployment → 405」埋了钩子）。
- 候选方案:
  - 解析顺序: A) 纯成员列表序（配置顺序即解析顺序）；B) local-first + 列表序（先全部 local、再按列表序 remote）；C) 每成员可标 priority（Artifactory priorityResolution 形态）。
  - 写路由: A) M3 virtual 完全只读（405 + Allow: GET）；B) 支持指定 local deployment 成员承接写（Artifactory 完整语义）。
- 决策: 解析选 **B（local-first + 列表序）**为 M3 默认，C 的逐成员 priority 列**留缝不实现**（virtual_members 表 M1 已带 position 列，语义升级为「同类内的次序」，跨类排序 local<remote 固定；Artifactory priorityResolution 精确语义待 repo-semantics §7.2 M3 逆向产出后校准，冲突时新 ADR）；**去重语义：命中即返回**（首个命中成员的副本，不做多成员归并——同一 artifact 不同成员副本不一致时以先命中者为准，`X-BinFlow-Resolved-From: <repoKey>` 响应头暴露来源）。写路由选 **A（M3 只读）**：PUT/DELETE 到 virtual → 405 + `Allow: GET, HEAD`（repo-semantics §1 已钉，M1 的 generic 实现已如此）；local deployment 承接写推迟到需求出现（PM 联动：M3 PRD 若立 FR 再议，届时不改本 ADR 的解析顺序面）。
- 理由: local-first 是「本地永远比代理新鲜可信」的保守默认，与 Artifactory 实践主流一致；命中即返回避免 M3 就背上「多源归并/择优」的复杂度（那需要 per-layout 的版本比较器，M4+ 的需求形态才清楚）；virtual 只读把 M3 范围收敛在「读聚合」，写路径继续单仓语义，权限模型不用动。
- 后果: virtual 解析在 `repo.Service.Get/List` 内实现（成员序读取 → 逐成员尝试 → 首命中返回），remote 成员的未命中**不产生缓存副作用**（miss 不落盘，只有直接 GET remote 仓才 pull-through 落盘——virtual miss 透传探索，防成员扫描污染缓存）；列表（List）聚合语义 = 各成员 path 前缀并集（目录树合并，不合并文件实体）；`virtual_members.position` 语义文档化为「同类内次序」；QA 需覆盖 local/remote 交叉命中矩阵 + Resolved-From 头断言。
- 联动记录（2026-08-19，T-79 依 PRD v1.1/v1.2 定案回写，触发本 ADR 自带的 PM 联动条款）：**两处语义按 PRD 升级，决策骨架（成员序、首命中即返、只读默认）不变。**
  ① **解析顺序：local-first 升级为两桶序（PRD C3/FR-21-AC7）**——Artifactory 为四桶序（repo-semantics §8.1 高置信度），BinFlow 因无独立 `<key>-cache` 影子仓投影（ADR-0012）简化为**两桶**：优先桶（`priorityResolution: true` 的成员，桶内声明序）在前，其余成员（桶内声明序）在后——local 不再绝对优先，被 priorityResolution 标记可越位（对齐 Artifactory per-repo 字段语义）。本文「跨类排序 local<remote 固定」表述作废，`virtual_members.position` 语义修订为「桶内声明序」。**stale/下一成员优先关系（PRD 定案）**：成员 remote 命中 stale 缓存（含过期副本，含 assumed-offline 期）即作为该成员解析结果返回、不跳下一成员；仅成员真正 404（负缓存/无副本/offline 无缓存）才继续桶序。
  ② **写路由：M3 只读升级为可选写路由（PRD FR-21-AC4，P1）**——virtual 配置 `defaultDeploymentRepo`（成员中的 local 仓 key；别名 `defaultDeploymentRepoRef`/`deploymentRepository` 亦接受）后可写：PUT/POST/DELETE 路由到该 local 成员执行（权限/覆盖检查/checksum 链按目标仓语义），virtual GET 立即可见；未配置 → 405 + `Allow: GET`（定案文案 `No local repository was configured as local deployment repository for the (<key>) virtual repository.`）；指向非 local 成员 → 400 建仓校验。npm publish 与 PyPI upload 同走路由。

## ADR-0014: console 包基线——挂载路由、server-side session + CSRF、SPA/REST 边界、前端构建链
- 状态: Accepted
- 日期: 2026-08-20
- 背景: M4 兑现 `internal/console` 占位（ADR-0002）。需定四件事：SPA 挂哪（与 ADR-0011 `/binflow/docs` 的关系、repo key 空间侵蚀）；浏览器认证形态（已有 Basic/API token/Bearer(docker)——浏览器面缺登录态）；前端调什么 API；前端构建链与 ADR-0005 依赖白名单的关系（node devDependencies 是否受管）。
- 候选方案:
  - 挂载: A) SPA 占 `/binflow/` 根（M1 占位处）——SPA 前端路由与 repo key/保留段空间互相侵蚀，深链与刷新的 fallback 规则复杂；B) SPA 挂 `/binflow/console/**` 保留段（与 `/binflow/docs` 同模式），`/binflow/` 根 302 → console。
  - 浏览器认证: A) server-side session（SQLite 表 + HttpOnly Cookie）；B) JWT（无状态，但登出/吊销需黑名单=又回到服务端状态，且 localStorage 存token 有 XSS 面）；C) 复用 API token 当 Cookie（token 永久性与浏览器会话语义错配，登出即吊销会杀死 CI token）。
  - 前端 API 面: A) console 专属 `/api/v1/console/**` 聚合面（OSS web/rest-ui 模式）；B) 前端直接消费既有/新增的通用 `/api/v1/**` 管理端点，不设 console 专属树。
  - 前端构建链: vite vs webpack；devDependencies 管辖。
- 决策:
  1. **挂载选 B**：SPA 挂 `/binflow/console/**`（保留段清单从 {api,v2} 扩为 {api,v2,docs,console}，repo key 建仓校验同步拒绝）；`GET /binflow/` → 302 → `/binflow/console/`。静态资产 `go:embed internal/console/dist`（web/ 源码构建产物复制进入，与 docs-site 同构，ADR-0011 模式复用）；SPA 前端路由 base=`/binflow/console/`，深链 fallback = 段内任意路径回 index.html（不越出 console 段，绝不吞内容路径）。
  2. **认证选 A（server-side session）**：004 新表 `web_sessions`（id 存 sha256 摘要、username、created/expires/last_used/revoked_at——存形态与 tokens 同规）；Cookie `bf_session`：HttpOnly + Secure(https) + SameSite=Lax + Path=/binflow；绝对 TTL 12h + 滑动续期（last_used 刷新，单次延长不超 12h）；登出 = revoke + Cookie 清除。`auth.Authenticator` 增第三臂（Basic/Bearer/Session），session 认出的 Principal 与 Basic 等权（无 TokenID、带 Groups）。**CSRF 三层**：① SameSite=Lax 挡跨站 POST（现代浏览器）；② console 前端所有变更请求带自定义头 `X-BinFlow-Console: 1`（跨站简单请求无法携带自定义头，会触发 CORS 预检而默认 CORS 不放行 → 攻击链断）；③ 变更端点对「Cookie 认证 + 无该头」的请求拒绝 403（服务端强制，不依赖浏览器行为）。Bearer/Basic 请求天然免疫 CSRF（非 Cookie 携带）。
  3. **API 面选 B（不设 console 专属树）**：前端消费通用 `/api/v1/**`（dogfooding——CLI/curl/前端同一 API 面，权限语义单源）；缺的管理端点按通用语义补（users/groups CRUD、system/gc、quota、audit 查询参数化），不造 `/api/v1/console/**`。OSS web/rest-ui 的「console 专属后端」模式不采纳——那是其 UI 版本与后端解耦的历史结构，BinFlow 单二进制同版本交付无此需求。
  4. **构建链 = vite + React + TypeScript**（React 与 ADR-0011 Docusaurus 同栈技能复用；vite 为当前生态默认，webpack 无增量收益）。**ADR-0005 边界澄清：其白名单管辖 Go module 依赖树（进二进制的面）；前端 devDependencies 属构建期工具链，不进二进制**——政策放宽但守三条：直接依赖最小化并 lockfile 锁定（pnpm/npm ci）；产物零运行时 CDN 依赖（全量打包，断网可用——对齐单二进制哲学）；CI 跑 `npm audit --production=false` 高危拦截 + 直接依赖清单变更走 architect 过目（与 Go 面同规的准入习惯）。Makefile 增 `console` 目标（build + 复制进 internal/console/dist）。
- 理由: 挂载 B 与 docs 同构、空间不侵蚀（A 的 SPA fallback 与内容路径 404 语义会打架）；session 方案在「可登出可吊销 + 无 XSS token 暴露面 + 与既有 sha256-token 存储形态同构」三点上全胜 JWT，代价仅一张表；CSRF 三层使 Cookie 面与 Bearer 面安全等价；API 面 B 让权限模型与文档只维护一份。
- 后果: 004 迁移含 web_sessions/groups/group_members/repo_usage（§6）；repo key 保留字 +2（docs/console——docs 伴随 ADR-0011 已事实占用，此处正式化）；auth 包加 session 臂与 `Principal.Groups`；httpapi 加 Cookie 解析、CSRF 头强制、`/binflow/console/**` 静态 + SPA fallback、`/binflow/` 302；前端工程进 `web/`（ux-designer 信息架构 + 前端实现票消费）；CI 需 node 工具链（构建期，部署产物不含）；审计记 console 登录/登出事件。
- 勘误（2026-08-20，T-108 依 tech-lead T-88 R1 裁决「PRD 命名面 + ADR 机制内核」；M4 PRD v1.0 后出且为 W 序列验收锚，命名以 PRD 为准，机制内核维持决策 2 不变）:
  ① **挂载**：`/binflow/console/**` → **`/binflow/ui/**`**，`/binflow/` 根 **301**（原 302）；保留字增 `ui`（ADR-0008 增补并集 {api,v2,docs,console,ui}）；静态资产指纹路径与缓存策略（`/binflow/assets/<hash>` immutable、SPA shell no-cache）按 PRD §3。
  ② **session 端点族**：login/logout 两端点 → **`/api/v1/session` 三动词**（POST 登录 / GET whoami——前端路由守卫必需，W 序列锚 / DELETE 登出）；cookie 名 `bf_session` → **`binflow_session`**（属性集 HttpOnly + Path=/binflow + SameSite=Lax 按 PRD CE-03；HTTPS 部署时加 Secure 属实现细节）。
  ③ **TTL**：固定 12h → **`console.session_ttl_hours` 默认 24（PRD Q1）+ `console.session_ttl_seconds` 覆盖键**（测试粒度，R4：两键并存 seconds 优先）；滑动续期受绝对 TTL 封顶的内核不变。（T-110 塌缩句：双键共用一键后，滑动续期被绝对封顶**吞没**——会话必死于 `created_at + TTL`，与活跃度无关；默认配置下活跃用户 24h 必掉线重登，前端/QA 不得按「滑动续期」字面理解为「活跃可续命」。）
  ④ **CSRF**：第三层「服务端强制 X-BinFlow-Console 头」**撤销**（与决策 3「前端消费通用 /api/v1」自冲突——强制头使 Cookie 认证面与 Basic/Token 面行为分叉，CLI 零感知被破坏）；主防线改为 **Origin 同源校验**（session cookie 认证的写请求携带非同源 `Origin` → 403，同源/无 Origin 放行，PRD FR-23-AC9/W38）；SameSite=Lax 保留为第一层；SPA 附 X-BinFlow-Console 头降级为前端自身第二层习惯（服务端不校验）。

## ADR-0015: M4 治理面基线——GC 在线触发、配额模型、备份/恢复
- 状态: Accepted
- 日期: 2026-08-20
- 背景: M4 治理四件（审计/GC/配额/备份）需要安全边界定案：GC 从 CLI-only 变为可在线触发（删数据面）；配额的 enforcement 点与计数口径；备份的一致性顺序（ADR-0006 已定 mtime 硬约束，此处定 export 顺序与 import 幂等）。
- 候选方案:
  - GC 触发: A) 仅 CLI（M1 现状，console 无法治理）；B) admin REST 在线触发（POST /api/v1/system/gc）+ 作业状态查询；C) 定时自动 GC。
  - 配额计数: A) PUT 时 SUM(nodes.size) 实时算（O(n) 每次写）；B) `repo_usage` 计数器行，与 node 增删同事务维护；C) 配额只统计不做（推迟）。
  - 备份导出: A) 停服冷备（最简单，违背可用性）；B) 在线 export：SQLite backup API（VACUUM INTO，在线一致性快照）+ blobs tar；顺序待定；C) 增量/快照卷依赖（LVM 等，部署形态耦合）。
  - import: A) 任意实例可导入（merge 语义，冲突规则复杂）；B) 仅空实例（fresh install），全量替换。
- 决策:
  1. **GC = B（admin REST 触发）+ 保留 CLI**：`POST /binflow/api/v1/system/gc`（body `{"dry_run":true}` 默认真；`apply` 必须 `{"dry_run":false,"confirm":true}` 双字段显式）→ 后台 goroutine 执行（storage.GC 既有契约，grace 不变），进程内互斥（并发触发 409 GC_IN_PROGRESS），`GET /binflow/api/v1/system/gc` 返回上次/当前作业状态（started/finished/removed 清单摘要）。**安全边界**：admin-only + 审计事件（gc.trigger 含 dry_run 标志）+ 默认 dry-run + grace 兜底不变（在线触发不缩短 grace——它不是紧急删除通道）。**不做 C（定时）**：M4 不引入调度器，周期执行留缝（配置项占位 [M5+]）。
  2. **配额 = B（计数器）+ config JSON 承载配额值**：配额值存 `repositories.config` 的 `quota_bytes`（0=不限，默认）；`repo_usage(repo_key, logical_bytes, updated_at)` 计数行与 node 增删**同一事务**维护（SQLite 单写者下无热行竞争放大）；enforcement 点在 `repo.Service.Put` 链——blob Commit 之前预检（`expect.Size` 已知时直判；流式未知 size 时写入后超限**回滚 node 但 blob 留待 GC**——不拒已落盘字节，只拒登记，超限响应码 PRD 定（建议 413 + QUOTA_EXCEEDED）。**口径 = 逻辑字节**（nodes.size 之和，非去重物理字节——配额按仓计量，跨仓共享 blob 的物理归属无法公平切分；物理占用另走 /api/v1/storage/stats 已有面）。remote 缓存 node 计入（可配 `quota_include_cache` 豁免 [M5+]）。
  3. **备份 = B（在线 export，顺序：先 DB 快照后 blobs tar）**：① `VACUUM INTO` 产出 SQLite 一致性快照（在线安全，WAL 兼容）；② tar `blobs/` 目录（**必须保 mtime**，ADR-0006 硬约束——tar 默认保留）；顺序裁定理由：DB 快照先定时，之后落盘的新 blob 只会成为 tar 里的**多余**未引用文件（import 后 GC 收），反之（tar 先、DB 后）DB 可能引用 tar 里不存在的新 blob → 恢复出悬空引用，不可接受。sessions/ 不进备份面（瞬态）。产物 = `<out>/binflow.db` + `<out>/blobs.tar` + manifest（版本、时间、blob 数、sha256 清单）。
  4. **import = B（仅空实例）**：目标实例必须零 repositories（校验后执行，非空 → 409/退出非 0）；流程 = 恢复 db → 解 tar 保 mtime → 启动时 GC dry-run 报告差异（预期只有多余 blob，无缺失）。**幂等** = 对同一备份重复 import 等价（db 覆盖 + tar 解压覆盖同 sha256 文件幂等）；跨版本 import 需迁移链可达（schema_migrations 版本号 ≤ 当前）。
  5. **入口形态**：export 走 CLI `binflow-server export --out <dir>`（读面，低危）+ admin REST 可选触发（异步作业，产物落 data_dir/exports/）；import 仅 CLI（写面高危，不做 REST——安全底线：危险操作走带外通道）。
- 理由: GC 在线化补齐 console 治理闭环，双字段显式确认 + 审计 + grace 不变把删数据风险压回 CLI 等价面；配额计数器避免每写 O(n)；先 DB 后 blobs 的顺序把「在线备份一致性」化为一条不可违反的简单规则；import 限定空实例免去 merge 语义的全部复杂度（备份恢复的正确心智本就是重建而非合并）。
- 后果: 004 迁移含 repo_usage；`repo.Service.Put` 链增配额预检（PutOpts 缝顺势承载豁免位——服务端计算的 metadata 是否计入配额随实现定，默认计入）；storage 无改动（GC 契约复用）；`binflow-server` 子命令 +2（export/import）；审计事件族扩展（gc.trigger/export/import）；M5 文档（tech-writer）必须写 mtime 保真与顺序两条 ops 硬约束。
- 勘误（2026-08-20，T-108，GC 触发面对齐 M4 PRD GE-03/FR-30，机制内核不变）：① 触发体由「dry_run 默认 + confirm 双字段」改为 **`{"apply": bool, "graceHours": int?}`**（apply=false 即 dry-run；graceHours 缺省用 `storage.gc_grace_hours`，grace 基准 = blob mtime 的 ADR-0006 内核不变）；② 执行模型由后台 goroutine + 状态查询改为 **M4 同步执行**（PRD Q6 暂行），`GET /api/v1/system/gc` 状态端点**不做**（P2 债务）；③ 互斥新增 **GC ↔ export 共用 data 目录级锁**（export 运行中 POST gc → 409 `export in progress`；反向 export CLI 退出码非 0——锁形态文件锁/进程内归实现票）；④ admin-only + 审计 + dry-run 默认姿态不变。配额面：专用 quota 端点不做，配置走 repositories 字段（quotaBytes）、用量观测走 `GET /api/v1/storage/usage/{repo}`（GE-06）——本 ADR 决策 2 的「config JSON 承载」内核不变。
- 勘误二（2026-08-20，T-112 依 T-96 架构 review N6/N7，备份面形态对齐 M4 PRD FR-32/GE-09 与 T-96 实现事实——溯源 T-96（commit 75c6d95）；机制内核不变，决策 3/4/5 原文存目）:
  ① **产物形态**：决策 3 的「`<out>/binflow.db` + `<out>/blobs.tar` + manifest」修订为**目录形态** `<out>/metadata.db` + `<out>/blobs/` + `<out>/manifest.json`（tar 系本 ADR 草案期措辞；`--tar` 单文件产物为 P2 债务——flag 已定义、显式报未实现，PRD FR-32-AC7）；「先 DB 后 blobs」顺序硬规则与 mtime 硬约束（ADR-0006 勘误②）不变。
  ② **入口形态**：决策 5 的「CLI `--out` + admin REST 可选触发（异步作业，产物落 data_dir/exports/）」修订为 **export/import 均仅 CLI**——`binflow-server export -c <cfg> --output <dir>` / `import -c <cfg> --input <dir>`（`--out` 措辞作废）；REST 面 M4 不做：`/api/export/**`、`/api/import/**` → 404 + E-01（PRD GE-09 有意不兼容，export 读面亦不豁免）。
  ③ **幂等释义与 import 语态**：决策 4 的「db 覆盖 + tar 解压覆盖」系 tar 形态时代措辞，修订为 = **清空目标 data dir 后**对同一备份重复导入等价（无半恢复：任何失败清回空）；「非空 → 409/退出非 0」收敛为 **CLI 退出码非 0**（import 无 REST 面）；「启动时 GC dry-run 报差异」降格为运维建议（文档面），不在 CLI 内强制——多余 blob 由常规 GC 收敛。
  ④ **配额键名与 remote 口径**（顺手收口 T-95 review NB4 + T-95 规格依据段登记的 §4.6 相抵项）：决策 2 的 `quota_bytes` 键名以上方勘误（T-108）行「repositories 字段（quotaBytes）」与 PRD §7 Q2 为准（camelCase）；「remote 缓存 node 计入（可配 `quota_include_cache` 豁免 [M5+]）」修订为 **pull-through 落盘不计量**（PRD Q2 暂行；005 回填一次性含既有 remote nodes，快照语义不回滚；M5+ 若需计量再按「计入开关」重开）——「config JSON 承载 + repo_usage 同事务计数」内核不变。architecture §3/§4.6/§7.6/§11.19 已同口径回写（T-112）。

## ADR-0016: 目录实体化不变量——putNode 材料化祖先 folder 行（隐式目录 404 定案）
- 状态: Accepted
- 日期: 2026-08-21
- 背景: T-100 发现的最大契约漂移：`repo.putNode` 只落文件行不落父目录行，`GET /api/storage/{repo}/{dir}` 对「制品路径推断出的隐式目录」404（storageNode 只认显式 folder 行，`?list` 同）。FE 以「搜索面前缀重构回退 + mkdir 逐段材料化祖先」两处兜底消化，代价已登记：review NB① 非根 404 语义被回退吞掉（空搜索结果渲染「空目录」而非「路径不存在」）、搜索面返回全后代（R1 同族代价）、mkdir N 次 PUT 且窄授权下祖先段静默失败；REST 兼容面（ADR-0003 对标面）对隐式目录仍 404。T-119 裁决定案。
- 候选方案:
  - A) **service 写路径材料化**：putNode 写目标节点前将路径每个祖先目录段以 folder 行落库（mkdir 语义内聚到写路径）；需 007 迁移回填历史库；remote 引擎语义需裁定。
  - B) **httpapi 读路径前缀聚合**：storageNode 目录 miss 时按前缀列举合成 FolderInfo（storageNode 是 item-info/?list/?permissions 三面共用汇点，一处缝盖三处）；零写放大、零迁移。
  - C) 维持 FE 兜底 + 架构注记。
- 决策: **选 A**。要点五条：
  1. **缝 = putNode**（Put/PutFromBlob/PutLandedBlob/PutManifest/folder-deploy 五条落库路径的唯一汇点）：写目标行前，对目标路径的每个祖先段执行 folder 臂写——即现有 putNode folder 语义原样复用（新行 or 幂等重放刷 UpdatedAt，与显式 mkdir/E-15 完全同一路径）。file 与 folder 目标均材料化（空仓 mkdir 链 `a/b/c/` 的 `a/`、`a/b/` 由服务端建，FE 逐段循环可删）。
  2. **顺序 = 祖先先于目标行**（与 blob-first「依赖行先落」同构）：崩溃窗口至多残留空 folder 行——良性、可删、pruneEmptyParents 可收——绝不出现「文件行存在而父目录行缺失」。
  3. **祖先是派生状态，不单独过门**：不判权、不过 governance pattern 门、不记审计事件（合法性继承自已通过全部门的目标写；folder 行 size 0 无内容，无提权面；quota/usage 计数 delta 0）。
  4. **007 迁移一次性回填**（双方言，INSERT OR IGNORE 幂等）：先落哨兵 blob 行（nodes.sha256 FK 前置），再递归 CTE 从既有 node path 推祖先集落 folder 行。
  5. **remote 引擎不跟随、不加读 fallback**：engine 直写 Nodes().Put 维持现状（M4 无 remote 目录浏览面，不可观察——§11.20 登记）；B 案的读路径合成**否决为常设机制**——双机制并存会掩盖不变量破坏（读 fallback 让「忘了材料化」的缺陷测试静默通过）。
- 理由: **模型不变量优于读路径补丁**——BinFlow 已全面承诺 folder 行为目录模型（哨兵 blob、mkdir 建行、prune 删行、Delete folder 分支、§3.4 ACL folder 契约），隐式目录洞是写路径未维持不变量而非模型二义；A 一次修复全部消费方（httpapi 三面、?list、?permissions、docker image 目录、export/备份、未来 REST/CLI），B 则要求每个目录解析点永久维护第二套投影语义（合成节点无 created/createdBy，FolderInfo 时间戳降级为零值）。**clean-room 取证支持 A 的语义**：storage-layout §3（高）参考实现 nodes DDL 有 node_type=0=folder——目录是实体行；repo-semantics §4（高）「删除后自动 prune 空父目录」只有当上传曾材料化父目录行才成立——BinFlow 的 pruneEmptyParents 在纯隐式树上今天是事实死代码，恰是不变量缺失的症状。**B 的性能优势经量化不成立**：显式 folder 的 FolderInfo 今天就已付全子树 List（writeFolderInfo → List → childInfos 首段投影），B 只是省写，且给每个真实 404（typo/深链探测）加一次前缀扫；A 稳态每写 = 深度次 PK 读 + 仅新目录 insert，对比 blob 落盘与摘要计算可忽略。**C 已被证实有缺陷**（NB①/R1 族/静默失败三登记），且不服务 REST 兼容面。
- 后果: 行为变更——`GET /api/storage/{repo}/{dir}` 与 `?list` 对任何有后代的目录 200（向参考实现行为靠拢，rest-api.md §3 的 FolderInfo 语义补全）；pruneEmptyParents 生效（删文件后空祖先链自动清除，repo-semantics §4 高置信行为）；docker 仓 image 目录成为可浏览实体（实现票须跑全套件确认无断言依赖现状）；FE 删 searchListing + mkdir 祖先循环两兜底（NB① 随之结构性关闭，非根 404 态恢复可达）；007 迁移进双方言链；export 快照自然含 folder 行与哨兵 blob（引用集 = nodes ∪ docker_refs 已覆盖）。回写 architecture §3.3（Put 契约注记）/§6（nodes.path 注释）/§11.20（remote 边界债）。

## ADR-0017: GA 镜像与 Chart 供应链——ghcr.io 命名空间、tag 策略、基镜像锚定、SBOM/签名可选性、Chart 仓库形态
- 状态: Accepted
- 日期: 2026-08-21
- 背景: M5 PRD §5.5 K2 待校准项到期。Q1 已由用户定案渠道家族（GitHub Releases + ghcr.io；Chart 仓库首选 GitHub Pages 形态、终形随 K2），本 ADR 收口剩余细节：ghcr.io 命名空间与镜像名、tag 家族、基镜像锚定粒度、GA 是否携带 SBOM 与签名、Chart 仓库终形。T-132（镜像双变体）在途按暂行先行，本 ADR 为勘误回写、不阻塞其实现（改动面收敛在 deploy/release 脚本常量——T-126 R5 预判成立）。
- 候选方案:
  - 命名空间/镜像名: A) 单镜像名 `ghcr.io/lzwzzy/binflow` + 变体 tag 后缀（`<VER>-alpine` / `<VER>-distroless` / 裸 `<VER>`）；B) 双镜像名（`binflow` 与 `binflow-alpine` 两仓库）。
  - tag 家族: A) 版本 tag + `latest`；B) 版本 tag + `latest` + `dev`/`edge` 滚动 tag；C) 仅版本 tag（无浮动）。
  - 基镜像锚定: A) 发行系 tag（`static-debian13:nonroot` / `alpine:3.24`）+ 发布清单记录当次解析 digest；B) Dockerfile 内 digest 钉死（最强逐字节可复现）。
  - SBOM/签名: A) GA 零携带（全 M6+）；B) syft SBOM 最小面（Release 附件）+ 签名 M6+；C) 全套（SBOM + cosign 签名 + SLSA provenance）。
  - Chart 仓库: A) GitHub Pages 经典 helm repo（index.yaml + tgz）；B) OCI artifacts（`helm push` 到 ghcr.io）。
- 决策:
  1. **命名空间 = `ghcr.io/lzwzzy/binflow`，单镜像名 + 变体 tag（选 A）**：ghcr 个人命名空间与 GitHub 账号同源，`lzwzzy` 与 module path `github.com/lzwzzy/binflow`（ADR-0008）一致——品牌与坐标单源；变体用 tag 后缀表达（业界 `-alpine` 惯例族），双镜像名徒增仓库、拉取权限、文档三处维护面。
  2. **tag 家族 = A（GA 不发滚动 tag）**：`v1.0.0`（= `<VER>`，Q4）→ **distroless**（PRD PB-03 暂行转正——默认推荐最小面）；`v1.0.0-alpine` 与 `v1.0.0-distroless` 显式变体皆发（裸 `<VER>` 与 `-distroless` 指向同一 manifest——「隐式推荐默认」与「显式变体」各取所需，成本是每版本 3 个 tag，可接受）；`latest` → 最新 `<VER>` 的 distroless。**dev/edge 滚动 tag 不进 GA 面**：滚动自动推送与 Q1「发布物清单逐项用户确认 + 凭证用户注入、不入 CI」结构性冲突（常驻推送无法逐项确认，且要求 CI 常驻凭证面）；M6+ 发布链 CI 化再评估。无 latest（C）被否：评估用户惯例上 `latest` 是发现路径，且它恒等最新版本 tag，无额外维护语义。
  3. **基镜像锚定 = 发行系 tag + digest 记录（选 A）**：distroless 锚定 **`gcr.io/distroless/static-debian13:nonroot`**——当前默认系是 debian13，debian12 已被取代（持续在冻结系上重建的 patch 发布会累积 CVE、撞 G24 trivy 门）；alpine 锚定 `alpine:3.24`（minor 锚定、patch 随 tag 浮动吸收，禁 `latest`）。锚定粒度取发行系 tag 而非 digest 钉死：每次发布重拉最新 patch（CVE 新鲜度优先），**发布清单记录当次解析 digest** 作可复现性记录项（NFR-S27）；digest 钉死（B）在手工发布模型下只带来重建摩擦与遗忘风险，可复现诉求由 digest 记录 + checksums 承担。
  4. **SBOM = GA 携带 syft 最小面（选 B）；签名（cosign / SLSA provenance / helm provenance）= M6+**：每变体 `syft <image>` 生成一份 SPDX JSON，作 **GitHub Release 附件**（与 checksums、离线包同家族）；不接 ghcr attestation、不进推送管道——保持「本地归档 → 清单 → 用户确认 → 推送」的 Q1 流程不变。SBOM 理由：单遍生成、零运行时依赖、零新凭证面；安全评审场景（PRD 场景 D）的材质证明从 checksums 到 SBOM 是自然延伸；syft 属构建侧工具，按 ADR-0005 准入记录、不入 go.mod（NFR-S31）。**签名后置的理由是结构性的**：① keyless cosign（OIDC）依赖 GitHub Actions runner 身份——与 Q1「凭证用户注入、推送非 CI 驱动」直接冲突，手工推送模型下 keyless 不可用；② 自持密钥签名引入长期私钥的生成/保管/轮换义务，与 M5 零密钥姿态（gitleaks 全仓零命中、NFR-S31）相抵；③ 无已发布公钥策略与用户验证指引的签名不构成安全承诺。M6+ 发布链 CI 化时 keyless cosign + SLSA provenance + helm chart provenance 一并评估（届时新 ADR）。
  5. **Chart 仓库 = GitHub Pages 经典 helm repo（选 A，Q1 首选转正）**：`https://lzwzzy.github.io/binflow/charts/`（index.yaml + `binflow-<VER>.tgz`；用户 `helm repo add binflow <url>`）。理由：① 消费零凭据、零工具版本门槛（helm 3.0+ 即用；OCI 形态要求 ≥3.8 且 `helm search` 的仓库发现面不覆盖 OCI）；② 渠道家族同质——Releases 与 Pages 均公共 Web 静态面，无注册表消费语义，受限网络的镜像/代理友好；③ 发布面吻合 Q1 手工模型（打包 tgz + 更新 index.yaml 提交，逐项可确认）；④ 离线包已含 chart tgz，无第三渠道诉求。OCI chart（`oci://ghcr.io/lzwzzy/...`）不排除 M6+ 随镜像渠道增补，GA 不双轨（与 ADR-0011「embed 主交付、独立托管不承诺双轨」同哲学）。
- 理由: 各条共同的主线：供应链每个面让「CVE 新鲜度与材质可审计」先行，把「逐字节可复现与签名信任链」记录化或后置——后者在 GA 手工发布模型下要么不可用（keyless 依赖 CI 身份）、要么是义务而非承诺（自持密钥）；渠道选择全部收敛到「公共 Web 静态面 + 手工可确认推送」，与 Q1 安全底线零张力。
- 后果: T-132 勘误一行（AC② 基底 `static-debian12:nonroot` → `static-debian13:nonroot`，deploy/release 脚本常量层）；PRD FR-35 基底表述与 PB-03/PB-05 置信度回写（T-130 顺带完成）；T-127 发布清单面增 SBOM 附件与基镜像 digest 记录项（T-147 G35 消费）；release 脚本常量定值（镜像 `ghcr.io/lzwzzy/binflow`、Chart 仓库 URL）；**推送动作安全底线不变**（R7：逐项用户确认、凭证用户注入不入库——本 ADR 只定「推什么、推到哪」，不改变「谁推、凭何确认」）；GitHub Pages 启用与 index.yaml 发布流程归 T-136/T-147 收口阶段（conductor + 用户执行）。

---

## ADR-0018: S3 存储后端——SDK 选型与 storage.Backend 扩展缝

- 状态: Accepted
- 日期: 2026-08-21
- 背景: ROADMAP M6+ 首项即为 S3 存储后端。当前 `storage.Engine` 是纯本地文件系统实现（blobs/ 二级分片目录 + sessions/ 会话目录）。S3 后端需同时满足：(a) ADR-0005 零 CGo 约束（六平台 goreleaser 交叉编译）；(b) 与既有 `storage.Engine` 接口的 blob 语义兼容（checksum 寻址、不可变、原子落盘在不同存储介质上需重新诠释）；(c) 依赖准入原则（ADR-0005 白名单扩展需 architect 记录）。
- 候选方案:
  - A) **AWS SDK Go v2** (`github.com/aws/aws-sdk-go-v2` + `service/s3`)：官方 SDK，最大 S3 兼容性（AWS S3 + 所有 S3-compatible 实现包括 MinIO、Ceph RGW、GCS S3 互操作模式），纯 Go（无 CGo），功能完整（multipart upload、pre-signed URL、transfer acceleration）。但模块数多（约 200+ 子模块），对 BinFlow 的 go.sum 体积有显著增量（当前 3 个直接依赖，引入后估增 50+ 模块）。
  - B) **minio-go** (`github.com/minio/minio-go/v7`)：MinIO 官方 Go SDK，纯 Go，高度优化，API 面比 aws-sdk-go-v2 小而聚焦（S3 就是它的全部），对 S3-compatible 实现的兼容性同样优秀。但它是面向 MinIO 服务器优化的，AWS 特有功能（如 STS/S3 Transfer Acceleration）支持不如 A 完整。模块数少（约 10 个直接依赖）。
  - C) **stdlib `net/http` + AWS SigV4 自签名**（手写 S3 REST 调用）：零外部依赖，但需要自实现 multipart upload、分块重试、预签名 URL、S3 错误解析、区域发现等。代码量估算 1500+ 行，且每个 S3 实现厂的兼容性差异（path-style vs virtual-hosted、签名版本、分块上传最小块大小）全部由自写代码承担——测试矩阵与维护负担不成比例。
  - D) 三方轻量库（如 `github.com/johannesboyne/gofakes3` 等）：主要用于测试/模拟，非生产级 S3 客户端，排除。
- 决策: **选 B — minio-go/v7**。理由：
  1. **纯 Go 零 CGo**：与 ADR-0005 基线一致，goreleaser 六平台交叉编译无 C 工具链需求。
  2. **API 面聚焦**：BinFlow 对 S3 的使用是「blob 存储 CRUD + multipart upload 替代 Session」（见 ADR-0019 的存储适配器设计），minio-go 的 `PutObject/GetObject/RemoveObject/ListObjects` + `NewMultipartUpload/CompleteMultipartUpload/AbortMultipartUpload` 精确覆盖，无多余模块。
  3. **S3-compatible 兼容性**：minio-go 对 AWS S3、MinIO、Ceph RGW、GCS（S3 互操作）、阿里云 OSS 等均有成熟验证——这是 BinFlow 用户的实际部署场景。
  4. **依赖面可控**：minio-go/v7 约 10 个直接依赖，对比 aws-sdk-go-v2 的 200+ 模块，供应链攻击面小一个数量级。对 `go.sum` 与二进制体积的增量在可接受范围（预估二进制增量 < 3MB，当前 21MB 远低于 40MB 预算）。
  5. **AWS 特有功能非必需**：BinFlow 的 S3 后端目标是「blob CRUD」，不涉及 STS、Transfer Acceleration、S3 Select 等高级功能——minio-go 的功能集恰好匹配。
- 理由: 在「S3 兼容性足够 + 纯 Go + 最小依赖面」三角上 minio-go 最优。aws-sdk-go-v2 的兼容性优势（STS 等）对 BinFlow 是过剩功能，其模块爆炸带来的 go.sum 膨胀与 `go mod download` 耗时（受限网络/air-gapped 构建场景）是负收益。自写方案（C）的维护成本不成立——S3 协议的表面积和兼容性差异远大于「约 1500 行」的线性估算。
- 后果:
  - `go.mod` 新增 `github.com/minio/minio-go/v7`（需 `go get` 验证可达性；受限网络需配置 `GOPROXY` 镜像，与 ADR-0005 后果一致）。
  - `internal/storage` 新增 `Backend` 接口（见 ADR-0019 存储扩展缝设计），`Engine` 拆为 `DiskEngine`（现有实现）与 `S3Engine`（新实现），`main` 装配时按配置选择。
  - 会话模型变更：S3 无本地文件系统语义，`Session` 接口的 `Append` 语义在 S3 上通过 multipart upload 实现（见 ADR-0019）。
  - 二进制体积增量：预估 2–3MB（需 M6 实现后 `make check-size` 实测确认）。
  - 依赖白名单追加：`github.com/minio/minio-go/v7` + 其传递依赖（`github.com/minio/crc64nvme`、`github.com/go-ini/ini` 等），全部需经 `go list -deps` 逐项确认无 CGo。
  - 配置面新增 `storage.backend` 字段（`disk` | `s3`），s3 形态下新增 `storage.s3` 配置段（endpoint、bucket、region、access_key_id、secret_access_key、use_ssl、path_style）。

---

## ADR-0019: 存储引擎扩展缝——Backend 接口与 Engine 重构

- 状态: Accepted
- 日期: 2026-08-21
- 背景: ADR-0018 确定 S3 SDK 为 minio-go。接下来的问题是：如何将 S3 适配进现有的 `storage.Engine` 接口？当前 Engine 接口（§3.1）隐含了本地文件系统语义（`BlobPath`、`AcquireDataLock`、`sessions/` 目录），且 `GC` 的 mark-sweep 依赖磁盘扫描。S3 是对象存储，无目录概念、无 rename 原子性、上传模型是 multipart upload 而非 append-to-file。直接让 Engine 同时承载 disk 和 S3 会导致接口被最低公共分母拉低。
- 候选方案:
  - A) **Engine 接口不变，S3Engine 实现全部方法**：`GC` 在 S3 上走 ListObjects + 引用集比对（mark-sweep 语义等价但实现不同），`Session` 在 S3 上走 multipart upload。缺点是 Engine 接口的某些方法在 S3 上是语义降级（`Open` 返回 `io.ReadSeekCloser` 在 S3 上无法真实 Seek，只能全量下载到内存/临时文件，非 Range GET 语义）。
  - B) **拆 Backend 接口，Engine 退化为门面**：`Backend` 是纯 blob CRUD 接口（无会话、无 GC、无锁），`DiskEngine` 和 `S3Engine` 都实现 `Backend`；`Engine` 接口保留但变为门面，组合 `Backend` + 会话管理（仅 disk 有会话，S3 的 multipart upload 在 adapter 层承载）。`GC` 移出 Engine 接口，变为独立函数（S3 上 GC = ListObjects vs 引用集）。
  - C) **完全拆开，disk 和 s3 各走各的接口**：`storage.DiskEngine` 和 `storage.S3Engine` 无共享接口，`main` 中按配置选择不同的 repo.Service 注入路径。这是最灵活但最破坏现有消费者（repo.Service、export/import、GC CLI）的方案。
- 决策: **选 B — Backend 拆分 + Engine 门面**。具体设计：
  1. **`Backend` 接口**（`internal/storage/backend.go`）：纯 blob CRUD——`Put(ctx, sha256, reader, size) (BlobRef, error)`、`Get(ctx, sha256) (io.ReadCloser, BlobRef, error)`、`Delete(ctx, sha256) error`、`Exists(ctx, sha256) (bool, error)`、`List(ctx) ([]string, error)`。无会话、无 GC、无锁。
  2. **`Engine` 接口保持不变**（现有消费者 `repo.Service`、`adapter/docker` 的 blob upload 直持、export/import 不变）。`DiskEngine` 的实现 = 现有 `diskEngine`，直接实现 `Engine`（不经过 Backend，避免中间层开销）。
  3. **`S3Engine` 实现 `Engine` 接口**：内部持有一个 `Backend`（即 S3 client），`BeginSession` 在 S3 上创建 multipart upload（upload ID 即 session ID），`Append` 映射为 `UploadPart`，`Commit` 映射为 `CompleteMultipartUpload`。`Open` 在 S3 上返回 `io.ReadCloser`（`GetObject` 的 body），**不实现 `ReadSeekCloser`**——契约降级为 `io.ReadCloser`（需要 Seek 的调用方 [docker blob GET with Range] 在 adapter 层通过 HTTP Range 头直接请求 S3 的部分对象，而非在 Go 层 Seek）。`GC` 在 S3 上走 `ListObjects` + 引用集比对。
  4. **`Engine.Open` 返回类型变更**：`io.ReadSeekCloser` → `io.ReadCloser`。这是对 §3.1 契约的向后兼容变更——当前版 `ReadSeekCloser` 只被 docker blob GET 的 Range 请求使用（adapter 层已用 `Seek` 跳到 offset），S3 形态下 docker blob GET 的 Range 请求在 adapter 层直接带 HTTP Range 头发 S3 GetObject，不经过 Go 侧的 Seek。disk 实现继续保持 `ReadSeekCloser`（返回的类型实现该接口，consumer 用类型断言，Go 惯用法）。
  5. **`Backend` 不在 M6 暴露为公开接口**：`Backend` 是 `internal/storage` 包内接口，不导出到 `internal/storage` 包外（`Engine` 仍然是唯一公开面）。这允许未来 `DiskEngine` 内部重构为 Backend 门面而不影响消费者。
- 理由: B 方案在「最小化消费者改动」与「不把 S3 语义硬塞进本地文件系统接口」之间取得平衡。`Engine` 接口保持稳定，repo.Service 与 adapter 的存储消费面不感知后端差异。`Backend` 的引入纯粹是内部实现细节，解决了 S3 的原子性（S3 PutObject 是原子的，无需 rename）和会话模型（multipart upload 替代 append-to-file）与本地文件系统的差异。A 方案让 `Engine` 接口承载过多语义降级（`ReadSeekCloser` 在 S3 上不可实现），C 方案破坏所有现有消费者。
- 后果:
  - `storage.Engine.Open` 签名从 `(io.ReadSeekCloser, BlobRef, error)` 变更为 `(io.ReadCloser, BlobRef, error)`——向后兼容变更（disk 实现返回的类型仍实现 `ReadSeekCloser`，现有 consumer 若类型断言 `io.ReadSeekCloser` 则继续工作）。
  - `storage.Backend` 接口是包内接口（`internal/storage` 内可见），不进入公开契约面。
  - `S3Engine` 的 `GC` 实现：`ListObjects` 获取 S3 上所有 key → 与引用集比对 → 删除未引用对象。grace 语义在 S3 上通过 object metadata 的 `LastModified`（mtime 等价）实现。
  - `storage.AcquireDataLock` 在 S3 形态下语义不变但锁目标是本地 `data_dir`（S3 形态下 data_dir 仍存在，用于锁文件、临时文件等）。
  - 配置面新增 `storage.backend`（`disk` 默认 | `s3`），s3 选型下 `storage.data_dir` 仍用于临时文件（multipart 下载时的临时缓存）和锁文件。
  - 现有 `blobs/` 布局与 `sessions/` 目录逻辑仅对 `disk` backend 生效；S3 backend 下 `blobs/` 是 S3 bucket 中的 key（`<prefix>/<sha256[0:2]>/<sha256>`，与本地布局同构，确保未来 backend 迁移时目录结构一致）。

---

## ADR-0020: OIDC/LDAP 认证扩展——认证流联邦与 session 对接

- 状态: Accepted
- 日期: 2026-08-21
- 背景: M6+ 第二项为 OIDC/LDAP。当前认证体系（ADR-0009/0014）仅支持本地用户（argon2id 密码 + API Token + web session）。引入 OIDC/LDAP 需要在不破坏现有三臂（Basic/Bearer/Session）的前提下扩展认证流，且必须与 ADR-0014 的 web session 体系无缝对接（OIDC 登录后仍发 `binflow_session` Cookie）。
- 候选方案:
  - A) **自建 OIDC/LDAP 客户端**：手写 OIDC Relying Party（RP）和 LDAP bind 逻辑，零外部依赖。但 OIDC 协议细节（PKCE、nonce 校验、state 参数、token endpoint 认证、userinfo 端点、JWKS 密钥轮换、ID Token 验证）表面积大，自建 ≥2000 行代码且安全边界（JWT 验证、签名算法白名单、audience 校验）容易出错。
  - B) **`coreos/go-oidc/v3`**（OIDC）+ **`go-ldap/ldap/v3`**（LDAP）：两个均为纯 Go 库，零 CGo，活跃维护。`go-oidc` 是 Dex 项目维护的 OIDC RP 库，静态度极高（Kubernetes、Grafana、Vault 等均依赖），内建 PKCE、nonce、ID Token 验证、JWKS 自动刷新。`go-ldap` 是 Go 生态 LDAP 事实标准，纯 Go 无 CGo。
  - C) **Dex 集成**（将 Dex 作为 BinFlow 的 sidecar 或内嵌）：Dex 是 OIDC Provider + LDAP connector，但部署复杂度大幅增加（多一个进程/容器），与「单二进制差异化」理念冲突。
  - D) **Ory Hydra/Kratos** 等全栈身份方案：过度工程化，面向大型身份平台而非制品仓库集成。
- 决策: **选 B — `coreos/go-oidc/v3` + `go-ldap/ldap/v3`**。具体设计：
  1. **认证扩展架构**：`auth.Authenticator` 接口**不变**。新增 `auth.IdentityProvider` 接口（`internal/auth/identity.go`），实现 OIDC 和 LDAP 两套 provider。`Authenticator` 的 `Authenticate` 方法扩展为：Basic 臂（本地密码 + token fallback）→ Bearer 臂（API token）→ Cookie 臂（web session）→ **新增：OIDC ID Token 臂**（`Authorization: Bearer <oidc_id_token>`，将 ID Token 验证后映射为 `*Principal`）。LDAP 不走独立的 HTTP 认证臂——LDAP 用户的认证发生在 login 端点（`POST /api/v1/session`），后端用 LDAP bind 验证用户名/密码。
  2. **OIDC 认证流**：
     - **配置**：`auth.oidc` 配置段（`issuer_url`、`client_id`、`client_secret`、`scopes`、`redirect_url`、`user_claim` 默认 `sub`、`group_claim` 默认 `groups`）。
     - **Web 登录流**：`GET /binflow/api/v1/session/oidc/auth` → 302 跳转 OIDC Provider → 回调 `GET /binflow/api/v1/session/oidc/callback` → 验证 ID Token + 签发 `binflow_session` Cookie（与本地用户登录同构）。首次 OIDC 登录用户自动创建本地 `users` 行（`is_admin=0`，`password_hash` 为空——OIDC 用户无本地密码，只能通过 OIDC 登录）。
     - **CLI/Docker 流**：OIDC ID Token 可以直接作为 Bearer token 使用（`Authorization: Bearer <oidc_id_token>`）。`Authenticator` 新增 OIDC 臂：验证 ID Token 签名（JWKS）→ 提取 claims → 映射为 `*Principal`（Name=`user_claim`，Groups=`group_claim`，Admin=管理员映射表）。
     - **管理员映射**：`auth.oidc.admin_group` 配置（OIDC group 名到 admin 的映射），或 `auth.oidc.admin_users` 列表（`sub` 值白名单）。首次登录创建的用户行 `is_admin` 按此映射设置；后续每次认证时根据 Provider 最新 claims 刷新 `is_admin`（OIDC 是权威源）。
  3. **LDAP 认证流**：
     - **配置**：`auth.ldap` 配置段（`url`、`bind_dn`、`bind_password`、`user_base_dn`、`user_filter`、`group_base_dn`、`group_filter`、`user_id_attribute` 默认 `uid`、`mail_attribute` 默认 `mail`）。
     - **仅 Web 登录流**：`POST /api/v1/session` 收到 `username/password` 后，先尝试本地用户（argon2id），失败后尝试 LDAP bind（用 `user_filter` 解析 DN → bind 验证密码 → 查询 groups）。LDAP 验证成功 → 自动创建本地 `users` 行（`is_admin` 按 `admin_group` 映射，`password_hash` 为空）→ 签发 `binflow_session` Cookie。
     - **不支持 CLI 直接使用 LDAP 密码**：`Authorization: Basic` 只验本地密码和 API token。LDAP 用户需先通过 Web 登录获取 session Cookie，或使用 API Token。
  4. **自动创建用户行**：OIDC 和 LDAP 认证成功但本地 `users` 表无对应行时，自动创建（`enabled=1`，`is_admin` 按映射规则，`password_hash` 为空）。空 `password_hash` 用户不能通过 Basic 臂登录（密码校验时发现空哈希 → 拒绝，不尝试 LDAP bind——LDAP bind 只在 login 端点触发）。
  5. **Token 签发**：OIDC/LDAP 用户登录后可通过 API `/api/v1/tokens` 签发 API Token（与本地用户同构），用于 CI/CD 场景。Verify 时 token 对应的 user 行 `password_hash` 可为空——token 验证不依赖密码哈希。
- 理由: `go-oidc` 是 OIDC RP 的事实标准，Kubernetes 生态广泛使用，安全性经过大量审计。`go-ldap` 同样是纯 Go LDAP 的事实标准。两者都满足 ADR-0005 零 CGo 约束。把认证逻辑封装在 `IdentityProvider` 接口后，`Authenticator` 只需要新增一个臂（OIDC Bearer），不改变现有三臂的语义。LDAP 只走 Web 登录流而非独立 Bearer 臂，简化了 LDAP 的集成复杂度（LDAP 没有标准化的 ID Token 概念，bind 是一次性验证，不合适做请求级 Bearer 认证）。
- 后果:
  - `go.mod` 新增两个依赖：`github.com/coreos/go-oidc/v3`、`github.com/go-ldap/ldap/v3`（均需 `go get` 验证可达性，纯 Go 零 CGo）。
  - 新增 `internal/auth/identity.go`：`IdentityProvider` 接口 + `OIDCProvider` + `LDAPProvider` 实现。
  - 新增 008 迁移 DDL：`ALTER TABLE users ADD COLUMN provider TEXT NOT NULL DEFAULT 'local'`（`local` | `oidc` | `ldap`）；`ALTER TABLE users ADD COLUMN provider_id TEXT NOT NULL DEFAULT ''`（OIDC `sub` 或 LDAP DN）；`CREATE UNIQUE INDEX idx_users_provider ON users(provider, provider_id) WHERE provider != 'local'`（Postgres 用 partial index，SQLite 用普通唯一索引因为不支持 WHERE 子句）。
  - 新增 008 迁移 DDL：`ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''`（M4 已有，此处确认被 OIDC/LDAP 消费）；LDAP `mail_attribute` 和 OIDC `email` claim 填充此列。
  - 新增 `/binflow/api/v1/session/oidc/auth` 和 `/binflow/api/v1/session/oidc/callback` 两个端点。
  - `POST /binflow/api/v1/session` 的认证逻辑从「本地密码 only」变为「先本地后 LDAP」。
  - `Authenticator.Authenticate` 新增 OIDC Bearer 臂（ID Token 验证 → Principal 映射）。
  - 配置面新增 `auth.oidc` 和 `auth.ldap` 两个顶层配置段。
  - 授权不感知 provider——`Principal` 的 `Name` 和 `Groups` 无论来自本地、OIDC 还是 LDAP，`Authorizer.Can` 统一处理。
  - 安全面：`redirect_url` 必须校验（防 OAuth 重定向劫持）；`state` 参数必须使用 PKCE + 随机 nonce；ID Token 的 `aud`/`iss`/`exp` 必须严格校验。

---

## ADR-0021: 复制/联邦——推拉模型与冲突处理

- 状态: Accepted
- 日期: 2026-08-21
- 背景: M6+ 第三项为复制/联邦。制品仓库的复制（replication）是 Artifactory 的核心企业功能，用于多站点分发、灾备、边缘节点。M6 的复制设计必须在「概念模型对齐 Artifactory（ADR-0003）」与「M6 复杂度可控」之间平衡。当前 BinFlow 是单实例单副本（ADR-0015 明确单副本约束），复制是多实例协同的第一步。
- 候选方案:
  - A) **推式复制（push replication）**：源实例在制品落地后主动推送到目标实例。优点：实时性高、适合边缘分发。缺点：需要目标实例 URL 和凭据、源实例需感知目标实例可用性、失败重试与积压管理复杂。
  - B) **拉式复制（pull replication）**：目标实例定时从源实例拉取。优点：实现简单（复用 remote 仓库的 pull-through 机制）、目标实例自带重试与离线容忍。缺点：延迟高、源实例无感知。
  - C) **事件驱动+拉式混合**：源实例发出事件（Webhook/消息队列），目标实例收到事件后拉取。优点：结合了实时性与拉式实现的简单性。缺点：需要事件总线（外部依赖），违背单二进制理念。
  - D) **全功能推拉双向联邦**：Artifactory Enterprise 的完整复制矩阵（推+拉+双向+事件同步+属性复制+逐项校验）。M6 范围过大，不可行。
- 决策: **选 A+B（推拉双模式），M6 先实现推式，拉式复用 remote 仓库机制**。具体设计：
  1. **复制配置模型**：`replication_configs` 新表（`replications` 表），每行 = 一个复制目标（`source_repo` → `target_url` + `target_repo` + `target_username` + `target_password` 加密存储）。配置通过 REST API 管理（`/api/v1/replications` CRUD）。
  2. **推式复制（push，M6 主实现）**：
     - **触发时机**：制品成功落地后（`repo.Service.Put` 链末），异步 goroutine 执行推式复制。不在 Put 的事务内阻塞客户端响应。
     - **传输协议**：HTTP（目标 BinFlow 实例的 REST API）。复用 `internal/remote` 包的 HTTP 客户端（stdlib-only，ADR-0012 决策 5）与凭据加密（AES-256-GCM，ADR-0012 决策 4）。
     - **推送内容**：blob 数据（checksum 校验） + node 元数据（path/size/mime/created_by/created_at）。目标实例走 `repo.Service.PutFromBlob`（blob 已上传则秒传）或完整上传。
     - **失败处理**：失败记录到 `replication_tasks` 表（`source_repo/target_repo/blob_sha256/status/attempts/last_error/created_at`），后台 goroutine 定时重试（指数退避，最多 5 次）。超过最大重试次数标记 `failed`，需手动重触发或清理。
     - **幂等**：目标实例校验 blob sha256 已存在则跳过数据上传，只建 node 行（秒传语义）。
     - **速率限制**：`replication_configs.max_bandwidth_bytes_per_sec`（可选，0=不限）。
  3. **拉式复制（pull，M6 复用 remote 机制）**：
     - 拉式复制 = 将 remote 仓库的 `url` 指向另一个 BinFlow 实例的对应仓库。现有的 `remote_cache` TTL 机制、条件再验证、staleness 降级全部复用。
     - 新增 `remote_configs.sync_interval_seconds` 字段（定期全量同步，默认 0=不自动同步，仅按需 pull-through）。
     - 拉式复制不新增表——它就是 remote 仓库的一个特例（上游是 BinFlow 而非公共注册表）。
  4. **冲突处理**：
     - **推式**：目标实例已存在同 path 的 node → 比较 `updated_at`（源实例的 `updated_at` vs 目标实例的 `updated_at`）。**M6 策略：先到先得（first-write-wins）**——目标实例的 `updated_at` 更新则跳过推送并记录 `skipped`。不做内容合并（Maven metadata 等）——M6 复制是「制品级」而非「元数据合并级」。
     - **拉式**：remote 仓库的缓存语义（checksum 命中永不再验）天然处理冲突——同一 sha256 的 blob 只存一份，不同 sha256 的同 path 制品按 TTL 过期回源。
     - **删除传播**：M6 不做。源实例删除制品不触发目标实例删除（删除是危险的跨实例传播）。M6+ 评估「删除同步」开关。
  5. **元数据同步**：
     - 推式复制按 `node.created_at` 倒序选择最近 N 个制品（默认 1000，可配 `replication_configs.max_items_per_push`），避免一次推送全量历史。
     - 增量复制：记录 `replication_configs.last_synced_at`，每次推送 `updated_at > last_synced_at` 的制品。
     - 全量复制：不接受（M6 scope，pull-through 已覆盖全量回源需求）。
  6. **审计**：每项复制事件记入 `audit_events`（`replication.push|replication.push.failed|replication.pull`），actor 为触发源（admin 或系统）。
- 理由: 推拉双模式对齐 Artifactory 的复制模型（ADR-0003），但 M6 的推式实现聚焦在「制品落地后异步推送」的最小可用版本。拉式复制通过复用 remote 仓库机制，几乎零增量实现成本。事件驱动（C）引入外部依赖，与单二进制理念冲突。全功能联邦（D）是 M6+ 两三个里程碑的增量，不在此 ADR 范围。
- 后果:
  - 新增 009 迁移 DDL：`replications` 表（`id/name/source_repo/target_url/target_repo/target_username/target_password_enc/max_bandwidth_bytes_per_sec/enabled/created_at/updated_at`）和 `replication_tasks` 表（`id/replication_id/blob_sha256/node_path/status/attempts/last_error/created_at/completed_at`）。
  - 新增 `internal/replication` 包（pusher/scheduler/status），依赖 `repo.Service`、`storage.Engine`、`remote.Fetch`（拉式复用）。
  - 新增 REST 端点：`POST/GET/DELETE /api/v1/replications`、`GET /api/v1/replications/{name}/tasks`、`POST /api/v1/replications/{name}/_trigger`（手动触发）。
  - `repo.Service.Put` 链末增加 `replication.Enqueue` 调用（异步，非阻塞）。
  - 推式复制的目标凭据加密复用 `internal/remote` 的 AES-256-GCM 方案（`BINFLOW_REMOTE_CREDENTIALS_KEY` env 改名或新增 `BINFLOW_REPLICATION_CREDENTIALS_KEY`，二者择一归实现票定）。
  - 安全面：复制目标的 URL 必须过 SSRF 校验（复用 `internal/remote` 的 SSRF guard，ADR-0012）；目标实例凭据必须加密存储。
  - 技术债：先到先得冲突策略对「同一制品在两个实例上被独立修改」的场景不处理（M6+ 引入「最后写入者胜」或「人工合并」）。

---

## ADR-0022: Prometheus 指标——暴露面、命名规范与零依赖实现

- 状态: Accepted
- 日期: 2026-08-21
- 背景: M6+ 第四项为 Prometheus 指标。当前 M1~M5 无指标暴露（仅结构化日志与 `/healthz`/`/readyz` 探针，ADR-0005 的 `/metrics` 端点已占位）。引入指标需在 ADR-0005 严格依赖准入（默认 stdlib）下满足 Prometheus 格式要求。
- 候选方案:
  - A) **`prometheus/client_golang`**：Prometheus 官方 Go 客户端库，功能最全（Counter/Gauge/Histogram/Summary、collector 注册、proc/process metrics、exemplar 支持），生态最成熟。但它是外部依赖，go.mod 新增约 10 个间接依赖，且部分功能（Histogram 的 quantile 估算）有锁竞争，在高并发场景需注意。
  - B) **stdlib `expvar` + 自写 Prometheus 文本格式序列化**：零外部依赖。`expvar` 提供 `Map`/`Int`/`Float` 等并发安全的基础类型。自写 Prometheus 文本格式（`# HELP`/`# TYPE`/metric line）约 100 行代码。不足：无 Histogram/Summary 支持（需自实现），无 process metrics（需自采集）。
  - C) **`VictoriaMetrics/metrics`**：纯 Go 替代方案，API 简洁，性能优于官方库（锁竞争更少），但生态不如官方库成熟，社区较小。
  - D) **OpenTelemetry Go SDK**：全功能可观测框架，但依赖树巨大（50+ 模块），不适合 BinFlow 的「最小依赖」原则。
- 决策: **选 B — stdlib `expvar` + 自写 Prometheus 文本格式序列化**。理由：
  1. **零新依赖**：`expvar` 是 stdlib，已经在 Go 标准库中。自写 Prometheus 文本格式序列化约 100 行代码，在 BinFlow 的规模下维护成本可忽略。
  2. **Histogram 需求 M6 可推迟**：M6 指标面以 Counter 和 Gauge 为主（请求数、错误数、并发数、存储容量、blob 数），延迟分布（Histogram）在 M6 阶段可用 `time.Since` + 自写分桶统计实现（约 200 行），或留在 M6+ 评估。
  3. **与 ADR-0005 一致**：依赖准入原则的核心是「默认 stdlib，引入外部库需 architect 记录」。M6 的指标需求在 stdlib 范围内可满足。如果 M6+ 出现 Histogram/Summary 的强需求（如 P99 延迟分布），再评估引入 `prometheus/client_golang`（届时新 ADR）。
  4. **Process metrics 自采集**：Go runtime 指标（`runtime.MemStats`、`runtime.NumGoroutine`）通过 `expvar` 暴露，约 50 行自采集代码。
- 具体设计：
  1. **暴露端点**：`GET /metrics`（根级，不带 `/binflow` 前缀——与 `/healthz`/`/readyz` 同族，ADR-0008 的探针/抓取基础端点）。`Content-Type: text/plain; version=0.0.4`。
  2. **指标命名规范**：`binflow_<subsystem>_<metric>_<unit>`（遵循 Prometheus 命名最佳实践）。例：
     - `binflow_http_requests_total{method,path,status}` — Counter
     - `binflow_http_request_duration_seconds` — Gauge（最近一次请求耗时，M6 简化版）
     - `binflow_storage_blobs_total` — Gauge
     - `binflow_storage_disk_used_bytes` — Gauge
     - `binflow_storage_sessions_active` — Gauge
     - `binflow_repo_operations_total{repo,type,action}` — Counter
     - `binflow_remote_cache_requests_total{repo,result}` — Counter（`result` = hit|miss|revalidated|stale|error）
     - `binflow_auth_attempts_total{result}` — Counter（`result` = success|failure）
     - `binflow_go_goroutines` — Gauge
     - `binflow_go_memstats_alloc_bytes` — Gauge
     - `binflow_go_gc_duration_seconds` — Gauge
  3. **实现**：`internal/metrics` 包（`registry.go` + `format.go` + `prometheus.go`）。`Registry` 是 `sync.Map` 的并发安全指标存储。`httpapi` 在 `mountProbes` 中挂 `/metrics` 端点，调用 `metrics.Format()` 生成 Prometheus 文本格式。
  4. **中间件集成**：HTTP 请求计数和延迟在 `httpapi` 的 middleware 链中记录（`accessLog` 中间件已记录请求信息，扩展为同时写指标）。
  5. **S3 存储指标**：当 `storage.backend=s3` 时，`binflow_storage_disk_used_bytes` 替换为 `binflow_storage_s3_objects_total` 和 `binflow_storage_s3_used_bytes`（通过 S3 ListObjects 或 bucket metrics 获取，取决于实现复杂度）。
- 理由: 指标是运维面，不是业务核心路径。用 stdlib 实现满足 M6 的「可观测性从无到有」需求，且保持零依赖基线。当 Histogram 延迟分布成为必需时，引入 `prometheus/client_golang` 的决策成本低（它已经是 Prometheus 指标的 Go 事实标准）。
- 后果:
  - 新增 `internal/metrics` 包（`registry.go` + `format.go` + `prometheus.go`，约 200 行代码，零外部依赖）。
  - `GET /metrics` 端点挂载（根级，与 `/healthz`/`/readyz` 同族）。
  - httpapi middleware 链扩展：`accessLog` 中间件同时写指标。
  - 二进制体积增量：零（stdlib 已有）。
  - 技术债：M6 的 Histogram 自实现分桶统计是简化版（可能不精确），M6+ 如需要 P99 精确延迟分布，需引入 `prometheus/client_golang` 或 `github.com/VictoriaMetrics/metrics`（届时新 ADR）。
  - 安全面：`/metrics` 端点默认暴露（与 `/healthz` 同族），不设认证。如需限制，由部署层（nginx/ingress）或配置 `server.metrics_auth` 控制（M6+ 选项）。

---

## ADR-0023: `bf` CLI 工具——独立二进制 vs 子命令

- 状态: Accepted
- 日期: 2026-08-21
- 背景: M6+ 第五项为 `bf` CLI。CLI 是面向用户的操作入口，应覆盖：仓库管理、制品上传/下载、token 管理、复制触发、系统状态查询。当前 BinFlow 的 CLI 面是 `binflow-server` 子命令（`serve`/`gc`/`export`/`import`），面向管理员运维。`bf` 是面向终端用户的客户端工具。
- 候选方案:
  - A) **独立二进制 `bf`**：独立 `cmd/bf/` 入口，`go build` 产出独立二进制。与 `binflow-server` 共享 `internal/` 包（http client、config 解析、auth token 管理）。优点：职责清晰（server vs client），用户心智简单（`bf push` / `bf pull`）。
  - B) **`binflow-server` 子命令**：`binflow-server client push` 等。优点：单二进制配送（下载一个二进制既能当 server 又能当 client）。缺点：server 二进制携带 client 依赖（如交互式终端库），binary 体积膨胀；且 server 配置语义（data_dir、listen）与 client 配置语义（server_url、token）在同一个 config 结构中混乱。
  - C) **`bf` 作为 `binflow-server` 的 alias**：`cmd/bf/main.go` 编译时与 `cmd/binflow-server/main.go` 共享代码但产出独立二进制名。优点：代码复用。缺点：本质是 B 的变体，代码耦合度高。
- 决策: **选 A — 独立二进制 `bf`**。具体设计：
  1. **入口**：`cmd/bf/main.go`，`go build` 产出 `bf`（或 `bf.exe`）。goreleaser 纳入多平台发布矩阵（与 `binflow-server` 同平台，ADR-0004）。
  2. **子命令族**（M6 最小集）：
     - `bf push <repo>/<path> <local-file>` — 上传制品
     - `bf pull <repo>/<path> [--output <dir>]` — 下载制品
     - `bf repo list` — 列仓库
     - `bf repo info <key>` — 仓库详情
     - `bf token create [--user <name>] [--ttl <hours>]` — 签发 token
     - `bf token revoke <id>` — 吊销 token
     - `bf status` — 服务器状态（版本、运行时间、存储统计）
     - `bf replication trigger <name>` — 手动触发复制（M6+）
  3. **配置**：`~/.binflow/config.yaml`（server_url + token），env `BF_SERVER_URL` / `BF_TOKEN` 覆盖。不与 `binflow-server` 的 `binflow.yaml` 共享配置结构。
  4. **实现**：`internal/client` 包（HTTP client 封装：Base URL 拼接、auth 头注入、错误信封解析、重试、进度条回调）。`bf` 子命令只做 CLI 参数解析 + 调 `internal/client` + 格式化输出。
  5. **依赖**：`internal/client` 依赖 `internal/config`（YAML 解析）和 `internal/auth`（Token 验证的客户端侧无需，仅需 HTTP 头注入）。不引入 `cobra`/`cli` 框架——Go 1.26 的 `flag` 包支持子命令，约 50 行自写 dispatch。
- 理由: 独立二进制清晰分离了 server 和 client 的职责与配置空间。`bf` 不携带存储引擎、元数据、HTTP 服务器等 server 组件，二进制体积预计 < 15MB（server 当前 21MB，client 不含 storage/metadata/httpapi）。子命令模式（B）会使 server 二进制膨胀且配置语义混乱。C 是过度工程化。
- 后果:
  - 新增 `cmd/bf/main.go` 和 `internal/client/` 包。
  - goreleaser 配置新增 `bf` 二进制（六平台，与 `binflow-server` 同矩阵）。
  - `internal/client` 包被 `bf` 命令行消费，也可被未来 M6+ 的 Artifactory 迁移工具（`bf migrate`）复用。
  - 依赖约束：`internal/client` 不得 import `internal/storage`/`internal/metadata`/`internal/httpapi`（client 是纯 HTTP 消费者，不嵌入 server 组件）。
  - 技术债：`bf` 的 `push/pull` 在 M6 只支持 Generic 和 raw 上传下载。Docker/Maven/npm/PyPI 的客户端已有原生工具，`bf` 不替代它们（`bf` 定位是管理 + 通用制品搬运）。

---

## ADR-0024: Artifactory 迁移工具——`bf migrate` 子命令

- 状态: Accepted
- 日期: 2026-08-21
- 背景: M6+ 第六项为 Artifactory 迁移工具。迁移工具的目标是帮助用户从 Artifactory 迁移到 BinFlow：读取 Artifactory 的仓库配置、用户、权限、制品数据，转换并写入 BinFlow。这是 ADR-0003（概念模型对齐 Artifactory）的兑现工具。
- 候选方案:
  - A) **独立工具 `bf migrate`**：作为 `bf` CLI 的子命令族（ADR-0023 的 `internal/client` 复用）。迁移 = 读取 Artifactory REST API + 转换 + 写入 BinFlow REST API。
  - B) **`binflow-server` 内嵌迁移端点**：`POST /api/v1/migrate/artifactory`，服务端直接读 Artifactory 实例。缺点：服务端需引入 Artifactory Java 格式解析（XML 配置、Derby DB 格式），且服务端出网拉取另一实例跨越了 SSRF 防护的边界。
  - C) **离线迁移**：先导出 Artifactory 数据为中间格式（JSON），再导入 BinFlow。两步操作，中间格式需额外维护。
- 决策: **选 A — `bf migrate` 子命令**。具体设计：
  1. **三阶段迁移**：
     - **Phase 1: 仓库配置**（`bf migrate repos`）— 读取 Artifactory 的 `export/repositories.config.xml` 或通过 REST API，转换为 BinFlow 的 `POST /api/v1/repositories` 调用。
     - **Phase 2: 用户和权限**（`bf migrate users`）— 读取 Artifactory 的 `export/security/` 目录或通过 REST API，转换为 BinFlow 的用户/组/permission target 创建。Artifactory 的 `$default` 等保留用户不迁移。
     - **Phase 3: 制品**（`bf migrate artifacts`）— 从 Artifactory 逐仓库下载制品（通过 REST API GET），上传到 BinFlow（通过 `bf push` 等价逻辑）。支持断点续传（记录已迁移的 repo+path 清单）。
  2. **Artifactory 端数据源**：主要通过 Artifactory REST API（`/api/repositories`、`/api/security/users`、`/api/security/permissions`、`/api/storage/<repo>/<path>`）读取。这是 Artifactory 的公开 API，不依赖反编译/私有格式。
  3. **转换映射**（ADR-0003 对齐表的逆向应用）：
     - Artifactory `local` → BinFlow `local`（type 和 package_type 直接映射）
     - Artifactory `remote` → BinFlow `remote`（`url`/`username`/`password` 迁移，密码调用 BinFlow 的凭据加密 API）
     - Artifactory `virtual` → BinFlow `virtual`（成员序迁移，`defaultDeploymentRepo` 迁移）
     - Artifactory `permission target` → BinFlow `permission_targets`（`repositories` → `repos`，`includesPattern` → `includes`，`excludesPattern` → `excludes`，`principals` → `principals`）
     - Artifactory `groups` → BinFlow `groups` + `user_groups` 成员关系
  4. **制品迁移验证**：迁移后对每个制品校验 sha256（从 Artifactory 响应头的 `X-Checksum-Sha256` 获取，对比 BinFlow 的 blob 摘要）。校验失败的制品记录到 `migration_failures.json` 日志。
  5. **dry-run 模式**：`bf migrate repos --dry-run` 打印转换后的 BinFlow 配置而不写入。
  6. **不迁移项**（M6 明确不做）：Artifactory 的 `cron` 任务、`property sets`、`mail` 配置、`backup` 配置、`plugins`、`build-info`、`release-bundles`、审计日志历史。
- 理由: `bf` CLI 是迁移工具的自然载体（ADR-0023 的 `internal/client` 可直接复用）。独立工具形态（A）让迁移在用户的工作站上运行，不要求 BinFlow server 出网访问 Artifactory（安全边界清晰）。离线两步法（C）增加中间格式维护成本，且用户需要先导出再导入，步骤多。
- 后果:
  - `cmd/bf` 新增 `migrate` 子命令族（`migrate repos`/`migrate users`/`migrate artifacts`/`migrate all`）。
  - `internal/client` 包临被 `bf migrate` 复用（HTTP client 封装）。
  - 新增 `internal/migrate` 包（Artifactory API reader + 转换器 + BinFlow writer），不 import `internal/storage`/`internal/metadata`（纯 HTTP 客户端）。
  - 迁移文档（tech-writer）：用户指南含「从 Artifactory 迁移到 BinFlow」完整流程。
  - 安全面：Artifactory 源实例的凭据由用户通过 `bf migrate` 的 `--source-url`/`--source-user`/`--source-password` 参数或 env 提供，不进 BinFlow 配置文件。

## ADR-0025: M6 收尾开放问题用户终裁——复制语义 / S3 验收等价 / 会话统一 / 禁用 seam

- 状态: Accepted
- 日期: 2026-08-22
- 背景: M6 在 DoD 盘点（iteration-371/374）时暴露的开放问题 Q2/Q6/Q7/Q8/Q9/Q10 以「暂行实现 / 条件腿」形态冻结，阻塞 milestone closure。Sprint 374 末尾用户授权 conductor 依据过往经验代行终裁。本 ADR 把六项裁决落为正式决议，终结延期仲裁。

- 决策:
  1. **Q6（replica 仓库类型）—— 认领暂行实现为正式决议**：「`target_repo` 指向可写 local backing 仓 + 未配置写路由的 virtual 只读门面（members=[backing]）」即定案，**不新增 `rclass=replica` 枚举**。已知限制（backing local 本身仍可被目标实例用户直写，仅门面只读）记为**已接受**——replica 端完整只读隔离归 M7+ RBAC/复制里程碑。PRD §6.4 复制词表与 Q6 单元格同步改「已定案」。
  2. **Q7（复制冲突策略）—— 认领暂行实现为正式决议**：同 path 同 checksum → 幂等成功（零传输）；不一致 → `status=failed`（attempts 记满防 cron 复活、completed_at 置位、**不动目标**）。正式决议**不引入 `status=conflict` 状态词**（009 任务状态闭集维持），failed + conflict 文案为终态口径。FR-59-AC3 / H47 验收行已按此回写，标记为终裁落定而非暂存。
  3. **Q8（S3 验收环境）—— MinIO 等价 + AWS 实腿延后 M7**：P0 以 MinIO 本地容器全序列 PASS 视为等价；AWS 真实 S3 后续条件腿如需到位，标注 `dep:用户环境`。**Q9（Artifactory 迁移验收）** 同口径：Docker 自建 Artifactory OSS 容器覆盖 P2 验收；真实企业版实例如需，标注 `dep:用户环境`。
  4. **Q10（复制私网目标默认放行）—— 认领暂行实现 + 补 config 桥接**：`DenyPrivateTargets` 默认 `false`（私网放行）为正式决议；T-162 建议的 `replication.allow_private_target`（默认 `true`）config 键落为**后续 P2 票 T-210**，scheme/host 校验、逐跳重检、DNS-rebinding pinning 仍然生效。
  5. **Q2（本地 filestore sessions 入 DB）—— 修订 ADR-0006 决策 2**：**采纳建议，本地 filestore 会话状态迁入新建 `upload_sessions` 表**（migration 010），磁盘 `sessions/<uuid>/{data,state.json}` 路径删除。落地为 **P1 票 T-209**。ADR-0006 决策 2 与后果段「`sessions/` 与 `blobs/` 是版本兼容承诺」相应修订：磁盘 session 目录不再是版本兼容承诺（DB 化为迁移边界）。

     **前提修正（勘误）**：本决策最初表述「与 S3 后端同表/同语义，该表 S3 后端 M6 已建」与事实不符。经 T-209 agent 复核并经 conductor 独立核实：`upload_sessions` 表在 M6 **从未实际落库**（migrations 001-009 无任何 `CREATE TABLE upload_sessions`）；S3 后端会话状态走 minio-go multipart upload（upload ID 即 session ID，状态由 S3 服务端持有），并非 DB；且磁盘 `ResumeSession` 与 S3 `ResumeSession` 均 hard 返回 `ErrSessionNotFound`（重启续传是既定 M2 能力，两腿至今都未实现）。PRD（milestone-6.md）曾*规划*该表，但 S3 实现最终以 multipart 代替、未建表。

     因此口径修正为：**仅本地 filestore 入新建表；S3 维持 multipart（其会话状态已由 S3 服务端持久化，无需另建 DB 表）**。「重启可续传」由「对齐 S3 既有行为」更正为「**新增能力**」——本地 filestore 经 `upload_sessions` 表在重启后恢复会话，S3 的 multipart 中间态本就可跨重启由 S3 持有（若需显式续传另行评估 M7）。
  6. **用户禁用 REST seam（悖论 A）—— P2 票 T-208**：`userCreateBody`/`userUpdateBody` 补 `enabled` 字段（新建缺省 true 不变；disabled 时 bool 指针区分缺省 vs 显式 false），`POST /api/security/users/{name}` 允许 admin 禁用用户，wire 禁用即护栏③（TokenVerifier 重查用户行 `enabled`）端到端验证。与 Artifactory「允许 API 禁用用户」行为对齐。

- 理由: 六项均属 M6 scope 内既定的「暂行 / 条件腿」，无引入新能力的分歧；用户已授权 conductor 依过往经验裁决并终结延期仲裁。Q2/Q10 附带的两个后续票（T-208/T-209）为补契约而非返工——暂行实现本体的正确性不因本裁决受质疑。

- 后果:
  - milestone-6.md §7 开放问题表 Q6/Q7/Q8/Q9/Q10 状态改「已定案」（替换「暂行…待终裁」）；Q2 改「已定案（修订 ADR-0006，实现票 T-209）」。
  - PRD §6.4 replication 词表 / FR-59-AC3 / H47 验收行确认终裁口径，去「暂存」措辞。
  - 后续两张工程票：**T-208**（用户禁用 REST seam，P2）+ **T-209**（filestore session 统一 DB，P1）。
  - **DoD #5 未决**：`git tag m6-done`（及补 `m5-done`）由 conductor 打本地 tag；`git push` 外发仍须用户单独授权，不打不进本 ADR。

## ADR-0026: M7 RBAC——闭集角色（users.role）+ permission target 增 m（manage）动作

- 状态: Accepted（2026-08-23，T-214 终裁转正——M7 立项确认（BOARD 2026-08-23 录板）、架构-PRD 三处分歧已收敛；转正修订五处：① readonly_admin 内容面语义维持原案并终裁（否决 PRD「数据面同权走 target」）；② wire 字段名统一 `adminRole`、枚举值 snake 形 `readonly_admin`（否决 kebab）；③ 路由清点表终版落 architecture §7.1（含建/删仓臂、usage 归域、GC 全路由写三处精化）；④ OIDC/LDAP readonly 组映射定案 `readonly_group` 键；⑤ 即时生效缝 = Verify 重查 role）
- 日期: 2026-08-23（Proposed）→ 2026-08-23（Accepted，T-214）
- 背景: M6 PRD §7 Q4 定案「更细粒度角色（read-only admin / repo admin）归 M7+ 的 RBAC 里程碑」。现状权限模型 = `users.is_admin` 布尔（M1 起，OIDC `admin_group` 映射亦写此列）+ 命名 permission target（r/w/d，M1 起；groups 承载 M4 起）；管理面由 httpapi `routeAuth{admin: bool}` 逐路由硬门。三个表达不了的诉求：① 审计/运维角色「能看全部管理面数据但不能改配置」（read-only admin）；② 「某仓的管理员」——现 model 里仓库配置 CRUD 是全局 admin 门，无法下放；③ ADR-0025 决策 1 遗留的 replica backing 仓只读隔离也依赖仓库域下放的写控制。约束：概念模型对齐 Artifactory（ADR-0003）；clean-room 依据 = docs/reverse/auth-model.md §4（高置信：动作集 read/write/annotate/delete/manage、admin 旁路 permission target、Ant 通配、excludes 优先）；不翻案 ADR-0025 已裁事项。
- 候选方案:
  - A) **仅扩 permission target 动作集（加 `m`，Artifactory 原样）**：仓库级 admin = target 授 m。优点：与 Artifactory 权限面 1:1、改动最小（`permission_principals` 加 `can_manage` 列 + 单仓配置族路由改判 `Can(m)`）。缺点：**read-only admin 无承载点**——BinFlow 管理面是路由级 admin 布尔门，不经 permission target 求值，「能看审计/用量/用户列表但不能改」没有落点；Q4 的半数需求落空。
  - B) **闭集角色列 + `m` 动作（分层）**：`users.role TEXT ∈ {admin, readonly_admin, user}`（代码闭集，非 DB 实体）承载管理面（`routeAuth.admin` → `CanManage(cap)` 六能力闭集 / 单仓族 `CanManageRepo`）；permission target 增 `m` 承载仓库级 admin；内容面 r/w/d 求值不变。优点：三面各归其位（管理面=角色、仓库域=target 的 m、内容面=target 的 r/w/d）；无新表（migration 011 = 两列一回填）；readonly_admin 全域只读 + 管理面只读，语义自洽；`is_admin` 可平滑降级为派生兼容镜像。
  - C) **完整 RBAC 实体（roles/role_permissions/user_roles 多对多、自定义角色）**：最灵活。缺点：Artifactory 本身没有自定义角色（admin 布尔 + target 动作即其全部），实体化反而**背离**对齐目标；角色-权限绑定表 + 求值缓存 + CRUD/审计面是三张票的量，M7 复杂度不成比例；自定义角色 = 权限审计的敌人（闭集才可枚举验证）。
  - D) 外置策略引擎（Casbin/OPA）：破坏 ADR-0005 依赖准入与单二进制哲学，结构上排除（非优劣权衡）。
- 决策: **选 B**。要点：
  1. **闭集角色**：`admin`（≡ 现 is_admin 全量）/ `readonly_admin`（管理面 {system:read, security:read, repo:read} + 内容面全域 r；w/d/m 硬拒）/ `user`（默认；管理面拒绝，内容面走 target）。角色是代码常量；新增角色 = 架构变更（新 ADR）。**readonly_admin 与 target 的关系（T-214① 终裁，否决 PRD FR-64「数据面与普通 user 同权走 target」措辞）**：角色短路——readonly_admin 的内容面（r 恒放行、w/d/m 恒拒）与仓库域求值均不查 permission target，target 行对其无任何效果；「不可组合」= 组合**无效**而非**非法**（不引入 principals 写入或入组的校验报错，组合静默无效果），防「只读管理员还能删」的矛盾配置。
  2. **管理面能力闭集**：`system:read|write`、`security:read|write`、`repo:read|write` 六个，`routeAuth{admin}` 逐路由改写映射（**终版清点表 = architecture §7.1 [M7]，T-214 依 router.go 实际注册点定稿——8 路由族全量，T-215 逐路由迁移施工图；两处文档误差一并勘误：`GET /api/security/token` 列表端点与 `/api/v1/repositories` 全族在 router 中不存在**）。自助族（改密/token 自铸/whoami/session、`/v2/token`）不属管理面，维持 required-only（Q11 裁决口径不变）。
  3. **仓库级 admin = target 的 `m`**（T-214 精化建/删仓边界）：`GET /api/repositories/{key}`（详情）、`POST /api/repositories/{key}`（部分更新）、`PUT /api/repositories/{key}` 的**替换既有仓臂**、quota 字段、`GET /api/storage/{repo}/{path}?permissions` 视图、`GET /api/v1/storage/usage/{repo}` 走 `CanManageRepo`：admin true / readonly_admin !write / user 走 `Can(repo, "", "m")`。**建仓臂（PUT 而 repo 不存在）与 `DELETE /api/repositories/{key}`（删仓）维持全局 `repo:write` admin-only 不下放**（PRD FR-65 边界；router 中建/换仓同路由，由 handler 分臂判门）。usage 保留既有内容面 r 放行（判定 = CanManageRepo(read) ∨ Can(r)，W26b 回归零变更）。m-holder **不获全局列表** `GET /api/repositories`（CapRepoRead；过滤列表 M8+，§11.30）。m-holder 编辑 permission target（POST/DELETE `/api/v1/permissions`）的门 = CapSecurityWrite ∨（target 的 repositories ⊆ 其 m 覆盖集，覆盖集校验在 handler——body 依赖），越界 403。GC 全路由（含 apply=false dry-run）= system:write，readonly_admin 403（T-214 对 PRD Q2 暂行「dry-run 开放」的否决：readonly 角色不 POST 写路由的守门不变量优先；如需开放 M8+ 拆 GET 只读路由）。m 的 target 匹配**只判 repos[]**，includes/excludes 不参与（manage 是仓库配置权、无路径子域，对齐 Artifactory manage 语义）。
  4. **无提权链不变量**：`m` 不开启任何 security:*/system:* 能力——repo-admin 不能改 permission targets（否则可自授全域 w）、不能管用户、不能指派角色（security:write = admin-only）。
  5. **两臂一致性**：docker token 臂（scope pull→r/push→w，ADR-0010）与 REST 臂最终都终止于 `Can()`，单决策点（auth.Service）；m 永不出 docker scope 词表。readonly_admin 的 push token 在 push 时逐请求 403，与 REST 面 403 同源。
  6. **migration 011**：`users.role` 列 + `is_admin=1 → 'admin'` 回填（is_admin 留兼容镜像、substore 同语句维护、M8 移除）+ `permission_principals.can_manage` 列；双方言同步。**wire（T-214③ 统一）**：users PUT/POST body 增 **`adminRole`**（camelCase 字段名对齐 Artifactory security wire 命名——rbac-model §1.2 `adminPrivileges` 同族；**值 = `user | readonly_admin | admin`** snake 形与 DB 列值/代码常量同拼——kebab `read-only-admin` 否决：Artifactory wire 枚举值无 kebab 形态〔access API v2 snake、BinFlow wire 枚举惯例 rclass/grant_type 均 snake〕，wire=DB=常量同拼消灭一层映射；DB 列名保持 `role`，handler 一处映射，quotaBytes→quota_bytes 先例）；admin-only 可写、闭集校验 400、与 `admin` 布尔冲突 400（admin=true ⇔ adminRole=admin）、GET 回显 + whoami/session 同名回显（console 门控消费，只读）；审计 `user.role.change`。**即时生效**：TokenRegistry.Verify 重查 users 行 role（与 enabled 同缝，T-208 护栏③先例）——存量 Token 的管理面权限随角色即时变化（V04）。**OIDC/LDAP readonly 组映射（T-214 显式定案：加映射键，非「不映射」）**：`oidc.<provider>.readonly_group`（组名，group claim 值）与 `ldap.readonly_group`（组 DN）——与既有 admin_group 同节同形；idp_sync 权威式求值（现状 is_admin 每次登录重写，idp_sync.go refreshProviderAdmin 事实）：`admin_group` 命中 → admin **>** `readonly_group` 命中 → readonly_admin **>** 其余 → user；两键缺省不配置 = 行为与今日一致（零配置零行为变化）。理由：readonly_admin 若无映射键则对联邦用户结构性不可维护（每次登录被权威重写回 user）；审计员组经 IdP 供给与 admin 组同构（场景 A）。
- 理由: B 是「Artifactory 对齐」与「Q4 需求可表达」的唯一交集：A 对齐但表达不了 read-only admin（BinFlow 管理面形态与 Artifactory 的 REST 面权限演进不同源，纯照搬留下半数需求空洞）；C 可表达但把闭集问题过度一般化——三个目标角色（admin/readonly/repo-admin）里两个已各有天然载体（is_admin、target 动作），只有 readonly_admin 需要新概念，为其建三张表是为想象买单。闭集还让 QA 可以枚举验证角色×能力矩阵（6×3 全表），自定义角色做不到。**T-214① 补充理由**（readonly_admin 内容面全域只读、否决与 target 组合）：① 角色名是**安全不变量**而非缺省便利——组员身份携带 w 授权 + 只读角色 = 可删的「只读管理员」，恰是 docs/reverse/rbac-model.md 实证的 Artifactory 最近似物（project 域 Viewer 预定义角色，成员单角色制、无叠加）所排除的形态（#1/#2 高置信：实例级无角色层、无 read-only admin，M7 种子 A 校准来源）；② 闭集可枚举验证（Role(3)×动作全表），组合语义使 QA 矩阵无界；③ 「读若干仓 + 写若干仓」本就由 user + target 表达，可组合的 readonly_admin 只制造第二条带优先级歧义的写路径，表达力零净增。groups 不引入角色语义维持（rbac-model #6：Artifactory 组级 adminPrivileges 存在，BinFlow M4 FR-27-AC9 有意不兼容断言维持——role 仅 user 行）。
- 后果: migration 011（双方言）；`Principal` 增 `Role`（`Admin` 降为派生便捷字段，既有 `p.Admin` 消费点语义不变——但**管理面判定代码必须迁移到 CanManage/CanManageRepo**，漏迁即 readonly_admin/repo-admin 静默 403）；httpapi `routeAuth` 结构变更 + authorize() 门位分支；`?permissions` 视图（SE-08）字母集扩 m；console 用户页增角色下拉、仓库页对 repo-admin 可见（过滤列表不做，§11.30）；ADR-0025 决策 1 的 replica backing 只读隔离自此有了下放基座（用 m 收写、门面读）——兑现归 M7 实现票评估；逆向补证 #14 已由 docs/reverse/rbac-model.md（2026-08-23，M7 种子 A 校准规格）回答：实例级无角色层/无 read-only admin（#1/#2 高置信）、组级 admin 布尔存在但 BinFlow 有意不跟进、manage 为 ACE 动作（auth-model §4）而仓库级 admin 的 Artifactory 正式对应物在 project 域——BinFlow 无 projects，target 的 m 是最小诚实同构（rbac-model §5 建议 2 背书），与本决策零冲突。

## ADR-0027: token 铸造二次认证（step-up）——端点 handler 插入、作用域全部非 admin session 臂、OIDC 腿 mint grant

- 状态: Accepted（2026-08-23，T-214 终裁修订转正——M7 立项确认（BOARD 2026-08-23 T-219 录板），Proposed 版的「若 M7 PRD 不立项则顺延」条件解除）
- 日期: 2026-08-23（Proposed）→ 2026-08-23（Accepted，T-214 修订）
- 修订记录（T-214，相对 Proposed 草案五处变更）：① 作用域 SSO 臂 → **全部非 admin web session 臂**（含本地用户）；② OIDC 腿 body `id_token` 新鲜性 → **`prompt=login` 重认证换发单次 mint grant**；③ 错误码 `reauthentication required` → **`step_up_required` / `step_up_invalid`**；④ config 键 `auth.token_stepup_fresh_seconds` → **`auth.token_step_up` + `auth.token_step_up_grant_ttl_seconds`**；⑤ body 字段 `password`/`id_token` → **`step_up_password`/`step_up_grant`**、审计维度 `second_factor` → **`step_up` + `step_up_method`**。
- 背景: M6 PRD §7 Q11 裁决（v1.2）开放非 admin 自铸 Token 时明示留痕「M7+ 可选加固：SSO session 铸管理 Token 需二次认证」。威胁模型（Q11 依据链②）：web session cookie 被盗 → 铸最长 365d 的 API Token = 把小时级劫持窗口（session TTL 24h 封顶）升级为季度级持久化立足点；且 IdP 侧停用用户只杀会话不触发护栏③（验证期禁用检查），Token 存活至 TTL。T-214③ 终裁把作用域从 Q11 留痕的 SSO 臂扩为全部非 admin session 臂，理由：威胁载体是 **session 臂本身**（cookie 即 bearer 证明、与身份源无关）；本地用户部署是 BinFlow 主体形态，只护 SSO 臂护了少数；LDAP 腿的 bind 重验机制被本地腿免费复用（argon2 同形校验）；开关默认 off，零现状行为变化。
- 候选方案:
  - A) **端点级二次凭据（token handler 内），OIDC 腿携 mint grant**：判定「session 臂 ∧ 非 admin ∧ 开关开启」→ 要求二次证明。本地/LDAP：body `step_up_password`（本地 argon2 校验 / LDAP bind 重验，`users.provider` 选腿）；OIDC：console 经 `prompt=login`（或 `max_age=0`）新鲜重认证换发**单次、短 TTL 的 mint grant**，铸造请求 body `step_up_grant` 携带。缺失 → 401 `step_up_required`；失验/过期/复用 → 401 `step_up_invalid`（均 OAuth 形错误体）。优点：插入点单一、零 schema 变更（grant 进程内台账）、`prompt=login` 为 OIDC Core 标准语义而非自造协议；grant 单次性使 cookie 窃贼与 XSS 均无法无声通过。
  - A') **Proposed 原案（OIDC 腿 body `id_token` + auth_time ≤ N 新鲜窗口）**：零服务端状态，但要求 console 在登录后留存 id_token——浏览器凭据留存扩大攻击面：新鲜窗口内 XSS 同时拿到 cookie + id_token 即可**无声**铸 Token，加固被自己的载体击穿；且「登录后 N 分钟免交互铸造」与「铸造即需重认证」的语义目标自相矛盾。T-214 否决。
  - B) **中间件级通用 step-up 层**：`web_sessions` 加 assurance/auth_time 列，authenticator 对「敏感端点族」统一要求高保证级别。优点：未来第二消费方（导出、密钥轮换、删除确认）免费复用。缺点：M7 唯一消费方是 token 签发；assurance 语义（新鲜窗口、提升流、降级）需全局配置面与 console 全局拦截——为一个端点建通用机制是为想象买单。
  - C) **不做**：Q11 四护栏（主体本人、TTL≤365d、验证期禁用检查、token.issue 审计）已对冲大部分风险；session 劫持另有 CSRF/Origin 校验与 HttpOnly cookie 两层。
- 决策: **选 A**（M7 立项为前提已满足）。要点：
  1. 触发条件 = 认证臂 = web session cookie **且** `p.Role != admin` **且** `auth.token_step_up`（默认 false）开启。豁免：admin session、Basic、Bearer（token）、`/v2/token`（本就 Basic 支撑）、匿名（401 挑战照旧）。Q11 四护栏语义零变更。
  2. 端点 = `POST /api/security/token`（唯一在产签发端点；architecture §7.1 主表原 `POST /api/v1/tokens` 行系 M1 规划残留、router 无此路由——T-214 勘误删除。M8+ 若落自有签发端点，本 ADR 同缝适用，防旁路）。
  3. 腿选择 = `users.provider`：local → `step_up_password` argon2 校验（与登录同参数）；ldap → `step_up_password` bind 重验（与登录 bind 同源）；oidc → `step_up_grant`。
  4. **mint grant 契约**：获取 = OIDC login init 携 `purpose=step_up`（authorize URL 强制 `prompt=login`）→ callback 对该 purpose **不建新 session**（已登录前提）、验 state 后换发 grant 并 302 回 console 铸造页；形态 = opaque 256-bit 随机串，服务端只存 sha256；绑定 {username, session_id}；**单次有效**（消费即删）；TTL = `auth.token_step_up_grant_ttl_seconds`（默认 300s，取值域 [60,3600]）；台账 = 进程内存（单实例部署事实；重启丢失 = 用户重走一次 re-auth，可接受，不建表——零 schema 变更）。
  5. 错误体：401 OAuth 形 `{"error":"step_up_required","error_description":"step-up authentication required to mint a token"}` / `{"error":"step_up_invalid","error_description":"step-up credential rejected, expired, or already used"}`。
  6. config：`auth.token_step_up`（bool，默认 **false**——不破既有脚本，企业部署文档建议开启）+ `auth.token_step_up_grant_ttl_seconds`（int，默认 300）。
  7. 审计：`token.issue` detail 增 `step_up: true` + `step_up_method ∈ {password, oidc_reauth}`——仅 step-up 路径铸造时出现；豁免臂不写。
  8. console：SSO 用户铸造按 provider 分流（本地/LDAP = 二次密码框；OIDC = 401 后引导 re-auth 流回跳续铸）。
- 理由: 插入点单一 + 威胁面精确对位：token 签发是唯一「session 臂可达 × 收益量级（24h→365d）」的端点，风险收敛在单 handler；作用域扩至全部非 admin session 臂使威胁载体（cookie 窃取）被完整覆盖而非按身份源打补丁；grant 方案在零 schema 代价下消除 id_token 留存这个自伤面（A' 的否决点），prompt=login 是标准 OIDC 语义；B 的通用层等第二个真实消费方出现再建（届时新 ADR）。
- 后果: token 端点 body 增可选字段 `step_up_password` / `step_up_grant`（不触发的请求不要求、不解析——wire 向后兼容）；OIDC init/callback 增 `purpose=step_up` 分支（T-219）；console 铸 Token 流对 SSO 用户有条件多一步（ux-designer 面）；tech-writer 文档须写「SSO 用户 CLI 铸 Token」路径（OIDC 用户无本地密码，CLI 侧建议先经 console）；护栏② TTL 上限与护栏③④ 不变；本地 session 臂由 Proposed 版的「不触发」改为「触发」——architecture §11.32 边界同步修订；默认关闭下全部行为不变（V25 回归）。

## ADR-0028: Engine.Close 会话语义——干净停机保留未过期上传会话（孤儿回收唯一路径 = 启动 sweep + TTL）

- 状态: Accepted
- 日期: 2026-08-23
- 背景: M7 PRD Q3/FR-67-V16（干净重启续传为目标行为）与 architecture §5.3.1 契约 7 原裁决（O-1：维持 Close 清会话、对外措辞限定「异常中断后」）正面冲突，T-214 收敛终裁。历史脉络：M1~M5 会话为进程内存/磁盘瞬态，Close 清册是防孤儿目录的双保险；T-209（ADR-0025 决策 5）把会话台账迁入 `upload_sessions` 表后，行回收已由「启动 sweep + TTL 过期回填」独立承担，kill -9 后续传成为引擎级既成能力——「SIGTERM 干净停机反而清会话」自此形成**干净路径劣于崩溃路径**的对称性倒挂。
- 候选方案:
  - A) **维持现状**（Close 清空未过期会话；文档措辞限定「异常中断后可续传」）：优点 = 计划内停机后存储目录零在途残留、停机即 clean slate 的运维直觉；缺点 = ① 对称性倒挂不可辩护（kill -9 可续传、SIGTERM 不可——等于激励运维用 kill -9）；② 续传的主价值场景（升级重启/compose restart/维护窗口，PRD 场景 C：2GB 层传到 70% 遇服务器升级）全部落空，FR-67-V16 与 M7 量化门槛（kill -9 与 SIGTERM 双腿可续传）无法达成；③ T-209 后 Close 清册已无结构必要性（回收职责已在 sweep+TTL，清册沦为重复保险）。
  - B) **Close 保留未过期会话**（`upload_sessions` 行 + `uploads/<id>/` 数据文件原样保留），孤儿回收唯一路径 = 启动 sweep + TTL：优点 = 三径（kill -9/SIGTERM/compose restart）对称可续传；Close 契约简化（不再承担回收）；与「sessions 瞬态不进备份面」不变量零冲突（export 本就不含该表，备份产物与语义无变化）。缺点 = 计划内停机后磁盘最多保留 24h TTL 的在途数据文件（孤儿风险由 sweep+TTL 兜底 + FR-70/V32 防孤儿回归钉死）；「停机即清册」的旧运维直觉需文档回写。
  - C) **config 开关**（如 `storage.close_keep_sessions`，默认 B 行为）：优点 = 两种心智可选；缺点 = 为无消费者的分歧面加旋钮（「刚好够用、不为想象买单」）、测试矩阵翻倍、默认值仍需本裁决——只是把冲突转嫁给用户。
- 决策: **选 B**。要点：
  1. **Close 语义修订**：停止接受 BeginSession 等变更（ErrEngineClosed）、排空在途 Append/Commit（由 `server.Shutdown` 30s 窗口先行等待）、**不删除任何未过期会话行与数据文件、也不做过期清理**——sweep 是会话行的唯一回收路径（单一回收路径不变量，T-209 语义保持）。
  2. 可观测性：Close 打一条 INFO——保留的未过期会话计数 + id 清单（截断），替代原「清册 INFO」。
  3. 对外措辞：「跨重启续传」不再限定异常中断；SIGTERM/compose restart/kill -9 一致。
  4. S3 后端不受影响（无 DB 会话、ResumeSession 恒 ErrSessionNotFound，ADR-0025 决策 5 勘误口径维持）。
- 理由: 对称性（干净路径不得劣于崩溃路径，否则等于惩罚规范运维）+ 主价值场景（升级重启续传是 PRD 场景 C 的立项动因）+ 结构约束消失（T-209 后 sweep+TTL 已独立承担回收，Close 清册降级为冗余双保险）+ 孤儿风险有界（24h TTL）且 FR-70/V32 防孤儿回归钉死。export/backup「瞬态不落盘」不变量不受影响——它约束的是备份产物内容，不是 Close 行为。
- 后果: ADR-0006 决策 2 追加修订注记（勘误④）；architecture §3.1（Close godoc）、§4.3（状态机注）、§5.3.1（节首引用 + 契约 7 重写）、§7.4/§9（停机链）、§11（条 11/33）同步修订；T-209 qa O-1 的「干净停机清会话（非回归）」判定在 M6 语境仍成立（当时 Close 清会话确为既定语义、非缺陷，qa 报告为历史记录不回改），M7 起为目标行为变更——PRD §5.6 反转表、FR-70/O-1 措辞与 tech-writer 文档以本 ADR 为准（T-214 报告附回写建议）；FR-70 O-1 落地腿 = V32（SIGTERM 后行+目录幸存 + 无孤儿回归钉死）；风险登记：若未来跨版本升级变更上传数据布局，Close 保留的在途文件须由启动迁移处置（概率低、sweep 兜底）。

## ADR-0029: M8 控制台对齐边界——IA/交互/操作流对齐 + 自有皮肤，clean-room 细则扩展至 UI 域，交互断言验收制

- 状态: Accepted（2026-08-23 转 正——conductor 终审通过 M8 规划四件套（PRD v1.0 + console-ui 行为规格 + console-m8 设计规格 + architecture §13），并终裁 PRD §7 开放问题 Q1~Q6（Q1 基线=7.84.10 实例 / Q2 默认亮色 / Q3 redirect 全量映射 M9 移除 / Q4 Governance 保留 BinFlow 分组 / Q5 前端栈维持现役 / Q6 UI 打磨并入域票），用户推翻出口保留。**转正勘误**：锚数以 console-ux §10 全量核对的 **242** 为准〔本文原写 283〕；spec 目录取现役 `web/e2e/m8/`〔PRD §4 的 `web/tests/m8/` 与现役 testDir 冲突，PM v1.1 勘误项〕）
- 日期: 2026-08-23
- 背景: 用户指令（2026-08-23 原话）：「前端 UI 和交互逻辑要求和 JFrog 一样」。conductor 执行口径：对齐 = 信息架构 + 交互逻辑 + 操作流（Artifactory 用户零学习成本）；服务端契约零改动（M7 的 RBAC/manage/step-up/续传语义全保留）——M8 是承载层（web/）重排，不是后端重写。两个前置裁决缺口：① ADR-0001 的 clean-room 流程以「行为规格 vs 反编译代码」为轴，UI 域的参考素材形态不同（JFrog 官方文档/公开网站/本地 OSS 容器的可观察行为，而非 reverse-src 反编译），且 UI 的视觉表达层（图标/样式/设计系统）受版权保护而无「公开规范」出口——协议对齐可以「以官方 spec 为准」，视觉对齐没有等价物；② 既有验收面（console-ux §10 的 data-testid 体系、18 个 Playwright spec）需要明确 M8 断言形态，防止「像素级像不像」成为事实验收标准。
- 候选方案:
  - A) **全面克隆（视觉 + 交互逐像素复刻）**：最贴近指令字面。缺点：JFrog UI 的视觉设计/图标/设计系统是受版权保护的表达，无公开规范可作 clean-room 出口——像素级复刻的产物本身就是复制证据；且 JFrog 前端与其后端版本耦合（OSS web/rest-ui 模式），逐像素跟随等于把 BinFlow 前端变成其 UI 的衍生作品。排除（合规硬约束，非权衡）。
  - B) **仅信息架构对齐，交互与视觉全自有**：导航分组/概念命名对齐，操作流与组件行为维持 BinFlow M4~M7 现状。优点：零合规风险、改动最小。缺点：「零学习成本」目标落空——学习成本主要来自操作流差异（树浏览与详情面板的组织、Set Me Up 式引导、权限矩阵编辑动线、确认对话时机），仅换导航标签不消除。
  - C) **三层对齐（IA + 交互逻辑 + 操作流）+ 自有皮肤；行为规格制产出、资产零复制、交互断言验收**：对齐「用户可感知的行为」，表达层（视觉资产）自有。
- 决策: **选 C**。要点六条：
  1. **对齐口径 = 三层**：① 信息架构——导航树分组与页面归属、概念命名沿用 Artifactory 术语（repo key / permission target / Set Me Up 等，与 ADR-0003 术语单源原则同构）；② 交互逻辑——组件行为模式（树 + 右侧详情面板的双栏浏览、列表分页/过滤/排序形态、内联编辑 vs 对话框的取舍、四态〔加载/空/错误/成功〕呈现、快捷键、确认对话的触发时机与文案结构）；③ 操作流——完成同一任务的点击路径与步骤序（建仓、上传、授权、GC 等任务的步骤数与顺序）。**视觉皮肤自有**：布局模式（三栏、树+详情、tab 页签）可复刻——布局模式不受版权保护；色彩/字体/图标/间距 token 出自 console-ux §7 自有体系演进，不做像素级克隆。
  2. **clean-room 对 UI 的应用细则（ADR-0001 在 UI 域的等价物）**：
     - **产出形态**：M8 的 UI 依据文件 = 行为规格（docs/reverse/console-ui.md，reverse-engineer 产出 + ux-designer 消费转译为 console-ux v2）——内容限定为：布局结构的文字描述与自绘线框、交互流步骤表、组件清单（职责/props 语义/状态矩阵）、data-testid 锚。规格标注置信度（沿用 ADR-0001 三级）。
     - **允许的参考素材**：JFrog 官方文档站（jfrog.com 文档中的行为描述与示意截图——用作理解行为，不取色不量像素）；本地 Artifactory OSS 容器的**行为观察**（M6 Q9 先例：Docker 自建 OSS 容器做对照）——「观察行为 → 文字规格」。
     - **禁止**：复制 JFrog 前端代码/组件/CSS/SVG 图标/logo/字体/设计系统产物；从其前端 JS bundle 或构建产物中提取任何结构（bundle 与 reverse-src 同属「永不入库、只读参考、产出限行为规格」铁律，且 UI 产物无反编译必要性——行为全可观察）；以 JFrog 截图做像素测量、取色、图标描摹。
  3. **交互断言验收制（非像素对比）**：e2e/QA 断言 = 交互与信息可达性——点击路径可达、元素存在性/可见性/禁用态、四态渲染、键盘流、路由跳转，锚 = data-testid（console-ux §10 命名规则冻结，283 个既有锚按规则迁移沿用）。**截图基线 / visual-regression 对比不作为 M8 验收门**；允许的样式断言 = computed style 对自有 token 的存在性校验（如「暗色主题下卡片背景色变量已应用」）。理由：视觉本就不是对齐口径（自有皮肤），像素断言会把皮肤钉成克隆证据，且截图基线跨平台脆弱。
  4. **服务端契约冻结声明**：M8 不改任何 REST wire（`/api/v1/**`、兼容层 `/api/security/**`、`/v2/**`、session 族）、不改 RBAC 判定（ADR-0026）、step-up（ADR-0027）、续传语义（ADR-0028/§5.3.1）、挂载与资源前缀（ADR-0014：`/binflow/ui/**`、`/binflow/assets/<hash>`、SPA basename `/binflow/ui`、`/binflow/` 301、repo key 保留字并集 {api,v2,docs,console,ui,assets}）、go:embed 构建链（web/dist → internal/console/dist，`make console`）。**例外通道**：行为规格若发现「Artifactory 操作流在 BinFlow 既有 API 面上表达不了」（如 dashboard 聚合数据缺失），必须走 PM 出 FR + architect 评审的独立票，UI 票内不得私加端点——冻结的完整性由「现有 REST 测试零改动全绿」背书。
  5. **范围 = BinFlow 已有功能面**：登录、仓库/制品浏览、搜索、security（用户/组/权限）、governance（审计/GC/配额/备份）、replication、system 信息。Xray/Pipelines/Build-info 等 JFrog 独立产品**不做且不占位**——导航不得出现无功能对应的入口（含禁用态占位）：「看起来有、点了是空壳」比没有更破坏信任与零学习成本目标。
  6. **前端应用内路由可变 + 外部前缀不变**：IA 重排若需要改应用内路径（console-ux §3.2 路由表），允许；约束 = 路由表全量同步 console-ux、深链回归覆盖、e2e 路径断言同票更新。外部可见 URL 面（`/binflow/ui/...` 前缀、资源段）一律不动。
- 理由: C 是用户指令（「交互逻辑一样」指向行为层）与 ADR-0001（表达层不复制）的唯一交集——「一样」的可交付诠释是「Artifactory 用户不读文档即可在 BinFlow 完成全部操作」，该目标由 IA/交互/操作流三层完整承载，视觉皮肤不是学习成本的主要来源且是唯一有版权硬风险的层。行为规格制直接复用 ADR-0001 已验证七里程碑的「规格与实现分离」流程；交互断言复用 console-ux §10 已落地的 data-testid 体系，验收面零新工具、零新依赖。契约冻结使 M8 的回归面收敛为「前端不破后端」单向验证，M1~M7 的 REST 测试资产原样充当守卫。
- 后果: reverse-engineer 新产出 docs/reverse/console-ui.md（M8 UI 票的前置依赖，置信度标注制）；console-ux.md 升 v2（IA 重排后的信息架构/线框/交互四态，§7 视觉 token 节保留并演进）；architecture.md 增 §13 [M8]（承载层重排约束 + web/src 存量存留判定，供 tech-lead 分票）；web/src 存量处置三档——逻辑层保留（lib/api.ts 等）、页面与导航重构、皮肤层重做（判定表见 architecture §13.4）；e2e 既有 18 spec 的断言迁移分两类（testid/交互断言沿用、路径断言随路由表改）；QA 新增「Artifactory 操作流对照验收表」形态（以行为规格为锚）；ADR-0014 的 session/CSRF/挂载决策全部维持；Xray/Pipelines 入口不出现；M8 债券（T-231 percent-encode、B-1 migrate 降级等）与本 ADR 正交，入 PRD 排期。

## ADR-0030: M9 SE 域端点群与权限位扩展——扇出收口 / m-holder 可达性 / 用户域补全（新增不破坏）

- 状态: Accepted（2026-08-24 转正——conductor 终审通过 M9 PRD v1.0 + §14.1 契约 + gap-endpoints 规格；三处口径差已裁：**K20 取 bare array**（集合列表裸数组，PRD 暂行 map 形作废）/ **E7 repos 侧过滤列表延后 M10** / **DELETE users 自删拒删纳入护栏**。展开契约 = architecture §14.1）
- 日期: 2026-08-24
- 背景: M8 契约冻结（ADR-0029 决策 4）随 `m8-done` 结束，熔断线排队的服务端缺口集中兑现：① 用户域（T-237 漂移①②③）——`enabled` 只写不读（T-208）、无 `DELETE /api/security/users/{name}`（auth-model §1 高置信：Artifactory 有，200 text 逐字文案在案）、组无成员查询（rbac-model §1.2 `?includeUsers=true` 高置信）——users/groups 页 N+1 逐用户汇总（T-246 QA-4b 实测）；② 扇出（T-99 缺口 + T-246 QA-4a）——repos 列表「已用」列 per-repo usage ≈ 170 请求/首屏；③ m-holder 可达性（T-241 §3.1 / ADR-0026 §11.30 登记）——`GET /api/v1/permissions` 为 CapSecurityRead 闭集，持 manage 的普通 user 只能走 API 路径，控制台 L2 边界卡兜底。M9 解冻基调（conductor 定）：**新增不破坏——既有端点行为零变，REST 既有 (调用者×动词×路径) 状态码组合零翻转**。
- 候选方案（主争议轴：m-holder 可达性的交付形态，PRD §7 Q3）:
  - A) **既有端点新可选参数 `?filter=manage`**：无 filter 分支字节不变（CapSecurityRead 闭集，m-holder 403 原样）；filter 分支在 handler 内判「CapSecurityRead → 全量 ∨ m 覆盖集非空 → ⊆ 覆盖集子集 ∨ 空集 → 403 同形」。优点：零既有行为差（守护断言零改动）、与族 4 写臂「handler 内覆盖集臂」同构（POST/DELETE permissions 已有先例）、信息隔离边界（NFR-S49）收敛在新分支内。缺点：控制台必须显式携参（m-holder 直链与 admin 全量两条取数路径）。
  - B) **门扩（CapSecurityRead ∨ 覆盖集非空）**：m-holder 裸 GET 即得过滤子集，控制台单一路径。缺点：翻转既有 403 → 200——违反 M9「新增不破坏」基调；无 filter 与有 filter 不可区分，调用方无法声明「我要的就是全量」（m-holder 永远拿不到明确失败信号）；降门扩大管理面读语义的风险不对称（PRD Q3 暂行同理由）。
  - C) **security:read 降门给 m-holder**：能力语义动 RBAC 内核（ADR-0026 闭集被凿穿——m-holder 可读全部 users/groups/token 面），账号存在性隐藏（§7.1 ?permissions B2 族安全姿态）被牺牲。否决。
- 决策: **选 A**。端点群定案（新端点/变更清单表，K18~K21 校准含其中）：

  | # | 端点 / 契约面 | 形态 | 门 | 层级 | 来源 |
  |---|---|---|---|---|---|
  | E1 | `GET /api/v1/storage/usage`（**新路由**；`?repos=a,b,c` 点名可选、`?include=counts` 可选） | bare array，行形与 usage/{repo} 同构 `{repo,usedBytes,quotaBytes}`；include 时增 `{nodeCount,updatedAt}`（updatedAt=配置变更时刻，语义文档钉死） | required + handler 逐仓 `CanManageRepo(read) ∨ Can(r)` **服务端过滤可见集**；空集 `200 []` 非 403；被排除仓零泄露 | C 自有 | FR-79.1 / K20 |
  | E2 | `GET /api/security/users` 列表 additive 加宽 | 增 `email`/`adminRole`（snake 三值闭集）/`enabled`（恒渲染）/`groups: []string`（空 = `[]`）；Artifactory 瘦回显 `{name,uri,realm}` 之上的自有超集（`source` 字段 T-185 先例） | CapSecurityRead 不变 | A 兼容（additive） | FR-78.1 / K19 |
  | E3 | `GET /api/security/users/{name}` 增 `enabled` 回显 | additive bool 恒渲染——与 T-208 写侧 *bool 闭环 | CapSecurityRead 不变 | A | FR-78.1 |
  | E4 | `DELETE /api/security/users/{name}`（**新动词**） | 成功 200 纯文本 `The user: '<name>' has been removed successfully.`（auth-model §1 逐字）；护栏全 **400**：内置 admin / 删除后无 admin 角色（`Cannot delete user '<name>'. There must be at least one user configured with admin privileges.`——组侧 rbac-model §1.2.2 同族文案与状态码镜像）/ **自删**（`Cannot delete the current authenticated user.`，离场路径=禁用 T-208）；同事务级联：permission_principals 行删、user_groups FK、tokens 全删（Verify 缝即时 401）、web_sessions 全 revoke；审计 `user.delete`（audit_events 保留） | CapSecurityWrite（无覆盖集臂——用户非仓库域主体） | A 兼容（护栏为自有增强） | FR-78.2 / K18 |
  | E5 | `GET /api/security/groups/{name}?includeUsers=true`（additive 参数） | 带参增 `userNames: []string`；不带参行为字节不变；**groups 列表不加宽**（membersCount 不落 wire——成员汇总单一事实源 = E2 的 users.groups 客户端推导 + 本参数按需） | CapSecurityRead 不变 | A | FR-78.3 / K19 |
  | E6 | `GET /api/v1/permissions?filter=manage`（additive 参数） | 候选 A 形态（上表外详述见 §14.1.6）：无 filter 字节不变；filter=manage → CapSecurityRead 全量 ∨ 覆盖集非空得 ⊆ 覆盖集子集（部分覆盖 target 隐藏）∨ 空集 403 同形；filter 值 ∉ {manage} → 400 errors[] 信封 | filter 分支 handler 判（route 维持 required-only） | C 自有 | FR-79.2 / K21 |
  | E7 | `GET /api/repositories` m-holder 过滤列表 | **延后 M10+**（PRD §2.2 Non-goal；CapRepoRead 维持，DeployDialog 双 403 降级臂继续承载；§11.36 债券登记 + 触发条件） | — | — | PRD Q5 关联 |
  | E8 | 组 `adminPrivileges` 字段 | **不做**：组级 admin 布尔 = 绕开 users.role 闭集的第二条提权路径，摧毁 Role(3)×能力 QA 矩阵可枚举性；UI 以 manage 持有徽章同构（T-237 已落） | — | D 有意差异 | PRD EP-05/Q4 |
  | E9 | `auth.ManageCoverage(ctx, p) (map[string]struct{}, bool)` | Go seam（无 wire）：principal（含组）持 m 的 target 并其 repos；E6 filter 分支与族 4 写臂现存覆盖集求值收敛到同一函数（单决策点）；O(targets×principals) 每请求一次，缓存留缝不实现 | — | 内部 | E6/族 4 |

  **K18~K21 校准**（PRD §5.5 回写）：K18 = 200 text 逐字 + 三护栏全 400（自删拒删——PRD Q2 暂行转正，理由：离场既有禁用路径、自删使会话中途吊销形似缺陷、无用户诉求）；K19 = 加宽字段清单如 E2/E5，groups 列表不加宽（防 membersCount/groups[] 双事实源漂移）；K20 = **bare array**（PRD 暂行 map 形否决——bare array 是 /api/v1 列表族既有惯例：permissions、replications 两先例；PRD N06 的 `jq '.repos[...]'` 示例需按 `.[] | select(.repo==...)'` 调整，归 PM 一行勘误）+ `?repos=` 逗号分隔点名；K21 = `filter=manage` 定案、值闭集单值。**零 schema 变更、零新配置键、零迁移**（users.enabled T-208 已落、user_groups/repo_usage/permission_principals 均在）。
- 理由: A 是「m-holder 可达性」与「M9 新增不破坏基调」的唯一交集——B 的可达性收益与 A 完全等价（过滤子集 + 越界隐藏），代价却是既有断言翻转与全量/子集不可区分；C 动安全内核。E1~E5 全部为 additive 或新路由，兼容矩阵 A 4 条（EP-01~04）/ C 3 条（EP-06/07 + E9 内部）/ D 2 条（EP-05/09 关联）与 PRD §5.2 对齐。E4 护栏取 400 非 409：镜像组侧 last-admin 保护的实测状态码（rbac-model §1.2.2），同族文案降低 Artifactory 用户学习成本。
- 后果: 实现分区——FR-78（internal/auth + httpapi security.go 族）与 FR-79-79.1（storage/repo 域 usage 聚合查询）与 FR-79-79.2（permissions filter 分支 + auth.ManageCoverage）area 不重叠可并行；审计新增 `user.delete`；T-241 e2e「m-holder GET 列表 403」腿**原样保留**（新增 filter=manage 腿另立，非翻转）；T-240 repos 列表 403 L2 腿零改动（E7 延后）；守护测试按 §14.5（RBAC 矩阵白名单为空 + wire golden 快照 + 无参数分支逐字节对照）；UI 消费面——repos 页「已用」列单请求注水、users 页 Status 列回显驱动、L2 边界说明卡退役（换 filter 取数路径，编辑器内 B1 注记维持）；PRD 回写项三处（N06 jq 形态、§5.5 K18~K21 落定、Q2/Q3 暂行转正）。

## ADR-0031: GC 并发安全模型——在途持有集 + 删除前引用复核（A+B 组合），graceHours=0 零误删

- 状态: Accepted（2026-08-24 转正——conductor 采信 A+B 组合（关 W-1/W-2 两窗口）；**顺序硬规则**：压力 spec 进 CI（T-256）必须先于 --workers=1 解除（T-268）。展开契约 = architecture §14.2）
- 日期: 2026-08-24
- 背景: T-232 实测：e2e 并行全量下 `graceHours=0` 的 gc apply 与其他 spec 的上传/删除语义互斥——`manifest PUT 500 blob not found`（t134-g32，GC 物理删除了并行 docker 推送的在途层）；权宜 = 全量验收 `--workers=1`，成为一切并行验证的系统性瓶颈。竞态解剖为两窗：**W-1 引用前窗口**（blob-first 落盘序：rename 进 blobs/（T1）→ 元数据 node 行提交（T2）；mark 快照落在 (T1,T2) 则 blob 无引用可见。docker「先传层后传 manifest」把毫秒窗放大到秒级）；**W-2 快照陈旧窗口**（单次 GC 内 mark 快照（T0）→ 目录遍历删除（T1），(T0,T1) 间完成全链提交的 blob 对快照不可见）。正常态两窗均被 mtime grace 兜底；`graceHours=0`（handler 映射亚秒时长，「无宽限期」是 W24 既有菜谱的显式合法值）击穿之。
- 候选方案:
  - A) **引用集复核（删除前单点复查）**：apply 对每个候选在物理删除前查 `nodes ∪ docker_refs` 单点存在性（新增 metadata `IsReferenced(sha)` 助手，走既有 idx_nodes_blob/idx_docker_refs_blob 索引）。关 W-2 ✓；关 W-1 ✗——node 行尚未提交，复查仍未见（docker 层-_manifest 窗口正是此形）。代价：每候选一次索引查询。
  - B) **在途持有集（engine hold set）**：`Commit`（disk rename / S3 CompleteMultipartUpload 时刻）把 sha256 注册进进程内持有集；repo.Service 元数据事务成功后 `ReleaseGCHold(sha256)`；TTL 过期兜底。关 W-1 ✓（落盘即持有，与引用可见性解耦）；关 W-2 ✗（release 后快照陈旧仍在）。代价：Engine 新 seam × 五条落库路径接线（Put/PutFromBlob/PutLandedBlob/PutManifest/remote pull-through）；漏 release 最坏后果 = blob 延迟 TTL 可回收——**保守方向的失败**，正确性无损。
  - C) **apply 前置 drain（写面静默）**：BeginSession 拒新（503）+ 在途会话有界排空后 mark-sweep。两窗全关 ✓。代价：可用性面（写者状态机 + 排空超时策略——10GB 在途层悬着怎么办）+ 与同步 REST 执行模型叠加——为一个 admin 触发、已持数据目录维护锁（ADR-0015 勘误③）的维护操作引入停写语义，不成比例。否决。
  - （评）**grace 下限拒绝 apply**：apply 拒绝 grace < 下限。两窗全关 ✓ 但破坏 W24 既有菜谱（upload → 删 node → graceHours:0 收）与 REST 契约（graceHours:0 显式合法值）。否决。
- 决策: **A+B 组合（W-1 归 B、W-2 归 A），C 否决**。要点：
  1. hold set 生命周期：`Commit` 注册 → 元数据提交成功后 release → TTL 兜底（`storage.gc_hold_ttl_seconds` 默认 600、下限 60，显式零值取默认——grace 同款零值语义）。release 是加速、TTL 是正确性兜底：进程崩溃在两者之间只会让 GC 暂时保守，不会 wedged。
  2. GC 候选判定两道闸：hold 集命中 → 跳过（不看 grace）；apply 删除前 `Live(sha)` 复核命中 → 跳过。dry-run 同样排除 hold 中 blob（安全方向；静默实例 grace=0 菜谱不受影响——上传完成后 hold 已 release，W24 行为不变）。K22 口径：同进程 dry-run 精确排除；CLI 跨进程 dry-run 可多报（无破坏面，报告标注「serve 运行中、在途上传不可见」）。
  3. 接口升级：`Engine.GC` 的 `referenced` 参数升级为 `GCMarker{ Mark() (map[string]struct{}, error); Live(sha string) (bool, error) }` 两方法接口（旧 func 形态适配器兼容）；DiskEngine/S3Engine/MigrationEngine 三实现同步（S3 的 Live 走单点 HeadObject）；`ReleaseGCHold(sha256) error` 进 Engine 公开面（§3.1 [M9] 注记）。
  4. 跨进程面：REST GC 与 serve 同进程 → hold 集可见全保护（e2e governance 腿即此面，`--workers=1` 解除的前置）；CLI gc 独立进程不可见 → 增 serve 心跳锁 `<data>/serve.lock`（进程生命周期 flock）：CLI **apply** 且显式 grace < 60s 且探测到 serve 持锁 → 拒绝执行退出非 0（提示走 REST 面或停服窗口）；默认 grace 的 CLI 运行不受限。
  5. REST gc 响应形状零变（candidateCount/candidateBytes/deletedCount）；候选计数可能因 hold 排除变小——语义方向 = 更保守，登记即可。无并发时的 dry-run/apply 结果与 M4~M8 逐字节同源（候选判定、审计、stats 口径零回归）。
- 理由: 两窗口成因不同（W-1 是「引用尚未存在」——任何基于引用的复核都看不见；W-2 是「引用已存在但快照太旧」——单点复查恰好覆盖），单一机制无法同时关两窗，A+B 各击其一且失败模式均为保守方向；C 用停写换正确性，在「GC 已持数据目录锁、admin 显式触发」的语境下过度；grace 下限则直接违约既有契约。hold set 的进程内形态依赖单实例部署事实（§9 多副本本就禁止/实验性），不引入跨进程协调复杂度——serve.lock 只做「拒绝」不做「协调」。
- 后果: `internal/storage` Engine 接口签名变更（GCMarker + ReleaseGCHold，§3.1 [M9] 注记）；repo.Service 五条落库路径接线 release（漏接 = 保守，可容忍但 review 清点）；新配置键 `storage.gc_hold_ttl_seconds`（§8）；CLI gc 增 serve.lock 探测；压力回归 spec（并行 docker push × grace=0 apply 循环零 blob-not-found 钉 W-1；并发「上传→删 node」× grace=0 apply 零误删钉 W-2）进 CI 后才允许解除 `--workers=1`（§14.5-5 顺序硬规则）；`go test -race ./internal/storage/... ./internal/repo/...` 全绿为 P0 验收；e2e 默认并发 3 连绿（FR-80-AC2）后 Makefile/脚本/文档的 `--workers=1` 约定降级为历史注记。

## ADR-0032: license/entitlement 体系与门控模型——自有 ed25519 文档、三档闭集、数据面单点门控

- 状态: Accepted（2026-08-25 转正——conductor 终审通过 M10 规划三件套（PRD v1.0 + architecture §15 + 主矩阵）；四处 PRD↔ADR 分歧按「以 ADR 为准」裁定并路由 T-293 as-built 收口）
- 日期: 2026-08-25
- 背景: 用户指令（2026-08-25 原话）：「binflow 也需要拥有和 Artifactory 的 license 控制，例如控制高可用等」——对标其功能分级门控行为模式（inv-2 §3：SubscriptionType 十档 × AddonType 80 项注解门控 + `artifactory.addons.disabled` 全局禁用 + `/api/system/licenses` 管理面 + 无订阅 403 映射〔inv-4 O5 实证〕）。clean-room 硬边界：不复制 JFrog license 密钥格式/校验算法/激活协议，只对齐行为模式。约束：M9 刚收口的端点族与 RBAC（ADR-0026/0030）不可破坏；ADR-0005 依赖准入（默认 stdlib）；单二进制零外部服务（无 callhome——inv-4 J4 明确不适用）。
- 候选方案:
  - 文档格式: A) **自有 JSON 载荷 + ed25519 签名（JWS compact 最小子集，stdlib 自实现 ≈80 行）**——`crypto/ed25519` 是 stdlib，零新依赖；签名对象 = payload 原文字节（免 canonicalization）；字段全部自有（clean-room 直接满足）。B) JWT（RFC 7519 EdDSA）经 golang-jwt/jwt——解析器成熟但引外部依赖破 ADR-0005 白名单，且 JWT 的 token 心智（短时效/aud 校验）与 license 文档（长效/逐字段语义）错位。C) X.509/PKCS#7——证书层级重武器，单签发方场景（一个 vendor → N 实例）用不上，且把格式复杂度变成攻击面。
  - 档位模型: A) 对标十档全量复制——无自有商业分层，多数档位（pro_xray/enterprise_plus 等）是 JFrog 产品线的映射，复制即谎报；B) **三档闭集 community/pro/enterprise**（行为模式对齐 oss/pro/ent 的「地板/中档/高档」结构，命名自有）；C) 二档（免费/商业）——表达不了「HA 等 ent 能力 vs 包型等 pro 能力」的用户明示分层需求。
  - 门控织入: A) middleware 拦截（路由前缀 → addon 映射）——门控判定是数据依赖（repo 行 package_type）+ 动词依赖（读放行/写拒绝）+ 状态依赖（tier），前缀映射三者皆表达不了；/v2 根级例外与 dispatchAPIProtocolMount 族绕出映射表——拦不住旁路的门 = 没有门；且给 §7.2 链序新增环节，违背 M9 不破坏基调。B) **能力注册表查询**（license.Manager 单点，三个既有决策缝之后插入：dispatchContent 写动词臂 / repo 建仓校验链 / feature addon handler 首行）。C) 编译期裁剪（build tag 决定编不编）——单二进制差异化被破坏（一个产物一个档），且 license 安装无法在线升级能力。
- 决策: **文档 A + 档位 B + 织入 B**。要点：
  1. **文档 v1** = `<base64url(payloadJSON)>.<base64url(ed25519Sig)>`；payload 字段 typ/alg/kid/ver/licenseId/licensee/tier/issuedAt/notBefore/expiresAt（community 可 null=永久）/addons（可选显式白名单）/limits（预留）——全部自有（architecture §15.1.1 定稿）。**签发链**：`bf license keygen/issue` 离线子命令，私钥 `--key`/env 注入永不入库；**验签公钥编译期内嵌**（kid→公钥单元素表，多 kid 轮换缝预留），测试经 Manager 构造注入测试钥对。
  2. **校验链**（安装/启动/每日 ticker 三处同一函数）：parse → typ/alg/kid 静态校验 → ed25519.Verify(payloadBytes) → 时间窗（leeway 1h）→ licenses 表落库（单行单证模型，**新验通过才替换**）；运行期 State 为 atomic 快照，expiry 跨越由每日 ticker 降级（无需重启）；全程离线。
  3. **降级行为闭集 D1~D7**（architecture §15.1.3）：读路径放行（数据不劫持）/ 写动词 403 + `X-Binflow-License-Required` 头 / 建仓 400 点名 / feature REST 403 / console 可见带徽章 / 到期即降无 grace / 安装失败不动现状态——闭集可表驱动枚举（QA 矩阵锚）。
  4. **REST 面**：`GET/POST/DELETE /api/system/license`（单数——单实例单证；Artifactory 复数 + activate/licenseChanged 是 HA 多证语义，随 HA addon 届时扩）；GET = CapSystemRead、POST/DELETE = CapSystemWrite；审计 license.install/delete/invalid。
  5. **不破坏论证**：门恒在 RBAC 之后（无权者仍收 RBAC 401/403，既有角色×路由矩阵零翻转）；五核心包型钉 community 地板恒过门（未装 license = 与 m9-done 逐字节一致）；routeAuth/enforce/authorize 零改动，license 以 Deps 注入不进链。
- 理由: ed25519 stdlib 是 ADR-0005 依赖纪律下唯一「零成本密码学」选项，自有格式让 clean-room 从根上成立（格式即设计而非抄写）；三档闭集对齐行为模式而不谎报产品线，闭集可枚举验证（ADR-0026 同哲学）；数据面单点门控以最小表面积织入——三个缝都是既有决策汇点（contentAction 的写标志、repo 校验链、handler 首行），零 middleware/路由结构改动是「新增不破坏」的结构性保证。
- 后果: 新增 `internal/license` 包（Manager/文档解析/验签/ticker，零外部依赖）；migration 012 含 licenses 表（双方言）；`bf` 增 license keygen/issue 子命令族（签发工具链，私钥管理规程归 tech-writer + §11.38）；§7.1 路由表增三端点（族 1/2）；config 增 `addons.disabled`（与 ADR-0033 共享）；守护面 = 无 license 实例跑 M9 基线矩阵期望文件零改动（§15.6-1）+ Tier×Addon×面 全表驱动；license 过期的邮件预警不做（无邮件子系统，§11 登记）；档位-能力矩阵是代码常量非数据（改档 = 发版，不建可运行时篡改的许可面）。
- **as-built 定案段（T-293 收口，2026-08-26 追加，不改动上文）——D1 优先于 PRD 85.3 的「disabled 读 403」**：PRD FR-85.3 写「该包型建仓/读/写全部 403」（含 AC4 的 install 403），与本 ADR D1（读路径恒放行——数据不劫持）正面冲突；conductor 规划期已裁定「PRD↔ADR 分歧以 ADR 为准」，T-283 按 D1 落地并验证。**终裁：D1 优先，85.3 的读面措辞作废**。语义定案：`addons.disabled` 熔断是**写平面与配置面的断路器，不是数据墓碑**——breaker 下 gated 包型的内容 GET/HEAD 恒 200（含 remote pull-through 的内部写豁免——门是 HTTP 动词面的，门后 nobody re-asks），npm install 等读客户端在熔断期照常工作；拒绝面 = 写动词 403（breaker 专用文案点名旋钮与恢复路径，**不带** `X-Binflow-License-Required` 头——装 license 救不了 config 熔断，头名不得说谎）+ 建仓/改仓/删仓/virtual 成员 400（D3）。证据：T-283 `TestT283DisabledBreaker`/`TestT283ServiceWritesBypassGate`（全形态读 200 + 门 DENIED 下 pull-through 落盘成功）。附带两项 as-built 校准随本段收口：① `/api/system/licenses`（复数旧路径）的拒绝形态 = **E-26 404**（K25 终裁——无路由即引导，非 400）；② PRD §9-4 期望本 ADR 承载的 K29（MPU scope 等价物）未入正文，as-built = 认证 + 目标仓写门（routeAuth required + path `w`）、会话 id 为不可猜测 capability（§5.3.1 契约 4 同源）、complete 校验链 sha256 必填 + sha1/md5 可选错配 409——契约展开见 architecture §15.4 as-built 块。

## ADR-0033: addon 注册表与包型 addon 化——编译期显式装配清单、Addon 描述符、五核心 retro-fit

- 状态: Accepted（2026-08-25 转正——conductor 终审通过 M10 规划三件套（PRD v1.0 + architecture §15 + 主矩阵）；五核心 retro-fit 为 community 地板——M1~M9 既有能力零降级）
- 日期: 2026-08-25
- 背景: 用户指令（2026-08-25 原话）：「补齐剩余的协议，例如 golang，huggingface 等，这也是 license 控制的功能，和 Artifactory 一样使用 addon 的方式加入进来」——52 缺失包型全部纳入后续规划、多里程碑分期，**包型 = addon 门控单元**。对标行为模式 = META-INF/addon.{xml,properties}（静态装配单元 + 描述元数据 + 档位标注，inv-2 §3/§4）与 AddonType 80 项「功能 → 档位」权威表；形态自有（clean-room 不复制 Spring bean 集格式）。架构前提：既有 adapter registry 已确立「显式 RegisterX 由 cmd 装配调用、no package-level singletons」约定（internal/adapter/registry.go 注释），addon 注册机制必须与之同构。
- 候选方案:
  - 注册机制: A) 各包 `init()` 自注册——Go 社区惯用，但与本 codebase 既有约定冲突（adapter registry 明示弃用 init：装配时序藏进导入图、依赖注入面无法进入——license.Manager 必须能到达每个 addon 的求值）。B) **显式装配清单**——每 addon 包导出描述符构造函数，cmd/binflow-server 装配处汇聚 literal slice；对标 addon.{xml,properties} 的行为模式（静态装配单元），形态 = Go slice。C) `go:generate` 生成清单文件——为静态可 grep 的清单引入生成步骤，生成物与源码两份事实、漂移风险。
  - 存量五包型: A) **retro-fit 为 community 地板 addon**（统一模型，MinTier=community 恒解锁）；B) 五核心豁免（分发直通、新型过门）——ForPackageType 对五型返回 ok=false 的特判散落 + 建仓校验两套清单，为省五张描述符造长期双轨。
  - Addon 形态: A) **单一描述符 struct + Kind 二分（package-type/feature）+ 可选行为接口**——差异在消费缝不在类型层；B) 大接口（Addon interface 含 Enable/Routes/Metadata 全家桶）——Java 式抽象层（PRODUCT 明示不做），feature addon 的 REST 挂载在 M10 没有真实样本，接口先行即想象买单。
- 决策: **注册 B + 五核心 A + 形态 A**。要点：
  1. **`internal/addons` 包**：`Addon{ID, Kind, MinTier, PackageType, DisplayName, Description}` 描述符 + `Registry`（New/ByID/ForPackageType/PackageTypeAddons/FeatureAddons）；装配期断言（重复 ID、Kind 字段错置、PackageType ≠ adapter.Protocol()）→ panic 启动期暴露（adapter.Register 同款姿态）。装配清单 = cmd 里 literal slice（§15.2.1 示意）。
  2. **包型 addon 与功能 addon 统一**：包型 addon 的行为载体 = adapter.Handler（既有注册与分发零改动，§5.1「新协议 = 新增子包 + Register，零改动核心」承诺保持）；feature addon（ha/xray-integration/distribution 等）M10 只落描述符 + 门控 seam + /api/v1/addons 可见性，**本体不实现**（首个真实样本 HA 归 M11+，届时 REST 挂载走既有 router 装配 + handler 首行 license.Require，不新增挂载框架）。依赖方向：`addons → license`（仅 Tier 类型）；license 不 import addons（AddonsEnabled 取原语参数防环）。
  3. **档位 × addon 解锁矩阵实现位** = `license.Manager.AddonEnabled(ctx, id, minTier)` 单一求值点：disabled config 命中 → false；tier ≥ minTier（community<pro<enterprise 全序）→ true（地板档不受 addons 白名单约束）；显式白名单存在且不含 id → false；档位不足 → D1~D7 降级闭集。**矩阵是代码不是数据**（MinTier 是描述符常量；调档 = 改代码发版）——运行时可改的许可面是安全审计的敌人。
  4. **五核心 retro-fit**：community 地板、行为零变化；`GET /api/v1/addons`（CapSystemRead，bare array：id/kind/minTier/enabled/reason/displayName/description）统一呈现，enabled/reason 与门控同源求值（可见性与执行不可能分叉，?permissions 视图先例）。
  5. **试点包型（M10 种子 C，建议 Go + Cargo）**：`internal/adapter/goproxy`（包名避开 go 关键字，Protocol()="go"，GOPROXY 协议以 golang.org 官方规范为基准）与 `internal/adapter/cargo`；MinTier 缺省 pro（档位归属 PM 终裁）；挂载与五包型完全同构；NuGet 移 M11（双协议栈三票量级，做试点会挤压基座验证信号）。试点验收锚 = 「注册→门控→license 缺失行为」全链（无证建仓 400 / 预置仓 push 403+头 / pull 200 / 装 pro 证全通）。
- 理由: 显式装配与本 codebase 的 adapter 约定同构（一个 codebase 一种习惯）、编译期完备（漏注册 = 编译错误）、可 grep 可 diff——对标 addon.{xml,properties} 的「静态装配单元」行为模式而形态自有；五核心 retro-fit 让「包型 = addon」成为无例外的单一模型，分发/建仓校验/console 呈现各只维护一条路径；矩阵代码化 + 单求值点沿袭 Can/ManageCoverage 的「单决策点」先例，闭集可枚举验证。
- 后果: 新增 `internal/addons` 包；五包型各增 `Addon()` 描述符导出（一次性小票）；config 增 `addons.disabled`（CSV，含五核心 id 时 WARN 但生效——排障旋钮，ADR-0032 共享）；console 建仓对话框与 addons 管理卡片消费 /api/v1/addons（console-ux 增补，档位徽章交互见 §11.41）；52 缺失包型的档位归属表（哪些 pro/哪些 enterprise——HuggingFace 含 xet CAS 等 AI/ML 13 型的建议归属）归 PM 以本 ADR 的 MinTier 机制填充，逐里程碑随实现票生效；试点票验收必须含真实客户端（go list -m / cargo build --registry）命令级证据。
- **as-built 附注（T-293 收口，2026-08-26 追加，不改动上文）**:
  1. 决策 5 的「NuGet 移 M11」建议被 PRD Q4 终裁推翻——M10 试点定 **Go + NuGet**（Cargo 归余量/T-294），T-285/T-287 均已落地；本 ADR 其余机制（显式装配/描述符/单求值点/retro-fit）按原文兑现，试点结论反而验证了「挂载与五包型完全同构、零 httpapi 核心改动」的承诺。
  2. 动态建仓谓词（T-283 兑现决策「建仓 400」）：`validateRepoTypeDyn`——gate 已接线且 registry-known 的包型（go/nuget/cargo）三 rclass 全开；静态五型保留 M3 class 裁定（docker 仍 local-only）；unknown 包型仍走静态枚举 400；槽位未解锁/被熔断 → **400** `ErrPackageTypeNotAvailable`（D3 形，PRD 85.1① 的 403 措辞不采用——配置校验面，非许可面），Create/Update/Delete 三面 + virtual 成员面（locked 成员拒绝建/改 virtual）四处接线。路由/门位 as-built 清点已回写 architecture §7.1。

## ADR-0034: 新协议包型的两个横切定案——管理面统一挂 `/binflow/api/<proto>/`（dispatchAPI 族）、对外绝对 URL 统一取 `server.base_url`

- 状态: Accepted（2026-08-26——FR-91-AC3 tech-lead 就绪度验收〔reports/agents/tl-fr91-ac3.md §4 K-3〕的两条 ADR 补记，T-293 落笔；M11 首张五协议适配票（conan/debian/rpm/helm）前生效）
- 日期: 2026-08-26
- 背景: FR-91 五份行为规格（conan/cargo/debian/rpm/helm）拆票时暴露两个每协议都会重遇的横切问题，逐票自裁会造五种答案：① 管理面 REST（reindex 族等 Artifactory 挂 `api/helm/{repoKey}/reindex`、`api/deb/...`）走哪条挂载机制；② 协议响应里必须自指的绝对 URL（cargo config.json 的 dl/api、conan upload_urls、helm index urls、各协议 Location 头）base 从哪来。代码现状：`server.base_url` 已存在（internal/config，「externally visible URL; empty = derive from request」）；npm/nuget/docker 三 adapter 已有 `Options.BaseURL` 注入先例；`apiProtocolMounts` 为封闭清单且注释明示「a protocol earns a mount only through its own ticket」；管理 REST 走 `dispatchAPI` 显式路由族。
- 候选方案:
  - 管理面挂载: A) **`dispatchAPI` 显式路由族**（router.go 各 family 同模式：`case rest == "<proto>/..."` + `routeAuth`）——管理面是 REST 语义，有独立的门（认证 + CanManageRepo）、独立的错误信封惯例。B) `apiProtocolMounts` 内容面重写——该清单的语义是「把 /api/<proto>/<repo>/<rest> 重写到内容面 dispatch」（RBAC + addon 门 + adapter 分发一次跑完），管理端点经它会被送进 adapter 分发而非 handler，门语义错位；且清单封闭是有意设计（防 look-alike 前缀）。C) 每协议自起 mux——与「单 ServeMux + dispatch」的既有结构（ADR-0005）冲突。
  - URL base: A) **统一 `server.base_url`，空值回退请求 scheme+host 推导**（X-Forwarded-Proto 认可）——配置一处、行为与 npm packument / nuget service index 现行先例完全一致。B) 每协议自造配置键（`cargo.base_url` / `conan.base_url`...）——同一问题的 N 份答案，运维心智与文档面双输。C) 恒请求推导不配置——反向代理后 host/port 形态不可信，npm/nuget 已证需要可配。
- 决策: **管理面 A + URL base A**。要点：
  1. 五协议（及后续全部包型）管理面统一挂 `$BASE/binflow/api/<proto>/...`（Artifactory 同构：conan reindex / deb reindex / yum reindex / helm reindex 同形，用户肌肉记忆直迁）；实现走 `dispatchAPI` 显式路由族，权限门 = 认证 + CanManageRepo（TL 验收 DB-4 定案）；**不是** `apiProtocolMounts`（该清单只承载内容面别名重写）。
  2. 全部新协议的对外绝对 URL（config.json 的 dl/api、upload/download_urls、index urls〔absolute 模式〕、Location 类响应头）统一取 `server.base_url`，空值回退请求推导（TL-1 定案）；不新增配置项、不各协议自造。 Helm `api/helm` 别名（HL-1）是**内容面**别名，走 `apiProtocolMounts` 加 "helm"——与第 1 条的管理面不冲突，两者是不同清单、不同语义。
- 理由: 两个定案都是「同一 codebase 一种习惯」的执行——npm/nuget/docker 先例已把 `Options.BaseURL` 注入和 dispatchAPI 族各自的正确用途演示在案，本 ADR 只是把先例升格为对新协议的强制契约，消灭逐票重裁；管理/内容两清单的语义分界借此钉死（管理 = handler + CanManageRepo，内容 = 重写 + adapter 分发），后续 review 有单点可引。
- 后果: M11 五协议拆票（T-294 Cargo local 起算）的 AC 直接引用本 ADR 两条；`apiProtocolMounts` 的扩容仍需各自协议票（封闭清单纪律不变）；helm.md §1.1/§7.3、cargo.md §3.1/§8 的规格建议修订已随 T-293 合入；`server.base_url` 成为多协议共用契约键，改其语义（如强制非空）需回本 ADR 评估。

## ADR-0035: 认证配置管理面——DB 配置描述符、license 式原子快照变更即生效、双源优先级（K31 终裁）、脱敏与测试连接边界

- 状态: Accepted（2026-08-26，M11 规划期 T-301 交付；REST 路径字面量/信封字段拼写/三协议字段集**以 T-302 复核版 auth-integration.md 锚点为唯一基准**——本 ADR 定机制不定字段枚举，见「行为对齐条款」）
- 日期: 2026-08-26
- 背景: 用户指令 2026-08-26 11:35（认证配置前端化）→ FR-92：OAuth2(OIDC)/LDAP/SAML 三协议配置段 REST 读/写 + 测试连接 + **变更即生效**（写成功后 ≤1s 下一次认证走新配置，不重启）+ 控制台页组。现状（internal/config/api.go + internal/auth）：三协议配置是 binflow.yaml 静态段（`auth.oidc`/`auth.ldap`，ADR-0020），装配期一次性注入 `OIDCProvider`/`LDAPProvider`——oidc.go 明示「All fields are populated at construction time and remain immutable afterward」，改配置 = 改文件 + 重启；SAML 无配置面（auth-integration §3 低置信区，复核票补齐）。敏感字段（client_secret/bind_password）env-only、YAML 中出现即拒收（rejectSecrets）。约束：五臂（Basic / api-key / Bearer+OIDC / 裸 token / cookie）行为零变化（92.5，M6/M7 回归硬门槛）；19:05 行为对齐条款；Artifactory 侧行为基准 = 配置描述符中心（config-formats §2 高置信：7.x 配置存 DB 的 `configs` 表而非 config.xml，UI/REST 写入权威）+ `GET/POST /api/system/configuration` 段族（LC-13，A 级）；BinFlow 已有运行时可变状态的管理先例 = license Manager（ADR-0032：DB 单行描述符 + 验后替换 + atomic.Pointer 快照）。
- 候选方案:
  - 配置承载: A) 维持 YAML 静态段 + 文件热重载（fsnotify 监听）；B) **DB 配置描述符权威 + 文件段首启种子**（Artifactory config descriptor 行为模式）；C) 双源分字段合并（文件管连接参数、DB 管凭据之类）。
  - 生效机制: A) 每次认证现读 DB（无缓存）；B) **license Manager 同款原子快照**——写路径验后（strict 校验 + OIDC discovery 探测）→ 落库 → 重建 provider → atomic.Pointer 换帧，读路径无锁取帧；C) 重启/SIGHUP 重载。
  - 测试连接: A) 专用裸 dialer（探测是 admin 配置的、非用户输入——但配置面经 REST 可达后即为攻击者可达面）；B) **复用 M3 SSRF Guard 机器 + ADR-0025 决策 4 私网姿态**；C) 新增独立豁免配置键族。
- 决策: **承载 B + 生效 B + 测试 B**。要点七条：
  1. **REST 挂载（K30）**：认证配置管理面走 `dispatchAPI` 显式路由族（ADR-0034 先例——管理面 = handler + routeAuth，**非** apiProtocolMounts），全部位于 `/binflow/api/` 下；门 = GET 族 `CapSecurityRead`（readonly_admin 可见，92.4 只读态）、写族与测试连接 `CapSecurityWrite`；错误体 errors[] 信封。**路径字面量、段名拼写、信封形态以 T-302 复核票锚点为准**（LC-13 A 级面：Artifactory `GET/POST /api/system/configuration` 的 ldapSettings/oauthSettings/samlSettings 段族 + Access 配置面）；T-305 开工暂行形态 = 每协议段独立读/写端点 + 测试连接端点（BinFlow C 级面，机制条款不变）——若 T-302 锚定 Artifactory 整文档面（单端点全描述符读/写），A 级面以 additive 端点补齐、不改本 ADR 机制。启停 = 段内 enabled 字段走同一写路径（立即生效）；不设独立开关端点，锚点另示则从锚点。
  2. **DB 模型**：migration 015（双方言）新表 **`auth_configs(section TEXT PRIMARY KEY, doc TEXT NOT NULL, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL)`**；section 闭集 = 三协议段（BinFlow 内部名 `ldap`/`oidc`/`saml`，wire 段名随 T-302 锚点映射）；doc = 该段 JSON 文本，**写路径 strict schema 校验**（未知键拒收，与 config 包 decodeRaw 同姿态；校验器由 T-302 字段表生成）。整段原子替换（PutLicense 同语义）——不做字段级 merge。
  3. **变更即生效 = license Manager 原子快照模式复用**。可复用论证——license Manager 三要素与本场景逐点对应：①运行时状态 = 无锁读 immutable snapshot（`State` ↔ 重建后的 provider 组）；②写路径 = 验后替换（Install 先 VerifyDocument 再 PutLicense + 换帧 ↔ 配置写先 strict 校验 + OIDC discovery 探测成功才落库 + 重建 + 换帧，失败 400 且**现配置继续生效**——D7 同构）；③持久化 = 单行描述符 + 启动 Load 回放（boot 从 auth_configs 重建快照）。auth 侧新增 ConfigManager（暂名，归 T-305）：认证臂逐请求取当前快照（不缓存于长生命周期对象）；OIDC 登录流在单请求生命周期内持旧帧完成（跨帧切换语义 = 下一次请求走新帧，文档化）；LDAP provider 构造无网络副作用、重建即时。换帧后下一次认证即新配置——≤1s 门槛由「无重启、无 ticker」结构性满足。**五臂不变量**：本配置面只改配置来源与 provider 生命周期，臂序/判定/错误语义零变化。
  4. **双源优先级（K31，Q4 暂行转正）**：①DB 段行存在 → **DB 权威**，binflow.yaml 同段（含对应 env secret）被覆盖 → 每次启动 WARN（点名段名 + REST 迁移路径），不 fail-fast（升级实例不应被遗留段卡死）；②DB 段行不存在 + 文件段已配置（enabled 或任一非默认键）→ **首启种子**：落库（secret 从 env 取值加密入库）、INFO 日志，此后该段归 DB；③段粒度权威，无字段级合并；④种子后的段禁用/修改只能走 REST——删文件段不影响 DB 段（文件段已被覆盖就不再有运行期语义）；⑤**边界条款**：非 IdP 的 auth.* 键（argon2 参数、token TTL 族、step-up 族、hash_concurrency）维持文件/env 承载，不入 DB 描述符——运行时可变面收敛于三协议段，段边界以 T-302 复核为准。ADR-0020 机制本体（provider/臂/登录流）不推翻，其「配置面新增 auth.oidc/auth.ldap 段」的承载语义由本条升级为兼容窗种子。
  5. **敏感字段纪律**：client_secret / LDAP manager（bind）password / SAML 私钥材料——DB 内一律 `enc:v1:`（AES-256-GCM，ADR-0012 决策 4 的机器与密钥**复用**：`BINFLOW_REMOTE_CREDENTIALS_KEY`——replication 先例〔cmd replicationCipher〕，该 env 文档语义升格为「实例级静态 secret 主密钥」，名字历史沿用）。POSTURE 同 remote creds：无主密钥 + 无含 secret 段行 → 正常启动；无主密钥 + 写入含 secret 配置 → 写路径拒绝；存量段行含 secret 而无主密钥 → 启动 fail-fast。**回显脱敏**：GET 对敏感字段回**固定脱敏哨兵**（形态随 T-302——Artifactory 回显形态优先）；PUT 语义 = 字段缺省或回传哨兵原值 → 保持不变，新明文 → 替换（write-only 模式，AC2「明文 grep 零命中」由此成立）；明文不落日志、不进审计——`auth.config.update` detail = actor/段名/变更键摘要，值恒 redact（92.6/AC7）。
  6. **测试连接边界（K32）**：测试连接端点对候选配置（body 携带或已存段）执行一次出站探测——LDAP：manager bind + searchFilter 试搜；OIDC：discovery fetch；SAML：metadata URL fetch。**零新 SSRF 面**（NFR-S60）：探测 client 复用 M3 Guard 机器（scheme 白名单按协议〔ldap(s)/http(s)〕、blockedRanges 分类清单、连接期 Control 钩子重检 + DNS rebinding pinning、缓冲型响应 64MB 上限〔ADR-0012 勘误二 ③ 同款〕、短超时），私网姿态沿 ADR-0025 决策 4——**默认放行**（IdP/目录的现实位置就是内网；Teredo/畸形地址等非私网类别仍拒），**不新增豁免配置键**。错误文案可诊断（阶段 + 类别词）不含凭据、不回显目标 URL 的 userinfo；测试动作记审计 `auth.config.test`（actor/段名/结果类别）。
  7. **SAML 边界（Q3 维持）**：配置面字段集照 T-302 锚定的 samlSettings 落地（含证书材料），测试连接 = metadata 拉取校验；SP 断言消费登录运行时不在 FR-92 DoD——字段持久化、可校验、可审计，运行时消费位留缝（复核票若证 Artifactory SAML 行为可低成本对齐且用户要求，进 M11 须置换）。
- 理由: 承载 B 是 Artifactory 行为模式（描述符中心、UI/REST 写入权威）与 BinFlow 已验证机制（licenses 单行描述符）的交集；A 的文件热重载引入 fsnotify 依赖与「谁改了文件、何时生效」的第二事实源——恰是本 FR 要消灭的 SSH 改 YAML 心智；C 的分字段合并使两源都半权威，排障不可判定，被 ③ 段粒度规则替代。生效 B 把「变更即生效」从轮询/重载问题化为单指针换帧问题，license Manager 的验后替换吸收了 OIDC discovery 的网络副作用（探测失败 = 写失败，绝不半切换）；五臂只换配置来源使 M6/M7 回归面收敛为零。测试 B 是 NFR-S60 的字面执行：复用已审计的 Guard 机器零新面，私网默认放行镜像 replication 的现实姿态判定（两者目标拓扑同构：均为运维显式配置的内网端点）。
- 后果: migration 015 auth_configs（双方言，两方言同步义务沿 ADR-0007）；internal/auth 增 ConfigManager + 认证臂快照消费缝、metadata 增 auth_configs sub-store、httpapi security 族增三段读/写 + 测试连接路由（dispatchAPI 族，T-305 area：internal/auth + httpapi + metadata）；config 包 `auth.oidc`/`auth.ldap` 段进入兼容窗（K31 规则②③）；审计词 `auth.config.update`/`auth.config.test` 入 audit.Actions picker；`BINFLOW_REMOTE_CREDENTIALS_KEY` 文档升格实例级主密钥（tech-writer 同步，T-328）；启动日志增「认证配置来源（DB/文件种子）」一行（§6.4）；控制台页组（T-307）消费本面（admin 可写、readonly_admin 只读、敏感框 write-only）；M6 H 序列抽样 + M7 RBAC 矩阵零回归 = DoD 硬门槛（AC6）。**行为对齐条款**：字段枚举、段名拼写、脱敏哨兵形态、REST 路径字面量以 T-302 复核版锚点为唯一基准（效力序：用户裁决 > 复核版规格 > 本 ADR > PRD 暂行值）；机制条款（表模型/快照/双源规则/SSRF 边界/加密链）不随锚点翻转。
- clean-room 边界: 设计依据 = docs/reverse/auth-integration.md（§1 LdapSetting/SearchPattern/LdapGroupSetting 字段与两种认证模式、§2 OAuth addon 形态、§5 密码加密线索）+ T-302 复核增补的 Artifactory 行为出处；实现不引用反编译类结构（三段 Go 形态由复核票字段表生成，不逐字段翻译 Java 访问器栈）；密码/secret 存储加密对齐「存在主密钥加密机制」的行为事实（§5 低置信 → 复核确认），算法与格式自有（enc:v1 链）。

## ADR-0036: 独立存储配置文件 binstore.yaml——链式 provider 表达、并存三分支与 fail-fast（K33/Q5 终裁）

- 状态: Accepted（2026-08-26，M11 规划期 T-301 交付；链式 provider 语义与模板体系的行为出处以 T-303 复核版 config-formats.md §1 锚点为准——分歧按 19:05 上 BOARD，见「模板条款」）
- 日期: 2026-08-26
- 背景: 用户指令 2026-08-26 11:45（存储配置独立文件化）→ FR-93：对齐 Artifactory `$JFROG_HOME/var/etc/artifactory/binarystore.xml` 行为模式——存储链与主配置解耦，一份文件表达 filestore / S3 / dual-write 迁移链（远期 cache-fs、Azure/GS 留扩展位）。现状（internal/config/api.go StorageConfig）：`backend` 枚举 disk|s3 + `storage.s3` 段 + `storage.migration` 段（Enabled/Completed 二布尔编码 M6 三模式 bypass/dual-write/completed；storage/migration.go 证实迁移状态是**运维声明**——引擎从不回写配置文件，StartMigration 幂等重扫）；凭据纪律：secret_access_key env-only、YAML 出现即拒收（rejectSecrets 递归 storage.s3）。约束：存量 binflow.yaml 内嵌段实例零破坏（AC4 第一臂）；storage REST / 迁移端点 / 备份恢复行为零变化（93.6——迁移端点本就不写配置文件）；部署矩阵三形态同步 + CD 链数据目录零触碰（93.5，T-325）；格式自由（YAML——clean-room 只对齐行为：独立文件、链式多方案、provider 语义，PRD 93.1）；坏文件拒启须指明文件与行号（AC4）。
- 候选方案:
  - 文件定位: A) 固定 `$BINFLOW_HOME/binstore.yaml`（home 相对，忽略 -c）；B) **与已解析生效的主配置文件同目录的 `binstore.yaml`**；C) 独立 `-binstore` flag / env 指定路径。
  - schema 形态: A) 扁平键平移（backend/s3/migration 三段原样搬入新文件）；B) **有序 provider 链 + 链级 migration 模式**（链为一等公民）；C) Artifactory 模板速记体系全量对齐（`<chain template="...">` 的预设展开语义）。
  - 并存处置: A) 独立文件权威 + 内嵌段静默忽略（仅 WARN 不分情形）；B) **等价 WARN / 语义分歧 fail-fast 三分支**；C) 并存即 fail-fast（不分等价与否）。
- 决策: **定位 B + schema B + 并存 B**。要点七条：
  1. **文件与解析（K33）**：文件名 `binstore.yaml`，位置 = **与已解析生效的主配置文件同目录**（跟随 serve 的 -c 解析序：`./binflow.yaml` → `$BINFLOW_HOME/binflow.yaml`；默认形态下两者同一目录，即 `$BINFLOW_HOME/binstore.yaml`——对齐 binarystore.xml「与实例配置同处」的行为）；不新增 flag/env。文件**缺席 = 零行为变化**（内嵌段照旧，存量实例升级零感知）。
  2. **schema 骨架（canonical 形态 = 显式链）**：
     ```yaml
     version: 1
     chain:                      # 有序 provider 链，声明序即链序
       - type: filestore         # 本地盘 provider（M11 无参数；dir 覆盖键为扩展位不实现）
       - type: s3                # 参数 = 现有 storage.s3 键族平移（bucket/region/endpoint/
         ...                     #   access_key_id/use_path_style/upload_part_size/upload_concurrency/
                                 #   bucket_prefix；精确键名归 T-306 对照 T-303）
     migration:                  # 仅当 chain = [filestore, s3] 时合法且必填
       mode: dual-write          # 闭集 bypass | dual-write | completed（M6 三模式的一等拼写）
       concurrency: 5
     ```
     type 闭集 = `filestore` | `s3`；**保留名** `cache-fs`/`azure`/`gs`——出现即拒启动、文案点名「保留位未实现」（诚实拒绝：不静默忽略、不假装支持）。链形校验：filestore/s3 各至多一次；M11 合法链形 = `[filestore]` / `[s3]` / `[filestore, s3]`（其余序与重复拒启）；`migration` 块仅双员链合法且必填（无迁移意图的 filestore+s3 并存是 cache-fs 语义，未实现故不存在该形态）。
  3. **三链映射（AC2/AC3）**：`[filestore]` ≡ 现状 backend=disk；`[s3]` ≡ backend=s3（S3-only，含迁移完成后的终态等价形态）；`[filestore, s3] + mode`：`bypass` ≡ 仅 disk 生效、S3 参数前置声明（预配置位）；`dual-write` ≡ migration.enabled（双写 + 读 S3 先磁盘兜底 + 后台拷贝——M6 H 序列口径复跑）；`completed` ≡ enabled+completed（S3 单写）。**API 面不动（93.6）**：`/api/v1/storage/migration/start|status` 行为零变化；配置文件所有权维持运维声明模型（系统不改写 binstore.yaml，同 binarystore.xml 由运维/部署工具编辑的行为面）。
  4. **凭据纪律维持（93.4，NFR-S58）**：binstore.yaml **不接受任何明文 secret 键**——rejectSecrets 扫描对 binstore.yaml 全文件生效（含 s3 provider 子树），出现 `secret_access_key` 或 secret 形键 → 拒启动，错误指明文件 + 行 + env 逃生口（`BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY` 恒注入生效链的 S3 provider）；文件权限宽于 0600 → 启动 WARN（非拒启——文件内本无 secret，0600 为部署文档建议）。
  5. **并存与冲突（K33/Q5 暂行转正——三分支）**：以「链键」= 内嵌 `backend`/`s3`/`migration` 三组键（binstore.yaml 所能表达的全部内容；data_dir/session_ttl/gc_* 恒以 binflow.yaml 为准、不参与并存规则）为判定对象：①**无 binstore.yaml + 链键显式设置** → 照常起 + 每启 WARN（迁移提示——兼容窗，AC4 第一臂）；②**binstore.yaml 在 + 内嵌链键缺席或全默认** → 干净形态、无 WARN；③**binstore.yaml 在 + 内嵌链键语义等价**（归一化到决策 3 三链映射后链形与参数一致，非文本比较）→ 文件生效 + WARN（迁移提示）；**语义分歧**（会装配出不同链）→ **fail-fast 拒启动**，错误指明两处来源文件与分歧键。「独立文件优先」只吞并等价遗留、不吞并分歧——分歧 = 运维改错文件或双手编辑，静默择一会让实例在非预期后端上启动（数据可见性事故，场景 B 的反面）。
  6. **env 覆盖语义**：binstore.yaml 生效时，链相关 env 覆盖键（`BINFLOW_STORAGE__BACKEND` 及 storage.s3.*/migration.* 族）**忽略 + WARN**（部署管道遗留值不阻塞升级——三分支同理对升级温和）；secret env 恒生效。
  7. **坏文件 fail-fast 指位（AC4）**：YAML 语法错 / 未知键 / 未知或保留 type / 非法链形 / migration 块误配 / 明文 secret → 一律拒启动；错误信息含 binstore.yaml 绝对路径 + 行号（strict decode 的 yaml.Node 定位，decodeRaw 同机制）+ 违规键名。启动成功打一行「存储链形态」INFO（provider 链摘要、凭据 redact，§6.4）；加载失败腿 WARN + 审计 `storage.config.load`。
  - **模板条款（template 速记不进 M11）**：canonical 显式链已覆盖三链全部表达力，模板层在恰有三种链的阶段是零收益词汇；Artifactory 模板体系（模板展开的隐含 provider/默认参数/组合规则）行为等价性以 T-303 复核结论为准——若模板语义存在可观测差异，按 19:05 上 BOARD 裁决后再以 additive 速记层补齐（不推翻本 ADR 的 canonical 形态）。
- 理由: 定位 B 让 -c 显式部署与默认部署同一心智（「binstore.yaml 就在 binflow.yaml 旁边」），C 为尚不存在的多文件部署形态加旋钮；schema B 把「链」表达为一等公民——dual-write 本就是链语义而非单选枚举，扁平平移（A）表达不出扩展位、全量模板（C）在三种链阶段为想象买单（PRD 93.2 已预留按需裁剪 + 分歧上 BOARD）；并存 B 把 Q5 暂行的两句（优先+WARN / 冲突 fail-fast）精确化为可断言三分支——等价/分歧分界使兼容窗温和（遗留段不卡升级）且安全（真分歧不静默择路），C 的无条件 fail-fast 会把「搬完文件忘删旧段」的等价遗留全部变成升级阻塞。
- 后果: internal/config 增 binstore 加载器（strict decode + 链形校验 + 三分支并存裁决 + secret 扫描）、cmd 装配改为消费归一化链形（T-306 area：internal/config + storage 装配）；deploy/charts 三形态存储配置示例同步 + CD 链 VM 切换验证（T-325，数据目录零触碰）；L08~L10 断言锚（三链 roundtrip / 双写降级 / 三分支并存 + 坏文件拒启）；M6 S3/迁移 + M1 存储 + M10 MPU/smart-remote 回归零变化（AC6）；存量实例升级路径 = 什么都不做（分支①）或建等价 binstore.yaml（分支③）。
- clean-room 边界: 行为依据 = docs/reverse/config-formats.md §1（文件位置/chain 模板/provider 可配置点，高置信）+ T-303 复核增补出处；**格式自有**（YAML 载体，PRD 93.1 明示 clean-room 只对齐行为）——不复制 binarystore.xml 的 XML 载体与模板展开器结构；provider 语义以复核票锚点校准（模板条款即为此预留的分歧门）。

<!-- ADR-0037 编号预留给复制硬化域（FR-101 / K37：contentSynchronisation 子字段集与 replica 隔离终裁执行——触点 T-317，票内落笔），本文档跳过该号不预写。 -->

## ADR-0038: GPG keypair 体系——CRUD/密封存储、repoKey 关联、openpgp 库选型 ProtonMail/go-crypto（K36）

- 状态: Accepted（2026-08-26，M11 规划期 T-301 交付，Q6 用户已裁进 M11；CRUD wire 字面量与 Artifactory 行为出处以 T-319 票内 mini 规格锚点为准——本 ADR 定机制与选型不定端点字面量）
- 日期: 2026-08-26
- 背景: FR-97/FR-98 签名腿（debian Release/InRelease、rpm repomd.asc）的前置 K-1（BOARD B10 T-319：票内先补 mini 规格）。需求：GPG keypair 生成/导入/查询/删除、口令保护私钥、按 repoKey 关联（debian/rpm 共用池；helm 不含——HL-4 已裁 `.prov` 走普通文件存储不经 keypair）。约束：ADR-0005 零 CGo 硬门（Makefile 全构建 `CGO_ENABLED=0` + cgo_deps 审计门；六平台 goreleaser；e2e/CD 链纯 Go 构建）；`golang.org/x/crypto/openpgp` **已弃用**（Go proposal #44226，2021 冻结——官方文档自述「unmaintained except for security fixes; unsafe by design」，缺 Ed25519/ECC、v5/v6、AEAD 等现代能力）；实例静态 secret 纪律已有先例（`enc:v1:` AES-256-GCM 链，remote〔ADR-0012 决策 4〕与 replication 共用 `BINFLOW_REMOTE_CREDENTIALS_KEY`）。
- 候选方案:
  - openpgp 库: A) `github.com/ProtonMail/go-crypto`（x/crypto/openpgp 的活跃继任 fork，API 兼容、import 直换）；B) `github.com/keybase/go-crypto`（早期流行 fork）；C) 维持 `golang.org/x/crypto/openpgp`（x/crypto 已在 go.mod，零新依赖）；D) 自实现 OpenPGP 消息子集（Clearsign/detach armor + RSA 签名）。
  - 私钥存储: A) 明文列、仅依赖 keypair 自带口令 S2K；B) **enc:v1 整封**（armored 私钥块与口令两列均以实例主密钥 AES-256-GCM 密封）；C) 口令不存、签名时运维重输（无人值守运行时，不可行）。
  - repoKey 关联: A) keypair 行内嵌 repoKey 列表（keypair 拥有仓）；B) **repo 配置正向引用 keypair id**（仓声明签名意图）。
- 决策: **库 A + 存储 B + 关联 B**。要点六条：
  1. **选型 = `github.com/ProtonMail/go-crypto`（现行 v1.4.1，2026-03-18 发布），准入 ADR-0005 白名单（M11 准入段）**。验证证据（2026-08-26 实测，goproxy.cn 镜像——proxy.golang.org 在本环境超时，ADR-0005 既知）：`go get`/`go list -m` 解析 v1.4.1 OK；`CGO_ENABLED=0` 构建全过（darwin/arm64、linux/amd64、windows/amd64 三平台 trivial-import 探针）；传递依赖 = `github.com/cloudflare/circl` v1.6.2（纯 Go，Ed448/X448/Curve25519 互操作）+ `golang.org/x/crypto`/`x/sys`（已在树）——净新增两模块。淘汰理由：**keybase/go-crypto** `@latest` = 伪版本 **2020-01-23**、六年余零 tag 零演进（生态迁出求证在案：terraform-provider-aws#27214、go-git、Helm 迁移轨迹均指向 ProtonMail fork）；**x/crypto/openpgp** 官方弃用标记 + 现代算法缺口（本库需处理的导入件可能含 Ed25519/ECC 密钥）；**D 自实现** OpenPGP 消息格式（S2K/压缩/armor/canonical text mode）的正确性面远超收益，安全件不自写。
  2. **存储模型**：新表 **`gpg_keypairs(keypair_id TEXT PRIMARY KEY, public_key TEXT NOT NULL, private_key_enc TEXT NOT NULL, passphrase_enc TEXT NOT NULL, algorithm TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL)`**（migration 015+，与 auth_configs 各自编号、先到先得，归实现票；双方言）。public_key = armored 公钥明文（公开物）；private_key_enc = **导入/生成原样的 armored 私钥块**（内部口令 S2K 保护保留）再整封 `enc:v1:`；passphrase_enc = 口令 `enc:v1:`。双列皆封的理由：S2K 口令不是静态密封的等价物——弱口令可被离线爆破，DB 泄露场景下实例主密钥是独立第二道防线（与 NFR-S57/S58 同向；ADR-0012「永不存明文」纪律的彻底执行）。POSTURE 沿实例静态 secret 族：无主密钥 → keypair 写路径拒绝；存量行存在而无主密钥 → 启动 fail-fast。
  3. **CRUD 面**：生成（服务端 keygen）与导入双入口；GET 回显公钥 + 元数据（keypair_id/algorithm/时间/被引用仓清单），**私钥与口令永不出库**（无导出端点——导出诉求出现走新裁决，边界留痕）；DELETE 护栏：被仓引用中的 keypair → 400（文案点名引用仓清单）。REST 路径/信封字面量与 Artifactory 行为出处（生成默认参数、回显形态）以 **T-319 mini 规格**锚定；门 = CapSecurityWrite（GET 族 CapSecurityRead）；审计 `keypair.create|delete`（detail 不含任何密钥材料）。
  4. **签名时序**：取行 → 主密钥解封 → openpgp open（口令）→ 签名 → 即弃；密钥材料零日志、零响应体。openpgp 库承担 keypair 解析与 OpenPGP 消息签名：debian Release.gpg（detached armor）/ InRelease（clearsign）、rpm repomd.asc（detached armor）。**M11 范围 = 仓库元数据签名**；RPM 包体头部签名（若远期需要，RPM 自有格式非 OpenPGP 消息）从同一私钥提取 RSA 材料另走格式实现——不进本 ADR。
  5. **repoKey 关联（K36）**：repo 配置增 keypair 引用字段（拼写随 T-319 mini 规格与 FR-97/98 票面）；**local debian/rpm 仓接受**；建/改仓校验引用存在（400）；一 keypair 多仓共用（共享池，含跨 debian/rpm 同钥）；helm 包型不接受该字段（未知键 400——HL-4）；virtual/remote 不接受（签名是 local 写路径行为）。未配 keypair 的 deb/rpm 仓维持 unsigned 行为零变化（签名 opt-in——AC4/AC7 的 unsigned 默认验收不受影响）。
  6. **生成默认（暂行待锚定，19:05 口径照 Artifactory 实际值）**：RSA-4096（apt/dnf 旧客户端最大兼容）、不含过期、主钥 + 签名子钥标准结构；导入接受 Ed25519/ECC 密钥（ProtonMail 库原生支持）。T-319 mini 规格若给出 Artifactory 生成默认的出处且与此不同 → 按 Q8 先例照 Artifactory 翻转并留痕（本 ADR 机制条款不变）。
- 理由: 库 A 是唯一同时满足「活跃维护 + 零 CGO + API 兼容 + 现代算法」的选项（x/crypto/openpgp 弃用文档与社区迁移轨迹共同指向它），三平台探针实证零 CGo；存储 B 以零新机制成本（同一 Cipher）把 DB 泄露的爆破面从 S2K 口令强度叠加到主密钥保管，且「导入原样保存」免二次编码错误面；关联 B 让所有权单向（仓声明签名意图），建仓校验与删除护栏天然可查——A 的反向列表在多仓共用下是双向维护的漂移源。
- 后果: go.mod 增 `github.com/ProtonMail/go-crypto` + `github.com/cloudflare/circl`（ADR-0005 M11 准入段留痕）；gpg_keypairs 迁移（双方言）；T-319 实现（CRUD + keygen/import + 关联校验 + mini 规格锚定 wire）、T-321/T-322 消费（debian/rpm 签名腿——apt 无 `[trusted=yes]`、dnf repo_gpgcheck=1 验收）；二进制体积增量预计 <1.5MB（净增两纯 Go 模块；check-size 实测归 T-319）；unsigned 回归零变化。
- clean-room 边界: 本 ADR 不依赖 reverse-src 取证——OpenPGP 消息格式 = RFC 4880 系公开规范，以规范为准（ADR-0001 既有红线）；Artifactory keypair 管理面的行为出处（端点族/字段/生成默认）由 T-319 票内 mini 规格补齐后锚定，锚定前不逐字承诺 wire 形态；机制条款（存储/密封/关联/护栏/选型）不随锚点翻转。

## ADR-0039: MPU REST 面整体翻转对齐 Artifactory——六端点 POST+QueryParam、token 能力凭据、complete?sha1= 202 + 异步任务模型（裁决②）

- 状态: Accepted（2026-08-28，M11 T-332 交付；用户 2026-08-28 07:5x 裁决②整体翻转——T-304 §4.2「MPU 面形状束」待裁项的终裁）
- 日期: 2026-08-28
- 背景: T-289（M10）以自有形状实装 `/api/v1/uploads` 六端点（JSON 体 create/config、GET urlPart/status 带路径 id、complete JSON sha256 门 201、status=会话字节进度）。T-304 §1.2/§4.2 逐条复核锚定 Artifactory 实况（`rest/resource/mpu/MultipartUploadResource.java` + `mpu/MultipartUploadServiceImpl.java` + 响应 record 四类 + `ConstantValues.java:1517-1520`）：**面级可观测差异**（动词/参数位置/状态码/校验和算法/进度模型/token 模型）。用户裁决②：整体翻转。连带 T-304 §1.2-B（create 范围）同裁改回：无包型校验 + virtual defaultDeploymentRepo 回落。
- 决策: 六端点 wire 形状照 Artifactory 翻转，**引擎缝（T-323R caller-blob 持久化/懒重建、Session Append/Commit/Abort 链、temp+fsync+rename 落盘）原样复用**——翻的是 wire，不是存储内核。形状表（出处：Resource 类逐行，高置信）：

  | 端点 | 翻转后形状 | 响应 |
  |---|---|---|
  | create | `POST ?repoKey=&repoPath=&partSizeMB=`（QueryParam，非 JSON 体；`@QueryParam` 三参 + `assertRepoKeyAndPathNotNull`） | **200** `{"token": "..."}`（`CreateResponse(token)` record） |
  | config | `GET /config`（能力探测；`isFeatureEnabled()` + jfrog-cli UA 版本门 `mpuJfrogCliMinRequiredVersion="2.62.2"`，低于则 supported:false；版本段解析失败按 catch→不低） | 200 `{"supported": bool}`（`ConfigResponse`） |
  | urlPart | `POST ?partNumber=N`（Bearer 会话 token；无路径 id） | 200 `{"url": "..."}`（`UrlResponse`） |
  | complete | `POST ?sha1=`（**sha1 非 sha256**——`@QueryParam("sha1")` + `assertSha1NotNull`） | **202 Accepted 空体**（`Response.accepted()`），落点异步 |
  | status | `POST`（Bearer；无路径 id/无清单形态） | 200 `{status, error, progress, checksumToken}`（`StatusResponse` 四键 record；Finished 臂带 checksumToken，异常臂带 error） |
  | abort | `POST`（Bearer） | 204 空体 |
  | part（urlPart 目标，非六端点之一） | `PUT /part/{id}/{n}` 维持（BinFlow 中继 URL，T-289-A 裁定不翻直传） | 202 会话回显（BinFlow 自有 URL 契约） |

  实现映射五条（Artifactory 语义 → BinFlow 载体）：
  1. **token 模型**：Artifactory = Access JWT（extension 内嵌 repoKey/repoPath/uploadId/tempPathKey，scope `internal:mpu:x`）。BinFlow 不改 internal/auth（area 纪律）→ **自解析能力 token = `<32B 随机 hex>.<会话 id>`**；其 sha256(随机段) + 2 天过期（`mpuTokenExpirySecs`）持久化进 T-323R caller blob（v2 字段），重启后凭 blob 复验——重启续传在新 wire 下保留。urlPart/complete/status/abort 四端点 + part PUT 走 dispatch 豁免车道（isLoginEntryPoint 同型谓词）：中间件验不过的 Bearer 由 handler 自裁——MPU 形状 → 能力路径（未知/失配 404，对齐 Artifactory `NotFoundException` 姿态）；非 MPU 形状 → 照中间件原判 401（presented-but-rejected 姿态不弱化）。
  2. **complete 异步任务模型**：202 即受理；后台 goroutine 跑 `Session.Commit(BlobRef{Sha1})`（引擎同步缝原样）→ 任务态机 **PARTS→PROCESSING→FINISHED(progress 100, 铸 checksum-deploy token)/NON_RETRYABLE_ERROR(error)**（词表 = jfrog-client-go 公开 completionStatus，真客户端验证锚定）。FINISHED 前经 `metadata.Blobs().Put`（既有导出缝，契约即「caller commits the physical blob first」）落 **blobs ledger 行**——Artifactory binary store 校验和索引的等价物；**节点（node 行）仍由客户端 checksum-deploy 落**（Artifactory 语义：complete 只组装+登记 blob，节点由客户端凭 checksumToken 零传输落库，治理门〔pattern/quota/覆盖对〕随该次 PUT 走——BinFlow generic 面 `X-Checksum-Deploy`/`PutFromBlob` 已备）；客户端不落 → 无引用 blob 归 GC 两阶段回收（ADR-0031 模型内）。FINISHED 铸 5 分钟（`mpuChecksumDeployTokenExpirySecs`）token——经 auth.Service `Issue` facet 铸**真 API token**（中间件可验）。错配 sha1 不再 complete 同步 409——任务态 NON_RETRYABLE_ERROR 经 status 可观测（S3 侧回收靠引擎 fail-locked 合同 + sweep）。ledger 直写而非新增 repo.Service 方法：避免跨 area 改 repo（并行票在邻区），缝契约吻合；若 architect 偏好收口为 `repo.Service.RecordCommittedBlob` 留后续票。
  3. **create 范围放开**（T-304 §1.2-B 改回）：无包型/无 layout 校验——generic 之外 local 仓（docker/maven/…）不再 400；repoKey 缺省语义 = **repoKey 指 virtual 时回落 defaultDeploymentRepo**（`getRepoPath` 行为；未配 default 的 virtual 保持原 key，写门照过、落地时 routeVirtualWrite 405——对齐 Artifactory 的迟到失败）。unknown repo / remote / 无 `w` → **403**「The user is not allowed to deploy to this location」（`ForbiddenException` 原文；改 BinFlow 旧 404 形态）；角色门 = admin/user（`@RolesAllowed`；readonly_admin 403）。
  4. **sha1 语义**：complete 校验和算法从 sha256 翻为 sha1（40 hex 必填；畸形 400「Query param sha1 is null」原文对缺失）；sha256/sha1/md5 三元组仍由引擎流式计算落账（BlobRef 全量），只有**门**换算法。
  5. **旧形状退役**：JSON 体 create/config 重分片、GET urlPart/status（含清单形态）、路径参数四端点、complete 201/sha256 同步门——全部下线（落到 E-26 404/400，无兼容垫片）；caller blob v1（无 token 字段）行 fail-closed 404——升级窗口内在途旧会话死亡重传（可接受：MPU 会话 TTL 24h，升级本就要求清空在途上传）。config 从「重分片」翻为「探测」（T-304 §1.2-D 已裁）。
- 理由: 裁决②的直接执行；六端点形状/状态码/token 模型在 reverse-src 逐行高置信锚定（record 字段名/QueryParam 名/202/204 全部取自类文件）；引擎缝零改动使 T-323R 重启续传、B1-B5 评审成果（锁序/溢出门/torn-part/S3 回收）在翻转后全部存活；节点落地移到客户端 checksum-deploy 是 Artifactory 原义（checksumToken 的存在本身即证据），且让 MPU 与 GC/引用计数模型正交。
- 后果: `internal/httpapi/uploads.go` 重写 wire 面 + `router.go` 路由表/豁免谓词；探针 `scripts/m10-mpu-probe.sh` 与 `docs/user/api-reference.md`、`docs/user/faq.md` MPU 节随翻；测试三文件重写。**真客户端验证（2026-08-28，jfrog-cli 2.122.0 + MinIO 真栈，220MiB 全链字节一致）修形三处**：① status 任务态词表 = jfrog-client-go 公开源码的 completionStatus 集——**PARTS/PROCESSING/FINISHED/NON_RETRYABLE_ERROR**（客户端解析表：QUEUED/PROCESSING 续轮询、FINISHED/ABORTED 终态、NON_RETRYABLE_ERROR 致命、其余一律 unexpected→abort；QUEUED/RETRYABLE_ERROR/ABORTED 本面不发）；② part PUT 是 **S3 PutObject 形**：真客户端把 urlPart 答案当 presigned 用——**PUT 不带任何 Authorization**（能力必须随 URL 查询串 `?token=` 传递；访问日志只记 path 不记 query，能力不落日志）且**只认 200**（202 也被当失败重试）；③ 分片**并发乱序到达**（--split-count 默认 5 工作池）→ 面内有界重排暂存（64 片/128MiB 预算，越界诚实 409；顺序快路仍直流引擎，内存与制品大小无关）。另：客户端 MPU 触发前置门 = `/api/system/version` ≥ **7.82.2**（客户端自查，与 config 探测叠加）。**已知残余**：① checksum-deploy token 为全用户 API token（5 分钟）非 internal:checksum-deploy:w 窄域——窄域需 internal/auth 扩展缝（后续票候选）；真客户端的 deploy PUT 同时带原凭据 + `X-Checksum-Deploy-Token` 头（服务端忽略未知头，流程不受影响）；② filestore 上 config 探测回 200 {supported:false}（探测端点的本义），其余五端点维持诚实 501（FR-90-AC3 不变——与 Artifactory 404 的分歧为既有裁定，保留）；③ complete→FINISHED 窗口 kill -9：任务态仅进程内，重启后 status 回 PARTS（行未消费），补救 = 客户端重发 complete（parts 持久、Commit 幂等去重）；④ jfrog-cli UA 版本门常量 2.62.2 照 `ConstantValues` 默认，未做可配置；⑤ 短片段后仍有在途已暂存片 → 会话判失败（S3 的 manifest 完整性声明 vs 本面流式 EOF 的残余语义差，真客户端不会触发）。
- clean-room 边界: 行为依据 = reverse-src 反编译类（Resource/ServiceImpl/四 record/ConstantValues）的**行为规格**（动词/参数名/状态码/字段名/常量值）——非代码翻译；`org.jfrog.storage` 外部库部分（任务枚举显示名）明确低置信登记；jfrog-cli-go 客户端侧行为以官方开源为准（待真机验证）。

## ADR-0040: dual-write S3 停机 fail-open——故障窗本地优先写 + 磁盘重试队列 + 读回退排空对账（裁决③ / T-327 D-A）

- 状态: Accepted（2026-08-28，M12 B0 T-333 交付；用户裁决③〔2026-08-28 07:5x〕+ T-327 D-A 登记的终裁落地；实现票 T-338〔FR-107，dep 本 ADR〕）
- 日期: 2026-08-28
- 背景: T-327 中期回归 L09 实测并登记 **D-A**：dual-write 装配下 S3 停机窗内 GET 存量件 500（`MigrationEngine.Open` 仅 `ErrBlobNotFound` 才落 disk，migration.go:247）、PUT 500 且 disk 零落盘（`BeginSession` S3 臂失败连带 abort disk 臂，migration.go:213-217）——与 M6 PRD FR-50 v1.0 文面「本地成功后异步写 S3（**S3 写失败不阻塞请求**，仅记录错误日志 + 重试队列）」冲突；归因 = M6 存量姿态经 binstore.yaml 等价映射透出（ADR-0036 决策 3），M6 H12~H15 验收从未测过停机窗。代码复核（T-333）发现**第三处更狠的 fail-closed**：`migrationSession.Commit` S3 臂失败会**反删已落盘的 disk blob**「维持一致性」（migration.go:811-815）——PUT 500 且已传数据被销毁，客户端被迫全量重传。用户裁决③：M12 补实现（故障窗内本地优先写 + 异步 S3 重试队列），M6 PRD 文面维持（FR-107.5——实现债收口，非范围变更）。约束：M6 迁移状态机 / M11 binstore.yaml 三链与 schema（ADR-0036）/ migration status 端点形状（93.6）零回归；单实例部署前提（§9）；分层纪律 storage 不依赖 metadata/audit。
- 候选方案:
  - 写路径形态: A) 全量异步化——每次 PUT 本地优先落盘 + 队列排水，S3 永远出请求关键路径（PRD 文面字面顺序）；B) **故障窗 fail-open——稳态维持同步双写，S3 臂任一失败开断路器窗，窗内 disk-only + 入队，恢复后排空对账**（裁决③「故障窗内」限定）；C) PRD 勘误收口 fail-closed（T-327 处置建议另一臂——用户已否）。
  - 队列载体: A) 进程内存队列；B) **磁盘目录 per-sha 文件（`<data>/replay-queue/<sha256>.json`，temp+rename 原子入队）**；C) 元数据库表（migration 016）。
  - 读回退判定: A) 维持仅 `ErrBlobNotFound` 回退；B) S3 错误分类学（网络类回退 / 语义类诚实失败）；C) **任何 S3 错误皆回退 disk 探测 + 可观测兜底**。
- 决策: **写 B + 队列 B + 读 C**。要点八条：
  1. **故障窗判定 = 断路器，不做错误分类学**。进程内三态：closed（稳态，双写照旧）/ open（故障窗）/ half-open（探测）。开窗唯一触发 = S3 臂任一操作失败（Begin/Append/Commit/Open——连接拒绝、超时、5xx、权限错，一律同视：S3 兼容生态〔MinIO/Ceph/R2〕的错误分类不可靠，「成功/不成功」二分是唯一稳定判据）。窗内 `BeginSession` 走 disk-only 快路（不再拨 S3——免 per-PUT 超时惩罚）、`Open`/`Stat` 直读 disk（超集，见要点 4）。half-open 探测 = 排空循环的首个真实拷贝；成功即关窗（audit `storage.replay.window{phase:close}`）。断路器是进程内状态（单实例前提，ADR-0031 serve.lock 同款立场）。**零新配置键**：binstore.yaml schema 冻结（ADR-0036），退避/上限为引擎内部常量，排空 worker 复用 `migration.concurrency`。
  2. **写路径 fail-open 状态机（三点降级，单缝覆盖全部写路径）**。会话生命周期内 S3 臂任一时刻失败（Begin 建臂 / Append 中途 / Commit 时）→ abort 该会话的 S3 臂（其 temp 数据弃置）、会话降级为 disk-only 继续；Commit = disk commit（ADR-0006 落盘协议不动）+ 入队 + 返回成功。**migration.go:813-815 的反删盘条款废止**——disk 已提交的 blob 永不因 S3 失败回滚（三处 fail-closed 全消除）。缝在 `MigrationEngine`/`migrationSession` 单点：普通上传、MPU 分片（ADR-0039 引擎缝）、remote pull-through 落盘全部免费获得。**fail-open 的地板 = disk**：disk Commit 失败（磁盘满等）仍诚实 500——只剩一个后端时没有降级余地。入队幂等：条目名 = sha256，重复 PUT 同内容不增深度（与 checksum 去重同源）。
  3. **读路径回退（site-2 消除）**：`Open`/`Stat` 的回退条件从 `errors.Is(err, ErrBlobNotFound)` 扩为「S3 任何错误」→ disk 探测；`Stat` 与 `Open` 镜像同改（checksum-deploy / `X-Checksum-Deploy` 路径依赖 Stat）。每次回退命中计 `binflow_storage_read_fallback_total` + 每窗首个错误 WARN（错误文本含 S3 侧错误链——权限错/桶名错不断服但可见）。窗内（断路器 open）直读 disk 免拨 S3。
  4. **一致性窗口声明（双写分歧的可见语义）**：窗内新 blob 的唯一副本在 disk；全部读路径经 S3-first + disk 回退仍 200——**读己之写保持**。S3 侧滞后量 = 窗口时长 + 排空时长；直接读 S3 桶的带外消费者会看到旧集合——**S3 桶是引擎私有物**（Artifactory eventual 排水期同姿态），带外读不在契约内。回退完整性论证：dual-write 迁移方向 disk→S3 且 disk 副本不自动删（FR-50：迁移完成后运维手动删）→ **disk 恒为超集**，回退无洞。分歧关闭点 = 排空完成 + 对账零缺（audit `storage.replay.drained` 为界）。
  5. **重试队列形态**：`<data>/replay-queue/<sha256>.json`，内容 `{version, sha256, enqueued_at}`（version 字段 + 演进规则，ADR-0006 state.json 同款姿态）；入队 = temp+fsync+rename 原子。attempts/退避为进程内状态，重启重置（无害——排空幂等）：指数退避 1s 起步 ×2 上限 5min，任一拷贝成功即重置。**无死信删除**：条目只在「确认 S3 已有」或「源 blob 消失」时移除；连续 16 轮全失败 → permanent-failed 记账（WARN + 指标），条目保留等 S3 侧修复后重试。队列目录是引擎私有**派生**状态，**不进版本兼容承诺**（ADR-0006 布局家族的瞬态面同款）——最坏情况（队列丢失/损坏）`StartMigration` 幂等重扫即可重建 diff，持久化是排空追赶的优化不是正确性前提。
  6. **排空与对账（恢复后追赶）**：关窗触发立即排空（worker = `migration.concurrency`，默认 5）；排空复用 `copyBlob`（SkipIfExists 幂等 + Commit 带 expect sha256 流式校验——内容完整性由拷贝时校验承载）。排空完成 → **对账 = 存在性 diff**（`diskList` × `s3Set`，既有盘点路径）：missing > 0 → 重入队收敛环（≤3 轮）；仍缺 → permanent WARN + 指标，人锤 = `POST /api/v1/storage/migration/start` 幂等重扫。与后台迁移扫描天然双通道幂等收敛（两者都是「S3 没有才拷」）。**migration status 端点形状逐字节不变**（93.6——队列状态走指标与日志，不进 FR-50 v1.1 信封）。
  7. **治理交互与模式边界**。GC（ADR-0031）：**队列不是引用、不是 hold**——hold 语义零变化；排空时源 blob 已被 GC 回收 → drop 条目记 `skipped_source_gone`（GC 只回收无引用 blob，无引用 blob 的 S3 副本本就不需要——dual-write Delete 本就双删）；队列积压不 pin 垃圾。窗口期 GC/Delete 维持诚实失败（GCSweep 双侧合并错误、Delete 非 NotFound 错误上抛）——**fail-open 只覆盖数据面 PUT/GET，不覆盖治理面**（管理面可重试，静默吞错反而掩盖 S3 故障）。模式边界：断路器/队列是 **dual-write 装配独有器官**——bypass（S3 不在链上）与 completed（disk 可已被运维删除，无回退余地）零新代码路径、行为零变化；**装配为 completed 且 replay-queue 非空 → 启动 fail-fast**（错误指明：先切回 dual-write 排空，或人工确认队列可弃再清目录——「声明迁移完成但数据没到」正是 ADR-0036 场景 B 反面的数据可见性事故；该 gate 只检查引擎自有队列状态，不引入 completed 全量验证义务）。
  8. **可观测面**（指标族 = ADR-0022 expvar 自研先例；审计词表 = `gc.run`/`cleanup.run` 的 run-history 姿态，系统事件入审计有先例〔internal/repo/cleanup.go 经 audit append facet〕）：指标六枚——`binflow_storage_replay_queue_depth`（gauge）、`binflow_storage_replay_drained_total`、`binflow_storage_replay_failed_total{outcome=retry|permanent|source_gone}`、`binflow_storage_read_fallback_total`（counter）、`binflow_storage_s3_window_open`（gauge 0/1）；审计两事件——`storage.replay.window`（detail: phase=open|close + first_error）、`storage.replay.drained`（detail: drained/skipped_source_gone/permanent_failed/reconcile_missing/rounds）；**发射点在装配层 facet**（engine 以事件回调暴露事实，internal/storage 不 import audit——分层纪律）；启动 INFO 一行队列水位（§6.4 启动行家族，>0 附排空预告）。
- 理由: 写 B 是裁决③的字面执行（「故障窗内」限定本地优先）且是回归面的结构性最小化——稳态同步双写一行不动，H13「上传后本地+S3 均有 blob」保持同步真值、AC3/AC5 的风险收敛为零；A 把稳态也翻成最终一致，为容错重写正确性模型不成比例，且 FR-50 文面的承诺语义本就是「S3 写失败不阻塞」而非「永远异步」——B 恰好兑现承诺而非字面顺序。队列 B 三重论证：存储自包含（ADR-0006 勘误①「storage 不读 DB」既有立场——C 破坏分层）、per-sha 文件名天然去重（checksum 寻址同源）、派生可重建（丢了 StartMigration 重扫即恢复）；A 直接违反 FR-107-AC2 重启幸存。读 C：分类学（B）在多 S3 实现间不可靠且两端都可能错（权限错被当 not-found 静默吞 / 网络错被当致命打断读），二分 + 可观测兜底唯一稳定；disk 超集论证使「任何错误回退」无正确性风险。
- 后果: internal/storage 单 area（MigrationEngine 断路器 + 会话三点降级 + replay 队列与排空循环 + 完成对账；T-338）；无 schema / 无配置面 / 无 API 面变化（部署矩阵三形态零触碰）；M6 PRD FR-50 文面自 M12 起有实现对应（107.5）；L09 断言语义翻转（停机窗 500 → 200 + 排空追赶）进 e2e 常备；migration.go:813-815 反删盘行为废止（对齐表登记行为差异：Artifactory eventual 排水**成功后删源**，BinFlow 永不自动删 disk 副本——FR-50 运维边界维持）；docs/user storage/迁移 FAQ 停机窗姿态由 tech-writer 票同步。
- **T-338 验收锚点骨架**（FR-107 AC1~AC5 的 ADR 展开，真栈 MinIO）：
  - AC-A1 停机窗 PUT（D-A site-1）：MinIO stop → PUT 3 制品全 200；disk 落盘可证（`blobs/<2hex>/<sha>` 布局）；queue_depth +3；同 sha 重复 PUT 深度不增（幂等入队）。
  - AC-A2 停机窗 GET 存量（D-A site-2）：三类件（窗口前仅 S3 可达件靠 disk 超集回退 / 窗口前双写件 / 窗口内新件）GET 全 200；`read_fallback_total` 增长；Stat 路径（`X-Checksum-Deploy`）同测。
  - AC-A3 中途死亡降级（D-A site-3，ADR 增补）：上传中途停 MinIO（Append/Commit 臂失败）→ PUT 仍 200 且 disk blob 保留（反删盘废止的直接断言）。
  - AC-B 队列持久化：窗口内重启 → 队列幸存 → 启动水位 INFO 行 → 恢复排空对账。
  - AC-C 排空对账：MinIO start → `window{close}` + drain → mc 侧逐对象 sha256 零缺；`storage.replay.drained` 审计在案；depth 归零。
  - AC-D 治理交互：窗口期「PUT → 删 node → gc apply」→ 条目 source_gone drop、排空不卡死；ADR-0031 压力回归（并行上传 × grace=0 apply）不回归。
  - AC-E 模式边界：M6 H12~H15 复跑绿；completed+队列非空拒启断言；filestore/s3 单员链行为零变化。
  - AC-F 可观测：指标六枚存在且语义正确 + 审计两事件 + 启动水位行。
  - AC-G 回归：binstore.yaml 三链 roundtrip（M11 L08/L09 口径）零回归；migration status 端点形状逐字节不变。
- clean-room 边界: 行为对齐锚点 = docs/reverse/config-formats.md §1.2（`s3-storage-v3` 模板展开链 `cache-fs( eventual( retry( s3-storage-v3 ) ) )`——**eventual**〔本地优先 + 异步排水，校验成功才动源〕与 **retry**〔失败重试〕两 provider 正是「故障窗不断服 + 恢复后追赶」的链语义原型）+ §1.3 C4（双写迁移态由 eventual + `_add` 排水机制表达，非 dual provider）；对齐的是**容错行为模式**（本地优先写、异步追赶、排空校验），不复制 provider 嵌套结构/模板展开器——BinFlow 的稳态同步双写是自有选择（M6 H 序列验证面），本 ADR 补的是链内 eventual/retry 容错段的等价行为；差异登记：Artifactory eventual 排水后删 NFS 源，BinFlow disk 副本由运维管理（FR-50 边界维持，见「后果」对齐表条目）。

## ADR-0041: Webhook 统一事件总线——单一 Emit 缝 + DB outbox（双方言）+ 复刻 replication 队列模式的投递器、HMAC/enc:v1 签名链、Guard 默认拒私网、36 事件注册-休眠、第 19 槽 KindFeature（K50 终案）

- 状态: Accepted（2026-08-30，M13 B0 T-359 交付，与 T-358〔webhook.md 规格票〕并行——决策基于 PRD §1.4 工作方式条款先行，wire 字面量锚点随 T-358 落地后回填，见「锚点回填位」；本 ADR 定机制不定字面量——ADR-0035/0038 同款条款）
- 日期: 2026-08-30
- 背景: FR-114/115（M13 主轴选题）：36 事件类型注册 + 订阅 CRUD/test REST + 事件源织入 + 可靠投递（outbox/退避/死信/签名/SSRF）+ 控制台最小面。取证特例（PRD §1.4-1）：反编译集合无 webhook addon——**JFrog 官方 REST 文档为唯一行为基准**（full-feature-matrix §待验证既定 + inv-4 L186 声明）；inv-4 锚点仅补白：I2（管理面在 Access 侧、`internal:webhook` 权限常量——BinFlow 单体平移，inv-4 判定「可整体平移、无需模拟 Access 拆分」）、I3（unifiedevent outbound dispatcher——HTTP POST JSON 出站形态存在性）、I5（worker-events 40+ 类型清单互证）、K3（outbox 表六方言 DDL——**模式**取证，载体自定）。既有参考缝（代码现状核对）：audit.Recorder（best-effort facet + Actions() 词表闭集）、replication Engine（DB 任务行 + DefaultRetryBackoff 退避切片 + revive + signal/drain 环——元数据域任务队列的已验证形态）、remote.Guard（SSRF 机器：classifyIP 分类清单 + Dialer 连接期 DNS rebinding pinning + `replication.allow_private_target` 开关先例〔config.go `DefaultAllowPrivateTarget=true`〕）、addons 18 槽 + license.Manager.AddonEnabled 单求值点（ADR-0032/0033）；migration 水位 017。约束：主路径零变化（FR-114 AC6——织入旁路，发布路径 P95 偏差 <10%〔NFR-P58〕）；双方言义务（ADR-0007）；零新外部依赖（ADR-0005——HMAC-SHA256/AES-256-GCM 皆 stdlib）；资源门维持（footprint ≤100MB / check-size ≤100MiB / 冷启动 <2s——webhook 引擎不得破 M12 转绿门〔NFR-P60〕）。
- 候选方案: （八轴）
  - 总线形态: A) **单一进程内总线** `internal/webhook.Bus`——域缝一行 `Emit(ctx, Event{Type, Repo, Path, ...})`，订阅匹配/fan-out/入箱/门控全归总线，域不认订阅；B) 各域自挂——repo 删除 seam / copy-move 族 / 属性系统 / 各 adapter 发布路径各自 import webhook 做匹配与入箱；C) 通用领域事件日志底座（audit 与 webhook 共源消费，event-sourcing lite）。
  - 订阅模型: A) **push**（HTTP POST outbound——webhook 本义，Artifactory 对齐）；B) pull（事件流 REST + 游标拉取）；C) push + 消费者注册面（inv-4 I4 unifiedevent registration 全平移——多消费者总线）。
  - 投递器: A) **新建 `internal/webhook.Dispatcher`，复刻 replication Engine 队列模式**（DB 任务行 + 退避切片 + signal/drain 环 + 启动 sweep——同模式新代码，不共用实例）；B) 泛化 internal/replication.Engine 两域共用一个引擎；C) 内存 channel 队列 + 盘上日志补持久。
  - 入箱时序: A) **同步小事务 fan-out**（域 mutation 提交后、HTTP 响应返回前：匹配订阅 → 逐订阅批插 delivery 行，单 DB 事务）；B) 域事务内真 outbox（与 node 写共享 metadata tx）；C) 异步进程内缓冲（channel → 后台 goroutine 入箱）。
  - 签名与 secret: A) **HMAC-SHA256 over 存储字节 + per-subscription secret + enc:v1 密封**；B) secret 明文列（仅依赖传输层 TLS）；C) mTLS 客户端证书（接收端多为内网 CI/CD 简单端点，形态不适配）。
  - SSRF 姿态: A) **默认拒私网 + `webhook.allow_private_target` 开关**（默认 false）；B) 默认放行（replication 同款——`DefaultAllowPrivateTarget=true`）；C) 订阅级豁免键。
  - 事件闭集: A) **36 型全注册 + Source 标注（wired | dormant）单一事实源**；B) 仅 wired 子集注册（artifact/artifactProperty/docker 约 12~15 型）。
  - 槽位: A) **ID="webhook"、Kind=KindFeature、MinTier=pro（Q4 暂行）**；B) 新增 Kind 第三值 "feature-int"（PRD 暂行写法字面化）；C) 不入槽恒开。
- 决策: **八轴全 A**。要点十条：
  1. **总线形态 = 单一 Emit 缝，与 audit 平行不合并**。`internal/webhook.Bus` 是唯一域入口：`Emit(ctx, Event)` 同步执行「门控查询 → 订阅匹配（event_type + criteria 过滤器）→ 逐订阅实例化 envelope → 批插 delivery 行」。域缝（M12 统一删除 seam、copy/move 操作族、属性系统、各协议发布路径）各加一行 Emit 调用，与既有 audit.Record 并列同址。**不合并进 audit 的论证**（否决轴 1-C）：契约不同（内部 append-only 治理记录 vs 外部官方 envelope 数据契约）、可靠性档位不同（audit best-effort swallow-and-log vs webhook at-least-once + 死信）、脱敏方向相反（audit 入库 redact vs webhook 出站签名明文）。**不用各域自挂**（否决轴 1-B）：匹配/门控/入箱逻辑在 N 个域包重复，订阅模型演进 = N 处改；Bus 把「事件织入面降级为不入箱」（门控三缝之一）收敛为单点。inv-4 I3 的 jfbus 多消费者总线与 I4 registration REST **不平移**（单体单消费者，无 Worker/Xray 对端）——留痕 M14+ build-info/Workers 域再评估。
  2. **订阅模型 = push-only**。BinFlow 主动 HTTP POST 到订阅回调 URL；2xx = 投递成功，3xx/4xx/5xx/超时/连接错 = 可重试失败（**重定向不跟随**——开放重定向是 Guard pinning 的绕过面，`CheckRedirect: ErrUseLastResponse`〔replication client 同款〕）。**at-least-once**：kill -9 落在「发送成功与状态落库之间」→ 重启后重投可能，接收端幂等义务文档化（tech-writer 指南）。pull 面（事件流 REST）不建。**test 端点 = 同步直发**：构造合成事件 → 同一签名/SSRF/HTTP 客户端路径单发一次（**不入 outbox、不重试、无死信**），响应携带 attempt 结果（状态码/耗时/错误类别）；记审计 `webhook.subscription.test`。
  3. **outbox 载体 = metadata DB 双方言两表（migration 018，sqlite + postgres，编号先到先得惯例）**——replication_tasks 先例（元数据域任务持久化的既有形态；ADR-0040 选磁盘目录是 storage 层「不读 DB」分层纪律的特例，webhook 无此约束）：
     ```sql
     webhook_subscriptions(
       id TEXT PRIMARY KEY,            -- uuid
       event_type TEXT NOT NULL,       -- 闭集校验（36 型，点 7）
       url TEXT NOT NULL,
       secret_enc TEXT NOT NULL DEFAULT '',  -- enc:v1 或空（空 = 无签名）
       criteria TEXT NOT NULL DEFAULT '{}',  -- 过滤器 JSON（repo/path 模式集；strict 未知键拒收）
       enabled INTEGER NOT NULL DEFAULT 1,    -- PG 方言 BOOLEAN（双方言差异照既有迁移惯例）
       created_at TEXT NOT NULL, created_by TEXT NOT NULL,
       updated_at TEXT NOT NULL, updated_by TEXT NOT NULL,
       UNIQUE(event_type, url))        -- 单事件类型/订阅暂行（Artifactory wire 惯例——PUT /api/webhooks/{type} 单型）；若 T-358 证实多事件型订阅 → 子表 additive 迁移，机制条款不变
     webhook_deliveries(
       id TEXT PRIMARY KEY,
       subscription_id TEXT NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,  -- 订阅删除 → 在途行级联消失（停止投递，不残留孤儿）
       event_type TEXT NOT NULL,
       payload TEXT NOT NULL,          -- fan-out 时逐订阅实例化的最终 envelope 字节（快照：订阅后续修改不追溯已入箱事件；envelope 含订阅标识字段 → 投递器直发零改写，HMAC 对存储字节计算）
       status TEXT NOT NULL CHECK (status IN ('pending','delivering','delivered','dead')),
       attempts INTEGER NOT NULL DEFAULT 0,
       next_attempt_at TEXT NOT NULL,
       last_error TEXT NOT NULL DEFAULT '',
       last_status_code INTEGER,
       created_at TEXT NOT NULL, delivered_at TEXT)
     -- 索引 (status, next_attempt_at)：queue 谓词 = status='pending' AND next_attempt_at <= now
     ```
     secret **投递时现读**（轮换即时生效于在途行）。delivered 行保留 7 天（repo cleanup cron 家族裁剪——控制台「最近投递」消费窗）；dead 行不自动清（重放或随订阅级联删除）。
  4. **投递器 = Dispatcher（replication Engine 队列模式的复刻，非共用）**。否决轴 3-B：replication 任务是 blob 推送（payload = sha256 引用 + 凭据解析 + 平面协议族），webhook 任务是信封直发（payload = 内嵌字节 + 签名头）——泛化共用使两域任务语义互相渗透，同模式各自实现是分区纪律的代价下限。**参数定案（K50，引擎内部常量——ADR-0040「零新配置键」同姿态，旋钮化等真实需求）**：尝试上限 **10**（含首发；首发后重试 9 次）；退避 = **2s 起步 ×2 递增、单次上限 10min**（2/4/8/16/32/64/128/256/512s——最坏故障窗 ≈17min）；单次 attempt 超时 **10s**；投递 worker 并发 **4**；超上限 → status=dead（可查询、可重放：dead→pending 重置 attempts，REST + 控制台双面）+ 告警日志一行。**启动 sweep**：status='delivering' 行全部回 pending（attempts 保留——kill -9 中途行的唯一回收路径，ADR-0028 孤儿回收同姿态）。**优雅停机**：SIGTERM → 停止取新行、在途 attempt 完成或超时、行状态落库（Engine.Close 家族接线）。**queue_depth gauge = pending 行数**。
  5. **签名与 secret 链**：机制 = **HMAC-SHA256(per-subscription secret, payload 存储字节)**，签名头名与编码（hex/base64）随 T-358 锚定（K49——官方契约字面量，本 ADR 不猜）；secret 缺省 = 不带签名头（secret 可选，照官方惯例）。secret 存储 = **enc:v1 AES-256-GCM**（复用 `BINFLOW_REMOTE_CREDENTIALS_KEY` 实例级主密钥族——ADR-0035/0038 同链）；POSTURE 同款三句：无主密钥 + 无 secret 行 → 正常启动；无主密钥 + 写带 secret 订阅 → 写路径拒绝；存量行含 secret 而无主密钥 → 启动 fail-fast。回显脱敏哨兵 + write-only PUT 语义（字段缺省或哨兵回传 = 保持不变，新明文 = 替换）；secret 不落日志、不进审计 detail（NFR-S66）。
  6. **SSRF = 复用 remote.Guard 机器，默认拒私网**（与 replication 默认放行**有意不对称**）：订阅 URL 校验与投递出站连接全部经 Guard（scheme 闭集 http/https、classifyIP 分类清单、Dialer 连接期 DNS rebinding pinning、响应体有界读取）。**不对称论证**：replication/auth 测试连接的私网目标是运维静态配置面（YAML/DB 段，现实拓扑即内网——ADR-0025 决策 4 判定在案）；webhook 订阅 URL 是 **REST CRUD 动态面 + 任意外发端点**——CI 集成向更广主体开放后的经典 SSRF 升梯向量，且接收端在私网（Jenkins dogfood）属常态 → 显式开关 `webhook.allow_private_target`（**默认 false**，拼写镜像 `replication.allow_private_target` 键族）承载而非默认放行。端点静态校验：host 必填、userinfo 拒收、fragment 拒收、URL 长度 ≤2048。last_error 携带错误片段（≤4KB、URL 脱敏）。
  7. **36 事件注册-休眠（Q6 暂行）**：单一事实源 = `internal/webhook/eventtypes.go` 常量集，每型 `{Name, Domain, Source}`，Source ∈ **wired**（BinFlow 有本体域、有织入点）| **dormant**（可订阅、零触发、呈现如实标注——不伪造触发源）。订阅校验对闭集（36 型全合法、未知型 400）；M13 wired 面 = artifact 族（deployed/deleted/copied/moved/properties）+ artifactProperty 族 + docker 族（pushed/deleted/tag*，dind 腿）；build/releaseBundle/distribution/curation 等无本体域 = dormant。**翻转路径**：Q6 用户裁定裁剪 → 删 dormant 常量 + 闭集缩 + 文档（存量 dormant 订阅处置归裁定票）；新域落地（M14+ build-info）→ dormant→wired 加织入行，schema 零变化（event_type 是字符串列）。清单终版以 T-358 webhook.md 为准（K48——PRD「36」为骨架数，以官方文档定案数为准）。
  8. **第 19 槽 = ID "webhook"、Kind=KindFeature、MinTier=pro（Q4 暂行）**。**不新增 Kind 第三值**（否决轴 8-B）：Kind 的路由语义是「package-type 的 ID 即内容分发路由键」，feature-integration 与 feature 无路由差异——加第三值动装配期断言与 /api/v1/addons 呈现为零收益；PRD「feature-int」读作注记性写法。**三缝**：① 订阅 REST（feature addon 面）门 DENIED → 403 + `X-Binflow-License-Required: webhook`（ADR-0032 D4 形）；② 事件织入面：Bus.Emit 首行查门（**注入 gate func——装配层 facet，internal/webhook 不 import license**，ADR-0040 决策 8 同款分层姿态）→ DENIED 直接 return 不入箱（主路径行为零变化；gate 指标 `binflow_addon_gate_requests_total{addon=webhook}` 自动生效）；③ `addons.disabled` 熔断（ADR-0032 as-built D1）：订阅写动词 403（breaker 专用文案、**无** license 头）+ 读面 200；**投递面 = 暂停不丢**——dispatcher 停止取行，恢复后照 next_attempt_at 排空（「熔断器不是数据墓碑」哲学的延伸：存量 delivery 是已订阅契约的存量数据面）。**Q4 翻转点**：T-358 取证官方 license 标注，community 可用则 MinTier pro→community = slots.go 常量一行 + 档位矩阵行 + license 文档（conductor/用户终裁，翻转点归 T-358 收口批）。
  9. **权限映射**：`internal:webhook`（inv-4 I2 的 Access 权限常量）的 BinFlow 映射 = 管理能力对：订阅 GET/list = **CapSystemRead**（readonly_admin 只读可见——FR-115.5 readonly 臂）；create/update/delete/test = **CapSystemWrite**（admin）。不映射 repo m 动作（过滤器可跨仓，m-holder 求值需「过滤器→仓集」语义，M13 不建——用户裁定 CI 工程师自助订阅则届时新裁决留缝）。挂载走 **dispatchAPI 显式路由族**（ADR-0034——管理面 = handler + routeAuth，**非** apiProtocolMounts）。
  10. **主路径契约与可观测**：入箱 = 同步小事务（本地 SQLite/PG 批插，亚毫秒级——NFR-P58 P95 偏差 <10% 的结构性保证）；**入箱失败 = WARN + `binflow_webhook_enqueue_failures_total` 指标**（本 ADR 增补——audit best-effort 同姿态不阻塞主路径，但 webhooks 有交付承诺，损失必须可见；L06 零丢承诺的对象是**已入箱**事件）。指标族：`binflow_webhook_{deliveries_total, retries_total, dead_letter_total, queue_depth}` + enqueue_failures_total（expvar 自研，ADR-0022 姿态）。审计五词入 Actions() 词表：`webhook.subscription.{create,update,delete,test}` + `webhook.dead_letter`（actor/订阅标识/事件类型/URL 脱敏——userinfo 剥离）。启动 INFO 一行 outbox 水位（pending 条数——ADR-0040 同款形态）。
- 理由: 八轴的共同判据是「参考缝复用 + 主路径结构性零变化 + 闭集可验证」。总线 A 把门控/匹配/入箱收敛为单点（事件织入面降级 = 单点 return，AC6 的结构性保证）；各域自挂（B）让订阅模型演进成本 ×N。push A 是 webhook 本义与官方文档基准的交集，pull/I4 为不存在的消费者（单体单消费者）买单。投递器 A 在「复用已验证模式」与「域语义不渗透」间取正确切面——replication Engine 的 backoff 切片/signal/drain/sweep 形态已被 revive 与两实例 e2e 验证，复刻而非泛化。入箱时序 A：真事务 outbox（B）要求 domain tx 贯穿 emit 缝——repo.Service.Put 无此结构（audit 同为提交后旁路），为强一致重写主路径违背「旁路增量」选题前提；C 的进程内缓冲在 kill -9 窗口丢事件（L06 红线）。签名 A 全 stdlib 且「对存储字节签名」使 body 与签名的对应关系免推理。SSRF A 的不对称是三条已判先例（ADR-0025 决策 4 两例）与 webhook 面形态差异的显式推理结果，非疏忽。闭集 A 让「可订阅但无触发」成为诚实可断言的状态而非静默 404；槽位 A 守住 Kind 二值闭集（新增不破坏）。
- 后果: 新增 `internal/webhook` 包（eventtypes.go / bus.go / store.go（metadata sub-store）/ dispatcher.go / sign.go；Deps 注入 gate func + Guard + Cipher + audit facet——不 import license）；migration 018 双方言两表；`internal/addons/slots.go` 增第 19 槽构造 + cmd 装配清单一行；config 增 `webhook.allow_private_target`（默认 false——与 replication 默认 true 的不对称在部署文档注明，tech-writer 义务）；audit 词表 +5；指标族 +5；httpapi dispatchAPI 族增订阅 CRUD/test 路由（T-362 area）；cmd 装配 Bus/Dispatcher 生命周期 + 优雅停机接线；控制台最小面（P1 独立 FE 票——订阅列表/新建/编辑/删除/test + 最近投递，readonly_admin 只读，MUI 四闸门）；tech-writer：接收端幂等义务 + HMAC 验签示例 + 私网开关说明 + dormant 事件标注。**分区提示**：internal/webhook + httpapi/webhooks.go + slots 单行 = 实现票 area；域缝插入行（repo/adapter 各一行 Emit）归事件织入大票**窗口独占**（PRD §1.3 既定，避免与并行票 area 重叠）。真实消费者 e2e（dogfood Jenkins 条件腿/容器接收器）归 FR-115 第二票（控制台 + e2e）。
- clean-room 边界: 行为依据 = JFrog 官方 REST 文档（事件清单/envelope 字段集/签名头契约/投递语义——T-358 逐条锚定出处）+ inv-4 §I/§K **行为模式**（outbox 模式存在性、outbound dispatcher 形态、internal:webhook 常量、事件类型清单互证）；不引用反编译代码结构——BinFlow 载体全自有（DB schema 两方言、envelope 构造、投递器、签名实现）；K3 六方言 DDL 只证「outbox 模式」不复制表结构；envelope 为官方 JSON 数据契约（PRD §1.4-4 红线——数据契约非代码）。
- **锚点回填位（T-358 webhook.md 落地后回填本节，机制条款不随锚点翻转）**：K47 订阅 REST wire（端点路径/方法/请求响应体/错误码——含单型/多型订阅形态对决策 3 UNIQUE 约束的校准）；K49 签名头名与编码 + envelope 字段集 + 过滤器语法（对决策 2/5 的字面量填充）；K48 事件清单终版与 BinFlow 触发源覆盖界（对决策 7 常量集的填充与 dormant 划界）；Q4 档位终裁（对决策 8 MinTier 的翻转点）；Q6 覆盖界终裁（对决策 7 的翻转点）。效力序：用户裁决（BOARD）> webhook.md（含 as-built 段）> 本 ADR > PRD 暂行值；分歧按 19:05 上 BOARD。
- **T-362 验收锚点骨架**（FR-114 实现大票：订阅 REST + 36 事件注册 + 事件源织入；AC 对 PRD FR-114 AC1~AC6）：
  - AC-1（CRUD+test wire）：curl 全链 create→get→list→update→delete（路径/状态码/信封照 webhook.md）；挂载断言 = dispatchAPI 族（管理面，ADR-0034）；test 端点同步直发一条合成事件（接收器断言信封）且响应含 attempt 结果（状态码/耗时）。
  - AC-2（事件闭集）：36 型全部可订阅；未知型 400；dormant 型可订阅、GET 回显标注「无触发源」（不伪造）。
  - AC-3（事件织入）：generic 仓 PUT → deployed（envelope 逐字段：类型/repo/path/时间戳/订阅标识——照 webhook.md）；DELETE / copy / move / 属性 PUT 各触发对应事件；docker push → docker 域（dind 腿）。
  - AC-4（过滤器）：repo/path 命中发/未命中不发（接收器计数双臂）；criteria 未知键 400（strict）。
  - AC-5（权限与门控）：user 角色订阅 CRUD 全 403 零副作用；readonly_admin 读可见/写 403；webhook 槽三缝——community 建订阅 403 + `X-Binflow-License-Required: webhook` → pro 200 → `addons.disabled` 熔断：订阅写 403（无头、breaker 文案）+ 读 200 + **事件不入箱**（制品 PUT 200 且 queue_depth 不增）。
  - AC-6（主路径零回归）：M1~M12 P0 抽样零回归 + 发布路径 P95 偏差 <10%（NFR-P58）。
- **T-364 验收锚点骨架**（FR-115 投递引擎票：outbox/重试/死信/签名/SSRF/可观测；AC 对 PRD FR-115 AC1~AC7）：
  - AC-1（outbox 幸存）：触发事件 → kill -9 → 重启 → 补投零丢；启动水位 INFO 行在场；sweep 断言（kill 前置 delivering 行 → 重启后 pending）。
  - AC-2（退避）：接收器 500×N → 观测退避序列（2s/4s/8s…）→ 恢复 200 → delivered；全程制品 PUT 零 5xx 零阻塞。
  - AC-3（死信与重放）：持续失败超 10 attempts → dead 可查（REST）+ 告警日志一行 + dead_letter_total；replay（dead→pending）→ 恢复成功；订阅删除 → 在途行级联消失。
  - AC-4（签名与 secret）：secret 订阅 → 接收器 HMAC-SHA256 验签绿（篡改 body 红）；无 secret 无签名头；DB grep 零明文（enc:v1）；GET 回显哨兵、PUT 哨兵回传 = 不变；无主密钥 + 写带 secret = 拒绝（POSTURE 臂）。
  - AC-5（SSRF）：私网订阅 URL 默认拒（含 DNS rebinding 腿：解析公网/连接私网 → 拒）；`webhook.allow_private_target: true` 臂投递成功；3xx 不跟随（开放重定向目标私网不触达）。
  - AC-6（可观测）：指标五枚存在且语义正确 + 审计五词 + 投递日志/last_error URL 脱敏 grep（userinfo 零命中）。
  - AC-7（NFR）：100 并发发布 → 入箱零丢 + 投递 P95 ≤2s（本地接收器）+ footprint/check-size/冷启动三门维持（M12 转绿门不破）。
