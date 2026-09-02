# M16 素材汇总（不做清单 66 项 + 活体偏差 47 项合并）

依据：M16 常设指令（2026-09-02）= 全前端 Artifactory 完全对齐 + 不做清单全面翻案（除 Xray）；制品树偏离为 P0 先行。xray_tied 条目已按要求从翻案清单剔除。

---

## A. 翻案清单（flippable，按域分组；xray_tied 已剔除）

### A0. 无翻案通道（永久红线，仅登记不议）
- clean-room 红线族：逐行翻译 Java→Go（含 AqlOqlParser/SQL builder）、复制 JFrog license 密钥格式、像素级复刻 UI/前端资产、官方协议 logo 矢量源复用（ADR-0001/0029 + package-icons §5）。附属：nuget/conan/docker 鲸腹三枚低置信图标商业化分发前商标复查。

### A1. 产品级 Non-goal（可翻，但须 PRODUCT.md 修订 + 用户终裁，非 parity 可翻）
- HA 高可用集群与联邦（active-active）本体——三程候选池续滚；解禁后建议专程里程碑 + ADR 群，不混编主线。
- Artifactory 全量 REST 兼容——高频子集承诺维持（M15 落点：AQL 只承诺语言子集）。
- 洞察报表/趋势分析图表（parity §9 永不建①；dashboard 去重率口径不变）。
- Builds / Federation / Lifecycles / Release Lifecycle / Repository Path Map 管理面（§9 永不建③）——连带：Build-info 域（PUT/append/查询/promotion/retention + docker promote）、Release Bundle 域、AQL 六域中的 build/module/dependency/promotion/releasebundle 面、/api/search/license·dependency·buildArtifacts；「不为 AQL/对齐而建功能本体」防线随域解除。D3 依赖树若立项须先出后端依赖解析域票。
- 历史已解禁兑现（无需再动）：LDAP/SAML/OIDC（M6 FR-54~56 + M11 FR-92）、Helm OCI / Go 协议（M10 FR-87 / M13 三态）、docker remote pull-through（M13 FR-116 / M14 FR-129）。

### A2. 搜索/统计域（M15 既定 M16 第一顺位）
- AQL statistics/usage 域 + /api/search/usage（usageSince）——Q1 分阶段边界首裁项；依赖 per-node 下载计数基建（与 B 侧详情页 Downloads/Last Downloaded 字段族同一基建，一鱼两吃）。
- QRL 全量版（v1/system/query_rate_limiter 三态 + 指标 job + REST 面）——M15 只落资源门简化版（K63）。
- UI 搜索族：artifactsearch / stashResults / packagesSearch / syntax-search——随 AQL 高级面；Smart Searches 系 Artifactory pro 档，须按 addon 槽裁决。
- 剩余老搜索端点：dates/creation（规格票判 trivial 可余量顺车）、badChecksum、versions/latestVersion。
- statisticsEnabled / sourceOrigin 行为化——待 stats 面立项，与 AQL usage 域同族。

### A3. 治理/存储域
- Cleanup-Retention 策略引擎 + 冷存储分层（M12 仅落回收站 14 天保留）。
- GC 维护面扩展：Cleanup Unused Cached Artifacts / Cleanup Virtual Repositories / Compress Internal Database / Prune Unreferenced Data / Quota 百分比控制——翻案与 C1 cron 裁定绑定。
- 备份调度 CRUD UI（New Backup / cron / next-run / 列表）+ import/export 管理页——现状 CLI 引导（M6 模型），翻案同绑 cron。

### A4. 包型/协议域
- Go 深化（sumdb 代理 + external 重定向）。
- Terraform Provider / GitLFS 包型。
- AI/ML 包型扩展（HuggingFace 等 13 型）。
- deb bz2 压缩档——ruled-out，唯一翻案通道 = 用户提供实现路径（纯 Go bzip2 写入器）；deb snapshot 族另行缓议（T-310 §10）。
- NuGet symbol server（.pdb/GUID 路径）——条件票五承未触发；M16 可六承或转正排期（mini as-built 规格随票，T-293 终裁口径）。
- 控制台包类型开禁：13 tiles 中 8 个**后端已实现**包型（go/nuget/cargo/conan/helm/helmoci/rpm/debian）被 'pro' 标记禁用——纯前端门，翻案成本低（见 B-仓库表单组）。
- M12 维持登记族：conan remote search 上游代理（P3）、属性复制协议面扩列、keypair T-319 / SAML T-331 差异族、crates.io 直连双主机。

