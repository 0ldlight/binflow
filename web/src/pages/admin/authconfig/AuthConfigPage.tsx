import { useEffect, useMemo, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import Paper from '@mui/material/Paper'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../../app/AuthContext'
import { useToast } from '../../../app/ToastContext'
import { useConfirm } from '../../../components/ConfirmDialog'
import { CopyButton } from '../../../components/CopyButton'
import { EmptyState } from '../../../components/EmptyState'
import { ErrorCard } from '../../../components/ErrorCard'
import { Skeleton } from '../../../components/Skeleton'
import { ApiError, canAdminWrite, errText, getAuthSection, getSamlSpCertificate, isReadOnlyAdmin, putAuthSection, regenerateSamlSpKey, testAuthSection } from '../../../lib/api'
import type { AuthSection, AuthTestReport } from '../../../lib/api'
import { useAsync } from '../../../lib/useAsync'
import { Sha256 } from '../../artifacts/sha256'
import {
  SECTION_ORDER,
  SECTIONS,
  buildPayload,
  initForm,
  isEmptySectionDoc,
  secretIsSet,
  secretWires,
} from './sections'
import type { FieldDef, FormState, SectionDef } from './sections'

import './authconfig.css'
import { tr } from '../../../i18n'

const tt = tr('admin')

// Admin > Security 认证配置页组（T-307，M11 FR-92 FE 腿；三协议 Tab =
// 子路由 /admin/security/auth/{ldap|oauth|saml}——repos 三 Tab 同款形态）。
//
// 行为基准 docs/reverse/auth-integration.md v2；BE 契约 = T-305 九端点
// （GET/PUT + POST …/test，errors[] 信封，脱敏哨兵回显）。FE 四陷阱的处理：
//   1. SAML noAutoUserCreation 反语义——表单呈正语义复选「Auto Create
//      Users」，wire 边界取反（sections.ts invert）；
//   2. 哨兵语义——GET 回显 20 星 → 表单留空 + placeholder「留空保持不变」，
//      提交时空值自 payload 剔除（绝不回传哨兵：400 红线，网络层有断言）；
//   3. SAML GET 空态 {} → 引导块（保存后消失）+ 表单全默认；
//   4. OAuth persistUsers 全局摊平 → 单段模型下即本段 auto_create_users
//      一位（BinFlow C 级 wire，T-305 漂移 1 对齐）。
// 权限姿态：GET = CapSecurityRead（readonly_admin 只读）；PUT/test =
// CapSecurityWrite（仅全量 admin）——readonly 控件 disabled + 反断言，
// 普通 user 导航不可达 + 直链 L2 无权限卡。PUT 是全量替换：非 secret 字段
// 逐字段回传（漏发会被服务端归一成默认值）。保存即生效（无需重启）页内明示。
//
// T-307R（T-307 遗留 1 收口）：SAML Tab 增 SP 加密证书卡（SamlCertCard）——
// 公钥下载 + 重生成（danger 确认；旧证书失效后果提示），BE = T-331 三端点
// （key/public[/regenerate]，text/plain）。未生成 404 = 锚定空态：下载禁用、
// 重生成兼作生成入口；指纹 SHA-256 over DER（openssl 可比对）+ 一键拷贝。

const MONO_INPUT = 'mono-input'

/** 单字段渲染（数据驱动；锚点全部落在 input 本体——T-299/T-291 纪律） */
function FieldControl({
  field,
  value,
  disabled,
  secretSet,
  onChange,
}: {
  field: FieldDef
  value: string | boolean
  disabled: boolean
  secretSet: boolean
  onChange: (v: string | boolean) => void
}) {
  const readonlyTitle = tt('只读管理员不可写（服务端 403 兜底）')

  // secret：值恒空；已设置（GET 哨兵）→ placeholder「留空保持不变」+ 提示行
  if (field.kind === 'secret') {
    return (
      <div className={`authcfg-field${field.full ? ' full' : ''}`}>
        <label htmlFor={field.anchor}>{field.label}</label>
        <TextField
          type="password"
          size="small"
          fullWidth
          margin="none"
          disabled={disabled}
          autoComplete="new-password"
          placeholder={secretSet ? tt('留空保持不变') : tt('未设置——输入以设置')}
          value={String(value ?? '')}
          onChange={(e) => onChange(e.target.value)}
          slotProps={{ htmlInput: { id: field.anchor, 'data-testid': field.anchor, className: MONO_INPUT, lang: 'en', spellCheck: false } }}
        />
        <p className="authcfg-secret-set" data-testid={field.setAnchor ?? `${field.anchor}-set`}>
          {secretSet ? (
            <>
              <span aria-hidden="true">🔒</span> {tt('已设置——服务端永不回显明文；保存时留空 = 保持不变，重新输入 = 替换。')}            </>
          ) : (
            <>
              <span aria-hidden="true">○</span> {tt('未设置。')}            </>
          )}
        </p>
      </div>
    )
  }

  if (field.kind === 'check') {
    return (
      <div className={`authcfg-field${field.full ? ' full' : ''}`} style={{ justifyContent: 'center' }}>
        <Tooltip title={disabled ? readonlyTitle : field.hint ?? ''} enterDelay={600}>
          <span>
            <FormControlLabel
              disabled={disabled}
              control={
                <Checkbox
                  size="small"
                  checked={value === true}
                  onChange={(e) => onChange(e.target.checked)}
                  slotProps={{ input: { 'data-testid': field.anchor } as ComponentPropsWithoutRef<'input'> }}
                />
              }
              label={<span style={{ fontSize: 'var(--bf-fs-form)' }}>{field.label}</span>}
            />
          </span>
        </Tooltip>
      </div>
    )
  }

  if (field.kind === 'locked') {
    return (
      <div className={`authcfg-field${field.full ? ' full' : ''}`}>
        {/* 锁定展示不是表单控件——非 label 元素（label 只指表单控件，axe） */}
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{field.label}</span>
        <Box
          data-testid={field.anchor}
          component="span"
          className="mono"
          lang="en"
          sx={{
            display: 'inline-block',
            padding: '5px 12px',
            minHeight: 32,
            lineHeight: '22px',
            bgcolor: 'action.hover',
            border: '1px solid var(--bf-border)',
            borderRadius: 'var(--bf-r-sm)',
            fontSize: 'var(--bf-fs-form)',
            color: 'text.secondary',
          }}
        >
          {String(value ?? '')}
        </Box>
        {field.hint && <p className="authcfg-hint">{field.hint}</p>}
      </div>
    )
  }

  // text / textarea / number / list
  return (
    <div className={`authcfg-field${field.full ? ' full' : ''}`}>
      <label htmlFor={field.anchor}>{field.label}</label>
      <TextField
        size="small"
        fullWidth
        margin="none"
        disabled={disabled}
        placeholder={field.placeholder}
        value={String(value ?? '')}
        onChange={(e) => onChange(e.target.value)}
        slotProps={{
          htmlInput: {
            id: field.anchor,
            'data-testid': field.anchor,
            className: field.mono ? MONO_INPUT : undefined,
            lang: 'en',
            spellCheck: false,
            ...(field.kind === 'number' ? { inputMode: 'numeric' } : {}),
          },
        }}
        {...(field.kind === 'textarea' ? { multiline: true, rows: field.rows ?? 4 } : {})}
      />
      {field.hint && <p className="authcfg-hint">{field.hint}</p>}
    </div>
  )
}

/** 测试连接报告（成功/失败消息原文照 BE；phase/category 徽标 mono） */
function TestReportBox({ report }: { report: AuthTestReport | { errorStatus: number; errorText: string } }) {
  if ('errorStatus' in report) {
    return (
      <Alert severity="error" sx={{ mt: 1 }} data-testid="authcfg-test-report" role="status">
        <div>{tt('探测请求失败（HTTP')} {report.errorStatus}{tt('）：')}<span lang="en">{report.errorText}</span>
        </div>
      </Alert>
    )
  }
  return (
    <Alert severity={report.ok ? 'success' : 'error'} sx={{ mt: 1 }} data-testid="authcfg-test-report" role="status">
      <div lang="en">{report.message || (report.ok ? 'OK' : 'failed')}</div>
      {(report.phase || report.category) && (
        <div className="authcfg-report-meta">
          {report.phase && (
            <span>
              phase: <span className="mono" lang="en">{report.phase}</span>
            </span>
          )}
          {report.category && (
            <span>
              category: <span className="mono" lang="en">{report.category}</span>
            </span>
          )}
          {!report.ok && <span>{tt('（消息原文照服务端信封——未翻译，便于排障比对）')}</span>}
        </div>
      )}
    </Alert>
  )
}

/** PEM（CERTIFICATE 块）→ SHA-256 指纹（over DER——openssl x509 -fingerprint
 *  可比对；冒号分隔大写 hex）。PEM 体即 DER 的 base64，无需 ASN.1 解析。
 *  复用 artifacts 的零依赖流式实现而非 crypto.subtle：后者要求安全上下文，
 *  局域网 http 部署（console 的常见形态）下不可用。 */
function pemSha256Fingerprint(pem: string): string | null {
  try {
    const b64 = pem.replace(/-----[A-Z ]+-----/g, '').replace(/\s+/g, '')
    const bin = atob(b64)
    const der = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) der[i] = bin.charCodeAt(i)
    const hex = new Sha256().update(der).digest()
    return (hex.match(/../g) ?? []).map((h) => h.toUpperCase()).join(':')
  } catch {
    return null // 非 PEM 形态——指纹行静默缺位，下载仍可用
  }
}

