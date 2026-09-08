// 应用常量（frontend-rewrite-architecture §2 app/config：常量/权限位表）
// ——与现役旧应用字面量逐字对齐（localStorage 键/轮询节奏/分页档），
// 跨旧新共享的持久化语义零迁移成本。
//
// 挂载契约真值 #2：UI_BASE 必须与 vite base '/binflow/ui/' 同步
// （真值 #1 在 vite.config.ts；ADR-0014/PRD FR-23，双真值手工同步纪律）。

/** SPA 挂载段（BrowserRouter basename 用——不带尾斜杠） */
export const UI_BASE = '/binflow/ui'

/** localStorage 键族（与现役键逐字一致） */
export const STORAGE_KEYS = {
  theme: 'binflow-console-theme',
  locale: 'binflow-console-locale',
  recentSearches: 'binflow-console-recent-searches',
} as const

/** 轮询节奏（复刻现役三 setInterval：replication 10s / migration 5s / logs 7s） */
export const POLL_MS = {
  replication: 10_000,
  migration: 5_000,
  logs: 7_000,
} as const

/** 分页档位（统一 Pager 家族语义；默认 100=家族页窗档） */
export const PAGE_SIZE_OPTIONS = [20, 50, 100, 200, 1000] as const
export const DEFAULT_PAGE_SIZE = 100
