import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from './support/a11y'
import { loginAs } from './support/roles'
import { m8Client, sessionApi } from './support/seed'

// T-241（FR-73 / console-m8 §4.5+§6.11 / UI-14）：权限 target 编辑器重排的
// 交互断言——两步资源对话框全链 / 四动作矩阵（r·w·d·m）/ 模式测试器 /
// m-holder 覆盖集腿（内 201 外 403，T-217 B1；控制台面 T-259 起按
// filter=manage 可达形态改写——L2 边界卡退役）/ readonly 只读腿 + 键盘流 + axe。
//
// 断言口径 = ADR-0029 决策 3（交互断言制；锚 = data-testid，不随路由改名）。
// 锚源：console-ux §10.3 冻结的 perm-* 族（perm-repo-add 自 T-241 起是两步
// 资源对话框的入口按钮——语义不变：加仓库走它）+ 本票新增
// （perms-{sort-*,count} / perm-manage-badge-<name> / perm-matrix-groups /
// perm-patterns-summary / perm-res-{dialog,step,repos,next,back,ok,cancel} /
// perm-repo-pick-<key>）。契约冻结：全程现役端点，零新端点。
//
// 运行前提与 m8 README §1 相同：make console && make build 后真二进制前台
// serve，BASE 指向它。键盘腿沿 smoke 先例：focus() 定位 + page.keyboard 驱动。

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

interface TargetEcho {
  name: string
  repos: string[]
  includePatterns: string[]
  excludePatterns: string[]
  principals: { users: Record<string, string[]>; groups: Record<string, string[]> }
}

async function findTarget(page: Page, name: string): Promise<TargetEcho> {
  const got = await sessionApi(page, 'GET', '/api/v1/permissions')
  expect(got.status).toBe(200)
  const t = (got.json as TargetEcho[]).find((x) => x.name === name)
  expect(t, `target ${name} must exist`).toBeTruthy()
  return t!
}

