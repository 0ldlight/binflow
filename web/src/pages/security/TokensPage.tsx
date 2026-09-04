import { useEffect, useRef, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import Divider from '@mui/material/Divider'
import Paper from '@mui/material/Paper'
import Select from '@mui/material/Select'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { Pager, useClientPager } from '../../components/Pager'
import { ApiError, apiJSON, apiText, canAdminWrite, errText, isReadOnlyAdmin } from '../../lib/api'
import { monoInputSx } from '../../lib/muiAtoms'

// Access Tokens 页真身（M14 T-386，FR-125.1——parity §3 M3；占位页载体
// 退役）。参照形态 = T-381 V6 实测：Artifactory token 面 = 生成区 + Identity
// Tokens 表（4 列 + 列选器）；V6c 生成表单字段集因 profile 密码门降级未核验
// （Q4 终裁未开）——本页以暂行字段集起步（有效期/签发对象/scope 只读说明），
// 票内留痕 reports/agents/T-386.md，终裁后随 T-395/K58 回写。
//
// [零新端点纪律]（AC2）消费面 = 既有 token REST 两端点，仅此两枚：
//   - POST /api/security/token（E-17，JSON 投影 + silent401）——mint/step-up
//     链与 SetMeUpDialog 同源同引擎（ADR-0027：非 admin web session 臂在
//     auth.token_step_up 开启时 401 step_up_required → 内联口令框；OIDC 臂
//     本页无重认证回跳承载——诚实降级为引导，不私建链路）；
//   - POST /api/security/token/revoke（E-18，form 体 token_id）。
// 服务端只存指纹、无令牌清单端点（console-ux §9-R6）——「Identity Tokens
// 表」如实降形为本会话经此页签发的台账（内存态，刷新即空；与明文一次性
// 语义同源），历史令牌吊销走「按 token_id 吊销」。行内指纹 = sha256 前
// 8 hex（与审计 token.issue 事件的 fingerprint 同digest，可对账）。
//
// [四态] 表格无取数（无 GET 端点）→ 不伪造骨架/错误态（T-387「无端点列
// 不伪造」同款纪律）；空态 = 会话台账空；错误态 = mint/吊销就地内联 + toast。
//
// [角色臂] admin 全量；readonly_admin 只读臂（L06）＝吊销禁用（服务端
// CapSecurityWrite 403 兜底）+ 只读注记——自铸不受限（Q11：mint 是
// required-only 自助面，与 Set Me Up 同门）；普通 user = L2 无权限卡
// （导航隐藏；自助铸币走 Set Me Up / REST——提示语指向）。

/** 有效期预设（秒）。closed set——不发明语义；「永不过期」仅 admin 可选
 *  （Q11 护栏 2：非 admin 只能有限 TTL 且 ≤ auth.token_nonadmin_max_ttl）。 */
const TTL_PRESETS: { label: string; seconds: number }[] = [
  { label: '1 小时', seconds: 3600 },
  { label: '24 小时', seconds: 24 * 3600 },
  { label: '7 天', seconds: 7 * 24 * 3600 },
  { label: '30 天', seconds: 30 * 24 * 3600 },
  { label: '365 天', seconds: 365 * 24 * 3600 },
]
const TTL_NEVER = { label: '永不过期', seconds: 0 }
const DEFAULT_TTL = 24 * 3600

interface MintResponse {
  access_token: string
  token_type: string
  scope: string
  token_id: number
  /** 缺省 = 永不过期（服务端 0 语义） */
  expires_in?: number
}

/** mint 请求体（E-17 JSON 投影——SetMeUpDialog.mintToken 同源形态；同源
 * 引擎页内副本：SetMeUpDialog 是锚冻结载体，不为此票改动） */
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
    // step-up 的 401 是对话分支（ADR-0027 决策 5 的 OAuth 形错误体），
    // 不触发全局「会话过期」监听
    silent401: true,
  })
}

