import { expect, test } from '@playwright/test'
import { loginAs, provisionRoles } from './support/roles'
import { m8Client, roleFixturesFromEnv, seedRepos } from './support/seed'

// T-235 双模式壳与路由重排（console-m8 §1/§2；FR-71 AC1~AC3）：
//   1. 三角色 × 双模式导航可达性（应用侧栏 2 条目 / 管理侧栏五分组 12 条目；
//      readonly_admin 见「管理」入口；普通用户无入口且 /admin/** 直链保持
//      应用侧栏 + 页面 L2 收敛——§2.2 姿态不变）。
//   2. 旧路由 20 条映射表驱动 redirect（console-m8 §1.4 兼容窗口，Q3 终裁
//      M9 移除）——URL 落新路由 + 页面锚到达（功能等价）；查询串保真。
//   3. 键盘导航：模式切换 / 用户菜单 Quick 动作全键盘驱动（§3.4）。
// 锚口径 = ADR-0029 决策 3：锚不随路由改名；URL 断言仅出现在外部前缀与
// 本兼容窗口两处（README §2.1）。

const REPO = 'm8-shell-legacy-local'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

/** 管理模式侧栏五分组 × 12 条目（console-m8 §1.3 全图） */
const ADMIN_GROUPS = ['仓库', '用户与权限', '治理', '监控', '常规'] as const

const ADMIN_ENTRIES: [string, string][] = [
  ['仓库', 'repos-page'],
  ['用户', 'users-page'],
  ['组', 'groups-page'],
  ['权限', 'perms-page'],
  ['Access Tokens', 'placeholder-page'],
  ['审计日志', 'audit-page'],
  ['维护（GC）', 'gc-page'],
  ['配额', 'quotas-page'],
  ['复制', 'repl-page'],
  ['备份 / 恢复', 'backup-page'],
  ['存储', 'placeholder-page'],
  ['系统信息', 'settings'],
]

