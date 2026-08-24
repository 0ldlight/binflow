import { createHash } from 'node:crypto'
import { expect, test } from '@playwright/test'

// T-100 探针（真后端）：制品树全链——上传（W13）→ 树浏览/面包屑/建目录
// （W12/W12b）→ 下载 sha 对账（W13）→ 删除与幂等（E-14）→ read-only
// 403 原因呈现（W12d）→ maven 表单生成路径 + 前端预检零写请求 →
// 治理 409/413 上传语义原样呈现（W12a UI 面）→ 搜索全链（W14b）→
// 大目录客户端分页（§6.5 兜底）。
//
// 运行前提：已 `make console && make build` 的真二进制在前台 serve，
// BASE 指向它（同 T-98/T-99 探针约定）；ADMIN_PW 默认 password（PRD §4）。
// 对账腿走 page.evaluate fetch（同源携带 session cookie——内容面与管理面
// 同凭据，ux R10 / CE-03）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: import('@playwright/test').Page, user = ADMIN, pw = ADMIN_PW) {
  await page.fill('[data-testid="login-username"]', user)
  await page.fill('[data-testid="login-password"]', pw)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

async function logoutViaUI(page: import('@playwright/test').Page) {
  await page.click('[data-testid="session-toggle"]')
  await page.click('[data-testid="logout-button"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page).toHaveURL(/\/login/)
}

/** 同源 fetch（携带 session cookie）；返回 {status, text} */
async function api(
  page: import('@playwright/test').Page,
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

test('W12/W12b/W13 generic tree: upload -> browse -> detail/download sha match -> mkdir -> breadcrumb -> delete idempotent', async ({ page }) => {
  const key = uniq('t100a')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })

  // 空树（W12 空态）
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-empty-dir"]')).toContainText('此目录为空')

  // 上传（W13）：目标 acme/，双文件，行级完成态 + checksum 比对徽标
  // （T-244 双 Deploy 入口收敛：tree-upload/UploadDialog 退役，统一走页头
  //   tree-deploy → DeployDialog——显式「部署」提交步 +1 次点击）
  const payloadA = 't100-probe-A'
  const payloadB = '{"v":1,"src":"t100"}'
  await page.click('[data-testid="tree-deploy"]')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toBeVisible()
  await page.fill('[data-testid="deploy-target"]', 'acme/')
  await page.setInputFiles('[data-testid="deploy-file-input"]', [
    { name: 'app.bin', mimeType: 'application/octet-stream', buffer: Buffer.from(payloadA) },
    { name: 'sbom.json', mimeType: 'application/json', buffer: Buffer.from(payloadB) },
  ])
  await page.click('[data-testid="deploy-submit"]')
  await expect(page.locator('[data-testid="deploy-row-app.bin"]')).toContainText('上传完成 201', { timeout: 15_000 })
  await expect(page.locator('[data-testid="deploy-row-app.bin"]')).toContainText('✓ checksum 一致')
  await expect(page.locator('[data-testid="deploy-row-sbom.json"]')).toContainText('✓ checksum 一致')
  await page.click('[data-testid="deploy-close"]')

  // 根层：acme 目录行出现（服务端 mkdir-on-put 语义），左树同步
  await expect(page.locator('[data-testid="tree-row-acme"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-node-acme"]')).toBeVisible()

  // 进目录（W12b）：children 表 + ?list 合并的 size 列
  await page.click('[data-testid="tree-row-acme"]')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${key}/acme$`))
  await expect(page.locator('[data-testid="tree-row-app.bin"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-app.bin"] td').nth(2)).toHaveText('12 B')
  await expect(page.locator('[data-testid="tree-row-sbom.json"]')).toBeVisible()

  // 详情面板：checksums + 服务端 item info 对账（P2 拷贝面）
  await page.click('[data-testid="tree-row-app.bin"]')
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('sha256')
  const item = await api(page, 'GET', `/api/storage/${key}/acme/app.bin`)
  expect(item.status).toBe(200)
  const itemJson = JSON.parse(item.text)
  expect(itemJson.checksums.sha256).toBe(createHash('sha256').update(payloadA).digest('hex'))
  expect(itemJson.createdBy).toBe(ADMIN)

  // 下载 sha 对账（W13）：本地流式哈希 vs item info checksums.sha256
  // （有效权限自 T-236 起是详情面板的第二个 Tab——console-m8 §3.3 C4；
  //   锚不变，操作流多一步 Tab 切换。verify 块在常规 Tab——断言完权限 Tab
  //   后显式切回；此前该腿隐性依赖「后台数据到达把 Tab 弹回常规」的缺陷，
  //   T-244 修复 target identity 重置后按显式切换书写）
  await page.click('[data-testid="node-tab-perms"]')
  await expect(page.locator('[data-testid="node-perms"]')).toBeVisible() // admin 面（?permissions）
  await page.click('[data-testid="node-tab-general"]')
  await page.click('[data-testid="node-download"]')
  await expect(page.locator('[data-testid="node-download-verify"]')).toContainText('✓ 下载落盘 sha256 与服务端一致', {
    timeout: 15_000,
  })

  // 建目录（E-15 尾斜杠 mkdir）——toast 断言按内容过滤（下载成功的
  // toast 仍在堆叠中）
  await page.click('[data-testid="tree-mkdir"]')
  await page.fill('[data-testid="tree-mkdir-input"]', 'docs')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('.toast').filter({ hasText: '已创建目录' })).toContainText('已创建目录 acme/docs/')
  await expect(page.locator('[data-testid="tree-row-docs"]')).toBeVisible()

  // 面包屑回根
  await page.click('[data-testid="tree-breadcrumb"] .crumb')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${key}$`))
  await expect(page.locator('[data-testid="tree-row-acme"]')).toBeVisible()

  // 删除（E-14）：文件 → 行消失 + 内容面 404；重复删除 = 404（幂等语义源）
  await page.click('[data-testid="tree-row-acme"]')
  await page.click('[data-testid="tree-row-app.bin"] [data-testid="delete-node-button"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('.toast').filter({ hasText: '已删除 acme/app.bin' })).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-app.bin"]')).toHaveCount(0)
  expect((await api(page, 'GET', `/${key}/acme/app.bin`)).status).toBe(404)
  expect((await api(page, 'DELETE', `/${key}/acme/app.bin`)).status).toBe(404)

  // 删除目录（递归）
  await page.click('[data-testid="tree-row-docs"] [data-testid="delete-node-button"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="tree-row-docs"]')).toHaveCount(0)
})

