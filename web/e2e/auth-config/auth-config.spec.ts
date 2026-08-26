import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { makeClient, roleFixturesFromEnv } from '../m8/support/seed'

// T-307 (M11 FR-92 FE 腿): the admin authentication-configuration page group —
// three protocol tabs over the T-305 nine-endpoint plane. Anchors from
// console-ux §10.5 (T-307 batch, v1.12).
//
// Legs:
//   CFG1  LDAP round-trip + the leave-empty-keeps-secret semantics at the
//         NETWORK layer (PUT body omits the empty secret; the 20-star
//         sentinel is never sent back — it is the 400 red line)
//   CFG2  sentinel negative leg: a direct PUT carrying the sentinel is
//         refused with the anchored message (UI never does this)
//   CFG3  OAuth(OIDC) round-trip + the live-discovery write refusal
//   CFG4  SAML anchored {} empty state + noAutoUserCreation reverse-wire
//         checkbox + the certificate-parse refusal on enable
//   CFG5  test-connection both forms (candidate = current form values,
//         stored = empty body) against a mocked probe backend, success and
//         failure verdicts rendered verbatim
//   CFG6  postures: readonly_admin read-only (disabled + counter-assertions,
//         REST PUT 403), plain user navigation-unreachable + direct-link L2
//   CFG7  axe dual-theme on all three tabs, serious+critical = 0
//
// Shared-state note: the section model is one row per protocol per instance
// (no DELETE), so this file runs SERIAL — parallel legs inside the file would
// clobber each other's section state. Cross-file: nothing else touches the
// /admin/security/* config plane. Restore: afterEach PUTs back the captured
// GET docs (sentinel secrets stripped — absent = keep), with an enabled=false
// fallback for the pristine-default LDAP doc (enabled+empty ldapUrl is not a
// validatable write).

test.describe.configure({ mode: 'serial' })

const SENTINEL = '********************'
const SECTION_PATHS: Record<string, string> = {
  ldap: '/binflow/api/v1/admin/security/ldap',
  oauth: '/binflow/api/v1/admin/security/oauth',
  saml: '/binflow/api/v1/admin/security/saml/config',
}

function adminClient() {
  const roles = roleFixturesFromEnv()
  return makeClient({ base: process.env.BASE ?? 'http://127.0.0.1:8080', username: roles.admin.name, password: roles.admin.password })
}

/** Strip sentinel secret fields from a captured GET doc so the restore PUT
 * means "keep the stored secret" (absent), never the forbidden echo. */
function stripSentinels(doc: Record<string, unknown>): Record<string, unknown> {
  const out = structuredClone(doc)
  if (out && typeof out === 'object') {
    const search = (out as { search?: Record<string, unknown> }).search
    if (search && search.managerPassword === SENTINEL) delete search.managerPassword
    if ((out as { client_secret?: unknown }).client_secret === SENTINEL) delete (out as Record<string, unknown>).client_secret
  }
  return out
}

async function captureSections(): Promise<Record<string, Record<string, unknown>>> {
  const client = adminClient()
  const snap: Record<string, Record<string, unknown>> = {}
  for (const [id, path] of Object.entries(SECTION_PATHS)) {
    const r = await client.request('GET', path)
    snap[id] = JSON.parse(r.text) as Record<string, unknown>
  }
  return snap
}

async function restoreSections(snap: Record<string, Record<string, unknown>>): Promise<void> {
  const client = adminClient()
  for (const [id, path] of Object.entries(SECTION_PATHS)) {
    const doc = stripSentinels(snap[id] ?? {})
    // The anchored never-saved state ({}, SAML §3.2) has no DELETE-side
    // restore — a PUT of {} would CANONICALIZE into a stored defaults row
    // and destroy the CFG4 empty-state leg on this instance. Leave it.
    if (Object.keys(doc).length === 0) continue
    try {
      await client.request('PUT', path, { body: doc })
    } catch {
      // The pristine LDAP default (enabled+empty URL) is not a validatable
      // write — land the disabled form of the same doc instead.
      await client.request('PUT', path, { body: { ...doc, enabled: false } }).catch(() => undefined)
    }
  }
}

let snapshots: Record<string, Record<string, unknown>> | null = null

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  snapshots = await captureSections()
})

