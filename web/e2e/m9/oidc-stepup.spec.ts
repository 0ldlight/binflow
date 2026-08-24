import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, seedRepos, sessionApi } from '../m8/support/seed'

// T-260 (FR-81 / N10-N12): the OIDC step-up console leg over the ADR-0027
// mint-grant contract (server zero-change, T-219). Two layers, mirroring the
// T-242 §1.5-3/§1.5-4 probe pattern:
//
//   mock leg (any instance): browser-boundary contract simulation — whoami
//     source=oidc + a single-use grant ledger in the route mock. Asserts the
//     FRONTEND consumption contract: 401 required -> re-auth guide (no password
//     form), pending-mint in sessionStorage, purpose=step_up navigation, the
//     fragment landing (strip + auto re-mint carrying step_up_grant + exactly
//     one presentation), replay -> 401 step_up_invalid verbatim, storage
//     hygiene (grant plaintext never persisted), orphan-fragment discard.
//
//   armed leg (skips unless the instance runs BINFLOW_AUTH_TOKEN_STEP_UP=true
//     AND auth.oidc against the mock IdP of scripts/mock-idp.mjs): the
//     live gate end to end — real 401s, a real IdP round trip with prompt=login
//     asserted at the IdP, a real single-use grant consumed by a real mint
//     (Bearer reconcile + token.issue audit step_up_method=oidc_reauth), and a
//     network-layer replay refused verbatim.

const ADR_STEP_UP_REQUIRED = JSON.stringify({
  error: 'step_up_required',
  error_description: 'step-up authentication required to mint a token',
})
const ADR_STEP_UP_INVALID = JSON.stringify({
  error: 'step_up_invalid',
  error_description: 'step-up credential rejected, expired, or already used',
})

/** mock IdP fixture identity (scripts/mock-idp.mjs) */
const IDP_USER = { username: 'ssouser', password: 'ssopass-t260' }

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 64-hex grant shape (server grantBytes=32, hex) */
function fakeGrant(): string {
  let g = ''
  for (let i = 0; i < 8; i++) g += Math.random().toString(16).slice(2, 10)
  return g.slice(0, 64).padEnd(64, '0')
}

interface MintBody {
  step_up_grant?: string
  step_up_password?: string
  expires_in?: number
}

/** sessionStorage snapshot as an object (for hygiene assertions). */
async function storageMap(page: Page): Promise<Record<string, string>> {
  return page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage)))
}

/** whoami 改写 source=oidc（mock 面：分流依据是 source 字段本身——默认实例
 *  无 IdP，GET 直通真实服务端仅改写身份属主；POST 等其他方法直通）。 */
async function mockOIDCSource(page: Page): Promise<void> {
  await page.route('**/api/v1/session', async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const res = await route.fetch()
    let who: Record<string, unknown> = {}
    try {
      who = (await res.json()) as Record<string, unknown>
    } catch {
      // 非 JSON 体：按原状态直通空对象
    }
    return route.fulfill({
      status: res.status(),
      contentType: 'application/json',
      body: JSON.stringify({ ...who, source: 'oidc' }),
    })
  })
}

// ---- mock leg：401 → 重认证引导 → fragment 回跳 → 自动续铸 → 单次性 ----------

