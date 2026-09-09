// 新建/编辑订阅对话框（M13 T-366——P3 新栈重写：shadcn Dialog md 档；
// 交互形态 = console-artifactory-parity M3/M4：居中 Dialog、动作右下
// Cancel 左主按钮右、Esc/遮罩关闭）。
// wire 语义（webhook.md §1/§2——行为契约逐条）：
// - key 创建后不可改（编辑态锁定展示）；^[A-Za-z][A-Za-z0-9_-]+$；
// - enabled 默认 false（官方 schema default）；
// - event_filter = 单域 + 域内多事件型（66 型闭集 13 域分组下拉；
//   wired/dormant 如实标注）；
// - criteria：本体三域表单托管五键；其余域原样透传（不丢配置）；
// - handler 恰一个：url 必填；secret 三态：留空 = 保持（省略字段）、
//   明文 = 设置/轮换、勾选「清除」= ""（擦除）——哨兵绝不回传；
// - 「发送测试」= POST /subscriptions/test 吃当前表单草稿体（完整订阅体
//   非 key 引用；同步单发不入箱），结果就地呈现。
// 锚族原样：wh-dialog/wh-form-key/description/enabled/debug/domain/type-<t>/
// any-local/any-remote/repos/include/exclude/scope-warn/criteria-note/url/
// secret/secret-clear/sign/error/test(-result)?/cancel/submit。
import { useEffect, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertBox, Badge, CheckRow } from '@/components/layout/bits'
import { TextInput, NativeSelect } from '@/components/layout/fields'
import { toast } from '@/lib/toast'
import { ApiError, errText } from '@/lib/api'
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
} from '@/lib/webhooks'
import type { CriteriaForm, SubscriptionRequest, TestOutcome, WebhookSubscription } from '@/lib/webhooks'
import { tr } from '@/i18n'

