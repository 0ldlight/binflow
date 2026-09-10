// AI mock provider（frontend-rewrite-architecture §1 AI 基座行 + §10 P5 门
// + §11 R6）：本地确定性假响应引擎——**组件契约：禁真请求**。
//
// 边界（audit 盲区② 裁定，写入组件契约）：Go 侧无任何 LLM/chat 端点，
// 本 adapter 的 run() 是纯本地状态机——不 fetch、不 WebSocket、不读网络
// 配置；任何后续真后端接入 = 新票换 adapter 实现，消费面（ChatPanel/
// 卡片族/锚）零改动。e2e 以路由拦截计数断言零网络（发送消息期间对
// /binflow/api 面零请求）。
//
// 行为表（确定性：同输入 → 同输出序列）：
//  1. echo（缺省臂）——上下文感知样板回显：解析用户消息首帧的上下文
//     哨兵行（AI_CONTEXT_MARK），回显坐标链（「当前在 docker-prod/
//     nginx/1.27」）+ 演示声明。
//  2. 存储占用演示（关键词：存储/占用/storage）——emit query_storage_usage
//     tool-call（**携带内联 result**：假表格数据）→ ToolResultCard 渲染；
//     后随 markdown 表格文本与小结。无人工介入，run 直落 complete。
//  3. 创建仓库演示（关键词：创建仓库/建仓/create repo）——先文本前言，
//     再 emit create_repository tool-call（**无 result**）+ 终态
//     requires-action/tool-calls → runtime 暂停（unstable_humanToolNames）
//     → ConfirmCard 渲染参数表 + [取消][创建仓库] 双钮；确认 =
//     addResult({ok:true,…}) → runtime 以含 result 的消息重入本 adapter
//     → yield 成功文案；取消 = addResult({ok:false, cancelled:true}) →
//     yield 取消文案（同一重入路径）。
//
// 流式形态：文本以数帧增量 yield（typewriter 观感，帧间 12ms——时延不
// 影响确定性断言，e2e 只等终态文本）。
//
// 文案 = i18n ai 域（zh-as-key）——响应模板是用户可见文案，双语与闸纪律
// （零硬编码 CJK）与 UI 面同守。
import type { ChatModelAdapter, ChatModelRunResult, ThreadMessage } from '@assistant-ui/react'

import type { AiContext } from './context'
import { aiContextCoord } from './context'
import { tr } from '@/i18n'

const t = tr('ai')

/** 演示工具名（锚与 ToolResultCard/ConfirmCard 的分派键） */
export const AI_TOOL_STORAGE = 'query_storage_usage'
export const AI_TOOL_CREATE_REPO = 'create_repository'

/** 上下文哨兵前缀（ChatPanel 注入消息首帧；adapter 解析后剔除） */
export const AI_CONTEXT_MARK = '[bf-ctx]'

/** 帧间时延（流式观感；e2e 终态断言不受影响） */
const FRAME_DELAY_MS = 12

export interface StorageToolResult {
  columns: string[]
  rows: string[][]
  unit: string
}

export interface CreateRepoToolResult {
  ok: boolean
  cancelled?: boolean
  repoKey?: string
  packageType?: string
  rclass?: string
}

interface ParsedUserFrame {
  context: Partial<AiContext> | null
  text: string
}

/** 解析用户消息首帧：`[bf-ctx] {json}` 哨兵行 + 正文（无哨兵 = 裸文本） */
export function parseContextSentinel(raw: string): ParsedUserFrame {
  const idx = raw.indexOf('\n')
  const first = idx === -1 ? raw : raw.slice(0, idx)
  const rest = idx === -1 ? '' : raw.slice(idx + 1)
  if (first.startsWith(AI_CONTEXT_MARK)) {
    try {
      const json = JSON.parse(first.slice(AI_CONTEXT_MARK.length).trim()) as Partial<AiContext>
      return { context: json && typeof json === 'object' ? json : null, text: rest }
    } catch {
      // 哨兵损坏（非 JSON）：整段按裸文本处理（诚实降级，不吞）
      return { context: null, text: raw }
    }
  }
  return { context: null, text: raw }
}

