#!/usr/bin/env node
// Penpot Phase C — Playwright driver: login → project → install plugin → run → inject.
//
// Usage:
//   PENPOT_USER=... PENPOT_PASS=... node tools/penpot-sync/inject.mjs probe   # DOM discovery only
//   PENPOT_USER=... PENPOT_PASS=... node tools/penpot-sync/inject.mjs inject  # full pipeline
//   PENPOT_USER=... PENPOT_PASS=... node tools/penpot-sync/inject.mjs verify  # screenshots of result
//
// Credentials live in process.env only. The :8899 static server runs in-process
// for the lifetime of the command (serial lifecycle, killed on exit).
// Playwright resolves from web/node_modules; system Chrome channel preferred.

import { createRequire } from 'node:module';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import { listen } from './serve.mjs';

const require = createRequire(new URL('../../web/package.json', import.meta.url));
const { chromium } = require('playwright');

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const VERIFY = path.join(ROOT, 'tools/penpot-sync/verify');
fs.mkdirSync(VERIFY, { recursive: true });

const PENPOT = (process.env.PENPOT_URL || 'http://localhost:9001').replace(/\/$/, '');
const USER = process.env.PENPOT_USER;
const PASS = process.env.PENPOT_PASS;
const PROJECT = process.env.PENPOT_PROJECT || 'BinFlow UI Parity';
const PLUGIN_URL = process.env.PLUGIN_URL || 'http://localhost:8899/manifest.json';
const SPEC_URL = process.env.SPEC_URL || 'http://localhost:8899/penpot-spec.json';
if (!USER || !PASS) { console.error('PENPOT_USER / PENPOT_PASS env required'); process.exit(1); }

const MODE = process.argv[2] || 'probe';
const shot = (page, name) => page.screenshot({ path: path.join(VERIFY, `${name}.png`) }).catch(() => {});

async function launch() {
  try { return await chromium.launch({ headless: true, channel: 'chrome' }); }
  catch { return await chromium.launch({ headless: true }); }
}

async function login(page) {
  await page.goto(PENPOT + '/', { waitUntil: 'networkidle' });
  const submit = page.locator('[data-testid="login-submit"], button[type="submit"]').first();
  try {
    await submit.waitFor({ timeout: 20000 });
  } catch {
    // frontend sometimes renders slow/blank right after boot — one retry
    await page.goto(PENPOT + '/#/login', { waitUntil: 'networkidle' });
    await submit.waitFor({ timeout: 20000 });
  }
  const inputs = page.locator('input:visible');
  await inputs.nth(0).fill(USER);
  await inputs.nth(1).fill(PASS);
  await submit.click();
  await page.waitForFunction(() => !location.hash.includes('login') && !location.pathname.includes('login'), null, { timeout: 30000 });
  await page.waitForTimeout(2500);
}

const dump = (page, label) => page.evaluate((l) => {
  const els = [...document.querySelectorAll('button, a[href], input, [role="button"], h1, h2, [class*="title" i]')];
  return l + '\nURL: ' + location.href + '\n' + els.slice(0, 80).map((e) =>
    `${e.tagName.toLowerCase()} | text="${(e.innerText || e.value || e.placeholder || '').trim().slice(0, 60)}" | aria=${e.getAttribute('aria-label') || ''} | testid=${e.dataset.testId || e.getAttribute('data-testid') || ''}`
  ).join('\n');
}, label);

const server = await listen(8899);
const browser = await launch();
const ctx = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
const page = await ctx.newPage();
page.on('pageerror', (e) => console.log('[pageerror]', String(e).slice(0, 200)));
page.on('console', (m) => { if (m.type() === 'error') console.log('[console]', m.text().slice(0, 200)); });

