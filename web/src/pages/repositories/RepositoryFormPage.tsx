import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, getRepositories, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import type { RepoListItem } from '../../lib/api'
import {
  PACKAGE_TYPES,
  RCLASSES,
  comboAllowed,
  createRepo,
  cfgBool,
  cfgNum,
  cfgStr,
  cfgStrList,
  getRepoDetail,
  updateRepo,
  validateRepoKey,
  validateUpstreamURL,
} from '../../lib/repos'
import type { PackageType, RClass, RepoConfigBody } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'

import './repositories.css'

// 建仓/编辑表单（console-m8 §4.4/§6.7，T-240 重排）：进页弹包类型网格
// （5 项，必选）→ **单页分区式**表单（常规设置 → 来源/成员 → 包类型
// 专属 → 治理/高级）+ 右栏实时摘要；底部 取消 / 重置 / 创建|保存（必填
// 未满足禁用）。编辑态 rclass/packageType 锁定（不可变是服务端 UpdateRepo
// 契约）。
//
// 字段语义与后端透传链严格对齐（T-95 起，零改动）：
// - governance（quotaBytes/includes/excludes）只在 LOCAL 仓呈现——
//   remote/virtual 的 config 被 repo.Service 规范化，未知字段丢弃，
//   表单不提供假入口。
// - 更新是 config 全量替换：编辑态从 GET 回显预填全部字段整体提交；
//   空值语义在字段提示里明示（includes 空 = 恢复默认 **/*，quota 0/空 =
//   不限——显式送 0 保持回显拼写稳定）。
// - remote password 永不回显（NFR-S14）；「留空保存 = 清除已存凭据」。
// - key/url 前端预检 + 服务端终裁：400 文案行内原样回显。
//
// 门（router.go / rbac.go 实测，CanManageRepo 语义）：
// - 创建（PUT 新 key 的 create 臂）= CapRepoWrite，仅全量 admin → 非
//   admin 直链预收敛为 L2 说明卡（省掉明知必 403 的请求）。
// - 编辑（POST / GET /{key}）走 CanManageRepo：admin 全量；readonly_admin
//   读通写拒（表单禁用 + repo-form-readonly-note，T-218 文案收口）；普通
//   user 持该仓 manage（m 动作）即可编辑——GET 通过即覆盖集内，403 → L2。
//   UI 不自行判定覆盖集（§7.10：403 驱动）。

const REMOTE_TTL_DEFAULTS = {
  retrievalCachePeriodSecs: '7200',
  missedRetrievalCachePeriodSecs: '1800',
  socketTimeoutSecs: '15',
  assumedOfflinePeriodSecs: '300',
}

const RCLASS_LABEL: Record<RClass, string> = { local: 'Local', remote: 'Remote', virtual: 'Virtual' }

/** 包类型网格五项（C7：现役五类，不照搬 33 项全集） */
const PKG_ITEMS: { id: PackageType; label: string; desc: string; icon: string }[] = [
  { id: 'generic', label: 'Generic', desc: '任意文件（curl 上传 / 下载）', icon: '◈' },
  { id: 'docker', label: 'Docker', desc: 'OCI 镜像（docker push / pull，仅 Local）', icon: '◆' },
  { id: 'maven', label: 'Maven', desc: 'JVM 构件（mvn deploy / 解析）', icon: '◾' },
  { id: 'npm', label: 'npm', desc: 'Node 包（npm publish / install）', icon: '◇' },
  { id: 'pypi', label: 'PyPI', desc: 'Python 包（twine / pip）', icon: '▫' },
]

interface FormState {
  rclass: RClass
  packageType: PackageType
  key: string
  description: string
  url: string
  username: string
  password: string
  allowPrivateUpstream: boolean
  retrievalCachePeriodSecs: string
  missedRetrievalCachePeriodSecs: string
  socketTimeoutSecs: string
  assumedOfflinePeriodSecs: string
  hardFail: boolean
  priorityResolution: boolean
  members: string[]
  defaultDeploymentRepo: string
  quotaBytes: string
  includesPattern: string
  excludesPattern: string
  handleReleases: boolean
  handleSnapshots: boolean
  checksumPolicyType: string
  snapshotVersionBehavior: string
}

