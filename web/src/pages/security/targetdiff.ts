// permission target 变更摘要（console-ux §4.9[4]：保存前 diff 确认——
// 权限是安全面，逐条列出将发生的授予/撤销再提交）。

import type { PermAction } from './api'

export interface TargetSnapshot {
  repos: string[]
  includes: string[]
  excludes: string[]
  users: Record<string, PermAction[]>
  groups: Record<string, PermAction[]>
}

export interface DiffLine {
  sign: '+' | '-'
  /** 中文说明（非 mono 部分） */
  text: string
  /** 标识符/动作（mono 部分，P2 可复制语义） */
  value: string
}

const ACTION_ORDER: Record<PermAction, number> = { read: 0, write: 1, delete: 2, manage: 3 }
const sortActions = (a: PermAction[]) => [...a].sort((x, y) => ACTION_ORDER[x] - ACTION_ORDER[y])
const sameActions = (a: PermAction[], b: PermAction[]) =>
  sortActions(a).join(',') === sortActions(b).join(',')

function diffList(prev: string[], next: string[], addText: string, delText: string, out: DiffLine[]) {
  for (const v of next) if (!prev.includes(v)) out.push({ sign: '+', text: addText, value: v })
  for (const v of prev) if (!next.includes(v)) out.push({ sign: '-', text: delText, value: v })
}

function diffPrincipals(
  prev: Record<string, PermAction[]>,
  next: Record<string, PermAction[]>,
  kind: string,
  out: DiffLine[],
) {
  const names = Array.from(new Set([...Object.keys(prev), ...Object.keys(next)])).sort()
  for (const name of names) {
    const before = sortActions(prev[name] ?? [])
    const after = sortActions(next[name] ?? [])
    for (const a of after) if (!before.includes(a)) out.push({ sign: '+', text: `授予${kind}`, value: `${name} ${a}` })
    for (const a of before) if (!after.includes(a)) out.push({ sign: '-', text: `撤销${kind}`, value: `${name} ${a}` })
  }
}

/** 原 → 新 的逐条变更（顺序：仓库 → include → exclude → 用户 → 组） */
export function buildTargetDiff(prev: TargetSnapshot, next: TargetSnapshot): DiffLine[] {
  const out: DiffLine[] = []
  diffList(prev.repos, next.repos, '添加仓库', '移除仓库', out)
  diffList(prev.includes, next.includes, '添加 include', '移除 include', out)
  diffList(prev.excludes, next.excludes, '添加 exclude', '移除 exclude', out)
  diffPrincipals(prev.users, next.users, '用户', out)
  diffPrincipals(prev.groups, next.groups, '组', out)
  return out
}

/** 集合等价（与 diff 同一语义：元素互含即等价，顺序/重复不敏感——review NB②） */
function sameSet(prev: string[], next: string[]): boolean {
  return prev.every((v) => next.includes(v)) && next.every((v) => prev.includes(v))
}

/**
 * 快照等价（diff 为空 ⇔ 保存按钮禁用 ⇔ 集合等价）。列表字段与
 * buildTargetDiff 的 `!includes` 判定保持同一集合语义——否则「删掉 chip
 * 再加回」的重排会让 diff 空而按钮可点，确认框误显新建文案。
 */
export function sameSnapshot(a: TargetSnapshot, b: TargetSnapshot): boolean {
  if (!sameSet(a.repos, b.repos)) return false
  if (!sameSet(a.includes, b.includes)) return false
  if (!sameSet(a.excludes, b.excludes)) return false
  const keys = (m: Record<string, PermAction[]>) => Object.keys(m).sort()
  if (keys(a.users).join(',') !== keys(b.users).join(',')) return false
  if (keys(a.groups).join(',') !== keys(b.groups).join(',')) return false
  for (const k of keys(a.users)) if (!sameActions(a.users[k], b.users[k])) return false
  for (const k of keys(a.groups)) if (!sameActions(a.groups[k], b.groups[k])) return false
  return true
}
