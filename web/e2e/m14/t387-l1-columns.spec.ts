import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, seedRepos } from '../m8/support/seed'

// T-387（M14 B8 FE，FR-125.2 L1）——列表工具栏列选器 + 刷新（parity
// console-artifactory-parity §5 L1：搜索框 / 过滤下拉 / **列选择器** /
// **刷新按钮**；计数在工具栏尾部或右下——两页既有 count 已满足）。载体 =
// RepositoriesPage / AuditPage（BOARD 票面指名两页；「列多者受益」）。
//
// 断言面（L07 + BOARD AC）：
//   ① 列选开合：触发钮 aria-haspopup/aria-expanded；Menu 开（项可见）/
//      Esc 关（回焦触发钮）；菜单项 = menuitemcheckbox + aria-checked，
//      勾选字形 aria-hidden 装饰（语义在 aria-checked）。
//   ② 列显隐（选/弃各腿）：弃一列 → 表头/行单元格同步 -1；勾回 → +1。
//   ③ 全选复位：reset 项清空隐藏集（localStorage 回 []）。
//   ④ 持久：per-page localStorage 键（binflow-console-cols-{repos,audit}），
//      reload 保持；两键互不染（audit 键不因 repos 操作出现）。
//   ⑤ 至少一列守卫：只剩一列可见时该项 aria-disabled 且点击被拒。
//   ⑥ 刷新取数：拦路闸门握住列表请求 → 进度环 + 禁用（取数中）→ 放行后
//      表回归、钮复原；刷新确实发出新请求（waitForRequest 对账）。
//   ⑦ 「无端点列不伪造」：仓库列选菜单列集 = 端点背书列闭集（T-404 起含
//      Replications 第 8 列——GET /v1/replications 端点背书；仍无「更新
//      时间」等无端点列）。
//   ⑧ 其余列表页不受影响：users 页零列选锚（新面只在两载体页）——T-414
//      起 users/groups/search 三页已推广列选器（e2e/m15/t414 谱系），本腿
//      口径改为「repos/audit 锚不越界到 users 页」。
//
// 锚源：console-ux §10.5 T-387 批（repos-columns-* / repos-refresh /
// audit-columns-* / audit-refresh——23 名全静态锚）；repos-* / audit-*
// 既有族零改名。选择器全部字面量（anchor-audit 对账口径：模板透传形
// 不可见，src 侧列项锚是 anchor: 属性字面量、无动态族可命中）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 延迟闸门：装上后匹配请求一律握住，返回的 open() 放行——刷新「取数中」
 *  态的确定性腿（真栈真请求，只握不伪造）。 */
async function installGate(page: import('@playwright/test').Page, pattern: string) {
  let release!: () => void
  const gate = new Promise<void>((r) => {
    release = r
  })
  await page.route(pattern, async (route) => {
    await gate
    await route.continue()
  })
  return () => release()
}

// ---- 1. 仓库页列选器全生命周期（①②③④⑤⑦） ----------------------------------

