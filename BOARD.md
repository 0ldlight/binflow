# 任务看板（BOARD）

> 唯一事实来源。**只有主会话（conductor）可以写本文件**，所有 subagent 只读。
> ticket 由 tech-lead 生成、主会话录入。当前里程碑：**M6+（展望/规划阶段）**。M1~M5 已完成，tag m1-done / m2-done / m3-done / m4-done / m5-done（2026-08-21）。

## 票据格式

```
- **T-<编号>** [P0|P1|P2] 标题 `role:<agent类型>` `area:<Go包/页面组/部署目标>` `dep:T-x,T-y`
  AC: ①可验证的验收标准 ②… ③…
```

- 优先级：P0 阻塞他人/当前里程碑关键路径；P1 本里程碑应完成；P2 可延后。
- `area` 示例：`internal/storage`、`internal/adapter/docker`、`internal/auth`、`web/src/pages`、`deploy/helm`。
  同一轮并行派发的 ticket，area 不得重叠。
- `dep` 列出必须先完成的 ticket；协议适配类 ticket 必须依赖对应的逆向规格票。

## 📥 待办（todo）

> **M6 正在开发**（2026-08-22）。装配缺口三处全部闭环（T-178/179/180 ✅）；**batch 3 已提交**（含 .gitignore 修复与被吞测试文件）；在途 T-163/T-184；T-165/T-168 待用户明示重派（卡主链五票）。

### Batch 3（全部完成）：T-151/T-152/T-154/T-155 已完成 ✅

### Batch 5: 复制与指标（dev-go-core）

- **T-163** [P1] Prometheus /metrics 端点（stdlib expvar 实现） `role:dev-go-core` `area:internal/metrics / internal/httpapi` `dep:T-149`
  AC: ① `internal/metrics/metrics.go`：`Registry` 基于 `sync.Map` 的并发安全指标存储；`Format()` 生成 Prometheus text format。四类指标：HTTP、存储、认证、复制。② `GET /metrics` 端点挂载（根级，与 `/healthz` 同级）。匿名可访问，可配 `metrics.require_auth=true` 限制。③ table-driven 单测：`metrics_test.go` 验证 Prometheus 格式正确性；`system_test.go` 验证 `/metrics` 端点。

### Batch 6: CLI 与迁移工具（devops-engineer + release-engineer）

- **T-165** [P2] internal/client 包（HTTP 客户端封装） `role:devops-engineer` `area:internal/client` `dep:T-149`
  AC: ① `internal/client/client.go`：`Client` 结构体（Base URL 拼接、auth 头注入、错误信封解析、重试、进度条回调）。不得 import `internal/storage`/`internal/metadata`/`internal/httpapi`。② `internal/client/repo.go`/`artifact.go`/`user.go`/`token.go`：CRUD 方法。③ table-driven 单测：`client_test.go` — 使用 `httptest` 模拟 BinFlow server。
  ▶ 2026-08-22：agent 中途被取消（瞬时 API 错误），**部分产出在盘**——conductor 复核 `go test ./internal/client/` 已 ok（101.9s），重派时先盘点半成品再续做（工作日志未写）。

- **T-166** [P2] bf CLI 四个子命令（cmd/bf/） `role:devops-engineer` `area:cmd/bf/` `dep:T-165`
  AC: ① `cmd/bf/main.go`：`flag` 包实现的子命令分发（非 cobra）。四个子命令：`bf repo create` / `bf artifact upload` / `bf user create` / `bf token create`。② 配置管理：`~/.bf/config.yaml`（YAML 格式，`profiles` 多 profile）。③ table-driven 单测：`main_test.go` 验证四个子命令 HTTP 调用链。

- **T-167** [P2] bf-migrate 迁移工具（Artifactory → BinFlow） `role:release-engineer` `area:cmd/bf-migrate/ / internal/migrate` `dep:T-165`
  AC: ① `cmd/bf-migrate/main.go`：`bf-migrate migrate` 子命令。三阶段迁移：repos → users → tokens。`--dry-run` 模式只统计不迁移。`--resume` 断点续传。② `internal/migrate/reader.go`/`converter.go`/`writer.go`：通过 Artifactory REST API 读取 → 转换 → 通过 `internal/client` 写入 BinFlow。③ table-driven 单测：`migrate_test.go` — 使用 mock Artifactory server + mock BinFlow server 验证。**Q9（待用户定案）**：验收环境。

### Batch 7: 部署与文档（devops-engineer + release-engineer + tech-writer）

- **T-168** [P2] M5 债务收编 — Go 1.26.6 + 优雅停机 + nginx SSL `role:devops-engineer` `area:go.mod / cmd/binflow-server / deploy/`
  AC: ① `go.mod` 升级到 Go 1.26.6（`go mod tidy` + 全量测试 race 绿）。`cmd/binflow-server` 优雅停机：SIGINT/SIGTERM 收到后 → 排空在途请求（graceful period 30s）→ 关闭 HTTP server → 关闭 Engine → 关闭 metadata Store。② `deploy/nginx/` 新增 SSL 配置模板。③ 验证：`make test` 全量 race 绿；`binflow-server` 收到 SIGTERM 后日志含 "shutting down gracefully"。
  ▶ 2026-08-22：agent 中途被取消（瞬时 API 错误），**部分产出在盘**——Go 工具链已升 1.26.6、优雅停机已实现（自有测试单跑通过）、`deploy/nginx/` 模板已建。**遗留红**：`cmd/binflow-server` 全量套件 `TestServeGracefulShutdownLog` 失败 + adapter 重复注册 panic。重派时先修这两处再走 AC③（工作日志未写）。

- **T-169** [P2] M5 债务收编 — G05 Windows 锁 + G15b systemd 裸机部署 `role:release-engineer` `area:internal/storage / deploy/systemd/`
  AC: ① `internal/storage/lock.go`：`AcquireDataLock` 在 Windows 上使用 `LockFileEx`。`deploy/systemd/` 新增 `binflow.service`。② `deploy/systemd/install.sh`：安装脚本。③ 验证：`make test` 全量 race 绿；Windows 交叉编译通过；`install.sh` 在 Linux 上执行无错误。

- **T-171** [P2] 文档 5 类（OIDC/LDAP/S3/bf CLI/迁移指南） `role:tech-writer` `area:docs-site/docs/` `dep:T-154,T-155,T-166,T-167`
  AC: ① `docs-site/docs/guides/` 新增：`oidc-config.md`、`ldap-config.md`、`s3-config.md`、`bf-cli.md`、`migrate-artifactory.md`。② `docs-site/docs/metrics/` 新增 `prometheus-reference.md`。③ 验证：`make docs` 构建通过；`/binflow/docs/guides/oidc-config` 200 可访问。

### Batch 8: QA 与集成（qa-engineer）

- **T-172** [P0] M6 回归基线 — 本地 filestore 下 M1~M5 全部 P0 序列复跑 `role:qa-engineer` `area:QA 全量（本地 filestore）` `dep:T-168`
  AC: ① M1 C 序列 P0 全绿。② M2 D 序列 P0 全绿。③ M3 M 序列 P0 全绿。④ M4 W 序列 P0 全绿。⑤ M5 G 序列 P0 全绿。⑥ 产出：QA 报告（H68），零失败零 5xx。

- **T-173** [P0] S3 后端下 M1~M5 全部 P0 序列复跑 + 兼容性验证 `role:qa-engineer` `area:QA 全量（S3 后端：MinIO）` `dep:T-164,T-172,T-178`
  AC: ① MinIO 容器上 M1~M5 全部 P0 序列复跑全绿。② S3 配置与健康检查（H07~H11）。③ 本地→S3 迁移（H12~H15）。④ S3 下 GC 与去重（H16~H18）。⑤ S3 下性能基线（H19~H21）。**Q8（待用户定案）**：AWS S3 验收为条件腿。（dep 增 T-178：S3 数据面接线是硬前置，2026-08-22）

- **T-174** [P0] OIDC + LDAP 集成验收（含认证臂优先级） `role:qa-engineer` `area:QA 认证（Keycloak + OpenLDAP）` `dep:T-157,T-158,T-179`
  AC: ① OIDC 登录流（H24~H29）：Keycloak SSO 登录 → 控制台 session 可用。② LDAP 登录流（H30~H35）：OpenLDAP 用户名密码登录 → session 创建。③ 认证臂优先级（H36~H38）：三种用户同时登录 → 各返回正确 source。④ 产出：QA 报告，含 IdP 版本与配置。

- **T-175** [P1] 复制多协议 + Prometheus 指标 + bf CLI + bf-migrate 集成验收 `role:qa-engineer` `area:QA 集成（两实例 + Prometheus + CLI + 迁移）` `dep:T-162,T-163,T-166,T-167,T-159,T-160`
  AC: ① 复制验收（H39~H51）：push 单向复制、replica 仓库只读、幂等等。② Prometheus 指标验收（H52~H55）：`/metrics` 端点 200 + 含 TYPE/HELP 行。③ bf CLI 验收（H56~H61）：四个子命令成功。④ bf-migrate 验收（H62~H67）：1 个 generic 仓库 100+ 制品迁移 → sha256 一致。⑤ 产出：QA 报告，含全部 H 序列结果。

### Batch 9: 核验发现的收尾债（conductor 建票，2026-08-22）

- **T-184** [P2] replication CRUD 面 docs 回写（T-180 遗留④） `role:architect` `area:docs/design`
  AC: ① architecture.md §7.1 路由表补 replications CRUD 四端点 + /replication/status（形状按 T-180 日志裁定表：bare array/201/204/409/target_password 只写不读/默认值）。② console-ux.md 治理组「复制」页补 CRUD 面说明（当前只读面板，CRUD UI 另票）。③ 同款「回写记录」题头。

## 🔨 进行中（doing）

- **T-163** [P1] Prometheus /metrics 端点 — 2026-08-22 已派发（T-180 收口释放 httpapi；详见 Batch 5 条目）。

- **T-184** [P2] replication CRUD 面 docs 回写 — 2026-08-22 已派发（新建票，见 Batch 9）。

## 🧪 测试中（qa）

（空）

## ✅ 已完成（done）

- **T-127** [P0] goreleaser 基线+版本注入（FR-34/PB-01/02） `role:devops-engineer` — done 2026-08-21（conductor 核验直收）
  六平台 CGO_ENABLED=0（linux/darwin/windows×amd64/arm64）；ldflags version/revision 注入三面一致（`--version`/启动日志/health.version）；check-size 全部 5-6MB（≤40MB）；裸 build 回退 dev；release.snapshot 模板；发布禁用。日志 reports/agents/T-127.md。

- **T-129** [P0] docs-site Docusaurus 脚手架+embed+/binflow/docs（K1 暂行形态） `role:devops-engineer` — done 2026-08-21（conductor 核验直收）
  Docusaurus 3.10 构建通过；搜索 @easyops-cn/docusaurus-search-local+nodejieba（K1 终裁）；baseUrl `/binflow/docs/` 零外链；`make docs` 构建+复制+docs-size（1.56MB）；go:embed Handler + router 匿名只读；docs 测试+路由集成测试全绿。日志 reports/agents/T-129.md。

- **T-128** [P0] FR-44 BE 材料化+007 回填（ADR-0016，照 T-119 草案；**待双 review**） `role:dev-go-core` — done 2026-08-21（conductor 核验，待双 review 后提交）
  materializeAncestors+putFolderRow+ensureFolderLedger；007 回填迁移（两步 SQL/递归 CTE/幂等）；12 测试适配；伴随修复 docs 重定向循环。repo+httpapi+docs 全套测试 race 绿。日志 reports/agents/T-128.md。

- **T-130** [P0] K1/K2 架构终裁+§7.1 两行补遗 `role:architect` — done 2026-08-21（conductor 核验直收）
  **K1（ADR-0011 增补①~⑤）**：搜索外挂 docusaurus-search-local+nodejieba（事实修正：内建搜索不搜正文）；baseUrl 原生前缀免 relink；fallback 阶梯至用户确认。**K2（新 ADR-0017）**：ghcr.io/lzwzzy/binflow 变体 tag、GA 无滚动 tag、基底 **distroless static-debian13**（修正 debian12 暂行）+ alpine:3.24、syft SBOM 最小面（cosign/SLSA M6+ 结构性理由）、Chart 仓库 GitHub Pages。§7.1 两行补遗；PRD v1.2。在途影响：T-129 搜索方案已知会、T-132 派单带 debian13 锚定。提交 3daa704。

