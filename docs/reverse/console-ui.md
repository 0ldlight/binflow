# Artifactory 控制台 UI 行为规格（console-ui）

> **来源与取舍声明**（clean-room，ADR-0001）：
> - 本规格基于**活体观察**：真实 Artifactory OSS 7.84.10（rev 78410900）实例，Playwright 全程走查取证（2026-08-23）。辅以反编译前端 bundle 的路由表/字符串交叉验证（`reverse-src/` 只读）。
> - **零复制红线**：JFrog 的图标、样式、代码、视觉资产零引用；本文只含行为描述（布局结构、交互流、文案点位、状态矩阵）。走查截图仅存于本机临时目录（`/tmp/bfwalk/`），**不入仓库**；admin 口令运行时从 VM 读取，**未写入任何文件**；Set Me Up 生成的 token 值已从取证输出中抹除。
> - 文案（按钮/列名/错误信息）以英文原样记录——它们是行为契约的一部分，BinFlow 皮肤下文案可中文化但语义应对齐。
> - 置信度：`高` = 活体观察 + DOM/路由双证；`中` = 单次观察或仅 bundle 可见；`低` = 推断，待动态验证。

---

## 1. 全局信息架构（IA）

### 1.1 两种导航模式（置信度: 高）

控制台有全局侧栏，按「上下文」切换两种模式，URL 前缀区分：

| 模式 | URL 前缀 | 侧栏结构 |
|---|---|---|
| 应用模式（普通使用） | `/ui/...`（packages / repos / builds / artifactSearchResults） | 分组 **Application**：Dashboard、Artifactory（可展开）、Xray、Distribution、Pipelines、Integrations |
| 管理模式（Administration） | `/ui/admin/...` | 分组 **Administration**：Projects、Environments、Repositories、User Management、Authentication Providers、General、Proxies、Monitoring；分组 **SERVICES**：Artifactory |

- Artifactory 应用项展开后子项：**Packages / Builds / Artifacts**（置信度: 高）。
- 登录后默认落地：应用模式 `/ui/packages`（Packages 页）；admin 首次登录先落 `/ui/admin/onboarding-page` 引导页（标题 "Welcome To JFrog Platform"，卡片 "Create a Repository" + 右上 "Skip"），点 Skip 后进正常落地页（置信度: 高）。
- 侧栏底部固定显示许可与版权行（如 `Open source license 7.84.10 rev ...` + `© Copyright ...`）（置信度: 高）。

### 1.2 管理模式侧栏二级结构（置信度: 高）

| 一级分区 | 二级项（观察到的完整列表） | 对应路由 |
|---|---|---|
| Projects | —（BinFlow 范围外） | `/ui/admin/projects` |
| Environments | —（范围外） | `/ui/admin/environments` |
| Repositories | Repositories、Layouts | `/ui/admin/repositories/local`、`/ui/admin/repositories/layouts` |
| User Management | Settings、Users、Groups、Global Roles、Permissions、Access Tokens | `/ui/admin/configuration/security/general`、`/ui/admin/management/users`、`/groups`、`/global_roles`、`/permissions`、`/ui/admin/configuration/security/access_tokens` |
| Authentication Providers | LDAP、SAML SSO、OAuth SSO、HTTP SSO、Crowd / JIRA | `/ui/admin/configuration/security/ldap` 等 |
| General | Settings、Mail Server、Webhooks、Manage Integrations | `/ui/admin/configuration/general` 等 |
| Proxies | —（范围外） | — |
| Monitoring | Storage、Service Status、System Logs | `/ui/admin/monitoring/storage-summary`、`service-status`（**OSS 下 404**）、`system_logs` |
| SERVICES → Artifactory | 打开 Artifactory 服务自有管理树（见 1.3） | `/ui/admin/artifactory/list` |

### 1.3 Artifactory 服务管理树（`/ui/admin/artifactory/list`，置信度: 高）

旧式服务级配置树，分组：General（Settings、Property Sets、HTTP Settings）、Services（Backups、Maven Indexer、Import & Export）、Repositories、System、Security（Settings、Keys Management、Certificates）、Advanced（Log Analytics、System Logs、Maintenance、Config Descriptor、Security Descriptor）、Packages、UI Settings。BinFlow 只需映射其中 Backups / Maintenance / Import & Export / System Logs 等已有功能。

