# M16 参照基线切换复核 — 7.84.10 → 7.161.20（活体 DOM 探测）

- role: qa-engineer ｜ 日期：2026-09-03 ｜ 实例：`http://127.0.0.1:8082`（admin/HTTP 直连；`/api/system/version` = **7.161.20 rev 86120900**，Enterprise Plus trial，addons 全开含 xray/curation/federated/release-bundle）
- 方法：Playwright（chromium 1.62.1，真实登录会话）只读 DOM 探测 + curl 纯读 REST 探针。**INC-1 纪律全程执行**：零写操作（无 Create/Save/Delete/Test/Run/PUT/POST-写；表单内填 Key/选包型均未提交，Cancel 退出）；onboarding 遮罩仅客户端 style 压制（零持久化偏好写）。
- 取证：截图 65 张 + DOM 转储 100 件 → `reports/agents/m16-baseline-evidence/`（`*.png` + `dumps/`）。
- 结论速览：7.161 UI 为新设计系统（el-plus + jf-steps + 微前端壳），**树工具带/快搜/帮助菜单/权限编辑器四处形态大改**；建仓表单三段结构与 T-439 设计一致；T-435 十五条中 **4 条已验证（V-l/V-m/C-a + V-k 反向关闭）、4 条部分、7 条维持待验证**。

---

## A. 7.84 → 7.161 形态差异清单（对 B 系 47 偏差逐项）

### A1. 全局壳（vs B-2.17/B-2.18）

