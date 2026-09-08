// 类名合成器（shadcn 底座标配）：clsx 收变参 + 条件类，tailwind-merge
// 消解 Tailwind 工具类冲突（后者按 tailwind 配置理解类语义）。
import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}
