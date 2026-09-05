import { expect, test } from '@playwright/test'

// T-463（FR-149.1）i18n 框架接入的最小机制腿——文案翻转/切换器/reload
// 持久归 T-464 的双语断言；本 spec 只锁三件本票交付的机制事实：
//
//  1. zh 默认locale：渲染文案与外提前一致（zh-as-key——键即文案），且
//     <html lang> 同步 zh-CN；零 [i18n] 运行时告警（AC2）。
//  2. en 持久化引导（localStorage binflow-console-locale=en）：en 目录包
//     值真渲染（T-464 填充后；骨架期回落断言已随填充翻新），lang=en，
//     目录包懒载 chunk 被请求（initI18n 闸生效）。
//  3. 持久化机制双向：写键 + reload 后仍按 en 引导（setLocale 的持久化
//     半边——切换器 UI 归 T-464，此处直接落 localStorage 模拟）。
//
// 默认 locale 断言零翻新：本 spec 不改任何既有 spec 的 zh 文案断言
// （AC2「既有全部 FE spec 零回归」由全量抽样回归证明）。

const LOCALE_KEY = 'binflow-console-locale'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary')
})

test('zh default: extracted copy renders verbatim, lang synced, no i18n warnings', async ({ page }) => {
  const warnings: string[] = []
  page.on('console', (m) => {
    if (m.type() === 'warning' && m.text().includes('[i18n]')) warnings.push(m.text())
  })
  await page.goto('/binflow/ui/login')
  // 外提前 LoginPage 的 zh 文案原样呈现（zh-as-key：键值恒等）
  await expect(page.locator('[data-testid="login-username"]')).toBeVisible()
  await expect(page.getByRole('button', { name: '登录' })).toBeVisible()
  await expect(page.getByText('制品仓库控制台')).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  expect(warnings, '默认 zh 引导不得出现 [i18n] 运行时告警').toEqual([])
})

test('en persisted boot: catalog values render, lang=en, catalog chunk fetched', async ({ page }) => {
  await page.addInitScript((k) => localStorage.setItem(k, 'en'), LOCALE_KEY)
  const catalogRequests: string[] = []
  page.on('request', (r) => {
    if (/\/assets\/catalogs-[^/]+\.js/.test(r.url())) catalogRequests.push(r.url())
  })
  await page.goto('/binflow/ui/login')
  // T-464 填充后：en 值真渲染（骨架期的「回落 zh 键文案」断言随填充翻新——
  // 目录包缺键时的回落语义仍由内核保证，此处断言当前填充态）
  await expect(page.getByText('Artifact Repository Console')).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  // initI18n 闸：en 引导确实懒载了目录包 chunk（zh 用户零请求）
  expect(catalogRequests.length).toBeGreaterThan(0)
})

test('persistence survives reload (en stays en until the key changes)', async ({ page }) => {
  // 一次性注入：sessionStorage 旗标让 init script 只在首次加载落键
  // （addInitScript 每次 reload 重放，不设旗标会把删掉的键写回）
  await page.addInitScript(
    ([k, m]) => {
      if (!sessionStorage.getItem(m)) {
        sessionStorage.setItem(m, '1')
        localStorage.setItem(k, 'en')
      }
    },
    [LOCALE_KEY, 't463-locale-seeded'],
  )
  await page.goto('/binflow/ui/login')
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  // 清键回 zh（等价 setLocale('zh') 的持久化半边——reload 后按 zh 引导）
  await page.evaluate((k) => localStorage.removeItem(k), LOCALE_KEY)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
})
