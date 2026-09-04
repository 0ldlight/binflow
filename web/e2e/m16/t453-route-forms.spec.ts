import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, sessionApi } from '../m8/support/seed'

// T-453（M16 批次④ B9，FR-145.1/.3——断言反转④：Q5 出口①路由化）：
//
//   ① /users/new、/groups/new **深链**整页表单（7.161.20 活体对位
//      /ui/admin/management/{users,groups}/new——2026-09-04 复核留痕
//      reports/agents/t453-probe/）+ 创建-列表-编辑闭环（Save 回列表）。
//   ② 列表内联展开卡**退役**断言：列表页 DOM 无 user-form / group-form；
//      「＋ 新建」= 导航入口；组编辑 = /groups/:name/edit 路由页（内联
//      卡创建/编辑同卡一并退役）。
//   ③ 能力位三旗（FR-145.3）：Can Update Profile / Disable UI Access /
//      Disable Internal Password——BE create（PUT userCreateBody）与
//      partial-update（POST userUpdateBody）均未承接 → 预留位两档
//      （T-439 先例：恒禁用 + 零提交 + hint 如实标注）。三件套断言：
//      现值默认档（profile=true / 其余=false，7.161 活体同值）+ 网络
//      payload 净度（PUT 体零携带三域）+ **API 漂移钉**（直连 PUT 携带
//      三域 → 201 而 GET 回显不动——BE 承接落地日本腿翻红即提示转正）。
//   ④ 管理位候裁臂：7.161 = Administer Platform + Manage Resources 双
//      布尔；BinFlow = 三值枚举（ADR-0026 暂行维持）——枚举下拉在场 +
//      候裁附注锚（user-form-role-parity-note）。
//   ⑤ readonly_admin 深链防御（/users/new 路由页只读呈现）+ axe 双主题
//      （组编辑路由页——创建页 axe 面在 m14/t384 腿④）。
//
// 锚源：console-ux §10.5 T-453 批（user-create-page / group-form-page /
// user-form-reserved-caps / user-form-profile-updatable / user-form-disable-ui /
// user-form-disable-internal-password / user-form-role-parity-note，
// v1.40）；既有族 user-form-* / group-form-* 载体迁移零改名。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** PUT /security/users/{name} 请求体捕获器（payload 净度断言用） */
function captureUserPuts(page: Page): { bodies: () => unknown[] } {
  const seen: unknown[] = []
  page.on('request', (r) => {
    if (r.method() !== 'PUT') return
    const url = new URL(r.url())
    if (!/^\/binflow\/api\/security\/users\/[^/]+$/.test(url.pathname)) return
    try {
      seen.push(r.postDataJSON())
    } catch {
      seen.push(null)
    }
  })
  return { bodies: () => [...seen] }
}

// ---- ①④ 用户：深链整页表单 + 能力位三旗预留位 + payload 净度 + 闭环 ------

