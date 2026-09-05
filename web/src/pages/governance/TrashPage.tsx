import { useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Breadcrumbs from '@mui/material/Breadcrumbs'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import MuiLink from '@mui/material/Link'
import Paper from '@mui/material/Paper'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, getNodeProperties, isReadOnlyAdmin } from '../../lib/api'
import { formatBytes, formatCount } from '../../lib/format'
import {
  TRASH_REPO,
  TRASH_PROP_KEYS,
  cleanTrash,
  emptyTrash,
  formatTrashTime,
  restoreTrash,
  trashSlot,
} from '../../lib/trash'
import type { TrashSummary } from '../../lib/trash'
import { listChildren } from '../artifacts/lib'
import type { ChildNode } from '../artifacts/lib'
import { useAsync } from '../../lib/useAsync'
import { tr } from '../../i18n'

const t = tr('governance')

// 回收站管理页（M12 T-352，FR-106 FE 腿；治理分组——gc/cleanup 破坏性管理面
// 同族姿态）。三块：
//
// - 门控卡：trashcan 槽（Q3 暂行 pro）锁定态呈现——照 License 页先例（槽
//   行 + 需要档位提示），锁定时整数据面不渲染（community 实例删除侧本就
//   不捕获，can 恒空——如实说明而非硬探 404）。addons 面失败/无注册表行 =
//   按未门控呈现（服务端 403 + X-Binflow-License-Required 终裁，§3.6.3
//   403 驱动纪律不在前端复制门控）。
// - 浏览：骑既有存储面（GET /api/storage/auto-trashcan + ?list&depth=1
//   元数据合并——listChildren 同源）；面包屑逐层下钻；行点击展开下方详情
//   面板（属性五元组 ?properties 断言面 + 文件事实）；目录名点击进入。
//   根 404 = can 未落库（尚未发生过捕获——内置仓懒落库）。
// - 动作（system:write，仅全量 admin；readonly_admin 只读 + 反断言）：
//   恢复（ConfirmDialog + 可选 to 目的地输入——目的地解析 to 覆盖 > 五元组
//   > 路径结构）/ 单条永久清除（danger 确认）/ 清空整罐（danger + 输入
//   EMPTY 强确认，GC apply 同款）。empty/clean 回执摘要（removed/files/
//   folders/bytes）就地呈现；restore 回执 = copy/move messages 原文 toast。
//
// NFR-S61：auto-trashcan 无 permission target 能点名——本页浏览器面在管理
// 壳内恒经会话（匿名重定向登录）；内容面匿名读由服务端 ACL 构造性拒绝。

