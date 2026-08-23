import { useState } from 'react'
import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'

import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { ApiError, errText } from '../../lib/api'
import { deleteRepo } from '../../lib/repos'

// 删仓强确认（console-m8 §4.6 仓库行 / C9 危险确认，T-240 自列表行与
// 详情危险区共用）：
//
// - 文案基线：「将永久删除 `<key>` 仓库及其全部制品。」+ □ 同时删除内容
//   （deleteContent）+ **输入 key 确认**（P5 强确认形态——Artifactory 无
//   输入确认，BinFlow 有意增强）。
// - 非空仓不勾 deleteContent 直接确认 → 服务端 400（原因含「holds N
//   node(s)」）被带回对话框原样呈现并预勾选——用户看见影响面再决定，
//   而不是撞 400（T-99 落定的两段流）。
// - 门（router.go）：DELETE = CapRepoWrite（仅全量 admin）；调用方负责
//   L4 预收敛，服务端 403 兜底。
//
// 锚（冻结）：repo-delete-content / repo-delete-confirm-key /
// repo-delete-reason；对话框基座 confirm-dialog / confirm-accept。

export interface RepoDeleteTarget {
  key: string
  rclass: string
  packageType: string
}

export function useRepoDelete({ onDeleted }: { onDeleted?: (key: string) => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState(false)

  const openDialog = async (repo: RepoDeleteTarget, reason?: string, presetContent = false): Promise<void> => {
    const holder = { typed: '', deleteContent: presetContent }
    const body: ReactNode = (
      <>
        {reason && (
          <div className="server-reason" data-testid="repo-delete-reason" lang="en">
            HTTP 400：{reason}
          </div>
        )}
        <p>
          将永久删除仓库 <b className="mono" lang="en">{repo.key}</b>（{repo.rclass} / {repo.packageType}）
          及其全部制品。制品不可变，删除<b>没有撤销</b>。
        </p>
        <label className="check-row">
          <input
            type="checkbox"
            checked={holder.deleteContent}
            onChange={(e) => {
              holder.deleteContent = e.target.checked
            }}
            data-testid="repo-delete-content"
          />
          同时删除内容（deleteContent）——非空仓必须勾选
        </label>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0 }}>
          <label htmlFor={`del-confirm-${repo.key}`}>
            输入仓库 key <b className="mono" lang="en">{repo.key}</b> 以确认：
          </label>
          <input
            id={`del-confirm-${repo.key}`}
            className="confirm-input"
            autoComplete="off"
            onChange={(e) => {
              holder.typed = e.target.value
            }}
            data-testid="repo-delete-confirm-key"
            lang="en"
          />
        </div>
      </>
    )
    const ok = await confirm({
      title: '删除仓库',
      body,
      danger: true,
      confirmLabel: deleting ? '删除中…' : '删除仓库',
      confirmDisabled: () => holder.typed !== repo.key,
    })
    if (!ok) return
    setDeleting(true)
    try {
      const text = await deleteRepo(repo.key, holder.deleteContent)
      toast.success(text)
      onDeleted?.(repo.key)
      // 回到该仓型 Tab（列表视角恢复；详情页发起时同样离开已删对象）
      navigate(`/admin/repositories/${repo.rclass === 'remote' || repo.rclass === 'virtual' ? repo.rclass : 'local'}`)
    } catch (err) {
      const apiErr = err instanceof ApiError ? err : new ApiError(0, errText(err))
      if (apiErr.status === 400 && apiErr.message.includes('deleteContent') && !presetContent) {
        setDeleting(false)
        // 非空仓未勾内容删除：把 400 原因（含制品数）带回对话框，预勾选重试
        await openDialog(repo, apiErr.message, true)
        return
      }
      toast.error(`删除失败：${apiErr.message}`)
    } finally {
      setDeleting(false)
    }
  }

  return openDialog
}
