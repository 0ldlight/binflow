import type { KeyboardEvent as ReactKeyboardEvent } from 'react'

// 键盘可达共享件（FR-75 / console-m8 §3.4 键盘清单 + §9，T-244）：
//
//   onTablistKeys   role=tablist 容器的 ←/→ 切换（Tab 组件 = tablist/tab +
//                   方向键，§9）——切换即选中 + 焦点随行（rAF 等重渲染后落焦）
//   onTableRowKeys  数据表行的 Enter 激活 + ↑/↓ 行移动（§3.4「表格行移动/
//                   行 = 链接（Enter 进入）」）——行元素需配 tabIndex={0}
//
// 树方向键与右键菜单（Shift+F10）在 ArtifactsBrowser 自持（节点几何与
// 菜单目标强耦合，不进共享层）。批量选择 Space 无承载面：console-m8
// §3.5 定案「无批量选择」（无复选框列、无批量端点），不造批量 UI。

/** tablist 的 ←/→：循环切换 + 选中 + 焦点随行。ids 与容器内 [role=tab]
 * 渲染序一致（条件渲染的 Tab 由调用方先过滤 ids）。泛型 T 让 setState 的
 * 联合类型直通（'general' | 'perms' 等）。 */
export function onTablistKeys<T extends string>(
  e: ReactKeyboardEvent<HTMLElement>,
  ids: readonly T[],
  current: T,
  onSelect: (id: T) => void,
): void {
  if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
  e.preventDefault()
  if (ids.length < 2) return
  const i = ids.indexOf(current)
  if (i < 0) return
  const next = ids[(i + (e.key === 'ArrowRight' ? 1 : -1) + ids.length) % ids.length]
  onSelect(next)
  const root = e.currentTarget
  // setTab 触发重渲染后按钮才存在/可见——rAF 后落焦（smu-tabs 同款时序）
  requestAnimationFrame(() => {
    const tabs = root.querySelectorAll<HTMLElement>('[role="tab"]')
    tabs[ids.indexOf(next)]?.focus()
  })
}

/** 数据表行键盘：Enter 激活（行 = 链接语义）；↑/↓ 在同 tbody 行间移动焦点。 */
export function onTableRowKeys(
  e: ReactKeyboardEvent<HTMLTableRowElement>,
  activate: () => void,
): void {
  if (e.key === 'Enter') {
    e.preventDefault()
    activate()
    return
  }
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
  e.preventDefault()
  const rows = Array.from(
    e.currentTarget.parentElement?.querySelectorAll<HTMLTableRowElement>('tr') ?? [],
  )
  const i = rows.indexOf(e.currentTarget)
  const next = e.key === 'ArrowDown' ? rows[i + 1] : rows[i - 1]
  next?.focus()
}
