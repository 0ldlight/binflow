import type { SxProps } from '@mui/material/styles'
import type { Theme } from '@mui/material/styles'

// MUI 组件层密度/配色收口（T-299 批次一：Login / 壳层 / 仓库域；
// T-300 批次二：浏览树/搜索/安全治理/admin 余面——追加 rowBtnSx /
// dangerBtnSx / badgeChipSx）。
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

/** 行内 outlined 小按钮（Set Me Up / 部署 / 保存一类）：outline 主色
 *  （accent）在表格 hover 行 surface-2 上 4.48:1（差 0.02 不过 axe 门），
 *  文字/边框显式走 text/border token，悬停底升 surface-3（同 token 家族，
 *  全承载面 ≥4.5:1）。T-299 批一在 RepositoriesPage 内首立（rowBtnSx），
 *  批次二起上收到 muiAtoms 供批次二页面共享——批一页面的本地副本随下批
 *  共享层票归一（文件不重叠，不动批一页）。 */
export const rowBtnSx: SxProps<Theme> = {
  color: 'var(--bf-text)',
  borderColor: 'var(--bf-border)',
  '&:hover': {
    borderColor: 'var(--bf-border-strong)',
    backgroundColor: 'var(--bf-surface-3)',
  },
}

/** 危险动作钮（删除/执行 GC 一类）：.btn.danger 同义——error 色板走
 *  MuiProvider 主题（palette.error = --bf-danger 双主题对齐），悬停底
 *  由 MUI 派生色承载（对比度经主题色算得，双主题 ≥4.5:1）。 */
export const dangerBtnSx: SxProps<Theme> = {
  '&:hover': { backgroundColor: 'color-mix(in srgb, var(--bf-danger) 10%, transparent)' },
}

/** mono 输入（无 .field 包裹的场景——filter-bar / 矩阵添加位等）：
 *  emotion 样式注入在 base.css 之后，(0,1,0) 的 .mono 类压不过 MUI
 *  input 的字体继承（font shorthand 覆写 font-family）——显式提特异性
 *  到 (0,2,0) 落 mono 栈。与 denseInputSx 展开合用。 */
export const monoInputSx = {
  '& .MuiOutlinedInput-input': {
    fontFamily: 'var(--bf-mono)',
  },
} satisfies SxProps<Theme>

/** 徽章 Chip（.badge 家族的 MUI 承载）：基类色继续由
 *  .badge/.badge.neutral|success|warning|danger 供给（className 续挂，
 *  后载样式表按同级特异性压过 MUI 默认），sx 只压密度与半径——批次二
 *  裁定 badge→Chip（组件层换装，锚与视觉语言不动）。 */
export const badgeChipSx: SxProps<Theme> = {
  height: 'auto',
  minHeight: 20,
  padding: '2px 7px',
  borderRadius: 'var(--bf-r-sm)',
  fontSize: 'var(--bf-fs-aux)',
  lineHeight: 1.4,
  '& .MuiChip-label': { padding: 0, display: 'inline-flex', alignItems: 'center', gap: '4px' },
}
