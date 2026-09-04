import { expect, test } from '@playwright/test'
import type { TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-414（M15 B1 FE，FR-135.2/135.3）——列选器三页推广（T-387 spec 形态
// 复用：users / groups / search 三列表页）+ repositories virtual Tab
// member-pop hover 对比度清账（T-391 L-a 遗留——color-mix 同配方）。
//
// 断言面（M15-SPLIT §1.2 AC + BOARD）：
//   ① 三页列选开合：触发钮 aria-haspopup/aria-expanded；Menu 开（项可见）
//      /Esc 关（回焦）；菜单项 = menuitemcheckbox + aria-checked。
//   ② 列显隐：弃一列 → 表头/行单元格同步 -1；勾回 +1；reload 持久
//      （per-page localStorage 键 binflow-console-cols-{users,groups,search}，
//      三键互不染）。
//   ③ 至少一列守卫 + 全选复位（存储回 []）。
//   ④ 「无端点列不伪造」：users 六列 / groups 四列（admin 视角，含操作列）/
//      search 五列 = 既有真实列闭集。
//   ⑤ member-pop（AC2）：T-391 配方在身（computed color ≠ 裸 accent——
//      回归腿，配方退役即红）+ 行 hover 态 axe 双主题 serious=0。
//   ⑥ 三页列选菜单开态 axe 双主题 serious=0。
//
// 锚源：console-ux §10.5 T-414 批（{users|groups|search}-columns{,-menu,-reset}
// + -item-{…} 逐列项——28 名全静态锚，anchor: 属性字面量形态）；三页既有
// 列锚（users-sort-* 等）零改名。选择器全字面量（对账器口径同 t387）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 列选菜单开合三件套（①）：aria 语义 + 项数 + 全勾初值。选择器全部由调用
 *  方以**字面量**传入（对账器口径：模板透传形不可见——t387 先例同款纪律）；
 *  返回菜单定位器。 */
async function openColumnsMenu(
  page: import('@playwright/test').Page,
  triggerSel: string,
  menuSel: string,
  itemSels: readonly string[],
) {
  const t = page.locator(triggerSel)
  await expect(t).toHaveAttribute('aria-haspopup', 'menu')
  await expect(t).toContainText(`列 ${itemSels.length}/${itemSels.length}`)
  await t.click()
  await expect(t).toHaveAttribute('aria-expanded', 'true')
  const m = page.locator(menuSel)
  await expect(m).toBeVisible()
  await expect(m.locator('[role="menuitemcheckbox"]')).toHaveCount(itemSels.length)
  for (const sel of itemSels) {
    await expect(page.locator(sel)).toHaveAttribute('aria-checked', 'true')
  }
  return m
}

/** 关合腿（①另一侧）：Esc 关菜单 + 回焦触发钮（字面量选择器同上） */
async function closeColumnsMenu(page: import('@playwright/test').Page, menuSel: string, triggerSel: string) {
  await page.keyboard.press('Escape')
  await expect(page.locator(menuSel)).toBeHidden()
  const t = page.locator(triggerSel)
  await expect(t).toHaveAttribute('aria-expanded', 'false')
  await expect(t).toBeFocused()
}

/** hex → rgb(...) 归一（⑤ computed 采样的比对形：--bf-accent 原值是 hex） */
function hexToRgb(hex: string): string {
  const h = hex.trim().replace('#', '')
  const n = parseInt(h.length === 3 ? h.split('').map((c) => c + c).join('') : h, 16)
  return `rgb(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255})`
}

// ---- 1. users 页列选全生命周期（①②③④） ---------------------------------------

test('admin: users column selector — open/close, hide/show, guard, reset, per-page persistence', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  // 内置 admin 用户恒在场——行锚不依赖夹具
  await expect(page.locator('[data-testid="user-row-admin"]')).toBeVisible()

  // 默认全显（admin 视角六列闭集：用户名/Email/组/角色/Status/操作——④）
  const th = page.locator('[data-testid="users-table"] thead th')
  await expect(th).toHaveCount(6)

  // 开（①）+ 全勾（六列项逐名断言——字面量锚全消费）
  const USERS_ITEMS = [
    '[data-testid="users-columns-item-name"]',
    '[data-testid="users-columns-item-email"]',
    '[data-testid="users-columns-item-groups"]',
    '[data-testid="users-columns-item-role"]',
    '[data-testid="users-columns-item-status"]',
    '[data-testid="users-columns-item-actions"]',
  ] as const
  const menu = await openColumnsMenu(page, '[data-testid="users-columns"]', '[data-testid="users-columns-menu"]', USERS_ITEMS)

  // 弃「Email」（②）：表头 + 行单元格同步 -1；菜单保持开
  await page.click('[data-testid="users-columns-item-email"]')
  await expect(menu).toBeVisible()
  await expect(th).toHaveCount(5)
  await expect(th.filter({ hasText: 'Email' })).toHaveCount(0)
  await expect(page.locator('[data-testid="user-row-admin"] td')).toHaveCount(5)
  await expect(page.locator('[data-testid="users-columns"]')).toContainText('列 5/6')

  // 持久（②）：localStorage 落盘 + reload 保持；groups/search 键不被染
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-users'))).toContain('email')
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-groups'))).toBeNull()
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-search'))).toBeNull()
  await page.reload()
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  await expect(th).toHaveCount(5)

  // 勾回（②另一腿）
  await page.click('[data-testid="users-columns"]')
  await page.click('[data-testid="users-columns-item-email"]')
  await expect(page.locator('[data-testid="users-columns-item-email"]')).toHaveAttribute('aria-checked', 'true')
  await expect(th).toHaveCount(6)

  // 至少一列守卫（③）：弃到只剩用户名 → 该项 aria-disabled 且点击被拒
  for (const sel of USERS_ITEMS.slice(1)) {
    await page.click(sel)
  }
  await expect(page.locator('[data-testid="users-columns-item-name"]')).toHaveAttribute('aria-disabled', 'true')
  // aria-disabled 态 Playwright 可点性检查拒发事件——dispatchEvent 直发 DOM
  // click 验证 handler 层守卫（t387 同款实证口径）
  await page.dispatchEvent('[data-testid="users-columns-item-name"]', 'click')
  await expect(page.locator('[data-testid="users-columns-item-name"]')).toHaveAttribute('aria-checked', 'true')
  await expect(th).toHaveCount(1)
  await expect(page.locator('[data-testid="user-row-admin"] td')).toHaveCount(1)

  // 全选复位（③）：表头回 6 + 存储回空数组
  await page.click('[data-testid="users-columns-reset"]')
  await expect(th).toHaveCount(6)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-users'))).toBe('[]')

  // 关（①另一腿）：Esc + 回焦
  await closeColumnsMenu(page, '[data-testid="users-columns-menu"]', '[data-testid="users-columns"]')
})

