import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'

// T-434（M16 批次① P0 首票，FR-142——树栈四项，断言反转①）：
//
//   ① 文件叶子进树（B-1.1）：目录展开 = 子目录行 + 文件叶子行
//      （tree-leaf-<path>）；仅含文件的目录不再「（空）」误导占位；文件
//      叶子点击 → 右侧 item view（文件 URL = 路径末段）；目录选中给直系
//      概要（子项〔Artifact Count〕目录 X · 文件 Y + Size〔直系文件合计〕）；
//      children 表操作列退役（行内 delete-node-button 只余详情面板承载）。
//   ② 选择 ≠ 展开（B-1.2）：仓库名单击 = 纯选中（aria-expanded 前后对照
//      不变）；twisty 独立展开/收起；深链祖先链自动展开维持。
//   ③ URL 模型（B-1.3）：活跃页签进 URL 路径段（/artifacts/[<TAB>/]<repo>/
//      <path>，省略 = general）；文件选择是路径末段（?focus= 退役）；旧
//      ?focus= 深链一次性 replace 折入；刷新/重开三态一致。
//   ④ 树头工具带（B-1.4）：包类型 facet 复选组 + Local/Remote/Virtual 组 +
//      Sort-by + Compacted/Non-Compacted 单选 + My Favorites（localStorage
//      持久——reload 保持）；rclass 组按 BinFlow 实有三态（remote 浏览面即
//      缓存落地行，不伪造 Cache 第四态——K67 冻结候选注记）。
//   ⑤ axe 双主题 serious/critical = 0（工具带 + 展开树 + 文件选中全形态）。
//
// 夹具纪律：Playwright 栈自建（仓库 PUT + 内容面上传，零外部状态）；锚源 =
// console-ux §10.5 T-434 批（tree-leaf-* + tree-toolband 族，v1.31）。

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

/** 树头工具带默认可见性断言（多腿共用的前置形态） */
async function expectToolband(page: Page) {
  await expect(page.locator('[data-testid="tree-toolband"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-repo-filter"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-favorites"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-sort-by"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-view-compacted"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-view-normal"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-facet-rclass-local"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-facet-rclass-remote"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-facet-rclass-virtual"]')).toBeVisible()
}

// ---------------------------------------------------------------------------
// ① 文件叶子进树 + 目录概要 + children 表收窄（B-1.1）
// ---------------------------------------------------------------------------

