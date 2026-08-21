# Sprint 278 迭代报告 — Batch 2 派发（T-150 ∥ T-153）

**日期**: 2026-08-21
**上轮**: sprint 277（M6 Batch 1 完成，Batch 2 已计划）
**本轮焦点**: 阶段 3 派发 → 两个后台 agent 在途

## 阶段 0 — 复位

- BOARD.md 看板：todo 含 M6 24 张 ticket（Batch 2~8），doing 含 T-150/T-153，done 含 M1~M5 + M6 T-148/T-149
- 两个后台 agent 在途：T-150（af369e004afa513f7，dev-go-storage）、T-153（a6f5709743e3b6da4，dev-go-core）
- 两个 area 不重叠（`internal/storage` vs `internal/auth`），可并行派发

## 阶段 1 — 补给看板（Planning）

⏭️ 跳过（todo 非空，Batch 2~8 完整）。

## 阶段 2 — 收口（Close-out）

⏭️ 跳过（无 agent 待收口）。

## 阶段 3 — 派发（Dispatch）

### 本轮派发

| Ticket | 角色 | 类型 | dep | 状态 |
|--------|------|------|-----|------|
| T-150 | dev-go-storage | 存储引擎基座 | T-149 ✅ | 后台 agent 在途 |
| T-153 | dev-go-core | 认证基座 | T-149 ✅ | 后台 agent 在途 |

两个 area 不重叠（`internal/storage` vs `internal/auth`），并行派发。

### T-150 背景

ADR-0019 设计：
- 新增 `Backend` 接口（Put/Get/Delete/Exists/List），包内可见（非导出）
- `Engine.Open` 返回类型从 `io.ReadSeekCloser` 变更为 `io.ReadCloser`
- DiskEngine 直接实现 Engine，不经过 Backend
- S3Engine（T-151）将通过 Backend 桥接

### T-153 背景

ADR-0020 设计：
- 新增 `IdentityProvider` 接口（ProviderName/Authenticate/Resolve）
- Authenticator 新增第 4 个 OIDC Bearer 臂
- 认证臂顺序：Basic → Bearer(API token) → OIDC Bearer(新) → Cookie → 匿名
- OIDC 用户首次登录时自动创建 users 行（source=oidc, password_hash 空）
- LDAP 仅参与 login 端点，不参与 per-request 认证

## 阶段 4 — 落盘

- ✅ BOARD.md 保持：T-150/T-153 在 doing 区
- ✅ 写迭代报告：reports/iteration-278.md
- ⏳ 等待两个 agent 完成后收口

## 阶段 5 — 战报

### 📊 Sprint 278 总览

| 指标 | 数值 |
|------|------|
| 回合 | Sprint 278（M6 Batch 2） |
| 派发 | 2 张（T-150 ∥ T-153） |
| 在途 | 2 个后台 agent |
| 剩余 | 24 张（todo 中） |

### 🚀 下一轮计划

等待 T-150/T-153 完成后：
- T-150 完成后解锁 T-151（S3Engine）+ T-152（S3 配置与健康检查）
- T-153 完成后解锁 T-154（OIDC Provider）+ T-155（LDAP Provider）
- 四个票可组成 Batch 3（T-151/T-152/T-154/T-155，area 两两不重叠）

### ⚠️ 开放问题

9 项开放问题（Q1~Q9）仍待用户定案，当前按暂行假设推进。