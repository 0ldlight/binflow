import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'

import { AuthProvider } from './app/AuthContext'
import { ConfirmProvider } from './components/ConfirmDialog'
import { MuiProvider } from './app/MuiProvider'
import { ThemeProvider } from './app/ThemeContext'
import { ToastProvider } from './app/ToastContext'
import { consumeStepUpFragment } from './lib/stepUpGrant'
import './styles/tokens.css'
import './styles/base.css'
import './styles/pages.css'
import './styles/governance.css'

// OIDC step-up 回跳 fragment 消费（T-260 / architecture §14.3-2）：必须在
// React 树渲染前同步完成——grant 提取入内存 + history.replaceState 抹除
// fragment（不入历史、刷新不重放）；AppShell 经 useStepUp 感知后重开续铸
// 视图。无该 fragment 时零副作用。
consumeStepUpFragment()

// 路由表（console-m8 §1.3/§1.4——M8 IA 重排，T-235）。basename =
// vite base = /binflow/ui（ADR-0014，不变）。双模式：应用模式
// （/dashboard /artifacts /search /profile）+ 管理模式（/admin/** 五分组：
// 仓库/用户与权限/治理/监控/常规）。页面组件原样挂载（B3/B4 域票再重排
// 页内形态）。M8 的旧路由兼容窗口（20 条映射）已按 Q3 终裁在 M9 移除
// （T-263）：M7 及以前的旧路径不再重定向，一律落 * 通配的 NotFound
// （404 页保留导航壳 + 深链回主页，T-239 形态）。
// testid 242 锚不随路由改名（ADR-0029 决策 3 / console-ux §10.5）。
const LoginPage = lazy(() => import('./pages/LoginPage'))
const DashboardPage = lazy(() => import('./pages/DashboardPage'))
// 编辑档案（T-239 自设置页拆分：改密 + API Token 说明，§6.5；实例只读
// 面归 T-238 的 SystemInfoPage——SettingsPage.tsx 至此解散删除）
const ProfilePage = lazy(() => import('./pages/ProfilePage'))
const RepositoriesPage = lazy(() => import('./pages/repositories/RepositoriesPage'))
const RepositoryFormPage = lazy(() => import('./pages/repositories/RepositoryFormPage'))
const RepoDetailPage = lazy(() => import('./pages/repositories/RepoDetailPage'))
// 跨仓制品树（T-236 落真身；自 repositories/tree 迁址）：仓库为顶层节点
// 的左树 + 右详情面板 + 右键菜单；URL 即状态（/artifacts/<repo>/<path>，
// 文件选中进 ?focus=），深链自动展开。一个组件同时承载根与子树两条路由
// （路由结构 T-235 已定，本票只换占位挂载）。
const ArtifactsBrowser = lazy(() => import('./pages/artifacts/ArtifactsBrowser'))
const SearchPage = lazy(() => import('./pages/search/SearchPage'))
// 安全组（T-101 形态原样；页面重排归 T-237/T-241）
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
// 存储概要（T-238 落真身；§6.18：stats + 逐仓 usage 现役端点编排）
const StorageSummaryPage = lazy(() => import('./pages/monitoring/StorageSummaryPage'))
// 系统信息（T-238 落真身；§6.19——只读展示，改密块归 /profile 的 T-239 拆分）
const SystemInfoPage = lazy(() => import('./pages/admin/SystemInfoPage'))
// License & Add-ons（M10 T-288，FR-84 FE 腿 / FR-86-AC5：license 装卸 + 档位
// × addon 解锁矩阵 + 建仓门控的可见性面）
const LicenseAddonsPage = lazy(() => import('./pages/admin/LicenseAddonsPage'))
const AppShell = lazy(() => import('./components/AppShell'))

