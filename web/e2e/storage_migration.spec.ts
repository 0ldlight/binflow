import { expect, test } from '@playwright/test'
import { existsSync } from 'node:fs'
import { join } from 'node:path'

// T-160: 存储迁移面板（GC 页「存储迁移（本地 → S3）」）——全 mock 探针，
// 不起真实实例、不跑真实迁移：
// - SPA 外壳与指纹资源直接由 dist/ 兜底（page.route fulfill；与
//   go:embed 消费的同一份构建产物，`npm run build` 后运行）；
// - /binflow/api/** 全量拦截（session / system/version / storage/stats /
//   storage/migration / storage/migration/start），未覆盖端点 404——探针
//   不依赖网络上的任何 BinFlow 实例，默认 baseURL 也只是占位。
// 覆盖：① 运行中进度渲染 + 5s 自动刷新（AC①②）；② 完成态（失败计数、
// 错误行、时间戳、红色水位条）；③ 501 未配置降级（AC 隐藏或提示）；
// ④ 非 admin 403 整面板隐藏；⑤ 轮询瞬断保留旧值不清屏。
// T-177 增补：⑥ 启动确认流程（对话框影响说明 + 输入 YES 门 + mock 202
// 状态体即刻并入 + running 态按钮退场）；⑦ 取消流程（不发 POST）；
// ⑧ 409 冲突态行内提示。①② 的 pct 断言随 AC④ 基数口径修正同步
// re-baseline（30.0/55.0/97.5 → 25.0/50.0/92.5，见 T-176 遗留③）。

const DIST = join(process.cwd(), 'dist')

test.beforeEach(() => {
  test.skip(!existsSync(join(DIST, 'index.html')), 'console not built — run `npm run build` first')
})

/** 单次迁移端点应答 */
interface MigrationReply {
  status: number
  body: unknown
}

interface MockOpts {
  /** whoami 的 admin 位（默认 true） */
  admin?: boolean
  /** /v1/storage/stats 应答码（默认 200；非 admin 腿给 403） */
  statsStatus?: number
  migration: (call: number) => MigrationReply
}

/** 安装全部 mock；返回迁移端点命中计数器 */
async function installMocks(page: import('@playwright/test').Page, opts: MockOpts): Promise<() => number> {
  let migrationCalls = 0

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
      body: JSON.stringify({ version: '6.0.0-t160', revision: 'e2e', product: 'BinFlow' }),
    }),
  )
  await page.route('**/binflow/api/v1/storage/stats', (route) =>
    route.fulfill({
      status: opts.statsStatus ?? 200,
      contentType: 'application/json',
      body: JSON.stringify(
        opts.statsStatus
          ? { errors: [{ message: 'forbidden' }] }
          : { blobs: 12483, logical_bytes: 904676213248, physical_bytes: 334451609600 },
      ),
    }),
  )
  await page.route('**/binflow/api/v1/storage/migration', (route) => {
    const reply = opts.migration(++migrationCalls)
    return route.fulfill({ status: reply.status, contentType: 'application/json', body: JSON.stringify(reply.body) })
  })

  // SPA 外壳：/binflow/ui/** 一律 index.html（history fallback 的等价物）；
  // 指纹资源在共享挂载 /binflow/assets/**（relink-assets.mjs 的产物形状）
  await page.route('**/binflow/assets/**', (route) => {
    const name = new URL(route.request().url()).pathname.replace('/binflow/assets/', '')
    return route.fulfill({ path: join(DIST, 'assets', name) })
  })
  await page.route('**/binflow/ui/**', (route) => route.fulfill({ path: join(DIST, 'index.html') }))

  return () => migrationCalls
}

// ---------------------------------------------------------------------------
// ① 运行中：进度条 + 字段 + 5s 自动刷新
// ---------------------------------------------------------------------------

