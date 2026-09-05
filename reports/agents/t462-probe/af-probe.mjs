// T-462 read-only probe: Artifactory 7.161.20 (:8082) — Maintenance page
// (GC cron fields / Cleanup families), Backups page (New Backup CRUD form),
// Import & Export page. Zero writes (INC-1): navigation + form-open only;
// no Save/Create/Run clicks; modals opened for field inventory are closed
// via Escape/reload.
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
  await page.evaluate(() => {
    document.querySelectorAll('.onboarding-main-wrapper, .welcome-wrapper').forEach((el) => el.remove())
  })
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${OUT}${name}.png`, fullPage: true })
  fs.writeFileSync(`${OUT}${name}.url.txt`, page.url())
  const text = await page.evaluate(() => document.body.innerText)
  fs.writeFileSync(`${OUT}${name}-text.txt`, text)
  log(`[${name}] url=${page.url()}`)
  log(`[${name}] text head:\n${text.slice(0, 1600)}\n----`)
  return text
}

// [A1] Maintenance page — GC cron slot family + Cleanup two families
await snapshot('a1-maintenance', `${AF}/ui/admin/artifactory/maintenance`)

// [A2] Backups page — list columns + New Backup button presence
await snapshot('a2-backups', `${AF}/ui/admin/artifactory/backups`)

// [A3] Import & Export page
await snapshot('a3-import-export', `${AF}/ui/admin/artifactory/import_export`)

// [A4] field inventory on maintenance page: every labeled input with its
// label text + placeholder + enabled state
{
  const fields = await page.evaluate(() => {
    const out = []
    const url = location.href
    for (const el of document.querySelectorAll('input, textarea, select')) {
      const r = el.getBoundingClientRect()
      if (r.width === 0 || r.height === 0) continue
      out.push({
        url,
        tag: el.tagName.toLowerCase(),
        type: el.getAttribute('type'),
        value: String(el.value ?? '').slice(0, 40),
        placeholder: el.getAttribute('placeholder'),
        disabled: el.disabled,
        ariaLabel: el.getAttribute('aria-label'),
      })
    }
    return out
  })
  fs.writeFileSync(`${OUT}a4-fields.json`, JSON.stringify(fields, null, 2))
}

// reopen maintenance for A4 context
await snapshot('a1-maintenance', `${AF}/ui/admin/artifactory/maintenance`)

// [A5] New Backup modal — open (no save), inventory fields, close
{
  try {
    const btn = page.getByRole('button', { name: /new backup/i }).first()
    await btn.waitFor({ state: 'visible', timeout: 10_000 })
    await btn.click()
    await page.waitForTimeout(1500)
    await page.screenshot({ path: `${OUT}a5-new-backup-modal.png`, fullPage: true })
    const text = await page.evaluate(() => document.body.innerText)
    fs.writeFileSync(`${OUT}a5-new-backup-modal-text.txt`, text)
    const fields = await page.evaluate(() => {
      const out = []
      for (const el of document.querySelectorAll('input, textarea, select')) {
        const r = el.getBoundingClientRect()
        if (r.width === 0 || r.height === 0) continue
        out.push({
          tag: el.tagName.toLowerCase(),
          type: el.getAttribute('type'),
          value: String(el.value ?? '').slice(0, 40),
          placeholder: el.getAttribute('placeholder'),
          disabled: el.disabled,
          ariaLabel: el.getAttribute('aria-label'),
        })
      }
      return out
    })
    fs.writeFileSync(`${OUT}a5-new-backup-fields.json`, JSON.stringify(fields, null, 2))
    log('[a5] new-backup modal inventoried')
    await page.keyboard.press('Escape')
  } catch (e) {
    log(`[a5] new-backup open failed: ${e.message.slice(0, 200)}`)
  }
}

fs.writeFileSync(`${OUT}af-probe-log.txt`, lines.join('\n'))
await browser.close()
console.log(lines.join('\n'))
