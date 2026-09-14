# 运行时分析 · 控制台壳与全局面（证据指针文档——正文在既有产物）

> 截图基线 ：8082（7.161.20）。路径前缀 `pc/` = `docs/reverse/frontend/parity-capture/`。

## 1. 截图与 DOM

| 资产 | 内容 |
|---|---|
| `pc/screenshots/screens/packages.png` | Packages 首页（卡片流） |
| `onboarding.png` / `quick-setup.png` | 首启向导 / 快速设置 |
| `general-config.png` / `af-general-settings.png` / `config-descriptor.png` | General Config 三视图（平台/Artifactory/描述符） |
| `notfound-404.png` + `pc/screenshots/states/route-404.png` | 404 页 |
| `pc/screenshots/dialogs/user-menu-dropdown.png` | 顶栏用户菜单（Platform/Administration/Quick Repository Creation/Set Me Up/Edit Profile/Logout） |
| `pc/screenshots/dialogs/quick-search-overlay.png` / `quick-search-focused.png` + `states/search-results-after-enter.png` | 顶栏快搜 overlay（输入 + 结果） |
| `pc/screenshots/states/hover-sidebar-item.png` / `hover-primary-button.png` | 悬停态 |
| `pc/screenshots/screens/user-profile.png` | Profile 页 |
| `pc/dom-snapshots/sidebar.html` / `topbar.html` + `pc/nav-tree.json` | 导航树实测（侧栏分组 + 顶栏） |
| `pc/tokens.json` | 设计 token 六面（palette/typography/radii/shadows/spacing/raw） |
| `pc/screenshots/screens/migration-tool.png` | 迁移工具页（壳层管理页样例） |

## 2. 行为规格（正文）

- `docs/reverse/console-ui.md`——全局 IA（双导航模式/管理树/顶栏/用户菜单）、页面骨架、13 交互流、组件与状态矩阵、OSS 缺位清单（7.84.10 走查——版本偏斜注意）。
- `docs/reverse/frontend/README.md` §1——前端架构事实五条（MFE 拉起/双 API 面/SSR 免认证路由族）。
- `docs/reverse/frontend/routes.yaml`——115 路径（artifactory MFE 89 + access MFE 26，E2 机读证据）。
- `docs/reverse/frontend/screens.yaml`——packages/onboarding/user-profile 屏四态判据。
- 管理态顶栏 Search Admin Resources 过滤框（340px）——t459-probe s3（活体无可观测过滤效果，差异留痕 v1.12）。

## 3. 已知运行时事实

- imports-map（systemjs MFE 资产图）：artifactory 7.161.12 / access 7.191.14 等 12+ MFE——frontend/README 证据源 B。
- 管理侧栏条目/分组形态——nav-tree.json + t459-probe（认证组六子项实据）。

## 4. 缺口声明

- 全局 loading/骨架屏专项：screens.yaml 标 UNKNOWN（树为虚拟滚动渐进渲染，console-ui §5）。
- 暗色主题：实例无主题控件，零实证（states-gaps open）。