test('W12d read-only user: browse allowed, delete 403 reason inline with guidance (admin curl leg 204)', async ({ page }) => {
  const key = uniq('t100b')
  const roUser = uniq('ro')
  const roPW = 'ro-password-1'
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/d/keep.bin`, 'keep')
  await api(page, 'PUT', `/${key}/d/gone.bin`, 'gone')
  // 「curl 侧仍 200/204」腿：同一删除动作对有权限主体（admin）成功
  expect((await api(page, 'DELETE', `/${key}/d/gone.bin`)).status).toBe(204)

  // read-only 主体：用户 + 仅 read 的 permission target（email 必填——
  // 用户管理层校验「valid user email」）
  await api(page, 'PUT', `/api/security/users/${roUser}`, {
    name: roUser,
    email: `${roUser}@example.com`,
    password: roPW,
    admin: false,
    groups: [],
  })
  const target = await api(page, 'POST', `/api/v1/permissions`, {
    name: `ro-${key}`,
    repos: [key],
    includePatterns: ['**'],
    excludePatterns: [],
    principals: { users: { [roUser]: ['read'] } },
  })
  expect(target.status).toBeLessThan(300)

  await logoutViaUI(page)
  await login(page, roUser, roPW)

  // 浏览 OK（内容面按路径 ACL）；仓库元数据为 admin 面 → 降级提示而非空白
  await page.goto(`/binflow/ui/artifacts/${key}/d`)
  await expect(page.locator('[data-testid="tree-row-keep.bin"]')).toBeVisible()
  await expect(page.locator('.tree-page .warn-box')).toContainText('管理员视图')

  // 删除被拒：403 原因行内呈现 + 权限指引（W12d）
  await page.click('[data-testid="tree-row-keep.bin"] [data-testid="delete-node-button"]')
  await page.click('[data-testid="confirm-accept"]')
  const err = page.locator('[data-testid="delete-error"]')
  await expect(err).toBeVisible()
  await expect(err).toContainText('HTTP 403')
  await expect(err).toContainText('permission denied')
  await expect(err).toContainText('delete 权限')
  await expect(page.locator('[data-testid="tree-row-keep.bin"]')).toBeVisible()
})

test('W12a on upload UI: 409 pattern and 413 quota messages surface verbatim', async ({ page }) => {
  const patKey = uniq('t100c')
  const quoKey = uniq('t100d')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${patKey}`, {
    rclass: 'local',
    packageType: 'generic',
    excludesPattern: 'tmp/**',
  })
  await api(page, 'PUT', `/api/repositories/${quoKey}`, {
    rclass: 'local',
    packageType: 'generic',
    quotaBytes: 8,
  })

  // 409：excludesPattern 拒绝（message 含两侧 pattern，原样呈现）
  await page.goto(`/binflow/ui/artifacts/${patKey}`)
  await page.click('[data-testid="tree-deploy"]')
  await page.fill('[data-testid="deploy-target"]', 'tmp/')
  await page.setInputFiles('[data-testid="deploy-file-input"]', [
    { name: 'x.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(16, 1) },
  ])
  await page.click('[data-testid="deploy-submit"]')
  const row409 = page.locator('[data-testid="deploy-row-x.bin"]')
  await expect(row409).toContainText('HTTP 409', { timeout: 15_000 })
  await expect(row409).toContainText('excludesPattern')
  await expect(row409).toContainText('tmp/**')

  // 413：quota 超限（message 含 used/quota 双值）
  await page.goto(`/binflow/ui/artifacts/${quoKey}`)
  await page.click('[data-testid="tree-deploy"]')
  await page.setInputFiles('[data-testid="deploy-file-input"]', [
    { name: 'big.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(64, 7) },
  ])
  await page.click('[data-testid="deploy-submit"]')
  const row413 = page.locator('[data-testid="deploy-row-big.bin"]')
  await expect(row413).toContainText('HTTP 413', { timeout: 15_000 })
  await expect(row413).toContainText('quota exceeded')
  await expect(row413).toContainText('配额已满')
})

test('maven upload form: GAV generates layout path, precheck blocks bad input with zero write requests', async ({ page }) => {
  const key = uniq('t100e')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'maven' })

  await page.goto(`/binflow/ui/artifacts/${key}`)
  await page.click('[data-testid="tree-deploy"]')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toBeVisible()

  const writes: string[] = []
  page.on('request', (r) => {
    if (new RegExp(`/binflow/${key}/`).test(r.url()) && r.method() === 'PUT') {
      writes.push(`${r.method()} ${r.url()}`)
    }
  })

  // 预检：version 空 → 前端拦（不送服务端吃 400；DeployDialog 对未过 GAV
  // 预检的选择零入队——行区不渲染）
  await page.fill('[data-testid="deploy-gav-groupId"]', 'com.acme')
  await page.fill('[data-testid="deploy-gav-artifactId"]', 'demo-app')
  await expect(page.locator('[data-testid="deploy-maven-preview"]')).toContainText('version 不能为空')
  await page.setInputFiles('[data-testid="deploy-file-input"]', [
    { name: 'whatever.jar', mimeType: 'application/java-archive', buffer: Buffer.alloc(24, 3) },
  ])
  await page.waitForTimeout(400)
  expect(writes).toHaveLength(0)
  await expect(page.locator('[data-testid="deploy-rows"]')).toHaveCount(0)

  // 合法 GAV：路径生成 + 文件改名 → 201 → 树上按 layout 路径可见
  await page.fill('[data-testid="deploy-gav-version"]', '1.0.0')
  await expect(page.locator('[data-testid="deploy-maven-preview"]')).toContainText('com/acme/demo-app/1.0.0/')
  await expect(page.locator('[data-testid="deploy-maven-preview"]')).toContainText('demo-app-1.0.0.jar')
  await page.setInputFiles('[data-testid="deploy-file-input"]', [
    { name: 'whatever.jar', mimeType: 'application/java-archive', buffer: Buffer.alloc(24, 3) },
  ])
  await page.click('[data-testid="deploy-submit"]')
  await expect(page.locator('[data-testid="deploy-row-demo-app-1.0.0.jar"]')).toContainText('上传完成 201', { timeout: 15_000 })
  await page.click('[data-testid="deploy-close"]')
  await expect(page.locator('[data-testid="tree-row-com"]')).toBeVisible()
  await page.click('[data-testid="tree-row-com"]')
  await page.click('[data-testid="tree-row-acme"]')
  await page.click('[data-testid="tree-row-demo-app"]')
  await page.click('[data-testid="tree-row-1.0.0"]')
  await expect(page.locator('[data-testid="tree-row-demo-app-1.0.0.jar"]')).toBeVisible()
  const item = await api(page, 'GET', `/api/storage/${key}/com/acme/demo-app/1.0.0/demo-app-1.0.0.jar`)
  expect(item.status).toBe(200)
})

