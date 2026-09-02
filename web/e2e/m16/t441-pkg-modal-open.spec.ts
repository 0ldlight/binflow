import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, sessionApi } from '../m8/support/seed'

// T-441（M16 批次② FE②-b，FR-143.3——B-2.6 翻正：包类型弹窗 924px + 八型
// 开禁，**形态翻转③**留痕——v1.4 的 440px 紧凑档定案按 7.161.20 活体勘误
// 翻平，parity 册 M1 行 v1.6）：
//
//   ① **modal 924px 居中**（7.161 实测 924×760 居中 el-dialog；7.84 审计锚
//      880px 勘误——reports/agents/m16-baseline-refresh.md §A3-7）：宽度
//      min(924, vw-48) + 视口居中（boundingBox 几何断言，T-382 体例）。
//   ② **tiles 维持 BinFlow 实有 13 型**（五核心 + 八门控，不伪造 Artifactory
//      41 型——型录吃 addons 注册表实时数据）+ **八型去禁用态**（门控
//      三件套退役：disabled/mono/opacity 0.4 → enabled/brand/1；档位徽章
//      pkg-tier-* 保留 = D5 可见性口径不变）。
//   ③ **纯前端门开禁、后端终裁**（AC2 的断言面）：community 实例上选定
//      门控型 → 提交 → 服务端 D3 拒绝（repo.Service ADR-0033 域）经
//      form-error 原文回显——前端不再预裁，坏请求交给 400 终裁。
//   ④ **八型建仓矩阵**（条件腿）：license 实例（八槽解锁）上逐型经控制台
//      建仓 + API 对账；community 宿主自 skip（PRD 条件票纪律：未触发
//      不构成 DoD 缺口——真实客户端 roundtrip 腿在票内 scratch 净实例
//      〔license 装载〕跑，证据 reports/agents/T-441.md）。
//   ⑤ axe 双主题：924px modal 全磁贴开禁态 serious/critical = 0。
//
// 锚源：console-ux §10（pkg-grid-* / pkg-tier-* / form-* 冻结族零改名；
// v1.34——尺寸档断言翻转③ + 门控三件套退役登记，无新锚）。

const CORE_PKG = ['generic', 'docker', 'maven', 'npm', 'pypi'] as const
const GATED_PKG = ['go', 'nuget', 'cargo', 'conan', 'helm', 'helmoci', 'rpm', 'debian'] as const

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 八门控槽解锁探测：任一槽未解锁（community 地板）= false。 */
async function gatedSlotsUnlocked(page: Page): Promise<boolean> {
  const probe = await sessionApi(page, 'GET', '/api/v1/addons')
  if (probe.status !== 200) return false
  const rows = (Array.isArray(probe.json) ? probe.json : []) as { id: string; kind: string; enabled: boolean }[]
  return GATED_PKG.every((id) => rows.some((r) => r.id === id && r.kind === 'package-type' && r.enabled))
}

// ---------------------------------------------------------------------------
// ①② modal 924px 居中 + 13 tiles + 八型开禁 -----------------------------------
// ---------------------------------------------------------------------------

test('admin: pkg-grid modal — 924px centered, 13 real tiles, gated eight enabled with tier badges', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/new')
  const grid = page.locator('[data-testid="pkg-grid"]')
  await expect(grid).toBeVisible()

  // 宽度档 + 居中（翻转③：440 紧凑档 → 924 居中档——7.161.20 实测勘误）。
  // Fade 收敛后再取 box（T-344C D7 半透明栈防假阳性同款）。
  await expect(grid).toHaveCSS('opacity', '1')
  const box = await grid.boundingBox()
  expect(box, 'pkg-grid paper has a box').toBeTruthy()
  const vw = page.viewportSize()?.width ?? 1280
  expect(box!.width).toBeCloseTo(Math.min(924, vw - 48), 0)
  expect(Math.abs(box!.x - (vw - box!.width) / 2), 'pkg-grid centered in viewport').toBeLessThanOrEqual(1)

  // 型录 = BinFlow 实有 13 型（五核心 + 八门控），不多不少——不伪造
  // Artifactory 7.161 的 41 型（Hugging Face/Terraform 族不在注册表）。
  const tiles = grid.locator('[data-testid^="pkg-grid-item-"]')
  await expect(tiles).toHaveCount(CORE_PKG.length + GATED_PKG.length)

  // 五核心：可选、地板无徽章
  for (const pt of CORE_PKG) {
    const tile = page.locator(`[data-testid="pkg-grid-item-${pt}"]`)
    await expect(tile).toBeEnabled()
    await expect(tile.locator(`[data-testid="pkg-tier-${pt}"]`)).toHaveCount(0)
  }

  // 八门控型：开禁（enabled + brand + opacity 1）+ pro 徽章保留 + desc 回归
  // 协议描述（禁用期被「需要 N 档」提示占据的行位交还）
  for (const pt of GATED_PKG) {
    const tile = page.locator(`[data-testid="pkg-grid-item-${pt}"]`)
    await expect(tile).toBeEnabled()
    await expect(tile).toHaveCSS('opacity', '1')
    await expect(tile.locator('.pkg-svg')).toHaveAttribute('data-variant', 'brand')
    await expect(tile.locator(`[data-testid="pkg-tier-${pt}"]`)).toHaveText('pro')
    await expect(tile).not.toHaveAttribute('title', /.+/) // 禁用提示 title 退役
  }
})

