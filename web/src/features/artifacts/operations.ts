// 制品操作面（P2 解锁项——capability matrix #12）：api/copy、api/move
// 的 FE 消费（0 调用 → Explorer 上下文菜单 + 多选批量动作）。
//
// 契约（internal/httpapi/operations.go / docs/reverse/repo-operations.md）：
//   POST /binflow/api/{copy,move}/{srcRepo}[/{srcPath}]?to=/{targetRepo}[/{targetPath}]
//   query：to（必填）/ dry=0|1 / failFast=0|1（suppressLayouts 默认 1）
//   200 → {messages: [{level, message}]}（level = INFO/WARN/ERROR 族）
//   community 档 → 403 + X-Binflow-License-Required（license 门——UI 侧
//   如实呈现服务端 message，不吞不译）
import { apiJSON } from '@/lib/api'

export interface CopyMoveMessage {
  level: string
  message: string
}

export interface CopyMoveOutcome {
  /** HTTP 状态（200 = 聚合成功；409 = 部分失败等——messages 仍是主体） */
  status: number
  messages: CopyMoveMessage[]
}

/** 一次 copy/move（或 dry-run 预演）。to 形如 `/<targetRepo>[/<targetPath>]` */
export async function copyOrMove(
  op: 'copy' | 'move',
  srcRepo: string,
  srcPath: string,
  to: string,
  opts: { dry?: boolean; failFast?: boolean; signal?: AbortSignal } = {},
): Promise<CopyMoveOutcome> {
  const params = new URLSearchParams()
  params.set('to', to)
  if (opts.dry) params.set('dry', '1')
  if (opts.failFast) params.set('failFast', '1')
  // 路径逐段编码（与内容面同文法——字面拼写直达）
  const enc = [srcRepo, ...srcPath.split('/').filter(Boolean)].map((s) => encodeURIComponent(s)).join('/')
  const body = await apiJSON<{ messages?: CopyMoveMessage[] }>(`/${op}/${enc}?${params.toString()}`, {
    method: 'POST',
    signal: opts.signal,
  })
  return { status: 200, messages: body.messages ?? [] }
}
