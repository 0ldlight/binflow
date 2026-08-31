import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { existsSync } from 'node:fs'
import { join } from 'node:path'

// T-372 fill (M13 FR-122.1, L21 段): the resident trash-can entry node at
// the tail of the cross-repo artifact tree (/artifacts) — console-m8 §4.3
// 「Trash Can 常驻节点不建」推翻条款的兑现面. Full-mock probe off dist/
// (trash-can.spec.ts 的同款基座): the click-through target is the M12 admin
// page whose unlocked shape only exists behind mocks here; the REAL-stack
// visibility/click leg lives in m8/artifacts-tree.spec.ts (T-372 增腿).
//
// 覆盖: ① admin 形态——节点常驻树尾（DOM 序 = 全部 tree-repo 行之后）、
// 点击深链跳转 /admin/governance/trash、返回后树与节点复位; ② 键盘——
// 末位仓库行 ↓ 落到节点（树行序集成）、Enter 激活跳转; ③ 常驻语义——
// 「过滤仓库」收空仓库行后节点不消失（常驻 ≠ 已加载仓库集成员）;
// ④ readonly_admin 臂——节点可见 → 页面只读注记 + 写动作反断言;
// ⑤ 普通 user——节点不渲染（管理壳同门可见性）; ⑥ axe 双主题
// serious/critical = 0.

const DIST = join(process.cwd(), 'dist')

test.beforeEach(() => {
  test.skip(!existsSync(join(DIST, 'index.html')), 'console not built — run `npm run build` first')
})

const TREE = '/binflow/ui/artifacts'
const TRASH = '/binflow/ui/admin/governance/trash'

/** 会话形态：admin / readonly_admin / 普通 user（admin=false 无 adminRole） */
type Role = 'admin' | 'readonly_admin' | 'user'

/** /api/repositories 回显（RepoListItem 逐字字段） */
const REPOS = [
  { key: 'generic-local', description: '', type: 'local', packageType: 'generic', url: '' },
  { key: 'maven-remote', description: '', type: 'remote', packageType: 'maven', url: 'https://repo.example.com/maven' },
]

/** can 根 FolderInfo：children = 原 repo key 目录（trash-can.spec 同款形） */
const ROOT_FOLDER = {
  uri: 'http://localhost/binflow/api/storage/auto-trashcan',
  repo: 'auto-trashcan',
  path: '/',
  created: '2026-08-28T10:00:00Z',
  createdBy: 'admin',
  size: '12',
  children: [{ uri: '/vlibs', folder: true }],
}

/**
 * 全 mock 基座：会话/version + 仓库清单（树面）+ addons 槽行与存储面
 * （点击落地的 M12 页面）。reposStatus 单独可覆写（普通 user 403 臂）。
 */
async function installMocks(page: Page, opts: { role?: Role; reposStatus?: number } = {}) {
  const role = opts.role ?? 'admin'
  await page.route('**/binflow/api/**', (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ errors: [{ message: 'unmocked endpoint' }] }),
    }),
  )
  await page.route('**/binflow/api/v1/session', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        username: role === 'admin' ? 'admin' : role === 'readonly_admin' ? 'ro-admin' : 'alice',
        admin: role !== 'user',
        ...(role === 'readonly_admin' ? { adminRole: 'readonly_admin' } : {}),
      }),
    }),
  )
  await page.route('**/binflow/api/system/version', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ version: '6.0.0-t372', revision: 'e2e', product: 'BinFlow' }),
    }),
  )
  await page.route('**/binflow/api/repositories', (route) =>
    route.fulfill({
      status: opts.reposStatus ?? 200,
      contentType: 'application/json',
      body: JSON.stringify(
        (opts.reposStatus ?? 200) === 200
          ? REPOS
          : { errors: [{ message: 'forbidden' }] },
      ),
    }),
  )
  // addons：解锁 trashcan 槽（点击落地页的浏览面可达）
  await page.route('**/binflow/api/v1/addons', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: 'trashcan',
          kind: 'feature',
          minTier: 'pro',
          enabled: true,
          reason: '',
          displayName: 'Trash Can',
          description: 'Soft-delete capture and restore.',
        },
      ]),
    }),
  )
  // 存储面：can 根（readonly/user 臂的浏览请求也走这里）
  await page.route('**/binflow/api/storage/auto-trashcan**', (route) => {
    const url = new URL(route.request().url())
    if (url.search.includes('properties')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ properties: {} }),
      })
    }
    if (url.pathname.endsWith('/auto-trashcan')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(ROOT_FOLDER) })
    }
    return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: 'Not Found' }] }) })
  })
  // SPA 外壳（dist 兜底； /binflow/assets/** 指纹资源）
  await page.route('**/binflow/assets/**', (route) => {
    const name = new URL(route.request().url()).pathname.replace('/binflow/assets/', '')
    return route.fulfill({ path: join(DIST, 'assets', name) })
  })
  await page.route('**/binflow/ui/**', (route) => route.fulfill({ path: join(DIST, 'index.html') }))
}

/** axe：serious（含 critical）= 0（t366 同款——扫描前挪开鼠标防悬停对比度假阳性） */
async function expectAxeClean(page: Page, label: string) {
  await page.mouse.move(1, 1)
  await page.waitForTimeout(100)
  const results = await new AxeBuilder({ page }).analyze()
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')
  const detail = serious.map((v) => `${v.id}(${v.impact}): ${v.nodes.length} nodes`).join(', ')
  expect(serious, `${label} — ${detail}`).toEqual([])
}