test('admin: app-mode sidebar (2 entries) -> admin mode (5 groups / 12 entries) -> back, all keyboard', async ({
  page,
}) => {
  await seedRepos(m8Client(), [{ key: REPO }])
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', roleFixturesFromEnv().admin.name)
  await page.fill('[data-testid="login-password"]', roleFixturesFromEnv().admin.password)
  await page.click('[data-testid="login-submit"]')

  // 登录落点 = /artifacts（console-m8 §1.1；T-236 占位承载）
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts$/)
  const nav = page.locator('[data-testid="app-nav"]')
  await expect(nav.locator('.nav-group-label', { hasText: '应用' })).toBeVisible()
  // 2 条目 + 模式切换项（button.nav-item）
  await expect(nav.locator('.nav-item')).toHaveCount(3)
  await expect(nav.locator('a.nav-item', { hasText: '仪表盘' })).toBeVisible()
  await expect(nav.locator('a.nav-item', { hasText: '制品' })).toBeVisible()
  // 应用模式无管理分组（无影子入口）
  for (const g of ADMIN_GROUPS) {
    await expect(nav.locator('.nav-group-label', { hasText: g })).toHaveCount(0)
  }

  // 键盘切管理模式：落 /admin/repositories/local + 管理侧栏
  await page.focus('[data-testid="nav-mode-switch"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)
  await expect(page.locator('[data-testid="repos-page"]')).toBeVisible()
  for (const g of ADMIN_GROUPS) {
    await expect(nav.locator('.nav-group-label', { hasText: g })).toBeVisible()
  }
  await expect(nav.locator('a.nav-item')).toHaveCount(12)
  // 面包屑（§1.3：管理页层级表达）
  await expect(page.locator('[data-testid="topbar-breadcrumb"]')).toContainText('仓库')

  // 12 条目逐项可达（URL 均落 /admin/** + 页面锚到达）
  for (const [label, anchor] of ADMIN_ENTRIES) {
    await page.click(`[data-testid="app-nav"] a.nav-item:text-is("${label}")`)
    await expect(page).toHaveURL(/\/binflow\/ui\/admin\//)
    await expect(page.locator(`[data-testid="${anchor}"]`).first()).toBeVisible()
  }

  // 键盘切回应用模式（aria-current 随模式翻转）
  await expect(page.locator('[data-testid="nav-mode-switch"]')).toHaveAttribute('aria-current', 'true')
  await page.focus('[data-testid="nav-mode-switch"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts$/)
  await expect(page.locator('[data-testid="nav-mode-switch"]')).not.toHaveAttribute('aria-current', 'true')
})

test('readonly_admin: admin mode reachable, readonly badge, no quick-create write entries', async ({
  page,
}) => {
  await loginAs(page, 'readonly_admin') // 内含只读徽章断言

  // 管理入口可见（M7 §7.3 语义保留）；深链进管理模式
  await page.goto('/binflow/ui/admin/governance/audit')
  await expect(page.locator('[data-testid="audit-page"]')).toBeVisible()
  const nav = page.locator('[data-testid="app-nav"]')
  for (const g of ADMIN_GROUPS) {
    await expect(nav.locator('.nav-group-label', { hasText: g })).toBeVisible()
  }
  await expect(page.locator('[data-testid="nav-mode-switch"]')).toHaveText(/返回应用/)

  // 用户菜单（§2.3）：readonly_admin 不见快速建仓/新建写入口（L4 预收敛）
  await page.click('[data-testid="session-toggle"]')
  await expect(page.locator('[data-testid="quick-set-me-up"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="quick-new-user"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="quick-new-perm"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="menu-edit-profile"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="menu-edit-profile"]')).toHaveCount(0)
})

test('plain user: no mode switch; /admin/** deep link keeps app sidebar + L2 convergence', async ({
  page,
}) => {
  await loginAs(page, 'user')

  await expect(page.locator('[data-testid="nav-mode-switch"]')).toHaveCount(0)
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-page"]')).toBeVisible()
  // 壳保留 + L2 无权限卡（§2.2：页面自身 403 收敛）；侧栏不泄管理分组（L1）
  await expect(page.locator('[data-testid="users-page"] [data-testid="empty-state"]')).toBeVisible()
  const nav = page.locator('[data-testid="app-nav"]')
  await expect(nav.locator('.nav-group-label', { hasText: '应用' })).toBeVisible()
  for (const g of ADMIN_GROUPS) {
    await expect(nav.locator('.nav-group-label', { hasText: g })).toHaveCount(0)
  }
  // 用户菜单只有 编辑档案/主题/登出（无 Quick 写入口）
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

  // 快速建仓子菜单（rclass 参数形态就位——T-240 消费）
  await page.focus('[data-testid="session-toggle"]')
  await page.keyboard.press('Enter')
  await page.focus('[data-testid="quick-new-repo-remote"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/new\?rclass=remote$/)
  await expect(page.locator('[data-testid="repo-form-page"]')).toBeVisible()
})

// ---- 旧路由兼容窗口：20 条映射表驱动（console-m8 §1.4）----------------------
// 实体腿先种（幂等）：仓库 + 根文件；用户/权限 target 来自 provisionRoles。

const LEGACY_ROUTES: { from: string; to: RegExp; anchor: string }[] = [
  { from: '/', to: /\/binflow\/ui\/artifacts$/, anchor: 'placeholder-page' },
  { from: '/repositories', to: /\/binflow\/ui\/admin\/repositories\/local$/, anchor: 'repos-page' },
  { from: '/repositories/new', to: /\/binflow\/ui\/admin\/repositories\/new$/, anchor: 'repo-form-page' },
  { from: `/repositories/${REPO}`, to: new RegExp(`/binflow/ui/admin/repositories/${REPO}$`), anchor: 'repo-detail-page' },
  { from: `/repositories/${REPO}/settings`, to: new RegExp(`/binflow/ui/admin/repositories/${REPO}/edit$`), anchor: 'repo-form-page' },
  { from: `/repositories/${REPO}/tree`, to: new RegExp(`/binflow/ui/artifacts/${REPO}$`), anchor: 'tree-page' },
  { from: `/repositories/${REPO}/tree/perf`, to: new RegExp(`/binflow/ui/artifacts/${REPO}/perf$`), anchor: 'tree-page' },
  { from: '/security/users', to: /\/binflow\/ui\/admin\/security\/users$/, anchor: 'users-page' },
  { from: `/security/users/${roleFixturesFromEnv().user.name}`, to: new RegExp(`/binflow/ui/admin/security/users/${roleFixturesFromEnv().user.name}$`), anchor: 'user-detail-page' },
  { from: '/security/groups', to: /\/binflow\/ui\/admin\/security\/groups$/, anchor: 'groups-page' },
  { from: '/security/permissions', to: /\/binflow\/ui\/admin\/security\/permissions$/, anchor: 'perms-page' },
  { from: '/security/permissions/new', to: /\/binflow\/ui\/admin\/security\/permissions\/new$/, anchor: 'perm-editor-page' },
  { from: '/security/permissions/m8-e2e-read', to: /\/binflow\/ui\/admin\/security\/permissions\/m8-e2e-read$/, anchor: 'perm-editor-page' },
  { from: '/security/tokens', to: /\/binflow\/ui\/admin\/security\/tokens$/, anchor: 'placeholder-page' },
  { from: '/audit', to: /\/binflow\/ui\/admin\/governance\/audit$/, anchor: 'audit-page' },
  { from: '/governance/gc', to: /\/binflow\/ui\/admin\/governance\/gc$/, anchor: 'gc-page' },
  { from: '/governance/quotas', to: /\/binflow\/ui\/admin\/governance\/quotas$/, anchor: 'quotas-page' },
  { from: '/governance/replication', to: /\/binflow\/ui\/admin\/governance\/replication$/, anchor: 'repl-page' },
  { from: '/governance/backup', to: /\/binflow\/ui\/admin\/governance\/backup$/, anchor: 'backup-page' },
  { from: '/settings', to: /\/binflow\/ui\/admin\/general\/settings$/, anchor: 'settings' },
]

test('legacy routes: all 20 console-m8 §1.4 redirects land on the new route with the page mounted', async ({
  page,
}) => {
  expect(LEGACY_ROUTES).toHaveLength(20)
  const client = m8Client()
  await seedRepos(client, [{ key: REPO }])
  // 树深链腿的根文件（空仓库根目录也是合法树态，但带一个文件让形态更真实）
  const put = await client.request('PUT', `/binflow/${REPO}/seed.txt`, {
    raw: true,
    headers: { 'Content-Type': 'application/octet-stream' },
    body: 'shell redirect fixture\n',
  })
  expect(put.status).toBeLessThan(300)

  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', roleFixturesFromEnv().admin.name)
  await page.fill('[data-testid="login-password"]', roleFixturesFromEnv().admin.password)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()

  for (const { from, to, anchor } of LEGACY_ROUTES) {
    await page.goto(`/binflow/ui${from}`)
    await expect(page, `redirect: ${from}`).toHaveURL(to)
    await expect(page.locator(`[data-testid="${anchor}"]`).first(), `anchor after ${from}`).toBeVisible()
  }

  // 查询串保真（树页 ?focus= 深链锚定依赖——T-231 编码矩阵同通道）
  await page.goto(`/binflow/ui/repositories/${REPO}/tree?focus=seed.txt`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${REPO}\\?focus=seed\\.txt$`))
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
})