test('admin: repos column selector — open/close, hide/show, guard, reset, per-page persistence', async ({
  page,
}) => {
  const key = uniq('t387r')
  await seedRepos(m8Client(), [{ key }])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toBeVisible()

  // 默认全显（7 列闭集——T-404 增 Replications 列；T-443 收敛冗余「类型」列
  //〔Q9/B-3.9〕：三 Tab 子路由即类型，type 列 + 列选项退役）
  const th = page.locator('[data-testid="repos-table"] thead th')
  await expect(th).toHaveCount(7)

  // 开（①）：aria 语义 + 菜单项恰 7、全勾；type 项已退役（反断言）
  const trigger = page.locator('[data-testid="repos-columns"]')
  await expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
  await expect(trigger).toContainText('列 7/7')
  await trigger.click()
  await expect(trigger).toHaveAttribute('aria-expanded', 'true')
  const menu = page.locator('[data-testid="repos-columns-menu"]')
  await expect(menu).toBeVisible()
  await expect(menu.locator('[role="menuitemcheckbox"]')).toHaveCount(7)
  await expect(menu.locator('[role="menuitemcheckbox"][aria-checked="true"]')).toHaveCount(7)
  // 「类型」列项退役（Q9）：菜单内无该文案项（文案级负断言——锚已随列退役，
  // 不以退役锚反断言以免 broken 假阳性）
  await expect(menu.getByText(/^类型$/, { exact: true })).toHaveCount(0)

  // 弃「描述」（②）：表头 + 行单元格同步 -1；菜单保持开（列选不收菜单）
  await page.click('[data-testid="repos-columns-item-description"]')
  await expect(menu).toBeVisible()
  await expect(th).toHaveCount(6)
  await expect(th.filter({ hasText: '描述' })).toHaveCount(0)
  await expect(page.locator(`[data-testid="repos-row-${key}"] td`)).toHaveCount(6)
  await expect(trigger).toContainText('列 6/7')

  // 持久（④）：localStorage 落盘 + reload 保持；audit 键不被染
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-repos'))).toContain('description')
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-audit'))).toBeNull()
  await page.reload()
  await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toBeVisible()
  await expect(th).toHaveCount(6)

  // 勾回（②另一腿）
  await trigger.click()
  await page.click('[data-testid="repos-columns-item-description"]')
  await expect(page.locator('[data-testid="repos-columns-item-description"]')).toHaveAttribute('aria-checked', 'true')
  await expect(th).toHaveCount(7)

  // 至少一列守卫（⑤）：弃到只剩 key → key 项 aria-disabled 且点击被拒
  const hideAllButKey = [
    '[data-testid="repos-columns-item-package"]',
    '[data-testid="repos-columns-item-replications"]',
    '[data-testid="repos-columns-item-upstream"]',
    '[data-testid="repos-columns-item-usage"]',
    '[data-testid="repos-columns-item-description"]',
    '[data-testid="repos-columns-item-actions"]',
  ]
  for (const sel of hideAllButKey) await page.click(sel)
  await expect(page.locator('[data-testid="repos-columns-item-key"]')).toHaveAttribute('aria-disabled', 'true')
  // 点击被拒的实证：aria-disabled 态 Playwright 可点性检查会拒发事件，
  // dispatchEvent 直发 DOM click 验证 handler 层守卫（勾不动）
  await page.dispatchEvent('[data-testid="repos-columns-item-key"]', 'click')
  await expect(page.locator('[data-testid="repos-columns-item-key"]')).toHaveAttribute('aria-checked', 'true')
  await expect(th).toHaveCount(1)
  await expect(page.locator(`[data-testid="repos-row-${key}"] td`)).toHaveCount(1)

  // 全选复位（③）：表头回 7 + 存储回空数组
  await page.click('[data-testid="repos-columns-reset"]')
  await expect(th).toHaveCount(7)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-repos'))).toBe('[]')

  // 关（①另一腿）：Esc 关菜单 + 回焦触发钮
  await page.keyboard.press('Escape')
  await expect(menu).toBeHidden()
  await expect(trigger).toHaveAttribute('aria-expanded', 'false')
  await expect(trigger).toBeFocused()
})

// ---- 2. 仓库页刷新（⑥）-------------------------------------------------------

test('admin: repos refresh re-fetches the list (in-flight spinner + disabled, then restores)', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator('[data-testid="repos-table"]')).toBeVisible()

  // 闸门装上（页面首载已完成，此后匹配请求一律握住）
  const open = await installGate(page, '**/api/repositories*')
  const refresh = page.locator('[data-testid="repos-refresh"]')
  await expect(refresh).toBeEnabled()

  // 点刷新：新请求确实发出（对账）+ 取数中态（进度环 + 禁用）
  const refetched = page.waitForRequest((r) => r.url().includes('/api/repositories'))
  await refresh.click()
  await refetched
  await expect(refresh.locator('[role="progressbar"]')).toBeVisible()
  await expect(refresh).toBeDisabled()

  // 放行：表回归、钮复原
  open()
  await expect(page.locator('[data-testid="repos-table"]')).toBeVisible()
  await expect(refresh).toBeEnabled()
})

// ---- 3. 审计页：列选 + 持久 + 复位 + 刷新 -------------------------------------

