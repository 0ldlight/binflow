// 旧全局对话框挂载壳（frontend-rewrite P3：LegacyBridge 拆除后仅存的过渡
// 件——SetMeUpDialog / DeployDialog / PropertiesTab 等 MUI 树仍被 P2 页面
// 经此挂载；Provider 已在 main.tsx 根常驻，这里只剩 Suspense）。P4 新栈
// 重写这些对话框后本文件随之退役（终验 MUI=0 强删清单成员）。
import { Suspense, lazy } from 'react'
import type { ComponentType, ReactNode } from 'react'

/** 带 props 的旧页面挂载壳（LegacyMount 的承接位——RepositoryFormPage 的
 *  PropertiesTab 嵌挂等；回退 = 空态） */
export function LegacyMount({ children }: { children: ReactNode }) {
  return <Suspense fallback={null}>{children}</Suspense>
}

/** 新页面里嵌挂旧全局对话框（Deploy/SetMeUp——MUI 树，P4 重写退役） */
export function LegacyDialogHost({ children }: { children: ReactNode }) {
  return <Suspense fallback={null}>{children}</Suspense>
}

/** 旧组件 lazy 装载便捷位（消费方 import 语句保持一行） */
export function lazyLegacy<T extends ComponentType>(load: () => Promise<{ default: T }>) {
  return lazy(load)
}
