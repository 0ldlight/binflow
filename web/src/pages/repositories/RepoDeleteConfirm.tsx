import { Input } from '@/components/ui/input'
import { useState } from 'react'
import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'

import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { errText } from '../../lib/api'
import { deleteRepo } from '../../lib/repos'
import { tr } from '../../i18n'

const t = tr('repositories')

// 删仓强确认（console-m8 §4.6 仓库行 / C9 危险确认，T-240 自列表行与
// 详情危险区共用；T-565 对齐 T-555 级联语义翻新）：
//
// - 文案基线：「将永久删除 `<key>` 仓库及其全部制品。」+ **输入 key
//   确认**（P5 强确认形态——同类控制台无输入确认，BinFlow 有意增强）。
// - 服务端（T-555/BIN-37）：删仓 = 静默级联，非空仓连同全部内容一并
//   删除，恒 200 JSON 报告体；不再有「非空仓 400 拒绝 + deleteContent
//   旗确认」两段流，也不再有对应复选框——破坏性警示由文案（删除全部
//   制品、没有撤销）+ 输入 key 承载。成功反馈携带报告体的级联计数
//   （deletedArtifactsCount > 0 时「已删除 N 项内容」）。
// - 门（router.go）：DELETE = CapRepoWrite（仅全量 admin）；调用方负责
//   L4 预收敛，服务端 403 兜底；未知仓 404 走通用错误 toast。
//
// 锚（冻结）：repo-delete-confirm-key；对话框基座 confirm-dialog /
// confirm-accept。（旧锚 repo-delete-content / repo-delete-reason 随
// 两段流退役，T-565。）

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

  const openDialog = async (repo: RepoDeleteTarget): Promise<void> => {
    const holder = { typed: '' }
    const body: ReactNode = (
      <>
        <p>{t('将永久删除仓库')} <b className="font-mono text-[0.95em]" lang="en">{repo.key}</b>{t('（')}{repo.rclass} / {repo.packageType}{t('） 及其全部制品。制品不可变，删除')}<b>{t('没有撤销')}</b>{t('。')}        </p>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0 }}>
          <label htmlFor={`del-confirm-${repo.key}`}>{t('输入仓库 key')} <b className="font-mono text-[0.95em]" lang="en">{repo.key}</b> {t('以确认：')}          </label>
          <Input
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
      title: t('删除仓库'),
      body,
      danger: true,
      confirmLabel: deleting ? t('删除中…') : t('删除仓库'),
      confirmDisabled: () => holder.typed !== repo.key,
    })
    if (!ok) return
    setDeleting(true)
    try {
      const report = await deleteRepo(repo.key)
      toast.success(
        report.deletedArtifactsCount > 0
          ? `${report.statusMsg}${t('（已删除 {n} 项内容）', { n: report.deletedArtifactsCount })}`
          : report.statusMsg,
      )
      onDeleted?.(repo.key)
      // 回到该仓型 Tab（列表视角恢复；详情页发起时同样离开已删对象）
      navigate(`/admin/repositories/${repo.rclass === 'remote' || repo.rclass === 'virtual' ? repo.rclass : 'local'}`)
    } catch (err) {
      toast.error(t('删除失败：{v1}', { v1: errText(err) }))
    } finally {
      setDeleting(false)
    }
  }

  return openDialog
}
