import { expect, test } from '@playwright/test'

import { loginAs } from './support/roles'
import { m8Client, seedRepos } from './support/seed'
import { expectA11yClean } from './support/a11y'

// T-244 键盘全链（FR-75 / console-m8 §3.4 键盘清单 + §9）：登录 → 导航 →
// 树（方向键/Enter/Shift+F10）→ 表格行（Enter 激活 + ↑↓ 移动）→ Tab 组件
// （tablist 方向键）→ 对话框（焦点陷阱/Esc/禁用钮不破口）→ 用户菜单
// quick-set-me-up 全局接线（T-244 共享层债③）。附：badge 对比度家族修复
// （债①）后的 axe 双主题复扫——serious/critical = 0 维持。
//
// 键盘纪律（README §2.4）：focus() 定位 + 一切激活用按键；断言只用
// toBeFocused / aria-* 属性 / URL，不用坐标。

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

// ---- 1. 登录全键盘 + 侧栏键盘导航 ----------------------------------------------

test('keyboard: login by keys, sidebar reachable and activating via Enter', async ({ page }) => {
  const key = uniq('kb-nav')
  const client = m8Client()
  await seedRepos(client, [{ key }])

  await page.goto('/binflow/ui/')
  await expect(page.locator('[data-testid="login-username"]')).toBeFocused()
  await page.keyboard.type(ADMIN)
  await page.keyboard.press('Tab')
  await expect(page.locator('[data-testid="login-password"]')).toBeFocused()
  await page.keyboard.type(ADMIN_PW)
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()

  // 登录后首个 Tab 落点在侧栏（nav 在 DOM 序先于 main）——不假设具体哪项，
  // 只断言焦点进入导航区，且 Enter 能激活当前聚焦的导航项（路由变化）。
  await page.keyboard.press('Tab')
  const inNav = await page.evaluate(() => !!document.activeElement?.closest('[data-testid="app-nav"]'))
  expect(inNav).toBe(true)

  // FE-Rewrite P2 四分组壳：模式切换概念退役（分组即模式）——键盘面改为
  // 直接驱动管理分组条目（focus 仓库 → Enter 落 /admin/repositories/）。
  await page.focus('[data-testid="app-nav"] a.nav-item:text-is("仓库")')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories/)
})

// ---- 2. 树方向键全语义（↑↓ sibling / → 展开 / ← 折叠 / Enter / Shift+F10）------

test('keyboard: tree arrows expand/collapse/navigate, row Enter opens, Shift+F10 menu', async ({ page }) => {
  const key = uniq('kb-tree')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/docs/guide.md`, { raw: true, body: 'kb\n' })
  // 第二个仓：↑ 的 sibling 腿需要
  await seedRepos(client, [{ key: `${key}-b` }])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toBeVisible()

  // ↓ 移动到下一个仓库节点（sibling；不假设紧邻是谁——实例上还有其它
  // 测试仓，只断言焦点移到了另一枚 tree-repo 节点，↑ 回到原节点）
  await page.focus(`[data-testid="tree-repo-${key}"]`)
  await page.keyboard.press('ArrowDown')
  const moved = await page.evaluate(() => document.activeElement?.getAttribute('data-testid') ?? '')
  expect(moved).toMatch(/^tree-repo-/)
  expect(moved).not.toBe(`tree-repo-${key}`)
  await page.keyboard.press('ArrowUp')
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toBeFocused()

  // → 展开：进入 -b 仓让目录加载效果激活（跨仓根 repoKey='' 时懒加载不
  // 拉仓外节点——展开只是视觉态），焦点回到 key 仓节点后 → 拉子层
  await page.goto(`/binflow/ui/artifacts/${key}-b`)
  await expect(page.locator(`[data-testid="tree-repo-${key}-b"]`)).toBeVisible()
  await page.focus(`[data-testid="tree-repo-${key}"]`)
  await page.keyboard.press('ArrowRight')
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toHaveAttribute('aria-expanded', 'true')
  await expect(page.locator('[data-testid="tree-node-docs"]')).toBeVisible()
  await page.keyboard.press('ArrowDown')
  await expect(page.locator('[data-testid="tree-node-docs"]')).toBeFocused()
  await page.keyboard.press('Enter') // 目录 Enter = 进路径（URL 即状态）
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${key}/docs$`))
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()

  // ← 收起：page.goto 是整页装载（组件重挂载、expanded 归零），在 -b 仓
  // 上下文里重走 → 展开 → ← 收起（选中仓是 -b，key 仓不被钉住）
  await page.goto(`/binflow/ui/artifacts/${key}-b`)
  await page.focus(`[data-testid="tree-repo-${key}"]`)
  await page.keyboard.press('ArrowRight')
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toHaveAttribute('aria-expanded', 'true')
  await page.keyboard.press('ArrowLeft')
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toHaveAttribute('aria-expanded', 'false')

  // 表格行：↑↓ 行移动 + 目录行 Enter 进路径 + 文件行 Enter 选中（T-434：文件
  // 选中进 URL 路径末段——?focus= 退役）
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="tree-row-docs"]')).toBeVisible()
  await page.focus('[data-testid="tree-row-docs"]')
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('ArrowUp')
  await expect(page.locator('[data-testid="tree-row-docs"]')).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${key}/docs$`))
  await page.focus('[data-testid="tree-row-guide.md"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${key}/docs/guide\\.md$`))
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()

  // Shift+F10：右键菜单键盘打开（Chromium 报 F10+shift）+ Esc 关闭
  await page.focus('[data-testid="tree-row-guide.md"]')
  await page.keyboard.press('Shift+F10')
  await expect(page.locator('[data-testid="tree-context-menu"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="tree-context-menu"]')).toHaveCount(0)
})

