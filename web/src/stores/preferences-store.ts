// 偏好仓（列选/行高/favorites/recent-searches——localStorage key 原样
// 迁移（architecture §6）：binflow-console-cols-* / bf-tree-* /
// binflow-console-recent-searches，跨页共享语义不变）。
// P1 骨架：态 + 动作就位；读写落盘 P2 随消费面接线（键常量先钉死）。
import { create } from 'zustand'

/** 偏好 localStorage 键族（与现役键逐字一致——零迁移成本） */
export const PREF_KEYS = {
  cols: (page: string) => `binflow-console-cols-${page}`,
  treeFavorites: 'bf-tree-favorites',
  treeCompacted: 'bf-tree-compacted',
  recentSearches: 'binflow-console-recent-searches',
} as const

export type RowHeight = 'compact' | 'default'

export interface PreferencesState {
  /** 树行高（compact=紧凑档） */
  treeRowHeight: RowHeight
  setTreeRowHeight: (height: RowHeight) => void
  /** 树收藏仓（repo key 集） */
  treeFavorites: string[]
  toggleTreeFavorite: (repoKey: string) => void
  /** 最近搜索词（8 条环形，旧序在前） */
  recentSearches: string[]
  pushRecentSearch: (term: string) => void
}

export const usePreferencesStore = create<PreferencesState>()((set) => ({
  treeRowHeight: 'default',
  setTreeRowHeight: (height) => set({ treeRowHeight: height }),
  treeFavorites: [],
  toggleTreeFavorite: (repoKey) =>
    set((s) => ({
      treeFavorites: s.treeFavorites.includes(repoKey)
        ? s.treeFavorites.filter((k) => k !== repoKey)
        : [...s.treeFavorites, repoKey],
    })),
  recentSearches: [],
  pushRecentSearch: (term) =>
    set((s) => ({
      recentSearches: [term, ...s.recentSearches.filter((t) => t !== term)].slice(0, 8),
    })),
}))