test('tree leaves: dirs expand into folder rows AND file leaves; file-only dirs are not "（空）"; leaf click opens item view', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t434a')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'guide')
  await api(page, 'PUT', `/${key}/docs/deep/nested.bin`, 'nested')
  await api(page, 'PUT', `/${key}/onlyfiles/a.bin`, 'a')
  await api(page, 'PUT', `/${key}/onlyfiles/b.bin`, 'b')

  await page.goto(`/binflow/ui/artifacts/${key}`)
  const repoRow = page.locator(`[data-testid="tree-repo-${key}"]`)
  await expect(repoRow).toBeVisible()

  // 仓根展开 = 子目录行 +（本夹具根层只有目录；文件的叶子在目录层断言）
  await repoRow.locator('.twisty').click()
  await expect(repoRow).toHaveAttribute('aria-expanded', 'true')
  await expect(page.locator('[data-testid="tree-node-docs"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-node-onlyfiles"]')).toBeVisible()

  // 混合目录：docs/ = 子目录 deep + 文件叶子 guide.md
  await page.locator('[data-testid="tree-node-docs"] .twisty').click()
  await expect(page.locator('[data-testid="tree-node-docs/deep"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-leaf-docs/guide.md"]')).toBeVisible()

  // 仅含文件的目录：叶子行在场 = 非空（「（空）」误导占位的反转断言）
  await page.locator('[data-testid="tree-node-onlyfiles"] .twisty').click()
  await expect(page.locator('[data-testid="tree-leaf-onlyfiles/a.bin"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-leaf-onlyfiles/b.bin"]')).toBeVisible()
  const emptyLevel = page.locator('.browser-tree-scroll .tree-empty-level')
  await expect(emptyLevel).toHaveCount(0)

  // 文件叶子点击 = 选中出右侧 item view（文件是 URL 路径末段，非 ?focus=）
  await page.locator('[data-testid="tree-leaf-docs/guide.md"]').click()
  await expect(page).toHaveURL(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"] h3 .mono')).toHaveText('docs/guide.md')

  // 叶子选中态（selected class）+ 表行联动选中
  await expect(page.locator('[data-testid="tree-leaf-docs/guide.md"]')).toHaveClass(/selected/)
  await expect(page.locator('[data-testid="tree-row-guide.md"]')).toHaveClass(/selected/)

  // 目录选中给直系概要（Artifact Count / Size——children 表收窄的补偿面）
  await page.locator('[data-testid="tree-node-docs"]').click()
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('目录 1 · 文件 1')
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('Size（直系文件合计）')

  // children 表收窄：操作列退役——表域零 delete-node-button 承载（详情
  // 面板是唯一行内锚承载，目录选中态已在场——见下）
  await expect(page.locator('[data-testid="tree-list"] [data-testid="delete-node-button"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="node-detail"] [data-testid="delete-node-button"]')).toBeVisible()
  // 文件选中后详情面板删除钮在场（删除收敛的唯一承载 + 危险确认门）
  await page.locator('[data-testid="tree-leaf-onlyfiles/a.bin"]').click()
  await expect(page.locator('[data-testid="node-detail"] [data-testid="delete-node-button"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ② 选择 ≠ 展开（B-1.2）
// ---------------------------------------------------------------------------

test('select is not expand: repo click keeps branch collapsed; twisty is independent; deep-link ancestors still auto-expand', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t434b')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'g')

  await page.goto(`/binflow/ui/artifacts/${key}`)
  const repoRow = page.locator(`[data-testid="tree-repo-${key}"]`)
  await expect(repoRow).toBeVisible()
  await expect(repoRow).toHaveAttribute('aria-expanded', 'false')

  // 单击仓库名 = 纯选中：右侧面切换（仓库形态详情），展开态不变（断言反转①
  // 的正题——原 selectedRepo 并集强制展开）
  await repoRow.click()
  await expect(page).toHaveURL(`/binflow/ui/artifacts/${key}`)
  await expect(repoRow).toHaveAttribute('aria-expanded', 'false')
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('包类型')

  // 再点两次仓库名：仍是纯选中，展开态不动（不因重复选中翻转）
  await repoRow.click()
  await expect(repoRow).toHaveAttribute('aria-expanded', 'false')

  // twisty 独立展开/收起
  await repoRow.locator('.twisty').click()
  await expect(repoRow).toHaveAttribute('aria-expanded', 'true')
  await expect(page.locator('[data-testid="tree-node-docs"]')).toBeVisible()
  await repoRow.locator('.twisty').click()
  await expect(repoRow).toHaveAttribute('aria-expanded', 'false')

  // 深链祖先链维持：/artifacts/<key>/docs → 仓根 + docs 自动展开 + 目录选中
  await page.goto(`/binflow/ui/artifacts/${key}/docs`)
  await expect(page.locator(`[data-testid="tree-repo-${key}"]`)).toHaveAttribute('aria-expanded', 'true')
  const docsNode = page.locator('[data-testid="tree-node-docs"]')
  await expect(docsNode).toBeVisible()
  await expect(docsNode).toHaveAttribute('aria-expanded', 'true')
  await expect(docsNode).toHaveClass(/selected/)
})

// ---------------------------------------------------------------------------
// ③ URL 模型段化（B-1.3）：页签段 + 文件路径段 + ?focus= 兼容映射 + 三态一致
// ---------------------------------------------------------------------------

test('url model: tab is a path segment, file is the last segment, legacy ?focus= maps once, reload keeps full state', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t434c')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'g')

  // 旧深链兼容映射：?focus= 一次性 replace 折入路径段（书签不猝死）
  await page.goto(`/binflow/ui/artifacts/${key}/docs?focus=${encodeURIComponent('guide.md')}`)
  await expect(page).toHaveURL(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('docs/guide.md')

  // 页签进 URL：切到有效权限 → /artifacts/properties 前缀段出现
  await page.click('[data-testid="node-tab-perms"]')
  await expect(page).toHaveURL(`/binflow/ui/artifacts/permissions/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-perms"]')).toBeVisible()

  // 切回常规：段消失（省略 = general 的规范形）
  await page.click('[data-testid="node-tab-general"]')
  await expect(page).toHaveURL(`/binflow/ui/artifacts/${key}/docs/guide.md`)

  // 三态一致（刷新/重开）：URL 完整承载 页签+仓库+路径+文件
  await page.click('[data-testid="node-tab-perms"]')
  const shared = page.url()
  await page.reload()
  await expect(page).toHaveURL(shared)
  await expect(page.locator('[data-testid="node-perms"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-detail"] h3 .mono')).toHaveText('docs/guide.md')

  // 页签段深链直达（Artifactory /tree/<TAB>/… 对位形态）
  await page.goto(`/binflow/ui/artifacts/properties/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-tab-props"]')).toHaveAttribute('aria-selected', 'true')
  await expect(page.locator('[data-testid="node-props"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ④ 树头工具带（B-1.4）：facet / rclass 组 / Sort-by / Compacted / My Favorites
// ---------------------------------------------------------------------------

test('toolband: pkg-type facet, rclass group, sort-by, compacted radio, my favorites persist across reload', async ({
  page,
}) => {
  await login(page)
  const ts = Date.now().toString(36)
  // 命名反序夹具：name 序与 rclass 序不同（z=local 在名序末、rclass 序首）
  const zLocal = `t434z${ts}`
  const aVirtual = `t434a${ts}`
  await api(page, 'PUT', `/api/repositories/${zLocal}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/api/repositories/${aVirtual}`, {
    rclass: 'virtual',
    packageType: 'generic',
    repositories: [zLocal],
  })
  await api(page, 'PUT', `/api/repositories/t434d${ts}`, { rclass: 'local', packageType: 'docker' })

  await page.goto('/binflow/ui/artifacts')
  await expectToolband(page)

  const rowZ = page.locator(`[data-testid="tree-repo-${zLocal}"]`)
  const rowA = page.locator(`[data-testid="tree-repo-${aVirtual}"]`)
  const rowD = page.locator(`[data-testid="tree-repo-t434d${ts}"]`)
  for (const r of [rowZ, rowA, rowD]) await expect(r).toBeVisible()

  // Sort-by 默认名称序：a < d < z（virtual 的 a 前置）
  const nameOrder = await page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLElement>('[data-tree-row]'))
      .map((el) => el.getAttribute('data-testid') ?? '')
      .filter((t) => t.startsWith('tree-repo-t434')),
  )
  expect(nameOrder.indexOf(`tree-repo-${aVirtual}`)).toBeLessThan(nameOrder.indexOf(`tree-repo-${zLocal}`))

  // Sort-by 切仓库类型：local（z、d）在 virtual（a）前
  await page.selectOption('[data-testid="tree-sort-by"]', 'rclass')
  const rclassOrder = await page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLElement>('[data-tree-row]'))
      .map((el) => el.getAttribute('data-testid') ?? '')
      .filter((t) => t.startsWith('tree-repo-t434')),
  )
  expect(rclassOrder.indexOf(`tree-repo-${zLocal}`)).toBeLessThan(rclassOrder.indexOf(`tree-repo-${aVirtual}`))
  await page.selectOption('[data-testid="tree-sort-by"]', 'name')

  // 包类型 facet：勾 docker → 树仅 docker 仓；清除复位
  await page.click('[data-testid="tree-facet-pkg"]')
  await expect(page.locator('[data-testid="tree-facet-pkg-panel"]')).toBeVisible()
  await page.check('[data-testid="tree-facet-pkg-docker"]')
  await expect(rowZ).toHaveCount(0)
  await expect(rowA).toHaveCount(0)
  await expect(rowD).toBeVisible()
  await page.click('[data-testid="tree-facet-pkg-clear"]')
  await expect(rowZ).toBeVisible()
  await expect(rowA).toBeVisible()
  // 收起 facet 弹层：点遮罩（backdrop onClose）——Escape 依赖层内焦点、
  // 触发钮被遮罩挡住，二者都不稳
  await page.mouse.click(5, 5)
  await expect(page.locator('[data-testid="tree-facet-pkg-panel"]')).toHaveCount(0)

  // rclass 组：勾 virtual → 仅 virtual 仓；取消复位
  await page.check('[data-testid="tree-facet-rclass-virtual"]')
  await expect(rowZ).toHaveCount(0)
  await expect(rowD).toHaveCount(0)
  await expect(rowA).toBeVisible()
  await page.uncheck('[data-testid="tree-facet-rclass-virtual"]')
  await expect(rowZ).toBeVisible()

  // Compacted 单选：行高收窄（结构门 = 容器档位类 + 行高实测）
  const heightOf = async () => (await rowZ.boundingBox())?.height ?? 0
  const normalH = await heightOf()
  await page.check('[data-testid="tree-view-compacted"]')
  await expect(page.locator('[data-testid="browser-tree"]')).toHaveClass(/compacted/)
  const compactH = await heightOf()
  expect(compactH).toBeLessThan(normalH)
  await page.check('[data-testid="tree-view-normal"]')
  await expect(page.locator('[data-testid="browser-tree"]')).not.toHaveClass(/compacted/)

  // My Favorites：右键标记 → 计数 1 → 过滤仅收藏 → reload 持久（AC4）
  await rowZ.click({ button: 'right' })
  await page.click('[data-testid="tree-context-favorite"]')
  await expect(page.locator('[data-testid="tree-favorites"]')).toContainText('（1）')
  await page.click('[data-testid="tree-favorites"]')
  await expect(rowA).toHaveCount(0)
  await expect(rowD).toHaveCount(0)
  await expect(rowZ).toBeVisible()
  // 取消收藏菜单项形态（同一入口的翻转）
  await rowZ.click({ button: 'right' })
  await expect(page.locator('[data-testid="tree-context-favorite"]')).toContainText('取消收藏')
  await page.keyboard.press('Escape')
  await page.reload()
  await expectToolband(page)
  await expect(page.locator('[data-testid="tree-favorites"]')).toContainText('（1）')
  await page.click('[data-testid="tree-favorites"]')
  await expect(page.locator(`[data-testid="tree-repo-${zLocal}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="tree-repo-${aVirtual}"]`)).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ⑤ axe 双主题：工具带 + 展开树（含文件叶子）+ 文件选中全形态
// ---------------------------------------------------------------------------

test('axe: tree page with toolband, file leaves and url-tabbed detail clean in both themes', async ({
  page,
}, testInfo: TestInfo) => {
  await login(page)
  const key = uniq('t434e')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'g')

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    // 页签段深链 + 仓根展开（叶子行在 DOM）后扫描
    await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="tree-leaf-docs/guide.md"]')).toBeVisible()
    await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="tree-page"]' })
  }
})
