// 备份 / 恢复（T-102 AC③ → T-462 / FR-145.7 翻正承载——P3 新栈重写）。
// 两卡：
// ① 定时备份（New Backup / 列表 / cron / next-run）：GET/PUT/DELETE
//   /api/v1/system/backups。表单四字段（Enabled / Backup Key / Cron
//   Expression / Next Backup Time / Server Path）；Artifactory 其余字段
//   （仓子集 / incremental / retention 轮转 / zip / 邮件告警）缺位不伪造。
// ② 导入 / 导出（CLI）：ADR-0015 勘误②边界维持——/api/export/** 404 有意
//   不兼容、import CLI-only；一次性 export 也走 CLI。
// 门：GET system:read；PUT/DELETE system:write——readonly_admin 表单与
// 操作禁用 + 注记。400 族行内原样呈现。
// 锚族原样：backup-page/backup-crud/backup-denied/backup-empty/
// backup-table/backup-row-<key>/backup-edit-<key>/backup-delete-<key>/
// backup-delete-confirm-key/backup-new/backup-form(-key(-error)?|-cron|
// -next|-path|-enabled|-gap|-error|-cancel|-save)/backup-readonly-note/
// backup-cli。
import { useState } from 'react'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { AlertBox, Badge, CheckRow } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { TextInput } from '@/components/layout/fields'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { canAdminWrite, errText, isReadOnlyAdmin } from '@/lib/api'
import {
  deleteBackup,
  listBackups,
  localInputToRFC3339,
  putBackup,
  validateBackupKey,
  validateExportPath,
} from '@/lib/governance'
import type { BackupConfig } from '@/lib/governance'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'

const t = tr('governance')

interface CmdBlock {
  title: string
  text: string
  note?: string
}

const BLOCKS: CmdBlock[] = [
  {
    title: t('导出（在线 export）'),
    text: [
      t('# 在线执行：持有 data 目录维护锁（与 GC 互斥），SQLite 快照先行，blobs 拷贝保 mtime'),
      'binflow-server export -c /etc/binflow/config.yaml \\',
      '  --output /backup/binflow-$(date +%F)',
    ].join('\n'),
    note: t('产物目录含口令哈希与 enc:v1: 密文——权限 0700 保管（NFR-S22）；--tar 打包为 P2 债务。'),
  },
  {
    title: t('恢复（停机 import）'),
    text: [
      t('# 一律停机执行；目标 data dir 必须为空（非空 fail-fast，无半恢复）'),
      'binflow-server import -c /etc/binflow/config.yaml \\',
      '  --input /backup/binflow-2026-08-21 --verify full',
    ].join('\n'),
    note: t('--verify spot（默认，size 全验 + sha256 抽验前 100）/ full（全量重哈希）；恢复后 web_sessions 不回带（需重登），remote 凭据依赖 BINFLOW_REMOTE_CREDENTIALS_KEY（ADR-0012 恢复链）。'),
  },
]

/** RFC3339 UTC → 可读（与 GC/ServiceStatus 页同形） */
function fmtUTC(v: string): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

/** 表单草稿（文本态；nextBackupTime 为 datetime-local 本地值） */
interface BackupFormState {
  key: string
  cron: string
  next: string
  path: string
  enabled: boolean
}

const CREATE_FORM: BackupFormState = { key: '', cron: '', next: '', path: '', enabled: true }

function editForm(b: BackupConfig): BackupFormState {
  // nextBackupTime 不回填已算出的 next-run——该可写位是「首跑时刻」而非
  // 只读回显；服务端语义：未给 = 表达式推算
  return { key: b.backupKey, cron: b.cronExp, next: '', path: b.exportPath, enabled: b.enabled }
}

export default function BackupPage() {
  return (
    <div data-testid="backup-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('备份 / 恢复')}</h2>
      </div>
      <BackupCrudCard />
      <ImportExportCard />
    </div>
  )
}

// ---------------------------------------------------------------------------
// ① 定时备份 CRUD（New Backup / 列表 / 编辑 / 删除）
// ---------------------------------------------------------------------------

