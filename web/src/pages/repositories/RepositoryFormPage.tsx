import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, errText, getRepositories } from '../../lib/api'
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

// 建仓/编辑表单（console-ux §4.4：三步 + 摘要侧栏；编辑复用同一组件，
// 步骤 1 锁定 rclass/packageType——二者不可变是服务端 UpdateRepo 契约）。
//
// 字段语义与后端透传链严格对齐（T-95）：
// - governance（quotaBytes/includes/excludes）只在 LOCAL 仓呈现——
//   remote/virtual 的 config 被 repo.Service 规范化，未知字段丢弃
//   （T-95 遗留⑤），表单不提供假入口。
// - 更新是 config 全量替换：编辑态从 GET 回显预填全部字段整体提交；
//   空值语义在字段提示里明示（includes 空 = 恢复默认 **/*，quota 0/空 =
//   不限——显式送 0 保持回显拼写稳定，后端 quotaBytes 为指针透传）。
// - remote password 永不回显（NFR-S14）；PUT 全量替换语义下「留空保存
//   = 清除已存凭据」，字段提示明示（无「保持凭据」API 形态，见日志）。
// - key/url 前端预检 + 服务端终裁：400 文案行内原样回显
//   （FR-24-AC5：缺 url 的 remote 前端即拦，零写请求——步骤门控兜底）。

const REMOTE_TTL_DEFAULTS = {
  retrievalCachePeriodSecs: '7200',
  missedRetrievalCachePeriodSecs: '1800',
  socketTimeoutSecs: '15',
  assumedOfflinePeriodSecs: '300',
}

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

