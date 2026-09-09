import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { loginAs } from '../m8/support/roles'

// T-494（M17 W4，FR-157 FE 缝——useAsync 双计修正 + SystemLogsPage 数据源
// 切换）：
//
//   ① 纯浏览不喂下载计数器（K69 单源契约的 FE 侧缝修正）：
//      NodeDetail 的 item-info GET 经内容面 Get 落点，as-built 即 +1 下载
//      计数（T-438 §3-2「探针面计数继承」登记——计数与 audit 行同址，BE
//      侧 de-probe 归 audit 面整理票）。纯浏览（只选中不下载）因此在多计
//      下载数。修正 = 详情读改道零计数的搜索投影面（checksum 搜索〔已知
//      digest——嵌套文件〕/ 名搜索〔根层文件，root ?list 400 无合并值〕；
//      远端派生行维持 item GET = T-461 回源 pull-through 本体——t461 spec
//      的既有断言锚）。断言口径：
//        - 浏览前后 ?stats 面计数不变（0 → 0）；
//        - 计数面（无 query 的 item GET）零请求（请求级探针钉缝本体）；
//        - 既有下载计数断言零回归：内容面 GET ×3 → 详情计数 = 3（精确值
//          ——修正后浏览零贡献，绝对值首次可断言）。
//   ② SystemLogsPage 数据源切换（T-459「审计承载」缺位解除——T-493 落地
//      GET /api/v1/system/logs 三臂）：主源 = 进程日志真身（源说明行/行
//      对账/尾随/过滤/下载附件 binflow-service.log/limit 档）；降级路径
//      保留——端点 404（旧二进制）→ 粘性回落审计跟踪源（T-459 as-built
//      形态延续 + 降级注记）。whats-new/console.md 注记反转位归 T-518
//      （本票留痕不写文档）。
//
// 锚源：node-downloads（T-445 批）/ tree-row-*（T-434 批）/ logs-* 族
// （T-459 批）。夹具自备（uniq 前缀 + 收尾不回收——e2e 栈惯例沿用 t445
// 的自铸仓库形态）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

async function login(page: Page, user = ADMIN, pw = ADMIN_PW) {
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', user)
  await page.fill('[data-testid="login-password"]', pw)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** 同源 fetch（携带 session cookie）；返回 {status, json}。noStore = 下载腿
 * 专用：内容面 GET 带 Etag/Last-Modified 无 Cache-Control，浏览器对同 URL
 * 重复 fetch 走条件请求（304 不落服务端 Get 落点 = 不计数）——真实下载
 * （落盘/新会话）无此形态，下载计数腿必须 no-store。 */
async function api(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
  noStore = false,
): Promise<{ status: number; json: unknown }> {
  const r = await page.evaluate(
    async ({ method, path, body, noStore }) => {
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        cache: noStore ? 'no-store' : undefined,
      })
      const text = await res.text()
      let json: unknown = null
      try {
        json = JSON.parse(text)
      } catch {
        json = null
      }
      return { status: res.status, json }
    },
    { method, path, body, noStore },
  )
  return r
}

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

interface StatsFace {
  downloadCount: number
  remoteDownloadCount: number
}

/** ?stats 面直读（读面零计数——T-438「probes don't count」） */
async function statsFace(page: Page, repo: string, path: string): Promise<StatsFace> {
  const r = await api(page, 'GET', `/api/storage/${repo}/${path}?stats`)
  expect(r.status).toBe(200)
  return r.json as StatsFace
}

/**
 * 计数面探针：捕获打到 item-info GET（无 query 的 /api/storage/<repo>/
 * <file>）的请求——?stats/?list/?properties/?permissions 等读臂不在此列
 * （?stats 经 List 解析零计数；本探针钉的是「纯浏览不得触内容面 Get 落
 * 点」这条缝）。
 */
function countingItemProbe(page: Page, repo: string, files: string[]): string[] {
  const hits: string[] = []
  page.on('request', (req) => {
    const url = req.url()
    for (const f of files) {
      const m = url.match(new RegExp(`/api/storage/${repo}/${f.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(\\?[^#]*)?$`))
      if (!m) continue
      const query = m[1] ?? ''
      // 读臂白名单：这些 query 形态不经内容面 Get（或本就零计数）——
      // 注意值可缺席（?stats 裸键形态），= 不能进锚
      if (/[?&](stats|list|properties|permissions|docker_tags)(=|&|$)/.test(query)) continue
      hits.push(url)
    }
  })
  return hits
}

// ---------------------------------------------------------------------------
// ① 纯浏览零计数 + 下载 3 次 → 详情计数 = 3（K69 单源不破）
// ---------------------------------------------------------------------------

