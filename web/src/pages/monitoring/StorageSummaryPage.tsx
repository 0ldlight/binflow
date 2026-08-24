import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { getRepositories, getStorageStats } from '../../lib/api'
import { dedupRatio, formatBytes, formatCount } from '../../lib/format'
import { getRepoUsage } from '../../lib/repos'
import type { RepoUsage } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'

// 存储概要（console-m8 §6.18 / FR-73 治理面，T-238——/admin/monitoring/storage
// 落真身；Artifactory Storage Summary 形态对齐，数据面 = BinFlow 现役端点）：
//
// - 刷新行（「数据最近刷新于 <ts>」+ [刷新]）+ 汇总卡行 + 仓库表（TOTAL
//   首行）——reverse §3.11 骨架三件套。
// - 数据 = GET /api/v1/storage/stats（system:read）+ GET /api/repositories
//   （repo:read）+ 逐仓 GET /api/v1/storage/usage/{key}（CanManageRepo read）。
//   仓库数大时串行拉取 + 进度提示；virtual 仓不请求 usage（不伪造 0，
//   沿配额页约定）。
// - 列收窄（契约冻结）：Artifactory 的 Files/Folders/Items 计数列不在
//   BinFlow 契约（usage 仅 usedBytes/quotaBytes）——不渲染、不伪造；
//   配额列是 BinFlow 自有增强（per-repo quota 模型）。
// - 四态（§3.2 存储概要行）：loading = 卡片独立骨架；空实例 = 0 值 +
//   建仓引导；stats 403 → 汇总卡 L3 隐藏（readonly_admin 读面全通，普通
//   user 由 repos 403 收敛为整页 L2 无权限卡）；失败 = 错误卡 + 重试，
//   Refresh 即时重取。
// - 口径注记：汇总卡来自 stats（实例全局 blob 面）；表内合计来自逐仓
//   usage（repo_usage.logical_bytes）——两口径独立呈现，不互推。

/** 逐仓用量拉取状态（串行；进度供刷新行提示） */
interface UsagePhase {
  status: 'loading' | 'ok'
  done: number
  total: number
  map: Record<string, RepoUsage>
  failed: string[]
}

const USAGE_IDLE: UsagePhase = { status: 'loading', done: 0, total: 0, map: {}, failed: [] }

/**
 * 逐仓串行拉取 usage（console-m8 §6.18：仓库数大时串行 + 进度提示）。
 * 单仓失败不阻断（行级降级「—」）；alive 闭包丢弃晚到响应（useAsync 同款
 * 语义——repos 列表变化或刷新 tick 时旧链作废）。
 */
function useRepoUsages(repos: { key: string; type: string }[], tick: number): UsagePhase {
  const [phase, setPhase] = useState<UsagePhase>(USAGE_IDLE)
  const key = repos.map((r) => `${r.key}:${r.type}`).join('|')

  useEffect(() => {
    const list = key === '' ? [] : key.split('|').map((s) => {
      const [k, t] = s.split(':')
      return { key: k, type: t }
    })
    const targets = list.filter((r) => r.type !== 'virtual')
    if (targets.length === 0) {
      setPhase({ status: 'ok', done: 0, total: 0, map: {}, failed: [] })
      return
    }
    let alive = true
    setPhase({ status: 'loading', done: 0, total: targets.length, map: {}, failed: [] })
    void (async () => {
      const map: Record<string, RepoUsage> = {}
      const failed: string[] = []
      for (let i = 0; i < targets.length; i++) {
        if (!alive) return
        try {
          const u = await getRepoUsage(targets[i].key)
          if (!alive) return
          map[targets[i].key] = u
        } catch {
          failed.push(targets[i].key)
        }
        if (!alive) return
        setPhase({ status: 'loading', done: i + 1, total: targets.length, map: { ...map }, failed: [...failed] })
      }
      if (alive) setPhase({ status: 'ok', done: targets.length, total: targets.length, map, failed })
    })()
    return () => {
      alive = false
    }
  }, [key, tick])

  return phase
}

