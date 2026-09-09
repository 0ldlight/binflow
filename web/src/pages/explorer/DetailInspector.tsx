// Artifact Detail（inspector 形态——总令 §十二；P2 六域之一）。
// 语义平移自旧 NodeDetail（audit §2.2）：
// - 三形态：仓库 / 目录 / 文件；三页签 General → 有效权限（admin）→
//   属性（节点形态——页签序 B-2.1）；页签进 URL 段（受控 prop）。
// - 字段族（B-2.3/4/10）：Name → Repository Path → File URL → Module ID
//   （file）→ 部署者 → Size → Created → Last Modified → 下载统计族
//   （file，?stats）→ 类型/tags（docker）。
// - 下载形态（B-2.12 + Q9）：24px 图标钮（直接下载）+ 伴随菜单（校验
//   能力 + checksum/mimeType 的家）。
// - 属性页签 = 旧 PropertiesTab（MUI 树）经 LegacyMount 嵌挂——终验强删
//   项（P4 重写）。
// - 锚族原样：node-detail / node-tab-* / node-file-url / node-repo-*
//   / node-downloads / node-last-downloaded(-by) / node-remote-downloads
//   / node-perms / node-download(-menu|-panel|-verify|-checksums)
//   / delete-node-button / node-module-id / node-remote-error。
import { lazy, useState } from 'react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { useAuth } from '@/app/AuthContext'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState } from '@/components/layout/states'
import { StateSkeleton } from '@/components/layout/states'
import { LegacyMount } from '@/components/layout/legacy-host'
import { getNodeProperties } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { getRepoDetail } from '@/lib/repos'
import { useAsync } from '@/lib/useAsync'
import { buildAssocOf, getBuildRun, moduleIdsOf } from '@/pages/builds/api'
import {
  contentFileURL,
  getItemForDetail,
  getItemPermissions,
  getNodeStats,
  getRepoUsageCounts,
} from '@/pages/artifacts/lib'
import type { ChildNode, ItemInfo } from '@/pages/artifacts/lib'
import {
  DOWNLOAD_COPY,
  EMPTY_VALUE,
  NO_SOURCE_HINTS,
  REPO_FIELD_LABELS,
  REMOTE_COPY,
  STATS_HINTS,
  STATS_LABELS,
} from '@/pages/artifacts/detailCopy'
import { tr } from '@/i18n'

import type { DetailTab, DownloadState } from './model'

const tt = tr('artifacts')

// 属性页签 = 旧 MUI PropertiesTab（LegacyMount 嵌挂——终验强删项）
const PropertiesTabLazy = lazy(() => import('@/pages/artifacts/PropertiesTab'))

export type DetailTarget =
  | { kind: 'repo'; repoKey: string }
  | { kind: 'node'; repoKey: string; node: ChildNode }

