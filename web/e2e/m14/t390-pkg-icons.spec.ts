import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, seedRepos } from '../m8/support/seed'

// T-390（M14 B5 FE，FR-127）——包型图标 30 枚四消费点接线（资产源
// docs/design/brand/package-icons，UX-1/K56 生产件；组件
// src/components/PkgIcon.tsx 单点引用）。断言面（AC2 逐消费点）：
//
//   ① pkg-grid：可选磁贴 = brand 官方标；门控磁贴三件套 = mono +
//      opacity 0.4 + pkg-tier-* 徽章（README §6.3：品牌色置灰会脏色）；
//      零 <text>/<title>（K56 字标 path 化 + 组件剥 title）；deb↔debian
//      资产映射；T-383 的 440px 档不因图标回归。
//   ② smu-grid 药丸：brand 官方标；五枚几何字符图标退役（innerText 无）。
//   ③ 类型列（仓库列表 Chip + 制品树 .ico）：mono currentColor——computed
//      stroke 与宿主 color 同值（随文字色，双主题同一套）。
//   ④ License & Add-ons 矩阵：包型槽 + trashcan/webhook 两 feature 槽 =
//      brand；其余四 feature 槽（properties/ha/repo-operations/
//      xray-integration）不在 30 枚集内 = 零图标（注记豁免腿，见日志）。
//   ⑤ 暗底提亮（K61，README §4 登记）：暗色主题下官方深色 <3:1 的枚
//      computed 色落到 --bf-pkgicon-* 提亮档；亮色主题零覆盖。
//
// 断言口径 = e2e/m8/README §2（ADR-0029 决策 3 交互断言制）：computed
// style 只对「token 存在性/接线形态」校验（currentColor、提亮档命中），
// 不做像素断言。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** computed stroke/fill 与宿主 color 逐值比对（mono currentColor 的接线证明）。
 *  形件逐枚遍历：svg 根的 fill="none" 不是色件（不参与），任一形件的
 *  stroke 或 fill 解析值跟随宿主 color 即通过（mono 版全 currentColor）。 */
async function expectStrokeFollowsColor(page: Page, iconSel: string): Promise<void> {
  const verdict = await page.evaluate((sel) => {
    const el = document.querySelector(sel)
    const svg = el?.querySelector('svg')
    if (!el || !svg) return { ok: false, why: 'missing', detail: '' }
    const color = getComputedStyle(el).color
    for (const shape of Array.from(svg.querySelectorAll('*'))) {
      const s = getComputedStyle(shape)
      if (s.stroke === color || s.fill === color) return { ok: true, why: '', detail: '' }
    }
    return { ok: false, why: 'no shape follows color', detail: color }
  }, iconSel)
  expect(verdict.ok, `mono currentColor wiring at ${iconSel} (${verdict.why}${verdict.detail ? `; host color ${verdict.detail}` : ''})`).toBe(true)
}

// ---- 1. pkg-grid：brand/门控三件套 + 零 text + deb 映射 + 440 档 -------------

const CORE_PKG = ['generic', 'docker', 'maven', 'npm', 'pypi'] as const
const PRO_PKG = ['go', 'nuget', 'cargo', 'conan', 'helm', 'helmoci', 'rpm', 'debian'] as const

