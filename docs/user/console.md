---
title: Web 控制台使用指南
sidebar_position: 30
---

# Web 控制台使用指南

> 适用版本：M8（新信息架构：双模式壳 / 跨仓制品树 / 管理域五分组 / Set Me Up 与 Deploy 对话框族；设计规格 `docs/design/console-m8.md`）。
> 本篇全部 UI 路径与对话框行为在 HEAD（`89b27ce` 构建，含内嵌控制台）的 scratch 实例（127.0.0.1:18091，七仓种子覆盖全部五种包类型）上以 Playwright 走查验证（11/11 通过：双模式导航、树深链、对话框族、管理域路由、10 条旧路径重定向〔M8 兼容窗口；M9 起已移除，见下节〕）；登录/会话/CSRF 段沿用 M4 QA 基线（T-103/T-105，报告 `reports/agents/T-103-qa.md` / `T-105-qa.md`），M8 未改动服务端会话语义。浏览器矩阵依据 T-104 与 T-120 修复后的跨引擎复核。

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
- **全局搜索**：顶栏搜索框（placeholder「搜索制品」），`⌘K` / `Ctrl+K` / `/`（非输入态）快捷键直达；Enter 进 `/search`。
- **空实例引导**：仓库数为 0 时制品页内嵌引导卡「创建仓库」+「跳过」（跳过状态存浏览器 localStorage，不设独立路由）。

管理模式五分组 12 条目全图：

```
仓库
└ 仓库                /admin/repositories（Local / Remote / Virtual 三 Tab）
用户与权限
├ 用户                /admin/security/users
├ 组                  /admin/security/groups
├ 权限                /admin/security/permissions
└ Access Tokens       /admin/security/tokens（占位页，见下文边界表）
治理
├ 审计日志            /admin/governance/audit
├ 维护（GC）          /admin/governance/gc
├ 配额                /admin/governance/quotas
├ 复制                /admin/governance/replication
└ 备份 / 恢复         /admin/governance/backup
监控
└ 存储                /admin/monitoring/storage
常规
└ 系统信息            /admin/general/settings
```

## 制品树浏览器（`/artifacts`）

跨仓树是应用模式的落地页与核心页：**全部仓库出现在一棵树上**，仓库是顶层节点（图标区分仓型与包类型），左侧树 + 右侧详情面板联动。

**页头工具栏**（左→右）：`Set Me Up`（客户端接入向导，见下节）· `部署 Deploy`（浏览器上传）· `管理仓库→`（跳仓库管理）· `过滤仓库` 输入框（前端过滤已加载的仓库清单，`清除` 复位）。

树交互：

- **懒加载一层**：点开节点才拉取下一层（10k 节点级目录树仍流畅——展开仅渲染可视区）。
- **URL 即状态**：选中路径进 URL（`/artifacts/<repo>/<path…>`）——可以直接分享或收藏**深链**，打开时自动展开祖先并滚动定位到节点。
- **详情右联**：单击节点，右侧面板即时展示详情；Enter 进入下一层。
- **右键菜单**（`Shift+F10` / Menu 键键盘可达）按对象三形态：
  - 文件：复制路径 / 下载 / 删除
  - 文件夹：复制路径 / 删除 / 刷新
  - 仓库：复制仓库路径 / 刷新 / 在仓库管理中打开
