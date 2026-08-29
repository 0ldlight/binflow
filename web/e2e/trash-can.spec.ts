import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { existsSync } from 'node:fs'
import { join } from 'node:path'

// T-352 fill (FR-106 FE 腿, L33): the trash-can admin page
// (/admin/governance/trash — 治理分组第六页). Full-mock probe off dist/
// (replication.spec.ts 的同款基座——真栈 harness 是 community 形态,
// trashcan 槽锁定, 浏览/恢复/清剿面不可达; unlocked 形态归本 mock 面,
// locked/readonly/anonymous 形态同时覆盖真栈可达的呈现面).
//
// 覆盖: ① 解锁态浏览(根列表 → 下钻 → 行选中五元组详情 + 拷贝锚);
// ② 恢复流(to 目的地输入 + 确认 → POST /api/trash/restore/{path}?to=);
// ③ 单条清除(danger 确认 → DELETE /api/trash/clean/{path} + 摘要);
// ④ 清空(输入 EMPTY 强确认 → POST /api/trash/empty + 摘要);
// ⑤ community 锁定态(槽行 → trash-locked, 浏览面不渲染不发请求);
// ⑥ can 未落库(根 404 → trash-unused); ⑦ readonly_admin 只读 + 反断言;
// ⑧ 非 admin 403 → L2 无权限卡; ⑨ 匿名直链重定向登录(NFR-S61 FE 腿).

const DIST = join(process.cwd(), 'dist')

test.beforeEach(() => {
  test.skip(!existsSync(join(DIST, 'index.html')), 'console not built — run `npm run build` first')
})

// sha256("hello")
const SHA_HELLO = '2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824'

/** whoami / addons / storage / trash 三面的 mock 形态 */
interface TrashMockOpts {
  admin?: boolean
  adminRole?: string
  /** trashcan 槽行(null = addons 403); 缺省 = 解锁 pro 行 */
  slot?: { enabled: boolean; minTier?: string } | null | '403'
  /** GET /api/storage/auto-trashcan(根): status + body; 缺省 = 正常 can */
  root?: { status: number; body?: unknown }
  /** POST/DELETE trash 三动词的应答(缺省 200 成功形) */
  onTrash?: (url: string, method: string) => { status: number; body: unknown }
}

/** 五元组 fixture(trash.time = 2026-08-28T10:00:00Z 的 epoch 毫秒) */
function tupleProps(over: Record<string, string[]> = {}): Record<string, string[]> {
  return {
    'trash.time': ['1787952557679'],
    'trash.deletedBy': ['admin'],
    'trash.originalRepository': ['vlibs'],
    'trash.originalRepositoryType': ['local'],
    'trash.originalPath': ['readme.md'],
    ...over,
  }
}

/** can 根 FolderInfo: children = 原 repo key 目录 */
const ROOT_FOLDER = {
  uri: 'http://localhost/binflow/api/storage/auto-trashcan',
  repo: 'auto-trashcan',
  path: '/',
  created: '2026-08-28T10:00:00Z',
  createdBy: 'admin',
  size: '12',
  children: [
    { uri: '/vlibs', folder: true },
  ],
}

/** vlibs 目录 FolderInfo + ?list&depth=1 文件元数据 */
const VLIBS_FOLDER = {
  uri: 'http://localhost/binflow/api/storage/auto-trashcan/vlibs',
  repo: 'auto-trashcan',
  path: '/vlibs/',
  created: '2026-08-28T10:00:00Z',
  createdBy: 'admin',
  size: '12',
  children: [
    { uri: '/com', folder: true },
    { uri: '/readme.md', folder: false },
  ],
}

const VLIBS_LIST = {
  files: [
    { uri: '/readme.md', folder: false, size: 12, lastModified: '2026-08-28T10:00:00Z', sha2: SHA_HELLO },
  ],
}

