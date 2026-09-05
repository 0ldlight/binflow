// 制品树域 API（T-100 / T-131；T-236 自 repositories/tree 迁址——跨仓树
// （/artifacts）消费，契约零改动）。两个平面：
//
// - 元数据面走 /binflow/api/storage/**（apiJSON 信封，401 全局监听生效）：
//   目录列举（E-09 FolderInfo children + ?list&depth=1 的文件元数据合并）、
//   节点详情（FileInfo 全字段）、?permissions admin 视图（SE-08）。
// - 内容面走 /binflow/{repo}/{path}（cookie 同凭据，ux R10）：上传 PUT
//   （E-11：201/409/403/413）、删除 DELETE（E-14：204/404）、下载 GET。
//   该面不在 api.ts 的 API_ROOT 下，错误解析本地复刻 errors[] 信封
//   （generic adapter 全族 E-01）。
//
// 契约注记（T-131 更新：ADR-0016 materializeAncestors 已就位）：
// - FolderInfo children 只有 uri('/name')+folder 两字段——size/mtime 不在
//   其中；?list&depth=1 给文件的 size/lastModified/sha1/sha2（根目录被
//   400 拒绝，见 handleStorageList），非根目录两请求合并。
// - **隐式目录已材料化**：putNode 前置 materializeAncestors（ADR-0016），
//   GET /api/storage/{repo}/{dir} 对任意段深度的隐式目录均返回 200
//   FolderInfo。FE 不再需要搜索面兜底——listChildren 直走主路径即可。
// - 目录删除携带尾斜杠（service Delete 的 isFolderNode 分支：无斜杠走
//   文件臂 404）；建目录是尾斜杠 PUT（E-15）；folder 行的存储拼写本身
//   带尾斜杠（'acme/'）。

import { ApiError, apiJSON } from '../../lib/api'

import { streamSha256 } from './sha256'
import { tr } from '../../i18n'

const t = tr('artifacts')

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
  /** 远端枚举层降级注记（T-461 消费 / T-448 §5-2 缝：repo.RemoteDegraded
   *  经 FolderInfo 体的投影——httpapi 渲染腿在途，缺席 = 无注记） */
  remoteDegraded?: string
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
  /** Docker tags for this manifest digest (T-134 G32a: via ?docker_tags enrichment) */
  tags?: string[]
  /** 远端派生行（M16 T-461 / FR-147）：listRemoteFolderItems on 的 remote
   *  仓（或其 virtual 成员）上游枚举出的 display-only 行——零落库、无
   *  digest（wire 判别 = ?list 元数据在场而 sha2 缺席：落库文件恒带 blob
   *  digest，见 internal/repo/browse.go browseDisplayNode 注）；点击触发
   *  回源 pull-through。根目录层无 ?list 合并（BE 400 拒绝 root list），
   *  无法判别——不标记（误标比漏标差）。 */
  remote?: boolean
}

/** listFolder 的双返回：children 行 + 远端枚举层的降级注记（M16 T-461） */
export interface FolderListing {
  nodes: ChildNode[]
  /** 远端层错误态注记（remote-browsing.md §4-1）：非空 = 上游不可达/
   *  静默期，nodes 是缓存行（不整树塌）。wire 面 = FolderInfo 体的可选
   *  字段 remoteDegraded——T-448 §5-2 预留缝（httpapi 渲染腿在途，
   *  字段缺席 = 无注记，零行为影响；BE 腿合入后自然点亮）。 */
  remoteDegraded: string
}

/** DockerTagsMap: digest hex → tag names (T-134 G32a: ?docker_tags response field) */
export type DockerTagsMap = Record<string, string[]>

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
 * 合并。**隐式目录已由 BE materializeAncestors 材料化**（ADR-0016），
 * listChildren 直走主路径，不再需要搜索面兜底。
 * 目录在前、同组按名排序（服务端 children 已按 uri 排序，合并后重排一次
 * 保持全序）。
 *
 * 当 isDockerRepo 为 true 且 dir 为 docker image 路径时，自动附加
 * ?docker_tags 查询参数以获取 digest→tag 映射（T-134 G32a）。
 */
export async function listChildren(
  repoKey: string,
  dir: string,
  signal?: AbortSignal,
  isDockerRepo?: boolean,
): Promise<ChildNode[]> {
  return (await listFolder(repoKey, dir, signal, isDockerRepo)).nodes
}