### A5. 复制/remote 域
- Replay + outbox 行级 REST 面——运营增强非协议兼容面、死信重放机制已备、翻转面小；曾因与 AQL 挤 httpapi lane 让位。
- remote 仓远端浏览实现段（回源目录枚举，不限已缓存）——Q4 三出口待终裁：全做 / 子集（PM 暂行倾向：helm index、docker tags 先行）/ 维持缓存浏览。
- remote 缓存树高并发 busy 重试预算——M15 FR-139.2 已改判收编（在途，非 M16 新项）。
- docker virtual 建仓矩阵开禁（Q6，PM 倾向开，helmoci virtual M13 先例）+ by-digest 拉取强刷（Q7，暂行维持 TTL 统一不强刷）。
- cron 双轨（复制域用户级 cron）——ruled-out，翻案归 C1 终裁。

### A6. 校验/一致性小票池
- virtual 成员同型校验全包型推广（T-367）——需 13 包型 × 三 rclass 回归矩阵；现态缺陷面窄。
- Tokens 生成表单字段集补核验（Q4/V6c 降级）——候商业版/云实例活体源条件票，源可得即翻正为高置信规格。
- license 公钥 config 覆盖——须走新 ADR（T-293 终裁③）。
- L1 列选器与刷新推广：E-04 repos 列表扩列 + 用户/组/权限列表（columnPrefs 共享层已就绪，接上即得；T-387 遗留）。
- 用户列表 Last Login 列——audit 已记录登录，可派生（M16 重开）。

### A7. UI 豁免翻案候选（flippable，一行级为主）
- E7 toast 锚位（F1 微调票：Snackbar anchorOrigin 一行级）——E 系唯一显式 flippable「再议」项。
- 决策项 C：Deploy 对话框居中 Dialog vs 抽屉化——终裁窗显式开放。
- E2 加载更多 vs 页码控件——理论可翻的范式项（×9 处），需用户定夺。
- E3 仓库类型 badge 中性色——视觉预算决策。
- Access Tokens 页 L1 列选器——架构约束（会话内存态台账无持久对象），token 面持久化时才重评。

### A8. stay-out 候选（Artifactory-has-no-counterpart / 对位即要避开的形态——建议维持不建，请用户确认）
- E4 权限编辑器整页 + 模式测试器 + diff 确认——BinFlow 增强面，无对齐对象。
- L2 行内 ⋮ 菜单（T-385 stood down）——V4 实证 7.84 行尾无 ⋮（= trash 直删，恰是 E1 要避开的形态）；「复制 key / Set Me Up 行内快捷」可留自有增强票（不挂 parity 旗）。
- 结果计数一致性（header/footer 恒一致）——Artifactory 实测有计数怪癖，明确不对齐缺陷。
- 仓库详情中间层（概要/配置/Replications）——Artifactory 无详情层，BinFlow 附加层，去留待裁（见 C9）。
- children 表复合形态——console-m8 §6.3 自设计，Artifactory 右侧纯 item view；与「文件进树」（B-1.1）联动：文件进树后表可收窄或保留为增强。
- disable 快照契约翻转（T-364 钉死）——无用户推翻信号维持关闭。

（剔除说明：Xray 漏洞扫描/合规平台、Xray 集成面、license 识别 licences.xml 91 模式、Build-info 首条、HA+Xray 组合条、webhook 57 型休眠触发源——均 xray_tied。webhook 裁剪方案（仅注册有源域）仍待用户裁定，ADR-0041 决策 7 翻转路径在案，其中非 xray 域触发源部分 M16 可裁。）

---

## B. 偏差清单（47 项；logic 11 / visual 17 / minor 19；组内 tree-deep 优先）

### B-1 logic（11 项）

