import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-265 (FR-82-AC2 / AC9, PRD N14 + N21): the M8 debt-pack console pair —
//
//   N14 tree filter   QA-3: the「过滤当前层」term survives cross-level /
//   reset             cross-repo navigation, so the children table renders a
//                     misleading "no match" on a level the term was never
//                     typed against. The scope rule under test: (repo, dir)
//                     change clears the term (and the repo-list filter on a
//                     repo boundary); same-layer navigation (?focus=) keeps
//                     it. Drill-down must land a NON-ZERO sub-level with no
//                     manual clearing, and a filtered-empty state must offer
//                     the standard「无匹配」copy + clear action (§3.1 空).
//   N21 topbar        the §2.1 entry is a real input now: Enter → /search?q=
//   search            with the term committed into recentSearches (the SAME
//                     localStorage store the search page reads), Esc clears
//                     + blurs, ⌘K// focus it, and the tree filter state is
//                     independent of the topbar term.
//
// Anchors follow ADR-0029 decision 3 (data-testid only); the topbar recent
// dropdown reuses the search page's interaction shape under a topbar-* family
// registered with this ticket's §10.5 batch.

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 两层夹具：root（2 目录 + 2 文件）与 root/alpha（1 目录 + 2 文件）——
 * 过滤词在两层都有可判定的非空/收窄行为 */
async function seedTreeFixture(): Promise<{ repoA: string; repoB: string }> {
  const repoA = `${uniq('t265a')}-local`
  const repoB = `${uniq('t265b')}-local`
  const client = m8Client()
  for (const key of [repoA, repoB]) {
    await client.request('PUT', `/binflow/api/repositories/${key}`, {
      body: { rclass: 'local', packageType: 'generic' },
    })
  }
  await client.request('PUT', `/binflow/${repoA}/root-a.txt`, { raw: true, body: 'a\n' })
  await client.request('PUT', `/binflow/${repoA}/root-b.bin`, { raw: true, body: 'b\n' })
  await client.request('PUT', `/binflow/${repoA}/alpha/alpha-1.txt`, { raw: true, body: '1\n' })
  await client.request('PUT', `/binflow/${repoA}/alpha/alpha-2.bin`, { raw: true, body: '2\n' })
  await client.request('PUT', `/binflow/${repoA}/alpha/nested/alpha-n.txt`, { raw: true, body: 'n\n' })
  await client.request('PUT', `/binflow/${repoA}/beta/beta-1.txt`, { raw: true, body: 'b1\n' })
  // 仅目录层（filesOnly 谓词收窄出空的判定面）
  await client.request('PUT', `/binflow/${repoA}/group/g-one/x.txt`, { raw: true, body: 'g\n' })
  await client.request('PUT', `/binflow/${repoB}/b-root.txt`, { raw: true, body: 'br\n' })
  return { repoA, repoB }
}

// ---- N14 / QA-3：过滤复位（跨层清空 / 同层保留 / 空态不误导） ------------------

