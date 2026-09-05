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
//   ② System Logs 查看器：日志行与 /api/v1/audit 对账 + 尾随刷新（自动
//      重取——请求计数）+ Pause 停拍 + 过滤（客户端窗口窄化）+ 下载
//      （download 事件）；普通 user 403 → L2 + 尾随自动停。
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

test('system logs: lines reconcile with audit API; tail refreshes; pause stops polling', async ({ page }) => {
  await loginAs(page, 'admin') // login.success 即审计事件——净实例也有日志行
  await page.goto('/binflow/ui/admin/monitoring/logs')
  await expect(page.locator('[data-testid="logs-page"]')).toBeVisible()

  // 源说明行（7.161 三选择器的单源如实降形——不伪造源）
  await expect(page.locator('[data-testid="logs-source"]')).toContainText('GET /api/v1/audit')

  // 行对账（并发写容忍口径——T-268 storage 页同款放宽）：默认并发下其他
  // worker 的登录/建仓在「页面取数 → 本腿 API 读」之间落审计行，newest-first
  // 的 line-0 精确索引对齐结构性不可达。改为：line-0 内容 = API 快照头部
  // 窗口内的某条真实事件（行内容忠实渲染才是对账点）+ 行数 ≤ API 快照数
  //（页面只可能比后读的快照旧）。
  const audit = await page.evaluate(
    async () => await (await fetch('/binflow/api/v1/audit?limit=100')).json(),
  )
  const events = (audit as { events: { action: string; actor: string }[] }).events
  const pane = page.locator('[data-testid="logs-pane"]')
  await expect(pane).toBeVisible()
  const line0 = page.locator('[data-testid="logs-line-0"]')
  await expect(line0).toBeVisible()
  const line0Text = (await line0.textContent()) ?? ''
  const head = events.slice(0, 8) // 容忍窗口：并行 worker 在两读之间写入的行
  const matched = head.some((e) => line0Text.includes(e.action) && line0Text.includes(e.actor))
  expect(matched, `line-0 应命中 API 快照头部事件：${line0Text}`).toBe(true)
  const lineCount = await page.locator('[data-testid^="logs-line-"]').count()
  expect(lineCount).toBeGreaterThan(0)
  expect(lineCount).toBeLessThanOrEqual(events.length)

  // 尾随刷新：倒计时在场；12s 窗口内至少一次自动重取（7s 周期 + 共租负载
  // 的 interval 节流余量）
  await expect(page.locator('[data-testid="logs-countdown"]')).toContainText(/秒后自动刷新/)
  let polls = 0
  page.on('request', (req) => {
    if (req.url().includes('/api/v1/audit')) polls++
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

test('system logs: client filter narrows the window; download fires a .log file', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/logs')
  await expect(page.locator('[data-testid="logs-line-0"]')).toBeVisible()
  // 先停尾随——本腿的计数断言要稳定窗口（并行 worker 的登录会进审计流，
  // 自动重取会移动行数；停拍后窗口只在手动刷新时变化）
  await page.click('[data-testid="logs-pause"]')
  await expect(page.locator('[data-testid="logs-countdown"]')).toHaveText('已暂停尾随')
  const total = await page.locator('[data-testid^="logs-line-"]').count()
  expect(total).toBeGreaterThan(0)

  // 过滤：窗口内子串窄化（admin 的 login.success 必在——本会话刚登录）
  await page.fill('[data-testid="logs-filter"]', 'login.success')
  await expect(page.locator('[data-testid^="logs-line-"]')).not.toHaveCount(total)
  const hits = await page.locator('[data-testid^="logs-line-"]').count()
  expect(hits).toBeGreaterThan(0)
  expect(hits).toBeLessThan(total + 1)
  await expect(page.locator('[data-testid="logs-pane"]')).toContainText(`过滤命中 ${hits}`)

  // 无匹配子串：过滤空态 + 清除过滤
  await page.fill('[data-testid="logs-filter"]', 'zzz-no-such-token')
  await expect(page.locator('[data-testid="logs-filter-empty"]')).toBeVisible()
  await page.click('[data-testid="logs-filter-empty"] button')
  await expect(page.locator('[data-testid^="logs-line-"]')).toHaveCount(total)

  // 下载：当前窗口导出 .log（Blob 落盘——download 事件 + 文件名前缀/后缀；
  // toMatch(^prefix-) 形态会对账器制造伪 broken——锚前缀正则同形，用
  // startsWith/endsWith 断言）
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.click('[data-testid="logs-download"]'),
  ])
  const fname = download.suggestedFilename()
  expect(fname.startsWith('binflow-system-log-') && fname.endsWith('.log')).toBe(true)

  // 窗口行数选择器：换档 → 按新 limit 重取（请求对账）+ 视图更新行刷新
  let sawLimit = false
  const onReq = (req: Request) => {
    if (req.url().includes('/api/v1/audit?') && req.url().includes('limit=200')) sawLimit = true
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
