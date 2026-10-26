---
description: Apple Design 审查门（intake ㉔ 强制门——批量前端实现前必过）
---

对 $ARGUMENTS 指定的界面/组件面执行一轮 Apple Design 审查（省略=本票涉及的全部新增/改动 UI 面）：

1. **载入知识**：先调 `apple-design` skill（用户级已装，源自 ~/.agents/skills/）——以 Fluid Interfaces 原则（response/motion/typography/restraint）为审查基准。
2. **对照本项目既裁口径**（docs/design/apple-design-penpot-review.md，2026-09-18/19 两轮复审结论优先于 skill 泛则）：
   - 交互文字色禁用源色 #007aff/#0a84ff（chart-only）；**2026-09-19 用户终裁黑白配色取代彩色映射**——状态/包型/图表全灰阶，语义须由文案/图标/形状/位置同时表达。
   - 圆角：控件 8 / 卡片 12-16 / hero 20；0.5px 边框一律 1px 等效；阴影按贴面层级；动效 120-240ms 且 prefers-reduced-motion 降级。
   - 消费层只读 `--bf-*` 语义 token，禁直读源色。
3. **机器门必跑**：`assert-design.mjs`（非灰阶 token literal 即拒）+ axe 双主题 serious/critical=0 + 键盘焦点/Esc/焦点恢复回归。
4. **产出**：审查报告（发现/严重度/修正项/复验证据）随票入 `reports/agents/T-<id>.md`；阻断项修复后**必须复审**——不得自称通过；截图基线按新视觉体系翻新，禁拿旧 golden 当通过证据。
