import { expect, test } from '@playwright/test'
import { existsSync } from 'node:fs'
import { join } from 'node:path'

// T-159: 复制面板（治理组「复制」页——push 目标状态 + 事件列表）——全
// mock 探针，不起真实实例：
// - SPA 外壳与指纹资源由 dist/ 兜底（page.route fulfill；与 go:embed
//   消费的同一份构建产物，`npm run build` 后运行）；
// - /binflow/api/** 全量拦截（session / system/version /
//   replication/status），未覆盖端点 404——探针不依赖网络上的任何
//   BinFlow 实例。
// - GET /api/v1/replication/status 端点在服务端尚未桥接（T-162 遗留），
//   响应形状为 T-159 推定的假设契约（internal/replication/model.go 的
//   ReplicationConfig + ConfigStatus + ReplicationTask → JSON）——桥接票
//   以 web/src/pages/governance/ReplicationPage.tsx 的类型注释为对齐基准。
// 覆盖：① 目标表 + 事件表渲染（计数/状态徽标/截断 sha256/拷贝锚）；
// ② 10s 自动刷新（AC②）；③ 空 targets 优雅降级；④ 501 未启用 / 404 未
// 桥接降级；⑤ 非 admin 403 → 无权限空态；⑥ 轮询瞬断保留旧值不清屏。

const DIST = join(process.cwd(), 'dist')

test.beforeEach(() => {
  test.skip(!existsSync(join(DIST, 'index.html')), 'console not built — run `npm run build` first')
})

/** 单次状态端点应答 */
interface StatusReply {
  status: number
  body: unknown
}

interface MockOpts {
  /** whoami 的 admin 位（默认 true） */
  admin?: boolean
  replication: (call: number) => StatusReply
}

/** 安装全部 mock；返回状态端点命中计数器 */
async function installMocks(page: import('@playwright/test').Page, opts: MockOpts): Promise<() => number> {
  let statusCalls = 0

  // 兜底先注册（Playwright 后注册者优先）：未覆盖的 API 一律 404，
  // 防止任何请求漏到真实网络
  await page.route('**/binflow/api/**', (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ errors: [{ message: 'unmocked endpoint' }] }),
    }),
  )
  await page.route('**/binflow/api/v1/session', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ username: 'admin', admin: opts.admin ?? true }),
    }),
  )
  await page.route('**/binflow/api/system/version', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ version: '6.0.0-t159', revision: 'e2e', product: 'BinFlow' }),
    }),
  )
  await page.route('**/binflow/api/v1/replication/status', (route) => {
    const reply = opts.replication(++statusCalls)
    return route.fulfill({ status: reply.status, contentType: 'application/json', body: JSON.stringify(reply.body) })
  })

  // SPA 外壳：/binflow/ui/** 一律 index.html（history fallback 的等价物）；
  // 指纹资源在共享挂载 /binflow/assets/**（relink-assets.mjs 的产物形状）
  await page.route('**/binflow/assets/**', (route) => {
    const name = new URL(route.request().url()).pathname.replace('/binflow/assets/', '')
    return route.fulfill({ path: join(DIST, 'assets', name) })
  })
  await page.route('**/binflow/ui/**', (route) => route.fulfill({ path: join(DIST, 'index.html') }))

  return () => statusCalls
}

/** 目标行 fixture（假设契约形状：配置行 + ConfigStatus 拍平） */
function target(over: Partial<Record<string, unknown>> = {}): Record<string, unknown> {
  return {
    id: 1,
    name: 'dr-site',
    source_repo: 'libs-release',
    target_url: 'https://dr.example.com',
    target_repo: 'libs-release',
    enabled: true,
    pending: 0,
    in_progress: 0,
    succeeded: 0,
    failed: 0,
    skipped: 0,
    last_success_at: '',
    updated_at: '2026-08-20T10:00:00Z',
    ...over,
  }
}

// sha256 fixture（真实 sha256：hello / test / abc）
const SHA_HELLO = '2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824'
const SHA_TEST = '9f86d088884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08'
const SHA_ABC = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'

