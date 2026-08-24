import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, errText, getRepositories, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { PERM_ACTIONS, deletePermissionTarget, listGroups, listPermissionTargets, listPermissionTargetsManaged, listUsers, savePermissionTarget } from './api'
import type { PermAction } from './api'
import { evaluatePath } from './pathmatch'
import { buildTargetDiff, sameSnapshot } from './targetdiff'
import type { DiffLine, TargetSnapshot } from './targetdiff'
import { TransferBox } from './TransferBox'

// 权限 target 编辑器（T-241 重排，console-m8 §4.5/§6.11 + reverse §3.8/§4.9）：
//   [1] 目标信息（name 编辑态锁定 + 适用仓库 chips + 「添加/编辑仓库…」入口）
//   [2] 路径模式（只读摘要 + 模式测试器——灵魂件：逐条命中明细 + exclude
//       优先最终判定；判定向量与 internal/auth pathmatch 同源，本地求值
//       零端点）——pattern 的编辑面在两步资源对话框第 2 步
//   [3] 用户区块 / [4] 组区块：各自展开矩阵形态，动作四列
//       read/write/delete/manage（manage 为 M7 扩展第 4 列，§7.2——矩阵头
//       tooltip 说明其不隐含读写删；wire 全词形，回显按 r/w/d/m 序）
//   [5] 保存 = 变更摘要 diff 确认（§4.9[4] BinFlow 保留）→ POST create-or-replace
// 两步资源对话框（§3.3 C6 / §4.5，对齐 reverse §3.8「Edit Repositories」）：
//   ① 选择仓库（双列穿梭；Any Local/Any Remote 通配桶不建——BinFlow 契约
//      repos[] 必须是现存仓库名，服务端 400 unknown repository，无 ** 约定）
//   ② 设置模式（可选；include/exclude 逐行 chip）→ 确定 回填
// 危险区：删除 target（连带全部授权行，单事务）。
//
// 覆盖集语义（任务项 4，T-217 B1 / console-m8 §7.2/§7.10；T-259 起控制台
// 可达）：manage 持有者（非 admin）可编辑「引用仓库全部落在其 manage 覆盖集
// 内」的 target（POST 是 create-or-replace，校验 union(body, 存量) ⊆ 覆盖集
// ），超出即 403——UI 不自行判定覆盖集，仅：① 保存/删除的 403 服务端原文
// 行内如实呈现（form-error / toast）；② m-holder 的注水走 E6
// `?filter=manage`（条目字段与全量一致），列表外/部分覆盖的 target 不在
// 呈现面（服务端信息隔离）。
//
// m-holder 注水形态（§14.1.6「E6 + 主体名手动录入」）：仓库目录
// （/api/repositories）与用户/组枚举（/api/security/{users,groups}）维持
// CapSecurityRead/CapRepoRead 闭集——m-holder 会话一律 403，故编辑器的
// 添加位降级为**手动录入**（仓库名/主体名 text entry），存在性与覆盖集
// 由服务端终裁（unknown → 400；覆盖集外 → 403）。
//
// readonly_admin（M7 FR-66）：编辑器可见但全控件只读（security:read 过 GET，
// 写端点 403——服务端是唯一守门，UI 只呈现）。

type PrincipalMap = Record<string, PermAction[]>

interface EditorState {
  name: string
  repos: string[]
  includes: string[]
  excludes: string[]
  users: PrincipalMap
  groups: PrincipalMap
}

const CREATE_INITIAL: EditorState = {
  name: '',
  repos: [],
  includes: ['**'],
  excludes: [],
  users: {},
  groups: {},
}

function snapshotOf(s: EditorState): TargetSnapshot {
  return { repos: [...s.repos], includes: [...s.includes], excludes: [...s.excludes], users: s.users, groups: s.groups }
}

/** 矩阵单元格（§4.9[3]：testid 契约 perm-matrix-cell-<kind>-<principal>-<action>） */
function MatrixCell({
  kind,
  name,
  action,
  on,
  onToggle,
  disabled,
}: {
  kind: 'user' | 'group'
  name: string
  action: PermAction
  on: boolean
  onToggle: () => void
  disabled?: boolean
}) {
  return (
    <td>
      <label className="matrix-cell">
        <input
          type="checkbox"
          checked={on}
          disabled={disabled}
          onChange={onToggle}
          aria-label={`${kind === 'user' ? '用户' : '组'} ${name} 的 ${action} 权限`}
          data-testid={`perm-matrix-cell-${kind}-${name}-${action}`}
        />
      </label>
    </td>
  )
}

const FOCUSABLE = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'

/**
 * 两步资源对话框（console-m8 §3.3 C6 / §4.5；对齐 reverse §3.8 的
 * Edit Repositories 形态：① Select Repositories（双列穿梭）→ ② Set
 * Patterns (Optional) → OK 回填）。焦点圈进对话框 + Tab 循环 + Esc = 取消
 * （不回填）。draft 状态在打开时从表单初始化，确定 时一次性 onApply。
 */
