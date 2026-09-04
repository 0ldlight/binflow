import { apiJSON } from '../../lib/api'

// AQL 模式支持库（T-419，FR-135.1——M15-SPLIT §1.2 AC2）：
//
// - wire 类型 + 执行入口：只消费 T-415 既有端点 `POST /api/search/aql`
//   （text/plain body = 查询文本，envelope results[] + range{...}——aql.md
//   §1/§3）。零新端点：连 ?compact 都不用（单一消费者，形态无对齐义务；
//   pretty 形态的 `" : "` 分隔仍是合法 JSON，统一层直接反序列化）。
// - 尾缀链重写：分页（.offset）与表头排序（.sort）的交互 = **改写编辑器
//   文本里的尾缀链段**再重放——查询文本是唯一事实源，UI 不在旁路攒影子
//   状态（排序/翻页后用户看得见查询变了什么）。链序 include→transitive→
//   sort→offset→limit→distinct（aql.md §2.5）由重写器按序归位，不会因为
//   插入段制造链序 400。

/** AQL 结果行（aql.md §2.2/§3.3——投影由查询的 include 决定，全部字段
 *  可缺省：include 未列出的域字段从输出剔除。FE 对缺省字段如实呈现 —，
 *  不伪造值）。 */
export interface AQLRow {
  repo?: string
  /** 父目录路径（'' = 仓根——与 basic 模式 E-09 的全路径不同形） */
  path?: string
  name?: string
  type?: string
  size?: number
  created?: string
  created_by?: string
  modified?: string
  updated?: string
  depth?: number
  actual_md5?: string
  actual_sha1?: string
  sha256?: string
  /** virtual 仓查询时隐式输出的逻辑字段（aql.md §7-3） */
  virtual_repos?: string[]
  /** property 投影聚合的嵌套成员（aql.md §3.3） */
  properties?: { key: string; value: string }[]
}

/** range 尾对象（aql.md §3.2——流式语义：end_pos/total = 本页行数，
 *  非全量计数；limit 仅查询声明时回显；notification = K63 截断通告）。 */
export interface AQLRange {
  start_pos: number
  end_pos: number
  total: number
  limit?: number
  notification?: string
}

export interface AQLResult {
  results: AQLRow[]
  range: AQLRange
}

/** K63 引擎结果上限（internal/search ResultCap = 1000）——无 .limit()
 *  查询的「是否还有下一页」判定口径：未声明 limit 时本页满 1000 行或
 *  带 notification 即认为还有。 */
export const AQL_RESULT_CAP = 1000

/** 执行一条 AQL 查询。非 2xx 由统一层折成 ApiError（errors[] 首条
 *  message = 服务端逐字文案——400 语法错 E1 / 408 超时 / 429 并发满，
 *  内联呈现层直接消费 ApiError.message）。 */
export function runAQL(query: string, signal?: AbortSignal): Promise<AQLResult> {
  return apiJSON<AQLResult>('/search/aql', { method: 'POST', rawBody: query, signal })
}

/** 从路径形态推导协议语义副行（§4.8：让工程师不点进去就能判断「是不是它」）。
 *  两模式共用（basic 的 E-09 全路径 / AQL 的 path+name 拼接），故落在本
 *  支持库而非 SearchPage——AqlPanel 与 SearchPage 互不 import。 */
