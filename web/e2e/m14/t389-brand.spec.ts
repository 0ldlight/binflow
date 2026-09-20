import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'

// T-389（M14 B4 FE，FR-126）——品牌 logo 候选 1「容器·双箭流」转正接线
// 的断言面。六用例中 web/ 四用例 + 资产档的可达性对账：
//
//   ① 侧栏顶 horizontal lockup（历史锚 brand-sidebar-mark）：42px 高、
//      840:256 等比；亮/暗主题分别使用 primary/dark SVG 变体；
//   ② 登录页 lockup（brand-login-lockup，48px 高）：h1 语义保留（img
//      alt=BinFlow）+ 亮/暗主题换变体（src 随 [data-theme] 翻转）；
//   ③ favicon.ico（16/32/48 三档 DIB）+ favicon.svg + favicon-{16,32,48}.png +
//      apple-touch 180 + Safari mask + manifest 192/512：全部经 link[rel] 声明、落共享资产
//      挂载 /binflow/assets/**（public 原位 /binflow/ui/brand/** 是 SPA
//      shell——不可达，wire-brand-assets.mjs 搬挂 + 指纹化），逐 URL 字节
//      对账（PNG/ICO magic + manifest icons 闭环）；
//   ④ axe 双主题：登录页 + 壳（品牌区入镜）serious/critical = 0。
//
// 锚源：console-ux §10（brand-sidebar-mark / brand-login-lockup——v1.21
// 批；login-* / app-nav* 冻结族零改名）。断言口径 = e2e/m8/README §2。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

/** 预置主题再进登录页（ThemeContext 首读 localStorage——匿名面无切换钮）。
 *  经 evaluate + reload 而非 addInitScript：init script 在同 page 的每次
 *  导航都会重放并累积，会把后续 loginAs 落地的壳主题一并钉死（本票实测
 *  踩坑）；localStorage 本就跨导航持久，显式写一次 + 重载即够。 */
async function loginPageWithTheme(page: import('@playwright/test').Page, theme: 'light' | 'dark') {
  await page.goto('/binflow/ui/login')
  // Login can paint before ThemeProvider's initial effect persists its theme;
  // wait for that effect before overwriting storage, otherwise the initial
  // light write can race the test's requested dark value.
  await page.waitForFunction(() => document.documentElement.dataset.theme !== '')
  await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
  await page.reload()
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
}

// ---- 1. 品牌位：侧栏 mark（固定暗变体）+ 登录 lockup（随主题换） ---------------

test('brand slots: sidebar and login lockups switch with theme', async ({ page }) => {
  // 登录页：lockup 在 h1 内、alt 承载可读名、几何 48px 高档 + 840:256 等比
  await loginPageWithTheme(page, 'light')
  const lockup = page.locator('h1 [data-testid="brand-login-lockup"]')
  await expect(lockup).toBeVisible()
  await expect(lockup).toHaveAttribute('alt', 'BinFlow')
  await expect(lockup).toHaveAttribute('height', '48')
  expect((await lockup.boundingBox())?.height).toBe(48)
  const lightSrc = await lockup.getAttribute('src')
  expect(lightSrc).toContain('image/svg') // vite 内联 data URI（<4KB 资产）
  // 副题仍在（lockup 只换品牌图形行，说明文字不动）
  await expect(page.getByText('制品仓库控制台')).toBeVisible()

  // 暗主题：变体翻转（lockup-dark ≠ lockup-horizontal）
  await loginPageWithTheme(page, 'dark')
  const darkSrc = await page.locator('[data-testid="brand-login-lockup"]').getAttribute('src')
  expect(darkSrc).toBeTruthy()
  expect(darkSrc).not.toBe(lightSrc)

  // 壳：侧栏横版 lockup 42px 高；主题切换时换 primary/dark 变体。
  await loginAs(page, 'admin')
  await page.evaluate(() => localStorage.setItem('binflow-console-theme', 'light'))
  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  const navLockup = page.locator('.app-nav-brand [data-testid="brand-sidebar-mark"]')
  await expect(navLockup).toBeVisible()
  await expect(navLockup).toHaveAttribute('alt', 'BinFlow')
  await expect(navLockup).toHaveAttribute('data-brand-variant', 'primary')
  expect((await navLockup.boundingBox())?.height).toBe(42)
  expect((await navLockup.boundingBox())?.width).toBeGreaterThanOrEqual(132)
  const navLightSrc = await navLockup.getAttribute('src')
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(navLockup).toHaveAttribute('data-brand-variant', 'dark')
  await expect(navLockup.getAttribute('src')).not.toBe(navLightSrc)

  // ◆ 品牌残稿退役（壳面零残留；包型网格的 ◆ 是包图标字汇，非品牌位）
  await expect(page.locator('.app-nav-brand').getByText('◆')).toHaveCount(0)
})