test('oidc step-up (mock): required -> re-auth guide (no password form); fragment lands stripped; auto re-mint carries the grant exactly once; replay refused verbatim', async ({
  page,
}, testInfo: TestInfo) => {
  test.setTimeout(120_000)
  const key = uniq('m9oidc')
  const grant = fakeGrant()
  await seedRepos(m8Client(), [{ key }])
  await mockOIDCSource(page)

  // mint 台账 mock：单次语义（grant 一经呈现即烧毁；重放/未知 → 401
  // step_up_invalid 逐字）；无凭据 → 401 step_up_required 逐字
  const bodies: MintBody[] = []
  let burned = false
  await page.route('**/api/security/token', async (route) => {
    let body: MintBody | null = null
    try {
      body = route.request().postDataJSON() as MintBody
    } catch {
      body = null
    }
    bodies.push(body ?? {})
    if (body?.step_up_grant === grant && !burned) {
      burned = true
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          access_token: 'mock-minted-token-t260',
          token_type: 'Bearer',
          scope: 'api:*',
          token_id: 'mock-token-id-t260',
          expires_in: 86400,
        }),
      })
    }
    if (body?.step_up_grant) {
      return route.fulfill({ status: 401, contentType: 'application/json', body: ADR_STEP_UP_INVALID })
    }
    return route.fulfill({ status: 401, contentType: 'application/json', body: ADR_STEP_UP_REQUIRED })
  })

  // authorize 跳转拦截：断言 purpose=step_up（T-219 契约），以静态页模拟
  // 「已到 IdP」；后续由测试直接落 fragment 模拟 callback 302 回跳
  let authorizeURL = ''
  await page.route('**/api/v1/oidc/login*', async (route) => {
    authorizeURL = route.request().url()
    return route.fulfill({ contentType: 'text/html', body: '<html><body>mock IdP (step-up)</body></html>' })
  })

  const sess = await loginAs(page, 'readonly_admin')
  expect(sess.username).toBeTruthy()

  await page.goto('/binflow/ui/artifacts')
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await page.click('[data-testid="tree-setmeup"]')
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)

  // ④ 首次铸币 → 401 required → OIDC 腿：重认证引导（口令框绝不出现）
  await page.click('[data-testid="smu-generate"]')
  const reauth = page.locator('[data-testid="smu-oidc-stepup"]')
  await expect(reauth).toBeVisible()
  await expect(page.locator('[data-testid="smu-stepup"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="smu-password"]')).toHaveCount(0)
  await expectA11yClean(page, testInfo, { include: '[data-testid="smu-dialog"]' })

  // ⑤ 发起：pending-mint 落 sessionStorage（上下文含 repo；grant 值绝不入）
  await page.click('[data-testid="smu-reauth"]')
  await expect(page.locator('body')).toContainText('mock IdP (step-up)')
  expect(authorizeURL).toContain('purpose=step_up')
  const pendingRaw = (await storageMap(page))['bf.pendingMint']
  expect(pendingRaw, 'pending-mint persisted before the IdP round trip').toBeTruthy()
  const pending = JSON.parse(pendingRaw as string) as { repo?: string; pkg?: string }
  expect(pending.repo).toBe(key)
  expect(pending.pkg).toBe('generic')

  // ⑥ 模拟 callback 302：同 tab 落 fragment（sessionStorage 存活）
  await page.goto(`/binflow/ui/#step_up_grant=${grant}`)

  // fragment 即抹除（history.replaceState——刷新不重放、地址栏不可见）
  await expect(page).not.toHaveURL(/#step_up_grant/)

  // 续铸视图自动重开 → 自动续铸成功（一次性令牌面板）
  await expect(page.locator('[data-testid="smu-dialog"]')).toBeVisible()
  const panel = page.locator('[data-testid="smu-token-panel"]')
  await expect(panel).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="smu-token"]')).toHaveText('mock-minted-token-t260')

  // ⑦ 请求体对账：step_up_grant 携带且恰好呈现一次（双发防护——第二发必
  //    invalid，前端只在挂载时发一次）
  const presented = bodies.filter((b) => b.step_up_grant === grant)
  expect(presented.length).toBe(1)
  expect(presented[0].expires_in).toBe(86400)
  expect(presented[0].step_up_password).toBeUndefined()

  // ⑧ 存储卫生：grant 明文只存续于 fragment 与请求体——sessionStorage 里
  //    既无 grant 值，成功后 pending 也已清
  const after = await storageMap(page)
  expect(JSON.stringify(after)).not.toContain(grant)
  expect(after['bf.pendingMint']).toBeUndefined()

  // ⑨ 网络层重放同 grant → 401 step_up_invalid（error_description 逐字）
  const replay = await sessionApi(page, 'POST', '/api/security/token', {
    grant_type: 'client_credentials',
    expires_in: 3600,
    step_up_grant: grant,
  })
  expect(replay.status).toBe(401)
  expect(replay.text.trim()).toBe(ADR_STEP_UP_INVALID)
})

