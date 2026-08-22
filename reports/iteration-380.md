# Sprint 380 迭代报告 — T-208/T-209 在途（轻量确认，无新动作）

**日期**: 2026-08-22
**上轮**: Sprint 379（T-210 收尾 done）
**本轮焦点**: T-208/T-209 仍在途、无完成通知；轻量确认存活与进度，不派发、不干预。

## 阶段 0 — 复位

- PRODUCT.md / ROADMAP.md 非空壳。
- 在途 agent：**T-208**（用户禁用 REST seam）、**T-209**（filestore session 入 DB）均存活、正在工作，无完成通知。
  - T-208：正在读 `internal/config/load.go` diff，核对 bool 指针字段模式（供 `enabled` 字段同构实现参考，只读不越界编辑）。
  - T-209：正在读 `internal/metadata/internal_test.go` 迁移测试内部，构建 migration 010 `upload_sessions` 表。
- git HEAD=`0a4d154`（sprint 379）。BOARD.md doing 区准确（仅 T-208/T-209）。
- 源码工作树未提交改动：`internal/httpapi/security*.go`、`internal/metadata/api.go`、`internal/metadata/substores_auth.go`、`internal/adapter/docker/token_test.go`、`internal/metadata/t97_group_refs_test.go` —— 均为两票在途正常改动。

## 阶段 1/2/3 — 均无动作

- 阶段 1（补给）：todo 空；无新里程碑。
- 阶段 2（收尾）：无完成通知，两票仍 doing。
- 阶段 3（派发）：todo 空；并行度 2/4 未满但无新票可派（ADR-0025 派生票已全派出）。

## 阶段 4 — 落盘

- ✅ 本报告。BOARD.md 无更新必要（doing 区准确）。

## 阶段 5 — 战报

见下方用户汇报。

## 阻塞与风险

- 无阻塞。两票在途。T-209（P1，新建表 + substore + 磁盘引擎重写 + cmd wiring）改动面最大。
- 待两票 done 后：review（T-208/T-209 关键模块可双 review）→ qa → commit → 重跑 DoD 五条 → 请示 tag/push（push 须用户单独授权）。

## 下轮计划

- 收尾 T-208/T-209；任一完成通知即进入 review/qa。
- 全 done 后 DoD 五条核验收口。