export function DetailInspector({
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
  canDelete: boolean
  canWriteProps: boolean
  onDelete: (node: ChildNode) => void
  tab: DetailTab
  onTabChange: (t: DetailTab) => void
  childrenNodes?: ChildNode[]
}) {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const activeTab: DetailTab =
    tab === 'props' && target.kind !== 'node' ? 'general' : tab === 'perms' && !admin ? 'general' : tab

  // 节点元数据（T-494：file 行走 getItemForDetail 零计数改道）
  const nodeItem = useAsync(
    () =>
      target.kind === 'node'
        ? getItemForDetail(target.repoKey, target.node.path, {
            sha256: target.node.sha256,
            remote: target.node.remote,
            folder: target.node.folder,
          })
        : Promise.resolve(null),
    [
      target.kind,
      target.kind === 'node' ? target.repoKey : '',
      target.kind === 'node' ? target.node.path : '',
    ],
  )
  const item: ItemInfo | null = target.kind === 'node' && nodeItem.status === 'ok' ? nodeItem.data : null
  const isFile = target.kind === 'node' && !target.node.folder

  const tabs: { id: DetailTab; label: string; visible: boolean }[] = [
    { id: 'general', label: tt('常规'), visible: true },
    { id: 'perms', label: tt('有效权限'), visible: admin },
    { id: 'props', label: tt('属性'), visible: target.kind === 'node' },
  ]

  return (
    <section className="card section node-detail rounded-md border border-border bg-surface-1 p-4" data-testid="node-detail" aria-label={tt('节点详情')}>
      <header className="node-detail-head mb-3 flex flex-wrap items-center gap-2">
        <h3 className="flex min-w-0 items-center gap-1.5 text-[15px] font-semibold">
          <span aria-hidden="true">
            {target.kind === 'repo' ? '▣' : target.node.folder ? '◻' : '◾'}
          </span>{' '}
          <span className="truncate font-mono" lang="en">
            {target.kind === 'repo' ? target.repoKey : target.node.path}
          </span>
        </h3>
        <div className="node-detail-actions ml-auto flex items-center gap-1.5">
          {isFile && target.kind === 'node' && (
            <FileDownloadActions
              repoKey={target.repoKey}
              node={target.node}
              item={item}
              download={download}
              onVerify={() => onDownload(target.node, item?.checksums?.sha256 ?? target.node.sha256 ?? '')}
            />
          )}
          {target.kind === 'node' && canDelete && (
            <Button variant="outline" size="sm" className="text-destructive" data-testid="delete-node-button" onClick={() => onDelete(target.node)}>
              {tt('删除')}
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={onClose}>
            {tt('关闭')}
          </Button>
        </div>
      </header>

      {/* 页签（tablist 方向键选择随焦点——Radix Tabs 内建） */}
      <div role="tablist" aria-label={tt('详情视图')} className="mb-3 flex gap-1 border-b border-border">
        {tabs
          .filter((t) => t.visible)
          .map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={activeTab === t.id}
              data-testid={`node-tab-${t.id}`}
              className={`-mb-px rounded-t-sm border-b-2 bg-transparent px-3 py-1.5 text-dense ${
                activeTab === t.id ? 'border-primary font-medium text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'
              }`}
              onClick={() => onTabChange(t.id)}
              onKeyDown={(e) => {
                const tabsBtns = Array.from(e.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]') ?? [])
                const idx = tabsBtns.indexOf(e.currentTarget)
                if (e.key === 'ArrowRight' && tabsBtns[idx + 1]) {
                  e.preventDefault()
                  tabsBtns[idx + 1].focus()
                  tabsBtns[idx + 1].click()
                } else if (e.key === 'ArrowLeft' && tabsBtns[idx - 1]) {
                  e.preventDefault()
                  tabsBtns[idx - 1].focus()
                  tabsBtns[idx - 1].click()
                }
              }}
            >
              {t.label}
            </button>
          ))}
      </div>

      {activeTab === 'general' ? (
        target.kind === 'repo' ? (
          <RepoGeneral repoKey={target.repoKey} />
        ) : (
          <NodeGeneral
            node={target.node}
            repoKey={target.repoKey}
            item={item}
            itemStatus={nodeItem.status}
            itemError={nodeItem.error}
            download={download}
            childrenNodes={childrenNodes}
          />
        )
      ) : activeTab === 'props' && target.kind === 'node' ? (
        <LegacyMount>
          <PropertiesTabLazy
            repoKey={target.repoKey}
            path={target.node.folder ? `${target.node.path}/` : target.node.path}
            canWrite={canWriteProps}
          />
        </LegacyMount>
      ) : (
        <PermsTab target={target} />
      )}
    </section>
  )
}

