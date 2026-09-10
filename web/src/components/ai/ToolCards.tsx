// AI 工具卡族（总令 §十五 消息渲染分层）：ToolCallCard（name+args 折叠）/
// ToolResultCard（结果形态）/ ConfirmCard（结构化确认 UI——参数表 + 双钮
// [取消][创建仓库]，确认动作回调抽象 = assistant-ui ToolCallMessagePartProps
// 的 addResult——渲染层是结果的事实源，真后端接入时换成 execute 回调）。
//
// 消费面 = MessagePrimitive.Parts 的 tools.by_name 分派（ChatPanel.tsx）：
//   query_storage_usage → StorageToolPart（ToolCallCard + ToolResultCard）
//   create_repository   → ConfirmCard（未决态）→ resolve 后 ToolResultCard
//   其余（Fallback）    → ToolCallCard（通用折叠形）
import type { ComponentPropsWithoutRef } from 'react'
import type { ToolCallMessagePartProps } from '@assistant-ui/react'

import type { CreateRepoToolResult, StorageToolResult } from '@/lib/ai/provider'
import { AI_TOOL_CREATE_REPO } from '@/lib/ai/provider'
import { cn } from '@/lib/utils'
import { tr } from '@/i18n'

const t = tr('ai')

/** 参数键值表（ConfirmCard 参数区与 ToolResultCard 复用） */
function ParamTable({ rows, testid }: { rows: Array<[string, string]>; testid?: string }) {
  return (
    <table className="w-full border-collapse text-dense" data-testid={testid}>
      <tbody>
        {rows.map(([k, v]) => (
          <tr key={k}>
            <th scope="row" className="w-[38%] border-b border-border px-2 py-1 text-left font-medium text-muted-foreground">
              {k}
            </th>
            <td className="border-b border-border px-2 py-1 font-mono break-all" lang="en">
              {v}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

/** args JSON → 稳定键序键值行 */
function argsRows(args: Record<string, unknown>): Array<[string, string]> {
  return Object.keys(args)
    .sort()
    .map((k) => [k, typeof args[k] === 'object' && args[k] !== null ? JSON.stringify(args[k]) : String(args[k])])
}

/**
 * ToolCallCard：工具调用折叠卡（name 徽标 + args 明细 <details> 折叠——
 * 总令 §十五 的 name+args 折叠形态）。已 resolve 时附结果形态摘要行。
 */
export function ToolCallCard({ toolName, args, result, className }: {
  toolName: string
  args: Record<string, unknown>
  result?: unknown
  className?: string
}) {
  const resolved = result !== undefined
  return (
    <div
      className={cn('ai-tool-call rounded-md border border-border bg-surface-1', className)}
      data-testid="ai-tool-call"
    >
      <div className="flex items-center gap-2 px-2.5 py-1.5">
        <span className="rounded-sm bg-secondary px-1.5 py-px font-mono text-aux" lang="en">
          {toolName}
        </span>
        <span className="text-aux text-muted-foreground">
          {resolved ? t('工具调用（已完成）') : t('工具调用')}
        </span>
      </div>
      <details className="border-t border-border px-2.5 py-1.5">
        <summary className="cursor-pointer select-none text-aux text-muted-foreground">{t('参数')}</summary>
        <div className="pt-1.5">
          <ParamTable rows={argsRows(args)} testid="ai-tool-call-args" />
        </div>
      </details>
    </div>
  )
}

/**
 * ToolResultCard：工具结果形态卡（演示面 = query_storage_usage 的
 * columns/rows 假表格；未知形态回落键值 JSON 折叠）。
 */
export function ToolResultCard({ result, isError, className }: { result: unknown; isError?: boolean; className?: string }) {
  const storage = result as Partial<StorageToolResult> | null
  const isTable = !!storage && Array.isArray(storage.columns) && Array.isArray(storage.rows)
  return (
    <div
      className={cn(
        'ai-tool-result rounded-md border bg-surface-1',
        isError ? 'border-destructive' : 'border-border',
        className,
      )}
      data-testid="ai-tool-result"
    >
      <div className="flex items-center gap-2 px-2.5 py-1.5">
        <span aria-hidden="true" className={cn('text-aux', isError ? 'text-destructive' : 'text-success')}>
          {isError ? '✗' : '✓'}
        </span>
        <span className="text-aux text-muted-foreground">{isError ? t('工具结果（错误）') : t('工具结果')}</span>
      </div>
      {isTable ? (
        <div className="overflow-x-auto px-2.5 pb-2">
          <table className="w-full border-collapse text-dense" data-testid="ai-tool-result-table">
            <thead>
              <tr>
                {(storage as StorageToolResult).columns.map((c) => (
                  <th key={c} scope="col" className="border-b border-border px-2 py-1 text-left font-semibold" lang="en">
                    {c}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(storage as StorageToolResult).rows.map((row, i) => (
                <tr key={i}>
                  {row.map((cell, j) => (
                    <td key={j} className="border-b border-border px-2 py-1 font-mono" lang="en">
                      {cell}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
          <p className="mt-1.5 text-aux text-muted-foreground">{(storage as StorageToolResult).unit}</p>
        </div>
      ) : (
        <div className="px-2.5 pb-2">
          <details className="pt-0.5">
            <summary className="cursor-pointer select-none text-aux text-muted-foreground">{t('结果数据')}</summary>
            <pre className="mt-1 overflow-x-auto rounded-sm bg-surface-2 p-2 font-mono text-aux" lang="en">
              {JSON.stringify(result, null, 2)}
            </pre>
          </details>
        </div>
      )}
    </div>
  )
}

/** 双钮动作钮（ConfirmCard 专用——形态对齐 Artifactory 确认对话） */
function ActionButton({ variant, ...props }: ComponentPropsWithoutRef<'button'> & { variant: 'primary' | 'ghost' }) {
  return (
    <button
      type="button"
      className={cn(
        'rounded-md px-3 py-1.5 text-dense font-medium transition-colors focus-visible:outline-2 focus-visible:outline-ring',
        variant === 'primary'
          ? 'bg-primary text-primary-foreground hover:opacity-90'
          : 'border border-border text-foreground hover:bg-accent',
      )}
      {...props}
    />
  )
}

/**
 * ConfirmCard：结构化确认 UI（总令 §十五 的 [Cancel][Create Repository]
 * 形态）——参数表 + 双钮。
 *
 * 确认动作回调抽象：本组件不持有任何业务语义——确认/取消经 props.addResult
 * （assistant-ui 渲染层结果写口）回传结构化结果；create_repository 的
 * 结果形态由 mock provider 定义（{ok, cancelled, …}），真后端接入时同形
 * 换成 API 调用 + 结果回填。tool-call part 字段以 spread 形直入 props
 * （assistant-ui MessagePrimitive.Parts 的 tools 分派契约）。
 */
export function ConfirmCard({ toolName, args, addResult }: ToolCallMessagePartProps) {
  const argMap = (args ?? {}) as Record<string, unknown>
  const repoKey = String(argMap.repoKey ?? '')
  const rows = argsRows(argMap)
  const accept = () => {
    addResult({
      ok: true,
      repoKey,
      rclass: String(argMap.rclass ?? 'local'),
      packageType: String(argMap.packageType ?? 'generic'),
    } satisfies CreateRepoToolResult)
  }
  const cancel = () => {
    addResult({ ok: false, cancelled: true } satisfies CreateRepoToolResult)
  }
  return (
    <div
      className="ai-confirm rounded-md border border-border bg-surface-1"
      data-testid="ai-confirm"
      role="group"
      aria-label={t('确认创建仓库')}
    >
      <div className="flex items-center gap-2 px-2.5 py-1.5">
        <span className="rounded-sm bg-info px-1.5 py-px font-mono text-aux text-info-foreground" lang="en">
          {toolName}
        </span>
        <span className="text-aux text-muted-foreground">{t('待确认——演示流程，确认后本地收账零网络')}</span>
      </div>
      <div className="px-2.5 pb-1">
        <ParamTable rows={rows} testid="ai-confirm-params" />
      </div>
      <div className="flex justify-end gap-2 px-2.5 pb-2.5 pt-1.5">
        <ActionButton variant="ghost" data-testid="ai-confirm-cancel" onClick={cancel}>
          {t('取消')}
        </ActionButton>
        <ActionButton variant="primary" data-testid="ai-confirm-accept" onClick={accept}>
          {toolName === AI_TOOL_CREATE_REPO ? t('创建仓库') : t('确认')}
        </ActionButton>
      </div>
    </div>
  )
}

/**
 * StorageToolPart：query_storage_usage 的分派渲染（ToolCallCard 折叠 +
 * ToolResultCard 表格——同 part 内两卡纵排）。
 */
export function StorageToolPart({ toolName, args, result, isError }: ToolCallMessagePartProps) {
  return (
    <div className="flex flex-col gap-2 py-1">
      <ToolCallCard toolName={toolName} args={(args ?? {}) as Record<string, unknown>} result={result} />
      {result !== undefined && <ToolResultCard result={result} isError={isError} />}
    </div>
  )
}

/**
 * ConfirmToolPart：create_repository 的分派渲染——未决 = ConfirmCard；
 * 已 resolve（确认/取消后的重入渲染）= ToolCallCard + 结果摘要行。
 */
export function ConfirmToolPart(props: ToolCallMessagePartProps) {
  const { toolName, args, result } = props
  if (result === undefined) return <ConfirmCard {...props} />
  const res = result as CreateRepoToolResult
  return (
    <div className="flex flex-col gap-2 py-1">
      <ToolCallCard toolName={toolName} args={(args ?? {}) as Record<string, unknown>} result={result} />
      <div
        className={cn(
          'rounded-md border px-2.5 py-1.5 text-dense',
          res.cancelled ? 'border-border text-muted-foreground' : 'border-border text-success',
        )}
        data-testid="ai-confirm-result"
      >
        {res.cancelled
          ? t('已取消（未发出请求）')
          : t('已确认：{v1}（演示收账）', { v1: res.repoKey ?? '' })}
      </div>
    </div>
  )
}

/** 通用 Fallback（未注册工具名——折叠形 ToolCallCard） */
export function ToolFallbackPart({ toolName, args, result, isError }: ToolCallMessagePartProps) {
  return (
    <div className="py-1">
      <ToolCallCard
        toolName={toolName}
        args={(args ?? {}) as Record<string, unknown>}
        result={result}
      />
      {result !== undefined && toolName !== AI_TOOL_CREATE_REPO && (
        <div className="mt-2">
          <ToolResultCard result={result} isError={isError} />
        </div>
      )}
    </div>
  )
}
