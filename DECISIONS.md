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
- 后果: 文档与客户端示例统一 `/binflow`（例 `curl http://host:8080/binflow/<repo>/path`）；`server.base_url` 非空时必须含 `/binflow`；**[M2 风险预告]** docker 客户端固定向 `/v2/...` 发请求、无法自定义前缀，届时二选一：反代 rewrite 到 `/binflow/v2` 或为 `/v2` 开根级例外——属实现层路由例外，不推翻本 ADR，M2 出细化票据时定；控制台相对路径以 `/binflow` 为基。（repo key 长度上限后经 T-22 回写为 `{1,62}`，见 PRD FR-3-AC4。）

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
