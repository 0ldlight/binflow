import { useEffect, useRef, useState } from 'react'
import type { ComponentPropsWithoutRef, FormEvent } from 'react'
import { Link as RouterLink } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import MuiLink from '@mui/material/Link'
import Chip from '@mui/material/Chip'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import Divider from '@mui/material/Divider'
import Paper from '@mui/material/Paper'
import Select from '@mui/material/Select'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'

import { useAuth } from '../app/AuthContext'
import { useToast } from '../app/ToastContext'
import { CopyButton } from '../components/CopyButton'
import { ApiError, apiJSON, apiText, canAdminWrite, errText } from '../lib/api'
import { monoInputSx } from '../lib/muiAtoms'
import { tr } from '../i18n'

const t = tr('console')

// 编辑档案（console-m8 §6.5——Artifactory /ui/user_profile 的 BinFlow
// 对齐面，T-239 自设置页拆分）：
//
// - 认证设置 = 修改口令（自 /settings 平移，authenticated 全员可用；
//   PUT /api/security/password 的错误体走用户管理纯文本层，统一由 api
//   层解析成 message 行内呈现——auth-shell W 腿的 password-* 锚随表单
//   整体迁址，锚名不变）。
// - Identity Token = 自助签发真身（T-457 / FR-145.4——parity B-1.8 翻正：
//   「签发指到 admin Tokens 页」的文档化设计推翻，本页内完成）。消费
//   面 = 既有 E-17 POST /api/security/token（JSON 投影 + silent401，与
//   SetMeUpDialog / TokensPage 同源同引擎——本页自铸臂只为自己签发，
//   无「代人签发」）；明文一次性（关弹窗即不可再取——服务端只存指纹，
//   console-ux §9-R6）；step-up 两臂（口令腿内联、OIDC 腿诚实降级引导，
//   ADR-0027——与 TokensPage 同形态）。管理面 Access Tokens 页继续承载
//   台账/按 id 吊销/代人签发（E-18 吊销是 CapSecurityWrite 管理面写
//   动作，自助面不越权摆入口）。
// - SSH Keys = 如实缺位（T-457：后端无 SSH 公钥端点——契约缺口 §9-R11
//   登记，不伪造增删入口）。
// T-344 批 D：残面换装——.card → Paper；.field + 裸 input + 手写 label →
// TextField 浮标 label（锚 password-* 落 input 本体）；.btn → Button。
// Tab 序（旧口令 → 新口令 → 确认 → 提交，浮标 label 非可聚焦元素）与
// Enter 隐式提交（原生 form + type=submit）零变化。