- **T-1** [P0] M1 里程碑 PRD `role:product-manager` `area:docs/prd` — done 2026-08-17
  产出 docs/prd/milestone-1.md（486 行）：FR-1~FR-6 全 AC、26 端点兼容矩阵、C01~C30 验收命令。核验通过。
  8 项开放问题已定案（Q1 `/binflow` 前缀；Q2 匿名读默认开；Q3 缺省 password；Q4 如实版本；Q5 纯 Go SQLite；Q6 Range P2；Q7 github.com/lzwzzy/binflow；Q8 devops 起草）→ 回写为 T-4。
- **T-2** [P0] M1 架构设计与元数据 schema 定稿 `role:architect` `area:docs/design` — done 2026-08-17
  产出 docs/design/architecture.md（12 节）+ ADR-0005/0006/0007（零 CGO 依赖基线 modernc.org/sqlite、blob 布局与落盘协议、迁移机制）。核验通过。
  决策对齐修订（/binflow 前缀、匿名读、Q3 口令、Q7 路径）→ T-5。
- **T-4** [P0] PRD v1.1 回写 8 项已定案决策 `role:product-manager` `area:docs/prd` `dep:T-1` — done 2026-08-17
  docs/prd/milestone-1.md 升 v1.1（525 行）：§0 修订记录；Q1 全文 URL 改 /binflow（104 处）+ /artifactory/** 404 断言；Q2 匿名读默认开（FR-5-AC12/13、C23/C27、NFR-S8）；Q5 零 CGO（FR-1-AC7）；Q7 落定；§9 已决决策表。核验通过（残留 /artifactory 均为有意保留）。
- **T-5** [P0] 架构文档对齐用户决策（含 Q3 修订） `role:architect` `area:docs/design` `dep:T-2` — done 2026-08-17
  ADR-0008（/binflow 统一前缀 + module 路径 + docker /v2 例外风险）、ADR-0009（匿名读默认开 + admin env 口令引导）；§7.1 路由全前缀化、§6 种子数据改 env 优先/缺省 password、Derby/H2 排除记录入 ADR-0005。核验通过。
- **T-3** [P0] Artifactory M1 行为规格（clean-room） `role:reverse-engineer` `area:docs/reverse` — done 2026-08-17
  四份规格共 471 行：rest-api.md（~34 端点，高28/中10/低3）、storage-layout.md（sha1 分片/binaries+nodes 表行为/_pre 暂存 24h 清理）、config-formats.md（三 XML → YAML 映射）、repo-semantics.md（local 8 路径规则/上传 5 步/删除 9 场景）。核验通过（置信度标注齐全、clean-room 抽查无代码翻译）。
  高价值发现：同 checksum 幂等重传免覆盖权限检查（官方未记载）、统一错误体 errors[] 形态、回收站 14 天。PRD §5.5 六项校准项可回写（PM 增量修订，随下轮或 T-6 一并处理）。
- **T-6** [P0] M1 工程 ticket 拆解 `role:tech-lead` — done 2026-08-17
  14 张票（T-7~T-20）+ 分批表（最大波 3 张）+ R1~R9 风险清单，全文 reports/agents/T-6.md。核验通过（area 无重叠、宽度 ≤4、校准项固化进 AC、低置信度不作 AC）。R1/R2→T-21；R3~R6→T-22；R7→T-23；R8 处置合理照准。
- **T-21** [P0] PRD v1.2 校准回写 `role:product-manager` `area:docs/prd` `dep:T-3,T-6` — done 2026-08-17
  R1（checksum 不一致 409）/R2（建仓 200 纯文本）/§5.5 六项全部定案（改「校准记录」表）；增补两条规格（ETag/304/416、幂等重传注记）；token 字段标待 T-23。核验通过（旧口径无残留，对照表左列旧值为有意保留）。T-18 QA 依赖已解除。
- **T-22** [P0] architecture.md 回写 R3~R6 `role:architect` `area:docs/design` `dep:T-6` — done 2026-08-17
  R3 匿名读主键名 security.anonymous_access（别名双键等价）；R4 permission_targets+permission_principals 两表替换扁平表；R5 repo key {1,62}；R6 错误信封 errors[] 数组形。核验通过（grep 四处落点 + 旧形态零残留）。ADR-0008/0009 仅追加回写注记。遗留：M4 可在 breaking 窗口移除旧键别名。
- **T-7** [P0] 工程脚手架 `role:devops-engineer` `area:仓库根` — done 2026-08-17
  go.mod（github.com/lzwzzy/binflow, go 1.26）/ Makefile（build/test/lint/dev/clean，GOPROXY 镜像）/ .golangci.yml v2 / CI 三步 / cmd 骨架 / internal 九包 doc.go。conductor 亲测复现：build 2.58MB、test PASS、lint 0 issues、gofmt 空、CGO_ENABLED=0 通过。遗留：CI 首跑绿待推送后确认；go.sum 待首依赖生成。
- **T-23** [P1] 补逆向规格 auth-model.md `role:reverse-engineer` `area:docs/reverse` `dep:T-3` — done 2026-08-17
  273 行六节：用户模型/改密/Token 生命周期/权限概览/M1 校准建议/待验证清单；置信度高 41/中 16/低 1。核验通过（clean-room 零违规）。关键校准：token 创建响应真实字段集（无 token_id）+form 编码；revoke 幂等 200；改密现行路径与 400 语义；建用户真实为 PUT {name}。→ 触发 T-24 PRD v1.3。
- **T-24** [P1] PRD v1.3 auth 校准回写 `role:product-manager` `area:docs/prd` `dep:T-23` — done 2026-08-17
  E-17 form 编码+真实字段集（token_id 超集扩展）；E-18 revoke XOR+幂等 200；E-16 双路由+旧口令 400；E-19 PUT {name} 兼容路由；§5.1 错误体三分层（制品 errors[]/用户管理纯文本/token OAuth）；§5.5 六项校准全部收口；顺手修 C20 剧本 ADMIN_PW 连锁 401 缺陷。核验通过（旧口径零残留）。T-15 派发时附 v1.3 口径。
- **T-10** [P0] metadata：SQLite Store+迁移器+001_init `role:dev-go-core` `area:internal/metadata` `dep:T-7` — done 2026-08-17（经一轮修复）
  14 文件：api/store/migrate/password/substores + 001_init.sql（9 表，permission 两表形态）+ 35 测试。双 review：正确性 APPROVE；架构 REQUEST_CHANGES 两 blocker 均已修复并复审通过——B1 LIKE 大小写误删（case_sensitive_like 入 DSN + 6 子用例破坏性断言）、B2 池 NumCPU + 三 PRAGMA 挪 DSN（四连接并发断言 + fail-fast）。conductor 复现：race 12.8s 绿 / lint 0。
  遗留（minor 不阻塞）：FilterUnreferenced TOCTOU 契约（T-13 派单附注）、tokenStore.Touch 上下文（T-11 顺车）、双进程首启竞态（M4 技术债）。
- **T-8** [P0] config 包 `role:dev-go-core` `area:internal/config` `dep:T-7` — done 2026-08-18（经一轮修复）
  7 文件：api/load/validate/config + 30 测试（覆盖率 91.9%）。review 3 blocker + 1 major 全修复并复审通过：B1 多文档 YAML（decode 后断言 io.EOF）、B2 sqlite DSN 白名单（拒绝 URI 形态）、B3 ADMIN_PASSWORD 大小写归一赋值、M1 redactDSN 脱敏。conductor 独立探针复验多文档秘密拦截。race 2.4s 绿 / lint 0。
  顺手：m2 TOCTOU 注释、m1 doc.go 例外清单补 DATA_DIR、m4/m5 测试补齐；n1 记录保留理由。
- **T-25** [P0] 架构文档回写 `role:architect` `area:docs/design、DECISIONS.md` `dep:T-8,T-9,T-10` — done 2026-08-18
  14 处回写（architecture.md 12 + ADR-0007 勘误 2）：GC 集合形签名/mark-sweep/mtime 硬约束（备份保留 mtime）、Engine.Close、sentinel 六全集、state.json 契约定稿与 version 演进规则、DSN per-connection PRAGMA 机制、case_sensitive_like 语义、事务边界 a 案裁定、DATA_DIR 四例外名。核验通过（旧措辞零残留，来源标注 18 处）。
- **T-9** [P0] storage 引擎 `role:dev-go-storage` `area:internal/storage` `dep:T-7` — done 2026-08-18（经三轮收敛）
  11 文件 ~1200 行实现 + 40 测试。三轮评审收敛：架构 B1/M1/M2（Close 契约、会话毒化、state 形状）→ 正确性探针 singleflight panic 死锁（defer teardown 修复 + waiter 释放测试）→ rename 失败/GC 保护场景固化。conductor 复验：race 104s 全绿、lint 0、零 CGO、包边界红线（binflow 依赖=1）、512MB RSS 增量为负。
  契约偏离（经 T-25 回写架构）：GC 集合形回调、Close() 入接口、state.json version 字段。已知边界：单 data dir 单 Engine 实例（doc.go 约束）。
- **T-11** [P0] auth 与 audit `role:dev-go-core` `area:internal/auth、internal/audit` `dep:T-8,T-10` — done 2026-08-18（经一轮修复）
  auth 13 文件 + audit 2 文件，29 测试/94 子用例。review 3 blocker 全修复并复审通过：B-1 哨兵导出别名（errors.Is 双拼法钉死）；B-2 pathmatch folder 语义（尾斜杠=folder，matchStart 仅 folder 生效，over-grant 探针三行钉死）；B-3 fail-closed 三测试 + 日志卫生。conductor 复现：race 23s 绿 / lint 0。
  新契约待 architect 回写（§3.4）：Can 的 path 尾斜杠=folder；文件路径与 pattern 全段匹配。顺手：Redact camelCase/header、ChangePassword 顺序对齐规格、Touch 每分钟节流。
- **T-12** [P0] repo.Service `role:dev-go-core` `area:internal/repo` `dep:T-9,T-10,T-11` — done 2026-08-18（经一轮修复）
  api/service/validate + 28 测试（真引擎基座 + hookStore 注入/journal 写序）。review 2 blocker 修复并复审通过（TrimSuffix 三处归一化：prune 保活目录行回归 + List 双形态一致）；事务边界被证实过硬（blob-first/FK 兜底/24 并发探针）。conductor 复现：race 19.5s 绿 / lint 0。修复 agent 被 429 击落于日志收尾，产出 100% 落盘。
- **T-26** [P1] architect 回写：Can folder 契约 + gosec 豁免复核 `role:architect` `area:docs/design、.golangci.yml` `dep:T-11` — done 2026-08-18
  §3.4 补 Can 尾斜杠=folder 三句契约（对齐 AuthorizationServiceBase）；gosec 五规则收窄：G301/G306 移除豁免、G204 限测试、G401 限 digest.go+测试、G115 限 password.go、G304 限两文件；隔离探针反向验证（新违规四条全中）；nolintlint require-explanation 启用。核验通过（lint 0）。
- **T-20** [P2] Range 与条件请求 `role:dev-registry-adapter` `area:internal/adapter/generic` `dep:T-13` — done 2026-08-18
  conditional.go：单区间解析（闭/开/后缀/钳制）+ 条件求值（INM 弱比较三形态/IMS/优先级）；206 走 Seek+CopyN；416 + bytes */total；HEAD 同分支。32 区间矩阵 + ETag 12 形态 + 条件 15 形态单测 + curl 9 步（-r 四形态/999999999- 416/INM 三拼写/-z 双侧）。T-13 零回归。轻量核验（P2+黑盒覆盖）替代 review；conductor 复现 race 绿 lint 0。curl -z 对 ISO 日期静默不发头的客户端怪癖已实证并绕开。M2 If-Range 扩展点已留（seek 数据面就绪）。
- **T-13** [P0] adapter SPI 与 Generic 适配器 `role:dev-registry-adapter` `area:internal/adapter、internal/adapter/generic` `dep:T-3,T-11,T-12` — done 2026-08-18（经一轮修复）
  SPI（Layout 解码→校验链/Register panic 条件/ForRepoType 并发安全）+ generic 四动词 + 校验头语义 + errors[] 信封 + FileInfo（size 字符串）。review 修复：repo.PutFromBlob（判权先于 blob 打开、双维校验、ErrOrphanBlob 堵死孤儿实体化与 sha256-only 降级）、BlobOpener 缝删除、404 文案分动词、控制字节拒绝、originalChecksums 上下文区分。路径安全 30+ 变体真机实测全 400（reviewer 取证）。curl 黑盒 12 场景全 PASS。
  遗留裁决归档：sha1-only deploy 维持 404（M3）；originalChecksums 持久化 M1 不需要；TOCTOU 零调用确认。PutFromBlob 契约待 architect 记入 §3.3（一句话）。