/** listChildren 的完整形态（T-461）：附带远端枚举层的降级注记 */
export async function listFolder(
  repoKey: string,
  dir: string,
  signal?: AbortSignal,
  isDockerRepo?: boolean,
): Promise<FolderListing> {
  // docker repo tree 需要 tag 数据：image 级 + manifests 级均附加 ?docker_tags
  const qs = isDockerRepo && dir !== '' ? '?docker_tags=1' : ''
  const folder = await apiJSON<ItemInfo & { dockerTags?: DockerTagsMap }>(
    `${storagePath(repoKey, dir)}${qs}`, { signal },
  )
  const dockerTags = folder.dockerTags ?? {}
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
    // 远端派生行判别（T-461）：元数据在场（?list 伺服到该行）而 sha2 缺席
    // ——落库文件恒带 blob digest，两者同时成立只可能是 display-only 行。
    // 派生行的 size=0/mtime=epoch 是占位非事实，呈现归 '—'。
    const remote = meta !== undefined && !meta.sha2
    return {
      name,
      path: dir === '' ? name : `${dir}/${name}`,
      folder: k.folder,
      size: remote ? null : meta ? meta.size : null,
      lastModified: remote ? '' : meta ? meta.lastModified : '',
      sha256: meta?.sha2 ?? '',
      remote: remote || undefined,
      // Docker manifest digest row: the child name IS the sha256 hex digest.
      // Match against the dockerTags map by child name or sha2 from ?list.
      tags: Object.prototype.hasOwnProperty.call(dockerTags, name)
        ? dockerTags[name]
        : Object.prototype.hasOwnProperty.call(dockerTags, meta?.sha2 ?? '')
          ? dockerTags[meta?.sha2 ?? '']
          : undefined,
    }
  })
  sortChildren(nodes)
  return { nodes, remoteDegraded: folder.remoteDegraded ?? '' }
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

// ---- 下载统计面（M16 T-445 / FR-144.2，消费 T-438 后端 ?stats）------------

/**
 * ?stats 面（GET /api/storage/{repo}/{path}?stats）：nodes 四列（下载计数
 * 的唯一 wire 面）的投影。计数无门（路由 = item-info 同款内容面读门，
 * 匿名随开关）；lastDownloadedBy 是例外——仅 CapSystemRead 档
 * （admin ∨ readonly_admin）回带，其余档服务端 omitempty 省略（FE 端
 * 「缺省 = '—'」，不伪造、也不区分「从未下载」与「非档位省略」）。
 * folder 行是结构性零值（CountDownload 的 SQL 边排除 folder）——详情页
 * 只在 file 形态消费本面（Artifactory folder item view 亦无下载族）。
 */
export interface NodeStats {
  uri: string
  downloadCount: number
  lastDownloaded?: string
  lastDownloadedBy?: string
  remoteDownloadCount: number
}

export function getNodeStats(repoKey: string, path: string, signal?: AbortSignal): Promise<NodeStats> {
  return apiJSON<NodeStats>(`${storagePath(repoKey, path)}?stats`, { signal })
}

/**
 * 仓级 usage + counts（M16 T-445 / FR-144.3：仓视图 Artifact Count 行数据
 * 源）。batch 面 `?repos=` 点名单行 + `?include=counts`——一次请求带回
 * usedBytes/quotaBytes/nodeCount（Size 与 Count 同源，替代 getRepoUsage
 * 单行面）。nodeCount = FILE 节点数（folder 哨兵行排除——repo service
 * 语义，与「Artifact Count」对位）。点名仓不可见（无权限/不存在）→ 行
 * 缺席（batch 面点名子集语义），返回 null。
 */
export interface RepoUsageCounts {
  usedBytes: number
  quotaBytes: number
  nodeCount: number
}

export async function getRepoUsageCounts(repoKey: string, signal?: AbortSignal): Promise<RepoUsageCounts | null> {
  const rows = await apiJSON<Array<RepoUsageCounts & { repo: string }>>(
    `/v1/storage/usage?include=counts&repos=${encodeURIComponent(repoKey)}`,
    { signal },
  )
  return Array.isArray(rows) ? (rows.find((r) => r.repo === repoKey) ?? null) : null
}

/**
 * 内容面绝对 URL（File URL 行的呈现与复制值——FR-144.2/.3）：origin +
 * `/binflow/<repo>/<path>`，folder 保留尾斜杠（与「直接下载」钮同一构造，
 * 运行时派生、无绝对路径假设；FileInfo.downloadUri 是 api/storage 形态，
 * 非 Artifactory 语义的下载 URL——不用）。
 */
export function contentFileURL(repoKey: string, ref: string): string {
  const trailing = ref.endsWith('/')
  const enc = ref
    .split('/')
    .filter((s) => s !== '')
    .map((s) => encodeURIComponent(s))
    .join('/')
  return `${window.location.origin}${CONTENT_ROOT}/${encodeURIComponent(repoKey)}${enc ? `/${enc}` : ''}${trailing ? '/' : ''}`
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
    xhr.onerror = () => reject(new ApiError(0, t('网络错误（上传中断）')))
    xhr.onabort = () => reject(new ApiError(0, t('已取消')))
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
 * 单段 PUT：BE 已通过 materializeAncestors（ADR-0016）在 putNode 时
 * 服务端材料化祖先目录行，FE 不再需要逐段 PUT 父目录。
 */
export async function mkdir(repoKey: string, dirPath: string): Promise<void> {
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
  if (name === '') return t('名称不能为空')
  if (name.includes('/')) return t('名称不能包含 /')
  if (name.includes('\\')) return t('名称不能包含反斜杠')
  if (name === '.' || name === '..') return t('不能以 . 或 .. 命名')
  // eslint-disable-next-line no-control-regex -- 与服务端 isControlByte 同口径
  if (/[\x00-\x1f\x7f]/.test(name)) return t('名称不能包含控制字符')
  return null
}

export { toApiError }