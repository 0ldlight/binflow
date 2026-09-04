// T-457 probe round 3: dismiss onboarding by deep-linking into the
// Artifactory module, then help dropdown / About / user-menu / profile.
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
await page.waitForLoadState('networkidle')
await page.waitForTimeout(1500)
// leave the onboarding home → Artifactory module (same top bar)
await page.goto('http://localhost:8082/ui/artifactory/')
await page.waitForLoadState('networkidle')
await page.waitForTimeout(2000)
// the onboarding welcome overlay spans the viewport and intercepts pointer
// events — detach it in the probe DOM only (server state untouched)
await page.evaluate(() => {
  document.querySelectorAll('.onboarding-main-wrapper, .welcome-wrapper').forEach((el) => el.remove())
})
await page.waitForTimeout(300)
await page.screenshot({ path: `${OUT}s0b-artifactory-module.png` })
fs.writeFileSync(`${OUT}s0b.url.txt`, page.url())

// [E1] help dropdown
log('[E1] help dropdown — click [aria-label="Get Help and Learn"]')
await page.click('button[aria-label="Get Help and Learn"]')
await page.waitForTimeout(900)
await page.screenshot({ path: `${OUT}s1-help-open.png` })
const dropLinks = await page.evaluate(() => {
  const menus = Array.from(document.querySelectorAll('.el-popper, [role="menu"], .el-dropdown-menu')).filter((m) => m.getBoundingClientRect().width > 0 && m.getBoundingClientRect().height > 0)
  return menus.map((m) => ({
    cls: m.className.toString().slice(0, 60),
    items: Array.from(m.querySelectorAll('a, li, [role="menuitem"], button')).map((el) => ({
      tag: el.tagName,
      text: el.textContent?.replace(/\s+/g, ' ').trim().slice(0, 90),
      href: el.getAttribute('href'),
    })).filter((x) => x.text),
  }))
})
log(JSON.stringify(dropLinks, null, 1))

// [E2] About dialog
log('')
log('[E2] About dialog')
const about = page.getByText('About', { exact: true }).first()
if (await about.count()) {
  await about.click()
  await page.waitForTimeout(1400)
  await page.screenshot({ path: `${OUT}s2-about.png` })
  const dlg = page.locator('[role="dialog"], .el-dialog').first()
  if (await dlg.count()) {
    const txt = (await dlg.innerText().catch(() => '')).trim()
    log('  dialog innerText verbatim:')
    txt.split('\n').forEach((l) => log(`    | ${l}`))
    fs.writeFileSync(`${OUT}s2-about.dom.html`, await dlg.evaluate((el) => el.outerHTML))
  } else {
    log('  no [role=dialog] matched; body tail:')
    ;(await page.evaluate(() => document.body.innerText)).split('\n').slice(-20).forEach((l) => log(`    | ${l}`))
  }
} else {
  log('  About item not matched')
}
await page.keyboard.press('Escape').catch(() => {})
await page.waitForTimeout(600)

// [E3] user menu → Edit Profile
log('')
log('[E3] user menu → Edit Profile')
await page.click('button[aria-label="User Menu"]')
await page.waitForTimeout(900)
await page.screenshot({ path: `${OUT}s3-user-menu.png` })
const um = await page.evaluate(() => {
  const menus = Array.from(document.querySelectorAll('.el-popper, [role="menu"]')).filter((m) => m.getBoundingClientRect().width > 0)
  return menus.map((m) => ({ cls: m.className.toString().slice(0, 60), text: m.innerText }))
})
log('  user menu innerText:')
JSON.stringify(um).split('\\n').forEach((l) => log(`    ${l}`))

const edit = page.getByText('Edit Profile', { exact: true }).first()
if (await edit.count()) {
  await edit.click()
  await page.waitForLoadState('networkidle')
  await page.waitForTimeout(1500)
  await page.screenshot({ path: `${OUT}s4-profile.png`, fullPage: true })
  fs.writeFileSync(`${OUT}s4-profile.url.txt`, page.url())
  log(`  profile url: ${page.url()}`)
  const ptxt = await page.evaluate(() => document.body.innerText)
  log('  profile page innerText (first 3000):')
  ptxt.slice(0, 3000).split('\n').forEach((l) => log(`    | ${l}`))
} else {
  log('  Edit Profile not matched')
}

fs.writeFileSync(`${OUT}probe3-evidence.txt`, lines.join('\n') + '\n')
await browser.close()
console.log('probe3 done')
