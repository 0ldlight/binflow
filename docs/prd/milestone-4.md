# PRD — M4 控制台与治理（Web Console / 权限完整 / 审计 / GC / 配额 / 备份恢复）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-4.md` |
| 里程碑 | M4 — 控制台与治理（对应 ROADMAP.md「M4 — 控制台与治理」全部条目） |
| 状态 | **v1.1**（T-109：R1 命名面与 ADR-0014 对齐标注——「PRD 面 + ADR 内核」裁决；R4 session TTL 双键收口；K1~K3 回写注记。v1.0 初版：§7 六项开放问题附暂行假设，用户定案后回写） |
| 上游依据 | PRODUCT.md（核心能力 5/6：Web 控制台、治理）、ROADMAP.md M4 节、M1 交付基线（milestone-1.md v1.3.1，`m1-done`）、M2 交付基线（milestone-2.md v1.3，`m2-done`）、M3 交付基线（milestone-3.md v1.2，`m3-done`）、ADR-0002（go:embed 单二进制）、ADR-0006（blob 布局与备份硬约束：mtime 保留、mark-sweep、grace=blob mtime）、ADR-0008（`/binflow` 前缀与 repo key 保留字）、ADR-0011（Docusaurus 同栈 React，M4 控制台同栈复用）、ADR-0012（remote 凭据 `enc:v1:` 密文——export/import 的敏感数据面）、docs/design/architecture.md §7.1（`/api/v1/audit` 预留、console 挂载位）、docs/reverse/auth-model.md（§1 用户/组字段与校验链、§4 权限概览，高置信度）、docs/reverse/rest-api.md（§3 `?permissions`、§4 搜索、§5 prune 端点） |
| 下游消费者 | tech-lead（拆票）、architect（console 路由 / 004 迁移 / session 定案 ADR）、ux-designer（信息架构并行票 `docs/design/console-ui.md`）、dev 各角色（httpapi / web 前端 / metadata）、qa-engineer（W 序列验收）、release-engineer（烟测）、tech-writer（M4 用户文档） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-20 | 初版：M4 范围、FR-23~FR-33、端点矩阵 CE/SE/SR/GE 四域 29 条、W01~W40 验收命令（curl + Playwright 浏览器面）、回归基线反转表（search 404 → 分派、console 占位 → SPA、groups 404 → 实现）、M1~M3 遗留收编 8 条、六项开放问题附暂行（session 机制 / 配额粒度 / 搜索范围 / 备份一致性窗口 / GC 执行形态 / docker remote 是否提前） |
| v1.1 | 2026-08-20 | T-109（tech-lead T-88 转交）三点收口：① **R1 命名面对齐**——控制台命名面（`/binflow/ui/**` 挂载、保留字 `ui`、`/binflow/` 301、cookie `binflow_session`）与 ADR-0014 原文的 `/binflow/console/**` 冲突，经 tech-lead 裁决「**PRD 面 + ADR 内核**」：本 PRD 命名面为胜出面（正文不改），ADR-0014 的机制内核（server-side session 落库、CSRF 分层、Authenticator 增 Session 臂、无 console 专属 API 树、vite+React 构建链）照常生效，其命名面由 T-108 勘误对齐到本文——FR-23 与 §5.2 补对齐注记。② **R4 TTL 双键收口**——`console.session_ttl_hours`（主键，默认 24）+ `console.session_ttl_seconds`（覆盖键，同给时以 seconds 为准，测试粒度用）；W08 维持 seconds 形态，FR-23 补覆盖键说明，Q1 暂行同步。③ §5.5 K1~K3 补注「待逆向扩编票回写」（暂行值已由 tech-lead 写死进 T-97/T-92 派单口径） |

---

## 1. 背景与目标

### 1.1 背景

M1~M3 交付了完整的「无界面制品源」：存储引擎、三型仓库（local/remote/virtual）、五协议（generic/docker/maven/npm/pypi）、认证骨架（users + permission targets + tokens）。但 BinFlow 至今只有 REST API——**PRODUCT 核心能力 5（Web 控制台）与 6（治理）整体缺席**：管理员建仓要写 curl、授权要手拼 JSON、审计事件躺在 `audit_events` 表里无查询面、GC 是一次性 CLI、配额与备份恢复为零。

M4 的用户价值排序（对齐 PRODUCT「平台工程 / DevOps 团队：内网统一制品源」）：