/** 公钥证书落盘（Blob + a[download]；无跨页跳转、无后端耦合） */
function downloadPem(pem: string): void {
  const blob = new Blob([pem.endsWith('\n') ? pem : `${pem}\n`], { type: 'application/x-pem-file' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'binflow-saml-sp.crt'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

/**
 * SAML SP 加密证书卡（T-307R——T-307 遗留 1 的 FE 接线，BE = T-331 三端点）。
 * Artifactory 姿态（§3.2/§6 UI 流）：下载即得 PEM；重生成 force 一对一替换、
 * 旧证书即刻失效——强确认对话框承载后果提示。权限分野：下载走
 * CapSecurityRead（readonly_admin 可用——公钥是给 IdP 的公开材料），重生成
 * 走 CapSecurityWrite（readonly 禁用 + 反断言，服务端 403 终裁）。
 * 未生成（GET 404）= 锚定空态：下载禁用；重生成兼作「生成」入口（勾选
 * Use EncryptedAssertion 保存时服务端也会自动生成——§3.3，卡内明示）。
 */
function SamlCertCard({ canWrite }: { canWrite: boolean }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [cert, setCert] = useState<string | null>(null)
  const [missing, setMissing] = useState(false)
  const [loadError, setLoadError] = useState<ApiError | null>(null)
  const [busy, setBusy] = useState<'load' | 'rotate' | null>(null)

  const load = async () => {
    setBusy('load')
    setLoadError(null)
    try {
      setCert(await getSamlSpCertificate())
      setMissing(false)
    } catch (err) {
      const e = err instanceof ApiError ? err : new ApiError(0, errText(err))
      if (e.status === 404) {
        setCert(null)
        setMissing(true) // 锚定空态：密钥对尚未生成（非错误）
      } else {
        setLoadError(e)
      }
    } finally {
      setBusy(null)
    }
  }

  useEffect(() => {
    void load()
    // 挂载即取——证书面独立于段配置 doc（保留行不在 GET 回显里）
  }, [])

  const fingerprint = useMemo(() => (cert ? pemSha256Fingerprint(cert) : null), [cert])

  const regenerate = async () => {
    const ok = await confirm({
      title: missing ? tt('生成 SP 加密证书') : tt('重新生成 SP 加密证书'),
      body: missing ? (
        <>{tt('将生成全新的服务提供方（SP）加密密钥对并立即生效。生成后须把公钥证书导入 IdP，加密断言（Use Encrypted Assertion）才能完成解密。')}        </>
      ) : (
        <>{tt('重新生成将创建')}<b>{tt('全新密钥对')}</b>——<b>{tt('旧公钥证书即刻失效')}</b>{tt('：已导入旧证书的 IdP 在重新导入新证书前，无法完成加密断言的解密，期间 SAML 加密登录会失败。')}          <br />{tt('新证书生成后即可下载导入；此操作不可撤销。')}        </>
      ),
      confirmLabel: missing ? tt('生成证书') : tt('重新生成'),
      danger: !missing, // 替换在用证书是破坏性操作；首生成不是
    })
    if (!ok) return
    setBusy('rotate')
    try {
      const pem = await regenerateSamlSpKey() // 响应体即新证书（D-5）——直接刷新展示
      setCert(pem)
      setMissing(false)
      toast.success(
        missing
          ? tt('SAML SP 加密证书已生成——下载公钥证书导入 IdP 后即可使用加密断言')
          : tt('SAML SP 证书已重新生成——旧证书即刻失效，请把新证书导入 IdP'),
      )
    } catch (err) {
      const e = err instanceof ApiError ? err : new ApiError(0, errText(err))
      toast.error(tt('证书{v1}失败（HTTP {v2}）：{v3}', { v1: missing ? tt('生成') : tt('重生成'), v2: e.status || tt('网络'), v3: e.message }))
    } finally {
      setBusy(null)
    }
  }

  const readonlyTitle = tt('只读管理员不可写（服务端 403 兜底）；公钥证书可下载')

  return (
    <Paper className="authcfg-group" sx={{ p: 2, mb: 2 }}>
      <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5 }}>{tt('SP 加密证书（服务提供方公钥）')}      </Typography>
      <Typography className="authcfg-group-hint" variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>{tt('Use Encrypted Assertion 需要 IdP 持有本服务的公钥证书（勾选保存时若未生成，服务端会自动生成一份）。私钥由服务端密封保存、永不外发——这里只有公钥面。')}      </Typography>
      {/* 四态：loading 只出骨架（禁用态按钮的 MUI 灰对比度不达标——
          控件待数据到达再上，与 SectionPanel 的 Skeleton 先行同语言）；
          错误出错误卡 + 重试；数据态分「未生成/已生成」两呈现 */}
      {busy === 'load' && <Skeleton lines={1} />}
      {loadError && <ErrorCard error={loadError} onRetry={() => void load()} />}
      {!loadError && busy !== 'load' && (
        <>
          {missing && (
            <p className="authcfg-cert-status">
              <span aria-hidden="true">○</span> {tt('未生成——服务端尚无 SP 加密密钥对；可立即生成，或留待保存加密断言配置时自动生成。')}            </p>
          )}
          {cert && (
            <p className="authcfg-cert-status">
              <span aria-hidden="true">🔒</span> {tt('已生成——指纹（SHA-256）：')}            </p>
          )}
          {fingerprint && (
            <div className="authcfg-cert-fp">
              <span className="mono" lang="en">
                {fingerprint}
              </span>
              <CopyButton value={fingerprint} label={tt('证书指纹')} />
            </div>
          )}
          <div className="authcfg-cert-actions">
            {/* 无证书即无下载物——按钮不渲染（空态唯一动作是生成） */}
            {cert && (
              <Button
                size="small"
                variant="contained"
                disabled={busy !== null}
                data-testid="authcfg-saml-spkey-download"
                onClick={() => downloadPem(cert)}
              >{tt('下载公钥证书（PEM）')}              </Button>
            )}
            <Tooltip title={canWrite ? tt('创建全新密钥对——旧证书即刻失效（有确认）') : readonlyTitle} enterDelay={600}>
              <span>
                <Button
                  size="small"
                  variant="outlined"
                  color="error"
                  disabled={!canWrite || busy !== null}
                  data-testid="authcfg-saml-spkey-regenerate"
                  onClick={() => void regenerate()}
                >
                  {busy === 'rotate' ? tt('生成中…') : missing ? tt('生成证书') : tt('重新生成证书')}
                </Button>
              </span>
            </Tooltip>
          </div>
        </>
      )}
    </Paper>
  )
}

function SectionPanel({ def, canWrite }: { def: SectionDef; canWrite: boolean }) {
  const toast = useToast()
  const q = useAsync(() => getAuthSection<unknown>(def.id), [def.id])

  const [form, setForm] = useState<FormState>({})
  const [baseline, setBaseline] = useState<FormState>({})
  const [secretsSet, setSecretsSet] = useState<Record<string, boolean>>({})
  const [neverSaved, setNeverSaved] = useState(false)
  const [saveError, setSaveError] = useState<ApiError | null>(null)
  const [busy, setBusy] = useState(false)
  const [testing, setTesting] = useState<'run' | 'stored' | null>(null)
  const [report, setReport] = useState<AuthTestReport | { errorStatus: number; errorText: string } | null>(null)
  const [testUser, setTestUser] = useState('')
  const [testPass, setTestPass] = useState('')
  // SAML：保存（useEncryptedAssertion=true）可能让服务端自动生出密钥对
  // （§3.3）——保存成功即重挂证书卡（key 变化 → 重新 GET，展示跟上实态）
  const [certTick, setCertTick] = useState(0)

  // GET 到达 → 表单初始化（secret 一律空；SAML {} 空态引导）
  useEffect(() => {
    if (q.status !== 'ok') return
    setForm(initForm(def, q.data))
    setBaseline(initForm(def, q.data))
    const set: Record<string, boolean> = {}
    for (const w of secretWires(def)) set[w] = secretIsSet(q.data, w)
    setSecretsSet(set)
    setNeverSaved(isEmptySectionDoc(q.data))
    setSaveError(null)
    setReport(null)
  }, [q.status, q.data, def])

  const disabled = !canWrite
  const dirty = useMemo(() => JSON.stringify(form) !== JSON.stringify(baseline), [form, baseline])
  // LDAP §1.6 测试信封：两半齐备才发候选探测（任一半填了另一半空 → 禁用，
  // 表单零坏请求；双空 = 允许发——服务端做不带用户 bind 的连通探测）
  const credsIncomplete = def.testCreds && ((testUser !== '') !== (testPass !== ''))

  const save = async () => {
    setBusy(true)
    setSaveError(null)
    try {
      const echo = await putAuthSection<unknown>(def.id, buildPayload(def, form))
      toast.success(tt('{v1} 配置已保存——即刻生效（无需重启）', { v1: def.tab }))
      setForm(initForm(def, echo))
      setBaseline(initForm(def, echo))
      const set: Record<string, boolean> = {}
      for (const w of secretWires(def)) set[w] = secretIsSet(echo, w)
      setSecretsSet(set)
      setNeverSaved(false)
      setReport(null)
      if (def.id === 'saml') setCertTick((t) => t + 1)
      q.reload()
    } catch (err) {
      setSaveError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  const runTest = async (mode: 'run' | 'stored') => {
    setTesting(mode)
    setReport(null)
    try {
      let body: unknown
      if (mode === 'run') {
        body = buildPayload(def, form)
        if (def.testCreds && testUser !== '' && testPass !== '') {
          body = { ...(body as Record<string, unknown>), testUsername: testUser, testPassword: testPass }
        }
      }
      setReport(await testAuthSection(def.id, body))
    } catch (err) {
      setReport({
        errorStatus: err instanceof ApiError ? err.status : 0,
        errorText: errText(err),
      })
    } finally {
      setTesting(null)
    }
  }

  // ---- 四态（§5.3）：loading / forbidden / error / 数据 ----
  return (
    <section>
      {q.status === 'loading' && <Skeleton lines={8} />}
      {q.status === 'error' && q.error && <ErrorCard error={q.error} onRetry={q.reload} />}
      {q.status === 'forbidden' && (
        <EmptyState
          message={tt('无权限读取认证配置')}
          hint={tt('认证配置属于管理面（security:read，需 admin / readonly_admin）。')}
        />
      )}
      {q.status === 'ok' && (
        <>
          {neverSaved && def.id === 'saml' && (
            <Alert severity="info" sx={{ mb: 2 }} data-testid="authcfg-saml-empty">{tt('本协议尚未保存过配置——GET 返回空对象（')}<span className="mono" lang="en">{'{}'}</span>{tt('，§3.2 锚定空态）。下方表单为默认值，填写后保存即创建。')}            </Alert>
          )}
          {def.groups.map((g) => (
            <Paper className="authcfg-group" key={g.title} sx={{ p: 2, mb: 2 }}>
              <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5 }}>
                {g.title}
              </Typography>
              {g.hint && (
                <Typography className="authcfg-group-hint" variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
                  {g.hint}
                </Typography>
              )}
              <div className="authcfg-grid">
                {g.fields.map((f) => (
                  <FieldControl
                    key={f.wire}
                    field={f}
                    value={form[f.wire] ?? (f.kind === 'check' ? false : '')}
                    disabled={disabled || busy}
                    secretSet={!!secretsSet[f.wire]}
                    onChange={(v) => setForm((prev) => ({ ...prev, [f.wire]: v }))}
                  />
                ))}
              </div>
            </Paper>
          ))}

          {/* SAML 专属：SP 加密证书卡（下载/重生成——T-331 三端点接线，
              紧邻 useEncryptedAssertion 所在的表单卡） */}
          {def.id === 'saml' && <SamlCertCard key={certTick} canWrite={canWrite} />}

          {/* 测试连接（POST …/test 双形态：候选 = 当前表单值；存量 = 空体探已保存配置） */}
          <Paper className="authcfg-group" data-testid="authcfg-test" sx={{ p: 2, mb: 2 }}>
            <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5 }}>{tt('测试连接')}            </Typography>
            <Typography className="authcfg-group-hint" variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>{tt('候选探测提交当前表单值（不落库）；存量探测直接探测已保存配置。LDAP 可附测试账号做真实用户绑定（§1.6——两半须齐备）。')}            </Typography>
            <div className="authcfg-test-actions">
              {def.testCreds && (
                <>
                  <TextField
                    size="small"
                    margin="none"
                    disabled={disabled}
                    placeholder="testUsername"
                    value={testUser}
                    onChange={(e) => setTestUser(e.target.value)}
                    sx={{ width: 200 }}
                    slotProps={{ htmlInput: { 'data-testid': 'authcfg-test-username', 'aria-label': tt('测试用户名（testUsername）'), className: MONO_INPUT, lang: 'en', autoComplete: 'off', spellCheck: false } }}
                  />
                  <TextField
                    size="small"
                    margin="none"
                    type="password"
                    disabled={disabled}
                    placeholder="testPassword"
                    value={testPass}
                    onChange={(e) => setTestPass(e.target.value)}
                    sx={{ width: 200 }}
                    slotProps={{ htmlInput: { 'data-testid': 'authcfg-test-password', 'aria-label': tt('测试口令（testPassword）'), className: MONO_INPUT, lang: 'en', autoComplete: 'new-password', spellCheck: false } }}
                  />
                </>
              )}
              <Tooltip title={credsIncomplete ? tt('测试账号两半须齐备（testUsername / testPassword）——只填一半会被服务端拒绝') : tt('用当前表单值探测（不落库）')}>
                <span>
                  <Button
                    size="small"
                    variant="contained"
                    disabled={disabled || testing !== null || credsIncomplete}
                    data-testid="authcfg-test-run"
                    onClick={() => void runTest('run')}
                  >
                    {testing === 'run' ? tt('探测中…') : tt('测试连接（当前表单）')}
                  </Button>
                </span>
              </Tooltip>
              <Tooltip title={tt('探测已保存的配置（空请求体）')}>
                <span>
                  <Button
                    size="small"
                    variant="outlined"
                    disabled={disabled || testing !== null || neverSaved}
                    data-testid="authcfg-test-stored"
                    onClick={() => void runTest('stored')}
                  >
                    {testing === 'stored' ? tt('探测中…') : tt('测试已保存配置')}
                  </Button>
                </span>
              </Tooltip>
              {credsIncomplete && (
                <Typography variant="caption" color="text.secondary">{tt('测试账号两半须齐备（testUsername / testPassword）——只填一半会被服务端拒绝。')}                </Typography>
              )}
            </div>
            {report && <TestReportBox report={report} />}
          </Paper>

          {saveError && (
            <Alert
              severity="error"
              sx={{ mb: 2 }}
              data-testid="authcfg-error"
              onClose={() => setSaveError(null)}
            >
              <div>{tt('保存被拒（HTTP')} {saveError.status || tt('网络')}{tt('）：')}              </div>
              <div className="mono" lang="en">
                {saveError.message}
              </div>
              <div>{tt('拒绝不改动在用配置（verify-then-replace）——修正后重试。')}</div>
            </Alert>
          )}

          {/* 保存/还原：readonly_admin 呈 disabled（控件禁用 + 反断言口径，
              §3.6 L4 的「写入口不渲染」在此让位——认证配置页的只读价值
              就在控件态可见，服务端 403 终裁） */}
          <Box sx={{ display: 'flex', gap: 1, mb: 2 }}>
            <Button
              variant="contained"
              disabled={!canWrite || !dirty || busy}
              data-testid="authcfg-save"
              onClick={() => void save()}
            >
              {busy ? tt('保存中…') : tt('保存配置')}
            </Button>
            <Button
              variant="outlined"
              disabled={!canWrite || !dirty || busy}
              data-testid="authcfg-reset"
              onClick={() => setForm(baseline)}
            >{tt('还原')}            </Button>
          </Box>
        </>
      )}
    </section>
  )
}