const CREATE_INITIAL: FormState = {
  rclass: 'local',
  packageType: 'generic',
  key: '',
  description: '',
  url: '',
  username: '',
  password: '',
  allowPrivateUpstream: false,
  ...REMOTE_TTL_DEFAULTS,
  hardFail: false,
  priorityResolution: false,
  members: [],
  defaultDeploymentRepo: '',
  quotaBytes: '0',
  includesPattern: '',
  excludesPattern: '',
  handleReleases: true,
  handleSnapshots: true,
  checksumPolicyType: 'client-checksums',
  snapshotVersionBehavior: 'deployer',
}

function prefillFromDetail(d: {
  key: string
  rclass: string
  packageType: string
  description: string
  configuration?: Record<string, unknown>
}): FormState {
  const cfg = d.configuration
  const f: FormState = {
    ...CREATE_INITIAL,
    rclass: (RCLASSES as string[]).includes(d.rclass) ? (d.rclass as RClass) : 'local',
    packageType: (PACKAGE_TYPES as string[]).includes(d.packageType)
      ? (d.packageType as PackageType)
      : 'generic',
    key: d.key,
    description: d.description,
  }
  if (f.rclass === 'remote') {
    f.url = cfgStr(cfg, 'url')
    f.username = cfgStr(cfg, 'username')
    f.allowPrivateUpstream = cfgBool(cfg, 'allowPrivateUpstream')
    f.hardFail = cfgBool(cfg, 'hardFail')
    f.priorityResolution = cfgBool(cfg, 'priorityResolution')
    f.retrievalCachePeriodSecs = String(cfgNum(cfg, 'retrievalCachePeriodSecs') ?? 7200)
    f.missedRetrievalCachePeriodSecs = String(cfgNum(cfg, 'missedRetrievalCachePeriodSecs') ?? 1800)
    f.socketTimeoutSecs = String(cfgNum(cfg, 'socketTimeoutSecs') ?? 15)
    f.assumedOfflinePeriodSecs = String(cfgNum(cfg, 'assumedOfflinePeriodSecs') ?? 300)
  }
  if (f.rclass === 'virtual') {
    f.members = cfgStrList(cfg, 'repositories')
    f.defaultDeploymentRepo = cfgStr(cfg, 'defaultDeploymentRepo')
  }
  if (f.rclass === 'local') {
    f.priorityResolution = cfgBool(cfg, 'priorityResolution')
    f.includesPattern = cfgStr(cfg, 'includesPattern')
    f.excludesPattern = cfgStr(cfg, 'excludesPattern')
    f.quotaBytes = String(cfgNum(cfg, 'quotaBytes') ?? 0)
    if (f.packageType === 'maven') {
      f.handleReleases = cfgBool(cfg, 'handleReleases', true)
      f.handleSnapshots = cfgBool(cfg, 'handleSnapshots', true)
      f.checksumPolicyType = cfgStr(cfg, 'checksumPolicyType') || 'client-checksums'
      f.snapshotVersionBehavior = cfgStr(cfg, 'snapshotVersionBehavior') || 'deployer'
    }
  }
  return f
}

function buildBody(f: FormState, mode: 'create' | 'edit'): RepoConfigBody {
  const body: RepoConfigBody = {
    rclass: f.rclass,
    packageType: f.packageType,
    description: f.description.trim(),
  }
  if (mode === 'create') body.key = f.key.trim()
  if (f.rclass === 'remote') {
    body.url = f.url.trim()
    body.username = f.username.trim()
    if (f.password !== '') body.password = f.password
    body.allowPrivateUpstream = f.allowPrivateUpstream
    body.hardFail = f.hardFail
    body.priorityResolution = f.priorityResolution
    const nums: [keyof RepoConfigBody, string][] = [
      ['retrievalCachePeriodSecs', f.retrievalCachePeriodSecs],
      ['missedRetrievalCachePeriodSecs', f.missedRetrievalCachePeriodSecs],
      ['socketTimeoutSecs', f.socketTimeoutSecs],
      ['assumedOfflinePeriodSecs', f.assumedOfflinePeriodSecs],
    ]
    for (const [k, v] of nums) {
      if (v.trim() !== '' && /^\d+$/.test(v.trim())) {
        ;(body as unknown as Record<string, unknown>)[k] = Number(v.trim())
      }
    }
  }
  if (f.rclass === 'virtual') {
    body.repositories = [...f.members]
    body.defaultDeploymentRepo = f.defaultDeploymentRepo
  }
  if (f.rclass === 'local') {
    body.priorityResolution = f.priorityResolution
    if (f.packageType === 'maven') {
      body.handleReleases = f.handleReleases
      body.handleSnapshots = f.handleSnapshots
      body.checksumPolicyType = f.checksumPolicyType
      body.snapshotVersionBehavior = f.snapshotVersionBehavior
    }
    body.includesPattern = f.includesPattern.trim()
    body.excludesPattern = f.excludesPattern.trim()
    // 显式 0 = 不限（后端 quotaBytes 指针透传，0 会稳定回显）；空输入视为清除
    const q = f.quotaBytes.trim()
    body.quotaBytes = /^\d+$/.test(q) ? Number(q) : 0
  }
  return body
}

