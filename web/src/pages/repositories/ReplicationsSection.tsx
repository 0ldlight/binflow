import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { useToast } from '../../app/ToastContext'
import { CopyButton } from '@/components/layout/copy-button'
import { useConfirm } from '../../components/ConfirmDialog'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { ApiError, errText } from '../../lib/api'
import { formatBytes, formatCount } from '../../lib/format'
import {
  configsForRepo,
  createReplicationConfig,
  deleteReplicationConfig,
  listReplicationConfigs,
  putReplicationEnabled,
  testReplicationConfig,
  testReplicationDraft,
  validateReplicationName,
  validateReplicationTargetURL,
} from '../../lib/replications'
import type { ReplicationTestResult } from '../../lib/replications'
import type { ReplicationConfig, ReplicationConfigBody } from '../../lib/replications'
import { useAsync } from '../../lib/useAsync'
import { tr } from '../../i18n'

const tt = tr('repositories')

// 仓库编辑页 Replications 节（T-404，R1 裁定形态）：**内嵌节**——照仓编辑
// 页既有节形态（分区 section，T-383 form-section-* 族第七名），无 modal/
// 无 drawer（parity R9：勿 modal 化）。承载：
//
// - 配置列表（既有 GET /api/v1/replications，客户端按 source_repo 过滤）
//   + 新建/编辑**内嵌表单**（字段族见下）+ E1 删除确认（输入 name 档）
//   + 行内启停开关（PUT /v1/replications/{id}——T-405 并行票的联合腿，
//   合并前真实实例 404：失败 toast + 行内保持原值，不乐观更新）。
// - 字段族两档（R3——M16 Q1 终裁后的现值，T-462 承载）：
//   · BinFlow 实字段：name/源仓（锁定）/目标 URL/目标仓/凭据/带宽节流/
//     批量上限/enabled/**cron_exp（定时全量同步——T-450 起真字段）**——
//     全量进 payload；
//   · Artifactory 对齐**预留位**（enableEventReplication/pathPrefix/sync
//     三开关）：引擎无对位——如实标注「预留位（当前无效）」、控件恒禁用、
//     **绝不进 payload**（不伪造语义）。
// - 编辑语义（REST 无字段级 PUT 的票内定案）：保存 = **删除 + 重建**
//   （DELETE+POST）——配置 id 变化、未决任务级联清空；表单内 repl-
//   recreate-note 明示后果，仅启停走行内开关（T-405）。
// - 表单 Test（T-422，FR-138.2/规格 §9.2-C/§9.3）：「测试连接」对表单当前
//   候选发一次只读探测——创建态走草稿面（未保存直接测，§9.2-C-9），编辑
//   态未改动时探已存配置（服务端解封已存凭据）；结果内联呈现（repl-test /
//   repl-test-result 锚）。探测零副作用、凭据不回显不落日志（NFR-S75）、
//   不看全局封锁态（§9.2-C-10——封锁拦执行不拦测试）。
// - 门：读 = system:read（admin/readonly_admin；普通 user 403 → 本节降级
//   注记）；写 = system:write（仅全量 admin——readonly_admin 只读呈现，
//   服务端 403 兜底）。
//
// 四态（console-ux §5.1）：loading 骨架 / 空（含建配置引导）/ 错误（501·404
// 降级提示，其余 ErrorCard + 重试）/ 数据。digest·路径·URL mono + 拷贝。
//
// FE-P4 MUI 清场：Paper/TextField/Switch/Checkbox/Alert/Table/Typography →
// Tailwind + `.field` 族 + 原生 table/checkbox（role=switch 承载启停）。
// 锚族原样（form-section-replications / repl-empty / repl-create / repl-list /
// repl-row-<name> / repl-toggle-<name> / repl-edit-<name> / repl-delete-<name> /
// repl-form(-name|-url|-target-repo|-username|-password|-bandwidth|-items|
// -enabled|-cron|-reserved|-event|-prefix|-syncDeletes|-syncProperties|
// -syncStatistics|-error|-cancel|-submit) / repl-test(-result) /
// repl-delete-confirm-name / repl-denied / repl-degraded）；字段校验与
// 确认/toast 语义零改点。

/** 表单草稿（文本态承载数字字段——空 = 0/缺省，提交时折算） */
interface ReplFormState {
  name: string
  targetUrl: string
  targetRepo: string
  username: string
  password: string
  bandwidth: string
  items: string
  cron: string
  enabled: boolean
}

