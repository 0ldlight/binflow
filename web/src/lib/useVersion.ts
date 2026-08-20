import { useEffect, useState } from 'react'

import { getVersion } from './api'
import type { VersionInfo } from './api'

// 版本号（顶栏/侧栏品牌位，console-ux §3.1/§3.5）：/api/system/version
// 是开放端点（只暴露产品名与构建 id），用模块级缓存避免每次挂壳重拉。
// 失败时显示 —（不伪装版本，Q4 延续）。

let cache: VersionInfo | null = null
let inflight: Promise<VersionInfo | null> | null = null

function fetchVersion(): Promise<VersionInfo | null> {
  if (!inflight) {
    inflight = getVersion()
      .then((v) => {
        cache = v
        return v
      })
      .catch(() => null)
  }
  return inflight
}

export function useVersion(): VersionInfo | null {
  const [info, setInfo] = useState<VersionInfo | null>(cache)

  useEffect(() => {
    if (cache) return
    let alive = true
    fetchVersion().then((v) => {
      if (alive) setInfo(v)
    })
    return () => {
      alive = false
    }
  }, [])

  return info
}
