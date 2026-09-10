#!/usr/bin/env node
// 编译期断言：组件层零硬编码色值（ADR-0029 自有皮肤纪律）。
//   T-234 立腿：src/ 下全部 css（styles 共享层 + components/ + pages/**）
//     ——tokens.css 仍为唯一色值字面量豁免层（token 定义层）。
//   T-264 (FR-82-AC4) 扩面：注释感知（css 注释中的对比度实测注记不参与）。
//   T-344 批 A 扩 TSX 双腿（mui-native-visual §5.2）：
//     腿 2 色值字面量：web/src/**/*.tsx 内 hex/rgb/hsl 字面量零容忍
//       （FE-P4 MUI 清场：MuiProvider 豁免层随 MuiProvider 删除而移除——
//       色板唯一来源 = tokens.css + Tailwind 语义类，闸语义只升不降）。
//     腿 3 主题优先：TSX 内 var(--bf-(bg|surface|text|accent|danger|
//       success|warning|info|scrim)[\w-]*) 命中即 FAIL——色板一律
//       Tailwind 语义类；--bf-sp-*/fs-*/r-*/mono/z 布局 token 与
//       --bf-sidebar 系（侧栏身份例外，§4.1）不受限；保留清单 css 文件
//       不在本腿扫描面（只扫 tsx）。
//   前端重写 P1（frontend-rewrite-architecture §3 等价纪律新栈版）：
//     src/styles/tw/（新栈 token 定义层 + Tailwind @theme 桥接层）整层
//     加入豁免清单——与 tokens.css 同级的 token 定义层位；层外新栈
//     tsx 仍受腿 2/3 约束（色板走 Tailwind 语义类，不写 var(--bf-色系)，
//     不写字面量）。闸语义不降：豁免仅限该目录，组件/页面 css 零放宽；
//     FE-P4 起 TSX 面零文件级豁免（MuiProvider 退役）。
// 其余 css 的声明中出现色值字面量即失败（transparent 关键字与
// color-mix(... var(--bf-*) ...) 不受限）。挂在 build 前置：
//   npm run assert:tokens   # 单独执行
//   npm run build           # 自动前置执行
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, dirname, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const srcDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')

// 新栈 token 桥接层（styles/tw/）：token 定义层豁免位（frontend-rewrite
// P1）——当前层内零色值字面量（值全在 tw/tokens.css，桥接只引用 var()）
let newTokenLayerCount = 0

// 递归收集 src 下全部 css；tokens.css（任意层级）与 styles/tw/ 层 = token
// 定义层，豁免
function collectCss(dir) {
  const found = []
  for (const f of readdirSync(dir).sort()) {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) found.push(...collectCss(p))
    else if (f.endsWith('.css')) {
      if (f === 'tokens.css') continue
      if (relative(srcDir, p).replaceAll('\\', '/').startsWith('styles/tw/')) {
        newTokenLayerCount++
        continue
      }
      found.push(p)
    }
  }
  return found
}
const cssFiles = collectCss(srcDir)

// TSX 扫描面（腿 2/3）：FE-P4 起零文件级豁免（MuiProvider 主题层已随
// MUI 清场退役——色值字面量与 --bf-* 色彩 token 在 TSX 面全量禁止）
function collectTsx(dir) {
  const found = []
  for (const f of readdirSync(dir).sort()) {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) found.push(...collectTsx(p))
    else if (f.endsWith('.tsx')) found.push(p)
  }
  return found
}
const tsxFiles = collectTsx(srcDir)

// 色值字面量形态：#rgb…#rrggbbaa / rgb( / rgba( / hsl( / hsla(
const LITERAL = /#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(/g

// --bf-* 色彩 token（腿 3）：族前缀 + 衍生后缀（text-2 / surface-1..3 /
// accent-fg 等）；sidebar 系与 border/shadow/布局 token 不受限
const BF_COLOR = /var\(\s*--bf-(?:bg|surface|text|accent|danger|success|warning|info|scrim)[\w-]*\s*\)/g

// 抹掉注释但保留换行结构——行号在失败信息里不漂移
function stripComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '))
}

// TSX 注释抹除（/* */ 与 //；// 前不得是 ':' ——放过 https:// 等 protocol）
function stripJsComments(text) {
  const noBlock = text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '))
  return noBlock.replace(/([^:])\/\/[^\n]*/g, (m, p1) => p1 + ' '.repeat(m.length - p1.length))
}

const failures = []
for (const p of cssFiles) {
  const f = relative(srcDir, p)
  const lines = stripComments(readFileSync(p, 'utf8')).split('\n')
  lines.forEach((line, i) => {
    for (const m of line.matchAll(LITERAL)) {
      failures.push(`${f}:${i + 1}: 硬编码色值 "${m[0]}"（组件层只允许 var(--bf-*) / transparent）`)
    }
  })
}

for (const p of tsxFiles) {
  const f = relative(srcDir, p)
  const lines = stripJsComments(readFileSync(p, 'utf8')).split('\n')
  lines.forEach((line, i) => {
    for (const m of line.matchAll(LITERAL)) {
      failures.push(`${f}:${i + 1}: TSX 色值字面量 "${m[0]}"（零豁免——色板走 tokens.css/Tailwind 语义类）`)
    }
    for (const m of line.matchAll(BF_COLOR)) {
      failures.push(`${f}:${i + 1}: TSX 引用 --bf-* 色彩 token "${m[0]}"（色板一律 Tailwind 语义类；sidebar 系与布局 token 例外）`)
    }
  })
}

if (failures.length > 0) {
  console.error(`assert-tokens: FAIL — ${failures.length} 处违规：`)
  for (const f of failures) console.error(`  ${f}`)
  process.exit(1)
}
console.log(
  `assert-tokens: OK — css ${cssFiles.length} 个（零硬编码色值）+ tsx ${tsxFiles.length} 个（零色值字面量、零 --bf-* 色彩 token 引用——FE-P4 起零文件级豁免）+ 新栈 token 桥接层 ${newTokenLayerCount} 个豁免（styles/tw/）`,
)
