// 渲染验证（B-1）：无头光栅化每个方向 SVG → ① ASCII 像素图（48/16 两档，供设计走查）② 断言（双色在 16px 档均存活、覆盖率非零）。
// 运行：node docs/brand/explorations/verify.mjs [文件名…]
import { createRequire } from 'node:module';
import { readdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const { chromium } = createRequire(path.join(here, '..', '..', '..', 'web', 'package.json'))('playwright');

const args = process.argv.slice(2);
const files = args.length ? args : readdirSync(here).filter(f => /^d\d+.*\.svg$/.test(f)).sort();
const INK = [29, 34, 44], ACC = [61, 99, 242];

const near = (p, c, tol = 60) => Math.abs(p[0] - c[0]) + Math.abs(p[1] - c[1]) + Math.abs(p[2] - c[2]) < tol;
const lum = p => 0.2126 * p[0] + 0.7152 * p[1] + 0.0722 * p[2];

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 400, height: 400 } });
let failures = 0;
for (const f of files) {
  await page.goto('file://' + path.join(here, f));
  const res = await page.evaluate(async ({ size }) => {
    const svg = document.querySelector('svg');
    svg.setAttribute('width', size); svg.setAttribute('height', size);
    const blob = new Blob([svg.outerHTML], { type: 'image/svg+xml' });
    const url = URL.createObjectURL(blob);
    const img = new Image(); img.src = url;
    await new Promise(r => { img.onload = r; img.onerror = () => r('ERR'); });
    if (!img.complete || img.naturalWidth === 0) return { error: 'load-failed' };
    const c = document.createElementNS('http://www.w3.org/1999/xhtml','canvas'); c.width = size; c.height = size;
    const ctx = c.getContext('2d');
    ctx.drawImage(img, 0, 0, size, size);
    const d = ctx.getImageData(0, 0, size, size).data;
    const px = [];
    for (let i = 0; i < d.length; i += 4) px.push([d[i], d[i + 1], d[i + 2], d[i + 3]]);
    return { px, w: size };
  }, { size: 48 });
  if (res.error) { console.log(`${f}: LOAD FAILED`); failures++; continue; }
  const ascii = res.px.map(p => p[3] < 40 ? ' ' : near(p, INK) ? '#' : near(p, ACC) ? 'o' : (lum(p) > 160 ? '.' : '*')).join('');
  const counts = { ink: 0, acc: 0 };
  for (const p of res.px) { if (near(p, INK)) counts.ink++; if (near(p, ACC)) counts.acc++; }

  // 16px 档：抗锯齿后按色相聚类（蓝通道显著高于红 = accent；暗 = ink）
  const r16 = await page.evaluate(async () => {
    const svg = document.querySelector('svg');
    svg.setAttribute('width', 16); svg.setAttribute('height', 16);
    const blob = new Blob([svg.outerHTML], { type: 'image/svg+xml' });
    const url = URL.createObjectURL(blob);
    const img = new Image(); img.src = url;
    await new Promise(r => { img.onload = r; });
    const c = document.createElementNS('http://www.w3.org/1999/xhtml','canvas'); c.width = 16; c.height = 16;
    const ctx = c.getContext('2d'); ctx.drawImage(img, 0, 0, 16, 16);
    return ctx.getImageData(0, 0, 16, 16).data;
  });
  let ink16 = 0, acc16 = 0;
  const a16 = [];
  for (let i = 0; i < r16.length; i += 4) {
    const [R, G, B, A] = [r16[i], r16[i + 1], r16[i + 2], r16[i + 3]];
    const bg = A < 40 || lum([R, G, B]) > 200;
    const isAcc = B > 140 && B - R > 40;
    if (isAcc) acc16++; else if (!bg) ink16++;
    a16.push(bg ? '.' : isAcc ? 'o' : '#');
  }
  const ok = ink16 >= 12 && acc16 >= 6;
  if (!ok) failures++;
  console.log(`\n=== ${f} === 48px ink:${counts.ink}px acc:${counts.acc}px | 16px ink:${ink16} acc:${acc16} ${ok ? 'OK' : '<< 16px 弱'}`);
  for (let y = 0; y < 48; y += 1) console.log(ascii.slice(y * 48, y * 48 + 48));
  console.log('--- 16px ---');
  for (let y = 0; y < 16; y++) console.log(a16.slice(y * 16, y * 16 + 16).join(''));
}
await browser.close();
console.log(failures ? `\n${failures} file(s) flagged` : '\nall passed');
process.exit(0);
