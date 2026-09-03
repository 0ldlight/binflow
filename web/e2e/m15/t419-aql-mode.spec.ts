import { expect, test } from '@playwright/test'
import type { TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-419（M15 B6 FE，FR-135.1）——搜索页 AQL 模式（模式切换 + AQL 编辑器 +
// 结果表，消费 T-415 既有端点 POST /api/search/aql；结果表复用 T-414 列
// 框架与列选器）。
//
// T-449 搜索栈翻新随动（零锚断链——AQL 行复用锚维持）：页内查询表单退役
//（查询面 = 顶栏驻留 + AQL 编辑器，search-input 断言翻为反断言）；列框架
// 收敛 ResultsTable（默认列 = 选择列 + 制品|路径|仓库|修改时间，大小/
// sha256 列选器可选项不默认呈现——断言反转②；size 排序腿先勾列再点）。
//
// 断言面（M15-SPLIT §1.2 AC + BOARD）：
//   ① 模式切换：默认基本；?mode=aql 深链 reload 持久；两模式列选器
//      （T-414 壳）均在场。
//   ② 合法查询结果渲染：行/计数副标（search-count 复用）/行锚沿
//      search-result-<i> 既有族；include 投影字段进列。
//   ③ 语法错内联呈现：400 E-01 文案逐字透传（v1c 链序错锚样本 +
//      未支持域 builds 点名）；408/429 mock 腿（真实例不可廉价触达——
//      错误族 wire 形态由 T-415 服务端测试钉死，本腿只验 UI 消费）。
//   ④ 分页排序交互（消费 range 的 offset/limit 语义）：表头排序注入/翻转/
//      摘除 .sort() 子句（查询文本可见改写）；上一页/下一页改写 .offset()
//      并按 range.start_pos/limit 回显；K63 截断通告（mock 200 腿）。
//   ⑤ axe 双主题 serious=0（结果态 + 错误态）。
//
// 锚源：console-ux §10.5 T-419 批（search-mode 族 + search-aql-* 族——
// 全静态锚，spec 侧选择器全字面量）；search 页既有锚零改名（本 spec 的
// 复用锚 = search-count / search-result-<i> / search-columns 族）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 本 spec 夹具：独立 generic local 仓 + 指定（名, 字节数）文件（幂等 PUT） */
async function seedAQLFixture(files: Array<{ name: string; bytes: number }>): Promise<string> {
  const repo = uniq('t419')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${repo}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  for (const f of files) {
    const put = await client.request('PUT', `/binflow/${repo}/sort/${f.name}`, {
      raw: true,
      headers: { 'Content-Type': 'application/octet-stream' },
      body: 'x'.repeat(f.bytes),
    })
    expect(put.status).toBeLessThan(300)
  }
  return repo
}

/** 进 AQL 模式并执行一条查询（编辑器 → 执行钮），等首行 */
async function runAql(page: import('@playwright/test').Page, query: string) {
  await page.fill('[data-testid="search-aql-input"]', query)
  await page.click('[data-testid="search-aql-run"]')
}

// ---- 1. 模式切换 + 深链 + 列选器两模式在场（①） -------------------------------

test('admin: search mode switch — basic default, ?mode=aql deep link, columns selector in both modes', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search')

  // 默认基本模式：查询面 = 顶栏驻留（页内输入 T-449 退役）、AQL 编辑器
  // 不在场、无查询无网格、URL 无 mode 参数
  await expect(page.locator('[data-testid="search-aql-input"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-grid"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()
  await expect(page).toHaveURL(/\/binflow\/ui\/search$/)
  // 模式切换容器（role=group——档位钮的语义父）
  await expect(page.locator('[data-testid="search-mode"]')).toBeVisible()
  await expect(page.locator('[data-testid="search-mode"]')).toHaveAttribute('role', 'group')

  // 切 AQL：编辑器在场、URL 承 mode=aql；列选器（T-414 壳）在场
  await page.click('[data-testid="search-mode-aql"]')
  await expect(page.locator('[data-testid="search-aql-input"]')).toBeVisible()
  await expect(page).toHaveURL(/\/binflow\/ui\/search\?mode=aql$/)
  await expect(page.locator('[data-testid="search-columns"]')).toBeVisible()
  await expect(page.locator('[data-testid="search-aql-run"]')).toBeVisible()

  // 深链 reload 持久（?mode=aql 直达——URL 即状态的读回环）
  await page.reload()
  await expect(page.locator('[data-testid="search-aql-input"]')).toBeVisible()

  // 切回基本：mode 参数摘除（查询不自动续跑——顶栏驻留词在，Enter 即重查）
  await page.click('[data-testid="search-mode-basic"]')
  await expect(page.locator('[data-testid="search-aql-input"]')).toHaveCount(0)
  await expect(page).toHaveURL(/\/binflow\/ui\/search$/)
})

// ---- 2. 合法查询：结果渲染 + include 投影进列 + 列选器联动（②） ----------------

test('admin: AQL query renders rows through the T-414 column frame; column selector operates in AQL mode', async ({
  page,
}) => {
  const marker = uniq('t419r')
  const repo = await seedAQLFixture([
    { name: `${marker}-a.bin`, bytes: 10 },
    { name: `${marker}-b.bin`, bytes: 20 },
  ])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search?mode=aql')
  await runAql(page, `items.find({"repo":"${repo}"}).include("repo","path","name","size","modified","sha256")`)

  // 行渲染（行锚沿既有 search-result-<i> 族）+ 计数副标（search-count 复用）
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  // 列框架归一（T-449）：name 列（链接）与 path 列分立——full path 跨单元格
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`${marker}-a.bin`)
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText('sort')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(repo)
  await expect(page.locator('[data-testid="search-count"]')).toHaveText('AQL 结果 – 2 行')

  // 列选器（AQL 模式内）：默认列集 = 选择列 + 制品|路径|仓库|修改时间（5 表头，
  // T-449 断言反转②）；勾入 sha256 → 6 + 行单元格 6；弃回 → 5
  const th = page.locator('[data-testid="search-page"] table thead th')
  await expect(th).toHaveCount(5)
  await expect(th.filter({ hasText: 'sha256' })).toHaveCount(0)
  await page.click('[data-testid="search-columns"]')
  await page.click('[data-testid="search-columns-item-sha256"]')
  await expect(th).toHaveCount(6)
  await expect(page.locator('[data-testid="search-result-0"] td')).toHaveCount(6)
  // sha256 列：投影值在场 + 一键拷贝（mono + CopyButton——T-414 列框架原样）
  const sha = page.locator('[data-testid="search-result-0"] td').nth(5)
  await expect(sha).toContainText('…')
  await expect(sha.locator('button')).toBeVisible()
  await page.click('[data-testid="search-columns-item-sha256"]')
  await expect(th).toHaveCount(5)

  // range 尾行：start_pos 回显 + 无 limit 时不伪造 limit 键
  await expect(page.locator('[data-testid="search-aql-range"]')).toContainText('start_pos 0 · 本页 2 行')
  await expect(page.locator('[data-testid="search-aql-range"]')).not.toContainText('limit')
})

// ---- 3. 语法错内联呈现：400 E-01 逐字 + 未支持域点名（③） ---------------------

test('admin: syntax error shows the 400 E-01 copy verbatim; unsupported domain is named, never a pseudo-empty set', async ({
  page,
}) => {
  const repo = await seedAQLFixture([{ name: 'f.bin', bytes: 1 }])
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search?mode=aql')

  // v1c 链序错样本（t407 活体锚）：.limit(1).sort(…) → E1 逐字（含原查询与残段）
  const bad = `items.find({"repo":"${repo}"}).limit(1).sort({"$asc":["name"]})`
  await runAql(page, bad)
  const err = page.locator('[data-testid="search-aql-error"]')
  await expect(err).toBeVisible({ timeout: 10_000 })
  await expect(err).toContainText('查询被拒绝（HTTP 400）')
  await expect(err).toContainText(
    `Failed to parse query: ${bad}, it looks like there is syntax error near the following sub-query: sort({"$asc":["name"]})`,
  )
  // 错误态不渲染结果表/空态文案（错误即终态——不吞 400 伪造空集）
  await expect(page.locator('[data-testid="search-result-0"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-count"]')).toHaveCount(0)

  // 未支持域：400 点名 builds（诚实拒绝，零伪空集）
  await runAql(page, 'builds.find({})')
  await expect(err).toBeVisible()
  await expect(err).toContainText('builds')
  await expect(page.locator('[data-testid="empty-state"]')).toHaveCount(0)
})

// ---- 4. 排序交互：表头注入/翻转/摘除 .sort() 子句（④排序） ---------------------

test('admin: column header clicks inject, flip and remove the .sort() clause in the editor', async ({
  page,
}) => {
  const marker = uniq('t419s')
  const repo = await seedAQLFixture([
    { name: `${marker}-small.bin`, bytes: 1 },
    { name: `${marker}-mid.bin`, bytes: 200 },
    { name: `${marker}-large.bin`, bytes: 40000 },
  ])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search?mode=aql')
  const base = `items.find({"repo":"${repo}"}).include("repo","path","name","size")`
  await runAql(page, base)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  const editor = page.locator('[data-testid="search-aql-input"]')

  // size 列默认不在场（T-449 断言反转②）——排序腿先经列选器勾入；
  // 随后收菜单（MUI Menu backdrop 挡表头点击）
  await page.click('[data-testid="search-columns"]')
  await page.click('[data-testid="search-columns-item-size"]')
  await page.keyboard.press('Escape')

  // 注入 asc（查询文本可见改写——无影子状态）；size 升序 → 首行 = 最小文件
  await page.click('[data-testid="search-aql-sort-size"]')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`${marker}-small.bin`)
  expect(await editor.inputValue()).toBe(`${base}.sort({"$asc":["size"]})`)
  // 表头 aria-sort 跟随
  await expect(page.locator('th').filter({ hasText: '大小' })).toHaveAttribute('aria-sort', 'ascending')

  // 翻转 desc → 首行 = 最大文件
  await page.click('[data-testid="search-aql-sort-size"]')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`${marker}-large.bin`)
  expect(await editor.inputValue()).toBe(`${base}.sort({"$desc":["size"]})`)
  await expect(page.locator('th').filter({ hasText: '大小' })).toHaveAttribute('aria-sort', 'descending')

  // 第三击摘除子句（回到无排序）
  await page.click('[data-testid="search-aql-sort-size"]')
  expect(await editor.inputValue()).toBe(base)
  await expect(page.locator('th').filter({ hasText: '大小' })).not.toHaveAttribute('aria-sort', 'ascending')
})

