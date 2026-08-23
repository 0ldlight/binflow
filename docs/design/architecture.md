# BinFlow 架构设计（M1 定稿；M2~M6 增量已并入，M7 增量标注 [M7]，M8 控制台对齐约束见 §13 [M8]）

> architect 维护。本文件在 ADR-0001~0027 基线上给出可并行开发的实现蓝图：包边界 = 并行开发 area 边界。
> 标注 **[M2+]** / **[M3+]** / **[M6+]** / **[M7]** 的内容当期不实现，只保证接口缝存在；标注「待逆向规格确认」的行为以 `docs/reverse/` 规格为准，规格冲突时先回 ADR。
> 文档中文，标识符/表名/字段英文。代码规范：错误 wrap 带上下文、显式 context、table-driven 测试、依赖注入。

---

## 1. 架构总览

```
                        ┌────────────────────────────────────────────────┐
                        │                cmd/binflow-server              │
                        │  load config → open metadata → mount handlers  │
                        └───────────────┬────────────────────────────────┘
                                        │ 组装（wire，构造函数注入）
        ┌───────────────────────────────┼─────────────────────────────────┐
        │                               │                                 │
┌───────▼────────┐  ┌───────────────────▼───────────┐  ┌─────────────────▼────────┐
│   httpapi      │  │          adapter              │  │         console          │
│ /binflow 前缀   │  │ generic[M1] docker[M2]        │  │ go:embed 静态前端 [M4]    │
│ 路由/middleware │  │ maven/npm/pypi[M3]            │  │ M1: /binflow/api/v1 JSON │
└───────┬────────┘  └───────┬───────────────────────┘  └──────────────────────────┘
        │                   │ Handler(http.Handler)
        │           ┌───────▼────────┐
        │           │      repo      │  仓库模型：local[M1] / remote,virtual[M3]
        │           │ Get/Put/Delete │  解析顺序、ACL 决策点
        │           └──┬──────────┬──┘
        │              │          │
        │     ┌────────▼───┐  ┌───▼─────────┐   ┌──────────┐   ┌─────────┐
        │     │  storage   │  │  metadata   │   │   auth   │   │  audit  │
        │     │ blob引擎    │  │ Store(SQL) │   │ 用户/令牌  │   │ 审计事件 │
        │     │ 会话/GC     │  │ 迁移器      │   │ ACL      │   │         │
        │     │ Backend↓    │  └───┬─────────┘   └────┬─────┘   └────┬────┘
        │     └──────┬─────┘      │                  │              │
        │            │            │                  │              │
        └────────────┴────────────┴──────────────────┴──────────────┘
                     依赖方向：上层 → 下层，同层禁止横向 import
                     所有跨包调用只走「§3 接口契约」中的小接口
```

请求入口统一为 `/binflow` 前缀（ADR-0008，用户定案）：

```
/binflow/api/v1/...        自有管理 API（需认证）
/binflow/api/...           Artifactory 兼容子集（需认证）
/binflow/v2/...            docker registry [M2]（已由 ADR-0010 改为 /v2 根级例外，见下）
/binflow/<repo>/<path...>  内容路径（GET/HEAD 默认匿名可读，ADR-0009）
/healthz /readyz /metrics  基础端点（不带前缀，探针/抓取用）
```

部署形态（ADR-0004）：单二进制 / Docker / compose / Helm / K8s 清单 / systemd / 离线包，全部是同一二进制 + 同一 YAML 的包装，无形态特有代码分支。详见 §9。

### 1.1 一次 Generic 上传的请求路径（M1 最小闭环）

```
PUT /binflow/<repo-key>/<path>                        (httpapi: middleware 链)
  → auth.Authenticator: 解析 Authorization → *Principal
  → auth.Authorizer:   Can(repo, path, "w", principal)
  → adapter/generic:   layout 解析（无布局，path 即相对路径）
  → repo.Local.Put:    storage.BeginSession → 流式写入 → Commit(sha256 必须 == 客户端 X-Checksum-Sha256，若有)
                       metadata: blob link + upsert node（blob-first 两条语句，FK 兜底——§3.2 事务边界注）
  → audit:             append(action="deploy", ...)
  → 201 Created
```

---

## 2. 包结构与职责边界

```
binflow/                       # Go module: github.com/lzwzzy/binflow（ADR-0008）
├── cmd/binflow-server/        # main：装配与生命周期；subcommand: serve | gc
├── internal/
│   ├── config/                # YAML + env 覆盖；强校验；不可变 Config 结构
│   ├── storage/               # blob 引擎：会话、checksum、原子落盘、GC
│   ├── metadata/              # Store 接口 + sqlite 实现 + 迁移器
│   ├── repo/                  # 仓库模型与解析顺序；服务层核心
│   ├── remote/                # [M3] remote 代理引擎：fetcher/缓存状态/SSRF 防护/凭据解密
│   ├── replication/           # [M6+] 复制/联邦：pusher/scheduler/status（ADR-0021）
│   ├── metrics/               # [M6+] Prometheus 指标暴露（stdlib expvar 实现，ADR-0022）
│   ├── migrate/               # [M6+] Artifactory 迁移工具（bf migrate 可复用，ADR-0024）
│   ├── adapter/               # 协议 SPI：Handler 挂载 + layout
│   │   └── generic/           # M1 唯一实现
│   ├── auth/                  # Principal / Authorizer / TokenRegistry / IdentityProvider（OIDC/LDAP M6+）
│   ├── audit/                 # 审计事件 append + 查询
│   └── client/                # [M6+] HTTP 客户端封装（被 bf CLI 消费，不 import storage/metadata）
│   ├── httpapi/               # Server、路由表、middleware、错误信封
│   └── console/               # go:embed 前端资产 [M4]，M1 仅 JSON API
├── web/                       # 控制台前端源码（构建产物进 internal/console）
├── docs/user/                 # 帮助文档 Markdown 源（tech-writer，按文件地图写入）
├── docs-site/                 # Docusaurus 站点配置与聚合构建（ADR-0011；内容源=docs/user，
│                              # writer 不碰本目录；build 产物复制进 internal/docs go:embed）
└── deploy/ charts/            # release-engineer 领地，架构只约定 §9
```

**边界规则（强约束，review 依据）**：
1. 包间只 import 对方**导出的接口与值类型**（定义在各自的 `api.go` / `doc.go` 中声明），禁止摸内部 struct 字段、禁止 import `_test` 之外的内部文件。
2. 依赖方向单向：`httpapi → adapter → repo → {storage, metadata}`；`auth`/`audit` 被 `httpapi`/`repo` 消费；`config` 被所有包消费（只读）。`storage` **不** import `metadata`（§4 的核心解耦）。违例=拒绝合并。
3. 接口的实现方在 `main` 装配时注入，任何包不做全局单例（`config` 除外，进程内不可变）。

各包职责与公开面：

| 包 | 职责 | 公开接口（唯一入口） | M1 不做 |
|---|---|---|---|
| `config` | 加载 `binflow.yaml`、env 覆盖（`BINFLOW_` 前缀）、校验、默认值 | `Load(path string) (*Config, error)`；`Config` 值树 | 热重载 [M4+] |
| `storage` | blob 生命周期：上传会话、checksum 计算、原子落盘、打开读、删除、GC；**M6+ Backend 接口**（disk/s3） | `Engine`（见 §3.1）；`Backend`（§3.1a，包内接口） | S3 后端（M6 实现，ADR-0018/0019） |
| `metadata` | 全部 SQL：repositories/nodes/blobs/users/tokens/audit_events 的 CRUD；迁移 | `Store`（见 §3.2）；`Migrate(ctx, dialect)` | 复杂查询优化、审计分库 |
| `repo` | 仓库语义：Get/Put/Delete/List/Search 的用例编排；local + virtual 解析（ADR-0013）；**M6+ 复制触发** | `Service`（见 §3.3）；`GetLocal(ctx, key)` | remote fetch 本体（归 internal/remote） |
| `remote` [M3] | 上游代理：pull-through fetch、TTL/条件再验证缓存状态、SSRF 双检、AES-GCM 凭据解密、stale-while-error | `remote.Fetch(ctx, repoKey, path) (FetchResult, error)`（fetcher 门面） | 重试库/熔断（stdlib-only，ADR-0012）、手动失效 UI |
| `replication` [M6+] | 复制/联邦：推式异步推送、拉式复用 remote 机制、任务状态跟踪、冲突处理（ADR-0021） | `Pusher` / `Scheduler` / `StatusReader`（消费方接口） | 双向复制、事件驱动、删除传播 |
| `metrics` [M6+] | Prometheus 指标：stdlib expvar 实现，Prometheus 文本格式输出（ADR-0022） | `Registry`（注册/更新/格式化） | Histogram 精确分桶、引入 prometheus/client_golang |
| `migrate` [M6+] | Artifactory 迁移：REST API 读取 + 转换映射 + BinFlow 写入（ADR-0024） | `ArtifactoryReader` / `Converter` / `BinFlowWriter`（纯 HTTP client） | 审计日志历史迁移、build-info 迁移 |
| `client` [M6+] | HTTP 客户端封装：Base URL 拼接、auth 头注入、错误信封解析、重试、进度条（被 bf CLI 消费） | `Client`（REST 方法族） | 非 REST 协议（docker/maven/npm 客户端） |
| `adapter` | SPI：协议无关的 Handler 注册与路由挂载；`layout` 包 | `Handler` + `Register/All`（见 §5.1） | 各协议本体 |
| `adapter/generic` | Generic(raw) 语义：path 即 layout、上传校验、目录列表 | 实现 `Handler` | —— |
| `auth` | 密码校验（argon2id）、Token 签发/校验、路径 ACL 决策；**M6+ OIDC/LDAP 身份提供者** | `Authenticator` / `Authorizer` / `TokenRegistry` / `IdentityProvider`（§3.4 扩展） | 组/匿名/LDAP [M4+] |
| `audit` | append-only 审计事件 + 查询 | `Logger`（§3.5） | UI、导出 [M4] |
| `httpapi` | 监听、路由表、middleware 链、统一错误信封、健康检查、优雅停机 | `Run(ctx, deps)`（§7） | —— |
| `console` | `//go:embed dist`（web/ 构建产物）；`Handler() http.Handler` 挂 `/binflow/ui/**`（ADR-0014 勘误①——包名不变、挂载段为 ui） | M1~M3 返回占位页 | 前端本体源码（在 `web/`，ux-designer + 前端票） |
| `docs`（internal/docs） | `//go:embed` Docusaurus build 产物；`Handler() http.Handler`（ADR-0011） | M1~M4 不存在（M5 脚手架票引入） | 独立站点双轨托管（不承诺） |

---

## 3. 模块间接口契约（Go）

> 以下签名是**并行开发的解耦契约**，dev 角色不得单方修改；变更走 architect（改本文件 + 通知主会话）。放在各包 `api.go`。

### 3.1 storage.Engine（owner: dev-go-storage）

```go
package storage

// BlobRef 指向一个已落盘的 blob。checksum 是全局主键。
type BlobRef struct {
    Sha256 string // hex, 小写, 64 chars, 主键
    Sha1   string // hex, 小写, 40 chars, 可为空（客户端未提供且流式已算出）
    Md5    string // hex, 小写, 32 chars
    Size   int64
}

// Session 一次上传会话；data 文件边写边算三种摘要。
// 实现必须并发安全：不同 session 可并行，同 session 串行。
type Session interface {
    ID() string
    // Offset 返回会话当前累计已收字节数（disk 后端 = uploads/<id>/data 文件长度）。
    // [M7] REST 续传的权威 offset 源：重哈希恢复（ResumeSession）后即重算后的文件
    // 长度，取代 adapter 侧内存镜像作为 416/Range 响应的基准。实现必须并发安全
    // （与 Append/Commit 互斥读）。
    Offset() int64
    // Append 追加并返回当前累计 offset；r 由 adapter 提供（http body）。
    // Append 一旦失败（含部分写），会话即被毒化：后续 Append/Commit 一律返回
    // wrap ErrSessionPoisoned 的错误（错误链双 %w 同时携带原始因，T-9 修复新增），
    // 调用方必须 Abort 丢弃——绝不允许把与摘要不符的部分写文件 rename 入库。
    Append(ctx context.Context, r io.Reader) (written int64, err error)
    // Commit 终结会话：校验期望摘要（非空则必须匹配）→ fsync → rename 入库。
    // 已存在同 sha256 blob 时：丢弃会话数据，返回已存在引用（幂等去重）。
    Commit(ctx context.Context, expect BlobRef) (BlobRef, error)
    // Abort 丢弃会话。nil receiver 安全。
    Abort(ctx context.Context) error
}

type Engine interface {
    // BeginSession 创建会话；dirs 自动创建。Close 后返回 ErrEngineClosed。
    BeginSession(ctx context.Context) (Session, error)
    // ResumeSession 按 id 恢复（T-209/M7 契约，ADR-0006 决策 2 修订版）：从 upload_sessions
    // 行 + uploads/<id>/data 部分数据重物化会话——重哈希重建三摘要链，返回可直接续 Append
    // 的会话。行缺失/已过期未清扫 → ErrSessionNotFound（fail-closed，行的唯一正确回收路径
    // 是 sweep）；数据文件缺失但目录在 → 按 offset 0 新建空文件恢复（有意行为，见实现 godoc）。
    // disk 后端注入 Options.Sessions（metadata.UploadSessionStore）后可用；S3 后端恒
    // ErrSessionNotFound（multipart 会话状态由 S3 服务端持有，TestS3ResumeSessionNotSupported
    // 钉死 hard 404 契约）。
    ResumeSession(ctx context.Context, id string) (Session, error)
    // Open 打开 blob 读取；调用方负责 Close。不存在 → ErrBlobNotFound（wrap）。
    // 返回的 BlobRef 只保证 Sha256+Size 有值；sha1/md5 的事实源是 metadata blobs 表
    //（无 sidecar，ADR-0006）——需要三摘要全量时用 Stat。（T-9 review 回写项 D）
    // [M6+] 返回类型从 io.ReadSeekCloser 变更为 io.ReadCloser（ADR-0019 决策 4）：
    // disk 实现仍返回可 Seek 的类型（类型断言可用），S3 实现返回 GetObject 的 body。
    // 需要 Seek 的调用方（docker blob GET with Range）在 adapter 层通过 HTTP Range 头
    // 直接请求 S3 的部分对象，而非在 Go 层 Seek。
    Open(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error)
    // Stat 是完整性校验而非轻量探测（T-9 review 回写项 E）：全量读内容重算三摘要并
    // 验证内容与路径名自洽，不自洽 → ErrBlobCorrupt。代价 O(size)，供 GC/一致性检查
    // 与修复流程用；存在性探测请走 metadata（blob 行在即视为物理在，异常由 GC 兜底）。
    Stat(ctx context.Context, sha256 string) (BlobRef, error)
    // Delete 物理删除；只允许 GC 调用（运行时制品删除走 metadata 层删引用）。
    // 引用集检查由调用方（GC）先完成；Close 后返回 ErrEngineClosed。
    Delete(ctx context.Context, sha256 string) error
    // GC 是 mark-sweep（T-9 review 回写项 A，集合形回调取代逐条形——逐条形 = blob 数次
    // DB 往返，集合形一次查询；且磁盘驱动 sweep 更彻底，能发现 blobs 表缺行/DB 回退旧
    // 备份产生的孤儿）：
    //   mark：referenced 一次返回全部被 nodes 引用的 sha256 集合（调用方实现为
    //         SELECT DISTINCT sha256 FROM nodes）；
    //   sweep：storage 扫描 blobs/ 磁盘，未在集合中且文件 mtime 早于 now-grace 者为候选。
    // apply=false 只返回候选清单（dry-run 为默认姿态）；apply=true 删除并返回实际删除清单。
    // grace <= 0 视为 DefaultGCGrace(24h)——零值不是「立即回收」，要无宽限期须显式传亚秒
    // 时长（回写项 J）。Close 后返回 ErrEngineClosed。
    // 注：引用集全量驻内存（1M nodes ≈ 100MB 量级），M6+ 大库需流式接口变体（回写项 A 注记）。
    GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error)
    // Close 关闭引擎：停止接受新会话与变更、排空在途 Append/Commit、保留未过期会话
    // （upload_sessions 行 + uploads/<id>/ 数据文件原样保留——干净停机后续传成立，
    // ADR-0028；孤儿回收唯一路径 = 启动 sweep + TTL，Close 不做任何会话删除，仅打
    // 保留清单 INFO）、拒绝后续 BeginSession/Delete/GC（ErrEngineClosed）；Open/Stat
    // 继续服务已提交 blob（只读不可变文件）。幂等。（T-9 回写项 B；[M7] ADR-0028 修订。）
    Close() error
}
```

### 3.1a storage.Backend（包内接口，[M6+] ADR-0019）

```go
package storage

// Backend 是 storage 包内的纯 blob CRUD 接口，不对外暴露。Engine 是唯一公开面。
// DiskEngine 直接实现 Engine（不经过 Backend），S3Engine 内部持有一个 Backend（即 S3 client）。
// 设计目的：Engine 接口保持稳定不变，S3 的实现差异被 Backend 封装在包内。
type Backend interface {
    // Put 写入一个完整的 blob。在 disk 上 = write + fsync + rename；
    // 在 S3 上 = PutObject（S3 PutObject 是原子操作，无需 rename）。
    Put(ctx context.Context, sha256 string, r io.Reader, size int64) (BlobRef, error)
    // Get 读取 blob。S3 返回 GetObject body。
    Get(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error)
    // Delete 物理删除 blob。
    Delete(ctx context.Context, sha256 string) error
    // Exists 检查 blob 是否存在（不读取内容）。
    Exists(ctx context.Context, sha256 string) (bool, error)
    // List 返回全部已知 blob sha256 列表（用于 GC sweep）。
    List(ctx context.Context) ([]string, error)
}
```

错误约定（包级 sentinel，wrap 后仍 `errors.Is` 可判；T-9 review 回写项 C/H 补全）：
`var ErrBlobNotFound`（Open/Stat/Delete 未命中）、`ErrSessionNotFound`（ResumeSession；早期文档误写 ErrNoSuchSession，以本名为准）、`ErrChecksumMismatch`（Commit 期望摘要不符，不落盘）、`ErrBlobCorrupt`（Stat 完整性校验失败）、`ErrEngineClosed`（Close 后的变更操作）、`ErrSessionPoisoned`（Append 失败后的会话毒化，双 %w 错误链）。

备份/锁公共面（M4 增量，T-96 落地、T-112 补记——跨包消费契约；REST gc 面（T-94）与 CLI gc/export 同原语，勿重写）：`AcquireDataLock(dataDir, op) (*DataLock, error)`（争用 wrap `ErrDataLockHeld`，fail-fast 不排队——排队的 export 会无限期扣押输出目录）+ `(*DataLock).Release()`（幂等、nil 安全）——data 目录级跨进程互斥锁（`<data>/.maintenance.lock` 0600 常驻不删，flock/LockFileEx），GC↔export 双向互斥（ADR-0015 勘误③）；锁文件首行 `pid=<n> op=<op>` 为跨包诊断契约（op 值域 {gc, export}）。manifest 族 `Manifest/ManifestMetadata/ManifestBlob` + `Validate`（哨兵 `ErrManifestInvalid`）/`WriteManifest`/`LoadManifest`（容忍未知字段，前向兼容）+ `CopyBlobsTree`/`HashFile`/`BlobPath`（engine.blobPath 委托之——§4.1 blob 路径形态的唯一定义点，engine/export/import 三消费方同源）。

### 3.2 metadata.Store（owner: dev-go-core）

```go
package metadata

type Store interface {
    // 迁移由 Open 时自动执行到最新（ADR-0007），不在此接口。
    Repos() RepoStore
    Nodes() NodeStore
    Blobs() BlobStore
    Users() UserStore
    Tokens() TokenStore
    Audits() AuditStore
    // Ping 供 /readyz 探活（T-10 实现扩展，合规——本文件声明「定稿以本文件 + 代码 api.go 为准」）。
    Ping(ctx context.Context) error
    Close() error
}

// M1 事务边界（T-10 review M1 裁决，采纳 a 案）：Store 接口不设 Txn 方法；
// Put 的「node + blob link」是两条语句、blob-first 顺序（BlobStore.Put DO NOTHING 幂等
// 在前，NodeStore.Put 在后），崩溃窗口由 FK（nodes.sha256 → blobs.sha256）兜底强制该顺序。
// 中间崩溃只残留无引用 blob 行（GC 可收，不损数据），语义等价于原「一个 SQL 事务」的意图，
// 且避免为一条组合语句扩接口。原 §3.2「Txn 只读透传」注释行系文档残缺，作废（T-25 清理）。
// 快照检查面（M4 备份，T-96；T-112 补记）：SnapshotChecksums/SnapshotSchemaVersion/
// LatestSchemaVersion/PurgeTransientFromSnapshot 为包级函数而非 Store 方法——快照是
// 制品不是活库（不经 Open，防对制品跑迁移+种子）；(*sqliteStore).VacuumInto 经 cmd 侧
// 消费接口（metadataSnapshotter，定义在消费侧）类型断言使用，不污染本接口。

type NodeStore interface {
    // Get 返回 ErrNodeNotFound 若无。path 形如 "org/app/1.0/app-1.0.jar"（repo 内相对）。
    Get(ctx context.Context, repoKey, path string) (*Node, error)
    Put(ctx context.Context, n *Node) error        // upsert by (repo_key, path)
    Delete(ctx context.Context, repoKey, path string) error // 只删引用，不动物理 blob
    // DeleteByPrefix 目录递归删（T-12 目录删基础）。前缀查询大小写敏感（case_sensitive_like=ON，
    // 见 §6 前言），与制品 path 大小写敏感语义一致（T-10 review B1 裁决）。
    DeleteByPrefix(ctx context.Context, repoKey, prefix string) error
    ListByPrefix(ctx context.Context, repoKey, prefix string) ([]*Node, error)
    // FilterUnreferenced 流式产出 blobs 表中不被任何 node 引用的 sha256（keyset 分页回调，
    // 非 OFFSET——边删边流不跳行）。定位（T-9 review 回写项 I）：**对账/一致性检查用**
    //（如 blobs 表与磁盘盘点）；GC mark 自 T-9 集合形回调后不再走它（mark =
    // SELECT DISTINCT sha256 FROM nodes），原「供 GC mark」定位作废。
}

type Node struct {
    RepoKey   string
    Path      string
    Sha256    string // → blobs.sha256
    Size      int64
    Mime      string
    CreatedBy string // principal 名
    CreatedAt string // RFC3339 UTC
    UpdatedAt string
}
// 其余子接口（RepoStore/BlobStore/UserStore/TokenStore/AuditStore/PermissionStore——
// 命名 permission target 的 CRUD 与判定查询，PRD E-24）字段见 §6 DDL，方法为常规 CRUD，定稿以本文件 + 代码 api.go 为准。
```