// ---- 2. groups 页：列选 + 持久 + 复位 + 键互不染（①②③④） ---------------------

test('admin: groups column selector — hide persists across reload; reset; keys do not bleed', async ({ page }) => {
  // 备料：唯一组（表行在场的前提；PUT 覆盖幂等）
  const group = uniq('t414g')
  const client = m8Client()
  const created = await client.request('PUT', `/binflow/api/security/groups/${group}`, {
    body: { name: group, description: 't414 column selector fixture' },
  })
  expect(created.status).toBeLessThan(300)

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/groups')
  await expect(page.locator(`[data-testid="group-row-${group}"]`)).toBeVisible()

  // admin 视角四列闭集：组名/权限数/成员数/操作（④）
  const th = page.locator('[data-testid="groups-table"] thead th')
  await expect(th).toHaveCount(4)

  // 弃「权限数」→ 表头 -1 + 持久（reload 保持）；四列项逐名断言（④ 列集闭包）
  const GROUPS_ITEMS = [
    '[data-testid="groups-columns-item-name"]',
    '[data-testid="groups-columns-item-perms"]',
    '[data-testid="groups-columns-item-members"]',
    '[data-testid="groups-columns-item-actions"]',
  ] as const
  await openColumnsMenu(page, '[data-testid="groups-columns"]', '[data-testid="groups-columns-menu"]', GROUPS_ITEMS)
  await page.click('[data-testid="groups-columns-item-perms"]')
  await expect(th).toHaveCount(3)
  await expect(th.filter({ hasText: '权限数' })).toHaveCount(0)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-groups'))).toContain('perms')
  await page.reload()
  await expect(page.locator(`[data-testid="group-row-${group}"]`)).toBeVisible()
  await expect(th).toHaveCount(3)

  // 键互不染：groups 操作不触 users/search 键（本腿同一浏览器上下文）
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-users'))).toBeNull()
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-search'))).toBeNull()

  // 全选复位（菜单开态点 reset）
  await page.click('[data-testid="groups-columns"]')
  await page.click('[data-testid="groups-columns-reset"]')
  await expect(th).toHaveCount(4)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-groups'))).toBe('[]')
  await closeColumnsMenu(page, '[data-testid="groups-columns-menu"]', '[data-testid="groups-columns"]')
})

