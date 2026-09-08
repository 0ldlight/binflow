import type { ComponentPropsWithoutRef, ReactNode } from 'react'

import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import { tr } from '../../i18n'

const t = tr('security')

// 双列穿梭（console-m8 §3.3 C5，对齐 reverse §4.11 Available/Selected 形态）：
// 左「可选」右「已选」，各带计数；空侧显示「未选择项」（No Items Selected
// 语义）。BinFlow 皮肤下条目本体是 checkbox——勾选即移入右侧、取消即移回，
// 键盘（Tab + Space）天然可达，且沿用 T-101 冻结的 checkbox 锚
// （user-form-group-<name> / group-form-member-<name>——T-267 曾随审计盲区
// 误退役，T-274 回填）。
//
// 本文件仅服务本票三页（Users/UserDetail/Groups）——area 目录内的新助手，
// 不触碰共享组件层（改共享层需另开票）。
//
// T-300 批次二：checkbox 迁 MUI（FormControlLabel + Checkbox，锚经
// slotProps.input 落 input 本体）；.transfer-item 的行布局类续挂
// （security.css 压过 MUI 默认），勾选/禁用/键盘链路零变化。

export interface TransferItem {
  /** 条目键（用户/组名——锚与提交体都用它） */
  name: string
  /** 展示名（键 ≠ 展示时用——T-514 通配桶：键 = wire 字面 ANY LOCAL、
   * 展示 = Any Local〔console-ui §3.8 预置行拼写〕；缺省展示 name） */
  label?: string
  /** 附加说明行（如组描述/用户 email）；可空 */
  note?: string
}

export function TransferBox({
  items,
  selected,
  onToggle,
  disabled = false,
  availableLabel = t('可选'),
  selectedLabel = t('已选'),
  itemTestid,
  renderNote,
}: {
  /** 全量候选（两列的并集） */
  items: readonly TransferItem[]
  /** 已选键集 */
  selected: readonly string[]
  onToggle: (name: string, next: boolean) => void
  disabled?: boolean
  availableLabel?: string
  selectedLabel?: string
  /** 条目 checkbox 的 testid（冻结锚：user-form-group-<name> 等；T-274 回填后调用方恢复传参） */
  itemTestid?: (name: string) => string
  /** 已选侧的附加说明（如组 → 成员数之外的用法）；默认同 available 侧 */
  renderNote?: (item: TransferItem) => ReactNode
}) {
  const available = items.filter((i) => !selected.includes(i.name))
  const chosen = items.filter((i) => selected.includes(i.name))
  const note = renderNote ?? ((i: TransferItem) => (i.note ? <span className="text-muted">{i.note}</span> : null))

  const row = (item: TransferItem, checked: boolean) => (
    <FormControlLabel
      key={item.name}
      className="transfer-item"
      disabled={disabled}
      control={
        <Checkbox
          size="small"
          checked={checked}
          onChange={(e) => onToggle(item.name, e.target.checked)}
          slotProps={{ input: { 'data-testid': itemTestid?.(item.name) } as ComponentPropsWithoutRef<'input'> }}
        />
      }
      label={
        <>
          <span className="mono" lang="en">
            {item.label ?? item.name}
          </span>
          {note(item)}
        </>
      }
    />
  )

  return (
    <div className="transfer">
      <div className="transfer-col" data-testid="transfer-available">
        <div className="transfer-head">
          <span>{availableLabel}</span>
          <span className="transfer-count">{available.length}</span>
        </div>
        {/* tabIndex：可滚动区键盘可达（axe scrollable-region-focusable） */}
        <div className="transfer-list" tabIndex={0} role="group" aria-label={t('{availableLabel}（{v1}）', { availableLabel: availableLabel, v1: available.length })}>
          {available.length === 0 ? <p className="transfer-empty">{t('（无可选项）')}</p> : available.map((i) => row(i, false))}
        </div>
      </div>
      <div className="transfer-col" data-testid="transfer-selected">
        <div className="transfer-head">
          <span>{selectedLabel}</span>
          <span className="transfer-count">{chosen.length}</span>
        </div>
        <div className="transfer-list" tabIndex={0} role="group" aria-label={t('{selectedLabel}（{v1}）', { selectedLabel: selectedLabel, v1: chosen.length })}>
          {chosen.length === 0 ? <p className="transfer-empty">{t('未选择项')}</p> : chosen.map((i) => row(i, true))}
        </div>
      </div>
    </div>
  )
}
