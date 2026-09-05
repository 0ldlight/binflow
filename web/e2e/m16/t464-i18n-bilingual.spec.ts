import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-464（FR-149.2/.3/.4——E6 翻案收口，断言反转⑥归一）：en 双资源包填充 +
// 语言切换器 + 断言双语化。五腿：
//
//   ① 切换器往返（侧栏脚 nav-locale 族）：zh 默认 → English → 整页 reload
//      → en 态 → reload 持久 → 中文 → 回 zh。当前 locale = ToggleButton
//      aria-pressed 指示。
//   ② en 抽样腿（AC1 ≥6 页面：树/表单/详情/搜索/安全/监控/治理）——en 值
//      真渲染（非骨架回落）；zh 全量 spec 零翻新（断言反转⑥：默认 locale
//      断言维持既有 spec，本 spec 只加 en 态）。
//   ③ 日期 locale 化（FR-149.4——B-3.15 的 en 变体）：搜索结果表 modified
//      = `MMM d, yyyy h:mm:ss AM/PM +ZZZZ`（zh 形 dd-MM-yy 由 t449 既有腿
//      钉死，此处不重复）；审计时间列 12 小时制同场断言。
//   ④ 术语两包保真（FR-149.2）：Set Me Up / Deploy 等英文术语在 zh 包原样
//      呈现（AC1 术语一致性断言），en 包同名同形。
//   ⑤ axe 双 locale（AC3）：en 态两页 + zh 态一页 serious/critical=0（zh
//      全路由双主题全量腿在 m8/a11y-sweep，此处是双 locale 抽样对）。
//
// 锚源：console-ux §10.5 T-464 批（nav-locale / nav-locale-zh /
// nav-locale-en，v1.46）；既有锚零改名（tree-page / form-section-general /
// repo-tab-summary / search-grid / app-nav 等原样消费）。

const LOCALE_KEY = 'binflow-console-locale'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 本 spec 夹具：独立 generic local 仓 + 一个制品（供详情/搜索腿消费） */
async function seedRepoWithArtifact(): Promise<string> {
  const repo = uniq('t464')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${repo}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  const put = await client.request('PUT', `/binflow${repo}/hello.txt`, {
    raw: true,
    headers: { 'Content-Type': 'application/octet-stream' },
    body: 't464 en catalog fixture',
  })
  expect(put.status).toBeLessThan(300)
  return repo
}

/** en 态引导：一次性落键（sessionStorage 旗标防 addInitScript 在后续
 *  reload 重放把测试内手动改写的 locale 覆写回 en——t463 spec 同款） */
async function bootEn(page: import('@playwright/test').Page) {
  await page.addInitScript(
    ([k, m]) => {
      if (!sessionStorage.getItem(m)) {
        sessionStorage.setItem(m, '1')
        localStorage.setItem(k, 'en')
      }
    },
    [LOCALE_KEY, 't464-locale-seeded'],
  )
}

test('switcher roundtrip: zh → en → zh via sidebar footer, reload persists', async ({ page }) => {
  await loginAs(page, 'admin') // zh 默认（净会话无键）

  // 切换器在场（侧栏脚注）+ 当前档指示（ToggleButton aria-pressed）
  await expect(page.locator('[data-testid="nav-locale"]')).toBeVisible()
  await expect(page.locator('[data-testid="nav-locale-zh"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-testid="nav-locale-en"]')).toHaveAttribute('aria-pressed', 'false')
  // zh 态文案（应用侧栏仪表盘项）
  await expect(page.locator('[data-testid="app-nav"]').getByText('仪表盘')).toBeVisible()

  // 切 en：setLocale = 持久化 + 整页 reload——断言在 reload 后的帧上成立
  await page.click('[data-testid="nav-locale-en"]')
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(page.locator('[data-testid="nav-locale-en"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-testid="app-nav"]').getByText('Dashboard')).toBeVisible()
  // reload 持久：仍是 en（localStorage 引导）
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(page.locator('[data-testid="app-nav"]').getByText('Dashboard')).toBeVisible()

  // 往返回 zh：文案翻转回来 + 档位指示复位
  await page.click('[data-testid="nav-locale-zh"]')
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.locator('[data-testid="nav-locale-zh"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-testid="app-nav"]').getByText('仪表盘')).toBeVisible()
  await expect(page.locator('[data-testid="app-nav"]').getByText('Dashboard')).toHaveCount(0)
})