- **T-14** [P0] httpapi 核心 `role:dev-go-core` `area:internal/httpapi、internal/console` `dep:T-8,T-11,T-13` — done 2026-08-18（经一轮修复）
  7 文件 + 真栈 harness 测试（覆盖率 84%）。EscapedPath 手写分发树（绕开 mux cleanPath 归一化）；middleware 固定链 + logFields；errors[] 信封；系统端点四件；SIGTERM 优雅停机。review 3 blocker + 2 major 全修复复审通过：readyz storage 探测（只读目录 503）、死 case 删除、statusRecorder 双标志、mid-stream panic 不嫁接信封、首段 unescape 三源同源（encoded key 可路由且无 ACL 旁路）。curl 冒烟 + 19 探针矩阵全绿。
  遗留归档：minor 1-4/6-8 转 T-15/T-16/PM；RepoLookup 缝判定可保留（PackageTypeOf 列非阻断重构）；M2 架构措辞（路由解析位于授权门后 + ADR-0009 补句）交 architect。
- **T-15** [P0] Artifactory 兼容 REST `role:dev-registry-adapter` `area:internal/httpapi（兼容 handlers）` `dep:T-3,T-14` — done 2026-08-18
  四组 handler（repositories/storage/security/permissions）+ router/middleware/server 扩展 + 5 测试文件（38 顶级测试、覆盖率 78.2%、T-14 零回归）。v1.3 全口径落地：建仓 200 纯文本、错误体三分层、旧口令 400、token form+JSON 嗅探+OAuth 错误体、revoke XOR 幂等双文案、?list 匿名 403/根 400。curl compat 10 场景全 PASS。轻量核验（对定案矩阵编码 + 黑盒覆盖，qa 合并 T-18 全矩阵复验）。
  遗留：T-16 装配三缝（日志已列）；email 仅校验不落盘（users 表无列，回显需派生票，M2 评估）；repo 列表无按调用者过滤（M1 无数据面）。
- **T-27** [P1] architect 回写：三处累积裁决 `role:architect` `area:docs/design、DECISIONS.md、docs/prd` `dep:T-13,T-14` — done 2026-08-18
  §3.3 PutFromBlob 契约（签名逐字对齐实现 + 四点注释）；§7.1「路由解析位置」段 + §3.4 交叉引用 + ADR-0009 补句；PRD C28a 勘误（sfu admin + v1.3.1 修订行）。核验通过。
- **T-16** [P0] cmd 装配与生命周期 `role:dev-go-core` `area:cmd/binflow-server、scripts` `dep:T-14,T-15` — done 2026-08-18（经 429 中断续跑）
  main.go 573 行装配链（无全局单例）+ 15 测试（覆盖率 77.1%）+ scripts/smoke.sh。serve/gc 双 subcommand；冷启动实测 0.15s（NFR-P1 <2s 达标 13 倍余量）；SIGTERM 排空 0.038s exit 0；postgres e2e 拒启零残留；gc dry-run/--apply/幸存断言；admin 缺省 WARN（argon2 真探测）。conductor 实跑 smoke.sh 全绿（C01/C03/C07/C08/匿名/停机）。曾被 429 击落，额度恢复后续跑完成，生产代码零损失。
  遗留：gc 无 -c 旗标（票面未要求）；二次信号强退未构造观察窗口（排空毫秒级）。
- **T-17** [P0] 开发环境 `role:devops-engineer` `area:deploy/dev、README.md` `dep:T-16` — done 2026-08-18
  多阶段 Dockerfile（非 root 10001/HEALTHCHECK /readyz/CGO 零）+ compose（命名卷持久化/35s grace/:? 强制口令）+ README（五步双路径+安全须知三件）。Docker 真机全跑：AC1 up→ping 2.35s 冷链、AC2 health ok、AC3/C29 restart+down&up 两轮 sha256 不变；容器 healthy、db 无明文、日志无凭据。conductor 复核 compose 语法（:? 触发符合预期）。遗留：版本 stamping/distroless/CI docker build 归 M5。
- **T-28** [P0] 修复 D2/D3：管理面 admin 分级 `role:dev-registry-adapter` `area:internal/httpapi` `dep:T-18` — done 2026-08-18
  routeAuth 补 admin-only：四读面（repositories 列表/单查、v1-stats、v1-health）+ token 签发；routeAuth 新增 oauth 位（token 族错误体统一 OAuth 形，顺手修 revoke 403 不一致）；ping/version/探针不误伤。矩阵测试 + 自跑 22/22 + QA 回归 23/23 关闭两缺陷。
- **T-18** [P0] QA 功能矩阵验收 `role:qa-engineer` `dep:T-16,T-17` — done 2026-08-18（首轮 FAIL→回归 ALL GREEN）
  两轮验收：round 1 FAIL（D2/D3 两 P1 同源）→ T-28 修复 → 回归 23/23 ALL GREEN、D2/D3 关闭、round 1 FAIL 撤回。最终：场景 1/3/4/6/7 + NFR-S1/S2/S3 + C28 全 PASS。方法学亮点：二轮净instance 自纠两误报 + A/B 对照构建证伪一个疑似回归（观察项 O4 供 M2 复核）。报告 reports/agents/T-18-qa.md。
- **T-19** [P0] QA 存储完整性/性能/持久化+README 复跑 `role:qa-engineer` `dep:T-18` — done 2026-08-18（ALL GREEN 零缺陷）
  场景 2/5/8/9/10 全绿：去重（blob 1 物理份）、慢上传中断零残留、kill -9 双轮+容器路径一致、1GB 流式 RSS 增量仅 56KB（限 256MB）、冷启动 0.065s（限 2s）、100 并发零 5xx、C29 两轮持久化、gc dry-run/apply/幸存、README worktree 干净复跑全 0。**DoD 第 1/2 条判定：满足**（P2 未做仅 Content-Type 映射，合规延后 M2）。报告 reports/agents/T-19-qa.md（M1 QA 总报告）。

---

## M2 票据（2026-08-18 起）

- **T-33** [P0] /v2 挂载+docker 基座 `role:dev-registry-adapter` — done 2026-08-18（经双 review 一轮修复）
  /v2 根级例外路由（ADR-0010）+ name 解析 + spec 信封 + Api-Version 头 + health registry 字段 + R10 测试反转。双 review 5 blocker 全修复复审通过：B1 平面感知认证挑战（context 信号下传，/v2 spec 体+Bearer、/binflow 不变——真栈 curl 双面验证）；B2 全段 dot-segment 防线（400 实证）；B3 _catalog 占位；B4 repo 门三因拆两分支（500+ERROR 日志）。RepoTypes 空 class 键放行（§5.1 勘误挂 M3）。提交 e15e87a+e62eb78。
- **T-35** [P0] repo docker 用例编排 `role:dev-go-core` `area:internal/repo` — done 2026-08-18（经一轮修复）
  Service +8 docker 方法（PutManifest 幂等/解析/ListTags/ListImages/DeleteManifest 走存储层同事务级联/删仓拆库）；known/supported 两层类型矩阵。review 3 blocker 修复复审通过：B1 refs 泄漏窗口关闭（DeleteRepoRefs 后置 + review 探针确定性复刻测试）；B2 幂等零变更（存储行读回实证）+ TagRepointed 三方定约；B3 哨兵拆分（ErrInvalidManifest/ErrInvalidCursor）。提交 ccd4577+4344da3。T-38 前置仅剩 T-37。
- **T-41** [P1] O1 断连日志定界 `role:dev-go-core` `area:internal/httpapi(middleware)` — done 2026-08-18
  statusRecorder 增 writeErr/panicDisconnect 槽位；断连（ctx.Canceled 主腿 + errno 兜底）≥500 降 WARN + client_disconnect 标注；recoverPanic 断连降级不注信封。进程外 e2e 实证（--limit-rate + kill -9：日志零 ERROR、真 500 反例保持 ERROR 三态表）。轻量核验（P1+e2e 证据）。提交 85df447。M5 升格 label 遗留登记。
- **T-42** [P1] gc 旗标+GC mark 扩容 `role:dev-go-core` `area:cmd/binflow-server` — done 2026-08-18
  gc -c（复用 serve 配置链）+ --grace-hours（与 days 并存 hours 胜）；mark = nodes ∪ docker_refs（ListRefsByManifest 聚合——agent 论证了 RefsByBlob 会回到 T-9 废弃的反连接路线）；真栈冒烟（refs-held 存活/级联删后转候选）。净树核验（T-37 WIP 致主仓瞬断，隔离手法 agent 自报 conductor 复现）。提交 69a6039。遗留：lint 有网补跑；子命令 --help exit 1 小票登记。
- **T-46** [P1] docker 接入用户文档 `role:tech-writer` `area:docs/user` — done 2026-08-19（M2 最后一票，经 429 中断续完）
  docs/user/docker-registry.md（306 行，Docusaurus 首批页面）：insecure-registries 三形态置顶/全名 tag 模型/buildx 两坑/Helm OCI 凭据文件方案/oras 双类型/五客户端命令表/有意不兼容清单+反代片段/常见报错对照。13 组命令复跑全过（两轮，T-45 基线产物）。观察项 O-1（crane 取消 token 的 client_disconnect 500 日志）转 T-41/T-37 域评估。提交 c230b10。
- **T-45** [P0] 部署烟测 `role:release-engineer` — done 2026-08-19（AC 4/4 + O2 全过）
  compose 实例 D04/D05/D16 全过（v1.3 口径）+ D21 restart 持久化 + O2 全新 dind 默认端口零配置复跑（49 请求零 5xx）+ 反代直通示例（nginx/traefik）+ compose TTL 透传微调（注明理由）。两轮清理彻底；digest 清单录报告（发布动作待用户确认）。提交 923db2e。
- **T-44** [P0] QA 五客户端 conformance `role:qa-engineer` — done 2026-08-19（首轮 FAIL→复验 PASS）
  首轮：podman/crane/oras/skopeo+buildx 等效+conformance 55/60+性能全绿；docker 三 P0 断（D44-1/2/3）。修复（2f505da）后复验：docker 行 10/10 全绿（错口令 exit1 恢复/buildx 双平台直推/匿名 token 链/挑战模式全链）+ helm push PASS（首轮归因修正：HELM_REGISTRY_CONFIG 亦是败因）+ conformance 无回归。**DoD §9 第 1/2 条满足**（P2 遗留按 PRD 不阻塞：D44-4/5/6 实现欠账归 T-35/T-40 域 T-45 后收口、helm plain-http login 文档转 T-46）。报告 reports/agents/T-44-qa.md。
- **T-56** [P1] M2 PRD v1.3 勘误 `role:product-manager` `area:docs/prd` — done 2026-08-19
  C6 ping 无条件 401 挑战定案（ping-缓存型客户端根因 + 匿名 token 路径 + 真实世界同构佐证）；C5 offline_token 接受忽略（MAY ignore）；C4 POST 表单凭据同权 + D15 oras/helm 双类型形态修正；C7 tags/list 删光后 200 tags:null。约 22 处，自检零残留。提交 4d3aebd。
- **T-54** [P1] F1 压测定论 `role:dev-go-core` `area:internal/metadata、internal/adapter/docker` — done 2026-08-19
  根因确凿：SQLITE_BUSY 全仓 race 负载下烧穿 5s busy_timeout（8 轮复现 5 中 + 服务端 ERROR 原文 + duration 13.7s）+ busy 误映射 500 + 第二根因 anonSeedOnce 跨迭代污染（-count>1 确定性缺陷）。修复：IsStoreBusy 分类 + 503+Retry-After+WARN（T-38/T-41 先例）+ per-handler 隔离 + harness 取证盲区补齐；修复后 8/8 压测全绿 + 两轮 count=5。conductor 复现 4 新测试 PASS/12 包/lint 0。遗留：token 面 busy 映射（T-37 后续）；WAL synchronous=NORMAL 评估（architect ADR-0007 后票）；count>1 需显式 -timeout（40m 定论口径）。提交 f21904b。
