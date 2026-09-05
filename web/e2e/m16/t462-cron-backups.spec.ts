import { expect, test } from '@playwright/test'
import type { TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, seedRepos } from '../m8/support/seed'

// T-462（M16 B14，FR-145.7——GC/备份 cron 消费面 + import/export 管理页；
// parity B-1.9 / B-1.10 翻正承载——M15 Q5 推翻的呈现面）：
//
//   ① 维护面 cron 三槽：GET/PUT /api/v1/system/maintenance 的 UI 消费——
//      行集与 API 同刻对账 + gc 槽存/读回/清除全链 + 坏表达式 400 行内
//      呈现（Invalid cronExp 点名原因）。
//   ② 备份定时 CRUD：New Backup 表单 → PUT 落库（API 对账 next-run 回显）
//      + 显式 nextBackupTime（本地时区 → RFC3339 UTC 换算）+ 过去时刻
//      400 拒 + 编辑改 cron + 删除（E1 输入 key 档）。
//   ③ readonly_admin：写面禁用 + 注记（服务端 403 兜底的 UI 预收敛）。
//   ④ 复制 cron 字段：配置携带 cron（REST 种）→ 治理复制页「调度」列
//      join 呈现 + 仓库编辑页 Replications 节列表回显 + 编辑表单预填。
//   ⑤ axe 双主题：维护（GC）页 + 备份页（表单开态）serious/critical = 0。
//
// 消费端点闭集（零新端点——T-450 已落）：/api/v1/system/maintenance、
// /api/v1/system/backups、/api/v1/system/cleanup、/api/v1/replications。
// 锚源 = console-ux §10.5 T-462 批块。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 当前维护面三槽态（API 侧读数） */
async function maintenanceSlots(page: import('@playwright/test').Page): Promise<
  Record<string, { cronExp: string; nextRun: string }>
> {
  const view = await page.evaluate(
    async () => await (await fetch('/binflow/api/v1/system/maintenance')).json(),
  )
  const out: Record<string, { cronExp: string; nextRun: string }> = {}
  for (const s of view.slots as Array<{ key: string; cronExp: string; nextRun: string }>) {
    out[s.key] = { cronExp: s.cronExp, nextRun: s.nextRun }
  }
  return out
}

// ---------------------------------------------------------------------------
// ① 维护面 cron：行集对账 + gc 槽生命周期 + 坏表达式 400
// ---------------------------------------------------------------------------

test('maintenance cron: three slots reconcile with API; gc slot set/readback/clear', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/gc')
  await expect(page.locator('[data-testid="gc-cron"]')).toBeVisible()

  // 三槽行族在场，且与 GET /system/maintenance 同刻对账
  await expect(page.locator('[data-testid="gc-cron-table"]')).toBeVisible()
  const slots = await maintenanceSlots(page)
  for (const key of ['gc', 'cleanup-unused-cache', 'cleanup-virtual']) {
    await expect(page.locator(`[data-testid="gc-cron-row-${key}"]`)).toBeVisible()
    const input = page.locator(`[data-testid="gc-cron-input-${key}"]`)
    await expect(input).toHaveValue(slots[key]?.cronExp ?? '')
  }
  // gc 槽的手动执行 = 滚向危险区入口（Run Now 与手动面并存——不另设直发）
  await expect(page.locator('[data-testid="gc-cron-run-gc"]')).toBeVisible()

  // gc 槽：从未调度缺省（或读回既有值——净实例为空）→ 存表达式 → 行内
  // next-run 点亮 + API 回读对账
  const expr = '0 0/30 * * * ?'
  await page.fill('[data-testid="gc-cron-input-gc"]', expr)
  await page.click('[data-testid="gc-cron-save-gc"]')
  await expect(page.locator('[data-testid="gc-cron-input-gc"]')).toHaveValue(expr, {
    timeout: 15_000,
  })
  const afterSet = await maintenanceSlots(page)
  expect(afterSet.gc.cronExp).toBe(expr)
  expect(afterSet.gc.nextRun).not.toBe('')
  await expect(page.locator('[data-testid="gc-cron-next-gc"]')).toHaveText(
    / UTC$/,
  )
  await expect(page.locator('[data-testid="gc-cron-next-gc"]')).toHaveText(
    afterSet.gc.nextRun.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC'),
  )

  // 坏表达式：服务端 400（Invalid cronExp 点名原因）行内呈现，且零残留
  // （API 侧表达式保持原值）
  await page.fill('[data-testid="gc-cron-input-gc"]', 'not-a-cron')
  await page.click('[data-testid="gc-cron-save-gc"]')
  await expect(page.locator('[data-testid="gc-cron-row-gc"] .field-error')).toContainText(
    'Invalid cronExp not-a-cron',
  )
  const afterBad = await maintenanceSlots(page)
  expect(afterBad.gc.cronExp).toBe(expr)

  // 清除：单态——无行 = 不调度（API 回读空 + 行内「未调度」）
  await page.click('[data-testid="gc-cron-clear-gc"]')
  await expect(page.locator('[data-testid="gc-cron-input-gc"]')).toHaveValue('', {
    timeout: 15_000,
  })
  const afterClear = await maintenanceSlots(page)
  expect(afterClear.gc.cronExp).toBe('')
  await expect(page.locator('[data-testid="gc-cron-row-gc"]')).toContainText('未调度')
})

