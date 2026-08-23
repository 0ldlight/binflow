import { expect, test } from '@playwright/test'
import { existsSync } from 'node:fs'
import { join } from 'node:path'

// T-158: 登录页 SSO（OIDC）+ LDAP 透明登录——全 mock 探针（hermetic，同
// T-160 storage_migration.spec.ts 的模式）：
// - SPA 外壳与指纹资源由 dist/ 兜底（page.route fulfill；与 go:embed
//   消费的同一份构建产物，`npm run build` 后运行）；
// - /binflow/api/** 全量拦截（session / system/version / v1/oidc/login），
//   未覆盖端点一律 404——探针不依赖网络上的任何 BinFlow 实例。
// 覆盖：① oidc 404（未启用，FR-54-AC6/H29）→ SSO 按钮整体隐藏、密码
//   表单不受影响；② oidc 302（已启用）→ 按钮可见，点击发起顶层 GET
//   /binflow/api/v1/oidc/login 并离开 SPA（文档请求以 mock IdP 落地页
//   应答；真实后端的 302→IdP 契约由 T-157 TestOIDCLoginRedirectContract
//   固化，Chromium 不跟随 fulfill 合成的导航 302）；③ 点击时复核失败
//   （探测后被关 404 / 服务 5xx）→ 行内错误提示（SSO 四态的错误面）；
// ④ LDAP 用户走同一表单（AC② 零分支）：错误目录口令 401 行内红字 →
//   正确目录凭据 200 落地壳（非 admin 收敛视图）。

const DIST = join(process.cwd(), 'dist')

// mock IdP 落地页（302 Location 的目标形态；授权码流的真实参数由后端生成）
const IDP_AUTHORIZE =
  'https://idp.mock.test/authorize?client_id=binflow-console&response_type=code&scope=openid&code_challenge_method=S256'
const IDP_PAGE = '<!doctype html><h1>Mock IdP</h1>'

test.beforeEach(() => {
  test.skip(!existsSync(join(DIST, 'index.html')), 'console not built — run `npm run build` first')
})

/** 单次 /v1/oidc/login 应答：302（location 可缺省用 mock IdP）或 404/5xx */
interface OIDCReply {
  status: number
  location?: string
}

interface SessionReply {
  status: number
  body: unknown
}

interface MockOpts {
  /** POST /v1/session 应答（缺省恒 401 invalid credentials） */
  postSession?: (body: { username?: string; password?: string }) => SessionReply
  /** GET /v1/oidc/login 第 n 次（从 1 起）调用的应答（缺省恒 404 = 未启用） */
  oidc?: (call: number) => OIDCReply
}

