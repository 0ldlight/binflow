import { useCallback, useMemo, useRef, useState } from 'react'
import type { DragEvent } from 'react'
import { Link } from 'react-router-dom'

import { useAuth } from '../../../app/AuthContext'
import { ApiError } from '../../../lib/api'
import { formatBytes } from '../../../lib/format'
import { putArtifact } from './lib'
import type { UploadProgress } from './lib'
import { blobSha256 } from './sha256'

// 上传对话框（console-ux §4.7；W13/W12d）：拖拽多文件、行级进度/速度、
// 浏览器端流式 sha256 + 服务端比对徽标、E-11 错误语义原样呈现
// （409 双值或 pattern / 403 权限指引 / 413 quota 文案）。maven 仓按
// §4.7 以 GAV 表单生成 layout 路径并做前端预检（layout.go 的
// 「至少 3 层目录 + 文件名 = module-version[-classifier].ext」同口径）。
//
// 队列模型：行一次只跑一个（hash → PUT 串行；逐文件进度清晰、不打爆
// 服务端配额预检）。关闭对话框 = 落闸（closedRef，泵循环断）+ abort 全部
// 在飞 XHR——排队中的剩余文件不再上传（review B1）。

export interface UploadDialogProps {
  repoKey: string
  /** generic：自由路径；maven：GAV 表单生成 */
  mode: 'generic' | 'maven'
  /** 目标目录（repo 相对，无尾斜杠；'' = 根） */
  dir: string
  onClose: () => void
  /** 任一行成功后通知父级刷新 */
  onUploaded: () => void
}

type Phase = 'hashing' | 'queued' | 'uploading' | 'done' | 'error'

interface Row {
  id: number
  file: File
  /** 展示名 = 上传后的目标文件名（maven 表单会改名） */
  fileName: string
  /** 目标目录（repo 相对） */
  targetDir: string
  phase: Phase
  localSha: string | null
  serverSha: string
  loaded: number
  total: number
  speed: number
  error: ApiError | null
}

interface GavForm {
  groupId: string
  artifactId: string
  version: string
  classifier: string
  packaging: string
}

const EMPTY_GAV: GavForm = { groupId: '', artifactId: '', version: '', classifier: '', packaging: 'jar' }

/** maven layout 路径生成 + 前端预检（与服务端 Parse 同口径） */
export function mavenTarget(f: GavForm): { dir: string; file: string; error: string | null } {
  const g = f.groupId.trim()
  const a = f.artifactId.trim()
  const v = f.version.trim()
  const c = f.classifier.trim()
  const p = f.packaging.trim() || 'jar'
  if (g === '') return { dir: '', file: '', error: 'groupId 不能为空' }
  if (g.includes('/')) return { dir: '', file: '', error: 'groupId 用点分隔，不能包含 /' }
  if (g.split('.').some((seg) => seg === '')) return { dir: '', file: '', error: 'groupId 有空段（连续点）' }
  if (a === '') return { dir: '', file: '', error: 'artifactId 不能为空' }
  if (a.includes('/') || a.includes(':')) return { dir: '', file: '', error: 'artifactId 不能包含 / 或 :' }
  if (v === '') return { dir: '', file: '', error: 'version 不能为空' }
  if (v === '-SNAPSHOT') return { dir: '', file: '', error: 'version 不能是裸 -SNAPSHOT' }
  if (v.includes('/')) return { dir: '', file: '', error: 'version 不能包含 /' }
  if (c.includes('/') || c.includes('.')) return { dir: '', file: '', error: 'classifier 不能包含 / 或 .' }
  if (p.includes('/') || p.includes('.')) return { dir: '', file: '', error: 'packaging 不能包含 / 或 .' }
  const file = `${a}-${v}${c ? `-${c}` : ''}.${p}`
  const dir = `${g.split('.').join('/')}/${a}/${v}`
  return { dir, file, error: null }
}

