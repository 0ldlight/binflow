// 会话仓（当前用户/权限位快照——与 AuthProvider 并轨：Provider 是请求
// 与生命周期的唯一事实源，本仓只做 UI 姿态镜像（导航门控/写口禁用），
// 不发请求不缓存服务数据）。
import { create } from 'zustand'

/** RBAC 闭集角色（与 lib/api 旧文件 AdminRole 同集合；P2 收编统一） */
export type AdminRole = 'user' | 'readonly_admin' | 'admin'

export interface SessionSnapshot {
  username: string
  adminRole: AdminRole
  /** 管理面可见（admin ∪ readonly_admin）与写能力（仅 admin）的 UI 位 */
  canSeeAdmin: boolean
  canAdminWrite: boolean
}

export interface SessionState {
  snapshot: SessionSnapshot | null
  setSnapshot: (snapshot: SessionSnapshot | null) => void
}

export const useSessionStore = create<SessionState>()((set) => ({
  snapshot: null,
  setSnapshot: (snapshot) => set({ snapshot }),
}))
