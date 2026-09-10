// ThemeProvider（新栈）：机制与旧 app/ThemeContext.tsx 同构平移——
// <html data-theme> + localStorage 'binflow-console-theme'，首访随
// prefers-color-scheme，默认亮色（ADR-0029 Q2 终裁）；Tailwind dark
// 变体绑 [data-theme="dark"]（styles/tw/tailwind.css @custom-variant）
// ——存储约定零迁移成本。P1 仅模块就位，不接线。
import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'

import { STORAGE_KEYS } from '@/app/config'

export type Theme = 'light' | 'dark'

function initialTheme(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEYS.theme)
    if (v === 'light' || v === 'dark') return v
  } catch {
    // 隐私模式等 localStorage 不可用：继续按系统偏好回落
  }
  if (typeof window.matchMedia === 'function') {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return 'light'
}

const ThemeContext = createContext<{ theme: Theme; toggle: () => void }>({
  theme: 'light',
  toggle: () => {},
})

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(initialTheme)

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    // AG Grid v36 Theming API 的深浅选档（html[data-ag-theme-mode]——
    // grid 的 styled-root portal 到 body 级，仓内属性不可达）
    document.documentElement.dataset.agThemeMode = theme
    try {
      localStorage.setItem(STORAGE_KEYS.theme, theme)
    } catch {
      // 不可写则仅本次会话生效
    }
  }, [theme])

  const toggle = useCallback(() => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'))
  }, [])

  return <ThemeContext.Provider value={{ theme, toggle }}>{children}</ThemeContext.Provider>
}

export function useTheme(): { theme: Theme; toggle: () => void } {
  return useContext(ThemeContext)
}
