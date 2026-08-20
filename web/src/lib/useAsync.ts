import { useCallback, useEffect, useState } from 'react'

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
 * 声明式服务端状态：fn 在 deps 变化时执行。
 * 403 映射为 forbidden（调用方决定隐藏或呈现无权限卡）。
 *
 * 旗标是**每次 effect 运行的闭包变量**而非共享 ref（review B2）：deps
 * 变化时旧请求仍在飞，共享 ref 会被新运行重置回 true，旧响应晚到即用
 * 旧数据覆盖新数据（T-99 仓库详情快速切换 A→B 必踩）；闭包语义下旧
 * 运行的 cleanup 已把它置 false，晚到响应被正确丢弃。StrictMode 双挂载
 * 下第一次运行的响应同样被丢弃。
 */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [tick, setTick] = useState(0)
  const [state, setState] = useState<Omit<AsyncState<T>, 'reload'>>({
    status: 'loading',
    data: null,
    error: null,
  })

  useEffect(() => {
    let alive = true
    setState({ status: 'loading', data: null, error: null })
    fn()
      .then((data) => {
        if (alive) setState({ status: 'ok', data, error: null })
      })
      .catch((err: unknown) => {
        if (!alive) return
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
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- fn 是调用点闭包，由 deps 刻画其输入
  }, [...deps, tick])

  const reload = useCallback(() => setTick((t) => t + 1), [])
  return { ...state, reload }
}
