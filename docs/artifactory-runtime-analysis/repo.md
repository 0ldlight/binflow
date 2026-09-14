# 运行时分析 · 仓库管理面（证据指针文档——正文在既有产物）

> 截图基线 ：8082（7.161.20）。路径前缀 `pc/` = `docs/reverse/frontend/parity-capture/`。

## 1. 截图

| 资产 | 内容 |
|---|---|
| `pc/screenshots/screens/repos-local.png` / `repos-remote.png` / `repos-virtual.png` | 仓库列表三 Tab（AG Grid + 计数 + Replications 列） |
| `repo-form-new-local.png` / `repo-form-edit-probe.png` | 建仓/编辑表单（jf-steps 三步：Basic/Advanced/Replications） |
| `pc/screenshots/dialogs/wizard-audit-probe-empty-3-name.png` / `wizard-audit-probe-empty-4-done.png` | 建仓向导（空仓探针 probe-empty 第 3/4 步） |
| `pc/screenshots/dialogs/delete-confirm-dialog.png` / `delete-confirm-row-menu.png` + `states/delete-confirm-row-not-found.png` | 行删除确认（kebab 菜单 + 危险确认弹窗） |
| `layouts.png` | Repository Layouts 管理页（26 默认 layout 注册面） |
| `property-sets.png` | Property Sets 管理页 |
| `ie-repositories.png` | import/export 侧仓库视图 |
| `federation.png` | Federation（企业面，静态页） |

## 2. 行为规格（正文）

- `docs/reverse/repo-semantics.md`——local/remote/virtual 语义、layout 解析、缓存规则。
- `docs/reverse/repo-operations.md`——仓库 CRUD/仓库级操作端点族。
- `docs/reverse/rest-api.md` + `docs/compatibility/matrix.yaml` D02（仓库配置 13 行）——configJSON round-trip、url 前缀、blackedOut 拒写联动。
- `docs/reverse/inv-3-protocols.md`——25 条内置 layout + 57 包型逐项。
- `docs/reverse/frontend/screens.yaml`——repositories-list/repository-form 屏四态（必填 blur 内联错误 "You must fill in this field"）。
- remote 表单 Test 连通性 / 远端浏览档——parity 锚册 B-3.6/B-3.20 as-built 注（7.161.20 实测同位证据）。

## 3. 已知运行时事实

- 建仓 modal 924px 居中（m16-baseline-refresh §A3-7 勘误——推翻 880px 旧锚）。
- 「＋ 新建仓库」下拉三预选（s3e-create-dropdown 证据）。
- 远端浏览官方开放面五型（deb/generic/maven/Opkg/rpm）——`docs/reverse/remote-browsing.md` §2。

## 4. 缺口声明

Federation 交互流（门内深层）无走查（license/环境前置不可达）；Property Sets 仅截图 + K68 候裁登记（无 wire 规格）。
