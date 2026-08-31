import type { ReactNode } from 'react'
import Box from '@mui/material/Box'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'

// 空态（console-ux §5.1）：一句话说明 + 一个主行动 + 一条提示。
// 禁止裸「暂无数据」；两种空（从未有数据 / 过滤后为空）由调用方给文案。
// T-344 批 B：div → MUI Stack + Typography；`empty-state`/`hint` 类名保
// 留在 DOM 上（spec 类钩子，mui-native-visual §3.8——CSS 规则已退役）。
//
// T-388（FR-125.3 / parity F2）：可选插画槽 `illustration`——Artifactory
// 空态形态 = 插画位 → 说明 → 主行动。槽位规格化（真插画资产归后续设计票，
// 换稿零返工的契约在此）：
//   · 尺寸 40×40px（parity F2 差距行口径）、位于说明上方；
//   · currentColor 线稿（候选 1「容器·双箭流」隐喻的占位线稿——容器虚线
//     框示「待填充」，双箭示「制品流入」；不引第三方插画库）；
//   · 装饰位 aria-hidden——语义由 message 文本承载，axe 零新扫描面；
//   · 挂载口径：主数据面空态（从未有数据 / 过滤后空两种）挂，403 无权限
//     卡不挂（错误语义不装饰，§5.1 Error 纪律）。

/** F2 插画槽本体（占位线稿——真插画资产票替换内部 svg，槽位契约不变） */
function EmptyArt() {
  return (
    <Box
      className="empty-art"
      data-testid="empty-art"
      aria-hidden="true"
      sx={{ width: 40, height: 40, color: 'text.secondary', flexShrink: 0 }}
    >
      <svg width={40} height={40} viewBox="0 0 48 48" fill="none" focusable="false" style={{ display: 'block' }}>
        {/* 容器（mark 几何族的圆角方——虚线示待填充） */}
        <rect
          x="5"
          y="5"
          width="38"
          height="38"
          rx="9"
          stroke="currentColor"
          strokeWidth="3"
          strokeDasharray="7 6"
        />
        {/* 双箭流（mark 候选 1 的箭隐喻：chevron + 实心三角，低一档示流入） */}
        <path
          d="M14.5 16.2 24.5 24l-10 7.8"
          stroke="currentColor"
          strokeWidth="3.5"
          strokeLinecap="round"
          strokeLinejoin="round"
          opacity="0.55"
        />
        <path d="M23.5 14.6 33.5 24l-10 9.4Z" fill="currentColor" opacity="0.55" />
      </svg>
    </Box>
  )
}

export function EmptyState({
  message,
  action,
  hint,
  illustration,
  testid = 'empty-state',
}: {
  message: string
  action?: ReactNode
  hint?: string
  /** F2 插画槽（默认关——63 既有落点零变化，主列表页空态逐位启用） */
  illustration?: boolean
  testid?: string
}) {
  return (
    <Stack className="empty-state" data-testid={testid} spacing={1} sx={{ py: 2, alignItems: 'flex-start' }}>
      {illustration && <EmptyArt />}
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
