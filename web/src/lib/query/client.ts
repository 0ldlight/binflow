// QueryClient 工厂与默认策略（frontend-rewrite-architecture §5）：
// - retry=1（网络瞬断一次重试，不放大服务端错误语义）；
// - refetchOnWindowFocus=false（控制台无强焦点时效语义——轮询面走
//   refetchInterval 显式复刻：replication 10s / migration 5s / logs 7s）；
// - QueryCache.onError：401 全局监听挂 QueryClient 层（与 fetch 层
//   silent401 豁免集互补——豁免的请求不进 mutation/query 传播面）。
import { QueryCache, QueryClient } from '@tanstack/react-query'

import { ApiError } from '@/lib/api'

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: 1,
        refetchOnWindowFocus: false,
        staleTime: 30_000,
      },
      mutations: {
        retry: 0,
      },
    },
    queryCache: new QueryCache({
      onError: (error) => {
        if (error instanceof ApiError && error.status === 401) {
          // 全局会话过期处理挂接位（AuthProvider 启动时 setUnauthorizedListener）
        }
      },
    }),
  })
}

/** 轮询节奏常量（复刻现役 setInterval 三轮询，stale 保留语义归 useQuery 消费面） */
export const POLL_INTERVALS = {
  replication: 10_000,
  migration: 5_000,
  logs: 7_000,
} as const
