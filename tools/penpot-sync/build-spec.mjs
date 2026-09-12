#!/usr/bin/env node
// Penpot Phase B — build penpot-spec.json from Phase A parity-capture assets.
//
// Inputs (read-only):
//   docs/reverse/frontend/parity-capture/{tokens.json, nav-tree.json}
//   tools/penpot-sync/capture/crawl-manifest.json
//   docs/reverse/frontend/parity-capture/screenshots/{screens,dialogs,states}/
// Outputs:
//   tools/penpot-sync/penpot-spec.json     — consumed by penpot-plugin/code.js
//   tools/penpot-sync/penpot-catalog.json  — rebuilt slug→route catalog (manifest 全量)
//
// Spec schema (consumed by plugin, keep in sync with code.js):
//   { meta, pages: [{ name, boards: [{ name, x, y, w, h, meta, children: [node] }] }] }
//   node kinds:
//     { kind:'image', name, url, x, y, w, h }
//     { kind:'rect',  name, x, y, w, h, fill?, opacity?, stroke?, strokeWidth?, dash?, radius? }
//     { kind:'text',  name, x, y, w, h, text, fontSize, color, wrap? }
//   Coordinates of children are RELATIVE to their board.
//
// Run: node tools/penpot-sync/build-spec.mjs

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const CAP = path.join(ROOT, 'docs/reverse/frontend/parity-capture');
const MANIFEST = path.join(ROOT, 'tools/penpot-sync/capture/crawl-manifest.json');
const OUT_SPEC = path.join(ROOT, 'tools/penpot-sync/penpot-spec.json');
const OUT_CATALOG = path.join(ROOT, 'tools/penpot-sync/penpot-catalog.json');

// ---------------------------------------------------------------------------
// Layout constants (1440×900 artboards per crawl-manifest meta.viewport)
const VW = 1440, VH = 900;
const GRID_COLS = 2, GAP_X = 260, GAP_Y = 190; // GAP_Y room for caption + board title

// Annotation colors — taken from tokens.json palette (live-computed).
const C = {
  text: '#707070',     // body-text (all contexts)
  surface: '#f7f7f7',  // page background
  inputText: '#556274',
  danger: '#e93838',   // rgb(233,56,56) error/input-invalid border
  border: '#c9d0e3',   // rgb(201,208,227) input border
};
const ANN = {
  nav: C.danger,        // 导航区
  action: C.inputText,  // 主操作区 (topbar/toolbar)
  content: C.border,    // 表格/内容区
  overlay: C.text,      // 弹窗遮罩几何
};

// ---------------------------------------------------------------------------
// manifest → catalog + screen groups

function groupOf(s) {
  const p = s.path, slug = s.slug;
  if (['login', 'onboarding', 'quick-setup', 'notfound-404'].includes(slug)) return 'Auth & Shell';
  if (p.startsWith('/repos') || p.startsWith('/admin/repositories')) return 'Repos';
  if (['/packages', '/builds', '/artifactSearchResults', '/bundles/source'].includes(p)
    || p.startsWith('/artifactory/lifecycle') || p.startsWith('/artifactory/release-bundles')) return 'Artifacts & Release';
  if (p.startsWith('/admin/management') || p === '/user_profile') return 'Identity & Access';
  if (p.startsWith('/admin/configuration/security')) return 'Security Config';
  if (p.startsWith('/admin/configuration')) return 'Platform Config';
  if (p.startsWith('/admin/monitoring')) return 'Monitoring';
  if (p.startsWith('/admin/artifactory')) return 'Artifactory Services';
  return 'Other';
}

// App-shell screens keep the persistent shell (sidebar 256 + topbar 48 + toolbar ~56).
// Pre-auth screens get a centered card frame instead. Tree pages add a tree-panel zone.
const PREAUTH = new Set(['login', 'onboarding', 'quick-setup', 'notfound-404']);
const TREE_SCREENS = new Set(['tree-general', 'tree-empty-repo', 'artifact-search-results']);

