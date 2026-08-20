import { useState } from 'react'

// 一键拷贝（P2：一切标识符可复制；mono 值 = 拷贝候选）。拷贝的是
// 完整值——展示可以截断，拷贝不许截断（console-ux §7.3）。

export function CopyButton({ value, label }: { value: string; label: string }) {
  const [done, setDone] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      // 剪贴板 API 不可用（非安全上下文等）：回落到老式选中拷贝。
      const ta = document.createElement('textarea')
      ta.value = value
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
    }
    setDone(true)
    window.setTimeout(() => setDone(false), 1500)
  }

  return (
    <button
      type="button"
      className="copy-btn mono"
      aria-label={`复制 ${label}`}
      title={`复制 ${label}`}
      onClick={() => void copy()}
    >
      {done ? '已复制' : '⧉'}
    </button>
  )
}
