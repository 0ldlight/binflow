# Sprint 289 迭代报告 — T-156 收口 + API 故障恢复（T-164 续跑 / T-165/T-168 回炉）+ T-161 派发

**日期**: 2026-08-22
**上轮**: Sprint 288（并行派发 T-165/T-168）
**本轮焦点**: T-156 核验收口；三个 agent 因瞬时 API 错误（400 modelCode）中断的处置；T-161 派发
**触发**: /loop 10m 挂载后的首轮（cron job e2981b8e）

## 阶段 0 — 复位

- 完成通知：T-156 agent 完成（状态 done，11 文件变更）
- 故障通知：T-164/T-165/T-168 三个 agent 于 01:38~01:41 相继因 `API Error 400 [1214][modelCode 不存在]` 中断——是提供方瞬时故障，非任务失败
- 后台命令 b60b6f77l（httpapi race 测试）exit 0 完成

## 阶段 1 — 补给看板（Planning）

⏭️ 跳过（todo 非空）。

## 阶段 2 — 收口（Close-out）

### T-156 — 008 元数据迁移 + 认证臂优先级 ✅ 核验通过

**agent 产出**（11 文件）：
- `migrations/{sqlite,postgres}/008_oidc_ldap.sql`（NEW）：users 表 +provider/provider_id
- `internal/auth/deps.go`：NewLDAPResolver 公共构造；NewFromStore 自动注入 userCreatorAdapter；adaptUser 读写 Provider/ProviderID
- `internal/auth/authenticator.go`：WithLDAP()/authenticateLDAP（Bind→Resolve→自动建用户→Principal）
- `internal/auth/session.go`：AuthenticateCredentials 本地密码失败后 LDAP 回退
- `internal/auth/ldap_login_test.go`（NEW）：登录回退 4 子测试 + 无 Provider/自动建用户/本地优先/Resolver 测试
- metadata 侧：api.go User struct + 字段；store.go/substores_auth.go 查询与 CRUD 全链路读写 provider 列；internal_test.go/usage_internal_test.go 回退列表同步
- `reports/agents/T-156.md` 工作日志

**conductor 复核证据**（独立重跑，非转述 agent）：
- `go test -race ./internal/auth/...` → ok 60.1s
- `go test -race ./internal/metadata/...` → ok 62.2s
- `go test -race ./internal/repo/...` → ok 161.6s
- `go test -race ./internal/httpapi/...` → exit 0
- `go build ./...` + `go vet ./internal/auth/... ./internal/metadata/...` → 干净

**遗留**：GetByProvider O(n) 遍历（后续可加 provider+provider_id 索引）——记入 done 条目。

### API 故障处置（三 agent 中断）

| Ticket | 中断时状态 | 处置 | 结果 |
|--------|-----------|------|------|
| T-164 | 14/14 migration 测试 -race 已绿，正要跑 storage 全量 | SendMessage 原地续跑 | ✅ 已恢复，后台收尾中 |
| T-165 | 正在修 TestClientAbsURL 的 base URL 断言 | SendMessage 续跑 | ❌ 系统标记不可恢复（stopped）→ 回 todo |
| T-168 | 自有测试单跑通过，正要跑全量套件 | SendMessage 续跑 | ❌ 同上 → 回 todo |

**半成品盘点**（conductor 实测）：
- T-165：`internal/client/` 在盘，`go test ./internal/client/` → **ok 101.9s**（包已可用）；`cmd/bf` ok 0.85s。重派价值高。
- T-168：go.mod/.tool-versions 已升 **go1.26.6**（本轮复核即运行在 1.26.6 工具链上）；优雅停机已实现；`deploy/nginx/` 已建。**红**：`cmd/binflow-server` 全量 `TestServeGracefulShutdownLog` FAIL + `adapter: Register: duplicate protocol "npm"` panic。
- 其余包全绿：auth/metadata/repo/httpapi/client/bf —— 工作树整体健康，仅 binflow-server 一包待修。

按系统规则，T-165/T-168 不重派，等用户明示后重派续做。

## 阶段 3 — 派发（Dispatch）

### T-161 — replication 模型与 009 迁移 已派发 🔄

- 依据：T-156 完成后 `internal/metadata` 空出；area `internal/replication / internal/metadata` 与在途 T-164（storage/httpapi）不重叠
- P0，dep T-149 ✅，dev-go-core 后台执行
- 并行度：2（T-164 + T-161），距上限 4 还有余量——但 T-157（httpapi）被 T-164 占区，T-170（deploy/）与 T-168 半成品潜在冲突，故本轮不加派

### 解锁链更新

- T-156 ✅ → T-157 可派（但 area 等 T-164 释放 httpapi）
- T-161 完成 → T-162（push 复制引擎）
- T-164 完成 → T-160（迁移控制台 UI）、T-173 前置就绪（还需 T-172）
- **受阻**：T-166/T-167（dep T-165）、T-172（dep T-168）——等 T-165/T-168 重派完成

## 阶段 4 — 落盘

- ✅ BOARD.md：T-156 → done（M6 9/26）；T-161 → doing；T-165/T-168 → todo（附半成品状态注记）；状态行更新
- ✅ 本报告
- ⏸️ git commit 暂缓：工作树含 T-164/T-161 在途改动 + T-168 遗留红测试，等收口后统一提交（M6 尚无任何 commit，从 T-148 起累积，下轮收口 T-164 后建议首次 M6 批量提交）

## 阶段 5 — 战报

### 📊 Sprint 289 总览

| 指标 | 数值 |
|------|------|
| 收口 | 1 张（T-156 ✅ conductor 核验直收） |
| 派发 | 1 张（T-161 replication 模型）+ 1 张续跑（T-164） |
| 取消 | 2 张回炉（T-165/T-168，半成品在盘） |
| 在途 | 2 个 agent（T-164, T-161） |
| 剩余 | 17 张（todo 15 + 回炉 2） |
| 已完成 | M6 总 9/26 张 |

### 🚀 下一轮计划

1. T-164 收口（storage 全量 race + 日志核验）→ 首次 M6 批量 commit 候选
2. T-161 收口 → 派发 T-162（push 复制引擎）
3. 请用户明示是否重派 T-165/T-168（半成品均高价值：client 包已全绿、Go 1.26.6 已升）

### ⚠️ 风险与开放问题

- **provider 稳定性**：本轮 3 个 agent 同窗口被 400 错误击落；loop 10m 已挂，若再现需在报告记录并向用户反馈
- **工作树未提交**：M6 全部产出（T-148~T-156 共 9 票）仍在工作树，建议 T-164 收口后立即批量提交
- 9 项开放问题（Q1~Q9）仍待用户定案