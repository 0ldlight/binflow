import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, seedRepos, sessionApi } from '../m8/support/seed'

// T-443（M16 批次② FE②-c，FR-143.4/.5——列表列集 + 入口分路由 +
// dirty-gating + remote Test 三臂消费）：
//
//   ① 入口分路由（B-3.8 翻正收口）：列表入口自平钮翻 **Create a Repository
//      下拉三预选**（Local/Remote/Virtual 各带一句描述——Artifactory
//      7.161.20 实测 el-dropdown 形态，m16-baseline-refresh §A3-1 / 证据
//      s3e-create-dropdown：Local "Upload and resolve your own packages"…
//      BinFlow 三型实有口径，federated/release-bundle 不伪造）；选中即分
//      路由深链 /admin/repositories/<rclass>/new（7.161 同构）；**表单内
//      rclass 控件移除**（form-rclass-* 三锚退役）；旧 /new 与 /new?rclass=
//      直链经路由表兼容映射（7 处跨页 emitter 零改动）；非法段落 404。
//   ② 列表列集对齐（B-3.9/Q9）：冗余「类型」列收敛（三 Tab 子路由即类型
//      ——列信息量为零；repos-columns-item-type 锚退役）；Replications 列
//      自 T-404 的仅 local 扩 **local + remote 两 Tab**（push-only 口径——
//      ADR-0021/parity §6A R10：BinFlow 无 pull 复制，remote 页签如实呈现
//      以该仓为源的 push 配置）；Project 列缺位登记不伪造（负断言）；
//      行操作超集维持已豁免（L2/E1 零倒退——Set Me Up/部署/删除三入口）。
//   ③ dirty-gating：进入编辑 Save disabled → 变更 enabled → 改回原值
//      再 disabled（无变更提交不可达——deep-equal 基线比较）；零写请求。
//   ④ remote Test 三臂（FR-143.5，消费 T-442 端点 POST /api/repositories/
//      {key}/test）：正确凭据成功（自指上游 /healthz）→ ok:true 绿；
//      凭据被拒（上游 401）→ 内联失败红（message 原文）；不可达（死端口
//      连接拒绝）→ status_code 0 呈现；草稿臂（表单改 url 未填密码 =
//      匿名探测）；零副作用（探测期零配置写请求 + GET 配置不变）。
//   ⑤ axe 双主题：列表页 + 下拉开态 + remote 编辑态（Test 成功结果在场）。
//
// 夹具纪律：Playwright 栈自建（API PUT/seed + uniq key + 收尾删除），零
// 外部状态；锚源 = console-ux §10.5 T-443 批（repos-create-{menu,<rclass>} /
// form-rclass-note / form-test / form-test-result / form-test-create-note，
// v1.35）。

