import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'

import CircularProgress from '@mui/material/CircularProgress'

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
// Access Tokens 真身（M14 T-386，FR-125.1）：签发（一次性明文 + step-up 链）
// + 会话台账 + 吊销；消费面 = 既有 token REST 两端点（零新端点）
const TokensPage = lazy(() => import('./pages/security/TokensPage'))
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'))
const AuditPage = lazy(() => import('./pages/audit/AuditPage'))
const GCPage = lazy(() => import('./pages/governance/GCPage'))
// 复制面板（T-159）：push 复制状态 + 事件列表（10s 轮询）
const ReplicationPage = lazy(() => import('./pages/governance/ReplicationPage'))
const QuotasPage = lazy(() => import('./pages/governance/QuotasPage'))
const BackupPage = lazy(() => import('./pages/governance/BackupPage'))
// 回收站（M12 T-352，FR-106 FE 腿：浏览/恢复/清空——治理分组破坏性管理面）
const TrashPage = lazy(() => import('./pages/governance/TrashPage'))
// Webhook 订阅管理（M13 T-366，FR-115.5 FE 腿：订阅 CRUD/test + 排障记录
// ——治理分组「Webhooks」；消费 /binflow/event/api/v1 七端点族）
const WebhooksPage = lazy(() => import('./pages/webhooks/WebhooksPage'))
// 存储概要（T-238 落真身；§6.18：stats + 逐仓 usage 现役端点编排）
const StorageSummaryPage = lazy(() => import('./pages/monitoring/StorageSummaryPage'))
// 系统信息（T-238 落真身；§6.19——只读展示，改密块归 /profile 的 T-239 拆分）
const SystemInfoPage = lazy(() => import('./pages/admin/SystemInfoPage'))
// License & Add-ons（M10 T-288，FR-84 FE 腿 / FR-86-AC5：license 装卸 + 档位
// × addon 解锁矩阵 + 建仓门控的可见性面）
const LicenseAddonsPage = lazy(() => import('./pages/admin/LicenseAddonsPage'))
// 认证配置（M11 T-307，FR-92 FE 腿：LDAP/OAuth(OIDC)/SAML 三协议 Tab =
// 子路由；索引重定向 ldap，非法段同收敛）
const AuthConfigPage = lazy(() => import('./pages/admin/authconfig/AuthConfigPage'))
const AppShell = lazy(() => import('./components/AppShell'))

// 路由分片加载态（T-344 批 A 改薄，mui-native-visual §4.1）：CircularProgress
function RouteFallback() {
  return (
    <div className="route-fallback">
      <CircularProgress size={18} aria-label="页面加载中" sx={{ mr: 'var(--bf-sp-2)' }} />
      加载中…
    </div>
  )
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
                    {/* 跨仓树（T-236）：根与子树同组件——URL 即状态。T-434
                        （FR-142.3）：活跃页签进 URL 路径段——
                        /artifacts/[<TAB>/]<repo>/<path>，TAB ∈
                        {general|properties|permissions}（省略 = general，
                        对位 Artifactory /ui/repos/tree/<TAB>/…）；文件选择
                        是路径末段（?focus= 退役，旧深链组件内一次性
                        replace 重定向）。多段 URL 走 :tab/:key 路由（首段
                        非页签词时组件按旧形折叠回 repo/path） */}
                    <Route path="artifacts" element={<ArtifactsBrowser />} />
                    <Route path="artifacts/:tab/:key/*" element={<ArtifactsBrowser />} />
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
                    {/* Access Tokens（M14 T-386 落真身：占位页载体退役，grep=0） */}
                    <Route path="admin/security/tokens" element={<TokensPage />} />
                    {/* 认证配置（T-307）：三协议 Tab = 同组件按段参数化；
                         非法段在组件内重定向 ldap */}
                    <Route
                      path="admin/security/auth"
                      element={<Navigate to="/admin/security/auth/ldap" replace />}
                    />
                    <Route path="admin/security/auth/:proto" element={<AuthConfigPage />} />

                    {/* —— 管理模式：治理 —— */}
                    <Route path="admin/governance/audit" element={<AuditPage />} />
                    <Route path="admin/governance/gc" element={<GCPage />} />
                    <Route path="admin/governance/quotas" element={<QuotasPage />} />
                    <Route path="admin/governance/replication" element={<ReplicationPage />} />
                    <Route path="admin/governance/backup" element={<BackupPage />} />
                    {/* 回收站（T-352）：trashcan 槽门控态 + 浏览/恢复/清空 */}
                    <Route path="admin/governance/trash" element={<TrashPage />} />
                    {/* Webhook 订阅（T-366）：治理分组第七页——事件订阅 + 投递排障 */}
                    <Route path="admin/governance/webhooks" element={<WebhooksPage />} />

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
