# Sprint 378 迭代报告 — 三票在途 + 看板卫生（去重头/状态行刷新）

**日期**: 2026-08-22
**上轮**: Sprint 377（T-209 前提勘误 + 口径拍板重开工）
**本轮焦点**: 三票仍在途、无完成通知 → 轻量复位 + 看板卫生清理，不制造性动作、不派发。

## 阶段 0 — 复位

- PRODUCT.md / ROADMAP.md 非空壳。
- 在途 agent：**T-208 / T-209 / T-210 三票后台运行中**（输出文件 mtime 22:59 / 23:00 / 23:15，均存活）。无完成通知 → 未完成。
- 源码工作树仍在变：`internal/config/*`、`internal/replication/*`（T-210）、`internal/httpapi/security*.go`、`internal/metadata/substores_auth.go`、`internal/adapter/docker/token_test.go`（T-208）。均为 agent 在途正常改动，未提交。
- git HEAD=`ae81c64`（上轮 ADR-0025 勘误 commit）。

## 本轮唯一动作：看板卫生（BOARD.md）

1. **去除重复的 `## 📥 待办（todo）` 头**：上轮两次 Edit 残留了两个 todo 区头（status 行下 + 空 todo 注记前），本轮合并为一个。
2. **状态行刷新**：去掉「Q2→会话统一 DB（对齐 S3）」的过时措辞，改「Q2→本地 filestore 会话入新建 `upload_sessions` 表，修订 ADR-0006」；补记「ADR-0025 决策 5 已随勘误 `ae81c64` 收窄为仅本地 filestore、S3 保持 multipart」；T-210（复制私网开关）补入派生票清单；DoD #5 tag 条件明确为「待三票 done + DoD 全绿后打」。

## 阶段 1/2/3 — 均无动作

- 阶段 1（补给）：todo 空；三张后续票已由 ADR-0025 生成并入 doing；无新里程碑 → 跳过。
- 阶段 2（收尾）：无完成通知；三票仍 doing，不做 review/qa。SPRINT-LOOP 规则：后台 agent 仍在跑 → 本轮不干预。
- 阶段 3（派发）：todo 空，且已在途 3 票（并行度满），无新票可派。

## 阶段 4 — 落盘

- ✅ BOARD.md 卫生（去重头 + 状态行刷新）+ 本报告。随后 commit。

## 阶段 5 — 战报

见下方用户汇报。

## 阻塞与风险

- 无阻塞。三票正常在途。T-209（P1，新建 `upload_sessions` 表 + upsetake 子store + 磁盘引擎重写 + cmd/ wiring）改动面最大，预计仍最长。
- 待三票 done 后：逐票 review（T-208/T-209 关键模块可双 review）→ qa → conventional commit；再重跑 DoD 五条核验，就 `m6-done`/`m5-done` tag 与 push 请示用户。

## 下轮计划

- 收到 T-208/T-209/T-210 任一完成通知 → 立即进入 review/qa/commit。
- 若三票持续在途 → 继续轻量确认，不制造性动作。