- **T-38-D1** [P2] mount action 域修 + D2 降级修复 `role:dev-registry-adapter` — done 2026-08-19
  D1：canMountFrom 改 canActions 映射 + 双臂回归（红→绿证明：stash 复现 QA 现象）；D2：T-52 未使其消失（ListImages 需 repo 根读，休眠分支对真未知镜像照跑）→ imageListed ERROR→Debug 降级 + 真栈复现整日志零 ERROR。顺手：IdleSessionEviction 去墙钟竞态（注入时钟）。提交 e906f4e（与 T-55 同批）。
- **T-55** [P1] /v2/token 401 错误体 OAuth 形一行修 `role:dev-registry-adapter` `area:internal/adapter/docker(token)` — done 2026-08-19
  RenderAuthFailure 路径感知：token 路由族 OAuth 形（invalid_client）+ 挑战头逐字节不变；资源端点 spec 体不动；writeOAuthError 去 401 附 Basic 副作用（归属调用方）。4 行表双向断言 + 真栈三组错误凭据复验。conductor 复现：build ok + 两包针对性测试绿。提交待与 T-38-D1 同批（同包在途隔离）。
- **T-53** [P1] M2 PRD v1.2 勘误收口 `role:product-manager` `area:docs/prd` — done 2026-08-19
  C3+D3 裁定 token 端点族全 OAuth（三处对齐+跨里程碑口径）；C1 name 全名模型（D 序列+FR-8-AC6+D04b/c scope 联动）；C2 ping 无 scope 注记+四形态；v1.2 修订行完整；三轮自检零残留。经 429 中断（改动在盘）恢复收尾。遗留：T-37 401=spec 一行修（conductor 待派）。提交 4034b14。
- **T-43** [P0] QA 协议矩阵+M1 回归基线 `role:qa-engineer` — done 2026-08-19（PASS 80/82）
  剧本 1-5/7/10：M1 回归基线全绿（E-26 反转验证）/D 序列 35/36 绿/跨协议去重/权限面/遗留抽查全过；worktree 隔离构建（避开 T-52 在途）。2 P2（D1 mount action 域错配、D2 探测 ERROR 污染）+F1 flake+3 PRD 勘误 → T-38-D1/T-53/T-54 收口波已派（m2-done 前清零）。QA 又自纠 3 个 harness 误报。报告 reports/agents/T-43-qa.md。
- **T-52** [P1] repo.ListTags 零 tag 契约修复 `role:dev-go-core` `area:internal/repo` — done 2026-08-19
  行数判别两态（零 manifest→ErrImageNotFound 维持；有 manifest 零 tag→空切片 nil error——恰是 adapter 渲染 tags:null 所需）；断言反转+补强 5 子用例；agent 守 area 纪律未越界（adapter 注释由 conductor 顺手落：dormant defense 标注）。提交 6544ffc。
- **T-40** [P0] catalog 与 tags/list+分页 `role:dev-registry-adapter` `area:internal/adapter/docker(catalog)` — done 2026-08-19
  catalog/tags/list 字典序（跨仓全局重排实测）+ Link 分页（last exclusive/n 缺省 100/无效 n 400）+ tags:null + Q5 过滤矩阵 + 删仓消失 + 占位分支替换。D09 全序列 curl 实证 + 14 测试 race 绿。轻量核验（对定案矩阵编码+黑盒），T-43 全量复验。发现 T-35 缺陷（ListTags 零 tag）→ T-52。提交 2cb5b9b。**M2 功能开发全部完成**。
- **T-51** [P1] R3 消歧回写 `role:architect` `area:docs/design、docs/prd` — done 2026-08-19
  §5.3 校验链①终审口径（透传+结构判读/判读优先序/裸 media type 语义/schema1 拒收/不裁回白名单理由）；DDL 注释同步；双 node 布局升格正式契约（manifest+blob 双落、DELETE 后 blob 200 为既定语义）；PRD DE-15 OCI-Subject 注记。旧措辞零残留。遗留转 conductor：repo 导出 shutdown 哨兵小票。
- **T-39** [P0] manifest 链 `role:dev-registry-adapter` `area:internal/adapter/docker(manifest)` — done 2026-08-19（经双 review 一轮修复）
  校验链（结构/digest/引用在场/嵌套 lazy）+ GET·HEAD 逐位一致 + Accept 协商 + tag 覆盖 + DELETE 级联/405。curl 41 断言 + 容器内 daemon 协商全序列 + docker manifest inspect 真客户端 exit 0。架构 APPROVE（R3 终审意见：透传+结构判读）；正确性 1 blocker（重复 digest 假失败真发布）修复复审通过：adapter 去重 + INSERT OR IGNORE 纵深 + fake PK 对齐 + 四形态 201 无幽灵测试 + 20 路并发钉住回归。提交 eef0d3f+fd3d68a。**M2 docker 域功能面全部闭环**。
- **T-50** [P0] ADR-0011 文档中心 Docusaurus `role:architect` `area:DECISIONS.md、docs/design` — done 2026-08-19
  用户定案落地：Docusaurus 选型（同栈 React/版本化/i18n）；**交付形态=go:embed 挂 /binflow/docs（统一前缀、匿名可读）**——离线自带文档对齐 15 分钟标准，独立托管用户自办不双轨；docs/user 纯 Markdown 源与 docs-site 配置分离（writer 不碰构建）；体积 5~15MB 预算 M5 check-size 把关超限 fallback tar。架构四处增量（包树/职责表/路由/部署段）。M5 需 Docusaurus 脚手架票先行（类 T-7）。
