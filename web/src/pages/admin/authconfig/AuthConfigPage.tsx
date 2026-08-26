import { useEffect, useMemo, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import TextField from '@mui/material/TextField'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../../app/AuthContext'
import { useToast } from '../../../app/ToastContext'
import { EmptyState } from '../../../components/EmptyState'
import { ErrorCard } from '../../../components/ErrorCard'
import { Skeleton } from '../../../components/Skeleton'
import { ApiError, canAdminWrite, errText, getAuthSection, isReadOnlyAdmin, putAuthSection, testAuthSection } from '../../../lib/api'
import type { AuthSection, AuthTestReport } from '../../../lib/api'
import { denseInputSx } from '../../../lib/muiAtoms'
import { onTablistKeys } from '../../../lib/keys'
import { useAsync } from '../../../lib/useAsync'
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
  const readonlyTitle = '只读管理员不可写（服务端 403 兜底）'

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
          placeholder={secretSet ? '留空保持不变' : '未设置——输入以设置'}
          value={String(value ?? '')}
          onChange={(e) => onChange(e.target.value)}
          sx={denseInputSx}
          slotProps={{ htmlInput: { id: field.anchor, 'data-testid': field.anchor, className: MONO_INPUT, lang: 'en', spellCheck: false } }}
        />
        <p className="authcfg-secret-set" data-testid={field.setAnchor ?? `${field.anchor}-set`}>
          {secretSet ? (
            <>
              <span aria-hidden="true">🔒</span> 已设置——服务端永不回显明文；保存时留空 = 保持不变，重新输入 = 替换。
            </>
          ) : (
            <>
              <span aria-hidden="true">○</span> 未设置。
            </>
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
        <span style={{ fontSize: 'var(--bf-fs-aux)', color: 'var(--bf-text-2)' }}>{field.label}</span>
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
            background: 'var(--bf-surface-2)',
            border: '1px solid var(--bf-border)',
            borderRadius: 'var(--bf-r-sm)',
            fontSize: 'var(--bf-fs-form)',
            color: 'var(--bf-text-2)',
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
        sx={denseInputSx}
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
        <div>
          探测请求失败（HTTP {report.errorStatus}）：<span lang="en">{report.errorText}</span>
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
          {!report.ok && <span>（消息原文照服务端信封——未翻译，便于排障比对）</span>}
        </div>
      )}
    </Alert>
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
      toast.success(`${def.tab} 配置已保存——即刻生效（无需重启）`)
      setForm(initForm(def, echo))
      setBaseline(initForm(def, echo))
      const set: Record<string, boolean> = {}
      for (const w of secretWires(def)) set[w] = secretIsSet(echo, w)
      setSecretsSet(set)
      setNeverSaved(false)
      setReport(null)
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
          message="无权限读取认证配置"
          hint="认证配置属于管理面（security:read，需 admin / readonly_admin）。"
        />
      )}
      {q.status === 'ok' && (
        <>
          {neverSaved && def.id === 'saml' && (
            <Alert severity="info" sx={{ mb: 2 }} data-testid="authcfg-saml-empty">
              本协议尚未保存过配置——GET 返回空对象（<span className="mono" lang="en">{'{}'}</span>，§3.2 锚定空态）。下方表单为默认值，填写后保存即创建。
            </Alert>
          )}
          {def.groups.map((g) => (
            <div className="authcfg-group" key={g.title}>
              <h3>{g.title}</h3>
              {g.hint && <p className="authcfg-group-hint">{g.hint}</p>}
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
            </div>
          ))}

          {/* 测试连接（POST …/test 双形态：候选 = 当前表单值；存量 = 空体探已保存配置） */}
          <div className="authcfg-group" data-testid="authcfg-test">
            <h3>测试连接</h3>
            <p className="authcfg-group-hint">
              候选探测提交当前表单值（不落库）；存量探测直接探测已保存配置。LDAP 可附测试账号做真实用户绑定（§1.6——两半须齐备）。
            </p>
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
                    sx={{ ...denseInputSx, width: 200 }}
                    slotProps={{ htmlInput: { 'data-testid': 'authcfg-test-username', 'aria-label': '测试用户名（testUsername）', className: MONO_INPUT, lang: 'en', autoComplete: 'off', spellCheck: false } }}
                  />
                  <TextField
                    size="small"
                    margin="none"
                    type="password"
                    disabled={disabled}
                    placeholder="testPassword"
                    value={testPass}
                    onChange={(e) => setTestPass(e.target.value)}
                    sx={{ ...denseInputSx, width: 200 }}
                    slotProps={{ htmlInput: { 'data-testid': 'authcfg-test-password', 'aria-label': '测试口令（testPassword）', className: MONO_INPUT, lang: 'en', autoComplete: 'new-password', spellCheck: false } }}
                  />
                </>
              )}
              <Tooltip title={credsIncomplete ? '测试账号两半须齐备（testUsername / testPassword）——只填一半会被服务端拒绝' : '用当前表单值探测（不落库）'}>
                <span>
                  <Button
                    size="small"
                    variant="contained"
                    disabled={disabled || testing !== null || credsIncomplete}
                    data-testid="authcfg-test-run"
                    onClick={() => void runTest('run')}
                  >
                    {testing === 'run' ? '探测中…' : '测试连接（当前表单）'}
                  </Button>
                </span>
              </Tooltip>
              <Tooltip title="探测已保存的配置（空请求体）">
                <span>
                  <Button
                    size="small"
                    variant="outlined"
                    disabled={disabled || testing !== null || neverSaved}
                    data-testid="authcfg-test-stored"
                    onClick={() => void runTest('stored')}
                  >
                    {testing === 'stored' ? '探测中…' : '测试已保存配置'}
                  </Button>
                </span>
              </Tooltip>
              {credsIncomplete && (
                <Typography variant="caption" color="text.secondary">
                  测试账号两半须齐备（testUsername / testPassword）——只填一半会被服务端拒绝。
                </Typography>
              )}
            </div>
            {report && <TestReportBox report={report} />}
          </div>

          {saveError && (
            <Alert
              severity="error"
              sx={{ mb: 2 }}
              data-testid="authcfg-error"
              onClose={() => setSaveError(null)}
            >
              <div>
                保存被拒（HTTP {saveError.status || '网络'}）：
              </div>
              <div className="mono" lang="en">
                {saveError.message}
              </div>
              <div>拒绝不改动在用配置（verify-then-replace）——修正后重试。</div>
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
              {busy ? '保存中…' : '保存配置'}
            </Button>
            <Button
              variant="outlined"
              disabled={!canWrite || !dirty || busy}
              data-testid="authcfg-reset"
              onClick={() => setForm(baseline)}
            >
              还原
            </Button>
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
        <h2>认证配置</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          {def.head}
        </span>
      </div>

      <p className="page-note" data-testid="authcfg-note-effect">
        ⓘ 保存即生效：配置在保存后的<b>下一个认证请求</b>起生效，无需重启（服务端热更新；已建立的会话不受影响）。
        敏感字段（密码/secret）服务端永不回显明文——留空保存 = 保持不变。
      </p>

      {readOnly && (
        <p className="page-note" data-testid="authcfg-readonly-note">
          ⓘ 只读管理员视角：认证配置只读呈现（security:read）；保存与测试连接是管理面写操作（security:write，仅全量
          admin）——控件已禁用，服务端 403 兜底。
        </p>
      )}

      <nav
        className="authcfg-tabs"
        role="tablist"
        aria-label="认证协议"
        onKeyDown={(e) =>
          onTablistKeys(e, SECTION_ORDER, section, (id) => navigate(`/admin/security/auth/${id}`))
        }
      >
        {SECTION_ORDER.map((id) => (
          <Link
            key={id}
            to={`/admin/security/auth/${id}`}
            className={`authcfg-tab${section === id ? ' active' : ''}`}
            role="tab"
            aria-current={section === id ? 'page' : undefined}
            aria-selected={section === id}
            data-testid={`authcfg-tab-${id}`}
          >
            {SECTIONS[id].tab}
          </Link>
        ))}
      </nav>

      <SectionPanel def={def} canWrite={canWrite} />
    </div>
  )
}
