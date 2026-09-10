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

/** 四分组全标签（nav-model.ts NAV_GROUPS——中文 label 逐字） */
const GROUPS = ['核心', '运营', '安全', '管理'] as const

/** 管理可达条目（label × 页面锚——18 条旧基线 + P3 解锁双页） */
const ADMIN_ENTRIES: [string, string][] = [
  ['仓库', 'repos-page'],
  ['用户', 'users-page'],
  ['组', 'groups-page'],
  ['权限', 'perms-page'],
  ['Access Tokens', 'tokens-page'], // M14 T-386
  ['签名密钥', 'keypair-page'], // FE-Rewrite P3 解锁（API 10 op）
  ['认证配置', 'authcfg-page'], // M11 T-307
  ['审计日志', 'audit-page'],
  ['配额', 'quotas-page'],
  ['复制', 'repl-page'],
  ['回收站', 'trash-page'], // M12 T-352
  ['存储', 'storage-page'], // T-238
  ['服务状态', 'status-page'], // T-459
  ['系统日志', 'logs-page'], // T-459
  ['系统信息', 'settings'], // T-459 归位监控组
  ['维护（GC）', 'gc-page'], // T-459 迁监控组
  ['备份 / 恢复', 'backup-page'], // T-459 迁监控组
  ['Webhooks', 'wh-page'], // M13 T-366
  ['设置', 'settings-page'], // FE-Rewrite P3 解锁（/v1/system/settings 六旋钮+QRL）
  ['License & Add-ons', 'license-page'], // M10 T-288
]

/** 全可见条目（plain 用户同集——nav-model visibility:'all'） */
const ALL_ENTRIES = ['仪表盘', '制品', '搜索', 'Builds', 'Release Bundles'] as const

test('admin: four-group sidebar (25 entries) all reachable, keyboard-driven', async ({ page }) => {
  await seedRepos(m8Client(), [{ key: REPO }])
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', roleFixturesFromEnv().admin.name)
  await page.fill('[data-testid="login-password"]', roleFixturesFromEnv().admin.password)
  await page.click('[data-testid="login-submit"]')

  // 登录落点 = /artifacts（T-492 B-3.2：随即自动选中首仓库——前缀断言）
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts(\/|$)/)
  const nav = page.locator('[data-testid="app-nav"]')

  // 四分组齐见（分组即模式——admin 全景一屏）
  for (const g of GROUPS) {
    await expect(nav.locator('.nav-group-label', { hasText: g })).toBeVisible()
  }
  // 25 条目（核心 4 + 运营 5 + 安全 7 + 管理 9——nav-model 全表）
  await expect(nav.locator('a.nav-item')).toHaveCount(25)
  // 一级条目图标（T-388 N2/V5 承接）：25/25 在场；分组标签不配（档位不变）
  await expect(nav.locator('a.nav-item [data-testid="nav-icon"]')).toHaveCount(25)
  await expect(nav.locator('.nav-group-label [data-testid="nav-icon"]')).toHaveCount(0)

  // 键盘驱动首条目：focus 仪表盘 → Enter 落 /dashboard
  await page.focus('a.nav-item:text-is("仪表盘")')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/dashboard$/)
  await expect(page.locator('[data-testid="dashboard"]')).toBeVisible()

  // 管理条目逐项可达（URL 落 /admin/** + 页面锚到达〔settings 系双锚族〕）
  for (const [label, anchor] of ADMIN_ENTRIES) {
    await page.click(`[data-testid="app-nav"] a.nav-item:text-is("${label}")`)
    await expect(page).toHaveURL(/\/binflow\/ui\/admin\//)
    await expect(page.locator(`[data-testid="${anchor}"]`).first()).toBeVisible()
  }
  // 面包屑（§1.3 承接：管理页层级表达）
  await page.click(`[data-testid="app-nav"] a.nav-item:text-is("仓库")`)
  await expect(page.locator('[data-testid="topbar-breadcrumb"]')).toContainText('仓库')
})

test('readonly_admin: four groups visible, readonly badge, no quick-create write entries', async ({
  page,
}) => {
  await loginAs(page, 'readonly_admin') // 内含只读徽章断言

  // 管理面深链 + 分组可见（读姿态——分组即模式，无切换概念）
  await page.goto('/binflow/ui/admin/governance/audit')
  await expect(page.locator('[data-testid="audit-page"]')).toBeVisible()
  const nav = page.locator('[data-testid="app-nav"]')
  for (const g of GROUPS) {
    await expect(nav.locator('.nav-group-label', { hasText: g })).toBeVisible()
  }

  // 用户菜单（§2.3）：readonly_admin 不见快速建仓/新建写入口（L4 预收敛）
  await page.click('[data-testid="session-toggle"]')
  await expect(page.locator('[data-testid="quick-set-me-up"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="quick-new-user"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="quick-new-perm"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="menu-edit-profile"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="menu-edit-profile"]')).toHaveCount(0)
})

test('plain user: only all-visible entries; /admin/** deep link keeps shell + L2 convergence', async ({
  page,
}) => {
  await loginAs(page, 'user')

  // 全可见条目 5 项（核心 3 + 运营 2）；管理/安全分组整组不渲染（L1）
  const nav = page.locator('[data-testid="app-nav"]')
  await expect(nav.locator('a.nav-item')).toHaveCount(5)
  for (const label of ALL_ENTRIES) {
    await expect(nav.locator(`a.nav-item:text-is("${label}")`)).toBeVisible()
  }
  for (const label of ['仓库', '复制', 'Webhooks', '回收站', '用户', '组', '权限', '配额', '设置']) {
    await expect(nav.locator(`a.nav-item:text-is("${label}")`)).toHaveCount(0)
  }
  await expect(nav.locator('.nav-group-label', { hasText: '安全' })).toHaveCount(0)
  await expect(nav.locator('.nav-group-label', { hasText: '管理' })).toHaveCount(0)

  // /admin/** 直链：页面自身 403 收敛（§2.2 L2）；壳保留
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-page"] [data-testid="empty-state"]')).toBeVisible()

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
