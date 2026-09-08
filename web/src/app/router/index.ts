// data router 骨架（frontend-rewrite-architecture §1：createBrowserRouter
// data 模式——loader/action 渐进启用；basename 契约不变）。
//
// P1 路由表空壳：P2 起按「页面路由全部保持现 URL」逐域落 RouteObject
// （树页签段/builds/bundles 三视图/search ?q= 等 URL 形态见不可变契约⑤）。
// basename = UI_BASE（挂载契约双真值之一，与 vite base 手工同步）。
import { createBrowserRouter } from 'react-router-dom'
import type { RouteObject } from 'react-router-dom'

import { UI_BASE } from '@/app/config'

/** 路由表（P1 空壳——P2 核心 UX 批次逐域填充） */
export const appRoutes: RouteObject[] = []

export function createAppRouter() {
  return createBrowserRouter(appRoutes, { basename: UI_BASE })
}
