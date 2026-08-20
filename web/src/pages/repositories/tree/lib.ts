// 制品树域 API（T-100）。两个平面：
//
// - 元数据面走 /binflow/api/storage/**（apiJSON 信封，401 全局监听生效）：
//   目录列举（E-09 FolderInfo children + ?list&depth=1 的文件元数据合并）、
//   节点详情（FileInfo 全字段）、?permissions admin 视图（SE-08）。
// - 内容面走 /binflow/{repo}/{path}（cookie 同凭据，ux R10）：上传 PUT
//   （E-11：201/409/403/413）、删除 DELETE（E-14：204/404）、下载 GET。
//   该面不在 api.ts 的 API_ROOT 下，错误解析本地复刻 errors[] 信封
//   （generic adapter 全族 E-01）。
//
// 契约注记（W12 消费实测，已登记工作日志「契约漂移」）：
// - FolderInfo children 只有 uri('/name')+folder 两字段——size/mtime 不在
//   其中；?list&depth=1 给文件的 size/lastModified/sha1/sha2（根目录被
//   400 拒绝，见 handleStorageList），非根目录两请求合并。
// - **隐式目录无 folder 行**：putNode 只落文件节点，不落父目录行——
//   GET /api/storage/{repo}/{dir} 对「制品路径推断出的目录」404（只有
//   E-15 尾斜杠 PUT 建过的目录有行）。树浏览对这类目录回退搜索面前缀
//   重构（searchListing；folder 行不进搜索索引，空 mkdir 目录走主路径）。
// - 目录删除携带尾斜杠（service Delete 的 isFolderNode 分支：无斜杠走
//   文件臂 404）；建目录是尾斜杠 PUT（E-15）；folder 行的存储拼写本身
//   带尾斜杠（'acme/'）。

import { ApiError, apiJSON } from '../../../lib/api'

import { streamSha256 } from './sha256'

const CONTENT_ROOT = '/binflow'

/** E-09 FileInfo/FolderInfo 的前端形态（size 是字符串——wire 契约） */
export interface ItemInfo {
  uri: string
  downloadUri?: string
  repo: string
  path: string
  created: string
  createdBy: string
  lastModified?: string
  modifiedBy?: string
  lastUpdated?: string
  size: string
  mimeType?: string
  checksums?: { sha1?: string; md5?: string; sha256?: string }
  originalChecksums?: { sha1?: string; md5?: string; sha256?: string }
  children?: { uri: string; folder: boolean }[]
}

/** 右表/左树的一行：folder 无 size/mtime（合并来源是文件清单） */
export interface ChildNode {
  name: string
  /** repo 相对全路径（folder 不带尾斜杠） */
  path: string
  folder: boolean
  size: number | null
  lastModified: string
  sha256: string
}

/** SE-08 ?permissions 视图（admin 门；403 由调用方隐藏该面） */
export interface PermissionsView {
  uri: string
  principals: { users: Record<string, string[]>; groups: Record<string, string[]> }
}

function storagePath(repoKey: string, rel: string): string {
  const enc = rel
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
  return `/storage/${encodeURIComponent(repoKey)}${enc ? `/${enc}` : ''}`
}

/** 内容面错误（E-01 errors[] 或纯文本）→ ApiError */
async function contentError(res: Response): Promise<ApiError> {
  const text = await res.text().catch(() => '')
  let message = ''
  if (text) {
    try {
      const parsed = JSON.parse(text) as { errors?: { message?: string }[] }
      const first = parsed?.errors?.[0]?.message
      if (typeof first === 'string' && first) message = first
    } catch {
      // 非 JSON：内容面恒为信封，兜底用原文
    }
  }
  if (!message) message = text.trim()
  if (!message) message = `HTTP ${res.status}`
  return new ApiError(res.status, message, text)
}

// ---- 元数据面 ----

/** 子目录集（'' = 根） */
export function ancestorDirs(dir: string): string[] {
  const clean = dir.replace(/^\/+|\/+$/g, '')
  if (clean === '') return ['']
  const segs = clean.split('/')
  return segs.map((_, i) => segs.slice(0, i + 1).join('/'))
}

function toApiError(err: unknown): ApiError {
  return err instanceof ApiError ? err : new ApiError(0, String(err))
}

/**
 * 一个目录的直接 children：FolderInfo（结构）+ ?list&depth=1（文件元数据）
 * 合并；**隐式目录**（制品路径推断出、无 folder 行——putNode 不落父目录
 * 行，实测 GET /api/storage/{repo}/{dir} 404）回退到搜索面前缀重构。
 * 目录在前、同组按名排序（服务端 children 已按 uri 排序，合并后重排一次
 * 保持全序）。
 */
