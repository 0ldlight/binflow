// Settings 新设页（capability matrix #25 解锁——「v1/system/settings 0 调用」
// 的真身承载；P3 新写）：
// - 运行时旋钮回显卡：GET /v1/system/settings（folder_download 六字段 +
//   trashcan.retention_days——解析配置的回显，只读姿态如实：这些是文件级
//   restart-effective 旋钮，REST 面只有 GET，改经实例 YAML）。
// - QRL 面板（capability matrix #24 解锁面——query_rate_limiter 三态）：
//   GET /v1/system/query_rate_limiter/config（disabled 态 = 400 verbatim →
//   三态判定：200 = 生效中、400 = disabled；mode 不在 GET 回显——enabled/
//   simulation 不可区分，如实注记）；POST 合并写（mode 承载三态翻转 +
//   rlSettings 双桶数值编辑）；DELETE 恢复出厂（danger 确认）。
// 门：GET system:read（readonly 可读）；POST/DELETE system:write（readonly
// 禁用 + 注记）。验证：三值须为正整数（服务端终裁）。
// 锚族（新锚——日志登记诉求）：settings-page/settings-knobs/
// settings-knob-<key>/settings-knobs-note/qrl-panel/qrl-state/qrl-mode-
// <m>/qrl-row-<type>/qrl-input-<type>-<field>/qrl-save/qrl-reset/
// qrl-readonly-note。
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { AlertBox } from '@/components/layout/bits'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { TextInput } from '@/components/layout/fields'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, apiJSON, apiText, canAdminWrite, errText, isReadOnlyAdmin } from '@/lib/api'
import { formatCount } from '@/lib/format'
import { tr } from '@/i18n'

const t = tr('monitoring')

/** GET /v1/system/settings 响应（internal/httpapi system_settings.go 契约） */
interface SystemSettings {
  folder_download: {
    enabled: boolean
    enabled_for_anonymous: boolean
    max_download_size_mb: number
    max_files: number
    max_concurrent_requests: number
    enabled_empty_directories: boolean
  }
  trashcan: {
    retention_days: number
  }
}

/** QRL 桶 wire 形（internal/search QRLSetting） */
interface QRLSetting {
  rlType: string
  permitsPerTimeFrame: number
  timeFrameMillis: number
  timeQuota: number
}

const QRL_ENDPOINT = '/v1/system/query_rate_limiter/config'

/** 出厂双桶（internal/search QRLDefaultSettings——K63 三元组同值映射）：
 *  disabled 态 GET 恒 400 无回显，兜底渲染桶行供「启用后即改」 */
const FACTORY_SETTINGS: QRLSetting[] = [
  { rlType: 'DEFAULT', permitsPerTimeFrame: 4, timeFrameMillis: 10000, timeQuota: 1000 },
  { rlType: 'LOW_PRIORITY', permitsPerTimeFrame: 4, timeFrameMillis: 10000, timeQuota: 1000 },
]

