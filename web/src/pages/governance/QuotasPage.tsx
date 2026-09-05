import { useState } from 'react'
import { Link } from 'react-router-dom'

import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import LinearProgress from '@mui/material/LinearProgress'
import MuiSkeleton from '@mui/material/Skeleton'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { getRepositories, isReadOnlyAdmin } from '../../lib/api'
import type { RepoListItem } from '../../lib/api'
import { errText } from '../../lib/api'
import { monoInputSx } from '../../lib/muiAtoms'
import { formatBytes } from '../../lib/format'
import { buildLocalQuotaBody, cfgNum, getRepoDetail, getRepoUsage, updateRepo } from '../../lib/repos'
import type { RepoUsage } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'
import { tr } from '../../i18n'

const tt = tr('governance')

// 配额页（console-ux §4.11 配额行 / §5.3；T-102 AC③）：
// - 每仓一行：key（mono 链接）/ 类型 / 已用 / 配额 / 水位条（≥80% 黄、
//   ≥100% 红——water-bar 样式与仓库详情页同源）/ 行内编辑 + 跳转仓库设置。
// - usage 逐仓独立拉取（virtual 无自身内容不请求，显示 —）。
// - 行内编辑**仅 local 仓**（governance 字段只在 local 的 config 透传链上
//   有效，remote/virtual 的规范化会丢弃）；保存走 POST 全量替换语义——
//   buildLocalQuotaBody 共享件（lib/repos，T-244 自本页与仓库详情页的
//   双份副本抽取合一）。
// - 非 admin：仓库列表 403 → 单张无权限卡（§3.6.3 L2）。

function WaterBar({ usage }: { usage: RepoUsage }) {
  const quota = usage.quotaBytes
  if (quota <= 0) {
    return (
      <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{tt('不限（quotaBytes 0）')}      </span>
    )
  }
  const pct = Math.min(100, (usage.usedBytes / quota) * 100)
  const cls = usage.usedBytes >= quota ? 'full' : pct >= 80 ? 'warn' : ''
  return (
    <div className="quota-bar" data-testid={`quota-bar-${usage.repo}`}>
      {/* T-344 批 C：水位条换 MUI LinearProgress（根续挂 .water-bar + warn/
          full 类名——governance spec toHaveClass 钩子；≥80% warning、≥100%
          error，aria 属性 LinearProgress 内建） */}
      <LinearProgress
        className={`water-bar${cls ? ` ${cls}` : ''}`}
        variant="determinate"
        value={Math.max(usage.usedBytes > 0 ? 2 : 0, Math.round(pct))}
        color={cls === 'full' ? 'error' : cls === 'warn' ? 'warning' : 'primary'}
        aria-label={tt('{v1} 配额水位', { v1: usage.repo })}
        sx={{ flex: 1, borderRadius: 'var(--bf-r-sm)' }}
      />
      <span className={`pct${cls ? ` ${cls}` : ''}`}>
        {pct.toFixed(0)}%{usage.usedBytes >= quota ? tt(' 满') : pct >= 80 ? tt(' 高') : ''}
      </span>
    </div>
  )
}