test.afterEach(async () => {
  if (snapshots) await restoreSections(snapshots)
  snapshots = null
})

/** Grab the next PUT body to one section (network-layer assertion seam). */
async function nextPut(page: import('@playwright/test').Page, urlPart: string): Promise<Record<string, unknown>> {
  const req = await page.waitForRequest((r) => r.method() === 'PUT' && r.url().includes(urlPart))
  return (req.postDataJSON() ?? {}) as Record<string, unknown>
}

// ---------------------------------------------------------------------------
// CFG0 — field inventory sweep: every T-307 field anchor renders on its tab,
//         checkboxes toggle, reset restores the server baseline
// ---------------------------------------------------------------------------

/** 本腿专项扫的剩余锚（其余 T-307 字段锚已由 CFG1~CFG6 逐名消费；字面量
 *  选择器形态——对账器 spec 口径可见，模板透传形对其不可见〔工具局限史〕） */
const SWEEP_TEXT: Record<string, string[]> = {
  ldap: [
    '[data-testid="authcfg-ldap-key"]', '[data-testid="authcfg-ldap-enabled"]',
    '[data-testid="authcfg-ldap-autocreate"]', '[data-testid="authcfg-ldap-allowprofile"]',
    '[data-testid="authcfg-ldap-paging"]', '[data-testid="authcfg-ldap-emailattr"]',
    '[data-testid="authcfg-ldap-poisoning"]', '[data-testid="authcfg-ldap-search-subtree"]',
    '[data-testid="authcfg-ldap-starttls"]', '[data-testid="authcfg-ldap-skiptls"]',
    '[data-testid="authcfg-ldap-group-basedn"]', '[data-testid="authcfg-ldap-admin-group"]',
    '[data-testid="authcfg-ldap-readonly-group"]',
  ],
  oauth: [
    '[data-testid="authcfg-oauth-readonly-group"]',
  ],
  saml: [
    '[data-testid="authcfg-saml-encrypted"]', '[data-testid="authcfg-saml-allowprofile"]',
    '[data-testid="authcfg-saml-autoredirect"]',
  ],
}

/** 复选字段锚（toggle 双翻回原值——不落库） */
const SWEEP_CHECKS: Record<string, string[]> = {
  ldap: [
    '[data-testid="authcfg-ldap-enabled"]', '[data-testid="authcfg-ldap-autocreate"]',
    '[data-testid="authcfg-ldap-allowprofile"]', '[data-testid="authcfg-ldap-paging"]',
    '[data-testid="authcfg-ldap-poisoning"]', '[data-testid="authcfg-ldap-search-subtree"]',
    '[data-testid="authcfg-ldap-starttls"]', '[data-testid="authcfg-ldap-skiptls"]',
  ],
  oauth: [
    '[data-testid="authcfg-oauth-enabled"]', '[data-testid="authcfg-oauth-autocreate"]',
  ],
  saml: [
    '[data-testid="authcfg-saml-enabled"]', '[data-testid="authcfg-saml-encrypted"]',
    '[data-testid="authcfg-saml-syncgroups"]', '[data-testid="authcfg-saml-autocreate"]',
    '[data-testid="authcfg-saml-allowprofile"]', '[data-testid="authcfg-saml-autoredirect"]',
    '[data-testid="authcfg-saml-verify-audience"]',
  ],
}

test('CFG0: field inventory — every T-307 anchor renders, checkboxes toggle, reset restores', async ({ page }) => {
  await loginAs(page, 'admin')
  for (const tab of ['ldap', 'oauth', 'saml']) {
    await page.goto(`/binflow/ui/admin/security/auth/${tab}`)
    await expect(page.locator(`[data-testid="authcfg-tab-${tab}"]`)).toHaveAttribute('aria-current', 'page')
    // 文本/锁定/数字类：在场即可见
    for (const sel of [...SWEEP_TEXT[tab], ...SWEEP_CHECKS[tab]]) {
      await expect(page.locator(sel)).toBeVisible()
    }
    // 复选类：双翻回原值（SAML autocreate 是反语义位默认未勾；
    // verify-audience/paging 等默认勾——读初值再翻回，不落库）
    for (const sel of SWEEP_CHECKS[tab]) {
      const loc = page.locator(sel)
      const was = await loc.isChecked()
      if (was) {
        await loc.uncheck()
        await expect(loc).not.toBeChecked()
        await loc.check()
        await expect(loc).toBeChecked()
      } else {
        await loc.check()
        await expect(loc).toBeChecked()
        await loc.uncheck()
        await expect(loc).not.toBeChecked()
      }
    }
  }

  // 还原（reset）：改一个字段 → 还原 → 回服务端基线（保存钮随 dirty 回落）
  await page.goto('/binflow/ui/admin/security/auth/ldap')
  const url = page.locator('[data-testid="authcfg-ldap-url"]')
  const before = await url.inputValue()
  await page.fill('[data-testid="authcfg-ldap-url"]', 'ldap://reset-probe.test:389/dc=x')
  await expect(page.locator('[data-testid="authcfg-save"]')).toBeEnabled()
  await page.click('[data-testid="authcfg-reset"]')
  await expect(url).toHaveValue(before)
  await expect(page.locator('[data-testid="authcfg-save"]')).toBeDisabled()
})

