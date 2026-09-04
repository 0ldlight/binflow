// T-457 probe round 4 (final): robust login wait, synthetic clicks that
// bypass the onboarding overlay interception, help menu + About + profile.
import { chromium } from '/Users/lzw/dev-center/web/node_modules/playwright/index.mjs'
import fs from 'node:fs'

const OUT = '/Users/lzw/dev-center/reports/agents/t457-probe/'
const lines = []
const log = (s = '') => lines.push(s)

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
await page.goto('http://localhost:8082/ui/login/')
await page.locator('input[name="username"]').waitFor({ state: 'visible', timeout: 30_000 })
await page.fill('input[name="username"]', 'admin')
await page.fill('input[name="password"]', 'JFrog@2026')
await page.click('button[type="submit"]')
await page.locator('button[aria-label="Get Help and Learn"]').waitFor({ state: 'attached', timeout: 30_000 })
await page.waitForTimeout(2500)
fs.writeFileSync(`${OUT}s0.url.txt`, page.url())
await page.screenshot({ path: `${OUT}s0-home.png` })

/** synthetic DOM click — bypasses pointer-interception by overlay banners */
const domClick = (sel) =>
  page.evaluate((s) => {
    const el = document.querySelector(s)
    if (!el) return 'missing'
    el.dispatchEvent(new MouseEvent('mouseenter', { bubbles: true }))
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    return 'clicked'
  }, sel)

const visiblePoppers = () =>
  page.evaluate(() =>
    Array.from(document.querySelectorAll('.el-popper, [role="menu"]'))
      .filter((m) => m.getBoundingClientRect().width > 0 && m.getBoundingClientRect().height > 0)
      .map((m) => ({
        cls: (m.className || '').toString().slice(0, 70),
        text: m.innerText,
        links: Array.from(m.querySelectorAll('a')).map((a) => ({ text: a.textContent?.trim(), href: a.getAttribute('href') })),
      })),
  )

// [E1] help dropdown
log('[E1] help dropdown (synthetic click on Get Help and Learn)')
log(`  click: ${await domClick('button[aria-label="Get Help and Learn"]')}`)
await page.waitForTimeout(1200)
await page.screenshot({ path: `${OUT}s1-help-open.png` })
const e1 = await visiblePoppers()
log(JSON.stringify(e1, null, 1))

// [E2] About
log('')
log('[E2] About dialog')
const aboutClicked = await page.evaluate(() => {
  const items = Array.from(document.querySelectorAll('[role="menuitem"], .el-dropdown-menu__item, li'))
  const hit = items.find((el) => el.textContent?.trim() === 'About')
  if (!hit) return 'missing'
  hit.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
  return 'clicked'
})
log(`  About item: ${aboutClicked}`)
await page.waitForTimeout(1600)
await page.screenshot({ path: `${OUT}s2-about.png` })
const dlgText = await page.evaluate(() => {
  const dlgs = Array.from(document.querySelectorAll('[role="dialog"], .el-dialog')).filter((d) => d.getBoundingClientRect().width > 0)
  return dlgs.map((d) => d.innerText)
})
log(`  dialogs: ${JSON.stringify(dlgText, null, 1)}`)
const dlgHtml = await page.evaluate(() => {
  const dlgs = Array.from(document.querySelectorAll('[role="dialog"], .el-dialog')).filter((d) => d.getBoundingClientRect().width > 0)
  return dlgs.map((d) => d.outerHTML).join('\n')
})
if (dlgHtml) fs.writeFileSync(`${OUT}s2-about.dom.html`, dlgHtml)
await page.keyboard.press('Escape')
await page.waitForTimeout(500)

// [E3] user menu → Edit Profile
log('')
log('[E3] user menu → Edit Profile')
log(`  click: ${await domClick('button[aria-label="User Menu"]')}`)
await page.waitForTimeout(1000)
await page.screenshot({ path: `${OUT}s3-user-menu.png` })
const e3 = await visiblePoppers()
log('  user menu: ' + JSON.stringify(e3, null, 1))
const editClicked = await page.evaluate(() => {
  const items = Array.from(document.querySelectorAll('[role="menuitem"], .el-dropdown-menu__item, li'))
  const hit = items.find((el) => el.textContent?.trim() === 'Edit Profile')
  if (!hit) return 'missing'
  hit.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
  return 'clicked'
})
log(`  Edit Profile: ${editClicked}`)
await page.waitForLoadState('networkidle')
await page.waitForTimeout(2500)
fs.writeFileSync(`${OUT}s4-profile.url.txt`, page.url())
await page.screenshot({ path: `${OUT}s4-profile.png`, fullPage: true })
log(`  profile url: ${page.url()}`)
const ptxt = await page.evaluate(() => document.body.innerText)
log('  page text (first 3500):')
ptxt.slice(0, 3500).split('\n').forEach((l) => log(`    | ${l}`))

fs.writeFileSync(`${OUT}probe4-evidence.txt`, lines.join('\n') + '\n')
await browser.close()
console.log('probe4 done')
