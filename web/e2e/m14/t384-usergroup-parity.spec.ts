import { expect, test } from '@playwright/test'
import type { Locator, Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, sessionApi } from '../m8/support/seed'

// T-384（M14 B3 FE，FR-124.3 M3）——用户/组创建形态「断言收口」（原票
// 「创建 modal 化」经 T-381 活体核验 v1.1 改判撤销：Artifactory 7.84 用户/组
// 创建实测 = **整页路由表单非 modal**，决策项 B 撤销）。
//
// **T-453 断言反转④（Q5 出口①路由化，2026-09-04）**：本 spec 的「内建
// 表单就地展开」断言面整体翻转为「/users/new、/groups/new 深链整页路由
// 表单」——T-384 期 parity v1.1 裁定的「列表页内建与路由页同档形态，可
// 保持」经 M16 审计 B-2.15 推翻（E5 豁免前提失效，v1.5 注销），本票兑现
// 路由化 + 内联卡退役。7.161.20 活体复核（2026-09-04，reports/agents/
// t453-probe/）：用户/组创建 = /ui/admin/management/{users,groups}/new
// 整页表单，页脚 Cancel | Reset〔初始禁置〕| Save〔初始禁置〕；用户表单
// Retype Password 在场（T-384 期「创建态无 Retype」定案随 T-453 翻案）。
//
// 钉死面（对照核验表逐项 ↔ reports/agents/T-384.md §1 + T-453 翻案）：
//   ① 创建入口形态：列表页「＋ 新建用户 / ＋ 新建组」钮 = **导航入口**
//      （点击即路由到 /new 整页表单；非 modal：无 dialog role；列表页
//      DOM 无 user-form/group-form——内联卡退役断言，详腿见
//      m16/t453-route-forms.spec.ts）；
//   ② 表单结构（用户）：四节 user-form-section-{settings|options|password|
//      groups}——核心字段集（name/email/password〔+Retype，T-453〕）对位
//      Artifactory User Name/Email/Password/Retype Password；角色三值下拉
//      + 组穿梭 = BinFlow RBAC 超集（FR-66；管理位双布尔候裁臂注记——
//      ADR-0026 暂行维持枚举）；能力位三旗 = 预留位（BE 未承接，恒禁用
//      零提交——详腿见 m16/t453 spec）；
//   ③ 组面：group-form-section-{settings|members}——组名/描述 + 成员选择
//      列表（Artifactory Users 双列的对位形态，穿梭增强）；External ID /
//      Auto Join / 组级管理位不建（console-m8 §6.10「无外部组模型」+
//      rbac-model §5 既有裁定）；
//   ④ 页脚三联：Cancel 最左 / Reset / Save 右（V6 实测形态；7.161 活体：
//      Reset/Save **初始禁置**——Reset dirty 门 + Save 必填门）——
//      user-form-{cancel|reset} / group-form-{cancel|reset} v1.20 复役锚；
//      用户/组表单按 7.161 **保留 Reset**（与 T-439 建仓表单移除重置钮的
//      Q9 处置为页面级差异化配置，差异留痕 parity 册）；
//   ⑤ 全链：入口 → 路由表单 → Save 落库 + 回列表 + API 对账（用户 E2
//      回显 / 组 E5 includeUsers 成员视图）。
//
// 锚源：console-ux §10.3 安全组（users-*/user-form-*/groups-*/group-form-*
// 冻结族零改名 + T-384 批 user-form-section-* / group-form-section-* 六节锚
// + 四枚页脚锚复役——T-453 载体迁移（列表内联卡 → 路由页）零改名）。
// 断言口径 = e2e/m8/README §2（ADR-0029 决策 3）。L4 入口门（readonly_admin
// 无创建钮）由 m8 users-groups 既有腿承载，不重复。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 路由整页表单形态三件套（① 的断言形态，T-453 反转后）：表单在场 +
 *  非 modal（页内无 dialog role）+ URL = /new 深链路由。 */
async function expectRoutedCreateForm(page: Page, form: string, route: RegExp): Promise<Locator> {
  const loc = page.locator(`[data-testid="${form}"]`)
  await expect(loc).toBeVisible()
  await expect(page.locator('[role="dialog"]')).toHaveCount(0)
  await expect(page).toHaveURL(route)
  return loc
}

