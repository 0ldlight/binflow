import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, sessionApi } from '../m8/support/seed'

// T-383（M14 B2 FE，FR-124.2 M1）——建仓形态「断言收口」（原票「单 Dialog
// 化」经 T-381 活体核验 v1.1 改判撤销：Artifactory 7.84 建仓实测 = 「Add
// Repositories 下拉选 rclass → 880px 磁贴网格 modal（Select Package Type）
// → 选型后落整页路由表单」**两段式**，BinFlow 现形态（pkg-grid Dialog +
// /admin/repositories/new 路由页表单）与其同构——形态已对齐，本 spec 把
// 对齐逐项钉死为断言，不改产品形态）。
//
// 钉死面（对照核验表逐项 ↔ reports/agents/T-383.md §1）：
//   ① 两段式：网格 = modal（role=dialog）+ 表单 = 整页路由（选型后 modal
//      关闭、URL 不变、表单仍在路由页——非单 modal 全程）；
//   ② rclass 选择形态：入口预选（列表钮 + quick 菜单三型下拉 = Add
//      Repositories 下拉的对位）+ ?rclass= 深链直达 + 页内单选组回显；
//   ③ 磁贴网格：radiogroup 语义 + 原生 button 磁贴 + 组合门控退役（docker
//      三仓型全开——T-431 沿 T-392 remote / T-431 virtual 的服务端矩阵）
//      + BinFlow 定案宽度档 440px（不追平 880px——33 包型 880px 网格
//      vs BinFlow 5 核心 + 门控槽位，追平即大面积留白；parity 册 M1 行
//      「现档位即可」既有裁定，票内留痕）；
//   ④ 六节结构：form-section-*（v1.19 批锚）条件呈现矩阵 × 三 rclass；
//   ⑤ 全链：选择 → 表单 → Save 落仓（toast + 详情 + API 对账）。
//
// 锚源：console-ux §10（pkg-grid-* / form-* 冻结族零改名 + T-383 批
// form-section-{general|source|members|policy|governance|advanced}）。
// 断言口径 = e2e/m8/README §2（ADR-0029 决策 3 交互断言制）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 网格 Dialog 选型并等待两段式过渡（modal 关闭、表单在路由页就绪）。
 *  返回表单页根 locator。 */
async function pickFromGrid(page: Page, pt: string): Promise<ReturnType<Page['locator']>> {
  await page.click(`[data-testid="pkg-grid-item-${pt}"]`)
  // 两段式钉死点：选型后 modal 关闭，表单**仍在整页路由**（URL 不变）
  await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCount(0)
  const form = page.locator('[data-testid="repo-form-page"]')
  await expect(form).toBeVisible()
  return form
}

/** 六节结构条件呈现矩阵（T-383 对照核验表 ④ 的断言形态）。
 *  visible = 在场节；hidden = 反断言（条件节不渲染——不是隐藏，锚计数 0）。 */
async function expectSections(
  page: Page,
  visible: Array<'general' | 'source' | 'members' | 'policy' | 'governance' | 'advanced'>,
): Promise<void> {
  const ALL = ['general', 'source', 'members', 'policy', 'governance', 'advanced'] as const
  for (const s of ALL) {
    const loc = page.locator(`[data-testid="form-section-${s}"]`)
    if (visible.includes(s)) await expect(loc).toBeVisible()
    else await expect(loc).toHaveCount(0)
  }
}

// ---- 1. 建仓全链：列表入口 → 磁贴选择 → 表单 → Save 落仓（+ API 对账） ------

