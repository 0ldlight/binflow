import { createHash } from 'node:crypto'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { sessionApi } from '../m8/support/seed'

// T-386（M14 B6 FE，FR-125.1）——Access Tokens 页真身。占位页载体随本票
// 退役（占位锚全 src/spec 退役入册 §10.6；组件删除——src 侧 grep=0）；消费
// 端点清单 == 既有 token REST 两端点（零新端点断言：本 spec 运行期对账 +
// 页面注释留痕）。参照形态 = T-381 V6：生成区 + Identity Tokens 表；V6c 表单
// 字段集低置信（Q4 终裁未开）——暂行字段集（有效期/签发对象/scope 只读）
// 断言在案，终裁翻转面留 T-395。
//
// 面板语义：服务端只存指纹、无令牌清单端点（console-ux §9-R6）——表 = 本
// 会话台账（刷新即空）；明文一次性（关闭/刷新不可再取——内存态保证）。
// step-up 腿两层（setmeup-deploy.spec 同构）：mock 拦截腿任何实例可跑 +
// 真实 armed 实例腿（BINFLOW_AUTH_TOKEN_STEP_UP=true 才跑，默认 skip）。

const ADR_STEP_UP_REQUIRED = JSON.stringify({
  error: 'step_up_required',
  error_description: 'step-up authentication required to mint a token',
})
const ADR_STEP_UP_INVALID = JSON.stringify({
  error: 'step_up_invalid',
  error_description: 'step-up credential rejected, expired, or already used',
})

/** 运行期「零新端点」对账的闭集：页面 + 壳的全部合法 API 面（会话探活、
 * 版本、token REST 两端点）。出现集外路径 = FAIL（AC2 断言本体）。 */
const ENDPOINT_ALLOWLIST = new Set([
  '/binflow/api/v1/session',
  '/binflow/api/system/version',
  '/binflow/api/security/token',
  '/binflow/api/security/token/revoke',
])

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

/** Bearer 臂对账：以铸出的令牌问 whoami（GET /api/v1/session 认任意臂）
 * ——UI 说「铸了 X 主体」，让服务端复核主体真身；吊销后同问 = 401。 */
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

/** 管理员 REST 直吊（收尾卫生——不留活探针凭据；E-18 form 体。node 侧
 * fetch + admin Basic——m8Client 的 request 类型面无 string raw 形态） */
async function revokeViaAdmin(tokenId: number): Promise<number> {
  const base = String(process.env.BASE ?? 'http://127.0.0.1:8080').replace(/\/+$/, '')
  const auth = Buffer.from(`${process.env.ADMIN_USER ?? 'admin'}:${process.env.ADMIN_PW ?? 'password'}`).toString(
    'base64',
  )
  const res = await fetch(`${base}/binflow/api/security/token/revoke`, {
    method: 'POST',
    headers: { Authorization: `Basic ${auth}`, 'Content-Type': 'application/x-www-form-urlencoded' },
    body: `token_id=${tokenId}`,
  })
  return res.status
}