function PasswordSection() {
  const toast = useToast()
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
      // 服务端纯文本层文案（如 Incorrect username/password / New password
      // has to be different from the old one）原样行内呈现
      setError(errText(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Paper component="section" className="card section" elevation={1} data-testid="profile-password">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>{t('认证设置 · 修改口令')}      </Typography>
      <form onSubmit={(e) => void onSubmit(e)}>
        <Stack sx={{ gap: 'var(--bf-sp-3)', maxWidth: 420 }}>
          <TextField
            label={t('当前口令')}
            size="small"
            type="password"
            value={oldPw}
            onChange={(e) => setOldPw(e.target.value)}
            slotProps={{ htmlInput: { 'data-testid': 'password-old', autoComplete: 'current-password' } }}
          />
          <TextField
            label={t('新口令')}
            size="small"
            type="password"
            value={newPw}
            onChange={(e) => setNewPw(e.target.value)}
            slotProps={{ htmlInput: { 'data-testid': 'password-new', autoComplete: 'new-password' } }}
          />
          <TextField
            label={t('确认新口令')}
            size="small"
            type="password"
            value={confirmPw}
            onChange={(e) => setConfirmPw(e.target.value)}
            slotProps={{ htmlInput: { 'data-testid': 'password-confirm', autoComplete: 'new-password' } }}
          />
          {error && (
            <p className="field-error" data-testid="password-error" role="alert">
              {error}
            </p>
          )}
          <Button type="submit" variant="contained" data-testid="password-submit" disabled={!canSubmit}>
            {saving ? t('保存中…') : t('修改口令')}
          </Button>
        </Stack>
      </form>
    </Paper>
  )
}

// ---- Identity Token 自助签发（T-457 / FR-145.4）----

/** 有效期预设（秒）——与 TokensPage 同档（自铸面闭集，不发明语义）；
 * 「永不过期」仅 admin 可选（Q11 护栏 2：非 admin 只能有限 TTL 且 ≤
 * auth.token_nonadmin_max_ttl，缺省 365 天）。 */
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

/** mint 请求体（E-17 JSON 投影——SetMeUpDialog.mintToken / TokensPage
 * 同源形态；页内副本：两载体是锚冻结/管理面域，不为本票改动）。自铸臂
 * 恒不携 username（只为自己签发——代人签发是管理面 Access Tokens 页的
 * admin 专属面）。 */
function mintToken(opts: { expiresIn: number; stepUpPassword?: string }): Promise<MintResponse> {
  return apiJSON<MintResponse>('/security/token', {
    method: 'POST',
    body: {
      grant_type: 'client_credentials',
      expires_in: opts.expiresIn,
      ...(opts.stepUpPassword ? { step_up_password: opts.stepUpPassword } : {}),
    },
    // step-up 的 401 是对话分支（ADR-0027 决策 5 的 OAuth 形错误体），
    // 不触发全局「会话过期」监听
    silent401: true,
  })
}

/** 从 ApiError.raw 提取 OAuth 形错误体（TokensPage 同源形态） */
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

/** 生成 modal（Dialog sm）。明文只在 done 面板存活，组件随 modal 卸载
 * 即丢弃——「仅展示一次 / 关闭不可再取」由态机保证：无任何持久层持有
 * 明文（7.161 形态）。step-up 口令腿 = 内联（不弹第二层）。 */
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
            // OIDC 臂（ADR-0027 决策 8）：无本地口令。重认证回跳 + 续铸的
            // 承载在 Set Me Up（AppShell resume 链）——本页不私建第二条链，
            // 诚实降级为引导（见下方面板文案）
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

  /** 即用 curl 样例（一次性面板伴随呈现——Basic 双臂：口令位即令牌，
   * auth-model §3.6；与 SetMeUp 文档口径一致） */
  const curlSample =
    mint.phase === 'done'
      ? `curl -u ${username}:${mint.token} ${window.location.origin}/binflow/api/v1/session`
      : ''

  return (
    <Dialog open onClose={onClose} maxWidth="sm" fullWidth data-testid="profile-token-dialog" aria-labelledby="profile-token-dialog-title">
      {mint.phase === 'done' ? (
        <>
          <DialogTitle id="profile-token-dialog-title">{t('Identity Token 已生成（仅此一次展示）')}</DialogTitle>
          <DialogContent dividers>
            <Paper
              variant="outlined"
              data-testid="profile-token-plaintext"
              sx={{ p: 1.5, borderColor: 'success.light', bgcolor: 'background.default' }}
            >
              <Typography variant="body2" sx={{ mb: 1 }}>{t('以')} <span className="mono" lang="en">{username}</span> {t('身份签发（token_id')}{' '}
                <span className="mono" lang="en" data-testid="profile-token-id">
                  #{mint.tokenId}
                </span>{t('，有效期')} {humanTtl(mint.expiresIn)}{t('）：')}              </Typography>
              <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1, mb: 1 }}>
                <Typography
                  component="code"
                  lang="en"
                  data-testid="profile-token-value"
                  sx={{ fontFamily: 'var(--bf-mono)', fontSize: 'var(--bf-fs-aux)', wordBreak: 'break-all', flex: 1 }}
                >
                  {mint.token}
                </Typography>
                <CopyButton value={mint.token} label="Identity Token" />
              </Box>
              <Divider sx={{ my: 1 }} />
              <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1, mb: 1 }}>
                <Typography
                  component="code"
                  lang="en"
                  data-testid="profile-token-curl"
                  sx={{ fontFamily: 'var(--bf-mono)', fontSize: 'var(--bf-fs-aux)', wordBreak: 'break-all', flex: 1 }}
                >
                  {curlSample}
                </Typography>
                <CopyButton value={curlSample} label={t('curl 即用样例')} />
              </Box>
              <Typography variant="caption" color="text.secondary">{t('关闭本面板后不可再查看（服务端只存指纹）。CI 与脚本请使用此令牌，不要用控制台口令—— docker login / curl -u / settings.xml / .pypirc 的口令位都填它；吊销联系管理员（按 token_id）。')}              </Typography>
            </Paper>
          </DialogContent>
          <DialogActions>
            <Box sx={{ flexGrow: 1 }} />
            <Button variant="contained" data-testid="profile-token-done" onClick={onClose}>{t('我已保存，关闭')}            </Button>
          </DialogActions>
        </>
      ) : mint.phase === 'need-password' ? (
        <>
          <DialogTitle id="profile-token-dialog-title">{t('需要二次口令（step-up）')}</DialogTitle>
          <DialogContent dividers>
            <Alert severity="warning" data-testid="profile-token-stepup" sx={{ mb: 1 }}>{t('实例开启 auth.token_step_up——非 admin 会话签发令牌需输入当前账号口令后继续（ADR-0027）。')}            </Alert>
            <TextField
              label={t('当前账号口令')}
              type="password"
              autoFocus
              inputRef={passwordRef}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  void runMint({ password })
                }
              }}
              fullWidth
              slotProps={{ htmlInput: { 'data-testid': 'profile-token-password', autoComplete: 'current-password' } }}
            />
            {mint.error && (
              <Alert severity="error" data-testid="profile-token-password-error" role="alert" sx={{ mt: 1 }} lang="en">
                {mint.error}
              </Alert>
            )}
          </DialogContent>
          <DialogActions>
            <Button onClick={onClose} data-testid="profile-token-cancel">{t('取消')}            </Button>
            <Button
              variant="contained"
              data-testid="profile-token-password-submit"
              disabled={mint.submitting || password === ''}
              onClick={() => void runMint({ password })}
            >
              {mint.submitting ? t('验证中…') : t('验证并生成')}
            </Button>
          </DialogActions>
        </>
      ) : mint.phase === 'oidc-stepup' ? (
        <>
          <DialogTitle id="profile-token-dialog-title">{t('需要重新认证（step-up）')}</DialogTitle>
          <DialogContent dividers>
            <Alert severity="warning" sx={{ mb: 1 }}>{t('SSO（OIDC）会话签发令牌需到身份提供方重新认证一次。完整的「跳转 IdP 重认证 → 自动续铸」链在 Set Me Up 接入向导内：从制品树任意仓库的 Set Me Up 进入并生成 （上下文会被记住，完成后自动续铸）；或联系管理员评估 auth.token_step_up 配置。')}            </Alert>
            <details>
              <summary>
                <Typography variant="caption" color="text.secondary" component="span">{t('服务端原文')}                </Typography>
              </summary>
              <Typography
                component="pre"
                lang="en"
                sx={{ fontFamily: 'var(--bf-mono)', fontSize: 'var(--bf-fs-aux)', color: 'text.secondary', wordBreak: 'break-all', m: 0 }}
              >
                {mint.raw ?? ''}
              </Typography>
            </details>
          </DialogContent>
          <DialogActions>
            <Button onClick={onClose} data-testid="profile-token-cancel">{t('关闭')}            </Button>
          </DialogActions>
        </>
      ) : (
        <>
          <DialogTitle id="profile-token-dialog-title">{t('生成 Identity Token')}</DialogTitle>
          <DialogContent dividers sx={{ display: 'grid', gap: 2, pt: 1 }}>
            <TextField
              label={t('有效期')}
              select
              value={String(ttl)}
              onChange={(e) => setTtl(Number(e.target.value))}
              helperText={
                adminWrite
                  ? t('「永不过期」仅管理员可选；非 admin 签发为有限期且受 auth.token_nonadmin_max_ttl 上限约束')
                  : t('自助签发为有限期（Q11 护栏：≤ auth.token_nonadmin_max_ttl，默认 365 天）')
              }
              sx={monoInputSx}
              slotProps={{
                select: {
                  native: true,
                  inputProps: { 'data-testid': 'profile-token-ttl' } as ComponentPropsWithoutRef<'select'>,
                } as ComponentPropsWithoutRef<typeof Select>,
              }}
            >
              {presets.map((p) => (
                <option key={p.seconds} value={String(p.seconds)}>
                  {p.label}
                </option>
              ))}
            </TextField>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Chip size="small" variant="outlined" label="api:*" lang="en" />
              <Typography variant="caption" color="text.secondary">{t('scope 固定（只读说明）：令牌携带本人全部权限——scope 参数仅经校验、不收窄权限域')}              </Typography>
            </Box>
            <Divider />
            <Typography variant="caption" color="text.secondary">{t('生成后明文只在结果面板展示一次（关闭即不可再取——服务端只存指纹）；签发动作记入审计日志。')}            </Typography>
            {mint.phase === 'error' && (
              <Alert severity="error" role="alert" data-testid="profile-token-error">{t('签发失败（HTTP')} {mint.status}{t('）：')}{mint.message}
              </Alert>
            )}
          </DialogContent>
          <DialogActions>
            <Button onClick={onClose} data-testid="profile-token-cancel">{t('取消')}            </Button>
            <Button
              variant="contained"
              data-testid="profile-token-submit"
              disabled={mint.phase === 'minting'}
              onClick={() => void runMint()}
            >
              {mint.phase === 'minting' ? t('生成中…') : t('生成')}
            </Button>
          </DialogActions>
        </>
      )}
    </Dialog>
  )
}