try {
  await login(page);
  await shot(page, '01-after-login');
  console.log(await dump(page, '=== post-login ==='));

  if (MODE === 'probe') {
    console.log('probe discovery done — use inject/verify');
    process.exit(0);
  }

  // --- shared: open the target project + file workspace ---
  async function openWorkspace() {
    const projRe = new RegExp(PROJECT.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i');
    // The dashboard renders h2 project cards late (racy allInnerTexts left idx=-1
    // and nth(-1)=.last() clicking nothing) — poll for the card via locator filter.
    const projCard = page.locator('h2').filter({ hasText: projRe }).first();
    await projCard.waitFor({ timeout: 20000, state: 'visible' });
    if (MODE === 'inject') {
      // fresh canvas per run — click INTO the project first, then its "+ 新文档"
      await projCard.click();
      await page.waitForTimeout(3000);
      console.log('after project click:', page.url());
      if (!page.url().includes('/files?')) { await projCard.click().catch(() => {}); await page.waitForTimeout(3000); console.log('retry click:', page.url()); }
      if (process.env.PENPOT_FILES_URL && !page.url().includes('/files?')) {
        await page.goto(PENPOT + '/#' + process.env.PENPOT_FILES_URL.replace(/^#/, ''), { waitUntil: 'networkidle' }).catch(() => {});
        await page.waitForTimeout(3000);
        console.log('direct files URL:', page.url());
      }
      // project files view exposes new-file as <a>新文档</a> in the header (2.17);
      // the dashboard cards used data-testid=project-new-file
      const newFile = page.locator('a:has-text("+ 新文档"), [data-testid="project-new-file"]').first();
      await newFile.waitFor({ timeout: 15000, state: 'visible' });
      await newFile.click();
    } else {
      // verify: open the project's files view, dblclick the newest file
      await projCard.click();
      await page.waitForTimeout(2000);
      const fileCard = page.locator('div[aria-label]').filter({ hasText: /新建文件|ago/ }).first();
      await fileCard.waitFor({ timeout: 10000 });
      await fileCard.dblclick({ timeout: 5000 }).catch(() => {});
    }
    await page.waitForURL((u) => String(u).includes('file-id'), { timeout: 20000 });
    console.log('workspace:', page.url());
    await page.waitForTimeout(3000);
  }

  if (MODE === 'inject') {
    await openWorkspace();
    // plugins panel: install (first run) or reuse (already installed)
    await page.locator('[data-testid="plugins-btn"]').click();
    await page.waitForTimeout(1500);
    if (await page.locator('text=BinFlow Parity Sync').count()) {
      console.log('plugin already installed');
    } else {
      const urlInput = page.locator('input[aria-label*="URL"], input[placeholder*="URL"], input[aria-label*="插件"]').first();
      await urlInput.waitFor({ timeout: 10000 });
      await urlInput.fill(PLUGIN_URL);
      await page.locator('button').filter({ hasText: /安装|Install/ }).first().click();
      await page.waitForTimeout(4000);
      await shot(page, '05-plugin-installed');
      const allow = page.locator('input[value="允许"], button:has-text("允许"), [data-testid*="allow" i]');
      if (await allow.count()) {
        await allow.first().click();
        console.log('install permission granted');
      }
      await page.waitForTimeout(2000);
    }
    // run: plugins panel → 已安装的插件 → 打开
    const openBtn = page.locator('button:has-text("打开"), [data-testid*="open-plugin"]').first();
    await openBtn.waitFor({ timeout: 10000 });
    await openBtn.click();
    await page.waitForTimeout(2000);
    // run-time permission (asked per run)
    const allow2 = page.locator('input[value="允许"], button:has-text("允许")');
    if (await allow2.count()) {
      await allow2.first().click();
      console.log('run permission granted');
    }
    await page.waitForTimeout(3000);
    await shot(page, '06-plugin-modal');

    // plugin UI iframe (ui.html served from :8899)
    let uiFrame = null;
    for (let i = 0; i < 20 && !uiFrame; i++) {
      uiFrame = page.frames().find((f) => f.url().includes('ui.html')) || null;
      if (!uiFrame) await page.waitForTimeout(1000);
    }
    if (!uiFrame) {
      console.log('plugin UI iframe not found; frames:', page.frames().map((f) => f.url().slice(0, 120)));
      console.log(await dump(page, '=== no-iframe state ==='));
      process.exit(3);
    }
    await uiFrame.locator('#spec-url').waitFor({ timeout: 15000 });
    await uiFrame.locator('#spec-url').fill(SPEC_URL);
    // plain click stalls on "scheduled navigations" (Penpot rebuilds the iframe)
    await uiFrame.locator('#go').evaluate((el) => el.click());
    console.log('generation started', new Date().toISOString());

    const deadline = Date.now() + 25 * 60 * 1000;
    let final = null;
    while (Date.now() < deadline) {
      const status = (await uiFrame.locator('#status').innerText().catch(() => '')).trim();
      if (/^(DONE|ERROR)/.test(status)) { final = status; break; }
      await page.waitForTimeout(5000);
      process.stdout.write(`  [${Math.ceil((deadline - Date.now()) / 60000)}m left] ${status.slice(0, 100)}\n`);
    }
    console.log('FINAL STATUS:', final || 'TIMEOUT');
    await shot(page, '07-plugin-done');
    process.exit(final && final.startsWith('DONE') ? 0 : 2);
  }

  if (MODE === 'verify') {
    // find the file whose workspace contains our generated pages (newest first)
    const projRe = new RegExp(PROJECT.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i');
    await page.locator('h2').filter({ hasText: projRe }).first().click();
    await page.waitForTimeout(2000);
    const cards = page.locator('div[aria-label]').filter({ hasText: /新建文件|ago/ });
    const n = await cards.count();
    console.log('candidate files:', n);
    let opened = false;
    for (let i = 0; i < Math.min(n, 8) && !opened; i++) {
      await cards.nth(i).dblclick({ timeout: 5000 }).catch(() => {});
      await page.waitForURL((u) => String(u).includes('file-id'), { timeout: 10000 }).catch(() => {});
      await page.waitForTimeout(3000);
      if (await page.locator('text=Design Tokens').count()) { opened = true; console.log('generated file: card', i, page.url()); }
      else { await page.goBack(); await page.waitForTimeout(2000); }
    }
    if (!opened) { console.log('no generated file found'); process.exit(4); }

    await page.keyboard.press('Shift+Digit1');
    await page.waitForTimeout(1500);
    await shot(page, 'verify-00-workspace');
    console.log('pages in file:', await page.locator('[data-testid="page-item"], [data-testid="page-name"]').allInnerTexts().catch(() => []));
    for (const name of ['Design Tokens', 'Navigation', 'Repos']) {
      const item = page.locator(`text="${name}"`).first();
      if (await item.count()) {
        await item.click();
        await page.waitForTimeout(1500);
        await page.keyboard.press('Shift+Digit1');
        await page.waitForTimeout(1500);
        await shot(page, `verify-${name.replace(/[^A-Za-z]+/g, '-').toLowerCase()}`);
        console.log('captured', name);
      } else {
        console.log('MISSING page entry:', name);
      }
    }
    process.exit(0);
  }
} finally {
  await browser.close();
  server.close();
}
