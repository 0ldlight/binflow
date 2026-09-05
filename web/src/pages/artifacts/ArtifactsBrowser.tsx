import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ComponentPropsWithoutRef, KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import FormControlLabel from '@mui/material/FormControlLabel'
import Menu from '@mui/material/Menu'
import MuiSkeleton from '@mui/material/Skeleton'
import Popover from '@mui/material/Popover'
import Radio from '@mui/material/Radio'
import RadioGroup from '@mui/material/RadioGroup'
import Select from '@mui/material/Select'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'

import { useAuth } from '../../app/AuthContext'
import { useConfirm } from '../../components/ConfirmDialog'
import { CopyButton } from '../../components/CopyButton'
import DeployDialog from '../../components/DeployDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { PkgIcon } from '../../components/PkgIcon'
import SetMeUpDialog from '../../components/SetMeUpDialog'
import { useToast } from '../../app/ToastContext'
import { ApiError, getRepositories, getStorageStats, isReadOnlyAdmin } from '../../lib/api'
import type { RepoListItem } from '../../lib/api'
import { formatBytes } from '../../lib/format'
import { cellBtnSx, monoInputSx } from '../../lib/muiAtoms'
import { cfgBool, cfgStrList, getRepoDetail } from '../../lib/repos'
import type { PackageType } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'
import { clientCommands } from '../repositories/commands'

import NodeDetail from './NodeDetail'
import type { DetailTab, DownloadState } from './NodeDetail'
import type { ChildNode } from './lib'
import './browser.css'
// 共享样式（T-100 建立的树/上传/搜索样式族；搜索页仍从原址引入——完整
// 归位随 T-239/T-240 域票收口，本票不越界改 pages/search）
import '../repositories/tree/tree.css'
import {
  ancestorDirs,
  deleteNode,
  downloadArtifact,
  listFolder,
  mkdir,
  saveBlob,
  validateNameSegment,
} from './lib'
import { tr } from '../../i18n'

const tt = tr('artifacts')

// 跨仓制品浏览器（console-m8 §6.3 / FR-72，T-236——Artifactory 树浏览器
// 对齐面；自 repositories/tree/TreePage 迁址重构）：
//
// - 左树：仓库为顶层节点（rclass×packageType 图标区分）+ 懒展开一层 +
//   树头工具带（T-434 / FR-142.4：过滤仓库文本框 + 包类型 facet 复选组 +
//   Local/Remote/Virtual 组 + Sort-by + Compacted/Non-Compacted 单选 +
//   My Favorites——对齐 Artifactory 树头；收藏为前端态，localStorage 持久）。
// - URL 即状态（T-434 / FR-142.3，断言反转①）：/artifacts[/<TAB>]/<repo>/
//   <path>——活跃页签是 URL 路径段（省略 = general，对位 Artifactory
//   /ui/repos/tree/<TAB>/…）；文件选择是路径段（?focus= 退役，旧深链
//   一次性 replace 重定向——书签不猝死）。深链自动展开祖先链（含被选
//   目录自身）并滚动定位（§4.3）。
// - 选择 ≠ 展开（T-434 / FR-142.2）：仓库名/目录单击 = 纯选中（右侧面
//   切换，不强制展开分支——原 selectedRepo 并集解除）；展开箭头独立操作；
//   键盘 →/← 独立展开/收起。
// - 文件叶子进树（T-434 / FR-142.1，断言反转①）：目录展开显示子目录与
//   文件行（filter n.folder 退役），仅含文件的目录不再渲染误导性「（空）」
//   占位；文件叶子点击 = 选中出右侧 item view；children 表随之收窄——
//   操作列（详情/下载/删除）退役（文件主路径走树/表行选中，删除收敛进
//   详情面板与右键菜单，两者都过危险确认——Q2 出口①，E1 不倒退）。
//   文件/目录末段判别经父目录 listing（父链本就装载）；未决期右侧骨架，
//   不闪错误形态。
// - 两个过滤词都按作用域复位（QA-3 / FR-82-AC2）：跨层/跨仓导航清空，
//   同层内（文件选中、树展开）保留——语义论证见过滤状态声明处注释。
// - 右键菜单（console-m8 C3 的 BinFlow 对齐面）：文件=复制路径/下载/
//   删除；目录=复制路径/删除/刷新；仓库=复制仓库路径/收藏（My Favorites）/
//   刷新/在仓库管理中打开。Move/Copy 无端点不建（零影子入口）。
//   Shift+F10 / Menu 键可达。
// - 403 收敛（§2.2/§3.6）：GET /api/repositories 走 CapRepoRead——普通
//   user 403 → 树顶层 L2 无权限卡 + 搜索/直链引导；已知 repo key 的深链
//   按路径 ACL 可达（合成顶层节点）。repo 元数据 403 → generic 降级。
// - readonly_admin（T-218 债收口）：上传/建目录/删除禁用 + 只读注记；
//   普通用户写入口保留（服务端 403 行内呈现，W12d）。
// - 协议特化（P6 随迁）：docker 两级 + tag 徽标 + digest 列（T-134 G32a）；
//   上传入口仅 generic/maven local，docker/npm/pypi 以接入命令块替代。
//
// T-300 批次二：控件层迁 MUI（Table 家族 / TextField / Button / Checkbox /
// Chip 徽章 / Alert / 右键菜单 Menu）。交互逻辑零变化：树键盘语义（↑↓→←
// Enter/Shift+F10）与行导航、过滤复位语义、锚点（tree-* 族）全部原样；
// 右键菜单项保持原生 button（artifacts-tree.spec 断言
// [data-testid="tree-context-menu"] button 计数——MenuItem 是 li，会断言
// 断链）。

const PAGE = 100
const BIG_DIR = 2000
/** 单层目录树节点渲染上限（懒加载一层 + content-visibility 之外的保险丝；
 * T-434 起口径含文件叶子——目录+文件合并计数） */
const TREE_LEVEL_CAP = 300

// ---- URL 模型（T-434 / FR-142.3）--------------------------------------------
//
// /artifacts[/<TAB>]/<repo>[/<path>…]，TAB ∈ {general|properties|permissions}
// （对位 Artifactory /ui/repos/tree/<TAB>/<repo>/<path>）。TAB 省略 = general：
// 旧 /artifacts/<repo>/<path> 全量保持规范形零重定向（书签与既有 spec 面
// 不猝死），页签非默认档才占段。文件是路径末段（?focus= 退役）——旧
// ?focus= URL 经一次性 replace 折入路径。已知边界：repo key 与 TAB 词
// （general/properties/permissions）同名时按 TAB 解析（该名仓库经
// /artifacts/general/<key> 仍可达）。

/** 页签 ID（NodeDetail 内部值）→ URL 段 slug */
const TAB_SLUG: Record<DetailTab, string> = { general: 'general', props: 'properties', perms: 'permissions' }
const SLUG_TO_TAB: Record<string, DetailTab> = {
  general: 'general',
  properties: 'props',
  permissions: 'perms',
}

function buildTreeUrl(tab: DetailTab, repo: string, segs: string[]): string {
  const slug = tab === 'general' ? '' : `${TAB_SLUG[tab]}/`
  if (!repo) return '/artifacts'
  const path = encodeDir(segs.join('/'))
  return `/artifacts/${slug}${encodeURIComponent(repo)}${path ? `/${path}` : ''}`
}

// ---- 树头工具带（T-434 / FR-142.4）-------------------------------------------

const FAV_KEY = 'bf-tree-favorites'
const COMPACT_KEY = 'bf-tree-compacted'

function loadFavorites(): Set<string> {
  try {
    const raw = localStorage.getItem(FAV_KEY)
    const arr = raw ? (JSON.parse(raw) as unknown) : []
    return new Set(Array.isArray(arr) ? arr.filter((s) => typeof s === 'string') : [])
  } catch {
    return new Set()
  }
}

/** Sort-by 选项（K67 冻结候选：名称 / 包类型 / 仓库类型，均升序、name 稳定末键） */
type TreeSort = 'name' | 'pkg' | 'rclass'

function compareRepos(a: RepoListItem, b: RepoListItem, sort: TreeSort): number {
  if (sort === 'pkg') {
    const d = (a.packageType || '').localeCompare(b.packageType || '')
    if (d !== 0) return d
  } else if (sort === 'rclass') {
    const d = (a.type || '').localeCompare(b.type || '')
    if (d !== 0) return d
  }
  return a.key.localeCompare(b.key)
}

type DirStatus =
  | { status: 'loading' }
  | { status: 'ok'; nodes: ChildNode[]; remoteDegraded?: string }
  | { status: 'forbidden'; error: ApiError }
  | { status: 'error'; error: ApiError }

function encodeDir(dir: string): string {
  return dir
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
}

/** 跨仓维度的目录缓存键 */
function ck(repoKey: string, dir: string): string {
  return `${repoKey}\n${dir}`
}

/** 右键菜单目标 */
type MenuTarget =
  | { kind: 'repo'; repoKey: string }
  | { kind: 'node'; repoKey: string; node: ChildNode }

