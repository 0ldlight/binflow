// 建仓/编辑表单（console-ux §4.4/§6.7——P2 新栈重写：RHF + Zod）。
//
// 验证规则迁移事实源 = frontend-rewrite-audit §4 建仓行：
// - key：[a-z][a-z0-9-]{1,62}（2~63 字符，服务端终裁）——create 臂必填；
// - remote：url 必填 + http/https 上游基址预检；4×TTL/超时非负整数；
// - virtual：至少一个成员；默认部署仓联动清空；
// - local：quotaBytes 非负整数；maven 策略族；conan forceConanAuthentication；
//   deb/rpm/helm 策略键（policyFields 字段册——check/合法 number 恒提交）；
// - dirty-gating（T-443）：stableFormString 键排序 deep-equal 基线——
//   编辑态零变更 Save 不可达；
// - secret：密码永不回显（NFR-S14）；「留空保存 = 清除已存凭据」。
//
// 形态承接：包类型网格 Dialog（进页必选；924px 居中档）→ 三段步进
// （Basic | Advanced | Replications——第三步仅编辑×local，内嵌旧
// ReplicationsSection〔MUI，LegacyMount——终验强删项〕）；右栏实时摘要；
// Cancel + Create/Save 两钮（无 Reset——Q9 冻结）；?section=replications
// 直落第三步；预留位字段族（repoLayout/Environments/notes/blackedOut/
// archiveBrowsing/maxUniqueSnapshots/SuppressPOM）恒禁用零提交。
// 锚族原样：repo-form-page / form-section-* / form-rclass-note /
// form-package-<pt> / form-key(-error|-ok) / form-description / form-url
// (-error) / form-username / form-password / form-test(-result|-create-
// note) / form-member-<key> / form-member-order / member-up-<i> /
// form-default-deploy / form-quota / form-includes / form-excludes /
// form-<TTL 键> / form-list-remote-folder-items / form-force-auth /
// form-<policy 键> / form-step-* / form-error / form-submit /
// repo-form-readonly-note / pkg-grid / pkg-grid-item-* / pkg-tier-* /
// pkg-grid-cancel / form-reserved-* 族。
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useForm } from 'react-hook-form'
import { z } from 'zod'

import { Button, ButtonAsChild } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useAuth } from '@/app/AuthContext'
import { toast } from '@/lib/toast'
import { LegacyMount } from '@/components/layout/legacy-host'
import { PkgIcon } from '@/components/PkgIcon'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { ApiError, canAdminWrite, errText, getRepositories, isReadOnlyAdmin, normalizeAdminRole } from '@/lib/api'
import type { RepoListItem } from '@/lib/api'
import { getAddons, packageTypeOptions, tierBadgeClass } from '@/lib/addons'
import type { PkgTypeOption } from '@/lib/addons'
import {
  RCLASSES,
  createRepo,
  cfgBool,
  cfgNum,
  cfgStr,
  cfgStrList,
  getRepoDetail,
  testRepoUpstream,
  updateRepo,
  validateRepoKey,
  validateUpstreamURL,
} from '@/lib/repos'
import type { PackageType, RClass, RepoConfigBody, RepoTestOverride, RepoTestResult } from '@/lib/repos'
import { useAsync } from '@/lib/useAsync'
import {
  POLICY_FIELDS,
  initialPolicyForm,
  policyBodyEntries,
  policyNumberValid,
  policyPkg,
  prefillPolicyForm,
} from '@/pages/repositories/policyFields'
import type { PolicyForm } from '@/pages/repositories/policyFields'
import {
  FORCE_CONAN_AUTH_HINT,
  FORCE_CONAN_AUTH_LABEL,
  FORM_STEPS,
  LIST_REMOTE_FOLDER_ITEMS_HINT,
  LIST_REMOTE_FOLDER_ITEMS_LABEL,
  RCLASS_ROUTE_NOTE,
  REMOTE_BROWSE_PKG_TYPES,
  REMOTE_TEST_CREATE_HINT,
  REMOTE_TEST_FAIL_NOTE,
  REMOTE_TEST_HINT,
  REMOTE_TEST_LABEL,
  REMOTE_TEST_OK_NOTE,
  REMOTE_TEST_STATUS_PREFIX,
  REMOTE_TEST_UNREACHED_NOTE,
  RESERVED_ARCHIVE_BROWSING_HINT,
  RESERVED_ARCHIVE_BROWSING_LABEL,
  RESERVED_BLACKED_OUT_HINT,
  RESERVED_BLACKED_OUT_LABEL,
  RESERVED_ENVIRONMENTS_HINT,
  RESERVED_GROUP_ADVANCED_TITLE,
  RESERVED_GROUP_BASIC_TITLE,
  RESERVED_INTERNAL_DESCRIPTION_HINT,
  RESERVED_MAX_UNIQUE_SNAPSHOTS_HINT,
  RESERVED_PLACEHOLDER,
  RESERVED_REPO_LAYOUT_HINT,
  RESERVED_SUPPRESS_POM_HINT,
  RESERVED_SUPPRESS_POM_LABEL,
  SAVE_CLEAN_HINT,
} from '@/pages/repositories/formCopy'
import { tr } from '@/i18n'
// 仓库管理域样式（pages/repositories 支撑模块族共享——旧页面退役后由新页直挂）
import '@/pages/repositories/repositories.css'
import { lazy } from 'react'

const t = tr('repositories')

// 旧 ReplicationsSection（MUI——第三步内嵌；终验强删项）
const ReplicationsSection = lazy(() => import('@/pages/repositories/ReplicationsSection'))

const REMOTE_TTL_DEFAULTS = {
  retrievalCachePeriodSecs: '7200',
  missedRetrievalCachePeriodSecs: '1800',
  socketTimeoutSecs: '15',
  assumedOfflinePeriodSecs: '300',
}

const RCLASS_LABEL: Record<RClass, string> = { local: 'Local', remote: 'Remote', virtual: 'Virtual' }

const PKG_ITEMS: { id: PackageType; label: string; desc: string }[] = [
  { id: 'generic', label: 'Generic', desc: t('任意文件（curl 上传 / 下载）') },
  { id: 'docker', label: 'Docker', desc: t('OCI 镜像（docker push / pull）') },
  { id: 'maven', label: 'Maven', desc: t('JVM 构件（mvn deploy / 解析）') },
  { id: 'npm', label: 'npm', desc: t('Node 包（npm publish / install）') },
  { id: 'pypi', label: 'PyPI', desc: t('Python 包（twine / pip）') },
]

