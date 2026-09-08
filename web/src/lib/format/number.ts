// 数字格式化：千分位整数（zh-CN 与 en-US 三位分组形态一致——显式随
// locale 取义）。平移自现 lib/format.ts（旧文件仍在服役）。
import { getLocale } from '@/i18n'

export function formatCount(n: number): string {
  return new Intl.NumberFormat(getLocale() === 'en' ? 'en-US' : 'zh-CN').format(n)
}
