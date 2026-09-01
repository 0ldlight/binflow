import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'

// T-416（M15 B4 FE，FR-136.3——断言反转②）：virtual 仓树消费。服务端聚合面
// 由 T-412 就绪（GET /api/storage/<virtual> folder 面 200 children = 成员并集，
// 同名路径首成员胜、深层递归、无成员/全空 → 200 空 children）；本 spec 钉 FE
// 消费实态——
//
//   ① RepoBranch virtual 动态展开恢复（T-406 as-built 受限面解除）：twisty
//      在场、aria-expanded 可翻、子级区渲染（与非 virtual 仓同形）。
//   ② 树展开 = children 并集逐名渲染：跨成员同前缀目录合并 + 各自独占目录
//      并列 + 深层递归（com → acme → app-m1/app-m2/shared → 1.0）。
//   ③ 右表 = 当前层并集（导航面）；同名目录（shared/）两个成员的文件并列。
//   ④ 空态翻转：有成员内容不再空态（tree-empty-virtual 反断言 + 行在场）；
//      全空成员维持空态（成员清单文案）；成员仓删除（FK 级联无成员）仍空态。
//   ⑤ 删除入口预收敛（RE-08：服务端 DELETE 一律 405）——行内/详情/右键三
//      出口收敛 + 405 服务端真相钉（预收敛的依据，非 FE 面孤证）。
//   ⑥ virtual 浏览面 axe 双主题 serious=0。
//
// 夹具纪律：Playwright 栈自建（双 local 成员 + virtual 仓 + 内容面上传），
// 零外部状态依赖；锚源 = console-ux §10.5 T-236 批 tree-* 族（tree-empty-virtual
// 语义随票翻转，锚册 v1.28 留痕）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: Page, user = ADMIN, pw = ADMIN_PW) {
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', user)
  await page.fill('[data-testid="login-password"]', pw)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** 同源 fetch（携带 session cookie）；返回 {status, text} */
async function api(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; text: string }> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      })
      return { status: res.status, text: await res.text() }
    },
    { method, path, body },
  )
}

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 夹具：双 local 成员 + virtual 仓。树形——
 *   m1: com/acme/app-m1/1.0/app-m1-1.0.bin、com/acme/shared/from-m1.txt、org/m1-only.txt
 *   m2: com/acme/app-m2/2.0/app-m2-2.0.bin、com/acme/shared/from-m2.bin、net/m2-only.txt
 * 并集根 = com（双成员同路径）/ net（m2 独占）/ org（m1 独占）；
 * com/acme 下 = app-m1 ∪ app-m2 ∪ shared（跨成员并集）；shared/ 内两成员文件并列。 */
async function seedUnionFixture(page: Page): Promise<{ m1: string; m2: string; v: string }> {
  // api() 是页面会话内的相对 fetch——先落到任意同源页再种夹具（about:blank
  // 上相对 URL 不可解析）
  await page.goto('/binflow/ui/')
  const m1 = uniq('t416a')
  const m2 = uniq('t416b')
  const v = uniq('t416v')
  await api(page, 'PUT', `/api/repositories/${m1}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/api/repositories/${m2}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/api/repositories/${v}`, {
    rclass: 'virtual',
    packageType: 'generic',
    repositories: [m1, m2],
  })
  await api(page, 'PUT', `/${m1}/com/acme/app-m1/1.0/app-m1-1.0.bin`, 'm1-app')
  await api(page, 'PUT', `/${m1}/com/acme/shared/from-m1.txt`, 'm1-shared')
  await api(page, 'PUT', `/${m1}/org/m1-only.txt`, 'm1-org')
  await api(page, 'PUT', `/${m2}/com/acme/app-m2/2.0/app-m2-2.0.bin`, 'm2-app')
  await api(page, 'PUT', `/${m2}/com/acme/shared/from-m2.bin`, 'm2-shared')
  await api(page, 'PUT', `/${m2}/net/m2-only.txt`, 'm2-net')
  return { m1, m2, v }
}

