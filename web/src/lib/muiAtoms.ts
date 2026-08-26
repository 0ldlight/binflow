import type { SxProps } from '@mui/material/styles'
import type { Theme } from '@mui/material/styles'

// MUI 组件层密度/配色收口（T-299 批次一：Login / 壳层 / 仓库域）。
//
// BOARD 2026-08-26 指令「前端 UI 框架使用 MUI，交互逻辑按 Artifactory」——
// 组件换 MUI，但控制台的密度带（console-ux §7.2：控件 32px / 主字号 13px）
// 与自有皮肤（tokens.css 三阶纵深）不变。MUI 自带的 Material 密度
// （outlined small 40px / 按钮 36px）与本规范有出入，统一在 sx 收口：
//   - 色值一律 var(--bf-*)（assert-tokens 纪律的 TSX 侧同款口径——不在
//     组件里写死颜色；主题翻转随 data-theme 属性即时生效，与 MuiProvider
//     的主题重建双轨同值）；
//   - 输入 = base.css .field input 的同形复刻（surface-3 底 / border 边
//     框 / accent 焁焦 / 32px 控件带）；
//   - 表单小按钮（行内/次级动作）= .btn 家族的 32px 带。
// recipes 为纯样式对象（无组件封装）——调用侧仍是裸 MUI 组件，插槽/受控
// 属性一望即知（锚点落在 input/select 上的纪律不被封装遮蔽）。

/** 输入族（TextField/OutlinedInput）：surface-3 底、border 边框、32px 带 */
export const denseInputSx: SxProps<Theme> = {
  '& .MuiOutlinedInput-root': {
    minHeight: 32,
    background: 'var(--bf-surface-3)',
    color: 'var(--bf-text)',
    fontSize: 'var(--bf-fs-form)',
    '& fieldset': { borderColor: 'var(--bf-border)', borderRadius: 'var(--bf-r-sm)' },
    '&:hover:not(.Mui-disabled) fieldset': { borderColor: 'var(--bf-border-strong)' },
    '&.Mui-focused fieldset': { borderColor: 'var(--bf-accent)' },
    '&.Mui-disabled': {
      background: 'var(--bf-surface-3)',
      color: 'var(--bf-text-muted)',
      '& fieldset': { borderColor: 'var(--bf-border)' },
    },
  },
  '& .MuiOutlinedInput-input': {
    padding: '5px 12px',
    '&::placeholder': { color: 'var(--bf-text-muted)', opacity: 1 },
  },
  // textarea（multiline）：`.field textarea` 的 base 样式会叠出内边框，
  // 这里显式压平（外框由 OutlinedInput fieldset 承载）
  '& .MuiInputBase-inputMultiline': {
    padding: '6px 12px',
    border: 'none',
    background: 'transparent',
    minHeight: 64,
    resize: 'vertical',
  },
  // native select（TextField select + slotProps.select.native）：箭头图标
  // 与 option 底色走 token
  '& .MuiNativeSelect-select': {
    paddingRight: '28px',
    '&:focus': { backgroundColor: 'transparent', borderRadius: 'var(--bf-r-sm)' },
  },
}

/** 行内弱化按钮（删除入口/用量重试一类）：.row-del 同形（text-2 常态、
 * danger 悬停、surface-2 悬停底——对比度纪律见 repositories.css 注记） */
export const quietBtnSx: SxProps<Theme> = {
  color: 'var(--bf-text-2)',
  fontSize: 'var(--bf-fs-aux)',
  minWidth: 0,
  minHeight: 24,
  padding: '2px 6px',
  '&:hover': {
    color: 'var(--bf-danger)',
    backgroundColor: 'var(--bf-surface-2)',
  },
  '&:focus-visible': { outline: '2px solid var(--bf-accent)' },
}