export function semanticOf(path: string): string | null {
  const segs = path.replace(/^\//, '').split('/')
  const file = segs[segs.length - 1]
  if (file === 'maven-metadata.xml' && segs.length >= 3) {
    return `maven-metadata · ${segs.slice(0, segs.length - 2).join(':')}:${segs[segs.length - 2]}`
  }
  if (segs.length >= 4) {
    const groupId = segs.slice(0, segs.length - 3).join('.')
    const artifactId = segs[segs.length - 3]
    const version = segs[segs.length - 2]
    if (file.startsWith(`${artifactId}-${version}`)) {
      return `maven GAV ${groupId}:${artifactId}:${version}`
    }
  }
  return null
}

// ---- T-449（FR-144.6 断言反转②）共享件 ---------------------------------------
//
// 结果表三件套归一到本支持库（两模式同一张网格——「列框架收敛」）：

/** ISO 时间 → `dd-MM-yy HH:mm:ss +ZZZZ`（Artifactory 结果表对位——
 *  parity B-3.15：浏览器本地时区 + 显式偏移后缀，如 `02-09-26 08:37:57
 *  +0800`；不可解析值如实返回 null 由调用方呈现 —）。 */
export function formatStamp(iso: string | null | undefined): string | null {
  if (!iso) return null
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  const p2 = (n: number) => String(n).padStart(2, '0')
  const off = -d.getTimezoneOffset()
  const sign = off >= 0 ? '+' : '-'
  const abs = Math.abs(off)
  const zone = `${sign}${p2(Math.floor(abs / 60))}${p2(abs % 60)}`
  return `${p2(d.getDate())}-${p2(d.getMonth() + 1)}-${String(d.getFullYear()).slice(2)} ${p2(d.getHours())}:${p2(d.getMinutes())}:${p2(d.getSeconds())} ${zone}`
}

/** 结果行 → 跨仓树深链（K67-3 路径段规范形：文件 = 路径末段，?focus=
 *  退役——T-434 兼容重定向维持一轮，本函数是发射端翻新后的唯一拼法，
 *  两模式共用；缺 repo 或 name 的投影行返回 null = 不给注定 404 的链接）。 */
export function treeUrl(repo: string | null | undefined, dir: string, name: string | null | undefined): string | null {
  if (!repo || !name) return null
  const segs = [...dir.split('/'), name].filter((s) => s !== '')
  return `/artifacts/${[repo, ...segs].map((s) => encodeURIComponent(s)).join('/')}`
}

// ---- 尾缀链重写（分页/排序交互的载体） --------------------------------------

/** 合法尾缀链段（链序即数组序，aql.md §2.5）。find 不在其中——
 *  `items.find({…})` 同为 `.name(body)` 形态，白名单是拆链的止步条件。 */
const TAIL_ORDER = ['include', 'transitive', 'sort', 'offset', 'limit', 'distinct'] as const
type TailName = (typeof TAIL_ORDER)[number]

interface TailSeg {
  name: string
  body: string
}

/** 段体不含括号（sort 的 {"$asc":["f"]} / include 的 "@k" 皆然）——
 *  `\s*$` 容忍尾随空白（多行查询习惯）。 */
const TAIL_SEG_RE = /\.(include|transitive|sort|offset|limit|distinct)\(([^()]*)\)\s*$/

/**
 * 拆尾缀链：自串尾逐段弹出白名单链段。病态输入（段体内带括号，如
 * `include("a(b)")`）会让该段与其前段弹不出——重写退化为「整段追加」，
 * 若与服务端既有段重复即 400 内联呈现（诚实失败，不静默吞掉用户查询）。
 */
export function splitTail(query: string): { head: string; segs: TailSeg[] } {
  let rest = query
  const segs: TailSeg[] = []
  for (;;) {
    const m = TAIL_SEG_RE.exec(rest)
    if (!m) break
    segs.unshift({ name: m[1], body: m[2] })
    rest = rest.slice(0, m.index)
  }
  return { head: rest, segs }
}

/** 置换一个链段（body=null = 移除该段），按链序归位拼回。 */
export function withTailClause(query: string, name: TailName, body: string | null): string {
  const { head, segs } = splitTail(query)
  const kept = segs.filter((s) => s.name !== name)
  const merged = body === null ? kept : [...kept, { name, body }]
  merged.sort(
    (a, b) => TAIL_ORDER.indexOf(a.name as TailName) - TAIL_ORDER.indexOf(b.name as TailName),
  )
  return head + merged.map((s) => `.${s.name}(${s.body})`).join('')
}

/** 读当前链段 body（无该段 = null）。 */
export function tailClause(query: string, name: TailName): string | null {
  return splitTail(query).segs.find((s) => s.name === name)?.body ?? null
}

export interface SortState {
  field: string
  dir: 'asc' | 'desc'
}

/** 解析 `.sort({"$asc":["field"]})`——多字段只认首字段（表头交互是
 *  单字段语义；用户手写的多字段排序在重写时保留原段不被触碰）。 */
export function sortState(query: string): SortState | null {
  const body = tailClause(query, 'sort')
  if (body === null) return null
  const m = /"\$(asc|desc)"\s*:\s*\[\s*"([^"]+)"/.exec(body)
  return m ? { dir: m[1] as 'asc' | 'desc', field: m[2] } : null
}

/** 表头点击的三态轮转：无 → asc → desc → 无（第三击摘除子句）。 */
export function nextSortClause(current: SortState | null, field: string): string | null {
  if (!current || current.field !== field) return `{"$asc":["${field}"]}`
  if (current.dir === 'asc') return `{"$desc":["${field}"]}`
  return null
}
