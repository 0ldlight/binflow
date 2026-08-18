# BinFlow 架构设计（M1 定稿）

> architect 维护。本文件在 ADR-0001~0010 基线上给出可并行开发的实现蓝图：包边界 = 并行开发 area 边界。
> 标注 **[M2+]** / **[M3+]** 的内容当期不实现，只保证接口缝存在；标注「待逆向规格确认」的行为以 `docs/reverse/` 规格为准，规格冲突时先回 ADR。
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
        │     └──────┬─────┘  └───┬─────────┘   └────┬─────┘   └────┬────┘
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
│   ├── adapter/               # 协议 SPI：Handler 挂载 + layout
│   │   └── generic/           # M1 唯一实现
│   ├── auth/                  # Principal / Authorizer / TokenRegistry
│   ├── audit/                 # 审计事件 append + 查询
│   ├── httpapi/               # Server、路由表、middleware、错误信封
│   └── console/               # go:embed 前端资产 [M4]，M1 仅 JSON API
├── web/                       # 控制台前端源码（构建产物进 internal/console）
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
| `storage` | blob 生命周期：上传会话、checksum 计算、原子落盘、打开读、删除、GC | `Engine`（见 §3.1） | S3 后端（只留 `Backend` 缝 [M6+]）、remote 缓存清理 [M3] |
| `metadata` | 全部 SQL：repositories/nodes/blobs/users/tokens/audit_events 的 CRUD；迁移 | `Store`（见 §3.2）；`Migrate(ctx, dialect)` | 复杂查询优化、审计分库 |
| `repo` | 仓库语义：Get/Put/Delete/List/Search 的用例编排；local 实现 | `Service`（见 §3.3）；`GetLocal(ctx, key)` | remote/virtual（接口占位，M3 实现） |
| `adapter` | SPI：协议无关的 Handler 注册与路由挂载；`layout` 包 | `Handler` + `Register/All`（见 §5.1） | 各协议本体 |
| `adapter/generic` | Generic(raw) 语义：path 即 layout、上传校验、目录列表 | 实现 `Handler` | —— |
| `auth` | 密码校验（argon2id）、Token 签发/校验、路径 ACL 决策 | `Authenticator` / `Authorizer` / `TokenRegistry`（§3.4） | 组/匿名/LDAP [M4+] |
| `audit` | append-only 审计事件 + 查询 | `Logger`（§3.5） | UI、导出 [M4] |
| `httpapi` | 监听、路由表、middleware 链、统一错误信封、健康检查、优雅停机 | `Run(ctx, deps)`（§7） | —— |
| `console` | `//go:embed dist`；`Handler() http.Handler` | M1 返回占位页 | 前端本体 [M4] |

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
    // ResumeSession 按 id 恢复（chunked 上传 [M2] 需要）；M1 返回 ErrSessionNotFound。
    ResumeSession(ctx context.Context, id string) (Session, error)
    // Open 打开 blob 读取；调用方负责 Close。不存在 → ErrBlobNotFound（wrap）。
    // 返回的 BlobRef 只保证 Sha256+Size 有值；sha1/md5 的事实源是 metadata blobs 表
    //（无 sidecar，ADR-0006）——需要三摘要全量时用 Stat。（T-9 review 回写项 D）
    Open(ctx context.Context, sha256 string) (io.ReadSeekCloser, BlobRef, error)
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
    // Close 关闭引擎：排空并丢弃在途会话目录（残留由下次启动清扫兜底）、拒绝后续
    // BeginSession/Delete/GC（ErrEngineClosed）；Open/Stat 继续服务已提交 blob（只读
    // 不可变文件）。幂等。（T-9 review 回写项 B——§7.4 停机序列要求生命周期对称。）
    Close() error
}
```

错误约定（包级 sentinel，wrap 后仍 `errors.Is` 可判；T-9 review 回写项 C/H 补全）：
`var ErrBlobNotFound`（Open/Stat/Delete 未命中）、`ErrSessionNotFound`（ResumeSession；早期文档误写 ErrNoSuchSession，以本名为准）、`ErrChecksumMismatch`（Commit 期望摘要不符，不落盘）、`ErrBlobCorrupt`（Stat 完整性校验失败）、`ErrEngineClosed`（Close 后的变更操作）、`ErrSessionPoisoned`（Append 失败后的会话毒化，双 %w 错误链）。

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
    // 三型统一入口；M1 只实现 local 分支，remote/virtual 返回 ErrNotImplemented。
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

### 3.4 auth（owner: dev-go-core）

```go
package auth

