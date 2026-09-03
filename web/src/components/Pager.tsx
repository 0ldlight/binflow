import { useState } from 'react'
import type { ReactNode } from 'react'

import MuiPagination from '@mui/material/Pagination'
import PaginationItem from '@mui/material/PaginationItem'
import Select from '@mui/material/Select'
import TextField from '@mui/material/TextField'
import type { ComponentPropsWithoutRef } from 'react'

import './pager.css'

// T-451（FR-144.7 / LC-98，E2 翻案 ×9）共享分页控件：
//
// - **形态锚 = parity 册 §11.2（K67/T-437 冻结，Q4 出口①）**：页码数字
//   序列（当前页高亮）+ 首/上一页/下一页/末页按钮 + 每页行数选择器。
//   边界态：首页「首页/上一页」禁置、末页「下一页/末页」禁置；单页
//   全量时控件整体呈现、全链禁置——**禁置不隐藏**（P7 键盘可达）。
// - **语义 C 注（LC-98）**：呈现对齐、语义自有——后端维持 keyset 游标
//   /AQL offset，页码在前端映射为页窗（游标链推进 / .offset() 重写），
//   深翻页 offset 扫描成本规避。末页未知的流式面（audit keyset / AQL
//   total=本页行数）页数 = 前沿 + 1：页码逐步揭示，末页钮逐页推进。
// - **每页行数档位（票内冻结）**：[20, 50, 100, 200, 1000]——100 =
//   BinFlow 家族页窗（docker n 缺省 / audit 缺省 / 搜索切片同源），
//   1000 = AQL 引擎上限（audit 服务端 limit 上限同值）。参照实例
//   7.161.20 活体复核：管理列表分页 selector 槽在场未启用（ag-grid
//   pageSizeComp 空）、单页全量面无档位可证——档位为 BinFlow 自有
//   冻结，非 Artifactory 逐字对位（锚册 v1.39 留痕）。
// - 锚族（固定名，一面一控件同刻唯一）：pager-range / pager-size /
//   pager-size-<n> / pager-first / pager-prev / pager-next / pager-last /
//   pager-page-<n>。根锚由调用方承载（各面既有 footer 锚零改名）。

/** 每页行数档位（票内冻结——见文件头注记） */
export const PAGER_SIZE_OPTIONS = [20, 50, 100, 200, 1000] as const

/** 缺省页大小 = 家族页窗 100（docker n / audit / 搜索同源） */
export const PAGER_SIZE_DEFAULT = 100

export interface PagerProps {
  /** 当前页（1 基） */
  page: number
  /** 已知页数：已知总量 = ceil(total/size)；流式/keyset = 前沿
   *  （more ? page+1 : page——页码序列只呈现已确证存在的页） */
  pageCount: number
  onPageChange: (page: number) => void
  /** 窗口行序（1 基；from=0 表空窗——空态面通常不渲染本控件） */
  from: number
  to: number
  /** 行总数；null = 未知（keyset/流式——range 行如实呈现「末页未知」） */
  total: number | null
  /** range 行尾注（各面语境：快滤/过滤/m-holder 等既有文案族续挂） */
  note?: ReactNode
  /** 每页行数（在场且提供 onChange = 行数选择器呈现） */
  pageSize?: number
  onPageSizeChange?: (size: number) => void
  /** 档位覆盖（缺省 PAGER_SIZE_OPTIONS；当前值须在档内） */
  sizeOptions?: readonly number[]
  /** 流式/keyset 面 = true：末页钮 title 注记（语义 C 注呈现层） */
  lastUnknown?: boolean
  /** 请求在飞等场景的全控件禁置 */
  disabled?: boolean
}

