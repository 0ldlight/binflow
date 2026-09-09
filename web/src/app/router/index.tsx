// data router（frontend-rewrite-architecture §1：createBrowserRouter data
// 模式）。P2 起为全应用唯一路由表——audit §1 路由总图 57 Route + 4 兼容
// 重定向逐条迁移（一个不丢）。P3（管理面全量）后全部路由指向新栈实现，
// LegacyBridge 拆除（bridge 零路由消费门达成）。
//
// URL 深链契约（不可变契约⑤）：树页签段+文件末段（/artifacts/[TAB/]repo/
// path，?focus= 一次性折入）、builds/bundles 三视图、search ?q/mode/scope、
// ?started= 消歧、?section= 直落——全部保持。
//
// basename = UI_BASE（挂载契约双真值之一，与 vite base 手工同步）。
import { Suspense, lazy } from 'react'
import type { ComponentType } from 'react'
import { Navigate, Outlet, createBrowserRouter, useSearchParams } from 'react-router-dom'
import type { RouteObject } from 'react-router-dom'

import { AuthProvider } from '@/app/AuthContext'
import { UI_BASE } from '@/app/config'
import { AppShell } from '@/app/shell/AppShell'
import { tr } from '@/i18n'


const t = tr('console')

// ---- P2 新实现（六域——loader 形态供 lazyEl 消费） ----
const LoginPage = () => import('@/pages/login/LoginPage')
const DashboardPage = () => import('@/pages/dashboard/DashboardPage')
const ExplorerPage = () => import('@/pages/explorer/ExplorerPage')
const SearchPage = () => import('@/pages/search/SearchPageV2')
const RepositoriesPage = () => import('@/pages/repos/RepositoriesPage')
const RepoDetailPage = () => import('@/pages/repos/RepoDetailPage')

// ---- P3 新实现（管理面全量 + Builds/Bundles/Profile/404——全部新栈） ----
const ProfilePage = () => import('@/pages/ProfilePage')
const BuildsPage = () => import('@/pages/builds/BuildsPage')
const BundlesPage = () => import('@/pages/bundles/BundlesPage')
const NotFoundPage = () => import('@/pages/NotFoundPage')
const UsersPage = () => import('@/pages/security/UsersPage')
const UserDetailPage = () => import('@/pages/security/UserDetailPage')
const UserCreatePage = () => import('@/pages/security/UserCreatePage')
const GroupsPage = () => import('@/pages/security/GroupsPage')
const PermissionsPage = () => import('@/pages/security/PermissionsPage')
const TokensPage = () => import('@/pages/security/TokensPage')
const KeypairPage = () => import('@/pages/security/KeypairPage')
const AuthConfigPage = () => import('@/pages/admin/authconfig/AuthConfigPage')
const AuditPage = () => import('@/pages/audit/AuditPage')
const QuotasPage = () => import('@/pages/governance/QuotasPage')
const ReplicationPage = () => import('@/pages/governance/ReplicationPage')
const TrashPage = () => import('@/pages/governance/TrashPage')
const GCPage = () => import('@/pages/governance/GCPage')
const BackupPage = () => import('@/pages/governance/BackupPage')
const StorageSummaryPage = () => import('@/pages/monitoring/StorageSummaryPage')
const SystemInfoPage = () => import('@/pages/monitoring/SystemInfoPage')
const ServiceStatusPage = () => import('@/pages/monitoring/ServiceStatusPage')
const SystemLogsPage = () => import('@/pages/monitoring/SystemLogsPage')
const SettingsPage = () => import('@/pages/monitoring/SettingsPage')
const LicenseAddonsPage = () => import('@/pages/admin/LicenseAddonsPage')
const WebhooksPage = () => import('@/pages/webhooks/WebhooksPage')

/** 路由分片加载态（新栈形态：spinner + 加载中…） */
function RouteFallback() {
  return (
    <div className="route-fallback">
      <span
        aria-hidden="true"
        className="mr-2 inline-block size-4 animate-spin rounded-full border-2 border-border border-t-primary align-[-2px]"
      />
      {t('加载中…')}
    </div>
  )
}