| # | 事实（7.161 实测） | 对位偏差项影响 |
|---|---|---|
| A1-1 | 平台壳 = 顶栏 `platform｜administration` 双态切换 + 全局搜索 + 帮助 + 用户菜单；左轨为**应用切换轨**：Artifactory(→/ui/packages)/Packages/Builds/Artifacts/Release Lifecycle/Xray/Distribution/AI-ML/Pipelines/Integrations | B-2.18 重写素材：7.84 的「Welcome 分组 + 5 子项认证组」形态已不存在 |
| A1-2 | 管理面导航分组（实测路由表，见 §D）：All Projects Overview/Stages & Lifecycle/Repositories/**User Management**(Users·Groups·**Global Roles**·Permissions·Access Tokens)/Proxies/Authentication(→configuration/security/ldap 单链接)/Security/General Management/**Monitoring**(Storage·**Service Status**·Log Analytics·Federation Status·**System Logs**·Artifactory Logs)/Topology/Support Zone/**Artifactory Settings**(General·Artifactory Security·Packages·HTTP Settings·**Repository Imp/Exp·System Imp/Exp**·Repositories Layouts·Migration Tool·Property Sets·Maven Indexer·Config/Security Descriptor·**Maintenance·Backups**·**Retention Policies**·User Plugins) | B-2.18 分组重写；维护/备份挂「Artifactory Settings」组（非治理组）；Webhooks 条目在管理导航**未见**（疑移 Integrations——待后续腿确认） |
| A1-3 | **帮助菜单 = JFrog Documentation / JFrog Academy / Navigation Tour——About 弹窗与 Release Notes 已消失**；版本串驻左轨脚注（"Enterprise Plus trial license 7.161.20 rev 86120900 Licensed to TEST JFrog Ltd."） | **B-2.17 重写**：批次④「帮助下拉 + About」范围需修订——BinFlow 侧栏脚注版本形态反而更贴近新基线 |
| A1-4 | 用户菜单 = Quick Repository Creation / Set Me Up / Edit Profile / Logout | B-1.8 相关联证：Edit Profile 在用户菜单；Set Me Up 双挂（树头 + 用户菜单） |
| A1-5 | 管理态顶栏含 **Search Admin Resources** 过滤框 | B-2.18 该子项维持（位置=顶栏非侧栏） |
| A1-6 | 首装 onboarding 全屏遮罩（Get Started / Don't show me again） | 新增事实（BinFlow 无对位，无需动作，登记即可） |

### A2. 制品树（vs B-1.1~1.4/B-3.2/B-3.4/B-2.1~4）

| # | 事实 | 影响 |
|---|---|---|
| A2-1 | 树头工具带：`Filter By [Repository Name 输入框｜Package Types 多选下拉（Select All/Clear All）｜Repository Types 多选下拉｜My Favorites｜Clear] + Tree View: [Compacted｜Non-Compacted]`；另有独立 **Sort 下拉三键 = Repository Type / Repository Key / Package Type**；树头右侧 = Set Me Up + ⋯(Deploy (Upload)/Manage Repositories) + 计数文案 "Happily serving 0 artifacts" | **B-1.4 修正**：facet 由 7.84 复选组**演化为多选下拉**；T-434 已按 7.84 形态实现复选组——形态层需 ux 裁决（跟新基线改下拉 or 维持复选组留痕差异）。Sort 三键语义与 T-434 一致（「名称」↔「Repository Key」同义，标签差异登记） |
| A2-2 | Repository Types facet = **六态**：Local/Remote/Cache/Virtual/**Release Bundle/Federated**（+Select All/Clear All）；Package Types facet 选项含 Agent Packages/Agent Plugins/Bazel Modules 等新包型 | BinFlow 三态（K67 冻结口径）不伪造第四态的决策不受影响；两新态为 A1 非目标域（federated/release-bundle），登记不跟 |
| A2-3 | **Trash Can = 树节点**，URL `/ui/repos/tree/General/auto-trashcan`，选中出 item view（单 General 页签 + Info: Artifact Count / Size: Show） | **B-3.4 翻转为已对齐**：BinFlow 回收站树节点形态（T-372）即 7.161 基线形态，「大半豁免」可升级为「对齐」 |
| A2-4 | 树含**系统仓可见**：jfrog-support-bundle（Package Type: Support Bundle）、artifactory-build-info | 新增事实；BinFlow 系统仓不进树 = 待裁差异（低优） |
| A2-5 | URL 模型实测：`/ui/repos/tree/General/example-repo-local`（页签段+仓库段） | B-1.3 锚不变，T-434 实现方向正确（TAB 省略=general 规范形为 BinFlow 自有便利，登记口径维持） |
| A2-6 | 初始态 = 首仓自动选中出 item view | B-3.2 维持 |
| A2-7 | 仓级 item view 页签 = **General｜Effective Permissions｜Properties｜Followers**（权限在属性**前**）；General/Info 字段 = Name/Package Type/**Repository Path**/**File URL（带复制钮）**/**Repository Layout**/Description/**Created（`03-09-26 01:31:10 +0800`）**/**Artifact Count/Size: Show**/Virtual Repository Associations/Included Repositories | B-2.1（页签序）与 B-2.4（仓元数据字段族）**全部维持为偏差**——批次③ 素材不动；日期格式 B-3.15 再次实证 |
| A2-8 | 仓行上下文菜单含 Delete Content / Native Browser（DOM 在场证据） | B-3.1 侧补充素材 |
| A2-9 | 文件叶子：本实例仓全空**不可直接观察**（7.84 锚维持；T-434 方向不受影响） | 登记不可观察原因 |

### A3. 建仓表单（vs B-1.5/B-2.5/B-2.6/B-3.6~8/11/12）——**T-439/T-441/T-443 直接输入**