**制品树栈（P0 先行复查区）**
1. 左树无文件叶子——树 folders-only，文件仅右侧 children 表行可达；仅含文件的目录渲染误导性「（空）」占位（`ArtifactsBrowser.tsx:1270` filter `n.folder`，:1272 空占位）。未登记豁免：需裁「登记豁免 or 补文件叶子」。
2. 选择即展开——仓库名单击强制展开分支（`isOpen = expanded.has(key) || selectedRepo === repo.key` :1106）；深链祖先链含被选目录自身（:237-246），Select ≠ 纯 select。深链自动展开本身对齐；纯选择语义未豁免。
3. URL/状态模型——活跃页签不进 URL（组件态，`NodeDetail.tsx:67`）；文件选择是 `?focus=` 查询参数非路径段（:267-274）。Artifactory = `/ui/repos/tree/<TAB>/<repo>/<path>`，页签即 URL 段、文件 URL 纯路径可重开。页签态与部分文件态不可分享深链。
4. 树头工具带缺失——无包类型 facet 复选组、无 Local/Remote/Cache/Virtual 组、无 Sort-by、无 Compacted/Non-Compacted 单选、无 My Favorites（仅单个「过滤仓库…」文本框 :498-519）。附带：reverse §3.2 漏记 Filter-by/Sort-by facets，逆向规格需回填。

**仓库表单**
5. 表单藏字段——maxUniqueSnapshots / repoLayoutRef / blackedOut / archiveBrowsingEnabled 全表单 grep=0，但 PUT /binflow/api/repositories 全收（`internal/httpapi/repositories.go` L55-61）：API 可达、控制台不可达。

**安全/治理/监控**
6. 权限动词集——无 Annotate（n）；write 未按 Deploy/Cache 呈现（Artifactory 5 列 Read/Deploy-Cache/Delete/Annotate/Manage vs BinFlow read/write/delete/manage）。M1 子集豁免被 M16 指令重开（见 C7）。
7. 用户表单能力位——缺 Can Update Profile / Disable UI Access / Disable Internal Password；Administer Platform + Manage Resources 双布尔被三值角色枚举（admin/readonly_admin/user）替代。
8. Profile 无自助 identity token 生成、无 SSH key 管理——签发指到 admin Tokens 页（文档化设计、非正式豁免）。
9. GC 维护面——无 cron 调度、无 Cleanup Unused Cached Artifacts / Cleanup Virtual Repositories / Compress Internal Database / Prune Unreferenced Data / Quota 百分比（仅 dry-run/apply 两钮）——绑 C1。
10. 备份页——无定时备份 CRUD（New Backup/cron/next-run/列表）、无 import/export 页（仅 CLI 引导卡）。
11. 监控组仅存储页——无 System Logs、无 Service Status（SystemInfoPage 挂常规组且无日志查看器）。

### B-2 visual（17 项）

**树/详情复合**
1. 详情页签集逐级不同——常规/属性/有效权限（属性仅节点、权限 admin-only）vs 每级 General/Effective Permissions/Properties/Followers + 文件级 Xray。Followers/Xray 系 Non-goal 已豁免；共享三页签的**顺序**（属性在权限前）无豁免覆盖。
2. 右侧复合——详情面板下挂 children 表（名称/类型/大小/修改时间/sha256/操作，每行 详情/下载/删除，:895-1000）。Artifactory 右侧纯 item view（文件夹给 Artifact Count/Size: Show）。复合形态已自设计规格；**行内删除与 E1 文本相抵**（见 C2）。
3. 文件元数据字段集——缺 File URL（含复制钮）、Module ID、Downloads/Last Downloaded By/Last Downloaded/Remote Downloads 族、Package Information·Dependency Declaration / Virtual Repository Associations / Included Repositories 块；多出 mimeType、checksum「上传时提供：一致 ✓」徽标、下载校验块。
4. 仓库/目录元数据——仓视图缺 Repository Layout / Description / Created / Artifact Count·Size(Show)；目录视图缺 File URL；多出 类型/子项/修改时间。
5. 表单结构——扁平单页（常规设置/治理），无 Basic | Advanced | Replications 步进条；复制配置为内联「＋新建复制配置」子表单非第三步。
6. 包类型弹窗——440px / 13 tiles（5 可用、8 禁用带 'pro'）vs 880px 居中 / ~33 型全可用（含 Gradle/Ivy/SBT/HuggingFace）。宽度/范围差异未登记。

