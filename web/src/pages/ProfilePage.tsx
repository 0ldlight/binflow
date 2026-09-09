// 编辑档案（console-m8 §6.5——P3 新栈重写）：
// - 认证设置 = 修改口令（authenticated 全员可用；PUT /api/security/password
//   纯文本层错误统一 message 行内——password-* 锚随表单迁移不变）。
// - Identity Token = 自助签发真身（T-457 / FR-145.4）：E-17 同源引擎；本页
//   自铸臂只为自己签发（无代人签发）；明文一次性；step-up 两臂（口令腿内
//   联、OIDC 腿诚实降级引导）。
// - SSH Keys = 如实缺位（后端无 SSH 公钥端点——契约缺口 §9-R11 登记，
//   不伪造增删入口）。
// 锚族原样：profile-page/profile-password/password-{old,new,confirm,error,
// submit}/profile-token(-generate|-dialog|-ttl|-submit|-cancel|-stepup|
// -password(-error|-submit)?|-plaintext|-value|-curl|-id|-done|-error|-goto|
// -docs)?/profile-ssh(-gap)?。
import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertBox, Badge } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { TextInput, NativeSelect } from '@/components/layout/fields'
import { toast } from '@/lib/toast'
import { ApiError, apiJSON, apiText, canAdminWrite, errText } from '@/lib/api'
import { tr } from '@/i18n'

const t = tr('console')

