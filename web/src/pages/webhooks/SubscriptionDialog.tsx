import { useEffect, useMemo, useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import Divider from '@mui/material/Divider'
import FormControlLabel from '@mui/material/FormControlLabel'
import Select from '@mui/material/Select'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'

import { useToast } from '../../app/ToastContext'
import { ApiError, errText } from '../../lib/api'
import { monoInputSx } from '../../lib/muiAtoms'
import {
  CRITERIA_MANAGED_DOMAINS,
  DOMAIN_LABELS,
  EVENT_DOMAINS,
  WEBHOOK_SECRET_SENTINEL,
  createSubscription,
  criteriaEmptyScope,
  criteriaFromForm,
  criteriaToForm,
  testSubscription,
  typesOfDomain,
  updateSubscription,
} from '../../lib/webhooks'
import type { CriteriaForm, SubscriptionRequest, TestOutcome, WebhookSubscription } from '../../lib/webhooks'

// 新建/编辑订阅对话框（M13 T-366；交互形态 = console-artifactory-parity
// M3/M4：居中 Dialog、动作右下 Cancel 左主按钮右、Esc/遮罩关闭）。
//
// wire 语义（webhook.md §1/§2）：
// - key 创建后不可改（编辑态锁定展示）；pattern ^[A-Za-z][A-Za-z0-9_-]+$；
// - enabled 默认 false（官方 schema default——建后默认禁用，需显式勾选）；
// - event_filter = 单域 + 域内多事件型（66 型闭集，13 域分组下拉；
//   wired/dormant 如实标注——休眠型可订阅但永不触发）；
// - criteria：本体三域（artifact/artifact_property/docker）表单托管五键
//   （anyLocal/anyRemote/repoKeys/include/exclude——Ant 通配）；其余域
//   维度不在最小面内，编辑时原样透传（不丢配置）、新建发 {}；
// - handler 恰一个（官方 minItems/maxItems 1）：预定义 webhook 型——
//   url 必填；secret 三态：留空 = 保持（省略字段；创建 = 不设）、
//   明文 = 设置/轮换、勾选「清除」= ""（擦除）——哨兵绝不回传；
// - 「发送测试」= POST /subscriptions/test 吃**当前表单草稿体**（官方
//   语义：完整订阅体非 key 引用；同步单发不入箱），结果就地呈现
//   （ok / 状态码 / 耗时）。

const KEY_RE = /^[A-Za-z][A-Za-z0-9_-]+$/

export interface DialogDraft {
  key: string
  description: string
  enabled: boolean
  domain: string
  types: string[]
  criteria: CriteriaForm
  /** 编辑前订阅的非托管 criteria 键（提交时合并回去） */
  criteriaPassthrough: Record<string, unknown>
  url: string
  useSign: boolean
  debug: boolean
  hasSecret: boolean
}

function draftOf(sub: WebhookSubscription | null): DialogDraft {
  if (!sub) {
    return {
      key: '',
      description: '',
      enabled: false,
      domain: 'artifact',
      types: ['deployed'],
      criteria: { anyLocal: true, anyRemote: false, repoKeys: '', includePatterns: '', excludePatterns: '' },
      criteriaPassthrough: {},
      url: '',
      useSign: false,
      debug: false,
      hasSecret: false,
    }
  }
  const h = sub.handlers[0]
  return {
    key: sub.key,
    description: sub.description,
    enabled: sub.enabled,
    domain: sub.event_filter.domain,
    types: [...sub.event_filter.event_types],
    criteria: criteriaToForm(sub.event_filter.criteria),
    criteriaPassthrough: sub.event_filter.criteria ?? {},
    url: h?.url ?? '',
    useSign: h?.use_secret_for_signing ?? false,
    debug: sub.debug,
    hasSecret: h?.secret === WEBHOOK_SECRET_SENTINEL,
  }
}

export default function SubscriptionDialog({
  open,
  editing,
  readOnly,
  onClose,
  onSaved,
}: {
  open: boolean
  /** null = 新建；否则编辑该订阅（key 锁定） */
  editing: WebhookSubscription | null
  readOnly: boolean
  onClose: () => void
  onSaved: (created: boolean) => void
}) {
  const toast = useToast()
  const [draft, setDraft] = useState<DialogDraft>(() => draftOf(editing))
  const [secret, setSecret] = useState('')
  const [secretClear, setSecretClear] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState('')
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<TestOutcome | null>(null)

  useEffect(() => {
    if (open) {
      setDraft(draftOf(editing))
      setSecret('')
      setSecretClear(false)
      setFormError('')
      setTestResult(null)
      setSubmitting(false)
      setTesting(false)
    }
  }, [open, editing])

  const domainTypes = useMemo(() => typesOfDomain(draft.domain), [draft.domain])
  const managedCriteria = CRITERIA_MANAGED_DOMAINS.includes(draft.domain)
  const keyError = draft.key === '' ? '' : KEY_RE.test(draft.key) ? '' : 'key 须字母开头，仅字母/数字/下划线/连字符'
  const urlError = draft.url === '' ? '' : /^https?:\/\/.+/.test(draft.url) ? '' : 'URL 须为 http(s)://…'
  const emptyScope = managedCriteria && criteriaEmptyScope(draft.criteria)

  const canSubmit =
    !readOnly && !submitting && KEY_RE.test(draft.key) && draft.types.length > 0 && /^https?:\/\/.+/.test(draft.url)

  /** 草稿 → wire 体（save/test 共用；secret 三态在这里落） */
  const buildBody = (): SubscriptionRequest => {
    const criteria = managedCriteria
      ? criteriaFromForm(draft.criteria, draft.criteriaPassthrough)
      : editing
        ? (draft.criteriaPassthrough ?? {})
        : {}
    const handler: SubscriptionRequest['handlers'][number] = {
      handler_type: 'webhook',
      url: draft.url.trim(),
      use_secret_for_signing: draft.useSign,
      custom_http_headers: [],
    }
    if (secret.trim() !== '') handler.secret = secret // 明文：设置/轮换
    else if (secretClear) handler.secret = '' // 擦除
    // 留空且未勾清除：省略字段 = 保持（update）；create 时即不设置
    return {
      key: draft.key,
      project_key: '',
      description: draft.description,
      enabled: draft.enabled,
      event_filter: { domain: draft.domain, event_types: [...draft.types], criteria },
      handlers: [handler],
      debug: draft.debug,
    }
  }

  const doTest = async () => {
    setTesting(true)
    setTestResult(null)
    setFormError('')
    try {
      const outcome = await testSubscription(buildBody())
      setTestResult(outcome)
    } catch (err) {
      // 400 = 草稿未过校验（同 save 的错误面）；其余照抛文案
      setFormError(errText(err))
    } finally {
      setTesting(false)
    }
  }

  const doSave = async (created: boolean) => {
    setSubmitting(true)
    setFormError('')
    try {
      const body = buildBody()
      if (created) await createSubscription(body)
      else await updateSubscription(draft.key, body)
      toast.success(created ? `订阅 ${draft.key} 已创建` : `订阅 ${draft.key} 已保存`)
      onSaved(created)
    } catch (err) {
      const msg =
        err instanceof ApiError && err.status === 403
          ? `写入被拒（403）：${err.message}——webhook 为 pro+ 档特性，当前实例未解锁`
          : errText(err)
      setFormError(msg)
      toast.error(msg)
    } finally {
      setSubmitting(false)
    }
  }

  const toggleType = (name: string) => {
    setDraft((d) => ({
      ...d,
      types: d.types.includes(name) ? d.types.filter((t) => t !== name) : [...d.types, name],
    }))
  }

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth data-testid="wh-dialog">
      <DialogTitle>
        {editing ? `编辑订阅 ${editing.key}` : '新建 Webhook 订阅'}
        {readOnly && (
          <Typography variant="caption" component="div" color="text.secondary">
            只读管理员：服务端拒绝写操作（403 兜底）
          </Typography>
        )}
      </DialogTitle>
      <DialogContent dividers sx={{ display: 'grid', gap: 2, pt: 1 }}>
        <Box sx={{ display: 'grid', gridTemplateColumns: { sm: '1fr 2fr' }, gap: 2 }}>
          <TextField
            label="key（创建后不可改）"
            value={draft.key}
            onChange={(e) => setDraft((d) => ({ ...d, key: e.target.value }))}
            error={keyError !== ''}
            helperText={keyError || '字母开头，仅字母/数字/下划线/连字符'}
            disabled={!!editing || readOnly}
            slotProps={{ htmlInput: { 'data-testid': 'wh-form-key', lang: 'en' } }}
            sx={monoInputSx}
          />
          <TextField
            label="描述"
            value={draft.description}
            onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
            disabled={readOnly}
            slotProps={{ htmlInput: { 'data-testid': 'wh-form-description' } }}
          />
        </Box>

        <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
          <FormControlLabel
            control={
              <Checkbox
                checked={draft.enabled}
                onChange={(e) => setDraft((d) => ({ ...d, enabled: e.target.checked }))}
                disabled={readOnly}
                slotProps={{ input: { 'data-testid': 'wh-form-enabled' } as ComponentPropsWithoutRef<'input'> }}
              />
            }
            label="启用（enabled）——官方默认建后禁用"
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={draft.debug}
                onChange={(e) => setDraft((d) => ({ ...d, debug: e.target.checked }))}
                disabled={readOnly}
                slotProps={{ input: { 'data-testid': 'wh-form-debug' } as ComponentPropsWithoutRef<'input'> }}
              />
            }
            label="debug 排障记录（成功投递也入记录环）"
          />
        </Box>

        <Divider />
        <Typography variant="subtitle2">事件（event_filter——单域，域内多选）</Typography>
        <Box sx={{ display: 'grid', gridTemplateColumns: { sm: '1fr 1fr' }, gap: 2, alignItems: 'center' }}>
          <TextField
            label="事件域（13 域闭集）"
            select
            size="small"
            value={draft.domain}
            onChange={(e) => setDraft((d) => ({ ...d, domain: e.target.value, types: [] }))}
            disabled={readOnly}
            slotProps={{
              select: {
                native: true,
                inputProps: { 'data-testid': 'wh-form-domain', lang: 'en' } as ComponentPropsWithoutRef<'select'>,
              } as ComponentPropsWithoutRef<typeof Select>,
            }}
          >
            {EVENT_DOMAINS.map((d) => (
              <option key={d} value={d}>
                {DOMAIN_LABELS[d] ?? d}
              </option>
            ))}
          </TextField>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Chip size="small" variant="outlined" color={draft.types.length > 0 ? 'success' : 'default'} label={`已选 ${draft.types.length}`} />
            <Typography variant="caption" color="text.secondary">
              已接线（wired）= BinFlow 有触发源；休眠（dormant）= 可订阅、校验通过、永不触发
            </Typography>
          </Box>
        </Box>
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, maxHeight: 168, overflowY: 'auto', border: 1, borderColor: 'divider', p: 1 }}>
          {domainTypes.map((t) => (
            <FormControlLabel
              key={t.name}
              control={
                <Checkbox
                  checked={draft.types.includes(t.name)}
                  onChange={() => toggleType(t.name)}
                  disabled={readOnly}
                  size="small"
                  slotProps={{ input: { 'data-testid': `wh-form-type-${t.name}` } as ComponentPropsWithoutRef<'input'> }}
                />
              }
              label={
                <span lang="en">
                  {t.name}
                  {t.source === 'dormant' && (
                    <Typography component="span" variant="caption" color="text.disabled" sx={{ ml: 0.5 }}>
                      （休眠）
                    </Typography>
                  )}
                </span>
              }
              sx={{ m: 0, width: { xs: '100%', sm: '33%' } }}
            />
          ))}
        </Box>

        {managedCriteria ? (
          <>
            <Divider />
            <Typography variant="subtitle2">过滤条件（criteria——仓库范围 + Ant 路径通配）</Typography>
            <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
              <FormControlLabel
                control={
                  <Checkbox
                    checked={draft.criteria.anyLocal}
                    onChange={(e) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, anyLocal: e.target.checked } }))}
                    disabled={readOnly}
                    slotProps={{ input: { 'data-testid': 'wh-form-any-local' } as ComponentPropsWithoutRef<'input'> }}
                  />
                }
                label="任意 local 仓（anyLocal，含未来新建）"
              />
              <FormControlLabel
                control={
                  <Checkbox
                    checked={draft.criteria.anyRemote}
                    onChange={(e) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, anyRemote: e.target.checked } }))}
                    disabled={readOnly}
                    slotProps={{ input: { 'data-testid': 'wh-form-any-remote' } as ComponentPropsWithoutRef<'input'> }}
                  />
                }
                label="任意 remote 仓（anyRemote）"
              />
            </Box>
            {emptyScope && (
              <Alert severity="warning" data-testid="wh-form-scope-warn">
                未选择任何仓库范围（anyLocal/anyRemote/repoKeys 全空）——订阅合法但**不会命中任何事件**（空选择不匹配）。
              </Alert>
            )}
            <Box sx={{ display: 'grid', gridTemplateColumns: { sm: '1fr 1fr 1fr' }, gap: 2 }}>
              <TextField
                label="仓库（repoKeys，逗号分隔）"
                value={draft.criteria.repoKeys}
                onChange={(e) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, repoKeys: e.target.value } }))}
                disabled={readOnly}
                slotProps={{ htmlInput: { 'data-testid': 'wh-form-repos', lang: 'en' } }}
                sx={monoInputSx}
              />
              <TextField
                label="include 路径 pattern（Ant，逗号分隔）"
                value={draft.criteria.includePatterns}
                onChange={(e) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, includePatterns: e.target.value } }))}
                disabled={readOnly}
                slotProps={{ htmlInput: { 'data-testid': 'wh-form-include', lang: 'en' } }}
                sx={monoInputSx}
              />
              <TextField
                label="exclude 路径 pattern（优先命中即排除）"
                value={draft.criteria.excludePatterns}
                onChange={(e) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, excludePatterns: e.target.value } }))}
                disabled={readOnly}
                slotProps={{ htmlInput: { 'data-testid': 'wh-form-exclude', lang: 'en' } }}
                sx={monoInputSx}
              />
            </Box>
          </>
        ) : (
          <Alert severity="info" data-testid="wh-form-criteria-note">
            该域的 criteria 维度（build/RB/distribution 等）不在控制台最小面内——REST 全量面可配；已有配置原样保留。
          </Alert>
        )}

        <Divider />
        <Typography variant="subtitle2">投递目标（handler——每订阅恰一个）</Typography>
        <TextField
          label="接收器 URL（http/https）"
          value={draft.url}
          onChange={(e) => setDraft((d) => ({ ...d, url: e.target.value }))}
          error={urlError !== ''}
          helperText={urlError || '事件以 POST JSON 投递；3xx 不跟随、4xx 不重试、≥500/发送失败按固定 10s 重试至多 5 次'}
          disabled={readOnly}
          slotProps={{ htmlInput: { 'data-testid': 'wh-form-url', lang: 'en' } }}
          sx={monoInputSx}
        />
        <Box sx={{ display: 'grid', gridTemplateColumns: { sm: '2fr 1fr' }, gap: 2, alignItems: 'start' }}>
          <TextField
            label="secret（write-only）"
            type="password"
            value={secret}
            onChange={(e) => {
              setSecret(e.target.value)
              if (e.target.value !== '') setSecretClear(false)
            }}
            placeholder={draft.hasSecret ? '已设置——留空保持不变' : '未设置'}
            disabled={readOnly}
            slotProps={{ htmlInput: { 'data-testid': 'wh-form-secret', autoComplete: 'new-password' } }}
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={secretClear}
                onChange={(e) => {
                  setSecretClear(e.target.checked)
                  if (e.target.checked) setSecret('')
                }}
                disabled={readOnly || !draft.hasSecret}
                slotProps={{ input: { 'data-testid': 'wh-form-secret-clear' } as ComponentPropsWithoutRef<'input'> }}
              />
            }
            label="清除已存 secret"
          />
        </Box>
        <FormControlLabel
          control={
            <Checkbox
              checked={draft.useSign}
              onChange={(e) => setDraft((d) => ({ ...d, useSign: e.target.checked }))}
              disabled={readOnly}
              slotProps={{ input: { 'data-testid': 'wh-form-sign' } as ComponentPropsWithoutRef<'input'> }}
            />
          }
          label="use_secret_for_signing（true = 对载荷 HMAC-SHA256 签名置 X-JFrog-Event-Auth；false = secret 明文直传该头）"
        />

        {formError !== '' && (
          <Alert severity="error" data-testid="wh-form-error" role="alert">
            {formError}
          </Alert>
        )}
        {testResult && (
          <Alert
            severity={testResult.ok ? 'success' : 'error'}
            data-testid="wh-test-result"
          >
            <div>
              {testResult.message ?? '（无回执）'}（<span lang="en">HTTP {testResult.attempt?.status_code ?? '—'}</span>
              {testResult.attempt?.status_code === 0 ? '（无响应）' : ''}，耗时{' '}
              <span className="mono" lang="en">{testResult.attempt?.elapsed_millis ?? '—'}ms</span>）
            </div>
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        {/* 试发吃当前草稿体（官方语义：完整订阅体，不落盘）；校验失败就地报错 */}
        <Button
          onClick={() => void doTest()}
          disabled={readOnly || testing || !KEY_RE.test(draft.key) || draft.types.length === 0 || !/^https?:\/\/.+/.test(draft.url)}
          data-testid="wh-form-test"
        >
          {testing ? '发送中…' : '发送测试'}
        </Button>
        <Box sx={{ flexGrow: 1 }} />
        <Button onClick={onClose} data-testid="wh-form-cancel">
          取消
        </Button>
        <Button
          variant="contained"
          disabled={!canSubmit}
          onClick={() => void doSave(!editing)}
          data-testid="wh-form-submit"
        >
          {submitting ? '保存中…' : editing ? '保存' : '创建'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}
