#!/usr/bin/env node
// Penpot Phase A — Artifactory :8082 UI evidence capture (serial, single browser).
//
// Usage (run from repo root, Node >= 20):
//   ARTIFACTORY_USER=admin ARTIFACTORY_PASSWORD='…' node tools/penpot-sync/capture/capture.mjs
// Env:
//   ARTIFACTORY_URL       default http://localhost:8082
//   ARTIFACTORY_USER      required (never written to any output file)
//   ARTIFACTORY_PASSWORD  required (never written to any output file)
//   OUT_DIR               default docs/reverse/frontend/parity-capture
//   PHASES                comma list to run subset: preauth,login,nav,setup,screens,dialogs,tokens,states,catalog,cleanup
//   MAX_SHOTS             screenshot budget, default 140
//
// Playwright resolves from web/node_modules (1.62.x). Chromium binaries must exist
// (~/Library/Caches/ms-playwright); if missing: cd web && npx playwright install chromium.
//
// Credentials discipline: /ui/api/v1/* REJECTS basic auth (api-map.yaml E4) — login goes
// through the real UI form. REST fallback/cleanup calls hit /artifactory/api/* which DOES
// accept basic auth. Credentials live only in process.env and in-memory.
//
// Test data uses the audit-probe- prefix and is deleted in the cleanup phase (REST, admin).

import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import fs from 'node:fs';

const require = createRequire(new URL('../../../web/package.json', import.meta.url));
const { chromium } = require('playwright');

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '../../..');
const BASE = (process.env.ARTIFACTORY_URL || 'http://localhost:8082').replace(/\/$/, '');
const OUT = path.resolve(ROOT, process.env.OUT_DIR || 'docs/reverse/frontend/parity-capture');
const PHASES = (process.env.PHASES || 'preauth,login,nav,setup,screens,dialogs,tokens,states,catalog,cleanup').split(',');
const MAX_SHOTS = Number(process.env.MAX_SHOTS || 140);
// SCREENS_CHUNK=start:end (inclusive-exclusive) limits the screens phase to a
// slice — the reference JVM wedges under sustained MFE rendering (~35 screens
// in one go); run in chunks with cool-down gaps between invocations.
const CHUNK = (() => { const m = /(\d+):(\d+)/.exec(process.env.SCREENS_CHUNK || ''); return m ? [Number(m[1]), Number(m[2])] : null; })();

const PROBE = {
  repoEmpty: 'audit-probe-empty',
  repoContent: 'audit-probe-content',
  repoDel: 'audit-probe-del',
  user: 'audit-probe-user',
  // throwaway local-instance probe account, not a secret of value
  pass: 'AuditProbe-2026-local',
  group: 'audit-probe-group',
  perm: 'audit-probe-perm',
};

if (!process.env.ARTIFACTORY_USER || !process.env.ARTIFACTORY_PASSWORD) {
  console.error('ERROR: ARTIFACTORY_USER and ARTIFACTORY_PASSWORD must be provided via env.');
  process.exit(2);
}
const USER = process.env.ARTIFACTORY_USER;
const PASS = process.env.ARTIFACTORY_PASSWORD;

// ------------------------------------------------------------------ io helpers
const run = { startedAt: new Date().toISOString(), base: BASE, phases: [], summary: {} };
const gaps = [];            // -> states-gaps.md
const tokenSamples = [];    // -> tokens.json (written at finalize)
const catalog = [];         // -> screens-catalog.json
let shotCount = 0;
const fingerprints = new Map(); // fingerprint -> slug (same-form screen merge)

const log = (...a) => console.log('[capture]', ...a);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const slugFs = (s) => s.replace(/[^a-z0-9-]+/gi, '-').replace(/^-+|-+$/g, '').toLowerCase();

function ensure(p) { fs.mkdirSync(p, { recursive: true }); return p; }
const DIR = {
  screens: ensure(path.join(OUT, 'screenshots', 'screens')),
  dialogs: ensure(path.join(OUT, 'screenshots', 'dialogs')),
  states: ensure(path.join(OUT, 'screenshots', 'states')),
  dom: ensure(path.join(OUT, 'dom-snapshots')),
};
function writeJson(rel, obj) {
  fs.writeFileSync(path.join(OUT, rel), JSON.stringify(obj, null, 2));
}
function writeRunLog() {
  run.summary = {
    screenshots: shotCount,
    screens: catalog.length,
    duplicates: [...fingerprints.values()].length ? catalog.filter((c) => c.duplicateOf).length : 0,
    gaps: gaps.length,
    finishedAt: new Date().toISOString(),
  };
  writeJson('run-log.json', run);
}
function gap(id, why) { gaps.push({ id, why }); log('GAP:', id, '—', why); }

async function shot(page, dir, name, full = true) {
  if (shotCount >= MAX_SHOTS) { gap(`budget-${name}`, 'screenshot budget reached'); return null; }
  const file = path.join(dir, `${slugFs(name)}.png`);
  await page.screenshot({ path: file, fullPage: full });
  shotCount += 1;
  return path.relative(OUT, file);
}

// ------------------------------------------------------------------ page helpers
async function settle(page) {
  try { await page.waitForLoadState('load', { timeout: 20000 }); } catch { /* keep going */ }
  try { await page.waitForLoadState('networkidle', { timeout: 8000 }); } catch { /* MFE chunks keep ticking */ }
  await sleep(2000);
  // Serial MFE hammering wedged the reference JVM once (503 under load); gate
  // every settle on a cheap unauthenticated ping so the crawler pauses for
  // recovery instead of screenshotting error pages.
  for (let i = 0; i < 12; i++) {
    const code = await page.evaluate(async () => {
      try { const r = await fetch('/artifactory/api/system/ping', { cache: 'no-store' }); return r.status; }
      catch { return 0; }
    }).catch(() => 0);
    if (code === 200) break;
    await sleep(10000);
  }
}