/** 在登录页登录（m-holder 等非 fixture 角色的会话腿） */
async function login(page: Page, username: string, password: string) {
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test('admin: two-step resource dialog full chain, four-action matrix, tester, manage badge', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  const repo = uniq('t241r')
  const group = uniq('t241g')
  const user = uniq('t241u')
  const target = uniq('t241t')

  // 备料：仓 + 组 + 用户（矩阵两区块的主体）
  expect((await sessionApi(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect((await sessionApi(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 't241' })).status).toBe(201)
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: 't241-pw-1',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)

  // —— 列表形态：列头排序 + 计数行 + axe ——
  await page.goto('/binflow/ui/admin/security/permissions')
  await expect(page.locator('[data-testid="perms-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="perms-count"]')).toContainText('权限 target 总数')
  await expect(page.locator('[data-testid="perms-sort-name"]')).toHaveAttribute('aria-sort', 'ascending')
  await page.click('[data-testid="perms-sort-name"]')
  await expect(page.locator('[data-testid="perms-sort-name"]')).toHaveAttribute('aria-sort', 'descending')
  const namesDesc = await page.locator('[data-testid="perms-table"] tbody .row-link').allTextContents()
  expect([...namesDesc].sort().reverse()).toEqual(namesDesc)
  await page.click('[data-testid="perms-sort-name"]') // 回 none
  await expect(page.locator('[data-testid="perms-sort-name"]')).toHaveAttribute('aria-sort', 'none')
  await expectA11yClean(page, testInfo, { include: '[data-testid="perms-page"]' })

  // —— 新建：必填门（名称/仓库未满足 → 创建禁用）——
  await page.click('[data-testid="perms-create"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/permissions\/new$/)
  await expect(page.locator('[data-testid="perm-save"]')).toBeDisabled()
  await page.fill('[data-testid="perm-form-name"]', target)

  // —— 两步资源对话框（§4.5）：① 穿梭选仓 ——
  await page.click('[data-testid="perm-repo-add"]')
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="perm-res-step"]')).toContainText('第 1 步')
  // T-344 批 D：ResourceDialog 换 MUI Dialog——入场 Fade 中途采样会把半透明
  // 栈算进对比度（T-344C D7 假阳性），扫描前等过渡收敛（断言语义不变）。
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toHaveCSS('opacity', '1')
  await expectA11yClean(page, testInfo, { include: '[data-testid="perm-res-dialog"]' })
  await page.check(`[data-testid="perm-repo-pick-${repo}"]`)
  await expect(page.locator('[data-testid="perm-res-repos"] [data-testid="transfer-selected"]')).toContainText(repo)
  await expect(page.locator('[data-testid="perm-res-repos"] [data-testid="transfer-available"]')).not.toContainText(repo)

  // —— ② 设置模式（默认 ** 收窄为 qa/**，排除 qa/tmp/**）——
  await page.click('[data-testid="perm-res-next"]')
  await expect(page.locator('[data-testid="perm-res-step"]')).toContainText('第 2 步')
  await page.click('[data-testid="perm-pattern-remove-include-0"]')
  await page.fill('[data-testid="perm-pattern-input-include"]', 'qa/**')
  await page.click('[data-testid="perm-pattern-add-include"]')
  await page.fill('[data-testid="perm-pattern-input-exclude"]', 'qa/tmp/**')
  await page.click('[data-testid="perm-pattern-add-exclude"]')
  await page.click('[data-testid="perm-res-ok"]')
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText(repo)
  await expect(page.locator('[data-testid="perm-patterns-summary"]')).toContainText('qa/**')

  // —— 模式测试器（pathmatch 本地求值）：命中 / exclude 优先 / 未命中 ——
  await page.fill('[data-testid="perm-pattern-test"]', 'qa/builds/app.bin')
  await expect(page.locator('[data-testid="perm-pattern-verdict"]')).toContainText('匹配')
  await page.fill('[data-testid="perm-pattern-test"]', 'qa/tmp/x.bin')
  await expect(page.locator('[data-testid="perm-pattern-verdict"]')).toContainText('不匹配')
  await expect(page.locator('[data-testid="perm-pattern-result"]')).toContainText('exclude 优先')
  await page.fill('[data-testid="perm-pattern-test"]', 'other/x.bin')
  await expect(page.locator('[data-testid="perm-pattern-verdict"]')).toContainText('不匹配')

  // —— 四动作矩阵：用户区块 r+w；组区块 r+d+m（四列全覆盖）——
  await page.selectOption('[data-testid="perm-add-user"]', user)
  await page.getByRole('button', { name: '添加用户' }).click()
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-read"]`)
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-write"]`)
  await page.selectOption('[data-testid="perm-add-group"]', group)
  await page.getByRole('button', { name: '添加组' }).click()
  await page.check(`[data-testid="perm-matrix-cell-group-${group}-read"]`)
  await page.check(`[data-testid="perm-matrix-cell-group-${group}-delete"]`)
  await page.check(`[data-testid="perm-matrix-cell-group-${group}-manage"]`)
  // 用户/组两表各自展开矩阵形态，manage 均为第 4 列（M7 §7.2）
  await expect(page.locator('[data-testid="perm-matrix"] th', { hasText: 'manage' })).toHaveCount(1)
  await expect(page.locator('[data-testid="perm-matrix-groups"] th', { hasText: 'manage' })).toHaveCount(1)
  await expect(page.locator(`[data-testid="perm-matrix-remove-user-${user}"]`)).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="perm-editor-page"]' })

  // —— 保存 = diff 确认 → toast → 列表 manage 徽章 ——
  await page.click('[data-testid="perm-save"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  const diff = page.locator('[data-testid="perm-diff"]')
  await expect(diff).toContainText(`添加仓库 ${repo}`)
  await expect(diff).toContainText('添加 include')
  await expect(diff).toContainText(`授予用户 ${user} read`)
  await expect(diff).toContainText(`授予用户 ${user} write`)
  await expect(diff).toContainText(`授予组 ${group} read`)
  await expect(diff).toContainText(`授予组 ${group} delete`)
  await expect(diff).toContainText(`授予组 ${group} manage`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/permissions$/)
  await expect(page.locator(`[data-testid="perm-row-${target}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="perm-manage-badge-${target}"]`)).toBeVisible()

  // API 对账：四动作 wire 往返（manage 全词形，回显 r/w/d/m 序）
  const t = await findTarget(page, target)
  expect(t.repos).toEqual([repo])
  expect(t.includePatterns).toEqual(['qa/**'])
  expect(t.excludePatterns).toEqual(['qa/tmp/**'])
  expect(t.principals.users[user]).toEqual(['read', 'write'])
  expect([...t.principals.groups[group]].sort()).toEqual(['delete', 'manage', 'read'])
})

test('readonly_admin: read-only walk (matrix disabled, no save/delete), write replay stays 403', async ({ page }, testInfo) => {
  const ro = await loginAs(page, 'readonly_admin')
  const repo = uniq('t241ro')
  const target = uniq('t241rot')

  // 备料走 admin REST 臂（readonly 会话无写面；makeClient 非 2xx 即抛）：
  // target 给 ro 用户授 read
  const client = m8Client()
  expect((await client.request('PUT', `/binflow/api/repositories/${repo}`, { body: { rclass: 'local', packageType: 'generic' } })).status).toBe(200)
  expect(
    (
      await client.request('POST', '/binflow/api/v1/permissions', {
        body: {
          name: target,
          repos: [repo],
          includePatterns: ['**'],
          excludePatterns: [],
          principals: { users: { [ro.username]: ['read'] }, groups: {} },
        },
      })
    ).status,
  ).toBe(201)

  // 列表：可见 + 只读注记 + 无创建入口
  await page.goto('/binflow/ui/admin/security/permissions')
  await expect(page.locator('[data-testid="perms-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="perms-create"]')).toHaveCount(0)
  await expect(page.locator(`[data-testid="perm-row-${target}"]`)).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="perms-page"]' })

  // 编辑器：矩阵只读（read 勾选、manage 禁用）、无保存/删除、对话框入口禁用
  await page.goto(`/binflow/ui/admin/security/permissions/${target}`)
  await expect(page.locator('[data-testid="perm-editor-readonly-note"]')).toBeVisible()
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${ro.username}-read"]`)).toBeChecked()
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${ro.username}-manage"]`)).toBeDisabled()
  await expect(page.locator('[data-testid="perm-save"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="perm-danger-zone"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="perm-repo-add"]')).toBeDisabled()
  await expectA11yClean(page, testInfo, { include: '[data-testid="perm-editor-page"]' })

  // 服务端兜底：同会话重放写请求——403（族 4 例外门的 readonly 臂：写侧
  // CanManageRepo(write=true) 恒拒）；读面存活
  const w = await sessionApi(page, 'POST', '/api/v1/permissions', {
    name: uniq('evil'),
    repos: [repo],
    includePatterns: ['**'],
    excludePatterns: [],
    principals: { users: {}, groups: {} },
  })
  expect(w.status).toBe(403)
  expect(w.text).toContain('administrator privileges')
  const r = await sessionApi(page, 'GET', '/api/v1/permissions')
  expect(r.status).toBe(200)
})