### 1.4 顶栏（应用模式，置信度: 高）

左→右：产品标识；仓库范围下拉（默认 "All"）；搜索类型下拉（Packages / Artifacts / Builds，切换后输入框 placeholder 变为 `Search <Type>`）；搜索输入；漏斗筛选图标；帮助图标（?）；用户菜单（"Welcome, \<name\>"）。管理模式下搜索 placeholder 为 "Search Admin Resources"。

### 1.5 用户菜单（"Welcome, admin" 下拉，置信度: 高）

| 项 | 行为 |
|---|---|
| Quick Repository Creation（子菜单） | Set Me Up、New Local Repository、New Remote Repository、New Virtual Repository（直达各自新建页） |
| New User / New Group / New Permission | 直达 `/ui/admin/management/{users,groups,permissions}/{new,new,create}` |
| Edit Profile | `/ui/user_profile` |
| Logout | `/ui/logout` |

### 1.6 前端测试锚点约定（置信度: 高，供 BinFlow 借鉴的**行为层**约定）

新 UI 对关键交互元素暴露 `data-cy` 属性：顶栏 `searchInput` / `searchButton` / `searchType-Artifacts` / `user-dropdown`；树节点 `data-cy="<节点名>"` 且元素 `id` 以完整路径开头。BinFlow 自有皮肤可采用同类稳定 test-id 约定（不必同名）。

---

## 2. 登录与引导

### 2.1 端点/布局

| 项 | 值 | 置信度 |
|---|---|---|
| 路由 | `/ui/login/` | 高 |
| 页面标题 | `Login - JFrog`（BinFlow 自有品牌） | 高 |
| 主文案 | 居中大标题 "WELCOME TO ..." | 高 |
| 字段 | Username（text, name=username）、Password（type=password, id=password-input） | 高 |
| 附加控件 | "Remember me" 复选框 | 高 |
| 动作 | "Login"（submit 型按钮，唯一按钮） | 高 |
| 缺位 | 未观察到 Forgot Password 链接（OSS） | 高 |

### 2.2 行为

- 提交失败即停留登录页；会话过期时任意 `/ui/*` 页重定向回 `/ui/login/`（观察到 token 过期后自动跳转）（置信度: 高）。
- 登录成功 → admin 首次进 onboarding，否则 `/ui/packages`（置信度: 高）。

---

## 3. 核心页面骨架

### 3.1 Packages（应用模式落地页，置信度: 高；BinFlow 可选对齐）

- 顶栏搜索 "Search Packages"；排序控件（Name 下拉 + 方向）。
- 内容为**卡片流**：每卡片 = 包图标 + 名称（Group ID）+ 时间戳 + `Latest version: x`，右侧统计 `n Versions` / `n Downloads`。

### 3.2 Artifacts 制品浏览器 `/ui/repos/tree/General[/<repo>/<path>...]`（置信度: 高）

**三栏式：左树 + 右详情**（无独立面包屑；URL 路径即位置）。

页头动作区（置信度: 高）：
- 按钮：`Set Me Up`、`Deploy`、`Manage Repositories`（跳仓库管理）。
- `My Favorites`（星标计数，初始 0）。
- 树过滤输入（placeholder `Filter repositories`）+ `Clear`。
- `Tree View:` 单选：`Compacted` / `Non-Compacted`。
- 页脚标语行：`Happily serving <N> artifacts`。

树头过滤/排序带（置信度: 高；**2026-09-02 M16 审计回填**——t226 活体取证补记，
原稿漏记该面）：
- `Filter by Package Type`：包类型**复选组**（全量包型各一枚复选，多选交集
  过滤左树仓库行；有勾选时可清）。
- 仓库类型组：`Local` / `Remote` / `Cache` / `Virtual` 四枚复选（同多选
  过滤语义；`Cache` 是 remote 仓缓存内容的独立过滤位）。