async function firstVisible(page, selectors, scope = null) {
  for (const sel of selectors) {
    try {
      const loc = (scope ? scope.locator(sel) : page.locator(sel)).first();
      if ((await loc.count()) && (await loc.isVisible())) return loc;
    } catch { /* bad selector, try next */ }
  }
  return null;
}

async function clickText(page, re, scope = null) {
  const loc = (scope ? scope.getByRole('button') : page.getByRole('button')).filter({ name: re }).first();
  try {
    if ((await loc.count()) && (await loc.isVisible())) { await loc.click({ timeout: 4000 }); return true; }
  } catch { /* fallthrough */ }
  return false;
}

const domSummaryFn = () => {
  const vis = (el) => {
    const s = getComputedStyle(el); const r = el.getBoundingClientRect();
    return s.display !== 'none' && s.visibility !== 'hidden' && r.width > 0 && r.height > 0;
  };
  const txt = (el) => (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 80);
  const all = (sel) => [...document.querySelectorAll(sel)].filter(vis);
  const first = (sels) => { for (const s of sels) { const e = all(s); if (e.length) return e[0]; } return null; };
  return {
    url: location.href,
    title: (() => { const t = first(['h1', 'h2']); return t ? txt(t) : document.title; })(),
    headings: all('h1,h2,h3').map(txt).filter(Boolean).slice(0, 12),
    breadcrumbs: all('[class*="breadcrumb"] li, [class*="breadcrumb"] a').map(txt).filter(Boolean).slice(0, 8),
    buttons: all('button').map(txt).filter(Boolean).slice(0, 30),
    tabs: all('[role="tab"], .el-tabs__item, [class*="tabs_item"]').map(txt).filter(Boolean).slice(0, 20),
    columns: all('th, .ag-header-cell-text, [role="columnheader"]').map(txt).filter(Boolean).slice(0, 40),
    selected: all('[class*="active"], [class*="selected"], [aria-selected="true"]').map(txt).filter(Boolean).slice(0, 10),
    inputs: all('input').map((i) => i.placeholder || i.name || i.type).filter(Boolean).slice(0, 15),
  };
};

async function summarize(page) {
  try { return await page.evaluate(domSummaryFn); } catch (e) { return { error: String(e).slice(0, 200) }; }
}

function fingerprint(summary) {
  return [summary.title, (summary.tabs || []).join('|'), (summary.columns || []).join('|'), (summary.headings || []).join('|')].join('::').slice(0, 400);
}

async function captureScreen(page, entry) {
  const url = `${BASE}/ui${entry.path}`;
  const item = { slug: entry.slug, url, seedScreen: entry.seed ?? null, effort: entry.effort, screenshot: null, duplicateOf: null, domSummary: null, navNode: null, error: null };
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    const summary = await summarize(page);
    item.domSummary = summary;
    const fp = fingerprint(summary);
    if (fingerprints.has(fp) && entry.effort !== 'core') {
      item.duplicateOf = fingerprints.get(fp);
    } else {
      if (!fingerprints.has(fp)) fingerprints.set(fp, entry.slug);
      item.screenshot = await shot(page, DIR.screens, entry.slug);
    }
  } catch (e) {
    item.error = String(e).slice(0, 300);
    gap(`screen-${entry.slug}`, item.error);
  }
  catalog.push(item);
  return item;
}