**制品详情/搜索**
7. 制品详情页签（独立取证口径）——3 页签、属性在权限前、无 Followers/Xray（同第 1 条主题）。
8. 有效权限渲染——chip 列表（空时一句话「admin 隐式全权」）vs Users|Groups|Permission Targets 分段开关 + 搜索 + AG grid + 列选择器；不显示哪个 target 授予权限位。
9. 属性编辑解剖——隐藏「+ 新增属性」表单 + 逐行 ✎/🗑 vs 常显 Property/Value 输入 + Add + Property|Property Set 分段 + 网格搜索；无 Property Set 概念。
10. General 字段集（独立取证口径）——无 File URL 链接、无 Module ID、无下载计数（同第 3 条主题）。
11. 搜索结果列集——仓库打头、无独立 name 列、加 大小+sha256、无行选择列（vs Artifact(name 链接)|Path|Repository|Modified + 选择列）。现行五列与 T-414 注释口径、console-m8 §6.4 口径**双双不一致**。
12. 下载形态——两个带文字按钮（下载并校验 + 直接下载）vs 单 24px 图标钮无校验伴随。
13. 查询位置——页内输入（名称/路径 + 仓库过滤，服务端重查）vs 顶栏驻留 + 网格内 100px 快滤。
14. 快搜入口——253px 紧凑输入 + 最近搜索下拉 vs 815px 全宽 overlay（范围页签 Artifacts/Packages/Builds + 底部 SEARCH/Cancel）。范围页签缺位系「R2 类型化落地后再现，不建影子入口」设计。

**安全/shell**
15. 用户/组创建为列表页内联展开卡（无 /users/new、/groups/new 路由）vs 路由整页表单（Cancel/Reset/Save）——与 E5/V6 决策前提矛盾，且 BinFlow 内部不一致（权限创建是路由页）。
16. 权限编辑器——内联「＋添加仓库…」+ 路径模式 textarea + 内联添加用户/组 vs 两步弹窗（① Select Repositories 双列 + Any Local/Any Remote/Any Distribution 预置 → ② Set Patterns include/exclude）。
17. 帮助钮——纯超链接（? 帮助 → docs 站）无下拉、无 About 版本弹窗（版本仅侧栏脚注 vdev）vs「?」下拉（Documentation/Online/Release Notes/About）+ About 弹窗。
18. Admin 导航分组——认证 = 单页三页签（LDAP/OAuth(OIDC)/SAML）vs 5 子项组（无 HTTP SSO、Crowd/JIRA）；Webhooks 挂治理非常规；维护/备份挂治理非服务节点；无侧栏「Search Admin Resources」过滤框。

### B-3 minor（19 项）

