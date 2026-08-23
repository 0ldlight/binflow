# BinFlow 控制台 UX 规范（console-ux）

| 项 | 值 |
|---|---|
| 文档 | `docs/design/console-ux.md` |
| 票据 | T-87（v1.0：信息架构与线框）/ T-116（v1.1：权限可见性定案 + testid 清单）/ T-118（v1.2：testid 清单回写转正）/ T-123（v1.3：§9 R10 例改道）/ T-235（v1.4：M8 路由重排锚保全映射 + 壳新锚） |
| 状态 | v1.4（2026-08-23） |
| 维护者 | ux-designer |
| 上游依据 | PRODUCT.md（Web 控制台/治理/Non-goals）、ROADMAP.md M4 节、docs/prd/milestone-1/2/3/4.md（端点矩阵与已定案行为）、docs/user/docker-registry.md（用户面口径）、docs/design/architecture.md §7（路由/console 挂载点）、internal/httpapi/router.go（路由门事实——§3.6.2 矩阵逐一核对）、reports/agents/T-98.md · T-99.md（漂移登记与 testid 素材）、reports/agents/T-98-review.md（N1 收敛建议）、BOARD.md（T-85 PRD / T-97 存在性不泄露裁决） |
| 下游消费者 | T-86（架构：console 包/session/前端工程结构）、tech-lead（M4 拆票）、前端 dev（页面组票）、qa-engineer（控制台验收） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-20 | T-87 初版：IA（导航树 + 18 路由 + 五协议×三仓型矩阵）、11 页线框（登录/仪表盘/仓库列表/建仓/仓库详情/制品树/上传/搜索/权限编辑器/审计/治理）、交互四态（通用原则 + 骨架屏策略 + 每页矩阵）、大目录策略、设计 token（暗色优先）、可达性、API 需求清单 R1~R10 |
| v1.1 | 2026-08-21 | T-116 权限可见性漂移集中定案：① §3.3 按路由门事实修订角色可见性——健康、仓库列表、Tokens 三处 v1.0 设想与实现的漂移定案，**均维持实现（admin-only）**，理由与放宽前置条件见 §3.6.1；② 新增 §3.6 权限可见性矩阵（403 收敛四层主姿态 + 端点×门矩阵 + 页面×角色呈现矩阵 + admin 硬编码裁定，收敛 T-98 review N1）；③ §3.1 治理分组补「审计日志」条目（v1.0 导航漏列而 §3.2 已有路由；`GET /api/v1/audit` 为 admin 门，归治理组）；④ §5.1 的 403 分流改挂 §3.6.3 分层规则（消除「403 一律无权限卡」与卡片级隐藏的矛盾）；⑤ 新增 §10 data-testid 命名清单（T-98/T-99 已落锚全量核对自源码 + 命名规则 + T-100~T-102 预定锚——T-104 断言锚源）；⑥ 修订记录自文末移至 §0 |
| v1.2 | 2026-08-21 | T-118 §10 testid 清单回写（T-104 断言锚冻结的前置）：① §10.3 预定锚**转正为已落地清单**——T-100~T-102 全部落码，逐一对码核对（差异注记随各组）；② `perm-matrix-cell-<principal>-<action>` 细化为 `perm-matrix-cell-{user|group}-<principal>-<action>`（防用户/组同名碰撞，T-101 遗留 2 定案），类段防碰撞原则升入 §10.1 命名规则；③ 搜索页 `search-filter-{package|type}` **删除**（实现仅 repo 过滤；R2 类型化过滤落地时回填）；④ 未落/裁剪锚（token 族 / `audit-export` / `copy-<field>`）新设 §10.4 承载，T-104 不得断言；⑤ grep 补记两处三票日志未列锚（`search-results` 结果表容器、`perm-pattern-{include|exclude}-<i>` chip 本体）；锚总量 **242 落点 / 27 文件**（`grep -rn "data-testid" web/src/`，动态族计一名约 230 锚） |
| v1.3 | 2026-08-21 | T-123 §9 R10 例改道（E4 定案一致性收口，T-107 移交 H-3 / T-103 同建议）：session 凭据等价性示例由「docker tags/list」改为「npm packument / pypi simple」——二者挂在 `/binflow` 前缀下，cookie `Path=/binflow` 可携 session；docker tags 因 cookie 结构性不达根级 `/v2`（docker 客户端走 `/v2/token` Basic 面）不再作例。依据：PRD M4 v1.3 CE-03 E4 注记、docs/user/faq.md |
| v1.4 | 2026-08-23 | T-235 M8 双模式壳与路由重排（console-m8 §1 落地）：① 新增 **§10.5 路由重排锚保全映射**——M8 新路由表 ↔ §10.2/§10.3 既有锚，242 锚**零改名**（ADR-0029 决策 3，W 资产保全）；② 壳新锚 10 枚入册（`nav-mode-switch` / `topbar-breadcrumb` / 用户菜单 Quick 动作族）；③ §10.4 Tokens 行注记更新——侧栏禁用占位（`nav-item.disabled` 计数断言）让位真实路由 `/admin/security/tokens` 的 `placeholder-page` 承载；④ IA/路由正文以 console-m8 为准（§0.2 冲突条款），本版不重写 §3.1/§3.2 旧路由表 |

---

## 1. 文档定位与边界

### 1.1 本文档定义什么

- 信息架构：导航树、SPA 路由、每页核心内容、五协议 × 三仓型的呈现差异。
- 关键页面线框（ASCII，标注区块用途）与交互四态（loading / empty / error / success）。
- 大目录（慢查询大 repo）的分页 / 懒加载 / 骨架屏策略。
- 设计 token（色彩 / 间距 / 字号 / 圆角 / 等宽字体应用规则）与可达性要求。
- 对后端 API 的 UX 需求清单（§9，供 T-86 与拆票消费——**只提需求，不设计 API**）。

### 1.2 本文档不定义什么（T-86 / 后续票的领地）

| 归属 | 内容 |
|---|---|
| T-86 architect | 前端工程结构（构建链、node/vite 与 ADR-0005 零依赖白名单的边界）、go:embed 形态、console 包、session 机制（cookie / header / TTL）、组件实现方式与是否引框架 |
| 后端票 | §9 所列 API 增量的具体设计（分页参数、搜索端点、GC/备份端点、token 列表） |
| T-85 PM | 权限模型的最终字段（本文件以 M1 已落地的 permission target 模型为基线画 UI，M4 扩展字段预留位置） |
| 永不进入 | 洞察报表 / 趋势分析图表（PRODUCT Non-goal「不做 UI 高级分析与洞察报表」）、漏洞扫描 UI（Xray Non-goal）、LDAP/SAML/OIDC 登录 UI |

**命名对齐 Artifactory**（降低迁移用户学习成本，全文一致）：repository **key**（不叫「仓库名」）、**deployment**（上传）、**permission target**、**include/exclude patterns**、rclass 三型 Local / Remote / Virtual、GAV、manifest / tag、dist-tags。UI 文案为中文，但上述术语保留英文原词。

### 1.3 挂载与认证前提（来自 architecture.md §7.1，UX 消费口径）

- 控制台 SPA 挂 `/binflow/`（go:embed）；所有数据请求走同源 `/binflow/api/**`，无跨域。
- 管理面 API 恒需认证（匿名读只作用于内容路径）；控制台**没有匿名模式**，未登录一律先到登录页。
- API 错误体三分层（制品 `errors[]` JSON / 用户管理纯文本 / token OAuth 形）——UI 的错误呈现统一收敛为「状态码 + message 提取」（§5.1），不把三种格式暴露给用户。

---

## 2. 用户与设计原则

**目标用户**：平台工程师 / 运维（M1~M3 的终端用户是 CI 脚本；控制台是**管理面**，给偶尔上来「建仓、授权、排障、看一眼缓存状态」的人，不是给每天泡 8 小时的运营人员）。

七条设计原则，前端实现与 QA 验收都以此为判据：

| # | 原则 | 落地要求 |
|---|---|---|
| P1 | **信息密度优先** | 表格行高 32px、13px 字号起步；一屏尽量给出「下一步动作」所需全部信息；不留装饰性留白（§7.2 密度 token） |
| P2 | **一切标识符可复制** | 路径 / digest / checksum / repo key / tag 一律 mono 渲染 + 一键拷贝（悬停或行尾 copy 按钮）；连续长串不折断（`word-break: break-all` 仅用于纯展示列） |
| P3 | **命令优先** | 每个仓库详情页给「客户端接入命令」块（docker login / settings.xml / .npmrc / pip.conf / curl），带复制按钮——工程师最终在终端完成 push/publish，控制台负责把命令配好（§4.5） |
| P4 | **暗色优先** | 默认暗色主题（管理工具惯例），亮色为等价次主题；token 全部语义化，两主题零样式分叉（§7.1） |
| P5 | **危险操作显式化** | 删仓 / 删 manifest / GC apply / 导入 = 危险区模式（红边框分区 + 输入 key 确认 + 影响面摘要）；制品不可变，删除没有「撤销」，不提供 fake undo |
| P6 | **协议差异显式化** | 五种 packageType 的浏览视图、可执行操作（上传/删除/刷新缓存）按协议真实能力收窄（§3.4 矩阵），禁用的动作**隐藏而非置灰**，但给出「为什么 + 替代命令」 |
| P7 | **键盘可达** | 全部操作可 Tab 到达；树用方向键导航；焦点环 2px 可见（§8） |

---

## 3. 信息架构

### 3.1 导航树（左侧固定，宽 224px，可折叠为 48px 图标栏）

```
BinFlow ◆                    ← 产品名 + 版本号（/api/system/version）
────────────────────────────
▣ 仪表盘                     ← / 
▣ 仓库                       ← /repositories（列表）
   └ (选中仓库的快捷上下文，非全局树；
      制品树在仓库详情页内，不进全局导航)
▣ 搜索                       ← /search
────────────────────────────  ← 「安全」分组（admin 可见）
▣ 安全
   ├ 用户                    ← /security/users
   ├ 组                      ← /security/groups        [M4 新模型]
   ├ 权限                    ← /security/permissions
   └ Access Tokens           ← /security/tokens
────────────────────────────  ← 「治理」分组（admin 可见）
▣ 治理
   ├ 审计日志                ← /audit（v1.1 补列：v1.0 导航漏列而 §3.2 已有路由；
   │                           GET /api/v1/audit 为 admin 门，归治理组）
   ├ 存储 & GC               ← /governance/gc
   ├ 复制                    ← /governance/replication（v1.3 补列，T-182 回写：
   │                           单向 push 复制目标与事件状态页，实现先于契约——见 §4.11）
   ├ 备份 / 恢复             ← /governance/backup
   └ 配额                    ← /governance/quotas
────────────────────────────
▣ 设置                       ← /settings（实例信息、匿名读开关状态、日志级别只读展示）
────────────────────────────
(底栏) admin ▾               ← 改密 / 発出登录 / 主题切换（暗/亮）
```

- **导航层级不超过两级**；制品树深度由仓库详情页内的面包屑承载（§4.6），不塞进全局导航。
- 分组标题（安全 / 治理）是标签不是可折叠项——条目总量 13 个（v1.1 补审计日志、v1.3 补复制后），折叠反而增加点击。
- 非 admin 用户按 §3.3 收窄可见性。

### 3.2 路由表（SPA 路由；`/binflow/` 为应用根，下表路径为应用内路径）

