// Copy/Move 对话框（P2 解锁面——capability matrix #12：api/copy、api/move
// 的 UI 消费；Explorer 上下文菜单 + 多选批量动作的落点）。
//
// 契约（operations.go）：POST /api/{copy,move}/{src}/{path}?to=/{tgtRepo}[/{tgtPath}]
// - to = /<targetRepo>[/<targetPath>]（尾斜杠 = into-directory 语义）
// - dry=1 预演（messages[] 原文呈现）；failFast 批量语义
// - community 档 403 + license 头——服务端 message 原文行内（不吞不译）
import { useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useConfirm } from '@/app/providers'
import { toast } from 'sonner'
import { ApiError, errText } from '@/lib/api'
import { getRepositories } from '@/lib/api'
import { copyOrMove } from '@/features/artifacts/operations'
import type { CopyMoveMessage } from '@/features/artifacts/operations'
import type { ChildNode } from '@/pages/artifacts/lib'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'

const tt = tr('artifacts')

export function CopyMoveDialog({
  op,
  repoKey,
  nodes,
  onClose,
  onDone,
}: {
  op: 'copy' | 'move'
  repoKey: string
  nodes: ChildNode[]
  onClose: () => void
  onDone: () => void
}) {
  const { confirm } = useConfirm()
  const [targetRepo, setTargetRepo] = useState('')
  const [targetPath, setTargetPath] = useState('')
  const [dryResult, setDryResult] = useState<CopyMoveMessage[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // 目标仓候选：local 仓（copy/move 的目标须可写；remote/virtual 不列）
  const repos = useAsync(() => getRepositories(), [])
  const candidates = useMemo(
    () => (repos.data ?? []).filter((r) => r.type === 'local' && r.key !== repoKey),
    [repos.data, repoKey],
  )

  const single = nodes.length === 1 ? nodes[0] : null
  const to = `/${targetRepo.trim()}${targetPath.trim() ? `/${targetPath.trim().replace(/^\/+/, '')}` : ''}`
  const canRun = targetRepo.trim() !== '' && !busy

  const runOnce = async (dry: boolean) => {
    setBusy(true)
    setError(null)
    try {
      let all: CopyMoveMessage[] = []
      for (const n of nodes) {
        const res = await copyOrMove(op, repoKey, n.folder ? `${n.path}/` : n.path, to, { dry })
        all = [...all, ...res.messages]
      }
      if (dry) {
        setDryResult(all)
      } else {
        const bad = all.some((m) => m.level.toUpperCase() === 'ERROR' || m.level.toUpperCase() === 'FATAL')
        if (bad) toast.error(tt('{op}完成，但部分条目失败——结果明细见对话框', { op: op === 'copy' ? tt('复制') : tt('移动') }))
        else toast.success(tt('{op}完成（{v1} 项）', { op: op === 'copy' ? tt('复制') : tt('移动'), v1: nodes.length }))
        onDone()
        onClose()
      }
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 403 && !errorIsLicenseGate(err)) {
          setError(tt('无权限（HTTP 403）：{v1}', { v1: err.message }))
        } else {
          setError(err.message)
        }
      } else {
        setError(errText(err))
      }
      if (dry) setDryResult(null)
    } finally {
      setBusy(false)
    }
  }

  const doRun = async () => {
    if (op === 'move') {
      const ok = await confirm({
        title: tt('移动 {v1} 项', { v1: nodes.length }),
        description: tt('move 会将源路径从 {repoKey} 移到 {to}（源删除 + 目标落地）。制品不可变，此操作没有撤销。', { repoKey, to }),
        danger: true,
        confirmLabel: tt('移动'),
      })
      if (!ok) return
    }
    await runOnce(false)
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-w-lg" data-testid="copy-move-dialog">
        <DialogHeader>
          <DialogTitle>{op === 'copy' ? tt('复制到…') : tt('移动到…')}</DialogTitle>
          <DialogDescription>
            {single ? (
              <>
                <span className="font-mono" lang="en">{repoKey}/{single.path}</span>
                {tt('（')}{single.folder ? tt('目录——递归整树') : tt('文件')}{tt('）')}
              </>
            ) : (
              tt('已选 {v1} 项（目录递归整树）', { v1: nodes.length })
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="field">
            <Label htmlFor="cm-target-repo">{tt('目标仓库')} *</Label>
            <select
              id="cm-target-repo"
              value={targetRepo}
              onChange={(e) => setTargetRepo(e.target.value)}
              data-testid="copy-move-target-repo"
              className="h-8 w-full rounded-sm border border-input bg-surface-1 px-2 text-dense"
            >
              <option value="">{tt('选择目标仓库…')}</option>
              {candidates.map((r) => (
                <option key={r.key} value={r.key} lang="en">
                  {r.key} · {r.packageType}
                </option>
              ))}
            </select>
            <p className="field-hint text-aux text-muted-foreground">{tt('目标必须是 local 仓（可写）；remote/virtual 不是合法目标。')}</p>
          </div>
          <div className="field">
            <Label htmlFor="cm-target-path">{tt('目标路径（可选）')}</Label>
            <Input
              id="cm-target-path"
              value={targetPath}
              onChange={(e) => setTargetPath(e.target.value)}
              placeholder="acme/release"
              className="font-mono"
              data-testid="copy-move-target-path"
              lang="en"
            />
            <p className="field-hint text-aux text-muted-foreground">{tt('留空 = 目标仓根；目录会以 into-directory 语义并入目标路径。')}</p>
          </div>
          {error && (
            <div data-testid="copy-move-error" role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense">
              <div className="font-medium text-destructive">{tt('操作失败')}</div>
              <div className="mt-1 break-all font-mono text-aux" lang="en">{error}</div>
              {errorIsLicenseGateText(error) && (
                <div className="mt-1 text-aux text-muted-foreground">{tt('copy/move 是 pro 域能力（repo-operations 槽）——community 档如实呈现 403，不伪装成功。')}</div>
              )}
            </div>
          )}
          {dryResult && (
            <div data-testid="copy-move-dry" className="rounded-md border border-border bg-surface-2 px-3 py-2">
              <div className="mb-1 text-aux font-medium">{tt('预演结果（dry-run，零写入）')}</div>
              <ul className="max-h-40 overflow-y-auto">
                {dryResult.map((m, i) => (
                  <li key={i} className={`font-mono text-aux ${m.level.toUpperCase() === 'ERROR' || m.level.toUpperCase() === 'FATAL' ? 'text-destructive' : 'text-muted-foreground'}`} lang="en">
                    [{m.level}] {m.message}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" data-testid="copy-move-cancel" onClick={onClose}>
            {tt('取消')}
          </Button>
          <Button variant="outline" disabled={!canRun} data-testid="copy-move-dry-run" onClick={() => void runOnce(true)}>
            {busy ? tt('执行中…') : tt('预演（dry-run）')}
          </Button>
          <Button disabled={!canRun} data-testid="copy-move-run" onClick={() => void doRun()}>
            {busy ? tt('执行中…') : op === 'copy' ? tt('复制') : tt('移动')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function errorIsLicenseGate(err: ApiError): boolean {
  return errorIsLicenseGateText(err.message)
}

function errorIsLicenseGateText(message: string): boolean {
  return /license|entitlement|pro\b|addon/i.test(message)
}
