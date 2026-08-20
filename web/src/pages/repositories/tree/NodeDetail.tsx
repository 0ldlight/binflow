import { Link } from 'react-router-dom'

import { useAuth } from '../../../app/AuthContext'
import { CopyButton } from '../../../components/CopyButton'
import { Skeleton } from '../../../components/Skeleton'
import { useAsync } from '../../../lib/useAsync'
import { getItem, getItemPermissions } from './lib'
import type { ChildNode } from './lib'

// 选中节点详情面板（console-ux §4.6 表格下方展开，非跳页）：FileInfo 全
// 字段（digest 全 mono + 拷贝，P2）、下载对账徽标（W13）、?permissions
// admin 面（SE-08，§3.6.3：403 → 该面隐藏，不产生新锚——admin 位仅做
// 预收敛，省掉明知 403 的请求）。

export interface DownloadState {
  path: string
  phase: 'loading' | 'done'
  sha: string
  /** undefined = 无对账源（根目录层无 list 合并值） */
  match: boolean | undefined
}

export default function NodeDetail({
  repoKey,
  node,
  download,
  onDownload,
  onClose,
}: {
  repoKey: string
  node: ChildNode
  download: DownloadState | null
  onDownload: (node: ChildNode, expectedSha: string) => void
  onClose: () => void
}) {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const item = useAsync(() => getItem(repoKey, node.path), [repoKey, node.path])
  const perms = useAsync(
    () => (admin ? getItemPermissions(repoKey, node.folder ? `${node.path}/` : node.path) : Promise.resolve(null)),
    [admin, repoKey, node.path],
  )

  const busy = download?.path === node.path && download.phase === 'loading'
  const verdict =
    download?.path === node.path && download.phase === 'done' && download.match !== undefined ? download.match : null

  return (
    <section className="card section node-detail" data-testid="node-detail" aria-label="节点详情">
      <header className="node-detail-head">
        <h3>
          <span aria-hidden="true">{node.folder ? '◻' : '◾'}</span>{' '}
          <span className="mono" lang="en">{node.path}</span>
        </h3>
        <div className="node-detail-actions">
          {!node.folder && (
            <>
              <button
                type="button"
                className="btn"
                disabled={busy}
                data-testid="node-download"
                onClick={() => {
                  const sha = item.data?.checksums?.sha256 ?? node.sha256 ?? ''
                  onDownload(node, sha)
                }}
              >
                {busy ? '下载中…' : '下载并校验'}
              </button>
              <a
                className="btn"
                href={`/binflow/${encodeURIComponent(repoKey)}/${node.path
                  .split('/')
                  .map((s) => encodeURIComponent(s))
                  .join('/')}`}
                download={node.name}
                title="大文件建议直接下载（不经浏览器 sha256 对账）"
              >
                直接下载
              </a>
            </>
          )}
          <button type="button" className="btn" onClick={onClose}>
            关闭
          </button>
        </div>
      </header>

      {item.status === 'loading' && <Skeleton lines={4} />}
      {item.status === 'error' && (
        <p className="text-2">详情加载失败（HTTP {item.error?.status}）——列表数据仍然有效。</p>
      )}
      {item.status === 'ok' && item.data && (
        <div className="node-detail-grid">
          <div>
            <div className="kv">
              <span className="k">类型</span>
              <span>{node.folder ? '目录' : '文件'}</span>
            </div>
            {!node.folder && (
              <>
                <div className="kv">
                  <span className="k">大小</span>
                  <span className="mono">{item.data.size}</span>
                </div>
                <div className="kv">
                  <span className="k">mimeType</span>
                  <span className="mono" lang="en">{item.data.mimeType ?? '—'}</span>
                </div>
              </>
            )}
            <div className="kv">
              <span className="k">createdBy</span>
              <span lang="en">{item.data.createdBy || '—'}</span>
            </div>
            <div className="kv">
              <span className="k">created</span>
              <span className="mono">{item.data.created || '—'}</span>
            </div>
            {item.data.lastModified && (
              <div className="kv">
                <span className="k">lastModified</span>
                <span className="mono">{item.data.lastModified}</span>
              </div>
            )}
            <div className="kv">
              <span className="k">uri</span>
              <span className="mono" style={{ wordBreak: 'break-all' }} lang="en">
                {item.data.uri} <CopyButton value={item.data.uri} label="uri" />
              </span>
            </div>
            {!node.folder && item.data.checksums && (
              <>
                {(['sha256', 'sha1', 'md5'] as const).map((algo) => {
                  const v = item.data?.checksums?.[algo]
                  if (!v) return null
                  return (
                    <div className="kv" key={algo}>
                      <span className="k">{algo}</span>
                      <span className="mono" lang="en">
                        <span data-testid={`node-copy-${algo}`}>
                          {v.length > 24 ? `${v.slice(0, 20)}…${v.slice(-8)}` : v}
                          <CopyButton value={v} label={algo} />
                        </span>
                      </span>
                    </div>
                  )
                })}
              </>
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
            {admin && (
              <div className="node-perms" data-testid="node-perms">
                <h4>有效权限（?permissions，admin 视图）</h4>
                {perms.status === 'loading' && <Skeleton lines={2} />}
                {perms.status === 'error' && (
                  <p className="text-2" title={perms.error?.message}>
                    权限视图不可用（HTTP {perms.error?.status}）
                  </p>
                )}
                {perms.status === 'ok' && perms.data && <PrincipalList view={perms.data} />}
                <p className="field-hint">
                  该视图与服务端授权判定同源；组编辑见{' '}
                  <Link to="/security/permissions" target="_blank">
                    权限 target
                  </Link>
                  。
                </p>
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  )
}

function PrincipalList({ view }: { view: { principals: { users: Record<string, string[]>; groups: Record<string, string[]> } } }) {
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
