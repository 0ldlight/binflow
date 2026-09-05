import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, errText, isReadOnlyAdmin } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { TransferBox } from './TransferBox'
import { PermSummaryTable } from './widgets'
import {
  getGroupWithUsers,
  grantsOfGroup,
  listPermissionTargets,
  listUsers,
  putGroup,
  updateUser,
  validateGroupName,
} from './api'
import { tr } from '../../i18n'

const t = tr('security')

// 组路由表单页（T-453 / FR-145.1，断言反转④——Q5 出口①路由化）：
// /admin/security/groups/new（创建）与 /admin/security/groups/:name/edit
// （编辑）深链整页表单，对位 Artifactory 7.161.20（2026-09-04 活体复核
// /ui/admin/management/groups/new：整页路由表单，字段 Group Name /
// Description / External ID / 管理位复选族 / Auto Join / 成员用户选择列表，
// 页脚 Cancel | Reset〔初始禁置〕| Save〔初始禁置〕）。
// GroupsPage 列表内联展开卡（创建 + 编辑同卡）随本票退役（B-2.15 / E5）。
//
// 字段域裁定（既有口径维持，不伪造 External ID / 管理位 / Auto Join）：
// External ID / Platform Administrator / Manage Resources / Manage Webhook /
// Automatically Join——BinFlow 组模型无对位域（console-m8 §6.10「无外部组
// 模型」+ rbac-model §5 组不承载角色语义），不建不做缺位登记。
//
// 成员单源（ADR-0030 K19/§14.1 E5，随内联卡迁移）：候选全集 = E2 users
// 列表内嵌 groups[] 投影；编辑态选区 = E5 ?includeUsers=true 按需单组读
// （比 E2 快照新鲜）；成员落盘仍经各用户部分更新臂（组侧写端点无票承载）。
//
// 页脚 Reset：用户/组表单按 7.161 保留（与 T-439 建仓表单移除重置钮的
// Q9 处置为页面级差异化配置，差异留痕见 parity 册）。

interface MembershipSnapshot {
  userGroups: Record<string, string[]>
}

/** E2 列表 → user→groups 索引（候选全集 + 成员落盘的当前组集） */
function snapshotFromUsers(users: readonly { name: string; groups: string[] }[]): MembershipSnapshot {
  const userGroups: Record<string, string[]> = {}
  for (const u of users) userGroups[u.name] = u.groups
  return { userGroups }
}