const FULL_STATUS = {
  targets: [
    target({ id: 1, name: 'dr-site', pending: 2, in_progress: 1, succeeded: 1204, skipped: 5, last_success_at: '2026-08-22T07:59:01Z' }),
    target({
      id: 2,
      name: 'edge-mirror',
      source_repo: 'docker-local',
      target_url: 'http://edge.internal:8080',
      target_repo: 'docker-edge',
      enabled: false,
      succeeded: 57,
      failed: 3,
    }),
    target({ id: 3, name: 'staging-push', source_repo: 'raw-prod', target_url: 'https://staging.example.com', target_repo: 'raw-staging', failed: 2, succeeded: 810, last_success_at: '2026-08-21T06:00:00Z' }),
  ],
  events: [
    {
      id: 4567,
      replication_id: 2,
      blob_sha256: SHA_TEST,
      node_path: 'org/app/1.0/app-1.0.bin',
      status: 'failed',
      attempts: 6,
      last_error: 'PUT /binflow/docker-edge/org/app/1.0/app-1.0.bin: connection refused',
      created_at: '2026-08-22T07:58:50Z',
      completed_at: '2026-08-22T07:59:40Z',
    },
    {
      id: 4566,
      replication_id: 1,
      blob_sha256: SHA_HELLO,
      node_path: 'com/acme/core/2.1/core-2.1.jar',
      status: 'success',
      attempts: 1,
      last_error: '',
      created_at: '2026-08-22T07:57:01Z',
      completed_at: '2026-08-22T07:57:02Z',
    },
    {
      id: 4565,
      replication_id: 3,
      blob_sha256: SHA_ABC,
      node_path: 'com/acme/cli/0.9/cli-0.9.tgz',
      status: 'pending',
      attempts: 0,
      last_error: '',
      created_at: '2026-08-22T07:59:10Z',
      completed_at: '',
    },
  ],
}

// ---------------------------------------------------------------------------
// ① 目标表 + 事件表：状态标签 / URL / 仓库映射 / 计数 / 截断 sha256 / 拷贝锚
// ---------------------------------------------------------------------------

test('renders target list and event list with counts, badges and copy anchors', async ({ page }) => {
  await installMocks(page, { replication: () => ({ status: 200, body: FULL_STATUS }) })
  await page.goto('/binflow/ui/admin/governance/replication')

  const targets = page.locator('[data-testid="repl-targets"]')
  await expect(targets).toBeVisible()

  // 目标 0：in_progress 优先于 pending →「复制中」；计数与时间齐备
  const row0 = targets.locator('[data-testid="repl-target-0"]')
  await expect(row0).toContainText('复制中')
  await expect(row0).toContainText('dr-site')
  await expect(row0).toContainText('https://dr.example.com')
  await expect(row0).toContainText('libs-release → libs-release')
  await expect(row0).toContainText('1,204')
  // URL 拷贝锚（P2：mono 标识可复制；aria-label = CopyButton 的「复制 」前缀 + 对象描述）
  await expect(row0.locator(`button[aria-label="复制 目标 URL dr-site"]`)).toBeVisible()
  // 上次成功时间已渲染（非空；不做精确断言防本地时区 flake）
  await expect(row0.locator('td').nth(8)).not.toHaveText('—')

  // 目标 1：已停用分支 + 从未成功 →「—」
  const row1 = targets.locator('[data-testid="repl-target-1"]')
  await expect(row1).toContainText('已停用')
  await expect(row1).toContainText('edge.internal:8080')
  await expect(row1).toContainText('docker-local → docker-edge')
  await expect(row1).toContainText('3')
  await expect(row1.locator('td').nth(8)).toHaveText('—')

  // 目标 2：失败优先于排队 →「异常（2 失败）」
  await expect(targets.locator('[data-testid="repl-target-2"]')).toContainText('异常（2 失败）')

  // 事件表：状态徽标原样呈现、制品路径按 replication_id 补源仓前缀、
  // sha256 截断 + 完整值拷贝、失败行错误原因
  const events = page.locator('[data-testid="repl-events"]')
  await expect(events).toBeVisible()
  const ev0 = events.locator('[data-testid="repl-event-0"]')
  await expect(ev0.locator('.badge')).toHaveText('failed')
  await expect(ev0).toContainText('docker-local/org/app/1.0/app-1.0.bin')
  // sha256 展示截断（7 位头 + 4 位尾），拷贝仍为完整值（§7.3）
  await expect(ev0).toContainText('9f86d08…0a08')
  await expect(ev0).toContainText('connection refused')
  await expect(ev0).toContainText('6')
  await expect(ev0.locator(`button[aria-label^="复制 sha256 ${SHA_TEST.slice(0, 7)}"]`)).toBeVisible()

  const ev1 = events.locator('[data-testid="repl-event-1"]')
  await expect(ev1.locator('.badge')).toHaveText('success')
  await expect(ev1).toContainText('libs-release/com/acme/core/2.1/core-2.1.jar')
  // 成功行无错误 → em dash
  await expect(ev1.locator('td').nth(5)).toHaveText('—')

  await expect(events.locator('[data-testid="repl-event-2"] .badge')).toHaveText('pending')
})

