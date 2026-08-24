import type { ReactNode } from 'react'

// 双列穿梭（console-m8 §3.3 C5，对齐 reverse §4.11 Available/Selected 形态）：
// 左「可选」右「已选」，各带计数；空侧显示「未选择项」（No Items Selected
// 语义）。BinFlow 皮肤下条目本体是 checkbox——勾选即移入右侧、取消即移回，
// 键盘（Tab + Space）天然可达。条目 testid（itemTestid prop）自 T-267 死锚
// 退役后无在册消费者，调用方均不再传；后续批次需要时经 §10 入册再启用。
//
// 本文件仅服务本票三页（Users/UserDetail/Groups）——area 目录内的新助手，
// 不触碰共享组件层（改共享层需另开票）。

export interface TransferItem {
  /** 条目键（用户/组名——锚与提交体都用它） */
  name: string
  /** 附加说明行（如组描述/用户 email）；可空 */
  note?: string
}

export function TransferBox({
  items,
  selected,
  onToggle,
  disabled = false,
  availableLabel = '可选',
  selectedLabel = '已选',
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
  /** 条目 checkbox 的 testid（可选——T-267 死锚退役后调用方均不再传，保留 prop 供后续批次复用） */
  itemTestid?: (name: string) => string
  /** 已选侧的附加说明（如组 → 成员数之外的用法）；默认同 available 侧 */
  renderNote?: (item: TransferItem) => ReactNode
}) {
  const available = items.filter((i) => !selected.includes(i.name))
  const chosen = items.filter((i) => selected.includes(i.name))
  const note = renderNote ?? ((i: TransferItem) => (i.note ? <span className="text-muted">{i.note}</span> : null))

  const row = (item: TransferItem, checked: boolean) => (
    <label key={item.name} className="transfer-item">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onToggle(item.name, e.target.checked)}
        data-testid={itemTestid?.(item.name)}
      />
      <span className="mono" lang="en">
        {item.name}
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
        <div className="transfer-list" tabIndex={0} role="group" aria-label={`${availableLabel}（${available.length}）`}>
          {available.length === 0 ? <p className="transfer-empty">（无可选项）</p> : available.map((i) => row(i, false))}
        </div>
      </div>
      <div className="transfer-col" data-testid="transfer-selected">
        <div className="transfer-head">
          <span>{selectedLabel}</span>
          <span className="transfer-count">{chosen.length}</span>
        </div>
        <div className="transfer-list" tabIndex={0} role="group" aria-label={`${selectedLabel}（${chosen.length}）`}>
          {chosen.length === 0 ? <p className="transfer-empty">未选择项</p> : chosen.map((i) => row(i, true))}
        </div>
      </div>
    </div>
  )
}
