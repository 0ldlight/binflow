---
title: Artifactory → BinFlow 操作路径对照表
sidebar_position: 31
---

# Artifactory → BinFlow 操作路径对照表

> 适用版本：M8（控制台新信息架构，对齐 Artifactory 7.84 操作流）；M9 增补删用户对照行。
> Artifactory 侧路径依据活体行为规格 `docs/reverse/console-ui.md`（OSS 7.84.10，clean-room 产出——行为描述，无 JFrog 资产复制）；BinFlow 侧全部路径在 HEAD（`89b27ce` 构建）scratch 实例上 Playwright 走查验证。概念层的术语对照（local/remote/virtual、permission target、checksum 等）见 [FAQ · 从 Artifactory 迁移对照表](faq.md#从-artifactory-迁移对照表)；数据搬迁工具见 [bf-migrate 迁移指南](guides/migrate-artifactory.md)。

M8 起 BinFlow 控制台与 Artifactory **同一动作在同样的位置、走同样的步骤**——本表逐任务给出两侧路径，帮助 Artifactory 用户零学习成本切换。BinFlow 路径均为登录后控制台内路径（前缀 `$BASE/binflow/ui`）。

## 导航结构对照

| | Artifactory（7.x 新 UI） | BinFlow（M8） |
|---|---|---|
| 应用模式入口 | 侧栏 Application：Dashboard、Artifactory（Packages/Builds/Artifacts）、Xray、Distribution、Pipelines | 侧栏 **应用**：仪表盘、制品（`/artifacts` 跨仓树） |
| 管理模式入口 | 侧栏 Administration：Projects、Environments、Repositories、User Management、Authentication Providers、General、Proxies、Monitoring + SERVICES → Artifactory | 侧栏 **管理模式五分组**：仓库 / 用户与权限 / 治理 / 监控 / 常规（`/admin/**`） |
| 模式切换 | 侧栏底部切换项 | 同位置（应用模式显「管理」，管理模式显「返回应用」） |
| 登录落点 | `/ui/packages`（Packages 页） | `/artifacts`（跨仓制品树——BinFlow 无 Packages 聚合页） |
| 侧栏底部 | 许可与版权行 | 版本行 `BinFlow v<version> · 单二进制制品仓库` |
| 全局搜索 | 顶栏类型下拉（Packages/Artifacts/Builds）+ 搜索框 | 顶栏搜索（固定类型「制品」；`⌘K` / `/`） |

> Artifactory 的 SERVICES → Artifactory 三级服务树不采纳：BinFlow 是单服务产品，其承载的功能（备份/维护）归入「治理」分组。

## 常见任务逐条对照

| 任务 | Artifactory 路径 | BinFlow 路径 | 备注 |
|---|---|---|---|
| 建仓 | Administration → Repositories → Repositories →「+」→ 包类型选择 → 表单 | 管理 → 仓库 → `+ 添加仓库` 下拉三预选（Local/Remote/Virtual）→ 分路由建仓页 → 包类型网格（13 型磁贴，进阶型带档位徽章）→ **三段步进表单**（Basic/Advanced/Replications，`/admin/repositories/{local\|remote\|virtual}/new`） | 同为进页先选包类型再填表单；Tab Local/Remote/Virtual 列表同构；BinFlow 步进条对位 Artifactory 的 Step 分段 |
| 找仓库 / 看仓库详情 | Repositories 列表行点击 | 仓库列表行点击（`/admin/repositories/:key`） | BinFlow 详情页含接入命令块与统计卡 |
| 编辑仓库 | 列表行 → Edit | 列表行 → 编辑页（`/admin/repositories/:key/edit`） | BinFlow 编辑态锁定 rclass/包类型 |
| 删仓 | 列表行垃圾桶 → Delete 对话框 | 列表行删除图标 / 详情页危险区（`/admin/repositories/:key`） | BinFlow 更强确认：非空仓须勾选「同时删除内容」+ **输入 repo key** |
| 建用户 | User Management → Users → New User | 管理 → 用户与权限 → 用户 → `+ 新建用户`（路由整页表单 `/admin/security/users/new`，**Retype Password 双录**） | 编辑表单同构（设置/选项/口令/相关组穿梭/权限矩阵）；页脚 Cancel\|Reset\|Save 同构；BinFlow 角色下拉三值（Artifactory 无对应面，见 FAQ） |
| 删用户 | User Management → Users → 行 Delete（对话框确认） | 用户列表行删除 / 编辑页危险区（M9 起；**输入用户名强确认**） | 两侧均不可逆；关键差异：BinFlow 三护栏 400（内置 admin / 最后一个 admin / 自删——Artifactory REST 面这些守卫在逆向规格中低置信/不可见）、级联吊销 token/会话；**重复删除 BinFlow 404、Artifactory 视为成功**（幂等 vs 有意非幂等，见[治理指南](admin/governance.md#删除用户m9-起)）；禁用（`enabled:false`）是离场的可逆路径 |
| 建组 | User Management → Groups → New Group | 组 → `+ 新建组`（`/admin/security/groups`） | BinFlow 组无 admin 位（防组内自提权） |
| 配权限 | User Management → Permissions → Create Permission → Edit Repositories（两步） | 权限 → `+ 新建权限` → 分区编辑器 → `编辑仓库…` 两步对话框（`/admin/security/permissions`） | 动作词两侧同为五值：Artifactory `read/deploy-cache/annotate/delete/manage`，BinFlow REST 面同集（`write` 收为 deploy-cache 别名；控制台矩阵五列化呈现更新中）；BinFlow 增模式测试器与保存前 diff |
| 找制品 | Artifactory → Artifacts 树（`/ui/repos/tree/...`）或顶栏搜索 | 应用 → 制品 树（`/artifacts/<repo>/<path>`）或顶栏搜索（`/search`） | 同为 URL 即状态、深链自动展开、树懒加载、`Filter repositories` 过滤 |
| 树上操作制品 | 树右键（Delete/Download 等） | 树右键或 `Shift+F10`：文件=复制路径/下载/删除；目录=复制路径/删除/刷新；仓库=复制仓库路径/刷新/在仓库管理中打开 | BinFlow 无 Move/Copy、无收藏/星标、无 Trash Can（删除即永久） |
| Set Me Up（客户端接入） | 选中仓库 → 页头 `Set Me Up`；或用户菜单 Quick Repository Creation → Set Me Up | 树页头 `Set Me Up` / 仓库列表行 / 仓库详情页头 → 包类型网格 → 配置/部署 Tab | BinFlow 同含「生成令牌」（24h token，非 admin 走 step-up 内联重验）；指令内容与接入文档同源 |
| UI 上传（Deploy） | 树页头 `Deploy` → 对话框（Target Repository / 模式 / 拖拽 / Target Path / Deploy） | 树页头 `部署 Deploy` / 仓库列表行 / 详情页头 → 对话框字段序相同 | BinFlow 仅 local Generic/Maven 可 UI 上传；docker/npm/pypi 以接入命令块替代；路径含特殊字符时显示编码回显 |
| 下载制品 | 树详情 Download | 树详情右键 → 下载（sha256 一致性提示） | — |
| 删制品 | 树右键 Delete | 树右键 → 删除（强确认，「制品不可变，删除没有撤销」） | — |
| 签发 Access Token | User Management → Access Tokens → Generate Token | Set Me Up 对话框「生成令牌」（本人 24h token）；或 REST `POST /api/security/token`（admin 可代铸） | BinFlow token 列表/吊销管理页为占位（吊销输入 token_id 或走 REST） |
| 吊销 Token | Access Tokens 列表 → Revoke | Access Tokens 占位页输入 token_id 吊销；或 REST `POST /api/security/token/revoke` | — |
| GC / 空间回收 | SERVICES → Advanced → Maintenance（GC cron + 配额百分比） | 管理 → 监控 → 维护（GC）（`/admin/monitoring/gc`）：dry-run → apply（输入实例名确认）+ **计划任务三槽卡**（cron 到点执行） | cron 调度已落地（[计划任务指南](admin/cron-scheduling.md)）；配额独立成页且为 per-repo 字节模型；Artifactory 的配额百分比/Compress/Prune 槽位无对位载体（页面如实缺位注记） |
| 配额 | Maintenance 内 `Enable Quota Control` 两个百分比 | 治理 → 配额（`/admin/governance/quotas`）：每仓水位条 + 行内编辑上限 | BinFlow per-repo `quotaBytes`（超出 413），比 OSS 两字段更强 |
| 审计 | （OSS 无此页；企业版 Audit Log） | 治理 → 审计日志（`/admin/governance/audit`）：过滤 + 游标分页 | BinFlow 自有页——OSS 7.84 无对应路由 |
| 备份 | SERVICES → Artifactory → Backups（cron 计划列表 + 表单） | 监控 → 备份 / 恢复（`/admin/monitoring/backup`）：**定时备份卡**（cron 计划列表 + 表单）+ CLI 引导卡 | cron 计划备份已对齐（[计划任务指南](admin/cron-scheduling.md#定时备份到点-export)）；实例零预置条目（Artifactory 出厂带 backup-daily/weekly）；产物形态与恢复链见[备份与恢复手册](admin/backup-restore.md) |
| 恢复 / 导入 | SERVICES → Import & Export | 备份 / 恢复 页 CLI 引导卡（import 是**停机带外 CLI**，有意不做 UI）；完整链见[备份与恢复手册](admin/backup-restore.md) | **语义差异**：恢复走带外通道，无 UI 进度面 |
| 看存储占用 | Monitoring → Storage Summary | 管理 → 监控 → 存储（`/admin/monitoring/storage`）：刷新行 + 汇总卡 + 逐仓表 | 同构（TOTAL 首行/列序对齐） |
| 看系统信息 | General → Settings | 管理 → 监控 → 系统信息（`/admin/monitoring/system-info`） | BinFlow 只读展示（写面在 `binflow.yaml`）；Artifactory 的 Logo/Custom Base URL 编辑不建 |
| 改自己的口令 | 用户菜单 → Edit Profile | 应用 → 编辑档案（`/profile`） | — |
| 复制（Replication） | 仓库编辑 Replications Tab（OSS 为降级提示）/ 全局复制页（许可功能） | 治理 → 复制（`/admin/governance/replication`，全局观测 + **调度列**）+ 仓库编辑页 Replications 节（local 仓配置 CRUD + 启停 + **`cron` 调度字段**） | BinFlow 复制配置自 M6 起 REST 全量可用（无 license 门）；`cron` 已为真字段（**cron 双轨**——事件轨不变，见[计划任务指南](admin/cron-scheduling.md#复制域cron-双轨)）；sync 等其余字段仍为预留位 |
| 用户菜单快捷动作 | Quick Repository Creation / New User·Group·Permission | 用户菜单同构（快速建仓子菜单〔新建 Local/Remote/Virtual 仓〕、新建用户/组/权限） | 非 admin 菜单按可见性裁剪；子菜单里的 `Set Me Up` 项暂为占位链接——Set Me Up 入口以树页/仓库列表行/详情页头为准 |

## Artifactory 有而 BinFlow 不建的面（如实登记）

| Artifactory 面 | BinFlow 现状 |
|---|---|
| Xray（漏洞扫描/合规） | **无对应**——产品 Non-goal，永不 |
| Pipelines / Distribution / Builds / Projects / Integrations | **无对应**——JFrog 独立产品，Non-goal |
| Packages 卡片落地页 | 不建（无包索引聚合面）；制品树是最近似落点 |
| Authentication Providers（LDAP/SAML/OAuth/HTTP SSO/Crowd 的 **UI 配置**） | 不建 UI；OIDC/LDAP 经 `binflow.yaml` 配置（[OIDC](guides/oidc-config.md) / [LDAP](guides/ldap-config.md)） |
| Repositories → Layouts（自定义仓库布局） | 无此模型，不建 |
| General → Mail Server / Webhooks / Manage Integrations、Proxies | 无对应功能面，不建 |
| Monitoring → System Logs（日志查看器） | 无端点，不建；日志在服务端结构化 JSON 输出 |
| Property Sets / Maven Indexer / Keys / Certificates / Config Descriptor | 无对应，不建 |
| 树的 Trash Can（回收站）/ My Favorites / 跨路径 Move·Copy | 回收站已有（治理页，pro 槽）；树内 **My Favorites 收藏过滤已有**（树头工具带，浏览器本地）；跨路径 Move/Copy 走 REST（[制品操作族](admin/artifact-operations.md)），树内不建入口 |
| AQL / 搜索族 | **AQL 已有**（items 域子集 + `stat.*` 统计字段 + usage 端点，见 [AQL 搜索指南](aql.md)）；老搜索三端点（gavc/prop/pattern）已有；`props`/`users`/`badge` 拼写 404 有意不做 |

## BinFlow 有而 Artifactory OSS 无对应的面（自有增强）

| BinFlow 面 | 说明 |
|---|---|
| 审计日志页 | OSS 7.84 无路由；BinFlow 全量动作审计 + 过滤查询 |
| per-repo 配额页（水位条） | OSS 仅 Maintenance 两个百分比字段 |
| 全局复制配置页 | OSS 404（许可功能）；BinFlow CRUD 端点已落地 |
| 角色三值模型（`readonly_admin` 实例级只读管理员） | Artifactory 无对应概念（近似能力 = target 只授 read） |
| Set Me Up 内 token 铸造的 step-up 内联重验 | Artifactory 无二次认证概念 |
| 权限模式测试器 / 保存前 diff 确认 | 安全面增强 |

## 下一步

- 控制台全貌：[Web 控制台使用指南](console.md)
- 概念术语对照与迁移三步走：[FAQ](faq.md#从-artifactory-迁移对照表)
- 定义与数据的批量搬迁：[bf-migrate 迁移指南](guides/migrate-artifactory.md)；真实源实例（OSS 7.84.10）迁移实录与差异清单见[附录 V28](admin/real-env-appendix.md#v28真实-artifactory-迁移实腿dep用户环境)
