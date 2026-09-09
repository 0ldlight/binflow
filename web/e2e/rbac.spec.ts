import { expect, test } from '@playwright/test'

// T-218 探针（PRD v1.1 FR-66，V12~V14）：控制台角色下拉 / manage 复选 /
// readonly_admin 只读态——真后端全流程，admin 凭据沿用既有探针约定
// （ADMIN_PW 默认 password）。
//
// V12 admin 在 UI 把用户设为 readonly_admin → API 回显 adminRole
// V13 readonly_admin 登录走查只读态（仓库/用户/组/权限/审计可见、编辑动作
//     无 UI 入口）+ 直接重放写请求仍 403（服务端兜底——UI 只是呈现层）
// V14 权限编辑器 principals 勾选 manage 保存 → API 回显 m 位（r/w/d/m 序）
//
// 并行安全：实体名全部 uniq()；对账腿走 page.evaluate fetch（同源 session）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

type Page = import('@playwright/test').Page

async function login(page: Page, username: string, password: string) {
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** 同源 fetch（携带 session cookie）；返回 {status, text, json} */
async function api(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; text: string; json: unknown }> {
  const r = await page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : body,
      })
      return { status: res.status, text: await res.text() }
    },
    { method, path, body },
  )
  let json: unknown = null
  try {
    json = JSON.parse(r.text)
  } catch {
    // 纯文本/空体
  }
  return { ...r, json }
}

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 从 errors[] 信封或纯文本提取 message（403 文案断言用） */
function msgOf(r: { json: unknown; text: string }): string {
  const j = r.json as { errors?: { message?: string }[] } | null
  return j?.errors?.[0]?.message ?? r.text.trim()
}

