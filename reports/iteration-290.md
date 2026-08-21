# Sprint 290 迭代报告 — 双收口（T-161/T-164）+ 首次 M6 批量提交 + 四张派发

**日期**: 2026-08-22
**上轮**: Sprint 289（T-156 收口 + T-161 派发 + T-164 续跑）
**本轮焦点**: T-161/T-164 双核验收口；M6 首次批量 commit；Batch 4/5 铺开（T-157/T-162/T-160/T-170）

## 阶段 0 — 复位

- 完成通知：T-161（replication 模型）+ T-164（S3 迁移，续跑后）双双回报 done
- 会话模式变更：用户将 effort 切至 ultracode（xhigh + workflow）——本轮仍按 SPRINT-LOOP 协议走 Agent 派发制

## 阶段 2 — 收口（Close-out）

### T-161 — replication 模型与 009 迁移 ✅ 核验通过

**agent 产出**：`internal/replication/{model,store,model_test}.go`（ReplicationConfig/ReplicationTask/ConfigStatus 派生聚合/Store 接口 + SQLiteStore 跑真实 009 schema）+ `metadata/replication_internal_test.go`（009 幂等 + PRAGMA 列形状锁定）。
- 009 SQL 开工时已在盘（随 T-156 批次落盘），agent 逐字段对照 architecture.md §6 定稿块一致后保留
- **AC 偏差（conductor 裁定接受）**：文件名 `009_replication.sql` vs AC 写的 `009_replication_tables.sql`——以架构契约定稿名为准
- 遗留：Store 未接 metadata.Store 装配面（后续桥接票）；isUniqueViolation 仅 sqlite 文案

**conductor 复核**：`go test -race ./internal/replication/... ./internal/metadata/...` → replication ok **17.6s** + metadata ok **119.3s**。

### T-164 — 本地→S3 在线迁移 ✅ 核验通过

**agent 产出**：`internal/storage/migration.go`（MigrationEngine 三模式 bypass/dual-write/completed；TeeReader+Pipe 流式双写；statusGuard 原子快照）+ `migration_test.go`（14 测试，927 blob 全量迁移含 S3 端硬断言）+ `internal/httpapi/migration.go`（状态/启动端点）+ server/router 接线。修复了 defer 陈旧局部 status 覆盖计数器的 bug。
- agent 履历：一度被 API 400 击落 → SendMessage 原地续跑 → 收尾完成

**conductor 复核**：`go test -race ./internal/storage/` → ok **196.3s**；`./internal/httpapi/` → ok **203.9s**。

**越区发现**（T-164 agent 报告，已核实归属）：
1. `cmd/binflow-server` 红测试（TestServeGracefulShutdownLog + duplicate protocol panic）——T-168 取消遗留，非本票
2. `internal/httpapi/storage.go:144` revive empty-block——HEAD 既有，开修复票候选

## 阶段 3 — 派发（Dispatch）

四张并行（达上限 4），area 互不重叠：

| Ticket | P | Role | Area | 解锁依据 |
|--------|---|------|------|---------|
| T-157 OIDC/LDAP HTTP 端点 | P0 | dev-go-core | `internal/httpapi` | T-156 ✅ + T-164 收口释放 httpapi |
| T-162 push 复制引擎 | P1 | dev-go-core | `internal/replication` | T-161 ✅（Q6/Q7 按 ADR-0021 暂行口径） |
| T-160 S3 迁移控制台 UI | P1 | dev-frontend | `web/src` | T-164 ✅ |
| T-170 部署矩阵更新 | P2 | release-engineer | `deploy/ / charts/` | T-152 ✅ T-156 ✅（绕行 deploy/nginx/ 的 T-168 遗留） |

## 阶段 4 — 落盘

- ✅ BOARD.md：T-161/T-164 → done（**M6 11/26**）；四张新票 → doing；状态行更新
- ✅ **首次 M6 批量提交**（详见下）
- ✅ 本报告

### git commit（M6 batch 1）

- 提交：`ed9de87 feat: M6 batch 1 — S3 storage+migration, OIDC/LDAP auth, replication model`（134 文件 +19113/-289）
- **纳入**：T-148~T-156、T-161、T-164 全部产出（internal/{auth,metadata,storage,httpapi,config,replication,repo,remote…}、migrations 008/009、cmd/{bf,bf-migrate}、docs/prd|design|reverse|user、Makefile/.goreleaser/ci/go.mod/go.sum、BOARD/DECISIONS/ROADMAP、reports/）
- **排除**（保持工作树脏、随其票走）：`cmd/binflow-server/`（T-168 半成品含红测试）、`deploy/nginx/`（T-168）、`internal/client/`（T-165 未闭票）、`docs-site/build`（重建噪音）、`.claude/agents/*`（非票据改动，来源未明）
- **amend 修正**：首次提交混入 `test_minio`（6.7MB 编译二进制，agent 草稿产物）——已从提交剔除并删除本地文件；全提交扫描确认其余可执行文件均为合法脚本
- 提交快照自洽性论证：排除路径均不被纳入代码 import；除 binflow-server（保留 HEAD 绿版）外所有包 race 实测绿

## 阶段 5 — 战报

### 📊 Sprint 290 总览

| 指标 | 数值 |
|------|------|
| 收口 | 2 张（T-161 ✅ T-164 ✅ conductor 核验直收） |
| 派发 | 4 张（T-157 P0 · T-162/T-160 P1 · T-170 P2） |
| 提交 | M6 batch 1 首次落库 |
| 在途 | 4 个 agent（达上限） |
| 已完成 | M6 总 **11/26** |

### 🚀 下一轮计划

1. 收口在途四张（T-157 完成 → 解锁 T-158/T-174 前置；T-162 → T-159/T-175 前置）
2. 待用户明示：重派 T-165（client 包已绿只差报告与收尾）/ T-168（修两处红+补日志）
3. 候选修复票：httpapi/storage.go:144 revive empty-block（HEAD 既有）

### ⚠️ 风险与开放问题

- API 400 风暴已两次出现（01:38 击落 3 agent）——若再现将评估降并行度
- 9 项开放问题（Q1~Q9）待定案；T-162 按 ADR-0021 暂行口径推进并在其报告标注假设