- `Sort by` 下拉：仓库行排序（名称序为默认档）。
- 上述 facet/排序位与「过滤仓库」文本框、`Tree View` 单选同区（树头），
  作用域 = 左树仓库顶层行，不进 URL。

树（置信度: 高）：
- 顶层 = 全部仓库节点（含图标按类型区分）+ 末尾常驻 `Trash Can` 节点（路径 `auto-trashcan`）。
- 节点 = 展开箭头 + 类型图标 + 名称；虚拟滚动（长树只渲染可视区）。
- **文件是树叶子**（2026-09-02 M16 审计回填补记）：目录展开 = 子目录行 +
  文件行（文件行 = 图标 + 名称，点击选中出右侧 item view；无展开箭头）。
- 展开懒加载子级；单击选中（右侧联动），URL 同步为完整路径；**深链接自动展开祖先并选中目标**（观察到直接 goto 文件 URL 后树展开到位）。

右侧详情面板（置信度: 高）：

| 节点类型 | Tab 集 | General/Info 字段（顺序） |
|---|---|---|
| 仓库 | General、Effective Permissions、Properties、Followers | Name、Package Type、Repository Path、File URL、Repository Layout、Artifact Count / Size（"Show" 链接展开）、Created |
| 文件夹 | 同上（无 Followers 时的差异未逐一取证，标中） | 未逐项取证 |
| 文件 | General、Effective Permissions、Properties、Followers、**Xray** | Name、Repository Path、File URL、Module ID、Deployed By、Size、Created、Last Modified、Downloads、Last Downloaded By、Last Downloaded、Remote Downloads |

文件详情附加区块（置信度: 高）：`Package Information` → `Dependency Declaration`（Build Tool 子标签：Maven / Ivy / Gradle / Sbt，内容为对应声明片段代码块）；`Virtual Repository Associations`；`Included Repositories`；`Checksums`（SHA-256 / SHA-1 / MD5，各带后缀 `(Uploaded: Identical)`）。

右键上下文菜单（置信度: 高）：

| 节点 | 菜单项 |
|---|---|
| 文件 | Copy、Move、Delete、Download |
| 文件夹 | Copy、Move、Delete Versions、Delete、Native Browser、Refresh |
| 仓库 | Copy Content、Move Content、Delete Versions、Delete Content、Native Browser、Refresh、Add to Favorites |

### 3.3 搜索结果页 `/ui/artifactSearchResults?name=<q>&type=artifacts`（置信度: 高）

- 标题 `Search Artifacts`；副标 `Search Results - <N> Items`（观察到计数与列表不一致的显示怪癖：显示 0 Items 但列表 2 行——不对齐实现）。
- 表列：`Artifact`、`Path`、`Repository`、`Modified`；行点击应回跳树（未逐一验证，标中）。
- 底部分页：`Showing 1 - 2 from 2 items`。

### 3.4 仓库管理列表 `/ui/admin/repositories/{local|remote|virtual}`（置信度: 高）

- 顶部 Tab：`Local` / `Remote` / `Virtual`；计数标题 `<N> Repositories`；右上 `Add Repositories`。
- 表列：`Repository Key`（含包类型图标，链接到编辑页）、`Type`、`Project`、`Environment`、`Replications`（数字）、`Shared With`（数字）。
- 行尾 hover 可见**垃圾桶图标**（行级删除入口）。
- 表格为 AG Grid：列头可点击排序（观察到 asc 排序图标）。
- 底部：`Showing 1 - 5 from 5 items` + 页码。

### 3.5 新建/编辑仓库 `/ui/admin/repositories/{type}/new|{key}/edit`（置信度: 高）