function isNonNegInt(v: string): boolean {
  return v.trim() === '' || /^\d+$/.test(v.trim())
}

/** 全表单门控：必填/预检不通过则提交不可用（表单零坏请求，§4.4） */
function formValid(f: FormState, mode: 'create' | 'edit'): { ok: boolean; reason?: string } {
  if (!comboAllowed(f.rclass, f.packageType)) {
    return { ok: false, reason: '该仓型 × 包类型组合不受支持（docker 仅 local）' }
  }
  if (mode === 'create') {
    if (f.key.trim() === '') return { ok: false, reason: 'Repository key 未填' }
    if (validateRepoKey(f.key.trim())) return { ok: false, reason: 'Repository key 不合规' }
  }
  if (f.rclass === 'remote') {
    if (f.url.trim() === '') return { ok: false, reason: '上游 URL 未填（remote 必填）' }
    if (validateUpstreamURL(f.url.trim())) return { ok: false, reason: '上游 URL 不合规' }
  }
  if (f.rclass === 'virtual' && f.members.length === 0) {
    return { ok: false, reason: 'virtual 仓至少需要一个成员' }
  }
  if (f.rclass === 'local' && !isNonNegInt(f.quotaBytes)) {
    return { ok: false, reason: 'quotaBytes 需为非负整数（字节）' }
  }
  if (f.rclass === 'remote') {
    for (const v of [
      f.retrievalCachePeriodSecs,
      f.missedRetrievalCachePeriodSecs,
      f.socketTimeoutSecs,
      f.assumedOfflinePeriodSecs,
    ]) {
      if (!isNonNegInt(v)) return { ok: false, reason: 'TTL/超时字段需为非负整数（秒）' }
    }
  }
  return { ok: true }
}

/** 建仓向导第 0 步：包类型网格对话框（§4.4 进页即弹；单选即选定关闭） */
function PackageTypeGrid({
  rclass,
  onPick,
  onCancel,
}: {
  rclass: RClass
  onPick: (pt: PackageType) => void
  onCancel: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    ref.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onCancel])

  return (
    <div className="modal-backdrop">
      <div
        ref={ref}
        tabIndex={-1}
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label="选择包类型"
        data-testid="pkg-grid"
      >
        <h2>选择包类型</h2>
        <p className="text-2">
          新建 <b>{RCLASS_LABEL[rclass]}</b> 仓库的第一步——包类型决定协议路由与客户端接入命令，创建后不可更改。
        </p>
        <div className="pkg-grid-items" role="radiogroup" aria-label="包类型">
          {PKG_ITEMS.map((p) => {
            const allowed = comboAllowed(rclass, p.id)
            return (
              <button
                type="button"
                key={p.id}
                role="radio"
                aria-checked={false}
                disabled={!allowed}
                title={allowed ? undefined : `${rclass} × ${p.id} 不受支持（docker 仅 local）`}
                data-testid={`pkg-grid-item-${p.id}`}
                onClick={() => onPick(p.id)}
              >
                <span className="pkg-icon" aria-hidden="true">
                  {p.icon}
                </span>
                <span className="pkg-name">{p.label}</span>
                <span className="pkg-desc">{allowed ? p.desc : `${p.desc}·不可用`}</span>
              </button>
            )
          })}
        </div>
        <div className="modal-actions">
          <button type="button" className="btn" data-testid="pkg-grid-cancel" onClick={onCancel}>
            取消
          </button>
        </div>
      </div>
    </div>
  )
}