// ---------------------------------------------------------------------------
// CFG1 — LDAP round-trip + leave-empty-keeps-secret (network layer)
// ---------------------------------------------------------------------------

test('CFG1: admin — LDAP tab round-trip, empty secret omitted from PUT, sentinel never sent', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/auth')
  // 索引重定向 → ldap Tab
  await expect(page).toHaveURL(/\/admin\/security\/auth\/ldap$/)
  const root = page.locator('[data-testid="authcfg-page"]')
  await expect(root).toBeVisible()

  // Tab 三枚 + 当前 ldap；常驻「保存即生效」说明行；锁定 key
  for (const id of ['ldap', 'oauth', 'saml']) {
    await expect(page.locator(`[data-testid="authcfg-tab-${id}"]`)).toBeVisible()
  }
  await expect(page.locator('[data-testid="authcfg-tab-ldap"]')).toHaveAttribute('aria-current', 'page')
  await expect(page.locator('[data-testid="authcfg-note-effect"]')).toBeVisible()
  await expect(page.locator('[data-testid="authcfg-ldap-key"]')).toHaveText('ldap')

  // 初始（默认段）：secret 未设置
  await expect(page.locator('[data-testid="authcfg-ldap-manager-set"]')).toContainText('未设置')
  const secret = page.locator('[data-testid="authcfg-ldap-manager-pw"]')
  await expect(secret).toHaveValue('')

  // 填表：URL/搜索段/扩展组 + 两个开关翻转（enabled 显式置位——前序腿的
  // restore 兜底可能把默认段落成 enabled=false，断言要确定性）
  await page.check('[data-testid="authcfg-ldap-enabled"]')
  await page.fill('[data-testid="authcfg-ldap-url"]', 'ldap://127.0.0.1:1/dc=t307,dc=test')
  await page.fill('[data-testid="authcfg-ldap-userdn"]', '')
  await page.fill('[data-testid="authcfg-ldap-search-filter"]', '(uid={0})')
  await page.fill('[data-testid="authcfg-ldap-search-base"]', 'ou=people')
  await page.fill('[data-testid="authcfg-ldap-manager-dn"]', 'cn=admin,dc=t307,dc=test')
  await page.fill('[data-testid="authcfg-ldap-group-filter"]', '(objectClass=group)')
  await page.fill('[data-testid="authcfg-ldap-group-nameattr"]', 'cn')
  await page.fill('[data-testid="authcfg-ldap-poolsize"]', '7')
  await page.uncheck('[data-testid="authcfg-ldap-paging"]')
  await page.check('[data-testid="authcfg-ldap-allowprofile"]')

  // 保存（无 secret）：payload 不含 managerPassword 键（空=剔除=保持）
  const put1 = nextPut(page, '/admin/security/ldap')
  await page.click('[data-testid="authcfg-save"]')
  const body1 = await put1
  expect(body1).toMatchObject({
    key: 'ldap',
    enabled: true,
    ldapUrl: 'ldap://127.0.0.1:1/dc=t307,dc=test',
    pagingSupportEnabled: false,
    allowUserToAccessProfile: true,
    poolSize: 7,
  })
  expect(body1.search).toMatchObject({
    searchFilter: '(uid={0})',
    searchBase: 'ou=people',
    managerDn: 'cn=admin,dc=t307,dc=test',
    searchSubTree: true,
  })
  expect((body1.search as Record<string, unknown>).managerPassword).toBeUndefined()
  expect(JSON.stringify(body1)).not.toContain(SENTINEL)

  // 回显：值在、secret 仍未设置
  await expect(page.locator('[data-testid="authcfg-ldap-url"]')).toHaveValue('ldap://127.0.0.1:1/dc=t307,dc=test')
  await expect(page.locator('[data-testid="authcfg-ldap-manager-set"]')).toContainText('未设置')

  // —— 设置 secret 腿（实例无主密钥则跳过：密封需要 BINFLOW_REMOTE_CREDENTIALS_KEY）——
  await secret.fill('wire-secret-307')
  const put2 = nextPut(page, '/admin/security/ldap')
  await page.click('[data-testid="authcfg-save"]')
  const body2 = await put2
  expect((body2.search as Record<string, unknown>).managerPassword).toBe('wire-secret-307')

  const refused = page.locator('[data-testid="authcfg-error"]')
  if (await refused.isVisible()) {
    const text = (await refused.textContent()) ?? ''
    test.skip(text.includes('no master key'), 'instance runs without BINFLOW_REMOTE_CREDENTIALS_KEY — secret-sealing legs need it')
  }

  // GET 回显哨兵 → 表单空 + placeholder「留空保持不变」+「已设置」提示
  await expect(secret).toHaveValue('')
  await expect(secret).toHaveAttribute('placeholder', '留空保持不变')
  await expect(page.locator('[data-testid="authcfg-ldap-manager-set"]')).toContainText('已设置')

  // 再保存（secret 留空 + 顺带改 poolSize 触发 dirty）：网络层两断言——
  // 不回传哨兵、空字段整个剔除
  await page.fill('[data-testid="authcfg-ldap-poolsize"]', '9')
  const put3 = nextPut(page, '/admin/security/ldap')
  await page.click('[data-testid="authcfg-save"]')
  const body3 = await put3
  expect((body3.search as Record<string, unknown>).managerPassword).toBeUndefined()
  expect(JSON.stringify(body3)).not.toContain(SENTINEL)
  expect(body3.poolSize).toBe(9)
})

