// QueryProvider：TanStack Query 装配（默认策略与 key 工厂见 lib/query）
// ——P1 仅模块就位，不接线（main.tsx 不动）。
import { QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import type { ReactNode } from 'react'

import { createQueryClient } from '@/lib/query'

export function QueryProvider({ children }: { children: ReactNode }) {
  // 实例生命周期随 Provider（React 18+ 严格模式下 useState 惰性初始化，
  // 不落模块级——避免热更/多根共享缓存）
  const [client] = useState(createQueryClient)
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}
