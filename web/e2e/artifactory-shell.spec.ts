import { expect, test } from '@playwright/test'
import { loginAs, provisionRoles } from './m8/support/roles'

const ARTIFACTORY_APP_ORDER = ['Packages', 'Builds', 'Artifacts', 'Release Lifecycle']
const APP_ORDER = [...ARTIFACTORY_APP_ORDER, 'Dashboard']
const ADMIN_SECTIONS = [
  'Repositories',
  'User Management',
  'Authentication',
  'Security',
  'General Management',
  'Monitoring',
  'Artifactory Settings',
  'BinFlow Extensions',
]
const ADMIN_ITEMS = [
  'Repositories',
  'Users',
  'Groups',
  'Permissions',
  'Access Tokens',
  'LDAP',
  'Signing Keys',
  'Settings',
  'Service Status',
  'Storage',
  'System Logs',
  'System Info',
  'Maintenance',
  'Backups',
  'Quotas',
  'Replication',
  'Trash',
  'Audit Log',
  'Webhooks',
  'License & Add-ons',
]

async function texts(locator: import('@playwright/test').Locator): Promise<string[]> {
  return (await locator.allTextContents()).map((value) => value.trim())
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

test('Artifactory-aligned shell remains directly usable', async ({ page }) => {
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
  expect(await texts(nav.locator('.app-nav-items .nav-group-label'))).toEqual(ADMIN_SECTIONS)
  expect(await texts(nav.locator('.app-nav-items a.nav-item'))).toEqual(ADMIN_ITEMS)
  await expect(page.locator('[data-testid^="nav-gap-"]')).toHaveCount(0)
  await expect(page.locator('[data-testid^="nav-menu-"]')).toHaveCount(0)
  await expect(page.getByTestId('nav-entry-trash')).toBeVisible()

  // Filtering finds a direct destination and keeps its Artifactory section.
  await page.fill('[data-testid="admin-filter"]', 'Backups')
  await expect(page.locator('[data-testid="admin-filter-empty"]')).toHaveCount(0)
  expect(await texts(nav.locator('.app-nav-items .nav-group-label'))).toEqual(['Artifactory Settings'])
  expect(await texts(nav.locator('.app-nav-items a.nav-item'))).toEqual(['Backups'])

  await page.fill('[data-testid="admin-filter"]', '')
  await page.click('[data-testid="nav-mode-platform"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/packages$/)
  await expect(nav).toHaveAttribute('data-mode', 'platform')
})

test('plain user sees usable app entries and cannot enter Administration', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/packages')
  const nav = page.locator('[data-testid="app-nav"]')

  await expect(nav).toHaveAttribute('data-mode', 'platform')
  expect(await texts(nav.locator('.app-nav-items a.nav-item'))).toEqual(APP_ORDER)
  await expect(page.locator('[data-testid="nav-mode-administration"]')).toBeDisabled()
})