// ---------------------------------------------------------------------------
// CFG2 — sentinel negative leg (direct PUT; the UI path never does this)
// ---------------------------------------------------------------------------

test('CFG2: sentinel echo-back PUT is refused (anchored message), live config intact', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/auth/ldap')
  await expect(page.locator('[data-testid="authcfg-ldap-url"]')).toBeVisible()

  // 服务端红线（用户裁定照 Artifactory）：哨兵回传 = 400 + 锚定文案
  const before = await page.evaluate(async () => {
    const res = await fetch('/binflow/api/v1/admin/security/ldap')
    return { status: res.status, body: await res.json() }
  })
  const section = { ...(before.body as Record<string, unknown>) }
  const search = { ...((section.search as Record<string, unknown>) ?? {}) }
  search.managerPassword = SENTINEL
  section.search = search
  const put = await page.evaluate(async (body) => {
    const res = await fetch('/binflow/api/v1/admin/security/ldap', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    return { status: res.status, text: await res.text() }
  }, section)
  expect(put.status).toBe(400)
  expect(put.text).toContain('refusing the masked placeholder')

  // 拒绝不改动在用配置（verify-then-replace）
  const after = await page.evaluate(async () => (await fetch('/binflow/api/v1/admin/security/ldap')).json())
  expect(after).toEqual(before.body)
})

// ---------------------------------------------------------------------------
// CFG3 — OAuth(OIDC) round-trip + live-discovery write refusal
// ---------------------------------------------------------------------------

