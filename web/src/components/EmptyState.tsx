import type { ReactNode } from 'react'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'

// 空态（console-ux §5.1）：一句话说明 + 一个主行动 + 一条提示。
// 禁止裸「暂无数据」；两种空（从未有数据 / 过滤后为空）由调用方给文案。
// T-344 批 B：div → MUI Stack + Typography；`empty-state`/`hint` 类名保
// 留在 DOM 上（spec 类钩子，mui-native-visual §3.8——CSS 规则已退役）。

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
    <Stack className="empty-state" data-testid={testid} spacing={1} sx={{ py: 2, alignItems: 'flex-start' }}>
      <Typography color="text.secondary">{message}</Typography>
      {action}
      {hint && (
        <Typography variant="body2" className="hint" color="text.secondary">
          {hint}
        </Typography>
      )}
    </Stack>
  )
}
