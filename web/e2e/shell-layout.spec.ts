import { expect, test } from '@playwright/test'

import { expectA11yClean } from './m8/support/a11y'

// T-UIB2（design-system-plan §6 批 2）：壳布局 token 消费腿。
// 断言口径（ADR-0029 决策 3 延伸）：布局尺寸是宪章 §13 自选值（240/64
// 非 Artifactory 260 的翻拍——皮肤色值克隆断言仍被禁止，此处只钉布局），
// computed 具体值断言合法：侧栏 240 / 顶栏 64 / 内容 max 1440 必须从
// --bf-sidebar-w / --bf-topbar-h / --bf-content-max 经桥接语义类
// （w-sidebar / h-topbar / max-w-content）生效——w-56 / h-12 /
// max-w-[1440px] 字面量回归（G4）在本腿翻红。
// axe 双主题随腿（serious/critical 零新增——现值基线
// docs/reverse/frontend/binflow-baseline/axe-current-*.json）。
// 运行前提：已 `make console && go build` 的真二进制在前台 serve，
// BASE 指向它（auth-shell.spec.ts 同款探针约定）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: import('@playwright/test').Page) {
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** 根节点布局 token 的 computed 取值（token 定义层 → 桥接 → 壳消费全链） */
async function rootLayoutTokens(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    const cs = getComputedStyle(document.documentElement)
    const v = (name: string) => cs.getPropertyValue(name).trim()
    return { sidebarW: v('--bf-sidebar-w'), topbarH: v('--bf-topbar-h'), contentMax: v('--bf-content-max') }
  })
}

test('shell consumes layout tokens: sidebar 240 / topbar 64 / content max 1440', async ({ page }) => {
  await page.goto('/binflow/ui/')
  await login(page)
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="dashboard-instance-card"]')).toBeVisible()

  // token 定义层取值（宪章 §13：BinFlow 自选值）
  const tokens = await rootLayoutTokens(page)
  expect(tokens.sidebarW).toBe('240px')
  expect(tokens.topbarH).toBe('64px')
  expect(tokens.contentMax).toBe('1440px')

  // 壳消费层：aside 宽 / 顶栏高 / 内容 max（桥接语义类生效的端到端证据）
  const aside = page.locator('aside')
  await expect(aside).toBeVisible()
  expect((await aside.boundingBox())?.width).toBe(240)
  await expect(page.locator('header.app-topbar')).toHaveCSS('height', '64px')
  const mainWrap = page.locator('main[data-slot="app-main"] > div')
  await expect(mainWrap).toHaveCSS('max-width', '1440px')
})

test('sidebar active indication: left accent bar + soft bg on the current entry', async ({ page }) => {
  await page.goto('/binflow/ui/')
  await login(page)
  // 登录落点 /artifacts（console-m8 §1.1）——「制品」条目持 active
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts(\/|$)/)
  const active = page.locator('a.nav-item.active')
  await expect(active).toHaveCount(1)
  await expect(active).toContainText('制品')
  // Penpot 式指示：左缘 2px primary 指示条（非 transparent）+ 软底（非透明）
  await expect(active).toHaveCSS('border-left-width', '2px')
  expect(await active.evaluate((el) => getComputedStyle(el).borderLeftColor)).not.toBe('rgba(0, 0, 0, 0)')
  expect(await active.evaluate((el) => getComputedStyle(el).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)')
  // 非 active 条目不带指示条（transparent 左缘）
  const idle = page.locator('a.nav-item:not(.active)').first()
  expect(await idle.evaluate((el) => getComputedStyle(el).borderLeftColor)).toBe('rgba(0, 0, 0, 0)')
})

test('shell axe: serious/critical zero, both themes', async ({ page }, testInfo) => {
  await page.goto('/binflow/ui/')
  await login(page)
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await expectA11yClean(page, testInfo)
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expectA11yClean(page, testInfo)
})