const manifest = JSON.parse(fs.readFileSync(MANIFEST, 'utf8'));
const shotsDir = (sub) => path.join(CAP, 'screenshots', sub);
const pngExists = (sub, name) => fs.existsSync(path.join(shotsDir(sub), `${name}.png`));

// dialog PNG → owning screen slug (file names as captured in Phase A).
const DIALOG_OWNERS = {
  'deploy-dialog-open': 'tree-general',
  'group-form-filled': 'group-new',
  'pageform-audit-probe-empty-filled': 'repo-form-new-local',
  'permission-matrix-editor': 'permission-new',
  'permission-picker-step': 'permission-new',
  'set-me-up-configure': 'tree-general',
  'token-generate-form': 'access-tokens',
  'user-form-filled': 'user-new',
  'wizard-audit-probe-empty-3-name': 'repos-local',
  'wizard-audit-probe-empty-4-done': 'repos-local',
};
// state PNG → owning screen slug.
const STATE_OWNERS = {
  'empty-repo-tree': 'tree-empty-repo',
  'group-form-validation-error': 'group-new',
  'hover-primary-button': 'packages',
  'loading-users-list': 'users',
  'login-error': 'login',
  'login-error-after': 'login',
  'login-form-unexpected': 'login',
  'permission-denied-admin-route': 'users',
  'permission-save-result': 'permission-new',
  'route-404': 'notfound-404',
  'user-form-validation-error': 'user-new',
};
// extra PNG in screenshots/screens/ that is a variant state, not a manifest screen.
const EXTRA_SCREEN_OWNERS = {
  'permission-new-blank': 'permission-new',
};

const screens = manifest.screens.map((s) => ({
  slug: s.slug,
  route: s.path,
  effort: s.effort,
  group: groupOf(s),
  captured: pngExists('screens', s.slug),
  note: s.note || '',
}));

const bySlug = new Map(screens.map((s) => [s.slug, s]));
const dialogFiles = fs.readdirSync(shotsDir('dialogs')).filter((f) => f.endsWith('.png')).map((f) => f.replace(/\.png$/, ''));
const stateFiles = fs.readdirSync(shotsDir('states')).filter((f) => f.endsWith('.png')).map((f) => f.replace(/\.png$/, ''));

// Attach dialog/state evidence to screens.
for (const df of dialogFiles) {
  const owner = bySlug.get(DIALOG_OWNERS[df] || df.split('-')[0]);
  if (owner) (owner.dialogs ||= []).push(df);
}
for (const sf of stateFiles) {
  const owner = bySlug.get(STATE_OWNERS[sf]);
  if (owner) (owner.states ||= []).push(sf);
}
for (const [ef, ownerSlug] of Object.entries(EXTRA_SCREEN_OWNERS)) {
  const owner = bySlug.get(ownerSlug);
  if (owner && pngExists('screens', ef)) (owner.extras ||= []).push(ef);
}

// ---------------------------------------------------------------------------
// Node factories

const img = (name, url, x, y, w, h) => ({ kind: 'image', name, url, x, y, w, h });
const rect = (name, x, y, w, h, o = {}) => ({
  kind: 'rect', name, x, y, w, h,
  fill: o.fill ?? null, opacity: o.opacity ?? null,
  stroke: o.stroke ?? null, strokeWidth: o.strokeWidth ?? (o.stroke ? 2 : null),
  dash: o.dash ?? false, radius: o.radius ?? null,
});
const text = (name, x, y, w, txt, o = {}) => ({
  kind: 'text', name, x, y, w, h: o.h ?? 20, text: txt,
  fontSize: o.fontSize ?? 14, color: o.color ?? C.text, wrap: o.wrap ?? false,
});

