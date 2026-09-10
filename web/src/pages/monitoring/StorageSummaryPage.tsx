// 存储概要（console-m8 §6.18 / FR-73——P3 新栈重写）：
// - 刷新行（「数据最近刷新于 <ts>」+ [刷新]）+ 汇总卡行 + 仓库表（TOTAL
//   首行）——reverse §3.11 骨架三件套。
// - 数据 = GET /api/v1/storage/stats + GET /api/repositories + 逐仓
//   GET /api/v1/storage/usage/{key}（串行 + 进度提示；virtual 不请求）。
// - 列收窄（契约冻结）：Files/Folders/Items 计数列不在契约——不渲染；
//   配额列是 BinFlow 自有增强。
// - 四态：loading 骨架 / 空实例引导 / stats 403 → 汇总卡 L3 隐藏 /
//   失败错误卡 + 重试。
// 锚族原样：storage-page/storage-refreshed-at/storage-refresh/
// storage-summary/storage-table/storage-total-row/storage-row-<key>。
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { Button, ButtonAsChild } from '@/components/ui/button'
import { Badge } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { getRepositories, getStorageStats } from '@/lib/api'
import { dedupRatio, formatBytes, formatCount } from '@/lib/format'
import { getRepoUsage } from '@/lib/repos'
import type { RepoUsage } from '@/lib/repos'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'

const tt = tr('monitoring')

/** 逐仓用量拉取状态（串行；进度供刷新行提示） */
interface UsagePhase {
  status: 'loading' | 'ok'
  done: number
  total: number
  map: Record<string, RepoUsage>
  failed: string[]
}

const USAGE_IDLE: UsagePhase = { status: 'loading', done: 0, total: 0, map: {}, failed: [] }

