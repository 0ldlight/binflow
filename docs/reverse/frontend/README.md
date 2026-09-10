# frontend/ — Artifactory 前端资产目录（Phase 0 任务 #7）

> 本目录是**目录化索引**（program charter §11/§42 骨架）：把散在 `docs/reverse/*.md`、`docs/design/` 锚册、
> FE-Rewrite 审计三件套与反编译前端源四处的 Artifactory 前端知识组织进一个入口。**不重写既有内容**——
> 本 README 与三个 yaml 清单只做索引、映射与缺口暴露；行为规格本体仍在各引用文件。
>
> 证据源与版本（冲突序 Runtime > Decompiled > Distribution）：
> - **A 反编译**：`reverse-src/artifactory` → `frontend/` 子树 661 文件 = `artifactory-ui/`（宿主 SPA，source-map 还原 34 文件，Vue2 + vue-router history 模式 base=`/ui/`，`routes:[]` 运行时注册）+ `frontend-server/`（SSR/API 网关，627 文件 TS，Express；全部路由挂 `${PREFIX}/api/v1` 下按服务前缀分发：`/ui`→artifactory、`/access`→access、`/mds`、`/event`…）。版本 7.161.24。
> - **B 运行时**：`http://localhost:8082`（7.161.20）。MFE 资产自 `/ui/api/v1/imports-map` 实证（systemjs-importmap：artifactory 7.161.12 / access 7.191.14 / distribution / insight / catalog / xray / retention / runtime / pipelines / xsc / unifiedpolicy…），路由表自两个 MFE `app.umd.js` 提取（见 routes.yaml 头注命令）。
> - **C 活体走查**：console-ui.md 基于真实 OSS **7.84.10** Playwright 全程走查（2026-08-23）——版本显著旧于 A/B，管理树/路由形态可能与 7.161 有偏斜，引用时注意其标注。
>
> E 等级：E1 = 静态反编译观察；E2 = 静态 + 运行时资产实证；E4 = 运行时行为实证（探针/走查）。

## 1. 前端架构事实（目录化索引的前置认知）

当客户端请求 `/ui/*` 页面时（E2，反编译 + 运行时双证）：

1. **frontend-server（Node SSR/网关）**先接：静态资产在 `${PREFIX}/webapp`、`/client-mfe`；API 在 `${PREFIX}/api/v1/*`（`/ui/api/v1/...`）按服务前缀分发到 50+ 个子路由文件（`routes/*.ts`）；未匹配路由统一 404 `Not Found`。
2. **宿主 SPA**（`artifactory-ui`，Vue2）从 `imports-map` 逐个拉起**微前端（MFE）**：每个服务一个 MFE（app.umd.js + manifest.umd.js），路由由 MFE 在运行时向宿主 router 注册（宿主自身 `routes:[]`）。
3. **Artifactory 管理页与 Access 管理页分属两个 MFE**：`/ui/admin/artifactory/*`（服务自有树：backups/maintenance/import_export/…）在 artifactory MFE；`/ui/admin/{management,configuration}/*`（users/groups/permissions/ldap/oauth/…）在 access MFE（7.191.14）。
4. **SSR 面与后端 REST 面是两套 API**：SSR 控制器端点走 `/ui/api/v1/ui/*`（session/router-token 认证；basic auth 不被接受——E4 实证：`/ui/api/v1/ui/auth/current` 带 basic 返回 `{"name":"anonymous",...}`，`/ui/api/v1/ui/auth/screen/footer` 401）；制品/管理 REST 走 `/artifactory/api/*` 与 `/access/*`。
5. 开放路由清单（免认证）在 SSR 常量表集中定义（`/health`、`/auth/login` 族、`/system/version`、各服务 `/webapp` 等——见 api-map.yaml 附表）。

## 2. 资产地图（三线 + 锚册）

### 2.1 线一：Artifactory 侧行为规格（docs/reverse/ 本体）

