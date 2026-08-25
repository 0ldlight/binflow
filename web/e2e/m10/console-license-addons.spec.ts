import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'

// T-288 fill (FR-84 FE 腿 + FR-86-AC5, L27): the console License & Add-ons
// page and the repo-create dialog's package-type tier badges.
//
// Posture assumption: the harness instance is the DEFAULT community form
// (no license installed — README §1 "起被测实例（无 license = community 形态）";
// the pro/enterprise/expired/disabled forms belong to the tier-matrix script,
// not the browser suite). On this form:
//   GET /api/v1/addons = 11 slots (T-282 manifest): five core + properties
//   unlocked, go/nuget/cargo locked (pro), ha/xray-integration locked
//   (enterprise); the stock verify key's private half was destroyed at
//   bootstrap (ADR-0032), so ANY posted document answers 400
//   LICENSE_INVALID — the install-error leg needs no key material.
//
// Assertion regime (m10 README §2.4 over ADR-0029 decision 3): anchors from
// console-ux §10 (T-288 batch, v1.10); tier badges assert the CLOSED-SET
// wire value (community|pro|enterprise), never marketing copy; gated entries
// are VISIBLE with their badge (D5) and DISABLED with a needed-tier hint —
// no visual/pixel assertions; readonly = disabled + counter-assertions.

const CORE_PKG = ['generic', 'docker', 'maven', 'npm', 'pypi'] as const
const PRO_PKG = ['go', 'nuget', 'cargo'] as const
const ENT_FEATURES = ['ha', 'xray-integration'] as const

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test('L27a: admin — nav entry, community floor card, live addons matrix', async ({ page }) => {
  await loginAs(page, 'admin')

  // 导航可见（「常规」分组第二页）
  await page.goto('/binflow/ui/admin/general/license')
  await expect(page.locator('[data-testid="license-page"]')).toBeVisible()
  await expect(page.locator('.nav-group-label', { hasText: '常规' })).toBeVisible()
  const navEntry = page.locator('[data-testid="app-nav"] .nav-item', { hasText: 'License & Add-ons' })
  await expect(navEntry).toBeVisible()
  await expect(navEntry).toHaveAttribute('href', '/binflow/ui/admin/general/license')

  // License 状态卡：community 地板（档位徽章 = 闭集 wire 值）
  await expect(page.locator('[data-testid="license-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="license-tier"]')).toHaveText('community')
  await expect(page.locator('[data-testid="license-floor"]')).toBeVisible()
  // 未授权：无 licensee/倒计时行、无卸载钮；装载面（admin）在
  await expect(page.locator('[data-testid="license-licensee"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="license-uninstall"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="license-doc-input"]')).toBeVisible()
  await expect(page.locator('[data-testid="license-install"]')).toBeVisible()
  // 空文档装载钮禁用（表单零坏请求）
  await expect(page.locator('[data-testid="license-install"]')).toBeDisabled()

  // 矩阵：装配序全槽位（AC1 ≥10；T-282 manifest = 11）
  await expect(page.locator('[data-testid="addons-card"]')).toBeVisible()
  const rows = page.locator('[data-testid="addons-table"] tbody tr')
  await expect(rows).toHaveCount(11)
  for (const id of [...CORE_PKG, ...PRO_PKG, ...ENT_FEATURES, 'properties']) {
    await expect(page.locator(`[data-testid="addons-row-${id}"]`)).toBeVisible()
  }
  // 五核心 + properties：地板无档位徽章（「—」），状态已解锁
  for (const id of [...CORE_PKG, 'properties']) {
    await expect(page.locator(`[data-testid="addons-tier-${id}"]`)).toHaveText('—')
    await expect(page.locator(`[data-testid="addons-state-${id}"]`)).toContainText('已解锁')
  }
  // 门控包型：pro 徽章 + 锁定态（需要 pro）；enterprise 功能槽同构
  for (const id of PRO_PKG) {
    await expect(page.locator(`[data-testid="addons-tier-${id}"]`)).toHaveText('pro')
    await expect(page.locator(`[data-testid="addons-state-${id}"]`)).toContainText('需要 pro')
  }
  for (const id of ENT_FEATURES) {
    await expect(page.locator(`[data-testid="addons-tier-${id}"]`)).toHaveText('enterprise')
    await expect(page.locator(`[data-testid="addons-state-${id}"]`)).toContainText('需要 enterprise')
  }
  // Kind 徽章 = wire 值（package-type | feature）
  await expect(page.locator('[data-testid="addons-row-go"] .badge', { hasText: 'package-type' })).toBeVisible()
  await expect(page.locator('[data-testid="addons-row-properties"] .badge', { hasText: 'feature' })).toBeVisible()
})

