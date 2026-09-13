// BinFlow Parity Sync — Penpot plugin (Phase C injector).
//
// Reads penpot-spec.json (Phase B output) from the plugin's own origin and
// materializes it: pages → boards → children (image bitmaps via uploadMediaData
// + fillImage fills, dashed annotation zone rects, metadata texts).
//
// API surface verified against penpot/penpot@develop plugin-api-test-suite
// (api-surface.json) and @penpot/plugin-types:
//   penpot.createPage() / openPage(page) / createBoard() / createRectangle()
//   penpot.createText(str) / uploadMediaData(name, Uint8Array, mime)
//   shape.fills = [{ fillOpacity, fillImage }] ; strokes dashed via strokeStyle
//
// UI protocol: ui.html posts {type:'generate', url} via parent.postMessage;
// progress flows back through penpot.ui.sendMessage.

/* global penpot */

const IMG_CACHE = new Map();
let BUSY = false;

penpot.ui.open('BinFlow Parity Sync', 'ui.html', { width: 420, height: 300 });

const log = (text) => penpot.ui.sendMessage({ type: 'log', text });
const done = (payload) => penpot.ui.sendMessage({ type: 'done', ...payload });

penpot.ui.onMessage(async (msg) => {
  if (!msg || typeof msg !== 'object') return;
  if (msg.type === 'close') { penpot.closePlugin(); return; }
  if (msg.type === 'generate' && !BUSY) {
    BUSY = true;
    try {
      await run(msg.url);
    } catch (e) {
      done({ ok: false, error: String(e && e.message ? e.message : e) });
    } finally {
      BUSY = false;
    }
  }
});

// ponytail: Penpot's plugin iframe replaces window.URL with a non-constructor
// stub — resolve root-relative URLs by string concat (all our URLs are either
// absolute or root-relative).
function absUrl(u, base) {
  if (/^https?:\/\//.test(u)) return u;
  const origin = base.replace(/^(https?:\/\/[^/]+).*$/, '$1');
  return origin + (u.startsWith('/') ? u : '/' + u);
}

async function fetchBytes(absImgUrl) {
  // plugin fetch response has .text() only → request the base64 flavor
  const b64Url = absImgUrl.replace('/img/', '/b64/');
  const res = await fetch(b64Url, { cache: 'no-store' });
  if (!res.ok) throw new Error(`fetch ${b64Url} → ${res.status}`);
  const b64 = (await res.text()).trim();
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes;
}

async function getImage(url, specBase) {
  if (IMG_CACHE.has(url)) return IMG_CACHE.get(url);
  const abs = absUrl(url, specBase);
  const name = abs.split('/').pop();
  let lastErr = null;
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const bytes = await fetchBytes(abs);
      const img = await penpot.uploadMediaData(name, bytes, 'image/png');
      IMG_CACHE.set(url, img);
      return img;
    } catch (e) {
      lastErr = e;
    }
  }
  throw new Error(`upload failed ${abs}: ${lastErr}`);
}

function applyNode(parent, node, specBase, errors, imageJobs) {
  const stage = (s) => errors.push(`[diag ${node.kind}:${node.name}] ${s}`);
  try {
  if (node.kind === 'image') {
    const r = penpot.createRectangle();
    if (!r) { stage('createRectangle null'); return; }
    parent.appendChild(r);
    r.name = node.name;
    r.x = node.x; r.y = node.y;
    r.resize(node.w, node.h);
    imageJobs.push({ rect: r, url: node.url });
    return;
  }
  if (node.kind === 'rect') {
    const r = penpot.createRectangle();
    parent.appendChild(r);
    r.name = node.name;
    r.x = node.x; r.y = node.y;
    r.resize(node.w, node.h);
    if (node.fill) r.fills = [{ fillColor: node.fill, fillOpacity: node.opacity ?? 1 }];
    else r.fills = [];
    if (node.stroke) {
      r.strokes = [{
        strokeColor: node.stroke,
        strokeWidth: node.strokeWidth ?? 2,
        strokeStyle: node.dash ? 'dashed' : 'solid',
        strokeAlignment: 'inner',
        strokeOpacity: 1,
      }];
    }
    if (node.radius) r.borderRadius = node.radius;
    return;
  }
  if (node.kind === 'text') {
    if (!node.text) return; // empty text shapes are rejected by the API
    const t = penpot.createText(node.text);
    if (!t) { stage('createText null'); return; }
    parent.appendChild(t);
    t.name = node.name;
    t.x = node.x; t.y = node.y;
    t.growType = 'auto-height';
    t.resize(Math.max(40, node.w), node.h || 20);
    t.fontSize = String(node.fontSize);
    t.fills = [{ fillColor: node.color, fillOpacity: 1 }];
    return;
  }
  errors.push(`unknown node kind: ${node.kind}`);
  } catch (e) {
    errors.push(`[node ${node.kind}:${node.name}] ${String(e && e.message ? e.message : e)}`);
  }
}

async function run(specUrl) {
  const t0 = Date.now();
  const spec = JSON.parse(await (await fetch(specUrl, { cache: 'no-store' })).text());
  const specBase = specUrl; // absUrl() only needs the origin
  const errors = [];
  let boardsDone = 0;
  const totalBoards = spec.pages.reduce((n, p) => n + p.boards.length, 0);
  log(`spec loaded: ${spec.pages.length} pages / ${totalBoards} boards`);

  for (const pageSpec of spec.pages) {
    const page = penpot.createPage();
    page.name = pageSpec.name;
    await penpot.openPage(page);
    const pageImageJobs = [];
    for (const bd of pageSpec.boards) {
      try {
        const board = penpot.createBoard();
        if (!board) throw new Error('createBoard null');
        board.name = bd.name;
        board.x = bd.x; board.y = bd.y;
        board.resize(bd.w, bd.h);
        for (const node of bd.children) applyNode(board, node, specBase, errors, pageImageJobs);
      } catch (e) {
        errors.push(`[board ${bd.name}] ${String(e && e.message ? e.message : e)}`);
      }
      boardsDone++;
    }
    log(`page "${pageSpec.name}": ${pageSpec.boards.length} boards — uploading ${pageImageJobs.length} images…`);
    // fills must be applied while THIS page is active; uploads serialized
    for (const job of pageImageJobs) {
      try {
        const img = await getImage(job.url, specBase);
        job.rect.fills = [{ fillOpacity: 1, fillImage: img }];
      } catch (e) {
        errors.push(String(e && e.message ? e.message : e));
        job.rect.fills = [{ fillColor: '#f7f7f7', fillOpacity: 1 }];
      }
    }
    log(`progress: ${boardsDone}/${totalBoards} boards done`);
  }

  done({
    ok: errors.length === 0,
    pages: spec.pages.length,
    boards: totalBoards,
    images: IMG_CACHE.size,
    errors: errors.length,
    errorSample: errors.slice(0, 5).join(' ;; '),
    seconds: Math.round((Date.now() - t0) / 1000),
  });
}
