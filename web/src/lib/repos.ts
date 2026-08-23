// 仓库域 API（T-99；契约源 internal/httpapi/repositories.go + usage.go）：
//
// - 列表/详情 GET 走 JSON 面（E-04/E-05）；创建 PUT / 更新 POST / 删除
//   DELETE 的成功体是纯文本（repo 管理面），错误统一 E-01 信封由 api 层
//   解析成 ApiError.message —— 400 校验文案（key/url/组合矩阵）、404
//   unknown key、403 非 admin 原样行内呈现。
// - governance 字段（T-95）：quotaBytes/patterns 只在 LOCAL 仓的 config
//   透传链上有效（remote/virtual 被 repo.Service 规范化丢弃——T-95 遗留⑤，
//   表单按 local 呈现，其余只读「—」）。
// - 更新是**全量替换**语义（UpdateRepo：提供了 config 就整体重写）：
//   编辑表单从 GET 回显预填全部字段再整体提交，避免「改一个字段丢
//   其它字段」；transport 未覆盖的本地 config 字段见工作日志契约漂移。

import { apiJSON, apiText } from './api'
import type { RepoListItem } from './api'

export type RClass = 'local' | 'remote' | 'virtual'
export type PackageType = 'generic' | 'docker' | 'maven' | 'npm' | 'pypi'

export const RCLASSES: RClass[] = ['local', 'remote', 'virtual']
export const PACKAGE_TYPES: PackageType[] = ['generic', 'docker', 'maven', 'npm', 'pypi']

/** Remote×Docker / Virtual×Docker 非法（FR-15-AC7：docker 仅 local） */
export function comboAllowed(rclass: RClass, packageType: PackageType): boolean {
  if (packageType === 'docker') return rclass === 'local'
  return true
}

/** ADR-0008 保留段（api/v2/docs/console/ui/assets——与 internal/repo/api.go 同源） */
export const RESERVED_REPO_KEYS = ['api', 'v2', 'docs', 'console', 'ui', 'assets']

/**
 * repo key 前端预检（服务端终裁）：[a-z][a-z0-9-]{1,62}（总长 2..63）+
 * 保留段。返回错误文案；null = 通过。
 */
export function validateRepoKey(key: string): string | null {
  if (key === '') return null // 空值不报错（必填在步骤门控拦）
  if (key.length < 2 || key.length > 63) return '长度需为 2~63 个字符'
  if (!/^[a-z]/.test(key)) return '必须以小写字母开头'
  if (!/^[a-z0-9-]*$/.test(key.slice(1))) return '只允许小写字母、数字与连字符（-）'
  if (RESERVED_REPO_KEYS.includes(key)) return `「${key}」是路由保留段，不能用作仓库 key`
  return null
}

