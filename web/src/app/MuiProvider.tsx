import { useMemo } from 'react'
import type { ReactNode } from 'react'
import CssBaseline from '@mui/material/CssBaseline'
import { ThemeProvider as MuiThemeProvider } from '@mui/material/styles'
import { createTheme } from '@mui/material/styles'

import { useTheme } from './ThemeContext'
import type { Theme } from './ThemeContext'

// MUI 主题（T-344 批 A 重写，mui-native-visual §2）：基底 = createTheme()
// 默认值（MUI v7 默认设计语言），仅保留规范列明的偏离项——品牌只留主色
// 与有限微调，组件皮肤/密度/动效交还 MUI 主题系统。
//
//   1. palette：双模式具体色板取值逐项对齐 src/styles/tokens.css 的
//      --bf-* 语义 token（亮色 :root / 暗色 [data-theme='dark']）。MUI 的
//      createTheme 拒绝 var() 引用（augmentColor 需真实色值算对比度与
//      hover 派生色），因此以字面量复刻 token 值——tokens.css 仍是唯一
//      权威，改 token 必须同步本文件（文件头即契约，R5）。本文件是 TSX
//      侧唯一色值字面量豁免层（assert-tokens TSX 腿，§5.2）。
//   2. 深浅色跟随：消费既有 ThemeContext（data-theme 属性），翻转时重建
//      MUI theme——不引 MUI colorSchemes 状态机，避免两套主题真值。
//   3. 密度档 = MUI small 全家（§2.4 components 默认值）；hover/focus 派
//      生色交还 MUI augmentColor（action 覆写删除）。
// 组件层 sx 色板一律 theme.palette（assert-tokens 禁 var(--bf-*) 色彩
// token 引用；--bf-sp-*/mono/z 布局 token 与 sidebar 系侧栏身份例外）。

/** 系统字体栈（tokens.css body 同值复刻；R6：不加载 Roboto 网络字体） */
const FONT_STACK =
  "system-ui, -apple-system, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"

/** 亮色 palette（对齐 tokens.css :root；命名 = --bf- token 语义） */
const LIGHT = {
  primary: '#0b6bcb', // --bf-accent
  secondary: '#55606e', // --bf-text-2（占位防默认紫误用；组件禁用 secondary）
  error: '#c9372f', // --bf-danger
  success: '#157f3d', // --bf-success
  warning: '#946200', // --bf-warning
  info: '#0b6bcb', // --bf-info
  bg: '#f3f5f7', // --bf-bg
  surface1: '#ffffff', // --bf-surface-1
  text: '#1d232c', // --bf-text
  text2: '#55606e', // --bf-text-2
  divider: '#d3dae2', // --bf-border
}

/** 暗色 palette（对齐 tokens.css [data-theme='dark']） */
const DARK = {
  primary: '#4aa3ff', // --bf-accent
  secondary: '#9aa4b2', // --bf-text-2
  error: '#f0605d', // --bf-danger
  success: '#3fbf6f', // --bf-success
  warning: '#d9a23a', // --bf-warning
  info: '#6cb2ff', // --bf-info
  bg: '#12161d', // --bf-bg
  surface1: '#1a202a', // --bf-surface-1
  text: '#e9edf3', // --bf-text
  text2: '#9aa4b2', // --bf-text-2
  divider: '#2b3442', // --bf-border
}

function muiTheme(t: Theme) {
  const c = t === 'dark' ? DARK : LIGHT
  return createTheme({
    palette: {
      mode: t,
      // 品牌主色：唯一品牌项。light/dark/contrastText/hover 全交 MUI
      // augmentColor 派生（§2.1），不再手写。
      primary: { main: c.primary },
      secondary: { main: c.secondary },
      error: { main: c.error },
      success: { main: c.success },
      warning: { main: c.warning },
      info: { main: c.info },
      background: { default: c.bg, paper: c.surface1 },
      text: { primary: c.text, secondary: c.text2 },
      divider: c.divider,
      // action 不覆写：悬停/激活反馈回归 MUI 按 mode 的默认派生
    },
    shape: { borderRadius: 6 }, // 品牌微调（= --bf-r-md；MUI 默认 4）
    typography: {
      fontFamily: FONT_STACK,
      fontSize: 14, // MUI 默认档（§2.2：自 13 上调——「像 MUI」核心一步）
      button: { textTransform: 'none' }, // 控制台按钮从不大写（Artifactory 观感）
    },
    components: {
      // 密度档收口（§2.4）：MUI small 全家，替代 muiAtoms 手写 32px 压制
      MuiButton: { defaultProps: { size: 'small' } },
      MuiTextField: { defaultProps: { size: 'small' } },
      MuiFormControl: { defaultProps: { size: 'small' } },
      MuiChip: { defaultProps: { size: 'small' } },
      MuiTable: { defaultProps: { size: 'small' } },
      MuiListItemButton: { defaultProps: { dense: true } },
      // 涟漪保留（显式声明：像 MUI；MUI 默认即开，落字为凭防误关）
      MuiButtonBase: { defaultProps: { disableRipple: false } },
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
