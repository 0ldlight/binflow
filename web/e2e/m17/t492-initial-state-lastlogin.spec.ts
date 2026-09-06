import { expect, test } from '@playwright/test'

import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-492（M17 确定层，FR-156.4 / FR-156-AC3——FE 收尾腿：B-3.2 初始态 +
// Users Last Login 列 + 列选器接入，T-468 收编）。
//
//   ① B-3.2 初始态（parity §12 B-3.2 翻正——M16 候裁挂起项收口）：
//      制品浏览进入（登录落点 / 模块进入）即**首仓库自动选中 + item view
//      呈现**——原「无选中 + 静态引导卡」初始态退役。首仓库 = 当前 Sort-by
//      序（默认名称序）第一行，与树呈现序同源；URL 以 replace 规范化为
//      /artifacts/<repo>（书签/刷新落点稳定）。
//   ①-边界 a（空仓）：首仓库为空仓 → item view 照常呈现 + children 表按
//      真实空态（tree-empty-dir）呈现——不因自动选中伪造内容。
//   ①-边界 b（无权限仓）：普通用户 GET /api/repositories 403（CapRepoRead
//      管理壳门）→ 不自动选中——L2 无权限卡（tree-root-denied）承载，URL
//      原地停在 /artifacts。
//   ①-回根让位：带选中态进入后经侧栏导航回跨仓根 → 不重复自动选中
//      （根态是用户主动回到的跨仓视图；hadSelection 钉一次即让位）。
//   ② Users 最近登录列（T-454 投影消费——GET /api/security/users 列表项
//      lastLoggedIn，RFC3339 UTC，omitempty 从未登录整键缺席）：admin 行
//      （本腿 UI 登录刷新投影）渲染截断到秒的本地形 + title 全值；API 直建
//      的从未登录用户行如实呈现「—（尚未登录）」（不伪造 Never 以外语义）。
//      列序对位 Artifactory（Status 之后）。列头排序 = 前端列头排序族
//      （RFC3339 字典序 = 时间序；缺席 '' 沉首/沉底）。
//   ②-列选器（columnPrefs，T-387/T-414 共享层）：新列进闭集（7 列）——
//      弃「最近登录」→ 表头/单元格同步退场；localStorage per-page 持久
//      reload 保持；全选复位（t414 spec 承载通用形态，本腿钉新列进出）。
//
// 锚源：既有锚复用（tree-page / node-detail / tree-empty-dir /
// tree-root-denied / user-row-<name> / users-columns{,-menu,-reset} 族）；
// 本票新增 users-columns-item-lastlogin + users-sort-lastlogin（锚册待回写
// ——conductor 收口）。夹具全自备（uniq 前缀 0t492 数字头 = 名称序首——
// 空仓边界腿的可控首仓库），收尾 DELETE ?deleteContent=true（自备实体）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

// 串行（本文件内）：首仓库两腿互为环境敏感——「首仓库 = 名称序第一」在
// 数字头空仓夹具在场时会翻转（空仓边界腿有意制造该翻转），首腿须先于它跑。
// 跨文件并行无害：其余套件不断言首仓库身份（落点断言均为前缀形）。
test.describe.configure({ mode: 'serial' })

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 实例当前仓库清单按名称序（与树 Sort-by 默认档同一比较器）的第一行 */
async function firstNameSortedKey(): Promise<string> {
  const client = m8Client()
  const res = await client.request('GET', '/binflow/api/repositories')
  expect(res.status).toBe(200)
  const keys = (JSON.parse(res.text) as { key: string }[]).map((r) => r.key)
  expect(keys.length).toBeGreaterThan(0)
  return [...keys].sort((a, b) => a.localeCompare(b))[0]
}

// ---- ① B-3.2：进入即首仓库自动选中 + item view ------------------------------

test('initial state: entering /artifacts auto-selects the first repo and shows its item view', async ({ page }) => {
  test.setTimeout(120_000)
  const first = await firstNameSortedKey()

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()

  // 自动选中：URL 规范化为 /artifacts/<首仓库>（replace——历史栈不增项）
  await expect(page).toHaveURL(`/binflow/ui/artifacts/${first}`)
  // item view 即刻呈现（仓库形态详情）+ 树行选中态（on-chain）
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"]')).toContainText(first)
  await expect(page.locator(`[data-testid="tree-repo-${first}"]`)).toHaveClass(/on-chain/)
})

