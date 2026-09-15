# BinFlow 后端包面地图（证据指针文档——正文在既有产物）

> 指针层：`internal/` 24 包的职责索引。正文在 `docs/design/architecture.md`（§2 包结构 / §3 模块接口契约）与各 ADR。采集日 2026-09-14。

## 1. 包清单（internal/，按依赖方向粗分层）

| 层 | 包 | 职责（一句话） | 规范出处 |
|---|---|---|---|
| 入口/传输 | httpapi | REST/协议路由、鉴权中间件、WriteTimeout 纪律 | architecture §7 |
| | console | 控制台挂载路由、server-side session + CSRF、SPA/REST 边界 | ADR-0014 |
| 领域 | repo | 仓库聚合（rclass 三态、configJSON、license 建仓门 D3） | §2 / ADR-0033 |
| | storage | Engine/Backend 缝、blob 落盘、GC 资格窗口 | §4 / ADR-0006/0019/0031/0049 |
| | metadata | SQLite/PG 双态元数据仓（nodes/blobs/… 41 表族） | §6 / 本目录 database-model.md |
| | auth | 用户/组/密码/会话/OIDC/LDAP 联邦 | ADR-0020/0035 |
| | search | AQL 引擎（EBNF→AST→IR→SQL）+ 老搜索族 | ADR-0043 / aql.md |
| | remote | pull-through 代理、SSRF 防护、远端浏览 | ADR-0012 / remote-browsing.md |
| | replication | push 复制 + 事件驱动队列 | ADR-0021 |
| | build / bundle | Build-info / Release Bundle 最小面（M17） | ADR-0045/0046 |
| | scheduler | cron 台账 + 统一 ticker | ADR-0044 |
| | webhook | 单一 Emit 缝 + DB outbox 投递器 | ADR-0041 |
| | keypair | GPG keypair CRUD/密封存储 | ADR-0038 |
| | audit | 审计事件族 | §2 |
| 平台 | adapter | 包型协议适配器 SPI（Generic + 协议族） | §5 |
| | addons | addon 注册表（编译期装配清单） | ADR-0033 |
| | license | ed25519 文档、三档闭集、单点门控 | ADR-0032 |
| | metrics | Prometheus 暴露面 | ADR-0022 |
| | config | binflow.yaml + binstore.yaml 配置模型 | §8 / ADR-0036 |
| | migrate / client / docs | 迁移机制 / 内部客户端 / 嵌入文档 | ADR-0007/0024 |

## 2. 伴生二进制

`cmd/binflow-server`（主服务）、`cmd/bf`（CLI，ADR-0023）、`cmd/bf-migrate`（Artifactory 迁移，ADR-0024）。

## 3. 接口契约正文

模块间 Go 接口契约逐条在 `docs/design/architecture.md` §3（129~501 行区间）；本文件不复制。

## 4. 缺口声明（真无证据的面）

无。包面与接口均有规范 + 代码双源。
