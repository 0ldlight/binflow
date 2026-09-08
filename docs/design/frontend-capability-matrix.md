# BinFlow 前端重写能力矩阵（frontend-capability-matrix）

> Phase 0 产出之二。30 节总令 §二十一「先盘点再设计，不凭感觉漏功能」的兑现：每能力 × 三面（页面/API/e2e）对账 + 重写处置。
> 事实源：frontend-rewrite-audit.md 四报告（含矛盾裁决表）。基线 develop@74174a07。
> 处置记号：**保持**（行为原样承接）｜**升级**（行为保、实现升）｜**解锁**（API 在而旧 FE 未用，重写接上）｜**新设**（总令要求的新 UX）｜**N/A**（闭集裁定）。

## 1. 总令 26 项能力对账

| # | 能力 | 页面面（现役载体） | API 面（消费/可用） | e2e 面（覆盖 spec） | 处置 |
|---|---|---|---|---|---|
| 1 | Login | LoginPage（密码+OIDC 探测 302 hack+LDAP 同表单） | POST/GET/DELETE session；oidc login/callback；**未用 auth/methods** | login/auth-shell/console-smoke | 升级（auth/methods 直消费，消 B1 漂移；SSO 三态改真端点） |
| 2 | Profile | ProfilePage（改密+identity token 自助+SSH 缺位卡） | password PUT；token POST（step-up 双腿） | t457/m9-oidc-stepup | 升级 |
| 3 | Dashboard | DashboardPage 五卡（403 L3 收敛/审计深链） | health/stats/repositories/audit/version | m8-auxiliary/governance-monitoring | 升级（卡片→指标面板形态） |
| 4 | Repository list | RepositoriesPage（三 Tab/7 列/排序/过滤/列选/usage 批量注水） | GET repositories?type= + usage?include=counts（cap≤3 钉死） | m8-repositories-admin/m9-usage-fanout/t443 | 升级（轻量 table+inspector） |
| 5 | Repository creation | RepositoryFormPage（包型网格→三段步进+dirty-gating） | PUT repositories/{key}（create 臂）+addons | t383/t439/t441/t443 | 升级（RHF+Zod 重写验证，30+ 字段域规则照 §4 表单盘点迁移） |
| 6 | Repository editing | 同上 edit 模式（rclass 锁定/Test 三臂草稿探测） | POST repositories/{key}；test | 同上+t443 | 升级 |
| 7 | Repository detail | RepoDetailPage（三 Tab/命令块/水位/QuotaEditor/危险区） | GET/{key}+usage/{key}+replications | m8-repositories-admin | 升级（→ Overview/Artifacts/Configuration/Storage/Permissions/Replication/Webhooks/Activity 八 Tab 总令形态） |
| 8 | Artifact browsing | ArtifactsBrowser 2,044 行（树+children 表+URL 即状态+工具带+右键+BIG_DIR） | storage ?list 懒单层+docker_tags+usage | artifacts-tree/t434/t131/t134/m15-virtual/remote-browse | **新设+升级**（Artifact Explorer=第一优先级：AG Grid+TanStack Virtual 虚拟化树，TREE_LEVEL_CAP/load-more 升级为真虚拟滚动；键盘/多选/上下文菜单全套） |
| 9 | Artifact upload | DeployDialog（队列泵+XHR 进度+maven GAV+409/403/413 语义卡） | 内容面 PUT+X-Checksum-Sha256；**MPU 7 op 未用** | artifacts/m8-setmeup-deploy/t104-review-leftovers/t104-perf（1GB） | 升级+**解锁**（MPU 分片上传可选接入） |
| 10 | Artifact download | 两形态（直接下载+流式 sha256 对账 tee） | 内容面 GET（streamSha256） | artifacts/t494（纯浏览零计数） | 升级 |
| 11 | Artifact delete | 危险确认+404 幂等+选中回跳 | 内容面 DELETE（目录尾斜杠） | artifacts | 保持 |
| 12 | Artifact copy/move | **无 UI**（树右键「复制」=剪贴板路径，语义分立） | api/copy、api/move、archive/download **0 调用** | 无 | **解锁**（Explorer 上下文菜单+多选批量操作——总令十二 inspector/批量形态的自然落点） |
| 13 | Search | SearchPage+AqlPanel+ResultsTable（顶栏驻留+recent+AQL 尾缀链+列选） | search/aql+artifact；**UI search 9 op 未用** | t449/t451/m15-t419/m9-fr82 | 升级（AG Grid 结果面）+评估解锁 artifactsearch 家族 |
| 14 | Users | Users/UserCreate/UserDetail（E2 单请求/角色三值/lastLogin/穿梭） | users CRUD 5 op | m8/m9-users-groups/t453/t384 | 升级 |
| 15 | Groups | Groups/GroupForm（409 点名 target 解析/逐用户落盘幂等） | groups CRUD 5 op | 同上+security | 升级 |
| 16 | Roles | **闭集 N/A**——adminRole 三值（admin/readonly_admin/user），无管理页无独立 API | users 载体字段 | rbac.spec 三角色走查 | N/A（矩阵单列裁定行——不新设 Roles 页；角色语义在 Users 表单内） |
| 17 | Permissions | PermissionsPage+PermissionEditorPage（两步对话框+五列矩阵+tester+diff 确认） | v1/permissions 4 op（m-holder filter=manage） | m8-permissions/m9-mholder/t455/security | 升级 |
| 18 | API Tokens | TokensPage+Profile 载体（一次性明文/台账会话态/吊销 by-id） | token mint/revoke（form-urlencoded） | t386/t457 | 升级（台账=服务端清单端点缺位 §9-R6——如实注记延续） |
| 19 | Audit | AuditPage（keyset 页窗/五过滤/54 词表/列选） | v1/audit cursor | governance/t387 | 升级（AG Grid+虚拟滚动——总令点名大数据面） |
| 20 | Webhooks | 列表+SubscriptionDialog+Drawer（66 型复选墙/secret 三态/试发/投递记录） | event/api/v1 6 op；**outbox 2 op 未用** | m13-t366×2 | 升级+**解锁**（outbox 死信面：列表+replay——T-496 已备 API） |
| 21 | Replication | ReplicationsSection 内嵌+ReplicationPage（10s 轮询/封锁/crud=删重建） | replications 10 op 全消费 | t404/replication | 升级 |
| 22 | Trash | TrashPage（锁卡/五元组/恢复/清空 typed EMPTY） | trash 3 op+storage 浏览复用 | trash-can/m12-t351/m13-t372 | 保持形态升级 |
| 23 | Governance | GC/Backup/Quotas/Migration（cron 槽/danger YES/5s 轮询） | maintenance/backups 全族 | governance/t462/storage_migration | 升级 |
| 24 | Monitoring | Storage/Status/Logs/SystemInfo（双源日志/尾随刷新） | health/stats/logs/schedules/version | t459-monitoring-nav | 升级（+**解锁** settings 旋钮面、QRL 管理面） |
| 25 | Settings | 兼容重定向→system-info | **v1/system/settings 0 调用** | — | **解锁**（Settings 页真身：folder_download 六字段+trashcan.retention_days 旋钮） |
| 26 | Addons/licensing | LicenseAddonsPage（装/卸/矩阵三态） | system/license+addons | m10-console-license-addons | 保持形态升级 |