const BASE = process.env.BASE ?? 'http://127.0.0.1:8080'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 编辑表单直达（Basic 步）；等 GET 回显预填收敛（dirty 基线就位）。 */
async function gotoEdit(page: Page, key: string, query = ''): Promise<void> {
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit${query}`)
  await expect(page.locator('[data-testid="repo-form-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-section-general"]')).toBeVisible()
}

// ---------------------------------------------------------------------------
// ① 入口下拉三预选 + 分路由深链 + /new 兼容映射 + rclass 控件移除 ------------
// ---------------------------------------------------------------------------

test('entry: three-preset dropdown routes to split paths; /new compat maps; in-form rclass control removed', async ({
  page,
}) => {
  await loginAs(page, 'admin')

  // 入口形态：下拉三预选（触发钮 repos-create 不变——锚沿 T-99）+ 三菜单项
  // 各带一句描述（7.161 形态：型名 + 一句描述行）
  await page.goto('/binflow/ui/admin/repositories/local')
  await page.click('[data-testid="repos-create"]')
  const menu = page.locator('[data-testid="repos-create-menu"]')
  await expect(menu).toBeVisible()
  for (const rc of ['local', 'remote', 'virtual'] as const) {
    const item = page.locator(`[data-testid="repos-create-${rc}"]`)
    await expect(item).toBeVisible()
    await expect(item).toContainText(rc === 'local' ? '上传' : rc === 'remote' ? '代理' : '聚合')
  }

  // 选中 Remote → 分路由深链（包类型网格随页即开）
  await page.click('[data-testid="repos-create-remote"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/remote\/new$/)
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await expect(page.locator('[data-testid="pkg-grid"]')).toContainText('Remote')
  // 网格取消 = 回对应 Tab（rclass 预选感知退出——既有语义随路由不变）
  await page.click('[data-testid="pkg-grid-cancel"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/remote$/)

  // 三分路由皆直达 + 表单内 rclass 控件移除（B-3.8）：单选组三锚退役，
  // 常规节以说明行承载仓型语境（form-rclass-note）
  for (const [rc, word] of [
    ['local', 'Local'],
    ['virtual', 'Virtual'],
  ] as const) {
    await page.goto(`/binflow/ui/admin/repositories/${rc}/new`)
    await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
    await page.click('[data-testid="pkg-grid-item-generic"]')
    for (const r of ['local', 'remote', 'virtual'] as const) {
      await expect(page.locator(`[data-testid="form-rclass-${r}"]`)).toHaveCount(0)
    }
    await expect(page.locator('[data-testid="form-rclass-note"]')).toContainText(word)
    await expect(page.locator('[data-testid="form-rclass-note"]')).toContainText('不可更改')
  }

  // /new 直链兼容映射：缺省 → local；?rclass= → 对应分路由（replace）
  await page.goto('/binflow/ui/admin/repositories/new')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local\/new$/)
  await page.goto('/binflow/ui/admin/repositories/new?rclass=virtual')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/virtual\/new$/)

  // 非法段不落建仓表单：静态三路由外落 404（与 Artifactory 未知 rclass 同姿）
  await page.goto('/binflow/ui/admin/repositories/federated/new')
  await expect(page.locator('[data-testid="not-found-path"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ② 列表列集：类型列收敛 + Replications 两 Tab + Project 缺位 + 行操作超集 --
// ---------------------------------------------------------------------------

test('columns: redundant type column collapsed; Replications on local+remote tabs (push-only); Project absent; row-action superset intact', async ({
  page,
}) => {
  const client = m8Client()
  const local = uniq('t443l')
  const remote = uniq('t443r')
  const virtual = uniq('t443v')
  await client.request('PUT', `/binflow/api/repositories/${local}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  await client.request('PUT', `/binflow/api/repositories/${remote}`, {
    body: { rclass: 'remote', packageType: 'maven', url: 'https://repo1.maven.org/maven2' },
  })
  await client.request('PUT', `/binflow/api/repositories/${virtual}`, {
    body: { rclass: 'virtual', packageType: 'generic', repositories: [local] },
  })

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator(`[data-testid="repos-row-${local}"]`)).toBeVisible({ timeout: 30_000 })

  // 类型列收敛（Q9）：7 列闭集、无「类型」表头；列选菜单项同步退役；
  // Project 列缺位负断言（不伪造——B-3.9/§9A-S8）
  const th = page.locator('[data-testid="repos-table"] thead th')
  await expect(th).toHaveCount(7)
  // 「类型」列收敛：精确整格匹配（「包类型」列含「类型」子串——子串负断言会假红）
  await expect(th.filter({ hasText: /^类型$/ })).toHaveCount(0)
  await expect(th.filter({ hasText: /^Project$/ })).toHaveCount(0)
  await page.click('[data-testid="repos-columns"]')
  // 列选项「类型」退役（文案级负断言——锚已随列退役，不以退役锚反断言）
  await expect(
    page.locator('[data-testid="repos-columns-menu"]').getByText(/^类型$/, { exact: true }),
  ).toHaveCount(0)
  await page.keyboard.press('Escape')

  // 行操作超集维持（L2/E1 豁免零倒退）：Set Me Up / 部署（local × generic）/
  // 删除三入口在场
  await expect(page.locator(`[data-testid="repos-setmeup-${local}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="repos-deploy-${local}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="repos-delete-${local}"]`)).toBeVisible()

  // Replications 列在 local Tab（T-404 既有）——表头 + 0 cell
  await expect(th.filter({ hasText: 'Replications' })).toHaveCount(1)
  await expect(page.locator(`[data-testid="repos-repl-${local}"]`)).toHaveText('0')

  // Remote Tab：Replications 列**新增**（T-443/B-3.9）——表头带 push-only
  // 口径注记（ADR-0021/R10：无 pull 复制，呈现以该仓为源的 push 配置）
  await page.click('[data-testid="repos-tab-remote"]')
  await expect(page.locator(`[data-testid="repos-row-${remote}"]`)).toBeVisible({ timeout: 30_000 })
  const thRemote = page.locator('[data-testid="repos-table"] thead th')
  const replHead = thRemote.filter({ hasText: 'Replications' })
  await expect(replHead).toHaveCount(1)
  await expect(replHead).toHaveAttribute('title', /push 复制配置.*无 pull 复制/)
  await expect(page.locator(`[data-testid="repos-repl-${remote}"]`)).toHaveText('0')

  // Virtual Tab：无 Replications 列（push 源聚合仓无对位语义）
  await page.click('[data-testid="repos-tab-virtual"]')
  await expect(page.locator(`[data-testid="repos-row-${virtual}"]`)).toBeVisible({ timeout: 30_000 })
  await expect(page.locator('[data-testid="repos-table"] thead th').filter({ hasText: 'Replications' })).toHaveCount(0)

  // 收尾
  for (const k of [virtual, remote, local]) {
    await client.request('DELETE', `/binflow/api/repositories/${k}`)
  }
})

