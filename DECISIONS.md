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
  2. 会话：`<data>/sessions/<uuid>/{data,state.json}`，纯磁盘状态，不入 DB；启动时扫描，过期（ttl，默认 24h）即清理。
  3. 落盘协议：session 追加写 `data`（边写边算 sha256/sha1/md5）→ fsync(data) → 若目标 blob 不存在则 `rename` 到 blobs 路径 → fsync(目标目录) → 提交元数据。同一文件系统内 rename 原子，崩溃只会留下 session 残渣或已完整 blob，无半写文件。
  4. 并发同一 blob：进程内 per-checksum singleflight 互斥；后到者发现目标已存在 → 丢弃自己的会话数据、返回已有 blob（幂等）。
  5. 引用与 GC：不设 ref_count 列；GC 为离线 CLI（`binflow-server gc`，默认 dry-run，`--apply` 才删），mark-sweep：未出现在 `nodes` 中且 `created_at < now - grace_period`（默认 24h，防与在途上传竞态）的 blob 才可删。
- 理由: 分片布局消除单目录规模问题且推导简单；全局内容寻址让跨仓去重、零拷贝移动/重命名（只改 nodes 行）免费获得；会话留磁盘使存储引擎可独立于元数据库测试与恢复；mark-sweep + grace period 用时间换锁，避免为 M1 引入分布式锁或引用计数热点。
- 后果: 备份 = `blobs/` 目录 + SQLite 文件的一致性快照（M4 交付工具）；blob 物理删除只有 GC 一个入口（运行时 DELETE 制品只删 node 引用，安全底线友好）；`sessions/` 与 `blobs/` 是版本兼容承诺，改名需新 ADR；「Artifactory 是否有 blob sidecar 属性文件」待逆向规格 docs/reverse/storage-layout.md 确认（若其有 properties 文件，BinFlow 也不跟进——以本决策为准，差异记入架构文档对齐表）。
- 勘误（2026-08-17，T-25 依 T-9 review 落地，不推翻决策本体）: ① 决策 5 的 grace 基准明确为 **blob 文件 mtime**（非 `blobs.created_at` 列——storage 不读 DB 的必然选择，方向安全：mtime 被推新只会多保留）；mark 形态升级为**引用集合回调**（调用方一次 `SELECT DISTINCT sha256 FROM nodes`）取代逐条反连接；grace/ttl 零值 = 默认 24h。② **备份/恢复硬约束**：必须保留 blob 文件 mtime（`tar` / `rsync -a` 默认保留），否则恢复后宽限期时钟重置、全部历史 blob 立即可回收——M4 备份票与 ops 文档必须遵守。③ 会话瞬态文件 state.json 的形状由架构 §4.1 契约定稿（`version` 字段 + 演进规则）；session 目录命名仍是兼容承诺，state.json 内容不是。

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