- 标题 `New Local Repository` / `Edit <key>`；顶部横向 Tab：**Basic / Advanced / Replications**。
- 打开新建页即弹 `Select Package Type` 对话框（网格单选，33 项：Maven、HuggingFace ML、Gradle、Ivy、SBT、Generic、Docker、Npm、Pypi、Go、Debian、Rpm、Swift、Terraform、Terraform BE、Alpine、Bower、Cargo、Chef、CocoaPods、Conan、Conda、CRAN、OCI、Gems、GitLfs、Helm、NuGet、Opkg、Composer、Pub、Puppet、Vagrant）。
- Basic 区块顺序：General Settings（`Repository Key`*、`Environments`）→ Repository Layout → Public Description → Internal Description → Include / Exclude Patterns（Include Patterns、Exclude Patterns）→ Xray 集成提示（未连接时显示降级文案）→ **包类型专属设置**（Maven 例：Checksum Policy、Maven Snapshot Version Behavior、Max Unique Snapshots、Handle Releases、Handle Snapshots、Suppress POM Consistency Checks）。
- Advanced 区块：Property Sets（Available/Selected 双列穿梭）→ Other Settings（Priority Resolution、Disable Artifact Resolution in Repository、Allow Content Browsing）。
- Replications Tab（OSS）：仅 `Learn more about the Replications feature` 降级提示（配置 UI 为许可功能）。
- 底部按钮：`Cancel` + `Create Local Repository`（新建）/ `Save`（编辑）。
- 校验：必填空时字段下出现 `You must fill in this field`；失焦（blur）即触发（置信度: 高）。

### 3.6 用户管理 `/ui/admin/management/users`（置信度: 高）

- 列表：`New user` 按钮；列 `Name`、`Email`、`Realm`、`Groups`（如 `1 | readers`）、`Admin`、`Status`、`Last Login`；底部 `Total Users: <N>`。行链接到 `/users/{name}/edit`。
- 新建/编辑表单（`/new`、`/{name}/edit`；标题 `Add new user` / `Edit user: <name>`）：
  - User Settings：User Name、Email Address、Roles（`Administer Platform`、`Manage Resources`）
  - Options：`Can Update Profile`、`Disable UI Access`、`Disable Internal Password`
  - Password / Retype Password
  - Multi-factor Authentication Status（`Reset Google MFA Enrollment`）
  - Related Groups：Available/Selected 双列（Selected 计数）
  - User Permissions：Tab `Repositories | Builds | Release Bundles`；表列 `Permission Name`、`Applied To` + 权限列 **Read / Deploy-Cache / Delete / Annotate / Manage**
  - 按钮：`Cancel`、`Reset`、`Save`
- 编辑页右上 `Actions` 菜单 → `Delete User`。

### 3.7 组管理 `/ui/admin/management/groups`（置信度: 高）

- 列表：`New Group` 按钮；列 `Name`、`Permissions`、`External`、`Admin`、`Auto Join`。
- 表单（`Add new group` / `Edit group '<name>'`）：Group Settings（Group Name、Description、External ID）、Roles（同用户）、Options（`Automatically Join New Users to this Group`）、Users 双列（Available n / Selected n）；编辑态额外显示 Group Permissions 矩阵（同 3.6 的权限列）。

### 3.8 权限管理 `/ui/admin/management/permissions`（置信度: 高）

- 列表：`New Permission` 按钮；列 `Permission Name`、`Users`、`Groups`、`Any Remote`。
- 编辑器 `/permissions/edit/<name>`（新建为 `/permissions/create`，主按钮 `Create` vs `Save`）：**单页分区块**：
  - `Name`
  - `Resources`（副文案 "Select the resource types to which the permission applies."）
  - `Users`（"Select the users and their actions on the selected resources."）
  - `Groups`（同上）
  - 区块入口按钮：`Edit Repositories`、`Edit Builds`（新建态为 `Add Repositories` / `Add Builds`）
  - 底部 `Cancel` / `Save`
- `Edit Repositories` 打开**两步对话框**：① `Select Repositories`（Available N / Selected N 双列；预置 `Any Local`、`Any Remote`、`Any Distribution`）→ ② `Set Patterns (Optional)`（"Setting patterns applies to the artifacts inside the selected repositories."，即 Include/Exclude 模式）；按钮 `Cancel` / `OK`。

### 3.9 维护（GC/配额）`/ui/admin/artifactory/advanced/maintenance`（置信度: 高）

