import { expect, test } from '@playwright/test'

// T-104 浏览器矩阵抽查面（FR-33-AC4，P2 观察）：登录 → 树浏览 → 上传
// 三链。经 playwright.matrix.config.ts 在 chromium/webkit/firefox 三引擎
// 下各跑一遍（chromium 为主矩阵对照，webkit/firefox 记录成败不阻塞）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test('chain 1: login (positive + negative inline error)', async ({ page }) => {
  await page.goto('/binflow/ui/')
  await expect(page).toHaveURL(/\/binflow\/ui\/login\?return=/)
  // 负腿：行内 401（不泄露存在性）
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', 'definitely-wrong')
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="login-error"]')).toBeVisible()
  await expect(page).toHaveURL(/\/login/)
  // 正腿：落壳
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
  await expect(page.locator('[data-testid="session-user"]')).toHaveText(ADMIN)
})

test('chain 2: tree browse (repo -> dir -> file row with size)', async ({ page, browser }) => {
  const key = `t104xb-${browser.browserType().name()}-${Date.now().toString(36)}`
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()

  await page.evaluate(
    async ({ key }) => {
      await fetch(`/binflow/api/repositories/${key}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ rclass: 'local', packageType: 'generic' }),
      })
      await fetch(`/binflow/${key}/acme/xb.bin`, { method: 'PUT', body: 't104-crossbrowser-payload' })
    },
    { key },
  )

  await page.goto(`/binflow/ui/repositories/${key}/tree`)
  await expect(page.locator('[data-testid="tree-row-acme"]')).toBeVisible({ timeout: 15_000 })
  await page.click('[data-testid="tree-row-acme"]')
  await expect(page.locator('[data-testid="tree-row-xb.bin"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="tree-row-xb.bin"] td').nth(2)).not.toHaveText('—')

  // 收尾
  await page.evaluate(async (key) => {
    await fetch(`/binflow/api/repositories/${key}?deleteContent=true`, { method: 'DELETE' })
  }, key)
})

test('chain 3: upload via dialog, row completes with checksum badge', async ({ page, browser }) => {
  const key = `t104xu-${browser.browserType().name()}-${Date.now().toString(36)}`
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()

  await page.evaluate(async (key) => {
    await fetch(`/binflow/api/repositories/${key}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ rclass: 'local', packageType: 'generic' }),
    })
  }, key)

  await page.goto(`/binflow/ui/repositories/${key}/tree`)
  await page.click('[data-testid="tree-upload"]')
  await expect(page.locator('[data-testid="upload-dialog"]')).toBeVisible()
  await page.fill('[data-testid="upload-target"]', 'up/')
  await page.setInputFiles('[data-testid="upload-file-input"]', [
    { name: 'xb-up.bin', mimeType: 'application/octet-stream', buffer: Buffer.from('t104-xb-upload') },
  ])
  await expect(page.locator('[data-testid="upload-file-0"]')).toContainText('上传完成 201', {
    timeout: 20_000,
  })
  await expect(page.locator('[data-testid="upload-verify-0"]')).toContainText('✓ checksum 一致')
  await page.click('[data-testid="upload-dialog"] .modal-actions .btn.primary')
  await expect(page.locator('[data-testid="tree-row-up"]')).toBeVisible()

  // 收尾
  await page.evaluate(async (key) => {
    await fetch(`/binflow/api/repositories/${key}?deleteContent=true`, { method: 'DELETE' })
  }, key)
})
