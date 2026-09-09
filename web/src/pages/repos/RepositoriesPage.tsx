// 仓库管理列表（console-ux §6.6 / reverse §3.4——P2 新栈重写：轻量 table
// + usage 批量注水 E1 cap≤3 钉死 + inspector 对位）。
//
// 语义承接（audit §2.6 列表行）：
// - 三 Tab = 子路由（/admin/repositories/{local|remote|virtual}）；
// - 7 列：key（链接+拷贝）/ 包类型 / Replications（local+remote Tab）/
//   上游或成员（details 弹层）/ 已用（批量注水+tooltip+失败重试）/ 描述 /
//   操作（SetMeUp·Deploy·删除）；列头排序（key/pkg 三态）；key 子串过滤；
//   客户端页窗；列选器；刷新；建仓下拉三预选；
// - usage 门控 = 列表 ok 后才发（单请求注水——m9/usage-fanout cap≤3 钉死）；
// - 门：GET repositories = CapRepoRead（普通 user 403 → L2）；写入口仅
//   全量 admin（L4）。
// - 锚族原样：repos-page / repos-count / repos-create(-menu|-<rclass>) /
//   repos-tab-* / repos-filter-key / repos-columns(-menu|-item-*|-reset) /
//   repos-refresh / repos-table / repos-row-<key> / repos-sort-key /
//   repos-usage-<key> / repos-repl-<key>(-run-<key>) / repos-setmeup-<key>
//   / repos-deploy-<key> / repos-delete-<key> / repos-pager /
//   repos-empty-filtered / repos-readonly-note。
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'

import { Button, ButtonAsChild } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { useAuth } from '@/app/AuthContext'
import { toast } from '@/lib/toast'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { Pager, useClientPager } from '@/components/layout/pager'
import { LegacyDialogHost } from '@/components/layout/legacy-host'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin } from '@/lib/api'
import type { RepoListItem } from '@/lib/api'
import { useColumnPrefs } from '@/lib/columnPrefs'
import type { ColumnDef } from '@/lib/columnPrefs'
import { cfgStr, cfgStrList, getRepositoriesFiltered, getUsageBatch } from '@/lib/repos'
import type { RepoUsageRow, RClass } from '@/lib/repos'
import { listReplicationConfigs, runReplicationNow } from '@/lib/replications'
import type { ReplicationConfig } from '@/lib/replications'
import { formatBytes, formatCount } from '@/lib/format'
import { onTableRowKeys } from '@/lib/keys'
import { useAsync } from '@/lib/useAsync'
import { PkgIcon } from '@/components/PkgIcon'
import { useRepoDelete } from '@/pages/repositories/RepoDeleteConfirm'
import { REPO_CREATE_ENTRY } from '@/pages/repositories/formCopy'
import { tr } from '@/i18n'
// 仓库管理域样式（pages/repositories 支撑模块族共享——旧页面退役后由新页直挂）
import '@/pages/repositories/repositories.css'
import { lazy } from 'react'

const tt = tr('repositories')

// 旧全局对话框（MUI 树——终验强删项）
const SetMeUpDialog = lazy(() => import('@/components/SetMeUpDialog'))
const DeployDialog = lazy(() => import('@/components/DeployDialog'))

const TYPE_LABEL: Record<string, string> = { local: 'Local', remote: 'Remote', virtual: 'Virtual' }
const PKG_LABEL: Record<string, string> = {
  generic: 'Generic',
  docker: 'Docker',
  maven: 'Maven',
  npm: 'npm',
  pypi: 'PyPI',
}

const TABS: { id: RClass; label: string }[] = [
  { id: 'local', label: 'Local' },
  { id: 'remote', label: 'Remote' },
  { id: 'virtual', label: 'Virtual' },
]