async function installMocks(page: Page, opts: TrashMockOpts = {}): Promise<() => number> {
  let storageCalls = 0

  // 兜底先注册(后注册者优先): 未覆盖的 API 一律 404, 不漏到真实网络
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
        username: opts.adminRole === 'readonly_admin' ? 'ro-admin' : 'admin',
        admin: opts.admin ?? true,
        ...(opts.adminRole ? { adminRole: opts.adminRole } : {}),
      }),
    }),
  )
  await page.route('**/binflow/api/system/version', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ version: '6.0.0-t352', revision: 'e2e', product: 'BinFlow' }),
    }),
  )
  // addons: 缺省 = 解锁 trashcan 槽(真栈 community 形态的对偶面)
  await page.route('**/binflow/api/v1/addons', (route) => {
    if (opts.slot === '403') {
      return route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({ errors: [{ message: 'forbidden' }] }),
      })
    }
    const row = opts.slot ?? { enabled: true, minTier: 'pro' }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: 'trashcan',
          kind: 'feature',
          minTier: row.minTier ?? 'pro',
          enabled: row.enabled,
          reason: row.enabled ? '' : `requires ${row.minTier ?? 'pro'}`,
          displayName: 'Trash Can',
          description: 'Soft-delete capture and restore.',
        },
      ]),
    })
  })
  // trash 三动词
  await page.route('**/binflow/api/trash/**', (route) => {
    const url = new URL(route.request().url())
    const rel = url.pathname.replace('/binflow/api/trash/', '')
    const reply =
      opts.onTrash?.(`${route.request().method()} trash/${rel}${url.search}`, route.request().method()) ?? {
        status: 200,
        body:
          route.request().method() === 'POST' && rel.startsWith('restore')
            ? { messages: [{ level: 'INFO', message: 'moving auto-trashcan/vlibs/readme.md completed successfully, 1 artifacts, 0 folders' }] }
            : { removed: 2, files: 1, folders: 1, bytes: 12 },
      }
    return route.fulfill({ status: reply.status, contentType: 'application/json', body: JSON.stringify(reply.body) })
  })
  // 存储面: 根/子目录 FolderInfo + ?list&depth=1 + ?properties(计数器记浏览请求)
  await page.route('**/binflow/api/storage/auto-trashcan**', (route) => {
    const url = new URL(route.request().url())
    const q = url.search
    storageCalls++
    if (q.includes('properties')) {
      const path = url.pathname.replace('/binflow/api/storage/auto-trashcan', '')
      if (path === '' || path === '/vlibs') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ properties: tupleProps({ 'trash.originalPath': ['vlibs/'] }) }),
        })
      }
      if (path === '/vlibs/readme.md') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ properties: { ...tupleProps(), license: ['apache-2.0'] } }),
        })
      }
      if (path === '/vlibs/com') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ properties: tupleProps({ 'trash.originalPath': ['com/'] }) }),
        })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ properties: {} }) })
    }
    if (url.pathname.endsWith('/auto-trashcan')) {
      const root = opts.root ?? { status: 200, body: ROOT_FOLDER }
      return route.fulfill({ status: root.status, contentType: 'application/json', body: JSON.stringify(root.body ?? { errors: [{ message: 'Not Found' }] }) })
    }
    if (url.pathname.endsWith('/vlibs')) {
      if (q.includes('list')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(VLIBS_LIST) })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(VLIBS_FOLDER) })
    }
    return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: 'Not Found' }] }) })
  })

  // SPA 外壳(dist 兜底; /binflow/assets/** 指纹资源)
  await page.route('**/binflow/assets/**', (route) => {
    const name = new URL(route.request().url()).pathname.replace('/binflow/assets/', '')
    return route.fulfill({ path: join(DIST, 'assets', name) })
  })
  await page.route('**/binflow/ui/**', (route) => route.fulfill({ path: join(DIST, 'index.html') }))

  return () => storageCalls
}

// ---------------------------------------------------------------------------
// ① 解锁态浏览: 根列表 → 下钻 → 行选中五元组详情 + 拷贝锚(P2)
// ---------------------------------------------------------------------------