/** 五元组行的呈现顺序（trash.time 格式化；其余 mono 原文） */
function tupleRows(props: Record<string, string[]>): { k: string; label: string; v: ReactNode }[] {
  const first = (k: string) => props[k]?.[0] ?? ''
  return [
    {
      k: 'trash.time',
      label: t('删除时间（trash.time）'),
      v: (
        <>
          <span className="mono">{formatTrashTime(first('trash.time'))}</span>
          <span className="text-2" style={{ marginLeft: 8 }}>
            <span className="mono" lang="en">{first('trash.time') || '—'}</span>{t('（epoch 毫秒）')}          </span>
        </>
      ),
    },
    { k: 'trash.deletedBy', label: t('删除者（trash.deletedBy）'), v: <span className="mono" lang="en">{first('trash.deletedBy') || '—'}</span> },
    {
      k: 'trash.originalRepository',
      label: t('原仓库（trash.originalRepository）'),
      v: (
        <>
          <span className="mono" lang="en">{first('trash.originalRepository') || '—'}</span>
          {first('trash.originalRepository') && (
            <CopyButton value={first('trash.originalRepository')} label={t('原仓库 {v1}', { v1: first('trash.originalRepository') })} />
          )}
        </>
      ),
    },
    { k: 'trash.originalRepositoryType', label: t('原仓型（trash.originalRepositoryType）'), v: <span className="mono" lang="en">{first('trash.originalRepositoryType') || '—'}</span> },
    {
      k: 'trash.originalPath',
      label: t('原路径（trash.originalPath）'),
      v: (
        <>
          <span className="mono" lang="en">{first('trash.originalPath') || '—'}</span>
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
  const toast = useToast()
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
  // 浏览挂起判定：槽求值中（hold——防锁定态首帧抢跑一发浏览请求）或已锁定
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
    const holder = { to: '' }
    const body: ReactNode = (
      <>
        <p>{t('将把该条目移回原位（恢复 = 一次系统身份的 move）：落地树剥除全部')}{' '}
          <span className="mono" lang="en">trash.*</span> {t('标记，原属性保留。')}        </p>
        <div className="kv">
          <span className="k">{t('条目')}</span>
          <span className="mono" lang="en">{TRASH_REPO}/{node.path}</span>
        </div>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0, marginTop: 12 }}>
          <label htmlFor="trash-restore-to">{t('目的地覆盖 to（可选，')}<span className="mono" lang="en">/&lt;repo&gt;[/&lt;path&gt;]</span>{t('）')}          </label>
          <input
            id="trash-restore-to"
            className="confirm-input"
            data-testid="trash-restore-to"
            autoComplete="off"
            placeholder={t('留空 = 按原位恢复')}
            onChange={(e) => {
              holder.to = e.target.value
            }}
            lang="en"
          />
        </div>
        <p className="field-hint" style={{ marginTop: 8 }}>{t('目的地解析：to 覆盖 &gt; 属性五元组（原仓/原路径）&gt; 路径结构首段；目标仓必须为 local，且目标已有同名文件会被覆盖（move 族 override 语义）。')}        </p>
      </>
    )
    const ok = await confirm({ title: t('恢复条目'), body, confirmLabel: t('恢复') })
    if (!ok) return
    setBusy(true)
    setOpError(null)
    try {
      const res = await restoreTrash(node.path, holder.to)
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
    const body: ReactNode = (
      <>
        <p>{t('将从回收站')}<b>{t('永久删除')}</b>{' '}
          <span className="mono" lang="en">{TRASH_REPO}/{node.path}</span>
          {node.folder ? t('（整个子树）') : ''}{t('。制品不可变，清除没有撤销；对应 blob 离开引用集、由常态 GC 回收。')}        </p>
      </>
    )
    const ok = await confirm({
      title: t('永久清除条目'),
      body,
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
    const holder = { typed: '' }
    const body: ReactNode = (
      <>
        <p>{t('将')}<b>{t('清空整个回收站')}</b>{t('——罐内全部条目永久删除（含所有原仓/原路径）。制品不可变， 清除没有撤销；对应 blob 离开引用集、由常态 GC 回收。保留期（默认 14 天）内未到期的 条目同样被清除。')}        </p>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0, marginTop: 12 }}>
          <label htmlFor="trash-empty-confirm">{t('输入')} <b className="mono" lang="en">EMPTY</b> {t('以确认：')}          </label>
          <input
            id="trash-empty-confirm"
            className="confirm-input"
            data-testid="trash-empty-confirm"
            autoComplete="off"
            onChange={(e) => {
              holder.typed = e.target.value
            }}
            lang="en"
          />
        </div>
      </>
    )
    const ok = await confirm({
      title: t('清空回收站'),
      body,
      danger: true,
      confirmLabel: t('清空回收站'),
      confirmDisabled: () => holder.typed !== 'EMPTY',
    })
    if (!ok) return
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

  // 已知空面(根级空罐 / can 未落库 404): 清空钮禁用——无对象可清, 不邀误操作
  const canKnownEmpty =
    (listing.status === 'ok' && (listing.data?.length ?? 0) === 0 && dir === '') ||
    (listing.status === 'error' && listing.error?.status === 404 && dir === '')

  return (
    <div data-testid="trash-page">
      <div className="page-header">
        <h2>{t('回收站')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{t('local 仓删除先捕获进内置仓')} <span className="mono" lang="en">{TRASH_REPO}</span>{t('（保留期默认 14 天，小时级 cron 自动清）')}        </span>
      </div>

      {/* 槽门控卡（License 页先例） */}
      {slot.status === 'loading' && (
        <Paper component="section" sx={{ p: 2, mb: 2 }}>
          <Skeleton lines={3} />
        </Paper>
      )}
      {slot.status === 'ok' && locked && (
        <Paper component="section" sx={{ p: 2, mb: 2 }} data-testid="trash-locked">
          <Typography variant="subtitle2" component="h3" sx={{ mb: 1 }}>{t('回收站未解锁（trashcan 槽 · 需要')} {tierNeeded} {t('档）')}          </Typography>
          <p className="field-hint" style={{ marginTop: 0 }}>{t('当前实例的')} <span className="mono" lang="en">trashcan</span> {t('功能槽处于锁定态（Q3 暂行 pro 档）：删除侧')}<b>{t('不捕获')}</b>{t('（维持硬删语义），trash REST 族对 community 答 403 +')}{' '}
            <span className="mono" lang="en">X-Binflow-License-Required: trashcan</span>{t('。装对应档位 license 后即刻解锁（')}<Link to="/admin/general/license">License &amp; Add-ons</Link>{t('）。')}          </p>
        </Paper>
      )}

      {!locked && (
        <>
          <Paper component="section" aria-label={t('回收站内容')} sx={{ p: 2, pb: 1.5, mb: 2 }}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: 2, mb: 1, flexWrap: 'wrap' }}>
              <Breadcrumbs
                data-testid="trash-breadcrumb"
                aria-label={t('回收站路径')}
                separator={<span aria-hidden="true">/</span>}
                sx={{ '& .MuiBreadcrumbs-li': { whiteSpace: 'nowrap' } }}
              >
                <MuiLink
                  component="button"
                  type="button"
                  lang="en"
                  onClick={() => navigateInto('')}
                  sx={{ fontFamily: 'var(--bf-mono)' }}
                >
                  {TRASH_REPO}
                </MuiLink>
                {crumbs.map((c, i) =>
                  i < crumbs.length - 1 ? (
                    <MuiLink
                      key={`${c}-${i}`}
                      component="button"
                      type="button"
                      lang="en"
                      onClick={() => navigateInto(crumbs.slice(0, i + 1).join('/'))}
                      sx={{ fontFamily: 'var(--bf-mono)' }}
                    >
                      {c}
                    </MuiLink>
                  ) : (
                    <Typography key={`${c}-${i}`} component="span" className="mono" lang="en" sx={{ fontSize: 'inherit' }}>
                      {c}
                    </Typography>
                  ),
                )}
              </Breadcrumbs>
              <Button variant="outlined" size="small" data-testid="trash-refresh" onClick={() => bump()}>{t('刷新')}              </Button>
            </Box>

            {readOnly && (
              <p className="field-hint" data-testid="trash-readonly-note">{t('ⓘ 只读管理员视角：恢复/清除/清空是管理面写操作（system:write，仅全量 admin）；浏览只读，服务端 403 兜底。')}              </p>
            )}

            {listing.status === 'loading' && <Skeleton lines={5} />}
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
              <Table size="small" data-testid="trash-list">
                <TableHead>
                  <TableRow>
                    <TableCell component="th" scope="col">{t('名称')}</TableCell>
                    <TableCell component="th" scope="col">{t('类型')}</TableCell>
                    <TableCell component="th" scope="col">{t('大小')}</TableCell>
                    <TableCell component="th" scope="col">{t('修改时间')}</TableCell>
                    <TableCell component="th" scope="col">{adminWrite && !readOnly ? t('操作') : ''}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {listing.data.map((node) => (
                    <TableRow
                      key={node.path}
                      hover
                      selected={selected?.path === node.path}
                      data-testid={`trash-row-${node.path}`}
                      onClick={() => setSelected(node)}
                      sx={{ cursor: 'pointer' }}
                    >
                      <TableCell>
                        {node.folder ? (
                          <MuiLink
                            component="button"
                            type="button"
                            className="mono"
                            lang="en"
                            onClick={(e) => {
                              e.stopPropagation()
                              navigateInto(node.path)
                            }}
                            title={t('进入目录')}
                            sx={{ fontFamily: 'var(--bf-mono)' }}
                          >
                            ▸ {node.name}
                          </MuiLink>
                        ) : (
                          <span className="mono" lang="en">{node.name}</span>
                        )}
                        <CopyButton value={`${TRASH_REPO}/${node.path}`} label={t('路径 {v1}', { v1: node.name })} />
                      </TableCell>
                      <TableCell>
                        <Chip size="small" className="badge neutral" label={node.folder ? t('目录') : t('文件')} />
                      </TableCell>
                      <TableCell className="mono">{node.size === null ? '—' : formatBytes(node.size)}</TableCell>
                      <TableCell className="mono">{node.lastModified || '—'}</TableCell>
                      <TableCell>
                        {adminWrite && !readOnly && (
                          <span style={{ display: 'inline-flex', gap: 4 }}>
                            <Button
                              variant="outlined"
                              size="small"
                              disabled={busy}
                              data-testid={`trash-restore-${node.path}`}
                              onClick={(e) => {
                                e.stopPropagation()
                                void doRestore(node)
                              }}
                            >{t('恢复…')}                            </Button>
                            <Button
                              variant="outlined"
                              color="error"
                              size="small"
                              disabled={busy}
                              data-testid={`trash-clean-${node.path}`}
                              onClick={(e) => {
                                e.stopPropagation()
                                void doClean(node)
                              }}
                            >{t('清除')}                            </Button>
                          </span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </Paper>

          {/* 选中条目详情：属性五元组（AC1 断言面） */}
          {selected && (
            <Paper component="section" aria-label={t('条目详情')} sx={{ p: 2, pb: 1.5, mb: 2 }} data-testid="trash-detail">
              <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>{t('条目详情（属性五元组）')}              </Typography>
              <div className="kv">
                <span className="k">{t('路径')}</span>
                <span className="mono" lang="en">{TRASH_REPO}/{selected.path}</span>
                <CopyButton value={`${TRASH_REPO}/${selected.path}`} label={t('回收站路径')} />
              </div>
              <div className="kv">
                <span className="k">{t('类型 / 大小')}</span>
                <span>
                  {selected.folder ? t('目录') : t('文件')} ·{' '}
                  <span className="mono">{selected.size === null ? '—' : formatBytes(selected.size)}</span>
                </span>
              </div>
              {selected.sha256 && (
                <div className="kv">
                  <span className="k">sha256</span>
                  <span className="mono" lang="en">{selected.sha256.slice(0, 12)}…</span>
                  <CopyButton value={selected.sha256} label="sha256" />
                </div>
              )}
              {detail.status === 'loading' && <Skeleton lines={4} />}
              {detail.status === 'error' && detail.error && (
                <p className="field-error" role="alert">{t('五元组读取失败：')}{detail.error.message}
                </p>
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
                    <p className="field-hint" style={{ marginBottom: 0 }}>{t('该节点无 trash.* 标记（裸行——捕获后、打标前崩溃的降级形态；保留期按行龄回退判定）。')}                    </p>
                  )}
                  {/* 随行原属性如实呈现（捕获时原属性随 move 保留——非 trash.* 键） */}
                  {Object.entries(detail.data)
                    .filter(([k]) => !k.startsWith('trash.'))
                    .map(([k, vs]) => (
                      <div className="kv" key={`x-${k}`}>
                        <span className="k">
                          <span className="mono" lang="en">{k}</span>{t('（随行属性）')}                        </span>
                        <span className="mono" lang="en">{vs.join(', ')}</span>
                        <CopyButton value={vs.join(', ')} label={t('属性 {k}', { k: k })} />
                      </div>
                    ))}
                </>
              )}
            </Paper>
          )}

          {/* 最近一次 empty/clean 的清剿摘要 */}
          {summary && (
            <Alert severity="success" icon={false} data-testid="trash-summary" sx={{ mb: 2 }}>
              <div className="headline">{t('清剿完成')}</div>
              <div className="kv">
                <span className="k">{t('移除')}</span>
                <span className="mono">{formatCount(summary.removed)} {t('项')}</span>
              </div>
              <div className="kv">
                <span className="k">{t('文件 / 目录')}</span>
                <span className="mono">{formatCount(summary.files)} / {formatCount(summary.folders)}</span>
              </div>
              <div className="kv">
                <span className="k">{t('字节')}</span>
                <span className="mono">{formatBytes(summary.bytes)}</span>
              </div>
              <div className="text-2" style={{ marginTop: 4 }}>{t('对应 blob 离开 GC 引用集，由常态 GC（宽限期后）回收——清剿本身不物理删 blob。')}              </div>
            </Alert>
          )}

          {opError && (
            <Alert severity="error" icon={false} role="alert" sx={{ mb: 2 }}>
              <div className="headline">
                <span aria-hidden="true">✗</span> {t('操作失败（HTTP')} {opError.status || t('网络')}{t('）')}                {opError.status === 403 ? t('——trash 族门为 system:write（仅全量 admin）；license 槽锁定时为 403 + X-Binflow-License-Required') : ''}
              </div>
              <pre lang="en">{opError.raw || opError.message}</pre>
            </Alert>
          )}

          {/* 危险区：清空整罐（P5） */}
          {adminWrite && !readOnly && (
            <Paper
              component="section"
              className="danger-zone"
              variant="outlined"
              sx={{ p: 2, pb: 1.5, mb: 2, borderColor: 'error.main' }}
            >
              <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>{t('危险区：清空回收站')}              </Typography>
              <p className="field-hint" style={{ marginTop: 0 }}>{t('永久清除罐内')}<b>{t('全部')}</b>{t('条目（含保留期内未到期项）。输入 EMPTY 强确认。')}              </p>
              <Button
                variant="outlined"
                color="error"
                size="small"
                disabled={busy || canKnownEmpty}
                onClick={() => void doEmpty()}
                data-testid="trash-empty"
                title={t('清空整个回收站（不可撤销）')}
              >{t('清空回收站…')}              </Button>
            </Paper>
          )}
        </>
      )}
    </div>
  )
}