test('running migration renders progress bar and auto-refreshes every 5s', async ({ page }) => {
  const migrationCalls = await installMocks(page, {
    migration: (call) => ({
      status: 200,
      body: {
        running: true,
        done: false,
        total: 1000,
        migrated: call === 1 ? 250 : 500,
        skipped: 50,
        failed: 0,
        started_at: '2026-08-22T08:00:00Z',
      },
    }),
  })
  await page.goto('/binflow/ui/admin/governance/gc')

  const panel = page.locator('[data-testid="migration-panel"]')
  await expect(panel).toBeVisible()

  // 首帧字段：AC④ 口径——分子 = migrated+failed（250+0），skipped 不在
  // 待迁移基数内 → 250/1000 = 25.0%（修正前 (250+50)/1000=30.0% 会提前触顶）
  await expect(panel.locator('[data-testid="migration-status"]')).toHaveText('迁移中')
  await expect(panel.locator('[data-testid="migration-pct"]')).toHaveText('25.0%')
  await expect(panel.locator('[data-testid="migration-bar-fill"]')).toHaveAttribute('style', /width:\s*25%/)
  await expect(panel.locator('[data-testid="migration-total"]')).toHaveText('1,000')
  await expect(panel.locator('[data-testid="migration-migrated"]')).toHaveText('250')
  await expect(panel.locator('[data-testid="migration-skipped"]')).toHaveText('50')
  await expect(panel.locator('[data-testid="migration-failed"]')).toHaveText('0')
  await expect(panel.locator('[data-testid="migration-started"]')).not.toHaveText('—')
  await expect(panel.locator('[data-testid="migration-finished"]')).toHaveText('—')

  // AC②：每 5s 自动刷新——第二次轮询到达后数值前进 500/1000 = 50.0%
  await expect(panel.locator('[data-testid="migration-pct"]')).toHaveText('50.0%', { timeout: 9_000 })
  await expect(panel.locator('[data-testid="migration-migrated"]')).toHaveText('500')
  expect(migrationCalls()).toBeGreaterThanOrEqual(2)
})

// ---------------------------------------------------------------------------
// ② 完成态（含失败）：状态标签 / 失败计数 / 错误行 / 时间戳 / 红色水位条
// ---------------------------------------------------------------------------

test('completed migration with failures shows done state, error row and red bar', async ({ page }) => {
  await installMocks(page, {
    migration: () => ({
      status: 200,
      body: {
        running: false,
        done: true,
        total: 200,
        migrated: 180,
        skipped: 15,
        failed: 5,
        error: 'copy blob sha256:9f86d0…: context deadline exceeded',
        started_at: '2026-08-22T08:00:00Z',
        finished_at: '2026-08-22T08:04:12Z',
      },
    }),
  })
  await page.goto('/binflow/ui/admin/governance/gc')

  const panel = page.locator('[data-testid="migration-panel"]')
  await expect(panel).toBeVisible()
  await expect(panel.locator('[data-testid="migration-status"]')).toHaveText('已完成（有失败）')
  // AC④ 口径：(180+5)/200 = 92.5%（skipped=15 不在基数内）；失败收尾 →
  // 水位条 danger 态（.full）
  await expect(panel.locator('[data-testid="migration-pct"]')).toHaveText('92.5%')
  await expect(panel.locator('[data-testid="migration-bar"]')).toHaveClass(/full/)
  await expect(panel.locator('[data-testid="migration-failed"]')).toHaveText('5')
  await expect(panel.locator('[data-testid="migration-error"]')).toContainText('context deadline exceeded')
  await expect(panel.locator('[data-testid="migration-finished"]')).not.toHaveText('—')
})

// ---------------------------------------------------------------------------
// ③ 501：未配置 S3 迁移——面板降级为提示，不渲染进度条
// ---------------------------------------------------------------------------

