// 权限 target 编辑器（T-241 重排——P3 新栈重写；audit §2.7 行为契约逐条）：
//   [1] 目标信息（name 编辑态锁定 + 适用仓库 chips + 「添加/编辑仓库…」入口）
//   [2] 路径模式（只读摘要 + 模式测试器——evaluatePath 本地求值零端点，
//       逐条命中明细 + exclude 优先终判，aria-live）
//   [3] 用户 / [4] 组区块：五动作矩阵（read/annotate/write/delete/manage，
//       列头 tooltip 语义边界；wire 双层 normalizePermActions/wireActions）
//   [5] 保存 = 变更摘要 diff 确认（buildTargetDiff ±行）→ POST create-or-replace
// 两步资源对话框（①选仓穿梭——三通配桶与具体仓混选；m-holder 403 时降级
// 手动录入；②patterns chip 逐行编辑）。
// 覆盖集语义：m-holder 注水走 E6 ?filter=manage；保存/删除 403 服务端原文
// 如实呈现（UI 不代持判定）。readonly_admin 全控件只读。
// 锚族原样：perm-editor-page/perm-form-name/perm-repos/perm-repo-remove-<r>/
// perm-repo-add/perm-res-dialog(-step-1|-step-2|-step|-repos|-next|-ok|-cancel)/
// perm-repo-pick-<k>/perm-repo-entry-input/perm-repo-entry-add/perm-buckets-note/
// perm-pattern(-input|-add|-remove)-{include|exclude}/perm-pattern-test/
// perm-pattern-result/perm-pattern-verdict/perm-matrix(-groups)?/
// perm-matrix-cell-<kind>-<principal>-<action>/perm-matrix-remove-<kind>-<name>/
// perm-add-user/perm-add-group/perm-diff/perm-save/perm-delete-button/
// perm-danger-zone/perm-editor-readonly-note/perm-editor-manage-note。
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge, AlertBox } from '@/components/layout/bits'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { TextInput, NativeSelect } from '@/components/layout/fields'
import { TransferBox } from '@/components/layout/transfer-box'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, errText, getRepositories, isReadOnlyAdmin, normalizeAdminRole } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import './security.css'
import { PERM_ACTIONS, WILDCARD_BUCKETS, deletePermissionTarget, isWildcardBucket, listGroups, listPermissionTargets, listPermissionTargetsManaged, listUsers, normalizePermActions, savePermissionTarget, wireActions } from './api'
import type { PermAction } from './api'
import { evaluatePath } from './pathmatch'
import { buildTargetDiff, sameSnapshot } from './targetdiff'
import type { DiffLine, TargetSnapshot } from './targetdiff'
import { tr } from '@/i18n'

const tt = tr('security')

type PrincipalMap = Record<string, PermAction[]>

// 预置桶穿梭条目（wire 字面键 + 活体拼写展示 + 覆盖语义一行注）
const PRESET_TRANSFER_ITEMS = WILDCARD_BUCKETS.map((b) => ({
  name: b.wire,
  label: b.label,
  note: b.wire === 'ANY DISTRIBUTION'
    ? tt('bundle 域通道（按 bundle 名生效）')
    : b.wire === 'ANY LOCAL'
      ? tt('覆盖全部 local 仓库（含授权后新建）')
      : tt('覆盖全部 remote 仓库（含授权后新建）'),
}))

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

/** 矩阵单元格（testid 契约 perm-matrix-cell-<kind>-<principal>-<action>） */
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
    <td className="px-3 py-1.5 text-center">
      <label className="matrix-cell">
        <input
          type="checkbox"
          checked={on}
          disabled={disabled}
          onChange={onToggle}
          aria-label={tt('{v1} {name} 的 {action} 权限', { v1: kind === 'user' ? tt('用户') : tt('组'), name: name, action: action })}
          data-testid={`perm-matrix-cell-${kind}-${name}-${action}`}
        />
      </label>
    </td>
  )
}