function BackupCrudCard() {
  const { session } = useAuth()
  const confirm = useConfirm()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const list = useAsync(listBackups, [])

  const [editor, setEditor] = useState<{ base: BackupConfig | null; form: BackupFormState } | null>(null)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [busyKey, setBusyKey] = useState('')

  const backups = list.data?.backups ?? []

  const startCreate = () => {
    setFormError(null)
    setEditor({ base: null, form: { ...CREATE_FORM } })
  }
  const startEdit = (b: BackupConfig) => {
    setFormError(null)
    setEditor({ base: b, form: editForm(b) })
  }

  const keyErr = editor && !editor.base ? validateBackupKey(editor.form.key.trim()) : null
  const pathErr = editor ? validateExportPath(editor.form.path.trim()) : null
  const nextRfc =
    editor && editor.form.next !== '' ? localInputToRFC3339(editor.form.next) : ''
  const nextInvalid = editor !== null && editor.form.next !== '' && nextRfc === null
  const canSave =
    editor !== null && !keyErr && !pathErr && !nextInvalid && !saving && adminWrite

  const doSave = async (): Promise<void> => {
    if (!editor || !canSave) return
    const { form } = editor
    setFormError(null)
    setSaving(true)
    try {
      const saved = await putBackup({
        backupKey: form.key.trim(),
        enabled: form.enabled,
        cronExp: form.cron.trim(),
        ...(nextRfc !== '' && nextRfc !== null ? { nextBackupTime: nextRfc } : {}),
        exportPath: form.path.trim(),
      })
      toast.success(
        saved.cronExp
          ? t('备份 {v1} 已保存（下次：{v2}）', { v1: saved.backupKey, v2: saved.nextScheduleBackup || t('表达式推算') })
          : t('备份 {v1} 已保存（未调度——表达式为空）', { v1: saved.backupKey }),
      )
      setEditor(null)
      list.reload()
    } catch (err) {
      // 400 族（Invalid cronExp / nextBackupTime 过去时 / exportPath 形态 /
      // key 形态）文案原样行内——服务端是唯一校验权威
      setFormError(errText(err))
    } finally {
      setSaving(false)
    }
  }

  const doDelete = async (b: BackupConfig): Promise<void> => {
    const typed = await confirm.prompt({
      title: t('删除备份配置'),
      description: (
        <p>
          {t('将删除备份配置')} <b className="font-mono" lang="en">{b.backupKey}</b>{t('（定时与 payload 配置一并移除，调度即刻停止）。已写出的历史备份目录不受影响； 此操作没有撤销。')}
          <br />{t('输入备份 key')} <b className="font-mono" lang="en">{b.backupKey}</b> {t('以确认。')}
        </p>
      ),
      placeholder: b.backupKey,
      mono: true,
      danger: true,
      confirmLabel: t('删除配置'),
      cancelLabel: t('取消'),
      anchor: 'backup-delete-confirm-key',
      validate: (v) => (v === b.backupKey ? null : t('输入与备份 key 不一致')),
    })
    if (typed === null || typed !== b.backupKey) return
    setBusyKey(b.backupKey)
    try {
      await deleteBackup(b.backupKey)
      toast.success(t('备份配置 {v1} 已删除', { v1: b.backupKey }))
      if (editor?.base?.backupKey === b.backupKey) setEditor(null)
      list.reload()
    } catch (err) {
      toast.error(t('删除失败：{v1}', { v1: errText(err) }))
    } finally {
      setBusyKey('')
    }
  }

  return (
    <section className="card section" data-testid="backup-crud">
      <h3 className="mb-0.5 text-dense font-semibold">{t('定时备份')}</h3>
      <p className="mb-2 text-dense text-2">
        {t('cron 到点由服务端执行全实例导出（与 CLI export 同载体，产物落')}{' '}
        <span className="font-mono" lang="en">{t('&lt;server path&gt;/&lt;key&gt;-&lt;时间戳&gt;')}</span>{t('）； 与 GC / 手动 export 共用 data 目录维护锁。')}
      </p>

      {list.status === 'loading' && <StateSkeleton lines={4} />}
      {list.status === 'error' && list.error && <ErrorCard error={list.error} onRetry={list.reload} />}
      {list.status === 'forbidden' && list.error && (
        <p className="field-hint" data-testid="backup-denied">{t('无权限读取备份配置（GET /api/v1/system/backups 需 system:read）。')}</p>
      )}

      {list.status === 'ok' && backups.length === 0 && !editor && (
        <div data-testid="backup-empty">
          <EmptyState
            message={t('未配置定时备份')}
            hint={t('新建一条备份配置（key + cron + 服务器路径）后，服务端按点到点导出全实例快照；一次性导出 / 恢复走下方 CLI。')}
            action={
              adminWrite ? (
                <Button size="sm" onClick={startCreate} data-testid="backup-new">{t('＋ New Backup')}</Button>
              ) : undefined
            }
          />
        </div>
      )}

      {list.status === 'ok' && backups.length > 0 && (
        <>
          <div className="overflow-x-auto">
            <table className="w-full text-dense" data-testid="backup-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-3 py-2 font-medium">Key</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('cron 表达式')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('下次备份')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('启用')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('上次运行 / 结果')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('导出路径')}</th>
                  {adminWrite && <th scope="col" className="px-3 py-2 text-right font-medium">{t('操作')}</th>}
                </tr>
              </thead>
              <tbody>
                {backups.map((b) => (
                  <tr key={b.backupKey} data-testid={`backup-row-${b.backupKey}`} className="border-b border-border/60 hover:bg-accent">
                    <td className="px-3 py-1.5 font-mono" lang="en">{b.backupKey}</td>
                    <td className="px-3 py-1.5 font-mono" lang="en">{b.cronExp || <span className="text-muted-foreground">{t('未调度')}</span>}</td>
                    <td className="px-3 py-1.5">
                      {b.cronExp === '' ? (
                        <span className="text-muted-foreground">—</span>
                      ) : b.enabled ? (
                        <span className="font-mono" lang="en" title={b.nextScheduleBackup}>
                          {fmtUTC(b.nextScheduleBackup)}
                        </span>
                      ) : (
                        <Badge>{t('已停用')}</Badge>
                      )}
                    </td>
                    <td className="px-3 py-1.5">
                      {b.enabled ? (
                        <span className="status-pill status-on">{t('启用')}</span>
                      ) : (
                        <span className="status-pill status-off">{t('停用')}</span>
                      )}
                    </td>
                    <td className="px-3 py-1.5">
                      {b.lastRun ? (
                        <span className="font-mono" lang="en" title={b.lastError || undefined}>
                          {fmtUTC(b.lastRun)}
                          {b.lastStatus ? t('（{v1}{v2}）', { v1: b.lastStatus, v2: b.lastStatus !== 'ok' && b.lastError ? t('：{v1}', { v1: b.lastError }) : '' }) : ''}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">{t('未运行')}</span>
                      )}
                    </td>
                    <td className="max-w-[220px] break-all px-3 py-1.5 font-mono" lang="en">
                      {b.exportPath} <CopyButton value={b.exportPath} label={t('导出路径 {v1}', { v1: b.backupKey })} />
                    </td>
                    {adminWrite && (
                      <td className="whitespace-nowrap px-3 py-1.5 text-right">
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7"
                          disabled={busyKey !== ''}
                          onClick={() => startEdit(b)}
                          data-testid={`backup-edit-${b.backupKey}`}
                        >
                          {t('编辑')}
                        </Button>{' '}
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 text-muted-foreground"
                          disabled={busyKey !== ''}
                          onClick={() => void doDelete(b)}
                          data-testid={`backup-delete-${b.backupKey}`}
                          aria-label={t('删除备份配置 {v1}', { v1: b.backupKey })}
                        >
                          {t('删除')}
                        </Button>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {adminWrite && !editor && (
            <Button variant="outline" size="sm" className="mt-2" onClick={startCreate} data-testid="backup-new">
              {t('＋ New Backup')}
            </Button>
          )}
        </>
      )}

      {readOnly && (
        <p className="admin-note" data-testid="backup-readonly-note">{t('只读管理员（readonly_admin）：备份配置读写为 system:write 面，编辑入口不呈现—— 服务端 403 兜底。')}</p>
      )}

      {editor && (
        <div className="repl-form" data-testid="backup-form">
          <h4 className="mt-4 mb-2 text-dense font-semibold">
            {editor.base ? t('编辑备份 {v1}', { v1: editor.base.backupKey }) : 'New Backup'}
          </h4>

          {!editor.base && (
            <div className="field">
              <label htmlFor="backup-key">Backup Key *</label>
              <TextInput
                id="backup-key"
                mono
                lang="en"
                value={editor.form.key}
                aria-invalid={!!keyErr || undefined}
                disabled={saving}
                onChange={(e) => setEditor({ ...editor, form: { ...editor.form, key: e.target.value } })}
                placeholder="nightly-full"
                className="w-[300px]"
                data-testid="backup-form-key"
              />
              {keyErr ? (
                <p className="field-error" data-testid="backup-form-key-error" role="alert">{keyErr}</p>
              ) : (
                <p className="field-hint">{t('1~64 字符，字母/数字/./_/-，首字符字母数字（与复制配置名同规则）。')}</p>
              )}
            </div>
          )}
          {editor.base && (
            <div className="kv">
              <span className="k">Backup Key</span>
              <span className="font-mono" lang="en">{editor.base.backupKey}</span>
            </div>
          )}

          <div className="field">
            <label htmlFor="backup-cron">{t('Cron Expression（Quartz 六/七域；留空 = 保存但不调度）')}</label>
            <TextInput
              id="backup-cron"
              mono
              lang="en"
              value={editor.form.cron}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...editor.form, cron: e.target.value } })}
              placeholder="0 0 2 ? * MON-FRI"
              className="w-[300px]"
              data-testid="backup-form-cron"
            />
            <p className="field-hint">
              {t('表达式合法性由服务端校验（')}<span className="font-mono" lang="en">Invalid cronExp …</span> {t('点名原因）； 例：')}<span className="font-mono" lang="en">0 0 2 ? * MON-FRI</span>{t('（工作日 2:00）/')} <span className="font-mono" lang="en">0 0 2 ? * SAT</span>{t('（周六 2:00）。')}
            </p>
          </div>

          <div className="field">
            <label htmlFor="backup-next">{t('Next Backup Time（可选——首跑时刻，须晚于当前）')}</label>
            <input
              id="backup-next"
              type="datetime-local"
              value={editor.form.next}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...editor.form, next: e.target.value } })}
              className="h-8 w-[260px] rounded-sm border border-input bg-surface-3 px-2 text-dense disabled:opacity-60"
              data-testid="backup-form-next"
            />
            <p className="field-hint">{t('留空 = 由表达式推算下次运行；显式时刻按浏览器本地时区转 UTC 提交（过去时刻被 400 拒绝）。')}</p>
          </div>

          <div className="field">
            <label htmlFor="backup-path">{t('Server Path For Backup *（服务器绝对路径）')}</label>
            <TextInput
              id="backup-path"
              mono
              lang="en"
              value={editor.form.path}
              aria-invalid={!!pathErr || undefined}
              disabled={saving}
              onChange={(e) => setEditor({ ...editor, form: { ...editor.form, path: e.target.value } })}
              placeholder="/backup/binflow"
              className="w-[380px]"
              data-testid="backup-form-path"
            />
            {pathErr ? (
              <p className="field-error" role="alert">{pathErr}</p>
            ) : (
              <p className="field-hint">
                {t('备份产物落地')} <span className="font-mono" lang="en">{t('&lt;path&gt;/&lt;key&gt;-&lt;时间戳&gt;')}</span> {t('子目录 （须绝对路径、不含 ..；与 data 目录的边界在 fire 时校验）。')}
              </p>
            )}
          </div>

          <CheckRow
            disabled={saving}
            checked={editor.form.enabled}
            onChange={(next) => setEditor({ ...editor, form: { ...editor.form, enabled: next } })}
            label={t('启用（Enabled）——停用 = 配置保留、不再调度')}
            testid="backup-form-enabled"
          />

          <p className="field-hint" data-testid="backup-form-gap">
            {t('Artifactory 表单的其余字段（仓子集〔BinFlow 导出恒为全实例快照〕/ 邮件告警 / Exclude New Repositories / Incremental / Retention / Zip 归档）在 BinFlow 无后端载体 ——缺位不伪造。')}
          </p>

          {formError && (
            <AlertBox severity="error" testid="backup-form-error" className="mt-4">
              <span lang={/^Invalid cronExp|^nextBackupTime|^exportPath|^backupKey/.test(formError) ? 'en' : undefined}>
                {formError}
              </span>
            </AlertBox>
          )}

          <div className="form-actions">
            <Button
              variant="outline"
              size="sm"
              disabled={saving}
              onClick={() => {
                setEditor(null)
                setFormError(null)
              }}
              data-testid="backup-form-cancel"
            >
              {t('取消')}
            </Button>
            <Button size="sm" disabled={!canSave} onClick={() => void doSave()} data-testid="backup-form-save">
              {saving ? t('保存中…') : editor.base ? t('保存') : t('创建备份')}
            </Button>
          </div>
        </div>
      )}
    </section>
  )
}

