import { expect, test } from '@playwright/test'

import { expectA11yClean } from './support/a11y'
import { loginAs, provisionRoles } from './support/roles'
import { m8Client, sessionApi } from './support/seed'

// T-238（FR-73 治理面 / console-m8 §6.13~§6.19 / UI-15~20）：治理五页归位
// （/admin/governance/*）+ 存储概要新页（/admin/monitoring/storage）+ 系统
// 信息页（/admin/general/settings）的交互断言——三角色腿 + 只读重放 403。
//
// 断言口径 = ADR-0029 决策 3（交互断言制；锚 = data-testid，不随路由改名）。
// 锚源：console-ux §10.2/§10.3 治理组冻结锚（gc-*/quota-*/repl-*/backup-*/
// audit-*）+ T-218 readonly 锚族 + 本票新增（storage-page/storage-refresh/
// storage-refreshed-at/storage-summary/storage-table/storage-total-row/
// storage-row-<key>/storage-progress/storage-partial/storage-empty、
// settings-health-{storage,metadata,registry}/settings-version/settings-license、
// gc-readonly-note/quotas-readonly-note/migration-readonly-note）。
//
// 运行前提与 m8 README §1 相同：make console && make build 后真二进制前台
// serve，BASE 指向它；只读重放腿走 sessionApi（同源 session fetch）。

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** en-US 千分位（formatCount 的同契约镜像——blob 计数对账用） */
const countFmt = new Intl.NumberFormat('en-US')

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

test('admin: governance 5 pages + storage summary + system info; storage table API parity; axe', async ({
  page,
}, testInfo) => {
  const client = m8Client()
  const repo = uniq('t238a')
  const QUOTA = 1024 * 1024
  // 种子：local generic + 1 MiB 配额 + 一个制品（表行 / 配额列 / 占比的数据面）
  expect(
    (
      await client.request('PUT', `/binflow/api/repositories/${repo}`, {
        body: { rclass: 'local', packageType: 'generic', quotaBytes: QUOTA },
      })
    ).status,
  ).toBeLessThan(300)
  expect(
    (
      await client.request('PUT', `/binflow/${repo}/seed.bin`, {
        raw: true,
        headers: { 'Content-Type': 'application/octet-stream' },
        body: 't238-storage-summary-fixture',
      })
    ).status,
  ).toBe(201)

  await loginAs(page, 'admin')

  // —— 治理五页归位：/admin/governance/* 逐页锚到达（§1.3 导航树）——
  const govPages: [string, string][] = [
    ['/admin/governance/audit', 'audit-page'],
    ['/admin/governance/gc', 'gc-page'],
    ['/admin/governance/quotas', 'quotas-page'],
    ['/admin/governance/replication', 'repl-page'],
    ['/admin/governance/backup', 'backup-page'],
  ]
  for (const [path, anchor] of govPages) {
    await page.goto(`/binflow/ui${path}`)
    await expect(page.locator(`[data-testid="${anchor}"]`), path).toBeVisible()
  }

  // —— 存储概要（§6.18）：刷新行 + 汇总卡 + TOTAL 首行 + 逐仓行 ——
  await page.goto('/binflow/ui/admin/monitoring/storage')
  await expect(page.locator('[data-testid="storage-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="storage-refreshed-at"]')).toContainText('UTC')
  await expect(page.locator('[data-testid="storage-summary"]')).toBeVisible()
  await expect(page.locator('[data-testid="storage-table"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="storage-total-row"]')).toContainText('100%')

  // 汇总卡 API 对账：blob 计数与 stats 一致（formatCount 同契约千分位）
  const stats = await sessionApi(page, 'GET', '/api/v1/storage/stats')
  expect(stats.status).toBe(200)
  const blobs = (stats.json as { blobs: number }).blobs
  expect(blobs).toBeGreaterThan(0)
  await expect(page.locator('[data-testid="storage-summary"]')).toContainText(countFmt.format(blobs))

  // 种子仓行：仓型/包类型 badge（wire 原值，不翻译）+ 配额列（1 MiB → MB
  // 量级）+ 用量列非空
  const row = page.locator(`[data-testid="storage-row-${repo}"]`)
  await expect(row).toBeVisible()
  await expect(row.locator('.badge', { hasText: 'local' })).toBeVisible()
  await expect(row).toContainText('generic')
  await expect(row).toContainText('MB')
  await expect(row.locator('td').nth(4)).toHaveText(/^\d+(\.\d+)? (B|KB|MB|GB|TB)$/)

  // 行用量 API 对账：usedBytes ≥ 制品字节、quotaBytes 回显
  const usage = await sessionApi(page, 'GET', `/api/v1/storage/usage/${repo}`)
  expect(usage.status).toBe(200)
  const u = usage.json as { usedBytes: number; quotaBytes: number }
  expect(u.usedBytes).toBeGreaterThanOrEqual('t238-storage-summary-fixture'.length)
  expect(u.quotaBytes).toBe(QUOTA)

  // 刷新按钮即时重取（§3.2 success：Refresh 即时重取，表格存活）
  await page.click('[data-testid="storage-refresh"]')
  await expect(page.locator('[data-testid="storage-table"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="storage-refreshed-at"]')).toContainText('UTC')

  await expectA11yClean(page, testInfo, { include: '[data-testid="storage-page"]' })

  // —— 系统信息（§6.19）：实例信息 + 健康子系统行 + 版本对账 ——
  await page.goto('/binflow/ui/admin/general/settings')
  await expect(page.locator('[data-testid="settings"]')).toBeVisible()
  await expect(page.locator('[data-testid="settings-instance"]')).toBeVisible()
  await expect(page.locator('[data-testid="settings-license"]')).toBeVisible()
  await expect(page.locator('[data-testid="settings-health"]')).toBeVisible()
  await expect(page.locator('[data-testid="settings-health-storage"]')).toBeVisible()
  await expect(page.locator('[data-testid="settings-health-metadata"]')).toBeVisible()
  await expect(page.locator('[data-testid="settings-health-registry"]')).toBeVisible()
  const version = await sessionApi(page, 'GET', '/api/system/version')
  expect(version.status).toBe(200)
  await expect(page.locator('[data-testid="settings-version"]')).toHaveText(
    `v${(version.json as { version: string }).version}`,
  )
  await expectA11yClean(page, testInfo, { include: '[data-testid="settings"]' })

  // 收尾
  await client.request('DELETE', `/binflow/api/repositories/${repo}?deleteContent=true`)
})

