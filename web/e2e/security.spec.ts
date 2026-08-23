import { expect, test } from '@playwright/test'

// T-101 探针：安全组三页面（用户/组/权限 target 编辑器）真后端全流程。
//
// 运行前提：已 `make console && make build` 的真二进制在前台 serve，
// BASE 指向它（同 T-98/T-99 探针约定）；ADMIN_PW 默认 password（PRD §4）。
//
// 覆盖：
// ① W33 三步流（建组 → 建用户入组 → 建 target：repo/pattern/principals
//    users+groups 双栏 r/w/d 矩阵 + 模式测试器 + diff 确认）+ API 三实体对账
// ② W33b 组授权即时生效：第二浏览器上下文（bob 会话）内容面 PUT 201 →
//    UI 移出组 → 同会话 PUT 403（无需重登；自有 read 存活）
// ③ W33c 删被引用组：409 + 引用 target 名呈现（链接直达编辑器）→ 解除 → 可删
// ④ 用户编辑/email 往返/口令重置入口/服务端 400 行内/404 分支/非 admin L2 收敛
//
// 对账腿走 page.evaluate fetch（同源携带 session cookie——管理面与内容面
// 同凭据，ux R10 / CE-03）。并行安全：全部实体名 uniq()。

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

test('W33 three-step flow: group -> user membership -> target matrix, tester + diff, API reconciliation', async ({ page }) => {
  const group = uniq('qa')
  const user = uniq('bob')
  const target = uniq('tgt')
  const repo = uniq('t101a')
  await page.goto('/binflow/ui/security/groups')
  await login(page, ADMIN, ADMIN_PW)

  // 准备一个 local generic 仓（target 的 repos 引用面；PUT 建与更均 200——后端事实）
  const made = await api(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })
  expect(made.status).toBe(200)

  // 第一步：建组
  await page.click('[data-testid="groups-create"]')
  await page.fill('[data-testid="group-form-name"]', group)
  await page.fill('[data-testid="group-form-description"]', 'W33 probe group')
  await page.click('[data-testid="group-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `组 ${group} 已创建` })).toBeVisible({ timeout: 8000 })
  await expect(page.locator(`[data-testid="group-row-${group}"]`)).toBeVisible()

  // 第二步：建用户入组
  await page.goto('/binflow/ui/security/users')
  await page.click('[data-testid="users-create"]')
  await page.fill('[data-testid="user-form-name"]', user)
  await page.fill('[data-testid="user-form-email"]', `${user}@example.com`)
  await page.fill('[data-testid="user-form-password"]', 'w33-probe-pw')
  await page.check(`[data-testid="user-form-group-${group}"]`)
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已创建` })).toBeVisible({ timeout: 8000 })
  await expect(page.locator(`[data-testid="user-row-${user}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="user-row-${user}"]`)).toContainText(group)

  // 第三步：建 target（repo / patterns / principals 双栏 r/w/d）
  await page.goto('/binflow/ui/security/permissions/new')
  await page.fill('[data-testid="perm-form-name"]', target)
  // 两步资源对话框（T-241，console-m8 §4.5）：① 穿梭选仓 → ② pattern → 确定
  await page.click('[data-testid="perm-repo-add"]')
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toBeVisible()
  await page.check(`[data-testid="perm-repo-pick-${repo}"]`)
  await expect(page.locator('[data-testid="perm-res-repos"] [data-testid="transfer-selected"]')).toContainText(repo)
  await page.click('[data-testid="perm-res-next"]')
  // include：默认 ** 收窄为 qa/**
  await page.click('[data-testid="perm-pattern-remove-include-0"]')
  await page.fill('[data-testid="perm-pattern-input-include"]', 'qa/**')
  await page.click('[data-testid="perm-pattern-add-include"]')
  await page.fill('[data-testid="perm-pattern-input-exclude"]', 'qa/tmp/**')
  await page.click('[data-testid="perm-pattern-add-exclude"]')
  await page.click('[data-testid="perm-res-ok"]')
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText(repo)

  // 模式测试器（编辑器灵魂件）：命中 / exclude 优先 / 未命中
  await page.fill('[data-testid="perm-pattern-test"]', 'qa/builds/app.bin')
  await expect(page.locator('[data-testid="perm-pattern-verdict"]')).toContainText('匹配')
  await page.fill('[data-testid="perm-pattern-test"]', 'qa/tmp/x.bin')
  await expect(page.locator('[data-testid="perm-pattern-verdict"]')).toContainText('不匹配')
  await expect(page.locator('[data-testid="perm-pattern-result"]')).toContainText('exclude 优先')
  await page.fill('[data-testid="perm-pattern-test"]', 'other/x.bin')
  await expect(page.locator('[data-testid="perm-pattern-verdict"]')).toContainText('不匹配')

  // 主体与动作矩阵：用户行 read；组行 read+write（双栏）
  await page.selectOption('[data-testid="perm-add-user"]', user)
  await page.getByRole('button', { name: '添加用户' }).click()
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-read"]`)
  await page.selectOption('[data-testid="perm-add-group"]', group)
  await page.getByRole('button', { name: '添加组' }).click()
  await page.check(`[data-testid="perm-matrix-cell-group-${group}-read"]`)
  await page.check(`[data-testid="perm-matrix-cell-group-${group}-write"]`)

  // 保存 = diff 确认（§4.9[4]）
  await page.click('[data-testid="perm-save"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  const diff = page.locator('[data-testid="perm-diff"]')
  await expect(diff).toContainText(`添加仓库 ${repo}`)
  await expect(diff).toContainText('添加 include')
  await expect(diff).toContainText(`授予用户 ${user} read`)
  await expect(diff).toContainText(`授予组 ${group} read`)
  await expect(diff).toContainText(`授予组 ${group} write`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/permissions$/)
  await expect(page.locator(`[data-testid="perm-row-${target}"]`)).toBeVisible()

  // API 对账：三实体字段回显
  const gotGroup = await api(page, 'GET', `/api/security/groups/${group}`)
  expect(gotGroup.status).toBe(200)
  expect((gotGroup.json as { description: string }).description).toBe('W33 probe group')
  const gotUser = await api(page, 'GET', `/api/security/users/${user}`)
  expect(gotUser.status).toBe(200)
  const uj = gotUser.json as { email: string; groups: string[]; admin: boolean }
  expect(uj.email).toBe(`${user}@example.com`)
  expect(uj.groups).toEqual([group])
  expect(uj.admin).toBe(false)
  const gotTarget = await api(page, 'GET', '/api/v1/permissions')
  interface TargetEcho {
    name: string
    repos: string[]
    includePatterns: string[]
    excludePatterns: string[]
    principals: { users: Record<string, string[]>; groups: Record<string, string[]> }
  }
  const t = (gotTarget.json as TargetEcho[]).find((x) => x.name === target)
  expect(t, `target ${target} must exist`).toBeTruthy()
  expect(t!.repos).toEqual([repo])
  expect(t!.includePatterns).toEqual(['qa/**'])
  expect(t!.excludePatterns).toEqual(['qa/tmp/**'])
  expect(t!.principals.users[user]).toEqual(['read'])
  expect([...t!.principals.groups[group]].sort()).toEqual(['read', 'write'])
})

test('W33b: group grant effective for a second session; UI removal is immediate (no re-login)', async ({ page, browser }) => {
  const group = uniq('w33b')
  const user = uniq('bob')
  const target = uniq('tgt')
  const repo = uniq('t101b')
  await page.goto('/binflow/ui/')
  await login(page, ADMIN, ADMIN_PW)

  // API 备料：repo + 组 + 用户（入组，非 admin）+ target（用户行 read，组行 r+w——write 只来自组）
  expect((await api(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect((await api(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 'w33b' })).status).toBe(201)
  expect(
    (
      await api(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: 'w33b-bob-pw',
        admin: false,
        groups: [group],
      })
    ).status,
  ).toBe(201)
  expect(
    (
      await api(page, 'POST', '/api/v1/permissions', {
        name: target,
        repos: [repo],
        includePatterns: ['qa/**'],
        excludePatterns: [],
        principals: { users: { [user]: ['read'] }, groups: { [group]: ['read', 'write'] } },
      })
    ).status,
  ).toBe(201)

  // 第二浏览器上下文：bob 会话（cookie 独立）
  const origin = new URL(page.url()).origin
  const bobCtx = await browser.newContext()
  const bob = await bobCtx.newPage()
  await bob.goto(`${origin}/binflow/ui/`)
  await login(bob, user, 'w33b-bob-pw')

  // 组授权生效：bob 内容面 PUT 201（write 来自组行）
  const put1 = await api(bob, 'PUT', `/${repo}/qa/builds/app-v1.bin`, 'w33b-probe-1')
  expect(put1.status).toBe(201)

  // UI 移出组（admin 上下文）
  await page.goto(`/binflow/ui/security/users/${user}`)
  await expect(page.locator(`[data-testid="user-form-group-${group}"]`)).toBeChecked()
  await page.uncheck(`[data-testid="user-form-group-${group}"]`)
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已更新` })).toBeVisible({ timeout: 8000 })

  // 同一会话（无重登）即时失效：PUT 403；自有 read 存活（GET 200）
  const put2 = await api(bob, 'PUT', `/${repo}/qa/builds/app-v2.bin`, 'w33b-probe-2')
  expect(put2.status).toBe(403)
  const get1 = await api(bob, 'GET', `/${repo}/qa/builds/app-v1.bin`)
  expect(get1.status).toBe(200)
  await bobCtx.close()
})

test('W33c: deleting a referenced group is refused (409) naming targets; unlink then delete succeeds', async ({ page }) => {
  const group = uniq('w33c')
  const target = uniq('tgt')
  const repo = uniq('t101c')
  await page.goto('/binflow/ui/security/groups')
  await login(page, ADMIN, ADMIN_PW)

  expect((await api(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect((await api(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 'w33c' })).status).toBe(201)
  expect(
    (
      await api(page, 'POST', '/api/v1/permissions', {
        name: target,
        repos: [repo],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: {}, groups: { [group]: ['read'] } },
      })
    ).status,
  ).toBe(201)

  // 删除被引用组：409 原文 + 引用 target 名（链接直达编辑器）。
  // 组是 API 带外建的——列表在页面挂载时已拉取，交互前刷新视图
  await page.reload()
  await page.click(`[data-testid="group-delete-${group}"]`)
  await page.click('[data-testid="confirm-accept"]')
  const reason = page.locator('[data-testid="group-delete-reason"]')
  await expect(reason).toBeVisible()
  await expect(reason).toContainText(target)
  await expect(page.locator(`[data-testid="group-row-${group}"]`)).toBeVisible() // 组仍在

  // 解除引用：链接进编辑器 → 移除组主体（diff 显示撤销）→ 保存
  await page.locator('[data-testid="group-delete-reason"] a').first().click()
  await expect(page.locator('[data-testid="perm-editor-page"]')).toBeVisible()
  await page.click(`[data-testid="perm-matrix-remove-group-${group}"]`)
  await page.click('[data-testid="perm-save"]')
  const diff = page.locator('[data-testid="perm-diff"]')
  await expect(diff).toContainText(`撤销组 ${group} read`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })

  // 解除后可删（服务端纯文本文案）
  await page.goto('/binflow/ui/security/groups')
  await page.click(`[data-testid="group-delete-${group}"]`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: 'has been removed successfully' })).toBeVisible({ timeout: 8000 })
  await expect(page.locator(`[data-testid="group-row-${group}"]`)).toHaveCount(0)
  const gone = await api(page, 'GET', `/api/security/groups/${group}`)
  expect(gone.status).toBe(404)
})

test('W33c dotted target name: 409 panel parses and links the full name (review B1)', async ({ page }) => {
  const group = uniq('w33d')
  const dotted = uniq('qa.build') // 含点 target 名——permissions.go 仅校验非空，完全可达
  const repo = uniq('t101d')
  await page.goto('/binflow/ui/security/groups')
  await login(page, ADMIN, ADMIN_PW)

  expect((await api(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect((await api(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 'w33d' })).status).toBe(201)
  expect(
    (
      await api(page, 'POST', '/api/v1/permissions', {
        name: dotted,
        repos: [repo],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: {}, groups: { [group]: ['read'] } },
      })
    ).status,
  ).toBe(201)

  // 删被引用组 → 409 面板：原文与解析链接都携带完整含点名（B1 前被截成 `qa`）
  await page.reload()
  await page.click(`[data-testid="group-delete-${group}"]`)
  await page.click('[data-testid="confirm-accept"]')
  const reason = page.locator('[data-testid="group-delete-reason"]')
  await expect(reason).toBeVisible()
  await expect(reason).toContainText(dotted)
  const link = reason.locator('a').first()
  await expect(link).toContainText(dotted)

  // 链接直达该 target 的编辑器（截断名会落到 404 空态）
  await link.click()
  const editor = page.locator('[data-testid="perm-editor-page"]')
  await expect(editor).toBeVisible()
  await expect(editor).toContainText(dotted)
  await expect(editor).not.toContainText('不存在')

  // NB② 回归：删掉 include chip 再加回（仅重排）→ 集合等价 → 保存按钮禁用
  //（旧 sameSnapshot 顺序敏感会让 diff 空而按钮可点、确认框误显新建文案；
  //  T-241 起 pattern 编辑面在两步对话框第 2 步）
  await page.click('[data-testid="perm-repo-add"]')
  await page.click('[data-testid="perm-res-next"]')
  await page.click('[data-testid="perm-pattern-remove-include-0"]')
  await page.fill('[data-testid="perm-pattern-input-include"]', '**')
  await page.click('[data-testid="perm-pattern-add-include"]')
  await page.click('[data-testid="perm-res-ok"]')
  await expect(page.locator('[data-testid="perm-save"]')).toBeDisabled()

  // 解除引用 + 删组收尾（闭环与 W33c 同）
  await page.click(`[data-testid="perm-matrix-remove-group-${group}"]`)
  await page.click('[data-testid="perm-save"]')
  await expect(page.locator('[data-testid="perm-diff"]')).toContainText(`撤销组 ${group} read`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${dotted} 已保存` })).toBeVisible({ timeout: 8000 })
  await page.goto('/binflow/ui/security/groups')
  await page.click(`[data-testid="group-delete-${group}"]`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: 'has been removed successfully' })).toBeVisible({ timeout: 8000 })
})

