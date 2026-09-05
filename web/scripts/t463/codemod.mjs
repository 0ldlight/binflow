// T-463 codemod：全树 CJK 文案 → t() 键引用（zh-as-key）。一次性工具，
// 产物（src/i18n/locales/en/*.ts 骨架 + manifests/zh/*.json 清单）为
// 提交物；常驻 CI 门是 scripts/assert-i18n.mjs（防回流 + 键集同构对账）。
//
// 用法：node scripts/t463/codemod.mjs [--dry]
//
// 转换规则（零语义变化的机械化保证）：
// - JSX 文本：按 JSX 编译语义归一（逐行 trim、空行剔除、行间以单空格
//   连接——ts.transpile 实测对齐），整节点 → {t('key')}；单行节点两侧
//   纯空格（无换行）在花括号外原样保留（JSX 对其保真）。
// - 字符串字面量：'中文' → t('中文')；JSX 属性位置（attr="中文"）补
//   花括号 attr={t('中文')}。
// - 模板字面量（头/尾字面量段含 CJK）：`${a} 项` → t('{a} 项', { a })；
//   非标识符表达式按序命名 v1/v2…，其源码内的 CJK 字面量递归键化
//   （内层编辑吸收进复合替换，不进全局编辑表——防偏移二次移位）。
// - 每文件注入 `import { tr } from '<rel>/i18n'` + `const t = tr('<域>')`
//   （t 名冲突时顺延 tt/tti）；域按路径映射（见 domainOf）。
// 危险位（case 子句/对象键/字面量类型）不得静默转换——显式报错人工裁决。
import ts from 'typescript'
import { readFileSync, writeFileSync, mkdirSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const WEB = dirname(dirname(dirname(fileURLToPath(import.meta.url)))) // web/（scripts/t463/ 上两级）
const SRC = join(WEB, 'src')
const DRY = process.argv.includes('--dry')
const CJK = /[\u3000-\u303F\u3290-\u329F\u3400-\u4DBF\u4E00-\u9FFF\uF900-\uFAFF\uFF01-\uFFE6]/

/** 文件 → 键域（与 src/i18n/index.ts I18nDomain 同构；脚本的单一事实源） */
function domainOf(rel) {
  if (rel.startsWith('i18n/')) return null // 内核与目录包不参与
  if (rel === 'main.tsx' || rel.startsWith('app/') || rel.startsWith('components/') || rel.startsWith('lib/')) return 'console'
  if (rel.startsWith('pages/repositories/')) return 'repositories'
  if (rel.startsWith('pages/artifacts/')) return 'artifacts'
  if (rel.startsWith('pages/search/')) return 'search'
  if (rel.startsWith('pages/security/')) return 'security'
  if (rel.startsWith('pages/governance/') || rel.startsWith('pages/audit/')) return 'governance'
  if (rel.startsWith('pages/monitoring/')) return 'monitoring'
  if (rel.startsWith('pages/webhooks/')) return 'webhooks'
  if (rel.startsWith('pages/admin/')) return 'admin'
  if (rel.startsWith('pages/')) return 'console' // 顶层散页（Dashboard/Login/...）
  return null
}

function walk(dir) {
  const out = []
  for (const e of readdirSync(dir)) {
    const p = join(dir, e)
    if (statSync(p).isDirectory()) out.push(...walk(p))
    else if (/\.tsx?$/.test(e) && !e.endsWith('.d.ts')) out.push(p)
  }
  return out
}

/** 单引号 TS 字面量转义（键值即 zh 原文——不做任何改写） */
function lit(s) {
  return "'" + s.replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/\n/g, '\\n').replace(/\r/g, '\\r') + "'"
}

/**
 * JSX 文本编译语义归一（ts/esbuild 实测对齐）：
 * - 含换行的空白 run → 单空格（行缘邻接换行的空白一并折叠）；
 * - 不含换行的空白 run 原样保真（单行节点缘空格 / 行内多空格）。
 * 即：\n 邻接空白才是折叠对象——纯空格永不折叠。
 */
function jsxNormalize(raw) {
  return raw
    .replace(/^\s+/, (m) => (m.includes('\n') ? '' : m))
    .replace(/\s+$/, (m) => (m.includes('\n') ? '' : m))
    .replace(/\s*\n\s*/g, ' ')
}

const usage = [] // { domain, key, tpl? }（含模板内嵌套键）
const manual = [] // 危险位报告

