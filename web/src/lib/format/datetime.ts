// 日期双 locale 格式化（T-464 FR-149.4：zh 现行形态 / en 走 en-US
// 12 小时制 `MMM d, yyyy`）。平移自现 lib/format.ts（旧文件仍在服役）。
import { getLocale } from '@/i18n'

const MON_EN = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'] as const

/**
 * 审计时间列：当日 HH:mm:ss，跨天补日期（console-ux §4.10——时间列
 * 固定宽 mono 防列抖动）。入参为 RFC3339 UTC 字符串。en 变体（T-464）：
 * 当日 `h:mm:ss AM/PM`，跨天 `MMM d, yyyy h:mm:ss AM/PM`。
 */
export function formatAuditTime(rfc3339: string, now = new Date()): string {
  const t = new Date(rfc3339)
  if (Number.isNaN(t.getTime())) return rfc3339
  const pad = (n: number) => String(n).padStart(2, '0')
  const sameDay =
    t.getFullYear() === now.getFullYear() &&
    t.getMonth() === now.getMonth() &&
    t.getDate() === now.getDate()
  if (getLocale() === 'en') {
    const h12 = t.getHours() % 12 || 12
    const ampm = t.getHours() < 12 ? 'AM' : 'PM'
    const hms = `${h12}:${pad(t.getMinutes())}:${pad(t.getSeconds())} ${ampm}`
    if (sameDay) return hms
    return `${MON_EN[t.getMonth()]} ${t.getDate()}, ${t.getFullYear()} ${hms}`
  }
  const hms = `${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`
  if (sameDay) return hms
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())} ${hms}`
}
