import { execSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { expect, test } from '@playwright/test'

// T-104 QA 注入断言：T-100/T-101 review 遗留的三条补充验证。
//
// ① 上传 403 腿（T-100 review NB③）：W12d E2E 只盖 delete-403；AC② 的
//    「403 权限指引」上传腿（UploadDialog UploadError）未触达——ro 用户
//    在树页上传 → HTTP 403 + 行内指引（非 admin 无权限页链接）。
// ② B1 残余臂（T-100 复核遗留，修复 77718cc）：关闭落在当前行 hashing 相
//    （纯本地 sha256，无 XHR 可 abort）→ 关闭后零新 PUT（route 计数断言）。
//    既有第 7 例只盖 uploading 相（route 挂起首条 PUT）。
// ③ sameSnapshot 双 pattern 重排（T-101 review 测试判别力缺口）：纯 Node
//    断言——基线 [a,b] → 逆序加回 [b,a] 集合等价（旧 join 实现会判 dirty）。

import { sameSnapshot } from '../src/pages/security/targetdiff'
import type { TargetSnapshot } from '../src/pages/security/targetdiff'

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

async function logoutViaUI(page: Page) {
  await page.click('[data-testid="session-toggle"]')
  await page.click('[data-testid="logout-button"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page).toHaveURL(/\/login/)
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

test('upload 403 (write denied) renders inline guidance for read-only user (T-100 NB③)', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t104up403')
  const roUser = uniq('ro')
  const roPW = 'ro-upload-pw-1'
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/d/keep.bin`, 'keep')
  expect(
    (
      await api(page, 'PUT', `/api/security/users/${roUser}`, {
        name: roUser,
        email: `${roUser}@example.com`,
        password: roPW,
        admin: false,
        groups: [],
      })
    ).status,
  ).toBeLessThan(300)
  expect(
    (
      await api(page, 'POST', '/api/v1/permissions', {
        name: `ro-${key}`,
        repos: [key],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: { [roUser]: ['read'] } },
      })
    ).status,
  ).toBeLessThan(300)

  await logoutViaUI(page)
  await login(page, roUser, roPW)

  // 树页上传（ro 有 read → 树可见；write 拒 → 403 行内指引）
  await page.goto(`/binflow/ui/repositories/${key}/tree`)
  await page.click('[data-testid="tree-deploy"]')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toBeVisible()
  await page.fill('[data-testid="deploy-target"]', 'denied/')
  await page.setInputFiles('[data-testid="deploy-file-input"]', [
    { name: 'x.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(16, 1) },
  ])
  await page.click('[data-testid="deploy-submit"]')
  const row = page.locator('[data-testid="deploy-row-x.bin"]')
  await expect(row).toContainText('HTTP 403', { timeout: 15_000 })
  await expect(row).toContainText('permission denied')
  // 403 指引行内呈现（AC②）：写入需 write 的说明；非 admin 不给权限页链接
  await expect(row).toContainText('当前会话对该路径没有所需权限')
  await expect(row).toContainText('write')
  await expect(page.locator('[data-testid="deploy-dialog"] a')).toHaveCount(0)

  // 被拒路径零残留 + 既有节点不受影响（curl 侧等价腿）
  expect((await api(page, 'GET', `/${key}/denied/x.bin`)).status).toBe(404)
  expect((await api(page, 'GET', `/${key}/d/keep.bin`)).status).toBe(200)
  expect(errors).toEqual([])
})

test('B1 residual arm: close during hashing phase emits zero PUT (fix 77718cc)', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t104hash')
  await page.goto('/binflow/ui/')
  await login(page)
  await api(page, 'PUT', `/api/repositories/${key}`, {
    rclass: 'local',
    packageType: 'generic',
    quotaBytes: 2_147_483_648, // 泄漏即真实落库（计数 + usage 双断言源）
  })

  let puts = 0
  await page.route(`**/binflow/${key}/**`, async (route) => {
    if (route.request().method() === 'PUT') puts++
    await route.continue().catch(() => {})
  })

  // 128MB：纯 JS sha256（30~80MB/s）哈希相 ≥ 1.5s——足够在哈希中点关闭。
  // Playwright buffer 上限 50MB——落盘走路径形态（basename = 行文件名）
  const TMP = process.env.QA_TMPDIR ?? '/tmp/t104'
  const bigPath = `${TMP}/hash128m.bin`
  if (!existsSync(bigPath)) {
    execSync(`mkdir -p ${TMP} && dd if=/dev/urandom of=${bigPath} bs=1048576 count=128 2>/dev/null`)
  }
  await page.goto(`/binflow/ui/repositories/${key}/tree`)
  await page.click('[data-testid="tree-deploy"]')
  await page.fill('[data-testid="deploy-target"]', 'big/')
  await page.setInputFiles('[data-testid="deploy-file-input"]', bigPath)
  await page.click('[data-testid="deploy-submit"]')
  // 行 0 处于哈希相（此相无 XHR，route 闸挂不住——正是残余臂的相）
  await expect(page.locator('[data-testid="deploy-row-hash128m.bin"]')).toContainText('哈希中', { timeout: 15_000 })
  await page.click('[data-testid="deploy-close"]') // 关闭
  await expect(page.locator('[data-testid="deploy-dialog"]')).toHaveCount(0)

  // 哈希最坏 ~4.3s（128MB @ 30MB/s）——等过全程再断言零 PUT
  await page.waitForTimeout(9_000)
  expect(puts).toBe(0)
  expect((await api(page, 'GET', `/${key}/big/hash128m.bin`)).status).toBe(404)
  const usage = JSON.parse((await api(page, 'GET', `/api/v1/storage/usage/${key}`)).text)
  expect(usage.usedBytes).toBe(0)
  expect(errors).toEqual([])
})

test('sameSnapshot: swapped-order double includes are equal (T-101 review leftover)', () => {
  const base: TargetSnapshot = {
    repos: ['r1'],
    includes: ['a/**', 'b/**'],
    excludes: [],
    users: {},
    groups: {},
  }
  // 双 pattern 逆序加回：旧 join 比较实现判 dirty（false）——集合语义必须 true
  expect(sameSnapshot(base, { ...base, includes: ['b/**', 'a/**'] })).toBe(true)
  // 三元素轮换（更强的顺序无关证据）
  const base3 = { ...base, includes: ['a/**', 'b/**', 'c/**'] }
  expect(sameSnapshot(base3, { ...base, includes: ['c/**', 'b/**', 'a/**'] })).toBe(true)
  // 判别力对照：真变更仍需判不等（防恒 true 的假实现）
  expect(sameSnapshot(base, { ...base, includes: ['a/**'] })).toBe(false)
  expect(sameSnapshot(base, { ...base, includes: ['a/**', 'b/**', 'x/**'] })).toBe(false)
})