test('V12: admin sets a user to readonly_admin in the UI; API echoes adminRole (snake) + audit trail', async ({ page }) => {
  const user = uniq('alice')
  await page.goto('/binflow/ui/admin/security/users')
  await login(page, ADMIN, ADMIN_PW)

  // 备料：普通用户（wire 初始态 user / admin:false）
  expect(
    (
      await api(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: 'v12-probe-pw',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)

  // GET 回显：下拉值 = wire snake（user）
  await page.goto(`/binflow/ui/admin/security/users/${user}`)
  const role = page.locator('[data-testid="user-form-role"]')
  await expect(role).toBeVisible()
  await expect(role).toHaveValue('user')
  await expect(page.locator('[data-testid="user-facts-role"]')).toHaveText('user')

  // UI 改选 readonly_admin → 保存（走既有部分更新通道）
  await role.selectOption('readonly_admin')
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已更新` })).toBeVisible({ timeout: 8000 })

  // API 对账：adminRole 三值 snake 回显；admin 布尔镜像 false；再降回 user 的回显腿
  const got = await api(page, 'GET', `/api/security/users/${user}`)
  expect(got.status).toBe(200)
  const uj = got.json as { adminRole: string; admin: boolean }
  expect(uj.adminRole).toBe('readonly_admin')
  expect(uj.admin).toBe(false)

  // UI 回显（reload 后下拉仍指 readonly_admin——GET 回显而非本地态）
  await page.reload()
  await expect(page.locator('[data-testid="user-form-role"]')).toHaveValue('readonly_admin')

  // 角色变更落审计（FR-64-AC6 的控制台侧对账：action=user.role.change，detail 带目标用户）
  const trail = await api(page, 'GET', `/api/v1/audit?action=user.role.change&limit=20`)
  expect(trail.status).toBe(200)
  const events = (trail.json as { events: { actor: string; action: string; detail: unknown }[] }).events
  expect(events.length).toBeGreaterThan(0)
  expect(events.some((e) => e.actor === ADMIN && JSON.stringify(e.detail).includes(user))).toBe(true)
})

test('V13: readonly_admin walk — admin pages visible, no write entry, replayed writes stay 403 server-side', async ({ page, browser }) => {
  const roName = uniq('roa')
  const group = uniq('rog')
  const repo = uniq('t218r')
  const target = uniq('tgt')
  await page.goto('/binflow/ui/')
  await login(page, ADMIN, ADMIN_PW)

  // 备料（admin 面）：组 + readonly_admin 用户 + generic 仓 + target（含该用户 read 行）
  expect((await api(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 'v13' })).status).toBe(201)
  expect(
    (
      await api(page, 'PUT', `/api/security/users/${roName}`, {
        name: roName,
        email: `${roName}@example.com`,
        password: 'v13-roa-pw',
        admin: false,
        adminRole: 'readonly_admin', // 与 admin:false 一致（true ⇔ admin）——冲突即 400
        groups: [],
      })
    ).status,
  ).toBe(201)
  expect((await api(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect(
    (
      await api(page, 'POST', '/api/v1/permissions', {
        name: target,
        repos: [repo],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: { [roName]: ['read'] }, groups: {} },
      })
    ).status,
  ).toBe(201)

  // readonly_admin 会话（独立上下文）
  const origin = new URL(page.url()).origin
  const roCtx = await browser.newContext()
  const ro = await roCtx.newPage()
  await ro.goto(`${origin}/binflow/ui/`)
  await login(ro, roName, 'v13-roa-pw')

  // 导航（P3 四分组 IA）：双模式切换退役（nav-mode-switch 随 P2 退役），
  // readonly_admin 直见 安全/管理 分组（读面全量）+ 会话「只读」徽章
  await expect(ro.locator('.nav-group-label', { hasText: '安全' })).toBeVisible()
  await expect(ro.locator('.nav-group-label', { hasText: '管理' })).toBeVisible()
  await expect(ro.locator('[data-testid="session-readonly-badge"]')).toBeVisible()

  // 仓库页：列表可见、只读注记、无「创建仓库」入口
  await ro.goto(`${origin}/binflow/ui/admin/repositories/local`)
  await expect(ro.locator('[data-testid="repos-page"]')).toBeVisible()
  await expect(ro.locator('[data-testid="repos-readonly-note"]')).toBeVisible()
  await expect(ro.locator('[data-testid="repos-create"]')).toHaveCount(0)

  // 用户页：列表可见、只读注记、无「创建用户」入口
  await ro.goto(`${origin}/binflow/ui/admin/security/users`)
  await expect(ro.locator('[data-testid="users-readonly-note"]')).toBeVisible()
  await expect(ro.locator('[data-testid="users-create"]')).toHaveCount(0)
  await expect(ro.locator(`[data-testid="user-row-${roName}"]`)).toBeVisible()
  await expect(ro.locator(`[data-testid="user-row-${roName}"] .badge`, { hasText: 'readonly_admin' })).toBeVisible()

  // 用户详情：GET 回显只读呈现——角色下拉禁用且值 = readonly_admin，保存禁用
  await ro.goto(`${origin}/binflow/ui/admin/security/users/${roName}`)
  await expect(ro.locator('[data-testid="user-form-role"]')).toHaveValue('readonly_admin')
  await expect(ro.locator('[data-testid="user-form-role"]')).toBeDisabled()
  await expect(ro.locator('[data-testid="user-form-submit"]')).toBeDisabled()
  await expect(ro.locator('[data-testid="user-form-readonly-note"]')).toBeVisible()

  // 组页：可见、无创建/编辑/删除入口
  await ro.goto(`${origin}/binflow/ui/admin/security/groups`)
  await expect(ro.locator('[data-testid="groups-readonly-note"]')).toBeVisible()
  await expect(ro.locator('[data-testid="groups-create"]')).toHaveCount(0)
  await expect(ro.locator(`[data-testid="group-row-${group}"]`)).toBeVisible()
  await expect(ro.locator(`[data-testid="group-delete-${group}"]`)).toHaveCount(0)

  // 权限页 + 编辑器深链：可见、只读呈现（矩阵复选禁用、无保存/删除）
  await ro.goto(`${origin}/binflow/ui/admin/security/permissions`)
  await expect(ro.locator('[data-testid="perms-readonly-note"]')).toBeVisible()
  await expect(ro.locator('[data-testid="perms-create"]')).toHaveCount(0)
  await expect(ro.locator(`[data-testid="perm-row-${target}"]`)).toBeVisible()
  await ro.goto(`${origin}/binflow/ui/admin/security/permissions/${target}`)
  await expect(ro.locator('[data-testid="perm-editor-readonly-note"]')).toBeVisible()
  await expect(ro.locator('[data-testid="perm-save"]')).toHaveCount(0)
  await expect(ro.locator('[data-testid="perm-danger-zone"]')).toHaveCount(0)
  await expect(ro.locator(`[data-testid="perm-matrix-cell-user-${roName}-read"]`)).toBeChecked()
  await expect(ro.locator(`[data-testid="perm-matrix-cell-user-${roName}-manage"]`)).toBeDisabled()

  // 审计页：可见（system:read）；词表含 user.role.change（T-215 移交项的前端镜像）
  await ro.goto(`${origin}/binflow/ui/admin/governance/audit`)
  await expect(ro.locator('[data-testid="audit-table"], [data-testid="empty-state"]').first()).toBeVisible()
  await expect(ro.locator('[data-testid="audit-filter-action"] option[value="user.role.change"]')).toHaveCount(1)

  // 服务端兜底：同一会话直接重放写请求——403 原文（UI 只是呈现层，无绕过）
  const w1 = await api(ro, 'PUT', `/api/repositories/${uniq('nope')}`, { rclass: 'local', packageType: 'generic' })
  expect(w1.status).toBe(403)
  expect(msgOf(w1)).toContain('administrator privileges')
  const w2 = await api(ro, 'POST', `/api/security/users/${roName}`, { name: roName, email: 'x@example.com' })
  expect(w2.status).toBe(403)
  expect(msgOf(w2)).toContain('administrator privileges')
  const w3 = await api(ro, 'DELETE', `/api/security/groups/${group}`)
  expect(w3.status).toBe(403)
  const w4 = await api(ro, 'POST', '/api/v1/permissions', {
    name: uniq('evil'),
    repos: [repo],
    includePatterns: ['**'],
    excludePatterns: [],
    principals: { users: { [roName]: ['read', 'write', 'delete', 'manage'] }, groups: {} },
  })
  expect(w4.status).toBe(403)
  expect(msgOf(w4)).toContain('administrator privileges')
  // 写面全灭的同时读面存活（GET 200——只读态不是 403 姿态）
  const r1 = await api(ro, 'GET', '/api/v1/permissions')
  expect(r1.status).toBe(200)
  await roCtx.close()
})

test('V14: manage checkbox round-trip in the principals matrix; API echoes the m bit in r/w/d/m order', async ({ page }) => {
  const carol = uniq('carol')
  const repo = uniq('t218m')
  const target = uniq('t-app')
  await page.goto('/binflow/ui/')
  await login(page, ADMIN, ADMIN_PW)

  // 备料：用户 + 仓 + target（carol 行仅 read——manage 位勾选是本腿主角）
  expect(
    (
      await api(page, 'PUT', `/api/security/users/${carol}`, {
        name: carol,
        email: `${carol}@example.com`,
        password: 'v14-carol-pw',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)
  expect((await api(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect(
    (
      await api(page, 'POST', '/api/v1/permissions', {
        name: target,
        repos: [repo],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: { [carol]: ['read'] }, groups: {} },
      })
    ).status,
  ).toBe(201)

  // 编辑器：矩阵含 manage 列；勾选 manage → diff 确认 → 保存
  await page.goto(`/binflow/ui/admin/security/permissions/${target}`)
  await expect(page.locator('[data-testid="perm-matrix"] th', { hasText: 'manage' })).toHaveCount(1)
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${carol}-read"]`)).toBeChecked()
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${carol}-manage"]`)).not.toBeChecked()
  await page.check(`[data-testid="perm-matrix-cell-user-${carol}-manage"]`)
  await page.click('[data-testid="perm-save"]')
  const diff = page.locator('[data-testid="perm-diff"]')
  await expect(diff).toContainText(`授予用户 ${carol} manage`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })

  // API 对账：GET /api/v1/permissions 回显 m 位，动作序 r/w/d/m（read 后随 manage）
  const got = await api(page, 'GET', '/api/v1/permissions')
  expect(got.status).toBe(200)
  const t = (got.json as { name: string; principals: { users: Record<string, string[]> } }[]).find(
    (x) => x.name === target,
  )
  expect(t, `target ${target} must exist`).toBeTruthy()
  expect(t!.principals.users[carol]).toEqual(['read', 'manage'])

  // UI 回显（reload 后 manage 复选仍勾——wire 往返而非本地态）
  await page.goto(`/binflow/ui/admin/security/permissions/${target}`)
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${carol}-manage"]`)).toBeChecked()
})
