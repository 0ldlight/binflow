// Post-build product-copy gate. Source-level scanning runs before Vite; this
// pass catches runtime-generated strings before the SPA is embedded/shipped.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join, relative } from 'node:path'

const root = fileURLToPath(new URL('../dist/', import.meta.url))
const extensions = new Set(['.html', '.js', '.css', '.json', '.svg', '.txt', '.webmanifest'])
const forbidden = new RegExp(['arti', 'factory'].join(''), 'i')
const violations = []

function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) {
      walk(path)
      continue
    }
    if (!extensions.has(entry.slice(entry.lastIndexOf('.')))) continue
    if (forbidden.test(readFileSync(path, 'utf8'))) violations.push(relative(root, path))
  }
}

walk(root)
if (violations.length) {
  console.error(`assert-built-brand-copy: ${violations.length} built file(s) contain third-party repository branding`)
  for (const file of violations) console.error(`  ${file}`)
  process.exit(1)
}
console.log('assert-built-brand-copy: OK（构建产物零第三方仓库品牌词）')
