// T-463 目录包/清单再生成：从当前源码的 t() 调用重算键路由并重写
// locales/en/*.ts + manifests/zh/*.json（与 codemod 同规则——≥2 域 → common）。
// 用途：手工修正键集后（如 emoji 垃圾键拆除）免重跑 codemod（codemod 不可
// 对已转换树重放——会把 t() 首参再包一层）。
import ts from 'typescript'
import { readFileSync, writeFileSync, readdirSync, statSync, rmSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const WEB = dirname(dirname(dirname(fileURLToPath(import.meta.url))))
const SRC = join(WEB, 'src')
const DOMAINS = ['console', 'repositories', 'artifacts', 'search', 'security', 'governance', 'monitoring', 'webhooks', 'admin', 'common']

function walk(d) {
  const out = []
  for (const e of readdirSync(d)) {
    const p = join(d, e)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    else if (/\.tsx?$/.test(e) && !e.endsWith('.d.ts')) out.push(p)
  }
  return out
}

const usage = []
for (const abs of walk(SRC)) {
  const rel = relative(SRC, abs)
  if (rel.startsWith('i18n/')) continue
  const sf = ts.createSourceFile(abs, readFileSync(abs, 'utf8'), ts.ScriptTarget.ES2022, true, rel.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const translators = new Map()
  for (const st of sf.statements) {
    if (!ts.isVariableStatement(st)) continue
    for (const d of st.declarationList.declarations) {
      if (!ts.isIdentifier(d.name) || !d.initializer || !ts.isCallExpression(d.initializer)) continue
      const c = d.initializer
      if (ts.isIdentifier(c.expression) && c.expression.text === 'tr' && c.arguments[0] && ts.isStringLiteral(c.arguments[0])) translators.set(d.name.text, c.arguments[0].text)
    }
  }
  if (!translators.size) continue
  const visit = (n) => {
    if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && translators.has(n.expression.text)) {
      const k = n.arguments[0]
      if (k && ts.isStringLiteral(k)) usage.push({ domain: translators.get(n.expression.text), key: k.text })
    }
    ts.forEachChild(n, visit)
  }
  visit(sf)
}

const keyDomains = new Map()
for (const { domain, key } of usage) {
  if (!keyDomains.has(key)) keyDomains.set(key, new Set())
  keyDomains.get(key).add(domain)
}
const routed = Object.fromEntries(DOMAINS.map((d) => [d, new Set()]))
for (const { domain, key } of usage) routed[keyDomains.get(key).size >= 2 ? 'common' : domain].add(key)

const lit = (s) => "'" + s.replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/\n/g, '\\n').replace(/\r/g, '\\r') + "'"

// 先清场（撤域文件也要消失，防 orphan），再写现役域
for (const old of readdirSync(join(SRC, 'i18n/locales/en'))) {
  if (old !== 'catalogs.ts') rmSync(join(SRC, 'i18n/locales/en', old))
}
const indexLines = [
  '// T-463 生成：en 目录包索引（initI18n 懒载入口——仅 en locale 引导时',
  '// 动态 import 本模块，zh 用户零额外字节）。各域包 = 键集同构骨架，',
  '// 值待 T-464 填充（空串 = 未填 → 运行时回落 zh 键文案）。',
]
for (const d of DOMAINS) {
  const keys = [...(routed[d] ?? new Set())].sort((a, b) => a.localeCompare(b, 'zh'))
  if (!keys.length) continue
  writeFileSync(
    join(SRC, `i18n/locales/en/${d}.ts`),
    [
      `// T-463 生成（scripts/assert-i18n.mjs 对账）：${d} 域 en 骨架——`,
      `// 键 = zh 文案原文（zh-as-key）；值待 T-464 填充（空串 = 未填回落 zh）。`,
      `import { registerEn } from '../../index'`,
      '',
      `registerEn('${d}', {`,
      ...keys.map((k) => `  ${lit(k)}: '',`),
      '})',
      '',
    ].join('\n'),
  )
  writeFileSync(join(SRC, `i18n/manifests/zh/${d}.json`), JSON.stringify(keys, null, 2) + '\n')
  indexLines.push(`import './${d}'`)
}
indexLines.push('')
writeFileSync(join(SRC, 'i18n/locales/en/catalogs.ts'), indexLines.join('\n'))
console.log(`regen: ${usage.length} usage points, ${keyDomains.size} unique keys`)
for (const d of DOMAINS) if (routed[d].size) console.log(`  ${d.padEnd(14)} ${routed[d].size} keys`)
