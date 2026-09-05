// 列出含星面字符（astral，U+10000+）的 t() 键——codemod 首跑的字面正则
// 在 UTF-16 码元语义下误把 emoji 高位代理当 CJK（豈=U+8C48 时 8C48-FAFF
// 覆盖 D800-DFFF）；转义版（豈 起）已收紧。纯 emoji 键 = 垃圾键待拆。
import ts from 'typescript'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

const SRC = new URL('../../src', import.meta.url).pathname
function walk(d) {
  const out = []
  for (const e of readdirSync(d)) {
    const p = join(d, e)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    else if (/\.tsx?$/.test(e) && !e.endsWith('.d.ts')) out.push(p)
  }
  return out
}
for (const abs of walk(SRC)) {
  const rel = relative(SRC, abs)
  if (rel.startsWith('i18n/')) continue
  const sf = ts.createSourceFile(abs, readFileSync(abs, 'utf8'), ts.ScriptTarget.ES2022, true, rel.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const translators = new Set()
  for (const st of sf.statements) {
    if (!ts.isVariableStatement(st)) continue
    for (const d of st.declarationList.declarations) {
      if (ts.isIdentifier(d.name) && d.initializer && ts.isCallExpression(d.initializer) && ts.isIdentifier(d.initializer.expression) && d.initializer.expression.text === 'tr') translators.add(d.name.text)
    }
  }
  const visit = (n) => {
    if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && translators.has(n.expression.text)) {
      const k = n.arguments[0]
      if (k && ts.isStringLiteral(k) && /[\u{10000}-\u{FFFFF}]/u.test(k.text)) {
        const pure = !/\p{Script=Han}|\p{P}/u.test(k.text.replace(/[\u{10000}-\u{FFFFF}]/gu, ''))
        console.log(`${rel}:${sf.getLineAndCharacterOfPosition(k.getStart()).line + 1}  ${JSON.stringify(k.text)}  translator=${n.expression.text}  astralOnly=${pure}`)
      }
    }
    ts.forEachChild(n, visit)
  }
  visit(sf)
}
