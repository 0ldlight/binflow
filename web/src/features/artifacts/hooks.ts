// 制品树域数据 hooks（frontend-rewrite-architecture §5 四段式）：
// lib/api（旧信封——过渡期单例语义：401 监听/ApiError 全局唯一）
//   → lib/query key 工厂 → 本 hooks → Explorer 组件。
//
// 树装载语义（懒单层 + T-494 分类门）：useQueries 驱动「想要的目录集」
// （选中仓根 + 装载链 ∪ 手动展开集）——键含 docker 标志（仓级 ?docker_tags
// 富化翻转 = 键变 = 重取，等价旧缓存清场）。旧 lib 函数无 AbortSignal
// 面（全量小响应，超时面交由 Query 的 signal 中断在 fetch 层）。
import { useMemo } from 'react'
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query'

import { ApiError, getRepositories, getStorageStats } from '@/lib/api'
import { getRepoDetail } from '@/lib/repos'
import { listFolder } from '@/pages/artifacts/lib'
import type { ChildNode, FolderListing } from '@/pages/artifacts/lib'
import { qk } from '@/lib/query'

export type DirStatus =
  | { status: 'loading' }
  | { status: 'ok'; nodes: ChildNode[]; remoteDegraded?: string }
  | { status: 'forbidden'; error: ApiError }
  | { status: 'error'; error: ApiError }

/** 仓库清单（CapRepoRead——admin/readonly 全量；普通 user 403） */
export function useRepositoriesQuery() {
  const query = useQuery({
    queryKey: qk.repositories(),
    queryFn: () => getRepositories(),
    // 清单在会话内稳定（写操作后显式 invalidate）
    staleTime: Infinity,
    retry: 1,
  })
  const forbidden = query.error instanceof ApiError && query.error.status === 403
  return { ...query, forbidden }
}

/** 选中仓元数据（403 → generic 降级语义——isForbidden 态由消费面判） */
export function useRepoMetaQuery(repoKey: string) {
  const query = useQuery({
    queryKey: qk.repository(repoKey),
    queryFn: () => getRepoDetail(repoKey),
    enabled: repoKey !== '',
    staleTime: Infinity,
    retry: 1,
  })
  const forbidden = query.error instanceof ApiError && query.error.status === 403
  return { ...query, forbidden }
}

/** 存储统计（admin 门——页脚标语行） */
export function useStorageStatsQuery(enabled: boolean) {
  return useQuery({
    queryKey: qk.storageStats(),
    queryFn: () => getStorageStats(),
    enabled,
    staleTime: 60_000,
  })
}

export interface DirEntry {
  repo: string
  dir: string
}

function dirQueryKey(repo: string, dir: string, isDocker: boolean) {
  return ['tree', repo, dir, isDocker ? 'docker' : 'plain'] as const
}

/**
 * 树目录装载（懒单层）：wanted = 想要装载的 (repo, dir) 集——由
 * Explorer 从「选中仓根 + 装载链 ∪ 手动展开集」推导（T-494 分类门在
 * 消费面：文件段/未决段不入集）。
 */
export function useDirQueries(wanted: DirEntry[], isDockerRepo: boolean) {
  // 稳定序列化（JSON——dir='' 与多目录集无歧义；join('\n') 会在 dir 空
  // 段时产生可错切的段边界）
  const wantedKey = useMemo(() => JSON.stringify([...wanted].sort((a, b) => (a.repo + '\u0000' + a.dir < b.repo + '\u0000' + b.dir ? -1 : 1))), [wanted])
  const entries = useMemo(() => JSON.parse(wantedKey) as DirEntry[], [wantedKey])
  const queries = useQueries({
    queries: entries.map((e) => ({
      queryKey: dirQueryKey(e.repo, e.dir, isDockerRepo),
      queryFn: () => listFolder(e.repo, e.dir, undefined, isDockerRepo && e.dir !== ''),
      // 跨仓根挂载期（repoKey='' 的空段）不发孤儿请求
      enabled: e.repo !== '',
      staleTime: Infinity,
      retry: 0,
      gcTime: 10 * 60_000,
    })),
  })
  const state = new Map<string, DirStatus>()
  entries.forEach((e, i) => {
    const q = queries[i]
    const key = `${e.repo}\n${e.dir}`
    if (!q || q.isPending) {
      state.set(key, { status: 'loading' })
    } else if (q.isError) {
      const err = q.error instanceof ApiError ? q.error : new ApiError(0, String(q.error))
      state.set(key, err.status === 403 ? { status: 'forbidden', error: err } : { status: 'error', error: err })
    } else {
      const data = q.data as FolderListing
      state.set(key, { status: 'ok', nodes: data.nodes, remoteDegraded: data.remoteDegraded || undefined })
    }
  })
  return state
}

/** 树缓存失效（全量刷新语义：docker 标志翻转 / 手动刷新） */
export function useTreeCacheClear() {
  const client = useQueryClient()
  return () => {
    void client.removeQueries({ queryKey: ['tree'] })
  }
}

/** 仓库清单失效（删除/上传/建目录后的列表重取） */
export function useRepoListInvalidate() {
  const client = useQueryClient()
  return () => {
    void client.invalidateQueries({ queryKey: qk.repositories() })
  }
}