test('unlocked: root list, drill-down, row detail carries the trash five-tuple', async ({ page }) => {
  const storageCalls = await installMocks(page)
  await page.goto('/binflow/ui/admin/governance/trash')

  await expect(page.locator('[data-testid="trash-page"]')).toBeVisible()
  // 根列表: 原 repo key 目录一行(排序目录在前)
  await expect(page.locator('[data-testid="trash-list"]')).toBeVisible()
  await expect(page.locator('[data-testid="trash-row-vlibs"]')).toBeVisible()
  await expect(page.locator('[data-testid="trash-row-vlibs"]')).toContainText('目录')
  // 拷贝锚(P2: mono 标识可复制——CopyButton 的「复制 」前缀 + 对象描述)
  await expect(page.locator('button[aria-label="复制 路径 vlibs"]')).toBeVisible()

  // 下钻: 目录名点击进 vlibs(面包屑段级)
  await page.locator('[data-testid="trash-row-vlibs"] button[title="进入目录"]').click()
  await expect(page.locator('[data-testid="trash-breadcrumb"]')).toContainText('vlibs')
  // 行锚 = can 相对全路径(含目录前缀)
  await expect(page.locator('[data-testid="trash-row-vlibs/com"]')).toBeVisible()
  await expect(page.locator('[data-testid="trash-row-vlibs/readme.md"]')).toContainText('文件')
  // ?list 合并的文件元数据(大小列非 —)
  await expect(page.locator('[data-testid="trash-row-vlibs/readme.md"]')).toContainText('12 B')

  // 行点击选中 → 详情面板: 五元组逐行(AC1 断言面)
  await page.locator('[data-testid="trash-row-vlibs/readme.md"] td').nth(2).click()
  const detail = page.locator('[data-testid="trash-detail"]')
  await expect(detail).toBeVisible()
  await expect(detail).toContainText('trash.time')
  await expect(detail).toContainText('1787952557679')
  await expect(detail).toContainText('2026-08-') // epoch ms 的本地化呈现
  await expect(detail).toContainText('trash.deletedBy')
  await expect(detail).toContainText('admin')
  await expect(detail).toContainText('trash.originalRepository')
  await expect(detail).toContainText('vlibs')
  await expect(detail).toContainText('trash.originalRepositoryType')
  await expect(detail).toContainText('local')
  await expect(detail).toContainText('trash.originalPath')
  await expect(detail).toContainText('readme.md')
  // 五元组之外的随行原属性如实呈现(license 行)
  await expect(detail).toContainText('apache-2.0')
  // 原路径/原仓/sha256 拷贝锚
  await expect(detail.locator('button[aria-label="复制 原路径"]')).toBeVisible()
  await expect(detail.locator('button[aria-label="复制 原仓库 vlibs"]')).toBeVisible()
  await expect(detail.locator('button[aria-label="复制 sha256"]')).toBeVisible()
  // 面包屑回根
  await page.locator('[data-testid="trash-breadcrumb"] button', { hasText: 'auto-trashcan' }).click()
  await expect(page.locator('[data-testid="trash-row-vlibs"]')).toBeVisible()
  // 刷新钮: 重发当前层浏览请求(计数器自证)
  const before = storageCalls()
  await page.locator('[data-testid="trash-refresh"]').click()
  await expect(page.locator('[data-testid="trash-row-vlibs"]')).toBeVisible()
  expect(storageCalls()).toBeGreaterThan(before)
})

// ---------------------------------------------------------------------------
// ② 恢复流: to 目的地输入 + 确认 → POST /api/trash/restore/{path}?to=
// ---------------------------------------------------------------------------

test('restore: confirm dialog with to override fires the REST verb', async ({ page }) => {
  const seen: string[] = []
  await installMocks(page, {
    onTrash: (label) => {
      seen.push(label)
      return { status: 200, body: { messages: [{ level: 'INFO', message: 'moving … completed successfully' }] } }
    },
  })
  await page.goto('/binflow/ui/admin/governance/trash')
  await page.locator('[data-testid="trash-row-vlibs"] button[title="进入目录"]').click()

  await page.locator('[data-testid="trash-restore-vlibs/readme.md"]').click()
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  const to = page.locator('[data-testid="trash-restore-to"]')
  await expect(to).toBeVisible()
  await to.fill('/vlibs/restored.md')
  await page.locator('[data-testid="confirm-accept"]').click()

  // wire: 路径段原样、to 走 query 编码
  expect(seen).toContain('POST trash/restore/vlibs/readme.md?to=%2Fvlibs%2Frestored.md')
  // 回执 messages 原文 toast(不翻译)
  await expect(page.locator('[data-testid="toast"]')).toContainText('completed successfully')
})

// ---------------------------------------------------------------------------
// ③ 单条永久清除: danger 确认 → DELETE /api/trash/clean/{path} + 摘要
// ---------------------------------------------------------------------------

test('clean: danger confirm fires DELETE and renders the purge summary', async ({ page }) => {
  const seen: string[] = []
  await installMocks(page, {
    onTrash: (label) => {
      seen.push(label)
      return { status: 200, body: { removed: 3, files: 2, folders: 1, bytes: 4096 } }
    },
  })
  await page.goto('/binflow/ui/admin/governance/trash')
  await page.locator('[data-testid="trash-row-vlibs"] button[title="进入目录"]').click()

  await page.locator('[data-testid="trash-clean-vlibs/com"]').click()
  await expect(page.locator('[data-testid="confirm-dialog"]')).toContainText('永久删除')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toContainText('整个子树')
  await page.locator('[data-testid="confirm-accept"]').click()

  expect(seen).toContain('DELETE trash/clean/vlibs/com')
  const summary = page.locator('[data-testid="trash-summary"]')
  await expect(summary).toBeVisible()
  await expect(summary).toContainText('3 项')
  await expect(summary).toContainText('2 / 1')
  await expect(summary).toContainText('4.0 KB')
  await expect(summary).toContainText('常态 GC')
})

