import { expect, test } from '@playwright/test'
import { expectA11yClean } from './m8/support/a11y'

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

async function login(page: import('@playwright/test').Page) {
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

test('release bundles: source/target route split and honest v2 target empty/history faces', async ({ page }, testInfo) => {
  await login(page)

  await page.click('[data-testid="nav-entry-release-lifecycle"]')
  await expect(page).toHaveURL('/binflow/ui/release-lifecycle')
  await expect(page.locator('[data-testid="bundles-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="bundles-page"] h2')).toHaveText('Release Lifecycle')
  await expect(page.locator('[data-testid="bundles-search"]')).toHaveAttribute('placeholder', 'Search Release Bundles')
  await expect(page.locator('[data-testid="bundles-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="bundle-mode-tabs"]')).toHaveCount(0)

  // Structural header check with one deterministic row; the unmocked empty face
  // above remains the real integration assertion.
  await page.route('**/binflow/api/release/bundles', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ bundles: { demo: { uri: '/demo', name: 'demo' } } }),
    })
  })
  await page.reload()
  await expect(page.locator('[data-testid="bundles-row-demo"] a')).toHaveAttribute('href', '/binflow/ui/release-bundles/demo')
  await expect(page.locator('[data-testid="bundles-table"] th').nth(0)).toHaveText('Release Bundle Name')
  await expect(page.locator('[data-testid="bundles-table"] th').nth(1)).toHaveText('Project')
  await expect(page.locator('[data-testid="bundles-table"] th').nth(2)).toHaveText('Number of Versions')
  await expect(page.locator('[data-testid="bundles-table"] th').nth(3)).toHaveText('Latest Version')
  await page.unroute('**/binflow/api/release/bundles')

  await page.goto('/binflow/ui/release-bundles/target')
  await expect(page.locator('[data-testid="target-bundles-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="target-bundles-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="target-bundles-empty"]')).toContainText('暂无 Received Release Bundle')

  await page.goto('/binflow/ui/release-bundles/target/audit-probe/1.0')
  await expect(page.locator('[data-testid="target-history-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="target-history-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="target-history-empty"]')).toContainText('没有 target 历史记录')

  // Legacy deep links remain a compatibility window while all shell navigation
  // and in-page links use the 同类控制台-shaped route family.
  await page.goto('/binflow/ui/bundles/target/audit-probe/1.0')
  await expect(page.locator('[data-testid="target-history-page"]')).toBeVisible()

  await expectA11yClean(page, testInfo, { include: '[data-testid="target-history-page"]' })
})
