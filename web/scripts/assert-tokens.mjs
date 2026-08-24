#!/usr/bin/env node
// T-234 编译期断言：组件层 css 零硬编码色值（ADR-0029 自有皮肤纪律）。
// T-264 (FR-82-AC4) 扩面：src/ 下全部 css（styles 共享层 + components/ +
// pages/** 页面级分片），tokens.css 仍为唯一色值字面量豁免层（token 定义层）。
// 其余 css 的声明中出现 hex / rgb( / rgba( / hsl( / hsla( 字面量即失败——
// 组件层只能经 var(--bf-*) 引色（transparent 关键字与
// color-mix(... var(--bf-*) ...) 不受限）。注释不参与断言（css 注释中的
// 对比度实测注记等说明文字允许出现色值，只断言真实声明）。挂在 build 前置：
//   npm run assert:tokens   # 单独执行
//   npm run build           # 自动前置执行
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, dirname, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const srcDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')

// 递归收集 src 下全部 css；tokens.css（任意层级）= token 定义层，豁免
function collectCss(dir) {
  const found = []
  for (const f of readdirSync(dir).sort()) {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) found.push(...collectCss(p))
    else if (f.endsWith('.css') && f !== 'tokens.css') found.push(p)
  }
  return found
}
const files = collectCss(srcDir)

// 色值字面量形态：#rgb…#rrggbbaa / rgb( / rgba( / hsl( / hsla(
const LITERAL = /#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(/g

// 抹掉注释但保留换行结构——行号在失败信息里不漂移
function stripComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '))
}

const failures = []
for (const p of files) {
  const f = relative(srcDir, p)
  const lines = stripComments(readFileSync(p, 'utf8')).split('\n')
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
console.log(
  `assert-tokens: OK — ${files.length} 个 css（src 全量，注释感知）零硬编码色值声明（${files
    .map((p) => relative(srcDir, p))
    .join(', ')}）`,
)
