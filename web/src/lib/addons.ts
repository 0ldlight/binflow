// license / addon 域 API（M10 T-288；契约源 internal/httpapi/license.go +
// addons.go，即 architecture §15.1.4 / §15.2.5——ADR-0032/0033）：
//
// - GET/POST/DELETE /api/system/license：GET 不回显文档原文（NFR-S52），
//   POST 的 body 是 license 文档全文（rawBody——非 JSON），成功 201 回
//   GET 同构状态体；DELETE 成功体是纯文本。readonly_admin 对 GET 可读、
//   对写两动词 403（CapSystemRead / CapSystemWrite）。
// - GET /api/v1/addons：bare array（治理族惯例），装配序全槽位清单；
//   enabled/reason 与门控执行同源求值（可见矩阵 = 执行矩阵，不会漂移）。
// - 档位闭集 community/pro/enterprise（license.Tier wire 值；闭集外按
//   地板呈现——UI 不放大）。
//
// 消费方：pages/admin/LicenseAddonsPage（管理页）+ 建仓对话框的包型档位
// 徽章（pages/repositories/RepositoryFormPage）——徽章一律吃本 API 的实时
// 数据，不在前端复制槽位清单（FR-86-AC2 单源规则）。T-441（M16 FR-143.3）
// 起建仓面不再消费 enabled 做禁用（前端槽位门整族退役——license 门系后端
// ADR-0033 域，D3 终裁）；enabled/disabledByConfig 仍服务 License 页状态格。

import { apiJSON, apiText } from './api'

// ---- 档位闭集 ----

export type LicenseTier = 'community' | 'pro' | 'enterprise'

/** 档位顺序（community 地板最小；闭集外归一为 community——呈现不放大） */
export function tierRank(tier: string): number {
  return tier === 'enterprise' ? 2 : tier === 'pro' ? 1 : 0
}

/** 归一后的档位 wire 值（未知值 → community） */
export function normalizeTier(tier: string): LicenseTier {
  return tier === 'pro' || tier === 'enterprise' ? tier : 'community'
}

/** 档位徽章 class（三色：community=中性灰 地板 / pro=蓝 / enterprise=金） */
export function tierBadgeClass(tier: string): string {
  const t = normalizeTier(tier)
  return t === 'community' ? 'badge neutral' : `badge tier-${t}`
}

// ---- GET /api/system/license（§15.1.4 字段清单，无文档原文回显） ----

export interface LicenseStatus {
  licensed: boolean
  tier: string
  licenseId: string
  licensee: string
  /** RFC3339 UTC 或空串（零值） */
  issuedAt: string
  notBefore: string
  expiresAt: string
  perpetual: boolean
  /** 永久 license / 未授权地板 = null；否则剩余整天数（过期后为负） */
  daysToExpiry: number | null
  /** 文档显式 addon 覆盖表（null = 档位全解锁） */
  addons: string[] | null
  limits: unknown
}

export function getLicense(): Promise<LicenseStatus> {
  return apiJSON<LicenseStatus>('/system/license')
}

/** 装载（验签失败 400 LICENSE_EXPIRED/LICENSE_INVALID；成功 201 即时生效） */
export function installLicense(doc: string): Promise<LicenseStatus> {
  return apiJSON<LicenseStatus>('/system/license', { method: 'POST', rawBody: doc })
}

/** 卸载（幂等；降回 community 地板，既有制品读不劫持——D1） */
export function uninstallLicense(): Promise<string> {
  return apiText('/system/license', { method: 'DELETE' })
}

// ---- GET /api/v1/addons（§15.2.5 bare array） ----

export interface AddonRow {
  id: string
  /** 'package-type' | 'feature' */
  kind: string
  /** 档位闭集 wire 值 */
  minTier: string
  enabled: boolean
  /** 未解锁槽位的原因子句（unlocked = 空串/缺省） */
  reason?: string
  displayName: string
  description: string
}

export function getAddons(): Promise<AddonRow[]> {
  return apiJSON<AddonRow[]>('/v1/addons')
}

/** 配置熔断态（addons.disabled CSV——reason 的 disabled 子句，最高优先） */
export function isDisabledByConfig(row: AddonRow): boolean {
  return !row.enabled && row.reason === 'disabled by configuration'
}

// ---- 建仓对话框消费视图（包型槽位 → 可选型清单） ----

export interface PkgTypeOption {
  /** 包型（= 槽位 id：Kind=package-type 断言 id === packageType） */
  id: string
  displayName: string
  description: string
  minTier: LicenseTier
  enabled: boolean
}

/** 包型槽位清单（装配序）。无注册表的栈（pre-M10 单元形态）= 空数组——
 *  调用方按五核心静态集呈现（那些形态本就没有门控型）。T-441 起本视图
 *  只供档位徽章与描述文案；enabled 保留给未来消费方（建仓面不再禁用）。 */
export function packageTypeOptions(rows: AddonRow[]): PkgTypeOption[] {
  return rows
    .filter((r) => r.kind === 'package-type')
    .map((r) => ({
      id: r.id,
      displayName: r.displayName,
      description: r.description,
      minTier: normalizeTier(r.minTier),
      enabled: r.enabled,
    }))
}
