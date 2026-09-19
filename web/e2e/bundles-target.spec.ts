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

  await page.goto('/binflow/ui/bundles/source')
  await expect(page.locator('[data-testid="bundles-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="bundle-tab-source"]')).toBeVisible()

  await page.click('[data-testid="bundle-tab-target"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/bundles\/target$/)
  await expect(page.locator('[data-testid="target-bundles-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="target-bundles-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="target-bundles-empty"]')).toContainText('暂无 Received Release Bundle')

  await page.goto('/binflow/ui/bundles/target/audit-probe/1.0')
  await expect(page.locator('[data-testid="target-history-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="target-history-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="target-history-empty"]')).toContainText('没有 target 历史记录')

  await expectA11yClean(page, testInfo, { include: '[data-testid="target-history-page"]' })
})
