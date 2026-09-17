import { test, expect } from '@playwright/test'

// T-146 QA: 文档中心 (剧本 4) — Playwright tests
// G18: offline availability, G19: completeness, G19b: help entry
//
// The server must be running with the docs face embedded (the committed
// internal/docs/dist tree — L024-2 posture; `make docs` is deprecated and
// NOT required).
// Target: http://localhost:18180 (baseURL from PLAYWRIGHT_BASE env or default).
//
// test.beforeAll is intentionally skipped — this spec runs against an
// already-running instance (started by the QA script).

// T-264 (FR-82-AC3)：缺省口令回退标准 scratch 口令，裸全量单命令可跑；
// docs QA 专用实例仍以 env 覆盖（ADMIN_PW=docsqa-test-pw-146 优先于缺省）。
const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

// L025-2：docs 嵌入面双态。CI 自 L024-2 起不建 docs 站（docs-site 退役，
// 二进制嵌入提交的 placeholder 壳——与本地 `make build` 同形）；本地跑过
// `make docs` 的构建则嵌真站。内容腿（导航/搜索/深链 200）只在真站态有意义，
// placeholder 态如实 skip 留痕；结构腿（段挂载 200 / 301 / 段外 404 / 离线
// 纯净）双态都成立，不 skip。
const PLACEHOLDER_MARK = '本构建未包含文档站产物'
let docsBuilt = true
test.beforeEach(async ({ request }) => {
  const resp = await request.get('/binflow/docs/')
  docsBuilt = resp.ok() && !(await resp.text()).includes(PLACEHOLDER_MARK)
})

