import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { expectCopied } from '../m8/support/clipboard'

// T-447（M16 批次③ B6，FR-144.4/.5——B-2.9 属性编辑解剖 + B-2.12 下载形态）：
//
//   ① 属性编辑常显（B-2.9 翻正，7.161.20 活体实证：Property name /
//      Property value 常显输入 + Add + 网格搜索）：隐藏「+ 新增属性」
//      旗标表单与逐行 ✎/🗑 退役（反断言钉死）；同名键 Add = 值集整体
//      替换（§11.40，兄弟键保留）；行内删除与 E1 统一（Q2 出口①——删除
//      走危险确认，可拒绝）。
//   ② 网格搜索（M10 属性夹具数据腿——build/env/qa/owner 键值族经 REST
//      ?properties 面 seeding）：键子串 / 值子串 / 无匹配提示 / 清空恢复。
//   ③ 下载形态单 24px 图标钮（B-2.12 翻正）：两带文字按钮收敛（反断
//      言）；直接下载走内容面 GET（计数单源 +1）；校验能力收进伴随菜单
//      （Q9）——verify 动作 + 结果块 + checksums/mimeType 全在菜单内，
//      General 页不再平铺（Q9 终裁「校验块收进伴随形态」的反断言）。
//      下载动作计数联动（T-438 埋点单源）：FE 下载完成 → ?stats 重读 →
//      UI 计数随增。
//   ④ axe 双主题：属性页签全解剖 + 伴随菜单展开态。
//
// 计数断言口径（T-438 §3-2 探针面计数继承——item-info GET 亦计数）：
// 不断言绝对值，断言「动作后面值严格增长」+「UI == 同刻面值」。
//
// 夹具纪律：Playwright 栈自建（仓库 PUT + 内容面上传 + REST ?properties
// seeding，零外部状态）；锚源 = console-ux §10.5 T-447 批（node-props-search
// / node-props-search-empty / node-download-menu 族 / node-download-panel /
// node-download-checksums，v1.37）。

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

/** 同源 fetch（携带 session cookie）；属性 PUT 走 ?properties= 查询串 */
async function api(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; json: unknown; text: string }> {
  const r = await page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      })
      const text = await res.text()
      let json: unknown = null
      try {
        json = JSON.parse(text)
      } catch {
        json = null
      }
      return { status: res.status, json, text }
    },
    { method, path, body },
  )
  return r
}

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

interface StatsFace {
  downloadCount: number
}

async function statsFace(page: Page, repo: string, path: string): Promise<StatsFace> {
  const r = await api(page, 'GET', `/api/storage/${repo}/${path}?stats`)
  expect(r.status).toBe(200)
  return r.json as StatsFace
}