test('501 (migration not configured) degrades to hint without progress bar', async ({ page }) => {
  await installMocks(page, {
    migration: () => ({
      status: 501,
      body: { errors: [{ message: 'migration is not configured' }] },
    }),
  })
  await page.goto('/binflow/ui/admin/governance/gc')

  const panel = page.locator('[data-testid="migration-panel"]')
  await expect(panel).toBeVisible()
  await expect(panel.locator('[data-testid="migration-unconfigured"]')).toBeVisible()
  await expect(panel.locator('[data-testid="migration-bar"]')).toHaveCount(0)
  await expect(panel.locator('[data-testid="skeleton"]')).toHaveCount(0)
  // T-177 AC①：降级态不渲染启动按钮（端点都 501，触发无从谈起）
  await expect(panel.locator('[data-testid="migration-start"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ④ 非 admin：迁移端点 403 → 整面板隐藏（§3.6.3 L3），邻卡不受影响
// ---------------------------------------------------------------------------

test('non-admin (403) hides the migration panel entirely', async ({ page }) => {
  await installMocks(page, {
    admin: false,
    statsStatus: 403,
    migration: () => ({ status: 403, body: { errors: [{ message: 'forbidden' }] } }),
  })
  await page.goto('/binflow/ui/admin/governance/gc')

  await expect(page.locator('[data-testid="migration-panel"]')).toHaveCount(0)
  // 同页存储概况卡走既有的无权限呈现（非 admin 不渲染一排红卡）
  await expect(page.locator('[data-testid="gc-stats"]')).toContainText('无权限查看存储概况')
})

// ---------------------------------------------------------------------------
// ⑤ 轮询瞬断：已有数据时保留旧值 + 行内「上次刷新失败」，不清屏
// ---------------------------------------------------------------------------

test('transient poll failure keeps last good data with inline hint', async ({ page }) => {
  await installMocks(page, {
    migration: (call) =>
      call === 1
        ? {
            status: 200,
            body: { running: true, done: false, total: 100, migrated: 40, skipped: 0, failed: 0, started_at: '2026-08-22T08:00:00Z' },
          }
        : { status: 500, body: { errors: [{ message: 'boom: storage engine offline' }] } },
  })
  await page.goto('/binflow/ui/admin/governance/gc')

  const panel = page.locator('[data-testid="migration-panel"]')
  await expect(panel.locator('[data-testid="migration-pct"]')).toHaveText('40.0%')

  // 第二次轮询 5xx：进度条不清空，行内提示到达
  await expect(panel.locator('[data-testid="migration-stale"]')).toBeVisible({ timeout: 9_000 })
  await expect(panel.locator('[data-testid="migration-stale"]')).toContainText('boom: storage engine offline')
  await expect(panel.locator('[data-testid="migration-pct"]')).toHaveText('40.0%')
  await expect(panel.locator('[data-testid="error-card"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// T-177 ⑥ 启动确认流程：对话框影响说明 + 输入 YES 门 → POST 202 → running
// ---------------------------------------------------------------------------

/** 从未启动过的全 0 基线快照（loadSnapshot 零值形态） */
const NEVER_STARTED = { running: false, done: false, total: 0, migrated: 0, skipped: 0, failed: 0 }

test('start migration: confirm dialog (impact notes + YES gate) posts and merges 202 status', async ({ page }) => {
  // GET 状态随启动翻转（闭包共享）：启动前全 0 基线，POST 命中后转 running
  let started = false
  const runningBody = {
    running: true,
    done: false,
    total: 300,
    migrated: 0,
    skipped: 40,
    failed: 0,
    started_at: '2026-08-22T09:00:00Z',
  }
  await installMocks(page, { migration: () => ({ status: 200, body: started ? runningBody : NEVER_STARTED }) })
  let startCalls = 0
  await page.route('**/binflow/api/v1/storage/migration/start', (route) => {
    startCalls++
    started = true
    return route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify(runningBody),
    })
  })
  await page.goto('/binflow/ui/admin/governance/gc')

  const panel = page.locator('[data-testid="migration-panel"]')
  await expect(panel.locator('[data-testid="migration-start"]')).toBeVisible()
  await panel.locator('[data-testid="migration-start"]').click()

  // 危险确认对话框：影响说明在场（AC②：写入放大 / 中断可恢复 / 完成前不关后端）
  const dialog = page.locator('[data-testid="confirm-dialog"]')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('写入放大')
  await expect(dialog).toContainText('中断可恢复')
  await expect(dialog).toContainText('完成前不要关闭后端')
  // 二次确认门：不输 YES 确认按钮不可用（§3.5 前置缝）
  await expect(dialog.locator('[data-testid="confirm-accept"]')).toBeDisabled()
  await dialog.locator('[data-testid="migration-confirm-text"]').fill('YES')
  await expect(dialog.locator('[data-testid="confirm-accept"]')).toBeEnabled()
  await dialog.locator('[data-testid="confirm-accept"]').click()

  await expect(dialog).toHaveCount(0)
  expect(startCalls).toBe(1)
  // 202 状态体即刻并入（不等下一轮轮询）：迁移中 + running 态按钮退场
  await expect(panel.locator('[data-testid="migration-status"]')).toHaveText('迁移中')
  await expect(panel.locator('[data-testid="migration-start"]')).toHaveCount(0)
  await expect(panel.locator('[data-testid="migration-total"]')).toHaveText('300')
  await expect(page.locator('[data-testid="toast"]')).toContainText('已启动')
})

// ---------------------------------------------------------------------------
// T-177 ⑦ 取消流程：对话框退出不触发 POST，面板保持未开始
// ---------------------------------------------------------------------------

test('start migration: cancel exits dialog without posting', async ({ page }) => {
  await installMocks(page, { migration: () => ({ status: 200, body: NEVER_STARTED }) })
  let startCalls = 0
  await page.route('**/binflow/api/v1/storage/migration/start', (route) => {
    startCalls++
    return route.fulfill({ status: 202, contentType: 'application/json', body: JSON.stringify(NEVER_STARTED) })
  })
  await page.goto('/binflow/ui/admin/governance/gc')

  const panel = page.locator('[data-testid="migration-panel"]')
  await expect(panel.locator('[data-testid="migration-start"]')).toBeVisible()
  await panel.locator('[data-testid="migration-start"]').click()

  // 已满足确认门的情况下取消——取消路径不受输入门影响
  const dialog = page.locator('[data-testid="confirm-dialog"]')
  await expect(dialog).toBeVisible()
  await dialog.locator('[data-testid="migration-confirm-text"]').fill('YES')
  await dialog.locator('[data-testid="confirm-cancel"]').click()

  await expect(dialog).toHaveCount(0)
  expect(startCalls).toBe(0)
  await expect(panel.locator('[data-testid="migration-status"]')).toHaveText('未开始')
  await expect(panel.locator('[data-testid="migration-start"]')).toBeVisible()
  await expect(page.locator('[data-testid="toast"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// T-177 ⑧ 409 冲突态：启动被拒 → 行内红面板（标题 + 服务端 message mono）
// ---------------------------------------------------------------------------

test('start migration: 409 conflict shows inline rejection panel', async ({ page }) => {
  await installMocks(page, { migration: () => ({ status: 200, body: NEVER_STARTED }) })
  await page.route('**/binflow/api/v1/storage/migration/start', (route) =>
    route.fulfill({
      status: 409,
      contentType: 'application/json',
      body: JSON.stringify({ errors: [{ message: 'storage: migration: migration is not enabled' }] }),
    }),
  )
  await page.goto('/binflow/ui/admin/governance/gc')

  const panel = page.locator('[data-testid="migration-panel"]')
  await panel.locator('[data-testid="migration-start"]').click()
  const dialog = page.locator('[data-testid="confirm-dialog"]')
  await dialog.locator('[data-testid="migration-confirm-text"]').fill('YES')
  await dialog.locator('[data-testid="confirm-accept"]').click()
  await expect(dialog).toHaveCount(0)

  const err = panel.locator('[data-testid="migration-start-error"]')
  await expect(err).toBeVisible()
  await expect(err).toContainText('409')
  // 服务端 message 原样呈现（排障要比对 API，§4.10 同口径）
  await expect(err).toContainText('migration is not enabled')
  // 拒绝后停在未开始态：按钮在（可重试），无成功 toast
  await expect(panel.locator('[data-testid="migration-start"]')).toBeVisible()
  await expect(page.locator('[data-testid="toast"]')).toHaveCount(0)
})