export default function RepositoryFormPage({ mode }: { mode: 'create' | 'edit' }) {
  const { session } = useAuth()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  const role = normalizeAdminRole(session?.adminRole, session?.admin ?? false)
  const toast = useToast()
  const navigate = useNavigate()
  const { key: routeKey } = useParams<{ key: string }>()
  const [searchParams] = useSearchParams()

  // Quick 建仓入口的 ?rclass= 形态（shell §2.3 / §1.4 路由表）
  const initialRclass: RClass = mode === 'create'
    ? searchParams.get('rclass') === 'remote' || searchParams.get('rclass') === 'virtual'
      ? (searchParams.get('rclass') as RClass)
      : 'local'
    : 'local'

  const [f, setF] = useState<FormState>({ ...CREATE_INITIAL, rclass: initialRclass })
  const [baseline, setBaseline] = useState<FormState>({ ...CREATE_INITIAL, rclass: initialRclass })
  const [pkgOpen, setPkgOpen] = useState(mode === 'create')
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((prev) => ({ ...prev, [k]: v }))

  // 编辑态：加载现有配置并预填（全量替换语义的保全前提）
  const detail = useAsync(
    () => (mode === 'edit' && routeKey ? getRepoDetail(routeKey) : Promise.resolve(null)),
    [mode, routeKey],
  )
  useEffect(() => {
    if (mode !== 'edit') return
    if (detail.status === 'ok' && detail.data) {
      const prefilled = prefillFromDetail(detail.data)
      setF(prefilled)
      setBaseline(prefilled)
    }
  }, [mode, detail.status, detail.data])

  // virtual 成员候选：现存 local/remote 仓（virtual 不可嵌套；不含自身）
  const candidates = useAsync(getRepositories, [])
  const memberOptions = useMemo(() => {
    const list = candidates.data ?? []
    return list.filter((r) => r.type !== 'virtual' && r.key !== f.key)
  }, [candidates.data, f.key])

  // —— 门（§3.6 / CanManageRepo 语义，见文件头注）——
  if (mode === 'create' && !admin) {
    return (
      <div data-testid="repo-form-page">
        <div className="page-header">
          <h2>新建仓库</h2>
        </div>
        <EmptyState
          message="无权限"
          hint={
            readOnly
              ? '只读管理员（readonly_admin）为只读呈现态：创建仓库是管理面写操作（repo:write，仅全量 admin；服务端 403 兜底）。'
              : `创建仓库是管理员操作（repo:write）；当前用户 ${session?.username} 不是 admin。`
          }
        />
      </div>
    )
  }

  if (mode === 'edit') {
    if (detail.status === 'loading') {
      return (
        <div data-testid="repo-form-page">
          <Skeleton lines={8} />
        </div>
      )
    }
    if (detail.status === 'error' && detail.error) {
      return (
        <div data-testid="repo-form-page">
          {detail.error.status === 404 ? (
            <EmptyState
              message={`仓库 ${routeKey} 不存在`}
              action={
                <Link className="btn" to="/admin/repositories/local">
                  ← 返回仓库列表
                </Link>
              }
            />
          ) : (
            <ErrorCard error={detail.error} onRetry={detail.reload} />
          )}
        </div>
      )
    }
    if (detail.status === 'forbidden' && detail.error) {
      // CanManageRepo：普通 user 仅覆盖集内可读——403 即覆盖集外（§7.10）
      return (
        <div data-testid="repo-form-page">
          <EmptyState
            message="无权限管理此仓库"
            hint={
              role === 'user'
                ? `单仓配置管理需要该仓的 manage 动作（permission target 授权）；${detail.error.message}`
                : detail.error.message
            }
            action={
              <Link className="btn" to="/admin/repositories/local">
                ← 返回仓库列表
              </Link>
            }
          />
        </div>
      )
    }
  }

  const gate = formValid(f, mode)
  const keyErr = mode === 'create' ? validateRepoKey(f.key.trim()) : null
  const urlErr = f.rclass === 'remote' ? validateUpstreamURL(f.url.trim()) : null
  // readonly_admin：GET 通过但写面必 403——全字段禁用 + 注记（M7 §7.3）
  const locked = readOnly
  const canSubmit = gate.ok && !submitting && !locked

  const doSubmit = async () => {
    setServerError(null)
    setSubmitting(true)
    try {
      const key = mode === 'create' ? f.key.trim() : (routeKey ?? '')
      const text =
        mode === 'create' ? await createRepo(key, buildBody(f, mode)) : await updateRepo(key, buildBody(f, mode))
      toast.success(text) // 服务端文案原样（"Successfully created repository '<key>'"）
      navigate(`/admin/repositories/${key}`)
    } catch (err) {
      // 400 校验文案（key/url/组合矩阵/成员规则）与 409/413 治理拒绝语义：
      // message 原样行内呈现（派单要求）；readonly/m 覆盖集外的 403 同形
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  const moveMember = (idx: number, delta: -1 | 1) => {
    const next = [...f.members]
    const j = idx + delta
    if (j < 0 || j >= next.length) return
    ;[next[idx], next[j]] = [next[j], next[idx]]
    set('members', next)
  }

  const localMembers = f.members.filter((m) => memberOptions.find((o) => o.key === m)?.type === 'local')

  const renderSection = (): ReactNode => {
    return (
      <>
        <section className="repo-form-section" aria-label="常规设置">
          <h3>常规设置</h3>
          <div className="radio-row" role="radiogroup" aria-label="仓型">
            {RCLASSES.map((rc) => (
              <label key={rc} className={mode === 'edit' ? 'disabled' : ''}>
                <input
                  type="radio"
                  name="rclass"
                  value={rc}
                  checked={f.rclass === rc}
                  disabled={mode === 'edit' || locked}
                  onChange={() => {
                    set('rclass', rc)
                    if (rc !== 'local' && f.packageType === 'docker') set('packageType', 'generic')
                  }}
                  data-testid={`form-rclass-${rc}`}
                />
                {rc === 'local' ? 'Local（本地存储）' : rc === 'remote' ? 'Remote（代理上游）' : 'Virtual（聚合）'}
              </label>
            ))}
          </div>
          <div className="radio-row" role="radiogroup" aria-label="包类型">
            {PACKAGE_TYPES.map((pt) => {
              const allowed = comboAllowed(f.rclass, pt)
              return (
                <label
                  key={pt}
                  className={allowed ? '' : 'disabled'}
                  title={allowed ? undefined : `${f.rclass} × ${pt} 不受支持（docker 仅 local）`}
                >
                  <input
                    type="radio"
                    name="packageType"
                    value={pt}
                    checked={f.packageType === pt}
                    disabled={mode === 'edit' || !allowed || locked}
                    onChange={() => set('packageType', pt)}
                    data-testid={`form-package-${pt}`}
                  />
                  {pt}
                </label>
              )
            })}
          </div>
          {mode === 'edit' && (
            <p className="field-note">仓型与包类型不可修改（变更会静默改变全部协议路由决策）。</p>
          )}
          {mode === 'create' && (
            <div className="field">
              <label htmlFor="f-key">Repository key *</label>
              <input
                id="f-key"
                className="mono-input"
                value={f.key}
                onChange={(e) => set('key', e.target.value)}
                placeholder="maven-remote"
                aria-invalid={!!keyErr}
                data-testid="form-key"
                lang="en"
                disabled={locked}
              />
              {keyErr ? (
                <p className="field-error" data-testid="form-key-error" role="alert">
                  {keyErr}
                </p>
              ) : f.key.trim() !== '' ? (
                <p className="field-hint" style={{ color: 'var(--bf-success)' }} data-testid="form-key-ok">
                  ✓ 可用
                </p>
              ) : (
                <p className="field-hint">规则 [a-z][a-z0-9-]{'{1,62}'}，共 2~63 字符；服务端终裁。</p>
              )}
            </div>
          )}
          {mode === 'edit' && (
            <div className="kv">
              <span className="k">Repository key</span>
              <span className="mono" lang="en">
                {routeKey}
              </span>
            </div>
          )}
          <div className="field">
            <label htmlFor="f-desc">描述</label>
            <textarea
              id="f-desc"
              value={f.description}
              onChange={(e) => set('description', e.target.value)}
              placeholder="用途、负责人、团队…"
              data-testid="form-description"
              disabled={locked}
            />
          </div>
        </section>

        {f.rclass === 'remote' && (
          <section className="repo-form-section" aria-label="来源">
            <h3>来源（Remote）</h3>
            <div className="field">
              <label htmlFor="f-url">上游 URL *</label>
              <input
                id="f-url"
                className="mono-input"
                value={f.url}
                onChange={(e) => set('url', e.target.value)}
                placeholder="https://repo1.maven.org/maven2"
                aria-invalid={!!urlErr}
                data-testid="form-url"
                lang="en"
                disabled={locked}
              />
              {urlErr ? (
                <p className="field-error" data-testid="form-url-error" role="alert">
                  {urlErr}
                </p>
              ) : (
                <p className="field-hint">http/https 上游基址；私网地址合法（SSRF 链按请求校验）。</p>
              )}
            </div>
            <div className="field">
              <label htmlFor="f-user">用户名（上游认证，可选）</label>
              <input
                id="f-user"
                value={f.username}
                onChange={(e) => set('username', e.target.value)}
                data-testid="form-username"
                disabled={locked}
              />
            </div>
            <div className="field">
              <label htmlFor="f-pass">密码（上游认证，可选）</label>
              <input
                id="f-pass"
                type="password"
                autoComplete="new-password"
                value={f.password}
                onChange={(e) => set('password', e.target.value)}
                placeholder="永不回显"
                data-testid="form-password"
                disabled={locked}
              />
              <p className="field-hint">
                密码不回显（NFR-S14）。注意：保存为<b>全量替换</b>语义——留空保存会
                <b>清除</b>已存凭据；需要保留请重新输入。
              </p>
            </div>
            <label className="check-row">
              <input
                type="checkbox"
                checked={f.allowPrivateUpstream}
                onChange={(e) => set('allowPrivateUpstream', e.target.checked)}
                data-testid="form-allow-private"
                disabled={locked}
              />
              允许私网上游（allowPrivateUpstream）
            </label>
            {f.allowPrivateUpstream && (
              <div className="warn-box" data-testid="form-private-warn">
                ⚠ 已放行私网上游：SSRF 防线对该仓放宽，变更会记录审计（NFR-S14）。
              </div>
            )}
          </section>
        )}

        {f.rclass === 'virtual' && (
          <section className="repo-form-section" aria-label="成员">
            <h3>成员（Virtual）</h3>
            <div className="field">
              <label>成员（可多选，↑↓ 调整解析顺序）</label>
              {candidates.status === 'loading' && <Skeleton lines={3} />}
              {candidates.status !== 'loading' && memberOptions.length === 0 && (
                <p className="field-hint">没有可用的 local/remote 仓可作为成员——先创建成员仓库。</p>
              )}
              {memberOptions.length > 0 && (
                <div className="member-pick" data-testid="form-member-pick">
                  {memberOptions.map((o: RepoListItem) => (
                    <label key={o.key}>
                      <input
                        type="checkbox"
                        checked={f.members.includes(o.key)}
                        onChange={(e) => {
                          if (e.target.checked) {
                            set('members', [...f.members, o.key])
                          } else {
                            set('members', f.members.filter((m) => m !== o.key))
                            // review B2：被取消的成员恰是默认部署仓时联动清空，
                            // 否则 state 残留旧值、select 显示空白，提交吃服务端
                            // 400 "not a member"（service.go validateVirtualMembers）
                            if (f.defaultDeploymentRepo === o.key) set('defaultDeploymentRepo', '')
                          }
                        }}
                        data-testid={`form-member-${o.key}`}
                        disabled={locked}
                      />
                      <span className="mono" lang="en">
                        {o.key}
                      </span>
                      <span className="badge neutral">{o.type}</span>
                      {cfgBool(o.configuration, 'priorityResolution') && (
                        <span className="badge warning">优先解析</span>
                      )}
                    </label>
                  ))}
                </div>
              )}
            </div>
            {f.members.length > 0 && (
              <div className="field">
                <label>已选成员（按解析顺序）</label>
                <div className="chip-list" data-testid="form-member-order">
                  {f.members.map((m, i) => (
                    <div key={m} className="chip-item">
                      <span className="idx">{i + 1}</span>
                      <span className="mono" lang="en">
                        {m}
                      </span>
                      <span className="chip-btns">
                        <button
                          type="button"
                          aria-label={`上移 ${m}`}
                          disabled={i === 0 || locked}
                          onClick={() => moveMember(i, -1)}
                          data-testid={`member-up-${i}`}
                        >
                          ↑
                        </button>
                        <button
                          type="button"
                          aria-label={`下移 ${m}`}
                          disabled={i === f.members.length - 1 || locked}
                          onClick={() => moveMember(i, 1)}
                          data-testid={`member-down-${i}`}
                        >
                          ↓
                        </button>
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}
            <div className="field">
              <label htmlFor="f-deploy">默认部署仓库（可选，仅 local 成员）</label>
              <select
                id="f-deploy"
                value={f.defaultDeploymentRepo}
                onChange={(e) => set('defaultDeploymentRepo', e.target.value)}
                disabled={localMembers.length === 0 || locked}
                data-testid="form-default-deploy"
              >
                <option value="">（未配置——写操作将返回 405）</option>
                {localMembers.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
              {f.defaultDeploymentRepo && (
                <p className="field-hint">经此 virtual 仓的部署将写入 {f.defaultDeploymentRepo}。</p>
              )}
            </div>
          </section>
        )}

        {f.rclass === 'local' && f.packageType === 'maven' && (
          <section className="repo-form-section" aria-label="Maven 策略">
            <h3>Maven 策略</h3>
            <label className="check-row">
              <input
                type="checkbox"
                checked={f.handleReleases}
                onChange={(e) => set('handleReleases', e.target.checked)}
                data-testid="form-handle-releases"
                disabled={locked}
              />
              接受 release 部署（handleReleases）
            </label>
            <label className="check-row">
              <input
                type="checkbox"
                checked={f.handleSnapshots}
                onChange={(e) => set('handleSnapshots', e.target.checked)}
                data-testid="form-handle-snapshots"
                disabled={locked}
              />
              接受 SNAPSHOT 部署（handleSnapshots）
            </label>
            <div className="field">
              <label htmlFor="f-checksum">checksum 策略</label>
              <select
                id="f-checksum"
                value={f.checksumPolicyType}
                onChange={(e) => set('checksumPolicyType', e.target.value)}
                data-testid="form-checksum-policy"
                disabled={locked}
              >
                <option value="client-checksums">client-checksums（客户端声明严格校验，默认）</option>
                <option value="server-generated-checksums">server-generated-checksums（服务端实测覆盖）</option>
              </select>
            </div>
            <div className="field">
              <label htmlFor="f-snapshot">SNAPSHOT 行为</label>
              <select
                id="f-snapshot"
                value={f.snapshotVersionBehavior}
                onChange={(e) => set('snapshotVersionBehavior', e.target.value)}
                data-testid="form-snapshot-behavior"
                disabled={locked}
              >
                <option value="deployer">deployer（按上传名存储，默认）</option>
                <option value="non-unique">non-unique</option>
                <option value="unique">unique（unique 改写为 P2，行为同 deployer）</option>
              </select>
            </div>
          </section>
        )}

        {f.rclass === 'local' && (
          <section className="repo-form-section" aria-label="治理">
            <h3>治理（governance）</h3>
            <div className="field">
              <label htmlFor="f-quota">配额 quotaBytes（字节）</label>
              <input
                id="f-quota"
                className="mono-input"
                inputMode="numeric"
                value={f.quotaBytes}
                onChange={(e) => set('quotaBytes', e.target.value)}
                aria-invalid={!isNonNegInt(f.quotaBytes)}
                data-testid="form-quota"
                disabled={locked}
              />
              <p className="field-hint">正整数；0 = 不限（默认）。超限写入收到 413（message 含 used/quota）。</p>
            </div>
            <div className="field">
              <label htmlFor="f-includes">includesPattern</label>
              <input
                id="f-includes"
                className="mono-input"
                value={f.includesPattern}
                onChange={(e) => set('includesPattern', e.target.value)}
                placeholder="**/*"
                data-testid="form-includes"
                lang="en"
                disabled={locked}
              />
              <p className="field-hint">逗号分隔多值；留空 / **/* = 匹配全部路径（保存为全量替换）。</p>
            </div>
            <div className="field">
              <label htmlFor="f-excludes">excludesPattern</label>
              <input
                id="f-excludes"
                className="mono-input"
                value={f.excludesPattern}
                onChange={(e) => set('excludesPattern', e.target.value)}
                placeholder="（无）"
                data-testid="form-excludes"
                lang="en"
                disabled={locked}
              />
              <p className="field-hint">
                exclude 优先于 include；留空 = 无排除。不匹配 includes 或命中 excludes 的上传收到 409（message 含双
                pattern）。
              </p>
            </div>
          </section>
        )}

        <section className="repo-form-section" aria-label="高级">
          <h3>高级</h3>
          {f.rclass === 'remote' && (
            <>
              <p className="section-sub">缓存与超时（秒）——产品默认 7200 / 1800 / 15 / 300。</p>
              {(
                [
                  ['retrievalCachePeriodSecs', '命中缓存 TTL'],
                  ['missedRetrievalCachePeriodSecs', '未命中负缓存 TTL'],
                  ['socketTimeoutSecs', 'socket 超时'],
                  ['assumedOfflinePeriodSecs', 'assumed-offline 静默期'],
                ] as const
              ).map(([k, label]) => (
                <div className="field" key={k}>
                  <label htmlFor={`f-${k}`}>{label}</label>
                  <input
                    id={`f-${k}`}
                    className="mono-input"
                    inputMode="numeric"
                    value={f[k]}
                    onChange={(e) => set(k, e.target.value)}
                    aria-invalid={!isNonNegInt(f[k])}
                    data-testid={`form-${k}`}
                    disabled={locked}
                  />
                </div>
              ))}
              <label className="check-row">
                <input
                  type="checkbox"
                  checked={f.hardFail}
                  onChange={(e) => set('hardFail', e.target.checked)}
                  data-testid="form-hard-fail"
                  disabled={locked}
                />
                hardFail（上游故障时直接失败，不降级）
              </label>
            </>
          )}
          <label className="check-row">
            <input
              type="checkbox"
              checked={f.priorityResolution}
              onChange={(e) => set('priorityResolution', e.target.checked)}
              data-testid="form-priority"
              disabled={locked}
            />
            优先解析（priorityResolution：作为 virtual 成员时优先桶标记）
          </label>
        </section>
      </>
    )
  }

  const summaryRows: [string, ReactNode][] = [
    ['key', <span className="mono" lang="en">{mode === 'create' ? f.key || '—' : (routeKey ?? '—')}</span>],
    ['rclass', f.rclass],
    ['packageType', f.packageType],
    ...(f.rclass === 'remote'
      ? ([
          ['url', <span className="mono" lang="en">{f.url || '—'}</span>],
          ['TTL', <span className="mono">{f.retrievalCachePeriodSecs || '—'}s</span>],
        ] as [string, ReactNode][])
      : []),
    ...(f.rclass === 'virtual'
      ? ([['成员', `${f.members.length} 个`]] as [string, ReactNode][])
      : []),
    ...(f.rclass === 'local'
      ? ([
          ['quotaBytes', <span className="mono">{f.quotaBytes || '0'}{f.quotaBytes === '0' ? '（不限）' : ''}</span>],
          ['patterns', <span className="mono" lang="en">{f.includesPattern || '**/*'} ; {f.excludesPattern || '(none)'}</span>],
        ] as [string, ReactNode][])
      : []),
  ]

  return (
    <div data-testid="repo-form-page">
      <div className="page-header">
        <h2>{mode === 'create' ? `新建 ${RCLASS_LABEL[f.rclass]} 仓库` : `编辑 ${routeKey}`}</h2>
      </div>

      {locked && (
        <p className="page-note" data-testid="repo-form-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：仓库配置只读——保存走单仓管理面写（CanManageRepo
          write），服务端 403 兜底。
        </p>
      )}

      <div className="form-layout">
        <section>
          {renderSection()}
          {serverError && (
            <div className="form-error" data-testid="form-error" role="alert">
              <div className="headline">
                保存失败（HTTP {serverError.status || '网络'}）
              </div>
              <div className="raw" lang="en">
                {serverError.message}
              </div>
            </div>
          )}
          <div className="form-actions">
            <button
              type="button"
              className="btn"
              onClick={() => navigate(mode === 'create' ? `/admin/repositories/${f.rclass}` : `/admin/repositories/${routeKey}`)}
              data-testid="form-cancel"
            >
              取消
            </button>
            <button
              type="button"
              className="btn"
              onClick={() => {
                setF(baseline)
                setServerError(null)
              }}
              data-testid="form-reset"
            >
              重置
            </button>
            <button
              type="button"
              className="btn primary"
              disabled={!canSubmit}
              title={locked ? '只读管理员不可写（服务端 403 兜底）' : gate.reason}
              onClick={() => void doSubmit()}
              data-testid="form-submit"
            >
              {submitting ? '保存中…' : mode === 'create' ? `创建 ${RCLASS_LABEL[f.rclass]} 仓库` : '保存'}
            </button>
          </div>
        </section>

        <aside className="card summary-box" data-testid="form-summary">
          <h3>摘要（实时）</h3>
          {summaryRows.map(([k, v]) => (
            <div className="kv" key={k}>
              <span className="k">{k}</span>
              <span>{v}</span>
            </div>
          ))}
        </aside>
      </div>

      {pkgOpen && mode === 'create' && (
        <PackageTypeGrid
          rclass={f.rclass}
          onPick={(pt) => {
            // 网格选定的包类型进入基线（重置不退回进页默认 generic）
            setF((prev) => ({ ...prev, packageType: pt }))
            setBaseline((prev) => ({ ...prev, packageType: pt }))
            setPkgOpen(false)
          }}
          onCancel={() => navigate(`/admin/repositories/${f.rclass}`)}
        />
      )}
    </div>
  )
}
