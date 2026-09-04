import { expect, test } from '@playwright/test'
import type { TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { expectCopied } from '../m8/support/clipboard'
import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-449（M16 批次③ B7，FR-144.6——FE 搜索栈：断言反转② 归一承载）：
//
//   ① 列集归一（B-2.11）：默认列 = 选择列（固定）+ Artifact(name 链接) |
//      Path | Repository | Modified；大小/sha256 = 列选器可选项不默认呈现
//      （defaultHidden 语义 + 「恢复默认列」复位 ≠ 全选）。
//   ② 行导航（B-3.14）：行体 inert（点行体不导航），深链只在 name 单元格
//      （href = K67-3 路径段规范形，?focus= 发射端退役——属性级断言，
//      不经重定向）。
//   ③ 查询位置（B-2.13）：顶栏驻留（Enter → /search?q=，驻留回显）+
//      网格内快滤（客户端窄化 + 无匹配态）；AQL 模式编辑器共存（编辑器
//      管服务端查询，快滤管已取回行——两层正交）。
//   ④ 快搜空历史占位（B-2.14/B-3.16 翻正腿）：聚焦恒渲染下拉，空历史
//      给「暂无最近搜索」占位（对位 Artifactory "No recent searches yet"）。
//   ⑤ 日期格式（B-3.15 结果表腿）：modified = dd-MM-yy HH:mm:ss +ZZZZ
//      （正则断言含时区偏移）。
//   ⑥ axe 双主题：结果 + 快滤激活 + AQL 模式结果态。
//
// 锚源：console-ux §10.5 T-449 批（search-quick-filter / search-quick-count /
// search-quick-filter-empty / search-select-all / search-row-select-<i> /
// search-selection-copy / search-result-link-<i> / search-grid /
// topbar-search-recent-empty + 复役 topbar-search-recent-clear，
// v1.38）；既有锚 search-result-<i> / search-count / search-pager /
// search-columns 族零改名。Dashboard 发射端路径段腿在 m8/auxiliary
// （URL 级断言既有），本 spec 以链接 href 属性级钉死发射形态。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 本 spec 夹具：独立 generic local 仓 + 指定（目录, 名, 字节数）文件集 */
async function seedFixture(files: Array<{ dir: string; name: string; bytes: number }>): Promise<string> {
  const repo = uniq('t449')
  const client = m8Client()
  await client.request('PUT', `/binflow/api/repositories/${repo}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  for (const f of files) {
    const path = f.dir ? `/${f.dir}/${f.name}` : `/${f.name}`
    const put = await client.request('PUT', `/binflow${repo}${path}`, {
      raw: true,
      headers: { 'Content-Type': 'application/octet-stream' },
      body: 'x'.repeat(f.bytes),
    })
    expect(put.status).toBeLessThan(300)
  }
  return repo
}

/** 顶栏驻留查询：填词 + Enter → /search?q=（T-449 起查询面唯一入口）。
 *  T-475 确定性：登录落地链（/login → / → index 重定向 /artifacts）上有
 *  两处竞速源——① 顶栏受控草稿锚在 location.key（驻留回显语义），导航
 *  换 key 即把已填词回滚为空；② Suspense 边界在 AppShell 之上（main.tsx
 *  懒加载布局），路由切换的 chunk 装载会整体重挂 AppShell——草稿状态随
 *  重挂清零。慢 runner 把窗口拉宽后 fill/Enter 踩进去 = Enter 提交空词 →
 *  裸 /search（无 ?q，CI 三连败形态）。三层钉死：等落地页**内容**就位
 *  （URL 先于重挂变化，waitForURL 不够）；受控值 poll 钉住（被回滚即重填
 *  自愈）；提交后核对 URL，空词分支（裸 /search）= 竞速踩中，整个重试。 */
async function topbarQuery(page: import('@playwright/test').Page, term: string) {
  const target = new RegExp(`/binflow/ui/search\\?q=${term}$`)
  const input = page.locator('[data-testid="topbar-search"]')
  await page.waitForSelector('[data-testid="tree-page"]') // 落地页内容（最终挂载帧）
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
  await expect(page).toHaveURL(target) // 三轮竞速重试后仍空词分支——带 URL 上下文响亮失败
}

// ---- 1. 列集归一 + 列选器默认态 + 日期格式（①⑤ B-2.11/B-3.15） ---------------

test('admin: default column set = select|Artifact|Path|Repository|Modified; size/sha256 opt-in; date carries tz offset', async ({
  page,
}) => {
  const marker = uniq('t449c')
  const repo = await seedFixture([{ dir: 'acme', name: `${marker}-one.bin`, bytes: 10 }])

  await loginAs(page, 'admin')
  await topbarQuery(page, marker)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })

  // 默认列集（B-2.11 断言反转②）：选择列（checkbox 表头，无文本）+ 四数据列
  const th = page.locator('[data-testid="search-grid"] thead th')
  await expect(th).toHaveCount(5)
  await expect(th.nth(0).locator('[data-testid="search-select-all"]')).toHaveCount(1)
  for (const label of ['制品', '路径', '仓库', '修改时间']) {
    await expect(th.filter({ hasText: label })).toHaveCount(1)
  }
  // 大小/sha256 不默认在场（移入列选器可选项——反断言）
  await expect(th.filter({ hasText: '大小' })).toHaveCount(0)
  await expect(th.filter({ hasText: 'sha256' })).toHaveCount(0)

  // 行单元格与表头同步（选择列 + 四数据列 = 5 cells）
  await expect(page.locator('[data-testid="search-result-0"] td')).toHaveCount(5)
  await expect(page.locator('[data-testid="search-result-0"] td').nth(1)).toContainText(`${marker}-one.bin`)
  await expect(page.locator('[data-testid="search-result-0"] td').nth(2)).toContainText('acme')
  await expect(page.locator('[data-testid="search-result-0"] td').nth(3)).toContainText(repo)

  // 日期格式（B-3.15）：dd-MM-yy HH:mm:ss +ZZZZ——正则含时区偏移
  await expect(page.locator('[data-testid="search-result-0"] td').nth(4)).toHaveText(
    /\d{2}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{4}/,
  )

  // 列选器：4/6 默认档——name/path/repo/modified 勾、size/sha256 未勾
  //（选择器全字面量——对账器口径，t387/t414 先例同款纪律）
  await page.click('[data-testid="search-columns"]')
  await expect(page.locator('[data-testid="search-columns-item-name"]')).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('[data-testid="search-columns-item-path"]')).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('[data-testid="search-columns-item-repo"]')).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('[data-testid="search-columns-item-modified"]')).toHaveAttribute('aria-checked', 'true')
  await expect(page.locator('[data-testid="search-columns-item-size"]')).toHaveAttribute('aria-checked', 'false')
  await expect(page.locator('[data-testid="search-columns-item-sha256"]')).toHaveAttribute('aria-checked', 'false')
  await expect(page.locator('[data-testid="search-columns"]')).toContainText('列 4/6')

  // 勾入 size → 表头 6（列选器可选项语义）；再勾 sha256 → 7；「恢复默认列」
  // 复位回默认集（≠ 全选——localStorage 落默认隐藏集而非空数组）
  await page.click('[data-testid="search-columns-item-size"]')
  await expect(th).toHaveCount(6)
  await page.click('[data-testid="search-columns-item-sha256"]')
  await expect(th).toHaveCount(7)
  await page.click('[data-testid="search-columns-reset"]')
  await expect(th).toHaveCount(5)
  expect(await page.evaluate(() => localStorage.getItem('binflow-console-cols-search'))).toBe('["size","sha256"]')
  await page.keyboard.press('Escape')

  // reload 持久：默认档跨会话（存储缺席回落 defaultHidden——先清存储再验）
  await page.evaluate(() => localStorage.removeItem('binflow-console-cols-search'))
  await page.reload()
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(th).toHaveCount(5)
})

// ---- 2. 行导航 name-only + 路径段深链（② B-3.14 + K67-3 发射端） --------------

test('admin: row body inert; only the name cell deep-links in path-segment form (no ?focus=)', async ({ page }) => {
  const marker = uniq('t449n')
  const repo = await seedFixture([{ dir: 'acme', name: `${marker}.bin`, bytes: 5 }])

  await loginAs(page, 'admin')
  await topbarQuery(page, marker)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })

  // name 单元格 = 深链（href 路径段规范形——文件是路径末段，零查询串）
  const link = page.locator('[data-testid="search-result-link-0"]')
  await expect(link).toBeVisible()
  await expect(link).toHaveAttribute('href', `/binflow/ui/artifacts/${repo}/acme/${marker}.bin`)
  await expect(link).not.toHaveAttribute('href', /focus=/)

  // 行体 inert（B-3.14）：点路径单元格（非链接区）不导航
  await page.locator('[data-testid="search-result-0"] td').nth(2).click()
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/search\\?q=${marker}$`))

  // 链接点击 → 跨仓树定位（T-236 深链消费）：路径段直达 + 详情面板
  await link.click()
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${repo}/acme/${marker}\\.bin$`))
  await expect(page.locator('[data-testid="node-detail"]')).toContainText(`${marker}.bin`)
})

// ---- 3. 网格内快滤 + 选择列 + 批量复制路径（③ B-2.13 + 选择列） ----------------

test('admin: in-grid quick filter narrows client-side; selection column copies selected paths', async ({ page }) => {
  const marker = uniq('t449q')
  const repo = await seedFixture([
    { dir: 'acme', name: `${marker}-alpha.bin`, bytes: 1 },
    { dir: 'acme', name: `${marker}-beta.bin`, bytes: 2 },
    { dir: 'other', name: `${marker}-gamma.bin`, bytes: 3 },
  ])

  await loginAs(page, 'admin')
  await topbarQuery(page, marker)
  await expect(page.locator('[data-testid="search-count"]')).toHaveText(`搜索结果 – 3 项`)
  await expect(page.locator('[data-testid="search-pager"]')).toContainText('显示 1 – 3 / 共 3 项')

  // 快滤：子串窄化（name/dir/repo 三域）+ 命中计数；分页行集同步
  await page.fill('[data-testid="search-quick-filter"]', 'acme')
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText('alpha')
  await expect(page.locator('[data-testid="search-result-1"]')).toContainText('beta')
  await expect(page.locator('[data-testid="search-result-2"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-quick-count"]')).toHaveText('快滤命中 2 / 3')
  await expect(page.locator('[data-testid="search-pager"]')).toContainText('共 2 项（快滤自 3）')

  // 无匹配态：占位提示在场 + 表体退场；清空恢复
  await page.fill('[data-testid="search-quick-filter"]', 'zzz-no-such')
  await expect(page.locator('[data-testid="search-quick-filter-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="search-result-0"]')).toHaveCount(0)
  await page.fill('[data-testid="search-quick-filter"]', '')
  await expect(page.locator('[data-testid="search-result-2"]')).toBeVisible()

  // 选择列：行选 + 全选（作用于快滤后可见行）+ 批量复制路径
  await page.fill('[data-testid="search-quick-filter"]', 'acme')
  await page.click('[data-testid="search-row-select-0"]')
  await expect(page.locator('[data-testid="search-selection-copy"]')).toContainText('复制路径（1）')
  await page.click('[data-testid="search-select-all"]')
  await expect(page.locator('[data-testid="search-selection-copy"]')).toContainText('复制路径（2）')
  await expectCopied(
    page,
    page.locator('[data-testid="search-selection-copy"]'),
    `${repo}/acme/${marker}-alpha.bin\n${repo}/acme/${marker}-beta.bin`,
  )

  // 全选再点 = 清空本页选择（选择动作行退场）
  await page.click('[data-testid="search-select-all"]')
  await expect(page.locator('[data-testid="search-selection-copy"]')).toHaveCount(0)
})

// ---- 4. 顶栏驻留查询 + 空历史占位（③④ B-2.13 + B-2.14/B-3.16） -----------------

test('topbar persistent query + quick-search empty-history placeholder renders on focus', async ({ page }) => {
  const marker = uniq('t449t')
  await seedFixture([{ dir: 'acme', name: `${marker}.bin`, bytes: 1 }])

  await loginAs(page, 'admin')

  // 空历史占位（B-3.16 翻正——历史为空聚焦也渲染下拉 + 占位文案）
  await page.goto('/binflow/ui/dashboard')
  await page.focus('[data-testid="topbar-search"]')
  const dd = page.locator('[data-testid="topbar-search-recent"]')
  await expect(dd).toBeVisible()
  await expect(page.locator('[data-testid="topbar-search-recent-empty"]')).toHaveText('暂无最近搜索')
  // 无历史 = 无清除钮（零死控件）；Esc 收起
  await expect(page.locator('[data-testid="topbar-search-recent-clear"]')).toHaveCount(0)
  await page.keyboard.press('Escape')
  await expect(dd).toHaveCount(0)

  // 顶栏驻留查询：Enter 提交 → URL 即查询态 + 驻留回显（输入框显值 = q）
  await page.fill('[data-testid="topbar-search"]', marker)
  await page.press('[data-testid="topbar-search"]', 'Enter')
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="topbar-search"]')).toHaveValue(marker)

  // 提交入历史：再聚焦 = 历史项在场（占位退场）；清除钮在场且一键清空
  //（清除收下拉——重新聚焦后空历史占位回场）
  await page.click('h2.search-headline')
  await page.focus('[data-testid="topbar-search"]')
  await expect(page.locator('[data-testid="topbar-search-recent-item-0"]')).toHaveText(marker)
  await expect(page.locator('[data-testid="topbar-search-recent-empty"]')).toHaveCount(0)
  await page.click('[data-testid="topbar-search-recent-clear"]')
  await expect(page.locator('[data-testid="topbar-search-recent"]')).toHaveCount(0)
  expect(JSON.parse((await page.evaluate(() => localStorage.getItem('binflow-console-recent-searches'))) ?? '[]')).toEqual([])
  await page.click('h2.search-headline')
  await page.focus('[data-testid="topbar-search"]')
  await expect(page.locator('[data-testid="topbar-search-recent-empty"]')).toBeVisible()

  // 深链直达（?q=）：结果立即呈现 + 驻留回显（直接进入不经顶栏）
  await page.goto(`/binflow/ui/search?q=${marker}`)
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="topbar-search"]')).toHaveValue(marker)

  // 空态引导（无 q）：指向顶栏查询面；网格未渲染（无表 = 无快滤/选择列）
  await page.goto('/binflow/ui/search')
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('在顶栏输入关键词开始搜索')
  await expect(page.locator('[data-testid="search-grid"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-quick-filter"]')).toHaveCount(0)
})

// ---- 5. AQL 编辑器共存形态：列框架归一复用 + 快滤正交（③ t419 族锚不破） -------

test('admin: AQL mode rides the converged column frame — name link, sort anchor, quick filter orthogonal', async ({
  page,
}) => {
  const marker = uniq('t449a')
  const repo = await seedFixture([
    { dir: 'sort', name: `${marker}-a.bin`, bytes: 10 },
    { dir: 'sort', name: `${marker}-b.bin`, bytes: 20 },
  ])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/search?mode=aql')
  await page.fill('[data-testid="search-aql-input"]', `items.find({"repo":"${repo}"}).include("repo","path","name","size","modified","sha256")`)
  await page.click('[data-testid="search-aql-run"]')
  await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('[data-testid="search-count"]')).toHaveText('AQL 结果 – 2 行')

  // AQL 行复用锚（t419 族）：行锚 + 计数 + range 尾行 + 排序锚（modified 默认在场）
  await expect(page.locator('[data-testid="search-aql-range"]')).toContainText('start_pos 0')
  await expect(page.locator('[data-testid="search-aql-sort-modified"]')).toBeVisible()
  // 列框架归一：选择列 + name 链接（AQL 投影行同样仅 name 深链）
  await expect(page.locator('[data-testid="search-grid"] thead th')).toHaveCount(5)
  await expect(page.locator('[data-testid="search-result-link-0"]')).toHaveAttribute(
    'href',
    `/binflow/ui/artifacts/${repo}/sort/${marker}-a.bin`,
  )
  // 日期格式腿（AQL 行同形——formatStamp 单源）
  await expect(page.locator('[data-testid="search-result-0"] td').nth(4)).toHaveText(
    /\d{2}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{4}/,
  )

  // 快滤正交（编辑器管服务端、快滤管已取回行）：窄化不清查询文本
  const editor = page.locator('[data-testid="search-aql-input"]')
  await page.fill('[data-testid="search-quick-filter"]', `${marker}-b`)
  await expect(page.locator('[data-testid="search-result-0"]')).toContainText(`${marker}-b.bin`)
  await expect(page.locator('[data-testid="search-result-1"]')).toHaveCount(0)
  expect(await editor.inputValue()).toContain(`items.find({"repo":"${repo}"})`)

  // 排序交互经归一表头（.sort() 注入——锚 search-aql-sort-* 在场可点）
  await page.fill('[data-testid="search-quick-filter"]', '')
  await page.click('[data-testid="search-aql-sort-modified"]')
  expect(await editor.inputValue()).toContain('.sort(')
})

// ---- 6. axe 双主题：结果 + 快滤激活 + AQL 结果态（⑥） --------------------------

test('axe: search stack — results, quick-filter active, AQL mode clean in both themes', async ({ page }, testInfo: TestInfo) => {
  test.setTimeout(300_000)
  const marker = uniq('t449x')
  const repo = await seedFixture([
    { dir: 'acme', name: `${marker}-axe.bin`, bytes: 5 },
    { dir: 'other', name: `${marker}-two.bin`, bytes: 6 },
  ])

  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)

    // 基本模式结果态（网格工具行 + 选择列 + 链接列 + 分页行同屏）
    await page.goto(`/binflow/ui/search?q=${marker}`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
    await page.click('[data-testid="search-row-select-0"]') // 选择动作行在场扫
    await expectA11yClean(page, testInfo)

    // 快滤激活态（命中计数 + 无匹配占位）
    await page.fill('[data-testid="search-quick-filter"]', 'zzz-none')
    await expect(page.locator('[data-testid="search-quick-filter-empty"]')).toBeVisible()
    await expectA11yClean(page, testInfo)

    // AQL 模式结果态（编辑器 + 排序表头 + range 尾行）
    await page.goto('/binflow/ui/search?mode=aql')
    await page.fill('[data-testid="search-aql-input"]', `items.find({"repo":"${repo}"}).include("repo","path","name")`)
    await page.click('[data-testid="search-aql-run"]')
    await expect(page.locator('[data-testid="search-result-0"]')).toBeVisible({ timeout: 10_000 })
    await expectA11yClean(page, testInfo)
  }
})
