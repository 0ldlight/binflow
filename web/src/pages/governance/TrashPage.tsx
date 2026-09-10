// 回收站管理页（M12 T-352——P3 新栈重写）。三块：
// - 门控卡：trashcan 槽（Q3 暂行 pro）锁定态呈现——照 License 页先例；
//   addons 面失败/无注册表行 = 按未门控呈现（服务端 403 终裁纪律）。
// - 浏览：骑既有存储面（GET /api/storage/auto-trashcan + ?list&depth=1——
//   listChildren 同源）；面包屑逐层下钻；行点击展开下方详情面板（属性
//   五元组 ?properties 断言面 + 文件事实）；目录名点击进入。根 404 =
//   can 未落库（懒落库语义）。
// - 动作（system:write，仅全量 admin；readonly_admin 只读 + 反断言）：
//   恢复（prompt 承载可选 to 目的地覆盖）/ 单条永久清除（danger 确认）/
//   清空整罐（prompt 输入 EMPTY 强确认）。empty/clean 回执摘要就地呈现；
//   restore 回执 = copy/move messages 原文 toast。
// 锚族原样：trash-page/trash-locked/trash-breadcrumb/trash-refresh/
// trash-readonly-note/trash-unused/trash-list/trash-row-<path>/
// trash-restore-<path>/trash-restore-to/trash-clean-<path>/trash-detail/
// trash-empty-confirm/trash-empty/trash-summary。
import { useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { AlertBox, Badge } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, canAdminWrite, errText, getNodeProperties, isReadOnlyAdmin } from '@/lib/api'
import { formatBytes, formatCount } from '@/lib/format'
import {
  TRASH_REPO,
  TRASH_PROP_KEYS,
  cleanTrash,
  emptyTrash,
  formatTrashTime,
  restoreTrash,
  trashSlot,
} from '@/lib/trash'
import type { TrashSummary } from '@/lib/trash'
import { listChildren } from '@/pages/artifacts/lib'
import type { ChildNode } from '@/pages/artifacts/lib'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'

const t = tr('governance')

/** 五元组行的呈现顺序（trash.time 格式化；其余 mono 原文） */
function tupleRows(props: Record<string, string[]>): { k: string; label: string; v: ReactNode }[] {
  const first = (k: string) => props[k]?.[0] ?? ''
  return [
    {
      k: 'trash.time',
      label: t('删除时间（trash.time）'),
      v: (
        <>
          <span className="font-mono">{formatTrashTime(first('trash.time'))}</span>
          <span className="ml-2 text-2">
            <span className="font-mono" lang="en">{first('trash.time') || '—'}</span>{t('（epoch 毫秒）')}
          </span>
        </>
      ),
    },
    { k: 'trash.deletedBy', label: t('删除者（trash.deletedBy）'), v: <span className="font-mono" lang="en">{first('trash.deletedBy') || '—'}</span> },
    {
      k: 'trash.originalRepository',
      label: t('原仓库（trash.originalRepository）'),
      v: (
        <>
          <span className="font-mono" lang="en">{first('trash.originalRepository') || '—'}</span>
          {first('trash.originalRepository') && (
            <CopyButton value={first('trash.originalRepository')} label={t('原仓库 {v1}', { v1: first('trash.originalRepository') })} />
          )}
        </>
      ),
    },
    { k: 'trash.originalRepositoryType', label: t('原仓型（trash.originalRepositoryType）'), v: <span className="font-mono" lang="en">{first('trash.originalRepositoryType') || '—'}</span> },
    {
      k: 'trash.originalPath',
      label: t('原路径（trash.originalPath）'),
      v: (
        <>
          <span className="font-mono" lang="en">{first('trash.originalPath') || '—'}</span>
          {first('trash.originalPath') && <CopyButton value={first('trash.originalPath')} label={t('原路径')} />}
        </>
      ),
    },
  ]
}

