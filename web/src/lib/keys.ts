import type { KeyboardEvent as ReactKeyboardEvent } from 'react'

// 键盘可达共享件（FR-75 / console-m8 §3.4 键盘清单 + §9，T-244）：
//
//   onTableRowKeys  数据表行的 Enter 激活 + ↑/↓ 行移动（§3.4「表格行移动/
//                   行 = 链接（Enter 进入）」）——行元素需配 tabIndex={0}
//
// onTablistKeys 已随 T-344 视觉批退役：末位消费者（repos/repo/authcfg/
// node 四处自持 tablist）全数换 MUI Tabs + selectionFollowsFocus（方向键
// 选择随焦点 = MUI 内建等价）；smu-tabs 是 SetMeUpDialog 自持 onTabKeys
// （querySelector 落焦），不经共享层。
//
// 树方向键与右键菜单（Shift+F10）在 ArtifactsBrowser 自持（节点几何与
// 菜单目标强耦合，不进共享层）。批量选择 Space 无承载面：console-m8
// §3.5 定案「无批量选择」（无复选框列、无批量端点），不造批量 UI。

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
