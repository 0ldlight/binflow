# T-218 review — FR-66 控制台角色与权限管理扩展 + read-only 只读态

- 评审人：code-reviewer ｜ 视角：correctness（wire 契约 + 只读态完备性 + 无绕过）
- 日期：2026-08-23 ｜ 结论：**APPROVE**（blocking 0 / non-blocking 4）

## 证据基线（全部本机实跑）

静态核对源：internal/auth/rbac.go（committed）、internal/httpapi/{router,security,session,permissions,middleware}.go、
internal/audit/api.go；前端 14 文件 diff + e2e/rbac.spec.ts 逐行。

```
cd web && npm run typecheck && npm run lint && npm run build   # 三绿（build 9.62s）
make console    # payload 158581 bytes（预算 5MB）
go test ./internal/console/    # ok
make build      # bin/binflow-server 重建（11:11，含新 console）
# scratch 实例 127.0.0.1:18219（BINFLOW_STORAGE__DATA_DIR 注意不是 SERVER__DATA_DIR）：
BASE=http://127.0.0.1:18219 playwright test e2e/rbac.spec.ts      # 3 passed (20.0s)
BASE=... playwright test --workers=2 e2e/security.spec.ts e2e/governance.spec.ts  # 10 passed (51.4s)
BASE=... playwright test --workers=2 e2e/repositories.spec.ts     # 4 passed (17.3s)
```

独立抽查（readonly_admin 会话，V13 五页之外）：Dashboard 只读横幅在位 + 读面存活；
Quotas 行内「编辑上限」可见但 disabled；同会话重放配额写（PUT /api/repositories/{key}
带 quotaBytes）→ **403 "administrator privileges required"**，读回 quotaBytes 未变；
GC 页 danger-zone 不渲染；**内容面**重放 `PUT /binflow/{repo}/probe-review.txt` 与
DELETE → 双 403（角色短路在内容面同样成立）。

## 分项核对

### 1. wire 契约 ✓
- `normalizeAdminRole`（web/src/lib/api.ts）：闭集 snake 三值，闭集外回退 admin 布尔
  镜像——与 `Principal.EffectiveRole`（rbac.go:67）同 fail-safe；服务端回显本身恒闭集
  （session.go:217/236、security.go:691 roleFromStored），回退只对旧二进制生效。方向
  正确：不放大权限。
- `canAdminWrite`（=admin）/`isReadOnlyAdmin`（=readonly_admin）/ user 两者皆非——与
  `CanManage`（admin 全过 / readonly 三读 / user 全拒）和 `CanManageRepo`（readonly
  !write）三值语义一一对应。
- `PermAction` 四值全词形 `manage`（permissions.go:185 接受并回显，未知动作 400 文案
  "read, write, delete, manage"）；`PERM_ACTIONS` r/w/d/m 序 = 服务端回显 append 序
  （V14 `['read','manage']` 逐字断言，实跑通过）；targetdiff ACTION_ORDER 同步扩。
- `UserUpdateBody.adminRole`：仅在角色变化时携带、**永不与 admin 布尔混发**
  （UserDetailPage submit `if (f.role !== baseRole) body.adminRole = f.role`）——结构性
  规避 security.go:934-948 的冲突 400。实测：PUT 建户带 adminRole+admin:false 一致对
  → 201（V13 备料 + 我的抽查）。

### 2. 只读态完备性 ✓（两处可见-但-403 的残留，见 non-blocking）
- AppShell：adminOnly 门放宽 `admin || readOnlyAdmin`，安全/治理组对 readonly 可见——
  与 11 个读端点门（CapSystemRead/CapSecurityRead/CapRepoRead，router.go:314-570）
  对齐；会话「只读」徽章在位。
- 九页退场清点：repos 列表（建仓隐藏+注记）/ 仓库表单（非 admin 空态 + readonly 专用
  文案）/ users 列表（建户隐藏+角色列徽章）/ 用户详情（email/角色/组员/口令/保存全
  disabled）/ groups（写入口隐藏+注记）/ perms 列表（建 target 隐藏+注记）/ perms
  编辑器（矩阵全控件 disabled、保存与危险区不渲染、/new 只读空态）/ quotas（编辑上限
  disabled）/ dashboard（readonly 横幅，读面全可见）。GC 危险区 `{admin && …}` 隐藏、
  replication 纯读、backup 静态引导页。
- **UI 无绕过**：V13 重放腿为真（同会话 page.evaluate fetch 重放建仓/存户/删组/写权限
  四写全 403 + message 断言）；我另独立重放管理面配额写与内容面 PUT/DELETE 亦 403。
  服务端是唯一守门，呈现层禁用只是 UX。

### 3. governance.ts 词表 ✓
`AUDIT_ACTIONS` 24 词条与 `internal/audit Actions()`（api.go:163-181）**逐位同序**：
`user.role.change` 落 group.member 之后、permission.create 之前。V13 断言
`option[value="user.role.change"]` 存在，实跑通过。

### 4. 越区报备 ✓
AppShell.tsx 在 web/src/components（area 根 web/src 内），改动仅导航门放宽 + 徽章
（+13/-2），为 AC2「管理页可见」所必需。报备成立。

### 5. clean-room ✓
无 reverse-src 对应嫌疑：仓库自有注释/测试 ID 风格，无反编译翻译痕迹。

## 必须修改（blocking）

无。

## 建议改进（non-blocking）

1. `web/src/pages/governance/MigrationPanel.tsx:242,323` — `canStart` 只看
   `okData && !running`，不含角色判定：S3 已配置的实例上 readonly_admin 会看到可点击的
   「启动迁移」，YES 二次确认后收到 403（服务端 CapSystemWrite 已挡，实测口径符合 PRD
   「或提交后如实呈现 403」）。建议 `canStart = … && canAdminWrite(session)`；同时修正
   T-218.md 中「迁移启动维持 admin 门」的说法——那只是服务端门，UI 门未维持。
2. `web/src/pages/repositories/tree/{TreePage,UploadDialog}.tsx` — 上传/删除按钮对
   readonly_admin 保留可点（内容面按 target 授权的设计使然），提交后 403。可用
   `isReadOnlyAdmin` 统一禁用以贴合只读叙事（内容面重放 403 已实测）。
3. `web/src/pages/security/api.ts` — `UserUpdateBody.adminRole?: string` 可收紧为
   `AdminRole`（UI 当前只发闭集值，收紧零成本防手滑）。
4. `web/src/pages/repositories/RepositoriesPage.tsx` 只读注记称「仓库配置…只读」，但
   readonly 点进仓库设置页是无权限空态而非只读呈现——文案与形态略错位，可与组页
   「禁用态按钮」口径合并另开小票。

## 环境备注（交 conductor）

- 评审期间工作树新落一批 Go 改动（internal/auth ldap/oidc 及测试、migrations SQL、
  npm session_gate_test 等，lint 清理性质：godoc 补注/未用参数 `_`/死 mock 删除）——
  按 conductor 指示属 T-220 在途债务票，未审未碰；扫描确认无功能变更。
- 本评审 `make console` 产物与 T-218 agent 产物逐字节一致（internal/console/dist 无
  新增 git diff）；`make build` 重建过 bin/binflow-server（11:11）。scratch 实例
  （:18219）与临时脚本已清理。
