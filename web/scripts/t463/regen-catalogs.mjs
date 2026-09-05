// T-463 目录包/清单再生成：从当前源码的 t() 调用重算键路由并重写
// locales/en/*.ts + manifests/zh/*.json（与 codemod 同规则——≥2 域 → common）。
// 用途：手工修正键集后（如 emoji 垃圾键拆除）免重跑 codemod（codemod 不可
// 对已转换树重放——会把 t() 首参再包一层）。
// T-464 守护：en 值已全量填充——再生成时**逐键保值**（存量值原样带回，
// 仅新增键落空串）；初版骨架工具的「全量清空」行为会抹掉整包 en 译值。
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

// 键/值字面量 = JSON.stringify（双引号形态——与 T-464 填充产物一致，
// 差异最小化；TS 合法且 assert-i18n 走编译器解析不受引号形态影响）
const lit = (s) => JSON.stringify(s)

/** T-464 保值：解析现存 en 域包的键→值（TS 编译器 API 与 assert-i18n
 *  同法——任意引号形态/转义精确还原；assert-i18n 保证键集同构）。解析
 *  失败/无值按空串处理 = 新键的骨架态。 */
function existingValues(domain) {
  const values = new Map()
  try {
    const path = join(SRC, `i18n/locales/en/${domain}.ts`)
    const sf = ts.createSourceFile(path, readFileSync(path, 'utf8'), ts.ScriptTarget.ES2022, true, ts.ScriptKind.TS)
    const visit = (n) => {
      if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === 'registerEn') {
        const obj = n.arguments[1]
        if (obj && ts.isObjectLiteralExpression(obj)) {
          for (const p of obj.properties) {
            if (ts.isPropertyAssignment(p) && ts.isStringLiteral(p.name) && ts.isStringLiteral(p.initializer) && p.initializer.text !== '') {
              values.set(p.name.text, p.initializer.text)
            }
          }
        }
      }
      ts.forEachChild(n, visit)
    }
    visit(sf)
  } catch {
    // 域包不存在（首次生成）——全空骨架
  }
  return values
}

// T-464 保值快照：必须在下方清场**之前**读取（清场会删除域文件）
const keptByDomain = Object.fromEntries(DOMAINS.map((d) => [d, existingValues(d)]))

// 先清场（撤域文件也要消失，防 orphan），再写现役域
for (const old of readdirSync(join(SRC, 'i18n/locales/en'))) {
  if (old !== 'catalogs.ts') rmSync(join(SRC, 'i18n/locales/en', old))
}
const indexLines = [
  '// T-463 生成：en 目录包索引（initI18n 懒载入口——仅 en locale 引导时',
  '// 动态 import 本模块，zh 用户零额外字节）。值 = T-464 填充态（再生成',
  '// 走逐键保值——scripts/t463/regen-catalogs.mjs）。',
]
for (const d of DOMAINS) {
  const keys = [...(routed[d] ?? new Set())].sort((a, b) => a.localeCompare(b, 'zh'))
  if (!keys.length) continue
  const kept = keptByDomain[d] ?? new Map()
  let preserved = 0
  const lines = keys.map((k) => {
    const v = kept.get(k)
    if (v !== undefined) preserved++
    return `  ${lit(k)}: ${v === undefined ? "''" : lit(v)},`
  })
  writeFileSync(
    join(SRC, `i18n/locales/en/${d}.ts`),
    [
      `// T-463 键集 / T-464 填充：${d} 域 en 目录包——键 = zh 文案原文`,
      `// （zh-as-key），值 = 逐义对译 en（术语对齐 Artifactory：repo key / node /`,
      `// checksum / Deploy / Set Me Up 等英文术语原样保留）。`,
      `import { registerEn } from '../../index'`,
      '',
      `registerEn('${d}', {`,
      ...lines,
      '})',
      '',
    ].join('\n'),
  )
  writeFileSync(join(SRC, `i18n/manifests/zh/${d}.json`), JSON.stringify(keys, null, 2) + '\n')
  indexLines.push(`import './${d}'`)
  console.log(`  ${d.padEnd(14)} ${keys.length} keys（保值 ${preserved}）`)
}
indexLines.push('')
writeFileSync(join(SRC, 'i18n/locales/en/catalogs.ts'), indexLines.join('\n'))
console.log(`regen: ${usage.length} usage points, ${keyDomains.size} unique keys`)
for (const d of DOMAINS) if (routed[d].size) console.log(`  ${d.padEnd(14)} ${routed[d].size} keys`)