// ---- 5. 分页交互：.offset() 改写 + range.start_pos/limit 回显（④分页） --------

test('admin: pager rewrites .offset() in chain order and consumes the range echo', async ({
  page,
}) => {
  const marker = uniq('t419p')
  const repo = await seedAQLFixture(
    [1, 2, 3, 4, 5].map((n) => ({ name: `${marker}-f${n}.bin`, bytes: n * 10 })),
  )

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search?mode=aql')
  const base = `items.find({"repo":"${repo}"}).include("name").sort({"$asc":["name"]})`
  await runAql(page, `${base}.limit(2)`)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  const editor = page.locator('[data-testid="search-aql-input"]')
  const range = page.locator('[data-testid="search-aql-range"]')

  // 第 1 页：start_pos 0、limit 回显 2；上一页禁用、下一页在场（满窗 = 可能还有）
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`${marker}-f1.bin`)
  await expect(range).toContainText('start_pos 0 · 本页 2 行 · total 2（流式语义 = 本页行数，非全量计数） · limit 2')
  await expect(page.locator('[data-testid="search-aql-prev"]')).toBeDisabled()
  await expect(page.locator('[data-testid="search-aql-next"]')).toBeEnabled()

  // 下一页：.offset(2) 注入且链序在 .limit() 之前；行 = f3/f4
  await page.click('[data-testid="search-aql-next"]')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`${marker}-f3.bin`)
  expect(await editor.inputValue()).toBe(`${base}.offset(2).limit(2)`)
  await expect(range).toContainText('start_pos 2')

  // 再下一页：offset(4)，末页 1 行 < limit → 下一页禁用（无截断标记即末尾）
  await page.click('[data-testid="search-aql-next"]')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`${marker}-f5.bin`)
  await expect(page.locator('[data-testid="search-result-1"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-aql-next"]')).toBeDisabled()
  await expect(page.locator('[data-testid="search-aql-prev"]')).toBeEnabled()

  // 上一页：offset(2)（按 limit 页大小回退）
  await page.click('[data-testid="search-aql-prev"]')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`${marker}-f3.bin`)
  expect(await editor.inputValue()).toBe(`${base}.offset(2).limit(2)`)
})

