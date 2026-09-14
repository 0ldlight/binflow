# UI Parity 矩阵（新宪章 §7 · Agent 3 六列格式）

> **证据指针文档——正文在既有产物。** 本矩阵零新增取证，逐行派生自：
> ① `docs/design/console-artifactory-parity.md`（parity 锚册：§7 页面×模式矩阵 + §12 B47 四态预归属表 48 行 + v1.6~v1.14 as-built 修订）；
> ② `docs/reverse/frontend/screens.yaml`（41 屏四态判据）+ `docs/reverse/console-ui.md`（7.84.10 走查）；
> ③ `docs/reverse/frontend/parity-capture/`（104 截图——缩写 `pc:s/`=screenshots/screens、`pc:d/`=dialogs、`pc:st/`=states）；
> ④ `docs/compatibility/matrix.yaml`（REST 四态主账；D13 UI 面 ⛔——UI parity 以本矩阵为准）。
> 归档日 2026-09-14（LOOP 022 后时点）。

## 列与状态词汇

| 列 | 含义 |
|---|---|
| Feature | 页面/交互族（P0 页面族粒度，同族细分并入行内注记） |
| Artifactory | 参照行为（一句） |
| Source | 证据指针（截图/规格锚） |
| Binflow | 载体（`web/src/...` as-built，或缺位声明） |
| Gap | 差距摘要（已对齐则「—」） |
| Status | **已对齐**（翻正已落）｜**在途**（承载票/BE 承接）｜**候裁**（Q 表挂起）｜**豁免维持**｜**stay-out**（刻意不对齐/超集）｜**缺位登记**（无载体不伪造）｜**未建·档位外** |

## A. 全局壳与导航（8 行）

| Feature | Artifactory | Source | Binflow | Gap | Status |
|---|---|---|---|---|---|
| 登录页 | 错误内联停留本页；admin 首登转 onboarding | `pc:s/login.png` `pc:st/login-error.png`；screens.yaml#login | `pages/login/LoginPage.tsx` | — | 已对齐 |
| 首启向导 / Packages 首页 | onboarding + Quick Setup；制品卡片流 + 排序 | `pc:s/onboarding.png` `pc:s/quick-setup.png` `pc:s/packages.png`；console-ui §3.1 | dashboard 承载 + ADR-0009 口令首启 | 形态自有，非逐字复刻 | 豁免维持 |
| 侧栏导航树（管理态分组） | Artifactory/Management/Configuration 分组 | `pc:nav-tree.json` `pc:dom-snapshots/sidebar.html`；t459-probe s4 | `app/shell/`（18 条目：监控 6/治理 4/常规 2/仓库 1/用户与权限 5） | 分组拓扑对齐 7.161 活体 | 已对齐（B-2.18/T-459） |
| 顶栏族（用户菜单 / Admin 过滤框） | 用户菜单六项；管理态顶栏 340px「Search Admin Resources」 | `pc:d/user-menu-dropdown.png`；t459-probe s3（A1-5） | shell 顶栏 + 管理态过滤框（真生效） | 活体过滤无效果，BinFlow 承载 7.84 语义（差异留痕 v1.12） | 已对齐 |
| 顶栏快搜 overlay | overlay + 最近搜索 + 空历史占位（+ 范围页签） | `pc:d/quick-search-overlay.png`；B-2.14/B-3.16 | 顶栏驻留搜索（253px 紧凑档 A4-5 + 「暂无最近搜索」占位） | 范围页签不做 | 已对齐（T-449）+ stay-out 分项 |
| 帮助钮 | ? 下拉（7.161 实测：JFrog Documentation/Academy/Navigation Tour） | t457-probe；B-2.17 | ? 下拉四项 + About 版本弹窗 | 四项集 = PRD 定案（经典成员回填），不追随实测 | 已对齐（T-457） |
| Profile 页 | 密码 + 自助 token/SSH | `pc:s/user-profile.png`；B-1.8 | `pages/ProfilePage.tsx`（Identity Token 自助签发 + step-up + curl 样例） | SSH key 无 BE 端点（`profile-ssh-gap`） | 已对齐（T-457）+ 缺位登记（SSH） |
| 404 页 | 平台 404 形态 | `pc:s/notfound-404.png` `pc:st/route-404.png` | `pages/NotFoundPage.tsx` | — | 已对齐 |