| # | 事实 | 影响 |
|---|---|---|
| A3-1 | 入口 = 单按钮 **Create a Repository**（el-dropdown）→ 五型带一句描述：Local/Remote/Virtual/**Release Bundle/Federated** → rclass 分路由 `/ui/admin/repositories/<rclass>/new`（local/remote/virtual 实测） | B-3.8 维持成立（下拉+分路由+表单内无 rclass 控件）；T-443 入口拓扑素材更新（按钮文案 + 描述行） |
| A3-2 | **步进条 `.jf-steps`：Basic｜Advanced｜Replications**（is-active 态）；步进切换被必填校验拦（Package Type/Repository Key 未填时错误「You must fill in this field」钉在 Basic） | **T-439 三段结构 + 复制配置第三步 = 与 7.161 完全一致，实现照旧**；补一条新锚：step 切换有必填前置门 |
| A3-3 | Local Basic 字段：Package Type / Repository Key（**0/64** 计数）/ Stage / General Settings: **Repository Layout**（=repoLayoutRef 落位 Basic）/ **Public Description｜Internal Description** 拆分 / Include·Exclude Patterns / JFrog Xray Integration（xray_tied 不跟） | T-439 字段域素材：描述拆分与 layout 落位实证 |
| A3-4 | Local Advanced = **Property Sets 双列**（默认选中 `artifactory` 集）+ Other Settings 四开关：**Priority Resolution / Disable Artifact Resolution（=blackedOut）/ Allow Artifact Content Browsing（=archiveBrowsingEnabled）/ Enable Redirect Download** | **B-1.5 四藏字段落位**：repoLayoutRef→Basic、blackedOut+archiveBrowsing→Advanced>Other Settings；maxUniqueSnapshots 在 Generic 下不可见（包型条件字段，预期 Maven 表单，本次未选 Maven——T-439 spec 腿需选 Maven 复核） |
| A3-5 | Local Replications = 引导文案 + **Enable Event Replication** 开关 + **Add Replication** + 表（URL/Sync Deletes/Sync Properties/Enabled）+ 空态文案 | 复制=第三步实证；T-439 迁载体设计正确 |
| A3-6 | footer = **Cancel｜Create Local Repository（初始 disabled=true，必填补齐后启用）**——**无重置钮**（用户表单与维护页**有** Reset：reset 为页面级差异化配置，repo 表单无） | **Q9 裁定获 7.161 实证**：T-439 移除重置钮 = 对齐；B-3.11 维持为偏差直至 T-439 落地 |
| A3-7 | 包类型选择 = **居中 el-dialog 924×760px**（7.84 审计锚 880px），tile=按钮+label-text；Local 全集 **41 型**（新增/更名：Hugging Face、Terraform/OpenTofu、Terraform Backend、Agent Packages、Agent Plugins、Machine Learning、Nix、Skills、OCI、Conda、CRAN）；Virtual 子集 ~33 型（P2 Eclipse 仅现于 virtual；Cargo/Conan/Opkg/Vagrant/Terraform Backend/Agent 族不在 virtual）；**全部可用、零禁用态**（enterprise license 下无 pro 锁） | **T-441 锚修订**：880px→**924px**（或「~880-924 居中弹窗」区间口径）；「8 tiles 开禁」是 BinFlow 自有门问题（对位形态=无禁用 tile，A4 条目方向不变）；13 tiles 维持 BinFlow 实有型口径不受影响 |
| A3-8 | Remote Basic = Package Type/Repository Key（**0/58**——64−6 恰为 `-cache` 后缀预留）/Stage/Include·Exclude/**URL/User Name/Password·Access Token/Enable Token Authentication/SSL·TLS Certificate/Test**（Test 在 **Basic** 段）/General: Repository Layout/**Remote Layout Mapping**/Public·Internal Description/Offline/Xray | **B-3.6 维持**：remote 侧 Test 在场且在 Basic——T-442 端点消费 + T-443 UI 落位素材；key 上限差异为 T-439 新登记细节 |
| A3-9 | Remote Advanced = 包型专属节（`Generic Settings`）含 **List Remote Artifacts**（= 7.84 "List Remote Folder Items" 更名）——远端浏览开关落位建仓 Advanced | **R-a 表单侧锚落袋**（remote-browsing.md §R-a 的建仓面证据补齐）；树呈现腿仍待语料（见 §B） |
| A3-10 | Virtual Basic = Package Type/Repository Key/Stage/Repository Layout/Public·Internal Description/Include·Exclude + footer Cancel｜Create Virtual Repository；**Force Authentication 未见于 Basic**（Advanced 段本次切换失败未取证） | B-3.12 素材部分更新；Force Auth 落位待补（T-439 票内核对） |
| A3-11 | 仓库列表 = 左轨 Recently Viewed/All Repositories/Inactive + Filters（Repository Type/Package Type/URL/Project/Stage/Other）+ Clear all；列 = **Repository Key/Repository Type/Package Type/Last Activity/Project**；系统仓 artifactory-build-info 在列 | B-3.9 更新：**Project 列实证**；Environment/Shared With/Replications 列未观察（视口右侧截断，未定论——T-443 票内再核）；「类型」冗余列在 7.161 也在（Repository Type 列为基线列，BinFlow 保留该列不再是偏差） |