function IdentityTokenSection() {
  const { session } = useAuth()
  const adminWrite = canAdminWrite(session)
  const [dialogOpen, setDialogOpen] = useState(false)

  return (
    <Paper component="section" className="card section" elevation={1} data-testid="profile-token">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>{t('Identity Token · 自助签发')}      </Typography>
      <p className="text-2">{t('CI 与脚本请使用 Identity Token——本页直接为自己签发（B-1.8：不再指到管理面）。 明文仅生成时展示一次，关闭后不可再取（服务端只存指纹）；实例开启 step-up 时非 admin 需二次口令。吊销与代人签发属管理面（')}        <MuiLink
          component={RouterLink}
          to="/admin/security/tokens"
          data-testid="profile-token-goto"
          sx={{ textDecoration: 'underline', textUnderlineOffset: 2 }}
        >{t('Access Tokens 页')}        </MuiLink>{t('）。')}      </p>
      <p>
        <a href="/binflow/docs/api-reference" target="_blank" rel="noopener noreferrer" data-testid="profile-token-docs">{t('查看文档')}        </a>
      </p>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
        <Button variant="contained" data-testid="profile-token-generate" onClick={() => setDialogOpen(true)}>{t('生成 Identity Token')}        </Button>
        <Typography variant="caption" color="text.secondary">{t('默认 24 小时；管理面台账与按 token_id 吊销见 Access Tokens 页')}        </Typography>
      </Stack>
      {dialogOpen && (
        <GenerateTokenDialog
          adminWrite={adminWrite}
          oidcLeg={session?.source === 'oidc'}
          username={session?.username ?? ''}
          onClose={() => setDialogOpen(false)}
        />
      )}
    </Paper>
  )
}

/** SSH Keys 卡（T-457）：后端无 SSH 公钥端点——契约缺口 §9-R11 登记，
 * 如实缺位（无增删表单、无影子入口；7.161 活体 = Add New SSH Key + 别名/
 * 签名两列表——承载须 BE 先立端点）。 */
function SshKeysSection() {
  return (
    <Paper component="section" className="card section" elevation={1} data-testid="profile-ssh">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
        SSH Keys
      </Typography>
      <Alert severity="info" data-testid="profile-ssh-gap">{t('后端尚无 SSH 公钥端点（console-ux §9-R11 契约缺口）——如实缺位，不在此伪造增删入口； 端点落地后本卡提供 Key 别名 / 公钥的登记与删除（对位 7.161 Profile 的 Add New SSH Key）。')}      </Alert>
    </Paper>
  )
}

export default function ProfilePage() {
  return (
    <div data-testid="profile-page">
      <div className="page-header">
        <h2>{t('编辑档案')}</h2>
      </div>
      <PasswordSection />
      <IdentityTokenSection />
      <SshKeysSection />
    </div>
  )
}
