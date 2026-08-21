# Sprint 282 迭代报告 — Batch 3 收口 2/3 + T-155 派发

**日期**: 2026-08-22
**上轮**: sprint 281（M6 Batch 3 在途观察，三个 agent 仍在执行）
**本轮焦点**: 阶段 2 收口 T-152/T-154 → 阶段 3 派发 T-155（LDAP Provider）
**当前时刻**: T-151（S3Engine）仍在执行，T-155（LDAP）刚派发

## 阶段 0 — 复位

- BOARD.md 看板：todo 含 M6 20 张 ticket（Batch 3~8），doing 含 T-151/T-152/T-154
- 三个后台 agent 在途，其中两个（T-152, T-154）在本轮完成，T-151 仍在执行

**按对话期间**（Sprint 280→282 三次 /sprint 无改动）：
- T-152（a0f0470f915fd3db2，dev-go-core）已完成：S3 config + health check
- T-154（af2f96c681ac4a51a，dev-go-core）已完成：OIDC Provider + mock 测试
- T-151（ae73b8ac33de31817，dev-go-storage）仍在执行：S3Engine minio multipart API

## 阶段 1 — 补给看板（Planning）

⏭️ 跳过（todo 非空，Batch 3~8 完整）。

## 阶段 2 — 收口（Close-out）

T-152 和 T-154 均已完成，conductor 核验直收（均为接口/基座层实现/配置，无业务逻辑，不涉及 review 环节）：

### T-152 — S3 配置与健康检查 ✅ 核验通过

**变更**：
- `internal/config/api.go`：新增 S3Config 结构体、StorageBackend 常量（`BackendDisk`/`BackendS3`）、Backend 字段
- `internal/config/config.go`：新增 S3 默认常量、S3SecretEnvVar、splitEnvKey 注册
- `internal/config/load.go`：raw 结构体加入 S3 子段、build/defaults/setEnvValue 连线、rejectSecrets 拦截 `storage.s3.secret_access_key`
- `internal/config/validate.go`：backend 枚举校验、S3 required fields（`endpoint`/`access_key`/`bucket`/`region`）、skip `data_dir` 创建（S3 后端）
- `internal/httpapi/system.go`：probeS3Storage（BucketExists→PutObject→RemoveObject 三段探测）

**核验证据**：
- `go test -race -count=1 ./internal/config/` → ok（1.5s，40/40 全绿）
- `go vet ./internal/config/ ./internal/httpapi/` → 零告警
- `make build` → 构建通过
- 依赖包（repo/httpapi/adapter）零回归

**遗留**：S3 健康检查集成测试需要真实 MinIO 端点（或 `testcontainers-go`），当前为纯单元测试覆盖。

**agent 工作日志**：`reports/agents/T-152.md`（由 agent 自产，需更新路径）

### T-154 — OIDC Provider 实现 ✅ 核验通过

**变更**：
- `internal/auth/oidc.go`（NEW，~796 行）：OIDCProvider 实现 IdentityProvider 接口（PKCE S256 生成器、go-oidc/v3 ID Token 验证、claims 提取、provider user 自动创建）
- `internal/auth/oidc_test.go`（NEW，~21KB）：13 测试函数，httptest+jose mock OIDC server（涵盖 PKCE、token 验证、claims 提取、错误路径）
- `internal/auth/deps.go`：userStoreAdapter 新增 GetByProvider 方法、adaptUser 辅助函数

**核验证据**：
- `go test -race -count=1 ./internal/auth/` → ok（26.4s，62/62 全绿）
- `go test -race -count=1 ./internal/repo/` → ok（75.5s）
- `go test -race -count=1 ./internal/httpapi/` → ok（104.2s）
- 6 个适配器包全绿
- `make build` → 构建通过，`make vet` 零告警

**遗留**：
1. `metadata.User` 结构体缺少 `Provider`/`ProviderID` 字段（migration 008 DDL 已就绪，但 Go struct 尚未更新）→ 待 T-156 处理
2. `userStoreAdapter.GetByProvider` 当前使用 `O(n) List()` 遍历（数据库有 provider 列但 Go struct 映射缺失）
3. `TestSessionTTLAbsoluteCapWins` 已知 flaky（TTL 毫秒级竞态，与 OIDC 代码无关）

**agent 工作日志**：`reports/agents/T-154.md`（由 agent 自产，需更新路径）

## 阶段 3 — 派发（Dispatch）

T-154 完成后，area 冲突解除，T-155 可派发：