async function openFilePropsTab(page: Page, repo: string, fileRel: string) {
  await page.goto(`/binflow/ui/artifacts/${repo}/${fileRel}`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await page.click('[data-testid="node-tab-props"]')
  await expect(page.locator('[data-testid="node-props"]')).toBeVisible()
}

// ---------------------------------------------------------------------------
// ① 属性编辑常显（B-2.9）：常显表单 + 同名键替换 + 确认删除 + 旧形态退役
// ---------------------------------------------------------------------------

test('properties: always-visible Property/Value form + Add, same-key replace keeps siblings, delete behind danger confirm', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t447a')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 't447-props')
  await openFilePropsTab(page, key, 'docs/guide.md')

  // 常显表单（无旗标开关——空态也在场）+ 空态引导
  await expect(page.locator('[data-testid="node-props-key-input"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-props-values-input-new"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-props-add"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-props-empty"]')).toBeVisible()

  // 旧形态退役反断言：无旗标草稿行（node-props-row-new count 0）；行内
  // 操作仅删除一枚钮（✎ 编辑 / ✓ 保存 / ✕ 取消退役——B-2.9 解剖收敛，
  // 锚已入 §10.6 退役表，此处按行按钮数钉形态）
  await expect(page.locator('[data-testid="node-props-row-new"]')).toHaveCount(0)

  // Add：非法键即时反馈（零坏请求出浏览器）→ 修正后落库为值集
  await page.fill('[data-testid="node-props-key-input"]', 'bad key')
  await expect(page.getByText('键须匹配', { exact: false })).toBeVisible()
  await expect(page.locator('[data-testid="node-props-add"]')).toBeDisabled()
  await page.fill('[data-testid="node-props-key-input"]', 'qa')
  await page.fill('[data-testid="node-props-values-input-qa"]', 'passed, rc1')
  await page.click('[data-testid="node-props-add"]')
  const row = page.locator('[data-testid="node-props-row-qa"]')
  await expect(row).toBeVisible()
  await expect(row).toContainText('passed, rc1')
  await expect(row.locator('button')).toHaveCount(1)
  expect(JSON.parse((await api(page, 'GET', `/api/storage/${key}/docs/guide.md?properties=qa`)).text)).toEqual({
    properties: { qa: ['passed', 'rc1'] },
  })

  // 同名键 Add = 值集整体替换（replaceHint 可见）；REST 种的兄弟键保留
  await api(page, 'PUT', `/api/storage/${key}/docs/guide.md?properties=owner=team-a`)
  await page.click('[data-testid="node-tab-general"]')
  await page.click('[data-testid="node-tab-props"]')
  await expect(page.locator('[data-testid="node-props-row-owner"]')).toContainText('team-a')
  await page.fill('[data-testid="node-props-key-input"]', 'qa')
  await expect(page.getByText('同名键 = 整体替换其值集', { exact: false })).toBeVisible()
  await page.fill('[data-testid="node-props-values-input-qa"]', 'released')
  await page.click('[data-testid="node-props-add"]')
  await expect(page.locator('[data-testid="node-props-row-qa"]')).toContainText('released')
  await expect(page.locator('[data-testid="node-props-row-qa"]')).not.toContainText('passed')
  await expect(page.locator('[data-testid="node-props-row-owner"]')).toContainText('team-a')
  expect(JSON.parse((await api(page, 'GET', `/api/storage/${key}/docs/guide.md?properties=qa,owner`)).text)).toEqual({
    properties: { qa: ['released'], owner: ['team-a'] },
  })

  // 删除走危险确认（E1 统一/Q2 出口①）：拒绝保留、接受移除
  await page.click('[data-testid="node-props-delete-qa"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await page.click('[data-testid="confirm-cancel"]')
  await expect(page.locator('[data-testid="node-props-row-qa"]')).toBeVisible()
  await page.click('[data-testid="node-props-delete-qa"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="node-props-row-qa"]')).toHaveCount(0)
  expect(JSON.parse((await api(page, 'GET', `/api/storage/${key}/docs/guide.md?properties`)).text)).toEqual({
    properties: { owner: ['team-a'] },
  })
})

// ---------------------------------------------------------------------------
// ② 网格搜索（M10 属性夹具数据腿——REST ?properties 面 seeding 的键值族）
// ---------------------------------------------------------------------------

test('properties grid search: key substring, value substring, no-match hint, clear restores', async ({ page }) => {
  await login(page)
  const key = uniq('t447b')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 't447-search')
  // M10 夹具词汇（L18/L19 腿同款键值族——REST 面逗号配对语法；查询串里的
  // 分号是矩阵参数语法、不经内容面不生效〔实测 404〕）
  await api(page, 'PUT', `/api/storage/${key}/docs/guide.md?properties=build=77,env=prod,qa=passed,owner=team-a`)
  await openFilePropsTab(page, key, 'docs/guide.md')

  for (const k of ['build', 'env', 'qa', 'owner']) {
    await expect(page.locator(`[data-testid="node-props-row-${k}"]`)).toBeVisible()
  }

  const search = page.locator('[data-testid="node-props-search"]')
  await expect(search).toBeVisible()

  // 键子串：buil → 仅 build 行
  await search.fill('buil')
  await expect(page.locator('[data-testid="node-props-row-build"]')).toBeVisible()
  for (const k of ['env', 'qa', 'owner']) {
    await expect(page.locator(`[data-testid="node-props-row-${k}"]`)).toHaveCount(0)
  }

  // 值子串：team-a → 仅 owner 行（值命中）
  await search.fill('team-a')
  await expect(page.locator('[data-testid="node-props-row-owner"]')).toBeVisible()
  for (const k of ['build', 'env', 'qa']) {
    await expect(page.locator(`[data-testid="node-props-row-${k}"]`)).toHaveCount(0)
  }

  // 无匹配：提示块（表隐藏）
  await search.fill('no-such-token')
  await expect(page.locator('[data-testid="node-props-search-empty"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-props-table"]')).toHaveCount(0)

  // 清空恢复全量（清除钮）
  await page.getByRole('button', { name: '清除属性搜索' }).click()
  for (const k of ['build', 'env', 'qa', 'owner']) {
    await expect(page.locator(`[data-testid="node-props-row-${k}"]`)).toBeVisible()
  }
})

// ---------------------------------------------------------------------------
// ③ 下载形态（B-2.12/Q9）：单 24px 图标钮 + 伴随菜单 + 计数联动
// ---------------------------------------------------------------------------

