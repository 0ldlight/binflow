import { useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, ReactNode, RefObject } from 'react'
import { Link } from 'react-router-dom'
import * as DialogPrimitive from '@radix-ui/react-dialog'

import { Button, ButtonAsChild } from '@/components/ui/button'
import { useAuth } from '../app/AuthContext'
import { PkgIcon } from './PkgIcon'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { ApiError, apiJSON, errText, getRepositories } from '../lib/api'
import { getRepoDetail, PACKAGE_TYPES } from '../lib/repos'
import type { PackageType, RepoDetail } from '../lib/repos'
import {
  GRANT_TTL_HINT_SECONDS,
  readPendingMint,
  savePendingMint,
  settleStepUpInvalid,
  settleStepUpMinted,
  stepUpGrantValue,
  useStepUp,
} from '../lib/stepUpGrant'
import type { PendingMint } from '../lib/stepUpGrant'
import { useAsync } from '../lib/useAsync'
import {
  CLIENT_PKG_META,
  placeholderCreds,
  smuConfigureCommands,
  smuDeployCommands,
  smuResolveCommands,
} from '../pages/repositories/commands'
import type { ClientCreds, CommandBlock } from '../pages/repositories/commands'

import './dialogs.css'
import { tr } from '../i18n'

const t = tr('console')

// Set Me Up 客户端接入向导（T-242，console-m8 §4.1 / reverse §4.1——Artifactory
// 「Set Up A Client」操作流的自有皮肤对齐面）：
//
// [壳形态] T-382 抽屉化（console-artifactory-parity D1 v1.1 实测参数）：
//          右侧抽屉（anchor right + temporary + 轻遮罩），宽
//          min(clamp(480px, 50vw, 800px), 100vw)——50vw 档（1600 视口 800px、
//          1280 视口 640px 实测），480/800 为上下钳制、100vw 兜窄屏；全高；
//          头部右上 X + Esc/遮罩关闭；步 0 = 包型药丸（横排胶囊）。
//
// [步骤 0] 包类型药丸——只列实例内已有仓库的包类型并集（对齐 reverse §4.1
//          「集合 = 实例内仓的包类型」）；零仓库 → 提示先建仓（不给空指令）。
//          有仓库上下文（入口传入 preselectedRepo）则跳过药丸直达主面板
//          （对齐 reverse：选中仓库 → 直接 Set Up A <PackageType> Client）。
// [主面板] 标题「配置 <PackageType> 客户端」+ 仓库下拉（预选当前仓）+
//          Tab 三枚：配置 Configure / 部署 Deploy / 解析 Resolve（role=tablist
//          + 方向键；T-382 前双 Tab 的内容映射留痕——
//            docker「登录与拉取」→ login 归 Configure、pull 归 Resolve；
//            maven pom <repositories> → Resolve；generic「下载与校验」→
//            Resolve；pypi「pip.conf」→ Resolve；npm 增安装验证（npm.md §4
//            同源）；generic/pypi 的 Configure 无配置步 → 导航提示）。
//          底栏 = 左「← 选择不同的包类型」文本链接 + 右「完成」主按钮
//          （Artifactory 底栏形态，v1.1 实测）。
//
// Token 生成区（§4.1 + T-219 遗留融合——console 此前无任何铸币面）：
// - 全部已认证会话可自铸（POST /api/security/token，Q11 自铸：仅本人 +
//   TTL 上限）；admin 会话是 ADR-0027 决策 1 的豁免臂（无二次凭据）。
// - **step-up 内联重验**（ADR-0027 / console-m8 §7.4）：auth.token_step_up
//   开启时非 admin session 臂铸币 → 401 step_up_required → 抽屉内口令框
//   聚焦重输；step_up_invalid → 内联错误（error_description 逐字，等价
//   Artifactory「Incorrect password」形态——不出第二个框）；正确 →
//   续铸成功。mint 请求走 silent401：step-up 的 401 是对话语义，不是会话
//   死亡（不能触发全局「登录过期」跳转）。
// - **OIDC 腿（T-260 / FR-81，ADR-0027 决策 8 后半的真身——T-242 期的
//   过渡注记至此收编）**：whoami source=oidc 的会话无本地口令可言——
//   401 step_up_required 不出口令框，改示「重新认证」引导：存 pending-mint
//   （sessionStorage bf.pendingMint，上下文 = {pkg, repo, 发起时刻}）→
//   全页跳转 /api/v1/oidc/login?purpose=step_up（服务端 302 IdP 且强制
//   prompt=login）→ 回跳 fragment #step_up_grant=<grant>（main.tsx 挂载期
//   提取即抹除，lib/stepUpGrant）→ AppShell 以 resume 形态重开本抽屉 →
//   挂载即自动续铸（body 携 step_up_grant）。401 step_up_invalid（过期/
//   复用/身份不符）→ 清 grant + pending（单次消费——绝不以旧 grant 重试）
//   + 内联 ADR 逐字文案 + 重新认证按钮（重走 init）。
// - 契约注记：端点无 description 字段（Artifactory 的
//   `MavenClient[SetMeUp]` 描述无 wire 位）——面板改示 token_id。
//
// 命令块数据源 = pages/repositories/commands.ts（P3 同源纪律：UI 不发明
// 命令）；铸币前凭据位给占位、成功后回填 username/token（一次性明文）。
//
// 403 收敛：仓库清单 GET /api/repositories 普通用户 403 → 已知 repo key 时
// 走单仓详情兜底（m-holder 持 manage 可读）；仍不可得 → L2 空态卡。
//
// FE-P4 MUI 清场：MUI Drawer → Radix Dialog 右向 sheet（dialog.tsx
// primitive 族同底座）；宽 clamp(480px,50vw,800px) 经 style 承载（v1.1
// 实测参数原样）。锚族与语义 DOM 原样（smu-dialog/-grid/-grid-item-<pt>/
// -repo/-tab-*/-pane-*/-cmd-*/-token/-token-panel/-stepup/-password*/
// -reauth/-generate/-back/-done/-close）；焦点圈进/Tab 循环/Esc/滚动锁定
// 交 Radix FocusScope（Esc 兜底不再需要文档级监听——DismissableLayer 的
// Esc 监听在 document 级、任意焦点位可达，且药丸/主面板切换的焦点掉
// body 场景同覆盖）；关闭回焦 = Radix FocusScope 卸载回焦（quick-set-me-up
// 菜单链路）+ 调用方显式 focus() 双保险。

