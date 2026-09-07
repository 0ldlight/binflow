import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'

// T-388（M14 B9 FE，FR-125.3/125.4——F2 空态插画槽 + N2 侧栏图标槽合并票）。
//
// F2（parity §6 F2，置信度中——未入 V1~V8 活体核验集，形态取规格口径）：
//   空态 = 插画位 → 说明 → 主行动。槽位规格：40×40px、currentColor 占位
//   线稿（候选 1「容器·双箭流」隐喻派生——真插画资产归后续设计票，槽位
//   契约不随换稿变）；装饰位 aria-hidden。挂载口径：主数据面空态（从未
//   有数据 / 过滤后空两种）挂，403 无权限卡不挂（错误语义不装饰）。
// N2/V5（parity §2 N2——V5 已核验 2026-08-31）：一级条目带 16px mono
//   图标（currentColor 随文字色），分组标签/子项无图标；Artifactory 档位。
//
// 确定性面说明：列表首空（还没有 X）依赖实例全局数据（并行套件会造数），
// 本 spec 取四处确定性空面承载断言——仓库过滤空 / 审计过滤空 / 搜索初始
// 空 / 搜索无匹配空；首空形态与其余页 wiring 为同一 EmptyState 落点
// （illustration prop 同构），由 shell.spec 图标腿 + axe 扫覆盖剩余面。
//
// 锚源：console-ux §10.5 T-388 批（empty-art / nav-icon）；既有 empty-state
// / repos-empty-filtered / audit-filter-* 零改名（search-input 随 T-449 页内
// 查询表单退役——无匹配腿改走顶栏驻留提交）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

// ---- 1. N2 侧栏一级条目图标（身份表 + mono currentColor） ----------------------

/** 16+2 一级条目 → 图标身份闭集（AppShell APP_NAV/ADMIN_NAV 接线表） */
const ADMIN_ICON: [string, string][] = [
  ['仓库', 'inventory_2'],
  ['用户', 'person'],
  ['组', 'group'],
  ['权限', 'lock'],
  ['Access Tokens', 'vpn_key'],
  ['认证配置', 'shield'],
  ['审计日志', 'history'],
  ['维护（GC）', 'delete_sweep'],
  ['配额', 'pie_chart'],
  ['复制', 'sync'],
  ['备份 / 恢复', 'backup'],
  ['回收站', 'delete'],
  ['Webhooks', 'bolt'],
  ['存储', 'storage'],
  ['系统信息', 'info'],
  ['License & Add-ons', 'card_membership'],
]

test('N2: first-level nav entries carry 16px mono icons (identity closed set, currentColor)', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  const nav = page.locator('[data-testid="app-nav"]')

  // 16/16 身份逐一钉死（接线表闭集——错一枚即红）
  for (const [label, icon] of ADMIN_ICON) {
    const entry = nav.locator(`a.nav-item:text-is("${label}")`)
    const el = entry.locator('[data-testid="nav-icon"]')
    await expect(el, `${label} icon`).toHaveCount(1)
    await expect(el).toHaveAttribute('data-icon', icon)
    await expect(el).toHaveAttribute('aria-hidden', 'true')
    // 槽位 16px
    expect(await el.evaluate((n) => getComputedStyle(n).width)).toBe('16px')
  }

  // mono currentColor：fill 的计算值 = 条目文字色（随文字色，含默认态）
  const probe = nav.locator('a.nav-item:text-is("仓库") [data-testid="nav-icon"]')
  const [fill, color] = await probe.evaluate((n) => {
    const cs = getComputedStyle(n)
    return [cs.fill, cs.color]
  })
  expect(fill, 'icon fill follows entry text color (currentColor)').toBe(color)

  // 档位反面：分组标签无图标、底部模式切换项维持既有字形（V5：仅一级条目）
  await expect(nav.locator('.nav-group-label [data-testid="nav-icon"]')).toHaveCount(0)
  await expect(nav.locator('[data-testid="nav-mode-switch"] [data-testid="nav-icon"]')).toHaveCount(0)

  // active 行图标在场（高亮不改图标档——结构断言，不断言视觉）
  await page.click('a.nav-item:text-is("审计日志")')
  await expect(page.locator('[data-testid="audit-page"]')).toBeVisible()
  await expect(
    page.locator('a.nav-item.active [data-testid="nav-icon"][data-icon="history"]'),
  ).toBeVisible()

  // 应用域 2 条目同档（dashboard / account_tree）；T-492（B-3.2）：应用模式
  // 落点随即自动选中首仓库——前缀断言（/artifacts 或 /artifacts/<repo>）
  await page.click('[data-testid="nav-mode-switch"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts(\/|$)/)
  await expect(nav.locator('a.nav-item [data-testid="nav-icon"]')).toHaveCount(2)
  await expect(nav.locator('[data-testid="nav-icon"][data-icon="dashboard"]')).toBeVisible()
  await expect(nav.locator('[data-testid="nav-icon"][data-icon="account_tree"]')).toBeVisible()
})

