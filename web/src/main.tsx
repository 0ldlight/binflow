import { StrictMode, Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useSearchParams } from 'react-router-dom'

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
import { initI18n, tr } from './i18n'

const t = tr('console')

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
// Release Bundles（M17 T-514，FR-153.3）：bundle 列表/详情只读查询面——
// 三视图一组件（ArtifactsBrowser 先例：/bundles → /bundles/:name →
// /bundles/:name/:version，URL 即状态）；消费 T-513 查询族端点
const BundlesPage = lazy(() => import('./pages/bundles/BundlesPage'))
// Builds（M17 T-512，FR-152.3）：build 域只读查询面——三视图一组件
// （BundlesPage 先例：/builds → /builds/:name → /builds/:name/:number，
// URL 即状态；?started= 消歧同名同号多 run）；消费 T-508 查询族端点
const BuildsPage = lazy(() => import('./pages/builds/BuildsPage'))
// 安全组（T-101 形态原样；页面重排归 T-237/T-241）
const UsersPage = lazy(() => import('./pages/security/UsersPage'))
const UserDetailPage = lazy(() => import('./pages/security/UserDetailPage'))
// T-453（FR-145.1，断言反转④——Q5 出口①路由化）：用户/组创建迁整页路由
// 表单（/users/new、/groups/new 深链，7.161.20 同构），列表内联展开卡退役；
// 组编辑同场路由化（/groups/:name/edit——内联卡创建/编辑同卡，一并退役）
const UserCreatePage = lazy(() => import('./pages/security/UserCreatePage'))
const GroupsPage = lazy(() => import('./pages/security/GroupsPage'))
const GroupFormPage = lazy(() => import('./pages/security/GroupFormPage'))
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
// 系统信息（T-238 落真身；§6.19——只读展示，改密块归 /profile 的 T-239 拆分；
// T-459 归位监控组（FR-145.5）：路由 /admin/monitoring/system-info）
const SystemInfoPage = lazy(() => import('./pages/monitoring/SystemInfoPage'))
// T-459（FR-145.5 / parity B-1.11）：监控组三页——服务状态（health +
// schedules 只读运行面）+ 系统日志（审计跟踪尾随查看器：尾随刷新/过滤/下载）
const ServiceStatusPage = lazy(() => import('./pages/monitoring/ServiceStatusPage'))
const SystemLogsPage = lazy(() => import('./pages/monitoring/SystemLogsPage'))
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
      <CircularProgress size={18} aria-label={t('页面加载中')} sx={{ mr: 'var(--bf-sp-2)' }} />{t('加载中…')}    </div>
  )
}

/** /admin/repositories/new 直链兼容映射（T-443）：?rclass= {local|remote|
 *  virtual} → 对应分路由（replace，不占历史）；缺省/非法值 → local。
 *  兼容窗内的 7 处跨页 emitter 零改动（见路由表注）。 */
function RepoCreateCompat() {
  const [sp] = useSearchParams()
  const rc = sp.get('rclass')
  const target = rc === 'remote' || rc === 'virtual' ? rc : 'local'
  return <Navigate to={`/admin/repositories/${target}/new`} replace />
}