const tt = tr('webhooks')

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
  const keyError = draft.key === '' ? '' : KEY_RE.test(draft.key) ? '' : tt('key 须字母开头，仅字母/数字/下划线/连字符')
  const urlError = draft.url === '' ? '' : /^https?:\/\/.+/.test(draft.url) ? '' : tt('URL 须为 http(s)://…')
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
      toast.success(created ? tt('订阅 {v1} 已创建', { v1: draft.key }) : tt('订阅 {v1} 已保存', { v1: draft.key }))
      onSaved(created)
    } catch (err) {
      const msg =
        err instanceof ApiError && err.status === 403
          ? tt('写入被拒（403）：{v1}——webhook 为 pro+ 档特性，当前实例未解锁', { v1: err.message })
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
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose() }}>
      <DialogContent className="max-h-[90vh] w-[min(880px,calc(100vw-32px))] overflow-y-auto sm:max-w-[880px]" data-testid="wh-dialog">
        <DialogHeader>
          <DialogTitle>
            {editing ? tt('编辑订阅 {v1}', { v1: editing.key }) : tt('新建 Webhook 订阅')}
            {readOnly && (
              <span className="block text-aux font-normal text-muted-foreground">{tt('只读管理员：服务端拒绝写操作（403 兜底）')}</span>
            )}
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <div className="field">
              <label htmlFor="wh-key">{tt('key（创建后不可改）')}</label>
              <TextInput
                id="wh-key"
                mono
                lang="en"
                value={draft.key}
                onChange={(e) => setDraft((d) => ({ ...d, key: e.target.value }))}
                aria-invalid={keyError !== '' || undefined}
                disabled={!!editing || readOnly}
                data-testid="wh-form-key"
              />
              {keyError ? (
                <p className="field-error">{keyError}</p>
              ) : (
                <p className="field-hint">{tt('字母开头，仅字母/数字/下划线/连字符')}</p>
              )}
            </div>
            <div className="field sm:col-span-2">
              <label htmlFor="wh-desc">{tt('描述')}</label>
              <TextInput
                id="wh-desc"
                value={draft.description}
                onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
                disabled={readOnly}
                data-testid="wh-form-description"
              />
            </div>
          </div>

          <div className="flex flex-wrap gap-4">
            <CheckRow
              checked={draft.enabled}
              onChange={(next) => setDraft((d) => ({ ...d, enabled: next }))}
              disabled={readOnly}
              label={tt('启用（enabled）——官方默认建后禁用')}
              testid="wh-form-enabled"
            />
            <CheckRow
              checked={draft.debug}
              onChange={(next) => setDraft((d) => ({ ...d, debug: next }))}
              disabled={readOnly}
              label={tt('debug 排障记录（成功投递也入记录环）')}
              testid="wh-form-debug"
            />
          </div>

          <div className="border-t border-border" />
          <h4 className="text-dense font-semibold">{tt('事件（event_filter——单域，域内多选）')}</h4>
          <div className="grid grid-cols-1 items-center gap-4 sm:grid-cols-2">
            <div className="field">
              <label htmlFor="wh-domain">{tt('事件域（13 域闭集）')}</label>
              <NativeSelect
                id="wh-domain"
                value={draft.domain}
                onChange={(e) => setDraft((d) => ({ ...d, domain: e.target.value, types: [] }))}
                disabled={readOnly}
                options={EVENT_DOMAINS.map((d) => ({ value: d, label: DOMAIN_LABELS[d] ?? d }))}
                data-testid="wh-form-domain"
              />
            </div>
            <div className="flex items-center gap-2">
              <Badge variant={draft.types.length > 0 ? 'success' : 'neutral'}>{tt('已选 {v1}', { v1: draft.types.length })}</Badge>
              <span className="text-aux text-muted-foreground">{tt('已接线（wired）= BinFlow 有触发源；休眠（dormant）= 可订阅、校验通过、永不触发')}</span>
            </div>
          </div>
          <div className="flex max-h-[168px] flex-wrap gap-1 overflow-y-auto rounded-sm border border-border p-2">
            {domainTypes.map((t) => (
              <label key={t.name} className="flex w-full items-center gap-1.5 text-dense sm:w-1/3">
                <input
                  type="checkbox"
                  checked={draft.types.includes(t.name)}
                  onChange={() => toggleType(t.name)}
                  disabled={readOnly}
                  data-testid={`wh-form-type-${t.name}`}
                />
                <span lang="en">
                  {t.name}
                  {t.source === 'dormant' && <span className="ml-0.5 text-aux text-muted-foreground">{tt('（休眠）')}</span>}
                </span>
              </label>
            ))}
          </div>

          {managedCriteria ? (
            <>
              <div className="border-t border-border" />
              <h4 className="text-dense font-semibold">{tt('过滤条件（criteria——仓库范围 + Ant 路径通配）')}</h4>
              <div className="flex flex-wrap gap-4">
                <CheckRow
                  checked={draft.criteria.anyLocal}
                  onChange={(next) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, anyLocal: next } }))}
                  disabled={readOnly}
                  label={tt('任意 local 仓（anyLocal，含未来新建）')}
                  testid="wh-form-any-local"
                />
                <CheckRow
                  checked={draft.criteria.anyRemote}
                  onChange={(next) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, anyRemote: next } }))}
                  disabled={readOnly}
                  label={tt('任意 remote 仓（anyRemote）')}
                  testid="wh-form-any-remote"
                />
              </div>
              {emptyScope && (
                <AlertBox severity="warning" testid="wh-form-scope-warn">
                  {tt('未选择任何仓库范围（anyLocal/anyRemote/repoKeys 全空）——订阅合法但**不会命中任何事件**（空选择不匹配）。')}
                </AlertBox>
              )}
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                <div className="field">
                  <label htmlFor="wh-repos">{tt('仓库（repoKeys，逗号分隔）')}</label>
                  <TextInput
                    id="wh-repos"
                    mono
                    lang="en"
                    value={draft.criteria.repoKeys}
                    onChange={(e) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, repoKeys: e.target.value } }))}
                    disabled={readOnly}
                    data-testid="wh-form-repos"
                  />
                </div>
                <div className="field">
                  <label htmlFor="wh-include">{tt('include 路径 pattern（Ant，逗号分隔）')}</label>
                  <TextInput
                    id="wh-include"
                    mono
                    lang="en"
                    value={draft.criteria.includePatterns}
                    onChange={(e) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, includePatterns: e.target.value } }))}
                    disabled={readOnly}
                    data-testid="wh-form-include"
                  />
                </div>
                <div className="field">
                  <label htmlFor="wh-exclude">{tt('exclude 路径 pattern（优先命中即排除）')}</label>
                  <TextInput
                    id="wh-exclude"
                    mono
                    lang="en"
                    value={draft.criteria.excludePatterns}
                    onChange={(e) => setDraft((d) => ({ ...d, criteria: { ...d.criteria, excludePatterns: e.target.value } }))}
                    disabled={readOnly}
                    data-testid="wh-form-exclude"
                  />
                </div>
              </div>
            </>
          ) : (
            <AlertBox severity="info" testid="wh-form-criteria-note">
              {tt('该域的 criteria 维度（build/RB/distribution 等）不在控制台最小面内——REST 全量面可配；已有配置原样保留。')}
            </AlertBox>
          )}

          <div className="border-t border-border" />
          <h4 className="text-dense font-semibold">{tt('投递目标（handler——每订阅恰一个）')}</h4>
          <div className="field">
            <label htmlFor="wh-url">{tt('接收器 URL（http/https）')}</label>
            <TextInput
              id="wh-url"
              mono
              lang="en"
              value={draft.url}
              onChange={(e) => setDraft((d) => ({ ...d, url: e.target.value }))}
              aria-invalid={urlError !== '' || undefined}
              disabled={readOnly}
              data-testid="wh-form-url"
            />
            {urlError ? (
              <p className="field-error">{urlError}</p>
            ) : (
              <p className="field-hint">{tt('事件以 POST JSON 投递；3xx 不跟随、4xx 不重试、≥500/发送失败按固定 10s 重试至多 5 次')}</p>
            )}
          </div>
          <div className="grid grid-cols-1 items-start gap-4 sm:grid-cols-3">
            <div className="field sm:col-span-2">
              <label htmlFor="wh-secret">{tt('secret（write-only）')}</label>
              <TextInput
                id="wh-secret"
                type="password"
                autoComplete="new-password"
                value={secret}
                onChange={(e) => {
                  setSecret(e.target.value)
                  if (e.target.value !== '') setSecretClear(false)
                }}
                placeholder={draft.hasSecret ? tt('已设置——留空保持不变') : tt('未设置')}
                disabled={readOnly}
                data-testid="wh-form-secret"
              />
            </div>
            <CheckRow
              checked={secretClear}
              onChange={(next) => {
                setSecretClear(next)
                if (next) setSecret('')
              }}
              disabled={readOnly || !draft.hasSecret}
              label={tt('清除已存 secret')}
              testid="wh-form-secret-clear"
            />
          </div>
          <CheckRow
            checked={draft.useSign}
            onChange={(next) => setDraft((d) => ({ ...d, useSign: next }))}
            disabled={readOnly}
            label={tt('use_secret_for_signing（true = 对载荷 HMAC-SHA256 签名置 X-JFrog-Event-Auth；false = secret 明文直传该头）')}
            testid="wh-form-sign"
          />

          {formError !== '' && (
            <AlertBox severity="error" testid="wh-form-error">
              {formError}
            </AlertBox>
          )}
          {testResult && (
            <AlertBox severity={testResult.ok ? 'success' : 'error'} testid="wh-test-result">
              <div>
                {testResult.message ?? tt('（无回执）')}{tt('（')}<span lang="en">HTTP {testResult.attempt?.status_code ?? '—'}</span>
                {testResult.attempt?.status_code === 0 ? tt('（无响应）') : ''}{tt('，耗时')}{' '}
                <span className="font-mono" lang="en">{testResult.attempt?.elapsed_millis ?? '—'}ms</span>{tt('）')}
              </div>
            </AlertBox>
          )}
        </div>
        <DialogFooter>
          {/* 试发吃当前草稿体（官方语义：完整订阅体，不落盘）；校验失败就地报错 */}
          <Button
            variant="outline"
            onClick={() => void doTest()}
            disabled={readOnly || testing || !KEY_RE.test(draft.key) || draft.types.length === 0 || !/^https?:\/\/.+/.test(draft.url)}
            data-testid="wh-form-test"
          >
            {testing ? tt('发送中…') : tt('发送测试')}
          </Button>
          <span className="flex-1" />
          <Button variant="outline" onClick={onClose} data-testid="wh-form-cancel">{tt('取消')}</Button>
          <Button disabled={!canSubmit} onClick={() => void doSave(!editing)} data-testid="wh-form-submit">
            {submitting ? tt('保存中…') : editing ? tt('保存') : tt('创建')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