test('readonly_admin: governance read faces visible, write entries disabled + notes, replayed writes stay 403', async ({
  page,
}) => {
  const client = m8Client()
  const repo = uniq('t238r')
  expect(
    (
      await client.request('PUT', `/binflow/api/repositories/${repo}`, {
        body: { rclass: 'local', packageType: 'generic', quotaBytes: 10240 },
      })
    ).status,
  ).toBeLessThan(300)

  await loginAs(page, 'readonly_admin')

  // 维护（GC）页：读面到达（stats system:read）；写面禁用 + 注记（M7 §7.3）
  await page.goto('/binflow/ui/admin/governance/gc')
  await expect(page.locator('[data-testid="gc-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="gc-stats"]')).toBeVisible()
  await expect(page.locator('[data-testid="gc-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="gc-dryrun"]')).toBeDisabled()
  await expect(page.locator('[data-testid="gc-apply"]')).toBeDisabled()
  await expect(page.locator('[data-testid="gc-grace-hours"]')).toBeDisabled()
  // 迁移面板读面照常（未配置实例 501 → 降级提示；面板不因只读隐藏）
  await expect(page.locator('[data-testid="migration-panel"]')).toBeVisible()

  // 配额页：行内编辑禁用（T-218 落地）+ 页级只读注记（T-238 推广）
  await page.goto('/binflow/ui/admin/governance/quotas')
  await expect(page.locator('[data-testid="quotas-readonly-note"]')).toBeVisible()
  await expect(page.locator(`[data-testid="quota-edit-${repo}"]`)).toBeDisabled()

  // 存储概要 + 系统信息：readonly_admin 读面全通（system:read）
  await page.goto('/binflow/ui/admin/monitoring/storage')
  await expect(page.locator('[data-testid="storage-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="storage-summary"]')).toBeVisible()
  await expect(page.locator(`[data-testid="storage-row-${repo}"]`)).toBeVisible()
  await page.goto('/binflow/ui/admin/general/settings')
  await expect(page.locator('[data-testid="settings-health"]')).toBeVisible()

  // 服务端兜底：同一会话直接重放治理面写请求——403（UI 只是呈现层）
  const gcDry = await sessionApi(page, 'POST', '/api/v1/system/gc', { apply: false })
  expect(gcDry.status).toBe(403) // dry-run 也是 system:write（rbac.go 注释口径）
  const migStart = await sessionApi(page, 'POST', '/api/v1/storage/migration/start', {})
  expect(migStart.status).toBe(403)
  const quotaWrite = await sessionApi(page, 'POST', `/api/repositories/${repo}`, {
    rclass: 'local',
    packageType: 'generic',
    quotaBytes: 20480,
  })
  expect(quotaWrite.status).toBe(403)
  // 写面全灭的同时读面存活（GET 200——只读态不是 403 姿态）
  const read = await sessionApi(page, 'GET', '/api/v1/storage/stats')
  expect(read.status).toBe(200)

  // 收尾
  await client.request('DELETE', `/binflow/api/repositories/${repo}?deleteContent=true`)
})

test('user: /admin/** deep links converge L2 (storage/governance) and L3 (health hidden)', async ({ page }) => {
  await loginAs(page, 'user')

  // 存储概要：主数据面（repos/stats）403 → 整页 L2 无权限卡
  await page.goto('/binflow/ui/admin/monitoring/storage')
  await expect(page.locator('[data-testid="storage-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="storage-page"] [data-testid="empty-state"]')).toBeVisible()
  await expect(page.locator('[data-testid="storage-summary"]')).toHaveCount(0)

  // 维护（GC）：stats 403 → L2 无权限卡；危险区（写入口）不渲染（L4）
  await page.goto('/binflow/ui/admin/governance/gc')
  await expect(page.locator('[data-testid="gc-page"]')).toContainText('无权限查看存储概况')
  await expect(page.locator('[data-testid="gc-danger-zone"]')).toHaveCount(0)

  // 系统信息：实例卡（开放端点）可见；健康行 403 驱动 L3 隐藏
  await page.goto('/binflow/ui/admin/general/settings')
  await expect(page.locator('[data-testid="settings-instance"]')).toBeVisible()
  await expect(page.locator('[data-testid="settings-health"]')).toHaveCount(0)
})
