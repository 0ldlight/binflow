# shadcn/ui 原生组件硬门

## 裁定

生产 JSX 不得手写与 shadcn/ui 同类的交互/表格原语。按钮、输入、多行输入、选择、复选、单选、开关、表格、弹层等必须消费 `web/src/components/ui/*`；`components/layout/*` 只允许做密度、布局、语义与数据行为的组合封装，不得重新实现控件本体。

2026-09-18 起该要求进入构建与 lint 前置门：

```bash
node web/scripts/assert-shadcn.mjs
```

脚本扫描 `web/src/**/*.tsx` 中 `components/ui` 之外的 raw `button/input/select/textarea/table/thead/tbody/tfoot/tr/th/td`，并拒绝用 `Input` 伪装 checkbox/radio。当前结果：0 violation；生产页面已全部落在 `Button/Input/Textarea/Select/Checkbox/RadioGroup/Switch/Table` 原语上。

## 例外

- **AG Grid**：仅用于 Explorer/Search/Audit 等虚拟数据网格。shadcn/ui 没有 data-grid primitive；AG Grid 外的筛选、按钮、菜单、弹层与状态仍必须使用 shadcn/ui。
- **HTML 容器与语义节点**：`div/section/nav/ul/li/a/span` 不属于本硬门；可访问性与焦点行为仍由 Radix primitive 与 e2e/axe 承证。
- **文件上传 input**：必须使用 `ui/Input type=file`，且保留 aria-label；浏览器 File API 与拖拽行为由领域组件组合。

## 已落地迁移

- 新增 shadcn/ui Table primitive，并替换管理面 raw table family。
- `layout/fields` 改为组合 `ui/Input`、`ui/Textarea`、Radix `ui/Select`；旧 `NativeSelect` API 更名 `SelectField`，空值映射 Radix sentinel，不回退原生 select。
- 复选、单选、开关分别迁移到 `ui/Checkbox`、`ui/RadioGroup`、`ui/Switch`；穿梭框和矩阵单元格只做业务组合。
- 弹层中的 Select z-index 高于 Dialog overlay，保证对话框内可选择。
- Dialog 增加 `max-height + overflow-y-auto`，高向导在窄视口中可滚动、取消按钮可达。
- E2E 新增 `e2e/support/shadcn.ts`，用 Radix `role=option/data-value` 驱动选择器，不再依赖原生 `selectOption`。

## 验证

- `npm run typecheck`：通过。
- `npm run lint`：0 error（存量 React hooks warnings 46 条，非本迁移引入；assert-shadcn/assert-i18n 通过）。
- `npm run test:unit`：7/7 通过。
- `npm run build` / `make console`：通过，构建链先执行 assert-penpot 与 assert-shadcn。
- Playwright 生产二进制目标通过：styleguide、shell/a11y sweep、repositories、permissions、users/groups、tree、search pager、governance、repo policy、Set Me Up/Deploy、Webhooks、keyboard/focus trap 等关键族。
