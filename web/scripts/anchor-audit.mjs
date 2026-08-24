#!/usr/bin/env node
// 锚册三方对账（T-244 立、T-267 口径单一权威化 + --ledger 模式；口径以
// console-ux §10.6 v1.9 为唯一权威定义，本脚本是该口径的可执行实现）：
//
//   src   第一方渲染 DOM 的锚落点：web/src 全量（data-testid="…" / testid="…"
//         prop / prop 缺省值 / itemTestid 回调 / 'data-testid' 对象键条件展开）
//         + web/scripts/mock-idp.mjs（第一方 IdP 模拟页——idp-login-* 锚的真身）
//   spec  web/e2e 全量 .ts 引用（[data-testid=…] / getByTestId / testid:；
//         README 等 .md 不在口径内）
//   册    docs/design/console-ux.md §10 全部锚名（{a|b} 备选展开、<动态段>
//         家族化）；§10.4 未落地白名单与 §10.6 退役总表由 --ledger 从册解析
//
// 四张表（桶定义见 §10.6）：
//   unregistered  src 有、册无（违反「先入册再落码」——硬违规）
//   retired       册有、src 无（必须落入 §10.6 退役表或 §10.4 白名单）
//   dead          src 有、spec 零消费（处置队列底稿——视图，不落册）
//   broken        spec 有、src 无（断链——硬门 = 0）
//
// 家族匹配：`repos-row-${key}`（src）/ `repos-row-<repoKey>`（册）/
// `repos-row-docker-local`（spec）统一归一为家族 `repos-row-*`；动态族前缀
// 覆盖同前缀的一切具体名（族内任一具体引用即视为消费全族——§10.6 掩蔽语义）。
//
// --ledger 模式（T-267）：解析 §10.4 白名单 + §10.6 退役总表，做册↔实态双向
// 断言，违例非零退出（qa 硬门）：
//   A1 unregistered = 0            A2 broken = 0
//   A3 retired 桶 ⊆ 退役表 ∪ 白名单（禁止静默退役）
//   A4 退役表条目 ∩ src 家族 = ∅（退役条目不得仍在 src，按家族精确名判）

import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const LEDGER = process.argv.includes('--ledger')

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..')
const SRC_DIRS = [join(ROOT, 'web', 'src')]
// 第一方渲染 DOM 的锚，但源码不在 web/src（IdP 模拟页由 e2e 直连渲染）
const SRC_FILES = [join(ROOT, 'web', 'scripts', 'mock-idp.mjs')]
const E2E = [join(ROOT, 'web', 'e2e')]
const DOC = join(ROOT, 'docs', 'design', 'console-ux.md')

function walk(dir, exts, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, exts, out)
    else if (exts.some((e) => name.endsWith(e))) out.push(p)
  }
  return out
}

const PREFIX = (fam) => fam.slice(0, -1) // 'tree-node-*' -> 'tree-node-'

/** 模板/动态名 → 家族名：`tree-node-${p}` / `tree-node-<p>` → `tree-node-*`。
 *  T-267 修复：原截断正则只认 `${` 与 `</`，从不匹配 `<x>` 角括号——册侧
 *  角括号形态（`upload-file-<i>` 等）以原始名进桶，--ledger A3 的精确名
 *  比对必然错配。现于首个 `$` / `<` / `{` 处截断。 */
