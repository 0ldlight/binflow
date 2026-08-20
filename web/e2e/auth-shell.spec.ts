import { expect, test } from '@playwright/test'

// T-98 探针：登录 → 框架壳 → 仪表盘 → 主题 → 404 → 登出（服务端吊销）
// 全流程（派单要求；正式 W 序列浏览器矩阵归 T-104）。
//
// 运行前提：已 `make console && make build` 的真二进制在前台 serve，
// BASE 指向它（同 T-91 的探针约定）；ADMIN_PW 默认 password（PRD §4）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: import('@playwright/test').Page) {
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

test('unauthenticated visit is redirected to /login with return', async ({ page }) => {
  await page.goto('/binflow/ui/')
  await expect(page).toHaveURL(/\/binflow\/ui\/login\?return=%2F$/)
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
})

test('wrong credentials show inline error and stay on /login', async ({ page }) => {
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', 'definitely-wrong')
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="login-error"]')).toHaveText('用户名或密码错误')
  await expect(page).toHaveURL(/\/login/)
})

test('login lands on shell; dashboard cards arrive; theme toggles; 404 keeps shell', async ({ page }) => {
  await page.goto('/binflow/ui/')
  await login(page)

  // 壳：会话指示 + 版本探测（/api/system/version 开放端点）
  await expect(page.locator('[data-testid="session-user"]')).toHaveText(ADMIN)
  await expect(page.locator('[data-testid="nav-version"]')).toContainText(/v.+/)

  // 导航占位：仓库/搜索禁用态呈现，仪表盘/设置可点
  await expect(page.locator('.app-nav .nav-item.disabled').first()).toBeVisible()
  await expect(page.locator('.app-nav .nav-item.disabled')).toHaveCount(9)

  // 仪表盘卡片独立到达（admin 登录下四张管理面卡都在）
  await expect(page.locator('[data-testid="dashboard-instance-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-health-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-storage-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-repos-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-audit-card"]')).toBeVisible()
  // 健康卡内容到达（而非骨架/错误态；registry 探针依装配可能 error）
  await expect(page.locator('[data-testid="dashboard-health-card"] .stat-row .v')).toHaveText(/^(ok|error)$/)

  // 占位页深链：/repositories 保持壳并标注交付票号
  await page.goto('/binflow/ui/repositories')
  await expect(page.locator('[data-testid="placeholder-page"]')).toContainText('T-99')

  // 主题切换：暗 → 亮 → 暗（token 零分叉，data-theme 属性切换）
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

  // 404：保留壳 + 返回链接
  await page.goto('/binflow/ui/definitely-not-a-route')
  await expect(page.locator('[data-testid="not-found"]')).toBeVisible()
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
})

test('logout revokes the session server-side and re-entry requires login', async ({ page }) => {
  await page.goto('/binflow/ui/')
  await login(page)

  await page.click('[data-testid="session-toggle"]')
  await page.click('[data-testid="logout-button"]')
  // 危险确认对话框（焦点陷阱 + Esc 可取消）：先 Esc 取消，再真正登出
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  // N2：modal 打开时全局快捷键让位——背景路由不得被换走
  await page.keyboard.press('/')
  await expect(page).not.toHaveURL(/search/)
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()

  await page.click('[data-testid="session-toggle"]')
  await page.click('[data-testid="logout-button"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page).toHaveURL(/\/login/)

  // 会话已吊销：回退受保护路由再次被守卫拦下
  await page.goto('/binflow/ui/settings')
  await expect(page).toHaveURL(/\/login\?return=/)
})

test('settings password change surfaces server plain-text wording inline', async ({ page }) => {
  // B3：改密错误分支——服务端纯文本层文案（错旧口令 = 400 非信封）
  // 必须行内原样呈现，不进 toast、不触发 401 全局处理
  await page.goto('/binflow/ui/settings')
  await login(page) // 守卫先拦到 /login?return=%2Fsettings，登录后回设置页
  await page.waitForSelector('[data-testid="password-old"]')

  await page.fill('[data-testid="password-old"]', 'definitely-wrong')
  await page.fill('[data-testid="password-new"]', 'new-password-1')
  await page.fill('[data-testid="password-confirm"]', 'new-password-1')
  await page.click('[data-testid="password-submit"]')

  await expect(page.locator('[data-testid="password-error"]')).toContainText('Incorrect username/password')
  await expect(page.locator('[data-testid="toast"]')).toHaveCount(0)
  await expect(page).toHaveURL(/settings/) // 行内错误，不跳转
})
