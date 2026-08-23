# M8 交互断言基座（web/e2e/m8/）

T-232 交付。M8 全部 UI 票（T-235~T-244）与 QA U 序列的 Playwright spec 落这里。
**断言口径 = ADR-0029 决策 3（交互断言制）**，本文是该决策在 spec 写法上的落地细则；
锚源 = console-ux §10（242 锚清单，v1.2 全量核对基准）。

> 勘误承接：spec 目录取现役 `web/e2e/m8/`（testDir `./e2e` 的子目录，无需改
> playwright.config）；PRD §4 字面的 `web/tests/m8/` 与现役冲突，不作数。

---

## 1. 运行口径（真栈 + 新鲜二进制纪律）

```sh
# 仓库根：console 与二进制（probe 同款新鲜度守卫——任何 Go 构建输入新于
# bin/binflow-server 即先 make build；scripts/m7-resume-probe.sh 有同构实现）
make console && make build

# 起被测实例（本地 filestore，scratch 数据目录）
./bin/binflow-server serve &        # 默认 127.0.0.1:8080；ADMIN_PW 缺省 password

# 种子（角色用户 + 仓库 + ≥10,000 节点目录树；幂等，可重复跑）
cd web && node scripts/seed-m8.mjs

# 跑 M8 面（既有 18 spec 与本目录同 testDir，全量 = npx playwright test）
npx playwright test e2e/m8
```

- `BASE`（默认 `http://127.0.0.1:8080`）指向被测实例；`ADMIN_USER`/`ADMIN_PW`
  沿既有探针约定。
- **二进制新鲜度纪律**（M7 T-216 陷阱的教训）：跑 e2e 前确认
  `find cmd internal go.mod go.sum tools.go -type f -newer bin/binflow-server`
  为空；非空即 `make build`。陈旧二进制会让 spec 替旧代码作证。
- spec 不 autostart 服务器（沿 T-89 起的 harness 约定：配置面是稳定缝）。

## 2. 断言口径（ADR-0029 决策 3 逐条落地）

### 2.1 锚 = data-testid，不随路由改名

```ts
// 对：锚选取（console-ux §10 命名规则冻结的 242 锚）
await expect(page.locator('[data-testid="repos-page"]')).toBeVisible()
await expect(page.locator('[data-testid="repos-row-docker-local"]')).toBeVisible()

// 错：文案/结构/CSS 类当锚（M8 重排的首批牺牲品）
await expect(page.getByText('仓库管理')).toBeVisible()          // 文案会中文化/微调
await expect(page.locator('.ag-grid-row')).toHaveCount(5)       // 组件实现细节
```

- 锚**不随路由改名**（ADR-0029 转正勘误 + console-m8 §1.4）：T-235 把
  `/repositories` 挪到 `/admin/repositories/local` 后，`repos-page`、`repos-row-<key>`
  等锚原样保留——**路径断言随路由表改，锚断言不动**。URL 断言只在外部前缀
  （`/binflow/ui/`）与重定向兼容窗口（旧路由 20 条）两处出现。
- 动态段：实体用原值（`repos-row-<repoKey>`）；**类段防碰撞**——主体有多类时
  名段前加 `{user|group}`（`perm-matrix-cell-user-alice-read`）。
- 锚唯一性按**视图**计：同名锚（如 `repos-empty` 同时在仪表盘与仓库页）必须
  scope 到页面根内再断言。
- 四态缺省锚：`skeleton` / `error-card` + `error-retry` / `empty-state` / `toast`。
  **403 收敛不产生新锚**：L3 隐藏 = 反断言（`toHaveCount(0)`），L2 复用
  `empty-state` 缺省锚。

### 2.2 操作流对照（核心体例：步骤 N → 目标锚 → 期望状态）

每条 M8 AC 先写成操作流表（进 spec 注释或 PRD 对照样），再逐行翻译为断言。
示例——「readonly_admin 打开用户编辑器」：

| 步骤 | 动作 | 目标锚 | 期望状态 |
|---|---|---|---|
| 1 | loginAs | `login-submit` | `app-nav` 可见 |
| 2 | 直链深页 | — | `user-form-page` 可见 |
| 3 | 看角色下拉 | `user-form-role` | `disabled` 且值 = `readonly_admin` |
| 4 | 找保存钮 | `user-form-submit` | `disabled` |
| 5 | 找只读注记 | `user-form-readonly-note` | 可见 |

```ts
await loginAs(page, 'readonly_admin')
await page.goto('/binflow/ui/admin/security/users/m8-e2e-readonly')
await expect(page.locator('[data-testid="user-form-role"]')).toHaveValue('readonly_admin')
await expect(page.locator('[data-testid="user-form-role"]')).toBeDisabled()
await expect(page.locator('[data-testid="user-form-submit"]')).toBeDisabled()
await expect(page.locator('[data-testid="user-form-readonly-note"]')).toBeVisible()
```

「期望状态」只用：存在/可见/隐藏（反断言）、`disabled`/`enabled`、`checked`、
聚焦、文本/值、URL、计数。这些都是**交互与信息可达性**，不是外观。

### 2.3 禁像素 diff；样式断言仅限自有 token 存在性