自上而下区块：
1. `Garbage Collection`：Cron Expression、Next Run Time、`Run Now`
2. `Enable Quota Control`：`Storage Space Warning (Percentage)`、`Storage Space Limit (Percentage)`
3. `Cleanup Unused Cached Artifacts`：Cron、Next Run、`Cleanup Unused Cached Artifacts Now`
4. `Cleanup Virtual Repositories`：Cron、Next Run、`Cleanup Virtual Repositories Now`
5. `Compress the Internal Database`、`Prune Unreferenced Data`（附操作说明文案）
- 底部 `Reset` / `Save`。

### 3.10 备份 `/ui/admin/artifactory/services/backups[/list|/new]`（置信度: 高）

- 列表：`New Backup` 按钮；列 `Key`、`Repositories`（`8 | a, b, ...` 计数+悬浮明细）、`Cron Expression`、`Next Schedule Backup`、`Enabled`、`Actions`。OSS 实例预置 `backup-daily`（`0 0 2 ? * MON-FRI`）与 `backup-weekly`（`0 0 2 ? * SAT`）。
- 表单 `New Backup`：Backup Settings（Enabled、Backup Key、Cron Expression、Next Backup Time、`Server Path For Backup` + `Browse`）；Advanced（`Send Mail to Admins if there are Backup Errors`、`Exclude New Repositories`、`Verify enough disk space is available for backup`、`Incremental`、`Retention Period Hours`、`Back up to a Zip Archive (Slow and CPU Intensive)`）；Repositories 双列；`Cancel` / `Save`。

### 3.11 存储概要 `/ui/admin/monitoring/storage-summary`（置信度: 高）

- 顶部 `The data was last refreshed on: <ts>` + `Refresh`。
- Binaries 汇总卡：Binaries Size / Binaries Count / Artifacts Size / Artifacts Count / Optimization（%）/ Items Count。
- Storage / File System 卡：Directory（filestore 路径）、`Used: X / Y (Z%)`。
- 仓库表：列 `Repository Key`、`Repository Type`、`Package Type`、`Percentage`、`Artifacts Size`、`Files`、`Folders`、`Items`；**首行为 TOTAL 汇总行**，次行 `Trash Can`（Type=Trash），随后各仓库。

### 3.12 系统日志 `/ui/admin/monitoring/system_logs`（置信度: 中）

页面标题 `System Logs Viewer`（查看器组件细节未深走——待验证清单）。

### 3.13 仓库布局 `/ui/admin/repositories/layouts`（置信度: 高；BinFlow 可选）

`New Repository Layout` 按钮；`<N> Repository Layouts` 计数；列 `Name`、`Artifact Path Pattern`；分页 `Showing 1 - 9 from 18 items`。

### 3.14 General Settings `/ui/admin/configuration/general`（置信度: 高）

- General Settings：`Server Name`、`Custom Base URL`、`Date Format`、`Enable Help Component`
- Look & Feel Settings：Logo 上传（File/URL 切换 + 拖放）、`Custom Message`（提示链接语法 `[http://example.com, text]`；Enabled、Title、Title Color、Message）
- `Custom Login Dialog`（登录前条款弹窗：Enabled、`Display Custom Login Dialog`：`Only Once` / `Every Login`、Title、Type、Message）
- `Reset` / `Save`

### 3.15 LDAP `/ui/admin/configuration/security/ldap`（置信度: 高）

- 顶部按钮 `Add Settings`、`Add Group`。
- 两个表格：`LDAP Settings`（列 Settings Name、LDAP Url）；`LDAP Group Settings`（列 Settings Name、LDAP Settings、Strategy）。
- 空态文案：`No results were found` + `Try to change your search`。

### 3.16 导入/导出 `/ui/admin/artifactory/import_export`（置信度: 高）

三张并列操作卡：
1. `Export Repository to Path`：Target Local Repository、`Export Path on Server`（Browse）、Exclude Metadata、`Create .m2 Compatible Export`、Output Verbose Log、`Export`
2. `Import Repository from Path`：Target Local Repository、`Import Path on Server`（Browse）、Exclude Metadata、Output Verbose Log、`Import`
3. `Import Repository from Zip`：Target Local Repository、拖放/Select file、Output Verbose Log、`Upload` → `Import`

### 3.17 Access Tokens `/ui/admin/configuration/security/access_tokens`（置信度: 中）