const CREATE_FORM: ReplFormState = {
  name: '',
  targetUrl: '',
  targetRepo: '',
  username: '',
  password: '',
  bandwidth: '',
  items: '',
  cron: '',
  enabled: true,
}

function editForm(c: ReplicationConfig): ReplFormState {
  return {
    // 编辑态 name 锁定（重建按原名；改名 = 删除后另建）
    name: c.name,
    targetUrl: c.target_url,
    targetRepo: c.target_repo,
    username: c.target_username,
    // 密码永不回显（NFR-S14 同款）：重建时留空 = 匿名目标——原配置带凭据
    // 则必须重新输入，表单注记明示
    password: '',
    bandwidth: c.max_bandwidth_bytes_per_sec > 0 ? String(c.max_bandwidth_bytes_per_sec) : '',
    items: c.max_items_per_push > 0 ? String(c.max_items_per_push) : '',
    // cron 回显台账表达式（清空提交 = 新配置纯事件轨——旧台账行随删除联动清）
    cron: c.cron_exp,
    enabled: c.enabled,
  }
}

function isNonNegInt(v: string): boolean {
  return v.trim() === '' || /^\d+$/.test(v.trim())
}

function numOrZero(v: string): number {
  const t = v.trim()
  return /^\d+$/.test(t) ? Number(t) : 0
}

/** 启停开关（原生 checkbox 承载 role=switch——锚在 input 上，键盘可达） */
function ToggleSwitch({
  checked,
  disabled,
  onCheckedChange,
  'aria-label': ariaLabel,
  'data-testid': testid,
}: {
  checked: boolean
  disabled?: boolean
  onCheckedChange: (next: boolean) => void
  'aria-label': string
  'data-testid': string
}) {
  return (
    <input
      type="checkbox"
      role="switch"
      checked={checked}
      disabled={disabled}
      aria-label={ariaLabel}
      data-testid={testid}
      onChange={(e) => onCheckedChange(e.target.checked)}
      className="size-4 accent-primary disabled:pointer-events-none disabled:opacity-50"
    />
  )
}

/** 预留位组（R3 缺口的如实呈现——cronExp 已于 T-462 转正出组）：恒禁用、
 *  零提交——视觉对齐 Artifactory 字段族，语义上不发明引擎不存在的行为。 */
function ReservedFields() {
  return (
    <div className="field" data-testid="repl-form-reserved">
      <p className="mb-2 text-dense text-muted-foreground">{tt('Artifactory 对齐字段（预留位——当前无效，不提交、不存储）')}</p>
      <label className="check-row">
        <input type="checkbox" checked readOnly disabled data-testid="repl-form-event" className="size-4" />
        {tt('事件复制（enableEventReplication）——BinFlow 引擎即事件驱动（上传即入队推送），语义恒真')}
      </label>
      <div className="field">
        <label htmlFor="repl-prefix">{tt('pathPrefix（路径前缀过滤）')}</label>
        <input
          id="repl-prefix"
          disabled
          placeholder={tt('（预留位）')}
          data-testid="repl-form-prefix"
          className="w-full"
          lang="en"
        />
        <p className="field-hint">{tt('预留位：引擎尚不支持路径前缀过滤（R3 缺口——后端模型扩展后启用）。')}</p>
      </div>
      {(
        [
          ['syncDeletes', tt('删除同步（syncDeletes）')],
          ['syncProperties', tt('属性同步（syncProperties）')],
          ['syncStatistics', tt('统计同步（syncStatistics）')],
        ] as const
      ).map(([key, label]) => (
        <label key={key} className="check-row">
          <input type="checkbox" disabled data-testid={`repl-form-${key}`} className="size-4" />
          {tt('{label}——预留位：引擎尚不支持', { label: label })}
        </label>
      ))}
    </div>
  )
}

