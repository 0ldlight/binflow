// AI 上下文模型（frontend-rewrite-architecture §1「AI 基座」行 + 总令 §十五
// Context-aware Copilot 设计意图）：从当前路由派生会话上下文坐标——
// domain（explorer/repos/admin/…）+ repoKey/path/buildName 等 URL 模型解析。
//
// 解析事实源 = app/router/index.tsx 路由表（URL 深链契约 §0-5 的只读消费，
// 不复刻页面内部状态）。纯函数、无 React 依赖——组件层 useLocation 取
// pathname/search 后调用；派生结果供 ChatPanel 头部徽章与 mock provider
// 的上下文感知样板消费（真后端接入后同形喂给提示词）。
//
// 边界（audit 盲区② 裁定）：本模块只做「当前在哪」的坐标派生，不发起
// 任何请求、不读会话/权限面（上下文徽章是导航坐标，不是数据快照）。

/** AI 面上下文域（与侧栏四分组语义对齐的粗粒度坐标） */
export type AiDomain =
  | 'explorer'
  | 'repos'
  | 'search'
  | 'builds'
  | 'bundles'
  | 'dashboard'
  | 'profile'
  | 'security'
  | 'governance'
  | 'monitoring'
  | 'admin'
  | 'console'

export interface AiContext {
  domain: AiDomain
  /** 完整路由（不含 basename——React Router location.pathname 形态） */
  route: string
  /** Explorer/仓库域：仓 key */
  repoKey?: string
  /** Explorer：仓内路径（目录/文件，含文件末段——选中态坐标） */
  path?: string
  /** Explorer：详情页签（general|properties|permissions） */
  tab?: string
  /** 仓库列表页：仓型（local|remote|virtual） */
  rclass?: string
  /** Builds：构建名 / 号 */
  buildName?: string
  buildNumber?: string
  /** Bundles：发行集名 / 版本 */
  bundleName?: string
  bundleVersion?: string
  /** Search：查询词（?q=） */
  query?: string
}

/** Explorer 页签段合法值（pages/explorer/model.ts TAB_SLUG 同源闭集） */
const EXPLORER_TABS = new Set(['general', 'properties', 'permissions'])

const RCLASS = new Set(['local', 'remote', 'virtual'])

function decodeSeg(raw: string): string {
  try {
    return decodeURIComponent(raw)
  } catch {
    return raw
  }
}

/**
 * 路由 → AIContext（纯函数）。分派次序与路由表同样从具体到宽泛：
 * /artifacts 的页签段优先于仓 key；/admin/repositories 的 new/rclass
 * 段优先于 :key；?q= 只在 /search 域取。
 */
export function deriveAiContext(pathname: string, search = ''): AiContext {
  const path = pathname.replace(/\/+$/, '') || '/'
  const segs = path.split('/').filter((s) => s !== '').map(decodeSeg)
  const ctx: AiContext = { domain: 'console', route: path }

  if (segs.length === 0) return ctx
  const [head, ...rest] = segs

  // —— Core 域 ——
  if (head === 'dashboard') return { ...ctx, domain: 'dashboard' }
  if (head === 'profile') return { ...ctx, domain: 'profile' }

  if (head === 'artifacts') {
    // /artifacts[/TAB/repo/path…|/repo/path…]（TAB ∈ general|properties|permissions）
    let cursor = rest
    if (cursor.length > 0 && EXPLORER_TABS.has(cursor[0])) {
      ctx.tab = cursor[0]
      cursor = cursor.slice(1)
    }
    if (cursor.length > 0) {
      ctx.repoKey = cursor[0]
      if (cursor.length > 1) ctx.path = cursor.slice(1).join('/')
    }
    ctx.domain = 'explorer'
    return ctx
  }

  if (head === 'search') {
    ctx.domain = 'search'
    const q = new URLSearchParams(search).get('q')
    if (q) ctx.query = q
    return ctx
  }

  if (head === 'builds') {
    ctx.domain = 'builds'
    if (rest.length > 0) ctx.buildName = rest[0]
    if (rest.length > 1) ctx.buildNumber = rest[1]
    return ctx
  }

  if (head === 'bundles') {
    ctx.domain = 'bundles'
    if (rest.length > 0) ctx.bundleName = rest[0]
    if (rest.length > 1) ctx.bundleVersion = rest[1]
    return ctx
  }

  // —— Administration 域（/admin/*）——
  if (head === 'admin') {
    const sub = rest[0]
    if (sub === 'repositories') {
      ctx.domain = 'repos'
      const a = rest[1]
      if (a === 'new' || a === undefined) return ctx // 兼容重定向/列表缺省
      if (RCLASS.has(a)) {
        ctx.rclass = a
        return ctx // 列表页（/new 子径同归建表单坐标）
      }
      ctx.repoKey = a // :key（/edit 子径同归详情坐标）
      return ctx
    }
    if (sub === 'security') return { ...ctx, domain: 'security' }
    if (sub === 'governance') return { ...ctx, domain: 'governance' }
    if (sub === 'monitoring') return { ...ctx, domain: 'monitoring' }
    if (sub === 'general') return { ...ctx, domain: 'admin' }
    return { ...ctx, domain: 'admin' }
  }

  return ctx
}

/**
 * 上下文坐标徽章文本（「docker-prod/nginx/1.27」形态——总令 §十五 的
 * 上下文注入展示形）。repoKey/path 拼坐标链；无坐标域回落 domain 标签
 * 键（渲染层走 i18n，本函数返回 domain 键名而非成品文案）。
 */
export function aiContextCoord(ctx: AiContext): string | null {
  if (ctx.repoKey) {
    const tail = ctx.path ? `/${ctx.path}` : ''
    return `${ctx.repoKey}${tail}`
  }
  if (ctx.buildName) return ctx.buildNumber ? `${ctx.buildName}/${ctx.buildNumber}` : ctx.buildName
  if (ctx.bundleName) return ctx.bundleVersion ? `${ctx.bundleName}/${ctx.bundleVersion}` : ctx.bundleName
  if (ctx.query) return ctx.query
  return null
}