### A4. 用户/组/权限/搜索（vs B-1.6/1.7/B-2.11~16/B-3.14/16/18）

| # | 事实 | 影响 |
|---|---|---|
| A4-1 | Users 列表：New user 入口；Filters（Admin/Status/Realm）+ Clear all；列 = **Name/Email/Realm/Groups/Admin/Status/Last Login**；footer `Total Users: 2` | **B-3.18 维持为偏差且方向实证**（Last Login/Realm 列在；A6「Last Login 可派生重开」获活体背书） |
| A4-2 | **New User = 路由整页 `/ui/admin/management/users/new`**；字段：Platform Auditor/Manage Webhook 管理位 + Options（**Can Update Profile / Disable UI Access / Disable Internal Password Login**）+ Password/Retype + MFA（Reset Google MFA Enrollment）+ Related Groups 双列（默认选中 readers）+ **User Permissions 矩阵：域页签 Repositories｜Builds｜Release Bundles × 五动词列 Read｜Deploy/Cache｜Delete｜Annotate｜Manage**；footer **Cancel｜Reset｜Save** | **B-1.6 维持**（五动词 + Deploy/Cache 拆分实证；Builds/Release Bundles 域页签为 A1 非目标域，BinFlow 只做 Repositories 域留痕）；**B-1.7 维持**（三能力旗实证）；**B-2.15 维持**（路由表单实证，E5 前提修正成立）；用户表单**有** Reset（与 repo 表单差异化——重置钮豁免口径须按页面分述） |
| A4-3 | Groups：New Group；Filters（Admin/Auto Join/External）；列 = Name/Permissions/External/Admin/Auto Join | B-2.15/B-3.19 素材更新 |
| A4-4 | **权限创建 = 路由页 `/ui/admin/management/permissions/create`**（Name / Resources〔**Add Repositories｜Add Builds｜Add Release Bundles**〕/ Users / Groups 分节 + footer Cancel｜Create）；**Add Repositories 开两步弹窗**：① Select Repositories（双列 + **Any Local/Any Remote/Any Distribution** 预置行 + 空态 No Items Selected）→ ② Set Patterns (Optional)（include/exclude）；弹窗 footer **Cancel｜OK**；列表列 = Permission Name/Users/Groups/**Any Remote** | **B-2.16 重写**：两步弹窗仍在但内嵌于「Add Repositories」子流；编辑器本体=路由页。批次④ 素材更新：预置三行 + 步② optional 实证 |
| A4-5 | 快搜 = 顶栏内联展开：收起 340px → 展开 **798px** 输入行 + 最近搜索卡 **740px**（**恒渲染 "No recent searches yet" 占位**）+ Submit Search + **Advanced Search** 两钮；7.84 全屏 overlay + Artifacts/Packages/Builds 范围页签形态**已消失** | **B-2.14 重写**：BinFlow 253px 紧凑输入+最近搜索下拉形态反而更近新基线；「范围页签缺位」注记可撤（7.161 亦无页签）；B-3.16（空历史占位）**维持为偏差且实证** |

### A5. 治理/存储/监控（vs B-1.9/10/11）

| # | 事实 | 影响 |
|---|---|---|
| A5-1 | Maintenance 页（`/ui/admin/artifactory/advanced/maintenance`）：**Garbage Collection（Cron Expression + Next Run Time + Run Now）** + Enable Quota Control（Warning/Limit Percentage）+ **Cleanup Unused Cached Artifacts（Cron+Next Run+Now）** + **Cleanup Virtual Repositories（Cron+Next Run+Now）** + Compress the Internal Database + Prune Unreferenced Data；footer **Reset｜Save** | **B-1.9 全项维持为偏差并获完整字段形态**（A3 翻案域素材）；cron+next-run UI 锚（cron-scheduling.md §4-4）活体实证 |
| A5-2 | Backups 页（`/ui/admin/artifactory/services/backups`）：**New Backup** 按钮 + 列表（Key/Repositories/**Cron Expression/Next Schedule Backup**/Enabled/Actions）；出厂两行：backup-daily `0 0 2 ? * MON-FRI`、backup-weekly `0 0 2 ? * SAT`（Next Run 显示为 Java Date.toString 形态 `Thu Sep 03 02:00:00 UTC 2026`） | **B-1.10 维持为偏差**；出厂 cron 两表达式活体钉死（cron-scheduling.md 出厂模板表互证）；import/export 两管理页在（Repository Imp/Exp + System Imp/Exp 路由） |
| A5-3 | Monitoring 组含 **Service Status + System Logs**（+Storage/Log Analytics/Federation Status/Artifactory Logs） | **B-1.11 维持为偏差**（监控组六页实证） |

