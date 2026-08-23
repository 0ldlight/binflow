import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { loginAs } from './support/roles'

// T-234 双主题皮肤冒烟（T-232 遗留①：自 web/src/styles/ 临时位迁入本
// 基座，playwright.styles.config.ts 随迁删除——现役 playwright.config 的
// testDir ./e2e 已覆盖本目录）。登录/探测改用基座助手（loginAs）。
//
// 断言口径（ADR-0029 决策 3）：交互断言制，禁像素 diff / 截图基线；样式
// 面只做「根节点自有 token 的 computed 应用存在性」校验（不比对具体色值
// ——把皮肤钉成克隆证据的断言形态被明令禁止）。axe 扫描核心页
// serious=0；主题切换按 data-theme 属性 + localStorage 持久 + token 取值
// 随主题整体换入（纯换值）验证。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

/** 根节点自有 token 的 computed 存在性（ADR-0029 允许的样式断言形态） */
async function rootTokens(page: Page) {
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
async function expectAxeClean(page: Page, label: string) {
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
  await loginAs(page, 'admin')
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
  await loginAs(page, 'admin')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expectAxeClean(page, 'dashboard(light)')
  // 新路由面（M8 IA；旧路径兼容窗口归 shell.spec 的映射表腿）
  for (const [label, path] of [
    ['repositories', '/binflow/ui/admin/repositories/local'],
    ['security-users', '/binflow/ui/admin/security/users'],
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
  await loginAs(page, 'admin')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expectAxeClean(page, 'dashboard(dark)')
  for (const [label, path] of [
    ['repositories', '/binflow/ui/admin/repositories/local'],
    ['security-users', '/binflow/ui/admin/security/users'],
  ] as const) {
    await page.goto(path)
    await expectAxeClean(page, `${label}(dark)`)
  }
})
