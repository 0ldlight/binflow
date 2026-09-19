import { expect, test } from '@playwright/test'
import { loginAs, provisionRoles } from './support/roles'
import { m8Client, roleFixturesFromEnv, seedRepos } from './support/seed'

// FE-Rewrite P2 四分组壳（architecture §4 IA——原 T-235 双模式概念退役：
// 分组即模式，权限可见性替代模式切换；nav-mode-switch 锚随形态退役 §10.7）：
//   1. 三角色 × 四分组导航可达性（核心/运营/安全/管理——admin 全见 25 条目；
//      readonly_admin 同见〔读姿态〕；普通用户仅见「全可见」条目——核心 3 +
//      运营 2，管理/安全分组整组不渲染〔L1〕，/admin/** 直链页面 L2 收敛）。
//   2. 旧路由终态（T-263，Q3 终裁）：20 条映射全量移除——旧路径直链落
//      NotFound（原始路径回显 + 回主页，T-239 形态）；查询串不复活重定向。
//   3. 键盘导航：侧栏条目与用户菜单 Quick 动作全键盘驱动（§3.4 承接）。
// 锚口径 = ADR-0029 决策 3：锚不随路由改名；nav-icon 一级条目锚在新壳延续
// （T-388 N2/V5 档位不变——分组标签不配）。

const REPO = 'm8-shell-legacy-local'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

/** Artifactory 7.161 application menu order (normative English labels). */
const APP_ENTRIES = ['packages', 'builds', 'artifacts', 'release-lifecycle'] as const

/** Existing BinFlow admin capabilities remain route-reachable after IA alignment. */
const ADMIN_ROUTE_ANCHORS: [string, string][] = [
  ['/admin/repositories/local', 'repos-page'],
  ['/admin/security/users', 'users-page'],
  ['/admin/security/groups', 'groups-page'],
  ['/admin/security/permissions', 'perms-page'],
  ['/admin/security/tokens', 'tokens-page'],
  ['/admin/security/keypair', 'keypair-page'],
  ['/admin/security/auth/ldap', 'authcfg-page'],
  ['/admin/governance/audit', 'audit-page'],
  ['/admin/governance/quotas', 'quotas-page'],
  ['/admin/governance/replication', 'repl-page'],
  ['/admin/governance/trash', 'trash-page'],
  ['/admin/monitoring/storage', 'storage-page'],
  ['/admin/monitoring/status', 'status-page'],
  ['/admin/monitoring/logs', 'logs-page'],
  ['/admin/monitoring/system-info', 'settings'],
  ['/admin/monitoring/gc', 'gc-page'],
  ['/admin/monitoring/backup', 'backup-page'],
  ['/admin/general/webhooks', 'wh-page'],
  ['/admin/monitoring/settings', 'settings-page'],
  ['/admin/general/license', 'license-page'],
]

test('admin: Artifactory app order, administration switch, and capability routes', async ({ page }) => {
  await seedRepos(m8Client(), [{ key: REPO }])
  await loginAs(page, 'admin')
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts(\/|$)/)
  const nav = page.locator('[data-testid="app-nav"]')

  await expect(nav).toHaveAttribute('data-mode', 'platform')
  await expect(nav.locator('a.nav-item')).toHaveCount(APP_ENTRIES.length)
  for (const label of APP_ENTRIES) {
    await expect(nav.locator(`[data-testid="nav-entry-${label}"]`)).toBeVisible()
  }

  // Keyboard navigation remains real link activation, not a custom menu shim.
  await page.focus('[data-testid="nav-entry-release-lifecycle"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL('/binflow/ui/artifactory/release-lifecycle')
  await expect(page.locator('[data-testid="bundles-page"]')).toBeVisible()

  await page.click('[data-testid="nav-mode-administration"]')
  await expect(nav).toHaveAttribute('data-mode', 'administration')
  for (const [route, anchor] of ADMIN_ROUTE_ANCHORS) {
    await page.goto(`/binflow/ui${route}`)
    await expect(page.locator(`[data-testid="${anchor}"]`).first()).toBeVisible()
  }

  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator('[data-testid="topbar-breadcrumb"]')).toContainText('Repositories')
})

test('readonly_admin: sees ordered admin tree, readonly badge, no quick-create write entries', async ({
  page,
}) => {
  await loginAs(page, 'readonly_admin') // includes readonly badge assertion

  await page.goto('/binflow/ui/admin/governance/audit')
  await expect(page.locator('[data-testid="audit-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="app-nav"]')).toHaveAttribute('data-mode', 'administration')
  await expect(page.getByTestId('nav-gap-proxies')).toBeDisabled()

  await page.click('[data-testid="session-toggle"]')
  await expect(page.locator('[data-testid="quick-set-me-up"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="quick-new-user"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="quick-new-perm"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="menu-edit-profile"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="menu-edit-profile"]')).toHaveCount(0)
})