// ---- 下载形态：24px 图标钮 + 伴随菜单（B-2.12 / Q9 终裁）----

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
  const [open, setOpen] = useState(false)
  const busy = download?.path === node.path && download.phase === 'loading'
  const verdict =
    download?.path === node.path && download.phase === 'done' && download.match !== undefined ? download.match : null
  const href = contentFileURL(repoKey, node.path)

  return (
    <>
      <a
        href={href}
        download={node.name}
        data-testid="node-download"
        aria-label={`${DOWNLOAD_COPY.iconLabel} ${node.name}`}
        title={DOWNLOAD_COPY.iconTitle}
        className="grid size-6 place-items-center rounded-sm hover:bg-accent"
      >
        <span aria-hidden="true" className="text-sm">⬇</span>
      </a>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            data-testid="node-download-menu"
            aria-label={DOWNLOAD_COPY.menuLabel}
            aria-haspopup="dialog"
            aria-expanded={open}
            title={DOWNLOAD_COPY.menuLabel}
            className="grid size-6 place-items-center rounded-sm hover:bg-accent"
          >
            <span aria-hidden="true" className="text-xs">▾</span>
          </button>
        </PopoverTrigger>
        <PopoverContent className="w-[380px] p-3" align="end" data-testid="node-download-panel">
          <div className="flex flex-col gap-2">
            <Button variant="outline" size="sm" data-testid="node-download-menu-verify" disabled={busy} onClick={onVerify}>
              {DOWNLOAD_COPY.verifyLabel}
            </Button>
            {busy && <div className="node-verify text-aux text-muted-foreground">{DOWNLOAD_COPY.verifyBusy}</div>}
            {verdict !== null && (
              <div
                className={`node-verify rounded-sm px-2 py-1 text-dense ${verdict ? 'bg-success/10 text-success' : 'bg-destructive/10 text-destructive'}`}
                data-testid="node-download-verify"
              >
                {verdict ? (
                  <>
                    {DOWNLOAD_COPY.verifyOk}
                    <div className="sub font-mono text-aux" lang="en">{download?.sha}</div>
                  </>
                ) : (
                  <>
                    {DOWNLOAD_COPY.verifyBad}
                    <div className="sub font-mono text-aux" lang="en">local {download?.sha}</div>
                  </>
                )}
              </div>
            )}
            <div className="border-t border-border pt-2" data-testid="node-download-checksums">
              <p className="mb-1 text-aux text-muted-foreground">{DOWNLOAD_COPY.checksumsHeader}</p>
              {!item ? (
                <p className="text-dense text-muted-foreground">{tt('元数据加载中…')}</p>
              ) : (
                <>
                  <div className="kv mb-1 flex gap-2 text-dense">
                    <span className="k w-24 shrink-0 text-muted-foreground">{DOWNLOAD_COPY.mimeTypeLabel}</span>
                    <span className="min-w-0 break-all font-mono" lang="en">{item.mimeType ?? EMPTY_VALUE}</span>
                  </div>
                  {(['sha256', 'sha1', 'md5'] as const).map((algo) => {
                    const v = item.checksums?.[algo]
                    if (!v) return null
                    const orig = item.originalChecksums?.[algo]
                    return (
                      <div className="kv mb-1 flex gap-2 text-dense" key={algo}>
                        <span className="k w-24 shrink-0 text-muted-foreground">{algo}</span>
                        <span className="min-w-0 break-all font-mono" lang="en">
                          {v.length > 24 ? `${v.slice(0, 20)}…${v.slice(-8)}` : v}
                          <CopyButton value={v} label={algo} />
                          {orig && (
                            <span
                              className={`checksum-badge ml-1 rounded-sm px-1 text-[11px] ${orig === v ? 'bg-success/10 text-success' : 'bg-warning/10 text-warning'}`}
                              title={tt('客户端上传时提供的 checksum 与服务端实际值比对')}
                            >
                              {tt('上传时提供：')}{orig === v ? tt('一致 ✓') : tt('不一致')}
                            </span>
                          )}
                        </span>
                      </div>
                    )
                  })}
                </>
              )}
            </div>
            <p className="text-aux text-muted-foreground">{DOWNLOAD_COPY.verifyHint}</p>
          </div>
        </PopoverContent>
      </Popover>
    </>
  )
}

// ---- 常规 Tab：仓库形态 ----

