#!/usr/bin/env node
// gen-brand-assets（FR-126 / T-389）——品牌 favicon / PWA PNG / manifest
// 的可复跑派生器。K56 生产件（web/src/assets/brand/*.svg）是唯一输入，
// 输出全部落 web/public/brand/（vite public 面，build 时由
// wire-brand-assets.mjs 搬入共享资产挂载并内容指纹化）。
//
// 本机无 rsvg-convert / magick（candidate-1/README §3 方案 A 不可用），
// 本脚本是该管线的等价实现（方案 B 同源思路）：playwright chromium
// headless 把 SVG 画上 canvas——PNG 直接 toBlob；favicon.ico 用 canvas
// ImageData 的 RGBA 在 Node 侧合成经典 32bit DIB 条目（BMP-in-ICO，
// 全平台浏览器支持面最宽的形态，不赌 PNG-in-ICO 的嗅探行为）。
//
// 派生纪律（candidate-1/README §3 逐条对齐）：
//   - favicon 16/32/48 用 mark.svg 单形（透明底——浏览器标签自供底色）；
//   - PWA 512 / apple 180 用 mark-dark 居中 + 8% 边距实底画布
//     （README「直接 rsvg 渲染会顶格」的边距条款；底色 = --bf-sidebar
//     亮色值 #1b2430——侧栏身份层恒深，双主题观感一致）；
//   - favicon.ico 收 16/32/48 三档；
//   - manifest.json 的 icon src 用稳定名（相对 manifest URL 解析），
//     wire-brand-assets.mjs 在 build 期改写为指纹名。
//
// 用法（手动复跑——换 logo 母版后）：
//   node scripts/gen-brand-assets.mjs [--samples <dir>] [--favicon-stroke 4.5]
//     --samples       额外输出 16px 样张（原尺寸 + 8x 最近邻放大）供
//                     「两箭可辨」人眼归档（BOARD AC1）
//     --favicon-stroke  仅 favicon 派生档的描边箭 stroke 覆写（README §3
//                     「16px 发糊允许 4→4.5」条款；母版不动）

import { Buffer } from 'node:buffer'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { chromium } from '@playwright/test'

const ROOT = new URL('..', import.meta.url).pathname
const BRAND = join(ROOT, 'src', 'assets', 'brand')
const OUT = join(ROOT, 'public', 'brand')

const args = process.argv.slice(2)
const sampleDir = args.includes('--samples') ? args[args.indexOf('--samples') + 1] : null
const strokeOverride = args.includes('--favicon-stroke') ? Number(args[args.indexOf('--favicon-stroke') + 1]) : null

/** 单形档：mark.svg 透明底直渲（favicon 三档） */
const MARK_SPECS = [16, 32, 48].map((size) => ({ name: `favicon-${size}.png`, size, margin: 0, dark: false }))
/** PWA 档：mark-dark + 8% 边距实底（180 apple / 512 manifest） */
const PWA_SPECS = [
  { name: 'apple-touch-icon.png', size: 180, margin: 0.08, dark: true },
  { name: 'icon-512.png', size: 512, margin: 0.08, dark: true },
]

