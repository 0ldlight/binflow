// 回收站域 API（M12 T-352，FR-106 FE 腿；契约源 internal/httpapi/trash.go +
// internal/repo/trash.go，即 docs/user/admin/trash-can.md）：
//
// - POST   /api/trash/empty              → TrashSummary（清空整个 can）
// - POST   /api/trash/restore/{path}?to= → CopyOrMoveResult（恢复 = 一次系统
//   身份的 move；`to` 为可选 /{repo}[/{path}] 目的地覆盖）
// - DELETE /api/trash/clean/{path}       → TrashSummary（单条/子树永久清除）
//
// 浏览不在此列——骑既有存储面（GET /api/storage/auto-trashcan 与
// ?properties 五元组断言面；lib/api.ts 的 getNodeProperties + artifacts/lib
// 的 listChildren），与控制台树同源。三动词的门 = system:write（仅全量
// admin；readonly_admin 403）+ trashcan 槽 license 门（Q3 暂行 pro；
// community 回 403 + X-Binflow-License-Required）。
//
// 契约注记：`transaction-size` 恢复参数被服务端解析并校验但语义惰性
// （逐项管线——注册差异），FE 不暴露该旋钮；TrashSummary 的 removed =
// files + folders。

import { apiJSON } from './api'
import type { AddonRow } from './addons'
import { getAddons } from './addons'

/** 内置回收站仓库 key（storage-layout §5；浏览面的 repo 段） */
export const TRASH_REPO = 'auto-trashcan'

/** trashcan 功能槽的 addons 注册表 id（slots.go Trashcan()） */
export const TRASH_SLOT_ID = 'trashcan'

/** 属性五元组键（T-345 AC1 断言面；trash.time 是 epoch 毫秒的十进制文本） */
export const TRASH_PROP_KEYS = [
  'trash.time',
  'trash.deletedBy',
  'trash.originalRepository',
  'trash.originalRepositoryType',
  'trash.originalPath',
] as const

/** empty/clean 的清剿报告（REST 体 = 审计 detail 同形） */
export interface TrashSummary {
  removed: number
  files: number
  folders: number
  bytes: number
}

/** restore 回执（copy/move 族 CopyOrMoveResult 的 messages 面） */
export interface TrashRestoreResult {
  messages: { level: string; message: string }[]
}

/** 回收站相对路径的 URL 编码（段级 encode；restore/clean 的 {path} 段） */
function trashPath(path: string): string {
  return path
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
}

/** 清空整个 can（内置仓行本身保留） */
export function emptyTrash(): Promise<TrashSummary> {
  return apiJSON<TrashSummary>('/trash/empty', { method: 'POST' })
}

/**
 * 恢复一条目。to 为可选目的地覆盖（/{repo}[/{path}]，段级编码进 query）；
 * 留空 = 按原位恢复（目的地解析：to 覆盖 > 属性五元组 > 路径结构首段）。
 */
export function restoreTrash(path: string, to = ''): Promise<TrashRestoreResult> {
  const q = to.trim() !== '' ? `?to=${encodeURIComponent(to.trim())}` : ''
  return apiJSON<TrashRestoreResult>(`/trash/restore/${trashPath(path)}${q}`, { method: 'POST' })
}

/** 单条（或目录子树）永久清除 */
export function cleanTrash(path: string): Promise<TrashSummary> {
  return apiJSON<TrashSummary>(`/trash/clean/${trashPath(path)}`, { method: 'DELETE' })
}

/** trashcan 槽的注册表行（无注册表的栈 = null——按未门控呈现，服务端终裁） */
export async function trashSlot(): Promise<AddonRow | null> {
  const rows = await getAddons()
  return rows.find((r) => r.id === TRASH_SLOT_ID) ?? null
}

/** trash.time（epoch 毫秒文本）→ 本地时间呈现；不可解析时原样返回 */
export function formatTrashTime(epochMs: string): string {
  const n = Number(epochMs)
  if (epochMs.trim() === '' || !Number.isFinite(n) || n <= 0) return epochMs || '—'
  const t = new Date(n)
  if (Number.isNaN(t.getTime())) return epochMs
  const pad = (v: number) => String(v).padStart(2, '0')
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`
}
