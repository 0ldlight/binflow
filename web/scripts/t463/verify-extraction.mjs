// T-463 零语义变化对账：HEAD 基线的 CJK 文案清单 vs 改后 t() 键清单，
// 逐文件多重集相等 ⇔ 每一条原文案都成为一条键值恒等的键（zh-as-key 的
// 机械化证明）。JSX 文本两侧同用编译语义归一（ts/esbuild 实测对齐）；
// 模板按同一占位符命名规则重建键形。
//
// 用法：node scripts/t463/verify-extraction.mjs  （需 git，只读）
import ts from 'typescript'
import { execFileSync } from 'node:child_process'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const WEB = dirname(dirname(dirname(fileURLToPath(import.meta.url))))
const SRC = join(WEB, 'src')
const CJK = /[\u3000-\u303F\u3290-\u329F\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF\uFF01-\uFFE6]/

function walk(dir) {
  const out = []
  for (const e of readdirSync(dir)) {
    const p = join(dir, e)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    else if (/\.tsx?$/.test(e) && !e.endsWith('.d.ts')) out.push(p)
  }
  return out
}

function jsxNormalize(raw) {
  return raw
    .replace(/^\s+/, (m) => (m.includes('\n') ? '' : m))
    .replace(/\s+$/, (m) => (m.includes('\n') ? '' : m))
    .replace(/\s*\n\s*/g, ' ')
}

/** 与 codemod 同款模板键重建：标识符 → 名；其余 → vN（出现序） */
function templateKey(node) {
  const parts = [node.head.text]
  let vIdx = 0
  const names = []
  for (const span of node.templateSpans) {
    let name
    if (ts.isIdentifier(span.expression) && /^[A-Za-z_][A-Za-z0-9_]*$/.test(span.expression.text)) name = span.expression.text
    else name = `v${++vIdx}`
    const eSrc = node.getSourceFile().text.slice(span.expression.getStart(), span.expression.end)
    if (names.some((n) => n.name === name && n.src !== eSrc)) name = `${name}${++vIdx}`
    names.push({ name, src: eSrc })
    parts.push(`{${name}}`, span.literal.text)
  }
  return parts.join('')
}

/** 一份源码的 CJK 文案清单（多重集） */
function inventory(text, path) {
  const sf = ts.createSourceFile(path, text, ts.ScriptTarget.ES2022, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const bag = []
  const visit = (node) => {
    if (ts.isTemplateExpression(node)) {
      const literalCJK = CJK.test(node.head.text) || node.templateSpans.some((s) => CJK.test(s.literal.text))
      if (literalCJK) {
        bag.push(templateKey(node))
        // codemod 同款：span 表达式内的 CJK 字面量独立键化（vars 源码内）
        for (const span of node.templateSpans) visit(span.expression)
        return
      }
      ts.forEachChild(node, visit)
      return
    }
    if (ts.isJsxText(node) && CJK.test(node.text)) {
      const raw = node.text
      const lead = /^[ \t]*/.exec(raw)[0]
      const trail = /[ \t]*$/.exec(raw)[0]
      bag.push(jsxNormalize(raw.slice(lead.length, raw.length - trail.length)))
      return
    }
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && CJK.test(node.text)) {
      bag.push(node.text)
      return
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)
  return bag.sort()
}

/** 改后清单：t()/tt()/tti()（tr 绑定）调用首参 */
function keyInventory(text, path) {
  const sf = ts.createSourceFile(path, text, ts.ScriptTarget.ES2022, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const translators = new Set()
  for (const st of sf.statements) {
    if (!ts.isVariableStatement(st)) continue
    for (const d of st.declarationList.declarations) {
      if (ts.isIdentifier(d.name) && d.initializer && ts.isCallExpression(d.initializer) && ts.isIdentifier(d.initializer.expression) && d.initializer.expression.text === 'tr') translators.add(d.name.text)
    }
  }
  const bag = []
  const visit = (node) => {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && translators.has(node.expression.text)) {
      const k = node.arguments[0]
      if (k && ts.isStringLiteral(k)) bag.push(k.text)
      for (const a of node.arguments.slice(1)) visit(a)
      return
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)
  return bag.sort()
}

const diffs = []
let filesChecked = 0
let entriesChecked = 0
for (const abs of walk(SRC)) {
  const rel = relative(SRC, abs)
  if (rel.startsWith('i18n/')) continue
  const after = readFileSync(abs, 'utf8')
  let before
  try {
    before = execFileSync('git', ['show', `HEAD:web/src/${rel}`], { cwd: WEB, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 })
  } catch {
    diffs.push(`${rel}: 无 HEAD 基线（新文件）——跳过对账，以 assert-i18n 兜底`)
    continue
  }
  const a = inventory(before, abs)
  const b = keyInventory(after, abs)
  filesChecked += 1
  entriesChecked += a.length
  if (JSON.stringify(a) !== JSON.stringify(b)) {
    const onlyBefore = a.filter((x) => !b.includes(x))
    const onlyAfter = b.filter((x) => !a.includes(x))
    diffs.push(`${rel}: BEFORE ${a.length} vs AFTER ${b.length} | 仅前: ${JSON.stringify(onlyBefore.slice(0, 3))} | 仅后: ${JSON.stringify(onlyAfter.slice(0, 3))}`)
  }
}

if (diffs.length) {
  console.error(`verify-extraction: ${diffs.length} file(s) diverged`)
  for (const d of diffs) console.error('  ' + d)
  process.exit(1)
}
console.log(`verify-extraction: OK——${filesChecked} 文件逐字对账相等（${entriesChecked} 条文案）`)
