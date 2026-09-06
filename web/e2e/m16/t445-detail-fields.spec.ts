import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { expectCopied } from '../m8/support/clipboard'

// T-445（M16 批次③ 首票，FR-144.1/.2/.3——B-2.1/7 页签序 + B-2.3/4/10 元数据
// 字段族 + Downloads 渲染）：
//
//   ① 页签序统一：General → Effective Permissions → Properties——权限在
//      属性前（7.161.20 活体 A2-7 + 7.84 reverse §3.2 逐级一致）；仓级无
//      属性页签（BinFlow 仓根无节点行契约）、非 admin 无权限页签（SE-08
//      门）——两档缺位下剩余序一致；admin-only 门控维持。
//   ② 元数据字段族：File URL（含复制钮，仓/目录/文件三形态齐备）；
//      Downloads / Last Downloaded By / Last Downloaded / Remote Downloads
//      族——消费 T-438 ?stats 面（计数全档可见、lastDownloadedBy 仅
//      CapSystemRead 档回带，其余档 UI 呈 '—' 不伪造）；仓视图
//      Repository Layout（'—'，K70 预留位）/ Description / Created（'—'，
//      无 wire 面）/ Artifact Count（usage counts 面 nodeCount）。
//   ③ 缺位登记（不伪造，反断言）：Module ID 不建（Build-info stay-out）；
//      Package Information / Virtual Repository Associations / Included
//      Repositories 块不建；folder 形态无下载统计族（结构性零值不渲染）。
//   ④ axe 双主题 serious/critical = 0（文件详情全字段族 + 仓视图）。
//
// 夹具纪律：Playwright 栈自建（仓库 PUT + 内容面上传 + 用户/权限面备料，
// 零外部状态）；锚源 = console-ux §10.5 T-445 批（node-file-url +
// node-downloads 族 + node-repo-* 族，v1.36）。
//
// 计数断言口径（T-438 §3-2 探针面计数继承）：item-info GET 会 +1 计数
// （as-built 继承，audit 面整理票候选）——本 spec 不断言绝对值，断言
// 「UI 呈现 == 同刻 ?stats 面」+「面值 >= 内容面 GET 次数」，对上游
// 计数口径翻转免疫。

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

/** 同源 fetch（携带 session cookie）；返回 {status, json, text}。noStore =
 *  下载腿专用（T-494 注）：内容面 GET 带 Etag/Last-Modified 无 Cache-Control，
 *  同 URL 重复 fetch 走条件请求（304 不落服务端 Get 落点 = 不计数）——
 *  T-494 起浏览零计数贡献，>=3 断言只靠真实 GET，必须 no-store。 */
