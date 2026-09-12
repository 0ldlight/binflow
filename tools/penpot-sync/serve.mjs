#!/usr/bin/env node
// Static server for the Penpot plugin route: serves manifest/code/ui, the spec,
// and the Phase A screenshots as /img/*. Exported as a handler factory so
// inject.mjs can run it in-process (single serial command lifecycle).
//
// Run standalone: node tools/penpot-sync/serve.mjs [port]

import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const PLUGIN_DIR = path.join(ROOT, 'tools/penpot-sync/penpot-plugin');
const SHOTS = path.join(ROOT, 'docs/reverse/frontend/parity-capture/screenshots');

const ROUTES = new Map([
  ['/manifest.json', path.join(PLUGIN_DIR, 'manifest.json')],
  ['/code.js', path.join(PLUGIN_DIR, 'code.js')],
  ['/ui.html', path.join(PLUGIN_DIR, 'ui.html')],
  ['/penpot-spec.json', path.join(ROOT, 'tools/penpot-sync/penpot-spec.json')],
]);

const TYPES = { '.json': 'application/json', '.js': 'text/javascript', '.html': 'text/html', '.png': 'image/png' };

function resolveFile(urlPath) {
  const u = new URL(urlPath, 'http://x');
  const p = decodeURIComponent(u.pathname);
  if (ROUTES.has(p)) return ROUTES.get(p);
  const m = /^\/img\/(screens|dialogs|states)\/([A-Za-z0-9._-]+)\.png$/.exec(p);
  if (m && !m[2].includes('..')) return path.join(SHOTS, m[1], `${m[2]}.png`);
  return null;
}

// The Penpot plugin iframe's fetch is sanitized to text/json only (no
// arrayBuffer/blob) — binary screenshots must ship as base64 text.
function resolveB64(urlPath) {
  const m = /^\/b64\/(screens|dialogs|states)\/([A-Za-z0-9._-]+)\.png$/.exec(decodeURIComponent(new URL(urlPath, 'http://x').pathname));
  if (!m || m[2].includes('..')) return null;
  const file = path.join(SHOTS, m[1], `${m[2]}.png`);
  return fs.existsSync(file) ? fs.readFileSync(file).toString('base64') : null;
}

export function createHandler() {
  return (req, res) => {
    // CORS: the Penpot app (and plugin iframe with patched fetch sending
    // authorization) needs permissive headers + preflight support.
    if (req.method === 'OPTIONS') {
      res.writeHead(204, {
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Methods': 'GET, OPTIONS',
        'Access-Control-Allow-Headers': '*',
      });
      res.end();
      return;
    }
    try {
      const b64 = resolveB64(req.url);
      if (b64 !== null) {
        res.writeHead(200, {
          'Content-Type': 'text/plain; charset=utf-8',
          'Cache-Control': 'no-store',
          'Access-Control-Allow-Origin': '*',
          'Access-Control-Allow-Headers': '*',
        });
        res.end(b64);
        return;
      }
      const file = resolveFile(req.url);
      if (!file || !fs.existsSync(file)) {
        res.writeHead(404).end('not found');
        return;
      }
      res.writeHead(200, {
        'Content-Type': TYPES[path.extname(file)] || 'application/octet-stream',
        'Cache-Control': 'no-store',
        'Access-Control-Allow-Origin': '*',
        'Access-Control-Allow-Headers': '*',
      });
      fs.createReadStream(file).pipe(res);
    } catch (e) {
      res.writeHead(500).end(String(e));
    }
  };
}

export function listen(port = 8899) {
  return new Promise((resolve) => {
    const srv = http.createServer(createHandler());
    srv.listen(port, '127.0.0.1', () => resolve(srv));
  });
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const port = Number(process.argv[2] || 8899);
  listen(port).then(() => console.log(`penpot-sync static server on http://localhost:${port}`));
}