test('CFG3: oauth tab — snake_case round-trip, secret omission, discovery refusal keeps config', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/auth/oauth')
  await expect(page.locator('[data-testid="authcfg-tab-oauth"]')).toHaveAttribute('aria-current', 'page')

  await page.uncheck('[data-testid="authcfg-oauth-enabled"]')
  await page.fill('[data-testid="authcfg-oauth-issuer"]', 'http://127.0.0.1:1')
  await page.fill('[data-testid="authcfg-oauth-client-id"]', 't307-client')
  await page.fill('[data-testid="authcfg-oauth-redirect"]', 'https://reg.example.test/cb')
  await page.fill('[data-testid="authcfg-oauth-scopes"]', 'openid, profile')
  await page.fill('[data-testid="authcfg-oauth-user-claim"]', 'preferred_username')
  await page.fill('[data-testid="authcfg-oauth-group-claim"]', 'groups')
  await page.fill('[data-testid="authcfg-oauth-admin-group"]', 't307-admins')
  await page.uncheck('[data-testid="authcfg-oauth-autocreate"]')

  const put1 = nextPut(page, '/admin/security/oauth')
  await page.click('[data-testid="authcfg-save"]')
  const body1 = await put1
  expect(body1).toMatchObject({
    enabled: false,
    issuer_url: 'http://127.0.0.1:1',
    client_id: 't307-client',
    redirect_url: 'https://reg.example.test/cb',
    scopes: ['openid', 'profile'],
    user_claim: 'preferred_username',
    group_claim: 'groups',
    admin_group: 't307-admins',
    auto_create_users: false,
  })
  expect(body1.client_secret).toBeUndefined()

  // secret 腿（无主密钥实例跳过）
  const secret = page.locator('[data-testid="authcfg-oauth-client-secret"]')
  await secret.fill('oauth-secret-307')
  const put2 = nextPut(page, '/admin/security/oauth')
  await page.click('[data-testid="authcfg-save"]')
  const body2 = await put2
  expect(body2.client_secret).toBe('oauth-secret-307')
  const refused = page.locator('[data-testid="authcfg-error"]')
  if (await refused.isVisible()) {
    const text = (await refused.textContent()) ?? ''
    test.skip(text.includes('no master key'), 'instance runs without BINFLOW_REMOTE_CREDENTIALS_KEY — secret-sealing legs need it')
  }
  await expect(secret).toHaveValue('')
  await expect(page.locator('[data-testid="authcfg-oauth-secret-set"]')).toContainText('已设置')

  // 启用腿：写路径做真实 discovery——不可达 issuer 被拒（errors[] 信封原文）
  await page.check('[data-testid="authcfg-oauth-enabled"]')
  await page.click('[data-testid="authcfg-save"]')
  const err = page.locator('[data-testid="authcfg-error"]')
  await expect(err).toBeVisible()
  await expect(err).toContainText('discovery')
  // 拒绝不落库：GET 仍是 enabled=false（存量配置原样在位）
  const doc = await page.evaluate(async () => (await fetch('/binflow/api/v1/admin/security/oauth')).json())
  expect(doc.enabled).toBe(false)
})

// ---------------------------------------------------------------------------
// CFG4 — SAML empty state + reverse-wire checkbox + certificate refusal
// ---------------------------------------------------------------------------

