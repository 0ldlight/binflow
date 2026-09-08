// 侧栏一级条目图标槽（FR-125.4 / T-388——parity console-artifactory-parity
// §2 N2，V5 活体核验档位：**仅一级条目配图标、子项裸文本**；BinFlow 侧栏
// 全部条目均为一级，故 2 应用域 + 18 管理域逐条接线，分组标签不配）。
//
// [定案留痕] 图标源 = 通用 Material 图标（Material Design icons，Apache-2.0）
//   的 path 几何，按本仓 mono 纪律裸 SVG 内联——不走 @mui/icons-material
//   包（新增依赖 + 按钮面单枚 tree-shake 也要拖 emotion 包装，违背「保持
//   bundle 小」），不复用 T-390 pkg-icons 层（那是包型身份图标——docker/
//   maven 等协议徽记的语义层，与导航通用符号不同族；票面明示「pkg-icons
//   层之外」）。Artifactory 7.84 实测为 icon-font `i` 元素——BinFlow 无
//   icon-font 基建，内联 SVG 是等价的零请求形态（PkgIcon 先例）。
// [mono currentColor] fill="currentColor" 随条目文字色（含 active/hover 态
//   ——navItemSx 单点控色，图标零自身颜色消费，assert-tokens 腿 2/3 天然
//   干净）；16px 槽位（parity N2 差距行口径）；aria-hidden 装饰位（图标
//   恒与条目可见文字同现）。
// [testid] nav-icon = 家族锚（console-ux §10.5 T-388 批；18 落点同族名，
//   计数/在场断言用）；data-icon = 图标身份属性（非 testid——对账器口径外，
//   PkgIcon data-icon 先例）。

/** 侧栏一级条目图标名（与 APP_NAV/ADMIN_NAV 接线闭集一致） */
export type NavIconName =
  | 'dashboard'
  | 'account_tree'
  | 'inventory_2'
  | 'person'
  | 'group'
  | 'lock'
  | 'vpn_key'
  | 'shield'
  | 'history'
  | 'delete_sweep'
  | 'pie_chart'
  | 'sync'
  | 'backup'
  | 'delete'
  | 'bolt'
  | 'storage'
  | 'info'
  | 'card_membership'
  | 'pulse'
  | 'article'
  | 'bundle'
  | 'build'

