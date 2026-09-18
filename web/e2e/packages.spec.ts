import { expect, test } from '@playwright/test'
import { expectA11yClean } from './m8/support/a11y'

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'
const auth = { Authorization: `Basic ${Buffer.from(`${ADMIN}:${ADMIN_PW}`).toString('base64')}` }

async function login(page: import('@playwright/test').Page) {
  await page.goto('/binflow/ui/packages')
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="packages-page"]')).toBeVisible()
}

test('packages: real repository catalog, filters, recent views, and honest API gap', async ({ page, request }, testInfo) => {
  const marker = Date.now().toString(36)
  const generic = `pkgs-generic-${marker}`
  const maven = `pkgs-maven-${marker}`
  for (const [key, body] of [
    [generic, { rclass: 'local', packageType: 'generic', description: 'Packages e2e generic repository' }],
    [maven, { rclass: 'remote', packageType: 'maven', url: 'https://repo1.maven.org/maven2', description: 'Packages e2e maven repository' }],
  ] as const) {
    const res = await request.put(`/binflow/api/repositories/${key}`, { data: body, headers: auth })
    expect(res.status()).toBeLessThan(300)
  }

  await login(page)
  await expect(page.locator('[data-testid="packages-grid"]')).toBeVisible()
  await expect(page.locator('[data-testid="package-card-generic"]')).toContainText(generic)
  await expect(page.locator('[data-testid="package-card-maven"]')).toContainText(maven)
  await expect(page.locator('[data-testid="packages-api-gap"]')).toContainText('latest version')

  await page.fill('[data-testid="packages-search"]', maven)
  await expect(page.locator('[data-testid="packages-grid"] [data-testid^="package-card-"]')).toHaveCount(1)
  await expect(page.locator('[data-testid="package-card-maven"]')).toBeVisible()
  await page.click('[data-testid="packages-clear"]')
  await expect(page.locator('[data-testid="package-card-generic"]')).toBeVisible()

  await page.click('[data-testid="packages-tab-recent"]')
  await expect(page.locator('[data-testid="packages-empty"]')).toBeVisible()
  await page.click('[data-testid="packages-tab-all"]')
  await page.click('[data-testid="package-open-generic"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)
  await page.goto('/binflow/ui/packages')
  await page.click('[data-testid="packages-tab-recent"]')
  await expect(page.locator('[data-testid="package-card-generic"]')).toBeVisible()

  await expectA11yClean(page, testInfo, { include: '[data-testid="packages-page"]' })

  await request.delete(`/binflow/api/repositories/${generic}?deleteContent=true`, { headers: auth })
  await request.delete(`/binflow/api/repositories/${maven}?deleteContent=true`, { headers: auth })
})
