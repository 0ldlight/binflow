#!/usr/bin/env node
// Penpot Dashboard UI Kit token gate：锁定 2026-09-18 Apple Design 复审通过的关键值。
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
const expected = [
  [color, '--bf-bg: #f5f5f6', 'light canvas'],
  [color, '--bf-sidebar: #ffffff', 'light sidebar'],
  [color, '--bf-border: #0000001a', 'light border'],
  [color, '--bf-accent: #0064cc', 'accessible light interactive blue'],
  [color, "[data-theme='dark']", 'dark theme'],
  [color, '--bf-bg: #333333', 'dark canvas'],
  [color, '--bf-surface-1: #3b3b3b', 'dark surface'],
  [color, '--bf-border: #ffffff26', 'dark border'],
  [color, '--bf-accent: #6cb2ff', 'accessible dark interactive blue'],
  [color, '--bf-danger: #ff8a84', 'accessible dark danger text'],
  [color, '--bf-chart-blue: #007aff', 'source chart blue'],
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
]
const failures = expected.filter(([source, needle]) => !source.includes(needle)).map(([, , name]) => name)
if (failures.length) {
  console.error(`assert-penpot: FAIL\n${failures.join('\n')}`)
  process.exit(1)
}
console.log('assert-penpot: OK — Dashboard UI Kit token contract is intact')