/** 旋钮回显卡（只读——REST 面只有 GET；变更经实例 YAML + 重启） */
function KnobsCard() {
  const q = useQuery({
    queryKey: ['settings', 'knobs'],
    queryFn: () => apiJSON<SystemSettings>('/v1/system/settings'),
  })

  if (q.isPending) {
    return (
      <section className="card section" data-testid="settings-knobs">
        <StateSkeleton lines={5} />
      </section>
    )
  }
  if (q.isError) {
    const error = q.error instanceof ApiError ? q.error : new ApiError(0, errText(q.error))
    if (error.status === 403) {
      return (
        <section className="card section" data-testid="settings-knobs">
          <EmptyState
            message={t('无权限查看运行时旋钮')}
            hint={t('GET /api/v1/system/settings 为管理员视图（system:read——admin / readonly_admin）。')}
          />
        </section>
      )
    }
    return (
      <section className="card section" data-testid="settings-knobs">
        <ErrorCard error={error} onRetry={() => void q.refetch()} />
      </section>
    )
  }

  const fd = q.data.folder_download
  const rows: { k: string; label: string; v: string | boolean }[] = [
    { k: 'folder_download.enabled', label: t('目录归档下载（folder_download.enabled）'), v: fd.enabled },
    { k: 'folder_download.enabled_for_anonymous', label: t('匿名可用（folder_download.enabled_for_anonymous）'), v: fd.enabled_for_anonymous },
    { k: 'folder_download.max_download_size_mb', label: t('单次上限 MB（max_download_size_mb）'), v: formatCount(fd.max_download_size_mb) },
    { k: 'folder_download.max_files', label: t('单次文件数上限（max_files）'), v: formatCount(fd.max_files) },
    { k: 'folder_download.max_concurrent_requests', label: t('并发上限（max_concurrent_requests）'), v: formatCount(fd.max_concurrent_requests) },
    { k: 'folder_download.enabled_empty_directories', label: t('含空目录（enabled_empty_directories）'), v: fd.enabled_empty_directories },
    { k: 'trashcan.retention_days', label: t('回收站保留天数（trashcan.retention_days）'), v: formatCount(q.data.trashcan.retention_days) },
  ]

  return (
    <section className="card section" data-testid="settings-knobs">
      <h3>{t('运行时旋钮（回显）')}</h3>
      <p className="field-hint">
        {t('GET /api/v1/system/settings 回显解析配置（YAML + env + 缺省）——与执行行为同源，不可能与实况相左。这些是文件级 restart-effective 旋钮：REST 面只读（写端点不设），变更经实例 YAML 后重启生效。')}
      </p>
      {rows.map((r) => (
        <div className="kv" key={r.k} data-testid={`settings-knob-${r.k}`}>
          <span className="k">{r.label}</span>
          <span>
            {typeof r.v === 'boolean' ? (
              r.v ? <span className="badge success">{t('开')}</span> : <span className="badge neutral">{t('关')}</span>
            ) : (
              <span className="font-mono" lang="en">{r.v}</span>
            )}
          </span>
        </div>
      ))}
      <p className="field-hint" data-testid="settings-knobs-note" style={{ marginBottom: 0 }}>
        {t('旋钮范围刻意克制：只回显运行时行为旋钮，永不携带密钥 / DSN / 文件路径（诚实、最小、只读的加锁面姿态）。')}
      </p>
    </section>
  )
}

/** QRL 面板（三态：disabled（GET 400 verbatim）/ 生效中（GET 200——mode
 *  不回显，enabled/simulation 不可区分，如实注记）；POST mode 承载翻转 +
 *  双桶数值编辑；DELETE 恢复出厂） */
