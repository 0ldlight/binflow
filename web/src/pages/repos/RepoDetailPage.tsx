// 仓库详情（console-ux §6.8——P2 新栈重写：三 Tab → 八 Tab 骨架，总令 §十
// 形态：Overview / Artifacts / Configuration / Storage / Permissions /
// Replication / Webhooks / Activity）。
//
// 内容映射（旧行为保全——audit §2.7 详情行）：
// - Overview：接入命令块（commands.ts 同源）+ 统计水位条（≥80% warn /
//   ≥100% error）+ remote 上游 / virtual 成员特化卡 + 危险区删仓；
// - Artifacts：直系 children 概要（listFolder 单层）+ 浏览深链；
// - Configuration：QuotaEditor 行内编辑（全量替换保全——保存前重取详情
//   重组完整 body）+ 字段只读 + 编辑器深链；
// - Storage：usage 单仓面（used/quota/files）；
// - Permissions：?permissions 有效权限视图（admin 门）；
// - Replication：本仓配置摘要 + ?section=replications 深链 + 全局页指针；
// - Webhooks：指针（事件订阅管理页——按仓过滤面归后续票）；
// - Activity：本仓最近审计（?repo= 过滤，8 条）。
// - 锚族原样：repo-detail-page / repo-setmeup / repo-deploy / repo-edit-link
//   / repo-tab-* / repo-commands / repo-usage-card / repo-remote-card /
//   repo-virtual-card / repo-danger-zone / repo-delete-button /
//   repo-governance-card / repo-quota-input / repo-quota-save /
//   repo-repl-card(-na|-goto|-edit-link|-table|-row-*) /
//   repo-manage-note / repo-detail-readonly-note。
import { lazy, useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { Button, ButtonAsChild } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAuth } from '@/app/AuthContext'
import { toast } from 'sonner'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { LegacyDialogHost } from '@/app/router/legacy-bridge'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin, normalizeAdminRole } from '@/lib/api'
import type { AuditEvent } from '@/lib/api'
import { getAuditEventsPage } from '@/lib/governance'
import { formatAuditTime, formatBytes, formatCount } from '@/lib/format'
import {
  buildLocalQuotaBody,
  cfgBool,
  cfgNum,
  cfgStr,
  cfgStrList,
  getRepoDetail,
  getRepoUsage,
  updateRepo,
} from '@/lib/repos'
import type { PackageType, RClass, RepoDetail, RepoUsage } from '@/lib/repos'
import { configsForRepo, listReplicationConfigs } from '@/lib/replications'
import type { ReplicationConfig } from '@/lib/replications'
import { useAsync } from '@/lib/useAsync'
import { listFolder } from '@/pages/artifacts/lib'
import { clientCommands } from '@/pages/repositories/commands'
import { useRepoDelete } from '@/pages/repositories/RepoDeleteConfirm'
import { getItemPermissions } from '@/pages/artifacts/lib'
import { tr } from '@/i18n'

const t = tr('repositories')

// 旧全局对话框（MUI——终验强删项）
const SetMeUpDialog = lazy(() => import('@/components/SetMeUpDialog'))
const DeployDialog = lazy(() => import('@/components/DeployDialog'))

/** 八 Tab（总令 §十） */
type DetailTab = 'overview' | 'artifacts' | 'configuration' | 'storage' | 'permissions' | 'replication' | 'webhooks' | 'activity'

const TABS: [DetailTab, string][] = [
  ['overview', t('概要')],
  ['artifacts', t('制品')],
  ['configuration', t('配置')],
  ['storage', t('存储')],
  ['permissions', t('权限')],
  ['replication', 'Replication'],
  ['webhooks', 'Webhooks'],
  ['activity', t('活动')],
]

function QuotaLine({ usage }: { usage: RepoUsage }) {
  const quota = usage.quotaBytes
  const used = usage.usedBytes
  if (quota <= 0) {
    return (
      <div className="water-line text-dense text-muted-foreground">
        {t('已用')} <span className="font-mono">{formatBytes(used)}</span> {t('· 配额不限（0）')}
      </div>
    )
  }
  const pct = quota > 0 ? Math.min(100, (used / quota) * 100) : 0
  const cls = used >= quota ? 'full' : pct >= 80 ? 'warn' : ''
  return (
    <div>
      <div
        className={`water-bar h-2 w-full max-w-md overflow-hidden rounded-sm bg-surface-2 ${cls === 'full' ? 'bg-destructive/20' : ''}`}
        role="progressbar"
        aria-valuenow={Math.round(pct)}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={t('{v1}配额水位', { v1: usage.repo ?? '' })}
      >
        <div
          className={`h-full ${cls === 'full' ? 'bg-destructive' : cls === 'warn' ? 'bg-warning' : 'bg-primary'}`}
          style={{ width: `${Math.max(used > 0 ? 2 : 0, Math.round(pct))}%` }}
        />
      </div>
      <div className="water-line mt-1.5 text-dense text-muted-foreground">
        <span className="font-mono">{formatBytes(used)}</span> / <span className="font-mono">{formatBytes(quota)}</span>
        {t('（')}{pct.toFixed(1)}{t('%）')}{used >= quota ? t(' · 已满（写入将 413）') : pct >= 80 ? t(' · 接近上限') : ''}
      </div>
    </div>
  )
}