type Principal struct { Name string; Admin bool; TokenID int64 } // TokenID>0 表示 token 认证

type Authenticator interface {
    // 支持：Basic（user:password 或 user:token）、Bearer <token> [M2 docker]。
    Authenticate(ctx context.Context, r *http.Request) (*Principal, error) // nil,nil = 匿名
}
type Authorizer interface {
    // M1 规则：admin 全通过；否则按命名 permission target（见 §6 permission_targets 表，PRD E-24）
    // 判定：repo 命中 repos[] 且 path 命中 includePatterns（**/* 两级通配）且不命中 excludePatterns
    //（exclude 优先）→ 按 principals 中该用户的 actions 授 r/w/d；无命中 = 拒绝。
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

```
<data-dir>/                          # 默认 ./data，可配
├── blobs/
│   └── <sha256[0:2]>/<sha256>       # 内容寻址，全局去重，文件不可变
│       例: blobs/ab/ab530313...     # 无扩展名、无 sidecar
├── sessions/
│   └── <uuid>/
│       ├── data                     # 追加写目标
│       └── state.json               # 形状见下方契约
├── binflow.db                       # SQLite（WAL 模式）；Postgres 形态下不存在
└── binflow.db-wal / -shm
```

**state.json 契约**（T-9 review 回写项 F + 修复票定稿；session 属瞬态文件，非 ADR-0006 的兼容承诺面——兼容承诺仅 `blobs/`+`sessions/` 目录命名，备份面 = blobs/ + SQLite 文件）：

```json
{"version": 1, "id": "<uuid>", "created_at": "<RFC3339>", "received": 0, "sha256": null}
```

- `version`：形状版本号，**v1 = 上形状**；演进规则（architect 定稿）：字段新增必须 bump version，读侧遇到未知 version 视为不可恢复（按过期清扫处理，M1 从 0 重传语义不变）；字段不得删除与改名。
- `sha256`：恒为 `null` 字面量——摘要不落盘（崩溃使运行中哈希链失效），M1 恢复即从 0 重传（ResumeSession 是 [M2] chunked 的缝）。
- `received`：磁盘已落字节数，M1 每次 Append 后更新（文件永不撒谎）；M1 无读者，[M2] chunked 续传以其为 offset 基准。
```

### 4.2 checksum 与去重语义

- **主键 sha256**（hex 小写）。sha1/md5 是附属校验：客户端提供则必须匹配（`X-Checksum-<ALGO>` 头，Artifactory 兼容名）；不提供则服务端计算并存 `blobs` 表供 Maven/PyPI 协议下载校验文件用 [M3]。
- **去重单位是 blob 而非制品**：两个仓库各放同一 jar，磁盘一份、`nodes` 两行。跨仓移动/复制制品 = 元数据行变更，零字节拷贝。
- 上传到 `Commit` 时若 `blobs/<xx>/<sha256>` 已存在：直接删除会话数据返回既有 `BlobRef`（进程内还用 per-checksum singleflight 把并发同 blob 的写收敛成一个落盘者，其余等待后读现成文件）。

### 4.3 上传会话状态机

```
created --Append(可多次)--> appending --Commit--> committed(终态, session 目录删除)
    │                            │
    └──Abort/过期----------------┴--> aborted(终态, 目录删除)
```

落盘协议（顺序不可换）：write(data) → **fsync(data)** → rename(data → blobs/xx/sha256)（目标已存在则丢弃）→ **fsync(blobs/xx 目录)** → 元数据事务。崩溃窗口分析：任一点断电，最坏残留 = 完整但未引用的 blob（GC 回收）或 session 残渣（启动清理），**不存在半写 blob 暴露给读者**。

### 4.4 引用与 GC（M1 最小实现）

- 引用事实 = `nodes.sha256` 集合；`blobs` 行是「曾经存在」（node 全删后 blob 行保留，供 GC 反查附属摘要）。
- 运行时 `Delete` 制品只删 `nodes` 行（软删语义在物理层）。物理回收唯一入口：`binflow-server gc [--apply]`，mark（**引用集合回调**，调用方实现 `SELECT DISTINCT sha256 FROM nodes`，一次查询——T-9 集合形，取代原 FilterUnreferenced 反连接逐条路线）→ sweep（storage 扫描 `blobs/` 磁盘，未引用且**文件 mtime** 早于 `now-grace` 才删；删除后同时清 `blobs` 行）。默认 dry-run 打印清单，`--apply` 才真删（安全底线）。
- **grace 基准是 blob 文件 mtime，不是 `blobs.created_at` 列**（T-9 回写项 G；storage 不读 DB 的必然选择，方向安全——mtime 被推新只会多保留）。**硬约束：备份/恢复工具必须保留 mtime（`tar` / `rsync -a` 默认保留；勿用会重置时间戳的复制方式），否则宽限期时钟被重置**——M4 备份票与 ops 文档必须遵守。
- grace 与 session ttl 的零值语义：`<= 0` 一律取默认 24h（T-9 回写项 J）——零值不是「立即回收」；要无宽限期须显式传亚秒时长。
- 启动时清扫：`sessions/` 下会话目录按 state.json `created_at`（缺失回退目录 mtime）超 ttl（默认 24h）删除；活会话（近期 mtime）受保护。
- `FilterUnreferenced`（metadata）自 GC 链路退役，转为对账/一致性检查用途（见 §3.2 注释，回写项 I）。

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
    // generic {"local"}；docker M2 {"local"}（remote/virtual docker 顺延 M3）。
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

因此 repo key 保留名校验：建仓时拒绝 `api`、`v2`（repo.Service 校验，`/binflow/v2` 双挂载**不提供**——ADR-0010 裁决）。

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
1. **offset 语义 vs 流式**：Registry chunked 协议是「断点对齐追加」（客户端持 offset、服务端必须可查询与续传），storage.Session 是「黑盒流式追加」。对接法：adapter 持有协议状态（`received` 由 state.json 持久化），PATCH 时先校验 `Content-Range` 与 `received` 对齐再调 `Append`；PUT finalize 用 `Commit(expect)` 一次性收口。**M1 的 ResumeSession 仍恒 ErrSessionNotFound**：M2 续传实现 = 磁盘 `received` 已持久（state.json），新会话不复用——客户端重发从其对齐点开始（docker 客户端 PATCH 失败会从头重传 uploads 会话，spec 允许；真正的跨进程断点续传 [M3+] 再启用 ResumeSession）。
2. **digest 算法**：Registry digest 形如 `sha256:<hex>`；BinFlow blob 主键就是裸 hex sha256——adapter 只剥 `sha256:` 前缀，无算法转换（spec 允许其他算法，M2 只实现 sha256，收到 `sha512:` 等 → 400 Unsupported）。
3. **cross-repo blob mount**（`?mount=<digest>&from=<repo>`）：POST uploads 带 mount 参数且目标 blob 已被 from-repo 引用 → 直接走 `repo.Service.PutFromBlob` 零拷贝挂账（M1 已备该契约）；mount 失败按 spec 降级为普通上传会话（202 + Location，不报错）。

**manifest 引用完整性（mediaType 校验链）**：PUT manifest 时逐条校验——① manifest 自身 mediaType ∈ {`application/vnd.docker.distribution.manifest.v2+json`, `...manifest.list.v2+json`, `application/vnd.oci.image.manifest.v1+json`, `application/vnd.oci.image.index.v1+json`}（M2 四种，其余 415/400）；② 逐 config/layer digest 查 `nodes`（本 repo 内）已存在，缺一即拒（防悬空引用——manifest blob 落盘先于校验失败则成为无引用 blob，GC 兜底，不损数据）；③ list/index 的嵌套 manifest digest 递归同校验（只查在场性，不递归解析其内部——spec 允许 lazy）。

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
  repo_key  TEXT PRIMARY KEY,              -- 唯一标识，[a-z][a-z0-9-]{1,62}（PRD FR-3-AC4；原架构 {1,31} 作废，后经 T-22 回写）；保留字 api/v2 禁用（ADR-0008 路由分发依赖）
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
  path      TEXT NOT NULL,                 -- repo 内相对路径，'/' 分隔，无前导 '/'
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
-- docker 制品的 node 布局约定（不加表，复用 nodes）：
--   manifest blob 的 node path = "<image>/manifests/<digest-hex>"；
--   layer/config blob 的 node path = "<image>/blobs/<digest-hex>"（image = name 去掉 repo key 首段后的相对名，
--   可含 '/'）。digest 寻址直达 nodes 主键，无需 JOIN。
CREATE TABLE docker_manifests (           -- manifest 元数据（manifest 本体是普通 blob/node）
  repo_key  TEXT NOT NULL REFERENCES repositories(repo_key) ON DELETE CASCADE,
  image     TEXT NOT NULL,                -- 镜像相对名（不含 repo key 首段）
  digest    TEXT NOT NULL,                -- 裸 hex sha256（blob 主键同源）
  media_type TEXT NOT NULL,               -- §5.3 四种之一
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

首启种子数据（迁移 001 内）：预置 `admin` 用户（is_admin=1）。口令引导（ADR-0009，用户定案）：env `BINFLOW_ADMIN_PASSWORD` 优先；未设置时使用**文档化缺省值 `password`**（仅限评估——文档与启动日志双重标注，检测到缺省值时启动打 WARN）；仅在 admin 用户不存在时生效，改密后不被后续启动覆盖。

模块路径：go.mod `module github.com/lzwzzy/binflow`（ADR-0008），所有 import 以此为根。

---

## 7. HTTP 层（httpapi）

### 7.1 路由表（stdlib ServeMux，ADR-0005）

```
/healthz            GET   存活（恒 200，无依赖检查）          → K8s liveness（基础端点，不带前缀）
/readyz             GET   就绪（metadata ping + storage 可写） → K8s readiness（同上）
/metrics            GET   [M5] Prometheus（同上）
/v2/...             *     docker adapter [M2]（**根级例外**，ADR-0010：不剥 /binflow 前缀，进同一
                          middleware 链；含 /v2/token 自有 token 端点；详见 §5.3）
/binflow/api/v1/... *     自有 API（稳定契约，全部需认证——匿名只作用于内容路径）：
  POST   /binflow/api/v1/tokens                    签发 token
  GET    /binflow/api/v1/users                     列用户（admin）
  POST   /binflow/api/v1/repositories              建仓
  GET    /binflow/api/v1/repositories              列仓
  GET    /binflow/api/v1/repositories/{key}        仓详情
  PUT    /binflow/api/v1/repositories/{key}        改仓
  DELETE /binflow/api/v1/repositories/{key}        删仓（级联删引用）
  GET    /binflow/api/v1/repositories/{key}/_list  制品列表（?prefix=）
  GET    /binflow/api/v1/audit                     审计查询（admin, ?repo=&actor=&since=）
/binflow/api/...    *     Artifactory 兼容子集 [按 docs/reverse/rest-api.md 逐步]（注意：兼容层路径不带 /artifactory 前缀，直接映射 /binflow/api/...）
/binflow/<repo>/... *     内容路径：按 repo.package_type 分发到 adapter（M1 = generic）
/binflow/           GET    console（M1: 占位 JSON；M4: go:embed SPA）
```

**认证分层默认值（ADR-0009）**：内容路径 `GET/HEAD` 匿名放行（`security.anonymous_access: true` 默认）；内容路径写操作与 `/binflow/api/**` 全部要求认证，不受该开关豁免。`anonymous_access: false` 时所有端点一律认证。

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

### 7.4 生命周期

优雅停机：SIGTERM → `server.Shutdown(ctx, 30s)`（等待在途上传 Commit 或超时丢弃 session）→ **关 storage.Engine（Close：排空在途会话，T-9 回写项 B）** → 关 metadata → 退出码 0。健康检查与停机语义是 ADR-0004 各部署形态的公共契约（§9）。

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
server:
  listen: ":8080"                # env BINFLOW_SERVER__LISTEN
  base_url: ""                   # 对外可见 URL（控制台链接/absolute path 用），空=请求推导
  graceful_timeout_seconds: 30
  cors_origins: []               # 空=同源限制
storage:
  data_dir: "./data"             # 唯一必须人工确认的路径；env 扁便捷拼写 BINFLOW_DATA_DIR（见四个例外名）
  session_ttl_hours: 24          # 零值 = 默认 24h（T-9 回写项 J）
  gc_grace_hours: 24             # 零值 = 默认 24h；grace 基准 = blob 文件 mtime（§4.4 硬约束）
metadata:
  driver: "sqlite"               # 'sqlite' | 'postgres'(M1 只实现 sqlite)
  dsn: ""                        # sqlite: 文件路径（空=data_dir/binflow.db）；postgres: URL
auth:
  argon2_memory_mb: 64
  token_default_ttl_hours: 720   # 30d

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

校验规则（`Load` 内，fail-fast）：port 可解析；`data_dir` 可创建/可写；driver ∈ 枚举；未知顶层键报错（防拼写静默失效）。`Config` 结构带 `Validate()` 与 `Defaults()`，表驱动测试覆盖每条规则。

---

## 9. 部署架构约定（ADR-0004 落地口径，供 release-engineer）

| 形态 | 进程 | 卷 | 健康检查 | 说明 |
|---|---|---|---|---|
| 单二进制 | `binflow-server serve -c binflow.yaml` | `<data_dir>`（blobs+sessions+db） | `/healthz` `/readyz` | systemd `Type=notify` 亦可 [M5] |
| Docker/compose | 同上，EXPOSE 8080 | volume → `/var/lib/binflow` | 同上 | 镜像内默认 `data_dir=/var/lib/binflow` |
| Helm/K8s | Deployment(1 副本，**M1 无 HA**) | PVC(RWO) → `/var/lib/binflow` | liveness=/healthz, readiness=/readyz | 多副本挂同 PVC 为**禁止**配置（values 校验拦截） |
| 离线包 | 镜像 tar + chart + 脚本 | 同上 | 同上 | 校验和齐全 |

**M2 增补（ADR-0010 裁决第 6 条）**：docker 可用性**不依赖反代**——单二进制/compose/Helm 形态下 `/v2/**` 由应用直接服务，`docker login <host>` 直连即可。已有 nginx/traefik 前置的用户可对 `/v2/` **直通不 rewrite**（`proxy_pass` 原样）；compose 产物（T-17 产物演进）默认**不加**反代组件，文档给「前置反代直通 `/v2/`」示例片段即可。

公共约定：配置挂载点 `/etc/binflow/binflow.yaml`（env 优先级更高）；日志 stdout（12-factor）；优雅停机期 ≥ 30s（terminationGracePeriodSeconds 对齐 §7.4）。

---

## 10. 与 Artifactory 概念对齐（ADR-0003 执行表）

| Artifactory | BinFlow | 一致点 | 已知差异（有意） |
|---|---|---|---|
| repo key / node / checksum 术语 | 同名 | 命名 | — |
| filestore（checksum 路径） | `blobs/<xx>/<sha256>` | 内容寻址去重 | 不用硬链接多目录；无 blob sidecar 属性文件（属性进 SQLite）——**其 filestore 是否带 properties 文件待逆向规格确认** |
| Derby/Postgres | SQLite(WAL)/Postgres | 双栈 | 嵌入默认零依赖 |
| `artifactory.config.xml` | `repositories` 表 + YAML | — | 运行时可改仓配置，无 XML |
| Access（用户/权限） | auth + users/tokens/permission_targets(+principals) 表 | 本地用户+token+命名 permission target ACL | M1 无组、无 SSO |
| `/api/` REST | 兼容子集 + `/api/v1` | 高频端点 | 全量兼容明确不做（PRODUCT）；统一挂 `/binflow` 前缀，不用 `/artifactory` 前缀、不做根路径镜像（ADR-0008） |

## 11. 已知妥协（技术债台账）

1. **Token 存 sha256 明文摘要**：高熵随机使可接受；若未来支持低熵 token 需换 argon2id。
2. **SQLite 单写者**：1000 并发读靠 WAL；写热点若出现按 ADR-0007 升级双池，不推架构。
3. **Session 崩溃恢复从 0 重传**：M1 无断点续传（Generic 单请求即可）；[M2] docker chunked 上传需要 offset 恢复，届时在 `state.json`（v1 形状，见 §4.1 契约）增加固定块摘要并 bump version——接口 `ResumeSession` 与 `received` 字段已留。
4. **审计「尽力而为」**：Append 失败不阻断业务；严格审计（两阶段）[M4+] 再议。
5. **remote_configs 密码列明文占位**：M3 接入前必须定静态加密方案（新 ADR）。
6. **metrics 缺位**：[M5] Prometheus；M1 仅结构化日志。
7. **单副本约束**：多副本 + 对象存储 [M6+]；此前 values/Helm 必须拦截多副本。
8. **匿名读的缓存不可见性**：remote 仓库 [M3] 若命中匿名读，代理层拉取上游使用仓配置凭据、审计 actor 记 `anonymous`；不因此放宽上游私有仓的写侧安全。
9. **GC 引用集全量驻内存**（T-9 review §1.1 代价注记）：集合形回调一次 `SELECT DISTINCT sha256 FROM nodes`，1M nodes ≈ 100MB 量级；M6+ 千万级 blob 需流式接口变体（届时新 ADR）。
10. **清扫仅启动时执行**（T-9 review 范围外发现）：长驻进程中被遗弃会话目录要等重启才清；`sweepSessions` 已就绪，后续票接线周期 ticker 即可，M1 接受。
11. **M2 chunked 断点续传降级**（ADR-0010/§5.3 裁定）：docker 分块上传的跨进程真续传（ResumeSession）暂不启用——offset 对齐由 state.json 的 `received` 支撑单会话内续传，客户端 PATCH 失败重传走 uploads 会话重建；跨进程续传 [M3+] 再启用（T-20 Range 已备 ReadSeekCloser 基础）。
12. **docker_gc 的 mark 集合扩容**：M2 起 GC 引用集合 = nodes ∪ docker_refs（§6 迁移 002 注记）；docker_manifests/docker_tags 行的级联清理由服务层维护（无 DB 级 FK 到复合主键部分列），一致性靠「manifest 删除同事务清 refs/tags」约定，QA 需覆盖孤儿 tag 用例。
13. **PutLandedBlob 用例缺口（T-38 review N2，T-48 登记）**：docker finalize 后 adapter 用 `store.Open` 把已落盘 blob 回读成流喂 `svc.Put`（uploads.go registerBlobNode）——绕过 §5.1「扩方法不绕过」条款，且每次 finalize 多一轮 O(size) 回读 + 三摘要重算（singleflight 免二份磁盘副本但 CPU/IO 不免；10GB 层推送是可感知延迟）。根因是 repo.Service 缺「从已提交 BlobRef 建 blobs 台账行 + node」的用例（PutFromBlob 拒孤儿台账、Put 只吃 body）。处置：**M3 前为 repo.Service 增 `PutLandedBlob(ctx, p, repoKey, path, ref, mime)`**（创建台账行的 PutFromBlob 变体），docker finalize 与未来 maven/npm chunked 上传共用，届时删除 adapter 回读（独立 dev-go-core 小票，不塞进 T-39）。

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
