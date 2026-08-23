import { expect, test } from '@playwright/test'

import { expectA11yClean } from './support/a11y'
import { expectCopied, grantClipboard } from './support/clipboard'
import { loginAs } from './support/roles'
import { m8Client, seedAll, seedRepos, sessionApi } from './support/seed'
import { measureTreeExpand, recordTiming } from './support/timing'

// T-236 跨仓制品树（FR-72 / console-m8 §6.3）：仓库顶层节点 + 懒展开 +
// URL 即状态 + 深链自动展开 + 右键菜单（C3）+ 过滤仓库 + readonly/user
// 收敛 + 性能计时（record-only，ADR-0029 决策 3）。
//
// 断言口径 = e2e/m8/README §2：锚不随路由改名（tree-page 族原样），路径
// 断言走新路由；对账腿用 sessionApi / m8Client（UI 说的话让 API 复核）。

const PERM_REPO = 'm8-perf-local'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

// ---- 顶层：仓库节点 + 过滤仓库（C2）----------------------------------------

test('cross-repo landing: repos are top-level tree nodes; filter narrows loaded set + clear restores', async ({
  page,
}) => {
  test.setTimeout(300_000)
  const key = uniq('m8tree')
  await seedRepos(m8Client(), [{ key }, { key: `${key}-b` }])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/artifacts')
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="browser-tree"]')).toBeVisible()

  // 仓库为顶层节点（rclass/packageType 图标区分，锚 = tree-repo-<key>）
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="tree-repo-${key}-b"]`)).toBeVisible()
  // 选中即时联动右面板（仓库形态详情）
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${key}$`))
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"]')).toContainText(key)

  // 过滤仓库（前端过滤已加载集）+ 清除复位
  await page.fill('[data-testid="tree-repo-filter"]', `${key}-b`)
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toHaveCount(0)
  await expect(page.locator(`[data-testid="tree-repo-${key}-b"]`)).toBeVisible()
  await page.click('[data-testid="tree-repo-filter-clear"]')
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toBeVisible()
})

// ---- 种子树（10k 节点压测对象）+ 懒展开 + 深链 ------------------------------

test('seed-m8 tree: deep link auto-expands ancestors, selects the node and scrolls it into view', async ({
  page,
}) => {
  test.setTimeout(300_000)
  await seedAll(m8Client(), { repoKey: PERM_REPO })

  await loginAs(page, 'admin')
  // 深链（URL 即状态）：祖先链自动展开 + 目标选中
  await page.goto(`/binflow/ui/artifacts/${PERM_REPO}/perf/w03`)
  const perfNode = page.locator(`[data-testid="tree-node-perf"]`)
  await expect(perfNode).toBeVisible()
  await expect(perfNode).toHaveAttribute('aria-expanded', 'true')
  const w03 = page.locator('[data-testid="tree-node-perf/w03"]')
  await expect(w03).toBeVisible()
  await expect(w03).toHaveClass(/selected/)
  // 滚动定位：目标节点在可视区内（树 pane 的滚动容器语义）
  const inView = await w03.evaluate((el) => {
    const pane = el.closest('.tree-pane')
    if (!pane) return false
    const pr = pane.getBoundingClientRect()
    const r = el.getBoundingClientRect()
    return r.top >= pr.top && r.bottom <= pr.bottom
  })
  expect(inView).toBe(true)

  // 当前层 children 表（懒加载一层的数据面）+ 选中文件进 URL（?focus=）
  await expect(page.locator('[data-testid="tree-list"] tbody tr').first()).toBeVisible()
  await page.click('[data-testid="tree-row-f000.txt"]')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${PERM_REPO}/perf/w03\\?focus=f000\\.txt$`))
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('perf/w03/f000.txt')

  // 目录导航改写 URL（无 focus 残留）
  await page.click('[data-testid="tree-breadcrumb"] button.crumb:first-child')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${PERM_REPO}$`))
})