| 路由 | 页面 | 核心内容 | 数据来源（现役 API） |
|---|---|---|---|
| `/login` | 登录 | 用户名 + 口令 | session（T-86） |
| `/` | 仪表盘 | 健康、存储 stats、仓库计数、remote 状态、最近审计 | `/api/v1/health`、`/api/v1/storage/stats`、`/api/repositories`、`/api/v1/audit` |
| `/repositories` | 仓库列表 | 表格 + 过滤 + 建仓入口 | `GET /api/repositories?type=&packageType=` |
| `/repositories/new` | 建仓 | 分步表单（§4.4） | `PUT /api/repositories/{key}` |
| `/repositories/:key` | 仓库详情·概要 | 配置、接入命令、统计、危险区 | `GET /api/repositories/{key}` |
| `/repositories/:key/tree/*` | 制品树浏览 | 左树右表（§4.6，按 packageType 特化） | `GET /api/storage/{repo}/{path}`、`/v2/_catalog`、`/v2/<name>/tags/list`、npm packument、`_list?prefix=` |
| `/repositories/:key/tree/*?upload` | 上传对话框 | 拖拽 + 进度 + 校验和（§4.7） | `PUT /binflow/{repo}/{path}` |
| `/repositories/:key/settings` | 仓库设置 | 编辑配置（按 rclass 特化字段） | `POST /api/repositories/{key}`（更新） |
| `/search` | 搜索 | 名称/路径检索 + 过滤 | `/api/search`（M4 新增，§9-R2） |
| `/security/users`、`/security/users/:name` | 用户列表/详情 | 建/禁用/改密 | `GET/PUT /api/security/users` |
| `/security/groups` | 组 | 组 CRUD + 成员 | M4 新模型（T-85/T-86） |
| `/security/permissions`、`/security/permissions/:name` | 权限 target 列表/编辑器 | §4.9 | `/api/v1/permissions` CRUD |
| `/security/tokens` | Token 管理 | 签发（仅创建时展示明文）/吊销 | `/api/security/token` + §9-R6 |
| `/audit` | 审计日志 | 过滤 + 表格 | `GET /api/v1/audit?repo=&actor=&since=` |
| `/governance/gc` | 存储 & GC | stats、dry-run、apply、存储迁移面板（T-160 回写：本地→S3 迁移只读进度，5s 轮询、501 未配置降级为一句提示、403 整面板隐藏——实际形态见 §4.11 迁移面板框） | `/api/v1/storage/stats` + GC 端点（§9-R4）+ `GET /api/v1/storage/migration`（迁移面板数据源，契约见 architecture.md §7.1） |
| `/governance/replication` | 复制 | 单向 push 复制目标表 + 最近事件表（T-159 回写：实现先于契约，T-159 核验发现——10s 轮询、四态收敛，实际形态见 §4.11 复制页框） | `GET /api/v1/replication/status`（T-180 桥接完成、**零差异**——T-159 假设契约全数坐实，契约定稿见 architecture.md §7.1；CRUD 面 `GET/POST /api/v1/replications`、`DELETE /api/v1/replications/{name}` 同批落地，**本页不消费——CRUD UI 另票**，见 §4.11 回写记录） |
| `/governance/backup` | 备份/恢复 | export / import | M4 新端点（§9-R5） |
| `/governance/quotas` | 配额 | per-repo 配额条 | M4 新端点（§9-R7） |
| `/settings` | 设置 | 实例信息（version/健康/匿名读开关/数据目录）/ 管理员改密 | `/api/system/version`、`/api/v1/health` |

未匹配路由 → 404 页（保留导航壳，给出返回仪表盘链接）。

### 3.3 角色可见性（v1.1 定案口径）

可见性基线是**路由门事实**（§3.6.2 端点×门矩阵，逐一核对自 `internal/httpapi/router.go`）：管理面（健康 / 存储统计 / 审计 / 仓库配置 CRUD / 权限 / 用户 / 组 / token / GC）一律 admin-only；内容面（storage children / 搜索 / 树浏览）按调用者路径 ACL；`system/version` 开放。v1.0 的「非 admin 可见只读健康 / 仅列出有 read 权限的 repo / 仅自己的 token」三项设想与实现漂移，v1.1 定案**维持实现**（理由与放宽前置条件见 §3.6.1）。

| 角色 | 可见面 | 说明 |
|---|---|---|
| admin | 全部入口与数据面 | 13 个导航入口全开 |
| 非 admin 已登录 | 仪表盘（实例卡 + 一段收敛说明）、仓库入口（列表页呈现无权限卡 + 搜索/直链引导——**制品面经树路由与搜索仍按自身 ACL 可达**）、搜索（结果按调用者 ACL 过滤）、设置（实例信息 + 改密） | 「安全」「治理」分组整体隐藏——**含 Tokens**（token 签发 admin-only：D3 定案，M4 无 scope 模型，非 admin 自签分支关闭）；仪表盘健康/存储/仓库/审计四卡隐藏（§3.6.3 L3） |
| 未登录 | 无（重定向 `/login?return=<原路由>`） | 登录成功后回跳 return |

判定信号（两个，各司其职、必须同向）：

- **whoami**（`GET /api/v1/session`，CE-04）的 `{username, admin}` —— 导航分组与写入口的**预收敛**信号（一次请求、零额外探测；v1.0 的「仓库列表探测」已不可用——该端点 admin-only）。
- **各请求自身 403**（useAsync `forbidden` 态）—— 数据面的**终裁**（§3.6.3 分层规则）。

### 3.4 五协议 × 三仓型呈现差异（本规范的核心矩阵）

**packageType 决定制品树的形态与详情视图**：

| packageType | 树形态 | 节点详情视图 | 版本/标签模型 | UI 上传 | UI 删除 | 接入命令块内容 |
|---|---|---|---|---|---|---|
| **generic** | 原始路径树（任意层级） | 文件信息：size / mime / createdBy / 修改时间 / `checksums{sha256,sha1,md5}` + `originalChecksums`（全 mono + 拷贝） | 无版本概念，路径即身份 | ✅ 拖拽上传（§4.7） | ✅ 文件/目录递归删 | `curl -T` / `curl -O` + sha256 校验 |
| **docker** | 两级列表：**镜像名**（`<repoKey>/<image>` 全名，来自 `/v2/_catalog`）→ **tag 表**（`tags/list`，n/last 分页） | **manifest 详情面板**：digest（mono+拷贝）、mediaType（透传值）、架构（index 子 manifest 列表）、config digest、layers 表（每层 digest/size，mono）、总 size、推入者/时间 | tag → digest 可变指针；by-tag 删除 405（spec） | ❌ 协议是 POST/PATCH/PUT 三步会话，浏览器不做 → 显示 `docker login/push` 命令 | ⚠️ 仅 by-digest 删 manifest（级联其 tags，确认框列明受影响 tag）；blob 删除永不提供（GC 唯一入口） | `docker login $REG`、push 全名 tag 模型图示、insecure-registries 提示、oras/helm 命令（对齐 docs/user/docker-registry.md） |
| **maven** | **GAV 树**：groupId 点转斜杠的段 → artifactId → version 目录；目录图标区分「含 SNAPSHOT」 | 版本目录视图：构件文件表（jar/pom/war/classifier 各一行，size + sha1）+ `maven-metadata.xml` 只读视图（versions/latest/release/snapshotVersions，格式化 XML）+ GAV 坐标拷贝（`groupId:artifactId:version`） | versions 列表（服务端计算的 metadata）+ SNAPSHOT unique/non-unique 形态（文件名即可辨识 timestamped） | ⚠️ 单文件 PUT（layout 严格校验，路径非法 400——表单预校验 groupId/artifactId/version 并生成目标路径） | ✅ 删构件文件；metadata 不可手删（服务端事实） | `settings.xml` mirror 片段、`dependency:get` 命令 |
| **npm** | 包列表（scope 分组：`@acme/*` 与无 scope 平铺）→ **版本列表**（packument `versions` 键序） | 包详情：`dist-tags`（latest/beta…，mono）、选中版本 `dist.integrity`（sha512）/ `shasum`（sha1）、tarball size、发布时间、依赖数 | semver 版本 + dist-tags 指针 | ❌ publish 是「packument + base64 附件单 PUT」协议 → 显示 `npm publish` 命令 | ⚠️ unpublish（整包/单版本，走 `-rev` 协议语义，确认框） | `.npmrc` registry 行 + `_auth` 说明 |
| **pypi** | 项目列表（**归一名**展示，`Demo_Pkg.1` 与 `demo-pkg-1` 合并，hover 提示原始名）→ 版本 → 文件（wheel/sdist） | 文件行：filename、`#sha256=`（mono+拷贝）、requires-python、类型 badge（wheel/sdist） | version + 文件名身份（同版本可多文件） | ❌ twine multipart 协议 → 显示 `twine upload` 命令 | ✅ 删文件（warehouse 语义，确认框） | `pip.conf` index-url、`twine upload`、`pip install` 带 hash 示例 |

**rclass 决定仓库详情页与操作集**：

| rclass | 详情页特化 | 浏览视图 | 写操作 |
|---|---|---|---|
| **local** | 布局/校验策略配置（maven 的 checksumPolicyType、snapshotVersionBehavior、handleReleases/Snapshots 等） | 本地制品树 | 上传 / 删除（按权限） |
| **remote** | 上游 url（mono，http/https badge；`allowPrivateUpstream=true` 时黄标「已放行私网上游」）、TTL（retrievalCachePeriodSecs / missedRetrievalCachePeriodSecs）、socketTimeout、assumedOfflinePeriodSecs、hardFail、缓存统计（命中/未命中/字节数，`/api/v1/remote/stats`）；**assumed-offline 状态 badge**（黄，tooltip「静默期内不回源」） | **已缓存内容**树（缓存即 node）；条目显示 fetched_at / 过期时间 | 上传按钮不渲染（PUT 405）；每条缓存给「删除缓存」动作（= `DELETE /binflow/<remote>/<path>`，删后再 GET 回源——文案「删除缓存，下次请求将重新回源」）；「刷新」= 删缓存 + 立即 GET 预热（两个动作，不合成一个假按钮） |
| **virtual** | **成员列表按解析顺序可视化**：优先桶（priorityResolution 标记）在前、桶内声明序；上下拖拽调序（保存即 PUT 重建配置）；`defaultDeploymentRepo` 选择器（仅列 local 成员；未配置时显式显示「写操作将返回 405」） | 聚合视图：children 并集；**来源成员列**（`X-BinFlow-Resolved-From` 头 / 聚合标注可得时显示，不可得时该列隐藏——不伪造数据） | 未配写路由：上传/删除不渲染 + 说明；配了：上传路由到 defaultDeploymentRepo（UI 明示「将写入 maven-local」） |

