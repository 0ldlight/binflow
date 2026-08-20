import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'

// 主题（console-ux P4：暗色优先，亮色为等价次主题，token 零分叉）。
// 偏好持久化在 localStorage；未设置时默认暗色（管理工具惯例）。

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'binflow-console-theme'

function initialTheme(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'dark' || v === 'light') return v
  } catch {
    // 隐私模式等 localStorage 不可用：回落默认暗色
  }
  return 'dark'
}

const ThemeContext = createContext<{ theme: Theme; toggle: () => void }>({
  theme: 'dark',
  toggle: () => {},
})

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(initialTheme)

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try {
      localStorage.setItem(STORAGE_KEY, theme)
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
