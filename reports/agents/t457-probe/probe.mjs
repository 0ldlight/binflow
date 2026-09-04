// T-457 read-only probe: Artifactory 7.161.20 (:8082) — help dropdown / About
// dialog / Profile (token + SSH) live form. Zero writes: dialogs opened,
// nothing saved; login only. Output: probe-evidence.txt + PNG + DOM dumps.
import { chromium } from '/Users/lzw/dev-center/web/node_modules/playwright/index.mjs'
import fs from 'node:fs'

const OUT = new URL('./', import.meta.url).pathname
const AF = 'http://localhost:8082'
const lines = []
const log = (s = '') => lines.push(s)

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })

// login
await page.goto(`${AF}/ui/login/`)
await page.fill('input[name="username"]', 'admin')
await page.fill('input[name="password"]', 'JFrog@2026')
await page.click('button[type="submit"]')
await page.waitForLoadState('networkidle')

// [E1] help dropdown in top bar
const helpBtn = page.locator('[data-cy="help-toggle"], [aria-label*="Help" i], button:has-text("?")').first()
log('[E1] help dropdown (top bar)')
let helpFound = false
for (const cand of await page.locator('button, a').all()) {
  const txt = (await cand.innerText().catch(() => '')).trim()
  if (txt === '?' || /help/i.test(txt)) {
    const attrs = await cand.evaluate((el) => ({
      tag: el.tagName,
      cy: el.getAttribute('data-cy'),
      aria: el.getAttribute('aria-label'),
      testid: el.getAttribute('data-testid'),
      title: el.getAttribute('title'),
      cls: el.className?.toString?.().slice(0, 80),
    }))
    log(`  candidate: ${JSON.stringify(attrs)} text="${txt.slice(0, 20)}"`)
  }
}
const tryOpenHelp = async (sel) => {
  const el = page.locator(sel).first()
  if (!(await el.count())) return false
  try {
    await el.click({ timeout: 3000 })
    await page.waitForTimeout(600)
    return true
  } catch {
    return false
  }
}
if (await tryOpenHelp('[data-cy="help-toggle"]')) helpFound = true
if (!helpFound) helpFound = await tryOpenHelp('button[title*="Help" i], [aria-label*="Help" i]')
if (!helpFound) {
  // last resort: any element whose visible glyph is exactly "?"
  const q = page.locator('span:text-is("?"), button:has(span:text-is("?"))').first()
  if (await q.count()) {
    await q.click().catch(() => {})
    await page.waitForTimeout(600)
    helpFound = true
  }
}
if (helpFound) {
  await page.screenshot({ path: `${OUT}s1-help-open.png` })
  const menu = page.locator('[role="menu"], .el-dropdown-menu, [data-cy*="help"]').first()
  const bodyText = await page.evaluate(() => document.body.innerText)
  const menuBlock = bodyText.split('\n').filter((l) => /Documentation|Training|Release Notes|About|Knowledge|JFrog/i.test(l))
  log('  open — menu-ish lines:')
  menuBlock.slice(0, 12).forEach((l) => log(`    | ${l.trim()}`))
  // menu links
  const links = await page.evaluate(() =>
    Array.from(document.querySelectorAll('[role="menu"] a, .el-dropdown-menu a, [role="menu"] li'))
      .map((el) => ({ text: el.textContent?.trim(), href: el.getAttribute('href') ?? null, cy: el.getAttribute('data-cy') }))
      .filter((x) => x.text),
  )
  log(`  menu entries: ${JSON.stringify(links, null, 1).slice(0, 1500)}`)
} else {
  log('  NOT FOUND: no ? help trigger matched — dump top bar text below')
  const bar = await page.evaluate(() => {
    const headers = Array.from(document.querySelectorAll('header, [class*="top"], [class*="header"]'))
    return headers.map((h) => h.innerText).join('\n---\n')
  })
  log(bar.slice(0, 800))
}

// [E2] About dialog
log('')
log('[E2] About dialog')
const aboutItem = page.locator('[role="menu"] :text("About"), .el-dropdown-menu :text("About"), li:has-text("About")').first()
if (await aboutItem.count()) {
  await aboutItem.click({ timeout: 3000 })
  await page.waitForTimeout(800)
  await page.screenshot({ path: `${OUT}s2-about.png` })
  const dlg = page.locator('[role="dialog"], .el-dialog').first()
  if (await dlg.count()) {
    const txt = (await dlg.innerText().catch(() => '')).trim()
    log('  dialog innerText (verbatim):')
    txt.split('\n').forEach((l) => log(`    | ${l}`))
    fs.writeFileSync(`${OUT}s2-about.dom.json`, JSON.stringify(await dlg.evaluate((el) => el.outerHTML), null, 1))
  }
} else {
  log('  NOT FOUND via menu — trying standalone About trigger')
}

// close any open dialog (About) — Escape, no button press
await page.keyboard.press('Escape').catch(() => {})
await page.waitForTimeout(400)

// [E3] Profile page (Edit Profile)
log('')
log('[E3] Profile page (Edit Profile)')
await page.goto(`${AF}/ui/`)
await page.waitForLoadState('networkidle')
await page.screenshot({ path: `${OUT}s0-home.png` })
fs.writeFileSync(`${OUT}s0-home.url.txt`, page.url())
// open user menu (avatar)
const avatar = page.locator('[data-cy="user-menu-toggle"], button[aria-label*="user" i], [class*="avatar"]').first()
if (await avatar.count()) {
  await avatar.click({ timeout: 3000 }).catch(() => {})
  await page.waitForTimeout(500)
  await page.screenshot({ path: `${OUT}s3-user-menu.png` })
  const menuText = await page.evaluate(() => document.body.innerText)
  log('  user menu lines:')
  menuText
    .split('\n')
    .filter((l) => /Profile|Token|SSH|Settings|Logout|Edit/i.test(l))
    .slice(0, 10)
    .forEach((l) => log(`    | ${l.trim()}`))
  const editProfile = page.locator(':text("Edit Profile")').first()
  if (await editProfile.count()) {
    await editProfile.click({ timeout: 3000 }).catch(() => {})
    await page.waitForLoadState('networkidle')
    await page.screenshot({ path: `${OUT}s4-profile.png`, fullPage: true })
    fs.writeFileSync(`${OUT}s4-profile.url.txt`, page.url())
    const ptxt = await page.evaluate(() => document.body.innerText)
    log(`  url: ${page.url()}`)
    log('  profile page text (verbatim, first 3000 chars):')
    ptxt.slice(0, 3000).split('\n').forEach((l) => log(`    | ${l}`))
  }
}

// [E4] Access Tokens page via profile nav if present (look for left menu entries)
log('')
log('[E4] profile sub-nav / token + ssh entries')
for (const label of ['Access Tokens', 'SSH Keys', 'Authentication Settings', 'Lock', 'Unlock']) {
  const hit = await page.locator(`:text-is("${label}")`).count()
  log(`  "${label}" on profile page: ${hit}`)
}

fs.writeFileSync(`${OUT}probe-evidence.txt`, lines.join('\n') + '\n')
await browser.close()
console.log('probe done')
