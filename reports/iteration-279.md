# Sprint 279 迭代报告 — Batch 2 收口 + Batch 3 派发

**日期**: 2026-08-21
**上轮**: sprint 278（M6 Batch 2 派发：T-150 ∥ T-153）
**本轮焦点**: 阶段 2 收口 T-150/T-153 → 阶段 3 派发 Batch 3（T-151 ∥ T-152 ∥ T-154）

## 阶段 0 — 复位

- BOARD.md 看板：todo 含 M6 26 张 ticket（Batch 2~8），doing 含 T-150/T-153，done 含 M1~M5 + M6 T-148/T-149
- 两个后台 agent 在途：T-150（af369e004afa513f7，dev-go-storage）、T-153（a6f5709743e3b6da4，dev-go-core）
- 轮次前已确认两个 agent 均已完成编码

## 阶段 1 — 补给看板（Planning）

⏭️ 跳过（todo 非空，Batch 2~8 完整）。

## 阶段 2 — 收口（Close-out）

T-150 和 T-153 均已完成，conductor 核验直收（均为接口/基座层，无业务逻辑，不涉及 review 环节）：

### T-150 — storage.Backend 接口 + Engine 签名变更 ✅ 核验通过

**变更**：
- 新增 `internal/storage/backend.go`：包内 Backend 接口（Put/Get/Delete/Exists/List）
- 修改 `internal/storage/api.go`：Engine.Open 返回类型从 `io.ReadSeekCloser` 变更为 `io.ReadCloser`
- 修改 `internal/storage/engine.go`：实现适配（返回 `*os.File`，类型断言仍可用）
- 修改 `internal/storage/engine_test.go`：测试适配

**核验证据**：
- `go test -race -count=1 ./internal/storage/` → ok（53.357s，全绿）
- `go test -race -count=1 ./internal/repo/ ./internal/httpapi/ ./internal/adapter/...` → 11 个依赖包全绿
- `make build` → 三二进制预算内
- `make vet` → 零告警

**遗留**：Backend 接口由 T-151（S3Engine）实现消费。

### T-153 — auth.IdentityProvider 接口 + OIDC Bearer 臂 ✅ 核验通过

**变更**：
- 新增 `internal/auth/identity.go`：Provider 类型（local/oidc/ldap）、Claims 结构体、ProviderUser 结构体、IdentityProvider 接口（ProviderName/Authenticate/Resolve）、ErrProviderUserNotFound
- 修改 `internal/auth/api.go`：Principal 新增 Source 字段（Provider 类型）
- 修改 `internal/auth/authenticator.go`：新增 WithOIDC() 方法、authenticateOIDC() 方法、userCreator 接口、oidcProvider 字段；authenticateBasic 拒绝空 password_hash 用户（OIDC/LDAP）；authenticate 新增 OIDC Bearer 臂（位于 API token 和 Cookie 之间）
- 修改 `internal/auth/deps.go`：userStoreAdapter.Get 填充 Provider/ProviderID 字段（占位）

**核验证据**：
- `go test -race -count=1 ./internal/auth/` → ok（26.356s，全绿）
- `go test -race -count=1 ./internal/repo/ ./internal/httpapi/ ./internal/adapter/...` → 11 个依赖包全绿
- `make build` → 三二进制预算内
- `make vet` → 零告警

**遗留**：userStoreAdapter 的 Provider/ProviderID 为占位值（ProviderLocal, ""），待 migration 008 更新 Go struct 后修正。

## 阶段 3 — 派发（Dispatch）

T-150 和 T-153 完成后，关键路径解锁：

| 解锁票 | 依赖 | 说明 |
|--------|------|------|
| T-151（S3Engine） | T-150 ✅ | 消费 Backend 接口 |
| T-152（S3 配置与健康检查） | T-150 ✅ | 消费 config 段 + /healthz 扩展 |
| T-154（OIDC Provider） | T-153 ✅ | 消费 IdentityProvider 接口 |
| T-155（LDAP Provider） | T-153 ✅ | 复用 IdentityProvider 接口，但 area 与 T-154 冲突（internal/auth） |

