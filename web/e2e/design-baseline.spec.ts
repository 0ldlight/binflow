import { mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { expect, test } from '@playwright/test'

import { scanA11y } from './m8/support/a11y'

// UI Phase 1 批 0（design-system-plan §6）：Playwright 截图基线 + axe 双主题现值。
//
// 目的：冻结「当前值 golden」——批 1（token 收敛）/批 5（旧 CSS 退役）的
// 「截图差分≈0」验收门以此为底；批 2/3 集中承受 G4 视觉变更时在此翻新。
//
// 运行（对 dev 实例——拓扑见 docs/ai-engineering/loop-state.yaml environment_topology）：
//   BASE=http://172.16.58.130:8083 ADMIN_PW=password \
//     npx playwright test e2e/design-baseline.spec.ts --update-snapshots
// 建基线用 --update-snapshots；此后裸跑即为差分门（缺基线/像素漂移即红）。
// golden 落 web/e2e/__screenshots__/（go:embed 只吞 dist/——不进二进制）；
// axe 现值 JSON 落 docs/reverse/frontend/binflow-baseline/（与 parity-capture 对称）。
//
// 已知妥协：动态区块（时间戳/相对时间）暂不 mask——批 1 差分若现噪声再补
// mask 清单（本文件头部登记），不在批 0 预支。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

// 核心页清单：覆盖全部侧栏组 + 大件（树/表格/表单）。增删页在此一处改。
// mask/容差 = 已知动态面（spec 自身登录写审计行——好行为，不该红基线）。
const ROUTES: Array<{
  name: string
  path: string
  ready?: string
  mask?: string[]
  maxDiffPixels?: number
}> = [
  {
    name: 'dashboard',
    path: '/binflow/ui/dashboard',
    ready: '[data-testid="dashboard-instance-card"]',
    mask: ['[data-testid="dashboard-audit-card"]'],
    // 实例卡存储字节计数随测试活动单调增长（数字位漂移非布局）
    maxDiffPixels: 12_000,
  },
  { name: 'explorer', path: '/binflow/ui/artifacts', ready: '[data-testid="tree-page"]' },
  { name: 'search', path: '/binflow/ui/search' },
  { name: 'builds', path: '/binflow/ui/builds' },
  { name: 'repos-local', path: '/binflow/ui/admin/repositories/local' },
  { name: 'repos-remote', path: '/binflow/ui/admin/repositories/remote' },
  { name: 'repos-virtual', path: '/binflow/ui/admin/repositories/virtual' },
  { name: 'repo-new-local', path: '/binflow/ui/admin/repositories/local/new' },
  { name: 'repo-detail', path: '/binflow/ui/admin/repositories/demo-local' },
  // admin 行 lastLogin 秒级时间戳随本 spec 登录滚动——mask 该单元格
  { name: 'users', path: '/binflow/ui/admin/security/users', mask: ['tr[data-testid="user-row-admin"] td[title]'] },
  { name: 'groups', path: '/binflow/ui/admin/security/groups' },
  { name: 'permissions', path: '/binflow/ui/admin/security/permissions' },
  // 审计页顶行 = 本 spec 两次登录的滚动新事件（~2 行位移），预算容纳
  { name: 'audit', path: '/binflow/ui/admin/governance/audit', maxDiffPixels: 4000 },
  // 存储页字节计数器随测试活动滚动（~40px 数字位）
  { name: 'storage', path: '/binflow/ui/admin/monitoring/storage', maxDiffPixels: 2_000 },
  { name: 'status', path: '/binflow/ui/admin/monitoring/status' },
]

const THEMES = ['light', 'dark'] as const
const AXE_DIR = fileURLToPath(new URL('../../docs/reverse/frontend/binflow-baseline', import.meta.url))

test.use({ viewport: { width: 1440, height: 900 } })

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test('login page baseline (logged out)', async ({ page }) => {
  await page.goto('/binflow/ui/login?return=%2F')
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await page.waitForTimeout(250)
  await expect(page).toHaveScreenshot('login.png')
})

for (const theme of THEMES) {
  test(`app baseline (${theme})`, async ({ page, request }) => {
    // 会话走 API 直登（cookie 注入），UI 表单腿归 login/auth-shell 族——
    // 本 spec 只管像素与 axe，不重复交互断言。
    const res = await request.post('/binflow/api/v1/session', {
      data: { username: ADMIN, password: ADMIN_PW },
    })
    const session = /binflow_session=([0-9a-f]+)/.exec(res.headers()['set-cookie'] ?? '')
    test.skip(!session, 'login failed — check ADMIN_USER/ADMIN_PW against the target instance')
    await page.context().addCookies([
      { name: 'binflow_session', value: session![1], url: new URL(res.url()).origin },
    ])
    // 主题钉死（不随宿主 prefers-color-scheme 漂移）
    await page.context().addInitScript((t) => localStorage.setItem('binflow-console-theme', t), theme)

    const axe: unknown[] = []
    for (const r of ROUTES) {
      await page.goto(r.path)
      if (r.ready) await page.locator(r.ready).first().waitFor({ timeout: 15_000 })
      else await page.waitForLoadState('networkidle')
      await page.waitForTimeout(300) // 字体/过渡沉降

      await expect(page).toHaveScreenshot(`${r.name}-${theme}.png`, {
        ...(r.mask ? { mask: r.mask.map((s) => page.locator(s)) } : {}),
        ...(r.maxDiffPixels ? { maxDiffPixels: r.maxDiffPixels } : {}),
      })

      // axe 现值（impact: null = 全谱记录，不做断言——批 0 是底片不是闸门）
      const results = await scanA11y(page, { impact: null })
      axe.push({
        route: r.name,
        url: page.url(),
        violations: results.violations.map((v) => ({
          id: v.id,
          impact: v.impact,
          help: v.help,
          nodes: v.nodes.length,
        })),
        passes: results.passes.length,
        incomplete: results.incomplete.length,
      })
    }
    mkdirSync(AXE_DIR, { recursive: true })
    writeFileSync(
      `${AXE_DIR}/axe-current-${theme}.json`,
      JSON.stringify({ capturedAt: new Date().toISOString(), theme, pages: axe }, null, 2),
    )
  })
}
