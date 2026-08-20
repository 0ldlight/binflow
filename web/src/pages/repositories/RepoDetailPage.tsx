import { useState } from 'react'
import type { ReactNode } from 'react'
import { Link, NavLink, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { useToast } from '../../app/ToastContext'
import { ApiError, errText } from '../../lib/api'
import { formatBytes } from '../../lib/format'
import {
  cfgBool,
  cfgNum,
  cfgStr,
  cfgStrList,
  deleteRepo,
  getRepoDetail,
  getRepoUsage,
} from '../../lib/repos'
import type { PackageType, RClass, RepoUsage } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'

import { clientCommands } from './commands'

// 仓库详情·概要（console-ux §4.5）：接入命令块（P3，docs/user 同源）+
// 统计（usage 端点：used / quota 水位条）+ governance / remote / virtual
// 特化 + 危险区（P5 删除）。「制品」tab 归 T-100（占位路由）。
//
// 删除流（AC②）：不勾 deleteContent 直接确认时若仓非空，服务端 400 的
// 原因（含「holds N node(s)」）被带回对话框原样呈现并预勾选——用户看见
// 影响面再决定，而不是撞 400。

function QuotaLine({ usage }: { usage: RepoUsage }) {
  const quota = usage.quotaBytes
  const used = usage.usedBytes
  if (quota <= 0) {
    return (
      <div className="water-line">
        <span className="label">
          已用 <span className="mono">{formatBytes(used)}</span> · 配额不限（0）
        </span>
      </div>
    )
  }
  const pct = quota > 0 ? Math.min(100, (used / quota) * 100) : 0
  const cls = used >= quota ? 'full' : pct >= 80 ? 'warn' : ''
  return (
    <div data-testid="repo-usage-bar">
      <div className={`water-bar${cls ? ` ${cls}` : ''}`} role="progressbar" aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100}>
        <div className="fill" style={{ width: `${Math.max(used > 0 ? 2 : 0, pct)}%` }} />
      </div>
      <div className="water-line" style={{ marginTop: 6 }}>
        <span className="label">
          <span className="mono">{formatBytes(used)}</span> / <span className="mono">{formatBytes(quota)}</span>（{pct.toFixed(1)}%）
          {used >= quota ? ' · 已满（写入将 413）' : pct >= 80 ? ' · 接近上限' : ''}
        </span>
      </div>
    </div>
  )
}

