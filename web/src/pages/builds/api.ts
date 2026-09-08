// Build-info 域 FE 查询面（T-512 / FR-152.3——契约源 internal/httpapi/build.go
// 〔T-508/T-509〕，wire 冻结 docs/reverse/build-info.md §1/§2.1/§3.1）：
//
// - GET /build                     → {uri, builds:[{uri:"/<name>", lastStarted}]}
// - GET /build/{name}              → {uri, buildsNumbers:[{uri:"/<number>", started}]}
//                                    （started 倒序；空可见集 = 404 零泄漏法——
//                                    不可读名与不存在同形）
// - GET /build/{name}/{number}     → {uri, buildInfo}（?started= 消歧同名同号
//                                    多 run；缺省 = 最新 run；?buildRepo= 寻址
//                                    自定义逻辑 build 仓）
//
// 读门 = r(buildRepo, buildName) 镜像：名单/号单面服务端可见集过滤（无授予
// 普通用户空集/404，零泄漏 oracle）；详情面拒绝先于存在（403 = 读门拒绝即
// 答案，不与 404 的「真缺」混同——BundlesPage 同款分立承载）。
//
// Module ID 反查（制品详情页 T-512 第三缺位解除）：无 node→module 反查端点
// （build_artifacts 的反向面 = aql.md §15.3 登记 artifacts(build) 入口 M18+
// 翻转点）——FE 走官方 BuildConstants 属性三键（build.name / build.number /
// build.timestamp，build-info.md §2.2「制品↔build 关联标记的机制本体」）：
// 节点属性族在场 → 单次 run 详情探测，在 modules[].artifacts[].path（关联形
// "<repo>/<path>"，record-only 行无 path）里精确匹配本制品坐标 → Module ID。
// 属性族缺席 = 无 build 关联，如实空态不伪造（完整反查面归 M18+ 翻译点）。

import { apiJSON } from '../../lib/api'
import type { AuditEvent } from '../../lib/api'

/** 缺省逻辑 build 仓（build-info.md §0-②：产品 DDL DEFAULT 三证） */
export const DEFAULT_BUILD_REPO = 'artifactory-build-info'

/** 名单行（uri = "/<name>" 相对形；一行 = (name, build_repo)，wire 不携
 *  repo 字段——跨 buildRepo 同名行 uri 同形，键按 name+lastStarted 复合） */
export interface BuildNameRow {
  uri: string
  lastStarted: string
}

export interface BuildNamesResponse {
  uri: string
  builds: BuildNameRow[]
}

/** 号单行（uri = "/<number>"；started = 规范 UTC Java 形，倒序 = 最新在前） */
export interface BuildNumberRow {
  uri: string
  started: string
}

export interface BuildNumbersResponse {
  uri: string
  buildsNumbers: BuildNumberRow[]
}

/** module 的制品行（path = 关联形 "<repo>/<path>"——record-only 行 path
 *  为空串「行存不冒领关联」；sha256 为上传文档值非节点复算值） */
export interface BuildArtifactInfo {
  type?: string
  sha1?: string
  sha256?: string
  md5?: string
  name?: string
  path?: string
}

export interface BuildDependencyInfo {
  type?: string
  sha1?: string
  sha256?: string
  md5?: string
  id?: string
  scopes?: string[]
}

export interface BuildModuleInfo {
  id: string
  type?: string
  artifacts: BuildArtifactInfo[]
  dependencies: BuildDependencyInfo[]
}

/** promotion 历史行（六元组；现势 = 按 timestamp 取最新——§2.4） */
export interface BuildStatusInfo {
  status: string
  timestamp: string
  comment?: string
  repository?: string
  ciUser?: string
  user?: string
}

/** GET 详情的 buildInfo echo：归档 payload 为底（未解释字段原样穿透——
 *  url/vcs/issues/buildAgent 等可选字段「在场才呈现」）+ 规范真值覆盖 */
export interface BuildInfo {
  name: string
  number: string
  started: string
  type?: string
  url?: string
  modules: BuildModuleInfo[]
  properties?: Record<string, string>
  statuses?: BuildStatusInfo[]
  [k: string]: unknown
}

export interface BuildDetailResponse {
  uri: string
  buildInfo: BuildInfo
}

function buildPath(name: string, number?: string): string {
  const segs = [name, number].filter((s): s is string => s !== undefined && s !== '')
  return `/build/${segs.map((s) => encodeURIComponent(s)).join('/')}`
}

export function listBuildNames(signal?: AbortSignal): Promise<BuildNamesResponse> {
  return apiJSON<BuildNamesResponse>('/build', { signal })
}

export function listBuildNumbers(name: string, signal?: AbortSignal): Promise<BuildNumbersResponse> {
  return apiJSON<BuildNumbersResponse>(buildPath(name), { signal })
}