- **T-49** [P1] OSS 工程结构参考规格 `role:reverse-engineer` `area:docs/reverse` — done 2026-08-18
  oss-structure.md（242 行）：51 pom 全量解析（37 实体+14 聚合）五域归类 ↔ internal/* 双向映射；L0-L5 单向依赖 + 四 SPI 接缝（JerseyApplication 双源扫描证据/CoreAddonsImpl 60+ 桩）；M3 导航三要点（协议全在 pro 但 OSS 有 50 个 *MetadataProvider 统一注册表同构点——M3 最有价值；addon 无 npm/pypi 接口→拆票按「OSS 接口面+pro 实现」双源；repo 类层次补强 repo-semantics）；结构启示 6 条。参考强度三级标注 [OSS]/[pro]/[双源]。M3 拆票输入就位。
- **T-48** [P1] architect 勘误 `role:architect` `area:docs/design` — done 2026-08-18
  §5.1 两例外（上传会话直持 Engine 限定上传端点族读路径仍走 Service；BlobLedger READ-only 同构先例）；§11 债务 13 行（PutLandedBlob M3 前小票）；RepoTypes 四点勘误正式落（声明性元数据/分发键约束/空 panic 保留/Layout 两态化）。遗留：httpapi New() 删 class 键写入的代码侧跟进（实现票）。
- **T-38** [P0] blob 域全链路 `role:dev-registry-adapter` `area:internal/adapter/docker(blob)` — done 2026-08-18（经双 review 一轮修复 + 2 次 429）
  upload 三式（416 错位恢复）+ mount 零拷贝降级 + 空层 32B 合成 + 读路径全套 + DELETE 405。curl 黑盒 38 断言（kill -9 重启链/AC7 跨协议去重）。双 review 4+1 blocker 修复复审通过：B1 互斥串行化（6 并发同 UUID 恰 1×202+5×416 + 连带修 done 标志）；B2 idle TTL 驱逐（24h 对齐+四动词 404）；B3 fd Close（真栈 50 mount 零累积）；B4 挂账失败 5xx（重试痊愈全链测试）。WithStorage 终判不越线（§5.1 勘误→T-48）。提交 2cd6d68+0b92149。
- **T-37** [P0] docker token 流 `role:dev-registry-adapter` `area:internal/adapter/docker(token)` — done 2026-08-18（经 429 中断续完）
  /v2/token（GET/POST form 双式/任意有效用户/匿名直发 _docker_anonymous 幂等 seed fail-closed/OAuth 错误体/offline_token 400）+ scope 三映射 + 挑战矩阵（真栈：PUT→pull,push / DELETE→pull,delete / read-only Bearer PUT→403 DENIED）+ D04 全系列 + D23 吊销链。docker login 本体受阻本机 daemon（VM+代理+无 insecure-registries）——容器内等效复现全协商；拒绝动用户配置（安全底线正确）。经第 5 次 429（代码全落盘）恢复收尾。提交 a89313f。
  转交：insecure-registries 说明 → T-46 文档；D05 → T-44。authorizeRoute 共用推导表已就绪（T-38/39/40 无需重推导）。
- **T-34** [P0] metadata 002_docker 迁移+DockerStore `role:dev-go-core` `area:internal/metadata` — done 2026-08-18（APPROVE 一轮过）
  review 0 blocker：DDL 对照固化为 pragma 测试（三索引=AC 笔误以定稿为准）；级联误删探针实证不可能（digest 即 manifest 身份 + image 谓词隔离）；2000 轮 DeleteManifest vs PutRefs 0 错误 0 残留；keyset BINARY collation 稳定。5 minor+2 nit 记录不阻塞。T-35 依此解锁。
- **T-36** [P2] generic Content-Type 扩展名映射 `role:dev-registry-adapter` `area:internal/adapter/generic` — done 2026-08-18
  mime.go 18 项映射（自有表→标准库分层，跨主机确定性）；PUT 端推断写 node.Mime（单一事实源：FileInfo/GET/HEAD/api/storage 自动一致）；客户端声明逐字优先；未知回退 octet-stream 不变。14 扩展名 curl 实测+大小写/复合扩展名/checksum-deploy 继承用例。轻量核验（P2+黑盒），T-43 全量复验。提交 0073358。
- **T-47** [P1] M2 PRD v1.1 回写（R1/R2/R4/R5） `role:product-manager` `area:docs/prd` — done 2026-08-18
  R1 realm=/v2/token + 双 token 入口说明（赶在 T-37 前完成）+ D04c 新增；R2 by-tag 405；R4 tags:null 断言注记；R5 四项定案（4MB/offline_token 400/service=binflow/last exclusive）。核验通过；ROADMAP 版本引用顺手修正。
- **T-34** [P0] metadata 002_docker 迁移+DockerStore（待 review 终裁） `role:dev-go-core` `area:internal/metadata` — 编码 done 2026-08-18
  002_docker.sql 三表三索引按架构定稿（零事务语句+守卫测试）；DockerStore 全 CRUD/级联单事务/keyset catalog；老库升级路径+并发用例；M1 零回归。conductor 复现全绿。提交 6df3b96。
- **T-32** [P0] M2 工程 ticket 拆解 `role:tech-lead` — done 2026-08-18
  14 票（T-33~T-46）+ 8 批次表 + R1~R12 风险清单，全文 reports/agents/T-32.md。核验通过（area 分区/M1 复用面/双 reviewer 标注合理）。R1/R2/R4/R5→T-47（PM）；R3→architect 消歧票（赶在 T-39 前）；R6/R7/R8/R9/R10 进对应票派单要点。
- **T-29** [P0] M2 PRD `role:product-manager` `area:docs/prd、ROADMAP.md` — done 2026-08-18
  milestone-2.md v1.0（515 行）：FR-7~FR-14（docker repo 类型/blob 三式/manifest schema2+OCI/catalog+tags/token 流/Helm OCI/五客户端矩阵/部署烟测）；DE-01~DE-17 兼容矩阵 + D01~D24 验收命令；M1 观察项 O1~O4 逐条定界；Q1 路由两案对比（待定）。核验通过。
- **T-30** [P0] M2 架构增量 `role:architect` `area:docs/design、DECISIONS.md` — done 2026-08-18
  **ADR-0010：/v2 根级例外**（三案评估：反代 rewrite 出局因裸机 docker 不可用、双挂载出局因三处双份生成；根级例外与 /healthz 同类豁免，token realm 免重写）。§5.3 docker adapter 13 行端点映射（offset 由 adapter 持协议态/cross-repo mount 走 PutFromBlob/mediaType 白名单+在场校验）；token 复用 TokenRegistry（scope pull→r push→w）；002 迁移三表（docker_manifests/tags/refs）。核验通过。三处待 T-31 校准点已入 §12。

- **T-58** [P0] M3 架构增量 `role:architect` — done 2026-08-19
  ADR-0012 remote 代理基线（TTL+条件再验证/artifact-metadata 分流/stale-while-error/SSRF 双检防 DNS rebinding/AES-GCM 凭据/stdlib-only）；ADR-0013 virtual（local-first+position 序/X-BinFlow-Resolved-From 头/M3 只读 405/探索 miss 不落盘）；§4.5 remote 缓存面（无影子仓）；MetadataProvider 注册表对齐 OSS；003 迁移（remote_configs 加密+remote_cache 表）。提交 82c973e。

- **T-59** [P0] M3 逆向规格 `role:reverse-engineer` `area:docs/reverse` — done 2026-08-19
  maven-npm-pypi.md（306 行：5 端点表/12 流程/6 布局组——npm publish 十步校验链、Maven metadata 两套规则、PyPI upload multipart、remote 六步 pull-through、virtual 四桶序）+ repo-semantics.md §7/§8 扩编（+158 行）。置信度高 ~102/中 ~24/低 0（不确定不入文降级待验证 8 条）。顺带关闭 M1 待验证 #4 + 勘误 2 处。提交 7042dae。

- **T-57** [P0] M3 PRD v1.0 `role:product-manager` `area:docs/prd、ROADMAP.md` — done 2026-08-19
  milestone-3.md（817 行）：FR-15~FR-22 共 66 AC（Maven layout/checksum 三态/metadata 合并+snapshot、npm、PyPI、remote pull-through+SSRF、virtual、conformance）；35 端点矩阵 + M01~M61 验收命令（mvn/npm/pip P0）；Q1~Q8 附暂行；ROADMAP 切 M3。Q4（docker remote 推迟 M4+）已转用户知悉。提交 e456f1b。

- **T-60** [P0] M3 PRD v1.1 校准 `role:product-manager` `area:docs/prd` — done 2026-08-19
  C1~C8 全定案（maven-metadata 服务端计算/layout 六字段/virtual 两桶简化/TTL 定案/写路由字段/PyPI 布局兼容子集/npm tarball）；M1 勘误吸收（snapshot policy 409）；Q3/Q7 定案（npm 403/重复 publish、PyPI sha256-only）；连带定案（remote checksum 不回源 404、上游故障默认 404+hardFail 502——推翻 v1.0 五处）；计数 29/1/0/5+1。自检零残留。遗留：Q1/Q2 待用户；M1 PRD 两处勘误小票建议。提交 0f48164。

- **T-61** [P0] M3 工程 ticket 拆解 `role:tech-lead` — done 2026-08-19
  16 票（T-62~T-77，P0×13）+9 批次+R1~R10；复用清单 8 面（storage.Session/迁移器/Service 覆盖链/SPI/权限/GC/QA 脚本）零重做；遗留 5 项处置（2 无票归档/T-73/T-63 收编/1 归 M4）；Q1 按 ADR 写死（R1 回写）、Q2 暂行入 T-71。全文 reports/agents/T-61.md。提交 8500821。

- **T-78** [P1] PRD v1.2 凭据回写 `role:product-manager` `area:docs/prd` — done 2026-08-19
  Q1 按 ADR-0012 关闭（AES-GCM/enc:v1:/env BINFLOW_REMOTE_CREDENTIALS_KEY/fail-fast/003 一次性加密）；新增 FR-15-AC9 四断言；C2 注记顺手。R1 达成——T-66 派发解锁。范围外三冲突转 T-79（ADR 勘误）。提交 2cacee1。

- **T-79** [P1] ADR-0012/0013 勘误 `role:architect` `area:DECISIONS.md` — done 2026-08-19
  勘误一：故障降级 404+assumed-offline+X-Binflow-Upstream-Error（Warning:111 作废）+负缓存定案；ADR-0013 联动：两桶序+可选写路由（决策骨架不变）；勘误二：SSRF 五参数以 PRD v1.2 为准+建仓只校验 scheme（IP 校验全在请求时——清单外新发现分歧）。T-62 已补发对齐提示；§4.5/§5.4/003 注释三处同步债挂 T-66/T-71 派单注明。提交 14ba31e。

- **T-62** [P0] metadata 003 迁移+Remote/Virtual `role:dev-go-core` `area:internal/metadata` — done 2026-08-19（APPROVE 一轮过）
  review 0 blocker：DDL 逐列一致+pragma 钉死；老库升级真原生 apply；400 次并发 upsert + 4×40 SetMembers 探针无 busy 逃逸；两桶序注释勘误后口径完整；clean-room 无嫌疑。5 non-blocking 记录（T-64 防明文窗口提示已转批 2 派单要点）。提交 54ed293+4bcd536。

- **T-63** [P0] adapter SPI 基座 `role:dev-go-core` — done 2026-08-19（APPROVE 一轮过）
  MetadataProvider 注册表 + npm/pypi 分发缝（escaped 逐字保留）+ class 键清理（§5.1 勘误收编）+ repo/api.go 两段拆分。review 0 blocker：6 项 seam 探针真栈全过；契约三决定全确认（路径重写/ClassReader 纪律/Versions 回落）；E-26 未反转。N4（协议票严格拒绝决策）已转 T-69/T-70 派单要点。提交 5b79a52+91c1f67。**批 1 全部闭环**。

- **T-65** [P0] SSRF 防护链+stdlib client `role:dev-go-core` `area:internal/remote` — done 2026-08-19（经双 review 一轮修复 + 429 中断续完）
  NFR-S13 七点全实现（48→94 断言/coverage 87%/零新依赖/注入 Resolver 零外网）。双 review 4 blocker 修复复审通过：B1 NAT64/6to4/Teredo 内嵌 IPv4 拆解递归过表（保 DNS64 放行侧）+ 重定向跟随面钉死；B2 zone 剥离；B3 godoc 契约修正；B4 HEAD 豁免 64MB 快速失败。顺手 Location userinfo 堵注入。安全 review 探针实证（go test -overlay 零树改动）。提交 f9fb2c9+0dc17a2。T-66 消费面接口七项已备。
- **T-80** [P0] httpapi REST 三型接线 `role:dev-go-core` `area:internal/httpapi/repositories.go` — done 2026-08-19（经 429 中断续完）
  repoConfig 扩 M3 字段+configJSON 组装（keep-current 信号）+ListReposFiltered 接线+configuration 回显+C26 翻转（docker 组合维持 400）。M01~M05 真二进制 curl 全过（M02b 三态/M03 成员校验/M04 过滤矩阵/M05 组合边界）+8 REST 测试+13 包零回归。T-66 fixture 前置就绪。

- **T-64** [P0] repo 三型模型+PutLandedBlob `role:dev-go-core` — done 2026-08-19（APPROVE 一轮过）
  review 0 blocker：finalize 切换 git diff 核实（O(size) 回读真删/busy 注入迁移/T-54 断言保留）；PutLandedBlob 并发同摘要+无重读钉板 -count=2 绿；C26 声明在快照验证。4 non-blocking（掩码大小写敏感→T-66 改/crash 窗口口径/审计置空/PutLandedBlob 段位）。提交 63135de+5662b22。
- **T-81** [P1] NAT64 勘误 `role:product-manager` — done 2026-08-19
  PRD NFR-S13② + ADR-0012 决策 3 各一句（拆解递归/DNS64 保留/Teredo 直拒/v4-compatible 收编/zone 剥离），溯源 T-65+0dc17a2。M42 测试向量扩充建议记日志。

- **T-70** [P0] PyPI adapter `role:dev-registry-adapter` `area:internal/adapter/pypi` — done 2026-08-20（经一轮修复）
  simple（PEP 503 三态/691 JSON/Vary）/upload（md5 三态/:action 400）/下载双入口；twine 7+pip 26 真实客户端 M30~M35b 全过；归一化矩阵+恶意文件名九变体探针全过。review 1 blocker（探针 fd 泄漏+审计伪造）+Vary+死 Del 修复复审通过。遗留：N4 缝层强制与 service Stat 面转 conductor；remote/virtual simple 400 过渡（T-71/72）。提交 c59bd6b+258aae1。
- **T-69** [P0] npm adapter `role:dev-registry-adapter` `area:internal/adapter/npm` — done 2026-08-20
  23 文件 4336 行：十步校验链/packument（tarball 重写/ETag-304/SLIM）/dist-tags 两形态/unpublish 联动/login 复用 TokenRegistry/N4 守卫。npm 10.9.8 真实客户端 M22~M28 全过（M26 403 勘误口径/M27 -rev 占位显式断言）。E-26 npm 翻转。-rev 对 packument 形 body 的有意偏离建议规格回写。cmd 装配 3 行待集成票。提交 91bb261。
- **T-67** [P0] Maven adapter（传输面） `role:dev-registry-adapter` `area:internal/adapter/maven` — 主体 done（91bb261），mvn 真客户端腿续跑中
  layout 六字段解析/checksum 三态/旁车/snapshot 语义/穿越防御；M11~M21 wire 序列+curl 等价全过。遗留②：Service SPI 缺 metadata 覆盖豁免入口（~10 行，T-68 依赖，挂 architect）。

- **T-66** [P0] remote fetcher `role:dev-go-core` `area:internal/remote、internal/repo` — done 2026-08-20（经双 review 一轮修复 + 429 续完）
  六步全矩阵 + AES-GCM 凭据链 + 分流接线；真二进制 M41~M48/FR-15-AC9/1GiB<200MB。双 review 4 blocker 修复复审通过：B1 单飞等待者重入全量重查（16 并发同 404 上游恰 1 次钉板）；B2 超时回发旧副本；B3 RepoTypes 升 {local,remote}；B4 契约 godoc。提交 05acd91+71e6c93。
- **T-67** [P0] Maven adapter `role:dev-registry-adapter` `area:internal/adapter/maven` — done 2026-08-20
  layout 六字段/checksum 三态/旁车/snapshot 语义/穿越防御 + cmd 装配 + REST local 字段透传。wire 序列全过 + **mvn 3.9.9 真客户端 M11/M12/M13/M16 布局腿**（BUILD SUCCESS/sha256 对账/全新 repo resolve/timestamped 落盘）。SPI 豁免遗留→T-68 实施/T-83 定约。提交 91bb261+71e6c93。
- **T-71** [P0] virtual 两桶解析+写路由 `role:dev-go-core` `area:internal/repo` — done 2026-08-20（APPROVE 一轮过）
  review 0 blocker：stale/miss 语义引擎侧核实（成功必 HasCopy/true miss 只以 Unfound）；非 Unfound 透传确认为 AC7 严格读法（安全面更优）；C5 文案逐字节相等；pre-read guard 顺序面正确。6 non-blocking（QA 钉板 hardFail 透传防顺手修复等）。提交 eab4363+f6e1017。
  virtual.go 两桶序（逐请求现算）/Get 三型分派/stale 命中即成员结果（HasCopy 消费）/探索性 miss pre-read guard/写路由（405+C5 文案/配置后换址 local）/ExtraHeaders 双头合并。17 测试群+真二进制 M50/M52/M53。遗留①②（协议面 StatusError+ExtraHeaders 两缝）→ T-82。提交 eab4363。
（T-82 done → done 区）
（T-68 编码完成 → review 区）
（T-83 done → done 区）

- **T-83** [P1] architect 回写 `role:architect` `area:docs/design` — done 2026-08-20
  §4.5 两处（checksum 登记不拒定案/故障语义勘误一收口）+ §5.4 渲染缝（StatusError+ExtraHeaders 复用勿另开缝）+ RepoTypes + **SPI SkipOverwriteCheck 最终契约**（收窄：仅服务端自有写入/写门不豁免/只跳 d 检查）+ T-79 三处遗留债顺带收口。提交 d983753。

- **T-82** [P0] 三协议双缝修复 `role:dev-registry-adapter` `area:internal/adapter/{maven,npm,pypi}` — done 2026-08-20
  StatusError 直渲染（npm 此前完全缺失→一律 500）+ ExtraHeaders 探测三协议；红绿验证（还原至 HEAD 三测试 FAIL 行号级）；virtual DELETE 405+C5 逐字/双头输出/上游计数冻结。遗留①pypi 上传早闸不感知路由（挂 architect）②npm RepoTypes 口径③maven PUT 文案对齐（已顺手做）。提交 5b13a1e。

- **T-68** [P0] maven metadata 计算器 `role:dev-registry-adapter` `area:internal/adapter/maven、internal/repo(SPI)` — done 2026-08-20（APPROVE 一轮过）
  计算器 ~700 行（触发四类/两组生成器/进程锁合并）+ SPI PutWithOptions（T-83 契约）。review 0 blocker：4-worker 并发探针终态收敛零 5xx；AC6 旁车现算对账；三沉默裁决全确认。7 non-blocking（T-83 godoc 措辞偏差转 architect/dotted 段守卫建议/async 超时排队面）。提交 dad9458+e61e580。

- **T-72** [P1] virtual metadata 聚合 `role:dev-registry-adapter` `area:adapter/{maven,npm,pypi}` — done 2026-08-20
  三协议聚合面：maven 桶序合并（MNG-5180/优先短路/block 透传）/npm putIfAbsent+并集/pypi 条目并集+JSON 回退。15 矩阵群全真栈；**npm/pip 真客户端 M54/M55 全过**；mvn 环境阻塞走 curl 等价（归 T-74/76）。跨 area 补缝（repo SPI 三方法 ~150 行，成员资格守卫）已 flagged 待 architect 复核。上游计数 2→3 校准点转 T-75。提交 09f833c。

- **T-84** [P0] cmd 三协议装配 `role:dev-go-core` `area:cmd/binflow-server` — done 2026-08-20（经 429 续完）
  npm（New+WithAuth+WithLedger+Register）/pypi（Register 一次调用）入 Deps.Adapters；真二进制三协议 smoke 全 exit 0（npm publish+install/pip twine+download/mvn deploy+dependency:get）+ 读面全 200 零 ERROR。QA 真客户端前置就绪。提交 59855b1。

- **T-73** [P2] sha1-only checksum deploy `role:dev-go-core` `area:internal/metadata(增量)、internal/repo、adapter/{maven,generic}` — done 2026-08-20
  GetBySha1（idx_blobs_sha1 消费者，纯增量三文件）+ PutFromBlob sha1 寻址（权限对前解析）+ generic 删旧拒绝分支 + maven putChecksumDeploy（ME-08 gate 后/artifact-only/calc 触发）。四触及包 race 绿 + 16 包 ok。area 偏离已申报（T-62 只留索引缝，接口面无查询——无法仅在 area 内实现）。npm/pypi 未启用（PRD 未点名）。提交 f1323ff。

- **T-74** [P0] QA 三协议功能矩阵 `role:qa-engineer` — done 2026-08-20（PASS 49/49）
  mvn/npm/pip/twine 真客户端全矩阵（M01~M05/M10~M21/M22~M28/M30~M35b 勘误口径全对）；边界+穿越 12 变体+NFR-S16/17/18+四协议去重全过；5xx=0；5 条 PRD 勘误建议（E1~E5 均不阻塞）。被测 f597c86 独立 worktree。报告 reports/agents/T-74-qa.md。

- **T-75** [P0] QA remote/virtual+SSRF `role:qa-engineer` — done 2026-08-20（PASS）
  M41~M48 全序（16 直连变体全 400 + NAT64 拆解/Teredo 直拒/DNS64 保留侧不拦 + 19 WARN 全录 + 300s 真静默窗）；virtual M50~M55b（C5 逐字/优先桶/聚合/hardFail 透传钉板/T-72 校准点实证）；NFR-S13~S15+S14（enc:v1: 明文 0）+P14（1GB RSS +28KB）。3 条 502 全设计内。PRD 勘误 E1/E2 + 观察 O1~O3 转交。报告 reports/agents/T-75-qa.md。

- **T-76** [P0] QA 客户端矩阵+回归+性能 `role:qa-engineer` — done 2026-08-20（PASS）
  mvn 五链/npm 7/pip 6/curl 4/Gradle P2 观察 1 = 25/25；M50/M54 三协议收口断言成立；M1 C 序列 23 + M2 D 序列 14 回归全绿（E-07/C26/E-26 反转成立）；docker D16 全链五域去重闭环；性能（冷启动 0.118s/50 并发 3.39s 零 5xx/M60c 182ms 分解为 M1 fsync 固定成本非 M3 引入——O1 转 PM）。**DoD §9 第 1/2 条终判：满足**。报告 reports/agents/T-76-qa.md（M3 QA 总报告）。

- **T-77** [P1] M3 用户文档 `role:tech-writer` `area:docs/user` — done 2026-08-20
  四篇指南 723 行（maven/npm/pypi 接入 + remote/virtual 管理）：建仓字段表 v1.2 默认值/SSRF 放行指引/凭据 env 与 fail-fast/14 行定案报错码逐字/不兼容全表；五个客户端坑收编；T-76 基线产物抽样复跑五链 exit 0（含 enc:v1: 有/明文无、405 C5 逐字）。提交 e7d2336。

- **T-86** [P0] M4 架构增量 `role:architect` `area:docs/design、DECISIONS.md` — done 2026-08-20
  ADR-0014（console 挂 /binflow/console 保留段 + server-side session 三层 CSRF + 无专属 API 树 + vite 构建链与 ADR-0005 边界澄清）；ADR-0015（GC 在线四安全边界 + repo_usage 同事务配额 + 备份先 DB 后 blobs 硬规则 + import 仅 CLI）；004 四表设计；保留字扩 {docs,console}。提交 d0ff1fb。

- **T-87** [P1] 控制台信息架构与线框 `role:ux-designer` `area:docs/design` — done 2026-08-20
  console-ux.md 665 行十节：导航树+18 路由表 / 11 页线框（权限编辑器模式测试器+diff 确认为核心）/ 四态矩阵+大 repo 骨架屏 / keyset 增量加载策略 / 暗色双主题 --bf-* token / 五协议×三仓型呈现差异矩阵 / R1~R10 API 需求清单。三大决策：协议能力收窄不伪装（UI 上传仅 generic/maven）、大目录 keyset+懒加载、权限编辑器防 ACL 漂移。提交 a6f0d77。

- **T-85** [P0] M4 PRD v1.0 `role:product-manager` `area:docs/prd、ROADMAP.md` — done 2026-08-20
  milestone-4.md 725 行：FR-23~FR-33 共 60+ AC（控制台/权限完整模型/治理四件）；29 端点矩阵 + W01~W40（curl+Playwright+CLI）；Q1~Q6 附暂行（session/配额粒度/搜索范围/备份窗口/docker remote 推迟/GC 形态）；M1~M3 遗留收编 9 条；ROADMAP 切 M4。与 ADR-0014/0015 裁决对齐（Q1 session 与 ADR 一致）。提交 3d1cfbf。

- **T-88** [P0] M4 工程 ticket 拆解 `role:tech-lead` — done 2026-08-20
  19 票（T-89~T-107）+9 批次+R1~R10；R1 裁决「PRD 面 + ADR 内核」；复用清单 10 面；ux R1~R10 映射；P2 债务十条。全文 reports/agents/T-88.md。提交 6f99900。
- **T-109** [P1] PRD v1.1 对齐收口 `role:product-manager` `area:docs/prd` — done 2026-08-20
  R1 对齐注记（PRD 面胜出+ADR 内核生效+T-108 勘误归属）；R4 TTL 双键（hours 主键+seconds 覆盖键）；K1~K3 回写注记。提交 18fe44c。

- **T-108** [P0] M4 勘误收口 `role:architect` `area:DECISIONS.md、docs/design` — done 2026-08-20
  ADR-0014 勘误（命名对齐 PRD 面：/binflow/ui 301/session 三动词含 whoami/binflow_session/TTL 双键/CSRF 改 Origin 校验）+ ADR-0008 保留字并集 {api,v2,docs,console,ui} + R2 架构勘误（gc 同步+互斥/usage 端点/groups 兼容层/004 email 列+audit 索引+user_groups 表名）+ ADR-0015 顺带勘误（报备）。R1 门槛清除，T-89 解锁。提交 ad418c5。

- **T-89** [P0] web 前端工程脚手架 `role:devops-engineer` `area:web/、internal/console、Makefile、CI` — done 2026-08-20
  vite6+React19+TS（base=/binflow/ui/）+ go:embed 三形态 Handler + relink-assets + CI node20 步（npm audit/tsc/eslint）+ Playwright 基建。conductor 复现：make console 75KB SPA（1.4% 预算）/console 测试绿/占位态 build 18.75MB。/binflow/ 301 live 证据；真 Chromium 2 spec 过。router 挂载归 T-91。提交 358b3c1。

- **T-90** [P0] 004 迁移+Groups/WebSessions/审计扩展 `role:dev-go-core` `area:internal/metadata` — done 2026-08-20（APPROVE 一轮过）
  review 0 blocker：DDL 逐列一致；同毫秒 keyset 翻页独立探针（50 行/带全页无跳重）；EXPLAIN 独立复核（sqlite 3.43.2 复合索引倒扫免 SORT）；幂等语义实测（changes() 同值 UPDATE）。6 non-blocking（cursor 形状校验/EXPLAIN SQL 漂移/索引列序断言/nil-vs-empty/哨兵同名——转 T-93/T-97 派单注意）。范围外：T-92 在制文件混入提交（粒度问题无缺陷）。提交 595e090+bf3f804。

- **T-92** [P0] 搜索域 `role:dev-go-core` `area:httpapi+repo+metadata` — done 2026-08-20（APPROVE 一轮过）
  review 0 blocker：ACL 零泄漏（与内容面同一 allow() 路径 + 双引用探针实测）；LIKE 转义/参数化/索引真实；fileInfoOf 纯提取。4 non-blocking（宽结果 limit 门→T-105 探针/零授权 200 空 vs 403 姿态→PRD 半句）。提交 358b3c1+0390958。

- **T-93** [P0] 审计查询面+词表 `role:dev-go-core` `area:internal/audit、internal/httpapi(audit)` — done 2026-08-20
  Filter 全参数+消费侧 keyset+NormalizeTimestamp；GET /api/v1/audit（limit 1..1000/cursor/403 矩阵）；W22 窗口/W23b 脱敏 grep 0/W39 append-only 全 404；NFR-S21 源码扫描测试；M4 十动作常量。conductor 复现：audit race 3.0s 绿/lint 0/4 HTTP 测试 PASS。提交 ccc1862（t93_audit_test.go 随 13bc7f3 补齐——依赖 T-95 的 harness 真 audit 接线）。

- **T-95** [P0] 治理字段+配额 enforcement+usage `role:dev-go-core` `area:internal/repo、internal/metadata(usage)、internal/httpapi(usage)` — done 2026-08-20（单 review 一轮修复）
  includes/excludes 双值（excludes 优先、默认 **/* 零开销短路）+ quotaBytes enforcement（Put 四族挂门、413 零残留、virtual 按目标 local）+ UsageStore 同事务 delta 标量子查询（无读→写升级）+ 005 回填 + usage 端点。review 取证：匹配器同构零漂移/挂点完备（docker finalize·mount·manifest step-1 无绕过）/413 原子性成立。B1（声明 checksum 幂等重传在配额顶误拒 413——三处 replaced 条件方向反）修复：existing != nil + 注释重写 + TestQuotaIdempotentRetransmitAtCeiling 三臂回退法验证 + 弱用例修正；NB2 action alias。conductor 复核：build/lint 0/定向测试 PASS。提交 13bc7f3+0a80ed0。遗留：docker /v2 渲染→T-111、manifest 双计数显示口径、预检非预留、透传清空局限、NB1/NB3/NB5/NB6 登记。

