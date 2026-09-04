import { expect, test } from '@playwright/test'
import type { TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-451（M16 批次③ B8，FR-144.7 / LC-98——E2 翻案：分页控件 ×9 统一）：
//
//   ① 客户端页窗（基本搜索结果表）：页码序列跳转 / 每页行数档 / 首末页
//      边界禁置 / 快滤联动回零（parity §11.2 冻结形态锚的已知总量臂）。
//   ② keyset 页窗（审计）：AC2 深翻页行为断言在 governance.spec 翻新腿
//      （105 事件造数 + 游标链推进 + 末页游标耗尽）——本 spec 补 axe 面。
//   ③ 管理列表单页全量态：控件整体呈现、全链禁置（禁置不隐藏——P7）。
//   ④ AQL 模式：页码直跳 = .offset() 重写 + 每页行数 = .limit() 重写
//      （查询文本是唯一事实源——语义 C 注：呈现对齐、语义自有）。
//   ⑤ axe 双主题：分页控件在场的结果/管理面 serious=0。
//
// 锚源：console-ux §10.5 T-451 批（pager-range / pager-size / pager-size-<n> /
// pager-first / pager-prev / pager-next / pager-last / pager-page-<n> +
// audit-pager，v1.39）；分治豁免面（tree-load-more 制品树增量）锚在
// e2e/artifacts.spec.ts 既有腿（parity §5 L4 分治口径）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 顶栏搜索提交（T-475 确定性原语，机制与重试形态见 t449 topbarQuery
 *  注释）：登录落地链上 AppShell 懒加载重挂会清零顶栏受控草稿、导航换
 *  key 会回滚已填词——慢 runner 上 fill/Enter 与之竞速 = 提交空词（裸
 *  /search 无 ?q）。等落地页内容就位 + 受控值钉住（回滚即重填）+ 提交
 *  后核对 URL，空词分支整个重试。 */
async function topbarSearchSubmit(page: import('@playwright/test').Page, term: string): Promise<void> {
  const target = new RegExp(`/binflow/ui/search\\?q=${term}$`)
  const input = page.locator('[data-testid="topbar-search"]')
  await page.waitForSelector('[data-testid="tree-page"]')
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.fill('[data-testid="topbar-search"]', term)
    await expect
      .poll(async () => {
        if ((await input.inputValue()) !== term) await page.fill('[data-testid="topbar-search"]', term)
        return input.inputValue()
      })
      .toBe(term)
    await page.press('[data-testid="topbar-search"]', 'Enter')
    if (await page.waitForURL(target, { timeout: 3_000 }).then(() => true, () => false)) return
  }
  await expect(page).toHaveURL(target)
}

// ---- ① 客户端页窗：页码跳转 / 每页行数 / 首末页禁置（基本搜索） --------------

test('admin: search results page-window pager — jump, size options, boundary disable', async ({ page }) => {
  test.setTimeout(120_000)
  const marker = uniq('t451s')
  const repo = uniq('t451r')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${repo}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  // 210 件：默认 100/页 = 3 页（100+100+10）；50/页 = 5 页（4×50+10）
  for (let i = 0; i < 210; i++) {
    const put = await client.request('PUT', `/binflow${repo}/d/f${String(i).padStart(4, '0')}-${marker}.bin`, {
      raw: true,
      headers: { 'Content-Type': 'application/octet-stream' },
      body: `x${i}`,
    })
    expect(put.status).toBeLessThan(300)
  }

  await loginAs(page, 'admin')
  await topbarSearchSubmit(page, marker)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 15_000 })

  // 第 1 页（100/页）：恰 100 行 + range 行 + 边界禁置
  await expect(page.locator('[data-testid="search-result-99"]')).toBeVisible()
  await expect(page.locator('[data-testid="search-result-100"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-range"]')).toHaveText(
    '显示 1 – 100 / 共 210 项',
  )
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-first"]')).toBeDisabled()
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-prev"]')).toBeDisabled()
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-next"]')).toBeEnabled()
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-last"]')).toBeEnabled()

  // 页码直跳第 3 页：201–210 窗（10 行，<i> = 窗内呈现序）→ 末页边界
  await page.click('[data-testid="search-pager"] [data-testid="pager-page-3"]')
  await expect(page.locator('[data-testid="search-result-9"]')).toBeVisible()
  await expect(page.locator('[data-testid="search-result-10"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-range"]')).toHaveText(
    '显示 201 – 210 / 共 210 项',
  )
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-next"]')).toBeDisabled()
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-last"]')).toBeDisabled()
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-prev"]')).toBeEnabled()

  // 末页钮直跳回末页（已知总量面 = 真末页）
  await page.click('[data-testid="search-pager"] [data-testid="pager-page-1"]')
  await page.click('[data-testid="search-pager"] [data-testid="pager-last"]')
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-range"]')).toHaveText(
    '显示 201 – 210 / 共 210 项',
  )

  // 每页行数：换档 50 → 回第 1 页、窗 50 行；档位枚举在场
  await page.selectOption('[data-testid="search-pager"] [data-testid="pager-size"]', '50')
  await expect(page.locator('[data-testid="search-result-49"]')).toBeVisible()
  await expect(page.locator('[data-testid="search-result-50"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-range"]')).toHaveText(
    '显示 1 – 50 / 共 210 项',
  )
  const options = await page
    .locator('[data-testid="search-pager"] [data-testid="pager-size"] option')
    .allTextContents()
  expect(options).toEqual(['20', '50', '100', '200', '1000'])

  // 末页再核对（50/页 → 5 页，末页 10 行）
  await page.click('[data-testid="search-pager"] [data-testid="pager-page-5"]')
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-range"]')).toHaveText(
    '显示 201 – 210 / 共 210 项',
  )
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-next"]')).toBeDisabled()

  // 首页钮：直跳回第 1 页（首边界复原）+ 快滤联动：窄化即回零第 1 页
  await page.click('[data-testid="search-pager"] [data-testid="pager-first"]')
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-range"]')).toHaveText(
    '显示 1 – 50 / 共 210 项',
  )
  await page.fill('[data-testid="search-quick-filter"]', 'f00')
  await expect(page.locator('[data-testid="search-quick-count"]')).toHaveText('快滤命中 100 / 210')
  await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-range"]')).toContainText(
    '（快滤自 210）',
  )
})