function PasswordSection() {
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const canSubmit = oldPw !== '' && newPw !== '' && confirmPw !== '' && !saving

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    if (newPw !== confirmPw) {
      setError(t('两次输入的新口令不一致'))
      return
    }
    setSaving(true)
    try {
      await apiText('/security/password', {
        method: 'PUT',
        body: { oldPassword: oldPw, newPassword: newPw },
      })
      toast.success(t('口令修改成功'))
      setOldPw('')
      setNewPw('')
      setConfirmPw('')
    } catch (err) {
      // 服务端纯文本层文案原样行内呈现
      setError(errText(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="card section" data-testid="profile-password">
      <h3 className="mb-2 text-dense font-semibold">{t('认证设置 · 修改口令')}</h3>
      <form onSubmit={(e) => void onSubmit(e)}>
        <div className="flex max-w-[420px] flex-col gap-3">
          <div className="field">
            <label htmlFor="pw-old">{t('当前口令')}</label>
            <TextInput
              id="pw-old"
              type="password"
              value={oldPw}
              onChange={(e) => setOldPw(e.target.value)}
              autoComplete="current-password"
              data-testid="password-old"
            />
          </div>
          <div className="field">
            <label htmlFor="pw-new">{t('新口令')}</label>
            <TextInput
              id="pw-new"
              type="password"
              value={newPw}
              onChange={(e) => setNewPw(e.target.value)}
              autoComplete="new-password"
              data-testid="password-new"
            />
          </div>
          <div className="field">
            <label htmlFor="pw-confirm">{t('确认新口令')}</label>
            <TextInput
              id="pw-confirm"
              type="password"
              value={confirmPw}
              onChange={(e) => setConfirmPw(e.target.value)}
              autoComplete="new-password"
              data-testid="password-confirm"
            />
          </div>
          {error && (
            <p className="field-error" data-testid="password-error" role="alert">
              {error}
            </p>
          )}
          <Button type="submit" data-testid="password-submit" disabled={!canSubmit}>
            {saving ? t('保存中…') : t('修改口令')}
          </Button>
        </div>
      </form>
    </section>
  )
}

// ---- Identity Token 自助签发（T-457 / FR-145.4）----

/** 有效期预设（秒）——自铸面闭集；「永不过期」仅 admin 可选（Q11 护栏 2） */
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

/** mint 请求体（E-17 JSON 投影）：自铸臂恒不携 username（只为自己签发） */
function mintToken(opts: { expiresIn: number; stepUpPassword?: string }): Promise<MintResponse> {
  return apiJSON<MintResponse>('/security/token', {
    method: 'POST',
    body: {
      grant_type: 'client_credentials',
      expires_in: opts.expiresIn,
      ...(opts.stepUpPassword ? { step_up_password: opts.stepUpPassword } : {}),
    },
    // step-up 的 401 是对话分支（ADR-0027 决策 5），不触发全局会话过期监听
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

function humanTtl(seconds: number): string {
  if (seconds <= 0) return TTL_NEVER.label
  if (seconds % (24 * 3600) === 0) return t('{v1} 天', { v1: seconds / (24 * 3600) })
  if (seconds % 3600 === 0) return t('{v1} 小时', { v1: seconds / 3600 })
  return t('{seconds} 秒', { seconds: seconds })
}

type MintState =
  | { phase: 'idle' | 'minting' }
  | { phase: 'need-password'; error: string | null; raw: string | null; submitting: boolean }
  | { phase: 'oidc-stepup'; raw: string | null }
  | { phase: 'done'; token: string; tokenId: number; expiresIn: number }
  | { phase: 'error'; message: string; status: number }

/** 生成 modal：明文只在 done 面板存活，组件卸载即丢弃；step-up 口令腿内联 */
function GenerateTokenDialog({
  adminWrite,
  oidcLeg,
  username,
  onClose,
}: {
  adminWrite: boolean
  oidcLeg: boolean
  username: string
  onClose: () => void
}) {
  const [ttl, setTtl] = useState(DEFAULT_TTL)
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
      const res = await mintToken({
        expiresIn: ttl,
        ...(opts.password ? { stepUpPassword: opts.password } : {}),
      })
      setMint({ phase: 'done', token: res.access_token, tokenId: res.token_id, expiresIn: res.expires_in ?? ttl })
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

  /** 即用 curl 样例（Basic 双臂：口令位即令牌） */
  const curlSample =
    mint.phase === 'done'
      ? `curl -u ${username}:${mint.token} ${window.location.origin}/binflow/api/v1/session`
      : ''

  const title =
    mint.phase === 'done'
      ? t('Identity Token 已生成（仅此一次展示）')
      : mint.phase === 'need-password'
        ? t('需要二次口令（step-up）')
        : mint.phase === 'oidc-stepup'
          ? t('需要重新认证（step-up）')
          : t('生成 Identity Token')

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-[560px]" data-testid="profile-token-dialog">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        {mint.phase === 'done' ? (
          <>
            <div className="rounded-md border border-success/40 bg-surface-2 p-3" data-testid="profile-token-plaintext">
              <p className="mb-1 text-dense">
                {t('以')} <span className="font-mono" lang="en">{username}</span> {t('身份签发（token_id')}{' '}
                <span className="font-mono" lang="en" data-testid="profile-token-id">#{mint.tokenId}</span>
                {t('，有效期')} {humanTtl(mint.expiresIn)}{t('）：')}
              </p>
              <div className="mb-1 flex items-start gap-2">
                <code lang="en" data-testid="profile-token-value" className="flex-1 break-all font-mono text-aux">
                  {mint.token}
                </code>
                <CopyButton value={mint.token} label="Identity Token" />
              </div>
              <div className="my-2 border-t border-border" />
              <div className="mb-1 flex items-start gap-2">
                <code lang="en" data-testid="profile-token-curl" className="flex-1 break-all font-mono text-aux">
                  {curlSample}
                </code>
                <CopyButton value={curlSample} label={t('curl 即用样例')} />
              </div>
              <p className="text-aux text-muted-foreground">
                {t('关闭本面板后不可再查看（服务端只存指纹）。CI 与脚本请使用此令牌，不要用控制台口令—— docker login / curl -u / settings.xml / .pypirc 的口令位都填它；吊销联系管理员（按 token_id）。')}
              </p>
            </div>
            <DialogFooter className="justify-end">
              <Button data-testid="profile-token-done" onClick={onClose}>{t('我已保存，关闭')}</Button>
            </DialogFooter>
          </>
        ) : mint.phase === 'need-password' ? (
          <>
            <AlertBox severity="warning" testid="profile-token-stepup">
              {t('实例开启 auth.token_step_up——非 admin 会话签发令牌需输入当前账号口令后继续（ADR-0027）。')}
            </AlertBox>
            <div className="field">
              <label htmlFor="pt-pass">{t('当前账号口令')}</label>
              <TextInput
                id="pt-pass"
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
                data-testid="profile-token-password"
              />
            </div>
            {mint.error && (
              <AlertBox severity="error" testid="profile-token-password-error" className="mt-1 [word-break:break-word]">
                <span lang="en">{mint.error}</span>
              </AlertBox>
            )}
            <DialogFooter>
              <Button variant="outline" onClick={onClose} data-testid="profile-token-cancel">{t('取消')}</Button>
              <Button
                data-testid="profile-token-password-submit"
                disabled={mint.submitting || password === ''}
                onClick={() => void runMint({ password })}
              >
                {mint.submitting ? t('验证中…') : t('验证并生成')}
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
              <Button variant="outline" onClick={onClose} data-testid="profile-token-cancel">{t('关闭')}</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <div className="flex flex-col gap-4">
              <div className="field">
                <label htmlFor="pt-ttl">{t('有效期')}</label>
                <NativeSelect
                  id="pt-ttl"
                  value={String(ttl)}
                  onChange={(e) => setTtl(Number(e.target.value))}
                  options={presets.map((p) => ({ value: String(p.seconds), label: p.label }))}
                  data-testid="profile-token-ttl"
                />
                <p className="field-hint">
                  {adminWrite
                    ? t('「永不过期」仅管理员可选；非 admin 签发为有限期且受 auth.token_nonadmin_max_ttl 上限约束')
                    : t('自助签发为有限期（Q11 护栏：≤ auth.token_nonadmin_max_ttl，默认 365 天）')}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <Badge mono lang="en">api:*</Badge>
                <span className="text-aux text-muted-foreground">{t('scope 固定（只读说明）：令牌携带本人全部权限——scope 参数仅经校验、不收窄权限域')}</span>
              </div>
              <div className="border-t border-border" />
              <p className="text-aux text-muted-foreground">{t('生成后明文只在结果面板展示一次（关闭即不可再取——服务端只存指纹）；签发动作记入审计日志。')}</p>
              {mint.phase === 'error' && (
                <AlertBox severity="error" testid="profile-token-error">
                  {t('签发失败（HTTP')} {mint.status}{t('）：')}{mint.message}
                </AlertBox>
              )}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={onClose} data-testid="profile-token-cancel">{t('取消')}</Button>
              <Button data-testid="profile-token-submit" disabled={mint.phase === 'minting'} onClick={() => void runMint()}>
                {mint.phase === 'minting' ? t('生成中…') : t('生成')}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

function IdentityTokenSection() {
  const { session } = useAuth()
  const adminWrite = canAdminWrite(session)
  const [dialogOpen, setDialogOpen] = useState(false)

  return (
    <section className="card section" data-testid="profile-token">
      <h3 className="mb-2 text-dense font-semibold">{t('Identity Token · 自助签发')}</h3>
      <p className="text-2">
        {t('CI 与脚本请使用 Identity Token——本页直接为自己签发（B-1.8：不再指到管理面）。 明文仅生成时展示一次，关闭后不可再取（服务端只存指纹）；实例开启 step-up 时非 admin 需二次口令。吊销与代人签发属管理面（')}
        <Link to="/admin/security/tokens" data-testid="profile-token-goto" className="text-primary underline underline-offset-2">
          {t('Access Tokens 页')}
        </Link>
        {t('）。')}
      </p>
      <p>
        <a href="/binflow/docs/api-reference" target="_blank" rel="noopener noreferrer" data-testid="profile-token-docs" className="text-primary underline underline-offset-2">
          {t('查看文档')}
        </a>
      </p>
      <div className="flex items-center gap-2">
        <Button data-testid="profile-token-generate" onClick={() => setDialogOpen(true)}>{t('生成 Identity Token')}</Button>
        <span className="text-aux text-muted-foreground">{t('默认 24 小时；管理面台账与按 token_id 吊销见 Access Tokens 页')}</span>
      </div>
      {dialogOpen && (
        <GenerateTokenDialog
          adminWrite={adminWrite}
          oidcLeg={session?.source === 'oidc'}
          username={session?.username ?? ''}
          onClose={() => setDialogOpen(false)}
        />
      )}
    </section>
  )
}

/** SSH Keys 卡（T-457）：后端无 SSH 公钥端点——契约缺口 §9-R11 登记，如实缺位 */
function SshKeysSection() {
  return (
    <section className="card section" data-testid="profile-ssh">
      <h3 className="mb-2 text-dense font-semibold">SSH Keys</h3>
      <AlertBox severity="info" testid="profile-ssh-gap">
        {t('后端尚无 SSH 公钥端点（console-ux §9-R11 契约缺口）——如实缺位，不在此伪造增删入口； 端点落地后本卡提供 Key 别名 / 公钥的登记与删除（对位 7.161 Profile 的 Add New SSH Key）。')}
      </AlertBox>
    </section>
  )
}

export default function ProfilePage() {
  return (
    <div data-testid="profile-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('编辑档案')}</h2>
      </div>
      <PasswordSection />
      <IdentityTokenSection />
      <SshKeysSection />
    </div>
  )
}
