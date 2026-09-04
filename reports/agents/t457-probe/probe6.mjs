// T-457 probe round 6: profile anatomy — navigate in-app (user menu → Edit
// Profile), hide onboarding overlay with display:none (no DOM removal).
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

const domClick = (sel) =>
  page.evaluate((s) => {
    const el = document.querySelector(s)
    if (!el) return 'missing'
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    return 'clicked'
  }, sel)

await domClick('button[aria-label="User Menu"]')
await page.waitForTimeout(900)
await page.evaluate(() => {
  const items = Array.from(document.querySelectorAll('[role="menuitem"], .el-dropdown-menu__item, li'))
  const hit = items.find((el) => el.textContent?.trim() === 'Edit Profile')
  if (hit) hit.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
})
await page.waitForTimeout(2500)
// hide (not remove) the onboarding overlay
await page.evaluate(() => {
  document.querySelectorAll('.onboarding-main-wrapper, .welcome-wrapper').forEach((el) => {
    el.style.display = 'none'
  })
})
await page.waitForTimeout(800)
fs.writeFileSync(`${OUT}s4-profile.url.txt`, page.url())
await page.screenshot({ path: `${OUT}s4-profile.png`, fullPage: true })
log(`url: ${page.url()}`)
const txt = await page.evaluate(() => document.body.innerText)
log('page text:')
txt.split('\n').forEach((l) => log(`  | ${l}`))
const els = await page.evaluate(() =>
  Array.from(document.querySelectorAll('button, a, input, [role="button"]'))
    .filter((el) => el.getBoundingClientRect().width > 0)
    .map((el) => ({
      tag: el.tagName,
      cy: el.getAttribute('data-cy'),
      testid: el.getAttribute('data-testid'),
      text: (el.innerText || el.value || el.placeholder || '').trim().slice(0, 60),
      type: el.getAttribute('type'),
    })),
)
log('interactive elements:')
els.forEach((e) => log(`  ${JSON.stringify(e)}`))

fs.writeFileSync(`${OUT}probe6-evidence.txt`, lines.join('\n') + '\n')
await browser.close()
console.log('probe6 done')
