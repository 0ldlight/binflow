import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { DragEvent as ReactDragEvent, KeyboardEvent as ReactKeyboardEvent } from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'

import { Badge } from '@/components/ui/badge'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { useAuth } from '../app/AuthContext'
import { ApiError, errText, getRepositories } from '../lib/api'
import { formatBytes } from '../lib/format'
import { getRepoDetail } from '../lib/repos'
import type { PackageType, RepoDetail } from '../lib/repos'
import { useAsync } from '../lib/useAsync'
import { blobSha256 } from '../pages/artifacts/sha256'
import { putArtifact } from '../pages/artifacts/lib'
import type { UploadProgress } from '../pages/artifacts/lib'
import { mavenTarget } from '../lib/maven'
import type { GavForm } from '../lib/maven'
import { Link } from 'react-router-dom'

import './dialogs.css'
import { tr } from '../i18n'

const t = tr('console')

// Deploy 对话框（T-242，console-m8 §4.2 / reverse §4.2——Artifactory Deploy
// 操作流的自有皮肤对齐面）。字段序：目标仓库（下拉）→ 包类型（只读回显）→
// 部署模式（单个/多个）→ 拖拽区（`拖拽文件到此处` / `选择文件`）→ 目标路径
// （mono 可编辑 + Copy）→ `部署`。
//
// BinFlow 既有增强全保留（§4.2）：浏览器端流式 sha256 + X-Checksum-Sha256、
// 行级进度/速度、409 双值 / 403 权限指引 / 413 quota 错误语义、maven 仓按
// GAV 表单生成 layout 路径 + 前端预检（mavenTarget 归位 lib/maven，T-244
// 树页双 Deploy 入口收敛后本对话框是浏览器上传唯一形态——UploadDialog
// 随 tree-upload 退役删除，upload-* 锚族显式退役见 console-ux §10.5）。
// docker/npm/pypi 不出现上传入口（以接入命令块替代，P6 语义）。
//
// T-231 债券承接（console-m8 §8）：目标路径输入框显示原值（可编辑）；每行
// 上传请求的**编码值**只读 mono 回显（% / 空格 / 中文文件名的
// percent-encode 形态可见，e2e 断言 50%25off 一类）。请求本体走
// putArtifact 的逐段 encodeURIComponent。
//
// 队列模型（UploadDialog 同款纪律）：行一次只跑一个（hash → PUT 串行）；
// 「部署」按钮启泵（对齐 Artifactory 的显式 Deploy 提交步）；关闭 = 落闸 +
// abort 在飞 XHR（排队文件不再上传）。
//
// FE-P4 MUI 清场：MUI Dialog/Button/Chip/Table/LinearProgress → Radix
// Dialog + shadcn/Tailwind 承载。锚族与语义 DOM 原样（deploy-dialog /
// deploy-repo / deploy-target / deploy-gav-* / deploy-drop / deploy-rows /
// deploy-row-<name> / deploy-echo-<name> / deploy-verify-<name> /
// deploy-submit / deploy-close）；焦点圈进/Tab 循环/Esc/滚动锁定交 Radix
// FocusScope（Esc 与 backdrop 点击 → close——落闸 + abort 语义由
// onOpenChange 承载，任意焦点位可达）。行内进度条 = 原生 role=progressbar
// div（aria 语义与 MUI LinearProgress 同构）。

interface MiniRepo {
  key: string
  packageType: string
  type: string
}

type Phase = 'queued' | 'hashing' | 'uploading' | 'done' | 'error'

interface Row {
  id: number
  file: File
  /** 展示名 = 上传目标文件名（maven 表单会按 GAV 改名） */
  fileName: string
  /** 目标目录（repo 相对，无尾斜杠） */
  targetDir: string
  phase: Phase
  localSha: string | null
  serverSha: string
  loaded: number
  total: number
  error: ApiError | null
}

const EMPTY_GAV: GavForm = { groupId: '', artifactId: '', version: '', classifier: '', packaging: 'jar' }

/** 与 pages/artifacts/lib contentURL 同口径的逐段编码（展示用回显） */
function encodedPath(dir: string, fileName: string): string {
  return [dir, fileName]
    .join('/')
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
}

function normalizeDir(target: string): string {
  return target.trim().replace(/^\/+/, '').replace(/\/+$/g, '')
}