test('tree filter: drill-down resets the term, sub-level renders non-zero (QA-3)', async ({ page }) => {
  const { repoA } = await seedTreeFixture()
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${repoA}`)
  await expect(page.locator('[data-testid="tree-row-root-a.txt"]')).toBeVisible()

  // 根层收窄：过滤词「root」命中 2 文件、隐掉目录行
  await page.fill('[data-testid="tree-filter"]', 'root')
  await expect(page.locator('[data-testid="tree-row-root-a.txt"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-list"] tbody tr')).toHaveCount(2)

  // 下钻（左树进 alpha——跨层导航）：词清空 + 子层非零呈现，无需手动清空。
  // T-434（select≠expand）：选中仓不再强制展开——树节点可见前先展开仓根
  await page.locator(`[data-testid="tree-repo-${repoA}"] .twisty`).click()
  await page.click('[data-testid="tree-node-alpha"]')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${repoA}/alpha$`))
  await expect(page.locator('[data-testid="tree-filter"]')).toHaveValue('')
  await expect(page.locator('[data-testid="tree-list"] tbody tr').first()).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-alpha-1.txt"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-nested"]')).toBeVisible()

  // 同层内导航（文件选中——URL 路径末段，T-434）保留词——children 集合未变，
  // 词语义完整
  await page.fill('[data-testid="tree-filter"]', 'alpha-1')
  await expect(page.locator('[data-testid="tree-list"] tbody tr')).toHaveCount(1)
  await page.click('[data-testid="tree-row-alpha-1.txt"]')
  await expect(page).toHaveURL(
    new RegExp(`/binflow/ui/artifacts/${repoA}/alpha/alpha-1\\.txt$`),
  )
  await expect(page.locator('[data-testid="tree-filter"]')).toHaveValue('alpha-1')

  // 面包屑上跳（跨层）再次清空
  await page.click('[data-testid="tree-breadcrumb"] button.crumb:first-child')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${repoA}$`))
  await expect(page.locator('[data-testid="tree-filter"]')).toHaveValue('')
})

test('tree filter: cross-repo switch clears both filters (term + repo list)', async ({ page }) => {
  const { repoA, repoB } = await seedTreeFixture()
  await loginAs(page, 'admin')

  // 跨仓根：仓库清单过滤收窄到 A
  await page.goto('/binflow/ui/artifacts')
  await page.fill('[data-testid="tree-repo-filter"]', repoA)
  await expect(page.locator(`[data-testid="tree-repo-${repoB}"]`)).toHaveCount(0)
  await expect(page.locator(`[data-testid="tree-repo-${repoA}"]`)).toBeVisible()

  // 进入 A（跨仓边界）：清单过滤复位——当前仓分支不再被滤掉（QA-3 同款误导）
  await page.click(`[data-testid="tree-repo-${repoA}"]`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${repoA}$`))
  await expect(page.locator('[data-testid="tree-repo-filter"]')).toHaveValue('')
  await expect(page.locator(`[data-testid="tree-repo-${repoB}"]`)).toBeVisible()

  // A 内当前层过滤词置入后切仓 B：词随 (repo, dir) 作用域一并清空
  await page.fill('[data-testid="tree-filter"]', 'root')
  await page.click(`[data-testid="tree-repo-${repoB}"]`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${repoB}$`))
  await expect(page.locator('[data-testid="tree-filter"]')).toHaveValue('')
  await expect(page.locator('[data-testid="tree-row-b-root.txt"]')).toBeVisible()
})

test('tree filter: filtered-empty state is explicit and clearable, not a bare empty tree', async ({
  page,
}) => {
  const { repoA } = await seedTreeFixture()
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${repoA}`)
  await expect(page.locator('[data-testid="tree-row-root-a.txt"]')).toBeVisible()

  // 过滤后为空（§3.1）：标准文案 + 清除钮——不是「这一层没有东西」的误导
  await page.fill('[data-testid="tree-filter"]', 'zzz-no-such-entry')
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('无匹配「zzz-no-such-entry」')
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('只作用于当前层')
  await page.click('[data-testid="tree-filter-clear"]')
  await expect(page.locator('[data-testid="tree-filter"]')).toHaveValue('')
  await expect(page.locator('[data-testid="tree-row-root-a.txt"]')).toBeVisible()

  // 仅「只看文件」收窄出的空（无过滤词）：按实际谓词呈现，不误报「无匹配」
  await page.goto(`/binflow/ui/artifacts/${repoA}/group`) // group 层只有目录 g-one
  await expect(page.locator('[data-testid="tree-row-g-one"]')).toBeVisible()
  await page.locator('.filter-bar .check-row input').check()
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('只有目录')
  await page.click('[data-testid="tree-filter-clear"]')
  await expect(page.locator('[data-testid="tree-list"] tbody tr')).toHaveCount(1)
})

// ---- N21 / AC9：顶栏搜索框全链 ------------------------------------------------

test('topbar search: Enter submits to /search?q= and commits the term into recentSearches', async ({
  page,
}) => {
  const { repoA } = await seedTreeFixture()
  const marker = 'alpha-1'
  await loginAs(page, 'admin')

  // 输入 → Enter：/search?q= 深链 + 顶栏驻留回显（T-449：查询面 = 顶栏——
  // 焦点驻留顶栏输入，显值 = q）+ 立即查询
  await page.goto(`/binflow/ui/artifacts/${repoA}`)
  await page.fill('[data-testid="topbar-search"]', marker)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/search\\?q=${marker}$`))
  await expect(page.locator('[data-testid="topbar-search"]')).toBeFocused()
  await expect(page.locator('[data-testid="topbar-search"]')).toHaveValue(marker)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(marker)

  // 顶栏提交即入列：重新聚焦顶栏 → 最近搜索首项 = 顶栏提交词（recentSearches
  // 单承载顶栏下拉——T-449 随页内输入退役）
  await page.click('h2.search-headline')
  await page.focus('[data-testid="topbar-search"]')
  await expect(page.locator('[data-testid="topbar-search-recent-item-0"]')).toHaveText(marker)
  const stored = await page.evaluate(() => window.localStorage.getItem('binflow-console-recent-searches'))
  expect(stored).toContain(marker)

  // 返回树：过滤态独立——顶栏词不渗入树的过滤输入（组件重挂载即回初始空）
  await page.goBack()
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${repoA}$`))
  await expect(page.locator('[data-testid="tree-filter"]')).toHaveValue('')
  await expect(page.locator('[data-testid="tree-repo-filter"]')).toHaveValue('')
})