1. **Web 控制台**是迁移用户的第一触点——Artifactory 用户的心智是「UI 建仓、树里浏览、页面上传、框里搜索」；
2. **权限模型完整实现（groups）**是企业接入的硬门槛——M1 只有 users + permission targets，组在 principals schema 里留了位但没有实体，「devs 组可写 devs/**」这类团队级授权目前无法表达；
3. **治理面**（审计查询 / GC 管理化 / 配额）让「内网唯一制品源」从可用变成可运营；
4. **备份/恢复**是制品仓库的生存底线（ADR-0006 早已把「备份 = blobs/ + SQLite 快照」定为 M4 交付工具）。

M4 在 M1~M3 地基上**追加**而非返工：控制台是既有 REST 之上的皮肤（UI 消费的端点 90% 已存在，新增的只有 session / 审计查询 / GC / usage 四族）；groups 打通 M1 预留的 principals schema（permission target 的 `principals.groups` 位）；GC 管理化是 CLI 语义的在线化；export/import 落实 ADR-0006 的快照公式。

### 1.2 M4 目标

> 一句话：交付一个浏览器可用的 Web 控制台（登录 / 仓库管理 / 制品树 / 上传下载 / 搜索），把 users/groups × repo × path 权限模型补完，并给出审计查询、GC、配额、备份恢复四件治理工具。

量化门槛（未达即里程碑不完成）：

| 指标 | M4 门槛 | 来源 |
|---|---|---|
| 控制台可用 | Playwright（Chromium）走通 登录 → 建仓 → 上传 → 浏览 → 搜索 → 删除 全链（W09~W14，P0 全绿） | ROADMAP M4 第一条 |
| 权限完整 | groups CRUD + 组成员授权即时生效：jane 经 `devs` 组获得 write、移出组后 403（W19，无需重启） | ROADMAP M4 第二条 |
| 审计可查 | `/api/v1/audit` 按 actor/action/repo/时间窗过滤，M1~M4 全动作词表可查且 append-only（W22/W23/W39） | ROADMAP M4 第三条 |
| GC 管理化 | REST dry-run 报告 + apply 后被引用 blob 幸存、孤儿消失（W24/W25） | ROADMAP M4 第三条 |
| 配额 | repo 级 quotaBytes 超限上传 413 且无部分写入；五协议上传路径全覆盖（W26/W27） | ROADMAP M4 第三条 |
| 备份恢复 | 在线 export → 空 data dir import → 四协议制品 + 用户/组/权限/token 全部可验证，mtime 保留（W28~W32） | ROADMAP M4 第四条 + ADR-0006 |
| 既有零回归 | M1 C 序列 P0 + M2 D 序列 P0 + M3 M 序列 P0 复跑全绿（断言按 §5.6 反转表更新） | M2 FR-7-AC2 先例 |
| 冷启动不回退 | 空库冷启动 < 2s（含 go:embed SPA 资产后） | M1 NFR-P1 传递 |

### 1.3 上游依赖与并行关系

- **信息架构（并行票，ux-designer）**：`docs/design/console-ui.md`——导航结构、页面清单、视觉规范、`data-testid` 命名约定。**本 PRD 只定功能边界与可观察验收，不规定布局与控件**；W 序列 Playwright 断言优先锚「行为结果」（URL / 表格行文本 / 落盘文件 checksum），必须用到选择器处一律以 console-ui 规范的 testid 为准（PRD 示例中的 testid 为占位）。
- **逆向校准（并行票，reverse-engineer）**：auth-model.md 扩编——groups 端点族（GET/PUT/POST/DELETE `/api/security/groups`）、`/api/search/artifact` 的 name 匹配语义、`GET /api/security/token` 列表形态（若做 token UI）。落地前本文按 §5.5 暂行值执行，落地后按 M1/M3 先例回写（+0.1）。
- **架构依赖（architect）**：console 挂载路由与 repo key 保留字 `ui`（ADR-0008 增补）、session 机制定案（§7 Q1）、004 迁移（users.email 列、groups/user_groups 表、sessions 表、audit 查询索引）、export/import CLI 设计。本 PRD 只约束可观察行为。
- **同栈约束（ADR-0011）**：控制台前端用 React（与 M5 文档中心同栈，构建链与技能复用）；源码进 `web/`，build 产物复制进 `internal/console` go:embed（architecture §2 既有约定），不引第二套框架。

---

## 2. 范围

### 2.1 In scope（与 ROADMAP M4 条目一一对应）

| # | ROADMAP 条目 | 本 PRD 功能需求 |
|---|---|---|
| 1 | Web 控制台：登录、仓库管理、制品树浏览、上传、搜索 | FR-23（基座与登录）+ FR-24（仓库管理页）+ FR-25（制品树/上传/下载）+ FR-26（搜索） |
| 2 | 权限模型完整实现（users/groups × repo × path）+ UI | FR-27（groups 与继承，服务端）+ FR-28（用户/组/权限管理 UI） |
| 3 | 审计日志、GC、配额 | FR-29（审计查询）+ FR-30（GC 管理化）+ FR-31（配额） |
| 4 | 备份/恢复（export/import） | FR-32 |
| 5 | （支撑面）repo 治理字段启用：includesPattern/excludesPattern（M1 勘误②收编） | FR-24 内 |
| 6 | （验收面）Playwright + 回归基线 + 性能 | FR-33 + §5.3/§5.6 |

（M4 的逆向规格、console-ui 设计、架构 ADR 为流程条目，作上游输入，不单列功能需求。）

### 2.2 Non-goals — M4 明确不做（防范围蔓延）

**产品级 Non-goals（继承 PRODUCT.md，全程有效）**：不做 HA/联邦、不做 Xray、不做 LDAP/SAML/OIDC、不做 Artifactory 全量 REST 兼容、不做 UI 高级分析与洞察报表。

**M4 里程碑级 Non-goals**：

| 不做项 | 归属 | M4 的隔离边界 |
|---|---|---|
| docker 类型的 remote 仓（Docker Hub pull-through） | M5+ 评估 | 维持 M3 现状：`{"rclass":"remote","packageType":"docker"}` → 400。M3 PRD Q4 已转用户知悉，本 PRD §7 Q5 继续挂起（暂行不进 M4——范围已满，替代路径 `skopeo copy` 可用） |
| OAuth2/OIDC 登录、SSO、MFA | M6+ | 控制台只有本地用户名口令登录（+ 既有 API Token 不变） |
| 移动端适配、暗色主题、i18n 界面多语言 | M5+ | 桌面 Chromium 宽度为主；i18n 由 ADR-0011 的 Docusaurus i18n 方案 M5 一并评估 |
| 审计分库、审计保留策略（retention/轮转）、审计导出 CSV | M5+/P2 | 审计只在 `audit_events` 单表内查询（limit ≤ 1000 分页）；CSV 导出 P2 |
| 全局配额 / 文件数配额 / 用户级配额 / remote 缓存配额 | M5+ | 仅 repo 级 `quotaBytes`（§7 Q2 暂行）；remote 仓 pull-through 落盘不计量 |
| 在线不停机 import / 增量备份 / 定时备份调度 | M5+ | import 一律停机 + 空 data dir；export 手动触发，无 cron |
| 异步任务框架（job 队列、进度推送 SSE/WebSocket） | M6+ | GC/audit 查询同步执行（§7 Q6 暂行）；UI 无推送，轮询/一次性请求 |
| immutable tag / tag retention / maxUniqueSnapshots 保留策略 | M5+ 评估 | M2 Q4、M3 Q6 的「M4 治理」预告在此收窄：配额/GC 优先，保留策略维持不做 |
| virtual 仓库的穿透删除、成员排除模式、per-user 视图 | M6+ | M3 §2.2 预告的 M4 项收窄为不做（DELETE 透传 M3 已定有意不兼容；per-user 视图依赖复杂缓存） |
| Maven 索引（`.index/*`）、npm search API（`/-/v1/search`） | 不排期 | 维持 404（M3 ME-10/NE-08）；控制台搜索走 §5.2 SR 域 |
| Artifactory `/api/v2/security/permissions/**` 路径 | 不排期 | 维持 `/binflow/api/v1/permissions`（M1 E-24 评估收口：v2 的 uuid/软删除语义复杂度不成比例，SE-09） |
| `bf` CLI、Artifactory 迁移工具 | M6+ | export/import 是**灾备语义**（自恢复），不是跨产品迁移 |

---

## 3. 用户与场景（M4 视角）

- **场景 A（管理员日常）**：平台工程师打开 `http://registry:8080/binflow/ui/`，登录后建 `npm-local` 仓、给 `devs` 组授 `devs/**` 写权限、在树里浏览 CI 推上来的制品并下载核对——全程不碰 curl。
- **场景 B（团队授权）**：新成员 jane 入职：admin 把 jane 加入 `devs` 组即获得全部组授权；离职时移出组（或删组），权限即时收回——**组是授权的行政单元，用户是组的成员**。
- **场景 C（安全审计）**：安全团队季度审计：按 actor=ci-bot + 时间窗拉取全部 deploy/delete 事件；确认审计不可篡改；对超配额拒绝（quota.exceeded）留痕复查。
- **场景 D（容量治理）**：磁盘告警：管理员 UI 里 dry-run GC 看可回收字节 → 确认 apply；给 `build-outputs` 仓设 500GB 配额防 CI 失控；上传超限得 413 与明确文案。
- **场景 E（灾备演练）**：运维在服务运行中执行 `binflow-server export` 产出快照目录（或 tar），拷贝离场；演练机以空 data dir `binflow-server import` 后启动，四协议制品与账号体系原地复活，blob mtime 保留（grace 时钟不重置——ADR-0006 勘误②）。
- **场景 F（迁移验收）**：从 Artifactory 迁移的团队用浏览器完成首个仓库与权限配置，体验「概念同名、行为同构」（repo key、permission target、groups、树浏览），只有 URL 前缀不同。

---

## 4. 功能需求

约定延续 M1~M3：`BASE=http://localhost:8080`、`admin`/`$ADMIN_PW`；新增 `UI=$BASE/binflow/ui`（控制台）、`JAR=/tmp/bf-cookie.jar`（curl cookie jar，session 面）、`BIN=./bin/binflow-server`（CLI 面）。优先级 P0/P1/P2 沿用 M1 定义。W 序列命令见 §5.4；浏览器面选择器 testid 以 ux-designer 的 console-ui 规范为准（本文示例为占位）。

### FR-23 控制台基座与登录会话（dev-go-core：httpapi/session；web 前端基座）

**用户故事**：作为管理员，我打开一个 URL 就能登录进 BinFlow 的管理界面——会话安全（HttpOnly、可登出、有 TTL）、SPA 深链可刷新、静态资产强缓存——这是我判断「这是个产品而不是一堆 API」的第一眼。

> **对齐注记（v1.1，R1）**：本 FR 与 **ADR-0014（T-108 勘误后）对齐**——**命名面以本 PRD 为准**（`/binflow/ui/**` 挂载、repo key 保留字 `ui`、`GET /binflow/` 301、cookie 名 `binflow_session`；tech-lead 裁决「PRD 面 + ADR 内核」，ADR-0014 原文的 `/binflow/console/**` 命名由 T-108 勘误对齐到本文）；**机制内核以 ADR-0014 为准**（server-side session 落库形态、CSRF 分层防御、`auth.Authenticator` 增 Session 第三臂、前端消费通用 `/api/v1/**` 不设 console 专属树、vite+React+TS 构建链）。两文冲突处以本注记的分界为准。

行为规格：

- **挂载与保留字**：控制台挂 `/binflow/ui/**`（SPA 专属前缀，避免与内容路径 `/binflow/<repo>/<path>` 冲突）；`GET /binflow/` → **301** 到 `/binflow/ui/`；深链（如 `/binflow/ui/repositories`）刷新返回 SPA shell（history fallback）。**repo key 保留字新增 `ui`**（建仓 400，ADR-0008 增补；存量库已有 `ui` 仓时启动 WARN 提示改名，路由仍占用——P2 注记）。资产带内容指纹（`/binflow/assets/<hash>.js|css`），`Cache-Control: public, max-age=31536000, immutable`；SPA shell 本体 `no-cache`。
- **登录会话（Q1 暂行：server-side session）**：`POST /binflow/api/v1/session`（JSON `{"username","password"}`，form 亦接受）→ 200 `{"username","admin":bool}` + `Set-Cookie: binflow_session=<opaque>; HttpOnly; Path=/binflow; SameSite=Lax`；session 落 SQLite（服务重启不掉线，P1 验证）、TTL 由 `console.session_ttl_hours`（**主键**，默认 24）控制；**覆盖键 `console.session_ttl_seconds`**（v1.1 双键收口：同给时以 seconds 为准，供测试/短期会话粒度——W08 用例依赖此键）；到期待遇同未认证（401）。session cookie 与 Basic/Token 是**等价认证凭据**（内容路径与管理面同用——控制台的上传/删除就靠它）。
- **登出**：`DELETE /binflow/api/v1/session` → 204，服务端吊销（cookie 重放 401）；`GET /binflow/api/v1/session` = whoami（200 当前主体 / 401）。
- **错误与防泄露**：错误凭据 401 E-01（`/api/v1` 族信封），文案不区分「用户不存在」与「口令错误」；登录成败均落审计（复用既有 `login.success`/`login.failed`）。登录爆破锁定 **P2 不做**（M4 只留审计与日志）。
- **CSRF 防线（暂行）**：SameSite=Lax 之外，凡以 session cookie 认证的**写操作**（非 GET/HEAD），若请求带 `Origin` 头且非同源 → 403（不携带 cookie 认证的 Basic/Token 请求不受影响——CLI 与 CI 零感知）。
- **体积与构建**：SPA build 产物 gzip 后 embed 增量目标 ≤ 5MB（记录项，40MB 二进制总预算 M5 check-size 把关）；`make build` 产出的单二进制自带完整控制台（go:embed，ADR-0002）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-23-AC1 | W01：`GET $BASE/binflow/` → 301 且 Location 为 `/binflow/ui/`；`GET $UI/` → 200 HTML 含 SPA 挂载点；深链 `$UI/repositories` 刷新 → 200 | P0 |
| FR-23-AC2 | W01b：建 repo key `ui` → 400（保留字）；既有合法 key 行为不变 | P0 |
| FR-23-AC3 | W02：指纹资产响应头含 `Cache-Control: ...immutable`；SPA shell 含 `no-cache` | P1 |
| FR-23-AC4 | W04/W04b：登录 200 且 `binflow_session` cookie 具备 `HttpOnly; Path=/binflow; SameSite=Lax` 三属性；whoami 200 | P0 |
| FR-23-AC5 | W05：错误凭据 → 401（E-01，文案不泄露用户存在性）；审计表新增一条 `login.failed`（W23 断言） | P0 |
| FR-23-AC6 | W07：logout 204 后，同 cookie 重放 whoami → 401（服务端吊销） | P0 |
| FR-23-AC7 | W08：`console.session_ttl_seconds=5` 重启后，登录 → sleep 6 → whoami 401（TTL 生效） | P1 |
| FR-23-AC8 | W37a：服务重启后（未过期）session 仍可用（server-side 落库） | P1 |
| FR-23-AC9 | W38：session cookie + 跨站 `Origin: http://evil.example` 的写请求（PUT 内容）→ 403；同源/无 Origin 同请求 → 201 | P0 |
| FR-23-AC10 | W37：含 embed 资产的空库冷启动 < 2s；二进制大小与 SPA gzip 体积记录进 QA 报告 | P0 |

### FR-24 仓库管理页与 repo 治理字段（web 前端 + dev-go-core 字段启用）

**用户故事**：作为管理员，我在 UI 里完成建仓/改仓/删仓全流程——字段与 M3 REST 完全同构（含 remote 凭据不回显），并且能给仓配 includes/excludes 与配额，而不是去改 YAML。

行为规格：

- **列表页**：全部仓库 + `type/packageType` 过滤（复用 E-04/RE-03）；行内显示 rclass、packageType、描述。
- **新建/编辑表单**：字段集 = M1 FR-3 + M3 FR-15 的合法子集（local/remote/virtual 三型；remote 的 url/username/password/TTL 族；virtual 的 repositories/priorityResolution/defaultDeploymentRepo；M4 新增 `quotaBytes` 与 `includesPattern`/`excludesPattern`）；表单校验前置（缺 url 的 remote 不出站），服务端仍是权威。remote `password` 一律不回显明文（NFR-S14 既有）。
- **删除**：非空仓需勾选 deleteContent 确认（对应 E-08 的 400/2xx 语义，UI 呈现原因）。
- **includesPattern/excludesPattern 启用（M1 勘误②收编）**：repo 配置新字段，默认 `**/*` / 空；**excludes 优先于 includes**（auth-model §4 高置信度）；不匹配的上传 → **409**、下载 → **404**（双值码，M1 勘误②定案值）；对 generic 与 maven/npm/pypi 的内容面生效（docker 域 P2 观察）。
- **remote 仓详情（P2）**：缓存统计只读视图（RE-11 端点 P2 维持，M3 预告的 M4 项收窄为 P2）；删缓存走既有 `DELETE /binflow/<remote>/<path>`。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-24-AC1 | W10：Playwright 表单建 `docker-ui-local`（local+docker）→ UI 列表出现该行；curl `GET /api/repositories/docker-ui-local` 的 `packageType=="docker"`（UI 与 REST 一致） | P0 |
| FR-24-AC2 | W11：UI 删非空仓——不勾 deleteContent → UI 呈现失败原因（对应 400）；勾选确认 → 成功且其后 `GET /binflow/docker-ui-local/**` 404 | P0 |
| FR-24-AC3 | W10b：UI 编辑 remote 仓 url → curl 单查回显新值；password 永不回显（GET 响应无明文/掩码形态固定） | P1 |
| FR-24-AC4 | W12a：includes/excludes 启用——仓配 `"includesPattern":"**/*.jar","excludesPattern":"secret/**"`：PUT `.txt` → 409（message 含 pattern）；PUT `secret/x.jar` → 409；GET excludes 命中路径 → 404；默认仓（未配）行为与 M1~M3 完全一致（回归） | P0 |
| FR-24-AC5 | UI 表单校验与服务端一致：缺 `url` 的 remote 提交 → 表单错误，服务端零写请求（网络面板断言）或 400 | P1 |
| FR-24-AC6 | W26 前置：表单可设 `quotaBytes`（正整数；0=不限）→ curl 单查回显 | P0 |

### FR-25 制品树浏览 / 上传 / 下载（web 前端）

**用户故事**：作为开发者，我在树里逐层展开仓库目录、看 size 与时间、页面直接上传文件、点下载拿到与命令行一致的 bytes——「仓库」在浏览器里是可触摸的。

行为规格：

- **树浏览**：逐层展开走既有 `GET /binflow/api/storage/{repo}/{path}`（E-09 children，按名排序）；virtual 仓走聚合视图（RE-09）；目录/文件图标区分、size/lastModified 列；**权限过滤自然生效**（无 read 权限的仓/路径不在树上，UI 不得绕过——与服务端同源判定）。
- **上传**：UI 上传 = `PUT /binflow/<repo>/<path>`（E-11 语义：201/409/403/413 原样呈现，含 quota 413 与 checksum 409 的原因文案）；上传目录可先建（尾斜杠 mkdir 语义 E-15）。
- **下载**：UI 触发 `GET /binflow/<repo>/<path>`（E-12），落盘内容与服务端 checksum 一致。
- **删除**：UI 删除走 E-14（204/404 幂等呈现）；无 delete 权限 → 403 原因呈现。
- **协议特化视图**：docker 仓显示 manifest/tag 层级、maven 仓按 GAV 目录语义、npm/pypi 按包目录（同一 storage 树，无专有端点）——P1。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-25-AC1 | W12：Playwright 展开 `generic-local` → `acme` → 行 `artifact.bin` 可见（size/时间列非空） | P0 |
| FR-25-AC2 | W12b：UI 下载 `artifact.bin` → 落盘文件 sha256 == curl 侧 item info 的 `checksums.sha256`（对账） | P0 |
| FR-25-AC3 | W13：UI 上传新文件到 `acme/` → 完成后树出现新行；curl item info 的 sha256 == 本地 `sha256sum` | P0 |
| FR-25-AC4 | W12c：docker 仓树可见 manifest/tag node；maven 仓树可见 GAV 目录（P1，抽查两协议各一） | P1 |
| FR-25-AC5 | W12d：read-only 用户（jane，仅 read）在 UI 删除文件 → 403 原因呈现，node 仍存在（curl 200） | P1 |
| FR-25-AC6 | 上传 1GB 文件经 UI → 成功且服务进程 RSS 增量 < 256MB（M1 NFR-P3 的 UI 版；UI 流式/分片形态归实现） | P1 |

### FR-26 搜索（REST + UI）

**用户故事**：作为开发者与 CI 作者，我用名字片段或 checksum 找到制品在哪个仓哪条路径——「这个东西推上来没有」不再靠逐仓翻树。

行为规格：

- **`GET /binflow/api/search/artifact?name=<frag>&repos=<csv>`（M1 E-26 的 search 域在 M4 打开）**：200 `{"results":[FileInfo...]}`（FileInfo 形态复用 E-09 字段集）；`name` 必填（缺 → 400 E-01）；`repos` 可选限定；**匹配语义暂行 = 路径/文件名子串（SQL LIKE），`*` 通配 P2**（Artifactory 语义待逆向校准，§5.5 K2）。
- **`GET /binflow/api/search/checksum?sha256=&sha1=&md5=&repos=`**：至少一个 checksum（全缺 → 400；格式非法 → 400）；精确命中返回全部引用该 blob 的 node（跨仓去重可见）；rest-api.md §4 高置信度。
- **权限过滤**：结果按调用者 ACL 过滤（无 read 的 repo 不出现；匿名按匿名通道语义——O4 定界维持）；admin 全见。
- **gavc（P2）**：`GET /api/search/gavc?g=&a=` 走 maven node 的路径结构匹配，兼容子集。
- **UI 搜索框（P1）**：name 搜索 → 结果表（repo/path/size）可跳转树视图。
- **未实现搜索族**：`/api/search/props|users|artifactory|pattern` 等 → 404 E-01（SR-04）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-26-AC1 | W14：`?name=artifact` → 200，results 含 `repo=="generic-local"` 与 `path=="/acme/artifact.bin"`；`?name=` 缺失 → 400 | P0 |
| FR-26-AC2 | W15：`?sha256=$SHA`（同 blob 两路径场景）→ results 恰含两条 node（跨仓去重可见性） | P0 |
| FR-26-AC3 | W16：`?name=x&repos=other-local` 过滤正确；`anonymous_access=false` 实例下 ci-bot 的结果不含其无 read 权限的 repo（ACL 过滤零泄漏，NFR-S24） | P0 |
| FR-26-AC4 | W14b：UI 搜索 `artifact` → 结果表出现且点击跳到树节点 | P1 |
| FR-26-AC5 | W14c：gavc 搜索（P2）：`?g=com.acme&a=demo-app` → maven 结果 | P2 |
| FR-26-AC6 | W36 边界：`/api/search/props?...` → 404（未实现族，SR-04） | P0 |

### FR-27 groups 与权限模型完整实现（dev-go-core：auth/metadata/004 迁移）

**用户故事**：作为安全管理员，我建 `devs` 组、把成员加进去、给组授 repo×path 权限——组成员变动即时生效，授权面 = 用户直接授权 ∪ 所属组授权，这就是 Artifactory 教会我的模型。

行为规格（对齐 Artifactory 概念模型；groups 端点族形态为暂行，待逆向校准 §5.5 K1）：

- **组实体**：`groups` = `{name, description}`（**组不带 admin 位**——有效 admin 判定维持 per-user，与 Artifactory 的组 admin 有意不兼容，SE-07 注记；auto-join 默认组 P2 不做）。组名规则：非空、≤64、`[a-z][a-z0-9._-]*`、保留字 `anonymous`/`_system_` → 400。
- **端点（兼容子集，路径 `/binflow/api/security/groups`）**：
  - `GET /api/security/groups` → 200 数组 `[{name, uri?, description}]`（uri 为详情链接，可选字段）；admin only；
  - `PUT /api/security/groups/{name}` → **201 无 body**（body `{name, description}`；body name ≠ 路径 → 400）；已存在 → 更新语义 200；
  - `POST /api/security/groups/{name}` → 200（部分更新，仅 description）；
  - `DELETE /api/security/groups/{name}` → 200 纯文本（成员关系解除；**被 permission target 引用 → 409**，message 列出引用的 target 名——BinFlow 暂行从严，防静默失去授权语义，§5.5 K3）；
  - 错误体走「用户管理纯文本」层（M1 §5.1 三分层沿用）。
- **成员关系**：用户的 `groups[]` 经 `PUT/POST /api/security/users/{name}` 维护（body 加 `groups:["devs"]`）；**组不存在 → 400**（文案对齐 auth-model §1.3-⑧：`Unable to find group by name '<g>'.`，高置信度）；GET 单用户回显 `groups`（SE-05 扩展回显：`{name, email, admin, groups[], realm, lastLoggedIn?}`）。
- **继承判定（核心语义）**：`Authorizer.Can(user, repo, path, action)` = Σ(permission targets 命中路径) 中 **user principals ∪ 所属 groups principals** 的动作并集；admin 用户隐式全权（M1 既有）；**组成员变动即时生效**（逐请求现算或缓存失效，实现归 architect）。M1 的 folder 尾斜杠契约、excludes 优先、同 checksum 幂等重传免覆盖检查等语义零变更。
- **有效权限视图**：`GET /api/storage/{repo}/{path}?permissions` → 200 `{"uri":..., "principals":{"users":{...},"groups":{...}}}`（r/w/d 位映射，rest-api.md §3 高置信度）；virtual/remote 仓 → 400（spec 同款限定，P2）。
- **email 落盘收编（M1 遗留）**：004 迁移给 users 加 email 列；PUT/POST 校验链不变（blank → 400），GET 回显。
- **审计**：组/权限变更落审计（FR-29 词表）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-27-AC1 | W17：组 CRUD——PUT `devs` → 201；GET 列表含 `devs`；POST 改 description → 200；GET 单组回显 | P0 |
| FR-27-AC2 | W18：`PUT users/jane`（含 `groups:["devs"]` + email）→ 201；GET 单用户 `groups==["devs"]` 且 email 回显；`groups:["nope"]` → 400（定案文案） | P0 |
| FR-27-AC3 | W19：组授权继承——target `devs-read`（groups{devs:[read,write]}，`devs/**`）：jane PUT `generic-local/devs/w.bin` → 201；DELETE 同路径 → 403（无 delete）；jane 对 `other/**` PUT → 403 | P0 |
| FR-27-AC4 | W19b：即时生效——jane 移出组（PUT users `groups:[]`）后同 PUT → 403（无重启、无延迟窗口） | P0 |
| FR-27-AC5 | W19c：并集语义——user 直接 read + 组 write：GET/PUT 均可；组与 user 同授 delete → 判定为有（一次成功） | P0 |
| FR-27-AC6 | W20：组删除保护——被 `devs-read` 引用时 DELETE → 409；删除 target 后 DELETE → 200；`GET users/jane` 的 groups 不再含 `devs`（联动清空） | P0 |
| FR-27-AC7 | W21：`GET .../devs/w.bin?permissions` → principals 含 jane（users）与 devs（groups）的授权位 | P1 |
| FR-27-AC8 | W40：email 落盘——PUT 用户带 email → GET 单用户回显一致（M1 遗留收编）；列表仍为 `{name,uri,realm}` 简形态 | P0 |
| FR-27-AC9 | 组名边界：`anonymous`/`_system_`/大写非法名 → 400；组不带 admin 位——admin 组成员的非 admin 用户对管理 API 仍 403（有意不兼容断言） | P0 |
| FR-27-AC10 | M1 权限语义零回归：C22/C27 复跑全绿（user-only 授权路径不变） | P0 |

### FR-28 用户 / 组 / 权限管理 UI（web 前端）

**用户故事**：作为管理员，我在 UI 里完成「建组 → 建用户入组 → 建 permission target 授 repo×path」三步，并在同一页面核对某用户的有效权限——权限管理不再是 JSON 手工活。

行为规格：用户页（列表/新建/编辑/改密入口/删除）、组页（列表/新建/成员维护/删除，删除被引用时呈现 409 原因）、permission target 页（name/repos/patterns/principals 的 users+groups 双栏，CRUD 走 `/api/v1/permissions` 既有端点）；token 管理页（列表/吊销，`GET /api/security/token` admin only）为 P2。表单字段集与 FR-27 服务端一致，错误呈现服务端原文（400/409 文案）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-28-AC1 | W33：Playwright 三步流——UI 建组 `qa-team` → 建用户 `bob` 入组 → 建 target（repo=generic-local, pattern=`qa/**`, principals: groups{qa-team:[read,write]}）→ curl 侧三者全部存在且字段正确 | P0 |
| FR-28-AC2 | W33b：UI 把 bob 移出组 → bob 会话内（第二浏览器上下文）同路径 PUT → 403（UI 即时生效呈现） | P1 |
| FR-28-AC3 | W33c：UI 删除被引用组 → 呈现 409 与引用 target 名；解除引用后可删 | P0 |
| FR-28-AC4 | UI 建 target 的 principals 同时含 user 与 group 授权 → curl GET `/api/v1/permissions/{name}` 回显双栏 | P1 |
| FR-28-AC5 | token 管理页（P2）：列表 + 吊销按钮 → 吊销后该 token 请求 401 | P2 |

### FR-29 审计日志查询面（dev-go-core：httpapi；audit_events 已有）

**用户故事**：作为安全审计员，我按人、按动作、按仓库、按时间窗检索操作历史，导出到报告里——并且我知道没有人（包括 admin）能改写历史。

行为规格：

- **`GET /binflow/api/v1/audit?repo=&actor=&action=&since=&until=&limit=&cursor=`（GE-01，architecture §7.1 预留位兑现）**：200 `{"events":[{id,time,actor,action,repo,path,detail}],"nextCursor":"..."}`（detail 为对象非字符串；time RFC3339 UTC；倒序）；limit 默认 100、上限 1000（超限 400）；`since/until` RFC3339 闭开区间；cursor 不透明。**admin only**（非 admin 403、未认证 401）。既有 `Filter{Repo,Actor,Limit}` 扩展为全参数（实现侧加索引：actor/time/action——归 architect）。
- **动作词表（M4 定案全集）**：M1 既有 `deploy/delete/download/login.success/login.failed/repo.create/repo.update/repo.delete/token.issue/token.revoke/password.change` + M4 新增 `group.create/group.update/group.delete/permission.create/permission.update/permission.delete/gc.run/export.run/import.run/quota.exceeded`。控制台登录复用 `login.success/login.failed`。
- **append-only 铁律**：无任何修改/删除审计行的 API（PUT/DELETE `/api/v1/audit*` → 404）；`quota.exceeded` 在 413 发生时落审计（actor/repo/path/used/quota 进 detail）。
- **脱敏**：Redact 链对新端点族同样生效（detail 无凭据明文，NFR-S3 延续）。
- **UI（P1）**：审计页过滤器（actor/action/repo/时间窗）+ 表格分页；CSV 导出 P2。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-29-AC1 | W22：`?actor=jane` → events 全部 `actor=="jane"`；`?action=login.failed` → ≥1（W05 产生）；`?since=&until=` 时间窗隔离正确；`?limit=1001` → 400 | P0 |
| FR-29-AC2 | W23：词表断言——构造 deploy/delete/repo.create/group.create/gc.run/quota.exceeded 各 ≥1 条并可查 | P0 |
| FR-29-AC3 | W23b：全量 audit 导出 grep——已发 token 明文、口令字面量、Authorization 头值均 0 命中（脱敏） | P0 |
| FR-29-AC4 | W39：`PUT/DELETE /api/v1/audit`（含 `/api/v1/audit/1`）→ 404（append-only 无修改面）；非 admin GET → 403、未认证 → 401 | P0 |
| FR-29-AC5 | W34：UI 审计页按 actor 过滤 → 表格行与 REST 同结果集（抽样对账） | P1 |
| FR-29-AC6 | 分页：`?limit=2` + 追随 nextCursor 两次 → 三页合计 == 无 limit 查询的 total（构造 ≥5 条事件） | P0 |

### FR-30 GC 管理化（dev-go-core：httpapi 接线 + 互斥）

**用户故事**：作为管理员，我先 dry-run 看看能回收多少磁盘、确认没有误伤引用，再 apply——垃圾回收从「SSH 上去跑 CLI」变成页面上的两次点击。

行为规格：

- **`POST /binflow/api/v1/system/gc`（GE-03）**：body `{"apply":bool, "graceHours":int?}`（graceHours 缺省用配置 `storage.gc_grace_hours`，默认 24；语义同 CLI：grace 基准 = blob mtime，ADR-0006）。dry-run → 200 `{"candidateCount":N,"candidateBytes":B,"deletedCount":0}`；apply → `{"candidateCount":N,"candidateBytes":B,"deletedCount":N}`。admin only。**同步执行**（Q6 暂行；mark-sweep 集合查询为既有实现）。
- **互斥（一致性关键）**：GC 与 export 共用 data 目录级锁——export 运行中 POST gc → **409**（E-01，message 含 `export in progress`）；GC 运行中 export CLI → 退出码非 0 并说明原因。锁形态（文件锁/进程内）归 architect，行为如上可观察。
- **物理删除唯一入口不变**：运行时 DELETE 制品只删 node 引用（ADR-0006 安全底线延续）；本端点是 blob 物理回收的第二个入口（与 CLI `gc` 等价语义）。
- **UI（P1）**：治理页 dry-run 显示候选数/字节 → apply 需二次确认 → 完成显示 deleted。**CLI `binflow-server gc` 行为零回归**（M2 O3 后既有）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-30-AC1 | W24：构造孤儿（上传→DELETE node；配 `graceHours:0`）→ dry-run 200 且 `candidateCount ≥ 1`；`/api/v1/storage/stats` 的 blob 数不变 | P0 |
| FR-30-AC2 | W25：apply → `deletedCount == candidateCount`、stats blob 数下降同量；**被引用 blob 幂等幸存**（用第二条引用同 blob 的路径 GET 200 验证——M1 C 序列手法）；再 dry-run → 0 候选 | P0 |
| FR-30-AC3 | W25b：互斥——后台 export（大 blob 拖长窗口）期间 POST gc → 409；反向 GC 长跑时 export CLI 退出码非 0（QA 手法注明构造方式） | P1 |
| FR-30-AC4 | W35：UI 治理页 dry-run → 显示候选 → apply 二次确认 → deleted 呈现；curl stats 对账一致 | P1 |
| FR-30-AC5 | CLI `gc -c/--apply/--grace-hours` 复跑 M2 O3 断言零回归；`gc.run` 落审计（actor=admin，REST 与 CLI 两条路径都落） | P0 |

### FR-31 配额（repo 级 quotaBytes）

**用户故事**：作为平台工程师，我给 `build-outputs` 仓设 500GB 上限——CI 失控推爆磁盘这件事，在仓库层就被挡住，且拒绝是原子的（不留半截文件）。

行为规格：

- **配置**：repo 配置扩展字段 `quotaBytes`（int64，**0 = 不限**，默认 0；BinFlow 扩展字段，同 `allowPrivateUpstream` 先例）。计量 = 该仓全部 node 引用 blob 的逻辑字节和（`SUM` 口径归实现；去重 blob 跨仓共享不重复计——**按本仓引用计**）。
- **执行点（P0 全覆盖）**：generic PUT / checksum-deploy 秒传、maven deploy（含旁车）、npm publish、pypi upload、docker blob PUT + manifest PUT、virtual 写路由（按目标 local 仓计量）。超限 → **413**（E-01，message 含 `quota exceeded` + used/quota 双值），**原子拒绝**（无部分写入：路径 404、stats 不变、docker 上传会话可弃）。**remote pull-through 落盘不计量**（Non-goal，§2.2）。
- **观测**：`GET /binflow/api/v1/storage/usage/{repo}`（GE-06）→ 200 `{"repo","usedBytes","quotaBytes"}`；quota 不影响读/删；`quota.exceeded` 落审计（FR-29）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-31-AC1 | W26：`quotaBytes:1024` 仓——PUT 800B → 201；再 PUT 800B → **413**（message 含 used/quota） | P0 |
| FR-31-AC2 | W26b：413 原子性——被拒路径 GET 404、stats blob 数不变、usage `usedBytes==800`（精确对账） | P0 |
| FR-31-AC3 | W26c：协议覆盖——docker push 超限 → 客户端报错且 manifest/blob 零残留；npm publish 超限 → CLI 报错 + 服务端 413（maven/pypi 各一 curl 抽验） | P0 |
| FR-31-AC4 | W27：读/删不受限——超限后 GET 已有制品 200、DELETE 204；删后空间释放，再传 → 201（used 回落正确） | P0 |
| FR-31-AC5 | 秒传与 mount 同受限（NFR-S23）：quota 仓内 `X-Checksum-Deploy` 秒传超限 → 413；docker mount 到 quota 仓超限 → 413 | P0 |
| FR-31-AC6 | `quotaBytes:0`（默认）行为与 M1~M3 完全一致（回归）；UI 仓详情显示 used/quota 进度（P1） | P0/P1 |

### FR-32 备份/恢复：export / import（dev-go-core CLI）

**用户故事**：作为运维，我在服务运行中做一次在线导出，拿到一份自包含快照（blobs + 元数据 + manifest）；空难恢复时在一台新机器上 import，一切照旧——包括 blob 的 mtime（grace 时钟不重置）。

行为规格（落实 ADR-0006「备份 = blobs/ + SQLite 快照」；一致性窗口为 Q4 暂行）：

- **`binflow-server export -c <cfg> --output <dir> [--tar]`（GE-07，在线执行）**：
  1. 获取 data 目录级锁（与 GC 互斥，FR-30-AC3）；
  2. SQLite **online backup API** 快照（不阻塞服务写入）落 `metadata.db`；
  3. 拷贝 `blobs/<2hex>/...`（**保留 mtime**：ADR-0006 勘误②硬约束——tar/rsync -a 默认满足）；
  4. 写 `manifest.json`：`{formatVersion, createdAt, binflowVersion, blobCount, totalBytes, metadata:{file, sha256}, graceNote}`。窗口内新增 blob 允许多拷（manifest 不引用 → import 后成 GC 候选）；**manifest 引用的 blob 缺失 = 导出损坏**（W28b 断言全存在）；
  5. 产物目录权限 0700（含凭据密文与口令哈希，NFR-S22）。
- **`binflow-server import -c <cfg> --input <dir> [--verify full]`（GE-08，停机执行）**：目标 data dir 必须为空（非空 → 退出码非 0 fail-fast）；校验 manifest（formatVersion 兼容、metadata sha256）；blob 完整性 **size 全验 + sha256 抽验（默认抽验前 100 个；`--verify full` 全量重哈希 P1）**；**缺失被引用 blob → fail-fast 退出码非 0**，目标目录清回空（无半恢复）；恢复 blobs（保留 mtime）+ metadata 文件就位；成功后正常 `serve` 启动。
- **恢复内容 = 全量状态**：repo 配置（含 quotaBytes/includes）、nodes/blobs、users/groups/permissions/tokens、audit_events、remote 凭据密文（`enc:v1:` 原样——import 侧未设 `BINFLOW_REMOTE_CREDENTIALS_KEY` 时启动 fail-fast，ADR-0012 既有行为在恢复链同样成立）。
- **不做**：增量/定时/不停机 import（§2.2）；Artifactory `/api/export|/api/import` REST 形态（GE-09）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-32-AC1 | W28：服务运行中 export → 退出码 0；输出含 `manifest.json`/`blobs/`/metadata 快照；manifest 字段齐全且 `metadata.sha256` 与文件实测一致 | P0 |
| FR-32-AC2 | W28b：manifest 引用的每个 blob 在输出目录存在（脚本遍历断言）；export 后新上传文件的 sha256 不在 manifest（窗口语义） | P0 |
| FR-32-AC3 | W30：往返保真——导出前构造：四协议各 ≥1 制品 + jane/devs/permission target + 一枚有效 token + quota 仓；import 到空 data dir 后：四协议 GET 200 且 sha256 一致、jane 登录 200、组授权矩阵成立（W19 复验）、token 仍可用、审计历史可查 | P0 |
| FR-32-AC4 | W31：完整性——抽删一个被引用 blob → import 退出码非 0 且明确报错、目标目录无半恢复；篡改 manifest blobCount → 同 fail-fast | P0 |
| FR-32-AC5 | W32：mtime 保留——导出源与 import 后同 blob 的 mtime 相等（`stat` 对比；grace 时钟不重置） | P0 |
| FR-32-AC6 | W30b：remote 凭据密文随行——export 含带凭据 remote 仓，import 侧无 env 密钥启动 → fail-fast（ADR-0012 行为恢复链成立）；设密钥后该仓代理链可用 | P1 |
| FR-32-AC7 | `--verify full` 全量 sha256 重哈希（P1）；`--tar` 单文件产物（P2） | P1/P2 |

### FR-33 端到端验收矩阵（Playwright + 回归基线 + 性能；qa + 全角色）

**用户故事**：作为评估者，「有控制台」不是截图自证——浏览器自动化全链走通、三个里程碑的既有行为零回归、性能不回退，才算数。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-33-AC1 | 浏览器矩阵（§5.3）：Chromium 走通 W09/W10/W11/W12/W13/W33/W34/W35 全链 exit 0 | P0 |
| FR-33-AC2 | W36：回归基线——M1 C 序列 P0 + M2 D 序列 P0 + M3 M 序列 P0 复跑全绿（断言按 §5.6 反转表更新） | P0 |
| FR-33-AC3 | W37：性能——空库冷启动 < 2s（含 embed）；SPA 首屏加载 < 3s（本机 Chromium，P1 记录）；100 并发 REST 混合（session+search+storage/usage）退出码全 0、零 5xx | P0 |
| FR-33-AC4 | WebKit / Firefox：登录 + 树浏览 + 上传三条链（P2 观察，不作门槛） | P2 |
| FR-33-AC5 | 部署烟测（release-engineer）：compose 起的实例上控制台可登录、建仓、上传（非本机 bare 进程） | P1 |
| FR-33-AC6 | 文档（tech-writer）：控制台使用指南 + 权限/组管理 + 治理（审计/GC/配额）+ 备份恢复操作手册（含 mtime/密钥注意事项） | P1（DoD 第 4 条） |

---

## 5. 兼容性矩阵（M4 核心）

### 5.1 层级定义（沿用 M1 §5.1 四层，M4 特化）

- **控制台域（CE）对齐基准 = BinFlow 自有**：Artifactory 的 Web UI 是私有前端、无公开兼容承诺——BinFlow 控制台不承诺其 UI 兼容；**兼容性承诺落在 UI 消费的 REST 面上**（90% 为 M1~M3 既有端点）。
- **安全域（SE：groups/用户扩展/权限视图）对齐基准 = Artifactory 行为**（auth-model.md；groups 端点族为暂行待校准）。
- **搜索域（SR）对齐基准 = Artifactory 公开 REST**（rest-api.md §4 已覆盖 checksum；artifact/gavc 待校准）。
- **治理域（GE：审计/GC/配额/备份）对齐基准 = 无对应或语义等价改道**——Artifactory 的审计 UI/prune/全量导出或为付费功能或为异步任务框架语义，BinFlow 走 `/api/v1` 与 CLI 自有形态（语义等同但路径不同 / 自有）。
- 错误契约沿用 M1 §5.1 三分层：session/audit/gc/usage/search 走 **E-01 `errors[]`**；groups/用户族走**用户管理纯文本层**。

### 5.2 M4 端点矩阵

「置信度」：高 = 逆向规格明文（auth-model/rest-api）或用户定案；中 = PRD 暂行（待 §5.5 校准）。编号前缀：CE = 控制台/会话，SE = 安全（groups/用户/权限视图），SR = 搜索，GE = 治理（审计/GC/配额/备份）。

> **对齐注记（v1.1，R1）**：CE 域（CE-01~07）与 **ADR-0014（T-108 勘误后）对齐**——命名面以本表为准（`/binflow/ui/**`、保留字 `ui`、301、cookie `binflow_session`；tech-lead 裁决「PRD 面 + ADR 内核」，ADR-0014 的 `/binflow/console/**` 命名由 T-108 勘误对齐），机制内核以 ADR-0014 为准（server-side session、CSRF 分层、无 console 专属 API 树、vite+React 构建链）。

| # | 端点（方法 路径） | 行为要点 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| CE-01 | `GET /binflow/` | 301 → `/binflow/ui/`（console 挂载入口；M1 占位 JSON 语义终结） | 自有 | P0 | — | W01 |
| CE-02 | `GET /binflow/ui/**` | SPA shell + 深链 history fallback；指纹资产 `Cache-Control: immutable`、shell `no-cache`；React 同栈（ADR-0011） | 自有 | P0 | — | W01/W02 |
| CE-03 | `POST /binflow/api/v1/session` | JSON/form 双形态；200 `{username,admin}` + `Set-Cookie binflow_session; HttpOnly; Path=/binflow; SameSite=Lax`；错凭据 401 E-01（不泄露存在性）；`login.success/login.failed` 落审计 | 自有（Q1 暂行 server-side） | P0 | — | W04/W05 |
| CE-04 | `GET /binflow/api/v1/session` | whoami：200 当前主体 / 401；TTL 到期 401 | 自有 | P0 | — | W06/W08 |
| CE-05 | `DELETE /binflow/api/v1/session` | 204；session 服务端吊销（cookie 重放 401）；重启不掉线（落库） | 自有 | P0 | — | W07/W37a |
| CE-06 | session 认证的写操作 Origin 校验 | 非同源 Origin + cookie 认证的写请求 → 403（CSRF 暂行防线）；Basic/Token 请求不受影响 | 自有 | P0 | — | W38 |
| CE-07 | repo key 保留字 `ui` | 建仓 400（ADR-0008 增补）；存量 `ui` 仓启动 WARN（P2） | 自有（行为变更） | P0 | — | W01b |
| SE-01 | `GET /binflow/api/security/groups` | 200 数组 `[{name,description,uri?}]`；admin only；错误体纯文本层 | 兼容（子集） | P0 | 中（K1） | W17 |
| SE-02 | `PUT /binflow/api/security/groups/{name}` | 201 无 body（body `{name,description}`；name 不符 → 400）；已存在 → 200 更新 | 兼容（子集） | P0 | 中（K1） | W17 |
| SE-03 | `POST /binflow/api/security/groups/{name}` | 200 部分更新（description）；404 不存在 | 兼容（子集） | P1 | 中（K1） | W17 |
| SE-04 | `DELETE /binflow/api/security/groups/{name}` | 200 纯文本；**被 permission target 引用 → 409（BinFlow 暂行从严，K3）**；成员联动解除 | 兼容（409 分支为 BinFlow 决策） | P0 | 中（K3） | W20 |
| SE-05 | `GET /binflow/api/security/users/{name}`（M4 补齐单查） | 200 `{name,email,admin,groups[],realm,lastLoggedIn?}`；无口令字段；404 无 body（auth-model §1.2） | 兼容（子集） | P0 | 高 | W18/W40 |
| SE-06 | `PUT/POST users` 的 `groups[]` 字段 | 组不存在 → 400 `Unable to find group by name '<g>'.`（§1.3-⑧ 高置信度）；email 落盘回显（M1 遗留收编） | 兼容 | P0 | 高 | W18/W40 |
| SE-07 | 权限继承判定 | 有效权限 = user principals ∪ 所属 groups principals（并集、即时生效）；**组无 admin 位**（有效 admin 维持 per-user——Artifactory 组 admin 有意不兼容） | 兼容（判定语义）/ 有意不兼容（组 admin） | P0 | 高（并集）/ 中（即时性为实现要求） | W19/W19b/W19c |
| SE-08 | `GET /binflow/api/storage/{repo}/{path}?permissions` | 200 `{"uri","principals":{"users","groups"}}`（r/w/d 位）；virtual/remote → 400（P2） | 兼容（子集） | P1 | 高（rest-api §3） | W21 |
| SE-09 | `/api/v2/security/permissions/**` | 不做 → 404（M1 E-24 评估收口：维持 `/api/v1/permissions`） | 有意不兼容 | P0 | — | W36 |
| SR-01 | `GET /binflow/api/search/artifact?name=&repos=` | 200 `{"results":[FileInfo]}`；name 必填（缺 400）；**匹配语义暂行子串**（K2）；结果按 ACL 过滤 | 兼容（子集） | P0 | 中（K2） | W14/W16 |
| SR-02 | `GET /binflow/api/search/checksum?sha256=&sha1=&md5=&repos=` | 至少一值（全缺/非法 → 400）；返回全部引用 node（跨仓去重可见） | 兼容 | P0 | 高（rest-api §4） | W15 |
| SR-03 | `GET /binflow/api/search/gavc?g=&a=&v?=&repos=` | maven GAV 结构匹配 | 兼容（子集） | P2 | 中 | W14c |
| SR-04 | `/api/search/props|users|artifactory|pattern|badge...` | 不做 → 404 + E-01（M1 E-26 的 search 域仅打开 artifact/checksum/(gavc P2) 三口） | 有意不兼容 | P0 | — | W36 |
| GE-01 | `GET /binflow/api/v1/audit?repo=&actor=&action=&since=&until=&limit=&cursor=` | architecture §7.1 预留兑现；倒序 + cursor 分页（limit 默认 100/上限 1000，超限 400）；admin only；append-only（无修改端点） | /api/v1 | P0 | — | W22/W39 |
| GE-02 | 审计动作词表扩展 | 新增 group.\*/permission.\*/gc.run/export.run/import.run/quota.exceeded（全表见 FR-29） | 自有（词表） | P0 | — | W23 |
| GE-03 | `POST /binflow/api/v1/system/gc` | `{"apply","graceHours"?}`；dry-run 报告 / apply 执行；同步（Q6 暂行）；与 export 互斥（409）；admin only | 语义等同但路径不同（Artifactory prune 不做，GE-04） | P0 | — | W24/W25/W25b |
| GE-04 | `POST /api/system/storage/prune/start`、`GET .../prune/status` | 不做 → 404（Artifactory 异步 prune 语义与 BinFlow mark-sweep 不同，改道 GE-03） | 有意不兼容 | P0 | 中（rest-api §5） | W36 |
| GE-05 | repo 配置 `quotaBytes` + 超限 413 | BinFlow 扩展字段（0=不限）；五协议上传全覆盖；原子拒绝；remote pull-through 不计量 | 自有（扩展字段） | P0 | — | W26/W27 |
| GE-06 | `GET /binflow/api/v1/storage/usage/{repo}` | `{repo,usedBytes,quotaBytes}`；admin or 有 read 权限用户 | /api/v1 | P0 | — | W26b |
| GE-07 | `binflow-server export`（CLI） | 在线导出：锁互斥 → SQLite backup 快照 → blobs 拷贝（保 mtime）→ manifest.json（含 metadata.sha256）；产物 0700 | 自有（CLI；Artifactory export REST 不承诺） | P0 | — | W28 |
| GE-08 | `binflow-server import`（CLI） | 停机恢复到空 data dir；size 全验 + sha256 抽验（`--verify full` P1）；缺失引用 fail-fast 无半恢复；保 mtime | 自有（CLI） | P0 | — | W30/W31/W32 |
| GE-09 | `/api/export/**`、`/api/import/**` | 不做 → 404 + E-01 | 有意不兼容 | P0 | — | W36 |