// ---------------------------------------------------------------------------
// ③ 纯前端门开禁、后端终裁：community 拒绝链 / licensed 成链（双态分支） ------
// ---------------------------------------------------------------------------

test('admin: frontend gate opened, backend verdict final — go tile submits through to the server decision', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const licensed = await gatedSlotsUnlocked(page)
  const key = uniq(licensed ? 't441go' : 't441rej')

  await page.goto('/binflow/ui/admin/repositories/new')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  // 门控磁贴可点（翻转③ 的手势面：community 也不再被前端拦在磁贴上）
  await page.click('[data-testid="pkg-grid-item-go"]')
  await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="form-package-go"]')).toBeChecked()

  await page.fill('[data-testid="form-key"]', key)
  await page.fill('[data-testid="form-description"]', 't441 gate-open probe')
  await page.click('[data-testid="form-submit"]')

  if (!licensed) {
    // community：服务端 D3 拒绝（ADR-0033 域）——form-error 原文回显，
    // 停留表单页（未跳详情）。前端门开禁后这条链是槽位纪律的唯一守门。
    await expect(page.locator('[data-testid="form-error"]')).toBeVisible()
    await expect(page.locator('[data-testid="form-error"]')).toContainText('package type not available')
    await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/new$/)
  } else {
    // licensed：八槽解锁——建仓成链（toast + 详情落点 + API 对账）
    await expect(page.locator('[data-testid="toast"]')).toContainText(`Successfully created repository '${key}'`)
    await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${key}$`))
    const got = await sessionApi(page, 'GET', `/api/repositories/${key}`)
    expect(got.status).toBe(200)
    expect((got.json as { packageType: string }).packageType).toBe('go')
    await m8Client().request('DELETE', `/binflow/api/repositories/${key}`)
  }
})

// ---------------------------------------------------------------------------
// ④ 八型建仓矩阵（条件腿：license 宿主全跑，community 自 skip） ----------------
// ---------------------------------------------------------------------------

test('admin (licensed): gated-eight console-create matrix — one repo per type via the opened tiles', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  // 八型条件腿：community 无 license 时后端建仓门必拒（D3——本票不越
  // ADR-0033 域开洞）；净实例装载 license 后此腿全跑（票内 scratch 实例
  // 即此形态，真实客户端 roundtrip 证据另附报告——spec 面钉控制台建仓链）
  if (!(await gatedSlotsUnlocked(page))) {
    test.skip(true, 'gated package-type slots not unlocked on this instance (community floor) — run against a licensed scratch instance for the full matrix')
  }

  for (const pt of GATED_PKG) {
    const key = uniq(`t441${pt}`)
    await page.goto('/binflow/ui/admin/repositories/new')
    await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
    await page.click(`[data-testid="pkg-grid-item-${pt}"]`)
    await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCount(0)
    await expect(page.locator(`[data-testid="form-package-${pt}"]`)).toBeChecked()

    await page.fill('[data-testid="form-key"]', key)
    await page.fill('[data-testid="form-description"]', `t441 console-create matrix ${pt}`)
    await page.click('[data-testid="form-submit"]')

    await expect(page.locator('[data-testid="toast"]')).toContainText(`Successfully created repository '${key}'`)
    await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${key}$`))

    // API 对账（UI 说的话让 API 复核——rclass/packageType 落库事实）
    const got = await sessionApi(page, 'GET', `/api/repositories/${key}`)
    expect(got.status, `${pt} repo created via console`).toBe(200)
    const body = got.json as { rclass: string; packageType: string }
    expect(body.rclass).toBe('local')
    expect(body.packageType).toBe(pt)

    // 收尾（空仓直接删——自备夹具纪律）
    expect((await m8Client().request('DELETE', `/binflow/api/repositories/${key}`)).status).toBeLessThan(300)
  }
})

// ---------------------------------------------------------------------------
// ⑤ axe 双主题：924px modal 全开禁态 ------------------------------------------
// ---------------------------------------------------------------------------

test('axe: 924px opened pkg-grid modal scans clean in both themes', async ({ page }, testInfo) => {
  test.setTimeout(300_000)
  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/repositories/new')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
    await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCSS('opacity', '1')
    await expectA11yClean(page, testInfo, { include: '[data-testid="pkg-grid"]' })
  }
})
