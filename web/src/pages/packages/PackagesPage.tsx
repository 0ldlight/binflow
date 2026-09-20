// Packages landing (同类控制台 /packages parity slice).
//
// Honest data contract: BinFlow currently exposes the repository inventory but
// has no package aggregate endpoint for latest version, version count,
// downloads, or security status. This page groups real repositories by package
// type and records that backend gap instead of fabricating package metrics.
import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { PkgIcon } from '@/components/PkgIcon'
import { ApiError, getRepositories } from '@/lib/api'
import type { RepoListItem } from '@/lib/api'
import { qk } from '@/lib/query'
import { tr } from '@/i18n'

const t = tr('console')
const RECENT_KEY = 'binflow-console-recent-packages'
const PACKAGE_LABELS: Record<string, string> = {
  generic: 'Generic', maven: 'Maven', npm: 'npm', pypi: 'PyPI', go: 'Go',
  docker: 'Docker', helm: 'Helm', debian: 'Debian', rpm: 'RPM', nuget: 'NuGet',
  cargo: 'Cargo', conan: 'Conan',
}

function readRecent(): string[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]') as unknown
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === 'string').slice(0, 12) : []
  } catch {
    return []
  }
}

export default function PackagesPage() {
  const navigate = useNavigate()
  const [term, setTerm] = useState('')
  const [tab, setTab] = useState<'all' | 'recent'>('all')
  const [recent, setRecent] = useState<string[]>(readRecent)
  const repos = useQuery({ queryKey: qk.repositories(), queryFn: () => getRepositories(), retry: 1 })

  const groups = useMemo(() => {
    const map = new Map<string, RepoListItem[]>()
    for (const repo of repos.data ?? []) {
      const list = map.get(repo.packageType) ?? []
      list.push(repo)
      map.set(repo.packageType, list)
    }
    return [...map.entries()]
      .map(([packageType, rows]) => ({ packageType, rows: [...rows].sort((a, b) => a.key.localeCompare(b.key)) }))
      .sort((a, b) => a.packageType.localeCompare(b.packageType))
  }, [repos.data])

  const q = term.trim().toLowerCase()
  const visible = useMemo(() => {
    let rows = groups
    if (tab === 'recent') rows = groups.filter((g) => recent.includes(g.packageType))
    if (!q) return rows
    return rows.filter((g) =>
      g.packageType.toLowerCase().includes(q)
      || g.rows.some((repo) => repo.key.toLowerCase().includes(q) || repo.description.toLowerCase().includes(q)),
    )
  }, [groups, q, recent, tab])

  const remember = (packageType: string) => {
    setRecent((prev) => {
      const next = [packageType, ...prev.filter((x) => x !== packageType)].slice(0, 12)
      localStorage.setItem(RECENT_KEY, JSON.stringify(next))
      return next
    })
  }

  const countByType = (rows: RepoListItem[], type: string) => rows.filter((r) => r.type === type).length
  const emptyMessage = tab === 'recent'
    ? t('最近没有浏览过的包类型')
    : q !== ''
      ? t('没有匹配的包类型或仓库')
      : t('还没有仓库')

  return (
    <div data-testid="packages-page" className="flex flex-col gap-3">
      <div className="page-header flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-semibold">{t('Packages')}</h2>
        <span className="text-aux text-muted-foreground">{t('按包类型浏览真实仓库目录')}</span>
      </div>

      <div className="flex flex-wrap items-center gap-2 py-2">
        <div role="tablist" aria-label={t('包类型视图')} className="flex items-center gap-1">
          <Button type="button" role="tab" variant={tab === 'all' ? 'secondary' : 'ghost'} aria-selected={tab === 'all'} data-testid="packages-tab-all" onClick={() => setTab('all')}>
            {t('All Packages')}
          </Button>
          <Button type="button" role="tab" variant={tab === 'recent' ? 'secondary' : 'ghost'} aria-selected={tab === 'recent'} data-testid="packages-tab-recent" onClick={() => setTab('recent')}>
            {t('Recently Viewed')}
          </Button>
        </div>
        <Input
          type="search"
          value={term}
          onChange={(e) => setTerm(e.target.value)}
          placeholder={t('Search packages')}
          aria-label={t('Search packages')}
          className="w-[260px]"
          data-testid="packages-search"
        />
        {q !== '' && (
          <Button variant="outline" size="sm" data-testid="packages-clear" onClick={() => setTerm('')}>{t('Clear all')}</Button>
        )}
      </div>

      {repos.isPending && <StateSkeleton lines={5} />}
      {repos.error && <ErrorCard error={repos.error as ApiError} onRetry={() => void repos.refetch()} />}
      {repos.isSuccess && visible.length === 0 && (
        <EmptyState
          message={emptyMessage}
          hint={t('包类型来自真实仓库清单；创建仓库后会自动出现在这里。')}
          testid="packages-empty"
        />
      )}

      {repos.isSuccess && visible.length > 0 && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3" data-testid="packages-grid">
          {visible.map((group) => {
            const first = group.rows[0]
            return (
              <section key={group.packageType} className="card rounded-lg p-4" data-testid={`package-card-${group.packageType}`}>
                <div className="flex items-start gap-3">
                  <PkgIcon id={group.packageType} variant="brand" size={28} className="pkg-icon" />
                  <div className="min-w-0 flex-1">
                    <h3 className="truncate text-dense font-semibold" lang="en">{PACKAGE_LABELS[group.packageType] ?? group.packageType}</h3>
                    <p className="text-aux text-muted-foreground">{group.rows.length} {t('个仓库')}</p>
                  </div>
                </div>
                <div className="mt-3 flex flex-wrap gap-1.5">
                  <Badge variant="tint-neutral" mono lang="en">local {countByType(group.rows, 'local')}</Badge>
                  <Badge variant="tint-neutral" mono lang="en">remote {countByType(group.rows, 'remote')}</Badge>
                  <Badge variant="tint-neutral" mono lang="en">virtual {countByType(group.rows, 'virtual')}</Badge>
                </div>
                <p className="mt-3 break-all font-mono text-aux text-muted-foreground" lang="en" data-testid={`package-repos-${group.packageType}`}>
                  {group.rows.slice(0, 4).map((r) => r.key).join(' · ')}{group.rows.length > 4 ? ' …' : ''}
                </p>
                <div className="mt-4 flex justify-end">
                  <Button
                    size="sm"
                    data-testid={`package-open-${group.packageType}`}
                    onClick={() => {
                      remember(group.packageType)
                      navigate(`/admin/repositories/${first.type}`)
                    }}
                  >
                    {t('查看仓库')}
                  </Button>
                </div>
              </section>
            )
          })}
        </div>
      )}

      <p className="max-w-3xl text-aux text-muted-foreground" data-testid="packages-api-gap">
        {t('当前包聚合后端缺口：latest version、versions、downloads 与 security 需要专用包清单 API；本页只展示真实仓库数据，不用占位数据冒充包指标。')}
      </p>
    </div>
  )
}
