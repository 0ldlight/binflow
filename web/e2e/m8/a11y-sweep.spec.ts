import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { loginAs } from './support/roles'
import { m8Client, seedRepos } from './support/seed'
import { expectA11yClean } from './support/a11y'

// T-246 终验 a11y fix-forward 的复验面（T-244 续手）：控制台**全路由 × 双主题**
// axe 扫描——QA-1（repo 详情 cmd 块 <pre> 键盘可达）/ QA-2（field-hint 内
// 链接下划线）两修复后 serious/critical = 0 的一次性全量口径。此后该 spec
// 留驻作回归面（任一页面结构性 a11y 回归在此暴露）。
//
// 页面清单 = console-m8 §1.3 导航树全部 14 条目 + 编辑器/表单/404 深页，
// 共 24 路由 × {light, dark} = 48 扫描 + 登录页 2 扫描。主题经 localStorage
// 显式落盘（不依赖上一腿翻转残留）。

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

/** 登录页（唯一无需会话的页）——本测试不登录，独立上下文 */
test('a11y sweep: login page in both themes', async ({ page }, testInfo) => {
  for (const theme of ['light', 'dark'] as const) {
    await page.goto('/binflow/ui/login')
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/login')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="login-page"]' })
  }
})

test('a11y sweep: all console routes in both themes (serious/critical = 0)', async ({ page }, testInfo) => {
  test.setTimeout(600_000) // 54 面（27 路由 × 双主题——T-352 增回收站）导航+axe；串行态 ~3m，默认并发（T-268）实测 5.1m（超 300s）~8.2m（超 480s，机上有并行验证负载），抬到 10m
  const key = uniq('a11y')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/docs/guide.md`, { raw: true, body: 'a11y\n' })

  await loginAs(page, 'admin')

  const routes: { url: string; settle?: string }[] = [
    // 应用模式（console-m8 §1.3）
    { url: '/dashboard', settle: '[data-testid="dashboard"]' },
    { url: '/artifacts', settle: '[data-testid="tree-page"]' },
    { url: `/artifacts/${key}`, settle: '[data-testid="tree-list"]' },
    { url: `/artifacts/${key}/docs`, settle: '[data-testid="node-detail"]' },
    { url: '/search', settle: '[data-testid="search-page"]' },
    { url: '/profile', settle: '[data-testid="profile-page"]' },
    // 管理模式：仓库
    { url: '/admin/repositories/local', settle: '[data-testid="repos-page"]' },
    { url: '/admin/repositories/remote', settle: '[data-testid="repos-page"]' },
    { url: '/admin/repositories/virtual', settle: '[data-testid="repos-page"]' },
    { url: '/admin/repositories/new', settle: '[data-testid="pkg-grid"]' },
    { url: `/admin/repositories/${key}`, settle: '[data-testid="repo-detail-page"]' }, // QA-1 面：cmd 块 pre
    { url: `/admin/repositories/${key}/edit`, settle: '[data-testid="repo-form-page"]' },
    // 管理模式：用户与权限
    { url: '/admin/security/users', settle: '[data-testid="users-page"]' },
    { url: '/admin/security/users/m8-e2e-user', settle: '[data-testid="user-form"]' },
    { url: '/admin/security/groups', settle: '[data-testid="groups-page"]' },
    { url: '/admin/security/permissions', settle: '[data-testid="perms-page"]' },
    { url: '/admin/security/permissions/new', settle: '[data-testid="perm-editor-page"]' },
    { url: '/admin/security/tokens', settle: '[data-testid="placeholder-page"]' },
    // 管理模式：治理（GC = QA-2 面：field-hint 链接）
    { url: '/admin/governance/audit', settle: '[data-testid="audit-page"]' },
    { url: '/admin/governance/gc', settle: '[data-testid="gc-page"]' },
    { url: '/admin/governance/quotas', settle: '[data-testid="quotas-page"]' },
    { url: '/admin/governance/replication', settle: '[data-testid="repl-page"]' },
    { url: '/admin/governance/backup', settle: '[data-testid="backup-page"]' },
    // 回收站（M12 T-352；community 真栈 = trashcan 槽锁定卡形态）
    { url: '/admin/governance/trash', settle: '[data-testid="trash-page"]' },
    // 管理模式：监控 / 常规
    { url: '/admin/monitoring/storage', settle: '[data-testid="storage-page"]' },
    { url: '/admin/general/settings', settle: '[data-testid="settings"]' },
    // 404（保留导航壳）
    { url: '/no-such-route-for-a11y', settle: '[data-testid="not-found"]' },
  ]

  let scans = 0
  for (const theme of ['light', 'dark'] as const) {
    // 主题显式落盘（ThemeProvider 挂载时读取）
    await page.goto('/binflow/ui/artifacts')
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    for (const r of routes) {
      await scanRoute(page, testInfo, r, theme)
      scans++
    }
  }
  // 口径自证：27 路由 × 2 主题（登录页另有独立腿；T-352 起含回收站）
  expect(scans).toBe(routes.length * 2)
})

// ---- QA-1 / QA-2 修复的元素级钉腿（终验缺陷的直接证明，非仅全页扫过门） ----

test('a11y fix-forward proof: QA-1 pre keyboard-reachable, QA-2 hint link underlined', async ({ page }) => {
  const key = uniq('a11yfix')
  const client = m8Client()
  await seedRepos(client, [{ key }])

  await loginAs(page, 'admin')

  // QA-1：repo 详情接入命令块 <pre> 可聚焦（scrollable-region-focusable 的
  // 修复形态 = tabindex 0；焦点环由全局 :focus-visible 承载，色值不作断言）
  await page.goto(`/binflow/ui/admin/repositories/${key}`)
  await expect(page.locator('[data-testid="repo-commands"] pre').first()).toBeVisible()
  const pre = page.locator('[data-testid="repo-commands"] pre').first()
  await expect(pre).toHaveAttribute('tabindex', '0')
  await pre.focus()
  await expect(pre).toBeFocused()

  // QA-2：GC 页 field-hint 内「审计日志」链接恒下划线（不再仅靠颜色区分）
  await page.goto('/binflow/ui/admin/governance/gc')
  const link = page.locator('[data-testid="gc-page"] .field-hint a').first()
  await expect(link).toBeVisible()
  const line = await link.evaluate((el) => getComputedStyle(el).textDecorationLine)
  expect(line).toContain('underline')
})

async function scanRoute(
  page: Page,
  testInfo: TestInfo,
  route: { url: string; settle?: string },
  theme: 'light' | 'dark',
): Promise<void> {
  await page.goto(`/binflow/ui${route.url}`)
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  if (route.settle) {
    await expect(page.locator(route.settle).first()).toBeVisible({ timeout: 10_000 })
  }
  await page.waitForTimeout(250) // 卡片/表格独立到达
  // 逐路由 attach 报告（失败即读 —— route+theme 进 url 字段）
  await expectA11yClean(page, testInfo)
}