// ---------------------------------------------------------------------------
// ③ dirty-gating：进入编辑 disabled → 变更 enabled → 改回 disabled ------------
// ---------------------------------------------------------------------------

test('dirty-gating: edit enters with Save disabled; change enables; reverting re-disables; no clean submit', async ({
  page,
}) => {
  const key = uniq('t443d')
  await seedRepos(m8Client(), [{ key }])

  await loginAs(page, 'admin')
  const writes: string[] = []
  page.on('request', (r) => {
    if (/\/binflow\/api\/repositories\//.test(r.url()) && (r.method() === 'PUT' || r.method() === 'POST')) {
      writes.push(`${r.method()} ${r.url()}`)
    }
  })

  // 进入编辑：Save disabled（dirty 基线 = GET 回显预填；deep-equal 零差异）
  await gotoEdit(page, key)
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-submit"]')).toHaveAttribute(
    'title',
    '尚未修改任何字段——无变更不可提交（改字段后启用）。',
  )

  // 变更 → enabled；改回原值（描述还原）→ 再 disabled（无变更提交不可达）
  await page.fill('[data-testid="form-description"]', 'changed by t443')
  await expect(page.locator('[data-testid="form-submit"]')).toBeEnabled()
  await page.fill('[data-testid="form-description"]', 'm8 e2e fixture (T-232)')
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()

  // 密码域：基线恒空（NFR-S14 不回显）——输入即 dirty
  await page.goto('/binflow/ui/admin/repositories/local') // 先离开（remote 域字段不在 local 表单）
  const rkey = uniq('t443dr')
  await m8Client().request('PUT', `/binflow/api/repositories/${rkey}`, {
    body: { rclass: 'remote', packageType: 'generic', url: 'https://repo1.maven.org/maven2' },
  })
  await gotoEdit(page, rkey)
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="form-password"]', 'typed-makes-dirty')
  await expect(page.locator('[data-testid="form-submit"]')).toBeEnabled()

  // 干净期零写请求（disabled 钮不可达 + 未触发任何提交）
  await page.waitForTimeout(300)
  expect(writes).toHaveLength(0)

  for (const k of [rkey, key]) {
    await m8Client().request('DELETE', `/binflow/api/repositories/${k}`)
  }
})

// ---------------------------------------------------------------------------
// ④ remote Test 三臂（T-442 端点消费）+ 草稿臂 + 零副作用 --------------------
// ---------------------------------------------------------------------------