test('admin: pkg-grid tiles — core brand marks, gated mono triple (mono + 0.4 + tier badge), zero <text>, 440px tier holds', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/new')
  const grid = page.locator('[data-testid="pkg-grid"]')
  await expect(grid).toBeVisible()

  // 可选五核心：恰 1 枚 svg + brand 版 + 装饰位（aria-hidden）
  for (const pt of CORE_PKG) {
    const tile = page.locator(`[data-testid="pkg-grid-item-${pt}"]`)
    await expect(tile).toBeEnabled()
    const icon = tile.locator('.pkg-svg')
    await expect(icon).toHaveCount(1)
    await expect(icon).toHaveAttribute('data-variant', 'brand')
    await expect(icon).toHaveAttribute('aria-hidden', 'true')
  }
  // brand 身份抽查（npm 红方 fill = 官方 #CB3837，亮色零提亮覆盖）
  await expect(page.locator('[data-testid="pkg-grid-item-npm"] .pkg-svg svg rect')).toHaveCSS(
    'fill',
    'rgb(203, 56, 55)',
  )

  // 门控八型三件套：mono + 磁贴 opacity 0.4 + pro 徽章（disabled）
  for (const pt of PRO_PKG) {
    const tile = page.locator(`[data-testid="pkg-grid-item-${pt}"]`)
    await expect(tile).toBeDisabled()
    await expect(tile).toHaveCSS('opacity', '0.4')
    await expect(tile.locator('.pkg-svg')).toHaveAttribute('data-variant', 'mono')
    await expect(tile.locator(`[data-testid="pkg-tier-${pt}"]`)).toHaveText('pro')
  }
  // mono currentColor：门控磁贴的形件色跟随宿主文字色（非品牌色、非死色）
  await expectStrokeFollowsColor(page, '[data-testid="pkg-grid-item-go"] .pkg-svg')

  // deb↔debian 资产映射（README §1 例外：目录名 deb，wire 值 debian）
  await expect(page.locator('[data-testid="pkg-grid-item-debian"] .pkg-svg')).toHaveAttribute(
    'data-icon',
    'debian',
  )

  // 零 <text>/<title>：K56 字标 path 化 + 组件剥 title（30 枚共同卫生）
  await expect(grid.locator('svg text')).toHaveCount(0)
  await expect(grid.locator('svg title')).toHaveCount(0)

  // T-383 440px 档联动复证：磁贴内嵌图标不得改动 Dialog 宽度档
  await expect(grid).toHaveCSS('opacity', '1')
  const box = await grid.boundingBox()
  expect(box, 'pkg-grid paper has a box').toBeTruthy()
  const vw = page.viewportSize()?.width ?? 1280
  expect(box!.width).toBeCloseTo(Math.min(440, vw - 48), 0)
})

// ---- 2. smu-grid 药丸：brand 官方标，几何字符图标退役 --------------------------

test('setmeup pills carry brand marks; geometric glyph icons retired', async ({ page }) => {
  const key = uniq('t390smu')
  await seedRepos(m8Client(), [{ key }, { key: `${key}-npmpkg`, packageType: 'npm' }])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/artifacts')
  await page.click('[data-testid="tree-setmeup"]')
  await expect(page.locator('[data-testid="smu-grid"]')).toBeVisible()

  for (const pt of ['generic', 'npm']) {
    const pill = page.locator(`[data-testid="smu-grid-item-${pt}"]`)
    await expect(pill).toBeVisible()
    // 药丸 = brand 官方标 + 纯文字名（字符图标退役：inner 文本只剩 label）
    await expect(pill.locator('.pkg-svg')).toHaveCount(1)
    await expect(pill.locator('.pkg-svg')).toHaveAttribute('data-variant', 'brand')
    const text = await pill.locator('.pkg-name').innerText()
    expect(text).toBe(pt === 'generic' ? 'Generic' : 'npm')
    expect([...'▫⬢⌬⬒⬓']).not.toContain(text.trim())
  }
  // npm 药丸的官方红（brand 身份抽查；含 step0 药丸面）
  await expect(page.locator('[data-testid="smu-grid-item-npm"] .pkg-svg svg rect')).toHaveCSS(
    'fill',
    'rgb(203, 56, 55)',
  )
})

// ---- 3. 类型列 mono currentColor：仓库列表 Chip + 制品树 .ico -----------------

