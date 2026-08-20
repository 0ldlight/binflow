// gen-pathmatch-fixtures.mjs — 权限模式测试器的同源 fixtures 生成器
//（console-ux §9-R8 / T-101 AC②：判定向量与 internal/auth pathmatch 的
// table-driven 用例同源，漂移即红）。
//
//   node scripts/gen-pathmatch-fixtures.mjs           # 重新生成（写文件）
//   node scripts/gen-pathmatch-fixtures.mjs --check   # 只比对，漂移退出码 1
//
// 解析 internal/auth/pathmatch_test.go 的 TestPathMatcherMatch 表（Go 复合
// 字面量 {pattern, path, want}），生成 web/src/pages/security/
// pathmatch.fixtures.ts。Go 侧用例增删后重跑本脚本；--check 供 CI 挂钩
//（CI 接线归 T-89 面，见 T-101 日志遗留）。

import { readFileSync, writeFileSync } from 'node:fs'

const SRC = new URL('../../internal/auth/pathmatch_test.go', import.meta.url)
const OUT = new URL('../src/pages/security/pathmatch.fixtures.ts', import.meta.url)

const check = process.argv.includes('--check')

const go = readFileSync(SRC, 'utf8')
const fnStart = go.indexOf('func TestPathMatcherMatch')
if (fnStart < 0) throw new Error('TestPathMatcherMatch not found in pathmatch_test.go')
const body = go.slice(fnStart, go.indexOf('\n}\n', fnStart))

// 表项形如 {"ci-out/**", "ci-out/y.bin", true},（行内注释 tolerated）
const ROW = /^\s*\{"((?:[^"\\]|\\.)*)",\s*"((?:[^"\\]|\\.)*)",\s*(true|false)\},/
const unquote = (raw) => {
  try {
    return JSON.parse(`"${raw}"`)
  } catch {
    throw new Error(`cannot unescape Go string literal segment: ${raw}`)
  }
}

const rows = []
for (const line of body.split('\n')) {
  const m = ROW.exec(line)
  if (!m) continue
  rows.push({ pattern: unquote(m[1]), path: unquote(m[2]), want: m[3] === 'true' })
}
if (rows.length < 30) throw new Error(`parsed only ${rows.length} rows — table format drifted?`)

const HEADER = `// GENERATED FILE — 不要手改。
// 事实源：internal/auth/pathmatch_test.go（TestPathMatcherMatch 表）。
// 再生成：cd web && node scripts/gen-pathmatch-fixtures.mjs
// 漂移检查：cd web && node scripts/gen-pathmatch-fixtures.mjs --check（CI 挂钩归 T-89 面）
// 消费者：e2e/pathmatch-parity.spec.ts（前端 pathmatch.ts 与 Go 判定逐条一致的断言锚）。

export interface PathMatchFixture {
  pattern: string
  path: string
  want: boolean
}

export const pathMatchFixtures: PathMatchFixture[] = [
`

const entry = (r) =>
  `  { pattern: ${JSON.stringify(r.pattern)}, path: ${JSON.stringify(r.path)}, want: ${r.want} },`
const next = `${HEADER}${rows.map(entry).join('\n')}\n]\n`

if (check) {
  const cur = readFileSync(OUT, 'utf8')
  if (cur !== next) {
    console.error(`drift: ${OUT.pathname} differs from internal/auth/pathmatch_test.go table`)
    console.error('regenerate with: cd web && node scripts/gen-pathmatch-fixtures.mjs')
    process.exit(1)
  }
  console.log(`ok: ${rows.length} fixtures in sync with pathmatch_test.go`)
  process.exit(0)
}

writeFileSync(OUT, next)
console.log(`wrote ${rows.length} fixtures -> ${OUT.pathname}`)
