# Penpot Phase A — Artifactory UI 证据爬取（capture/）

对标实例 `:8082`（7.161.20，addons 全开）的 UI 全量证据爬取套件。**串行执行**（单浏览器单页流，
无任何并行套件）。产物供 Phase B（spec 生成）与 Phase D（验收比对）消费。

## 运行（需 Bash 会话：conductor 主会话或 dev-frontend）

```bash
cd /Users/lzw/dev-center
ARTIFACTORY_USER=admin ARTIFACTORY_PASSWORD='<凭据>' node tools/penpot-sync/capture/capture.mjs
```

- 凭据**只经 env 注入**，脚本从 `process.env` 读取；任何落盘文件（含 run-log/tokens/截图元数据）
  均不含凭据——grep 自查已过（`JFrog@` 零命中）。
- Playwright 复用 `web/node_modules`（1.62.x，createRequire 解析，cwd 无关）。Chromium 二进制缺时：
  `cd web && npx playwright install chromium`。
- 可选 env：`ARTIFACTORY_URL`（默认 http://localhost:8082）、`OUT_DIR`、
  `PHASES=preauth,login,nav,setup,screens,dialogs,tokens,states,catalog,cleanup`（子集重跑）、
  `MAX_SHOTS`（截图预算，默认 140）。

## 关键设计约束（为什么这么写）

1. **登录走 UI 表单**：`/ui/api/v1/*` 拒 basic auth（api-map.yaml E4 实证），必须表单建 session。
   选择器锚：`input[name=username]` / `input#password-input`（console-ui.md §2，高置信）+ 兜底链。
   登录形态异常（SSO 跳转等）→ 落 `dom-snapshots/login-form.html` + 截图后退出，不硬闯。
2. **REST 只用于兜底与清理**：`/artifactory/api/*` 接受 basic auth（api-map 证据）；UI 触达失败时
   才回退 REST 造数据，并登记 gap（弹窗形态未取证不装作已取证）。
3. **测试数据 `audit-probe-` 前缀**（3 仓 + 1 用户 + 1 组 + 1 权限目标），cleanup 阶段 REST 删净并
   复核 `audit-probe-*` 零残留；用户实例是参照实例，写腿最小化。
4. **截图预算**：同形态屏指纹（标题+Tab+列头+heading）去重合并，目标 60-100 张覆盖全差异面。
5. **登录错误态只打一发**：错误口令尝试 1 次（防锁账）。

## 阶段（严格串行）

| 阶段 | 产物 |
|---|---|
| preauth | `screenshots/screens/login.png`、`screenshots/states/login-error*.png`、登录页 token 采样 |
| login | UI 表单建 session；onboarding 重定向则顺手取证 |
| nav | `nav-tree.json`（顶栏+侧栏完整层级：图标/文案/折叠态全展开后序列化）、`dom-snapshots/{sidebar,topbar}.html` |
| setup | `audit-probe-*` 数据创建（仓库向导全步截图=弹窗取证；用户/组表单空提交=校验错误态取证；Deploy 对话框=成功态取证） |
| screens | 清单 67 屏逐屏：全页截图 + DOM 概要（标题/breadcrumb/按钮/列头/Tab/选中态） |
| dialogs | Set Me Up（token 不生成）/ 权限两步选择器+矩阵 / Token 表单（不提交）/ 用户菜单 / 快速搜索 / 树右键菜单 / 删除确认（真删 audit-probe-del） |
| tokens | tokens.json：色板(hex)/字族字阶/间距/圆角/阴影/边框，多上下文采样 |
| states | loading（路由节流）/ empty（空仓树+空搜索）/ 404 / hover / permission-denied（probe 用户第二上下文） |
| catalog | screens-catalog.json（与 screens.yaml 对账：新增屏/缺失 seed 标注）+ states-gaps.md |
| cleanup | REST 删净 audit-probe-* 并复核 |

## 产物目录

`docs/reverse/frontend/parity-capture/`（已加 .gitignore 防百张 PNG 误提交；本阶段产物不 commit）：

```
parity-capture/
├── nav-tree.json            # 导航全层级
├── screens-catalog.json     # 屏目录 + screens.yaml 对账
├── tokens.json              # 设计 token（多上下文）
├── states-gaps.md           # 未触达态清单（下一轮走查票输入）
├── run-log.json             # 逐阶段执行账（含失败项）
├── screenshots/{screens,dialogs,states}/
└── dom-snapshots/           # sidebar/topbar/login-form HTML + DOM 概要 JSON
```

## 种子与活体发现

爬取清单 = `crawl-manifest.json`（手工派生自 `docs/reverse/frontend/routes.yaml` E2 提取集，每条
path 有出处；范围外屏 projects/pipelines/xray 显式 skip 并留理由）。**活体发现为准**：运行后以
screens-catalog.json 对账（新增屏=route 无 seed 映射）。

## 已知限制（Phase A 收口前须知）

- 树深链 `/repos/tree/General/<repo>` 格式未实证（best-effort + gap 登记）。
- Pro 屏（retention/lifecycle/release-bundles）在 addons 全开实例应可渲染，但内层流程未驱动。
- 慢网 loading 态靠路由节流模拟，只覆盖列表屏；树虚拟滚动渐进渲染需 >2k 节点，未造。

## Phase B 走查（dialogs-walkthrough.mjs，LOOP 013）

清偿 states-gaps.md 台账的交互走查脚本（与 capture.mjs 同一套纪律：单浏览器串行、
settle ping 健康门、audit-probe- 前缀数据 + cleanup 零残留复核）：

```bash
ARTIFACTORY_USER=admin ARTIFACTORY_PASSWORD='<凭据>' \
  [WALK_GAPS=user-menu,quick-search,...] node tools/penpot-sync/capture/dialogs-walkthrough.mjs
```

- `WALK_GAPS` 子集重跑；结果台账**合并历史**（`walkthrough-results.json` + 重写 `states-gaps.md`，
  带 status 列：cleared/partial/open）。
- 选择器证据链：Phase A domSummary（顶栏按钮 "Platform/Administration/A"——A=头像）、
  `dom-snapshots/walkthrough-*.json`（AG-Grid 行 + "Row actions" kebab、el-drawer 表单
  `#release-bundle-name/version/signing-key`、el-form-item 标签定位表单域）。
- capture.mjs 补充 env：`SCREENS_SLUGS=逗号清单`（精确补拍，不覆盖已好 PNG）、
  `SKIP_GAPS=1`（补拍跑不重写 gap 台账）。
- 客户端注入类取证（api-failure 用 Playwright route abort）**不触服务端**——遵守
  「不向参照实例注入 5xx」红线。