test('repo list chip and tree node type marks are mono currentColor', async ({ page }) => {
  const key = uniq('t390npm')
  await seedRepos(m8Client(), [{ key, packageType: 'npm' }])

  await loginAs(page, 'admin')
  // 仓库列表：包类型 Chip 挂 mono 图标（MUI Chip icon 槽）
  await page.goto('/binflow/ui/admin/repositories/local')
  const row = page.locator(`[data-testid="repos-row-${key}"]`)
  await expect(row).toBeVisible()
  await expect(row.locator('.badge.neutral .pkg-svg')).toHaveCount(1)
  await expect(row.locator('.badge.neutral .pkg-svg')).toHaveAttribute('data-variant', 'mono')
  await expect(row.locator('.badge.neutral .pkg-svg')).toHaveAttribute('data-icon', 'npm')
  await expectStrokeFollowsColor(page, `[data-testid="repos-row-${key}"] .badge.neutral .pkg-svg`)

  // 制品树：仓库节点 .ico 的包型角标 mono（currentColor 随 .ico 的 text-2）
  await page.goto('/binflow/ui/artifacts')
  const node = page.locator(`[data-testid="tree-repo-${key}"]`)
  await expect(node).toBeVisible()
  await expect(node.locator('.ico .pkg-svg')).toHaveAttribute('data-variant', 'mono')
  await expectStrokeFollowsColor(page, `[data-testid="tree-repo-${key}"] .ico .pkg-svg`)
})

// ---- 4/5. License 矩阵 brand + 暗底提亮档 + axe 双主题 ------------------------

test('addons matrix: package-type & trashcan/webhook rows carry brand marks; feature rows exempt; dark lifts applied', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/general/license')
  await expect(page.locator('[data-testid="addons-table"]')).toBeVisible()

  // ④ 包型槽 13 + trashcan/webhook：brand 官方标；deb↔debian 映射复证
  for (const id of [...CORE_PKG, ...PRO_PKG]) {
    await expect(page.locator(`[data-testid="addons-row-${id}"] .pkg-svg`)).toHaveCount(1)
    await expect(page.locator(`[data-testid="addons-row-${id}"] .pkg-svg`)).toHaveAttribute(
      'data-variant',
      'brand',
    )
  }
  await expect(page.locator('[data-testid="addons-row-debian"] .pkg-svg')).toHaveAttribute(
    'data-icon',
    'debian',
  )
  for (const id of ['trashcan', 'webhook']) {
    await expect(page.locator(`[data-testid="addons-row-${id}"] .pkg-svg`)).toHaveAttribute(
      'data-variant',
      'brand',
    )
  }
  // 豁免腿：非 30 枚集的四 feature 槽零图标
  for (const id of ['properties', 'ha', 'repo-operations', 'xray-integration']) {
    await expect(page.locator(`[data-testid="addons-row-${id}"] .pkg-svg`)).toHaveCount(0)
  }

  // ⑤ 暗底提亮（K61）：暗色主题下官方深色 → --bf-pkgicon-* 档（computed 命中）
  await page.evaluate(() => localStorage.setItem('binflow-console-theme', 'dark'))
  await page.goto('/binflow/ui/admin/general/license')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.locator('[data-testid="addons-table"]')).toBeVisible()
  // trashcan #55606e → #748294（rgb(116, 130, 148)）
  await expect(
    page.locator('[data-testid="addons-row-trashcan"] .pkg-svg svg path').first(),
  ).toHaveCSS('stroke', 'rgb(116, 130, 148)')

  // 暗色建仓网格：npm 红方提亮档 #d35857（rgb(211, 88, 87)）；门控 mono 不受提亮影响
  await page.goto('/binflow/ui/admin/repositories/new')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await expect(page.locator('[data-testid="pkg-grid-item-npm"] .pkg-svg svg rect')).toHaveCSS(
    'fill',
    'rgb(211, 88, 87)',
  )

  // 亮色复证零覆盖（官方色原样）
  await page.evaluate(() => localStorage.setItem('binflow-console-theme', 'light'))
  await page.goto('/binflow/ui/admin/general/license')
  await expect(
    page.locator('[data-testid="addons-row-trashcan"] .pkg-svg svg path').first(),
  ).toHaveCSS('stroke', 'rgb(85, 96, 110)')
})

test('axe: icon-bearing surfaces scan clean in both themes (license matrix + pkg-grid modal)', async ({
  page,
}, testInfo) => {
  test.setTimeout(300_000)
  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/general/license')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="addons-table"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="addons-card"]' })

    await page.goto('/binflow/ui/admin/repositories/new')
    await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
    await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCSS('opacity', '1')
    await expectA11yClean(page, testInfo, { include: '[data-testid="pkg-grid"]' })
  }
})
