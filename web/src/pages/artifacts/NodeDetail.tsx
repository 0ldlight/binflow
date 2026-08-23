import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { useAuth } from '../../app/AuthContext'
import { CopyButton } from '../../components/CopyButton'
import { Skeleton } from '../../components/Skeleton'
import { formatBytes } from '../../lib/format'
import { getRepoDetail, getRepoUsage } from '../../lib/repos'
import { useAsync } from '../../lib/useAsync'
import { getItem, getItemPermissions } from './lib'
import type { ChildNode, ItemInfo } from './lib'

// 详情面板（console-m8 §3.3 C4 / §6.3[2]——跨仓树右联）：
//
// - 三形态：仓库（getRepoDetail + usage）/ 目录（FolderInfo）/ 文件
//   （FileInfo 全字段）；Tab = 常规 + 有效权限（admin 渲染——admin 位仅做
//   预收敛省掉明知 403 的请求，§3.6.3）。
// - 字段序对齐 reverse §3.2：名称 → 包类型 → Repository Path → File URL
//   → 计数/大小 → 部署者/Created；文件附 Checksums 块（sha256/sha1/md5
//   各带「（上传时提供：一致）」徽标——映射 originalChecksums 比对）。
// - docker 特化：manifest digest 行的 tag 徽标（T-134 G32a 随迁）。
// - Properties / Followers / Xray Tab 不建（无后端 / Non-goal）。

export interface DownloadState {
  path: string
  phase: 'loading' | 'done'
  sha: string
  /** undefined = 无对账源（根目录层无 list 合并值） */
  match: boolean | undefined
}

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
  onDelete,
}: {
  target: DetailTarget
  download: DownloadState | null
  onDownload: (node: ChildNode, expectedSha: string) => void
  onClose: () => void
  /** 删除入口可见性（readonly_admin 预收敛禁用；普通用户走服务端 403） */
  canDelete: boolean
  onDelete: (node: ChildNode) => void
}) {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const [tab, setTab] = useState<'general' | 'perms'>('general')
  useEffect(() => setTab('general'), [target])

  // 节点元数据（文件/目录）在顶层取——头部「下载并校验」按钮的对账源
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
            <>
              <button
                type="button"
                className="btn"
                disabled={download?.path === target.node.path && download.phase === 'loading'}
                data-testid="node-download"
                onClick={() => onDownload(target.node, item?.checksums?.sha256 ?? target.node.sha256 ?? '')}
                title="下载并做 sha256 对账"
              >
                {download?.path === target.node.path && download.phase === 'loading' ? '下载中…' : '下载并校验'}
              </button>
              <a
                className="btn"
                href={`/binflow/${encodeURIComponent(target.repoKey)}/${target.node.path
                  .split('/')
                  .map((s) => encodeURIComponent(s))
                  .join('/')}`}
                download={target.node.name}
                title="大文件建议直接下载（不经浏览器 sha256 对账）"
              >
                直接下载
              </a>
            </>
          )}
          {target.kind === 'node' && canDelete && (
            <button type="button" className="btn danger" data-testid="delete-node-button" onClick={() => onDelete(target.node)}>
              删除
            </button>
          )}
          <button type="button" className="btn" onClick={onClose}>
            关闭
          </button>
        </div>
      </header>

      <div className="node-tabs" role="tablist" aria-label="详情视图">
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'general'}
          className={`node-tab${tab === 'general' ? ' active' : ''}`}
          data-testid="node-tab-general"
          onClick={() => setTab('general')}
        >
          常规
        </button>
        {admin && (
          <button
            type="button"
            role="tab"
            aria-selected={tab === 'perms'}
            className={`node-tab${tab === 'perms' ? ' active' : ''}`}
            data-testid="node-tab-perms"
            onClick={() => setTab('perms')}
          >
            有效权限
          </button>
        )}
      </div>

      {tab === 'general' ? (
        target.kind === 'repo' ? (
          <RepoGeneral repoKey={target.repoKey} />
        ) : (
          <NodeGeneral node={target.node} repoKey={target.repoKey} item={item} itemStatus={nodeItem.status} itemError={nodeItem.error} download={download} />
        )
      ) : (
        <PermsTab target={target} />
      )}
    </section>
  )
}

// ---- 常规 Tab：仓库形态 ----------------------------------------------------