// ---- 6. mock 腿：429/408 错误族 + K63 截断通告（③④的不可廉价触达面） ---------

test('admin: 429/408 error families and the K63 truncation notice render inline (mocked wire)', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search?mode=aql')
  const err = page.locator('[data-testid="search-aql-error"]')

  // 429 + Retry-After（aql.md §4 E8——body 逐字 too many requests）
  await page.route('**/binflow/api/search/aql', (route) =>
    route.fulfill({
      status: 429,
      headers: { 'Content-Type': 'application/json', 'Retry-After': '1' },
      body: JSON.stringify({ errors: [{ status: 429, message: 'too many requests' }] }),
    }),
  )
  await runAql(page, 'items.find({"repo":"x"})')
  await expect(err).toBeVisible({ timeout: 10_000 })
  await expect(err).toContainText('并发已满（HTTP 429）')
  await expect(err).toContainText('too many requests')
  await page.unroute('**/binflow/api/search/aql')

  // 408（执行超时——码位非 503/504，文案透传）
  await page.route('**/binflow/api/search/aql', (route) =>
    route.fulfill({
      status: 408,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ errors: [{ status: 408, message: 'context deadline exceeded' }] }),
    }),
  )
  await runAql(page, 'items.find({"repo":"y"})')
  await expect(err).toContainText('查询超时（HTTP 408）')
  await expect(err).toContainText('context deadline exceeded')
  await page.unroute('**/binflow/api/search/aql')

  // K63 截断通告：range.notification 官方文案逐字 + 下一页保持可用
  //（mock 体自洽：end_pos/total = 行数——流式约定，本腿不伪造矛盾 wire）
  await page.route('**/binflow/api/search/aql', (route) =>
    route.fulfill({
      status: 200,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        results: [{ repo: 'r', path: 'a', name: 'n.bin', size: 10 }],
        range: {
          start_pos: 0,
          end_pos: 1,
          total: 1,
          notification: 'AQL query reached the search hard limit, results are trimmed.',
        },
      }),
    }),
  )
  await runAql(page, 'items.find({"repo":"z"})')
  const note = page.locator('[data-testid="search-aql-notification"]')
  await expect(note).toBeVisible({ timeout: 10_000 })
  await expect(note).toContainText('AQL query reached the search hard limit, results are trimmed.')
  await expect(page.locator('[data-testid="search-aql-range"]')).toContainText('start_pos 0 · 本页 1 行 · total 1')
  // notification 在场 = 还有更多行——下一页保持可用（无 limit 声明不回显 limit 键）
  await expect(page.locator('[data-testid="search-aql-range"]')).not.toContainText('limit')
  await expect(page.locator('[data-testid="search-aql-next"]')).toBeEnabled()
})

