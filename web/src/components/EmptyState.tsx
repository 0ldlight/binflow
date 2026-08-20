import type { ReactNode } from 'react'

// 空态（console-ux §5.1）：一句话说明 + 一个主行动 + 一条提示。
// 禁止裸「暂无数据」；两种空（从未有数据 / 过滤后为空）由调用方给文案。

export function EmptyState({
  message,
  action,
  hint,
  testid = 'empty-state',
}: {
  message: string
  action?: ReactNode
  hint?: string
  testid?: string
}) {
  return (
    <div className="empty-state" data-testid={testid}>
      <div>{message}</div>
      {action}
      {hint && <div className="hint">{hint}</div>}
    </div>
  )
}