```ts
// 禁（ADR-0029 决策 3 原文：截图基线/visual-regression 不作 M8 验收门）
await expect(page.locator('.card')).toHaveScreenshot('card.png')   // 禁止
await expect(page.locator('.card')).toMatchSnapshot()               // 禁止
await expect(page.locator('.card')).toHaveCSS('color', 'rgb(18,22,29)') // 禁止（把皮肤钉成克隆证据）

// 允许的唯一形态：computed style 对自有 token 的「存在性」校验——
// 只证明 token 已应用（解析值非缺省回退），永不比对具体色值。
const applied = await page.evaluate(() => {
  const cs = getComputedStyle(document.documentElement)
  return cs.getPropertyValue('--bf-sidebar').trim() !== ''
})
expect(applied).toBe(true)   // 暗色/亮色主题切换后 token 仍在位
```

理由（ADR-0029）：视觉本就不是对齐口径（自有皮肤），像素断言会把皮肤钉成
克隆证据，且截图基线跨平台脆弱。布局/交互形态用 2.2 的锚断言表达。

### 2.4 键盘流（page.keyboard）

```ts
// 登录全键盘（smoke.spec.ts 的实跑体例）：focus() 定位 + 键激活。
// focus() 是 Tab 遍历终态的确定性替身；一切「激活」必须是按键。
await expect(page.locator('[data-testid="login-username"]')).toBeFocused() // autofocus 事实
await page.keyboard.type(name)
await page.keyboard.press('Tab')
await expect(page.locator('[data-testid="login-password"]')).toBeFocused()
await page.keyboard.type(password)
await page.keyboard.press('Enter')                    // 表单提交
```

树/对话框键盘语义（console-ux §8 + console-m8 §3.4）：`↑↓` 移动、`→/←`
展开/收起、`Enter` 激活、`Esc` 关闭、`Shift+F10` 右键菜单、焦点陷阱内
`Tab` 循环。断言用 `toBeFocused()` + `aria-expanded` 属性，不用坐标。

### 2.5 四态断言

```ts
// loading：骨架锚出现（瞬态，poll 容忍）
await expect(page.locator('[data-testid="skeleton"]')).toHaveCount(1)
// empty：缺省锚或实例锚
await expect(page.locator('[data-testid="repos-empty"]')).toBeVisible()
// error：错误卡 + 重试锚；403 走 L2（empty-state）/L3（反断言）而非错误卡
await expect(page.locator('[data-testid="error-card"]')).toBeVisible()
await expect(page.locator('[data-testid="error-retry"]')).toBeEnabled()
// success：toast 锚 + 对象 key
await expect(page.locator('[data-testid="toast"]')).toContainText(key)
```

### 2.6 对账断言（UI 说的话让 API 复核）

```ts
const got = await sessionApi(page, 'GET', `/api/security/users/${user}`)
expect(got.status).toBe(200)
expect((got.json as { adminRole: string }).adminRole).toBe('readonly_admin')
```

`sessionApi`（support/seed.ts）= 同源 fetch 携带页面 session——V12~V14 起
的控制台对账惯例。UI 断言 + API 回读双证是 M7 沉淀的口径，M8 沿用。

## 3. 助手清单（全部已自证，见 helpers.spec.ts）

| 助手 | 入口 | 自证腿 |
|---|---|---|
| `loginAs(page, role)` | `support/roles.ts` | 三角色各一：壳渲染 + 会话徽章 |
| `provisionRoles()` | `support/roles.ts` | loginAs 内含；幂等 PUT |
| `seedRepos` / `seedTree` / `seedAll` / `countTreeNodes` | `web/scripts/seed-m8.mjs`（CLI 同源）；spec 侧经 `support/seed.ts` | ≥10,000 节点深列举验证 + 双点抽查 |
| `sessionApi(page, …)` | `support/seed.ts` | （随对账腿） |
| `expectCopied` / `grantClipboard` | `support/clipboard.ts` | 仓库行 CopyButton → 剪贴板全值 |
| `expectA11yClean` / `scanA11y` | `support/a11y.ts`（@axe-core/playwright，devDependency） | 登录页 serious/critical 扫描 |
| `measureFirstInteractive` / `measureTreeExpand` / `recordTiming` | `support/timing.ts` | 登录首屏 + 树层展开（record-only，不作门） |

种子树形态（性能腿数据，确定性计划）：`perf/` 根 + 宽层 50 目录 × 20 文件
（一层展开的真实负载）+ 120 条深链 × 75 层（借祖先物化廉价堆节点数）=
**10,291 节点 / 1,120 次 PUT**；`node scripts/seed-m8.mjs --plan-only` 看计划。

## 4. 给后续票的规矩

1. 新页面组**先补 console-ux §10 锚清单再落码**（console-m8 §1.4 流程）；
   spec 只断言清单内锚。
2. 旧路由兼容窗口的重定向腿写在 T-235 的映射表驱动 spec 里，其余 spec
   一律走新路由 + 锚。
3. 任何「像素/截图」提案 = 违 ADR-0029，直接拒。
4. 契约冻结（决策 4）：spec 不得为断言方便要求新端点——表达不了的走
   PM 出 FR 的独立票。