test('CFG4: saml tab — {} empty-state guidance, noAutoUserCreation reverse wire, cert-parse refusal', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/auth/saml')
  await expect(page.locator('[data-testid="authcfg-tab-saml"]')).toHaveAttribute('aria-current', 'page')

  // 空态：段从未保存（复用实例上已保存过 → 跳过本腿，{} 是一次性状态）
  const empty = page.locator('[data-testid="authcfg-saml-empty"]')
  const stored = await page.evaluate(async () => {
    const doc = await (await fetch('/binflow/api/v1/admin/security/saml/config')).json()
    return Object.keys(doc).length > 0
  })
  test.skip(stored, 'saml section already saved on this instance — the anchored {} empty state is a fresh-instance leg')

  await expect(empty).toBeVisible()
  // §3.1 默认值经反语义复选呈现：noAutoUserCreation 默认 true → 未勾选；
  // verifyAudienceRestriction 默认 true → 勾选
  await expect(page.locator('[data-testid="authcfg-saml-autocreate"]')).not.toBeChecked()
  await expect(page.locator('[data-testid="authcfg-saml-verify-audience"]')).toBeChecked()

  await page.uncheck('[data-testid="authcfg-saml-enabled"]')
  await page.fill('[data-testid="authcfg-saml-spname"]', 't307-sp')
  await page.fill('[data-testid="authcfg-saml-login-url"]', 'https://idp.example.test/sso')
  await page.fill('[data-testid="authcfg-saml-logout-url"]', 'https://idp.example.test/logout')
  await page.fill('[data-testid="authcfg-saml-cert"]', 'not-a-pem')
  await page.fill('[data-testid="authcfg-saml-groupattr"]', 'memberOf')
  await page.fill('[data-testid="authcfg-saml-emailattr"]', 'mail')
  await page.check('[data-testid="authcfg-saml-syncgroups"]')

  // 反语义：勾选「Auto Create Users」→ wire noAutoUserCreation = false
  await page.check('[data-testid="authcfg-saml-autocreate"]')
  const put1 = nextPut(page, '/admin/security/saml/config')
  await page.click('[data-testid="authcfg-save"]')
  const body1 = await put1
  expect(body1).toMatchObject({
    enableIntegration: false,
    serviceProviderName: 't307-sp',
    loginUrl: 'https://idp.example.test/sso',
    logoutUrl: 'https://idp.example.test/logout',
    certificate: 'not-a-pem', // 未启集成不解析证书
    syncGroups: true,
    groupAttribute: 'memberOf',
    emailAttribute: 'mail',
    noAutoUserCreation: false,
    verifyAudienceRestriction: true,
  })

  // 保存后空态引导消失
  await expect(empty).toHaveCount(0)

  // 启用腿：enableIntegration=true 触发证书解析（§3.3 流程 1）——垃圾 PEM 400
  await page.check('[data-testid="authcfg-saml-enabled"]')
  await page.click('[data-testid="authcfg-save"]')
  const err = page.locator('[data-testid="authcfg-error"]')
  await expect(err).toBeVisible()
  await expect(err).toContainText('CERTIFICATE')

  // 取消勾选 → wire 回 true（否定式还原；同时收掉启用位，让保存可过）
  await page.uncheck('[data-testid="authcfg-saml-autocreate"]')
  await page.uncheck('[data-testid="authcfg-saml-enabled"]')
  const put2 = nextPut(page, '/admin/security/saml/config')
  await page.click('[data-testid="authcfg-save"]')
  expect((await put2).noAutoUserCreation).toBe(true)
})

// ---------------------------------------------------------------------------
// CFG5 — test connection, both forms, mocked verdicts
// ---------------------------------------------------------------------------

test('CFG5: test connection — candidate (form values + §1.6 creds) / stored (empty body), verdicts verbatim', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/auth/ldap')
  await expect(page.locator('[data-testid="authcfg-ldap-url"]')).toBeVisible()
  await page.fill('[data-testid="authcfg-ldap-url"]', 'ldap://127.0.0.1:1/dc=t307,dc=test')
  await page.fill('[data-testid="authcfg-ldap-search-filter"]', '(uid={0})')
  await page.click('[data-testid="authcfg-save"]')
  await expect(page.locator('[data-testid="authcfg-test"]')).toBeVisible()

  // 半凭据禁用（§1.6 两半齐备——表单零坏请求）
  await page.fill('[data-testid="authcfg-test-username"]', 't307-user')
  await expect(page.locator('[data-testid="authcfg-test-run"]')).toBeDisabled()
  await page.fill('[data-testid="authcfg-test-password"]', 't307-pass')
  await expect(page.locator('[data-testid="authcfg-test-run"]')).toBeEnabled()

  // mock：候选探测成功形态
  const verdicts: Array<Record<string, unknown> & { status: number }> = [
    { status: 200, ok: true, phase: 'user_bind', category: 'ok', message: 'Successfully connected and authenticated the test user' },
    { status: 400, ok: false, phase: 'dial', category: 'unreachable', message: 'could not connect to the target (dial failed or timed out)' },
    { status: 200, ok: false, phase: 'config', category: 'unset', message: 'no stored configuration for this section; submit the candidate form first' },
  ]
  let idx = 0
  const bodies: string[] = []
  await page.route('**/binflow/api/v1/admin/security/ldap/test', async (route) => {
    bodies.push(route.request().postData() ?? '')
    const v = verdicts[Math.min(idx, verdicts.length - 1)]
    idx += 1
    await route.fulfill({ status: v.status, contentType: 'application/json', body: JSON.stringify(v) })
  })

  // 候选探测：体 = 表单当前值 + testUsername/testPassword
  await page.click('[data-testid="authcfg-test-run"]')
  const report = page.locator('[data-testid="authcfg-test-report"]')
  await expect(report).toBeVisible()
  await expect(report).toContainText('Successfully connected and authenticated the test user')
  await expect(report).toContainText('user_bind')
  const sent = JSON.parse(bodies[0]) as Record<string, unknown>
  expect(sent.testUsername).toBe('t307-user')
  expect(sent.testPassword).toBe('t307-pass')
  expect(sent.ldapUrl).toBe('ldap://127.0.0.1:1/dc=t307,dc=test')

  // 失败形态：400 + TestReport 体（非 errors[] 信封）——消息原文照 BE
  await page.click('[data-testid="authcfg-test-run"]')
  await expect(report).toContainText('could not connect to the target')
  await expect(report).toContainText('unreachable')

  // 存量探测：空请求体
  await page.click('[data-testid="authcfg-test-stored"]')
  await expect(report).toContainText('no stored configuration for this section')
  expect(bodies[2]).toBe('')
})

