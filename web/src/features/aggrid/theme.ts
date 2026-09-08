// AG Grid v36 主题桥（新栈 token 桥接层——.ts 文件，与 styles/tw 同级的
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
// 参数集：colorScheme 部件供给的四根（背景/前景/边框/chrome）+ accent
// ——其余（行 hover/选中/奇偶行等）由 quartz 基座以 ref 派生。
import { themeQuartz } from 'ag-grid-community'

/** 新栈 token 桥接主题（全局单例——参数是纯 var() 引用，零字面色值） */
export const agThemeBridge = themeQuartz.withParams({
  backgroundColor: 'var(--bf-surface-1)',
  foregroundColor: 'var(--bf-text)',
  borderColor: 'var(--bf-border)',
  chromeBackgroundColor: 'var(--bf-surface-2)',
  accentColor: 'var(--bf-accent)',
})
