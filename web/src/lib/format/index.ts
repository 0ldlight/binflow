// lib/format 出口：展示格式化纯函数（字节/去重率/双 locale 日期/千分位）。
//
// 命名阴影注记：旧 lib/format.ts 在 MUI 终验前服役且优先于本 index——
// 新代码显式子模块路径导入（'lib/format/bytes' 等），旧文件退役后恢复。
export { dedupRatio, formatBytes } from './bytes'
export { formatAuditTime } from './datetime'
export { formatCount } from './number'
