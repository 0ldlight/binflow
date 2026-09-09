// 新栈双列穿梭（console-m8 §3.3 C5——旧 pages/security/TransferBox.tsx 的
// MUI 实现退役，本件等价承接；.transfer-* 视觉类沿用 security.css）。
// 条目本体 = 原生 checkbox——勾选即移入右侧、取消即移回，键盘（Tab +
// Space）天然可达；冻结锚（user-form-group-<name> / group-form-member-<name>
// / perm-repo-pick-<key>）由 itemTestid 透传落 input 本体。
import type { ReactNode } from 'react'

import { tr } from '@/i18n'

const t = tr('security')

export interface TransferItem {
  /** 条目键（用户/组名/通配桶 wire 字面——锚与提交体都用它） */
  name: string
  /** 展示名（键 ≠ 展示时用——通配桶：键 = ANY LOCAL、展示 = Any Local） */
  label?: string
  /** 附加说明行（如组描述/用户 email/桶覆盖语义注）；可空 */
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
  /** 条目 checkbox 的 testid（冻结锚） */
  itemTestid?: (name: string) => string
  /** 已选侧的附加说明；默认同 available 侧 */
  renderNote?: (item: TransferItem) => ReactNode
}) {
  const available = items.filter((i) => !selected.includes(i.name))
  const chosen = items.filter((i) => selected.includes(i.name))
  const note = renderNote ?? ((i: TransferItem) => (i.note ? <span className="text-muted-foreground">{i.note}</span> : null))

  const row = (item: TransferItem, checked: boolean) => (
    <label key={item.name} className="transfer-item">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onToggle(item.name, e.target.checked)}
        {...(itemTestid?.(item.name) ? { 'data-testid': itemTestid(item.name) } : {})}
      />
      <span className="mono" lang="en">
        {item.label ?? item.name}
      </span>
      {note(item)}
    </label>
  )

  return (
    <div className="transfer">
      <div className="transfer-col" data-testid="transfer-available">
        <div className="transfer-head">
          <span>{availableLabel}</span>
          <span className="transfer-count">{available.length}</span>
        </div>
        {/* tabIndex：可滚动区键盘可达（axe scrollable-region-focusable） */}
        <div
          className="transfer-list"
          tabIndex={0}
          role="group"
          aria-label={t('{availableLabel}（{v1}）', { availableLabel: availableLabel, v1: available.length })}
        >
          {available.length === 0 ? <p className="transfer-empty">{t('（无可选项）')}</p> : available.map((i) => row(i, false))}
        </div>
      </div>
      <div className="transfer-col" data-testid="transfer-selected">
        <div className="transfer-head">
          <span>{selectedLabel}</span>
          <span className="transfer-count">{chosen.length}</span>
        </div>
        <div
          className="transfer-list"
          tabIndex={0}
          role="group"
          aria-label={t('{selectedLabel}（{v1}）', { selectedLabel: selectedLabel, v1: chosen.length })}
        >
          {chosen.length === 0 ? <p className="transfer-empty">{t('未选择项')}</p> : chosen.map((i) => row(i, true))}
        </div>
      </div>
    </div>
  )
}