test('virtual tree: dynamic expansion renders the member union by name (deep recursion, same-name dir merge)', async ({
  page,
}) => {
  await login(page)
  const { m1, m2, v } = await seedUnionFixture(page)
  await page.goto('/binflow/ui/artifacts')

  // ① RepoBranch 动态展开恢复：twisty 在场、aria-expanded 可翻（T-406 的
  //    静态占位形态退役——翻转面就是这两行断言）
  const repoRow = page.locator(`[data-testid="tree-repo-${v}"]`)
  await expect(repoRow).toBeVisible()
  await expect(repoRow).toHaveAttribute('aria-expanded', 'false')
  await repoRow.locator('.twisty').click()
  await expect(repoRow).toHaveAttribute('aria-expanded', 'true')

  // ② 根并集逐名渲染：com（双成员同路径首成员行）/ net（m2 独占）/ org（m1 独占）
  for (const seg of ['com', 'net', 'org']) {
    await expect(page.locator(`[data-testid="tree-node-${seg}"]`)).toBeVisible()
  }

  // 深层递归：com → acme →（app-m1 ∪ app-m2 ∪ shared，跨成员并集）→ 1.0
  await page.locator('[data-testid="tree-node-com"] .twisty').click()
  await expect(page.locator('[data-testid="tree-node-com/acme"]')).toBeVisible()
  await page.locator('[data-testid="tree-node-com/acme"] .twisty').click()
  await expect(page.locator('[data-testid="tree-node-com/acme/app-m1"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-node-com/acme/app-m2"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-node-com/acme/shared"]')).toBeVisible()
  await page.locator('[data-testid="tree-node-com/acme/app-m1"] .twisty').click()
  await expect(page.locator('[data-testid="tree-node-com/acme/app-m1/1.0"]')).toBeVisible()

  // ③ 右表 = 当前层并集：仓根选中（URL 即状态）→ com/net/org 行
  await repoRow.click()
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${v}$`))
  await expect(page.locator('[data-testid="tree-empty-virtual"]')).toHaveCount(0)
  for (const seg of ['com', 'net', 'org']) {
    await expect(page.locator(`[data-testid="tree-row-${seg}"]`)).toBeVisible()
  }

  // 同层跨成员并集：com/acme 的 children 表 = app-m1 + app-m2 + shared
  await page.click('[data-testid="tree-row-com"]')
  await page.click('[data-testid="tree-row-acme"]')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${v}/com/acme$`))
  for (const name of ['app-m1', 'app-m2', 'shared']) {
    await expect(page.locator(`[data-testid="tree-row-${name}"]`)).toBeVisible()
  }

  // 同名目录合并：shared/ 在两个成员里各有内容——两行并列（首成员胜是
  // 「同名路径」的行级语义；同名目录下的不同文件名是并集，逐名都在）
  await page.click('[data-testid="tree-row-shared"]')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${v}/com/acme/shared$`))
  await expect(page.locator('[data-testid="tree-row-from-m1.txt"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-from-m2.bin"]')).toBeVisible()

  // 文件详情（pull 解析面经内容面元数据）：focus 选中 → 详情面板在场
  await page.click('[data-testid="tree-row-from-m1.txt"]')
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"] h3 .mono')).toHaveText(
    'com/acme/shared/from-m1.txt',
  )

  // 夹具自证：两成员各自的根确集（防止「并集」断言撞上单成员内容的巧合——
  // m1 没有 net/、m2 没有 org/，只有聚合面能同时给出三者；children uri 形
  // 如 "/name"，按精确段断言）
  const m1Root = await api(page, 'GET', `/api/storage/${m1}`)
  const m2Root = await api(page, 'GET', `/api/storage/${m2}`)
  expect(m1Root.text).not.toContain('"uri":"/net"')
  expect(m2Root.text).not.toContain('"uri":"/org"')
})

test('virtual empty states: content ends the empty state; empty members and cascade-memberless keep it', async ({
  page,
}) => {
  await login(page)

  // 全空成员：成员在册但零内容 → 空态维持（文案带成员清单）
  const e1 = uniq('t416e1')
  const e2 = uniq('t416e2')
  const ev = uniq('t416ev')
  await api(page, 'PUT', `/api/repositories/${e1}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/api/repositories/${e2}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/api/repositories/${ev}`, {
    rclass: 'virtual',
    packageType: 'generic',
    repositories: [e1, e2],
  })
  await page.goto(`/binflow/ui/artifacts/${ev}`)
  const emptyCard = page.locator('[data-testid="tree-empty-virtual"]')
  await expect(emptyCard).toBeVisible()
  await expect(emptyCard).toContainText(e1)
  await expect(emptyCard).toContainText(e2)

  // 空态翻转（断言反转②正题）：成员有内容后不再空态——行在场 + 锚反断言
  await api(page, 'PUT', `/${e1}/wake/wake.bin`, 'wake')
  await page.reload()
  await expect(page.locator('[data-testid="tree-empty-virtual"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="tree-row-wake"]')).toBeVisible()

  // 无成员态（成员仓删除 → virtual_members FK 级联清空）：仍诚实空页 + 空态卡
  const m3 = uniq('t416m3')
  const v3 = uniq('t416v3')
  await api(page, 'PUT', `/api/repositories/${m3}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/api/repositories/${v3}`, {
    rclass: 'virtual',
    packageType: 'generic',
    repositories: [m3],
  })
  await api(page, 'PUT', `/${m3}/gone/gone.bin`, 'gone')
  const del = await api(page, 'DELETE', `/api/repositories/${m3}?deleteContent=true`)
  expect(del.status).toBe(200)
  await page.goto(`/binflow/ui/artifacts/${v3}`)
  await expect(page.locator('[data-testid="tree-empty-virtual"]')).toBeVisible()
})

test('virtual delete affordances are converged away (RE-08: server DELETE answers 405)', async ({ page }) => {
  await login(page)
  const { v } = await seedUnionFixture(page)
  await page.goto(`/binflow/ui/artifacts/${v}/com/acme/shared`)

  // 服务端真相钉：内容面 DELETE 经 virtual 一律 405（预收敛的依据——入口
  // 收敛是不给注定失败的影子路径，不是掩盖能力；FE deleteNode 同面）
  const del = await api(page, 'DELETE', `/${v}/com/acme/shared/from-m1.txt`)
  expect(del.status).toBe(405)

  // 行内删除钮：virtual 仓不渲染（delete-node-button 同时是行内钮与详情
  // 面板删除钮的锚——选中文件后一并反断言）
  await expect(page.locator('[data-testid="delete-node-button"]')).toHaveCount(0)
  await page.click('[data-testid="tree-row-from-m1.txt"]')
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="delete-node-button"]')).toHaveCount(0)

  // 右键菜单：删除项 disabled + RE-08 title（复制路径仍可用——只收敛删除）
  await page.click('[data-testid="tree-row-from-m1.txt"]', { button: 'right' })
  const menuItem = page.locator('[data-testid="tree-context-delete"]')
  await expect(menuItem).toBeVisible()
  await expect(menuItem).toBeDisabled()
  await expect(menuItem).toHaveAttribute(
    'title',
    /virtual 仓不经手删除（RE-08，服务端 405）——请到持有该制品的成员仓删除/,
  )
})

test('axe: virtual browser view (expanded union tree) clean in both themes', async ({ page }, testInfo: TestInfo) => {
  await login(page)
  const { v } = await seedUnionFixture(page)

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto(`/binflow/ui/artifacts/${v}`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator(`[data-testid="tree-repo-${v}"]`)).toBeVisible()

    // 树展开态（子级区渲染中）+ 右表并集行都在场时扫描
    await page.locator(`[data-testid="tree-repo-${v}"] .twisty`).click()
    await expect(page.locator('[data-testid="tree-node-com"]')).toBeVisible()
    await expect(page.locator('[data-testid="tree-row-com"]')).toBeVisible()
    await expectA11yClean(page, testInfo)
  }
})
