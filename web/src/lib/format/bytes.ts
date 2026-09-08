// 字节格式化：1024 基（注册表惯例 KB/MB）；0 与负数给 0。
// 平移自现 lib/format.ts（旧文件仍在服役——MUI 终验时随旧消费面退役）。
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

/** 去重率 = 1 − 物理/逻辑（console-ux §4.2[4]；只给数字不给图表） */
export function dedupRatio(logical: number, physical: number): number {
  if (logical <= 0) return 0
  const r = 1 - physical / logical
  return Math.min(1, Math.max(0, r))
}