test('download: single 24px icon (direct download), verify capability + checksums in the companion menu, count linkage', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t447c')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 't447-dl-payload')
  await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()

  // 单 24px 图标钮（B-2.12）：两带文字按钮收敛的反断言
  const icon = page.locator('[data-testid="node-download"]')
  await expect(icon).toBeVisible()
  const box = await icon.boundingBox()
  expect(box, 'icon button is 24px').toMatchObject({ width: 24, height: 24 })
  const actionsText = await page.locator('.node-detail-actions').innerText()
  expect(actionsText).not.toContain('下载并校验')
  expect(actionsText).not.toContain('直接下载')

  // General 页不再平铺校验块（Q9 终裁「收进伴随形态」）
  await expect(page.locator('[data-testid="node-detail"]')).not.toContainText('sha256')
  await expect(page.locator('[data-testid="node-detail"]')).not.toContainText('mimeType')

  // 伴随菜单：校验动作 + checksums（截断呈现）+ mimeType 全在菜单内
  await page.click('[data-testid="node-download-menu"]')
  const panel = page.locator('[data-testid="node-download-panel"]')
  await expect(panel).toBeVisible()
  await expect(page.locator('[data-testid="node-download-menu-verify"]')).toBeVisible()
  const sums = page.locator('[data-testid="node-download-checksums"]')
  await expect(sums).toContainText('sha256')
  await expect(sums).toContainText('mimeType')
  // 拷贝不截断（§7.3）：sha256 完整值进剪贴板
  const item = (await api(page, 'GET', `/api/storage/${key}/docs/guide.md`)).json as {
    checksums: { sha256: string }
  }
  await expectCopied(page, sums.locator('.copy-btn').first(), item.checksums.sha256)

  // 直接下载（图标）走内容面 GET——计数单源 +1（探针面计数继承下只断言增长）。
  // 先 Esc 收起菜单（Popover 背景层拦截图标点击）
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="node-download-panel"]')).toBeHidden()
  const before = (await statsFace(page, key, 'docs/guide.md')).downloadCount
  const dl = page.waitForEvent('download', { timeout: 15_000 })
  await icon.click()
  await dl
  await expect
    .poll(async () => (await statsFace(page, key, 'docs/guide.md')).downloadCount, { timeout: 10_000 })
    .toBeGreaterThan(before)

  // 校验能力（菜单内）：verify 完成 → 结果块 ✓；计数联动——UI 计数随增。
  // 口径：DOM 读数是挂载时刻的快照（不随面值实时走），联动 = verify 完成
  // 触发 refreshKey 重读后 DOM 计数严格大于动作前快照（探针面计数继承下
  // 不比对面值绝对数）
  const uiBefore = parseInt(await page.locator('[data-testid="node-downloads"]').innerText(), 10)
  expect(Number.isFinite(uiBefore)).toBe(true)
  await page.click('[data-testid="node-download-menu"]')
  await page.click('[data-testid="node-download-menu-verify"]')
  await expect(page.locator('[data-testid="node-download-verify"]')).toContainText(
    '✓ 下载落盘 sha256 与服务端一致',
    { timeout: 15_000 },
  )
  // 联动重读：verify 的内容面 GET 已计数——refreshKey 重读后 UI 呈现 > 动作前快照
  await expect
    .poll(async () => parseInt(await page.locator('[data-testid="node-downloads"]').innerText(), 10), {
      timeout: 10_000,
    })
    .toBeGreaterThan(uiBefore)
})

// ---------------------------------------------------------------------------
// ④ axe 双主题：属性页签全解剖 + 伴随菜单展开态
// ---------------------------------------------------------------------------

test('axe: properties tab and download companion menu clean in both themes', async ({ page }, testInfo: TestInfo) => {
  await login(page)
  const key = uniq('t447e')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 't447-axe')
  await api(page, 'PUT', `/api/storage/${key}/docs/guide.md?properties=build=77,env=prod,qa=passed`)

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)

    // 属性页签（常显表单 + 网格 + 搜索）
    await page.click('[data-testid="node-tab-props"]')
    await expect(page.locator('[data-testid="node-props-table"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="tree-page"]' })

    // 伴随菜单展开态（Popover 焦点圈进 + 校验动作 + checksums 区）。
    // T-459 复核注记：可见 ≠ 过场结束——axe 的 color-contrast 对半透明色按
    // 实际像素采样，Popover 进场动画未完时采样即假性炸裂（共租负载下 3/3
    // 复现；settle 后净绿）。全页扫描前统一 settle 400ms，同款处置见
    // t457-profile-help-about axe 腿。
    await page.click('[data-testid="node-tab-general"]')
    await page.click('[data-testid="node-download-menu"]')
    await expect(page.locator('[data-testid="node-download-checksums"]')).toBeVisible()
    await page.waitForTimeout(400)
    await expectA11yClean(page, testInfo)
    await page.keyboard.press('Escape')
    await expect(page.locator('[data-testid="node-download-panel"]')).toBeHidden()
  }
})