// manifest 字段注记：
//   - start_url 用相对 "../ui/"：PWA 规范按 manifest URL 解析，落
//     /binflow/assets/ 后即 /binflow/ui/（SPA 段）。不用绝对字面量——
//     relink-assets 的自校验禁止 dist 下任何 "/binflow/ui/" 引号字面量
//     （那是给「死资产 URL」设的闸，app 路由引用走相对形合规且语义同）。
//   - scope 缺省 = start_url 的父目录（= /binflow/ui/），规范自带，不复制。
//   - theme/background = tokens.css 双主题值的 PWA 面（侧栏身份层恒深 /
//     内容底亮色）；display standalone = 控制台作为独立应用窗的形态。
const MANIFEST = {
  name: 'BinFlow Console',
  short_name: 'BinFlow',
  description: 'BinFlow 制品仓库控制台（单二进制，随实例交付）',
  start_url: '../ui/',
  display: 'standalone',
  background_color: '#f3f5f7',
  theme_color: '#1b2430',
  icons: [
    { src: 'apple-touch-icon.png', sizes: '180x180', type: 'image/png', purpose: 'any' },
    { src: 'icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
  ],
}

// ---- 页内侧：SVG → canvas，回传 RGBA + PNG bytes --------------------------------
//
// data URL 形态的 SVG 不污染 canvas（无外部引用），toBlob/getImageData
// 均可同步取值。绘制保持几何居中：边距档 = mark 占画布 84%（两侧各 8%）。
// 页内全局（document/Image/FileReader）一律经 globalThis 取用——evaluate
// 回调在浏览器侧执行，Node 侧 lint（scripts 全局面窄集）不认这些名字，
// 显式 globalThis 既保序列化自包含又零共享配置改动（seed-m8 同款纪律）。
async function renderAll(page, marks) {
  return page.evaluate(
    ({ light, dark, markSpecs, pwaSpecs, stroke }) => {
      const svgFor = (svg, wantStroke) =>
        wantStroke ? svg.replace('stroke-width="4"', `stroke-width="${wantStroke}"`) : svg
      const draw = (svg, spec) => {
        const canvas = globalThis.document.createElement('canvas')
        canvas.width = spec.size
        canvas.height = spec.size
        const ctx = canvas.getContext('2d', { willReadFrequently: true })
        if (spec.margin > 0) {
          ctx.fillStyle = '#1b2430'
          ctx.fillRect(0, 0, spec.size, spec.size)
        }
        const img = new globalThis.Image()
        img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg)
        // decode() 在 evaluate 内同步等待不可用——data URL 图片在设置
        // src 后的完整解码以 load 事件为准；此处借 Promise 化。
        return new Promise((resolve) => {
          img.onload = () => {
            const inner = spec.margin > 0 ? spec.size * (1 - 2 * spec.margin) : spec.size
            const off = (spec.size - inner) / 2
            ctx.drawImage(img, off, off, inner, inner)
            const { data } = ctx.getImageData(0, 0, spec.size, spec.size)
            canvas.toBlob((blob) => {
              const fr = new globalThis.FileReader()
              fr.onload = () => resolve({ name: spec.name, size: spec.size, rgba: Array.from(data), png: fr.result })
              fr.readAsDataURL(blob)
            }, 'image/png')
          }
        })
      }
      const jobs = [
        ...markSpecs.map((s) => draw(svgFor(light, stroke), s)),
        ...pwaSpecs.map((s) => draw(dark, s)),
      ]
      return Promise.all(jobs)
    },
    { light: marks.light, dark: marks.dark, markSpecs: MARK_SPECS, pwaSpecs: PWA_SPECS, stroke: strokeOverride },
  )
}

