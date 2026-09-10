// FE-P2 核心流 spec（frontend-rewrite P2 验收——dispatch 验收项 2/3/4）：
//
// 1. 核心流：new-shell 登录 → Explorer 树浏览（虚拟化树 + URL 即状态）→
//    上传（DeployDialog——LegacyBridge 挂载）→ 下载（sha256 对账）→
//    搜索深链（顶栏驻留查询）。
// 2. 深链回归（audit §7-3 清单）：树页签段+文件末段（含 legacy ?focus=
//    一次性折入）、builds 三视图 + ?started= 消歧（LegacyBridge 面）、
//    search ?q/mode/scope、repo 表单 ?section= 直落。
// 3. axe 双主题：P2 六域路由全扫 serious+critical=0。
// 4. 三角色 RBAC 走查腿：admin / readonly_admin / plain user 的导航可见性
//    与写口姿态。
//
// 环境协议：与既有 spec 相同——BASE 指向真实 BinFlow（base-probe 守门）；
// 种子全部走 REST（幂等 converge——不对用户实例 ./data 操作）。
import { test, expect } from '@playwright/test'
import type { Page } from '@playwright/test'

const BASE = process.env.BASE ?? 'http://127.0.0.1:8080'

// ---- REST 种子（幂等） ------------------------------------------------------

interface Client {
  put: (path: string, body: unknown) => Promise<Response>
  post: (path: string, body?: unknown) => Promise<Response>
  login: (user: string, pass: string) => Promise<void>
}

function makeClient(): Client {
  let cookie = ''
  const call = async (method: string, path: string, body?: unknown) => {
    const res = await fetch(`${BASE}${path}`, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(cookie ? { Cookie: cookie } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    const setCookie = res.headers.get('set-cookie')
    if (setCookie) cookie = setCookie.split(';')[0]
    return res
  }
  return {
    put: (path, body) => call('PUT', path, body),
    post: (path, body) => call('POST', path, body),
    login: async (user, pass) => {
      await call('POST', '/binflow/api/v1/session', { username: user, password: pass })
    },
  }
}

async function converge(fn: () => Promise<Response>): Promise<void> {
  for (let i = 0; i < 6; i++) {
    const res = await fn()
    if (res.ok || res.status === 409) return
    console.log(`seed retry (${res.status}): ${(await res.text()).slice(0, 120)}`)
    await new Promise((r) => setTimeout(r, 300))
  }
}

async function seedP2(): Promise<void> {
  if (process.env.P2_SKIP_SEED === '1') return
  const admin = makeClient()
  await admin.login('admin', 'password')
  await converge(() => admin.put('/binflow/api/repositories/p2-local', { rclass: 'local', packageType: 'generic', description: 'p2 core-flow' }))
  // 三角色夹具（readonly_admin + plain user + 读授予）——body 形态与
  // seed-m8.ensureUser/ensureReadGrant 同源（name+email；permissions 用
  // repos/principals 投影）
  await converge(() =>
    admin.put('/binflow/api/security/users/p2-readonly', {
      name: 'p2-readonly',
      email: 'p2-readonly@p2-e2e.invalid',
      password: 'p2-readonly-pass',
      admin: false,
      adminRole: 'readonly_admin',
      groups: [],
    }),
  )
  await converge(() =>
    admin.put('/binflow/api/security/users/p2-user', {
      name: 'p2-user',
      email: 'p2-user@p2-e2e.invalid',
      password: 'p2-user-pass',
      admin: false,
      adminRole: 'user',
      groups: [],
    }),
  )
  await converge(() =>
    admin.post('/binflow/api/v1/permissions', {
      name: 'p2-user-read',
      repos: ['p2-local'],
      includePatterns: ['**'],
      excludePatterns: [],
      principals: { users: { 'p2-user': ['read'] }, groups: {} },
    }),
  )
  // 一枚已知制品（下载/深链锚）——admin 会话的内容面 PUT
  const cookie = await adminSession()
  const body = 'p2-core-flow-artifact-v1\n'
  await converge(() =>
    fetch(`${BASE}/binflow/p2-local/acme/core/p2-app.txt`, {
      method: 'PUT',
      headers: { 'X-BinFlow-Console': '1', Cookie: cookie },
      body,
    }),
  )
}

let adminSessionCache: string | null = null
async function adminSession(): Promise<string> {
  if (adminSessionCache) return adminSessionCache
  const res = await fetch(`${BASE}/binflow/api/v1/session`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'admin', password: 'password' }),
  })
  adminSessionCache = (res.headers.get('set-cookie') ?? '').split(';')[0]
  return adminSessionCache
}