test('users: edit roundtrip, reset-password entry, server 400 inline, 404s, non-admin L2 collapse', async ({ page, browser }) => {
  const group = uniq('d')
  const user = uniq('dave')
  const origin0 = '/binflow/ui/security/users'
  await page.goto(origin0)
  await login(page, ADMIN, ADMIN_PW)
  expect((await api(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 'd-probe' })).status).toBe(201)

  // 服务端 400 行内：表单勾选组后组被带外删除 → 提交 → 400 原文（纯文本层）
  await page.click('[data-testid="users-create"]')
  await page.fill('[data-testid="user-form-name"]', user)
  await page.fill('[data-testid="user-form-email"]', `${user}@example.com`)
  await page.fill('[data-testid="user-form-password"]', 'd-probe-pw')
  await page.check(`[data-testid="user-form-group-${group}"]`)
  await api(page, 'DELETE', `/api/security/groups/${group}`)
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="user-form-error"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-form-error"]')).toContainText(`Unable to find group by name '${group}'`)

  // 正常创建（不带组）→ 详情编辑 email 往返 + 组员维护 + 口令重置入口
  await page.uncheck(`[data-testid="user-form-group-${group}"]`) // 已带外删除，取消勾选
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已创建` })).toBeVisible({ timeout: 8000 })

  await page.goto(`/binflow/ui/security/users/${user}`)
  await page.fill('[data-testid="user-form-email"]', `${user}-new@example.com`)
  // 重置口令入口（无需旧口令）；T-237 起表单带确认口令位（console-m8 §6.9）——两处都填
  await page.fill('[data-testid="user-form-password"]', 'd-probe-pw-2')
  await page.fill('[data-testid="user-form-password2"]', 'd-probe-pw-2')
  await page.click('[data-testid="user-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `用户 ${user} 已更新` })).toBeVisible({ timeout: 8000 })
  const got = await api(page, 'GET', `/api/security/users/${user}`)
  expect(got.status).toBe(200)
  expect((got.json as { email: string }).email).toBe(`${user}-new@example.com`)
  expect((got.json as { groups: string[] }).groups).toEqual([])

  // 新口令可用（W33b 手法轻量腿：重置后的口令能登录）
  const origin = new URL(page.url()).origin
  const ctx = await browser.newContext()
  const p2 = await ctx.newPage()
  await p2.goto(`${origin}/binflow/ui/`)
  await login(p2, user, 'd-probe-pw-2')
  await expect(p2.locator('[data-testid="session-user"]')).toHaveText(user)

  // 非 admin L2 收敛：安全导航组隐藏；直链 → 无权限卡（empty-state 缺省锚）
  await expect(p2.locator('.nav-group-label', { hasText: '安全' })).toHaveCount(0)
  await expect(p2.locator('.nav-group-label', { hasText: '治理' })).toHaveCount(0)
  await p2.goto(`${origin}/binflow/ui/security/users`)
  await expect(p2.locator('[data-testid="users-page"] [data-testid="empty-state"]')).toBeVisible()
  await expect(p2.locator('[data-testid="users-page"] [data-testid="empty-state"]')).toContainText('无权限')
  await p2.goto(`${origin}/binflow/ui/settings`)
  await expect(p2.locator('[data-testid="settings-health"]')).toHaveCount(0) // 403 驱动隐藏（v1.1 N1）
  await ctx.close()

  // admin 的 settings-health 锚在（同一 403 驱动姿态的可见面）
  await page.goto('/binflow/ui/settings')
  await expect(page.locator('[data-testid="settings-health"]')).toBeVisible()

  // 404 分支
  const ghost = uniq('ghost')
  await page.goto(`/binflow/ui/security/users/${ghost}`)
  await expect(page.locator('[data-testid="user-detail-page"]')).toContainText('不存在')
  await page.goto(`/binflow/ui/security/permissions/${ghost}`)
  await expect(page.locator('[data-testid="perm-editor-page"]')).toContainText('不存在')
})

test('groups: name precheck + edit description roundtrip', async ({ page }) => {
  await page.goto('/binflow/ui/security/groups')
  await login(page, ADMIN, ADMIN_PW)

  // 前端预检：保留字与非法字符（FR-27-AC9 同口径，零写请求）
  await page.click('[data-testid="groups-create"]')
  await page.fill('[data-testid="group-form-name"]', 'anonymous')
  await expect(page.locator('[data-testid="group-form"] .field-error')).toContainText('保留名')
  await expect(page.locator('[data-testid="group-form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="group-form-name"]', 'Bad_Name')
  await expect(page.locator('[data-testid="group-form"] .field-error')).toContainText('小写字母开头')

  // 建组 → 编辑描述往返（PUT 更新态：200）
  const g = uniq('edit')
  await page.fill('[data-testid="group-form-name"]', g)
  await page.fill('[data-testid="group-form-description"]', 'first')
  await page.click('[data-testid="group-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `组 ${g} 已创建` })).toBeVisible({ timeout: 8000 })
  await page.click(`[data-testid="group-edit-${g}"]`)
  await page.fill('[data-testid="group-form-description"]', 'second')
  await page.click('[data-testid="group-form-submit"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: '描述已更新' })).toBeVisible({ timeout: 8000 })
  const got = await api(page, 'GET', `/api/security/groups/${g}`)
  expect(got.status).toBe(200)
  expect((got.json as { description: string }).description).toBe('second')

  // 收尾：未引用组直接可删
  await page.click(`[data-testid="group-delete-${g}"]`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: 'has been removed successfully' })).toBeVisible({ timeout: 8000 })
})
