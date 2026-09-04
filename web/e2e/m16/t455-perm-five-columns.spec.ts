import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, sessionApi } from '../m8/support/seed'

// T-455（M16 批次④ B10 票①，FR-146.1 FE 消费面——T-444 wire 翻新 +
// parity B-1.6/B-2.16 翻正）：
//
//   ① **矩阵五列**：编辑器用户/组矩阵 = read / annotate / write / delete /
//      manage（列序对位 7.161.20 活体 Repositories 资源型 Read / Annotate /
//      Deploy/Cache / Delete/Overwrite / Manage——探针证据
//      reports/agents/t455-probe/）；annotate 独立勾选位（wire 正名单词，
//      cell 锚 = 冻结族 perm-matrix-cell-<kind>-<principal>-<action> 的新成员）。
//   ② **wire 双层往返**：write 勾选位 → GET 回显正名单形 deploy-cache
//      （PUT 仍收 write 别名——两形等效）；annotate 勾选 → 回显 annotate；
//      保存 payload 用正名单形（网络层断言）。回显序 = read, deploy-cache,
//      annotate, delete, manage（T-444 K68.2）。
//   ③ **兼容窗水合**：legacy/正名单授权（write 或 deploy-cache 单形）水合
//      后 write 列如实勾选、annotate 不勾（拆分语义：write 不携带 annotate
//      ——零提权）；未改动保存钮禁用（GET→PUT→GET 零漂移）；补勾 annotate
//      保存 → 仅增 annotate（不连带、不丢 write）。
//   ④ **两步弹窗可点步头**：perm-res-step-{1,2} 常驻可点直跳（7.161 活体
//      同形），aria-current 标当前步；既有 perm-res-next/step 链零回退。
//   ⑤ readonly_admin 深链：annotate 列只读（disabled，同 manage 位形态）。
//
// 锚源：console-ux §10.5 T-455 批（perm-res-step-{1,2}；perm-matrix-cell 族
// 扩 annotate 成员——族口径 §10.6）；既有锚 perm-matrix{,-groups} /
// perm-matrix-cell-* / perm-res-* / perm-save / perm-diff 零改名。

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

interface TargetEcho {
  name: string
  principals: { users: Record<string, string[]>; groups: Record<string, string[]> }
}