async function login(page: import('@playwright/test').Page) {
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

// ---------------------------------------------------------------------------
// G18: 离线可用性
// ---------------------------------------------------------------------------

test('G18-1: docs home page returns 200 with BinFlow branding', async ({ page }) => {
  test.skip(!docsBuilt, 'placeholder docs embed (L024-2 posture) — content legs need a make docs build')
  await page.goto('/binflow/docs/')
  await expect(page).toHaveTitle(/BinFlow/)
  // The page should contain the sidebar with navigation links
  await expect(page.locator('nav.menu')).toBeVisible()
})

test('G18-2: 301 redirect from /binflow/docs to /binflow/docs/', async ({ request }) => {
  const resp = await request.get('/binflow/docs', { maxRedirects: 0 })
  expect(resp.status()).toBe(301)
  expect(resp.headers()['location']).toContain('/binflow/docs/')
})

test('G18-3: no external host references in page source (offline-ready)', async ({ page }) => {
  await page.goto('/binflow/docs/')
  // Docusaurus 路由未匹配的过渡兜底视图内含 docusaurus.io 外链（自定义 404
  // 是代码分割 chunk，并发争用下加载可滞后）——一次性 content() 采样会捕到
  // 该瞬态（T-268 轮 A 实测 647ms 误红）。先等兜底外链清零再取样。
  await expect(page.locator('a[href*="docusaurus.io"]')).toHaveCount(0, { timeout: 15_000 })
  const html = await page.content()
  // All script and link src/href should be relative or point to /binflow/docs/
  const externalRefs = html.match(/(?:src|href)="https?:\/\/(?!localhost|127\.0\.0\.1)[^"]+"/g)
  expect(externalRefs).toBeNull()
})

test('G18-4: search bar is present and functional', async ({ page }) => {
  test.skip(!docsBuilt, 'placeholder docs embed (L024-2 posture) — content legs need a make docs build')
  await page.goto('/binflow/docs/')
  // The search input should be visible
  await expect(page.locator('input.navbar__search-input')).toBeVisible()
  // Type a search term and verify it sticks. Docusaurus hydration can replace
  // the input AFTER the fill under CPU contention (default parallelism, T-268:
  // value observed wiped to "" in 2/2 rounds) — a fixed sleep + one-shot read
  // races it. Re-fill until the value survives a settling window; the
  // assertion itself is unchanged.
  const input = page.locator('input.navbar__search-input')
  for (let attempt = 0; attempt < 5; attempt++) {
    await input.fill('docker')
    try {
      await expect(input).toHaveValue('docker', { timeout: 3_000 })
      break
    } catch {
      // hydration reset the field — refill and retry
    }
  }
  await expect(input).toHaveValue('docker')
})

test('G18-5: all navigation links are clickable without 404', async ({ page }) => {
  test.skip(!docsBuilt, 'placeholder docs embed (L024-2 posture) — content legs need a make docs build')
  // Navigate to each major page and verify 200
  const pages = [
    { path: '/binflow/docs/', label: '文档中心首页' },
    { path: '/binflow/docs/docker-registry', label: 'Docker接入' },
    { path: '/binflow/docs/integrations/maven', label: 'Maven接入' },
    { path: '/binflow/docs/integrations/npm', label: 'npm接入' },
    { path: '/binflow/docs/integrations/pypi', label: 'PyPI接入' },
    { path: '/binflow/docs/integrations/generic', label: 'Generic接入' },
    { path: '/binflow/docs/console', label: '控制台指南' },
    { path: '/binflow/docs/admin/remote-virtual', label: 'Remote/Virtual' },
    { path: '/binflow/docs/admin/groups-permissions', label: '权限管理' },
    { path: '/binflow/docs/admin/governance', label: '治理' },
    { path: '/binflow/docs/admin/backup-restore', label: '备份恢复' },
    { path: '/binflow/docs/faq', label: 'FAQ' },
    { path: '/binflow/docs/api-reference', label: 'API参考' },
  ]

  for (const { path, label } of pages) {
    const resp = await page.goto(path)
    expect(resp?.status(), `Page ${label} (${path}) should return 200`).toBe(200)
  }
})

// ---------------------------------------------------------------------------
// G19: 安装页面完整性
// ---------------------------------------------------------------------------

test('G19-1: all install pages accessible', async ({ page }) => {
  test.skip(!docsBuilt, 'placeholder docs embed (L024-2 posture) — content legs need a make docs build')
  const installPages = [
    '/binflow/docs/install/binary',
    '/binflow/docs/install/docker',
    '/binflow/docs/install/compose',
    '/binflow/docs/install/helm',
    '/binflow/docs/install/k8s',
    '/binflow/docs/install/systemd',
    '/binflow/docs/install/offline',
    '/binflow/docs/install/upgrade',
  ]

  for (const path of installPages) {
    const resp = await page.goto(path)
    expect(resp?.status(), `Install page ${path} should return 200`).toBe(200)
  }
})

// ---------------------------------------------------------------------------
// G19b: 帮助入口
// ---------------------------------------------------------------------------

test('G19b-1: console help dropdown Documentation item points to /binflow/docs/', async ({ page }) => {
  // Navigate to the console shell — the SPA will redirect to /login
  await page.goto('/binflow/ui/')
  // Login first (the help dropdown is in the AppShell, rendered after auth)
  await login(page)
  // T-457（B-2.17 翻正）：帮助升格 ? 下拉——Documentation 项承接原外链
  const helpToggle = page.locator('[data-testid="topbar-help"]')
  await expect(helpToggle).toBeVisible()
  await expect(helpToggle).toContainText('帮助')
  await helpToggle.click()
  const helpDocs = page.locator('[data-testid="help-docs"]')
  await expect(helpDocs).toBeVisible()
  await expect(helpDocs).toHaveAttribute('href', '/binflow/docs/')
  await expect(helpDocs).toHaveAttribute('target', '_blank')
  await page.keyboard.press('Escape')
})

// ---------------------------------------------------------------------------
// G22: 404 page works
// ---------------------------------------------------------------------------

test('G22: 404 page renders for non-existent pages', async ({ page }) => {
  const resp = await page.goto('/binflow/docs/definitely-not-a-real-page')
  expect(resp?.status()).toBe(404)
  // The page should still render the Docusaurus 404 page with some content
  await expect(page.locator('body')).not.toBeEmpty()
})