1. 右键菜单集——更小（文件=复制/下载/删除，目录=复制/删除/刷新，仓库=复制路径/刷新/在仓库管理中打开）vs Copy/Move/Delete/Versions/Delete Native/Browser Refresh（+repo 侧 Favorites/Copy Content）——大半豁免（Pro 门控 + E1），admin 深链为自有增强。
2. 初始态——无选中 + 静态引导卡 vs 首仓库自动选中（first-root active highlighted）并出 item view。
3. 空态/上限/分页——「（空）」占位、单层 TREE_LEVEL_CAP=300、BIG_DIR=2000 警示、表 100/页加载更多。加载更多系 E2 豁免；上限/警示为自工程通知、未豁免但无害。
4. 系统节点——无 build-info 伪仓库（§9）；Trash 为叶节点（🗑 emoji + 中文「回收站」）路由 /admin/governance/trash、admin∪readonly_admin 可见——已大半豁免（E6 + T-372 最小入口形态）。
5. 仓库/目录字段（同族复核）——见 B-2.4（原始归 minor：Repository Layout/Description/Created/Artifact Count 与目录 File URL 缺位无豁免条目）。
6. remote 表单无 Test 连通性（唯一 Test 在复制子表单）——R6 只锚了复制侧，remote 侧无条目。
7. 编辑表单 Save 无 dirty-gating——进入即可点、无变更亦可提交（Artifactory disabled until modified）。
8. 创建入口拓扑——单「＋ 添加仓库」→ /new 表单内 rclass 单选行 vs Add Repositories 下拉（Local/Remote/Virtual）预选 + 按 rclass 分路由、表单内无 rclass 控件。
9. 仓库列表列集——缺 Project/Environment/Shared With；Remote 页签缺 Replications 列（push-only 模型 ADR-0021/R10）；多出冗余「类型」列 + 已用/描述/操作（上游/成员为跨页签合并列）。
10. 仓库行操作超集——⧉ copy-key + Set Me Up + 部署（可部署型）+ 删除 vs 单 trash 图标——已豁免（L2 v1.1 + E1 更严确认）。
11. 表单 footer 多「重置」钮（创建/编辑均有）——M1 锚点仅 Cancel + Create/Save。
12. 表单概念级缺口——Environments 多选、Public/Internal 描述拆分、Force Authentication（virtual）、Suppress POM Consistency Checks（maven）。
13. 仓库详情中间页——行点击开 /:key（概要/配置/Replications），编辑在 /:key/edit vs 行点击直进编辑表单——parity §7 已标 ✅ 自有（附加层）。
14. 搜索行导航——整行可点 → /artifacts/…?focus= vs 仅 Artifact name 单元格深链（行体 inert）。
15. 日期格式——结果表 `2026-09-02 00:37:51`（无时区偏移）vs `02-09-26 08:37:57 +0800`；详情 General 显示原始 ISO（T/.000Z 未格式化）。
16. 快搜空历史——历史为空不渲染下拉（`AppShell.tsx:628`）vs 恒渲染「No recent searches yet」占位。
17. 结果计数一致性——BinFlow header/footer 恒一致 vs Artifactory 实测 header 计数滞后错位——明确不对齐缺陷（已豁免）。
18. 用户列表列——无 Realm、无 Last Login、无网格搜索框（「无端点列不伪造」；audit 已记登录，Last Login 可派生重开）。
19. 组/权限列表——列集差异（组多成员数/操作，权限多仓库数/patterns/用户数/组数）+ 行内「编辑」钮超集（Artifactory 行尾仅 trash，V4 实证无 ⋮ 无行内编辑）。

---

## C. 与既有裁定冲突点（立项稿交用户确认清单）

1. **cron 双轨（Q5）**——conductor 曾裁不引入（T-402a 勘误：事件驱动唯一引擎、Replicate Now 覆盖手动全量）；用户新指令覆盖面含 GC cron 字段、Cleanup 两族 cron、备份定时 CRUD、复制域用户级 cron。须明确「全做」是否含调度范式；若引入建议独立 ADR + 维护/调度专域。
2. **E1（§1.4 锁死）双冲突**——(a) Artifactory 行内 trash 直删 + 绿色 Delete 主按钮形态 vs E1 反向更严（输入 key 强确认 + 危险区 + 影响面摘要）：全做是否要求倒退 E1；(b) 内部矛盾：制品浏览 children 表每行红色「删除」钮已违反 E1 现行文本——须先修 E1 文本范围（管理列表 vs 浏览器表）或收約件 UI。
3. **E6（锁死：UI 中文 + 英文术语保真）vs「完全一致」**——语言是否在 parity 域内（Artifactory 全英文）。
4. **E2（加载更多 ×9 vs 页码控件）**——范式级翻转需用户定夺。
5. **E5 前提已被实测推翻**——V6 撤销决策 B 的依据「BinFlow 已用路由实体表单」不成立（用户/组创建实为内联卡）；E5 条目与现状矛盾，须重裁（改路由页 or 修登记）。
6. **§9 永不建 vs「全做」边界**——M14 已立防线「交互完全一致不解锁功能本体」（与 M13「行为逐项对齐不解锁 HA」同构）。洞察报表、漏洞合规 UI（xray 侧暂缓）、Builds/Federation/Lifecycles/Release Lifecycle/Repository Path Map 管理面是否进 M16：进则须先改 PRODUCT.md（Build-info/Release Bundle/AQL 六域随域），且建议专程里程碑不混编。
7. **权限动词子集豁免重开**——Annotate 是否加（连带后端动词域、前端矩阵列、write 是否拆 Deploy/Cache 两列、既有权限数据语义迁移）。
8. **表单域豁免缺口**——maxUniqueSnapshots/repoLayoutRef（M4 延期裁定）+ blackedOut/archiveBrowsingEnabled（无豁免条目）：补齐范围确认。
9. **BinFlow 自有增强层去留（Artifactory 无对位）**——仓库详情中间页、children 表复合、E4 测试器/diff、快搜范围页签缺位、重置钮：逐项裁「维持自有 or 收敛对齐」。
10. **条件票机制 vs 直接排期**——NuGet symbol（五承未触发）、remote 远端浏览 Q4 三出口（PM 倾向子集：helm index/docker tags 先行）、docker virtual Q6/Q7、Tokens 字段核验（候活体源）：M16 转正 or 续滚。
11. **webhook 57 型休眠裁剪**（ADR-0041 决策 7 翻转路径在案）——剔除 xray 域后仍有非 xray 触发源裁剪待裁。
12. **t381 事故残留**（VM 取证快照 15MB + 空仓；REST 删除被 OSS license 门挡）——conductor 决定项。
13. **「全做」语义总界定**——= 控制台交互 parity（推荐口径，B 清单域）还是产品域扩张（A1 域，须 PRODUCT.md 修订 + 专程里程碑 + ADR 群）。

