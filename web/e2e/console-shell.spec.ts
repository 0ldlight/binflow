import { expect, test } from '@playwright/test'
import { loginAs, provisionRoles } from './m8/support/roles'

const CONSOLE_APP_ORDER = ['软件包', '构建', '制品', '发布生命周期']
const APP_ORDER = [...CONSOLE_APP_ORDER, '仪表盘']
const ADMIN_SECTIONS = [
  '仓库',
  '用户管理',
  '认证',
  '安全',
  '通用管理',
  '监控',
  '仓库设置',
  'BinFlow 扩展',
]
const ADMIN_ITEMS = [
  '仓库',
  '用户',
  '组',
  '权限',
  '访问令牌',
  'LDAP',
  '签名密钥',
  '设置',
  '服务状态',
  '存储',
  '系统日志',
  '系统信息',
  '维护',
  '备份',
  '配额',
  '复制',
  '回收站',
  '审计日志',
  'Webhooks',
  '许可与扩展',
]

async function texts(locator: import('@playwright/test').Locator): Promise<string[]> {
  return (await locator.allTextContents()).map((value) => value.trim())
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

test('同类控制台-aligned shell remains directly usable', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/packages')
  const nav = page.locator('[data-testid="app-nav"]')

  await expect(nav).toHaveAttribute('data-mode', 'platform')
  await expect(page.locator('[data-testid="nav-mode-platform"]')).toHaveAttribute('aria-current', 'page')
  expect(await texts(nav.locator('.app-nav-items a.nav-item'))).toEqual(APP_ORDER)

  await page.click('[data-testid="nav-mode-administration"]')
  await expect(page).toHaveURL('/binflow/ui/admin/repositories/local')
  await expect(nav).toHaveAttribute('data-mode', 'administration')
  await expect(page.locator('[data-testid="nav-mode-administration"]')).toHaveAttribute('aria-current', 'page')
  expect(await texts(nav.locator('.app-nav-items .nav-group-label'))).toEqual(ADMIN_SECTIONS)
  expect(await texts(nav.locator('.app-nav-items a.nav-item'))).toEqual(ADMIN_ITEMS)
  await expect(page.locator('[data-testid^="nav-gap-"]')).toHaveCount(0)
  await expect(page.locator('[data-testid^="nav-menu-"]')).toHaveCount(0)
  await expect(page.getByTestId('nav-entry-trash')).toBeVisible()

  // Filtering finds a direct destination and keeps its 同类控制台 section.
  await page.fill('[data-testid="admin-filter"]', '备份')
  await expect(page.locator('[data-testid="admin-filter-empty"]')).toHaveCount(0)
  expect(await texts(nav.locator('.app-nav-items .nav-group-label'))).toEqual(['仓库设置'])
  expect(await texts(nav.locator('.app-nav-items a.nav-item'))).toEqual(['备份'])

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