test('admin: audit column selector + refresh (hide persists across reload; refresh re-fetches page 1)', async ({
  page,
}) => {
  await loginAs(page, 'admin') // 登录本身即写审计事件（页头注记）
  await page.goto('/binflow/ui/admin/governance/audit')
  await expect(page.locator('[data-testid="audit-table"]')).toBeVisible()
  const th = page.locator('[data-testid="audit-table"] thead th')
  await expect(th).toHaveCount(6)

  // 弃「来源」→ 表头 -1 + 持久（reload 保持）
  await page.click('[data-testid="audit-columns"]')
  // 列集 = 既有六列闭集、默认全勾（⑦ 审计侧对位）
  const auditItems = [
    '[data-testid="audit-columns-item-time"]',
    '[data-testid="audit-columns-item-actor"]',
    '[data-testid="audit-columns-item-action"]',
    '[data-testid="audit-columns-item-target"]',
    '[data-testid="audit-columns-item-source"]',
    '[data-testid="audit-columns-item-detail"]',
  ]
  for (const sel of auditItems) {
    await expect(page.locator(sel)).toHaveAttribute('aria-checked', 'true')
  }
  await page.click('[data-testid="audit-columns-item-source"]')
  await expect(th).toHaveCount(5)
  await expect(th.filter({ hasText: '来源' })).toHaveCount(0)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-audit'))).toContain('source')
  await page.reload()
  await expect(page.locator('[data-testid="audit-table"]')).toBeVisible()
  await expect(th).toHaveCount(5)

  // 复位（菜单开态点 reset；菜单不自动收）
  await page.click('[data-testid="audit-columns"]')
  await page.click('[data-testid="audit-columns-reset"]')
  await expect(th).toHaveCount(6)

  // 刷新（⑥）：Esc 收菜单（Modal 背板吃点击）→ 闸门 → 取数中 → 放行回归
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="audit-columns-menu"]')).toBeHidden()
  const open = await installGate(page, '**/api/v1/audit*')
  const refresh = page.locator('[data-testid="audit-refresh"]')
  await expect(refresh).toBeEnabled()
  const refetched = page.waitForRequest((r) => r.url().includes('/api/v1/audit'))
  await refresh.click()
  await refetched
  await expect(refresh.locator('[role="progressbar"]')).toBeVisible()
  await expect(refresh).toBeDisabled()
  open()
  await expect(page.locator('[data-testid="audit-table"]')).toBeVisible()
  await expect(refresh).toBeEnabled()
})

// ---- 4. 其余列表页不受影响（⑧） ----------------------------------------------
// T-414 更新：users/groups/search 三页列选器推广后，本腿口径改为「锚互不
// 越界」——users 页有自己的 users-columns（T-414 批），但 repos/audit 的
// 列选锚仍不得出现在场（per-page 偏好面隔离）。

test('admin: column selectors stay per-page — repos/audit anchors absent from users page', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="repos-columns"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="audit-columns"]')).toHaveCount(0)
  // users 页自己的列选器在场（T-414 推广面）
  await expect(page.locator('[data-testid="users-columns"]')).toBeVisible()
  // 既有列表面照常（本票只加不删：users 表头仍在场）
  await expect(page.locator('[data-testid="users-table"] thead th').first()).toBeVisible()
})

// ---- 5. axe 双主题（列选菜单开态 + 页面全量） ----------------------------------

test('axe: L1 toolbar surfaces scan clean in both themes (repos + audit, menus open)', async ({ page }, testInfo) => {
  test.setTimeout(300_000)
  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)

    await page.goto('/binflow/ui/admin/repositories/local')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="repos-table"]')).toBeVisible()
    await page.click('[data-testid="repos-columns"]')
    await expect(page.locator('[data-testid="repos-columns-menu"]')).toBeVisible()
    await expectA11yClean(page, testInfo) // 全页扫（portal 菜单同文档内）
    await page.keyboard.press('Escape')

    await page.goto('/binflow/ui/admin/governance/audit')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="audit-table"]')).toBeVisible()
    await page.click('[data-testid="audit-columns"]')
    await expect(page.locator('[data-testid="audit-columns-menu"]')).toBeVisible()
    await expectA11yClean(page, testInfo)
    await page.keyboard.press('Escape')
  }
})
