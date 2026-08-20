import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './app/AuthContext'
import { ConfirmProvider } from './components/ConfirmDialog'
import { ThemeProvider } from './app/ThemeContext'
import { ToastProvider } from './app/ToastContext'
import './styles/tokens.css'
import './styles/base.css'
import './styles/pages.css'

// 路由表（console-ux §3.2，应用内路径；basename = vite base =
// /binflow/ui，见 vite.config.ts）。每个页面 React.lazy——懒加载缝
// 是 T-89 定下的构建形态，后续票加页面不改本文件的构建形状。
// 已交付：login / 仪表盘 / 设置（T-98）、仓库组（T-99：列表/新建/
// 详情/设置；tree 归 T-100）；其余已规划路由渲染占位页（携带交付
// 票号），未匹配路由渲染 404。
const LoginPage = lazy(() => import('./pages/LoginPage'))
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const RepositoriesPage = lazy(() => import('./pages/repositories/RepositoriesPage'))
const RepositoryFormPage = lazy(() => import('./pages/repositories/RepositoryFormPage'))
const RepoDetailPage = lazy(() => import('./pages/repositories/RepoDetailPage'))
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
                    {/* 仓库组（T-99；tree 归 T-100，深链保留占位） */}
                    <Route path="repositories" element={<RepositoriesPage />} />
                    <Route path="repositories/new" element={<RepositoryFormPage mode="create" />} />
                    <Route path="repositories/:key" element={<RepoDetailPage />} />
                    <Route path="repositories/:key/settings" element={<RepositoryFormPage mode="edit" />} />
                    <Route path="repositories/:key/tree/*" element={<PlaceholderPage title="制品" ticket="T-100" />} />
                    {/* 占位（T-100/T-101/T-102），深链可达 */}
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