test('m-holder: coverage-in 201 / coverage-out 403 (body + B1 union), console lists the covered set (T-259)', async ({ page, browser }) => {
  await loginAs(page, 'admin')
  const repoA = uniq('t241a')
  const repoB = uniq('t241b')
  const carol = uniq('t241m')
  const pw = 't241-mh-pw'
  const tManage = uniq('t241mt') // carol 的 manage 来源（仅覆盖 repoA）
  const tOwn = uniq('t241own') // carol 覆盖集内自建
  const tEnt = uniq('t241ent') // admin 建在 repoB 上（B1 替换臂的存量）

  for (const repo of [repoA, repoB]) {
    expect((await sessionApi(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  }
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${carol}`, {
        name: carol,
        email: `${carol}@example.com`,
        password: pw,
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)
  // manage 不隐含 r/w/d（ADR-0026）：carol 只拿 repoA 的 manage 位
  expect(
    (
      await sessionApi(page, 'POST', '/api/v1/permissions', {
        name: tManage,
        repos: [repoA],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: { [carol]: ['manage'] }, groups: {} },
      })
    ).status,
  ).toBe(201)
  expect(
    (
      await sessionApi(page, 'POST', '/api/v1/permissions', {
        name: tEnt,
        repos: [repoB],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: { [carol]: ['read'] }, groups: {} },
      })
    ).status,
  ).toBe(201)

  // m-holder 会话（独立上下文；相对 URL 走 harness baseURL）
  const ctx = await browser.newContext()
  const mh = await ctx.newPage()
  await login(mh, carol, pw)

  // 读面 403：security:read 是管理面读端点——控制台编辑器的 L2 由它驱动
  const list = await sessionApi(mh, 'GET', '/api/v1/permissions')
  expect(list.status).toBe(403)

  // 覆盖集内：建 target（repos ⊆ {repoA}）→ 201（T-217 族 4 例外门放行）
  const inCov = await sessionApi(mh, 'POST', '/api/v1/permissions', {
    name: tOwn,
    repos: [repoA],
    includePatterns: ['**'],
    excludePatterns: [],
    principals: { users: {}, groups: {} },
  })
  expect(inCov.status).toBe(201)

  // 覆盖集外（body 侧）：repos 含 repoB → 403 + 覆盖集原文
  const outCov = await sessionApi(mh, 'POST', '/api/v1/permissions', {
    name: uniq('t241x'),
    repos: [repoA, repoB],
    includePatterns: ['**'],
    excludePatterns: [],
    principals: { users: {}, groups: {} },
  })
  expect(outCov.status).toBe(403)
  expect(outCov.text).toContain('manage coverage')

  // B1（替换臂存量并集）：body 只指名 repoA，但同名存量 tEnt 在 repoB 上
  // → union(body, 存量) = {repoA, repoB} ⊄ 覆盖集 → 403（跨覆盖集吊销洞闭合）
  const b1 = await sessionApi(mh, 'POST', '/api/v1/permissions', {
    name: tEnt,
    repos: [repoA],
    includePatterns: ['**'],
    excludePatterns: [],
    principals: { users: {}, groups: {} },
  })
  expect(b1.status).toBe(403)
  expect(b1.text).toContain('manage coverage')
  // 存量未被触碰（清单字节不变——B1 修复的钉死断言）
  const entAfter = await findTarget(page, tEnt)
  expect(entAfter.repos).toEqual([repoB])
  expect(entAfter.principals.users[carol]).toEqual(['read'])

  // 删除同门：覆盖集内 204；覆盖集外 403
  const delIn = await sessionApi(mh, 'DELETE', `/api/v1/permissions/${tOwn}`)
  expect(delIn.status).toBe(204)
  const delOut = await sessionApi(mh, 'DELETE', `/api/v1/permissions/${tEnt}`)
  expect(delOut.status).toBe(403)

  // 控制台呈现（m-holder 视角，T-259 形态——L2 边界卡对 m-holder 退役）：
  // 列表走 ?filter=manage 渲染覆盖集内子集（carol 的覆盖集 = {repoA}，故
  // tManage 在列、tEnt/repoB 侧零出现——服务端信息隔离的 UI 面）；编辑器
  // 对覆盖集内 target 全字段可达，覆盖集外深链（tEnt）= notFound 形态的
  // 边界说明（T-241 的「双页 L2」腿随 L2 卡退役按新形态改写，边界语义
  // 保留在列表注记 + 覆盖集外深链文案两处）
  await mh.goto('/binflow/ui/admin/security/permissions')
  await expect(mh.locator(`[data-testid="perm-row-${tManage}"]`)).toBeVisible()
  await expect(mh.locator(`[data-testid="perm-row-${tEnt}"]`)).toHaveCount(0)
  await expect(mh.locator('[data-testid="perms-page"]')).not.toContainText(repoB)
  await expect(mh.locator('[data-testid="perms-manage-note"]')).toBeVisible()
  await expect(mh.locator('[data-testid="perms-page"] [data-testid="empty-state"]')).toHaveCount(0)
  await mh.goto(`/binflow/ui/admin/security/permissions/${tManage}`)
  await expect(mh.locator(`[data-testid="perm-matrix-cell-user-${carol}-manage"]`)).toBeChecked()
  await mh.goto(`/binflow/ui/admin/security/permissions/${tEnt}`)
  await expect(mh.locator('[data-testid="perm-editor-page"] [data-testid="empty-state"]')).toBeVisible()
  await expect(mh.locator('[data-testid="perm-editor-page"]')).toContainText('manage 覆盖集')
  await ctx.close()
})

test('keyboard: dialog Enter/Space/Esc cycle, matrix Space toggle, Enter on Save submits', async ({ page }) => {
  await loginAs(page, 'admin')
  const repoA = uniq('t241ka')
  const repoB = uniq('t241kb')
  const user = uniq('t241ku')
  const target = uniq('t241kt')
  const pw = 't241-kb-pw'

  for (const repo of [repoA, repoB]) {
    expect((await sessionApi(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  }
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: pw,
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)
  expect(
    (
      await sessionApi(page, 'POST', '/api/v1/permissions', {
        name: target,
        repos: [repoA],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: { [user]: ['read'] }, groups: {} },
      })
    ).status,
  ).toBe(201)

  // 行链接：focus + Enter 进编辑器（无鼠标）
  await page.goto('/binflow/ui/admin/security/permissions')
  const row = page.locator(`[data-testid="perm-row-${target}"] a.row-link`)
  await row.focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="perm-editor-page"]')).toBeVisible()

  // 对话框打开（Enter）→ 焦点落在取消（安全默认）；Space 勾选仓库 → Esc = 取消不回填
  await page.locator('[data-testid="perm-repo-add"]').focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="perm-res-cancel"]')).toBeFocused()
  await page.locator(`[data-testid="perm-repo-pick-${repoB}"]`).focus()
  await page.keyboard.press('Space')
  await expect(page.locator('[data-testid="perm-res-repos"] [data-testid="transfer-selected"]')).toContainText(repoB)
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="perm-repos"]')).not.toContainText(repoB)

  // 重开：两步走完（Enter 下一步 → 输入 Enter 加 pattern → Enter 确定）
  await page.locator('[data-testid="perm-repo-add"]').focus()
  await page.keyboard.press('Enter')
  await page.locator(`[data-testid="perm-repo-pick-${repoB}"]`).focus()
  await page.keyboard.press('Space')
  await page.locator('[data-testid="perm-res-next"]').focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="perm-res-step"]')).toContainText('第 2 步')
  await page.locator('[data-testid="perm-pattern-input-include"]').focus()
  await page.keyboard.type('kb/**')
  await page.keyboard.press('Enter')
  await page.locator('[data-testid="perm-res-ok"]').focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText(repoB)
  await expect(page.locator('[data-testid="perm-patterns-summary"]')).toContainText('kb/**')

  // 矩阵：cell focus + Space 勾选 write
  await page.locator(`[data-testid="perm-matrix-cell-user-${user}-write"]`).focus()
  await page.keyboard.press('Space')
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${user}-write"]`)).toBeChecked()

  // 保存：focus + Enter 提交（diff 确认 → Enter 确认）
  const save = page.locator('[data-testid="perm-save"]')
  await save.focus()
  await expect(save).toBeEnabled()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await page.locator('[data-testid="confirm-accept"]').focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })

  // API 对账
  const t = await findTarget(page, target)
  expect([...t.repos].sort()).toEqual([repoA, repoB].sort())
  expect(t.principals.users[user]).toEqual(['read', 'write'])
  expect(t.includePatterns).toContain('kb/**')
})

