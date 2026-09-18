# Penpot Dashboard UI Kit 源规格与 BinFlow 映射

## 1. 事实源

- 官方源文件：[Dashboard UI Kit - Dashboard, Free Admin Dashboard (Community)](https://penpot.github.io/penpot-files/Dashboard%20UI%20Kit%20-%20Dashboard%2C%20Free%20Admin%20Dashboard%20(Community).penpot)
- Penpot 导出 manifest：\`penpot-library/1.2.0-RC1\` / \`penpot-exporter-figma-plugin/0.19.3\`
- 源 file id：\`125029ac-fafc-80d1-8007-86f30de360d1\`
- 团队内导入 file id：\`c514c1fb-1cda-8125-8008-a6bdf55481dd\`
- 团队 id：\`c514c1fb-1cda-8125-8008-a57ff957bc19\`
- 机器可审计数据：\`docs/design/penpot-dashboard-ui-kit-audit.json\`
- 可复现分析器：\`tools/penpot-sync/analyze-dashboard-kit.py\`

> 2026-09-18 Penpot MCP 已成功握手并读取 \`high_level_overview\` / \`tools/list\` / \`penpot_api_info\`。插件当前连接 \`binflow-v1\`，模板源则通过官方 \`.penpot\` 导出包离线审计，避免把 \`binflow-v1\` 的派生风格误当模板。

## 2. 源结构

\`\`\`text
objects                 5,980
top-level boards           12
component definitions      46
component instances     2,419
font family           Inter (1,792 spans) / SF Pro (18)
native token sets            1
native token entries        0
\`\`\`

关键 board：

| Board | 尺寸 | 说明 |
|---|---:|---|
| Dashboard Overview | 1440×1024 | 白底紧凑桌面版；左 Sidebar 212、右 Right Bar 280、Header 68 |
| Dashboard Overview | 1440×1024 | #333 暗色紧凑桌面版 |
| Dashboard Overview | 1440×1724 | #f5f5f6 扩展桌面版；Sidebar 220、Header 40 |
| Dashboard Overview | 1440×1724 | #333 暗色扩展桌面版 |
| Dashboard App | 393×1767 | 移动端亮/暗两版 |
| SnowUI / Design system | 854×999 等 | 源设计系统与组件说明 |

## 3. 直接观测 token

Penpot 源文件启用了 \`design-tokens/v1\`，但唯一 token set \`Collection 1/Mode 1\` **没有任何 token entry**；所有视觉值直接落在 shape 上。以下为从源 shape 统计推导的 token，不是 Penpot 原生命名。

### Color

| 语义 | 源观测 | BinFlow 映射决策 |
|---|---|---|
| 亮色 canvas | 扩展桌面板 #f5f5f6；紧凑板 #ffffff | \`--bf-bg\` 采用 #f5f5f6，保留 #ffffff 为 card surface |
| 暗色 canvas | #333333 | 暗色 \`--bf-bg\` 采用 #333333 |
| 亮色 card / panel | #ffffff、#f9f9fa | \`--bf-surface-1=#ffffff\`；\`--bf-surface-2=#f9f9fa\` |
| 暗色 card | #ffffff @ 4% | \`#ffffff @ 4%\`（#333 合成约 #3b3b3b） |
| 主文字 | #1c1c1c / #ffffff | \`--bf-text=#1c1c1c\` / 暗色 #ffffff |
| 次文字 | #3c3c43 @ 60% / #ebebf5 @ 60% | 语义映射 \`--bf-text-2\`，需按宿主实测保留 AA |
| 边框 | #000000 @ 10% / #ffffff @ 15% | 亮/暗 \`--bf-border\` 直接映射 |
| 图表/品牌蓝 | #007aff / #0a84ff | 图形与焦点可用源色；承载 13px 文字的交互实底需可访问替代 #0064cc / #0a84ff+深色前景 |
| 状态红 | #ff3b30 / #ff453a | 图表/图标源色；文字/按钮保留可访问深色变体 |
| 状态绿 | #34c759 / #30d158 | 图表/图标源色；文字/按钮保留可访问深色变体 |
| 紫 | #af52de / #bf5af2 | 图表/图标/边框源色 |
| 青 | #00c7be / #63e6e2 | 图表/图标源色 |
| 黄 | #ffcc00 / #ffd60a | 图表/图标源色，禁作白字正文 |

### Typography

源主导字体为 Inter。桌面高保真中高频档：

| 字号 / 字重 | 行高 | BinFlow 映射 |
|---|---:|---|
| 12 / 400 | 1.333 | \`--bf-fs-xs\` 辅助信息 |
| 14 / 400 | 1.429 | \`--bf-fs-base\` 正文/表单 |
| 17.5 / 600 | 1.5 | 接近 \`--bf-fs-md\`，BinFlow 密度保留 16px |
| 24 / 600 | 1.5 或 1.333 | \`--bf-fs-2xl\` 页面/仪表盘强调 |

BinFlow 保留 13px \`--bf-fs-sm\` 作为管理表格密度档；这是产品密度差异，不复制模板的松散表格。

### Radius / spacing / shadow

| 维度 | 源高频观测 | 映射决策 |
|---|---|---|
| Radius | 8、12、16、20、24 | 桌面管理面收敛为 8 / 12 / 16；20/24 只用于 dashboard hero/card，不给密集表格 |
| Gap | 2、4、8、12、16、28 | 现有 4/8/12/16/24 阶保留，新增 28 作为 dashboard 区块档 |
| Padding | 4、8、12×16、16、24 | 控件 4×8/4×12；卡片 16 或 24；不复制移动端 40/160 |
| Shadow | #000 10% y=.5 blur=.5；#000 10% y=2 blur=4 | 替代现有过重阴影 1/2 档；modal 继续保留更强层级 |
| Border | #000 10% / #fff 15%，0.5px | 桌面 Web 使用 1px 等值，避免 0.5px 在 Windows 缩放下的不稳定 |

## 4. 组件映射

| Penpot 源组件 | BinFlow / shadcn 承载 |
|---|---|
| Button | \`ui/Button\` |
| Card / Block | \`ui/Card\` + domain card |
| Sidebar | \`AppShell/Sidebar\` |
| Header | \`AppShell/Topbar\` |
| Right Bar | Explorer inspector / detail drawer |
| Search | \`ui/Input\` + command palette |
| Breadcrumb | \`ui/Breadcrumb\` |
| Icon & Text | shadcn composition + lucide icon |
| ChartMotion / DonutChart / Horizontal | 后续图表域组合，色彩从源 palette 派生 |
| Dashboard Overview | Dashboard / monitoring pages |
| Dashboard App | 不直接复刻移动端；BinFlow 当前范围是桌面管理台，保留响应式降级 |

## 5. 落地状态（2026-09-18）

- `web/src/design-system/tokens/color.css`：亮/暗壳、surface、border、文字、交互蓝、chart-only 色已按本映射更新。
- `web/src/design-system/tokens/radius.css`：4/8/8/12/16/20/24 阶已落地。
- `web/src/design-system/tokens/spacing.css`：新增 28px dashboard 区块档。
- `web/src/design-system/tokens/shadow.css`：shadow 1/2 降至模板贴面层级，modal shadow 保留焦点隔离。
- `web/src/design-system/tailwind.css`：chart 色与 xl/2xl/3xl radius、28px dashboard spacing 已进 `@theme` 桥。
- 登录文档链接显式 `text-info`；暗色交互蓝 #6cb2ff、危险文字 #ff8a84 为 AA 修正。
- Badge soft 底由透明 color-mix 改为与 `--bf-bg` 合成，避免 axe 无法稳定合成透明背景。

## 6. 有意差异

1. **壳尺寸**：模板 212/220 sidebar、40/68 header；BinFlow 为 240/64，以 Artifactory 管理台信息密度和导航长度为准。
2. **表格字号**：模板是 dashboard 展示面；BinFlow 有大量治理表格，保留 13px 密度档。
3. **可访问色彩**：模板蓝/状态亮色不做无条件的正文按钮底色；交互文字仍须 AA。
4. **移动端**：模板 393×1767 移动板只作为窄窗口降级参考，不作为 P0 复刻范围。