标题 `Access Tokens` + `Generate Token`（表单细节未深走）。

### 3.18 编辑档案 `/ui/user_profile`（置信度: 高）

- Authentication Settings：`Generate an Identity Token`
- `Identity Tokens` 表：Description、Token ID（UUID）、Issued At、Expiry Date（Set Me Up 生成的 token 描述为 `MavenClient[SetMeUp]`，默认 24h 过期）
- `Secure Shell (SSH)`：`Add New SSH Key`；表：Key Alias、Key Signature

---

## 4. 关键交互流（编号，置信度: 高除单独标注）

1. **Set Me Up（Configure）**：选中仓库 → 页头 `Set Me Up`（或用户菜单入口）→ 对话框标题 `Set Up A <PackageType> Client`（Tab `Configure` / `Deploy`，仓库下拉预选当前仓库）→ 密码框（placeholder `Your JFrog account Password`）→ `Generate Token & Create Instructions`：
   - 错误口令 → 内联错误 `Incorrect password`（不出对话框）；
   - 正确 → `The token has been generated successfully!` + token 值 + `Copy` + 按客户端的配置说明（Maven：settings.xml 三个放置位置 `${user.home}/.m2/settings.xml` / `${maven.home}/conf/settings.xml` / 自定义 `-s settings.xml`；服务器定义项 Releases / Plugin Releases / Snapshots / Plugin Snapshots / Mirror Any；`Generate Settings` 按钮；文档链接）→ `Done` 关闭。
   - `Select a different package type`（返回链接）→ 网格对话框 `Set Up A Client`（"Select a package type to learn how to resolve and deploy packages to Artifactory."；本实例显示 generic / docker / maven / npm——与实例内已有仓库的包类型集合一致，标中）。
2. **Deploy（UI 上传）**：页头 `Deploy` → 对话框：Target Repository、Package Type、Repository Layout（模式串）、`Single Deploy`/`Multiple Deploy` 单选、`Drop file` 或 `Select file`、`Target Path`、`Deploy` 提交。
3. **树导航/深链**：URL 即状态（`/ui/repos/tree/General/<path>`）；深链自动展开祖先；树懒加载展开；`Filter repositories` 输入过滤仓库列表 + `Clear` 复位。
4. **树右键菜单**：见 3.2 表；仓库级含 `Add to Favorites`（联动 `My Favorites` 计数）。
5. **快速搜索**：顶栏选类型（Artifacts）→ 输入关键词 → Enter → 跳 `/ui/artifactSearchResults?name=<q>&type=artifacts`；结果表 + 分页（`Showing a - b from c items`）。搜索框旁有 `recentSearches` 锚点（历史记录下拉，未深走，标中）。
6. **新建用户**：必填未满足时 **`Save` 置灰禁用**（观察到 disabled）；满足后可提交；`Reset` 恢复初始值。必填错误文案 `You must fill in this field`。
7. **新建仓库**：进页弹包类型选择（必选）；`Repository Key` 必填（同上校验文案）；`Create Local Repository` 完成创建；`Cancel` 退出。
8. **删除确认（危险操作）**：
   - 用户：`Actions → Delete User` → 对话框 `Are you sure? You are about to remove the user. Do you wish to proceed?`（`Cancel` / `OK`）
   - 仓库：行垃圾桶 → 对话框 `Delete Repository — Are you sure you want to delete the <key> repository? All artifacts will be permanently deleted.`（`Cancel` / `Delete`）
   - 模式总结：**标题 + 后果说明 + Cancel/确认双按钮**，确认按钮使用危险色。
9. **权限编辑**：列表行点击 → 编辑器；`Edit Repositories` 两步对话框（选仓库 → 可选 Patterns）→ `OK` 回填 → `Save` 持久化。
10. **表格排序/分页**：AG Grid 列头点击排序（升/降）；底部分页 `Showing a - b from c items` + 页码（观察到每页 ~9-10 行，标中）。
11. **双列穿梭选择**（Available N / Selected N）：用户/组表单、Property Sets、备份仓库、权限仓库均复用同一组件形态；空侧显示 `No Items Selected`。
12. **表单保存反馈**：`Save`/`Create` 主按钮 + `Reset`（部分表单）；校验内联于字段下。**保存成功 toast 未捕获**（可能静默或瞬时，标低——待验证清单）。
13. **会话过期**：任意页 → 重定向 `/ui/login/`。

