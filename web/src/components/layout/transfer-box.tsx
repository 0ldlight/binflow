// 新栈双列穿梭（console-m8 §3.3 C5——旧 pages/security/TransferBox.tsx 的
// MUI 实现退役，本件等价承接）。
// 条目本体 = 原生 checkbox——勾选即移入右侧、取消即移回，键盘（Tab +
// Space）天然可达；冻结锚（user-form-group-<name> / group-form-member-<name>
// / perm-repo-pick-<key>）由 itemTestid 透传落 input 本体。
// 批 6 重皮（design-system-plan §4 P2「TransferBox 重皮」）：security.css
// 的 .transfer-* 十规则迁 Tailwind 语义类（等值：token 引用 → 语义类；
// 720px 断点 → max-[720px]）；.transfer-item 类留 DOM 作 e2e 钩
// （m9/users-groups 的 transfer-selected .transfer-item 选择器）。
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
    <label
      key={item.name}
      className="transfer-item flex min-h-7 cursor-pointer items-center gap-2 text-dense has-[input:disabled]:cursor-default has-[input:disabled]:opacity-70"
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onToggle(item.name, e.target.checked)}
        {...(itemTestid?.(item.name) ? { 'data-testid': itemTestid(item.name) } : {})}
      />
      <span className="font-mono text-[0.95em]" lang="en">
        {item.label ?? item.name}
      </span>
      {note(item)}
    </label>
  )

  return (
    <div className="grid max-w-[720px] grid-cols-2 gap-3 max-[720px]:grid-cols-1">
      <div
        className="flex min-h-[148px] max-h-[300px] flex-col rounded-md border border-border bg-surface-1"
        data-testid="transfer-available"
      >
        <div className="flex items-baseline justify-between border-b border-border px-3 py-2 text-aux font-semibold text-muted-foreground">
          <span>{availableLabel}</span>
          <span className="tabular-nums text-subtle">{available.length}</span>
        </div>
        {/* tabIndex：可滚动区键盘可达（axe scrollable-region-focusable） */}
        <div
          className="flex-1 overflow-y-auto px-2 py-1"
          tabIndex={0}
          role="group"
          aria-label={t('{availableLabel}（{v1}）', { availableLabel: availableLabel, v1: available.length })}
        >
          {available.length === 0 ? <p className="m-2 text-aux text-subtle">{t('（无可选项）')}</p> : available.map((i) => row(i, false))}
        </div>
      </div>
      <div
        className="flex min-h-[148px] max-h-[300px] flex-col rounded-md border border-border bg-surface-1"
        data-testid="transfer-selected"
      >
        <div className="flex items-baseline justify-between border-b border-border px-3 py-2 text-aux font-semibold text-muted-foreground">
          <span>{selectedLabel}</span>
          <span className="tabular-nums text-subtle">{chosen.length}</span>
        </div>
        <div
          className="flex-1 overflow-y-auto px-2 py-1"
          tabIndex={0}
          role="group"
          aria-label={t('{selectedLabel}（{v1}）', { selectedLabel: selectedLabel, v1: chosen.length })}
        >
          {chosen.length === 0 ? <p className="m-2 text-aux text-subtle">{t('未选择项')}</p> : chosen.map((i) => row(i, true))}
        </div>
      </div>
    </div>
  )
}
