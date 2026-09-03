import { useState } from 'react'
import type { MouseEvent as ReactMouseEvent } from 'react'

import { Link } from 'react-router-dom'

import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import IconButton from '@mui/material/IconButton'
import Popover from '@mui/material/Popover'
import Stack from '@mui/material/Stack'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { CopyButton } from '../../components/CopyButton'
import { Skeleton } from '../../components/Skeleton'
import { formatBytes } from '../../lib/format'
import { getRepoDetail } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'
import { DOWNLOAD_COPY, EMPTY_VALUE, NO_SOURCE_HINTS, REPO_FIELD_LABELS, STATS_HINTS, STATS_LABELS } from './detailCopy'
import { contentFileURL, getItem, getItemPermissions, getNodeStats, getRepoUsageCounts } from './lib'
import type { ChildNode, ItemInfo } from './lib'
import PropertiesTab from './PropertiesTab'

// 详情面板（console-m8 §3.3 C4 / §6.3[2]——跨仓树右联）：
//
// - 三形态：仓库（getRepoDetail + usage counts）/ 目录（FolderInfo）/
//   文件（FileInfo 全字段）；Tab = 常规 + 有效权限（admin 渲染——admin
//   位仅做预收敛省掉明知 403 的请求，§3.6.3）+ 属性（节点形态，T-291
//   MUI 首票——?properties 读读写族）。
// - **页签序（T-445 / FR-144.1，B-2.1/7）**：General → Effective
//   Permissions → Properties——权限在属性前（7.161.20 活体 A2-7 + 7.84
//   reverse §3.2，逐级一致）；仓库根无属性页签（属性挂在节点上，
//   §15.3.2——BinFlow 仓根无节点行契约，缺位登记），非 admin 无权限
//   页签（SE-08 门）——两档缺位下的剩余序仍一致。
// - T-434 / FR-142.3：活跃页签进 URL 路径段（tab prop 受控——对位
//   Artifactory /ui/repos/tree/<TAB>/…）；目标切换不重置页签（页签是
//   视图态随 URL 走，T-246 修的是父渲染重置——受控 prop 后该类重置
//   物理消失）。URL 页签对目标无效时回退常规渲染不重写 URL（admin 位
//   异步到位，重写会抖）。
// - 目录形态给直系概要（T-434 / FR-142.1：children 表收窄后目录选中给
//   Artifact Count / Size——数据源 = 父组件已装载的当前层 listing）。
// - **字段族（T-445 / FR-144.2/.3，B-2.3/4/10）**：字段序对齐 reverse
//   §3.2 + 7.161 活体——Name → Repository Path → File URL（含复制钮，
//   三形态齐备）→ Deployed By → Size → Created → Last Modified →
//   Downloads / Last Downloaded By / Last Downloaded / Remote Downloads
//   （消费 T-438 ?stats 面——file 形态）；仓视图补 Repository Layout /
//   Description / Created / Artifact Count（Layout 与 Created 无源恒
//   '—' + title 登记——不伪造）。**T-447 / Q9 终裁消化**：mimeType/
//   Checksums 徽标块/下载校验块自 General 页收进下载伴随菜单（见
//   FileDownloadActions）；General 页保留的类型/tags 为已裁维持项。
// - **缺位登记（不伪造）**：Module ID 不建（dep Build-info，§9A-S8——
//   M17 解禁）；Package Information / Dependency Declaration /
//   Virtual Repository Associations / Included Repositories 块不建
//   （BinFlow 无包信息域/无 virtual↔file 关联面——块级缺位，登记待域
//   落地）；仓视图的 Show 懒展开不建（usage counts 面本就廉价，直接
//   渲染——形态简化留痕）。
// - docker 特化：manifest digest 行的 tag 徽标（T-134 G32a 随迁）。
// - Followers / Xray Tab 不建（Non-goal）。

export interface DownloadState {
  path: string
  phase: 'loading' | 'done'
  sha: string
  /** undefined = 无对账源（根目录层无 list 合并值） */
  match: boolean | undefined
}

/** 详情页签 ID（URL 段 slug 映射见 ArtifactsBrowser TAB_SLUG） */
export type DetailTab = 'general' | 'props' | 'perms'

/** 详情对象：仓库（dir='' 且有元数据）｜目录｜文件 */
export type DetailTarget =
  | { kind: 'repo'; repoKey: string }
  | { kind: 'node'; repoKey: string; node: ChildNode }

