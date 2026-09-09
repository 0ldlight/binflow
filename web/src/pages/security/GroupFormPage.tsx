// 组路由表单页（T-453 / FR-145.1——P3 新栈重写）：/admin/security/groups/new
// （创建）与 /:name/edit（编辑）深链整页表单，对位 7.161.20。
// - 字段域裁定维持：不伪造 External ID / 管理位 / Auto Join（BinFlow 组
//   模型无对位域）；
// - 成员单源（ADR-0030 K19/E5）：候选全集 = E2 users 投影；编辑态选区 =
//   E5 ?includeUsers=true 按需单组读；成员落盘经各用户部分更新臂（幂等
//   护栏防重复入组；失败名单行内呈现）；
// - 页脚 Cancel | Reset（初始禁置）| Save（dirty 门）。
// 锚族原样：group-form-page/group-form(-section-settings|-section-members|
// -name|-description|-members|-member-<name>|-cancel|-reset|-submit)/
// group-perm-matrix/group-form-readonly-note。
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { AlertBox } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { TextInput, TextArea } from '@/components/layout/fields'
import { TransferBox } from '@/components/layout/transfer-box'
import { toast } from '@/lib/toast'
import { ApiError, errText, isReadOnlyAdmin } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import './security.css'
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
import { tr } from '@/i18n'

const t = tr('security')

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
  const editMode = mode === 'edit'

  // 数据面（编辑态三请求 / 创建态一请求）：E2 users + E5 单组读 + targets
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
  // E5 到达即种入表单（render-time seeding——setState-in-effect 反模式规避）
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
  // dirty（7.161 活体：Reset 初始禁置——dirty 门）
  const dirty = editMode
    ? e5Detail !== null && (description !== e5Detail.description || memberAdded.length > 0 || memberRemoved.length > 0)
    : groupName.trim() !== '' || description.trim() !== '' || (members ?? []).length > 0
  const backToList = () => navigate('/admin/security/groups')

  /** 成员落盘：逐用户替换组集（幂等护栏——竞窗外已在组的跳过追加） */
  const applyMembership = async (group: string) => {
    if (!snapshot) return undefined
    const failures: string[] = []
    for (const m of memberAdded) {
      const current = snapshot.userGroups[m]
      if (!current) {
        failures.push(m)
        continue
      }
      if (current.includes(group)) continue
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
          new ApiError(0, t('部分成员变更未落盘（{v1}）——组本体已保存；请重试或到用户编辑器逐个处理。', { v1: failures.join(', ') })),
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
      backToList()
    } catch (err) {
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  // 编辑态 E5 异常态：404 = 组已被带外删除；403 = 非管理员深链；其余可重试
  if (editMode && (e5.status === 'error' || e5.status === 'forbidden') && e5.error) {
    return (
      <div data-testid="group-form-page">
        {e5.status === 'forbidden' ? (
          <EmptyState message={t('无权限访问组管理')} hint={e5.error.message} />
        ) : e5.error.status === 404 ? (
          <EmptyState
            message={t('组 {name} 不存在', { name: name })}
            action={
              <ButtonAsChild variant="outline" size="sm">
                <Link to="/admin/security/groups">{t('← 返回组列表')}</Link>
              </ButtonAsChild>
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
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">
          {editMode ? (
            <>
              {t('编辑组 ·')} <span className="font-mono" lang="en">{name}</span>{' '}
              <CopyButton value={name} label={t('组名 {name}', { name: name })} />
            </>
          ) : (
            t('新建组')
          )}
        </h2>
        <ButtonAsChild variant="outline" size="sm" className="ml-auto">
          <Link to="/admin/security/groups">{t('← 返回列表')}</Link>
        </ButtonAsChild>
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="group-form-readonly-note">
          {t('ⓘ 只读管理员（readonly_admin）视角：组创建/编辑是管理面写操作，本页为只读呈现 （服务端 403 兜底，UI 不代持判定）。')}
        </p>
      )}

      <section className="card inline-form" data-testid="group-form" aria-label={editMode ? t('编辑组') : t('新建组')}>
        <div className="form-section" data-testid="group-form-section-settings">
          <h4>{t('组设置')}</h4>
          <div className="field">
            <label htmlFor="gf-name">{t('组名')}{editMode ? t('（不可变）') : ' *'}</label>
            <TextInput
              id="gf-name"
              mono
              lang="en"
              value={editMode ? name : groupName}
              disabled={editMode || readOnly}
              onChange={(e) => setGroupName(e.target.value)}
              placeholder="qa-team"
              aria-invalid={!!nameErr || undefined}
              data-testid="group-form-name"
            />
            {nameErr ? (
              <p className="field-error" role="alert">{nameErr}</p>
            ) : (
              <p className="field-hint">{t('[a-z][a-z0-9._-]*，≤64；保留字 anonymous / _system_ 拒绝。')}</p>
            )}
          </div>
          <div className="field">
            <label htmlFor="gf-desc">{t('描述')}</label>
            <TextArea
              id="gf-desc"
              className="max-w-[480px]"
              value={description}
              disabled={readOnly}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('用途、负责人…')}
              data-testid="group-form-description"
            />
          </div>
        </div>
        <div className="form-section" data-testid="group-form-section-members">
          <h4>{t('成员')}</h4>
          <p className="field-hint">{t('勾选即加入（右列）；保存后即时生效——移出即失去该组授权，无需重登。')}</p>
          {!snapshot || !membersReady ? (
            <StateSkeleton lines={2} />
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
            {targets.status === 'loading' && <StateSkeleton lines={3} />}
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
          <AlertBox severity="error">
            <div className="font-medium">{editMode ? t('保存失败') : t('创建失败')}</div>
            <div className="mt-1 break-all font-mono text-aux opacity-90">{serverError.message}</div>
          </AlertBox>
        )}
        <div className="form-actions">
          <Button variant="outline" size="sm" data-testid="group-form-cancel" onClick={() => navigate('/admin/security/groups')}>
            {t('取消')}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!dirty || submitting}
            data-testid="group-form-reset"
            onClick={() => {
              setGroupName('')
              setDescription(e5Detail?.description ?? '')
              setMembers(initialMembers)
            }}
          >
            {t('重置')}
          </Button>
          <Button
            size="sm"
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
