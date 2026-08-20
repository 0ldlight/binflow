import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './app/AuthContext'
import { ConfirmProvider } from './components/ConfirmDialog'
import { ThemeProvider } from './app/ThemeContext'
import { ToastProvider } from './app/ToastContext'
import './styles/tokens.css'
import './styles/base.css'

// 路由表（console-ux §3.2，应用内路径；basename = vite base =
// /binflow/ui，见 vite.config.ts）。每个页面 React.lazy——懒加载缝
// 是 T-89 定下的构建形态，后续票加页面不改本文件的构建形状。
// 本批（T-98）实际交付：login / 仪表盘 / 设置；其余已规划路由渲染
// 占位页（携带交付票号），未匹配路由渲染 404。
const LoginPage = lazy(() => import('./pages/LoginPage'))
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const PlaceholderPage = lazy(() => import('./pages/PlaceholderPage'))
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'))
const AppShell = lazy(() => import('./components/AppShell'))

function RouteFallback() {
  return <div className="route-fallback">加载中…</div>
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <ToastProvider>
        <BrowserRouter basename="/binflow/ui">
          <AuthProvider>
            <ConfirmProvider>
              <Suspense fallback={<RouteFallback />}>
                <Routes>
                  <Route path="/login" element={<LoginPage />} />
                  <Route path="/" element={<AppShell />}>
                    <Route index element={<DashboardPage />} />
                    <Route path="settings" element={<SettingsPage />} />
                    {/* 占位（T-99/T-100/T-101/T-102），深链可达 */}
                    <Route path="repositories/*" element={<PlaceholderPage title="仓库" ticket="T-99" />} />
                    <Route path="search" element={<PlaceholderPage title="搜索" ticket="T-100" />} />
                    <Route
                      path="security/*"
                      element={<PlaceholderPage title="安全" ticket="T-101" adminOnly />}
                    />
                    <Route
                      path="governance/*"
                      element={<PlaceholderPage title="治理" ticket="T-102" adminOnly />}
                    />
                    <Route
                      path="audit"
                      element={<PlaceholderPage title="审计" ticket="T-102" adminOnly />}
                    />
                    <Route path="*" element={<NotFoundPage />} />
                  </Route>
                </Routes>
              </Suspense>
            </ConfirmProvider>
          </AuthProvider>
        </BrowserRouter>
      </ToastProvider>
    </ThemeProvider>
  </StrictMode>,
)