export async function listChildren(
  repoKey: string,
  dir: string,
  signal?: AbortSignal,
): Promise<ChildNode[]> {
  let folder: ItemInfo
  try {
    folder = await apiJSON<ItemInfo>(storagePath(repoKey, dir), { signal })
  } catch (err) {
    if (err instanceof ApiError && err.status === 404 && dir !== '') {
      // 隐式目录：无 folder 行——用搜索面（name 是路径子串）拉全后代再
      // 投影到第一段。folder 行不进搜索索引（nodeQuery 显式排除 '%/'），
      // 空的 mkdir 目录在此不可见（由 item-info 主路径覆盖）。
      return searchListing(repoKey, dir, signal)
    }
    throw err
  }
  const kids = folder.children ?? []
  let files: { uri: string; size: number; lastModified: string; sha2?: string; folder: boolean }[] = []
  if (dir !== '') {
    // 根目录 ?list 400（"Cannot list files of root."）；非根用 depth=1 补
    // 直接子文件的 size/mtime/sha。失败不致命：降级为无元数据列。
    try {
      const listed = await apiJSON<{ files: typeof files }>(`${storagePath(repoKey, dir)}?list&depth=1`, { signal })
      files = (listed.files ?? []).filter((f) => !f.folder)
    } catch {
      files = []
    }
  }
  const byName = new Map(files.map((f) => [f.uri.replace(/^\//, ''), f]))
  const nodes: ChildNode[] = kids.map((k) => {
    const name = k.uri.replace(/^\//, '')
    const meta = !k.folder ? byName.get(name) : undefined
    return {
      name,
      path: dir === '' ? name : `${dir}/${name}`,
      folder: k.folder,
      size: meta ? meta.size : null,
      lastModified: meta ? meta.lastModified : '',
      sha256: meta?.sha2 ?? '',
    }
  })
  sortChildren(nodes)
  return nodes
}

/** 隐式目录的搜索面重构：name=<dir>/ 的子串命中 + 前缀收窄 + 首段投影 */
async function searchListing(repoKey: string, dir: string, signal?: AbortSignal): Promise<ChildNode[]> {
  const params = new URLSearchParams({ name: `${dir}/`, repos: repoKey })
  const res = await apiJSON<{ results: ItemInfo[] }>(`/search/artifact?${params.toString()}`, { signal })
  const prefix = `${dir}/`
  const byName = new Map<string, ChildNode>()
  for (const hit of res.results ?? []) {
    const p = hit.path.replace(/^\//, '')
    if (!p.startsWith(prefix)) continue // 子串匹配会把「a<dir>/」一并带回
    const rel = p.slice(prefix.length)
    if (rel === '') continue
    const name = rel.split('/')[0]
    const isFolder = rel.includes('/')
    const existing = byName.get(name)
    if (existing?.folder) continue
    if (!isFolder && existing) continue
    byName.set(name, {
      name,
      path: `${dir}/${name}`,
      folder: isFolder,
      size: isFolder ? null : Number(hit.size) || 0,
      lastModified: hit.lastModified ?? '',
      sha256: isFolder ? '' : (hit.checksums?.sha256 ?? ''),
    })
  }
  const nodes = Array.from(byName.values())
  sortChildren(nodes)
  return nodes
}

function sortChildren(nodes: ChildNode[]): void {
  nodes.sort((a, b) =>
    a.folder === b.folder ? (a.name < b.name ? -1 : a.name > b.name ? 1 : 0) : a.folder ? -1 : 1,
  )
}

/** 节点详情（file 全字段 / folder 为 FolderInfo） */
export function getItem(repoKey: string, path: string, signal?: AbortSignal): Promise<ItemInfo> {
  return apiJSON<ItemInfo>(storagePath(repoKey, path), { signal })
}

/** ?permissions（admin）；非 admin 403 → 调用方按 §3.6.3 隐藏 */
export function getItemPermissions(
  repoKey: string,
  path: string,
  signal?: AbortSignal,
): Promise<PermissionsView> {
  return apiJSON<PermissionsView>(`${storagePath(repoKey, path)}?permissions`, { signal })
}

// ---- 内容面 ----

function contentURL(repoKey: string, path: string): string {
  // 尾斜杠是 folder 语义（E-15 mkdir / 目录删除的 isFolderNode 分支），
  // 不能在过滤空段时丢掉
  const trailing = path.endsWith('/')
  const enc = path
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
  return `${CONTENT_ROOT}/${encodeURIComponent(repoKey)}${enc ? `/${enc}` : ''}${trailing ? '/' : ''}`
}

export interface UploadOutcome {
  /** 201 响应头 X-Checksum-Sha256（服务端实测） */
  serverSha256: string
}

export interface UploadProgress {
  loaded: number
  total: number
}

/**
 * PUT 上传（E-11）：XHR 取上传进度（fetch 无上传进度事件）。携带
 * X-Checksum-Sha256 时服务端做客户端校验（不一致 409，message 含
 * received/actual 双值）。浏览器同源 PUT 自动带 Origin（CSRF 主防线）。
 */
export function putArtifact(opts: {
  repoKey: string
  path: string
  file: Blob
  sha256?: string
  onProgress?: (p: UploadProgress) => void
  registerXhr?: (xhr: XMLHttpRequest) => void
}): Promise<UploadOutcome> {
  const { repoKey, path, file, sha256, onProgress, registerXhr } = opts
  return new Promise<UploadOutcome>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    registerXhr?.(xhr)
    xhr.open('PUT', contentURL(repoKey, path))
    xhr.setRequestHeader('X-BinFlow-Console', '1')
    if (sha256) xhr.setRequestHeader('X-Checksum-Sha256', sha256)
    if (onProgress) {
      xhr.upload.onprogress = (e) => onProgress({ loaded: e.loaded, total: e.total })
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve({ serverSha256: (xhr.getResponseHeader('X-Checksum-Sha256') ?? '').toLowerCase() })
        return
      }
      // E-01 信封解析（与 contentError 同款，但拿的是 xhr.responseText）
      let message = ''
      try {
        const parsed = JSON.parse(xhr.responseText) as { errors?: { message?: string }[] }
        const first = parsed?.errors?.[0]?.message
        if (typeof first === 'string' && first) message = first
      } catch {
        // 非 JSON 兜底
      }
      if (!message) message = xhr.responseText.trim() || `HTTP ${xhr.status}`
      reject(new ApiError(xhr.status, message, xhr.responseText))
    }
    xhr.onerror = () => reject(new ApiError(0, '网络错误（上传中断）'))
    xhr.onabort = () => reject(new ApiError(0, '已取消'))
    xhr.send(file)
  })
}

/**
 * 删除（E-14）：204 无体；重复删除/不存在 → 404（幂等语义由调用方
 * 呈现）。**目录必须携带尾斜杠**（service Delete 的 isFolderNode 分支
 * ——实测无斜杠 404 "Could not locate artifact"，有斜杠递归删全子树）。
 */
export async function deleteNode(repoKey: string, path: string, folder: boolean): Promise<void> {
  const res = await fetch(contentURL(repoKey, folder ? `${path}/` : path), {
    method: 'DELETE',
    headers: { 'X-BinFlow-Console': '1' },
    credentials: 'same-origin',
  })
  if (res.status === 204) return
  throw await contentError(res)
}

/**
 * 建目录（E-15）：尾斜杠 PUT 空 body → folder 节点 201。
 *
 * 逐段材料化祖先：目录节点是显式的（putNode 不落隐式父目录行，契约
 * 漂移 1）——「隐式目录下新建的空目录」在搜索面重构的列表里不可见
 * （folder 行不进搜索索引）。因此 mkdir 把目标路径的每一段都 PUT 一遍
 * （已存在的 folder 行重放 = 幂等 redeploy，只刷新 mtime），保证新目录
 * 在任何父目录下列表可见。祖先段失败不致命（窄授权下 403 可忍），只有
 * 目标段失败才向上抛。
 */
export async function mkdir(repoKey: string, dirPath: string): Promise<void> {
  const segs = dirPath.replace(/\/+$/, '').split('/').filter((s) => s !== '')
  for (let i = 1; i <= segs.length; i++) {
    const target = `${segs.slice(0, i).join('/')}/`
    try {
      await mkdirOnce(repoKey, target)
    } catch (err) {
      if (i === segs.length) throw err
    }
  }
}

async function mkdirOnce(repoKey: string, dirPath: string): Promise<void> {
  const res = await fetch(contentURL(repoKey, dirPath), {
    method: 'PUT',
    headers: { 'X-BinFlow-Console': '1' },
    credentials: 'same-origin',
  })
  if (res.status >= 200 && res.status < 300) return
  throw await contentError(res)
}

export interface DownloadResult {
  blob: Blob
  sha256: string
}

/**
 * 下载 + 边收边算 sha256（对账数据源）。tee 出两条腿：一条进哈希、一条
 * 聚成 Blob 落盘——哈希内存上界 = 单块；Blob 腿是 save-as 的固有成本
 * （大文件走「直接下载」链接，不经此路径）。
 */
export async function downloadArtifact(repoKey: string, path: string): Promise<DownloadResult> {
  const res = await fetch(contentURL(repoKey, path), {
    headers: { 'X-BinFlow-Console': '1' },
    credentials: 'same-origin',
  })
  if (!res.ok) throw await contentError(res)
  if (!res.body) {
    const blob = await res.blob()
    return { blob, sha256: '' }
  }
  const [hashLeg, blobLeg] = res.body.tee()
  const sha256 = await streamSha256(hashLeg)
  const blob = await new Response(blobLeg).blob()
  return { blob, sha256 }
}

/** 触发浏览器落盘 */
export function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 10_000)
}

/** 目录/文件名段校验（与 adapter validateRelPath 同口径的前端预检） */
export function validateNameSegment(name: string): string | null {
  if (name === '') return '名称不能为空'
  if (name.includes('/')) return '名称不能包含 /'
  if (name.includes('\\')) return '名称不能包含反斜杠'
  if (name === '.' || name === '..') return '不能以 . 或 .. 命名'
  // eslint-disable-next-line no-control-regex -- 与服务端 isControlByte 同口径
  if (/[\x00-\x1f\x7f]/.test(name)) return '名称不能包含控制字符'
  return null
}

export { toApiError }