test('pure browse feeds no download counter; 3 real downloads show exactly 3', async ({ page }) => {
  await login(page)
  const key = uniq('t494a')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 't494-guide')
  await api(page, 'PUT', `/${key}/docs/other.md`, 't494-other')
  // 根层文件：无 ?list 合并值（root list 400）——名搜索改道臂的覆盖锚
  await api(page, 'PUT', `/${key}/readme.md`, 't494-root')

  // 基线：三文件计数全 0（上传与 ?stats 读均不计数）
  expect((await statsFace(page, key, 'docs/guide.md')).downloadCount).toBe(0)
  expect((await statsFace(page, key, 'docs/other.md')).downloadCount).toBe(0)
  expect((await statsFace(page, key, 'readme.md')).downloadCount).toBe(0)

  const probe = countingItemProbe(page, key, ['docs/guide.md', 'docs/other.md', 'readme.md'])

  // —— 纯浏览：深链（整页重挂载 = 最坏情形）+ 表行点击（target 切换）——
  await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-downloads"]')).toHaveText('0')

  // target 切换（guide → other → guide）：P2 栈 children 面为 AG Grid——
  // 树行/网格行双锚同名 + 行虚拟化下 force 点击落点不稳，改用深链切换
  // （URL 即状态——语义等价，下一段整页重挂载深链腿本就覆盖最坏情形）
  await page.goto(`/binflow/ui/artifacts/${key}/docs/other.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('other.md')
  await expect(page.locator('[data-testid="node-downloads"]')).toHaveText('0')
  await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-downloads"]')).toHaveText('0')

  // 深链往返（重挂载）+ 根层文件（名搜索臂）：同样零计数
  await page.goto(`/binflow/ui/artifacts/${key}/docs/other.md`)
  await expect(page.locator('[data-testid="node-downloads"]')).toHaveText('0')
  await page.goto(`/binflow/ui/artifacts/${key}/readme.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('readme.md')
  await expect(page.locator('[data-testid="node-downloads"]')).toHaveText('0')
  await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-downloads"]')).toHaveText('0')

  // 浏览后计数面零贡献：?stats 仍 0（UI 与面同刻一致）
  expect((await statsFace(page, key, 'docs/guide.md')).downloadCount).toBe(0)
  expect((await statsFace(page, key, 'readme.md')).downloadCount).toBe(0)
  expect(probe, `纯浏览不得触计数面（item-info GET）: ${probe.join(', ')}`).toHaveLength(0)

  // —— 既有下载计数断言（精确值——修正后浏览零贡献）：内容面 GET ×3
  //  （no-store：同 URL 重复 fetch 的条件请求 304 不落计数——见 api 注）——
  for (let i = 0; i < 3; i++) {
    expect((await api(page, 'GET', `/${key}/docs/guide.md`, undefined, true)).status).toBe(200)
  }
  expect((await statsFace(page, key, 'docs/guide.md')).downloadCount).toBe(3)

  // 详情计数 = 3（重进详情页取新读数——?stats 读面零计数，读数不 inflate）
  await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-downloads"]')).toHaveText('3')
  expect((await statsFace(page, key, 'docs/guide.md')).downloadCount).toBe(3)
  expect(probe, `详情重读仍不得触计数面: ${probe.join(', ')}`).toHaveLength(0)
})

// ---------------------------------------------------------------------------
// ② SystemLogsPage：进程日志主源 + 404 粘性降级（T-459 形态延续）
// ---------------------------------------------------------------------------