> 计数：**29 条**。兼容/兼容（子集）**11**（SE-01~08、SR-01~03，其中 SE-04 的 409 分支、SE-07 的组 admin 分支为行内有意不兼容点）；`/api/v1` / 自有（含 CLI/扩展字段/词表/行为变更）**13**（CE-01~07、GE-01/02/05/06/07/08）；语义等同但路径不同 **1**（GE-03）；有意不兼容 **4**（SE-09、SR-04、GE-04、GE-09）。另：仓库管理/树/上传 UI 消费的端点零新增（E-04~E-15、RE-01~09 既有），不在本表重复计数。

### 5.3 浏览器与客户端分级矩阵（conformance 判定标准）

「全过」定义：所列操作退出码 0（Playwright：断言全过）且服务端日志无 5xx。

| 客户端 | 必测操作 | 分级 |
|---|---|---|
| Chromium（Playwright） | 登录/登出、建仓/删仓、树浏览、上传、下载、搜索框、组/用户/权限三步流、审计过滤、GC dry-run→apply | **P0 必须全过** |
| curl（session + REST 面） | W01~W08、W14~W31、W38~W40（cookie jar 会话 + 全部新端点正负断言） | **P0 必须全过** |
| WebKit / Firefox（Playwright） | 登录 + 树浏览 + 上传 | P2 观察（不作门槛） |
| CLI（binflow-server） | `export`/`import`/`gc`（含互斥与旗标） | **P0 必须全过** |
| 真实协议客户端回归 | docker/mvn/npm/pip 各一条链（FR-33-AC2 基线内） | **P0 必须全过** |

