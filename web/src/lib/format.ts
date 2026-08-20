// 展示格式化：字节、去重率、审计时间（mono 场景消费）。

/** 1024 基字节格式化（注册表惯例）；0 与负数给 0 */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const digits = v >= 100 || i === 0 ? 0 : 1
  return `${v.toFixed(digits)} ${units[i]}`
}

/** 千分位整数 */
export function formatCount(n: number): string {
  return new Intl.NumberFormat('en-US').format(n)
}

/** 去重率 = 1 − 物理/逻辑（console-ux §4.2[4]；只给数字不给图表） */
export function dedupRatio(logical: number, physical: number): number {
  if (logical <= 0) return 0
  const r = 1 - physical / logical
  return Math.min(1, Math.max(0, r))
}

/**
 * 审计时间列：当日 HH:mm:ss，跨天补日期（console-ux §4.10——时间列
 * 固定宽 mono 防列抖动）。入参为 RFC3339 UTC 字符串。
 */
export function formatAuditTime(rfc3339: string, now = new Date()): string {
  const t = new Date(rfc3339)
  if (Number.isNaN(t.getTime())) return rfc3339
  const pad = (n: number) => String(n).padStart(2, '0')
  const hms = `${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`
  const sameDay =
    t.getFullYear() === now.getFullYear() &&
    t.getMonth() === now.getMonth() &&
    t.getDate() === now.getDate()
  if (sameDay) return hms
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())} ${hms}`
}
