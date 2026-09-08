// query key 工厂（frontend-rewrite-architecture §5：按域 + cursor 建模
// ——audit/outbox 两处 keyset 游标进 key，其余全量数组零参数）。
// P1 为骨架占位：键形即契约，P2+ features/* hooks 按此消费，改名视同
// 缓存语义变更。

export const qk = {
  /** 会话探活（silent401 面） */
  session: () => ['session'] as const,
  version: () => ['version'] as const,
  health: () => ['health'] as const,
  storageStats: () => ['storage', 'stats'] as const,
  /** 仓库清单（?type= 过滤进 key——不同 Tab 各自缓存页） */
  repositories: (type?: string) => ['repo', type ?? 'all'] as const,
  repository: (key: string) => ['repo', key] as const,
  /** 制品树（repo, path）逐节点懒展开 */
  tree: (repo: string, path: string) => ['tree', repo, path] as const,
  node: (repo: string, path: string) => ['node', repo, path] as const,
  /** 审计 keyset 游标链（filters + cursor 进 key——页窗映射） */
  audit: (filters: unknown, cursor: string) => ['audit', filters, cursor] as const,
  /** webhook 投递死信（keyset 同形） */
  outbox: (filters: unknown, cursor: string) => ['outbox', filters, cursor] as const,
  users: () => ['users'] as const,
  user: (name: string) => ['users', name] as const,
  groups: () => ['groups'] as const,
  group: (name: string) => ['groups', name] as const,
  permissions: (filter?: string) => ['permissions', filter ?? 'all'] as const,
  schedules: (domain?: string) => ['schedules', domain ?? 'all'] as const,
} as const
