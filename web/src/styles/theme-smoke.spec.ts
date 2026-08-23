import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

// T-234 双主题皮肤冒烟（临时位置：与 styles 同级；T-232 的 web/e2e/m8/
// 基座合入后迁移——见同目录 playwright.styles.config.ts 头注）。
//
// 断言口径（ADR-0029）：交互断言制，禁像素 diff / 截图基线；样式面只做
// 「根节点自有 token 的 computed 应用存在性」校验（不比对具体色值）。
// axe 扫描核心页 serious=0；主题切换按 data-theme 属性 + localStorage
// 持久 + token 取值随主题整体换入（纯换值）验证。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: import('@playwright/test').Page) {
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** 根节点自有 token 的 computed 存在性（ADR-0029 允许的样式断言形态） */
async function rootTokens(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    const cs = getComputedStyle(document.documentElement)
    const v = (name: string) => cs.getPropertyValue(name).trim()
    return {
      sidebar: v('--bf-sidebar'),
      bg: v('--bf-bg'),
      surface1: v('--bf-surface-1'),
      text: v('--bf-text'),
      accent: v('--bf-accent'),
      shadow1: v('--bf-shadow-1'),
      shadow2: v('--bf-shadow-2'),
      shadow3: v('--bf-shadow-3'),
    }
  })
}

/** axe：核心页 serious（含 critical）= 0 */
async function expectAxeClean(page: import('@playwright/test').Page, label: string) {
  const results = await new AxeBuilder({ page }).analyze()
  const serious = results.violations.filter(
    (v) => v.impact === 'serious' || v.impact === 'critical',
  )
  const detail = serious.map((v) => `${v.id}(${v.impact}): ${v.nodes.length} nodes`).join(', ')
  expect(serious, `${label} — ${detail}`).toEqual([])
}

test('默认亮色（Q2 终裁）；根节点自有 token 已应用且切换为纯换值 + 持久化', async ({
  page,
}) => {
  // 全新 context（无 localStorage）：系统偏好未表态 → 亮色默认
  await login(page)
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')

  const light = await rootTokens(page)
  for (const [k, v] of Object.entries(light)) expect(v, `token --bf-* ${k} 未应用`).not.toBe('')

  // 切暗色：data-theme 换 + token 组整体换入（同一语义名，取值不同）
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  const dark = await rootTokens(page)
  expect(dark.bg).not.toBe(light.bg)
  expect(dark.sidebar).not.toBe(light.sidebar)
  expect(dark.accent).not.toBe(light.accent)

  // localStorage 持久：reload 后仍为暗色
  await page.reload()
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

  // 切回亮色
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  expect((await rootTokens(page)).bg).toBe(light.bg)
})

test('亮色主题：核心页 axe serious=0', async ({ page }) => {
  await login(page)
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expectAxeClean(page, 'dashboard(light)')
  for (const [label, path] of [
    ['repositories', '/binflow/ui/repositories'],
    ['security-users', '/binflow/ui/security/users'],
    ['search', '/binflow/ui/search'],
  ] as const) {
    await page.goto(path)
    await expectAxeClean(page, `${label}(light)`)
  }
})

test('暗色主题：核心页 axe serious=0', async ({ page }) => {
  // 预置持久化偏好 → 暗色
  await page.addInitScript(() => {
    try {
      localStorage.setItem('binflow-console-theme', 'dark')
    } catch {
      // 隐私模式：回落默认亮色，本腿由 data-theme 断言守卫
    }
  })
  await login(page)
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expectAxeClean(page, 'dashboard(dark)')
  for (const [label, path] of [
    ['repositories', '/binflow/ui/repositories'],
    ['security-users', '/binflow/ui/security/users'],
  ] as const) {
    await page.goto(path)
    await expectAxeClean(page, `${label}(dark)`)
  }
})