async function api(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
  noStore = false,
): Promise<{ status: number; json: unknown; text: string }> {
  const r = await page.evaluate(
    async ({ method, path, body, noStore }) => {
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        cache: noStore ? 'no-store' : undefined,
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
    { method, path, body, noStore },
  )
  return r
}

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 详情页签的 DOM 序（[role=tab] 内文本，按渲染序） */
async function tabOrder(page: Page): Promise<string[]> {
  return page.locator('[data-testid="node-detail"] [role="tablist"] [role="tab"]').allInnerTexts()
}

interface StatsFace {
  downloadCount: number
  lastDownloaded?: string
  lastDownloadedBy?: string
  remoteDownloadCount: number
}

/** ?stats 面直读（同会话 cookie） */
async function statsFace(page: Page, repo: string, path: string): Promise<StatsFace> {
  const r = await api(page, 'GET', `/api/storage/${repo}/${path}?stats`)
  expect(r.status).toBe(200)
  return r.json as StatsFace
}

// ---------------------------------------------------------------------------
// ① 页签序统一（B-2.1/7）——权限在属性前，逐级一致
// ---------------------------------------------------------------------------

test('detail tabs: General → Effective Permissions → Properties at every level (permissions before properties)', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t445a')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'g')

  // 仓库级：常规 → 有效权限（属性页签缺席 = 仓根无节点行契约，缺位登记
  // 不伪造——Artifactory 仓级有 Properties，BinFlow 无仓级属性面）
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  expect(await tabOrder(page)).toEqual(['常规', '有效权限'])
  await expect(page.locator('[data-testid="node-tab-props"]')).toHaveCount(0)

  // 目录级：三页签，权限在属性前
  await page.goto(`/binflow/ui/artifacts/${key}/docs`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  expect(await tabOrder(page)).toEqual(['常规', '有效权限', '属性'])

  // 文件级：同序（reverse §3.2 文件页签集含 Followers/Xray——Non-goal）
  await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  expect(await tabOrder(page)).toEqual(['常规', '有效权限', '属性'])

  // URL slug 零变化（T-434 模型不动——仅渲染序互换）
  await page.click('[data-testid="node-tab-perms"]')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/permissions/${key}/docs/guide.md$`))
})

// ---------------------------------------------------------------------------
// ② File URL 三形态 + 复制钮 + Downloads 族端到端（消费 T-438 ?stats 面）
// ---------------------------------------------------------------------------

test('file detail: File URL copy button, downloads family end-to-end via ?stats (admin sees lastDownloadedBy)', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t445b')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'payload-t445')

  // 内容面 3 次 GET（AC 口径：curl 下载 3 次；no-store——见 api 注的 304 形态）
  for (let i = 0; i < 3; i++) {
    expect((await api(page, 'GET', `/${key}/docs/guide.md`, undefined, true)).status).toBe(200)
  }

  await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  const detail = page.locator('[data-testid="node-detail"]')
  await expect(detail).toBeVisible()

  // File URL 行：绝对内容面 URL（origin + /binflow/<repo>/<path>）+ 复制钮
  // 拿到完整值（§7.3——展示可断流，拷贝不截断）
  const origin = new URL(page.url()).origin
  const fileURL = `${origin}/binflow/${key}/docs/guide.md`
  const urlCell = page.locator('[data-testid="node-file-url"]')
  await expect(urlCell).toContainText(fileURL)
  await expectCopied(page, urlCell.locator('.copy-btn'), fileURL)

  // 字段序（reverse §3.2）：File URL 在 Repository Path 之后、部署者之前
  const labels = await detail.locator('.kv .k').allInnerTexts()
  const idx = (name: string) => labels.findIndex((l) => l === name)
  expect(idx('File URL')).toBeGreaterThan(idx('Repository Path'))
  expect(idx('File URL')).toBeLessThan(idx('部署者'))

  // 下载统计族：UI 呈现 == 同刻 ?stats 面（探针面计数继承下不赌绝对值）；
  // 面值 >= 3（内容面 GET 已落）；lastDownloadedBy = 内容面调用者（admin）
  const downloads = page.locator('[data-testid="node-downloads"]')
  await expect(downloads).not.toHaveText('…')
  const face = await statsFace(page, key, 'docs/guide.md')
  expect(face.downloadCount).toBeGreaterThanOrEqual(3)
  await expect(downloads).toHaveText(String(face.downloadCount))
  await expect(page.locator('[data-testid="node-last-downloaded-by"]')).toHaveText(ADMIN)
  await expect(page.locator('[data-testid="node-last-downloaded"]')).toHaveText(
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/,
  )
  // local 仓无 remote 服务下载（T-438 §1.3 语义：该列计 remote 仓服务点）
  await expect(page.locator('[data-testid="node-remote-downloads"]')).toHaveText(/^\d+$/)

  // 目录形态：File URL 补齐（B-2.4——尾斜杠拼写）；无下载统计族
  // （folder 结构性零值不渲染，Artifactory folder item view 同为无下载族）
  await page.goto(`/binflow/ui/artifacts/${key}/docs`)
  await expect(page.locator('[data-testid="node-file-url"]')).toContainText(
    `/binflow/${key}/docs/`,
  )
  await expect(page.locator('[data-testid="node-downloads"]')).toHaveCount(0)

  // 仓库形态：File URL 行在场（GET /api/repositories 回带的 url 字段）
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="node-file-url"]')).toContainText(`/${key}`)
})

test('detail fields: Module ID and virtual-association blocks are absent (registered, not fabricated)', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t445f')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'g')

  await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  const detailText = await page.locator('[data-testid="node-detail"]').innerText()
  // 缺位登记反断言：Module ID 不建（Build-info stay-out §9A-S8）；Package
  // Information / Dependency Declaration / Virtual Repository Associations /
  // Included Repositories 块不建（BinFlow 无包信息域/仓关联面——不伪造）
  for (const absent of [
    'Module ID',
    'Package Information',
    'Dependency Declaration',
    'Virtual Repository Associations',
    'Included Repositories',
  ]) {
    expect(detailText, `field ${absent} must not be fabricated`).not.toContain(absent)
  }
})

test('plain user: download counts visible, lastDownloadedBy withheld as "—" (CapSystemRead gate)', async ({
  page,
  browser,
}) => {
  await login(page)
  const key = uniq('t445c')
  const user = uniq('plain')
  const target = uniq('tgt')
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'g')
  await api(page, 'GET', `/${key}/docs/guide.md`)
  expect(
    (
      await api(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: 't445-plain-pw',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)
  expect(
    (
      await api(page, 'POST', '/api/v1/permissions', {
        name: target,
        repos: [key],
        includePatterns: ['**'],
        excludePatterns: [],
        principals: { users: { [user]: ['read'] } },
        groups: {},
      })
    ).status,
  ).toBe(201)

  // 普通用户会话（独立上下文）：深链合成顶层节点 → 文件详情可达
  const origin = new URL(page.url()).origin
  const ctx = await browser.newContext()
  const plain = await ctx.newPage()
  await plain.goto(`${origin}/binflow/ui/`)
  await plain.fill('[data-testid="login-username"]', user)
  await plain.fill('[data-testid="login-password"]', 't445-plain-pw')
  await plain.click('[data-testid="login-submit"]')
  await expect(plain.locator('[data-testid="app-nav"]')).toBeVisible()
  await plain.goto(`${origin}/binflow/ui/artifacts/${key}/docs/guide.md`)
  await expect(plain.locator('[data-testid="node-detail"]')).toBeVisible()

  // 页签序（非 admin 档）：常规 → 属性（权限页签 SE-08 门缺席——剩余序一致）
  expect(
    await plain.locator('[data-testid="node-detail"] [role="tablist"] [role="tab"]').allInnerTexts(),
  ).toEqual(['常规', '属性'])

  // 计数面无门（?stats 计数全档可见）；lastDownloadedBy 服务端 omitempty
  // 省略 → UI '—'（不伪造、不区分「从未下载」与「非档位省略」）
  const downloads = plain.locator('[data-testid="node-downloads"]')
  await expect(downloads).not.toHaveText('…')
  const face = await statsFace(plain, key, 'docs/guide.md')
  expect(face.lastDownloadedBy).toBeUndefined()
  expect(face.downloadCount).toBeGreaterThanOrEqual(1)
  await expect(downloads).toHaveText(String(face.downloadCount))
  await expect(plain.locator('[data-testid="node-last-downloaded-by"]')).toHaveText('—')
  await ctx.close()
})

// ---------------------------------------------------------------------------
// ③ 仓视图字段族（B-2.4）：Layout/Description/Created/Artifact Count
// ---------------------------------------------------------------------------

test('repo view: Description echoes, Repository Layout and Created render "—" (no source, not fabricated), Artifact Count from usage counts', async ({
  page,
}) => {
  await login(page)
  const key = uniq('t445d')
  const desc = `t445-描述-${key}`
  await api(page, 'PUT', `/api/repositories/${key}`, {
    rclass: 'local',
    packageType: 'generic',
    description: desc,
  })
  await api(page, 'PUT', `/${key}/a/x.bin`, 'a')
  await api(page, 'PUT', `/${key}/b/y.bin`, 'b')

  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()

  // Description 回显（GET /api/repositories 回带）
  await expect(page.locator('[data-testid="node-repo-description"]')).toHaveText(desc)

  // Repository Layout / Created：无 wire 源（K70 预留位 / CreatedAt 未投影）
  // ——恒 '—' + title 登记（缺位不伪造）
  const layout = page.locator('[data-testid="node-repo-layout"]')
  await expect(layout).toHaveText('—')
  await expect(layout).toHaveAttribute('title', /K70/)
  await expect(page.locator('[data-testid="node-repo-created"]')).toHaveText('—')

  // Artifact Count（usage counts 面 nodeCount = FILE 节点数，folder 哨兵
  // 行排除——2 个文件）；大小行同源在场（Artifactory 的 Size: Show 懒展开
  // 不建：usage 面廉价直接渲染，形态简化留痕）
  await expect(page.locator('[data-testid="node-repo-artifact-count"]')).toHaveText('2')
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('大小')
})

// ---------------------------------------------------------------------------
// ④ axe 双主题：字段族全形态（文件详情 + 仓视图）
// ---------------------------------------------------------------------------

test('axe: detail field family clean in both themes', async ({ page }, testInfo: TestInfo) => {
  await login(page)
  const key = uniq('t445e')
  await api(page, 'PUT', `/api/repositories/${key}`, {
    rclass: 'local',
    packageType: 'generic',
    description: 'axe-fixture',
  })
  await api(page, 'PUT', `/${key}/docs/guide.md`, 'g')
  await api(page, 'GET', `/${key}/docs/guide.md`)

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto(`/binflow/ui/artifacts/${key}/docs/guide.md`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="node-file-url"]')).toBeVisible()
    await expect(page.locator('[data-testid="node-last-downloaded-by"]')).not.toHaveText('…')
    await expectA11yClean(page, testInfo, { include: '[data-testid="tree-page"]' })

    // 仓视图（Layout/Created '—' + Artifact Count 行同扫）
    await page.goto(`/binflow/ui/artifacts/${key}`)
    await expect(page.locator('[data-testid="node-repo-artifact-count"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="tree-page"]' })
  }
})