### 3.3 repo.Service（owner: dev-go-core；用例编排核心）

```go
package repo

type Service interface {
    // 三型统一入口；M1 只实现 local 分支；M3 起 remote 分流 internal/remote.Fetch、
    // virtual 走成员序解析（ADR-0012/0013，接线见 §5.4 首段）。
    Get(ctx context.Context, principal *auth.Principal, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, error)
    Put(ctx context.Context, principal *auth.Principal, repoKey, path string, body io.Reader, expect storage.BlobRef, mime string) (*metadata.Node, error)
    Delete(ctx context.Context, principal *auth.Principal, repoKey, path string) error
    List(ctx context.Context, principal *auth.Principal, repoKey, prefix string) ([]*metadata.Node, error)
    // PutFromBlob 从既有 blob 引用创建 node（秒传路径，T-13 修复新增契约）：
    // ① 判权先于 blob 打开（无权者不消耗 IO）；② storage.Open 确认物理在 +
    //    blobs 台账行在，双维校验；③ 孤儿 blob（物理在、台账缺行）→ ErrOrphanBlob
    //    ——拒绝秒传把无主 blob 实体化成 node，避免该制品永久失去 sha1/md5 附属
    //    摘要（sha256-only 降级不可逆）；④ sha1/md5 只从台账行取，不重算。
    PutFromBlob(ctx context.Context, principal *auth.Principal, repoKey, path string, ref storage.BlobRef, mime string) (*metadata.Node, error)
    // CreateRepo/UpdateRepo/DeleteRepo：仓库配置 CRUD（校验 key 唯一、类型合法）。
}

// 依赖注入构造（repo 只依赖接口，不 import sqlite 实现）：
func New(st storage.Engine, md metadata.Store, az auth.Authorizer, au audit.Logger) Service
```

**Put 的事务边界**（正确性关键）：`storage.Session.Commit`（物理 blob 就位）成功之后、metadata 写 blob link + node（两条语句、blob-first，见 §3.2 事务边界注）之前崩溃 → 产生一个无引用 blob，由 GC grace 过期回收，**不损数据**；反向（先写元数据后落盘）则会出现元数据指向不存在 blob 的致命态，**禁止**（FK 兜底强制）。

**目录实体化不变量**（ADR-0016，T-119 裁决——隐式目录 404 定案）：`putNode` 写目标节点（file 或 folder）之前，先将目标路径的**每个祖先目录段**以 folder 行材料化（走 putNode 的 folder 臂 = 服务端 mkdir，与 E-15 显式 mkdir 同一路径：新行，或幂等重放刷 UpdatedAt）。三条硬规则：① 顺序 = 祖先先于目标行（与 blob-first 同构——崩溃窗口至多残留**空 folder 行**（良性，pruneEmptyParents 可收），绝不出现「文件行存在而父目录行缺失」）；② 祖先行是**派生状态**——不单独判权、不过 governance 门、不记审计（合法性继承自已通过全部门的目标写；size 0 → quota/usage delta 0）；③ 历史库由 007 迁移一次性回填（先落哨兵 blob 行满足 FK，再递归 CTE 推祖先集，INSERT OR IGNORE 幂等）。效果：`GET /api/storage/{repo}/{dir}` 与 `?list` 对任何有后代的目录 200 FolderInfo（隐式目录 404 消除），`pruneEmptyParents`（repo-semantics §4 的删除后空祖先链清除）随之从事实死代码变为活语义。remote 引擎直写 `Nodes().Put` 不材料化祖先（M4 无 remote 目录浏览面，不可观察——§11.20）。**不加读路径前缀合成 fallback**：双机制会掩盖不变量破坏（ADR-0016 决策 5）。

### 3.4 auth（owner: dev-go-core）

```go
package auth

type Principal struct { Name string; Admin bool; TokenID int64; Groups []string } // TokenID>0 表示 token 认证；Groups M4 起认证时填充（permission_principals 的 group 行消费源）

type Authenticator interface {
    // 支持三臂：Basic（user:password 或 user:token）、Bearer <token> [M2 docker]、
    // Cookie binflow_session（web_sessions 表，M4，ADR-0014 勘误②——HttpOnly/Path=/binflow/SameSite=Lax，
    // TTL console.session_ttl_hours 默认 24（seconds 覆盖键测试粒度）+ last_used 滑动续期受绝对封顶，登出 revoke）。
    // [M6+] 第四臂：Bearer <oidc_id_token>——OIDC ID Token 验证（JWKS 签名校验 + claims 提取），
    // 映射为 *Principal（Name=user_claim、Groups=group_claim、Admin=管理员映射表）。
    // LDAP 不走独立 Bearer 臂——LDAP 用户的认证在 login 端点（POST /api/v1/session）触发。
    Authenticate(ctx context.Context, r *http.Request) (*Principal, error) // nil,nil = 匿名
}

// [M6+] IdentityProvider 身份提供者接口（ADR-0020 决策 1）：
// OIDCProvider 和 LDAPProvider 实现此接口。Authenticator 的 OIDC Bearer 臂
// 与 login 端点的 LDAP bind 均消费此接口，Authorizer 不感知 provider 差异。
type IdentityProvider interface {
    // ProviderName 返回 "oidc" 或 "ldap"（对应 users.provider 列值）。
    ProviderName() string
    // Authenticate 用 provider 特有方式验证凭据。OIDC：验证 ID Token（JWKS + claims）。
    // LDAP：用用户名/密码执行 bind 验证。返回 Principal 用于后续授权。
    Authenticate(ctx context.Context, r *http.Request) (*Principal, error)
    // Resolve 按 provider_id 查找用户（OIDC sub / LDAP DN），返回 Principal。
    // 用于首次登录自动创建 users 行后的 Principal 填充。
    Resolve(ctx context.Context, providerID string) (*Principal, error)
}
type Authorizer interface {
    // M1 规则：admin 全通过；否则按命名 permission target（见 §6 permission_targets 表，PRD E-24）
    // 判定：repo 命中 repos[] 且 path 命中 includePatterns（**/* 两级通配）且不命中 excludePatterns
    //（exclude 优先）→ 按 principals 中命中该用户 **或其 Groups 任一**（M4：principal_type='group' 行，
    // membership 在认证时解析进 Principal.Groups）的 actions 授 r/w/d；无命中 = 拒绝。
    // 匿名（p == nil）：action=="r" 且 security.anonymous_access==true 时内容路径放行（ADR-0009）；
    // 写操作与管理面（/binflow/api/**）无论开关一律拒绝匿名。
    // 路由解析位置（T-14 review 终判）：repo key → repo 行查询位于授权门之后；RepoLookup 用
    // metadata.Get 是有意为之的匿名读前置缝（无权者在查询前即被拦），见 §7.1 注记。
    // folder 契约（T-11 review B-2 修复新增）：path 以尾斜杠标识 folder；Ant matchStart
    // 前缀规则仅对 folder 路径生效，文件路径必须与 pattern 全段匹配；调用方路由 folder
    // 请求时须保留尾斜杠（语义对齐 docs/reverse/auth-model.md AuthorizationServiceBase）。
    Can(ctx context.Context, p *Principal, repoKey, path, action string) bool // action: r|w|d
}
type TokenRegistry interface {
    Issue(ctx context.Context, user string, ttl time.Duration) (plaintext string, err error) // 只在签发时可见
    Verify(ctx context.Context, plaintext string) (*Principal, error)
    Revoke(ctx context.Context, tokenID int64) error
}
// token 存储：sha256(plaintext) + argon2id? 不——sha256 即可（高熵随机 32B，无字典攻击面）。
// 用户密码：argon2id（golang.org/x/crypto/argon2），标准参数（t=1,m=64MB,p=4）。
```

#### 3.4a RBAC 增量 [M7]（ADR-0026，Accepted——2026-08-23 T-214 终裁转正）

> 现状：`users.is_admin` 布尔 + permission target（r/w/d）两件套，管理面由 httpapi 的
> `routeAuth{admin: bool}` 逐路由硬门。M7 按 ADR-0026 扩展为**闭集角色 + `m` 动作**分层模型，
> 不引入角色实体表（候选对比与裁决见 ADR-0026）。

```go
package auth

// Role 是闭集系统角色（users.role 列，migration 011 回填 is_admin=1 → admin）。
// 闭集 = 代码常量，不是 DB 实体——新增角色是架构变更（新 ADR），不是数据变更。
type Role string

const (
    RoleAdmin         Role = "admin"          // 全量：内容面 + 管理面读写（≡ 现 is_admin）
    RoleReadOnlyAdmin Role = "readonly_admin" // 管理面只读 + 内容面全域只读（Q4 需求）
    RoleUser          Role = "user"           // 默认：内容面走 permission target，管理面拒绝
)

// Principal 增量：Role 随认证填充（与 Groups 同源同刻）；Admin 保留为派生便捷字段
// （Admin = Role == RoleAdmin），既有 p.Admin 消费点语义不变——readonly_admin 的
// Admin=false，管理面判定必须改走 CanManage（下方），不得继续用 p.Admin。
type Principal struct { Name string; Role Role; Admin bool; TokenID int64; Groups []string }

// ManagementCapability 管理面闭集能力（routeAuth.admin 布尔门的精确化替身）。
// 路由→能力的清点表已定稿（§7.1 [M7] 终版，T-214；T-215 逐路由迁移施工图）。
type ManagementCapability string

const (
    CapSystemRead    ManagementCapability = "system:read"    // 健康面板/存储统计/审计查询/复制状态/迁移状态
    CapSystemWrite   ManagementCapability = "system:write"   // GC 触发/迁移启动/复制配置 CRUD
    CapSecurityRead  ManagementCapability = "security:read"  // users/groups/token 列表与详情
    CapSecurityWrite ManagementCapability = "security:write" // users/groups/permission targets CRUD、角色指派、token 吊销
    CapRepoRead      ManagementCapability = "repo:read"      // 仓库配置族的全局读（列表/详情）
    CapRepoWrite     ManagementCapability = "repo:write"     // 建仓/删仓（全局写）
)

// Authorizer 增量（实现仍在 auth.Service，同 Can 一处求值，禁止第二决策点）：
CanManage(ctx, p, cap ManagementCapability) bool
CanManageRepo(ctx, p, repoKey string, write bool) bool
```

**求值链（精确语义，三面合一）**：

```text
① 内容面（既有 Can，动作集扩为 r|w|d|m）——docker token 臂与 REST 臂都终止于此，两臂一致性由本条保证：
   Can(p, repoKey, path, action):
     p == nil            → anonymous_read && action == "r"                 （ADR-0009，不变）
     p.Role == admin     → true                                        （admin 旁路，不变）
     p.Role == readonly_admin → action == "r" ? true : false             （全域只读；w/d/m 硬拒；角色短路——target 不参与 readonly_admin 求值，组合无效而非非法。T-214① 终裁，ADR-0026 决策 1）
     其余（user）        → permission target 求值（不变）：repo ∈ repos[] 且 path 命中
                           include 不命中 exclude 且 principals 行（user 或其 Groups 任一）
                           携带 can_<action> → 授予；无命中 = 拒绝
     m 动作的 target 匹配特例：只判 repos[]，includes/excludes 不参与（manage 是仓库配置
     权，无路径子域——对齐 Artifactory manage 动作语义，auth-model §4）

② 管理面（全局）——CanManage(p, cap)：
     p == nil                     → 401（认证门在前，middleware 序不变）
     p.Role == admin              → true（全部 cap）
     p.Role == readonly_admin     → cap ∈ {system:read, security:read, repo:read}
     p.Role == user               → false（自助端点不属管理面：token 自铸/改密/whoami
                                    维持 required-only，现状不变，Q11 裁决口径）

③ 管理面（仓库域）——CanManageRepo(p, repoKey, write)——单 repo 寻址的配置族路由
  （/api/repositories/{key}、/api/v1/repositories/{key} 的 GET/PUT/DELETE、quota 字段）：
     p == nil                     → 401
     p.Role == admin              → true
     p.Role == readonly_admin     → !write（读 verbs 放行、写 verbs 403）
     p.Role == user               → Can(p, repoKey, "", "m")             （仓库级 admin = m 动作）
```

**不变量（review 依据，违反 = 拒绝合并）**：
1. **无提权链**：`m` 动作不开启任何 `security:*`/`system:*` 能力——repo-admin 不能改
   permission targets（否则可自授全域 w）、不能管用户、不能改角色（角色指派是
   `security:write`，admin-only）。
2. **单决策点**：CanManage/CanManageRepo 与 Can 同在 auth.Service 实现；docker token 臂
   （scope pull→r、push→w，ADR-0010 第 5 条）与 REST 臂的判定最终都落在 Can——
   scope 签发不预检能力、逐请求判定的现状不变（readonly_admin 的 push token 在
   push 时逐请求 403，与 REST 面 403 同源）。
3. **m 不出 docker 协议面**：docker 协议无仓库配置操作，`/v2/token` 的 scope 词表维持
   pull/push/delete，永不映射 m。
4. **wire 兼容（T-214③ 终裁）**：wire 字段名 = **`adminRole`**（camelCase，对齐 Artifactory
   security wire 字段命名——rbac-model §1.2 `adminPrivileges` 同族）；值 = **`user | readonly_admin | admin`**
   （snake，与 DB 列值/代码常量同拼——kebab `read-only-admin` 否决：Artifactory wire 枚举值无
   kebab 形态、BinFlow wire 枚举惯例 snake〔rclass/grant_type〕、同拼消灭映射层）；DB 列名保持
   `role`，handler 一处映射（quotaBytes→quota_bytes 先例）。进 PUT/POST `/api/security/users/{name}`
   body 与 GET 回显 + whoami/session 回显（console 门控消费，只读）；admin-only 可写；非 admin 传入 →
   403；值 ∉ 闭集 → 400；与 `admin` 布尔冲突 → 400（admin=true ⇔ adminRole=admin）。`is_admin` 列保留为
   兼容镜像（substore 同语句维护，M8 移除）。**即时生效**：TokenRegistry.Verify 重查 users 行 role
   （与 enabled 同缝，T-208 护栏③先例）——存量 Token 的管理面权限随角色即时变化（V04）。
   OIDC/LDAP 同步（idp_sync）权威式改写 role（每次登录，refreshProviderAdmin 现状语义）：
   `admin_group` 命中 → `admin` > `readonly_group` 命中 → `readonly_admin` > 其余 → `user`；
   映射键 `oidc.<provider>.readonly_group`（组名）与 `ldap.readonly_group`（组 DN），缺省不配置
   = 行为与今日一致（ADR-0026 决策 6，T-214 定案——非「不映射」）。

**migration 011 草案**（双方言同步，§6 注记）：
```sql
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
UPDATE users SET role = 'admin' WHERE is_admin = 1;   -- 回填；is_admin 留作兼容镜像，M8 移除
ALTER TABLE permission_principals ADD COLUMN can_manage INTEGER NOT NULL DEFAULT 0;
```

### 3.5 audit.Logger（owner: dev-go-core）

```go
package audit

type Event struct {
    Time string; Actor string; Action string // deploy|delete|download|login|repo.create...
    Repo string; Path string; Detail string   // JSON 字符串
}
type Logger interface {
    Append(ctx context.Context, e Event) error // 失败记日志但不阻断业务（尽力而为）
    Query(ctx context.Context, f Filter) ([]Event, error)
}
```

---

## 4. 存储引擎设计（ADR-0006 展开）

### 4.1 磁盘布局

> **T-209 勘误（2026-08-23 回写，ADR-0006 决策 2 修订版）**：会话状态自磁盘 `sessions/<uuid>/state.json`
> 迁入元数据库 `upload_sessions` 表（migration 010）；磁盘只留数据文件，目录名 `sessions/` 改
> `uploads/`。磁盘 session 目录**不是**版本兼容承诺（ADR-0025 决策 5）；本节旧版 state.json
> 契约作废。

```
<data-dir>/                          # 默认 ./data，可配
├── blobs/
│   └── <sha256[0:2]>/<sha256>       # 内容寻址，全局去重，文件不可变
│       例: blobs/ab/ab530313...     # 无扩展名、无 sidecar
├── uploads/
│   └── <uuid>/
│       └── data                     # 追加写目标（唯一磁盘瞬态；状态在 upload_sessions 行）
├── binflow.db                       # SQLite（WAL 模式）；Postgres 形态下不存在
└── binflow.db-wal / -shm
```

**upload_sessions 行契约**（metadata.UploadSessionStore，T-209；state blob 对 metadata 层不透明，引擎自有）：

- 行 = `(id TEXT PK, state TEXT, created_at, expires_at)`；`BeginSession` 建行、`Commit/Abort` 删行、
  `ResumeSession` 读行重哈希恢复（摘要不落盘——崩溃使运行中哈希链失效，恢复即从数据文件重算）。
- offset 事实源 = `uploads/<id>/data` 文件长度（文件永不撒谎；DB 行的 state 收计数是尽力而为的
  记账，ResumeSession 以重算为准）。
- 备份面不含该表（瞬态，PurgeTransientFromSnapshot 与 web_sessions 并列清）；过期行由启动
  sweep（`ListExpired`）+ 引擎清扫回收，sweep 失败**不得**升级为删目录（T-209 review2 B1 定案）。

### 4.2 checksum 与去重语义

- **主键 sha256**（hex 小写）。sha1/md5 是附属校验：客户端提供则必须匹配（`X-Checksum-<ALGO>` 头，Artifactory 兼容名）；不提供则服务端计算并存 `blobs` 表供 Maven/PyPI 协议下载校验文件用 [M3]。
- **去重单位是 blob 而非制品**：两个仓库各放同一 jar，磁盘一份、`nodes` 两行。跨仓移动/复制制品 = 元数据行变更，零字节拷贝。
- 上传到 `Commit` 时若 `blobs/<xx>/<sha256>` 已存在：直接删除会话数据返回既有 `BlobRef`（进程内还用 per-checksum singleflight 把并发同 blob 的写收敛成一个落盘者，其余等待后读现成文件）。

### 4.3 上传会话状态机

```
created --Append(可多次)--> appending --Commit--> committed(终态, upload_sessions 行 + uploads/<id>/ 目录删除)
    │                            │
    └──Abort/过期(sweep)---------┴--> aborted(终态, 行 + 目录删除)

[M7] 续传态：appending --进程崩溃(kill -9)--> 落盘行+部分数据幸存 --ResumeSession--> appending
（干净停机 SIGTERM → Engine.Close 保留未过期会话，续传同样成立——ADR-0028 终裁，三径
kill -9/SIGTERM/compose restart 对称，「跨重启续传」不再限定异常中断）
```

落盘协议（顺序不可换）：write(data) → **fsync(data)** → rename(data → blobs/xx/sha256)（目标已存在则丢弃）→ **fsync(blobs/xx 目录)** → 元数据事务。崩溃窗口分析：任一点断电，最坏残留 = 完整但未引用的 blob（GC 回收）或 session 残渣（启动清理），**不存在半写 blob 暴露给读者**。

### 4.4 引用与 GC（M1 最小实现）

- 引用事实 = `nodes.sha256` 集合；`blobs` 行是「曾经存在」（node 全删后 blob 行保留，供 GC 反查附属摘要）。
- 运行时 `Delete` 制品只删 `nodes` 行（软删语义在物理层）。物理回收唯一入口：`binflow-server gc [--apply]`，mark（**引用集合回调**，调用方实现 `SELECT DISTINCT sha256 FROM nodes`，一次查询——T-9 集合形，取代原 FilterUnreferenced 反连接逐条路线）→ sweep（storage 扫描 `blobs/` 磁盘，未引用且**文件 mtime** 早于 `now-grace` 才删；删除后同时清 `blobs` 行）。默认 dry-run 打印清单，`--apply` 才真删（安全底线）。
- **grace 基准是 blob 文件 mtime，不是 `blobs.created_at` 列**（T-9 回写项 G；storage 不读 DB 的必然选择，方向安全——mtime 被推新只会多保留）。**硬约束：备份/恢复工具必须保留 mtime（`tar` / `rsync -a` 默认保留；勿用会重置时间戳的复制方式），否则宽限期时钟被重置**——M4 备份票与 ops 文档必须遵守。
- grace 与 session ttl 的零值语义：`<= 0` 一律取默认 24h（T-9 回写项 J）——零值不是「立即回收」；要无宽限期须显式传亚秒时长。
- 启动时清扫（T-209 修订）：`upload_sessions` 表按 `expires_at`（`ListExpired`）超 ttl（默认 24h）删行并同步删 `uploads/<id>/` 目录（目录 mtime 兜底孤儿）；未过期行保护其目录不被误杀（QA 钉死：kill -9 后行 + 部分数据幸存）。sweep 失败仅使本次 Open 失败重试，**不得**删除 uploads/ 下任何内容（T-209 review2 B1）。
- `FilterUnreferenced`（metadata）自 GC 链路退役，转为对账/一致性检查用途（见 §3.2 注释，回写项 I）。

