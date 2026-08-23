import { useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, ReactNode, RefObject } from 'react'
import { Link } from 'react-router-dom'

import { useAuth } from '../app/AuthContext'
import { CopyButton } from './CopyButton'
import { EmptyState } from './EmptyState'
import { ErrorCard } from './ErrorCard'
import { Skeleton } from './Skeleton'
import { ApiError, apiJSON, errText, getRepositories } from '../lib/api'
import { getRepoDetail, PACKAGE_TYPES } from '../lib/repos'
import type { PackageType, RepoDetail } from '../lib/repos'
import { useAsync } from '../lib/useAsync'
import {
  CLIENT_PKG_META,
  placeholderCreds,
  smuConfigureCommands,
  smuDeployCommands,
} from '../pages/repositories/commands'
import type { ClientCreds, CommandBlock } from '../pages/repositories/commands'

import './dialogs.css'

// Set Me Up 客户端接入向导（T-242，console-m8 §4.1 / reverse §4.1——Artifactory
// 「Set Up A Client」操作流的自有皮肤对齐面）：
//
// [步骤 0] 包类型网格——只列实例内已有仓库的包类型并集（对齐 reverse §4.1
//          「集合 = 实例内仓的包类型」）；零仓库 → 提示先建仓（不给空指令）。
//          有仓库上下文（入口传入 preselectedRepo）则跳过网格直达主对话框
//          （对齐 reverse：选中仓库 → 直接 Set Up A <PackageType> Client）。
// [主对话框] 标题「配置 <PackageType> 客户端」+ 仓库下拉（预选当前仓）+
//          Tab 配置 Configure / 部署 Deploy（role=tablist + 方向键）。
//
// Token 生成区（§4.1 + T-219 遗留融合——console 此前无任何铸币面）：
// - 全部已认证会话可自铸（POST /api/security/token，Q11 自铸：仅本人 +
//   TTL 上限）；admin 会话是 ADR-0027 决策 1 的豁免臂（无二次凭据）。
// - **step-up 内联重验**（ADR-0027 / console-m8 §7.4）：auth.token_step_up
//   开启时非 admin session 臂铸币 → 401 step_up_required → 对话框内口令框
//   聚焦重输；step_up_invalid → 内联错误（error_description 逐字，等价
//   Artifactory「Incorrect password」形态——不出第二个对话框）；正确 →
//   续铸成功。mint 请求走 silent401：step-up 的 401 是对话语义，不是会话
//   死亡（不能触发全局「登录过期」跳转）。
// - 契约注记：端点无 description 字段（Artifactory 的
//   `MavenClient[SetMeUp]` 描述无 wire 位）——面板改示 token_id。
//
// 命令块数据源 = pages/repositories/commands.ts（P3 同源纪律：UI 不发明
// 命令）；铸币前凭据位给占位、成功后回填 username/token（一次性明文）。
//
// 403 收敛：仓库清单 GET /api/repositories 普通用户 403 → 已知 repo key 时
// 走单仓详情兜底（m-holder 持 manage 可读）；仍不可得 → L2 空态卡。

/** Artifactory Set Me Up 令牌默认 24h（reverse §3.18：SetMeUp token 默认 24h 过期） */
const TOKEN_TTL_SECONDS = 24 * 60 * 60

interface MintResponse {
  access_token: string
  token_type: string
  scope: string
  token_id: string
  expires_in?: number
}

