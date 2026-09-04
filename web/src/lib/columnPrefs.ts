import { useCallback, useMemo, useState } from 'react'

// T-387（FR-125.2 L1）：列表列显隐偏好——console-artifactory-parity L1
// （列选器）两载体页（RepositoriesPage / AuditPage）共享的持久层。
//
// - 存储：localStorage per-page（键由调用方传，如 'binflow-console-cols-
//   repos'），与 ThemeContext（binflow-console-theme）/ 最近搜索
//   （binflow-console-recent-searches）同款「浏览器本地、无账号面」偏好。
//   值 = 被隐藏列 id 的 JSON 数组（默认空数组 = 全显，存储面最小）。
// - 防漂移：读回按当页列集清洗——未知 id 剔除（列集演进不炸）、「全隐」
//   视为无效回落默认列（至少一列在场是表格的底线语义，与 toggle 的守卫
//   同一条不变量）。
// - 不可用（隐私模式等）：try/catch 双向，退化为会话内不持久
//   （SearchPage recentSearches 同款姿态）。
// - T-449（FR-144.6 断言反转②）：defaultHidden 第三参——列集不再全显为
//   缺省（Artifactory 对位：大小/sha256 是列选器可选项不默认在场）。
//   缺省 = []（既有调用方 repos/audit/users/groups 语义零变化）；存储
//   缺席/腐化/全隐回落均回落 defaultHidden 而非全显，「恢复默认」同值。

/** 列选器列定义：label = 菜单文案（与表头一致）；anchor = 菜单项锚
 *  （anchor-audit 的 anchor: 属性形态——数据驱动锚的字面量落点）。 */
export interface ColumnDef {
  id: string
  label: string
  anchor: string
}

export interface ColumnPrefs {
  hidden: ReadonlySet<string>
  visibleCount: number
  total: number
  isVisible: (id: string) => boolean
  /** 勾/弃一列；「弃」在只剩最后一列可见时拒绝（至少一列在场） */
  toggle: (id: string) => void
  /** 恢复默认列集（= 清空隐藏集回到 defaultHidden） */
  reset: () => void
}

/** 读回 + 清洗（「全隐」无效 → 回落默认列集） */
function readHidden(all: readonly string[], key: string, defaults: readonly string[]): Set<string> {
  const fallback = new Set(defaults.filter((id) => all.includes(id)))
  let raw: unknown
  try {
    const stored = window.localStorage.getItem(key)
    if (stored === null) return fallback // 首访：默认列集（T-449 起非全显）
    raw = JSON.parse(stored)
  } catch {
    return fallback
  }
  if (!Array.isArray(raw)) return fallback
  const known = new Set(all)
  const hidden = new Set(raw.filter((v): v is string => typeof v === 'string' && known.has(v)))
  if (hidden.size >= all.length) return fallback
  return hidden
}

export function useColumnPrefs(
  all: readonly string[],
  storageKey: string,
  defaultHidden: readonly string[] = [],
): ColumnPrefs {
  // 页面侧列集是模块级常量——useMemo 只为稳定回调依赖，不求演进响应
  const ids = useMemo(() => [...all], [all])
  const defaults = useMemo(() => [...defaultHidden], [defaultHidden])
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => readHidden(ids, storageKey, defaults))

  const persist = useCallback(
    (next: ReadonlySet<string>) => {
      try {
        window.localStorage.setItem(storageKey, JSON.stringify([...next]))
      } catch {
        // localStorage 不可用：会话内有效即可
      }
    },
    [storageKey],
  )

  const toggle = useCallback(
    (id: string) => {
      setHidden((prev) => {
        const next = new Set(prev)
        if (next.has(id)) {
          next.delete(id)
        } else {
          // 至少一列在场：已隐藏 total-1 列时最后一列不可再弃
          if (prev.size >= ids.length - 1) return prev
          next.add(id)
        }
        persist(next)
        return next
      })
    },
    [ids, persist],
  )

  const reset = useCallback(() => {
    const base = new Set(defaults)
    setHidden(base)
    persist(base)
  }, [persist, defaults])

  const isVisible = useCallback((id: string) => !hidden.has(id), [hidden])

  return {
    hidden,
    visibleCount: ids.length - hidden.size,
    total: ids.length,
    isVisible,
    toggle,
    reset,
  }
}