## 2. 总令未列但盘上存在的域（防漏）

| 域 | 现役 | 处置 |
|---|---|---|
| Builds（M17 新） | BuildsPage 三视图+搜索档 | 升级（+**解锁** promote/retention 写面——API 在，旧 FE 明文「无 UI」） |
| Bundles（M17 新） | BundlesPage 三视图+HEAD 探针 | 保持形态升级 |
| 认证配置（LDAP/OAuth/SAML） | AuthConfigPage 数据驱动 FieldDef+SP 证书 | 升级（数据驱动表单模式平移 RHF 动态 schema） |
| keypair（GPG） | **无 UI**（仅审计词表出现） | **解锁**（API 10 op 全备） |
| reindex 动作 | **无 UI** | 解锁（仓库详情高级动作区） |
| 帮助/About/文档站 | help 菜单+/binflow/docs 内嵌站 | 保持（文档站零改） |
| i18n 双语 | 自研内核 2,054 键 | **保持机制原样**（三道闸/懒 chunk/reload——仅组件消费面随重写重排） |
| 主题 | tokens.css 双主题 data-theme | 升级为 Tailwind 主题层（token 值原样迁移） |
| 品牌资产 | favicon/PWA/marks/lockup | 保持（gen/wire 脚本原样） |

## 3. e2e 保真三支柱（重写验收的对账基线）

1. **testid 锚**：874 src 家族/4,218 spec 引用原样迁入（§10.5 先例=换栈零锚改名）；anchor-audit 仅改 src 解析面。
2. **wire 原文**：错误消息 verbatim 上屏纪律（errors[]/纯文本/OAuth 三形态折衷器平移 lib/api.ts toApiError）。
3. **键盘行为**：树箭头/Shift+F10/tablist/焦点陷阱——Radix 焦点行为差异需**重验**（76 spec 中键盘腿清单=m8/keyboard+smoke+permissions 等）。