// ---- 3. 管理表格行 Enter + ↑↓（共享 onTableRowKeys，repos/users 双页实证）------

test('keyboard: admin table rows move by arrows and activate by Enter', async ({ page }) => {
  const keyA = uniq('kb-row-a')
  const keyB = uniq('kb-row-b')
  const client = m8Client()
  await seedRepos(client, [
    { key: keyA },
    { key: keyB },
  ])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator(`[data-testid="repos-row-${keyA}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="repos-row-${keyB}"]`)).toBeVisible()

  await page.focus(`[data-testid="repos-row-${keyA}"]`)
  await page.keyboard.press('ArrowDown')
  const moved = await page.evaluate(() => document.activeElement?.getAttribute('data-testid') ?? '')
  expect(moved).toMatch(/^repos-row-/)
  expect(moved).not.toBe(`repos-row-${keyA}`)
  await page.keyboard.press('ArrowUp')
  await expect(page.locator(`[data-testid="repos-row-${keyA}"]`)).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${keyA}$`))
  await expect(page.locator('[data-testid="repo-detail-page"]')).toBeVisible()

  // 用户表同款（共享 helper 双页实证）
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  const firstUser = page.locator('[data-testid="users-table"] tbody tr').first()
  await firstUser.focus()
  await page.keyboard.press('ArrowDown')
  const second = page.locator('[data-testid="users-table"] tbody tr').nth(1)
  await expect(second).toBeFocused()
})

// ---- 4. Tab 组件方向键（repo-tabs / node-tabs，共享 onTablistKeys）------------

test('keyboard: tablist arrow keys switch repo detail tabs and node detail tabs', async ({ page }) => {
  const key = uniq('kb-tab')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/docs/guide.md`, { raw: true, body: 'kb\n' })

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}`)
  await expect(page.locator('[data-testid="repo-tab-summary"]')).toBeVisible()

  await page.focus('[data-testid="repo-tab-summary"]')
  await page.keyboard.press('ArrowRight')
  await expect(page.locator('[data-testid="repo-tab-config"]')).toHaveAttribute('aria-selected', 'true')
  await expect(page.locator('[data-testid="repo-tab-config"]')).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(page.locator('[data-testid="repo-tab-replications"]')).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(page.locator('[data-testid="repo-tab-config"]')).toHaveAttribute('aria-selected', 'true')

  // 树详情面板 node-tabs：常规 → 有效权限 → 属性（T-445 / FR-144.1 页签序
  // ——权限在属性前，7.161.20 活体 A2-7 + 7.84 reverse §3.2 逐级一致；
  // T-291 的属性 Tab 与 T-236 的权限 Tab 渲染序互换，锚不变）
  await page.goto(`/binflow/ui/artifacts/${key}/docs`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await page.focus('[data-testid="node-tab-general"]')
  await page.keyboard.press('ArrowRight')
  await expect(page.locator('[data-testid="node-tab-perms"]')).toHaveAttribute('aria-selected', 'true')
  await expect(page.locator('[data-testid="node-perms"]')).toBeVisible()
  await page.keyboard.press('ArrowRight')
  await expect(page.locator('[data-testid="node-tab-props"]')).toHaveAttribute('aria-selected', 'true')
  await expect(page.locator('[data-testid="node-props"]')).toBeVisible()
})

// ---- 5. 对话框焦点陷阱 + Esc + 禁用钮不破口 + quick-set-me-up 接线 ------------

test('keyboard: dialog focus trap wraps, Esc closes, disabled buttons do not break the trap', async ({ page }) => {
  const key = uniq('kb-dlg')
  const client = m8Client()
  await seedRepos(client, [{ key }])

  await loginAs(page, 'admin')

  // quick-set-me-up 全局接线（T-244 债③）：用户菜单 → Set Me Up 开全局
  // 对话框（无仓库上下文 → 步骤 0 网格）；Esc 关闭回焦菜单钮
  await page.goto('/binflow/ui/artifacts')
  await page.click('[data-testid="session-toggle"]')
  await page.focus('[data-testid="quick-set-me-up"]')
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="smu-dialog"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="smu-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="session-toggle"]')).toBeFocused()

  // Deploy 对话框：陷阱在禁用的「部署」钮（空队列）上不破口——Tab 首尾
  // 循环只经可聚焦控件；Esc 关闭
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await page.click('[data-testid="tree-deploy"]')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="deploy-submit"]')).toBeDisabled()
  await page.focus('[data-testid="deploy-close"]')
  await page.keyboard.press('Tab') // 尾（最后一个可聚焦=关闭）→ 首（仓库下拉）
  await expect(page.locator('[data-testid="deploy-repo"]')).toBeFocused()
  await page.keyboard.press('Shift+Tab') // 首 → 尾
  await expect(page.locator('[data-testid="deploy-close"]')).toBeFocused()
  // 连打 Tab 十次焦点仍在对话框内（陷阱不漏）
  for (let i = 0; i < 10; i++) await page.keyboard.press('Tab')
  const trapped = await page.evaluate(
    () => !!document.activeElement?.closest('[data-testid="deploy-dialog"]'),
  )
  expect(trapped).toBe(true)
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toHaveCount(0)

  // ConfirmDialog：打开即聚焦取消（安全默认）；Esc = 取消
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="tree-mkdir"]')).toBeVisible()
  await page.focus('[data-testid="tree-mkdir"]')
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="confirm-cancel"]')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toHaveCount(0)
})

// ---- 6. axe 双主题复扫（badge 对比度家族修复后 serious=0 维持）------------------

test('axe: badge-heavy pages clean in both themes after the contrast family fix', async ({ page }, testInfo) => {
  test.setTimeout(480_000) // 8 次扫描 + 8 次显式主题装载/导航（重表页）；串行时代 90s 在默认并发（T-268）下两轮实测 1.7m / 3.1m 仍超，对齐 a11y-sweep 的多扫描预算量级
  const key = uniq('kb-axe')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/docs/guide.md`, { raw: true, body: 'axe\n' })

  await loginAs(page, 'admin')
  // 亮色（默认）与暗色各扫四个 badge 密集页：树（rclass/untagged/类型徽章）、
  // 仓库列表（包类型/类型）、用户（角色徽章）、仪表盘（状态点+徽章）
  const pages = [
    { url: `/binflow/ui/artifacts/${key}/docs`, scope: '[data-testid="tree-page"]' },
    { url: '/binflow/ui/admin/repositories/local', scope: '[data-testid="repos-page"]' },
    { url: '/binflow/ui/admin/security/users', scope: '[data-testid="users-page"]' },
    { url: '/binflow/ui/dashboard', scope: '[data-testid="dashboard"]' },
  ]
  for (const theme of ['light', 'dark'] as const) {
    for (const p of pages) {
      // 主题显式落盘（ThemeContext 读 localStorage；不依赖上一腿的翻转残留）
      await page.goto('/binflow/ui/artifacts')
      await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
      await page.goto(p.url)
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      await page.waitForTimeout(300) // 卡片独立到达
      await expectA11yClean(page, testInfo, { include: p.scope })
    }
  }
})