/**
 * 两步资源对话框（对齐 reverse §3.8 Edit Repositories 形态：① Select
 * Repositories（双列穿梭）→ ② Set Patterns (Optional) → OK 回填）。
 * 新栈 = shadcn Dialog（Radix 焦点圈 + Esc = 取消不回填）；打开即聚焦
 * 取消（安全默认——回调 ref + 微任务）；720px 宽由 DialogContent 承载。
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

  // 打开即聚焦取消（安全默认——Radix 默认聚焦首个可聚焦件，此处改道）。
  // ref 必须 useCallback 稳定引用：内联箭头每渲染一新身份，React 走
  // detach(null)→attach(node) 重挂——每敲一个字符（state 更新重渲染）就把
  // 焦点抢回取消钮，键盘 type+Enter 腿实测炸在此（旧版同坑注记）。
  const cancelRef = useCallback((node: HTMLButtonElement | null) => {
    if (node) queueMicrotask(() => node.focus())
  }, [])

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

  /** 手动录入仓库名（m-holder 分支）：目录 403 下的加入面——服务端终裁 */
  const addRepoEntry = () => {
    const v = repoEntry.trim()
    if (v === '') return
    setRepos((p) => (p.includes(v) ? p : [...p, v]))
    setRepoEntry('')
  }

  /** pattern 编辑列（锚：perm-pattern-{input,add,remove}-{include|exclude}） */
  const renderPatternCol = (kind: 'includes' | 'excludes') => {
    const isIncl = kind === 'includes'
    const list = isIncl ? includes : excludes
    const setList = isIncl ? setIncludes : setExcludes
    const input = isIncl ? includeInput : excludeInput
    const setInput = isIncl ? setIncludeInput : setExcludeInput
    const word = isIncl ? 'include' : 'exclude'
    return (
      <div className="pattern-col">
        <h4>{isIncl ? tt('include patterns（命中任一即纳入）') : tt('exclude patterns（命中任一即排除——优先于 include）')}</h4>
        {list.length === 0 && (
          <p className="empty">{isIncl ? tt('空 = 匹配全部路径（等价 **）') : tt('（无排除）')}</p>
        )}
        {list.map((p, i) => (
          <div key={p} className="pattern-chip" data-testid={`perm-pattern-${word}-${i}`}>
            <span className="val" lang="en">{p}</span>
            <button
              type="button"
              aria-label={tt('移除 {p}', { p: p })}
              onClick={() => setList((prev) => prev.filter((x) => x !== p))}
              data-testid={`perm-pattern-remove-${word}-${i}`}
            >
              ✕
            </button>
          </div>
        ))}
        <div className="pattern-add">
          <TextInput
            mono
            lang="en"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addPattern(kind, input)
              }
            }}
            placeholder={isIncl ? 'ci-out/**' : 'ci-out/tmp/**'}
            className="w-[220px]"
            aria-label={tt('添加 {word} pattern', { word: word })}
            data-testid={`perm-pattern-input-${word}`}
          />
          <Button variant="outline" size="sm" onClick={() => addPattern(kind, input)} data-testid={`perm-pattern-add-${word}`}>
            {tt('添加')}
          </Button>
        </div>
      </div>
    )
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent
        className="max-w-[720px] w-[min(720px,calc(100vw-48px))]"
        onOpenAutoFocus={(e) => e.preventDefault()}
        data-testid="perm-res-dialog"
      >
        <DialogHeader>
          <DialogTitle>{step === 1 ? (create ? tt('添加仓库') : tt('编辑仓库')) : tt('设置模式（可选）')}</DialogTitle>
        </DialogHeader>
        {/* 可点步头（两步头常驻可点、「2 Set Patterns (Optional)」标可选） */}
        <div className="perm-res-steps" role="list" aria-label={tt('两步流程')}>
          {([1, 2] as const).map((n) => (
            <button
              key={n}
              type="button"
              role="listitem"
              aria-current={step === n ? 'step' : undefined}
              data-testid={`perm-res-step-${n}`}
              onClick={() => setStep(n)}
              className={`text-left text-aux ${step === n ? 'font-semibold text-foreground' : 'text-muted-foreground hover:text-foreground'}`}
            >
              {`${n} ${n === 1 ? tt('选择仓库') : tt('设置模式（可选）')}`}
            </button>
          ))}
        </div>
        <p className="perm-res-step" data-testid="perm-res-step">
          {tt('第')} {step} {tt('步，共 2 步')}{' '}
          {step === 1
            ? tt(' · 选择此 target 适用的仓库（pattern 与主体在仓库范围内生效）')
            : tt(' · 模式作用于所选仓库内的制品路径（repo 相对路径；** 跨段 / * 段内）')}
        </p>
        <div className="perm-res-body">
          {step === 1 ? (
            <div data-testid="perm-res-repos">
              {reposStatus === 'error' ? (
                <p className="field-error">{tt('仓库列表不可用——关闭后重试（编辑器保存仍需至少一个仓库）。')}</p>
              ) : reposStatus === 'loading' ? (
                <p className="field-hint">{tt('仓库列表加载中…')}</p>
              ) : reposStatus === 'forbidden' ? (
                // m-holder：仓库目录 403——已选仓可摘除，新增走手动录入
                <>
                  <TransferBox
                    items={[
                      ...PRESET_TRANSFER_ITEMS,
                      ...[...new Set(repos)].filter((k) => !isWildcardBucket(k)).map((k) => ({ name: k })),
                    ]}
                    selected={repos}
                    onToggle={(k, next) => setRepos((p) => (next ? [...p, k] : p.filter((x) => x !== k)))}
                    availableLabel={tt('可选仓库')}
                    selectedLabel={tt('已选仓库')}
                    itemTestid={(k) => `perm-repo-pick-${k}`}
                  />
                  <div className="pattern-add">
                    <TextInput
                      mono
                      lang="en"
                      value={repoEntry}
                      onChange={(e) => setRepoEntry(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          addRepoEntry()
                        }
                      }}
                      placeholder={tt('仓库名（服务端校验）')}
                      className="w-[220px]"
                      aria-label={tt('手动录入仓库名')}
                      data-testid="perm-repo-entry-input"
                    />
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={repoEntry.trim() === ''}
                      onClick={addRepoEntry}
                      data-testid="perm-repo-entry-add"
                    >
                      {tt('添加仓库')}
                    </Button>
                  </div>
                  <p className="admin-note" data-testid="perm-buckets-note">
                    {tt('ⓘ 仓库目录是管理面读端点（本会话 403）——无法浏览候选仓库；手动录入仓库名加入，服务端终裁 （unknown repository → 400；覆盖集外保存 → 403）。三预置桶不依赖目录端点、照常可勾选：Any Local / Any Remote 按 class 覆盖全部（含授权后新建）仓库，Any Distribution 覆盖 Release Bundle 域（模式作用于 bundle 名）；桶按字面进 manage 覆盖集判定。')}
                  </p>
                </>
              ) : (
                <>
                  <TransferBox
                    items={[...PRESET_TRANSFER_ITEMS, ...repoKeys.map((k) => ({ name: k }))]}
                    selected={repos}
                    onToggle={(k, next) => setRepos((p) => (next ? [...p, k] : p.filter((x) => x !== k)))}
                    availableLabel={tt('可选仓库')}
                    selectedLabel={tt('已选仓库')}
                    itemTestid={(k) => `perm-repo-pick-${k}`}
                  />
                  <p className="admin-note" data-testid="perm-buckets-note">
                    {tt('ⓘ 预置桶与具体仓库同场可勾选：Any Local / Any Remote 按 class 覆盖全部（含授权后新建）仓库〔virtual 无桶〕，Any Distribution 覆盖 Release Bundle 域〔第 2 步模式作用于 bundle 名——include/exclude 的路径位语义〕；桶与具体仓各自独立生效，ANY 家族互不隐式覆盖。')}
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
        <DialogFooter>
          <Button ref={cancelRef} variant="outline" size="sm" data-testid="perm-res-cancel" onClick={onClose}>{tt('取消')}</Button>
          {step === 2 && (
            <Button variant="outline" size="sm" onClick={() => setStep(1)}>{tt('← 上一步')}</Button>
          )}
          {step === 1 ? (
            <Button size="sm" data-testid="perm-res-next" onClick={() => setStep(2)}>{tt('下一步')}</Button>
          ) : (
            <Button size="sm" data-testid="perm-res-ok" onClick={() => onApply(repos, includes, excludes)}>
              {tt('确定')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export default function PermissionEditorPage({ mode }: { mode: 'create' | 'edit' }) {
  const { name: routeName = '' } = useParams<{ name: string }>()
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const mHolder = normalizeAdminRole(session?.adminRole, session?.admin ?? false) === 'user'
  const confirm = useConfirm()
  const navigate = useNavigate()

  // 编辑器注水：m-holder 走 E6 覆盖集过滤列表——403 即 L2
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
  // 404 判定与 hydration 解耦：列表 ok 而 name 不在时 hydration 永不触发
  const notFound =
    mode === 'edit' && targets.status === 'ok' && !(targets.data ?? []).some((t) => t.name === routeName)
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)
  const [testPath, setTestPath] = useState('')
  const [addUser, setAddUser] = useState('')
  const [addGroup, setAddGroup] = useState('')
  const [resOpen, setResOpen] = useState(false)

  // 编辑态：从列表过滤（无单查端点）——GET 回显正名单经 normalizePermActions
  // 归一（deploy-cache→write），保存侧 wireActions 反向序列化，往返稳定
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
  // m-holder 的枚举端点一律 403——主体添加位降级手动录入，服务端终裁
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

  // m-holder 的 create 臂保持 L4——深链 /new 如实说明，不放大入口
  if (mHolder && mode === 'create') {
    return (
      <div data-testid="perm-editor-page">
        <div className="page-header">
          <h2 className="text-lg font-semibold">{tt('新建权限')}</h2>
        </div>
        <EmptyState
          message={tt('新建 permission target 是管理员入口')}
          hint={tt('当前用户 {v1} 不是管理员。manage 持有者的控制台编辑入口是权限列表项（仓库全部落在 manage 覆盖集内的 target）；新建也可经 API（POST /api/v1/permissions，覆盖集内 201，引用覆盖集外仓库——含替换前的存量——服务端 403）。', { v1: session?.username })}
          action={
            <ButtonAsChild variant="outline" size="sm">
              <Link to="/admin/security/permissions">{tt('← 返回权限列表')}</Link>
            </ButtonAsChild>
          }
        />
      </div>
    )
  }

  if (mode === 'edit') {
    if (targets.status === 'loading') {
      return (
        <div data-testid="perm-editor-page">
          <StateSkeleton lines={10} />
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
      return (
        <div data-testid="perm-editor-page">
          <EmptyState
            message={tt('无权限访问权限管理')}
            hint={
              mHolder
                ? tt('当前会话无 manage 覆盖集（由携带 manage 的 permission target 授予）——列表与编辑器对无覆盖集的普通用户不可用（{v1}）。', { v1: targets.error.message })
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
            <EmptyState
              message={tt('permission target {routeName} 不在 manage 覆盖集内（或不存在）', { routeName: routeName })}
              hint={tt('manage 持有者可编辑的 target 需引用仓库全部落在覆盖集内（部分覆盖的由服务端隐藏）；覆盖集外的维护经 API（服务端 403 兜底）。')}
              action={
                <ButtonAsChild variant="outline" size="sm">
                  <Link to="/admin/security/permissions">{tt('← 返回权限列表')}</Link>
                </ButtonAsChild>
              }
            />
          ) : (
            <EmptyState
              message={tt('permission target {routeName} 不存在', { routeName: routeName })}
              action={
                <ButtonAsChild variant="outline" size="sm">
                  <Link to="/admin/security/permissions">{tt('← 返回权限列表')}</Link>
                </ButtonAsChild>
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
          <h2 className="text-lg font-semibold">{tt('新建权限')}</h2>
        </div>
        <EmptyState
          message={tt('只读管理员无法创建 permission target')}
          hint={tt('创建 target 是管理面写操作（security:write，服务端 403 兜底）。')}
          action={
            <ButtonAsChild variant="outline" size="sm">
              <Link to="/admin/security/permissions">{tt('← 返回权限列表')}</Link>
            </ButtonAsChild>
          }
        />
      </div>
    )
  }

  const doSave = async () => {
    // [5] 变更摘要 diff 确认（+ 授予 / − 移除 逐条列出）
    const lines: ReactNode = diff.length > 0 ? (
      <div className="diff-list" data-testid="perm-diff">
        {diff.map((d, i) => (
          <div key={i} className={`d ${d.sign === '+' ? 'add' : 'del'}`}>
            <span className="sign" aria-label={d.sign === '+' ? tt('新增') : tt('移除')}>{d.sign}</span>
            <span>
              {d.text} <span className="mono-v" lang="en">{d.value}</span>
            </span>
          </div>
        ))}
      </div>
    ) : (
      <p className="text-2">{tt('没有字段级变更（仅顺序或重复调整）。')}</p>
    )
    const ok = await confirm.confirm({
      title: mode === 'create' ? tt('创建 target {v1}', { v1: f.name.trim() }) : tt('保存 {v1} 的变更', { v1: f.name.trim() }),
      description: (
        <div>
          <p>{tt('将提交到')} <span className="font-mono" lang="en">{f.name.trim()}</span>{tt('（create-or-replace：同名整体替换，单事务）：')}</p>
          {lines}
        </div>
      ),
      confirmLabel: tt('确认保存'),
    })
    if (!ok) return

    setServerError(null)
    setSubmitting(true)
    try {
      // 无动作的主体行不提交（空 r/a/w/d/m ≡ 未授权）；动作词经 wireActions
      // 序列化为正名单形（write → deploy-cache——与 GET 回显同形）
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
      toast.success(tt('permission target {v1} 已保存', { v1: f.name.trim() }))
      navigate('/admin/security/permissions')
    } catch (err) {
      // 覆盖集边界：非 security-writer 的 403 服务端原文行内如实呈现
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  const doDelete = async () => {
    const ok = await confirm.confirm({
      title: tt('删除 target {routeName}', { routeName: routeName }),
      description: (
        <p>
          {tt('将删除')} <span className="font-mono" lang="en">{routeName}</span> {tt('及其全部授权行（用户与组两侧）。依赖此 target 的主体将')}<b>{tt('立即')}</b>{tt('失去相应访问（除非其它 target 覆盖）。')}
        </p>
      ),
      confirmLabel: tt('删除'),
      danger: true,
    })
    if (!ok) return
    try {
      await deletePermissionTarget(routeName)
      toast.success(tt('permission target {routeName} 已删除', { routeName: routeName }))
      navigate('/admin/security/permissions')
    } catch (err) {
      toast.error(tt('删除失败：{v1}', { v1: errText(err) }))
    }
  }

  /** 主体 × 动作矩阵表（perm-matrix / perm-matrix-groups 锚） */
  const renderMatrixTable = (kind: 'users' | 'groups') => {
    const cellKind: 'user' | 'group' = kind === 'users' ? 'user' : 'group'
    const names = Object.keys(f[kind]).sort()
    return (
      <table className="w-full text-dense" data-testid={kind === 'users' ? 'perm-matrix' : 'perm-matrix-groups'}>
        <thead>
          <tr className="border-b border-border text-left text-aux text-muted-foreground">
            <th scope="col" className="px-3 py-2 font-medium">{tt('主体')}</th>
            <th scope="col" className="px-3 py-2 font-medium">read</th>
            <th scope="col" className="px-3 py-2 font-medium" title={tt('annotate = 属性写位（7.161 标签 Annotate）：properties 的 PUT/DELETE 门；不隐含内容写（write 是独立列）')}>annotate</th>
            <th scope="col" className="px-3 py-2 font-medium" title={tt('write = 部署位（7.161 标签 Deploy/Cache；wire 正名 deploy-cache，PUT 仍收 write 别名）；不携带 annotate——属性写需另勾 annotate 列')}>write</th>
            <th scope="col" className="px-3 py-2 font-medium" title={tt('delete = 删除/覆盖（7.161 标签 Delete/Overwrite）')}>delete</th>
            <th scope="col" className="px-3 py-2 font-medium" title={tt('manage = 仓库级 admin 派生位（只判 repos[]，pattern 不参与）；不隐含读写删')}>manage</th>
          </tr>
        </thead>
        <tbody>
          {names.map((name) => {
            const actions = f[kind][name] ?? []
            return (
              <tr key={`${kind}:${name}`} className="border-b border-border/60 hover:bg-accent">
                <td className="px-3 py-1.5">
                  <span className="matrix-user-cell">
                    {cellKind === 'group' && (
                      <span aria-hidden="true" title={tt('组（组成员并集授权）')}>👥</span>
                    )}
                    <span className="font-mono" lang="en">{name}</span>
                    <Badge>{cellKind === 'group' ? tt('组') : tt('用户')}</Badge>
                    <button
                      type="button"
                      className="principal-remove"
                      aria-label={tt('移除主体 {name}', { name: name })}
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
              <td colSpan={6} className="px-3 py-2 text-muted-foreground">
                {kind === 'users'
                  ? tt('还没有用户主体——从下方添加。')
                  : tt('还没有组主体——从下方添加。授权 = 用户自身行 ∪ 所属组行的动作并集。')}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    )
  }

  return (
    <div data-testid="perm-editor-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">
          {mode === 'create' ? tt('新建权限') : (
            <>{tt('权限 /')} <span className="font-mono" lang="en">{routeName}</span></>
          )}
        </h2>
        {mode === 'edit' && (
          <ButtonAsChild variant="outline" size="sm" className="ml-auto">
            <Link to="/admin/security/permissions">{tt('← 返回列表')}</Link>
          </ButtonAsChild>
        )}
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="perm-editor-readonly-note">
          {tt('ⓘ 只读管理员（readonly_admin）视角：本编辑器为只读呈现（矩阵含 manage 位）；保存/删除是管理面写操作， 服务端 403 兜底——UI 不代持判定。')}
        </p>
      )}

      {mHolder && (
        <p className="admin-note" data-testid="perm-editor-manage-note">
          {tt('ⓘ 当前会话以 manage 持有者身份编辑（注水 = manage 覆盖集过滤列表）：仓库目录与用户/组枚举是管理面读端点 （本会话 403）——新增仓库与主体走手动录入，服务端终裁（unknown → 400；覆盖集外保存/删除 → 403）。')}
        </p>
      )}

      <section className="card perm-section">
        <h3>{tt('目标信息')}</h3>
        <div className="field">
          <label htmlFor="pe-name">{tt('名称')}</label>
          <TextInput
            id="pe-name"
            mono
            lang="en"
            value={mode === 'create' ? f.name : routeName}
            disabled={mode === 'edit' || readOnly}
            onChange={(e) => setF((p) => ({ ...p, name: e.target.value }))}
            placeholder="ci-out-rw"
            aria-invalid={!nameValid || undefined}
            data-testid="perm-form-name"
          />
          {mode === 'create' &&
            (nameValid ? (
              <p className="field-hint">{tt('同名保存 = 整体替换（create-or-replace）。')}</p>
            ) : (
              <p className="field-error">{tt('名称必填')}</p>
            ))}
        </div>
        <div className="field max-w-[640px]">
          <label>{tt('适用仓库（至少一个；pattern 与主体在仓库范围内生效）')}</label>
          {f.repos.length > 0 && (
            <div className="sec-chips mb-2" data-testid="perm-repos">
              {f.repos.map((r) => (
                <span key={r} className="pattern-chip !mb-0">
                  <span className="val" lang="en" title={isWildcardBucket(r) ? tt('通配桶（wire 字面）——语义见「添加/编辑仓库」对话框注记') : undefined}>
                    {r}
                  </span>
                  <button
                    type="button"
                    aria-label={tt('移除仓库 {r}', { r: r })}
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
            <Button variant="outline" size="sm" disabled={readOnly} onClick={() => setResOpen(true)} data-testid="perm-repo-add">
              {mode === 'create' ? tt('＋ 添加仓库…') : tt('编辑仓库…')}
            </Button>
            {reposState.status === 'error' && (
              <span className="field-hint">{tt('仓库列表不可用（')}{reposState.error?.message ?? tt('未知错误')}{tt('）——对话框内可重试。')}</span>
            )}
          </div>
        </div>
      </section>

      <section className="card perm-section">
        <h3>
          {tt('路径模式（repo 相对路径；')}<span className="font-mono" lang="en">**</span> {tt('跨段 /')} <span className="font-mono" lang="en">*</span> {tt('段内）')}
        </h3>
        <div className="perm-pattern-summary" data-testid="perm-patterns-summary">
          <span className="text-2">{tt('include：')}</span>
          {f.includes.length === 0 ? (
            <span className="text-muted-foreground">{tt('（空 = 匹配全部路径）')}</span>
          ) : (
            f.includes.map((p) => <Badge key={p} mono lang="en">{p}</Badge>)
          )}
          <span className="ml-3 text-2">{tt('exclude：')}</span>
          {f.excludes.length === 0 ? (
            <span className="text-muted-foreground">{tt('（无）')}</span>
          ) : (
            f.excludes.map((p) => <Badge key={p} mono lang="en">{p}</Badge>)
          )}
          <span className="text-aux text-muted-foreground">{tt('（在「')}{mode === 'create' ? tt('添加') : tt('编辑')}{tt('仓库」对话框第 2 步修改）')}</span>
        </div>

        <div className="tester">
          <div className="row">
            <span className="whitespace-nowrap text-aux text-2">{tt('模式测试器')}</span>
            <TextInput
              mono
              lang="en"
              value={testPath}
              onChange={(e) => setTestPath(e.target.value)}
              placeholder={tt('输入任意路径即时判定，如 ci-out/builds/42/app.bin（尾 / 表示目录）')}
              className="flex-1"
              aria-label={tt('模式测试器路径输入')}
              data-testid="perm-pattern-test"
            />
          </div>
          {evaluation && (
            <div className="tester-result" data-testid="perm-pattern-result" aria-live="polite">
              {evaluation.includesEmpty && (
                <div className="line">
                  <span>–</span>
                  <span>{tt('include 为空 =')} <b>{tt('匹配全部路径')}</b>{tt('（auth 语义）')}</span>
                </div>
              )}
              {evaluation.includes.map((v) => (
                <div key={`i:${v.pattern}`} className={`line${v.hit ? ' hit' : ''}`}>
                  <span aria-hidden="true">{v.hit ? '✓' : '✗'}</span>
                  <span>
                    include <span className="pat" lang="en">{v.pattern}</span> {v.hit ? tt('命中') : tt('未命中')}
                  </span>
                </div>
              ))}
              {evaluation.excludes.map((v) => (
                <div key={`e:${v.pattern}`} className={`line${v.hit ? ' hit' : ''}`}>
                  <span aria-hidden="true">{v.hit ? '✗' : '–'}</span>
                  <span>
                    exclude <span className="pat" lang="en">{v.pattern}</span> {v.hit ? tt('命中（排除）') : tt('未命中')}
                  </span>
                </div>
              ))}
              <div className="verdict">
                <span className={`badge ${evaluation.match ? 'success' : 'danger'}`} data-testid="perm-pattern-verdict">
                  {evaluation.match ? tt('✓ 匹配') : tt('✗ 不匹配')}
                </span>
                <span className="text-aux font-normal text-2">
                  {evaluation.excludedBy !== null
                    ? tt('被 exclude `{v1}` 排除（exclude 优先）', { v1: evaluation.excludedBy })
                    : evaluation.match
                      ? evaluation.includedBy !== null
                        ? tt('命中 include `{v1}`', { v1: evaluation.includedBy })
                        : tt('include 为空（匹配全部）')
                      : tt('无 include 命中')}
                </span>
              </div>
            </div>
          )}
        </div>
      </section>

      <section className="card perm-section">
        <h3>{tt('用户')}</h3>
        <p className="field-hint">{tt('选择用户及其在所选资源上的动作（§6.11[3]）。')}</p>
        {renderMatrixTable('users')}
        <div className="matrix-add">
          {usersForbidden ? (
            <TextInput
              mono
              lang="en"
              value={addUser}
              disabled={readOnly}
              onChange={(e) => setAddUser(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addPrincipal('users', addUser.trim())
                }
              }}
              placeholder={tt('输入用户名（服务端校验）')}
              className="w-[260px]"
              aria-label={tt('输入要添加的用户名')}
              data-testid="perm-add-user"
            />
          ) : (
            <NativeSelect
              value={addUser}
              disabled={readOnly}
              onChange={(e) => setAddUser(e.target.value)}
              className="w-[260px]"
              aria-label={tt('选择要添加的用户')}
              data-testid="perm-add-user"
              options={[
                { value: '', label: tt('＋ 添加用户…') },
                ...userOptions.map((n) => ({ value: n, label: n })),
              ]}
            />
          )}
          <Button
            variant="outline"
            size="sm"
            disabled={addUser.trim() === '' || readOnly}
            onClick={() => addPrincipal('users', addUser.trim())}
          >
            {tt('添加用户')}
          </Button>
        </div>
        {usersState.status === 'forbidden' && (
          <p className="field-hint">{tt('用户枚举是管理面读端点（本会话 403）——手动录入用户名，服务端校验（unknown → 400）。')}</p>
        )}
        {usersState.status === 'error' && (
          <p className="field-hint">{tt('用户列表不可用（')}{usersState.error?.message ?? tt('未知错误')}{tt('）——可刷新重试。')}</p>
        )}
      </section>

      <section className="card perm-section">
        <h3>{tt('组')}</h3>
        <p className="field-hint">{tt('选择组及其在所选资源上的动作（组成员 = 动作并集）。')}</p>
        {renderMatrixTable('groups')}
        <div className="matrix-add">
          {groupsForbidden ? (
            <TextInput
              mono
              lang="en"
              value={addGroup}
              disabled={readOnly}
              onChange={(e) => setAddGroup(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  addPrincipal('groups', addGroup.trim())
                }
              }}
              placeholder={tt('输入组名（服务端校验）')}
              className="w-[260px]"
              aria-label={tt('输入要添加的组名')}
              data-testid="perm-add-group"
            />
          ) : (
            <NativeSelect
              value={addGroup}
              disabled={readOnly}
              onChange={(e) => setAddGroup(e.target.value)}
              className="w-[260px]"
              aria-label={tt('选择要添加的组')}
              data-testid="perm-add-group"
              options={[
                { value: '', label: tt('＋ 添加组…') },
                ...groupOptions.map((n) => ({ value: n, label: `👥 ${n}` })),
              ]}
            />
          )}
          <Button
            variant="outline"
            size="sm"
            disabled={addGroup.trim() === '' || readOnly}
            onClick={() => addPrincipal('groups', addGroup.trim())}
          >
            {tt('添加组')}
          </Button>
        </div>
        {groupsState.status === 'forbidden' && (
          <p className="field-hint">{tt('组枚举是管理面读端点（本会话 403）——手动录入组名，服务端校验（unknown → 400）。')}</p>
        )}
        {groupsState.status === 'error' && (
          <p className="field-hint">{tt('组列表不可用（')}{groupsState.error?.message ?? tt('未知错误')}{tt('）——可刷新重试。')}</p>
        )}
        <p className="admin-note">
          {tt('ⓘ admin 隐式拥有全部权限，不列入矩阵；无动作的主体不会提交（空 r/a/w/d/m ≡ 未授权）。manage = 仓库级 admin（可编辑其 manage 覆盖集内的 target、读写该仓配置族；不含建/删仓与安全面）——manage 持有者（控制台或 API）编辑 target 时，服务端要求其引用的全部仓库（含替换前的存量，T-217 B1）落在覆盖集内，超出即 403。 write 与 annotate 是两个独立位：write 只开内容部署（wire 正名 deploy-cache），属性写（properties） 由 annotate 位单独授予——两者互不隐含（T-444 拆分语义，勾选互不联动）。')}
        </p>
      </section>

      {serverError && (
        <AlertBox severity="error" testid="form-error">
          <div className="font-medium">{tt('保存失败（HTTP')} {serverError.status || tt('网络')}{tt('）')}</div>
          <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{serverError.message}</div>
        </AlertBox>
      )}

      <div className="form-actions">
        <Button variant="outline" size="sm" onClick={() => navigate('/admin/security/permissions')}>
          {readOnly ? tt('返回列表') : tt('取消')}
        </Button>
        {!readOnly && (
          <Button
            size="sm"
            disabled={!nameValid || f.repos.length === 0 || !dirty || submitting}
            title={
              !nameValid
                ? tt('名称必填')
                : f.repos.length === 0
                  ? tt('至少选择一个适用仓库')
                  : !dirty
                    ? tt('没有变更')
                    : undefined
            }
            onClick={() => void doSave()}
            data-testid="perm-save"
          >
            {submitting ? tt('保存中…') : mode === 'create' ? tt('创建') : tt('保存')}
          </Button>
        )}
      </div>

      {mode === 'edit' && !readOnly && (
        <div className="danger-zone mt-6 rounded-md border border-destructive/50 p-4" data-testid="perm-danger-zone">
          <h3 className="mb-1 text-dense font-semibold text-destructive">{tt('危险区')}</h3>
          <p className="mb-1.5 text-dense text-2">{tt('删除 target 会连带删除其全部授权行（单事务，无撤销）。')}</p>
          <Button
            variant="outline"
            size="sm"
            className="border-destructive/50 text-destructive hover:bg-destructive/10"
            onClick={() => void doDelete()}
            data-testid="perm-delete-button"
          >
            {tt('删除 target…')}
          </Button>
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