### 4.5 remote 代理缓存存储面（M3 增量，ADR-0012）

- **落盘协议与本地完全同源**：上游 miss 响应体流式走 `storage.BeginSession → Append → Commit`（sha256 由服务端自算记账；上游若给 digest/校验头则 `Commit(expect)` 强校验，不符即弃——上游投毒防线）。缓存 node 落在 remote 仓自身的 repo_key 下（BinFlow 不采用 Artifactory 的 `<remoteKey>-cache` 影子仓——那是其存储分片的历史包袱，我们的 nodes 表直接承载，语义等价、少一层间接；差异记 §10 对齐表）。
- **缓存状态 = 003 新表 `remote_cache`**（验证器元数据 etag/last_modified/fetched_at/expires_at，per repo_key+path），nodes/blobs 不加列（保持本地制品面零污染）。
- **分流**：artifact（制品路径，layout 判定）默认长 TTL、checksum 命中永不再验（不可变原则）；metadata（maven-metadata.xml / npm packument / simple index HTML）默认短 TTL（独立列 content_ttl vs metadata_ttl），过期走条件再验证，304 刷新时钟。
- **上游 original checksum「登记不拒」（PRD v1.2 定案，T-66 review 裁决 4 回写）**：上游响应的 `X-Checksum-*` 头读为 original checksum，与实测值比对后 **M3 只 WARN 登记、不拒收**（PRD「四值策略 M4」优先于架构草案的「强校验不符即弃」）。M4 落地注记：repo-semantics §7.5 高置信度默认策略 `generate-if-absent` 是**拒收**语义，四值策略票必须实现拒收分支，当前 WARN 分支即挂接点；M3 的 WARN 未把 original 值入库，M4 的 original 登记无 M3 历史数据（可接受，M4 票补）。
- **上游故障降级（T-79 勘误一定案口径，取代本节初版 stale-while-error 措辞）**：上游 5xx/超时/连接失败 → 仓标记 **assumed-offline**（静默 `assumedOfflinePeriodSecs` 默认 300s，期内零上游流量）+ 有缓存（**含过期**）→ 服务缓存附 `X-Binflow-Upstream-Error: <摘要>` 头；无缓存 → **404**（E-01，message 含 offline/assumed offline 状态）；仅 `hardFail: true`（默认 false）→ 502。上游 404 → 写负缓存（missedRetrievalCachePeriodSecs 1800）+ 有过期副本仍回发（"expired but serving"）。
- **`X-BinFlow-Cache: HIT|MISS|REVALIDATED|STALE` 响应头**（QA 断言锚点，内容路径响应统一附加）。
- **GC**：缓存 node 与本地 node 同为引用事实，无特判——删 remote 仓级联删 nodes，blob 由 GC 统一回收。virtual 探索性 miss **不落盘**（ADR-0013，防成员扫描污染缓存）。

### 4.6 配额 enforcement（M4 增量，ADR-0015；T-112 勘误：键名与 remote 计量口径对齐 PRD §7 Q2/实现）

- **enforcement 点在 `repo.Service.Put` 链**（PutFromBlob/PutLandedBlob/PutOpts 同链）：`repositories.config` 的 **`quotaBytes`**（0=不限，默认；键名勘误 T-112：原误写 `quota_bytes`——实现/PRD/REST 均为 camelCase，T-95 review NB4）+ `repo_usage` 计数行（logical_bytes，与 node 增删**同一事务**维护——SQLite 单写者无热行竞争放大）。
- 预检时序：expect.Size 已知（秒传/mount/docker finalize）→ Commit 前直判；流式 size 未知 → 落盘后判，超限**回滚 node 登记但 blob 留待 GC**（不拒已落盘字节，只拒登记），响应 413 + QUOTA_EXCEEDED（码值 PRD 定）。
- **口径 = 逻辑字节**（nodes.size 之和）：配额按仓计量，跨仓共享 blob 的物理归属无法公平切分；物理占用走既有 `/api/v1/storage/stats`。**remote 缓存 node 不计量**（口径勘误 T-112：原「计入 + `quota_include_cache` 豁免位 [M5+]」与 PRD §7 Q2「pull-through 落盘不计量」相抵，按 Q2 执行——engine 的 cache node 写不走计量，T-95；既有 remote nodes 已由 005 回填一次性计入，快照语义不回滚；M5+ 若需计量再按「计入开关」重开）。

### 4.7 S3 对象存储后端（M6 增量，ADR-0019）

> **设计原则**：S3 后端与 disk 后端共享同一个 `Engine` 接口（§3.1），实现差异封装在 `Backend` 内部接口（§3.1a）。调用方（adapter、repo.Service）不感知后端类型，仅通过 `Engine` 的门面操作。

**blob 布局**：`<bucket>/<prefix>/blobs/<xx>/<sha256>`（xx = sha256 前两字符，与 disk 布局同构）。S3 后端无 `uploads/` 瞬态目录——上传会话 = multipart upload，中间态由 S3 服务端持有（见下方 multipart 模型；`upload_sessions` 表仅 disk 后端使用，T-209 口径）。

**S3Engine 与 DiskEngine 的核心差异**：

| 维度 | DiskEngine（M1-M5） | S3Engine（M6） |
|---|---|---|
| blob 寻址 | 本地文件系统 `blobs/<xx>/<sha256>` | S3 key `<prefix>/blobs/<xx>/<sha256>` |
| 上传会话 | `uploads/<uuid>/data` + `upload_sessions` 行（T-209）+ rename 原子落盘；`ResumeSession` 重哈希恢复 | **S3 multipart upload**（单次 5GB+ blob 分块上传，S3 服务端管理中间态；ResumeSession 恒 ErrSessionNotFound） |
| Open 返回类型 | 返回 `*os.File`（可 Seek，类型断言可用） | 返回 `io.ReadCloser`（S3 GetObject body，不可 Seek） |
| GC sweep | 本地 `filepath.Walk` 扫 `blobs/` 目录 | **S3 ListObjectsV2** 分页扫 key 前缀，结合 metadata 引用集合判定 |
| 并发安全 | rename 原子性 + fsync 保证 | S3 PUT 是原子操作（单对象），无 partial write 暴露 |
| 备份/恢复 | tar 目录树 + 保 mtime（§7.6） | 备份/恢复 [M7+]——S3 对象版本控制 + 跨桶复制为非默认高级功能 |

**S3 multipart 上传会话模型**（替换 disk 的 `sessions/<uuid>/` 目录）：

1. **BeginSession** → 调 S3 `CreateMultipartUpload`，返回 `UploadID`，会话状态存内存 map（`map[uuid]s3SessionState`，重启丢失 = 会话废弃，S3 侧残留 multipart upload 由定期 `AbortMultipartUpload` 清扫——配置项 `storage.s3.incomplete_upload_cleanup_hours` 默认 24h）。
2. **Append** → 调 S3 `UploadPart`（partNumber 递增，一次 Append 一个 part）。**单 part 上限 5GiB**（S3 限制），超大 blob 分段由 adapter 层控制（M6 的 Docker/Maven adapter 无需改——其 blob 远小于 5GiB）。
3. **Commit** → 调 S3 `CompleteMultipartUpload`（提交所有 part）→ 写 `blobs` 台账行。若 Commit 失败（part 列表不完整/ETag 失配），S3 返回错误，会话标记为失败，残留 multipart upload 由定期清扫回收。
4. **Abort/过期** → 调 S3 `AbortMultipartUpload`（即时释放 S3 侧中间态存储）。

**S3 后端对 GC 的影响**（ADR-0019 决策 5）：

- **mark 阶段不变**：引用集合 = `SELECT DISTINCT sha256 FROM nodes UNION SELECT DISTINCT blob_digest FROM docker_refs`（与 disk 后端同集合）。
- **sweep 阶段变体**：disk 用 `filepath.Walk` 扫 `blobs/` 目录；S3 用 `ListObjectsV2` 分页扫 `blobs/` 前缀，逐个 key 提取 sha256 → 不在引用集合中 → **且** GC grace 期（`gc_grace_hours`）比对 S3 `LastModified`（等价于 disk 的 mtime 语义）→ `DeleteObject`。
- **性能**：S3 ListObjects 每次 1000 条，百万 blob 需约 1000 次 API 调用（费用约 $0.005/千次，即 $5/百万 blob 扫描——M6 文档标注）。

**S3 配置段**（§8 详细 schema）：

```yaml
storage:
  backend: "disk"          # 'disk' | 's3'（M6 默认 disk，向后兼容）
  data_dir: "./data"       # disk 后端的数据目录（s3 后端时忽略）
  s3:
    bucket: ""
    prefix: ""             # 对象 key 前缀，多实例共享桶时使用
    region: "us-east-1"
    endpoint: ""           # 兼容 S3 协议的对象存储（MinIO/Ceph/阿里云 OSS），空=使用 AWS 默认 endpoint
    access_key_id: ""      # 走 env BINFLOW_STORAGE__S3__ACCESS_KEY_ID（秘密不入 YAML）
    secret_access_key: ""  # 走 env BINFLOW_STORAGE__S3__SECRET_ACCESS_KEY（秘密不入 YAML）
    use_path_style: false  # MinIO 等需要 path-style 寻址
    force_path_style: false
    max_retries: 3
    upload_part_size_mb: 64
    incomplete_upload_cleanup_hours: 24
```

**S3 后端已知限制**（§11 M6 技术债登记）：

1. **Open 不返回 Seek**：`Engine.Open` 返回 `io.ReadCloser`（§3.1 M6 变更），docker blob GET with Range 需在 adapter 层通过 S3 `Range` 头直取部分对象，而非 Go 层 Seek。
2. **单实例共享桶时的锁**：M6 不实现跨实例的 blob 写锁（disk 的 singleflight 在进程内有效，S3 侧无分布式锁——同 blob 并发写最后一个 CompleteMultipartUpload 获胜，S3 LastModified 更新）。
3. **备份/恢复**：S3 后端的 backup/restore 不在 M6 范围（disk 后端已有 §7.6 的 CLI export/import），M7+ 再议 S3 版本控制或跨区域复制方案。

---

## 5. 适配器 SPI（owner: dev-registry-adapter，M1 = Generic）

### 5.1 接口契约

```go
package adapter

// Handler 一个协议（如 generic、docker）的全部 HTTP 行为。
type Handler interface {
    // Protocol 用于路由前缀与文档；如 "generic"、"docker"。
    Protocol() string
    // RepoTypes 是声明性元数据（T-33 review 裁定，T-48 落）：声明本协议可服务的仓库 class，
    // 供校验/文档/未来 class 维度能力使用；不得作为任何分发 map 的键。
    // 声明与行为必须一致（T-66 review blocking-1：generic 服务 remote 仓后 RepoTypes 须同步升级，
    // 否则元数据说谎）：generic {"local","remote"}（virtual 待 T-71）；docker {"local"}；
    // maven/npm/pypi M3 起按各自内容面实际服务的 class 声明。
    RepoTypes() []string
    // Layout 把请求路径切为 (repoKey, repoRelPath)；httpapi 已剥离 /binflow 前缀。
    // generic: 首段=repoKey，余下=repoRelPath。
    // docker [M2]: 见 §5.3——挂根级例外 /v2（ADR-0010），name 首段=repoKey；错误契约两态化
    //（语法错 wrap ErrBadRequestPath→400；不可寻址形状→404 形，§5.1 分发键约束段）。
    Layout(r *http.Request) (repoKey, relPath string, err error)
    // ServeHTTP 业务本体：中间件已在 httpapi 完成 auth+audit 前置；handler 内调 repo.Service。
    http.Handler
}

// 注册机制：各协议包 init() 调 Register；httpapi 启动时 Mount 全部。
// byType 键仅 package type（≡ Protocol()）；重复 package type panic（启动期暴露）；
// 空 RepoTypes panic 保留（空声明=装配 bug）；class 不是键，generic 与 docker 同声明
// class=local 是合法状态（T-33 裁定，T-48 落）。
func Register(h Handler)          // 空 RepoTypes / 重复 package type panic
func All() []Handler
```

**挂载与分发（ADR-0008 + ADR-0010）**：所有产品端点统一挂 `/binflow` 前缀；**例外：`/v2/**` 为 docker 协议硬编码根级例外**（客户端不可配置前缀，性质同 `/healthz` 的「客户端协议豁免类」，进同一 middleware 链但不剥前缀）。httpapi 剥离前缀后按首段分发：
- `/v2/**` → docker adapter（根级例外路由，不剥前缀，见 §5.3）；
- 首段 ∈ 保留段（`api`）→ REST 路由；
- 否则首段视为 repo key → 查 repositories 表 → 按 `package_type` 分发到对应 adapter.Handler（M1 generic，M2 增 docker）。

因此 repo key 保留名校验：建仓时拒绝 `api`、`v2`（repo.Service 校验，`/binflow/v2` 双挂载**不提供**——ADR-0010 裁决）；M4 起并集为 **{api, v2, docs, console, ui, assets}**（ADR-0008 增补/T-108+T-110；docs/console/ui=ADR-0011/0014 挂载段、assets=SPA 指纹资产前缀遮蔽，见 §7.1；存量同名仓启动 WARN、路由仍占用）。

**依赖方向（T-48 依 T-38 review N1 勘误）**：`adapter/*` → `repo.Service` + `auth`（读 Principal）；**禁止**直接 import `storage`/`metadata`，例外两条（其余「需要流式细节时经 `repo.Service` 扩方法，不得绕过」维持）：
- **例外一（§5.3 裁定第 1 条，docker blob upload）**：上传端点族（POST/PATCH/PUT/GET/DELETE `/v2/<name>/blobs/uploads*`）直持 `storage.Engine` 驱动 Session 生命周期（BeginSession/Append/Commit/Abort）——协议态（received、UUID 配对）在 adapter，storage 不感知协议头；该例外仅限上传会话对接，blob 读路径仍经 `repo.Service.Get`。metadata 触点同构先例：路由数据只读查询（RepoLookup 用 `metadata.Get`，T-14 终判的匿名读前置缝，docker 包 repolookup 同构）。
- **例外二（先例 T-13 review B1 终判）**：digest 台账的 **READ-only** 查询经 consumer-side `BlobLedger` 接口注入（generic 与 docker 同构同用途：sha1/md5 头族 + mount 响应），类型经 repo.Service 签名同源，消费端接口注入，不摸内部结构。

新协议接入 = 新增子包 + Register，**零改动** httpapi/repo 核心。

**分发 map 键约束（T-33 裁定，T-48 落）**：httpapi adapters map 的键**仅** package type（≡ `Protocol()`）；class 键（"local"/"remote"/"virtual"）是零读取死键且为唯一碰撞源，`New()` 不写、后续不得 reintroduce。分发语义不变：`dispatchContent` 只按 `row.PackageType` 查表。**Layout 错误契约两态化**：客户端语法错误（dot-segment/坏编码/超长）wrap `ErrBadRequestPath`（400）；不可寻址形状（单段 name/无路由尾）允许返回 404 形错误。

### 5.2 Generic（参考实现，M1）

| 方法+路径（均含 `/binflow` 前缀） | 语义 |
|---|---|
| `PUT /binflow/<repo>/<path...>` | 上传；可选 `X-Checksum-Sha256/-Sha1/-Md5` 校验；`Content-Type` 存 mime；成功 201 |
| `GET /binflow/<repo>/<path...>` | 下载；带 `X-Checksum-Sha256` 响应头；404 信封 |
| `DELETE /binflow/<repo>/<path...>` | 删 node 引用；200 |
| `HEAD` | 同 GET 但无 body |
| `GET /binflow/api/v1/repositories/<repo>/_list?prefix=` | 前缀列表（自有 API，控制台/CLI 用） |

目录语义：Generic 无 layout 解析，`path` 原样存储（Artifactory 兼容）。「是否提供 `.../list` 目录 HTML」**待逆向规格确认**，M1 只给 JSON 列表。

### 5.3 Docker Registry v2 adapter（M2 增量，ADR-0010；依官方 Docker Registry HTTP API V2 规范设计，行为细节待 docs/reverse/docker-registry.md（T-31）校准）

**路由与 name 映射**：挂根级例外 `/v2/**`（不剥 `/binflow`）。name（`/v2/<name>/...`）按 `/` 切分：首段 = repo key，余段 = 镜像相对名（如 `/v2/team1/app/manifests/latest` → repoKey=`team1`、image=`app`）。单段 name（无 `/`）→ 404（待 T-31 校准 Artifactory 行为）。

**端点 → 内部面映射表**：

| Registry 端点 | 方法 | 内部映射 |
|---|---|---|
| `/v2/` | GET | ping：认证模式探测（匿名 → 401 + `Www-Authenticate: Bearer realm=<base_url>/v2/token,service=binflow`，见 token 流） |
| `/v2/<name>/blobs/uploads/` | POST | `storage.BeginSession` → 202 + `Location: /v2/<name>/blobs/uploads/<session-id>`（Location 用根级路径，docker 客户端按响应头回访）；`Docker-Upload-UUID` 头 = session ID |
| 同上 `?_method=HEAD` 或 PATCH/PUT 前探测 | HEAD | `Session` 当前 offset（`received`）→ 204 + `Range: 0-<received-1>` |
| `/v2/<name>/blobs/uploads/<id>` | PATCH | **流式 Append**：`Content-Range` 若给出必须 == 当前 `received`（失配 416 + `Range` 头；这是 chunked offset 语义与 Append 流式语义的对接点——Append 只接受严格追加，Range 校验由 adapter 做，storage 不感知协议头）→ 202 + `Location` + `Range` |
| 同上 | PUT | finalize：query `digest=<sha256:...>` 必须与 Append 累计摘要一致（`Session.Commit(expect)`，失配 → 400 Blob Invalid）→ blob 落盘 + `blobs` 台账行 + **manifest/layer 挂账（见 mediaType 链）**；monolithic 单发 PUT（body 直接带内容）= BeginSession + Append + Commit 一气呵成；`?digest` 缺失 → 400 |
| `/v2/<name>/blobs/<digest>` | GET/HEAD | digest → `repo.Service.Get`（path 为 image 相对名下的 blob 寻址由 adapter 翻译：digest 直查 `nodes` 的 docker 布局行）；支持 Range（M1 T-20 已备 `ReadSeekCloser`） |
| 同上 | DELETE | blob 删引用（仅当无 manifest 引用时物理回收交 GC） |
| `/v2/<name>/manifests/<ref>` | PUT | manifest = **JSON blob**：按普通 blob 走 BeginSession/Commit（digest = sha256 of body）+ `manifests` 表行（mediaType、size、digest）；引用的 config/layer digest 必须**先已存在**（mount 或已上传），否则 404/400（Manifest Invalid）——引用完整性前置校验，防悬空 manifest |
| 同上 | GET/HEAD | 按 ref 解析：`sha256:` 前缀 → digest 直查；否则 tag → `docker_tags` 查 digest → manifest blob；响应 `Content-Type` = manifest 的 mediaType；`Docker-Content-Digest` 头必带 |
| 同上 | DELETE | tag 删除（ref 是 tag）或 manifest 删除（ref 是 digest，连带其 tags） |
| `/v2/<name>/tags/list` | GET | `docker_tags` 按 repo+name 前缀查询；分页 `n`/`last`（name 排序，官方分页语义） |
| `/v2/_catalog` | GET | `repositories` 表 package_type='docker' 的 key 列表 + `n`/`last` 分页；**管理面语义**（需认证，匿名 401——对齐 spec 与安全直觉，待 T-31 校准 Artifactory 是否放宽） |
| `/v2/token` | GET | Bearer 签发（ADR-0010 第 4 条），Basic 凭据 + `service`/`scope` 参数 → `{"token","expires_in"}`（附 `access_token` 同值兼容字段） |

**blob upload 与 storage.Session 的语义对接**（三条关键裁定；裁定 1 的直持 storage.Engine 是 §5.1 例外一，T-48 勘误交叉引用）：
1. **offset 语义 vs 流式**：Registry chunked 协议是「断点对齐追加」（客户端持 offset、服务端必须可查询与续传），storage.Session 是「黑盒流式追加」。对接法：adapter 持有协议状态（`received` 计数），PATCH 时先校验 `Content-Range` 与 `received` 对齐再调 `Append`；PUT finalize 用 `Commit(expect)` 一次性收口。~~M1 的 ResumeSession 仍恒 ErrSessionNotFound~~（**T-209 已作废**：ResumeSession 自 migration 010 起真实可用；[M7] REST 重暴露路径见 §5.3.1——`received` 的权威源随之上移至引擎会话状态）。
2. **digest 算法**：Registry digest 形如 `sha256:<hex>`；BinFlow blob 主键就是裸 hex sha256——adapter 只剥 `sha256:` 前缀，无算法转换（spec 允许其他算法，M2 只实现 sha256，收到 `sha512:` 等 → 400 Unsupported）。
3. **cross-repo blob mount**（`?mount=<digest>&from=<repo>`）：POST uploads 带 mount 参数且目标 blob 已被 from-repo 引用 → 直接走 `repo.Service.PutFromBlob` 零拷贝挂账（M1 已备该契约）；mount 失败按 spec 降级为普通上传会话（202 + Location，不报错）。

