import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from './support/a11y'
import { loginAs } from './support/roles'
import { sessionApi } from './support/seed'

// T-237（FR-73 / console-m8 §6.9/§6.10 / UI-12/13）：用户与组管理页重排的
// 交互断言——三角色腿 + 键盘流 + axe。
//
// 断言口径 = ADR-0029 决策 3（交互断言制；锚 = data-testid，不随路由改名）。
// 锚源：console-ux §10.3 安全组（users-*/user-form-*/groups-*/group-form-*）
// + 本票新增（users-sort-*/user-form-{enabled,password2,reset,cancel}/
// user-perm-*/transfer-{available,selected}/group-form-{members,member-*,
// reset,cancel}/group-{perm-*,perms-*,members-*,manage-badge-*}/
// groups-{sort-*,count}）。
//
// 运行前提与 m8 README §1 相同：make console && make build 后真二进制
// 前台 serve，BASE 指向它；角色种子经 loginAs 幂等预备（M8_SKIP_PROVISION
// 可跳过）。键盘腿沿 smoke 先例：focus() 定位 + page.keyboard 驱动（不落鼠标）。

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 在登录页尝试登录（失败腿复用同一锚观察错误行——不禁用断言） */
async function tryLogin(page: Page, origin: string, username: string, password: string) {
  await page.goto(`${origin}/binflow/ui/`)
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)
  await page.click('[data-testid="login-submit"]')
}

test('admin: create user with transfer membership, edit partitions, role dropdown, enabled flip round-trip', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  const group = uniq('t237g')
  const user = uniq('t237u')

  // 备料：一个组（穿梭的右侧目标）
  expect((await sessionApi(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 't237' })).status).toBe(201)

  // —— 列表形态：列头排序 + 分页行（T-451 页码控件）+ axe ——
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-count"]')).toContainText(/共 \d+ 项/)
  await expect(page.locator('[data-testid="users-sort-name"]')).toHaveAttribute('aria-sort', 'ascending')
  await page.click('[data-testid="users-sort-name"]')
  await expect(page.locator('[data-testid="users-sort-name"]')).toHaveAttribute('aria-sort', 'descending')
  const namesDesc = await page.locator('[data-testid="users-table"] tbody .row-link').allTextContents()
  expect([...namesDesc].sort().reverse()).toEqual(namesDesc)
  await page.click('[data-testid="users-sort-name"]') // 回 none
  await expect(page.locator('[data-testid="users-sort-name"]')).toHaveAttribute('aria-sort', 'none')
  await expectA11yClean(page, testInfo, { include: '[data-testid="users-page"]' })

  // —— 新建：分区表单 + 双列穿梭 + 必填门 ——
  await page.click('[data-testid="users-create"]')
  await expect(page.locator('[data-testid="user-form"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeDisabled() // 必填未满足置灰（reverse §4.6）
  await page.fill('[data-testid="user-form-name"]', user)
  await page.fill('[data-testid="user-form-email"]', `${user}@example.com`)
  await page.fill('[data-testid="user-form-password"]', 't237-pw-1')
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeEnabled()
  // 穿梭：勾选 = 移入「已选组」列（checkbox 锚沿用 T-101 冻结形态）
  await page.check(`[data-testid="user-form-group-${group}"]`)
  await expect(page.locator('[data-testid="user-form-groups"] [data-testid="transfer-selected"]')).toContainText(group)
  await expect(page.locator('[data-testid="user-form-groups"] [data-testid="transfer-available"]')).not.toContainText(group)
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已创建` })).toBeVisible({ timeout: 8000 })
  await expect(page.locator(`[data-testid="user-row-${user}"]`)).toContainText(group)
  await expect(page.locator(`[data-testid="user-row-${user}"]`).locator('.badge', { hasText: 'user' })).toBeVisible()

  // —— 编辑器：分区（用户设置/选项/口令/相关组）+ 角色 + 穿梭移出 ——
  await page.goto(`/binflow/ui/admin/security/users/${user}`)
  await expect(page.locator('[data-testid="user-form-email"]')).toHaveValue(`${user}@example.com`)
  await expect(page.locator('[data-testid="user-form-role"]')).toHaveValue('user')
  await expect(page.locator(`[data-testid="user-form-group-${group}"]`)).toBeChecked()
  // 无授权 → 空态文案（不是空表）
  await expect(page.locator('[data-testid="user-perm-matrix"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="user-perms"]')).toContainText('未获得任何')

  await page.uncheck(`[data-testid="user-form-group-${group}"]`)
  await expect(page.locator('[data-testid="user-form-groups"] [data-testid="transfer-selected"]')).not.toContainText(group)
  await page.selectOption('[data-testid="user-form-role"]', 'readonly_admin')
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已更新` }).last()).toBeVisible({ timeout: 8000 })
  const got = await sessionApi(page, 'GET', `/api/security/users/${user}`)
  expect(got.status).toBe(200)
  expect((got.json as { adminRole: string; groups: string[] }).adminRole).toBe('readonly_admin')
  expect((got.json as { groups: string[] }).groups).toEqual([])

  // —— enabled 翻转（T-224 非缺陷②验证：表单全量提交形态天然满足）——
  await expect(page.locator('[data-testid="user-form-password"]')).toHaveValue('') // 表单已随 reload 复位
  await page.uncheck('[data-testid="user-form-enabled"]')
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已更新` }).last()).toBeVisible({ timeout: 8000 })
  // 禁用账号无法登录（401 → 行内错误，无跳转绕行）
  const origin = new URL(page.url()).origin
  const off = await page.context().browser()!.newContext()
  const offPage = await off.newPage()
  await tryLogin(offPage, origin, user, 't237-pw-1')
  await expect(offPage.locator('[data-testid="login-error"]')).toBeVisible()
  await off.close()
  // 重新启用 → 同口令可登录
  await page.check('[data-testid="user-form-enabled"]')
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已更新` }).last()).toBeVisible({ timeout: 8000 })
  const on = await page.context().browser()!.newContext()
  const onPage = await on.newPage()
  await tryLogin(onPage, origin, user, 't237-pw-1')
  await expect(onPage.locator('[data-testid="session-user"]')).toHaveText(user)
  await on.close()

  // 收尾：确认口令不一致挡提交
  await expect(page.locator('[data-testid="user-form-password"]')).toHaveValue('')
  await page.fill('[data-testid="user-form-password"]', 't237-pw-2')
  await page.fill('[data-testid="user-form-password2"]', 'mismatch')
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="user-form-password2"]', 't237-pw-2')
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeEnabled()
})

