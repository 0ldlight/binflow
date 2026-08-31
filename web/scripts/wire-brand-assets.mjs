#!/usr/bin/env node
// wire-brand-assets（FR-126 / T-389）——favicon / PWA PNG / manifest 的
// build 期搬挂 + 内容指纹化。挂在 `npm run build` 的 relink-assets 之后。
//
// 为什么必须搬：vite public 面（web/public/brand/**）build 后落在
// dist/brand/**，URL = base 前缀的 /binflow/ui/brand/**——而 ui 段是
// 纯 SPA shell（internal/console serveShell 无文件特例，ADR-0014 T-108
// errata 的「dumb fallback」刻意设计），public 原位永远取不到字节。
// 唯一可服务的静态面是共享资产挂载 /binflow/assets/<扁平名>。
//
// 为什么指纹化：serveAsset 对整个挂载一律
// `Cache-Control: public, max-age=31536000, immutable`（「Names are
// content-fingerprinted by the build」是挂载契约的前半句）。favicon /
// PWA 图标若以稳定名入住，换 logo 母版后老客户端会把旧图标钉死一年。
// 因此本步按内容 sha1 重命名（favicon-16-<h8>.png），manifest 的
// icons[].src 一并改写后再整体指纹化——immutable 语义保持诚实，且
// 不需要动服务端（纯 FE 票红线：*.go diff = 0）。
//
// 自校验（与 relink-assets 同姿势）：任一品牌 href 未命中映射、或
// dist/brand/ 残留、或目标文件缺席 → 非零退出，绝不带死 URL 出门。
import { Buffer } from 'node:buffer'
import { createHash } from 'node:crypto'
import { mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

const dist = new URL('../dist', import.meta.url).pathname
const brandDir = join(dist, 'brand')
const assetsDir = join(dist, 'assets')
const htmlPath = join(dist, 'index.html')

const FROM_PREFIX = '/binflow/ui/brand/'
const TO_PREFIX = '/binflow/assets/'

const hash8 = (buf) => createHash('sha1').update(buf).digest('hex').slice(0, 8)
const fingerprintedName = (name, h) => {
  const dot = name.lastIndexOf('.')
  return `${name.slice(0, dot)}-${h}${name.slice(dot)}`
}

const entries = await readdir(brandDir).catch(() => null)
if (!entries || entries.length === 0) {
  console.error('wire-brand-assets: dist/brand/ is empty — web/public/brand/ missing? Run scripts/gen-brand-assets.mjs.')
  process.exit(1)
}

await mkdir(assetsDir, { recursive: true })
const urlFor = new Map() // stable name -> fingerprinted /binflow/assets URL

for (const name of entries) {
  if (name === 'manifest.json') continue // 先改写再指纹化（下方单独处理）
  const buf = await readFile(join(brandDir, name))
  const out = fingerprintedName(name, hash8(buf))
  await writeFile(join(assetsDir, out), buf)
  urlFor.set(name, TO_PREFIX + out)
}

// manifest：icons[].src（稳定名、相对 manifest URL 解析）改写为指纹名，
// 再整体指纹化入住——相对引用随 manifest 落在 /binflow/assets/ 下解析。
const manifest = JSON.parse(await readFile(join(brandDir, 'manifest.json'), 'utf8'))
for (const icon of manifest.icons) {
  const stable = icon.src.split('/').pop()
  const mapped = urlFor.get(stable)
  if (!mapped) {
    console.error(`wire-brand-assets: manifest icon "${icon.src}" has no generated file (public/brand/${stable})`)
    process.exit(1)
  }
  icon.src = mapped.split('/').pop()
}
const manifestBuf = Buffer.from(JSON.stringify(manifest, null, 2) + '\n')
const manifestOut = fingerprintedName('manifest.json', hash8(manifestBuf))
await writeFile(join(assetsDir, manifestOut), manifestBuf)
urlFor.set('manifest.json', TO_PREFIX + manifestOut)

// index.html：/binflow/ui/brand/<name> → /binflow/assets/<fingerprint>。
// 校验方向 = 「html 引用的必有映射」（icon-512 这类仅 manifest 消费的
// 文件不要求进 html——反向强求会把 manifest 专属档误判为断链）。
let html = await readFile(htmlPath, 'utf8')
const referenced = new Set(
  [...html.matchAll(new RegExp(`${FROM_PREFIX.replace(/\//g, '\\/')}(?<name>[\\w.-]+)`, 'g'))].map((m) => m.groups.name),
)
for (const name of referenced) {
  if (!urlFor.has(name)) {
    console.error(`wire-brand-assets: index.html references ${FROM_PREFIX}${name} but no such file was generated`)
    process.exit(1)
  }
  html = html.split(FROM_PREFIX + name).join(urlFor.get(name))
}
await writeFile(htmlPath, html)

// 自校验：属性面无残留（注释里的管线说明合法提及路径形态，不在此列）
// + 目标文件在场。
if (new RegExp(`(href|src|content)="${FROM_PREFIX.replace(/\//g, '\\/')}`).test(html)) {
  console.error('wire-brand-assets: surviving /binflow/ui/brand/ URL in an attribute of index.html')
  process.exit(1)
}
const present = new Set(await readdir(assetsDir))
for (const url of urlFor.values()) {
  if (!present.has(url.split('/').pop())) {
    console.error(`wire-brand-assets: ${url} not written — inconsistent state`)
    process.exit(1)
  }
}
await rm(brandDir, { recursive: true })

console.log(
  `wire-brand-assets: ${urlFor.size} brand files fingerprinted onto ${TO_PREFIX} (` +
    [...urlFor.values()].map((u) => u.split('/').pop()).join(', ') +
    ')',
)
