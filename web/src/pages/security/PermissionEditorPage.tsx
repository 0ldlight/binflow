import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ComponentPropsWithoutRef, ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import Paper from '@mui/material/Paper'
import Select from '@mui/material/Select'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, errText, getRepositories, isReadOnlyAdmin, normalizeAdminRole } from '../../lib/api'
import { monoInputSx } from '../../lib/muiAtoms'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { PERM_ACTIONS, deletePermissionTarget, listGroups, listPermissionTargets, listPermissionTargetsManaged, listUsers, normalizePermActions, savePermissionTarget, wireActions } from './api'
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
//   [3] 用户区块 / [4] 组区块：各自展开矩阵形态，动作五列（T-455，parity
//       B-1.6 翻正）read/annotate/write/delete/manage——列序对位 7.161.20
//       活体（Read/Annotate/Deploy-Cache/Delete-Overwrite/Manage）；列头
//       tooltip 说明语义边界（manage 不隐含读写删 §7.2；write 不携带
//       annotate；annotate = 属性写独立位）。wire 词双层（T-444）：GET 回显
//       正名单 read/deploy-cache/annotate/delete/manage，PUT 另收 write 别名
//       ——水合归一/保存序列化收口在 api.ts 两函数，见 normalizePermActions
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
    <TableCell>
      <label className="matrix-cell">
        <Checkbox
          size="small"
          checked={on}
          disabled={disabled}
          onChange={onToggle}
          slotProps={{
            input: {
              'aria-label': `${kind === 'user' ? '用户' : '组'} ${name} 的 ${action} 权限`,
              'data-testid': `perm-matrix-cell-${kind}-${name}-${action}`,
            } as ComponentPropsWithoutRef<'input'>,
          }}
        />
      </label>
    </TableCell>
  )
}

// T-344 批 D：手写 Tab 循环陷阱（FOCUSABLE 常量）已随 MUI Dialog 的
// FocusTrap 退役——禁用钮不破口由 getTabbable 语义原生覆盖。