// ---- 3. search 页：列选（T-449 断言反转②——默认档 + 持久 + 恢复默认） ---------

test('admin: search column selector — size/sha256 opt-in by default, persists; reset restores the default set', async ({
  page,
}) => {
  // 备料：独立仓 + 唯一文件（auxiliary.spec seedSearchFixture 同款）
  const marker = `t414s${Date.now().toString(36)}`
  const repo = `${marker}-local`
  const file = `${marker}.bin`
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${repo}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  const put = await client.request('PUT', `/binflow/${repo}/acme/${file}`, {
    raw: true,
    headers: { 'Content-Type': 'application/octet-stream' },
    body: 't414 search fixture\n',
  })
  expect(put.status).toBeLessThan(300)

  await loginAs(page, 'admin')
  // 深链带 q 立即查询（「选中态回显」——reload 后查询自动重放，持久腿的载体）
  await page.goto(`/binflow/ui/search?q=${marker}`)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })

  // T-449 断言反转②默认档：选择列（固定）+ 制品/路径/仓库/修改时间 = 5 表头；
  // 大小/sha256 不默认在场（表无独立锚——以页根限定表头，不新增锚）
  const th = page.locator('[data-testid="search-page"] table thead th')
  await expect(th).toHaveCount(5)

  // 开菜单：六列闭集（制品 name 为 T-449 新增项）+ 默认勾选态分流
  //（选择器全字面量——对账器口径，t387 先例同款纪律）
  const t = page.locator('[data-testid="search-columns"]')
  await expect(t).toHaveAttribute('aria-haspopup', 'menu')
  await expect(t).toContainText('列 4/6')
  await t.click()
  const m = page.locator('[data-testid="search-columns-menu"]')
  await expect(m).toBeVisible()
  await expect(m.locator('[role="menuitemcheckbox"]')).toHaveCount(6)
  await expect(page.locator('[data-testid="search-columns-item-name"]')).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('[data-testid="search-columns-item-path"]')).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('[data-testid="search-columns-item-repo"]')).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('[data-testid="search-columns-item-modified"]')).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('[data-testid="search-columns-item-size"]')).toHaveAttribute('aria-checked', 'false')
  await expect(page.locator('[data-testid="search-columns-item-sha256"]')).toHaveAttribute('aria-checked', 'false')

  // 勾入「大小」→ 表头 + 行单元格 6 + 持久（URL 承 q，reload 结果回归；
  // 存储面 = 隐藏集——size 出集仍留 sha256）
  await page.click('[data-testid="search-columns-item-size"]')
  await expect(m).toBeVisible()
  await expect(th).toHaveCount(6)
  await expect(page.locator('[data-testid="search-result-0"] td')).toHaveCount(6)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-search'))).toBe('["sha256"]')
  await page.reload()
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(th).toHaveCount(6)

  // 「恢复默认列」（T-449：复位 = 默认列集 ≠ 全选——大小/sha256 收回；
  // 存储落默认隐藏集而非空数组）
  await page.click('[data-testid="search-columns"]')
  await page.click('[data-testid="search-columns-reset"]')
  await expect(th).toHaveCount(5)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-search'))).toBe('["size","sha256"]')
  await closeColumnsMenu(page, '[data-testid="search-columns-menu"]', '[data-testid="search-columns"]')
})