// ------------------------------------------------------------------ login (UI form, per api-map E4: /ui/api/v1/* rejects basic auth)
async function uiLogin(page, user, pass) {
  await page.goto(`${BASE}/ui/login`, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await settle(page);
  // MFE login chunk renders late on 7.161.x (snapshot-confirmed boot shell) —
  // wait explicitly for a form field before probing candidates.
  try {
    await page.waitForSelector(
      'input[name="username"], input#user-name, input[autocomplete="username"], form input[type="text"], input[type="text"]',
      { timeout: 45000, state: 'visible' },
    );
  } catch { /* fall through to firstVisible probe + honest dump */ }
  if (!page.url().includes('/ui/login') && !page.url().endsWith('/login')) {
    // already authenticated or redirected — record and continue
    return { ok: true, note: `login page redirected to ${page.url()}` };
  }
  const userField = await firstVisible(page, [
    'input[name="username"]', 'input#user-name', 'input[autocomplete="username"]', 'form input[type="text"]', 'input[type="text"]',
  ]);
  const passField = await firstVisible(page, ['input#password-input', 'input[name="password"]', 'input[type="password"]']);
  if (!userField || !passField) {
    // possible SSO redirect — record honestly, do not force
    fs.writeFileSync(path.join(DIR.dom, 'login-form.html'), (await page.content()).slice(0, 500000));
    await shot(page, DIR.states, 'login-form-unexpected', false);
    return { ok: false, note: 'login form fields not found — dumped login-form.html (SSO variant?)' };
  }
  // Element Plus inputs need the native-setter + input-event flow (plain fill()
  // left the form invalid and submission no-op'd — verified against system
  // Chrome where this exact sequence logs in).
  const typeNative = async (el, value) => {
    await el.evaluate((node, v) => {
      const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
      setter.call(node, v);
      node.dispatchEvent(new Event('input', { bubbles: true }));
    }, value);
  };
  await typeNative(userField, user);
  await typeNative(passField, pass);
  const loginBtn = await firstVisible(page, ['button[type="submit"]', 'form button']);
  if (loginBtn) await loginBtn.click();
  else if (!(await clickText(page, /^log\s*in$/i)) && !(await clickText(page, /log\s*in$/i))) {
    return { ok: false, note: 'no submit button' };
  }
  await page.waitForURL((u) => !`${u}`.includes('/login'), { timeout: 20000 }).catch(() => {});
  await settle(page);
  const ok = !page.url().includes('/ui/login');
  return { ok, note: `landed on ${page.url()}` };
}

// ------------------------------------------------------------------ REST helpers (backend plane accepts basic auth)
function authHeader() { return 'Basic ' + Buffer.from(`${USER}:${PASS}`).toString('base64'); }
async function rest(method, apiPath, body) {
  const res = await fetch(`${BASE}/artifactory/api${apiPath}`, {
    method,
    headers: { Authorization: authHeader(), ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  return { status: res.status, text: (await res.text()).slice(0, 200) };
}

// ------------------------------------------------------------------ token sampling
const TOKEN_TARGETS = [
  { element: 'sidebar', sels: ['[class*="sidenav"]', '[class*="side-nav"]', 'aside', 'nav', '[class*="sidebar"]'] },
  { element: 'topbar', sels: ['header[class*="top"]', '[class*="top-bar"]', '[class*="topbar"]', 'header'] },
  { element: 'body-text', sels: ['body'] },
  { element: 'primary-button', sels: ['button[class*="primary"]', 'button[class*="main"]', '[class*="btn-primary"]'] },
  { element: 'secondary-button', sels: ['button[class*="secondary"]', '[class*="btn-secondary"]'] },
  { element: 'table-header', sels: ['th', '.ag-header-cell', '[class*="header-cell"]'] },
  { element: 'link', sels: ['a'] },
  { element: 'input', sels: ['input[type="text"]', 'input:not([type])'] },
  { element: 'card', sels: ['[class*="card"]'] },
  { element: 'mono', sels: ['pre', 'code', '[class*="mono"]'] },
  { element: 'error', sels: ['[class*="error"]', '.error'] },
  { element: 'success', sels: ['[class*="success"]'] },
  { element: 'warning', sels: ['[class*="warn"]'] },
];
async function sampleTokens(page, context) {
  const found = await page.evaluate((targets) => {
    const vis = (el) => { const s = getComputedStyle(el); const r = el.getBoundingClientRect(); return s.display !== 'none' && r.width > 0 && r.height > 0; };
    const out = [];
    for (const t of targets) {
      let el = null;
      for (const sel of t.sels) {
        const els = [...document.querySelectorAll(sel)].filter(vis);
        if (els.length) { el = els[0]; break; }
      }
      if (!el) continue;
      const cs = getComputedStyle(el); const r = el.getBoundingClientRect();
      out.push({
        element: t.element, selector: t.sels.find((s) => document.querySelector(s)) || t.sels[0],
        color: cs.color, backgroundColor: cs.backgroundColor,
        fontFamily: cs.fontFamily, fontSize: cs.fontSize, fontWeight: cs.fontWeight,
        borderRadius: cs.borderRadius, boxShadow: cs.boxShadow,
        border: `${cs.borderTopWidth} ${cs.borderTopStyle} ${cs.borderTopColor}`,
        padding: `${cs.paddingTop} ${cs.paddingRight} ${cs.paddingBottom} ${cs.paddingLeft}`,
        box: { w: Math.round(r.width), h: Math.round(r.height) },
      });
    }
    return out;
  }, TOKEN_TARGETS);
  for (const f of found) tokenSamples.push({ context, ...f });
}
const rgbHex = (v) => {
  const m = /rgba?\((\d+)[,\s]+(\d+)[,\s]+(\d+)(?:[,\s/]+([\d.]+))?\)/.exec(v || '');
  if (!m) return v || null;
  const h = (n) => Number(n).toString(16).padStart(2, '0');
  return `#${h(m[1])}${h(m[2])}${h(m[3])}${m[4] && m[4] !== '1' ? Math.round(m[4] * 255).toString(16).padStart(2, '0') : ''}`;
};

// ------------------------------------------------------------------ phases
async function phasePreAuth(context) {
  const page = await context.newPage();
  const p = { name: 'preauth', ok: true, items: [] };
  // login screen (clean, unauthenticated)
  await page.goto(`${BASE}/ui/login`, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await settle(page);
  const s = await shot(page, DIR.screens, 'login');
  p.items.push({ screen: 'login', screenshot: s });
  await sampleTokens(page, 'login-form');
  // login error state: exactly ONE wrong attempt (lockout-safe)
  const passField = await firstVisible(page, ['input#password-input', 'input[type="password"]']);
  const userField = await firstVisible(page, ['input[name="username"]', 'input[type="text"]']);
  if (passField && userField) {
    await userField.fill(USER);
    await passField.fill(`${PASS}-wrong-once-probe`);
    const s2 = await clickText(page, /log\s*in/i) ? await shot(page, DIR.states, 'login-error', false) : null;
    await sleep(1500);
    const s3 = await shot(page, DIR.states, 'login-error-after', false);
    p.items.push({ state: 'login-error', screenshot: s3 || s2 });
    await sampleTokens(page, 'login-error');
  } else {
    gap('login-error', 'login form not found for error-state capture');
  }
  run.phases.push(p);
  return page;
}

async function phaseLogin(prePage) {
  const p = { name: 'login', ok: true, items: [] };
  const page = prePage; // reuse pre-auth page (now showing error state) — fresh goto resets form
  const res = await uiLogin(page, USER, PASS);
  p.items.push(res);
  if (!res.ok) { p.ok = false; run.phases.push(p); throw new Error(`login failed: ${res.note}`); }
  // onboarding redirect?
  if (page.url().includes('onboarding')) {
    await settle(page);
    p.items.push({ onboarding: true, screenshot: await shot(page, DIR.screens, 'onboarding') });
    await clickText(page, /skip/i);
    await settle(page);
  }
  run.phases.push(p);
  return page;
}

async function phaseNav(page) {
  const p = { name: 'nav', ok: true, items: [] };
  // expand all collapsible nodes in the left rail (serial, capped)
  for (let i = 0; i < 80; i += 1) {
    let clicked = false;
    const handles = await page.$$('[aria-expanded="false"]');
    for (const h of handles) {
      try {
        const box = await h.boundingBox();
        if (box && box.x < 340 && box.width > 0) { await h.click(); await sleep(250); clicked = true; break; }
      } catch { /* stale, continue */ }
    }
    if (!clicked) break;
  }
  const nav = await page.evaluate(() => {
    const vis = (el) => { const s = getComputedStyle(el); const r = el.getBoundingClientRect(); return s.display !== 'none' && s.visibility !== 'hidden' && r.width > 0 && r.height > 0; };
    const directText = (el) => [...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent.trim()).join(' ').replace(/\s+/g, ' ').trim().slice(0, 60);
    const icon = (el) => { const i = el.querySelector(':scope > * svg, :scope > i, :scope > [class*="icon"]'); return i ? String(i.className.baseVal ?? i.className).slice(0, 80) : null; };
    const serialize = (el, depth) => {
      if (!el || depth > 9) return null;
      const kids = [...el.children].filter(vis).slice(0, 80);
      return {
        tag: el.tagName.toLowerCase(),
        cls: String(el.className?.baseVal ?? el.className ?? '').slice(0, 120),
        role: el.getAttribute('role'),
        text: directText(el) || undefined,
        aria: el.getAttribute('aria-label') || undefined,
        href: el.getAttribute('href') || undefined,
        expanded: el.getAttribute('aria-expanded') || undefined,
        icon: icon(el) || undefined,
        children: kids.map((k) => serialize(k, depth + 1)).filter(Boolean),
      };
    };
    const pickBy = (sels, test) => {
      let best = null; let bestScore = -1;
      for (const s of sels) {
        for (const el of [...document.querySelectorAll(s)].filter(vis)) {
          const r = el.getBoundingClientRect();
          const score = test(r) ? r.width * r.height : -1;
          if (score > bestScore) { best = el; bestScore = score; }
        }
      }
      return best;
    };
    const sidebar = pickBy(['[class*="sidenav"]', '[class*="side-nav"]', 'aside', 'nav', '[class*="sidebar"]'], (r) => r.left < 340 && r.height > 400);
    const topbar = pickBy(['[class*="top-bar"]', '[class*="topbar"]', 'header'], (r) => r.top < 220 && r.width > 700);
    const links = [...document.querySelectorAll('a[href*="/ui/"]')].filter(vis).map((a) => ({ text: directText(a) || a.getAttribute('aria-label'), href: a.getAttribute('href') })).slice(0, 300);
    return { url: location.href, sidebar: serialize(sidebar, 0), topbar: serialize(topbar, 0), links };
  });
  writeJson('nav-tree.json', nav);
  fs.writeFileSync(path.join(DIR.dom, 'sidebar.html'), (await page.evaluate(() => (document.querySelector('[class*="sidenav"], aside, nav, [class*="sidebar"]')?.outerHTML || '<!-- none -->'))).slice(0, 800000));
  fs.writeFileSync(path.join(DIR.dom, 'topbar.html'), (await page.evaluate(() => (document.querySelector('header')?.outerHTML || '<!-- none -->'))).slice(0, 400000));
  p.items.push({ links: (nav.links || []).length, hasSidebar: !!nav.sidebar, hasTopbar: !!nav.topbar });
  // remember link map for catalog navNode matching
  run.navLinks = nav.links || [];
  run.phases.push(p);
}

async function createRepoViaWizard(page, key, screenshots) {
  // Try the dialog wizard from the repositories list; fall back to the page form.
  await page.goto(`${BASE}/ui/admin/repositories/local`, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await settle(page);
  let wizard = false;
  if (await clickText(page, /new repository|create repository|new repo/i)) {
    wizard = true; await sleep(800);
    // step: package type — pick Generic
    const generic = page.getByText(/^generic$/i).first();
    if (await generic.isVisible().catch(() => false)) { if (screenshots) await shot(page, DIR.dialogs, `wizard-${key}-1-packagetype`); await generic.click(); await sleep(700); }
    // step: repo class — pick Local
    const local = page.getByText(/^local$/i).first();
    if (await local.isVisible().catch(() => false)) { if (screenshots) await shot(page, DIR.dialogs, `wizard-${key}-2-class`); await local.click(); await sleep(700); }
    // step: name
    const nameInput = await firstVisible(page, ['input[placeholder*="name" i]', 'input[type="text"]']);
    if (nameInput) await nameInput.fill(key);
    if (screenshots) await shot(page, DIR.dialogs, `wizard-${key}-3-name`);
    await clickText(page, /create (local )?repository|create/i);
    await sleep(2500);
    if (screenshots) await shot(page, DIR.dialogs, `wizard-${key}-4-done`);
    await page.keyboard.press('Escape'); await sleep(400);
  }
  if (!wizard) {
    // page-form fallback (7.x older flow): /admin/repositories/local/new
    await page.goto(`${BASE}/ui/admin/repositories/local/new`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    const nameInput = await firstVisible(page, ['input[placeholder*="name" i]', 'input[type="text"]']);
    if (nameInput) await nameInput.fill(key);
    if (screenshots) await shot(page, DIR.dialogs, `pageform-${key}-filled`);
    await clickText(page, /^create|save|^next$/i);
    await sleep(2000);
  }
  // verify via REST — single source of truth for "did it exist"
  const check = await rest('GET', `/repositories/${key}`);
  if (check.status !== 200) {
    const created = await rest('PUT', `/repositories/${key}`, { key, rclass: 'local', packageType: 'generic' });
    return { key, ui: 'rest-fallback', status: created.status };
  }
  return { key, ui: wizard ? 'wizard' : 'page-form', status: 200 };
}

async function createProbeUser(page) {
  await page.goto(`${BASE}/ui/admin/management/users/new`, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await settle(page);
  // empty-submit first → inline validation error state (screens.yaml user-form error)
  await clickText(page, /^save$|^create$|^next$/i);
  await sleep(900);
  await shot(page, DIR.states, 'user-form-validation-error');
  await sampleTokens(page, 'form-validation-error');
  // fill: name + email + password + retype (defensive candidates)
  const fields = {
    name: ['input[placeholder*="user name" i]', 'input[name*="name" i]', 'form input[type="text"]'],
    email: ['input[type="email"]', 'input[placeholder*="email" i]', 'input[name*="email" i]'],
    pass: ['input[type="password"]'],
  };
  const nameI = await firstVisible(page, fields.name); if (nameI) await nameI.fill(PROBE.user);
  const emailI = await firstVisible(page, fields.email); if (emailI) await emailI.fill(`${PROBE.user}@example.invalid`);
  const pw = await page.locator('input[type="password"]');
  const n = await pw.count();
  if (n >= 1) await pw.first().fill(PROBE.pass);
  if (n >= 2) await pw.nth(1).fill(PROBE.pass);
  await shot(page, DIR.dialogs, 'user-form-filled');
  await clickText(page, /^save$|^create$|^next$/i);
  await sleep(2000);
  const check = await rest('GET', `/security/users/${PROBE.user}`);
  if (check.status !== 200) {
    const r = await rest('PUT', `/security/users/${PROBE.user}`, { name: PROBE.user, email: `${PROBE.user}@example.invalid`, password: PROBE.pass, admin: false });
    return { user: PROBE.user, ui: 'rest-fallback', status: r.status };
  }
  return { user: PROBE.user, ui: 'form', status: 200 };
}

async function createProbeGroup(page) {
  await page.goto(`${BASE}/ui/admin/management/groups/new`, { waitUntil: 'domcontentloaded', timeout: 30000 });
  await settle(page);
  await clickText(page, /^save$|^create$|^next$/i);
  await sleep(900);
  await shot(page, DIR.states, 'group-form-validation-error');
  const nameI = await firstVisible(page, ['input[placeholder*="group" i]', 'input[type="text"]']);
  if (nameI) await nameI.fill(PROBE.group);
  await shot(page, DIR.dialogs, 'group-form-filled');
  await clickText(page, /^save$|^create$|^next$/i);
  await sleep(1500);
  const check = await rest('GET', `/security/groups/${PROBE.group}`);
  if (check.status !== 200) {
    const r = await rest('PUT', `/security/groups/${PROBE.group}`, { name: PROBE.group });
    return { group: PROBE.group, ui: 'rest-fallback', status: r.status };
  }
  return { group: PROBE.group, ui: 'form', status: 200 };
}

async function phaseSetup(page) {
  const p = { name: 'setup', ok: true, items: [] };
  p.items.push(await createRepoViaWizard(page, PROBE.repoEmpty, true));
  p.items.push(await createRepoViaWizard(page, PROBE.repoContent, false));
  p.items.push(await createRepoViaWizard(page, PROBE.repoDel, false));
  p.items.push(await createProbeUser(page));
  p.items.push(await createProbeGroup(page));
  // deploy one artifact into content repo via Deploy dialog (fills screens.yaml §8-7 success-state gap)
  try {
    await page.goto(`${BASE}/ui/repos/tree/General/${PROBE.repoContent}`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    const opened = await clickText(page, /^deploy$/i);
    await sleep(900);
    await shot(page, DIR.dialogs, 'deploy-dialog-open');
    const tmp = path.join(OUT, 'deploy-test.txt');
    fs.writeFileSync(tmp, `penpot phase-a probe artifact ${new Date().toISOString()}\n`);
    const fileInput = page.locator('input[type="file"]').first();
    if (opened && (await fileInput.count())) {
      await fileInput.setInputFiles(tmp);
      await sleep(700);
      await shot(page, DIR.dialogs, 'deploy-dialog-file-chosen');
      await clickText(page, /^deploy$|^upload$/i);
      await sleep(2500);
      await shot(page, DIR.states, 'deploy-success');
      await sampleTokens(page, 'deploy-success');
    } else {
      // explicit text-body PUT on the artifact plane (rest() is JSON-only)
      const res = await fetch(`${BASE}/artifactory/${PROBE.repoContent}/probe/deploy-test.txt`, { method: 'PUT', headers: { Authorization: authHeader() }, body: 'penpot phase-a probe artifact\n' });
      p.items.push({ deploy: 'rest-fallback', status: res.status });
      gap('deploy-dialog', 'Deploy dialog not openable via UI — REST fallback used, dialog form unrecorded');
    }
  } catch (e) { p.items.push({ deploy: 'error', error: String(e).slice(0, 200) }); gap('deploy-dialog', String(e).slice(0, 160)); }
  run.phases.push(p);
}

async function runScreens(page, manifest) {
  const screenEntries = CHUNK ? manifest.screens.slice(CHUNK[0], CHUNK[1]) : manifest.screens;
  for (const entry of screenEntries) {
    if (entry.slug === 'login' || entry.slug === 'onboarding') continue; // handled in preauth/login phases
    if (entry.effort === 'best' && shotCount >= MAX_SHOTS - 20) { gap(`screen-${entry.slug}`, 'budget guard skipped best-effort screen'); continue; }
    await captureScreen(page, entry);
    log('screen', entry.slug, entry.error ? 'ERR ' + entry.error.slice(0, 80) : 'ok');
  }
}

async function phaseDialogs(page) {
  const p = { name: 'dialogs', ok: true, items: [] };
  const closeDialog = async () => { await page.keyboard.press('Escape'); await sleep(400); const x = await firstVisible(page, ['[aria-label="Close"]', 'button[class*="close"]']); if (x) await x.click().catch(() => {}); await sleep(400); };

  // Set Me Up (tree page header) — Configure + Deploy tabs, token NOT generated
  try {
    await page.goto(`${BASE}/ui/repos/tree/General`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    if (await clickText(page, /set me up/i)) {
      await sleep(900);
      await shot(page, DIR.dialogs, 'set-me-up-configure');
      const depTab = page.getByRole('tab', { name: /deploy/i }).first();
      if (await depTab.isVisible().catch(() => false)) { await depTab.click(); await sleep(700); await shot(page, DIR.dialogs, 'set-me-up-deploy-tab'); }
      await sampleTokens(page, 'set-me-up-dialog');
      await closeDialog();
      p.items.push({ dialog: 'set-me-up', ok: true });
    } else { gap('set-me-up', 'Set Me Up button not found on tree page'); }
  } catch (e) { gap('set-me-up', String(e).slice(0, 160)); }

  // permission editor: two-step picker + matrix (console-ui §4-9)
  try {
    await page.goto(`${BASE}/ui/admin/management/permissions/new`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    await shot(page, DIR.screens, 'permission-new-blank');
    const nameI = await firstVisible(page, ['input[type="text"]']);
    if (nameI) await nameI.fill(PROBE.perm);
    const addBtn = await (async () => { for (const re of [/^add$/i, /add user/i, /add group/i, /users/i, /groups/i]) { if (await clickText(page, re)) return re.source; } return null; })();
    await sleep(900);
    await shot(page, DIR.dialogs, 'permission-picker-step');
    const pickInput = await firstVisible(page, ['input[type="text"]', 'input[type="search"]']);
    if (pickInput) { await pickInput.fill(PROBE.user); await sleep(700); }
    if (await clickText(page, /^next$|^ok$|^add$|^select$/i)) { await sleep(900); }
    await shot(page, DIR.dialogs, 'permission-matrix-editor');
    await sampleTokens(page, 'permission-matrix');
    // try to persist via UI (harmless probe data, cleaned later); REST fallback ensures existence
    await clickText(page, /^save$|^create$|^next$/i);
    await sleep(1500);
    await shot(page, DIR.states, 'permission-save-result');
    const check = await rest('GET', `/security/permissions/${PROBE.perm}`);
    if (check.status !== 200) {
      const r = await rest('PUT', `/security/permissions/${PROBE.perm}`, { name: PROBE.perm, repositories: [PROBE.repoContent], principals: { users: { [PROBE.user]: ['r'] } } });
      p.items.push({ dialog: 'permission-editor', persist: 'rest-fallback', status: r.status, opened: addBtn });
    } else p.items.push({ dialog: 'permission-editor', persist: 'ui', opened: addBtn });
  } catch (e) { gap('permission-editor', String(e).slice(0, 160)); }

  // token generate form (NOT submitted)
  try {
    await page.goto(`${BASE}/ui/admin/configuration/security/access_tokens`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    if (await clickText(page, /generate token/i)) {
      await sleep(900);
      await shot(page, DIR.dialogs, 'token-generate-form');
      await sampleTokens(page, 'token-dialog');
      await closeDialog();
      p.items.push({ dialog: 'token-generate', ok: true });
    } else gap('token-generate', 'Generate Token button not found');
  } catch (e) { gap('token-generate', String(e).slice(0, 160)); }

  // user menu dropdown (top-right)
  try {
    const acct = await firstVisible(page, ['[class*="user"] button', 'button[aria-label*="user" i]', '[data-test*="user"]', '[class*="account"]']);
    if (acct) {
      await acct.click(); await sleep(800);
      await shot(page, DIR.dialogs, 'user-menu-dropdown', false);
      await sampleTokens(page, 'user-menu');
      await page.keyboard.press('Escape');
      p.items.push({ dialog: 'user-menu', ok: true });
    } else gap('user-menu', 'account button not found');
  } catch (e) { gap('user-menu', String(e).slice(0, 160)); }

  // quick search overlay
  try {
    const search = await firstVisible(page, ['input[type="search"]', 'input[placeholder*="search" i]', 'input[class*="search"]']);
    if (search) {
      await search.click(); await search.fill('audit-probe'); await sleep(1200);
      await shot(page, DIR.dialogs, 'quick-search-overlay', false);
      await page.keyboard.press('Escape');
      p.items.push({ dialog: 'quick-search', ok: true });
    } else gap('quick-search', 'search input not found');
  } catch (e) { gap('quick-search', String(e).slice(0, 160)); }

  // tree context menu (right-click) on the probe artifact folder
  try {
    await page.goto(`${BASE}/ui/repos/tree/General/${PROBE.repoContent}`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    const node = await firstVisible(page, ['[class*="tree"] [class*="node"]', '[role="treeitem"]', '[class*="entity-name"]', '[class*="tree"] span']);
    if (node) {
      await node.click({ button: 'right' }); await sleep(800);
      await shot(page, DIR.dialogs, 'tree-context-menu', false);
      await page.keyboard.press('Escape');
      p.items.push({ dialog: 'tree-context-menu', ok: true });
    } else gap('tree-context-menu', 'tree node not found');
  } catch (e) { gap('tree-context-menu', String(e).slice(0, 160)); }

  // delete-confirm (danger pattern) — real delete of the throwaway repo
  try {
    await page.goto(`${BASE}/ui/admin/repositories/local`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    const row = page.locator('tr', { hasText: PROBE.repoDel }).first();
    if (await row.isVisible().catch(() => false)) {
      const del = row.getByRole('button', { name: /delete|trash|remove/i }).first();
      const alt = (await del.count()) ? del : row.locator('[class*="delete"], [class*="trash"], [class*="action"]').first();
      await alt.click({ timeout: 4000 });
      await sleep(900);
      await shot(page, DIR.dialogs, 'delete-confirm-dialog');
      await sampleTokens(page, 'delete-confirm');
      if (await clickText(page, /^delete$|^confirm$|^yes$/i)) { await sleep(2000); await shot(page, DIR.states, 'delete-after'); }
      const check = await rest('GET', `/repositories/${PROBE.repoDel}`);
      if (check.status === 200) { await rest('DELETE', `/repositories/${PROBE.repoDel}?deleteContent=true`); p.items.push({ dialog: 'delete-confirm', delete: 'rest-fallback' }); }
      else p.items.push({ dialog: 'delete-confirm', delete: 'ui' });
    } else gap('delete-confirm', 'probe repo row not visible in list');
  } catch (e) { gap('delete-confirm', String(e).slice(0, 160)); }

  run.phases.push(p);
}

async function phaseTokens(page) {
  const p = { name: 'tokens', ok: true, items: [] };
  const contexts = [
    ['repositories-list', '/admin/repositories/local'],
    ['tree-page', '/repos/tree/General'],
    ['user-form', '/admin/management/users/new'],
  ];
  for (const [ctx, pathUrl] of contexts) {
    try {
      await page.goto(`${BASE}/ui${pathUrl}`, { waitUntil: 'domcontentloaded', timeout: 30000 });
      await settle(page);
      await sampleTokens(page, ctx);
    } catch (e) { p.items.push({ context: ctx, error: String(e).slice(0, 160) }); }
  }
  // hover states
  try {
    const sidebarItem = await firstVisible(page, ['[class*="sidenav"] a', 'aside a', 'nav a', '[class*="sidebar"] a']);
    if (sidebarItem) {
      await sidebarItem.hover(); await sleep(500);
      await shot(page, DIR.states, 'hover-sidebar-item', false);
      await sampleTokens(page, 'hover-sidebar-item');
    } else gap('hover-sidebar', 'no sidebar link hoverable');
    const btn = await firstVisible(page, ['button[class*="primary"]', 'button']);
    if (btn) { await btn.hover(); await sleep(500); await shot(page, DIR.states, 'hover-primary-button', false); }
  } catch (e) { gap('hover', String(e).slice(0, 160)); }
  run.phases.push(p);
}

async function phaseStates(page, context) {
  const p = { name: 'states', ok: true, items: [] };
  // loading: throttle UI API plane, reload users list, shoot mid-flight
  try {
    const slow = (route) => setTimeout(() => { route.continue().catch(() => { /* route already handled (nav raced the 4s delay) */ }); }, 4000);
    await context.route('**/ui/api/v1/**', slow);
    await context.route('**/artifactory/api/**', slow);
    await page.goto(`${BASE}/ui/admin/management/users`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await sleep(900);
    await shot(page, DIR.states, 'loading-users-list', false);
    await sampleTokens(page, 'loading');
    await context.unroute('**/ui/api/v1/**', slow);
    await context.unroute('**/artifactory/api/**', slow);
    await settle(page);
  } catch (e) { gap('loading-state', String(e).slice(0, 160)); }
  // empty: empty repo tree + empty search results
  try {
    await page.goto(`${BASE}/ui/repos/tree/General/${PROBE.repoEmpty}`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    await shot(page, DIR.states, 'empty-repo-tree');
  } catch (e) { gap('empty-repo-tree', String(e).slice(0, 160)); }
  try {
    const search = await firstVisible(page, ['input[type="search"]', 'input[placeholder*="search" i]']);
    if (search) {
      await search.fill('zzz-no-such-probe-item');
      await page.keyboard.press('Enter');
      await sleep(1800);
      await shot(page, DIR.states, 'empty-search-results');
      await sampleTokens(page, 'empty-state');
    }
  } catch (e) { gap('empty-search', String(e).slice(0, 160)); }
  // 404
  try {
    await page.goto(`${BASE}/ui/__penpot_404_probe__`, { waitUntil: 'domcontentloaded', timeout: 30000 });
    await settle(page);
    await shot(page, DIR.states, 'route-404');
  } catch (e) { gap('route-404', String(e).slice(0, 160)); }
  // permission denied: fresh context as the non-admin probe user
  try {
    const c2 = await context.browser().newContext({ viewport: { width: 1440, height: 900 } });
    const p2 = await c2.newPage();
    const l = await uiLogin(p2, PROBE.user, PROBE.pass);
    if (l.ok) {
      await p2.goto(`${BASE}/ui/admin/management/users`, { waitUntil: 'domcontentloaded', timeout: 30000 });
      await settle(p2);
      await shot(p2, DIR.states, 'permission-denied-admin-route');
      writeJson('dom-snapshots/permission-denied-summary.json', await summarize(p2));
    } else gap('permission-denied', `probe user login failed: ${l.note}`);
    await c2.close();
  } catch (e) { gap('permission-denied', String(e).slice(0, 160)); }
  run.phases.push(p);
}

function phaseCatalog(manifest) {
  const p = { name: 'catalog', ok: true, items: [] };
  for (const item of catalog) {
    const link = (run.navLinks || []).find((l) => l && l.href && l.href.includes(item.url.replace(`${BASE}/ui`, '/ui').replace(/\/$/, '')));
    item.navNode = link ? link.text : null;
    item.isNewScreen = item.seedScreen === null && item.slug !== 'login';
  }
  // screens.yaml reconciliation
  const walkedSeed = ['login', 'onboarding', 'packages', 'artifacts-tree', 'artifact-search-results', 'repositories-list', 'repository-form', 'users-list', 'user-form', 'groups-list', 'group-form', 'permissions-list', 'permission-editor', 'maintenance', 'backups-list', 'backup-form', 'storage-summary', 'system-logs-viewer', 'repository-layouts', 'general-settings', 'ldap-config', 'import-export', 'access-tokens', 'user-profile', 'set-me-up-dialog', 'deploy-dialog'];
  const capturedSeeds = new Set(catalog.map((c) => c.seedScreen).filter(Boolean));
  const missingSeeds = walkedSeed.filter((s) => !capturedSeeds.has(s));
  const newScreens = catalog.filter((c) => c.isNewScreen).map((c) => c.slug);
  writeJson('screens-catalog.json', {
    meta: { base: BASE, manifestScreens: manifest.screens.length, captured: catalog.length, newScreens, missingSeeds, note: 'seed = docs/reverse/frontend/screens.yaml id; isNewScreen = live route with no seed mapping' },
    screens: catalog,
    dialogsCaptured: manifest.dialogs.filter((d) => !gaps.some((g) => g.id === d.id)),
    dialogsGapped: manifest.dialogs.filter((d) => gaps.some((g) => g.id === d.id)).map((d) => d.id),
  });
  p.items.push({ captured: catalog.length, newScreens: newScreens.length, missingSeeds });
  run.phases.push(p);
}

async function phaseCleanup() {
  const p = { name: 'cleanup', ok: true, items: [] };
  const tries = [
    ['permission', () => rest('DELETE', `/security/permissions/${PROBE.perm}`)],
    ['group', () => rest('DELETE', `/security/groups/${PROBE.group}`)],
    ['user', () => rest('DELETE', `/security/users/${PROBE.user}`)],
    ['repo-content', () => rest('DELETE', `/repositories/${PROBE.repoContent}?deleteContent=true`)],
    ['repo-empty', () => rest('DELETE', `/repositories/${PROBE.repoEmpty}?deleteContent=true`)],
    ['repo-del', () => rest('DELETE', `/repositories/${PROBE.repoDel}?deleteContent=true`)],
  ];
  for (const [name, f] of tries) {
    try {
      const r = await f();
      p.items.push({ [name]: r.status }); // 404 = already gone, fine
    } catch (e) { p.items.push({ [name]: `error ${String(e).slice(0, 120)}` }); p.ok = false; }
  }
  // verify nothing audit-probe-* remains
  try {
    const res = await fetch(`${BASE}/artifactory/api/repositories`, { headers: { Authorization: authHeader() } });
    const list = await res.json();
    const left = (Array.isArray(list) ? list : []).filter((r) => String(r.key || '').startsWith('audit-probe-'));
    p.items.push({ probeReposRemaining: left.map((r) => r.key) });
    if (left.length) p.ok = false;
  } catch { /* non-fatal */ }
  run.phases.push(p);
}

function finalize(manifest) {
  // tokens.json
  const byCtx = {};
  for (const t of tokenSamples) {
    byCtx[t.context] = byCtx[t.context] || [];
    byCtx[t.context].push(t);
  }
  writeJson('tokens.json', {
    meta: { base: BASE, note: 'computed styles sampled from live 7.161.20 UI; context = page/dialog the element was found on' },
    palette: tokenSamples.map((t) => ({ context: t.context, element: t.element, color: rgbHex(t.color), backgroundColor: rgbHex(t.backgroundColor), border: t.border })),
    typography: tokenSamples.map((t) => ({ context: t.context, element: t.element, fontFamily: t.fontFamily, fontSize: t.fontSize, fontWeight: t.fontWeight })),
    radii: tokenSamples.map((t) => ({ context: t.context, element: t.element, borderRadius: t.borderRadius })),
    shadows: tokenSamples.map((t) => ({ context: t.context, element: t.element, boxShadow: t.boxShadow })),
    spacing: tokenSamples.map((t) => ({ context: t.context, element: t.element, padding: t.padding, box: t.box })),
    raw: byCtx,
  });
  // states-gaps.md
  const staticGaps = [
    ['dark-mode', 'instance theme is light; dark variant not captured this phase'],
    ['toast-family', 'only deploy/delete/save toasts naturally triggered are captured; exhaustive toast inventory out of reach'],
    ['tree-virtual-scroll-loading', 'large-directory lazy-load needs >2k nodes to force; only throttled-list loading captured'],
    ['api-failure-error-pages', 'server-side API failures (5xx mid-session) not injectable without breaking the reference instance — out of scope by directive'],
    ['sso-login-variants', 'SSO/MFA login redirect forms not exercised (instance uses internal auth)'],
    ['pro-inner-flows', 'retention policy create/save, lifecycle promotion flows opened but not driven end-to-end'],
  ];
  const all = [...gaps.map((g) => ({ id: g.id, why: g.why })), ...staticGaps.map(([id, why]) => ({ id, why }))];
  const md = ['# Parity Capture — State/Interaction Gaps (Phase A)\n\n',
    'Entries the crawler could NOT naturally reach. Feed these into the next walkthrough ticket.\n\n',
    '| id | why not captured |\n|---|---|\n',
    ...all.map((g) => `| ${g.id} | ${g.why} |\n`)].join('');
  fs.writeFileSync(path.join(OUT, 'states-gaps.md'), md);
  phaseCatalog(manifest);
  writeRunLog();
  log('DONE — screenshots:', shotCount, 'screens:', catalog.length, 'gaps:', all.length, '->', OUT);
}

// ------------------------------------------------------------------ main (strictly serial)
async function main() {
  const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, 'crawl-manifest.json'), 'utf8'));
  // Bundled-headless Chromium stalls on the 7.161 login MFE (boot shell forever,
  // verified against system Chrome where the form renders in ~8s) — prefer the
  // system Chrome channel; fall back to bundled chromium if absent.
  let browser;
  try {
    browser = await chromium.launch({ headless: true, channel: 'chrome' });
  } catch {
    browser = await chromium.launch({ headless: true });
  }
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  let page = null;
  const isolated = async (name, fn) => {
    if (!PHASES.includes(name)) return;
    try {
      await fn();
    } catch (e) {
      run.phases.push({ name, ok: false, error: String(e).slice(0, 400) });
      console.error('[capture] phase failed:', name, String(e).slice(0, 300));
    }
  };
  try {
    if (PHASES.includes('preauth')) {
      try { page = await phasePreAuth(context); } catch (e) { run.phases.push({ name: 'preauth', ok: false, error: String(e).slice(0, 400) }); page = await context.newPage(); }
    } else page = await context.newPage();
    let authed = true;
    if (PHASES.includes('login')) {
      try { await phaseLogin(page); } catch (e) { authed = false; run.phases.push({ name: 'login', ok: false, error: String(e).slice(0, 400) }); }
    }
    if (authed) {
      await isolated('nav', () => phaseNav(page));
      await isolated('setup', () => phaseSetup(page));
      await isolated('screens', () => runScreens(page, manifest));
      await isolated('dialogs', () => phaseDialogs(page));
      await isolated('tokens', () => phaseTokens(page));
      await isolated('states', () => phaseStates(page, context));
    } else {
      run.fatal = 'login failed — authenticated phases skipped (see dom-snapshots/login-form.html)';
      console.error('[capture] FATAL:', run.fatal);
    }
  } finally {
    try { if (PHASES.includes('cleanup')) await phaseCleanup(); } catch (e) { run.cleanupError = String(e).slice(0, 300); }
    finalize(manifest);
    await browser.close();
  }
  process.exit(run.fatal ? 1 : 0);
}
main();