/** 逐仓串行拉取 usage（仓库数大时串行 + 进度提示；单仓失败不阻断） */
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

  // 刷新时间戳：最慢一腿（逐仓 usage 串行链）落定时刻
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
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{tt('存储')}</h2>
        <span className="text-aux text-2">{tt('实例存储汇总与仓库维度用量')}</span>
      </div>

      {/* 刷新行（reverse §3.11：last refreshed on + Refresh） */}
      <div className="storage-refresh-row flex flex-wrap items-center justify-between gap-2">
        <span className="text-2" data-testid="storage-refreshed-at">
          {tt('数据最近刷新于：')}
          <span className="font-mono" lang="en">
            {fetchedAt ? fetchedAt.toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC') : '—'}
          </span>
        </span>
        <Button
          variant="outline"
          size="sm"
          onClick={doRefresh}
          data-testid="storage-refresh"
          disabled={usage.status === 'loading'}
          title={usage.status === 'loading' ? tt('正在拉取仓库用量') : tt('重新拉取汇总与逐仓用量')}
        >
          {usage.status === 'loading' ? tt('刷新中…') : tt('刷新')}
        </Button>
      </div>

      {repos.status === 'loading' && <StateSkeleton lines={8} />}
      {repos.status === 'error' && repos.error && <ErrorCard error={repos.error} onRetry={doRefresh} />}
      {repos.status === 'forbidden' && repos.error && (
        <EmptyState
          message={tt('无权限查看存储概要')}
          hint={tt('存储统计、仓库列表与用量端点为管理员视图（GET /api/v1/storage/stats、GET /api/repositories 仅 admin / readonly_admin）。')}
        />
      )}

      {repos.status === 'ok' && (
        <>
          {/* 汇总卡（stats 口径；403 → L3 卡片隐藏——§3.2） */}
          {stats.status === 'ok' && stats.data && (
            <section className="card section" data-testid="storage-summary">
              <div className="kv">
                <span className="k">{tt('blob 计数')}</span>
                <span className="font-mono" lang="en">{formatCount(stats.data.blobs)}</span>
              </div>
              <div className="kv">
                <span className="k">{tt('逻辑容量')}</span>
                <span className="font-mono" lang="en">{formatBytes(stats.data.logical_bytes)}</span>
              </div>
              <div className="kv">
                <span className="k">{tt('物理占用')}</span>
                <span className="font-mono" lang="en">{formatBytes(stats.data.physical_bytes)}</span>
              </div>
              <div className="kv">
                <span className="k">{tt('去重率（优化率）')}</span>
                <span className="font-mono" lang="en">
                  {(dedupRatio(stats.data.logical_bytes, stats.data.physical_bytes) * 100).toFixed(0)}%
                </span>
              </div>
            </section>
          )}
          {stats.status === 'loading' && (
            <section className="card section" data-testid="storage-summary">
              <StateSkeleton lines={4} />
            </section>
          )}
          {stats.status === 'error' && stats.error && (
            <section className="card section" data-testid="storage-summary">
              <h3>{tt('汇总')}</h3>
              <ErrorCard error={stats.error} onRetry={doRefresh} />
            </section>
          )}

          {usage.status === 'loading' && usage.total > 0 && (
            <p className="field-hint">
              {tt('正在拉取仓库用量（')}<span className="font-mono" lang="en">{usage.done}/{usage.total}</span>{tt('，串行）…')}
            </p>
          )}

          {list.length === 0 ? (
            <EmptyState
              message={tt('还没有仓库')}
              hint={tt('存储概要按仓库聚合用量——创建第一个仓库并上传制品后，这里会呈现汇总与逐仓明细。')}
              action={
                <ButtonAsChild size="sm">
                  <Link to="/admin/repositories/new">{tt('创建第一个仓库')}</Link>
                </ButtonAsChild>
              }
            />
          ) : (
            measured.length > 0 && (
              <table className="w-full text-dense" data-testid="storage-table">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-3 py-2 font-medium">{tt('仓库')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('仓型')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('包类型')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('占比')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('制品大小')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{tt('配额')}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr className="storage-total border-b border-border font-medium hover:bg-accent" data-testid="storage-total-row">
                    <td className="px-3 py-1.5"><b>TOTAL</b></td>
                    <td className="px-3 py-1.5"><span className="text-muted-foreground">—</span></td>
                    <td className="px-3 py-1.5"><span className="text-muted-foreground">—</span></td>
                    <td className="px-3 py-1.5 font-mono" lang="en">100%</td>
                    <td className="px-3 py-1.5 font-mono" lang="en">{formatBytes(totalUsed)}</td>
                    <td className="px-3 py-1.5 font-mono" lang="en">{quotaSum > 0 ? formatBytes(quotaSum) : '—'}</td>
                  </tr>
                  {list.map((r) => {
                    const u = usage.map[r.key]
                    const virtual = r.type === 'virtual'
                    const pct = !virtual && u && totalUsed > 0 ? (u.usedBytes / totalUsed) * 100 : null
                    return (
                      <tr key={r.key} data-testid={`storage-row-${r.key}`} className="border-b border-border/60 hover:bg-accent">
                        <td className="px-3 py-1.5">
                          <Link className="row-link font-mono text-primary hover:underline" to={repoLink(r.key)} lang="en">
                            {r.key}
                          </Link>{' '}
                          <CopyButton value={r.key} label={tt('仓库 key {v1}', { v1: r.key })} />
                        </td>
                        <td className="px-3 py-1.5">
                          <Badge mono lang="en">{r.type}</Badge>
                        </td>
                        <td className="px-3 py-1.5" lang="en">{r.packageType}</td>
                        <td className="px-3 py-1.5 font-mono" lang="en">
                          {pct !== null ? `${pct.toFixed(0)}%` : <span className="text-muted-foreground">—</span>}
                        </td>
                        <td className="px-3 py-1.5 font-mono" lang="en">
                          {virtual ? (
                            <span className="text-muted-foreground" title={tt('聚合视图，无自身内容')}>—</span>
                          ) : u ? (
                            formatBytes(u.usedBytes)
                          ) : (
                            <span
                              className="text-muted-foreground"
                              title={usage.failed.includes(r.key) ? tt('用量拉取失败') : tt('用量加载中')}
                            >
                              —
                            </span>
                          )}
                        </td>
                        <td className="px-3 py-1.5 font-mono" lang="en">
                          {virtual ? (
                            <span className="text-muted-foreground">—</span>
                          ) : u ? (
                            u.quotaBytes > 0 ? formatBytes(u.quotaBytes) : tt('不限')
                          ) : (
                            <span className="text-muted-foreground">—</span>
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
              {tt('部分数据')}
              {usage.failed.length > 0 && (
                <>{tt('：')}<span className="font-mono" lang="en">{usage.failed.length}</span> {tt('个仓库用量拉取失败（行内 —）')}</>
              )}
              {measured.length > 50 && <>{tt('：仓库数较多（')}{measured.length}{tt('），用量串行拉取，以上为当前快照')}</>}
            </p>
          )}
          <p className="field-hint mt-3">
            {tt('表内合计来自逐仓')} <span className="font-mono" lang="en">repo_usage</span> {tt('计量（与节点写入同事务）； 汇总卡来自实例 blob 面（stats）。文件 / 目录 / 条目计数列不在 REST 契约 （usage 仅 usedBytes / quotaBytes），不渲染、不伪造。')}
          </p>
        </>
      )}
    </div>
  )
}
