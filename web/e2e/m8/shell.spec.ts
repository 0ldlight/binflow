import { expect, test } from '@playwright/test'
import { loginAs, provisionRoles } from './support/roles'
import { m8Client, roleFixturesFromEnv, seedRepos } from './support/seed'

// T-235 双模式壳与路由重排（console-m8 §1/§2；FR-71 AC1~AC3）：
//   1. 三角色 × 双模式导航可达性（应用侧栏 2 条目 / 管理侧栏五分组 14 条目
//      〔M8 基线 12 + M10 T-288 的「常规」分组 License & Add-ons 项 + M11 T-307「用户与权限」分组认证配置项〕；
//      readonly_admin 见「管理」入口；普通用户无入口且 /admin/** 直链保持
//      应用侧栏 + 页面 L2 收敛——§2.2 姿态不变）。
//   2. 旧路由终态（T-263，Q3 终裁）：console-m8 §1.4 的 20 条映射全量移除
//      ——旧路径不再重定向，直链落 NotFound（原始路径回显 + 回主页，
//      T-239 形态）；查询串不复活重定向。首页 `/` 的 index 落点保留。
//   3. 键盘导航：模式切换 / 用户菜单 Quick 动作全键盘驱动（§3.4）。
// 锚口径 = ADR-0029 决策 3：锚不随路由改名；URL 断言仅出现在外部前缀与
// 本终态腿两处（README §2.1；决策 6：路径断言随路由表改，同票携带）。

const REPO = 'm8-shell-legacy-local'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

/** 管理模式侧栏五分组 × 16 条目（console-m8 §1.3 全图 + M10 T-288/M11 T-307/M12 T-352/M13 T-366 增量）。
 *  T-388（N2/V5）起每条一级条目带 16px mono 图标（nav-icon 家族锚）——档位
 *  仅一级条目：分组标签与底部模式切换项不配（V5 活体核验口径）。 */
const ADMIN_GROUPS = ['仓库', '用户与权限', '治理', '监控', '常规'] as const

const ADMIN_ENTRIES: [string, string][] = [
  ['仓库', 'repos-page'],
  ['用户', 'users-page'],
  ['组', 'groups-page'],
  ['权限', 'perms-page'],
  ['Access Tokens', 'tokens-page'], // M14 T-386 落真身（原占位页承载）
  ['认证配置', 'authcfg-page'], // M11 T-307（FR-92——LDAP/OAuth/SAML 三协议）
  ['审计日志', 'audit-page'],
  ['维护（GC）', 'gc-page'],
  ['配额', 'quotas-page'],
  ['复制', 'repl-page'],
  ['备份 / 恢复', 'backup-page'],
  ['回收站', 'trash-page'], // M12 T-352（FR-106——浏览/恢复/清空；槽门控态）
  ['Webhooks', 'wh-page'], // M13 T-366（FR-115.5——订阅 CRUD/test + 投递排障）
  ['存储', 'storage-page'], // T-238 落真身（原 placeholder-page 占位）
  ['系统信息', 'settings'],
  ['License & Add-ons', 'license-page'], // M10 T-288（FR-86-AC5）
]

test('admin: app-mode sidebar (2 entries) -> admin mode (5 groups / 16 entries) -> back, all keyboard', async ({
  page,
}) => {
  await seedRepos(m8Client(), [{ key: REPO }])
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', roleFixturesFromEnv().admin.name)
  await page.fill('[data-testid="login-password"]', roleFixturesFromEnv().admin.password)
  await page.click('[data-testid="login-submit"]')

  // 登录落点 = /artifacts（console-m8 §1.1；T-236 起跨仓树真身承载）
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts$/)
  const nav = page.locator('[data-testid="app-nav"]')
  await expect(nav.locator('.nav-group-label', { hasText: '应用' })).toBeVisible()
  // 2 条目 + 模式切换项（button.nav-item）
  await expect(nav.locator('.nav-item')).toHaveCount(3)
  await expect(nav.locator('a.nav-item', { hasText: '仪表盘' })).toBeVisible()
  await expect(nav.locator('a.nav-item', { hasText: '制品' })).toBeVisible()
  // 应用域一级条目图标（T-388 N2/V5：2/2）
  await expect(nav.locator('a.nav-item [data-testid="nav-icon"]')).toHaveCount(2)
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
  await expect(nav.locator('a.nav-item')).toHaveCount(16)
  // 一级条目图标（T-388 N2/V5）：16/16 逐条在场、aria-hidden 装饰位；
  // 分组标签与模式切换项不配（档位 = 仅一级条目）
  await expect(nav.locator('a.nav-item [data-testid="nav-icon"]')).toHaveCount(16)
  await expect(nav.locator('.nav-group-label [data-testid="nav-icon"]')).toHaveCount(0)
  await expect(nav.locator('[data-testid="nav-mode-switch"] [data-testid="nav-icon"]')).toHaveCount(0)
  for (const [label] of ADMIN_ENTRIES) {
    await expect(
      nav.locator(`a.nav-item:text-is("${label}") [data-testid="nav-icon"]`),
      `icon for ${label}`,
    ).toBeVisible()
  }
  // 面包屑（§1.3：管理页层级表达）
  await expect(page.locator('[data-testid="topbar-breadcrumb"]')).toContainText('仓库')

  // 16 条目逐项可达（URL 均落 /admin/** + 页面锚到达）
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

  // 首页 index 落点不受移除影响（§1.1 登录落点，非兼容窗口）
  await page.goto('/binflow/ui/')
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts$/)
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

  // 404 页深链回主页（T-239 已备：主行动回应用模式首页 /artifacts）
  await page.click('[data-testid="not-found-home"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts$/)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
})