/**
 * 两步资源对话框（console-m8 §3.3 C6 / §4.5；对齐 reverse §3.8 的
 * Edit Repositories 形态：① Select Repositories（双列穿梭）→ ② Set
 * Patterns (Optional) → OK 回填）。焦点圈进对话框 + Tab 循环 + Esc = 取消
 * （不回填）。draft 状态在打开时从表单初始化，确定 时一次性 onApply。
 * T-344 批 D：自有 modal 壳（.modal-backdrop + 手写焦点陷阱）→ MUI
 * Dialog（ConfirmDialog 同款接线）：打开即聚焦取消（安全默认） =
 * disableAutoFocus + 回调 ref + 微任务（Modal 二段式提交——T-344C D6）；
 * Esc 兜底 = 文档级监听（MUI 的 Esc 挂 modal root——T-344C D5）；宽度
 * 720px 由 paper sx 承载（security.css 的 .modal.perm-res-modal 行退役）。
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

  // 打开即聚焦取消（安全默认）：回调 ref + 微任务承载（T-344C D6——挂载期
  // useEffect 时 Modal 内容尚未落 DOM）。ref 必须 useCallback 稳定引用：
  // 每次渲染新建的函数会让 React 走「detach(null) → attach(node)」重挂，
  // 对话框内每敲一个字符（state 更新重渲染）就把焦点抢回取消钮——
  // permissions.spec 的 type+Enter 键盘腿实测炸在此（键入落进按钮）。
  const focusCancel = useCallback((node: HTMLButtonElement | null) => {
    if (node) queueMicrotask(() => node.focus())
  }, [])

  // Esc 兜底（T-344C D5）：MUI 已处理的 Esc 会 stopPropagation，不双触发。
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.preventDefault()
      onClose()
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
          <TextField
            size="small"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addPattern(kind, input)
              }
            }}
            placeholder={isIncl ? 'ci-out/**' : 'ci-out/tmp/**'}
            sx={{ width: 220 }}
            slotProps={{
              htmlInput: {
                'aria-label': `添加 ${word} pattern`,
                'data-testid': `perm-pattern-input-${word}`,
                lang: 'en',
                className: 'mono',
              },
            }}
          />
          <Button
            variant="outlined"
            size="small"
           
            onClick={() => addPattern(kind, input)}
            data-testid={`perm-pattern-add-${word}`}
          >
            添加
          </Button>
        </div>
      </div>
    )
  }

  // paper slotProps 以变量承载（嵌套字面量触发 data-* 过剩属性检查——
  // T-344C §3.5 同款修法）
  const paperProps = {
    'data-testid': 'perm-res-dialog',
    sx: { width: 'min(720px, calc(100vw - 48px))' },
  }

  return (
    <Dialog
      open
      disableAutoFocus
      onClose={(_, reason) => {
        // Esc / backdrop 点击 = 取消不回填（旧壳行为原样）
        if (reason === 'escapeKeyDown' || reason === 'backdropClick') onClose()
      }}
      aria-label={step === 1 ? '选择仓库' : '设置模式'}
      slotProps={{ paper: paperProps }}
    >
      <DialogTitle>{step === 1 ? (create ? '添加仓库' : '编辑仓库') : '设置模式（可选）'}</DialogTitle>
      <DialogContent>
        {/* 可点步头（T-455，对位 7.161.20 活体 Add Repositories 弹窗：两个步
            头常驻可点、「2 Set Patterns (Optional)」标可选——探针证据
            reports/agents/t455-probe/）。非当前步可直跳（活体同形）；footer
            的 下一步/上一步 链保留（锚 perm-res-next 冻结）。 */}
        <div className="perm-res-steps" role="list" aria-label="两步流程">
          {([1, 2] as const).map((n) => (
            <Button
              key={n}
              role="listitem"
              variant="text"
              size="small"
              color={step === n ? 'primary' : 'inherit'}
              aria-current={step === n ? 'step' : undefined}
              data-testid={`perm-res-step-${n}`}
              onClick={() => setStep(n)}
              sx={{ justifyContent: 'flex-start', fontWeight: step === n ? 600 : 400, minWidth: 0 }}
            >
              {`${n} ${n === 1 ? '选择仓库' : '设置模式（可选）'}`}
            </Button>
          ))}
        </div>
        <p className="perm-res-step" data-testid="perm-res-step">
          第 {step} 步，共 2 步
          {step === 1 ? ' · 选择此 target 适用的仓库（pattern 与主体在仓库范围内生效）' : ' · 模式作用于所选仓库内的制品路径（repo 相对路径；** 跨段 / * 段内）'}
        </p>
        <div className="perm-res-body">
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
                    <TextField
                      size="small"
                      value={repoEntry}
                      onChange={(e) => setRepoEntry(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          addRepoEntry()
                        }
                      }}
                      placeholder="仓库名（服务端校验）"
                      sx={{ width: 220 }}
                      slotProps={{
                        htmlInput: {
                          'aria-label': '手动录入仓库名',
                          'data-testid': 'perm-repo-entry-input',
                          lang: 'en',
                          className: 'mono',
                        },
                      }}
                    />
                    <Button
                      variant="outlined"
                      size="small"
                     
                      disabled={repoEntry.trim() === ''}
                      onClick={addRepoEntry}
                      data-testid="perm-repo-entry-add"
                    >
                      添加仓库
                    </Button>
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
      </DialogContent>
      <DialogActions>
        <Button ref={focusCancel} variant="outlined" size="small" data-testid="perm-res-cancel" onClick={onClose}>
          取消
        </Button>
        {step === 2 && (
          <Button variant="outlined" size="small" onClick={() => setStep(1)}>
            ← 上一步
          </Button>
        )}
        {step === 1 ? (
          <Button variant="contained" size="medium" data-testid="perm-res-next" onClick={() => setStep(2)}>
            下一步
          </Button>
        ) : (
          <Button
            variant="contained"
            size="medium"
            data-testid="perm-res-ok"
            onClick={() => onApply(repos, includes, excludes)}
          >
            确定
          </Button>
        )}
      </DialogActions>
    </Dialog>
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

  // 编辑态：从列表过滤（无单查端点）；404 / loading / 403 分支。
  // T-444 兼容窗收口（T-455）：GET 回显正名单含 'deploy-cache'——水合过
  // normalizePermActions 归一为 UI 词域（write 列勾选如实亮起），wire 原词
  // 不再原样持有；保存侧 wireActions 反向序列化，GET→PUT→GET 往返稳定。
  useEffect(() => {
    if (mode !== 'edit' || targets.status !== 'ok') return
    const t = (targets.data ?? []).find((x) => x.name === routeName)
    if (!t) return
    const next: EditorState = {
      name: t.name,
      repos: [...t.repos],
      includes: t.includePatterns.length > 0 ? [...t.includePatterns] : [],
      excludes: [...t.excludePatterns],
      users: Object.fromEntries(Object.entries(t.principals.users).map(([k, v]) => [k, normalizePermActions(v)])),
      groups: Object.fromEntries(Object.entries(t.principals.groups).map(([k, v]) => [k, normalizePermActions(v)])),
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
            <Button variant="outlined" size="small" component={Link} to="/admin/security/permissions">
              ← 返回权限列表
            </Button>
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
                <Button variant="outlined" size="small" component={Link} to="/admin/security/permissions">
                  ← 返回权限列表
                </Button>
              }
            />
          ) : (
            <EmptyState
              message={`permission target ${routeName} 不存在`}
              action={
                <Button variant="outlined" size="small" component={Link} to="/admin/security/permissions">
                  ← 返回权限列表
                </Button>
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
            <Button variant="outlined" size="small" component={Link} to="/admin/security/permissions">
              ← 返回权限列表
            </Button>
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
      // 无动作的主体行不提交（空 r/a/w/d/m ≡ 未授权；撤销全部动作 = 移除主体）；
      // 动作词经 wireActions 序列化为正名单形（write → deploy-cache——与 GET
      // 回显同形，PUT 的 write 别名只是收词兼容，不再由 FE 发出）
      const users = Object.fromEntries(
        Object.entries(f.users)
          .filter(([, a]) => a.length > 0)
          .map(([k, a]) => [k, wireActions(a)]),
      )
      const groups = Object.fromEntries(
        Object.entries(f.groups)
          .filter(([, a]) => a.length > 0)
          .map(([k, a]) => [k, wireActions(a)]),
      )
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

  /** 主体 × 动作矩阵表（§6.11 [3]/[4]：用户表锚 perm-matrix，组表锚
   * perm-matrix-groups）。T-455 五列（parity B-1.6 翻正——Q7）：read /
   * annotate / write / delete / manage，列序对位 7.161.20 活体 Repositories
   * 资源型矩阵（Read / Annotate / Deploy/Cache / Delete/Overwrite / Manage
   * ——探针证据 reports/agents/t455-probe/）；列头词用 BinFlow wire 正名单
   * 词形，7.161 标签进 title。 */
  const renderMatrixTable = (kind: 'users' | 'groups') => {
    const cellKind: 'user' | 'group' = kind === 'users' ? 'user' : 'group'
    const names = Object.keys(f[kind]).sort()
    return (
      <Table data-testid={kind === 'users' ? 'perm-matrix' : 'perm-matrix-groups'}>
        <TableHead>
          <TableRow>
            <TableCell component="th" scope="col">主体</TableCell>
            <TableCell component="th" scope="col">read</TableCell>
            <TableCell component="th" scope="col" title="annotate = 属性写位（7.161 标签 Annotate）：properties 的 PUT/DELETE 门；不隐含内容写（write 是独立列）">annotate</TableCell>
            <TableCell component="th" scope="col" title="write = 部署位（7.161 标签 Deploy/Cache；wire 正名 deploy-cache，PUT 仍收 write 别名）；不携带 annotate——属性写需另勾 annotate 列">write</TableCell>
            <TableCell component="th" scope="col" title="delete = 删除/覆盖（7.161 标签 Delete/Overwrite）">delete</TableCell>
            <TableCell component="th" scope="col" title="manage = 仓库级 admin 派生位（只判 repos[]，pattern 不参与）；不隐含读写删">manage</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {names.map((name) => {
            const actions = f[kind][name] ?? []
            return (
              <TableRow key={`${kind}:${name}`} hover>
                <TableCell>
                  <span className="matrix-user-cell">
                    {cellKind === 'group' && (
                      <span aria-hidden="true" title="组（组成员并集授权）">
                        👥
                      </span>
                    )}
                    <span className="mono" lang="en">
                      {name}
                    </span>
                    <Chip size="small" className="badge neutral" label={cellKind === 'group' ? '组' : '用户'} />
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
                </TableCell>
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
              </TableRow>
            )
          })}
          {names.length === 0 && (
            <TableRow>
              <TableCell colSpan={6} className="text-muted">
                {kind === 'users'
                  ? '还没有用户主体——从下方添加。'
                  : '还没有组主体——从下方添加。授权 = 用户自身行 ∪ 所属组行的动作并集。'}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
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
          <Button variant="outlined" size="small" component={Link} to="/admin/security/permissions">
            ← 返回列表
          </Button>
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
          <TextField
            id="pe-name"
            size="small"
            value={mode === 'create' ? f.name : routeName}
            disabled={mode === 'edit' || readOnly}
            onChange={(e) => setF((p) => ({ ...p, name: e.target.value }))}
            placeholder="ci-out-rw"
            error={!nameValid}
            sx={{ width: 320 }}
            slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'perm-form-name', lang: 'en' } }}
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
            <Button
              variant="outlined"
              size="small"
             
              disabled={readOnly}
              onClick={() => setResOpen(true)}
              data-testid="perm-repo-add"
            >
              {mode === 'create' ? '＋ 添加仓库…' : '编辑仓库…'}
            </Button>
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
              <Chip key={p} size="small" className="badge neutral mono" label={p} sx={{ fontFamily: 'var(--bf-mono)' }} />
            ))
          )}
          <span className="text-2" style={{ marginLeft: 12 }}>exclude：</span>
          {f.excludes.length === 0 ? (
            <span className="text-muted">（无）</span>
          ) : (
            f.excludes.map((p) => (
              <Chip key={p} size="small" className="badge neutral mono" label={p} sx={{ fontFamily: 'var(--bf-mono)' }} />
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
            <TextField
              size="small"
              value={testPath}
              onChange={(e) => setTestPath(e.target.value)}
              placeholder="输入任意路径即时判定，如 ci-out/builds/42/app.bin（尾 / 表示目录）"
              sx={{ ...monoInputSx, flex: 1 }}
              slotProps={{
                htmlInput: {
                  'aria-label': '模式测试器路径输入',
                  'data-testid': 'perm-pattern-test',
                  lang: 'en',
                  className: 'mono',
                },
              }}
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
                <Chip
                  size="small"
                  variant="outlined"
                  color={evaluation.match ? 'success' : 'error'}
                  className={`badge ${evaluation.match ? 'success' : 'danger'}`}
                  label={evaluation.match ? '✓ 匹配' : '✗ 不匹配'}
                  data-testid="perm-pattern-verdict"
                />
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
            <TextField
              size="small"
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
              sx={{ ...monoInputSx, width: 260 }}
              slotProps={{
                htmlInput: {
                  'aria-label': '输入要添加的用户名',
                  'data-testid': 'perm-add-user',
                  lang: 'en',
                  className: 'mono',
                },
              }}
            />
          ) : (
            <TextField
              select
              size="small"
              value={addUser}
              disabled={readOnly}
              onChange={(e) => setAddUser(e.target.value)}
              sx={{ ...monoInputSx, width: 260 }}
              slotProps={{
                select: {
                  native: true,
                  inputProps: {
                    'aria-label': '选择要添加的用户',
                    'data-testid': 'perm-add-user',
                  } as ComponentPropsWithoutRef<'select'>,
                } as ComponentPropsWithoutRef<typeof Select>,
              }}
            >
              <option value="">＋ 添加用户…</option>
              {userOptions.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </TextField>
          )}
          <Button
            variant="outlined"
            size="small"
           
            disabled={addUser.trim() === '' || readOnly}
            onClick={() => addPrincipal('users', addUser.trim())}
          >
            添加用户
          </Button>
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
            <TextField
              size="small"
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
              sx={{ ...monoInputSx, width: 260 }}
              slotProps={{
                htmlInput: {
                  'aria-label': '输入要添加的组名',
                  'data-testid': 'perm-add-group',
                  lang: 'en',
                  className: 'mono',
                },
              }}
            />
          ) : (
            <TextField
              select
              size="small"
              value={addGroup}
              disabled={readOnly}
              onChange={(e) => setAddGroup(e.target.value)}
              sx={{ ...monoInputSx, width: 260 }}
              slotProps={{
                select: {
                  native: true,
                  inputProps: {
                    'aria-label': '选择要添加的组',
                    'data-testid': 'perm-add-group',
                  } as ComponentPropsWithoutRef<'select'>,
                } as ComponentPropsWithoutRef<typeof Select>,
              }}
            >
              <option value="">＋ 添加组…</option>
              {groupOptions.map((n) => (
                <option key={n} value={n}>
                  👥 {n}
                </option>
              ))}
            </TextField>
          )}
          <Button
            variant="outlined"
            size="small"
           
            disabled={addGroup.trim() === '' || readOnly}
            onClick={() => addPrincipal('groups', addGroup.trim())}
          >
            添加组
          </Button>
        </div>
        {groupsState.status === 'forbidden' && (
          <p className="field-hint">组枚举是管理面读端点（本会话 403）——手动录入组名，服务端校验（unknown → 400）。</p>
        )}
        {groupsState.status === 'error' && (
          <p className="field-hint">组列表不可用（{groupsState.error?.message ?? '未知错误'}）——可刷新重试。</p>
        )}
        <p className="admin-note">
          ⓘ admin 隐式拥有全部权限，不列入矩阵；无动作的主体不会提交（空 r/a/w/d/m ≡ 未授权）。manage =
          仓库级 admin（可编辑其 manage 覆盖集内的 target、读写该仓配置族；不含建/删仓与安全面）——manage
          持有者（控制台或 API）编辑 target 时，服务端要求其引用的全部仓库（含替换前的存量，T-217 B1）落在覆盖集内，超出即 403。
          write 与 annotate 是两个独立位：write 只开内容部署（wire 正名 deploy-cache），属性写（properties）
          由 annotate 位单独授予——两者互不隐含（T-444 拆分语义，勾选互不联动）。
        </p>
      </section>

      {serverError && (
        <Alert severity="error" data-testid="form-error">
          <div className="headline">保存失败（HTTP {serverError.status || '网络'}）</div>
          <div className="raw" lang="en">
            {serverError.message}
          </div>
        </Alert>
      )}

      <div className="form-actions">
        <Button variant="outlined" size="small" component={Link} to="/admin/security/permissions">
          {readOnly ? '返回列表' : '取消'}
        </Button>
        {!readOnly && (
          <Button
            variant="contained"
            size="small"
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
          </Button>
        )}
      </div>

      {/* T-344 批 D：危险区 Paper 化（mui-native-visual §3.3）——
          variant outlined + error 边；类名留 DOM（inert）。 */}
      {mode === 'edit' && !readOnly && (
        <Paper
          variant="outlined"
          className="danger-zone"
          sx={{ mt: 'var(--bf-sp-5)', p: 'var(--bf-sp-4)', borderColor: 'error.main' }}
          data-testid="perm-danger-zone"
        >
          <Typography variant="subtitle2" component="h3" color="error" sx={{ mb: 1 }}>
            危险区
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            删除 target 会连带删除其全部授权行（单事务，无撤销）。
          </Typography>
          <Button
            variant="outlined"
            color="error"
            size="small"

            onClick={() => void doDelete()}
            data-testid="perm-delete-button"
          >
            删除 target…
          </Button>
        </Paper>
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
