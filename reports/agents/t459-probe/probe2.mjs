// T-459 probe round 2: admin filter behavior on Enter + pause state +
// System Logs text area presence. Zero writes (INC-1).
import { chromium } from '/Users/lzw/dev-center/web/node_modules/playwright/index.mjs'
import fs from 'node:fs'

const OUT = new URL('./', import.meta.url).pathname
const AF = 'http://localhost:8082'
const lines = []
const log = (s = '') => lines.push(s)

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })

await page.goto(`${AF}/ui/login/`)
await page.locator('input[name="username"]').waitFor({ state: 'visible', timeout: 30_000 })
await page.fill('input[name="username"]', 'admin')
await page.fill('input[name="password"]', 'JFrog@2026')
await page.click('button[type="submit"]')
await page.waitForTimeout(3000)

// [E5] admin filter — Enter behavior (URL change / results panel?)
await page.goto(`${AF}/ui/admin/monitoring/storage-summary`)
await page.waitForLoadState('networkidle').catch(() => {})
await page.waitForTimeout(2500)
await page.evaluate(() => {
  document.querySelectorAll('.onboarding-main-wrapper, .welcome-wrapper').forEach((el) => el.remove())
})
const box = page.locator('input[placeholder*="Search Admin"]').first()
if (await box.count()) {
  await box.fill('backup')
  await page.keyboard.press('Enter')
  await page.waitForTimeout(1500)
  await page.screenshot({ path: `${OUT}s5-admin-filter-enter.png`, fullPage: false })
  fs.writeFileSync(`${OUT}s5-admin-filter-enter.url.txt`, page.url())
  log(`[E5] after Enter url=${page.url()}`)
  const bodyHead = (await page.evaluate(() => document.body.innerText)).slice(0, 900)
  fs.writeFileSync(`${OUT}s5-admin-filter-enter-text.txt`, bodyHead)
  log(`[E5] body head:\n${bodyHead}\n----`)

  // nav link count + hidden states after Enter
  const navState = await page.evaluate(() =>
    [...document.querySelectorAll('nav a, aside a')].map((a) => ({
      text: a.textContent?.trim().slice(0, 40),
      hidden: a.getBoundingClientRect().height === 0,
    })),
  )
  fs.writeFileSync(`${OUT}s5-admin-filter-enter-nav.json`, JSON.stringify(navState, null, 1))
  log('[E5] nav after enter: ' + JSON.stringify(navState.filter((n) => !n.hidden).map((n) => n.text)))
}

// [E6] System Logs page — pause toggle + log content area
await page.goto(`${AF}/ui/admin/monitoring/system_logs`)
await page.waitForLoadState('networkidle').catch(() => {})
await page.waitForTimeout(2500)
{
  const main = await page.evaluate(() => {
    const main = document.querySelector('main') ?? document.body
    const textareas = [...main.querySelectorAll('textarea, pre, [class*="log"], [class*="terminal"]')].map((el) => ({
      tag: el.tagName.toLowerCase(),
      cls: String(el.className ?? '').slice(0, 70),
      textLen: (el.textContent ?? '').length,
      textHead: (el.textContent ?? '').slice(0, 300),
    }))
    const buttons = [...main.querySelectorAll('button')].map((b) => b.textContent?.trim().slice(0, 40)).filter(Boolean)
    return { textareas, buttons }
  })
  fs.writeFileSync(`${OUT}s6-system-logs-anatomy.json`, JSON.stringify(main, null, 1))
  log('[E6] system logs anatomy: ' + JSON.stringify(main).slice(0, 1500))

  // pause -> countdown stops (read-only UI state)
  const pauseBtn = page.locator('button', { hasText: 'Pause' }).first()
  if (await pauseBtn.count()) {
    await pauseBtn.click()
    await page.waitForTimeout(1200)
    const after = await page.evaluate(() => document.body.innerText.match(/Refreshing Logs in \d+ seconds?|Paused[^\n]*/)?.[0])
    log(`[E6] after Pause: "${after}"`)
    await page.screenshot({ path: `${OUT}s6-system-logs-paused.png`, fullPage: false })
    // resume to leave no client state behind (reload also clears)
    await page.reload().catch(() => {})
  }
}

await browser.close()
fs.writeFileSync(`${OUT}probe2-evidence.txt`, lines.join('\n') + '\n')
console.log(lines.join('\n'))