/** 页脚三联（④ 的断言形态）：DOM 在场 + 几何序 Cancel < Reset < Save
 *  （V6 实测「Cancel 最左 x=294 / Save 右」的同形体例）+ 主次级皮肤。
 *  初始禁置（Reset/Save）由调用方按需断言（创建链腿各闸门口径不同）。
 *  三 locator 由调用方以字面锚构造（对账器口径：模板透传形不可见）。 */
async function expectFooterTriple(cancel: Locator, reset: Locator, save: Locator): Promise<void> {
  for (const b of [cancel, reset, save]) await expect(b).toBeVisible()
  const xs = await Promise.all([cancel, reset, save].map((b) => b.boundingBox()))
  expect(xs[0]!.x, 'cancel is far-left').toBeLessThan(xs[1]!.x)
  expect(xs[1]!.x, 'reset is middle').toBeLessThan(xs[2]!.x)
  // P3 新栈：contained/outlined 语义 → bg-primary（主操作底色）/ outline
  // 边框变体（MUI 类名随栈退役——形状钉语义不变）
  await expect(save).toHaveClass(/bg-primary/)
  await expect(cancel).toHaveClass(/border/)
  await expect(reset).toHaveClass(/border/)
}

// ---- 1. 用户创建全链：列表入口 → 路由整页表单 → Save 落用户（+ API 对账）--

