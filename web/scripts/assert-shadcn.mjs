#!/usr/bin/env node
// shadcn/ui 硬门：生产 JSX 不得绕过 components/ui 手写交互/表格原语。
// AG Grid 是虚拟数据网格专用引擎（shadcn 无对应 data-grid primitive）；
// 其外层按钮、筛选、弹层与状态仍必须消费 components/ui/*。
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src')
const primitives = new Set(['button', 'input', 'select', 'textarea', 'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td'])
const violations = []

function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      if (path.relative(root, file) === 'components/ui') continue
      walk(file)
      continue
    }
    if (!entry.isFile() || !/\.tsx$/.test(entry.name)) continue
    const source = fs.readFileSync(file, 'utf8')
    const rel = path.relative(process.cwd(), file)
    for (const tag of primitives) {
      const re = new RegExp(`<${tag}(?:\\s|/>|>)`, 'g')
      if (re.test(source)) violations.push(`${rel}: raw <${tag}>`)
    }
    const choiceRe = /<Input\b(?:(?!\/>)[\s\S])*?type=["'](?:checkbox|radio)["']/g
    if (choiceRe.test(source)) violations.push(`${rel}: Input checkbox/radio must use Checkbox/RadioGroup`)
  }
}

walk(root)
if (violations.length) {
  console.error(`assert-shadcn: FAIL (${violations.length})\n${[...new Set(violations)].join('\n')}`)
  process.exit(1)
}
console.log('assert-shadcn: OK — production JSX consumes shadcn/ui primitives')
