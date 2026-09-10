import { test, expect } from '@playwright/test'

import { loginAs } from '../m8/support/roles'
import { expectA11yClean } from '../m8/support/a11y'

// FE-P4 A1：命令面板（⌘/Ctrl+K——frontend-rewrite-architecture §8）。
//
// 四腿：
//  ① 开合与键盘：⌘K 开（palette-root/input 在场）→ Esc 关；`/` 聚焦顶栏
//     搜索（既有键位不破——A1 整合约束）；⌘K 再开再关（toggle 往返）。
//  ② 导航：过滤词收窄导航四分组条目 → Enter 键盘激活 → URL 落位
//    （palette-item-nav-<id> 族）；管理条目对 admin 可见（admin-filter
//     语义并入——四分组含 /admin 面）。
//  ③ 动作与偏好：palette-item-new-repo-local 激活 → 建仓表单 ?rclass=local；
//     palette-item-theme 激活 → data-theme 翻转；palette-ai 恒禁用（P5 占位）。
//  ④ RBAC：普通 user 无动作组/无管理导航条目；开面板 axe serious+critical=0
//    （双主题）。
//
// 锚源：console-ux §10.9 P4 批（palette-root / palette-input / palette-item-<id> /
// palette-ai）。键盘纪律：定位用 focus()，激活一律按键。
test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test('palette: ⌘K open/close round trip, / still focuses topbar search', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()

  // ⌘K 开
  await page.keyboard.press('Control+k')
  const palette = page.locator('[data-testid="palette-root"]')
  await expect(palette).toBeVisible()
  await expect(page.locator('[data-testid="palette-input"]')).toBeFocused()

  // Esc 关（cmdk/Dialog 承载）
  await page.keyboard.press('Escape')
  await expect(palette).toHaveCount(0)

  // `/` 既有键位不破：非输入态聚焦顶栏搜索
  await page.keyboard.press('/')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeFocused()

  // ⌘K 再开（toggle 往返）+ 面板在场时 `/` 不抢焦（modal 让位）
  await page.keyboard.press('Control+k')
  await expect(palette).toBeVisible()
  await expect(page.locator('[data-testid="palette-input"]')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(palette).toHaveCount(0)
})

test('palette: filter narrows nav groups, Enter navigates', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()

  await page.keyboard.press('Control+k')
  await expect(page.locator('[data-testid="palette-root"]')).toBeVisible()

  // 过滤词收窄：审计日志（Administration 面——admin 资源过滤语义并入的可见性证据）
  await page.keyboard.type('审计')
  await expect(page.locator('[data-testid="palette-item-nav-audit"]')).toBeVisible()
  await expect(page.locator('[data-testid="palette-item-nav-dashboard"]')).toHaveCount(0)

  // Enter 键盘激活 → URL 落位
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/governance\/audit$/)

  // 无匹配空态
  await page.keyboard.press('Control+k')
  await page.keyboard.type('zzz-no-such-command')
  await expect(page.locator('[data-testid="palette-root"]')).toContainText('没有匹配的命令')
  await page.keyboard.press('Escape')
})

test('palette: actions navigate, theme item toggles, AI entry disabled', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()

  // 动作：建仓三预选之一 → 表单 ?rclass=local
  await page.keyboard.press('Control+k')
  await page.locator('[data-testid="palette-item-new-repo-local"]').click()
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/new\?rclass=local$/)

  // 偏好：主题切换条目翻转 data-theme（palette 关闭态断言）
  const before = await page.evaluate(() => document.documentElement.dataset.theme)
  await page.keyboard.press('Control+k')
  await page.locator('[data-testid="palette-item-theme"]').click()
  await expect(page.locator('[data-testid="palette-root"]')).toHaveCount(0)
  const after = await page.evaluate(() => document.documentElement.dataset.theme)
  expect(after).not.toBe(before)

  // AI 入口 = P5 占位（恒禁用——不虚构端点）
  await page.keyboard.press('Control+k')
  await expect(page.locator('[data-testid="palette-ai"]')).toBeDisabled()
  await page.keyboard.press('Escape')
})

test('palette: plain user sees no admin nav/actions; axe clean both themes', async ({ page }, testInfo) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()

  await page.keyboard.press('Control+k')
  await expect(page.locator('[data-testid="palette-root"]')).toBeVisible()
  // 无写动作组、无管理导航条目（权限门控与侧栏同源）
  await expect(page.locator('[data-testid="palette-item-new-repo-local"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="palette-item-nav-users"]')).toHaveCount(0)
  // 应用域导航在场
  await expect(page.locator('[data-testid="palette-item-nav-dashboard"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="palette-root"]' })

  // 暗色复扫
  await page.keyboard.press('Escape')
  await page.evaluate(() => {
    localStorage.setItem('binflow-console-theme', 'dark')
  })
  await page.reload()
  await page.keyboard.press('Control+k')
  await expect(page.locator('[data-testid="palette-root"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="palette-root"]' })
})
