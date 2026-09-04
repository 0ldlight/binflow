// T-457 probe round 2: help dropdown / About / user-menu / profile page.
import { chromium } from '/Users/lzw/dev-center/web/node_modules/playwright/index.mjs'
import fs from 'node:fs'

const OUT = '/Users/lzw/dev-center/reports/agents/t457-probe/'
const lines = []
const log = (s = '') => lines.push(s)

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
await page.goto('http://localhost:8082/ui/login/')
await page.fill('input[name="username"]', 'admin')
await page.fill('input[name="password"]', 'JFrog@2026')
await page.click('button[type="submit"]')
await page.waitForLoadState('networkidle')
await page.waitForTimeout(1500)

// [E1] help dropdown
log('[E1] help dropdown — click [aria-label="Get Help and Learn"]')
await page.keyboard.press("Escape"); await page.waitForTimeout(300); await page.click("button[aria-label=\"Get Help and Learn\"]", { force: true })
await page.waitForTimeout(800)
await page.screenshot({ path: `${OUT}s1-help-open.png` })
const dropLinks = await page.evaluate(() => {
  const menus = Array.from(document.querySelectorAll('.el-popper, [role="menu"], .el-dropdown-menu, .jfrog-help-menu')).filter((m) => m.getBoundingClientRect().width > 0)
  return menus.map((m) =>
    Array.from(m.querySelectorAll('a, li, [role="menuitem"]')).map((el) => ({
      tag: el.tagName,
      text: el.textContent?.replace(/\s+/g, ' ').trim().slice(0, 80),
      href: el.getAttribute('href'),
    })).filter((x) => x.text),
  )
})
log(JSON.stringify(dropLinks, null, 1))
log('body tail (menu visible):')
const bt = await page.evaluate(() => document.body.innerText)
bt.split('\n').slice(-25).forEach((l) => log(`  | ${l}`))

// [E2] About dialog from the menu
log('')
log('[E2] About dialog')
const about = page.locator('.el-popper:visible :text("About"), [role="menu"]:visible :text("About")').first()
if (await about.count()) {
  await about.click()
  await page.waitForTimeout(1200)
  await page.screenshot({ path: `${OUT}s2-about.png` })
  const dlg = page.locator('[role="dialog"], .el-dialog').first()
  if (await dlg.count()) {
    const txt = (await dlg.innerText().catch(() => '')).trim()
    log('  dialog innerText verbatim:')
    txt.split('\n').forEach((l) => log(`    | ${l}`))
    fs.writeFileSync(`${OUT}s2-about.dom.html`, await dlg.evaluate((el) => el.outerHTML))
  } else {
    log('  no [role=dialog] matched')
  }
} else {
  log('  About item not matched')
}
await page.keyboard.press('Escape').catch(() => {})
await page.waitForTimeout(400)
await page.keyboard.press('Escape').catch(() => {})

// [E3] user menu → Edit Profile
log('')
log('[E3] user menu → Edit Profile')
await page.click('button[aria-label="User Menu"]')
await page.waitForTimeout(700)
await page.screenshot({ path: `${OUT}s3-user-menu.png` })
const umText = await page.evaluate(() => {
  const menus = Array.from(document.querySelectorAll('.el-popper, [role="menu"]')).filter((m) => m.getBoundingClientRect().width > 0)
  return menus.map((m) => m.innerText)
})
log('  user menu innerText:')
umText.join('\n').split('\n').forEach((l) => log(`    | ${l.trim()}`))

const edit = page.locator('.el-popper:visible :text("Edit Profile"), [role="menu"]:visible :text("Edit Profile")').first()
if (await edit.count()) {
  await edit.click()
  await page.waitForLoadState('networkidle')
  await page.waitForTimeout(1200)
  await page.screenshot({ path: `${OUT}s4-profile.png`, fullPage: true })
  fs.writeFileSync(`${OUT}s4-profile.url.txt`, page.url())
  log(`  profile url: ${page.url()}`)
  const ptxt = await page.evaluate(() => document.body.innerText)
  log('  profile page innerText (first 2500):')
  ptxt.slice(0, 2500).split('\n').forEach((l) => log(`    | ${l}`))
} else {
  log('  Edit Profile not matched')
}

fs.writeFileSync(`${OUT}probe2-evidence.txt`, lines.join('\n') + '\n')
await browser.close()
console.log('probe2 done')