**manifest 引用完整性（mediaType 校验链；链①经 T-51 正式消歧——R3 终审口径，取代本节初版白名单）**：PUT manifest 时逐条校验——
① **Content-Type 透传存储（非白名单）+ 结构性验证**：四种已知类型（docker manifest v2 / manifest list v2 / OCI image manifest / OCI index）走专形解析；**未知 CT 不拒收**，按 body 自身形状判读——`manifests[]` → index 语义、`config` → image 语义、**两者兼有时按 index 判读**（判读优先序，manifest.go:544 现状钉死；OCI 1.1 无此合法形态，属 edge case 防御）、皆无 → 400 MANIFEST_INVALID。透传对象 = 裸 media type（剥 `;` 参数后存储，参数非 media type 语义）。**唯一显式拒收的 CT 家族 = schema1 两种**（`application/vnd.docker.distribution.manifest.v1+json` / `+prettyjws`）→ 400；缺 CT 拒收（无值可透传）。不裁回白名单的理由（精简）：cosign 签名 manifest（`application/vnd.dev.cosign.simplesigning.v1+json`）、旧版 oras artifact manifest 等生态类型今天就真实在推，白名单直接挡掉（Helm 本体 manifest CT 是标准 OCI，吃透传的是 cosign/oras 一类）；结构判读已接管白名单的防「任意 JSON 假 manifest」职责，白名单无增量安全收益（T-39 review §五）。
② 逐 config/layer digest 查 `nodes`（本 repo 内）已存在，缺一即拒（防悬空引用——manifest blob 落盘先于校验失败则成为无引用 blob，GC 兜底，不损数据）；未知 CT 透传类型同样走②③（结构判读使校验链对透传类型全部生效）。
③ list/index 的嵌套 manifest digest 递归同校验（只查在场性，不递归解析其内部——spec 允许 lazy）。

**token 认证流（ADR-0010 第 4/5 条，复用 TokenRegistry 不平行一套）**：
```
docker login <host>
  → GET /v2/                                   (匿名)
  ← 401 + Www-Authenticate: Bearer realm="https://<host>/v2/token",service="binflow"
  → GET /v2/token?service=binflow&scope=...    (Basic 凭据)
  ← 200 {"token":"<jwt>","expires_in":3600}
  → 后续请求 Authorization: Bearer <jwt>
```
签发 = `TokenRegistry.Issue`（有限 TTL）+ scope 编码进 token 声明；Verify 解出 Principal + scope，`pull`→`r`、`push`→`w`、manifest DELETE→`d` 映射进 `Authorizer.Can`。无 Bearer 头时回退 Basic 直连（匿名读开启时 pull 匿名放行——与 §7.1 认证分层一致）。

#### 5.3.1 blob upload 会话的 REST 重暴露——重启续传 [M7]（N6/O-2 收口）

> 现状：`sessionRegistry`（`internal/adapter/docker/uploads.go`）是进程内存表
> UUID → liveUpload；重启后 lookup miss → 404 `BLOB_UPLOAD_UNKNOWN`，客户端从零重传。
> 引擎级 `ResumeSession` 已就位并被单测钉死（T-209），但无生产调用方——本节接通
> 「DB 行 → HTTP 会话」的最后一公里。REST 重暴露方向由 ADR-0006 决策 2（修订版）+
> ADR-0025 决策 5 裁定；**Close 语义有 ADR**（[M7] ADR-0028，T-214 终裁——见契约 7）；
> 其余为实现契约。

**决策：adapter 层 lazy 重建（lookup miss → 引擎重物化），不做启动预载**。

```text
registry.resolve(ctx, id):                        # GET/HEAD(offset)/PATCH/PUT/DELETE 五动词统一入口
    up, ok := lookup(id); if ok → return up       # 进程内快路径（现状不变）
    # —— 慢路径 [M7]：在 registry 锁内 double-check + 单飞 funnel ——
    # （engine 侧同 id 并发 ResumeSession 是 last-writer-wins、被顶替句柄后续
    #  Append 报 already finalized——不 funnel 会把该错误漏成 500）
    sess, err := engine.ResumeSession(ctx, id)
    errors.Is(err, storage.ErrSessionNotFound) → return miss          # → 404 BLOB_UPLOAD_UNKNOWN（S3 后端恒走此臂）
    err != nil                                   → 500 + log（存储故障，不毒化注册表）
    up = &liveUpload{sess: sess, received: sess.Offset()}             # 权威 offset = 重哈希后的数据文件长度
    register(id, up); return up
```

**关键契约**：
1. **offset 权威性转移**：`liveUpload.received` 从「adapter 内存镜像」降级为缓存，
   事实源 = 引擎会话状态（`Session.Offset()`，§3.1 [M7] 新增）。Content-Range 对齐
   检查与 416 + 权威 `Range` 响应语义不变（QA 已钉死 416 行为，属回归面）。
2. **重建后五动词齐备**：PUT finalize 与 DELETE cancel 重建后立即执行——客户端重启后
   直接 `PUT ?digest=` 收口或 DELETE 弃置都是 distribution spec 合法流。
3. **过期 = 未知**：ResumeSession 对「已过期未清扫行」必须按 `ErrSessionNotFound` 处理
   （fail-closed；sweep 是行的唯一回收路径）。若实现复核发现引擎未查过期，属实现票必改项。
4. **无 repo 绑定校验**（与进程内行为一致）：会话 id 是不可猜测的 capability
   （uuid），URL 的 `<name>` 段只决定授权门，不参与会话寻址——重建路径不引入新面。
5. **S3 后端契约不变**：ResumeSession 恒 `ErrSessionNotFound` → resolve 直落 404
   （`TestS3ResumeSessionNotSupported` 钉死）。S3 的 REST 续传 [M8+] 评估——需把
   multipart upload ID 落 `upload_sessions` 表并实现 S3 ResumeSession，M7 不做（§11.31）。
6. **与 upload_sessions 表的关系**：表是引擎级会话台账（T-209）；本节零 schema 变更、
   零 metadata 触点（adapter 只调 engine，§5.1 例外一范围内）。备份不含该表（瞬态）
   不变——重启恢复的是无备份瞬态，续传失败最坏 = 客户端重传，与今日行为一致。
7. **O-1 语义终裁（[M7] ADR-0028，T-214——推翻本节原「维持现状」裁决）**：干净停机
   （SIGTERM/compose restart → `Engine.Close`）**保留未过期会话**（行 + 数据文件），
   三径 kill -9/SIGTERM/compose restart 对称可续传，「跨重启续传」不再限定异常中断。
   理由：T-209 会话 DB 化后行的回收已由「启动 sweep + TTL」独立承担，Close 清册降级为
   冗余双保险，且形成「干净路径劣于崩溃路径」的对称性倒挂；续传的主价值场景（升级重启/
   维护窗口，PRD 场景 C）正落在干净停机。Close 收口动作 = 打一条保留清单 INFO（未过期
   会话计数 + id，截断）；孤儿回收唯一路径 = sweep + TTL（单一回收路径不变量）；
   export/backup「瞬态不落盘」不变量不变（它约束的是备份产物内容，不是 Close 行为）。
8. **协议范围**：仅 docker（唯一 chunked 会话协议）。generic/maven/npm/pypi 是单请求
   上传，无再暴露面；未来协议引入 chunked（OCI chunked blob 等）复用 resolve 模式
   （缝在 adapter，核心零改动）。

### 5.4 M3 协议适配器与 remote/virtual（三节增量；行为细节以 M3 PRD（T-57）为准，本文定结构）

**remote/virtual 的服务层接线**：`repo.Service.Get` 内分流——repo.type=remote → 调 `internal/remote.Fetch`（缓存判定→命中直出/过期再验证/miss 回源，ADR-0012 勘误一/二口径）；repo.type=virtual → 成员序解析（ADR-0013 联动记录：**两桶序**——priorityResolution 优先桶在前、桶内声明序；stale 命中即成员结果、真 404 续桶；首命中返回 + `X-BinFlow-Resolved-From` 头，探索性 miss 不落盘）。adapter 对三型仓库无感知差异——协议差异全部在 adapter、仓库类型差异全部在 service 层（docker 的 remote/virtual 同样走此缝）。

**服务层渲染缝（T-66 落地、T-83 补记，T-67/69/70/71/72 复用勿另开缝）**：
- `repo.StatusError`（包级，api.go SPI 段）：承载仓库类语义的精确客户端渲染——`Code/Message/Header` + `Unwrap` 哨兵（如 405 + `Allow: GET`、virtual C5 定案文案）；内容 adapter 的错误渲染入口对其**原样渲染**（状态码、message、头随之）。解决「冻结的 Service 签名无法把渲染语义送达 HTTP 面」——不改签名、不在 httpapi 拦截（后者破坏 adapter 错误信封归属）。
- `ExtraHeaders() http.Header`（结构化探测，零 import、零仓库类知识）：`Service.Get` 返回的 reader 可实现该接口，adapter 以类型断言探测并把头并进响应（remote 的 `X-BinFlow-Cache` / `X-Binflow-Upstream-Error`、T-71 的 `X-BinFlow-Resolved-From` 同缝）。建议（T-66 review non-blocking）：四协议复制断言+拷贝处提升为 `internal/adapter` 基座助手 `CopyExtraHeaders(w, rc)`，防漂移。
- httpapi 的 `writeServiceError`/`writeStorageError` 需含同一 `*StatusError` 分支（T-66 review 范围外缺口：REST 面 PUT/DELETE remote 仓的 405+Allow / 204 映射，与 generic 内容面同构）——归 T-80 或小票补齐。

**SPI 豁免入口（T-67 遗留②最终契约，T-68 已授权方向，T-83 定稿措辞）**：`repo.Service` 的 SPI 面新增可选写参数——`PutOptions{ SkipOverwriteCheck bool }`，经 `PutOpts(ctx, p, repoKey, path, body, expect, mime, opts)` 进入（既有 `Put` 签名不变、等价 `opts{}` 零值）。语义：**只跳过「覆盖写需对旧 node 有 delete 权限」的检查**，读门（r 权限）、路径校验、checksum 链、幂等同字节豁免全部不变；豁免判定**仅限服务端自有写入**——maven-metadata.xml 计算器（T-68）与旁车落盘（repo-semantics §3「metadata/旁车永不触发覆盖检查」高置信度）两类调用方，adapter 直传的客户端 PUT **不得**置位（godoc 明示「for server-side computed artifacts only; client puts must not set」）。安全边界：豁免仍要求 principal 有该仓 `w` 权限（写门不豁免），仅绕过对既有内容的 `d` 权限要求——服务端计算的 metadata 是写操作的伴生产物，其可写性由触发它的主制品写权限担保。

**元数据抽取注册表（对齐 OSS MetadataProvider 骨架，oss-structure §4）**：`internal/adapter/<proto>` 各自带一个 `MetadataProvider`（layout 解析 + 该协议的 metadata/artifact 分流判定 + 版本比较器接口），在 adapter 包注册——service 层的 remote 缓存 TTL 分流与 virtual 版本择优（M4+）消费它。M3 落地时 `repo/api.go` 拆「公开用例面 / adapter SPI 面」两段（OSS papi/capi 同构，防 adapter 摸内部）。

#### 5.4.1 Maven（`internal/adapter/maven`，挂 `/binflow/<repo>/<path>` 内容路径）

| 端点形态（Maven 2 布局） | 映射 |
|---|---|
| `GET/PUT/HEAD <repo>/<groupPath>/<artifact>/<version>/<artifactId>-<version>.<ext>` | artifact：直通 node（GET 走 remote 缓存分流）；PUT 落盘 + 触发 metadata 更新（下条） |
| `GET/PUT .../maven-metadata.xml`（+ `.sha1/.md5/.sha256` 校验文件） | **生成合并**：不存储主文档——按 `MetadataProvider` 解析 GAV，聚合本仓 versions（nodes 查询）∪ remote 成员缓存版本（virtual），**按需生成** XML + 摘要文件；remote 仓的 maven-metadata.xml 本身按 metadata TTL 缓存。PUT maven-metadata.xml（客户端 deploy 时的本地版本）接受并合并入生成源（Artifactory 语义：mvn deploy 只推 version 文件，metadata 服务端算） |
| layout 解析 | Maven2 默认布局正则（oss-structure §4 RepoLayout 骨架）：`{orgPath}/{module}/{baseRev}/{module}-{baseRev}.{ext}` + classifier/目录 marker；M3 只实现 maven-2-default，自定义 RepoLayout 不做 |

存储面：无专属表——version 事实 = nodes 的 artifact 路径族（GAV 解析后聚合查询）；checksum 文件（`.sha1` 等）按需生成或随 PUT 存储（PRD 定，倾向按需生成——不可变制品的摘要可重算）。

#### 5.4.2 npm（`internal/adapter/npm`，挂 `/binflow/<repo>/...`，registry 协议路径）

| 端点形态 | 映射 |
|---|---|
| `GET <repo>/<pkg>` | packument（完整 metadata JSON document，含 `versions{}`、`dist-tags`）：**存储为主 + 增量合并**——packument 本体作为 node 存储（path = `<pkg>/packument.json`，不暴露直读），publish 时合并新 version、unpublish 删 version、dist-tag 原子更新 |
| `GET <repo>/<pkg>/-/<file>.tgz` | tarball：普通 blob node（sha256/sha1 由 npm dist.integrity 用 base64 sha512？——M3 只接 sha256/sha1 tarball，sha512 tarball 落盘记账但 integrity 校验按 PRD 定） |
| `PUT <repo>/<pkg>` | publish（document + 内嵌 data 为 tarball base64）：解析→tarball 走 storage 落盘→packument 合并（单事务）；重复 version → 403/409（npm 语义 forbids overwrite，PRD 定码） |
| dist-tags 端点族（`/-/package/<pkg>/dist-tags` 等） | 操作 packument 的 dist-tags 段 |

schema 面：npm document 字段（name/versions[].dist.tarball+integrity/dist-tags）按 npm registry 官方 schema；BinFlow 不做全文校验，只解析 publish 必需字段（name/version/dist），其余透传存储（未知字段保留——客户端兼容）。

#### 5.4.3 PyPI（`internal/adapter/pypi`，PEP 503 simple + twine upload）

| 端点形态 | 映射 |
|---|---|
| `GET <repo>/simple/` | 根 index（项目名列表 HTML）：按需生成（nodes 的 pypi 布局前缀聚合 + PEP 503 名归一化 `-_.` 折叠） |
| `GET <repo>/simple/<normalized-name>/` | 项目页 HTML（文件列表 + `#sha256=` fragment + `data-requires-python`）：**按需生成**，不存储——文件事实 = nodes（`<norm-name>/<filename>` 布局） |
| `POST <repo>/`（twine multipart upload） | 解析 form（`:action=file_upload` / name/version/filename/content）→ storage 落盘 + node 建行；重复 filename → 400（PyPI 禁止重传，PRD 定码） |
| pip 客户端解析流 | `pip install --index-url https://host/binflow/<repo>/simple`（PRD 文档面） |

实现形态：simple index 是**生成器**（HTML 模板 + nodes 聚合查询），remote 缓存按 metadata TTL 分流（上游 simple 页过期再验证）；twine 的 hash 算法段（sha256/blake2b 等）按 PRD 收敛到 sha256 主键（客户端给非 sha256 摘要时服务端自算 sha256 记账、不强校验其算法——与 §4.2 附属校验语义一致）。

---

## 6. 元数据 Schema（SQLite DDL，ADR-0003/0007 展开）

> 时间戳统一 RFC3339 UTC 文本；布尔一律 **INTEGER 0/1**（T-10 review M10 勘误：原文「布尔用 INTEGER」与 remote_configs.unreachable_mask 的 BOOLEAN 声明自相矛盾——SQLite 中 BOOLEAN 仅是 NUMERIC 亲和、Postgres 中 BOOLEAN 又不吃 0/1 字面量，统一 INTEGER，Postgres 方言文件自行映射）；两方言共同子集（无 AUTOINCREMENT/RETURNING 依赖）；迁移文件 `internal/metadata/migrations/{sqlite,postgres}/001_init.sql` 起步，**文件体内禁止自带 BEGIN/COMMIT**（迁移器已包事务，嵌套即错——T-10 review M9）。
>
> SQLite 连接机制（T-10 review B2 修复后定稿，详见 ADR-0007 勘误）：`foreign_keys=ON`、`busy_timeout=5000`、`case_sensitive_like=ON` 三条 **per-connection PRAGMA 必须经 DSN `_pragma=...` 下发**（每连接生效；`db.ExecContext` 只打到池中第一条连接，扩池后 FK 会静默失效）；`journal_mode=WAL` 是库级、同样走 DSN 统一管理；池 `MaxOpenConns = NumCPU`。
>
> **前缀查询大小写敏感**：`case_sensitive_like=ON` 全局生效，`ListByPrefix`/`DeleteByPrefix` 的 LIKE 臂与 `=` 精确臂同为二进制比较——与 repo key（`[a-z0-9-]` 全小写字符集）和制品 path（大小写敏感，Maven groupId/Generic 任意路径）的语义一致（T-10 review B1 裁决；修复前 LIKE 跨大小写误匹配曾致前缀删除静默多删行）。
>
> `schema_migrations` 表由迁移器自建（鸡生蛋：记账表必须先于首个迁移文件存在），不在 001_init.sql 内——与代码实现对齐（T-10 review M10）。

