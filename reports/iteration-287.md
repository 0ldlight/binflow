# Sprint 287 迭代报告 — 等待回合（T-156/T-164 在途）

**日期**: 2026-08-22
**上轮**: Sprint 286（等待回合，T-156/T-164 在途）
**本轮焦点**: T-156 修复 migration down 索引依赖，T-164 S3 迁移执行中
**当前时刻**: 两个 P0 关键路径 agent 仍在执行，均无完成

## 阶段 0 — 复位

- BOARD.md 看板：todo 含 M6 17 张 ticket（Batch 4~8），doing 含 T-164/T-156
- 两个后台 agent 均在途，无新完成通知

## 阶段 1 — 补给看板（Planning）

⏭️ 跳过（todo 非空，Batch 4~8 完整）。

## 阶段 2 — 收口（Close-out）

⏭️ 跳过（无新完成 agent）。

## 阶段 3 — 派发（Dispatch）

### 在途进度观察

| Ticket | Agent | 最新进度 |
|--------|-------|---------|
| **T-156** | dev-go-core | 正在修复 migration rewind（down）中 `provider` 列索引依赖问题：`idx_users_provider` 索引引用 `provider` 列，需要先 DROP INDEX 再 DROP COLUMN。08 迁移 SQL 已写好，正在修复 `internal_test.go` 的 rewind 列表。 |
| **T-164** | dev-go-storage | S3 迁移（双写+后台迁移）仍在执行中 |

### 可派发评估

与 Sprint 286 一致，P0 依赖未满足或 area 冲突，P2 暂缓：
- T-161（P0，replication 模型）area 冲突 T-156（metadata）
- T-163（P1，metrics 端点）area 冲突 T-164（httpapi）
- T-165（P2，client 包）✅ 可派发但暂缓
- T-168（P2，Go 1.26.6 + 优雅停机）✅ 可派发但暂缓

### 决策：继续等待

不派发 P2 ticket 以避免分散 agent 资源。T-156 正在修复索引依赖，预计接近完成。

## 阶段 4 — 落盘

- ✅ 写迭代报告：reports/iteration-287.md
- ⏳ 等待 T-156 或 T-164 完成后收口

## 阶段 5 — 战报

### 📊 Sprint 287 总览

| 指标 | 数值 |
|------|------|
| 回合 | Sprint 287（等待回合：T-156/T-164 在途） |
| 收口 | 0 张 |
| 派发 | 0 张（暂缓，等 P0 关键路径解锁） |
| 在途 | 2 个后台 agent（T-156, T-164） |
| 剩余 | 17 张（todo 中） |
| 已完成 | M6 总 8/26 张 |

### 🔍 在途状态

| Ticket | Agent | 当前进度 |
|--------|-------|---------|
| T-156 | dev-go-core | 008 迁移 SQL 完成，正在修复 migration rewind 索引依赖 → 测试验证中 |
| T-164 | dev-go-storage | S3 迁移（双写+后台迁移）执行中 |

### 🚀 下一轮计划

T-156 修复索引依赖后进入测试阶段，预计近期完成：
1. T-156 完成 → 派发 T-157（OIDC/LDAP HTTP 端点，P0）+ T-170（部署矩阵，P2）
2. T-164 完成 → 收口 + 派发 T-160（控制台 UI，P1）

### ⚠️ 开放问题

9 项开放问题（Q1~Q9）仍待用户定案，当前按暂行假设推进。