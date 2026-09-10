// AI 消息 markdown 渲染层（总令 §十五 消息渲染分层之一）：react-markdown
// + remark-gfm——标题/列表/粗斜体/行内码 + **GFM 表格** + 围栏代码块
// （mono + 一键拷贝——digest/checksum/路径同款纪律）。
//
// XSS 面：react-markdown 默认不透传原始 HTML（escape-by-default）——mock
// 文案与未来真模型输出均无注入面；不启 rehype-raw。
import type { ComponentPropsWithoutRef } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { CopyButton } from '@/components/layout/copy-button'
import { cn } from '@/lib/utils'
import { tr } from '@/i18n'

const t = tr('ai')

function extractText(node: React.ReactNode): string {
  if (node === null || node === undefined || node === false || node === true) return ''
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(extractText).join('')
  if (typeof node === 'object' && 'props' in (node as unknown as Record<string, unknown>)) {
    return extractText((node as unknown as { props?: { children?: React.ReactNode } }).props?.children)
  }
  return ''
}

/** 围栏代码块：mono + 拷贝钮（值不截断纪律——剪贴板恒全量） */
function CodeShell({ text, children }: { text: string; children: React.ReactNode }) {
  return (
    <div className="ai-code-shell group relative my-2 overflow-hidden rounded-md border border-border bg-surface-2" data-testid="ai-code-block">
      <div className="flex items-center justify-end px-1 pt-1">
        {/* e2e 锚：代码块拷贝钮（.copy-btn 家族类同源，锚挂宿主 span） */}
        <span data-testid="ai-copy-code">
          <CopyButton
            value={text}
            label={t('代码块')}
            className="rounded-sm px-1.5 py-0.5 text-muted-foreground hover:bg-surface-3 hover:text-foreground"
          />
        </span>
      </div>
      <pre className="overflow-x-auto px-3 pb-2 pt-0 font-mono text-dense leading-relaxed">
        <code>{children}</code>
      </pre>
    </div>
  )
}

/** 行内码：mono + 弱底 */
function InlineCode({ children, className, ...rest }: ComponentPropsWithoutRef<'code'>) {
  return (
    <code className={cn('rounded-sm bg-surface-2 px-1 py-px font-mono text-[0.92em]', className)} {...rest}>
      {children}
    </code>
  )
}

/** code 分派器：围栏块（children 含换行）→ CodeShell；行内 → InlineCode */
function Code({ className, children, ...rest }: ComponentPropsWithoutRef<'code'>) {
  const text = extractText(children)
  if (text.includes('\n') || /language-/.test(className ?? '')) {
    return <CodeShell text={text.replace(/\n$/, '')}>{children}</CodeShell>
  }
  return (
    <InlineCode className={className} {...rest}>
      {children}
    </InlineCode>
  )
}

/** markdown 文本渲染（消息 text part 的渲染器） */
export function MarkdownText({ text, className }: { text: string; className?: string }) {
  return (
    <div className={cn('ai-markdown text-dense leading-relaxed', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          code: Code,
          pre: ({ children }) => <>{children}</>, // 围栏壳由 CodeShell 承载
          table: ({ children }) => (
            <div className="my-2 overflow-x-auto">
              <table className="w-full border-collapse text-dense" data-testid="ai-md-table">
                {children}
              </table>
            </div>
          ),
          th: ({ children }) => (
            <th className="border-b border-border px-2 py-1 text-left font-semibold">{children}</th>
          ),
          td: ({ children }) => <td className="border-b border-border px-2 py-1 align-top">{children}</td>,
          p: ({ children }) => <p className="my-1.5 first:mt-0 last:mb-0">{children}</p>,
          ul: ({ children }) => <ul className="my-1.5 list-disc pl-5">{children}</ul>,
          ol: ({ children }) => <ol className="my-1.5 list-decimal pl-5">{children}</ol>,
          li: ({ children }) => <li className="my-0.5">{children}</li>,
          h1: ({ children }) => <h3 className="mt-3 mb-1 text-h3 font-semibold first:mt-0">{children}</h3>,
          h2: ({ children }) => <h3 className="mt-3 mb-1 text-h3 font-semibold first:mt-0">{children}</h3>,
          h3: ({ children }) => <h4 className="mt-2 mb-1 text-dense font-semibold first:mt-0">{children}</h4>,
          a: ({ children, href }) => (
            <a href={href} className="text-primary hover:underline" target="_blank" rel="noopener noreferrer">
              {children}
            </a>
          ),
          blockquote: ({ children }) => (
            <blockquote className="my-2 border-l-2 border-border pl-3 text-muted-foreground">{children}</blockquote>
          ),
          hr: () => <hr className="my-3 border-border" />,
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