function QuotaRow({ repo, onChanged }: { repo: RepoListItem; onChanged: () => void }) {
  const toast = useToast()
  // readonly_admin（M7 FR-66）：配额写是 repoManage write——编辑入口禁用
  //（服务端 403 兜底；普通 user 在列表层已被 403 收敛，走不进本行）
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  // virtual 不持有自身内容：不请求 usage（不伪造 0）
  const usage = useAsync(
    () => (repo.type === 'virtual' ? Promise.resolve(null) : getRepoUsage(repo.key)),
    [repo.key, repo.type],
  )

  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [editErr, setEditErr] = useState<string | null>(null)

  const local = repo.type === 'local'
  const u = usage.data

  const startEdit = () => {
    setDraft(String(u?.quotaBytes ?? cfgNum(repo.configuration, 'quotaBytes') ?? 0))
    setEditErr(null)
    setEditing(true)
  }

  const save = async (): Promise<void> => {
    const t = draft.trim()
    if (!/^\d+$/.test(t) || Number(t) > Number.MAX_SAFE_INTEGER) {
      setEditErr(tt('需为非负整数（字节）；0 = 不限'))
      return
    }
    setSaving(true)
    setEditErr(null)
    try {
      // 全量替换语义：先取详情再重组完整 body（只覆写 quotaBytes）
      const detail = await getRepoDetail(repo.key)
      const text = await updateRepo(repo.key, buildLocalQuotaBody(detail, Number(t)))
      toast.success(text)
      setEditing(false)
      usage.reload()
      onChanged()
    } catch (err) {
      setEditErr(errText(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <TableRow data-testid={`quota-row-${repo.key}`} hover>
      <TableCell>
        <Link className="row-link mono" to={`/admin/repositories/${repo.key}`} lang="en">
          {repo.key}
        </Link>{' '}
        <CopyButton value={repo.key} label={tt('仓库 key {v1}', { v1: repo.key })} />
      </TableCell>
      <TableCell>
        <Chip size="small" className="badge neutral" label={repo.type} lang="en" />
      </TableCell>
      <TableCell>
        {repo.type === 'virtual' ? (
          <span className="text-muted">{tt('—（聚合视图，无自身内容）')}</span>
        ) : usage.status === 'loading' ? (
          <MuiSkeleton sx={{ display: 'inline-block', width: 48, height: 10, verticalAlign: 'middle' }} role="progressbar" aria-label={tt('用量加载中')} />
        ) : usage.status === 'ok' && u ? (
          <span className="mono">{formatBytes(u.usedBytes)}</span>
        ) : (
          <span className="text-muted" title={usage.error?.message ?? tt('用量不可用')}>
            —
          </span>
        )}
      </TableCell>
      <TableCell>
        {editing ? (
          <>
            <TextField
              size="small"
              inputMode="numeric"
              autoComplete="off"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              sx={{ ...monoInputSx, width: 140, display: 'inline-flex' }}
              slotProps={{
                htmlInput: {
                  'aria-label': tt('{v1} 的新配额（字节）', { v1: repo.key }),
                  'data-testid': `quota-input-${repo.key}`,
                  lang: 'en',
                  className: 'mono',
                },
              }}
            />{' '}
            <span className="text-muted" style={{ fontSize: 'var(--bf-fs-aux)' }}>
              {/^\d+$/.test(draft.trim()) && Number(draft) > 0 ? `≈ ${formatBytes(Number(draft))}` : tt('0 = 不限')}
            </span>
          </>
        ) : local ? (
          <span className="mono">{u ? (u.quotaBytes > 0 ? formatBytes(u.quotaBytes) : tt('0（不限）')) : '—'}</span>
        ) : (
          <span className="text-muted">{tt('—（仅 local 仓支持）')}</span>
        )}
      </TableCell>
      <TableCell className="quota-bar-cell">
        {usage.status === 'ok' && u ? <WaterBar usage={u} /> : <span className="text-muted">—</span>}
      </TableCell>
      <TableCell>
        {editing ? (
          <>
            <Button
              variant="outlined"
              size="small"
             
              disabled={saving}
              onClick={() => void save()}
              data-testid={`quota-save-${repo.key}`}
            >
              {saving ? tt('保存中…') : tt('保存')}
            </Button>{' '}
            <Button
              variant="outlined"
              size="small"
             
              disabled={saving}
              onClick={() => setEditing(false)}
            >{tt('取消')}            </Button>
            {editErr && (
              <div className="field-error" role="alert">
                {editErr}
              </div>
            )}
          </>
        ) : (
          <>
            {local &&
              usage.status === 'ok' &&
              u && (
                <Button
                  variant="outlined"
                  size="small"
                 
                  disabled={readOnly}
                  title={readOnly ? tt('只读管理员：配额写是管理面写操作（服务端 403 兜底）') : undefined}
                  onClick={startEdit}
                  data-testid={`quota-edit-${repo.key}`}
                >{tt('编辑上限')}                </Button>
              )}{' '}
            <Link className="text-2" to={`/admin/repositories/${repo.key}/edit`} style={{ fontSize: 'var(--bf-fs-aux)' }}>{tt('仓库设置 →')}            </Link>
          </>
        )}
      </TableCell>
    </TableRow>
  )
}

export default function QuotasPage() {
  // L2 收敛走 repos 403（整页主数据面），无需 whoami 预收敛
  const repos = useAsync(getRepositories, [])
  const list = repos.data ?? []
  // readonly_admin（M7 §7.3 推广）：行内编辑已按行禁用（T-218），页级
  // 再给只读注记——配额写是 repoManage write，服务端 403 兜底
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)

  return (
    <div data-testid="quotas-page">
      <div className="page-header">
        <h2>{tt('配额')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{tt('水位 ≥80% 黄 · ≥100% 红（此后写入 413）')}        </span>
      </div>
      {readOnly && (
        <p className="admin-note" data-testid="quotas-readonly-note">{tt('只读管理员（readonly_admin）：配额读写面可见，行内编辑已禁用—— 配额写是管理面写操作（repoManage write），提交会被服务端 403 拒绝。')}        </p>
      )}

      {repos.status === 'loading' && <Skeleton lines={8} />}
      {repos.status === 'error' && repos.error && <ErrorCard error={repos.error} onRetry={repos.reload} />}
      {repos.status === 'forbidden' && repos.error && (
        <EmptyState
          message={tt('无权限查看配额')}
          hint={tt('仓库列表与用量端点为管理员视图（GET /api/repositories 仅 admin）。')}
        />
      )}
      {repos.status === 'ok' &&
        (list.length === 0 ? (
          <EmptyState
            message={tt('还没有仓库')}
            hint={tt('配额在创建 local 仓库时或仓库设置页配置（quotaBytes，0 = 不限）')}
            action={
              <Button variant="contained" size="small" component={Link} to="/admin/repositories/new">{tt('创建第一个仓库')}              </Button>
            }
          />
        ) : (
          <Table>
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">{tt('仓库')}</TableCell>
                <TableCell component="th" scope="col">{tt('类型')}</TableCell>
                <TableCell component="th" scope="col">{tt('已用')}</TableCell>
                <TableCell component="th" scope="col">{tt('配额')}</TableCell>
                <TableCell component="th" scope="col">{tt('水位')}</TableCell>
                <TableCell component="th" scope="col">{tt('操作')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {list.map((r) => (
                <QuotaRow key={r.key} repo={r} onChanged={repos.reload} />
              ))}
            </TableBody>
          </Table>
        ))}
      <p className="field-hint" style={{ marginTop: 12 }}>{tt('计量为 repo_usage.logical_bytes（与节点写入同事务）；quotaBytes 仅 local 仓生效， 超限写入原子拒绝（413 + quota.exceeded 审计）。')}      </p>
    </div>
  )
}