// ---------------------------------------------------------------------------
// ② AC②：每 10s 自动刷新——第二次轮询到达后状态前进
// ---------------------------------------------------------------------------

test('auto-refreshes every 10s and reflects new state', async ({ page }) => {
  const statusCalls = await installMocks(page, {
    replication: (call) =>
      call === 1
        ? { status: 200, body: { targets: [target()], events: [] } }
        : {
            status: 200,
            body: { targets: [target({ failed: 1 })], events: [] },
          },
  })
  await page.goto('/binflow/ui/admin/governance/replication')

  const row = page.locator('[data-testid="repl-target-0"]')
  // 首帧：无任务 → 正常
  await expect(row).toContainText('正常')

  // 第二次轮询（10s）：failed=1 → 异常（1 失败）
  await expect(row).toContainText('异常（1 失败）', { timeout: 15_000 })
  expect(statusCalls()).toBeGreaterThanOrEqual(2)
})

// ---------------------------------------------------------------------------
// ③ 空 targets / 空 events：优雅降级为空态（AC：未配置复制）
// ---------------------------------------------------------------------------

test('empty payload degrades to empty states without tables', async ({ page }) => {
  await installMocks(page, { replication: () => ({ status: 200, body: { targets: [], events: [] } }) })
  await page.goto('/binflow/ui/admin/governance/replication')

  await expect(page.locator('[data-testid="repl-empty-targets"]')).toBeVisible()
  await expect(page.locator('[data-testid="repl-empty-events"]')).toBeVisible()
  await expect(page.locator('[data-testid="repl-targets-table"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="repl-events-table"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="skeleton"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ④ 501（实例未启用复制）与 404（端点未桥接——当前真实二进制的实际行为）
//    都降级为提示，不渲染表格
// ---------------------------------------------------------------------------

test('501 (replication disabled) degrades to hint', async ({ page }) => {
  await installMocks(page, {
    replication: () => ({ status: 501, body: { errors: [{ message: 'replication is not configured' }] } }),
  })
  await page.goto('/binflow/ui/admin/governance/replication')

  const hint = page.locator('[data-testid="repl-unavailable"]')
  await expect(hint).toBeVisible()
  await expect(hint).toContainText('未启用复制')
  await expect(page.locator('[data-testid="repl-targets"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="repl-events"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="skeleton"]')).toHaveCount(0)
})

test('404 (endpoint not bridged yet) degrades to hint', async ({ page }) => {
  await installMocks(page, {
    replication: () => ({ status: 404, body: { errors: [{ message: 'not found' }] } }),
  })
  await page.goto('/binflow/ui/admin/governance/replication')

  const hint = page.locator('[data-testid="repl-unavailable"]')
  await expect(hint).toBeVisible()
  await expect(hint).toContainText('尚未桥接')
  await expect(page.locator('[data-testid="repl-targets"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑤ 非 admin：403 → 页面主数据面单张无权限空态（§3.6.3 L2，缺省锚）
// ---------------------------------------------------------------------------

test('non-admin (403) shows permission empty state', async ({ page }) => {
  await installMocks(page, {
    admin: false,
    replication: () => ({ status: 403, body: { errors: [{ message: 'forbidden' }] } }),
  })
  await page.goto('/binflow/ui/admin/governance/replication')

  await expect(page.locator('[data-testid="empty-state"]')).toContainText('无权限查看复制状态')
  await expect(page.locator('[data-testid="repl-targets"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="error-card"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑥ 轮询瞬断：已有数据时保留旧值 + 行内「上次刷新失败」，不清屏
// ---------------------------------------------------------------------------

test('transient poll failure keeps last good data with inline hint', async ({ page }) => {
  await installMocks(page, {
    replication: (call) =>
      call === 1
        ? { status: 200, body: FULL_STATUS }
        : { status: 500, body: { errors: [{ message: 'boom: metadata db is closed' }] } },
  })
  await page.goto('/binflow/ui/admin/governance/replication')

  const row = page.locator('[data-testid="repl-target-0"]')
  await expect(row).toContainText('复制中')

  // 第二次轮询 5xx：表格不清空，行内提示到达
  await expect(page.locator('[data-testid="repl-stale"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="repl-stale"]')).toContainText('boom: metadata db is closed')
  await expect(row).toContainText('复制中')
  await expect(page.locator('[data-testid="repl-target-0"]')).toContainText('1,204')
  await expect(page.locator('[data-testid="error-card"]')).toHaveCount(0)
})
