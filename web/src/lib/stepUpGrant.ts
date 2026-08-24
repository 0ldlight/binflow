import { useSyncExternalStore } from 'react'

// OIDC step-up 控制台腿（T-260 / FR-81，消费 ADR-0027 决策 4+8 的既有服务端
// 契约，architecture §14.3 UI 消费面）：
//
// - **fragment 承载**：IdP 重认证回跳后 302 落 `/binflow/ui/#step_up_grant=
//   <64hex>`——grant 明文只出现在 fragment 与后续 mint 请求体两处（§14.3-4
//   日志卫生），本模块负责「提取即抹除」（history.replaceState，不入历史、
//   刷新不重放、地址栏不可见）。
// - **pending-mint 上下文**：发起重认证跳转前把 {pkg, repo, startedAt} 存
//   sessionStorage（键 `bf.pendingMint`）——同 tab 跨导航存活、随 tab 关闭
//   消失；grant 载体永不落 localStorage，pending 里也绝不存 grant 值。
// - **单次消费**：grant 仅存 JS 内存；mint 401 `step_up_invalid`（过期/已
//   用/身份不符）即清 grant + pending，绝不以旧 grant 重试——重走 init。
// - grant TTL 服务端配置 [60,3600] 无查询端点：倒计时以默认 300s 呈现
//   （提示性质，服务端 401 才是权威裁决）。
//
// 本模块零 console.*：任何值（尤其 grant）不出现在前端日志面。

/** sessionStorage 键（§14.3-1 指定拼写） */
const PENDING_KEY = 'bf.pendingMint'

/** grant 形态：256-bit hex（服务端 internal/auth grantBytes=32 → hex 64 位） */
const GRANT_SHAPE = /^[0-9a-f]{64}$/

/** fragment 前缀（服务端 oidc.go stepUpGrantFragment——唯一约定拼写字面量） */
export const STEP_UP_GRANT_FRAGMENT = '#step_up_grant='

/**
 * 倒计时呈现基准（秒）。实际 TTL 是实例配置（[60,3600]），无查询端点——
 * 这里按 ADR-0027 决策 6 的默认 300s 呈现，超时由服务端 401 兜底。
 */
export const GRANT_TTL_HINT_SECONDS = 300

/** pending 陈旧线：grant TTL 上界 3600s + 60s 余量——超线静默清理
 * （流程可能半途死在 IdP，不给 sessionStorage 留永久残留） */
const PENDING_STALE_MS = (3600 + 60) * 1000

/** 发起视图上下文：SetMeUp 主对话框的包类型 + 仓库。铸造本身与仓库无关
 * （POST /api/security/token 是用户域），上下文只为回跳后重开同一视图。 */
export interface PendingMint {
  pkg: string
  repo: string
  /** 发起时刻（epoch ms）——陈旧判定与「等待重认证完成」提示的时长基准 */
  startedAt: number
}

function parsePending(raw: string | null): PendingMint | null {
  if (!raw) return null
  try {
    const p = JSON.parse(raw) as Partial<PendingMint>
    if (
      typeof p.pkg !== 'string' ||
      typeof p.repo !== 'string' ||
      typeof p.startedAt !== 'number' ||
      !Number.isFinite(p.startedAt)
    ) {
      return null
    }
    return { pkg: p.pkg, repo: p.repo, startedAt: p.startedAt }
  } catch {
    return null // 损坏载荷按不存在处理
  }
}

/** 读 pending（陈旧即顺手清理）；sessionStorage 不可用（隐私模式等）返回 null */
export function readPendingMint(): PendingMint | null {
  let raw: string | null = null
  try {
    raw = sessionStorage.getItem(PENDING_KEY)
  } catch {
    return null
  }
  const p = parsePending(raw)
  if (!p) {
    if (raw !== null) clearPendingMint()
    return null
  }
  if (Date.now() - p.startedAt > PENDING_STALE_MS) {
    clearPendingMint()
    return null
  }
  return p
}

/** 发起重认证跳转前落上下文（grant 值绝不进此结构） */
export function savePendingMint(p: PendingMint): void {
  try {
    sessionStorage.setItem(PENDING_KEY, JSON.stringify(p))
  } catch {
    // sessionStorage 不可用：流程退化为无上下文续铸（grant 仍可用）
  }
}

/** 清 pending（mint 成功 / invalid / 用户放弃续铸） */
export function clearPendingMint(): void {
  try {
    sessionStorage.removeItem(PENDING_KEY)
  } catch {
    // 不可用即无处可清
  }
}

// ---- in-memory grant store（useSyncExternalStore 消费） ---------------------

export interface StepUpState {
  /** 当前持有的 mint grant（仅 JS 内存；null = 无待续铸） */
  grant: string | null
  /** grant 到手时刻（epoch ms）——倒计时基准 */
  grantAt: number
  /** 随 grant 恢复的发起上下文 */
  pending: PendingMint | null
}

let state: StepUpState = { grant: null, grantAt: 0, pending: null }

const listeners = new Set<() => void>()
function emit(): void {
  for (const l of listeners) l()
}
function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** 组件面消费：grant 到手（→ AppShell 重开续铸视图）与倒计时 */
export function useStepUp(): StepUpState {
  return useSyncExternalStore(subscribe, () => state)
}

/** 非 React 面（挂载 effect）取当前 grant 值 */
export function stepUpGrantValue(): string | null {
  return state.grant
}

/**
 * 结算成功：grant 已消费铸出令牌——清 grant + pending，续铸视图回到常态。
 */
export function settleStepUpMinted(): void {
  state = { grant: null, grantAt: 0, pending: null }
  clearPendingMint()
  emit()
}

/**
 * 结算 401 step_up_invalid：grant 过期/已用/身份不符——即清 grant + pending
 * （单次消费 UX：绝不以旧 grant 重试，重走 init 由用户显式发起）。
 */
export function settleStepUpInvalid(): void {
  settleStepUpMinted()
}

/** 用户关闭续铸视图：视同放弃（服务端 grant 自然 TTL 失效，不主动作废） */
export function abandonStepUp(): void {
  settleStepUpMinted()
}

// ---- fragment 消费（app 挂载期、早于路由渲染） ------------------------------

/**
 * 处理回跳 fragment：`#step_up_grant=<grant>` → 提取入内存 → 立即
 * `history.replaceState` 抹除（保 pathname/search，弃 fragment；不触发
 * popstate，路由零感知）→ 有 pending 上下文才激活 grant，否则静默丢弃
 * （孤儿 fragment 同样抹除——防刷新重放/分享泄漏，§14.3-2）。
 *
 * 幂等：无该前缀 fragment 时零副作用。main.tsx 模块作用域调用一次。
 */
export function consumeStepUpFragment(): void {
  const hash = window.location.hash
  if (!hash.startsWith(STEP_UP_GRANT_FRAGMENT)) return
  // 抹除先于一切（即便后续判定丢弃，fragment 也不留地址栏）
  history.replaceState(history.state, '', window.location.pathname + window.location.search)
  const value = hash.slice(STEP_UP_GRANT_FRAGMENT.length)
  if (!GRANT_SHAPE.test(value)) return // 形态不符 → 孤儿：已抹除，静默丢弃
  const pending = readPendingMint()
  if (!pending) return // 无发起上下文（跨 tab 回跳/残留）→ 静默丢弃，正常启动
  state = { grant: value, grantAt: Date.now(), pending }
  emit()
}