test('admin: user create chain — routed full-page form → Save lands the user, back to list', async ({ page }) => {
  await loginAs(page, 'admin')
  const user = uniq('t384u')
  const group = uniq('t384g')

  // 备料：目标组（表单挂载时才取组清单——先建后开表单）
  expect(
    (await sessionApi(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 't384 parity probe' })).status,
  ).toBe(201)

  // 入口（T-453 反转④）：列表页「＋ 新建用户」→ 导航到 /users/new 整页
  // 路由表单（非 modal；列表页 DOM 无内联卡——该断言在 m16/t453 spec）
  await page.goto('/binflow/ui/admin/security/users')
  await page.click('[data-testid="users-create"]')
  const form = await expectRoutedCreateForm(page, 'user-form', /\/binflow\/ui\/admin\/security\/users\/new$/)
  await expect(form).toHaveAttribute('aria-label', '新建用户')

  // 表单结构（②）：四节在场，字段各归其节（核心字段 + BinFlow 超集）
  await expect(page.locator('[data-testid="user-form-section-settings"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-section-options"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-section-password"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-section-groups"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-section-settings"] [data-testid="user-form-name"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-section-settings"] [data-testid="user-form-email"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-section-settings"] [data-testid="user-form-role"]')).toHaveValue('user')
  await expect(page.locator('[data-testid="user-form-section-options"] [data-testid="user-form-enabled"]')).toBeChecked()
  await expect(page.locator('[data-testid="user-form-section-password"] [data-testid="user-form-password"]')).toBeVisible()
  // Retype（T-453：7.161 创建表单实测在场——T-384 期缺位定案翻案）
  await expect(page.locator('[data-testid="user-form-section-password"] [data-testid="user-form-password2"]')).toBeVisible()

  // 必填门（reverse §4.6：必填未满足 Save 置灰；7.161 活体 Save 初始禁置）
  // → 三必填齐（口令双录）→ 放行
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="user-form-name"]', user)
  await page.fill('[data-testid="user-form-email"]', `${user}@example.com`)
  await page.fill('[data-testid="user-form-password"]', 't384-pw-1')
  await page.fill('[data-testid="user-form-password2"]', 't384-pw-1')
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeEnabled()

  // 相关组（穿梭）：勾选即入「已选组」列 = Artifactory Related Groups 对位
  await page.check(`[data-testid="user-form-group-${group}"]`)
  await expect(page.locator('[data-testid="user-form-groups"] [data-testid="transfer-selected"]')).toContainText(group)
  await page.click('[data-testid="user-form-submit"]')

  // toast + 保存后回列表（Artifactory 同姿；T-453：路由表单 Save 即导航）
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已创建` })).toBeVisible({ timeout: 8000 })
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/users$/)
  await expect(page.locator(`[data-testid="user-row-${user}"]`)).toContainText(group)

  // API 对账（UI 说的话让 API 复核——GET 全量回显）
  const got = await sessionApi(page, 'GET', `/api/security/users/${user}`)
  expect(got.status).toBe(200)
  const body = got.json as { name: string; email: string; adminRole: string; enabled: boolean; groups: string[] }
  expect(body.name).toBe(user)
  expect(body.email).toBe(`${user}@example.com`)
  expect(body.adminRole).toBe('user')
  expect(body.enabled).toBe(true)
  expect(body.groups).toContain(group)

  // 收尾：先删用户（组员行随用户级联）再删组
  expect((await m8Client().request('DELETE', `/binflow/api/security/users/${user}`)).status).toBe(200)
  expect((await m8Client().request('DELETE', `/binflow/api/security/groups/${group}`)).status).toBe(200)
})

// ---- 2. 组创建全链：成员选择列表经逐用户落盘（+ E5 对账） --------------------

test('admin: group create chain — settings/members sections, per-user membership lands (E5 reconciled)', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const group = uniq('t384g')
  const member = uniq('t384m')

  // 备料：成员用户（穿梭候选 = E2 users 投影，路由页自身取数——先建后进页）
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${member}`, {
        name: member,
        email: `${member}@example.com`,
        password: 't384-m-pw-1',
        admin: false,
        adminRole: 'user',
        groups: [],
      })
    ).status,
  ).toBe(201)

  // 入口（T-453 反转④）：列表页「＋ 新建组」→ 导航到 /groups/new 整页表单
  await page.goto('/binflow/ui/admin/security/groups')
  await page.click('[data-testid="groups-create"]')
  const form = await expectRoutedCreateForm(page, 'group-form', /\/binflow\/ui\/admin\/security\/groups\/new$/)
  await expect(form).toHaveAttribute('aria-label', '新建组')

  // 组面结构（③）：组设置（名称/描述）+ 成员选择列表两节；External ID /
  //  组级管理位 / Auto Join 不渲染（§6.10 + rbac-model §5 既有裁定——
  //  反断言以节内字段集表达）
  await expect(page.locator('[data-testid="group-form-section-settings"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-form-section-members"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-form-section-settings"] [data-testid="group-form-name"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-form-section-settings"] [data-testid="group-form-description"]')).toBeVisible()
  await expect(form).not.toContainText('External ID')
  await expect(form).not.toContainText('Automatically Join')

  // 必填门（组名空 → 主钮置灰）→ 组名/描述 → 放行
  await expect(page.locator('[data-testid="group-form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="group-form-name"]', group)
  await page.fill('[data-testid="group-form-description"]', 't384 parity probe')
  await expect(page.locator('[data-testid="group-form-submit"]')).toBeEnabled()

  // 成员选择列表（Artifactory Users 双列的对位形态）：勾选 → 已选成员列
  await page.check(`[data-testid="group-form-member-${member}"]`)
  await expect(page.locator('[data-testid="group-form-members"] [data-testid="transfer-selected"]')).toContainText(member)
  await page.click('[data-testid="group-form-submit"]')

  // toast（含成员计数）+ 保存后回列表 + 新行在场
  await expect(
    page.locator('[data-testid="toast"]').filter({ hasText: `组 ${group} 已创建（成员 +1）` }),
  ).toBeVisible({ timeout: 8000 })
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/groups$/)
  await expect(page.locator(`[data-testid="group-row-${group}"]`)).toBeVisible()

  // API 对账：E5 带参形态——成员事实源 = user_groups 行，经逐用户组集替换落盘
  const got = await sessionApi(page, 'GET', `/api/security/groups/${group}?includeUsers=true`)
  expect(got.status).toBe(200)
  const body = got.json as { name: string; description: string; userNames: string[] }
  expect(body.name).toBe(group)
  expect(body.description).toBe('t384 parity probe')
  expect(body.userNames).toEqual([member])

  // 收尾：先删成员（成员行随用户级联）再删组
  expect((await m8Client().request('DELETE', `/binflow/api/security/users/${member}`)).status).toBe(200)
  expect((await m8Client().request('DELETE', `/binflow/api/security/groups/${group}`)).status).toBe(200)
})

// ---- 3. 形态钉死：页脚三联 + Reset/Cancel 语义（双表单同形） ------------------

