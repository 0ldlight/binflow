# BinFlow 前端实构地图（证据指针文档——正文在既有产物）

> 指针层：`web/src/` 实构的索引 + 设计资产链接。采集日 2026-09-14，全部派生自工作树实态与 docs/design/ 既有审计，零新增取证。

## 1. 栈与形态（as-built）

- **框架**：React SPA，`createBrowserRouter` data 模式（`web/src/app/router/index.tsx`，256 行，自述 57 Route + 4 兼容路径）。
- **UI 原语**：shadcn 风格组件 19 件——`web/src/components/ui/`（badge/button/card/command/context-menu/dialog/drawer/dropdown-menu/input/label/popover/scroll-area/select/separator/sheet/skeleton/sonner/tabs/tooltip）。
- **样式**：Tailwind + 语义 token——`web/src/styles/tokens.css`（`--bf-*` 语义变量 × 亮暗双主题，`[data-theme='dark']` 整组覆盖，编译期断言禁裸色值）+ `styles/tw/{tailwind,tokens}.css` + `base/pages/governance.css`。
- **状态/数据**：`web/src/stores/`、`app/AuthContext.tsx`、`app/ToastContext.tsx`、`app/providers/`。
- **i18n**：`web/src/i18n/`（双语包，E6 条款）。
- **AI 基座**：`web/src/components/ai/`（fe-rewrite phase 5 零网络 shell）。

## 2. 页面地图（49 个 .tsx，按目录族）

| 目录 | 页面 | 对齐参照 |
|---|---|---|
| login/ | LoginPage | Artifactory /ui/login |
| dashboard/ | DashboardPage | 对位 Packages 首页职责（自有形态） |
| explorer/ | ExplorerPage + TreePanel/DetailInspector/ChildrenGrid/CopyMoveDialog | Artifactory 树浏览器 + 详情 |
| artifacts/ | PropertiesTab | 制品属性页签 |
| search/ | SearchPageV2（基本 + AQL 双模式） | Artifactory 搜索 + AQL |
| repos/ + repositories/ | RepositoriesPage（三 Tab）/RepositoryFormPage/RepoDetailPage/ReplicationsSection/RepoDeleteConfirm | Admin Repositories 族 |
| security/ | Users/Groups/Permissions 三列表 + User(Group/Permission) 表单页 + Tokens/Keypair 页 + widgets | Admin Management 安全族 |
| admin/ | AuthConfigPage（LDAP/OAuth/SAML，FR-92 闭集）、LicenseAddonsPage | Authentication 组 / License |
| governance/ | Backup/GC/Quotas/Replication/Trash/MigrationPanel | 治理 → M16 后归监控组 |
| monitoring/ | ServiceStatus/Settings/StorageSummary/SystemInfo/SystemLogs 六页 | 监控组（T-459） |
| builds/ bundles/ | BuildsPage + Promote/Retention 弹窗、BundlesPage + CreateBundleDialog | M17 产品域最小面 |
| webhooks/ | WebhooksPage + Outbox/Subscription 组件 | 常规组（T-459 迁址） |
| audit/ | AuditPage | 自有增强（对位 Artifactory Support Zone 族缺位） |
| — | ProfilePage、NotFoundPage | Profile / 404 |

## 3. 全局壳与弹窗/抽屉族

- 壳：`web/src/app/shell/`（侧栏 18 条目分组 + 顶栏驻留搜索——B-2.13/B-2.18 as-built）。
- 业务弹窗：`components/{DeployDialog,SetMeUpDialog,ConfirmDialog}.tsx` + `dialogs.css`。
- 品牌与协议图标：`components/BrandLogo.tsx`、`PkgIcon.tsx`（+ pkg-icon.css）。
- 功能组件族：`web/src/features/{aggrid,artifacts}/`。

## 4. 设计规范与审计正文（不在此重述）

| 文件 | 角色 |
|---|---|
| `docs/design/frontend-rewrite-audit.md`（133KB） | FE-Rewrite 审计三件套之一：§路由总图（57 Route）+ §API 契约完整路由表（≈156 管理面动词字面量）——BinFlow 前端事实账 |
| `docs/design/frontend-rewrite-architecture.md` | 重写架构决策（栈/目录/数据层/阶段） |
| `docs/design/console-ux.md`（325KB） | UX 规范：IA/线框/交互四态 §5/token/a11y/data-testid 锚册 |
| `docs/design/console-artifactory-parity.md`（544 行） | parity 锚册：N/M/D/L/F/R 六系模式 + §7 差距矩阵 + §12 B47 四态预归属表（48 行）——`docs/ui-parity-matrix.md` 的派生源 |
| `docs/design/frontend-capability-matrix.md` | 26 项能力对账 + e2e 保真三支柱 |

## 5. e2e 资产

`web/e2e/`（m8/m9/m10/m14/m16 目录族 + a11y 双主题 sweep——各票 as-built 留痕见 parity 锚册修订记录）。

## 6. 缺口声明（真无证据的面）

无。前端实构可全量从工作树 + 上述审计对账。