// Zone helpers — dashed annotation frames over the screenshot bitmap.
const zone = (label, x, y, w, h, color) => ([
  rect(`zone:${label}`, x, y, w, h, { stroke: color, dash: true, strokeWidth: 2 }),
  text(`zone-label:${label}`, x + 6, y + 4, Math.max(120, w - 12), `${label} (${w}×${h})`, { fontSize: 11, color }),
]);

// ---------------------------------------------------------------------------
// Board builders

function screenBoard(s, x, y) {
  const children = [img('screenshot', `/img/screens/${s.slug}.png`, 0, 0, VW, VH)];
  if (PREAUTH.has(s.slug)) {
    // centered form card (login form width 375 evidenced in tokens.json)
    if (s.slug === 'login') {
      children.push(...zone('form-card', 532, 240, 375, 420, ANN.content));
    } else {
      children.push(...zone('content-card', 320, 150, 800, 600, ANN.content));
    }
  } else {
    children.push(...zone('nav-sidebar', 0, 0, 256, VH, ANN.nav));
    children.push(...zone('topbar', 256, 0, VW - 256, 48, ANN.action));
    children.push(...zone('toolbar-primary-actions', 256, 48, VW - 256, 56, ANN.action));
    children.push(...zone('content-table', 256, 104, VW - 256, VH - 104, ANN.content));
    if (TREE_SCREENS.has(s.slug)) {
      children.push(...zone('tree-panel', 256, 104, 424, VH - 104, ANN.content));
    }
  }
  children.push(text('board-meta', 0, -30, VW, s.meta, { fontSize: 12, wrap: true, h: 60 }));
  return { name: `${s.slug} — ${s.route}`, x, y, w: VW, h: VH, meta: s.meta, children };
}

function overlayBoard(kind, file, ownerSlug, x, y) {
  const url = kind === 'screens' ? `/img/screens/${file}.png` : `/img/${kind}/${file}.png`;
  const zoneLabel = kind === 'dialogs' ? 'dialog-overlay-approx' : 'state-fullpage';
  const children = [
    img('screenshot', url, 0, 0, VW, VH),
    ...(kind === 'dialogs'
      ? zone(zoneLabel, 480, 210, 480, 480, ANN.overlay)
      : zone(zoneLabel, 256, 104, VW - 256, VH - 104, ANN.content)),
    text('board-meta', 0, -30, VW, `${kind}/${file}.png — context: ${ownerSlug}`, { fontSize: 12, wrap: true, h: 40 }),
  ];
  return { name: `${kind}:${file}`, x, y, w: VW, h: VH, meta: `${kind} evidence for ${ownerSlug}`, children };
}

// ---------------------------------------------------------------------------
// Page 1 — Design Tokens (from tokens.json, deduped)