test('admin: /users/new deep link — reserved capability flags, clean payload, create→list→edit loop', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const user = uniq('t453u')

  // 深链直达（非入口点击——URL 即状态）：整页表单在场 + 页头 + 返回列表
  await page.goto('/binflow/ui/admin/security/users/new')
  await expect(page.locator('[data-testid="user-create-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-create-page"] h2')).toHaveText('新建用户')

  // 能力位三旗（③ 预留位第一件）：默认值档 = 7.161 活体同值
  // （Can Update Profile=true / Disable UI Access=false / Disable Internal
  //  Password=false）+ 全员恒禁用（BE 未承接域，T-439 两档纪律）
  await expect(page.locator('[data-testid="user-form-reserved-caps"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-profile-updatable"]')).toBeChecked()
  await expect(page.locator('[data-testid="user-form-profile-updatable"]')).toBeDisabled()
  await expect(page.locator('[data-testid="user-form-disable-ui"]')).not.toBeChecked()
  await expect(page.locator('[data-testid="user-form-disable-ui"]')).toBeDisabled()
  await expect(page.locator('[data-testid="user-form-disable-internal-password"]')).not.toBeChecked()
  await expect(page.locator('[data-testid="user-form-disable-internal-password"]')).toBeDisabled()

  // 管理位候裁臂（④）：三值枚举下拉在场（ADR-0026 暂行）+ 双布尔差异
  // 附注锚在场（候裁挂附注——不建双布尔不伪造）
  await expect(page.locator('[data-testid="user-form-role"]')).toHaveValue('user')
  const roleOptions = page.locator('[data-testid="user-form-role"] option')
  await expect(roleOptions).toHaveCount(3)
  await expect(page.locator('[data-testid="user-form-role-parity-note"]')).toBeVisible()

  // 创建（③ payload 净度：PUT 体零携带三域——预留位零提交的网络层对账）
  const puts = captureUserPuts(page)
  await page.fill('[data-testid="user-form-name"]', user)
  await page.fill('[data-testid="user-form-email"]', `${user}@example.com`)
  await page.fill('[data-testid="user-form-password"]', 't453-pw-1')
  await page.fill('[data-testid="user-form-password2"]', 't453-pw-1')
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已创建` })).toBeVisible({ timeout: 8000 })

  // 闭环：Save 即回列表 + 行在场；行 name 链接 → 编辑页（既有路由维持）
  await expect(page).toHaveURL(/\/admin\/security\/users$/)
  await expect(page.locator(`[data-testid="user-row-${user}"]`)).toBeVisible()
  await page.click(`[data-testid="user-row-${user}"] a.row-link`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/security/users/${user}$`))
  await expect(page.locator('[data-testid="user-detail-page"]')).toBeVisible()

  // payload 净度断言：唯一 PUT 体 = 六键（name/email/password/admin/
  // adminRole/enabled/groups），三能力位 + 双布尔管理位均不在体
  const bodies = puts.bodies()
  expect(bodies).toHaveLength(1)
  const body = bodies[0] as Record<string, unknown>
  expect(Object.keys(body).sort()).toEqual(['admin', 'adminRole', 'email', 'enabled', 'groups', 'name', 'password'])
  expect('profileUpdatable' in body).toBe(false)
  expect('disableUIAccess' in body).toBe(false)
  expect('internalPasswordDisabled' in body).toBe(false)

  // API 漂移钉（tripwire，③ 第三件）：直连 PUT 携带三域 → 201（decode
  // 位静默吞咽）而 GET 回显维持出厂档——BE 承接票落地日此断言翻红，
  // 届时按 T-439 转正流程解禁预留位（控件可交互 + 进 body + 三链腿改写）
  const pinned = uniq('t453pin')
  const put = await sessionApi(page, 'PUT', `/api/security/users/${pinned}`, {
    name: pinned,
    email: `${pinned}@example.com`,
    password: 't453-pin-pw',
    admin: false,
    groups: [],
    profileUpdatable: false,
    disableUIAccess: true,
    internalPasswordDisabled: true,
  })
  expect(put.status).toBe(201)
  const echo = await sessionApi(page, 'GET', `/api/security/users/${pinned}`)
  expect(echo.status).toBe(200)
  const ej = echo.json as {
    profileUpdatable: boolean
    disableUIAccess: boolean
    internalPasswordDisabled: boolean
  }
  expect(ej.profileUpdatable, 'drift pin: BE drops the flag (reserved slot stays)').toBe(true)
  expect(ej.disableUIAccess, 'drift pin: BE drops the flag').toBe(false)
  expect(ej.internalPasswordDisabled, 'drift pin: BE drops the flag').toBe(false)

  // 收尾
  const client = m8Client()
  await client.request('DELETE', `/binflow/api/security/users/${pinned}`)
  await client.request('DELETE', `/binflow/api/security/users/${user}`)
})

// ---- ①② 组：深链整页表单 + 内联卡退役 + 编辑路由页闭环 --------------------