- **T-110** [P1] assets 保留字 + TTL 塌缩句 `role:architect` `area:DECISIONS.md、docs/design` — done 2026-08-20（经 429 续完）
  ADR-0008 增补 assets（六字集并集）+ ADR-0014/§7.5 塌缩句（会话必死于 created_at+TTL 与活跃度无关——防前端/QA 误读）+ §6 DDL 注释同步。T-108 遗留①闭合。提交 8f26a0a。

- **T-91** [P0] session 三臂+console 挂载+CSRF `role:dev-go-core` — done 2026-08-20（经双 review 一轮修复 + 2 次 429）
  Cookie 第三臂（256bit/失效即拒/Touch 封顶）+ session 三动词 + csrfGuard（Origin + XFP）+ console 挂载 + TTL 双键。curl 全周期 24 PASS + Playwright 探针转绿。双 review 修复复审通过：B1 登录端点豁免（stale cookie 5 子例 + B1B2 咬合）；B2 login-CSRF Origin 守卫（6 子例）；assets 保留字；N6 audit nil 兜底。16 包 race 绿。提交 906d82a+67c3380。**批 2 闭环**。

- **T-138** [P0] FR-39 systemd 单元+安装脚本（PB-07）  — done 2026-08-21（批 4 完成）

- **T-139** [P0] FR-40 离线安装包（PB-08）  — done 2026-08-21（批 4 完成）

