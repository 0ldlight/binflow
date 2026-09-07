// T-463（FR-149.1）CI 断言：组件层零硬编码中文 + i18n 键集同构对账。
//
// 用法：node scripts/assert-i18n.mjs [--stats]
// 退出码：0 = 全绿；1 = 发现违规（新增硬编码 CJK / 键集漂移）。
// 挂点：package.json scripts.build（vite build 前）与 scripts.lint 链尾；
// CI 经 `npm run build` / `npm run lint` 双路生效（.circleci/config.yml）。
//
// 三道闸：
//  1. 硬编码闸——src 内（i18n 目录包/清单除外）任何 CJK 出现在 JSX 文本、
//     字符串/模板字面量、且不是翻译器（`const X = tr('域')` 绑定）调用的
//     首参 → 违规。行内豁免标记：行尾 `// i18n-allow`（数据值等特殊场景，
//     须在报告中登记）。
//  2. 键集闸——t() 键按域路由（≥2 域 → common）后，与 en 目录包
//     （locales/en/*.ts 的 registerEn 键集）双向同构：missing key（运行时
//     告警源）与 orphan key（已删文案的目录残留）都算漂移。
//  3. 清单闸——zh 键清单（manifests/zh/*.json）与路由结果逐域一致
//     （zh-as-key：清单即 zh 包的物化形态，T-464 填 en 的对照底稿）。
import ts from 'typescript'
import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const WEB = dirname(dirname(fileURLToPath(import.meta.url)))
const SRC = join(WEB, 'src')
const STATS = process.argv.includes('--stats')
const CJK = /[\u3000-\u303F\u3290-\u329F\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF\uFF01-\uFFE6]/
const DOMAINS = ['console', 'repositories', 'artifacts', 'bundles', 'search', 'security', 'governance', 'monitoring', 'webhooks', 'admin', 'common']

const violations = []

function walk(dir) {
  const out = []
  for (const e of readdirSync(dir)) {
    const p = join(dir, e)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    else if (/\.tsx?$/.test(e) && !e.endsWith('.d.ts')) out.push(p)
  }
  return out
}

const usage = [] // { domain, key }
const lineAllows = (sf, node) => {
  const { line } = sf.getLineAndCharacterOfPosition(node.getStart(sf))
  return sf.text.split('\n')[line]?.endsWith('// i18n-allow')
}

for (const abs of walk(SRC)) {
  const rel = relative(SRC, abs)
  if (rel.startsWith('i18n/')) continue // 内核/目录包/清单——键的合法居所
  const src = readFileSync(abs, 'utf8')
  const sf = ts.createSourceFile(abs, src, ts.ScriptTarget.ES2022, true, rel.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)

  // 翻译器绑定：顶层 `const X = tr('domain')`（tr 自 */i18n 导入）
  const translators = new Map()
  for (const st of sf.statements) {
    if (!ts.isVariableStatement(st)) continue
    for (const d of st.declarationList.declarations) {
      if (!ts.isIdentifier(d.name) || !d.initializer || !ts.isCallExpression(d.initializer)) continue
      const call = d.initializer
      if (!ts.isIdentifier(call.expression) || call.expression.text !== 'tr') continue
      const domArg = call.arguments[0]
      if (domArg && ts.isStringLiteral(domArg) && DOMAINS.includes(domArg.text)) translators.set(d.name.text, domArg.text)
    }
  }

  const visit = (node) => {
    // 键采集 + 豁免：翻译器调用首参
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && translators.has(node.expression.text)) {
      const key = node.arguments[0]
      if (key && ts.isStringLiteral(key)) usage.push({ domain: translators.get(node.expression.text), key: key.text })
      // 其余实参（vars 对象）仍需巡视（对象值里不得藏硬编码中文）
      for (const a of node.arguments.slice(1)) visit(a)
      return
    }
    if (ts.isTemplateExpression(node)) {
      const literalCJK = CJK.test(node.head.text) || node.templateSpans.some((s) => CJK.test(s.literal.text))
      if (literalCJK && !lineAllows(sf, node)) {
        violations.push({ rel, node, msg: '硬编码中文模板（未走 t()）' })
      }
      ts.forEachChild(node, visit)
      return
    }
    if (ts.isJsxText(node) && CJK.test(node.text) && !lineAllows(sf, node)) {
      violations.push({ rel, node, msg: '硬编码中文 JSX 文本' })
    }
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && CJK.test(node.text) && !lineAllows(sf, node)) {
      violations.push({ rel, node, msg: '硬编码中文字符串' })
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)
}

