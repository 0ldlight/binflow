// API 三入口常量（frontend-rewrite-architecture §5）：管理面 REST、
// webhook 事件面、内容面——同源不同前缀，共用同一信封（client.ts）。
// 与旧 lib/api.ts 的 API_ROOT 及 lib/webhooks.ts 的独立根等价（P2+ 迁移
// 消费面后旧文件随 MUI 终验退役）。

/** 管理面 REST 根（ADR-0014 决策 3：前端消费通用管理面） */
export const API_ROOT = '/binflow/api'

/** webhook 事件面根（官方 Event 命名空间挂前缀——不在 API_ROOT 下） */
export const EVENT_ROOT = '/binflow/event/api/v1'

/** 内容面 URL 构造：/binflow/{repo}/{path}（下载/上传/mkdir 直连面）。
 * 段逐段 encodeURIComponent（与旧 api.ts storagePropsPath 同文法——
 * 内容路径按字面拼写直达，无隐式 redirect 可依赖）。 */
export function contentUrl(repoKey: string, path: string): string {
  const rel = path
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
  return `/binflow/${encodeURIComponent(repoKey)}${rel ? `/${rel}` : ''}`
}
