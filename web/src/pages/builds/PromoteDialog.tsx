// Build promote 对话框（P3 解锁面——POST /api/build/promote/{name}/{number}
// 的控制台承载；契约 = internal/build PromotionRequest + build-info.md §2.4）。
// 两臂：targetRepo 空 = status-only（只翻状态免 w 半）；非空 = 跨仓迁移
// （目标必须 local；copy=false = MOVE 缺省）。dryRun 预演先行（零副作用、
// 无历史行）；failFast 缺省 true（首个悬空关联即拒整单）。messages[] 原文
// 流呈现（error|warning|info 语义色——部分失败乘 200 的官方形态）。
// 锚族（新锚——日志登记诉求）：build-promote-dialog/build-promote-status/
// build-promote-comment/build-promote-target/build-promote-dryrun/
// build-promote-copy/build-promote-failfast/build-promote-run/
// build-promote-cancel/build-promote-submit/build-promote-result。
import { useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertBox, CheckRow } from '@/components/layout/bits'
import { TextInput, NativeSelect } from '@/components/layout/fields'
import { toast } from '@/lib/toast'
import { ApiError, errText, getRepositories } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'
import { promoteBuild } from './api'
import type { PromotionMessage } from './api'

const t = tr('builds')

interface PromoteState {
  status: string
  comment: string
  ciUser: string
  targetRepo: string
  copy: boolean
  dryRun: boolean
  failFast: boolean
}

const INITIAL: PromoteState = {
  status: '',
  comment: '',
  ciUser: '',
  targetRepo: '',
  copy: false,
  dryRun: true,
  failFast: true,
}