test('remote test: three arms inline (ok / creds-refused / unreachable), draft override, zero side effects', async ({
  page,
}) => {
  const client = m8Client()
  // 三臂夹具（全部自备 + 自指/本机上游——INC-1 纪律：只打自备实体）：
  //  - ok：自指 /healthz（匿名 200——正确凭据=无需凭据的同源姿）
  //  - 拒：上游 401（管理面匿名 401——凭据被拒族）
  //  - 死：连接拒绝（127.0.0.1:1 —— 传输层不可达，status_code 0）
  const okKey = uniq('t443ok')
  const authKey = uniq('t443auth')
  await client.request('PUT', `/binflow/api/repositories/${okKey}`, {
    body: { rclass: 'remote', packageType: 'generic', url: `${BASE}/healthz`, allowPrivateUpstream: true },
  })
  await client.request('PUT', `/binflow/api/repositories/${authKey}`, {
    body: { rclass: 'remote', packageType: 'generic', url: `${BASE}/binflow/api/repositories`, allowPrivateUpstream: true },
  })

  await loginAs(page, 'admin')
  const configWrites: string[] = []
  page.on('request', (r) => {
    if (
      /\/binflow\/api\/repositories\/[^/]+$/.test(r.url()) &&
      (r.method() === 'PUT' || r.method() === 'POST')
    ) {
      configWrites.push(`${r.method()} ${r.url()}`)
    }
  })

  // 臂① 正确凭据成功：编辑态 Basic 步 Test → 绿内联（message 原文 + 状态码）
  await gotoEdit(page, okKey)
  await expect(page.locator('[data-testid="form-test"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-test"]')).toBeEnabled()
  await page.click('[data-testid="form-test"]')
  const result = page.locator('[data-testid="form-test-result"]')
  await expect(result).toBeVisible()
  await expect(result).toContainText('tested successfully')
  await expect(result).toContainText('上游可达且凭据被接受')
  await expect(result).toContainText('上游应答 HTTP 200')

  // 臂② 草稿臂（改 url 未填密码 = 匿名探测死端口）：内联失败红 + 未触达
  await page.fill('[data-testid="form-url"]', 'http://127.0.0.1:1/x')
  await page.click('[data-testid="form-test"]')
  await expect(result).toContainText('connection failed')
  await expect(result).toContainText('未触达上游')

  // 臂③ 凭据被拒（上游 401）：内联失败红（message 原文含 401）
  await gotoEdit(page, authKey)
  await page.click('[data-testid="form-test"]')
  const result2 = page.locator('[data-testid="form-test-result"]')
  await expect(result2).toBeVisible()
  await expect(result2).toContainText('Connection failed')
  await expect(result2).toContainText('401')
  await expect(result2).toContainText('探测未通过')

  // 零副作用：三臂探测期零配置写请求（只读探测的构造性证明——UI 消费面
  // 对账）+ GET 配置原样（草稿 url 绝不落盘）
  await page.waitForTimeout(300)
  expect(configWrites).toHaveLength(0)
  const got = await sessionApi(page, 'GET', `/api/repositories/${okKey}`)
  expect(got.status).toBe(200)
  expect((got.json as { configuration?: { url?: string } }).configuration?.url).toBe(`${BASE}/healthz`)

  // 建仓态无 Test（端点按已存 key 寻址——仓不存在则 404）：按钮不呈现，
  // 以 hint 如实说明（不造死按钮）
  await page.goto('/binflow/ui/admin/repositories/remote/new')
  await page.click('[data-testid="pkg-grid-item-generic"]')
  await expect(page.locator('[data-testid="form-test"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="form-test-create-note"]')).toBeVisible()

  for (const k of [authKey, okKey]) {
    await client.request('DELETE', `/binflow/api/repositories/${k}`)
  }
})

// ---------------------------------------------------------------------------
// ⑤ axe 双主题：列表 + 下拉开态 + remote 编辑态（Test 成功结果在场） ----------
// ---------------------------------------------------------------------------

test('axe: list page, create dropdown open, remote edit with test result — clean in both themes', async ({
  page,
}, testInfo: TestInfo) => {
  test.setTimeout(300_000) // 3 面 × 双主题；axe 在默认并发下实测 30s+/面
  const client = m8Client()
  const okKey = uniq('t443ax')
  await client.request('PUT', `/binflow/api/repositories/${okKey}`, {
    body: { rclass: 'remote', packageType: 'generic', url: `${BASE}/healthz`, allowPrivateUpstream: true },
  })

  await loginAs(page, 'admin')
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)

    // 列表页（类型列收敛后的 7 列表 + 工具栏）
    await page.goto('/binflow/ui/admin/repositories/local')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="repos-page"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repos-page"]' })

    // 入口下拉开态（Menu 门户挂 body——include 走菜单锚）
    await page.click('[data-testid="repos-create"]')
    await expect(page.locator('[data-testid="repos-create-menu"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repos-create-menu"]' })
    await page.keyboard.press('Escape')

    // remote 编辑态：Test 成功结果在场（内联 Alert 的双主题对比度义务面）
    await gotoEdit(page, okKey)
    await page.click('[data-testid="form-test"]')
    await expect(page.locator('[data-testid="form-test-result"]')).toContainText('tested successfully')
    await expectA11yClean(page, testInfo, { include: '[data-testid="repo-form-page"]' })
  }

  await client.request('DELETE', `/binflow/api/repositories/${okKey}`)
})