export default function ReplicationsSection({
  repoKey,
  canWrite,
  focus,
}: {
  repoKey: string
  /** 全量 admin（system:write）——写入口的 L4 预收敛 */
  canWrite: boolean
  /** 深链 ?section=replications：滚动定位本节（列表 Run 动作/详情指针的落点） */
  focus?: boolean
}) {
  const toast = useToast()
  const confirm = useConfirm()
  const list = useAsync(listReplicationConfigs, [])
  const [editor, setEditor] = useState<{ base: ReplicationConfig | null; form: ReplFormState } | null>(null)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<number | null>(null)
  // T-422（FR-138.2）：表单 Test——草稿/已存配置的连通探测结果（内联呈现，
  // 成功绿/失败红；探测零副作用、凭据不回显不落日志 NFR-S75）。
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<ReplicationTestResult | null>(null)
  const rootRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (focus && rootRef.current) rootRef.current.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [focus])

  const configs = useMemo(() => configsForRepo(list.data ?? [], repoKey), [list.data, repoKey])

  const startCreate = () => {
    setFormError(null)
    setTestResult(null)
    setEditor({ base: null, form: { ...CREATE_FORM } })
  }
  const startEdit = (c: ReplicationConfig) => {
    setFormError(null)
    setTestResult(null)
    setEditor({ base: c, form: editForm(c) })
  }

  const buildBody = (f: ReplFormState): ReplicationConfigBody => ({
    name: f.name.trim(),
    source_repo: repoKey,
    target_url: f.targetUrl.trim(),
    target_repo: f.targetRepo.trim(),
    target_username: f.username.trim(),
    target_password: f.password,
    max_bandwidth_bytes_per_sec: numOrZero(f.bandwidth),
    max_items_per_push: numOrZero(f.items),
    // T-462：cron_exp 转正——显式随体（空 = 纯事件轨；服务端校验合法性，
    // Invalid cronExp 点名原因行内呈现）
    cron_exp: f.cron.trim(),
    enabled: f.enabled,
  })

  const gate = (f: ReplFormState, mode: 'create' | 'edit'): string | null => {
    if (mode === 'create') {
      const e = validateReplicationName(f.name.trim())
      if (e) return e
    }
    const url = validateReplicationTargetURL(f.targetUrl)
    if (url) return url
    if (f.targetRepo.trim() === '') return tt('目标仓 key 未填')
    if (!isNonNegInt(f.bandwidth)) return tt('带宽节流需为非负整数（字节/秒）')
    if (!isNonNegInt(f.items)) return tt('批量上限需为非负整数')
    return null
  }

  const doSave = async () => {
    if (!editor) return
    const { base, form } = editor
    const mode = base ? 'edit' : 'create'
    setFormError(null)
    setSaving(true)
    try {
      const body = buildBody(form)
      if (mode === 'create') {
        await createReplicationConfig(body)
        toast.success(tt('复制配置 {v1} 已创建', { v1: body.name }))
        setEditor(null)
        list.reload()
        return
      }
      // 编辑 = 删除 + 重建（REST 无字段级 PUT 的票内定案，见文件头注）
      await deleteReplicationConfig(base!.name)
      try {
        await createReplicationConfig(body)
        toast.success(tt('复制配置 {v1} 已重建（字段修改生效）', { v1: body.name }))
        setEditor(null)
      } catch (err) {
        // 旧配置已删、重建失败：如实呈现——表单保持打开（重试即再建）
        setFormError(
          tt('原配置已删除，但重建失败：{v1}——表单保持打开，修正后重试创建（未决任务已随删除清空）。', { v1: errText(err) }),
        )
      }
      list.reload()
    } catch (err) {
      setFormError(errText(err))
    } finally {
      setSaving(false)
    }
  }

  const doToggle = async (c: ReplicationConfig, next: boolean) => {
    setBusyId(c.id)
    try {
      const updated = await putReplicationEnabled(c.id, next)
      toast.success(tt('复制配置 {v1} 已{v2}', { v1: c.name, v2: updated.enabled ? tt('启用') : tt('停用') }))
      list.reload()
    } catch (err) {
      // T-405 合并前真实实例对该动词 404——不乐观更新，行内保持原值
      const status = err instanceof ApiError ? err.status : 0
      toast.error(
        tt('启停失败{v1}：{v2}', { v1: status ? tt('（HTTP {status}）', { status: status }) : '', v2: errText(err) }) +
          (status === 404 ? tt('——PUT /api/v1/replications/{id} 尚未在本实例落地（T-405 联合腿）') : ''),
      )
    } finally {
      setBusyId(null)
    }
  }

  // T-422（FR-138.2）：表单 Test——创建态走草稿面（未保存候选直接测，
  // §9.2-C-9）；编辑态表单未改动且未输入新密码时探已存配置（服务端解封
  // 已存凭据），任一改动则整候选草稿测（旧密文绝不随新候选外发）。探测
  // 零副作用、凭据不回显（NFR-S75）；不看全局封锁态（§9.2-C-10）。
  const doTest = async () => {
    if (!editor) return
    const { base, form } = editor
    setTesting(true)
    setTestResult(null)
    setFormError(null)
    try {
      let res: ReplicationTestResult
      if (!base) {
        res = await testReplicationDraft({
          ...(form.name.trim() !== '' ? { name: form.name.trim() } : {}),
          target_url: form.targetUrl.trim(),
          target_repo: form.targetRepo.trim(),
          target_username: form.username.trim(),
          target_password: form.password,
        })
      } else {
        const unchanged =
          form.targetUrl.trim() === base.target_url &&
          form.targetRepo.trim() === base.target_repo &&
          form.username.trim() === base.target_username &&
          form.password === ''
        res = unchanged
          ? await testReplicationConfig(base.id)
          : await testReplicationConfig(base.id, {
              target_url: form.targetUrl.trim(),
              target_repo: form.targetRepo.trim(),
              target_username: form.username.trim(),
              ...(form.password !== '' ? { target_password: form.password } : {}),
            })
      }
      setTestResult(res)
    } catch (err) {
      setFormError(tt('测试连接无法执行：{v1}', { v1: errText(err) }))
    } finally {
      setTesting(false)
    }
  }

  const doDelete = async (c: ReplicationConfig) => {
    const holder = { typed: '' }
    const body: ReactNode = (
      <>
        <p>{tt('将删除复制配置')} <b className="mono" lang="en">{c.name}</b>{tt('（')}<span className="mono" lang="en">{c.source_repo} → {c.target_url}/{c.target_repo}</span>{tt('）。 其')}<b>{tt('未决推送任务随之级联清空')}</b>{tt('，已推送制品不受影响；此操作没有撤销。')}</p>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0 }}>
          <label htmlFor={`repl-del-confirm-${c.name}`}>{tt('输入配置名')} <b className="mono" lang="en">{c.name}</b> {tt('以确认：')}</label>
          <input
            id={`repl-del-confirm-${c.name}`}
            className="confirm-input"
            autoComplete="off"
            onChange={(e) => {
              holder.typed = e.target.value
            }}
            data-testid="repl-delete-confirm-name"
            lang="en"
          />
        </div>
      </>
    )
    const ok = await confirm({
      title: tt('删除复制配置'),
      body,
      danger: true,
      confirmLabel: tt('删除配置'),
      confirmDisabled: () => holder.typed !== c.name,
    })
    if (!ok) return
    try {
      await deleteReplicationConfig(c.name)
      toast.success(tt('复制配置 {v1} 已删除', { v1: c.name }))
      if (editor?.base?.id === c.id) setEditor(null)
      list.reload()
    } catch (err) {
      toast.error(tt('删除失败：{v1}', { v1: errText(err) }))
    }
  }

  const f = editor?.form
  const gateErr = f ? gate(f, editor?.base ? 'edit' : 'create') : null
  const nameErr = f && !editor?.base ? validateReplicationName(f.name.trim()) : null
  const urlErr = f ? validateReplicationTargetURL(f.targetUrl) : null

  return (
    <section
      aria-label={tt('复制')}
      ref={rootRef}
      data-testid="form-section-replications"
      data-active={focus ? 'true' : undefined}
      className="rounded-md border border-border bg-surface-1 p-4 pb-3"
      style={{ marginBottom: 8 }}
    >
      <h3 className="mb-1 text-[15px] font-semibold">{tt('复制（Replications）')}</h3>
      <p className="mb-3 text-dense text-muted-foreground">{tt('单向 push：本仓 → 目标实例仓（ADR-0021）。引擎为事件驱动——上传即入队推送，失败按指数退避重试； 配置面为全局管理端点（system:read/write）。')}</p>

      {list.status === 'loading' && <StateSkeleton lines={3} />}

      {list.status === 'forbidden' && (
        <p className="field-hint" data-testid="repl-denied">{tt('复制配置为全局管理面（GET /api/v1/replications 需 system:read）——当前会话无权查看；仓库配置本身的编辑不受影响。')}</p>
      )}

      {list.status === 'error' && list.error && (list.error.status === 501 || list.error.status === 404) && (
        <p className="field-hint" data-testid="repl-degraded">
          {list.error.status === 501
            ? tt('本实例未启用复制（端点 501）；在实例配置启用后本节自动呈现。')
            : tt('本实例的复制端点不可用（HTTP 404）。')}
        </p>
      )}

      {list.status === 'error' && list.error && list.error.status !== 501 && list.error.status !== 404 && (
        <ErrorCard error={list.error} onRetry={list.reload} />
      )}

      {list.status === 'ok' && configs.length === 0 && !editor && (
        <div data-testid="repl-empty">
          <EmptyState
            message={tt('本仓尚无复制配置')}
            hint={tt('配置一条 push 目标后，本仓新上传将异步推送到目标实例（事件驱动，≤1 分钟 sweep 兜底）。')}
            action={
              canWrite ? (
                <Button size="sm" onClick={startCreate} data-testid="repl-create">{tt('＋ 新建复制配置')}</Button>
              ) : undefined
            }
          />
        </div>
      )}

      {list.status === 'ok' && configs.length > 0 && (
        <>
          <table data-testid="repl-list" className="w-full border-collapse text-dense">
            <thead>
              <tr className="border-b border-border text-left text-aux text-muted-foreground">
                <th scope="col" className="px-2 py-1.5 font-medium">{tt('启用')}</th>
                <th scope="col" className="px-2 py-1.5 font-medium">{tt('名称')}</th>
                <th scope="col" className="px-2 py-1.5 font-medium">{tt('目标（实例 / 仓）')}</th>
                <th scope="col" className="px-2 py-1.5 font-medium">{tt('凭据')}</th>
                <th scope="col" className="px-2 py-1.5 font-medium">{tt('调度')}</th>
                <th scope="col" className="px-2 py-1.5 font-medium">{tt('节流 / 批量')}</th>
                {canWrite && (
                  <th scope="col" className="px-2 py-1.5 text-right font-medium">{tt('操作')}</th>
                )}
              </tr>
            </thead>
            <tbody>
              {configs.map((c) => (
                <tr key={c.id} data-testid={`repl-row-${c.name}`} className="border-b border-border/60 align-top">
                  <td className="px-2 py-1.5">
                    <ToggleSwitch
                      checked={c.enabled}
                      disabled={!canWrite || busyId === c.id}
                      onCheckedChange={(next) => void doToggle(c, next)}
                      aria-label={tt('启用复制配置 {v1}', { v1: c.name })}
                      data-testid={`repl-toggle-${c.name}`}
                    />
                  </td>
                  <td className="px-2 py-1.5 font-mono" lang="en">
                    {c.name}
                  </td>
                  <td className="max-w-[320px] break-all px-2 py-1.5 font-mono" lang="en">
                    {c.target_url} <CopyButton value={c.target_url} label={tt('目标 URL {v1}', { v1: c.name })} />
                    <br />→ {c.target_repo}
                  </td>
                  <td className="px-2 py-1.5">{c.target_username || <span className="text-muted-foreground">{tt('匿名')}</span>}</td>
                  <td className="px-2 py-1.5" data-testid={`repl-row-sched-${c.name}`}>
                    {c.cron_exp ? (
                      <>
                        <span className="font-mono" lang="en">{c.cron_exp}</span>
                        <br />
                        <span className="text-2" title={c.next_schedule_sync}>
                          {c.enabled && c.next_schedule_sync
                            ? tt('下次 {v1}', { v1: c.next_schedule_sync.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') })
                            : c.enabled
                              ? tt('未排')
                              : tt('已停用')}
                        </span>
                      </>
                    ) : (
                      <span className="text-muted-foreground">{tt('事件驱动')}</span>
                    )}
                  </td>
                  <td className="px-2 py-1.5 font-mono" lang="en">
                    {c.max_bandwidth_bytes_per_sec > 0 ? `${formatBytes(c.max_bandwidth_bytes_per_sec)}/s` : '—'} /{' '}
                    {formatCount(c.max_items_per_push)}
                  </td>
                  {canWrite && (
                    <td className="whitespace-nowrap px-2 py-1.5 text-right">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => startEdit(c)}
                        data-testid={`repl-edit-${c.name}`}
                      >{tt('编辑')}</Button>{' '}
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => void doDelete(c)}
                        data-testid={`repl-delete-${c.name}`}
                        aria-label={tt('删除复制配置 {v1}', { v1: c.name })}
                      >{tt('删除')}</Button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
          {canWrite && !editor && (
            <Button variant="outline" size="sm" onClick={startCreate} data-testid="repl-create" className="mt-3">{tt('＋ 新建复制配置')}</Button>
          )}
        </>
      )}

      {editor && f && (
        <div className="repl-form" data-testid="repl-form">
          <h4 className="mt-4 mb-2 text-[15px] font-semibold">
            {editor.base ? tt('编辑复制配置 {v1}', { v1: editor.base.name }) : tt('新建复制配置')}
          </h4>

          {editor.base && (
            <div className="warn-box" data-testid="repl-recreate-note">{tt('⚠ 保存 =')} <b>{tt('删除并重建')}</b>{tt('该配置（REST 无字段级更新——启停请用行内开关）：配置 id 变化，')}<b>{tt('未决推送任务随之清空')}</b>{tt('。原密码不回显——原配置带凭据时需重新输入（留空 = 匿名目标）。')}</div>
          )}

          {!editor.base && (
            <div className="field">
              <label htmlFor="repl-name">{tt('配置名 *')}</label>
              <input
                id="repl-name"
                value={f.name}
                disabled={saving}
                onChange={(e) => setEditor({ ...editor, form: { ...f, name: e.target.value } })}
                placeholder="dr-site"
                className="mono-input w-full"
                data-testid="repl-form-name"
                lang="en"
                aria-invalid={!!nameErr}
              />
              {nameErr ? (
                <p className="field-error" data-testid="repl-form-name-error" role="alert">
                  {nameErr}
                </p>
              ) : (
                <p className="field-hint">{tt('1~64 字符，字母/数字/./_/-，首字符字母数字；全局唯一（409 终裁）。')}</p>
              )}
            </div>
          )}
          {editor.base && (
            <div className="kv">
              <span className="k">{tt('配置名')}</span>
              <span className="mono" lang="en">
                {editor.base.name}
              </span>
            </div>
          )}

          <div className="kv" style={{ marginBottom: 8 }}>
            <span className="k">{tt('源仓库')}</span>
            <span className="mono" lang="en">
              {repoKey}
            </span>
          </div>

          <div className="field">
            <label htmlFor="repl-url">{tt('目标实例 URL *')}</label>
            <input
              id="repl-url"
              value={f.targetUrl}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...f, targetUrl: e.target.value } })}
              placeholder="https://dr.example.com"
              className="mono-input w-full"
              data-testid="repl-form-url"
              lang="en"
              aria-invalid={!!urlErr}
            />
            {urlErr ? (
              <p className="field-error" role="alert">
                {urlErr}
              </p>
            ) : (
              <p className="field-hint">{tt('目标 BinFlow/Artifactory 实例基址（绝对 http/https）；私网地址合法。')}</p>
            )}
          </div>

          <div className="field">
            <label htmlFor="repl-target-repo">{tt('目标仓 key *')}</label>
            <input
              id="repl-target-repo"
              value={f.targetRepo}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...f, targetRepo: e.target.value } })}
              placeholder="libs-release"
              className="mono-input w-full"
              data-testid="repl-form-target-repo"
              lang="en"
            />
            <p className="field-hint">{tt('目标实例上的仓 key（推送写入面；不存在时任务失败并重试）。')}</p>
          </div>

          <div className="field">
            <label htmlFor="repl-username">{tt('用户名（目标认证，可选）')}</label>
            <input
              id="repl-username"
              value={f.username}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...f, username: e.target.value } })}
              data-testid="repl-form-username"
              className="w-full"
            />
          </div>
          <div className="field">
            <label htmlFor="repl-password">{tt('密码（目标认证，可选）')}</label>
            <input
              id="repl-password"
              type="password"
              autoComplete="new-password"
              value={f.password}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...f, password: e.target.value } })}
              placeholder={editor.base ? tt('永不回显——留空 = 匿名目标') : tt('匿名目标可留空')}
              data-testid="repl-form-password"
              className="w-full"
            />
            <p className="field-hint">{tt('只写不读（ADR-0012 封存）。需要实例配置凭据主键（BINFLOW_REMOTE_CREDENTIALS_KEY），未配时带密码提交收到 400（文案原样呈现）。')}</p>
          </div>

          <div className="field">
            <label htmlFor="repl-bandwidth">{tt('带宽节流 max_bandwidth_bytes_per_sec（字节/秒）')}</label>
            <input
              id="repl-bandwidth"
              value={f.bandwidth}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...f, bandwidth: e.target.value } })}
              placeholder="0"
              className="mono-input w-full"
              data-testid="repl-form-bandwidth"
              inputMode="numeric"
              style={{ maxWidth: 300 }}
              aria-invalid={!isNonNegInt(f.bandwidth)}
            />
            <p className="field-hint">{tt('0 = 不限（BinFlow 超集字段——Artifactory 无）。')}</p>
          </div>
          <div className="field">
            <label htmlFor="repl-items">{tt('单次批量上限 max_items_per_push')}</label>
            <input
              id="repl-items"
              value={f.items}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...f, items: e.target.value } })}
              placeholder="1000"
              className="mono-input w-full"
              data-testid="repl-form-items"
              inputMode="numeric"
              style={{ maxWidth: 300 }}
              aria-invalid={!isNonNegInt(f.items)}
            />
            <p className="field-hint">{tt('0 = 缺省 1000（BinFlow 超集字段）。')}</p>
          </div>

          <label className="check-row">
            <input
              type="checkbox"
              checked={f.enabled}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...f, enabled: e.target.checked } })}
              data-testid="repl-form-enabled"
              className="size-4"
            />
            {tt('启用（enabled）——停用配置保留但不再推送')}
          </label>

          {/* T-462（FR-150.4 / M15 Q5 推翻）：cronExp 转正——定时全量同步
              表达式（021 台账 domain=replication）。空 = 仅事件轨；调度只
              触发全量对账，增量仍走事件轨（同制品不双推）。合法性服务端
              校验（Invalid cronExp 点名原因行内呈现）。 */}
          <div className="field">
            <label htmlFor="repl-cron">{tt('cronExp（定时全量同步，可选——Quartz 六/七域）')}</label>
            <input
              id="repl-cron"
              value={f.cron}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...f, cron: e.target.value } })}
              placeholder="0 0 /12 * * ?"
              className="mono-input w-full"
              data-testid="repl-form-cron"
              lang="en"
              style={{ maxWidth: 300 }}
            />
            <p className="field-hint" data-testid="repl-form-cron-hint">{tt('空 = 仅事件轨（上传即推送）；填表达式 = 另按点到点全量对账（与 Replicate Now 同载体， 同制品不双推）。启停开关同时停/启两条轨。')}</p>
            {editor.base && editor.base.cron_exp && (
              <div className="kv" style={{ marginBottom: 0 }}>
                <span className="k">{tt('下次定时同步')}</span>
                <span className="mono" lang="en">
                  {editor.base.next_schedule_sync || tt('—（停用或不可达）')}
                </span>
              </div>
            )}
          </div>

          <ReservedFields />

          {formError && (
            <div data-testid="repl-form-error" role="alert" className="mt-4 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense">
              <div>{formError}</div>
            </div>
          )}

          {testResult && (
            <div
              data-testid="repl-test-result"
              role="status"
              className={`mt-4 rounded-md border px-3 py-2 text-dense ${testResult.ok ? 'border-success/40 bg-success/10' : 'border-destructive/40 bg-destructive/10'}`}
            >
              <div lang="en">{testResult.message}</div>
              <div>
                {testResult.ok ? tt('目标可达且凭据被接受') : tt('探测未通过')}
                {testResult.status_code > 0 ? tt('（目标应答 HTTP {v1}）', { v1: testResult.status_code }) : tt('（未触达目标）')}
              </div>
            </div>
          )}

          <div className="form-actions">
            <Button
              variant="outline"
              size="sm"
              disabled={saving}
              onClick={() => {
                setEditor(null)
                setFormError(null)
                setTestResult(null)
              }}
              data-testid="repl-form-cancel"
            >{tt('取消')}</Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!!urlErr || f.targetRepo.trim() === '' || testing || saving}
              title={tt('对表单当前候选发一次只读连通探测（不落盘、不看封锁态）')}
              onClick={() => void doTest()}
              data-testid="repl-test"
            >
              {testing ? tt('测试中…') : tt('测试连接')}
            </Button>
            <Button
              size="sm"
              disabled={!!gateErr || saving}
              title={gateErr ?? undefined}
              onClick={() => void doSave()}
              data-testid="repl-form-submit"
            >
              {saving ? tt('保存中…') : editor.base ? tt('删除并重建') : tt('创建配置')}
            </Button>
          </div>
        </div>
      )}
    </section>
  )
}