function processFile({ abs, rel, domain }) {
  const src = readFileSync(abs, 'utf8')
  const sf = ts.createSourceFile(abs, src, ts.ScriptTarget.ES2022, true, rel.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)

  // 编辑作用域栈：复合模板的内层编辑只进本层替换文本，不外溢
  const editStack = [[]]
  const cur = () => editStack[editStack.length - 1]

  /** 表达式源码（给定编辑表内含编辑应用后） */
  const splice = (text, list, base) => {
    let out = text
    for (const ed of [...list].sort((a, b) => b.start - a.start)) {
      out = out.slice(0, ed.start - base) + ed.text + out.slice(ed.end - base)
    }
    return out
  }
  const exprSrc = (node, list) => {
    const s = node.getStart(sf)
    return splice(src.slice(s, node.end), list.filter((ed) => ed.start >= s && ed.end <= node.end), s)
  }

  // 绑定名收集（翻译器名冲突检测）
  const bindings = new Set()
  const collectBindings = (node) => {
    const addPat = (name) => {
      if (!name) return
      if (ts.isIdentifier(name)) bindings.add(name.text)
      else if (ts.isBindingPattern(name)) name.elements.forEach((el) => addPat(el.name))
    }
    if (ts.isVariableDeclaration(node) || ts.isParameter(node) || ts.isBindingElement(node)) addPat(node.name)
    else if ((ts.isFunctionDeclaration(node) || ts.isClassDeclaration(node) || ts.isEnumDeclaration(node) || ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) && node.name) bindings.add(node.name.text)
    else if (ts.isImportSpecifier(node)) bindings.add((node.propertyName ?? node.name).text)
    else if (ts.isCatchClause(node) && node.variableDeclaration) addPat(node.variableDeclaration.name)
    ts.forEachChild(node, collectBindings)
  }
  collectBindings(sf)
  const tName = !bindings.has('t') ? 't' : !bindings.has('tt') ? 'tt' : 'tti'

  /** 模板 → t('key', { vars })（头/尾字面量含 CJK 时整体键化） */
  const templateEdit = (node) => {
    // 内层表达式先行递归键化（独立作用域——编辑吸收进 vars 源码）
    editStack.push([])
    for (const span of node.templateSpans) visit(span.expression)
    const inner = editStack.pop()
    const parts = [node.head.text]
    const vars = []
    let vIdx = 0
    for (const span of node.templateSpans) {
      const e = span.expression
      let name
      if (ts.isIdentifier(e) && /^[A-Za-z_][A-Za-z0-9_]*$/.test(e.text)) name = e.text
      else {
        vIdx += 1
        name = `v${vIdx}`
      }
      const eSrc = exprSrc(e, inner)
      if (vars.some((v) => v.name === name && v.src !== eSrc)) name = `${name}${++vIdx}` // 同名异表达式消歧
      parts.push(`{${name}}`, span.literal.text)
      vars.push({ name, src: eSrc })
    }
    const key = parts.join('')
    if (key.includes('\n')) {
      manual.push({ rel, line: sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1, kind: 'multiline-template', text: key.slice(0, 60) })
      return
    }
    usage.push({ domain, key, tpl: true })
    const varsSrc = vars.length ? `, { ${vars.map((v) => `${v.name}: ${v.src}`).join(', ')} }` : ''
    cur().push({ start: node.getStart(sf), end: node.end, text: `${tName}(${lit(key)}${varsSrc})` })
  }

  const visit = (node) => {
    if (ts.isTemplateExpression(node)) {
      const literalCJK = CJK.test(node.head.text) || node.templateSpans.some((s) => CJK.test(s.literal.text))
      if (literalCJK) {
        templateEdit(node)
        return // 整体键化；内层字面量已经内层作用域吸收
      }
      ts.forEachChild(node, visit) // 字面量无 CJK：仅表达式内可能有键化点
      return
    }
    if (ts.isJsxText(node) && CJK.test(node.text)) {
      const raw = node.text
      // 缘部纯空格 run（不含换行）编译保真——留在花括号外；其余（含
      // 换行的 run）由 jsxNormalize 按编译语义折叠/剔除
      const lead = /^[ \t]*/.exec(raw)[0]
      const trail = /[ \t]*$/.exec(raw)[0]
      const key = jsxNormalize(raw.slice(lead.length, raw.length - trail.length))
      if (key) {
        usage.push({ domain, key })
        cur().push({ start: node.pos, end: node.end, text: `${lead}{${tName}(${lit(key)})}${trail}` })
      }
      return
    }
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && CJK.test(node.text)) {
      const p = node.parent
      if (
        ts.isLiteralTypeNode(p) ||
        ts.isCaseClause(p) ||
        (ts.isPropertyAssignment(p) && p.name === node) ||
        (ts.isEnumMember(p) && p.name === node) ||
        (ts.isPropertyDeclaration(p) && p.name === node) ||
        (ts.isMethodDeclaration(p) && p.name === node)
      ) {
        manual.push({ rel, line: sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1, kind: ts.isCaseClause(p) ? 'case-clause' : ts.isLiteralTypeNode(p) ? 'literal-type' : 'object-key', text: node.text.slice(0, 60) })
        return
      }
      usage.push({ domain, key: node.text })
      const inAttr = ts.isJsxAttribute(p) && p.initializer === node
      cur().push({ start: node.getStart(sf), end: node.end, text: inAttr ? `{${tName}(${lit(node.text)})}` : `${tName}(${lit(node.text)})` })
      return
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)

  const edits = editStack[0]
  if (!edits.length) return { rel, changed: false, count: 0 }

  // 注入 import + 翻译器：最后一个 import 的行终结符之后（紧随 import 块），
  // 先于任何非 import 语句（= 先于任何使用点）；文件头注释归属不动
  const imports = sf.statements.filter((s) => ts.isImportDeclaration(s))
  let insertAt
  let inject
  const depth = rel.split('/').length - 1
  const importPath = depth === 0 ? './i18n' : `${'../'.repeat(depth)}i18n`
  const decl = `import { tr } from ${JSON.stringify(importPath)}\n\nconst ${tName} = tr('${domain}')\n`
  if (imports.length) {
    const last = imports[imports.length - 1]
    insertAt = last.end
    if (src[insertAt] === '\r') insertAt += 1
    if (src[insertAt] === '\n') insertAt += 1
    inject = decl
  } else {
    insertAt = 0
    inject = decl + '\n'
  }
  edits.push({ start: insertAt, end: insertAt, text: inject })

  let out = splice(src, edits, 0)
  if (!DRY) writeFileSync(abs, out)
  return { rel, changed: true, count: edits.length - 1, tName }
}