export default function GroupFormPage({ mode }: { mode: 'create' | 'edit' }) {
  const { name: routeName = '' } = useParams<{ name: string }>()
  const name = mode === 'edit' ? routeName : ''
  const { session } = useAuth()
  const navigate = useNavigate()
  const readOnly = isReadOnlyAdmin(session)
  const toast = useToast()
  const editMode = mode === 'edit'

  // 数据面（编辑态三请求 / 创建态一请求）：E2 users（候选全集 + 成员落盘
  // 依据）+ E5 单组读（编辑态选区）+ 权限 targets（编辑态矩阵）
  const users = useAsync(listUsers, [])
  const e5 = useAsync(
    () => (editMode ? getGroupWithUsers(name) : Promise.resolve(null)),
    [editMode, name],
  )
  const targets = useAsync(listPermissionTargets, [])

  const snapshot = users.status === 'ok' && users.data ? snapshotFromUsers(users.data) : null
  const e5Detail = e5.status === 'ok' && e5.data ? e5.data : null
  const [groupName, setGroupName] = useState('')
  const [description, setDescription] = useState('')
  // 编辑态选区 = E5 userNames；null = 取数中。创建态空选集。
  const [members, setMembers] = useState<string[] | null>(editMode ? null : [])
  // E5 到达即种入表单（render-time seeding——setState-in-effect 反模式规避；
  // 重试/刷新换新 detail 引用时重种，用户后续编辑不被覆盖直到新数据到达）
  const [seeded, setSeeded] = useState<typeof e5Detail>(null)
  if (e5Detail && e5Detail !== seeded) {
    setSeeded(e5Detail)
    setDescription(e5Detail.description)
    setMembers(e5Detail.userNames)
  }
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  const initialMembers = e5Detail?.userNames ?? []
  const membersReady = !editMode || members !== null
  const nameErr = editMode ? null : validateGroupName(groupName.trim())
  const memberAdded = (members ?? []).filter((m) => !initialMembers.includes(m))
  const memberRemoved = initialMembers.filter((m) => !(members ?? []).includes(m))
  const canSubmit = (editMode || (groupName.trim() !== '' && nameErr === null)) && membersReady && !readOnly
  // dirty（7.161 活体：Reset 初始禁置——dirty 门）：创建态 pristine = 空
  // 名/空描述/零成员；编辑态 = 与 E5 回显的偏差（选区未种入前不 dirty）
  const dirty = editMode
    ? e5Detail !== null && (description !== e5Detail.description || memberAdded.length > 0 || memberRemoved.length > 0)
    : groupName.trim() !== '' || description.trim() !== '' || (members ?? []).length > 0
  const backToList = () => navigate('/admin/security/groups')

  /** 成员落盘：逐用户替换组集（add → 追加本组；remove → 去掉本组）。
   *  幂等护栏：E5 视图与 E2 快照之间带外并发下，已入组用户跳过追加
   *  （避免重复组名）；移除臂的 filter 天然幂等。 */
  const applyMembership = async (group: string) => {
    if (!snapshot) return undefined
    const failures: string[] = []
    for (const m of memberAdded) {
      const current = snapshot.userGroups[m]
      if (!current) {
        failures.push(m)
        continue
      }
      if (current.includes(group)) continue // 视图竞窗外已在组——收敛而非重复追加
      try {
        await updateUser(m, { groups: [...current, group] })
      } catch {
        failures.push(m)
      }
    }
    for (const m of memberRemoved) {
      const current = snapshot.userGroups[m]
      if (!current) {
        failures.push(m)
        continue
      }
      try {
        await updateUser(m, { groups: current.filter((g) => g !== group) })
      } catch {
        failures.push(m)
      }
    }
    return failures
  }

  const submit = async () => {
    setServerError(null)
    setSubmitting(true)
    const target = editMode ? name : groupName.trim()
    try {
      await putGroup(target, description.trim())
      let failures: string[] = []
      if (memberAdded.length > 0 || memberRemoved.length > 0) {
        failures = (await applyMembership(target)) ?? []
      }
      if (failures.length > 0) {
        // 部分成员变更未落盘：组本体已保存（PUT 先成），失败名单行内呈现
        setServerError(
          new ApiError(
            0,
            t('部分成员变更未落盘（{v1}）——组本体已保存；请重试或到用户编辑器逐个处理。', { v1: failures.join(', ') }),
          ),
        )
        return
      }
      if (editMode) {
        toast.success(
          memberAdded.length === 0 && memberRemoved.length === 0
            ? t('组 {target} 的描述已更新', { target: target })
            : t('组 {target} 已更新（成员 +{v1} −{v2}）', { target: target, v1: memberAdded.length, v2: memberRemoved.length }),
        )
      } else {
        toast.success(memberAdded.length > 0 ? t('组 {target} 已创建（成员 +{v1}）', { target: target, v1: memberAdded.length }) : t('组 {target} 已创建', { target: target }))
      }
      // 创建-列表-编辑闭环：保存后回列表（7.161 同姿）
      backToList()
    } catch (err) {
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  // 编辑态 E5 异常态：404 = 组已被带外删除（空态回列表）；403 = 非管理
  // 员深链（无权限卡——L4 收敛，服务端是唯一守门）；其余错误卡可重试
  if (editMode && (e5.status === 'error' || e5.status === 'forbidden') && e5.error) {
    return (
      <div data-testid="group-form-page">
        {e5.status === 'forbidden' ? (
          <EmptyState message={t('无权限访问组管理')} hint={e5.error.message} />
        ) : e5.error.status === 404 ? (
          <EmptyState
            message={t('组 {name} 不存在', { name: name })}
            action={
              <Button variant="outlined" size="small" component={Link} to="/admin/security/groups">{t('← 返回组列表')}              </Button>
            }
          />
        ) : (
          <ErrorCard error={e5.error} onRetry={e5.reload} />
        )}
      </div>
    )
  }

  const userItems = snapshot
    ? Object.keys(snapshot.userGroups)
        .sort()
        .map((u) => ({ name: u, note: snapshot.userGroups[u].length > 0 ? snapshot.userGroups[u].join(', ') : undefined }))
    : []
  const grants = editMode && targets.status === 'ok' && targets.data ? grantsOfGroup(targets.data, name) : []

  return (
    <div data-testid="group-form-page">
      <div className="page-header">
        <h2>
          {editMode ? (
            <>{t('编辑组 ·')} <span className="mono" lang="en">{name}</span>{' '}
              <CopyButton value={name} label={t('组名 {name}', { name: name })} />
            </>
          ) : (
            t('新建组')
          )}
        </h2>
        <Button variant="outlined" size="small" component={Link} to="/admin/security/groups">{t('← 返回列表')}        </Button>
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="group-form-readonly-note">{t('ⓘ 只读管理员（readonly_admin）视角：组创建/编辑是管理面写操作，本页为只读呈现 （服务端 403 兜底，UI 不代持判定）。')}        </p>
      )}

      <section className="card inline-form" data-testid="group-form" aria-label={editMode ? t('编辑组') : t('新建组')}>
        {/* T-384 节锚（v1.20 批）随路由化迁本页：组面两节——载体迁移零改名 */}
        <div className="form-section" data-testid="group-form-section-settings">
          <h4>{t('组设置')}</h4>
          <div className="field">
            <label htmlFor="gf-name">{t('组名')}{editMode ? t('（不可变）') : ' *'}</label>
            <TextField
              id="gf-name"
              size="small"
              value={editMode ? name : groupName}
              disabled={editMode || readOnly}
              onChange={(e) => setGroupName(e.target.value)}
              placeholder="qa-team"
              error={!!nameErr}
              sx={{ width: 320 }}
              slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'group-form-name', lang: 'en' } }}
            />
            {nameErr ? (
              <p className="field-error" role="alert">
                {nameErr}
              </p>
            ) : (
              <p className="field-hint">{t('[a-z][a-z0-9._-]*，≤64；保留字 anonymous / _system_ 拒绝。')}</p>
            )}
          </div>
          <div className="field">
            <label htmlFor="gf-desc">{t('描述')}</label>
            <TextField
              id="gf-desc"
              size="small"
              multiline
              minRows={2}
              value={description}
              disabled={readOnly}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('用途、负责人…')}
              sx={{ width: 480 }}
              slotProps={{ htmlInput: { 'data-testid': 'group-form-description' } }}
            />
          </div>
        </div>
        <div className="form-section" data-testid="group-form-section-members">
          <h4>{t('成员')}</h4>
          <p className="field-hint">{t('勾选即加入（右列）；保存后即时生效——移出即失去该组授权，无需重登。')}</p>
          {!snapshot ? (
            <Skeleton lines={2} />
          ) : !membersReady ? (
            <Skeleton lines={2} />
          ) : (
            <div data-testid="group-form-members">
              <TransferBox
                items={userItems}
                selected={members ?? []}
                disabled={readOnly}
                onToggle={(u, next) => setMembers((p) => (next ? [...(p ?? []), u] : (p ?? []).filter((x) => x !== u)))}
                availableLabel={t('可选用户')}
                selectedLabel={t('已选成员')}
                itemTestid={(u) => `group-form-member-${u}`}
              />
            </div>
          )}
        </div>
        {editMode && (
          <div className="form-section">
            <h4>{t('组权限矩阵')}</h4>
            <p className="field-hint">{t('只读汇总（来源 = 引用本组的 permission target）——变更入口在权限编辑器。')}</p>
            {targets.status === 'loading' && <Skeleton lines={3} />}
            {targets.status === 'ok' && (
              <PermSummaryTable
                rows={grants}
                rowTestidPrefix="group-perm"
                emptyHint={t('该组未被任何 permission target 引用——组的授权经 target 生效。')}
              />
            )}
            {targets.status !== 'loading' && targets.status !== 'ok' && (
              <p className="field-hint">{t('权限汇总不可用（')}{targets.error?.message}{t('）。')}</p>
            )}
          </div>
        )}
        {serverError && (
          <Alert severity="error">
            <div className="headline">{editMode ? t('保存失败') : t('创建失败')}</div>
            <div className="raw">{serverError.message}</div>
          </Alert>
        )}
        <div className="form-actions">
          {/* 页脚三联（7.161 活体：Cancel 最左 / Reset〔初始禁置〕/ Save 右） */}
          <Button variant="outlined" size="small" component={Link} to="/admin/security/groups" data-testid="group-form-cancel">{t('取消')}          </Button>
          <Button
            variant="outlined"
            size="small"
            disabled={!dirty || submitting}
            data-testid="group-form-reset"
            onClick={() => {
              setGroupName('')
              setDescription(e5Detail?.description ?? '')
              setMembers(initialMembers)
            }}
          >{t('重置')}          </Button>
          <Button
            variant="contained"
            size="small"
            disabled={!canSubmit || !dirty || submitting}
            title={readOnly ? t('只读管理员：组创建/编辑是管理面写操作（服务端 403）') : undefined}
            onClick={() => void submit()}
            data-testid="group-form-submit"
          >
            {submitting ? t('保存中…') : editMode ? t('保存') : t('创建组')}
          </Button>
        </div>
      </section>
    </div>
  )
}
