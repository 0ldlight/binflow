# Sprint 377 迭代报告 — T-209 前提勘误 + 口径拍板重开工

**日期**: 2026-08-22
**上轮**: Sprint 376（三票在途等待）
**本轮焦点**: T-209 以 blocked 返回，暴露 ADR-0025 决策 5 的「`upload_sessions` 表 S3 已建」前提为事实错误；本轮核实、勘误三处回写、口径拍板为 (b)、T-209 重开工。

## 阶段 0 — 复位

- PRODUCT.md / ROADMAP.md 非空壳。
- 在途 agent：**T-208 / T-210** 仍在后台运行（未完成）；**T-209** 上轮以 blocked 返回，本轮处理。
- git：HEAD 含 sprint 376 报告。

## 本轮唯一实质产出：T-209 前提勘误

T-209 agent blocked 报告称「`upload_sessions` 表 S3 后端 M6 已建、复用其接口」前提不成立。conductor 独立复核，全部证实：

1. `grep -rin upload_sessions internal/ cmd/` → 仅 docs（architecture.md 指标名 / milestone-6.md PRD 规划行 127/129/565/762/776/792/847），**代码与迁移零命中**。
2. `internal/metadata/migrations/` 仅 `postgres` / `sqlite`，001-009 **无任何 `CREATE TABLE upload_sessions`**。
3. S3 会话走 minio-go multipart（`NewMultipartUpload`，upload ID 即 session ID，状态由 S3 服务端持有），`sessions map[string]*s3Session` 在内存（s3.go:37）。
4. 磁盘 `ResumeSession`（engine.go:190-193）与 S3 `ResumeSession` 均 hard 返回 `ErrSessionNotFound`——**重启续传是既定 M2 能力，两腿至今都未实现**。

结论：PRD 曾*规划* `upload_sessions` 表，但 S3 实现最终以 multipart 代替、**从未建表**；ADR-0025 决策 5 的「与 S3 同表/同语义 / S3 已建」前提为 fabrication。

## 勘误回写（三处）

- **DECISIONS.md ADR-0025 决策 5** 追加「前提修正（勘误）」段：仅本地 filestore 入**新建** `upload_sessions` 表（migration 010）；S3 维持 multipart（状态由 S3 服务端持久化，无需 DB 表）；「重启可续传」由「对齐 S3 既有行为」更正为「新增能力」。
- **BOARD.md T-209** 票面重写：范围收窄为「仅本地 filestore」，新增 migration 010 + `UploadSessions()` substore + 磁盘引擎落表/续传，`area` 扩为 `+ cmd/binflow-server`；AC1 改「重启可续传是新能力」；AC3 改「S3 回归绿（multipart 不变）」。
- **docs/prd/milestone-6.md Q2 单元格**：状态改「已定案（ADR-0025 决策 5，含前提修正）」，去掉「统一/对齐 S3」措辞，注明 S3 不入表、续传为新能力。

## 口径拍板 + T-209 重开工

拍板 agent 提出的 (a)/(b) 两口径为 **(b)**：仅本地 filestore 入表，S3 保持 multipart 不动。理由：S3 会话状态本已由 S3 服务端持久化，另建 DB 表是重复状态；「统一」本是为了追一个并不存在的 S3 DB 路径。同时授权 agent **最小化改动 cmd/ `openStorageEngine`**（三处 `storage.OpenEngine` 调用行 ~845/~861/~1062），注入点 = `Options` 加 `Sessions metadata.UploadSessionStore` + `openStorageEngine` 传 `md`。

已 `SendMessage` 恢复 blocked agent aef5c0f6c06cc27a8，附完整勘误 + 口径 + wiring 授权 + 验收要求（重启续传/S3 回归均须附真实执行证据）。

## 阶段 1/2/3 — 说明

- 阶段 1：todo 空，无新里程碑。
- 阶段 2：T-209 由 blocked 转回 doing（无 code 改动故无 review/qa）。
- 阶段 3：无新票可派（T-208/T-210 在途，T-209 重开工，并行度 3）。

## 阶段 4 — 落盘

- ✅ DECISIONS.md / BOARD.md / milestone-6.md 三处勘误回写 + 本报告。待 commit（随本轮战报）。

## 阶段 5 — 战报

见下方用户汇报。

## 阻塞与风险

- **T-208 / T-210 仍在途**，勿扰动；T-209 重开工。
- cmd/ wiring 是本轮勘误引入的唯一跨边界点，已授权 agent 最小改动并给出明确注入方案，风险可控。
- DoD #5（`m6-done` tag + 补 `m5-done`）仍待三票 done + DoD 全绿后定；`git push` 外发仍须用户单独授权。

## 下轮计划

- 待 T-208/T-209/T-210 完成通知 → 逐票 review（T-209 关键模块可双 review）→ qa → conventional commit。
- 三票 done 后重跑 DoD 五条核验，再就 tag 与 push 请示用户。