function RepoGeneral({ repoKey }: { repoKey: string }) {
  const meta = useAsync(() => getRepoDetail(repoKey), [repoKey])
  const usage = useAsync(() => getRepoUsageCounts(repoKey), [repoKey])

  if (meta.status === 'loading') return <StateSkeleton lines={4} />
  if (meta.status === 'forbidden') {
    return (
      <p className="text-dense text-muted-foreground">
        {tt('仓库元数据为管理员视图（HTTP 403）——树按 generic 语义呈现；上传/删除权限由内容面按路径 ACL 判定。')}
      </p>
    )
  }
  if (meta.status === 'error' || !meta.data) {
    return (
      <p className="text-dense text-muted-foreground">
        {tt('详情加载失败（HTTP')} {meta.error?.status ?? 0}{tt('）。')}
      </p>
    )
  }
  const m = meta.data
  return (
    <div className="node-detail-grid">
      <div>
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">{tt('名称')}</span>
          <span className="font-mono" lang="en">{m.key}</span>
        </div>
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">{tt('包类型')}</span>
          <span className="flex gap-1">
            <span className="badge neutral rounded-sm bg-secondary px-1.5 py-px text-[11px]">{m.packageType}</span>
            <span className="badge neutral rounded-sm bg-secondary px-1.5 py-px text-[11px]">{m.rclass}</span>
          </span>
        </div>
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">Repository Path</span>
          <span className="min-w-0 break-all font-mono" lang="en">
            {m.key}/ <CopyButton value={`${m.key}/`} label={tt('仓库路径')} />
          </span>
        </div>
        {m.url && (
          <div className="kv mb-1 flex gap-2 text-dense">
            <span className="k w-36 shrink-0 text-muted-foreground">File URL</span>
            <span className="min-w-0 break-all font-mono" lang="en" data-testid="node-file-url">
              {m.url} <CopyButton value={m.url} label="File URL" />
            </span>
          </div>
        )}
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">{REPO_FIELD_LABELS.repoLayout}</span>
          <span className="font-mono" data-testid="node-repo-layout" title={NO_SOURCE_HINTS.repoLayout}>
            {EMPTY_VALUE}
          </span>
        </div>
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">{REPO_FIELD_LABELS.description}</span>
          <span data-testid="node-repo-description">{m.description || EMPTY_VALUE}</span>
        </div>
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">{REPO_FIELD_LABELS.created}</span>
          <span className="font-mono" data-testid="node-repo-created" title={NO_SOURCE_HINTS.repoCreated}>
            {EMPTY_VALUE}
          </span>
        </div>
        {usage.status === 'ok' && usage.data && (
          <>
            <div className="kv mb-1 flex gap-2 text-dense">
              <span className="k w-36 shrink-0 text-muted-foreground">{REPO_FIELD_LABELS.artifactCount}</span>
              <span className="font-mono" data-testid="node-repo-artifact-count">
                {usage.data.nodeCount}
              </span>
            </div>
            <div className="kv mb-1 flex gap-2 text-dense">
              <span className="k w-36 shrink-0 text-muted-foreground">{tt('大小')}</span>
              <span className="font-mono">{formatBytes(usage.data.usedBytes)}</span>
              {usage.data.quotaBytes > 0 && (
                <span className="text-muted-foreground"> {tt('/ 配额')} {formatBytes(usage.data.quotaBytes)}</span>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

// ---- 常规 Tab：目录 / 文件形态 ----

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
  const statsRefreshKey =
    download?.path === node.path && download.phase === 'done' ? `done:${download.sha}` : 'base'

  if (itemStatus === 'loading') return <StateSkeleton lines={4} />
  if (itemStatus !== 'ok' || !item) {
    if (node.remote) {
      return (
        <p className="text-dense text-muted-foreground" data-testid="node-remote-error">
          {REMOTE_COPY.fetchFailed(itemError?.status ?? 0)}
        </p>
      )
    }
    return (
      <p className="text-dense text-muted-foreground">
        {tt('详情加载失败（HTTP')} {itemError?.status ?? 0}{tt('）——列表数据仍然有效。')}
      </p>
    )
  }
  return (
    <div className="node-detail-grid">
      <div>
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">{tt('名称')}</span>
          <span className="font-mono" lang="en">{node.name}</span>
        </div>
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">Repository Path</span>
          <span className="min-w-0 break-all font-mono" lang="en">
            {repoKey}/{nodeRef} <CopyButton value={`${repoKey}/${nodeRef}`} label={tt('制品路径')} />
          </span>
        </div>
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">File URL</span>
          <span className="min-w-0 break-all font-mono" lang="en" data-testid="node-file-url">
            {fileURL} <CopyButton value={fileURL} label="File URL" />
          </span>
        </div>
        {!node.folder && <ModuleIdRow repoKey={repoKey} path={node.path} />}
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">{tt('部署者')}</span>
          <span lang="en">{item.createdBy || EMPTY_VALUE}</span>
        </div>
        {!node.folder && (
          <div className="kv mb-1 flex gap-2 text-dense">
            <span className="k w-36 shrink-0 text-muted-foreground">{tt('大小')}</span>
            <span className="font-mono">{item.size}</span>
          </div>
        )}
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">Created</span>
          <span className="font-mono">{item.created || EMPTY_VALUE}</span>
        </div>
        {item.lastModified && (
          <div className="kv mb-1 flex gap-2 text-dense">
            <span className="k w-36 shrink-0 text-muted-foreground">{tt('修改时间')}</span>
            <span className="font-mono">{item.lastModified}</span>
          </div>
        )}
        {!node.folder && <FileStatsRows repoKey={repoKey} path={node.path} refreshKey={statsRefreshKey} />}
        {node.folder && (
          <>
            <div className="kv mb-1 flex gap-2 text-dense">
              <span className="k w-36 shrink-0 text-muted-foreground">{tt('子项（Artifact Count）')}</span>
              <span className="font-mono">
                {childrenNodes
                  ? tt('目录 {v1} · 文件 {v2}', {
                      v1: childrenNodes.filter((n) => n.folder).length,
                      v2: childrenNodes.filter((n) => !n.folder).length,
                    })
                  : tt('{v1} 项', { v1: item.children?.length ?? 0 })}
              </span>
            </div>
            {childrenNodes && childrenNodes.some((n) => !n.folder && n.size !== null) && (
              <div className="kv mb-1 flex gap-2 text-dense">
                <span className="k w-36 shrink-0 text-muted-foreground">{tt('Size（直系文件合计）')}</span>
                <span className="font-mono">
                  {formatBytes(childrenNodes.reduce((acc, n) => acc + (!n.folder && n.size !== null ? n.size : 0), 0))}
                </span>
              </div>
            )}
          </>
        )}
        <div className="kv mb-1 flex gap-2 text-dense">
          <span className="k w-36 shrink-0 text-muted-foreground">{tt('类型')}</span>
          <span>{node.folder ? tt('目录') : tt('文件')}</span>
        </div>
        {!node.folder && node.tags && node.tags.length > 0 && (
          <div className="kv mb-1 flex gap-2 text-dense">
            <span className="k w-36 shrink-0 text-muted-foreground">tags</span>
            <span className="flex flex-wrap gap-1">
              {node.tags.map((tag) => (
                <span
                  key={tag}
                  className="badge neutral rounded-sm bg-secondary px-1.5 py-px text-[11px]"
                  data-testid={`tag-badge-${tag}`}
                  title={`tag: ${tag}`}
                >
                  {tag}
                </span>
              ))}
            </span>
          </div>
        )}
      </div>
    </div>
  )
}

// ---- Module ID 行（T-512——build.* 属性三键 → run 探测 → 深链）----

function ModuleIdRow({ repoKey, path }: { repoKey: string; path: string }) {
  const props = useAsync(() => getNodeProperties(repoKey, path), [repoKey, path])
  const probeTitle = tt('制品的 build module 关联：节点 build.name/build.number/build.timestamp 属性三键（CI 矩阵参数部署时写入）→ run 详情的 modules[].artifacts[] 路径精确匹配；无反查端点（artifacts(build) AQL 入口为 M18+ 翻转点）。')

  if (props.status === 'loading') {
    return (
      <div className="kv mb-1 flex gap-2 text-dense">
        <span className="k w-36 shrink-0 text-muted-foreground">Module ID</span>
        <span className="font-mono" data-testid="node-module-id">{STATS_HINTS.loading}</span>
      </div>
    )
  }
  if (props.status !== 'ok') {
    const errTitle = `${tt('module 关联探测不可用（HTTP')} ${props.error?.status ?? 0}${tt('）')}`
    return (
      <div className="kv mb-1 flex gap-2 text-dense">
        <span className="k w-36 shrink-0 text-muted-foreground">Module ID</span>
        <span className="font-mono" data-testid="node-module-id" title={errTitle}>{EMPTY_VALUE}</span>
      </div>
    )
  }
  const assoc = buildAssocOf(props.data ?? {})
  if (!assoc) {
    return (
      <div className="kv mb-1 flex gap-2 text-dense">
        <span className="k w-36 shrink-0 text-muted-foreground">Module ID</span>
        <span className="font-mono" data-testid="node-module-id" title={tt('无 build 关联——节点未携带 build.* 属性族（CI 以矩阵参数部署时写入 build.name/build.number/build.timestamp）。')}>{EMPTY_VALUE}</span>
      </div>
    )
  }
  return <ModuleIdProbe repoKey={repoKey} path={path} assoc={assoc} probeTitle={probeTitle} />
}

function ModuleIdProbe({
  repoKey,
  path,
  assoc,
  probeTitle,
}: {
  repoKey: string
  path: string
  assoc: { name: string; number: string; startedISO?: string }
  probeTitle: string
}) {
  const run = useAsync(
    () => getBuildRun(assoc.name, assoc.number, { started: assoc.startedISO }),
    [assoc.name, assoc.number, assoc.startedISO ?? ''],
  )

  let body: ReactNode
  if (run.status === 'loading') {
    body = <span>{STATS_HINTS.loading}</span>
  } else if (run.status !== 'ok' || !run.data) {
    const errTitle = `${tt('build.* 属性指向的 run 不可达（HTTP')} ${run.error?.status ?? 0}${tt('）——403 = 无该 build 读门 / 404 = run 不在本实例；不伪造关联。')}`
    body = <span title={errTitle}>{EMPTY_VALUE}</span>
  } else {
    const ids = moduleIdsOf(run.data.buildInfo, repoKey, path)
    if (ids.length === 0) {
      body = <span title={tt('build.* 属性指向的 run 模块不含本制品（同名同号多 run 或关联已失）——不伪造。')}>{EMPTY_VALUE}</span>
    } else {
      const to = `/builds/${encodeURIComponent(assoc.name)}/${encodeURIComponent(assoc.number)}${
        run.data.buildInfo.started ? `?started=${encodeURIComponent(run.data.buildInfo.started)}` : ''
      }`
      body = (
        <>
          {ids.map((id, i) => (
            <span key={id}>
              {i > 0 && ' '}
              <Link className="row-link font-mono" lang="en" to={to} title={probeTitle} data-testid="node-module-id">
                {id}
              </Link>
            </span>
          ))}
          <span className="ml-2 font-mono text-aux text-muted-foreground" lang="en">
            {assoc.name}#{assoc.number}
          </span>
        </>
      )
    }
  }
  return (
    <div className="kv mb-1 flex gap-2 text-dense">
      <span className="k w-36 shrink-0 text-muted-foreground" title={probeTitle}>Module ID</span>
      <span className="min-w-0 font-mono" title={probeTitle}>{body}</span>
    </div>
  )
}

// ---- 下载统计族（file 形态——?stats 面）----

function FileStatsRows({ repoKey, path, refreshKey }: { repoKey: string; path: string; refreshKey: string }) {
  const stats = useAsync(() => getNodeStats(repoKey, path), [repoKey, path, refreshKey])
  const d = stats.status === 'ok' ? stats.data : null
  const errTitle =
    stats.status === 'error' || stats.status === 'forbidden' ? STATS_HINTS.unavailable(stats.error?.status ?? 0) : undefined
  const v = (value: string | number | undefined): string => {
    if (stats.status === 'loading') return STATS_HINTS.loading
    if (!d) return EMPTY_VALUE
    return value === undefined || value === '' ? EMPTY_VALUE : String(value)
  }
  const busyText = stats.status === 'loading' ? STATS_HINTS.loading : EMPTY_VALUE
  const row = (label: string, testid: string, content: ReactNode, lang?: 'en') => (
    <div className="kv mb-1 flex gap-2 text-dense">
      <span className="k w-36 shrink-0 text-muted-foreground">{label}</span>
      <span className="font-mono" data-testid={testid} title={errTitle} lang={lang}>
        {content}
      </span>
    </div>
  )
  return (
    <>
      {row(STATS_LABELS.downloads, 'node-downloads', d ? String(d.downloadCount) : busyText)}
      {row(STATS_LABELS.lastDownloadedBy, 'node-last-downloaded-by', v(d?.lastDownloadedBy), 'en')}
      {row(STATS_LABELS.lastDownloaded, 'node-last-downloaded', v(d?.lastDownloaded))}
      {row(STATS_LABELS.remoteDownloads, 'node-remote-downloads', d ? String(d.remoteDownloadCount) : busyText)}
    </>
  )
}

// ---- 有效权限 Tab（admin；?permissions SE-08）----

function PermsTab({ target }: { target: DetailTarget }) {
  const path = target.kind === 'repo' ? '' : target.node.folder ? `${target.node.path}/` : target.node.path
  const perms = useAsync(() => getItemPermissions(target.repoKey, path), [target.repoKey, path])

  return (
    <div className="node-perms" data-testid="node-perms">
      {perms.status === 'loading' && <StateSkeleton lines={2} />}
      {perms.status === 'error' && (
        <p className="text-dense text-muted-foreground" title={perms.error?.message}>
          {tt('权限视图不可用（HTTP')} {perms.error?.status}{tt('）')}
        </p>
      )}
      {perms.status === 'forbidden' && (
        <EmptyState message={tt('无权限查看有效权限')} hint={tt('该视图按 permission target 授予面呈现（admin 视图）。')} />
      )}
      {perms.status === 'ok' && perms.data && <PrincipalList view={perms.data} />}
      <p className="field-hint mt-2 text-aux text-muted-foreground">
        {tt('该视图与服务端授权判定同源；授权编辑见')}{' '}
        <Link to="/admin/security/permissions" className="text-primary underline" target="_blank">{tt('权限 target')}</Link>
        {tt('。')}
      </p>
    </div>
  )
}

function PrincipalList({ view }: { view: { principals: { users: Record<string, string[]>; groups: Record<string, string[]> } } }) {
  const users = Object.entries(view.principals.users ?? {})
  const groups = Object.entries(view.principals.groups ?? {})
  if (users.length === 0 && groups.length === 0) {
    return <p className="text-dense text-muted-foreground">{tt('没有 permission target 覆盖此路径（admin 隐式全权）。')}</p>
  }
  return (
    <div className="perm-chips flex flex-wrap gap-1.5">
      {users.map(([name, bits]) => (
        <span className="chip-item flex items-center gap-1 rounded-sm border border-border bg-surface-2 px-2 py-0.5 text-dense" key={`u-${name}`}>
          <span lang="en">{name}</span>
          <span className="font-mono text-aux">{bits.join('')}</span>
        </span>
      ))}
      {groups.map(([name, bits]) => (
        <span className="chip-item flex items-center gap-1 rounded-sm border border-border bg-surface-2 px-2 py-0.5 text-dense" key={`g-${name}`}>
          <span aria-hidden="true">👥</span>
          <span lang="en">{name}</span>
          <span className="font-mono text-aux">{bits.join('')}</span>
        </span>
      ))}
    </div>
  )
}
