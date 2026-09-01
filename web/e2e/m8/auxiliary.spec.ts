import { expect, test, type Locator } from '@playwright/test'
import { loginAs, provisionRoles } from './support/roles'
import { m8Client, roleFixturesFromEnv } from './support/seed'
import { expectA11yClean } from './support/a11y'

// T-239 应用模式辅助页（console-m8 §6.1/§6.2/§6.4/§6.5 + §4.7 会话过期）：
//   1. 仪表盘三角色数据面（admin 全卡 + 快捷入口；readonly_admin 横幅 +
//      无写入口；普通用户收敛实例卡）+ 审计行 → 制品树深链。
//   2. 搜索全链：输入 → Enter → 计数副标 + 结果 → 行点击/Enter →
//      /artifacts 深链自动展开（消费 T-236）；recentSearches（localStorage）
//      键盘应用 + 清除；?q= 深链回显。
//   3. 登录错误态 + return 回跳 + 文档链接；404 深链回显 + 回主页。
//   4. /profile 拆分形态（改密 + API Token 说明）全角色可达。
// 锚口径 = ADR-0029 决策 3（先入 console-ux §10.5 清单再落码的新锚：
// dashboard-{repos-create,audit-all,audit-row-<i>}、search-{recent,
// recent-item-<i>,recent-clear,pager}、profile-{page,password,token,
// token-docs,token-goto}、not-found-{path,home}、login-docs）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

/** 本 spec 的可检索夹具：独立仓库 + 唯一文件名（重复跑幂等——PUT 覆盖） */
async function seedSearchFixture(): Promise<{ repo: string; file: string; marker: string }> {
  const marker = `t239s${Date.now().toString(36)}`
  const repo = `${marker}-local`
  const file = `${marker}.bin`
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${repo}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  const put = await client.request('PUT', `/binflow/${repo}/acme/${file}`, {
    raw: true,
    headers: { 'Content-Type': 'application/octet-stream' },
    body: 't239 search fixture\n',
  })
  expect(put.status).toBeLessThan(300)
  return { repo, file, marker }
}

// ---- 仪表盘（§6.2）：三角色数据面 + 快捷入口 + 审计行深链 ------------------