test('admin: routed-form shape pin — footer triple Cancel/Reset/Save, reset+cancel semantics', async ({ page }) => {
  await loginAs(page, 'admin')

  // 背景组：Cancel 回列表后的「列表在场」断言需要表本体（净实例零组呈空态）
  const ctx = uniq('t384ctx')
  expect(
    (await sessionApi(page, 'PUT', `/api/security/groups/${ctx}`, { name: ctx, description: 't384 list context' })).status,
  ).toBe(201)

  // 用户表单：页脚三联（V6 实测：Cancel 最左 / Reset / Save 右）+ 双初始
  // 禁置（7.161 活体：Reset 初始 disabled——dirty 门；Save 初始 disabled
  // ——必填门）
  await page.goto('/binflow/ui/admin/security/users')
  await page.click('[data-testid="users-create"]')
  await expect(page.locator('[data-testid="user-form-reset"]')).toBeDisabled()
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeDisabled()
  await expectFooterTriple(
    page.locator('[data-testid="user-form-cancel"]'),
    page.locator('[data-testid="user-form-reset"]'),
    page.locator('[data-testid="user-form-submit"]'),
  )

  // Reset 语义：已填字段回 CREATE_INITIAL（清空，不提交）；dirty 即解禁
  await page.fill('[data-testid="user-form-name"]', 'shape-probe')
  await expect(page.locator('[data-testid="user-form-reset"]')).toBeEnabled()
  await page.fill('[data-testid="user-form-email"]', 'shape@example.com')
  await page.fill('[data-testid="user-form-password"]', 'shape-pw')
  await page.fill('[data-testid="user-form-password2"]', 'shape-pw')
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeEnabled()
  await page.click('[data-testid="user-form-reset"]')
  await expect(page.locator('[data-testid="user-form-name"]')).toHaveValue('')
  await expect(page.locator('[data-testid="user-form-email"]')).toHaveValue('')
  await expect(page.locator('[data-testid="user-form-password"]')).toHaveValue('')
  await expect(page.locator('[data-testid="user-form-submit"]')).toBeDisabled()

  // Cancel 语义：路由表单的 Cancel = 导航回列表（Artifactory 同姿）
  await page.click('[data-testid="user-form-cancel"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/users$/)
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()

  // 组表单：页脚三联同形 + Reset 初始禁置 + Cancel 同语义
  await page.goto('/binflow/ui/admin/security/groups')
  await page.click('[data-testid="groups-create"]')
  await expect(page.locator('[data-testid="group-form-reset"]')).toBeDisabled()
  await expect(page.locator('[data-testid="group-form-submit"]')).toBeDisabled()
  await expectFooterTriple(
    page.locator('[data-testid="group-form-cancel"]'),
    page.locator('[data-testid="group-form-reset"]'),
    page.locator('[data-testid="group-form-submit"]'),
  )
  await page.click('[data-testid="group-form-cancel"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/groups$/)
  await expect(page.locator('[data-testid="groups-table"]')).toBeVisible()

  // 收尾：背景组
  expect((await m8Client().request('DELETE', `/binflow/api/security/groups/${ctx}`)).status).toBe(200)
})

// ---- 4. axe 双主题：创建态表单整页（用户四节 + 组两节 × light/dark） ----------

test('axe: user/group create-form pages clean in both themes', async ({ page }, testInfo) => {
  test.setTimeout(300_000) // 4 面 × 双主题；axe 在默认并发下实测 30s+/面
  await loginAs(page, 'admin')

  // 备料一个组：用户表单的相关组节须呈现穿梭本体（而非空组降级态）
  const group = uniq('t384a11y')
  expect(
    (await sessionApi(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 't384 axe' })).status,
  ).toBe(201)

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)

    // 用户创建页（四节 + 穿梭就绪后扫——路由页整页即扫描面）
    await page.goto('/binflow/ui/admin/security/users')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await page.click('[data-testid="users-create"]')
    await expect(page.locator(`[data-testid="user-form-group-${group}"]`)).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="user-create-page"]' })

    // 组创建页（组设置 + 成员穿梭）
    await page.goto('/binflow/ui/admin/security/groups')
    await page.click('[data-testid="groups-create"]')
    await expect(page.locator('[data-testid="group-form-members"] [data-testid="transfer-available"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="group-form-page"]' })
  }

  // 收尾
  expect((await m8Client().request('DELETE', `/binflow/api/security/groups/${group}`)).status).toBe(200)
})