export interface DeployDialogProps {
  /** 入口仓库上下文；在候选集（local generic/maven）内则预选 */
  preselectedRepo?: string
  /** 入口目录上下文（树页空目录 CTA 等）：generic 模式目标路径初值 */
  preselectedDir?: string
  onClose: () => void
  /** 任一行成功后通知父级刷新 */
  onUploaded?: () => void
}

export default function DeployDialog({ preselectedRepo, preselectedDir, onClose, onUploaded }: DeployDialogProps) {
  const { session } = useAuth()
  const admin = !!session?.admin

  // 候选仓库：local × {generic, maven}（docker/npm/pypi 无浏览器上传——§4.2）
  const list = useAsync(() => getRepositories(), [])
  const needFallback = list.status === 'forbidden' && !!preselectedRepo
  const fallback = useAsync(
    () => (needFallback ? getRepoDetail(preselectedRepo) : Promise.resolve(null)),
    [needFallback, preselectedRepo],
  )
  const candidates: MiniRepo[] = useMemo(() => {
    const okType = (rclass: string, packageType: string) =>
      rclass === 'local' && (packageType === 'generic' || packageType === 'maven')
    if (list.status === 'ok') return (list.data ?? []).filter((r) => okType(r.type, r.packageType))
    if (needFallback && fallback.status === 'ok' && fallback.data) {
      const d = fallback.data as RepoDetail
      return okType(d.rclass, d.packageType) ? [{ key: d.key, packageType: d.packageType, type: d.rclass }] : []
    }
    // 双 403 降级（W12d 语义保全，T-244 收敛时补臂）：普通 user 仓清单与
    // 单仓详情都不可得（无 manage）时，入口仓按 Generic 直传降级——与
    // 树页 uploadable 的「元数据 403 → generic 语义」同款；写权限由内容面
    // 按路径 ACL 终裁（403 行内指引），协议形态由服务端 layout 终裁。
    if (needFallback && fallback.status === 'forbidden' && preselectedRepo) {
      return [{ key: preselectedRepo, packageType: 'generic', type: 'local' }]
    }
    return []
  }, [list.status, list.data, needFallback, fallback.status, fallback.data, preselectedRepo])
  /** 候选是 403 降级合成（包类型为假设）——界面须如实标注 */
  const degradedCandidate =
    list.status === 'forbidden' && needFallback && fallback.status === 'forbidden'

  const [repoKey, setRepoKey] = useState('')
  const initRef = useRef(false)
  useEffect(() => {
    if (list.status === 'loading' || (needFallback && fallback.status === 'loading')) return
    if (initRef.current) return
    initRef.current = true
    const pre = preselectedRepo ? candidates.find((r) => r.key === preselectedRepo) : undefined
    setRepoKey((pre ?? candidates[0])?.key ?? '')
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 仅初始化一次
  }, [list.status, fallback.status, candidates])

  const mode = useMemo<'generic' | 'maven'>(() => {
    const pt = candidates.find((r) => r.key === repoKey)?.packageType
    return pt === 'maven' ? 'maven' : 'generic'
  }, [candidates, repoKey])
  const packageType: PackageType = mode === 'maven' ? 'maven' : 'generic'

  // ---- 表单态 ----
  const [deployMode, setDeployMode] = useState<'single' | 'multi'>('multi')
  const [target, setTarget] = useState(preselectedDir ?? '')
  const [gav, setGav] = useState<GavForm>(EMPTY_GAV)
  const [sendChecksum, setSendChecksum] = useState(true)
  const [dragOver, setDragOver] = useState(false)

  const maven = useMemo(() => mavenTarget(gav), [gav])
  const mavenReady = mode === 'maven' && !maven.error && gav.groupId !== ''

  // ---- 队列 ----
  const [rows, setRows] = useState<Row[]>([])
  const rowsRef = useRef<Row[]>([])
  const nextId = useRef(1)
  const xhrs = useRef(new Set<XMLHttpRequest>())
  const pumping = useRef(false)
  const closedRef = useRef(false)
  const startedRef = useRef(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const sendFlag = useRef(sendChecksum)
  sendFlag.current = sendChecksum

  const patch = useCallback((id: number, p: Partial<Row>) => {
    rowsRef.current = rowsRef.current.map((r) => (r.id === id ? { ...r, ...p } : r))
    setRows(rowsRef.current)
  }, [])

  const pump = useCallback(async () => {
    if (pumping.current) return
    pumping.current = true
    try {
      for (;;) {
        if (closedRef.current) break
        const row = rowsRef.current.find((r) => r.phase === 'queued' || r.phase === 'hashing')
        if (!row) break
        let sha = row.localSha
        if (!sha) {
          // 未算过哈希的行先算（queued 入队 → hashing → 回 queued → uploading）
          patch(row.id, { phase: 'hashing' })
          try {
            sha = await blobSha256(row.file)
            patch(row.id, { localSha: sha, phase: 'queued' })
          } catch {
            patch(row.id, { phase: 'error', error: new ApiError(0, t('本地 sha256 计算失败')) })
            continue
          }
          if (closedRef.current) break
        }
        patch(row.id, { phase: 'uploading', loaded: 0, total: row.file.size })
        try {
          const out = await putArtifact({
            repoKey,
            path: row.targetDir === '' ? row.fileName : `${row.targetDir}/${row.fileName}`,
            file: row.file,
            sha256: sendFlag.current && sha ? sha : undefined,
            onProgress: (p: UploadProgress) => patch(row.id, { loaded: p.loaded, total: p.total }),
            registerXhr: (xhr) => xhrs.current.add(xhr),
          })
          if (!closedRef.current) {
            patch(row.id, { phase: 'done', serverSha: out.serverSha256, loaded: row.file.size })
            onUploaded?.()
          }
        } catch (err) {
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
    (files: FileList | File[]) => {
      if (mode === 'maven' && !mavenReady) return // FR-24-AC5：GAV 预检未过零写请求
      let picked = Array.from(files)
      if (mode === 'maven') picked = picked.slice(0, 1) // maven：单构件按 GAV 改名
      if (deployMode === 'single') {
        // 单个部署：新选文件替换队列中未开始的行
        rowsRef.current = rowsRef.current.filter((r) => r.phase !== 'queued' && r.phase !== 'hashing')
      }
      const dir = mode === 'maven' ? maven.dir : normalizeDir(target)
      const rename = mode === 'maven' ? maven.file : undefined
      const additions: Row[] = picked.map((file) => ({
        id: nextId.current++,
        file,
        fileName: rename ?? file.name,
        targetDir: dir,
        // 显式「部署」提交步（§4.2）：入队即 queued，哈希随泵启动
        phase: 'queued',
        localSha: null,
        serverSha: '',
        loaded: 0,
        total: file.size,
        error: null,
      }))
      rowsRef.current = [...rowsRef.current, ...additions]
      setRows(rowsRef.current)
      if (startedRef.current) window.setTimeout(() => void pump(), 0)
    },
    [deployMode, mode, maven.dir, maven.file, mavenReady, target, pump],
  )

  const onDrop = (e: ReactDragEvent<HTMLDivElement>) => {
    e.preventDefault()
    setDragOver(false)
    if (e.dataTransfer?.files?.length) addFiles(e.dataTransfer.files)
  }

  const startDeploy = () => {
    startedRef.current = true
    void pump()
  }

  const retry = (row: Row) => {
    patch(row.id, { phase: 'queued', error: null, loaded: 0 })
    window.setTimeout(() => void pump(), 0)
  }

  const close = () => {
    // 落闸再 abort（UploadDialog B1 同款纪律）：泵每轮检查闸，abort 只打断
    // 在飞的那一个 XHR；排队文件不再发新 PUT
    closedRef.current = true
    for (const xhr of xhrs.current) xhr.abort()
    xhrs.current.clear()
    onClose()
  }

  // 回焦锚：挂载时捕获场外焦点元素，卸载时回焦（quick-set-me-up 等启动钮
  // 的回焦家族契约——ui/dialog 的 DialogContent 同款：Radix FocusScope 的
  // unmount 回焦在 React 19.2 下实测不发火，此处显式承载；微任务让位：
  // 若 Radix 自身回焦恢复，不抢已就位的焦点）
  const returnFocusRef = useRef<HTMLElement | null>(null)
  useEffect(() => {
    returnFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    return () => {
      queueMicrotask(() => {
        if (document.activeElement === document.body || document.activeElement === null) {
          const el = returnFocusRef.current
          if (el && el.isConnected) el.focus({ preventScroll: true })
        }
      })
    }
  }, [])

  const allSettled = rows.length > 0 && rows.every((r) => r.phase === 'done' || r.phase === 'error')
  const inFlight = rows.some((r) => r.phase === 'uploading' || r.phase === 'hashing')
  const canDeploy =
    rows.length > 0 && !inFlight && (mode === 'generic' || mavenReady) && !allSettled

  const dropHint =
    mode === 'generic'
      ? t('拖拽文件到此处，或点击选择（部署模式决定单文件替换还是多文件追加）')
      : t('选择构件文件（文件名按 GAV 坐标重命名为 layout 名）')

  return (
    <DialogPrimitive.Root open onOpenChange={(open) => { if (!open) close() }}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay data-slot="dialog-overlay" className="fixed inset-0 z-[90] bg-scrim" />
        <DialogPrimitive.Content
          data-testid="deploy-dialog"
          aria-labelledby="deploy-dialog-title"
          className="fixed top-1/2 left-1/2 z-[90] flex max-h-[calc(100vh-96px)] w-[min(720px,calc(100vw-48px))] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-surface-1 shadow-modal"
        >
          <DialogPrimitive.Title id="deploy-dialog-title" className="text-h3 px-4 pt-4 font-semibold">
            {t('部署 Deploy')}
          </DialogPrimitive.Title>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
            {list.status === 'loading' || (needFallback && fallback.status === 'loading') ? (
              <StateSkeleton lines={3} />
            ) : list.status === 'error' && list.error ? (
              <ErrorCard error={list.error} onRetry={list.reload} />
            ) : candidates.length === 0 ? (
              <EmptyState
                message={t('没有可经浏览器上传的仓库')}
                hint={t('浏览器上传面向 local 的 Generic / Maven 仓；docker / npm / pypi 协议请用对应客户端发布（仓库详情页有接入命令）。')}
                action={
                  admin ? (
                    <ButtonAsChild>
                      <Link to="/admin/repositories/new">{t('创建 Generic 仓库')}</Link>
                    </ButtonAsChild>
                  ) : undefined
                }
              />
            ) : (
              <>
                <div className="deploy-field-grid">
                  <div className="field">
                    <label htmlFor="deploy-repo">{t('目标仓库')}</label>
                    <select
                      id="deploy-repo"
                      data-testid="deploy-repo"
                      value={repoKey}
                      onChange={(e) => setRepoKey(e.target.value)}
                    >
                      {candidates.map((r) => (
                        <option key={r.key} value={r.key}>
                          {r.key}
                        </option>
                      ))}
                    </select>
                    {degradedCandidate && (
                      <div className="field-hint">{t('仓库元数据为管理员视图（HTTP 403）——按 Generic 语义直传；实际协议与 写权限由服务端终裁（被拒原因会在此原样呈现）。')}</div>
                    )}
                  </div>
                  <div className="field">
                    <label>{t('包类型（只读）')}</label>
                    <div>
                      <Badge variant="outline">{packageType === 'maven' ? 'Maven' : 'Generic'}</Badge>{' '}
                      <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{t('local 仓 · PUT 直传')}</span>
                    </div>
                  </div>
                  <div className="field">
                    <label>{t('部署模式')}</label>
                    <div role="radiogroup" aria-label={t('部署模式')} style={{ display: 'flex', gap: 12 }}>
                      <label className="check-row">
                        <input
                          type="radio"
                          name="deploy-mode"
                          value="single"
                          checked={deployMode === 'single'}
                          onChange={() => setDeployMode('single')}
                        />{t('单个部署')}</label>
                      <label className="check-row">
                        <input
                          type="radio"
                          name="deploy-mode"
                          value="multi"
                          checked={deployMode === 'multi'}
                          onChange={() => setDeployMode('multi')}
                        />{t('多个部署')}</label>
                    </div>
                  </div>
                </div>

                {mode === 'generic' ? (
                  <div className="field" style={{ marginTop: 12 }}>
                    <label htmlFor="deploy-target">{t('目标路径（repo 相对目录，可修改）')}</label>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                      <input
                        id="deploy-target"
                        className="mono-input w-full"
                        data-testid="deploy-target"
                        value={target}
                        placeholder={t('例如 acme/release/（空 = 仓库根）')}
                        onChange={(e) => setTarget(e.target.value)}
                        spellCheck={false}
                      />
                      <CopyButton value={normalizeDir(target)} label={t('目标路径')} />
                    </div>
                    <div className="field-hint deploy-echo" lang="en">{t('请求编码回显：')}{repoKey}/{encodedPath(normalizeDir(target), rows[0]?.fileName ?? t('<文件名>'))}
                    </div>
                  </div>
                ) : (
                  <div style={{ marginTop: 12 }}>
                    <div className="deploy-field-grid">
                      {(
                        [
                          ['groupId', 'groupId', 'com.acme'],
                          ['artifactId', 'artifactId', 'demo-app'],
                          ['version', 'version', '1.0.0'],
                          ['classifier', t('classifier（可选）'), 'sources'],
                          ['packaging', 'packaging', 'jar'],
                        ] as const
                      ).map(([field, label, ph]) => (
                        <div className="field" key={field}>
                          <label htmlFor={`deploy-gav-${field}`}>{label}</label>
                          <input
                            id={`deploy-gav-${field}`}
                            className="mono-input w-full"
                            data-testid={`deploy-gav-${field}`}
                            value={gav[field]}
                            placeholder={ph}
                            onChange={(e) => setGav((cur) => ({ ...cur, [field]: e.target.value }))}
                            spellCheck={false}
                          />
                        </div>
                      ))}
                    </div>
                    <div className="field-hint deploy-echo" data-testid="deploy-maven-preview" lang="en">
                      {maven.error ? `✗ ${maven.error}` : gav.groupId === '' ? t('填写坐标后生成 layout 路径') : `${maven.dir}/${maven.file}`}
                    </div>
                  </div>
                )}

                <div
                  className={`deploy-drop${dragOver ? ' over' : ''}`}
                  data-testid="deploy-drop"
                  role="button"
                  tabIndex={0}
                  aria-label={t('拖拽文件到此处，或按回车选择文件')}
                  onDragOver={(e) => {
                    e.preventDefault()
                    setDragOver(true)
                  }}
                  onDragLeave={() => setDragOver(false)}
                  onDrop={onDrop}
                  onKeyDown={(e: ReactKeyboardEvent<HTMLDivElement>) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      fileInput.current?.click()
                    }
                  }}
                  onClick={() => fileInput.current?.click()}
                >
                  <div aria-hidden="true">⬇</div>
                  <div>{dropHint}</div>
                </div>
                <input
                  ref={fileInput}
                  type="file"
                  multiple={mode === 'generic' && deployMode === 'multi'}
                  hidden
                  data-testid="deploy-file-input"
                  onChange={(e) => {
                    if (e.target.files?.length) addFiles(e.target.files)
                    e.target.value = ''
                  }}
                />

                {rows.length > 0 && (
                  <table data-testid="deploy-rows" className="w-full border-collapse text-dense" style={{ marginTop: 6 }}>
                    <thead>
                      <tr className="border-b border-border text-left text-aux text-muted-foreground">
                        <th className="w-6 px-2 py-1.5 font-medium">#</th>
                        <th className="px-2 py-1.5 font-medium">{t('文件（目标路径 / 编码回显）')}</th>
                        <th className="px-2 py-1.5 font-medium">{t('大小')}</th>
                        <th className="px-2 py-1.5 font-medium">{t('sha256 / 进度')}</th>
                        <th className="px-2 py-1.5 font-medium">{t('状态')}</th>
                        <th className="px-2 py-1.5 font-medium">{t('操作')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {rows.map((r, i) => (
                        <tr key={r.id} data-testid={`deploy-row-${r.fileName}`} className="border-b border-border/60 align-top">
                          <td className="px-2 py-1.5">{i + 1}</td>
                          <td className="px-2 py-1.5">
                            <div className="mono" lang="en">
                              {r.fileName}
                            </div>
                            <div className="deploy-echo" data-testid={`deploy-echo-${r.fileName}`} lang="en">
                              {repoKey}/{encodedPath(r.targetDir, r.fileName)}
                            </div>
                          </td>
                          <td className="px-2 py-1.5 font-mono">{formatBytes(r.file.size)}</td>
                          <td className="px-2 py-1.5 min-w-[180px]">
                            {r.phase === 'hashing' ? (
                              <span className="text-2">{t('正在计算本地 sha256…')}</span>
                            ) : r.localSha ? (
                              <span className="mono" lang="en" title={r.localSha}>
                                {r.localSha.slice(0, 12)}…
                              </span>
                            ) : (
                              <span className="text-2">—</span>
                            )}
                            {r.phase === 'uploading' && (
                              <div className="flex items-center gap-1.5">
                                <div
                                  role="progressbar"
                                  aria-label={t('上传进度 {v1}', { v1: r.fileName })}
                                  aria-valuenow={Math.round(r.total > 0 ? Math.min(100, (r.loaded / r.total) * 100) : 0)}
                                  aria-valuemin={0}
                                  aria-valuemax={100}
                                  className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-sm bg-surface-2"
                                >
                                  <div
                                    className="h-full bg-primary transition-[width]"
                                    style={{ width: `${r.total > 0 ? Math.min(100, (r.loaded / r.total) * 100) : 0}%` }}
                                  />
                                </div>
                                <span className="text-aux text-2">
                                  {Math.round(r.total > 0 ? (r.loaded / r.total) * 100 : 0)}%
                                </span>
                              </div>
                            )}
                          </td>
                          <td className="px-2 py-1.5">
                            {r.phase === 'done' ? (
                              <span className="flex flex-wrap items-center gap-1">
                                <Badge variant="success">{t('上传完成 201')}</Badge>
                                {r.localSha && r.serverSha && (
                                  <Badge
                                    variant={r.localSha === r.serverSha ? 'success' : 'destructive'}
                                    data-testid={`deploy-verify-${r.fileName}`}
                                  >
                                    {r.localSha === r.serverSha ? t('✓ checksum 一致') : t('✗ 不一致')}
                                  </Badge>
                                )}
                              </span>
                            ) : r.phase === 'error' && r.error ? (
                              <DeployError err={r.error} admin={admin} />
                            ) : (
                              <span className="text-2">
                                {r.phase === 'hashing' ? t('哈希中') : r.phase === 'queued' ? t('待部署') : t('上传中')}
                              </span>
                            )}
                          </td>
                          <td className="px-2 py-1.5">
                            {r.phase === 'error' && (
                              <Button variant="outline" size="sm" className="min-w-0" onClick={() => retry(r)}>{t('重试')}</Button>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}

                <label className="check-row" style={{ marginTop: 8 }}>
                  <input
                    type="checkbox"
                    checked={sendChecksum}
                    onChange={(e) => setSendChecksum(e.target.checked)}
                  />{t('计算并附带 X-Checksum-Sha256（推荐：服务端校验，不一致 409）')}</label>
              </>
            )}
          </div>
          <div className="flex justify-end gap-2 border-t border-border px-4 py-3">
            <Button variant="outline" data-testid="deploy-close" onClick={close}>{t('关闭')}</Button>
            <Button
              data-testid="deploy-submit"
              disabled={!canDeploy}
              onClick={startDeploy}
            >{t('部署')}</Button>
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}

/** E-11 错误语义原样呈现（409 双值 / 403 权限指引 / 413 quota——UploadDialog 同款） */
function DeployError({ err, admin }: { err: ApiError; admin: boolean }) {
  return (
    <div className="deploy-error">
      <span>
        <Badge variant={err.status === 403 || err.status === 413 || err.status === 409 ? 'destructive' : 'outline'}>
          {`HTTP ${err.status}`}
        </Badge>
      </span>
      <span className="raw" lang="en">
        {errText(err)}
      </span>
      {err.status === 403 && (
        <span className="field-hint">{t('当前会话对该路径没有所需权限（写入需 write；覆盖已有文件还需对旧文件的 delete）。')}{admin && t(' 可在权限 target 里为该路径加 write 动作。')}
        </span>
      )}
      {err.status === 413 && <span className="field-hint">{t('仓库配额已满——服务端已原子拒绝，未落任何残留。')}</span>}
      {err.status === 409 && (
        <span className="field-hint">{t('409：路径被 include/exclude pattern 拒绝，或声明的 checksum 与实际内容不一致（message 含 received/actual 双值）。')}</span>
      )}
    </div>
  )
}
