// 渲染验证脚本（B-1）：playwright chromium 截图 contact sheet → PNG。
// 运行：cd web && node ../docs/brand/explorations/render.mjs
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const { chromium } = createRequire(path.join(here, '..', '..', '..', 'web', 'package.json'))('playwright');
const out = (n) => path.join(here, n);

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 1200 }, deviceScaleFactor: 1 });
await page.goto('file://' + path.join(here, 'contact-sheet.html'));
await page.waitForTimeout(400);
const secs = page.locator('section');
await secs.nth(0).screenshot({ path: out('sheet-light.png') });
await secs.nth(1).screenshot({ path: out('sheet-dark.png') });
await page.screenshot({ path: out('contact-sheet.png'), fullPage: true });
await browser.close();
console.log('rendered: sheet-light.png / sheet-dark.png / contact-sheet.png');