function RouteFallback() {
  return <div className="route-fallback">加载中…</div>
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      {/* MUI 主题桥（T-291 首票）：palette 对齐 tokens.css、深浅色跟随
          ThemeContext；仅 MUI 组件消费，存量页面样式零改动 */}
      <MuiProvider>
      <ToastProvider>
        <BrowserRouter basename="/binflow/ui">
          <AuthProvider>
            <ConfirmProvider>
              <Suspense fallback={<RouteFallback />}>
                <Routes>
                  <Route path="/login" element={<LoginPage />} />
                  <Route path="/" element={<AppShell />}>
                    {/* —— 应用模式（console-m8 §1.3）—— */}
                    {/* 登录落点 = /artifacts（§1.1）；匿名访问 / 时壳的
                        认证守卫先于本 index 重定向生效，return=%2F 语义不变 */}
                    <Route index element={<Navigate to="/artifacts" replace />} />
                    <Route path="dashboard" element={<DashboardPage />} />
                    {/* 跨仓树（T-236）：根与子树同组件——URL 即状态 */}
                    <Route path="artifacts" element={<ArtifactsBrowser />} />
                    <Route path="artifacts/:key/*" element={<ArtifactsBrowser />} />
                    <Route path="search" element={<SearchPage />} />
                    {/* 编辑档案（T-239 拆分落位：改密 + API Token；§6.5） */}
                    <Route path="profile" element={<ProfilePage />} />

                    {/* —— 管理模式：仓库（§1.3 五分组之一）—— */}
                    <Route
                      path="admin"
                      element={<Navigate to="/admin/repositories" replace />}
                    />
                    <Route
                      path="admin/repositories"
                      element={<Navigate to="/admin/repositories/local" replace />}
                    />
                    {/* 三 Tab 子路由（Tab 形态归 T-240；页面原样挂载） */}
                    <Route path="admin/repositories/local" element={<RepositoriesPage />} />
                    <Route path="admin/repositories/remote" element={<RepositoriesPage />} />
                    <Route path="admin/repositories/virtual" element={<RepositoriesPage />} />
                    <Route
                      path="admin/repositories/new"
                      element={<RepositoryFormPage mode="create" />}
                    />
                    <Route path="admin/repositories/:key" element={<RepoDetailPage />} />
                    <Route
                      path="admin/repositories/:key/edit"
                      element={<RepositoryFormPage mode="edit" />}
                    />

                    {/* —— 管理模式：用户与权限 —— */}
                    <Route path="admin/security/users" element={<UsersPage />} />
                    <Route path="admin/security/users/:name" element={<UserDetailPage />} />
                    <Route path="admin/security/groups" element={<GroupsPage />} />
                    <Route path="admin/security/permissions" element={<PermissionsPage />} />
                    <Route
                      path="admin/security/permissions/new"
                      element={<PermissionEditorPage mode="create" />}
                    />
                    <Route
                      path="admin/security/permissions/:name"
                      element={<PermissionEditorPage mode="edit" />}
                    />
                    {/* Access Tokens（ux R6/P2：签发引导 + 吊销占位，§6.12） */}
                    <Route
                      path="admin/security/tokens"
                      element={<PlaceholderPage title="Access Tokens" ticket="P2" adminOnly />}
                    />

                    {/* —— 管理模式：治理 —— */}
                    <Route path="admin/governance/audit" element={<AuditPage />} />
                    <Route path="admin/governance/gc" element={<GCPage />} />
                    <Route path="admin/governance/quotas" element={<QuotasPage />} />
                    <Route path="admin/governance/replication" element={<ReplicationPage />} />
                    <Route path="admin/governance/backup" element={<BackupPage />} />

                    {/* —— 管理模式：监控 / 常规（T-238 落真身：存储概要 +
                         系统信息；占位/设置页挂载让位，路由结构不变。
                         T-288 增 License & Add-ons 页）—— */}
                    <Route path="admin/monitoring/storage" element={<StorageSummaryPage />} />
                    <Route path="admin/general/settings" element={<SystemInfoPage />} />
                    <Route path="admin/general/license" element={<LicenseAddonsPage />} />

                    {/* 未匹配 → 404 页（T-263 起旧路由兼容窗口不再兜底：
                         M7 及以前的旧路径同样落这里） */}
                    <Route path="*" element={<NotFoundPage />} />
                  </Route>
                </Routes>
              </Suspense>
            </ConfirmProvider>
          </AuthProvider>
        </BrowserRouter>
      </ToastProvider>
      </MuiProvider>
    </ThemeProvider>
  </StrictMode>,
)
