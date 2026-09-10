// Explorer 域共享模型（页面与子面板间的纯类型/常量——页面薄组装的缝）。
import type { ChildNode } from '@/pages/artifacts/lib'

/** 右键菜单目标（repo / 节点） */
export type MenuTarget =
  | { kind: 'repo'; repoKey: string }
  | { kind: 'node'; repoKey: string; node: ChildNode }

/** Sort-by 选项（K67 冻结候选：名称 / 包类型 / 仓库类型，均升序、name 稳定末键） */
export type TreeSort = 'name' | 'pkg' | 'rclass'

/** 详情页签 ID（URL 段 slug 映射——TAB ∈ {general|properties|permissions}） */
export type DetailTab = 'general' | 'props' | 'perms'

export const TAB_SLUG: Record<DetailTab, string> = { general: 'general', props: 'properties', perms: 'permissions' }
export const SLUG_TO_TAB: Record<string, DetailTab> = {
  general: 'general',
  properties: 'props',
  permissions: 'perms',
}

export interface DownloadState {
  path: string
  phase: 'loading' | 'done'
  sha: string
  /** undefined = 无对账源（根目录层无 list 合并值） */
  match: boolean | undefined
}

/** BIG_DIR 警示阈值（children 契约暂无游标——客户端虚拟滚动 + 引导搜索） */
export const BIG_DIR = 2000