### 5.4 M4 核心验收命令（W 序列，QA 直接引用）

> `$BASE/$ADMIN_PW` 沿用 §4 约定；Playwright（`web/e2e/`，baseURL `$BASE`，node ≥ 20）为浏览器面；选择器 testid 以 console-ui 规范为准（示例为占位，QA 按 ux 票终稿替换）。

```bash
# 环境准备
export BASE=http://localhost:8080
export ADMIN_PW=password
export UI=$BASE/binflow/ui
JAR=/tmp/bf-cookie.jar; rm -f $JAR

# ---- 控制台基座（FR-23） ----
# W01 SPA 承载与深链（CE-01/02）
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' $BASE/binflow/    # 301 http://localhost:8080/binflow/ui/
curl -s $UI/ | grep -c 'id="root"'                                        # 1（SPA 挂载点）
curl -s -o /dev/null -w '%{http_code}\n' $UI/repositories                 # 200（深链 fallback）
# W01b 保留字 ui（CE-07）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/ui -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic"}' -o /dev/null -w '%{http_code}\n'   # 400
# W02 资产缓存头（CE-02）
ASSET=$(curl -s $UI/ | grep -o '/binflow/assets/[^"]*\.js' | head -1)
curl -sI $BASE$ASSET | grep -i 'cache-control'        # 含 immutable
curl -sI $UI/ | grep -i 'cache-control'               # no-cache
# W03/W04/W04b 登录（CE-03）
curl -s -o /dev/null -w '%{http_code}\n' $BASE/binflow/api/v1/session    # 401（未认证 whoami）
curl -sc $JAR -X POST $BASE/binflow/api/v1/session -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}"                # 200 {"username":"admin","admin":true}
curl -si -X POST $BASE/binflow/api/v1/session -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PW\"}" | grep -i '^set-cookie'   # HttpOnly; Path=/binflow; SameSite=Lax
# W05 错误凭据（CE-03；401 同文案不泄露存在性）
curl -s -X POST $BASE/binflow/api/v1/session -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"wrong"}' -o /dev/null -w '%{http_code}\n'      # 401
# W06 whoami（CE-04）
curl -sb $JAR $BASE/binflow/api/v1/session | jq -r .username             # admin
# W07 登出与吊销（CE-05）
curl -sb $JAR -X DELETE $BASE/binflow/api/v1/session -o /dev/null -w '%{http_code}\n'   # 204
curl -sb $JAR -o /dev/null -w '%{http_code}\n' $BASE/binflow/api/v1/session              # 401
# W08 TTL 过期（覆盖键 console.session_ttl_seconds=5 重启后；主键 hours 见 FR-23）：重新登录 → sleep 6 → W06 同命令 401
# W37a 重启不掉线：登录 → restart 服务 → W06 同命令 200（P1）

# ---- 浏览器面（FR-23/24/25/28；Playwright Chromium） ----
cd web && npm ci && npx playwright install chromium
cat > e2e/w09-login.spec.ts <<'EOF'
import { test, expect } from '@playwright/test';
test('login lands on app shell', async ({ page }) => {
  await page.goto('/binflow/ui/');
  await page.fill('[data-testid="login-username"]', 'admin');
  await page.fill('[data-testid="login-password"]', process.env.ADMIN_PW!);
  await page.click('[data-testid="login-submit"]');
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible();
});
EOF
npx playwright test e2e/w09-login.spec.ts --project=chromium        # exit 0
# W10 建仓（FR-24-AC1）：repositories 页表单建 docker-ui-local（local+docker）→ 行可见；
#   交叉核验：curl -su admin:$ADMIN_PW $BASE/binflow/api/repositories/docker-ui-local | jq -r .packageType  # docker
# W10b 编辑 remote url（FR-24-AC3）：UI 改 url → curl 单查回显新值；响应无 password 明文
# W11 删仓（FR-24-AC2）：UI 删非空 docker-ui-local——不勾 deleteContent → 失败原因可见；
#   勾选确认 → 成功；curl GET /binflow/docker-ui-local/x → 404
# W12 树浏览（FR-25-AC1）：artifacts 页展开 generic-local → acme → 行 artifact.bin（size/时间非空）
# W12b 下载对账（FR-25-AC2）：download 事件 saveAs 后 sha256sum == curl item info 的 checksums.sha256
# W13 UI 上传（FR-25-AC3）：setInputFiles 上传 uploaded-ui.bin 到 acme/ → 行出现；
#   curl $BASE/binflow/api/storage/generic-local/acme/uploaded-ui.bin | jq -r .checksums.sha256 对账
# W12d 删除与 403（FR-25-AC5）：read-only 用户 UI 删除 → 403 原因呈现，curl GET 仍 200
# W14b 搜索框（FR-26-AC4）：输入 artifact → 结果表 → 点击跳树节点
# W33 权限三步流（FR-28-AC1）：UI 建组 qa-team → 建用户 bob 入组 → 建 target（qa/**，组 rw）；
#   curl -su admin:$ADMIN_PW $BASE/binflow/api/security/groups/qa-team 与 /api/security/users/bob
#   与 /api/v1/permissions 三者字段正确；W33b：UI 移出组 → bob 第二上下文同路径 PUT 403；
#   W33c：UI 删被引用组 → 409 与 target 名呈现
# W34 审计页（FR-29-AC5）：actor=admin 过滤 → 行与 REST ?actor=admin 结果集一致（抽样）
# W35 GC 页（FR-30-AC4）：dry-run 显示候选 → apply 二次确认 → deleted 呈现，curl stats 对账

# ---- repo 治理字段（FR-24） ----
# W12a includes/excludes 双值码（FR-24-AC4；M1 勘误②收编）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/pattern-local -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","includesPattern":"**/*.jar","excludesPattern":"secret/**"}' \
  -o /dev/null -w '%{http_code}\n'                                       # 200
printf 'x' > t.txt; printf 'y' > t.jar
curl -su admin:$ADMIN_PW -T t.txt $BASE/binflow/pattern-local/a/t.txt -o /dev/null -w '%{http_code}\n'    # 409
curl -su admin:$ADMIN_PW -T t.jar $BASE/binflow/pattern-local/secret/t.jar -o /dev/null -w '%{http_code}\n'  # 409
# GET excludes 命中路径 → 404；默认仓（未配 pattern）t.txt PUT → 201（行为不变）

# ---- 搜索（FR-26） ----
# W14 artifact 搜索（SR-01）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/artifact?name=artifact" | jq -r '.results[].path' \
  | grep -x '/acme/artifact.bin'                                          # 退出码 0
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/artifact" -o /dev/null -w '%{http_code}\n'    # 400
# W15 checksum 搜索（SR-02；先构造同 blob 两路径：C07 场景）
SHA=$(sha256sum artifact.bin | cut -d' ' -f1)
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/checksum?sha256=$SHA" | jq '.results | length'  # ≥2
# W16 过滤与 ACL（SR-01/SR-02；anonymous_access=false 实例）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/artifact?name=artifact&repos=other-local" | jq '.results | length'  # 0
curl -su ci-bot:ci-pw   "$BASE/binflow/api/search/artifact?name=artifact" | jq -r '.results[].repo' | sort -u  # 仅其有 read 的 repo
# W14c gavc（P2）：curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/gavc?g=com.acme&a=demo-app"

# ---- groups 与权限继承（FR-27） ----
# W17 组 CRUD（SE-01~03）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/groups/devs -H 'Content-Type: application/json' \
  -d '{"name":"devs","description":"M4 QA"}' -o /dev/null -w '%{http_code}\n'    # 201
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/groups | jq -r '.[].name' | grep -x devs
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/groups/devs -H 'Content-Type: application/json' \
  -d '{"description":"updated"}' -o /dev/null -w '%{http_code}\n'                # 200
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/groups/devs | jq -r .description   # updated
# W18 成员与 email（SE-05/06；M1 遗留收编）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/jane -H 'Content-Type: application/json' \
  -d '{"name":"jane","email":"jane@example.com","password":"jane-pw","admin":false,"groups":["devs"]}' \
  -o /dev/null -w '%{http_code}\n'                                               # 201
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users/jane | jq -c '{groups,email}'   # {"groups":["devs"],"email":"jane@example.com"}
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/jane2 -H 'Content-Type: application/json' \
  -d '{"name":"jane2","email":"j2@example.com","password":"x","admin":false,"groups":["nope"]}' \
  -o /dev/null -w '%{http_code}\n'                                               # 400
# W19 组授权继承（SE-07）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions -H 'Content-Type: application/json' -d '{
  "name":"devs-rw","repos":["generic-local"],"includePatterns":["devs/**"],
  "principals":{"groups":{"devs":["read","write"]}}}' -o /dev/null -w '%{http_code}\n'    # 2xx
printf 'w' > w.bin
curl -su jane:jane-pw -T w.bin $BASE/binflow/generic-local/devs/w.bin -o /dev/null -w '%{http_code}\n'   # 201
curl -su jane:jane-pw -X DELETE $BASE/binflow/generic-local/devs/w.bin -o /dev/null -w '%{http_code}\n'  # 403
curl -su jane:jane-pw -T w.bin $BASE/binflow/generic-local/other/o.bin -o /dev/null -w '%{http_code}\n'  # 403
# W19b 即时生效：PUT users/jane groups:[] → 201；jane 再 PUT devs/w2.bin → 403
# W19c 并集：user 直接 read + 组 write → GET/PUT 均可；双授 delete → 删除成功
# W20 组删除保护（SE-04）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/groups/devs -o /dev/null -w '%{http_code}\n'  # 409（被 devs-rw 引用）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/v1/permissions/devs-rw -o /dev/null -w '%{http_code}\n'  # 2xx
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/security/groups/devs -o /dev/null -w '%{http_code}\n'  # 200
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users/jane | jq -c .groups   # []（联动清空）
# W21 有效权限视图（SE-08；重建 devs/jane/devs-rw 后）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/storage/generic-local/devs/w.bin?permissions" | jq -c .principals  # 含 jane 与 devs

# ---- 审计（FR-29） ----
# W22 查询面（GE-01）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?actor=jane&limit=50" | jq -r '.events[].actor' | sort -u   # jane
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=login.failed&limit=10" | jq '.events | length'      # ≥1
SINCE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?since=$SINCE&limit=5" | jq '.events | length'              # 仅新事件
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?limit=1001" -o /dev/null -w '%{http_code}\n'               # 400
curl -su ci-bot:ci-pw $BASE/binflow/api/v1/audit -o /dev/null -w '%{http_code}\n'                               # 403
curl -s $BASE/binflow/api/v1/audit -o /dev/null -w '%{http_code}\n'                                             # 401
# W22b 分页（FR-29-AC6）：limit=2 + nextCursor 两次 → 三页合计 == 全量 total（≥5 条事件）
# W23 词表 + W23b 脱敏（GE-02；NFR-S3）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?limit=1000" > /tmp/audit.json
for a in deploy delete repo.create group.create login.failed; do
  jq -e --arg a "$a" '.events[].action | select(. == $a)' /tmp/audit.json >/dev/null && echo "OK $a"
done
grep -c "$ADMIN_PW" /tmp/audit.json           # 0（口令字面量零命中）
TOKEN=$(curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token -d 'grant_type=client_credentials' | jq -r .access_token)
grep -c "$TOKEN" /tmp/audit.json              # 0（token 明文零命中）
# W39 append-only（GE-01）
curl -sb $JAR -X DELETE $BASE/binflow/api/v1/audit/1 -o /dev/null -w '%{http_code}\n'   # 404（无修改面）
curl -sb $JAR -X PUT $BASE/binflow/api/v1/audit -o /dev/null -w '%{http_code}\n'       # 404

# ---- GC（FR-30） ----
# W24 dry-run（GE-03；构造孤儿：上传→DELETE，graceHours:0 令其可回收）
curl -su admin:$ADMIN_PW -T t.jar $BASE/binflow/generic-local/gc/a.jar -o /dev/null -w '%{http_code}\n'  # 201
B1=$(curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/stats | jq .blobs)
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/generic-local/gc/a.jar -o /dev/null -w '%{http_code}\n' # 204
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/system/gc -H 'Content-Type: application/json' \
  -d '{"apply":false,"graceHours":0}'    # 200 candidateCount ≥1
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/stats | jq .blobs   # == B1（未动）
# W25 apply（引用幸存：另建一条引用同 blob 的路径再删其一）
curl -su admin:$ADMIN_PW -T t.jar $BASE/binflow/generic-local/gc/keep.jar -o /dev/null -w '%{http_code}\n'  # 201
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/system/gc -H 'Content-Type: application/json' \
  -d '{"apply":true,"graceHours":0}' | jq '.deletedCount'    # == candidateCount
curl -su admin:$ADMIN_PW $BASE/binflow/generic-local/gc/keep.jar -o /dev/null -w '%{http_code}\n'  # 200（被引用幸存）
# W25b 互斥：大 blob 后台 export 期间 POST gc → 409；反向 GC 长跑中 export CLI 退出码非 0（QA 注明手法）

# ---- 配额（FR-31） ----
# W26 超限 413（GE-05）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/tiny -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","quotaBytes":1024}' -o /dev/null -w '%{http_code}\n'   # 200
head -c 800 /dev/urandom > a800.bin
curl -su admin:$ADMIN_PW -T a800.bin $BASE/binflow/tiny/a.bin -o /dev/null -w '%{http_code}\n'   # 201
curl -su admin:$ADMIN_PW -T a800.bin $BASE/binflow/tiny/b.bin -o /dev/null -w '%{http_code}\n'   # 413（message 含 used/quota）
# W26b 原子性与 usage（GE-06）
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/tiny/b.bin                # 404
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/usage/tiny | jq '.usedBytes'                # 800
# W26c 协议覆盖：docker push/npm publish 到 tiny 超限 → 客户端报错 + 服务端 413 + 零残留（maven/pypi curl 抽验）
# W27 读删不受限与回落（FR-31-AC4）：GET a.bin 200；DELETE 204；再传 800B → 201；usage 回落后再验证

# ---- 备份/恢复（FR-32） ----
# W28 在线 export（GE-07；服务运行中）
./bin/binflow-server export -c binflow.yaml --output /tmp/bk42; echo $?     # 0
jq '{formatVersion,blobCount,totalBytes,metadata}' /tmp/bk42/manifest.json  # 字段齐全
sha256sum /tmp/bk42/metadata.db | cut -d' ' -f1   # == manifest.metadata.sha256
ls /tmp/bk42/metadata.db /tmp/bk42/blobs >/dev/null && echo LAYOUT_OK
# W28b 窗口语义：manifest 引用的每个 blob 存在（遍历断言）；export 后新上传文件 sha256 ∉ manifest
# W30 往返保真（GE-08）：导出前构造 四协议制品+jane/devs/权限/token+quota 仓
BINFLOW_DATA_DIR=/tmp/fresh-data ./bin/binflow-server import -c binflow.yaml --input /tmp/bk42; echo $?  # 0
BINFLOW_DATA_DIR=/tmp/fresh-data BINFLOW_ADMIN_PASSWORD=$ADMIN_PW ./bin/binflow-server serve -c binflow.yaml &
#   断言：四协议 GET 200 sha256 一致；jane 登录 200；W19 组授权矩阵复验；token Bearer 可用；/api/v1/audit 历史可查
# W30b remote 凭据密文随行（FR-32-AC6）：带凭据 remote 仓导出→无 env 密钥启动 fail-fast；设密钥后代理链通
# W31 完整性 fail-fast：rm /tmp/bk42/blobs/<2hex>/<被引用 sha256> → import 退出码非 0 且 /tmp/fresh2 为空；
#   jq '.blobCount += 1' 篡改 manifest → import 退出码非 0
# W32 mtime 保留：导出源与 import 后同 blob 的 stat mtime 相等（grace 时钟不重置）

# ---- CSRF / 回归 / 性能（FR-23/33） ----
# W38 同源校验（CE-06；先用有效 session）
curl -sb $JAR -X PUT --data-binary 'csrf' $BASE/binflow/generic-local/acme/csrf.bin \
  -H 'Origin: http://evil.example' -o /dev/null -w '%{http_code}\n'    # 403
curl -sb $JAR -X PUT --data-binary 'csrf' $BASE/binflow/generic-local/acme/csrf.bin \
  -o /dev/null -w '%{http_code}\n'                                     # 201（无 Origin/同源）
# W40 email（SE-05）：见 W18 的 jq 断言
# W36 回归基线：M1 C 序列 P0 + M2 D 序列 P0 + M3 M 序列 P0 复跑（断言按 §5.6 反转表更新）；
#   边界抽查：/api/search/props → 404；/api/system/storage/prune/start → 404；
#   /api/v2/security/permissions/x → 404；/api/export/x → 404
# W37 性能：空库冷启动 < 2s（含 embed，ping 计时）；SPA 首屏 < 3s（Chromium trace，P1）；
#   100 并发混合：seq 100 | xargs -P100 -I{} curl -sf -b $JAR "$BASE/binflow/api/v1/session" → 全 0、零 5xx
```

