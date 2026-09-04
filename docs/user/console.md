---
title: Web 控制台使用指南
sidebar_position: 30
---

# Web 控制台使用指南

> 适用版本：M8（新信息架构：双模式壳 / 跨仓制品树 / 管理域五分组 / Set Me Up 与 Deploy 对话框族；设计规格 `docs/design/console-m8.md`）；**M9 增补**：Set Me Up 的 OIDC 重认证腿（T-260）、用户/组页的 Status 真值与删除面（T-257）、旧路径重定向窗口全量移除（T-263，见[旧路径 → 新路径](#旧路径--新路径m9-起不再重定向)）；**M15 增补**：搜索页 AQL 模式（T-419）、virtual 仓聚合浏览（T-416）、复制 ▶ Replicate Now 与 Test 连接/全局封锁（T-420/T-422）。
> 本篇全部 UI 路径与对话框行为在 HEAD（`89b27ce` 构建，含内嵌控制台）的 scratch 实例（127.0.0.1:18091，七仓种子覆盖全部五种包类型）上以 Playwright 走查验证（11/11 通过：双模式导航、树深链、对话框族、管理域路由、10 条旧路径重定向〔M8 兼容窗口；M9 起已移除，见下节〕）；登录/会话/CSRF 段沿用 M4 QA 基线（T-103/T-105，报告 `reports/agents/T-103-qa.md` / `T-105-qa.md`），M8 未改动服务端会话语义。浏览器矩阵依据 T-104 与 T-120 修复后的跨引擎复核。M9 增补面在 HEAD 构建的自起 scratch/armed 栈（2026-08-25）复验：shell 旧路径 19 条 404 断言、users-groups 6 腿、oidc-stepup 4 腿全绿。

M4 起单二进制自带 Web 控制台（go:embed，零外部依赖、断网可用）。**M8 起控制台的信息架构与操作流对齐 Artifactory**（同一个动作在同样的位置、走同样的步骤——从 Artifactory 迁移的用户零学习成本；逐任务的操作路径对照见 [Artifactory → BinFlow 操作路径对照表](artifactory-path-map.md)）。控制台仍是**管理面**——CI 与脚本继续走 REST/token，两者同一 API、同一权限模型。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 一个本地账号（默认 `admin` / `$ADMIN_PW`）。控制台**没有匿名模式**：匿名读只作用于制品内容路径，未登录一律先到登录页。
- 浏览器：**Chromium 系**（Chrome/Edge，QA 主矩阵）；WebKit（Safari 内核）与 Firefox 支持登录/制品树/上传链（见[浏览器兼容](#浏览器兼容)）。

## 登录与会话

浏览器打开 `$BASE/binflow/`——服务端 301 到控制台 `$BASE/binflow/ui/`（控制台是嵌入二进制的 SPA，无需单独部署）：

```bash
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' $BASE/binflow/
# 301 http://localhost:8080/binflow/ui/
```

登录（用户名 + 口令，无「记住我」——本地用户模型无邮件通道，改密入口在登录后的[编辑档案页](#监控与常规) `/profile`）：

- 错误凭据：行内红字「用户名或密码错误」；服务端 401 文案对「口令错」与「用户不存在」**完全相同**（不泄露用户存在性），并落 `login.failed` 审计。
- 成功：服务端签发会话 cookie，**登录落点是制品树 `/artifacts`**；会话过期重登后回跳 `return` 参数指定的原路由。

会话机制（服务端存储，curl/CLI 可完全等效操作）：

```bash
# 登录（JSON 或 form 均可）→ 200 {"username":"admin","admin":true} + Set-Cookie
curl -s -c jar.txt -X POST $BASE/binflow/api/v1/session \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<口令>"}'
# whoami（前端路由守卫同一端点）
curl -s -b jar.txt $BASE/binflow/api/v1/session        # 200 {"username":...,"admin":true}
# 登出 → 204，服务端吊销会话；同 cookie 重放 → 401
curl -s -b jar.txt -X DELETE $BASE/binflow/api/v1/session -o /dev/null -w '%{http_code}\n'   # 204
```

行为要点（均经 QA 实测）：

| 面 | 行为 |
|---|---|
| cookie | `binflow_session`；`HttpOnly; Path=/binflow; SameSite=Lax`；TLS 部署（`server.base_url` 为 https 或请求为 https）自动加 `Secure`，明文 HTTP 不加 |
| TTL | `console.session_ttl_hours`（默认 **24**）；`console.session_ttl_seconds` 覆盖键（测试粒度，同给时 seconds 胜） |
| **会话必死语义** | 滑动续期被绝对 TTL **封顶吞没**：会话必死于 `登录时刻 + TTL`，**与活跃度无关**——默认配置下活跃用户 24h 也必然掉线重登（T-110 塌缩定案）。不要按「一直操作就一直在」规划长任务 |
| 重启 | 会话落元数据库，实例重启**不掉线**（重启后 whoami 仍 200） |
| 日志 | session 值不进任何日志（签发/吊销记 info 级结构化日志，不含值） |
| 等价性 | 会话 cookie 与 Basic/Token **等权**：内容路径、npm packument、pypi simple、管理面同一认证。唯一例外是 docker `/v2` 面——cookie `Path=/binflow` 作用域结构性不含根级 `/v2`（见 [FAQ](faq.md#docker-login-为什么不走控制台的会话)） |

**CSRF 防线（Origin 同源校验）**：会话 cookie 认证的**非 GET/HEAD** 写请求，携带 `Origin` 且非同源 → **403**；无 Origin（curl/CI）或同源放行。Basic/Token/Bearer 认证天然免疫——**CI 与脚本零感知**：

```bash
# cookie + 跨站 Origin 的写 → 403（E-01 信封）
curl -s -b jar.txt -X PUT $BASE/binflow/generic-local/a/f.txt \
  --data-binary @f.txt -H 'Origin: http://evil.example' -o /dev/null -w '%{http_code}\n'   # 403
# 同一请求去掉 Origin → 正常写入（201，仓库存在时）
```

## 双模式导航（M8 新信息架构）

控制台侧栏按上下文切换**两种模式**（对齐 Artifactory 的应用/管理双侧栏）：

| 模式 | 路由前缀 | 侧栏分组 | 落地页 |
|---|---|---|---|
| 应用模式 | `/artifacts` `/dashboard` `/search` `/profile` | **应用**：仪表盘、制品 | `/artifacts`（跨仓制品树） |
| 管理模式 | `/admin/**` | **仓库 / 用户与权限 / 治理 / 监控 / 常规** 五分组 | `/admin/repositories` |

- **模式切换**：侧栏底部常驻切换项——应用模式显示「管理」（admin / readonly_admin 可见），管理模式显示「返回应用」。URL 进 `/admin/**` 即自动渲染管理模式侧栏。
- **管理页带面包屑**：顶栏显示层级（如 `仓库 / maven-remote / 编辑`），逐级可回退。
- **版本行**：侧栏底部固定 `BinFlow v<version> · 单二进制制品仓库`（版本来自 `/api/system/version`）。
- **全局搜索**：顶栏搜索框（placeholder「搜索制品」），`⌘K` / `Ctrl+K` / `/`（非输入态）快捷键直达；Enter 进 `/search`。聚焦展开**最近搜索下拉**（localStorage 最近 8 条、去重置顶）——空历史给「暂无最近搜索」占位（恒渲染下拉本体），子串无匹配给无匹配提示；`清除` 清空历史（零历史不渲染清除钮）。Esc 两段：先收下拉、再清输入。
- **空实例引导**：仓库数为 0 时制品页内嵌引导卡「创建仓库」+「跳过」（跳过状态存浏览器 localStorage，不设独立路由）。

管理模式五分组 12 条目全图：

```
仓库
└ 仓库                /admin/repositories（Local / Remote / Virtual 三 Tab）
用户与权限
├ 用户                /admin/security/users
├ 组                  /admin/security/groups
├ 权限                /admin/security/permissions
└ Access Tokens       /admin/security/tokens（M14 真身页，见下文）
治理
├ 审计日志            /admin/governance/audit
├ 维护（GC）          /admin/governance/gc
├ 配额                /admin/governance/quotas
├ 复制                /admin/governance/replication
├ 备份 / 恢复         /admin/governance/backup
├ 回收站              /admin/governance/trash（M12）
└ Webhooks            /admin/governance/webhooks（M13）
监控
└ 存储                /admin/monitoring/storage
常规
└ 系统信息            /admin/general/settings
```

## 制品树浏览器（`/artifacts`）

跨仓树是应用模式的落地页与核心页：**全部仓库出现在一棵树上**，仓库是顶层节点（图标区分仓型与包类型），左侧树 + 右侧详情面板联动。

**页头工具栏**（左→右）：`Set Me Up`（客户端接入向导，见下节）· `部署 Deploy`（浏览器上传）· `管理仓库→`（跳仓库管理）。

**树头工具带**（树区顶部三行带，固定不随树滚动）：

| 控件 | 行为 |
|---|---|
| 过滤仓库 | 输入框前端过滤已加载的仓库清单（原页头工具栏位迁入），`清除` 复位 |
| 包类型 facet | 复选组，选项集 = 已加载仓库的**实有**包类型——勾选仅显示命中仓 |
| 仓型复选 | Local / Remote / Virtual 三复选（BinFlow 实有三态；Artifactory 的 Cache 位是 remote 缓存子集视图，不单列） |
| Sort-by | 名称 / 包类型 / 仓库类型三序 |
| Compacted | Compacted / Non-Compacted 单选——紧凑档收窄行高（大树密度场景） |
| My Favorites | 仅显示收藏仓（浏览器 localStorage 持久）；**收藏标记入口 = 仓库节点右键菜单** |

树交互：

- **文件是树叶子**：目录与文件同树呈现（同层目录在前）——点文件叶子直接出文件详情，「仅含文件的目录」不再是「（空）」。
- **懒加载一层**：点开节点才拉取下一层（10k 节点级目录树仍流畅——展开仅渲染可视区）。
- **选择 ≠ 展开**：单击只做选中（右侧详情联动），不展开节点；展开由箭头 / 键盘 `→` / 深链祖先链驱动。
- **URL 即状态**：`/artifacts/[<页签>/]<repo>/<path…>`——页签段 ∈ `general`（省略即默认）/ `properties` / `permissions`，文件 = 路径末段。可以直接分享或收藏**深链**，打开时自动展开祖先并滚动定位到节点；旧 `?focus=` 形与旧多段形打开时**一次性折入**路径段规范形。
- **详情右联**：单击节点，右侧面板即时展示详情；Enter 进入下一层。
- **右键菜单**（`Shift+F10` / Menu 键键盘可达）按对象三形态：
  - 文件：复制路径 / 下载 / 删除
  - 文件夹：复制路径 / 删除 / 刷新
  - 仓库：复制仓库路径 / 刷新 / 在仓库管理中打开 / 收藏（My Favorites 标记位）
- 当前层 children 表（名称/类型/大小/修改时间/操作者）支持「过滤当前层」与「只看文件」；大目录客户端增量「加载更多」（统一分页控件的分治豁免面——大树滚动场景保持增量形态），超过 2000 条提示改用[搜索](#搜索与仪表盘)。**行内操作列已收敛**：children 表不再有详情/下载/删除三钮——删除走详情面板与右键菜单（均过危险确认）。
- **仓型面（M14/M15）**：local 仓直列内容；**remote 仓只列已缓存制品**（浏览永不回源——空目录提示「远程仓库：仅展示已缓存的制品」，与 Artifactory 的 remote-cache FolderInfo 同口径；回源拉取走包管理器协议面）；**virtual 仓聚合浏览 M15 起可用**（children = 成员仓并集，树动态展开/深链与 local 同形；成员全空时空态卡点名成员清单；virtual 仓不经手删除——删除入口按服务端 405 预收敛不渲染）。
- **跨路径 Move/Copy 不做树内入口**（REST 面自 M12 起可用——[制品操作族](admin/artifact-operations.md)）；**删除先入回收站**（pro 槽 `trashcan`，社区档为硬删——治理页 [回收站](#治理admingovernance) 可浏览/恢复）。

详情面板（Tab 式，渲染序 `常规` → `有效权限`（admin 渲染）→ `属性`；页签进 URL 段，非默认页签深链直达）：

- 仓库/文件夹/文件各有字段集；路径与 URL 一律 mono + 拷贝按钮。
- **File URL**（仓库 / 目录 / 文件三形态齐备）：内容面绝对 URL + 复制钮——与「下载」钮同一构造，粘给 curl 即可用。
- **文件形态字段序**：Name → Repository Path → File URL → 部署者 → 大小 → Created → 修改时间 → **下载族**——`Downloads` / `Last Downloaded By` / `Last Downloaded` / `Remote Downloads` 四行，数据源 [`?stats` 面](api-reference.md)（本地计数实时；`Remote Downloads` 对 local 仓恒 0）。从未下载、或服务端对低档位省略下载者身份时，该行如实显示「—」；**打开详情面板本身也计入下载计数**（存储面节点读取递增计数器——把 `Downloads` 读作「被访问次数」，不要当精确的人工下载统计）。
- **文件下载形态**：单 24px 图标钮（浏览器原生落盘）+ 伴随菜单（▾）——菜单内承载 `下载并校验`（浏览器实测 sha256 与服务端对账，结果就地呈现 + toast）、`mimeType` 行与 **Checksums** 区（sha256/sha1/md5 截断呈现、拷贝不截断、各带「（上传时提供：一致 ✓）」徽标——映射 originalChecksums 比对）。常规页不再平铺校验块；校验下载完成后下载计数随任务完成联动刷新。docker manifest digest 行的 **tag 徽标**维持（树表对 docker 仓显示「标签/摘要」列替代「类型/sha256」）。
- **仓库形态字段族**：Name → Package Type → Repository Path → File URL → Repository Layout（`—` 预留位——BinFlow 布局由协议固定，无自定义 layout 引擎）→ Description → Created（`—`——创建时间未投影到该查询面）→ Artifact Count / Size（usage counts 面，count 与 size 单请求带回）。
- 目录形态：直系概要（子项目录/文件计数 + 直系文件大小合计）。
- **属性页签**（编辑解剖见[属性系统](properties.md)）：常显 Property/Value 表单 + `Add Property` 钮（同名键 Add = 该键值集整体替换、兄弟键保留）；网格 Search 键/值子串过滤（大小写不敏感，无匹配有提示块）；行内删除走**危险确认对话框**（可拒绝）。
- 权限过滤自然生效：无 read 权限的子树不可见；操作中 403 行内呈现并指向权限模型。
- 协议深度特化视图（npm 包目录、pypi 归一名视图、maven `maven-metadata.xml` 只读面板）为登记后续项——当前统一按路径树呈现。

## Set Me Up：客户端接入向导

三入口：制品树页头 `Set Me Up` 按钮（选中仓库预选）· 仓库管理列表行操作 · 仓库详情页头。对话框流程：

1. **包类型网格**「选择客户端类型」：只列实例内**已有仓库的包类型**并集（五种：Generic / Docker / Maven / npm / PyPI）；零仓库 → 空态提示先建仓。从仓库上下文进入则跳过网格直达主对话框。
2. **主对话框**「配置 `<PackageType>` 客户端」：仓库下拉（预选当前仓，只列该包类型仓）+ `配置 Configure` / `部署 Deploy` 两个 Tab——
   - **配置**：解析/拉取侧指令（docker login+pull、settings.xml、pom repositories、`.npmrc`、pip.conf、curl 下载校验）；**部署**：发布侧指令（curl -T、docker build/push、mvn deploy、npm publish、.pypirc+twine）。每块独立 Copy；内容与 docs/user 各[接入指南](integrations/npm.md)同源（UI 不发明命令）。
   - 凭据位：铸币前显示 `<USERNAME>` / `<TOKEN 或口令>` 占位；生成令牌成功后自动回填。
3. **生成令牌**（控制台铸币位）——非 admin 会话按 whoami 的 `source` 自动分腿：
   - **admin 会话**：直接点「生成令牌并创建指引」——管理员臂免二次口令（ADR-0027 决策 1，界面有说明行）。
   - **本地 / LDAP 用户**（`source=local|ldap`，M8 起）：生成区含**口令框**；`auth.token_step_up` 开启时，无凭据请求被服务端 401 `step_up_required` 拒绝 → 对话框内联口令重验表单（自动聚焦）；口令错误 401 `step_up_invalid` → 内联错误（服务端原文，不出第二层对话框）；正确口令续铸成功。
   - **SSO 用户**（`source=oidc`，M9 起）：**不出口令框**——401 `step_up_required` 时改出「重新认证并继续」引导面板，点击后全页跳转 `/binflow/api/v1/oidc/login?purpose=step_up`（IdP 强制重认证）；回跳的 `#step_up_grant=` fragment 由控制台在应用引导期消费（URL 即抹除，不进历史与日志），Set Me Up 自动重开并自动续铸；grant 单次即焚，失败内联 `error_description` 原文 + 重新认证入口。IdP 侧取消/中断的半途流程在下次打开对话框时给出「等待重认证完成」提示。语义全解见 [Token 铸造二次认证（step-up）](admin/token-step-up.md)。
   - 成功 → **「令牌已生成」一次性面板**：token 明文（mono + Copy + 「关闭后不可再查看」提示）+ 24 小时过期与 token_id 说明。签发走 `POST /api/security/token`（默认 `expires_in=86400`）。

## Deploy：浏览器上传对话框

入口与 Set Me Up 对称（树页头 / 仓库列表行 / 仓库详情页头）。字段序：目标仓库（下拉，**只列 local 的 Generic / Maven 仓**）→ 包类型（只读回显）→ 部署模式（单个/多个）→ 拖拽区（`拖拽文件到此处` 或点击选择）→ 目标路径（mono 可编辑）→ **`部署` 显式提交**（文件入队后需点「部署」才启动哈希+上传；关闭对话框即中止排队与在飞上传）。

- 浏览器端流式计算 sha256 与服务端返回值比对，成功显示**一致徽标**；409 双值（received/actual）/ 403 权限指引 / 413 quota 文案**原样呈现**。
- maven 仓按 **GAV 表单**生成 layout 路径（五输入实时预览 + 前端预检）。
- **docker / npm / pypi 仓不出现上传入口**（协议发布是客户端会话，UI 以接入命令块替代）。
- **特殊字符安全**（M8 修复）：目标路径同时显示原值（可编辑）与**请求编码值只读回显**（`%`→`%25`、空格→`%20`、中文→UTF-8 percent 编码）——含 `%` `#` `?` 空格中文的路径不再因二次编码踩坑（T-231 修复矩阵）。

## 管理模式各域

### 仓库（`/admin/repositories`）

- **三 Tab 列表**：`/admin/repositories/{local|remote|virtual}` 子路由；「N 个仓库」计数 + 右上 `+ 添加仓库` 下拉（**Create a Repository 三预选**：Local / Remote / Virtual，每项 = 型名 + 一句描述——按型直达建仓分路由）；列头排序（key / 包类型）+ 行尾删除入口；每行 Set Me Up / Deploy 快捷钮。**Replications 列覆盖 local 与 remote 两 Tab**（每仓复制配置计数；表头注记 push-only 口径——无 pull 复制，remote 页签如实呈现以该仓为源的 push 配置；virtual Tab 无此列）。local Tab **▶ Run = 真触发 Replicate Now**（对本仓逐启用配置 POST run，toast 回报排程数〔0 = 空跑如实说明〕+「查看任务」深链复制页；全部停用则按钮禁用——REST 语义见[治理指南 · Replicate Now](admin/governance.md#replicate-now手动全量同步m15t-420)）。
- **建仓向导（分路由 + 三段步进）**：入口 `+ 添加仓库` → 三预选直达 `/admin/repositories/{local|remote|virtual}/new`（仓型由路由预选——表单内不再有仓型单选；旧 `/new?rclass=` 深链兼容映射一跳，既有跨页入口零改动）。进页弹**包类型网格**（924px 居中，13 型磁贴全量呈现——五核心型 + 八进阶型〔带 `pro` 档位徽章〕；磁贴恒可选，**档位门由服务端终裁**：community 档提交进阶型 → 400 行内回显 `package type not available on this instance...`，表单页不跳走）→ **三段步进表单**：
  - `Basic`：常规（key / 描述 / 包型锁定回显）+ 来源（remote 的上游 URL 与凭据）或成员（virtual 成员清单）；
  - `Advanced`：策略（包型专属策略键）/ 治理（local 的配额与 patterns）/ 高级（预留位族——Repository Layout / Environments / Blacked Out 等恒禁用 + 如实标注，**提交体零携带**）；
  - `Replications`：仅**编辑态 local 仓**有第三段（建仓态两段——仓尚不存在，复制配置源仓必 400）；`?section=replications` 深链直落第三步；
  - 页脚 `Cancel` + `Create`（无重置钮）；非活跃步整步卸载。
  - key 规则 `[a-z][a-z0-9-]{1,62}` 前端预检、服务端终裁（400 行内回显）。**保留字 `api` / `v2` / `docs` / `console` / `ui` / `assets` 建仓即 400**。
- **仓库详情** `/admin/repositories/:key`：概要 / 接入命令（与接入文档同源）/ 统计（配额水位条）/ 配置（配额行内编辑 + patterns；**manage 持有者**亦可编辑本仓配置——见 [RBAC 指南](admin/rbac-roles.md)）/ Replications（M14：本仓复制配置摘要卡 + 深链编辑节 + 全局复制页入口）Tab + 危险区（删仓仅全量 admin 可见）。
- **编辑** `/admin/repositories/:key/edit`：rclass/包类型锁定，其余字段同建仓表单（三段步进同形）。**dirty-gating**：进入时 Save 禁置，表单与打开时回显**逐字段深度比对**——有实质变更才解禁（改回原值重新禁置；密码字段输入即视为变更）；干净态点不动、零写请求。**remote 仓「测试连接」钮**（仅编辑态在场——探测端点按已存仓 key 寻址，建仓态给说明行不给死按钮）：草稿探测按表单与已存配置的 diff 决定凭据形态——**带了密码 = 用表单明文凭据探测**；只改了 URL/用户名没填密码 = 按匿名探测（已存密封密钥**绝不**静默发往改动后的候选主机）；零改动 = 探已存配置。判定内联呈现（绿/红 + 上游状态码；连接层失败 = 「未触达上游」）；探测零副作用（不写任何配置）。**编辑态 local 仓另有 Replications 节**（push 复制配置：列表 + 新建/编辑表单 + 行内启停开关 + 输入 name 强确认删除；表单带「测试连接」按钮——创建态测草稿、编辑态未改动时探已存配置；Artifactory 的 cron/sync 等字段为预留位恒禁用——如实标注引擎尚不支持）；编辑保存 = 删除 + 重建（未决任务级联清空、目标口令不回显需重输——留空即匿名目标）。remote/virtual 仓不适用（push 源是 local）。REST 语义见[治理指南 · 复制](admin/governance.md#复制push-replication)。
- **删除**：两段强确认——非空仓必须勾选 `同时删除内容` + **输入 repo key 确认**（不勾选直接删非空仓会被服务端 400 拒绝）。
- 治理字段（仅 local 仓）：`quotaBytes` 与 `includesPattern` / `excludesPattern`（详见[治理指南](admin/governance.md)）。

### 用户与权限（`/admin/security/*`）

- **用户**：列表（Name/Email/Groups/Role/**Status**）——列表为**单请求**数据源（`GET /api/security/users` 一条已含 email/adminRole/enabled/groups，无逐用户扇出），Status 列徽章（启用/禁用）真值即服务端 `enabled` 回显。**创建与编辑均为路由整页表单**（列表内联卡已退役）：创建 `/admin/security/users/new`（深链直达），编辑 `/admin/security/users/:name`——四节结构（用户设置〔含**角色三值下拉** `user/readonly_admin/admin`，仅 admin 可改〕/ 选项 / 口令 / 相关组双列穿梭 + 权限矩阵）。创建页含 **Retype Password 双录**（两次不一致挡提交）；页脚 `Cancel` | `Reset` | `Save`——Reset 与 Save **初始禁置**（Reset 以打开时回显为基线，未改过不可复位；Save 叠加必填门）。readonly_admin 深链进创建页 = 全控件禁用 + 说明行（服务端 403 终裁）。角色语义见 [RBAC 角色与仓库级管理员](admin/rbac-roles.md)。
- **删除用户**（M9 起，列表行按钮 + 编辑页危险区双入口）：**输入用户名强确认**（逐字匹配才解禁）+ 不可恢复级联文案（组员/授权/token/会话同事务删除、审计保留、**重复删除 404 非幂等**）。自删与内置 admin 行 UI 预禁用并述因；last-admin 与 404（已被他人删）不预判，服务端原文如实呈现。REST 语义与三护栏见[治理指南 · 删除用户](admin/governance.md#删除用户m9-起)。
- **能力位三旗（预留位）**：用户表单选项节的 `Can Update Profile` / `Disable UI Access` / `Disable Internal Password` 三开关当前为**恒禁用预留位**——服务端尚未承接这三域（提交体零携带；表单显示的是服务端回显的出厂档：profile 可更新、其余关）。hint 注记两臂语义：Disable UI Access = 仅拒 UI 登录（API/Token 不受影响）；Disable Internal Password = 内部口令停用、认证走外部 IdP。承接落地后控件转正。
- **组**：列表 + **路由表单**（创建 `/admin/security/groups/new`、编辑 `/admin/security/groups/:name/edit`——列表内联编辑卡已退役；组设置 + 成员穿梭 + 组权限矩阵；页脚 `Cancel` | `Reset` | `Save` 双初始禁置同用户表单）。成员计数/花名册由用户列表**客户端过滤**推导（单请求全量新鲜）；编辑页打开时按需取 `GET /api/security/groups/{name}?includeUsers=true`（恰一次，用户/组列表不重拉），穿梭两侧与用户页 groups 列同源（user_groups 行的两视图）。组成员写侧仍走逐用户组集替换（组侧写端点未开）。
- **权限 target**：列表 → 单页分区编辑器（名称 / 资源 / 用户 / 组）+ **两步资源对话框**（`编辑仓库…` → ① 选仓库 → ② 可选 include/exclude patterns）+ 动作矩阵 + **模式测试器**（输入路径即时显示逐条命中与最终判定）+ 保存前 diff 确认。动作动词在 REST 面是**五值闭集** `read / deploy-cache / annotate / delete / manage`（`write` 仍被接受为 `deploy-cache` 的兼容别名，回显恒正名单形——语义见[用户组与权限管理](admin/groups-permissions.md#动作动词read--deploy-cache--annotate--delete--manage)）；编辑器矩阵随五值化呈现更新中。完整操作与 curl 对账见[用户组与权限管理](admin/groups-permissions.md)。仓库级管理员（manage 持有者）经 `GET /api/v1/permissions?filter=manage` 可达本编辑器（覆盖集内 target，见 [RBAC 指南](admin/rbac-roles.md#manage-能做什么--不能做什么)）。
- **Access Tokens**（M14 真身页，T-386）：**生成令牌** modal（有效期档 1h/24h（缺省）/7d/30d/365d，**永不过期仅 admin**；admin 可选签发对象代人签发；scope 恒 `api:*` 只读说明——服务端不收窄权限域）→ 一次性明文面板（mono + 拷贝 + 「关闭后不可再查看」）；**会话台账**（token_id/指纹/主体/有效期/状态/操作——服务端只存指纹、无令牌清单端点，刷新即空，历史令牌按 token_id 吊销对账走审计日志）；吊销双出口（行内 danger 确认 + admin 专属按 token_id 吊销盒）。**铸币门 = 任何已认证会话**（Q11：与 Set Me Up 同端点同门，非 admin 仅能自铸、有效期有限——本页在管理模式，普通 user 直链为无权限卡，其日常铸币走 Set Me Up）；readonly_admin 吊销禁用、自铸不受限；OIDC 会话的 step-up 重认证走 Set Me Up 链。

### 治理（`/admin/governance/*`）

- **审计日志**（`/admin/governance/audit`）：时间窗/操作者/仓库/动作/路径过滤 + **keyset 游标链页窗**（统一分页控件：前沿逐页推进、向后直跳已缓存页；「加载更多」增量追加形态已退役——列表不再累积，path 过滤口径 = 本页窗口）；动作值原样 mono 显示（不翻译）。词表见[治理指南](admin/governance.md#审计)。
- **维护（GC）**（`/admin/governance/gc`）：GC 状态 + dry-run 结果面板 + apply **输入实例名二次确认**；存储迁移进度面板同页。
- **配额**（`/admin/governance/quotas`）：每仓 used/quota 水位条（80% 黄 / 100% 红）+ 行内编辑上限。
- **复制**（`/admin/governance/replication`）：复制目标表 + 最近事件（10s 轮询）；**M15 起页头新增全局封锁卡**（blockPush/blockPull 两方向独立 Switch——「无论配置如何都不触发」的应急刹车，与 `binflow.yaml`/REST 三面同源；readonly_admin 只读呈现）。M14 起配置 CRUD 另入[仓库编辑页 Replications 节](#仓库adminrepositories)，本页保持全局观测视角；REST 与引擎语义见[治理指南 · 复制](admin/governance.md#复制push-replication)。
- **Webhooks**（`/admin/governance/webhooks`，M13）：订阅列表（行内启停/试发/编辑/删除）+ 新建/编辑对话框（13 域分组事件型选择，休眠型灰显如实标注）+ 详情抽屉（最近投递记录——状态/耗时/重试计数/载荷快照）。写动词 pro 槽 `webhook`；readonly_admin 只读臂（无新建钮、写动作禁用）。REST 语义与接收端配方见 [Webhook 使用指南](admin/webhooks.md)。
- **备份 / 恢复**（`/admin/governance/backup`）：CLI 引导卡（export/import 命令与警示，一键复制）——备份恢复是**高危带外操作**，不做进度 UI；完整链见[备份与恢复手册](admin/backup-restore.md)。
- **回收站**（`/admin/governance/trash`，M12）：`auto-trashcan` 内置仓的浏览/恢复/清空面（槽 `trashcan` 门控态呈现）。捕获/保留期语义见 [Trash can 管理](admin/trash-can.md)。

### 监控与常规

- **存储**（`/admin/monitoring/storage`，M8 新页）：刷新行 + 汇总卡（blob/制品大小与计数、优化率）+ 逐仓用量表（TOTAL 首行）。
- **系统信息**（`/admin/general/settings`）：实例信息（版本/修订/产品，开放端点）+ 健康卡（`/api/v1/health` 子系统状态，承载 Artifactory Service Status 的语义）。匿名读开关、数据目录、日志级别等**无查询端点，页面不展示**——配置以 `binflow.yaml` 为准。
- **编辑档案**（`/profile`，应用模式）：修改口令 + API Token 使用说明。M7 版设置页已拆分为系统信息（管理域）与编辑档案（应用域）两页。

### 搜索与仪表盘

- **搜索**（`/search`）：**双模式**——`基本`即名称子串检索（`GET /api/search/artifact`，结果按调用者权限过滤）；`AQL` 模式切换后出 mono 查询编辑器（⌘/Ctrl+Enter 执行，工具行明示子集边界：items + property + statistics 域、操作符清单、未支持域 400）。两模式共用**同一张结果网格**：
  - **列集**：选择列（固定）+ `制品`（name 链接）/ `路径` / `仓库` / `修改时间` 默认五列；`大小` / `sha256` 是列选器可选项（默认不呈现）；「恢复默认列」≠ 全显。
  - **查询面 = 顶栏驻留输入**：页内关键词/仓库过滤表单已退役——顶栏输入 Enter 即 `/search?q=`；`?repos=` 深链参数退役（旧链仍按 `?q=` 查询，仅丢仓库窄化，不猝死）。
  - **网格快滤**：结果表上方 name/dir/repo 三域子串客户端窄化（全量已取回，客户端窄化即全量语义）+ 命中计数 + 无匹配占位；与 AQL 编辑器两层正交（编辑器管服务端查询、快滤管已取回行）。
  - **行导航**：深链唯一载体 = 制品行 name 单元格链接（`/artifacts/<repo>/<path>` 路径段形直达树定位）；行体其余部分不可点。选择列支持全选（作用于快滤后可见行）与「复制路径（N）」批量拷贝——无批量删除/下载入口（无对应端点，不伪造）。
  - **日期格式**：`dd-MM-yy HH:mm:ss +ZZZZ`（浏览器本地时区 + 显式偏移）。
  - AQL 模式：**排序 = 表头注入/翻转 `.sort()` 段、分页 = 页码改写 `.offset()`、每页行数改写 `.limit()`**（换页大小清 offset 回第 1 页——查询文本是唯一事实源）；400 语法错**逐字内联**呈现（含未支持域点名），408/429 分流人话提示，截断通告按官方文案呈现并给分页指引。语言与错误对照见 [AQL 搜索指南](aql.md)。checksum 精确反查暂未接 UI（页面引导走 REST）。
  - **分页**：统一分页控件——页码序列（当前页高亮）+ 首/上一页/下一页/末页四钮 + 每页行数档 **[20, 50, 100, 200, 1000]**（缺省 100）；边界态禁置不隐藏（首页禁 first/prev、末页禁 next/last、单页全量全链禁置）。
- **仪表盘**（`/dashboard`）：健康/存储/仓库/最近审计卡片，各自独立加载。remote 上游健康统计端点未开放，仓库卡仅列 remote 计数（上游状态显示 `—`）——remote 仓的 assumed-offline 细节看仓库列表/详情页。

## 角色可见性

| 角色 | 可见 |
|---|---|
| admin | 全部入口与数据面（应用 + 管理两模式） |
| readonly_admin | 应用模式 + **管理模式全部读面**（五分组 12 条目可见）；一切写入口禁用 + 「服务端 403 兜底」说明；Set Me Up 铸币走 step-up（见[step-up 指南](admin/token-step-up.md)） |
| 非 admin 已登录（`user` 角色） | 仪表盘（实例卡 + 收敛说明）、制品树与搜索（按自身路径 ACL）、编辑档案。「管理」入口**不渲染**；`/admin/**` 直链 = 页面壳 + 无权限卡。仓库清单是管理面——树区顶层显示无权限卡 + 搜索/直链引导，但**已知 repo key 的深链与搜索结果按自身路径 ACL 可达** |
| 未登录 | 无（重定向 `/login?return=`） |

401 与 403 在界面上分流：401 一律「登录已过期」toast + 重定向登录页；403 按层级收敛（导航/按钮不渲染、页面级无权限卡、卡片级隐藏、写入口不渲染）——403 只表达权限，不用于表达「功能不存在」。

## 旧路径 → 新路径（M9 起不再重定向）

M8 路由重排曾为 M7 及以前的控制台路径提供**自动客户端重定向**兼容窗口；**M9 起该窗口已全量移除**（ADR-0029 Q3 终裁）。旧路径直链现在落在 **404 页**（保留导航壳，回显请求的地址并提供「回主页」链接）——**书签、内部 wiki 与自动化脚本里引用的旧路径请按下表更新**：

| 旧路径（已失效） | 新路径 |
|---|---|
| `/repositories` | `/admin/repositories/local` |
| `/repositories/new` | `/admin/repositories/new` |
| `/repositories/:key` | `/admin/repositories/:key` |
| `/repositories/:key/settings` | `/admin/repositories/:key/edit` |
| `/repositories/:key/tree/<path…>` | `/artifacts/:key/<path…>`（文件深链 = 路径末段；旧 `?focus=` 形打开时自动折入路径段规范形） |
| `/settings` | `/admin/general/settings`（改密块在 `/profile`） |
| `/security/users*`、`/security/groups*`、`/security/permissions*`、`/security/tokens` | `/admin/security/…` 同名尾段 |
| `/audit` | `/admin/governance/audit` |
| `/governance/gc` / `/governance/quotas` / `/governance/replication` / `/governance/backup` | `/admin/governance/…` 同名尾段 |

> 首页 `/` 不受影响：登录后的落点仍是 `/artifacts`——这不是兼容窗口，是控制台的固定首页语义。

## 浏览器兼容

| 引擎 | 状态 | 依据 |
|---|---|---|
| Chromium（Chrome/Edge） | **主支持**：QA 全量矩阵（登录/建仓/树/上传/搜索/安全/审计/GC 全链） | T-104（Chromium for Testing 151） |
| WebKit（Safari 内核） | 登录/制品树/上传链可用 | T-104 发现的懒加载 CSS 资源路径缺陷（D-104-1）已由 T-120 修复，修复后跨引擎复核通过 |
| Firefox | 同 WebKit | 同上 |

已知边界：三引擎均为 QA 抽查口径（登录/树/上传三链），非全量矩阵；Chromium 是唯一全量回归引擎，生产环境推荐 Chromium 系浏览器。

## 多语言与计划任务（预埋）

以下能力**尚未定案落地**，本节为预留文档位，不构成当前版本承诺：

- **控制台界面语言**：当前界面为中文单语。多语言切换（含英文界面）在规划中——落地后本节将改写为语言切换入口、覆盖范围与回退行为说明。
- **计划任务（cron 调度）**：备份 / 维护 / 复制三域的 cron 计划任务能力在规划中。现状：复制为事件驱动引擎（无 cron 计划面）、备份是 export/import 带外任务（无 UI 进度面）、仓库表单中的 `cron` / `sync` 等字段为恒禁用预留位——调度域落地后管理域将新增计划任务页，预留位字段届时转正。

## 有意不兼容与已知边界（M8 控制台）

| 项 | 说明 |
|---|---|
| Access Tokens 令牌清单 | 服务端只存指纹、**无令牌清单端点**（R6 未落地）——控制台台账是会话内存态（刷新即空，明文只展示一次）；历史令牌吊销走按 token_id 出口 + 审计日志指纹对账 |
| Packages 卡片落地页 / Builds / Xray / Pipelines / Distribution | 不建——JFrog 独立产品（Non-goal），制品树是最近似落点 |
| Authentication Providers（SAML/Crowd 等）配置页 | 不建（产品 Non-goal）；OIDC/LDAP 走 `binflow.yaml`（见[专题指南](guides/oidc-config.md)） |
| 仓库 Layouts / Proxies / Mail Server / cron 计划备份 | 不建——BinFlow 无对应功能面；备份是 export/import 任务（见[备份手册](admin/backup-restore.md)） |
| 跨路径 Move/Copy 的树内入口 | 不建 UI——copy/move 走 REST（[制品操作族](admin/artifact-operations.md)，M12 起）；回收站 UI 在治理页（M12 起）。树内收藏（My Favorites）已有——仓库级、浏览器本地 |
| 审计 CSV 导出 / 搜索 checksum 反查 UI | P2 债务：按钮不渲染 / 页面引导走 REST |
| token 签发/吊销落审计 | `token.issue` / `token.revoke` 均落审计（detail 含指纹与 TTL；step-up 路径另含 `step_up` 维度，见[step-up 指南](admin/token-step-up.md#审计)） |
| 非 admin 的仓库清单 | 管理面 CapRepoRead 门是定案（存在性不泄露），非缺陷；制品可达性走内容面 |

## 下一步

- 从 Artifactory 迁移的逐任务操作路径：[Artifactory → BinFlow 操作路径对照表](artifactory-path-map.md)；真实源实例迁移实录与差异清单：[附录 V28](admin/real-env-appendix.md)
- 授权三步流与组语义：[用户组与权限管理](admin/groups-permissions.md)；角色模型：[RBAC 角色与仓库级管理员](admin/rbac-roles.md)
- 审计 / GC / 配额：[治理指南](admin/governance.md)；备份恢复：[备份与恢复手册](admin/backup-restore.md)
- CI 与脚本不走控制台，走 [API Token](faq.md#高频场景高-qps-请用-access-token) 与各协议[接入指南](README.md)
