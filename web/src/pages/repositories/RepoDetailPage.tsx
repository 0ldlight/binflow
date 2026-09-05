import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import LinearProgress from '@mui/material/LinearProgress'
import Paper from '@mui/material/Paper'
import Tab from '@mui/material/Tab'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Tabs from '@mui/material/Tabs'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'

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
import { monoInputSx } from '../../lib/muiAtoms'
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
import { configsForRepo, listReplicationConfigs } from '../../lib/replications'
import type { ReplicationConfig } from '../../lib/replications'
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
//   Replications T-404 指针升级（R1 裁定）：本仓配置摘要 + 指向仓库编辑页
//               Replications 节的深链（配置 CRUD 真身）+ 全局复制页链接
//               （状态/事件面，T-159/T-180）；remote/virtual = 不适用注记
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
    <div>
      {/* T-344 批 C：水位条换 MUI LinearProgress（≥80% warning、≥100%
          error；aria-label 补齐——T-344B 交接要点 9 的存量欠账） */}
      <LinearProgress
        className={`water-bar${cls ? ` ${cls}` : ''}`}
        variant="determinate"
        value={Math.max(used > 0 ? 2 : 0, Math.round(pct))}
        color={cls === 'full' ? 'error' : cls === 'warn' ? 'warning' : 'primary'}
        aria-label={`${usage.repo ?? ''}配额水位`}
        sx={{ height: 8, borderRadius: 'var(--bf-r-sm)' }}
      />
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
        <TextField
          size="small"
          inputMode="numeric"
          autoComplete="off"
          value={value}
          disabled={disabled}
          error={bad}
          onChange={(e) => setDraft(e.target.value)}
          sx={{ ...monoInputSx, width: 160 }}
          slotProps={{
            htmlInput: {
              'aria-label': '配额 quotaBytes（字节）',
              'data-testid': 'repo-quota-input',
              lang: 'en',
              className: 'mono',
            },
          }}
        />
        <Button
          variant="outlined"
          size="small"
          disabled={disabled || saving || bad || value.trim() === current}
          onClick={() => void save()}
          data-testid="repo-quota-save"
        >
          {saving ? '保存中…' : '保存配额'}
        </Button>
        <Button
          variant="outlined"
          size="small"
          disabled={disabled || draft === null}
          onClick={() => {
            setDraft(null)
            setError(null)
          }}
        >
          取消
        </Button>
      </div>
      {error && (
        <p className="field-error" role="alert">
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
  // T-404：本仓复制配置摘要（Replications Tab 指针升级）——local 仓才拉
  // （push 源 = local；列表端点无 query，客户端按 source_repo 过滤）
  const repl = useAsync(
    () =>
      state.data && state.data.rclass === 'local'
        ? listReplicationConfigs()
        : Promise.resolve([] as ReplicationConfig[]),
    [state.status, state.data?.rclass],
  )
  const replConfigs = useMemo(
    () => configsForRepo(repl.data ?? [], routeKey ?? ''),
    [repl.data, routeKey],
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
            <Button component={Link} to="/artifacts" variant="outlined" size="small">
              ← 前往制品浏览
            </Button>
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
              <Button component={Link} to="/admin/repositories/local" variant="outlined" size="small">
                ← 返回仓库列表
              </Button>
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
        <Chip size="small" className="badge neutral" label={repo.rclass} />
        <Chip size="small" className="badge neutral" label={repo.packageType} />
        <div className="detail-head-actions">
          <Button
            variant="outlined"
            size="small"
            data-testid="repo-setmeup"
            title="Set Me Up：客户端接入向导"
            onClick={() => setSmuOpen(true)}
          >
            Set Me Up
          </Button>
          {rclass === 'local' && (packageType === 'generic' || packageType === 'maven') && (
            <Button
              variant="outlined"
              size="small"
              data-testid="repo-deploy"
              disabled={readOnly}
              title={readOnly ? '只读管理员不可写（服务端 403 兜底）' : '部署到本仓（浏览器上传）'}
              onClick={() => setDeployOpen(true)}
            >
              ⬆ 部署 Deploy
            </Button>
          )}
          <Button component={Link} variant="outlined" size="small" to={`/artifacts/${repo.key}`}>
            浏览制品 →
          </Button>
          {canEditConfig && (
            <Button
              component={Link}
              variant="contained"
              size="small"
              to={`/admin/repositories/${repo.key}/edit`}
              data-testid="repo-edit-link"
            >
              编辑配置
            </Button>
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
          ⓘ 只读管理员（readonly_admin）视角：仓库配置只读、浏览器部署（Deploy）已禁用——配置保存走单仓管理面写
          （CanManageRepo write）、部署走制品写面，服务端一律 403 兜底。
        </p>
      )}

      {/* T-344 批 C：repo-tabs 换 MUI Tabs（锚 repo-tab-* 落 Tab 根
          <button>、aria-selected 内建；方向键选择随焦点 = MUI 内建，
          keyboard spec tablist 腿等价覆盖） */}
      <Tabs
        value={tab}
        onChange={(_e, id: DetailTab) => setTab(id)}
        selectionFollowsFocus
        aria-label="仓库视图"
        sx={{ mb: 'var(--bf-sp-4)', borderBottom: 1, borderColor: 'divider' }}
      >
        {(
          [
            ['summary', '概要'],
            ['config', '配置'],
            ['replications', 'Replications'],
          ] as [DetailTab, string][]
        ).map(([id, label]) => (
          <Tab key={id} value={id} label={label} data-testid={`repo-tab-${id}`} />
        ))}
      </Tabs>

      {tab === 'summary' && (
        <div className="detail-grid">
          <div>
            <section className="card section" data-testid="repo-commands">
              <h3>客户端接入（{repo.packageType}）</h3>
              {commands.map((c) => (
                <div className="cmd-block" key={c.title}>
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
                    <Chip
                      size="small"
                      className="badge neutral"
                      label={cfgStr(cfg, 'url').startsWith('https') ? 'https' : 'http'}
                      sx={{ ml: 1, verticalAlign: 'middle' }}
                    />
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
                {/* T-461（FR-147 AC1）：远端浏览可选档回显（编辑表单同源字段
                    listRemoteFolderItems——GET configuration 投影） */}
                <div className="kv">
                  <span className="k">远端浏览</span>
                  <span data-testid="repo-remote-browse">
                    {cfgBool(cfg, 'listRemoteFolderItems')
                      ? '开启（listRemoteFolderItems——树含上游未缓存条目，点击回源拉取）'
                      : '关闭（仅浏览已缓存内容）'}
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
              {/* T-344 批 D：危险区 Paper 化（§3.3）——outlined + error 边 */}
              <Paper
                variant="outlined"
                className="danger-zone"
                sx={{ p: 'var(--bf-sp-4)', borderColor: 'error.main' }}
                data-testid="repo-danger-zone"
              >
                <Typography variant="subtitle2" component="h3" color="error" sx={{ mb: 1 }}>
                  危险区
                </Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
                  删除仓库及其（可选）全部内容。制品不可变，此操作没有撤销。
                </Typography>
                <Button
                  variant="outlined"
                  color="error"
                  size="small"
                  onClick={() => requestDelete(repo)}
                  data-testid="repo-delete-button"
                >
                  删除仓库…
                </Button>
              </Paper>
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

          <section className="card section">
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
                <Link to={`/admin/repositories/${repo.key}/edit`}>
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
        // T-404 指针升级（R1 裁定）：配置 CRUD 真身 = 仓库编辑页 Replications
        // 节（本 Tab 保留指针形态——BOARD 裁定「仓 Tab 保留指针」）。本 Tab
        // 呈现本仓配置摘要（GET /v1/replications 客户端按 source_repo 过滤，
        // 四态）+ 指向编辑节的深链 + 全局复制页链接（状态/事件面）。
        // remote/virtual：push 复制模型不适用（源 = local 仓）——如实注记。
        <section className="card section" data-testid="repo-repl-card">
          <h3>Replications</h3>
          {rclass !== 'local' ? (
            <>
              <p className="text-2" data-testid="repo-repl-na">
                复制是 local 仓的 push 模型（源仓 → 目标实例仓，ADR-0021）——{rclass} 仓不适用。
              </p>
              <Button component={Link} variant="outlined" size="small" to="/admin/governance/replication" data-testid="repo-repl-goto">
                前往复制管理 →
              </Button>
            </>
          ) : repl.status === 'loading' ? (
            <Skeleton lines={2} />
          ) : repl.status === 'forbidden' ? (
            <p className="text-2">
              复制配置为全局管理面（GET /api/v1/replications 需 system:read）——当前会话无权查看。
            </p>
          ) : repl.status === 'error' && repl.error && (repl.error.status === 501 || repl.error.status === 404) ? (
            <p className="text-2">
              {repl.error.status === 501 ? '本实例未启用复制（端点 501）。' : '复制端点不可用（HTTP 404）。'}
            </p>
          ) : repl.status === 'error' && repl.error ? (
            <ErrorCard error={repl.error} onRetry={repl.reload} />
          ) : replConfigs.length === 0 ? (
            <>
              <p className="text-2">本仓尚无复制配置。</p>
              {canEditConfig && (
                <Button
                  component={Link}
                  variant="outlined"
                  size="small"
                  to={`/admin/repositories/${repo.key}/edit?section=replications`}
                  data-testid="repo-repl-edit-link"
                >
                  在编辑页配置复制 →
                </Button>
              )}
            </>
          ) : (
            <>
              <Table size="small" data-testid="repo-repl-table">
                <TableHead>
                  <TableRow>
                    <TableCell component="th" scope="col">名称</TableCell>
                    <TableCell component="th" scope="col">目标（实例 / 仓）</TableCell>
                    <TableCell component="th" scope="col">状态</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {replConfigs.map((c) => (
                    <TableRow key={c.id} data-testid={`repo-repl-row-${c.name}`} hover>
                      <TableCell className="mono" lang="en">
                        {c.name}
                      </TableCell>
                      <TableCell className="mono" lang="en" sx={{ maxWidth: 360, whiteSpace: 'normal', wordBreak: 'break-all' }}>
                        {c.target_url} → {c.target_repo}
                      </TableCell>
                      <TableCell>
                        <Chip
                          size="small"
                          variant="outlined"
                          color={c.enabled ? 'success' : 'default'}
                          className={`badge ${c.enabled ? 'success' : 'neutral'}`}
                          label={c.enabled ? '已启用' : '已停用'}
                        />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {canEditConfig ? (
                <Button
                  component={Link}
                  variant="outlined"
                  size="small"
                  to={`/admin/repositories/${repo.key}/edit?section=replications`}
                  data-testid="repo-repl-edit-link"
                  sx={{ mt: 1.5 }}
                >
                  管理本仓复制配置 →
                </Button>
              ) : (
                <p className="field-hint">只读呈现（readonly_admin）；编辑需全量 admin 或该仓 manage 持有者。</p>
              )}
            </>
          )}
          {rclass === 'local' && (
            <p className="field-hint" style={{ marginBottom: 0 }}>
              推送状态与最近事件见{' '}
              <Link to="/admin/governance/replication" data-testid="repo-repl-goto">
                全局复制页
              </Link>
              （10s 轮询）。
            </p>
          )}
        </section>
      )}

      {smuOpen && routeKey && <SetMeUpDialog preselectedRepo={routeKey} onClose={() => setSmuOpen(false)} />}
      {deployOpen && routeKey && (
        <DeployDialog preselectedRepo={routeKey} onClose={() => setDeployOpen(false)} onUploaded={state.reload} />
      )}
    </div>
  )
}