export default function NodeDetail({
  target,
  download,
  onDownload,
  onClose,
  canDelete,
  canWriteProps,
  onDelete,
  tab,
  onTabChange,
  childrenNodes,
}: {
  target: DetailTarget
  download: DownloadState | null
  onDownload: (node: ChildNode, expectedSha: string) => void
  onClose: () => void
  /** 删除入口可见性（readonly_admin 预收敛禁用；普通用户走服务端 403） */
  canDelete: boolean
  /** 属性写姿态（与 canDelete 同源：readonly_admin 预收敛禁用，普通用户
   *  保留入口由服务端 403 兜底——属性写权限是路径 `w`，非管理面能力） */
  canWriteProps: boolean
  onDelete: (node: ChildNode) => void
  /** 活跃页签（T-434 / FR-142.3：URL 路径段受控，省略 = general） */
  tab: DetailTab
  onTabChange: (t: DetailTab) => void
  /** 目录形态的直系子项概要数据（父组件已装载的当前层 listing；缺省回退
   *  FolderInfo.children 计数——无 listing 的场景） */
  childrenNodes?: ChildNode[]
}) {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  // 页签有效性：URL 携带的页签对当前目标不可用（仓库根无属性页签 / 非
  // admin 无权限页签）时回退常规渲染，不重写 URL（admin 位异步到位，
  // 重写会抖/环）
  const activeTab: DetailTab =
    tab === 'props' && target.kind !== 'node'
      ? 'general'
      : tab === 'perms' && !admin
        ? 'general'
        : tab

  // 节点元数据（文件/目录）在顶层取——下载伴随菜单的对账源
  // （checksums.sha256）与常规 Tab 共用一次请求。
  const nodeItem = useAsync(
    () => (target.kind === 'node' ? getItem(target.repoKey, target.node.path) : Promise.resolve(null)),
    [target.kind, target.kind === 'node' ? target.repoKey : '', target.kind === 'node' ? target.node.path : ''],
  )
  const item: ItemInfo | null = target.kind === 'node' && nodeItem.status === 'ok' ? nodeItem.data : null
  const isFile = target.kind === 'node' && !target.node.folder

  return (
    <section className="card section node-detail" data-testid="node-detail" aria-label="节点详情">
      <header className="node-detail-head">
        <h3>
          {target.kind === 'repo' ? (
            <>
              <span aria-hidden="true">▣</span>{' '}
              <span className="mono" lang="en">{target.repoKey}</span>
            </>
          ) : (
            <>
              <span aria-hidden="true">{target.node.folder ? '◻' : '◾'}</span>{' '}
              <span className="mono" lang="en">{target.node.path}</span>
            </>
          )}
        </h3>
        <div className="node-detail-actions">
          {isFile && target.kind === 'node' && (
            // 下载形态（T-447 / FR-144.5，B-2.12 翻正 + Q9 处置）：单 24px
            // 图标钮（直接下载——浏览器原生落盘）+ 伴随菜单（校验能力与
            // checksum/mimeType 的家）。原两带文字按钮（下载并校验 / 直接
            // 下载）收敛；General 页的 mimeType/校验徽标块/下载校验块随迁
            // 伴随（Q9 终裁「校验块收进伴随形态」）。
            <FileDownloadActions
              repoKey={target.repoKey}
              node={target.node}
              item={item}
              download={download}
              onVerify={() => onDownload(target.node, item?.checksums?.sha256 ?? target.node.sha256 ?? '')}
            />
          )}
          {target.kind === 'node' && canDelete && (
            <Button
              variant="outlined"
              color="error"
              size="small"
             
              data-testid="delete-node-button"
              onClick={() => onDelete(target.node)}
            >
              删除
            </Button>
          )}
          <Button variant="outlined" size="small" onClick={onClose}>
            关闭
          </Button>
        </div>
      </header>

      {/* T-344 批 D：node-tabs 换 MUI Tabs（spec §3.5 browser.css 行的 D 波
          评估落地）。锚 node-tab-* 落 Tab 根 <button>（元素型不变）、
          aria-selected 内建；方向键「选择随焦点」= selectionFollowsFocus
          （T-344D 批 C 的 repos/repo/authcfg 同款——keyboard.spec §4 的
          node-tab 腿由 MUI 行为覆盖，onTablistKeys 末位消费者随之退役）。
          T-434：value 受控于 URL 页签段（activeTab 经目标有效性校验）。
          T-445 / FR-144.1（B-2.1）：渲染序 = 常规 → 有效权限 → 属性——
          权限在属性前（7.161.20 活体 A2-7 + 7.84 reverse §3.2 逐级一致；
          URL slug 与 tab 值零变化，仅渲染序互换）。 */}
      <Tabs
        value={activeTab}
        onChange={(_e, id: DetailTab) => onTabChange(id)}
        selectionFollowsFocus
        aria-label="详情视图"
        sx={{ borderBottom: 1, borderColor: 'divider', mb: 'var(--bf-sp-3)' }}
      >
        <Tab value="general" label="常规" data-testid="node-tab-general" />
        {admin && <Tab value="perms" label="有效权限" data-testid="node-tab-perms" />}
        {target.kind === 'node' && <Tab value="props" label="属性" data-testid="node-tab-props" />}
      </Tabs>

      {activeTab === 'general' ? (
        target.kind === 'repo' ? (
          <RepoGeneral repoKey={target.repoKey} />
        ) : (
          <NodeGeneral node={target.node} repoKey={target.repoKey} item={item} itemStatus={nodeItem.status} itemError={nodeItem.error} download={download} childrenNodes={childrenNodes} />
        )
      ) : activeTab === 'props' && target.kind === 'node' ? (
        // 属性页签（T-291）：目录/文件节点均可挂属性（§15.3.2）；仓库根形态
        // 不渲染该 Tab。folder 行的存储拼写带尾斜杠——?properties 面按
        // storageNode 寻址，folder 需带尾斜杠（与目录删除同款 isFolderNode
        // 分支口径）。
        <PropertiesTab
          repoKey={target.repoKey}
          path={target.node.folder ? `${target.node.path}/` : target.node.path}
          canWrite={canWriteProps}
        />
      ) : (
        <PermsTab target={target} />
      )}
    </section>
  )
}

