import type { SxProps } from '@mui/material/styles'
import type { Theme } from '@mui/material/styles'

// MUI 组件层配方（T-344 批 A 收口，mui-native-visual §2.4）：
//
//   - 压制配方退役：denseInputSx（32px 直角输入压制）/ badgeChipSx（方角
//     徽章压制）/ dangerBtnSx（手写悬停底）按规范删除——「让 MUI 默认皮肤
//     生效」的正解是删压制规则本身。本票起保名空对象（inert）：约 190 处
//     页面引用点归批 B/C 换装时逐波摘除，全部摘净后删除导出（批 D 死类
//     清扫）。视觉即时交还主题：输入 = OutlinedInput small 默认浮标形态、
//     徽章 = Chip small 默认胶囊、危险钮 = color="error" 默认派生。
//   - 三 btn 配方归一为 cellBtnSx：只留布局项（minWidth/minHeight/
//     padding），色彩项删（color="error" / 默认 variant 承载）。旧名
//     quietBtnSx / rowBtnSx 暂为别名（引用点归批 C 摘）。
//   - monoInputSx 保留：mono 字体非皮肤诉求——emotion 注入序在 base.css
//     之后，(0,1,0) 的 .mono 类压不过 MUI input 的 font 继承（font
//     shorthand 覆写 font-family），显式提特异性到 (0,2,0) 落 mono 栈的
//     修法仍必要。
//
// 色值纪律（assert-tokens TSX 腿）：配方内不得引用 --bf-* 色彩 token——
// 色板一律 theme.palette（R5 双源同值）或组件默认。

/** 压制配方退役位（inert）：密集输入皮肤已交还主题（§2.4 组件默认值 +
 *  OutlinedInput 默认形态）。引用点批 C 摘净后删除本导出。 */
export const denseInputSx = {} satisfies SxProps<Theme>

/** 压制配方退役位（inert）：徽章皮肤已交还主题（Chip small 默认胶囊，
 *  语义色经 color prop）。引用点批 C 摘净后删除本导出。 */
export const badgeChipSx = {} satisfies SxProps<Theme>

/** 压制配方退役位（inert）：危险钮悬停底已交还主题（color="error" 的
 *  MUI 派生）。引用点批 C 摘净后删除本导出。 */
export const dangerBtnSx = {} satisfies SxProps<Theme>

/** 行内小按钮（表格单元格内的次级动作）：三配方归一，布局项 + 主题派生
 *  的中性色板（palette 快捷，非 --bf-* token——断言腿合规）。outlined
 *  primary 的主色文字在表格 hover 行（surface-2 ≈ action.hover）上实测
 *  4.48:1（T-299 rowBtnSx 注记的坑，色彩项退役即回归）——文字/边框钉
 *  text/divider（全承载面 ≥4.5:1），危险动作仍走使用点 color="error"。 */
export const cellBtnSx = {
  minWidth: 0,
  minHeight: 24,
  padding: '2px 6px',
  color: 'text.primary',
  borderColor: 'divider',
} satisfies SxProps<Theme>

/** 别名（批 C 归一过渡）：行内弱化钮 */
export const quietBtnSx = cellBtnSx

/** 别名（批 C 归一过渡）：行内 outlined 钮 */
export const rowBtnSx = cellBtnSx

/** mono 输入（无 .field 包裹的场景——filter-bar / 矩阵添加位等）：
 *  与组件默认皮肤并用（非压制配方），(0,2,0) 落 mono 栈。 */
export const monoInputSx = {
  '& .MuiOutlinedInput-input': {
    fontFamily: 'var(--bf-mono)',
  },
} satisfies SxProps<Theme>
