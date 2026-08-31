import { useCallback, useMemo, useState } from 'react'

// T-387（FR-125.2 L1）：列表列显隐偏好——console-artifactory-parity L1
// （列选器）两载体页（RepositoriesPage / AuditPage）共享的持久层。
//
// - 存储：localStorage per-page（键由调用方传，如 'binflow-console-cols-
//   repos'），与 ThemeContext（binflow-console-theme）/ 最近搜索
//   （binflow-console-recent-searches）同款「浏览器本地、无账号面」偏好。
//   值 = 被隐藏列 id 的 JSON 数组（默认空数组 = 全显，存储面最小）。
// - 防漂移：读回按当页列集清洗——未知 id 剔除（列集演进不炸）、「全隐」
//   视为无效回落全显（至少一列在场是表格的底线语义，与 toggle 的守卫
//   同一条不变量）。
// - 不可用（隐私模式等）：try/catch 双向，退化为会话内不持久
//   （SearchPage recentSearches 同款姿态）。

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
  /** 全选复位（= 清空隐藏集） */
  reset: () => void
}

/** 读回 + 清洗（「全隐」无效 → 全显） */
function readHidden(all: readonly string[], key: string): Set<string> {
  let raw: unknown
  try {
    raw = JSON.parse(window.localStorage.getItem(key) ?? '[]')
  } catch {
    return new Set()
  }
  if (!Array.isArray(raw)) return new Set()
  const known = new Set(all)
  const hidden = new Set(raw.filter((v): v is string => typeof v === 'string' && known.has(v)))
  if (hidden.size >= all.length) return new Set()
  return hidden
}

export function useColumnPrefs(all: readonly string[], storageKey: string): ColumnPrefs {
  // 页面侧列集是模块级常量——useMemo 只为稳定回调依赖，不求演进响应
  const ids = useMemo(() => [...all], [all])
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => readHidden(ids, storageKey))

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
    const empty = new Set<string>()
    setHidden(empty)
    persist(empty)
  }, [persist])

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
