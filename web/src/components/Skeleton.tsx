import { useEffect, useState } from 'react'
import MuiSkeleton from '@mui/material/Skeleton'

// 骨架屏（console-ux §5.1/§5.2）：150ms 延迟显示防闪烁；脉动动画交 MUI
// Skeleton（pulse 默认档）。T-344 批 B：div.skeleton+CSS 手作动画退役，
// MUI 原生形态（variant text——形状与文本行近似，§5.2 骨架准则）。
// 批 D 收口：树域/搜索页的「skeleton line」页面直连消费已摘净
// （MuiSkeleton variant=text 直挂），base.css 的 .skeleton 族整块退役。

export function useElapsed(ms: number): boolean {
  const [ready, setReady] = useState(false)
  useEffect(() => {
    const t = window.setTimeout(() => setReady(true), ms)
    return () => window.clearTimeout(t)
  }, [ms])
  return ready
}

export function Skeleton({ lines = 3 }: { lines?: number }) {
  const visible = useElapsed(150)
  if (!visible) return null
  return (
    <div data-testid="skeleton" aria-hidden="true">
      {Array.from({ length: lines }, (_, i) => (
        <MuiSkeleton key={i} variant="text" width={`${88 - i * 12}%`} sx={{ my: 0.5 }} />
      ))}
    </div>
  )
}