- **T-140** [P1] FR-47 URI 硬编码 /binflow 修复  — done 2026-08-21（批 4 完成）

- **T-142** [P1] FR-41 内容（下）：API 参考+FAQ 扩写+管理页增强  — done 2026-08-21（批 4 完成）

- **T-144** [P0] QA 全链+回归基线  — done 2026-08-21（批 5 完成，28/28 PASS）
  G30a~G30d 目录实体化 4/4 + G30e Playwright FE 4/4 + G31a~G31c Token 审计 3/3 + G32 Docker 树视图 6/6 + G33 URI 基址族 1/1 + G34 四里程碑回归 1/1 + §5.6 反转表 6/6 + FR-44-AC6 3/3。修复 t131-g30e.spec.ts 两处 data-testid 不匹配。日志 reports/agents/T-144-qa.md。

- **T-146** [P0] QA：文档中心（剧本 4） `role:qa-engineer` — done 2026-08-21（批 6 完成，6/6 AC PASS，报告 reports/agents/T-146-qa.md）
  G18 离线可用性 PASS + G19 文档完整性 PASS + G19b 帮助入口 PASS + G20 curl/doc 一致性 PASS + G21 2.25MB<<40MB PASS + G22 构建零错误 PASS

- **T-147** [P0] QA：性能基准 + GA 总矩阵 + 发布清单（剧本 7/8/10） `role:qa-engineer` — done 2026-08-21（批 7 完成，M5 最后一票）
  AC1 性能：1000 并发零 5xx（421.9 req/s 聚合）、冷启动 98ms（<2s）、RSS 物理足迹 17.7M（<100MB）、1GB 流式 RSS 增量 ~6MB（<256MB）、7 次同内容上传 = 1 物理 blob。AC2 基准：REST GET/PUT 吞吐、搜索 P95 23ms、export 1459.8 MiB/s、GC 27-30ms、vs M4 无 >20% 回归。AC3 GA 总矩阵：94/98 PASS（3 DEFERRED Q3 条件腿 + 1 BLOCKED nginx SSL）、6 平台 sha256 全部验证、M5 DoD 7/7 条 PASS。报告 reports/agents/T-147-qa.md
- **T-145** [P0] QA：发布矩阵与部署形态全量（剧本 2/3 + 条件腿） `role:qa-engineer` — done 2026-08-21（批 6 完成，PASS 46/49，1 BLOCKED + 2 DEFERRED）
  G01~G04 产物+版本+可跑腿 PASS + G06~G08 Docker 双变体 8/8 PASS + G09 compose 5/6（nginx SSL 证书 BLOCKED）+ G11~G13b Helm 9/9 PASS + G14 K8s 7/7 PASS + G15~G16 systemd 4/4 PASS + G17 离线包 4/4 PASS。G05 Windows/G15b systemd 真机 DEFERRED（用户环境未到位）。报告 reports/agents/T-145-qa.md

## ✅ 已完成（done）— M6

- **T-148** [P0] goreleaser 三二进制发布矩阵扩展（bf + bf-migrate） `role:devops-engineer` — done 2026-08-21（conductor 核验直收）
  `make build` 产出三二进制（bf 2.6MB / bf-migrate 2.6MB / binflow-server 22MB，全在预算内）；`--version` 同源 ldflags 注入；`.goreleaser.yaml` 六平台三ID；`make check-size` 覆盖三二进制；`make test` 三二进制 race 绿。日志 reports/agents/T-148.md。

- **T-149** [P0] M6 依赖白名单准入（go.mod + ADR-0005 扩展） `role:devops-engineer` — done 2026-08-21（conductor 核验直收）
  `go.mod` 新增 minio-go/v7 + go-oidc/v3 + go-ldap/v3；`make check-deps` 零 CGo 基线通过；`go mod verify` 通过；`make build` 三二进制全在预算内；`make vet` 零告警；18/18 测试包 race 绿。ADR-0005 白名单追加。日志 reports/agents/T-149.md。

- **T-150** [P0] storage.Backend 接口 + Engine 签名变更（Open → io.ReadCloser） `role:dev-go-storage` — done 2026-08-21（conductor 核验直收）
  新增 `internal/storage/backend.go`（包内 Backend 接口：Put/Get/Delete/Exists/List）；`internal/storage/api.go` Engine.Open 返回类型从 `io.ReadSeekCloser` 变更为 `io.ReadCloser`；`internal/storage/engine.go` 实现适配；`engine_test.go` 测试适配。全量 storage 测试 race 绿，11 个依赖包零回归。日志 reports/agents/T-150.md。

- **T-153** [P0] auth.IdentityProvider 接口 + 认证臂扩展（OIDC Bearer 臂） `role:dev-go-core` — done 2026-08-21（conductor 核验直收）
  新增 `internal/auth/identity.go`（Provider 类型/Claims/ProviderUser/IdentityProvider 接口/ErrProviderUserNotFound）；`internal/auth/api.go` Principal 新增 Source 字段；`internal/auth/authenticator.go` 新增 WithOIDC()/authenticateOIDC()/userCreator 接口；`internal/auth/deps.go` userStoreAdapter 适配 Provider/ProviderID。全量 auth 测试 race 绿，11 个依赖包零回归。日志 reports/agents/T-153.md。

- **T-152** [P0] S3 配置与健康检查（config 段 + /healthz 扩展） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/config/api.go` S3Config 结构体/StorageBackend 常量/Backend 字段；`internal/config/config.go` S3 默认常量/S3SecretEnvVar/splitEnvKey；`internal/config/load.go` raw 结构体 S3 子段/build/defaults/setEnvValue/rejectSecrets；`internal/config/validate.go` backend 枚举校验/S3 required fields/skip data_dir 创建；`internal/httpapi/system.go` probeS3Storage（BucketExists→PutObject→RemoveObject）。config 40 测试 race 绿，httpapi 构建通过，依赖包零回归。日志 reports/agents/T-152.md。

- **T-154** [P0] OIDC Provider 实现（go-oidc/v3） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/auth/oidc.go`（~796 行，OIDCProvider 实现 IdentityProvider 接口/PKCE 生成器/claims 提取/go-oidc/v3 ID Token 验证）；`internal/auth/oidc_test.go`（13 测试，httptest+jose mock OIDC server）；`internal/auth/deps.go` 依赖连线（GetByProvider/adaptUser）。auth 62 测试 race 绿，repo/httpapi/6 适配器包零回归。遗留：metadata.User 缺 Provider/ProviderID 字段（008 migration DDL 已有但 Go struct 未更新），userStoreAdapter.GetByProvider 使用 O(n) List() 遍历。日志 reports/agents/T-154.md。

- **T-151** [P0] S3Engine 实现（minio-go/v7） `role:dev-go-storage` — done 2026-08-22（conductor 核验直收）
  新增 `internal/storage/s3.go`（~755 行，S3Engine/s3Session/s3Backend 实现，multipart upload/go:generate 注册）；`internal/storage/s3_test.go`（~1000 行，mock S3 HTTP server + 25 table-driven 测试）。全量 storage 测试 race 绿（41.4s），repo/httpapi/6 适配器包零回归。mock server 并发写同 blob 收敛测试修复（uploadID 唯一性）。T-150 Backend 接口消费方完工。日志 reports/agents/T-151.md。

- **T-155** [P0] LDAP Provider 实现（go-ldap/v3） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/auth/ldap.go`（~596 行，LDAPProvider 实现 IdentityProvider 接口/连接池/Bind 流/搜索组/AdminGroup 判定）；`internal/auth/ldap_test.go`（~949 行，mock LDAP server + 16 table-driven 测试）。auth 25.5s race 绿，repo/httpapi/6 适配器包零回归。日志 reports/agents/T-155.md。

- **T-156** [P0] 008 元数据迁移（users 表 provider 列 + 认证臂优先级） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `migrations/{sqlite,postgres}/008_oidc_ldap.sql`（users 表 +provider/provider_id）；`internal/auth/deps.go` NewLDAPResolver + adaptUser 读 Provider/ProviderID；`internal/auth/authenticator.go` WithLDAP/authenticateLDAP（Bind→Resolve→自动建用户）；`internal/auth/session.go` AuthenticateCredentials 本地密码失败后 LDAP 回退；`internal/auth/ldap_login_test.go` 新增登录回退测试组；metadata User struct/查询/Create/Get/List 全链路读写 provider 列。conductor 复核：auth 60.1s / metadata 62.2s / repo 161.6s / httpapi 全部 race 绿，build+vet 干净。遗留：GetByProvider O(n) 遍历（可后续加索引）。日志 reports/agents/T-156.md。

- **T-161** [P0] replication 模型与 009 迁移（replications + replication_tasks 表） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/replication/{model,store}.go`（ReplicationConfig/ReplicationTask/ConfigStatus 聚合/Store 接口/SQLiteStore 实现，UNIQUE(name)+busy 503+status 白名单）+ `model_test.go`（11 测试 CRUD 全路径）+ `metadata/replication_internal_test.go`（009 幂等+列形状锁定）。009 SQL 以 architecture.md §6 定稿名为 `009_replication.sql`（AC 写 `009_replication_tables.sql`，以架构契约为准）。conductor 复核：replication 17.6s + metadata 119.3s race 绿。遗留：Store 未接 metadata.Store 装配面（桥接票）；isUniqueViolation 仅 sqlite 文案。日志 reports/agents/T-161.md。

- **T-164** [P1] 本地→S3 在线迁移（双写+后台迁移） `role:dev-go-storage` — done 2026-08-22（conductor 核验直收）
  新增 `internal/storage/migration.go`（MigrationEngine 三模式 bypass/dual-write/completed；migrationSession TeeReader+Pipe 流式双写、Commit 侧失败回滚；statusGuard 原子快照含 defer 覆盖 bug 修复）+ `migration_test.go`（14 测试，含 927 blob 全量迁移 S3 端硬断言）+ `internal/httpapi/migration.go`（状态/启动端点）+ server/router 接线。agent 一度因 API 400 中断后原地续跑收尾。conductor 复核：storage 196.3s + httpapi 203.9s race 绿。越区发现：binflow-server 红测试属 T-168 遗留（已记录）。日志 reports/agents/T-164.md。

- **T-160** [P1] S3 迁移进度控制台 UI `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  新增 `web/src/pages/governance/MigrationPanel.tsx`（5s 轮询 hook + 四态：Skeleton/403 整面板隐藏/501 未配置降级/ErrorCard 重试且瞬断保留旧值）+ GCPage 接线 + `web/e2e/storage_migration.spec.ts`（5 用例 hermetic 全 mock）。conductor 复核：build 1.34s + Playwright **5/5 passed (7.2s)**。**契约漂移记录**：票面字段 total_blobs/in_progress/completed 不存在，实存契约为 total/running/done（以 internal/storage/migration.go json tag 为准），前端按实际实现——待 architect 回写契约；未配置实返 501（非 AC 写的 404）。遗留：迁移启动按钮（危险面）建议单独出票；console-ux.md 路由表待补录；web 全量 lint 存量 3 错（他人 spec）建议 chore 票。日志 reports/agents/T-160.md。

- **T-170** [P2] 部署矩阵更新（compose/k8s/helm 含 S3 + OIDC + LDAP 配置示例） `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  compose：minio（--profile s3 锚定版本+healthcheck）+ minio-init 建桶 + S3 env 段（密钥纯 env 引用）+ depends_on 门控（默认路径零改动）；charts/binflow：values config.s3/oidc/ldap 三段 + configmap 透传（顺带修正 2 处既有渲染缺陷）+ secretKeyRef + schema/NOTES 同步；k8s 清单注释示例段。conductor 复核：helm lint 0 failed + compose config 默认/s3 双路 OK + kustomize OK。烟测：默认盘路径全绿（无回归）；--profile s3 建桶→healthy→起服→roundtrip sha256 一致。**发现两处上游缺口（已建 T-178）**：probeS3Storage Secure:true 写死（http MinIO /readyz 永久 503）；cmd 装配无 backend 分支（S3 数据面未激活）。遗留：上游修复后补一次 --profile s3 全绿复测（归 T-178 验收）。日志 reports/agents/T-170.md。