## B. 制品浏览与详情（8 行）

| Feature | Artifactory | Source | Binflow | Gap | Status |
|---|---|---|---|---|---|
| 树浏览器（栈 as-built） | 懒加载/URL 同步/深链自动展开；文件叶子在树；树头工具带 | `pc:s/tree-general.png` `pc:s/tree-empty-repo.png`；screens.yaml#artifacts-tree；B-1.1~B-1.4（K67 树栈四项） | `pages/explorer/TreePanel.tsx` | — | 已对齐（T-434） |
| 树右键菜单 | Delete Content/Native Browser/Refresh（门控项更深） | `pc:d/tree-context-menu.png`；B-3.1 | 右键菜单（子集） | Pro 门控族不建 | 豁免维持 |
| children 表 | 目录子项表 + 行内删除 | B-2.2 | `explorer/ChildrenGrid.tsx`（增强保留 §9A-S5） | 行内删除已过危险确认（E1 统一） | 已对齐（T-434 + Q2 出口①） |
| 制品详情页签集 | 页签序（权限在属性前，admin-only 门控） | B-2.1；console-ui §3 | `explorer/DetailInspector.tsx` | 三级页签序断言在途 | 在途（T-445） |
| 文件元数据字段集 | File URL/Downloads 族/virtual 关联块 | B-2.3 | 详情字段族 | 端到端字段族在途；Module ID stay-out（M17 解禁） | 在途（T-445/T-438） |
| 属性编辑 | 常显 Property/Value + Add + 网格搜索 + Property\|Property Set 分段 | B-2.9 | `pages/artifacts/PropertiesTab.tsx` | Property Set 分段不建（K68 候裁） | 已对齐（T-447） |
| 下载形态 | 单 24px 图标钮 + 伴随菜单收校验/checksums/mimeType | B-2.12 | 详情下载钮 + Popover 伴随 + 计数联动 | — | 已对齐（T-447） |
| Deploy / Set Me Up 弹窗 | Deploy 选文件；Configure/Deploy/Resolve 三页签（50vw） | `pc:d/deploy-dialog-open.png` `pc:d/set-me-up-configure.png`；console-ui §4-1 | `components/{DeployDialog,SetMeUpDialog}.tsx` | — | 已对齐 |

## C. 搜索（6 行）

| Feature | Artifactory | Source | Binflow | Gap | Status |
|---|---|---|---|---|---|
| 搜索结果页（列集） | 选择列+Artifact\|Path\|Repository\|Modified 默认在场 | `pc:s/artifact-search-results.png`；B-2.11 | `pages/search/SearchPageV2.tsx`（默认列归一 + 列选器） | — | 已对齐（T-449） |
| 查询位置 | 顶栏驻留 + 网格快滤 | B-2.13 | 顶栏驻留（Enter → /search?q=）+ 快滤两层正交 | — | 已对齐（T-449） |
| AQL 编辑器 | Query Language 面板（服务端查询面） | `docs/reverse/aql.md` | SearchPageV2 AQL 模式（offset/limit 重写） | 面板形态自有 | 已对齐（T-451 AQL 腿） |
| 结果行导航 | 仅 name 单元格深链 | B-3.14 | 行体 inert + name 链接（K67-3 规范形） | — | 已对齐（T-449） |
| 日期格式 | `dd-MM-yy HH:mm:ss +ZZZZ` | B-3.15 | 结果表 formatStamp 单源 | 详情腿 ISO 格式化在途 | 已对齐（结果表）+ 在途（详情腿 T-445） |
| 结果计数一致性 | header/footer 可不一致（参照实测缺陷） | §9A-S3 | 恒一致 | 刻意更优 | stay-out |

## D. 仓库管理（12 行）