// ---- 7. axe 双主题：结果态 + 错误态（⑤） ---------------------------------------

test('axe: AQL mode — results and error states clean in both themes', async ({ page }, testInfo: TestInfo) => {
  test.setTimeout(300_000)
  const repo = await seedAQLFixture([{ name: 'axe.bin', bytes: 5 }])
  await loginAs(page, 'admin')

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/search?mode=aql')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)

    // 结果态（编辑器 + 表头排序钮 + 结果表 + range 尾行同屏）
    await runAql(page, `items.find({"repo":"${repo}"}).include("repo","path","name","size","modified","sha256")`)
    await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
    await expectA11yClean(page, testInfo)

    // 错误态（内联 Alert——mono 长文案的对比度面）
    await page.route('**/binflow/api/search/aql', (route) =>
      route.fulfill({
        status: 400,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          errors: [
            {
              status: 400,
              message:
                'Failed to parse query: items.find({"repo":"x"}), it looks like there is syntax error near the following sub-query: items.find({"repo":"x"',
            },
          ],
        }),
      }),
    )
    await runAql(page, 'items.find({"repo":"x"')
    await expect(page.locator('[data-testid="search-aql-error"]')).toBeVisible()
    await expectA11yClean(page, testInfo)
    await page.unroute('**/binflow/api/search/aql')
  }
})