/** 配置 Tab 的行内配额编辑（CanManageRepo：admin / m-holder 可写） */
function QuotaEditor({ repo, disabled, onSaved }: { repo: RepoDetail; disabled: boolean; onSaved: () => void }) {
  const [draft, setDraft] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const current = String(cfgNum(repo.configuration, 'quotaBytes') ?? 0)
  const value = draft ?? current
  const bad = !/^\d+$/.test(value.trim())

  const save = async () => {
    setError(null)
    setSaving(true)
    try {
      // 全量替换语义：保存瞬间重取详情再重组完整 body（只覆写 quotaBytes）
      const fresh = await getRepoDetail(repo.key)
      const text = await updateRepo(repo.key, buildLocalQuotaBody(fresh, Number(value.trim())))
      toast.success(text)
      setDraft(null)
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : errText(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <div className="kv mb-1 flex gap-2 text-dense">
        <span className="k w-32 shrink-0 text-muted-foreground">quotaBytes</span>
        <span>
          <span className="font-mono">{current}</span>
          {cfgNum(repo.configuration, 'quotaBytes') ? '' : t('（不限）')}
        </span>
      </div>
      <div className="quota-edit flex items-center gap-2">
        <Input
          inputMode="numeric"
          autoComplete="off"
          value={value}
          disabled={disabled}
          aria-invalid={bad}
          onChange={(e) => setDraft(e.target.value)}
          className="w-40 font-mono"
          aria-label={t('配额 quotaBytes（字节）')}
          data-testid="repo-quota-input"
          lang="en"
        />
        <Button variant="outline" size="sm" disabled={disabled || saving || bad || value.trim() === current} onClick={() => void save()} data-testid="repo-quota-save">
          {saving ? t('保存中…') : t('保存配额')}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={disabled || draft === null}
          onClick={() => {
            setDraft(null)
            setError(null)
          }}
        >
          {t('取消')}
        </Button>
      </div>
      {error && (
        <p className="field-error mt-1 text-aux text-destructive" role="alert">{error}</p>
      )}
      <p className="field-hint mt-1 text-aux text-muted-foreground">{t('正整数；0 = 不限。超限写入收到 413（message 含 used/quota）。')}</p>
    </div>
  )
}

export default function RepoDetailPage() {
  const { key: routeKey } = useParams<{ key: string }>()
  const { session } = useAuth()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  const role = normalizeAdminRole(session?.adminRole, session?.admin ?? false)
  const mHolder = role === 'user'

  const [tab, setTab] = useState<DetailTab>('overview')
  useEffect(() => setTab('overview'), [routeKey])

  const [smuOpen, setSmuOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)

  const state = useAsync(() => (routeKey ? getRepoDetail(routeKey) : Promise.resolve(null)), [routeKey])
  const usage = useAsync(
    () =>
      state.data && state.data.rclass !== 'virtual' && routeKey
        ? getRepoUsage(routeKey)
        : Promise.resolve(null),
    [state.status, state.data?.rclass, routeKey],
  )
  const repl = useAsync(
    () =>
      state.data && state.data.rclass === 'local'
        ? listReplicationConfigs()
        : Promise.resolve([] as ReplicationConfig[]),
    [state.status, state.data?.rclass],
  )
  const replConfigs = useMemo(() => configsForRepo(repl.data ?? [], routeKey ?? ''), [repl.data, routeKey])
  const requestDelete = useRepoDelete({})

  if (state.status === 'loading') {
    return <div data-testid="repo-detail-page"><StateSkeleton lines={8} /></div>
  }
  if (state.status === 'forbidden' && state.error) {
    return (
      <div data-testid="repo-detail-page">
        <EmptyState
          message={t('无权限查看仓库')}
          hint={
            mHolder
              ? t('单仓管理视图需要该仓的 manage 动作（permission target 授权）；{v1}', { v1: state.error.message })
              : t('仓库管理面为管理员视图（{v1}）', { v1: state.error.message })
          }
          action={<ButtonAsChild variant="outline" size="sm"><Link to="/artifacts">{t('← 前往制品浏览')}</Link></ButtonAsChild>}
        />
      </div>
    )
  }
  if (state.status === 'error' && state.error) {
    return (
      <div data-testid="repo-detail-page">
        {state.error.status === 404 ? (
          <EmptyState
            message={t('仓库 {routeKey} 不存在', { routeKey })}
            hint={t('key 可能打错，或仓库已被删除')}
            action={<ButtonAsChild variant="outline" size="sm"><Link to="/admin/repositories/local">{t('← 返回仓库列表')}</Link></ButtonAsChild>}
          />
        ) : (
          <ErrorCard error={state.error} onRetry={state.reload} />
        )}
      </div>
    )
  }
  if (!state.data) return null
  const repo = state.data
  const cfg = repo.configuration
  const rclass = repo.rclass as RClass
  const packageType = repo.packageType as PackageType
  const commands = clientCommands(packageType, repo.key)
  const canEditConfig = !readOnly
  const canDelete = admin

  const kv = (k: string, v: React.ReactNode) => (
    <div className="kv mb-1 flex gap-2 text-dense">
      <span className="k w-36 shrink-0 text-muted-foreground">{k}</span>
      <span className="min-w-0 break-all">{v}</span>
    </div>
  )

  return (
    <div data-testid="repo-detail-page" className="flex flex-col gap-3">
      <p className="m-0">
        <Link to={`/admin/repositories/${rclass}`} className="text-primary hover:underline">{t('← 仓库')}</Link>
      </p>
      <div className="detail-head flex flex-wrap items-center gap-2">
        <span className="key text-lg font-semibold" lang="en">{repo.key}</span>
        <CopyButton value={repo.key} label={t('仓库 key {v1}', { v1: repo.key })} />
        <span className="badge neutral rounded-sm bg-secondary px-1.5 py-0.5 text-[11px]">{repo.rclass}</span>
        <span className="badge neutral rounded-sm bg-secondary px-1.5 py-0.5 text-[11px]">{repo.packageType}</span>
        <div className="detail-head-actions ml-auto flex flex-wrap items-center gap-1.5">
          <Button variant="outline" size="sm" data-testid="repo-setmeup" title={t('Set Me Up：客户端接入向导')} onClick={() => setSmuOpen(true)}>
            Set Me Up
          </Button>
          {rclass === 'local' && (packageType === 'generic' || packageType === 'maven') && (
            <Button variant="outline" size="sm" data-testid="repo-deploy" disabled={readOnly} title={readOnly ? t('只读管理员不可写（服务端 403 兜底）') : t('部署到本仓（浏览器上传）')} onClick={() => setDeployOpen(true)}>
              {t('⬆ 部署 Deploy')}
            </Button>
          )}
          <ButtonAsChild variant="outline" size="sm">
            <Link to={`/artifacts/${repo.key}`}>{t('浏览制品 →')}</Link>
          </ButtonAsChild>
          {canEditConfig && (
            <ButtonAsChild size="sm" data-testid="repo-edit-link">
              <Link to={`/admin/repositories/${repo.key}/edit`}>{t('编辑配置')}</Link>
            </ButtonAsChild>
          )}
        </div>
      </div>
      {repo.description && <p className="detail-desc text-dense text-muted-foreground">{repo.description}</p>}

      {mHolder && (
        <p className="page-note rounded-md border border-border bg-surface-2 px-3 py-2 text-dense text-muted-foreground" data-testid="repo-manage-note">
          {t('ⓘ 当前会话以 manage 持有者身份管理此仓（permission target 授予）：配置可编辑；删除仓库仍是全局管理面写（服务端 403 兜底）。')}
        </p>
      )}
      {readOnly && (
        <p className="page-note rounded-md border border-border bg-surface-2 px-3 py-2 text-dense text-muted-foreground" data-testid="repo-detail-readonly-note">
          {t('ⓘ 只读管理员（readonly_admin）视角：仓库配置只读、浏览器部署（Deploy）已禁用——配置保存走单仓管理面写 （CanManageRepo write）、部署走制品写面，服务端一律 403 兜底。')}
        </p>
      )}

      {/* 八 Tab 条（总令 §十） */}
      <div role="tablist" aria-label={t('仓库视图')} className="flex flex-wrap gap-1 border-b border-border">
        {TABS.map(([id, label]) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            data-testid={`repo-tab-${id}`}
            className={`-mb-px rounded-t-sm border-b-2 bg-transparent px-3 py-1.5 text-dense ${tab === id ? 'border-primary font-medium text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
            onClick={() => setTab(id)}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <div className="detail-grid grid grid-cols-1 gap-3 lg:grid-cols-[minmax(0,1fr)_320px]">
          <div className="flex flex-col gap-3">
            <section className="card section rounded-md border border-border bg-surface-1 p-4" data-testid="repo-commands">
              <h3 className="mb-2 text-[13px] font-semibold">{t('客户端接入（')}{repo.packageType}{t('）')}</h3>
              {commands.map((c) => (
                <div className="cmd-block mb-2 rounded-md border border-border bg-surface-2 p-2" key={c.title}>
                  <header className="mb-1 flex items-center justify-between gap-2">
                    <span className="text-dense font-medium">{c.title}</span>
                    <CopyButton value={c.text} label={c.title} />
                  </header>
                  <pre lang="en" tabIndex={0} className="overflow-x-auto font-mono text-aux">{c.text}</pre>
                  {c.note && <div className="note mt-1 text-aux text-muted-foreground">{c.note}</div>}
                </div>
              ))}
              <p className="field-hint text-aux text-muted-foreground">
                {t('地址按当前访问 origin 生成（')}{window.location.origin}{t('）；命令与 docs/user 接入文档同源。')}
              </p>
            </section>

            <section className="card section rounded-md border border-border bg-surface-1 p-4" data-testid="repo-usage-card">
              <h3 className="mb-2 text-[13px] font-semibold">{t('统计')}</h3>
              {rclass === 'virtual' ? (
                <p className="text-dense text-muted-foreground">{t('virtual 仓不持有自身内容——用量见各成员仓库。')}</p>
              ) : usage.status === 'loading' ? (
                <StateSkeleton lines={2} />
              ) : usage.status === 'ok' && usage.data ? (
                <>
                  <QuotaLine usage={usage.data} />
                  {rclass === 'remote' && (
                    <p className="field-hint mt-2 text-aux text-muted-foreground">{t('已用 = 缓存内容逻辑字节；命中率等 remote 统计端点未开放（ux R9，显示 —）。')}</p>
                  )}
                  {rclass === 'local' && usage.data.quotaBytes === 0 && (
                    <p className="field-hint mt-2 text-aux text-muted-foreground">{t('配额 0 = 不限；在「配置」Tab 设置 quotaBytes 后此处显示水位条。')}</p>
                  )}
                </>
              ) : (
                <p className="text-dense text-muted-foreground" title={usage.error?.message ?? ''}>
                  {t('用量不可用（')}{usage.error ? `HTTP ${usage.error.status}` : '—'}{t('）')}
                </p>
              )}
            </section>

            {rclass === 'remote' && (
              <section className="card section rounded-md border border-border bg-surface-1 p-4" data-testid="repo-remote-card">
                <h3 className="mb-2 text-[13px] font-semibold">{t('上游')}</h3>
                {kv('URL', (
                  <span className="font-mono" lang="en">
                    {cfgStr(cfg, 'url')} <CopyButton value={cfgStr(cfg, 'url')} label={t('上游 URL')} />
                    <span className="badge neutral ml-1 rounded-sm bg-secondary px-1.5 py-px text-[11px]">
                      {cfgStr(cfg, 'url').startsWith('https') ? 'https' : 'http'}
                    </span>
                  </span>
                ))}
                {kv(t('用户名'), cfgStr(cfg, 'username') || t('—（匿名）'))}
                {kv(t('密码'), <span className="text-muted-foreground">{t('不回显（NFR-S14）')}</span>)}
                {cfgBool(cfg, 'allowPrivateUpstream') && (
                  <div className="warn-box mt-2 rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-dense">{t('⚠ 已放行私网上游（allowPrivateUpstream）——SSRF 防线对该仓放宽。')}</div>
                )}
                {kv(t('命中 / 未命中 TTL'), <span className="font-mono">{cfgNum(cfg, 'retrievalCachePeriodSecs') ?? 7200}s / {cfgNum(cfg, 'missedRetrievalCachePeriodSecs') ?? 1800}s</span>)}
                {kv(t('socket 超时 / 静默期'), <span className="font-mono">{cfgNum(cfg, 'socketTimeoutSecs') ?? 15}s / {cfgNum(cfg, 'assumedOfflinePeriodSecs') ?? 300}s</span>)}
                {kv(t('远端浏览'), (
                  <span data-testid="repo-remote-browse">
                    {cfgBool(cfg, 'listRemoteFolderItems')
                      ? t('开启（listRemoteFolderItems——树含上游未缓存条目，点击回源拉取）')
                      : t('关闭（仅浏览已缓存内容）')}
                  </span>
                ))}
              </section>
            )}

            {rclass === 'virtual' && (
              <section className="card section rounded-md border border-border bg-surface-1 p-4" data-testid="repo-virtual-card">
                <h3 className="mb-2 text-[13px] font-semibold">{t('成员（解析顺序）')}</h3>
                <ol className="list-decimal pl-5 font-mono">
                  {cfgStrList(cfg, 'repositories').map((m) => (
                    <li key={m} lang="en">{m}</li>
                  ))}
                </ol>
                <div className="mt-2">
                  {kv(t('默认部署仓库'), <span className="font-mono" lang="en">{cfgStr(cfg, 'defaultDeploymentRepo') || t('（未配置）')}</span>)}
                </div>
                {!cfgStr(cfg, 'defaultDeploymentRepo') && (
                  <p className="field-hint text-aux text-muted-foreground">{t('未配置写路由：经此仓的部署 / 删除操作将返回 405。')}</p>
                )}
              </section>
            )}
          </div>

          {canDelete && (
            <aside>
              <section className="danger-zone rounded-md border border-destructive bg-surface-1 p-4" data-testid="repo-danger-zone">
                <h3 className="mb-1 text-[13px] font-semibold text-destructive">{t('危险区')}</h3>
                <p className="mb-2 text-dense text-muted-foreground">{t('删除仓库及其（可选）全部内容。制品不可变，此操作没有撤销。')}</p>
                <Button variant="outline" size="sm" className="border-destructive text-destructive" onClick={() => requestDelete(repo)} data-testid="repo-delete-button">
                  {t('删除仓库…')}
                </Button>
              </section>
            </aside>
          )}
        </div>
      )}

      {tab === 'artifacts' && <RepoArtifactsPanel repoKey={repo.key} />}

      {tab === 'configuration' && (
        <div className="flex flex-col gap-3">
          <section className="card section rounded-md border border-border bg-surface-1 p-4" data-testid="repo-governance-card">
            <h3 className="mb-2 text-[13px] font-semibold">{t('治理（governance）')}</h3>
            {rclass === 'local' ? (
              <div>
                <QuotaEditor repo={repo} disabled={!canEditConfig} onSaved={state.reload} />
                {kv('includesPattern', (
                  <span className="font-mono" lang="en">
                    {cfgStr(cfg, 'includesPattern') || t('**/*（默认）')}{' '}
                    <CopyButton value={cfgStr(cfg, 'includesPattern') || '**/*'} label="includesPattern" />
                  </span>
                ))}
                {kv('excludesPattern', (
                  <span className="font-mono" lang="en">
                    {cfgStr(cfg, 'excludesPattern') || t('（无）')}{' '}
                    <CopyButton value={cfgStr(cfg, 'excludesPattern')} label="excludesPattern" />
                  </span>
                ))}
                <p className="field-hint text-aux text-muted-foreground">{t('exclude 优先于 include；仅 local 仓支持治理字段。')}</p>
              </div>
            ) : (
              <p className="text-dense text-muted-foreground">{t('—（governance 字段仅 local 仓支持；')}{rclass} {t('的配置规范化会丢弃未知字段）')}</p>
            )}
          </section>

          <section className="card section rounded-md border border-border bg-surface-1 p-4">
            <h3 className="mb-2 text-[13px] font-semibold">{t('高级')}</h3>
            {kv(t('优先解析'), cfgBool(cfg, 'priorityResolution') ? t('是（priorityResolution）') : t('否'))}
            {rclass === 'remote' && (
              <>
                {kv('hardFail', cfgBool(cfg, 'hardFail') ? t('是（上游故障直接失败）') : t('否'))}
                {kv(t('允许私网上游'), cfgBool(cfg, 'allowPrivateUpstream') ? t('是（SSRF 防线放宽）') : t('否'))}
              </>
            )}
            {rclass === 'local' && packageType === 'maven' && (
              <>
                {kv('handleReleases / handleSnapshots', `${cfgBool(cfg, 'handleReleases', true) ? '✓' : '—'} / ${cfgBool(cfg, 'handleSnapshots', true) ? '✓' : '—'}`)}
                {kv('checksumPolicyType', <span className="font-mono" lang="en">{cfgStr(cfg, 'checksumPolicyType') || 'client-checksums'}</span>)}
                {kv('snapshotVersionBehavior', <span className="font-mono" lang="en">{cfgStr(cfg, 'snapshotVersionBehavior') || 'deployer'}</span>)}
              </>
            )}
            {rclass === 'virtual' && kv(t('默认部署仓库'), <span className="font-mono" lang="en">{cfgStr(cfg, 'defaultDeploymentRepo') || t('（未配置）')}</span>)}
            {canEditConfig ? (
              <p className="field-hint text-aux text-muted-foreground">
                {t('字段级修改走')}{' '}
                <Link to={`/admin/repositories/${repo.key}/edit`} className="text-primary underline">{t('编辑器')}</Link>
                {t('（全量替换语义——保存时整体重写 config）。')}
              </p>
            ) : (
              <p className="field-hint text-aux text-muted-foreground">{t('只读呈现（readonly_admin）；编辑需全量 admin 或该仓 manage 持有者。')}</p>
            )}
          </section>
        </div>
      )}

      {tab === 'storage' && (
        <section className="card section rounded-md border border-border bg-surface-1 p-4">
          <h3 className="mb-2 text-[13px] font-semibold">{t('存储')}</h3>
          {rclass === 'virtual' ? (
            <p className="text-dense text-muted-foreground">{t('virtual 仓不持有自身内容——用量见各成员仓库（Storage Tab 逐仓面）。')}</p>
          ) : usage.status === 'ok' && usage.data ? (
            <>
              {kv(t('已用'), <span className="font-mono">{formatBytes(usage.data.usedBytes)}</span>)}
              {kv(t('配额'), <span className="font-mono">{usage.data.quotaBytes > 0 ? formatBytes(usage.data.quotaBytes) : t('不限（0）')}</span>)}
              <QuotaLine usage={usage.data} />
            </>
          ) : (
            <p className="text-dense text-muted-foreground">{t('用量不可用（')}{usage.error ? `HTTP ${usage.error.status}` : '…'}{t('）')}</p>
          )}
        </section>
      )}

      {tab === 'permissions' && <RepoPermsPanel repoKey={repo.key} admin={session?.admin ?? false} />}

      {tab === 'replication' && (
        <section className="card section rounded-md border border-border bg-surface-1 p-4" data-testid="repo-repl-card">
          <h3 className="mb-2 text-[13px] font-semibold">Replication</h3>
          {rclass !== 'local' ? (
            <>
              <p className="text-dense text-muted-foreground" data-testid="repo-repl-na">
                {t('复制是 local 仓的 push 模型（源仓 → 目标实例仓，ADR-0021）——')}{rclass} {t('仓不适用。')}
              </p>
              <ButtonAsChild variant="outline" size="sm">
                <Link to="/admin/governance/replication" data-testid="repo-repl-goto">{t('前往复制管理 →')}</Link>
              </ButtonAsChild>
            </>
          ) : repl.status === 'loading' ? (
            <StateSkeleton lines={2} />
          ) : repl.status === 'forbidden' ? (
            <p className="text-dense text-muted-foreground">{t('复制配置为全局管理面（GET /api/v1/replications 需 system:read）——当前会话无权查看。')}</p>
          ) : repl.status === 'error' && repl.error && (repl.error.status === 501 || repl.error.status === 404) ? (
            <p className="text-dense text-muted-foreground">
              {repl.error.status === 501 ? t('本实例未启用复制（端点 501）。') : t('复制端点不可用（HTTP 404）。')}
            </p>
          ) : repl.status === 'error' && repl.error ? (
            <ErrorCard error={repl.error} onRetry={repl.reload} />
          ) : replConfigs.length === 0 ? (
            <>
              <p className="text-dense text-muted-foreground">{t('本仓尚无复制配置。')}</p>
              {canEditConfig && (
                <ButtonAsChild variant="outline" size="sm">
                  <Link to={`/admin/repositories/${repo.key}/edit?section=replications`} data-testid="repo-repl-edit-link">{t('在编辑页配置复制 →')}</Link>
                </ButtonAsChild>
              )}
            </>
          ) : (
            <>
              <table className="w-full text-dense" data-testid="repo-repl-table">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-2 py-1.5 font-medium">{t('名称')}</th>
                    <th scope="col" className="px-2 py-1.5 font-medium">{t('目标（实例 / 仓）')}</th>
                    <th scope="col" className="px-2 py-1.5 font-medium">{t('状态')}</th>
                  </tr>
                </thead>
                <tbody>
                  {replConfigs.map((c) => (
                    <tr key={c.id} data-testid={`repo-repl-row-${c.name}`} className="border-b border-border/60">
                      <td className="px-2 py-1.5 font-mono" lang="en">{c.name}</td>
                      <td className="max-w-[360px] break-all px-2 py-1.5 font-mono" lang="en">{c.target_url} → {c.target_repo}</td>
                      <td className="px-2 py-1.5">
                        <span className={`rounded-sm border px-1.5 py-0.5 text-[11px] ${c.enabled ? 'border-success text-success' : 'border-border text-muted-foreground'}`}>
                          {c.enabled ? t('已启用') : t('已停用')}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {canEditConfig ? (
                <ButtonAsChild variant="outline" size="sm" className="mt-2">
                  <Link to={`/admin/repositories/${repo.key}/edit?section=replications`} data-testid="repo-repl-edit-link">{t('管理本仓复制配置 →')}</Link>
                </ButtonAsChild>
              ) : (
                <p className="field-hint text-aux text-muted-foreground">{t('只读呈现（readonly_admin）；编辑需全量 admin 或该仓 manage 持有者。')}</p>
              )}
            </>
          )}
          {rclass === 'local' && (
            <p className="field-hint text-aux text-muted-foreground">
              {t('推送状态与最近事件见')}{' '}
              <Link to="/admin/governance/replication" data-testid="repo-repl-goto" className="text-primary underline">{t('全局复制页')}</Link>
              {t('（10s 轮询）。')}
            </p>
          )}
        </section>
      )}

      {tab === 'webhooks' && (
        <section className="card section rounded-md border border-border bg-surface-1 p-4">
          <h3 className="mb-2 text-[13px] font-semibold">Webhooks</h3>
          <p className="text-dense text-muted-foreground">{t('本仓相关的 webhook 订阅与投递记录在事件订阅管理页维护（criteria 按 repoKeys 圈定）。')}</p>
          <ButtonAsChild variant="outline" size="sm">
            <Link to="/admin/general/webhooks">{t('前往 Webhooks 管理 →')}</Link>
          </ButtonAsChild>
        </section>
      )}

      {tab === 'activity' && <RepoActivityPanel repoKey={repo.key} />}

      {smuOpen && routeKey && (
        <LegacyDialogHost>
          <SetMeUpDialog preselectedRepo={routeKey} onClose={() => setSmuOpen(false)} />
        </LegacyDialogHost>
      )}
      {deployOpen && routeKey && (
        <LegacyDialogHost>
          <DeployDialog preselectedRepo={routeKey} onClose={() => setDeployOpen(false)} onUploaded={state.reload} />
        </LegacyDialogHost>
      )}
    </div>
  )
}

/** Artifacts Tab：直系 children 概要（单层 listFolder）+ 浏览深链 */
function RepoArtifactsPanel({ repoKey }: { repoKey: string }) {
  const listing = useAsync(() => listFolder(repoKey, ''), [repoKey])
  const nodes = listing.status === 'ok' ? (listing.data?.nodes ?? []) : []
  const files = nodes.filter((n) => !n.folder)
  return (
    <section className="card section rounded-md border border-border bg-surface-1 p-4">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-[13px] font-semibold">{t('制品（仓根直系）')}</h3>
        <ButtonAsChild variant="outline" size="sm">
          <Link to={`/artifacts/${repoKey}`}>{t('在制品浏览器中打开 →')}</Link>
        </ButtonAsChild>
      </div>
      {listing.status === 'loading' && <StateSkeleton lines={3} />}
      {listing.status === 'forbidden' && (
        <EmptyState message={t('无权限浏览此仓库')} hint={t('内容面按路径 ACL 判定。')} />
      )}
      {listing.status === 'error' && listing.error && <ErrorCard error={listing.error} onRetry={listing.reload} />}
      {listing.status === 'ok' && (
        <>
          <div className="mb-2 flex gap-4 text-dense text-muted-foreground">
            <span>{t('目录')} {formatCount(nodes.length - files.length)}</span>
            <span>{t('文件')} {formatCount(files.length)}</span>
            <span>{t('直系文件合计')} {formatBytes(files.reduce((acc, n) => acc + (n.size ?? 0), 0))}</span>
          </div>
          {nodes.length === 0 ? (
            <EmptyState message={t('此仓库尚无内容')} hint={t('上传第一个制品，或创建子目录组织布局。')} />
          ) : (
            <table className="w-full text-dense">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-2 py-1.5 font-medium">{t('名称')}</th>
                  <th scope="col" className="px-2 py-1.5 font-medium">{t('类型')}</th>
                  <th scope="col" className="px-2 py-1.5 font-medium">{t('大小')}</th>
                </tr>
              </thead>
              <tbody>
                {nodes.slice(0, 50).map((n) => (
                  <tr key={n.name} className="border-b border-border/60">
                    <td className="px-2 py-1.5 font-mono" lang="en">
                      <Link to={`/artifacts/${repoKey}${n.path ? `/${n.path.split('/').map((s) => encodeURIComponent(s)).join('/')}` : ''}`} className="text-primary hover:underline">
                        {n.name}
                      </Link>
                    </td>
                    <td className="px-2 py-1.5">{n.folder ? t('目录') : t('文件')}</td>
                    <td className="px-2 py-1.5 font-mono">{!n.folder && n.size !== null ? formatBytes(n.size) : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
    </section>
  )
}

/** Permissions Tab：?permissions 有效权限视图（admin 门） */
function RepoPermsPanel({ repoKey, admin }: { repoKey: string; admin: boolean }) {
  const perms = useAsync(() => (admin ? getItemPermissions(repoKey, '') : Promise.resolve(null)), [repoKey, admin])
  if (!admin) {
    return (
      <section className="card section rounded-md border border-border bg-surface-1 p-4">
        <h3 className="mb-2 text-[13px] font-semibold">{t('权限')}</h3>
        <p className="text-dense text-muted-foreground">{t('有效权限视图是管理员面（?permissions 需单仓 manage 读门）——readonly/user 会话不渲染。')}</p>
      </section>
    )
  }
  const view = perms.status === 'ok' ? perms.data : null
  const users = Object.entries(view?.principals.users ?? {})
  const groups = Object.entries(view?.principals.groups ?? {})
  return (
    <section className="card section rounded-md border border-border bg-surface-1 p-4">
      <h3 className="mb-2 text-[13px] font-semibold">{t('权限（有效权限视图）')}</h3>
      {perms.status === 'loading' && <StateSkeleton lines={2} />}
      {perms.status === 'error' && perms.error && <ErrorCard error={perms.error} onRetry={perms.reload} />}
      {view && users.length === 0 && groups.length === 0 && (
        <p className="text-dense text-muted-foreground">{t('没有 permission target 覆盖此路径（admin 隐式全权）。')}</p>
      )}
      {view && (users.length > 0 || groups.length > 0) && (
        <div className="flex flex-wrap gap-1.5">
          {users.map(([name, bits]) => (
            <span key={`u-${name}`} className="chip-item flex items-center gap-1 rounded-sm border border-border bg-surface-2 px-2 py-0.5 text-dense">
              <span lang="en">{name}</span>
              <span className="font-mono text-aux">{bits.join('')}</span>
            </span>
          ))}
          {groups.map(([name, bits]) => (
            <span key={`g-${name}`} className="chip-item flex items-center gap-1 rounded-sm border border-border bg-surface-2 px-2 py-0.5 text-dense">
              <span aria-hidden="true">👥</span>
              <span lang="en">{name}</span>
              <span className="font-mono text-aux">{bits.join('')}</span>
            </span>
          ))}
        </div>
      )}
      <p className="field-hint mt-2 text-aux text-muted-foreground">
        {t('该视图与服务端授权判定同源；授权编辑见')}{' '}
        <Link to="/admin/security/permissions" className="text-primary underline">{t('权限 target')}</Link>{t('。')}
      </p>
    </section>
  )
}

/** Activity Tab：本仓最近审计（?repo= 过滤） */
function RepoActivityPanel({ repoKey }: { repoKey: string }) {
  const audit = useAsync(
    () => getAuditEventsPage({ repo: repoKey, actor: '', action: '', since: '', until: '' }, '', 8),
    [repoKey],
  )
  const events = audit.status === 'ok' ? (audit.data?.events ?? []) : []
  return (
    <section className="card section rounded-md border border-border bg-surface-1 p-4">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-[13px] font-semibold">{t('活动（最近审计 8 条）')}</h3>
        <Link to="/admin/governance/audit" className="text-[12px] text-primary hover:underline">{t('查看全部 →')}</Link>
      </div>
      {audit.status === 'loading' && <StateSkeleton lines={4} />}
      {audit.status === 'forbidden' && (
        <p className="text-dense text-muted-foreground">{t('审计日志是管理员视图（HTTP 403）。')}</p>
      )}
      {audit.status === 'error' && audit.error && <ErrorCard error={audit.error} onRetry={audit.reload} />}
      {audit.status === 'ok' && events.length === 0 && (
        <EmptyState message={t('暂无审计事件')} hint={t('建仓、上传、删除等操作会记录在这里')} />
      )}
      {audit.status === 'ok' && events.length > 0 && (
        <table className="w-full text-dense">
          <thead>
            <tr className="border-b border-border text-left text-aux text-muted-foreground">
              <th scope="col" className="px-2 py-1.5 font-medium">{t('时间')}</th>
              <th scope="col" className="px-2 py-1.5 font-medium">{t('操作者')}</th>
              <th scope="col" className="px-2 py-1.5 font-medium">{t('动作')}</th>
              <th scope="col" className="px-2 py-1.5 font-medium">{t('对象')}</th>
            </tr>
          </thead>
          <tbody>
            {events.map((ev: AuditEvent, i) => (
              <tr key={ev.id} className="border-b border-border/60" data-testid={`repo-activity-row-${i}`}>
                <td className="whitespace-nowrap px-2 py-1.5 font-mono">{formatAuditTime(ev.time)}</td>
                <td className="px-2 py-1.5">{ev.actor}</td>
                <td className="whitespace-nowrap px-2 py-1.5 font-mono" lang="en">{ev.action}</td>
                <td className="break-all px-2 py-1.5 font-mono" lang="en">{ev.path}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
