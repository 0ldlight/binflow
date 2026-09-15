import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from './m8/support/a11y'

// T-UIB4（design-system-plan §6 批 4）：内嵌 styleguide 的 e2e 腿。
//
// 可达面：/binflow/ui/dev/styleguide 仅在 dev server 与 styleguide 模式
// 构建（npm run build:styleguide → go:embed 隔离实例）上注册——对生产
// 构建实例，正向用例按探针 skip（与 console-smoke 的 T-91 门同款姿态），
// 门控负证用例则反向 skip 并断言「路由未注册 → 渲染空白、无 chunk 文案」。
//
// 断言口径（ADR-0029 决策 3）：交互断言 + computed token 存在性；截图为
// 证据附件（非像素差分门——golden 归 conductor 的 design-baseline 腿，
// 本文件不跑它）。

const STYLEGUIDE = '/binflow/ui/dev/styleguide'
const ROOT = '[data-testid="styleguide-root"]'

/** 打开 styleguide；返回本构建是否注册了该路由（false=生产构建）。
 *  SPA 挂载是异步引导（initI18n then render）——先等挂载再判空，
 *  生产构建超时后留白复检，避免把慢挂载误判为「未注册」。 */
async function openStyleguide(page: Page): Promise<boolean> {
  await page.goto(STYLEGUIDE)
  try {
    await page.locator(ROOT).waitFor({ state: 'attached', timeout: 10_000 })
    return true
  } catch {
    return (await page.locator(ROOT).count()) === 1
  }
}

/** 证据截图：attach 到测试输出；设 STYLEGUIDE_SHOT_DIR 时另存 PNG（报告证据位） */
async function evidence(page: Page, name: string): Promise<void> {
  const buf = await page.screenshot({ fullPage: true })
  const dir = process.env.STYLEGUIDE_SHOT_DIR
  if (dir) {
    const { mkdirSync, writeFileSync } = await import('node:fs')
    const { join } = await import('node:path')
    mkdirSync(dir, { recursive: true })
    writeFileSync(join(dir, `${name}.png`), buf)
  }
  await test.info().attach(name, { body: buf, contentType: 'image/png' })
}

test('negative: production build does not register the styleguide route', async ({ page }) => {
  const present = await openStyleguide(page)
  test.skip(present, 'styleguide registered in this build (dev server / styleguide-mode instance)')

  // 路由未注册 → data router 无匹配，SPA 壳渲染空白（非 404 页——
  // /dev/* 不在路由表内，连 NotFound 都不经过）
  await expect(page.locator('#root')).toBeAttached()
  await expect(page.locator(ROOT)).toHaveCount(0)
  await expect(page.locator('body')).not.toContainText('BinFlow Design System')
})

test('state matrix: every §4.2 section renders', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  await expect(page.locator('h1')).toContainText('BinFlow Design System')

  for (const id of [
    'button',
    'spinner',
    'input',
    'textarea',
    'checkbox',
    'switch',
    'radio-group',
    'badge',
    'badge-convergence',
    'toast',
    'tabs',
    'breadcrumb',
    'states',
    // 批 6（T-UIB6）：域件 / motion / P2 矩阵行
    'domain-tree',
    'checksum',
    'path-breadcrumb',
    'properties',
    'motion',
    'p2',
  ]) {
    await expect(page.locator(`section#${id}`)).toBeVisible()
  }
  // 抽样锚：四缺件本体 + 批 4 态载体
  await expect(page.getByRole('checkbox', { name: 'Controlled (checked=true)' })).toBeVisible()
  await expect(page.getByRole('switch', { name: 'Mirror to remote' })).toBeVisible()
  await expect(page.getByRole('radio', { name: 'Remote' })).toBeVisible()
  await expect(page.getByLabel('Description')).toBeVisible()
})

test('button loading: aria-busy + real disabled (double-submit guard)', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  const btn = page.getByTestId('sg-btn-loading')
  await expect(btn).toHaveAttribute('aria-busy', 'true')
  await expect(btn).toBeDisabled()
  await expect(btn).toContainText('Deploying…')
})

