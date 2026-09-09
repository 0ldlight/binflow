// Release Bundle 域 API（T-514 / FR-153.3——FE 查询面；契约源
// internal/httpapi/bundle.go〔T-513 落地〕，端点族锚 docs/reverse/
// release-bundle.md §1 源侧查询族）：
//
// - GET /release/bundles                 → {uri, bundles:[{uri,name}]}
// - GET /release/bundles/{name}          → {uri, versions:[{uri,version,state,created}]}
// - GET /release/bundles/{name}/{version}→ {uri, info:{name,version,status,
//                                          created,created_by,signature,type,
//                                          artifacts:[{repo,path,sha256,size}]}}
// - HEAD  同 GET 单查                    → X-Checksum-Sha256（描述符 JSON 的
//                                          sha256，无 body——E5 锚定面）
//
// 读门 = CapSystemRead ∨ Any Distribution 通道（Can(p,'ANY DISTRIBUTION',
// bundle_name,'read')）——无授予的普通用户列表 200 空集（服务端可见集
// 过滤、零泄漏 oracle）；单查对不可见名 404（与不存在同形）。FE 不预判
// 权限，四态如实呈现。
//
// 创建面（POST /api/release/bundle，pro 槽位门）本票不建 UI——列表页注记
// 引导 API（创建腿归后续票，票面「FE 主要消费查询面」）。

import { rawRequest, apiJSON } from '../../lib/api'

/** 名清单行（uri = "/<name>" 相对形——平台自有 echo 形，spec-pending T-513 §2-3） */
export interface BundleNameRow {
  uri: string
  name: string
}

export interface BundleNamesResponse {
  uri: string
  bundles: BundleNameRow[]
}

/** 版本清单行（created = RFC3339 UTC；state ∈ COMPLETE | INPROGRESS） */
export interface BundleVersionRow {
  uri: string
  version: string
  state: string
  created: string
}

export interface BundleVersionsResponse {
  uri: string
  versions: BundleVersionRow[]
}

/** 清单行（sha256='' = pending 未快照——T-513 容忍缺位政策，如实呈现不伪造） */
export interface BundleArtifactRow {
  repo: string
  path: string
  sha256: string
  size: number
}

/** 描述符（storing_repo/keep/source_service_id 按 E⑤ 省略不伪造；signature
 *  = 清单身份集摘要占位；type 恒 SOURCE 回显） */
export interface BundleDescriptor {
  name: string
  version: string
  status: string
  created: string
  created_by: string
  signature: string
  type: string
  artifacts: BundleArtifactRow[]
}

export interface BundleDetailResponse {
  uri: string
  info: BundleDescriptor
}

export function listBundleNames(): Promise<BundleNamesResponse> {
  return apiJSON<BundleNamesResponse>('/release/bundles')
}

export function listBundleVersions(name: string): Promise<BundleVersionsResponse> {
  return apiJSON<BundleVersionsResponse>(`/release/bundles/${encodeURIComponent(name)}`)
}

export function getBundleDescriptor(name: string, version: string): Promise<BundleDetailResponse> {
  return apiJSON<BundleDetailResponse>(
    `/release/bundles/${encodeURIComponent(name)}/${encodeURIComponent(version)}`,
  )
}

/** HEAD 校验和探针（E5 锚定面）：描述符 JSON 字节的 sha256。服务端恒带头；
 *  头缺席（异常代理层剥头等）回空串——调用方按「不可得」如实呈现。 */
export async function headBundleChecksum(name: string, version: string): Promise<string> {
  const res = await rawRequest(
    `/release/bundles/${encodeURIComponent(name)}/${encodeURIComponent(version)}`,
    { method: 'HEAD' },
  )
  return res.headers.get('X-Checksum-Sha256') ?? ''
}

// ---- 创建面（P3 解锁——capability matrix 未列域 bundles 行：「POST create、
// GET status 只读面已用」）：
// POST /api/release/bundle：显式清单形（{name, version, artifacts[]}）——
// 官方 AQL 装配/signature/uuid 三通道被 400 点名拒绝（不静默丢弃）。
// 冲突三态：202 新建 / 200 同清单续建 / 409 异清单或已完成（flat 体
// "Bundle already exists"）。门 = sys:write + release-bundle 槽（pro+）。

/** 显式清单行（repo/path 必填；sha256 可空 = 不校验钉） */
export interface BundleManifestItem {
  repo: string
  path: string
  sha256: string
}

export interface BundleCreateResponse {
  bundle_path: string
}

export function createBundle(
  name: string,
  version: string,
  artifacts: BundleManifestItem[],
): Promise<BundleCreateResponse> {
  return apiJSON<BundleCreateResponse>('/release/bundle', {
    method: 'POST',
    body: { name, version, artifacts },
  })
}
