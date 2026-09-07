import { expect, test } from '@playwright/test'
import type { Page, Request, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'

// T-459（M16 批次④ B12，FR-145.5/.6b——parity B-1.11 + B-2.18 翻正）：
//
//   ① Service Status 页：总体徽标与 /api/v1/health 对账 + 子系统三行 +
//      版本对账（/api/system/version 同源）+ 调度台账节（schedules 投影）
//      ——零新端点（AC4「对位既有 metrics/health 端点」）；普通 user 深链
//      L2 收敛。
//   ② System Logs 查看器（T-494 / FR-157 数据源切换——T-459「审计承载」
//      缺位解除）：日志行与 GET /api/v1/system/logs 对账（T-493 进程日志
//      真身——slog 环形尾随）+ 尾随刷新（自动重取——请求计数）+ Pause
//      停拍 + 过滤（服务端子串——?filter 臂）+ 下载（服务端附件臂
//      binflow-service.log）；普通 user 403 → L2 + 尾随自动停。降级路径
//      （端点 404 → 审计承载延续）归 t494 spec 的降级腿。
//   ③ 导航分组（B-2.18）：监控组六页（存储/服务状态/系统日志/系统信息/
//      维护/备份）+ Webhooks 归常规组 + 四条旧深链 replace 折入新址。
//   ④ 侧栏 Search Admin Resources 过滤框：过滤生效（条目窄化 + 整组隐藏
//      + 无匹配注记 + Esc 清词）——7.161 活体形态（顶栏，A1-5 勘误）。
//   ⑤ axe 双主题：两新页 serious/critical = 0。
//
// 消费端点闭集 = 既有面（零新端点）：/api/v1/health、/api/system/version、
// /api/v1/system/schedules、/api/v1/audit。锚源 = console-ux §10.5 T-459 批。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

/** 管理侧栏某分组下的条目文案集 */
async function groupEntries(page: Page, groupTitle: string): Promise<string[]> {
  const group = page.locator(`[data-testid="app-nav"] div:has(> .nav-group-label:text-is("${groupTitle}"))`)
  return group.locator('a.nav-item').evaluateAll((els) => els.map((e) => (e.textContent ?? '').trim()))
}

// ---------------------------------------------------------------------------
// ① Service Status：health 对账 + 子系统 + 版本 + 调度台账 + user L2
// ---------------------------------------------------------------------------

test('service status: badge/subsystems/version reconcile with health + version APIs', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/status')
  await expect(page.locator('[data-testid="status-page"]')).toBeVisible()

  // 总体徽标 + 子系统三行与 /api/v1/health 同刻对账
  const health = await page.evaluate(
    async () => await (await fetch('/binflow/api/v1/health')).json(),
  )
  await expect(page.locator('[data-testid="status-badge"]')).toHaveText(health.status)
  await expect(page.locator('[data-testid="status-sys"]')).toBeVisible()
  for (const sys of ['storage', 'metadata', 'registry']) {
    await expect(page.locator(`[data-testid="status-sys-${sys}"]`)).toBeVisible()
    await expect(page.locator(`[data-testid="status-sys-${sys}"]`)).toContainText(
      (health as Record<string, { status: string }>)[sys].status,
    )
  }

  // 版本与 /api/system/version 同源（useVersion 模块级缓存同源）
  const version = await page.evaluate(
    async () => await (await fetch('/binflow/api/system/version')).json(),
  )
  await expect(page.locator('[data-testid="status-version"]')).toContainText(
    `v${(version as { version: string }).version}`,
  )
  await expect(page.locator('[data-testid="status-version"]')).toContainText(
    (version as { product: string }).product,
  )

  // 实例 URL = 浏览器在看的真实地址；节点 = 单节点（不伪造 HA 表）
  await expect(page.locator('[data-testid="status-url"]')).toHaveText(`${new URL(page.url()).origin}/binflow`)
  await expect(page.locator('[data-testid="status-nodes"]')).toContainText('单节点')

  // Uptime 如实缺位（无端点不伪造）
  await expect(page.locator('[data-testid="status-uptime-gap"]')).toContainText('无查询端点')

  // 调度台账节在场：行集或空态二择（净实例无 cron 配置 → 空态；配置过则行）
  await expect(page.locator('[data-testid="status-schedules"]')).toBeVisible()
  const sched = await page.evaluate(
    async () => await (await fetch('/binflow/api/v1/system/schedules')).json(),
  )
  const rows = (sched as { schedules: unknown[] }).schedules
  if (rows.length === 0) {
    await expect(page.locator('[data-testid="status-schedules"]')).toContainText('未配置定时任务')
  } else {
    await expect(page.locator('[data-testid="status-sched-0"]')).toBeVisible()
  }

  // 手动刷新：两读面一起重取（health 请求重发即可证）
  let sawHealth = false
  const onReq = (req: Request) => {
    if (req.url().includes('/api/v1/health')) sawHealth = true
  }
  page.on('request', onReq)
  await page.click('[data-testid="status-refresh"]')
  await expect
    .poll(async () => sawHealth, { timeout: 10_000 })
    .toBe(true)
  page.off('request', onReq)
})