// ---- 右键菜单（C3：文件/目录/仓库三形态 + 键盘 + 删除对账）------------------

test('context menu: file/folder/repo forms, Shift+F10 reachable, delete reconciles via API', async ({ page }) => {
  const key = uniq('m8ctx')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/docs/guide.md`, { raw: true, body: 'ctx-menu-probe\n' })
  await client.request('PUT', `/binflow/${key}/docs/legacy.bin`, { raw: true, body: 'stale\n' })

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${key}/docs`)

  // 文件形态：复制路径 / 下载 / 删除
  const row = page.locator('[data-testid="tree-row-guide.md"]')
  await expect(row).toBeVisible()
  await row.click({ button: 'right' })
  await expect(page.locator('[data-testid="tree-context-menu"]')).toBeVisible()
  for (const item of ['copy-path', 'download', 'delete']) {
    await expect(page.locator(`[data-testid="tree-context-${item}"]`)).toBeVisible()
  }
  // Move/Copy 不建（零影子入口）
  await expect(page.locator('[data-testid="tree-context-menu"] button')).toHaveCount(3)
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="tree-context-menu"]')).toHaveCount(0)

  // 目录形态（树节点）：复制路径 / 删除 / 刷新 —— 键盘 Shift+F10 打开
  const dirNode = page.locator('[data-testid="tree-node-docs"]')
  await expect(dirNode).toBeVisible()
  await dirNode.focus()
  await page.keyboard.press('Shift+F10')
  await expect(page.locator('[data-testid="tree-context-menu"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-context-copy-path"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-context-refresh"]')).toBeVisible()
  await page.keyboard.press('Escape')

  // 仓库形态：复制仓库路径 / 刷新 / 在仓库管理中打开
  await page.locator(`[data-testid="tree-repo-${key}"]`).click({ button: 'right' })
  await expect(page.locator('[data-testid="tree-context-copy-repo-path"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-context-open-admin"]')).toBeVisible()
  await page.keyboard.press('Escape')

  // 删除流（菜单入口 → 危险确认 → 行淡出 + API 对账 404）
  await page.locator('[data-testid="tree-row-legacy.bin"]').click({ button: 'right' })
  await page.click('[data-testid="tree-context-delete"]')
  await expect(page.locator('[data-testid="confirm-dialog"], .modal')).toBeVisible()
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('.toast').filter({ hasText: '已删除' })).toContainText('docs/legacy.bin')
  await expect(page.locator('[data-testid="tree-row-legacy.bin"]')).toHaveCount(0)
  // 对账（§2.6）：UI 说删了，让同源 API 复核（sessionApi 不抛 4xx）
  const gone = await sessionApi(page, 'GET', `/api/storage/${key}/docs/legacy.bin`)
  expect(gone.status).toBe(404)
})

test('context menu copy-path lands the FULL value on the clipboard', async ({ page }) => {
  test.skip(!(await grantClipboard(page)), 'clipboard legs are chromium-only (the supported matrix)')
  const key = uniq('m8copy')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/bin/app.tar`, { raw: true, body: 'x\n' })

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${key}/bin`)
  await page.locator('[data-testid="tree-row-app.tar"]').click({ button: 'right' })
  await expectCopied(page, page.locator('[data-testid="tree-context-copy-path"]'), `${key}/bin/app.tar`)
})

// ---- 角色收敛（T-218 债：写入口按角色禁用）----------------------------------