test('admin: groups editor — membership transfer writes per-user, matrix + manage badge, delete guard wording', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const group = uniq('t237mg')
  const userA = uniq('t237a')
  const userB = uniq('t237b')

  // 备料：两个用户（成员穿梭的目标）
  for (const u of [userA, userB]) {
    expect(
      (
        await sessionApi(page, 'PUT', `/api/security/users/${u}`, {
          name: u,
          email: `${u}@example.com`,
          password: 't237-pw-x',
          admin: false,
          groups: [],
        })
      ).status,
    ).toBe(201)
  }

  // 组页列表形态 + 新建（成员穿梭在创建态即生效）
  await page.goto('/binflow/ui/admin/security/groups')
  await expect(page.locator('[data-testid="groups-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="groups-count"]')).toContainText(/共 \d+ 项/)
  await page.click('[data-testid="groups-create"]')
  await page.fill('[data-testid="group-form-name"]', group)
  await page.fill('[data-testid="group-form-description"]', 't237 members')
  await page.check(`[data-testid="group-form-member-${userA}"]`)
  await expect(page.locator('[data-testid="group-form-members"] [data-testid="transfer-selected"]')).toContainText(userA)
  await page.click('[data-testid="group-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `组 ${group} 已创建` })).toBeVisible({ timeout: 8000 })

  // API 对账：成员经用户侧组集落盘
  let got = await sessionApi(page, 'GET', `/api/security/users/${userA}`)
  expect((got.json as { groups: string[] }).groups).toEqual([group])
  await expect(page.locator(`[data-testid="group-members-${group}"]`)).toHaveText('1') // 成员数列

  // 编辑器：组权限矩阵（target 引用该组并授 manage → 矩阵行 + manage 徽章）
  const repo = uniq('t237r')
  expect((await sessionApi(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect(
    (
      await sessionApi(page, 'POST', '/api/v1/permissions', {
        name: uniq('t237t'),
        repos: [repo],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: {}, groups: { [group]: ['read', 'manage'] } },
      })
    ).status,
  ).toBe(201)
  await page.reload()
  await expect(page.locator(`[data-testid="group-manage-badge-${group}"]`)).toBeVisible()
  await page.click(`[data-testid="group-edit-${group}"]`)
  await expect(page.locator('[data-testid="group-perm-matrix"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-perm-matrix"] th', { hasText: 'manage' })).toHaveCount(1)

  // 移出一个成员 + 加入另一个 → 保存 → 对账
  await page.uncheck(`[data-testid="group-form-member-${userA}"]`)
  await page.check(`[data-testid="group-form-member-${userB}"]`)
  await page.click('[data-testid="group-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `组 ${group} 已更新` })).toBeVisible({ timeout: 8000 })
  got = await sessionApi(page, 'GET', `/api/security/users/${userA}`)
  expect((got.json as { groups: string[] }).groups).toEqual([])
  got = await sessionApi(page, 'GET', `/api/security/users/${userB}`)
  expect((got.json as { groups: string[] }).groups).toEqual([group])

  // 删除守卫：确认文案带后果说明（§4.6 组行）
  await page.click(`[data-testid="group-delete-${group}"]`)
  await expect(page.locator('[data-testid="confirm-dialog"]')).toContainText('不可撤销')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="group-delete-reason"]')).toBeVisible() // 被 target 引用 → 409 面板
})

// （axe 腿内联于 admin/readonly 两用例；键盘腿见下）

test('readonly_admin: users/groups read-only walk, write replay stays 403 server-side', async ({ page }, testInfo) => {
  const ro = await loginAs(page, 'readonly_admin')
  const self = ro.username

  // 用户列表：可见 + 只读注记 + 无写入口；自身角色徽章
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-create"]')).toHaveCount(0)
  await expect(page.locator(`[data-testid="user-row-${self}"] .badge`)).toContainText('readonly_admin')

  // 用户编辑器：全编辑面禁用（角色下拉/启用/口令/穿梭/保存）
  await page.goto(`/binflow/ui/admin/security/users/${self}`)
  await expect(page.locator('[data-testid="user-form-role"]')).toHaveValue('readonly_admin')
  await expect(page.locator('[data-testid="user-form-role"]')).toBeDisabled()
  await expect(page.locator('[data-testid="user-form-enabled"]')).toBeDisabled()
  await expect(page.locator('[data-testid="user-form-password"]')).toBeDisabled()
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeDisabled()
  await expect(page.locator('[data-testid="user-form-readonly-note"]')).toBeVisible()
  const transferBoxes = page.locator('[data-testid="user-form-groups"] input[type="checkbox"]')
  if ((await transferBoxes.count()) > 0) {
    await expect(transferBoxes.first()).toBeDisabled()
  }
  await expectA11yClean(page, testInfo, { include: '[data-testid="user-detail-page"]' })

  // 组页：可见 + 只读注记 + 无创建/编辑/删除入口（拷贝钮是读操作，允许）
  await page.goto('/binflow/ui/admin/security/groups')
  await expect(page.locator('[data-testid="groups-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="groups-create"]')).toHaveCount(0)
  await expect(
    page.locator(
      '[data-testid="groups-table"] tbody [data-testid^="group-edit-"], [data-testid="groups-table"] tbody [data-testid^="group-delete-"]',
    ),
  ).toHaveCount(0)
  await expectA11yClean(page, testInfo, { include: '[data-testid="groups-page"]' })

  // 服务端兜底：同一会话重放写请求——403（UI 只是呈现层，无绕过）
  const w1 = await sessionApi(page, 'POST', `/api/security/users/${self}`, { name: self, email: 'x@example.com' })
  expect(w1.status).toBe(403)
  const w2 = await sessionApi(page, 'PUT', `/api/security/groups/${uniq('nope')}`, { name: 'nope', description: '' })
  expect(w2.status).toBe(403)
  // 读面存活（只读态不是 403 姿态）
  const r1 = await sessionApi(page, 'GET', '/api/security/users')
  expect(r1.status).toBe(200)
})

test('keyboard: row link Enter opens editor; transfer Space moves item; Enter on focused Save submits', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const group = uniq('t237k')
  const user = uniq('t237ku')
  expect((await sessionApi(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 'kbd' })).status).toBe(201)
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: 't237-pw-k',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)

  // 行链接：focus + Enter 进编辑器（无鼠标）
  await page.goto('/binflow/ui/admin/security/users')
  const row = page.locator(`[data-testid="user-row-${user}"] a.row-link`)
  await row.focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="user-detail-page"]')).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/security/users/${user}$`))

  // 穿梭：checkbox focus + Space 勾选 → 移入已选列
  const box = page.locator(`[data-testid="user-form-group-${group}"]`)
  await box.focus()
  await page.keyboard.press('Space')
  await expect(page.locator('[data-testid="user-form-groups"] [data-testid="transfer-selected"]')).toContainText(group)

  // 保存：focus + Enter 提交（表单全量体）
  const save = page.locator('[data-testid="user-form-submit"]')
  await save.focus()
  await expect(save).toBeEnabled()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已更新` }).last()).toBeVisible({ timeout: 8000 })
})