- **T-157** [P0] OIDC/LDAP HTTP 端点挂载 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/httpapi/oidc.go`（OIDCLoginFlow seam + login 302 state+PKCE S256 + callback code→token→Bearer 臂验证→自动建用户→签发 session；事务 cookie 全退出路径清除）+ server Deps.OIDC（nil→disabled 404 不暴露端点）+ router 挂载 + T-91 登录豁免重构 isLoginEntryPoint 覆盖 OIDC 路由（陈旧 cookie 不锁死 SSO）+ `oidc_routes_test.go`（mock IdP 7 组）+ `ldap_session_test.go`（真 LDAPProvider+mock 目录 7 例「先本地后 LDAP」全栈验证）。conductor 复核：scoped OIDC/LDAP race 绿（6.4s）+ build/vet 干净；agent 自跑全量 httpapi 117.8s 绿。**第三处装配缺口 → 已建 T-179**：config 无 auth.oidc/ldap 段、cmd 未构造 provider、auth 缺 userCreator 导出（阻塞 T-174）。遗留：whoami source 硬编码 local（并入 T-179）；本地/LDAP 同名用户冲突行为待 Q5 定案（现状 500）。日志 reports/agents/T-157.md。

- **T-162** [P1] push 复制引擎（事件驱动 + cron 兜底） `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  新增 `internal/replication/engine.go`（Enqueue panic 护罩非阻塞 / Run drain+wake+cron 兜底 / 退避 1s→16s 共 6 次尝试 / pushOnce HEAD 幂等探测+PUT 走目标 REST 带 X-Checksum-Sha256 / 复用 remote 的 SSRF Guard 与 AES-GCM Cipher / 全量测试注入缝）+ engine_test.go（15 测试）+ engine_integration_test.go（**两真实实例**：A 上传 201→推送→B GET 200 sha256 一致、replica PUT/DELETE 405、目标宕机上传不受影响）+ repo 侧 Replicator 接口最小接线（PutWithOptions 链末 notifyReplicator，goroutine+WithoutCancel+recover）+ 挂钩 5 测试。conductor 复核：replication 18.7s + repo 104.6s race 绿，build/vet/lint 干净。**Q6/Q7 暂行假设（待定案，已标注单点切换位 pushOnce HEAD 分支）**：Q6=可写 local backing+未路由 virtual 只读门面；Q7=checksum 一致幂等成功/不一致 failed 不动目标（与 ADR-0021 字面有出入）；私有目标默认放行（DenyPrivateTargets 保留收紧位）。遗留：cmd 装配+replications REST 端点待桥接票；审计词汇/限速项未消费。日志 reports/agents/T-162.md。

- **T-169** [P2] M5 债务收编 — G05 Windows 锁 + G15b systemd 裸机部署 `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  发现 AC① 实质已被 T-96 满足（datalock_windows.go LockFileEx 已落）——补 windows 真机腿测试文件 + 修 backup_test 中 Windows 必红断言（GOOS 感知）+ 空路径守卫子测试，不动 go.mod。G15b：contrib/systemd 扩展——binflow.service（SIGTERM+TimeoutStopSec=45 优雅停机锚点+UMask=0027）+ install.sh 修 3 个真机 bug（sha256 CWD 解析/重装 ETXTBSY/全新安装 restart 循环）+ dry-run 全链路 + is-active 硬门。**真机烟测**：Ubuntu22.04+systemd251 容器（PID1）install→active→readyz 200→建仓/上传/下载 sha256 一致→restart 存活→优雅停机日志→幂等重装→systemd-analyze verify→purge 全绿。conductor 复核：scoped DataLock/Backup race 绿 + bash -n + windows/linux×amd64/arm64 交叉编译 OK。遗留：make test 全量绿被 T-168 遗留红阻断（非本票区）；docs/user/install/systemd.md 对齐待他人票。日志 reports/agents/T-169.md。

- **T-158** [P1] 控制台 SSO 登录 UI `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  LoginPage 增 SSO 按钮（探测 `GET oidc/login` redirect:manual——302=启用/404=禁用，浏览器不触达 IdP；点击复核+行内错误态+503 保留重试）；密码表单零分支（LDAP 同表单 401→200 落地壳用例）；styles 仅登录页小节追加纯 token。conductor 复核：build 2.65s + Playwright **5 passed**（--repeat-each=2 → 10 passed）+ 既有 storage_migration spec 无回归。**后端需求提议（未越权实现）**：公开 `GET /api/v1/auth/methods`——已并入 T-179 AC⑥。遗留：真实启用态验证归 T-174（T-179 接线前置）；make console 嵌入重编归主会话集成步骤。日志 reports/agents/T-158.md。

- **T-178** [P0] S3 后端服务端装配接线 + /readyz Secure bug `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  `system.go` 新增 secureFromEndpoint（https→true/http→false/无 scheme→TLS 默认），probeS3Storage 不再写死——根因实证：minio-go v7.3.0 要求 scheme 与 Secure 一致否则 minio.New 直接报错。`main.go` openStorageEngine 按 backend 分支（disk 逐字保留；s3 构造 minio 客户端 env secret fail-fast + BucketExists 缺桶拒起 + OpenS3EngineWithClient；migration 按 T-164 语义接线双写/纯 S3/拒绝非法组合）。**附带发现上游签名缝**：`*MigrationEngine.StatusView()` 不满足 `httpapi.MigrationStarter.StatusView() any` 签名（REST 迁移端点将恒 501）——cmd 侧 migrationStarter 适配器桥接，收编归 T-180。conductor 复核：scoped readyz/S3 装配 race 绿（httpapi 7.9s + cmd 2.7s）+ **全量 httpapi 统一复跑 114.5s 绿**（T-157+T-178 合流）+ build/vet 干净。红绿证明：还原 Secure:true 旧 bug 签名复现 FAIL。遗留：T-180 收编两项；bucket 不自动创建（按必须预存在）。日志 reports/agents/T-178.md。

- **T-176** [P2] 契约回写与杂项 chore `role:architect` — done 2026-08-22（conductor 核验直收）
  architecture.md §7.1 补 migration 两端点契约（字段以实现 json tag 为准 + 501 非 404 语义 + 回写记录标注）+ §8 配置段与校验规则；console-ux.md 三处补录（路由表/线框/14 testid 锚）；web/e2e 三 spec 存量 no-unused-vars 最小修复。conductor 复核：`npx eslint e2e/` exit=0 两轮一致 + diff 恰为申报 5 文件。遗留分流：PRD 旧字段勘误 → T-181；migration.go godoc 口径 → T-180 AC⑥；前端 pct 基数口径 → T-177 AC④。日志 reports/agents/T-176.md。

- **T-159** [P1] 控制台复制面板 UI `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  新增治理组「复制」页 ReplicationPage.tsx（目标表+事件表、10s 轮询、四态收敛）+ AppShell 导航 + lazy 路由（2 行既定装配缝）+ hermetic 7 用例 spec。落位裁决：票面「管理页」无对应页，按信息架构归治理组新开页。**契约假设清单**（端点尚未桥接）：按 internal/replication/model.go 推定 `{targets[], events[]}` 聚合形状，字段名/空值/降级语义全量标注在 T-159.md 与组件头注释——**T-180 桥接票的对齐基准**。conductor 复核：build 1.49s + Playwright **7/7 passed** + 全量 lint/typecheck exit 0。回归对照：全量 e2e 73/3（2 例基线同败为既有、1 例隔离重跑过=顺序性 flake）——**watch 项：基线 2 败待溯源**。遗留：console-ux 回写 → T-182；事件 keyset 分页待真实端点。日志 reports/agents/T-159.md。

- **T-179** [P0] OIDC/LDAP 认证面服务端装配 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  config 三文件加 auth.oidc/ldap 段（键名逐字对齐 T-170 charts snake_case；rejectSecrets 递归；validate 必填+URL 形状）；auth 导出 NewUserCreator/NewOIDCResolver + OIDCWired/LDAPWired facet + authenticateSession Source=u.Provider（不再硬编码）；httpapi auth_methods.go `GET /api/v1/auth/methods`（匿名三态）；cmd wireAuthProviders（OIDC discovery fail-fast/LDAP 懒池/typed-nil 守卫/close 排空）。**范围偏离已接受**：whoami 序列化点物理在 httpapi/session.go，最小增量加 source 字段。conductor 复核：config 1.9s + auth 29.1s（一次并行负载 flake 复跑绿）+ scoped httpapi 6.9s + cmd 3.0s race 绿；cmd 全量仅剩 T-168 两已知红。**发现 charts 缺陷 → T-183**：configmap oidc 块漏渲染 enabled 行（Helm 启用 OIDC 静默失效）。遗留：PRD skip_tls_verify/group_base_dn 无对应字段未实现（规格待验证）；users 列表 source 字段归属 T-174 前确认。日志 reports/agents/T-179.md。

- **T-182** [P2] console-ux 复制页回写 `role:architect` — done 2026-08-22（conductor 核验直收）
  console-ux.md 五处补录（导航/路由表/线框 23 行/11 个 repl-* 锚/三处一致性修正），全部以 ReplicationPage.tsx 实际形态为准；同款回写题头 + 假设契约标注（T-180 对齐锚）。conductor 复核：diff 单文件。日志 reports/agents/T-182.md。

- **T-181** [P2] PRD M6 勘误 `role:product-manager` — done 2026-08-22（conductor 核验直收）
  PRD v1.0→v1.1：§4.1 FR-50 实际契约+501 语义+start 端点；§5.2/§5.4 矩阵同步；§7 Q6/Q7「暂行已实现待终裁」+新增 Q10（私有目标放行）+交叉指针。三方依据链核对（architecture≡migration.go≡httpapi 501 文案）后落笔。conductor 复核：旧字段名仅剩 2 处有意保留（勘误记载）。遗留：ROADMAP「PRD v1.0」字样待改（区外）；ADR 拟编号与 DECISIONS.md 错位记录在案。日志 reports/agents/T-181.md。

- **T-177** [P2] 迁移启动按钮 UI `role:dev-frontend` — done 2026-08-22（conductor 核验直收）
  MigrationPanel 启动按钮（数据态+!running 渲染/403 隐藏/501 无按钮）+ useConfirm 复用（danger+YES 门+四条影响说明按实际语义）+ 202 即刻并入 + 409 行内提示；pct 基数改 (migrated+failed)/total（AC④）。conductor 复核：Playwright **8 passed** + build 1.3s + typecheck/lint 0。遗留：console-ux 三处回写与 T-182 同族（migration-start 等三锚）→ 并入后续文档票；无停止迁移 REST 面（另立票候选）。日志 reports/agents/T-177.md。

- **T-183** [P1] charts oidc enabled 渲染补丁 `role:release-engineer` — done 2026-08-22（conductor 核验直收）
  configmap.yaml oidc 块 +1 行 `enabled: {{ .Values.config.oidc.enabled }}`（逐字对齐 ldap 块）。conductor 复核：helm lint 0 failed + template grep `enabled: true` 在位 + diff 恰 1 行。agent 附负面对照（删行复现静默失效链）。验证中确认 T-170 的 existingSecret required 门为有意防呆。日志 reports/agents/T-183.md。

- **T-180** [P1] 复制面桥接收编 `role:dev-go-core` — done 2026-08-22（conductor 核验直收）
  httpapi replication.go（CRUD 四端点 + status 聚合，T-159 契约**零差异**落地：凭据不下发/''哨兵/[]非 null/newest-first/默认 limit 50）+ Deps 两 seam + MigrationStarter 签名收编（storage 强类型直插，删 cmd 适配器）+ SecureFromEndpoint 导出单点化（删 cmd 副本）+ storage godoc 口径修正；cmd 复制引擎全装配（第二 store 连接池/同钥 cipher/AttachReplicator/start+drain 生命周期）。**真二进制 smoke**：空 200/匿名 401/校验 400/重复 409/上传后 pending 任务行/DELETE 204 FK 级联/SIGTERM→drained→exit。conductor 复核：replication 10.7s + scoped httpapi 5.7s + cmd skip 25.1s race 绿（agent 自跑 httpapi 全量 119.6s 零回归）。**遗留②重大**：`.gitignore:51` 裸名吞掉 cmd/ 下未跟踪测试文件——conductor 已修为 `/binflow-server` 并入 batch 3。遗留：audit 词汇/CRUD UI 票/sub-store DSN 收编。日志 reports/agents/T-180.md。

## 🚫 阻塞（blocked）

（空）