test('en sampling: tree / form / detail / search / security / monitoring / governance', async ({ page }) => {
  await bootEn(page)
  await loginAs(page, 'admin')

  // ① 制品树（/artifacts）：跨仓树 aria-label 翻转
  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-page"] [role="tree"]')).toHaveAttribute(
    'aria-label',
    'Cross-repository artifact tree',
  )

  // ② 建仓表单（/admin/repositories/local/new）：先包类型选择步（其标题与
  // 选项文案即 en 抽样面），选 Generic 后分节标题翻转
  await page.goto('/binflow/ui/admin/repositories/local/new')
  await expect(page.getByRole('heading', { name: 'Choose a package type' })).toBeVisible()
  await page.getByRole('radio', { name: /Any file/ }).click()
  await expect(page.getByRole('heading', { name: 'General Settings' })).toBeVisible()
  await expect(page.locator('[data-testid="form-section-general"]')).toBeVisible()

  // ③ 仓库详情：Tab 标签翻转（概要 → Overview）
  const repo = await seedRepoWithArtifact()
  await page.goto(`/binflow/ui/admin/repositories/${repo}`)
  await expect(page.getByRole('tab', { name: 'Overview' })).toBeVisible()

  // ④ 搜索空结果：文案翻转（键 {q} 插值）
  await page.goto('/binflow/ui/search?q=t464-no-such-artifact')
  await expect(page.getByText('No artifacts match')).toBeVisible()

  // ⑤ 安全（用户管理 h2）
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()

  // ⑥ 监控（服务状态）
  await page.goto('/binflow/ui/admin/monitoring/status')
  await expect(page.getByText('Overall status')).toBeVisible()

  // ⑦ 治理（审计日志 h2 + en 时间形——12 小时制，腿③同场）
  await page.goto('/binflow/ui/admin/governance/audit')
  await expect(page.getByRole('heading', { name: 'Audit Log' })).toBeVisible()
  // admin 登录即产生 login.success 事件；时间列 en 形 = 当日 h:mm:ss AM/PM
  // 或跨天 MMM d, yyyy h:mm:ss AM/PM（formatAuditTime en 变体）
  await expect
    .poll(async () => (await page.locator('.audit-time').first().textContent())?.trim() ?? '')
    .toMatch(/^(\d{1,2}:\d{2}:\d{2} (AM|PM)|[A-Z][a-z]{2} \d{1,2}, \d{4} \d{1,2}:\d{2}:\d{2} (AM|PM))$/)
})

test('en date format: search results modified = MMM d, yyyy h:mm:ss AM/PM +ZZZZ', async ({ page }) => {
  await bootEn(page)
  await loginAs(page, 'admin')
  await seedRepoWithArtifact()
  await page.goto(`/binflow/ui/search?q=hello.txt`)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible()
  // FR-144.6 的 en 变体（FR-149.4）：同款本地时区 + 显式偏移，12 小时制——
  // 定位到 modified 单元格（含 AM/PM + 偏移特征）后整格正则断言
  const stampCell = page.locator('[data-testid="search-result-0"] td').filter({ hasText: /(AM|PM) [+-]\d{4}/ })
  await expect(stampCell).toHaveCount(1)
  await expect(stampCell).toHaveText(/^[A-Z][a-z]{2} \d{1,2}, \d{4} \d{1,2}:\d{2}:\d{2} (AM|PM) [+-]\d{4}$/)
  // 顺带核 zh 形锚不被 en 污染：同格不得再是 dd-MM-yy 24h 形
  await expect(stampCell).not.toHaveText(/^\d{2}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{4}$/)
})

test('terminology parity: Set Me Up / Deploy verbatim in zh and en catalogs', async ({ page }) => {
  // zh 包：英文术语原样呈现（FR-149.2——E6 条款「术语两包保真」）
  const repo = await seedRepoWithArtifact()
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${repo}`)
  await expect(page.getByRole('button', { name: 'Set Me Up' })).toBeVisible()
  // zh 态的部署入口带「部署 Deploy」双语文案——Deploy 术语原样在场
  await expect(page.locator('main').getByText('部署 Deploy')).toBeVisible()

  // en 包：同术语同名同形（不翻成 Set me up! 之类）
  await page.evaluate((k) => localStorage.setItem(k, 'en'), LOCALE_KEY)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
  await expect(page.getByRole('button', { name: 'Set Me Up' })).toBeVisible()
  await expect(page.locator('main').getByText('⬆ Deploy')).toBeVisible()
})

test('axe dual locale: en two pages + zh one page, serious/critical = 0', async ({ page }, testInfo) => {
  await bootEn(page)
  await loginAs(page, 'admin')

  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  await expectA11yClean(page, testInfo) // en · 树（含侧栏切换器）

  await page.goto('/binflow/ui/admin/monitoring/status')
  await expect(page.getByText('Overall status')).toBeVisible()
  await expectA11yClean(page, testInfo) // en · 服务状态

  // zh 抽样对（全路由双主题全量腿在 m8/a11y-sweep，此处一页成对即可）
  await page.evaluate((k) => localStorage.setItem(k, 'zh'), LOCALE_KEY)
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expect(page.getByText('总体状态')).toBeVisible()
  await expectA11yClean(page, testInfo)
})