test('admin: dashboard full data face, quick entries, audit row deep-links the tree', async ({ page }) => {
  const { repo, file, marker } = await seedSearchFixture()
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')

  await expect(page.locator('[data-testid="dashboard-instance-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-health-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-storage-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-repos-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-audit-card"]')).toBeVisible()

  // 快捷入口（§6.2 线框 [3]）：建仓直达——仅全量 admin
  const create = page.locator('[data-testid="dashboard-repos-create"]')
  await expect(create).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-readonly-note"]')).toHaveCount(0)

  // 审计行点击进对象（§6.2 线框 [5]）：upload 事件行 → 树深链自动展开。
  // 卡片窗口 = getRecentAudit(8)（最新 8 条）；默认并发（T-268）下其他
  // worker 的事件流会在本腿读卡前把目标行挤出窗口——等待无解，改「补种
  // 新唯一文件（新 audit 事件）+ 重读」重试：断言的仍是窗口内可见的那条
  // upload 行与其深链，语义不弱化。
  const client = m8Client()
  let visible = ''
  let row: Locator | null = null
  for (let attempt = 0; attempt < 6 && row === null; attempt++) {
    const name = attempt === 0 ? file : `${marker}-r${attempt}.bin`
    if (attempt > 0) {
      const put = await client.request('PUT', `/binflow/${repo}/acme/${name}`, {
        raw: true,
        headers: { 'Content-Type': 'application/octet-stream' },
        body: 't239 search fixture\n',
      })
      expect(put.status).toBeLessThan(300)
      await page.goto('/binflow/ui/dashboard')
    }
    const candidate = page.locator('[data-testid^="dashboard-audit-row-"]').filter({ hasText: name }).first()
    try {
      await expect(candidate).toBeVisible({ timeout: 8_000 })
      visible = name
      row = candidate
    } catch {
      // 窗口又被并行事件推走——下一轮补种重读
    }
  }
  expect(row, '6 轮补种后 upload 行仍未进入最新 8 条窗口').not.toBeNull()
  await row!.click()
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${repo}/acme\\?focus=${visible}$`))
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"]')).toContainText(visible)

  // 「查看全部 →」直达审计页（新路由，不再依赖旧路由 redirect 兜底）
  await page.goto('/binflow/ui/dashboard')
  await page.click('[data-testid="dashboard-audit-all"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/governance\/audit$/)
  await expect(page.locator('[data-testid="audit-page"]')).toBeVisible()
})

test('readonly_admin: dashboard visible with readonly banner, create entry pre-converged away', async ({ page }) => {
  await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/dashboard')

  // 读面全通（CapRepoRead 家族——T-236 契约漂移定案：readonly 可见清单）
  await expect(page.locator('[data-testid="dashboard-instance-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-health-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-storage-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="dashboard-repos-card"]')).toBeVisible()
  // M7 readonly 横幅（§7.3 语义融入新形态）
  await expect(page.locator('[data-testid="dashboard-readonly-note"]')).toBeVisible()
  // L4 写入口预收敛：建仓快捷入口不渲染（服务端 403 兜底）
  await expect(page.locator('[data-testid="dashboard-repos-create"]')).toHaveCount(0)
})

test('plain user: dashboard converges to instance card + guidance note', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/dashboard')

  await expect(page.locator('[data-testid="dashboard-instance-card"]')).toBeVisible()
  for (const card of ['dashboard-health-card', 'dashboard-storage-card', 'dashboard-repos-card', 'dashboard-audit-card']) {
    await expect(page.locator(`[data-testid="${card}"]`)).toHaveCount(0)
  }
  // 收敛说明（§3.6.4）：指引指向现役入口（用户菜单编辑档案 + 搜索）
  await expect(page.locator('[data-testid="dashboard"] .card.section')).toContainText('编辑档案')
})

// ---- 搜索（§6.4 / reverse §3.3·§4.5）--------------------------------------

test('search: keyboard chain query -> count subtitle -> row Enter deep-links the tree', async ({ page }) => {
  const { repo, file, marker } = await seedSearchFixture()
  await loginAs(page, 'admin')

  // 顶栏搜索入口落 SearchPage（T-235 壳结构；T-265 起顶栏是真输入框——
  // 空词 Enter 保留「纯入口跳 /search」通道，本页 autoFocus 回显维持）
  await page.goto('/binflow/ui/dashboard')
  await page.focus('[data-testid="topbar-search"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/search$/)
  await expect(page.locator('[data-testid="search-input"]')).toBeFocused()

  // 空关键词引导态：不查询
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('输入关键词开始搜索')

  // 键盘输入 + Enter 显式提交（立即查询 + 记入最近搜索）
  await page.keyboard.type(marker)
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`/acme/${file}`)

  // 计数副标（reverse §3.3；计数与行数一致性 = 验收项，不对齐其计数怪癖）
  await expect(page.locator('[data-testid="search-count"]')).toContainText('1 项')
  // 底部计数行（C1：显示 a – b / 共 c 项；无更多页不渲染加载更多）
  await expect(page.locator('[data-testid="search-pager"]')).toContainText('显示 1 – 1 / 共 1 项')
  await expect(page.locator('[data-testid="search-more"]')).toHaveCount(0)

  // 行键盘激活 → 跨仓树深链自动展开（T-236 消费）+ URL 即状态
  await page.focus('[data-testid="search-result-0"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${repo}/acme\\?focus=${file}$`))
  await expect(page.locator('[data-testid="node-detail"]')).toContainText(`acme/${file}`)
})

test('search: recentSearches persist in localStorage, keyboard-apply, clear', async ({ page }) => {
  const { marker } = await seedSearchFixture()
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search')

  // 历史为空：聚焦不渲染下拉
  await page.focus('[data-testid="search-input"]')
  await expect(page.locator('[data-testid="search-recent"]')).toHaveCount(0)

  await page.keyboard.type(marker)
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })

  // localStorage 持久（键 binflow-console-recent-searches）
  const stored = await page.evaluate(() => window.localStorage.getItem('binflow-console-recent-searches'))
  expect(stored).toContain(marker)

  // 有关键词时下拉不遮挡结果；↑↓ 显式导航展开历史
  await page.focus('[data-testid="search-input"]')
  await expect(page.locator('[data-testid="search-recent"]')).toHaveCount(0)
  await page.keyboard.press('ArrowDown')
  await expect(page.locator('[data-testid="search-recent"]')).toBeVisible()
  await expect(page.locator('[data-testid="search-recent-item-0"]')).toHaveText(marker)
  await page.keyboard.press('Enter') // 应用历史项：回填关键词并查询
  await expect(page.locator('[data-testid="search-recent"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-input"]')).toHaveValue(marker)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible()

  // 清空关键词聚焦 = 全量历史；Esc 关闭；一键清除历史。
  // （先点标题失焦再清空——fill 对已聚焦元素不重放 focus 事件）
  await page.click('h2.search-headline')
  await page.fill('[data-testid="search-input"]', '')
  await page.focus('[data-testid="search-input"]')
  await expect(page.locator('[data-testid="search-recent"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="search-recent"]')).toHaveCount(0)
  // 重新聚焦（先失焦再聚焦，focus 事件确定性重放）后一键清除
  await page.click('h2.search-headline')
  await page.focus('[data-testid="search-input"]')
  await expect(page.locator('[data-testid="search-recent"]')).toBeVisible()
  await page.click('[data-testid="search-recent-clear"]')
  await expect(page.locator('[data-testid="search-recent"]')).toHaveCount(0)
  const after = await page.evaluate(() => window.localStorage.getItem('binflow-console-recent-searches'))
  expect(JSON.parse(after ?? '[]')).toEqual([])
})