| Feature | Artifactory | Source | Binflow | Gap | Status |
|---|---|---|---|---|---|
| 仓库列表三 Tab（含 Replications 列） | local/remote/virtual + 计数 + AG Grid；Remote 每行复制计数 | `pc:s/repos-{local,remote,virtual}.png`；B-3.9 | `pages/repos/RepositoriesPage.tsx` | Project/Shared With/Environment 列缺位不伪造 | 已对齐（T-443）+ 缺位登记 |
| 建仓入口拓扑 | 「＋ 新建仓库」下拉三预选分路由 | s3e-create-dropdown；B-3.8 | 下拉三预选 → /admin/repositories/&lt;rclass&gt;/new | — | 已对齐（T-443） |
| 包类型弹窗 | 924px 居中 + 型录磁贴 | B-2.6 | 建仓弹窗 924px + 13 型实有 | 型录不伪造 41 型 | 已对齐（T-441） |
| 仓库表单步进（含 footer） | Basic\|Advanced\|Replications 三步；Cancel+Save 两钮 | B-2.5/B-3.11；`pc:s/repo-form-new-local.png` | `pages/repos/RepositoryFormPage.tsx`（jf-steps + 两钮，Reset 已裁撤） | — | 已对齐（T-439） |
| dirty-gating | 无变更 Save 置灰 | B-3.7 | deep-equal 基线门 | — | 已对齐（T-443） |
| remote Test 连通性 | Basic 步来源节旁三臂 | B-3.6 | Test 三臂（绿/凭据拒红/不可达红） | — | 已对齐（T-442/T-443） |
| 表单字段缺口（隐藏四域 + 概念级三域） | PUT 全收 UI 无（maxUniqueSnapshots 等）；Stage/Suppress POM/描述拆分 | B-1.5/B-3.12 | 预留位形态（恒禁用零提交 + tripwire）；Force Auth 实字段 | configJSON 转发须 BE 承接票 | 在途（BE 承接） |
| 远端浏览可选档 | listRemoteFolderItems 复选（官方五型） | B-3.20；`docs/reverse/remote-browsing.md` | 表单/详情/树三面 | 呈现域自有（helm/debian/rpm）；BE note 渲染腿在途 | 已对齐（T-461）+ 在途（T-448 缝） |
| 仓库详情中间页 | Artifactory 无详情层 | §9A-S4 | `pages/repos/RepoDetailPage.tsx` 自有增强 | 刻意超集 | stay-out |
| 行操作超集 | 单 trash 直删（轻确认 520px） | B-3.10；`pc:d/delete-confirm-row-menu.png` | copy-key/Set Me Up/部署超集 | L2/E1 家族豁免 | 豁免维持 |
| Repository Layouts 管理页 | 26 默认 layout 注册面 | `pc:s/layouts.png` | 无（布局由协议 adapter 固定） | K70 口径 | 未建·档位外 |
| 危险删除确认 | 标题+后果+Cancel/确认（危险色） | `pc:d/delete-confirm-dialog.png` | `components/ConfirmDialog.tsx` | — | 已对齐 |

## E. 安全管理（10 行）