/** mint 一枚（UI 链路）并等待一次性明文面板——返回 { token, tokenId } */
async function mintViaUi(page: Page): Promise<{ token: string; tokenId: number }> {
  await page.click('[data-testid="token-create"]')
  await expect(page.locator('[data-testid="token-dialog"]')).toBeVisible()
  await page.click('[data-testid="token-submit"]')
  await expect(page.locator('[data-testid="token-plaintext"]')).toBeVisible({ timeout: 15_000 })
  const token = ((await page.locator('[data-testid="token-value"]').textContent()) ?? '').trim()
  const tokenId = Number(
    (((await page.locator('[data-testid="token-plaintext"]').textContent()) ?? '').match(/#(\d+)/) ?? [])[1],
  )
  return { token, tokenId }
}

// ---- ① 创建 modal 全链：一次性明文 → 台账行 → 关闭/刷新不可再取 + 零新端点 ----

test('tokens: admin mint chain — one-time plaintext, ledger row, close+reload kill it, endpoint budget', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/tokens')

  // 路由真身（占位锚退役的运行期面：真身页根在场即承载已换——全量
  // grep=0 + 锚册 §10.6 退役登记是 AC2 的静态证据）
  await expect(page.locator('[data-testid="tokens-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="tokens-ledger-note"]')).toBeVisible()

  // 运行期端点审计（AC2）：自真身页就绪起，本页发出的全部 API 路径必须
  // ⊆ 既有闭集（壳探活两枚 + token REST 两枚）。登录落点 /artifacts 的树
  // 面请求（repositories / storage stats / 登录页 SSO 探针）不在窗口内。
  const seen = new Set<string>()
  page.on('request', (req) => {
    const url = new URL(req.url())
    if (url.pathname.startsWith('/binflow/api/')) seen.add(url.pathname)
  })

  // 空态（会话台账空）+ 空态 CTA
  await expect(page.locator('[data-testid="tokens-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="token-create-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="token-table"]')).toHaveCount(0)

  // 创建 modal（Dialog sm）+ 暂行字段集（Q4 未裁——断言在案）：
  // admin 见「签发对象」与「永不过期」；scope 为只读说明非选项
  await page.click('[data-testid="token-create"]')
  const dialog = page.locator('[data-testid="token-dialog"]')
  await expect(dialog).toBeVisible()
  await expect(page.locator('[data-testid="token-form-subject"]')).toBeVisible()
  await expect(page.locator('[data-testid="token-form-ttl"]')).toHaveValue('86400') // 默认 24h
  const ttlOptions = await page.locator('[data-testid="token-form-ttl"] option').evaluateAll((els) =>
    els.map((e) => (e as HTMLOptionElement).value),
  )
  expect(ttlOptions).toEqual(['3600', '86400', '604800', '2592000', '31536000', '0']) // 含永不过期（admin）
  await expect(dialog).toContainText('api:*') // scope 只读说明（非选项）

  // 生成 → 一次性明文面板（64 hex mono + 拷贝 + 警示文案）
  await page.click('[data-testid="token-submit"]')
  const panel = page.locator('[data-testid="token-plaintext"]')
  await expect(panel).toBeVisible({ timeout: 15_000 })
  const { token, tokenId } = await (async () => {
    const token = ((await page.locator('[data-testid="token-value"]').textContent()) ?? '').trim()
    const tokenId = Number(((await panel.textContent())?.match(/#(\d+)/) ?? [])[1])
    return { token, tokenId }
  })()
  expect(token).toMatch(/^[0-9a-f]{64}$/)
  await expect(panel).toContainText('不可再查看')
  expect(Number.isFinite(tokenId)).toBe(true)

  // 台账行落地：指纹 = sha256 前 8 hex（与服务端审计 detail 同 digest；
  // 单元格内还有拷贝钮字形——文本锚定用前缀正则）
  const row = page.locator(`[data-testid="token-row-${tokenId}"]`)
  await expect(row).toBeVisible()
  const fp = createHash('sha256').update(token).digest('hex').slice(0, 8)
  await expect(page.locator(`[data-testid="token-fingerprint-${tokenId}"]`)).toHaveText(
    new RegExp(`^${fp}\\b`),
  )
  await expect(page.locator(`[data-testid="token-status-${tokenId}"]`)).toHaveText('有效')
  await expect(row).toContainText('admin') // 主体 = 签发会话
  await expect(page.locator('[data-testid="tokens-count"]')).toContainText('1 条会话台账')

  // Bearer 对账：铸出的令牌真的活着、主体真是 admin
  const alive = await whoamiVia(page, token)
  expect(alive.status).toBe(200)
  expect(alive.username).toBe('admin')

  // 「我已保存」关闭 → 明文退场（面板 + 全页不残留）
  await page.click('[data-testid="token-plaintext-done"]')
  await expect(page.locator('[data-testid="token-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="token-plaintext"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="tokens-page"]')).not.toContainText(token)

  // 重开 modal = 全新表单（无明文残留）；取消关闭
  await page.click('[data-testid="token-create"]')
  await expect(page.locator('[data-testid="token-submit"]')).toBeVisible()
  await expect(page.locator('[data-testid="token-plaintext"]')).toHaveCount(0)
  await page.click('[data-testid="token-cancel"]')
  await expect(page.locator('[data-testid="token-dialog"]')).toHaveCount(0)

  // 刷新：明文不可再取 + 会话台账即空（§9-R6 无清单端点的如实降形）
  await page.reload()
  await expect(page.locator('[data-testid="tokens-empty"]')).toBeVisible()
  await expect(page.locator(`[data-testid="token-row-${tokenId}"]`)).toHaveCount(0)
  await expect(page.locator('[data-testid="tokens-page"]')).not.toContainText(token)
  // 令牌本身仍有效（刷新只清 UI 台账，不吊销）
  const still = await whoamiVia(page, token)
  expect(still.status).toBe(200)

  // 零新端点对账（AC2）：窗口内全部 API 路径 ⊆ 既有闭集
  const outside = [...seen].filter((p) => !ENDPOINT_ALLOWLIST.has(p))
  expect(outside, '页面只消费既有 token REST + 壳探活闭集').toEqual([])
  expect(seen.has('/binflow/api/security/token')).toBe(true)

  // 收尾：吊销探针令牌（不留活凭据）
  expect(await revokeViaAdmin(tokenId)).toBe(200)
})

// ---- ② 吊销：danger 确认（取消零副作用 / 确认翻转 + 服务端 401 对账） ------

test('tokens: revoke via danger confirm flips the row and kills the credential server-side', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/tokens')
  const minted = await mintViaUi(page)
  expect(minted.token).toMatch(/^[0-9a-f]{64}$/)
  await page.click('[data-testid="token-plaintext-done"]')

  // 行吊销 → danger 确认框（取消钮聚焦 = 安全默认）——先取消：零副作用
  await page.click(`[data-testid="token-revoke-${minted.tokenId}"]`)
  const confirm = page.locator('[data-testid="confirm-dialog"]')
  await expect(confirm).toBeVisible()
  await expect(confirm).toContainText(`吊销令牌 #${minted.tokenId}`)
  await expect(page.locator('[data-testid="confirm-accept"]')).toHaveClass(/Error/) // danger 红变体
  await expect(page.locator('[data-testid="confirm-cancel"]')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(confirm).toHaveCount(0)
  await expect(page.locator(`[data-testid="token-status-${minted.tokenId}"]`)).toHaveText('有效')
  const stillAlive = await whoamiVia(page, minted.token)
  expect(stillAlive.status).toBe(200) // 取消 = 服务端事实不动

  // 再吊销 → 确认 → 状态翻转 + Bearer 臂 401（服务端终裁对账）
  await page.click(`[data-testid="token-revoke-${minted.tokenId}"]`)
  await expect(confirm).toBeVisible()
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator(`[data-testid="token-status-${minted.tokenId}"]`)).toHaveText('已吊销', {
    timeout: 10_000,
  })
  await expect(page.locator(`[data-testid="token-revoke-${minted.tokenId}"]`)).toBeDisabled() // 已吊销行不可再吊
  const dead = await whoamiVia(page, minted.token)
  expect(dead.status).toBe(401)
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `#${minted.tokenId} 已吊销` })).toBeVisible()
  await expect(page.locator('[data-testid="tokens-count"]')).toContainText('含已吊销 1')
})