/** 树尾 DOM 序断言素材：页面里全部树行的 testid 序列 */
async function treeRowTestids(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLElement>('[data-tree-row]')).map((el) => el.getAttribute('data-testid') ?? ''),
  )
}

// ---------------------------------------------------------------------------
// ① admin：节点常驻树尾 + 点击深链跳转 M12 页面 + 返回复位
// ---------------------------------------------------------------------------

test('trash node: resident at tree tail; click deep-links to the M12 page; back restores the tree', async ({ page }) => {
  await installMocks(page)
  await page.goto(TREE)

  const node = page.locator('[data-testid="tree-trash-node"]')
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  await expect(node).toBeVisible()
  await expect(node).toContainText('回收站')
  await expect(node).toHaveAttribute('role', 'treeitem')
  // 常驻树尾：DOM 序在全部 tree-repo 行之后（reverse §3.2 末尾常驻形态）
  const rows = await treeRowTestids(page)
  expect(rows.filter((r) => r.startsWith('tree-repo-')).length).toBeGreaterThanOrEqual(2)
  expect(rows[rows.length - 1]).toBe('tree-trash-node')

  // 点击 = 深链跳转（最小面：入口跳转，页身沿用 M12 回收站页）
  await node.click()
  await expect(page).toHaveURL(new RegExp(`${TRASH}$`))
  await expect(page.locator('[data-testid="trash-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="trash-list"]')).toBeVisible()

  // 返回：树与节点复位（深链可回环）
  await page.goBack()
  await expect(page).toHaveURL(new RegExp(`${TREE}$`))
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
  await expect(node).toBeVisible()
})

// ---------------------------------------------------------------------------
// ② 键盘：末位仓库行 ↓ 落到节点（树行序集成）；Enter 激活跳转
// ---------------------------------------------------------------------------

test('trash node: keyboard — ArrowDown from the last repo row lands on it; Enter activates', async ({ page }) => {
  await installMocks(page)
  await page.goto(TREE)

  // 仓库清单到位后再取行序（树行渲染是异步的——先等首枚仓库行）
  await expect(page.locator('[data-testid="tree-repo-generic-local"]')).toBeVisible()
  const rows = await treeRowTestids(page)
  const lastRepo = rows.filter((r) => r.startsWith('tree-repo-')).pop()
  expect(lastRepo, '预置至少一枚仓库行').toBeTruthy()

  await page.focus(`[data-testid="${lastRepo}"]`)
  await page.keyboard.press('ArrowDown')
  await expect(page.locator('[data-testid="tree-trash-node"]')).toBeFocused()
  // Enter 激活 = 同点击的跳转语义（§3.4 树键盘清单）
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`${TRASH}$`))
  await expect(page.locator('[data-testid="trash-page"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ③ 常驻语义：过滤仓库收空仓库行后节点不消失
// ---------------------------------------------------------------------------

test('trash node: stays resident while the repo filter empties the repo rows', async ({ page }) => {
  await installMocks(page)
  await page.goto(TREE)

  await expect(page.locator('[data-testid="tree-repo-generic-local"]')).toBeVisible()
  await page.fill('[data-testid="tree-repo-filter"]', 'no-such-repo')
  await expect(page.locator('[data-testid="tree-repo-generic-local"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="tree-page"]')).toContainText('没有匹配')
  // 常驻节点不在过滤域内（它不是已加载仓库集成员）
  await expect(page.locator('[data-testid="tree-trash-node"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ④ readonly_admin 臂：节点可见 → 页面只读注记 + 写动作反断言
// ---------------------------------------------------------------------------

test('trash node: readonly_admin sees it; landing page is browse-only', async ({ page }) => {
  await installMocks(page, { role: 'readonly_admin' })
  await page.goto(TREE)

  const node = page.locator('[data-testid="tree-trash-node"]')
  await expect(node).toBeVisible()
  await node.click()
  await expect(page).toHaveURL(new RegExp(`${TRASH}$`))
  await expect(page.locator('[data-testid="trash-readonly-note"]')).toBeVisible()
  // 写动作反断言（L4 姿态：readonly_admin 不渲染管理面写入口）
  await expect(page.locator('[data-testid="trash-empty"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="trash-restore-vlibs"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="trash-clean-vlibs"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑤ 普通 user：管理壳同门——节点不渲染（仓库清单 403 形态）
// ---------------------------------------------------------------------------

test('trash node: plain user gets no admin surface — node counter-asserted', async ({ page }) => {
  await installMocks(page, { role: 'user', reposStatus: 403 })
  await page.goto(TREE)

  await expect(page.locator('[data-testid="tree-root-denied"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-trash-node"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑥ axe 双主题：树页（含常驻节点）serious/critical = 0
// ---------------------------------------------------------------------------

test('trash node: axe both themes on the tree page — serious/critical = 0', async ({ page }) => {
  await installMocks(page)
  for (const theme of ['light', 'dark'] as const) {
    await page.goto(TREE)
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto(TREE)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="tree-trash-node"]')).toBeVisible()
    await expectAxeClean(page, `${TREE} (${theme})`)
  }
})