test('service status: plain user deep link converges L2 (health is admin-plane)', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/admin/monitoring/status')
  await expect(page.locator('[data-testid="status-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="status-page"] [data-testid="empty-state"]')).toBeVisible()
  await expect(page.locator('[data-testid="status-overall"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ② System Logs：行对账 + 尾随刷新 + Pause + 过滤 + 下载 + user 403
// ---------------------------------------------------------------------------

test('system logs: lines reconcile with the process-log API; tail refreshes; pause stops polling', async ({ page }) => {
  // 行对账数据源 = 页面自己消费的那枚响应（并行 worker 的请求持续进环，
  // 事后再读 API 快照会滑窗——对账点应是「行内容忠实渲染了某枚真实响应」）
  let sawBody: { lines: string[] } | null = null
  page.on('response', async (res) => {
    if (!res.url().includes('/api/v1/system/logs?') || res.url().includes('download=1')) return
    try {
      sawBody = (await res.json()) as { lines: string[] }
    } catch {
      // 非 JSON 忽略
    }
  })
  await loginAs(page, 'admin') // 本会话的每个请求（含本腿 fetch）都进进程日志环
  await page.goto('/binflow/ui/admin/monitoring/logs')
  await expect(page.locator('[data-testid="logs-page"]')).toBeVisible()

  // 源说明行（T-494 切换：进程日志真身——不再是审计承载）
  await expect(page.locator('[data-testid="logs-source"]')).toContainText('GET /api/v1/system/logs')

  // 行对账：line-0 ∈ 页面消费的响应体（快照滑窗免疫）
  const pane = page.locator('[data-testid="logs-pane"]')
  await expect(pane).toBeVisible()
  const line0 = page.locator('[data-testid="logs-line-0"]')
  await expect(line0).toBeVisible()
  const line0Text = ((await line0.textContent()) ?? '').trimEnd() // 行 span 含渲染换行
  expect(sawBody, '应已捕获页面消费的 system/logs 响应').not.toBeNull()
  expect(sawBody!.lines.length).toBeGreaterThan(0)
  expect(sawBody!.lines).toContain(line0Text)
  const lineCount = await page.locator('[data-testid^="logs-line-"]').count()
  expect(lineCount).toBeGreaterThan(0)
  expect(lineCount).toBeLessThanOrEqual(sawBody!.lines.length)

  // 尾随刷新：倒计时在场；12s 窗口内至少一次自动重取（7s 周期 + 共租负载
  // 的 interval 节流余量）
  await expect(page.locator('[data-testid="logs-countdown"]')).toContainText(/秒后自动刷新/)
  let polls = 0
  page.on('request', (req) => {
    if (req.url().includes('/api/v1/system/logs?')) polls++
  })
  await page.waitForTimeout(12_000)
  expect(polls).toBeGreaterThanOrEqual(1) // 7s 周期至少触发一次自动重取

  // Pause：停拍后窗口内零新请求；文案翻「已暂停」；按钮 aria-pressed
  await page.click('[data-testid="logs-pause"]')
  await expect(page.locator('[data-testid="logs-pause"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-testid="logs-countdown"]')).toHaveText('已暂停尾随')
  const before = polls
  await page.waitForTimeout(9000)
  expect(polls).toBe(before) // 停拍窗口零自动请求

  // Refresh now：手动重取即时发一次（停拍不挡手动）
  const beforeManual = polls
  await page.click('[data-testid="logs-refresh"]')
  await page.waitForTimeout(1000)
  expect(polls).toBe(beforeManual + 1)
})

test('system logs: server-side filter narrows the tail window; download carries the attachment arm', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/logs')
  await expect(page.locator('[data-testid="logs-line-0"]')).toBeVisible()
  // 先停尾随——本腿的计数断言要稳定窗口（自动重取会滑窗）
  await page.click('[data-testid="logs-pause"]')
  await expect(page.locator('[data-testid="logs-countdown"]')).toHaveText('已暂停尾随')

  // 过滤（T-494 起服务端 ?filter 子串——本页自身的取数就是 access 行，
  // 'msg=access' 必命中；去抖后重取，窗内全部命中行）
  await page.fill('[data-testid="logs-filter"]', 'msg=access')
  await expect(page.locator('[data-testid="logs-pane"]')).toContainText('过滤命中', { timeout: 10_000 })
  const hits = await page.locator('[data-testid^="logs-line-"]').count()
  expect(hits).toBeGreaterThan(0)
  for (let i = 0; i < Math.min(hits, 5); i++) {
    await expect(page.locator(`[data-testid="logs-line-${i}"]`)).toContainText('msg=access')
  }

  // 无匹配子串：过滤空态 + 清除过滤（行集恢复）
  await page.fill('[data-testid="logs-filter"]', 'zzz-no-such-token')
  await expect(page.locator('[data-testid="logs-filter-empty"]')).toBeVisible({ timeout: 10_000 })
  await page.click('[data-testid="logs-filter-empty"] button')
  await expect(page.locator('[data-testid^="logs-line-"]')).not.toHaveCount(0)

  // 下载：服务端附件臂（T-493 download=1——Content-Disposition 定名）
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.click('[data-testid="logs-download"]'),
  ])
  expect(download.suggestedFilename()).toBe('binflow-service.log')

  // 窗口行数选择器：换档 → 按新 limit 重取（请求对账）+ 视图更新行刷新
  let sawLimit = false
  const onReq = (req: Request) => {
    if (req.url().includes('/api/v1/system/logs?') && req.url().includes('limit=200')) sawLimit = true
  }
  page.on('request', onReq)
  await page.selectOption('[data-testid="logs-limit"]', '200')
  await expect
    .poll(async () => sawLimit, { timeout: 10_000 })
    .toBe(true)
  page.off('request', onReq)
  await expect(page.locator('[data-testid="logs-lines"]')).toBeVisible()
  await expect(page.locator('[data-testid="logs-updated-at"]')).toContainText('UTC')
})