// ---------------------------------------------------------------------------
// ④ 清空整罐: EMPTY 强确认 → POST /api/trash/empty
// ---------------------------------------------------------------------------

test('empty: typed EMPTY gate unlocks the purge; wrong typing keeps it disabled', async ({ page }) => {
  const seen: string[] = []
  await installMocks(page, {
    onTrash: (label) => {
      seen.push(label)
      return { status: 200, body: { removed: 5, files: 3, folders: 2, bytes: 1024 } }
    },
  })
  await page.goto('/binflow/ui/admin/governance/trash')

  await page.locator('[data-testid="trash-empty"]').click()
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  // 强确认门: 未键入时确认钮禁用
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeDisabled()
  await page.locator('[data-testid="trash-empty-confirm"]').fill('EMPTY')
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeEnabled()
  await page.locator('[data-testid="confirm-accept"]').click()

  expect(seen).toContain('POST trash/empty')
  await expect(page.locator('[data-testid="trash-summary"]')).toContainText('5 项')
})

// ---------------------------------------------------------------------------
// ⑤ community 锁定态: 槽行 → trash-locked; 浏览面不渲染也不发请求
// ---------------------------------------------------------------------------

test('locked slot (community): gate card renders, no browse requests', async ({ page }) => {
  const storageCalls = await installMocks(page, { slot: { enabled: false, minTier: 'pro' } })
  await page.goto('/binflow/ui/admin/governance/trash')

  const locked = page.locator('[data-testid="trash-locked"]')
  await expect(locked).toBeVisible()
  await expect(locked).toContainText('需要 pro')
  await expect(locked).toContainText('X-Binflow-License-Required')
  // 浏览面/动作面整块不渲染(零请求——锁定态不发存储面探测)
  await expect(page.locator('[data-testid="trash-list"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="trash-empty"]')).toHaveCount(0)
  await expect(storageCalls()).toBe(0)
})

// ---------------------------------------------------------------------------
// ⑥ can 未落库: 根 404 → 锚定空态(懒落库语义, 与「空罐」两分)
// ---------------------------------------------------------------------------

test('unused can (root 404) degrades to the anchored empty state', async ({ page }) => {
  await installMocks(page, { root: { status: 404 } })
  await page.goto('/binflow/ui/admin/governance/trash')

  await expect(page.locator('[data-testid="trash-unused"]')).toBeVisible()
  await expect(page.locator('[data-testid="trash-unused"]')).toContainText('尚未')
  await expect(page.locator('[data-testid="trash-list"]')).toHaveCount(0)
  // 空罐态清空钮禁用(已知空, 不邀误操作)
  await expect(page.locator('[data-testid="trash-empty"]')).toBeDisabled()
})

// ---------------------------------------------------------------------------
// ⑦ readonly_admin: 浏览只读 + 注记; 写动作全反断言(L4)
// ---------------------------------------------------------------------------

test('readonly_admin: browse-only with note; write actions counter-asserted', async ({ page }) => {
  await installMocks(page, { adminRole: 'readonly_admin' })
  await page.goto('/binflow/ui/admin/governance/trash')

  await expect(page.locator('[data-testid="trash-list"]')).toBeVisible()
  await expect(page.locator('[data-testid="trash-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="trash-restore-vlibs"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="trash-clean-vlibs"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="trash-empty"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑧ 非 admin: 浏览面 403 → L2 无权限卡(缺省 empty-state 锚)
// ---------------------------------------------------------------------------

test('non-admin (403) shows the permission empty state', async ({ page }) => {
  await installMocks(page, { admin: false, slot: '403', root: { status: 403 } })
  await page.goto('/binflow/ui/admin/governance/trash')

  await expect(page.locator('[data-testid="empty-state"]')).toContainText('无权限查看回收站')
  await expect(page.locator('[data-testid="trash-list"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="error-card"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑨ NFR-S61 FE 腿: 匿名直链进不了管理面——会话守卫先于一切(重定向 login)
// ---------------------------------------------------------------------------

test('anonymous deep link redirects to login with return (NFR-S61 FE side)', async ({ page }) => {
  await installMocks(page)
  // 后注册优先: 会话面改答 401(匿名)——管理壳守卫先于页面一切数据面
  await page.route('**/binflow/api/v1/session', (route) =>
    route.fulfill({ status: 401, contentType: 'application/json', body: '{}' }),
  )
  await page.goto('/binflow/ui/admin/governance/trash')

  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await expect(page).toHaveURL(/\/login\?return=/)
  await expect(page.locator('[data-testid="trash-page"]')).toHaveCount(0)
})