function tokensPage() {
  const b1 = { name: 'Tokens — Palette & Typography', x: 0, y: 0, w: VW, h: VH, meta: 'live-computed styles, contexts: set-me-up-dialog / permission-matrix / token-dialog / repositories-list / tree-page / user-form / loading', children: [] };
  const b2 = { name: 'Tokens — Spacing & Radii & Shadows', x: VW + GAP_X, y: 0, w: VW, h: VH, meta: 'input metrics from tokens.json spacing', children: [] };

  b1.children.push(
    text('t-title', 48, 40, 900, 'Artifactory 7.161.20 — computed palette (Phase A live sampling)', { fontSize: 24 }),
    text('t-sub', 48, 76, 900, 'source: docs/reverse/frontend/parity-capture/tokens.json — sampled across dialogs/lists/tree/forms', { fontSize: 12, color: C.border }),
  );
  // swatch grid 5 across
  const swatches = [
    { hex: '#707070', label: 'body-text\nall contexts' },
    { hex: '#f7f7f7', label: 'surface bg\npage/dialog' },
    { hex: '#556274', label: 'input-text\nforms' },
    { hex: '#e93838', label: 'error border\nrgb(233,56,56)' },
    { hex: '#c9d0e3', label: 'input border\nrgb(201,208,227)' },
  ];
  swatches.forEach((s, i) => {
    const sx = 48 + (i % 5) * 272, sy = 140;
    b1.children.push(
      rect(`sw-${s.hex}`, sx, sy, 240, 140, { fill: s.hex, stroke: C.border }),
      text(`sw-label-${s.hex}`, sx, sy + 150, 240, `${s.hex}\n${s.label}`, { fontSize: 12, wrap: true, h: 48 }),
    );
  });
  // typography samples
  b1.children.push(text('t-type', 48, 400, 900, 'Typography — Open Sans / Helvetica Neue / Arial', { fontSize: 20 }));
  const samples = [
    [24, '400'], [20, '400'], [16, '400'], [14, '400 — base body & input size (evidenced)'], [12, '400 — caption/table density'],
  ];
  samples.forEach(([sz, note], i) => {
    b1.children.push(text(`type-${sz}`, 48, 440 + i * 46, 1340, `The quick brown fox — ${sz}px ${note}`, { fontSize: sz }));
  });
  b1.children.push(text('t-type-note', 48, 690, 1340, 'fontFamily (computed): "Open Sans", "Helvetica Neue", Helvetica, Arial, sans-serif — weight 400 across all sampled contexts; no other weights evidenced in Phase A.', { fontSize: 12, wrap: true, h: 48, color: C.border }));

  b2.children.push(
    text('t-spacing-title', 48, 40, 900, 'Spacing & component metrics (input)', { fontSize: 24 }),
    rect('input-sample', 48, 110, 375, 40, { fill: null, stroke: C.border, radius: 4 }),
    text('input-sample-label', 48, 158, 700, 'input: 375×40, border 1px #c9d0e3, radius 4px, padding 0 12px — evidenced in tokens.json spacing', { fontSize: 12, wrap: true, h: 40 }),
    rect('input-error-sample', 48, 220, 375, 40, { fill: null, stroke: C.danger, radius: 4 }),
    text('input-error-label', 48, 268, 700, 'invalid input: border 1px #e93838 (set-me-up-dialog & token-dialog contexts)', { fontSize: 12, wrap: true, h: 40 }),
    rect('spacing-12', 48, 340, 24, 24, { fill: C.border }),
    text('spacing-label', 84, 342, 600, '12px — horizontal input padding (only spacing value evidenced)', { fontSize: 12 }),
    text('radius-title', 48, 420, 900, 'Radii', { fontSize: 20 }),
    rect('radius-4', 48, 460, 200, 80, { fill: null, stroke: C.border, radius: 4 }),
    text('radius-4-label', 48, 550, 400, '4px — inputs/buttons (borderRadius evidenced)', { fontSize: 12 }),
    rect('radius-0', 300, 460, 200, 80, { fill: null, stroke: C.border }),
    text('radius-0-label', 300, 550, 400, '0px — body/cards/panels (evidenced)', { fontSize: 12 }),
    text('shadow-title', 48, 620, 900, 'Shadows', { fontSize: 20 }),
    text('shadow-note', 48, 656, 1340, 'boxShadow: none across ALL sampled contexts (18 samples) — elevation is flat, hierarchy via borders/backgrounds.', { fontSize: 12, wrap: true, h: 32 }),
  );
  return { name: 'Design Tokens', boards: [b1, b2] };
}

// ---------------------------------------------------------------------------
// Page 2 — Navigation (geometric reconstruction from nav-tree.json + sidebar.html)

