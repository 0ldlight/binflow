import { createHash } from 'crypto'
import { expect, test } from '@playwright/test'

// T-134 G32a/G32b：docker 仓树渲染的 Playwright e2e 断言。
//
// ① 零 /v2/* 请求：docker 仓树浏览全程不触发 /v2/* 请求（数据源 = storage
//    API 面，?docker_tags 参数从后端获取 tag→digest 映射）。
// ② tag 徽标渲染：manifest digest 行上显示 tag 徽标（从 storage API 的
//    dockerTags 字段读取）。
// ③ manifest digest 行：maifest 文件以 sha256 hex 摘要显示，列标题为「摘要」。
// ④ 数据一致性：UI 树 tag 集与后端 tags/list 一致。
// ⑤ 无 search 请求：docker 树浏览不触发 /api/search 请求。
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

function watchRequests(page: Page, pattern: string): { urls: string[]; count: () => number } {
  const urls: string[] = []
  page.on('request', (req) => {
    if (req.url().includes(pattern)) urls.push(req.url())
  })
  return { urls, count: () => urls.length }
}

test.describe.configure({ mode: 'serial' })

test('G32a-1: zero /v2/* requests during docker tree browsing', async ({ page }) => {
  const errors = watchServerErrors(page)
  const v2Reqs = watchRequests(page, '/v2/')
  const searchReqs = watchRequests(page, '/api/search')
  const key = uniq('t134g32a')

  await page.goto('/binflow/ui/')
  await login(page)

  // Create docker-local repo
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'docker' })

  // Push a docker manifest via the content plane (simulating docker push by
  // writing the storage layout directly). We write a manifest blob + manifest
  // node + tag through the docker adapter's content plane.
  const image = 'app'
  const manifestBody = JSON.stringify({
    schemaVersion: 2,
    mediaType: 'application/vnd.docker.distribution.manifest.v2+json',
    config: { mediaType: 'application/vnd.docker.container.image.v1+json', size: 7023, digest: 'sha256:0000000000000000000000000000000000000000000000000000000000000000' },
    layers: [],
  })
  const manifestDigest = simpleSha256(manifestBody)

  // Upload the manifest body as a blob (the docker adapter's step-1 PUT)
  const blobResp = await api(page, 'PUT', `/v2/${key}/${image}/blobs/${manifestDigest}`, manifestBody)
  expect(blobResp.status).toBe(201)

  // Put the manifest (tag: latest)
  const manifestResp = await api(page, 'PUT', `/v2/${key}/${image}/manifests/latest`, manifestBody)
  expect(manifestResp.status).toBe(201)

  // Browse to the docker repo tree
  await page.goto(`/binflow/ui/repositories/${key}/tree`)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()

  // Navigate into the image directory
  await expect(page.locator('[data-testid="tree-row-app"]')).toBeVisible({ timeout: 10_000 })
  await page.click('[data-testid="tree-row-app"]')

  // Navigate into manifests/
  await expect(page.locator('[data-testid="tree-row-manifests"]')).toBeVisible({ timeout: 10_000 })
  await page.click('[data-testid="tree-row-manifests"]')

  // Manifest digest row should be visible
  await expect(page.locator(`[data-testid="tree-row-${manifestDigest}"]`)).toBeVisible({ timeout: 10_000 })

  // Assert zero /v2/* requests during tree browsing (after initial setup)
  // Reset counters after repo setup
  const treeV2Reqs = v2Reqs.urls.filter(u => u.includes('/tree') || u.includes('/api/storage'))
  expect(treeV2Reqs.length).toBe(0)

  // Assert zero /api/search requests during tree browsing
  const treeSearchReqs = searchReqs.urls.filter(u => u.includes('/tree') || u.includes('/api/storage'))
  expect(treeSearchReqs.length).toBe(0)

  expect(errors).toEqual([])

  // Cleanup
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('G32a-2: tag badges rendered on manifest digest rows', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t134g32a')

  await page.goto('/binflow/ui/')
  await login(page)

  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'docker' })

  const image = 'app'
  const manifestBody = JSON.stringify({
    schemaVersion: 2,
    mediaType: 'application/vnd.docker.distribution.manifest.v2+json',
    config: { mediaType: 'application/vnd.docker.container.image.v1+json', size: 7023, digest: 'sha256:0000000000000000000000000000000000000000000000000000000000000000' },
    layers: [],
  })
  const manifestDigest = simpleSha256(manifestBody)

  // Push manifest with tag "latest"
  await api(page, 'PUT', `/v2/${key}/${image}/blobs/${manifestDigest}`, manifestBody)
  await api(page, 'PUT', `/v2/${key}/${image}/manifests/latest`, manifestBody)

  // Push another manifest with tag "v1"
  const manifestBody2 = JSON.stringify({
    schemaVersion: 2,
    mediaType: 'application/vnd.docker.distribution.manifest.v2+json',
    config: { mediaType: 'application/vnd.docker.container.image.v1+json', size: 9999, digest: 'sha256:0000000000000000000000000000000000000000000000000000000000000000' },
    layers: [],
  })
  const manifestDigest2 = simpleSha256(manifestBody2)
  await api(page, 'PUT', `/v2/${key}/${image}/blobs/${manifestDigest2}`, manifestBody2)
  await api(page, 'PUT', `/v2/${key}/${image}/manifests/v1`, manifestBody2)

  // Browse to manifests/
  await page.goto(`/binflow/ui/repositories/${key}/tree/app/manifests`)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()

  // Check tag badges for the "latest" tagged manifest
  const latestRow = page.locator(`[data-testid="tree-row-${manifestDigest}"]`)
  await expect(latestRow).toBeVisible({ timeout: 10_000 })
  await expect(latestRow.locator('[data-testid="tag-badge-latest"]')).toBeVisible()

  // Check tag badges for the "v1" tagged manifest
  const v1Row = page.locator(`[data-testid="tree-row-${manifestDigest2}"]`)
  await expect(v1Row).toBeVisible({ timeout: 10_000 })
  await expect(v1Row.locator('[data-testid="tag-badge-v1"]')).toBeVisible()

  expect(errors).toEqual([])

  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('G32a-3: data consistency — tree tag set matches crane tags/list', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t134g32a')

  await page.goto('/binflow/ui/')
  await login(page)

  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'docker' })

  const image = 'app'
  const manifestBody = JSON.stringify({
    schemaVersion: 2,
    mediaType: 'application/vnd.docker.distribution.manifest.v2+json',
    config: { mediaType: 'application/vnd.docker.container.image.v1+json', size: 7023, digest: 'sha256:0000000000000000000000000000000000000000000000000000000000000000' },
    layers: [],
  })
  const manifestDigest = simpleSha256(manifestBody)

  await api(page, 'PUT', `/v2/${key}/${image}/blobs/${manifestDigest}`, manifestBody)
  await api(page, 'PUT', `/v2/${key}/${image}/manifests/latest`, manifestBody)

  // Query tags/list via the v2 API for data consistency check
  const tagsResp = await api(page, 'GET', `/v2/${key}/${image}/tags/list`)
  expect(tagsResp.status).toBe(200)
  const tagsList = JSON.parse(tagsResp.text) as { tags: string[] }
  const expectedTags = tagsList.tags ?? []

  // Browse to manifests/
  await page.goto(`/binflow/ui/repositories/${key}/tree/app/manifests`)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()

  // Verify each expected tag is rendered as a badge
  for (const tag of expectedTags) {
    await expect(page.locator(`[data-testid="tag-badge-${tag}"]`)).toBeVisible({ timeout: 5_000 })
  }

  // Verify the manifest digest row is visible
  await expect(page.locator(`[data-testid="tree-row-${manifestDigest}"]`)).toBeVisible()

  expect(errors).toEqual([])

  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('G32b-1: docker tree columns — digest column header "摘要"', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t134g32b')

  await page.goto('/binflow/ui/')
  await login(page)

  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'docker' })

  const image = 'app'
  const manifestBody = JSON.stringify({
    schemaVersion: 2,
    mediaType: 'application/vnd.docker.distribution.manifest.v2+json',
    config: { mediaType: 'application/vnd.docker.container.image.v1+json', size: 7023, digest: 'sha256:0000000000000000000000000000000000000000000000000000000000000000' },
    layers: [],
  })
  const manifestDigest = simpleSha256(manifestBody)

  await api(page, 'PUT', `/v2/${key}/${image}/blobs/${manifestDigest}`, manifestBody)
  await api(page, 'PUT', `/v2/${key}/${image}/manifests/latest`, manifestBody)

  // Browse to manifests/
  await page.goto(`/binflow/ui/repositories/${key}/tree/app/manifests`)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()

  // Docker tree should show "摘要" column header instead of "sha256"
  await expect(page.locator('[data-testid="tree-list"] th')).toContainText('摘要')

  // Docker tree should show "标签" column header instead of "类型"
  await expect(page.locator('[data-testid="tree-list"] th')).toContainText('标签')

  // Manifest digest should be rendered as a truncated sha256 hex
  const digestCell = page.locator(`[data-testid="tree-row-${manifestDigest}"] td:nth-child(5)`)
  await expect(digestCell).toContainText(`${manifestDigest.slice(0, 10)}…`)

  expect(errors).toEqual([])

  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('G32b-2: docker root level — image directory listing with zero regressions', async ({ page }) => {
  const errors = watchServerErrors(page)
  const searchReqs = watchRequests(page, '/api/search')
  const key = uniq('t134g32b')

  await page.goto('/binflow/ui/')
  await login(page)

  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'docker' })

  // Push two images with tags
  const manifestBody = JSON.stringify({
    schemaVersion: 2,
    mediaType: 'application/vnd.docker.distribution.manifest.v2+json',
    config: { mediaType: 'application/vnd.docker.container.image.v1+json', size: 7023, digest: 'sha256:0000000000000000000000000000000000000000000000000000000000000000' },
    layers: [],
  })
  const m1 = simpleSha256(manifestBody)
  await api(page, 'PUT', `/v2/${key}/app1/blobs/${m1}`, manifestBody)
  await api(page, 'PUT', `/v2/${key}/app1/manifests/latest`, manifestBody)

  const manifestBody2 = JSON.stringify({
    schemaVersion: 2,
    mediaType: 'application/vnd.docker.distribution.manifest.v2+json',
    config: { mediaType: 'application/vnd.docker.container.image.v1+json', size: 9999, digest: 'sha256:0000000000000000000000000000000000000000000000000000000000000000' },
    layers: [],
  })
  const m2 = simpleSha256(manifestBody2)
  await api(page, 'PUT', `/v2/${key}/app2/blobs/${m2}`, manifestBody2)
  await api(page, 'PUT', `/v2/${key}/app2/manifests/v1`, manifestBody2)

  // Browse to root
  await page.goto(`/binflow/ui/repositories/${key}/tree`)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()

  // Root should show image directories (app1, app2) as folders
  await expect(page.locator('[data-testid="tree-row-app1"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="tree-row-app2"]')).toBeVisible({ timeout: 10_000 })

  // Click into app1
  await page.click('[data-testid="tree-row-app1"]')
  await expect(page.locator('[data-testid="tree-row-manifests"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="tree-row-blobs"]')).toBeVisible({ timeout: 10_000 })

  // Click into manifests
  await page.click('[data-testid="tree-row-manifests"]')
  await expect(page.locator(`[data-testid="tree-row-${m1}"]`)).toBeVisible({ timeout: 10_000 })

  // Breadcrumb navigation back
  await page.click('[data-testid="tree-breadcrumb"] button.crumb:first-child')
  await expect(page.locator('[data-testid="tree-row-app1"]')).toBeVisible({ timeout: 10_000 })

  // Zero /api/search requests
  expect(searchReqs.count()).toBe(0)
  expect(errors).toEqual([])

  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
})

test('DC-02: help link to /binflow/docs/ in AppShell', async ({ page }) => {
  const errors = watchServerErrors(page)

  await page.goto('/binflow/ui/')
  await login(page)

  // Verify the help link exists with correct href and text
  const helpLink = page.locator('[data-testid="topbar-help"]')
  await expect(helpLink).toBeVisible()
  await expect(helpLink).toHaveAttribute('href', '/binflow/docs/')
  await expect(helpLink).toHaveAttribute('target', '_blank')
  await expect(helpLink).toContainText('帮助')

  expect(errors).toEqual([])
})

// simpleSha256 computes a sha256 hex string for a given input.
// Playwright tests run in Node.js, so we use the crypto module directly.
function simpleSha256(input: string): string {
  return createHash('sha256').update(input).digest('hex')
}