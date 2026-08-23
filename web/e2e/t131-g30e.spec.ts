import { expect, test } from '@playwright/test'

// T-131 G30e：FE 删隐式目录双兜底（DM-02 FE 面）的 Playwright 回归断言。
//
// ① 无 /api/search 请求：树浏览（上传 + 浏览 + mkdir + 导航）全程不触发
//    /api/search 请求（searchListing 兜底已删除，listChildren 直走主路径）。
// ② 404 路径可达：typo 深链接 → 非根目录 404 分支（EmptyState "路径不存在"）
//    —— 不再被 searchListing 兜底吞噬。
// ③ mkdir 单段回归：建目录只发一条 PUT（不再逐段材料化）。
// ④ 大目录分页回归：多文件场景 listChildren 正常分页。
//
// 运行前提：make console && make build 的真二进制前台 serve，BASE 指向它。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

type Page = import('@playwright/test').Page

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: Page, user = ADMIN, pw = ADMIN_PW) {
  await page.fill('[data-testid="login-username"]', user)
  await page.fill('[data-testid="login-password"]', pw)
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

function watchServerErrors(page: Page): string[] {
  const bad: string[] = []
  page.on('response', (r) => {
    if (r.status() >= 500) bad.push(`${r.status()} ${r.url()}`)
  })
  return bad
}

/** 监控指定 pattern 的请求，返回计数与 URL 列表 */
function watchRequests(page: Page, pattern: string): { urls: string[]; count: () => number } {
  const urls: string[] = []
  page.on('request', (req) => {
    if (req.url().includes(pattern)) urls.push(req.url())
  })
  return { urls, count: () => urls.length }
}

test.describe.configure({ mode: 'serial' })

test('G30e-1: zero /api/search requests during tree browse (upload + mkdir + navigate)', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t131g30e')
  const { count } = watchRequests(page, '/api/search')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })

  // 上传文件到嵌套目录（触发 materializeAncestors）
  await page.goto(`/binflow/ui/repositories/${key}/tree`)
  await page.click('[data-testid="tree-upload"]')
  await page.fill('[data-testid="upload-target"]', 'deep/nested/')
  await page.setInputFiles('[data-testid="upload-file-input"]', [
    { name: 'data.bin', mimeType: 'application/octet-stream', buffer: Buffer.from('g30e-probe') },
  ])
  await expect(page.locator('[data-testid="upload-file-0"]')).toContainText('完成', { timeout: 15_000 })
  await page.click('[data-testid="upload-dialog"] .modal-actions .btn:not(.primary)') // 关闭

  // 浏览：点击进入 deep → 检查目录可见
  await expect(page.locator('[data-testid="tree-row-deep"]')).toBeVisible({ timeout: 10_000 })
  await page.click('[data-testid="tree-row-deep"]')
  await expect(page.locator('[data-testid="tree-row-nested"]')).toBeVisible({ timeout: 10_000 })
  await page.click('[data-testid="tree-row-nested"]')
  await expect(page.locator('[data-testid="tree-row-data.bin"]')).toBeVisible({ timeout: 10_000 })

  // 建目录（单 PUT 不再逐段）
  await page.click('[data-testid="tree-mkdir"]')
  await page.fill('[data-testid="tree-mkdir-input"]', 'subdir')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="tree-row-subdir"]')).toBeVisible({ timeout: 10_000 })

  // 面包屑回到根
  await page.click('[data-testid="tree-breadcrumb"] button.crumb:first-child')
  await expect(page.locator('[data-testid="tree-row-deep"]')).toBeVisible({ timeout: 10_000 })

  // 断言：全程零 /api/search 请求
  expect(count()).toBe(0)
  expect(errors).toEqual([])

  // 清理
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('G30e-2: typo deep-link renders 404 EmptyState for non-root dir (NB1 structural closure)', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t131g30e')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })

  // 直接深链到不存在的路径（非根目录 404）
  // 之前 searchListing 兜底会吞噬这个 404 并尝试搜索面重构
  // 现在应该直接渲染 EmptyState "路径不存在"
  await page.goto(`/binflow/ui/repositories/${key}/tree/nosuch/dir`)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-page"]')).toContainText('路径不存在')

  // 回仓库根按钮可用
  await page.click('button:has-text("← 回仓库根")')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${key}$`))
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()

  // 根目录不应触发 404（根目录 400 被 listChildren 处理为空列表）
  await expect(page.locator('[data-testid="tree-empty-dir"]')).toBeVisible()

  expect(errors).toEqual([])
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('G30e-3: mkdir single-segment regression — one PUT per directory', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t131g30e')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })

  // 上传文件材料化祖先：deep/nested/data.bin
  await api(page, 'PUT', `/${key}/deep/nested/data.bin`, 'probe')

  // 在已有材料化祖先的目录树中，新建子目录只发一条 PUT
  let puts = 0
  await page.route(`**/binflow/${key}/**`, async (route) => {
    if (route.request().method() === 'PUT') puts++
    await route.continue().catch(() => {})
  })

  await page.goto(`/binflow/ui/repositories/${key}/tree/deep/nested`)
  await page.click('[data-testid="tree-mkdir"]')
  await page.fill('[data-testid="tree-mkdir-input"]', 'child')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="tree-row-child"]')).toBeVisible({ timeout: 10_000 })

  // 只应有一条 PUT（child/），不应该有 deep/nested/ 的逐段 PUT
  expect(puts).toBe(1)

  // 检查目录确实创建成功
  const got = await api(page, 'GET', `/api/storage/${key}/deep/nested/child`)
  expect(got.status).toBe(200)

  expect(errors).toEqual([])
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('G30e-4: large directory pagination regression — listChildren works with >PAGE files', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t131g30e')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })

  // 上传 30 个文件到同一目录（超过 PAGE_SIZE=25 默认值，但小到不触发 BIG_DIR）
  const dir = 'many'
  for (let i = 0; i < 30; i++) {
    await api(page, 'PUT', `/${key}/${dir}/f${i.toString().padStart(3, '0')}.bin`, `payload-${i}`)
  }

  await page.goto(`/binflow/ui/repositories/${key}/tree/${dir}`)
  await page.waitForSelector('[data-testid="tree-list"] tbody tr', { timeout: 10_000 })

  const rowCount = await page.locator('[data-testid="tree-list"] tbody tr').count()
  // 大目录分页：30 个文件应该全部可见
  expect(rowCount).toBe(30)

  // 确认文件可浏览且无搜索请求
  const searchReqs = watchRequests(page, '/api/search')
  await page.click('[data-testid="tree-row-f000.bin"]')
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  expect(searchReqs.count()).toBe(0)

  expect(errors).toEqual([])
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})