async function findTarget(page: Page, name: string): Promise<TargetEcho> {
  const got = await sessionApi(page, 'GET', '/api/v1/permissions')
  expect(got.status).toBe(200)
  const t = (got.json as TargetEcho[]).find((x) => x.name === name)
  expect(t, `target ${name} must exist`).toBeTruthy()
  return t!
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test('admin: five-column matrix (read/annotate/write/delete/manage) with annotate round-trip', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  const repo = uniq('t455r')
  const user = uniq('t455u')
  const group = uniq('t455g')
  const target = uniq('t455t')
  expect((await sessionApi(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: 't455-probe-pw',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)
  expect((await sessionApi(page, 'PUT', `/api/security/groups/${group}`, { name: group, description: 't455' })).status).toBe(201)

  await page.goto('/binflow/ui/admin/security/permissions/new')
  await page.fill('[data-testid="perm-form-name"]', target)

  // 两步对话框：选仓 → 步头直跳第 2 步（腿④同链，一并覆盖）→ 确定
  await page.click('[data-testid="perm-repo-add"]')
  await page.click(`[data-testid="perm-repo-pick-${repo}"]`)
  await expect(page.locator('[data-testid="perm-res-step-1"]')).toHaveAttribute('aria-current', 'step')
  await page.click('[data-testid="perm-res-step-2"]')
  await expect(page.locator('[data-testid="perm-res-step"]')).toContainText('第 2 步')
  await expect(page.locator('[data-testid="perm-pattern-input-include"]')).toBeVisible()
  await page.click('[data-testid="perm-res-ok"]')
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText(repo)

  // 五列表头：两表各自 annotate 列恰一（列序对位活体）
  await expect(page.locator('[data-testid="perm-matrix"] th', { hasText: 'annotate' })).toHaveCount(1)
  await expect(page.locator('[data-testid="perm-matrix-groups"] th', { hasText: 'annotate' })).toHaveCount(1)
  const headTexts = await page.locator('[data-testid="perm-matrix"] th').allTextContents()
  expect(headTexts.slice(1).map((t) => t.trim())).toEqual(['read', 'annotate', 'write', 'delete', 'manage'])

  // 用户行 r+a+w；组行 annotate+manage（annotate 独立勾选位——不联动 write）
  await page.selectOption('[data-testid="perm-add-user"]', user)
  await page.getByRole('button', { name: '添加用户' }).click()
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-read"]`)
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-annotate"]`)
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-write"]`)
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${user}-delete"]`)).not.toBeChecked()
  await page.selectOption('[data-testid="perm-add-group"]', group)
  await page.getByRole('button', { name: '添加组' }).click()
  await page.check(`[data-testid="perm-matrix-cell-group-${group}-annotate"]`)
  await page.check(`[data-testid="perm-matrix-cell-group-${group}-manage"]`)
  await expect(page.locator(`[data-testid="perm-matrix-cell-group-${group}-write"]`)).not.toBeChecked()

  await expectA11yClean(page, testInfo, { include: '[data-testid="perm-editor-page"]' })

  // 保存 = diff 确认（annotate/write 各自一行——拆分语义可见）
  await page.click('[data-testid="perm-save"]')
  const diff = page.locator('[data-testid="perm-diff"]')
  await expect(diff).toContainText(`授予用户 ${user} annotate`)
  await expect(diff).toContainText(`授予用户 ${user} write`)
  await expect(diff).toContainText(`授予组 ${group} annotate`)
  await expect(diff).toContainText(`授予组 ${group} manage`)
  // 网络 payload 净度：PUT 体动作词 = 正名单形（write → deploy-cache）
  const posted: string[] = []
  page.on('request', (req) => {
    if (req.method() === 'POST' && req.url().includes('/api/v1/permissions')) {
      const body = req.postDataJSON() as { principals?: { users?: Record<string, string[]> } }
      posted.push(...(body.principals?.users?.[user] ?? []))
    }
  })
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })
  expect(posted).toEqual(['read', 'deploy-cache', 'annotate'])

  // GET 回显对账：正名单单形 + 正名单序（T-444 K68.2）
  const t = await findTarget(page, target)
  expect(t.principals.users[user]).toEqual(['read', 'deploy-cache', 'annotate'])
  expect([...t.principals.groups[group]].sort()).toEqual(['annotate', 'manage'])
})

test('compat window: legacy write / deploy-cache hydrate the write column, annotate stays off until checked', async ({ page }) => {
  await loginAs(page, 'admin')
  const repo = uniq('t455cw')
  const user = uniq('t455cwu')
  const target = uniq('t455cwt')
  expect((await sessionApi(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: 't455-cw-pw',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)
  // API 备料用 write 别名（PUT 收词面）：等价 deploy-cache，不附带 annotate
  expect(
    (
      await sessionApi(page, 'POST', '/api/v1/permissions', {
        name: target,
        repos: [repo],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: { [user]: ['read', 'write'] }, groups: {} },
      })
    ).status,
  ).toBe(201)

  // 水合：write 列勾选（兼容窗展示面收口——T-444 §4.2 登记的漂移由本票消
  // 除），annotate 不勾（拆分语义零提权：write 不携带 annotate）
  await page.goto(`/binflow/ui/admin/security/permissions/${target}`)
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${user}-write"]`)).toBeChecked()
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${user}-annotate"]`)).not.toBeChecked()
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${user}-read"]`)).toBeChecked()
  // 未改动 = 零漂移（GET→UI→PUT 等价：保存钮禁用）
  await expect(page.locator('[data-testid="perm-save"]')).toBeDisabled()

  // 补勾 annotate → 保存：仅增 annotate（write 位不动、不双写）
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-annotate"]`)
  await page.click('[data-testid="perm-save"]')
  const diff = page.locator('[data-testid="perm-diff"]')
  await expect(diff).toContainText(`授予用户 ${user} annotate`)
  await expect(diff).not.toContainText('移除')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })

  const t = await findTarget(page, target)
  expect(t.principals.users[user]).toEqual(['read', 'deploy-cache', 'annotate'])

  // 撤销 annotate（往返双向）：uncheck → diff 移除行 → 回显回落
  await page.goto(`/binflow/ui/admin/security/permissions/${target}`)
  await page.uncheck(`[data-testid="perm-matrix-cell-user-${user}-annotate"]`)
  await page.click('[data-testid="perm-save"]')
  await expect(page.locator('[data-testid="perm-diff"]')).toContainText(`撤销用户 ${user} annotate`)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })
  const t2 = await findTarget(page, target)
  expect(t2.principals.users[user]).toEqual(['read', 'deploy-cache'])
})

