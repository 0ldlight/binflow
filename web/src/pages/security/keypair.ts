// GPG 签名密钥对面（M11 T-319 后端 / P3 FE 解锁——capability matrix
// 未列域表「keypair：API 10 op 全备，无 UI」的解锁承载）。
// 契约源 internal/httpapi/keypair.go（Artifactory 兼容 /api/security/keypair
// 族 + BinFlow-native generate + /api/v2/repositories/{repoKey}/keyPairs
// 关联面）。私钥与口令永不出现在任何响应（ADR-0038 决策 3）。
import { apiJSON, apiText } from '@/lib/api'

/** KeyPairSummary 回显（spec §1.1 四字段 + BinFlow 增补的来源/引用回显） */
export interface KeypairSummary {
  pairName: string
  pairType: string
  alias: string
  publicKey: string
  algorithm: string
  createdAt: string
  updatedAt: string
  updatedBy: string
  repositories: string[]
}

/** 导入/更新体（vaultKey/vaultPublicKey 字面被 400 拒绝——不构造） */
export interface KeypairImportBody {
  pairName: string
  pairType: string
  alias: string
  privateKey: string
  publicKey: string
  passphrase: string
}

/** BinFlow-native 服务端生成体（spec §2.2） */
export interface KeypairGenerateBody {
  pairName: string
  alias: string
  passphrase: string
  keyBits: number
  uidName: string
  uidComment: string
  uidEmail: string
}

export function listKeypairs(): Promise<KeypairSummary[]> {
  return apiJSON<KeypairSummary[]>('/security/keypair')
}

export function getKeypair(pairName: string): Promise<KeypairSummary> {
  return apiJSON<KeypairSummary>(`/security/keypair/${encodeURIComponent(pairName)}`)
}

/** 导入（POST create-or-replace，201 回显 Summary） */
export function importKeypair(body: KeypairImportBody): Promise<KeypairSummary> {
  return apiJSON<KeypairSummary>('/security/keypair', { method: 'POST', body })
}

/** 更新材料（PUT，404 absent，200 回显） */
export function updateKeypair(body: KeypairImportBody): Promise<KeypairSummary> {
  return apiJSON<KeypairSummary>('/security/keypair', { method: 'PUT', body })
}

/** 删除（200 纯文本 "OK"；被仓库引用 → 400 message 列 repo 名） */
export function deleteKeypair(pairName: string): Promise<string> {
  return apiText(`/security/keypair/${encodeURIComponent(pairName)}`, { method: 'DELETE' })
}

/** 校验：pairName-only = 校验已存对；携材料 = 校验材料（200 文案固定） */
export function verifyKeypair(body: { pairName: string } | KeypairImportBody): Promise<string> {
  return apiText('/security/keypair/verify', { method: 'POST', body })
}

/** 仓库关联对的公钥（armored text） */
export function repoPublicKey(repoKey: string): Promise<string> {
  return apiText(`/security/keypair/public/repositories/${encodeURIComponent(repoKey)}`)
}

/** 服务端生成（POST /v1/admin/security/keypair/generate，201 回显；重名 409） */
export function generateKeypair(body: KeypairGenerateBody): Promise<KeypairSummary> {
  return apiJSON<KeypairSummary>('/v1/admin/security/keypair/generate', { method: 'POST', body })
}

/** 关联（body = pair name 纯文本——Artifactory 7.19 关联面） */
export function associateKeypair(repoKey: string, pairName: string): Promise<string> {
  return apiText(`/v2/repositories/${encodeURIComponent(repoKey)}/keyPairs`, {
    method: 'POST',
    rawBody: pairName,
  })
}

/** 解除关联（名字即守卫——关联的是别的名 → 404） */
export function disassociateKeypair(repoKey: string, keyName: string): Promise<string> {
  return apiText(
    `/v2/repositories/${encodeURIComponent(repoKey)}/keyPairs/${encodeURIComponent(keyName)}`,
    { method: 'DELETE' },
  )
}
