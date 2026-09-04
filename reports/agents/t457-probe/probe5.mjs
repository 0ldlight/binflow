// T-457 probe round 5: profile page anatomy behind the onboarding overlay
// (overlay detached in probe DOM only — zero server writes).
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
await page.goto('http://localhost:8082/ui/user_profile')
await page.waitForLoadState('networkidle')
await page.waitForTimeout(2500)
await page.evaluate(() => {
  document.querySelectorAll('.onboarding-main-wrapper, .welcome-wrapper').forEach((el) => el.remove())
})
await page.waitForTimeout(600)
await page.screenshot({ path: `${OUT}s4-profile.png`, fullPage: true })
fs.writeFileSync(`${OUT}s4-profile.url.txt`, page.url())
log(`url: ${page.url()}`)
const txt = await page.evaluate(() => document.body.innerText)
log('page text:')
txt.split('\n').forEach((l) => log(`  | ${l}`))
// interactive elements inventory
const els = await page.evaluate(() =>
  Array.from(document.querySelectorAll('button, a, input, [role="button"]'))
    .filter((el) => el.getBoundingClientRect().width > 0)
    .map((el) => ({
      tag: el.tagName,
      cy: el.getAttribute('data-cy'),
      testid: el.getAttribute('data-testid'),
      text: (el.innerText || el.value || el.placeholder || '').trim().slice(0, 50),
      type: el.getAttribute('type'),
    })),
)
log('interactive elements:')
els.forEach((e) => log(`  ${JSON.stringify(e)}`))

fs.writeFileSync(`${OUT}probe5-evidence.txt`, lines.join('\n') + '\n')
await browser.close()
console.log('probe5 done')