test('admin: two-phase create chain — grid modal pick → full-page form → Save lands the repo', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const key = uniq('t383new')

  // 入口 A：仓库列表「＋ 添加仓库」（rclass 由当前 Tab 语境 + URL query 预选）
  await page.goto('/binflow/ui/admin/repositories/local')
  await page.click('[data-testid="repos-create"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/new$/)

  // 段 1（选择）：网格 = modal + 表单页已在路由上（两段式第一段）
  const grid = page.locator('[data-testid="pkg-grid"]')
  await expect(grid).toBeVisible()
  await expect(grid).toHaveAttribute('role', 'dialog')
  await expect(grid).toContainText('选择包类型')
  await expect(page.locator('[data-testid="repo-form-page"]')).toBeVisible()

  // 段 2（表单）：选 generic → modal 关闭、URL 不变、表单仍在路由页
  await pickFromGrid(page, 'generic')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/new$/)
  await expect(page.locator('[data-testid="form-rclass-local"]')).toBeChecked()
  await expect(page.locator('[data-testid="form-package-generic"]')).toBeChecked()

  // 六节结构（local × generic）：常规/治理/高级在场；来源/成员/策略不渲染
  //（常规节逐字锚定 = key/描述字段所在节——全链填充动作的目标节）
  await expect(page.locator('[data-testid="form-section-general"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-section-advanced"]')).toBeVisible()
  await expectSections(page, ['general', 'governance', 'advanced'])

  // Save 落仓：必填未满足禁用 → 填 key → 创建 → toast + 详情落点
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="form-key"]', key)
  await page.fill('[data-testid="form-description"]', 't383 parity probe')
  await expect(page.locator('[data-testid="form-submit"]')).toBeEnabled()
  await page.click('[data-testid="form-submit"]')

  // toast 是 5s TTL 瞬态：先验 toast 再验 URL（m8 repositories-admin 同款次序）
  await expect(page.locator('[data-testid="toast"]')).toContainText(`Successfully created repository '${key}'`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${key}$`))
  await expect(page.locator('[data-testid="repo-detail-page"] .key')).toHaveText(key)

  // API 对账（UI 说的话让 API 复核）
  const got = await sessionApi(page, 'GET', `/api/repositories/${key}`)
  expect(got.status).toBe(200)
  const body = got.json as { rclass: string; packageType: string }
  expect(body.rclass).toBe('local')
  expect(body.packageType).toBe('generic')

  // 收尾（空仓直接删）
  expect((await m8Client().request('DELETE', `/binflow/api/repositories/${key}`)).status).toBeLessThan(300)
})

// ---- 2. 深链 + rclass 条件节矩阵（?rclass= 直达表单页；只读不落仓） ----------

test('admin: ?rclass= deep links reach the form page; six-section matrix per rclass', async ({ page }) => {
  await loginAs(page, 'admin')

  // 深链 remote：直达表单页 + 网格即开（rclass 由 URL 预选，非向导内 Tab）
  await page.goto('/binflow/ui/admin/repositories/new?rclass=remote')
  const grid = page.locator('[data-testid="pkg-grid"]')
  await expect(page.locator('[data-testid="repo-form-page"]')).toBeVisible()
  await expect(grid).toBeVisible()
  await expect(grid).toContainText('Remote')

  // 组合门控退役（T-431）：remote × docker 可选（T-392 开的服务端格，FE 门
  // 随 virtual 开禁一并退役）；license 门控槽位的禁用与此无关、另行断言
  await expect(page.locator('[data-testid="pkg-grid-item-docker"]')).toBeEnabled()
  await expect(page.locator('[data-testid="pkg-grid-item-maven"]')).toBeEnabled()

  await pickFromGrid(page, 'maven')
  await expect(page.locator('[data-testid="form-rclass-remote"]')).toBeChecked()
  await expect(page.locator('[data-testid="form-package-maven"]')).toBeChecked()
  // 六节（remote × maven）：来源在场（上游 URL）；治理/成员/策略不渲染
  await expectSections(page, ['general', 'source', 'advanced'])
  await expect(page.locator('[data-testid="form-section-source"] [data-testid="form-url"]')).toBeVisible()

  // 深链 virtual：成员节在场；来源/治理不渲染
  await page.goto('/binflow/ui/admin/repositories/new?rclass=virtual')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await pickFromGrid(page, 'generic')
  await expect(page.locator('[data-testid="form-rclass-virtual"]')).toBeChecked()
  //（成员节逐字锚定 = virtual 的定义节——成员选择 + 解析顺序）
  await expect(page.locator('[data-testid="form-section-members"]')).toBeVisible()
  await expectSections(page, ['general', 'members', 'advanced'])

  // 默认入口 local × maven：Maven 策略节在场（deb/rpm/helm 策略组仍在高级节内）
  await page.goto('/binflow/ui/admin/repositories/new')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await pickFromGrid(page, 'maven')
  await expectSections(page, ['general', 'policy', 'governance', 'advanced'])
  //（策略节内容锚定：handle*/checksum/SNAPSHOT 旧静态锚已随 v1.9 零消费
  //  退役——节内以节锚 + 字段 label 文案锚定，不复活退役锚）
  await expect(page.locator('[data-testid="form-section-policy"]')).toContainText('checksum 策略')
})

// ---- 3. rclass 入口形态 + 磁贴网格 Dialog 形态钉死（宽度档定案 + Esc） --------

test('admin: grid modal shape pin — radiogroup tiles, 440px decided tier, Esc cancel; quick-menu dropdown entry', async ({
  page,
}) => {
  await loginAs(page, 'admin')

  // 入口 B：quick 菜单「快速建仓」三型下拉 = Artifactory「Add Repositories」
  // 下拉（Local/Remote/Virtual 预选）的对位形态（parity v1.1：手势等价）
  await page.click('[data-testid="session-toggle"]')
  await page.click('[data-testid="quick-new-repo-remote"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/new\?rclass=remote$/)
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  // rclass 预选感知的退出：取消回对应 Tab（remote）
  await page.click('[data-testid="pkg-grid-cancel"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/remote$/)

  // 磁贴网格形态：radiogroup 语义 + 原生 button 磁贴（五核心恒在场；门控
  // 槽位随 license 增列——不断言精确计数，只断言闭集地板 + 磁贴控件形态）
  await page.goto('/binflow/ui/admin/repositories/new')
  const grid = page.locator('[data-testid="pkg-grid"]')
  await expect(grid).toBeVisible()
  await expect(page.locator('[data-testid="pkg-grid"] [role="radiogroup"]')).toBeVisible()
  for (const pt of ['generic', 'docker', 'maven', 'npm', 'pypi']) {
    const tile = page.locator(`[data-testid="pkg-grid-item-${pt}"]`)
    await expect(tile).toBeVisible()
    await expect(tile).toHaveAttribute('role', 'radio')
  }

  // 宽度档钉死（票内定案）：BinFlow 440px 紧凑档——不追平 v1.1 实测 880px
  // （33 包型网格的档位；BinFlow 5 核心 + 门控槽位，追平即大面积留白）。
  // 几何断言体例 = T-382 expectDrawerGeometry 同款（boundingBox，非像素）。
  await expect(grid).toHaveCSS('opacity', '1') // Fade 收敛后再取 box
  const box = await grid.boundingBox()
  expect(box, 'pkg-grid paper has a box').toBeTruthy()
  const vw = page.viewportSize()?.width ?? 1280
  expect(box!.width).toBeCloseTo(Math.min(440, vw - 48), 0)

  // Esc = 取消关闭（M4 族通用规格）：回对应 Tab、无写请求语义
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCount(0)
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)
})

// ---- 4. axe 双主题：建仓两段全态（网格 modal + 创建态表单 × local/remote） ----

test('axe: create-repo states clean in both themes (grid modal + create-form local & remote)', async ({
  page,
}, testInfo) => {
  test.setTimeout(300_000) // 6 面 × 双主题；axe 在默认并发下实测 30s+/面
  await loginAs(page, 'admin')

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/repositories/new')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)

    // 段 1：磁贴网格 modal（a11y-sweep 路由面只扫得到网格开态——此处同态
    // 复扫钉本票面；入场 Fade 收敛后再扫，防半透明栈假阳性 T-344C D7）
    await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
    await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCSS('opacity', '1')
    await expectA11yClean(page, testInfo, { include: '[data-testid="pkg-grid"]' })

    // 段 2a：创建态表单 local × generic（sweep 只见网格开态/编辑态——创建态
    // 六节呈现是本票新增扫描面）
    await pickFromGrid(page, 'generic')
    await expect(page.locator('[data-testid="form-section-governance"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repo-form-page"]' })

    // 段 2b：创建态表单 remote × maven（来源节 + 高级 TTL 四键的创建态面）
    await page.goto('/binflow/ui/admin/repositories/new?rclass=remote')
    await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
    await pickFromGrid(page, 'maven')
    await expect(page.locator('[data-testid="form-section-source"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repo-form-page"]' })
  }
})