/** Material 图标 path 几何（24×24 viewBox；Apache-2.0，逐枚 16px 槽校型） */
const PATHS: Record<NavIconName, string> = {
  dashboard: 'M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z',
  account_tree:
    'M22 11V3h-7v3H9V3H2v8h7V8h2v10h4v3h7v-8h-7v3h-2V8h2v3zM7 9H4V5h3v4zm10 6h3v4h-3v-4zm0-10h3v4h-3V5z',
  inventory_2:
    'M20 2H4c-1 0-2 .9-2 2v3.01c0 .72.43 1.34 1 1.69V20c0 1.1 1.1 2 2 2h14c.9 0 2-.9 2-2V8.7c.57-.35 1-.97 1-1.69V4c0-1.1-1-2-2-2zm-5 12H9v-2h6v2zm5-7H4V4h16v3z',
  person:
    'M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z',
  group:
    'M16 11c1.66 0 2.99-1.34 2.99-3S17.66 5 16 5c-1.66 0-3 1.34-3 3s1.34 3 3 3zm-8 0c1.66 0 2.99-1.34 2.99-3S9.66 5 8 5C6.34 5 5 6.34 5 8s1.34 3 3 3zm0 2c-2.33 0-7 1.17-7 3.5V19h14v-2.5c0-2.33-4.67-3.5-7-3.5zm8 0c-.29 0-.62.02-.97.05 1.16.84 1.97 1.97 1.97 3.45V19h6v-2.5c0-2.33-4.67-3.5-7-3.5z',
  lock:
    'M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1s3.1 1.39 3.1 3.1v2z',
  vpn_key:
    'M12.65 10C11.83 7.67 9.61 6 7 6c-3.31 0-6 2.69-6 6s2.69 6 6 6c2.61 0 4.83-1.67 5.65-4H17v4h4v-4h2v-4H12.65zM7 14c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2z',
  shield: 'M12 1 3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4z',
  history:
    'M13 3c-4.97 0-9 4.03-9 9H1l3.89 3.89.07.14L9 12H6c0-3.87 3.13-7 7-7s7 3.13 7 7-3.13 7-7 7c-1.93 0-3.68-.79-4.94-2.06l-1.42 1.42C8.27 19.99 10.51 21 13 21c4.97 0 9-4.03 9-9s-4.03-9-9-9zm-1 5v5l4.28 2.54.72-1.21-3.5-2.08V8h-2z',
  delete_sweep:
    'M15 16h4v2h-4zm0-8h4v2h-4zm0 4h4v2h-4zM3 18c0 1.1.9 2 2 2h6c1.1 0 2-.9 2-2V8H3v10zM14 2h-3l-1-1H6l-1 1H2v2h12V2z',
  pie_chart:
    'M11 2v20c-5.07-.5-9-4.79-9-10s3.93-9.5 9-10zm2.03 0v8.99H22c-.47-4.74-4.24-8.52-8.97-8.99zm0 11.01V22c4.74-.47 8.5-4.25 8.97-8.99h-8.97z',
  sync:
    'M12 4V1L8 5l4 4V6c3.31 0 6 2.69 6 6 0 1.01-.25 1.97-.7 2.8l1.46 1.46C19.54 15.03 20 13.57 20 12c0-4.42-3.58-8-8-8zm0 14c-3.31 0-6-2.69-6-6 0-1.01.25-1.97.7-2.8L5.24 7.74C4.46 8.97 4 10.43 4 12c0 4.42 3.58 8 8 8v3l4-4-4-4v3z',
  backup:
    'M19.36 10.04C18.67 6.59 15.64 4 12 4 9.11 4 6.6 5.64 5.35 8.04 2.34 8.36 0 10.91 0 14c0 3.31 2.69 6 6 6h13c2.76 0 5-2.24 5-5 0-2.64-2.05-4.78-4.64-4.96zM14 13v4h-4v-4H7l5-5 5 5h-3z',
  delete: 'M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z',
  bolt:
    'M11 21h-1l1-7H7.5c-.58 0-.57-.32-.38-.66.19-.34.05-.08.07-.12C8.48 10.94 10.42 7.54 13 3h1l-1 7h3.5c.49 0 .56.33.47.51l-.07.15C12.96 17.55 11 21 11 21z',
  storage: 'M2 20h20v-4H2v4zm2-3h2v2H4v-2zM2 4v4h20V4H2zm4 3H4V5h2v2zm-4 7h20v-4H2v4zm2-3h2v2H4v-2z',
  info:
    'M11 17h2v-6h-2v6zm1-15C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8zm-1-11h2V7h-2v2z',
  card_membership:
    'M20 2H4c-1.11 0-2 .89-2 2v11c0 1.11.89 2 2 2h4v5l4-2 4 2v-5h4c1.11 0 2-.89 2-2V4c0-1.11-.89-2-2-2zm0 13H4v-2h16v2zm0-5H4V4h16v6z',
  // T-459 监控组新页（服务状态/系统日志）两枚。article = Material「文章」
  // 标准几何（Apache-2.0）；pulse 无现成 Material 单 path 可逐字对照——
  // 本仓自绘的方波心电折线（轴对齐厚描边，同 16px 槽/monocolor 纪律），
  // 非复刻任何第三方资产。
  article:
    'M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-5 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z',
  pulse:
    'M2 12 H5 V5 H7 V17 H9 V12 H12 V9 H14 V12 H22 V14 H14 V11 H12 V14 H9 V19 H7 V7 H5 V14 H2 Z',
  // T-514 Release Bundles 应用域条目：两层抽屉堆叠（自绘轴对齐厚描边
  // 同款纪律——无 Material 单 path 逐字对照，非复刻第三方资产）。
  bundle: 'M3 3h18v7H3V3zm7 3h4v1.5h-4zM3 14h18v7H3v-7zm7 3h4v1.5h-4z',
  // T-512 Builds 应用域条目：Material「build」扳手标准几何（Apache-2.0，
  // 逐字对照 material-icons build）——CI 构建语义。
  build:
    'M22.7 19l-9.1-9.1c.9-2.3.4-5-1.5-6.9-2-2-5-2.4-7.4-1.3L9 6 6 9 1.6 4.7C.4 7.1.9 10.1 2.9 12.1c1.9 1.9 4.6 2.4 6.9 1.5l9.1 9.1c.4.4 1 .4 1.4 0l2.3-2.3c.5-.4.5-1.1.1-1.4z',
}

/**
 * 侧栏一级条目图标。装饰位（aria-hidden——恒与条目可见文字同现）；
 * 16px 槽（parity N2）；marginRight 由消费位控制（侧栏条目间距档）。
 */
export function NavIcon({ name, size = 16 }: { name: NavIconName; size?: number }) {
  return (
    <svg
      className="nav-icon"
      data-testid="nav-icon"
      data-icon={name}
      aria-hidden="true"
      focusable="false"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      style={{ flexShrink: 0, marginRight: 'var(--bf-sp-2)' }}
    >
      <path d={PATHS[name]} />
    </svg>
  )
}