/** 安装全部 mock；返回 { oidc 端点命中数, 其中顶层导航(document)次数 } */
async function installMocks(
  page: import('@playwright/test').Page,
  opts: MockOpts = {},
): Promise<{ oidcCalls: () => number; documentNavs: () => number }> {
  let oidcCalls = 0
  let documentNavs = 0

  // 兜底先注册（Playwright 后注册者优先）：未覆盖的 API 一律 404，
  // 防止任何请求漏到真实网络
  await page.route('**/binflow/api/**', (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ errors: [{ message: 'unmocked endpoint' }] }),
    }),
  )
  // 会话面：GET = whoami 探活（未登录 401）；POST = 登录提交
  await page.route('**/binflow/api/v1/session', async (route) => {
    if (route.request().method() === 'POST') {
      let body: { username?: string; password?: string } = {}
      try {
        body = route.request().postDataJSON() as { username?: string; password?: string }
      } catch {
        body = {}
      }
      const reply =
        opts.postSession?.(body) ?? { status: 401, body: { errors: [{ message: 'invalid credentials' }] } }
      return route.fulfill({ status: reply.status, contentType: 'application/json', body: JSON.stringify(reply.body) })
    }
    return route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({ errors: [{ message: 'unauthorized' }] }),
    })
  })
  await page.route('**/binflow/api/system/version', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ version: '6.0.0-t158', revision: 'e2e', product: 'BinFlow' }),
    }),
  )
  await page.route('**/binflow/api/v1/oidc/login*', (route) => {
    oidcCalls += 1
    const reply = opts.oidc?.(oidcCalls) ?? { status: 404 }
    // 点击后的顶层导航（document）：Chromium 不跟随 fulfill 合成的 302，
    // 302 场景直接以 mock IdP 落地页应答——断言点是「浏览器确实对 SSO
    // 入口发起了顶层 GET 并离开 SPA」。
    if (route.request().resourceType() === 'document' && reply.status === 302) {
      documentNavs += 1
      return route.fulfill({ status: 200, contentType: 'text/html', body: IDP_PAGE })
    }
    if (reply.status === 302) {
      return route.fulfill({ status: 302, headers: { location: reply.location ?? IDP_AUTHORIZE } })
    }
    return route.fulfill({
      status: reply.status,
      contentType: 'application/json',
      body: JSON.stringify({ errors: [{ message: reply.status === 404 ? 'not implemented' : 'oidc sign-in failed' }] }),
    })
  })
  // 仪表盘管理面四端点恒 403：非 admin 落地时卡片走「403 即隐藏」收敛
  //（useAsync 将 403 映射 forbidden），与本票登录流无关、仅为落地视图降噪
  const forbidden = (route: import('@playwright/test').Route) =>
    route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: 'forbidden' }] }) })
  await page.route('**/binflow/api/v1/health', forbidden)
  await page.route('**/binflow/api/v1/storage/stats', forbidden)
  await page.route('**/binflow/api/repositories', forbidden)
  await page.route('**/binflow/api/v1/audit*', forbidden)

  // SPA 外壳：/binflow/ui/** 一律 index.html（history fallback 的等价物）；
  // 指纹资源在共享挂载 /binflow/assets/**（relink-assets.mjs 的产物形状）
  await page.route('**/binflow/assets/**', (route) => {
    const name = new URL(route.request().url()).pathname.replace('/binflow/assets/', '')
    return route.fulfill({ path: join(DIST, 'assets', name) })
  })
  await page.route('**/binflow/ui/**', (route) => route.fulfill({ path: join(DIST, 'index.html') }))

  return { oidcCalls: () => oidcCalls, documentNavs: () => documentNavs }
}

// ---------------------------------------------------------------------------
// ① oidc disabled（404）：SSO 按钮不可见，密码表单完好
// ---------------------------------------------------------------------------