// ---- 下载形态：单 24px 图标钮 + 伴随菜单（T-447 / FR-144.5，B-2.12/Q9）------

// Artifactory 7.161 单 24px 图标钮无校验伴随（B-2.12 目标形）；BinFlow 的
// sha256 对账/mimeType/checksum 徽标是自有增强——Q9 终裁「校验块收进伴随
// 形态」：能力与信息全部收进伴随菜单，General 页不再平铺。直接下载走
// 内容面 GET（T-438 埋点单源——服务端计数，无 FE 侧第二通道）；校验下载
// 完成后由 FileStatsRows 的 refreshKey 联动刷新计数（直接下载是浏览器
// 原生锚点，无 JS 完成回调——计数仍由服务端单源落库，下次统计读自然带回）。
function FileDownloadActions({
  repoKey,
  node,
  item,
  download,
  onVerify,
}: {
  repoKey: string
  node: ChildNode
  item: ItemInfo | null
  download: DownloadState | null
  onVerify: () => void
}) {
  const [menuAnchor, setMenuAnchor] = useState<HTMLButtonElement | null>(null)
  const busy = download?.path === node.path && download.phase === 'loading'
  const verdict =
    download?.path === node.path && download.phase === 'done' && download.match !== undefined ? download.match : null
  const href = contentFileURL(repoKey, node.path)

  return (
    <>
      <Tooltip title={DOWNLOAD_COPY.iconTitle}>
        <IconButton
          size="small"
          sx={{ width: 24, height: 24 }}
          component="a"
          href={href}
          download={node.name}
          data-testid="node-download"
          aria-label={`${DOWNLOAD_COPY.iconLabel} ${node.name}`}
        >
          <span aria-hidden="true" style={{ fontSize: 14 }}>⬇</span>
        </IconButton>
      </Tooltip>
      <Tooltip title={DOWNLOAD_COPY.menuLabel}>
        <IconButton
          size="small"
          sx={{ width: 24, height: 24 }}
          data-testid="node-download-menu"
          aria-label={DOWNLOAD_COPY.menuLabel}
          aria-haspopup="dialog"
          aria-expanded={menuAnchor !== null}
          onClick={(e: ReactMouseEvent<HTMLButtonElement>) => setMenuAnchor(e.currentTarget)}
        >
          <span aria-hidden="true" style={{ fontSize: 12 }}>▾</span>
        </IconButton>
      </Tooltip>
      <Popover
        open={menuAnchor !== null}
        anchorEl={menuAnchor}
        onClose={() => setMenuAnchor(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        slotProps={{
          paper: {
            'data-testid': 'node-download-panel',
            role: 'dialog',
            'aria-label': DOWNLOAD_COPY.menuLabel,
            sx: {
              border: '1px solid var(--bf-border)',
              boxShadow: 'var(--bf-shadow-2)',
              borderRadius: 'var(--bf-r-md)',
              p: 'var(--bf-sp-3)',
              maxWidth: 380,
            },
          } as React.ComponentPropsWithoutRef<'div'>,
        }}
      >
        <Stack spacing={1}>
          <Button
            size="small"
            variant="outlined"
            data-testid="node-download-menu-verify"
            disabled={busy}
            onClick={onVerify}
          >
            {DOWNLOAD_COPY.verifyLabel}
          </Button>
          {/* 校验呈现（node-download-verify——原 General 页右栏随迁；菜单
              开着才可见，最终结果另有 toast 承载，关闭菜单不丢反馈） */}
          {busy && <div className="node-verify">{DOWNLOAD_COPY.verifyBusy}</div>}
          {verdict !== null && (
            <div className={`node-verify ${verdict ? 'ok' : 'bad'}`} data-testid="node-download-verify">
              {verdict ? (
                <>
                  {DOWNLOAD_COPY.verifyOk}
                  <div className="mono sub" lang="en">{download?.sha}</div>
                </>
              ) : (
                <>
                  {DOWNLOAD_COPY.verifyBad}
                  <div className="mono sub" lang="en">local {download?.sha}</div>
                </>
              )}
            </div>
          )}
          <Divider />
          {/* checksum/mimeType 区（Q9：自 General 页收进伴随） */}
          <div data-testid="node-download-checksums">
            <Typography variant="caption" color="text.secondary">
              {DOWNLOAD_COPY.checksumsHeader}
            </Typography>
            {!item ? (
              <Typography variant="body2" color="text.secondary">元数据加载中…</Typography>
            ) : (
              <>
                <div className="kv">
                  <span className="k">{DOWNLOAD_COPY.mimeTypeLabel}</span>
                  <span className="mono" lang="en">{item.mimeType ?? EMPTY_VALUE}</span>
                </div>
                {(['sha256', 'sha1', 'md5'] as const).map((algo) => {
                  const v = item.checksums?.[algo]
                  if (!v) return null
                  const orig = item.originalChecksums?.[algo]
                  return (
                    <div className="kv" key={algo}>
                      <span className="k">{algo}</span>
                      <span className="mono" lang="en">
                        <span>
                          {v.length > 24 ? `${v.slice(0, 20)}…${v.slice(-8)}` : v}
                          <CopyButton value={v} label={algo} />
                        </span>
                        {orig && (
                          <span
                            className={`checksum-badge ${orig === v ? 'ok-badge' : 'warning'}`}
                            title="客户端上传时提供的 checksum 与服务端实际值比对"
                          >
                            上传时提供：{orig === v ? '一致 ✓' : '不一致'}
                          </span>
                        )}
                      </span>
                    </div>
                  )
                })}
              </>
            )}
          </div>
          <Typography variant="caption" color="text.secondary">
            {DOWNLOAD_COPY.verifyHint}
          </Typography>
        </Stack>
      </Popover>
    </>
  )
}

// ---- 常规 Tab：仓库形态 ----------------------------------------------------

// 仓视图字段族（T-445 / FR-144.3，B-2.4——7.161.20 活体 A2-7 的字段序）：
// Name → Package Type → Repository Path → File URL → Repository Layout →
// Description → Created → Artifact Count → Size。Layout/Created 无 wire 源
// （K70 预留位 / CreatedAt 未投影）——恒 '—' + title 登记，不伪造；
// Virtual Repository Associations / Included Repositories 块缺位登记
// （BinFlow 无仓关联面——不建不伪造，归 M16-SPLIT §1.2 登记项）。
function RepoGeneral({ repoKey }: { repoKey: string }) {
  const meta = useAsync(() => getRepoDetail(repoKey), [repoKey])
  const usage = useAsync(() => getRepoUsageCounts(repoKey), [repoKey])

  if (meta.status === 'loading') return <Skeleton lines={4} />
  if (meta.status === 'forbidden') {
    return (
      <p className="text-2">
        仓库元数据为管理员视图（HTTP 403）——树按 generic 语义呈现；上传/删除权限由内容面按路径 ACL 判定。
      </p>
    )
  }
  if (meta.status === 'error' || !meta.data) {
    return <p className="text-2">详情加载失败（HTTP {meta.error?.status ?? 0}）。</p>
  }
  const m = meta.data
  return (
    <div className="node-detail-grid">
      <div>
        <div className="kv">
          <span className="k">名称</span>
          <span className="mono" lang="en">{m.key}</span>
        </div>
        <div className="kv">
          <span className="k">包类型</span>
          <span>
            <Chip size="small" className="badge neutral" label={m.packageType} />{' '}
            <Chip size="small" className="badge neutral" label={m.rclass} />
          </span>
        </div>
        <div className="kv">
          <span className="k">Repository Path</span>
          <span className="mono" style={{ wordBreak: 'break-all' }} lang="en">
            {m.key}/ <CopyButton value={`${m.key}/`} label="仓库路径" />
          </span>
        </div>
        {m.url && (
          <div className="kv">
            <span className="k">File URL</span>
            <span className="mono" style={{ wordBreak: 'break-all' }} lang="en" data-testid="node-file-url">
              {m.url} <CopyButton value={m.url} label="File URL" />
            </span>
          </div>
        )}
        <div className="kv">
          <span className="k">{REPO_FIELD_LABELS.repoLayout}</span>
          <span className="mono" data-testid="node-repo-layout" title={NO_SOURCE_HINTS.repoLayout}>
            {EMPTY_VALUE}
          </span>
        </div>
        <div className="kv">
          <span className="k">{REPO_FIELD_LABELS.description}</span>
          <span data-testid="node-repo-description">{m.description || EMPTY_VALUE}</span>
        </div>
        <div className="kv">
          <span className="k">{REPO_FIELD_LABELS.created}</span>
          <span className="mono" data-testid="node-repo-created" title={NO_SOURCE_HINTS.repoCreated}>
            {EMPTY_VALUE}
          </span>
        </div>
        {/* usage counts 面（admin ∪ 有读权限档）：一次请求带回 count+size；
            不可见档两行缺席（Artifactory 的 Size: Show 懒展开不建——usage
            面廉价，直接渲染，形态简化留痕） */}
        {usage.status === 'ok' && usage.data && (
          <>
            <div className="kv">
              <span className="k">{REPO_FIELD_LABELS.artifactCount}</span>
              <span className="mono" data-testid="node-repo-artifact-count">
                {usage.data.nodeCount}
              </span>
            </div>
            <div className="kv">
              <span className="k">大小</span>
              <span className="mono">{formatBytes(usage.data.usedBytes)}</span>
              {usage.data.quotaBytes > 0 && (
                <span className="text-2"> / 配额 {formatBytes(usage.data.quotaBytes)}</span>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

// ---- 常规 Tab：目录 / 文件形态 ----------------------------------------------

// 字段序（T-445 / FR-144.2/.3）：Name → Repository Path → File URL（B-2.4
// 目录视图同样补齐）→〔Module ID 不建——Build-info stay-out〕→ 部署者 →
// 大小 → Created → 修改时间 → 下载统计族（file 形态）→ BinFlow 自有
// 增强（类型/tags——T-447/Q9 后仅存两项；mimeType 与 Checksums 块已收进
// 下载伴随菜单）。
function NodeGeneral({
  node,
  repoKey,
  item,
  itemStatus,
  itemError,
  download,
  childrenNodes,
}: {
  node: ChildNode
  repoKey: string
  item: ItemInfo | null
  itemStatus: 'loading' | 'ok' | 'error' | 'forbidden'
  itemError: { status: number } | null
  download: DownloadState | null
  childrenNodes?: ChildNode[]
}) {
  const nodeRef = node.folder ? `${node.path}/` : node.path
  const fileURL = contentFileURL(repoKey, nodeRef)
  // 下载计数联动（T-438 埋点单源）：FE 可观测的下载完成（校验下载落盘）
  // 作为 ?stats 重读信号——UI 计数与 nodes 单源对齐；直接下载是浏览器原生
  // 锚点无完成回调，不触发（服务端计数照落，下次读自然带回）
  const statsRefreshKey =
    download?.path === node.path && download.phase === 'done' ? `done:${download.sha}` : 'base'

  if (itemStatus === 'loading') return <Skeleton lines={4} />
  if (itemStatus !== 'ok' || !item) {
    return <p className="text-2">详情加载失败（HTTP {itemError?.status ?? 0}）——列表数据仍然有效。</p>
  }
  return (
    <div className="node-detail-grid">
      <div>
        <div className="kv">
          <span className="k">名称</span>
          <span className="mono" lang="en">{node.name}</span>
        </div>
        <div className="kv">
          <span className="k">Repository Path</span>
          <span className="mono" style={{ wordBreak: 'break-all' }} lang="en">
            {repoKey}/{nodeRef} <CopyButton value={`${repoKey}/${nodeRef}`} label="制品路径" />
          </span>
        </div>
        <div className="kv">
          <span className="k">File URL</span>
          <span className="mono" style={{ wordBreak: 'break-all' }} lang="en" data-testid="node-file-url">
            {fileURL} <CopyButton value={fileURL} label="File URL" />
          </span>
        </div>
        <div className="kv">
          <span className="k">部署者</span>
          <span lang="en">{item.createdBy || EMPTY_VALUE}</span>
        </div>
        {!node.folder && (
          <div className="kv">
            <span className="k">大小</span>
            <span className="mono">{item.size}</span>
          </div>
        )}
        <div className="kv">
          <span className="k">Created</span>
          <span className="mono">{item.created || EMPTY_VALUE}</span>
        </div>
        {item.lastModified && (
          <div className="kv">
            <span className="k">修改时间</span>
            <span className="mono">{item.lastModified}</span>
          </div>
        )}
        {/* 下载统计族（T-445 / FR-144.2——file 形态；folder 结构性零值
            不渲染〔Artifactory folder item view 同为无下载族〕；组件在
            item 到位后才挂载 = 统计读晚于本页的 item-info GET，页内浏览
            计数先落库再呈现，读数与 UI 一致——T-438 §3-2 探针面计数继承
            的确定性呈现；statsRefreshKey = 下载完成后的计数联动重读） */}
        {!node.folder && <FileStatsRows repoKey={repoKey} path={node.path} refreshKey={statsRefreshKey} />}
        {node.folder && (
          // T-434 / FR-142.1：children 表收窄后目录形态给直系概要
          //（Artifact Count / Size——Artifactory 目录 item view 同位字段；
          // Size 口径 = 直系文件已知大小合计，folder 无 size 契约不虚构）
          <>
            <div className="kv">
              <span className="k">子项（Artifact Count）</span>
              <span className="mono">
                {childrenNodes
                  ? `目录 ${childrenNodes.filter((n) => n.folder).length} · 文件 ${childrenNodes.filter((n) => !n.folder).length}`
                  : `${item.children?.length ?? 0} 项`}
              </span>
            </div>
            {childrenNodes && childrenNodes.some((n) => !n.folder && n.size !== null) && (
              <div className="kv">
                <span className="k">Size（直系文件合计）</span>
                <span className="mono">
                  {formatBytes(childrenNodes.reduce((acc, n) => acc + (!n.folder && n.size !== null ? n.size : 0), 0))}
                </span>
              </div>
            )}
          </>
        )}
        {/* ---- BinFlow 自有增强（parity 族之后；mimeType/Checksums 块已随
              Q9 终裁收进下载伴随菜单〔T-447〕——General 页不再平铺）---- */}
        <div className="kv">
          <span className="k">类型</span>
          <span>{node.folder ? '目录' : '文件'}</span>
        </div>
        {/* docker 特化：manifest digest 行的 tag 徽标（T-134 G32a） */}
        {!node.folder && node.tags && node.tags.length > 0 && (
          <div className="kv">
            <span className="k">tags</span>
            <span>
              {node.tags.map((tag) => (
                <Chip
                  key={tag}
                  size="small"
                  className="badge neutral"
                  label={tag}
                  data-testid={`tag-badge-${tag}`}
                  title={`tag: ${tag}`}
                  sx={{ mr: 0.5 }}
                />
              ))}
            </span>
          </div>
        )}
      </div>
    </div>
  )
}

// ---- 下载统计族（file 形态——T-445 / FR-144.2，消费 T-438 ?stats 面）--------

// 四态：loading '…' / error '—'+title / ok 值；lastDownloadedBy 缺席
// （从未下载 或 调用者非 CapSystemRead 档——服务端 omitempty）一律 '—'，
// 不区分原因（区分即泄漏档位信息）。计数与远端计数全档可见（?stats 计
// 数面无门）。
function FileStatsRows({ repoKey, path, refreshKey }: { repoKey: string; path: string; refreshKey: string }) {
  // refreshKey：FE 可观测的下载完成信号（联动重读——见 NodeGeneral 注）
  const stats = useAsync(() => getNodeStats(repoKey, path), [repoKey, path, refreshKey])
  const d = stats.status === 'ok' ? stats.data : null
  const errTitle =
    stats.status === 'error' || stats.status === 'forbidden'
      ? STATS_HINTS.unavailable(stats.error?.status ?? 0)
      : undefined
  const v = (value: string | number | undefined): string => {
    if (stats.status === 'loading') return STATS_HINTS.loading
    if (!d) return EMPTY_VALUE
    return value === undefined || value === '' ? EMPTY_VALUE : String(value)
  }
  const busyText = stats.status === 'loading' ? STATS_HINTS.loading : EMPTY_VALUE
  return (
    <>
      <div className="kv">
        <span className="k">{STATS_LABELS.downloads}</span>
        <span className="mono" data-testid="node-downloads" title={errTitle}>
          {d ? String(d.downloadCount) : busyText}
        </span>
      </div>
      <div className="kv">
        <span className="k">{STATS_LABELS.lastDownloadedBy}</span>
        <span className="mono" lang="en" data-testid="node-last-downloaded-by" title={errTitle}>
          {v(d?.lastDownloadedBy)}
        </span>
      </div>
      <div className="kv">
        <span className="k">{STATS_LABELS.lastDownloaded}</span>
        <span className="mono" data-testid="node-last-downloaded" title={errTitle}>
          {v(d?.lastDownloaded)}
        </span>
      </div>
      <div className="kv">
        <span className="k">{STATS_LABELS.remoteDownloads}</span>
        <span className="mono" data-testid="node-remote-downloads" title={errTitle}>
          {d ? String(d.remoteDownloadCount) : busyText}
        </span>
      </div>
    </>
  )
}

// ---- 有效权限 Tab（admin；?permissions SE-08）--------------------------------

function PermsTab({ target }: { target: DetailTarget }) {
  const path = target.kind === 'repo' ? '' : target.node.folder ? `${target.node.path}/` : target.node.path
  const perms = useAsync(() => getItemPermissions(target.repoKey, path), [target.repoKey, path])

  return (
    <div className="node-perms" data-testid="node-perms">
      {perms.status === 'loading' && <Skeleton lines={2} />}
      {perms.status === 'error' && (
        <p className="text-2" title={perms.error?.message}>
          权限视图不可用（HTTP {perms.error?.status}）
        </p>
      )}
      {perms.status === 'ok' && perms.data && <PrincipalList view={perms.data} />}
      <p className="field-hint">
        该视图与服务端授权判定同源；授权编辑见{' '}
        <Link to="/admin/security/permissions" target="_blank">
          权限 target
        </Link>
        。
      </p>
    </div>
  )
}

function PrincipalList({
  view,
}: {
  view: { principals: { users: Record<string, string[]>; groups: Record<string, string[]> } }
}) {
  const users = Object.entries(view.principals.users ?? {})
  const groups = Object.entries(view.principals.groups ?? {})
  if (users.length === 0 && groups.length === 0) {
    return <p className="text-2">没有 permission target 覆盖此路径（admin 隐式全权）。</p>
  }
  return (
    <div className="perm-chips">
      {users.map(([name, bits]) => (
        <span className="chip-item" key={`u-${name}`}>
          <span lang="en">{name}</span>
          <span className="mono">{bits.join('')}</span>
        </span>
      ))}
      {groups.map(([name, bits]) => (
        <span className="chip-item" key={`g-${name}`}>
          <span aria-hidden="true">👥</span>
          <span lang="en">{name}</span>
          <span className="mono">{bits.join('')}</span>
        </span>
      ))}
    </div>
  )
}
