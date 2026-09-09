// Access Tokens 页（M14 T-386——P3 新栈重写）。
// [零新端点纪律] 消费面 = 既有 token REST 两端点：
//   - POST /api/security/token（E-17，JSON 投影 + silent401）——mint/step-up
//     链（ADR-0027：非 admin web session 臂在 auth.token_step_up 开启时
//     401 step_up_required → 内联口令框；OIDC 臂诚实降级引导，不私建链路）；
//   - POST /api/security/token/revoke（E-18，form 体 token_id）。
// 服务端只存指纹、无令牌清单端点（console-ux §9-R6）——台账 = 本会话经此页
// 签发的内存态（刷新即空）；历史令牌吊销走「按 token_id 吊销」。指纹 =
// sha256 前 8 hex（与审计 token.issue 事件同 digest）。
// [四态] 表格无取数（无 GET 端点）→ 不伪造骨架/错误态；空态 = 台账空。
// [角色臂] admin 全量；readonly_admin 吊销禁用 + 注记（自铸不受限）；
// 普通 user = L2 无权限卡。
// 锚族原样：tokens-page/token-create(-empty)?/token-table/token-row-<id>/
// token-fingerprint-<id>/token-status-<id>/token-revoke-<id>/token-revoke-byid
// /token-revoke-id/token-revoke-byid-go/tokens-count/tokens-empty/
// tokens-readonly-note/tokens-ledger-note/token-dialog/token-plaintext/
// token-value/token-plaintext-done/token-stepup/token-password(-error)?/
// token-password-submit/token-cancel/token-form-subject/token-form-ttl/
// token-submit/token-mint-error。
import { useEffect, useRef, useState } from 'react'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge, AlertBox } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState } from '@/components/layout/states'
import { Pager, useClientPager } from '@/components/layout/pager'
import { TextInput, NativeSelect } from '@/components/layout/fields'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, apiJSON, apiText, canAdminWrite, errText, isReadOnlyAdmin } from '@/lib/api'
import { tr, getLocale } from '@/i18n'

const t = tr('security')

/** 有效期预设（秒）。closed set——「永不过期」仅 admin 可选（Q11 护栏 2） */
const TTL_PRESETS: { label: string; seconds: number }[] = [
  { label: t('1 小时'), seconds: 3600 },
  { label: t('24 小时'), seconds: 24 * 3600 },
  { label: t('7 天'), seconds: 7 * 24 * 3600 },
  { label: t('30 天'), seconds: 30 * 24 * 3600 },
  { label: t('365 天'), seconds: 365 * 24 * 3600 },
]
const TTL_NEVER = { label: t('永不过期'), seconds: 0 }
const DEFAULT_TTL = 24 * 3600

interface MintResponse {
  access_token: string
  token_type: string
  scope: string
  token_id: number
  /** 缺省 = 永不过期（服务端 0 语义） */
  expires_in?: number
}

/** mint 请求体（E-17 JSON 投影——SetMeUpDialog.mintToken 同源形态） */
function mintToken(opts: {
  expiresIn: number
  username?: string
  stepUpPassword?: string
}): Promise<MintResponse> {
  return apiJSON<MintResponse>('/security/token', {
    method: 'POST',
    body: {
      grant_type: 'client_credentials',
      expires_in: opts.expiresIn,
      ...(opts.username ? { username: opts.username } : {}),
      ...(opts.stepUpPassword ? { step_up_password: opts.stepUpPassword } : {}),
    },
    // step-up 的 401 是对话分支（OAuth 形错误体），不触发全局会话过期监听
    silent401: true,
  })
}

/** 从 ApiError.raw 提取 OAuth 形错误体 */
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

/** sha256 前 8 hex——与服务端 auth.TokenFingerprint / 审计 detail 同 digest
 *  （非安全上下文 crypto.subtle 缺席 → 空串，不伪造） */