function navPage(navTree) {
  const navTexts = [];
  (function walk(n) { if (n.text) navTexts.push(n.text); (n.children || []).forEach(walk); })(navTree.sidebar || {});
  const topTexts = [];
  (function walk2(n) { if (n.text) topTexts.push(n.text); (n.children || []).forEach(walk2); })(navTree.topbar || {});
  const links = (navTree.links || []).map((l) => `${l.text || '(icon)'} → ${l.href}`).join('\n');

  const b = { name: 'Shell — sidebar + topbar (reconstruction)', x: 0, y: 0, w: VW, h: VH, meta: 'from nav-tree.json + dom-snapshots/sidebar.html', children: [] };
  // topbar
  b.children.push(
    rect('topbar-bg', 0, 0, VW, 48, { fill: C.surface, stroke: C.border }),
    rect('logo', 16, 8, 64, 32, { fill: null, stroke: C.inputText }),
    text('logo-label', 16, 48, 80, '', { fontSize: 10 }),
    text('tab-platform', 120, 14, 120, topTexts[0] || 'platform', { fontSize: 14 }),
    text('tab-administration', 240, 14, 200, topTexts[1] || 'administration', { fontSize: 14 }),
    text('topbar-links', VW - 500, 14, 480, 'packages · security · release-bundles · AI/ML · pipelines · integrations', { fontSize: 12, color: C.border }),
  );
  // sidebar
  b.children.push(rect('sidebar-bg', 0, 48, 256, VH - 48, { fill: C.surface, stroke: C.border }));
  const items = [
    [1, 'All Projects (selector)'],
    [1, 'Artifactory'], [2, 'Packages'], [2, 'Builds'], [2, 'Artifacts'], [2, 'Release Lifecycle'],
    [1, 'Xray'],
    [1, 'Distribution'], [2, 'Release Bundles'],
    [1, 'AI/ML'],
    [1, 'Pipelines'],
    [1, 'Integrations'], [2, 'Tools & Integrations'], [2, 'Webhooks'], [2, 'GraphQL Playground'],
  ];
  items.forEach(([lvl, label], i) => {
    const iy = 64 + i * 36;
    b.children.push(
      text(`nav-${label}`, 20 + (lvl - 1) * 20, iy, 216, label, { fontSize: lvl === 1 ? 14 : 13, color: lvl === 1 ? C.inputText : C.text }),
    );
  });
  b.children.push(
    text('nav-footer', 16, VH - 84, 224, 'JFrog Platform\nEnterprise Plus trial license\n7.161.20 rev 86120900\n© Copyright 2026 JFrog Ltd', { fontSize: 10, wrap: true, h: 70, color: C.border }),
  );
  // content placeholder + zone keys
  b.children.push(...zone('content', 256, 48, VW - 256, VH - 48, ANN.content));
  b.children.push(text('legend', 300, 80, 1000, 'annotation keys — red dashed: navigation zone · slate dashed: primary-action zone · light-blue dashed: content/table zone', { fontSize: 12, color: C.border }));
  b.children.push(text('links-meta', 0, -30, VW, `links: ${links.replace(/\n/g, ' | ')}`, { fontSize: 12, wrap: true, h: 40 }));
  return { name: 'Navigation', boards: [b] };
}

// ---------------------------------------------------------------------------
// Screen pages (grouped) + rebuilt catalog

const GROUP_ORDER = ['Artifacts & Release', 'Repos', 'Identity & Access', 'Security Config', 'Platform Config', 'Artifactory Services', 'Monitoring', 'Auth & Shell', 'Other'];

const pages = [tokensPage(), navPage(JSON.parse(fs.readFileSync(path.join(CAP, 'nav-tree.json'), 'utf8')))];

const dialogByOwner = new Map();
for (const df of dialogFiles) {
  const owner = DIALOG_OWNERS[df];
  if (!owner) continue;
  if (!dialogByOwner.has(owner)) dialogByOwner.set(owner, []);
  dialogByOwner.get(owner).push(df);
}
const stateByOwner = new Map();
for (const sf of stateFiles) {
  const owner = STATE_OWNERS[sf];
  if (!owner) continue;
  if (!stateByOwner.has(owner)) stateByOwner.set(owner, []);
  stateByOwner.get(owner).push(sf);
}