// ---- Node 侧：RGBA → 32bit BMP-in-ICO -------------------------------------------
//
// ICONDIR + ICONDIRENTRY（width/height 单字节，0 表示 256）+ 每档
// BITMAPINFOHEADER(biHeight = 2h：XOR+AND) + 自底向上 BGRA 行 + 全零
// AND 掩码（透明度由 alpha 承载；行宽按 32bit 对齐）。
function buildIco(images) {
  const maskRow = (w) => ((w + 31) >> 5) * 4
  const entry = (img) => {
    const { size } = img
    const px = size * size * 4
    const mask = maskRow(size) * size
    return 40 + px + mask
  }
  let offset = 6 + images.length * 16
  const total = offset + images.reduce((n, i) => n + entry(i), 0)
  const buf = Buffer.alloc(total)
  buf.writeUInt16LE(0, 0)
  buf.writeUInt16LE(1, 2)
  buf.writeUInt16LE(images.length, 4)
  images.forEach((img, i) => {
    const base = 6 + i * 16
    buf.writeUInt8(img.size >= 256 ? 0 : img.size, base)
    buf.writeUInt8(img.size >= 256 ? 0 : img.size, base + 1)
    buf.writeUInt8(0, base + 2)
    buf.writeUInt8(0, base + 3)
    buf.writeUInt16LE(1, base + 4)
    buf.writeUInt16LE(32, base + 6)
    buf.writeUInt32LE(entry(img), base + 8)
    buf.writeUInt32LE(offset, base + 12)
    // BITMAPINFOHEADER
    buf.writeUInt32LE(40, offset)
    buf.writeInt32LE(img.size, offset + 4)
    buf.writeInt32LE(img.size * 2, offset + 8)
    buf.writeUInt16LE(1, offset + 12)
    buf.writeUInt16LE(32, offset + 14)
    buf.writeUInt32LE(0, offset + 20) // biSizeImage：未压缩可置 0
    // 像素自底向上、RGBA→BGRA
    let p = offset + 40
    for (let y = img.size - 1; y >= 0; y--) {
      for (let x = 0; x < img.size; x++) {
        const k = (y * img.size + x) * 4
        buf.writeUInt8(img.rgba[k + 2], p++)
        buf.writeUInt8(img.rgba[k + 1], p++)
        buf.writeUInt8(img.rgba[k], p++)
        buf.writeUInt8(img.rgba[k + 3], p++)
      }
    }
    // AND 掩码：Buffer.alloc 已置零，跳写即可
    offset += entry(img)
  })
  return buf
}

// ---- 主流程 -----------------------------------------------------------------------

const [light, dark] = await Promise.all([
  readFile(join(BRAND, 'mark.svg'), 'utf8'),
  readFile(join(BRAND, 'mark-dark.svg'), 'utf8'),
])
await mkdir(OUT, { recursive: true })

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 512, height: 512 } })
const rendered = await renderAll(page, { light, dark })
await browser.close()

for (const r of rendered) {
  await writeFile(join(OUT, r.name), Buffer.from(r.png.split(',')[1], 'base64'))
  console.log(`png  ${r.name}  ${r.size}x${r.size}`)
}
const bySize = Object.fromEntries(rendered.map((r) => [r.size, r]))
const ico = buildIco([bySize[16], bySize[32], bySize[48]])
await writeFile(join(OUT, 'favicon.ico'), ico)
console.log(`ico  favicon.ico  16+32+48 DIB (${ico.length} bytes)`)
await writeFile(join(OUT, 'manifest.json'), JSON.stringify(MANIFEST, null, 2) + '\n')
console.log('json manifest.json')

if (sampleDir) {
  await mkdir(sampleDir, { recursive: true })
  await writeFile(join(sampleDir, 'favicon-16.png'), Buffer.from(bySize[16].png.split(',')[1], 'base64'))
  // 16px 样张的 8x 最近邻放大（两箭可辨的人眼归档面——BOARD AC1）
  const browser2 = await chromium.launch()
  const p2 = await browser2.newPage()
  const up = await p2.evaluate((dataUrl) => {
    const canvas = globalThis.document.createElement('canvas')
    canvas.width = 128
    canvas.height = 128
    const ctx = canvas.getContext('2d')
    ctx.imageSmoothingEnabled = false
    const img = new globalThis.Image()
    img.src = dataUrl
    return new Promise((resolve) => {
      img.onload = () => {
        ctx.drawImage(img, 0, 0, 128, 128)
        canvas.toBlob((b) => {
          const fr = new globalThis.FileReader()
          fr.onload = () => resolve(fr.result)
          fr.readAsDataURL(b)
        }, 'image/png')
      }
    })
  }, bySize[16].png)
  await browser2.close()
  await writeFile(join(sampleDir, 'favicon-16-at-8x.png'), Buffer.from(up.split(',')[1], 'base64'))
  console.log(`samples -> ${sampleDir}`)
}
