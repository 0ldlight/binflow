// Product-copy gate: BinFlow UI code and browser tests must not reintroduce
// third-party repository branding. Wire-compatibility names live behind the
// Go API surface and are intentionally outside this frontend assertion.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join, relative } from 'node:path'

const root = fileURLToPath(new URL('../', import.meta.url))
const roots = ['src', 'e2e', 'scripts']
const extensions = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.css'])
const forbidden = new RegExp(['arti', 'factory'].join(''), 'i')
const violations = []

function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) {
      walk(path)
      continue
    }
    if (path.endsWith('assert-brand-copy.mjs')) continue
    if (!extensions.has(entry.slice(entry.lastIndexOf('.')))) continue
    const text = readFileSync(path, 'utf8')
    if (forbidden.test(text)) violations.push(relative(root, path))
  }
}

for (const dir of roots) walk(join(root, dir))
if (violations.length) {
  console.error(`assert-brand-copy: ${violations.length} file(s) contain third-party repository branding`)
  for (const file of violations) console.error(`  ${file}`)
  process.exit(1)
}
console.log('assert-brand-copy: OK（前端源码与测试零第三方仓库品牌词）')