export default function PromoteDialog({
  name,
  number,
  started,
  onClose,
  onDone,
}: {
  name: string
  number: string
  started?: string
  onClose: () => void
  onDone: () => void
}) {
  // 候选目标仓（local only——服务端终裁，列表只驱动下拉）
  const repos = useAsync(getRepositories, [])
  const localRepos = useMemo(
    () => (repos.data ?? []).filter((r) => r.type === 'local').map((r) => r.key).sort(),
    [repos.data],
  )

  const [f, setF] = useState<PromoteState>(INITIAL)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const [result, setResult] = useState<{ dryRun: boolean; messages: PromotionMessage[] } | null>(null)

  const statusOnly = f.targetRepo === ''
  const canSubmit = !busy && (statusOnly ? f.status.trim() !== '' : f.status.trim() !== '')

  const run = async (dryRun: boolean) => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const res = await promoteBuild(
        name,
        number,
        {
          status: f.status.trim(),
          comment: f.comment,
          ciUser: f.ciUser,
          timestamp: '',
          dryRun,
          sourceRepo: '',
          targetRepo: f.targetRepo.trim(),
          copy: f.copy,
          artifacts: null,
          dependencies: false,
          scopes: [],
          properties: {},
          failFast: f.failFast,
        },
        { started },
      )
      if (dryRun) {
        setResult({ dryRun: true, messages: res.messages ?? [] })
        toast.success(t('预演完成（零副作用）——{v1} 条消息', { v1: (res.messages ?? []).length }))
      } else {
        setResult({ dryRun: false, messages: res.messages ?? [] })
        const errs = (res.messages ?? []).filter((m) => m.level === 'error')
        if (errs.length > 0) {
          toast.error(t('晋升完成但有 {v1} 条 error 消息', { v1: errs.length }))
        } else {
          toast.success(t('build {name}#{number} 已晋升（{v1} 条消息）', { name, number, v1: (res.messages ?? []).length }))
        }
        onDone()
      }
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-[560px]" data-testid="build-promote-dialog">
        <DialogHeader>
          <DialogTitle>
            {t('晋升 build ·')} <span className="font-mono" lang="en">{name}#{number}</span>
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="field">
            <label htmlFor="bp-status">{t('目标状态（status *）')}</label>
            <TextInput
              id="bp-status"
              mono
              lang="en"
              value={f.status}
              onChange={(e) => setF((p) => ({ ...p, status: e.target.value }))}
              placeholder="released"
              data-testid="build-promote-status"
            />
            <p className="field-hint">{t('追加一行 promotion 历史（现势 = 最新行，永不改写旧行）。')}</p>
          </div>
          <div className="field">
            <label htmlFor="bp-comment">comment</label>
            <TextInput
              id="bp-comment"
              value={f.comment}
              onChange={(e) => setF((p) => ({ ...p, comment: e.target.value }))}
              placeholder={t('（可选——本次晋升说明）')}
              data-testid="build-promote-comment"
            />
          </div>
          <div className="field">
            <label htmlFor="bp-ciuser">ciUser</label>
            <TextInput
              id="bp-ciuser"
              mono
              lang="en"
              value={f.ciUser}
              onChange={(e) => setF((p) => ({ ...p, ciUser: e.target.value }))}
              placeholder={t('（可选——触发晋升的 CI 用户）')}
              data-testid="build-promote-ciuser"
            />
          </div>
          <div className="field">
            <label htmlFor="bp-target">{t('目标仓（targetRepo——留空 = status-only：只翻状态不迁制品）')}</label>
            <NativeSelect
              id="bp-target"
              value={f.targetRepo}
              onChange={(e) => setF((p) => ({ ...p, targetRepo: e.target.value }))}
              options={[
                { value: '', label: t('（status-only 臂）') },
                ...localRepos.map((r) => ({ value: r, label: r })),
              ]}
              data-testid="build-promote-target"
            />
            {!statusOnly && (
              <p className="field-hint">{t('非空 = 跨仓迁移：关联制品从 build 仓迁到目标仓（目标必须 local）；关联行逐一过源 r / 目标 w / 配额 / pattern 门。')}</p>
            )}
          </div>
          {!statusOnly && (
            <CheckRow
              checked={f.copy}
              onChange={(next) => setF((p) => ({ ...p, copy: next }))}
              label={t('copy=true 保留源（缺省 false = MOVE——generic 走 move 臂，docker 清单级联清源）')}
              testid="build-promote-copy"
            />
          )}
          <CheckRow
            checked={f.failFast}
            onChange={(next) => setF((p) => ({ ...p, failFast: next }))}
            label={t('failFast（缺省开：首个悬空关联即拒整单；关 = 跳过该行出 warning 继续）')}
            testid="build-promote-failfast"
          />
          {error && (
            <AlertBox severity="error">
              <div className="font-medium">{t('晋升被拒（HTTP')} {error.status || t('网络')}{t('）')}</div>
              <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{error.message}</div>
            </AlertBox>
          )}
          {result && (
            <div data-testid="build-promote-result">
              <p className="field-hint mb-1">
                {result.dryRun
                  ? t('预演消息（零副作用——无迁移、无历史行、无审计）：')
                  : t('晋升消息（messages[] 原文）：')}
              </p>
              <div className="max-h-48 overflow-y-auto rounded-md border border-border p-2">
                {result.messages.length === 0 ? (
                  <p className="text-2">{t('（无消息——全部通过）')}</p>
                ) : (
                  result.messages.map((m, i) => (
                    <p
                      key={i}
                      className={`text-dense ${m.level === 'error' ? 'text-destructive' : m.level === 'warning' ? 'text-warning' : 'text-2'}`}
                      lang="en"
                    >
                      <b className="font-mono">[{m.level}]</b> {m.message}
                    </p>
                  ))
                )}
              </div>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => void run(true)} disabled={busy || !canSubmit} data-testid="build-promote-run">
            {busy ? t('执行中…') : t('预演（dryRun）')}
          </Button>
          <span className="flex-1" />
          <Button variant="outline" onClick={onClose} data-testid="build-promote-cancel">{t('取消')}</Button>
          <Button disabled={busy || !canSubmit} onClick={() => void run(false)} data-testid="build-promote-submit">
            {busy ? t('晋升中…') : t('执行晋升')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