// ---- 4. member-pop hover 对比度（AC2，T-391 L-a 清账） -------------------------

test('admin: virtual member-pop — T-391 color-mix recipe applied + axe clean on hovered row in both themes', async ({
  page,
}, testInfo: TestInfo) => {
  test.setTimeout(300_000)
  // 备料：virtual 仓 + 两 local 成员（API 直备——repositories.spec 表单腿的
  // wire 同形：repositories 数组 = 声明序成员）
  const m1 = uniq('t414v1')
  const m2 = uniq('t414v2')
  const vkey = uniq('t414v')
  const client = m8Client()
  for (const key of [m1, m2]) {
    expect(
      (await client.request('PUT', `/binflow/api/repositories/${key}`, { body: { rclass: 'local', packageType: 'generic' } }))
        .status,
    ).toBeLessThan(300)
  }
  const vput = await client.request('PUT', `/binflow/api/repositories/${vkey}`, {
    body: { key: vkey, rclass: 'virtual', packageType: 'generic', repositories: [m1, m2] },
  })
  expect(vput.status).toBeLessThan(300)

  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/repositories/virtual')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    const row = page.locator(`[data-testid="repos-row-${vkey}"]`)
    await expect(row).toBeVisible()

    // 行 hover（行底 = action.hover 叠 --bf-bg——T-391 缺陷底）+ 浮层开
    await row.hover()
    await row.locator('.member-pop summary').click()
    const pop = row.locator('.member-pop .pop')
    await expect(pop).toBeVisible()
    await expect(pop).toContainText(m1)

    // 配方在身（回归腿）：summary 的 computed color ≠ 裸 accent（配方退役
    // 即红——T-391 手算 + 浏览器 computed 复核的同形体例）
    const { summaryColor, accent } = await page.evaluate((key) => {
      const el = document.querySelector(`[data-testid="repos-row-${key}"] .member-pop summary`)
      return {
        summaryColor: el ? getComputedStyle(el).color : '',
        accent: getComputedStyle(document.documentElement).getPropertyValue('--bf-accent'),
      }
    }, vkey)
    testInfo.attach(`member-pop-computed-${theme}`, {
      body: JSON.stringify({ theme, summaryColor, accent: accent.trim() }, null, 2),
      contentType: 'application/json',
    })
    expect(summaryColor, `theme=${theme}: color-mix 配方应在身（≠裸 accent）`).not.toBe(hexToRgb(accent))

    // hover 行底上 axe 双主题 serious=0（AC2 承证腿——光标停在行上扫全页）
    await expectA11yClean(page, testInfo)

    // 收浮层 + 归位（下一主题重进）
    await page.keyboard.press('Escape')
  }
})

// ---- 5. 三页列选菜单开态 axe 双主题（AC3） --------------------------------------

test('axe: users/groups/search column menus open — clean in both themes', async ({ page }, testInfo: TestInfo) => {
  test.setTimeout(300_000)
  // groups 页菜单开态需要页面在场（表空也开——菜单挂 filter-bar 非表）；
  // 备一个组保证表头列也在场（扫的面更真）
  const group = uniq('t414ga')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/security/groups/${group}`, {
    body: { name: group, description: 't414 axe fixture' },
  })

  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)

    for (const [route, trigger, menu] of [
      ['/binflow/ui/admin/security/users', 'users-columns', 'users-columns-menu'],
      ['/binflow/ui/admin/security/groups', 'groups-columns', 'groups-columns-menu'],
      ['/binflow/ui/search', 'search-columns', 'search-columns-menu'],
    ] as const) {
      await page.goto(route)
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      await page.click(`[data-testid="${trigger}"]`)
      await expect(page.locator(`[data-testid="${menu}"]`)).toBeVisible()
      // 入场过渡 settle（MUI Menu 渐入期扫 axe 会拿半透明 Paper 的混合底
      // 误报 contrast——a11y-sweep 250ms「卡片独立到达」先例同款）
      await page.waitForTimeout(300)
      await expectA11yClean(page, testInfo) // 全页扫（portal 菜单同文档内）
      await page.keyboard.press('Escape')
    }
  }
})