/** Artifactory Set Me Up 令牌默认 24h（reverse §3.18：SetMeUp token 默认 24h 过期） */
const TOKEN_TTL_SECONDS = 24 * 60 * 60

/** Tab 三枚（T-382，D1 v1.1）：配置 Configure / 部署 Deploy / 解析 Resolve */
const TABS = ['configure', 'deploy', 'resolve'] as const

/**
 * OIDC step-up 重认证入口（T-219 服务端契约：GET → 302 IdP authorize 且
 * 强制 prompt=login；要求活跃 console session；disabled 实例 404）。
 * 服务端固定回跳 /binflow/ui/#step_up_grant=<grant>——无 return 参数
 * （防开放重定向），续铸上下文由 sessionStorage pending-mint 承载。
 */
const OIDC_STEP_UP_LOGIN_URL = '/binflow/api/v1/oidc/login?purpose=step_up'

interface MintResponse {
  access_token: string
  token_type: string
  scope: string
  token_id: string
  expires_in?: number
}

/** POST /api/security/token（JSON projection，E-17 BinFlow 扩展形态） */
function mintToken(expiresIn: number, stepUpPassword?: string, stepUpGrant?: string): Promise<MintResponse> {
  return apiJSON<MintResponse>('/security/token', {
    method: 'POST',
    body: {
      grant_type: 'client_credentials',
      expires_in: expiresIn,
      ...(stepUpPassword ? { step_up_password: stepUpPassword } : {}),
      ...(stepUpGrant ? { step_up_grant: stepUpGrant } : {}),
    },
    // step-up 的 401 是预期的对话分支（ADR-0027 决策 5 的 OAuth 形错误体），
    // 不得触发全局「会话过期」监听——这里按预期 401 豁免
    silent401: true,
  })
}

/** 从 ApiError.raw 提取 OAuth 形错误体（{error, error_description}） */
function oauthErrorOf(err: unknown): { code: string; description: string } | null {
  if (!(err instanceof ApiError) || !err.raw) return null
  try {
    const parsed = JSON.parse(err.raw) as { error?: unknown; error_description?: unknown }
    if (typeof parsed.error !== 'string') return null
    return {
      code: parsed.error,
      description: typeof parsed.error_description === 'string' ? parsed.error_description : '',
    }
  } catch {
    return null
  }
}

