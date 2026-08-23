#!/usr/bin/env node
// T-234 编译期断言：组件层 css 零硬编码色值（ADR-0029 自有皮肤纪律）。
//
// tokens.css 是唯一允许出现色值字面量的文件（token 定义层）；其余
// src/styles/*.css 中出现 hex / rgb( / rgba( / hsl( / hsla( 字面量即
// 失败——组件层只能经 var(--bf-*) 引色（transparent 关键字与
// color-mix(... var(--bf-*) ...) 不受限）。挂在 build 前置：
//   npm run assert:tokens   # 单独执行
//   npm run build           # 自动前置执行
import { readdirSync, readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const stylesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'styles')
const files = readdirSync(stylesDir)
  .filter((f) => f.endsWith('.css') && f !== 'tokens.css')
  .sort()

// 色值字面量形态：#rgb…#rrggbbaa / rgb( / rgba( / hsl( / hsla(
const LITERAL = /#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(/g

const failures = []
for (const f of files) {
  const lines = readFileSync(join(stylesDir, f), 'utf8').split('\n')
  lines.forEach((line, i) => {
    for (const m of line.matchAll(LITERAL)) {
      failures.push(`${f}:${i + 1}: 硬编码色值 "${m[0]}"（组件层只允许 var(--bf-*) / transparent）`)
    }
  })
}

if (failures.length > 0) {
  console.error(`assert-tokens: FAIL — ${failures.length} 处硬编码色值：`)
  for (const f of failures) console.error(`  ${f}`)
  process.exit(1)
}
console.log(`assert-tokens: OK — ${files.length} 个组件 css 零硬编码色值（${files.join(', ')}）`)