// ---- mock leg：grant 过期/复用 → 内联 ADR 文案 + 重新发起；旧 grant 绝不重试 --

test('oidc step-up (mock): invalid grant -> inline ADR verbatim + re-init entry; the stale grant is never retried and pending is cleared', async ({
  page,
}) => {
  test.setTimeout(60_000) // 冷实例 + 并行播种时的树加载余量（88 腿同款）
  const key = uniq('m9inv')
  const staleGrant = fakeGrant()
  await seedRepos(m8Client(), [{ key }])
  await mockOIDCSource(page)

  // 台账不认识这个 grant（模拟过期/已用）→ 401 invalid 逐字
  const grantPosts: string[] = []
  await page.route('**/api/security/token', async (route) => {
    let body: MintBody | null = null
    try {
      body = route.request().postDataJSON() as MintBody
    } catch {
      body = null
    }
    if (body?.step_up_grant) grantPosts.push(body.step_up_grant)
    const answer = body?.step_up_grant ? ADR_STEP_UP_INVALID : ADR_STEP_UP_REQUIRED
    return route.fulfill({ status: 401, contentType: 'application/json', body: answer })
  })

  await page.route('**/api/v1/oidc/login*', async (route) => {
    return route.fulfill({ contentType: 'text/html', body: '<html><body>mock IdP (invalid leg)</body></html>' })
  })

  await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/artifacts')
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await page.click('[data-testid="tree-setmeup"]')
  await page.click('[data-testid="smu-generate"]')
  await expect(page.locator('[data-testid="smu-oidc-stepup"]')).toBeVisible()
  await page.click('[data-testid="smu-reauth"]')
  await expect(page.locator('body')).toContainText('mock IdP (invalid leg)')

  // 回跳携一个已失效的 grant（模拟 TTL 已过/他处已用）
  await page.goto(`/binflow/ui/#step_up_grant=${staleGrant}`)
  await expect(page).not.toHaveURL(/#step_up_grant/)

  // 自动续铸 → 401 → 内联 ADR 逐字 + 重认证入口（非口令框）
  const reauth = page.locator('[data-testid="smu-oidc-stepup"]')
  await expect(reauth).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="smu-reauth-error"]')).toHaveText(
    'step-up credential rejected, expired, or already used',
  )
  await expect(page.locator('[data-testid="smu-reauth"]')).toBeVisible()
  await expect(page.locator('[data-testid="smu-password"]')).toHaveCount(0)

  // 单次消费：旧 grant 恰好呈现一次（无自动重试循环），pending 即清
  await expect.poll(() => grantPosts.length).toBe(1)
  expect(grantPosts[0]).toBe(staleGrant)
  const after = await storageMap(page)
  expect(after['bf.pendingMint']).toBeUndefined()

  // 重新发起 = 全新 init：pending 以新上下文重写（不以旧 grant 重试的实证）
  await page.click('[data-testid="smu-reauth"]')
  await expect(page.locator('body')).toContainText('mock IdP (invalid leg)')
  const rewritten = JSON.parse((await storageMap(page))['bf.pendingMint'] as string) as { repo?: string }
  expect(rewritten.repo).toBe(key)
})

// ---- mock leg：pending 态提示 + 孤儿 fragment --------------------------------