type MintState =
  | { phase: 'idle' }
  | { phase: 'minting' }
  | { phase: 'need-password'; error: string | null; raw: string | null; submitting: boolean }
  /** OIDC 腿（T-260）：无口令可输——重认证引导面板（error = 上次 invalid 的
   *  ADR 逐字文案；busy = 已发起跳转、页面即将卸载） */
  | { phase: 'need-reauth'; error: string | null; raw: string | null; busy: boolean }
  | { phase: 'done'; token: string; tokenId: string; expiresIn?: number }
  | { phase: 'error'; message: string; status: number }

export interface SetMeUpDialogProps {
  /** 入口仓库上下文（树选中仓/列表行/详情头）——有值则直达主对话框 */
  preselectedRepo?: string
  /** 回跳续铸态（T-260）：fragment grant 到手后 AppShell 以此重开本对话
   *  框——跳过网格恢复 {pkg, repo} 上下文，挂载即自动携 grant 续铸 */
  resume?: PendingMint | null
  onClose: () => void
}

interface MiniRepo {
  key: string
  packageType: string
  type: string
}

export default function SetMeUpDialog({ preselectedRepo, resume, onClose }: SetMeUpDialogProps) {
  const { session } = useAuth()
  const admin = !!session?.admin
  // step-up 腿分流（ADR-0027 决策 3）：oidc → mint grant（重认证）；
  // local/ldap/未知回退 → 口令腿（T-242 形态，旧行为原样）
  const oidcLeg = session?.source === 'oidc'
  // 续铸倒计时基准（grant 到手时刻；settle 后归零——届时面板已离开续铸态）
  const stepUp = useStepUp()

  // 仓库全集（包类型并集 + 下拉数据源）；403 时已知 key 走单仓详情兜底
  const list = useAsync(() => getRepositories(), [])
  const needFallback = list.status === 'forbidden' && !!preselectedRepo
  const fallback = useAsync(
    () => (needFallback ? getRepoDetail(preselectedRepo) : Promise.resolve(null)),
    [needFallback, preselectedRepo],
  )

  const repos: MiniRepo[] = useMemo(() => {
    if (list.status === 'ok') return (list.data ?? []) as MiniRepo[]
    if (needFallback && fallback.status === 'ok' && fallback.data) {
      const d = fallback.data as RepoDetail
      return [{ key: d.key, packageType: d.packageType, type: d.rclass }]
    }
    return []
  }, [list.status, list.data, needFallback, fallback.status, fallback.data])

  const resolving = list.status === 'loading' || (needFallback && fallback.status === 'loading')

  // ---- 阶段机：resolving →（有预选仓）main｜（无）grid ----
  const [pkg, setPkg] = useState<PackageType | null>(null)
  const [repoKey, setRepoKey] = useState('')
  const [showGrid, setShowGrid] = useState(false)
  const initRef = useRef(false)
  useEffect(() => {
    if (resolving || initRef.current) return
    initRef.current = true
    // 续铸态（T-260）：pending 上下文即事实源——即便仓库清单兜底未及/仓库
    // 已被删，也按原上下文开主对话框（命令块依赖上下文，铸造不依赖）
    const pre = resume
      ? { packageType: resume.pkg, key: resume.repo }
      : preselectedRepo
        ? repos.find((r) => r.key === preselectedRepo)
        : undefined
    if (pre) {
      setPkg(pre.packageType as PackageType)
      setRepoKey(pre.key)
    } else {
      setShowGrid(true)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 仅初始化一次（repos 就绪后）
  }, [resolving, repos])

  const availableTypes = useMemo(
    () => PACKAGE_TYPES.filter((pt) => repos.some((r) => r.packageType === pt)),
    [repos],
  )
  const pkgRepos = useMemo(() => (pkg ? repos.filter((r) => r.packageType === pkg) : []), [repos, pkg])

  // ---- 铸币 ----
  const [mint, setMint] = useState<MintState>({ phase: 'idle' })
  const [password, setPassword] = useState('')
  const passwordRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (mint.phase === 'need-password' && !mint.error) passwordRef.current?.focus()
  }, [mint])

  const runMint = async (opts: { password?: string; grant?: string } = {}) => {
    setMint(
      mint.phase === 'need-password'
        ? { phase: 'need-password', error: mint.error, raw: mint.raw, submitting: true }
        : { phase: 'minting' },
    )
    try {
      const res = await mintToken(TOKEN_TTL_SECONDS, opts.password, opts.grant)
      if (opts.grant) settleStepUpMinted() // grant 已消费铸出令牌：清 grant + pending
      setMint({ phase: 'done', token: res.access_token, tokenId: res.token_id, expiresIn: res.expires_in })
    } catch (err) {
      const oauth = oauthErrorOf(err)
      if (err instanceof ApiError && err.status === 401 && oauth) {
        if (oauth.code === 'step_up_required') {
          if (oidcLeg) {
            // OIDC 腿（T-260）：无口令可输——重认证引导，不弹口令框
            setMint({ phase: 'need-reauth', error: null, raw: err.raw, busy: false })
          } else {
            // §7.4：口令框聚焦重输——内联呈现，不弹第二层对话框
            setMint({ phase: 'need-password', error: null, raw: err.raw, submitting: false })
          }
        } else if (oauth.code === 'step_up_invalid') {
          // 单次消费 UX（T-260）：grant 过期/复用/身份不符——立即清 grant +
          // pending，绝不以旧 grant 重试（重走 init 由用户显式发起）
          if (opts.grant) settleStepUpInvalid()
          // 错误文案 ADR-0027 决策 5 逐字（error_description）
          const invalid = { error: oauth.description || err.message, raw: err.raw } as const
          setMint(
            oidcLeg
              ? { phase: 'need-reauth', ...invalid, busy: false }
              : { phase: 'need-password', ...invalid, submitting: false },
          )
        } else {
          setMint({ phase: 'error', message: oauth.description || err.message, status: err.status })
        }
        return
      }
      setMint({ phase: 'error', message: errText(err), status: err instanceof ApiError ? err.status : 0 })
    }
  }

  // ---- OIDC 腿：重认证发起（T-260，§14.3-1）----
  const onReauth = () => {
    if (mint.phase !== 'need-reauth' || mint.busy) return
    if (!pkg || !repoKey) return // 主对话框内两值恒有——防御式收口
    setMint({ phase: 'need-reauth', error: mint.error, raw: mint.raw, busy: true })
    // 上下文先落 sessionStorage（同 tab 跨导航存活；grant 值绝不入此结构）
    savePendingMint({ pkg, repo: repoKey, startedAt: Date.now() })
    // 全页跳转：服务端 302 → IdP（prompt=login）；页面即将卸载，busy 态防双发
    window.location.assign(OIDC_STEP_UP_LOGIN_URL)
  }

  // ---- 回跳续铸（T-260，§14.3-2）：挂载即自动携 grant 重发 mint ----
  const resumeFiredRef = useRef(false)
  useEffect(() => {
    if (!resume || resumeFiredRef.current) return
    resumeFiredRef.current = true
    const grant = stepUpGrantValue()
    if (grant) void runMint({ grant })
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 续铸仅在挂载时发一次
  }, [resume])

  // ---- 命令块凭据：铸币前占位、成功后回填（token 仅明文面板期存在） ----
  const creds: ClientCreds = useMemo(() => {
    if (mint.phase === 'done') {
      const username = session?.username ?? 'me'
      return { username, secret: mint.token, npmAuth: btoa(`${username}:${mint.token}`) }
    }
    return placeholderCreds(session?.username ?? '')
  }, [mint, session?.username])

  const configureBlocks = pkg && repoKey ? smuConfigureCommands(pkg, repoKey, creds) : []
  const deployBlocks = pkg && repoKey ? smuDeployCommands(pkg, repoKey, creds) : []
  const resolveBlocks = pkg && repoKey ? smuResolveCommands(pkg, repoKey) : []

  // 回焦锚：挂载时捕获场外焦点元素，卸载时回焦（quick-set-me-up 等启动钮
  // 的回焦家族契约——ui/dialog 的 DialogContent 同款：Radix FocusScope 的
  // unmount 回焦在 React 19.2 下实测不发火，此处显式承载；微任务让位：
  // 若 Radix 自身回焦恢复，不抢已就位的焦点）
  const returnFocusRef = useRef<HTMLElement | null>(null)
  useEffect(() => {
    returnFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    return () => {
      queueMicrotask(() => {
        if (document.activeElement === document.body || document.activeElement === null) {
          const el = returnFocusRef.current
          if (el && el.isConnected) el.focus({ preventScroll: true })
        }
      })
    }
  }, [])

  // ---- Tab 三枚（role=tablist + 方向键，§9；T-382 起 Configure/Deploy/
  //      Resolve——方向键在三枚间循环） ----
  const [tab, setTab] = useState<(typeof TABS)[number]>('configure')
  const tabRef = useRef<HTMLDivElement>(null)
  const onTabKeys = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
    e.preventDefault()
    const i = TABS.indexOf(tab)
    const next =
      e.key === 'ArrowRight' ? TABS[(i + 1) % TABS.length] : TABS[(i - 1 + TABS.length) % TABS.length]
    setTab(next)
    tabRef.current?.querySelector<HTMLButtonElement>(`[data-testid="smu-tab-${next}"]`)?.focus()
  }

  const pkgMeta = pkg ? CLIENT_PKG_META.find((m) => m.id === pkg) : undefined

  const renderGrid = (): ReactNode => {
    if (list.status === 'error' && list.error) {
      return <ErrorCard error={list.error} onRetry={list.reload} />
    }
    if (list.status === 'forbidden' && (!needFallback || fallback.status !== 'ok')) {
      return (
        <EmptyState
          message={t('无权限列出仓库')}
          hint={t('仓库清单是管理面读端点（admin 与只读管理员可见）。请从具体仓库的详情页或制品树打开 Set Me Up。')}
        />
      )
    }
    if (repos.length === 0) {
      return (
        <EmptyState
          message={t('这个实例还没有仓库')}
          hint={t('Set Me Up 按已有仓库的包类型生成接入指引——先创建仓库。')}
          action={
            admin ? (
              <ButtonAsChild>
                <Link to="/admin/repositories/new">{t('创建仓库')}</Link>
              </ButtonAsChild>
            ) : undefined
          }
        />
      )
    }
    return (
      <div
        className="smu-grid-items"
        role="radiogroup"
        aria-label={t('包类型')}
        data-testid="smu-grid"
        onKeyDown={(e) => {
          // radiogroup 方向键（§8/§9 键盘清单）：←↑→↓ 在网格项间移动焦点
          //（选择仍由 Enter/Space/点击承载——网格项即按钮）
          const keys = ['ArrowRight', 'ArrowDown', 'ArrowLeft', 'ArrowUp']
          if (!keys.includes(e.key)) return
          e.preventDefault()
          const items = Array.from(
            e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="radio"]:not([disabled])'),
          )
          if (items.length === 0) return
          const i = items.indexOf(document.activeElement as HTMLButtonElement)
          const forward = e.key === 'ArrowRight' || e.key === 'ArrowDown'
          const next = forward ? items[(i + 1) % items.length] : items[(i - 1 + items.length) % items.length]
          next?.focus()
        }}
      >
        {CLIENT_PKG_META.filter((m) => availableTypes.includes(m.id)).map((m) => (
          // 药丸形态（T-382，D1 v1.1 实测：步 0 = 包型药丸横排）——desc 收进
          // title 提示（药丸面保持紧凑，信息不丢）
          <button
            type="button"
            key={m.id}
            className="smu-grid-item"
            role="radio"
            aria-checked={false}
            title={m.desc}
            data-testid={`smu-grid-item-${m.id}`}
            onClick={() => {
              setPkg(m.id)
              const rs = pkgReposOf(m.id, repos)
              const pre = preselectedRepo ? rs.find((r) => r.key === preselectedRepo) : undefined
              setRepoKey((pre ?? rs[0])?.key ?? '')
              setShowGrid(false)
            }}
          >
            {/* T-390（FR-127）：包型身份走 brand 版官方标（字符图标退役）；
                药丸内与包名同现 = 装饰位（aria-hidden 在 PkgIcon 内） */}
            <PkgIcon id={m.id} variant="brand" size={18} className="pkg-icon" />
            <span className="pkg-name">{m.label}</span>
          </button>
        ))}
      </div>
    )
  }

  // 头部：标题（可及名源）+ 右上 X（抽屉族通用规格，§4）
  const header = (title: ReactNode) => (
    <div className="flex items-start justify-between gap-2 px-4 pt-4">
      <DialogPrimitive.Title id="smu-dialog-title" className="text-h3 min-w-0 font-semibold">
        {title}
      </DialogPrimitive.Title>
      <button
        type="button"
        aria-label={t('关闭')}
        data-testid="smu-close"
        onClick={onClose}
        className="mt-0.5 rounded-sm p-1 text-muted-foreground hover:bg-surface-2 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
      >
        ✕
      </button>
    </div>
  )

  return (
    <DialogPrimitive.Root open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay data-slot="dialog-overlay" className="fixed inset-0 z-[90] bg-scrim" />
        <DialogPrimitive.Content
          data-testid="smu-dialog"
          aria-labelledby="smu-dialog-title"
          className="box-border fixed inset-y-0 right-0 z-[90] flex w-[min(clamp(480px,50vw,800px),100vw)] flex-col border-l border-border bg-surface-1 shadow-modal"
        >
          {showGrid || !pkg ? (
            <>
              {header(t('选择客户端类型'))}
              <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
                <p className="text-2">{t('选择包类型，了解如何向 BinFlow 解析与部署制品。')}</p>
                {resolving ? <StateSkeleton lines={3} /> : renderGrid()}
              </div>
              <div className="flex justify-end border-t border-border px-4 py-3">
                <Button variant="outline" onClick={onClose}>{t('关闭')}</Button>
              </div>
            </>
          ) : (
            <>
              {header(
                <>{t('配置')} {pkgMeta?.label ?? pkg} {t('客户端')}
                  {repoKey && (
                    <span className="mono" lang="en" style={{ marginLeft: 8, fontSize: 'var(--bf-fs-body)' }}>
                      {repoKey}
                    </span>
                  )}
                </>,
              )}
              <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
                <div className="field">
                  <label htmlFor="smu-repo">{t('仓库')}</label>
                  <select
                    id="smu-repo"
                    className="w-full"
                    data-testid="smu-repo"
                    value={repoKey}
                    onChange={(e) => setRepoKey(e.target.value)}
                  >
                    {pkgRepos.map((r) => (
                      <option key={r.key} value={r.key}>
                        {r.key}
                        {r.type !== 'local' ? t('（{v1}）', { v1: r.type }) : ''}
                      </option>
                    ))}
                  </select>
                  <div className="field-hint">{t('下拉只列')} {pkgMeta?.label ?? pkg} {t('类型的仓库。')}</div>
                </div>

                <div className="smu-tabs" role="tablist" aria-label={t('接入指引')} ref={tabRef} onKeyDown={onTabKeys}>
                  <button
                    type="button"
                    className="smu-tab"
                    role="tab"
                    aria-selected={tab === 'configure'}
                    data-testid="smu-tab-configure"
                    onClick={() => setTab('configure')}
                  >{t('配置 Configure')}</button>
                  <button
                    type="button"
                    className="smu-tab"
                    role="tab"
                    aria-selected={tab === 'deploy'}
                    data-testid="smu-tab-deploy"
                    onClick={() => setTab('deploy')}
                  >{t('部署 Deploy')}</button>
                  <button
                    type="button"
                    className="smu-tab"
                    role="tab"
                    aria-selected={tab === 'resolve'}
                    data-testid="smu-tab-resolve"
                    onClick={() => setTab('resolve')}
                  >{t('解析 Resolve')}</button>
                </div>

                {tab === 'configure' ? (
                  <div role="tabpanel" data-testid="smu-pane-configure">
                    {session && (
                      <TokenArea
                        mint={mint}
                        admin={admin}
                        username={session.username}
                        oidcLeg={oidcLeg}
                        resuming={!!resume}
                        grantAt={resume ? stepUp.grantAt : null}
                        password={password}
                        setPassword={setPassword}
                        passwordRef={passwordRef}
                        onGenerate={() => void runMint()}
                        onSubmitPassword={() => void runMint({ password })}
                        onReauth={onReauth}
                      />
                    )}
                    {configureBlocks.length === 0 && (
                      // 包型无配置步（generic/pypi——curl/pip 开箱即用）：导航提示
                      // 替代命令块，不发明命令（P3 同源纪律）
                      <p className="field-hint">
                        {pkg === 'pypi'
                          ? t('pip 无需登录步骤——解析配置（pip.conf）在「解析 Resolve」Tab，发布凭据（.pypirc）在「部署 Deploy」Tab。')
                          : t('curl 无需预配置——匿名读默认开；解析/上传命令见另两 Tab，需要认证的路径在命令中加 -u <用户名>:<令牌>。')}
                      </p>
                    )}
                    {configureBlocks.map((c) => (
                      <CmdBlock key={c.title} block={c} />
                    ))}
                  </div>
                ) : tab === 'deploy' ? (
                  <div role="tabpanel" data-testid="smu-pane-deploy">
                    {deployBlocks.map((c, i) => (
                      <CmdBlock key={c.title} block={c} testid={`smu-cmd-dep-${pkg}-${i}`} />
                    ))}
                  </div>
                ) : (
                  <div role="tabpanel" data-testid="smu-pane-resolve">
                    {resolveBlocks.map((c, i) => (
                      <CmdBlock key={c.title} block={c} testid={`smu-cmd-res-${pkg}-${i}`} />
                    ))}
                  </div>
                )}
              </div>
              {/* 底栏（Artifactory 形态，v1.1 实测）：左文本返回链接 + 右主按钮 */}
              <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-3">
                <Button
                  variant="link"
                  size="sm"
                  data-testid="smu-back"
                  className="px-0"
                  onClick={() => setShowGrid(true)}
                >{t('← 选择不同的包类型')}</Button>
                <Button data-testid="smu-done" onClick={onClose}>{t('完成')}</Button>
              </div>
            </>
          )}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}