// ---------------------------------------------------------------------------
// ② 导入 / 导出（CLI）——ADR-0015 勘误②边界
// ---------------------------------------------------------------------------

function ImportExportCard() {
  return (
    <section className="card section" data-testid="backup-cli">
      <h3>{t('导入 / 导出（CLI）')}</h3>
      <p className="text-2">
        {t('浏览器面不提供交互式 export / import（')}<span className="font-mono" lang="en">/api/export/**</span> {t('维持 404—— ADR-0015 勘误②；import 停机高危，CLI-only）。上面的定时备份是服务端自动导出的配置面； 一次性导出与恢复走 CLI：')}
      </p>
      {BLOCKS.map((b) => (
        <div className="cmd-block" key={b.title}>
          <header>
            <span>{b.title}</span>
            <CopyButton value={b.text} label={b.title} />
          </header>
          {/* tabIndex：可滚动区键盘可达 */}
          <pre lang="en" tabIndex={0}>{b.text}</pre>
          {b.note && <div className="note">{b.note}</div>}
        </div>
      ))}
      <p className="field-hint mb-0">{t('完整手册（mtime 保管告警、恢复链、停机强一致可选项）见 docs/user 备份恢复篇； export.run / import.run / backup.schedule.set 记录经审计日志查询。')}</p>
    </section>
  )
}