| 文件 | 覆盖 | 版本/取证 | 限制 |
|---|---|---|---|
| `../console-ui.md` | 全局 IA（双导航模式/管理树/顶栏/用户菜单）、登录、≈20 个核心页面骨架、13 条交互流、组件与状态矩阵、OSS 缺位清单 | OSS 7.84.10 活体走查 + bundle 交叉 | 版本旧于 A/B；未覆盖 MFE 路由全集（retention/lifecycle 等） |
| `../auth-integration.md` §1.4/§2.3/§3.2/§6 | LDAP/OAuth/SAML 三协议**配置页** UI 端点表与表单形态（FE 票直接消费） | 7.161.16 反编译 | 仅三协议；SCIM/Vault/HTTP SSO/Crowd 无 |
| `../cron-scheduling.md` §4 | `/ui/api/crontime` next-run 预览端点 + 维护页 Cron/Next Run/Run Now 形态 | 反编译 + 官方文档 | — |
| `../gap-endpoints.md` | users/groups/permissions/storage 用量的 REST 回显与扇出（UI 数据源侧） | 7.161 反编译 | — |
| `../aql.md` §14 | UI 搜索族四端点 wire | 反编译 + t226 活体核验 | — |
| `../remote-browsing.md` | 树浏览器 remote 仓远端浏览语义（UI 树数据行为侧） | 反编译 + 官方 | — |
| `../inv-2-surface.md` §1 | `/ui/api/v1` 端点功能族目录（357 resource 全量清点的 UI 面部分） | 7.161.24 | 清点级，无逐端点 wire |

### 2.2 线二：BinFlow 侧 FE 资产（docs/design/，消费侧不是参照侧）

| 文件 | 大小 | 角色 |
|---|---|---|
| `docs/design/frontend-rewrite-audit.md` | 133KB | **FE-Rewrite 审计三件套之一**：五分册（架构依赖/页面能力/API 契约/质量资产/完备性批评）；§路由总图与 §API 完整路由表（≈156 管理面动词字面量）是 BinFlow 自有前端的事实账 |
| `docs/design/frontend-rewrite-architecture.md` | 12KB | 重写架构决策（栈/目录/数据层/阶段计划） |
| `docs/design/frontend-capability-matrix.md` | 7.7KB | 26 项能力对账 + e2e 保真三支柱 |
| `docs/design/console-artifactory-parity.md` | 120KB | **parity 锚册**：N/M/D/L/F/R 六系交互模式 + 差距矩阵 + B47 四态预归属（M14+ UI-parity 的直接引用源） |
| `docs/design/console-ux.md` | 325KB | UX 规范：IA/线框/**交互四态 §5**/token/a11y/data-testid 清单 |
| `docs/design/console-m8.md` | 62KB | M8 控制台设计规格（Artifactory 对齐重排的历史层） |

### 2.3 线三：反编译前端源（reverse-src/artifactory → frontend/，E1）

| 子树 | 规模 | 内容 | 对规格的价值 |
|---|---|---|---|
| `frontend/artifactory-ui/` | 34 文件 | Vue2 微前端宿主：router（history base `/ui/`）、store（modules + navigationConstraints）、microfrontendLoaders、filters（formatBytes/formatJPU/formatPackage/capitalize）、Element 主题 | 宿主机制（路由注册/导航约束）证据；**SPA 路由表不在此**（在 MFE bundle 内） |
| `frontend/frontend-server/` | 627 文件 | SSR/API 网关：routes/（50+ 子路由：Access 8、Artifactory、Distribution、Event、System、Xray×2、Pipelines、Metadata…）、Controllers/、Middlewares/（CSRF 白名单、CSP、nonce、TraceID、TenantID）、Constants/（AUTHENTICATION 开放路由表、NODE_SERVER_PATH 前缀表） | `/ui/api/v1` 全 API 面、认证中间件链、MFE 静态服务拓扑的第一手证据 |

