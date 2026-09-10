import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'

// 前端重写 P2 起：main.tsx 切换到新 data router（app/router/）——新壳
// app/shell/（四分组侧栏 + Topbar）上线；未重写域经 LegacyBridge 挂载
// （app/router/legacy-bridge.tsx——已于 P3 末随全页迁移拆除）。
//
// FE-P4 MUI 清场：旧树根（LegacyThemeRoot + MuiProvider + 旧 Snackbar
// ToastProvider）整体退役——ThemeContext/MuiProvider/ToastContext 的 MUI
// 载体删除，toast 经 app/ToastContext 桥接 sonner（零消费面改动）。
// 存留：ConfirmDialog 的 ConfirmProvider（body + confirmDisabled 的
// 深形 API——useRepoDelete/Properties/Replications 消费面，P4 重写为
// Radix 壳、API 逐字保真；新栈 confirm-provider 与之并存各司其职）。
//
// Provider 树（外→内）：
//   AppProviders（新栈：Theme > Query > sonner Toast > 新 Confirm）
//     └ ConfirmProvider（深形危险确认层——FE-P4 Radix 化）
//       └ RouterProvider（data router——AuthProvider 在根布局路由内）
//
// 引导顺序不变：consumeStepUpFragment（渲染前同步消费 OIDC 回跳
// fragment）→ initI18n 闸 → 渲染（en 目录包注册先于模块级 t() 求值点）。
import { AppProviders } from '@/app/providers'
import { ConfirmProvider } from '@/components/ConfirmDialog'
import { createAppRouter } from '@/app/router'
import { consumeStepUpFragment } from '@/lib/stepUpGrant'
import { initI18n } from '@/i18n'

import './styles/tokens.css'
import './styles/base.css'
import './styles/pages.css'
import './styles/governance.css'
// 新栈 Tailwind 入口（styles/tw/tailwind.css——@theme 桥接 + dark 变体绑
// [data-theme]）。preflight 在共存期关闭（tailwind.css 头注——与旧
// base.css 的元素基线零冲突）；MUI=0 终验时评估恢复整栈。
import './styles/tw/tailwind.css'

// OIDC step-up 回跳 fragment 消费（T-260 / architecture §14.3-2）：渲染前
// 同步完成——grant 提取入内存 + history.replaceState 抹除。无 fragment
// 时零副作用。
consumeStepUpFragment()

initI18n().then(() => {
  const router = createAppRouter()
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <AppProviders>
        <ConfirmProvider>
          <RouterProvider router={router} />
        </ConfirmProvider>
      </AppProviders>
    </StrictMode>,
  )
})