test('oidc disabled (404) keeps the SSO button hidden; password form intact', async ({ page }) => {
  const { oidcCalls } = await installMocks(page) // 缺省恒 404
  await page.goto('/binflow/ui/login')

  // 探测确实发生后再断言隐藏（避免「初始渲染本来就没有」的假通过）
  await expect.poll(oidcCalls, 'mount probe should hit the oidc login route').toBeGreaterThanOrEqual(1)
  await expect(page.locator('[data-testid="login-sso"]')).toHaveCount(0)
  await expect(page.locator('.login-divider')).toHaveCount(0)
  await expect(page.locator('[data-testid="login-username"]')).toBeVisible()
  await expect(page.locator('[data-testid="login-submit"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ② oidc enabled（302）：按钮可见；点击 = 顶层 GET /oidc/login，离开 SPA
// ---------------------------------------------------------------------------

test('SSO click issues a top-level GET on /oidc/login and leaves the SPA for the IdP', async ({ page }) => {
  const { oidcCalls, documentNavs } = await installMocks(page, { oidc: () => ({ status: 302 }) })
  await page.goto('/binflow/ui/login')

  const sso = page.locator('[data-testid="login-sso"]')
  await expect(sso).toBeVisible()
  await expect(sso).toHaveText('使用 SSO 登录')
  // 挂载探测是 fetch（redirect:'manual'），不会导航离开登录页
  await expect(page).toHaveURL(/\/binflow\/ui\/login/)
  expect(documentNavs()).toBe(0)

  await sso.click()
  // 顶层导航命中 SSO 入口（OD-01），浏览器离开 SPA；文档请求以 mock
  // IdP 落地页应答（真实链路：后端 302 → IdP，见文件头注释）
  await expect(page.locator('h1')).toHaveText('Mock IdP')
  await expect(page).toHaveURL(/\/binflow\/api\/v1\/oidc\/login$/)
  expect(documentNavs()).toBe(1)
  // 挂载探测 + 点击复核两次 fetch 也都命中该路由
  expect(oidcCalls()).toBeGreaterThanOrEqual(3)
})

// ---------------------------------------------------------------------------
// ③ 点击复核失败：探测时启用、点击时已被关（404）→ 行内错误 + 按钮收敛
// ---------------------------------------------------------------------------

test('SSO recheck finds the endpoint gone (404): inline error, button collapses', async ({ page }) => {
  await installMocks(page, {
    oidc: (call) => (call === 1 ? { status: 302 } : { status: 404 }),
  })
  await page.goto('/binflow/ui/login')

  const sso = page.locator('[data-testid="login-sso"]')
  await expect(sso).toBeVisible()
  await sso.click()

  await expect(page.locator('[data-testid="sso-error"]')).toContainText('SSO 登录未启用')
  await expect(sso).toHaveCount(0)
  // 未离开登录页；密码表单仍是可用出路
  await expect(page).toHaveURL(/\/binflow\/ui\/login/)
  await expect(page.locator('[data-testid="login-submit"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ③b 点击复核遇 5xx：行内错误，按钮保留可重试
// ---------------------------------------------------------------------------

test('SSO recheck hits 5xx: inline error, button stays for retry', async ({ page }) => {
  await installMocks(page, {
    oidc: (call) => (call === 1 ? { status: 302 } : { status: 503 }),
  })
  await page.goto('/binflow/ui/login')

  const sso = page.locator('[data-testid="login-sso"]')
  await expect(sso).toBeVisible()
  await sso.click()

  await expect(page.locator('[data-testid="sso-error"]')).toContainText('SSO 登录暂不可用')
  await expect(sso).toBeVisible()
  await expect(page).toHaveURL(/\/binflow\/ui\/login/)
})

// ---------------------------------------------------------------------------
// ④ LDAP 透明登录（AC②）：同一表单、零分支——错口令 401 行内红字，
//    目录凭据 200 落地壳（非 admin 收敛视图）
// ---------------------------------------------------------------------------

test('LDAP user signs in through the same form: 401 inline, directory credentials land in the shell', async ({
  page,
}) => {
  await installMocks(page, {
    // 后端先本地后 LDAP 回退：这里 mock「jdoe + dir-secret」为目录有效凭据
    postSession: (body) =>
      body.password === 'dir-secret'
        ? { status: 200, body: { username: 'jdoe', admin: false } }
        : { status: 401, body: { errors: [{ message: 'invalid credentials' }] } },
  })
  await page.goto('/binflow/ui/')

  // 未登录：守卫重定向登录页并带 return
  await expect(page).toHaveURL(/\/binflow\/ui\/login\?return=%2F$/)

  // 目录用户错误口令：后端统一 401，前端行内呈现（不解释本地/LDAP 差异）
  await page.fill('[data-testid="login-username"]', 'jdoe')
  await page.fill('[data-testid="login-password"]', 'wrong-dir-password')
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="login-error"]')).toHaveText('用户名或密码错误')

  // 同一表单重试正确目录凭据 → session 成功 → 落地壳（与本地用户无差异）
  await page.fill('[data-testid="login-password"]', 'dir-secret')
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
  await expect(page.locator('[data-testid="session-user"]')).toHaveText('jdoe')
  // 非 admin：仪表盘收敛为实例卡 + 说明（管理卡 403 即隐藏）。
  // M8 IA（T-235）：登录落点改为 /artifacts，仪表盘改由侧栏入口 SPA 内
  // 到达（本腿的 session 是 route mock——page.goto 整页刷新会触发真实
  // whoami 401 被守卫弹回，必须走应用内导航）
  await page.click('[data-testid="app-nav"] a.nav-item:text-is("仪表盘")')
  await expect(page.locator('[data-testid="dashboard-instance-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-health-card"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="dashboard-audit-card"]')).toHaveCount(0)
})