async function fingerprintOf(plaintext: string): Promise<string> {
  try {
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(plaintext))
    return Array.from(new Uint8Array(digest).slice(0, 4))
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('')
  } catch {
    return ''
  }
}

function humanTtl(seconds: number): string {
  if (seconds <= 0) return TTL_NEVER.label
  if (seconds % (24 * 3600) === 0) return t('{v1} 天', { v1: seconds / (24 * 3600) })
  if (seconds % 3600 === 0) return t('{v1} 小时', { v1: seconds / 3600 })
  return t('{seconds} 秒', { seconds: seconds })
}

/** 会话台账行（内存态——服务端无清单端点，刷新即空） */
interface TokenRow {
  tokenId: number
  subject: string
  scope: string
  /** 秒；0 = 永不过期 */
  expiresIn: number
  mintedAt: number
  fingerprint: string
  revoked: boolean
}

type MintState =
  | { phase: 'idle' | 'minting' }
  | { phase: 'need-password'; error: string | null; raw: string | null; submitting: boolean }
  | { phase: 'oidc-stepup'; raw: string | null }
  | { phase: 'done'; token: string; tokenId: number; expiresIn: number }
  | { phase: 'error'; message: string; status: number }

export default function TokensPage() {
  const { session } = useAuth()
  const confirm = useConfirm()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const denied = !adminWrite && !readOnly // 普通 user：L2 无权限卡（§3.6.3）

  // 会话台账 + 创建 modal（明文只活在 modal 态——关闭即卸载丢弃）
  const [rows, setRows] = useState<TokenRow[]>([])
  const pager = useClientPager(rows.length, `ledger|${rows.length}`)
  const pageRows = pager.slice(rows)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [busyId, setBusyId] = useState<number | null>(null)
  const [revokeIdInput, setRevokeIdInput] = useState('')
  const [byidBusy, setByidBusy] = useState(false)

  /** 吊销执行（行内与按 ID 共用）：E-18 form 体 token_id；200 "Token not
   *  found" 是幂等文案（可能已吊销）——台账同样翻已吊销态 */
  const runRevoke = async (tokenId: number) => {
    try {
      const text = await apiText('/security/token/revoke', {
        method: 'POST',
        formBody: { token_id: String(tokenId) },
      })
      const found = !text.includes('not found')
      setRows((prev) => prev.map((r) => (r.tokenId === tokenId ? { ...r, revoked: true } : r)))
      toast.success(
        found
          ? t('令牌 #{tokenId} 已吊销', { tokenId: tokenId })
          : t('令牌 #{tokenId} 服务端未找到（可能已被吊销）——按已吊销呈现', { tokenId: tokenId }),
      )
      return true
    } catch (err) {
      const oauth = oauthErrorOf(err)
      toast.error(t('吊销失败：{v1}', { v1: oauth?.description || errText(err) }))
      return false
    }
  }

  const doRevokeRow = async (row: TokenRow) => {
    const ok = await confirm.confirm({
      title: t('吊销令牌 #{v1}', { v1: row.tokenId }),
      description: t('吊销后所有以该令牌认证的客户端（docker/maven/npm/pip、CI 流水线）立即失效。此操作不可撤销（{v1} 主体，有效期 {v2}）。', { v1: row.subject, v2: humanTtl(row.expiresIn) }),
      confirmLabel: t('吊销'),
      danger: true,
    })
    if (!ok) return
    setBusyId(row.tokenId)
    await runRevoke(row.tokenId)
    setBusyId(null)
  }

  const doRevokeById = async () => {
    const raw = revokeIdInput.trim()
    if (!/^\d+$/.test(raw)) return
    const tokenId = Number(raw)
    const ok = await confirm.confirm({
      title: t('吊销令牌 #{tokenId}', { tokenId: tokenId }),
      description: t('按 token_id 吊销一条历史令牌（不在本会话台账内）。吊销后该令牌立即失效，此操作不可撤销。'),
      confirmLabel: t('吊销'),
      danger: true,
    })
    if (!ok) return
    setByidBusy(true)
    const done = await runRevoke(tokenId)
    if (done) setRevokeIdInput('')
    setByidBusy(false)
  }

  if (denied) {
    return (
      <div data-testid="tokens-page">
        <div className="page-header">
          <h2 className="text-lg font-semibold">Access Tokens</h2>
        </div>
        <EmptyState
          message={t('无权限访问 Access Tokens')}
          hint={t('令牌管理页属管理面板（admin / 只读管理员）。普通用户的自助令牌：制品树任意仓库 → Set Me Up（生成接入令牌），或 REST POST /api/security/token。')}
        />
      </div>
    )
  }

  return (
    <div data-testid="tokens-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">Access Tokens</h2>
        <span className="text-aux text-2">{t('自铸 / 吊销 API 令牌（E-17 / E-18；scope 恒 api:*——携带主体全部权限）')}</span>
        <Button size="sm" className="ml-auto" data-testid="token-create" onClick={() => setDialogOpen(true)}>
          {t('生成令牌')}
        </Button>
      </div>

      {readOnly && (
        <AlertBox severity="info" testid="tokens-readonly-note">
          {t('只读管理员：吊销是管理面写动作（服务端以 403 兜底）——已禁用；自铸不受限（Q11 自助面，仅本人 + 有限期）。')}
        </AlertBox>
      )}
      <AlertBox severity="info" testid="tokens-ledger-note">
        {t('下表是')}<b>{t('本会话经此页签发')}</b>{t('的令牌台账——服务端只存指纹、无令牌清单端点（console-ux §9-R6），刷新后表格即空、 明文不可再取。历史令牌的吊销走下方「按 token_id 吊销」；签发/吊销事件可到审计日志以指纹对账。')}
      </AlertBox>

      {rows.length === 0 ? (
        <EmptyState
          illustration
          message={t('本次会话还没有经此页签发的令牌')}
          hint={t('生成后在创建面板一次性展示明文（关闭即不可再取）；本页无服务端清单可回看。')}
          testid="tokens-empty"
          action={
            <Button size="sm" data-testid="token-create-empty" onClick={() => setDialogOpen(true)}>
              {t('生成第一个令牌')}
            </Button>
          }
        />
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full text-dense" data-testid="token-table">
            <thead>
              <tr className="border-b border-border text-left text-aux text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">token_id</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('指纹')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('主体')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('有效期')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('状态')}</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">{t('操作')}</th>
              </tr>
            </thead>
            <tbody>
              {pageRows.map((row) => (
                <tr key={row.tokenId} data-testid={`token-row-${row.tokenId}`} className="border-b border-border/60 hover:bg-accent">
                  <td className="px-3 py-1.5 font-mono" lang="en">#{row.tokenId}</td>
                  <td className="px-3 py-1.5 font-mono" lang="en" data-testid={`token-fingerprint-${row.tokenId}`}>
                    {row.fingerprint || '—'}{' '}
                    {row.fingerprint && <CopyButton value={row.fingerprint} label={t('指纹 #{v1}', { v1: row.tokenId })} />}
                  </td>
                  <td className="px-3 py-1.5 font-mono" lang="en">{row.subject}</td>
                  <td className="px-3 py-1.5">
                    {humanTtl(row.expiresIn)}
                    <div className="text-aux text-muted-foreground">
                      {new Date(row.mintedAt).toLocaleTimeString(getLocale() === 'en' ? 'en-US' : 'zh-CN')}
                    </div>
                  </td>
                  <td className="px-3 py-1.5">
                    {row.revoked ? (
                      <span data-testid={`token-status-${row.tokenId}`}>{t('已吊销')}</span>
                    ) : (
                      <Badge variant="success" testid={`token-status-${row.tokenId}`}>{t('有效')}</Badge>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-3 py-1.5 text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 border-destructive/50 text-destructive hover:bg-destructive/10"
                      data-testid={`token-revoke-${row.tokenId}`}
                      disabled={readOnly || row.revoked || busyId === row.tokenId}
                      title={row.revoked ? t('已吊销') : t('吊销（danger 确认）')}
                      onClick={() => void doRevokeRow(row)}
                    >
                      {busyId === row.tokenId ? t('吊销中…') : t('吊销')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {rows.length > 0 && (
        <div className="mt-1" data-testid="tokens-count">
          <Pager
            page={pager.page}
            pageCount={pager.pageCount}
            onPageChange={pager.setPage}
            from={pager.from}
            to={pager.to}
            total={rows.length}
            note={t('（会话台账 · 含已吊销 {v1}）', { v1: rows.filter((r) => r.revoked).length })}
            pageSize={pager.size}
            onPageSizeChange={pager.setSize}
          />
        </div>
      )}

      {adminWrite && (
        <div className="mt-4 flex flex-wrap items-center gap-2 rounded-md border border-border p-2" data-testid="token-revoke-byid">
          <TextInput
            mono
            lang="en"
            inputMode="numeric"
            placeholder={t('如 42')}
            value={revokeIdInput}
            onChange={(e) => setRevokeIdInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                void doRevokeById()
              }
            }}
            className="min-w-[240px]"
            aria-label={t('按 token_id 吊销（历史令牌）')}
            data-testid="token-revoke-id"
          />
          <Button
            variant="outline"
            size="sm"
            className="border-destructive/50 text-destructive hover:bg-destructive/10"
            data-testid="token-revoke-byid-go"
            disabled={byidBusy || !/^\d+$/.test(revokeIdInput.trim())}
            onClick={() => void doRevokeById()}
          >
            {byidBusy ? t('吊销中…') : t('吊销')}
          </Button>
          <span className="text-aux text-muted-foreground">
            {t('台账外的历史令牌以 id 吊销（console-ux §9-R6 的既有出口；token_id 见审计日志 token.issue 事件）')}
          </span>
        </div>
      )}

      {dialogOpen && (
        <CreateTokenDialog
          adminWrite={adminWrite}
          oidcLeg={session?.source === 'oidc'}
          username={session?.username ?? ''}
          onClose={() => setDialogOpen(false)}
          onMinted={(row) => setRows((prev) => [row, ...prev])}
        />
      )}
    </div>
  )
}

/** 创建 modal。明文只在 done 面板存活，组件随 modal 卸载即丢弃——
 *  「仅展示一次 / 刷新不可再取」由态机保证：无任何持久层持有明文。
 *  step-up 口令腿 = smu-stepup 同形态（内联、不弹第二层）。 */
function CreateTokenDialog({
  adminWrite,
  oidcLeg,
  username,
  onClose,
  onMinted,
}: {
  adminWrite: boolean
  oidcLeg: boolean
  username: string
  onClose: () => void
  onMinted: (row: TokenRow) => void
}) {
  const [ttl, setTtl] = useState(DEFAULT_TTL)
  const [subject, setSubject] = useState('')
  const [password, setPassword] = useState('')
  const passwordRef = useRef<HTMLInputElement>(null)
  const [mint, setMint] = useState<MintState>({ phase: 'idle' })

  useEffect(() => {
    if (mint.phase === 'need-password' && !mint.error) passwordRef.current?.focus()
  }, [mint])

  const presets = adminWrite ? [...TTL_PRESETS, TTL_NEVER] : TTL_PRESETS

  const runMint = async (opts: { password?: string } = {}) => {
    setMint(
      mint.phase === 'need-password'
        ? { phase: 'need-password', error: mint.error, raw: mint.raw, submitting: true }
        : { phase: 'minting' },
    )
    try {
      const onBehalf = subject.trim()
      const res = await mintToken({
        expiresIn: ttl,
        ...(onBehalf && adminWrite ? { username: onBehalf } : {}),
        ...(opts.password ? { stepUpPassword: opts.password } : {}),
      })
      const expiresIn = res.expires_in ?? ttl
      const fingerprint = await fingerprintOf(res.access_token)
      onMinted({
        tokenId: res.token_id,
        subject: onBehalf && adminWrite ? onBehalf : username,
        scope: res.scope,
        expiresIn,
        mintedAt: Date.now(),
        fingerprint,
        revoked: false,
      })
      toast.success(t('令牌 #{v1} 已生成——明文仅此一次展示', { v1: res.token_id }))
      setMint({ phase: 'done', token: res.access_token, tokenId: res.token_id, expiresIn })
    } catch (err) {
      const oauth = oauthErrorOf(err)
      if (err instanceof ApiError && err.status === 401 && oauth) {
        if (oauth.code === 'step_up_required') {
          if (oidcLeg) {
            // OIDC 臂：重认证回跳 + 续铸承载在 Set Me Up——不私建第二条链
            setMint({ phase: 'oidc-stepup', raw: err.raw })
          } else {
            setMint({ phase: 'need-password', error: null, raw: err.raw, submitting: false })
          }
          return
        }
        if (oauth.code === 'step_up_invalid') {
          setMint({
            phase: 'need-password',
            error: oauth.description || err.message,
            raw: err.raw,
            submitting: false,
          })
          return
        }
      }
      setMint({ phase: 'error', message: oauth?.description || errText(err), status: err instanceof ApiError ? err.status : 0 })
    }
  }

  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose() }}>
      <DialogContent className="sm:max-w-[560px]" data-testid="token-dialog">
        <DialogHeader>
          <DialogTitle>
            {mint.phase === 'done'
              ? t('令牌已生成（仅此一次展示）')
              : mint.phase === 'need-password'
                ? t('需要二次口令（step-up）')
                : mint.phase === 'oidc-stepup'
                  ? t('需要重新认证（step-up）')
                  : t('生成 Access Token')}
          </DialogTitle>
        </DialogHeader>
        {mint.phase === 'done' ? (
          <>
            <div className="rounded-md border border-success/40 bg-surface-2 p-3" data-testid="token-plaintext">
              <p className="mb-1 text-dense">
                {t('以')} <span className="font-mono" lang="en">{subject.trim() && adminWrite ? subject.trim() : username}</span>{' '}
                {t('身份签发（token_id')} <span className="font-mono" lang="en">#{mint.tokenId}</span>{t('，有效期')} {humanTtl(mint.expiresIn)}{t('）：')}
              </p>
              <div className="mb-1 flex items-start gap-2">
                <code
                  lang="en"
                  data-testid="token-value"
                  className="flex-1 break-all font-mono text-aux"
                >
                  {mint.token}
                </code>
                <CopyButton value={mint.token} label="API Token" />
              </div>
              <p className="text-aux text-muted-foreground">
                {t('关闭本面板后不可再查看（服务端只存指纹）。CI 与脚本请使用此令牌，不要用控制台口令—— docker login / curl -u / settings.xml / .pypirc 的口令位都填它。')}
              </p>
            </div>
            <DialogFooter className="justify-end">
              <Button data-testid="token-plaintext-done" onClick={onClose}>{t('我已保存，关闭')}</Button>
            </DialogFooter>
          </>
        ) : mint.phase === 'need-password' ? (
          <>
            <AlertBox severity="warning" testid="token-stepup">
              {t('实例开启 auth.token_step_up——非 admin 会话签发令牌需输入当前账号口令后继续（ADR-0027）。')}
            </AlertBox>
            <div className="field">
              <label htmlFor="token-pass">{t('当前账号口令')}</label>
              <TextInput
                id="token-pass"
                type="password"
                ref={passwordRef}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    void runMint({ password })
                  }
                }}
                autoComplete="current-password"
                data-testid="token-password"
              />
            </div>
            {mint.error && (
              <AlertBox severity="error" testid="token-password-error" className="mt-1 [word-break:break-word]" >
                <span lang="en">{mint.error}</span>
              </AlertBox>
            )}
            <details className="mt-1">
              <summary className="cursor-pointer text-aux text-muted-foreground">{t('服务端原文')}</summary>
              <pre lang="en" className="mt-1 break-all font-mono text-aux text-muted-foreground">{mint.raw ?? ''}</pre>
            </details>
            <DialogFooter>
              <Button variant="outline" onClick={onClose} data-testid="token-cancel">{t('取消')}</Button>
              <Button
                data-testid="token-password-submit"
                disabled={mint.submitting || password === ''}
                onClick={() => void runMint({ password })}
              >
                {mint.submitting ? t('验证中…') : t('验证并生成令牌')}
              </Button>
            </DialogFooter>
          </>
        ) : mint.phase === 'oidc-stepup' ? (
          <>
            <AlertBox severity="warning">
              {t('SSO（OIDC）会话签发令牌需到身份提供方重新认证一次。完整的「跳转 IdP 重认证 → 自动续铸」链在 Set Me Up 接入向导内：从制品树任意仓库的 Set Me Up 进入并生成 （上下文会被记住，完成后自动续铸）；或联系管理员评估 auth.token_step_up 配置。')}
            </AlertBox>
            <details>
              <summary className="cursor-pointer text-aux text-muted-foreground">{t('服务端原文')}</summary>
              <pre lang="en" className="mt-1 break-all font-mono text-aux text-muted-foreground">{mint.raw ?? ''}</pre>
            </details>
            <DialogFooter>
              <Button variant="outline" onClick={onClose} data-testid="token-cancel">{t('关闭')}</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <div className="flex flex-col gap-4">
              {adminWrite && (
                <div className="field">
                  <label htmlFor="token-subject">{t('签发对象（可选——代人签发）')}</label>
                  <TextInput
                    id="token-subject"
                    mono
                    lang="en"
                    placeholder={username}
                    value={subject}
                    onChange={(e) => setSubject(e.target.value)}
                    data-testid="token-form-subject"
                  />
                  <p className="field-hint">{t('留空 = 为自己签发；填其他用户名 = 以该用户主体签发（admin 专属，Q11）')}</p>
                </div>
              )}
              <div className="field">
                <label htmlFor="token-ttl">{t('有效期')}</label>
                <NativeSelect
                  id="token-ttl"
                  value={String(ttl)}
                  onChange={(e) => setTtl(Number(e.target.value))}
                  options={presets.map((p) => ({ value: String(p.seconds), label: p.label }))}
                  data-testid="token-form-ttl"
                />
                <p className="field-hint">
                  {adminWrite
                    ? t('「永不过期」仅管理员可选；非 admin 主体有限期且受 auth.token_nonadmin_max_ttl 上限约束')
                    : t('非 admin 签发为有限期（Q11 护栏：≤ auth.token_nonadmin_max_ttl，默认 365 天）')}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <Badge mono lang="en">api:*</Badge>
                <span className="text-aux text-muted-foreground">
                  {t('scope 固定（只读说明）：令牌携带主体全部权限——scope 参数仅经校验、不收窄权限域，故不设选项（端点亦无描述字段，token_id 即标识）')}
                </span>
              </div>
              <div className="border-t border-border" />
              <p className="text-aux text-muted-foreground">
                {t('生成后明文只在结果面板展示一次（关闭即不可再取——服务端只存指纹）；签发动作记入审计日志。')}
              </p>
              {mint.phase === 'error' && (
                <AlertBox severity="error" testid="token-mint-error">
                  {t('签发失败（HTTP')} {mint.status}{t('）：')}{mint.message}
                </AlertBox>
              )}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={onClose} data-testid="token-cancel">{t('取消')}</Button>
              <Button data-testid="token-submit" disabled={mint.phase === 'minting'} onClick={() => void runMint()}>
                {mint.phase === 'minting' ? t('生成中…') : t('生成令牌')}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