// i18n 引导闸（T-463，FR-149.1）：渲染前就位——同步 <html lang>；en
// locale 时懒载 en 目录包（zh 零开销零请求）。门后渲染保证模块级 t()
// 求值点（formCopy 等常量模块）在目录包注册完成之后执行。
initI18n().then(() => {
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
                    {/* Builds（T-512）：三视图一组件——名单 / run 号单
                        （:name）/ run 详情（:name/:number，?started= 消歧）。
                        只读查询面（读门 = r(buildRepo, buildName) 镜像；名单
                        面服务端可见集过滤，页面自身不设门） */}
                    <Route path="builds" element={<BuildsPage />} />
                    <Route path="builds/:name" element={<BuildsPage />} />
                    <Route path="builds/:name/:number" element={<BuildsPage />} />
                    {/* Release Bundles（T-514）：三视图一组件——名单 / 版本单
                        （:name）/ 描述符（:name/:version）。应用域只读查询面
                        （读门 = 系统读权限 ∨ Any Distribution 通道；普通用户
                        名单 200 空集走空态——服务端可见集过滤，页面自身不设门） */}
                    <Route path="bundles" element={<BundlesPage />} />
                    <Route path="bundles/:name" element={<BundlesPage />} />
                    <Route path="bundles/:name/:version" element={<BundlesPage />} />
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
                    {/* T-443（FR-143.4，B-3.8）：建仓入口分路由——三静态路由
                        承 rclass prop（Artifactory 7.161.20 实测
                        /ui/admin/repositories/<rclass>/new 同构；非法段不匹配
                        落 * 通配 404，与 Artifactory 未知 rclass 404 同姿）。
                        静态三段不与 :key / :key/edit 竞争（段深不同）。 */}
                    <Route
                      path="admin/repositories/local/new"
                      element={<RepositoryFormPage mode="create" rclass="local" />}
                    />
                    <Route
                      path="admin/repositories/remote/new"
                      element={<RepositoryFormPage mode="create" rclass="remote" />}
                    />
                    <Route
                      path="admin/repositories/virtual/new"
                      element={<RepositoryFormPage mode="create" rclass="virtual" />}
                    />
                    {/* /new 直链兼容映射（T-443）：旧深链（?rclass= 形态 =
                        quick 菜单与 7 处跨页入口）一次性 replace 到分路由；
                        路由表是兼容层的唯一落点——7 个跨页 emitter（AppShell
                        quick 菜单 / Dashboard / Deploy / SetMeUp / Artifacts /
                        Quotas / StorageSummary）零改动跟进。 */}
                    <Route path="admin/repositories/new" element={<RepoCreateCompat />} />
                    <Route path="admin/repositories/:key" element={<RepoDetailPage />} />
                    <Route
                      path="admin/repositories/:key/edit"
                      element={<RepositoryFormPage mode="edit" />}
                    />

                    {/* —— 管理模式：用户与权限 —— */}
                    <Route path="admin/security/users" element={<UsersPage />} />
                    {/* T-453（FR-145.1，断言反转④）：创建 = 整页路由表单——
                        /users/new 深链（7.161.20 实测 /ui/admin/management/
                        users/new 同构）；静态段深于 :name 段，React Router
                        按 specificity 排序静态优先，「new」永不落入 :name。 */}
                    <Route path="admin/security/users/new" element={<UserCreatePage />} />
                    <Route path="admin/security/users/:name" element={<UserDetailPage />} />
                    <Route path="admin/security/groups" element={<GroupsPage />} />
                    {/* T-453：组创建/编辑同场路由化（7.161 /groups/new 同构；
                        编辑 = :name/edit——组内联卡创建/编辑同卡一并退役） */}
                    <Route path="admin/security/groups/new" element={<GroupFormPage mode="create" />} />
                    <Route path="admin/security/groups/:name/edit" element={<GroupFormPage mode="edit" />} />
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

                    {/* —— 管理模式：治理（T-459 重排：维护·备份迁监控组 /
                         Webhooks 迁常规组——旧深链经下方兼容映射折入） —— */}
                    <Route path="admin/governance/audit" element={<AuditPage />} />
                    <Route path="admin/governance/quotas" element={<QuotasPage />} />
                    <Route path="admin/governance/replication" element={<ReplicationPage />} />
                    {/* 回收站（T-352）：trashcan 槽门控态 + 浏览/恢复/清空 */}
                    <Route path="admin/governance/trash" element={<TrashPage />} />

                    {/* —— 管理模式：监控（T-459 扩为服务节点组六页：存储 +
                         服务状态 + 系统日志 + 系统信息〔自常规组归位〕 +
                         维护〔GC〕/ 备份恢复〔自治理组迁入——服务级页挂
                         服务节点组，T-462 的 cron 消费面同场〕）—— */}
                    <Route path="admin/monitoring/storage" element={<StorageSummaryPage />} />
                    <Route path="admin/monitoring/status" element={<ServiceStatusPage />} />
                    <Route path="admin/monitoring/logs" element={<SystemLogsPage />} />
                    <Route path="admin/monitoring/system-info" element={<SystemInfoPage />} />
                    <Route path="admin/monitoring/gc" element={<GCPage />} />
                    <Route path="admin/monitoring/backup" element={<BackupPage />} />

                    {/* —— 管理模式：常规（T-459：Webhooks 自治理组迁入；
                         系统信息已归监控组）—— */}
                    <Route path="admin/general/webhooks" element={<WebhooksPage />} />
                    <Route path="admin/general/license" element={<LicenseAddonsPage />} />

                    {/* T-459 路由迁移兼容窗（一轮——旧深链 replace 折入新址，
                         T-434 ?focus= 同款纪律；仓内 emitter 已随票翻新）：
                         general/settings → monitoring/system-info；
                         governance/{gc,backup} → monitoring/{gc,backup}；
                         governance/webhooks → general/webhooks */}
                    <Route
                      path="admin/general/settings"
                      element={<Navigate to="/admin/monitoring/system-info" replace />}
                    />
                    <Route
                      path="admin/governance/gc"
                      element={<Navigate to="/admin/monitoring/gc" replace />}
                    />
                    <Route
                      path="admin/governance/backup"
                      element={<Navigate to="/admin/monitoring/backup" replace />}
                    />
                    <Route
                      path="admin/governance/webhooks"
                      element={<Navigate to="/admin/general/webhooks" replace />}
                    />

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
})