test('oidc step-up (mock): pending-mint hint on the mint face; orphan fragments are stripped and dropped silently', async ({
  page,
}) => {
  test.setTimeout(60_000)
  const key = uniq('m9pend')
  await seedRepos(m8Client(), [{ key }])
  await mockOIDCSource(page)
  await page.route('**/api/security/token', async (route) => {
    return route.fulfill({ status: 401, contentType: 'application/json', body: ADR_STEP_UP_REQUIRED })
  })
  await page.route('**/api/v1/oidc/login*', async (route) => {
    return route.fulfill({ contentType: 'text/html', body: '<html><body>mock IdP (pending leg)</body></html>' })
  })

  await loginAs(page, 'readonly_admin')

  // 半途流程（IdP 侧取消/中断后回来）：pending 在、grant 不在 → 铸造面提示
  await page.goto('/binflow/ui/artifacts')
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await page.click('[data-testid="tree-setmeup"]')
  await page.click('[data-testid="smu-generate"]')
  await expect(page.locator('[data-testid="smu-oidc-stepup"]')).toBeVisible()
  await page.click('[data-testid="smu-reauth"]')
  await expect(page.locator('body')).toContainText('mock IdP (pending leg)')

  // 模拟「IdP 取消」直接回控制台（无 fragment）
  await page.goto('/binflow/ui/artifacts')
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await page.click('[data-testid="tree-setmeup"]')
  await expect(page.locator('[data-testid="smu-pending-hint"]')).toContainText('等待重认证完成')
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="smu-dialog"]')).toHaveCount(0)

  // 孤儿 fragment（无 pending）：抹除 + 静默丢弃（不开续铸视图）
  await page.evaluate(() => sessionStorage.removeItem('bf.pendingMint'))
  await page.goto(`/binflow/ui/#step_up_grant=${fakeGrant()}`)
  await expect(page).not.toHaveURL(/#step_up_grant/)
  await expect(page.locator('[data-testid="smu-dialog"]')).toHaveCount(0)

  // 形态不符的 fragment（非 64hex）：同样抹除、不入内存
  await page.goto('/binflow/ui/#step_up_grant=not-a-grant')
  await expect(page).not.toHaveURL(/#step_up_grant/)
  await expect(page.locator('[data-testid="smu-dialog"]')).toHaveCount(0)
})

// ---- armed leg：真实 armed 实例 + mock IdP 全链（默认实例自动 skip） ---------

/** 探测 OIDC 是否启用（302 → opaqueredirect；disabled → 404） */
async function oidcEnabled(page: Page): Promise<boolean> {
  return page.evaluate(async () => {
    const res = await fetch('/binflow/api/v1/oidc/login', {
      redirect: 'manual',
      credentials: 'same-origin',
    })
    return res.type === 'opaqueredirect' || res.status === 0
  })
}

test('oidc step-up (armed instance): live gate full chain via the mock IdP — re-auth lands the fragment grant, the mint consumes it once, the replay is refused verbatim, the audit carries oidc_reauth', async ({
  page,
}) => {
  test.setTimeout(180_000)
  const key = uniq('m9armed')
  await seedRepos(m8Client(), [{ key }])

  // 探针①：oidc.enabled（默认实例 404 → skip；mock 腿已覆盖前端契约面）。
  // 先落一页再 evaluate——about:blank 上相对 URL 的 fetch 无法解析。
  await page.goto('/binflow/ui/')
  test.skip(!(await oidcEnabled(page)), 'oidc not enabled on this instance (covered by the mocked legs)')

  // SSO 登录（真实浏览器链：console → IdP 表单 → callback → session）
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="login-sso"]')).toBeVisible()
  await page.click('[data-testid="login-sso"]')
  await expect(page.locator('[data-testid="idp-login-page"]')).toBeVisible({ timeout: 30_000 })
  await page.fill('#idp-username', IDP_USER.username)
  await page.fill('#idp-password', IDP_USER.password)
  await page.click('#idp-submit')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible({ timeout: 30_000 })
  await expect(page.locator('[data-testid="session-user"]')).toHaveText(IDP_USER.username)

  // whoami source=oidc（腿分流的身份属主事实）
  const who = await sessionApi(page, 'GET', '/api/v1/session')
  expect(who.status).toBe(200)
  expect((who.json as { source?: string }).source).toBe('oidc')

  // 探针②：armed（非 admin OIDC session 无凭据铸币 → 401 step_up_required；
  // 未 armed 实例直接 200 → skip）
  const probe = await sessionApi(page, 'POST', '/api/security/token', {
    grant_type: 'client_credentials',
    expires_in: 3600,
  })
  test.skip(
    probe.status !== 401 || (probe.json as { error?: string })?.error !== 'step_up_required',
    'instance not armed with auth.token_step_up (covered by the mocked legs)',
  )

  // 记录 mint 请求体（真放行——只为捕获回跳后的 step_up_grant 供重放腿）
  const bodies: MintBody[] = []
  await page.route('**/api/security/token', async (route) => {
    try {
      bodies.push(route.request().postDataJSON() as MintBody)
    } catch {
      bodies.push({})
    }
    return route.continue()
  })

  await page.goto('/binflow/ui/artifacts')
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await page.click('[data-testid="tree-setmeup"]')
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)

  // 401 required → OIDC 腿引导 → 真实 purpose=step_up 跳转（IdP 侧实证
  // prompt=login——T-219 的强制重认证参数）
  await page.click('[data-testid="smu-generate"]')
  await expect(page.locator('[data-testid="smu-oidc-stepup"]')).toBeVisible()
  await page.click('[data-testid="smu-reauth"]')
  await expect(page.locator('[data-testid="idp-login-page"]')).toBeVisible({ timeout: 30_000 })
  expect(decodeURIComponent(page.url())).toContain('prompt=login')

  // 重认证（同一身份——第二身份回跳拒发 grant 是 T-224 已证的服务端面）
  await page.fill('#idp-username', IDP_USER.username)
  await page.fill('#idp-password', IDP_USER.password)
  await page.click('#idp-submit')

  // fragment 回跳 → 即抹除 → 续铸视图自动重开 → 真 grant 消费 → 令牌面板
  const panel = page.locator('[data-testid="smu-token-panel"]')
  await expect(panel).toBeVisible({ timeout: 30_000 })
  await expect(page).not.toHaveURL(/#step_up_grant/)
  const token = ((await page.locator('[data-testid="smu-token"]').textContent()) ?? '').trim()
  expect(token.length).toBeGreaterThan(20)

  // 请求体对账：真 grant（64hex）恰呈现一次
  const granted = bodies.filter((b) => typeof b.step_up_grant === 'string')
  expect(granted.length).toBe(1)
  const grant = granted[0].step_up_grant as string
  expect(grant).toMatch(/^[0-9a-f]{64}$/)

  // 令牌可用对账（N10）：Bearer 臂 GET /api/system/version = 200
  const bearer = await page.evaluate(async (t) => {
    const r = await fetch('/binflow/api/system/version', { headers: { Authorization: `Bearer ${t}` } })
    return r.status
  }, token)
  expect(bearer).toBe(200)

  // 存储卫生：grant 不落 sessionStorage；成功后 pending 清空
  const after = await storageMap(page)
  expect(JSON.stringify(after)).not.toContain(grant)
  expect(after['bf.pendingMint']).toBeUndefined()

  // 单次性（N11）：网络层重放同 grant → 真 401 step_up_invalid（逐字）
  const replay = await sessionApi(page, 'POST', '/api/security/token', {
    grant_type: 'client_credentials',
    expires_in: 3600,
    step_up_grant: grant,
  })
  expect(replay.status).toBe(401)
  expect((replay.json as { error?: string }).error).toBe('step_up_invalid')
  expect((replay.json as { error_description?: string }).error_description).toBe(
    'step-up credential rejected, expired, or already used',
  )

  // 审计对账（N10）：token.issue 含 step_up_method=oidc_reauth（admin 臂读；
  // detail 在 wire 上是 JSON 对象——扁平化后做包含断言，与序列化形态解耦）
  const audit = await m8Client().request('GET', '/binflow/api/v1/audit?limit=10')
  expect(audit.status).toBe(200)
  const events = (JSON.parse(audit.text) as { events?: { action?: string; actor?: string; detail?: unknown }[] })
    .events ?? []
  const flat = (v: unknown) => JSON.stringify(v).replace(/\\"/g, '"')
  const hit = events.find((e) => e.action === 'token.issue' && flat(e.detail).includes('"step_up_method":"oidc_reauth"'))
  expect(hit, 'token.issue audit entry carries step_up_method=oidc_reauth').toBeTruthy()
  expect(hit?.actor).toBe(IDP_USER.username)
})