test('W14b search: empty-keyword guide, debounced results, semantic subline, row click locates tree node', async ({ page }) => {
  const key = uniq('t100f')
  const marker = uniq('libcore')
  const fileName = `${marker}.bin`
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/acme/${fileName}`, 'search-probe')

  await page.goto('/binflow/ui/search')
  // 空关键词引导态：不发起查询
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('输入关键词开始搜索')

  // 防抖后出结果（repo + path + size）
  await page.fill('[data-testid="search-input"]', marker)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(key)
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`/acme/${fileName}`)

  // 仓库过滤收窄：过滤到不存在的 repo → 无结果空态
  await page.fill('[data-testid="search-filter-repo"]', 'no-such-repo')
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('没有匹配', { timeout: 10_000 })
  await page.fill('[data-testid="search-filter-repo"]', key)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })

  // 行点击跳树定位（?focus= 自动选中 + 详情面板）
  await page.click('[data-testid="search-result-0"]')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${key}/acme\\?focus=${fileName}`))
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"]')).toContainText(`acme/${fileName}`)
})

test('upload dialog close stops the queue: remaining files never PUT (review B1)', async ({ page }) => {
  const key = uniq('t100h')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, {
    rclass: 'local',
    packageType: 'generic',
    quotaBytes: 1048576, // 配额仓：泄漏的排队上传会真实烧配额
  })

  // 第一条 PUT 在 route 处挂起——保证「关闭」发生在 1 行在飞 + 2 行排队
  let puts = 0
  let releaseFirst: () => void = () => {}
  await page.route(`**/binflow/${key}/**`, async (route) => {
    if (route.request().method() === 'PUT') {
      puts++
      if (puts === 1) {
        await new Promise<void>((resolve) => {
          releaseFirst = resolve
        })
      }
    }
    // 行 0 被abort 后 continue 对已取消请求是 no-op/拒绝——都不影响计数断言
    await route.continue().catch(() => {})
  })

  await page.goto(`/binflow/ui/artifacts/${key}`)
  await page.click('[data-testid="tree-deploy"]')
  await page.fill('[data-testid="deploy-target"]', 'q/')
  await page.setInputFiles('[data-testid="deploy-file-input"]', [
    { name: 'f0.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(8, 1) },
    { name: 'f1.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(8, 2) },
    { name: 'f2.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(8, 3) },
  ])
  await page.click('[data-testid="deploy-submit"]')
  // 行 0 进入上传中（其 PUT 停在 route 闸上）后关闭对话框
  await expect(page.locator('[data-testid="deploy-row-f0.bin"]')).toContainText('上传中', { timeout: 15_000 })
  await page.click('[data-testid="deploy-close"]') // 关闭（落闸 + abort）
  await expect(page.locator('[data-testid="deploy-dialog"]')).toHaveCount(0)
  releaseFirst()

  // 断言：关闭后不再有新 PUT（唯一一条是行 0），排队两文件从未落库
  await page.waitForTimeout(1200)
  expect(puts).toBe(1)
  expect((await api(page, 'GET', `/${key}/q/f1.bin`)).status).toBe(404)
  expect((await api(page, 'GET', `/${key}/q/f2.bin`)).status).toBe(404)
  // 配额面复核：usage 仅可能含行 0 的 8 字节（abort 与 route 放行的竞态下
  // 行 0 可达可不到），绝无 24 字节（三条全落）形态
  const usage = await api(page, 'GET', `/api/v1/storage/usage/${key}`)
  expect(usage.status).toBe(200)
  const used = JSON.parse(usage.text).usedBytes as number
  expect(used).toBeLessThanOrEqual(8)
})

test('large directory: client-side load-more pagination (ux R1 fallback)', async ({ page }) => {
  test.setTimeout(120_000)
  const key = uniq('t100g')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })

  // 220 个节点（>2 页、<2000 提示线）：批量 API 铺数
  await page.evaluate(
    async ({ key, n }) => {
      const batch = 20
      for (let off = 0; off < n; off += batch) {
        await Promise.all(
          Array.from({ length: Math.min(batch, n - off) }, (_, i) =>
            fetch(`/binflow/${key}/f${String(off + i).padStart(4, '0')}.bin`, {
              method: 'PUT',
              body: `payload-${off + i}`,
            }),
          ),
        )
      }
    },
    { key, n: 220 },
  )

  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="tree-list"] tbody tr')).toHaveCount(100, { timeout: 20_000 })
  // 过滤只作用于已加载集（§6.3）
  await page.fill('[data-testid="tree-filter"]', 'f0001')
  await expect(page.locator('[data-testid="tree-list"] tbody tr')).toHaveCount(1)
  await page.fill('[data-testid="tree-filter"]', '')
  await expect(page.locator('[data-testid="tree-list"] tbody tr')).toHaveCount(100)
  // 加载更多：增量追加（骨架/已有行不重绘语义）
  await page.click('[data-testid="tree-load-more"]')
  await expect(page.locator('[data-testid="tree-list"] tbody tr')).toHaveCount(200, { timeout: 20_000 })
  await page.click('[data-testid="tree-load-more"]')
  await expect(page.locator('[data-testid="tree-list"] tbody tr')).toHaveCount(220, { timeout: 20_000 })
  await expect(page.locator('[data-testid="tree-load-more"]')).toHaveCount(0)
})
