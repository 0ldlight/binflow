# Sprint 381 迭代报告 — T-208 复审 APPROVE；qa 被 T-209 中途重构阻断

**日期**: 2026-08-22
**上轮**: Sprint 380（T-208/T-209 在途，轻量确认）
**本轮焦点**: 完成 T-208（用户禁用 REST seam）独立复审 → APPROVE；确认 T-209 仍在途并阻塞 T-208 qa（传递依赖编译不过）；不提交、不派发。

## 阶段 0 — 复位

- PRODUCT.md / ROADMAP.md 非空壳。
- 在途 agent：T-208 已返回 done（工作树落盘）；T-209 仍后台运行（transcript 存活，最近工具活动 = 本地 23:38，秒级前，正读 `internal/metadata/api.go` 并推进 migration 010）。
- git HEAD=`cdd28a2`（sprint 380 报告）。
- 工作树未提交改动：T-208（httpapi/security*.go、adapter/docker/token_test.go、metadata api.go SetEnabled 段、substores_auth.go SetEnabled 段、t97_group_refs_test.go）与 T-209（storage/*、metadata 010 迁移 + substores_upload.go + UploadSession 段）**共享 `api.go`/`substores_auth.go`/`store.go`**，两票改动交织，不可切分提交。

## T-208 复审结论 — APPROVE（五 AC 全核）

独立复核（非 agent 自报）逐项：

1. **AC1 禁用即 401**：`internal/httpapi/security.go` 三处用户写路由（`PUT` create / `POST` create / `POST` partial-update）均在 `internal/httpapi/router.go:515-528` 注册为 `routeAuth{required:true, admin:true}`——admin gate 在 handler 之前，`SetEnabled` 只可能被 admin 触达。create 路径 `enabled := body.Enabled == nil || *body.Enabled`；replace 路径与 `handleUserUpdatePost` 均在 `body.Enabled != nil` 时调 `Users().SetEnabled`。语义正确。
2. **AC2 缺省不变**：`Enabled *bool` 指针区分「缺省」vs「显式 false」；create 缺省 true、replace/update 缺省保留存储值，未提供时完全不碰 enabled 列。`TestUserEnabledSeam` 的 create-default-true 与 replace-absent 两个子用例钉死该语义。
3. **AC3 非 admin 403**：路由 gate 复用既有 admin gate（router.go 三处 `admin:true`），`TestUserEnabledSeam`「non-admin cannot disable anyone」子用例断言 403 且受害者仍可登录。
4. **AC4 session 臂 401**：复核 auth 层确认该行为**已存在且经得起核**——`token.go:51`（TokenVerifier 重查用户行，`!u.Enabled → "token owner disabled"`）、`session.go:90`（Session Verify 重解析 owner，`!u.Enabled` 拒）、`session.go:128`（IssueSession 拒禁用主体）、`session.go:255`（禁用登录失败报 `ReasonUserDisabled` 而非通用口令错）。即「禁用立即失效既有 Token 与会话」是既有护栏，T-208 只是把 REST 写缝接上，`SetEnabled` 文档注释准确非空话。
5. **AC5 全仓 -race 绿**：**暂无法验证**——后台 `-race` 探针（task b5zv3qdhl）以 exit 0 结束但结果为 `internal/auth ok / internal/httpapi build failed`，失败点 `internal/storage/session.go:215 undefined: sessionDataFile`。

## 关键发现：qa 阻断根因 = T-209 中途重构，非 T-208 缺陷

- `session.go:215` 引用的 `sessionDataFile`（对应 `dataFileName`）是 T-209 正在进行的「磁盘 session → upload_sessions 表」重命名中途态：`internal/storage/{api,doc,engine,session}.go` 均修改，`substores_upload.go` + 010 两个迁移文件为新增，符号尚未闭环 → httpapi 传递依赖编译失败。
- 这是 agent 在途正常扰动，非损坏。T-209 落地后 `go build ./...` 应复原。
- **因此 T-208 qa 与提交一并后移**：不可在 `internal/storage` 不可编译时跑 qa，也不可在两票共享 `api.go`/`substores_auth.go`/`store.go` 时切分提交（否则会把 T-209 半成品扫进 T-208 commit）。

## 阶段 1/2/3 — 均无动作

- 阶段 1（补给）：todo 空；ADR-0025 派生票已全派出。
- 阶段 2（收尾）：T-208 复审完成（APPROVE），待 qa；T-209 无完成通知。
- 阶段 3（派发）：todo 空，并行度 1/4，无新票可派。

## 阶段 4 — 落盘

- ✅ BOARD.md：T-208 doing→review（附「APPROVE，qa 待 T-209 落地解锁 build」）；状态行更新为「T-210 done / T-208 review / T-209 在途，并记 qa 阻断根因」。
- ✅ 本报告。**不提交**——工作树两票交织 + storage 不可编译。

## 阻塞与风险

- T-209（P1，新建 010 表 + substore + 磁盘引擎重写 + cmd wiring）改动面最大，仍在途。其未落地前 T-208 无法 qa/提交。
- 待 T-209 完成通知 → 先验 `go build ./...` 复原 → T-208 qa（跑 `TestUserEnabledSeam` + 全仓 `-race`）→ 分票提交（T-208 与 T-209 按文件归属切分）；随后 T-209 review/qa/commit。
- 全 done 后 DoD 五条核验收口，`m6-done`/`m5-done` tag 与 push 仍须用户单独授权。

## 下轮计划

- 收到 T-209 完成通知 → 立即 `go build ./...` 验复原 → T-208 qa + commit。
- T-209 若持续在途 → 继续轻量确认存活，不干预。
