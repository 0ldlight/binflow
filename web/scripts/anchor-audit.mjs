#!/usr/bin/env node
// 锚册三方对账（T-244 立、T-267 口径单一权威化 + --ledger 模式；口径以
// console-ux §10.6 v1.9 为唯一权威定义，本脚本是该口径的可执行实现）：
//
//   src   第一方渲染 DOM 的锚落点：web/src 全量（data-testid="…" / testid="…"
//         prop / prop 缺省值 / itemTestid 回调 / 'data-testid' 对象键条件展开）
//         + web/scripts/mock-idp.mjs（第一方 IdP 模拟页——idp-login-* 锚的真身）
//   spec  web/e2e 全量 .ts 引用（[data-testid=…] 三种引号 × ^=/$=/*= 算子 ×
//         ${} 模板 / getByTestId / testid: / toHaveAttribute·toMatch·toBe 值
//         断言形；README 等 .md 不在口径内）
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
  // 注：值类一律排除换行——注释里折行的 data-testid 字面量不得进家族集。
  // T-274 补形：原正则 = 后不允许空白且值类沿用旧引号集，两形态漏收——
  // ① `const testid = \`repos-usage-${repoKey}\`` 变量模板赋值（渲染位
  //    data-testid={testid}，运行时锚真实存在，T-267 期双向隐形）；
  // ② prop 缺省 `testid = 'empty-state'`（原由第二条正则单独承载，现并入，
  //    避免同落点双计）。
  for (const m of text.matchAll(/(?:data-testid|testid)\s*=\s*\{?["'`]([^"'`\n]+)["'`]/g)) {
    addSrc(toFamily(m[1]), rel)
  }
  // itemTestid 回调形态（TransferBox：条目 testid 由回调拼出）
  for (const m of text.matchAll(/itemTestid=\{[^`]*`([^`\n]+)`/g)) {
    addSrc(toFamily(m[1]), rel)
  }
  // 对象键条件展开形态（widgets：{ 'data-testid': `user-status-${name}` }——
  // T-267 盲区修复：三元双臂以对象键铺开时前两条正则均不可见）。
  // T-299 补形：值类从仅反引号模板扩至引号字面量——MUI 迁移后锚经
  // slotProps 对象下沉到 input/select 本体（{ htmlInput: { 'data-testid':
  // 'login-username' } }），字面量形态成为主流落点（工具局限史同款教训：
  // 判定域外的锚会让「册有 src 无」假阳性）。
  for (const m of text.matchAll(/['"]data-testid['"]\s*:\s*(['"`])([^'"`\n]+)\1/g)) {
    addSrc(toFamily(m[2]), rel)
  }
  // T-307 补形：字段册属性形态（authconfig/sections.ts 的 anchor: 'name' ——
  // 数据驱动表单的锚以对象属性字面量落点，渲染位 data-testid={field.anchor}
  // 经变量透传，前三条正则均不可见；setAnchor 同理（secret「已设置」提示行
  // 的独立锚）。'anchor' 键全库仅该文件使用（grep 自证）——域外零外溢。
  for (const m of text.matchAll(/\b(?:anchor|setAnchor)\s*:\s*(['"`])([^'"`\n]+)\1/g)) {
    addSrc(toFamily(m[2]), rel)
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
// T-274 补形（DEFECT-1 根因①）：原正则只认双引号字面 `data-testid="x"` 且排除
// `$`——e2e 的两大主流引用形态对其不可见：模板串 `[data-testid="repos-row-${key}"]`
// （外层反引号、内层引号 + 插值）与前缀选择器 `[data-testid^="repos-row-"]`。
// 全部 19 活族 / 数十处动态引用隐形 → T-267 按 dead 桶误杀（T-274 回填）。
// 现覆盖六种形态：
//   S1 属性选择器：三种引号 × ^=/$=/*= 算子 × ${} 插值段（模板值由 toFamily
//      在首个动态段截断；前缀算子的截值按前缀命中家族）
//   S2 getByTestId(…)（含模板串）
//   S3 testid: 选项形
//   S4 toHaveAttribute('data-testid', 值)——属性值断言也是消费（usage-fanout
//      的排序腿靠它锁 repos-row-*）
//   S5 toMatch(/^前缀-) 值比对（keyboard 腿对 activeElement testid 的前缀断言）
//   S6 (not.)toBe(`前缀-${…}`) 模板值比对（同上，窄门：前缀含连字符，防普通
//      字符串断言进桶制造伪 broken）
// 纯透传形（值以 `${` 开头，如 helper 的 `[data-testid="${anchor}"]`）跳过：
// 锚名来自调用方实参，本文件不可解析，计入只会制造伪 broken。
const REF_RES = [
  /data-testid\s*(?:\^|\*|\$)?=\s*(["'`])([^"'`\n]+)\1/g,
  /getByTestId\(\s*(["'`])([^'"`\n]+)\1/g,
  /testid:\s*(["'`])([^'"`\n]+)\1/g,
  /toHaveAttribute\(\s*(['"])data-testid\1\s*,\s*(["`])([^'"`\n]+)\2/g,
  /\.toMatch\(\s*\/\^([a-z][a-z0-9]*(?:-[a-z0-9]+)*-)/g,
  /\.\s*(?:not\s*\.)?toBe\(\s*([`"'])([a-z][a-z0-9]*(?:-[a-z0-9]+)*-\$\{[^`"']*)\1/g,
]

/** spec 侧家族归一：模板/动态名走 toFamily；前缀形（^= 截值与 toMatch 前缀，
 *  特征 = 尾连字符）归一为 `前缀-*`，与 dead/broken 的前缀命中语义一致。 */
function specFamily(raw) {
  if (raw.endsWith('-')) return raw.slice(0, -1) + '-*'
  return toFamily(raw)
}

const specRefs = new Set()
const specRaw = []
for (const dir of E2E) {
  for (const f of walk(dir, ['.ts'])) {
    const text = readFileSync(f, 'utf8')
    const rel = f.slice(ROOT.length + 1)
    for (const re of REF_RES) {
      re.lastIndex = 0
      for (const m of text.matchAll(re)) {
        // 各形态的捕获组位置不同：S1/S2/S3/S4/S6 取最后一个「值」组，S5 单组
        const raw = m[3] ?? m[2] ?? m[1]
        if (!raw || raw.startsWith('${')) continue
        specRaw.push({ raw, spec: rel })
        specRefs.add(specFamily(raw))
      }
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
  // v1.15（T-352/T-353 行文假阳性）：回收站批的标识符引用——存储常量 /
  // REST 参数 / CSS 类钩子 / 文档文件名段，均非 testid 锚
  'auto-trashcan', 'transaction-size', 'confirm-input', 'trash-can',
  // v1.17（T-372 行文假阳性）：树尾入口批的 spec 文件名段，非 testid 锚
  't372-trash-node', 'artifacts-tree',
  // v1.18（T-382 行文假阳性）：抽屉化批引用的上游规范文件名段，非 testid 锚
  'console-artifactory-parity',
  // v1.21（T-389 行文假阳性）：品牌位批的标识符引用——CSS 类钩子（app-nav-brand，
  // 结构钩子非 testid）/ token 名（bf-bg）/ 资产文件名段（mark-dark、
  // lockup-horizontal、lockup-dark）/ 目录与脚本 spec 文件名段（docs-site、
  // wire-brand-assets、t389-brand），均非 testid 锚
  'app-nav-brand', 'bf-bg', 'docs-site', 'mark-dark', 'lockup-horizontal', 'lockup-dark',
  'wire-brand-assets', 't389-brand',
  // v1.22（T-387 行文假阳性）：L1 批的标识符引用——aria 属性名（批次语义
  // 描述，非锚——aria-expanded 等同款先例在册）/ localStorage 键名
  //（binflow-console-cols-{repos,audit}，偏好面非锚）/ CSS 类钩子
  //（filter-bar）/ 措辞连词（per-page、recent-searches）/ spec 文件名段
  //（t387-l1-columns），均非 testid 锚
  'aria-checked', 'aria-disabled', 'aria-haspopup', 'aria-hidden',
  'binflow-console-cols-audit', 'binflow-console-cols-repos',
  'filter-bar', 'per-page', 'recent-searches', 't387-l1-columns',
  // v1.24（T-388 行文假阳性）：空态/图标批的标识符引用——属性名（data-icon，
  // 图标身份属性非 testid——PkgIcon data-icon 同款先例）/ 许可证串的分词
  // 残段（Apache-2.0 的 'pache-2'）/ spec 文件名段（t388-f2n2），均非锚
  'data-icon', 'pache-2', 't388-f2n2',
  // v1.25（T-404 行文假阳性）：复制 CRUD 批的标识符引用——ConfirmDialog
  // 的 prop 名（confirm-disabled，语义描述非锚）/ 深链聚焦属性（data-active，
  // data 属性非 testid）/ R5 锚定引用的 Artifactory CSS 类名（icon-run）/
  // 措辞连词（flip-off）/ `repo-repl-*` 星号速记的截断残段（族内实名
  // repo-repl-card 等均在册）/ spec 文件名段（t404-replication-crud、
  // repositories-admin——m8 迁移腿所在文件），均非锚
  'confirm-disabled', 'data-active', 'icon-run', 'flip-off', 'repo-repl',
  't404-replication-crud', 'repositories-admin',
  // v1.27（T-414 行文假阳性）：列选器三页推广批的标识符引用——CSS 类钩子
  // （member-pop / row-link，浮层与行内链接的结构钩子非锚）/ CSS 函数名
  // （color-mix，T-391 配方描述）/ localStorage 键名（binflow-console-cols-
  // {users,groups,search}，偏好面非锚——v1.22 两键先例同款）/ spec 文件名段
  // （t414-columns-promo），均非 testid 锚
  'member-pop', 'row-link', 'color-mix',
  'binflow-console-cols-users', 'binflow-console-cols-groups', 'binflow-console-cols-search',
  't414-columns-promo',
  // v1.33（T-439 行文假阳性）：三段步进批的标识符引用——Artifactory 侧
  // CSS 类名（jf-steps，活体形态描述非锚）/ 措辞连词（as-built——
  // 「翻正 · 已落」行的 as-built 注定语、decode-only——transport 只解码
  // 不转发的两档定档用词），均非 testid 锚
  'jf-steps', 'as-built', 'decode-only',
  // v1.34（T-441 行文假阳性）：翻转③批的标识符引用——对比度配方的
  // token 名（surface-2，磁贴底色描述非锚）/ spec 文件名段
  // （t441-pkg-modal-open——本票新 spec 的文件名），均非 testid 锚
  'surface-2', 't441-pkg-modal-open',
  // v1.35（T-443 行文假阳性）：入口分路由批的标识符引用——dirty 判定的
  // 措辞连词（dirty-gating——票面 AC 用词）/ push-only 口径注记的缩写
  // （ADR-0021/R10 行文）/ spec 文件名段（t443-list-entry-dirty-test
  // ——本票新 spec 的文件名），均非 testid 锚
  'dirty-gating', 'push-only', 't443-list-entry-dirty-test',
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