---

## B. T-435 十五条待验证归位（7.161.20 活体）

| # | 项 | 结论 | 证据（可直接引用） |
|---|---|---|---|
| V-i | usage 端点排序键 | **维持待验证（语料缺）** | 实例仅 `jfrog-usage-logs` 1 个系统文件、example-repo-local 空（`items.find()`=1 行）；无下载语料，零写纪律不可造 |
| V-j | dates `to` 缺省语义 | 维持待验证（需 created 在未来 item，只读不可造） | — |
| V-k | dates `dateFields` 缺省集 | **验证关闭（反向）：无缺省集——缺省即 400** `'dateFields' parameter cannot be empty!`；补 `dateFields=created` 窄窗查询 → 404 `No results found.`（系统仓不进搜索面） | `curl /artifactory/api/search/dates?from=…&to=…`（无 dateFields）→ 400 逐字；**aql.md §14.3/§8.2「可选 CSV」表述需勘误为必填** |
| V-l | QRL REST 现值 | **已验证**：disabled 出厂态，`GET /artifactory/api/v1/system/query_rate_limiter/config` → HTTP 400 body 逐字 `Query rate limiter is disabled`（enterprise license 仍默认关——license 无关性一并实证） | curl 输出在案 |
| V-m | UI 搜索族挂载前缀 | **已验证 + 勘误**：真实树 = **`/artifactory/ui/<注册子路径>`（无 `/api` 段）**——规格推测 `/artifactory/ui/api/…` 需勘误；stashResults → 404 `Stash search results endpoint is disabled`（逐字节命中，企业档仍出厂关）；artifactsearch `GET pkg{type}` 小写型生效（nuget/npm/helm 选项集 JSON 已捕获，含 allowedComparators），docker/maven → 400 `{"error":"Unsupported package"}`；syntax-search 在挂（错 body → 400 errors envelope `An error has occurred while attempting to parse the sent JSON`，body 模型仍待对拍） | 全部 curl 输出在案；**aql.md §14.5 路径勘误 + T-452 挂载锚更新** |
| V-n | `downloaded_by` 非 admin 脱敏 | 维持待验证（无非 admin 凭据；造用户=写） | — |
| V-o | creation created-回显-fallback | 维持待验证（唯一语料 created==modified 不可分辨） | — |
| R-a | 远端浏览 UI 呈现 | **部分**：建仓面落袋——remote Advanced > `Generic Settings` 节内 **"List Remote Artifacts"**（7.84 "List Remote Folder Items" 更名）开关在场（截图 s8-remote-advanced.png）；**树呈现腿维持待验证**（实例无 remote 仓，建仓=写操作不可执行） | 表单侧证据可入 remote-browsing.md；树侧候 BinFlow T-448/T-461 e2e 自证 |
| R-b | simple/list 模式差异 | 维持待验证（同上） | — |
| R-c | virtual 树远端行去重排序 | 维持待验证（无 virtual+remote 语料） | — |
| R-d | deb/rpm 索引尺寸上限 | 不适用（实现侧压测腿，Artifactory 无公开对位） | — |
| C-a | `/ui/api/crontime` 现值 | **已验证（全）**：真实路径 **`/artifactory/ui/crontime?cron=<expr>[&isReplication=true]`**（规格拼法多 `/api` 段需勘误）；成功 `{"nextTime":"Wed Sep 02 20:00:00 UTC 2026"}`（Java Date.toString 逐字）；错误臂：空/缺参 → `{"error":"emptyCron"}` 400，三段式与 Unix 五段 → `{"error":"invalidCron"}` 400 | curl 全族输出在案；**cron-scheduling.md §4-3 路径与错误码族勘误落袋** |
| C-b | 复制 REST cronExp 400 两文案 | 维持待验证（PUT=写；INC-1 不可执行） | — |
| C-c | 5 分钟闸作用域 | **部分**：crontime 预览层实证 `* * * * * ?` 与 `0 * * * * ?` 在 **isReplication=true → 400 `{"feedbackMsg":{"error":"shortCron"},"nextTime":…}`**；同表达式**无 flag → 200**——「闸仅复制类」推断获预览层支持，保存层未证（需写） | 两臂 curl 对照在案 |
| C-d | 裸 `/N` 触发集 | **部分**：`0 0 /4 * * ?` 被接受，nextTime 自 17:45 UTC → **20:00 UTC**（下一个 4 的倍数钟点），与触发集 {0,4,…,20} 一致；单点 next-run 无法完备枚举集合（24 环绕假设仍按 Quartz 域上界 23 排除，置信高） | curl 在案；T-446 next-run 计算器测试臂维持 |