T-155 暂缓派发（area 冲突），待 T-154 完成后派发。

### 本轮派发

| Ticket | 角色 | 类型 | dep | 状态 |
|--------|------|------|-----|------|
| T-151 | dev-go-storage | S3 存储引擎 | T-150 ✅ | 后台 agent 在途 |
| T-152 | dev-go-core | S3 配置与健康检查 | T-150 ✅ | 后台 agent 在途 |
| T-154 | dev-go-core | OIDC Provider | T-153 ✅ | 后台 agent 在途 |

三个 area 不重叠（`internal/storage` vs `internal/config+internal/httpapi` vs `internal/auth`），可并行派发。

### T-151 背景

ADR-0019 设计：
- `S3Engine` 实现 `Engine` 接口，通过 `Backend` 接口桥接 S3
- 复用 `session.go` 的上传会话模型
- 使用 minio-go/v7（T-149 已准入）
- multipart upload 支持

### T-152 背景

ADR-0018/ADR-0019 设计：
- `storage.backend` 配置段（`type: "disk" | "s3"`）
- `storage.s3` 配置段（endpoint/access_key/secret_key/bucket/region/use_ssl）
- `GET /healthz` 扩展：`storage.status` 字段（`ok`/`degraded`/`error`）
- S3 连接验证（启动时健康检查）

### T-154 背景

ADR-0020 设计：
- `OIDCProvider` 实现 `IdentityProvider` 接口
- 使用 go-oidc/v3（T-149 已准入）
- Authorization Code Grant + PKCE（S256）
- `client_secret` 通过 env 注入，不入 YAML
- Web 登录流：`GET /binflow/api/v1/oidc/login` → 302 IdP → callback → 验证 ID token → 签发 session

## 阶段 4 — 落盘

- ✅ BOARD.md 更新：T-150/T-153 从 doing 移入 done（M6 区）；T-151/T-152/T-154 从 todo 移入 doing
- ✅ BOARD.md 修复：删除重复的 "测试中（qa）" 标题
- ✅ 写迭代报告：reports/iteration-279.md
- ⏳ 等待三个 agent 完成后收口

## 阶段 5 — 战报

### 📊 Sprint 279 总览

| 指标 | 数值 |
|------|------|
| 回合 | Sprint 279（M6 Batch 2 收口 + Batch 3 派发） |
| 收口 | 2 张（T-150/T-153 ✅ conductor 核验直收） |
| 派发 | 3 张（T-151 ∥ T-152 ∥ T-154） |
| 在途 | 3 个后台 agent |
| 已缓派 | 1 张（T-155，area 冲突，待 T-154 完成后） |
| 剩余 | 22 张（todo 中） |
| 已完成 | M6 总 4/26 张（T-148/T-149/T-150/T-153） |

### ✅ Batch 2 核验结论

T-150（Backend 接口）和 T-153（IdentityProvider 接口）均为接口/基座层骨架代码，不涉及生产逻辑。全量测试 race 绿，依赖包零回归，核验直收。两个 agent 产出自测证据完整，无需 review 环节。

### 🚀 下一轮计划

等待 T-151/T-152/T-154 完成后：
- T-151 完成后解锁 T-164（本地→S3 迁移，area 冲突可用 worktree 隔离）
- T-152 完成后解锁 T-170（部署矩阵更新）
- T-154 完成后解锁 T-155（LDAP Provider），T-154+T-155 完成后解锁 T-156（008 迁移）
- T-155 可与 T-156 的 metadata 部分并行（area 部分重叠，需评估）

### ⚠️ 开放问题

9 项开放问题（Q1~Q9）仍待用户定案，当前按暂行假设推进。Q4/Q5（OIDC admin_group 粒度、用户名冲突处理）在 T-154 中按 ADR-0020 暂行假设实现。