const captured = screens.filter((s) => s.captured);
const groupsWithCaptures = [];
for (const g of GROUP_ORDER) {
  const members = captured.filter((s) => s.group === g);
  if (!members.length) continue;
  groupsWithCaptures.push([g, members]);
}
// Groups that have zero captures still appear as empty pages? No — note them in catalog only.

let screenBoardsTotal = 0, overlayBoardsTotal = 0, imageNodes = 0;
for (const [g, members] of groupsWithCaptures) {
  const boards = [];
  // layout: screens in 2-col grid; each screen's dialogs/states get an extra board
  // stacked in the same column flow.
  const flow = []; // {board, captionRows}
  for (const s of members) {
    s.meta = [
      `route: /ui${s.route === '/login' ? '/login' : s.route}`,
      `group: ${s.group} · effort: ${s.effort}`,
      s.note ? `manifest note: ${s.note}` : null,
      s.dialogs?.length ? `dialogs: ${s.dialogs.join(', ')}` : null,
      s.states?.length ? `states: ${s.states.join(', ')}` : null,
      'zones: nav(red)/topbar+toolbar(slate)/content(lightblue) — geometry approximated from shell constants',
    ].filter(Boolean).join('\n');
    flow.push(screenBoard(s, 0, 0));
    screenBoardsTotal++;
    for (const df of s.dialogs || []) { flow.push(overlayBoard('dialogs', df, s.slug, 0, 0)); overlayBoardsTotal++; }
    for (const sf of s.states || []) { flow.push(overlayBoard('states', sf, s.slug, 0, 0)); overlayBoardsTotal++; }
    for (const ef of s.extras || []) { flow.push(overlayBoard('screens', ef, s.slug, 0, 0)); overlayBoardsTotal++; }
  }
  flow.forEach((bd, i) => {
    bd.x = (i % GRID_COLS) * (VW + GAP_X);
    bd.y = Math.floor(i / GRID_COLS) * (VH + GAP_Y);
    imageNodes += bd.children.filter((c) => c.kind === 'image').length;
    boards.push(bd);
  });
  pages.push({ name: g, boards });
}

// ---------------------------------------------------------------------------
// Write spec + catalog

const spec = {
  meta: {
    generatedAt: new Date().toISOString(),
    source: 'BinFlow penpot-sync Phase B — from docs/reverse/frontend/parity-capture (Phase A evidence)',
    viewport: { w: VW, h: VH },
    palette: C,
    annotation: ANN,
    counts: { pages: pages.length, boards: pages.reduce((n, p) => n + p.boards.length, 0), images: imageNodes },
  },
  pages,
};
fs.writeFileSync(OUT_SPEC, JSON.stringify(spec));
fs.writeFileSync(OUT_CATALOG, JSON.stringify({
  meta: {
    rebuiltAt: new Date().toISOString(),
    note: 'Phase B rebuild — manifest slug→route 全量 (crawl-manifest.json is the authority); capture status = PNG presence. Phase A screens-catalog.json only had 4 entries because the screens phase was chunked; this catalog restores the full mapping.',
    manifestScreens: screens.length,
    captured: captured.length,
    groupsWithCaptures: groupsWithCaptures.map(([g, m]) => `${g} (${m.length})`),
    groupsWithoutCaptures: GROUP_ORDER.filter((g) => !captured.some((s) => s.group === g)),
  },
  screens,
}, null, 2));

const totalBoards = pages.reduce((n, p) => n + p.boards.length, 0);
console.log(`penpot-spec.json: ${pages.length} pages, ${totalBoards} boards (${screenBoardsTotal} screens + ${overlayBoardsTotal} dialog/state overlays + 3 tokens/nav), ${imageNodes} image nodes, ${(fs.statSync(OUT_SPEC).size / 1e6).toFixed(1)} MB`);
console.log(`penpot-catalog.json: ${screens.length} manifest screens, ${captured.length} captured`);
