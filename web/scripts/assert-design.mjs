#!/usr/bin/env node
// Design token gate：Penpot structure + BinFlow black-and-white monochrome.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src/design-system')
const read = (rel) => fs.readFileSync(path.join(root, rel), 'utf8')
const color = read('tokens/color.css')
const radius = read('tokens/radius.css')
const spacing = read('tokens/spacing.css')
const shadow = read('tokens/shadow.css')
const bridge = read('tailwind.css')
const pkgIcon = fs.readFileSync(path.join(root, '../components/pkg-icon.css'), 'utf8')
const expected = [
  [color, '--bf-bg: #f5f5f5', 'light canvas'],
  [color, '--bf-sidebar: #ffffff', 'light sidebar'],
  [color, '--bf-accent: #171717', 'black light interactive accent'],
  [color, "[data-theme='dark']", 'dark theme'],
  [color, '--bf-bg: #0a0a0a', 'dark canvas'],
  [color, '--bf-surface-1: #171717', 'dark surface'],
  [color, '--bf-accent: #ffffff', 'white dark interactive accent'],
  [radius, '--bf-r-sm: 8px', 'control radius'],
  [radius, '--bf-r-lg: 12px', 'card radius'],
  [radius, '--bf-r-xl: 16px', 'large-card radius'],
  [radius, '--bf-r-2xl: 20px', 'dashboard radius'],
  [spacing, '--bf-sp-dashboard: 28px', 'dashboard rhythm'],
  [shadow, '--bf-shadow-1: 0 0.5px 0.5px rgb(0 0 0 / 10%)', 'flat shadow'],
  [shadow, '--bf-shadow-2: 0 2px 4px rgb(0 0 0 / 10%)', 'overlay shadow'],
  [bridge, '--color-chart-blue: var(--bf-chart-blue)', 'chart bridge'],
  [bridge, '--radius-xl: var(--bf-r-xl)', 'radius bridge'],
  [bridge, '--spacing-dashboard: var(--bf-sp-dashboard)', 'spacing bridge'],
  [pkgIcon, "filter: grayscale(1)", 'grayscale package icons'],
]
const failures = expected.filter(([source, needle]) => !source.includes(needle)).map(([, name]) => name)
const hexes = [...color.matchAll(/#[0-9a-f]{6}/gi)].map((m) => m[0])
const colored = hexes.filter((hex) => {
  const r = parseInt(hex.slice(1, 3), 16), g = parseInt(hex.slice(3, 5), 16), b = parseInt(hex.slice(5, 7), 16)
  return Math.max(r, g, b) - Math.min(r, g, b) > 2
})
if (colored.length) failures.push(`non-grayscale token literal: ${colored.join(', ')}`)
if (failures.length) {
  console.error(`assert-design: FAIL\n${failures.join('\n')}`)
  process.exit(1)
}
console.log('assert-design: OK — Penpot structure + black-and-white token contract is intact')