const COLUMNS: ColumnDef[] = [
  { id: 'key', label: 'Repository Key', anchor: 'repos-columns-item-key' },
  { id: 'package', label: tt('包类型'), anchor: 'repos-columns-item-package' },
  { id: 'replications', label: 'Replications', anchor: 'repos-columns-item-replications' },
  { id: 'upstream', label: tt('上游 / 成员'), anchor: 'repos-columns-item-upstream' },
  { id: 'usage', label: tt('已用'), anchor: 'repos-columns-item-usage' },
  { id: 'description', label: tt('描述'), anchor: 'repos-columns-item-description' },
  { id: 'actions', label: tt('操作'), anchor: 'repos-columns-item-actions' },
]
const COLUMN_IDS = COLUMNS.map((c) => c.id)
const COLS_KEY = 'binflow-console-cols-repos'

function tabFromPath(pathname: string): RClass {
  const seg = pathname.split('/').filter(Boolean).pop() ?? ''
  return seg === 'remote' || seg === 'virtual' ? (seg as RClass) : 'local'
}

function truncate(s: string, max = 36): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s
}

/** usage 批量注水（E1：整页恰一次 GET usage?include=counts——cap≤3 钉死） */
function useUsageBatch(enabled: boolean) {
  const [tick, setTick] = useState(0)
  const [state, setState] = useState<{ status: 'loading' | 'ok' | 'error'; index: Map<string, RepoUsageRow> | null; error: ApiError | null }>({
    status: 'loading',
    index: null,
    error: null,
  })
  useEffect(() => {
    if (!enabled) return
    let alive = true
    setState({ status: 'loading', index: null, error: null })
    getUsageBatch(true)
      .then((rows) => {
        if (alive) setState({ status: 'ok', index: new Map(rows.map((r) => [r.repo, r])), error: null })
      })
      .catch((err: unknown) => {
        if (!alive) return
        setState({ status: 'error', index: null, error: err instanceof ApiError ? err : new ApiError(0, String(err)) })
      })
    return () => {
      alive = false
    }
  }, [enabled, tick])
  const reload = useCallback(() => setTick((t) => t + 1), [])
  return { ...state, reload }
}

function UsageCell({ repoKey, rclass, usage }: { repoKey: string; rclass: string; usage: ReturnType<typeof useUsageBatch> }) {
  const testid = `repos-usage-${repoKey}`
  if (rclass === 'virtual' || usage.status === 'loading') {
    return (
      <span className="text-muted-foreground" data-testid={testid}>—</span>
    )
  }
  if (usage.status === 'error') {
    return (
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground"
        data-testid={testid}
        title={tt('{v1}（点击重试）', { v1: usage.error?.message ?? tt('用量不可用') })}
        aria-label={tt('仓库 {repoKey} 用量加载失败，点击重试', { repoKey })}
        onClick={(e) => {
          e.stopPropagation()
          usage.reload()
        }}
      >
        —
      </button>
    )
  }
  const row = usage.index?.get(repoKey)
  if (!row) {
    return (
      <span className="text-muted-foreground" data-testid={testid}>—</span>
    )
  }
  return (
    <span className="font-mono" data-testid={testid} title={row.nodeCount !== undefined ? tt('{v1} 个文件', { v1: formatCount(row.nodeCount) }) : undefined}>
      {formatBytes(row.usedBytes)}
    </span>
  )
}

/** 列头排序钮（§4.7 三态循环——模块级组件，静态声明） */
function SortTh({
  label,
  k,
  sortKey,
  sortDir,
  onToggle,
  testid,
}: {
  label: string
  k: 'key' | 'package'
  sortKey: 'key' | 'package' | null
  sortDir: 'asc' | 'desc'
  onToggle: (k: 'key' | 'package') => void
  testid?: string
}) {
  return (
    <th
      scope="col"
      aria-sort={sortKey === k ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}
      data-testid={testid}
      className="cursor-pointer whitespace-nowrap px-3 py-2 text-left text-aux font-medium text-muted-foreground hover:text-foreground"
      onClick={() => onToggle(k)}
    >
      {label}
      <span aria-hidden="true" className={`ml-1 ${sortKey === k ? '' : 'opacity-30'}`}>
        {sortKey === k && sortDir === 'desc' ? '↓' : '↑'}
      </span>
    </th>
  )
}