interface PkgChoice {
  id: PackageType
  label: string
  desc: string
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

// ---- 表单模型（audit §4 建仓行 → Zod schema）----

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
  listRemoteFolderItems: boolean
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
  forceConanAuthentication: boolean
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
  listRemoteFolderItems: false,
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
  forceConanAuthentication: false,
  policy: {},
}

const NON_NEG_INT = /^\d+$/

/** Zod schema 按形态装配（mode/rclass/packageType 条件域——audit §4 逐条） */
function buildSchema(mode: 'create' | 'edit', f: FormState) {
  const key = mode === 'create'
    ? z.string().trim().min(1, t('Repository key 未填')).refine((v) => validateRepoKey(v) === null, t('Repository key 不合规'))
    : z.string()
  const base = z.object({
    key,
    quotaBytes: z.string().refine((v) => v.trim() === '' || NON_NEG_INT.test(v.trim()), t('quotaBytes 需为非负整数（字节）')),
    url: z.string(),
    members: z.array(z.string()),
    policy: z.record(z.string(), z.union([z.string(), z.boolean()])),
  })
  return base.superRefine((val, ctx) => {
    if (f.rclass === 'remote') {
      if (val.url.trim() === '') {
        ctx.addIssue({ code: 'custom', path: ['url'], message: t('上游 URL 未填（remote 必填）') })
      } else if (validateUpstreamURL(val.url.trim())) {
        ctx.addIssue({ code: 'custom', path: ['url'], message: t('上游 URL 不合规') })
      }
      for (const k of ['retrievalCachePeriodSecs', 'missedRetrievalCachePeriodSecs', 'socketTimeoutSecs', 'assumedOfflinePeriodSecs'] as const) {
        const v = (f as unknown as Record<string, string>)[k]
        if (!(v.trim() === '' || NON_NEG_INT.test(v.trim()))) {
          ctx.addIssue({ code: 'custom', path: [k], message: t('TTL/超时字段需为非负整数（秒）') })
        }
      }
    }
    if (f.rclass === 'virtual' && f.members.length === 0) {
      ctx.addIssue({ code: 'custom', path: ['members'], message: t('virtual 仓至少需要一个成员') })
    }
    if (f.rclass === 'local') {
      const pkg = policyPkg(f.packageType)
      if (pkg && !policyNumberValid(pkg, f.policy)) {
        ctx.addIssue({ code: 'custom', path: ['policy'], message: t('策略键的数值字段需为非负整数（historyCycles / yumRootDepth）') })
      }
    }
  })
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
    packageType: (d.packageType || 'generic') as PackageType,
    key: d.key,
    description: d.description,
  }
  if (f.rclass === 'remote') {
    f.url = cfgStr(cfg, 'url')
    f.username = cfgStr(cfg, 'username')
    f.allowPrivateUpstream = cfgBool(cfg, 'allowPrivateUpstream')
    f.hardFail = cfgBool(cfg, 'hardFail')
    f.listRemoteFolderItems = cfgBool(cfg, 'listRemoteFolderItems')
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
    const pkg = policyPkg(f.packageType)
    if (pkg) f.policy = prefillPolicyForm(pkg, cfg)
    if (f.packageType === 'conan') f.forceConanAuthentication = cfgBool(cfg, 'forceConanAuthentication')
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
    body.listRemoteFolderItems = f.listRemoteFolderItems
    body.priorityResolution = f.priorityResolution
    const nums: [keyof RepoConfigBody, string][] = [
      ['retrievalCachePeriodSecs', f.retrievalCachePeriodSecs],
      ['missedRetrievalCachePeriodSecs', f.missedRetrievalCachePeriodSecs],
      ['socketTimeoutSecs', f.socketTimeoutSecs],
      ['assumedOfflinePeriodSecs', f.assumedOfflinePeriodSecs],
    ]
    for (const [k, v] of nums) {
      if (v.trim() !== '' && NON_NEG_INT.test(v.trim())) {
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
    if (f.packageType === 'conan') {
      body.forceConanAuthentication = f.forceConanAuthentication
    }
    const pkg = policyPkg(f.packageType)
    if (pkg) Object.assign(body as unknown as Record<string, unknown>, policyBodyEntries(pkg, f.policy))
    body.includesPattern = f.includesPattern.trim()
    body.excludesPattern = f.excludesPattern.trim()
    const q = f.quotaBytes.trim()
    body.quotaBytes = NON_NEG_INT.test(q) ? Number(q) : 0
  }
  return body
}

/** 规范形序列化（键排序 deep-equal 基线——T-443 dirty-gating） */
function stableFormString(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(stableFormString).join(',')}]`
  if (v !== null && typeof v === 'object') {
    const entries = Object.entries(v as Record<string, unknown>)
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
      .map(([k, val]) => `${JSON.stringify(k)}:${stableFormString(val)}`)
    return `{${entries.join(',')}}`
  }
  return JSON.stringify(v) ?? 'null'
}

function formStateEquals(a: FormState, b: FormState): boolean {
  return stableFormString(a) === stableFormString(b)
}

/** Test 草稿臂（T-443）：密码填 → 整组草稿；url/user 改 → 匿名探测；零改动 → 无 body */
function buildTestBody(f: FormState, baseline: FormState | null): RepoTestOverride | undefined {
  if (baseline === null) return { url: f.url.trim(), username: f.username.trim(), password: f.password }
  if (f.password !== '') {
    return { url: f.url.trim(), username: f.username.trim(), password: f.password }
  }
  if (f.url.trim() !== baseline.url.trim() || f.username.trim() !== baseline.username.trim()) {
    return { url: f.url.trim(), username: f.username.trim() }
  }
  return undefined
}

type FormStep = 'basic' | 'advanced' | 'replications'

/** 预留位文本域（恒禁用 + 零提交） */
function ReservedTextField({ id, label, hint, anchor, multiline }: { id: string; label: string; hint: string; anchor: string; multiline?: boolean }) {
  return (
    <div className="field flex flex-col gap-1">
      <label htmlFor={id} className="text-dense text-muted-foreground">{label}</label>
      {multiline ? (
        <textarea
          id={id}
          disabled
          placeholder={RESERVED_PLACEHOLDER}
          rows={2}
          data-testid={anchor}
          lang="en"
          className="form-field w-full rounded-sm border border-input bg-surface-1 p-2 text-dense opacity-60"
        />
      ) : (
        <Input id={id} disabled placeholder={RESERVED_PLACEHOLDER} className="w-[420px]" data-testid={anchor} lang="en" />
      )}
      <p className="field-hint text-aux text-muted-foreground">{hint}</p>
    </div>
  )
}

function ReservedCheck({ label, hint, anchor }: { label: string; hint: string; anchor: string }) {
  return (
    <div>
      <Label className="flex items-center gap-1.5 font-normal text-muted-foreground">
        <input type="checkbox" className="size-3.5" disabled data-testid={anchor} /> {label}
      </Label>
      <p className="field-hint text-aux text-muted-foreground">{hint}</p>
    </div>
  )
}

function ReservedBasicFields() {
  return (
    <div className="field flex flex-col gap-2" data-testid="form-reserved-basic">
      <p className="text-dense text-muted-foreground">{RESERVED_GROUP_BASIC_TITLE}</p>
      <ReservedTextField id="f-repo-layout" label={t('Repository Layout（repoLayoutRef）')} hint={RESERVED_REPO_LAYOUT_HINT} anchor="form-repo-layout" />
      <ReservedTextField id="f-environments" label={t('环境段（Environments / Stage）')} hint={RESERVED_ENVIRONMENTS_HINT} anchor="form-environments" />
      <ReservedTextField id="f-internal-description" label={t('内部描述（Internal Description / notes）')} hint={RESERVED_INTERNAL_DESCRIPTION_HINT} anchor="form-internal-description" multiline />
    </div>
  )
}

function ReservedAdvancedChecks() {
  return (
    <div className="field flex flex-col gap-2" data-testid="form-reserved-advanced">
      <p className="text-dense text-muted-foreground">{RESERVED_GROUP_ADVANCED_TITLE}</p>
      <ReservedCheck label={RESERVED_BLACKED_OUT_LABEL} hint={RESERVED_BLACKED_OUT_HINT} anchor="form-blacked-out" />
      <ReservedCheck label={RESERVED_ARCHIVE_BROWSING_LABEL} hint={RESERVED_ARCHIVE_BROWSING_HINT} anchor="form-archive-browsing" />
    </div>
  )
}

/** 包类型网格（924px 居中档；磁贴恒 brand 官方标 + 档位徽章） */
function PackageTypeGrid({ rclass, choices, onPick, onCancel }: { rclass: RClass; choices: PkgChoice[]; onPick: (pt: PackageType) => void; onCancel: () => void }) {
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onCancel() }}>
      <DialogContent className="max-w-[924px] w-[min(924px,calc(100vw-48px))]" data-testid="pkg-grid">
        <DialogHeader>
          <DialogTitle>{t('选择包类型')}</DialogTitle>
        </DialogHeader>
        <p className="text-dense text-muted-foreground">
          {t('新建')} <b>{RCLASS_LABEL[rclass]}</b> {t('仓库的第一步——包类型决定协议路由与客户端接入命令，创建后不可更改。')}
        </p>
        <div className="pkg-grid-items grid grid-cols-3 gap-2" role="radiogroup" aria-label={t('包类型')}>
          {choices.map((c) => {
            const badgeTier = c.opt && c.opt.minTier !== 'community' ? c.opt.minTier : null
            return (
              <button
                type="button"
                key={c.id}
                role="radio"
                aria-checked={false}
                className="pkg-grid-item flex flex-col items-start gap-1 rounded-md border border-border bg-surface-1 p-3 text-left hover:bg-accent"
                data-testid={`pkg-grid-item-${c.id}`}
                onClick={() => onPick(c.id)}
              >
                <PkgIcon id={c.id} variant="brand" size={22} className="pkg-icon" />
                <span className="pkg-name flex items-center gap-1 text-dense font-medium">
                  {c.label}
                  {badgeTier && (
                    <span
                      className={`rounded-sm border px-1 text-[11px] ${tierBadgeClass(badgeTier)} ${badgeTier === 'enterprise' ? 'border-warning text-warning' : 'border-info text-info'}`}
                      data-testid={`pkg-tier-${c.id}`}
                      lang="en"
                    >
                      {badgeTier}
                    </span>
                  )}
                </span>
                <span className="pkg-desc text-aux text-muted-foreground">{c.desc}</span>
              </button>
            )
          })}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" data-testid="pkg-grid-cancel" onClick={onCancel}>{t('取消')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export default function RepositoryFormPage({ mode, rclass }: { mode: 'create' | 'edit'; rclass?: RClass }) {
  const { session } = useAuth()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  const role = normalizeAdminRole(session?.adminRole, session?.admin ?? false)
  const navigate = useNavigate()
  const { key: routeKey } = useParams<{ key: string }>()
  const [searchParams] = useSearchParams()

  const initialRclass: RClass = mode === 'create' ? (rclass ?? 'local') : 'local'

  const form = useForm<FormState>({ defaultValues: { ...CREATE_INITIAL, rclass: initialRclass } })
  const f = form.watch()
  const [baseline, setBaseline] = useState<FormState | null>(null)
  const [step, setStep] = useState<FormStep>(
    mode === 'edit' && searchParams.get('section') === 'replications' ? 'replications' : 'basic',
  )
  const [pkgOpen, setPkgOpen] = useState(mode === 'create')
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<RepoTestResult | null>(null)

  // 逐字段写入（字符串字段族的 onChange 面——类型收窄经 setValue 泛型）
  const set = (k: keyof FormState, v: string | boolean | string[] | PolicyForm) =>
    form.setValue(k, v as never, { shouldDirty: true })

  const pickPackage = (pt: PackageType) => {
    const pkg = policyPkg(pt)
    const policy = pkg ? initialPolicyForm(pkg) : {}
    form.setValue('packageType', pt, { shouldDirty: true })
    form.setValue('policy', policy, { shouldDirty: true })
  }

  const setPolicy = (wire: string, v: string | boolean) => {
    const next = { ...f.policy, [wire]: v }
    form.setValue('policy', next, { shouldDirty: true })
  }

  // 编辑态：加载现有配置并预填（同一对象落 dirty 基线）
  const detail = useAsync(
    () => (mode === 'edit' && routeKey ? getRepoDetail(routeKey) : Promise.resolve(null)),
    [mode, routeKey],
  )
  useEffect(() => {
    if (mode !== 'edit') return
    if (detail.status === 'ok' && detail.data) {
      const prefilled = prefillFromDetail(detail.data)
      form.reset(prefilled)
      setBaseline(prefilled)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 预填一次性
  }, [mode, detail.status, detail.data])

  // virtual 成员候选
  const candidates = useAsync(getRepositories, [])
  const memberOptions = useMemo(() => {
    const list = candidates.data ?? []
    return list.filter((r) => r.type !== 'virtual' && r.key !== f.key)
  }, [candidates.data, f.key])

  // 包型可选集（addons 注册表实时数据）
  const addons = useAsync(getAddons, [])
  const pkgChoices = useMemo(() => buildPkgChoices(packageTypeOptions(addons.data ?? [])), [addons.data])


  if (mode === 'create' && !admin) {
    return (
      <div data-testid="repo-form-page">
        <div className="page-header"><h2 className="text-lg font-semibold">{t('新建仓库')}</h2></div>
        <EmptyState
          message={t('无权限')}
          hint={
            readOnly
              ? t('只读管理员（readonly_admin）为只读呈现态：创建仓库是管理面写操作（repo:write，仅全量 admin；服务端 403 兜底）。')
              : t('创建仓库是管理员操作（repo:write）；当前用户 {v1} 不是 admin。', { v1: session?.username })
          }
        />
      </div>
    )
  }

  if (mode === 'edit') {
    if (detail.status === 'loading') {
      return <div data-testid="repo-form-page"><StateSkeleton lines={8} /></div>
    }
    if (detail.status === 'error' && detail.error) {
      return (
        <div data-testid="repo-form-page">
          {detail.error.status === 404 ? (
            <EmptyState
              message={t('仓库 {routeKey} 不存在', { routeKey })}
              action={<ButtonAsChild variant="outline" size="sm"><Link to="/admin/repositories/local">{t('← 返回仓库列表')}</Link></ButtonAsChild>}
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
          <EmptyState
            message={t('无权限管理此仓库')}
            hint={
              role === 'user'
                ? t('单仓配置管理需要该仓的 manage 动作（permission target 授权）；{v1}', { v1: detail.error.message })
                : detail.error.message
            }
            action={<ButtonAsChild variant="outline" size="sm"><Link to="/admin/repositories/local">{t('← 返回仓库列表')}</Link></ButtonAsChild>}
          />
        </div>
      )
    }
  }

  const keyErr = mode === 'create' ? validateRepoKey(f.key.trim()) : null
  const urlErr = f.rclass === 'remote' && f.url.trim() !== '' ? validateUpstreamURL(f.url.trim()) : null
  const locked = readOnly
  const dirty = mode !== 'edit' || baseline === null || !formStateEquals(f, baseline)

  // 全表单门控（audit §4 formValid 逐条——Save 零坏请求）
  const gateReason: string | null = (() => {
    if (mode === 'create') {
      if (f.key.trim() === '') return t('Repository key 未填')
      if (validateRepoKey(f.key.trim())) return t('Repository key 不合规')
    }
    if (f.rclass === 'remote') {
      if (f.url.trim() === '') return t('上游 URL 未填（remote 必填）')
      if (validateUpstreamURL(f.url.trim())) return t('上游 URL 不合规')
      for (const v of [f.retrievalCachePeriodSecs, f.missedRetrievalCachePeriodSecs, f.socketTimeoutSecs, f.assumedOfflinePeriodSecs]) {
        if (!(v.trim() === '' || NON_NEG_INT.test(v.trim()))) return t('TTL/超时字段需为非负整数（秒）')
      }
    }
    if (f.rclass === 'virtual' && f.members.length === 0) return t('virtual 仓至少需要一个成员')
    if (f.rclass === 'local') {
      if (!(f.quotaBytes.trim() === '' || NON_NEG_INT.test(f.quotaBytes.trim()))) return t('quotaBytes 需为非负整数（字节）')
      const pkg = policyPkg(f.packageType)
      if (pkg && !policyNumberValid(pkg, f.policy)) return t('策略键的数值字段需为非负整数（historyCycles / yumRootDepth）')
    }
    return null
  })()
  const canSubmit = gateReason === null && !submitting && !locked && dirty

  const doSubmit = async () => {
    // Zod schema（audit §4 规则事实源）终验——门控是同步等价面
    const check = await buildSchema(mode, f).safeParseAsync(f)
    if (!check.success) {
      setServerError(new ApiError(400, check.error.issues[0]?.message ?? t('表单校验未通过')))
      return
    }
    setServerError(null)
    setSubmitting(true)
    try {
      const key = mode === 'create' ? f.key.trim() : (routeKey ?? '')
      const text = mode === 'create' ? await createRepo(key, buildBody(f, mode)) : await updateRepo(key, buildBody(f, mode))
      toast.success(text)
      navigate(`/admin/repositories/${key}`)
    } catch (err) {
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  const doTest = async () => {
    if (!routeKey) return
    setTesting(true)
    setTestResult(null)
    try {
      const res = await testRepoUpstream(routeKey, buildTestBody(f, baseline))
      setTestResult(res)
    } catch (err) {
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setTesting(false)
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
  const replStepLive = mode === 'edit' && f.rclass === 'local'
  const activeStep: FormStep = step === 'replications' && !replStepLive ? 'basic' : step

  const policyDef = f.rclass === 'local' ? policyPkg(f.packageType) : null

  const checkRow = (label: string, checked: boolean, onChange: (v: boolean) => void, opts: { testid?: string; hint?: string } = {}) => (
    <div>
      <Label className="flex cursor-pointer items-center gap-1.5 font-normal">
        <input type="checkbox" className="size-3.5" checked={checked} disabled={locked} onChange={(e) => onChange(e.target.checked)} data-testid={opts.testid} />
        {label}
      </Label>
      {opts.hint && <p className="field-hint text-aux text-muted-foreground">{opts.hint}</p>}
    </div>
  )

  return (
    <div data-testid="repo-form-page" className="flex flex-col gap-3">
      <div className="page-header"><h2 className="text-lg font-semibold">{mode === 'create' ? t('新建 {v1} 仓库', { v1: RCLASS_LABEL[f.rclass] }) : t('编辑 {routeKey}', { routeKey })}</h2></div>

      {locked && (
        <p className="page-note rounded-md border border-border bg-surface-2 px-3 py-2 text-dense text-muted-foreground" data-testid="repo-form-readonly-note">
          {t('ⓘ 只读管理员（readonly_admin）视角：仓库配置只读——保存走单仓管理面写（CanManageRepo write），服务端 403 兜底。')}
        </p>
      )}

      <div className="form-layout flex flex-col gap-4 lg:flex-row">
        <section className="min-w-0 flex-1">
          {/* 三段步进（Basic | Advanced | Replications——第三步仅编辑×local） */}
          <div role="tablist" aria-label={t('仓库表单分区步进')} className="mb-4 flex border-b border-border">
            {([
              ['basic', FORM_STEPS.basic, 'Step 1 of 3: Basic', 'form-step-basic'],
              ['advanced', FORM_STEPS.advanced, 'Step 2 of 3: Advanced', 'form-step-advanced'],
              ...(replStepLive ? [['replications', FORM_STEPS.replications, 'Step 3 of 3: Replications', 'form-step-replications'] as const] : []),
            ] as [FormStep, string, string, string][]).map(([id, label, aria, testid]) => (
              <button
                key={id}
                type="button"
                role="tab"
                aria-selected={activeStep === id}
                aria-label={aria}
                data-testid={testid}
                className={`-mb-px rounded-t-sm border-b-2 bg-transparent px-4 py-2 text-dense ${activeStep === id ? 'border-primary font-medium text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
                onClick={() => setStep(id)}
              >
                {label}
              </button>
            ))}
          </div>

          {activeStep === 'replications' ? (
            <LegacyMount>
              <ReplicationsSection repoKey={routeKey ?? ''} canWrite={admin} focus={searchParams.get('section') === 'replications'} />
            </LegacyMount>
          ) : activeStep === 'basic' ? (
            <>
              <section className="form-section mb-3 rounded-md border border-border bg-surface-1 p-3" aria-label={t('常规设置')} data-testid="form-section-general">
                <h3 className="mb-2 text-[13px] font-semibold">{t('常规设置')}</h3>
                <p className="field-note mb-2 text-dense text-muted-foreground" data-testid="form-rclass-note">
                  {t('仓型：')}<b>{RCLASS_LABEL[f.rclass]}</b>——{RCLASS_ROUTE_NOTE}
                </p>
                <div className="radio-row flex flex-wrap gap-3" role="radiogroup" aria-label={t('包类型')}>
                  {pkgChoices.map((c) => {
                    const badgeTier = c.opt && c.opt.minTier !== 'community' ? c.opt.minTier : null
                    return (
                      <Label key={c.id} className={`flex cursor-pointer items-center gap-1 font-normal ${mode === 'edit' ? 'opacity-60' : ''}`}>
                        <input
                          type="radio"
                          name="packageType"
                          className="size-3.5"
                          checked={f.packageType === c.id}
                          onChange={() => pickPackage(c.id)}
                          value={c.id}
                          disabled={mode === 'edit' || locked}
                          data-testid={`form-package-${c.id}`}
                        />
                        {c.label}
                        {badgeTier && (
                          <span
                            className={`rounded-sm border px-1 text-[11px] ${badgeTier === 'enterprise' ? 'border-warning text-warning' : 'border-info text-info'}`}
                            data-testid={`pkg-tier-${c.id}`}
                            lang="en"
                          >
                            {badgeTier}
                          </span>
                        )}
                      </Label>
                    )
                  })}
                </div>
                {mode === 'edit' && <p className="field-note mt-1 text-aux text-muted-foreground">{t('包类型不可修改（变更会静默改变全部协议路由决策）。')}</p>}
                {mode === 'create' ? (
                  <div className="field mt-2 flex flex-col gap-1">
                    <label htmlFor="f-key" className="text-dense">Repository key *</label>
                    <Input id="f-key" value={f.key} onChange={(e) => set('key', e.target.value)} placeholder="maven-remote" aria-invalid={Boolean(keyErr)} disabled={locked} className="font-mono" data-testid="form-key" lang="en" />
                    {keyErr ? (
                      <p className="field-error text-aux text-destructive" data-testid="form-key-error" role="alert">{keyErr}</p>
                    ) : f.key.trim() !== '' ? (
                      <p className="field-hint text-aux text-success" data-testid="form-key-ok">{t('✓ 可用')}</p>
                    ) : (
                      <p className="field-hint text-aux text-muted-foreground">{t('规则 [a-z][a-z0-9-]')}{'{1,62}'}{t('，共 2~63 字符；服务端终裁。')}</p>
                    )}
                  </div>
                ) : (
                  <div className="kv mt-2 flex gap-2 text-dense">
                    <span className="k text-muted-foreground">Repository key</span>
                    <span className="font-mono" lang="en">{routeKey}</span>
                  </div>
                )}
                <div className="field mt-2 flex flex-col gap-1">
                  <label htmlFor="f-desc" className="text-dense">{t('描述')}</label>
                  <textarea
                    id="f-desc"
                    rows={2}
                    value={f.description}
                    onChange={(e) => set('description', e.target.value)}
                    placeholder={t('用途、负责人、团队…')}
                    disabled={locked}
                    data-testid="form-description"
                    className="form-field rounded-sm border border-input bg-surface-1 p-2 text-dense outline-none focus-visible:border-ring"
                  />
                </div>
                <div className="mt-2"><ReservedBasicFields /></div>
              </section>

              {f.rclass === 'remote' && (
                <section className="form-section mb-3 rounded-md border border-border bg-surface-1 p-3" aria-label={t('来源')} data-testid="form-section-source">
                  <h3 className="mb-2 text-[13px] font-semibold">{t('来源（Remote）')}</h3>
                  <div className="field flex flex-col gap-1">
                    <label htmlFor="f-url" className="text-dense">{t('上游 URL *')}</label>
                    <Input id="f-url" value={f.url} onChange={(e) => set('url', e.target.value)} placeholder="https://repo1.maven.org/maven2" aria-invalid={Boolean(urlErr)} disabled={locked} className="font-mono" data-testid="form-url" lang="en" />
                    {urlErr ? (
                      <p className="field-error text-aux text-destructive" data-testid="form-url-error" role="alert">{urlErr}</p>
                    ) : (
                      <p className="field-hint text-aux text-muted-foreground">{t('http/https 上游基址；私网地址合法（SSRF 链按请求校验）。')}</p>
                    )}
                  </div>
                  <div className="field mt-2 flex flex-col gap-1">
                    <label htmlFor="f-user" className="text-dense">{t('用户名（上游认证，可选）')}</label>
                    <Input id="f-user" value={f.username} onChange={(e) => set('username', e.target.value)} disabled={locked} data-testid="form-username" />
                  </div>
                  <div className="field mt-2 flex flex-col gap-1">
                    <label htmlFor="f-pass" className="text-dense">{t('密码（上游认证，可选）')}</label>
                    <Input id="f-pass" type="password" autoComplete="new-password" value={f.password} onChange={(e) => set('password', e.target.value)} placeholder={t('永不回显')} disabled={locked} data-testid="form-password" />
                    <p className="field-hint text-aux text-muted-foreground">{t('密码不回显（NFR-S14）。注意：保存为')}<b>{t('全量替换')}</b>{t('语义——留空保存会')}<b>{t('清除')}</b>{t('已存凭据；需要保留请重新输入。')}</p>
                  </div>
                  {mode === 'edit' ? (
                    <div className="field mt-2">
                      <Button variant="outline" size="sm" disabled={testing || locked || f.url.trim() === '' || Boolean(urlErr)} title={REMOTE_TEST_HINT} onClick={() => void doTest()} data-testid="form-test">
                        {testing ? t('测试中…') : REMOTE_TEST_LABEL}
                      </Button>
                      <p className="field-hint mt-1 text-aux text-muted-foreground">{REMOTE_TEST_HINT}</p>
                      {testResult && (
                        <div data-testid="form-test-result" role="status" className={`mt-2 rounded-md border px-3 py-2 text-dense ${testResult.ok ? 'border-success/50 bg-success/10' : 'border-destructive/40 bg-destructive/10'}`}>
                          <div lang="en">{testResult.message}</div>
                          <div>
                            {testResult.ok ? REMOTE_TEST_OK_NOTE : REMOTE_TEST_FAIL_NOTE}
                            {testResult.status_code > 0
                              ? t('（{REMOTE_TEST_STATUS_PREFIX}{v1}）', { REMOTE_TEST_STATUS_PREFIX, v1: testResult.status_code })
                              : t('（{REMOTE_TEST_UNREACHED_NOTE}）', { REMOTE_TEST_UNREACHED_NOTE })}
                          </div>
                        </div>
                      )}
                    </div>
                  ) : (
                    <p className="field-hint mt-2 text-aux text-muted-foreground" data-testid="form-test-create-note">{REMOTE_TEST_CREATE_HINT}</p>
                  )}
                  <div className="mt-2">{checkRow(t('允许私网上游（allowPrivateUpstream）'), f.allowPrivateUpstream, (v) => set('allowPrivateUpstream', v))}</div>
                  {f.allowPrivateUpstream && (
                    <div className="warn-box mt-2 rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-dense">{t('⚠ 已放行私网上游：SSRF 防线对该仓放宽，变更会记录审计（NFR-S14）。')}</div>
                  )}
                </section>
              )}

              {f.rclass === 'virtual' && (
                <section className="form-section mb-3 rounded-md border border-border bg-surface-1 p-3" aria-label={t('成员')} data-testid="form-section-members">
                  <h3 className="mb-2 text-[13px] font-semibold">{t('成员（Virtual）')}</h3>
                  <div className="field">
                    <label className="text-dense">{t('成员（可多选，↑↓ 调整解析顺序）')}</label>
                    {candidates.status === 'loading' && <StateSkeleton lines={3} />}
                    {candidates.status !== 'loading' && memberOptions.length === 0 && (
                      <p className="field-hint text-aux text-muted-foreground">{t('没有可用的 local/remote 仓可作为成员——先创建成员仓库。')}</p>
                    )}
                    <div className="member-pick mt-1 flex flex-col gap-1">
                      {memberOptions.map((o: RepoListItem) => (
                        <Label key={o.key} className="flex cursor-pointer items-center gap-1.5 font-normal">
                          <input
                            type="checkbox"
                            className="size-3.5"
                            checked={f.members.includes(o.key)}
                            disabled={locked}
                            onChange={(e) => {
                              if (e.target.checked) {
                                set('members', [...f.members, o.key])
                              } else {
                                set('members', f.members.filter((m) => m !== o.key))
                                if (f.defaultDeploymentRepo === o.key) set('defaultDeploymentRepo', '')
                              }
                            }}
                            data-testid={`form-member-${o.key}`}
                          />
                          <span className="font-mono" lang="en">{o.key}</span>{' '}
                          <span className="badge neutral rounded-sm bg-secondary px-1.5 py-px text-[11px]">{o.type}</span>
                          {cfgBool(o.configuration, 'priorityResolution') && (
                            <span className="badge warning rounded-sm border border-warning px-1.5 py-px text-[11px] text-warning">{t('优先解析')}</span>
                          )}
                        </Label>
                      ))}
                    </div>
                  </div>
                  {f.members.length > 0 && (
                    <div className="field mt-2">
                      <label className="text-dense">{t('已选成员（按解析顺序）')}</label>
                      <div className="chip-list mt-1 flex flex-wrap gap-1.5" data-testid="form-member-order">
                        {f.members.map((m, i) => (
                          <div key={m} className="chip-item flex items-center gap-1.5 rounded-sm border border-border bg-surface-2 px-2 py-1 text-dense">
                            <span className="idx">{i + 1}</span>
                            <span className="font-mono" lang="en">{m}</span>
                            <span className="chip-btns flex">
                              <button type="button" className="grid size-6 place-items-center rounded-sm hover:bg-accent disabled:opacity-40" aria-label={t('上移 {m}', { m })} disabled={i === 0 || locked} onClick={() => moveMember(i, -1)} data-testid={`member-up-${i}`}>
                                <span aria-hidden="true">↑</span>
                              </button>
                              <button type="button" className="grid size-6 place-items-center rounded-sm hover:bg-accent disabled:opacity-40" aria-label={t('下移 {m}', { m })} disabled={i === f.members.length - 1 || locked} onClick={() => moveMember(i, 1)}>
                                <span aria-hidden="true">↓</span>
                              </button>
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                  <div className="field mt-2 flex w-[300px] flex-col gap-1">
                    <label htmlFor="f-deploy" className="text-dense">{t('默认部署仓库（可选，仅 local 成员）')}</label>
                    <select
                      id="f-deploy"
                      value={f.defaultDeploymentRepo}
                      onChange={(e) => set('defaultDeploymentRepo', e.target.value)}
                      disabled={localMembers.length === 0 || locked}
                      data-testid="form-default-deploy"
                      className="h-8 rounded-sm border border-input bg-surface-1 px-2 text-dense"
                    >
                      <option value="">{t('（未配置——写操作将返回 405）')}</option>
                      {localMembers.map((m) => (
                        <option key={m} value={m}>{m}</option>
                      ))}
                    </select>
                    {f.defaultDeploymentRepo && (
                      <p className="field-hint text-aux text-muted-foreground">{t('经此 virtual 仓的部署将写入')} {f.defaultDeploymentRepo}{t('。')}</p>
                    )}
                  </div>
                </section>
              )}
            </>
          ) : (
            <>
              {f.rclass === 'local' && f.packageType === 'maven' && (
                <section className="form-section mb-3 rounded-md border border-border bg-surface-1 p-3" aria-label={t('Maven 策略')} data-testid="form-section-policy">
                  <h3 className="mb-2 text-[13px] font-semibold">{t('Maven 策略')}</h3>
                  {checkRow(t('接受 release 部署（handleReleases）'), f.handleReleases, (v) => set('handleReleases', v))}
                  {checkRow(t('接受 SNAPSHOT 部署（handleSnapshots）'), f.handleSnapshots, (v) => set('handleSnapshots', v))}
                  <div className="field mt-2 w-[420px]">
                    <label htmlFor="f-checksum" className="text-dense">{t('checksum 策略')}</label>
                    <select id="f-checksum" value={f.checksumPolicyType} onChange={(e) => set('checksumPolicyType', e.target.value)} disabled={locked} className="mt-1 h-8 w-full rounded-sm border border-input bg-surface-1 px-2 text-dense">
                      <option value="client-checksums">{t('client-checksums（客户端声明严格校验，默认）')}</option>
                      <option value="server-generated-checksums">{t('server-generated-checksums（服务端实测覆盖）')}</option>
                    </select>
                  </div>
                  <div className="field mt-2 w-[420px]">
                    <label htmlFor="f-snapshot" className="text-dense">{t('SNAPSHOT 行为')}</label>
                    <select id="f-snapshot" value={f.snapshotVersionBehavior} onChange={(e) => set('snapshotVersionBehavior', e.target.value)} disabled={locked} className="mt-1 h-8 w-full rounded-sm border border-input bg-surface-1 px-2 text-dense">
                      <option value="deployer">{t('deployer（按上传名存储，默认）')}</option>
                      <option value="non-unique">non-unique</option>
                      <option value="unique">{t('unique（unique 改写为 P2，行为同 deployer）')}</option>
                    </select>
                  </div>
                  <div className="mt-2"><ReservedTextField id="f-max-unique-snapshots" label={t('Max Unique Snapshots（maxUniqueSnapshots）')} hint={RESERVED_MAX_UNIQUE_SNAPSHOTS_HINT} anchor="form-max-unique-snapshots" /></div>
                  <div className="mt-2"><ReservedCheck label={RESERVED_SUPPRESS_POM_LABEL} hint={RESERVED_SUPPRESS_POM_HINT} anchor="form-suppress-pom" /></div>
                </section>
              )}

              {f.rclass === 'local' && (
                <section className="form-section mb-3 rounded-md border border-border bg-surface-1 p-3" aria-label={t('治理')} data-testid="form-section-governance">
                  <h3 className="mb-2 text-[13px] font-semibold">{t('治理（governance）')}</h3>
                  <div className="field w-[420px]">
                    <label htmlFor="f-quota" className="text-dense">{t('配额 quotaBytes（字节）')}</label>
                    <Input id="f-quota" value={f.quotaBytes} onChange={(e) => set('quotaBytes', e.target.value)} aria-invalid={!(f.quotaBytes.trim() === '' || NON_NEG_INT.test(f.quotaBytes.trim()))} disabled={locked} className="mt-1 font-mono" data-testid="form-quota" inputMode="numeric" />
                    <p className="field-hint text-aux text-muted-foreground">{t('正整数；0 = 不限（默认）。超限写入收到 413（message 含 used/quota）。')}</p>
                  </div>
                  <div className="field mt-2 w-[420px]">
                    <label htmlFor="f-includes" className="text-dense">includesPattern</label>
                    <Input id="f-includes" value={f.includesPattern} onChange={(e) => set('includesPattern', e.target.value)} placeholder="**/*" disabled={locked} className="mt-1 font-mono" data-testid="form-includes" lang="en" />
                    <p className="field-hint text-aux text-muted-foreground">{t('逗号分隔多值；留空 / **/* = 匹配全部路径（保存为全量替换）。')}</p>
                  </div>
                  <div className="field mt-2 w-[420px]">
                    <label htmlFor="f-excludes" className="text-dense">excludesPattern</label>
                    <Input id="f-excludes" value={f.excludesPattern} onChange={(e) => set('excludesPattern', e.target.value)} placeholder={t('（无）')} disabled={locked} className="mt-1 font-mono" data-testid="form-excludes" lang="en" />
                    <p className="field-hint text-aux text-muted-foreground">{t('exclude 优先于 include；留空 = 无排除。不匹配 includes 或命中 excludes 的上传收到 409（message 含双 pattern）。')}</p>
                  </div>
                </section>
              )}

              <section className="form-section mb-3 rounded-md border border-border bg-surface-1 p-3" aria-label={t('高级')} data-testid="form-section-advanced">
                <h3 className="mb-2 text-[13px] font-semibold">{t('高级')}</h3>
                {f.rclass === 'remote' && (
                  <>
                    <p className="mb-2 text-aux text-muted-foreground">{t('缓存与超时（秒）——产品默认 7200 / 1800 / 15 / 300。')}</p>
                    {(
                      [
                        ['retrievalCachePeriodSecs', t('命中缓存 TTL')],
                        ['missedRetrievalCachePeriodSecs', t('未命中负缓存 TTL')],
                        ['socketTimeoutSecs', t('socket 超时')],
                        ['assumedOfflinePeriodSecs', t('assumed-offline 静默期')],
                      ] as const
                    ).map(([k, label]) => (
                      <div className="field mb-2 w-[420px]" key={k}>
                        <label htmlFor={`f-${k}`} className="text-dense">{label}</label>
                        <Input
                          id={`f-${k}`}
                          value={(f as unknown as Record<string, string>)[k]}
                          onChange={(e) => set(k as keyof FormState, e.target.value)}
                          aria-invalid={!NON_NEG_INT.test(((f as unknown as Record<string, string>)[k] ?? '').trim())}
                          disabled={locked}
                          className="mt-1 font-mono"
                          data-testid={`form-${k}`}
                          inputMode="numeric"
                        />
                      </div>
                    ))}
                    {checkRow(t('hardFail（上游故障时直接失败，不降级）'), f.hardFail, (v) => set('hardFail', v))}
                    {REMOTE_BROWSE_PKG_TYPES.includes(f.packageType) && (
                      <div className="mt-2">
                        {checkRow(LIST_REMOTE_FOLDER_ITEMS_LABEL, f.listRemoteFolderItems, (v) => set('listRemoteFolderItems', v), { testid: 'form-list-remote-folder-items', hint: LIST_REMOTE_FOLDER_ITEMS_HINT })}
                      </div>
                    )}
                  </>
                )}
                {checkRow(t('优先解析（priorityResolution：作为 virtual 成员时优先桶标记）'), f.priorityResolution, (v) => set('priorityResolution', v))}
                {f.rclass === 'local' && f.packageType === 'conan' && (
                  <div className="mt-2">
                    {checkRow(FORCE_CONAN_AUTH_LABEL, f.forceConanAuthentication, (v) => set('forceConanAuthentication', v), { testid: 'form-force-auth', hint: FORCE_CONAN_AUTH_HINT })}
                  </div>
                )}
                <div className="mt-2"><ReservedAdvancedChecks /></div>
                {policyDef && (
                  <>
                    <p className="mt-3 mb-2 text-aux text-muted-foreground">
                      {t('索引引擎策略键（仅')} {f.packageType} {t('仓；值域/默认值由服务端终裁）')}
                    </p>
                    {POLICY_FIELDS[policyDef].map((fd) => {
                      const anchor = `form-${fd.wire}`
                      if (fd.kind === 'check') {
                        return (
                          <div key={fd.wire}>
                            {checkRow(fd.label, f.policy[fd.wire] === true, (v) => setPolicy(fd.wire, v), { testid: anchor, hint: fd.hint })}
                          </div>
                        )
                      }
                      if (fd.kind === 'select') {
                        return (
                          <div className="field mb-2 w-[420px]" key={fd.wire}>
                            <label htmlFor={`f-policy-${fd.wire}`} className="text-dense">{fd.label}</label>
                            <select
                              id={`f-policy-${fd.wire}`}
                              value={String(f.policy[fd.wire] ?? '')}
                              onChange={(e) => setPolicy(fd.wire, e.target.value)}
                              disabled={locked}
                              data-testid={anchor}
                              className="mt-1 h-8 w-full rounded-sm border border-input bg-surface-1 px-2 text-dense"
                            >
                              {(fd.options ?? []).map((o) => (
                                <option key={o} value={o} lang="en">{o}</option>
                              ))}
                            </select>
                            {fd.hint && <p className="field-hint text-aux text-muted-foreground">{fd.hint}</p>}
                          </div>
                        )
                      }
                      return (
                        <div className="field mb-2 w-[420px]" key={fd.wire}>
                          <label htmlFor={`f-policy-${fd.wire}`} className="text-dense">{fd.label}</label>
                          <Input
                            id={`f-policy-${fd.wire}`}
                            value={String(f.policy[fd.wire] ?? '')}
                            onChange={(e) => setPolicy(fd.wire, e.target.value)}
                            aria-invalid={fd.kind === 'number' && !(String(f.policy[fd.wire] ?? '').trim() === '' || NON_NEG_INT.test(String(f.policy[fd.wire] ?? '').trim()))}
                            disabled={locked}
                            placeholder={fd.placeholder}
                            className="mt-1 font-mono"
                            data-testid={anchor}
                            lang="en"
                            inputMode={fd.kind === 'number' ? 'numeric' : undefined}
                          />
                          {fd.hint && <p className="field-hint text-aux text-muted-foreground">{fd.hint}</p>}
                        </div>
                      )
                    })}
                  </>
                )}
              </section>
            </>
          )}

          {serverError && (
            <div data-testid="form-error" role="alert" className="mt-3 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense">
              <div>{t('保存失败（HTTP')} {serverError.status || t('网络')}{t('）')}</div>
              <div className="mt-1 break-all font-mono text-aux" lang="en">{serverError.message}</div>
            </div>
          )}

          <div className="form-actions mt-3 flex gap-2">
            <Button variant="outline" size="sm" onClick={() => navigate(mode === 'create' ? `/admin/repositories/${f.rclass}` : `/admin/repositories/${routeKey}`)}>
              {t('取消')}
            </Button>
            <Button
              size="sm"
              disabled={!canSubmit}
              title={locked ? t('只读管理员不可写（服务端 403 兜底）') : gateReason ?? (mode === 'edit' && !dirty ? SAVE_CLEAN_HINT : undefined)}
              onClick={() => void doSubmit()}
              data-testid="form-submit"
            >
              {submitting ? t('保存中…') : mode === 'create' ? t('创建 {v1} 仓库', { v1: RCLASS_LABEL[f.rclass] }) : t('保存')}
            </Button>
          </div>
        </section>

        <aside className="card summary-box w-full shrink-0 rounded-md border border-border bg-surface-1 p-3 lg:w-72">
          <h3 className="mb-2 text-[13px] font-semibold">{t('摘要（实时）')}</h3>
          {([
            ['key', <span key="k" className="font-mono" lang="en">{mode === 'create' ? f.key || '—' : (routeKey ?? '—')}</span>],
            ['rclass', f.rclass],
            ['packageType', f.packageType],
            ...(f.rclass === 'remote' ? ([['url', <span key="u" className="font-mono" lang="en">{f.url || '—'}</span>], ['TTL', <span key="t" className="font-mono">{f.retrievalCachePeriodSecs || '—'}s</span>]] as [string, React.ReactNode][]) : []),
            ...(f.rclass === 'virtual' ? ([[t('成员'), t('{v1} 个', { v1: f.members.length })]] as [string, React.ReactNode][]) : []),
            ...(f.rclass === 'local' ? ([['quotaBytes', <span key="q" className="font-mono">{f.quotaBytes || '0'}{f.quotaBytes === '0' ? t('（不限）') : ''}</span>], ['patterns', <span key="p" className="font-mono" lang="en">{f.includesPattern || '**/*'} ; {f.excludesPattern || '(none)'}</span>]] as [string, React.ReactNode][]) : []),
          ] as [string, React.ReactNode][]).map(([k, v]) => (
            <div className="kv mb-1 flex gap-2 text-dense" key={k}>
              <span className="k text-muted-foreground">{k}</span>
              <span className="min-w-0 break-all">{v}</span>
            </div>
          ))}
        </aside>
      </div>

      {pkgOpen && mode === 'create' && (
        <PackageTypeGrid
          rclass={f.rclass}
          choices={pkgChoices}
          onPick={(pt) => {
            pickPackage(pt)
            setPkgOpen(false)
          }}
          onCancel={() => navigate(`/admin/repositories/${f.rclass}`)}
        />
      )}
    </div>
  )
}
