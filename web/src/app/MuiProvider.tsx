import { useMemo } from 'react'
import type { ReactNode } from 'react'
import CssBaseline from '@mui/material/CssBaseline'
import { ThemeProvider as MuiThemeProvider } from '@mui/material/styles'
import { createTheme } from '@mui/material/styles'

import { useTheme } from './ThemeContext'
import type { Theme } from './ThemeContext'

// MUI 主题桥（T-291，BOARD 2026-08-26 指令：前端 UI 框架使用 MUI，交互
// 逻辑仍按 Artifactory / console-ux 既有规范）。
//
// 职责刻意最小（最小主题，存量页面不迁移）：
//   1. CssBaseline —— 与 base.css 同值的 body 背景/文字/字体基线（双源同
//      值，不产生视觉漂移）；
//   2. palette —— 双模式具体色板，**取值逐项对齐 src/styles/tokens.css
//      的 --bf-* 语义 token**（亮色 :root / 暗色 [data-theme='dark']）。
//      MUI 的 createTheme 拒绝 var() 引用（augmentColor 需真实色值计算
//      对比度与 hover 派生色），因此这里以字面量复刻 token 值——tokens.css
//      仍是唯一权威，改 token 时必须同步本文件（文件头即契约）。
//   3. 深浅色跟随：消费既有 ThemeContext（data-theme 属性那套），翻转时
//      重建 MUI theme——不引入 MUI 自己的 colorScheme 状态机，避免两套
//      主题真值。
// 组件层新增 CSS 仍走 --bf-* token（assert-tokens 纪律不变）；emotion 的
// 内联样式仅限 MUI 组件自身的 sx 微调，色值一律引用 var(--bf-*)。

/** base.css 的 body 字体栈（同值复刻，避免 Roboto 请求） */
const FONT_STACK =
  "system-ui, -apple-system, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"

/** 亮色 palette（对齐 tokens.css :root；命名 = --bf- token 语义） */
const LIGHT = {
  primary: '#0b6bcb', // --bf-accent
  accentFg: '#ffffff', // --bf-accent-fg
  error: '#c9372f', // --bf-danger
  success: '#157f3d', // --bf-success
  warning: '#946200', // --bf-warning
  info: '#0b6bcb', // --bf-info
  bg: '#f3f5f7', // --bf-bg
  surface1: '#ffffff', // --bf-surface-1
  surface3: '#dfe5ec', // --bf-surface-3
  border: '#d3dae2', // --bf-border
  text: '#1d232c', // --bf-text
  text2: '#55606e', // --bf-text-2
  textMuted: '#646f7b', // --bf-text-muted
}

/** 暗色 palette（对齐 tokens.css [data-theme='dark']） */
const DARK = {
  primary: '#4aa3ff', // --bf-accent
  accentFg: '#0b0e13', // --bf-accent-fg
  error: '#f0605d', // --bf-danger
  success: '#3fbf6f', // --bf-success
  warning: '#d9a23a', // --bf-warning
  info: '#6cb2ff', // --bf-info
  bg: '#12161d', // --bf-bg
  surface1: '#1a202a', // --bf-surface-1
  surface3: '#262f3d', // --bf-surface-3
  border: '#2b3442', // --bf-border
  text: '#e9edf3', // --bf-text
  text2: '#9aa4b2', // --bf-text-2
  textMuted: '#7f8a99', // --bf-text-muted
}

function muiTheme(t: Theme) {
  const c = t === 'dark' ? DARK : LIGHT
  return createTheme({
    palette: {
      mode: t,
      primary: { main: c.primary, contrastText: c.accentFg },
      secondary: { main: c.text2 },
      error: { main: c.error },
      success: { main: c.success },
      warning: { main: c.warning },
      info: { main: c.info },
      background: { default: c.bg, paper: c.surface1 },
      text: { primary: c.text, secondary: c.text2, disabled: c.textMuted },
      divider: c.border,
      action: { active: c.text2, hoverOpacity: 0.06 },
    },
    shape: { borderRadius: 6 }, // --bf-r-md
    typography: {
      fontFamily: FONT_STACK,
      fontSize: 13, // --bf-fs-body（密度主字号）
      button: { textTransform: 'none' }, // 控制台按钮从不大写（Artifactory 观感）
    },
  })
}

/** MUI 主题挂载点：必须位于 ThemeProvider 内（消费 useTheme） */
export function MuiProvider({ children }: { children: ReactNode }) {
  const { theme } = useTheme()
  const theme_ = useMemo(() => muiTheme(theme), [theme])
  return (
    <MuiThemeProvider theme={theme_}>
      <CssBaseline />
      {children}
    </MuiThemeProvider>
  )
}