export default function ArtifactsBrowser() {
  const routeParams = useParams<{ tab?: string; key?: string }>()
  // splat 需无类型参数表读取（'*' 不在 RouteComponentParams 的键集内）
  const splatRaw = useParams()['*'] ?? ''
  const navigate = useNavigate()
  const location = useLocation()
  const [params] = useSearchParams()
  const toast = useToast()
  const confirm = useConfirm()
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const readOnly = isReadOnlyAdmin(session)
  // T-372 / FR-122.1：树尾常驻回收站入口的可见性——管理壳同门
  // （AppShell canSeeAdmin：admin ∪ readonly_admin），普通 user 不渲染
  const canSeeAdmin = admin || readOnly

  // ---- URL 解析（T-434 / FR-142.3：页签段 + 文件路径段）----
  // 两条路由同组件承载：artifacts/:tab/:key/*（新形，tab 是段）与
  // artifacts/:key/*（tab 省略 = general 的规范形 + 单段旧形）。旧 ?focus=
  // 与「首段不是页签词」的多段 URL 是 legacy——一次性 replace 折入规范形
  // （重定向目标与当前 pathname 相同时不动作，防环）。
  const tabParam = routeParams.tab
  const keyParam = routeParams.key ?? ''
  const focusParam = params.get('focus')
  const splatKey = splatRaw.split('/').filter((s) => s !== '').join('\n')
  const parsed = useMemo(() => {
    let tab: DetailTab = 'general'
    let repo = ''
    let segs: string[] = []
    let legacy = false
    const splatSegs = splatKey === '' ? [] : splatKey.split('\n')
    if (tabParam !== undefined) {
      if (tabParam in SLUG_TO_TAB) {
        tab = SLUG_TO_TAB[tabParam]
        repo = keyParam
        segs = splatSegs
      } else {
        // 多段旧形落在本路由（:tab 吃掉了 repo key）——回填为 repo + path
        legacy = true
        repo = tabParam
        segs = [keyParam, ...splatSegs].filter((s) => s !== '')
      }
    } else if (keyParam) {
      if (keyParam in SLUG_TO_TAB) tab = SLUG_TO_TAB[keyParam] // /artifacts/<TAB>：跨仓根的页签视图
      else {
        repo = keyParam
        segs = splatSegs
      }
    }
    if (focusParam) {
      // ?focus= 退役：折入路径末段（跨仓根上的 focus 无承载面，丢弃）
      if (repo) segs = [...segs, focusParam]
      legacy = true
    }
    return { tab, repo, segs, legacy }
  }, [tabParam, keyParam, splatKey, focusParam])

  const repoKey = parsed.repo
  const tab = parsed.tab
  const legacyTarget = parsed.legacy ? buildTreeUrl(parsed.tab, parsed.repo, parsed.segs) : null
  useEffect(() => {
    // 规范形一次性重定向（?focus= 折入路径 / 多段旧形回填页签段）
    if (legacyTarget && location.pathname !== legacyTarget) navigate(legacyTarget, { replace: true })
  }, [legacyTarget, location.pathname, navigate])

  // ---- 仓库列表（CapRepoRead：admin/readonly 全量；普通 user 403） ----
  const reposQuery = useAsync(() => getRepositories(), [])
  const repos: RepoListItem[] = useMemo(() => (reposQuery.status === 'ok' ? (reposQuery.data ?? []) : []), [
    reposQuery.status,
    reposQuery.data,
  ])

  // 空实例引导（console-m8 §1.1：创建仓库 + 跳过，跳过状态存 localStorage）
  const [onboardSkipped, setOnboardSkipped] = useState(() => localStorage.getItem('bf-skip-onboarding') === '1')

  // ---- 选中仓库元数据（admin 面）：403 → 未知协议，按 generic 降级 ----
  const meta = useAsync(() => (repoKey ? getRepoDetail(repoKey) : Promise.resolve(null)), [repoKey])
  const repoMeta = meta.status === 'ok' ? meta.data : null
  const packageType = repoMeta ? (repoMeta.packageType as PackageType) : undefined
  const rclass = repoMeta ? repoMeta.rclass : undefined
  const uploadable = repoMeta
    ? rclass === 'local' && (packageType === 'generic' || packageType === 'maven')
    : meta.status === 'forbidden'
  const mkdirable = repoMeta ? rclass === 'local' && packageType === 'generic' : meta.status === 'forbidden'
  const isDockerRepo = repoMeta ? packageType === 'docker' : false
  // T-416（FR-136.3，断言反转②）：T-406 的内容面 gate 解除——服务端聚合面
  // （T-412）就绪，virtual 仓与非 virtual 仓同形浏览（树动态展开 + children
  // 表 = 成员并集）。isVirtual 仍作两处语义分流：① 并集为空时的成员感知
  // 空态文案（成员读取自规范回显 configuration.repositories）；② 删除入口
  // 预收敛（RE-08：virtual 不经手删除，服务端一律 405——不给注定失败的
  // 影子入口，与 uploadable/mkdirable 的 rclass 门同款姿态）。
  // remote 仓列表 = 缓存落地行（T-406 服务端同票放开，维持）。
  const isVirtual = repoMeta ? repoMeta.rclass === 'virtual' : false
  const virtualMembers = repoMeta ? cfgStrList(repoMeta.configuration, 'repositories') : []
  // T-461（FR-147）：remote 仓的远端浏览可选档（listRemoteFolderItems——
  // 默认 false，off = 仅缓存行 diff=0）。on 时 BE 在 listing 里并入上游
  // 枚举的 display-only 行（helm index 全树 / deb·rpm 元数据臂——T-442
  // 引擎），virtual 成员的远端行同样并入（§8.5 扩面——T-448）；FE 消费 =
  // 行标记 + 文案 + 点击回源（元数据面 GET 即 pull-through）。403（普通
  // user 深链）时未知——按 off 口径呈现（行为仍由服务端单源决定）。
  const remoteBrowseOn =
    repoMeta !== null && repoMeta.rclass === 'remote' && cfgBool(repoMeta.configuration, 'listRemoteFolderItems')

  // ---- 目录加载（缓存 + 去重；键含 repo 维度——跨仓切换不复用脏缓存） ----
  const [dirState, setDirState] = useState<Record<string, DirStatus>>({})
  const cacheRef = useRef(new Map<string, { nodes: ChildNode[]; remoteDegraded: string }>())
  const inflightRef = useRef(new Set<string>())
  const [tick, setTick] = useState(0)
  /** 手动/深链展开的目录（跨仓复合键） */
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const expandedKey = Array.from(expanded).sort().join('\n')

  // ---- 路径末段分类（T-434 / FR-142.1：文件是路径段）----
  // 末段是文件还是目录由父目录 listing 判别（父链本就装载，零额外请求）；
  // 未决期（父 listing 在途）右侧骨架呈现，不闪目录形态。装载链取乐观
  // 全路径（目录深链免瀑布）；文件路径多付一次 FileInfo GET——文件选中
  // 非热路径，正确性优先（优化余地登记票内遗留）。
  const pathSegs = parsed.segs
  const fullDir = pathSegs.join('/')
  const parentDir = pathSegs.length > 0 ? pathSegs.slice(0, -1).join('/') : ''
  const lastSeg = pathSegs.length > 0 ? pathSegs[pathSegs.length - 1] : ''
  const parentSt = repoKey ? dirState[ck(repoKey, parentDir)] : undefined
  const parentSettled =
    pathSegs.length === 0 || parentSt?.status === 'ok' || parentSt?.status === 'forbidden' || parentSt?.status === 'error'
  const lastChild =
    parentSt?.status === 'ok' && lastSeg !== '' ? parentSt.nodes.find((n) => n.name === lastSeg) : undefined
  const fileSelected = !!(lastChild && !lastChild.folder)
  const dir = fileSelected ? parentDir : fullDir
  const focusFile = fileSelected ? lastSeg : null
  /** 选中文件的树路径（与 ChildNode.path 同构：根层文件无前导斜杠） */
  const focusPath = focusFile !== null ? (dir !== '' ? `${dir}/${focusFile}` : focusFile) : null

  // 展开链 = 分类后 dir 的祖先（含自身）；装载链 = 乐观全路径的祖先（含自身）
  const chain = useMemo(() => ancestorDirs(dir), [dir])
  const chainKey = chain.join('\n')
  const loadChainKey = useMemo(() => ancestorDirs(fullDir).join('\n'), [fullDir])

  const loadDir = useCallback(
    (repo: string, d: string, force = false) => {
      const key = ck(repo, d)
      if (!force && (cacheRef.current.has(key) || inflightRef.current.has(key))) {
        const cached = cacheRef.current.get(key)
        if (cached) {
          setDirState((s) => ({
            ...s,
            [key]: { status: 'ok', nodes: cached.nodes, remoteDegraded: cached.remoteDegraded || undefined },
          }))
        }
        return
      }
      cacheRef.current.delete(key)
      inflightRef.current.add(key)
      setDirState((s) => ({ ...s, [key]: { status: 'loading' } }))
      // T-134 G32a: docker repo tree passes isDockerRepo for ?docker_tags enrichment
      listFolder(repo, d, undefined, repo === repoKey && isDockerRepo)
        .then(({ nodes, remoteDegraded }) => {
          cacheRef.current.set(key, { nodes, remoteDegraded })
          inflightRef.current.delete(key)
          setDirState((s) => ({ ...s, [key]: { status: 'ok', nodes, remoteDegraded: remoteDegraded || undefined } }))
        })
        .catch((err: unknown) => {
          inflightRef.current.delete(key)
          const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
          setDirState((s) => ({
            ...s,
            [key]: apiErr.status === 403 ? { status: 'forbidden', error: apiErr } : { status: 'error', error: apiErr },
          }))
        })
    },
    [repoKey, isDockerRepo],
  )

  // T-134 G32a: isDockerRepo 翻转（repoMeta 异步到位）时缓存可能取于无
  // ?docker_tags 的请求——失效重取；切仓同样清缓存（docker 标志是仓级）
  useEffect(() => {
    cacheRef.current.clear()
    inflightRef.current.clear()
    setDirState({})
    setTick((t) => t + 1)
  }, [repoKey, isDockerRepo])

  useEffect(() => {
    // 跨仓根（未选仓）时手动展开的分支照常加载——T-416 收口：virtual twisty
    // 恢复动态展开后「点了箭头永远转骨架」不可接受。该缺口先于本票存在且对
    // 全部 rclass 同形（原 effect 以 !repoKey 早退，expanded 集被一并跳过）；
    // 修复为「选中仓的根 + 祖先链 ∪ 手动展开集」，行为只增不改。
    // T-434：装载链取乐观全路径（末段判别前就并行拉满——目录深链免瀑布）。
    const wanted = new Set<string>(expanded)
    if (repoKey) {
      wanted.add(ck(repoKey, ''))
      for (const d of loadChainKey === '' ? [] : loadChainKey.split('\n')) wanted.add(ck(repoKey, d))
    }
    if (wanted.size === 0) return
    for (const key of wanted) {
      const sep = key.indexOf('\n')
      loadDir(key.slice(0, sep), key.slice(sep + 1))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- chain/expanded 以 join key 刻画
  }, [repoKey, loadChainKey, expandedKey, tick, loadDir])

  const refresh = useCallback(() => {
    cacheRef.current.clear()
    setDirState({})
    setTick((t) => t + 1)
    reposQuery.reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 全量刷新语义
  }, [reposQuery.reload])

  // 切仓/切目录：自动展开祖先链（深链 §4.3）。T-434 / FR-142.2（断言
  // 反转①）：单击仓库名 = 纯选中——dir === '' 不再强制展开仓分支（原
  // selectedRepo 并集 + chain(['']) 双路强制展开解除）；进入子路径（深链/
  // 下钻）才展开仓根 + 祖先链（含被选目录自身——目录选中即见其内容，
  // Artifactory 同形）。展开态只增不减（用户手动展开不因导航被收起）。
  useEffect(() => {
    if (!repoKey) return
    if (dir !== '') {
      setExpanded((prev) => {
        const next = new Set(prev)
        next.add(ck(repoKey, ''))
        for (const d of chain) next.add(ck(repoKey, d))
        return next
      })
    }
    setVisible(PAGE)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- dir/repo 变化即重置
  }, [dir, repoKey, chainKey])

  // 深链滚动定位：祖先链全部就绪后把当前节点（目录/文件叶子/仓库行）滚进可视区
  const chainSettled = !!repoKey && chain.every((d) => dirState[ck(repoKey, d)]?.status === 'ok')
  useEffect(() => {
    if (!chainSettled) return
    const sel =
      focusPath !== null
        ? document.querySelector(`[data-testid="tree-leaf-${CSS.escape(focusPath)}"]`)
        : dir === ''
          ? document.querySelector(`[data-testid="tree-repo-${CSS.escape(repoKey)}"]`)
          : document.querySelector(`[data-testid="tree-node-${CSS.escape(dir)}"]`)
    sel?.scrollIntoView({ block: 'nearest' })
  }, [chainSettled, repoKey, dir, focusPath])

  // ---- 导航 / 选中（URL 即状态：页签段 + 目录与文件都进路径，T-434） ----
  const goTo = useCallback(
    (repo: string, d: string) => {
      navigate(buildTreeUrl(tab, repo, d === '' ? [] : d.split('/').filter(Boolean)))
    },
    [navigate, tab],
  )

  /** 文件选中（null = 收起选中，回当前目录形态）——文件是 URL 路径末段 */
  const selectFile = useCallback(
    (name: string | null) => {
      const segs = dir.split('/').filter(Boolean)
      navigate(buildTreeUrl(tab, repoKey, name ? [...segs, name] : segs))
    },
    [navigate, tab, repoKey, dir],
  )

  /** 页签切换 = URL 段交换（保持当前选择与文件） */
  const goTab = useCallback(
    (t: DetailTab) => {
      navigate(buildTreeUrl(t, repoKey, pathSegs))
    },
    [navigate, repoKey, pathSegs],
  )

  // ---- 过滤 / 分页（§6.3：只作用于已加载集，提示边界） ----
  const [repoFilter, setRepoFilter] = useState('')
  const [filter, setFilter] = useState('')
  const [filesOnly, setFilesOnly] = useState(false)
  const [visible, setVisible] = useState(PAGE)
  useEffect(() => setVisible(PAGE), [filter, filesOnly, dir])

  // ---- 树头工具带状态（T-434 / FR-142.4）----
  // 包类型 facet / rclass 组 / Sort-by 是会话态（视图过滤器）；Compacted
  // 与 My Favorites 是偏好态（localStorage 持久——AC4 reload 保持）。
  // facet 空集 = 不过滤（默认全量；403 合成节点 type/packageType 为空串
  // 不被勾选项误伤——默认即全量）。
  const [pkgFacets, setPkgFacets] = useState<Set<string>>(new Set())
  const [rclassFacets, setRclassFacets] = useState<Set<string>>(new Set())
  const [sortBy, setSortBy] = useState<TreeSort>('name')
  const [compacted, setCompacted] = useState(() => localStorage.getItem(COMPACT_KEY) === '1')
  const [favorites, setFavorites] = useState<Set<string>>(() => loadFavorites())
  const [favOnly, setFavOnly] = useState(false)
  const [facetAnchor, setFacetAnchor] = useState<HTMLElement | null>(null)
  useEffect(() => {
    localStorage.setItem(COMPACT_KEY, compacted ? '1' : '0')
  }, [compacted])
  useEffect(() => {
    localStorage.setItem(FAV_KEY, JSON.stringify(Array.from(favorites).sort()))
  }, [favorites])
  const toggleFavorite = useCallback((repo: string) => {
    setFavorites((prev) => {
      const next = new Set(prev)
      if (next.has(repo)) next.delete(repo)
      else next.add(repo)
      return next
    })
  }, [])

  // 过滤复位（QA-3 / FR-82-AC2，T-246 终验观察；T-434 文件选中不改 dir——
  // 同层语义维持）：「过滤当前层」是对特定 (repo, dir) 已加载 children
  // 集合的谓词——词是用户对着那一层敲进去的。跨层导航（下钻 / 面包屑
  // 上跳 / 左树跳兄弟层）或跨仓切换后，旧词落在一个它从未针对过的新集合
  // 上，命中与否纯属巧合，空结果会被误读成「这一层没有东西」（QA-3 空
  // 树误导）；因此 (repo, dir) 任一维变化即清空。同层内导航（文件选中、
  // 树节点展开/收起）不动 children 集合，词的语义完整——保留。「只看
  // 文件」是跨层稳定的视图偏好、不是针对单层的词，跨层保留（仅它收窄
  // 出的空态文案单独呈现，见下方空态分支）。
  useEffect(() => {
    setFilter('')
  }, [repoKey, dir])
  // 「过滤仓库」在跨仓边界同界复位：进入某仓后保留它，当前仓分支会被从
  // 左树上滤掉（所在位置从树里消失——与 QA-3 同款的误导）；切回跨仓根
  // 也回到全量选择语境。
  useEffect(() => {
    setRepoFilter('')
  }, [repoKey])

  const cur = repoKey ? dirState[ck(repoKey, dir)] : undefined
  // 末段分类未决（父 listing 在途）：children 表与详情都骨架呈现——不闪
  // 目录形态再翻文件形态（错形态期间点页签会产生错误路径的请求）
  const curUncertain = pathSegs.length > 0 && !parentSettled
  const rows = useMemo(() => {
    const nodes = cur?.status === 'ok' ? cur.nodes : []
    const f = filter.trim().toLowerCase()
    return nodes.filter((n) => (filesOnly ? !n.folder : true)).filter((n) => (f ? n.name.toLowerCase().includes(f) : true))
  }, [cur, filter, filesOnly])
  const total = cur?.status === 'ok' ? cur.nodes.length : 0

  // 选中文件（URL 末段对账到父目录 listing 的文件行）
  const selectedNode = fileSelected ? lastChild : undefined

  // ---- 操作：上传 / 建目录 / 删除 / 下载 / 右键菜单 ----
  // 对话框族（T-242）：页头动作区 Set Me Up / Deploy（console-m8 §6.3[1]）。
  // T-244 双 Deploy 入口收敛：面包屑位 tree-upload（UploadDialog）退役，
  // 页头 tree-deploy（DeployDialog）是浏览器上传唯一入口（console-m8
  // §6.3[1] 裁定）；空目录 CTA 与目录上下文经 preselectedDir 带入。
  const [smuOpen, setSmuOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<{ path: string; err: ApiError } | null>(null)
  const [download, setDownload] = useState<DownloadState | null>(null)
  // 右键菜单目标（T-300 批次二迁 MUI Menu：Esc/backdrop 关闭与首项聚焦由
  // Menu 原生承载，坐标钳制仍在前端）
  const [menu, setMenu] = useState<{ x: number; y: number; target: MenuTarget } | null>(null)

  const openMenuAt = useCallback((x: number, y: number, target: MenuTarget) => {
    // 视口边缘钳制（菜单宽 ~200px / 高 ~160px）
    const px = Math.min(x, window.innerWidth - 220)
    const py = Math.min(y, window.innerHeight - 170)
    setMenu({ x: Math.max(8, px), y: Math.max(8, py), target })
  }, [])

  const doDownload = useCallback(
    async (repo: string, node: ChildNode, expectedSha: string) => {
      setDownload({ path: node.path, phase: 'loading', sha: '', match: undefined })
      try {
        const { blob, sha256 } = await downloadArtifact(repo, node.path)
        saveBlob(blob, node.name)
        const match = expectedSha ? expectedSha === sha256 : undefined
        setDownload({ path: node.path, phase: 'done', sha: sha256, match })
        if (match === false) toast.error(tt('下载完成，但 sha256 与服务端不一致（{v1}）', { v1: node.name }))
        else toast.success(tt('下载完成{v1}', { v1: match ? tt('：sha256 与服务端一致') : '' }))
      } catch (err) {
        setDownload(null)
        const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
        toast.error(tt('下载失败（HTTP {v1}）：{v2}', { v1: apiErr.status, v2: apiErr.message }))
      }
    },
    [toast],
  )

  const confirmDelete = useCallback(
    async (repo: string, node: ChildNode) => {
      const isCache = repoMeta?.rclass === 'remote'
      const ok = await confirm({
        title: node.folder ? tt('删除目录') : isCache ? tt('删除缓存') : tt('删除制品'),
        danger: true,
        confirmLabel: tt('删除'),
        body: (
          <p>{tt('将永久删除')} <b className="mono" lang="en">{repo}/{node.path}</b>
            {node.folder ? tt('（目录及其全部内容）') : ''}{tt('。制品不可变，删除没有撤销。')}            {isCache ? tt(' 这是 remote 缓存——删除后下次请求将重新回源。') : ''}
          </p>
        ),
      })
      if (!ok) return
      // T-434：文件选中是 URL 路径末段——删除的节点若是当前选中（文件 =
      // 末段 / 目录 = 当前 dir），选中态随目标一起退役并回跳父目录。
      // 否则 URL 指向已删节点，末段判别会落进 404 空态（「路径不存在」
      // 对刚亲手删除它的用户是误导——?focus= 时代该缺口同样存在，路径
      // 段化后必须收口）。删除前取值（删除后 listing 变迁不影响判定）。
      const wasSelected = repo === repoKey && (node.folder ? dir === node.path : focusFile === node.name)
      const backToParent = () => {
        const parent = node.path.includes('/') ? node.path.slice(0, node.path.lastIndexOf('/')) : ''
        goTo(repo, parent)
      }
      try {
        await deleteNode(repo, node.path, node.folder)
        toast.success(tt('已删除 {v1}', { v1: node.path }))
        setDeleteError(null)
        if (wasSelected) backToParent()
        refresh()
      } catch (err) {
        const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
        if (apiErr.status === 404) {
          // E-14 幂等：路径已不存在 = 目标状态已达成
          toast.success(tt('已删除 {v1}（此前已不存在——删除幂等）', { v1: node.path }))
          if (wasSelected) backToParent()
          refresh()
          return
        }
        setDeleteError({ path: `${repo}/${node.path}`, err: apiErr })
      }
    },
    // dir/focusFile/goTo 是 T-434 选中态回跳的活值（wasSelected 判定）——
    // 不入依赖会钉死在挂载初值（终验探针抓出的真缺陷：URL 永不回跳）
    [confirm, repoMeta, refresh, toast, repoKey, dir, focusFile, goTo],
  )

  const doMkdir = async () => {
    const holder = { name: '' }
    const ok = await confirm({
      title: tt('新建目录'),
      body: (
        <>
          <p>{tt('在')} <b className="mono" lang="en">{dir === '' ? tt('{repoKey}/（根）', { repoKey: repoKey }) : `${repoKey}/${dir}/`}</b> {tt('下创建目录 （尾斜杠 PUT，E-15 mkdir 语义）。')}          </p>
          <div className="field" style={{ marginBottom: 0 }}>
            <input
              className="confirm-input mono"
              autoComplete="off"
              placeholder={tt('目录名')}
              data-testid="tree-mkdir-input"
              onChange={(e) => {
                holder.name = e.target.value
              }}
            />
          </div>
        </>
      ),
      confirmDisabled: () => validateNameSegment(holder.name.trim()) !== null,
    })
    if (!ok) return
    const name = holder.name.trim()
    const target = dir === '' ? `${name}/` : `${dir}/${name}/`
    try {
      await mkdir(repoKey, target)
      toast.success(tt('已创建目录 {target}', { target: target }))
      refresh()
      setExpanded((prev) => new Set(prev).add(ck(repoKey, target.replace(/\/$/, ''))))
    } catch (err) {
      const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
      toast.error(tt('创建目录失败（HTTP {v1}）：{v2}', { v1: apiErr.status, v2: apiErr.message }))
    }
  }

  const commands = repoMeta && packageType && !uploadable ? clientCommands(packageType, repoKey) : []

  // 详情面板目标：选中文件 > 当前目录/仓库 > 无（跨仓根引导）
  const detailTarget = selectedNode
    ? { kind: 'node' as const, repoKey, node: selectedNode }
    : repoKey
      ? dir === ''
        ? { kind: 'repo' as const, repoKey }
        : {
            kind: 'node' as const,
            repoKey,
            node: {
              name: dir.split('/').pop() ?? '',
              path: dir,
              folder: true,
              size: null,
              lastModified: '',
              sha256: '',
            } satisfies ChildNode,
          }
      : null

  // 页脚标语行（§6.3[6]：stats admin 门，非 admin 隐藏）
  const stats = useAsync(() => (admin ? getStorageStats() : Promise.resolve(null)), [admin])

  // ---- 仓库列表（普通 user 403 → 深链合成顶层节点） ----
  const repoNodes: RepoListItem[] = useMemo(() => {
    if (reposQuery.status === 'ok') return repos
    if (reposQuery.status === 'forbidden' && repoKey) {
      return [{ key: repoKey, description: '', type: '', packageType: '', url: '' }]
    }
    return []
  }, [reposQuery.status, repos, repoKey])

  /** 树头工具带的可选包类型集（已加载仓库清单的去重排序——不伪造未实有型） */
  const pkgTypes = useMemo(
    () => Array.from(new Set(repoNodes.map((r) => r.packageType).filter((t) => t !== ''))).sort(),
    [repoNodes],
  )

  // 仓库清单 → 工具带流水线：名称过滤 → 包类型 facet → rclass 组 →
  // My Favorites → Sort-by（ facet 空集 = 不过滤；sort 恒生效，name 稳定）
  const filteredRepos = useMemo(() => {
    const f = repoFilter.trim().toLowerCase()
    const out = repoNodes
      .filter((r) => (f ? r.key.toLowerCase().includes(f) : true))
      .filter((r) => (pkgFacets.size > 0 ? pkgFacets.has(r.packageType) : true))
      .filter((r) => (rclassFacets.size > 0 ? rclassFacets.has(r.type) : true))
      .filter((r) => (favOnly ? favorites.has(r.key) : true))
    return [...out].sort((a, b) => compareRepos(a, b, sortBy))
  }, [repoNodes, repoFilter, pkgFacets, rclassFacets, favOnly, favorites, sortBy])

  // ---- 渲染 ----
  const emptyInstance = reposQuery.status === 'ok' && repos.length === 0 && !repoKey && !onboardSkipped

  return (
    <div data-testid="tree-page" className="tree-page browser-page">
      {/* 页头动作区（console-m8 §6.3[1]：Set Me Up / Deploy / 管理仓库） */}
      <div className="browser-toolbar">
        <div className="browser-actions">
          <Button
            variant="outlined"
            size="small"
            sx={cellBtnSx}
            data-testid="tree-setmeup"
            title={tt('客户端接入向导（按包类型生成接入命令与令牌）')}
            onClick={() => setSmuOpen(true)}
          >
            Set Me Up
          </Button>
          <Button
            variant="contained"
            size="small"
            data-testid="tree-deploy"
            disabled={readOnly}
            title={readOnly ? tt('只读管理员不可写（服务端 403 兜底）') : tt('浏览器上传（local Generic / Maven 仓）')}
            onClick={() => setDeployOpen(true)}
          >{tt('⬆ 部署 Deploy')}          </Button>
          {admin && (
            <Button variant="outlined" size="small" sx={cellBtnSx} component={Link} to="/admin/repositories/local">{tt('管理仓库 →')}            </Button>
          )}
        </div>
      </div>

      {emptyInstance ? (
        <section className="card section">
          <h3>{tt('这个实例还没有仓库')}</h3>
          <p className="text-2">{tt('创建第一个仓库后，全部制品会以跨仓树的形式展示在这里。')}</p>
          <p>
            {admin && (
              <Button variant="contained" size="small" component={Link} to="/admin/repositories/new">{tt('创建仓库')}              </Button>
            )}{' '}
            <Button
              variant="outlined"
              size="small"
              sx={cellBtnSx}
              onClick={() => {
                localStorage.setItem('bf-skip-onboarding', '1')
                setOnboardSkipped(true)
              }}
            >{tt('跳过')}            </Button>
          </p>
        </section>
      ) : (
        <>
          {readOnly && (
            <p className="browser-readonly-note" data-testid="tree-readonly-note">{tt('当前会话是只读管理员（readonly_admin）：浏览面全量可见，上传/删除等写操作已禁用——服务端对写面一律 403 兜底。')}            </p>
          )}

          <div className="tree-breadcrumb" data-testid="tree-breadcrumb">
            {repoKey ? (
              <>
                <button type="button" className="crumb" onClick={() => goTo(repoKey, '')}>
                  <span className="mono" lang="en">{repoKey}</span>
                </button>
                {dir !== '' &&
                  ancestorDirs(dir).map((d) => (
                    <span key={d} className="crumb-seg">
                      <span className="sep" aria-hidden="true">/</span>
                      <button type="button" className={`crumb${d === dir ? ' cur' : ''}`} onClick={() => goTo(repoKey, d)}>
                        <span className="mono" lang="en">{d.split('/').pop()}</span>
                      </button>
                    </span>
                  ))}
                <CopyButton value={dir === '' ? `${repoKey}/` : `${repoKey}/${dir}/`} label={tt('当前路径')} />
              </>
            ) : (
              <span className="text-2">{tt('在左侧选择仓库开始浏览')}</span>
            )}
            <span className="spacer" />
            <Button variant="outlined" size="small" sx={cellBtnSx} onClick={refresh} title={tt('重新加载当前视图')}>{tt('↻ 刷新')}            </Button>
            {mkdirable && (
              <Button
                variant="outlined"
                size="small"
                sx={cellBtnSx}
                data-testid="tree-mkdir"
                disabled={readOnly}
                title={readOnly ? tt('只读管理员不可写（服务端 403 兜底）') : undefined}
                onClick={() => void doMkdir()}
              >{tt('+ 目录')}              </Button>
            )}
          </div>

          {meta.status === 'forbidden' && (
            <div className="warn-box">{tt('仓库元数据为管理员视图（HTTP 403）——树按 generic 语义呈现；上传/删除权限由内容面按路径 ACL 判定，操作被拒时原因会在此原样呈现。')}            </div>
          )}

          {commands.length > 0 && (
            <section className="card section" data-testid="tree-commands">
              <h3>{tt('此协议不走浏览器上传')}</h3>
              <p className="text-2">
                {packageType === 'docker'
                  ? tt('docker 是 POST/PATCH/PUT 三步会话协议——经 docker 客户端推送；登录经 /v2/token（registry 面，与控制台会话无关）。')
                  : tt('{packageType} 的发布协议是 multipart/packument 形态——请用对应客户端发布。', { packageType: packageType })}{tt('以下命令与仓库详情页同源：')}              </p>
              {commands.map((c) => (
                <div className="cmd-block" key={c.title}>
                  <header>
                    <span>{c.title}</span>
                    <CopyButton value={c.text} label={c.title} />
                  </header>
                  {/* tabIndex：可滚动区键盘可达（QA-1 同款，树页命令块同形面） */}
                  <pre lang="en" tabIndex={0}>{c.text}</pre>
                  {c.note && <div className="note">{c.note}</div>}
                </div>
              ))}
            </section>
          )}

          {repoMeta && rclass === 'virtual' && (
            <div className="warn-box">{tt('virtual 仓是聚合视图：浏览合并各成员；上传/删除不经 virtual（删除走成员仓本身——BinFlow 有意不兼容，RE-08）。')}            </div>
          )}
          {repoMeta && rclass === 'remote' && (
            <div className="warn-box" data-testid="tree-remote-note">
              {remoteBrowseOn
                ? tt('远端浏览已开启（listRemoteFolderItems）：树包含上游未缓存的目录与条目（按 metadata TTL 缓存枚举）；点击未缓存条目会回源拉取并落地缓存。')
                : tt('remote 仓浏览的是已缓存内容；首次访问的路径需经客户端拉取后才会出现在树上（可在仓库配置开启远端浏览 listRemoteFolderItems）。')}
            </div>
          )}

          {deleteError && (
            <Alert
              severity="error"
              data-testid="delete-error"
              sx={{ mt: 2, '& .MuiAlert-message': { width: '100%' } }}
            >
              <div className="headline">{tt('删除')} <span className="mono" lang="en">{deleteError.path}</span> {tt('失败（HTTP')} {deleteError.err.status}{tt('）')}              </div>
              <div className="raw" lang="en">{deleteError.err.message}</div>
              {deleteError.err.status === 403 && (
                <div>{tt('当前会话没有对该路径的 delete 权限（read-only）。权限模型按 permission target 的路径 pattern 授予——')}                  {admin ? (
                    <>{tt('需要管理员在')}{' '}
                      <Link to="/admin/security/permissions" target="_blank">{tt('权限 target')}                      </Link>{' '}{tt('里给该路径加 delete 动作。')}                    </>
                  ) : (
                    <>{tt('请联系管理员为你的账号授予该路径的 delete 动作。')}</>
                  )}
                </div>
              )}
              <div style={{ marginTop: 8 }}>
                <Button variant="outlined" size="small" sx={cellBtnSx} onClick={() => setDeleteError(null)}>{tt('知道了')}                </Button>
              </div>
            </Alert>
          )}

          <div className="tree-layout browser-layout">
            {/* 左树：树头工具带（T-434 / FR-142.4）+ 仓库顶层 + 懒展开子树
                （reverse §3.2/§4.3）。工具带在滚动区外常驻（带不随树滚走）。 */}
            <nav className={`tree-pane browser-tree${compacted ? ' compacted' : ''}`} aria-label={tt('制品树')} data-testid="browser-tree">
              <TreeToolband
                repoFilter={repoFilter}
                onRepoFilter={setRepoFilter}
                reposStatus={reposQuery.status}
                pkgTypes={pkgTypes}
                pkgFacets={pkgFacets}
                onTogglePkg={(t) =>
                  setPkgFacets((prev) => {
                    const next = new Set(prev)
                    if (next.has(t)) next.delete(t)
                    else next.add(t)
                    return next
                  })
                }
                onClearPkg={() => setPkgFacets(new Set())}
                rclassFacets={rclassFacets}
                onToggleRclass={(rc) =>
                  setRclassFacets((prev) => {
                    const next = new Set(prev)
                    if (next.has(rc)) next.delete(rc)
                    else next.add(rc)
                    return next
                  })
                }
                sortBy={sortBy}
                onSort={setSortBy}
                compacted={compacted}
                onCompacted={setCompacted}
                favCount={favorites.size}
                favOnly={favOnly}
                onFavOnly={setFavOnly}
                facetAnchor={facetAnchor}
                onFacetAnchor={setFacetAnchor}
              />
              <div className="browser-tree-scroll" role="tree" aria-label={tt('跨仓制品树')}>
                {reposQuery.status === 'loading' && <TreeSkeleton />}
                {reposQuery.status === 'error' && reposQuery.error && (
                  <ErrorCard error={reposQuery.error} onRetry={reposQuery.reload} />
                )}
                {reposQuery.status === 'forbidden' && !repoKey && (
                  <EmptyState
                    message={tt('无权限列出仓库')}
                    hint={tt('仓库清单是管理员/只读管理员视图（HTTP 403）。可以用搜索定位制品，或用已知仓库 key 的链接直达。')}
                    testid="tree-root-denied"
                    action={
                      <Button variant="outlined" size="small" sx={cellBtnSx} component={Link} to="/search">{tt('去搜索')}                      </Button>
                    }
                  />
                )}
                {(reposQuery.status === 'ok' || reposQuery.status === 'forbidden') &&
                  (filteredRepos.length === 0 ? (
                    <div className="tree-empty-level">
                      {repoFilter ? tt('没有匹配「{repoFilter}」的仓库（仅过滤已加载集）', { repoFilter: repoFilter }) : tt('（无仓库）')}
                    </div>
                  ) : (
                    filteredRepos.map((r) => (
                      <RepoBranch
                        key={r.key}
                        repo={r}
                        selectedRepo={repoKey}
                        currentDir={dir}
                        focusPath={focusPath}
                        dirState={dirState}
                        expanded={expanded}
                        onToggle={(repo, d) =>
                          setExpanded((prev) => {
                            const next = new Set(prev)
                            const key = ck(repo, d)
                            if (next.has(key)) next.delete(key)
                            else next.add(key)
                            return next
                          })
                        }
                        onNavigate={goTo}
                        onMenu={openMenuAt}
                      />
                    ))
                  ))}
                {/* 树尾常驻回收站入口（T-372 / FR-122.1）：console-m8 §4.3
                    推翻条款的兑现面（推翻留痕在该节）；reverse §3.2 末尾
                    常驻形态。最小面 = 跳转 M12 页面；不参与过滤仓库的
                    过滤域（常驻 ≠ 已加载仓库集成员）；工具带 facet 同样
                    不滤它（常驻语义） */}
                {canSeeAdmin && <TrashTreeNode onOpen={() => navigate('/admin/governance/trash')} />}
              </div>
            </nav>

            {/* 右列：详情面板（右联，页签态进 URL——T-434）+ 当前层 children 表 */}
            <div className="tree-main">
              {curUncertain ? (
                // 末段分类未决（父 listing 在途）：骨架占位——文件/目录形态
                // 判定前不渲染详情（错形态期间的页签点击会打错误路径请求）
                <div data-testid="skeleton" aria-hidden="true" style={{ paddingTop: 8 }}>
                  {Array.from({ length: 4 }, (_, i) => (
                    <MuiSkeleton key={i} variant="text" width={`${88 - i * 8}%`} sx={{ my: 0.5 }} />
                  ))}
                </div>
              ) : detailTarget && repoKey ? (
                <NodeDetail
                  target={detailTarget}
                  download={download}
                  onDownload={(n, sha) => void doDownload(repoKey, n, sha)}
                  onClose={() => selectFile(null)}
                  canDelete={!readOnly && !isVirtual}
                  canWriteProps={!readOnly}
                  onDelete={(n) => void confirmDelete(repoKey, n)}
                  tab={tab}
                  onTabChange={goTab}
                  // 目录形态的直系概要（Artifact Count / Size——FR-142.1：
                  // children 表收窄后目录选中给概要；数据源 = 已装载 listing）
                  childrenNodes={
                    detailTarget.kind === 'node' && detailTarget.node.folder && cur?.status === 'ok' ? cur.nodes : undefined
                  }
                />
              ) : (
                <section className="card section node-detail">
                  <h3>{tt('制品浏览器')}</h3>
                  <p className="text-2">{tt('左侧是全部仓库的树：单击选中（此处联动详情），展开箭头（或 → 键）展开下一层——目录与文件都出现在树里。 选中路径与页签都会进入 URL——可以直接分享或收藏深链，打开时自动展开定位。')}                  </p>
                  <p className="text-2">{tt('右键（或')} <kbd>Shift+F10</kbd>{tt('）打开操作菜单：复制路径 / 下载 / 删除 / 刷新。')}                  </p>
                </section>
              )}

              {repoKey && (
                <>
                  <div className="filter-bar">
                    <TextField
                      type="search"
                      size="small"
                      placeholder={tt('过滤当前层（仅已加载集）…')}
                      value={filter}
                      onChange={(e) => setFilter(e.target.value)}
                      sx={{ ...monoInputSx, width: 240 }}
                      slotProps={{ htmlInput: { 'data-testid': 'tree-filter', 'aria-label': tt('过滤当前层'), className: 'mono' } }}
                    />
                    <FormControlLabel
                      className="check-row"
                      control={
                        <Checkbox
                          size="small"
                          checked={filesOnly}
                          onChange={(e) => setFilesOnly(e.target.checked)}
                        />
                      }
                      label={tt('只看文件')}
                    />
                    <span className="count">
                      {total > 0 && <>{tt('共')} {total} {tt('项 · 已显示')} {Math.min(visible, rows.length)}</>}
                    </span>
                  </div>

                  {/* T-461（FR-147 AC3）：远端枚举层错误态——上游不可达/静默期
                      时 listing 仍 200（缓存行不整树塌，§4-1），note 经 FolderInfo
                      可选字段 remoteDegraded 上 wire（T-448 §5-2 缝——渲染腿在途
                      时字段缺席，本横幅不渲染，零行为影响）。 */}
                  {cur?.status === 'ok' && cur.remoteDegraded && (
                    <div className="warn-box" data-testid="tree-remote-degraded" title={cur.remoteDegraded}>{tt('⚠ 远端枚举不可用（上游故障或 assumed-offline 静默期）——已缓存条目仍可用；未缓存条目暂不可见。')}                      <span className="mono" lang="en" style={{ fontSize: 'var(--bf-fs-aux, 12px)' }}> {cur.remoteDegraded}</span>
                    </div>
                  )}

                  {curUncertain || cur?.status === 'loading' || !cur ? (
                    <TableSkeleton />
                  ) : cur.status === 'forbidden' ? (
                    <EmptyState
                      message={tt('无权限浏览此目录')}
                      hint={tt('内容面按路径 ACL 判定（{v1}）。可回到有权限的层级，或用搜索定位制品。', { v1: cur.error.message })}
                      action={
                        <Button variant="outlined" size="small" sx={cellBtnSx} component={Link} to="/search">{tt('去搜索')}                        </Button>
                      }
                    />
                  ) : cur.status === 'error' && cur.error.status === 404 ? (
                    <EmptyState
                      message={tt('路径不存在')}
                      hint={tt('节点可能已被删除，或链接里的路径有误。')}
                      action={
                        <Button variant="outlined" size="small" sx={cellBtnSx} onClick={() => goTo(repoKey, '')}>{tt('← 回仓库根')}                        </Button>
                      }
                    />
                  ) : cur.status === 'error' && cur.error ? (
                    <ErrorCard error={cur.error} onRetry={refresh} />
                  ) : rows.length === 0 ? (
                    total === 0 ? (
                      isVirtual ? (
                        // T-416 断言反转②：T-406 的「聚合浏览暂未支持」空态翻转
                        // 为「成员并集为空」——有成员内容时不再空态（走上方正常
                        // 表格分支）；无成员/全空维持空态，文案按两态区分（成员
                        // 清单读自 configuration.repositories 规范回显）。锚
                        // tree-empty-virtual 保留、语义随票翻转（锚册 v1.28）。
                        <EmptyState
                          message={dir === '' ? tt('虚拟仓库：暂无聚合内容') : tt('此路径在成员仓库中无内容')}
                          hint={
                            dir === ''
                              ? virtualMembers.length
                                ? tt('成员 {v1} 当前均无内容——成员仓库有制品后会聚合到这里（若成员已被删除，请在仓库管理中更新成员列表）。', { v1: virtualMembers.join(tt('、')) })
                                : tt('未配置成员仓库——在仓库管理中配置成员后，成员内容会聚合到这里。')
                              : tt('虚拟仓库按成员并集浏览，此路径下没有任何成员的内容。')
                          }
                          testid="tree-empty-virtual"
                        />
                      ) : (
                        <EmptyState
                          message={tt('此目录为空')}
                          hint={
                            uploadable
                              ? tt('上传第一个制品，或创建子目录组织布局。')
                              : rclass === 'remote'
                                ? remoteBrowseOn
                                  ? tt('远程仓库：缓存与远端枚举在此层均无条目。')
                                  : tt('远程仓库：仅展示已缓存的制品（浏览不回源）。')
                                : tt('此仓库尚无内容。')
                          }
                          testid="tree-empty-dir"
                          action={
                            uploadable && !readOnly ? (
                              <Button
                                variant="contained"
                                size="small"
                                onClick={() => {
                                  setDeployOpen(true)
                                }}
                              >{tt('上传第一个制品')}                              </Button>
                            ) : undefined
                          }
                        />
                      )
                    ) : filter.trim() !== '' ? (
                      // QA-3 / §3.1 空态：过滤后为空 ≠ 这一层没有内容——标准
                      // 文案 + 清除过滤（连「只看文件」一并复位，保证非空回呈现）
                      <EmptyState
                        message={tt('无匹配「{filter}」的条目', { filter: filter })}
                        hint={
                          filesOnly
                            ? tt('过滤只作用于当前层已加载的条目，且「只看文件」正在一并收窄范围。')
                            : tt('过滤只作用于当前层已加载的条目。')
                        }
                        action={
                          <Button
                            variant="outlined"
                            size="small"
                            sx={cellBtnSx}
                            data-testid="tree-filter-clear"
                            onClick={() => {
                              setFilter('')
                              setFilesOnly(false)
                            }}
                          >{tt('清除过滤')}                          </Button>
                        }
                      />
                    ) : (
                      // 仅「只看文件」收窄出的空（无过滤词）：按实际谓词呈现，
                      // 不误报「无匹配」
                      <EmptyState
                        message={tt('当前层没有文件（只有目录）')}
                        hint={tt('「只看文件」正在收窄列表。')}
                        action={
                          <Button
                            variant="outlined"
                            size="small"
                            sx={cellBtnSx}
                            data-testid="tree-filter-clear"
                            onClick={() => setFilesOnly(false)}
                          >{tt('清除「只看文件」')}                          </Button>
                        }
                      />
                    )
                  ) : (
                    <>
                      {total > BIG_DIR && (
                        <div className="warn-box">{tt('目录过大（')}{total} {tt('项）：本页为客户端分页（children 契约暂无游标）。建议改用')}{' '}
                          <Link to="/search">{tt('搜索')}</Link> {tt('定位制品。')}                        </div>
                      )}
                      {/* children 表（T-434 / FR-142.1 收窄）：文件主路径走树叶子
                          与本表行选中；原操作列（详情/下载/删除）退役——下载在
                          右键菜单与详情面板，删除收敛进详情面板（危险确认门），
                          Q2 出口①：E1 不倒退（确认流原样，仅入口收拢）。 */}
                      <Table className="tree-table" data-testid="tree-list">
                        <TableHead>
                          <TableRow>
                            <TableCell component="th" scope="col">{tt('名称')}</TableCell>
                            {isDockerRepo ? <TableCell component="th" scope="col">{tt('标签')}</TableCell> : <TableCell component="th" scope="col">{tt('类型')}</TableCell>}
                            <TableCell component="th" scope="col">{tt('大小')}</TableCell>
                            <TableCell component="th" scope="col">{tt('修改时间')}</TableCell>
                            <TableCell component="th" scope="col">{isDockerRepo ? tt('摘要') : 'sha256'}</TableCell>
                          </TableRow>
                        </TableHead>
                        <TableBody>
                          {rows.slice(0, visible).map((n) => (
                            <TableRow
                              key={n.name}
                              data-testid={`tree-row-${n.name}`}
                              className={focusFile === n.name ? 'selected' : ''}
                              hover
                              onClick={() => (n.folder ? goTo(repoKey, n.path) : selectFile(n.name))}
                              onContextMenu={(e: ReactMouseEvent) => {
                                e.preventDefault()
                                openMenuAt(e.clientX, e.clientY, { kind: 'node', repoKey, node: n })
                              }}
                              tabIndex={0}
                              onKeyDown={(e) => {
                                if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
                                  e.preventDefault()
                                  const rect = e.currentTarget.getBoundingClientRect()
                                  openMenuAt(rect.left + 60, rect.top + 14, { kind: 'node', repoKey, node: n })
                                } else if (e.key === 'Enter') {
                                  if (n.folder) goTo(repoKey, n.path)
                                  else selectFile(n.name)
                                }
                              }}
                            >
                              <TableCell sx={{ whiteSpace: 'normal', wordBreak: 'break-word' }}>
                                <span aria-hidden="true">{n.folder ? '◻' : '◾'}</span>{' '}
                                <span className={n.folder ? 'row-link mono' : 'mono'} lang="en">
                                  {n.name}
                                </span>
                                {/* T-461：远端派生行标记——display-only（无 digest/
                                    size/mtime，列呈现 '—'），点击即回源（元数据面
                                    GET 触发 pull-through，成功后落地成缓存行） */}
                                {n.remote && (
                                  <Chip
                                    size="small"
                                    variant="outlined"
                                    color="info"
                                    label={tt('远端')}
                                    data-testid="tree-row-uncached"
                                    title={tt('上游枚举的未缓存条目——点击将回源拉取（成功后落地缓存）')}
                                    sx={{ ml: 0.75, verticalAlign: 'middle' }}
                                  />
                                )}
                              </TableCell>
                              {isDockerRepo ? (
                                <TableCell>
                                  {n.tags && n.tags.length > 0
                                    ? n.tags.map((tag) => (
                                        <Chip
                                          key={tag}
                                          size="small"
                                          className="badge neutral"
                                          label={tag}
                                          data-testid={`tag-badge-${tag}`}
                                          title={`tag: ${tag}`}
                                        />
                                      ))
                                    : !n.folder
                                      ? <Chip size="small" variant="outlined" color="warning" className="badge warning" label="untagged" />
                                      : '—'}
                                </TableCell>
                              ) : (
                                <TableCell>{n.folder ? tt('目录') : tt('文件')}</TableCell>
                              )}
                              <TableCell className="mono">{n.folder ? '—' : n.size !== null ? formatBytes(n.size) : '—'}</TableCell>
                              <TableCell className="mono">{n.lastModified ? n.lastModified.replace('T', ' ').slice(0, 19) : '—'}</TableCell>
                              <TableCell className="mono" title={n.sha256}>
                                {n.sha256 ? `${n.sha256.slice(0, 10)}…` : '—'}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                      {visible < rows.length && (
                        <div style={{ marginTop: 12 }}>
                          <Button
                            variant="outlined"
                            size="small"
                            sx={cellBtnSx}
                            data-testid="tree-load-more"
                            onClick={() => setVisible((v) => v + PAGE)}
                          >{tt('加载更多（')}{Math.min(visible, rows.length)}/{rows.length}{tt('）')}                          </Button>
                        </div>
                      )}
                    </>
                  )}
                </>
              )}
            </div>
          </div>

          {/* 页脚标语行（§6.3[6]，对齐 Artifactory "Happily serving" 行） */}
          {stats.status === 'ok' && stats.data && (
            <p className="browser-footer">{tt('已服务')} {stats.data.blobs.toLocaleString()} {tt('个 blob · 逻辑容量')} {formatBytes(stats.data.logical_bytes)}
            </p>
          )}
        </>
      )}

      {menu && (
        <TreeContextMenu
          menu={menu}
          readOnly={readOnly}
          canSeeAdmin={admin}
          repoRclass={repoMeta?.rclass}
          // T-416：菜单目标可能是左树上任一仓（非当前仓）——虚拟判定按目标
          // repoKey 查仓库清单，而非当前仓 rclass。清单 403（普通 user 深链）
          // 时查不到 → 不预收敛，交给服务端 405 原样呈现。
          targetVirtual={repoNodes.some((r) => r.key === menu.target.repoKey && r.type === 'virtual')}
          // T-434 / FR-142.4：My Favorites（前端态收藏，reverse §3.2 仓库
          // 右键 Add to Favorites 的对位——标记入口在仓库菜单）
          targetFavorite={favorites.has(menu.target.repoKey)}
          onToggleFavorite={toggleFavorite}
          onClose={() => setMenu(null)}
          onCopyPath={(value) => {
            void navigator.clipboard.writeText(value).then(
              () => toast.success(tt('已复制 {value}', { value: value })),
              () => toast.error(tt('剪贴板不可用')),
            )
          }}
          onDownload={(repo, node) => void doDownload(repo, node, node.sha256)}
          onDelete={(repo, node) => void confirmDelete(repo, node)}
          onRefresh={(repo) => {
            setMenu(null)
            if (repo === repoKey) refresh()
            else loadDir(repo, '', true)
          }}
          onOpenAdmin={(repo) => {
            setMenu(null)
            navigate(`/admin/repositories/${encodeURIComponent(repo)}`)
          }}
        />
      )}

      {smuOpen && (
        <SetMeUpDialog preselectedRepo={repoKey || undefined} onClose={() => setSmuOpen(false)} />
      )}
      {deployOpen && (
        <DeployDialog
          preselectedRepo={repoKey || undefined}
          preselectedDir={dir || undefined}
          onClose={() => setDeployOpen(false)}
          onUploaded={refresh}
        />
      )}
    </div>
  )
}

// ---- 左树：仓库顶层节点（rclass×packageType 图标区分，console-m8 C2） --------

// T-390（FR-127）：包型角标走 PkgIcon mono（currentColor 随 .ico 的
// text-2 色——原 PKG_ICON 五枚几何字符表退役）；rclass 角标仍是字符
//（30 枚集不含 rclass 形）。
const RC_ICON: Record<string, string> = { local: '▣', remote: '◈', virtual: '◍' }

// ---- 树头工具带（T-434 / FR-142.4——Artifactory 树头对齐面）------------------
//
// reverse §3.2 回填后的 Artifactory 形态：过滤输入 + Clear + Tree View 单选
// （Compacted/Non-Compacted）+ Filter-by-Package-Type 复选组 + Local/Remote/
// Cache/Virtual 组 + Sort-by。BinFlow 语义映射：rclass 组为实有三态
// local/remote/virtual（remote 浏览面本就是缓存落地行——Artifactory 的
// Cache 是 remote 的缓存子集视图，不伪造第四态）；包类型 facet 的选项集
// 取自已加载仓库清单（13 型实有，不伪造未实现型）。控件沿 MUI small 档
// （mui-native-visual §7 密度）。

const RCLASS_FACETS: { id: string; label: string }[] = [
  { id: 'local', label: 'Local' },
  { id: 'remote', label: 'Remote' },
  { id: 'virtual', label: 'Virtual' },
]

function TreeToolband({
  repoFilter,
  onRepoFilter,
  reposStatus,
  pkgTypes,
  pkgFacets,
  onTogglePkg,
  onClearPkg,
  rclassFacets,
  onToggleRclass,
  sortBy,
  onSort,
  compacted,
  onCompacted,
  favCount,
  favOnly,
  onFavOnly,
  facetAnchor,
  onFacetAnchor,
}: {
  repoFilter: string
  onRepoFilter: (v: string) => void
  reposStatus: string
  pkgTypes: string[]
  pkgFacets: Set<string>
  onTogglePkg: (t: string) => void
  onClearPkg: () => void
  rclassFacets: Set<string>
  onToggleRclass: (rc: string) => void
  sortBy: TreeSort
  onSort: (s: TreeSort) => void
  compacted: boolean
  onCompacted: (v: boolean) => void
  favCount: number
  favOnly: boolean
  onFavOnly: (v: boolean) => void
  facetAnchor: HTMLElement | null
  onFacetAnchor: (el: HTMLElement | null) => void
}) {
  return (
    <div className="tree-toolband" data-testid="tree-toolband">
      <div className="toolband-row">
        <TextField
          type="search"
          size="small"
          placeholder={tt('过滤仓库…')}
          value={repoFilter}
          onChange={(e) => onRepoFilter(e.target.value)}
          disabled={reposStatus !== 'ok'}
          sx={{ width: 152 }}
          slotProps={{ htmlInput: { 'data-testid': 'tree-repo-filter', 'aria-label': tt('过滤仓库（仅已加载集）') } }}
        />
        {repoFilter && (
          <Button
            variant="outlined"
            size="small"
            sx={cellBtnSx}
            data-testid="tree-repo-filter-clear"
            onClick={() => onRepoFilter('')}
          >{tt('清除')}          </Button>
        )}
        <Button
          variant="outlined"
          size="small"
          sx={cellBtnSx}
          data-testid="tree-favorites"
          aria-pressed={favOnly}
          title={tt('只看收藏的仓库（收藏经仓库右键菜单标记，浏览器本地持久）')}
          onClick={() => onFavOnly(!favOnly)}
        >
          {favOnly ? '★' : '☆'} My Favorites{favCount > 0 ? tt('（{favCount}）', { favCount: favCount }) : ''}
        </Button>
      </div>
      <div className="toolband-row">
        <Button
          variant="outlined"
          size="small"
          sx={cellBtnSx}
          aria-haspopup="dialog"
          title={tt('按包类型过滤仓库树（复选组，空 = 不过滤）')}
          data-testid="tree-facet-pkg"
          onClick={(e) => onFacetAnchor(e.currentTarget)}
        >{tt('包类型')}{pkgFacets.size > 0 ? tt('（{v1}）', { v1: pkgFacets.size }) : ''} ▾
        </Button>
        {RCLASS_FACETS.map((rc) => (
          <FormControlLabel
            key={rc.id}
            className="check-row"
            control={
              <Checkbox
                size="small"
                checked={rclassFacets.has(rc.id)}
                onChange={() => onToggleRclass(rc.id)}
                slotProps={{ input: { 'data-testid': `tree-facet-rclass-${rc.id}` } as ComponentPropsWithoutRef<'input'> }}
              />
            }
            label={<span lang="en">{rc.label}</span>}
            title={tt('仓库类型 {v1} 过滤（空选 = 不过滤）', { v1: rc.label })}
          />
        ))}
      </div>
      <div className="toolband-row">
        <TextField
          select
          size="small"
          label={tt('排序')}
          value={sortBy}
          onChange={(e) => onSort(e.target.value as TreeSort)}
          sx={{ width: 128 }}
          slotProps={{
            select: {
              native: true,
              inputProps: {
                'aria-label': tt('树排序（Sort by）'),
                'data-testid': 'tree-sort-by',
              } as ComponentPropsWithoutRef<'select'>,
            } as ComponentPropsWithoutRef<typeof Select>,
          }}
        >
          <option value="name">{tt('名称')}</option>
          <option value="pkg">{tt('包类型')}</option>
          <option value="rclass">{tt('仓库类型')}</option>
        </TextField>
        {/* Tree View 单选：Compacted（紧凑行高）/ Non-Compacted（标准） */}
        <RadioGroup
          row
          aria-label={tt('树视图密度（Tree View）')}
          value={compacted ? '1' : '0'}
          onChange={(e) => onCompacted(e.target.value === '1')}
          sx={{ gap: 0.5 }}
        >
          <FormControlLabel
            value="1"
            control={<Radio size="small" slotProps={{ input: { 'data-testid': 'tree-view-compacted' } as ComponentPropsWithoutRef<'input'> }} />}
            label={<span title={tt('紧凑行高（Compacted）')}>{tt('紧凑')}</span>}
          />
          <FormControlLabel
            value="0"
            control={<Radio size="small" slotProps={{ input: { 'data-testid': 'tree-view-normal' } as ComponentPropsWithoutRef<'input'> }} />}
            label={<span title={tt('标准行高（Non-Compacted）')}>{tt('标准')}</span>}
          />
        </RadioGroup>
      </div>
      <Popover
        open={facetAnchor !== null}
        anchorEl={facetAnchor ?? undefined}
        onClose={() => onFacetAnchor(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        slotProps={{
          paper: {
            'data-testid': 'tree-facet-pkg-panel',
            sx: { border: '1px solid var(--bf-border)', p: 'var(--bf-sp-2)', maxWidth: 260 },
          } as ComponentPropsWithoutRef<'div'>,
        }}
      >
        <div className="toolband-facet-title" lang="en">Filter by Package Type</div>
        {pkgTypes.length === 0 ? (
          <p className="text-2" style={{ margin: 0 }}>{tt('（已加载集中没有带包类型的仓库）')}</p>
        ) : (
          pkgTypes.map((t) => (
            <FormControlLabel
              key={t}
              className="check-row"
              control={
                <Checkbox
                  size="small"
                  checked={pkgFacets.has(t)}
                  onChange={() => onTogglePkg(t)}
                  slotProps={{ input: { 'data-testid': `tree-facet-pkg-${t}` } as ComponentPropsWithoutRef<'input'> }}
                />
              }
              label={<span lang="en">{t}</span>}
            />
          ))
        )}
        {pkgFacets.size > 0 && (
          <Button variant="outlined" size="small" sx={cellBtnSx} data-testid="tree-facet-pkg-clear" onClick={onClearPkg}>{tt('清除（')}{pkgFacets.size}{tt('）')}          </Button>
        )}
      </Popover>
    </div>
  )
}

function RepoBranch({
  repo,
  selectedRepo,
  currentDir,
  focusPath,
  dirState,
  expanded,
  onToggle,
  onNavigate,
  onMenu,
}: {
  repo: RepoListItem
  selectedRepo: string
  currentDir: string
  /** 选中文件的完整路径（文件选中态传给叶子行；null = 无文件选中） */
  focusPath: string | null
  dirState: Record<string, DirStatus>
  expanded: Set<string>
  onToggle: (repo: string, dir: string) => void
  onNavigate: (repo: string, dir: string) => void
  onMenu: (x: number, y: number, target: MenuTarget) => void
}) {
  const key = ck(repo.key, '')
  // T-416（FR-136.3）：T-406 的 virtual 静态化解除——服务端聚合面（T-412）
  // 就绪，virtual 仓与非 virtual 仓同形：twisty 动态展开 + 子级区
  //（children = 成员并集，深层递归与 local/remote 一致）。
  // T-434 / FR-142.2（断言反转①）：isOpen 只由 expanded 集（展开箭头/键盘
  // → / 深链祖先链）决定——selectedRepo 并集解除，单击仓库名 = 纯选中。
  const isOpen = expanded.has(key)
  const st = dirState[key]

  return (
    <div>
      <div
        className={`tree-node repo-node${selectedRepo === repo.key ? ' on-chain' : ''}`}
        data-testid={`tree-repo-${repo.key}`}
        data-tree-row=""
        role="treeitem"
        aria-expanded={isOpen}
        aria-level={1}
        tabIndex={0}
        onClick={() => onNavigate(repo.key, '')}
        onContextMenu={(e: ReactMouseEvent) => {
          e.preventDefault()
          onMenu(e.clientX, e.clientY, { kind: 'repo', repoKey: repo.key })
        }}
        onKeyDown={(e) => onTreeKeys(e, { repo: repo.key, dir: '', isOpen }, onToggle, onNavigate, onMenu)}
      >
        <span
          role="button"
          tabIndex={-1}
          aria-label={isOpen ? tt('收起 {v1}', { v1: repo.key }) : tt('展开 {v1}', { v1: repo.key })}
          className="twisty"
          onClick={(e) => {
            e.stopPropagation()
            onToggle(repo.key, '')
          }}
        >
          {isOpen ? '▾' : '▸'}
        </span>
        <span aria-hidden="true" className="ico" title={`${repo.type || tt('仓库')} · ${repo.packageType || tt('未知包类型')}`}>
          {RC_ICON[repo.type] ?? '▣'}
          <PkgIcon id={repo.packageType || 'generic'} variant="mono" size={14} className="tree-pkg" />
        </span>
        <span className="mono" lang="en">{repo.key}</span>
      </div>
      {isOpen &&
        (!st || st.status === 'loading' ? (
          <div className="tree-skel" aria-hidden="true">
            {Array.from({ length: 6 }, (_, i) => (
              <MuiSkeleton key={i} variant="text" width={`${70 - i * 6}%`} sx={{ my: 0.5 }} />
            ))}
          </div>
        ) : st.status === 'forbidden' ? (
          <div className="tree-denied" title={st.error.message}>{tt('⃠ 无权限')}          </div>
        ) : st.status === 'error' ? (
          <div className="tree-denied" title={st.error.message}>{tt('加载失败（HTTP')} {st.error.status}{tt('）')}          </div>
        ) : (
          <TreeLevel
            repoKey={repo.key}
            dirPath=""
            depth={1}
            currentRepo={selectedRepo}
            currentDir={currentDir}
            focusPath={focusPath}
            dirState={dirState}
            expanded={expanded}
            onToggle={onToggle}
            onNavigate={onNavigate}
            onMenu={onMenu}
          />
        ))}
    </div>
  )
}

// ---- 左树：树尾常驻回收站入口（T-372 / FR-122.1） ---------------------------
//
// console-m8 §4.3 原「Trash Can 常驻节点不建」条款被 M12 FR-106 推翻后的
// 兑现面（推翻留痕见该节）；Artifactory 树浏览器末尾常驻 Trash Can 的对齐
// 形态（reverse §3.2）。最小面：入口跳转 /admin/governance/trash——页身
// （浏览/恢复/清除/清空）全部沿用 M12 T-352 页面，本节点零数据请求、零
// 自有状态。可见性与管理壳同门（admin ∪ readonly_admin），普通 user 不
// 渲染（§2.2 管理面可见性纪律）。键盘：↑↓ 沿树行 DOM 序移动、Enter/Space
// 激活；叶节点无展开语义（→/← 不响应，twisty 位以空槽占位对齐栅格）。

function TrashTreeNode({ onOpen }: { onOpen: () => void }) {
  const onKeys = (e: ReactKeyboardEvent<HTMLElement>) => {
    const rows = Array.from(document.querySelectorAll<HTMLElement>('[data-tree-row]'))
    const idx = rows.indexOf(e.currentTarget)
    if (e.key === 'ArrowDown' && rows[idx + 1]) {
      e.preventDefault()
      rows[idx + 1].focus()
    } else if (e.key === 'ArrowUp' && rows[idx - 1]) {
      e.preventDefault()
      rows[idx - 1].focus()
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onOpen()
    }
  }
  return (
    <div
      className="tree-node repo-node trash-node"
      data-testid="tree-trash-node"
      data-tree-row=""
      role="treeitem"
      aria-level={1}
      tabIndex={0}
      title={tt('回收站（local 仓删除捕获——恢复 / 永久清除 / 清空，管理页）')}
      onClick={onOpen}
      onKeyDown={onKeys}
    >
      <span aria-hidden="true" className="twisty" />
      <span aria-hidden="true" className="ico">🗑</span>
      <span>{tt('回收站')}</span>
    </div>
  )
}

// ---- 左树：目录层（只渲染已展开路径 + 手动展开层——懒加载一层；T-434 起
// 目录与文件叶子同行渲染——排序沿用 sortChildren 的目录在前 + 同组按名） ----

function TreeLevel({
  repoKey,
  dirPath,
  depth,
  currentRepo,
  currentDir,
  focusPath,
  dirState,
  expanded,
  onToggle,
  onNavigate,
  onMenu,
}: {
  repoKey: string
  dirPath: string
  depth: number
  currentRepo: string
  currentDir: string
  focusPath: string | null
  dirState: Record<string, DirStatus>
  expanded: Set<string>
  onToggle: (repo: string, dir: string) => void
  onNavigate: (repo: string, dir: string) => void
  onMenu: (x: number, y: number, target: MenuTarget) => void
}) {
  const st = dirState[ck(repoKey, dirPath)]
  if (!st || st.status === 'loading') {
    return (
      <div className="tree-skel" aria-hidden="true">
        {Array.from({ length: Math.min(6, 15 - depth * 2) }, (_, i) => (
          <MuiSkeleton key={i} variant="text" width={`${76 - depth * 10 - i * 6}%`} sx={{ my: 0.5 }} />
        ))}
      </div>
    )
  }
  if (st.status === 'forbidden') {
    return (
      <div className="tree-denied" title={st.error.message}>{tt('⃠ 无权限')}      </div>
    )
  }
  if (st.status === 'error') {
    return (
      <div className="tree-denied" title={st.error.message}>{tt('加载失败（HTTP')} {st.error.status}{tt('）')}      </div>
    )
  }
  // T-434 / FR-142.1（断言反转①）：filter n.folder 退役——文件与目录都进
  // 树；「（空）」只在真空目录渲染（仅含文件的目录现在有叶子行可见，
  // 误导性空占位症状连带消除）。
  if (st.nodes.length === 0) {
    return <div className="tree-empty-level">{tt('（空）')}</div>
  }
  const capped = st.nodes.slice(0, TREE_LEVEL_CAP)
  return (
    <>
      {capped.map((n) => {
        if (!n.folder) {
          // 文件叶子：无展开语义（twisty 空槽对齐栅格——trash-node 同款），
          // 点击/Enter = 选中（URL 末段 → 右侧 item view）
          const leafSelected = currentRepo === repoKey && focusPath === n.path
          return (
            <div
              key={n.name}
              className={`tree-node leaf-node${leafSelected ? ' selected' : ''}`}
              data-testid={`tree-leaf-${n.path}`}
              data-tree-row=""
              role="treeitem"
              aria-level={depth + 1}
              aria-selected={leafSelected}
              tabIndex={0}
              style={{ paddingLeft: depth * 14 + 6 }}
              onClick={() => onNavigate(repoKey, n.path)}
              onContextMenu={(e: ReactMouseEvent) => {
                e.preventDefault()
                onMenu(e.clientX, e.clientY, { kind: 'node', repoKey, node: n })
              }}
              onKeyDown={(e) => onTreeKeys(e, { repo: repoKey, dir: n.path, isOpen: false, leaf: true, node: n }, onToggle, onNavigate, onMenu)}
            >
              <span aria-hidden="true" className="twisty" />
              <span aria-hidden="true" className="ico">◾</span>
              <span className="mono" lang="en">{n.name}</span>
            </div>
          )
        }
        const key = ck(repoKey, n.path)
        const isOpen = expanded.has(key)
        const onChain = currentRepo === repoKey && (currentDir === n.path || currentDir.startsWith(`${n.path}/`))
        const selected = currentRepo === repoKey && currentDir === n.path
        return (
          <div key={n.name}>
            <div
              className={`tree-node${onChain ? ' on-chain' : ''}${selected ? ' selected' : ''}`}
              data-testid={`tree-node-${n.path}`}
              data-tree-row=""
              role="treeitem"
              aria-expanded={isOpen}
              aria-selected={selected}
              aria-level={depth + 1}
              tabIndex={0}
              style={{ paddingLeft: depth * 14 + 6 }}
              onClick={() => onNavigate(repoKey, n.path)}
              onContextMenu={(e: ReactMouseEvent) => {
                e.preventDefault()
                onMenu(e.clientX, e.clientY, { kind: 'node', repoKey, node: n })
              }}
              onKeyDown={(e) => onTreeKeys(e, { repo: repoKey, dir: n.path, isOpen, node: n }, onToggle, onNavigate, onMenu)}
            >
              <span
                role="button"
                tabIndex={-1}
                aria-label={isOpen ? tt('收起 {v1}', { v1: n.name }) : tt('展开 {v1}', { v1: n.name })}
                className="twisty"
                onClick={(e) => {
                  e.stopPropagation()
                  onToggle(repoKey, n.path)
                }}
              >
                {isOpen ? '▾' : '▸'}
              </span>
              <span aria-hidden="true" className="ico">◻</span>
              <span className="mono" lang="en">{n.name}</span>
            </div>
            {isOpen && (
              <TreeLevel
                repoKey={repoKey}
                dirPath={n.path}
                depth={depth + 1}
                currentRepo={currentRepo}
                currentDir={currentDir}
                focusPath={focusPath}
                dirState={dirState}
                expanded={expanded}
                onToggle={onToggle}
                onNavigate={onNavigate}
                onMenu={onMenu}
              />
            )}
          </div>
        )
      })}
      {st.nodes.length > capped.length && (
        <div className="tree-empty-level">{tt('… 其余')} {st.nodes.length - capped.length} {tt('项未渲染（单层上限')} {TREE_LEVEL_CAP}{tt('）')}        </div>
      )}
    </>
  )
}

/** §3.4/§8 树键盘语义：↑↓ 移动、→ 展开、← 收起（叶子 = 上跳父层）、
 * Enter 激活、Shift+F10 菜单。T-434：叶子行（leaf）无展开语义——→ 不响应，
 * ← 上跳父目录；node 优先取行自带的 ChildNode（文件叶子带 sha256/size） */
type TreeKeyPos = { repo: string; dir: string; isOpen: boolean; leaf?: boolean; node?: ChildNode }

function onTreeKeys(
  e: ReactKeyboardEvent<HTMLElement>,
  pos: TreeKeyPos,
  onToggle: (repo: string, dir: string) => void,
  onNavigate: (repo: string, dir: string) => void,
  onMenu: (x: number, y: number, target: MenuTarget) => void,
): void {
  // Shift+F10 在 Chromium 报 e.key === 'F10' + shiftKey（部分平台报
  // 'ContextMenu'）；Menu 键报 'ContextMenu'。三者都收。
  if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
    e.preventDefault()
    const rect = e.currentTarget.getBoundingClientRect()
    if (pos.node) {
      onMenu(rect.left + 40, rect.top + 14, { kind: 'node', repoKey: pos.repo, node: pos.node })
    } else if (pos.dir === '') {
      onMenu(rect.left + 40, rect.top + 14, { kind: 'repo', repoKey: pos.repo })
    } else {
      // 目录节点：菜单目标 = 该目录（删除/复制路径/刷新）
      onMenu(rect.left + 40, rect.top + 14, {
        kind: 'node',
        repoKey: pos.repo,
        node: { name: pos.dir.split('/').pop() ?? '', path: pos.dir, folder: true, size: null, lastModified: '', sha256: '' },
      })
    }
    return
  }
  const rows = Array.from(document.querySelectorAll<HTMLElement>('[data-tree-row]'))
  const idx = rows.indexOf(e.currentTarget)
  if (e.key === 'ArrowDown' && rows[idx + 1]) {
    e.preventDefault()
    rows[idx + 1].focus()
  } else if (e.key === 'ArrowUp' && rows[idx - 1]) {
    e.preventDefault()
    rows[idx - 1].focus()
  } else if (e.key === 'ArrowRight') {
    e.preventDefault()
    if (!pos.leaf && !pos.isOpen) onToggle(pos.repo, pos.dir)
  } else if (e.key === 'ArrowLeft') {
    e.preventDefault()
    if (pos.leaf) onNavigate(pos.repo, pos.dir.split('/').slice(0, -1).join('/'))
    else if (pos.isOpen) onToggle(pos.repo, pos.dir)
    else onNavigate(pos.repo, pos.dir.split('/').slice(0, -1).join('/'))
  } else if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    onNavigate(pos.repo, pos.dir)
  }
}

// ---- 右键上下文菜单（console-m8 C3；键盘可达 §3.4） --------------------------
//
// T-300 批次二迁 MUI Menu：壳（portal/backdrop/Esc/首项聚焦/↑↓ 循环——
// MenuList 的 moveFocus 对无 tabindex 的非交互子元素自动跳过）交 MUI；
// 菜单项保持原生 button——artifacts-tree.spec 断言
// `[data-testid="tree-context-menu"] button` 计数，MenuItem（li）会断链。
// 视觉沿 .context-item（browser.css），壳的 surface/border/shadow 经 sx 复刻。

interface MenuItem {
  id: string
  label: string
  disabled?: boolean
  title?: string
  run: () => void
}

function TreeContextMenu({
  menu,
  readOnly,
  canSeeAdmin,
  repoRclass,
  targetVirtual,
  targetFavorite,
  onToggleFavorite,
  onClose,
  onCopyPath,
  onDownload,
  onDelete,
  onRefresh,
  onOpenAdmin,
}: {
  menu: { x: number; y: number; target: MenuTarget }
  readOnly: boolean
  canSeeAdmin: boolean
  repoRclass?: string
  /** 菜单目标是否 virtual 仓（T-416：删除预收敛——RE-08 服务端 405） */
  targetVirtual: boolean
  /** 菜单目标仓库是否已收藏（T-434 My Favorites） */
  targetFavorite: boolean
  onToggleFavorite: (repo: string) => void
  onClose: () => void
  onCopyPath: (value: string) => void
  onDownload: (repo: string, node: ChildNode) => void
  onDelete: (repo: string, node: ChildNode) => void
  onRefresh: (repo: string) => void
  onOpenAdmin: (repo: string) => void
}) {
  const t = menu.target
  const readonlyTitle = tt('只读管理员不可删（服务端 403 兜底）')
  // 与详情面板删除钮同一语义（行内删除钮已随 children 表收窄退役——
  // Q2 出口①：删除收敛进详情与右键，两者都过危险确认，E1 不倒退）：
  // virtual 不给注定 405 的入口
  const virtualDeleteTitle = tt('virtual 仓不经手删除（RE-08，服务端 405）——请到持有该制品的成员仓删除')
  const deleteBlocked = readOnly || targetVirtual
  const deleteTitle = readOnly ? readonlyTitle : targetVirtual ? virtualDeleteTitle : undefined
  const items: MenuItem[] =
    t.kind === 'repo'
      ? [
          { id: 'copy-repo-path', label: tt('复制仓库路径'), run: () => { onCopyPath(`${t.repoKey}/`); onClose() } },
          {
            id: 'favorite',
            label: targetFavorite ? tt('取消收藏（My Favorites）') : tt('加入收藏（My Favorites）'),
            title: tt('收藏是前端态（localStorage 持久）；树头 My Favorites 可只看收藏仓库'),
            run: () => { onToggleFavorite(t.repoKey); onClose() },
          },
          { id: 'refresh', label: tt('刷新'), run: () => onRefresh(t.repoKey) },
          ...(canSeeAdmin
            ? [{ id: 'open-admin', label: tt('在仓库管理中打开'), run: () => onOpenAdmin(t.repoKey) }]
            : []),
        ]
      : t.node.folder
        ? [
            { id: 'copy-path', label: tt('复制路径'), run: () => { onCopyPath(`${t.repoKey}/${t.node.path}/`); onClose() } },
            {
              id: 'delete',
              label: tt('删除'),
              disabled: deleteBlocked,
              title: deleteTitle,
              run: () => { onClose(); onDelete(t.repoKey, t.node) },
            },
            { id: 'refresh', label: tt('刷新'), run: () => onRefresh(t.repoKey) },
          ]
        : [
            { id: 'copy-path', label: tt('复制路径'), run: () => { onCopyPath(`${t.repoKey}/${t.node.path}`); onClose() } },
            { id: 'download', label: tt('下载'), run: () => { onClose(); onDownload(t.repoKey, t.node) } },
            {
              id: 'delete',
              label: repoRclass === 'remote' ? tt('删除缓存') : tt('删除'),
              disabled: deleteBlocked,
              title: deleteTitle,
              run: () => { onClose(); onDelete(t.repoKey, t.node) },
            },
          ]

  return (
    <Menu
      open
      onClose={onClose}
      anchorReference="anchorPosition"
      anchorPosition={{ left: menu.x, top: menu.y }}
      slotProps={{
        list: { 'aria-label': tt('操作菜单') } as ComponentPropsWithoutRef<'ul'>,
        paper: {
          sx: {
            border: '1px solid var(--bf-border)',
            borderRadius: 'var(--bf-r-md)',
            boxShadow: 'var(--bf-shadow-2)',
            padding: 'var(--bf-sp-1)',
            minWidth: 200,
          },
          'data-testid': 'tree-context-menu',
        } as ComponentPropsWithoutRef<'div'>,
      }}
    >
      {items.map((item, i) => (
        <button
          key={item.id}
          type="button"
          role="menuitem"
          className={`context-item${item.id === 'delete' && !item.disabled ? ' danger' : ''}`}
          data-testid={`tree-context-${item.id}`}
          disabled={item.disabled}
          title={item.title}
          // tabIndex={-1}：显式 tabindex 让 MenuList.moveFocus 视为可聚焦项
          // （原生 button 无 tabindex 属性会被跳过）；-1 不进 Tab 序
          tabIndex={-1}
          autoFocus={i === 0}
          onClick={item.run}
        >
          {item.label}
        </button>
      ))}
    </Menu>
  )
}

function TreeSkeleton() {
  return (
    <div className="tree-skel" aria-hidden="true">
      {Array.from({ length: 8 }, (_, i) => (
        <MuiSkeleton key={i} variant="text" width={`${80 - i * 5}%`} sx={{ my: 0.5 }} />
      ))}
    </div>
  )
}

function TableSkeleton() {
  return (
    <div data-testid="skeleton" aria-hidden="true" style={{ paddingTop: 8 }}>
      {Array.from({ length: 10 }, (_, i) => (
        <MuiSkeleton key={i} variant="text" width={`${90 - i * 5}%`} sx={{ my: 0.5 }} />
      ))}
    </div>
  )
}

// T434-MARKER-PROBE