// ---- ③ 管理列表单页全量：控件整体呈现、全链禁置（禁置不隐藏） ------------------

test('admin: management lists single-page full set — control present, whole chain disabled', async ({ page }) => {
  await loginAs(page, 'admin')

  // 用户列表（单页全量态）
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-count"] [data-testid="pager-range"]')).toContainText(
    /显示 1 – \d+ \/ 共 \d+ 项/,
  )
  for (const btn of ['pager-first', 'pager-prev', 'pager-next', 'pager-last']) {
    await expect(page.locator(`[data-testid="users-count"] [data-testid="${btn}"]`)).toBeDisabled()
  }
  await expect(page.locator('[data-testid="users-count"] [data-testid="pager-page-1"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-count"] [data-testid="pager-size"]')).toBeVisible()

  // 权限 target 列表（同形态；m-holder 注记腿在 m9/permissions-mholder）
  await page.goto('/binflow/ui/admin/security/permissions')
  await expect(page.locator('[data-testid="perms-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="perms-count"] [data-testid="pager-range"]')).toContainText(
    /显示 1 – \d+ \/ 共 \d+ 项/,
  )
  for (const btn of ['pager-first', 'pager-prev', 'pager-next', 'pager-last']) {
    await expect(page.locator(`[data-testid="perms-count"] [data-testid="${btn}"]`)).toBeDisabled()
  }
})

// ---- ④ AQL：页码直跳 = .offset() 重写；每页行数 = .limit() 重写（C 注） -------

test('admin: AQL pager — page-number jump rewrites .offset(), size selector rewrites .limit()', async ({ page }) => {
  const marker = uniq('t451a')
  const repo = uniq('t451a')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${repo}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  for (let i = 1; i <= 5; i++) {
    const put = await client.request('PUT', `/binflow${repo}/${marker}-f${i}.bin`, {
      raw: true,
      headers: { 'Content-Type': 'application/octet-stream' },
      body: `x${i}`,
    })
    expect(put.status).toBeLessThan(300)
  }

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search?mode=aql')
  const base = `items.find({"repo":"${repo}"}).include("name").sort({"$asc":["name"]})`
  await page.fill('[data-testid="search-aql-input"]', `${base}.limit(2)`)
  await page.click('[data-testid="search-aql-run"]')
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 15_000 })
  const editor = page.locator('[data-testid="search-aql-input"]')

  // 页码直跳 [2]：.offset(2) 重写（(N-1)×limit 网格对齐）
  await page.click('[data-testid="pager-page-2"]')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText('-f3.bin')
  expect(await editor.inputValue()).toBe(`${base}.offset(2).limit(2)`)

  // 每页行数：手写档位值 2 在选择器如实在场（非档值不显示空白）；换 50
  // → .limit(50) 重写 + .offset() 清除（回第 1 页——旧 offset 在新页大小
  // 下指向错位窗口）
  const sizes = await page.locator('[data-testid="pager-size"] option').allTextContents()
  expect(sizes).toContain('2')
  await page.selectOption('[data-testid="pager-size"]', '50')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText('-f1.bin')
  expect(await editor.inputValue()).toBe(`${base}.limit(50)`)
  // 全量 5 行 < 50 → 单页末尾：next/last 禁置；range 行如实「末页未知」口径
  // 不适用（已知本页即末页——next 禁置由满窗判定给出）
  await expect(page.locator('[data-testid="pager-next"]')).toBeDisabled()
  await expect(page.locator('[data-testid="pager-last"]')).toBeDisabled()
})

// ---- ⑤ axe 双主题：分页控件在场的结果/管理面 serious=0 ------------------------

test('axe: pager surfaces clean in both themes (results + management list)', async ({ page }, testInfo: TestInfo) => {
  test.setTimeout(300_000)
  const marker = uniq('t451x')
  const repo = uniq('t451x')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${repo}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  for (let i = 0; i < 130; i++) {
    const put = await client.request('PUT', `/binflow${repo}/d/x${String(i).padStart(4, '0')}-${marker}.bin`, {
      raw: true,
      headers: { 'Content-Type': 'application/octet-stream' },
      body: `x${i}`,
    })
    expect(put.status).toBeLessThan(300)
  }

  await loginAs(page, 'admin')
  // 结果面（两页 + 控件全链在场）与管理面各扫一遍、双主题
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)

    await page.goto(`/binflow/ui/search?q=${marker}`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="search-pager"]')).toBeVisible({ timeout: 15_000 })
    await expectA11yClean(page, testInfo, { include: '[data-testid="search-page"]' })

    // 深翻到第 2 页再扫（翻页态的控件/表格组合面）
    await page.click('[data-testid="search-pager"] [data-testid="pager-page-2"]')
    await expect(page.locator('[data-testid="search-pager"] [data-testid="pager-range"]')).toContainText('101')
    await expectA11yClean(page, testInfo, { include: '[data-testid="search-page"]' })

    await page.goto('/binflow/ui/admin/security/users')
    await expect(page.locator('[data-testid="users-count"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="users-page"]' })
  }
})