**归位小结**：已验证/关闭 4 条（V-l、V-m、C-a、V-k 反向）、部分 4 条（R-a/C-c/C-d + V-m 内 syntax-search body）、维持 7 条（V-i/j/n/o、R-b/c、C-b——阻塞原因均为「零写纪律 + 实例无语料/凭据」，非环境不可达）。三份规格（aql.md §14.5/§14.3、remote-browsing.md、cron-scheduling.md §4-3）各有一处**路径/必填性勘误**待回写。

---

## C. 对在途批次② 的即时修正建议（T-439/T-441/T-442/T-443）

1. **T-439（结构）——零修正，两处补强**：三段步进 + 复制第三步 + 描述拆分 + layout 落 Basic + 无重置钮 + 必填门（Create 初始 disabled）全部与 7.161 一致，照稿实现。补强①：**step 切换必填前置门**（未填 Package Type/Key 时 Advanced 不可达）为 7.161 新锚，建议纳入 T-439 spec；补强②：**remote Repository Key 上限 0/58（-cache 后缀预留）** vs local 0/64——若 BinFlow 键长校验统一 64，登记差异或跟进。
2. **T-439（字段域）——一处再核**：`maxUniqueSnapshots` 在 Generic 表单不可见，包型条件字段（预期 Maven）——spec 腿须**选 Maven 复核**（本次未选，诚实登记未证）。
3. **T-441（包类型弹窗）——锚修订**：居中弹窗 **924×760**（7.84 锚 880px）——建议锚口径改「居中 el-dialog ~880-924px」或直接 924px + parity 册 M1 行再修订留痕。tiles 无禁用态（enterprise 全开）佐证「8 型开禁=纯 BinFlow 自有门解除」方向；13 型维持实有型不伪造。**per-rclass 类型集差异**（virtual 无 Cargo/Conan/Opkg/Vagrant 等）为新增事实——BinFlow 单一类型集口径若要跟随需另立票，不建议本票夹带。
4. **T-442（remote Test + 远端浏览）**：**Test 按钮落位 Basic 段**（非 Advanced）——FR-143.5 消费面落位按此；远端浏览开关落位 **Advanced > `<包型> Settings` 节**，标签 **"List Remote Artifacts"**（R-a 表单侧锚可回填 remote-browsing.md）。
5. **T-443（列表/入口/dirty）**：入口按钮文案 **Create a Repository** + 下拉**带一句描述**；列表左轨 Recently Viewed/All Repositories/Inactive + 过滤器族（Repository Type/Package Type/URL/Project/Stage/Other）为 7.161 新形态——列集实证 Repository Key/Repository Type/Package Type/Last Activity/Project；Environment/Shared With/Replications 列**未定论**（视口截断），票内再核后再定列集裁剪。
6. **跨批次预警（不动作，供 conductor 排期）**：① B-2.14 快搜形态重写（overlay→内联展开）与 B-2.17 帮助菜单重写（About 消失）影响批次③④素材；② B-2.16 权限编辑器重写（路由页+Add Repositories 两步弹窗）影响批次④；③ B-3.4（Trash 树节点）可翻转为「已对齐」——parity 册 v1.2 出口件顺车更新；④ B-2.18 导航分组按 §D 路由表重写。