/** 坐标链文本（上下文徽章/回显同源）：repoKey/path、build、query */
export function contextCoordText(ctx: Partial<AiContext> | null): string | null {
  if (!ctx) return null
  return aiContextCoord(ctx as AiContext)
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/** 演示存储表（repoKey 在场则行列内引用——确定性派生） */
function storageResult(repoKey?: string): StorageToolResult {
  const key = repoKey ?? 'example-local'
  return {
    columns: ['repoKey', 'packageType', 'objects', 'used'],
    rows: [
      [key, 'generic', '1,284', '2.4 GiB'],
      [`${key}-cache`, 'generic', '366', '412 MiB'],
      ['docker-prod', 'docker', '8,921', '6.8 GiB'],
    ],
    unit: t('演示数据（mock provider 本地生成）'),
  }
}

const isStorageIntent = (s: string) => /存储|占用|storage/i.test(s)
const isCreateRepoIntent = (s: string) => /创建仓库|建仓|create\s+(a\s+)?repo/i.test(s)
/** 错误态演示臂（四态之错误：adapter 抛错 → incomplete/error → 错误卡+重试） */
const isFailureIntent = (s: string) => /模拟错误|simulate\s+error/i.test(s)

/** 最后一条消息文本（user 消息取拼接文本；assistant 忽略） */
function lastUserText(messages: readonly ThreadMessage[]): string | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (m.role !== 'user') continue
    return m.content
      .filter((p): p is { type: 'text'; text: string } => p.type === 'text')
      .map((p) => p.text)
      .join('\n')
  }
  return null
}

/**
 * 重入臂探测：addToolResult 后 runtime 以同一 assistant 消息重入本
 * adapter——此时 options.messages 只含 [user]（LocalRuntime 的
 * performRoundtrip 以 parentId 取链，见 local-thread-runtime-core 实测），
 * resolved 结果只能从 unstable_getMessage()（进行中消息的活引用）读。
 */
function resolvedCreateRepo(self: ThreadMessage | null): CreateRepoToolResult | null {
  if (!self || self.role !== 'assistant') return null
  const part = self.content.find((p) => p.type === 'tool-call' && p.toolName === AI_TOOL_CREATE_REPO)
  if (part && part.type === 'tool-call' && part.result !== undefined) {
    return part.result as CreateRepoToolResult
  }
  return null
}

/** 逐帧 yield 文本增量（cumulative content 契约——每次携带全量文本；
 * baseline = 已落的非文本 part（tool-call 等），帧序保持在其后） */
async function* yieldTextFrames<T extends object>(baseline: readonly T[], full: string, signal: AbortSignal) {
  const frames = chunkText(full)
  let acc = ''
  for (const frame of frames) {
    if (signal.aborted) return
    acc += frame
    await sleep(FRAME_DELAY_MS)
    yield { content: [...baseline, { type: 'text' as const, text: acc }] }
  }
}

/** markdown 文本按 2~3 段切帧（粗粒度——避免逐字符帧的冗长） */
function chunkText(text: string): string[] {
  const lines = text.split('\n')
  const out: string[] = []
  let buf: string[] = []
  for (const line of lines) {
    buf.push(line)
    if (buf.length >= 2 && line === '') {
      out.push(buf.join('\n'))
      buf = []
    }
  }
  if (buf.length) out.push(buf.join('\n'))
  return out.length ? out : [text]
}

/** 域显示名（ChatPanel 头部徽章与 echo 回显共用的词表） */
export const AI_DOMAIN_WORD: Record<string, string> = {
  explorer: t('制品浏览器'),
  repos: t('仓库管理'),
  search: t('搜索'),
  builds: t('构建'),
  bundles: t('发行集'),
  dashboard: t('仪表盘'),
  profile: t('个人档案'),
  security: t('安全'),
  governance: t('治理'),
  monitoring: t('监控'),
  admin: t('常规管理'),
  console: t('控制台'),
}

/** 上下文感知样板（echo 臂——「当前在 …」形态） */
function echoReply(text: string, coord: string | null, domain: string | undefined): string {
  const domainWord = domain ? (AI_DOMAIN_WORD[domain] ?? domain) : null
  const where = coord
    ? t('当前在 `{v1}`（{v2} 域）', { v1: coord, v2: domainWord ?? domain ?? 'console' })
    : t('当前在 {v1} 域（无具体坐标）', { v1: domainWord ?? domain ?? 'console' })
  // 段落拼接走 join('\n\n')——模板字面量零 CJK 字符（assert-i18n 闸纪律）
  return [
    where,
    t('收到：「{v1}」', { v1: text.slice(0, 120) }),
    t('这是 **本地演示响应**（mock provider——AI 基座无后端端点）。试试：'),
    `- ${t('「查一下存储占用」——演示工具调用（tool-call + 结果表格）')}`,
    `- ${t('「帮我创建仓库」——演示结构化确认（参数表 + 确认/取消）')}`,
  ].join('\n\n')
}