test('topbar search: recent dropdown rides the shared store; Esc clears + blurs; shortcuts focus', async ({
  page,
}) => {
  const { repoA } = await seedTreeFixture()
  const marker = 'root-a'
  await loginAs(page, 'admin')

  // 顶栏提交一次（入列），回应用页聚焦顶栏 → 下拉呈现共享历史
  await page.goto(`/binflow/ui/artifacts/${repoA}`)
  await page.fill('[data-testid="topbar-search"]', marker)
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await page.goto(`/binflow/ui/artifacts/${repoA}`)

  await page.focus('[data-testid="topbar-search"]')
  await expect(page.locator('[data-testid="topbar-search-recent"]')).toBeVisible()
  await expect(page.locator('[data-testid="topbar-search-recent-item-0"]')).toHaveText(marker)

  // 子串收窄 + ↑↓/Enter 键盘链应用历史项（沿 SearchPage 语义）
  await page.keyboard.type('root')
  await expect(page.locator('[data-testid="topbar-search-recent-item-0"]')).toHaveText(marker)
  await page.keyboard.press('ArrowDown')
  await expect(page.locator('[data-testid="topbar-search-recent-item-0"]')).toHaveClass(/active/)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/search\\?q=${marker}$`))

  // Esc 清空失焦两段（T-449 起「无匹配」也呈现占位下拉——恒渲染翻正）：
  // Esc#1 收下拉（含占位态）；Esc#2 清空 + 失焦
  await page.goto(`/binflow/ui/artifacts/${repoA}`)
  await page.focus('[data-testid="topbar-search"]')
  await expect(page.locator('[data-testid="topbar-search-recent"]')).toBeVisible()
  await page.keyboard.press('Escape') // 收下拉
  await expect(page.locator('[data-testid="topbar-search-recent"]')).toHaveCount(0)
  await page.keyboard.type('transient')
  await expect(page.locator('[data-testid="topbar-search-recent-empty"]')).toBeVisible()
  await page.keyboard.press('Escape') // 占位态下拉在场 → 先收它
  await expect(page.locator('[data-testid="topbar-search-recent"]')).toHaveCount(0)
  await page.keyboard.press('Escape') // 下拉已收 → 清空 + 失焦
  await expect(page.locator('[data-testid="topbar-search"]')).toHaveValue('')
  await expect(page.locator('[data-testid="topbar-search"]')).not.toBeFocused()

  // ⌘K 聚焦（下拉随聚焦展开，Esc#1 收它、Esc#2 清空失焦）；「/」非输入态聚焦
  await page.keyboard.press('Control+k')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeFocused()
  await expect(page.locator('[data-testid="topbar-search-recent"]')).toBeVisible()
  await page.keyboard.press('Escape')
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="topbar-search"]')).not.toBeFocused()
  await page.keyboard.press('/')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeFocused()
})

test('topbar search: empty-term Enter keeps the plain /search entry (autoFocus preserved)', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')

  // 空词 Enter = 纯入口跳 /search（T-235 前身通道；T-449 起查询面驻留
  // 顶栏——焦点留驻顶栏输入，页内网格未渲染）
  await page.focus('[data-testid="topbar-search"]')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/search$/)
  await expect(page.locator('[data-testid="topbar-search"]')).toBeFocused()
  await expect(page.locator('[data-testid="topbar-search"]')).toHaveValue('')
  await expect(page.locator('[data-testid="search-grid"]')).toHaveCount(0)
})

// ---- axe：新输入框结构可达（serious/critical = 0；§9） ------------------------

test('axe: topbar search input + open recent dropdown scan clean at serious/critical', async (
  { page },
  testInfo,
) => {
  const { repoA } = await seedTreeFixture()
  await loginAs(page, 'admin')

  // 先造一条历史（词无命中也无妨——入列在提交时），让下拉展开态
  // （输入框 aria-expanded + 列表 + 清除钮）进扫描面
  await page.goto(`/binflow/ui/artifacts/${repoA}`)
  await page.fill('[data-testid="topbar-search"]', 'axe-probe')
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="empty-state"]')).toBeVisible({ timeout: 10_000 })
  await page.goto(`/binflow/ui/artifacts/${repoA}`)
  await page.focus('[data-testid="topbar-search"]')
  await expect(page.locator('[data-testid="topbar-search-recent"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '.app-topbar' })
})
