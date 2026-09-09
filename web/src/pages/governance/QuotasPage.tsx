// 配额页（console-ux §4.11 / §5.3——P3 新栈重写）：
// - 每仓一行：key（mono 链接）/ 类型 / 已用 / 配额 / 水位条（≥80% 黄、
//   ≥100% 红——water-bar 样式与仓库详情页同源）/ 行内编辑 + 跳转仓库设置。
// - usage 逐仓独立拉取（virtual 无自身内容不请求，显示 —）。
// - 行内编辑**仅 local 仓**；保存走 POST 全量替换语义——buildLocalQuotaBody
//   共享件（lib/repos）。
// - 非 admin：仓库列表 403 → 单张无权限卡（§3.6.3 L2）。
// 锚族原样：quotas-page/quotas-readonly-note/quota-row-<key>/quota-bar-<key>/
// quota-input-<key>/quota-save-<key>/quota-edit-<key>。
import { useState } from 'react'
import { Link } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { Badge } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { TextInput } from '@/components/layout/fields'
import { toast } from '@/lib/toast'
import { getRepositories, isReadOnlyAdmin } from '@/lib/api'
import type { RepoListItem } from '@/lib/api'
import { errText } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { buildLocalQuotaBody, cfgNum, getRepoDetail, getRepoUsage, updateRepo } from '@/lib/repos'
import type { RepoUsage } from '@/lib/repos'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'

const tt = tr('governance')

function WaterBar({ usage }: { usage: RepoUsage }) {
  const quota = usage.quotaBytes
  if (quota <= 0) {
    return (
      <span className="text-aux text-2">{tt('不限（quotaBytes 0）')}</span>
    )
  }
  const pct = Math.min(100, (usage.usedBytes / quota) * 100)
  const cls = usage.usedBytes >= quota ? 'full' : pct >= 80 ? 'warn' : ''
  return (
    <div className="quota-bar" data-testid={`quota-bar-${usage.repo}`}>
      {/* 新栈水位条（water-bar + .fill——governance.css 既有 token 消费面；
          ≥80% warn、≥100% full，governance spec 的 toHaveClass 钩子保持） */}
      <div
        className={`water-bar${cls ? ` ${cls}` : ''}`}
        role="progressbar"
        aria-label={tt('{v1} 配额水位', { v1: usage.repo })}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(pct)}
      >
        <div className="fill" style={{ width: `${Math.max(usage.usedBytes > 0 ? 2 : 0, Math.round(pct))}%` }} />
      </div>
      <span className={`pct${cls ? ` ${cls}` : ''}`}>
        {pct.toFixed(0)}%{usage.usedBytes >= quota ? tt(' 满') : pct >= 80 ? tt(' 高') : ''}
      </span>
    </div>
  )
}

