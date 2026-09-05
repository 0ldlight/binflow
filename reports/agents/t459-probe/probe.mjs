// T-459 read-only probe: Artifactory 7.161.20 (:8082) — System Logs viewer /
// Service Status page / admin-mode "Search Admin Resources" filter.
// Zero writes (INC-1): navigation + read-only GETs only; no Create/Save/Run
// clicks; the admin filter input is typed into and then cleared via reload
// (client-side filter only — no server state).
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

const snapshot = async (name, url) => {
  await page.goto(url)
  await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(2500)
  // detach onboarding overlay if present (client-side only)
  await page.evaluate(() => {
    document.querySelectorAll('.onboarding-main-wrapper, .welcome-wrapper').forEach((el) => el.remove())
  })
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${OUT}${name}.png`, fullPage: true })
  fs.writeFileSync(`${OUT}${name}.url.txt`, page.url())
  const text = await page.evaluate(() => document.body.innerText)
  fs.writeFileSync(`${OUT}${name}-text.txt`, text)
  log(`[${name}] url=${page.url()}`)
  log(`[${name}] text head:\n${text.slice(0, 1200)}\n----`)
  return text
}

// [E1] System Logs page — toolbar anatomy (tail refresh / filter / download)
await snapshot('s1-system-logs', `${AF}/ui/admin/monitoring/system_logs`)
{
  const controls = await page.evaluate(() => {
    const out = []
    for (const el of document.querySelectorAll('button, input, select, [role="button"]')) {
      const r = el.getBoundingClientRect()
      if (r.width === 0 || r.height === 0) continue
      const label =
        el.getAttribute('aria-label') ?? el.getAttribute('placeholder') ?? el.textContent?.trim().slice(0, 60) ?? ''
      out.push({
        tag: el.tagName.toLowerCase(),
        cy: el.getAttribute('data-cy'),
        label: String(label).slice(0, 80),
        cls: String(el.className ?? '').slice(0, 60),
      })
    }
    return out
  })
  fs.writeFileSync(`${OUT}s1-system-logs-controls.json`, JSON.stringify(controls, null, 1))
  log('[E1] system logs controls: ' + JSON.stringify(controls))

  // Throttle/tail-refresh/selectors network shape (read-only GETs)
  const apiCalls = []
  page.on('request', (req) => {
    const u = req.url()
    if (u.includes('/ui/api/') || u.includes('/api/system') || u.includes('log')) apiCalls.push(`${req.method()} ${u}`)
  })
  await page.reload({ waitUntil: 'networkidle' }).catch(() => {})
  await page.waitForTimeout(2500)
  fs.writeFileSync(`${OUT}s1-system-logs-api.json`, JSON.stringify(apiCalls, null, 1))
  log('[E1] system logs api calls: ' + JSON.stringify(apiCalls.slice(0, 25)))
}

// [E2] Service Status page
await snapshot('s2-service-status', `${AF}/ui/admin/monitoring/service-status`)
{
  const headings = await page.evaluate(() =>
    [...document.querySelectorAll('h1,h2,h3,h4,[class*="title"]')].map((h) => h.textContent?.trim()).filter(Boolean),
  )
  fs.writeFileSync(`${OUT}s2-service-status-headings.json`, JSON.stringify(headings, null, 1))
  log('[E2] service status headings: ' + JSON.stringify(headings))
}

// [E3] admin-mode topbar "Search Admin Resources" filter (position + placeholder
//      + filters the sidebar) — baseline A1-5 says topbar in admin mode.
await page.goto(`${AF}/ui/admin/monitoring/storage-summary`)
await page.waitForLoadState('networkidle').catch(() => {})
await page.waitForTimeout(2500)
await page.evaluate(() => {
  document.querySelectorAll('.onboarding-main-wrapper, .welcome-wrapper').forEach((el) => el.remove())
})
{
  const found = await page.evaluate(() => {
    const inputs = [...document.querySelectorAll('input')].map((el) => {
      const r = el.getBoundingClientRect()
      return {
        placeholder: el.getAttribute('placeholder'),
        aria: el.getAttribute('aria-label'),
        x: Math.round(r.x),
        y: Math.round(r.y),
        w: Math.round(r.width),
        visible: r.width > 0,
      }
    })
    const adminHits = inputs.filter((i) => /admin/i.test(`${i.placeholder} ${i.aria}`))
    return { total: inputs.length, adminHits }
  })
  fs.writeFileSync(`${OUT}s3-admin-filter.json`, JSON.stringify(found, null, 1))
  log('[E3] admin-mode search inputs: ' + JSON.stringify(found))
}

// type into the admin filter and observe the sidebar shrink (client-side)
{
  const box = page.locator('input[placeholder*="Search Admin"], input[aria-label*="Search Admin" i]').first()
  if (await box.count()) {
    const before = await page.evaluate(() => document.querySelectorAll('nav a, aside a').length)
    await box.fill('backup')
    await page.waitForTimeout(800)
    await page.screenshot({ path: `${OUT}s3-admin-filter-backup.png`, fullPage: false })
    const after = await page.evaluate(() => document.querySelectorAll('nav a, aside a').length)
    const visibleLabels = await page.evaluate(() =>
      [...document.querySelectorAll('nav a, aside a')]
        .filter((a) => a.getBoundingClientRect().height > 0)
        .map((a) => a.textContent?.trim()),
    )
    fs.writeFileSync(
      `${OUT}s3-admin-filter-effect.json`,
      JSON.stringify({ linksBefore: before, linksAfter: after, visibleLabels }, null, 1),
    )
    log(`[E3] filter effect: links ${before} -> ${after}; visible: ${JSON.stringify(visibleLabels)}`)
    await box.fill('')
  } else {
    log('[E3] admin filter input not found by placeholder — see s3-admin-filter.json inventory')
  }
}

// [E4] sidebar group titles in admin mode (Monitoring / Artifactory Settings …)
{
  const groups = await page.evaluate(() => {
    const nav = document.querySelector('nav, aside') ?? document.body
    return [...nav.querySelectorAll('li, [class*="group"], [class*="label"], [class*="section"]')]
      .map((el) => el.textContent?.trim().slice(0, 120))
      .filter((t) => t && !t.includes('http'))
      .slice(0, 60)
  })
  fs.writeFileSync(`${OUT}s4-sidebar-groups.json`, JSON.stringify(groups, null, 1))
  log('[E4] sidebar groups sample: ' + JSON.stringify(groups.slice(0, 20)))
}

await browser.close()
fs.writeFileSync(`${OUT}probe-evidence.txt`, lines.join('\n') + '\n')
console.log(lines.join('\n'))