export function Pager({
  page,
  pageCount,
  onPageChange,
  from,
  to,
  total,
  note,
  pageSize,
  onPageSizeChange,
  sizeOptions = PAGER_SIZE_OPTIONS,
  lastUnknown = false,
  disabled = false,
}: PagerProps) {
  return (
    <div className="pager">
      <span className="pager-range" data-testid="pager-range">
        {total === null ? `显示 ${from} – ${to}（末页未知）` : `显示 ${from} – ${to} / 共 ${total} 项`}
        {note}
      </span>
      {pageSize !== undefined && onPageSizeChange !== undefined && (
        <label className="pager-size">
          <span aria-hidden="true">每页</span>
          <TextField
            select
            size="small"
            value={pageSize}
            disabled={disabled}
            onChange={(e) => {
              const n = Number(e.target.value)
              if (Number.isFinite(n) && n > 0) onPageSizeChange(n)
            }}
            sx={{ width: 88 }}
            slotProps={{
              select: {
                native: true,
                inputProps: {
                  'data-testid': 'pager-size',
                  'aria-label': '每页行数',
                } as ComponentPropsWithoutRef<'select'>,
              } as ComponentPropsWithoutRef<typeof Select>,
            }}
          >
            {sizeOptions.map((n) => (
              <option key={n} value={n} data-testid={`pager-size-${n}`}>
                {n}
              </option>
            ))}
          </TextField>
          <span aria-hidden="true">行</span>
        </label>
      )}
      <MuiPagination
        count={Math.max(1, pageCount)}
        page={page}
        disabled={disabled}
        onChange={(_, p) => {
          if (p !== page) onPageChange(p)
        }}
        showFirstButton
        showLastButton
        size="small"
        siblingCount={1}
        boundaryCount={1}
        aria-label="分页"
        renderItem={(item) => {
          // 锚族赋值形态（对账器口径：data-testid/testid 赋值面的引号字面
          // 量——锚册 §10.5 T-451 批；省略项〔ellipsis〕不挂锚）
          let testid: string | undefined
          if (item.type === 'first') testid = 'pager-first'
          else if (item.type === 'previous') testid = 'pager-prev'
          else if (item.type === 'next') testid = 'pager-next'
          else if (item.type === 'last') testid = 'pager-last'
          else if (item.type === 'page') testid = `pager-page-${item.page}`
          return (
            <PaginationItem
              {...item}
              data-testid={testid}
              title={
                item.type === 'last' && lastUnknown
                  ? '流式末页未知——逐页推进（呈现对齐、语义自有 C 注）'
                  : undefined
              }
            />
          )
        }}
      />
    </div>
  )
}

// ---- 客户端页窗（全量已取回面的窗口化；keyset/AQL 面不用本钩子） -----------

/** epoch 口径（ResultsTable T-449 切片回零同款）：结果集标识变了即回落
 *  第 1 页——状态里只存「某 epoch 下用户翻到的页」，无 effect 回零。
 *  initialSize 仅在挂载时生效（后续为用户选择态）。 */
export function useClientPager(total: number, epoch: unknown, initialSize = PAGER_SIZE_DEFAULT) {
  const [state, setState] = useState<{ epoch: unknown; page: number; size: number }>(() => ({
    epoch: undefined,
    page: 1,
    size: initialSize,
  }))
  // epoch 未匹配 = 新结果集：派生回落第 1 页（页大小偏好保留）
  const live = state.epoch === epoch ? state : { epoch, page: 1, size: state.size }
  const pageCount = Math.max(1, Math.ceil(total / live.size))
  // 结果缩水（删除行等）时夹取到在库的最后一页
  const page = Math.min(live.page, pageCount)
  const from = total === 0 ? 0 : (page - 1) * live.size + 1
  const to = Math.min(page * live.size, total)
  const setPage = (p: number) => setState({ epoch, page: p, size: live.size })
  const setSize = (n: number) => setState({ epoch, page: 1, size: n })
  /** 窗口切片（稳定引用输入；调用方 useMemo 的行集） */
  const slice = <T,>(rows: readonly T[]): T[] => rows.slice((page - 1) * live.size, page * live.size)
  return { page, size: live.size, pageCount, from, to, setPage, setSize, slice }
}