// ---- 2. favicon / PWA / manifest：共享资产挂载上的字节对账 ---------------------

test('favicon ico(16+32+48) + png tiers + apple 180 + manifest 512 wired and served', async ({
  page,
  request,
}) => {
  await page.goto('/binflow/ui/')

  // link 面：ico + svg + png 三档 + apple-touch，全部指纹化入共享挂载
  const iconHrefs = await page.locator('link[rel="icon"], link[rel="apple-touch-icon"]').evaluateAll((els) =>
    els.map((el) => (el as HTMLLinkElement).href),
  )
  expect(iconHrefs.length).toBe(6)
  const svgHref = iconHrefs.find((href) => href.endsWith('.svg'))
  expect(svgHref).toBeTruthy()
  expect((await request.get(svgHref!)).status()).toBe(200)
  // Vite serves canonical public/brand files during UI development; the
  // production build moves the same bytes to immutable /binflow/assets/.
  const base = new URL(page.url()).origin
  const devAssetBase = `${base}/binflow/ui/brand/`
  for (const href of iconHrefs) {
    expect(href.includes('/binflow/assets/') || href.startsWith(devAssetBase)).toBe(true)
  }
  const icoHref = iconHrefs.find((h) => h.endsWith('.ico'))
  expect(icoHref).toBeTruthy()

  // ico 字节：ICONDIR magic + 恰好 3 档（16/32/48 DIB）
  const ico = await request.get(icoHref!)
  expect(ico.status()).toBe(200)
  const icoBuf = await ico.body()
  expect(icoBuf.length).toBeGreaterThan(0)
  expect(icoBuf[0]).toBe(0)
  expect(icoBuf[1]).toBe(0)
  expect(icoBuf[2]).toBe(1) // type=icon
  expect(icoBuf.readUInt16LE(4)).toBe(3)

  // png 档：16/32/48 + apple 180，PNG magic
  const pngHrefs = iconHrefs.filter((h) => h.endsWith('.png'))
  expect(pngHrefs.length).toBe(4)
  for (const href of pngHrefs) {
    const res = await request.get(href)
    expect(res.status()).toBe(200)
    expect(res.headers()['content-type']).toBe('image/png')
    const buf = await res.body()
    expect(buf[0]).toBe(0x89)
    expect(buf.toString('ascii', 1, 4)).toBe('PNG')
  }

  // manifest：application/json、start_url 解析回 SPA 段、icons 闭环（180+512）
  const manifestHref = await page.locator('link[rel="manifest"]').evaluate((el) => (el as HTMLLinkElement).href)
  expect(manifestHref.includes('/binflow/assets/') || manifestHref.startsWith(devAssetBase)).toBe(true)
  const manifestRes = await request.get(manifestHref)
  expect(manifestRes.status()).toBe(200)
  expect(manifestRes.headers()['content-type']).toContain('application/json')
  const manifest = (await manifestRes.json()) as {
    icons: Array<{ src: string; sizes: string; type: string }>
    start_url: string
  }
  expect(new URL(manifest.start_url, manifestHref).pathname).toBe('/binflow/ui/')
  expect(manifest.icons.map((i) => i.sizes).sort()).toEqual(['192x192', '512x512'])
  for (const icon of manifest.icons) {
    const res = await request.get(new URL(icon.src, manifestHref).toString())
    expect(res.status()).toBe(200)
    expect(res.headers()['content-type']).toBe('image/png')
  }

  // Safari pinned-tab mask uses the monochrome modular symbol.
  const maskHref = await page.locator('link[rel="mask-icon"]').evaluate((el) => (el as HTMLLinkElement).href)
  expect(maskHref.includes('/binflow/assets/') || maskHref.startsWith(devAssetBase)).toBe(true)
  const mask = await request.get(maskHref)
  expect(mask.status()).toBe(200)
  expect((await mask.body()).toString()).toContain('<svg')
})

// ---- 3. axe 双主题：品牌区入镜的登录页 + 壳 -------------------------------------

test('axe: login brand area + shell clean in both themes', async ({ page }, testInfo) => {
  await loginPageWithTheme(page, 'light')
  await expectA11yClean(page, testInfo, { include: '[data-testid="login-page"]' })
  await loginPageWithTheme(page, 'dark')
  await expectA11yClean(page, testInfo, { include: '[data-testid="login-page"]' })

  await loginAs(page, 'admin')
  await page.evaluate(() => localStorage.setItem('binflow-console-theme', 'light'))
  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  // 壳挂载等待（boot 屏竞态）：goto 后会话验证在途时 app-nav 尚未上树，
  // 直接扫 include 会扑空——可见性断言即挂载闸
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="app-nav"]' })
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expectA11yClean(page, testInfo, { include: '[data-testid="app-nav"]' })
})
