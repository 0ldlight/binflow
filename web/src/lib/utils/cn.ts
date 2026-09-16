// 类名合成器（shadcn 底座标配）：clsx 收变参 + 条件类，tailwind-merge
// 消解 Tailwind 工具类冲突（后者按 tailwind 配置理解类语义）。
//
// twMerge 默认组表只认 Tailwind 内置字号阶（text-xs…text-9xl），本仓
// @theme 自定义密度字号（design-system/tailwind.css §密度字号：dense/aux/
// form/h3/h2）会被误判进 text-color 组——基线 text-aux 与变体色类
// （text-success 等）同串时前者被当冲突消解（L024-9 修票前消费点被迫
// 绕开此症，批 5 badge tint 族即病灶现场）。经 extendTailwindMerge 把
// 五个语义名注册进 font-size 组后语义归位：字号类与色彩类互不消解，
// 同组内仍按后者胜。
import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      'font-size': [{ text: ['dense', 'aux', 'form', 'h3', 'h2'] }],
    },
  },
})

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}