/** POST /api/security/token（JSON projection，E-17 BinFlow 扩展形态） */
function mintToken(expiresIn: number, stepUpPassword?: string): Promise<MintResponse> {
  return apiJSON<MintResponse>('/security/token', {
    method: 'POST',
    body: {
      grant_type: 'client_credentials',
      expires_in: expiresIn,
      ...(stepUpPassword ? { step_up_password: stepUpPassword } : {}),
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
  | { phase: 'done'; token: string; tokenId: string; expiresIn?: number }
  | { phase: 'error'; message: string; status: number }

export interface SetMeUpDialogProps {
  /** 入口仓库上下文（树选中仓/列表行/详情头）——有值则直达主对话框 */
  preselectedRepo?: string
  onClose: () => void
}

interface MiniRepo {
  key: string
  packageType: string
  type: string
}

export default function SetMeUpDialog({ preselectedRepo, onClose }: SetMeUpDialogProps) {
  const { session } = useAuth()
  const admin = !!session?.admin

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
    const pre = preselectedRepo ? repos.find((r) => r.key === preselectedRepo) : undefined
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

  const runMint = async (stepUpPassword?: string) => {
    setMint(
      mint.phase === 'need-password'
        ? { phase: 'need-password', error: mint.error, raw: mint.raw, submitting: true }
        : { phase: 'minting' },
    )
    try {
      const res = await mintToken(TOKEN_TTL_SECONDS, stepUpPassword)
      setMint({ phase: 'done', token: res.access_token, tokenId: res.token_id, expiresIn: res.expires_in })
    } catch (err) {
      const oauth = oauthErrorOf(err)
      if (err instanceof ApiError && err.status === 401 && oauth) {
        if (oauth.code === 'step_up_required') {
          // §7.4：口令框聚焦重输——内联呈现，不弹第二层对话框
          setMint({ phase: 'need-password', error: null, raw: err.raw, submitting: false })
        } else if (oauth.code === 'step_up_invalid') {
          // 错误文案 ADR-0027 决策 5 逐字（error_description）
          setMint({ phase: 'need-password', error: oauth.description || err.message, raw: err.raw, submitting: false })
        } else {
          setMint({ phase: 'error', message: oauth.description || err.message, status: err.status })
        }
        return
      }
      setMint({ phase: 'error', message: errText(err), status: err instanceof ApiError ? err.status : 0 })
    }
  }

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

  // ---- Tab（role=tablist + 方向键，§9） ----
  const [tab, setTab] = useState<'configure' | 'deploy'>('configure')
  const tabRef = useRef<HTMLDivElement>(null)
  const onTabKeys = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
    e.preventDefault()
    const next = e.key === 'ArrowRight' ? 'deploy' : 'configure'
    setTab(next)
    tabRef.current?.querySelector<HTMLButtonElement>(`[data-testid="smu-tab-${next}"]`)?.focus()
  }

  // ---- 焦点陷阱 + Esc（ConfirmDialog 同款基座；modal root 聚焦承载
  //      scrollable-region-focusable 的可达性形态） ----
  const rootRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    rootRef.current?.focus()
    // disabled 控件不可聚焦（口令空时 smu-password-submit 禁用——陷阱首尾
    // 落在禁用钮上 focus() 无效会破口；T-244 复核收口）
    const FOCUSABLE =
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab') return
      const nodes = Array.from(rootRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])
      if (nodes.length === 0) return
      const first = nodes[0]
      const last = nodes[nodes.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  const pkgMeta = pkg ? CLIENT_PKG_META.find((m) => m.id === pkg) : undefined

  const renderGrid = (): ReactNode => {
    if (list.status === 'error' && list.error) {
      return <ErrorCard error={list.error} onRetry={list.reload} />
    }
    if (list.status === 'forbidden' && (!needFallback || fallback.status !== 'ok')) {
      return (
        <EmptyState
          message="无权限列出仓库"
          hint="仓库清单是管理面读端点（admin 与只读管理员可见）。请从具体仓库的详情页或制品树打开 Set Me Up。"
          testid="smu-grid-denied"
        />
      )
    }
    if (repos.length === 0) {
      return (
        <EmptyState
          message="这个实例还没有仓库"
          hint="Set Me Up 按已有仓库的包类型生成接入指引——先创建仓库。"
          testid="smu-grid-empty"
          action={
            admin ? (
              <Link className="btn primary" to="/admin/repositories/new">
                创建仓库
              </Link>
            ) : undefined
          }
        />
      )
    }
    return (
      <div
        className="smu-grid-items"
        role="radiogroup"
        aria-label="包类型"
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
          <button
            type="button"
            key={m.id}
            role="radio"
            aria-checked={false}
            data-testid={`smu-grid-item-${m.id}`}
            onClick={() => {
              setPkg(m.id)
              const rs = pkgReposOf(m.id, repos)
              const pre = preselectedRepo ? rs.find((r) => r.key === preselectedRepo) : undefined
              setRepoKey((pre ?? rs[0])?.key ?? '')
              setShowGrid(false)
            }}
          >
            <span className="pkg-icon" aria-hidden="true">
              {m.icon}
            </span>
            <span className="pkg-name">{m.label}</span>
            <span className="pkg-desc">{m.desc}</span>
          </button>
        ))}
      </div>
    )
  }

  return (
    <div
      className="modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={rootRef}
        tabIndex={-1}
        className="modal smu-modal"
        role="dialog"
        aria-modal="true"
        aria-label={pkgMeta ? `配置 ${pkgMeta.label} 客户端` : '选择客户端类型'}
        data-testid="smu-dialog"
      >
        {showGrid || !pkg ? (
          <>
            <h2>选择客户端类型</h2>
            <p className="text-2">选择包类型，了解如何向 BinFlow 解析与部署制品。</p>
            {resolving ? <Skeleton lines={3} /> : renderGrid()}
            <div className="modal-actions">
              <button type="button" className="btn" data-testid="smu-close" onClick={onClose}>
                关闭
              </button>
            </div>
          </>
        ) : (
          <>
            <h2>
              配置 {pkgMeta?.label ?? pkg} 客户端
              {repoKey && (
                <span className="mono" lang="en" style={{ marginLeft: 8, fontSize: 'var(--bf-fs-body)' }}>
                  {repoKey}
                </span>
              )}
            </h2>
            <button
              type="button"
              className="smu-back"
              data-testid="smu-back"
              onClick={() => setShowGrid(true)}
            >
              ← 选择不同的包类型
            </button>

            <div className="field" style={{ marginTop: 12 }}>
              <label htmlFor="smu-repo">仓库</label>
              <select
                id="smu-repo"
                data-testid="smu-repo"
                value={repoKey}
                onChange={(e) => setRepoKey(e.target.value)}
              >
                {pkgRepos.map((r) => (
                  <option key={r.key} value={r.key}>
                    {r.key}
                    {r.type !== 'local' ? `（${r.type}）` : ''}
                  </option>
                ))}
              </select>
              <div className="field-hint">下拉只列 {pkgMeta?.label ?? pkg} 类型的仓库。</div>
            </div>

            <div className="smu-tabs" role="tablist" aria-label="接入指引" ref={tabRef} onKeyDown={onTabKeys}>
              <button
                type="button"
                role="tab"
                aria-selected={tab === 'configure'}
                data-testid="smu-tab-configure"
                onClick={() => setTab('configure')}
              >
                配置 Configure
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={tab === 'deploy'}
                data-testid="smu-tab-deploy"
                onClick={() => setTab('deploy')}
              >
                部署 Deploy
              </button>
            </div>

            {tab === 'configure' ? (
              <div role="tabpanel" data-testid="smu-pane-configure">
                {session && (
                  <TokenArea mint={mint} admin={admin} username={session.username} password={password} setPassword={setPassword} passwordRef={passwordRef} onGenerate={() => void runMint()} onSubmitPassword={() => void runMint(password)} />
                )}
                {configureBlocks.map((c, i) => (
                  <CmdBlock key={c.title} block={c} testid={`smu-cmd-conf-${pkg}-${i}`} />
                ))}
              </div>
            ) : (
              <div role="tabpanel" data-testid="smu-pane-deploy">
                {deployBlocks.map((c, i) => (
                  <CmdBlock key={c.title} block={c} testid={`smu-cmd-dep-${pkg}-${i}`} />
                ))}
              </div>
            )}

            <div className="modal-actions">
              <button type="button" className="btn primary" data-testid="smu-done" onClick={onClose}>
                完成
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function pkgReposOf(pt: PackageType, repos: MiniRepo[]): MiniRepo[] {
  return repos.filter((r) => r.packageType === pt)
}

function CmdBlock({ block, testid }: { block: CommandBlock; testid: string }) {
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

/** Token 生成区：直接铸币 + step-up 401 内联口令重验 + 一次性明文面板 */
function TokenArea({
  mint,
  admin,
  username,
  password,
  setPassword,
  passwordRef,
  onGenerate,
  onSubmitPassword,
}: {
  mint: MintState
  admin: boolean
  username: string
  password: string
  setPassword: (v: string) => void
  passwordRef: RefObject<HTMLInputElement | null>
  onGenerate: () => void
  onSubmitPassword: () => void
}) {
  return (
    <section data-testid="smu-token-area" style={{ margin: '12px 0' }}>
      {mint.phase === 'done' ? (
        <div className="smu-token-panel" data-testid="smu-token-panel">
          <div>
            <b>令牌已生成</b>（以 <span className="mono" lang="en">{username}</span> 身份自铸）
          </div>
          <div className="token-line">
            <code data-testid="smu-token" lang="en">
              {mint.token}
            </code>
            <CopyButton value={mint.token} label="API Token" />
          </div>
          <div className="field-hint">
            关闭对话框后不可再查看（服务端只存指纹）。有效期 24 小时（token_id{' '}
            <span className="mono" lang="en">{mint.tokenId}</span>
            ）——CI 与脚本请使用此令牌，不要用控制台口令。
          </div>
        </div>
      ) : mint.phase === 'need-password' ? (
        <div className="smu-stepup" data-testid="smu-stepup">
          <div className="field" style={{ marginBottom: 8 }}>
            <label htmlFor="smu-password">服务端要求二次口令（step-up）——输入当前账号口令后继续铸币</label>
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
            <summary className="field-hint">服务端原文</summary>
            <pre className="smu-error-raw" lang="en">
              {mint.raw ?? ''}
            </pre>
          </details>
          <div style={{ marginTop: 8 }}>
            <button
              type="button"
              className="btn primary"
              data-testid="smu-password-submit"
              disabled={mint.submitting || password === ''}
              onClick={onSubmitPassword}
            >
              {mint.submitting ? '验证中…' : '验证并生成令牌'}
            </button>
          </div>
        </div>
      ) : (
        <>
          <div style={{ marginBottom: 8 }}>
            <button
              type="button"
              className="btn primary"
              data-testid="smu-generate"
              disabled={mint.phase === 'minting'}
              onClick={onGenerate}
            >
              {mint.phase === 'minting' ? '生成中…' : '生成令牌并创建指引'}
            </button>
          </div>
          <p className="field-hint">
            {admin
              ? `以 ${username}（管理员会话）自铸 24 小时令牌——管理员臂免二次口令（ADR-0027 决策 1）。`
              : `以 ${username} 身份自铸 24 小时令牌（仅本人、TTL 有上限）；实例开启 step-up 时需口令重验。`}
            {mint.phase === 'error' && (
              <span className="smu-error-inline" role="alert" data-testid="smu-mint-error" style={{ display: 'block' }}>
                铸币失败（HTTP {mint.status}）：{mint.message}
              </span>
            )}
          </p>
        </>
      )}
    </section>
  )
}