/** 从 ApiError.raw 提取 OAuth 形错误体（SetMeUpDialog 同源形态） */
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
 * （非安全上下文 crypto.subtle 缺席 → 空串，不伪造） */
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
  if (seconds % (24 * 3600) === 0) return `${seconds / (24 * 3600)} 天`
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`
  return `${seconds} 秒`
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
  const toast = useToast()
  const confirm = useConfirm()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const denied = !adminWrite && !readOnly // 普通 user：L2 无权限卡（§3.6.3）

  // 会话台账 + 创建 modal（明文只活在 modal 态——关闭即卸载丢弃）
  const [rows, setRows] = useState<TokenRow[]>([])
  // T-451（E2 翻案）：客户端页窗（会话台账行集——吊销翻态不改行数，
  // 字符串键口径下不丢页位）
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
          ? `令牌 #${tokenId} 已吊销`
          : `令牌 #${tokenId} 服务端未找到（可能已被吊销）——按已吊销呈现`,
      )
      return true
    } catch (err) {
      const oauth = oauthErrorOf(err)
      toast.error(`吊销失败：${oauth?.description || errText(err)}`)
      return false
    }
  }

  const doRevokeRow = async (row: TokenRow) => {
    const ok = await confirm({
      title: `吊销令牌 #${row.tokenId}`,
      body: `吊销后所有以该令牌认证的客户端（docker/maven/npm/pip、CI 流水线）立即失效。此操作不可撤销（${
        row.subject
      } 主体，有效期 ${humanTtl(row.expiresIn)}）。`,
      confirmLabel: '吊销',
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
    const ok = await confirm({
      title: `吊销令牌 #${tokenId}`,
      body: '按 token_id 吊销一条历史令牌（不在本会话台账内）。吊销后该令牌立即失效，此操作不可撤销。',
      confirmLabel: '吊销',
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
          <h2>Access Tokens</h2>
        </div>
        <EmptyState
          message="无权限访问 Access Tokens"
          hint="令牌管理页属管理面板（admin / 只读管理员）。普通用户的自助令牌：制品树任意仓库 → Set Me Up（生成接入令牌），或 REST POST /api/security/token。"
        />
      </div>
    )
  }

  return (
    <div data-testid="tokens-page">
      <div className="page-header">
        <h2>Access Tokens</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          自铸 / 吊销 API 令牌（E-17 / E-18；scope 恒 api:*——携带主体全部权限）
        </span>
      </div>

      <Box sx={{ display: 'flex', gap: 1, mb: 1, alignItems: 'center' }}>
        <Box sx={{ flexGrow: 1 }} />
        <Button variant="contained" data-testid="token-create" onClick={() => setDialogOpen(true)}>
          生成令牌
        </Button>
      </Box>

      {readOnly && (
        <Alert severity="info" data-testid="tokens-readonly-note" sx={{ mb: 1 }}>
          只读管理员：吊销是管理面写动作（服务端以 403 兜底）——已禁用；自铸不受限（Q11 自助面，仅本人 + 有限期）。
        </Alert>
      )}
      <Alert severity="info" data-testid="tokens-ledger-note" sx={{ mb: 1 }}>
        下表是<b>本会话经此页签发</b>的令牌台账——服务端只存指纹、无令牌清单端点（console-ux §9-R6），刷新后表格即空、
        明文不可再取。历史令牌的吊销走下方「按 token_id 吊销」；签发/吊销事件可到审计日志以指纹对账。
      </Alert>

      {rows.length === 0 ? (
        <EmptyState
          illustration
          message="本次会话还没有经此页签发的令牌"
          hint="生成后在创建面板一次性展示明文（关闭即不可再取）；本页无服务端清单可回看。"
          testid="tokens-empty"
          action={
            <Button variant="contained" data-testid="token-create-empty" onClick={() => setDialogOpen(true)}>
              生成第一个令牌
            </Button>
          }
        />
      ) : (
        <Paper variant="outlined">
          <Table size="small" data-testid="token-table">
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">token_id</TableCell>
                <TableCell component="th" scope="col">指纹</TableCell>
                <TableCell component="th" scope="col">主体</TableCell>
                <TableCell component="th" scope="col">有效期</TableCell>
                <TableCell component="th" scope="col">状态</TableCell>
                <TableCell component="th" scope="col" align="right">操作</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {pageRows.map((row) => (
                <TableRow key={row.tokenId} data-testid={`token-row-${row.tokenId}`} hover>
                  <TableCell className="mono" lang="en">
                    #{row.tokenId}
                  </TableCell>
                  <TableCell className="mono" lang="en" data-testid={`token-fingerprint-${row.tokenId}`}>
                    {row.fingerprint || '—'}{' '}
                    {row.fingerprint && <CopyButton value={row.fingerprint} label={`指纹 #${row.tokenId}`} />}
                  </TableCell>
                  <TableCell className="mono" lang="en">
                    {row.subject}
                  </TableCell>
                  <TableCell>
                    {humanTtl(row.expiresIn)}
                    <Typography component="div" variant="caption" color="text.secondary">
                      {new Date(row.mintedAt).toLocaleTimeString()}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Chip
                      size="small"
                      color={row.revoked ? 'default' : 'success'}
                      variant={row.revoked ? 'outlined' : 'filled'}
                      label={row.revoked ? '已吊销' : '有效'}
                      data-testid={`token-status-${row.tokenId}`}
                    />
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: 'nowrap' }}>
                    <Tooltip title={row.revoked ? '已吊销' : '吊销（danger 确认）'}>
                      <span>
                        <Button
                          size="small"
                          variant="outlined"
                          color="error"
                          data-testid={`token-revoke-${row.tokenId}`}
                          disabled={readOnly || row.revoked || busyId === row.tokenId}
                          onClick={() => void doRevokeRow(row)}
                          sx={monoBtnSx}
                        >
                          {busyId === row.tokenId ? '吊销中…' : '吊销'}
                        </Button>
                      </span>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Paper>
      )}
      {rows.length > 0 && (
        <Box sx={{ mt: 1 }} data-testid="tokens-count">
          <Pager
            page={pager.page}
            pageCount={pager.pageCount}
            onPageChange={pager.setPage}
            from={pager.from}
            to={pager.to}
            total={rows.length}
            note={`（会话台账 · 含已吊销 ${rows.filter((r) => r.revoked).length}）`}
            pageSize={pager.size}
            onPageSizeChange={pager.setSize}
          />
        </Box>
      )}

      {adminWrite && (
        <Box component={Paper} variant="outlined" sx={{ mt: 2, p: 1.5, display: 'flex', gap: 1, alignItems: 'center', flexWrap: 'wrap' }} data-testid="token-revoke-byid">
          <TextField
            size="small"
            label="按 token_id 吊销（历史令牌）"
            placeholder="如 42"
            value={revokeIdInput}
            onChange={(e) => setRevokeIdInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                void doRevokeById()
              }
            }}
            sx={{ ...monoInputSx, minWidth: 240 }}
            slotProps={{ htmlInput: { 'data-testid': 'token-revoke-id', lang: 'en', inputMode: 'numeric' } }}
          />
          <Button
            variant="outlined"
            color="error"
            data-testid="token-revoke-byid-go"
            disabled={byidBusy || !/^\d+$/.test(revokeIdInput.trim())}
            onClick={() => void doRevokeById()}
          >
            {byidBusy ? '吊销中…' : '吊销'}
          </Button>
          <Typography variant="caption" color="text.secondary">
            台账外的历史令牌以 id 吊销（console-ux §9-R6 的既有出口；token_id 见审计日志 token.issue 事件）
          </Typography>
        </Box>
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

/** 表格行内小按钮的布局配方（cellBtnSx 的 Button 版形态——cellBtnSx 本体
 * 面向 outlined 小钮同形，这里直引即可省——保留局部别名便于 sx 组合） */
const monoBtnSx = { minWidth: 0, minHeight: 24 } as const

/** 创建 modal（Dialog sm——FR-125.1）。明文只在 done 面板存活，组件随
 * modal 卸载即丢弃——「仅展示一次 / 刷新不可再取」由态机保证：无任何
 * 持久层持有明文。step-up 口令腿 = smu-stepup 同形态（内联、不弹第二层）。 */
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
  const toast = useToast()
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
      toast.success(`令牌 #${res.token_id} 已生成——明文仅此一次展示`)
      setMint({ phase: 'done', token: res.access_token, tokenId: res.token_id, expiresIn })
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

  return (
    <Dialog open onClose={onClose} maxWidth="sm" fullWidth data-testid="token-dialog" aria-labelledby="token-dialog-title">
      {mint.phase === 'done' ? (
        <>
          <DialogTitle id="token-dialog-title">令牌已生成（仅此一次展示）</DialogTitle>
          <DialogContent dividers>
            <Paper
              variant="outlined"
              data-testid="token-plaintext"
              sx={{ p: 1.5, borderColor: 'success.light', bgcolor: 'background.default' }}
            >
              <Typography variant="body2" sx={{ mb: 1 }}>
                以 <span className="mono" lang="en">{subject.trim() && adminWrite ? subject.trim() : username}</span>{' '}
                身份签发（token_id <span className="mono" lang="en">#{mint.tokenId}</span>
                ，有效期 {humanTtl(mint.expiresIn)}）：
              </Typography>
              <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1, mb: 1 }}>
                <Typography
                  component="code"
                  lang="en"
                  data-testid="token-value"
                  sx={{ fontFamily: 'var(--bf-mono)', fontSize: 'var(--bf-fs-aux)', wordBreak: 'break-all', flex: 1 }}
                >
                  {mint.token}
                </Typography>
                <CopyButton value={mint.token} label="API Token" />
              </Box>
              <Typography variant="caption" color="text.secondary">
                关闭本面板后不可再查看（服务端只存指纹）。CI 与脚本请使用此令牌，不要用控制台口令——
                docker login / curl -u / settings.xml / .pypirc 的口令位都填它。
              </Typography>
            </Paper>
          </DialogContent>
          <DialogActions>
            <Box sx={{ flexGrow: 1 }} />
            <Button variant="contained" data-testid="token-plaintext-done" onClick={onClose}>
              我已保存，关闭
            </Button>
          </DialogActions>
        </>
      ) : mint.phase === 'need-password' ? (
        <>
          <DialogTitle id="token-dialog-title">需要二次口令（step-up）</DialogTitle>
          <DialogContent dividers>
            <Alert severity="warning" data-testid="token-stepup" sx={{ mb: 1 }}>
              实例开启 auth.token_step_up——非 admin 会话签发令牌需输入当前账号口令后继续（ADR-0027）。
            </Alert>
            <TextField
              label="当前账号口令"
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
              slotProps={{ htmlInput: { 'data-testid': 'token-password', autoComplete: 'current-password' } }}
            />
            {mint.error && (
              <Alert severity="error" data-testid="token-password-error" role="alert" sx={{ mt: 1 }} lang="en">
                {mint.error}
              </Alert>
            )}
            <Box sx={{ mt: 1 }}>
              <details>
                <summary>
                  <Typography variant="caption" color="text.secondary" component="span">
                    服务端原文
                  </Typography>
                </summary>
                <Typography
                  component="pre"
                  lang="en"
                  sx={{ fontFamily: 'var(--bf-mono)', fontSize: 'var(--bf-fs-aux)', color: 'text.secondary', wordBreak: 'break-all', m: 0 }}
                >
                  {mint.raw ?? ''}
                </Typography>
              </details>
            </Box>
          </DialogContent>
          <DialogActions>
            <Button onClick={onClose} data-testid="token-cancel">
              取消
            </Button>
            <Button
              variant="contained"
              data-testid="token-password-submit"
              disabled={mint.submitting || password === ''}
              onClick={() => void runMint({ password })}
            >
              {mint.submitting ? '验证中…' : '验证并生成令牌'}
            </Button>
          </DialogActions>
        </>
      ) : mint.phase === 'oidc-stepup' ? (
        <>
          <DialogTitle id="token-dialog-title">需要重新认证（step-up）</DialogTitle>
          <DialogContent dividers>
            <Alert severity="warning" sx={{ mb: 1 }}>
              SSO（OIDC）会话签发令牌需到身份提供方重新认证一次。完整的「跳转 IdP 重认证 →
              自动续铸」链在 Set Me Up 接入向导内：从制品树任意仓库的 Set Me Up 进入并生成
              （上下文会被记住，完成后自动续铸）；或联系管理员评估 auth.token_step_up 配置。
            </Alert>
            <details>
              <summary>
                <Typography variant="caption" color="text.secondary" component="span">
                  服务端原文
                </Typography>
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
            <Button onClick={onClose} data-testid="token-cancel">
              关闭
            </Button>
          </DialogActions>
        </>
      ) : (
        <>
          <DialogTitle id="token-dialog-title">生成 Access Token</DialogTitle>
          <DialogContent dividers sx={{ display: 'grid', gap: 2, pt: 1 }}>
            {adminWrite && (
              <TextField
                label="签发对象（可选——代人签发）"
                placeholder={username}
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
                helperText="留空 = 为自己签发；填其他用户名 = 以该用户主体签发（admin 专属，Q11）"
                sx={monoInputSx}
                slotProps={{ htmlInput: { 'data-testid': 'token-form-subject', lang: 'en' } }}
              />
            )}
            <TextField
              label="有效期"
              select
              value={String(ttl)}
              onChange={(e) => setTtl(Number(e.target.value))}
              helperText={
                adminWrite
                  ? '「永不过期」仅管理员可选；非 admin 主体有限期且受 auth.token_nonadmin_max_ttl 上限约束'
                  : '非 admin 签发为有限期（Q11 护栏：≤ auth.token_nonadmin_max_ttl，默认 365 天）'
              }
              slotProps={{
                select: {
                  native: true,
                  inputProps: { 'data-testid': 'token-form-ttl' } as ComponentPropsWithoutRef<'select'>,
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
              <Typography variant="caption" color="text.secondary">
                scope 固定（只读说明）：令牌携带主体全部权限——scope 参数仅经校验、不收窄权限域，故不设选项（端点亦无描述字段，token_id 即标识）
              </Typography>
            </Box>
            <Divider />
            <Typography variant="caption" color="text.secondary">
              生成后明文只在结果面板展示一次（关闭即不可再取——服务端只存指纹）；签发动作记入审计日志。
            </Typography>
            {mint.phase === 'error' && (
              <Alert severity="error" role="alert" data-testid="token-mint-error">
                签发失败（HTTP {mint.status}）：{mint.message}
              </Alert>
            )}
          </DialogContent>
          <DialogActions>
            <Button onClick={onClose} data-testid="token-cancel">
              取消
            </Button>
            <Button
              variant="contained"
              data-testid="token-submit"
              disabled={mint.phase === 'minting'}
              onClick={() => void runMint()}
            >
              {mint.phase === 'minting' ? '生成中…' : '生成令牌'}
            </Button>
          </DialogActions>
        </>
      )}
    </Dialog>
  )
}
