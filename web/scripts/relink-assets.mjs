// relink-assets: run by `npm run build` right after `vite build`.
//
// ADR-0014 (T-108 errata) pins the fingerprinted assets to the SHARED mount
// /binflow/assets/<hash>.<ext> — outside the /binflow/ui/** segment, so the
// Go handler's segment-internal history fallback can stay dumb (every path
// under /binflow/ui/ is the SPA shell; no file/asset special-casing). Vite's
// `base` is a single value, so with base=/binflow/ui/ the emitted index.html
// and CSS reference /binflow/ui/assets/... instead. This script rewrites
// those references onto the shared mount and moves nothing else:
//
//   dist/index.html   <script>/<link> URLs  ->  /binflow/assets/...
//   dist/assets/*.css  url(...) references  ->  /binflow/assets/...
//
// Chunk-to-chunk imports are RELATIVE specifiers in Rollup output ("./C-xyz.js"),
// so they follow the importing chunk onto the shared mount automatically. The
// one thing this pass cannot see is an asset URL embedded from JS via
// import.meta.ROLLUP_FILE_URL/new URL(...) — the scaffold uses none; the day a
// screen needs that, switch this pass for vite's experimental.renderBuiltUrl
// (same mapping, hook-level) and delete this note.
import { readdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

const dist = new URL('../dist', import.meta.url).pathname
const from = '/binflow/ui/assets/'
const to = '/binflow/assets/'

const targets = [join(dist, 'index.html')]
for (const entry of await readdir(join(dist, 'assets'), { withFileTypes: true })) {
  if (entry.isFile() && entry.name.endsWith('.css')) {
    targets.push(join(dist, 'assets', entry.name))
  }
}

let rewritten = 0
for (const file of targets) {
  const source = await readFile(file, 'utf8')
  if (!source.includes(from)) continue
  await writeFile(file, source.split(from).join(to))
  rewritten++
}
console.log(`relink-assets: ${rewritten} file(s) rewritten ${from} -> ${to}`)