test('two-step dialog: step headers jump both ways, Next chain intact', async ({ page }) => {
  await loginAs(page, 'admin')
  const repo = uniq('t455dj')
  expect((await sessionApi(page, 'PUT', `/api/repositories/${repo}`, { rclass: 'local', packageType: 'generic' })).status).toBe(200)

  await page.goto('/binflow/ui/admin/security/permissions/new')
  await page.fill('[data-testid="perm-form-name"]', uniq('t455djt'))
  await page.click('[data-testid="perm-repo-add"]')
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toBeVisible()

  // 初始第 1 步：aria-current 在步头 1；步头 2 直跳（不经 Next）
  await expect(page.locator('[data-testid="perm-res-step-1"]')).toHaveAttribute('aria-current', 'step')
  await expect(page.locator('[data-testid="perm-res-step-2"]')).not.toHaveAttribute('aria-current', 'step')
  await page.click(`[data-testid="perm-repo-pick-${repo}"]`)
  await page.click('[data-testid="perm-res-step-2"]')
  await expect(page.locator('[data-testid="perm-res-step"]')).toContainText('第 2 步')
  await expect(page.locator('[data-testid="perm-res-step-2"]')).toHaveAttribute('aria-current', 'step')
  await expect(page.locator('[data-testid="perm-pattern-input-include"]')).toBeVisible()

  // 步头 1 回跳（pattern 输入在场与否是步内容的判据）
  await page.click('[data-testid="perm-res-step-1"]')
  await expect(page.locator('[data-testid="perm-pattern-input-include"]')).toHaveCount(0)
  await expect(page.locator(`[data-testid="perm-repo-pick-${repo}"]`)).toBeVisible()

  // 既有 Next 链零回退（m8 键盘腿的鼠标面复核）
  await page.click('[data-testid="perm-res-next"]')
  await expect(page.locator('[data-testid="perm-res-step"]')).toContainText('第 2 步')
  await page.click('[data-testid="perm-res-cancel"]')
  await expect(page.locator('[data-testid="perm-res-dialog"]')).toHaveCount(0)
})

test('readonly_admin deep link: annotate column rendered read-only like manage', async ({ page }) => {
  const ro = await loginAs(page, 'readonly_admin')
  const repo = uniq('t455ro')
  const target = uniq('t455rot')
  // 备料走 admin REST 臂（readonly 会话无写面——m8Client 同源惯例）
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
          principals: { users: { [ro.username]: ['read', 'annotate'] }, groups: {} },
        },
      })
    ).status,
  ).toBe(201)

  await page.goto(`/binflow/ui/admin/security/permissions/${target}`)
  await expect(page.locator('[data-testid="perm-editor-readonly-note"]')).toBeVisible()
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${ro.username}-annotate"]`)).toBeChecked()
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${ro.username}-annotate"]`)).toBeDisabled()
  await expect(page.locator(`[data-testid="perm-matrix-cell-user-${ro.username}-manage"]`)).toBeDisabled()
  await expect(page.locator('[data-testid="perm-save"]')).toHaveCount(0)
})
