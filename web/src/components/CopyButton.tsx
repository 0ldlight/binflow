import { useState } from 'react'
import IconButton from '@mui/material/IconButton'
import Tooltip from '@mui/material/Tooltip'
import { tr } from '../i18n'

const t = tr('console')

// 一键拷贝（P2：一切标识符可复制；mono 值 = 拷贝候选）。拷贝的是
// 完整值——展示可以截断，拷贝不许截断（console-ux §7.3）。
// T-344 批 B：button.copy-btn 文字钮 → MUI IconButton + Tooltip（复制态
// 由 Tooltip「已复制」+ success 色 + ✓ 字形三重反馈承载）；aria-label
// 纪律不变（§8：`复制 <对象描述>`——e2e 定位契约）。
// `copy-btn` 类名保留：repositories.spec:104 以 `.copy-btn` 类定位行内
// 拷贝钮（spec 类钩子纪律）；同名 CSS 是 AppShell/SearchPage 的「清除
// 历史」小文字钮在用（mui-native-visual §3.2 该行的实际退役时点后移）。

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
    <Tooltip title={done ? t('已复制') : t('复制 {label}', { label: label })}>
      <IconButton
        className="copy-btn"
        size="small"
        aria-label={t('复制 {label}', { label: label })}
        color={done ? 'success' : 'default'}
        onClick={() => void copy()}
      >
        <span aria-hidden="true" className="mono" style={{ fontSize: 13 }}>
          {done ? '✓' : '⧉'}
        </span>
      </IconButton>
    </Tooltip>
  )
}