test('L27b: install refused — the 400 wire verdict renders verbatim, floor intact', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/general/license')
  await expect(page.locator('[data-testid="license-doc-input"]')).toBeVisible()

  // 任意文档在 stock 公钥下必拒（D7）——错误体两 wire 码之一，原文呈现
  await page.fill('[data-testid="license-doc-input"]', 'not-a-license-document')
  await page.click('[data-testid="license-install"]')
  const err = page.locator('[data-testid="license-install-error"]')
  await expect(err).toBeVisible()
  await expect(err).toContainText('LICENSE_INVALID')
  await expect(err).toContainText('HTTP 400')

  // 拒装不动在装 license（community 地板维持）
  await expect(page.locator('[data-testid="license-tier"]')).toHaveText('community')
  await expect(page.locator('[data-testid="license-uninstall"]')).toHaveCount(0)
})

test('L27c: readonly_admin read-only visible; plain user navigation unreachable', async ({ browser }) => {
  // —— readonly_admin：页面可见、矩阵只读、无写入口（AC5 只读可见腿）——
  const ro = await (await browser.newContext()).newPage()
  await loginAs(ro, 'readonly_admin')
  await ro.goto('/binflow/ui/admin/general/license')
  await expect(ro.locator('[data-testid="license-page"]')).toBeVisible()
  await expect(ro.locator('[data-testid="app-nav"] .nav-item', { hasText: 'License & Add-ons' })).toBeVisible()
  await expect(ro.locator('[data-testid="license-readonly-note"]')).toBeVisible()
  // 写面反断言（L4 预收敛；服务端 403 兜底）
  await expect(ro.locator('[data-testid="license-doc-input"]')).toHaveCount(0)
  await expect(ro.locator('[data-testid="license-install"]')).toHaveCount(0)
  await expect(ro.locator('[data-testid="license-uninstall"]')).toHaveCount(0)
  // 读面全量：状态卡 + 矩阵
  await expect(ro.locator('[data-testid="license-tier"]')).toHaveText('community')
  await expect(ro.locator('[data-testid="addons-table"]')).toBeVisible()
  await expect(ro.locator('[data-testid="addons-state-go"]')).toContainText('需要 pro')

  // —— 普通 user：导航不可达 + 直链 L2 收敛 ——
  const user = await (await browser.newContext()).newPage()
  await loginAs(user, 'user')
  await expect(user.locator('[data-testid="app-nav"] .nav-item', { hasText: 'License & Add-ons' })).toHaveCount(0)
  await user.goto('/binflow/ui/admin/general/license')
  await expect(user.locator('[data-testid="license-page"]')).toBeVisible()
  await expect(user.locator('[data-testid="license-page"] [data-testid="empty-state"]')).toBeVisible()
  await expect(user.locator('[data-testid="addons-table"]')).toHaveCount(0)
})

test('L27d: repo-create dialog — core badgeless floor, gated pro-badged + disabled', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/new')
  const grid = page.locator('[data-testid="pkg-grid"]')
  await expect(grid).toBeVisible()

  // 五核心：在、无档位徽章（地板）、可选
  for (const id of CORE_PKG) {
    await expect(page.locator(`[data-testid="pkg-grid-item-${id}"]`)).toBeVisible()
    await expect(page.locator(`[data-testid="pkg-grid-item-${id}"] [data-testid="pkg-tier-${id}"]`)).toHaveCount(0)
  }
  // docker 仅 local 的组合约束维持（既有行为零回归）
  await expect(page.locator('[data-testid="pkg-grid-item-docker"]')).toBeEnabled()

  // 门控型：可见带 pro 徽章（D5）、禁用、提示需要 pro
  for (const id of PRO_PKG) {
    const item = page.locator(`[data-testid="pkg-grid-item-${id}"]`)
    await expect(item).toBeVisible()
    await expect(item).toBeDisabled()
    await expect(page.locator(`[data-testid="pkg-grid-item-${id}"] [data-testid="pkg-tier-${id}"]`)).toHaveText('pro')
    await expect(item).toHaveAttribute('title', /需要 pro/)
  }

  // 选定地板型进表单：radio 行同族徽章 + 门控型禁用（表单面零坏请求）
  await page.locator('[data-testid="pkg-grid-item-generic"]').click()
  await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="form-package-maven"]')).toBeEnabled()
  for (const id of PRO_PKG) {
    await expect(page.locator(`[data-testid="form-package-${id}"]`)).toBeDisabled()
    await expect(page.locator(`label:has([data-testid="form-package-${id}"]) [data-testid="pkg-tier-${id}"]`)).toHaveText('pro')
  }
})

test('axe: license page + repo dialog with tier badges scan clean at serious/critical', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/general/license')
  await expect(page.locator('[data-testid="addons-table"]')).toBeVisible()
  await expectA11yClean(page, testInfo)

  // 建仓对话框（含门控禁用项与徽章）单独扫 modal 面
  await page.goto('/binflow/ui/admin/repositories/new')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="pkg-grid"]' })
})