test('initial state edge: empty first repo — item view presents, children table shows the honest empty state', async ({
  page,
}) => {
  test.setTimeout(120_000)
  // a0-头前缀：repo key 域 [a-z][a-z0-9-]{1,62} 禁数字头（服务端 400）；
  // 'a0…' 排序在常见夹具头（a11y* / at416* / kb* / m8* / t*——ICU 序 '0' <
  // '1' < 字母）与 m8 种子仓之前——本腿可控「首仓库 = 空仓」（首仓身份仍
  // 逐次对账钉死，见下方前提断言）
  const key = uniq('a0t492')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${key}`, {
    body: { rclass: 'local', packageType: 'generic', description: 't492 empty-first fixture' },
  })
  try {
    const first = await firstNameSortedKey()
    // 前提钉死：本腿的空仓就是首仓库（实例若另有更前的仓，此处红出而不
    // 是误把别的仓当空仓断言）
    expect(first, '空仓夹具应排名称序首位（a0-头）').toBe(key)

    await loginAs(page, 'admin')
    await page.goto('/binflow/ui/artifacts')
    await expect(page).toHaveURL(`/binflow/ui/artifacts/${key}`)
    // item view 照常呈现 + 真实空态（不因自动选中伪造内容）
    await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
    await expect(page.locator('[data-testid="tree-empty-dir"]')).toBeVisible()
  } finally {
    await client.request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
  }
})

test('initial state edge: plain user (repo list 403) — no auto-select, L2 card, URL stays at /artifacts', async ({
  page,
}) => {
  test.setTimeout(120_000)
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator('[data-testid="tree-root-denied"]')).toBeVisible()
  // 无可选中清单 → 不自动选中：URL 原地（无仓段）
  await expect(page).toHaveURL('/binflow/ui/artifacts')
  await expect(page.locator('[data-testid="node-detail"]')).toHaveCount(0)
})

test('initial state yield: navigating back to the cross-repo root after a selection keeps the root view', async ({
  page,
}) => {
  test.setTimeout(120_000)
  const key = uniq('t492r')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${key}`, {
    body: { rclass: 'local', packageType: 'generic', description: 't492 root-return fixture' },
  })
  try {
    await loginAs(page, 'admin')
    // 带选中态进入（挂载即选中——hadSelection 钉住）→ 侧栏导航回根
    await page.goto(`/binflow/ui/artifacts/${key}`)
    await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
    await page.click('[data-testid="app-nav"] a.nav-item:text-is("制品")')
    // 回根不重复自动选中：根态保持（无 item view），URL 原地
    await expect(page).toHaveURL('/binflow/ui/artifacts')
    await expect(page.locator('[data-testid="node-detail"]')).toHaveCount(0)
  } finally {
    await client.request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
  }
})

// ---- ② Users 最近登录列（T-454 投影消费 + 列选器接入） -----------------------

test('users last login column: projection renders, never-login honest, column order, sort control', async ({
  page,
}) => {
  test.setTimeout(120_000)
  const name = uniq('t492u')
  const client = m8Client()
  // API 直建用户：无 UI 登录 → 无 login.success 审计行 → lastLoggedIn 整键缺席
  const created = await client.request('PUT', `/binflow/api/security/users/${name}`, {
    body: { name, email: `${name}@t492.invalid`, password: 't492-never-login-pw', admin: false, adminRole: 'user', groups: [] },
  })
  expect(created.status).toBeLessThan(300)
  try {
    await loginAs(page, 'admin') // 本腿登录刷新 admin 的投影（列表行应有值）
    await page.goto('/binflow/ui/admin/security/users')
    await expect(page.locator('[data-testid="users-table"]')).toBeVisible()

    // 列头在场 + 列序对位 Artifactory（Status 之后、操作之前）
    const th = page.locator('[data-testid="users-table"] thead th')
    await expect(page.locator('[data-testid="users-sort-lastlogin"]')).toBeVisible()
    const headers = await th.evaluateAll((els) => els.map((e) => e.textContent ?? ''))
    expect(headers.indexOf('Status')).toBeLessThan(headers.indexOf('最近登录'))
    expect(headers.indexOf('最近登录')).toBeLessThan(headers.length - 1)

    // admin 行：截断到秒的呈现形 + title 全值（RFC3339 UTC）
    const adminCell = page.locator('[data-testid="user-row-admin"] td.mono')
    await expect(adminCell).toHaveText(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/)
    await expect(adminCell).toHaveAttribute('title', /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/)

    // 从未登录用户行：整键缺席 → 如实呈现（不伪造）
    const neverCell = page.locator(`[data-testid="user-row-${name}"] td.mono`)
    await expect(neverCell).toHaveText('—（尚未登录）')

    // 列头排序控件（三态循环 asc → desc；行序受共享夹具影响不断言具体序）
    await page.click('[data-testid="users-sort-lastlogin"]')
    await expect(page.locator('[data-testid="users-sort-lastlogin"]')).toHaveAttribute('aria-sort', 'ascending')
    await page.click('[data-testid="users-sort-lastlogin"]')
    await expect(page.locator('[data-testid="users-sort-lastlogin"]')).toHaveAttribute('aria-sort', 'descending')
  } finally {
    await client.request('DELETE', `/binflow/api/security/users/${name}`)
  }
})

test('users last login column: column-selector integration — hide persists across reload, reset restores', async ({
  page,
}) => {
  test.setTimeout(120_000)
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/security/users')
  const th = page.locator('[data-testid="users-table"] thead th')
  await expect(th).toHaveCount(7) // admin 视角七列闭集（T-492 增最近登录）
  await expect(page.locator('[data-testid="user-row-admin"] td.mono')).toHaveCount(1)

  // 弃「最近登录」：表头 + 单元格同步退场
  await page.click('[data-testid="users-columns"]')
  await expect(page.locator('[data-testid="users-columns-menu"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-columns-item-lastlogin"]')).toHaveAttribute('aria-checked', 'true')
  await page.click('[data-testid="users-columns-item-lastlogin"]')
  await expect(page.locator('[data-testid="users-sort-lastlogin"]')).toHaveCount(0)
  await expect(th).toHaveCount(6)
  await expect(page.locator('[data-testid="user-row-admin"] td.mono')).toHaveCount(0)

  // columnPrefs per-page 持久：localStorage 落盘 + reload 保持
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-users'))).toContain('lastLogin')
  await page.reload()
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-sort-lastlogin"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="user-row-admin"] td.mono')).toHaveCount(0)

  // 全选复位：列回场 + 存储回空数组
  await page.click('[data-testid="users-columns"]')
  await page.click('[data-testid="users-columns-reset"]')
  await expect(page.locator('[data-testid="users-sort-lastlogin"]')).toBeVisible()
  await expect(th).toHaveCount(7)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-users'))).toBe('[]')
})