// —— 键路由（与 codemod 同规则）：≥2 域使用的键 → common ——

const keyDomains = new Map()
for (const { domain, key } of usage) {
  if (!keyDomains.has(key)) keyDomains.set(key, new Set())
  keyDomains.get(key).add(domain)
}
const routed = Object.fromEntries(DOMAINS.map((d) => [d, new Set()]))
for (const { domain, key } of usage) routed[keyDomains.get(key).size >= 2 ? 'common' : domain].add(key)

// —— en 目录包键集（解析 registerEn 调用）——

function parseCatalog(path) {
  if (!existsSync(path)) return null
  const sf = ts.createSourceFile(path, readFileSync(path, 'utf8'), ts.ScriptTarget.ES2022, true, ts.ScriptKind.TS)
  let keys = null
  const visit = (node) => {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === 'registerEn') {
      const obj = node.arguments[1]
      if (obj && ts.isObjectLiteralExpression(obj)) {
        keys = new Set(obj.properties.filter(ts.isPropertyAssignment).map((p) => (ts.isStringLiteral(p.name) ? p.name.text : null)).filter(Boolean))
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)
  return keys
}

for (const d of DOMAINS) {
  const catalog = parseCatalog(join(SRC, `i18n/locales/en/${d}.ts`))
  const expected = routed[d]
  if (!catalog && expected.size === 0) continue
  if (!catalog) {
    violations.push({ rel: `i18n/locales/en/${d}.ts`, msg: `目录包缺失（路由期 ${expected.size} 键）` })
    continue
  }
  for (const k of expected) {
    if (!catalog.has(k)) {
      violations.push({ rel: `i18n/locales/en/${d}.ts`, msg: `missing key（运行时告警源）: ${k.slice(0, 50)}` })
    }
  }
  for (const k of catalog) {
    if (!expected.has(k)) {
      violations.push({ rel: `i18n/locales/en/${d}.ts`, msg: `orphan key（文案已删/未路由至此）: ${k.slice(0, 50)}` })
    }
  }
  // zh 清单闸
  const manifestPath = join(SRC, `i18n/manifests/zh/${d}.json`)
  if (!existsSync(manifestPath)) {
    violations.push({ rel: `i18n/manifests/zh/${d}.json`, msg: '清单缺失' })
  } else {
    const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))
    const m = new Set(manifest)
    for (const k of expected) if (!m.has(k)) { violations.push({ rel: `i18n/manifests/zh/${d}.json`, msg: `清单缺键: ${k.slice(0, 50)}` }) }
    for (const k of m) if (!expected.has(k)) { violations.push({ rel: `i18n/manifests/zh/${d}.json`, msg: `清单多键: ${k.slice(0, 50)}` }) }
  }
}

// —— 输出 ——

if (STATS) {
  console.log(`i18n stats: ${usage.length} usage points, ${keyDomains.size} unique keys`)
  for (const d of DOMAINS) console.log(`  ${d.padEnd(14)} ${routed[d].size} keys`)
}

if (violations.length) {
  console.error(`assert-i18n: ${violations.length} violation(s)`)
  for (const v of violations.slice(0, 40)) {
    if (v.node) {
      const { line } = v.node.getSourceFile().getLineAndCharacterOfPosition(v.node.getStart())
      console.error(`  ${v.rel}:${line + 1}  ${v.msg}`)
    } else {
      console.error(`  ${v.rel}  ${v.msg}`)
    }
  }
  if (violations.length > 40) console.error(`  …另有 ${violations.length - 40} 处`)
  process.exit(1)
}
console.log(`assert-i18n: OK（零硬编码中文；${usage.length} 调用点 / ${keyDomains.size} 键，en 包与 zh 清单同构）`)