---

## D. M16 骨架建议（主轴/副线）

**前置**：立项稿先落 C 表确认（C1/C2/C6/C13 四项定边界），再冻结范围；全程维持 clean-room 红线与 E1/E6 锁死项零倒退检查。

**主轴：控制台 full-parity 收口大程**（47 偏差 → 4 批次；web 为主、Go 后端小域配合；批次即非重叠分区）
- 批次① 制品树栈（P0 先行，tree-deep）：文件叶子进树（连带消除「（空）」症状 + children 表收窄决策）、选择≠展开语义、页签进 URL/文件路径段化、树头 facet/Sort-by/树视图切换；同步修 E1 文本范围与 reverse §3.2 facet 回填。
- 批次② 仓库管理表单栈：Basic/Advanced/Replications 三段结构、字段域补齐（layout/snapshot 上限/blackedOut/archiveBrowsing/Environments/描述拆分/Force Auth/Suppress POM/remote Test/dirty-gating/入口拓扑分路由/重置钮）、包类型 modal 880px + 8 个已实现包型开禁、列表列集（Remote Replications 列按裁定）。
- 批次③ 详情/搜索栈：页签序统一（权限在属性前）、File URL + 复制、Downloads/Last Downloaded 字段族（依赖统计基建）、Module ID、权限矩阵 Annotate、属性编辑解剖、下载形态、日期格式、搜索列集/行导航/快搜空历史占位。
- 批次④ 安全/shell 栈：用户/组创建路由表单化（E5 重裁联动）、权限编辑两步弹窗（Any Local/Any Remote/Any Distribution 预置）、用户能力位三旗、profile 自助 token/SSH key、监控 System Logs/Service Status、帮助下拉 + About、导航分组与侧栏过滤、（条件）GC/备份 cron 调度域。
- 后端配合小域（internal/httpapi + storage lane）：Annotate 动词与 write 拆分、per-node 下载计数（喂批次③ + 副线 usage）、Last Login 派生端点、（条件）cron 调度域。
- 出口件：parity 文档 v1.2（E5 前提修正、E1 范围修正、新增豁免/stay-out 登记、reverse §3.2 回填）+ 逐批 V 式活体复核。

**副线：AQL 高级面首程**（M15 既定 M16 第一顺位；视 lane 容量裁剪，与主轴后端小域共线）
- P0：statistics/usage 域 + /api/search/usage（复用主轴下载计数基建）。
- P1：QRL 全量版（三态 + 指标 job + REST 面）。
- P2 顺车：UI 搜索族（artifactsearch/stashResults/packagesSearch/syntax-search）、老搜索端点 dates/creation。

**条件小票池（非 DoD）**：NuGet symbol 六承、docker virtual Q6 开禁、E7/F1 toast 微调、L1 列选器推广（E-04 + 用户/组/权限 + Users 列补）、license 公钥 ADR、t381 残留清理。

**容量与让位**：Replay+outbox、virtual 同型全包型推广、Go/Terraform/GitLFS/AI-ML 包型、Cleanup/冷存储等继续滚 M17+（dev-go-core/httpapi 容量被主轴 Annotate/统计基建/cron 条件域占用）；HA/Xray/Build-info 族维持 PRODUCT.md 门（解禁走专程里程碑，不混编）。
