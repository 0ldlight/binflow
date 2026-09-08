// LegacyBridge（frontend-rewrite P2 过渡架构——dispatch 核心决策）：
// 未重写的旧页面（MUI 树）以 lazy 路由挂载进新 data router，仅做
// Provider 包裹桥，**零逻辑适配**。
//
// ┌──────────────────────────────────────────────────────────────────────┐
// │ 【终验强删项（MUI=0 门）】                                          │
// │ 本文件 + 它引用的全部旧模块（pages/legacy 树 / app/MuiProvider /     │
// │ app/ToastContext / components/ConfirmDialog / SetMeUpDialog /        │
// │ DeployDialog / PropertiesTab / ReplicationsSection / …）在 Phase 终 │
// │ 验「MUI=0 / Emotion=0 依赖清零」时整体删除。删除清单见               │
// │ reports/agents/FE-P2.md。逐域重写（P3）时对应路由改指新实现，本桥  │
// │ 的 lazy 清单同步缩短。                                              │
// └──────────────────────────────────────────────────────────────────────┘
//
// Provider 布局：旧 Provider 树（ThemeShim → MuiProvider → 旧 Toast →
// 旧 Confirm）常驻 main.tsx 根——LegacyBridge 页面与新页面共享同一批
// 全局单例（401 监听 / 会话 / 旧确认层一次挂载），本桥只剩 Suspense
// 分片回退。新页面的新栈 Provider（Query/sonner/新 Confirm）在
// AppProviders 层，两族互不感知。
//
// 路由深链契约（audit §1 路由总图）：57 Route + 4 重定向逐条对照迁移，
// 一个不丢——新路由表是唯一事实源，本文件只负责「旧组件 → 新路由元素」
// 的懒挂载。
import { Suspense, lazy } from 'react'
import type { ComponentType, ReactNode } from 'react'

import { tr } from '@/i18n'

const t = tr('console')

/** 路由分片加载态（对齐旧 RouteFallback 形态：spinner + 加载中…，零锚） */
function LegacyRouteFallback() {
  return (
    <div className="route-fallback">
      <span aria-hidden="true" className="mr-2 inline-block size-4 animate-spin rounded-full border-2 border-border border-t-primary align-[-2px]" />
      {t('加载中…')}
    </div>
  )
}

/** 旧页面组件挂进新路由：lazy 分片 + Suspense 回退（Provider 在根常驻）。 */
export function legacyPage(load: () => Promise<{ default: ComponentType }>) {
  const Page = lazy(load)
  return (
    <Suspense fallback={<LegacyRouteFallback />}>
      <Page />
    </Suspense>
  )
}

/** 带 props 的旧页面挂载壳（GroupForm/PermissionEditor 的 mode prop 等）：
 *  消费方 lazy() 取组件后作为 children 传入——回退同款。 */
export function LegacyMount({ children }: { children: ReactNode }) {
  return <Suspense fallback={<LegacyRouteFallback />}>{children}</Suspense>
}

/** 新页面里嵌挂旧全局对话框（Deploy/SetMeUp——MUI 树，P4 重写退役）：
 *  Provider 已在根常驻——只剩 Suspense（lazy 分片回退 = 空态）。 */
export function LegacyDialogHost({ children }: { children: ReactNode }) {
  return <Suspense fallback={null}>{children}</Suspense>
}
