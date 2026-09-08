// 侧栏 IA 模型（frontend-rewrite-architecture §4：四分组 + 权限可见性）
// ——页面路由全部保持现 URL（IA 重组=侧栏分组重组非路由重组）。
// P1 骨架：组结构与可见位就位，条目路由 P2 随路由表落。
import type { LucideIcon } from 'lucide-react'
import {
  Boxes,
  FileSearch,
  FolderTree,
  Gauge,
  ShieldCheck,
  SlidersHorizontal,
  Activity,
} from 'lucide-react'

/** 条目可见位（权限门控——服务端是唯一守门，这里只驱动呈现） */
export type NavVisibility = 'all' | 'admin-sight' | 'admin-write'

export interface NavItem {
  id: string
  /** 目标路由（保持现 URL 形态） */
  to: string
  icon: LucideIcon
  visibility: NavVisibility
  /** 计数徽章注入位（如回收站/复制运行中） */
  badge?: 'trash' | 'replication'
}

export interface NavGroup {
  id: string
  label: string
  icon: LucideIcon
  items: NavItem[]
}

/** 四分组骨架（§4：Core / Operations / Security / Administration） */
export const NAV_GROUPS: NavGroup[] = [
  {
    id: 'core',
    label: 'Core',
    icon: Gauge,
    items: [
      { id: 'dashboard', to: '/dashboard', icon: Gauge, visibility: 'all' },
      { id: 'artifacts', to: '/artifacts', icon: FolderTree, visibility: 'all' },
      { id: 'repositories', to: '/admin/repositories/local', icon: Boxes, visibility: 'admin-sight' },
      { id: 'search', to: '/search', icon: FileSearch, visibility: 'all' },
    ],
  },
  {
    id: 'operations',
    label: 'Operations',
    icon: Activity,
    items: [
      { id: 'builds', to: '/builds', icon: Activity, visibility: 'all' },
      // bundles / replication / webhooks / trash：P2 随路由表补条目
    ],
  },
  {
    id: 'security',
    label: 'Security',
    icon: ShieldCheck,
    items: [
      // users / groups / tokens / permissions / auth / audit：P2 补
    ],
  },
  {
    id: 'administration',
    label: 'Administration',
    icon: SlidersHorizontal,
    items: [
      // governance / monitoring / settings / system：P2 补
    ],
  },
]