// ---------------------------------------------------------------------------
// CFG6 — postures: readonly_admin read-only; plain user unreachable
// ---------------------------------------------------------------------------

test('CFG6: readonly_admin disabled-everything + PUT 403; plain user navigation-unreachable + L2', async ({ browser }) => {
  // —— readonly_admin：页面可见、控件全禁用（disabled + 反断言）、注记在 ——
  const ro = await (await browser.newContext()).newPage()
  await loginAs(ro, 'readonly_admin')
  await ro.goto('/binflow/ui/admin/security/auth/ldap')
  await expect(ro.locator('[data-testid="authcfg-page"]')).toBeVisible()
  await expect(ro.locator('[data-testid="app-nav"] .nav-item', { hasText: '认证配置' })).toBeVisible()
  await expect(ro.locator('[data-testid="authcfg-readonly-note"]')).toBeVisible()
  for (const anchor of [
    'authcfg-ldap-url',
    'authcfg-ldap-search-filter',
    'authcfg-ldap-manager-pw',
    'authcfg-ldap-poolsize',
    'authcfg-ldap-autocreate',
    'authcfg-save',
    'authcfg-test-run',
    'authcfg-test-stored',
    'authcfg-test-username',
  ]) {
    await expect(ro.locator(`[data-testid="${anchor}"]`)).toBeDisabled()
  }
  // 数据面可读（GET = CapSecurityRead）：表单在场即读成功（forbidden 会
  // 收敛为无权限卡、无控件）
  await expect(ro.locator('[data-testid="authcfg-ldap-url"]')).toBeVisible()
  // 写面服务端终裁：PUT 403
  const put = await ro.evaluate(async () => {
    const res = await fetch('/binflow/api/v1/admin/security/ldap', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: 'ldap', enabled: false }),
    })
    return res.status
  })
  expect(put).toBe(403)

  // —— 普通 user：导航不可达 + 直链 L2 无权限卡（无表单）——
  const user = await (await browser.newContext()).newPage()
  await loginAs(user, 'user')
  await expect(user.locator('[data-testid="app-nav"] .nav-item', { hasText: '认证配置' })).toHaveCount(0)
  await user.goto('/binflow/ui/admin/security/auth/ldap')
  await expect(user.locator('[data-testid="authcfg-page"]')).toBeVisible()
  await expect(user.locator('[data-testid="authcfg-page"] [data-testid="empty-state"]')).toBeVisible()
  await expect(user.locator('[data-testid="authcfg-ldap-url"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// CFG7 — axe dual-theme, all three tabs
// ---------------------------------------------------------------------------

test('CFG7: axe — three tabs scan clean at serious/critical in both themes', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  for (const id of ['ldap', 'oauth', 'saml']) {
    await page.goto(`/binflow/ui/admin/security/auth/${id}`)
    await expect(page.locator(`[data-testid="authcfg-tab-${id}"]`)).toHaveAttribute('aria-current', 'page')
    await expectA11yClean(page, testInfo, { include: '[data-testid="authcfg-page"]' })
  }

  // 双主题：默认主题（light 或系统偏好）扫完一轮后翻转再扫（编辑态表单；
  // disabled 态由 CFG6 覆盖）——翻转以属性变化为断言，不假设默认方向
  const before = await page.locator('html').getAttribute('data-theme')
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).not.toHaveAttribute('data-theme', before ?? '')
  await expectA11yClean(page, testInfo, { include: '[data-testid="authcfg-page"]' })
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).toHaveAttribute('data-theme', before ?? 'light')
})