/** 上游 URL 前端预检：http/https 且带 host（与服务端 parseRemoteConfig 同口径） */
export function validateUpstreamURL(url: string): string | null {
  if (url === '') return null
  let u: URL
  try {
    u = new URL(url)
  } catch {
    return 'URL 格式无效'
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') return 'scheme 必须是 http 或 https'
  if (!u.host) return '缺少 host'
  return null
}

// ---- GET 面 ----

/** GET /api/repositories/{key} 的回显体（configuration 是规范化后的 config） */
export interface RepoDetail {
  key: string
  rclass: string
  packageType: string
  description: string
  url: string
  configuration?: Record<string, unknown>
}

/** GET /api/v1/storage/usage/{repo}（T-95 GE-06；admin 或有 read 权限） */
export interface RepoUsage {
  repo: string
  usedBytes: number
  quotaBytes: number
}

/** PUT/POST /api/repositories/{key} 请求体（transport 字段子集，按 rclass 组装） */
export interface RepoConfigBody {
  key?: string
  rclass: RClass
  packageType: PackageType
  description: string
  // remote
  url?: string
  username?: string
  password?: string // 仅创建/更换时携带；GET 永不回显（NFR-S14）
  allowPrivateUpstream?: boolean
  hardFail?: boolean
  retrievalCachePeriodSecs?: number
  missedRetrievalCachePeriodSecs?: number
  socketTimeoutSecs?: number
  assumedOfflinePeriodSecs?: number
  // virtual
  repositories?: string[]
  defaultDeploymentRepo?: string
  // local（governance T-95 + maven 策略族 T-64/T-67 透传）
  priorityResolution?: boolean
  handleReleases?: boolean
  handleSnapshots?: boolean
  snapshotVersionBehavior?: string
  checksumPolicyType?: string
  includesPattern?: string
  excludesPattern?: string
  quotaBytes?: number
}

export function getRepositoriesFiltered(repoType = '', packageType = ''): Promise<RepoListItem[]> {
  const params = new URLSearchParams()
  if (repoType) params.set('type', repoType)
  if (packageType) params.set('packageType', packageType)
  const qs = params.toString()
  return apiJSON<RepoListItem[]>(`/repositories${qs ? `?${qs}` : ''}`)
}

export function getRepoDetail(key: string): Promise<RepoDetail> {
  return apiJSON<RepoDetail>(`/repositories/${encodeURIComponent(key)}`)
}

export function getRepoUsage(key: string): Promise<RepoUsage> {
  return apiJSON<RepoUsage>(`/v1/storage/usage/${encodeURIComponent(key)}`)
}

/** 创建（PUT；成功体纯文本 "Successfully created repository '<key>'"） */
export function createRepo(key: string, body: RepoConfigBody): Promise<string> {
  return apiText(`/repositories/${encodeURIComponent(key)}`, { method: 'PUT', body })
}

/** 更新（POST 更新拼法；key 不存在 404——防误建） */
export function updateRepo(key: string, body: RepoConfigBody): Promise<string> {
  return apiText(`/repositories/${encodeURIComponent(key)}`, { method: 'POST', body })
}

/** 删除（非空仓需 deleteContent，否则 400 且 message 携带 node 数） */
export function deleteRepo(key: string, deleteContent: boolean): Promise<string> {
  return apiText(
    `/repositories/${encodeURIComponent(key)}${deleteContent ? '?deleteContent=true' : ''}`,
    { method: 'DELETE' },
  )
}

// ---- configuration 回显读取（unknown 收窄小工具） ----

export function cfgStr(cfg: Record<string, unknown> | undefined, field: string): string {
  const v = cfg?.[field]
  return typeof v === 'string' ? v : ''
}

export function cfgBool(cfg: Record<string, unknown> | undefined, field: string, dflt = false): boolean {
  const v = cfg?.[field]
  return typeof v === 'boolean' ? v : dflt
}

export function cfgNum(cfg: Record<string, unknown> | undefined, field: string): number | null {
  const v = cfg?.[field]
  return typeof v === 'number' && Number.isFinite(v) ? v : null
}

export function cfgStrList(cfg: Record<string, unknown> | undefined, field: string): string[] {
  const v = cfg?.[field]
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
}

// ---- 配额行内编辑共享件（T-244 抽取：QuotasPage 与 RepoDetailPage 此前
// 各持一份同源副本——T-240 登记；行为零变化，字段集 = RepositoryFormPage
// buildBody 的 local 分支）----

/**
 * 组装「只覆写 quotaBytes」的 local 仓全量替换体（POST 全量替换语义——
 * 先 GET 详情再重组完整 body，防止「改配额丢其它字段」）。maven 仓附
 * 包类型专属字段。
 */
export function buildLocalQuotaBody(d: RepoDetail, quotaBytes: number): RepoConfigBody {
  const cfg = d.configuration
  const body: RepoConfigBody = {
    rclass: 'local',
    packageType: (d.packageType as PackageType) ?? 'generic',
    description: d.description ?? '',
    priorityResolution: cfgBool(cfg, 'priorityResolution'),
    includesPattern: cfgStr(cfg, 'includesPattern'),
    excludesPattern: cfgStr(cfg, 'excludesPattern'),
    quotaBytes,
  }
  if (d.packageType === 'maven') {
    body.handleReleases = cfgBool(cfg, 'handleReleases', true)
    body.handleSnapshots = cfgBool(cfg, 'handleSnapshots', true)
    body.checksumPolicyType = cfgStr(cfg, 'checksumPolicyType') || 'client-checksums'
    body.snapshotVersionBehavior = cfgStr(cfg, 'snapshotVersionBehavior') || 'deployer'
  }
  return body
}
