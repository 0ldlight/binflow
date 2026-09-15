// 渲染验证（B-1 生产件）：brand/*.svg 光栅化 → ASCII 走查 + 断言（双色存活、自适应 media query 生效）。
// 运行：node docs/brand/verify-brand.mjs
import { createRequire } from 'node:module';
import { readdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const brandDir = path.join(here, '..', '..', 'brand');
const { chromium } = createRequire(path.join(here, '..', '..', 'web', 'package.json'))('playwright');

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 400, height: 400 } });
let failures = 0;

const raster = async (file, h, scheme = 'light') => {
  await page.emulateMedia({ colorScheme: scheme });
  await page.goto('file://' + path.join(brandDir, file));
  return page.evaluate(async (h) => {
    const svg = document.querySelector('svg');
    const vb = svg.viewBox.baseVal;
    const W = Math.round(h * vb.width / vb.height), H = h;
    svg.setAttribute('width', W); svg.setAttribute('height', H);
    const blob = new Blob([svg.outerHTML], { type: 'image/svg+xml' });
    const img = new Image(); img.src = URL.createObjectURL(blob);
    await new Promise(r => { img.onload = r; img.onerror = () => r('ERR'); });
    if (!img.complete || img.naturalWidth === 0) return null;
    const c = document.createElementNS('http://www.w3.org/1999/xhtml', 'canvas');
    c.width = W; c.height = H;
    const ctx = c.getContext('2d');
    ctx.drawImage(img, 0, 0, W, H);
    return { d: ctx.getImageData(0, 0, W, H).data, W, H };
  }, h);
};

const classify = (r, ds = 1) => {
  const rows = []; let acc = 0, ink = 0;
  for (let y = 0; y < r.H; y += ds) {
    let row = '';
    for (let x = 0; x < r.W; x += ds) {
      const i = (y * r.W + x) * 4;
      const [R, G, B, A] = [r.d[i], r.d[i + 1], r.d[i + 2], r.d[i + 3]];
      const L = 0.2126 * R + 0.7152 * G + 0.0722 * B;
      const solid = A > 90;
      if (solid && B > 130 && B - R > 35) { row += 'o'; acc++; }        // accent（蓝紫）
      else if (solid && (L < 120 || L > 165)) { row += L < 120 ? '#' : '+'; ink++; } // ink（深或浅）
      else if (solid) { row += '*'; ink++; }
      else row += '.';
    }
    rows.push(row);
  }
  return { rows, acc, ink };
};

const show = (title, map) => { console.log(`\n=== ${title} ink:${map.ink} acc:${map.acc} ===`); for (const r of map.rows) console.log(r); };

for (const f of readdirSync(brandDir).filter(x => x.endsWith('.svg')).sort()) {
  const isLockup = f.startsWith('logo') && f !== 'logo-mark.svg';
  const h = isLockup ? 24 : 48, ds = isLockup ? 2 : 1;

  const l = await raster(f, h, 'light');
  if (!l) { console.log(`${f}: LOAD FAILED`); failures++; continue; }
  show(`${f} [light ${l.W}x${l.H}]`, classify(l, ds));

  const l16 = await raster(f, isLockup ? 16 : 16, 'light');
  const m16 = classify(l16);
  const ok16 = m16.ink >= 8 && m16.acc >= 3;
  if (!ok16) failures++;
  console.log(`16px 档: ink:${m16.ink} acc:${m16.acc} ${ok16 ? 'OK' : '<< 弱'}`);
  if (!isLockup) for (const r of m16.rows) console.log(r);

  const d = await raster(f, h, 'dark');
  const dmap = classify(d, ds);
  if (!isLockup) show(`${f} [dark ${d.W}x${d.H}]`, dmap);
  const okDark = dmap.ink > 10 && dmap.acc > 3;
  if (!okDark) failures++;
  console.log(`dark 档: ink:${dmap.ink} acc:${dmap.acc} ${okDark ? 'OK（media query 生效）' : '<< 未生效/过弱'}`);
}
await browser.close();
console.log(failures ? `\n${failures} check(s) FAILED` : '\nALL PASSED');
process.exit(failures ? 1 : 0);