export default function UploadDialog({ repoKey, mode, dir, onClose, onUploaded }: UploadDialogProps) {
  const { session } = useAuth()
  const admin = session?.admin ?? false
  const [rows, setRows] = useState<Row[]>([])
  const rowsRef = useRef<Row[]>([])
  const [target, setTarget] = useState(dir === '' ? '' : `${dir}/`)
  const [sendChecksum, setSendChecksum] = useState(true)
  const [dragOver, setDragOver] = useState(false)
  const [gav, setGav] = useState<GavForm>(EMPTY_GAV)
  const nextId = useRef(1)
  const xhrs = useRef(new Set<XMLHttpRequest>())
  const pumping = useRef(false)
  // B1（review）：关闭闸——close() 置位后泵不再发起新 PUT（对白框重开是
  // 新组件实例，闸随实例重建）
  const closedRef = useRef(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const mavenInput = useRef<HTMLInputElement>(null)
  // checkbox 的读取时点：入队那一刻的值决定是否携带 header（hint 已注明）
  const sendFlag = useRef(sendChecksum)
  sendFlag.current = sendChecksum

  const patch = useCallback((id: number, p: Partial<Row>) => {
    // rowsRef 同步改（泵读 ref，避免 setState 异步导致的取旧队），state
    // 只服务渲染
    rowsRef.current = rowsRef.current.map((r) => (r.id === id ? { ...r, ...p } : r))
    setRows(rowsRef.current)
  }, [])

  const pump = useCallback(async () => {
    if (pumping.current) return
    pumping.current = true
    try {
      for (;;) {
        // B1（review）：关闭即停队列——abort 的 rejection 回到 catch 后这里
        // 直接断泵，不再 find 下一行发起新 PUT（否则剩余排队文件在无 UI
        // 反馈下继续落库、烧配额）
        if (closedRef.current) break
        const row = rowsRef.current.find((r) => r.phase === 'hashing' || r.phase === 'queued')
        if (!row) break
        let sha = row.localSha
        if (row.phase === 'hashing') {
          try {
            sha = await blobSha256(row.file)
            patch(row.id, { localSha: sha, phase: 'queued' })
          } catch {
            patch(row.id, { phase: 'error', error: new ApiError(0, '本地 sha256 计算失败') })
            continue
          }
        }
        patch(row.id, { phase: 'uploading', loaded: 0, total: row.file.size, speed: 0 })
        let lastAt = performance.now()
        let lastLoaded = 0
        try {
          const out = await putArtifact({
            repoKey,
            path: row.targetDir === '' ? row.fileName : `${row.targetDir}/${row.fileName}`,
            file: row.file,
            sha256: sendFlag.current && sha ? sha : undefined,
            onProgress: (p: UploadProgress) => {
              const now = performance.now()
              const dt = (now - lastAt) / 1000
              if (dt > 0.2) {
                const speed = (p.loaded - lastLoaded) / dt
                lastAt = now
                lastLoaded = p.loaded
                patch(row.id, { loaded: p.loaded, speed })
              } else {
                patch(row.id, { loaded: p.loaded })
              }
            },
            registerXhr: (xhr) => xhrs.current.add(xhr),
          })
          if (!closedRef.current) {
            patch(row.id, { phase: 'done', serverSha: out.serverSha256, loaded: row.file.size })
            onUploaded()
          }
        } catch (err) {
          // 关闭触发的 abort（'已取消'）不是上传错误：不标错、不续泵
          if (closedRef.current) break
          const apiErr = err instanceof ApiError ? err : new ApiError(0, String(err))
          patch(row.id, { phase: 'error', error: apiErr })
        }
      }
    } finally {
      pumping.current = false
    }
  }, [patch, repoKey, onUploaded])

  const addFiles = useCallback(
    (files: FileList | File[], targetDir: string, rename?: string) => {
      const additions: Row[] = Array.from(files).map((file) => ({
        id: nextId.current++,
        file,
        fileName: rename ?? file.name,
        targetDir,
        phase: 'hashing',
        localSha: null,
        serverSha: '',
        loaded: 0,
        total: file.size,
        speed: 0,
        error: null,
      }))
      rowsRef.current = [...rowsRef.current, ...additions]
      setRows(rowsRef.current)
      window.setTimeout(() => void pump(), 0)
    },
    [pump],
  )

  const onDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    setDragOver(false)
    if (mode === 'generic' && e.dataTransfer?.files?.length) {
      addFiles(e.dataTransfer.files, normalizeDir(target))
    }
  }

  const maven = useMemo(() => mavenTarget(gav), [gav])
  const allSettled = rows.length > 0 && rows.every((r) => r.phase === 'done' || r.phase === 'error')
  const close = () => {
    // 先落闸再 abort：泵循环每轮开头检查 closedRef（B1）——abort 只能打断
    // 在飞的那一个 XHR，若不落闸，catch 会继续取下一行排队文件发新 PUT
    closedRef.current = true
    for (const xhr of xhrs.current) xhr.abort()
    xhrs.current.clear()
    onClose()
  }

  const retry = (row: Row) => {
    patch(row.id, { phase: 'queued', error: null, loaded: 0 })
    window.setTimeout(() => void pump(), 0)
  }

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.stopPropagation()}>
      <div className="modal upload-modal" role="dialog" aria-modal="true" aria-label="上传制品" data-testid="upload-dialog">
        <h2>
          上传到 <span className="mono" lang="en">{repoKey}/{mode === 'maven' && maven.dir ? `${maven.dir}/…` : normalizeDir(target) || '（仓库根）'}</span>
        </h2>
        <div className="modal-body">
          {mode === 'generic' ? (
            <div className="field">
              <label htmlFor="upload-target-input">
                目标路径（repo 相对目录，可修改）
              </label>
              <input
                id="upload-target-input"
                className="mono-input"
                data-testid="upload-target"
                value={target}
                placeholder="例如 acme/release/"
                onChange={(e) => setTarget(e.target.value)}
                spellCheck={false}
              />
              <div className="field-hint">尾斜杠可省；空 = 仓库根。同路径重传 = 覆盖（对旧文件需 delete 权限）。</div>
            </div>
          ) : (
            <div className="maven-form">
              <div className="gav-grid">
                {(
                  [
                    ['groupId', 'groupId', 'com.acme'],
                    ['artifactId', 'artifactId', 'demo-app'],
                    ['version', 'version', '1.0.0'],
                    ['classifier', 'classifier（可选）', 'sources'],
                    ['packaging', 'packaging', 'jar'],
                  ] as const
                ).map(([field, label, ph]) => (
                  <div className="field" key={field}>
                    <label htmlFor={`gav-${field}`}>{label}</label>
                    <input
                      id={`gav-${field}`}
                      className="mono-input"
                      data-testid={`upload-gav-${field}`}
                      value={gav[field]}
                      placeholder={ph}
                      onChange={(e) => setGav((cur) => ({ ...cur, [field]: e.target.value }))}
                      spellCheck={false}
                    />
                  </div>
                ))}
              </div>
              <div className="field">
                <label>生成的 layout 路径（前端已按 maven-2-default 预检）</label>
                <div className="mono preview-path" data-testid="upload-maven-preview" lang="en">
                  {maven.error ? (
                    <span className="upload-invalid">✗ {maven.error}</span>
                  ) : gav.groupId === '' ? (
                    <span className="text-2">填写坐标后生成</span>
                  ) : (
                    <span>
                      {maven.dir}/<b>{maven.file}</b>
                    </span>
                  )}
                </div>
                {maven.error && <div className="field-error">前端预检未通过——修正后再选择文件（FR-24-AC5：零写请求）。</div>}
              </div>
            </div>
          )}

          <div
            className={`drop-zone${dragOver ? ' over' : ''}`}
            data-testid="upload-drop"
            role="button"
            tabIndex={0}
            aria-label="拖拽文件到此处，或按回车选择文件"
            onDragOver={(e) => {
              e.preventDefault()
              setDragOver(true)
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={onDrop}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                ;(mode === 'maven' ? mavenInput : fileInput).current?.click()
              }
            }}
            onClick={() => (mode === 'maven' ? mavenInput : fileInput).current?.click()}
          >
            <div aria-hidden="true">⬇</div>
            <div>
              {mode === 'generic'
                ? '拖拽文件到此处（支持多选），或点击选择'
                : '选择构件文件（文件名将按 GAV 坐标重命名为 layout 名）'}
            </div>
          </div>
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            data-testid="upload-file-input"
            onChange={(e) => {
              if (e.target.files?.length) addFiles(e.target.files, normalizeDir(target))
              e.target.value = ''
            }}
          />
          <input
            ref={mavenInput}
            type="file"
            hidden
            data-testid="upload-maven-input"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f && !maven.error && gav.groupId !== '') addFiles([f], maven.dir, maven.file)
              e.target.value = ''
            }}
          />

          {rows.length > 0 && (
            <table className="table upload-table">
              <thead>
                <tr>
                  <th style={{ width: 28 }}>#</th>
                  <th>文件</th>
                  <th>大小</th>
                  <th>本地 sha256 / 进度</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr key={r.id} data-testid={`upload-file-${i}`}>
                    <td>{i + 1}</td>
                    <td>
                      <div className="mono" lang="en">{r.fileName}</div>
                      {r.targetDir && (
                        <div className="text-2 mono" style={{ fontSize: 'var(--bf-fs-aux)' }} lang="en">
                          {r.targetDir}/
                        </div>
                      )}
                    </td>
                    <td className="mono">{formatBytes(r.file.size)}</td>
                    <td style={{ minWidth: 200 }}>
                      {r.phase === 'hashing' ? (
                        <span className="text-2">正在计算本地 sha256…</span>
                      ) : r.localSha ? (
                        <span className="mono" lang="en" title={r.localSha}>
                          {short(r.localSha)}
                        </span>
                      ) : (
                        <span className="text-2">—</span>
                      )}
                      {r.phase === 'uploading' && (
                        <div className="upload-progress">
                          <div className="bar">
                            <div
                              className="fill"
                              style={{ width: `${r.total > 0 ? Math.min(100, (r.loaded / r.total) * 100) : 0}%` }}
                            />
                          </div>
                          <span className="text-2">
                            {Math.round(r.total > 0 ? (r.loaded / r.total) * 100 : 0)}% · {formatBytes(r.speed)}/s
                          </span>
                        </div>
                      )}
                    </td>
                    <td>
                      {r.phase === 'done' ? (
                        <span>
                          <span className="badge success">上传完成 201</span>{' '}
                          {r.localSha && r.serverSha && (
                            <span
                              className={`badge ${r.localSha === r.serverSha ? 'success' : 'danger'}`}
                              data-testid={`upload-verify-${i}`}
                            >
                              {r.localSha === r.serverSha ? '✓ checksum 一致' : '✗ 不一致'}
                            </span>
                          )}
                        </span>
                      ) : r.phase === 'error' && r.error ? (
                        <UploadError err={r.error} admin={admin} />
                      ) : (
                        <span className="text-2">
                          {r.phase === 'hashing' ? '哈希中' : r.phase === 'queued' ? '排队中' : '上传中'}
                        </span>
                      )}
                    </td>
                    <td>
                      {r.phase === 'error' && (
                        <button type="button" className="btn" data-testid={`upload-retry-${i}`} onClick={() => retry(r)}>
                          重试
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          <label className="check-row">
            <input
              type="checkbox"
              checked={sendChecksum}
              onChange={(e) => setSendChecksum(e.target.checked)}
            />
            计算并附带 X-Checksum-Sha256（推荐：服务端校验，不一致 409）
          </label>
        </div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={close}>
            关闭
          </button>
          <button type="button" className="btn primary" disabled={!allSettled} onClick={close}>
            完成
          </button>
        </div>
      </div>
    </div>
  )
}

/** E-11 错误语义原样呈现（§4.7：409 双值 / 403 权限指引 / 413 quota） */
function UploadError({ err, admin }: { err: ApiError; admin: boolean }) {
  return (
    <div className="upload-error">
      <span className={`badge ${err.status === 403 || err.status === 413 || err.status === 409 ? 'danger' : 'neutral'}`}>
        HTTP {err.status}
      </span>
      <div className="raw mono" lang="en">
        {err.message}
      </div>
      {err.status === 403 && (
        <div className="hint">
          当前会话对该路径没有所需权限（写入需 write；覆盖已有文件还需对旧文件的 delete）。
          {admin && (
            <>
              {' '}
              <Link to="/security/permissions" target="_blank">
                权限 target 配置 ↗
              </Link>
            </>
          )}
        </div>
      )}
      {err.status === 413 && <div className="hint">仓库配额已满——服务端已原子拒绝（used/quota 见 message），未落任何残留。</div>}
      {err.status === 409 && (
        <div className="hint">
          409：路径被仓库 include/exclude pattern 拒绝（message 含两侧 pattern），或声明的 checksum 与实际内容不一致（message 含 received/actual 双值）。
        </div>
      )}
      {err.status === 404 && <div className="hint">404：目标仓库不存在，或路径写法被服务端拒绝。</div>}
    </div>
  )
}

function normalizeDir(target: string): string {
  return target.trim().replace(/^\/+/, '').replace(/\/+$/g, '')
}

function short(hex: string): string {
  return hex.length > 16 ? `${hex.slice(0, 12)}…${hex.slice(-4)}` : hex
}