const files = walk(SRC)
  .map((abs) => ({ abs, rel: relative(SRC, abs) }))
  .map((f) => ({ ...f, domain: domainOf(f.rel) }))
  .filter((f) => f.domain)
const results = files.map(processFile)

// —— 键路由：≥2 域使用的键 → common；单域键 → 该域 ——

const keyDomains = new Map()
for (const { domain, key } of usage) {
  if (!keyDomains.has(key)) keyDomains.set(key, new Set())
  keyDomains.get(key).add(domain)
}
const routed = {}
for (const { domain, key } of usage) {
  const target = keyDomains.get(key).size >= 2 ? 'common' : domain
  ;(routed[target] ??= new Set()).add(key)
}

// —— 生成 en 骨架 + zh 清单 ——

const DOMAINS = ['console', 'repositories', 'artifacts', 'search', 'security', 'governance', 'monitoring', 'webhooks', 'admin', 'common']
if (!DRY) {
  mkdirSync(join(SRC, 'i18n/locales/en'), { recursive: true })
  mkdirSync(join(SRC, 'i18n/manifests/zh'), { recursive: true })
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
}

// —— 报告 ——

const changed = results.filter((r) => r.changed)
console.log(`files: ${files.length}, changed: ${changed.length}, usage points: ${usage.length}, unique keys: ${keyDomains.size}`)
for (const d of DOMAINS) {
  const n = (routed[d] ?? new Set()).size
  if (n) console.log(`  ${d.padEnd(14)} ${n} keys`)
}
if (manual.length) {
  console.log('MANUAL REVIEW NEEDED:')
  for (const m of manual) console.log(`  ${m.rel}:${m.line} [${m.kind}] ${m.text}`)
  process.exitCode = 2
}
// 非模板键内含 {name} 形态：运行时安全（插值只替换实传 vars 的占位符——
// 无 vars 调用原样保留），但登记备查（防未来同名 var 误伤）
for (const u of usage) {
  if (!u.tpl && /\{[A-Za-z0-9_]+\}/.test(u.key)) console.log(`brace-in-key (vars-free, safe): ${u.domain}: ${u.key.slice(0, 60)}`)
}
const names = changed.filter((r) => r.tName !== 't').map((r) => `${r.rel}→${r.tName}`)
if (names.length) console.log('translator renamed (conflict):', names.join(', '))
