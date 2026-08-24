import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { CopyButton } from '../../components/CopyButton'
import DeployDialog from '../../components/DeployDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import SetMeUpDialog from '../../components/SetMeUpDialog'
import { Skeleton } from '../../components/Skeleton'
import { useToast } from '../../app/ToastContext'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import { formatBytes } from '../../lib/format'
import { onTablistKeys } from '../../lib/keys'
import {
  buildLocalQuotaBody,
  cfgBool,
  cfgNum,
  cfgStr,
  cfgStrList,
  getRepoDetail,
  getRepoUsage,
  updateRepo,
} from '../../lib/repos'
import type { PackageType, RClass, RepoDetail, RepoUsage } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'

import './repositories.css'
import { clientCommands } from './commands'
import { useRepoDelete } from './RepoDeleteConfirm'

// 仓库详情（console-m8 §6.8——BinFlow 自有增强页，T-240 三 Tab 化）：
//
//   概要        接入命令（P3，docs/user 同源）+ 统计（usage 水位条）+
//               remote 上游 / virtual 成员特化
//   配置        治理（quota 行内编辑 + patterns）+ 高级字段 +
//               「打开编辑器」入口（全量编辑走 /edit 的全量替换保全）
//   Replications OSS 同款降级位——BinFlow 的复制配置由全局复制页承载
//               （/admin/governance/replication，T-159/T-180），本仓级
//               不建第二入口
//
// 门（router.go / rbac.go 实测）：GET /api/repositories/{key} 走
// CanManageRepo——admin/readonly_admin 全量可读；普通 user 仅覆盖集内
// （持该仓 manage）可得，403 → L2。于是：
// - 危险区（删除 = CapRepoWrite）仅全量 admin 渲染（L4）。
// - 配置编辑（quota 行内 + 编辑器入口）对 admin 与 m-holder（普通 user，
//   GET 已过即覆盖集内）开放；readonly_admin 禁用 + 注记（§7.3）。
//   UI 不自行判定覆盖集（§7.10：403 驱动）。

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
      <div
        className={`water-bar${cls ? ` ${cls}` : ''}`}
        role="progressbar"
        aria-valuenow={Math.round(pct)}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <div className="fill" style={{ width: `${Math.max(used > 0 ? 2 : 0, pct)}%` }} />
      </div>
      <div className="water-line" style={{ marginTop: 6 }}>
        <span className="label">
          <span className="mono">{formatBytes(used)}</span> / <span className="mono">{formatBytes(quota)}</span>
          （{pct.toFixed(1)}%）{used >= quota ? ' · 已满（写入将 413）' : pct >= 80 ? ' · 接近上限' : ''}
        </span>
      </div>
    </div>
  )
}

/** 配置 Tab 的行内配额编辑（CanManageRepo 语义：admin / m-holder 可写） */
function QuotaEditor({
  repo,
  disabled,
  onSaved,
}: {
  repo: RepoDetail
  disabled: boolean
  onSaved: () => void
}) {
  const toast = useToast()
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
      <div className="kv">
        <span className="k">quotaBytes</span>
        <span>
          <span className="mono">{current}</span>
          {cfgNum(repo.configuration, 'quotaBytes') ? '' : '（不限）'}
        </span>
      </div>
      <div className="quota-edit">
        <input
          className="mono-input"
          inputMode="numeric"
          aria-label="配额 quotaBytes（字节）"
          value={value}
          disabled={disabled}
          aria-invalid={bad}
          onChange={(e) => setDraft(e.target.value)}
          data-testid="repo-quota-input"
        />
        <button
          type="button"
          className="btn"
          disabled={disabled || saving || bad || value.trim() === current}
          onClick={() => void save()}
          data-testid="repo-quota-save"
        >
          {saving ? '保存中…' : '保存配额'}
        </button>
        <button
          type="button"
          className="btn"
          disabled={disabled || draft === null}
          onClick={() => {
            setDraft(null)
            setError(null)
          }}
          data-testid="repo-quota-cancel"
        >
          取消
        </button>
      </div>
      {error && (
        <p className="field-error" role="alert" data-testid="repo-quota-error">
          {error}
        </p>
      )}
      <p className="field-hint">正整数；0 = 不限。超限写入收到 413（message 含 used/quota）。</p>
    </div>
  )
}

type DetailTab = 'summary' | 'config' | 'replications'