export default function AuthConfigPage() {
  const { session } = useAuth()
  const navigate = useNavigate()
  const { proto } = useParams()
  const section = (SECTION_ORDER as string[]).includes(proto ?? '') ? (proto as AuthSection) : null
  const canWrite = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)
  // 页面级 L2：普通 user 导航不可达（admin nav 组门），直链由数据面 403 收敛

  if (!section) {
    return <Navigate to="/admin/security/auth/ldap" replace />
  }
  const def = SECTIONS[section]

  return (
    <div data-testid="authcfg-page">
      <div className="page-header">
        <h2>{tt('认证配置')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          {def.head}
        </span>
      </div>

      <p className="page-note" data-testid="authcfg-note-effect">{tt('ⓘ 保存即生效：配置在保存后的')}<b>{tt('下一个认证请求')}</b>{tt('起生效，无需重启（服务端热更新；已建立的会话不受影响）。 敏感字段（密码/secret）服务端永不回显明文——留空保存 = 保持不变。')}      </p>

      {readOnly && (
        <p className="page-note" data-testid="authcfg-readonly-note">{tt('ⓘ 只读管理员视角：认证配置只读呈现（security:read）；保存与测试连接是管理面写操作（security:write，仅全量 admin）——控件已禁用，服务端 403 兜底。')}        </p>
      )}

      {/* T-344 批 C：Tab 条换 MUI Tabs（锚 authcfg-tab-* 落 Tab 根 <a>，
          aria-current=page 续挂；方向键选择随焦点 = MUI 内建，替代
          onTablistKeys） */}
      <Tabs
        value={section}
        onChange={(_e, id: AuthSection) => navigate(`/admin/security/auth/${id}`)}
        selectionFollowsFocus
        sx={{ mb: 'var(--bf-sp-4)', borderBottom: 1, borderColor: 'divider' }}
      >
        {SECTION_ORDER.map((id) => (
          <Tab
            key={id}
            component={Link}
            to={`/admin/security/auth/${id}`}
            value={id}
            label={SECTIONS[id].tab}
            aria-current={section === id ? 'page' : undefined}
            data-testid={`authcfg-tab-${id}`}
          />
        ))}
      </Tabs>

      <SectionPanel def={def} canWrite={canWrite} />
    </div>
  )
}