运行时补充（E2/E4，7.161.20 实例）：MFE `imports-map`、artifactory MFE（7.161.12）与 access MFE（7.191.14）路由表提取——落在本目录 `routes.yaml`。

## 3. 目录清单（本目录产出）

| 文件 | 内容 | 性质 |
|---|---|---|
| `routes.yaml` | 路由→屏→MFE 映射（artifactory MFE 87 条 + access MFE 26 条有效路由 + 宿主/SSR 层） | evidence-index（E1+E2） |
| `screens.yaml` | 屏清单：每屏交互四态（加载/空/错误/成功）覆盖度与出处 | evidence-index（E1/E2/E4） |
| `api-map.yaml` | 屏→后端 API 映射（SSR 面 + REST 面），含 SSR 开放路由附表 | evidence-index（E1+E4） |

## 4. 阅读序（按任务进入点）

- **实现 Artifactory 对齐的 UI 票**：console-ui.md（行为基准）→ 本目录 routes/screens（路由与四态）→ console-artifactory-parity.md（模式级差距）→ aql.md §14 / auth-integration.md（数据源 wire）。
- **FE-Rewrite/自检**：frontend-rewrite-audit.md → frontend-capability-matrix.md → console-ux.md §5（四态规范）。
- **逆向取新证**：frontend-server/src/routes/*.ts（API 面）→ 运行时 imports-map + MFE bundle（路由面）→ console-ui.md §8（旧活体待验证项）。

## 5. 缺口清单（UNKNOWN 汇总——供任务 #8 unknown 队列收割）

1. **MFE 路由表仅两 MFE 提取**（artifactory/access）；distribution/insight/catalog/xray/retention 独立 MFE 的路由表未提取（BinFlow 范围外为主，retention 例外见 2）。
2. **retention/lifecycle/release-bundles 屏零规格**：路由在 artifactory MFE 表内实证（`admin/artifactory/services/retention/*` 5 条、`artifactory/lifecycle`、`release-bundles/target*`），console-ui.md（7.84.10 走查）未覆盖这些屏——Pro 许可功能，OSS 走查天然缺位。需要：Pro 实例（:8082）活体走查。
3. **7.161 管理树形态与 7.84.10 的偏斜未对拍**：console-ui.md §1.2/§1.3 的侧栏结构基于 7.84.10；7.161 access MFE 已见 `scim/vault_integration/vaults/global_roles` 等新页，旧稿无。需要：:8082 走查对拍增补（只读）。
4. **屏级交互四态大多 UNKNOWN**：console-ui.md §5 仅全局模式级（加载态=低置信）；逐屏四态见 screens.yaml 逐行标注。需要：Playwright 走查补态取证（失败/空数据注入较难，部分态只能反推）。
5. **屏→API 映射大量 UNKNOWN**：MFE bundle 内 API 调用经压缩混淆（无常量字面量可 grep），逐屏 wire 只能靠 devtools 抓包或 SSR 路由文件反推（api-map.yaml 已给 SSR 面 E1 全量）。需要：浏览器 devtools 网络面板走查（:8082）。
6. **宿主 navigationConstraints 机制未规格化**：`artifactory-ui/src/store/navigationConstraints/`（contextBuilder/module/utils）是管理树按许可/权限裁剪的机制层，行为语义未提取。
7. **onboarding/quick-setup/create_migration/migration_tool 屏**：路由实证存在，屏内容零规格。

## 6. 与既有规格的一致性自检

- 本目录不与 console-ui.md/parity 册重复记行为——凡引用处均带文件+节号。
- 与 console-ui.md §1.2 的差异（access MFE 含 scim/vaults 等新页）非冲突，是版本差（7.84.10 vs 7.161.14）——已在 §5-3 登记为对拍缺口，不改旧稿（裁定权 conductor）。