// ---- ③ 按 token_id 吊销（台账外历史令牌）+ 签发错误内联 ------------------------

test('tokens: revoke-by-id for out-of-ledger tokens; mint error surfaces inline', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/tokens')

  // 台账外历史令牌：直接经 REST 铸一枚（同一既有端点——零新端点不变）
  const res = await sessionApi(page, 'POST', '/api/security/token', {
    grant_type: 'client_credentials',
    expires_in: 3600,
  })
  expect(res.status).toBe(200)
  const minted = res.json as { access_token: string; token_id: number }
  expect(minted.access_token).toMatch(/^[0-9a-f]{64}$/)

  // 按 id 吊销：输入 + danger 确认 → toast + Bearer 401
  await expect(page.locator('[data-testid="token-revoke-byid"]')).toBeVisible()
  await page.fill('[data-testid="token-revoke-id"]', String(minted.token_id))
  await page.click('[data-testid="token-revoke-byid-go"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await page.click('[data-testid="confirm-accept"]')
  await expect(
    page.locator('[data-testid="toast"]').filter({ hasText: `#${minted.token_id} 已吊销` }),
  ).toBeVisible({ timeout: 10_000 })
  const dead = await whoamiVia(page, minted.access_token)
  expect(dead.status).toBe(401)

  // 非法输入（非数字）：主钮禁用——不产生请求
  await page.fill('[data-testid="token-revoke-id"]', 'not-a-number')
  await expect(page.locator('[data-testid="token-revoke-byid-go"]')).toBeDisabled()

  // 签发错误内联：admin 代人不存在的主体 → 错误面就地呈现（as-built =
  // 500 "token creation failed"——契约漂移登记：auth-model 3.1 语义上属
  // 「username is required or unknown」的 400 invalid_request 臂，但
  // Tokens.Issue 的 subject 查找错误未被 handler 的 ErrInvalidCredentials
  // 分支收编〔服务端 diff=0，票内不改——以能跑通的为准，日志「契约漂移」项〕）
  await page.click('[data-testid="token-create"]')
  await page.fill('[data-testid="token-form-subject"]', 'no-such-user-t386')
  await page.click('[data-testid="token-submit"]')
  const err = page.locator('[data-testid="token-mint-error"]')
  await expect(err).toBeVisible({ timeout: 10_000 })
  await expect(err).toContainText('HTTP 500')
  await expect(err).toContainText('token creation failed')
  await page.click('[data-testid="token-cancel"]')
  await expect(page.locator('[data-testid="token-dialog"]')).toHaveCount(0)
})