test('readonly_admin: full repo inventory + write entries disabled + readonly note', async ({ page }) => {
  const key = uniq('m8ro')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/d/keep.bin`, { raw: true, body: 'ro\n' })

  await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/artifacts')
  // CapRepoRead：readonly_admin 见全量清单（router.go 434 注释口径）
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toBeVisible()
  await expect(page.locator('[data-testid="tree-readonly-note"]')).toBeVisible()

  await page.goto(`/binflow/ui/artifacts/${key}/d`)
  await expect(page.locator('[data-testid="tree-row-keep.bin"]')).toBeVisible()
  // 写入口禁用（T-218 债收口：上传/建目录/删除）
  await expect(page.locator('[data-testid="tree-upload"]')).toBeDisabled()
  await expect(page.locator('[data-testid="tree-mkdir"]')).toBeDisabled()
  await expect(page.locator('[data-testid="delete-node-button"]').first()).toBeDisabled()
  // 右键菜单删除项同样禁用
  await page.locator('[data-testid="tree-row-keep.bin"]').click({ button: 'right' })
  await expect(page.locator('[data-testid="tree-context-delete"]')).toBeDisabled()
  await page.keyboard.press('Escape')
})

test('plain user: repo inventory 403 -> L2 card; known-key deep link works via path ACL', async ({ page }) => {
  const key = uniq('m8usr')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/d/file.bin`, { raw: true, body: 'u\n' })
  // 独立命名 target：ensureReadGrant 的 m8-e2e-read 是 create-if-absent，
  // 已被 seed 占用（只盖 m8-perf-local）——本腿给仓库建自己的授权
  await client.request('POST', '/binflow/api/v1/permissions', {
    body: {
      name: `m8usr-${key}`,
      repos: [key],
      includePatterns: ['**'],
      excludePatterns: [],
      principals: { users: { 'm8-e2e-user': ['read'] }, groups: {} },
    },
  })

  await loginAs(page, 'user')
  await page.goto('/binflow/ui/artifacts')
  // 顶层仓库列表不可得（GET /api/repositories 403）→ L2 无权限卡 + 引导
  await expect(page.locator('[data-testid="tree-root-denied"]')).toBeVisible()
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toHaveCount(0)

  // 已知 repo key 的深链按路径 ACL 可达（合成顶层节点 + 子树渲染）
  await page.goto(`/binflow/ui/artifacts/${key}/d`)
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-file.bin"]')).toBeVisible()
  // repo 元数据 admin 面 403 → 降级提示（warn-box）
  await expect(page.locator('.tree-page .warn-box')).toContainText('管理员视图')
  // 普通用户写入口保留（服务端 403 行内呈现，W12d 语义不变）
  await expect(page.locator('[data-testid="tree-upload"]')).toBeEnabled()
})

// ---- axe 结构可达性（serious/critical = 0 门；§9）----------------------------

test('axe: artifacts browser + detail panel scan clean at serious/critical impact', async ({ page }, testInfo) => {
  const key = uniq('m8axe')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  await client.request('PUT', `/binflow/${key}/docs/guide.md`, { raw: true, body: 'axe\n' })

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${key}/docs`)
  await expect(page.locator('[data-testid="tree-row-guide.md"]')).toBeVisible()
  // 详情面板（目录形态）展开后再扫——Tab/树/表全在 DOM
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="tree-page"]' })
})

// ---- 性能计时（record-only；树层展开 = 懒加载一层的真实负载）------------------

test('timing: tree expand on a seeded level (lazy one-level load)', async ({ page }, testInfo) => {
  const key = uniq('m8exp')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  for (let w = 0; w < 6; w++) {
    for (let f = 0; f < 3; f++) {
      await client.request('PUT', `/binflow/${key}/perf/w${String(w).padStart(2, '0')}/f${f}.txt`, {
        raw: true,
        body: `x ${w} ${f}\n`,
      })
    }
  }

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${key}`)
  const root = page.locator('[data-testid="tree-node-perf"]')
  await expect(root).toBeVisible()
  const expand = await measureTreeExpand(page, root, `[data-testid^="tree-node-perf/"]`)
  await recordTiming(testInfo, 'tree-expand', expand)

  expect(expand.childRows).toBe(6)
  expect(expand.expandMs).toBeGreaterThan(0)
})
