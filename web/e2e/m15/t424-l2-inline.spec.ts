import { expect, test } from '@playwright/test'
import type { TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, sessionApi } from '../m8/support/seed'

// T-424（M15 B8 FE，FR-140.1 / LC-79 C 层自有增强——L31 主腿）：仓库列表
// 行内 L2 快捷的交互断言成文——「复制 key」与「Set Me Up」自 M8 起即有
// （CopyButton 挂 key 列 / repos-setmeup-* 挂操作列），本票是**纪律票零
// 行为改动**：不改 src、不加锚，把既有能力钉成断言 + E1 不倒退缺席断言
// + a11y 腿（PRD §FR-140 AC1 / M15-SPLIT §1.2）。
//
// 断言面：
//   ① 复制 key：aria-label（§8 命名纪律「复制 <对象描述>」）+ Space 键盘
//      激活 + 剪贴板拿到完整 key（展示可截断、拷贝不许截断——§7.3）+
//      回显断言（✓ 字形翻转 = CopyButton done 态）+ 不触发行导航。
//   ② Set Me Up 直开：行内入口开的是**同一只 T-382 抽屉**——以抽屉自己的
//      规范锚族（smu-dialog / smu-repo / smu-tab-{configure,deploy,resolve}）
//      为证，零重复实现（不存在 repos-smu-* 之类的平行锚）+ URL 不离列表。
//   ③ E1 不倒退（缺席断言）：行尾无 ⋮ 菜单形态（aria-haspopup=0——T-385
//      撤旗 + T-400 precision 注记的注册口径）+ 行内删除唯一形态 = admin
//      文本钮且只开强确认（点击后行仍在 + 取消腿 + API 200）；readonly
//      视角行内零删除动作。parity §9 E1：删除类动作不进列表行内/不做一键删。
//   ④ a11y：aria-label 逐钮断言 + 键盘可达（Tab/焦点 + Space 激活——
//      WAI-ARIA button 标准双激活键之一）+ axe 双主题 serious=0。
//
// 键盘注记（在册观察，非本票修——共享件超 area）：行 onKeyDown
// （onTableRowKeys）不判 e.target，焦点在行内按钮上按 **Enter** 会被劫持
// 为行导航（preventDefault 同时吞掉按钮原生 click）；Space 不受影响（行
// 处理器只拦 Enter/↑↓）。WCAG 2.1.1 不破（Space 可激活），本 spec 以
// Space 为键盘激活键；Enter 劫持若要收口应改 web/src/lib/keys.ts（六页
// 共享件，单独提票）。
//
// 锚源：全部为既有注册锚（console-ux §10.5——repos-row-* / repos-setmeup-*
// / repos-delete-* / smu-* / confirm-*），本票零新锚零改名；`.copy-btn`
// 是 CopyButton 保留的 spec 类钩子（组件注释在案，repositories.spec:104
// 同款）。夹具自备自清（uniq key + deleteContent 收尾）——破坏性动作纪律
// 见 web/e2e/README §3。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 备一个 local generic 仓（API 直备——SUT 是列表行内动作，不是建仓表单） */
async function seedRepo(key: string): Promise<void> {
  const res = await m8Client().request('PUT', `/binflow/api/repositories/${key}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  expect(res.status).toBeLessThan(300)
}

// ---- 1. 复制 key：aria-label + Space 激活 + 剪贴板全值 + ✓ 回显 --------------

test('admin: row copy-key — aria-label, Space activates, clipboard gets full key, check echo, no row nav', async ({
  page,
}) => {
  const key = uniq('t424cp')
  await seedRepo(key)

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  const row = page.locator(`[data-testid="repos-row-${key}"]`)
  await expect(row).toBeVisible({ timeout: 30_000 })

  const copyBtn = row.locator('.copy-btn')
  await expect(copyBtn).toHaveCount(1)
  // a11y：可访问名走 §8 命名纪律（「复制 <对象描述>」——e2e 定位契约）
  await expect(copyBtn).toHaveAttribute('aria-label', `复制 仓库 key ${key}`)

  // 键盘可达：焦点可进入（Tab 序内原生 button）+ Space 激活（标准双激活键
  // 之一；Enter 见文件头在册注记）。剪贴板权限先授（headless chromium 的
  // readText 需要 clipboard-read——repositories.spec:103 同款）
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await copyBtn.focus()
  await expect(copyBtn).toBeFocused()
  await page.keyboard.press('Space')

  // 剪贴板断言：完整 key（mono 值 = 拷贝候选；展示可截断、拷贝不截断）
  const clip = await page.evaluate(() => navigator.clipboard.readText())
  expect(clip).toBe(key)

  // 回显断言：CopyButton done 态——字形 ⧉ → ✓（1.5s TTL 内采样；.mono 是
  // 字形 span 的专属类，不与 MUI ripple span 相撞）
  await expect(copyBtn.locator('span.mono')).toHaveText('✓')

  // 隔离层回归：激活不触发行导航（review B1——点击/键盘两路径都不换页）
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)
  await expect(row).toBeVisible()

  // 收尾
  await m8Client().request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
})

// ---- 2. Set Me Up 直开：抽屉复用（smu-* 规范锚族）+ Esc 回焦 -----------------

test('admin: row Set Me Up direct-open — same T-382 drawer (smu-* anchors), preselected, Esc + focus return', async ({
  page,
}) => {
  const key = uniq('t424smu')
  await seedRepo(key)

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  const row = page.locator(`[data-testid="repos-row-${key}"]`)
  await expect(row).toBeVisible({ timeout: 30_000 })

  // 入口是文本钮（可访问名 = 文本「Set Me Up」）——键盘腿：focus + Space
  const launch = row.locator('[data-testid^="repos-setmeup-"]')
  await expect(launch).toContainText('Set Me Up')
  await launch.focus()
  await expect(launch).toBeFocused()
  await page.keyboard.press('Space')

  // 抽屉复用的证明：开出来的是 smu-* 规范锚族本体（与 AppShell 全局入口 /
  // 详情头入口 / 制品页入口同一实现），预选仓直接落主对话框——不存在为
  // 行内入口另造的平行实现锚。
  const drawer = page.locator('[data-testid="smu-dialog"]')
  await expect(drawer).toBeVisible()
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)
  await expect(page.locator('[data-testid="smu-tab-configure"]')).toBeVisible()
  await expect(page.locator('[data-testid="smu-tab-deploy"]')).toBeVisible()
  await expect(page.locator('[data-testid="smu-tab-resolve"]')).toBeVisible()

  // 直开不换页：URL 仍钉在列表 Tab（行点击才是导航）
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)

  // 抽屉族通用规格：Esc 关闭 + 回焦启动元素（L02）
  await page.keyboard.press('Escape')
  await expect(drawer).toHaveCount(0)
  await expect(launch).toBeFocused()

  // 收尾
  await m8Client().request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
})

// ---- 3. E1 不倒退（admin）：无 ⋮ 菜单形态；删除只走强确认 --------------------

test('admin: E1 no-regression — no row-end action menu; delete affordance only opens strong confirm', async ({
  page,
}) => {
  const key = uniq('t424e1')
  await seedRepo(key)

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  const row = page.locator(`[data-testid="repos-row-${key}"]`)
  await expect(row).toBeVisible({ timeout: 30_000 })

  // 缺席断言①：行内无 ⋮ 动作菜单形态（aria-haspopup=0——T-385 撤旗后
  // T-400 注册口径：MoreVert 全树 grep=0；Artifactory 行尾亦无此形态）
  await expect(row.locator('button[aria-haspopup]')).toHaveCount(0)

  // 行内删除唯一形态 = admin 文本钮（可发现性优于 icon-only 直删——E1
  // 家族注册形态；aria-label 在身）+ 无任何其它删除动作位
  const del = row.locator('[data-testid^="repos-delete-"]')
  await expect(del).toContainText('删除')
  await expect(del).toHaveAttribute('aria-label', `删除仓库 ${key}`)
  await expect(row.locator('[aria-label*="删除"]')).toHaveCount(1)

  // 危险区确认不倒退：点击只开强确认（输入 key + deleteContent 两段流），
  // 仓未被删（行仍在 + API 200）；取消腿安全退出
  await del.click()
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="confirm-dialog"]')).toContainText('没有撤销')
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeDisabled()
  await expect(row).toBeVisible()
  await page.click('[data-testid="confirm-cancel"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toHaveCount(0)
  await expect(row).toBeVisible()
  expect((await sessionApi(page, 'GET', `/api/repositories/${key}`)).status).toBe(200)

  // 收尾
  await m8Client().request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
})

// ---- 4. E1 不倒退（readonly）：L2 快捷在（读面），行内零删除动作 --------------

test('readonly_admin: L2 quick actions present (read-plane), zero delete affordance in row', async ({ page }) => {
  const key = uniq('t424ro')
  await seedRepo(key)

  await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  const row = page.locator(`[data-testid="repos-row-${key}"]`)
  await expect(row).toBeVisible({ timeout: 30_000 })

  // L2 快捷是读面动作：readonly 可用（复制 key / Set Me Up 均不写服务端）
  const copyBtn = row.locator('.copy-btn')
  await expect(copyBtn).toHaveCount(1)
  await expect(copyBtn).toHaveAttribute('aria-label', `复制 仓库 key ${key}`)
  await expect(row.locator('[data-testid^="repos-setmeup-"]')).toBeVisible()

  // 缺席断言：readonly 行内零删除动作 + 无 ⋮ 菜单形态（CapRepoWrite 门，
  // L4 预收敛——服务端 403 兜底在 m8/repositories-admin 腿）
  await expect(row.locator('[data-testid^="repos-delete-"]')).toHaveCount(0)
  await expect(row.locator('button[aria-haspopup]')).toHaveCount(0)

  // 收尾（admin 面）
  await m8Client().request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
})

// ---- 5. axe 双主题：行内动作在场的列表页 serious=0 -----------------------------

test('axe: repositories list with inline actions clean in both themes', async ({ page }, testInfo: TestInfo) => {
  test.setTimeout(300_000)
  const key = uniq('t424ax')
  await seedRepo(key)

  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/repositories/local')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toBeVisible({ timeout: 30_000 })
    await expectA11yClean(page, testInfo)
  }

  // 收尾
  await m8Client().request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
})