test('maintenance cron: cleanup Run Now posts the manual face (apply carrier)', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/gc')
  await expect(page.locator('[data-testid="gc-cron"]')).toBeVisible()

  // 两 cleanup 槽的「立即清理」= POST /system/cleanup {apply:true}（T-324
  // 手动面，与调度 fire 同载体）；确认对话框后请求实发
  const posted = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().includes('/api/v1/system/cleanup'),
  )
  await page.click('[data-testid="gc-cron-run-cleanup-unused-cache"]')
  await page.locator('[data-testid="confirm-dialog"] [data-testid="confirm-accept"]').click()
  const req = await posted
  expect(JSON.parse(req.postData() ?? '{}')).toEqual({ apply: true })

  // 报告聚合回 toast（净实例空跑也完成）
  await expect(page.locator('[data-testid="toast"]')).toContainText('清理完成', {
    timeout: 30_000,
  })
})

// ---------------------------------------------------------------------------
// ② 备份定时 CRUD：New Backup → next-run 回显 → 显式首跑 → 编辑 → 删除
// ---------------------------------------------------------------------------

test('backups: New Backup form lands config with cron + next-run; edit and E1 delete', async ({
  page,
}) => {
  const key = uniq('t462bk').toLowerCase()
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/backup')
  await expect(page.locator('[data-testid="backup-crud"]')).toBeVisible()
  // CLI 卡同页在场（import/export 管理面——ADR-0015 勘误②边界注记）
  await expect(page.locator('[data-testid="backup-cli"]')).toBeVisible()

  // 创建：key + cron + 绝对路径
  const form = () => page.locator('[data-testid="backup-form"]')
  const openForm = async () => {
    await page.click('[data-testid="backup-new"]')
    await expect(form()).toBeVisible()
  }
  await openForm()
  // key 形态行内预检（FE 镜像 validBackupKey——闭集外字符）
  await page.fill('[data-testid="backup-form-key"]', 'bad key!')
  await expect(page.locator('[data-testid="backup-form-key-error"]')).toContainText(
    '仅允许字母/数字',
  )
  // 缺位注记在场（§3.10 Advanced/Repositories 无载体——如实缺位）
  await expect(page.locator('[data-testid="backup-form-gap"]')).toContainText('无后端载体')
  await page.fill('[data-testid="backup-form-key"]', key)
  await page.fill('[data-testid="backup-form-cron"]', '0 0 2 ? * SAT')
  await page.fill('[data-testid="backup-form-path"]', '/tmp/t462-e2e')
  await expect(page.locator('[data-testid="backup-form-enabled"]')).toBeChecked()
  await page.click('[data-testid="backup-form-save"]')

  // 行到达 + API 对账（nextScheduleBackup 由表达式推算）
  await expect(page.locator(`[data-testid="backup-row-${key}"]`)).toBeVisible({
    timeout: 15_000,
  })
  await expect(page.locator('[data-testid="backup-table"]')).toBeVisible()
  const readBack = await page.evaluate(
    async (k) =>
      await (await fetch(`/binflow/api/v1/system/backups/${k}`)).json(),
    key,
  )
  expect(readBack.cronExp).toBe('0 0 2 ? * SAT')
  expect(readBack.exportPath).toBe('/tmp/t462-e2e')
  expect(readBack.enabled).toBe(true)
  expect(readBack.nextScheduleBackup).not.toBe('')
  await expect(page.locator(`[data-testid="backup-row-${key}"]`)).toContainText(
    readBack.nextScheduleBackup.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC'),
  )

  // 编辑：改 cron + 显式首跑时刻（datetime-local 本地时区 → RFC3339 UTC
  // 换算在浏览器侧对账——与服务端推算口径一致）
  await page.click(`[data-testid="backup-edit-${key}"]`)
  await expect(form()).toBeVisible()
  await expect(page.locator('[data-testid="backup-form-cron"]')).toHaveValue('0 0 2 ? * SAT')
  await page.fill('[data-testid="backup-form-cron"]', '0 0 3 ? * MON-FRI')
  const localNext = '2030-01-01T03:00'
  await page.fill('[data-testid="backup-form-next"]', localNext)
  await page.click('[data-testid="backup-form-save"]')
  await expect(page.locator(`[data-testid="backup-row-${key}"]`)).toContainText('0 0 3 ? * MON-FRI', {
    timeout: 15_000,
  })
  const edited = await page.evaluate(
    async (k) =>
      await (await fetch(`/binflow/api/v1/system/backups/${k}`)).json(),
    key,
  )
  expect(edited.cronExp).toBe('0 0 3 ? * MON-FRI')
  expect(edited.nextScheduleBackup).toBe(
    await page.evaluate((v) => new Date(v).toISOString().replace(/\.\d{3}Z$/, 'Z'), localNext),
  )

  // 过去时刻：服务端 400（not in the future）行内原样呈现
  await page.click(`[data-testid="backup-edit-${key}"]`)
  await page.fill('[data-testid="backup-form-next"]', '2020-01-01T00:00')
  await page.click('[data-testid="backup-form-save"]')
  await expect(page.locator('[data-testid="backup-form-error"]')).toContainText(
    'is not in the future',
  )

  // 删除：E1 输入 key 档（错名不动 / 对名放行）+ API 对账
  await page.click('[data-testid="backup-form-cancel"]')
  await page.click(`[data-testid="backup-delete-${key}"]`)
  await page.fill('[data-testid="backup-delete-confirm-key"]', `${key}-wrong`)
  // 错名：确认钮禁用（confirmDisabled 缝——E1 档不动）
  await expect(
    page.locator('[data-testid="confirm-dialog"] [data-testid="confirm-accept"]'),
  ).toBeDisabled()
  await page.locator('[data-testid="confirm-dialog"] [data-testid="confirm-cancel"]').click()
  await expect(page.locator(`[data-testid="backup-row-${key}"]`)).toBeVisible()
  await page.click(`[data-testid="backup-delete-${key}"]`)
  await page.fill('[data-testid="backup-delete-confirm-key"]', key)
  await page.locator('[data-testid="confirm-dialog"] [data-testid="confirm-accept"]').click()
  await expect(page.locator(`[data-testid="backup-row-${key}"]`)).toHaveCount(0, {
    timeout: 15_000,
  })
  const gone = await page.evaluate(
    async (k) => await (await fetch(`/binflow/api/v1/system/backups/${k}`)).status,
    key,
  )
  expect(gone).toBe(404)
})