export default function StorageSummaryPage() {
  const [tick, setTick] = useState(0)
  const [fetchedAt, setFetchedAt] = useState<Date | null>(null)
  const stats = useAsync(getStorageStats, [tick])
  const repos = useAsync(getRepositories, [tick])
  const list = useMemo(() => repos.data ?? [], [repos.data])
  const usage = useRepoUsages(list, tick)

  // 刷新时间戳：最慢一腿（逐仓 usage 串行链）落定时刻——空实例时立即为 ok
  useEffect(() => {
    if (usage.status === 'ok') setFetchedAt(new Date())
  }, [usage.status, usage.done])

  const doRefresh = () => setTick((t) => t + 1)

  // 表合计（usage 口径；virtual 不计入）
  const measured = list.filter((r) => r.type !== 'virtual')
  const totalUsed = measured.reduce((sum, r) => sum + (usage.map[r.key]?.usedBytes ?? 0), 0)
  const quotaSum = measured.reduce((sum, r) => {
    const q = usage.map[r.key]?.quotaBytes ?? 0
    return q > 0 ? sum + q : sum
  }, 0)
  const partial = usage.failed.length > 0 || measured.length > 50

  const repoLink = (key: string) => `/admin/repositories/${encodeURIComponent(key)}`

  return (
    <div data-testid="storage-page">
      <div className="page-header">
        <h2>存储</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          实例存储汇总与仓库维度用量
        </span>
      </div>

      {/* 刷新行（reverse §3.11：last refreshed on + Refresh） */}
      <div className="storage-refresh-row">
        <span className="text-2" data-testid="storage-refreshed-at">
          数据最近刷新于：
          <span className="mono" lang="en">
            {fetchedAt ? fetchedAt.toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC') : '—'}
          </span>
        </span>
        <button
          type="button"
          className="btn"
          onClick={doRefresh}
          data-testid="storage-refresh"
          disabled={usage.status === 'loading'}
          title={usage.status === 'loading' ? '正在拉取仓库用量' : '重新拉取汇总与逐仓用量'}
        >
          {usage.status === 'loading' ? '刷新中…' : '刷新'}
        </button>
      </div>

      {repos.status === 'loading' && <Skeleton lines={8} />}
      {repos.status === 'error' && repos.error && <ErrorCard error={repos.error} onRetry={doRefresh} />}
      {repos.status === 'forbidden' && repos.error && (
        <EmptyState
          message="无权限查看存储概要"
          hint="存储统计、仓库列表与用量端点为管理员视图（GET /api/v1/storage/stats、GET /api/repositories 仅 admin / readonly_admin）。"
        />
      )}

      {repos.status === 'ok' && (
        <>
          {/* 汇总卡（stats 口径；403 → L3 卡片隐藏——§3.2） */}
          {stats.status === 'ok' && stats.data && (
            <section className="card section" data-testid="storage-summary">
              <div className="kv">
                <span className="k">blob 计数</span>
                <span className="mono" lang="en">
                  {formatCount(stats.data.blobs)}
                </span>
              </div>
              <div className="kv">
                <span className="k">逻辑容量</span>
                <span className="mono" lang="en">
                  {formatBytes(stats.data.logical_bytes)}
                </span>
              </div>
              <div className="kv">
                <span className="k">物理占用</span>
                <span className="mono" lang="en">
                  {formatBytes(stats.data.physical_bytes)}
                </span>
              </div>
              <div className="kv">
                <span className="k">去重率（优化率）</span>
                <span className="mono" lang="en">
                  {(dedupRatio(stats.data.logical_bytes, stats.data.physical_bytes) * 100).toFixed(0)}%
                </span>
              </div>
            </section>
          )}
          {stats.status === 'loading' && (
            <section className="card section" data-testid="storage-summary">
              <Skeleton lines={4} />
            </section>
          )}
          {stats.status === 'error' && stats.error && (
            <section className="card section" data-testid="storage-summary">
              <h3>汇总</h3>
              <ErrorCard error={stats.error} onRetry={doRefresh} />
            </section>
          )}

          {usage.status === 'loading' && usage.total > 0 && (
            <p className="field-hint">
              正在拉取仓库用量（<span className="mono" lang="en">{usage.done}/{usage.total}</span>，串行）…
            </p>
          )}

          {list.length === 0 ? (
            <EmptyState
              message="还没有仓库"
              hint="存储概要按仓库聚合用量——创建第一个仓库并上传制品后，这里会呈现汇总与逐仓明细。"
              action={
                <Link className="btn primary" to="/admin/repositories/new">
                  创建第一个仓库
                </Link>
              }
            />
          ) : (
            measured.length > 0 && (
              <table className="table" data-testid="storage-table">
                <thead>
                  <tr>
                    <th scope="col">仓库</th>
                    <th scope="col">仓型</th>
                    <th scope="col">包类型</th>
                    <th scope="col">占比</th>
                    <th scope="col">制品大小</th>
                    <th scope="col">配额</th>
                  </tr>
                </thead>
                <tbody>
                  <tr className="storage-total" data-testid="storage-total-row">
                    <td>
                      <b>TOTAL</b>
                    </td>
                    <td>
                      <span className="text-muted">—</span>
                    </td>
                    <td>
                      <span className="text-muted">—</span>
                    </td>
                    <td className="mono" lang="en">
                      100%
                    </td>
                    <td className="mono" lang="en">
                      {formatBytes(totalUsed)}
                    </td>
                    <td className="mono" lang="en">
                      {quotaSum > 0 ? formatBytes(quotaSum) : '—'}
                    </td>
                  </tr>
                  {list.map((r) => {
                    const u = usage.map[r.key]
                    const virtual = r.type === 'virtual'
                    const pct = !virtual && u && totalUsed > 0 ? (u.usedBytes / totalUsed) * 100 : null
                    return (
                      <tr key={r.key}>
                        <td>
                          <Link className="row-link mono" to={repoLink(r.key)} lang="en">
                            {r.key}
                          </Link>{' '}
                          <CopyButton value={r.key} label={`仓库 key ${r.key}`} />
                        </td>
                        <td>
                          <span className="badge neutral">{r.type}</span>
                        </td>
                        <td lang="en">{r.packageType}</td>
                        <td className="mono" lang="en">
                          {pct !== null ? `${pct.toFixed(0)}%` : <span className="text-muted">—</span>}
                        </td>
                        <td className="mono" lang="en">
                          {virtual ? (
                            <span className="text-muted" title="聚合视图，无自身内容">
                              —
                            </span>
                          ) : u ? (
                            formatBytes(u.usedBytes)
                          ) : (
                            <span
                              className="text-muted"
                              title={usage.failed.includes(r.key) ? '用量拉取失败' : '用量加载中'}
                            >
                              —
                            </span>
                          )}
                        </td>
                        <td className="mono" lang="en">
                          {virtual ? (
                            <span className="text-muted">—</span>
                          ) : u ? (
                            u.quotaBytes > 0 ? formatBytes(u.quotaBytes) : '不限'
                          ) : (
                            <span className="text-muted">—</span>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            )
          )}

          {partial && usage.status === 'ok' && (
            <p className="field-hint">
              部分数据
              {usage.failed.length > 0 && (
                <>
                  ：<span className="mono" lang="en">{usage.failed.length}</span> 个仓库用量拉取失败（行内 —）
                </>
              )}
              {measured.length > 50 && <>：仓库数较多（{measured.length}），用量串行拉取，以上为当前快照</>}
            </p>
          )}
          <p className="field-hint" style={{ marginTop: 12 }}>
            表内合计来自逐仓 <span className="mono" lang="en">repo_usage</span> 计量（与节点写入同事务）；
            汇总卡来自实例 blob 面（stats）。文件 / 目录 / 条目计数列不在 REST 契约
            （usage 仅 usedBytes / quotaBytes），不渲染、不伪造。
          </p>
        </>
      )}
    </div>
  )
}