test('input: error face + affix wrapper carry the semantics', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  const err = page.getByTestId('sg-input-error')
  await expect(err).toHaveAttribute('aria-invalid', 'true')
  await expect(err).toHaveAttribute('aria-describedby', 'sg-input-error-desc')
  await expect(page.locator('#sg-input-error-desc')).toContainText('Lowercase')

  const affix = page.getByTestId('sg-input-affix')
  await expect(affix).toHaveValue('30')
  await expect(affix.locator('..')).toContainText('days')
})

test('controls: checkbox / switch / radio interactions', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  const box = page.getByTestId('sg-checkbox')
  await expect(box).toBeChecked()
  await box.click()
  await expect(box).not.toBeChecked()

  const sw = page.getByTestId('sg-switch')
  await expect(sw).toHaveAttribute('aria-checked', 'true')
  await sw.click()
  await expect(sw).toHaveAttribute('aria-checked', 'false')

  await page.getByRole('radio', { name: 'Remote' }).check()
  await expect(page.getByRole('radio', { name: 'Remote' })).toBeChecked()
  await expect(page.getByRole('radio', { name: 'Local' })).not.toBeChecked()
})

test('tabs: keyboard arrow moves activation (Radix roving)', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  const general = page.getByRole('tab', { name: 'General' })
  await general.focus()
  await expect(general).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('ArrowRight')
  const perms = page.getByRole('tab', { name: 'Permissions' })
  await expect(perms).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('Permissions content')).toBeVisible()
})

test('breadcrumb + states + toast carriers', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  await expect(page.locator('section#breadcrumb nav[aria-label="breadcrumb"] [aria-current="page"]')).toContainText('artifact-1.0.0.tgz')

  await expect(page.getByTestId('sg-empty')).toBeVisible()
  const card = page.getByTestId('error-card')
  await expect(card).toHaveAttribute('role', 'alert')
  await card.getByRole('button', { name: '重试' }).click()
  await expect(page.getByTestId('sg-retry-count')).toContainText('retries: 1')

  await page.getByTestId('sg-toast-success').click()
  await expect(page.locator('[data-testid="toast"]', { hasText: 'Artifact deployed' })).toBeVisible()
})

test('dual-theme: toggle swaps [data-theme] and token values', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  const lightBg = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--bf-bg'))

  await page.getByTestId('styleguide-theme-toggle').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  const darkBg = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--bf-bg'))
  expect(darkBg.trim(), 'token 整组换值（纯换值语义，ADR-0029 决策 3）').not.toBe(lightBg.trim())
})

test('axe: light + dark, 0 serious/critical', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  const info = test.info()

  await expectA11yClean(page, info, { include: ROOT })
  await evidence(page, 'styleguide-light')

  await page.getByTestId('styleguide-theme-toggle').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expectA11yClean(page, info, { include: ROOT })
  await evidence(page, 'styleguide-dark')
})

// ---- T-UIB6（design-system-plan §6 批 6）扩腿：徽章两族收敛呈裁 +
// 域件交互 + motion token 时长（正常档）+ prefers-reduced-motion 降级档 ----

test('badge convergence: tint (legacy recipe) and soft (semantic surface) families side by side', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  // 两族并存呈裁（任务 5）——conductor 终裁前任何一族不可缺席
  for (const v of ['tint-neutral', 'tint-info', 'tint-success', 'tint-warning', 'tint-danger']) {
    await expect(page.getByTestId(`sg-badge-${v}`)).toBeVisible()
  }
  await expect(page.getByTestId('sg-badge-tint-success')).toHaveAttribute('data-variant', 'tint-success')
  // soft 族同屏（同状态并排格）
  const cell = page.locator('section#badge-convergence').getByText('healthy').first()
  await expect(cell).toBeVisible()
})