function lazyEl(load: () => Promise<{ default: ComponentType }>) {
  const Page = lazy(load)
  return (
    <Suspense fallback={<RouteFallback />}>
      <Page />
    </Suspense>
  )
}

// ---- 带 props 的页面挂载腿 ----
const NewRepoForm = lazy(() => import('@/pages/repos/RepositoryFormPage'))
const GroupForm = lazy(() => import('@/pages/security/GroupFormPage'))
const PermEditor = lazy(() => import('@/pages/security/PermissionEditorPage'))

function GroupFormCreate() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <GroupForm mode="create" />
    </Suspense>
  )
}
function GroupFormEdit() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <GroupForm mode="edit" />
    </Suspense>
  )
}
function PermEditorCreate() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <PermEditor mode="create" />
    </Suspense>
  )
}
function PermEditorEdit() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <PermEditor mode="edit" />
    </Suspense>
  )
}

function RepoFormCreateLocal() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <NewRepoForm mode="create" rclass="local" />
    </Suspense>
  )
}
function RepoFormCreateRemote() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <NewRepoForm mode="create" rclass="remote" />
    </Suspense>
  )
}
function RepoFormCreateVirtual() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <NewRepoForm mode="create" rclass="virtual" />
    </Suspense>
  )
}
function RepoFormEdit() {
  return (
    <Suspense fallback={<RouteFallback />}>
      <NewRepoForm mode="edit" />
    </Suspense>
  )
}

/** /admin/repositories/new 直链兼容映射（T-443）：?rclass= → 分路由 */
function RepoCreateCompat() {
  const [sp] = useSearchParams()
  const rc = sp.get('rclass')
  const target = rc === 'remote' || rc === 'virtual' ? rc : 'local'
  return <Navigate to={`/admin/repositories/${target}/new`} replace />
}

/** 根布局：AuthProvider 常驻（useNavigate 需要 Router 语境——data router
 *  下 Provider 必须在 RouterProvider 内部，故以 pathless 布局路由承载） */
function RootLayout() {
  return (
    <AuthProvider>
      <Outlet />
    </AuthProvider>
  )
}