### 5.5 待校准项（M4 逆向票落地后回写，流程同 M1 §5.5 / M3 §5.5）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K1 | groups 端点族形态（GET 列表元素字段、PUT 201 无 body / POST 200 / DELETE 200 纯文本与文案） | 201 无 body（对齐 users PUT 先例）/ 删除 200 纯文本 | auth-model.md 扩编（reverse-engineer 并行票） |
| K2 | `/api/search/artifact` 的 name 匹配语义（子串 vs 前缀 vs 通配）与 results 字段形态 | 子串（SQL LIKE）；FileInfo 形态复用 | 同上 + rest-api.md 扩编 |
| K3 | 组删除被 permission target 引用的语义 | 409 拒绝（BinFlow 从严：显式优于静默失权） | Artifactory 行为若为级联删引用则评估是否跟进（P2） |

> （v1.1 注）K1~K3 暂行值已由 tech-lead 按 v1.0 **写死进 T-97/T-92 派单口径**（实现按暂行推进）；**待逆向扩编票落地后按本表流程回写**——校准值若与暂行冲突，以校准值为准并同步勘误 T-97/T-92 的对应断言。

### 5.6 回归基线反转表（M4 起生效，qa 更新既有断言）

| 既有断言 | 来源 | M4 起的期望 |
|---|---|---|
| `/binflow/api/search/**` → 404 | M1 E-26 / C24 | 按 SR-01/02 分派（artifact/checksum 200）；未实现族（props/users 等）仍 404（SR-04） |
| `GET /binflow/` 返回占位 JSON | M1 架构 console 占位 | 301 → `/binflow/ui/`（CE-01）；SPA 深链 200 |
| `/binflow/api/security/groups/**` → 404 | M1 E-26 | 按 SE-01~04 分派 |
| `GET /binflow/api/v1/audit` → 404 | 未实现（架构预留） | 200（GE-01） |
| repo key `ui` 可建仓 | M1 FR-3-AC4 合法字符集内 | 400 保留字（CE-07）；存量 `ui` 仓启动 WARN（P2） |
| `GET /api/security/users` 列表元素 | M1 E-19 `{name,uri,realm}` | 不变（单用户 GET 扩展回显 email/groups——SE-05，双端点分工） |
| M1 C / M2 D / M3 M 序列 P0 | T-18/19/43/44/74/75/76 基线 | 全部复跑全绿（五协议与权限面零回归是 M4 的 P0 硬门槛） |

