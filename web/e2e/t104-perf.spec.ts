import { execSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { request as pwRequest, expect, test } from '@playwright/test'

// T-104 QA 性能记录腿（正式口径归 T-105，本文件只记录 + UI 面硬断言）：
// ① FR-25-AC6：UI 上传 1GB → 服务进程 RSS 增量 < 256MB（流式服务端 +
//    浏览器 4MB 分片哈希——T-100 遗留 8 的实测归属）
// ② NFR-P20 P1：50 并发登录无 session 串号（50 独立 cookie jar，身份
//    回读与 session 值唯一性双断言）
// ③ SPA 冷加载时间记录（fresh context 空缓存，3 次采样）
//
// 运行前提：QA_SERVER_PID 指向被测 serve 进程（RSS 采样）；1GB 播种文件
// 惰性生成于 QA_TMPDIR（默认 /tmp/t104）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'
const TMP = process.env.QA_TMPDIR ?? '/tmp/t104'
const GIG = `${TMP}/gig.bin`

type Page = import('@playwright/test').Page

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: Page) {
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

async function api(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; text: string }> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      })
      return { status: res.status, text: await res.text() }
    },
    { method, path, body },
  )
}

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

function rssKb(pid: number): number {
  return Number(execSync(`ps -o rss= -p ${pid}`, { encoding: 'utf8' }).trim())
}

test('FR-25-AC6: 1GB UI upload completes; server RSS delta < 256MB', async ({ page }) => {
  test.setTimeout(600_000)
  const pid = Number(process.env.QA_SERVER_PID ?? 0)
  test.skip(!pid, 'QA_SERVER_PID not set — RSS sampling unavailable')
  const errors: string[] = []
  page.on('response', (r) => {
    if (r.status() >= 500) errors.push(`${r.status()} ${r.url()}`)
  })

  if (!existsSync(GIG)) {
    execSync(`mkdir -p ${TMP} && dd if=/dev/urandom of=${GIG} bs=1048576 count=1024 2>/dev/null`)
  }
  const localSha = execSync(`shasum -a 256 ${GIG}`, { encoding: 'utf8' }).split(' ')[0]

  const key = uniq('t104gig')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })

  const baseline = rssKb(pid)
  const samples: number[] = [baseline]
  const sampler = setInterval(() => {
    try {
      samples.push(rssKb(pid))
    } catch {
      /* 进程退出即停 */
    }
  }, 1_000)

  await page.goto(`/binflow/ui/repositories/${key}/tree`)
  await page.click('[data-testid="tree-deploy"]')
  await page.fill('[data-testid="deploy-target"]', 'bulk/')
  // 本版 Playwright 的 FilePayload 仅收 buffer——1GB 走路径形态（文件名取
  // basename gig.bin），避免 Node 侧 1GB 常驻与 CDP 整块搬运
  await page.setInputFiles('[data-testid="deploy-file-input"]', GIG)
  await page.click('[data-testid="deploy-submit"]')
  await expect(page.locator('[data-testid="deploy-row-gig.bin"]')).toContainText('上传完成 201', {
    timeout: 420_000,
  })
  await expect(page.locator('[data-testid="deploy-verify-gig.bin"]')).toContainText('✓ checksum 一致')
  await page.click('[data-testid="deploy-close"]')

  // 落库对账 + 收尾采样（GC 回落窗口）
  const item = JSON.parse((await api(page, 'GET', `/api/storage/${key}/bulk/gig.bin`)).text)
  expect(item.checksums.sha256).toBe(localSha)
  await page.waitForTimeout(4_000)
  clearInterval(sampler)

  const peak = Math.max(...samples)
  const deltaKb = peak - baseline
  console.log(
    `[t104-perf] 1GB upload: baseline=${baseline}KB peak=${peak}KB delta=${deltaKb}KB (${(deltaKb / 1024).toFixed(1)}MB), samples=${samples.length}`,
  )
  expect(deltaKb).toBeLessThan(256 * 1024)
  expect(errors).toEqual([])

  // 清理（scratch 实例瘦身；1GB blob 转 GC 候选）
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('NFR-P20 P1: 50 concurrent logins, no session cross-talk', async ({ page }) => {
  const errors: string[] = []
  page.on('response', (r) => {
    if (r.status() >= 500) errors.push(`${r.status()} ${r.url()}`)
  })
  // page 初始 url 为 about:blank（origin 不可用）——base 取配置面
  const base = process.env.BASE ?? new URL(page.url()).origin
  await page.goto('/binflow/ui/')
  await login(page)

  // 3 个探针用户轮转 × 50 并发登录（API 上下文 = 独立 cookie jar）
  const users = [uniq('cc1'), uniq('cc2'), uniq('cc3')]
  const pw = 'cc-probe-pw-1'
  for (const u of users) {
    expect(
      (
        await api(page, 'PUT', `/api/security/users/${u}`, {
          name: u,
          email: `${u}@example.com`,
          password: pw,
          admin: false,
          groups: [],
        })
      ).status,
    ).toBeLessThan(300)
  }

  const results = await Promise.all(
    Array.from({ length: 50 }, async (_, i) => {
      const expectUser = users[i % users.length]!
      const ctx = await pwRequest.newContext({ baseURL: base })
      try {
        const login = await ctx.post('/binflow/api/v1/session', {
          data: { username: expectUser, password: pw },
        })
        const cookie = login.headers()['set-cookie'] ?? ''
        const who = await ctx.get('/binflow/api/v1/session')
        const body = (await who.json()) as { username?: string }
        return { loginStatus: login.status(), whoStatus: who.status(), expectUser, gotUser: body.username, cookie }
      } finally {
        await ctx.dispose()
      }
    }),
  )

  expect(results.filter((r) => r.loginStatus === 200)).toHaveLength(50)
  expect(results.filter((r) => r.whoStatus === 200)).toHaveLength(50)
  // 无串号：每个 jar 回读身份 == 请求身份
  expect(results.filter((r) => r.gotUser === r.expectUser)).toHaveLength(50)
  // session 值唯一：50 个互不相同的 binflow_session（无复用/串发）
  const sessions = results.map((r) => /binflow_session=([^;]+)/.exec(r.cookie)?.[1] ?? '')
  expect(sessions.filter((s) => s !== '')).toHaveLength(50)
  expect(new Set(sessions).size).toBe(50)
  expect(errors).toEqual([])
})

test('SPA cold-load timing record (fresh context x3, record-only)', async ({ browser }) => {
  const timings: unknown[] = []
  for (let i = 0; i < 3; i++) {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    const t0 = Date.now()
    await page.goto('/binflow/ui/')
    await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
    const tVisible = Date.now() - t0
    const nav = await page.evaluate(() => {
      const n = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming
      return { dclMs: Math.round(n.domContentLoadedEventEnd), loadMs: Math.round(n.loadEventEnd) }
    })
    timings.push({ run: i + 1, tVisibleMs: tVisible, ...nav })
    await ctx.close()
  }
  console.log('[t104-perf] SPA cold load (fresh context, empty cache):', JSON.stringify(timings))
  expect(timings).toHaveLength(3)
})