```sql
-- 001_init.sql (sqlite dialect)；schema_migrations 见上方注记，不在本文件

CREATE TABLE repositories (
  repo_key  TEXT PRIMARY KEY,              -- 唯一标识，[a-z][a-z0-9-]{1,62}（PRD FR-3-AC4；原架构 {1,31} 作废，后经 T-22 回写）；保留字 api/v2/docs/console/ui/assets 禁用（并集定案 ADR-0008 增补，T-108+T-110；api/v2=路由分发，docs/console/ui/assets=ADR-0011/0014 挂载与资产段）
  type      TEXT NOT NULL,                 -- 'local' | 'remote' | 'virtual'
  package_type TEXT NOT NULL,              -- 'generic' | 'docker' | 'maven' | 'npm' | 'pypi'
  description TEXT NOT NULL DEFAULT '',
  config    TEXT NOT NULL DEFAULT '{}',    -- JSON：type 特有配置（remote.url 等 [M3]）
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX idx_repositories_type ON repositories(type, package_type);

-- 远端代理配置 [M3]，M1 建表不使用（双栈同步演进成本 < 后期迁移）
CREATE TABLE remote_configs (
  repo_key   TEXT PRIMARY KEY REFERENCES repositories(repo_key) ON DELETE CASCADE,
  url        TEXT NOT NULL,
  username   TEXT NOT NULL DEFAULT '',
  password   TEXT NOT NULL DEFAULT '',     -- 加密存储 [M3 定密钥方案]
  cache_ttl_seconds INTEGER NOT NULL DEFAULT 0,
  unreachable_mask INTEGER NOT NULL DEFAULT 0   -- 原 BOOLEAN 声明经 T-25 勘误为 INTEGER（§6 前言）
);

CREATE TABLE blobs (
  sha256     TEXT PRIMARY KEY,             -- hex 小写 64
  sha1       TEXT NOT NULL DEFAULT '',
  md5        TEXT NOT NULL DEFAULT '',
  size       INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE nodes (
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  path      TEXT NOT NULL,                 -- repo 内相对路径，'/' 分隔，无前导 '/'；尾斜杠 = folder 行（目录实体化不变量：任何 node 写前其全部祖先目录段已有 folder 行，ADR-0016 + 007 回填；folder 行引用共享空哨兵 blob、size 0，删除空链由 pruneEmptyParents 收）
  sha256    TEXT NOT NULL REFERENCES blobs(sha256),
  size      INTEGER NOT NULL,
  mime      TEXT NOT NULL DEFAULT 'application/octet-stream',
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (repo_key, path)
);
CREATE INDEX idx_nodes_blob ON nodes(sha256);   -- GC 反连接 & 删 blob 前检查

CREATE TABLE users (
  username        TEXT PRIMARY KEY,
  password_hash   TEXT NOT NULL,           -- argon2id 编码串（PHC 格式）
  is_admin        INTEGER NOT NULL DEFAULT 0,
  enabled         INTEGER NOT NULL DEFAULT 1,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);

CREATE TABLE tokens (
  id           INTEGER PRIMARY KEY,        -- sqlite: ROWID；postgres: SERIAL（方言内允许）
  username     TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  token_sha256 TEXT NOT NULL UNIQUE,       -- sha256(plaintext)
  expires_at   TEXT NOT NULL,              -- RFC3339；'9999-12-31T00:00:00Z' 表永不过期
  created_at   TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_tokens_user ON tokens(username);

-- 权限模型：命名 permission target（PRD E-24，M1 定稿；后经 T-22 回写——原设计的扁平
-- permissions 行（username×repo_key×path_prefix 单前缀）被 PRD 推翻，差异见下注释）。
CREATE TABLE permission_targets (
  name       TEXT PRIMARY KEY,             -- target 名，唯一（如 'ci-out-rw'）
  repos      TEXT NOT NULL DEFAULT '[]',   -- JSON 数组：适用的 repo key 列表（'*' 不用，显式列举）
  includes   TEXT NOT NULL DEFAULT '[]',   -- JSON 数组：includePatterns，'**'/'*' 两级通配
  excludes   TEXT NOT NULL DEFAULT '[]',   -- JSON 数组：excludePatterns；exclude 命中优先于 include
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE permission_principals (       -- target × principal × actions（users；groups 列 [M4] 扩展）
  id          INTEGER PRIMARY KEY,
  target_name TEXT NOT NULL REFERENCES permission_targets(name) ON DELETE CASCADE,
  principal   TEXT NOT NULL,               -- 用户名（groups：principal_type 区分 [M4]）
  principal_type TEXT NOT NULL DEFAULT 'user',
  can_read    INTEGER NOT NULL DEFAULT 0,  -- actions ∈ read|write|delete；write 含上传不含删除
  can_write   INTEGER NOT NULL DEFAULT 0,
  can_delete  INTEGER NOT NULL DEFAULT 0,
  UNIQUE (target_name, principal, principal_type)
);
-- 与被作废的扁平行的差异（T-10 迁移文件同款注释）：
-- ① target 有 name（可被 /binflow/api/v1/permissions CRUD 引用与整体删除，删除即授权失效）；
-- ② 多 repo + include/exclude 双 pattern（原单 path_prefix 只有 include 语义）；
-- ③ principals 独立成表（原 username 内联），groups 仅加行不加表结构。

CREATE TABLE audit_events (
  id         INTEGER PRIMARY KEY,
  time       TEXT NOT NULL,
  actor      TEXT NOT NULL,
  action     TEXT NOT NULL,                -- deploy|delete|download|login.success|login.failed|repo.create|...
  repo_key   TEXT NOT NULL DEFAULT '',
  path       TEXT NOT NULL DEFAULT '',
  detail     TEXT NOT NULL DEFAULT '{}'    -- JSON
);
CREATE INDEX idx_audit_time ON audit_events(time);
CREATE INDEX idx_audit_repo ON audit_events(repo_key, time);

-- virtual 仓库解析顺序 [M3]
CREATE TABLE virtual_members (
  virtual_repo TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  member_repo  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  position     INTEGER NOT NULL,
  PRIMARY KEY (virtual_repo, member_repo)
);

-- ===== 002_docker.sql（M2 增量，ADR-0010；架构定稿，dev-go-core 落迁移文件）=====
-- docker 制品的 node 布局约定（不加表，复用 nodes；**双 node 布局**经 T-39 裁定、T-51 追认为正式契约）：
--   manifest blob 的 node path = "<image>/manifests/<digest-hex>"；
--   layer/config blob 的 node path = "<image>/blobs/<digest-hex>"（image = name 去掉 repo key 首段后的相对名，
--   可含 '/'）。digest 寻址直达 nodes 主键，无需 JOIN。
--   双 node 布局语义（T-39 review non-blocking #2）：同一 manifest digest 同时落 manifests 行与
--   （经引用链）blobs 行族——index 子 manifest 即使 descriptor mediaType 未知/拼错也能在 blobs 路径
--   探测成功（mediaType 选路失误被双行兜住）；DELETE manifest 后 blob node 存活、manifest GET 404
--   而 blob GET 仍 200 是**既定语义**（blob 物理回收归 GC）。此布局已是探测锚点与 FR-7-AC4 测试钉死面。
CREATE TABLE docker_manifests (           -- manifest 元数据（manifest 本体是普通 blob/node）
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  image     TEXT NOT NULL,                -- 镜像相对名（不含 repo key 首段）
  digest    TEXT NOT NULL,                -- 裸 hex sha256（blob 主键同源）
  media_type TEXT NOT NULL,               -- 客户端透传的裸 media type（剥 ; 参数，非白名单——§5.3 校验链①，T-51 消歧）
  size      INTEGER NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY (repo_key, image, digest)
);
CREATE INDEX idx_docker_manifests_image ON docker_manifests(repo_key, image);

CREATE TABLE docker_tags (                -- tag → digest 指针（ mutable，可重指）
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  image     TEXT NOT NULL,
  tag       TEXT NOT NULL,                -- 'latest' 等；tag 字符集按 spec（[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}）
  digest    TEXT NOT NULL,                -- → docker_manifests.digest（逻辑外键；跨表 FK 到复合主键的部分列不做，校验在服务层）
  updated_by TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (repo_key, image, tag)
);
CREATE INDEX idx_docker_tags_image ON docker_tags(repo_key, image);

CREATE TABLE docker_refs (                -- manifest ↔ blob 引用账（config/layer；GC 引用事实的第二来源）
  repo_key  TEXT NOT NULL,
  image     TEXT NOT NULL,
  manifest_digest TEXT NOT NULL,          -- 引用方
  blob_digest     TEXT NOT NULL,          -- 被引用（config 或 layer，child_media_type 记角色）
  child_media_type TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (repo_key, image, manifest_digest, blob_digest)
);
CREATE INDEX idx_docker_refs_blob ON docker_refs(blob_digest);  -- 删 blob 前查引用（对齐 idx_nodes_blob 的用途）
-- GC 影响（§4.4 增补）：mark 阶段的引用集合从「SELECT DISTINCT sha256 FROM nodes」扩为
-- 「UNION SELECT DISTINCT blob_digest FROM docker_refs」——manifest 引用的 layer/config 即使
-- 无独立 node 行也不可回收。docker_manifests/docker_tags 行随 nodes 级联语义由服务层维护。

-- ===== 003_remote_virtual.sql（M3 增量，ADR-0012/0013；架构定稿，dev-go-core 落迁移文件）=====

-- remote_configs 扩列（M1 占位表转正）：
--   ALTER TABLE remote_configs ADD COLUMN content_ttl_seconds INTEGER NOT NULL DEFAULT 86400;   -- artifact 长 TTL（默认 24h 后可再验证；checksum 命中永不再验）
--   ALTER TABLE remote_configs ADD COLUMN metadata_ttl_seconds INTEGER NOT NULL DEFAULT 600;    -- metadata 短 TTL（maven-metadata/packument/simple 页）
--   ALTER TABLE remote_configs ADD COLUMN allow_private_upstream INTEGER NOT NULL DEFAULT 0;    -- SSRF 豁免（显式，审计事件记录）
--   ALTER TABLE remote_configs RENAME COLUMN unreachable_mask TO blocked_out;                   -- 手动遮蔽（ADR-0012 决策 2）
--   password 列语义变更：明文 → 'enc:v1:<b64(nonce+ciphertext)>'（AES-256-GCM，密钥 env
--   BINFLOW_REMOTE_CREDENTIALS_KEY；迁移内完成存量加密，无密钥且有存量行 → 启动 fail-fast）。
CREATE TABLE remote_cache (               -- 缓存验证器元数据（per repo+path；nodes 不加列，本地制品面零污染）
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  path      TEXT NOT NULL,
  etag      TEXT NOT NULL DEFAULT '',
  last_modified TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL,               -- RFC3339
  expires_at TEXT NOT NULL,
  kind      TEXT NOT NULL DEFAULT 'content', -- 'content' | 'metadata'（TTL 分流，ADR-0012）
  PRIMARY KEY (repo_key, path)
);
CREATE INDEX idx_remote_cache_expiry ON remote_cache(expires_at);  -- 周期清扫候选（M3 顺手可做，非必须）

-- virtual_members（001 已建）position 列语义：'桶内声明序'（ADR-0013 联动记录：两桶序——
-- priorityResolution 优先桶在前、其余成员桶内声明序；T-79 定案取代初版 local-first）——无 DDL 变更，仅注释与文档语义。

-- npm/PyPI 无专属表：npm packument 是 node（<pkg>/packument.json，§5.4.2）；pypi simple 页
-- 按需生成（§5.4.3）；maven maven-metadata.xml 按需生成（§5.4.1）。三协议共用 nodes+blobs。

-- ===== 004_console_governance.sql（M4 增量，ADR-0014/0015 + T-108 勘误；架构定稿，dev-go-core 落迁移文件）=====
-- 另含两处列级增量（PRD FR-27-AC8/GE-01 定）：
--   ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';   -- M1 遗留收编：PUT/POST 校验链不变（blank→400），GET 回显
--   CREATE INDEX idx_audit_actor  ON audit_events(actor, time);    -- 审计查询索引（PRD 归 architect）：GE-01 全参数查询面
--   CREATE INDEX idx_audit_action ON audit_events(action, time);   --（idx_audit_time 001 已建，actor/action 复合补齐过滤矩阵）

CREATE TABLE groups (                     -- 用户组（permission_principals.principal_type='group' 的消费面，M1 预留兑现）
  id          INTEGER PRIMARY KEY,        -- sqlite: ROWID；postgres: SERIAL（方言内允许）
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

CREATE TABLE user_groups (                -- 成员关系（表名对齐 PRD §0 架构依赖行；T-108 勘误，原草图名 group_members）
  group_id  INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  username  TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  PRIMARY KEY (group_id, username)
);
CREATE INDEX idx_user_groups_username ON user_groups(username);   -- 认证热路径（fillGroups 逐请求）
                                                                  -- 补索引（T-97 review NB1 勘误，T-115 落地）；实现侧走
                                                                  -- 新迁移文件 006（迁移只追加，不回写 004）
-- Authorizer 消费路径：认证时 JOIN 解析进 Principal.Groups（§3.4）；permission_principals 的
-- group 行在 Can 判定时与用户组名单匹配——无 DDL 变更（principal_type 列 M1 已备）。
-- 管理端点：组 CRUD 走兼容层 /api/security/groups（SE-01~04）；成员关系经 PUT/POST /api/security/users/{name}
-- 的 groups[] 字段维护（组不存在 → 400 定案文案）；组被 permission target 引用时删除 → 409（PRD K3 从严）。

CREATE TABLE web_sessions (               -- 浏览器会话（ADR-0014 决策 2 + 勘误③；存形态与 tokens 同规）
  id_hash      TEXT PRIMARY KEY,          -- sha256(session id plaintext)，明文只出现在 Cookie binflow_session
  username     TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
  created_at   TEXT NOT NULL,
  expires_at   TEXT NOT NULL,             -- 绝对 TTL：console.session_ttl_hours 默认 24（seconds 覆盖键测试粒度）；滑动受绝对封顶
  last_used_at TEXT NOT NULL DEFAULT '',
  revoked_at   TEXT NOT NULL DEFAULT ''   -- 登出 = revoke；过期/吊销行由启动清扫（同 upload_sessions 表的 DB sweep 模式，T-209 口径）
);
CREATE INDEX idx_web_sessions_user ON web_sessions(username);

CREATE TABLE repo_usage (                 -- 配额计数（ADR-0015 决策 2）：与 node 增删同事务维护
  repo_key      TEXT PRIMARY KEY REFERENCES repositories(repo_key) ON DELETE CASCADE,
  logical_bytes INTEGER NOT NULL DEFAULT 0,
  updated_at    TEXT NOT NULL
);

-- ===== 008_oidc_ldap.sql（M6 增量，ADR-0020；架构定稿，dev-go-core 落迁移文件）=====
-- 在 users 表增加 provider/provider_id 列，支持 OIDC/LDAP 身份源。
-- 必须先行于 009_replication.sql（replications 引用 users 表）。
--   ALTER TABLE users ADD COLUMN provider TEXT NOT NULL DEFAULT 'local';
--     'local' = 本地密码/token 用户（既有语义不变）；'oidc' = OIDC 认证用户；
--     'ldap' = LDAP 认证用户（password_hash 为空，无本地密码）。
--   ALTER TABLE users ADD COLUMN provider_id TEXT NOT NULL DEFAULT '';
--     OIDC: ID Token 的 'sub' claim；LDAP: 用户的 DN（distinguished name）。
--     本地用户（provider='local'）必须为空字符串。
-- 索引（SQLite 方言）：
--   CREATE INDEX idx_users_provider ON users(provider, provider_id);
--     SQLite 不支持部分唯一索引（WHERE provider != 'local'），故使用普通非唯一索引；
--     非本地用户的 (provider, provider_id) 唯一性由服务层 enforce。
-- 索引（Postgres 方言）：
--   CREATE UNIQUE INDEX idx_users_provider ON users(provider, provider_id) WHERE provider != 'local';
--     Postgres 支持条件唯一索引，非本地用户直接由 DB 保证唯一性。

-- ===== 009_replication.sql（M6 增量，ADR-0021；架构定稿，dev-go-core 落迁移文件）=====
-- 复制/联邦：push 复制（异步，制品 Put 后触发）+ pull 复制（复用 remote 机制，ADR-0021 决策 2）。
CREATE TABLE replications (
  id                         INTEGER PRIMARY KEY,
  name                       TEXT NOT NULL UNIQUE,       -- 人类可读名称
  source_repo                TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  target_url                 TEXT NOT NULL,              -- 目标 BinFlow 实例 base URL（如 https://remote.example.com）
  target_repo                TEXT NOT NULL,              -- 目标实例上的仓库 key
  target_username            TEXT NOT NULL DEFAULT '',
  target_password_enc        TEXT NOT NULL DEFAULT '',   -- AES-256-GCM 加密（enc:v1:<b64> 格式，与 remote_configs.password 同规）
  max_bandwidth_bytes_per_sec INTEGER NOT NULL DEFAULT 0, -- 0 = 不限速
  max_items_per_push         INTEGER NOT NULL DEFAULT 1000, -- 每次触发推送的制品数上限
  enabled                    INTEGER NOT NULL DEFAULT 1,
  created_at                 TEXT NOT NULL,
  updated_at                 TEXT NOT NULL
);
CREATE INDEX idx_replications_source ON replications(source_repo);

CREATE TABLE replication_tasks (  -- 单次复制任务记录（push 方向）
  id              INTEGER PRIMARY KEY,
  replication_id  INTEGER NOT NULL REFERENCES replications(id) ON DELETE CASCADE,
  blob_sha256     TEXT NOT NULL,              -- 被复制的 blob hex sha256
  node_path       TEXT NOT NULL,              -- 仓库内相对路径
  status          TEXT NOT NULL DEFAULT 'pending', -- pending|in_progress|success|failed|skipped
  attempts        INTEGER NOT NULL DEFAULT 0,
  last_error      TEXT NOT NULL DEFAULT '',
  created_at      TEXT NOT NULL,
  completed_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_replication_tasks_status ON replication_tasks(replication_id, status);
CREATE INDEX idx_replication_tasks_pending ON replication_tasks(status, created_at);  -- 调度器扫描候选

-- ===== 010_upload_sessions.sql（M6 收尾增量，ADR-0025 决策 5 / T-209；架构 §4.1 回写补记）=====
-- 本地 filestore 上传会话状态入 DB（磁盘 sessions/<uuid>/state.json 方案废止）。
-- 仅 disk 后端使用；S3 会话走 multipart（状态由 S3 服务端持有）。
CREATE TABLE upload_sessions (
  id         TEXT PRIMARY KEY,               -- 会话 uuid（commit 前不绑定 blob，故主键非 sha256）
  state      TEXT NOT NULL DEFAULT '',       -- 引擎自有不透明状态 blob（metadata 层不解读）
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL                   -- ttl 默认 24h；启动 sweep（ListExpired）回收
);
CREATE INDEX idx_upload_sessions_expiry ON upload_sessions(expires_at);
-- 快照语义：PurgeTransientFromSnapshot 与 web_sessions 并列清（瞬态不进备份面）。

-- ===== 011_rbac.sql（[M7] 增量，ADR-0026 Accepted（T-214 终裁）；架构定稿，dev-go-core 落迁移文件）=====
-- 闭集角色（users.role）+ permission target 的 manage 动作。无新表、无新索引。
--   wire 字段名 = adminRole（值与列值同拼 snake 三值，§3.4a 不变量 4）；idp_sync 求值 =
--   admin_group > readonly_group（oidc.<p>.readonly_group / ldap.readonly_group）> user。
--   ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
--     闭集 'admin' | 'readonly_admin' | 'user'（代码常量 Role*，§3.4a）；校验在服务层。
--   UPDATE users SET role = 'admin' WHERE is_admin = 1;
--     回填；is_admin 列保留为兼容镜像（substore 同语句维护，M8 迁移移除——OIDC idp_sync
--     的每次登录刷新改写 role：admin_group 命中 → 'admin' > readonly_group 命中 → 'readonly_admin' > 其余 → 'user'）。
--   ALTER TABLE permission_principals ADD COLUMN can_manage INTEGER NOT NULL DEFAULT 0;
--     m 动作 = 仓库级 admin（repo 配置族路由的 CanManageRepo 消费，§3.4a 求值链③）；
--     m 的 target 匹配只判 repos[]，includes/excludes 不参与。
```

首启种子数据（迁移 001 内）：预置 `admin` 用户（is_admin=1）。口令引导（ADR-0009，用户定案）：env `BINFLOW_ADMIN_PASSWORD` 优先；未设置时使用**文档化缺省值 `password`**（仅限评估——文档与启动日志双重标注，检测到缺省值时启动打 WARN）；仅在 admin 用户不存在时生效，改密后不被后续启动覆盖。

模块路径：go.mod `module github.com/lzwzzy/binflow`（ADR-0008），所有 import 以此为根。

---

## 7. HTTP 层（httpapi）

### 7.1 路由表（stdlib ServeMux，ADR-0005）

