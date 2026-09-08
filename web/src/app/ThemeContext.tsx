import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'

// 主题（console-m8 §5 + ADR-0029 Q2 终裁：默认亮色，对齐 Artifactory
// 默认观感；暗色为等价次主题，token 零分叉——推翻旧 console-ux 的
// 暗色优先）。偏好持久化在 localStorage；首访（未设置时）尊重系统
// prefers-color-scheme，系统未表态或不支持时回落亮色。

export type Theme = 'light' | 'dark'

const STORAGE_KEY = 'binflow-console-theme'

function initialTheme(): Theme {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'light' || v === 'dark') return v
  } catch {
    // 隐私模式等 localStorage 不可用：继续按系统偏好回落
  }
  if (typeof window.matchMedia === 'function') {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return 'light'
}

// 前端重写 P2（frontend-rewrite-architecture §1）：context 导出 = 过渡期
// 桥接位——legacy-bridge 以新栈 ThemeProvider 的真值注入本 context
// （MuiProvider 消费 useTheme 不改），保证过渡期 <html data-theme> 单一
// 真值。MUI 终验（MUI=0 门）时本文件随 legacy 树整体退役。
export const ThemeContext = createContext<{ theme: Theme; toggle: () => void }>({
  theme: 'light',
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