---

## 6. 非功能需求（NFR）与遗留收编

### 6.1 性能（M4 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P16 冷启动不回退 | 空库启动到 ping `OK` < 2s（含 go:embed SPA 资产，W37）；二进制大小与 SPA gzip 体积记录（总预算 40MB 由 M5 check-size 把关，超限记录不阻塞 M4） | P0 |
| NFR-P17 查询面 | 10 万 node 规模仓：`?name=` 搜索 P95 < 1s；10 万条 audit 上 `?actor=&since=` 查询 P95 < 500ms（QA 造数脚本；无全表扫描计划——索引归 architect，004 迁移） | P0 |
| NFR-P18 控制台 API | 树展开（children 1000 子项目录）P95 < 300ms；单目录超大（>10k 子项）分页为 P2，超限不 5xx | P1 |
| NFR-P19 export 吞吐 | 记录项：≥ 100MB/s（机械盘下限，P1 记录不设门）；import 同口径记录 | P1 |
| NFR-P20 并发 | 100 并发 REST 混合（session/search/usage/children）零 5xx（W37）；50 并发 Playwright 登录无 session 串号（P1 抽查） | P0/P1 |

### 6.2 安全底线（M4 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S19 会话安全 | cookie 三属性（HttpOnly/Path=/binflow/SameSite=Lax）；服务端可吊销；固定 TTL；错误文案不泄露用户存在性；session 值为高熵随机（≥ 128bit，不可枚举）；日志不记 session 值 | W04b/W05/W07/NFR-S3 延续 |
| NFR-S20 CSRF | cookie 认证的写操作强制 Origin 同源校验（403）；SameSite=Lax 双保险；Basic/Token 路径不受影响（CI 零感知） | W38 |
| NFR-S21 审计 append-only | 无任何 API 可修改/删除审计行（404）；metadata 层无 UPDATE/DELETE audit 的代码路径（评审项）；查询端点 admin only | W39 + 代码评审 |
| NFR-S22 export 产物敏感面 | 产物含口令哈希与 `enc:v1:` 凭据密文——目录权限 0700；manifest 不含任何明文秘密；tech-writer 手册明示「产物按密文同等保管」 | W28 权限断言 + 文档 |
| NFR-S23 配额绕过封锁 | checksum-deploy 秒传、docker mount、virtual 写路由、分块上传全部过配额检查（负断言全 413） | W26 变体 |
| NFR-S24 搜索零越权 | 结果按 ACL 过滤（谓词下推或后置过滤），无 read 权限的 repo 零泄漏；admin 全见 | W16 |
| NFR-S25 权限判定即时性 | 组成员/授权变更后**下一次请求**即生效（无定时刷新窗口）；变更留审计 | W19b/W23 |
| NFR-S26 UI 不绕权 | 控制台全部数据面走与 curl 相同的端点与 ACL 判定（无特权内部 API）；session 凭据与 Basic/Token 同权不同源判定 | W12d/W19b 交叉断言 |