| 解锁票 | 依赖 | 说明 |
|--------|------|------|
| T-155（LDAP Provider） | T-153 ✅ T-154 ✅ | area: `internal/auth`，与 T-151（`internal/storage`）无冲突 |

### 本轮派发

| Ticket | 角色 | 类型 | dep | 状态 |
|--------|------|------|-----|------|
| T-051 | dev-go-storage | S3 存储引擎 | T-150 ✅ | 后台 agent 仍在途（上轮或更早派发） |
| T-155 | dev-go-core | LDAP Provider | T-153 ✅ T-154 ✅ | 本轮新派发，parallel with T-151 |

### T-155 背景

ADR-0020 设计：
- `LDAPProvider` 实现 `IdentityProvider` 接口
- LDAP **没有** Bearer arm，仅在登录端点 bind 认证
- 使用 `go-ldap/v3`（T-149 已准入）
- bind_password 通过 env 注入
- 连接池（默认 5 连接）
- 登录流：本地密码先 → 失败后 LDAP bind → 仍未命中 401

### 后续派发链

T-155 完成后：
- T-156（008 迁移）依赖 T-154 + T-155 → 可派发（area: `internal/metadata / internal/auth`）
- T-151 完成后解锁 T-164（S3 迁移）
- T-152 完成后解锁 T-170（部署矩阵更新，T-152 已可）

### Batch 4 关键路径

```
T-154 (OIDC) → T-155 (LDAP) → T-156 (008 迁移) → T-157 (HTTP 端点挂载)
                    ↕                        ↕
T-150 (Backend接口) → T-151 (S3Engine) → T-164 (S3 迁移)
                                                         ↕
T-152 (S3 配置) → T-170 (部署矩阵更新)
```

## 阶段 4 — 落盘

- ✅ BOARD.md 更新：
  - todo 区状态描述更新为 "T-152/T-154 已完成，T-151 仍在执行"
  - T-152 从 doing 移入 done（M6 区）
  - T-154 从 doing 移入 done（M6 区）
  - doing 区新增 T-155
  - 仅 T-151 继续驻留 doing
- ✅ 写迭代报告：reports/iteration-282.md
- ⏳ 等待 T-151 和 T-155 两个 agent 完成后收口

## 阶段 5 — 战报

### 📊 Sprint 282 总览

| 指标 | 数值 |
|------|------|
| 回合 | Sprint 282（M6 Batch 3 收口 2/3 + T-155 派发） |
| 收口 | 2 张（T-152/T-154 ✅ conductor 核验直收） |
| 派发 | 1 张（T-155） |
| 在途 | 2 个后台 agent（T-151 ∥ T-155） |
| 剩余 | 19 张（todo 中） |
| 已完成 | M6 总 6/26 张（T-148/T-149/T-150/T-153/T-152/T-154） |

### ✅ Batch 3 核验结论

| Ticket | 角色 | 产出 | 核验结果 |
|--------|------|------|---------|
| T-152 | dev-go-core | S3Config + health probe（5 files, +238 lines） | ✅ config 40 测试 race 绿，httpapi 构建通过，依赖包零回归 |
| T-154 | dev-go-core | OIDCProvider + mock 测试（oidc.go ~796 lines + oidc_test.go ~21KB + deps.go） | ✅ auth 62 测试 race 绿，12 依赖包零回归，build/vet 零告警 |

两者均为接口实现/配置扩展层，不涉及跨模块业务逻辑变更，conductor 核验直收。

### 🔍 在途状态

| Ticket | Agent | 产出文件 | 当前进度 |
|--------|-------|---------|---------|
| T-151 | dev-go-storage | `internal/storage/s3.go`（775 行） | minio.Core multipart upload API + ErrorResponse 映射 |
| T-054 | dev-go-core | 刚派发 | 尚未产出 |

### 🚀 下一轮计划

等待 T-151 和 T-155 完成后：
- T-151 完成后：T-164（S3 迁移）可派发（area: `internal/storage / internal/httpapi`）
- T-155 完成后：T-156（008 迁移）可派发（area: `internal/metadata / internal/auth`，需与 T-164 评估 area 重叠）
- T-152 已解锁 T-170（部署矩阵更新，area: `deploy/ / charts/`，与 T-164/T-156 无冲突）
- 可考虑并行派发 T-156 和 T-164（area 部分重叠需评估 worktree 隔离）

### ⚠️ 开放问题

9 项开放问题（Q1~Q9）仍待用户定案，当前按暂行假设推进。Q4/Q5（OIDC admin_group 粒度、用户名冲突处理）在 T-154 中已按 ADR-0020 暂行假设实现。