function RepoGeneral({ repoKey }: { repoKey: string }) {
  const meta = useAsync(() => getRepoDetail(repoKey), [repoKey])
  const usage = useAsync(() => getRepoUsage(repoKey), [repoKey])

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
            <span className="badge neutral">{m.packageType}</span>{' '}
            <span className="badge neutral">{m.rclass}</span>
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
            <span className="mono" style={{ wordBreak: 'break-all' }} lang="en">
              {m.url} <CopyButton value={m.url} label="File URL" />
            </span>
          </div>
        )}
        {usage.status === 'ok' && usage.data && (
          <div className="kv">
            <span className="k">大小</span>
            <span className="mono">{formatBytes(usage.data.usedBytes)}</span>
            {usage.data.quotaBytes > 0 && (
              <span className="text-2"> / 配额 {formatBytes(usage.data.quotaBytes)}</span>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

// ---- 常规 Tab：目录 / 文件形态 ----------------------------------------------

function NodeGeneral({
  node,
  repoKey,
  item,
  itemStatus,
  itemError,
  download,
}: {
  node: ChildNode
  repoKey: string
  item: ItemInfo | null
  itemStatus: 'loading' | 'ok' | 'error' | 'forbidden'
  itemError: { status: number } | null
  download: DownloadState | null
}) {
  const busy = download?.path === node.path && download.phase === 'loading'
  const verdict =
    download?.path === node.path && download.phase === 'done' && download.match !== undefined ? download.match : null
  const nodeRef = node.folder ? `${node.path}/` : node.path

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
          <span className="k">类型</span>
          <span>{node.folder ? '目录' : '文件'}</span>
        </div>
        {!node.folder && (
          <>
            <div className="kv">
              <span className="k">大小</span>
              <span className="mono">{item.size}</span>
            </div>
            <div className="kv">
              <span className="k">mimeType</span>
              <span className="mono" lang="en">{item.mimeType ?? '—'}</span>
            </div>
          </>
        )}
        {node.folder && (
          <div className="kv">
            <span className="k">子项</span>
            <span className="mono">{item.children?.length ?? 0} 项</span>
          </div>
        )}
        <div className="kv">
          <span className="k">Repository Path</span>
          <span className="mono" style={{ wordBreak: 'break-all' }} lang="en">
            {repoKey}/{nodeRef} <CopyButton value={`${repoKey}/${nodeRef}`} label="制品路径" />
          </span>
        </div>
        <div className="kv">
          <span className="k">部署者</span>
          <span lang="en">{item.createdBy || '—'}</span>
        </div>
        <div className="kv">
          <span className="k">Created</span>
          <span className="mono">{item.created || '—'}</span>
        </div>
        {item.lastModified && (
          <div className="kv">
            <span className="k">修改时间</span>
            <span className="mono">{item.lastModified}</span>
          </div>
        )}
        {!node.folder && item.checksums && (
          <>
            {(['sha256', 'sha1', 'md5'] as const).map((algo) => {
              const v = item.checksums?.[algo]
              if (!v) return null
              const orig = item.originalChecksums?.[algo]
              return (
                <div className="kv" key={algo}>
                  <span className="k">{algo}</span>
                  <span className="mono" lang="en">
                    <span data-testid={`node-copy-${algo}`}>
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
        {/* docker 特化：manifest digest 行的 tag 徽标（T-134 G32a） */}
        {!node.folder && node.tags && node.tags.length > 0 && (
          <div className="kv" data-testid="node-tags">
            <span className="k">tags</span>
            <span>
              {node.tags.map((tag) => (
                <span key={tag} className="badge neutral" data-testid={`tag-badge-${tag}`} title={`tag: ${tag}`}>
                  {tag}
                </span>
              ))}
            </span>
          </div>
        )}
      </div>
      <div>
        {verdict !== null && (
          <div className={`node-verify ${verdict ? 'ok' : 'bad'}`} data-testid="node-download-verify">
            {verdict ? (
              <>
                ✓ 下载落盘 sha256 与服务端一致
                <div className="mono sub" lang="en">{download?.sha}</div>
              </>
            ) : (
              <>
                ✗ 不一致！下载内容与服务端登记的 checksum 不匹配
                <div className="mono sub" lang="en">local {download?.sha}</div>
              </>
            )}
          </div>
        )}
        {busy && <div className="node-verify">正在下载并计算 sha256（大文件稍慢）…</div>}
      </div>
    </div>
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