### 6.3 可观测性（M4 增量）

- 结构化日志沿用 M1 字段集；console/session 域请求的 `user` 记 session 主体名；session 签发/吊销记 info 级（不含 session 值）。
- `/binflow/api/v1/health` 只增字段原则：新增 `console`（embed 资产就绪）子系统状态（向后兼容）。
- `quota.exceeded`、`gc.run`、`export.run`、`import.run` 同时进结构化日志（WARN/INFO）与审计表——容量与灾备操作可追溯。

### 6.4 M1~M3 遗留收编（逐条定界）

| 遗留项 | 来源 | M4 处置 |
|---|---|---|
| Email 不落盘（users 表无列） | M1 T-15 遗留 | **收编**（FR-27-AC8/SE-05/06）：004 迁移加列 + 回显；列表简形态不变 |
| local/remote 的 includesPattern/excludesPattern 双值码（下载 404/上传 409） | M1 勘误②（repo-semantics §9） | **收编**（FR-24-AC4）：字段启用 + 双值码落 AC |
| O4：匿名读开时已认证零权限用户 403 | M1/M2/M3 定界维持 | **评审维持不改**：保守正确方向（已认证走自身 ACL），控制台搜索/树/下载沿用同一判定（NFR-S24/S26）；「回落匿名通道」明确不做，归档关闭 |
| `/api/v2/security/permissions` 是否补路径 | M1 E-24 悬置 | **关闭：不补**（SE-09 有意不兼容，理由见矩阵） |
| remote 缓存手动管理面 | M3 §2.2（M4） | **收窄**：删缓存走既有 DELETE（M3 已交付）；统计端点 RE-11 与 UI 视图列 P2；按路径批量 evict 不做（M6+） |
| maxUniqueSnapshots / immutable tag / tag retention | M2 Q4、M3 Q6（「M4 治理」预告） | **推迟 M5+ 评估**（§2.2）：M4 治理面收敛为审计/GC/配额三件 |
| virtual DELETE 透传 | M3 RE-08（M4 评估） | **维持不做**（M6+）：防误删上游缓存，删缓存对 remote 成员操作 |
| 双进程首启竞态（SQLite） | M1 T-10 技术债（M4） | 归 architect 评估（export CLI 与 serve 并存加剧该面——互斥锁设计时一并考虑）；PRD 不约束实现，登记 |
| docker remote pull-through | M3 Q4（M4+ 评估） | **开放问题 Q5 维持挂起**（§7），暂行不进 M4 |