```
/healthz            GET   存活（恒 200，无依赖检查）          → K8s liveness（基础端点，不带前缀）
/readyz             GET   就绪（metadata ping + storage 可写） → K8s readiness（同上）
/metrics            GET   [M6] Prometheus 指标（expvar + 自写 text format，ADR-0022；不带
                          /binflow 前缀，与 /healthz/readyz 同为根级探测端点）
                          **可用指标**（M6 最小集）：
                          - binflow_blob_count（gauge，当前 blob 总数）
                          - binflow_blob_bytes（gauge，当前 blob 物理总字节）
                          - binflow_node_count（gauge，当前 node 总数）
                          - binflow_repo_count（gauge，当前仓库数）
                          - binflow_http_requests_total（counter，labels: method, path_group, status_class）
                          - binflow_http_request_duration_seconds（histogram，labels: method, path_group）
                          - binflow_upload_sessions_active（gauge，活跃上传会话数）
                          - binflow_go_memstats（go 标准 expvar 内存指标族）
/metrics/json       GET   [M6] 同上指标，JSON 格式（expvar 原生格式，调试/自定义采集器用）
/v2/...             *     docker adapter [M2]（**根级例外**，ADR-0010：不剥 /binflow 前缀，进同一
                          middleware 链；含 /v2/token 自有 token 端点；详见 §5.3）
/binflow/api/v1/... *     自有 API（稳定契约，全部需认证——匿名只作用于内容路径）：
  ~~POST /binflow/api/v1/tokens（签发 token）；POST|GET /binflow/api/v1/repositories（建/列仓）；
  GET|PUT|DELETE /binflow/api/v1/repositories/{key}（仓详情/改/删）；GET …/{key}/_list（制品列表）~~
                          （M1 规划残留勘误 [M7] T-214 核实 router.go：上述 /api/v1 自有
                          端点从未实现——token 签发落在兼容面 POST /api/security/token、
                          仓库 REST 全族落在 /api/repositories[/{key}]、制品列表落在
                          /api/storage/{repo}/{path}?list；[M7] 管理门清点以实际注册点为准）
  GET    /binflow/api/v1/audit                     审计查询（admin, ?repo=&actor=&action=&since=&until=&limit=&cursor=，M4 GE-01）
  POST   /binflow/api/v1/system/gc                 GC 触发（admin, {"apply":bool,"graceHours"?}，同步执行，
                                                 GC↔export 互斥 409——ADR-0015 勘误；GET 状态端点 P2 债务不做）
  POST/GET/DELETE /binflow/api/v1/session          session 三动词（登录 / whoami / 登出——ADR-0014 勘误②）
  GET/POST /binflow/api/v1/permissions、DELETE /binflow/api/v1/permissions/{name}
                                                 permission target CRUD（E-24 自有面，admin；纯文本错误——
                                                 管理面）：POST = create-or-replace（单事务 PutTarget 整体替换
                                                 target + principals 行，失败无半授权残留）；GET = 列全部 target
                                                 含 principals；DELETE /{name} = 删 target（授权即时失效）。body
                                                 {name,repos,includePatterns,excludePatterns,principals{users,
                                                 groups}}；无 PUT 动词、无单读 GET /{name}（Artifactory /api/v2/
                                                 security/permissions 有意不承诺——M4 §2.2 裁定）——T-130 补行
                                                 （实现自 T-15 起即在 router，表漏列，T-122 遗留①）
  GET/POST /binflow/api/security/users、GET/PUT/POST /binflow/api/security/users/{name}
                                                 用户面（admin；兼容层路径非 /api/v1——E-16~E-19 既有事实，
                                                 M1 实现即落 security 段，原行 /api/v1/users 系笔误）；PUT /{name}
                                                 = create-or-replace，两态皆 201 无 body（auth-model §1.3-⑪，
                                                 T-97 R5 翻转）；POST /{name} = 部分更新（指针字段区分缺省/显式空，
                                                 groups[] 维护成员，200 无 body）；集合 POST = create-only 409；
                                                 DELETE /{name} 未做（FR-28 UI 需求补票 + user.delete 审计，P2 登记）
  GET/PUT/POST/DELETE /binflow/api/security/groups[/{name}]  组 CRUD（兼容层路径，非 /api/v1——PRD SE-01；
                                                 成员关系经 users 端点；被 permission target 引用删除 → 409）
  POST   /binflow/api/security/users/authorization/changePassword
                                                 改密（E-16 双路由的真实路径别名，与自有 PUT /api/security/
                                                 password 并存）；认证即门、非 admin——自助：userName 缺省=
                                                 调用者本人，admin 可指名他人、非 admin 指名 → 403；
                                                 newPassword1≠newPassword2 → 400、旧口令错误 → 400 非 401；
                                                 200 纯文本（用户管理纯文本层，auth-model §2.1/§2.4）；exact-
                                                 match case 先于 security/users/ 前缀 POST、不落 /{name}
                                                 部分更新臂——T-122 补行（实现早于 T-115 即在 router，表漏列）
  PUT    /binflow/api/security/password             自有改密路径（E-16 双路由的自有形态，PRD FR-5-AC3）：
                                                 body {"oldPassword","newPassword"}，目标恒为认证主体（无
                                                 userName 字段、不能指名他人）；认证即门、非 admin——自助改密；
                                                 旧口令错误 400、成功 200 纯文本（用户管理纯文本层，
                                                 auth-model §2.1 同族）——T-130 补行（实现自 T-15 起即在
                                                 router，表漏列，T-122 遗留②；与上方 authorization/
                                                 changePassword 真实路径别名并存）
  GET    /binflow/api/v1/storage/usage/{repo}       配额用量观测（{repo,usedBytes,quotaBytes}——GE-06；
                                                 quota 配置走 repositories 字段 quotaBytes，无专用设置端点）
  GET    /binflow/api/v1/storage/migration          存储迁移进度快照（admin；磁盘 filestore→S3 后台迁移，
                                                 T-164）。响应 {running,done,total,migrated,skipped,failed,
                                                 error?,started_at?,finished_at?}——total=启动盘点时待迁移
                                                 blob 数（在本地盘而不在 S3 的），skipped=盘点时 S3 已有
                                                 （幂等跳过不重拷），migrated/failed=已拷贝/拷贝失败计数；
                                                 error=首错文本、started_at/finished_at=RFC3339，三者
                                                 omitempty（未发生即不出键；从未启动过迁移时返回全 0
                                                 基线快照）。**未处于双写装配（backend≠s3，或 migration.
                                                 enabled≠true，或已完成置 completed——端点仅在 dual-write
                                                 启动时接线，T-178）→ 501「migration is not configured」，
                                                 非 404**。回写记录：实现先于契约，T-160 核验发现（PRD M6
                                                 FR-50 原记 {total_blobs,in_progress,completed} 拟名未按
                                                 实现落地，本行以 internal/storage/migration.go 的 json tag
                                                 为准；PRD 面勘误归 product-manager 另行处理）
  POST   /binflow/api/v1/storage/migration/start    触发后台迁移（admin；幂等——已在运行中时重复调用即返回
                                                 当前状态，202 + 上述状态体；启动被拒 409。盘点 +
                                                 SkipIfExists 拷贝、中断重启续跑——T-164 语义）
  POST   /binflow/api/v1/auth/oidc/init              [M6] OIDC 登录初始化（接受 JSON {"provider"} 或 URL query
                                                 ?provider=，返回 {"redirect_url"}——前端/CLI 重定向到 OIDC
                                                 Provider 的授权端点；state 参数由服务端生成并存入临时
                                                 cookie `binflow_oidc_state`，HttpOnly+SameSite=Lax，TTL 10min）
  GET    /binflow/api/v1/auth/oidc/callback           [M6] OIDC 回调端点（OIDC Provider 授权后回跳终点；
                                                 验证 state 参数 ↔ `binflow_oidc_state` cookie → 用 code 换
                                                 ID Token → 验证 JWT 签名与 claims → 签发本地 session/token；
                                                 登录成功 → 302 到 /binflow/ui/；失败 → 302 到 /binflow/ui/login?error=）
  GET    /binflow/api/v1/auth/oidc/providers          [M6] 列出已配置的 OIDC Provider 列表（公开端点，匿名可读：
                                                 [{"name","display_name","icon_url"}]，不暴露 client_id/secret）
  GET    /binflow/api/v1/replications                 [M6] 列出全部复制配置（admin；bare JSON array——沿
                                                 /api/v1/permissions 惯例；行形 = 配置行减密码字段：
                                                 {id,name,source_repo,target_url,target_repo,
                                                 target_username,max_bandwidth_bytes_per_sec,
                                                 max_items_per_push,enabled,created_at,updated_at}。
                                                 target_password 只写不读：POST 收明文、ADR-0012 enc:v1
                                                 封存落库、任何响应永不回显；target_username 在 CRUD 面
                                                 保留（无密码即无敏感性，管理面惯例）——status 面不出）
  POST   /binflow/api/v1/replications                 [M6] 创建复制配置（admin；201 + 上述配置行）。body 即
                                                 读形诸字段 + target_password（enabled 为 *bool）；校验：
                                                 name 1..64 字符 [A-Za-z0-9._-] 且首字符字母数字（DELETE
                                                 单段可寻址的前提）、source_repo 必须存在（400 点名问题
                                                 键；仅存在性预检查，不校验 local 类型——见回写记录）、
                                                 target_url 绝对 http/https（REST 时 400 优于任务事后
                                                 not-retryable）、数值字段非负；enabled 缺省 true、
                                                 max_items_per_push 缺省 1000（009 DDL 默认，store INSERT
                                                 恒带列由 handler 归一）；重复名 409；带 target_password
                                                 而无 master key → 400 点名 BINFLOW_REMOTE_CREDENTIALS_KEY
                                                 （与 remote 仓凭据同钥，ADR-0012 fail-fast）；store
                                                 busy 503
  DELETE /binflow/api/v1/replications/{name}          [M6] 删复制配置（admin；按 name 寻址非 id——store 按 id
                                                 删、name 经列表解析；204 无 body；任务行随 009 FK 级联；
                                                 未知名 404；store busy 503）
  GET    /binflow/api/v1/replication/status           [M6] 复制面板聚合载荷（admin；T-159 面板 10s 轮询端点，
                                                 T-180 桥接）：targets[] = 配置行 × ConfigStatus 拍平
                                                 （凭据全不下发，target_username 亦不出）+ events[] =
                                                 任务行跨配置合并 newest-first（?limit= 1..500，缺省
                                                 50）；两数组恒 [] 非 null；*_at 为 RFC3339 文本、'' 表
                                                 「从未/未终态」
                                                 ——回写记录：实现先于契约，T-180 核验发现（T-184 回写，
                                                 T-180 遗留之 docs 项）。本处原 ADR-0021 草案 7 端点中
                                                 GET/{id}、PUT/{id}（create-or-replace）、POST/{id}/
                                                 trigger（202 + {"task_count"}）、GET/{id}/tasks
                                                 （?status=&limit=&offset=）四处未落地且不承诺：PUT 待
                                                 enable/disable 语义裁定（router.go 注释挂号）、trigger
                                                 与按配置 tasks 无消费方——事件经本 status 聚合面可见；
                                                 寻址由 {id} 改 **name**；原行「source_repo 为 local
                                                 类型」未实现（仅存在性检查）。四端点均 admin 门（路由 +
                                                 create/delete handler 双查）、未接线实例（Deps.
                                                 Replication 为 nil）统一 501 非 404、错误体 errors[]
                                                 信封（治理族惯例）、审计动作 replication.config.create /
                                                 replication.config.delete。契约以 internal/httpapi/
                                                 replication.go 为准
  GET    /binflow/api/storage/{repo}/{path}?permissions
                                                 有效权限视图（SE-08/FR-27，M4 T-97；path 空=仓根）；
                                                 **admin 门**——T-97 review B2 定案：管理面数据（枚举主体名
                                                 与 r/w/d 分布），BinFlow 无 manage 动作、admin 为最近映射，
                                                 空门会使匿名读实例枚举 users/groups、击穿登录面存在性隐藏；
                                                 非 local 仓 → 400（先判型，remote 不触发上游拉取）、local+
                                                 item 不存在 → 404；形状 {"uri","principals":{"users":{"<主体
                                                 名>":["r","w","d"]},"groups":{...}}}——key=主体名、value=
                                                 权限字母集合（字母全集 r/w/n/d/m、BinFlow 用 r/w/d 子集；
                                                 无任何权限的主体不出现、无 target 覆盖=空对象）——T-113
                                                 勘误定案（逆向规格 §3 原记键值方向相反）；视图与判定共用
                                                 targetCovers 谓词、与 Authorizer.Can 不可能分叉——T-122
                                                 补行：T-97 草案②迟未落地，径按定案形态书写（草案②「匿名
                                                 同门」门位亦被 B2 admin 门取代）
/binflow/api/...    *     Artifactory 兼容子集 [按 docs/reverse/rest-api.md 逐步]（注意：兼容层路径不带 /artifactory 前缀，直接映射 /binflow/api/...）
/binflow/<repo>/... *     内容路径：按 repo.package_type 分发到 adapter（M1 = generic）
/binflow/assets/**  *     SPA 指纹资产（/binflow/assets/<hash>.js|css，immutable 缓存 [M4]）
/binflow/ui/**      *     Web 控制台 SPA [M4]（go:embed，ADR-0014 勘误①——挂 ui 段；段内
                          history fallback 回 shell，不越段吞内容路径；shell no-cache）
/binflow/docs/...   *     帮助文档站 [M5]（Docusaurus build 产物 go:embed，ADR-0011；
                          进 middleware 链但匿名可读——文档不设认证）
/binflow/           GET    301 → /binflow/ui/（M4 起；M1~M3 为占位 JSON）
```

**认证分层默认值（ADR-0009）**：内容路径 `GET/HEAD` 匿名放行（`security.anonymous_access: true` 默认）；内容路径写操作与 `/binflow/api/**` 全部要求认证，不受该开关豁免。`anonymous_access: false` 时所有端点一律认证。

**[M7] 管理面门位精确化（ADR-0026 Accepted）**：`routeAuth{admin: bool}` 升级为
`routeAuth{manage ManagementCapability | repoManage bool}`——middleware `authorize()` 的
判定序不变（401 → 门位 → 内容 action），门位求值走 `auth.CanManage/CanManageRepo`
（§3.4a）。**路由清点表终版（T-214，2026-08-23——依 router.go 实际注册点逐路由 diff，
T-215 逐路由迁移施工图；实测对照源 = T-211 `m7-rbac-matrix.sh` 基线（user 管理面全 403
零副作用 / admin 全 200 / read-only-admin 列 SKIP 待 T-215），reports/agents/T-211.md；
相对基线的三处修正：① `GET /api/v1/replications`（配置列表）补入系统观测读；②
`GET /api/v1/permissions` 列表归安全读、token 列表端点不存在（M1 E-17 有意不存在，T-211
实测 404 佐证）；③ usage/{repo} 从系统读移入仓库域（quota 配置与用量观测同域，且保留
内容面 r 放行）。注：`GET /api/v1/storage/migration` 在未装配双写的裸实例过门后为 501
（§7.1 既有契约），readonly_admin 矩阵断言按「过门 501」而非 200 判**：

| # | 路由族 | 路由（router.go 实际注册点） | 现门 | [M7] 门 |
|---|---|---|---|---|
| 1 | 系统观测读 | GET `/api/v1/health`；GET `/api/v1/storage/stats`；GET `/api/v1/audit`；GET `/api/v1/storage/migration`；GET `/api/v1/replications`（配置列表）；GET `/api/v1/replication/status` | admin | `CapSystemRead` |
| 2 | 系统变更写 | POST `/api/v1/system/gc`（**含 apply=false dry-run**——readonly_admin 403，T-214 对 Q2 暂行的否决）；POST `/api/v1/storage/migration/start`；POST `/api/v1/replications`；DELETE `/api/v1/replications/{name}` | admin | `CapSystemWrite` |
| 3 | 安全读 | GET `/api/security/users`（列表）；GET `/api/security/users/{name}`；GET `/api/security/groups`（列表）；GET `/api/security/groups/{name}`；GET `/api/v1/permissions`（列表） | admin | `CapSecurityRead`（无 token 列表端点——router 无此路由，PRD FR-64 读面清单勘误） |
| 4 | 安全写 | POST `/api/security/users`（集合）；PUT·POST `/api/security/users/{name}`（含 **`adminRole`** 字段）；PUT·POST·DELETE `/api/security/groups/{name}`；POST `/api/v1/permissions`；DELETE `/api/v1/permissions/{name}`；POST `/api/security/token/revoke` | admin | `CapSecurityWrite`；**例外**：POST·DELETE `/api/v1/permissions` 追加 m-holder 覆盖集臂（handler：CapSecurityWrite ∨ target.repos ⊆ 调用者 m 覆盖集，越界 403——FR-65） |
| 5 | 仓库全局读 | GET `/api/repositories`（列表） | admin（D2/C22b） | `CapRepoRead`（readonly_admin 可见全量；m-holder **不开放**——过滤列表 M8+，§11.30） |
| 6 | 仓库全局写（建/删仓） | PUT `/api/repositories/{key}` 的**建仓臂**（repo 不存在）；DELETE `/api/repositories/{key}`（删仓） | admin | `CapRepoWrite`（handler 分臂：PUT 命中既有仓 → 族 7 替换臂；建/删仓不下放，FR-65） |
| 7 | 仓库域配置族 | GET `/api/repositories/{key}`（详情）；POST `/api/repositories/{key}`（部分更新）；PUT `/api/repositories/{key}`（**替换臂**）+ quota 字段；GET `/api/storage/{repo}/{path}?permissions`；GET `/api/v1/storage/usage/{repo}` | admin（usage 为 required + 用例判定） | `repoManage`（CanManageRepo：admin true / readonly_admin !write / user 走 `m`）；usage 判定 = CanManageRepo(read) ∨ Can(r)——W26b 既有 r 放行零回归；`?permissions` 字母集扩 m |
| 8 | 自助与协议面（显式不变） | PUT `/api/security/password`；POST `/api/security/users/authorization/changePassword`；POST `/api/security/token`（自铸，Q11）；POST·GET·DELETE `/api/v1/session`；`/api/v1/oidc/*`；GET `/api/v1/auth/methods`；GET `/api/storage/{repo}/{path}`（item / ?list——内容面语义，handler 判匿名/读权）；GET `/api/search/{artifact,checksum}`；`/v2/**`（含 `/v2/token`、`/v2/_catalog`——docker token 臂） | required-only / 空 / 内容 action | **不变**（不属管理面）；POST `/api/security/token` 增 step-up 钩子（ADR-0027 终版，下段） |

**[M7] token 自铸的二次认证插入点（ADR-0027 Accepted——T-214③ 终版契约）**：`POST /api/security/token`
handler 内——认证臂 = web session cookie **且** `p.Role != admin` **且** `auth.token_step_up`（默认
false）开启时要求二次证明，腿由 `users.provider` 决定：local → body `step_up_password`（argon2
校验，与登录同参数）；ldap → `step_up_password`（bind 重验，与登录同源）；oidc → `step_up_grant`
（console 经 `prompt=login` 新鲜重认证换发的**单次** mint grant，TTL ≤ `auth.token_step_up_grant_ttl_seconds`
默认 300s，绑定 user+session，进程内台账）。缺失 → 401 OAuth 形 `step_up_required`；失验/过期/复用 →
401 `step_up_invalid`。豁免：admin session、Basic、Bearer（token）、`/v2/token`、匿名（401 挑战照旧）。
中间件级通用 step-up 层与 OIDC 腿 body `id_token` 新鲜窗口两方案均否决（候选对比见 ADR-0027 修订版）。

**路由解析位置（T-14 review RepoLookup 缝终判）**：repo key → repo 行查询位于**授权门之后**；RepoLookup 用 `metadata.Get` 是有意为之的匿名读前置缝——安全面成立（无权者在查询前即被拦截，repo 行不泄漏给未授权请求）。`PackageTypeOf(ctx, key)` 收回服务层列 T-15/M2 非阻断重构项。

### 7.2 middleware 链（顺序固定）

`requestID → accessLog(方法/路径/状态/耗时/principal) → recover(panic→500+log) → CORS(可配 origin) → authenticator → authorizer(按路由所需 action) → handler`

authenticator 产出的 Principal 可为 nil（匿名）；authorizer 按 §3.4 规则结合 `security.anonymous_access` 决定放行或 401/403（管理面匿名一律 401）。

### 7.3 错误信封（所有非 2xx 统一）

```json
HTTP/1.1 404 Not Found
Content-Type: application/json
{ "errors": [ { "status": 404, "message": "node libs/foo.jar not found in repo generic-local" } ] }
```

数组形 `{"errors":[{status,message}]}` 为**全部端点**（含内容路径、/api/v1、探针除外的基础端点）统一格式，依据 docs/reverse/rest-api.md §0（高置信度定案，后经 T-22 回写，取代本节原占位单对象形）。

### 7.5 Console 与浏览器 session（M4 增量，ADR-0014 + T-108 勘误）

- **登录流**：`POST /binflow/api/v1/session`（JSON `{"username","password"}`，form 亦接受）→ 签发 `web_sessions` 行 + `Set-Cookie: binflow_session`（HttpOnly + Path=/binflow + SameSite=Lax；HTTPS 部署加 Secure）；`GET /api/v1/session` = whoami（前端路由守卫）；`DELETE` = 登出（revoke + cookie 重放 401）。session cookie 与 Basic/Token 为**等价认证凭据**（内容路径与管理面同用）。TTL = `console.session_ttl_hours`（默认 24）+ `console.session_ttl_seconds` 覆盖键（测试粒度，seconds 优先）；滑动续期受绝对 TTL 封顶（ADR-0014 内核）——**T-110 塌缩句：双键共用一键后滑动被绝对封顶吞没，会话必死于 `created_at + TTL`、与活跃度无关**（默认 24h 活跃用户也掉线重登；前端/QA 勿按「滑动续期」字面写成「活跃可续命」）。
- **CSRF（勘误后两层 + 习惯层）**：① SameSite=Lax；② **Origin 同源校验**（服务端强制：session cookie 认证的写请求携带非同源 `Origin` → 403，同源/无 Origin 放行——PRD FR-23-AC9）；③ SPA 自身附 `X-BinFlow-Console` 头作为习惯层（服务端不校验——撤销原「强制头」设计，它使 Cookie 面与 Basic/Token 面行为分叉，违背「前端消费通用 API」原则）。Bearer/Basic 天然免疫 CSRF。
- **SPA/REST 边界**：前端直接消费通用 `/api/v1/**` 与兼容层 `/api/security/**`（无 console 专属 API 树——CLI/curl/前端同一面，权限语义单源）；前端工程 = `web/`（vite + React + TS，base=/binflow/ui/），构建产物复制进 `internal/console/dist`（Makefile `console` 目标，与 docs-site 同构）。ADR-0005 白名单管辖 Go 依赖树；前端 devDependencies 不进二进制，政策见 ADR-0014 决策 4（lockfile 锁定、零运行时 CDN、CI npm audit、直接依赖变更过 architect）。

### 7.6 备份/恢复（M4 增量，ADR-0015 + T-112 勘误：形态对齐 M4 PRD FR-32/GE-09 与 T-96 实现）

> **勘误（2026-08-20，T-112 依 T-96 架构 review N6/N7；产物形态以 M4 PRD FR-32/GE-09 为准——PRD 后出且为 W 序列验收锚，按 T-88 R1 先例「PRD 命名面 + ADR 机制内核」；顺序硬规则、空实例、mtime、互斥锁内核均不变，ADR-0015 勘误二同口径）**：① 产物由「db + blobs.tar + manifest」修订为**目录形态**（tar 系 ADR 草案期措辞；`--tar` 单文件产物 P2 债务，flag 已定义、显式报未实现）；② CLI flag `--out` → **`--output`**；③ 「admin REST 异步触发（产物落 data_dir/exports/）」删除——**无 REST 面**（`/api/export/**` 404，GE-09）；④ import「非空 409」为 REST 语态，收敛为 CLI 退出码非 0；「启动 GC dry-run 报差异」降格为运维建议，非 CLI 内置步骤。

- **export（在线，仅 CLI：`binflow-server export -c <cfg> --output <dir> [--tar]`）**：① 取 data 目录维护锁（`storage.AcquireDataLock`，锁文件 `<data>/.maintenance.lock` 0600，flock/LockFileEx——GC↔export 双向互斥，ADR-0015 勘误③；REST gc 面与 CLI 同原语，T-94）→ ② `VACUUM INTO` SQLite 一致性快照，落 `<out>/metadata.db`（快照内 purge `web_sessions` 后再计 manifest 哈希，§11.19）→ ③ 拷贝 `blobs/<2hex>/` **目录树**（保 mtime + 权限位，ADR-0006 勘误②硬约束）→ ④ `manifest.json`（引用集 = **快照自身** nodes ∪ docker_refs，非磁盘现状；窗口内多余 blob 允许、import 后为普通 GC 候选；导出侧引用缺失 = fail-fast——源实例悬空引用堵在源头）。**顺序不可换**：先 DB 后 blobs，多余 blob 无害（恢复后常规 GC 收敛）；反向产生悬空引用。产物 = `<out>/metadata.db` + `<out>/blobs/` + `<out>/manifest.json`（**目录形态**；产物目录 0700、metadata.db/manifest.json 0600，NFR-S22）。**无 REST 面**（`/api/export/**` 404——GE-09；ADR-0015 决策 5「admin REST 异步触发」M4 不做）。审计 `export.run` 写**源实例**活库（快照已封存，事件属源实例历史——恢复侧不含它；actor=admin，CLI 无可命名 principal 的既有口径）。
- **import（仅 CLI：`binflow-server import -c <cfg> --input <dir> [--verify spot|full]`；停机 + 空 data dir，高危写面不走 REST——安全底线；不取维护锁：目标空 + 停机，无争用对象）**：目标 data dir 必须空（唯一豁免 `.maintenance.lock` 锁残留；非空 → **CLI 退出码非 0**，import 无 REST 面）。**先验证后写盘**：manifest 结构校验（formatVersion ≤ 1、blobCount/totalBytes 自洽、metadata.file 裸文件名防穿越）+ `metadata.db` sha256 实测 + blob **size 全验 + sha256 spot 前 100**（sha 排序确定性抽样；`--verify full` 全量重哈希 P1）+ schema 版本天花板（快照 schema > 本 build 最新 → 拒；旧快照恢复首开自动升迁）；写序 **db 先 blobs 后**（保 mtime；备份内名 `metadata.db` 固定供 manifest 引用，落位尊重目标机 config 命名——默认 `binflow.db`/显式 dsn）；任何失败清回空（**无半恢复**）；幂等 = **清空目标后**对同一备份重复导入等价（ADR-0015 勘误二释义）；跨版本需迁移链可达（schema_migrations ≤ 当前）。审计 `import.run` 落恢复库。「启动 GC dry-run 报差异」为运维建议（文档面），不在 CLI 内强制——多余 blob 由常规 GC 收敛。