test('plain user: only Artifactory app entries; /admin/** deep link keeps shell + L2 convergence', async ({
  page,
}) => {
  await loginAs(page, 'user')

  const nav = page.locator('[data-testid="app-nav"]')
  await expect(nav.locator('a.nav-item')).toHaveCount(APP_ENTRIES.length)
  for (const label of APP_ENTRIES) {
    await expect(nav.locator(`[data-testid="nav-entry-${label}"]`)).toBeVisible()
  }
  await expect(page.locator('[data-testid="nav-mode-administration"]')).toBeDisabled()
  await expect(nav.locator('.nav-group-label', { hasText: 'Administration' })).toHaveCount(0)

  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-page"] [data-testid="empty-state"]')).toBeVisible()

  await page.click('[data-testid="session-toggle"]')
  await expect(page.locator('[data-testid="quick-new-user"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="menu-edit-profile"]')).toBeVisible()
})

test('admin: user-menu quick actions are keyboard reachable', async ({ page }) => {
  await loginAs(page, 'admin')

  await page.focus('[data-testid="session-toggle"]')
  await page.keyboard.press('Enter')
  await page.focus('[data-testid="quick-new-perm"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/permissions\/new$/)
  await expect(page.locator('[data-testid="perm-editor-page"]')).toBeVisible()

  // 快速建仓子菜单（T-240 消费；T-443 起 /new?rclass= 链接经路由表兼容映射
  // 落 remote 分路由——兼容窗语义在此钉死）
  await page.focus('[data-testid="session-toggle"]')
  await page.keyboard.press('Enter')
  await page.focus('[data-testid="quick-new-repo-remote"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/remote\/new$/)
  await expect(page.locator('[data-testid="repo-form-page"]')).toBeVisible()
})

// ---- 旧路由终态（console-m8 §1.4 映射表随 T-263 全量移除）------------------
// 20 条映射中的 19 条旧路径不再重定向：直链落 NotFound——404 页保留导航壳、
// 深链状态回显原始路径（not-found-path）+ 回主页（not-found-home）。第 20
// 条 `/` 是 index 落点（console-m8 §1.1 登录落点），不属于兼容窗口，保留。
// URL 断言 = 「未发生跳转」的精确匹配；锚零改名——not-found 族沿用 T-239。

const LEGACY_PATHS = [
  '/repositories',
  '/repositories/new',
  `/repositories/${REPO}`,
  `/repositories/${REPO}/settings`,
  `/repositories/${REPO}/tree`,
  `/repositories/${REPO}/tree/perf`,
  '/security/users',
  `/security/users/${roleFixturesFromEnv().user.name}`,
  '/security/groups',
  '/security/permissions',
  '/security/permissions/new',
  '/security/permissions/m8-e2e-read',
  '/security/tokens',
  '/audit',
  '/governance/gc',
  '/governance/quotas',
  '/governance/replication',
  '/governance/backup',
  '/settings',
]

test('legacy routes: all console-m8 §1.4 paths land NotFound (redirect window removed)', async ({
  page,
}) => {
  expect(LEGACY_PATHS).toHaveLength(19)
  await loginAs(page, 'admin')

  // 首页 index 落点不受移除影响；T-492（B-3.2）：/artifacts 落点随即自动
  // 选中首仓库——前缀断言
  await page.goto('/binflow/ui/')
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts(\/|$)/)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()

  for (const from of LEGACY_PATHS) {
    await page.goto(`/binflow/ui${from}`)
    // 未发生客户端跳转：URL 原地不动 + 404 形态（壳内）+ 原始路径回显
    await expect(page, `404: ${from}`).toHaveURL(`/binflow/ui${from}`)
    await expect(page.locator('[data-testid="not-found"]'), `not-found after ${from}`).toBeVisible()
    await expect(
      page.locator('[data-testid="not-found-path"]'),
      `path echo after ${from}`,
    ).toHaveText(from)
  }

  // 查询串不复活重定向（原「查询串保真」腿反转）：旧树路径 + ?focus= 同落 404
  await page.goto(`/binflow/ui/repositories/${REPO}/tree?focus=seed.txt`)
  await expect(page).toHaveURL(
    new RegExp(`/binflow/ui/repositories/${REPO}/tree\\?focus=seed\\.txt$`),
  )
  await expect(page.locator('[data-testid="not-found"]')).toBeVisible()
  await expect(page.locator('[data-testid="not-found-path"]')).toHaveText(
    `/repositories/${REPO}/tree`,
  )

  // 404 页深链回主页（T-239 已备：主行动回首页 /artifacts）；T-492 前缀断言
  await page.click('[data-testid="not-found-home"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts(\/|$)/)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
})
