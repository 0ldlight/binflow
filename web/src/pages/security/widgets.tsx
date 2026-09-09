// 安全域共享小件（P3 新栈重写——旧 MUI 实现随本批退役）：
//   PermSummaryTable  只读权限矩阵（§6.9[5]/§6.10：r/a/w/d/m 五列，manage
//                     头带「不隐含读写删」说明——§7.2）
//   StatusLabel/useUserDelete 已上收共享层：components/layout/bits.tsx
//   （StatusLabel）与本文件 useUserDelete（新确认层 prompt 承载输入型
//   强确认，user-delete-confirm-name 锚保持）
// 排序三件（SortTh/useTableSort/applySort）上收 components/layout/table.tsx。
import { useState } from 'react'
import { Link } from 'react-router-dom'

import { toast } from '@/lib/toast'

import { useConfirm } from '@/app/providers'
import { Badge } from '@/components/layout/bits'
import { ApiError, errText } from '@/lib/api'
import { deleteUser } from './api'
import { PERM_ACTIONS } from './api'
import type { PrincipalGrantRow } from './api'
import { tr } from '@/i18n'

const t = tr('security')

/** 只读权限矩阵（§6.9[5]）：Permission Name │ 应用途径 │ r/a/w/d/m。
 *  行 = target；来源 = 直接 + 经组（用户视角）或直接（组视角）。 */
export function PermSummaryTable({
  rows,
  rowTestidPrefix,
  emptyHint,
}: {
  rows: readonly PrincipalGrantRow[]
  rowTestidPrefix: string
  emptyHint: string
}) {
  if (rows.length === 0) {
    return <p className="text-muted-foreground perm-summary-empty">{emptyHint}</p>
  }
  return (
    <table className="perm-summary w-full text-dense" data-testid={`${rowTestidPrefix}-matrix`}>
      <thead>
        <tr className="border-b border-border text-left text-aux text-muted-foreground">
          <th scope="col" className="px-3 py-2 font-medium">Permission Name</th>
          <th scope="col" className="px-3 py-2 font-medium">{t('应用途径')}</th>
          {PERM_ACTIONS.map((a) => (
            <th
              key={a}
              scope="col"
              className="th-action px-3 py-2 text-center font-medium"
              title={
                a === 'manage'
                  ? t('manage = 仓库配置派生权（不隐含读写删）')
                  : a === 'annotate'
                    ? t('annotate = 属性写位（7.161 标签 Annotate；不隐含内容写）')
                    : a === 'write'
                      ? t('write = 部署位（7.161 标签 Deploy/Cache；wire 正名 deploy-cache；不携带 annotate）')
                      : a === 'delete'
                        ? t('delete = 删除/覆盖（7.161 标签 Delete/Overwrite）')
                        : undefined
              }
            >
              {a}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.target} className="border-b border-border/60 hover:bg-accent">
            <td className="px-3 py-1.5">
              <Link className="row-link font-mono" to={`/admin/security/permissions/${encodeURIComponent(r.target)}`} lang="en">
                {r.target}
              </Link>
            </td>
            <td className="px-3 py-1.5">
              <span className="sec-chips">
                {r.sources.map((s) =>
                  s === 'direct' ? (
                    <Badge key="direct">{t('直接')}</Badge>
                  ) : (
                    <Badge key={s} mono lang="en">{s}</Badge>
                  ),
                )}
              </span>
            </td>
            {PERM_ACTIONS.map((a) => (
              <td key={a} className="td-mark px-3 py-1.5 text-center">
                {r.actions.includes(a) ? (
                  <span className="mark-on text-success" aria-label={t('{a} 已授予', { a: a })}>
                    ✓
                  </span>
                ) : (
                  <span className="mark-off text-muted-foreground" aria-label={t('{a} 未授予', { a: a })}>
                    —
                  </span>
                )}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

// ---- 删用户强确认（T-257；E4 DELETE /api/security/users/{name} 消费面） ----
//
// 形态：新确认层 prompt（输入型）+ **输入用户名强确认**（M9 review 裁定的
// 升格形态——级联不可恢复 + 重复删除是确定性 404 非幂等，误触无补救路径）
// + 文案体现「不可恢复」。user-delete-confirm-name 锚保持。
//
// 服务端护栏如实呈现（UI 只预禁用可达的两态：自删/内置 admin——其余
// 〔最后一个 admin 400、404 已删〕是并发/环境事实，交服务端终裁原样上浮）：
// 404 `User not found`（已被他人删）按事实刷新列表，不伪造成成功。

export interface UserDeleteHooks {
  /** 删除成功后的收尾（列表刷新 / 详情页返回列表）——在调用方闭包里 */
  onDeleted?: (name: string) => void
}

export function useUserDelete({ onDeleted }: UserDeleteHooks = {}) {
  const confirm = useConfirm()
  const [deleting, setDeleting] = useState(false)

  const openDialog = async (name: string): Promise<void> => {
    const typed = await confirm.prompt({
      title: t('删除用户'),
      description: (
        <>
          {t('确定要移除用户')} <b className="font-mono" lang="en">{name}</b> {t('吗？此操作')}<b>{t('不可恢复')}</b>{t('。')}
          {' '}{t('同一事务内级联：组员关系、permission target 中的直接授权、全部 API token 与活跃会话 （持有其 token 的请求随即 401）。审计历史保留。重复删除会被服务端拒绝（404，有意非幂等）。')}
        </>
      ),
      placeholder: name,
      mono: true,
      danger: true,
      confirmLabel: t('删除用户'),
      cancelLabel: t('取消'),
      anchor: 'user-delete-confirm-name',
      validate: (v) => (v === name ? null : t('输入与用户名不一致（{name}）', { name })),
    })
    if (typed === null || typed !== name) return
    setDeleting(true)
    try {
      const text = await deleteUser(name)
      toast.success(text) // 服务端纯文本（"The user: 'x' has been removed successfully."）
      onDeleted?.(name)
    } catch (err) {
      // 如实呈现：400 护栏（内置/last-admin/自删）与 404（已被他人删）的
      // 服务端原文即最终事实——不重试不美化
      toast.error(t('删除失败：{v1}', { v1: errText(err) }))
      if (err instanceof ApiError && err.status === 404) onDeleted?.(name)
    } finally {
      setDeleting(false)
    }
  }

  return { openDialog, deleting }
}