test('system logs: process-log source is primary — lines reconcile, filter narrows server-side, download attachment', async ({
  page,
}) => {
  // 行对账数据源 = 页面自己消费的那枚响应（并行 worker 的请求持续进环，
  // 事后再读 API 快照会滑窗——对账点应是「行内容忠实渲染了某枚真实响应」）
  let sawBody: { lines: string[]; count: number; held: number; truncated: boolean } | null = null
  page.on('response', async (res) => {
    if (!res.url().includes('/api/v1/system/logs?') || res.url().includes('download=1')) return
    try {
      sawBody = (await res.json()) as typeof sawBody
    } catch {
      // 非 JSON（拦截腿等）忽略
    }
  })
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/monitoring/logs')
  await expect(page.locator('[data-testid="logs-page"]')).toBeVisible()

  // 源说明行：进程日志真身（不再是审计承载）
  await expect(page.locator('[data-testid="logs-source"]')).toContainText('GET /api/v1/system/logs')
  await expect(page.locator('[data-testid="logs-source"]')).not.toContainText('GET /api/v1/audit')

  // 行对账：line-0 ∈ 页面消费的响应体（快照滑窗免疫）；环形遥测在场
  const pane = page.locator('[data-testid="logs-pane"]')
  await expect(pane).toBeVisible()
  const line0 = page.locator('[data-testid="logs-line-0"]')
  await expect(line0).toBeVisible()
  const line0Text = ((await line0.textContent()) ?? '').trimEnd() // 行 span 含渲染换行
  expect(sawBody, '应已捕获页面消费的 system/logs 响应').not.toBeNull()
  expect(sawBody!.lines.length, '响应体应含行').toBeGreaterThan(0)
  expect(sawBody!.lines).toContain(line0Text)
  expect(typeof sawBody!.held).toBe('number')

  // 尾随：倒计时在场；停拍（后续过滤/下载腿要稳定窗口）
  await expect(page.locator('[data-testid="logs-countdown"]')).toContainText(/秒后自动刷新/)
  await page.click('[data-testid="logs-pause"]')
  await expect(page.locator('[data-testid="logs-countdown"]')).toHaveText('已暂停尾随')

  // Refresh now：手动重取即时发一次（process 面请求对账）
  let sawRefresh = false
  const onReq = (url: string) => {
    if (url.includes('/api/v1/system/logs?')) sawRefresh = true
  }
  page.on('request', (req) => onReq(req.url()))
  await page.click('[data-testid="logs-refresh"]')
  await expect
    .poll(async () => sawRefresh, { timeout: 10_000 })
    .toBe(true)

  // 过滤（服务端子串——本页自身的轮询就是 access 行，'msg=access' 必命中）
  await page.fill('[data-testid="logs-filter"]', 'msg=access')
  await expect(page.locator('[data-testid="logs-pane"]')).toContainText('过滤命中', { timeout: 10_000 })
  const hits = await page.locator('[data-testid^="logs-line-"]').count()
  expect(hits).toBeGreaterThan(0)
  for (let i = 0; i < Math.min(hits, 5); i++) {
    await expect(page.locator(`[data-testid="logs-line-${i}"]`)).toContainText('msg=access')
  }

  // 无匹配子串：过滤空态 + 清除过滤（清除后行集恢复）
  await page.fill('[data-testid="logs-filter"]', 'zzz-no-such-token')
  await expect(page.locator('[data-testid="logs-filter-empty"]')).toBeVisible({ timeout: 10_000 })
  await page.click('[data-testid="logs-filter-empty"] button')
  await expect(page.locator('[data-testid^="logs-line-"]')).not.toHaveCount(0)

  // 下载：服务端附件臂（Content-Disposition 定名 binflow-service.log）
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.click('[data-testid="logs-download"]'),
  ])
  expect(download.suggestedFilename()).toBe('binflow-service.log')

  // 窗口行数选择器：换档 → 按新 limit 重取（请求对账）
  let sawLimit = false
  const onLimit = (url: string) => {
    if (url.includes('/api/v1/system/logs?') && url.includes('limit=200')) sawLimit = true
  }
  page.on('request', (req) => onLimit(req.url()))
  await page.selectOption('[data-testid="logs-limit"]', '200')
  await expect
    .poll(async () => sawLimit, { timeout: 10_000 })
    .toBe(true)
  await expect(page.locator('[data-testid="logs-lines"]')).toBeVisible()
  await expect(page.locator('[data-testid="logs-updated-at"]')).toContainText('UTC')
})

test('system logs: endpoint 404 degrades to the audit carrier (T-459 posture continues)', async ({ page }) => {
  await loginAs(page, 'admin')
  // 端点拦截（旧二进制形态模拟）：process 面 404 → 粘性回落审计源
  await page.route(/\/api\/v1\/system\/logs/, (route) =>
    route.fulfill({ status: 404, contentType: 'application/json', body: '{"errors":[{"message":"not mounted"}]}' }),
  )
  await page.goto('/binflow/ui/admin/monitoring/logs')
  await expect(page.locator('[data-testid="logs-page"]')).toBeVisible()

  // 降级注记 + T-459 as-built 源文案（审计承载原样延续）
  await expect(page.locator('[data-testid="logs-degraded"]')).toContainText('HTTP 404')
  await expect(page.locator('[data-testid="logs-source"]')).toContainText('GET /api/v1/audit')

  // 审计行渲染（本会话登录即审计事件——newest-first 行集在场）
  await expect(page.locator('[data-testid="logs-line-0"]')).toBeVisible()
  const line0Text = (await page.locator('[data-testid="logs-line-0"]').textContent()) ?? ''
  expect(line0Text).toMatch(/login\.success|actor=/)

  // 降级源 = 客户端窗口过滤（T-459 形态：不重取，窗口内窄化）
  await page.click('[data-testid="logs-pause"]') // 稳定窗口
  await page.fill('[data-testid="logs-filter"]', 'login.success')
  await expect(page.locator('[data-testid^="logs-line-"]').first()).toContainText('login.success')

  // 降级源下载 = 窗口 Blob 导出（binflow-system-log-* 命名）
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.click('[data-testid="logs-download"]'),
  ])
  const fname = download.suggestedFilename()
  expect(fname.startsWith('binflow-system-log-') && fname.endsWith('.log')).toBe(true)
})