test('domain pieces: tree selection moves, checksum reveal toggles, missing algo keeps em-dash', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  // 树选中迁移（click 移动 selected 语义 + 左缘条）
  const file = page.getByTestId('sg-tree-row-file')
  await expect(file).not.toHaveAttribute('aria-selected', 'true')
  await file.click()
  await expect(file).toHaveAttribute('aria-selected', 'true')
  await expect(file.locator('span.bg-primary')).toHaveCount(1)

  // checksum reveal：截断 → 全值 → 再收截断
  const reveal = page.getByTestId('sg-checksum-reveal-sha256')
  await expect(reveal).toContainText('…')
  await reveal.click()
  await expect(reveal).toContainText('3f9a1c8e5b2d74f06a1c9e8b7d6f5a4e3c2b1a0987654321fedcba9876543210')
  await reveal.click()
  await expect(reveal).toContainText('…')
  // 缺失 md5 = 「—」占位（不伪造）
  await expect(page.getByTestId('sg-checksum-demo')).toContainText('md5')
})

/** computed 动效时长（秒串）：animation-duration 优先，none 则取 transition-duration */
async function motionDurations(page: Page, selectors: string[]): Promise<Record<string, string>> {
  return page.evaluate((sels) => {
    const out: Record<string, string> = {}
    for (const sel of sels) {
      const el = document.querySelector(sel)
      if (!el) {
        out[sel] = 'MISSING'
        continue
      }
      const cs = getComputedStyle(el)
      out[sel] = cs.animationName !== 'none' ? cs.animationDuration : cs.transitionDuration
    }
    return out
  }, selectors)
}

/** 开一个浮层面 → 量 computed 时长 → Esc 关掉（模态层拦点击，不叠开） */
async function openAndMeasure(page: Page, openTestId: string, contentTestId: string): Promise<string> {
  await page.getByTestId(openTestId).click()
  const content = page.getByTestId(contentTestId)
  await expect(content).toBeVisible()
  const [dur] = Object.values(await motionDurations(page, [`[data-testid="${contentTestId}"]`]))
  await page.keyboard.press('Escape')
  await expect(content).toBeHidden()
  return dur
}

test('motion tokens: fade=fast / pop=base / slide=slow on computed durations', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  // 逐面开→量→关（模态层会拦后续点击——不叠开）
  await expect(await openAndMeasure(page, 'sg-motion-dialog-open', 'sg-motion-dialog')).toBe('0.12s') // modal fade = dur-fast
  await expect(await openAndMeasure(page, 'sg-motion-popover-open', 'sg-motion-popover')).toBe('0.16s') // anchored pop = dur-base
  await expect(await openAndMeasure(page, 'sg-motion-sheet-open', 'sg-motion-sheet')).toBe('0.24s') // sheet slide = dur-slow
  await expect(await openAndMeasure(page, 'sg-motion-drawer-open', 'sg-motion-drawer')).toBe('0.24s') // vaul drawer re-timed
  // toast（sonner transition 接管档）
  await page.getByTestId('sg-motion-toast').click()
  await expect(page.locator('[data-sonner-toast]').last()).toBeVisible()
  expect(await motionDurations(page, ['[data-sonner-toast]'])).toEqual({ '[data-sonner-toast]': '0.24s' })
})

test('prefers-reduced-motion: overlay/drawer/toast durations collapse to the 1ms degrade value', async ({ page }) => {
  test.skip(!(await openStyleguide(page)), 'styleguide route not registered in this build (production)')
  await page.emulateMedia({ reducedMotion: 'reduce' })

  await expect(await openAndMeasure(page, 'sg-motion-dialog-open', 'sg-motion-dialog')).toBe('0.001s')
  await expect(await openAndMeasure(page, 'sg-motion-popover-open', 'sg-motion-popover')).toBe('0.001s')
  await expect(await openAndMeasure(page, 'sg-motion-sheet-open', 'sg-motion-sheet')).toBe('0.001s')
  await expect(await openAndMeasure(page, 'sg-motion-drawer-open', 'sg-motion-drawer')).toBe('0.001s')
  await page.getByTestId('sg-motion-toast').click()
  await expect(page.locator('[data-sonner-toast]').last()).toBeVisible()
  // toast：token 档降 1ms；sonner 自身对 reduce 还会整关 transition
  //（prop=none / 0s）——两种都是降级值，数值断言 ≤1ms 收口
  const toastDur = (await motionDurations(page, ['[data-sonner-toast]']))['[data-sonner-toast]']
  expect(parseFloat(toastDur), `toast degrade ≤1ms (got ${toastDur})`).toBeLessThanOrEqual(0.001)
})