- 当前层 children 表（名称/类型/大小/修改时间/操作者）支持「过滤当前层」与「只看文件」；大目录客户端分页「加载更多」，超过 2000 条提示改用[搜索](#搜索与仪表盘)。
- **跨路径 Move/Copy 不做**（服务端无端点）；**无回收站**——删除即永久（危险确认文案明示）。

详情面板三形态（Tab 式：`常规` / `有效权限`（admin 渲染））：

- 仓库/文件夹/文件各有字段集；路径与 URL 一律 mono + 拷贝按钮；文件可「下载并校验」（浏览器实测 sha256 与服务端对账）。
- 文件附加块：**Checksums**（sha256/sha1/md5 各带「（上传时提供：一致 ✓）」徽标——映射 originalChecksums 比对）；docker manifest digest 行的 **tag 徽标**（树表对 docker 仓显示「标签/摘要」列替代「类型/sha256」）。
- 权限过滤自然生效：无 read 权限的子树不可见；操作中 403 行内呈现并指向权限模型。
- 协议深度特化视图（npm 包目录、pypi 归一名视图、maven `maven-metadata.xml` 只读面板）为登记后续项——当前统一按路径树呈现。

## Set Me Up：客户端接入向导

三入口：制品树页头 `Set Me Up` 按钮（选中仓库预选）· 仓库管理列表行操作 · 仓库详情页头。对话框流程：

1. **包类型网格**「选择客户端类型」：只列实例内**已有仓库的包类型**并集（五种：Generic / Docker / Maven / npm / PyPI）；零仓库 → 空态提示先建仓。从仓库上下文进入则跳过网格直达主对话框。
2. **主对话框**「配置 `<PackageType>` 客户端」：仓库下拉（预选当前仓，只列该包类型仓）+ `配置 Configure` / `部署 Deploy` 两个 Tab——
   - **配置**：解析/拉取侧指令（docker login+pull、settings.xml、pom repositories、`.npmrc`、pip.conf、curl 下载校验）；**部署**：发布侧指令（curl -T、docker build/push、mvn deploy、npm publish、.pypirc+twine）。每块独立 Copy；内容与 docs/user 各[接入指南](integrations/npm.md)同源（UI 不发明命令）。
   - 凭据位：铸币前显示 `<USERNAME>` / `<TOKEN 或口令>` 占位；生成令牌成功后自动回填。
3. **生成令牌**（控制台铸币位）：
   - **admin 会话**：直接点「生成令牌并创建指引」——管理员臂免二次口令（ADR-0027 决策 1，界面有说明行）。
   - **非 admin 会话**：生成区含**口令框**；`auth.token_step_up` 开启时，无凭据请求被服务端 401 `step_up_required` 拒绝 → 对话框内联口令重验表单（自动聚焦）；口令错误 401 `step_up_invalid` → 内联错误（服务端原文，不出第二层对话框）；正确口令续铸成功。语义全解见 [Token 铸造二次认证（step-up）](admin/token-step-up.md)。
   - 成功 → **「令牌已生成」一次性面板**：token 明文（mono + Copy + 「关闭后不可再查看」提示）+ 24 小时过期与 token_id 说明。签发走 `POST /api/security/token`（默认 `expires_in=86400`）。

## Deploy：浏览器上传对话框

入口与 Set Me Up 对称（树页头 / 仓库列表行 / 仓库详情页头）。字段序：目标仓库（下拉，**只列 local 的 Generic / Maven 仓**）→ 包类型（只读回显）→ 部署模式（单个/多个）→ 拖拽区（`拖拽文件到此处` 或点击选择）→ 目标路径（mono 可编辑）→ **`部署` 显式提交**（文件入队后需点「部署」才启动哈希+上传；关闭对话框即中止排队与在飞上传）。

- 浏览器端流式计算 sha256 与服务端返回值比对，成功显示**一致徽标**；409 双值（received/actual）/ 403 权限指引 / 413 quota 文案**原样呈现**。
- maven 仓按 **GAV 表单**生成 layout 路径（五输入实时预览 + 前端预检）。
- **docker / npm / pypi 仓不出现上传入口**（协议发布是客户端会话，UI 以接入命令块替代）。
- **特殊字符安全**（M8 修复）：目标路径同时显示原值（可编辑）与**请求编码值只读回显**（`%`→`%25`、空格→`%20`、中文→UTF-8 percent 编码）——含 `%` `#` `?` 空格中文的路径不再因二次编码踩坑（T-231 修复矩阵）。

## 管理模式各域

### 仓库（`/admin/repositories`）

- **三 Tab 列表**：`/admin/repositories/{local|remote|virtual}` 子路由；「N 个仓库」计数 + 右上 `+ 添加仓库`；列头排序（key / 包类型）+ 行尾删除入口；每行 Set Me Up / Deploy 快捷钮。
- **建仓向导**：`/admin/repositories/new` 进页弹**包类型网格**（五项必选）→ 单页分区表单（常规设置 → 来源/成员 → 包类型专属 → 高级）+ 右栏实时摘要。key 规则 `[a-z][a-z0-9-]{1,62}` 前端预检、服务端终裁（400 行内回显）。**保留字 `api` / `v2` / `docs` / `console` / `ui` / `assets` 建仓即 400**。
- **仓库详情** `/admin/repositories/:key`：概要 / 接入命令（与接入文档同源）/ 统计（配额水位条）/ 配置（配额行内编辑 + patterns；**manage 持有者**亦可编辑本仓配置——见 [RBAC 指南](admin/rbac-roles.md)）四 Tab + 危险区（删仓仅全量 admin 可见）。
- **编辑** `/admin/repositories/:key/edit`：rclass/包类型锁定，其余字段同建仓表单。
- **删除**：两段强确认——非空仓必须勾选 `同时删除内容` + **输入 repo key 确认**（不勾选直接删非空仓会被服务端 400 拒绝）。
- 治理字段（仅 local 仓）：`quotaBytes` 与 `includesPattern` / `excludesPattern`（详见[治理指南](admin/governance.md)）。

### 用户与权限（`/admin/security/*`）

- **用户**：列表（Name/Email/Groups/Role/Status）→ 编辑页分区表单（用户设置〔含**角色三值下拉** `user/readonly_admin/admin`，仅 admin 可改〕/ 选项 / 口令 / 相关组双列穿梭 / 权限矩阵）。角色语义见 [RBAC 角色与仓库级管理员](admin/rbac-roles.md)。
- **组**：列表 + 表单（组设置 + 成员穿梭 + 组权限矩阵）。
- **权限 target**：列表 → 单页分区编辑器（名称 / 资源 / 用户 / 组）+ **两步资源对话框**（`编辑仓库…` → ① 选仓库 → ② 可选 include/exclude patterns）+ 四动作矩阵（read/write/delete/manage）+ **模式测试器**（输入路径即时显示逐条命中与最终判定）+ 保存前 diff 确认。完整操作与 curl 对账见[用户组与权限管理](admin/groups-permissions.md)。
- **Access Tokens**：占位页（签发引导 + 输入 token_id 吊销）；控制台日常铸币走 [Set Me Up](#set-me-up客户端接入向导) 对话框。

### 治理（`/admin/governance/*`）

- **审计日志**（`/admin/governance/audit`）：时间窗/操作者/仓库/动作/路径过滤 + 游标「加载更多」；动作值原样 mono 显示（不翻译）。词表见[治理指南](admin/governance.md#审计)。
- **维护（GC）**（`/admin/governance/gc`）：GC 状态 + dry-run 结果面板 + apply **输入实例名二次确认**；存储迁移进度面板同页。
- **配额**（`/admin/governance/quotas`）：每仓 used/quota 水位条（80% 黄 / 100% 红）+ 行内编辑上限。
- **复制**（`/admin/governance/replication`）：复制目标表 + 最近事件（10s 轮询）。
- **备份 / 恢复**（`/admin/governance/backup`）：CLI 引导卡（export/import 命令与警示，一键复制）——备份恢复是**高危带外操作**，不做进度 UI；完整链见[备份与恢复手册](admin/backup-restore.md)。

### 监控与常规

- **存储**（`/admin/monitoring/storage`，M8 新页）：刷新行 + 汇总卡（blob/制品大小与计数、优化率）+ 逐仓用量表（TOTAL 首行）。
- **系统信息**（`/admin/general/settings`）：实例信息（版本/修订/产品，开放端点）+ 健康卡（`/api/v1/health` 子系统状态，承载 Artifactory Service Status 的语义）。匿名读开关、数据目录、日志级别等**无查询端点，页面不展示**——配置以 `binflow.yaml` 为准。
- **编辑档案**（`/profile`，应用模式）：修改口令 + API Token 使用说明。M7 版设置页已拆分为系统信息（管理域）与编辑档案（应用域）两页。

### 搜索与仪表盘

- **搜索**（`/search`）：名称子串检索，结果按调用者权限过滤（无 read 权限的仓库不出现在结果里）；支持按仓库收窄；行点击**深链进制品树**（自动展开定位）。checksum 精确反查暂未接 UI（页面引导走 REST）。
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
| `/repositories/:key/tree/<path…>` | `/artifacts/:key/<path…>`（树深链的 `?focus=` 参数原样可用） |
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

## 有意不兼容与已知边界（M8 控制台）

| 项 | 说明 |
|---|---|
| Access Tokens 管理页 | 仍为占位（签发引导 + token_id 吊销输入；列表/吊销管理面 R6 未落地）；控制台铸币走 Set Me Up 对话框 |
| Packages 卡片落地页 / Builds / Xray / Pipelines / Distribution | 不建——JFrog 独立产品（Non-goal），制品树是最近似落点 |
| Authentication Providers（SAML/Crowd 等）配置页 | 不建（产品 Non-goal）；OIDC/LDAP 走 `binflow.yaml`（见[专题指南](guides/oidc-config.md)） |
| 仓库 Layouts / Proxies / Mail Server / Webhooks / cron 计划备份 | 不建——BinFlow 无对应功能面；备份是 export/import 任务（见[备份手册](admin/backup-restore.md)） |
| 跨路径 Move/Copy、回收站（Trash Can）、收藏/星标 | 不建——无对应端点；删除即永久 |
| 审计 CSV 导出 / 搜索 checksum 反查 UI | P2 债务：按钮不渲染 / 页面引导走 REST |
| token 签发/吊销落审计 | `token.issue` / `token.revoke` 均落审计（detail 含指纹与 TTL；step-up 路径另含 `step_up` 维度，见[step-up 指南](admin/token-step-up.md#审计)） |
| 非 admin 的仓库清单 | 管理面 CapRepoRead 门是定案（存在性不泄露），非缺陷；制品可达性走内容面 |

## 下一步

- 从 Artifactory 迁移的逐任务操作路径：[Artifactory → BinFlow 操作路径对照表](artifactory-path-map.md)；真实源实例迁移实录与差异清单：[附录 V28](admin/real-env-appendix.md)
- 授权三步流与组语义：[用户组与权限管理](admin/groups-permissions.md)；角色模型：[RBAC 角色与仓库级管理员](admin/rbac-roles.md)
- 审计 / GC / 配额：[治理指南](admin/governance.md)；备份恢复：[备份与恢复手册](admin/backup-restore.md)
- CI 与脚本不走控制台，走 [API Token](faq.md#高频场景高-qps-请用-access-token) 与各协议[接入指南](README.md)
