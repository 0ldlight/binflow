import { useEffect, useState } from 'react'

// 骨架屏（console-ux §5.1/§5.2）：150ms 延迟显示防闪烁；脉动动画在
// prefers-reduced-motion 下由 CSS 关闭（base.css）。

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
        <div key={i} className="skeleton line" style={{ width: `${88 - i * 12}%` }} />
      ))}
    </div>
  )
}