test('admin: /groups/new deep link + inline-card retirement + edit route loop', async ({ page }) => {
  await loginAs(page, 'admin')
  const group = uniq('t453g')
  const member = uniq('t453m')
  const client = m8Client()

  // 成员备料（E2 投影候选——先建后进表单页）
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${member}`, {
        name: member,
        email: `${member}@example.com`,
        password: 't453-m-pw',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)

  // 断言反转④（②）：列表页 DOM **无**内联表单卡（退役断言——B-2.15 兑现）。
  // 背景组：净实例零组时列表呈空态而非表——退役断言需要表本体在场
  const ctx = uniq('t453ctx')
  expect(
    (await sessionApi(page, 'PUT', `/api/security/groups/${ctx}`, { name: ctx, description: 't453 list context' })).status,
  ).toBe(201)
  await page.goto('/binflow/ui/admin/security/groups')
  await expect(page.locator('[data-testid="groups-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-form"]')).toHaveCount(0)

  // 「＋ 新建组」= 导航入口 → /groups/new 深链整页表单
  await page.click('[data-testid="groups-create"]')
  await expect(page).toHaveURL(/\/admin\/security\/groups\/new$/)
  await expect(page.locator('[data-testid="group-form-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-form"]')).toBeVisible()

  // 创建（成员穿梭在创建态即生效）→ Save 回列表
  await page.fill('[data-testid="group-form-name"]', group)
  await page.fill('[data-testid="group-form-description"]', 't453 routed form')
  await page.check(`[data-testid="group-form-member-${member}"]`)
  await page.click('[data-testid="group-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `组 ${group} 已创建（成员 +1）` })).toBeVisible({
    timeout: 8000,
  })
  await expect(page).toHaveURL(/\/admin\/security\/groups$/)
  await expect(page.locator(`[data-testid="group-row-${group}"]`)).toBeVisible()

  // 闭环编辑臂：行内「编辑」→ /groups/:name/edit 路由页（内联编辑卡退役）
  // ——E5 选区种入穿梭已选列 + 权限矩阵节在场
  await page.click(`[data-testid="group-edit-${group}"]`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/security/groups/${group}/edit$`))
  await expect(page.locator('[data-testid="group-form-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-form-members"] [data-testid="transfer-selected"]')).toContainText(member)
  await expect(page.locator('[data-testid="group-form"]')).toContainText('组权限矩阵')

  // 描述改写 → Save 回列表 → E5 API 对账
  await page.fill('[data-testid="group-form-description"]', 't453 edited')
  await page.click('[data-testid="group-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: '描述已更新' })).toBeVisible({ timeout: 8000 })
  await expect(page).toHaveURL(/\/admin\/security\/groups$/)
  const got = await sessionApi(page, 'GET', `/api/security/groups/${group}?includeUsers=true`)
  expect(got.status).toBe(200)
  expect((got.json as { description: string; userNames: string[] }).description).toBe('t453 edited')
  expect((got.json as { userNames: string[] }).userNames).toEqual([member])

  // 用户列表页同款退役断言（② 的 users 侧）：列表 DOM 无 user-form，
  // 入口点击 = 导航
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form"]')).toHaveCount(0)
  await page.click('[data-testid="users-create"]')
  await expect(page).toHaveURL(/\/admin\/security\/users\/new$/)

  // 收尾：先删成员再删组与背景组
  await client.request('DELETE', `/binflow/api/security/users/${member}`)
  await client.request('DELETE', `/binflow/api/security/groups/${group}`)
  await client.request('DELETE', `/binflow/api/security/groups/${ctx}`)
})

// ---- ⑤ readonly_admin 深链防御 + axe 双主题（组编辑路由页） ------------------

test('readonly_admin: /users/new deep link renders read-only; axe clean on group edit route', async (
  { page },
  testInfo: TestInfo,
) => {
  await loginAs(page, 'readonly_admin')

  // L4 深链防御：readonly_admin 直接进 /users/new = 只读呈现（创建钮在
  // 列表 L4 不渲染，路由页是深链兜底面——服务端 403 终裁）
  await page.goto('/binflow/ui/admin/security/users/new')
  await expect(page.locator('[data-testid="user-create-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-create-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-name"]')).toBeDisabled()
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeDisabled()

  // axe 双主题：组编辑路由页（E5 种入后的整页形态——t384 腿④覆盖创建页）。
  // 备料走 admin 直连客户端（readonly_admin 写面 403）
  const client = m8Client()
  const group = uniq('t453ax')
  expect((await client.request('PUT', `/binflow/api/security/groups/${group}`, { body: { name: group, description: 't453 axe' } })).status).toBe(201)
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto(`/binflow/ui/admin/security/groups/${group}/edit`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="group-form-members"] [data-testid="transfer-available"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="group-form-page"]' })
  }
  expect((await client.request('DELETE', `/binflow/api/security/groups/${group}`)).status).toBe(200)
})