function QuotaRow({ repo, onChanged }: { repo: RepoListItem; onChanged: () => void }) {
  // readonly_admin：配额写是 repoManage write——编辑入口禁用（服务端 403 兜底）
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  // virtual 不持有自身内容：不请求 usage（不伪造 0）
  const usage = useAsync(
    () => (repo.type === 'virtual' ? Promise.resolve(null) : getRepoUsage(repo.key)),
    [repo.key, repo.type],
  )

  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [editErr, setEditErr] = useState<string | null>(null)

  const local = repo.type === 'local'
  const u = usage.data

  const startEdit = () => {
    setDraft(String(u?.quotaBytes ?? cfgNum(repo.configuration, 'quotaBytes') ?? 0))
    setEditErr(null)
    setEditing(true)
  }

  const save = async (): Promise<void> => {
    const v = draft.trim()
    if (!/^\d+$/.test(v) || Number(v) > Number.MAX_SAFE_INTEGER) {
      setEditErr(tt('需为非负整数（字节）；0 = 不限'))
      return
    }
    setSaving(true)
    setEditErr(null)
    try {
      // 全量替换语义：先取详情再重组完整 body（只覆写 quotaBytes）
      const detail = await getRepoDetail(repo.key)
      const text = await updateRepo(repo.key, buildLocalQuotaBody(detail, Number(v)))
      toast.success(text)
      setEditing(false)
      usage.reload()
      onChanged()
    } catch (err) {
      setEditErr(errText(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <tr data-testid={`quota-row-${repo.key}`} className="border-b border-border/60 hover:bg-accent">
      <td className="px-3 py-1.5">
        <Link className="row-link font-mono text-primary hover:underline" to={`/admin/repositories/${repo.key}`} lang="en">
          {repo.key}
        </Link>{' '}
        <CopyButton value={repo.key} label={tt('仓库 key {v1}', { v1: repo.key })} />
      </td>
      <td className="px-3 py-1.5">
        <Badge mono lang="en">{repo.type}</Badge>
      </td>
      <td className="px-3 py-1.5">
        {repo.type === 'virtual' ? (
          <span className="text-muted-foreground">{tt('—（聚合视图，无自身内容）')}</span>
        ) : usage.status === 'loading' ? (
          <span className="inline-block h-2 w-12 animate-pulse rounded-sm bg-surface-2" role="progressbar" aria-label={tt('用量加载中')} />
        ) : usage.status === 'ok' && u ? (
          <span className="font-mono">{formatBytes(u.usedBytes)}</span>
        ) : (
          <span className="text-muted-foreground" title={usage.error?.message ?? tt('用量不可用')}>—</span>
        )}
      </td>
      <td className="px-3 py-1.5">
        {editing ? (
          <>
            <TextInput
              mono
              lang="en"
              inputMode="numeric"
              autoComplete="off"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              className="w-[140px]"
              aria-label={tt('{v1} 的新配额（字节）', { v1: repo.key })}
              data-testid={`quota-input-${repo.key}`}
            />{' '}
            <span className="text-aux text-muted-foreground">
              {/^\d+$/.test(draft.trim()) && Number(draft) > 0 ? `≈ ${formatBytes(Number(draft))}` : tt('0 = 不限')}
            </span>
          </>
        ) : local ? (
          <span className="font-mono">{u ? (u.quotaBytes > 0 ? formatBytes(u.quotaBytes) : tt('0（不限）')) : '—'}</span>
        ) : (
          <span className="text-muted-foreground">{tt('—（仅 local 仓支持）')}</span>
        )}
      </td>
      <td className="quota-bar-cell px-3 py-1.5">
        {usage.status === 'ok' && u ? <WaterBar usage={u} /> : <span className="text-muted-foreground">—</span>}
      </td>
      <td className="whitespace-nowrap px-3 py-1.5">
        {editing ? (
          <>
            <Button variant="outline" size="sm" className="h-7" disabled={saving} onClick={() => void save()} data-testid={`quota-save-${repo.key}`}>
              {saving ? tt('保存中…') : tt('保存')}
            </Button>{' '}
            <Button variant="outline" size="sm" className="h-7" disabled={saving} onClick={() => setEditing(false)}>
              {tt('取消')}
            </Button>
            {editErr && (
              <span className="field-error" role="alert">{editErr}</span>
            )}
          </>
        ) : (
          <>
            {local && usage.status === 'ok' && u && (
              <Button
                variant="outline"
                size="sm"
                className="h-7"
                disabled={readOnly}
                title={readOnly ? tt('只读管理员：配额写是管理面写操作（服务端 403 兜底）') : undefined}
                onClick={startEdit}
                data-testid={`quota-edit-${repo.key}`}
              >
                {tt('编辑上限')}
              </Button>
            )}{' '}
            <Link className="text-aux text-2 hover:underline" to={`/admin/repositories/${repo.key}/edit`}>{tt('仓库设置 →')}</Link>
          </>
        )}
      </td>
    </tr>
  )
}

export default function QuotasPage() {
  // L2 收敛走 repos 403（整页主数据面）
  const repos = useAsync(getRepositories, [])
  const list = repos.data ?? []
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)

  return (
    <div data-testid="quotas-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{tt('配额')}</h2>
        <span className="text-aux text-2">{tt('水位 ≥80% 黄 · ≥100% 红（此后写入 413）')}</span>
      </div>
      {readOnly && (
        <p className="admin-note" data-testid="quotas-readonly-note">
          {tt('只读管理员（readonly_admin）：配额读写面可见，行内编辑已禁用—— 配额写是管理面写操作（repoManage write），提交会被服务端 403 拒绝。')}
        </p>
      )}

      {repos.status === 'loading' && <StateSkeleton lines={8} />}
      {repos.status === 'error' && repos.error && <ErrorCard error={repos.error} onRetry={repos.reload} />}
      {repos.status === 'forbidden' && repos.error && (
        <EmptyState
          message={tt('无权限查看配额')}
          hint={tt('仓库列表与用量端点为管理员视图（GET /api/repositories 仅 admin）。')}
        />
      )}
      {repos.status === 'ok' &&
        (list.length === 0 ? (
          <EmptyState
            message={tt('还没有仓库')}
            hint={tt('配额在创建 local 仓库时或仓库设置页配置（quotaBytes，0 = 不限）')}
            action={
              <ButtonAsChild size="sm">
                <Link to="/admin/repositories/new">{tt('创建第一个仓库')}</Link>
              </ButtonAsChild>
            }
          />
        ) : (
          <table className="w-full text-dense">
            <thead>
              <tr className="border-b border-border text-left text-aux text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">{tt('仓库')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{tt('类型')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{tt('已用')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{tt('配额')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{tt('水位')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{tt('操作')}</th>
              </tr>
            </thead>
            <tbody>
              {list.map((r) => (
                <QuotaRow key={r.key} repo={r} onChanged={repos.reload} />
              ))}
            </tbody>
          </table>
        ))}
      <p className="field-hint mt-3">{tt('计量为 repo_usage.logical_bytes（与节点写入同事务）；quotaBytes 仅 local 仓生效， 超限写入原子拒绝（413 + quota.exceeded 审计）。')}</p>
    </div>
  )
}
