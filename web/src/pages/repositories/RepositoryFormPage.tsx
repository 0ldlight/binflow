import { useEffect, useMemo, useState } from 'react'
import type { ComponentPropsWithoutRef, ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControlLabel from '@mui/material/FormControlLabel'
import IconButton from '@mui/material/IconButton'
import Paper from '@mui/material/Paper'
import Radio from '@mui/material/Radio'
import Select from '@mui/material/Select'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { PkgIcon } from '../../components/PkgIcon'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, getRepositories, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import type { RepoListItem } from '../../lib/api'
import Chip from '@mui/material/Chip'
import { getAddons, lockedHint, packageTypeOptions, tierBadgeClass } from '../../lib/addons'
import type { PkgTypeOption } from '../../lib/addons'
import {
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
import {
  POLICY_FIELDS,
  initialPolicyForm,
  policyBodyEntries,
  policyNumberValid,
  policyPkg,
  prefillPolicyForm,
} from './policyFields'
import type { PolicyForm } from './policyFields'
import ReplicationsSection from './ReplicationsSection'

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
//
// T-299 批次一：控件层迁 MUI（TextField/Radio/Checkbox/原生 select 保留在
// MUI 组合内/FormControlLabel/Button/IconButton/Alert），密度与配色经
// lib/muiAtoms 收口。交互逻辑零变化：锚点全部落在 input/select/button 本体
// （fill/check/selectOption/click 直达）；radiogroup/label 结构保持
// （m10 的 `label:has(input)` 断言形态）；键位、门控、预填与提交链路不动。

const REMOTE_TTL_DEFAULTS = {
  retrievalCachePeriodSecs: '7200',
  missedRetrievalCachePeriodSecs: '1800',
  socketTimeoutSecs: '15',
  assumedOfflinePeriodSecs: '300',
}

const RCLASS_LABEL: Record<RClass, string> = { local: 'Local', remote: 'Remote', virtual: 'Virtual' }

/** 包类型网格五核心静态元数据（C7；门控型 go/nuget/cargo 等自 addons API
 *  动态并入——M10 T-288：可选集与徽章/锁定态都吃注册表实时数据，前端不
 *  复制槽位清单。图标不在元数据里——PkgIcon 按 id 解析，T-390 起几何
 *  字符图标族（五核心 + 门控通用星形）退役） */
const PKG_ITEMS: { id: PackageType; label: string; desc: string }[] = [
  { id: 'generic', label: 'Generic', desc: '任意文件（curl 上传 / 下载）' },
  { id: 'docker', label: 'Docker', desc: 'OCI 镜像（docker push / pull，仅 Local）' },
  { id: 'maven', label: 'Maven', desc: 'JVM 构件（mvn deploy / 解析）' },
  { id: 'npm', label: 'npm', desc: 'Node 包（npm publish / install）' },
  { id: 'pypi', label: 'PyPI', desc: 'Python 包（twine / pip）' },
]

/** 策略键分组标题（T-353 字段册的呈现面） */
const POLICY_GROUP_TITLE = { debian: 'Deb 索引策略', rpm: 'RPM 索引策略', helm: 'Helm 强制布局' } as const

/** 建仓面的包型可选集（M10 T-288）：五核心静态项 + addons API 的门控槽位。
 *  加载中/请求失败 = 仅五核心（community 地板恒合法；门控型缺席不误放，
 *  服务端 D3 建仓门终裁——UI 预收敛而已）。 */
interface PkgChoice {
  id: PackageType
  label: string
  desc: string
  /** addons 槽位行（五核心在注册表栈上有行；undefined = 无行，按地板放行） */
  opt?: PkgTypeOption
}

function buildPkgChoices(options: PkgTypeOption[]): PkgChoice[] {
  const items: PkgChoice[] = PKG_ITEMS.map((p) => ({ ...p, opt: options.find((o) => o.id === p.id) }))
  for (const o of options) {
    if (PKG_ITEMS.some((p) => p.id === o.id)) continue
    items.push({ id: o.id as PackageType, label: o.displayName, desc: o.description, opt: o })
  }
  return items
}

/** 单项可选取舍：组合矩阵（docker 仅 local）× 槽位解锁态；返回禁用原因
 *  （null = 可选）。槽位禁用优先呈现（addons.disabled 熔断高于组合约束的
 *  信息量——它对 admin 是可行动的）。 */
function pkgChoiceBlock(rclass: RClass, c: PkgChoice): string | null {
  if (c.opt && !c.opt.enabled) return lockedHint(c.opt)
  if (!comboAllowed(rclass, c.id)) return `${rclass} × ${c.id} 不受支持（docker 仅 local）`
  return null
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
  /** deb/rpm/helm 策略键（T-353 字段册驱动；generic 等其余包型 = 空对象） */
  policy: PolicyForm
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
  policy: {},
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
    // M10 T-288：wire 值由服务端注册表终裁过（存量仓的 packageType 必为
    // 已装配槽位），直接收窄——旧「五核心静态枚举守卫」会把门控仓（go
    // 等）静默改写成 generic，全量替换提交即错仓型。
    packageType: (d.packageType || 'generic') as PackageType,
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
    // deb/rpm/helm 策略键逐键回显（全量替换提交的保全前提——漏发=丢配置）
    const pkg = policyPkg(f.packageType)
    if (pkg) f.policy = prefillPolicyForm(pkg, cfg)
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
    // deb/rpm/helm 策略键（T-353）：check/合法 number 恒进 body（POINTER 语义
    // ——显式 false/0 必须过 round trip），空 text 剔除归默认
    const pkg = policyPkg(f.packageType)
    if (pkg) Object.assign(body as unknown as Record<string, unknown>, policyBodyEntries(pkg, f.policy))
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
  if (f.rclass === 'local') {
    const pkg = policyPkg(f.packageType)
    if (pkg && !policyNumberValid(pkg, f.policy)) {
      return { ok: false, reason: '策略键的数值字段需为非负整数（historyCycles / yumRootDepth）' }
    }
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

/** 建仓向导第 0 步：包类型网格对话框（§4.4 进页即弹；单选即选定关闭）。
 *  M10 T-288：每型带档位徽章（community 地板无徽章；pro/enterprise 徽章），
 *  未解锁型禁用 + 提示「需要 N 档」——D5 可见性口径（入口可见带徽章，
 *  不是隐藏）。
 *  T-299：网格项保持原生 button（radiogroup 语义 + pkg-grid 卡片形态），
 *  取消钮迁 MUI；焦点/Esc 管理零变化。
 *  T-344 批 D：modal 壳 → MUI Dialog（.modal-backdrop/.modal 手作族随之
 *  从 base.css 退役）；锚 pkg-grid 落 paper（div）、pkg-grid-item-* 仍在
 *  原生 button 本体；Esc/backdrop 点击 = 取消（文档级兜底同批 B 对话框）。 */
function PackageTypeGrid({
  rclass,
  choices,
  onPick,
  onCancel,
}: {
  rclass: RClass
  choices: PkgChoice[]
  onPick: (pt: PackageType) => void
  onCancel: () => void
}) {
  // Esc 兜底（T-344C D5）：MUI 已处理的 Esc stopPropagation，不双触发。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onCancel()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onCancel])

  const paperProps = {
    'data-testid': 'pkg-grid',
    sx: { width: 'min(440px, calc(100vw - 48px))' },
  }

  return (
    <Dialog
      open
      onClose={(_, reason) => {
        if (reason === 'escapeKeyDown' || reason === 'backdropClick') onCancel()
      }}
      aria-label="选择包类型"
      slotProps={{ paper: paperProps }}
    >
      <DialogTitle>选择包类型</DialogTitle>
      <DialogContent>
        <p className="text-2">
          新建 <b>{RCLASS_LABEL[rclass]}</b> 仓库的第一步——包类型决定协议路由与客户端接入命令，创建后不可更改。
        </p>
        <div className="pkg-grid-items" role="radiogroup" aria-label="包类型">
          {choices.map((c) => {
            const block = pkgChoiceBlock(rclass, c)
            const badgeTier = c.opt && c.opt.minTier !== 'community' ? c.opt.minTier : null
            return (
              <button
                type="button"
                key={c.id}
                role="radio"
                aria-checked={false}
                disabled={block !== null}
                title={block ?? undefined}
                className="pkg-grid-item"
                data-testid={`pkg-grid-item-${c.id}`}
                onClick={() => onPick(c.id)}
              >
                {/* T-390（FR-127）：包型身份走 brand 版官方标；禁用态
                    （license 门控/组合约束）换 mono + 容器 opacity 0.4
                    ——品牌色置灰会脏色（README §6.3），三件套 = mono +
                    opacity + pkg-tier-* 徽章（徽章在下）。
                    className="pkg-grid-item" 本票复线：T-240 起磁贴类名
                    从未落 DOM（repositories.css 的卡面族 dead 至今，本票
                    门控三件套断言暴露）——卡面 + 禁用置灰随类名复活；
                    几何档（440px Dialog 宽）不受影响（spec 复证）。 */}
                <PkgIcon id={c.id} variant={block ? 'mono' : 'brand'} size={22} className="pkg-icon" />
                <span className="pkg-name">
                  {c.label}
                  {badgeTier && (
                    <Chip
                      component="span"
                      size="small"
                      variant="outlined"
                      color={badgeTier === 'enterprise' ? 'warning' : 'info'}
                      className={tierBadgeClass(badgeTier)}
                      label={badgeTier}
                      data-testid={`pkg-tier-${c.id}`}
                      lang="en"
                      sx={{ ml: 0.5, verticalAlign: 'middle' }}
                    />
                  )}
                </span>
                <span className="pkg-desc">{block ?? c.desc}</span>
              </button>
            )
          })}
        </div>
      </DialogContent>
      <DialogActions>
        <Button variant="outlined" size="small" data-testid="pkg-grid-cancel" onClick={onCancel}>
          取消
        </Button>
      </DialogActions>
    </Dialog>
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

  /** 换包类型（网格选定/单选）：deb/rpm/helm 的策略键表单随之初始化——
   *  字段册驱动，键集随包类型闭集切换（创建态唯一入口；编辑态锁定不可换） */
  const pickPackage = (pt: PackageType) => {
    const pkg = policyPkg(pt)
    const policy = pkg ? initialPolicyForm(pkg) : {}
    setF((prev) => ({ ...prev, packageType: pt, policy }))
    setBaseline((prev) => ({ ...prev, packageType: pt, policy }))
  }

  const setPolicy = (wire: string, v: string | boolean) =>
    setF((prev) => ({ ...prev, policy: { ...prev.policy, [wire]: v } }))

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

  // 包型可选集（M10 T-288）：addons 注册表实时数据（徽章/锁定态与 License
  // 页同源）。加载中/403/失败 = 仅五核心地板项——门控型不误放，服务端终裁。
  const addons = useAsync(getAddons, [])
  const pkgChoices = useMemo(() => buildPkgChoices(packageTypeOptions(addons.data ?? [])), [addons.data])

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
                <Button component={Link} to="/admin/repositories/local" variant="outlined" size="small">
                  ← 返回仓库列表
                </Button>
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
              <Button component={Link} to="/admin/repositories/local" variant="outlined" size="small">
                ← 返回仓库列表
              </Button>
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
    // deb/rpm/helm 策略键分组（T-353）：local × 对应包类型才呈现
    const policyDef = f.rclass === 'local' ? policyPkg(f.packageType) : null
    return (
      <>
        {/* T-344 批 D：分区卡 Paper 化（§3.5 repositories 行）——
            .repo-form-section 手作族随本批退役。
            T-383：六节 Paper 加 form-section-* 锚（v1.19 入册）——M1 建仓
            形态对齐核验的「六节结构」断言钉死用；纯锚位，零逻辑。 */}
        <Paper component="section" aria-label="常规设置" data-testid="form-section-general" sx={{ p: 2, pb: 1.5, mb: 2 }}>
          <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
            常规设置
          </Typography>
          <div className="radio-row" role="radiogroup" aria-label="仓型">
            {RCLASSES.map((rc) => (
              <FormControlLabel
                key={rc}
                className={mode === 'edit' ? 'disabled' : undefined}
                disabled={mode === 'edit' || locked}
                control={
                  <Radio
                    size="small"
                    checked={f.rclass === rc}
                    onChange={() => {
                      set('rclass', rc)
                      if (rc !== 'local' && f.packageType === 'docker') pickPackage('generic')
                    }}
                    value={rc}
                    name="rclass"
                    slotProps={{ input: { 'data-testid': `form-rclass-${rc}` } as ComponentPropsWithoutRef<'input'> }}
                  />
                }
                label={rc === 'local' ? 'Local（本地存储）' : rc === 'remote' ? 'Remote（代理上游）' : 'Virtual（聚合）'}
              />
            ))}
          </div>
          <div className="radio-row" role="radiogroup" aria-label="包类型">
            {pkgChoices.map((c) => {
              const block = pkgChoiceBlock(f.rclass, c)
              const badgeTier = c.opt && c.opt.minTier !== 'community' ? c.opt.minTier : null
              return (
                <FormControlLabel
                  key={c.id}
                  className={block ? 'disabled' : undefined}
                  title={block ?? undefined}
                  disabled={mode === 'edit' || block !== null || locked}
                  control={
                    <Radio
                      size="small"
                      checked={f.packageType === c.id}
                      onChange={() => pickPackage(c.id)}
                      value={c.id}
                      name="packageType"
                      slotProps={{ input: { 'data-testid': `form-package-${c.id}` } as ComponentPropsWithoutRef<'input'> }}
                    />
                  }
                  label={
                    <>
                      {c.label}
                      {badgeTier && (
                        <Chip
                          component="span"
                          size="small"
                          variant="outlined"
                          color={badgeTier === 'enterprise' ? 'warning' : 'info'}
                          className={tierBadgeClass(badgeTier)}
                          label={badgeTier}
                          data-testid={`pkg-tier-${c.id}`}
                          lang="en"
                          sx={{ ml: 0.5, verticalAlign: 'middle' }}
                        />
                      )}
                    </>
                  }
                />
              )
            })}
          </div>
          {mode === 'edit' && (
            <p className="field-note">仓型与包类型不可修改（变更会静默改变全部协议路由决策）。</p>
          )}
          {mode === 'create' && (
            <div className="field">
              <label htmlFor="f-key">Repository key *</label>
              <TextField
                id="f-key"
                size="small"
                value={f.key}
                onChange={(e) => set('key', e.target.value)}
                placeholder="maven-remote"
                error={!!keyErr}
                disabled={locked}
                slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'form-key', lang: 'en' } }}
              />
              {keyErr ? (
                <p className="field-error" data-testid="form-key-error" role="alert">
                  {keyErr}
                </p>
              ) : f.key.trim() !== '' ? (
                <Box component="p" className="field-hint" sx={{ color: 'success.main' }} data-testid="form-key-ok">
                  ✓ 可用
                </Box>
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
            <TextField
              id="f-desc"
              multiline
              minRows={2}
              value={f.description}
              onChange={(e) => set('description', e.target.value)}
              placeholder="用途、负责人、团队…"
              disabled={locked}
              slotProps={{ htmlInput: { 'data-testid': 'form-description' } }}
            />
          </div>
        </Paper>

        {f.rclass === 'remote' && (
          <Paper component="section" aria-label="来源" data-testid="form-section-source" sx={{ p: 2, pb: 1.5, mb: 2 }}>
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
              来源（Remote）
            </Typography>
            <div className="field">
              <label htmlFor="f-url">上游 URL *</label>
              <TextField
                id="f-url"
                size="small"
                value={f.url}
                onChange={(e) => set('url', e.target.value)}
                placeholder="https://repo1.maven.org/maven2"
                error={!!urlErr}
                disabled={locked}
                slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'form-url', lang: 'en' } }}
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
              <TextField
                id="f-user"
                size="small"
                value={f.username}
                onChange={(e) => set('username', e.target.value)}
                disabled={locked}
                slotProps={{ htmlInput: { 'data-testid': 'form-username' } }}
              />
            </div>
            <div className="field">
              <label htmlFor="f-pass">密码（上游认证，可选）</label>
              <TextField
                id="f-pass"
                size="small"
                type="password"
                autoComplete="new-password"
                value={f.password}
                onChange={(e) => set('password', e.target.value)}
                placeholder="永不回显"
                disabled={locked}
                slotProps={{ htmlInput: { 'data-testid': 'form-password' } }}
              />
              <p className="field-hint">
                密码不回显（NFR-S14）。注意：保存为<b>全量替换</b>语义——留空保存会
                <b>清除</b>已存凭据；需要保留请重新输入。
              </p>
            </div>
            <FormControlLabel
              className="check-row"
              disabled={locked}
              control={
                <Checkbox
                  size="small"
                  checked={f.allowPrivateUpstream}
                  onChange={(e) => set('allowPrivateUpstream', e.target.checked)}
                />
              }
              label="允许私网上游（allowPrivateUpstream）"
            />
            {f.allowPrivateUpstream && (
              <div className="warn-box">
                ⚠ 已放行私网上游：SSRF 防线对该仓放宽，变更会记录审计（NFR-S14）。
              </div>
            )}
          </Paper>
        )}

        {f.rclass === 'virtual' && (
          <Paper component="section" aria-label="成员" data-testid="form-section-members" sx={{ p: 2, pb: 1.5, mb: 2 }}>
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
              成员（Virtual）
            </Typography>
            <div className="field">
              <label>成员（可多选，↑↓ 调整解析顺序）</label>
              {candidates.status === 'loading' && <Skeleton lines={3} />}
              {candidates.status !== 'loading' && memberOptions.length === 0 && (
                <p className="field-hint">没有可用的 local/remote 仓可作为成员——先创建成员仓库。</p>
              )}
              {memberOptions.length > 0 && (
                <div className="member-pick">
                  {memberOptions.map((o: RepoListItem) => (
                    <FormControlLabel
                      key={o.key}
                      disabled={locked}
                      control={
                        <Checkbox
                          size="small"
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
                          slotProps={{ input: { 'data-testid': `form-member-${o.key}` } as ComponentPropsWithoutRef<'input'> }}
                        />
                      }
                      label={
                        <>
                          <span className="mono" lang="en">
                            {o.key}
                          </span>{' '}
                          <Chip component="span" size="small" className="badge neutral" label={o.type} sx={{ mx: 0.5 }} />
                          {cfgBool(o.configuration, 'priorityResolution') && (
                            <Chip component="span" size="small" variant="outlined" color="warning" className="badge warning" label="优先解析" sx={{ mx: 0.5 }} />
                          )}
                        </>
                      }
                    />
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
                        <IconButton
                          size="small"
                          aria-label={`上移 ${m}`}
                          disabled={i === 0 || locked}
                          onClick={() => moveMember(i, -1)}
                          data-testid={`member-up-${i}`}
                        >
                          <span aria-hidden="true">↑</span>
                        </IconButton>
                        <IconButton
                          size="small"
                          aria-label={`下移 ${m}`}
                          disabled={i === f.members.length - 1 || locked}
                          onClick={() => moveMember(i, 1)}
                        >
                          <span aria-hidden="true">↓</span>
                        </IconButton>
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}
            <div className="field">
              <label htmlFor="f-deploy">默认部署仓库（可选，仅 local 成员）</label>
              {/* native select（真实 <select>/<option>）：selectOption/toHaveValue
                  锚链路零变化；MUI 只承载外形（OutlinedInput 包裹 + 箭头） */}
              <TextField
                id="f-deploy"
                select
                size="small"
                value={f.defaultDeploymentRepo}
                onChange={(e) => set('defaultDeploymentRepo', e.target.value)}
                disabled={localMembers.length === 0 || locked}
                sx={{ width: 300 }}
                slotProps={{
                  select: {
                    native: true,
                    // data-testid 经 inputProps 下沉到 <select> 本体（slot
                    // 根 props 落在 MUI Select 的根 div 上——selectOption 锚
                    // 必须在真 select 元素上）
                    inputProps: {
                      'data-testid': 'form-default-deploy',
                    } as ComponentPropsWithoutRef<'select'>,
                  } as ComponentPropsWithoutRef<typeof Select>,
                }}
              >
                <option value="">（未配置——写操作将返回 405）</option>
                {localMembers.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </TextField>
              {f.defaultDeploymentRepo && (
                <p className="field-hint">经此 virtual 仓的部署将写入 {f.defaultDeploymentRepo}。</p>
              )}
            </div>
          </Paper>
        )}

        {f.rclass === 'local' && f.packageType === 'maven' && (
          <Paper component="section" aria-label="Maven 策略" data-testid="form-section-policy" sx={{ p: 2, pb: 1.5, mb: 2 }}>
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
              Maven 策略
            </Typography>
            <FormControlLabel
              className="check-row"
              disabled={locked}
              control={
                <Checkbox
                  size="small"
                  checked={f.handleReleases}
                  onChange={(e) => set('handleReleases', e.target.checked)}
                />
              }
              label="接受 release 部署（handleReleases）"
            />
            <FormControlLabel
              className="check-row"
              disabled={locked}
              control={
                <Checkbox
                  size="small"
                  checked={f.handleSnapshots}
                  onChange={(e) => set('handleSnapshots', e.target.checked)}
                />
              }
              label="接受 SNAPSHOT 部署（handleSnapshots）"
            />
            <div className="field">
              <label htmlFor="f-checksum">checksum 策略</label>
              <TextField
                id="f-checksum"
                select
                size="small"
                value={f.checksumPolicyType}
                onChange={(e) => set('checksumPolicyType', e.target.value)}
                disabled={locked}
                sx={{ width: 420 }}
                slotProps={{ select: { native: true } as ComponentPropsWithoutRef<typeof Select> }}
              >
                <option value="client-checksums">client-checksums（客户端声明严格校验，默认）</option>
                <option value="server-generated-checksums">server-generated-checksums（服务端实测覆盖）</option>
              </TextField>
            </div>
            <div className="field">
              <label htmlFor="f-snapshot">SNAPSHOT 行为</label>
              <TextField
                id="f-snapshot"
                select
                size="small"
                value={f.snapshotVersionBehavior}
                onChange={(e) => set('snapshotVersionBehavior', e.target.value)}
                disabled={locked}
                sx={{ width: 420 }}
                slotProps={{ select: { native: true } as ComponentPropsWithoutRef<typeof Select> }}
              >
                <option value="deployer">deployer（按上传名存储，默认）</option>
                <option value="non-unique">non-unique</option>
                <option value="unique">unique（unique 改写为 P2，行为同 deployer）</option>
              </TextField>
            </div>
          </Paper>
        )}

        {f.rclass === 'local' && (
          <Paper component="section" aria-label="治理" data-testid="form-section-governance" sx={{ p: 2, pb: 1.5, mb: 2 }}>
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
              治理（governance）
            </Typography>
            <div className="field">
              <label htmlFor="f-quota">配额 quotaBytes（字节）</label>
              <TextField
                id="f-quota"
                size="small"
                value={f.quotaBytes}
                onChange={(e) => set('quotaBytes', e.target.value)}
                error={!isNonNegInt(f.quotaBytes)}
                disabled={locked}
                slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'form-quota', inputMode: 'numeric' } }}
              />
              <p className="field-hint">正整数；0 = 不限（默认）。超限写入收到 413（message 含 used/quota）。</p>
            </div>
            <div className="field">
              <label htmlFor="f-includes">includesPattern</label>
              <TextField
                id="f-includes"
                size="small"
                value={f.includesPattern}
                onChange={(e) => set('includesPattern', e.target.value)}
                placeholder="**/*"
                disabled={locked}
                slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'form-includes', lang: 'en' } }}
              />
              <p className="field-hint">逗号分隔多值；留空 / **/* = 匹配全部路径（保存为全量替换）。</p>
            </div>
            <div className="field">
              <label htmlFor="f-excludes">excludesPattern</label>
              <TextField
                id="f-excludes"
                size="small"
                value={f.excludesPattern}
                onChange={(e) => set('excludesPattern', e.target.value)}
                placeholder="（无）"
                disabled={locked}
                slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'form-excludes', lang: 'en' } }}
              />
              <p className="field-hint">
                exclude 优先于 include；留空 = 无排除。不匹配 includes 或命中 excludes 的上传收到 409（message 含双
                pattern）。
              </p>
            </div>
          </Paper>
        )}

        <Paper component="section" aria-label="高级" data-testid="form-section-advanced" sx={{ p: 2, pb: 1.5, mb: 2 }}>
          <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
            高级
          </Typography>
          {f.rclass === 'remote' && (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mt: -1, mb: 1.5 }}>
                缓存与超时（秒）——产品默认 7200 / 1800 / 15 / 300。
              </Typography>
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
                  <TextField
                    id={`f-${k}`}
                    size="small"
                    value={f[k]}
                    onChange={(e) => set(k, e.target.value)}
                    error={!isNonNegInt(f[k])}
                    disabled={locked}
                    slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': `form-${k}`, inputMode: 'numeric' } }}
                  />
                </div>
              ))}
              <FormControlLabel
                className="check-row"
                disabled={locked}
                control={
                  <Checkbox
                    size="small"
                    checked={f.hardFail}
                    onChange={(e) => set('hardFail', e.target.checked)}
                  />
                }
                label="hardFail（上游故障时直接失败，不降级）"
              />
            </>
          )}
          <FormControlLabel
            className="check-row"
            disabled={locked}
            control={
              <Checkbox
                size="small"
                checked={f.priorityResolution}
                onChange={(e) => set('priorityResolution', e.target.checked)}
              />
            }
            label="优先解析（priorityResolution：作为 virtual 成员时优先桶标记）"
          />

          {/* deb/rpm/helm 策略键（T-353，FR-113.2/113.5）：字段册驱动——REST
              已透传（T-327R/T-329 D-E），表单按包类型收窄呈现；check/合法
              number 恒提交（POINTER 语义，flip-off 必须过 round trip） */}
          {policyDef && (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 2, mb: 1.5 }}>
                {POLICY_GROUP_TITLE[policyDef]}——索引引擎策略键（仅{' '}
                {f.packageType} 仓；值域/默认值由服务端终裁）
              </Typography>
              {POLICY_FIELDS[policyDef].map((fd) => {
                const anchor = `form-${fd.wire}`
                if (fd.kind === 'check') {
                  return (
                    <div key={fd.wire}>
                      <FormControlLabel
                        className="check-row"
                        disabled={locked}
                        control={
                          <Checkbox
                            size="small"
                            checked={f.policy[fd.wire] === true}
                            onChange={(e) => setPolicy(fd.wire, e.target.checked)}
                            slotProps={{ input: { 'data-testid': anchor } as ComponentPropsWithoutRef<'input'> }}
                          />
                        }
                        label={fd.label}
                      />
                      {fd.hint && <p className="field-hint">{fd.hint}</p>}
                    </div>
                  )
                }
                if (fd.kind === 'select') {
                  return (
                    <div className="field" key={fd.wire}>
                      <label htmlFor={`f-policy-${fd.wire}`}>{fd.label}</label>
                      <TextField
                        id={`f-policy-${fd.wire}`}
                        select
                        size="small"
                        value={String(f.policy[fd.wire] ?? '')}
                        onChange={(e) => setPolicy(fd.wire, e.target.value)}
                        disabled={locked}
                        sx={{ width: 420 }}
                        slotProps={{
                          select: {
                            native: true,
                            inputProps: { 'data-testid': anchor } as ComponentPropsWithoutRef<'select'>,
                          } as ComponentPropsWithoutRef<typeof Select>,
                        }}
                      >
                        {(fd.options ?? []).map((o) => (
                          <option key={o} value={o} lang="en">
                            {o}
                          </option>
                        ))}
                      </TextField>
                      {fd.hint && <p className="field-hint">{fd.hint}</p>}
                    </div>
                  )
                }
                return (
                  <div className="field" key={fd.wire}>
                    <label htmlFor={`f-policy-${fd.wire}`}>{fd.label}</label>
                    <TextField
                      id={`f-policy-${fd.wire}`}
                      size="small"
                      value={String(f.policy[fd.wire] ?? '')}
                      onChange={(e) => setPolicy(fd.wire, e.target.value)}
                      error={fd.kind === 'number' && !isNonNegInt(String(f.policy[fd.wire] ?? ''))}
                      disabled={locked}
                      placeholder={fd.placeholder}
                      sx={{ width: 420 }}
                      slotProps={{
                        htmlInput: {
                          className: 'mono-input',
                          'data-testid': anchor,
                          lang: 'en',
                          ...(fd.kind === 'number' ? { inputMode: 'numeric' as const } : {}),
                        },
                      }}
                    />
                    {fd.hint && <p className="field-hint">{fd.hint}</p>}
                  </div>
                )
              })}
            </>
          )}
        </Paper>

        {/* T-404（R1 裁定形态）：复制配置内嵌节——**编辑态 × local** 才呈现
            （push 源是本仓；建仓态仓尚不存在，POST /v1/replications 的
            source_repo 前置校验必 400）。节内自治（列表四态 + 内嵌表单），
            与主表单状态零耦合——主表单的提交/重置不触及复制配置。深链
            ?section=replications = 仓列表 Run 动作与详情指针的落点。 */}
        {mode === 'edit' && f.rclass === 'local' && (
          <ReplicationsSection
            repoKey={routeKey ?? ''}
            canWrite={admin}
            focus={searchParams.get('section') === 'replications'}
          />
        )}
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
            <Alert
              severity="error"
              data-testid="form-error"
              role="alert"
              sx={{ mt: 'var(--bf-sp-4)' }}
            >
              <div>
                保存失败（HTTP {serverError.status || '网络'}）
              </div>
              <div className="mono" lang="en" style={{ fontSize: 'var(--bf-fs-aux)' }}>
                {serverError.message}
              </div>
            </Alert>
          )}
          <div className="form-actions">
            <Button
              variant="outlined"
              size="small"
              onClick={() => navigate(mode === 'create' ? `/admin/repositories/${f.rclass}` : `/admin/repositories/${routeKey}`)}
            >
              取消
            </Button>
            <Button
              variant="outlined"
              size="small"
              onClick={() => {
                setF(baseline)
                setServerError(null)
              }}
              data-testid="form-reset"
            >
              重置
            </Button>
            <Button
              variant="contained"
              size="small"
              disabled={!canSubmit}
              title={locked ? '只读管理员不可写（服务端 403 兜底）' : gate.reason}
              onClick={() => void doSubmit()}
              data-testid="form-submit"
            >
              {submitting ? '保存中…' : mode === 'create' ? `创建 ${RCLASS_LABEL[f.rclass]} 仓库` : '保存'}
            </Button>
          </div>
        </section>

        <aside className="card summary-box">
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
          choices={pkgChoices}
          onPick={(pt) => {
            // 网格选定的包类型进入基线（重置不退回进页默认 generic）
            pickPackage(pt)
            setPkgOpen(false)
          }}
          onCancel={() => navigate(`/admin/repositories/${f.rclass}`)}
        />
      )}
    </div>
  )
}