function storageReply(result: StorageToolResult, coord: string | null, repoKey?: string): string {
  const head = `| ${result.columns.join(' | ')} |`
  const sep = `| ${result.columns.map(() => '---').join(' | ')} |`
  const rows = result.rows.map((r) => `| ${r.join(' | ')} |`).join('\n')
  const prefix = coord ? t('已按当前坐标 `{v1}`，', { v1: coord }) : ''
  const aql = repoKey ? `items.find({"repo":"${repoKey}"}).include("stat")` : 'items.find().include("stat")'
  return `${prefix}${t('调用工具 `query_storage_usage`，结果如下（{v1}）：', { v1: result.unit })}

${head}
${sep}
${rows}

${t('等价 AQL（真实实现将下发服务端执行）：')}

\`\`\`aql
${aql}
\`\`\`

${t('演示表格由本地 mock 生成——真实实现需服务端存储统计端点（当前无此 API，不虚构）。')}`
}

function createRepoPreamble(): string {
  return `${t('好的——创建仓库前请确认以下参数（**演示流程**：确认后本地收账，不发任何请求）：')}\n`
}

function createRepoSuccess(res: CreateRepoToolResult): string {
  return `${t('仓库 `{v1}` 已创建（**演示**：rclass={v2}，packageType={v3}）。', {
    v1: res.repoKey ?? 'example-local',
    v2: res.rclass ?? 'local',
    v3: res.packageType ?? 'generic',
  })}

${t('确认动作由本地 mock 直接收账——真实实现将在这里调用建仓 API 并回填结果；后端端点到位前不虚构任何请求。')}`
}

/**
 * mock ChatModelAdapter——run 为 async generator（LocalRuntime 逐帧消费）。
 * 零网络：函数体内无任何 I/O，仅 setTimeout 帧间隔。
 */
export const mockChatAdapter: ChatModelAdapter = {
  async *run(options): AsyncGenerator<ChatModelRunResult, void> {
    const signal = options.abortSignal

    // —— 重入臂：create_repository 已被 ConfirmCard resolve（结果在进行中
    //    消息上——unstable_getMessage 活引用，非 options.messages）——
    const resolved = resolvedCreateRepo(options.unstable_getMessage?.() ?? null)
    if (resolved) {
      if (resolved.cancelled) {
        yield* yieldTextFrames([], t('已取消创建仓库（未发出任何请求——演示流程零网络）。'), signal)
        return
      }
      yield* yieldTextFrames([], createRepoSuccess(resolved), signal)
      return
    }

    const raw = lastUserText(options.messages) ?? ''
    const { context, text } = parseContextSentinel(raw)
    const coord = contextCoordText(context)
    const domain = context?.domain
    const repoKey = context?.repoKey

    // —— 存储占用演示：tool-call + 内联 result（直落 complete）——
    if (isStorageIntent(text)) {
      const toolCallId = `demo-storage-${options.messages.length}`
      const result = storageResult(repoKey)
      const args = { scope: repoKey ? `repo:${repoKey}` : 'all', unit: 'binary' }
      const part = {
        type: 'tool-call' as const,
        toolCallId,
        toolName: AI_TOOL_STORAGE,
        args,
        argsText: JSON.stringify(args),
        result,
      }
      // 帧序：前言 → tool-call（含结果）→ 全量结果文本
      const preface = t('正在查询存储占用…')
      yield { content: [{ type: 'text' as const, text: preface }] }
      await sleep(FRAME_DELAY_MS)
      if (signal.aborted) return
      const withTool = [{ type: 'text' as const, text: preface }, part]
      yield { content: withTool }
      yield* yieldTextFrames(withTool, storageReply(result, coord, repoKey), signal)
      return
    }

    // —— 创建仓库演示：未 resolve 的 tool-call + requires-action 暂停 ——
    if (isCreateRepoIntent(text)) {
      const toolCallId = `demo-create-${options.messages.length}`
      const args = {
        repoKey: repoKey ?? 'demo-local',
        rclass: 'local',
        packageType: 'generic',
      }
      const part = {
        type: 'tool-call' as const,
        toolCallId,
        toolName: AI_TOOL_CREATE_REPO,
        args,
        argsText: JSON.stringify(args),
      }
      yield* yieldTextFrames([], createRepoPreamble(), signal)
      if (signal.aborted) return
      await sleep(FRAME_DELAY_MS)
      // 终帧：前言 + 未决 tool-call + requires-action（humanTool 暂停点）
      yield {
        content: [
          { type: 'text' as const, text: createRepoPreamble() },
          part,
        ],
        status: { type: 'requires-action' as const, reason: 'tool-calls' as const },
      }
      return
    }

    // —— 错误态演示臂：抛错 → runtime 置 incomplete/error → 错误卡 + 重试 ——
    if (isFailureIntent(text)) {
      throw new Error(t('演示失败（模拟错误态——本地抛错，非网络）'))
    }

    // —— echo 臂（缺省）——
    yield* yieldTextFrames([], echoReply(text.trim(), coord, domain), signal)
  },
}