export default function TrashPage() {
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const adminWrite = canAdminWrite(session)
  const confirm = useConfirm()

  const [rev, setRev] = useState(0)
  /** can 相对目录（'' = 根） */
  const [dir, setDir] = useState('')
  const [selected, setSelected] = useState<ChildNode | null>(null)
  const [busy, setBusy] = useState(false)
  /** 最近一次 empty/clean 的清剿摘要（就地呈现） */
  const [summary, setSummary] = useState<TrashSummary | null>(null)
  const [opError, setOpError] = useState<ApiError | null>(null)
  const bump = () => setRev((r) => r + 1)

  // 槽门控（License 页先例）：注册表行读实时求值；失败 = 按未门控呈现
  const slot = useAsync(trashSlot, [rev])
  const locked = slot.status === 'ok' && slot.data !== null && !slot.data.enabled
  const tierNeeded = slot.data?.minTier ?? 'pro'
  const holdBrowse = slot.status === 'loading' || locked

  // 浏览（锁定/求值中不发请求）；404 = can 未落库（懒落库语义）
  const listing = useAsync(
    () => (holdBrowse ? Promise.resolve(null) : listChildren(TRASH_REPO, dir)),
    [dir, rev, holdBrowse],
  )

  // 选中条目的属性五元组（?properties 断言面）
  const detail = useAsync(
    () =>
      selected && !holdBrowse
        ? getNodeProperties(TRASH_REPO, selected.path)
        : Promise.resolve<Record<string, string[]> | null>(null),
    [selected, rev, holdBrowse],
  )

  const navigateInto = (d: string) => {
    setDir(d)
    setSelected(null)
    setSummary(null)
    setOpError(null)
  }

  const afterMutation = () => {
    setSelected(null)
    bump()
  }

  const doRestore = async (node: ChildNode): Promise<void> => {
    // prompt 承载可选目的地覆盖（留空 = 按原位恢复——空值门与 confirm
    // 语义冲突，改走 prompt：初始 ''，validate 放行空）
    const to = await confirm.prompt({
      title: t('恢复条目'),
      description: (
        <>
          <p className="mb-2">
            {t('将把该条目移回原位（恢复 = 一次系统身份的 move）：落地树剥除全部')}{' '}
            <span className="font-mono" lang="en">trash.*</span> {t('标记，原属性保留。')}
          </p>
          <div className="kv">
            <span className="k">{t('条目')}</span>
            <span className="font-mono" lang="en">{TRASH_REPO}/{node.path}</span>
          </div>
          <p className="mt-2 text-aux text-muted-foreground">
            {t('目的地解析：to 覆盖 &gt; 属性五元组（原仓/原路径）&gt; 路径结构首段；目标仓必须为 local，且目标已有同名文件会被覆盖（move 族 override 语义）。')}
          </p>
        </>
      ),
      placeholder: t('留空 = 按原位恢复'),
      mono: true,
      allowEmpty: true,
      confirmLabel: t('恢复'),
      cancelLabel: t('取消'),
      anchor: 'trash-restore-to',
    })
    if (to === null) return
    setBusy(true)
    setOpError(null)
    try {
      const res = await restoreTrash(node.path, to)
      // CopyOrMoveResult messages 原文（服务端文案，不翻译）
      const first = res.messages.find((m) => m.level !== 'ERROR') ?? res.messages[0]
      toast.success(first?.message ?? t('恢复完成'))
      setSummary(null)
      afterMutation()
    } catch (err) {
      setOpError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  const doClean = async (node: ChildNode): Promise<void> => {
    const ok = await confirm.confirm({
      title: t('永久清除条目'),
      description: (
        <p>
          {t('将从回收站')}<b>{t('永久删除')}</b>{' '}
          <span className="font-mono" lang="en">{TRASH_REPO}/{node.path}</span>
          {node.folder ? t('（整个子树）') : ''}{t('。制品不可变，清除没有撤销；对应 blob 离开引用集、由常态 GC 回收。')}
        </p>
      ),
      danger: true,
      confirmLabel: t('永久清除'),
    })
    if (!ok) return
    setBusy(true)
    setOpError(null)
    try {
      const sum = await cleanTrash(node.path)
      setSummary(sum)
      toast.success(t('已清除 {v1} 项（{v2} 文件 / {v3} 目录）', { v1: formatCount(sum.removed), v2: formatCount(sum.files), v3: formatCount(sum.folders) }))
      afterMutation()
    } catch (err) {
      setOpError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  const doEmpty = async (): Promise<void> => {
    const typed = await confirm.prompt({
      title: t('清空回收站'),
      description: (
        <p>
          {t('将')}<b>{t('清空整个回收站')}</b>{t('——罐内全部条目永久删除（含所有原仓/原路径）。制品不可变， 清除没有撤销；对应 blob 离开引用集、由常态 GC 回收。保留期（默认 14 天）内未到期的 条目同样被清除。')}
          <br />{t('输入')} <b className="font-mono" lang="en">EMPTY</b> {t('以确认。')}
        </p>
      ),
      placeholder: 'EMPTY',
      mono: true,
      danger: true,
      confirmLabel: t('清空回收站'),
      cancelLabel: t('取消'),
      anchor: 'trash-empty-confirm',
      validate: (v) => (v === 'EMPTY' ? null : t('需输入 EMPTY')),
    })
    if (typed !== 'EMPTY') return
    setBusy(true)
    setOpError(null)
    try {
      const sum = await emptyTrash()
      setSummary(sum)
      toast.success(t('回收站已清空：移除 {v1} 项', { v1: formatCount(sum.removed) }))
      afterMutation()
    } catch (err) {
      setOpError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  const crumbs = dir.split('/').filter((s) => s !== '')

  // 已知空面(根级空罐 / can 未落库 404): 清空钮禁用——无对象可清
  const canKnownEmpty =
    (listing.status === 'ok' && (listing.data?.length ?? 0) === 0 && dir === '') ||
    (listing.status === 'error' && listing.error?.status === 404 && dir === '')

  return (
    <div data-testid="trash-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('回收站')}</h2>
        <span className="text-aux text-2">
          {t('local 仓删除先捕获进内置仓')} <span className="font-mono" lang="en">{TRASH_REPO}</span>{t('（保留期默认 14 天，小时级 cron 自动清）')}
        </span>
      </div>

      {/* 槽门控卡（License 页先例） */}
      {slot.status === 'loading' && (
        <section className="card mb-4 p-3">
          <StateSkeleton lines={3} />
        </section>
      )}
      {slot.status === 'ok' && locked && (
        <section className="mb-4 rounded-md border border-border bg-surface-1 p-3" data-testid="trash-locked">
          <h3 className="mb-1 text-dense font-semibold">
            {t('回收站未解锁（trashcan 槽 · 需要')} {tierNeeded} {t('档）')}
          </h3>
          <p className="field-hint mt-0">
            {t('当前实例的')} <span className="font-mono" lang="en">trashcan</span> {t('功能槽处于锁定态（Q3 暂行 pro 档）：删除侧')}<b>{t('不捕获')}</b>{t('（维持硬删语义），trash REST 族对 community 答 403 +')}{' '}
            <span className="font-mono" lang="en">X-Binflow-License-Required: trashcan</span>{t('。装对应档位 license 后即刻解锁（')}<Link className="text-primary hover:underline" to="/admin/general/license">License &amp; Add-ons</Link>{t('）。')}
          </p>
        </section>
      )}

      {!locked && (
        <>
          <section aria-label={t('回收站内容')} className="mb-4 rounded-md border border-border bg-surface-1 px-3 pb-2 pt-3">
            <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
              <nav data-testid="trash-breadcrumb" aria-label={t('回收站路径')} className="flex flex-wrap items-center gap-1 text-dense">
                <button
                  type="button"
                  lang="en"
                  className="font-mono text-primary hover:underline"
                  onClick={() => navigateInto('')}
                >
                  {TRASH_REPO}
                </button>
                {crumbs.map((c, i) =>
                  i < crumbs.length - 1 ? (
                    <span key={`${c}-${i}`} className="flex items-center gap-1">
                      <span aria-hidden="true">/</span>
                      <button
                        type="button"
                        lang="en"
                        className="font-mono text-primary hover:underline"
                        onClick={() => navigateInto(crumbs.slice(0, i + 1).join('/'))}
                      >
                        {c}
                      </button>
                    </span>
                  ) : (
                    <span key={`${c}-${i}`} className="flex items-center gap-1">
                      <span aria-hidden="true">/</span>
                      <span className="font-mono" lang="en">{c}</span>
                    </span>
                  ),
                )}
              </nav>
              <Button variant="outline" size="sm" data-testid="trash-refresh" onClick={() => bump()}>{t('刷新')}</Button>
            </div>

            {readOnly && (
              <p className="field-hint" data-testid="trash-readonly-note">{t('ⓘ 只读管理员视角：恢复/清除/清空是管理面写操作（system:write，仅全量 admin）；浏览只读，服务端 403 兜底。')}</p>
            )}

            {listing.status === 'loading' && <StateSkeleton lines={5} />}
            {listing.status === 'error' && listing.error && (
              <>
                {listing.error.status === 404 ? (
                  <EmptyState
                    testid="trash-unused"
                    message={t('回收站尚未启用（未发生过捕获）')}
                    hint={t('内置仓 {TRASH_REPO} 懒落库：首次捕获删除时才创建。若实例刚装好 pro license，先删一次制品再回来。', { TRASH_REPO: TRASH_REPO })}
                  />
                ) : (
                  <ErrorCard error={listing.error} onRetry={listing.reload} />
                )}
              </>
            )}
            {listing.status === 'forbidden' && listing.error && (
              <EmptyState
                message={t('无权限查看回收站')}
                hint={t('浏览走存储面（GET /api/storage/{TRASH_REPO}，管理面读门）；写操作另需 system:write。', { TRASH_REPO: TRASH_REPO })}
              />
            )}
            {listing.status === 'ok' && listing.data && listing.data.length === 0 && (
              <EmptyState
                message={dir === '' ? t('回收站是空的') : t('此目录为空')}
                hint={dir === '' ? t('local 仓的删除会捕获到这里（保留期内可恢复）；本地生成物（索引/元数据）不入站。') : undefined}
              />
            )}
            {listing.status === 'ok' && listing.data && listing.data.length > 0 && (
              <table className="w-full text-dense" data-testid="trash-list">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-3 py-2 font-medium">{t('名称')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('类型')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('大小')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('修改时间')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{adminWrite && !readOnly ? t('操作') : ''}</th>
                  </tr>
                </thead>
                <tbody>
                  {listing.data.map((node) => (
                    <tr
                      key={node.path}
                      className={`cursor-pointer border-b border-border/60 hover:bg-accent${selected?.path === node.path ? ' bg-accent' : ''}`}
                      data-testid={`trash-row-${node.path}`}
                      onClick={() => setSelected(node)}
                    >
                      <td className="px-3 py-1.5">
                        {node.folder ? (
                          <button
                            type="button"
                            className="font-mono text-primary hover:underline"
                            lang="en"
                            onClick={(e) => {
                              e.stopPropagation()
                              navigateInto(node.path)
                            }}
                            title={t('进入目录')}
                          >
                            ▸ {node.name}
                          </button>
                        ) : (
                          <span className="font-mono" lang="en">{node.name}</span>
                        )}
                        <CopyButton value={`${TRASH_REPO}/${node.path}`} label={t('路径 {v1}', { v1: node.name })} />
                      </td>
                      <td className="px-3 py-1.5">
                        <Badge>{node.folder ? t('目录') : t('文件')}</Badge>
                      </td>
                      <td className="px-3 py-1.5 font-mono">{node.size === null ? '—' : formatBytes(node.size)}</td>
                      <td className="px-3 py-1.5 font-mono">{node.lastModified || '—'}</td>
                      <td className="px-3 py-1.5">
                        {adminWrite && !readOnly && (
                          <span className="inline-flex gap-1">
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7"
                              disabled={busy}
                              data-testid={`trash-restore-${node.path}`}
                              onClick={(e) => {
                                e.stopPropagation()
                                void doRestore(node)
                              }}
                            >
                              {t('恢复…')}
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 border-destructive/50 text-destructive hover:bg-destructive/10"
                              disabled={busy}
                              data-testid={`trash-clean-${node.path}`}
                              onClick={(e) => {
                                e.stopPropagation()
                                void doClean(node)
                              }}
                            >
                              {t('清除')}
                            </Button>
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>

          {/* 选中条目详情：属性五元组（AC1 断言面） */}
          {selected && (
            <section aria-label={t('条目详情')} className="mb-4 rounded-md border border-border bg-surface-1 px-3 pb-2 pt-3" data-testid="trash-detail">
              <h3 className="mb-2 text-dense font-semibold">{t('条目详情（属性五元组）')}</h3>
              <div className="kv">
                <span className="k">{t('路径')}</span>
                <span className="font-mono" lang="en">{TRASH_REPO}/{selected.path}</span>
                <CopyButton value={`${TRASH_REPO}/${selected.path}`} label={t('回收站路径')} />
              </div>
              <div className="kv">
                <span className="k">{t('类型 / 大小')}</span>
                <span>
                  {selected.folder ? t('目录') : t('文件')} ·{' '}
                  <span className="font-mono">{selected.size === null ? '—' : formatBytes(selected.size)}</span>
                </span>
              </div>
              {selected.sha256 && (
                <div className="kv">
                  <span className="k">sha256</span>
                  <span className="font-mono" lang="en">{selected.sha256.slice(0, 12)}…</span>
                  <CopyButton value={selected.sha256} label="sha256" />
                </div>
              )}
              {detail.status === 'loading' && <StateSkeleton lines={4} />}
              {detail.status === 'error' && detail.error && (
                <p className="field-error" role="alert">{t('五元组读取失败：')}{detail.error.message}</p>
              )}
              {detail.status === 'ok' && detail.data && (
                <>
                  {tupleRows(detail.data).map((r) => (
                    <div className="kv" key={r.k}>
                      <span className="k">{r.label}</span>
                      <span>{r.v}</span>
                    </div>
                  ))}
                  {TRASH_PROP_KEYS.every((k) => !(k in (detail.data ?? {}))) && (
                    <p className="field-hint mb-0">{t('该节点无 trash.* 标记（裸行——捕获后、打标前崩溃的降级形态；保留期按行龄回退判定）。')}</p>
                  )}
                  {/* 随行原属性如实呈现（非 trash.* 键） */}
                  {Object.entries(detail.data)
                    .filter(([k]) => !k.startsWith('trash.'))
                    .map(([k, vs]) => (
                      <div className="kv" key={`x-${k}`}>
                        <span className="k">
                          <span className="font-mono" lang="en">{k}</span>{t('（随行属性）')}
                        </span>
                        <span className="font-mono" lang="en">{vs.join(', ')}</span>
                        <CopyButton value={vs.join(', ')} label={t('属性 {k}', { k: k })} />
                      </div>
                    ))}
                </>
              )}
            </section>
          )}

          {/* 最近一次 empty/clean 的清剿摘要 */}
          {summary && (
            <AlertBox severity="success" testid="trash-summary" className="mb-4">
              <div className="font-medium">{t('清剿完成')}</div>
              <div className="kv">
                <span className="k">{t('移除')}</span>
                <span className="font-mono">{formatCount(summary.removed)} {t('项')}</span>
              </div>
              <div className="kv">
                <span className="k">{t('文件 / 目录')}</span>
                <span className="font-mono">{formatCount(summary.files)} / {formatCount(summary.folders)}</span>
              </div>
              <div className="kv">
                <span className="k">{t('字节')}</span>
                <span className="font-mono">{formatBytes(summary.bytes)}</span>
              </div>
              <div className="mt-1 text-2">{t('对应 blob 离开 GC 引用集，由常态 GC（宽限期后）回收——清剿本身不物理删 blob。')}</div>
            </AlertBox>
          )}

          {opError && (
            <AlertBox severity="error" className="mb-4">
              <div className="font-medium">
                <span aria-hidden="true">✗</span> {t('操作失败（HTTP')} {opError.status || t('网络')}{t('）')}
                {opError.status === 403 ? t('——trash 族门为 system:write（仅全量 admin）；license 槽锁定时为 403 + X-Binflow-License-Required') : ''}
              </div>
              <pre lang="en" className="font-mono text-aux">{opError.raw || opError.message}</pre>
            </AlertBox>
          )}

          {/* 危险区：清空整罐（P5） */}
          {adminWrite && !readOnly && (
            <section className="danger-zone mb-4 rounded-md border border-destructive/50 px-3 pb-2 pt-3">
              <h3 className="mb-2 text-dense font-semibold text-destructive">{t('危险区：清空回收站')}</h3>
              <p className="field-hint mt-0">{t('永久清除罐内')}<b>{t('全部')}</b>{t('条目（含保留期内未到期项）。输入 EMPTY 强确认。')}</p>
              <Button
                variant="outline"
                size="sm"
                className="border-destructive/50 text-destructive hover:bg-destructive/10"
                disabled={busy || canKnownEmpty}
                onClick={() => void doEmpty()}
                data-testid="trash-empty"
                title={t('清空整个回收站（不可撤销）')}
              >
                {t('清空回收站…')}
              </Button>
            </section>
          )}
        </>
      )}
    </div>
  )
}