export function getBuildRun(
  name: string,
  number: string,
  opts: { started?: string; buildRepo?: string } = {},
  signal?: AbortSignal,
): Promise<BuildDetailResponse> {
  const p = new URLSearchParams()
  if (opts.started) p.set('started', opts.started)
  if (opts.buildRepo && opts.buildRepo !== DEFAULT_BUILD_REPO) p.set('buildRepo', opts.buildRepo)
  const qs = p.toString()
  return apiJSON<BuildDetailResponse>(`${buildPath(name, number)}${qs ? `?${qs}` : ''}`, { signal })
}

// ---- build 事件时间线（audit 面，零新端点） ----------------------------------
//
// build 域五词（T-509 §6.1——audit/api.go Actions() 闭集）：upload/append/
// delete/retention 四词 Repo = build 仓（名级坐标 Repo=buildRepo、Path=
// build 名）；promote 词 Repo = 目标仓（无目标 = build 仓）——两查询覆盖：
// Q1 repo=<buildRepo>（四词 + 状态-only promote）+ Q2 action=build.promote
// （跨目标仓的 promote 行），按 id 去重合并后客户端过滤本 run（Path === 名
// && detail.number === 号；delete 行的 detail.started 与 run 不符时剔除——
// 同名同号四元下别的 run 被删不冒充本 run 事件）。retention 是名级窗口事件
// （detail 无 number，不可归属单个 run）——不进 run 时间线，票内裁定留痕。
// GET /api/v1/audit 为 admin 面：非 admin 403 由调用方整段隐藏（隐藏不泄
// 漏存在性）。

export interface BuildTimelineEvent {
  id: number
  time: string
  actor: string
  action: string
  detail: Record<string, unknown>
}

/** run 级事件词 → 呈现序（时间线行序按 time 倒序由调用方排） */
const RUN_ACTIONS = new Set(['build.upload', 'build.append', 'build.promote', 'build.delete'])

async function auditPage(query: string): Promise<AuditEvent[]> {
  // 403 = 非 admin：上抛由调用方隐藏整段（区分于可重试错误）——不吞异常
  const page = await apiJSON<{ events: AuditEvent[]; nextCursor: string }>(`/v1/audit?${query}`)
  return page.events ?? []
}

/** 单 build run 的事件时间线（两查询 + 客户端过滤；admin 面） */
export async function fetchBuildRunEvents(
  buildRepo: string,
  name: string,
  number: string,
  started?: string,
): Promise<BuildTimelineEvent[]> {
  const [byRepo, promotes] = await Promise.all([
    auditPage(`repo=${encodeURIComponent(buildRepo)}&limit=1000`),
    auditPage(`action=build.promote&limit=1000`),
  ])
  const seen = new Set<number>()
  const rows: BuildTimelineEvent[] = []
  for (const e of [...byRepo, ...promotes]) {
    if (seen.has(e.id)) continue
    seen.add(e.id)
    if (!RUN_ACTIONS.has(e.action)) continue
    if (e.path !== name) continue
    const d = (e.detail ?? {}) as Record<string, unknown>
    if (String(d.number ?? '') !== number) continue
    // delete 行带 started（被删 run 的规范启动时刻）——四元消歧：他 run 的
    // 删除事件不进本 run 时间线
    if (e.action === 'build.delete' && started && d.started && d.started !== started) continue
    rows.push({ id: e.id, time: e.time, actor: e.actor, action: e.action, detail: d })
  }
  rows.sort((a, b) => (a.time < b.time ? 1 : a.time > b.time ? -1 : b.id - a.id))
  return rows
}

// ---- Module ID 探测（制品详情页消费） ----------------------------------------

/** 节点属性里的 build 关联三键（官方 BuildConstants——build-info.md §2.2） */
export interface BuildAssocKeys {
  name: string
  number: string
  /** build.timestamp（epoch 毫秒）——转 RFC3339 作 ?started= 消歧字面 */
  startedISO?: string
}

/** 属性 map → build 关联坐标（build.name+build.number 双在场才算；多值取
 *  首值——属性面是值集合，官方机制是单值写入） */
export function buildAssocOf(props: Record<string, string[]>): BuildAssocKeys | null {
  const name = props['build.name']?.[0]
  const number = props['build.number']?.[0]
  if (!name || !number) return null
  const out: BuildAssocKeys = { name, number }
  const ts = props['build.timestamp']?.[0]
  if (ts && /^\d+$/.test(ts)) {
    const iso = new Date(Number(ts)).toISOString()
    if (!Number.isNaN(Date.parse(iso))) out.startedISO = iso
  }
  return out
}

/** run 详情 → 引用本制品（repo/path）的 module id 集（关联形路径精确匹配；
 *  record-only 行无 path 不匹配——不冒领） */
export function moduleIdsOf(info: BuildInfo, repoKey: string, path: string): string[] {
  const want = `${repoKey}/${path}`
  const ids: string[] = []
  for (const m of info.modules ?? []) {
    for (const a of m.artifacts ?? []) {
      if (a.path === want && !ids.includes(m.id)) {
        ids.push(m.id)
        break
      }
    }
  }
  return ids
}
