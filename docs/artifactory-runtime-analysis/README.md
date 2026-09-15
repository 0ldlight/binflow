# Artifactory 运行时行为分析（证据指针文档——正文在既有产物）

> 新宪章 Discovery 归档的**运行时侧**入口：对标实例活体取证资产的地图。只索引、不复制、不重扫。
> 全部资产采集自 22 轮兼容程序既有产物，采集日归档 2026-09-14。

## 1. 参照实例基线

- **主参照**：`http://localhost:8082`——Artifactory **7.161.20 pro，addons 全开**（admin；凭据不入库）。截图/DOM/tokens 即此实例。
- **副参照**：t226 OSS 7.84.10（console-ui.md 走查源——版本显著旧于 7.161，引用注意偏斜标注）。
- 反编译 7.161.24 见姊妹目录 `docs/artifactory-source-analysis/`。

## 2. 资产地图（docs/reverse/frontend/parity-capture/）

| 资产 | 规模 | 内容 |
|---|---|---|
| `screenshots/screens/` | 62 张 | 全屏页面（login/packages/树/搜索/仓库/安全/治理/企业面…） |
| `screenshots/dialogs/` | 23 张 | 弹窗族（建仓向导/Deploy/Set Me Up/权限两步选择器/删除确认/快搜 overlay/RB 创建五步…） |
| `screenshots/states/` | 19 张 | 状态面（四态/悬停/登录错误/权限拒绝/404/toast/树 loading…） |
| `tokens.json` | 六面 | palette / typography / radii / shadows / spacing / raw——设计 token 基线 |
| `nav-tree.json` | — | 侧栏 + 顶栏 + links 实测导航树 |
| `screens-catalog.json` | manifest 69 | 页清单（slug/url/seedScreen/effort/domSummary）+ 13 已捕弹窗 |
| `dom-snapshots/` | 10 件 | login-form/sidebar/topbar HTML + 7 个 walkthrough 探针 JSON |
| `walkthrough-results.json` | — | 走查结果 + apiCallsObserved（UI 数据源侧观察） |
| `run-log.json` / `states-gaps.md` | — | 采集批次日志 / 状态缺口表（dark-mode 与 SSO 变体 open） |

**合计 104 张截图。**

## 3. 采集方法学（可复跑）

工具链：`tools/penpot-sync/capture/`——`capture.mjs`（全量爬取，串行单浏览器）、`crawl-manifest.json`（清单）、`dialogs-walkthrough.mjs`（弹窗走查）、README（运行说明：凭据经环境变量）。
方法学纪律（states-gaps.md）：无 5xx 注入（api-failure 经客户端连接中断取得）；不修改参照实例安全配置（signing-key 前置缺失时 RB 创建止步于 review 步）；ref-load 纪律（不强制 >2k 节点虚拟滚动）。

## 4. 各面索引

| 面 | 索引文件 |
|---|---|
| 认证与身份（auth） | `auth.md` |
| 控制台壳与全局（console） | `console.md` |
| 仓库管理（repo） | `repo.md` |
| 制品浏览与操作（artifact） | `artifact.md` |
| 搜索（search） | `search.md` |
| 安全管理（security） | `security.md` |
| 管理与运维（admin） | `admin.md` |

## 5. 既有行为规格（截图之外的正文）

`docs/reverse/` 60+ 规格文件（REST/存储/协议/enterprise/storage/logging 四散目录）——总入口 `docs/reverse/README.md` 预期清单表。UI 行为本体：`docs/reverse/console-ui.md`（7.84.10 全程走查：IA/页面骨架/13 交互流/状态矩阵）+ `docs/reverse/frontend/`（routes.yaml 115 路径 / screens.yaml 41 屏四态 / api-map.yaml UI 网关面）。

## 6. 版本偏斜警示（引用时必读）

- 走查正文（console-ui.md）= 7.84.10；截图/DOM/tokens = 7.161.20；反编译 = 7.161.24。三线冲突序 Runtime > Decompiled > Distribution（frontend/README 头注）。
- 已知实例级缺口（states-gaps.md）：无主题控件（dark-mode 不可达——BinFlow 暗色须锚自有 token 基线）；SSO/MFA 登录变体未采集（实例 internal auth）。

## 7. 缺口声明（真无证据的面）

- **暗色主题参照**：7.161.20 实例无 UI 主题控件，dark 变体零实证（states-gaps open 项）。
- **SSO/MFA 登录表单族**：未采集（实例 internal auth）。
- **Pro 门内深层流**：RB 签发/分发等仅到 review 步（安全配置不可改纪律）；Federation/Lifecycle 等仅静态页截图。