test('search: ?q= deep link restores the query state and queries immediately', async ({ page }) => {
  const { file, marker } = await seedSearchFixture()
  await loginAs(page, 'admin')

  // 直链（深链状态回显）：关键词回填 + 立即查询（不等防抖）
  await page.goto(`/binflow/ui/search?q=${marker}`)
  await expect(page.locator('[data-testid="search-input"]')).toHaveValue(marker)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(file)
  await expect(page).toHaveURL(new RegExp(`[?&]q=${marker}`))

  // 搜索页 axe（serious/critical = 0——结果态含计数行/拷贝钮）
  await expectA11yClean(page, test.info(), { include: '[data-testid="search-page"]' })
})

// ---- 登录（§6.1）/ 404（§1.3）----------------------------------------------

test('login: inline error state, docs link, return round-trip', async ({ page }) => {
  const fixture = roleFixturesFromEnv().admin

  // 未认证访问受保护页：守卫带 return 弹回登录页（§4.7 会话过期同通道）
  await page.goto('/binflow/ui/search')
  await expect(page).toHaveURL(/\/binflow\/ui\/login\?return=%2Fsearch$/)

  // 常驻说明 + 文档链接（§6.1 [6]）
  await expect(page.locator('[data-testid="login-docs"]')).toBeVisible()

  // 错误态：行内呈现、停留本页（§6.1：无跳转、无 Remember me）
  await page.fill('[data-testid="login-username"]', fixture.name)
  await page.fill('[data-testid="login-password"]', 'definitely-wrong')
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="login-error"]')).toHaveText('用户名或密码错误')
  await expect(page).toHaveURL(/\/login/)

  // 回跳：正确凭据后落回原路由
  await page.fill('[data-testid="login-password"]', fixture.password)
  await page.click('[data-testid="login-submit"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/search$/)
  await expect(page.locator('[data-testid="search-page"]')).toBeVisible()
})

test('404: keeps the shell, echoes the attempted path, links home', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/definitely-not-a-route')

  await expect(page.locator('[data-testid="not-found"]')).toBeVisible()
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
  // 深链状态回显：触发 404 的原始路径
  await expect(page.locator('[data-testid="not-found-path"]')).toHaveText('/definitely-not-a-route')
  // 回主页 = 应用模式首页（/artifacts，§1.1）
  await page.click('[data-testid="not-found-home"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/artifacts$/)
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
})

// ---- /profile（§6.5：设置页拆分形态）---------------------------------------

test('profile: split form (password + token note) reachable from the user menu for every role', async ({ page }) => {
  await loginAs(page, 'user')

  // 用户菜单「编辑档案」直达（§2.3：全角色可见）
  await page.click('[data-testid="session-toggle"]')
  await page.click('[data-testid="menu-edit-profile"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/profile$/)
  await expect(page.locator('[data-testid="profile-page"]')).toBeVisible()

  // 认证设置 = 改密（password-* 冻结锚随表单整体迁址）
  await expect(page.locator('[data-testid="profile-password"]')).toBeVisible()
  await expect(page.locator('[data-testid="password-old"]')).toBeVisible()
  await expect(page.locator('[data-testid="password-submit"]')).toBeDisabled()

  // API Token 说明面（§6.5[2]；T-386 起目标页真身：台账 + 生成）
  await expect(page.locator('[data-testid="profile-token"]')).toBeVisible()
  await expect(page.locator('[data-testid="profile-token-docs"]')).toHaveAttribute('href', '/binflow/docs/api-reference')
  await page.click('[data-testid="profile-token-goto"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/tokens$/)
  // 普通用户：目标页自身 L2 收敛（T-386 真身的 denied 臂 = 无权限卡；
  // 管理面页根在场，占位页锚已退役）
  await expect(page.locator('[data-testid="tokens-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="tokens-page"] [data-testid="empty-state"]')).toBeVisible()
  await expect(page.locator('[data-testid="tokens-page"] [data-testid="token-create"]')).toHaveCount(0)

  // profile 页 axe（serious/critical = 0；先锚定页面根——lazy 分片到达后再扫）
  await page.goto('/binflow/ui/profile')
  await expect(page.locator('[data-testid="profile-page"]')).toBeVisible()
  await expectA11yClean(page, test.info(), { include: '[data-testid="profile-page"]' })
})