优雅停机：SIGTERM → `server.Shutdown(ctx, 30s)`（等待在途上传 Commit；超时未收口的会话**保留**而非丢弃——[M7] ADR-0028）→ **关 storage.Engine（Close：停止变更、保留未过期会话 + 保留清单 INFO——[M7] ADR-0028 修订 T-9 回写项 B）** → 关 metadata → 退出码 0。健康检查与停机语义是 ADR-0004 各部署形态的公共契约（§9）。

---

## 8. 配置模型（config 包）

单 YAML `binflow.yaml` + env 覆盖（`BINFLOW_` 前缀，`__` 表层级，如 `BINFLOW_STORAGE__DATA_DIR`；列表/复杂值仅 YAML）。原则：凡是路径/端口/外部端点皆可覆盖，行为参数有安全默认；秘密（口令类，如 `BINFLOW_ADMIN_PASSWORD`）**不入 YAML**，只走 env。

**四个例外名**（不走 `__` 层级拼写；T-8 实现定稿，经 T-25 回写）：
1. `BINFLOW_ADMIN_PASSWORD` — admin 引导口令（秘密，ADR-0009）；
2. `BINFLOW_SECURITY_ANONYMOUS_ACCESS` — `security.anonymous_access` 的用户可见拼写（PRD/NFR-S8；`__` 规则拼写同样有效）；
3. `BINFLOW_DATA_DIR` — `storage.data_dir` 的扁平便捷拼写（docker `-e` / compose 高频；`BINFLOW_STORAGE__DATA_DIR` 同样有效）；
4. `BINFLOW_HOME` — **cmd 层（T-16）保留名**，config 包忽略。与 `BINFLOW_DATA_DIR` 的边界：HOME 是 config/data 默认值的**目录解析根**（未显式给路径时据此推导）；DATA_DIR 是 `storage.data_dir` 的**直配**，优先级高于 HOME 推导出的默认值。

```yaml
# binflow.yaml —— 全量字段（M1）；未列字段一律不给默认值即零值
# [M6] 新增 storage.backend、storage.s3、auth.oidc、auth.ldap、replication 段
server:
  listen: ":8080"                # env BINFLOW_SERVER__LISTEN
  base_url: ""                   # 对外可见 URL（控制台链接/absolute path 用），空=请求推导
  graceful_timeout_seconds: 30
  cors_origins: []               # 空=同源限制
storage:
  backend: "disk"                # [M6] 'disk' | 's3'（默认 disk，向后兼容；ADR-0019）
  data_dir: "./data"             # disk 后端的数据目录（s3 后端时忽略）；env 扁便捷拼写 BINFLOW_DATA_DIR
  session_ttl_hours: 24          # 零值 = 默认 24h（T-9 回写项 J）
  gc_grace_hours: 24             # 零值 = 默认 24h；grace 基准 = blob 文件 mtime（§4.4 硬约束）
  s3:                            # [M6] S3 对象存储后端配置（backend=s3 时必填；ADR-0019）
    bucket: ""
    prefix: ""                   # 对象 key 前缀，多实例共享桶时使用
    region: "us-east-1"
    endpoint: ""                 # 兼容 S3 协议的对象存储（MinIO/Ceph/阿里云 OSS），空=使用 AWS 默认 endpoint
    access_key_id: ""            # 走 env BINFLOW_STORAGE__S3__ACCESS_KEY_ID（秘密不入 YAML）
    secret_access_key: ""        # 走 env BINFLOW_STORAGE__S3__SECRET_ACCESS_KEY（秘密不入 YAML）
    use_path_style: false
    max_retries: 3
    upload_part_size_mb: 64      # multipart upload 单 part 大小（MB）
    incomplete_upload_cleanup_hours: 24  # 未完成的 multipart upload 自动清理周期
  migration:                     # [M6] 磁盘→S3 在线迁移（T-164；回写记录：实现先于契约，T-160 核验
                                 # 发现后随 T-176 补录——键名核对自 internal/config）
    enabled: false               # true = 双写模式（写双发 disk+S3、读 S3 优先 miss 回退磁盘）；
                                 # 此时 §7.1 迁移端点接线，否则两端点 501
    completed: false             # 迁移全部完成后置 true：纯 S3 模式（本地 blob 由运维手动清理）
    concurrency: 5               # 后台拷贝 goroutine 数（enabled 时必须为正；零值默认 5）
metadata:
  driver: "sqlite"               # 'sqlite' | 'postgres'
  dsn: ""                        # sqlite: 文件路径（空=data_dir/binflow.db）；postgres: URL
auth:
  argon2_memory_mb: 64
  token_default_ttl_hours: 720   # 30d
  oidc:                          # [M6] OIDC 身份提供者列表（ADR-0020）；可配置多个 Provider
    - name: "google"             # Provider 唯一标识（用于 /api/v1/auth/oidc/init?provider=）
      display_name: "Google"     # 控制台登录按钮显示名
      issuer: "https://accounts.google.com"  # OIDC Issuer URL（/.well-known/openid-configuration 自动发现）
      client_id: ""              # 走 env BINFLOW_AUTH__OIDC__<N>__CLIENT_ID（秘密不入 YAML）
      client_secret: ""          # 走 env BINFLOW_AUTH__OIDC__<N>__CLIENT_SECRET（秘密不入 YAML）
      redirect_url: ""           # 空 = {base_url}/binflow/api/v1/auth/oidc/callback
      scopes: ["openid","profile","email"]
      claim_mapping:             # OIDC claims → BinFlow 用户字段映射
        username_claim: "email"  # 默认用 email 作为 username
        email_claim: "email"
        display_name_claim: "name"
      auto_provision: true       # 首次登录时自动创建用户（admin=false，需手动授权）
      icon_url: ""               # 控制台登录按钮图标（可选）
  ldap:                          # [M6] LDAP 身份提供者（ADR-0020）；单实例
    enabled: false
    host: ""
    port: 389                    # 636 为 LDAPS（推荐）
    use_ssl: false
    base_dn: ""                  # 如 "dc=example,dc=com"
    user_dn_pattern: ""          # 如 "uid={0},ou=users"——{0} 替换为登录用户名
    bind_user: ""                # 走 env BINFLOW_AUTH__LDAP__BIND_PASSWORD（秘密不入 YAML）
    bind_password: ""
    username_attribute: "uid"    # LDAP 属性映射到 BinFlow username
    email_attribute: "mail"
    display_name_attribute: "cn"
    auto_provision: true
replication:                     # [M6] 复制调度器配置（ADR-0021）
  enabled: true
  scheduler_interval_seconds: 60 # 调度器扫描间隔（检查 pending 任务）
  max_concurrent_tasks: 10       # 全局并发复制任务上限
  task_retry_max: 3              # 单个任务最大重试次数
  task_retry_delay_seconds: 300  # 失败任务重试间隔（5 分钟）
security:
  anonymous_access: true         # 内容路径 GET/HEAD 匿名放行（ADR-0009）；false = 全端点认证
                                 # 用户可见主键名（PRD/NFR-S8）；env BINFLOW_SECURITY_ANONYMOUS_ACCESS
                                 # 兼容别名 auth.anonymous_read（env BINFLOW_AUTH__ANONYMOUS_READ）：
                                 # config 包双键等价读取，两键同给且不一致 → 启动报错（T-8 已实现，
                                 # 键名冲突经 T-22 回写统一以 PRD 形态为准）
audit:
  enabled: true
logging:
  level: "info"                  # debug|info|warn|error
  format: "json"                 # json|console
```

校验规则（`Load` 内，fail-fast）：port 可解析；`data_dir` 可创建/可写（disk 后端）；`storage.backend` ∈ {disk, s3}；`metadata.driver` ∈ {sqlite, postgres}；s3 后端时 `bucket`/`region` 必填；`storage.migration.enabled=true` 时 `concurrency` 必须为正（零值经 `Defaults()` 填 5，显式 ≤0 报错；`enabled=true` 且 `backend≠s3` 由 cmd 装配层拒绝启动——双写的 S3 写目标缺失，T-164）；OIDC provider 的 `name`/`issuer` 必填且 `name` 唯一；LDAP enabled 时 `host`/`base_dn` 必填；未知顶层键报错（防拼写静默失效）。`Config` 结构带 `Validate()` 与 `Defaults()`，表驱动测试覆盖每条规则。

---

## 9. 部署架构约定（ADR-0004 落地口径，供 release-engineer）

| 形态 | 进程 | 卷 | 健康检查 | 说明 |
|---|---|---|---|---|
| 单二进制 | `binflow-server serve -c binflow.yaml` | `<data_dir>`（blobs+sessions+db） | `/healthz` `/readyz` | systemd `Type=notify` 亦可 [M5] |
| Docker/compose | 同上，EXPOSE 8080 | volume → `/var/lib/binflow` | 同上 | 镜像内默认 `data_dir=/var/lib/binflow` |
| Helm/K8s | Deployment(1 副本，**M1 无 HA**) | PVC(RWO) → `/var/lib/binflow` | liveness=/healthz, readiness=/readyz | 多副本挂同 PVC 为**禁止**配置（values 校验拦截） |
| 离线包 | 镜像 tar + chart + 脚本 | 同上 | 同上 | 校验和齐全 |

**M3 网络增补（ADR-0012）**：remote 仓库使 BinFlow 由纯内网服务变为**出网客户端**——部署文档（tech-writer）需给出方向性出网要求（上游 443/80 出站放行）；compose/Helm 产物不内置 egress 代理，网络策略归用户。SSRF 防护在应用层（配置校验 + 连接时双检），不依赖部署层网络策略。

**M2 增补（ADR-0010 裁决第 6 条）**：docker 可用性**不依赖反代**——单二进制/compose/Helm 形态下 `/v2/**` 由应用直接服务，`docker login <host>` 直连即可。已有 nginx/traefik 前置的用户可对 `/v2/` **直通不 rewrite**（`proxy_pass` 原样）；compose 产物（T-17 产物演进）默认**不加**反代组件，文档给「前置反代直通 `/v2/`」示例片段即可。

**M5 增补（ADR-0011）**：帮助文档中心 = Docusaurus 站点，build 产物 **go:embed 进 binflow-server**、挂 `/binflow/docs/**`（统一前缀内）——每种部署形态自带文档（离线/air-gapped 场景可查，对齐「15 分钟跑通」成功标准）；独立域名托管为用户可选自办（同一份静态产物），BinFlow 不维护双轨。源文件工作流：tech-writer 只写 `docs/user/*.md`（frontmatter 用 Docusaurus 兼容子集），`docs-site/` 聚合构建（配置归 architect/release-engineer，writer 不碰）。Makefile 增 `docs` 目标；二进制 40MB 预算对 docs 增量（典型 5~15MB）在 M5 check-size 实测，超限 fallback 独立 tar。

**M6 增补（ADR-0019/0020/0021/0022）**：

- **S3 后端（ADR-0019）**：
  - K8s 形态：S3 后端时**无需 PVC**（blob 数据全在对象存储），Deployment 可为多副本（M6 多副本实验性——单副本为推荐配置，多副本需验证 SQLite 写冲突场景，见 §11.21）。
  - 环境变量：`BINFLOW_STORAGE__S3__ACCESS_KEY_ID`、`BINFLOW_STORAGE__S3__SECRET_ACCESS_KEY` 必须通过 K8s Secret 注入（不落 YAML）。
  - 网络要求：入站（用户/客户端）→ 端口 8080；出站 → S3 endpoint（HTTPS 443）用于对象存储操作。
  - MinIO 示例：`storage.s3.endpoint: "http://minio.minio.svc.cluster.local:9000"` + `use_path_style: true`。

- **OIDC/LDAP（ADR-0020）**：
  - OIDC callback URL 必须是 `{base_url}/binflow/api/v1/auth/oidc/callback`，反代/TLS 终结层必须透传该路径。
  - OIDC Provider 的 `client_secret` 走 env `BINFLOW_AUTH__OIDC__<N>__CLIENT_SECRET`（`<N>` 为配置数组索引，从 0 开始）。
  - LDAP 绑定密码走 env `BINFLOW_AUTH__LDAP__BIND_PASSWORD`。
  - 自签 LDAP 证书：需将 CA 证书挂载到容器并设置 `SSL_CERT_FILE` 或 `LDAPTLS_CACERT` 环境变量。

- **Prometheus 指标（ADR-0022）**：
  - `/metrics` 端点输出 Prometheus text format（`Content-Type: text/plain; version=0.0.4`），无 `/binflow` 前缀。
  - K8s ServiceMonitor/PodMonitor 直接抓取 `/metrics` 即可，无需 sidecar 或 exporter。
  - 指标采集间隔建议 30s-60s，`binflow_http_request_duration_seconds` histogram 的 bucket 分位按需调整。

- **复制（ADR-0021）**：
  - 复制调度器为服务内 goroutine（`scheduler_interval_seconds` 默认 60s），无需额外 sidecar。
  - 目标实例密码（`target_password_enc`）通过 REST API 提交时以明文传输（HTTPS 保护），服务端 AES-256-GCM 加密后入库。
  - 复制为单向 push，多实例互通需在各实例上分别配置 `replications`。

| 形态 | 卷（disk 后端） | 卷（S3 后端） | 健康检查 | 说明 |
|---|---|---|---|---|
| 单二进制 | `<data_dir>`（blobs+sessions+db） | 仅 `<data_dir>`（db+sessions） | `/healthz` `/readyz` `/metrics` | S3 后端时 data_dir 仅存储 SQLite 与瞬时会话 |
| Docker/compose | volume → `/var/lib/binflow` | 仅 db+sessions volume | 同上 | S3 后端时 volume 可大幅缩小 |
| Helm/K8s | PVC(RWO) → `/var/lib/binflow` | **无 PVC**（S3）或小型 PVC（仅 db）/ emptyDir（sessions） | 同上 + ServiceMonitor | S3 后端时多副本实验性支持（§11.21） |
| 离线包 | 镜像 tar + chart + 脚本 | 同上 | 同上 | — |

公共约定：配置挂载点 `/etc/binflow/binflow.yaml`（env 优先级更高）；日志 stdout（12-factor）；优雅停机期 ≥ 30s（terminationGracePeriodSeconds 对齐 §7.4）。

---

## 10. 与 Artifactory 概念对齐（ADR-0003 执行表）

| Artifactory | BinFlow | 一致点 | 已知差异（有意） |
|---|---|---|---|
| repo key / node / checksum 术语 | 同名 | 命名 | — |
| filestore（checksum 路径） | `blobs/<xx>/<sha256>` | 内容寻址去重 | 不用硬链接多目录；无 blob sidecar 属性文件（属性进 SQLite）——**其 filestore 是否带 properties 文件待逆向规格确认** |
| Derby/Postgres | SQLite(WAL)/Postgres | 双栈 | 嵌入默认零依赖 |
| `artifactory.config.xml` | `repositories` 表 + YAML | — | 运行时可改仓配置，无 XML |
| remote 影子缓存仓（`<remoteKey>-cache`） | 缓存 node 直接落 remote 仓自身 repo_key + `remote_cache` 验证器表 | pull-through 语义等价 | 不做影子仓间接层（Artifactory 是其存储分片历史包袱，ADR-0012）；待 repo-semantics §7.3 M3 逆向印证后如有行为差异再评估 |
| virtual priorityResolution | 两桶序：priorityResolution 优先桶（桶内声明序）→ 其余成员（桶内声明序）（ADR-0013 联动记录，T-79 定案） | 聚合解析（Artifactory 四桶的两桶简化，客户端不可观察差异） | stale 命中即成员结果、真 404 续桶；写路由 defaultDeploymentRepo 见 PRD FR-21 |
| Access（用户/权限） | auth + users/tokens/permission_targets(+principals) 表 | 本地用户+token+命名 permission target ACL | M1 无组、无 SSO |
| Access 动作集 read/write/annotate/delete/manage（auth-model §4，高置信） | r/w/d + **[M7] m（manage）**（§3.4a） | manage 动作对齐 = 仓库级 admin（CanManageRepo 消费）；m 只判 repos[]、无路径子域 | annotate/distribute/managedXrayMeta 不做；Artifactory 无 read-only admin 角色——BinFlow `users.role` 闭集的 `readonly_admin` 是 Q4 需求的自有扩展（**有意差异**，ADR-0026） |
| `/api/` REST | 兼容子集 + `/api/v1` | 高频端点 | 全量兼容明确不做（PRODUCT）；统一挂 `/binflow` 前缀，不用 `/artifactory` 前缀、不做根路径镜像（ADR-0008） |

## 11. 已知妥协（技术债台账）

1. **Token 存 sha256 明文摘要**：高熵随机使可接受；若未来支持低熵 token 需换 argon2id。
2. **SQLite 单写者**：1000 并发读靠 WAL；写热点若出现按 ADR-0007 升级双池，不推架构。
3. **Session 崩溃恢复从 0 重传**（~~M1 现状~~ **T-209 引擎级收口、[M7] REST 面收口**）：ResumeSession 自 migration 010 起重哈希恢复（§3.1/§4.1）；[M7] §5.3.1 接通 HTTP 重暴露（lazy 重建 + `Session.Offset()` 权威 offset）。本条仅存历史脉络，设计债已清，剩实现票。
4. **审计「尽力而为」**：Append 失败不阻断业务；严格审计（两阶段）[M4+] 再议。
5. **remote_configs 密码列明文占位**：M3 接入前必须定静态加密方案（新 ADR）。
6. **metrics 缺位**：[M5] Prometheus；M1 仅结构化日志。
7. **单副本约束**：多副本 + 对象存储 [M6+]；此前 values/Helm 必须拦截多副本。
8. **匿名读的缓存不可见性**：remote 仓库 [M3] 若命中匿名读，代理层拉取上游使用仓配置凭据、审计 actor 记 `anonymous`；不因此放宽上游私有仓的写侧安全。
9. **GC 引用集全量驻内存**（T-9 review §1.1 代价注记）：集合形回调一次 `SELECT DISTINCT sha256 FROM nodes`，1M nodes ≈ 100MB 量级；M6+ 千万级 blob 需流式接口变体（届时新 ADR）。
10. **清扫仅启动时执行**（T-9 review 范围外发现）：长驻进程中被遗弃会话目录要等重启才清；`sweepSessions` 已就绪，后续票接线周期 ticker 即可，M1 接受。
11. ~~M2 chunked 断点续传降级~~（**N6/O-2 定案 [M7] §5.3.1**）：跨进程续传经 adapter lazy 重建接通（lazy 优于启动预载：启动时间不受在途会话量影响）；S3 后端维持 hard 404（§11.31）；干净停机保留未过期会话（~~清会话并限定「异常中断后」措辞~~——[M7] ADR-0028 终裁推翻，三径对称可续传，O-1 收口）。T-211 实测注记：GET 状态腿（204+Range）重启前已在且正确，T-216 范围 = 会话重建接线，非状态腿补齐。
12. **docker_gc 的 mark 集合扩容**：M2 起 GC 引用集合 = nodes ∪ docker_refs（§6 迁移 002 注记）；docker_manifests/docker_tags 行的级联清理由服务层维护（无 DB 级 FK 到复合主键部分列），一致性靠「manifest 删除同事务清 refs/tags」约定，QA 需覆盖孤儿 tag 用例。
13. **PutLandedBlob 用例缺口（T-38 review N2，T-48 登记）**：docker finalize 后 adapter 用 `store.Open` 把已落盘 blob 回读成流喂 `svc.Put`（uploads.go registerBlobNode）——绕过 §5.1「扩方法不绕过」条款，且每次 finalize 多一轮 O(size) 回读 + 三摘要重算（singleflight 免二份磁盘副本但 CPU/IO 不免；10GB 层推送是可感知延迟）。根因是 repo.Service 缺「从已提交 BlobRef 建 blobs 台账行 + node」的用例（PutFromBlob 拒孤儿台账、Put 只吃 body）。处置：**M3 前为 repo.Service 增 `PutLandedBlob(ctx, p, repoKey, path, ref, mime)`**（创建台账行的 PutFromBlob 变体），docker finalize 与未来 maven/npm chunked 上传共用，届时删除 adapter 回读（独立 dev-go-core 小票，不塞进 T-39）。
14. **remote 缓存无主动失效（ADR-0012 边界）**：M3 只有 TTL + 条件再验证 + blocked_out 手动遮蔽；`DELETE /api/v1/repositories/{key}` 外的「按路径强制失效」端点不做（M4 治理面）。idx_remote_cache_expiry 已留周期清扫缝。
15. **virtual 多源归并择优不做（ADR-0013 边界）**：首命中即返回，无 per-layout 版本比较器；`X-BinFlow-Resolved-From` 头让用户可诊断「拿到旧版本」问题。M4+ 若有需求（latest 择优）再引入 MetadataProvider 的版本比较器接口。
16. **npm packument 整档存储**：合并写放大（大包多 version 时每次 publish 重写整份 packument JSON）；M3 规模可接受，M6+ 若有巨型包性能问题再拆 per-version 行。
17. **配额计数与 docker/maven 服务端写不同链**（ADR-0015 落地注记）：PutFromBlob/PutLandedBlob/PutOpts 全族必须都过配额预检，实现票须覆盖 docker finalize 与 maven metadata 计算器两条服务端写路径，防旁路。
18. **web_sessions 清扫复用启动清扫模式**（与 storage sessions/ 同构但不同表）：长驻进程的过期会话行要等重启才清；量大前（M4 会话量低）接受，M5+ 可与 storage 周期清扫同票接线。
19. **备份不含 web_sessions（remote_cache 随 db 走）**：导出面 = db（含 remote_cache 表）+ blobs——即 remote 缓存元数据随 db 走、缓存 blob 随 blobs/ 走，语义自洽；web_sessions 属运行态不入备份（export 时从快照 purge，恢复后用户重登）。（标题勘误 T-112：原「不含 remote_cache/web_sessions」与正文「db 含 remote_cache 表」自相矛盾——实现按正文语义，T-96 review N7。）
20. **remote 缓存落节点不材料化祖先 folder 行（ADR-0016 边界）**：remote engine 直写 `Nodes().Put` 不经 putNode，缓存树的目录无 folder 行——M4 无 remote 目录浏览面（repo-semantics §7.1 listRemoteFolderItems 默认 false、console 树只浏览 local 仓），不可观察；M5+ 若开 remote 目录浏览/聚合（repo-semantics §8.5），engine 落节点须改经 repo.Service 或共享材料化助手（届时与 §4.5 增量票合并评估）。