## 5. 组件与状态矩阵

| 组件 | 行为要点 | 置信度 |
|---|---|---|
| 数据表 | AG Grid；列头排序；行内图标动作（仓库表=删除）；底部分页文案统一 `Showing a - b from c items` | 高 |
| 批量选择 | **未观察到**复选框列/批量操作（仓库、用户、组、权限列表均无）——BinFlow 无需为对齐而增加批量选择 | 高 |
| 双列穿梭 | Available/Selected + 计数 + 空态 `No Items Selected` | 高 |
| 分步对话框 | 步骤号 + 步骤标题（权限资源选择、Set Me Up 包类型）| 高 |
| 空态 | `No results were found` + `Try to change your search`（LDAP 表）；卡片空态未见 | 高/中 |
| 加载态 | 未专项取证（虚拟滚动树存在渐进渲染）| 低 |
| 危险确认 | 标题+后果+Cancel/Delete(OK) | 高 |
| 键盘快捷键 | **无应用级全局快捷键**（bundle 中仅内嵌 CodeMirror 代码编辑器默认键位，用于 Config Descriptor 编辑）| 中 |
| 页面标题模式 | 列表页=资源名复数；编辑页=`Edit <key>` / `Edit user: <name>` / `Edit Permission <name>`；新建页=`New X` / `Add new X` / `Create Permission` | 高 |

## 6. 与公开文档的关系

- 本规格全部为活体观察所得行为描述，未发现与 JFrog 官方用户文档冲突之处；官方文档不覆盖的细节（错误文案、禁用态时机、右键菜单集、深链行为、data-cy 约定）为本文补充项。
- OSS 与商业版差异由活体验证（而非文档推断）：Audit Log 页、全局 Replication 配置页、Service Status 页在 OSS 7.84.10 **不存在/404**。

## 7. OSS 7.84.10 缺位清单（BinFlow 有而 Artifactory OSS 无对应 UI，置信度: 高）

| BinFlow 页面 | Artifactory OSS 现状 |
|---|---|
| governance/AuditPage（审计日志） | 无路由、无导航项（`/ui/audit`、`/ui/admin/artifactory/audit` 均 404）——BinFlow 保留自有页，无需对齐参照 |
| governance/ReplicationPage（复制管理） | 全局复制配置页 404；仓库编辑 Replications Tab 为降级提示——BinFlow 保留自有页 |
| system 状态页 | Monitoring → Service Status 链接存在但 404（疑由未加载的微前端提供） |
| 配额（QuotasPage） | 仅有 Maintenance 内 `Enable Quota Control` 两个百分比字段（无独立页） |

反向（Artifactory 有、BinFlow 范围外不做）：Xray / Distribution / Pipelines / Integrations / Builds / Packages(可选) / Projects / Environments / Webhooks / Mail Server / OAuth / SAML / Crowd / Property Sets / Maven Indexer / 冷存储等。

## 8. 待验证清单（低置信度汇总）

1. 保存成功后的 toast 形态与停留时长（未捕获）。
2. 各列表加载态（骨架屏/spinner）形态。
3. 表格默认每页行数（观察到 9 与 5 两种场景，疑与行高/视口相关）。
4. `Effective Permissions` / `Properties` / `Followers` 标签页内容结构（未逐一展开取证）。
5. 文件夹节点详情字段集（仅推断与文件类似）。
6. 搜索 `recentSearches` 下拉行为与 `filterIconButton`（漏斗）筛选面板内容。
7. Deploy 对话框实际提交后的反馈与 Target Path 自动推导规则。
8. `Trash Can` 内恢复（restore）交互（仅观察到入口与统计行）。
9. Maintenance 各 `Run Now` 的执行反馈形态。
10. 权限编辑器 Users/Groups 区块展开后的完整矩阵字段（本文仅录得区块骨架与入口按钮）。
