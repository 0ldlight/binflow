# 运行时分析 · 安全管理面（证据指针文档——正文在既有产物）

> 截图基线 ：8082（7.161.20）。路径前缀 `pc/` = `docs/reverse/frontend/parity-capture/`。

## 1. 截图

| 资产 | 内容 |
|---|---|
| `pc/screenshots/screens/users.png` + `states/loading-users-list.png` / `api-failure-users.png` | 用户列表（列集/Total Users 计数/loading/api 失败态——失败经客户端连接中断） |
| `user-new.png` / `user-edit-probe.png` | 用户创建/编辑表单 |
| `pc/screenshots/dialogs/user-form-filled.png` + `states/user-form-validation-error.png` | 用户表单空提交内联错误 + 填充态 |
| `groups.png` / `group-new.png` / `group-edit-probe.png` | 组三页 |
| `pc/screenshots/dialogs/group-form-filled.png` + `states/group-form-validation-error.png` | 组表单两态 |
| `permissions.png` | 权限目标列表 |
| `permission-new.png` / `permission-new-blank.png` | 权限新建（含空白态） |
| `pc/screenshots/dialogs/permission-picker-step.png` / `permission-matrix-editor.png` + `states/permission-save-result.png` | 两步 user/group 选择器 + 矩阵编辑器 + 保存结果 |
| `states/permission-denied-admin-route.png` + `pc/dom-snapshots/permission-denied-summary.json` | 非 admin 访问管理路由的拒绝/重定向态 |
| `keys-management.png` | Keys Management（GPG keypair） |
| `access-tokens.png` + `dialogs/token-generate-form.png` | Access Tokens（归 auth.md 详列） |

## 2. 行为规格（正文）

- `docs/reverse/rbac-model.md`——两层授权模型、角色闭集、组 CRUD 与 effective admin。
- `docs/reverse/gap-endpoints.md`——users 回显字段级（无 enabled 布尔，status 枚举）、DELETE 级联守卫、组成员暴露面（includeUsers/UI 扇出/Access v2 members）、permission 列表无过滤面。
- `docs/compatibility/matrix.yaml` D04（安全 35 行：✅19/◐3/❌12/超集1）。
- `docs/reverse/frontend/screens.yaml`——users-list/user-form/groups-list 等屏四态判据（穿梭空侧 "No Items Selected"）。
- 权限动词五列（Read/Annotate/Deploy-Cache/Delete-Overwrite/Manage）——t455-probe 活体实据 + parity B-1.6。
- 7.161 权限新建/编辑为三步向导路由页（Resources/Users/Groups panel-*）——t455-probe。

## 3. 已知运行时事实

- 用户/组创建 = 整页路由表单（非 modal）——T-381 核验（7.84）；7.161 管理位 = Administer Platform + Manage Resources 双布尔——T-453 对照登记。

## 4. 缺口声明

Build-info 权限面板（Build Info Repositories 资源型）无专项截图；Projects 域授权 UI 无证据（企业面，见 admin.md 缺口）。
