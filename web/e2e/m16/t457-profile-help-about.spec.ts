import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'

// T-457（M16 批次④ B11，FR-145.4/.6a——parity B-1.8 + B-2.17 翻正）：
//
//   ① Profile identity token 自助签发全链：生成 → 一次性明文（关弹窗即
//      不可再取——服务端只存指纹，7.161 形态）+ **即时可用断言**（铸出的
//      令牌真发 API 请求：Bearer 臂 GET /api/v1/session = 200 且主体吻合；
//      Basic 臂 = curl -u 形态〔面板内 curl 样例的口令位〕同测）。
//   ② 普通 user 自铸：有限期闭集（无「永不过期」——Q11 护栏）、无「代人
//      签发」；Bearer 对账主体 = 本人。
//   ③ step-up 内联腿（mock 拦截：ADR-0027 逐字错误体 + 第三次放行真铸）
//      ——t386 ⑤ 同构。
//   ④ SSH Keys 如实缺位：后端无端点（console-ux §9-R11 契约缺口登记）——
//      注记在场 + 增删表单反断言（FE 摆不出没有的端点，不伪造）。
//   ⑤ ? 帮助下拉四项（B-2.17）：Documentation（/binflow/docs/ 外链）/
//      Online Training（禁用占位——无对应服务，7.161 活体处置留痕）/
//      Release Notes（/binflow/docs/install/upgrade 实链）/ About（版本
//      弹窗——消费 /api/system/version）；侧栏脚注 nav-about 同入口。
//
// 消费端点闭集 = 既有面（零新端点）：session 探活 / system/version /
// POST /api/security/token（E-17）/ revoke 收尾（E-18，管理臂）。
// 锚源 = console-ux §10.5 T-457 批（v1.42）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

const ADR_STEP_UP_REQUIRED = JSON.stringify({
  error: 'step_up_required',
  error_description: 'step-up authentication required to mint a token',
})
const ADR_STEP_UP_INVALID = JSON.stringify({
  error: 'step_up_invalid',
  error_description: 'step-up credential rejected, expired, or already used',
})

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

/** Bearer 臂对账：铸出的令牌真发一个 API 请求（GET /api/v1/session 认
 * 任意臂）——UI 说「铸了 X 主体」，让服务端复核主体真身。 */
async function whoamiVia(page: Page, token: string): Promise<{ status: number; username?: string }> {
  return page.evaluate(async (t) => {
    const res = await fetch('/binflow/api/v1/session', { headers: { Authorization: `Bearer ${t}` } })
    let username: string | undefined
    try {
      username = (await res.json()).username
    } catch {
      // 401 体非 JSON——保持 undefined
    }
    return { status: res.status, username }
  }, token)
}

/** Basic 臂对账（curl -u 形态——面板内样例的口令位就是令牌）：以
 * username:token 过 GET /api/v1/session（auth-model §3.6 双臂）。 */
async function whoamiViaBasic(page: Page, username: string, token: string): Promise<{ status: number; username?: string }> {
  return page.evaluate(
    async ({ u, t }) => {
      const res = await fetch('/binflow/api/v1/session', {
        headers: { Authorization: `Basic ${btoa(`${u}:${t}`)}` },
      })
      let who: string | undefined
      try {
        who = (await res.json()).username
      } catch {
        // 401 体非 JSON
      }
      return { status: res.status, username: who }
    },
    { u: username, t: token },
  )
}

/** 管理员 REST 直吊（收尾卫生——不留活探针凭据；E-18 form 体） */
async function revokeViaAdmin(tokenId: number): Promise<number> {
  const base = String(process.env.BASE ?? 'http://127.0.0.1:8080').replace(/\/+$/, '')
  const auth = Buffer.from(`${ADMIN}:${ADMIN_PW}`).toString('base64')
  const res = await fetch(`${base}/binflow/api/security/token/revoke`, {
    method: 'POST',
    headers: { Authorization: `Basic ${auth}`, 'Content-Type': 'application/x-www-form-urlencoded' },
    body: `token_id=${tokenId}`,
  })
  return res.status
}