// ---- T-266 对比度类回退的复扫腿（两主题 × 判定两态 + 用户页角色徽章） -------
// perm-verdict-* / role-warning 自持类已回退共享 .badge.{success,danger,
// warning}（T-244 把修法上收到 base.css 后冗余）。判定徽章只在测试器求值后
// 渲染——a11y-sweep 的静态路由扫不到该面，本腿显式驱动两态，light/dark 双
// 主题 axe 复扫 serious=0（等价门 = §8 语义色文字 ≥4.5:1 两主题，两配方实测
// 5.0~6.2:1，数值表见 reports/agents/T-266.md）；用户页 admin 行徽章同族面
// 附 computed 配方快照作等价性证据（ADR-0029 决策 3 允许的 token 存在性校验）。

test('contrast rollback (T-266): shared-badge verdict states + role badge axe-clean in both themes', async ({
  page,
}, testInfo) => {
  await loginAs(page, 'admin')
  const repo = uniq('t266r')
  expect((await sessionApi(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)

  const verdict = page.locator('[data-testid="perm-pattern-verdict"]')
  for (const theme of ['light', 'dark'] as const) {
    // 主题显式落盘后重进（a11y-sweep 同款——不依赖上一腿翻转残留；编辑器
    // 表单是组件本地态，重建而非切题）
    await page.goto('/binflow/ui/admin/security/permissions/new')
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/security/permissions/new')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)

    // 备料（不保存——纯本地态）：两步对话框加 include qa/** / exclude qa/tmp/**
    await page.fill('[data-testid="perm-form-name"]', uniq('t266t'))
    await page.click('[data-testid="perm-repo-add"]')
    await page.click(`[data-testid="perm-repo-pick-${repo}"]`)
    await page.click('[data-testid="perm-res-next"]')
    await page.fill('[data-testid="perm-pattern-input-include"]', 'qa/**')
    await page.click('[data-testid="perm-pattern-add-include"]')
    await page.fill('[data-testid="perm-pattern-input-exclude"]', 'qa/tmp/**')
    await page.click('[data-testid="perm-pattern-add-exclude"]')
    await page.click('[data-testid="perm-res-ok"]')

    // 命中 → 共享 .badge.success（perm-verdict-success 退役，类名实证）
    await page.fill('[data-testid="perm-pattern-test"]', 'qa/builds/app.bin')
    await expect(verdict).toContainText('匹配')
    await expect(verdict).toHaveClass(/badge success/)
    await expect(verdict).not.toHaveClass(/perm-verdict/)
    await expectA11yClean(page, testInfo, { include: '[data-testid="perm-pattern-result"]' })

    // 未命中（exclude 优先）→ 共享 .badge.danger
    await page.fill('[data-testid="perm-pattern-test"]', 'qa/tmp/x.bin')
    await expect(verdict).toContainText('不匹配')
    await expect(verdict).toHaveClass(/badge danger/)
    await expectA11yClean(page, testInfo, { include: '[data-testid="perm-pattern-result"]' })
  }

  // 用户页角色徽章（role-warning → badge warning）：内置 admin 行在场即渲染
  // 该面；双主题 computed 配方快照（token 存在性——非像素断言）
  const snapshots: Record<string, unknown> = {}
  for (const theme of ['light', 'dark'] as const) {
    await page.goto('/binflow/ui/admin/security/users')
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/security/users')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    const adminBadge = page.locator('[data-testid="user-row-admin"] .badge.warning')
    await expect(adminBadge).toHaveCount(1)
    await expect(adminBadge).toHaveText('admin')
    snapshots[theme] = await adminBadge.evaluate((el) => {
      const cs = getComputedStyle(el)
      return { color: cs.color, backgroundColor: cs.backgroundColor }
    })
    await expectA11yClean(page, testInfo, { include: '[data-testid="users-table"]' })
  }
  await testInfo.attach('t266-role-badge-computed', {
    body: JSON.stringify(snapshots, null, 2),
    contentType: 'application/json',
  })
})
