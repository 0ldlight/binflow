import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError } from './api'

// 服务端状态四态容器（console-ux §5.3）：loading / ok / error 之外单列
// forbidden——「API 403 即隐藏」是导航与卡片的收敛兜底（§3.3），与
// 「实例故障需要错误卡 + 重试」分开，避免非 admin 用户看到一排无权限红卡。
export type AsyncStatus = 'loading' | 'ok' | 'error' | 'forbidden'

export interface AsyncState<T> {
  status: AsyncStatus
  data: T | null
  error: ApiError | null
  /** 错误卡的重试按钮：重跑一次请求 */
  reload: () => void
}

/**
 * 声明式服务端状态：fn 在 deps 变化时执行；卸载后不落盘。
 * 403 映射为 forbidden（调用方决定隐藏或呈现无权限卡）。
 */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [tick, setTick] = useState(0)
  const [state, setState] = useState<Omit<AsyncState<T>, 'reload'>>({
    status: 'loading',
    data: null,
    error: null,
  })
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    setState({ status: 'loading', data: null, error: null })
    fn()
      .then((data) => {
        if (alive.current) setState({ status: 'ok', data, error: null })
      })
      .catch((err: unknown) => {
        if (!alive.current) return
        if (err instanceof ApiError && err.status === 403) {
          setState({ status: 'forbidden', data: null, error: err })
          return
        }
        setState({
          status: 'error',
          data: null,
          error: err instanceof ApiError ? err : new ApiError(0, String(err)),
        })
      })
    return () => {
      alive.current = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fn 是调用点闭包，由 deps 刻画其输入
  }, [...deps, tick])

  const reload = useCallback(() => setTick((t) => t + 1), [])
  return { ...state, reload }
}