export default function RepoDetailPage() {
  const { key: routeKey } = useParams<{ key: string }>()
  const { session } = useAuth()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  // 普通 user 且 GET 已通过 ⇒ 覆盖集内（m-holder）；详情页为其开放配置编辑
  const role = normalizeAdminRole(session?.adminRole, session?.admin ?? false)
  const mHolder = role === 'user'

  const [tab, setTab] = useState<DetailTab>('summary')
  useEffect(() => setTab('summary'), [routeKey])

  // 对话框族（T-242）：详情头 Set Me Up / Deploy 入口（dialog state 就地）
  const [smuOpen, setSmuOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)

  const state = useAsync(() => (routeKey ? getRepoDetail(routeKey) : Promise.resolve(null)), [routeKey])
  // virtual 仓无自身内容（usage 恒 0），不发起请求
  const usage = useAsync(
    () =>
      state.data && state.data.rclass !== 'virtual' && routeKey
        ? getRepoUsage(routeKey)
        : Promise.resolve(null),
    [state.status, state.data?.rclass, routeKey],
  )

  const requestDelete = useRepoDelete({})

  if (state.status === 'loading') {
    return (
      <div data-testid="repo-detail-page">
        <Skeleton lines={8} />
      </div>
    )
  }
  if (state.status === 'forbidden' && state.error) {
    // CanManageRepo：admin/readonly 全量、普通 user 覆盖集内——403 即无门
    return (
      <div data-testid="repo-detail-page">
        <EmptyState
          message="无权限查看仓库"
          hint={
            mHolder
              ? `单仓管理视图需要该仓的 manage 动作（permission target 授权）；${state.error.message}`
              : `仓库管理面为管理员视图（${state.error.message}）`
          }
          action={
            <Link className="btn" to="/artifacts">
              ← 前往制品浏览
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
              <Link className="btn" to="/admin/repositories/local">
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
  // 配置编辑位：readonly 禁用；admin 与 m-holder 开放（文件头注的门语义）
  const canEditConfig = !readOnly
  // 危险区：删除是 CapRepoWrite（仅全量 admin）——L4 预收敛
  const canDelete = admin

  return (
    <div data-testid="repo-detail-page">
      <p style={{ margin: '0 0 var(--bf-sp-2)' }}>
        <Link to={`/admin/repositories/${rclass}`}>← 仓库</Link>
      </p>
      <div className="detail-head">
        <span className="key" lang="en">
          {repo.key}
        </span>
        <CopyButton value={repo.key} label={`仓库 key ${repo.key}`} />
        <span className="badge neutral">{repo.rclass}</span>
        <span className="badge neutral">{repo.packageType}</span>
        <div className="detail-head-actions">
          <button
            type="button"
            className="btn"
            data-testid="repo-setmeup"
            title="Set Me Up：客户端接入向导"
            onClick={() => setSmuOpen(true)}
          >
            Set Me Up
          </button>
          {rclass === 'local' && (packageType === 'generic' || packageType === 'maven') && (
            <button
              type="button"
              className="btn"
              data-testid="repo-deploy"
              title="部署到本仓（浏览器上传）"
              onClick={() => setDeployOpen(true)}
            >
              ⬆ 部署 Deploy
            </button>
          )}
          <Link className="btn" to={`/artifacts/${repo.key}`} data-testid="repo-goto-tree">
            浏览制品 →
          </Link>
          {canEditConfig && (
            <Link className="btn primary" to={`/admin/repositories/${repo.key}/edit`} data-testid="repo-edit-link">
              编辑配置
            </Link>
          )}
        </div>
      </div>
      {repo.description && <p className="detail-desc">{repo.description}</p>}

      {mHolder && (
        <p className="page-note" data-testid="repo-manage-note">
          ⓘ 当前会话以 manage 持有者身份管理此仓（permission target 授予）：配置可编辑；删除仓库仍是全局管理面写（服务端
          403 兜底）。
        </p>
      )}
      {readOnly && (
        <p className="page-note" data-testid="repo-detail-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：仓库配置只读——保存走单仓管理面写（CanManageRepo write），服务端 403
          兜底。
        </p>
      )}

      <div
        className="repo-tabs"
        role="tablist"
        aria-label="仓库视图"
        onKeyDown={(e) => onTablistKeys(e, ['summary', 'config', 'replications'], tab, setTab)}
      >
        {(
          [
            ['summary', '概要'],
            ['config', '配置'],
            ['replications', 'Replications'],
          ] as [DetailTab, string][]
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            className={`repo-tab${tab === id ? ' active' : ''}`}
            data-testid={`repo-tab-${id}`}
            onClick={() => setTab(id)}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'summary' && (
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
                  {/* tabIndex：命令块 pre 实际溢出（overflow-x auto）——可滚动
                      区须键盘可达（WCAG 2.1 axe scrollable-region-focusable，
                      T-246 终验 QA-1 fix-forward；焦点环走全局 :focus-visible） */}
                  <pre lang="en" tabIndex={0}>{c.text}</pre>
                  {c.note && <div className="note">{c.note}</div>}
                </div>
              ))}
              <p className="field-hint">
                地址按当前访问 origin 生成（{window.location.origin}）；命令与 docs/user 接入文档同源。
              </p>
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
                      配额 0 = 不限；在「配置」Tab 设置 quotaBytes 后此处显示水位条。
                    </p>
                  )}
                </>
              ) : (
                <p className="text-2" title={usage.error?.message ?? ''}>
                  用量不可用（{usage.error ? `HTTP ${usage.error.status}` : '—'}）
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
                    {cfgNum(cfg, 'retrievalCachePeriodSecs') ?? 7200}s /{' '}
                    {cfgNum(cfg, 'missedRetrievalCachePeriodSecs') ?? 1800}s
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

          {canDelete && (
            <aside>
              <div className="danger-zone" data-testid="repo-danger-zone">
                <h3>危险区</h3>
                <p>删除仓库及其（可选）全部内容。制品不可变，此操作没有撤销。</p>
                <button
                  type="button"
                  className="btn danger"
                  onClick={() => requestDelete(repo)}
                  data-testid="repo-delete-button"
                >
                  删除仓库…
                </button>
              </div>
            </aside>
          )}
        </div>
      )}

      {tab === 'config' && (
        <div>
          <section className="card section" data-testid="repo-governance-card">
            <h3>治理（governance）</h3>
            {rclass === 'local' ? (
              <div>
                <QuotaEditor repo={repo} disabled={!canEditConfig} onSaved={state.reload} />
                <div className="kv">
                  <span className="k">includesPattern</span>
                  <span className="mono" lang="en">
                    {cfgStr(cfg, 'includesPattern') || '**/*（默认）'}{' '}
                    <CopyButton
                      value={cfgStr(cfg, 'includesPattern') || '**/*'}
                      label="includesPattern"
                    />
                  </span>
                </div>
                <div className="kv">
                  <span className="k">excludesPattern</span>
                  <span className="mono" lang="en">
                    {cfgStr(cfg, 'excludesPattern') || '（无）'}{' '}
                    <CopyButton value={cfgStr(cfg, 'excludesPattern')} label="excludesPattern" />
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

          <section className="card section" data-testid="repo-advanced-card">
            <h3>高级</h3>
            <div className="kv">
              <span className="k">优先解析</span>
              <span>{cfgBool(cfg, 'priorityResolution') ? '是（priorityResolution）' : '否'}</span>
            </div>
            {rclass === 'remote' && (
              <>
                <div className="kv">
                  <span className="k">hardFail</span>
                  <span>{cfgBool(cfg, 'hardFail') ? '是（上游故障直接失败）' : '否'}</span>
                </div>
                <div className="kv">
                  <span className="k">允许私网上游</span>
                  <span>{cfgBool(cfg, 'allowPrivateUpstream') ? '是（SSRF 防线放宽）' : '否'}</span>
                </div>
              </>
            )}
            {rclass === 'local' && packageType === 'maven' && (
              <>
                <div className="kv">
                  <span className="k">handleReleases / handleSnapshots</span>
                  <span>
                    {cfgBool(cfg, 'handleReleases', true) ? '✓' : '—'} / {cfgBool(cfg, 'handleSnapshots', true) ? '✓' : '—'}
                  </span>
                </div>
                <div className="kv">
                  <span className="k">checksumPolicyType</span>
                  <span className="mono" lang="en">
                    {cfgStr(cfg, 'checksumPolicyType') || 'client-checksums'}
                  </span>
                </div>
                <div className="kv">
                  <span className="k">snapshotVersionBehavior</span>
                  <span className="mono" lang="en">
                    {cfgStr(cfg, 'snapshotVersionBehavior') || 'deployer'}
                  </span>
                </div>
              </>
            )}
            {rclass === 'virtual' && (
              <div className="kv">
                <span className="k">默认部署仓库</span>
                <span className="mono" lang="en">
                  {cfgStr(cfg, 'defaultDeploymentRepo') || '（未配置）'}
                </span>
              </div>
            )}
            {canEditConfig ? (
              <p className="field-hint">
                字段级修改走{' '}
                <Link to={`/admin/repositories/${repo.key}/edit`} data-testid="repo-edit-link-config">
                  编辑器
                </Link>
                （全量替换语义——保存时整体重写 config）。
              </p>
            ) : (
              <p className="field-hint">只读呈现（readonly_admin）；编辑需全量 admin 或该仓 manage 持有者。</p>
            )}
          </section>
        </div>
      )}

      {tab === 'replications' && (
        <section className="card section degraded" data-testid="repo-repl-degraded">
          <h3>Replications</h3>
          <p className="text-2">
            本仓的复制配置由全局复制页承载（BinFlow 复制是 push 目标模型，配置不按仓分页）。
          </p>
          <Link className="btn" to="/admin/governance/replication" data-testid="repo-repl-goto">
            前往复制管理 →
          </Link>
        </section>
      )}

      {smuOpen && routeKey && <SetMeUpDialog preselectedRepo={routeKey} onClose={() => setSmuOpen(false)} />}
      {deployOpen && routeKey && (
        <DeployDialog preselectedRepo={routeKey} onClose={() => setDeployOpen(false)} onUploaded={state.reload} />
      )}
    </div>
  )
}