| Feature | Artifactory | Source | Binflow | Gap | Status |
|---|---|---|---|---|---|
| 用户列表 | 列集 + Total Users 计数 | `pc:s/users.png`；B-3.18 | `pages/security/UsersPage.tsx` | Last Login 列条件票；Realm 列无端点 | 在途（T-468 条件）+ 缺位登记 |
| 用户/组表单路由化 | 整页路由表单（非 modal） | `pc:s/user-new.png` `pc:d/user-form-filled.png`；B-2.15 | /users/new、/groups/new 深链整页 + 内联卡退役断言 | — | 已对齐（T-453） |
| 用户能力位三旗 | Can Update Profile/Disable UI Access/Disable Internal Password | B-1.7 | 三旗预留位（恒禁用 + tripwire） | BE decode 吞咽、GET 回显不动 | 在途（BE 承接） |
| 管理位形态 | Administer Platform + Manage Resources 双布尔 | t453 探针；B-1.7 候裁 | 三值枚举（ADR-0026）+ 差异注记 | 双布尔不建不伪造 | 候裁（ADR-0046 tripwire） |
| 组族（列表/表单） | 列集 + 表单域（External ID/Auto Join 等） | `pc:s/groups.png` `pc:d/group-form-filled.png` | `pages/security/{GroupsPage,GroupFormPage}.tsx` | 无对位域三件缺位登记 | 已对齐 + 缺位登记 |
| 权限列表 | 权限目标列表 | `pc:s/permissions.png` | `pages/security/PermissionsPage.tsx` | — | 已对齐 |
| 权限编辑器 | 选仓双列穿梭 + 双步头 + 矩阵编辑（7.161 三步向导路由页） | `pc:d/permission-picker-step.png` `pc:d/permission-matrix-editor.png`；B-2.16 | `pages/security/PermissionEditorPage.tsx`（+Any 三预置桶 T-514） | 三步向导形态不建（差异留痕） | 已对齐（T-455/T-514） |
| 权限动词五列 | Read/Annotate/Deploy-Cache/Delete-Overwrite/Manage | t455-probe；B-1.6 | 编辑器五列（wire 正名单 + 独立位列零联动） | — | 已对齐（T-444/T-455） |
| Access Tokens / Keys Management | 台账 + Generate 表单；GPG keypair 管理 | `pc:s/access-tokens.png` `pc:d/token-generate-form.png` `pc:s/keys-management.png` | `pages/security/{TokensPage,KeypairPage}.tsx` | 服务端令牌清单端点缺（§9-R6） | 已对齐 + 在途（条件） |
| 认证配置组 | 六子项（LDAP/SAML/OAuth/HTTP SSO/Crowd·JIRA/SCIM） | `pc:s/{ldap,saml,oauth,http-sso,crowd,scim}.png`；B-2.18 | `pages/admin/authconfig/AuthConfigPage.tsx`（三协议，FR-92 闭集） | HTTP SSO/Crowd/SCIM 不存在 | 已对齐 + 缺位登记 |

## F. 治理与监控（10 行）

| Feature | Artifactory | Source | Binflow | Gap | Status |
|---|---|---|---|---|---|
| 监控组六页 | Storage/Service Status/System Logs/System Info/维护/备份 | `pc:s/storage-summary.png` 等；B-1.11 | `pages/monitoring/` 六页 | Uptime 无端点缺位 | 已对齐（T-459） |
| System Logs | 三选择器 + 尾随 7s + Pause/Refresh + 下载 | `pc:s/system-logs-af.png`；t459-probe | SystemLogsPage（日志源=审计跟踪） | 进程日志端点族缺（BE 建议票） | 已对齐（降级定案）+ 在途（BE） |
| 维护 GC 面 | GC/Cleanup 族 + cron 槽 + Run Now | `pc:s/maintenance.png`；B-1.9 | gc-cron 三槽行表（表达式/next-run/Run Now 并存） | Quota 百分比/Compress/Prune 无载体 | 已对齐（T-462）+ 缺位登记 |
| 备份定时 CRUD | New Backup 表单 + 列表 + E1 删除 | `pc:s/backups.png`；B-1.10 | 监控组备份页两卡（表单四字段 + CLI 卡） | 仓子集/incremental/zip 等无载体字段 | 已对齐（T-462）+ 缺位登记 |
| Import/Export 管理页 | 交互式导入导出 UI | `pc:s/import-export.png` | CLI 卡同页 | 交互式刻意不做（ADR-0015 勘误②） | stay-out |
| 治理组（配额/回收站/迁移） | 对位分散 | `pc:s/` 治理族 | `pages/governance/`（Quotas/Trash/Replication/MigrationPanel） | Trash 最小入口（B-3.4） | 豁免维持 |
| 复制管理面 | 仓级 Replications 步 + 状态图标链 | R1~R10（锚册 §6A） | `pages/repositories/ReplicationsSection.tsx` + ReplicationPage | pull 复制不做（ADR-0021） | 已对齐（push 域） |
| Webhooks 管理页 | 常规组订阅管理 | B-2.18 | `pages/webhooks/WebhooksPage.tsx`（+Outbox/Subscription 组件） | — | 已对齐（T-459） |
| License/Addons 面 | License 与 addon 管理 | `pc:s/` 企业族 | `pages/admin/LicenseAddonsPage.tsx` | 自有 ed25519 三档（ADR-0032 刻意异构） | 豁免维持 |
| 审计页 | Support Zone 族（非独立 UI） | — | `pages/audit/AuditPage.tsx` 自有增强 | 刻意超集 | stay-out |