test('system logs: plain user 403 -> L2 card and tail auto-pauses', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/admin/monitoring/logs')
  await expect(page.locator('[data-testid="logs-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="logs-page"] [data-testid="empty-state"]')).toBeVisible()
  // 尾随自动停（注定 403 的轮询没有意义）——按钮呈「继续」态
  await expect(page.locator('[data-testid="logs-pause"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-testid="logs-countdown"]')).toHaveText('已暂停尾随')
})

// ---------------------------------------------------------------------------
// ③ 导航分组（B-2.18）：监控组六页 + Webhooks 常规组 + 旧深链折入
// ---------------------------------------------------------------------------

test('nav grouping: monitoring holds 6 service pages; webhooks in general; legacy URLs fold', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/storage')
  await expect(page.locator('[data-testid="storage-page"]')).toBeVisible()

  // 监控组（服务节点组）：存储/服务状态/系统日志/系统信息/维护/备份
  expect(await groupEntries(page, '监控')).toEqual([
    '存储',
    '服务状态',
    '系统日志',
    '系统信息',
    '维护（GC）',
    '备份 / 恢复',
  ])
  // 常规组：Webhooks + License & Add-ons（系统信息已迁出）
  expect(await groupEntries(page, '常规')).toEqual(['Webhooks', 'License & Add-ons'])
  // 治理组：审计/配额/复制/回收站（维护·备份已迁出）
  expect(await groupEntries(page, '治理')).toEqual(['审计日志', '配额', '复制', '回收站'])

  // 四条旧深链 replace 折入新址（T-434 ?focus= 同款一轮兼容窗）
  const folds: [string, string][] = [
    ['/admin/general/settings', '/admin/monitoring/system-info'],
    ['/admin/governance/gc', '/admin/monitoring/gc'],
    ['/admin/governance/backup', '/admin/monitoring/backup'],
    ['/admin/governance/webhooks', '/admin/general/webhooks'],
  ]
  for (const [from, to] of folds) {
    await page.goto(`/binflow/ui${from}`)
    await expect(page).toHaveURL(`/binflow/ui${to}`, { timeout: 10_000 })
  }

  // 新路由页锚逐页到达
  await page.goto('/binflow/ui/admin/monitoring/status')
  await expect(page.locator('[data-testid="status-page"]')).toBeVisible()
  await page.goto('/binflow/ui/admin/monitoring/logs')
  await expect(page.locator('[data-testid="logs-page"]')).toBeVisible()
  await page.goto('/binflow/ui/admin/monitoring/system-info')
  await expect(page.locator('[data-testid="settings"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ④ 侧栏 Search Admin Resources 过滤框（B-2.18——7.161 活体：管理态顶栏）
// ---------------------------------------------------------------------------

test('admin filter: filters sidebar entries, hides empty groups, Esc clears', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/storage')
  await expect(page.locator('[data-testid="storage-page"]')).toBeVisible()

  // 管理态顶栏 = 管理资源过滤框（制品搜索让位）；placeholder 逐字对位活体
  const box = page.locator('[data-testid="admin-filter"]')
  await expect(box).toBeVisible()
  await expect(box).toHaveAttribute('placeholder', 'Search Admin Resources…')
  await expect(page.locator('[data-testid="topbar-search"]')).toHaveCount(0)

  // 过滤生效：子串命中条目窄化 + 整组隐藏（「备份」只命中监控组的备份/恢复）
  await box.fill('备份')
  await expect(page.locator('[data-testid="app-nav"] a.nav-item')).toHaveCount(1)
  await expect(page.locator('[data-testid="app-nav"] a.nav-item')).toHaveText('备份 / 恢复')
  await expect(page.locator('.nav-group-label', { hasText: '治理' })).toHaveCount(0)

  // 无匹配：注记 + 空侧栏如实反馈
  await box.fill('zzz-none')
  await expect(page.locator('[data-testid="app-nav"] a.nav-item')).toHaveCount(0)
  await expect(page.locator('[data-testid="admin-filter-empty"]')).toBeVisible()

  // Esc 清词（两段 Esc 同款语义——直接清空）
  await box.press('Escape')
  await expect(page.locator('[data-testid="app-nav"] a.nav-item')).toHaveCount(18)
  await expect(page.locator('[data-testid="admin-filter-empty"]')).toHaveCount(0)

  // ⌘K 聚焦管理过滤框（管理模式下快捷键指向当前框）
  await page.keyboard.press('Meta+k')
  await expect(box).toBeFocused()

  // 回应用模式：制品搜索恢复（单框随模式让位）
  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()
  await expect(page.locator('[data-testid="admin-filter"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑤ axe 双主题：两新页 serious/critical = 0
// ---------------------------------------------------------------------------

test('axe: status + logs pages clean in both themes', async ({ page }, testInfo: TestInfo) => {
  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/monitoring/status')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="status-page"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="status-page"]' })

    await page.goto('/binflow/ui/admin/monitoring/logs')
    await expect(page.locator('[data-testid="logs-page"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="logs-page"]' })
  }
})
