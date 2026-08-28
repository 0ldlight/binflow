#!/usr/bin/env node
// 编译期断言：组件层零硬编码色值（ADR-0029 自有皮肤纪律）。
//   T-234 立腿：src/ 下全部 css（styles 共享层 + components/ + pages/**）
//     ——tokens.css 仍为唯一色值字面量豁免层（token 定义层）。
//   T-264 (FR-82-AC4) 扩面：注释感知（css 注释中的对比度实测注记不参与）。
//   T-344 批 A 扩 TSX 双腿（mui-native-visual §5.2）：
//     腿 2 色值字面量：web/src/**/*.tsx 的 sx/style 内 hex/rgb/hsl 字面量
//       仅允许出现在 src/app/MuiProvider.tsx（主题定义层，与 tokens.css
//       同级豁免——R5 双源同值的字面量复刻位）。
//     腿 3 主题优先：TSX 内 var(--bf-(bg|surface|text|accent|danger|
//       success|warning|info|scrim)[\w-]*) 命中即 FAIL——色板一律
//       theme.palette / 组件默认；--bf-sp-*/fs-*/r-*/mono/z 布局 token 与
//       --bf-sidebar 系（侧栏身份例外，§4.1）不受限；保留清单 css 文件
//       不在本腿扫描面（只扫 tsx）。
// 其余 css 的声明中出现色值字面量即失败（transparent 关键字与
// color-mix(... var(--bf-*) ...) 不受限）。挂在 build 前置：
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
const cssFiles = collectCss(srcDir)

// TSX 扫描面（腿 2/3）：MuiProvider = 主题定义层，唯一豁免
function collectTsx(dir) {
  const found = []
  for (const f of readdirSync(dir).sort()) {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) found.push(...collectTsx(p))
    else if (f.endsWith('.tsx') && !p.endsWith(join('app', 'MuiProvider.tsx'))) found.push(p)
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
      failures.push(`${f}:${i + 1}: TSX 色值字面量 "${m[0]}"（仅 app/MuiProvider.tsx 主题层豁免；色板走 theme.palette）`)
    }
    for (const m of line.matchAll(BF_COLOR)) {
      failures.push(`${f}:${i + 1}: TSX 引用 --bf-* 色彩 token "${m[0]}"（色板一律 theme.palette / 组件默认；sidebar 系与布局 token 例外）`)
    }
  })
}

if (failures.length > 0) {
  console.error(`assert-tokens: FAIL — ${failures.length} 处违规：`)
  for (const f of failures) console.error(`  ${f}`)
  process.exit(1)
}
console.log(
  `assert-tokens: OK — css ${cssFiles.length} 个（零硬编码色值）+ tsx ${tsxFiles.length} 个（MuiProvider 外零色值字面量、零 --bf-* 色彩 token 引用）`,
)
