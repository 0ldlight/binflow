// relink-assets: run by `npm run build` right after `vite build`.
//
// ADR-0014 (T-108 errata) pins the fingerprinted assets to the SHARED mount
// /binflow/assets/<hash>.<ext> — outside the /binflow/ui/** segment, so the
// Go handler's segment-internal history fallback can stay dumb (every path
// under /binflow/ui/ is the SPA shell; no file/asset special-casing). Vite's
// `base` is a single value, so with base=/binflow/ui/ the build bakes that
// segment into THREE faces:
//
//   1. full URLs in dist/index.html        /binflow/ui/assets/…
//   2. full URLs in dist/assets/*.css url(…)  (same shape)
//   3. a runtime JOIN inside the JS chunks: the preload helper's asset-URL
//      function  assetsURL = dep => "/binflow/ui/" + dep  where dep is the
//      outDir-relative name ("assets/<hash>.<ext>"). Lazy routes resolve
//      their chunk/CSS deps through it AT RUNTIME — T-104 D-104-1: those
//      URLs land in the ui segment where the SPA fallback answers the shell
//      as text/html (Chromium silently drops the page-local CSS and burns
//      ~20 dead 497B prefetches per navigation; WebKit/Firefox strict-MIME
//      -reject the stylesheet, the helper then rejects the dynamic import,
//      and the whole route renders blank).
//
// This pass maps all three faces onto the shared mount and moves nothing else:
//
//   faces 1+2:  /binflow/ui/assets/…   ->  /binflow/assets/…
//   face 3:     "/binflow/ui/" + dep   ->  "/binflow/" + dep  (dep already
//               carries "assets/", so the join lands on /binflow/assets/…)
//
// Chunk-to-chunk imports are RELATIVE specifiers in Rollup output ("./C-xyz.js"),
// so they follow the importing chunk onto the shared mount automatically. The
// router basename is the distinct "/binflow/ui" literal (no trailing slash,
// src/main.tsx) and is deliberately untouched.
//
// After rewriting the pass VERIFIES itself: any surviving /binflow/ui/assets
// literal or "/binflow/ui/" join prefix anywhere under dist/ fails the build.
// A future vite that changes the emission shape must update this pass, not
// ship dead URLs — exactly the drift that let D-104-1 through T-89 unnoticed.
//
// The one thing this pass still cannot see is an asset URL embedded from JS
// via import.meta.ROLLUP_FILE_URL/new URL(…) — the scaffold uses none; the day
// a screen needs that, switch this pass for vite's experimental.renderBuiltUrl
// (same mapping, hook-level) and delete this note.
import { readdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

const dist = new URL('../dist', import.meta.url).pathname

// Faces 1+2: full emitted URLs (index.html <script>/<link>, CSS url(...)).
const literalFrom = '/binflow/ui/assets/'
const literalTo = '/binflow/assets/'

// Face 3: the base-join string literal baked into the preload helper. The
// quote character is the minifier's choice, so accept ' " or ` — same char on
// both ends — and keep it in the replacement.
const joinRe = /(["'`])\/binflow\/ui\/\1/g
const joinTo = '$1/binflow/$1'
const joinTestRe = /(["'`])\/binflow\/ui\/\1/

const targets = [join(dist, 'index.html')]
for (const entry of await readdir(join(dist, 'assets'), { withFileTypes: true })) {
  if (entry.isFile() && (entry.name.endsWith('.css') || entry.name.endsWith('.js'))) {
    targets.push(join(dist, 'assets', entry.name))
  }
}

let literalHits = 0
let joinHits = 0
let touched = 0
for (const file of targets) {
  const source = await readFile(file, 'utf8')
  let next = source
  if (source.includes(literalFrom)) {
    literalHits += source.split(literalFrom).length - 1
    next = next.split(literalFrom).join(literalTo)
  }
  const matches = next.match(joinRe) ?? []
  if (matches.length > 0) {
    joinHits += matches.length
    next = next.replace(joinRe, joinTo)
  }
  if (next !== source) {
    await writeFile(file, next)
    touched++
  }
}
console.log(
  `relink-assets: ${literalHits} full URL(s) and ${joinHits} preload join(s) -> ${literalTo}` +
    ` (${touched}/${targets.length} candidate file(s) rewritten)`,
)

// Self-check: the build must not ship a dead ui-segment asset URL in ANY
// shape. Every file under dist/ is scanned, not just the rewrite targets.
const leftovers = []
async function scan(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      await scan(full)
      continue
    }
    const text = await readFile(full, 'utf8').catch(() => '')
    if (text.includes(literalFrom)) {
      leftovers.push(`${full}: still contains ${literalFrom}`)
    }
    if (joinTestRe.test(text)) {
      leftovers.push(`${full}: still contains a "/binflow/ui/" preload join`)
    }
  }
}
await scan(dist)
if (leftovers.length > 0) {
  for (const line of leftovers) console.error(`relink-assets: LEFTOVER ${line}`)
  process.exit(1)
}
console.log('relink-assets: verified — no ui-segment asset reference survives under dist/')
