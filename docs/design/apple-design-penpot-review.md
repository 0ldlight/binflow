# Apple Design 审查 — Penpot Dashboard UI Kit 对齐（2026-09-18）

## 审查对象

- 设计事实源：\`docs/design/penpot-dashboard-ui-kit-audit.json\` 与 \`docs/design/penpot-dashboard-ui-kit.md\`
- 代表交互面：BinFlow styleguide（Button/Input/Select/Checkbox/Radio/Switch/Tabs/Dialog/Drawer/Toast）、AppShell、仓库表单/列表、Set Me Up、Deploy、Webhooks
- 已读取 skill：\`/Users/lzw/.agents/skills/apple-design/SKILL.md\`
- 现有验证输入：typecheck / lint / build / a11y sweep / keyboard focus trap / styleguide / 核心业务 E2E

## 结论

**Conditional Pass —— 允许进入 Penpot token 对齐实现；不得直接照抄模板亮色作交互文字色。**

阻断项已给出明确修正；实现后需复跑 a11y、双主题、键盘与代表性业务 E2E，并翻新截图基线。此报告不宣称最终实现复审通过。

## 审查发现

### A1 / P0：模板主蓝不能直接用于普通按钮文字

- 源观测：#007aff（亮）、#0a84ff（暗）
- 风险：#007aff 对 #ffffff 对比度 4.02:1，低于 13/14px 普通文字 AA 4.5:1。
- Apple Design 原则：Responsibility / feedback；可读性优先于复刻。
- 修正：
  - 图表、图标、大号标题可使用 #007aff / #0a84ff；焦点环可用可访问亮蓝；
  - 亮色实底交互使用同相可访问蓝 #0064cc（5.68:1）；危险/成功文字同样走 AA 变体；
  - 暗色实底/链接使用 #6cb2ff + 深色前景/浅色文字（≥5:1）；#0a84ff 仅图形源色。
- 复审：映射文档已强制该分支，后续 token patch 必须照此执行。

### A2 / P0：Penpot 源没有可用命名 token

- 源观测：唯一 token set \`Collection 1/Mode 1\` 为空；所有颜色/字号/圆角直接落在 shape。
- 风险：如果声称“已提取 Penpot tokens”，会制造虚假来源链。
- Apple Design 原则：Craft / no assumption。
- 修正：审计器从 5,980 个 shape 统计直接值，明确标注为 derived tokens；映射文档区分 source observed 与 BinFlow semantic mapping。

### A3 / P1：大圆角不能全站照抄

- 源观测：8、12、16、20、24 均高频；dashboard 卡片多为 20/24。
- 风险：治理表格、表单、密集列表使用 20/24 会扩大控件占位并削弱扫描性。
- Apple Design 原则：Simplicity is not minimalism；适配上下文。
- 修正：控件 8、卡片 12/16；仅 Dashboard hero/图表卡使用 20；24 保留给空态/展示型容器，不进入表格与表单。

### A4 / P1：亮色壳应改为浅色，不应沿用深色侧栏

- 源观测：亮色桌面为白/浅灰壳，边框 #000 10%；暗色为 #333 + #fff 15%。
- 风险：现 BinFlow 亮色侧栏深底会偏离 Penpot 模板气质，也会让双主题信息层级不一致。
- 修正：亮色 sidebar 改白底 + #000 10% 分隔；暗色 sidebar 改 #333 底 + #fff 15% 分隔。导航 active 使用低强度软底与 2px 指示，不做强实底。

### A5 / P1：0.5px 边框不能直接用于 Web 管理台

- 源观测：Sidebar/Header 边框 0.5px。
- 风险：非 Retina、Windows DPI 与浏览器缩放下会出现模糊或消失。
- 修正：视觉目标映射为 1px #0000001a / #ffffff26；不保留 0.5px 字面值。

### A6 / P1：阴影过重，应降至模板的贴面层级

- 源观测：#000 10% y=0.5 blur=0.5 与 y=2 blur=4。
- 现状：BinFlow shadow 1/2/3 偏重。
- Apple Design 原则：Depth 应服务层级，不制造噪声。
- 修正：shadow-flat/overlay 改为源观测；modal 保留更强 shadow 以维持焦点隔离。

### A7 / P1：图表色与语义状态色分层

- 源观测：#ff3b30、#34c759、#af52de、#00c7be、#ffcc00 及其暗色变体。
- 风险：亮饱和状态色直接承载白字会失败；黄色尤其不可作正文底。
- 修正：新增 chart-only 槽位，语义操作/徽章继续使用可访问变体。文档明确“图形源色 ≠ 交互文字色”。

### A8 / P2：字体与密度保留 BinFlow 管理台需求

- 源观测：Inter 12/14/17.5/24，行高约 1.33/1.43/1.5。
- 现状：代码已使用 Inter，并有 13px 表格密度档。
- 修正：保留 Inter；12/14/24 直接映射；17.5 收敛到 16，13 保留为表格密度。此为产品上下文差异，不是视觉偏差。

### A9 / P2：动效只允许低幅度、可降级

- 源文件包含 ChartMotion 组件，但静态导出不含运动参数。
- 修正：不猜测弹簧参数。已有 Dialog/Drawer/Toast token 动效保持 120–240ms fade/pop/slide；图表进入只做 opacity/小幅 translate，\`prefers-reduced-motion\` 全部 1ms。

## 2026-09-19 Monochrome 复审

用户终裁整体配色为黑白。修正如下：

- Penpot 结构、密度、圆角、间距、阴影继续生效。
- 交互色改为亮色黑 / 暗色白，全部满足 AA。
- 状态、包型、图表全部黑白灰；语义必须同时由文案、图标、形状或位置表达。
- `assert-design.mjs` 拒绝任何非灰阶 token literal。
- 本节取代 2026-09-18 复审中的彩色交互映射；源模板色彩仅保留在审计证据中。

## 实现复审（2026-09-18）

Token patch 已按 A1–A7 落地：

- 亮色 shell：#f5f5f6 canvas / #ffffff sidebar / #0000001a border。
- 暗色 shell：#333333 canvas / #ffffff@4% surface / #ffffff26 border。
- 交互蓝：亮 #0064cc；暗 #6cb2ff。#007aff/#0a84ff 保留为 chart-only 源色。
- 暗色危险文字修正为 #ff8a84。
- Radius/spacing/shadow/chart tokens 均进入 token 层与 Tailwind bridge。
- 发现并修复三类复审阻断：登录文档链接继承弱化色、暗色链接/危险文字对比不足、透明 badge 背景导致 axe 不稳定。

复验结果：

- `.m8/a11y-sweep.spec.ts` + `.shell-layout.spec.ts`：6/6 passed（全控制台双主题 serious/critical = 0）。
- `.styleguide.spec.ts`：12 passed / 1 environment skip；控件、tabs、Dialog/Drawer/Toast、双主题、motion、reduced-motion 全通过。
- 核心业务回归：repositories、permissions、Set Me Up / Deploy、Webhooks 共 27 passed / 1 conditional skip。
- `npm run typecheck` / `npm run lint` / `npm run build`：通过（lint 仅存量 hooks warnings）。

**复审结论：Penpot token / 代表性交互层通过。** 仍不宣称完整产品最终验收；截图基线需在新的视觉体系下统一翻新后再跑视觉差分。

## 实现门槛

1. Token patch 必须按本报告 A1/A4/A5/A6/A7 执行。
2. 消费层禁止直接读源色；只能走 \`--bf-*\` 语义 token。
3. 双主题 axe serious/critical = 0。
4. 键盘焦点、Dialog/Select z-index、Esc/焦点恢复回归全部通过。
5. 截图基线统一翻新，不允许把旧 BinFlow 视觉作为 Penpot 对齐后的 golden。
