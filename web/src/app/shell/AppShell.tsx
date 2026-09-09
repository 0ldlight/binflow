// 应用壳（新壳——architecture §4：侧栏（四分组+权限可见性）+ Topbar +
// Main）。认证守卫：checking → boot-screen；anonymous → /login?return=
// （AuthProvider 在根路由层常驻——本壳只做守卫与布局）。
//
// Set Me Up 全局对话框（含 OIDC step-up 回跳续铸）以 LegacyDialogHost
// 挂载——MUI 树的旧组件，P4 重写后随桥退役（终验强删项清单成员）。
import { lazy, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { LegacyDialogHost } from '@/components/layout/legacy-host'
import { isReadOnlyAdmin } from '@/lib/api'
import { useStepUp, abandonStepUp } from '@/lib/stepUpGrant'
import type { PendingMint } from '@/lib/stepUpGrant'
import { useVersion } from '@/lib/useVersion'
import { tr } from '@/i18n'

import { adminCrumbs, appTitle } from './breadcrumbs'
import { NAV_GROUPS } from './nav-model'
import { Sidebar } from './Sidebar'
import { Topbar } from './Topbar'

const t = tr('console')

// MUI 树旧组件的懒分片（P4 重写退役——终验强删项清单成员）
const SetMeUpDialog = lazy(() => import('@/components/SetMeUpDialog'))

export function AppShell() {
  const { status, session } = useAuth()
  const location = useLocation()
  const version = useVersion()

  const admin = session?.admin ?? false
  const readOnlyAdmin = isReadOnlyAdmin(session)
  const canSeeAdmin = admin || readOnlyAdmin

  // 管理资源过滤（/admin 域顶栏过滤框 → 侧栏条目客户端子串过滤）
  const [adminFilter, setAdminFilter] = useState('')
  const onAdminFilter = useCallback((term: string) => setAdminFilter(term), [])
  const inAdminArea = location.pathname.startsWith('/admin')
  const adminMode = inAdminArea && canSeeAdmin

  const groups = useMemo(() => {
    const visible = NAV_GROUPS.map((g) => ({
      ...g,
      items: g.items.filter((i) => i.visibility === 'all' || canSeeAdmin),
    }))
    const q = adminMode ? adminFilter.trim().toLowerCase() : ''
    // 无权限条目清空后整组退役（普通 user 不见空的安全/管理组标签）；
    // 管理过滤词下清空 = admin-filter-empty 注记承载（Sidebar 渲染）
    const filtered = q
      ? visible.map((g) => ({ ...g, items: g.items.filter((i) => i.label.toLowerCase().includes(q)) }))
      : visible
    return filtered.filter((g) => g.items.length > 0 || q !== '')
  }, [canSeeAdmin, adminMode, adminFilter])

  // ---- About 弹窗（侧栏脚注 nav-about 与顶栏 help-about 同一入口） ----
  const [aboutOpen, setAboutOpen] = useState(false)

  // ---- Set Me Up 全局对话框 + OIDC step-up 回跳续铸 ----
  const [smuOpen, setSmuOpen] = useState(false)
  const openerRef = useRef<HTMLElement | null>(null)
  const openSetMeUp = useCallback((focus: HTMLElement | null) => {
    openerRef.current = focus
    setSmuOpen(true)
  }, [])

  const stepUp = useStepUp()
  const [resumeOpen, setResumeOpen] = useState(false)
  const resumeCtxRef = useRef<PendingMint | null>(null)
  useEffect(() => {
    if (!stepUp.grant || !stepUp.pending) return
    resumeCtxRef.current = stepUp.pending
    setResumeOpen(true)
  }, [stepUp.grant, stepUp.pending])
  useEffect(() => {
    if (resumeOpen && status === 'authenticated') setSmuOpen(true)
  }, [resumeOpen, status])
  const smuMounted = smuOpen || (resumeOpen && status === 'authenticated')

  if (status === 'checking') {
    return (
      <div className="boot-screen">
        <span
          aria-hidden="true"
          className="mr-2 inline-block size-4 animate-spin rounded-full border-2 border-border border-t-primary align-[-2px]"
        />
        {t('正在验证会话…')}
      </div>
    )
  }

  if (status === 'anonymous') {
    const target = location.pathname + location.search
    const safe = target.startsWith('/') && !target.startsWith('//') ? target : '/'
    return <Navigate to={`/login?return=${encodeURIComponent(safe)}`} replace />
  }

  const crumbs = adminMode || inAdminArea ? adminCrumbs(location.pathname) : null

  return (
    <div data-slot="app-shell" className="flex h-screen w-full overflow-hidden bg-background text-foreground">
      <aside className="w-56 shrink-0 overflow-y-auto bg-sidebar text-sidebar-foreground">
        <Sidebar
          groups={groups}
          version={version ? version.version : null}
          onAbout={() => setAboutOpen(true)}
          adminFilter={adminFilter}
        />
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <Topbar
          crumbs={crumbs}
          title={appTitle(location.pathname)}
          adminMode={adminMode}
          onAdminFilter={onAdminFilter}
          onOpenSetMeUp={openSetMeUp}
          aboutOpen={aboutOpen}
          onAboutOpenChange={setAboutOpen}
        />
        <main data-slot="app-main" className="min-h-0 flex-1 overflow-y-auto px-5 py-5">
          <div className="mx-auto max-w-[1440px]">
            <Outlet />
          </div>
        </main>
      </div>
      {smuMounted && (
        <LegacyDialogHost>
          <SetMeUpDialog
            preselectedRepo={resumeOpen ? resumeCtxRef.current?.repo : undefined}
            resume={resumeOpen ? resumeCtxRef.current : null}
            onClose={() => {
              if (resumeOpen) {
                abandonStepUp()
                setResumeOpen(false)
              }
              setSmuOpen(false)
              openerRef.current?.focus()
            }}
          />
        </LegacyDialogHost>
      )}
    </div>
  )
}
