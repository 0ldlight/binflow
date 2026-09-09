// Build retention 对话框（P3 解锁面——POST /api/build/retention/{name} 的
// 控制台承载；契约 = internal/build RetentionRequest 四字段 + build-info.md
// §2.5）。窗口 = count（保留最近 N 个）∨ minimumBuildDate（保留此时刻后）∨
// 排除号列表；deleteBuildArtifacts 勾选连制品一并删（危险位）。async 缺省
// true：200 应答自「已验证计划」，执行后台跑（文案明示）；async=false 同步
// 执行（阶梯 403/404/400 同步应答）。
// 锚族（新锚——日志登记诉求）：build-retention-dialog/build-retention-count/
// build-retention-min-date/build-retention-keep/
// build-retention-delete-artifacts/build-retention-cancel/
// build-retention-submit。
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertBox, CheckRow } from '@/components/layout/bits'
import { TextInput } from '@/components/layout/fields'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, errText } from '@/lib/api'
import { localInputToRFC3339 } from '@/lib/governance'
import { tr } from '@/i18n'
import { setBuildRetention } from './api'

const t = tr('builds')

interface RetentionState {
  count: string
  minimumDate: string
  keep: string
  deleteArtifacts: boolean
}

const INITIAL: RetentionState = { count: '', minimumDate: '', keep: '', deleteArtifacts: false }

export default function RetentionDialog({
  name,
  onClose,
  onDone,
}: {
  name: string
  onClose: () => void
  onDone: () => void
}) {
  const confirm = useConfirm()
  const [f, setF] = useState<RetentionState>(INITIAL)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  const countNum = f.count.trim() === '' ? 0 : Number(f.count.trim())
  const countInvalid = f.count.trim() !== '' && (!/^\d+$/.test(f.count.trim()) || countNum < 0)
  const minDateRfc = f.minimumDate !== '' ? localInputToRFC3339(f.minimumDate) : ''
  const minDateInvalid = f.minimumDate !== '' && minDateRfc === null
  const keepList = f.keep.split(',').map((s) => s.trim()).filter((s) => s !== '')
  const windowEmpty = f.count.trim() === '' && f.minimumDate === ''
  const canSubmit = !busy && !countInvalid && !minDateInvalid && !windowEmpty

  const run = async () => {
    // 窗口外的 run 全删（制品可选连删）——确认承载影响面
    const ok = await confirm.confirm({
      title: t('设置保留策略 · {name}', { name }),
      description: f.deleteArtifacts
        ? t('窗口外的 run 连同其关联制品一并删除（deleteBuildArtifacts=true——制品删除不可撤销）。执行走后台（async——200 应答自已验证计划，后台失败记服务端日志不静默丢失）。')
        : t('窗口外的 run 记录被删除（build 信息与历史行；关联制品保留——deleteBuildArtifacts=false）。执行走后台（async——200 应答自已验证计划）。'),
      confirmLabel: t('设置并执行'),
      danger: f.deleteArtifacts,
    })
    if (!ok) return
    setBusy(true)
    setError(null)
    try {
      await setBuildRetention(name, {
        deleteBuildArtifacts: f.deleteArtifacts,
        count: countNum,
        minimumBuildDate: minDateRfc ?? '',
        buildNumbersNotToBeDiscarded: keepList,
      })
      toast.success(t('保留策略已设置（窗口执行中——后台 async）'))
      onDone()
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-[520px]" data-testid="build-retention-dialog">
        <DialogHeader>
          <DialogTitle>
            {t('保留策略（retention） ·')} <span className="font-mono" lang="en">{name}</span>
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <p className="field-hint">
            {t('名级窗口（POST /api/build/retention/{name}）：窗口外的 run 丢弃；两条件取并集保留（满足任一即留）。')}
          </p>
          <div className="field">
            <label htmlFor="br-count">{t('保留最近 N 个 run（count）')}</label>
            <TextInput
              id="br-count"
              mono
              lang="en"
              inputMode="numeric"
              value={f.count}
              onChange={(e) => setF((p) => ({ ...p, count: e.target.value }))}
              placeholder="10"
              aria-invalid={countInvalid || undefined}
              data-testid="build-retention-count"
            />
            {countInvalid && <p className="field-error">{t('count 需为非负整数')}</p>}
          </div>
          <div className="field">
            <label htmlFor="br-date">{t('保留此时刻之后的 run（minimumBuildDate）')}</label>
            <input
              id="br-date"
              type="datetime-local"
              value={f.minimumDate}
              onChange={(e) => setF((p) => ({ ...p, minimumDate: e.target.value }))}
              className="h-8 w-[260px] rounded-sm border border-input bg-surface-3 px-2 text-dense"
              data-testid="build-retention-min-date"
            />
            {minDateInvalid && <p className="field-error">{t('时间格式无法解析')}</p>}
            <p className="field-hint">{t('浏览器本地时区折算 UTC 提交；留空 = 不按时窗。')}</p>
          </div>
          <div className="field">
            <label htmlFor="br-keep">{t('永不丢弃的 run 号（逗号分隔）')}</label>
            <TextInput
              id="br-keep"
              mono
              lang="en"
              value={f.keep}
              onChange={(e) => setF((p) => ({ ...p, keep: e.target.value }))}
              placeholder="1, 42, 100"
              data-testid="build-retention-keep"
            />
          </div>
          <CheckRow
            checked={f.deleteArtifacts}
            onChange={(next) => setF((p) => ({ ...p, deleteArtifacts: next }))}
            label={t('连关联制品一并删除（deleteBuildArtifacts——危险：制品不可变，删除无撤销）')}
            testid="build-retention-delete-artifacts"
          />
          {windowEmpty && (
            <AlertBox severity="warning">{t('窗口全空 = 无保留条件（服务端会拒绝）——填 count 或 minimumBuildDate 至少一项。')}</AlertBox>
          )}
          {error && (
            <AlertBox severity="error">
              <div className="font-medium">{t('设置被拒（HTTP')} {error.status || t('网络')}{t('）')}</div>
              <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{error.message}</div>
            </AlertBox>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} data-testid="build-retention-cancel">{t('取消')}</Button>
          <Button variant="destructive" disabled={!canSubmit} onClick={() => void run()} data-testid="build-retention-submit">
            {busy ? t('设置中…') : t('设置并执行')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
