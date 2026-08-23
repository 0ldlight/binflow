#!/usr/bin/env node
// 锚册三方对账（T-244，console-ux §10 v1.7 的册↔src↔spec 对账器）：
//
//   src   web/src 全量 testid 落点（data-testid="…" 与 testid="…" prop 形态，
//         静态 + 模板前缀）
//   spec  web/e2e 全量 testid 引用（[data-testid=…] / getByTestId / testid:）
//   册    docs/design/console-ux.md §10 全部锚名（{a|b} 备选展开、<动态段>
//         家族化）
//
// 输出四张表：
//   unregistered  src 有、册无（违反「先入册再落码」——入册或退役）
//   retired       册有、src 无（已退役/漂移——需显式退役条目或回册；
//                 §10.4 未落地锚白名单豁免）
//   dead          src 有、spec 零消费（死锚——册上死锚清单的底稿）
//   broken        spec 有、src 无（断链——spec 对空断言，必须修）
//
// 家族匹配：`repos-row-${key}`（src）/ `repos-row-<repoKey>`（册）/
// `repos-row-docker-local`（spec）统一归一为家族 `repos-row-*`。

import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..')
const SRC = join(ROOT, 'web', 'src')
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

/** 模板/动态名 → 家族名：`tree-node-${p}` / `tree-node-<p>` → `tree-node-*` */
function toFamily(name) {
  const cut = name.search(/\$\{|</)
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
for (const f of walk(SRC, ['.tsx', '.ts'])) {
  const text = readFileSync(f, 'utf8')
  const rel = f.slice(ROOT.length + 1)
  for (const m of text.matchAll(/(?:data-testid|testid)=\{?["'`]([^"'`]+)["'`]/g)) {
    addSrc(toFamily(m[1]), rel)
  }
  // prop 缺省值形态：testid = 'empty-state'（解构缺省，渲染位在 data-testid={testid}）。
  // 负向后顾排除 data-testid="…"（其内含的 testid 子串不得重复计数）
  for (const m of text.matchAll(/(?<![-a-z])testid\s*=\s*['"`]([a-z0-9-]+)['"`]/g)) {
    addSrc(m[1], rel)
  }
  // itemTestid 回调形态（TransferBox：条目 testid 由回调拼出——T-243 对账
  // 口径明示包含「itemTestid 回调/三元构造的动态族」）
  for (const m of text.matchAll(/itemTestid=\{[^`]*`([^`]+)`/g)) {
    addSrc(toFamily(m[1]), rel)
  }
}
// widgets.PermSummaryTable 的 ${rowTestidPrefix} 动态前缀：调用方实参
// group-perm / user-perm（GroupsPage/UserDetailPage），拼出 §10.3 在册三族
addSrc('group-perm-matrix', 'pages/security/widgets.tsx (rowTestidPrefix=group-perm)')
addSrc('group-perm-row-*', 'pages/security/widgets.tsx')
addSrc('user-perm-matrix', 'pages/security/widgets.tsx (rowTestidPrefix=user-perm)')
addSrc('user-perm-row-*', 'pages/security/widgets.tsx')
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
    // 注意 <…> 内容类不得含空格（否则 tree-node-<path> 会吞掉同行后续锚名）
    for (const m of v.matchAll(/[a-z][a-z0-9]*(?:-[a-z0-9]+)*(?:-<[A-Za-z|.{}-]+>)*/g)) {
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
  'perm-matrix-cell-*', 'perm-matrix-remove-*',
  'user-perm-row-*', 'group-perm-row-*',
])
function docMatchesSrc(df, sf) {
  if (df === sf) return true
  if (df.endsWith('-*') && sf.startsWith(PREFIX(df))) return true
  if (sf.endsWith('-*') && df.startsWith(PREFIX(sf))) return true
  if (sf.endsWith('-*') && df === sf.slice(0, -2)) return true // 行文短写 ≈ 动态族
  return false
}

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
const NOT_LANDED = [
  'tokens-page', 'token-create', 'token-plaintext', 'token-revoke-*', 'audit-export',
  'search-filter-package', 'search-filter-type', 'copy-*',
]
const retired = [...docFams]
  .filter((f) => {
    if (srcFams.has(f)) return false
    if (DYNAMIC_PREFIX_ALIASES.has(f)) return false
    for (const sf of srcFams) {
      if (docMatchesSrc(f, sf)) return false
    }
    return !NOT_LANDED.includes(f)
  })
  .sort()

const dead = [...srcFams]
  .filter((f) => {
    if (specRefs.has(f)) return false
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

console.log(`src 锚家族：${srcFams.size}（落点 ${[...srcAnchors.values()].reduce((a, v) => a + v.count, 0)}）`)
console.log(`spec 引用家族：${specRefs.size}（具体引用 ${specRaw.length}）`)
console.log(`册锚家族：${docFams.size}`)
console.log(`\n== unregistered（src 有、册无）==\n  ${fmt(unregistered, true) || '（无）'}`)
console.log(`\n== retired（册有、src 无）==\n  ${fmt(retired) || '（无）'}`)
console.log(`\n== dead（src 有、spec 零消费；${dead.length} 家族）==\n  ${fmt(dead, true) || '（无）'}`)
console.log(`\n== broken（spec 引用、src 无——断链，必须修）==\n  ${fmt(broken) || '（无）'}`)
