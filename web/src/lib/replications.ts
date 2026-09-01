// 复制配置 CRUD API（T-404；契约源 internal/httpapi/replication.go——T-180
// 落的 REST 面，wire 形状逐字段对照）：
//
//   GET    /api/v1/replications         配置全量（凭据密码永不下发）
//   POST   /api/v1/replications         创建（201 = 创建后的配置行）
//   DELETE /api/v1/replications/{name}  删除（任务行随 009 FK 级联）
//   PUT    /api/v1/replications/{id}    **T-405 并行票落**——启停翻转，
//                                      body {enabled}；本封装先行接线，
//                                      合并前真实实例对该动词 404（router
//                                      只挂 GET/POST/DELETE），联合腿由
//                                      conductor 合并后验证（票内注明）。
//   POST   /api/v1/replications/{id}/run  **T-420（FR-138.1）**——Replicate
//                                      Now：对该配置种一次全量对账（异步；
//                                      观测走既有 /api/v1/replication/
//                                      status 面，规格 §9.2-A 排程语义）。
//
// 语义注记（R3/R4 勘误，parity v1.2 §6A）：
// - BinFlow 复制引擎 = 事件驱动（上传 hook 入队）+ 固定间隔 sweep 兜底，
//   **无用户级 cron**——Artifactory 的 cronExp/enableEventReplication/
//   pathPrefix/syncDeletes/syncProperties/syncStatistics 六字段在本引擎
//   无对位（wire 与存储层均无）；表单以「预留位（当前无效）」呈现且
//   **绝不进 payload**（不伪造语义）。
// - BinFlow 超集字段：max_bandwidth_bytes_per_sec（带宽节流）/
//   max_items_per_push（单次批量上限）——Artifactory 无，保留呈现。
// - 编辑语义：REST 无字段级 PUT（T-405 只裁启停）——字段修改的唯一可
//   跑通路径 = 删除 + 重建（DELETE+POST），配置 id 变化、未决任务级联
//   清空；表单内明示后果（调用方 repl-recreate-note）。
// - target_password 只写不读：POST 明文进、服务端 ADR-0012 封存、响应
//   无该字段；空密码 = 匿名目标（跳过 cipher）。密码需要实例配置
//   BINFLOW_REMOTE_CREDENTIALS_KEY，未配时带密码的创建 400（文案原样
//   行内呈现）。

import { apiJSON } from './api'

/** 配置行（GET/POST/PUT 响应体——replicationConfigResponse，凭据密码无字段） */
export interface ReplicationConfig {
  id: number
  name: string
  source_repo: string
  target_url: string
  target_repo: string
  target_username: string
  max_bandwidth_bytes_per_sec: number
  max_items_per_push: number
  enabled: boolean
  created_at: string
  updated_at: string
}

/** 创建体（replicationConfigBody）。enabled 显式传——服务端缺省 true，
 *  表单永远带用户所见值（flip-off 必须过 round trip）。 */
export interface ReplicationConfigBody {
  name: string
  source_repo: string
  target_url: string
  target_repo: string
  target_username: string
  target_password: string
  max_bandwidth_bytes_per_sec: number
  max_items_per_push: number
  enabled: boolean
}

/** 全量配置列表（CapSystemRead：admin / readonly_admin；普通 user 403） */
export function listReplicationConfigs(): Promise<ReplicationConfig[]> {
  return apiJSON<ReplicationConfig[]>('/v1/replications')
}

/** 创建（CapSystemWrite；409 = 名称已占，400 = 校验/未知源仓/密码无主键） */
export function createReplicationConfig(body: ReplicationConfigBody): Promise<ReplicationConfig> {
  return apiJSON<ReplicationConfig>('/v1/replications', { method: 'POST', body })
}

/** 删除（按 name——服务端经清单解析 id；204 无体；任务行级联） */
export function deleteReplicationConfig(name: string): Promise<void> {
  return apiJSON<void>(`/v1/replications/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

/**
 * 启停翻转（T-405 联合腿）：PUT /v1/replications/{id}，body {enabled}。
 * 响应假定 = 更新后的配置行（与 POST 同形）——T-405 落地前该动词在真实
 * 实例 404，调用方按 ApiError 呈现并保持行内原值（不乐观更新）。
 */
export function putReplicationEnabled(id: number, enabled: boolean): Promise<ReplicationConfig> {
  return apiJSON<ReplicationConfig>(`/v1/replications/${id}`, { method: 'PUT', body: { enabled } })
}

/** 全量同步触发响应（T-420，FR-138.1——replicationRunResponse）。`info`
 *  为锚定排程文案（规格 §9.1 UI 面）；scheduled = 本次种下的任务行数
 *  （0 = 源仓当前无制品，合法的空跑）；capped = max_items_per_push 截断
 *  （再点一次取下一段，路径序确定性分片）。 */
export interface ReplicationRunResult {
  info: string
  id: number
  name: string
  scheduled: number
  capped: boolean
}

/**
 * Replicate Now（T-420）：POST /v1/replications/{id}/run——对该配置种一次
 * 全量对账（每个源仓文件节点一条 pending 任务，异步执行；状态经
 * /api/v1/replication/status 观测面查询）。语义照规格 §9.2-A：重复触发
 * 不去重（结果收敛，200）；enabled:false 拒绝（409，先 PUT enabled=true）；
 * 封锁门（§9.2-A-5）归 T-422。
 */
export function runReplicationNow(id: number): Promise<ReplicationRunResult> {
  return apiJSON<ReplicationRunResult>(`/v1/replications/${id}/run`, { method: 'POST' })
}

/**
 * 配置名合法性的前端镜像（validReplicationName）：1..64 字符的
 * [A-Za-z0-9._-]、首字符必须字母数字。闭集字符集保证 name 可作 DELETE
 * 路径的单一段。返回 null = 合法，否则为行内错误文案。
 */
export function validateReplicationName(name: string): string | null {
  if (name === '') return '配置名未填'
  if (name.length > 64) return '配置名最长 64 字符'
  if (!/^[A-Za-z0-9]/.test(name)) return '配置名须以字母或数字开头'
  if (!/^[A-Za-z0-9._-]+$/.test(name)) return '配置名仅允许字母/数字/./_/-（无空格与路径段）'
  return null
}

/**
 * 目标 URL 合法性的前端镜像（validTargetURL——engine.targetURL 的创建门）：
 * 绝对 http/https 且带 host。私网地址合法（SSRF 链按请求校验，
 * replication.allow_private_target 缺省放行）。
 */
export function validateReplicationTargetURL(raw: string): string | null {
  const v = raw.trim()
  if (v === '') return '目标 URL 未填'
  try {
    const u = new URL(v)
    if ((u.protocol !== 'http:' && u.protocol !== 'https:') || u.host === '') {
      return '目标 URL 须为带主机的绝对 http/https 地址'
    }
    return null
  } catch {
    return '目标 URL 须为带主机的绝对 http/https 地址'
  }
}

/** 按源仓过滤（列表端点无 query 参数——客户端过滤，仓级两消费面共用） */
export function configsForRepo(configs: ReplicationConfig[], repoKey: string): ReplicationConfig[] {
  return configs.filter((c) => c.source_repo === repoKey)
}