// ---- ④ readonly_admin 只读臂（L06）：吊销禁用/按 id 隐藏；自铸不受限 ---------

test('tokens: readonly_admin arm — revoke disabled + by-id hidden; self-mint finite-TTL only', async ({ page }) => {
  const sess = await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/admin/security/tokens')

  await expect(page.locator('[data-testid="tokens-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="tokens-readonly-note"]')).toBeVisible()
  // 只读收敛（L4 同款）：管理面写出口不渲染
  await expect(page.locator('[data-testid="token-revoke-byid"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="token-revoke-byid-go"]')).toHaveCount(0)

  // 自铸可用（Q11 自助面）：无「签发对象」（admin 专属）、无「永不过期」（Q11 护栏）
  const { token, tokenId } = await mintViaUi(page)
  expect(token).toMatch(/^[0-9a-f]{64}$/)
  const dialogText = await page.locator('[data-testid="token-dialog"]').textContent()
  expect(dialogText).not.toContain('永不过期')
  await page.click('[data-testid="token-plaintext-done"]')

  // 台账行主体 = 本人；行内吊销禁用（服务端 CapSecurityWrite 403 兜底）
  await expect(page.locator(`[data-testid="token-row-${tokenId}"]`)).toContainText(sess.username)
  await expect(page.locator(`[data-testid="token-revoke-${tokenId}"]`)).toBeDisabled()

  // 服务端对账：自铸令牌活着、主体 = readonly 用户；403 兜底实证（REST 直吊）
  const alive = await whoamiVia(page, token)
  expect(alive.status).toBe(200)
  expect(alive.username).toBe(sess.username)
  const direct = await page.evaluate(async (id) => {
    const res = await fetch('/binflow/api/security/token/revoke', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: `token_id=${id}`,
    })
    return res.status
  }, tokenId)
  expect(direct).toBe(403) // 只读臂的服务端事实

  // 收尾：管理员 REST 吊销探针令牌
  expect(await revokeViaAdmin(tokenId)).toBe(200)
})

// ---- ⑤ step-up 内联腿（mock 拦截：ADR 逐字错误体 + 第三次放行真铸） ----------