// ---- 2. F2 插画槽形态（尺寸/位置/CTA 关系 + 双空形态） ------------------------

test('F2: illustration slot — 40px, above message, CTA below (repos filtered-empty)', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  // 过滤空（确定性）：前端子串过滤无命中 → 过滤后空态
  await page.fill('[data-testid="repos-filter-key"]', 't388-no-such-repo')
  const empty = page.locator('[data-testid="repos-empty-filtered"]')
  await expect(empty).toBeVisible()

  const art = empty.locator('[data-testid="empty-art"]')
  await expect(art).toBeVisible()
  await expect(art).toHaveAttribute('aria-hidden', 'true') // 装饰位——语义在文案

  // 槽位规格：40×40（boundingBox 整数断言）
  const artBox = await art.boundingBox()
  const msgBox = await empty.locator('p').first().boundingBox()
  const ctaBox = await empty.locator('button').first().boundingBox()
  expect(artBox?.width).toBe(40)
  expect(artBox?.height).toBe(40)
  // 形态序：插画位 → 说明 → 主行动（parity F2）
  expect(artBox?.y, 'art above message').toBeLessThan(msgBox!.y)
  expect(ctaBox!.y, 'CTA below message').toBeGreaterThan(msgBox!.y)
})

test('F2: search page — initial empty and no-match empty both carry the slot', async ({ page }) => {
  await loginAs(page, 'admin')

  // 初始空（从未查询——确定性）
  await page.goto('/binflow/ui/search')
  const initial = page.locator('[data-testid="empty-state"]')
  await expect(initial).toBeVisible()
  await expect(initial.locator('[data-testid="empty-art"]')).toBeVisible()

  // 无匹配空（T-449：查询面 = 顶栏驻留——顶栏 Enter 提交，确定性无命中）
  await page.fill('[data-testid="topbar-search"]', 't388-no-such-artifact-qq')
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('没有匹配')
  await expect(page.locator('[data-testid="empty-state"] [data-testid="empty-art"]')).toBeVisible()
})

test('F2: audit filtered-empty carries the slot (third list page, same construct)', async ({ page }) => {
  await loginAs(page, 'admin') // 登录即写审计事件（页头注记）
  await page.goto('/binflow/ui/admin/governance/audit')
  await expect(page.locator('[data-testid="audit-table"], [data-testid="empty-state"]')).toBeVisible()
  // repo 精确过滤无命中（350ms 防抖）→ 过滤后空态（audit-empty-filtered 锚）
  await page.fill('[data-testid="audit-filter-repo"]', 't388-no-such-repo')
  await page.waitForTimeout(700)
  const empty = page.locator('[data-testid="audit-empty-filtered"]')
  await expect(empty).toBeVisible()
  await expect(empty).toContainText('当前过滤条件下无匹配事件')
  await expect(empty.locator('[data-testid="empty-art"]')).toBeVisible()
})

// ---- 3. F2 反断言：403 无权限卡不挂插画（错误语义不装饰） --------------------

test('F2 negative: 403 no-permission empty state stays plain (no illustration)', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/admin/repositories')
  const empty = page.locator('[data-testid="repos-page"], [data-testid="empty-state"]').first()
  await expect(empty).toBeVisible()
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('无权限查看仓库列表')
  await expect(page.locator('[data-testid="empty-state"] [data-testid="empty-art"]')).toHaveCount(0)
})

// ---- 4. axe 双主题（插画槽在页面 + 侧栏图标面） -------------------------------

test('axe: illustration + sidebar icon surfaces scan clean in both themes', async ({ page }, testInfo) => {
  test.setTimeout(300_000)
  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/repositories/local')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    // 过滤空 → 插画槽在场态全页扫（侧栏 16 图标同页覆盖）
    await page.fill('[data-testid="repos-filter-key"]', 't388-no-such-repo')
    await expect(page.locator('[data-testid="repos-empty-filtered"]')).toBeVisible()
    await expect(page.locator('[data-testid="empty-art"]')).toBeVisible()
    await expectA11yClean(page, testInfo)
  }
})