function pkgReposOf(pt: PackageType, repos: MiniRepo[]): MiniRepo[] {
  return repos.filter((r) => r.packageType === pt)
}

function CmdBlock({ block, testid }: { block: CommandBlock; testid?: string }) {
  return (
    <div className="cmd-block" data-testid={testid}>
      <header>
        <span>{block.title}</span>
        <CopyButton value={block.text} label={block.title} />
      </header>
      {/* tabIndex：对话框内 pre 实际溢出（overflow-x auto）——可滚动区域须
          键盘可达（WCAG 2.1，axe scrollable-region-focusable） */}
      <pre lang="en" tabIndex={0}>
        {block.text}
      </pre>
      {block.note && <div className="note">{block.note}</div>}
    </div>
  )
}

/** 续铸倒计时（提示性质：服务端 TTL 无查询端点，按默认 300s 呈现） */
function useGrantCountdown(grantAt: number | null): number {
  const [left, setLeft] = useState(0)
  useEffect(() => {
    if (!grantAt) {
      setLeft(0)
      return
    }
    const tick = () => setLeft(Math.max(0, GRANT_TTL_HINT_SECONDS - Math.floor((Date.now() - grantAt) / 1000)))
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [grantAt])
  return left
}

function mmss(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/** Token 生成区：直接铸币 + step-up 双腿（local/ldap 内联口令重验、oidc
 * 重认证引导 + 回跳续铸）+ 一次性明文面板 */
function TokenArea({
  mint,
  admin,
  username,
  oidcLeg,
  resuming,
  grantAt,
  password,
  setPassword,
  passwordRef,
  onGenerate,
  onSubmitPassword,
  onReauth,
}: {
  mint: MintState
  admin: boolean
  username: string
  /** OIDC 会话（whoami source=oidc）——step-up 走重认证腿 */
  oidcLeg: boolean
  /** 回跳续铸态（挂载即自动携 grant 续铸） */
  resuming: boolean
  /** grant 到手时刻（倒计时基准；非续铸态为 null） */
  grantAt: number | null
  password: string
  setPassword: (v: string) => void
  passwordRef: RefObject<HTMLInputElement | null>
  onGenerate: () => void
  onSubmitPassword: () => void
  onReauth: () => void
}) {
  const ttlLeft = useGrantCountdown(grantAt)
  // 「等待重认证完成」提示：pending 存在但 grant 未到手（IdP 侧取消/中断的
  // 半途流程）——只对 OIDC 腿呈现，idle 态提示可重新发起
  const pending = oidcLeg && mint.phase === 'idle' ? readPendingMint() : null
  const pendingAgeMin = pending ? Math.max(1, Math.round((Date.now() - pending.startedAt) / 60000)) : 0

  return (
    <section style={{ margin: '12px 0' }}>
      {mint.phase === 'done' ? (
        <div className="smu-token-panel" data-testid="smu-token-panel">
          <div>
            <b>{t('令牌已生成')}</b>{t('（以')} <span className="mono" lang="en">{username}</span> {t('身份自铸）')}</div>
          <div className="token-line">
            <code data-testid="smu-token" lang="en">
              {mint.token}
            </code>
            <CopyButton value={mint.token} label="API Token" />
          </div>
          <div className="field-hint">{t('关闭抽屉后不可再查看（服务端只存指纹）。有效期 24 小时（token_id')}{' '}
            <span className="mono" lang="en">{mint.tokenId}</span>{t('）——CI 与脚本请使用此令牌，不要用控制台口令。')}</div>
        </div>
      ) : mint.phase === 'need-password' ? (
        <div className="smu-stepup" data-testid="smu-stepup">
          <div className="field" style={{ marginBottom: 8 }}>
            <label htmlFor="smu-password">{t('服务端要求二次口令（step-up）——输入当前账号口令后继续铸币')}</label>
            <input
              id="smu-password"
              ref={passwordRef}
              type="password"
              autoComplete="current-password"
              data-testid="smu-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  onSubmitPassword()
                }
              }}
            />
          </div>
          {mint.error && (
            <p className="smu-error-inline" role="alert" data-testid="smu-password-error" lang="en">
              {mint.error}
            </p>
          )}
          <details>
            <summary className="field-hint">{t('服务端原文')}</summary>
            <pre className="smu-error-raw" lang="en">
              {mint.raw ?? ''}
            </pre>
          </details>
          <div style={{ marginTop: 8 }}>
            <Button
              data-testid="smu-password-submit"
              disabled={mint.submitting || password === ''}
              onClick={onSubmitPassword}
            >
              {mint.submitting ? t('验证中…') : t('验证并生成令牌')}
            </Button>
          </div>
        </div>
      ) : mint.phase === 'need-reauth' ? (
        // OIDC 腿（T-260 / FR-81）：无本地口令——重认证引导，替代口令框
        <div className="smu-stepup" data-testid="smu-oidc-stepup">
          <p className="field-hint" style={{ marginBottom: 8 }}>{t('服务端要求重新认证（step-up）：SSO 会话铸造令牌需到身份提供方重新登录一次。点击后将跳转登录页 （强制重新输入 IdP 凭据），完成后自动返回此处继续铸币——本抽屉的上下文会被记住。')}</p>
          {mint.error && (
            <p className="smu-error-inline" role="alert" data-testid="smu-reauth-error" lang="en">
              {mint.error}
            </p>
          )}
          {mint.error && (
            <p className="field-hint">{t('重认证凭证已失效（过期、已使用或身份不符）——需重新走一次登录，不会以旧凭证重试。')}</p>
          )}
          <details>
            <summary className="field-hint">{t('服务端原文')}</summary>
            <pre className="smu-error-raw" lang="en">
              {mint.raw ?? ''}
            </pre>
          </details>
          <div style={{ marginTop: 8 }}>
            <Button
              data-testid="smu-reauth"
              disabled={mint.busy}
              onClick={onReauth}
            >
              {mint.busy ? t('等待重认证完成…') : t('重新认证并继续')}
            </Button>
          </div>
        </div>
      ) : (
        <>
          {resuming && mint.phase === 'minting' && (
            // 回跳续铸 in-flight（§14.3-2）：grant 已到手、mint 自动重发中
            <p className="field-hint" style={{ marginBottom: 8 }} role="status">{t('重认证完成——正在自动续铸令牌…（重认证凭证约')} {mmss(ttlLeft)} {t('内有效）')}</p>
          )}
          <div style={{ marginBottom: 8 }}>
            <Button
              data-testid="smu-generate"
              disabled={mint.phase === 'minting'}
              onClick={onGenerate}
            >
              {mint.phase === 'minting' ? t('生成中…') : t('生成令牌并创建指引')}
            </Button>
          </div>
          <p className="field-hint">
            {admin
              ? t('以 {username}（管理员会话）自铸 24 小时令牌——管理员臂免二次口令（ADR-0027 决策 1）。', { username: username })
              : oidcLeg
                ? t('以 {username}（SSO 会话）自铸 24 小时令牌（仅本人、TTL 有上限）；实例开启 step-up 时需到 IdP 重新认证。', { username: username })
                : t('以 {username} 身份自铸 24 小时令牌（仅本人、TTL 有上限）；实例开启 step-up 时需口令重验。', { username: username })}
            {pending && (
              <span className="field-hint" data-testid="smu-pending-hint" style={{ display: 'block' }}>{t('有一笔铸造正在等待重认证完成…（')}{pendingAgeMin} {t('分钟前发起；若已在登录页取消，直接重新生成即可再次发起）')}</span>
            )}
            {mint.phase === 'error' && (
              <span className="smu-error-inline" role="alert" style={{ display: 'block' }}>{t('铸币失败（HTTP')} {mint.status}{t('）：')}{mint.message}
              </span>
            )}
          </p>
        </>
      )}
    </section>
  )
}