// ---------------------------------------------------------------------------
// ③ readonly_admin：写面禁用 + 注记（403 兜底的 UI 预收敛）
// ---------------------------------------------------------------------------

test('readonly admin: cron inputs and backup editors gated with notes', async ({ page }) => {
  await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/admin/monitoring/gc')
  await expect(page.locator('[data-testid="gc-cron"]')).toBeVisible()
  for (const key of ['gc', 'cleanup-unused-cache', 'cleanup-virtual']) {
    await expect(page.locator(`[data-testid="gc-cron-input-${key}"]`)).toBeDisabled()
  }
  await expect(page.locator('[data-testid="gc-cron-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="gc-cron-gap"]')).toContainText('无后端载体')

  await page.goto('/binflow/ui/admin/monitoring/backup')
  await expect(page.locator('[data-testid="backup-crud"]')).toBeVisible()
  // 只读态：写入口不渲染（New Backup 钮 absent）+ 注记在场
  await expect(page.locator('[data-testid="backup-new"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="backup-readonly-note"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ④ 复制 cron 字段：治理页「调度」列 join + 仓编辑节回显/预填
// ---------------------------------------------------------------------------

test('replication cron: governance sched column joins config; repo form prefills', async ({
  page,
}) => {
  const repoKey = uniq('t462r').toLowerCase()
  const name = uniq('t462cfg').toLowerCase()
  // 表达式选「永不迫近」形态（12 月 31 日 3:00）——测试窗内零 fire，目标
  // 不可达也无任务噪声
  const expr = '0 0 3 31 12 ?'
  await seedRepos(m8Client(), [{ key: repoKey }])
  await loginAs(page, 'admin')
  const created = await page.evaluate(
    async ({ repoKey, name, expr }) => {
      const res = await fetch('/binflow/api/v1/replications', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name,
          source_repo: repoKey,
          target_url: 'http://127.0.0.1:1/',
          target_repo: repoKey,
          target_username: '',
          target_password: '',
          max_bandwidth_bytes_per_sec: 0,
          max_items_per_push: 1000,
          enabled: true,
          cron_exp: expr,
        }),
      })
      return { status: res.status, body: await res.json() }
    },
    { repoKey, name, expr },
  )
  expect(created.status).toBe(201)
  expect(created.body.cron_exp).toBe(expr)
  expect(created.body.next_schedule_sync).toContain('-12-31T')

  try {
    // 治理复制页：targets 表「调度」列 join 配置面回显（行序不定——按
    // 表达式文本定位本配置的调度格）
    await page.goto('/binflow/ui/admin/governance/replication')
    await expect(page.locator('[data-testid="repl-targets"]')).toBeVisible({
      timeout: 15_000,
    })
    const sched = page.locator('[data-testid^="repl-sched-"]').filter({ hasText: expr })
    await expect(sched).toHaveCount(1)
    await expect(sched).toContainText('下次 ')

    // 仓库编辑页 Replications 节：列表调度列 + 编辑表单预填
    await page.goto(`/binflow/ui/admin/repositories/${repoKey}/edit?section=replications`)
    const section = page.locator('[data-testid="form-section-replications"]')
    await expect(section).toBeVisible()
    await expect(page.locator(`[data-testid="repl-row-sched-${name}"]`)).toContainText(expr)
    await page.click(`[data-testid="repl-edit-${name}"]`)
    await expect(page.locator('[data-testid="repl-form"]')).toBeVisible()
    await expect(page.locator('[data-testid="repl-form-cron"]')).toHaveValue(expr)
    await expect(page.locator('[data-testid="repl-form-cron-hint"]')).toContainText('全量对账')
  } finally {
    // 清场：配置删除（台账行联动删）
    await page.evaluate(
      async (n) => {
        await fetch(`/binflow/api/v1/replications/${n}`, { method: 'DELETE' })
      },
      name,
    )
  }
})

// ---------------------------------------------------------------------------
// ④b 普通 user 深链：备份页 L2 收敛（backup-denied——GET system:read 403）
// ---------------------------------------------------------------------------

test('plain user: backup deep link converges to denied note (L2)', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/admin/monitoring/backup')
  await expect(page.locator('[data-testid="backup-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="backup-denied"]')).toBeVisible()
  // 写入口不渲染（无权即无写面）
  await expect(page.locator('[data-testid="backup-new"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑤ axe 双主题：维护（GC）页 + 备份页（表单开态）
// ---------------------------------------------------------------------------

test('axe: gc + backup pages clean in both themes (form open)', async ({ page }, testInfo: TestInfo) => {
  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/monitoring/gc')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="gc-cron"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="gc-page"]' })

    await page.goto('/binflow/ui/admin/monitoring/backup')
    await expect(page.locator('[data-testid="backup-crud"]')).toBeVisible()
    // 表单开态（新建）一并扫描——datetime-local / checkbox / mono 输入族
    const newBtn = page.locator('[data-testid="backup-new"]')
    if (await newBtn.count()) {
      await newBtn.click()
      await expect(page.locator('[data-testid="backup-form"]')).toBeVisible()
    }
    await expectA11yClean(page, testInfo, { include: '[data-testid="backup-page"]' })
  }
})