/** 路由表（P2 核心 UX 批次——audit §1 路由总图逐条对照） */
export const appRoutes: RouteObject[] = [
  {
    element: <RootLayout />,
    children: [
      { path: '/login', element: lazyEl(LoginPage) },
      {
        path: '/',
        element: <AppShell />,
        children: [
          // 登录落点 = /artifacts（§1.1）；匿名访问 / 时壳的认证守卫
          // 先于本 index 重定向生效，return=%2F 语义不变
          { index: true, element: <Navigate to="/artifacts" replace /> },
          { path: 'dashboard', element: lazyEl(DashboardPage) },
          // Artifact Explorer（P2 第一优先）：根与子树同组件——URL 即状态
          // （页签段+文件末段契约，TAB ∈ {general|properties|permissions}）
          { path: 'artifacts', element: lazyEl(ExplorerPage) },
          { path: 'artifacts/:tab/:key/*', element: lazyEl(ExplorerPage) },
          { path: 'artifacts/:key/*', element: lazyEl(ExplorerPage) },
          { path: 'search', element: lazyEl(SearchPage) },
          // Builds / Bundles：P3 新栈重写（+promote/retention/create 写面解锁）
          { path: 'builds', element: lazyEl(BuildsPage) },
          { path: 'builds/:name', element: lazyEl(BuildsPage) },
          { path: 'builds/:name/:number', element: lazyEl(BuildsPage) },
          { path: 'bundles', element: lazyEl(BundlesPage) },
          { path: 'bundles/:name', element: lazyEl(BundlesPage) },
          { path: 'bundles/:name/:version', element: lazyEl(BundlesPage) },
          { path: 'profile', element: lazyEl(ProfilePage) },

          // —— 仓库域（P2 新实现）——
          { path: 'admin', element: <Navigate to="/admin/repositories" replace /> },
          { path: 'admin/repositories', element: <Navigate to="/admin/repositories/local" replace /> },
          { path: 'admin/repositories/local', element: lazyEl(RepositoriesPage) },
          { path: 'admin/repositories/remote', element: lazyEl(RepositoriesPage) },
          { path: 'admin/repositories/virtual', element: lazyEl(RepositoriesPage) },
          { path: 'admin/repositories/local/new', element: <RepoFormCreateLocal /> },
          { path: 'admin/repositories/remote/new', element: <RepoFormCreateRemote /> },
          { path: 'admin/repositories/virtual/new', element: <RepoFormCreateVirtual /> },
          { path: 'admin/repositories/new', element: <RepoCreateCompat /> },
          { path: 'admin/repositories/:key', element: lazyEl(RepoDetailPage) },
          { path: 'admin/repositories/:key/edit', element: <RepoFormEdit /> },

          // —— 用户与权限（P3 新栈重写）——
          { path: 'admin/security/users', element: lazyEl(UsersPage) },
          { path: 'admin/security/users/new', element: lazyEl(UserCreatePage) },
          { path: 'admin/security/users/:name', element: lazyEl(UserDetailPage) },
          { path: 'admin/security/groups', element: lazyEl(GroupsPage) },
          { path: 'admin/security/groups/new', element: <GroupFormCreate /> },
          { path: 'admin/security/groups/:name/edit', element: <GroupFormEdit /> },
          { path: 'admin/security/permissions', element: lazyEl(PermissionsPage) },
          { path: 'admin/security/permissions/new', element: <PermEditorCreate /> },
          { path: 'admin/security/permissions/:name', element: <PermEditorEdit /> },
          { path: 'admin/security/tokens', element: lazyEl(TokensPage) },
          { path: 'admin/security/keypair', element: lazyEl(KeypairPage) },
          { path: 'admin/security/auth', element: <Navigate to="/admin/security/auth/ldap" replace /> },
          { path: 'admin/security/auth/:proto', element: lazyEl(AuthConfigPage) },

          // —— 治理（P3 新栈重写）——
          { path: 'admin/governance/audit', element: lazyEl(AuditPage) },
          { path: 'admin/governance/quotas', element: lazyEl(QuotasPage) },
          { path: 'admin/governance/replication', element: lazyEl(ReplicationPage) },
          { path: 'admin/governance/trash', element: lazyEl(TrashPage) },

          // —— 监控（P3 新栈重写 + Settings 新设——capability matrix #25 解锁）——
          { path: 'admin/monitoring/storage', element: lazyEl(StorageSummaryPage) },
          { path: 'admin/monitoring/status', element: lazyEl(ServiceStatusPage) },
          { path: 'admin/monitoring/logs', element: lazyEl(SystemLogsPage) },
          { path: 'admin/monitoring/system-info', element: lazyEl(SystemInfoPage) },
          { path: 'admin/monitoring/settings', element: lazyEl(SettingsPage) },
          { path: 'admin/monitoring/gc', element: lazyEl(GCPage) },
          { path: 'admin/monitoring/backup', element: lazyEl(BackupPage) },

          // —— 常规（License P3 新栈重写；Webhooks 仍 LegacyBridge——本域
          //     P3 批次内重写后摘除）——
          { path: 'admin/general/webhooks', element: lazyEl(WebhooksPage) },
          { path: 'admin/general/license', element: lazyEl(LicenseAddonsPage) },

          // T-459 路由迁移兼容窗（旧深链 replace 折入新址；settings 直落
          // P3 新设页——旧兼容期曾折 system-info，Settings 真身落地后归位）
          { path: 'admin/general/settings', element: <Navigate to="/admin/monitoring/settings" replace /> },
          { path: 'admin/governance/gc', element: <Navigate to="/admin/monitoring/gc" replace /> },
          { path: 'admin/governance/backup', element: <Navigate to="/admin/monitoring/backup" replace /> },
          { path: 'admin/governance/webhooks', element: <Navigate to="/admin/general/webhooks" replace /> },

          // 未匹配 → 404（保留导航壳）
          { path: '*', element: lazyEl(NotFoundPage) },
        ],
      },
    ],
  },
]

export function createAppRouter() {
  return createBrowserRouter(appRoutes, { basename: UI_BASE })
}