export default function RepoDetailPage() {
  const { key: routeKey } = useParams<{ key: string }>()
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const toast = useToast()
  const confirm = useConfirm()
  const navigate = useNavigate()

  const [deleting, setDeleting] = useState(false)

  const state = useAsync(() => (routeKey ? getRepoDetail(routeKey) : Promise.resolve(null)), [routeKey])
  // virtual 仓无自身内容（usage 恒 0），不发起请求
  const usage = useAsync(
    () =>
      state.data && state.data.rclass !== 'virtual' && routeKey
        ? getRepoUsage(routeKey)
        : Promise.resolve(null),
    [state.status, state.data?.rclass, routeKey],
  )

  if (state.status === 'loading') {
    return (
      <div data-testid="repo-detail-page">
        <Skeleton lines={8} />
      </div>
    )
  }
  if (state.status === 'forbidden' && state.error) {
    // 实测契约：/api/repositories 全面 admin-only（路由门）——非 admin 的
    // 403 呈现无权限卡而非空白（§5.1；见工作日志契约漂移 1）
    return (
      <div data-testid="repo-detail-page">
        <EmptyState
          message="无权限查看仓库"
          hint={`仓库管理面为管理员视图（${state.error.message}）`}
          action={
            <Link className="btn" to="/">
              ← 返回仪表盘
            </Link>
          }
        />
      </div>
    )
  }
  if (state.status === 'error' && state.error) {
    return (
      <div data-testid="repo-detail-page">
        {state.error.status === 404 ? (
          <EmptyState
            message={`仓库 ${routeKey} 不存在`}
            hint="key 可能打错，或仓库已被删除"
            action={
              <Link className="btn" to="/repositories">
                ← 返回仓库列表
              </Link>
            }
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

  const confirmDelete = async (reason?: string, presetContent = false): Promise<void> => {
    const holder = { typed: '', deleteContent: presetContent }
    const body: ReactNode = (
      <>
        {reason && (
          <div className="server-reason" data-testid="repo-delete-reason" lang="en">
            HTTP 400：{reason}
          </div>
        )}
        <p>
          将删除仓库 <b className="mono" lang="en">{repo.key}</b>
          （{repo.rclass} / {repo.packageType}）。制品不可变，删除<b>没有撤销</b>。
        </p>
        <label className="check-row">
          <input
            type="checkbox"
            checked={holder.deleteContent}
            onChange={(e) => {
              holder.deleteContent = e.target.checked
            }}
            data-testid="repo-delete-content"
          />
          同时删除内容（deleteContent）
        </label>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0 }}>
          <label htmlFor="del-confirm">
            输入仓库 key <b className="mono" lang="en">{repo.key}</b> 以确认：
          </label>
          <input
            id="del-confirm"
            className="confirm-input"
            autoComplete="off"
            onChange={(e) => {
              holder.typed = e.target.value
            }}
            data-testid="repo-delete-confirm-key"
            lang="en"
          />
        </div>
      </>
    )
    const ok = await confirm({
      title: '删除仓库',
      body,
      danger: true,
      confirmLabel: deleting ? '删除中…' : '删除仓库',
      confirmDisabled: () => holder.typed !== repo.key,
    })
    if (!ok) return
    setDeleting(true)
    try {
      const text = await deleteRepo(repo.key, holder.deleteContent)
      toast.success(text)
      navigate('/repositories')
    } catch (err) {
      const apiErr = err instanceof ApiError ? err : new ApiError(0, errText(err))
      if (apiErr.status === 400 && apiErr.message.includes('deleteContent') && !presetContent) {
        setDeleting(false)
        // 非空仓未勾内容删除：把 400 原因（含制品数）带回对话框，预勾选重试
        await confirmDelete(apiErr.message, true)
        return
      }
      toast.error(`删除失败：${apiErr.message}`)
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div data-testid="repo-detail-page">
      <p style={{ margin: '0 0 var(--bf-sp-2)' }}>
        <Link to="/repositories">← 仓库</Link>
      </p>
      <div className="detail-head">
        <span className="key" lang="en">
          {repo.key}
        </span>
        <CopyButton value={repo.key} label={`仓库 key ${repo.key}`} />
        <span className="badge neutral">{repo.rclass}</span>
        <span className="badge neutral">{repo.packageType}</span>
      </div>
      {repo.description && <p className="detail-desc">{repo.description}</p>}

      <nav className="tabs" aria-label="仓库视图">
        <NavLink to={`/repositories/${repo.key}`} end className={({ isActive }) => (isActive ? 'active' : '')}>
          概要
        </NavLink>
        <NavLink to={`/repositories/${repo.key}/tree`} className={({ isActive }) => (isActive ? 'active' : '')}>
          制品
        </NavLink>
        <NavLink to={`/repositories/${repo.key}/settings`} className={({ isActive }) => (isActive ? 'active' : '')}>
          设置
        </NavLink>
      </nav>

      <div className="detail-grid">
        <div>
          <section className="card section" data-testid="repo-commands">
            <h3>客户端接入（{repo.packageType}）</h3>
            {commands.map((c, i) => (
              <div className="cmd-block" key={c.title} data-testid={`repo-cmd-${packageType}-${i}`}>
                <header>
                  <span>{c.title}</span>
                  <CopyButton value={c.text} label={c.title} />
                </header>
                <pre lang="en">{c.text}</pre>
                {c.note && <div className="note">{c.note}</div>}
              </div>
            ))}
            <p className="field-hint">地址按当前访问 origin 生成（{window.location.origin}）；命令与 docs/user 接入文档同源。</p>
          </section>

          <section className="card section" data-testid="repo-usage-card">
            <h3>统计</h3>
            {rclass === 'virtual' ? (
              <p className="text-2">virtual 仓不持有自身内容——用量见各成员仓库。</p>
            ) : usage.status === 'loading' ? (
              <Skeleton lines={2} />
            ) : usage.status === 'ok' && usage.data ? (
              <>
                <QuotaLine usage={usage.data} />
                {rclass === 'remote' && (
                  <p className="field-hint" style={{ marginTop: 8 }}>
                    已用 = 缓存内容逻辑字节；命中率等 remote 统计端点未开放（ux R9，显示 —）。
                  </p>
                )}
                {rclass === 'local' && usage.data.quotaBytes === 0 && (
                  <p className="field-hint" style={{ marginTop: 8 }}>
                    配额 0 = 不限；在设置页配置 quotaBytes 后此处显示水位条。
                  </p>
                )}
              </>
            ) : (
              <p className="text-2" title={usage.error?.message ?? ''}>
                用量不可用（{usage.error ? `HTTP ${usage.error.status}` : '—'}）
              </p>
            )}
          </section>

          <section className="card section" data-testid="repo-governance-card">
            <h3>治理</h3>
            {rclass === 'local' ? (
              <div>
                <div className="kv">
                  <span className="k">quotaBytes</span>
                  <span>
                    <span className="mono">{String(cfgNum(cfg, 'quotaBytes') ?? 0)}</span>
                    {cfgNum(cfg, 'quotaBytes') ? '' : '（不限）'}
                  </span>
                </div>
                <div className="kv">
                  <span className="k">includesPattern</span>
                  <span className="mono" lang="en">
                    {cfgStr(cfg, 'includesPattern') || '**/*（默认）'} <CopyButton value={cfgStr(cfg, 'includesPattern') || '**/*'} label="includesPattern" />
                  </span>
                </div>
                <div className="kv">
                  <span className="k">excludesPattern</span>
                  <span className="mono" lang="en">
                    {cfgStr(cfg, 'excludesPattern') || '（无）'} <CopyButton value={cfgStr(cfg, 'excludesPattern')} label="excludesPattern" />
                  </span>
                </div>
                <p className="field-hint">exclude 优先于 include；仅 local 仓支持治理字段。</p>
              </div>
            ) : (
              <p className="text-2">
                —（governance 字段仅 local 仓支持；{rclass} 的配置规范化会丢弃未知字段）
              </p>
            )}
          </section>

          {rclass === 'remote' && (
            <section className="card section" data-testid="repo-remote-card">
              <h3>上游</h3>
              <div className="kv">
                <span className="k">URL</span>
                <span className="mono" lang="en">
                  {cfgStr(cfg, 'url')} <CopyButton value={cfgStr(cfg, 'url')} label="上游 URL" />
                  <span className="badge neutral" style={{ marginLeft: 8 }}>
                    {cfgStr(cfg, 'url').startsWith('https') ? 'https' : 'http'}
                  </span>
                </span>
              </div>
              <div className="kv">
                <span className="k">用户名</span>
                <span>{cfgStr(cfg, 'username') || '—（匿名）'}</span>
              </div>
              <div className="kv">
                <span className="k">密码</span>
                <span className="text-2">不回显（NFR-S14）</span>
              </div>
              {cfgBool(cfg, 'allowPrivateUpstream') && (
                <div className="warn-box" style={{ marginTop: 8 }}>
                  ⚠ 已放行私网上游（allowPrivateUpstream）——SSRF 防线对该仓放宽。
                </div>
              )}
              <div className="kv">
                <span className="k">命中 / 未命中 TTL</span>
                <span className="mono">
                  {cfgNum(cfg, 'retrievalCachePeriodSecs') ?? 7200}s / {cfgNum(cfg, 'missedRetrievalCachePeriodSecs') ?? 1800}s
                </span>
              </div>
              <div className="kv">
                <span className="k">socket 超时 / 静默期</span>
                <span className="mono">
                  {cfgNum(cfg, 'socketTimeoutSecs') ?? 15}s / {cfgNum(cfg, 'assumedOfflinePeriodSecs') ?? 300}s
                </span>
              </div>
            </section>
          )}

          {rclass === 'virtual' && (
            <section className="card section" data-testid="repo-virtual-card">
              <h3>成员（解析顺序）</h3>
              <ol className="mono" style={{ paddingLeft: 20 }}>
                {cfgStrList(cfg, 'repositories').map((m) => (
                  <li key={m} lang="en">
                    {m}
                  </li>
                ))}
              </ol>
              <div className="kv" style={{ marginTop: 8 }}>
                <span className="k">默认部署仓库</span>
                <span className="mono" lang="en">
                  {cfgStr(cfg, 'defaultDeploymentRepo') || '（未配置）'}
                </span>
              </div>
              {!cfgStr(cfg, 'defaultDeploymentRepo') && (
                <p className="field-hint">未配置写路由：经此仓的部署 / 删除操作将返回 405。</p>
              )}
            </section>
          )}
        </div>

        {admin && (
          <aside>
            <div className="danger-zone" data-testid="repo-danger-zone">
              <h3>危险区</h3>
              <p>删除仓库及其（可选）全部内容。制品不可变，此操作没有撤销。</p>
              <button
                type="button"
                className="btn danger"
                disabled={deleting}
                onClick={() => void confirmDelete()}
                data-testid="repo-delete-button"
              >
                删除仓库…
              </button>
            </div>
          </aside>
        )}
      </div>
    </div>
  )
}