test('tokens step-up: 401 required -> inline password form; invalid -> verbatim ADR text; retry mints', async ({
  page,
}) => {
  // readonly_admin = 非 admin web session 臂（step-up 的作用域）
  const sess = await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/admin/security/tokens')
  await page.click('[data-testid="token-create"]')

  // 拦截铸币端点：① 401 required（ADR 逐字）② 错口令 401 invalid（ADR 逐字）
  // ③ 放行真服务端（默认实例 step-up 关 → 200 真铸）
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

  // ① 首次签发 → 内联口令框（不弹第二层 modal）+ 聚焦
  await page.click('[data-testid="token-submit"]')
  await expect(page.locator('[data-testid="token-stepup"]')).toBeVisible()
  await expect(page.locator('[data-testid="token-password"]')).toBeFocused()
  await expect(page.locator('[data-testid="token-plaintext"]')).toHaveCount(0)
  await expect(page.locator('[role="dialog"]')).toHaveCount(1) // 内联呈现，无第二层

  // ② 错误口令 → 内联错误 = error_description 逐字
  await page.fill('[data-testid="token-password"]', 'definitely-wrong-password')
  await page.click('[data-testid="token-password-submit"]')
  await expect(page.locator('[data-testid="token-password-error"]')).toHaveText(
    'step-up credential rejected, expired, or already used',
  )
  await expect(page.locator('[data-testid="token-stepup"]')).toHaveCount(1) // 仍在原 modal

  // ③ 正确口令 → 续铸成功（请求体携 step_up_password + expires_in）
  await page.fill('[data-testid="token-password"]', sess.password)
  await page.click('[data-testid="token-password-submit"]')
  await expect(page.locator('[data-testid="token-plaintext"]')).toBeVisible({ timeout: 15_000 })
  expect(calls).toBe(3)
  const third = bodies[2] as { step_up_password?: string; expires_in?: number }
  expect(third.step_up_password).toBe(sess.password)
  expect(third.expires_in).toBe(86400)

  // 台账行落地后收尾（管理员 REST 吊销，不留活凭据）
  const tokenId = Number(
    ((await page.locator('[data-testid="token-plaintext"]').textContent()) ?? '').match(/#(\d+)/)?.[1],
  )
  await page.click('[data-testid="token-plaintext-done"]')
  expect(await revokeViaAdmin(tokenId)).toBe(200)
})

// ---- ⑥ step-up 真实 armed 实例腿（BINFLOW_AUTH_TOKEN_STEP_UP=true 才跑） ------

test('tokens step-up (real armed instance): full chain against the live gate', async ({ page }) => {
  const sess = await loginAs(page, 'readonly_admin')
  // 探针：armed 实例的非 admin session 签发答 401 step_up_required；默认
  // 实例直接 200 → skip（mock 腿已覆盖默认实例）
  const probe = await sessionApi(page, 'POST', '/api/security/token', {
    grant_type: 'client_credentials',
    expires_in: 3600,
  })
  test.skip(
    probe.status !== 401 || (probe.json as { error?: string })?.error !== 'step_up_required',
    'instance not armed with auth.token_step_up (covered by the mocked leg)',
  )
  if (probe.status === 200) {
    // 默认实例：探针已铸出一枚——顺手吊销再 skip（不留凭据）
    const p = probe.json as { token_id?: number }
    if (p.token_id) await revokeViaAdmin(p.token_id)
  }

  await page.goto('/binflow/ui/admin/security/tokens')
  await page.click('[data-testid="token-create"]')
  await page.click('[data-testid="token-submit"]')
  await expect(page.locator('[data-testid="token-stepup"]')).toBeVisible()

  // 真服务端错口令 → ADR-0027 决策 5 逐字（服务端原文，不由 UI 改写）
  await page.fill('[data-testid="token-password"]', 'wrong-password-for-probe')
  await page.click('[data-testid="token-password-submit"]')
  await expect(page.locator('[data-testid="token-password-error"]')).toHaveText(
    'step-up credential rejected, expired, or already used',
  )

  // 正确口令 → 真铸成功 + 台账行
  await page.fill('[data-testid="token-password"]', sess.password)
  await page.click('[data-testid="token-password-submit"]')
  await expect(page.locator('[data-testid="token-plaintext"]')).toBeVisible({ timeout: 15_000 })
  const tokenId = Number(
    ((await page.locator('[data-testid="token-plaintext"]').textContent()) ?? '').match(/#(\d+)/)?.[1],
  )
  await page.click('[data-testid="token-plaintext-done"]')
  await expect(page.locator(`[data-testid="token-row-${tokenId}"]`)).toBeVisible()
  expect(await revokeViaAdmin(tokenId)).toBe(200)
})

// ---- ⑦ 焦点链：modal/确认双形态（AC3——键盘全流程） -----------------------------

test('tokens: focus chains — modal trap/Esc-return and confirm cancel-default/Esc-cancel', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/tokens')

  // 创建 modal：连打 Tab 十次焦点仍在 modal 内；Esc 关闭 + 回焦启动钮
  await page.click('[data-testid="token-create"]')
  await expect(page.locator('[data-testid="token-dialog"]')).toBeVisible()
  for (let i = 0; i < 10; i++) await page.keyboard.press('Tab')
  const trapped = await page.evaluate(() => !!document.activeElement?.closest('[data-testid="token-dialog"]'))
  expect(trapped, 'focus stays trapped inside the modal').toBe(true)
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="token-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="token-create"]')).toBeFocused()

  // 确认形态：铸一枚 → 行吊销 → 确认框开即聚焦取消（安全默认）；Tab →
  // accept；Esc = 取消（状态不翻转、服务端不动）
  await mintViaUi(page)
  const tokenId = Number(
    ((await page.locator('[data-testid="token-plaintext"]').textContent()) ?? '').match(/#(\d+)/)?.[1],
  )
  await page.click('[data-testid="token-plaintext-done"]')
  await page.click(`[data-testid="token-revoke-${tokenId}"]`)
  const confirm = page.locator('[data-testid="confirm-dialog"]')
  await expect(confirm).toBeVisible()
  await expect(page.locator('[data-testid="confirm-cancel"]')).toBeFocused()
  await page.keyboard.press('Tab') // cancel -> accept
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeFocused()
  await page.keyboard.press('Escape') // Esc = 取消
  await expect(confirm).toHaveCount(0)
  await expect(page.locator(`[data-testid="token-status-${tokenId}"]`)).toHaveText('有效')

  // 键盘全流程真吊销：行钮 Enter → Tab+Enter 确认
  await page.focus(`[data-testid="token-revoke-${tokenId}"]`)
  await page.keyboard.press('Enter')
  await expect(confirm).toBeVisible()
  await page.keyboard.press('Tab')
  await page.keyboard.press('Enter')
  await expect(page.locator(`[data-testid="token-status-${tokenId}"]`)).toHaveText('已吊销', { timeout: 10_000 })
})

// ---- ⑧ axe 双主题：页面闭态 + modal 明文开态（serious/critical = 0） ----------

test('axe: tokens page + plaintext modal clean in both themes', async ({ page }, testInfo) => {
  test.setTimeout(300_000) // 4 面 × axe 在默认并发下实测 30s+/面
  await loginAs(page, 'admin')

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/security/tokens')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)

    // 闭态（空台账 + 两注记）
    await expect(page.locator('[data-testid="tokens-empty"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="tokens-page"]' })

    // 开态（生成表单 → 明文面板——a11y-sweep 路由面只见闭态，开态是本票新增面）
    await page.click('[data-testid="token-create"]')
    await page.click('[data-testid="token-submit"]')
    await expect(page.locator('[data-testid="token-plaintext"]')).toBeVisible({ timeout: 15_000 })
    await expect(page.locator('[data-testid="token-dialog"]')).toHaveCSS('opacity', '1')
    await expectA11yClean(page, testInfo, { include: '[data-testid="token-dialog"]' })
    // 收尾：本轮铸出的令牌即轮内吊销（token_id 自面板文本取），不留活凭据
    const tokenId = Number(
      ((await page.locator('[data-testid="token-plaintext"]').textContent()) ?? '').match(/#(\d+)/)?.[1],
    )
    await page.click('[data-testid="token-plaintext-done"]')
    expect(await revokeViaAdmin(tokenId)).toBe(200)
  }
})
