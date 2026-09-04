import { useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import Chip from '@mui/material/Chip'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TableSortLabel from '@mui/material/TableSortLabel'

import { useConfirm } from '../../components/ConfirmDialog'
import { useToast } from '../../app/ToastContext'
import { ApiError, errText } from '../../lib/api'
import { deleteUser } from './api'
import { PERM_ACTIONS } from './api'
import type { PermAction, PrincipalGrantRow } from './api'

// 用户/组页共用小件（T-237，area 目录内助手——与 TransferBox 同批）：
//   SortTh / useTableSort  C1 列头排序（§4.7：点击循环 asc → desc → none）
//   PermSummaryTable       只读权限矩阵（§6.9[5]/§6.10：T-455 起 r/a/w/d/m
//                          五列，manage 头带「不隐含读写删」说明——§7.2）
//   useUserDelete          删用户强确认（T-257，E4 消费面——列表行与编辑页
//                          危险区共用；RepoDeleteConfirm 同款 holder 闭包缝）

export type SortDir = 'asc' | 'desc'

export interface SortState<K extends string> {
  key: K | null
  dir: SortDir
}

/** 列头排序循环：none → asc → desc → none（多列不建，§4.7） */
export function useTableSort<K extends string>(initial: SortState<K> = { key: null, dir: 'asc' }) {
  const [sort, setSort] = useState<SortState<K>>(initial)
  const toggle = (key: K) => {
    setSort((p) => {
      if (p.key !== key) return { key, dir: 'asc' }
      if (p.dir === 'asc') return { key, dir: 'desc' }
      return { key: null, dir: 'asc' }
    })
  }
  return { sort, toggle }
}

/** 排序表头：TableSortLabel 承载点击与键盘（T-344 批 C 换装），aria-sort
 *  仍由 th 同步（§8 语义标签）。锚与三态判定在 th 本体不动；label 的
 *  onClick stopPropagation——鼠标点 label / 点 th 空白区 / 键盘 Enter
 *  三条路径都恰触发一次 onToggle（ButtonBase 键盘模拟直调 onClick，
 *  不派生冒泡 click，热区双挂不会双发）。 */
export function SortTh<K extends string>({
  label,
  sortKey,
  sort,
  onToggle,
  testid,
}: {
  label: string
  sortKey: K
  sort: SortState<K>
  onToggle: (key: K) => void
  testid?: string
}) {
  const active = sort.key === sortKey
  const ariaSort = active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'
  return (
    <TableCell
      component="th"
      scope="col"
      aria-sort={ariaSort}
      data-testid={testid}
      onClick={() => onToggle(sortKey)}
    >
      <TableSortLabel
        active={active}
        direction={active ? sort.dir : 'asc'}
        onClick={(e) => {
          e.stopPropagation()
          onToggle(sortKey)
        }}
      >
        {label}
      </TableSortLabel>
    </TableCell>
  )
}

/** 排序应用（稳定副本；取值器返回字符串/数字，空值沉底） */
export function applySort<T>(rows: readonly T[], sort: SortState<string>, valueOf: (r: T) => string | number | null): T[] {
  if (!sort.key) return [...rows]
  const dir = sort.dir === 'asc' ? 1 : -1
  return [...rows].sort((a, b) => {
    const va = valueOf(a) ?? ''
    const vb = valueOf(b) ?? ''
    if (va === vb) return 0
    return (va < vb ? -1 : 1) * dir
  })
}

/** 只读权限矩阵（§6.9[5]）：Permission Name │ 应用途径 │ r/a/w/d/m
 *  （T-455 五列——write 列含 deploy-cache 别名归一，见 api.ts）。行 =
 *  target；来源 = 直接 + 经组（用户视角）或直接（组视角）。 */
export function PermSummaryTable({
  rows,
  rowTestidPrefix,
  emptyHint,
}: {
  rows: readonly PrincipalGrantRow[]
  rowTestidPrefix: string
  emptyHint: string
}) {
  if (rows.length === 0) {
    return <p className="text-muted perm-summary-empty">{emptyHint}</p>
  }
  return (
    <Table className="perm-summary" data-testid={`${rowTestidPrefix}-matrix`}>
      <TableHead>
        <TableRow>
          <TableCell component="th" scope="col">Permission Name</TableCell>
          <TableCell component="th" scope="col">应用途径</TableCell>
          {PERM_ACTIONS.map((a) => (
            <TableCell
              key={a}
              component="th"
              scope="col"
              className="th-action"
              title={
                a === 'manage'
                  ? 'manage = 仓库配置派生权（不隐含读写删）'
                  : a === 'annotate'
                    ? 'annotate = 属性写位（7.161 标签 Annotate；不隐含内容写）'
                    : a === 'write'
                      ? 'write = 部署位（7.161 标签 Deploy/Cache；wire 正名 deploy-cache；不携带 annotate）'
                      : a === 'delete'
                        ? 'delete = 删除/覆盖（7.161 标签 Delete/Overwrite）'
                        : undefined
              }
            >
              {a}
            </TableCell>
          ))}
        </TableRow>
      </TableHead>
      <TableBody>
        {rows.map((r) => (
          <TableRow key={r.target} hover>
            <TableCell>
              <Link className="row-link mono" to={`/admin/security/permissions/${encodeURIComponent(r.target)}`} lang="en">
                {r.target}
              </Link>
            </TableCell>
            <TableCell>
              <span className="sec-chips">
                {r.sources.map((s) =>
                  s === 'direct' ? (
                    <Chip key="direct" size="small" className="badge neutral" label="直接" />
                  ) : (
                    <Chip
                      key={s}
                      size="small"
                      className="badge neutral mono"
                      label={s}
                      sx={{ fontFamily: 'var(--bf-mono)' }}
                      lang="en"
                    />
                  ),
                )}
              </span>
            </TableCell>
            {PERM_ACTIONS.map((a) => (
              <TableCell key={a} className="td-mark">
                {r.actions.includes(a as PermAction) ? (
                  <span className="mark-on" aria-label={`${a} 已授予`}>
                    ✓
                  </span>
                ) : (
                  <span className="mark-off" aria-label={`${a} 未授予`}>
                    —
                  </span>
                )}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

// ---- Status 徽章（T-257；E2/E3 enabled 真值消费——列表列与详情 facts 行共用） ----

/** 启用/禁用徽章——自持 status-pill 类（不挂 .badge 基类：用户行内
 *  .badge 锚位已被角色徽章/组 chips 占用，加挂会破 m8 套件行级 .badge
 *  锚的视图唯一性，ADR-0029 决策 3）。name 可空（详情页无行锚需求）。
 *  T-344 批 C：Chip 原生皮肤（outlined success/error，类名续挂 inert）。 */
export function StatusLabel({ enabled, name }: { enabled: boolean; name?: string }) {
  const testid = name ? { 'data-testid': `user-status-${name}` } : {}
  return enabled ? (
    <Chip size="small" variant="outlined" color="success" className="status-pill status-on" label="启用" {...testid} />
  ) : (
    <Chip size="small" variant="outlined" color="error" className="status-pill status-off" label="禁用" {...testid} />
  )
}

// ---- 删用户强确认（T-257；E4 DELETE /api/security/users/{name} 消费面） ----
//
// 形态：ConfirmDialog 基座 + **输入用户名强确认**（M9 review 裁定的升级
// 形态——console-m8 §4.6 用户行的「双按钮」基线由本票升格，理由：级联
// 不可恢复〔组员/授权/token/会话同事务删除〕+ 重复删除是确定性 404 非
// 幂等，误触无补救路径）+ 文案体现「不可恢复」。
//
// 服务端护栏如实呈现（UI 只预禁用可达的两态：自删/内置 admin——其余
// 〔最后一个 admin 400、404 已删〕是并发/环境事实，交服务端终裁原样上浮）：
// 404 `User not found`（已被他人删）按事实刷新列表，不伪造成成功。

export interface UserDeleteHooks {
  /** 删除成功后的收尾（列表刷新 / 详情页返回列表）——在调用方闭包里 */
  onDeleted?: (name: string) => void
}

export function useUserDelete({ onDeleted }: UserDeleteHooks = {}) {
  const toast = useToast()
  const confirm = useConfirm()
  const [deleting, setDeleting] = useState(false)

  const openDialog = async (name: string): Promise<void> => {
    const holder = { typed: '' }
    const body: ReactNode = (
      <>
        <p>
          确定要移除用户 <b className="mono" lang="en">{name}</b> 吗？此操作<b>不可恢复</b>。
        </p>
        <p className="text-muted" style={{ fontSize: 12 }}>
          同一事务内级联：组员关系、permission target 中的直接授权、全部 API token 与活跃会话
          （持有其 token 的请求随即 401）。审计历史保留。重复删除会被服务端拒绝（404，有意非幂等）。
        </p>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0 }}>
          <label htmlFor={`del-user-confirm-${name}`}>
            输入用户名 <b className="mono" lang="en">{name}</b> 以确认：
          </label>
          <input
            id={`del-user-confirm-${name}`}
            className="confirm-input"
            autoComplete="off"
            onChange={(e) => {
              holder.typed = e.target.value
            }}
            data-testid="user-delete-confirm-name"
            lang="en"
          />
        </div>
      </>
    )
    const ok = await confirm({
      title: '删除用户',
      body,
      danger: true,
      confirmLabel: '删除用户',
      confirmDisabled: () => holder.typed !== name,
    })
    if (!ok) return
    setDeleting(true)
    try {
      const text = await deleteUser(name)
      toast.success(text) // 服务端纯文本（"The user: 'x' has been removed successfully."）
      onDeleted?.(name)
    } catch (err) {
      // 如实呈现：400 护栏（内置/last-admin/自删）与 404（已被他人删）的
      // 服务端原文即最终事实——不重试不美化
      toast.error(`删除失败：${errText(err)}`)
      if (err instanceof ApiError && err.status === 404) onDeleted?.(name)
    } finally {
      setDeleting(false)
    }
  }

  return { openDialog, deleting }
}