### 3.5 全局元素

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 顶栏(高 48px)：BinFlow ◆ v1.0.0-m4   [全局搜索 ⌘K]        admin ▾  [◐ 主题] │
├───────────┬──────────────────────────────────────────────────────────────────┤
│ 左导航     │  页面内容区（max-width 1440px 居中，内容贴左）                     │
│ (§3.1)    │                                                                  │
└───────────┴──────────────────────────────────────────────────────────────────┘
```

- **全局搜索 ⌘K**：聚焦即跳 `/search` 并置焦输入框（跨页快捷键，`/` 键等效）。
- **顶栏版本号**：来自 `/api/system/version`（如实返回 BinFlow 版本，不伪装——Q4 定案的 UI 侧延续）。
- **toast**：右下角堆叠，成功 5s 自动消失、错误常驻直至手动关闭；toast 内可带一个动作链接（如「查看审计」）。
- **危险确认对话框**：居中 modal，焦点圈进对话框，Esc 关闭，确认按钮需满足前置（如输入 repo key）才可用。

### 3.6 权限可见性矩阵（v1.1 新增，T-116 定案）

#### 3.6.1 两条契约漂移的定案（供 PM/architect 会签）

**漂移 A：健康可见性——维持 admin-only，规范向实现对齐。** `/api/v1/health` 的路由门 `routeAuth{required, admin}`（router.go 注释明示 D2 决策）是正确姿态，不放宽：

1. 健康端点暴露实例内部状态（storage/metadata/registry 子系统细节与失败原因），与 `/api/v1/storage/stats` 同属运维面数据——D2 已定「非 admin 不得得知实例体量」，健康放宽等于在同一 stance 上开旁门。
2. BinFlow 安全立场一贯**存在性与内部状态不泄露**（登录失败不泄露用户存在性、`?permissions` 收 admin 门、T-97 conductor 裁决同款）；健康是典型的内部状态。
3. 需要健康信号的角色（运维/排障者）就是 admin 本身；部署侧探活有免认证的 `/healthz` / `/readyz`，不依赖此端点——放宽没有真实受益者。
4. 非 admin 的实际工作（CI push/pull、浏览制品）不需要实例健康就能完成。

→ **无需后端改动票**。UI 侧：仪表盘健康卡、设置页健康行对非 admin 隐藏（L3）。

**漂移 B：仓库列表可见性——维持路由门 admin-only。** service 层（`ListReposFiltered`）虽只要求 authenticated 并支持过滤，但路由门收紧是对的，不改：

1. 权限模型是 **path-keyed**（permission target 的 include/exclude pattern），没有 repo-keyed ACL。「列出调用者可读的 repo」需把全部 target 的 pattern 对全部 repo key 求值——语义不正交（`**` 授予的是「将来一切路径」，不是「某个仓的存在」）、无索引、结果不可判定为精确集合。
2. 列表即**存在性枚举**：非 admin 看到全量列表即得知其无权访问的仓库存在，违背存在性不泄露立场（router.go 注释引用的 FR-5-AC8 / C22b 断言同款）。
3. 非 admin 的**制品可达性已由内容面承担**：`GET /api/storage/{repo}/{path}`（authenticated + 路径 ACL）、`GET /api/search/artifact`（T-92：与内容面同一 `allow()` 路径、按调用者过滤、零泄漏）——管理面列表不是必需路径。
4. M4 控制台的非 admin 主场景（按权限浏览/上传制品）不依赖仓库管理列表；管理动作（建/改/删仓）本就 admin。

→ **无需后端改动票**。若未来产品确需「非 admin 可见自己可读的仓库」，前置条件是一张后端票（**本轮不派，仅登记模板**）：repo-keyed 读授权枚举模型 + `GET /api/repositories` 路由门放宽 + 按 ACL 过滤 + C22b 断言更新。届时前端零改版——403 消失，列表自动出现（§3.6.3 L2 的「放宽跟随」列）。

**附带定案（矩阵核对中浮出的第三处 §3.3 旧设想）**：Tokens 收回 admin-only。`POST /api/security/token` 及 `/revoke` 均为 admin+OAuth 门（D3：M1 无 scope 模型，非 admin 自签 `api:*` token 会携带完整主体权限，分支保持关闭）。v1.0 §3.3「安全→Access Tokens（仅自己的 token）」作废；非 admin 的「安全」分组整组隐藏。

#### 3.6.2 端点 × 门矩阵（事实来源：`internal/httpapi/router.go`，T-116 逐一核对）

| 端点 | 门 | console 消费面 |
|---|---|---|
| `GET /api/system/ping` · `GET /api/system/version` | 开放 | 实例卡（恒可见，含非 admin） |
| `GET /healthz` · `GET /readyz`（根级，非 /binflow 下） | 开放（探针专用） | console 不消费 |
| `POST /api/v1/session` | 开放（本身即凭据呈现；CSRF Origin 校验另置） | 登录 |
| `GET` / `DELETE /api/v1/session` | authenticated | whoami（预收敛信号）/ 登出 |
| `GET /api/v1/health` | **admin**（D2） | 仪表盘健康卡、设置页健康行 |
| `GET /api/v1/storage/stats` | **admin**（D2） | 仪表盘存储卡、GC 页概况 |
| `GET /api/v1/storage/usage/{key}` | authenticated（use case 裁 admin 或 read 授权；拒者 403） | 仓库详情统计卡（页面本身 admin，实际仅 admin 可达） |
| `GET /api/v1/audit` | **admin** | 仪表盘最近审计、审计页 |
| `POST /api/v1/system/gc` | **admin** | GC 页 |
| `GET/PUT/POST/DELETE /api/repositories[/{key}]` | **admin**（D2，**含列表**——漂移 B 定案维持） | 仓库列表/详情/建仓/设置/危险区 |
| `GET /api/storage/{repo}/{path}`（item info） | 内容面语义（匿名读开时匿名可读；路径 ACL 在 use case） | 制品树（T-100） |
| `GET /api/storage/{repo}/{path}?list` | 门空，handler 自答匿名 403 / 200 | `_list` 兜底 |
| `GET /api/storage/{repo}/{path}?permissions` | **admin**（SE-08，T-97 review B2） | 树视图权限列（T-100） |
| `GET /api/search/artifact` · `/api/search/checksum` | 门空（use case 裁匿名通道；**结果按调用者 ACL 过滤**，T-92） | 搜索页（T-100） |
| `PUT /api/security/password`（及 changePassword alias） | authenticated | 设置页改密（非 admin 可用） |
| `POST /api/security/token` · `POST /api/security/token/revoke` | **admin**（D3，OAuth 错误体） | Tokens 页 |
| `GET/POST/PUT /api/security/users[/{name}]` | **admin** | 用户页（T-101） |
| `GET/PUT/POST/DELETE /api/security/groups[/{name}]` | **admin** | 组页（T-101） |
| `GET/POST/DELETE /api/v1/permissions[/{name}]` | **admin** | 权限 target 编辑器（T-101） |
| 内容面 `/binflow/{repo}/{path}`（GET/PUT/DELETE） | 读 = 匿名可（开关开时）；写/删 = authenticated + action ACL | 树浏览 / 上传 / 删除（T-100） |
| `/binflow/api/npm/**` · `/binflow/api/pypi/**`（协议挂载，重写至内容面） | 同内容面 | console 不直接消费（R10） |
| `/v2/**`（docker 平面，根级例外） | adapter 自持（token scope） | console 不直接消费 |

> 矩阵是**快照**：后端任何路由门变更须同步本表（改门=改契约）。UI 的可见性不自行猜测，一律以本表 + 运行时 403 为准。

#### 3.6.3 403 收敛规则（统一主姿态）

**主姿态：数据面 403 → 隐藏。** 分四层，每层一种姿态、不得混用：

| 层 | 对象 | 403 姿态 | 放宽跟随 |
|---|---|---|---|
| L1 导航入口/分组 | 左导航条目、「安全/治理」分组 | **不渲染**（whoami `admin` 位预收敛；直链仍渲染页面壳） | 需一次性前端微调（见下方裁定） |
| L2 页面主数据面 | 页面的主数据请求整体 403（如仓库列表对非 admin） | **单张无权限卡**：页面壳保留，卡内说明（管理面需管理员 / 需要什么权限）+ 引导（搜索、直链、权限文档）——整页数据全 403 时不得留空白壳 | **自动**（403 消失，内容自现） |
| L3 卡片/行级数据 | 仪表盘健康/存储/仓库/审计卡、设置页健康行、详情页次级卡 | **隐藏该卡/行**（对非 admin 渲染一排无权限卡是噪音） | **自动** |
| L4 动作按钮 | 建仓 / 删除 / GC apply 等写入口 | **不渲染**（隐藏而非置灰——P6 同款纪律） | 随 L1（whoami 位） |

- **只读降级不是 403 姿态**：能力收窄（docker 仓无 UI 上传、virtual 未配写路由 405 等）由 §3.4 协议×仓型矩阵与端点能力表达，用「说明 + 替代接入命令」呈现；403 只表达权限，绝不用于表达功能有无。
- **401 ≠ 403**：401 一律「登录已过期」toast + 重定向登录（§5.1），不进本矩阵。
- **admin 硬编码裁定（T-98 review N1 的收敛）**：数据呈现（L2/L3）一律 403 驱动，**禁止**用 whoami admin 位硬编码数据卡/行的显隐——后端放宽端点门时 UI 自动跟随；whoami `admin` 位仅用于 L1 导航分组与 L4 写入口的预收敛（省掉明知必 403 的请求噪音），且必须与 403 收敛**同向**（admin 位收紧面 ⊆ 403 收紧面，永不允许出现「admin 位放行而端点 403」的破窗）。存量偏离一处：设置页健康行（T-98 `{admin && …}`）应改为与仪表盘健康卡相同的 403 驱动——一行前端修正，随 T-100~T-102 任一批次或 T-104 前顺手收口，并补 `settings-health` 锚（§10.3）。

#### 3.6.4 页面 × 角色呈现姿态矩阵

| 页面/区块 | admin | 非 admin 已登录 | 未登录 |
|---|---|---|---|
| 登录 | — | — | 登录表单 |
| 仪表盘 | 五卡全量（实例/健康/存储/仓库/审计） | 实例卡 + 一段收敛说明（四张管理卡 L3 隐藏） | 重定向登录 |
| 仓库列表 | 表格 + 过滤 + 建仓 CTA | **无权限卡 + 搜索/直链引导**（L2）；建仓按钮不渲染（L4） | 重定向登录 |
| 仓库详情/设置/危险区 | 全量 | 无权限卡（L2，管理面端点族 admin） | 重定向登录 |
| 制品树（T-100） | 全量 | 按路径 ACL 浏览；无权子树 403 → 该子树无权限卡；上传/删除按写权限呈现，操作中 403 错误行内呈现并指向权限模型（§4.7） | 重定向登录 |
| 搜索（T-100） | 全量结果 | 结果按自身 ACL 过滤（可为空——空态按「无结果」呈现，不解释「被过滤」） | 重定向登录 |
| 安全全部页面（用户/组/权限/Tokens） | 全量 | 导航组隐藏（L1）；直链 → 页面级无权限卡（L2） | 重定向登录 |
| 治理全部页面（审计/GC/复制/备份/配额） | 全量 | 导航组隐藏（L1）；直链 → 页面级无权限卡（L2） | 重定向登录 |
| 设置 | 实例信息（含健康行）+ 改密 | 实例信息（版本/修订/用户）+ 改密；健康行 L3 隐藏（改 403 驱动后） | 重定向登录 |

---

## 4. 关键页面线框

> 线框标注 `[n]` 与下方编号对应；仅画关键态（默认态），四态差异见 §5.3 矩阵。

### 4.1 登录

```
┌──────────────────────────────────────────────┐
│                                              │
│              BinFlow ◆                       │  [1] 品牌区（无侧导航壳——登录页独立布局）
│         制品仓库控制台                        │
│                                              │
│   ┌──────────────────────────────────────┐   │
│   │ 用户名          [__________________] │   │  [2] 文本输入，autofocus，
│   │                  mono 渲染输入值       │   │      autocomplete="username"
│   │ 密 码          [__________________] │   │  [3] type=password，
│   │                                          │
│   │ (错误行：用户名或密码错误 [4])            │
│   │                                          │
│   │         [ 登 录 ]                       │  [5] 主按钮；两字段均非空才可用
│   └──────────────────────────────────────┘   │
│                                              │
│   管理面需认证。CI 与脚本请使用 API Token。    │  [6] 常驻说明 + 「创建 Token」文档链接
└──────────────────────────────────────────────┘
```

- [4] 错误行内展示（红），不弹 toast；提交中按钮转 loading 态（spinner + 禁用）。
- 不做「记住我」「忘记密码」（本地用户模型，M4 无邮件通道）；改密入口在登录后的设置页。
- 表单提交走 session 机制（T-86 定 cookie/header）；成功后跳 `return` 参数指定路由，无 return 则 `/`。

### 4.2 仪表盘（`/`）

```
┌ topbar ──────────────────────────────────────────────────────────────────────┐
│ 仪表盘                                                    2026-08-20 14:32  │
├──────────────────────────────────────────────────────────────────────────────┤
│ [1] 健康          [2] 存储                                [3] 仓库            │
│ ┌────────────┐   ┌──────────────────────────┐          ┌──────────────────┐  │
│ │ ● ok       │   │ blob 12,483 个            │          │ Local    6       │  │
│ │ storage ok │   │ 逻辑 842 GB / 物理 311 GB │          │ Remote   3       │  │
│ │ metadata ok│   │ 去重率 63% [4]            │          │ Virtual  2       │  │
│ └────────────┘   └──────────────────────────┘          └──────────────────┘  │
│ [5] Remote 状态（仅列非健康项；全健康时整块收起为一行「3/3 上游正常」）           │
│ ┌────────────────────────────────────────────────────────────────────────┐  │
│ │ ⚠ maven-remote   assumed-offline（静默至 14:47）   上游连接失败  [查看]   │  │
│ └────────────────────────────────────────────────────────────────────────┘  │
│ [6] 最近审计（最新 8 条）                                    [查看全部 →]    │
│ ┌────────────────────────────────────────────────────────────────────────┐  │
│ │ 时间             操作者    动作              对象                        │  │
│ │ 14:31:02        admin    REPO_CREATE       maven-snapshot              │  │
│ │ 14:02:11        ci-bot    PUT               docker-local/acme/app      │  │
│ │ …（mono 路径列，行点击进对象所在页）                                    │  │
│ └────────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────────┘
```

- [1] 来自 `/api/v1/health`，子系统任一非 ok 整卡黄/红；[2] 来自 `/api/v1/storage/stats`。
- [4] 去重率 = 1 − 物理/逻辑；只给数字与横条，**不做时间序列图表**（Non-goal：洞察报表）。
- 空实例时：[3] 显示「还没有仓库」+ 主 CTA「创建仓库」+ 文档链接（§5.3 空态）。

### 4.3 仓库列表（`/repositories`）

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 仓库                                                          [＋ 创建仓库] │
├──────────────────────────────────────────────────────────────────────────────┤
│ [搜索 key…]"
│ 类型 [全部▾]  包类型 [全部▾]                              共 11 个仓库      │
├──────┬──────────┬──────────┬─────────┬──────────────┬──────────┬─────────────┤
│ key  │ 类型      │ 包类型    │ 制品/缓存│ 上游 / 成员   │ 大小      │ 更新时间     │
├──────┼──────────┼──────────┼─────────┼──────────────┼──────────┼─────────────┤
│ docker-│ Local   │ Docker   │ 1,204   │ —            │ 412 GB   │ 2 小时前     │
│  local │ Remote  │ Maven    │ 8,391   │ repo1.maven… │ 220 GB   │ 5 分钟前     │
│ maven- │ ⚠offline│          │         │ [4]          │          │             │
│  remote│         │          │         │              │          │             │
│ maven- │ Virtual │ Maven    │ —       │ 2 成员        │ —        │ —           │
│  virtual         │          │         │ [5]          │          │             │
└──────┴──────────┴──────────┴─────────┴──────────────┴──────────┴─────────────┘
  [1] mono + 主链接（进详情）        [2] badge          [3] 点击列头排序（key/大小/时间）
```

- [1] key 列 mono，悬停行显示拷贝 key 按钮；行点击进 `/repositories/:key`。
- [2] 类型三值 badge；remote 的 assumed-offline 状态以 ⚠ 前缀呈现（tooltip 说明）。
- [4] remote 行显示上游 url 截断（mono，tooltip 全文）；[5] virtual 行显示成员数，点开浮层列成员序。
- 「制品/缓存」列：local = node 数；remote = 已缓存条目数（remote stats 不可用时显示 `—`，不显示 0）。
- 删除仓库**不在列表行内**（误触面太大），收进详情页危险区（§4.5[7]）。

### 4.4 建仓（`/repositories/new`，分步表单，三步 + 侧栏摘要）

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 创建仓库                                              步骤 2 / 3  ● ● ○     │
├───────────────────────────────────────────────────────┬──────────────────────┤
│                                                       │ 摘要（实时）          │
│ [步骤 1 · 类型]                                       │ ┌──────────────────┐ │
│   仓型  ( ) Local  ( ) Remote  ( ) Virtual            │ │ key   maven-     │ │
│   包类型 [Docker ▾][Maven][npm][PyPI][Generic]        │ │       remote     │ │
│         （组合非法项置灰：Remote×Docker 等，           │ │ rclass remote    │ │
│           tooltip 引用 §3.4 能力矩阵）                 │ │ type  maven      │ │
│                                                       │ │ url   http://…   │ │
│ [步骤 2 · 标识与来源]（按步骤 1 的仓型动态渲染）        │ │ TTL    7200s     │ │
│   Repository key [maven-remote____________]  ✓ 可用   │ └──────────────────┘ │
│     · 规则实时校验 [a-z][a-z0-9-]{1,62}，非法即红      │                      │
│   描述   [________________________________]           │                      │
│   ── 仅 Remote ──                                     │                      │
│   上游 URL [https://repo1.maven.org/maven2]           │ [上一步] [下一步 →]  │
│     · 非法 scheme 即时红（file:///ftp:// 拒绝）        │                      │
│   用户名/密码（密码不回显，写入即掩码）                 │                      │
│   □ 允许私网上游 allowPrivateUpstream                  │                      │
│     （勾选即黄条警示 + 「将记录审计」）                 │                      │
│   ── 仅 Virtual ──                                    │                      │
│   成员（可多选，拖拽排序；□ 优先解析）                  │                      │
│   默认部署仓库 [（未配置）▾]（仅列 local 成员）         │                      │
│                                                       │                      │
│ [步骤 3 · 策略]（按包类型渲染：maven 校验策略/snapshot │                      │
│   行为/handle 开关；remote 的 TTL/超时/hardFail…）      │                      │
│                                                       │ [取消]  [创建仓库]   │
└───────────────────────────────────────────────────────┴──────────────────────┘
```

- 分步理由：字段集随 rclass × packageType 组合差异大，单页长表单会迫使工程师在无关字段间穿行；每步校验通过才可下一步。
- key 与 url 的校验规则**前端预检 + 服务端终裁**（前端放过的仍以 API 400 文案为准，行内回显）。
- 创建成功 → toast `Successfully created repository '<key>'`（对齐 API 文案）+ 跳详情页。
- 「编辑仓库」复用同一组件（步骤 1 锁定 rclass/packageType 不可改）。

### 4.5 仓库详情·概要（`/repositories/:key`，tab：概要 | 制品 | 设置 | 危险区）

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ ← 仓库    docker-local                                    [Local] [Docker]   │
│           https://registry.example.com/docker-local  [⧉]      创建于 … by admin│
├──────────────────────────────────────────────────────────────────────────────┤
│ [概要]  制品  设置                                    ┌──── 危险区 ────────┐│
│                                                       │ 删除仓库…          ││
│ [1] 客户端接入（tab 折叠面板，按包类型给命令）           └───────────────────┘│
│ ┌──────────────────────────────────────────────────────────────────────────┐ │
│ │ ▸ Docker                                                              ▐⧉│ │
│ │   docker login registry.example.com                                   │ │
│ │   docker tag app:1.0 registry.example.com/docker-local/acme/app:1.0   │ │
│ │   docker push registry.example.com/docker-local/acme/app:1.0          │ │
│ │   ⓘ 首段是仓库 key；单段 name 404。明文 HTTP 需配 insecure-registries  │ │
│ │ ▸ crane / oras / helm                                                  │ │
│ └──────────────────────────────────────────────────────────────────────────┘ │
│ [2] 统计            [3] 本仓最近事件                                           │
│ ┌───────────────┐   ┌────────────────────────────────────────────────────┐   │
│ │ 镜像 47        │   │ 14:20 admin PUT acme/app:1.0.3     [查看审计 →]    │   │
│ │ tag  312       │   │ 13:55 ci-bot PUT acme/app:1.0.2                   │   │
│ │ 存储 84 GB     │   │ …                                                │   │
│ └───────────────┘   └────────────────────────────────────────────────────┘   │
│ [4] remote 特化：上游健康 / assumed-offline 倒计时 / 命中率 / 缓存字节数        │
│     virtual 特化：成员解析序列（优先桶 + 声明序，含 assumed-offline 标记）      │
└──────────────────────────────────────────────────────────────────────────────┘
```

- [1] 接入命令块：host 取 `server.base_url`（空则按当前请求 origin 推导——与后端 absolute path 口径一致）；每块独立拷贝按钮 [⧉]；命令内容与 docs/user/ 各接入文档同源（tech-writer 文档为准，UI 不发明新命令）。
- 危险区（[7]/右上图）：删除仓库需二次确认——非空仓显示「将删除 N 个制品」+ `□ 同时删除内容（deleteContent）` + **输入 key 确认**；对齐 API：不带 deleteContent 的非空删除是 400，UI 必须把这条路走通而不是让用户撞 400。

### 4.6 制品树浏览（`/repositories/:key/tree/*`）——generic 形态为主，docker/maven/npm/pypi 特化见 §3.4

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ docker-local / acme / app                                    [⧉ 复制路径]   │
│ 面包屑: [docker-local] / [acme] / [app]      过滤 [__________] ▾ 只看文件 □   │
├───────────────┬──────────────────────────────────────────────────────────────┤
│ 树 (280px)     │ 列表（当前层 children，keyset 分页）                          │
│ ▾ ◾ docker-   │ ┌────┬────────────┬──────┬─────────┬──────────┬───────────┐ │
│    local      │ │名称 │ 类型        │大小   │修改时间  │操作者     │操作        │ │
│   ▸ ◻ acme    │ ├────┼────────────┼──────┼─────────┼──────────┼───────────┤ │
│   ▾ ◻ app  ◀  │ │◻ v1│ 目录        │—     │2 小时前  │admin     │           │ │
│     ├ ◻ v1    │ │◻ v2│ 目录        │—     │1 天前    │ci-bot    │           │ │
│     ├ ◻ v2 ◀  │ │◻ v2/│ manifest  │ 12 MB │1 天前    │ci-bot   │详情 删除  │ │
│     └ ◻ …     │ │…（共 214 项，已加载 100）                              │ │
│  [懒加载：点开  │ │                              [加载更多 (100/214)] [1]     │ │
│   才拉该层     │ └──────────────────────────────────────────────────────────┘ │
│   children]   │ ── 选中节点详情面板（点击「详情」展开于表格下方，非跳页）────     │
│               │ sha256  a3f5…9c2e [⧉]   sha1  77d0…41 [⧉]   md5 … [⧉]        │
│               │ Docker-Content-Digest: sha256:a3f5…9c2e [⧉]                   │
│               │ mediaType application/vnd.oci.image.index.v1+json             │
│               │ layers: 8 层 · config: sha256:be11… [⧉] · 推入 ci-bot 14:20   │
└───────────────┴──────────────────────────────────────────────────────────────┘
```

行为约定：

- **左树**：仅渲染「已展开路径」（面包屑路径自动展开）；节点展开时拉取该目录一层 children（懒加载），排序目录在前。树高度超出容器即内部滚动；**不做全量树**。
- **右表**：显示当前选中目录的直接 children；分页见 §6（页大小 100，与 docker `n` 缺省一致；「加载更多」增量追加，不用页码——keyset 游标对页码不友好，且用户心智是「往下翻」）。
- **过滤框**：输入即在**已加载**条目中前端过滤，并提示「结果仅含已加载的 100/214 项 → [加载全部匹配]」（切换为带过滤的服务端查询，若 API 支持，§9-R1）。
- **docker 仓特化**：左树退化为镜像名两三级（repoKey 固定首段）；右表首层 = 镜像列表（`_catalog` 过滤本 repo），二级 = tag 表（`n`/`last` 分页），点 tag → 详情面板（manifest 视图，§3.4）。「删除」仅 by-digest，确认框列出将被级联的 tags。
- **maven 仓特化**：版本目录行给「复制 GAV 坐标」；`maven-metadata.xml` 在树中以特殊图标呈现，详情面板显示格式化 XML + latest/release 摘要行。
- **virtual 仓**：表格多一列「来源成员」（数据可得时，§3.4 rclass 行）；不可删除（405 语义），操作列只保留「详情」。

### 4.7 上传（对话框，从树页/详情页触发；仅 generic 与 maven local）

```
┌ 上传到 docker…generic-local/acme/ ──────────────────────────────┐  ← 标题即目标
│ 目标仓库 generic-local   目标路径 [acme/______________] [⧉]      │  ← 路径可改，
│                                                                  │     mono 输入
│ ┌────────────────────────────────────────────────────────────┐   │
│ │                                                            │   │
│ │            ⬇  拖拽文件到此处，或 [选择文件]                 │   │  ← 整块 drop
│ │                                                            │   │     zone，
│ │                                                            │   │     支持多选
│ └────────────────────────────────────────────────────────────┘   │
│ ┌──┬──────────────┬────────┬───────────────┬──────────────────┐  │
│ │✓ │ app.tar.gz   │ 12 MB  │ sha256 e3b0…  │ 上传完成 201      │  │  ← 服务端返回
│ │✓ │ sbom.json    │ 3 KB   │ 与本地一致 ✓   │ 上传完成 201      │  │     checksum
│ │⟳ │ big.bin      │ 1.2 GB │ 64% · 38 MB/s │ ━━━━━━━░░░░░      │  │     与本地计算
│ │✗ │ old.bin      │ —      │ 409 收到与实际 │ checksum 不一致    │  │     比对结果
│ └──┴──────────────┴────────┴───────────────┴──────────────────┘  │
│ ☑ 计算并附带 X-Checksum-Sha256（推荐）                            │
│ 覆盖语义提示：同路径不同内容 = 覆盖，需对旧文件有删除权限          │
│                                        [关闭]  全部完成后 [完成]  │
└──────────────────────────────────────────────────────────────────┘
```

- **进度**：单文件进度条 + 速度 + 已传/总量；批量文件各自独立，失败的文件给行内错误与「重试」。
- **校验和展示**（P2 原则的核心场景）：浏览器端流式计算 sha256（Upload 前），成功后展示「服务端 sha256 vs 本地」比对徽标；不一致（409）时同时给出 received / actual 两个值（对齐 API message 的双值语义）。
- 403（无写权限/覆盖需 d 权限）错误行明确指向权限模型：「需要对 acme/ 的 write（或对旧文件的 delete）权限」+ 链接到权限页。
- **maven 仓**：目标路径由表单生成（groupId / artifactId / version / classifier / packaging 五输入，实时预览生成的 layout 路径并做前端 layout 预检——非法组合前端即拦，不送到服务端吃 400）。
- docker/npm/pypi 仓不出现上传入口（§3.4），其位置以「接入命令」块替代（§4.5[1]）。

### 4.8 搜索（`/search`）

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 搜索                                                                          │
│ [名称或路径包含…____________________]  (⌘K / "/" 聚焦)                        │
│ 仓库 [全部▾]  包类型 [全部▾]  类型 [全部▾]                    约 1,203 条结果   │
├──────┬──────────┬──────────────────────────────────┬────────┬───────────────┤
│ 类型  │ 仓库      │ 路径 / 名称                       │ 版本    │ 更新时间        │
├──────┼──────────┼──────────────────────────────────┼────────┼───────────────┤
│ 🐳   │ docker-   │ acme/app                         │ v1.0.3 │ 2 小时前        │
│      │  local    │  (tags: v1.0.3, v1.0.2 …)        │        │                │
│ ☕   │ maven-    │ com/acme/demo-app                │ 1.2.0  │ 1 天前          │
│      │  local    │  (latest 1.2.0 · release 1.1.0)  │        │                │
│ 📦   │ npm-local │ demo-pkg                         │ 2.1.0  │ 3 天前          │
└──────┴──────────┴──────────────────────────────────┴────────┴───────────────┘
                              [加载更多 (100/约1203)]
```

- 结果行按类型给**语义化副行**（docker 列 tags 摘要、maven 列 latest/release、npm 列 dist-tags latest、pypi 列最新版本文件、generic 只列路径）——搜索是跨协议的统一入口，副行让工程师不点进去就能判断「是不是它」。
- 行点击 → 对应协议的树视图定位到该节点（复用 §4.6 详情面板）。
- 空关键词不发起查询（显示引导态）；搜索端点分页见 §9-R2。

### 4.9 安全：权限 target 编辑器（`/security/permissions/:name`）

permission target = `{name, repos[], includePatterns[], excludePatterns[], principals{users, groups}}`，actions ∈ read / write / delete（M1 已落地模型；M4 若扩 action，矩阵加列即可，布局不变）。

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 权限 / ci-out-rw                                                    [删除…] │
├──────────────────────────────────────────────────────────────────────────────┤
│ [1] 基本信息                                                                   │
│   名称 [ci-out-rw____]（编辑态锁定）                                            │
│   适用仓库  [✕generic-local] [✕npm-local] [+ 添加仓库 ▾]   ← chips 多选        │
│                                                                              │
│ [2] 路径模式                                                                   │
│   include patterns          exclude patterns                                  │
│   ┌──────────────────────┐  ┌──────────────────────┐                          │
│   │ ci-out/**        [✕] │  │ ci-out/tmp/**    [✕] │   ← 逐行 chip 列表，     │
│   │ release/*        [✕] │  │ [+ 添加模式]          │     支持 ** 与 *，       │
│   │ [+ 添加模式]         │  └──────────────────────┘     mono 渲染              │
│   └──────────────────────┘                                                    │
│   模式测试器 [ci-out/builds/42/app.bin______________] [测试]                    │
│   → ✓ 命中 include `ci-out/**`；✗ 被 exclude `ci-out/tmp/**` 排除               │
│     ⇒ 结果：不匹配（exclude 优先）          ← 逐条命中明细，模式高亮             │
│                                                                              │
│ [3] 主体与动作矩阵                                                             │
│   ┌────────────┬──────┬────────┬────────┐                                    │
│   │ 主体        │ read │ write  │ delete │   ← 用户行与组行分组渲染，           │
│   ├────────────┼──────┼────────┼────────┤     组行带 👥 前缀                   │
│   │ ci-bot      │  ☑   │   ☑    │   ☐    │                                    │
│   │ 👥 ci-team  │  ☑   │   ☐    │   ☐    │                                    │
│   └────────────┴──────┴────────┴────────┘                                    │
│   [+ 添加用户 ▾] [+ 添加组 ▾]                                                  │
│   ⓘ admin 隐式拥有全部权限，不列入矩阵                                          │
│                                                                              │
│                                        [取消]  [保存变更] → [4] 变更确认       │
└──────────────────────────────────────────────────────────────────────────────┘
```

- [2] **模式测试器是本页的灵魂**：include/exclude 的 `**` 语义（folder 前缀匹配、全段匹配）对人是认知负担——输入任意路径即时显示每条 pattern 的命中/排除与最终判定，把语义「可见化」。前端测试器与服务端 `auth.pathmatch` 判定必须同源（§9-R8：建议后端提供一次性判定端点或导出判定规则测试向量，防 UI 与 ACL 漂移）。
- [4] 保存前弹**变更摘要 diff**（「+ 授予 ci-team write @ npm-local」「− 移除 exclude ci-out/tmp/**」逐条列出）——权限是安全面，diff 确认后才提交。
- 列表页（`/security/permissions`）：name / 仓库数 / patterns 数 / 主体数 / 更新时间；行点击进编辑器。
- 用户页与组页为标准 CRUD 表格；**Token 页**关键约定：创建响应的 `access_token` 明文只在创建成功的面板展示一次（mono + 拷贝 + 「关闭后不可再查看」警示，对齐 NFR-S2「明文仅返回一次」）。

### 4.10 审计日志（`/audit`）

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 审计                                                                          │
│ 时间 [2026-08-20 00:00 ▾ ~ 现在]  操作者 [全部▾]  仓库 [全部▾]  动作 [全部▾]   │
│ 搜索对象路径包含 [____________]                                                │
├────────┬────────┬──────────┬──────────────────────────────┬──────────────────┤
│ 时间    │ 操作者  │ 动作      │ 对象 (repo/path, mono)        │ 来源 / 备注       │
├────────┼────────┼──────────┼──────────────────────────────┼──────────────────┤
│14:31:02│ admin   │ REPO_     │ maven-snapshot               │ 10.0.2.15        │
│        │        │ CREATE    │                              │                  │
│14:02:11│ ci-bot  │ PUT       │ docker-local/acme/app        │ token#84         │
└────────┴────────┴──────────┴──────────────────────────────┴──────────────────┘
                              [加载更多 (50/约1,842)]   [导出 CSV]
```

- 动作值原样显示（不翻译 enum——排障时要把值贴给同事/日志比对）。
- 时间列固定宽 mono（`HH:mm:ss`，跨天显示日期）；默认倒序。
- 「导出 CSV」仅在当前过滤条件下导出已加载集合或触发服务端导出（M4 后端能力，§9-R3）。

### 4.11 治理（GC / 备份 / 配额，共用的危险区模式）

```
┌ 存储 & GC ────────────────────────────────────────────────────────────────────┐
│ 存储概况：blob 12,483 · 逻辑 842 GB · 物理 311 GB   （与仪表盘同源）            │
│ GC 状态：上次运行 2026-08-19 03:00 · 回收 4.2 GB · grace 24h                   │
├─ 危险区 ────────────────────────────────────────────────────────────────────┤
│ [试运行 dry-run]  → 结果面板：候选 blob 列表（digest mono / 大小 / 最后引用时间）│
│                     摘要「预计回收 N 项 / X GB；grace 期内不回收」              │
│ [执行 GC] → 确认对话框（输入仓库实例名或 YES 确认）→ 进度 → 结果（实际回收数）   │
└──────────────────────────────────────────────────────────────────────────────┘
┌ 存储迁移（本地 → S3）───────── T-160 回写：实现形态（MigrationPanel）──────────┐
│ 只读进度卡，位于概况卡与危险区之间：状态点（未开始/迁移中/已完成/已完成（有    │
│ 失败））+ 进度水位条 + 百分比 + blob 总数 / 已迁移 / 已跳过（S3 已存在）/ 失败  │
│ / 错误（有才显示）+ 开始·结束时间；每 5s 轮询 GET /api/v1/storage/migration。   │
│ 收敛：501（实例未配置 S3 迁移）→ 一句提示降级、不渲染进度条；403（非 admin）→  │
│ 整面板隐藏（§3.6.3 L3）；轮询瞬断但有旧数据 → 保留进度 + 行内「上次刷新失败」   │
│ （观察进行中迁移时单次掉线不清屏）。控制台无触发/写入口——迁移由服务端          │
│ storage.migration 配置与 POST /api/v1/storage/migration/start 驱动（运维面）。  │
└──────────────────────────────────────────────────────────────────────────────┘
┌ 复制（/governance/replication）── T-159 回写：实现形态（ReplicationPage）────┐
│ 页头「复制 · 单向 push：源仓库 → 目标实例（ADR-0021）」+ 两张只读卡；每 10s   │
│ 轮询 GET /api/v1/replication/status（挂载即取；轮询刷新不清空已到达数据）。   │
│ ·复制目标表：状态（运行态标签 + 状态点；优先级 已停用 > 异常（N 失败）>       │
│   复制中 > 排队（N）> 正常）/ 目标名 / 目标 URL（mono + 拷贝）/ 仓库（源 →    │
│   目标，mono）/ pending / 进行中 / 失败（>0 标红）/ 累计成功 / 上次成功        │
│   （'—' = 从未成功）。targets[] 空 → 「未配置复制目标」空态（配置经 REST      │
│   /api/v1/replications 创建——T-180 已落地，见框尾 CRUD 面注记）。             │
│ ·最近事件表：时间（mono）/ 状态 badge（值原样英文不翻译，沿 §4.10 口径；      │
│   009 闭集 pending|in_progress|success|failed|skipped）/ 制品路径（按         │
│   replication_id 映射源仓前缀补全，mono + 拷贝）/ sha256（截断展示、拷贝      │
│   完整值，§7.3）/ 尝试次数 / 最近错误（空为 '—'）；事件跨配置合并、时间       │
│   倒序（最近 N 条，无分页——量级远低于 §6 虚拟化阈值）。                       │
│ 收敛（§5.1/§3.6.3）：loading 骨架仅首帧；403（非 admin）→ 页面主数据面单张    │
│   无权限卡（L2，复用缺省 empty-state 锚）；404（端点未桥接）/ 501（实例       │
│   未启用复制）→ 降级为一句提示、不渲染表格；其它错误：无数据 → 错误卡 +       │
│   重试，有旧数据 → 保留表格 + 行内「上次刷新失败」（瞬断不清屏，同迁移        │
│   面板语义）。控制台只读呈现——配置 CRUD 走 REST 面（T-180 已落地，见框尾）；  │
│   CRUD UI 另票（T-180 遗留 4），本页暂不消费。                                 │
│ 回写记录：实现先于契约，T-159 核验发现——GET /api/v1/replication/status 为     │
│   前端假设契约（形状按 internal/replication/model.go 推定，序列化沿           │
│   MigrationStatusView 惯例：snake_case / int64 计数 / *_at 文本、'' 表空）。  │
│   **T-180 桥接核验：status 面零差异**（假设全数坐实，「待桥接」收口）；同批    │
│   新增 CRUD 面（GET/POST /api/v1/replications、DELETE /{name}；无 PUT/        │
│   trigger）：bare array 列表 / 201 / 204 / 重复名 409 / target_password 只写   │
│   不读 / CRUD 面出 target_username 而 status 面不出 / enabled 缺省 true /      │
│   max_items_per_push 缺省 1000 / events limit 缺省 50——契约定稿见             │
│   architecture.md §7.1（回写记录：实现先于契约，T-180 核验发现）。本页仍       │
│   只读：CRUD UI 另票，组件零改动、repl-* 锚不变（§10.3）。                     │
└──────────────────────────────────────────────────────────────────────────────┘
┌ 备份 / 恢复 ──────────────────────────────────────────────────────────────────┐
│ [导出 export] → 任务进度 + 产物下载链接                                        │
│ [导入 import] → 文件选择 + 校验和确认 + ⚠「导入将覆盖当前元数据」→ 双重确认      │
└──────────────────────────────────────────────────────────────────────────────┘
配额页：每仓库一行（key / 已用 / 配额上限 / 水位条），超 80% 黄、100% 红；编辑上限行内进行。
```

---

## 5. 交互四态

### 5.1 通用原则

**Loading（加载）**

- 首屏/换页：**骨架屏**（skeleton），形状与最终内容近似（表头 + N 行灰块、卡片灰块），150ms 延迟显示（防闪烁）；详见 §5.2。
- 局部动作（保存/删除/上传）：按钮内 spinner + 禁用，**不用全屏遮罩**（遮罩阻断并行操作，管理面高频多窗口）。
- 慢查询（>3s）：骨架下方出现次级提示「仍在加载——大型仓库首次列举可能较慢」，>15s 追加「[在新窗口重试] 或检查实例健康」。

**Empty（空）**

- 每个空态 = 一句话说明 + 一个主行动 + 一条文档链接。禁止裸「暂无数据」。
- 区分**两种空**：① 从未有数据（仓库列表空 → 「创建第一个仓库」CTA）；② 过滤后为空（「无匹配 “maven-snap” 的仓库 · [清除过滤]」）。
- 空仓库详情（制品 tab）：generic → 「[上传第一个制品]」；docker → 展示 `docker push` 命令块（空仓的最佳空态就是教你怎么推）；npm/pypi 同理给 publish/upload 命令。

**Error（错误）**

- 统一呈现：区块内错误卡（图标 + 一句人话 + 原始 message 折叠区 mono + [重试]）。原始 message 默认折叠——工程师需要它排障，但不该淹没页面。
- 状态码分流：401 → toast「登录已过期」+ 重定向登录（带 return）；403 → 按 §3.6.3 分层收敛（L3 卡片/行级隐藏、L2 页面主数据面单张无权限卡——说明需要什么权限或「管理面需管理员」并给搜索/直链引导、L4 动作不渲染；表单/上传等**操作中** 403 仍走行内错误并指向权限模型，§4.7）；404 → 与空态区分（「仓库不存在」vs「还没有仓库」）；400 → 表单行内错误（建仓/上传）；5xx/网络 → 错误卡 + 重试 + 「查看健康状态」链接。
- **绝不**把三种错误体格式（errors[] / 纯文本 / OAuth 形）的差异暴露给用户；前端统一解析出 message。

**Success（成功）**

- 创建/保存：toast（含对象 key，mono）+ 就地跳转或状态刷新；删除：行淡出 + toast。
- 上传成功：**必须**展示服务端 checksum 比对结果（§4.7）——这是仓库产品的成功态，不是「上传完成」四个字。
- token 创建：专用结果面板（明文仅此一次，§4.9）。
- 不用 confetti/动画庆祝（P1 密度原则；且 `prefers-reduced-motion` 下骨架动画也关闭）。

### 5.2 骨架屏策略（慢查询大 repo 专项）

| 场景 | 策略 |
|---|---|
| 制品树首开（大 repo 根目录） | 左树仅骨架化**可见区**（~15 个节点占位），右表 10 行骨架；首层 children 返回后骨架替换，目录节点逐个可交互（不等全量） |
| 深目录翻页 | 「加载更多」按钮转 spinner，**已有行保持可交互**（增量追加到尾部，不重绘整表） |
| docker tags / _catalog | 同上；API `n=100` 分页与「加载更多」一一对应 |
| remote 仓首次浏览 | 可能触发回源（首字节慢）——骨架上方提示「首次访问将从上游拉取，可能较慢」（对齐 assumedOffline/TTL 语义，用户可预期等待） |
| 聚合视图（virtual） | 成员逐个返回即逐块渲染（流式），不等全部成员；某成员失败不阻塞整页（该子块显示成员级错误卡） |
| 搜索 | 输入防抖 300ms 后查询；结果区骨架 8 行 |
| 仪表盘 | 各卡片独立骨架独立到达（health 慢不该挡 storage stats） |

骨架动画：1.2s ease 循环的 3% 亮度脉动；`prefers-reduced-motion: reduce` 时静态灰块。

### 5.3 每页四态矩阵

| 页面 | loading | empty | error | success |
|---|---|---|---|---|
| 登录 | 按钮 spinner | —（未登录无空态） | 401 行内红字「用户名或密码错误」 | 跳 return 路由 |
| 仪表盘 | 卡片级骨架（各自独立） | 空实例：统计卡显示 0 + 「创建第一个仓库」CTA | 子系统非 ok 黄/红卡；API 失败该卡错误态，其余照常 | —（只读页） |
| 仓库列表 | 表格骨架 8 行 | ①建仓 CTA ②无匹配→清除过滤 | 错误卡 + 重试（403 显示无权限卡） | 建仓 toast + 跳详情 |
| 仓库详情 | 概要骨架 | 空「制品」tab 按协议给命令空态（§5.1） | 404「仓库不存在」（key 打错时） | 设置保存 toast |
| 制品树 | §5.2 专项 | 空目录「此目录为空」+ 上传/建目录入口 | 404（路径不存在，面包屑可回退）；403 无权限卡 | 删除行淡出；详情面板即时刷新 |
| 上传 | 行级进度条 | 拖拽区初始引导态 | 409 双值展示 / 403 权限指引 / 网络中断可重试 | checksum 比对徽标 + 201 |
| 搜索 | 结果骨架 8 行 | 引导态（无关键词）/ 无结果（给拼写与过滤建议） | 错误卡 + 重试 | 行点击定位到树 |
| 权限编辑器 | 表单骨架 | 新建态：空表单 + 默认值（include `**`） | 400 校验行内（重名等）；保存冲突 toast | diff 确认 → 保存 toast |
| 审计 | 表格骨架 | 「暂无审计事件（当前过滤下）」 | 错误卡 | 导出任务 toast |
| GC/备份 | dry-run 结果区骨架 | 无候选：「没有可回收的 blob」✓ 绿色空态（这是好消息） | apply 失败红色结果面板 + 重试 | 结果面板（预计 vs 实际回收） |
| Token | 列表骨架 | 「还没有 token」+ 签发 CTA | 403（非 admin）无权限卡 | 一次性明文面板 |

---

## 6. 大目录与性能的 UX 约定

1. **页大小 100**，与 docker API `n` 缺省一致；「加载更多」增量模式，不做页码跳转（keyset 游标 + 工程师「往下翻」心智）。
2. **树只懒加载一层**；右侧表格只列当前层 children；`list&deep=1` 深列举不用于浏览（仅搜索/导出消费）。
3. **前端过滤只作用于已加载集**，并显式提示已加载边界（§4.6）——绝不假装过滤了全量。
4. **行数 >500 时前端必须虚拟化渲染**（窗口化）；这是 UX 对实现的硬性要求（性能预算：首屏交互 < 1s，滚动不掉帧），实现方式归 T-86/前端票。
5. 已知 API 缺口与兜底：M1~M3 的 storage children / `_list` 无分页参数——过渡期前端「一次拉取 + 客户端分页」，children 超过 2,000 条时提示「目录过大，建议用搜索或 `_list?prefix=`」；§9-R1 提出分页参数需求，后端落地后前端切换为服务端分页（UI 形态不变，仍为「加载更多」）。

---

## 7. 设计 token

**交付形态**：CSS custom properties（`--bf-*` 前缀），一套语义变量 × 两套主题取值。不引组件库、不依赖 CSS-in-JS（与 T-86 的轻量边界一致——本文只定 token 值与语义，不定样式方案）。

### 7.1 色彩（暗色优先；亮色为等价次主题）

```
暗色（默认）                                 亮色
--bf-bg:            #0d1117                 #ffffff      页面底
--bf-surface-1:     #161b22                 #f6f8fa      卡片/表格
--bf-surface-2:     #1c2129                 #eff2f5      悬停行/次级面板
--bf-surface-3:     #222831                 #e4e8ed      输入框/drop zone
--bf-border:        #2d333b                 #d0d7de      分隔线/表格线
--bf-border-strong: #444c56                 #8c959f      悬停边框
--bf-text:          #e6edf3                 #1f2328      正文（≥ 12:1）
--bf-text-2:        #9198a1                 #57606a      次要（≥ 4.5:1）
--bf-text-muted:    #6e7681                 #6e7681      弱化（仅辅助时间戳等）
--bf-accent:        #4493f8                 #0969da      主操作/链接/焦点
--bf-accent-fg:     #0d1117                 #ffffff      accent 上的文字
--bf-success:       #3fb950                 #1a7f37
--bf-warning:       #d29922                 #9a6700
--bf-danger:        #f85149                 #cf222e
--bf-info:          #58a6ff                 #0969da
```

语义色使用规则：

- 四语义色只用于**状态表达**（badge、行内错误、水位条、确认按钮 danger 变体），绝不用于装饰。
- badge = 语义色 15% 透明底 + 语义色文字（两主题下文字对比度均需 ≥ 4.5:1，QA 用 §8 清单核验）。
- `--bf-danger` 保留给：错误、删除按钮、危险区边框；「assumed-offline」用 warning 不用 danger（静默期是设计内状态，非故障）。
- 仓库类型 badge 用中性色（surface-2 底 + text-2 字）——颜色预算留给状态。

### 7.2 间距 / 字号 / 圆角 / 密度

```
间距（4px 基数）：--bf-sp-1:4  -2:8  -3:12  -4:16  -5:24  -6:32  -7:48
字号：  --bf-fs-aux:12    表格辅助列/标签
        --bf-fs-body:13   表格正文（密度主字号）
        --bf-fs-form:14   表单/正文段落
        --bf-fs-h3:16     卡片标题/tab
        --bf-fs-h2:20     页面标题
        --bf-fs-h1:24     仅登录页品牌
行高：正文 1.5，标题 1.25
圆角：  --bf-r-sm:4（输入框/badge）  -md:6（按钮/卡片）  -lg:8（modal/drop zone）
密度：  表格行高 32px；导航项 32px；控件高 32px；页面内容区左右 padding 24px
        表格列间距 12px；危险区：1px danger 边框 + sp-4 内距
层级：  z-toast 100 / z-modal 90 / z-dropdown 80 / z-nav-sticky 70
```

### 7.3 等宽字体（mono）应用规则

mono 栈：`ui-monospace, "SF Mono", "Cascadia Code", Menlo, Consolas, "Liberation Mono", monospace`。

**一律 mono**：制品路径、目录路径、digest（`sha256:…`）、checksum（sha1/md5/sha256 hex）、repo key、tag 名、npm 版本号、maven GAV、文件名、审计对象列、上游 URL、接入命令块、token 明文、pattern（include/exclude）。

**例外（不用 mono）**：时间戳用表格专用 mono 变体（等宽防列抖动）——即时间戳也 mono；描述、用户名、组名、email 用正文字体。

规则：mono 值 = 可复制候选（P2）；凡是 mono 且超 20 字符的展示，配独立拷贝按钮或悬停拷贝图标；digest 展示可截断（`a3f5…9c2e`）但拷贝复制**完整值**。

---

## 8. 可达性

| 项 | 要求 |
|---|---|
| 对比度 | 正文 ≥ 7:1、次要文字与语义色文字 ≥ 4.5:1（两主题分别核验 §7.1 全部 token 组合）；焦点环 accent 对 bg ≥ 3:1 |
| 键盘 | 全页面可 Tab 序操作；树节点方向键（↑↓ 移动、→ 展开、← 收起）；对话框焦点陷阱 + Esc 关闭；表格行是链接（Enter 进入），不用 roving tabindex 复杂化 |
| 焦点 | `:focus-visible` 2px accent 外框 + 2px offset；不移除默认焦点环 |
| 语义标签 | 导航 `<nav>`、主内容 `<main>`、表格 `<table>` 表头 `scope`；图标按钮必带 `aria-label`（拷贝按钮标注被拷贝对象，如「复制 sha256」） |
| 状态表达 | 不以颜色为唯一信号——badge 带文字（⚠/✓/✗ 图标 + 文案）；进度条有百分比文本 |
| 动效 | 骨架脉动与行淡出在 `prefers-reduced-motion: reduce` 下禁用 |
| 语言 | 界面中文 `lang="zh-CN"`；命令块与 mono 值保留英文原文并标 `lang="en"` 不翻译 |

---

## 9. 对 T-86 / 后端的 UX 需求清单（API gaps，非设计）

控制台可用的现役 API 已覆盖约 80% 交互；以下缺口若不补，UX 按「兜底形态」降级（已注明）。此表是拆票输入，**设计与实现归对应 owner**。

| # | 需求 | 兜底（不落地时） |
|---|---|---|
| R1 | 目录列举分页：`GET /api/storage/{repo}/{path}` 的 children 或 `_list?prefix=` 支持 `limit` + `cursor`（keyset） | 客户端分页 + >2,000 条提示（§6.5），大目录体验降级 |
| R2 | 搜索端点：名称/路径前缀与包含匹配，过滤 repo/packageType，分页；返回类型化结果（docker tag / maven GAV / npm 包 / pypi 文件 / generic 路径）+ 语义副行字段 | 搜索页不可用，导航树内过滤顶替 |
| R3 | 审计分页 + 时间范围过滤 + CSV 导出 | 「加载更多」走 since 游标；导出按钮隐藏 |
| R4 | GC：状态查询（上次运行/可回收估算）+ dry-run + apply 触发端点 | GC 页只显示 stats 概况，操作引导用 CLI（`binflow-server gc`） |
| R5 | 备份 export（任务 + 产物下载）与 import（上传 + 确认） | 页面隐藏，文档引导 CLI |
| R6 | Token 列表/按 id 吊销的管理面（`GET /api/v1/tokens` 类）——现役只有签发与 revoke | Token 页只保留「签发」与「吊销（输入 token_id）」，无列表 |
| R7 | 配额：per-repo 上限设置与用量查询 | 配额页隐藏（M4 若裁掉则本页整体移除，不影响 IA 其余部分） |
| R8 | 权限 pattern 判定的同源保证：前端测试器与服务端 `auth.pathmatch` 一致（提供判定规则测试向量或一次性判定端点） | 测试器标注「仅供参考，以保存后实际生效为准」（体验降级，不阻塞） |
| R9 | remote 缓存统计（`/api/v1/remote/stats`，M3 P2）实装 + assumed-offline 状态查询 | remote 详情页统计块显示 `—` |
| R10 | `/binflow` 前缀下的浏览器可读代理已具备且可携 session 凭据——npm packument GET `/binflow/api/npm/{repo}/{pkg}`、pypi simple GET `/binflow/api/pypi/{repo}/simple/{project}/`（cookie `Path=/binflow` 覆盖；GET 无 CSRF 面）——确认 session 对这些路径等价可用（T-86）。docker tags/list **不作例**（E4 定案：cookie 结构性不达根级 `/v2`，docker 客户端走 `/v2/token` Basic 面——PRD M4 v1.3 CE-03 注记、docs/user/faq.md） | 走 `/api/storage/{repo}/{path}` 兜底（信息少：无 dist-tags 摘要 / 无 simple 归一文件行）；docker tag 面无 session 可携代理（E4），树数据源挂另票 |

---

## 10. data-testid 命名清单（QA 断言锚；v1.1 新增，v1.2 转正）

本节是 T-104 Playwright 断言的**唯一锚源**。§10.2/§10.3 与 `web/src` 实际落码逐一核对（T-116 首核 + **T-118 v1.2 全量 grep 复核**，含 `testid` 以 prop 形态传入 Card/EmptyState 的情形）。T-100~T-102 已全部落码，其 v1.1 预定锚于 v1.2 **转正为已落地清单**（与实现的差异注记见 §10.3 各组；未落/裁剪锚移入 §10.4，T-104 不得断言）。已落地锚改名视同破坏性变更，需过 conductor；后续新页面组恢复「先入本清单再落码」流程。

### 10.1 命名规则

- 一律 kebab-case；页面根 = `<page>` 或 `<page>-page`，页面内元素 = `<页面前缀>-<element>`。
- 动态段：实体标识用原值（`repos-row-<repoKey>`、`form-member-<memberKey>`）；序列用下标 `<i>`（`member-up-<i>`、`repo-cmd-<pkg>-<i>`）。
- **类段防碰撞（v1.2）**：动态段含主体名且主体有多类（用户/组）时，名段前先给类段——`perm-matrix-cell-{user|group}-<principal>-<action>`、`perm-matrix-remove-{user|group}-<name>`。用户 `ci-bot` 与组 `ci-bot` 同名时锚不得合并（T-101 遗留 2 定案）。
- 表单字段共享 `form-` 前缀（建仓/编辑复用同一表单组件）。
- 四态基元有缺省锚：`skeleton` / `error-card` + `error-retry` / `empty-state`（实例可用 `testid` prop 覆盖）/ `toast` + `toast-stack`。**403 收敛不产生新锚**：L3 隐藏 = 锚随卡片消失（断言用 `toHaveCount(0)` 类反断言），L2 复用 `empty-state` 缺省锚。
- 锚唯一性按**当前视图**计，不全局唯一（`repos-empty` 同时用于仪表盘仓库卡与仓库页空态——断言须 scope 到页面根锚内，如 `repos-page >> repos-empty`）。
- 可拷贝标识（P2）的拷贝按钮以 `aria-label` 标注被拷对象（§8），不强制 testid；需要断言拷贝行为时用 `copy-<field>` 命名。

### 10.2 已落地锚（T-98 基座 + T-99 仓库组；核对自源码，v1.2 复核通过）

**壳与全局基元（T-98）**

```
app-boot  app-nav  nav-version
session-toggle  session-user  logout-button
topbar-search  topbar-theme-toggle
confirm-dialog  confirm-accept  confirm-cancel
toast-stack  toast  skeleton  error-card  error-retry  empty-state（缺省）
```

**登录 / 404 / 占位（T-98）**

```
login-page  login-username  login-password  login-error  login-submit
not-found  placeholder-page
```

**仪表盘（T-98）**

```
dashboard（页面根）
dashboard-instance-card（恒可见——version 开放端点）
dashboard-health-card  dashboard-storage-card  dashboard-repos-card  dashboard-audit-card（admin 数据卡，403 隐藏）
dashboard-audit-table
repos-empty（空实例「创建第一个仓库」CTA——与仓库页共用锚名，注意 scope）
```

**设置（T-98）**

```
settings（页面根）  settings-instance  settings-password
password-old  password-new  password-confirm  password-error  password-submit
```

**仓库列表（T-99）**

```
repos-page  repos-create  repos-filter-key  repos-filter-type  repos-filter-package
repos-count  repos-table  repos-row-<repoKey>
repos-empty  repos-empty-filtered（403 分支复用 empty-state 缺省锚——「无权限查看仓库列表」）
```

**建仓/编辑表单（T-99）**

```
repo-form-page  form-summary  form-error  form-prev  form-next  form-submit
form-rclass-{local|remote|virtual}  form-package-{generic|docker|maven|npm|pypi}
form-key  form-key-error  form-key-ok  form-description
form-url  form-url-error  form-username  form-password
form-allow-private  form-private-warn
form-member-pick  form-member-<memberKey>  form-member-order  member-up-<i>  member-down-<i>
form-default-deploy
form-quota  form-includes  form-excludes
form-handle-releases  form-handle-snapshots  form-checksum-policy  form-snapshot-behavior
form-retrievalCachePeriodSecs  form-missedRetrievalCachePeriodSecs
form-socketTimeoutSecs  form-assumedOfflinePeriodSecs  form-hard-fail  form-priority
```

**仓库详情（T-99）**

```
repo-detail-page  repo-commands  repo-cmd-<packageType>-<i>
repo-usage-card  repo-usage-bar  repo-governance-card  repo-remote-card  repo-virtual-card
repo-danger-zone  repo-delete-button  repo-delete-content  repo-delete-confirm-key  repo-delete-reason
```

### 10.3 已落地锚·T-100~T-102 批次（v1.1 预定 → v1.2 转正；T-118 逐一对码）

```
树页（T-100）：tree-page  tree-breadcrumb  tree-node-<path>  tree-list  tree-row-<name>
  tree-filter  tree-refresh  tree-mkdir  tree-mkdir-input  tree-upload  tree-commands
  tree-empty-dir  tree-load-more
  node-detail  node-copy-<sha256|sha1|md5>  node-download  node-download-verify  node-perms
  delete-node-button  delete-error
上传（T-100）：upload-dialog  upload-target  upload-drop  upload-file-input  upload-file-<i>
  upload-retry-<i>  upload-verify-<i>
  upload-gav-<groupId|artifactId|version|classifier|packaging>  upload-maven-preview  upload-maven-input
搜索页（T-100）：search-page  search-input  search-filter-repo  search-count  search-results
  search-result-<i>  search-more（空态复用缺省 empty-state）
安全组（T-101）：
  用户：users-page  users-create  users-table  user-row-<name>
        user-form（创建/编辑共用）  user-form-{name,email,password,admin,groups,submit,error}
        user-form-group-<name>  user-detail-page  user-facts
  组：  groups-page  groups-create  groups-table  group-row-<name>  group-form
        group-form-{name,description,submit,error}  group-edit-<name>  group-delete-<name>
        group-delete-reason（409 冲突面板）  group-delete-dismiss
  权限：perms-page  perms-create  perms-table  perm-row-<name>  perm-editor-page
        perm-form-name  perm-repos  perm-repo-add  perm-repo-remove-<key>
        perm-pattern-{include|exclude}-<i>（chip 本体）
        perm-pattern-input-{include|exclude}  perm-pattern-add-{include|exclude}
        perm-pattern-remove-{include|exclude}-<i>
        perm-pattern-test  perm-pattern-result  perm-pattern-verdict
        perm-matrix  perm-matrix-cell-{user|group}-<principal>-<action>
        perm-matrix-remove-{user|group}-<name>  perm-add-user  perm-add-group
        perm-diff  perm-save  perm-danger-zone  perm-delete-button（行内错误复用 form-error）
治理组（T-102）：
  审计：audit-page  audit-filter-{repo|actor|action|since|until|path}  audit-count
        audit-table  audit-row-<i>  audit-more  audit-empty  audit-empty-filtered
  GC：  gc-page  gc-stats  gc-danger-zone  gc-grace-hours  gc-confirm-text
        gc-dryrun  gc-apply  gc-error  gc-result  gc-empty-ok
        迁移面板（T-160 回写，随 T-176 补录——实现先于清单；e2e/storage_migration.spec.ts 断言）：
        migration-panel  migration-status  migration-bar  migration-bar-fill  migration-pct
        migration-total  migration-migrated  migration-skipped  migration-failed
        migration-error  migration-started  migration-finished
        migration-unconfigured（501 降级提示）  migration-stale（轮询瞬断行内提示）
  复制页（T-159 回写，随 T-182 补录——实现先于契约，T-159 核验发现；端点已由
        T-180 桥接、零差异（见 §4.11 回写记录），组件零改动、锚随组件不变；
        e2e/replication.spec.ts 断言）：
        repl-page  repl-targets  repl-targets-table  repl-target-<i>
        repl-empty-targets（目标空态）  repl-events  repl-events-table  repl-event-<i>
        repl-empty-events（事件空态；两空态锚为 EmptyState 的 testid prop 形态）
        repl-unavailable（404/501 降级提示）  repl-stale（轮询瞬断行内提示）
        （403 分支复用缺省 empty-state 锚——L2 无权限卡，§10.1 四态基元）
  配额：quotas-page  quotas-table  quotas-empty  quota-row-<repoKey>  quota-bar-<repoKey>
        quota-edit-<repoKey>  quota-input-<repoKey>  quota-save-<repoKey>
        quota-cancel-<repoKey>  quota-error-<repoKey>
  备份：backup-page  backup-cmd-{export|import}
设置补锚：settings-health（§3.6.3 N1 收口已兑现——健康行 403 驱动 + 锚，T-101 落地）
```

v1.1 → v1.2 差异注记（核对基准 = v1.1 §10.3 预定清单 vs 源码）：

- **树页/上传**：预定锚全落。增补 `tree-breadcrumb` / `tree-filter` / `tree-refresh` / `tree-mkdir` / `tree-mkdir-input` / `tree-upload` / `tree-commands` / `tree-empty-dir` / `delete-error` / `node-download` / `node-download-verify` / `node-perms` / `upload-file-input` / `upload-verify-<i>` 与 maven 表单族（`upload-gav-*` / `upload-maven-preview` / `upload-maven-input`）。`node-copy-<field>` 的 field 域实证为 sha256|sha1|md5。
- **树页命令卡复用 `repo-cmd-<packageType>-<i>`**（§10.2 锚名，不带 tree 前缀）——断言须 scope 到 `tree-page`，防与仓库详情页同名碰撞（§10.1 视图内唯一规则）。
- **搜索页**：**`search-filter-{package|type}` 删除**——R2 类型化过滤未落地，实现仅 repo 过滤（csv 传参）；R2 落地时回填本节。增补 `search-count`、`search-results`（结果表容器，grep 补记——T-100 日志未列）。
- **安全组**：**`perm-matrix-cell` 细化**——`<principal>-<action>` 前插 `{user|group}` 类段（T-101 遗留 2，用户/组同名碰撞），同族 `perm-matrix-remove-{user|group}-<name>` 一致。token 族锚未落（P2 占位页，§10.4）。`perm-pattern-{input,add}-{include,exclude}` 无 `<i>` 下标（每栏一个输入/添加位），仅 `perm-pattern-remove` 与 chip 本体带 `<i>`——对码实证，勿按 v1.1 手写体例臆造下标。`perm-pattern-{include|exclude}-<i>`（chip 本体）为 grep 补记（T-101 日志 shorthand 未单列）。
- **治理组**：预定锚全落（`audit-export` 除外，P2，§10.4）。时间窗过滤落为 `audit-filter-since` / `audit-filter-until` 两个独立锚（v1.1 预定清单未含时间窗）；其余增补见上（`gc-danger-zone` / `gc-grace-hours` / `gc-confirm-text` / `gc-error` / `gc-empty-ok` / `audit-count` / `audit-table` / `audit-empty{,-filtered}` / `quota-bar|input|save|cancel|error-<repoKey>` / `quotas-table` / `quotas-empty` / `backup-page` / `backup-cmd-{export|import}`）。

### 10.4 未落地锚（T-104 不得断言；落地时回写本节并升版本）

| 锚 | 归属 | 状态 |
|---|---|---|
| `tokens-page` `token-create` `token-plaintext` `token-revoke-<id>` | 安全组 Tokens 页 | P2 兜底占位（v1.4：路由 `/admin/security/tokens` 落地，`placeholder-page` 承载占位态——旧 `nav-item.disabled` 计数断言随之退役，auth-shell/spec 改断路由 + `placeholder-page`） |
| `audit-export` | 审计 CSV 导出（ux R3） | P2 债务，不渲染 |
| `search-filter-package` `search-filter-type` | 搜索页类型过滤 | v1.1 预定、v1.2 删除——R2 类型化过滤落地时回填 §10.3 |
| `copy-<field>` | 拷贝按钮（§10.1 可选约定） | 现仅 `aria-label` 标注被拷对象（§8），无 testid；需要断言拷贝行为时再加 |

### 10.5 路由重排锚保全映射（v1.4 新增，T-235——M8 IA 重排的锚口径）

M8 路由表（console-m8 §1.4）重排后，§10.2/§10.3 的 **242 锚零改名**：锚挂在页面/组件上，不挂路由路径（ADR-0029 决策 3「testid 锚不随路由改名」）。W 序列断言迁移口径 = **路径断言随路由表改、锚断言不动**；旧路由 20 条客户端 redirect（兼容窗口，M9 移除）只出现在 `e2e/m8/shell.spec.ts` 的映射表腿。

| M8 新路由 | 承载锚（不变） | 备注 |
|---|---|---|
| `/dashboard` | `dashboard` 页根 + dashboard-* 卡族 | 原 `/`；登录落点让位 `/artifacts` |
| `/artifacts`、`/artifacts/:key/*` | `placeholder-page`（根）/ `tree-page` 族 + `?focus=` 深链参数 | 原 `/repositories/:key/tree/*`；跨仓树根归 T-236 |
| `/search` `/profile` | `search-page` 族 / `settings` + `password-*` | `/profile` 现挂设置页组件（T-239 拆分） |
| `/admin/repositories/{local\|remote\|virtual}` | `repos-page` 族 | 原 `/repositories`；Tab 形态归 T-240 |
| `/admin/repositories/new` `?rclass=` | `repo-form-page` + `form-*` 族 | Quick 建仓入口的参数形态（T-240 消费） |
| `/admin/repositories/:key[/edit]` | `repo-detail-page` 族 / `repo-form-page` | 原 `/repositories/:key[/settings]` |
| `/admin/security/{users\|groups\|permissions\|tokens}[/:name\|/new]` | `users-*` `user-*` `groups-*` `perm-*` 族 / `placeholder-page` | 原 `/security/*` |
| `/admin/governance/{audit\|gc\|quotas\|replication\|backup}` | `audit-*` `gc-*` `quota-*` `repl-*` `backup-*` 族 | 原 `/audit` `/governance/*` |
| `/admin/monitoring/storage` | `placeholder-page`（新页归 T-238） | 新路由 |
| `/admin/general/settings` | `settings` + `password-*`（改密迁 `/profile` 前 doubled 挂载） | 原 `/settings` |

**T-235 壳新锚（10 枚，先入本清单再落码流程兑现）**：

```
nav-mode-switch（双模式切换项——button + aria-current；admin/readonly_admin 可见）
topbar-breadcrumb（管理模式面包屑容器）
quick-set-me-up  quick-new-repo-{local|remote|virtual}（用户菜单·快速建仓；仅全量 admin）
quick-new-user  quick-new-group  quick-new-perm（用户菜单·新建；仅全量 admin）
menu-edit-profile（用户菜单·编辑档案 → /profile；全角色可见）
```

**锚总量复核口径（v1.4 实测）**：`grep -rn "data-testid" web/src/` = **293 落点 / 29 文件**（v1.2 基线 242 之后，T-104~T-234 各票陆续增锚至 HEAD 的 283 落点——ADR-0029 原写 283 即此原始 grep 数）；T-235 净变化 = 壳**删 0 改 0、新增 10**（AppShell 10 → 20），占位路由新增 0（复用 `placeholder-page`）。另：`web/src/styles/theme-smoke.spec.ts`（7 处选择器引用，非锚）随 T-232 遗留①迁出 `src/` 至 `e2e/m8/theme-smoke.spec.ts`，不再计入 src 侧 grep。

**锚总量（v1.2 核对基准）**：`grep -rn "data-testid" web/src/` = **242 处落点 / 27 文件**；动态族计一名约 **230 锚**（§10.2 + §10.3 合计）。v1.1 预定锚转正流程至此闭环（v1.1 文末「落码后回写本节并升 v1.2」约定兑现）。