function toFamily(name) {
  const cut = name.search(/[$<{]/)
  if (cut > 0) return name.slice(0, cut).replace(/-+$/, '') + '-*'
  return name
}

// ---- src 锚（data-testid="…" + testid="…" prop 形态 + prop 缺省值） ----
const srcAnchors = new Map() // family -> { file, count }
function addSrc(fam, rel) {
  const cur = srcAnchors.get(fam) ?? { file: rel, count: 0 }
  cur.count++
  srcAnchors.set(fam, cur)
}
const srcFiles = [...walk(SRC_DIRS[0], ['.tsx', '.ts']), ...SRC_FILES]
for (const f of srcFiles) {
  const text = readFileSync(f, 'utf8')
  const rel = f.slice(ROOT.length + 1)
  // 注：值类一律排除换行——注释里折行的 data-testid 字面量不得进家族集
  for (const m of text.matchAll(/(?:data-testid|testid)=\{?["'`]([^"'`\n]+)["'`]/g)) {
    addSrc(toFamily(m[1]), rel)
  }
  // prop 缺省值形态：testid = 'empty-state'（解构缺省，渲染位在 data-testid={testid}）。
  // 负向后顾排除 data-testid="…"（其内含的 testid 子串不得重复计数）
  for (const m of text.matchAll(/(?<![-a-z])testid\s*=\s*['"`]([a-z0-9-]+)['"`]/g)) {
    addSrc(m[1], rel)
  }
  // itemTestid 回调形态（TransferBox：条目 testid 由回调拼出）
  for (const m of text.matchAll(/itemTestid=\{[^`]*`([^`\n]+)`/g)) {
    addSrc(toFamily(m[1]), rel)
  }
  // 对象键条件展开形态（widgets：{ 'data-testid': `user-status-${name}` }——
  // T-267 盲区修复：三元双臂以对象键铺开时前两条正则均不可见）
  for (const m of text.matchAll(/['"]data-testid['"]\s*:\s*`([^`\n]+)`/g)) {
    addSrc(toFamily(m[1]), rel)
  }
}
// widgets.PermSummaryTable 的 ${rowTestidPrefix} 动态前缀：调用方实参
// group-perm / user-perm（GroupsPage/UserDetailPage），拼出 §10.3 在册三族
//（行族 -row-* 已随 T-267 死锚退役移除，仅 -matrix 表体仍渲染）
addSrc('group-perm-matrix', 'pages/security/widgets.tsx (rowTestidPrefix=group-perm)')
addSrc('user-perm-matrix', 'pages/security/widgets.tsx (rowTestidPrefix=user-perm)')
addSrc('perm-matrix', 'pages/security/PermissionEditorPage.tsx (kind===users 臂)')
addSrc('perm-matrix-groups', 'pages/security/PermissionEditorPage.tsx (kind===groups 臂)')

// ---- spec 引用 ----
const specRefs = new Set()
const specRaw = []
for (const dir of E2E) {
  for (const f of walk(dir, ['.ts'])) {
    const text = readFileSync(f, 'utf8')
    const rel = f.slice(ROOT.length + 1)
    for (const m of text.matchAll(
      /(?:data-testid="([^"$]+)"|getByTestId\(\s*['"`]([^'"`$][^'"`]*)['"`]|testid:\s*['"`]([^'"`$][^'"`]*)['"`])/g,
    )) {
      const raw = m[1] ?? m[2] ?? m[3]
      if (!raw) continue
      specRaw.push({ raw, spec: rel })
      specRefs.add(toFamily(raw))
    }
  }
}

/** 具体引用是否命中 src 家族集合（静态相等或动态族前缀） */
function hitsFamily(ref, fams) {
  if (fams.has(ref) || fams.has(toFamily(ref))) return true
  for (const fam of fams.keys()) {
    if (fam.endsWith('-*') && ref.startsWith(PREFIX(fam))) return true
  }
  return false
}

// 组件逻辑自消费（§10.6 口径）：src 内 querySelector 等选择器引用的锚不算死锚
// ——移除会破坏运行时行为（Tab 焦点管理 / 树深链滚动定位）。模板选择器取其
// 字面前缀（`[data-testid="smu-tab-${next}"]` → 前缀 smu-tab-，覆盖同前缀
// 的一切家族）；静态选择器按精确名。
const selectorRefs = new Set()
const selectorPrefixes = new Set()
for (const f of srcFiles) {
  const text = readFileSync(f, 'utf8')
  for (const m of text.matchAll(/\[data-testid="([^"$"\n]*)(\$|")/g)) {
    if (!m[1]) continue
    if (m[2] === '$') selectorPrefixes.add(m[1])
    else selectorRefs.add(toFamily(m[1]))
  }
}

// ---- 册（console-ux §10；{a|b} 展开 + <…> 家族化） ----
const doc = readFileSync(DOC, 'utf8')
const sec10 = doc.slice(doc.indexOf('## 10. data-testid'))
const docTokens = new Set()
for (let line of sec10.split('\n')) {
  // 展开 {a|b|c} 备选段（form-rclass-{local|remote|virtual} 等）：对每个
  // 备选组做笛卡尔替换（组内首段替换后剩余组继续）。内容类含数字/连字符
  //（password2、copy-repo-path 一类的备选段）
  let variants = [line]
  for (const g of line.matchAll(/\{([a-z0-9|-]+)\}/g)) {
    const alts = g[1].split('|')
    variants = variants.flatMap((v) => alts.map((a) => v.replace(g[0], a)))
  }
  for (const v of variants) {
    // 注意 <…> 内容类不得含空格（否则 tree-node-<path> 会吞掉同行后续锚名）；
    // T-267 修复：内容类补数字（<sha256|sha1|md5>——否则 node-copy 截成裸名，
    // 与退役表的 node-copy-* 家族形错配）
    for (const m of v.matchAll(/[a-z][a-z0-9]*(?:-[a-z0-9]+)*(?:-<[A-Za-z0-9|.{}-]+>)*/g)) {
      docTokens.add(m[0])
    }
  }
}
const STOP = new Set([
  'data-testid', 'testid', 'console-ux', 'console-m8', 'playwright', 'react', 'hooks',
  'aria-label', 'aria-sort', 'aria-current', 'aria-expanded', 'aria-selected',
  'aria-valuenow', 'aria-valuemin', 'aria-valuemax', 'kebab-case', 'tabindex',
  'props', 'getbytestid', 'copy-field', 'page-element',
  // §10 行文里的非锚连词词（人工核对过的假阳性）
  'auth-shell', 'ci-bot', 'dev-frontend', 'm-holder', 'readonly-admin', 'admin-only',
  'nav-item', 'empty-state-page', 'role-menuitem', 'keyset', 'scope-col', 'theme-smoke',
  'anchor-audit', 'm7-done',
  // §10.6 v1.9 行文新增的非锚词（web/scripts/mock-idp.mjs 的文件名）
  'mock-idp',
])
const docFams = new Set()
for (const t of docTokens) {
  if (STOP.has(t)) continue
  if (!(t.includes('-') || ['app-boot', 'app-nav', 'skeleton', 'toast'].includes(t))) continue
  if (t.length <= 3) continue
  docFams.add(toFamily(t))
}
// 单词锚（无连字符）在停词规则外显式补录（§10.2 行文「dashboard（页面根）」等）
for (const w of ['tree-page', 'dashboard', 'settings', 'toast', 'skeleton', 'login-page']) docFams.add(w)

/** 宽松命中：册的行文短写（quota-bar）≈ src 动态族（quota-bar-*）；
 *  src 的非后缀动态前缀（${rowTestidPrefix}-matrix）由本函数人工映射补：
 *  widgets.tsx 的 PermSummaryTable 以 prop 前缀渲染 perm-matrix /
 *  group-perm-matrix / user-perm-matrix 三族（§10.3 已在册）。 */
const DYNAMIC_PREFIX_ALIASES = new Set([
  'perm-matrix', 'group-perm-matrix', 'user-perm-matrix',
  'perm-matrix-cell-*',
])
function docMatchesSrc(df, sf) {
  if (df === sf) return true
  if (df.endsWith('-*') && sf.startsWith(PREFIX(df))) return true
  if (sf.endsWith('-*') && df.startsWith(PREFIX(sf))) return true
  if (sf.endsWith('-*') && df === sf.slice(0, -2)) return true // 行文短写 ≈ 动态族
  return false
}

// ---- 册侧结构化段落（--ledger）：§10.4 白名单 + §10.6 退役总表 ----

/** 取 §10.x 小节全文（至下一个 ### / ## 标题或文末） */
function subsection(title) {
  const at = doc.indexOf(title)
  if (at < 0) throw new Error(`册解析失败：找不到 ${title}`)
  const rest = doc.slice(at)
  const end = rest.slice(title.length).search(/\n#{2,3} /)
  return end < 0 ? rest : rest.slice(0, title.length + end)
}

/** 表格首列反引号锚名 → 家族集 */
function tableCol1Families(section) {
  const out = new Set()
  for (const line of section.split('\n')) {
    if (!line.startsWith('|')) continue
    const col1 = line.split('|')[1] ?? ''
    for (const m of col1.matchAll(/`([^`]+)`/g)) out.add(toFamily(m[1]))
  }
  return out
}

const planned = tableCol1Families(subsection('### 10.4'))
const retiredRegistered = tableCol1Families(subsection('### 10.6'))

// ---- 对账 ----
const srcFams = new Set(srcAnchors.keys())

const unregistered = [...srcFams]
  .filter((f) => {
    if (docFams.has(f)) return false
    if (DYNAMIC_PREFIX_ALIASES.has(f)) return false // widgets prop 前缀渲染，册上有
    if (f.startsWith('${rowTestidPrefix}')) return false // 同上：原始模板族不计未入册
    for (const df of docFams) {
      if (docMatchesSrc(df, f)) return false
    }
    return true
  })
  .sort()

// §10.4 未落地锚（src 缺位是设计态，不算 retired）
const retired = [...docFams]
  .filter((f) => {
    if (srcFams.has(f)) return false
    if (DYNAMIC_PREFIX_ALIASES.has(f)) return false
    for (const sf of srcFams) {
      if (docMatchesSrc(f, sf)) return false
    }
    return !planned.has(f)
  })
  .sort()

const dead = [...srcFams]
  .filter((f) => {
    if (specRefs.has(f)) return false
    if (selectorRefs.has(f)) return false
    if ([...selectorPrefixes].some((p) => f.startsWith(p))) return false
    for (const ref of specRaw) {
      if (ref.raw === f) return false
      if (f.endsWith('-*') && ref.raw.startsWith(PREFIX(f))) return false
    }
    return true
  })
  .sort()

const broken = [...new Set(specRaw.map((r) => r.raw))]
  .filter((raw) => !hitsFamily(raw, srcFams))
  .sort()

// ---- 输出 ----
const fmt = (list, withSrc) =>
  list.map((x) => (withSrc ? `${x}  (${srcAnchors.get(x)?.file})` : x)).join('\n  ')

const registered = [...srcFams].filter((f) => {
  if (docFams.has(f) || DYNAMIC_PREFIX_ALIASES.has(f)) return true
  for (const df of docFams) if (docMatchesSrc(df, f)) return true
  return false
})

console.log(`src 锚家族：${srcFams.size}（落点 ${[...srcAnchors.values()].reduce((a, v) => a + v.count, 0)}）`)
console.log(`spec 引用家族：${specRefs.size}（具体引用 ${specRaw.length}）`)
console.log(`册锚家族：${docFams.size}；registered ${registered.length} / 白名单 ${planned.size} / 退役表 ${retiredRegistered.size}`)
console.log(`\n== unregistered（src 有、册无）==\n  ${fmt(unregistered, true) || '（无）'}`)
console.log(`\n== retired（册有、src 无）==\n  ${fmt(retired) || '（无）'}`)
console.log(`\n== dead（src 有、spec 零消费；${dead.length} 家族——处置队列底稿）==\n  ${fmt(dead, true) || '（无）'}`)
console.log(`\n== broken（spec 引用、src 无——断链，必须修）==\n  ${fmt(broken) || '（无）'}`)

// ---- --ledger 断言（qa 硬门；违例逐条打印并以非零码退出） ----
if (LEDGER) {
  const violations = []
  // A3/A4 基名归一：表 token 常为 `-*` 家族形（upload-file-*），retired 桶成员可能为
  // 裸基名（upload-file）——精确 has 会错配。looseMatch 的行文短写规则在此同样适用。
  const base = (t) => t.replace(/-\*$/, '')
  const covered = (f) => retiredRegistered.has(f) || planned.has(f)
    || retiredRegistered.has(f + '-*') || planned.has(f + '-*')
    || [...retiredRegistered, ...planned].some((t) => t.endsWith('-*') && base(t) === f)
  if (unregistered.length > 0) violations.push(`A1 unregistered ≠ 0：${unregistered.join(', ')}`)
  if (broken.length > 0) violations.push(`A2 broken ≠ 0：${broken.join(', ')}`)
  for (const f of retired) {
    if (!covered(f)) {
      violations.push(`A3 静默退役/漂移：${f}（册有 src 无，但既不在 §10.6 退役表也不在 §10.4 白名单）`)
    }
  }
  for (const f of retiredRegistered) {
    if (srcFams.has(f)) violations.push(`A4 退役条目仍在 src：${f}`)
  }
  console.log(`\n== ledger 断言（A1 unregistered=0 / A2 broken=0 / A3 retired⊆退役表∪白名单 / A4 退役表∩src=∅）==`)
  if (violations.length) {
    console.log(`  FAIL（${violations.length} 条）：\n  ${violations.join('\n  ')}`)
    process.exit(1)
  }
  console.log('  PASS（registered 与册一致：unregistered/broken 双零，retired 全部在表）')
}