/** Profile 页 UI 链路铸一枚——返回 { token, tokenId }（明文面板在场断言后）。
 * 前置：生成弹窗已开（调用方控制开窗时机——连续断言中间不必关开）。 */
async function mintFromOpenDialog(page: Page): Promise<{ token: string; tokenId: number }> {
  await expect(page.locator('[data-testid="profile-token-dialog"]')).toBeVisible()
  await page.click('[data-testid="profile-token-submit"]')
  await expect(page.locator('[data-testid="profile-token-plaintext"]')).toBeVisible({ timeout: 15_000 })
  const token = ((await page.locator('[data-testid="profile-token-value"]').textContent()) ?? '').trim()
  const tokenId = Number(
    (((await page.locator('[data-testid="profile-token-plaintext"]').textContent()) ?? '').match(/#(\d+)/) ?? [])[1],
  )
  return { token, tokenId }
}

// ---------------------------------------------------------------------------
// ① admin 自助签发全链：一次性明文 + 即时可用（Bearer + Basic/curl 臂）+ 关闭不可再取
// ---------------------------------------------------------------------------

test('profile token: admin self-mint chain — one-time plaintext, immediately usable (Bearer + curl Basic), gone after close', async ({
  page,
}) => {
  const sess = await loginAs(page, 'admin')
  await page.goto('/binflow/ui/profile')
  await expect(page.locator('[data-testid="profile-page"]')).toBeVisible()

  // B-1.8 翻正面：签发在本页（说明卡在 + 生成钮在）；改密卡共存
  await expect(page.locator('[data-testid="profile-token"]')).toBeVisible()
  await expect(page.locator('[data-testid="profile-password"]')).toBeVisible()
  await expect(page.locator('[data-testid="profile-token-generate"]')).toBeVisible()
  // 管理面二级入口维持（m8 auxiliary 同锚；Link 解析为 basename 全路径）
  await expect(page.locator('[data-testid="profile-token-goto"]')).toHaveAttribute(
    'href',
    '/binflow/ui/admin/security/tokens',
  )
  await expect(page.locator('[data-testid="profile-token-docs"]')).toHaveAttribute(
    'href',
    '/binflow/docs/api-reference',
  )

  // 生成弹窗：admin 见「永不过期」；默认 24h
  await page.click('[data-testid="profile-token-generate"]')
  const dialog = page.locator('[data-testid="profile-token-dialog"]')
  await expect(dialog).toBeVisible()
  await expect(page.locator('[data-testid="profile-token-ttl"]')).toHaveValue('86400')
  const ttlOptions = await page.locator('[data-testid="profile-token-ttl"] option').evaluateAll((els) =>
    els.map((e) => (e as HTMLOptionElement).value),
  )
  expect(ttlOptions).toEqual(['3600', '86400', '604800', '2592000', '31536000', '0']) // 含永不过期（admin）

  // 生成 → 一次性明文（64 hex + token_id + 警示）
  const { token, tokenId } = await mintFromOpenDialog(page)
  const panel = page.locator('[data-testid="profile-token-plaintext"]')
  await expect(panel).toBeVisible()
  expect(token).toMatch(/^[0-9a-f]{64}$/)
  expect(Number.isFinite(tokenId)).toBe(true)
  await expect(panel).toContainText('不可再查看')
  await expect(page.locator('[data-testid="profile-token-id"]')).toContainText(`#${tokenId}`)

  // 即时可用（AC3 断言本体）：铸出的令牌真发 API 请求
  const bearer = await whoamiVia(page, token)
  expect(bearer.status).toBe(200)
  expect(bearer.username).toBe(sess.username)

  // 面板内 curl 样例（-u 用户名:令牌——Basic 双臂的即用形态）：样例携带
  // 令牌 + 以该形态真实请求 = 200 且主体吻合
  const curlLine = ((await page.locator('[data-testid="profile-token-curl"]').textContent()) ?? '').trim()
  expect(curlLine).toContain(`-u ${sess.username}:${token}`)
  const basic = await whoamiViaBasic(page, sess.username, token)
  expect(basic.status).toBe(200)
  expect(basic.username).toBe(sess.username)

  // 关闭 → 明文退场；重开 = 全新表单（无明文残留）；刷新后同样不可再取
  await page.click('[data-testid="profile-token-done"]')
  await expect(page.locator('[data-testid="profile-token-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="profile-page"]')).not.toContainText(token)
  await page.click('[data-testid="profile-token-generate"]')
  await expect(page.locator('[data-testid="profile-token-submit"]')).toBeVisible()
  await expect(page.locator('[data-testid="profile-token-plaintext"]')).toHaveCount(0)
  await page.click('[data-testid="profile-token-cancel"]')
  await page.reload()
  await expect(page.locator('[data-testid="profile-token"]')).not.toContainText(token)

  // 令牌本身仍有效（关闭只清 UI 呈现，不吊销）——收尾前再对账一次
  const still = await whoamiVia(page, token)
  expect(still.status).toBe(200)

  // 收尾：管理臂 REST 吊销探针令牌（不留活凭据）→ 死亡对账
  expect(await revokeViaAdmin(tokenId)).toBe(200)
  const dead = await whoamiVia(page, token)
  expect(dead.status).toBe(401)
})

// ---------------------------------------------------------------------------
// ② 普通 user 自铸：有限期闭集（无永不过期/无代人签发）+ Bearer 主体对账
// ---------------------------------------------------------------------------

test('profile token: plain user self-mint — capped TTL set, no on-behalf field, subject is self', async ({ page }) => {
  const sess = await loginAs(page, 'user')
  await page.goto('/binflow/ui/profile')

  await page.click('[data-testid="profile-token-generate"]')
  await expect(page.locator('[data-testid="profile-token-dialog"]')).toBeVisible()
  const ttlOptions = await page.locator('[data-testid="profile-token-ttl"] option').evaluateAll((els) =>
    els.map((e) => (e as HTMLOptionElement).value),
  )
  expect(ttlOptions).toEqual(['3600', '86400', '604800', '2592000', '31536000']) // 无「永不过期」（Q11 护栏）
  const dialogText = await page.locator('[data-testid="profile-token-dialog"]').textContent()
  expect(dialogText).not.toContain('代人签发')
  expect(dialogText).not.toContain('签发对象')

  // 签发错误内联（mock 一次性 500——表单态零丢失后放行真铸）
  let errCalls = 0
  await page.route('**/api/security/token', async (route) => {
    errCalls += 1
    if (errCalls === 1) {
      return route.fulfill({ status: 500, contentType: 'application/json', body: '{}' })
    }
    return route.continue()
  })
  await page.click('[data-testid="profile-token-submit"]')
  await expect(page.locator('[data-testid="profile-token-error"]')).toBeVisible()
  await expect(page.locator('[data-testid="profile-token-error"]')).toContainText('HTTP 500')
  await page.unroute('**/api/security/token')

  // 放行真服务端——生成成功（弹窗已开，TTL 检查同窗）
  const { token, tokenId } = await mintFromOpenDialog(page)
  expect(token).toMatch(/^[0-9a-f]{64}$/)
  const alive = await whoamiVia(page, token)
  expect(alive.status).toBe(200)
  expect(alive.username).toBe(sess.username)
  await page.click('[data-testid="profile-token-done"]')

  expect(await revokeViaAdmin(tokenId)).toBe(200)
})

// ---------------------------------------------------------------------------
// ③ step-up 内联腿（mock 拦截：ADR 逐字错误体 + 第三次放行真铸）
// ---------------------------------------------------------------------------

test('profile token step-up: 401 required -> inline password form; invalid -> verbatim ADR text; retry mints', async ({
  page,
}) => {
  // readonly_admin = 非 admin web session 臂（step-up 的作用域）
  const sess = await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/profile')
  await page.click('[data-testid="profile-token-generate"]')

  const bodies: unknown[] = []
  let calls = 0
  await page.route('**/api/security/token', async (route) => {
    calls += 1
    try {
      bodies.push(route.request().postDataJSON())
    } catch {
      bodies.push(null)
    }
    if (calls === 1) {
      return route.fulfill({ status: 401, contentType: 'application/json', body: ADR_STEP_UP_REQUIRED })
    }
    if (calls === 2) {
      return route.fulfill({ status: 401, contentType: 'application/json', body: ADR_STEP_UP_INVALID })
    }
    return route.continue()
  })

  // ① 首次签发 → 内联口令框（不弹第二层）+ 聚焦
  await page.click('[data-testid="profile-token-submit"]')
  await expect(page.locator('[data-testid="profile-token-stepup"]')).toBeVisible()
  await expect(page.locator('[data-testid="profile-token-password"]')).toBeFocused()
  await expect(page.locator('[data-testid="profile-token-plaintext"]')).toHaveCount(0)
  await expect(page.locator('[role="dialog"]')).toHaveCount(1)

  // ② 错误口令 → 内联错误 = error_description 逐字
  await page.fill('[data-testid="profile-token-password"]', 'definitely-wrong-password')
  await page.click('[data-testid="profile-token-password-submit"]')
  await expect(page.locator('[data-testid="profile-token-password-error"]')).toHaveText(
    'step-up credential rejected, expired, or already used',
  )

  // ③ 正确口令 → 续铸成功（请求体携 step_up_password + expires_in）
  await page.fill('[data-testid="profile-token-password"]', sess.password)
  await page.click('[data-testid="profile-token-password-submit"]')
  await expect(page.locator('[data-testid="profile-token-plaintext"]')).toBeVisible({ timeout: 15_000 })
  expect(calls).toBe(3)
  const third = bodies[2] as { step_up_password?: string; expires_in?: number }
  expect(third.step_up_password).toBe(sess.password)
  expect(third.expires_in).toBe(86400)

  const tokenId = Number(
    ((await page.locator('[data-testid="profile-token-plaintext"]').textContent()) ?? '').match(/#(\d+)/)?.[1],
  )
  await page.click('[data-testid="profile-token-done"]')
  expect(await revokeViaAdmin(tokenId)).toBe(200)
})

// ---------------------------------------------------------------------------
// ④ SSH Keys：如实缺位（§9-R11 契约缺口——注记在场 + 表单反断言）
// ---------------------------------------------------------------------------

test('profile ssh: honest gap note, no fabricated add/delete form (backend endpoint absent)', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/profile')

  await expect(page.locator('[data-testid="profile-ssh"]')).toBeVisible()
  await expect(page.locator('[data-testid="profile-ssh-gap"]')).toBeVisible()
  await expect(page.locator('[data-testid="profile-ssh-gap"]')).toContainText('SSH 公钥端点')
  // 反断言：不伪造增删入口（FE 摆不出没有的端点）——结构断言（卡内零
  // 输入/按钮/表单），不引用不存在的锚名（锚册 broken 硬门）
  const sshCard = page.locator('[data-testid="profile-ssh"]')
  await expect(sshCard.locator('button, input, form, table')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑤ ? 帮助下拉四项 + About 版本弹窗（B-2.17）
// ---------------------------------------------------------------------------

test('help dropdown: four items, honest disabled gap, About dialog matches /api/system/version', async ({ page }) => {
  await loginAs(page, 'user')

  // 下拉形态：topbar-help 触发钮（原纯链接翻正）→ 菜单四项
  const help = page.locator('[data-testid="topbar-help"]')
  await expect(help).toBeVisible()
  await expect(help).toHaveAttribute('aria-haspopup', 'menu')
  await help.click()
  await expect(page.locator('[data-testid="help-docs"]')).toBeVisible()

  // Documentation：/binflow/docs/ 外链（console-ux §3.5 定案形态）
  await expect(page.locator('[data-testid="help-docs"]')).toHaveAttribute('href', '/binflow/docs/')
  await expect(page.locator('[data-testid="help-docs"]')).toHaveAttribute('target', '_blank')

  // Online Training：无对应服务 → 禁用占位（7.161 活体形态处置，登记不伪造）
  await expect(page.locator('[data-testid="help-training"]')).toBeDisabled()
  await expect(page.locator('[data-testid="help-training"]')).toContainText('暂无对应服务')

  // Release Notes：实链到文档站「升级与版本说明」
  await expect(page.locator('[data-testid="help-release-notes"]')).toHaveAttribute(
    'href',
    '/binflow/docs/install/upgrade',
  )
  await expect(page.locator('[data-testid="help-release-notes"]')).toHaveAttribute('target', '_blank')

  // About → 版本弹窗：版本/构建与 /api/system/version 同源对账
  await page.click('[data-testid="help-about"]')
  const about = page.locator('[data-testid="about-dialog"]')
  await expect(about).toBeVisible()
  await expect(page.locator('[data-testid="about-brand-mark"]')).toBeVisible()
  const ver = (await page.evaluate(async () => (await (await fetch('/binflow/api/system/version')).json()) as {
    version: string
    revision: string
    product: string
  })) as { version: string; revision: string; product: string }
  await expect(page.locator('[data-testid="about-version"]')).toHaveText(`v${ver.version}`)
  await expect(page.locator('[data-testid="about-revision"]')).toHaveText(ver.revision)
  await expect(page.locator('[data-testid="about-product"]')).toHaveText(ver.product)
  // 侧栏脚注版本行同源（nav-version 冻结锚零回退）
  await expect(page.locator('[data-testid="nav-version"]')).toHaveText(`v${ver.version}`)
  await page.click('[data-testid="about-close"]')
  await expect(about).toHaveCount(0)

  // 侧栏脚注入口（B-2.17：vdev 行升格）：nav-about 点击同样开 About
  await expect(page.locator('[data-testid="nav-about"]')).toBeVisible()
  await page.locator('[data-testid="nav-about"]').scrollIntoViewIfNeeded()
  await page.click('[data-testid="nav-about"]')
  await expect(page.locator('[data-testid="about-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="about-version"]')).toHaveText(`v${ver.version}`)
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="about-dialog"]')).toHaveCount(0)

  // 菜单可关（Esc 回焦触发钮——MUI Menu 原生）
  await help.click()
  await expect(page.locator('[data-testid="help-docs"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="help-docs"]')).toBeHidden()
})

// ---------------------------------------------------------------------------
// ⑥ axe 双主题：Profile 页全解剖 + 帮助菜单展开态 + About 弹窗态
// ---------------------------------------------------------------------------

test('axe: profile page, help menu, and About dialog clean in both themes', async ({ page }, testInfo: TestInfo) => {
  await loginAs(page, 'user')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/profile')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible()

    // Profile 页（改密 + Identity Token + SSH 缺位卡）
    await expectA11yClean(page, testInfo, { include: '[data-testid="profile-page"]' })

    // 帮助菜单展开态（含禁用占位项）。
    // T-459 复核注记：可见 ≠ 过场结束——axe 的 color-contrast 对半透明色按
    // 实际像素采样，菜单/弹窗进出场动画未完时采样即假性炸裂（共租负载下
    // 3/3 复现：菜单退场项与 about-close 先后中招；settle 后净绿）。两处
    // 全页扫描前统一 settle 400ms，同款处置见 t447-props-download axe 腿。
    await page.click('[data-testid="topbar-help"]')
    await expect(page.locator('[data-testid="help-training"]')).toBeVisible()
    await page.waitForTimeout(400)
    await expectA11yClean(page, testInfo)

    // About 弹窗态（版本块 + 关闭钮）
    await page.click('[data-testid="help-about"]')
    await expect(page.locator('[data-testid="about-dialog"]')).toBeVisible()
    await page.waitForTimeout(400)
    await expectA11yClean(page, testInfo)
    await page.keyboard.press('Escape')
    await expect(page.locator('[data-testid="about-dialog"]')).toHaveCount(0)
  }
})
