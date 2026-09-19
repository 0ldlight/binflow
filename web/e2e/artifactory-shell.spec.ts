import { expect, test } from '@playwright/test'
import { loginAs, provisionRoles } from './m8/support/roles'

const APP_ORDER = ['Packages', 'Builds', 'Artifacts', 'Release Lifecycle']
const ADMIN_ORDER = [
  'All Projects Overview',
  'Stages & Lifecycle',
  'Repositories',
  'User Management',
  'Proxies',
  'Authentication',
  'Security',
  'General Management',
  'Monitoring',
  'Topology',
  'Support Zone',
  'Artifactory Settings',
]
const USER_MANAGEMENT_ORDER = ['Users', 'Groups', 'Permissions', 'Global Roles', 'Access Tokens']

async function texts(locator: import('@playwright/test').Locator): Promise<string[]> {
  return (await locator.allTextContents()).map((value) => value.trim().replace(/\s*›$/, ''))
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

test('Artifactory shell: exact app order, mode switch, admin tree, hover/focus flyout', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/packages')
  const nav = page.locator('[data-testid="app-nav"]')

  await expect(nav).toHaveAttribute('data-mode', 'platform')
  await expect(page.locator('[data-testid="nav-mode-platform"]')).toHaveAttribute('aria-current', 'page')
  expect(await texts(nav.locator('.app-nav-items a.nav-item'))).toEqual(APP_ORDER)

  await page.click('[data-testid="nav-mode-administration"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local/)
  await expect(nav).toHaveAttribute('data-mode', 'administration')
  await expect(page.locator('[data-testid="nav-mode-administration"]')).toHaveAttribute('aria-current', 'page')
  const adminItems = await texts(nav.locator('.app-nav-items .nav-item'))
  expect(adminItems.slice(0, ADMIN_ORDER.length)).toEqual(ADMIN_ORDER)
  // Superset capabilities stay terminal and clearly labeled, never interleaved.
  expect(adminItems.slice(ADMIN_ORDER.length)).toEqual(['Quotas', 'Replication', 'Trash', 'Audit Log', 'Webhooks', 'License & Add-ons'])
  await expect(page.getByTestId('nav-gap-proxies')).toBeDisabled()

  // Parent focus opens the same ordered flyout as hover; Global Roles remains
  // an explicit disabled reference gap rather than a fake route.
  // Keyboard traversal opens the same flyout: Administration → All Projects
  // Overview → Stages → Repositories → User Management.
  await page.evaluate(() => {
    document.querySelector('[data-testid="nav-entry-user-management"]')?.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }),
    )
  })
  const menu = page.getByTestId('nav-menu-user-management')
  await expect(menu).toBeVisible()
  expect(await texts(menu.locator('.nav-item'))).toEqual(USER_MANAGEMENT_ORDER)
  await expect(menu.getByTestId('nav-gap-global-roles')).toBeDisabled()

  // Administration filtering preserves reference order and removes non-matches.
  await page.fill('[data-testid="admin-filter"]', 'Backups')
  await expect(page.locator('[data-testid="admin-filter-empty"]')).toHaveCount(0)
  expect(await texts(nav.locator('.app-nav-items .nav-item'))).toEqual(['Artifactory Settings'])
  await page.hover('[data-testid="nav-entry-artifactory-settings"]')
  await expect(page.getByTestId('nav-menu-artifactory-settings')).toBeVisible()
  expect(await texts(page.getByTestId('nav-menu-artifactory-settings').locator('.nav-item'))).toEqual(['Backups'])

  await page.fill('[data-testid="admin-filter"]', '')
  await page.click('[data-testid="nav-mode-platform"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/packages$/)
  await expect(nav).toHaveAttribute('data-mode', 'platform')
})

test('plain user sees only the Artifactory app menu and cannot enter Administration', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/packages')
  const nav = page.locator('[data-testid="app-nav"]')

  await expect(nav).toHaveAttribute('data-mode', 'platform')
  expect(await texts(nav.locator('.app-nav-items a.nav-item'))).toEqual(APP_ORDER)
  await expect(page.locator('[data-testid="nav-mode-administration"]')).toBeDisabled()
})
