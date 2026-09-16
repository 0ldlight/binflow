// cn() 单测（L024-9）：twMerge 字号类组注册后的消解语义。
// 运行：node --test src/lib/utils/cn.test.ts（Node >=22.6 类型剥离）。
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { cn } from './cn.ts'

// 修票病灶：自定义字号类（text-aux）被默认组表误判进 text-color 组，
// 与语义色类同串时被当冲突消解（badge 基线字号丢失——批 5 曾以任意值
// token 自持规避）。注册 font-size 组后两族互不消解。
test('font-size semantic class survives a later color class in the same string', () => {
  assert.equal(cn('text-aux', 'text-success'), 'text-aux text-success')
})

test('all five custom size names coexist with colors', () => {
  for (const size of ['text-dense', 'text-aux', 'text-form', 'text-h3', 'text-h2']) {
    assert.equal(cn(size, 'text-destructive'), `${size} text-destructive`)
    assert.equal(cn('text-muted-foreground', size), `text-muted-foreground ${size}`)
  }
})

// 同组内仍是后者胜（消解语义不因注册而丢失）。
test('custom size vs custom size: last wins', () => {
  assert.equal(cn('text-aux', 'text-h3'), 'text-h3')
})

test('color vs color: last wins (default groups untouched)', () => {
  assert.equal(cn('text-success', 'text-destructive'), 'text-destructive')
})

test('custom size vs built-in size: last wins', () => {
  assert.equal(cn('text-aux', 'text-xs'), 'text-xs')
})

// badge 配方回归：基线 text-aux + 变体色类 + mono/dot 追加类同串，
// 字号/色彩/字族三组各归各位（旧 twMerge 输出缺 text-aux）。
test('badge recipe: base size + variant color + append classes', () => {
  const out = cn('text-aux font-medium', 'text-badge-info font-normal', 'font-mono')
  assert.equal(out, 'text-aux text-badge-info font-normal font-mono')
})

// tint 规避法退役的等值面：语义类与旧的字长任意值写法同为 font-size
// 组，同串时后者胜（迁移后消费点不再出现该写法）。
test('semantic size replaces the retired arbitrary-value form', () => {
  assert.equal(cn('text-[length:var(--bf-fs-xs)]', 'text-aux'), 'text-aux')
})