/** 步骤门控：当前步骤的校验不通过则「下一步/提交」不可用（表单零坏请求） */
function stepValid(step: number, f: FormState, mode: 'create' | 'edit'): { ok: boolean; reason?: string } {
  if (step === 1) {
    if (!comboAllowed(f.rclass, f.packageType)) {
      return { ok: false, reason: '该仓型 × 包类型组合不受支持（docker 仅 local）' }
    }
    return { ok: true }
  }
  if (step === 2) {
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
    return { ok: true }
  }
  // step 3
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

const STEP_TITLES = ['类型', '标识与来源', '策略']

export default function RepositoryFormPage({ mode }: { mode: 'create' | 'edit' }) {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const toast = useToast()
  const navigate = useNavigate()
  const { key: routeKey } = useParams<{ key: string }>()

  const [step, setStep] = useState(1)
  const [f, setF] = useState<FormState>(CREATE_INITIAL)
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((prev) => ({ ...prev, [k]: v }))

  // 编辑态：加载现有配置并预填（全量替换语义的保全前提）
  const detail = useAsync(
    () => (mode === 'edit' && routeKey ? getRepoDetail(routeKey) : Promise.resolve(null)),
    [mode, routeKey],
  )
  useEffect(() => {
    if (detail.status === 'ok' && detail.data) setF(prefillFromDetail(detail.data))
  }, [detail.status, detail.data])

  // virtual 成员候选：现存 local/remote 仓（virtual 不可嵌套；不含自身）
  const candidates = useAsync(getRepositories, [])
  const memberOptions = useMemo(() => {
    const list = candidates.data ?? []
    return list.filter((r) => r.type !== 'virtual' && r.key !== f.key)
  }, [candidates.data, f.key])

  if (!admin) {
    return (
      <div data-testid="repo-form-page">
        <div className="page-header">
          <h2>{mode === 'create' ? '创建仓库' : '仓库设置'}</h2>
        </div>
        <EmptyState
          message="无权限"
          hint={`仓库${mode === 'create' ? '创建' : '配置修改'}是管理员操作；当前用户 ${session?.username} 不是 admin。`}
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
                <Link className="btn" to="/repositories">
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
      return (
        <div data-testid="repo-form-page">
          <EmptyState message="无权限读取仓库配置" hint={detail.error.message} />
        </div>
      )
    }
  }

  const gate = stepValid(step, f, mode)
  const keyErr = mode === 'create' ? validateRepoKey(f.key.trim()) : null
  const urlErr = f.rclass === 'remote' ? validateUpstreamURL(f.url.trim()) : null

  const doSubmit = async () => {
    setServerError(null)
    setSubmitting(true)
    try {
      const key = mode === 'create' ? f.key.trim() : (routeKey ?? '')
      const text =
        mode === 'create' ? await createRepo(key, buildBody(f, mode)) : await updateRepo(key, buildBody(f, mode))
      toast.success(text) // 服务端文案原样（"Successfully created repository '<key>'"）
      navigate(`/repositories/${key}`)
    } catch (err) {
      // 400 校验文案（key/url/组合矩阵/成员规则）与 409/413 治理拒绝语
      // 义：message 原样行内呈现（派单要求）
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

  const renderStep = (): ReactNode => {
    if (step === 1) {
      return (
        <fieldset>
          <legend className="field-label">仓型与包类型{mode === 'edit' ? '（编辑态锁定，二者不可变）' : ''}</legend>
          <div className="radio-row" role="radiogroup" aria-label="仓型">
            {RCLASSES.map((rc) => (
              <label key={rc} className={mode === 'edit' ? 'disabled' : ''}>
                <input
                  type="radio"
                  name="rclass"
                  value={rc}
                  checked={f.rclass === rc}
                  disabled={mode === 'edit'}
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
                    disabled={mode === 'edit' || !allowed}
                    onChange={() => set('packageType', pt)}
                    data-testid={`form-package-${pt}`}
                  />
                  {pt}
                </label>
              )
            })}
          </div>
          {mode === 'edit' && <p className="field-note">仓型与包类型不可修改（变更会静默改变全部协议路由决策）。</p>}
        </fieldset>
      )
    }

    if (step === 2) {
      return (
        <>
          {mode === 'create' && (
            <div className="field">
              <label htmlFor="f-key">Repository key</label>
              <input
                id="f-key"
                className="mono-input"
                value={f.key}
                onChange={(e) => set('key', e.target.value)}
                placeholder="maven-remote"
                aria-invalid={!!keyErr}
                data-testid="form-key"
                lang="en"
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
          <div className="field">
            <label htmlFor="f-desc">描述</label>
            <textarea
              id="f-desc"
              value={f.description}
              onChange={(e) => set('description', e.target.value)}
              placeholder="用途、负责人、团队…"
              data-testid="form-description"
            />
          </div>

          {f.rclass === 'remote' && (
            <>
              <div className="field">
                <label htmlFor="f-url">上游 URL（必填）</label>
                <input
                  id="f-url"
                  className="mono-input"
                  value={f.url}
                  onChange={(e) => set('url', e.target.value)}
                  placeholder="https://repo1.maven.org/maven2"
                  aria-invalid={!!urlErr}
                  data-testid="form-url"
                  lang="en"
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
                />
                允许私网上游（allowPrivateUpstream）
              </label>
              {f.allowPrivateUpstream && (
                <div className="warn-box" data-testid="form-private-warn">
                  ⚠ 已放行私网上游：SSRF 防线对该仓放宽，变更会记录审计（NFR-S14）。
                </div>
              )}
            </>
          )}

          {f.rclass === 'virtual' && (
            <>
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
                            disabled={i === 0}
                            onClick={() => moveMember(i, -1)}
                            data-testid={`member-up-${i}`}
                          >
                            ↑
                          </button>
                          <button
                            type="button"
                            aria-label={`下移 ${m}`}
                            disabled={i === f.members.length - 1}
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
                  disabled={localMembers.length === 0}
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
            </>
          )}
        </>
      )
    }

    // step 3：策略（按 rclass × packageType 渲染）
    return (
      <>
        {f.rclass === 'local' && (
          <>
            <h3 className="section-title">治理（governance）</h3>
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
              />
              <p className="field-hint">exclude 优先于 include；留空 = 无排除。不匹配 includes 或命中 excludes 的上传收到 409（message 含双 pattern）。</p>
            </div>

            {f.packageType === 'maven' && (
              <>
                <h3 className="section-title">Maven 策略</h3>
                <label className="check-row">
                  <input
                    type="checkbox"
                    checked={f.handleReleases}
                    onChange={(e) => set('handleReleases', e.target.checked)}
                    data-testid="form-handle-releases"
                  />
                  接受 release 部署（handleReleases）
                </label>
                <label className="check-row">
                  <input
                    type="checkbox"
                    checked={f.handleSnapshots}
                    onChange={(e) => set('handleSnapshots', e.target.checked)}
                    data-testid="form-handle-snapshots"
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
                  >
                    <option value="deployer">deployer（按上传名存储，默认）</option>
                    <option value="non-unique">non-unique</option>
                    <option value="unique">unique（unique 改写为 P2，行为同 deployer）</option>
                  </select>
                </div>
              </>
            )}
          </>
        )}

        {f.rclass === 'remote' && (
          <>
            <h3 className="section-title">缓存与超时</h3>
            {(
              [
                ['retrievalCachePeriodSecs', '命中缓存 TTL（秒）'],
                ['missedRetrievalCachePeriodSecs', '未命中负缓存 TTL（秒）'],
                ['socketTimeoutSecs', 'socket 超时（秒）'],
                ['assumedOfflinePeriodSecs', 'assumed-offline 静默期（秒）'],
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
                />
                <p className="field-hint">产品默认 7200 / 1800 / 15 / 300。</p>
              </div>
            ))}
            <label className="check-row">
              <input
                type="checkbox"
                checked={f.hardFail}
                onChange={(e) => set('hardFail', e.target.checked)}
                data-testid="form-hard-fail"
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
          />
          优先解析（priorityResolution：作为 virtual 成员时优先桶标记）
        </label>
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
        <h2>{mode === 'create' ? '创建仓库' : `仓库设置 · ${routeKey}`}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          步骤 {step} / 3
        </span>
      </div>

      <div className="stepper" aria-label={`步骤 ${step} / 3`}>
        {STEP_TITLES.map((t, i) => (
          <span key={t} style={{ display: 'contents' }}>
            {i > 0 && <span className="sep" />}
            <span className={`step-label${i + 1 === step ? ' now' : i + 1 < step ? ' done' : ''}`}>
              <span className="dot" aria-hidden="true" />
              {t}
            </span>
          </span>
        ))}
      </div>

      <div className="form-layout">
        <section className="card">
          {renderStep()}
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
            {mode === 'edit' && (
              <button type="button" className="btn" onClick={() => navigate(`/repositories/${routeKey}`)}>
                取消
              </button>
            )}
            {step > 1 && (
              <button
                type="button"
                className="btn"
                onClick={() => setStep((s) => s - 1)}
                data-testid="form-prev"
              >
                上一步
              </button>
            )}
            {step < 3 && (
              <button
                type="button"
                className="btn primary"
                disabled={!gate.ok}
                title={gate.reason}
                onClick={() => setStep((s) => s + 1)}
                data-testid="form-next"
              >
                下一步 →
              </button>
            )}
            {step === 3 && (
              <button
                type="button"
                className="btn primary"
                disabled={!gate.ok || submitting}
                title={gate.reason}
                onClick={() => void doSubmit()}
                data-testid="form-submit"
              >
                {submitting ? '保存中…' : mode === 'create' ? '创建仓库' : '保存变更'}
              </button>
            )}
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
    </div>
  )
}