---

## 7. 开放问题（需用户定案；暂行假设 v1.0 起生效）

| # | 问题 | 影响面 | 暂行假设 |
|---|---|---|---|
| Q1 | **控制台会话机制**：server-side session（SQLite sessions 表 + opaque cookie，可吊销/可审计/重启不掉线）vs JWT（无状态、签名密钥管理与吊销难题） | FR-23/CE-03~05、004 迁移、安全审计 | **server-side session**：HttpOnly + SameSite=Lax + TTL 24h（`console.session_ttl_hours` 主键 + `console.session_ttl_seconds` 覆盖键，v1.1 双键收口）+ 登出即吊销 + 落库（重启不掉线）。理由：BinFlow 无多实例（单二进制），无状态 JWT 的唯一优势（水平扩展/跨服务）不存在，而吊销与审计是治理里程碑的题中之义 |
| Q2 | **配额粒度**：仅 repo 级 bytes？是否要全局配额/文件数/用户级？超限行为拒绝（413）还是告警放行 | FR-31/GE-05、部署文档 | **仅 repo 级 `quotaBytes`（0=不限），超限原子拒绝 413**；全局/文件数/用户级不做（§2.2）。企业诉求出现再扩 |
| Q3 | **搜索范围**：P0 是否只搜 node 名/路径（SQL LIKE），properties/元数据（npm packument 字段、maven GAV、props）是否进 M4 | FR-26/SR 域、性能预算 | **P0 = name 子串 + checksum 精确；gavc P2；properties/元数据搜索 M5+**（props 体系 M1~M3 未建，属新地基非搜索面缺口） |
| Q4 | **备份一致性窗口**：在线 export（GC 互斥 + manifest 边界，接受窗口内多余 blob）vs 维护窗口停机 export（强一致、操作重） | FR-32/GE-07、运维手册 | **在线 export**：锁互斥（GC/export）+ SQLite backup API 快照先行 + manifest 引用完整性断言；停机 export 作为可选操作说明写入文档（同一 CLI，用户自行停服执行即得强一致） |
| Q5 | **docker remote pull-through 是否提前进 M4**（M3 Q4 遗留，conductor 已转知悉） | ROADMAP 边界、工作量 | **不进 M4**（§2.2）：M4 已满（控制台+权限+治理+备份）；维持 400「not supported」，M5+ 评估。用户若定案提前，M4 增补 FR 并重排 |
| Q6 | **GC 在线化执行形态**：同步 REST（实现简单、长库占请求）vs 异步 job + 状态端点（Artifactory prune 同构） | FR-30/GE-03、超时语义 | **同步**（单机规模 mark-sweep 为既有集合查询，秒级；NFR-P17 面配套）；异步 job 框架 M6+（§2.2）。超长库的行为以 CLI `gc` 兜底（同语义离线跑） |

---

## 8. M4 验收剧本（QA 总纲）

1. **回归基线**：§5.6 反转表更新断言 → M1 C / M2 D / M3 M 序列 P0 复跑全绿（FR-33-AC2）。
2. **控制台基座**：W01 → W01b → W02 → W03~W08（session 全周期含 TTL）→ W37a → W38（CSRF）。
3. **浏览器链**：W09（登录）→ W10/W10b/W11（仓库管理）→ W12/W12b/W12d/W13（树/下载/上传/删除）→ W14b（搜索框）。
4. **搜索**：W14 → W15 → W16（ACL 零泄漏）→ W36 边界（未实现族 404）。
5. **权限完整**：W17 → W18/W40 → W19/W19b/W19c → W20 → W21 → W33/W33b/W33c（UI 面）→ C22/C27 回归。
6. **治理**：W22/W22b → W23/W23b → W39（审计）→ W24/W25/W25b（GC）→ W26/W26c/W27（配额含协议覆盖）。
7. **备份恢复**：W28/W28b → W30/W30b → W31 → W32（往返/完整性/mtime）。
8. **性能与安全**：W37（冷启动/并发）+ NFR-P17/P18 造数抽查 + NFR-S19~S26 断言。
9. **部署烟测**：FR-33-AC5（compose 实例控制台链）。
10. **文档**：FR-33-AC6（控制台/治理/备份手册）。

## 9. M4 DoD

1. §4 全部 P0/P1 AC 经 qa 验证全绿（P2 延后在 BOARD 记录）；
2. §8 剧本全绿，§5.3 矩阵 Chromium + curl + CLI 三个 P0 成员全过；
3. §5.5 三项待校准（K1~K3）已由逆向票回写定案（或用户豁免）；§7 六项开放问题用户定案后回写（+0.1）；
4. tech-writer 产出控制台使用、权限/组管理、治理（审计/GC/配额）、备份恢复四篇文档（含 NFR-S22 产物保管告警）；
5. release-engineer 烟测报告（compose 控制台链）归档；
6. 主会话完成 `m4-done` tag（对外发布任何制品先经用户确认）。

---

*本 PRD v1.0 由 product-manager（T-85）依据 PRODUCT.md、ROADMAP.md M4 节与 M1/M2/M3 交付基线撰写；与 `docs/reverse/` 规格冲突时按 §5.5 流程回写修订。*