21. **S3 后端 Open 不返回 Seek（ADR-0019 决策 4）**：`Engine.Open` 返回 `io.ReadCloser`，disk 实现仍返回可 Seek 的类型（类型断言可用），S3 实现返回 `GetObject` body。需要 Range 的调用方（docker blob GET）在 adapter 层通过 HTTP Range 头直接请求 S3 的部分对象，而非 Go 层 Seek（§4.7 限制 1）。

22. **S3 后端单实例共享桶时无分布式锁（ADR-0019 限制 2）**：disk 的 per-checksum singleflight 在进程内有效，S3 侧无跨进程 blob 写锁——同 blob 并发写最后一个 `CompleteMultipartUpload` 获胜，S3 `LastModified` 更新。M6 多副本为实验性，正式 HA 需引入分布式锁（Postgres advisory lock 或 Redis）。

23. **S3 后端备份/恢复不覆盖（ADR-0019 限制 3）**：§7.6 的 CLI export/import 仅支持 disk 后端。S3 后端的备份/恢复 [M7+] 再议（S3 版本控制或跨区域复制方案）。

24. **SQLite 写冲突在 S3 后端的多副本场景（ADR-0019）**：S3 后端 blob 数据在对象存储，但 SQLite 元数据仍在本地磁盘——多副本共享 S3 但各有独立 SQLite 时，元数据不一致（副本 A 创建的 node 行，副本 B 不可见）。M6 单副本为推荐配置；多副本需共享 Postgres 元数据后端 + S3 blob 存储（届时元数据写冲突消除）。

25. **OIDC/LDAP 用户自动创建后的权限管理（ADR-0020）**：`auto_provision: true` 创建的用户 `is_admin=false`，初次登录后无任何仓库权限——需管理员手动分配 permission target。后续版本可考虑 OIDC claim → group 映射或默认角色。

26. **OIDC session 与本地 session 的 TTL 共用（ADR-0020）**：OIDC 登录签发的是标准 `web_sessions` 行，TTL 与本地密码登录一致（`console.session_ttl_hours`）。OIDC ID Token 自身的 `exp` 不控制 BinFlow 会话时长——OIDC 只用于初次认证，后续请求不验证 ID Token 新鲜度。如需强制周期性 OIDC 重认证，需额外配置 `max_session_age`（M7+）。

27. **复制为单向 push 无双向同步（ADR-0021 决策 1）**：M6 只实现 push 复制（制品 Put 后异步触发）+ pull 复制（复用 remote 机制）。双向同步（active-active）为 M7+ 需求，需冲突解决策略（last-write-wins 或 CRDT）。

28. **复制任务无去重（ADR-0021）**：同一 blob 被多次触发复制时，`replication_tasks` 表可能产生多条 pending 记录（不同 `node_path` 但同 `blob_sha256`）。目标端 blob 去重由目标实例的存储引擎完成（sha256 主键），但网络传输浪费。后续可加 `(replication_id, blob_sha256)` 的 pending 去重。

29. **Prometheus 指标为自写格式非官方 client（ADR-0022 决策 1）**：为避免新增依赖（`prometheus/client_golang` 及其传递依赖树），M6 使用 stdlib `expvar` + 自写 Prometheus text format 序列化器。指标类型（counter/gauge/histogram）正确性需在实现层严格保证，且 histogram 的 `_bucket`/`_sum`/`_count` 后缀规则必须符合 Prometheus 数据模型。若后续社区要求 OpenTelemetry 集成，可按 ADR-0005 重新评估依赖准入。

30. **[M7] repo-admin 无过滤列表**（ADR-0026 边界）：仓库列表/详情对 readonly_admin 全量可见、对 repo-admin（`m` 授权的 user）**不做按授权过滤的列表视图**——repo-admin 需已知 repo key 才能寻址单仓配置族（CanManageRepo 逐仓判定）。过滤列表（「只看我管的仓」）若控制台有诉求，M8+ 评估（需 target 覆盖域反查，成本 = Can 的求值逆问题）。

31. **[M7] S3 后端无 REST 续传**（§5.3.1 契约 5）：multipart 会话不落 `upload_sessions` 表、S3 ResumeSession 恒 ErrSessionNotFound——S3 形态下 docker 重启后 uploads URL 仍 404、客户端重传。若做：upload ID 落表 + S3 ResumeSession（ListParts 重建 part 清单与 offset），M8+ 评估（届时新 ADR 或本条扩写）。

32. **[M7] token 二次认证的边界**（ADR-0027 Accepted，T-214 终版）：作用域 = **全部非 admin web session 臂**（含本地用户——T-214 扩围：威胁载体是 session 臂本身而非 IdP，本地部署是主体形态）；OIDC 腿 = `prompt=login` 重认证换发单次 mint grant（body `step_up_grant`，ADR-0027 决策 4）——~~只收 body id_token 新鲜性校验~~（否决：console 留存 id_token 的浏览器凭据留存面会在新鲜窗口内被 XSS 无声击穿）；Bearer-OIDC 臂（ID Token 直作 Bearer）铸 Token 不触发——该臂本身即新鲜凭据；`/v2/token` 与 Basic 臂不触发；默认关闭（`auth.token_step_up`）。

33. **[M7] 债务包 E 的非设计项**（conductor 种子第 E 条，登记防丢）：N3 ctx 取消窄窗（Append EOF→SetState 用请求 ctx，恰取消即毒化完好会话——实现票以 `context.WithoutCancel` 收口，fail-closed 现状可接受）；N2 `TestV2BlobSessionSweepResidue` restart 臂注释与断言空洞化修正（Close 语义 ADR-0028 变更后该臂重评——Close 后重新种入过期行+目录再断言 sweep）；O-1 收口动作 = §5.3.1 契约 7 的 Close **保留清单** INFO 日志（ADR-0028——保留而非清册）；`internal/auth` 53 条既有 lint 清零；008/009 迁移 .sql 行尾换行补齐。

34. **[M8] e2e 断言迁移债**（ADR-0029 决策 3/6 的代价面）：IA 重排改变应用内路由后，web/e2e 既有 18 spec（4448 行）中的**路径断言**需逐票同步（testid/交互断言按 §13.5 规则沿用）；迁移期间「路径断言未跟上 IA」会被误读为回归——tech-lead 分票时每个改路由的 UI 票必须同票携带其 e2e 路径断言更新，禁止跨票欠账。data-testid 命名规则（console-ux §10.1）在 M8 冻结——锚改名仅允许随组件语义变化，逐票回写 §10 清单。

35. **[M8] 契约冻结的张力登记**：行为规格（docs/reverse/console-ui.md）产出后，预期存在「Artifactory 操作流在 BinFlow 既有 API 面上表达不了」的缺口（候选：dashboard 聚合卡片、仓库列表的过滤/排序参数、Set Me Up 的上下文数据）。ADR-0029 决策 4 的例外通道（PM 出 FR + architect 评审独立票）是唯一出口；若 M8 PRD 立项的聚合端点超过 ~3 个，应视为 IA 对齐口径过宽的信号，回本节重评（对齐「BinFlow 已有功能面」的呈现，不为想象中的操作流扩后端）。

## 12. 待逆向规格确认清单（阻塞点挂 docs/reverse/）

| # | 问题 | 规格文件 | 影响面 |
|---|---|---|---|
| 1 | Artifactory filestore 是否有 blob sidecar/properties 文件 | storage-layout.md | §4.1（已按无 sidecar 设计，仅记录差异） |
| 2 | 兼容层错误体确切 JSON 形状 | rest-api.md | §7.3 |
| 3 | Generic 仓库目录列表行为（HTML/JSON/有无） | rest-api.md | §5.2 |
| 4 | `X-Checksum-*` 头不全给时的校验策略 | rest-api.md | §4.2 |
| 5 | Artifactory 首启 admin 引导行为（BinFlow 已定案：env 优先/缺省 password，ADR-0009；逆向仅用于文档对齐描述） | config-formats.md | §6 种子数据 |
| 6 | Artifactory 匿名读默认值与其「匿名仅 GET 内容」边界（用作 ADR-0009 的对齐佐证） | auth-model.md | §7.1 认证分层 |
| 7 [M2] | 单段 name（无 repo 前缀）的 404 行为、`/v2/_catalog` 匿名是否放宽、Www-Authenticate realm/service 参数确切形态 | docker-registry.md（T-31 产出后） | §5.3 路由与 token 流 |
| 8 [M3] | ~~virtual priorityResolution 精确语义~~（已由 repo-semantics §8.1 定案：两桶序，ADR-0013 联动记录收口）、remote 影子缓存仓行为（`<key>-cache` 在 Artifactory 的可见性/清理语义）、maven-metadata.xml 合并算法细节（version 去重/排序/placement——C1 已定案大部分） | repo-semantics.md §7.3/§8（余项） | §5.4 接线、§6 003 注释 |
| 9 [M3] | npm publish 重复 version 状态码（npm 官方语义 403 vs Artifactory）、twine 重复 filename 码、sha512 integrity 的校验策略 | maven-npm-pypi 规格票（M3）+ M3 PRD | §5.4.2/§5.4.3 |
| 10 [M6] | Artifactory OIDC 登录流的精确行为（callback URL 路径、state/code 参数形态、ID Token 验证细节）——作为 ADR-0020 的对齐参考 | auth-model.md（存续增补） | §3.4/§7.1 |
| 11 [M6] | Artifactory 复制/联邦的 push/pull 触发语义（trigger 事件、retry 策略、进度追踪 API 形状）——作为 ADR-0021 的对齐参考 | repo-semantics.md（存续增补）/ federation.md | §7.1/§8 |
| 12 [M6] | Artifactory S3 存储后端的 blob 布局（checksum 路径是否一致、multipart upload 的 session 语义差异）——作为 ADR-0019 对齐验证 | storage-layout.md（存续增补） | §4.7 |
| 13 [M6] | Artifactory Prometheus/expvar 指标名命名惯例（metric name prefix、label 命名风格）——作为 ADR-0022 的对齐参考（非块——Prometheus 无厂商标准，仅一致性佐证） | metrics.md（新） | §7.1 |
| 14 [M7] | ~~Artifactory `manage` 动作的精确边界（是否路径作用域、对 REST 仓库配置端点的实际映射）；group 是否可承载 admin/角色语义（BinFlow role 用户级 only 的对齐佐证）；Artifactory 是否存在 read-only admin 等价物（readonly_admin 有意差异的取证）~~ **已回答**（2026-08-23，rbac-model.md，M7 种子 A 校准规格：实例级无角色层/无 read-only admin〔#1/#2 高置信〕、组级 adminPrivileges 存在但 BinFlow 有意不跟进、manage 为 ACE 动作〔auth-model §4〕而仓库级 admin 正式对应物在 project 域——BinFlow 无 projects，target 的 m 是最小诚实同构，rbac-model §5 建议 2 背书） | rbac-model.md（已交付）+ auth-model.md §4 | §3.4a / §10 / ADR-0026（T-214 收口） |
| 15 [M8] | Artifactory 控制台的 IA / 交互流 / 操作流行为规格（导航分组与页面归属、Artifacts 树+详情双栏行为、Set Me Up 面板数据、权限矩阵编辑动线、四态与确认对话时机）——**M8 全部 UI 票的前置依赖**；产出限定 = 行为规格（布局描述/交互流表/组件清单/状态矩阵 + 置信度），素材边界见 ADR-0029 决策 2（官方文档 + 本地 OSS 容器行为观察；禁止产物拆解与像素取证） | console-ui.md（新，reverse-engineer） | §13 全节 / ADR-0029 |

---

## 13. [M8] 控制台承载层重排——架构约束（ADR-0029 展开）

> M8 主轴 = 用户指令「前端 UI 和交互逻辑要求和 JFrog 一样」。定性：**承载层（web/）重排，不是后端重写**。本节是 M8 全部 UI 票的约束源；对齐口径与 clean-room 细则以 ADR-0029 为准（Proposed——PRD 定稿后转正，转正可带勘误）。

### 13.1 冻结面（M8 不可触碰的不变量）

1. **服务端契约零改动**：REST wire（`/api/v1/**`、兼容层 `/api/security/**`、`/v2/**`、session 族 `POST/GET/DELETE /api/v1/session`）、RBAC 判定（§3.4a / ADR-0026 六能力闭集与角色短路）、step-up（ADR-0027）、上传续传语义（§5.3.1 / ADR-0028）、错误信封（§7.3）。回归背书 = **M1~M7 既有 REST 测试零改动全绿**——任何「必须改后端断言才能过」的 UI 票即违约信号。例外通道：仅 PM 出 FR + architect 评审的独立票（§11.35 登记张力与熔断线）。
2. **挂载与资源前缀不变性**（ADR-0014 + T-108/T-110 勘误口径维持）：SPA 段 `/binflow/ui/**`（深链 fallback 段内回 index.html）、指纹资产共享段 `/binflow/assets/<hash>.<ext>`（immutable 缓存）、`GET /binflow/` → 301 → `/binflow/ui/`、SPA basename `/binflow/ui`（vite base + relink-assets 构建链不改）、repo key 保留字并集 {api, v2, docs, console, ui, assets}。UI 资源前缀是对外 URL 兼容承诺的一部分（书签/文档/反代规则消费），M8 重排不得触碰。
3. **go:embed 构建链不变**：`web/` 源码 → `vite build && node scripts/relink-assets.mjs` → 产物复制进 `internal/console/dist` → `go:embed` 打入单二进制（`make console`）。零运行时 CDN 依赖红线（ADR-0014 决策 4）维持——对齐 Artifactory 的任何 UI 能力都不得引入外链字体/图标 CDN。
4. **认证与浏览器会话形态不变**：server-side session + `binflow_session` cookie（HttpOnly + Path=/binflow + SameSite=Lax）、CSRF 两层（SameSite + Origin 同源校验）、`X-BinFlow-Console` 习惯头、401 全局监听与会话过期重登流（lib/api.ts 既有契约）。IA 重排后的所有页面继续消费同一 AuthContext 信号（admin / readonly_admin / anonymous 三态）。
5. **范围红线**：只对齐 BinFlow 已有功能面的呈现；Xray / Pipelines / Build-info 等 JFrog 独立产品不做**且不占位**（导航不出现无功能对应的入口，含禁用态——ADR-0029 决策 5）。

### 13.2 变更面（允许重排的层）

- `web/src` 内部一切：应用内路由路径（console-ux §3.2 路由表随之升版）、导航 IA、页面组件结构、样式皮肤。
- `docs/design/console-ux.md` 升 v2：IA 重排后的信息架构 / 线框 / 交互四态 / testid 清单；§7 视觉 token 节保留并演进（自有皮肤，非重写为他人皮肤）。
- 约束：应用内路由变更必须同票完成三件事——路由表回写、深链回归、e2e 路径断言更新（§11.34）。

### 13.3 前置依赖（排期硬序）

1. **ADR-0029 转正**（M8 PRD 定稿时）。
2. **docs/reverse/console-ui.md 行为规格**（reverse-engineer，§12 表第 15 行）：导航树、页面归属、Artifacts 树+详情双栏行为、建仓/上传/授权/GC 操作流步骤表、四态与确认对话矩阵、Set Me Up 面板数据面——带置信度标注。UI 实现票只依据规格 + console-ux v2 编码，不直接对照 Artifactory 实例（clean-room 规格与实现分离，UI 域细则 = ADR-0029 决策 2）。
3. console-ux v2（ux-designer 消费规格转译）——tech-lead 依 §13.4 分票。

### 13.4 存量 web/src 存留判定（tech-lead 分票依据；不写代码）

**判定原则**：逻辑层（纯函数 / API 封装 / 权限判定辅助）**保留**；页面布局与导航（IA 表达层）**重构**（领域逻辑与数据获取留下，视图结构按行为规格重排）；视觉皮肤（styles）**重做**（自有 token 演进；CSS 是零复制红线的最外层）。

| 存档 | 文件 | 判定 | 依据 |
|---|---|---|---|
| **保留（不动或微调）** | `lib/api.ts` | 保留 | 统一请求层 + wire 类型 + 错误三格式收敛 + 401 监听——契约冻结的直接受益者；IA 重排对其零需求 |
| | `lib/format.ts` / `lib/useAsync.ts` / `lib/useVersion.ts` | 保留 | 通用工具，跨 IA 存活 |
| | `app/AuthContext.tsx` / `ThemeContext.tsx` / `ToastContext.tsx` | 保留 | Provider 组合与 M8 无冲突；AuthContext 的 whoami/admin/readonlyAdmin 信号是角色可见性基座（§13.1.4），IA 重排后仍唯一消费 |
| | `components/ConfirmDialog.tsx` / `CopyButton.tsx` / `Skeleton.tsx` / `ErrorCard.tsx` / `EmptyState.tsx` | 保留（可按行为规格扩展 API） | 通用原语；Artifactory 确认对话模式由 ConfirmDialog 承载或扩展，不另起炉灶 |
| | `pages/security/pathmatch.ts` + `pathmatch.fixtures.ts` + `targetdiff.ts`；`pages/repositories/tree/sha256.ts`、`tree/lib.ts`；`pages/repositories/commands.ts` | 保留 | 纯逻辑零 UI；commands.ts 是 Set Me Up 的对物——IA 对齐时**升格为组件化消费**（Artifactory Set Me Up 面板的逻辑底座已在） |
| | `lib/repos.ts` / `lib/governance.ts` / `pages/security/api.ts` | 保留 | 领域数据获取层，重构页面继续消费 |
| **重构（逻辑留、视图重排）** | `components/AppShell.tsx` | 重构 | IA 主战场：NAV 数组的数据结构（分组 + adminOnly + ticket 占位机制）可保留，分组与条目按行为规格重排；pageTitle 前缀匹配函数随之改；⌘K/对话框让位等交互逻辑保留 |
| | `main.tsx` 路由表 | 重构 | 懒加载缝（React.lazy）构建形态不变（T-89 既定）；路径与嵌套按 console-ux v2 §3.2 重写 |
| | `pages/` 全部页面组件（Dashboard / Login / Settings / repositories 组 / tree / search / security 组 / audit / governance 组） | 重构 | 视图结构按 Artifactory 模式重排（树+详情双栏、tab 归属、表格形态）；数据获取与提交逻辑（消费 lib/*）留下 |
| **重做（皮肤层）** | `styles/*`（tokens / base / pages / governance / security / tree css） | 重做 | 自有皮肤按 §7 token 演进；组件重排后类名体系随之重写；零复制红线最外层（不取色、不描摹） |
| **随票迁移** | `web/e2e/*`（18 spec） | 迁移 | testid/交互断言沿用、路径断言随路由表改（§13.5）；每个改路由的 UI 票同票携带 |

**分票 area 边界建议**（供 tech-lead）：按页面域切（壳+皮肤 / artifacts 域 / security 域 / governance+administration 域），lib/ 与 styles/ 的写权限归属须与消费页面票错峰或独占——沿用「area 不得重叠」规则。

### 13.5 Playwright e2e 策略——交互断言的适配（ADR-0029 决策 3）

1. **断言形态**：交互与信息可达性——点击路径可达、元素存在/可见/禁用、四态渲染、键盘流、路由跳转；锚 = data-testid（§10 命名规则冻结）。**禁止**引入 screenshot baseline / toHaveScreenshot 类视觉对比作为验收门。
2. **允许的样式断言**：computed style 对自有 token 的存在性校验（如暗色主题变量已应用到根节点）——断言「皮肤系统在工作」，不断言「长得像谁」。
3. **既有 18 spec 处置**：断言二分——testid/交互类沿用（组件重构后锚保持或按 §10 规则迁移）；路径类（URL/glob 断言）随路由表逐票同步（§11.34 禁止跨票欠账）。
4. **新增「操作流对照」spec 形态**：以行为规格的操作流表为锚生成——每行 = 步骤 N → 目标锚 → 期望状态（可见/可用/值/跳转）；这是「Artifactory 用户零学习成本」的可测等价物，替代像素对比成为 M8 的对齐验收面。
5. **回归面**：M1~M7 REST 测试零改动全绿（契约冻结背书，§13.1.1）+ 既有 e2e 迁移后全绿；CI `-timeout 20m` 债券与 e2e 增量的关系由 tech-lead 在 M8 排期时合并评估。

### 13.6 M8 债券的架构挂点（与 ADR-0029 正交，PRD 排期）

T-231（internal/client 上传路径 percent-encode 缺陷，含 %/#/?/空格/UTF-8 回归矩阵）属 Go 客户端面、与 UI 重排零耦合；UI 打磨 4 条若与 IA 重排同域，应并入对应 UI 票消化而非单独立票（避免同 area 双写）；其余（B-1 / CI timeout / V28 证据移植 / ROADMAP 勾账 / dialer 样板）均不触本节约束。