## G. 全局交互模式（7 行）

| Feature | Artifactory | Source | Binflow | Gap | Status |
|---|---|---|---|---|---|
| 空态文案 | "No results were found" + "Try to change your search" | screens.yaml 全局注（E4） | 全局空态组件 | — | 已对齐 |
| 分页控件 | "Showing a-b from c items" + 页码（7.84）/ag-grid 翻页器（7.161） | B-3.3；K67 分页锚；§11.2 | `components/Pager.tsx` ×9 面（档位 [20,50,100,200,1000] 冻结） | 形态锚以 7.84 页码控件为准 | 已对齐（T-451） |
| 危险确认 + 内联校验 | 标题+后果双按钮；"You must fill in this field" blur 触发 | screens.yaml 全局注；`pc:st/user-form-validation-error.png` | ConfirmDialog + 表单内联错误 | — | 已对齐 |
| Toast 反馈 | 顶部居中单条 ~2-3s | F1 锚；`pc:st/toast-user-created.png` 等 3 张 | ToastContext + sonner | — | 已对齐 |
| 边缘态（悬停 / api 失败 / 权限拒绝） | 悬停高亮；列表错误态；非 admin 管理路由拒绝 | `pc:st/hover-sidebar-item.png` `pc:st/api-failure-users.png` `pc:st/permission-denied-admin-route.png` | 主题 token 悬停 + 错误/守卫态 | — | 已对齐 |
| 亮暗双主题 | （7.161 实例无主题控件——零实证） | states-gaps.md open 项 | tokens.css 亮暗双谱 + a11y 双主题 sweep | 参照不可达，锚自有基线 | 缺位登记（参照侧） |
| 主题/品牌 token | 7.161 设计 token 六面 | `pc:tokens.json` | `web/src/styles/tokens.css`（--bf-* 语义系） | 新宪章品牌系统重定义 | 在途（宪章 B-1/B-3 轨） |

## H. 企业面（5 行）

| Feature | Artifactory | Source | Binflow | Gap | Status |
|---|---|---|---|---|---|
| Builds 页 | build-info 浏览 + promote | `pc:s/builds.png` | `pages/builds/BuildsPage.tsx`（M17 最小面 + Promote/Retention 弹窗） | 深层 retention UI 差异 | 部分对齐（最小面） |
| Release Bundles | RB 列表/创建多步/分发 | `pc:s/release-bundles.png` + `pc:d/release-bundle-create-*.png` 6 张 | `pages/bundles/BundlesPage.tsx` + CreateBundleDialog | 分发编排不做（Distribution 外部服务 ⛔） | 部分对齐（最小面） |
| 企业策略面（Lifecycle/Retention/Federation） | 独立策略管理页 | `pc:s/{lifecycle,retention-policies,federation}.png` | 无独立面（RetentionDialog 最小承载） | D11 ❌ / ADR 域外 | 未建·档位外 |
| 外部集成与 Platform 面 | Vault/Crowd/SCIM/HTTP SSO/反向代理/证书/Mail/Log Analytics + Xray/Pipelines | `pc:s/vaults.png` 等 10+ 张 | 无 | ENT UNKNOWN 簇（28 条）；Xray 永不建 | 未建·档位外 |
| Migration Tool | 迁移工具页 | `pc:s/migration-tool.png` | `governance/MigrationPanel.tsx`（bf-migrate CLI 承载） | UI 形态自有 | 豁免维持 |

## 汇总

- **行数**：66（A8 + B8 + C6 + D12 + E10 + F10 + G7 + H5）。
- **状态分布**（按行主位计）：已对齐 43 ｜ 在途 6 ｜ 部分对齐（最小面）2 ｜ 候裁 1 ｜ 豁免维持 6 ｜ stay-out 4 ｜ 缺位登记（主位）1 ｜ 未建·档位外 3；另有 8 处「已对齐 + 缺位分项」与 3 处「已对齐 + 在途分项」行内注记。
- **口径声明**：本矩阵为 UI 面视图，行态以引用的锚册 §12/修订记录与 screens.yaml 为准；REST 面四态仍在 `docs/compatibility/matrix.yaml`（D13 ⛔ 维持）。
