// lib/api 出口：三入口一信封 client + roots（域端点函数在 P2+ 按
// features/* 逐域落位，不回填本层）。
//
// 命名阴影注记：旧 lib/api.ts 在 MUI 终验前服役，bundler 解析序里
// '../lib/api' 优先命中旧**文件**而非本目录 index——新代码一律显式
// 'lib/api/client' / 'lib/api/roots' 子模块路径导入；旧文件退役后本
// 出口恢复短路径角色。
export { ApiError, apiJSON, apiText, errText, eventJSON, rawRequest, setUnauthorizedListener } from './client'
export type { RequestOptions } from './client'
export { API_ROOT, EVENT_ROOT, contentUrl } from './roots'
