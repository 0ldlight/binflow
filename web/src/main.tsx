import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'

// 前端重写 P2 起：main.tsx 切换到新 data router（app/router/）——新壳
// app/shell/（四分组侧栏 + Topbar）上线；未重写域经 LegacyBridge 挂载
// （app/router/legacy-bridge.tsx——终验强删项，MUI=0 门）。
//
// Provider 树（外→内）：
//   AppProviders（新栈：Theme > Query > sonner Toast > 新 Confirm）
//     └ 旧树根（过渡期常驻——终验强删项）：
//         LegacyThemeShim（旧 ThemeContext 注入新主题真值）
//           └ MuiProvider（旧 MUI 主题——LegacyBridge 页面与旧 Toast 消费）
//             └ 旧 ToastProvider（旧 Snackbar——AuthContext 的 useToast 消费面；
//                 sonner 与之并存至 P4 收敛）
//               └ 旧 ConfirmProvider（useRepoDelete 等新旧共用件的确认层——
//                 新页与旧页同源消费，删除流零重写）
//                 └ RouterProvider（data router——AuthProvider 在根布局路由内）
//
// 引导顺序不变：consumeStepUpFragment（渲染前同步消费 OIDC 回跳
// fragment）→ initI18n 闸 → 渲染（en 目录包注册先于模块级 t() 求值点）。
import { AppProviders } from '@/app/providers'
import { ThemeContext as LegacyThemeContext } from '@/app/ThemeContext'
import { MuiProvider } from '@/app/MuiProvider'
import { ToastProvider as LegacyToastProvider } from '@/app/ToastContext'
import { ConfirmProvider as LegacyConfirmProvider } from '@/components/ConfirmDialog'
import { useTheme } from '@/app/providers'
import { createAppRouter } from '@/app/router'
import { consumeStepUpFragment } from '@/lib/stepUpGrant'
import { initI18n } from '@/i18n'

import './styles/tokens.css'
import './styles/base.css'
import './styles/pages.css'
import './styles/governance.css'
// 新栈 Tailwind 入口（styles/tw/tailwind.css——@theme 桥接 + dark 变体绑
// [data-theme]）。preflight 在共存期关闭（tailwind.css 头注——与 MUI
// CssBaseline / 旧 base.css 的元素基线零冲突）；MUI=0 终验时恢复整栈。
import './styles/tw/tailwind.css'

// OIDC step-up 回跳 fragment 消费（T-260 / architecture §14.3-2）：渲染前
// 同步完成——grant 提取入内存 + history.replaceState 抹除。无 fragment
// 时零副作用。
consumeStepUpFragment()

/** 旧 ThemeContext 注入桥（新主题真值 → 旧 context 形状，MuiProvider 消费） */
function LegacyThemeRoot({ children }: { children: React.ReactNode }) {
  const theme = useTheme()
  return <LegacyThemeContext.Provider value={theme}>{children}</LegacyThemeContext.Provider>
}

initI18n().then(() => {
  const router = createAppRouter()
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <AppProviders>
        <LegacyThemeRoot>
          <MuiProvider>
            <LegacyToastProvider>
              <LegacyConfirmProvider>
                <RouterProvider router={router} />
              </LegacyConfirmProvider>
            </LegacyToastProvider>
          </MuiProvider>
        </LegacyThemeRoot>
      </AppProviders>
    </StrictMode>,
  )
})