function UpstreamCell({ repo }: { repo: RepoListItem }) {
  if (repo.type === 'remote') {
    const url = cfgStr(repo.configuration, 'url')
    if (!url) return <span className="text-muted-foreground">—</span>
    return (
      <span className="font-mono" title={url} lang="en">{truncate(url)}</span>
    )
  }
  if (repo.type === 'virtual') {
    const members = cfgStrList(repo.configuration, 'repositories')
    if (members.length === 0) return <span className="text-muted-foreground">—</span>
    return (
      <details className="member-pop" onClick={(e) => e.stopPropagation()}>
        <summary className="cursor-pointer">{members.length} {tt('成员')}</summary>
        <div className="pop relative z-10 mt-1 rounded-md border border-border bg-popover p-2 text-dense shadow-flat">
          <ol className="font-mono">
            {members.map((m) => (
              <li key={m} lang="en">{m}</li>
            ))}
          </ol>
          <div className="mt-1 text-[11px] text-muted-foreground">{tt('按声明序（优先解析成员在前由其自身配置标记）')}</div>
        </div>
      </details>
    )
  }
  return <span className="text-muted-foreground">—</span>
}

export default function RepositoriesPage() {
  const { session } = useAuth()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const tab = tabFromPath(pathname)

  const [createOpen, setCreateOpen] = useState(false)
  const [keyQuery, setKeyQuery] = useState('')
  const state = useAsync(() => getRepositoriesFiltered(tab, ''), [tab])
  const reload = state.reload
  const usage = useUsageBatch(state.status === 'ok')
  const repls = useAsync(
    () =>
      (tab === 'local' || tab === 'remote') && state.status === 'ok'
        ? listReplicationConfigs()
        : Promise.resolve(null),
    [tab, state.status],
  )
  const replIndex = useMemo(() => {
    const m = new Map<string, ReplicationConfig[]>()
    for (const c of repls.data ?? []) {
      const cur = m.get(c.source_repo)
      if (cur) cur.push(c)
      else m.set(c.source_repo, [c])
    }
    return m
  }, [repls.data])

  const cols = useColumnPrefs(COLUMN_IDS, COLS_KEY)
  const [colsOpen, setColsOpen] = useState(false)

  const requestDelete = useRepoDelete({ onDeleted: reload })

  const [smuKey, setSmuKey] = useState<string | null>(null)
  const [deployKey, setDeployKey] = useState<string | null>(null)

  const runReplications = async (repoKey: string, enabled: ReplicationConfig[]) => {
    if (enabled.length === 0) return
    let total = 0
    try {
      for (const c of enabled) {
        const res = await runReplicationNow(c.id)
        total += res.scheduled
      }
    } catch (err) {
      toast.error(tt('复制触发失败（{repoKey}）：{v1}', { repoKey, v1: errText(err) }))
      return
    }
    const names = enabled.map((c) => c.name).join(tt('、'))
    toast.success(
      total > 0
        ? tt('已触发 {repoKey} 的全量同步：排程 {v1} 项任务（{names}）', { repoKey, v1: formatCount(total), names })
        : tt('已触发 {repoKey} 的全量同步：源仓当前无制品，本次为空跑（{names}）', { repoKey, names }),
      {
        action: { label: tt('查看任务'), onClick: () => navigate('/admin/governance/replication') },
      },
    )
  }

  const [sortKey, setSortKey] = useState<'key' | 'package' | null>(null)
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')
  const toggleSort = (k: 'key' | 'package') => {
    if (sortKey !== k) {
      setSortKey(k)
      setSortDir('asc')
    } else if (sortDir === 'asc') {
      setSortDir('desc')
    } else {
      setSortKey(null)
      setSortDir('asc')
    }
  }

  const q = keyQuery.trim().toLowerCase()
  const rows = (state.data ?? []).filter((r) => (q ? r.key.toLowerCase().includes(q) : true))
  const sorted = [...rows].sort((a, b) => {
    if (!sortKey) return 0
    const va = sortKey === 'key' ? a.key : a.packageType
    const vb = sortKey === 'key' ? b.key : b.packageType
    if (va === vb) return 0
    return ((va < vb ? -1 : 1) * (sortDir === 'asc' ? 1 : -1)) as number
  })
  const pageEpoch = `${state.status}|${sorted.length}|${q}|${sortKey ?? ''}|${sortDir}`
  const pager = useClientPager(sorted.length, pageEpoch)
  const pageRows = pager.slice(sorted)

  return (
    <div data-testid="repos-page" className="flex flex-col gap-3">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{tt('仓库')}</h2>
        <div className="repos-head-actions ml-auto flex items-center gap-2">
          <span className="repos-head-count text-aux text-muted-foreground" data-testid="repos-count">
            {state.status === 'ok' ? tt('{v1} 个仓库', { v1: rows.length }) : '…'}
          </span>
          {admin && (
            <Popover open={createOpen} onOpenChange={setCreateOpen}>
              <PopoverTrigger asChild>
                <Button size="sm" aria-haspopup="menu" aria-expanded={createOpen} title={tt('新建仓库——选择仓型后进入对应建仓表单')} data-testid="repos-create">
                  {tt('＋ 新建仓库')}
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-64 p-1" align="end" data-testid="repos-create-menu">
                {TABS.map((t) => (
                  <button
                    key={t.id}
                    type="button"
                    data-testid={`repos-create-${t.id}`}
                    className="block w-full rounded-sm px-2 py-2 text-left text-dense hover:bg-accent"
                    onClick={() => {
                      setCreateOpen(false)
                      navigate(`/admin/repositories/${t.id}/new`)
                    }}
                  >
                    <b>{REPO_CREATE_ENTRY[t.id].label}</b>
                    <br />
                    <span className="text-aux text-muted-foreground">{REPO_CREATE_ENTRY[t.id].desc}</span>
                  </button>
                ))}
              </PopoverContent>
            </Popover>
          )}
        </div>
      </div>

      {readOnly && (
        <p className="page-note rounded-md border border-border bg-surface-2 px-3 py-2 text-dense text-muted-foreground" data-testid="repos-readonly-note">
          {tt('ⓘ 只读管理员（readonly_admin）视角：仓库清单与配置只读；创建/删除仓库与浏览器部署（Deploy）等写操作已禁用——服务端一律 403 兜底。')}
        </p>
      )}

      {/* Tab 条（子路由形态——Link 承载） */}
      <div className="flex gap-1 border-b border-border">
        {TABS.map((t) => (
          <Link
            key={t.id}
            to={`/admin/repositories/${t.id}`}
            aria-current={tab === t.id ? 'page' : undefined}
            data-testid={`repos-tab-${t.id}`}
            className={`-mb-px rounded-t-sm border-b-2 px-3 py-1.5 text-dense ${tab === t.id ? 'border-primary font-medium' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
          >
            {t.label}
          </Link>
        ))}
      </div>

      <div className="filter-bar flex flex-wrap items-center gap-2">
        <Input
          type="search"
          placeholder={tt('搜索 {v1} 仓 key…', { v1: TYPE_LABEL[tab] })}
          value={keyQuery}
          onChange={(e) => setKeyQuery(e.target.value)}
          className="w-[260px] font-mono"
          data-testid="repos-filter-key"
          aria-label={tt('搜索仓库 key')}
        />
        <span className="filter-tail-actions filter-tail-end ml-auto flex items-center gap-1.5">
          <Popover open={colsOpen} onOpenChange={setColsOpen}>
            <PopoverTrigger asChild>
              <Button variant="outline" size="sm" aria-haspopup="menu" aria-expanded={colsOpen} data-testid="repos-columns" title={tt('自定义显示列（偏好保存在本浏览器）')}>
                <span aria-hidden="true">▤</span> {tt('列')} {cols.visibleCount}/{COLUMNS.length}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-56 p-1" align="end" role="menu" data-testid="repos-columns-menu">
              {COLUMNS.map((c) => {
                const visible = cols.isVisible(c.id)
                const last = visible && cols.visibleCount === 1
                return (
                  <button
                    key={c.id}
                    type="button"
                    role="menuitemcheckbox"
                    aria-checked={visible}
                    aria-disabled={last || undefined}
                    title={last ? tt('至少保留一列') : undefined}
                    data-testid={c.anchor}
                    className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
                    onClick={() => {
                      if (!last) cols.toggle(c.id)
                    }}
                  >
                    <span aria-hidden="true" className="col-check">{visible ? '☑' : '☐'}</span>
                    {c.label}
                  </button>
                )
              })}
              <div role="separator" className="my-1 border-t border-border" />
              <button
                type="button"
role="menuitem"
                aria-disabled={cols.visibleCount === COLUMNS.length || undefined}
                title={cols.visibleCount === COLUMNS.length ? tt('全部列已在场') : tt('显示全部列')}
                data-testid="repos-columns-reset"
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-dense hover:bg-accent aria-disabled:opacity-50"
                onClick={() => cols.reset()}
              >
                {tt('全选列')}
              </button>
            </PopoverContent>
          </Popover>
          <Button
            variant="outline"
            size="icon"
            className="size-8"
            aria-label={tt('刷新仓库列表')}
            data-testid="repos-refresh"
            disabled={state.status === 'loading'}
            onClick={reload}
            title={tt('重新拉取仓库清单与用量')}
          >
            {state.status === 'loading' ? (
              <span role="progressbar" aria-label={tt('重新拉取仓库清单与用量')} className="inline-block size-4 animate-spin rounded-full border-2 border-border border-t-primary" />
            ) : (
              <span aria-hidden="true">↻</span>
            )}
          </Button>
        </span>
      </div>

      {state.status === 'loading' && <StateSkeleton lines={8} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'forbidden' && state.error && (
        <EmptyState
          message={tt('无权限查看仓库列表')}
          hint={tt('仓库清单是管理面读端点（admin 与只读管理员可见）。制品访问请使用制品浏览、搜索或仓库直链。')}
        />
      )}
      {state.status === 'ok' &&
        (rows.length === 0 ? (
          q !== '' ? (
            <EmptyState
              illustration
              message={tt('无匹配的仓库（「{keyQuery}」）', { keyQuery })}
              action={
                <Button variant="outline" size="sm" onClick={() => setKeyQuery('')}>
                  {tt('清除过滤')}
                </Button>
              }
              testid="repos-empty-filtered"
            />
          ) : admin ? (
            <EmptyState
              illustration
              message={tt('还没有 {v1} 仓库', { v1: TYPE_LABEL[tab] })}
              action={
                <ButtonAsChild size="sm">
                  <Link to={`/admin/repositories/${tab}/new`}>{tt('创建第一个')}{TYPE_LABEL[tab]}{tt('仓库')}</Link>
                </ButtonAsChild>
              }
              hint={
                tab === 'local'
                  ? tt('建议从 generic 起步（适配任意文件；协议仓按客户端接入文档选型）')
                  : tab === 'remote'
                    ? tt('Remote 仓代理上游（如 repo1.maven.org），制品按需缓存')
                    : tt('Virtual 仓聚合多个 local/remote 成员，统一团队出口')
              }
            />
          ) : (
            <EmptyState illustration message={tt('还没有 {v1} 仓库', { v1: TYPE_LABEL[tab] })} hint={tt('仓库由管理员创建')} />
          )
        ) : (
          <>
            <table className="w-full text-dense" data-testid="repos-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  {cols.isVisible('key') && <SortTh label="Repository Key" k="key" sortKey={sortKey} sortDir={sortDir} onToggle={toggleSort} testid="repos-sort-key" />}
                  {cols.isVisible('package') && <SortTh label={tt('包类型')} k="package" sortKey={sortKey} sortDir={sortDir} onToggle={toggleSort} />}
                  {(tab === 'local' || tab === 'remote') && cols.isVisible('replications') && (
                    <th
                      scope="col"
                      className="whitespace-nowrap px-3 py-2 font-medium"
                      title={tab === 'remote' ? tt('push 复制配置（以该仓为源）——BinFlow 无 pull 复制（remote 缓存是另一能力域，ADR-0021/parity R10）') : tt('push 复制配置（以该仓为源）')}
                    >
                      Replications
                    </th>
                  )}
                  {cols.isVisible('upstream') && <th scope="col" className="px-3 py-2 font-medium">{tt('上游 / 成员')}</th>}
                  {cols.isVisible('usage') && <th scope="col" className="px-3 py-2 font-medium">{tt('已用')}</th>}
                  {cols.isVisible('description') && <th scope="col" className="px-3 py-2 font-medium">{tt('描述')}</th>}
                  {cols.isVisible('actions') && <th scope="col" className="px-3 py-2 font-medium">{tt('操作')}</th>}
                </tr>
              </thead>
              <tbody>
                {pageRows.map((repo) => (
                  <tr
                    key={repo.key}
                    data-testid={`repos-row-${repo.key}`}
                    className="cursor-pointer border-b border-border/60 hover:bg-accent"
                    tabIndex={0}
                    onClick={() => navigate(`/admin/repositories/${repo.key}`)}
                    onKeyDown={(e) => onTableRowKeys(e, () => navigate(`/admin/repositories/${repo.key}`))}
                  >
                    {cols.isVisible('key') && (
                      <td className="px-3 py-1.5">
                        <Link
                          className="row-link font-mono text-primary hover:underline"
                          to={`/admin/repositories/${repo.key}`}
                          onClick={(e) => e.stopPropagation()}
                          lang="en"
                        >
                          {repo.key}
                        </Link>{' '}
                        <span onClick={(e) => e.stopPropagation()}>
                          <CopyButton value={repo.key} label={tt('仓库 key {v1}', { v1: repo.key })} />
                        </span>
                      </td>
                    )}
                    {cols.isVisible('package') && (
                      <td className="px-3 py-1.5">
                        <span className="badge neutral inline-flex items-center gap-1 rounded-sm bg-secondary px-1.5 py-0.5 text-[11px]">
                          <PkgIcon id={repo.packageType} variant="mono" size={13} />
                          {PKG_LABEL[repo.packageType] ?? repo.packageType}
                        </span>
                      </td>
                    )}
                    {(tab === 'local' || tab === 'remote') && cols.isVisible('replications') && (
                      <td className="px-3 py-1.5">
                        <ReplicationsCell
                          repoKey={repo.key}
                          configs={repls.data ? (replIndex.get(repo.key) ?? []) : undefined}
                          state={repls.status}
                          error={repls.error?.message}
                          canRun={admin}
                          onRun={(enabled) => void runReplications(repo.key, enabled)}
                        />
                      </td>
                    )}
                    {cols.isVisible('upstream') && (
                      <td className="px-3 py-1.5"><UpstreamCell repo={repo} /></td>
                    )}
                    {cols.isVisible('usage') && (
                      <td className="px-3 py-1.5"><UsageCell repoKey={repo.key} rclass={repo.type} usage={usage} /></td>
                    )}
                    {cols.isVisible('description') && (
                      <td className="max-w-[260px] break-words px-3 py-1.5 text-muted-foreground">{repo.description || '—'}</td>
                    )}
                    {cols.isVisible('actions') && (
                      <td className="px-3 py-1.5" onClick={(e) => e.stopPropagation()}>
                        <Button variant="outline" size="sm" className="h-7" data-testid={`repos-setmeup-${repo.key}`} title={tt('Set Me Up：{v1} 的客户端接入向导', { v1: repo.key })} onClick={() => setSmuKey(repo.key)}>
                          Set Me Up
                        </Button>{' '}
                        {repo.type === 'local' && (repo.packageType === 'generic' || repo.packageType === 'maven') && (
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7"
                            data-testid={`repos-deploy-${repo.key}`}
                            disabled={readOnly}
                            title={readOnly ? tt('只读管理员不可写（服务端 403 兜底）') : tt('部署到 {v1}（浏览器上传）', { v1: repo.key })}
                            onClick={() => setDeployKey(repo.key)}
                          >
                            {tt('部署')}
                          </Button>
                        )}{' '}
                        {admin && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 bg-transparent text-muted-foreground"
                            data-testid={`repos-delete-${repo.key}`}
                            aria-label={tt('删除仓库 {v1}', { v1: repo.key })}
                            title={tt('删除仓库 {v1}', { v1: repo.key })}
                            onClick={(e) => {
                              e.stopPropagation()
                              requestDelete({ key: repo.key, rclass: repo.type, packageType: repo.packageType })
                            }}
                          >
                            {tt('删除')}
                          </Button>
                        )}
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="table-foot" data-testid="repos-pager">
              <Pager
                page={pager.page}
                pageCount={pager.pageCount}
                onPageChange={pager.setPage}
                from={pager.from}
                to={pager.to}
                total={sorted.length}
                note={q !== '' ? tt('（按「{keyQuery}」过滤）', { keyQuery }) : undefined}
                pageSize={pager.size}
                onPageSizeChange={pager.setSize}
              />
            </div>
          </>
        ))}

      {smuKey && (
        <LegacyDialogHost>
          <SetMeUpDialog preselectedRepo={smuKey} onClose={() => setSmuKey(null)} />
        </LegacyDialogHost>
      )}
      {deployKey && (
        <LegacyDialogHost>
          <DeployDialog preselectedRepo={deployKey} onClose={() => setDeployKey(null)} onUploaded={reload} />
        </LegacyDialogHost>
      )}
    </div>
  )
}

/** Replications 列（T-404/T-420：Run 动作 = 逐启用配置全量对账） */
function ReplicationsCell({
  repoKey,
  configs,
  state,
  error,
  canRun,
  onRun,
}: {
  repoKey: string
  configs: ReplicationConfig[] | undefined
  state: string
  error?: string
  canRun: boolean
  onRun: (enabled: ReplicationConfig[]) => void
}) {
  const testid = `repos-repl-${repoKey}`
  if (state !== 'ok' || !configs) {
    return (
      <span
        className="text-muted-foreground"
        data-testid={testid}
        title={state === 'error' ? tt('复制配置不可用（{v1}）', { v1: error ?? tt('加载失败') }) : state === 'forbidden' ? tt('复制配置为管理面（system:read）') : undefined}
      >
        —
      </span>
    )
  }
  if (configs.length === 0) {
    return (
      <span data-testid={testid} title={tt('未配置复制（No Replication Configured）')}>0</span>
    )
  }
  const enabledConfigs = configs.filter((c) => c.enabled)
  const tip =
    (enabledConfigs.length > 0
      ? tt('Run Replication——对本仓 {v1} 条启用配置各触发一次全量同步（异步执行，任务状态见全局复制页）；', { v1: enabledConfigs.length })
      : tt('已配置 {v1} 条复制（全部停用）——启用后才能触发；', { v1: configs.length })) +
    tt('共 {v1} 条（{v2} 启用）', { v1: configs.length, v2: enabledConfigs.length }) +
    (canRun ? '' : tt('；只读管理员不可触发'))
  return (
    <span title={tip} onClick={(e) => e.stopPropagation()}>
      <button
        type="button"
        className="grid size-7 place-items-center rounded-sm hover:bg-accent disabled:opacity-40"
        aria-label={tt('复制 {repoKey}：{v1} 条配置（{v2} 启用）', { repoKey, v1: configs.length, v2: enabledConfigs.length })}
        data-testid={`repos-repl-run-${repoKey}`}
        disabled={!canRun || enabledConfigs.length === 0}
        onClick={(e) => {
          e.stopPropagation()
          onRun(enabledConfigs)
        }}
      >
        <span aria-hidden="true">▶</span>
      </button>
    </span>
  )
}
