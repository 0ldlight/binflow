// 一键拷贝（新栈 CopyButton）：值不截断纪律（复制完整值——显示可截断，
// 剪贴板恒全值）；.copy-btn 类 = e2e 钩子（§7.3 家族锚，原样保留）；
// clipboard API + execCommand 降级（非安全上下文）。
import { useCallback, useRef, useState } from 'react'

import { cn } from '@/lib/utils'
import { tr } from '@/i18n'

const t = tr('common')

export function CopyButton({
  value,
  label,
  className,
}: {
  value: string
  /** aria-label 语境（如「仓库 key my-repo」） */
  label: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | null>(null)

  const copy = useCallback(
    (e: React.MouseEvent | React.KeyboardEvent) => {
      // 隔离层：宿主行/单元格的点击导航不被触发（旧 CopyButton 同款纪律）
      e.stopPropagation()
      const done = () => {
        setCopied(true)
        if (timer.current) window.clearTimeout(timer.current)
        timer.current = window.setTimeout(() => setCopied(false), 1500)
      }
      if (navigator.clipboard?.writeText) {
        void navigator.clipboard.writeText(value).then(done, () => fallbackCopy(value, done))
      } else {
        fallbackCopy(value, done)
      }
    },
    [value],
  )

  return (
    <button
      type="button"
      className={cn('copy-btn', className)}
      aria-label={t('复制 {label}', { label })}
      title={copied ? t('已复制') : t('复制 {label}', { label })}
      onClick={copy}
      onKeyDown={(e) => {
        if (e.key === ' ' || e.key === 'Enter') {
          e.preventDefault()
          copy(e)
        }
      }}
    >
      <span aria-hidden="true" className="font-mono text-[12px]">
        {copied ? '✓' : '⧉'}
      </span>
    </button>
  )
}

/** execCommand 降级（非安全上下文 / clipboard API 不可用） */
function fallbackCopy(value: string, done: () => void): void {
  try {
    const ta = document.createElement('textarea')
    ta.value = value
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    done()
  } catch {
    // 剪贴板整体不可用：无成功态
  }
}