function ResourceDialog({
  create,
  repoKeys,
  reposStatus,
  initialRepos,
  initialIncludes,
  initialExcludes,
  onApply,
  onClose,
}: {
  create: boolean
  repoKeys: readonly string[]
  reposStatus: 'loading' | 'ok' | 'error' | 'forbidden'
  initialRepos: string[]
  initialIncludes: string[]
  initialExcludes: string[]
  onApply: (repos: string[], includes: string[], excludes: string[]) => void
  onClose: () => void
}) {
  const [step, setStep] = useState<1 | 2>(1)
  const [repos, setRepos] = useState<string[]>([...initialRepos])
  const [includes, setIncludes] = useState<string[]>([...initialIncludes])
  const [excludes, setExcludes] = useState<string[]>([...initialExcludes])
  const [includeInput, setIncludeInput] = useState('')
  const [excludeInput, setExcludeInput] = useState('')
  const [repoEntry, setRepoEntry] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)

  // 焦点陷阱（ConfirmDialog 同款）：打开即聚焦取消（安全默认），Tab 循环，Esc = 取消
  useEffect(() => {
    cancelRef.current?.focus()
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab') return
      const nodes = Array.from(rootRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])
      if (nodes.length === 0) return
      const first = nodes[0]
      const last = nodes[nodes.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  const addPattern = (kind: 'includes' | 'excludes', raw: string) => {
    const v = raw.trim()
    if (v === '') return
    if (kind === 'includes') {
      setIncludes((p) => (p.includes(v) ? p : [...p, v]))
      setIncludeInput('')
    } else {
      setExcludes((p) => (p.includes(v) ? p : [...p, v]))
      setExcludeInput('')
    }
  }

  /** 手动录入仓库名（m-holder 分支）：目录 403 下的加入面——服务端终裁
   *  （unknown repository → 400；覆盖集外保存 → 403） */
  const addRepoEntry = () => {
    const v = repoEntry.trim()
    if (v === '') return
    setRepos((p) => (p.includes(v) ? p : [...p, v]))
    setRepoEntry('')
  }

  /** pattern 编辑列（§4.9 锚：perm-pattern-{input,add,remove}-{include|exclude}） */
  const renderPatternCol = (kind: 'includes' | 'excludes') => {
    const isIncl = kind === 'includes'
    const list = isIncl ? includes : excludes
    const setList = isIncl ? setIncludes : setExcludes
    const input = isIncl ? includeInput : excludeInput
    const setInput = isIncl ? setIncludeInput : setExcludeInput
    const word = isIncl ? 'include' : 'exclude'
    return (
      <div className="pattern-col">
        <h4>{isIncl ? 'include patterns（命中任一即纳入）' : 'exclude patterns（命中任一即排除——优先于 include）'}</h4>
        {list.length === 0 && (
          <p className="empty">{isIncl ? '空 = 匹配全部路径（等价 **）' : '（无排除）'}</p>
        )}
        {list.map((p, i) => (
          <div key={p} className="pattern-chip" data-testid={`perm-pattern-${word}-${i}`}>
            <span className="val" lang="en">
              {p}
            </span>
            <button
              type="button"
              aria-label={`移除 ${p}`}
              onClick={() => setList((prev) => prev.filter((x) => x !== p))}
              data-testid={`perm-pattern-remove-${word}-${i}`}
            >
              ✕
            </button>
          </div>
        ))}
        <div className="pattern-add">
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addPattern(kind, input)
              }
            }}
            placeholder={isIncl ? 'ci-out/**' : 'ci-out/tmp/**'}
            aria-label={`添加 ${word} pattern`}
            data-testid={`perm-pattern-input-${word}`}
            lang="en"
          />
          <button type="button" className="btn" onClick={() => addPattern(kind, input)} data-testid={`perm-pattern-add-${word}`}>
            添加
          </button>
        </div>
      </div>
    )
  }

  return (
    <div
      className="modal-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={rootRef}
        className="modal perm-res-modal"
        role="dialog"
        aria-modal="true"
        aria-label={step === 1 ? '选择仓库' : '设置模式'}
        data-testid="perm-res-dialog"
      >
        <h2>{step === 1 ? (create ? '添加仓库' : '编辑仓库') : '设置模式（可选）'}</h2>
        <p className="perm-res-step" data-testid="perm-res-step">
          第 {step} 步，共 2 步
          {step === 1 ? ' · 选择此 target 适用的仓库（pattern 与主体在仓库范围内生效）' : ' · 模式作用于所选仓库内的制品路径（repo 相对路径；** 跨段 / * 段内）'}
        </p>
        <div className="modal-body perm-res-body">
          {step === 1 ? (
            <div data-testid="perm-res-repos">
              {reposStatus === 'error' ? (
                <p className="field-error">仓库列表不可用——关闭后重试（编辑器保存仍需至少一个仓库）。</p>
              ) : reposStatus === 'loading' ? (
                <p className="field-hint">仓库列表加载中…</p>
              ) : reposStatus === 'forbidden' ? (
                // m-holder（§14.1.6「E6 + 手动录入」）：仓库目录 403——已选
                // 仓可摘除（穿梭右列），新增走手动录入，服务端终裁
                <>
                  <TransferBox
                    items={[...new Set(repos)].map((k) => ({ name: k }))}
                    selected={repos}
                    onToggle={(k, next) => setRepos((p) => (next ? [...p, k] : p.filter((x) => x !== k)))}
                    availableLabel="可选仓库"
                    selectedLabel="已选仓库"
                    itemTestid={(k) => `perm-repo-pick-${k}`}
                  />
                  <div className="pattern-add">
                    <input
                      value={repoEntry}
                      onChange={(e) => setRepoEntry(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          addRepoEntry()
                        }
                      }}
                      placeholder="仓库名（服务端校验）"
                      aria-label="手动录入仓库名"
                      data-testid="perm-repo-entry-input"
                      lang="en"
                    />
                    <button
                      type="button"
                      className="btn"
                      disabled={repoEntry.trim() === ''}
                      onClick={addRepoEntry}
                      data-testid="perm-repo-entry-add"
                    >
                      添加仓库
                    </button>
                  </div>
                  <p className="admin-note">
                    ⓘ 仓库目录是管理面读端点（本会话 403）——无法浏览候选仓库；手动录入仓库名加入，服务端终裁
                    （unknown repository → 400；覆盖集外保存 → 403）。Artifactory 的 Any Local / Any Remote 通配桶不建：
                    repos[] 必须逐个指名现存仓库。
                  </p>
                </>
              ) : (
                <>
                  <TransferBox
                    items={repoKeys.map((k) => ({ name: k }))}
                    selected={repos}
                    onToggle={(k, next) => setRepos((p) => (next ? [...p, k] : p.filter((x) => x !== k)))}
                    availableLabel="可选仓库"
                    selectedLabel="已选仓库"
                    itemTestid={(k) => `perm-repo-pick-${k}`}
                  />
                  <p className="admin-note">
                    ⓘ Artifactory 的 Any Local / Any Remote 通配桶不建：BinFlow 契约 repos[] 必须逐个指名现存仓库
                    （服务端校验 unknown repository 即 400）。
                  </p>
                </>
              )}
            </div>
          ) : (
            <div className="pattern-cols">
              {renderPatternCol('includes')}
              {renderPatternCol('excludes')}
            </div>
          )}
        </div>
        <div className="modal-actions">
          <button ref={cancelRef} type="button" className="btn" data-testid="perm-res-cancel" onClick={onClose}>
            取消
          </button>
          {step === 2 && (
            <button type="button" className="btn" onClick={() => setStep(1)}>
              ← 上一步
            </button>
          )}
          {step === 1 ? (
            <button type="button" className="btn primary" data-testid="perm-res-next" onClick={() => setStep(2)}>
              下一步
            </button>
          ) : (
            <button
              type="button"
              className="btn primary"
              data-testid="perm-res-ok"
              onClick={() => onApply(repos, includes, excludes)}
            >
              确定
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

export default function PermissionEditorPage({ mode }: { mode: 'create' | 'edit' }) {
  const { name: routeName = '' } = useParams<{ name: string }>()
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  // 普通 user = 潜在 m-holder（判定形态沿 RepositoriesPage/PermissionsPage
  // 既有）：是否真持有 manage 由 filter=manage 的 200/403 判定，UI 不预判
  const mHolder = normalizeAdminRole(session?.adminRole, session?.admin ?? false) === 'user'
  const toast = useToast()
  const confirm = useConfirm()
  const navigate = useNavigate()

  // 编辑器注水：m-holder 走 E6 覆盖集过滤列表（字段与全量一致）——403 即
  // L2（无 manage/覆盖集空），200 空数组时深链名不在列表 → notFound 呈现
  const targets = useAsync(
    () => (mHolder ? listPermissionTargetsManaged() : listPermissionTargets()),
    [mHolder],
  )
  const reposState = useAsync(getRepositories, [])
  const usersState = useAsync(listUsers, [])
  const groupsState = useAsync(listGroups, [])

  const [f, setF] = useState<EditorState>(CREATE_INITIAL)
  const [baseline, setBaseline] = useState<TargetSnapshot>(snapshotOf(CREATE_INITIAL))
  const [hydrated, setHydrated] = useState(mode === 'create')
  // 404 判定与 hydration 解耦：列表 ok 而 name 不在（deep link 打错/已被
  // 带外删除）时 hydration 永不触发——不能用 hydrated 作 404 信号
  const notFound =
    mode === 'edit' && targets.status === 'ok' && !(targets.data ?? []).some((t) => t.name === routeName)
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)
  const [testPath, setTestPath] = useState('')
  const [addUser, setAddUser] = useState('')
  const [addGroup, setAddGroup] = useState('')
  const [resOpen, setResOpen] = useState(false)

  // 编辑态：从列表过滤（无单查端点）；404 / loading / 403 分支
  useEffect(() => {
    if (mode !== 'edit' || targets.status !== 'ok') return
    const t = (targets.data ?? []).find((x) => x.name === routeName)
    if (!t) return
    const next: EditorState = {
      name: t.name,
      repos: [...t.repos],
      includes: t.includePatterns.length > 0 ? [...t.includePatterns] : [],
      excludes: [...t.excludePatterns],
      users: Object.fromEntries(Object.entries(t.principals.users).map(([k, v]) => [k, [...v]])),
      groups: Object.fromEntries(Object.entries(t.principals.groups).map(([k, v]) => [k, [...v]])),
    }
    setF(next)
    setBaseline(snapshotOf(next))
    setHydrated(true)
  }, [mode, routeName, targets.status, targets.data])

  const evaluation = useMemo(
    () => (testPath.trim() !== '' ? evaluatePath(f.includes, f.excludes, testPath.trim()) : null),
    [f.includes, f.excludes, testPath],
  )
  const diff: DiffLine[] = useMemo(
    () => (hydrated ? buildTargetDiff(baseline, snapshotOf(f)) : []),
    [hydrated, baseline, f],
  )
  const dirty = mode === 'create' || (hydrated && !sameSnapshot(baseline, snapshotOf(f)))

  const repoKeys = (reposState.data ?? []).map((r) => r.key).sort()
  const userOptions = (usersState.data ?? []).map((u) => u.name).filter((n) => !(n in f.users))
  const groupOptions = (groupsState.data ?? []).map((g) => g.name).filter((n) => !(n in f.groups))
  // m-holder 的三个枚举端点（仓库目录/用户/组）一律 403（闭集不开放，
  // §14.1.6）——主体添加位据此降级为手动录入（仓库目录的 forbidden 分支
  // 在 ResourceDialog 内按 reposStatus 处理），服务端终裁
  const usersForbidden = usersState.status === 'forbidden'
  const groupsForbidden = groupsState.status === 'forbidden'

  const toggleAction = (kind: 'users' | 'groups', name: string, action: PermAction) => {
    setF((p) => {
      const cur = p[kind][name] ?? []
      const next = cur.includes(action) ? cur.filter((a) => a !== action) : [...cur, action]
      return { ...p, [kind]: { ...p[kind], [name]: next } }
    })
  }

  const addPrincipal = (kind: 'users' | 'groups', name: string) => {
    if (name === '') return
    setF((p) => (name in p[kind] ? p : { ...p, [kind]: { ...p[kind], [name]: [] } }))
    if (kind === 'users') setAddUser('')
    else setAddGroup('')
  }

  const removePrincipal = (kind: 'users' | 'groups', name: string) => {
    setF((p) => {
      const next = { ...p[kind] }
      delete next[name]
      return { ...p, [kind]: next }
    })
  }

  // m-holder 的 create 臂保持 L4（「＋ 新建权限」入口仅 admin 渲染；控制台
  // 的 m-holder 编辑入口是列表项）。深链 /new 如实说明，不放大入口——
  // POST 本身是族 4 覆盖集内写（服务端终裁），API 臂不受影响。
  if (mHolder && mode === 'create') {
    return (
      <div data-testid="perm-editor-page">
        <div className="page-header">
          <h2>新建权限</h2>
        </div>
        <EmptyState
          message="新建 permission target 是管理员入口"
          hint={`当前用户 ${session?.username} 不是管理员。manage 持有者的控制台编辑入口是权限列表项（仓库全部落在 manage 覆盖集内的 target）；新建也可经 API（POST /api/v1/permissions，覆盖集内 201，引用覆盖集外仓库——含替换前的存量——服务端 403）。`}
          action={
            <Link className="btn" to="/admin/security/permissions">
              ← 返回权限列表
            </Link>
          }
        />
      </div>
    )
  }

  if (mode === 'edit') {
    if (targets.status === 'loading') {
      return (
        <div data-testid="perm-editor-page">
          <Skeleton lines={10} />
        </div>
      )
    }
    if (targets.status === 'error' && targets.error) {
      return (
        <div data-testid="perm-editor-page">
          <ErrorCard error={targets.error} onRetry={targets.reload} />
        </div>
      )
    }
    if (targets.status === 'forbidden' && targets.error) {
      // m-holder：filter=manage 403 = 无 manage/覆盖集空（与无 filter 同形，
      // 零新增可区分面）——普通 user 的 L2 保留面
      return (
        <div data-testid="perm-editor-page">
          <EmptyState
            message="无权限访问权限管理"
            hint={
              mHolder
                ? `当前会话无 manage 覆盖集（由携带 manage 的 permission target 授予）——列表与编辑器对无覆盖集的普通用户不可用（${targets.error.message}）。`
                : targets.error.message
            }
          />
        </div>
      )
    }
    if (notFound) {
      return (
        <div data-testid="perm-editor-page">
          {mHolder ? (
            // m-holder：过滤列表不含此名 = 覆盖集外/部分覆盖（服务端信息隔离
            // 下与不存在同形）——边界说明的常驻位（T-241 L2 卡的退役承接面）
            <EmptyState
              message={`permission target ${routeName} 不在 manage 覆盖集内（或不存在）`}
              hint="manage 持有者可编辑的 target 需引用仓库全部落在覆盖集内（部分覆盖的由服务端隐藏）；覆盖集外的维护经 API（服务端 403 兜底）。"
              action={
                <Link className="btn" to="/admin/security/permissions">
                  ← 返回权限列表
                </Link>
              }
            />
          ) : (
            <EmptyState
              message={`permission target ${routeName} 不存在`}
              action={
                <Link className="btn" to="/admin/security/permissions">
                  ← 返回权限列表
                </Link>
              }
            />
          )}
        </div>
      )
    }
  }

  const nameValid = f.name.trim() !== ''

  // readonly_admin 的创建臂：纯写流程无只读形态——如实呈现只读空态
  if (readOnly && mode === 'create') {
    return (
      <div data-testid="perm-editor-page">
        <div className="page-header">
          <h2>新建权限</h2>
        </div>
        <EmptyState
          message="只读管理员无法创建 permission target"
          hint="创建 target 是管理面写操作（security:write，服务端 403 兜底）。"
          action={
            <Link className="btn" to="/admin/security/permissions">
              ← 返回权限列表
            </Link>
          }
        />
      </div>
    )
  }

  const doSave = async () => {
    // [5] 变更摘要 diff 确认（§4.9 线框：+ 授予 / − 移除 逐条列出）
    const lines: ReactNode = diff.length > 0 ? (
      <div className="diff-list" data-testid="perm-diff">
        {diff.map((d, i) => (
          <div key={i} className={`d ${d.sign === '+' ? 'add' : 'del'}`}>
            <span className="sign" aria-label={d.sign === '+' ? '新增' : '移除'}>
              {d.sign}
            </span>
            <span>
              {d.text} <span className="mono-v" lang="en">{d.value}</span>
            </span>
          </div>
        ))}
      </div>
    ) : (
      // sameSnapshot 集合语义后，编辑态空 diff 已被按钮禁用拦下——此分支
      // 仅剩理论路径（重排/重复），文案如实陈述
      <p className="text-2">没有字段级变更（仅顺序或重复调整）。</p>
    )
    const ok = await confirm({
      title: mode === 'create' ? `创建 target ${f.name.trim()}` : `保存 ${f.name.trim()} 的变更`,
      body: (
        <div>
          <p>
            将提交到 <span className="mono" lang="en">{f.name.trim()}</span>
            （create-or-replace：同名整体替换，单事务）：
          </p>
          {lines}
        </div>
      ),
      confirmLabel: '确认保存',
    })
    if (!ok) return

    setServerError(null)
    setSubmitting(true)
    try {
      // 无动作的主体行不提交（空 r/w/d/m ≡ 未授权；撤销全部动作 = 移除主体）
      const users = Object.fromEntries(Object.entries(f.users).filter(([, a]) => a.length > 0))
      const groups = Object.fromEntries(Object.entries(f.groups).filter(([, a]) => a.length > 0))
      await savePermissionTarget({
        name: f.name.trim(),
        repos: f.repos,
        includePatterns: f.includes,
        excludePatterns: f.excludes,
        principals: { users, groups },
      })
      toast.success(`permission target ${f.name.trim()} 已保存`)
      navigate('/admin/security/permissions')
    } catch (err) {
      // 覆盖集边界（T-217）：非 security-writer 的 403 服务端原文行内如实呈现
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  const doDelete = async () => {
    const ok = await confirm({
      title: `删除 target ${routeName}`,
      body: (
        <p>
          将删除 <span className="mono" lang="en">{routeName}</span> 及其全部授权行（用户与组两侧）。依赖此
          target 的主体将<b>立即</b>失去相应访问（除非其它 target 覆盖）。
        </p>
      ),
      confirmLabel: '删除',
      danger: true,
    })
    if (!ok) return
    try {
      await deletePermissionTarget(routeName)
      toast.success(`permission target ${routeName} 已删除`)
      navigate('/admin/security/permissions')
    } catch (err) {
      // 覆盖集外的删除同样 403——服务端原文 toast（不代持判定）
      toast.error(`删除失败：${errText(err)}`)
    }
  }

  /** 主体 × 动作矩阵表（§6.11 [3]/[4]：用户表锚 perm-matrix，组表锚 perm-matrix-groups） */
  const renderMatrixTable = (kind: 'users' | 'groups') => {
    const cellKind: 'user' | 'group' = kind === 'users' ? 'user' : 'group'
    const names = Object.keys(f[kind]).sort()
    return (
      <table className="table" data-testid={kind === 'users' ? 'perm-matrix' : 'perm-matrix-groups'}>
        <thead>
          <tr>
            <th scope="col">主体</th>
            <th scope="col">read</th>
            <th scope="col">write</th>
            <th scope="col">delete</th>
            <th scope="col" title="manage = 仓库级 admin 派生位（只判 repos[]，pattern 不参与）；不隐含读写删">manage</th>
          </tr>
        </thead>
        <tbody>
          {names.map((name) => {
            const actions = f[kind][name] ?? []
            return (
              <tr key={`${kind}:${name}`}>
                <td>
                  <span className="matrix-user-cell">
                    {cellKind === 'group' && (
                      <span aria-hidden="true" title="组（组成员并集授权）">
                        👥
                      </span>
                    )}
                    <span className="mono" lang="en">
                      {name}
                    </span>
                    <span className="badge neutral">{cellKind === 'group' ? '组' : '用户'}</span>
                    <button
                      type="button"
                      className="principal-remove"
                      aria-label={`移除主体 ${name}`}
                      disabled={readOnly}
                      onClick={() => removePrincipal(kind, name)}
                      data-testid={`perm-matrix-remove-${cellKind}-${name}`}
                    >
                      ✕
                    </button>
                  </span>
                </td>
                {PERM_ACTIONS.map((a) => (
                  <MatrixCell
                    key={a}
                    kind={cellKind}
                    name={name}
                    action={a}
                    on={actions.includes(a)}
                    disabled={readOnly}
                    onToggle={() => toggleAction(kind, name, a)}
                  />
                ))}
              </tr>
            )
          })}
          {names.length === 0 && (
            <tr>
              <td colSpan={5} className="text-muted">
                {kind === 'users'
                  ? '还没有用户主体——从下方添加。'
                  : '还没有组主体——从下方添加。授权 = 用户自身行 ∪ 所属组行的动作并集。'}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    )
  }

  return (
    <div data-testid="perm-editor-page">
      <div className="page-header">
        <h2>
          {mode === 'create' ? '新建权限' : (
            <>
              权限 / <span className="mono" lang="en">{routeName}</span>
            </>
          )}
        </h2>
        {mode === 'edit' && (
          <Link className="btn" to="/admin/security/permissions">
            ← 返回列表
          </Link>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="perm-editor-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：本编辑器为只读呈现（矩阵含 manage 位）；保存/删除是管理面写操作，
          服务端 403 兜底——UI 不代持判定。
        </p>
      )}

      {mHolder && (
        <p className="admin-note" data-testid="perm-editor-manage-note">
          ⓘ 当前会话以 manage 持有者身份编辑（注水 = manage 覆盖集过滤列表）：仓库目录与用户/组枚举是管理面读端点
          （本会话 403）——新增仓库与主体走手动录入，服务端终裁（unknown → 400；覆盖集外保存/删除 → 403）。
        </p>
      )}

      <section className="card perm-section">
        <h3>目标信息</h3>
        <div className="field">
          <label htmlFor="pe-name">名称</label>
          <input
            id="pe-name"
            className="mono-input"
            value={mode === 'create' ? f.name : routeName}
            disabled={mode === 'edit' || readOnly}
            onChange={(e) => setF((p) => ({ ...p, name: e.target.value }))}
            placeholder="ci-out-rw"
            aria-invalid={!nameValid}
            data-testid="perm-form-name"
            lang="en"
          />
          {mode === 'create' &&
            (nameValid ? (
              <p className="field-hint">同名保存 = 整体替换（create-or-replace）。</p>
            ) : (
              <p className="field-error">名称必填</p>
            ))}
        </div>
        <div className="field" style={{ maxWidth: 640 }}>
          <label>适用仓库（至少一个；pattern 与主体在仓库范围内生效）</label>
          {f.repos.length > 0 && (
            <div className="sec-chips" style={{ marginBottom: 8 }} data-testid="perm-repos">
              {f.repos.map((r) => (
                <span key={r} className="pattern-chip" style={{ marginBottom: 0 }}>
                  <span className="val" lang="en">
                    {r}
                  </span>
                  <button
                    type="button"
                    aria-label={`移除仓库 ${r}`}
                    disabled={readOnly}
                    onClick={() => setF((p) => ({ ...p, repos: p.repos.filter((x) => x !== r) }))}
                    data-testid={`perm-repo-remove-${r}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
            </div>
          )}
          <div className="pattern-add">
            {/* 锚沿用 T-101 冻结的 perm-repo-add：本票起是两步资源对话框的入口
                （§4.5「编辑仓库」按钮；新建态文案 = Add Repositories，reverse §3.8） */}
            <button
              type="button"
              className="btn"
              disabled={readOnly}
              onClick={() => setResOpen(true)}
              data-testid="perm-repo-add"
            >
              {mode === 'create' ? '＋ 添加仓库…' : '编辑仓库…'}
            </button>
            {reposState.status === 'error' && (
              <span className="field-hint">仓库列表不可用（{reposState.error?.message ?? '未知错误'}）——对话框内可重试。</span>
            )}
          </div>
        </div>
      </section>

      <section className="card perm-section">
        <h3>路径模式（repo 相对路径；<span className="mono" lang="en">**</span> 跨段 / <span className="mono" lang="en">*</span> 段内）</h3>
        <div className="perm-pattern-summary" data-testid="perm-patterns-summary">
          <span className="text-2">include：</span>
          {f.includes.length === 0 ? (
            <span className="text-muted">（空 = 匹配全部路径）</span>
          ) : (
            f.includes.map((p) => (
              <span key={p} className="badge neutral mono" lang="en">
                {p}
              </span>
            ))
          )}
          <span className="text-2" style={{ marginLeft: 12 }}>exclude：</span>
          {f.excludes.length === 0 ? (
            <span className="text-muted">（无）</span>
          ) : (
            f.excludes.map((p) => (
              <span key={p} className="badge neutral mono" lang="en">
                {p}
              </span>
            ))
          )}
          <span className="text-muted" style={{ fontSize: 'var(--bf-fs-aux)' }}>
            （在「{mode === 'create' ? '添加' : '编辑'}仓库」对话框第 2 步修改）
          </span>
        </div>

        <div className="tester">
          <div className="row">
            <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)', whiteSpace: 'nowrap' }}>
              模式测试器
            </span>
            <input
              value={testPath}
              onChange={(e) => setTestPath(e.target.value)}
              placeholder="输入任意路径即时判定，如 ci-out/builds/42/app.bin（尾 / 表示目录）"
              aria-label="模式测试器路径输入"
              data-testid="perm-pattern-test"
              lang="en"
            />
          </div>
          {evaluation && (
            <div className="tester-result" data-testid="perm-pattern-result" aria-live="polite">
              {evaluation.includesEmpty && (
                <div className="line">
                  <span>–</span>
                  <span>
                    include 为空 = <b>匹配全部路径</b>（auth 语义）
                  </span>
                </div>
              )}
              {evaluation.includes.map((v) => (
                <div key={`i:${v.pattern}`} className={`line${v.hit ? ' hit' : ''}`}>
                  <span aria-hidden="true">{v.hit ? '✓' : '✗'}</span>
                  <span>
                    include <span className="pat" lang="en">{v.pattern}</span> {v.hit ? '命中' : '未命中'}
                  </span>
                </div>
              ))}
              {evaluation.excludes.map((v) => (
                <div key={`e:${v.pattern}`} className={`line${v.hit ? ' hit' : ''}`}>
                  <span aria-hidden="true">{v.hit ? '✗' : '–'}</span>
                  <span>
                    exclude <span className="pat" lang="en">{v.pattern}</span> {v.hit ? '命中（排除）' : '未命中'}
                  </span>
                </div>
              ))}
              <div className="verdict">
                <span
                  className={`badge ${evaluation.match ? 'success' : 'danger'}`}
                  data-testid="perm-pattern-verdict"
                >
                  {evaluation.match ? '✓ 匹配' : '✗ 不匹配'}
                </span>
                <span className="text-2" style={{ fontWeight: 400, fontSize: 'var(--bf-fs-aux)' }}>
                  {evaluation.excludedBy !== null
                    ? `被 exclude \`${evaluation.excludedBy}\` 排除（exclude 优先）`
                    : evaluation.match
                      ? evaluation.includedBy !== null
                        ? `命中 include \`${evaluation.includedBy}\``
                        : 'include 为空（匹配全部）'
                      : '无 include 命中'}
                </span>
              </div>
            </div>
          )}
        </div>
      </section>

      <section className="card perm-section">
        <h3>用户</h3>
        <p className="field-hint">选择用户及其在所选资源上的动作（§6.11[3]）。</p>
        {renderMatrixTable('users')}
        <div className="matrix-add">
          {usersForbidden ? (
            // name-entry（§14.1.6）：用户枚举对 m-holder 403——手动录入用户名，
            // 存在性由服务端终裁（unknown → 400）。perm-add-user 冻结锚随控件
            // 形态迁移（select → input，admin/readonly 路径零变——T-241
            // perm-repo-add 语义滑移同款纪律）
            <input
              value={addUser}
              disabled={readOnly}
              onChange={(e) => setAddUser(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addPrincipal('users', addUser.trim())
                }
              }}
              placeholder="输入用户名（服务端校验）"
              aria-label="输入要添加的用户名"
              data-testid="perm-add-user"
              lang="en"
            />
          ) : (
            <select
              value={addUser}
              disabled={readOnly}
              onChange={(e) => setAddUser(e.target.value)}
              aria-label="选择要添加的用户"
              data-testid="perm-add-user"
            >
              <option value="">＋ 添加用户…</option>
              {userOptions.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          )}
          <button
            type="button"
            className="btn"
            disabled={addUser.trim() === '' || readOnly}
            onClick={() => addPrincipal('users', addUser.trim())}
          >
            添加用户
          </button>
        </div>
        {usersState.status === 'forbidden' && (
          <p className="field-hint">用户枚举是管理面读端点（本会话 403）——手动录入用户名，服务端校验（unknown → 400）。</p>
        )}
        {usersState.status === 'error' && (
          <p className="field-hint">用户列表不可用（{usersState.error?.message ?? '未知错误'}）——可刷新重试。</p>
        )}
      </section>

      <section className="card perm-section">
        <h3>组</h3>
        <p className="field-hint">选择组及其在所选资源上的动作（组成员 = 动作并集）。</p>
        {renderMatrixTable('groups')}
        <div className="matrix-add">
          {groupsForbidden ? (
            // name-entry 同用户区块：组枚举 403 → 手动录入组名（服务端终裁）
            <input
              value={addGroup}
              disabled={readOnly}
              onChange={(e) => setAddGroup(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addPrincipal('groups', addGroup.trim())
                }
              }}
              placeholder="输入组名（服务端校验）"
              aria-label="输入要添加的组名"
              data-testid="perm-add-group"
              lang="en"
            />
          ) : (
            <select
              value={addGroup}
              disabled={readOnly}
              onChange={(e) => setAddGroup(e.target.value)}
              aria-label="选择要添加的组"
              data-testid="perm-add-group"
            >
              <option value="">＋ 添加组…</option>
              {groupOptions.map((n) => (
                <option key={n} value={n}>
                  👥 {n}
                </option>
              ))}
            </select>
          )}
          <button
            type="button"
            className="btn"
            disabled={addGroup.trim() === '' || readOnly}
            onClick={() => addPrincipal('groups', addGroup.trim())}
          >
            添加组
          </button>
        </div>
        {groupsState.status === 'forbidden' && (
          <p className="field-hint">组枚举是管理面读端点（本会话 403）——手动录入组名，服务端校验（unknown → 400）。</p>
        )}
        {groupsState.status === 'error' && (
          <p className="field-hint">组列表不可用（{groupsState.error?.message ?? '未知错误'}）——可刷新重试。</p>
        )}
        <p className="admin-note">
          ⓘ admin 隐式拥有全部权限，不列入矩阵；无动作的主体不会提交（空 r/w/d/m ≡ 未授权）。manage =
          仓库级 admin（可编辑其 manage 覆盖集内的 target、读写该仓配置族；不含建/删仓与安全面）——manage
          持有者（控制台或 API）编辑 target 时，服务端要求其引用的全部仓库（含替换前的存量，T-217 B1）落在覆盖集内，超出即 403。
        </p>
      </section>

      {serverError && (
        <div className="form-error" data-testid="form-error" role="alert">
          <div className="headline">保存失败（HTTP {serverError.status || '网络'}）</div>
          <div className="raw" lang="en">
            {serverError.message}
          </div>
        </div>
      )}

      <div className="form-actions">
        <Link className="btn" to="/admin/security/permissions">
          {readOnly ? '返回列表' : '取消'}
        </Link>
        {!readOnly && (
          <button
            type="button"
            className="btn primary"
            disabled={!nameValid || f.repos.length === 0 || !dirty || submitting}
            title={
              !nameValid
                ? '名称必填'
                : f.repos.length === 0
                  ? '至少选择一个适用仓库'
                  : !dirty
                    ? '没有变更'
                    : undefined
            }
            onClick={() => void doSave()}
            data-testid="perm-save"
          >
            {submitting ? '保存中…' : mode === 'create' ? '创建' : '保存'}
          </button>
        )}
      </div>

      {mode === 'edit' && !readOnly && (
        <div className="danger-zone" style={{ marginTop: 'var(--bf-sp-5)' }} data-testid="perm-danger-zone">
          <h3>危险区</h3>
          <p>删除 target 会连带删除其全部授权行（单事务，无撤销）。</p>
          <button type="button" className="btn danger" onClick={() => void doDelete()} data-testid="perm-delete-button">
            删除 target…
          </button>
        </div>
      )}

      {resOpen && (
        <ResourceDialog
          create={mode === 'create'}
          repoKeys={repoKeys}
          reposStatus={reposState.status}
          initialRepos={f.repos}
          initialIncludes={f.includes}
          initialExcludes={f.excludes}
          onApply={(repos, includes, excludes) => {
            setF((p) => ({ ...p, repos, includes, excludes }))
            setResOpen(false)
          }}
          onClose={() => setResOpen(false)}
        />
      )}
    </div>
  )
}
