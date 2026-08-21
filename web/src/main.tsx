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
import './styles/governance.css'

// 路由表（console-ux §3.2，应用内路径；basename = vite base =
// /binflow/ui，见 vite.config.ts）。每个页面 React.lazy——懒加载缝
// 是 T-89 定下的构建形态，后续票加页面不改本文件的构建形状。
// 已交付：login / 仪表盘 / 设置（T-98）、仓库组（T-99：列表/新建/
// 详情/设置；tree 归 T-100）、治理组（T-102：审计 / GC / 配额 / 备份
// 兜底页）；其余已规划路由渲染占位页（携带交付票号），未匹配路由
// 渲染 404。
const LoginPage = lazy(() => import('./pages/LoginPage'))
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const RepositoriesPage = lazy(() => import('./pages/repositories/RepositoriesPage'))
const RepositoryFormPage = lazy(() => import('./pages/repositories/RepositoryFormPage'))
const RepoDetailPage = lazy(() => import('./pages/repositories/RepoDetailPage'))
// 制品树 + 搜索（T-100：树浏览/上传/下载/删除 + 搜索页）
const TreePage = lazy(() => import('./pages/repositories/tree/TreePage'))
const SearchPage = lazy(() => import('./pages/search/SearchPage'))
// 安全组（T-101：用户/组/权限 target 编辑器；tokens P2 兜底占位）
const UsersPage = lazy(() => import('./pages/security/UsersPage'))
const UserDetailPage = lazy(() => import('./pages/security/UserDetailPage'))
const GroupsPage = lazy(() => import('./pages/security/GroupsPage'))
const PermissionsPage = lazy(() => import('./pages/security/PermissionsPage'))
const PermissionEditorPage = lazy(() => import('./pages/security/PermissionEditorPage'))
const PlaceholderPage = lazy(() => import('./pages/PlaceholderPage'))
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'))
const AuditPage = lazy(() => import('./pages/audit/AuditPage'))
const GCPage = lazy(() => import('./pages/governance/GCPage'))
// 复制面板（T-159）：push 复制状态 + 事件列表（10s 轮询）
const ReplicationPage = lazy(() => import('./pages/governance/ReplicationPage'))
const QuotasPage = lazy(() => import('./pages/governance/QuotasPage'))
const BackupPage = lazy(() => import('./pages/governance/BackupPage'))
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
                    {/* 制品树（T-100）：splat = repo 相对目录深链 */}
                    <Route path="repositories/:key/tree/*" element={<TreePage />} />
                    {/* 搜索（T-100，⌘K 全局快捷键的落点） */}
                    <Route path="search" element={<SearchPage />} />
                    {/* 安全组（T-101）：用户/组/权限 target 编辑器 */}
                    <Route path="security/users" element={<UsersPage />} />
                    <Route path="security/users/:name" element={<UserDetailPage />} />
                    <Route path="security/groups" element={<GroupsPage />} />
                    <Route path="security/permissions" element={<PermissionsPage />} />
                    <Route path="security/permissions/new" element={<PermissionEditorPage mode="create" />} />
                    <Route path="security/permissions/:name" element={<PermissionEditorPage mode="edit" />} />
                    {/* 兜底占位（Access Tokens——ux R6 P2：签发引导 + 既有 revoke） */}
                    <Route
                      path="security/*"
                      element={<PlaceholderPage title="Access Tokens" ticket="P2" adminOnly />}
                    />
                    {/* 治理组（T-102）：审计 / GC / 配额 / 备份（备份为 R5 兜底引导页）；
                        复制面板（T-159）：push 复制状态 + 事件列表 */}
                    <Route path="audit" element={<AuditPage />} />
                    <Route path="governance/gc" element={<GCPage />} />
                    <Route path="governance/replication" element={<ReplicationPage />} />
                    <Route path="governance/quotas" element={<QuotasPage />} />
                    <Route path="governance/backup" element={<BackupPage />} />
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