function QrlPanel() {
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const confirm = useConfirm()
  const queryClient = useQueryClient()

  const [mode, setMode] = useState<'disabled' | 'enabled' | 'simulation'>('enabled')
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [writeError, setWriteError] = useState<string | null>(null)

  const q = useQuery({
    queryKey: ['qrl', 'config'],
    queryFn: async () => {
      try {
        const body = await apiJSON<{ rlSettings: QRLSetting[] }>(QRL_ENDPOINT)
        return { state: 'active' as const, settings: body.rlSettings ?? [] }
      } catch (err) {
        // disabled 态的 400 verbatim：本面据此判定三态（GET 不回显 mode）。
        // 桶行以出厂默认值兜底渲染（internal/search QRLDefaultSettings 的
        // K63 三元组同值——启用后可直接编辑，无需先启用再读）
        if (err instanceof ApiError && err.status === 400 && err.message.includes('Query rate limiter is disabled')) {
          return { state: 'disabled' as const, settings: FACTORY_SETTINGS }
        }
        throw err
      }
    },
  })

  const settings = q.data?.settings ?? []
  // GET 到达即种入草稿（数值编辑面）
  const activeData = q.data?.state === 'active' ? q.data : null
  useEffect(() => {
    if (!activeData) return
    setDrafts((prev) => {
      const next = { ...prev }
      for (const s of activeData.settings) {
        if (next[`${s.rlType}.permitsPerTimeFrame`] === undefined) next[`${s.rlType}.permitsPerTimeFrame`] = String(s.permitsPerTimeFrame)
        if (next[`${s.rlType}.timeFrameMillis`] === undefined) next[`${s.rlType}.timeFrameMillis`] = String(s.timeFrameMillis)
        if (next[`${s.rlType}.timeQuota`] === undefined) next[`${s.rlType}.timeQuota`] = String(s.timeQuota)
      }
      return next
    })
  }, [activeData])

  const reload = () => {
    setDrafts({})
    void queryClient.refetchQueries({ queryKey: ['qrl', 'config'] })
  }

  const draftOf = (s: QRLSetting, field: 'permitsPerTimeFrame' | 'timeFrameMillis' | 'timeQuota') =>
    drafts[`${s.rlType}.${field}`] ?? String(s[field])
  const dirty =
    q.data?.state === 'active' &&
    settings.some((s) =>
      (['permitsPerTimeFrame', 'timeFrameMillis', 'timeQuota'] as const).some(
        (f) => draftOf(s, f) !== String(s[f]),
      ),
    )

  const save = async () => {
    setSaving(true)
    setWriteError(null)
    try {
      const rlSettings = settings.map((s) => ({
        rlType: s.rlType,
        permitsPerTimeFrame: Number(draftOf(s, 'permitsPerTimeFrame')),
        timeFrameMillis: Number(draftOf(s, 'timeFrameMillis')),
        timeQuota: Number(draftOf(s, 'timeQuota')),
      }))
      const text = await apiText(QRL_ENDPOINT, {
        method: 'POST',
        body: { mode, rlSettings },
      })
      toast.success(text) // "Query rate limiter configuration was updated successfully"
      reload()
    } catch (err) {
      // 400（invalid setting——非正整数 / 桶型闭集）服务端原文行内
      setWriteError(errText(err))
    } finally {
      setSaving(false)
    }
  }

  /** 只翻 mode 不动桶值（merge 语义——未携带的半保持） */
  const flipMode = async (next: 'disabled' | 'enabled' | 'simulation') => {
    const ok = await confirm.confirm({
      title: next === 'disabled' ? t('停用查询限流器') : t('切换限流器状态（{v1}）', { v1: next }),
      description:
        next === 'disabled'
          ? t('停用后 GET 面恒答 400（"Query rate limiter is disabled"）；AQL 查询不再延迟节流（K63 门不受影响——限流器只做延迟，从不拒绝）。进程生命周期态：重启后回到出厂（disabled）。')
          : t('mode 经 POST 体的 BinFlow 承载位写入（disabled / enabled / simulation——进程生命周期态，重启回出厂 disabled）。桶数值不变（合并语义）。'),
      confirmLabel: t('切换'),
      danger: next === 'disabled',
    })
    if (!ok) return
    setSaving(true)
    setWriteError(null)
    try {
      const text = await apiText(QRL_ENDPOINT, { method: 'POST', body: { mode: next } })
      toast.success(text)
      setMode(next === 'disabled' ? 'disabled' : next)
      reload()
    } catch (err) {
      setWriteError(errText(err))
    } finally {
      setSaving(false)
    }
  }

  const reset = async () => {
    const ok = await confirm.confirm({
      title: t('恢复出厂限流配置'),
      description: t('删除自定义配置并回到出厂桶值（DEFAULT / LOW_PRIORITY 双桶同 K63 三元组映射）。进程生命周期态：重启后同样回到出厂。'),
      confirmLabel: t('恢复出厂'),
      danger: true,
    })
    if (!ok) return
    setSaving(true)
    setWriteError(null)
    try {
      const text = await apiText(QRL_ENDPOINT, { method: 'DELETE' })
      toast.success(text) // "Query rate limiter configuration was deleted successfully"
      reload()
    } catch (err) {
      setWriteError(errText(err))
    } finally {
      setSaving(false)
    }
  }

  const stateLabel =
    q.data?.state === 'disabled'
      ? { label: t('已停用（disabled）'), dot: 'warn' }
      : q.data?.state === 'active'
        ? { label: t('生效中（enabled / simulation——GET 不回显 mode，不可区分）'), dot: 'ok' }
        : { label: t('…'), dot: 'warn' }

  return (
    <section className="card section" data-testid="qrl-panel">
      <h3>{t('查询限流器（Query Rate Limiter）')}</h3>
      <p className="field-hint">
        {t('AQL 查询的延迟节流面（aql.md §14.4）：只延迟、从不拒绝——K63 门的 429/408 行为在任何状态下不变。双桶 DEFAULT / LOW_PRIORITY 各三值（每时间窗许可数 / 窗长毫秒 / 时间配额），须为正整数。')}
      </p>

      {q.isPending && <StateSkeleton lines={4} />}
      {q.isError && <ErrorCard error={q.error instanceof ApiError ? q.error : new ApiError(0, errText(q.error))} onRetry={reload} />}

      {q.data && (
        <>
          <div className="kv">
            <span className="k">{t('状态')}</span>
            <span data-testid="qrl-state">
              <span className={`status-dot ${stateLabel.dot}`} aria-hidden="true" /> {stateLabel.label}
            </span>
          </div>

          {(q.data.state === 'active' || q.data.state === 'disabled') && (
            <table className="mt-2 w-full text-dense" data-testid="qrl-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-3 py-2 font-medium">rlType</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('许可数 / 窗')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('窗长（毫秒）')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('时间配额')}</th>
                </tr>
              </thead>
              <tbody>
                {settings.map((s) => (
                  <tr key={s.rlType} data-testid={`qrl-row-${s.rlType}`} className="border-b border-border/60">
                    <td className="px-3 py-1.5 font-mono" lang="en">{s.rlType}</td>
                    {(['permitsPerTimeFrame', 'timeFrameMillis', 'timeQuota'] as const).map((f) => (
                      <td key={f} className="px-3 py-1.5">
                        <TextInput
                          mono
                          lang="en"
                          inputMode="numeric"
                          disabled={!adminWrite || saving || readOnly}
                          value={draftOf(s, f)}
                          onChange={(e) => setDrafts((d) => ({ ...d, [`${s.rlType}.${f}`]: e.target.value }))}
                          className="w-[140px]"
                          aria-label={`${s.rlType} ${f}`}
                          data-testid={`qrl-input-${s.rlType}-${f}`}
                        />
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {writeError && (
            <AlertBox severity="error" className="mt-2">
              <span className="break-all font-mono text-aux" lang="en">{writeError}</span>
            </AlertBox>
          )}

          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              disabled={!adminWrite || saving || readOnly || !dirty}
              onClick={() => void save()}
              data-testid="qrl-save"
              title={readOnly ? t('只读管理员：限流写是 system:write（服务端 403 兜底）') : undefined}
            >
              {saving ? t('保存中…') : t('保存限流配置')}
            </Button>
            {q.data.state === 'active' ? (
              <Button
                variant="outline"
                size="sm"
                className="border-destructive/50 text-destructive hover:bg-destructive/10"
                disabled={!adminWrite || saving || readOnly}
                onClick={() => void flipMode('disabled')}
                data-testid="qrl-mode-disabled"
              >
                {t('停用限流器')}
              </Button>
            ) : (
              <Button
                variant="outline"
                size="sm"
                disabled={!adminWrite || saving || readOnly}
                onClick={() => void flipMode('enabled')}
                data-testid="qrl-mode-enabled"
              >
                {t('启用限流器')}
              </Button>
            )}
            <Button
              variant="outline"
              size="sm"
              disabled={!adminWrite || saving || readOnly}
              onClick={() => void reset()}
              data-testid="qrl-reset"
            >
              {t('恢复出厂')}
            </Button>
            {readOnly && (
              <span className="text-aux text-muted-foreground" data-testid="qrl-readonly-note">
                {t('只读管理员：限流器读面可见，写面已禁用（system:write，服务端 403 兜底）。')}
              </span>
            )}
          </div>
          <p className="field-hint" style={{ marginBottom: 0 }}>
            {t('mode 是 BinFlow 的 POST 承载位（disabled / enabled / simulation——Artifactory 走 system.properties，BinFlow 无 properties 面）；文档为进程生命周期态，重启回出厂（DB 持久化是已登记缺口）。')}
          </p>
        </>
      )}
    </section>
  )
}

export default function SettingsPage() {
  return (
    <div data-testid="settings-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('设置')}</h2>
        <span className="text-aux text-2">{t('运行时旋钮回显与查询限流器管理')}</span>
      </div>
      <KnobsCard />
      <QrlPanel />
    </div>
  )
}
