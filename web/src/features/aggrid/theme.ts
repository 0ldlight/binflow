// AG Grid v36 主题桥（新栈 token 桥接层——.ts 文件，与 design-system/ 同级的
// token 消费定位；assert-tokens 的 TSX 腿不扫本层，语义等价 MuiProvider
// 的 palette 复刻位：唯一职责是把 AG Grid 的 Theming API 参数桥到
// --bf-* 语义 token——值随 <html data-theme> 活动解析，深浅零双轨）。
//
// 裁定史：CSS 文件式主题（ag-theme-quartz[-dark].css）在 v36 会把亮色
// 参数物化进内部 styled-root（portal 到 body 级，仓内类不可达）——
// 该路线弃用；withPart(colorSchemeVariable) 的 data-ag-theme-mode 选档
// 对 portaled styled-root 同样不生效。withParams 的 CSS 变量引用
// （ColorValue 接受 var()）是唯一直接消费 token 的官方通道。
//
// 批 6（design-system-plan §6 批 6「AG Grid 主题双谱」）：自四根部件
// 供给扩到行/密度面——行 hover=surface-2、选中行=accent 10% 软底派生
// （§4.2 DataTable selected 同口径）、cell 字体/字号走 mono 之外的
// sans 主档（--bf-font-sans / 13px=--bf-fs-sm 密度主档）、rowHeight 32
// 对齐控制台表格密度。对齐 token 而非逐像素复刻旧主题（§8 妥协登记：
// AG Grid 是皮肤非交互面）；表头/奇偶行等其余派生仍由 quartz 基座从
// backgroundColor/foregroundColor 推导。
import { themeQuartz } from 'ag-grid-community'

/** 新栈 token 桥接主题（全局单例——参数是纯 var() 引用，零字面色值） */
export const agThemeBridge = themeQuartz.withParams({
  backgroundColor: 'var(--bf-surface-1)',
  foregroundColor: 'var(--bf-text)',
  borderColor: 'var(--bf-border)',
  chromeBackgroundColor: 'var(--bf-surface-2)',
  accentColor: 'var(--bf-accent)',
  // 行态（§4.2 DataTable★ 同口径：hover=surface-2 / selected=accent 10% 软底）
  rowHoverColor: 'var(--bf-surface-2)',
  selectedRowBackgroundColor: 'color-mix(in srgb, var(--bf-accent) 10%, var(--bf-surface-1))',
  // 密度（13px = --bf-fs-sm 表格密度主档；rowHeight 32 对齐轻量 DataTable）
  cellFontFamily: 'var(--bf-font-sans)',
  cellFontSize: 13,
  rowHeight: 32,
})
