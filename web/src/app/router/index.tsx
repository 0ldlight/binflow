// data router（frontend-rewrite-architecture §1：createBrowserRouter data
// 模式）。P2 起为全应用唯一路由表——audit §1 路由总图 57 Route + 4 兼容
// 重定向逐条迁移（一个不丢），其中六域指向新实现（Login / Dashboard /
// Repositories 全套 / Artifact Explorer / Artifact Detail（inspector 形态
// 随 Explorer 承载）/ Search），其余全部走 LegacyBridge。
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

import { LegacyMount, legacyPage } from './legacy-bridge'

const t = tr('console')

// ---- P2 新实现（六域——loader 形态供 lazyEl 消费） ----
const LoginPage = () => import('@/pages/login/LoginPage')
const DashboardPage = () => import('@/pages/dashboard/DashboardPage')
const ExplorerPage = () => import('@/pages/explorer/ExplorerPage')
const SearchPage = () => import('@/pages/search/SearchPageV2')
const RepositoriesPage = () => import('@/pages/repos/RepositoriesPage')
const RepoDetailPage = () => import('@/pages/repos/RepoDetailPage')

// ---- LegacyBridge 挂载（未重写域——终验强删项） ----
const ProfilePage = () => import('@/pages/ProfilePage')
const BuildsPage = () => import('@/pages/builds/BuildsPage')
const BundlesPage = () => import('@/pages/bundles/BundlesPage')
const UsersPage = () => import('@/pages/security/UsersPage')
const UserDetailPage = () => import('@/pages/security/UserDetailPage')
const UserCreatePage = () => import('@/pages/security/UserCreatePage')
const GroupsPage = () => import('@/pages/security/GroupsPage')
const PermissionsPage = () => import('@/pages/security/PermissionsPage')
const TokensPage = () => import('@/pages/security/TokensPage')
const NotFoundPage = () => import('@/pages/NotFoundPage')
const AuditPage = () => import('@/pages/audit/AuditPage')
const GCPage = () => import('@/pages/governance/GCPage')
const ReplicationPage = () => import('@/pages/governance/ReplicationPage')
const QuotasPage = () => import('@/pages/governance/QuotasPage')
const BackupPage = () => import('@/pages/governance/BackupPage')
const TrashPage = () => import('@/pages/governance/TrashPage')
const WebhooksPage = () => import('@/pages/webhooks/WebhooksPage')
const StorageSummaryPage = () => import('@/pages/monitoring/StorageSummaryPage')
const SystemInfoPage = () => import('@/pages/monitoring/SystemInfoPage')
const ServiceStatusPage = () => import('@/pages/monitoring/ServiceStatusPage')
const SystemLogsPage = () => import('@/pages/monitoring/SystemLogsPage')
const LicenseAddonsPage = () => import('@/pages/admin/LicenseAddonsPage')
const AuthConfigPage = () => import('@/pages/admin/authconfig/AuthConfigPage')

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
const LegacyGroupForm = lazy(() => import('@/pages/security/GroupFormPage'))
const LegacyPermEditor = lazy(() => import('@/pages/security/PermissionEditorPage'))

function GroupFormCreate() {
  return (
    <LegacyMount>
      <LegacyGroupForm mode="create" />
    </LegacyMount>
  )
}
function GroupFormEdit() {
  return (
    <LegacyMount>
      <LegacyGroupForm mode="edit" />
    </LegacyMount>
  )
}
function PermEditorCreate() {
  return (
    <LegacyMount>
      <LegacyPermEditor mode="create" />
    </LegacyMount>
  )
}
function PermEditorEdit() {
  return (
    <LegacyMount>
      <LegacyPermEditor mode="edit" />
    </LegacyMount>
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
          // Builds / Bundles：LegacyBridge（P3 重写域）
          { path: 'builds', element: legacyPage(BuildsPage) },
          { path: 'builds/:name', element: legacyPage(BuildsPage) },
          { path: 'builds/:name/:number', element: legacyPage(BuildsPage) },
          { path: 'bundles', element: legacyPage(BundlesPage) },
          { path: 'bundles/:name', element: legacyPage(BundlesPage) },
          { path: 'bundles/:name/:version', element: legacyPage(BundlesPage) },
          { path: 'profile', element: legacyPage(ProfilePage) },

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

          // —— 用户与权限（LegacyBridge——P3 域）——
          { path: 'admin/security/users', element: legacyPage(UsersPage) },
          { path: 'admin/security/users/new', element: legacyPage(UserCreatePage) },
          { path: 'admin/security/users/:name', element: legacyPage(UserDetailPage) },
          { path: 'admin/security/groups', element: legacyPage(GroupsPage) },
          { path: 'admin/security/groups/new', element: <GroupFormCreate /> },
          { path: 'admin/security/groups/:name/edit', element: <GroupFormEdit /> },
          { path: 'admin/security/permissions', element: legacyPage(PermissionsPage) },
          { path: 'admin/security/permissions/new', element: <PermEditorCreate /> },
          { path: 'admin/security/permissions/:name', element: <PermEditorEdit /> },
          { path: 'admin/security/tokens', element: legacyPage(TokensPage) },
          { path: 'admin/security/auth', element: <Navigate to="/admin/security/auth/ldap" replace /> },
          { path: 'admin/security/auth/:proto', element: legacyPage(AuthConfigPage) },

          // —— 治理（LegacyBridge）——
          { path: 'admin/governance/audit', element: legacyPage(AuditPage) },
          { path: 'admin/governance/quotas', element: legacyPage(QuotasPage) },
          { path: 'admin/governance/replication', element: legacyPage(ReplicationPage) },
          { path: 'admin/governance/trash', element: legacyPage(TrashPage) },

          // —— 监控（LegacyBridge）——
          { path: 'admin/monitoring/storage', element: legacyPage(StorageSummaryPage) },
          { path: 'admin/monitoring/status', element: legacyPage(ServiceStatusPage) },
          { path: 'admin/monitoring/logs', element: legacyPage(SystemLogsPage) },
          { path: 'admin/monitoring/system-info', element: legacyPage(SystemInfoPage) },
          { path: 'admin/monitoring/gc', element: legacyPage(GCPage) },
          { path: 'admin/monitoring/backup', element: legacyPage(BackupPage) },

          // —— 常规（LegacyBridge）——
          { path: 'admin/general/webhooks', element: legacyPage(WebhooksPage) },
          { path: 'admin/general/license', element: legacyPage(LicenseAddonsPage) },

          // T-459 路由迁移兼容窗（4 条重定向——旧深链 replace 折入新址）
          { path: 'admin/general/settings', element: <Navigate to="/admin/monitoring/system-info" replace /> },
          { path: 'admin/governance/gc', element: <Navigate to="/admin/monitoring/gc" replace /> },
          { path: 'admin/governance/backup', element: <Navigate to="/admin/monitoring/backup" replace /> },
          { path: 'admin/governance/webhooks', element: <Navigate to="/admin/general/webhooks" replace /> },

          // 未匹配 → 404（保留导航壳）
          { path: '*', element: legacyPage(NotFoundPage) },
        ],
      },
    ],
  },
]

export function createAppRouter() {
  return createBrowserRouter(appRoutes, { basename: UI_BASE })
}