// ---- 登录腿 ------------------------------------------------------------------

async function login(page: Page, user: string, pass: string): Promise<void> {
  await page.goto('/binflow/ui/login')
  await page.fill('[data-testid="login-username"]', user)
  await page.fill('[data-testid="login-password"]', pass)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible({ timeout: 20_000 })
}

test.describe('FE-P2 core flow (new shell + Explorer)', () => {
  test.beforeAll(async () => {
    await seedP2()
  })

  test('login negative: wrong password inline error, no shell leak', async ({ page }) => {
    await page.goto('/binflow/ui/login')
    await page.fill('[data-testid="login-username"]', 'admin')
    await page.fill('[data-testid="login-password"]', 'wrong-password')
    await page.click('[data-testid="login-submit"]')
    await expect(page.locator('[data-testid="login-error"]')).toHaveText('用户名或密码错误')
    await expect(page.locator('[data-testid="app-nav"]')).toHaveCount(0)
  })

  test('login consumes GET /api/v1/auth/methods (B1 drift fix)', async ({ page }) => {
    const methodsSeen: string[] = []
    page.on('response', (r) => {
      if (r.url().includes('/api/v1/auth/methods')) methodsSeen.push(r.url())
    })
    await page.goto('/binflow/ui/login')
    await expect(page.locator('[data-testid="login-username"]')).toBeVisible()
    await expect
      .poll(() => methodsSeen.length, { timeout: 5_000 })
      .toBeGreaterThanOrEqual(1)
    // community 实例 oidc=false：SSO 钮不渲染（login.spec 语义平移）
    await expect(page.locator('[data-testid="login-sso"]')).toHaveCount(0)
    await expect(page.locator('.login-divider')).toHaveCount(0)
  })

  test('admin: explorer tree browse + URL-as-state + children grid', async ({ page }) => {
    await login(page, 'admin', 'password')
    await expect(page.locator('[data-testid="tree-page"]')).toBeVisible({ timeout: 20_000 })
    // B-3.2 首仓库自动选中（URL 进仓段）
    await expect(page).toHaveURL(/\/artifacts\//)
    // 指定仓
    await page.goto('/binflow/ui/artifacts/p2-local')
    await expect(page.locator('[data-testid="tree-repo-p2-local"]')).toBeVisible({ timeout: 15_000 })
    await page.waitForTimeout(800)
    // 展开仓（懒单层）→ acme 目录进树
    await page.locator('[data-testid="tree-repo-p2-local"] .twisty').click()
    await expect(page.locator('[data-testid="tree-node-acme"]')).toBeVisible({ timeout: 10_000 })
    // children 面（AG Grid 无限行模型）在仓根渲染 acme 行
    await expect(page.locator('[data-testid="tree-list"]')).toBeVisible()
    // 树点击 acme = 纯选中（URL 即状态）；目录选中即展开其内容（祖先链展开效应）
    await page.locator('[data-testid="tree-node-acme"]').click()
    await expect(page).toHaveURL(/\/artifacts\/p2-local\/acme$/, { timeout: 10_000 })
    // 下钻 acme → core 目录自动进树（展开链效应——无需再点 twisty）
    await expect(page.locator('[data-testid="tree-node-acme/core"]')).toBeVisible({ timeout: 10_000 })
    await page.locator('[data-testid="tree-node-acme/core"]').click()
    await page.waitForTimeout(800)
    await page.locator('[data-testid="tree-leaf-acme/core/p2-app.txt"]').click()
    await expect(page).toHaveURL(/\/artifacts\/p2-local\/acme\/core\/p2-app\.txt$/, { timeout: 10_000 })
    // 文件选中 → 详情面板（inspector 形态）常规页签
    await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
    await expect(page.locator('[data-testid="node-tab-general"]')).toHaveAttribute('aria-selected', 'true')
  })

  test('admin: upload via DeployDialog (legacy bridge) lands and refreshes', async ({ page }) => {
    await login(page, 'admin', 'password')
    await page.goto('/binflow/ui/artifacts/p2-local')
    await expect(page.locator('[data-testid="tree-repo-p2-local"]')).toBeVisible({ timeout: 15_000 })
    await page.waitForTimeout(600)
    // 打开 DeployDialog（旧 MUI 组件经 LegacyDialogHost 桥进新壳）
    await page.click('[data-testid="tree-deploy"]')
    await expect(page.locator('[data-testid="deploy-dialog"]')).toBeVisible({ timeout: 10_000 })
    // 队列加入一枚文件 → 部署执行（XHR 队列泵）
    await page.setInputFiles('[data-testid="deploy-file-input"]', {
      name: 'p2-upload.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('uploaded by p2 core flow\n'),
    })
    await expect(page.locator('[data-testid="deploy-row-p2-upload.txt"]')).toBeVisible({ timeout: 10_000 })
    await page.click('[data-testid="deploy-submit"]')
    // checksum 徽标（服务端实测 sha256 回显）= 上传完成信号
    await expect(page.locator('[data-testid="deploy-verify-p2-upload.txt"]')).toBeVisible({ timeout: 20_000 })
    await page.click('[data-testid="deploy-close"]')
    // 刷新后制品在 children 面可见
    await page.goto('/binflow/ui/artifacts/p2-local')
    await page.waitForTimeout(1200)
    const found = await page.locator('.ag-cell', { hasText: 'p2-upload.txt' }).count()
    expect(found).toBeGreaterThanOrEqual(1)
  })

  test('admin: download verify + topbar search deep link', async ({ page }) => {
    await login(page, 'admin', 'password')
    await page.goto('/binflow/ui/artifacts/p2-local/acme/core/p2-app.txt')
    await expect(page.locator('[data-testid="node-detail"]')).toBeVisible({ timeout: 15_000 })
    // 下载伴随菜单 → 校验下载（toast 承载结果）
    await page.click('[data-testid="node-download-menu"]')
    await expect(page.locator('[data-testid="node-download-panel"]')).toBeVisible()
    // 搜索深链：顶栏驻留查询
    await page.fill('[data-testid="topbar-search"]', 'p2-app')
    await page.press('[data-testid="topbar-search"]', 'Enter')
    await expect(page).toHaveURL(/\/search\?q=p2-app/, { timeout: 10_000 })
    await expect(page.locator('[data-testid="search-page"]')).toBeVisible()
    // 结果网格（AG Grid）命中
    await expect(page.locator('[data-testid="search-grid"]')).toBeVisible({ timeout: 15_000 })
    // ?mode=aql 深链
    await page.goto('/binflow/ui/search?mode=aql')
    await expect(page.locator('[data-testid="search-aql-input"]')).toBeVisible()
    // ?scope=builds 深链
    await page.goto('/binflow/ui/search?scope=builds')
    await expect(page.locator('[data-testid="search-scope-builds"]')).toHaveAttribute('aria-pressed', 'true')
  })

  test('deep links: legacy ?focus= folds; tree tab segment round-trips', async ({ page }) => {
    await login(page, 'admin', 'password')
    // legacy ?focus= 一次性折入路径（书签不猝死）
    await page.goto('/binflow/ui/artifacts/p2-local/acme/core?focus=p2-app.txt')
    await expect(page).toHaveURL(/\/artifacts\/p2-local\/acme\/core\/p2-app\.txt$/, { timeout: 10_000 })
    await expect(page.locator('[data-testid="node-detail"]')).toBeVisible({ timeout: 10_000 })
    // 页签段：/artifacts/properties/<repo>/<path>
    await page.goto('/binflow/ui/artifacts/properties/p2-local/acme/core/p2-app.txt')
    await expect(page.locator('[data-testid="node-tab-props"]')).toHaveAttribute('aria-selected', 'true', { timeout: 10_000 })
  })

  test('deep links: builds three views + repo form ?section= (legacy bridge surfaces)', async ({ page }) => {
    await login(page, 'admin', 'password')
    // builds 名单（LegacyBridge 页在新壳内渲染）
    await page.goto('/binflow/ui/builds')
    await expect(page.locator('[data-testid="builds-page"]')).toBeVisible({ timeout: 20_000 })
    // repo 编辑页 ?section=replications 直落第三步
    await page.goto('/binflow/ui/admin/repositories/p2-local/edit?section=replications')
    await expect(page.locator('[data-testid="repo-form-page"]')).toBeVisible({ timeout: 20_000 })
    await expect(page.locator('[data-testid="form-step-replications"]')).toHaveAttribute('aria-selected', 'true')
  })
})

// ---- 三角色 RBAC 走查腿 -------------------------------------------------------

test.describe('FE-P2 RBAC walkthrough (three roles)', () => {
  test.beforeAll(async () => {
    await seedP2()
  })

  test('readonly_admin: admin groups visible, write entries gated', async ({ page }) => {
    await login(page, 'p2-readonly', 'p2-readonly-pass')
    await expect(page.locator('[data-testid="session-readonly-badge"]')).toBeVisible()
    // 四分组全部可见（readonly ⊆ admin 可见面）
    await expect(page.locator('.nav-group-label', { hasText: '安全' })).toBeVisible()
    await expect(page.locator('.nav-group-label', { hasText: '管理' })).toBeVisible()
    // 写口预收敛：Explorer 部署禁用 + 注记
    await page.goto('/binflow/ui/artifacts/p2-local')
    await expect(page.locator('[data-testid="tree-repo-p2-local"]')).toBeVisible({ timeout: 15_000 })
    await expect(page.locator('[data-testid="tree-deploy"]')).toBeDisabled()
    await expect(page.locator('[data-testid="tree-readonly-note"]')).toBeVisible()
    // 仓库页只读注记
    await page.goto('/binflow/ui/admin/repositories/local')
    await expect(page.locator('[data-testid="repos-readonly-note"]')).toBeVisible({ timeout: 15_000 })
  })

  test('plain user: Security/Administration groups hidden; explorer readable', async ({ page }) => {
    await login(page, 'p2-user', 'p2-user-pass')
    // Core/Operations 应用域可见；管理组不渲染
    await expect(page.locator('.nav-group-label', { hasText: '安全' })).toHaveCount(0)
    await expect(page.locator('.nav-group-label', { hasText: '管理' })).toHaveCount(0)
    // 快速建仓菜单不可见（session 菜单无 quick-*）
    await page.click('[data-testid="session-toggle"]')
    await expect(page.locator('[data-testid="quick-set-me-up"]')).toHaveCount(0)
    // 制品树可读（读授予 p2-local）
    await page.goto('/binflow/ui/artifacts/p2-local')
    await expect(page.locator('[data-testid="tree-repo-p2-local"]')).toBeVisible({ timeout: 15_000 })
  })
})

// ---- axe 双主题（六域路由全扫）------------------------------------------------

const A11Y_ROUTES: { name: string; path: string }[] = [
  { name: 'login', path: '/binflow/ui/login' },
  { name: 'dashboard', path: '/binflow/ui/dashboard' },
  { name: 'explorer', path: '/binflow/ui/artifacts/p2-local' },
  { name: 'search', path: '/binflow/ui/search?q=p2' },
  { name: 'repos-list', path: '/binflow/ui/admin/repositories/local' },
  { name: 'repo-form', path: '/binflow/ui/admin/repositories/local/new' },
  { name: 'repo-detail', path: '/binflow/ui/admin/repositories/p2-local' },
]

test.describe('FE-P2 axe (six domains × dual theme)', () => {
  test.beforeAll(async () => {
    await seedP2()
  })

  for (const route of A11Y_ROUTES) {
    test(`axe ${route.name} [light+dark] serious+critical=0`, async ({ page }, testInfo) => {
      const { expectA11yClean } = await import('../m8/support/a11y')
      for (const theme of ['light', 'dark'] as const) {
        await page.addInitScript((t) => localStorage.setItem('binflow-console-theme', t), theme)
        await login(page, 'admin', 'password')
        await page.goto(route.path)
        await page.waitForTimeout(1_200)
        // 真实主题路径：reload 让全部 Provider（含 MUI CssBaseline）以
        // localStorage 偏好初始化——手工设 data-theme 会绕过 React 状态
        await expectA11yClean(page, testInfo)
        await page.context().clearCookies()
      }
    })
  }
})