---

## D. 7.161 路由表（实测锚，供批次④ 与 e2e 对拍）

```
平台壳    /ui/ ｜平台↔administration 顶栏双态
应用轨    /ui/packages /ui/builds /ui/repos/tree /ui/artifactory/release-lifecycle
          /ui/security-and-compliance-teaser /ui/release-bundles /ui/teasers/ml_sh
          /ui/teaser-pipelines /ui/integrations
管理面    /ui/admin/projects/list · /ui/admin/stages · /ui/admin/repositories
          /ui/admin/management/{users,groups,global_roles,permissions}
          /ui/admin/management/users/new · /ui/admin/management/permissions/create
          /ui/admin/configuration/security/access_tokens · /ui/admin/proxies/configuration
          /ui/admin/configuration/security/ldap · /ui/admin/configuration/security/general
          /ui/admin/configuration/general
          /ui/admin/monitoring/{storage-summary,service-status,system_logs,federation-status}
          /ui/admin/artifactory/advanced/{log_analytics,system_logs,config_descriptor,security_descriptor,maintenance}
          /ui/admin/artifactory/services/{backups,indexer,retention/policies}
          /ui/admin/artifactory/configuration/{artifactory_general,reverse_proxy,property_sets,user_plugins}
          /ui/admin/artifactory/import_export/{repositories,system} · /ui/admin/repositories/layouts
          /ui/admin/artifactory/{security/general,config_packages,migration_tool}
建仓      /ui/admin/repositories/<rclass>/new（local/remote/virtual 实测；推测 release-bundles/federated）
制品树    /ui/repos/tree/<TAB>/<repo>[/<path>]（General 实测；auto-trashcan 为树节点）
REST      /artifactory/ui/crontime · /artifactory/ui/{artifactsearch,stashResults,syntax-search,…}（无 /api 段）
          /artifactory/api/v1/system/query_rate_limiter/config（v1 system 面）
```

## E. 自测与纪律记录

- 全部 UI 探测 = Playwright chromium 真实会话（登录 s0 → s8 共 8 腿 22 面）；全部 REST = curl 纯读（GET 为主；`POST /api/search/aql`、`POST /ui/syntax-search` 为语义只读查询且后者失败于 body 解析）。
- **零写执行清单**：未点任何 Create/Save/Delete/Confirm/Test/Run Now/「Don't show me again」；表单内填写（Key/包型）均未提交且 Cancel 退出；未创建任何用户/仓库/备份配置；onboarding 遮罩仅页面内 style 压制。
- 零 git 写、BOARD 零写、reverse-src 零接触、实例零配置变更（复核实况：`/api/repositories` 仍仅 example-repo-local，出厂态未动）。
- 局限（如实登记）：文件叶子/目录空态不可观察（仓全空）；virtual Advanced（Force Authentication）切换未成；repo 列表右缘列（Environment/Shared With/Replications）未定论；Webhooks 管理项去向未确认；syntax-search 正确 body 模型未对拍。五项均已给出补验路径。
