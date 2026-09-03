import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, seedRepos } from '../m8/support/seed'

// T-404（M14 FR-131，T-402②包 A）——复制 CRUD 内嵌表单（R1 裁定形态）：
// 仓库编辑页 Replications 节（form-section-replications，**内嵌节无
// modal/drawer**，R9）+ 仓 Tab 指针升级 + 仓列表 Replications 列/行级 Run
// 三件套。真栈腿 + 拦路对账（网络层 payload 净度）+ 受控 mock 腿（降级态、
// PUT 启停的 T-405 合并前形态）。列表臂的 Run 动作自 T-420（M15 FR-138.1）
// 起为真触发（POST /v1/replications/{id}/run——占位深链语义退役）。
//
// 断言面（AC1/AC2）：
//   ① 创建臂：空态 → 内嵌表单 → POST 落库（API 对账）+ **payload 净度**
//      （预留字段族零提交——cronExp/pathPrefix/sync 三开关不在 body，R3
//      勘误的「不伪造语义」网络层实证）+ 预留位控件恒禁用。
//   ② 编辑臂：重建语义（DELETE+POST——REST 无字段级 PUT 的票内定案）：
//      恰一条 DELETE + 一条 POST、名称不变、字段更新、无重复行。
//   ③ 删除臂：E1 输入 name 档（错名不动 / 对名放行）+ 取消腿 + API 对账。
//   ④ 启停臂（T-405 联合）：mock 200 腿钉死成功路径（PUT {id} + body
//      {enabled} + 行内翻转）；真栈腿按响应分流——合并前 404 = toast
//      如实呈现 + 行内不乐观更新（两条形态都合法，合并后自然走 200 腿）。
//   ⑤ 只读臂：readonly_admin——节只读呈现（开关禁用、写入口不渲染、
//      Tab 无编辑深链）。
//   ⑥ 列表臂：Replications 列（0 = 纯文本「0」；已配置 = Run 图标——T-420
//      起为真触发：toast 排程回报 + 「查看任务」深链全局复制页）+
//      remote Tab 无该列。
//   ⑦ 降级臂（受控 mock）：501 → repl-degraded；403 → repl-denied。
//   ⑧ axe 双主题：节 + 表单开态。
//
// 锚源：console-ux §10.5 T-404 批（v1.25——repl-* 配置面族 + repo-repl-*
// 指针族 + repos-repl-* 列族 + form-section-replications 第七节）。
//
// T-439 步进翻新：本节载体自「内嵌第七节」迁表单第三步（FR-143.1——
// Basic|Advanced|Replications 步进条；M6 能力语义零变化）。本 spec 全部
// /edit 导航改走 ?section=replications 深链（步进第三步直落——既有深链
// 落点语义不变，且每腿顺带钉死深链 → 第三步分派）；锚与断言面零改名。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 管理面 REST 直建一条复制配置（真栈，admin Basic） */
async function seedConfig(repoKey: string, name: string, over: Record<string, unknown> = {}) {
  return m8Client().request('POST', '/binflow/api/v1/replications', {
    body: {
      name,
      source_repo: repoKey,
      target_url: 'https://dr.example.com',
      target_repo: `${repoKey}-dr`,
      target_username: '',
      target_password: '',
      max_bandwidth_bytes_per_sec: 0,
      max_items_per_push: 0,
      enabled: true,
      ...over,
    },
  })
}

/** GET /api/v1/replications 的页内会话读（admin 会话；返回数组） */
async function listConfigs(page: Page): Promise<Array<Record<string, unknown>>> {
  const res = await page.evaluate(async () => {
    const r = await fetch('/binflow/api/v1/replications', { headers: { Accept: 'application/json' } })
    return { status: r.status, body: await r.text() }
  })
  expect(res.status, `list replications: ${res.body}`).toBe(200)
  return JSON.parse(res.body) as Array<Record<string, unknown>>
}

async function configsOf(page: Page, repoKey: string, name?: string) {
  const all = await listConfigs(page)
  return all.filter((c) => c.source_repo === repoKey && (!name || c.name === name))
}

// ---- 1. 创建臂：内嵌表单 → POST 落库 + payload 净度 + 预留位 ------------------

test('admin: inline create — form posts the wire set only (reserved fields never submitted)', async ({
  page,
}) => {
  const key = uniq('t404a')
  const name = uniq('t404cfg').toLowerCase()
  await seedRepos(m8Client(), [{ key }])

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
  const section = page.locator('[data-testid="form-section-replications"]')
  await expect(section).toBeVisible()
  // R1 形态钉死：内嵌节（无 dialog/drawer role——整页分区）
  await expect(page.locator('[data-testid="repo-form-page"] [role="dialog"]')).toHaveCount(0)
  // 空态 → 新建入口
  await expect(page.locator('[data-testid="repl-empty"]')).toBeVisible()
  await page.click('[data-testid="repl-create"]')

  const form = page.locator('[data-testid="repl-form"]')
  await expect(form).toBeVisible()
  // 预留位组（R3 勘误）：在场 + 恒禁用 + 「预留位」如实标注
  const reserved = page.locator('[data-testid="repl-form-reserved"]')
  await expect(reserved).toBeVisible()
  await expect(reserved).toContainText('预留位')
  await expect(page.locator('[data-testid="repl-form-cron"]')).toBeDisabled()
  await expect(page.locator('[data-testid="repl-form-event"]')).toBeDisabled()
  await expect(page.locator('[data-testid="repl-form-prefix"]')).toBeDisabled()
  await expect(page.locator('[data-testid="repl-form-syncDeletes"]')).toBeDisabled()
  await expect(page.locator('[data-testid="repl-form-syncProperties"]')).toBeDisabled()
  await expect(page.locator('[data-testid="repl-form-syncStatistics"]')).toBeDisabled()

  // 必填门：空表单不可提交；name 非法（闭集外字符）行内预检
  await expect(page.locator('[data-testid="repl-form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="repl-form-name"]', 'bad name!')
  await expect(page.locator('[data-testid="repl-form-name-error"]')).toBeVisible()
  await page.fill('[data-testid="repl-form-name"]', name)
  await page.fill('[data-testid="repl-form-url"]', 'https://dr.example.com')
  await page.fill('[data-testid="repl-form-target-repo"]', `${key}-dr`)
  await page.fill('[data-testid="repl-form-username"]', 'uploader')
  await expect(page.locator('[data-testid="repl-form-bandwidth"]')).toHaveValue('')
  await page.fill('[data-testid="repl-form-bandwidth"]', '1048576')
  await page.fill('[data-testid="repl-form-items"]', '500')
  // enabled 默认勾选（服务端缺省 true 的表单镜像）
  await expect(page.locator('[data-testid="repl-form-enabled"]')).toBeChecked()
  await expect(page.locator('[data-testid="repl-form-submit"]')).toBeEnabled()

  // payload 净度（网络层对账）：POST body 键集 = wire 闭集——预留字段零提交
  const posted = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().includes('/api/v1/replications'),
  )
  await page.click('[data-testid="repl-form-submit"]')
  const req = await posted
  const body = JSON.parse(req.postData() ?? '{}') as Record<string, unknown>
  expect(Object.keys(body).sort()).toEqual(
    [
      'enabled',
      'max_bandwidth_bytes_per_sec',
      'max_items_per_push',
      'name',
      'source_repo',
      'target_password',
      'target_repo',
      'target_url',
      'target_username',
    ].sort(),
  )
  expect(body.source_repo).toBe(key)
  expect(body.enabled).toBe(true)
  expect(body.target_username).toBe('uploader')

  // 行到达 + API 对账
  await expect(page.locator(`[data-testid="repl-row-${name}"]`)).toBeVisible()
  const rows = await configsOf(page, key, name)
  expect(rows).toHaveLength(1)
  expect(rows[0].target_repo).toBe(`${key}-dr`)
  expect(rows[0].enabled).toBe(true)
  expect(rows[0].max_bandwidth_bytes_per_sec).toBe(1048576)

  // 重名 409 终裁 → 行内错误（服务端文案原样）；取消收表单
  await page.click('[data-testid="repl-create"]')
  await page.fill('[data-testid="repl-form-name"]', name)
  await page.fill('[data-testid="repl-form-url"]', 'https://dr2.example.com')
  await page.fill('[data-testid="repl-form-target-repo"]', `${key}-dr2`)
  await page.click('[data-testid="repl-form-submit"]')
  await expect(page.locator('[data-testid="repl-form-error"]')).toBeVisible()
  await expect(page.locator('[data-testid="repl-form-error"]')).toContainText(name)
  await page.click('[data-testid="repl-form-cancel"]')
  await expect(page.locator('[data-testid="repl-form"]')).toHaveCount(0)
})

// ---- 2. 编辑臂：重建语义（DELETE+POST 恰一对，名称不变、无重复行） ----------

test('admin: edit = delete + recreate (no field-level PUT) — exactly one pair, name kept', async ({
  page,
}) => {
  const key = uniq('t404b')
  const name = uniq('t404edit').toLowerCase()
  await seedRepos(m8Client(), [{ key }])
  await seedConfig(key, name, { target_repo: 'old-target' })

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
  await expect(page.locator(`[data-testid="repl-row-${name}"]`)).toContainText('old-target')

  await page.click(`[data-testid="repl-edit-${name}"]`)
  const form = page.locator('[data-testid="repl-form"]')
  await expect(form).toBeVisible()
  // 重建语义警示在场（票内定案留痕面）
  await expect(page.locator('[data-testid="repl-recreate-note"]')).toBeVisible()
  // 预填回显（密码除外——永不回显）
  await expect(page.locator('[data-testid="repl-form-url"]')).toHaveValue('https://dr.example.com')
  await expect(page.locator('[data-testid="repl-form-target-repo"]')).toHaveValue('old-target')
  await expect(page.locator('[data-testid="repl-form-password"]')).toHaveValue('')
  // 编辑态 name 锁定（无输入位）
  await expect(page.locator('[data-testid="repl-form-name"]')).toHaveCount(0)

  await page.fill('[data-testid="repl-form-target-repo"]', 'new-target')
  const del = page.waitForRequest(
    (r) => r.method() === 'DELETE' && r.url().endsWith(`/api/v1/replications/${name}`),
  )
  const post = page.waitForRequest((r) => r.method() === 'POST' && r.url().includes('/api/v1/replications'))
  await page.click('[data-testid="repl-form-submit"]')
  await del
  const req = await post
  expect((JSON.parse(req.postData() ?? '{}') as Record<string, unknown>).name).toBe(name)

  await expect(page.locator(`[data-testid="repl-row-${name}"]`)).toContainText('new-target')
  // API 对账：恰一条、名称不变、字段更新
  const rows = await configsOf(page, key)
  expect(rows).toHaveLength(1)
  expect(rows[0].name).toBe(name)
  expect(rows[0].target_repo).toBe('new-target')
})

// ---- 3. 删除臂：E1 输入 name 档（错名不动 / 对名放行 / 取消腿） --------------

test('admin: delete requires typing the config name (E1) — wrong name refused, cancel keeps', async ({
  page,
}) => {
  const key = uniq('t404c')
  const name = uniq('t404del').toLowerCase()
  await seedRepos(m8Client(), [{ key }])
  await seedConfig(key, name)

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)

  // 取消腿：Esc/取消离开对话框，行保留
  await page.click(`[data-testid="repl-delete-${name}"]`)
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeDisabled()
  await page.fill('[data-testid="repl-delete-confirm-name"]', `${name}-typo`)
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeDisabled()
  await page.click('[data-testid="confirm-cancel"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toHaveCount(0)
  await expect(page.locator(`[data-testid="repl-row-${name}"]`)).toBeVisible()

  // 对名放行：DELETE 落库 + 行消失 + API 对账
  await page.click(`[data-testid="repl-delete-${name}"]`)
  await page.fill('[data-testid="repl-delete-confirm-name"]', name)
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeEnabled()
  const gone = page.waitForRequest(
    (r) => r.method() === 'DELETE' && r.url().endsWith(`/api/v1/replications/${name}`),
  )
  await page.click('[data-testid="confirm-accept"]')
  await gone
  await expect(page.locator(`[data-testid="repl-row-${name}"]`)).toHaveCount(0)
  await expect(page.locator('[data-testid="repl-empty"]')).toBeVisible()
  expect(await configsOf(page, key)).toHaveLength(0)
})

// ---- 4. 启停臂（T-405 联合）：mock 200 钉死成功路径 + 真栈分流腿 --------------

test('toggle: PUT /v1/replications/{id} with {enabled} — mocked 200 flips; live 404 keeps value + toast', async ({
  page,
}) => {
  const key = uniq('t404d')
  const name = uniq('t404tog').toLowerCase()
  await seedRepos(m8Client(), [{ key }])
  const created = await seedConfig(key, name, { enabled: true })
  const id = (JSON.parse(created.text) as Record<string, unknown>).id as number

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
  const toggle = page.locator(`[data-testid="repl-toggle-${name}"]`)
  await expect(toggle).toBeChecked()

  // 成功路径（mock = T-405 合并后的端到端形态假定：PUT 200 回更新后的配置行，
  // 后续列表 GET 回翻转后的真值——组件成功后重取列表，只拦 PUT 看不到翻转）
  const flipped = { id, name, source_repo: key, target_url: 'https://dr.example.com', target_repo: `${key}-dr`, target_username: '', max_bandwidth_bytes_per_sec: 0, max_items_per_push: 1000, enabled: false, created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z' }
  await page.route(`**/api/v1/replications/${id}`, async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback()
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(flipped) })
  })
  await page.route('**/api/v1/replications', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([flipped]) })
  })
  const put = page.waitForRequest((r) => r.method() === 'PUT' && r.url().includes(`/api/v1/replications/${id}`))
  await toggle.click()
  const putReq = await put
  expect(JSON.parse(putReq.postData() ?? '{}')).toEqual({ enabled: false })
  await expect(toggle).not.toBeChecked()
  await page.unroute(`**/api/v1/replications/${id}`)
  await page.unroute('**/api/v1/replications')

  // 真栈分流腿：合并前（PUT 无路由 → 404）= toast 如实呈现 + 行内不乐观
  // 更新；合并后 = 200 翻转。两形态皆合法——按实际响应分流断言。
  const respPromise = page.waitForResponse((r) => r.url().includes(`/api/v1/replications/${id}`) && r.request().method() === 'PUT')
  await toggle.click()
  const resp = await respPromise
  if (resp.status() === 200) {
    await expect(toggle).toBeChecked()
  } else {
    expect([404, 405]).toContain(resp.status())
    await expect(toggle).not.toBeChecked() // 未乐观更新
    await expect(page.locator('[data-testid="toast"]')).toContainText('启停失败')
  }
})

// ---- 5. 只读臂：readonly_admin——节只读、写入口不渲染、Tab 无编辑深链 ---------

test('readonly_admin: section read-only — switch disabled, write entries absent, tab pointer without edit link', async ({
  page,
}) => {
  const key = uniq('t404e')
  const name = uniq('t404ro').toLowerCase()
  await seedRepos(m8Client(), [{ key }])
  await seedConfig(key, name, { enabled: false })

  await loginAs(page, 'readonly_admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
  await expect(page.locator('[data-testid="form-section-replications"]')).toBeVisible()
  await expect(page.locator(`[data-testid="repl-row-${name}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="repl-toggle-${name}"]`)).toBeDisabled()
  await expect(page.locator('[data-testid="repl-create"]')).toHaveCount(0)
  await expect(page.locator(`[data-testid="repl-edit-${name}"]`)).toHaveCount(0)
  await expect(page.locator(`[data-testid="repl-delete-${name}"]`)).toHaveCount(0)

  // 仓 Tab 指针（升级形态）：摘要行 + 全局页链接；readonly 无编辑深链
  await page.goto(`/binflow/ui/admin/repositories/${key}`)
  await page.click('[data-testid="repo-tab-replications"]')
  await expect(page.locator('[data-testid="repo-repl-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-repl-table"]')).toBeVisible()
  await expect(page.locator(`[data-testid="repo-repl-row-${name}"]`)).toContainText('已停用')
  await expect(page.locator('[data-testid="repo-repl-edit-link"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="repo-repl-goto"]')).toHaveAttribute(
    'href',
    '/binflow/ui/admin/governance/replication',
  )
})

// ---- 6. 列表臂：Replications 列（0 = 纯文本 / 已配置 = Run 触发）------------

// T-420 起列表臂的 ▶ 为真触发（POST /v1/replications/{id}/run 逐启用配置
// ——executeall 语义），占位的「点击深链编辑节」退役：断言翻转为
// toast 排程回报 + URL 停留列表页 + 深链入口移交 toast 的「查看任务」
// （全局复制页 = 任务状态翻转的观测面）。
test('admin: repos list Replications column — plain 0 vs Run trigger (toast + status link); remote tab column is push-only', async ({
  page,
}) => {
  const bare = uniq('t404f')
  const wired = uniq('t404g')
  const name = uniq('t404run').toLowerCase()
  const rem = uniq('t404r')
  await seedRepos(m8Client(), [{ key: bare }, { key: wired }])
  await seedConfig(wired, name, { enabled: true })
  await seedConfig(wired, `${name}-off`, { enabled: false })
  await m8Client().request('PUT', `/binflow/api/repositories/${rem}`, {
    body: { rclass: 'remote', packageType: 'generic', description: 't404 remote', url: 'https://example.com/upstream' },
  })

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  // 未配置 = 纯文本「0」（R5 OSS 未启用分支同款 cell）
  await expect(page.locator(`[data-testid="repos-repl-${bare}"]`)).toHaveText('0')
  // 已配置 = icon-run 行级动作（aria-label 携带条数/启用数——T-404 形态不变）
  const run = page.locator(`[data-testid="repos-repl-run-${wired}"]`)
  await expect(run).toBeVisible()
  await expect(run).toHaveAttribute('aria-label', `复制 ${wired}：2 条配置（1 启用）`)
  await run.click()
  // 真触发：排程 toast（空仓 = 0 项的空跑文案），URL 停留列表页
  await expect(page.locator('[data-testid="toast"]')).toContainText(`已触发 ${wired} 的全量同步`, { timeout: 8000 })
  await expect(page).toHaveURL(/\/admin\/repositories\/local$/)
  // toast 的「查看任务」深链到全局复制页（任务状态翻转的观测面）
  await page.locator('[data-testid="toast"] a, [data-testid="toast"] button').filter({ hasText: '查看任务' }).click()
  await expect(page).toHaveURL(/\/admin\/governance\/replication$/)
  await expect(page.locator('[data-testid="toast"]')).toHaveCount(0)

  // remote Tab 亦有该列（T-443 / B-3.9 翻正：Artifactory 对位列存在——BinFlow
  // 口径 = push-only，ADR-0021/R10：呈现以该仓为源的 push 配置，无 pull 概念
  // ——表头 tooltip 注记）。本腿先备 remote 行再断言（非空表非空断言——
  // 空表时表头不渲染的空洞在 T-443 复核发现）
  await page.goto('/binflow/ui/admin/repositories/remote')
  await expect(page.locator(`[data-testid="repos-row-${rem}"]`)).toBeVisible({ timeout: 30_000 })
  const replHead = page.locator('[data-testid="repos-table"] thead th').filter({ hasText: 'Replications' })
  await expect(replHead).toHaveCount(1)
  await expect(replHead).toHaveAttribute('title', /无 pull 复制/)
  await expect(page.locator(`[data-testid="repos-repl-${rem}"]`)).toHaveText('0')

  // remote 仓详情 Replications Tab：不适用注记（R10——配置面 push 源 = local 仓）
  await page.goto(`/binflow/ui/admin/repositories/${rem}`)
  await page.click('[data-testid="repo-tab-replications"]')
  await expect(page.locator('[data-testid="repo-repl-card"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-repl-na"]')).toBeVisible()

  // 收尾
  await m8Client().request('DELETE', `/binflow/api/repositories/${rem}`)
})

// ---- 7. 降级臂（受控 mock）：501 → repl-degraded；403 → repl-denied ----------

test('degraded states render notes, not tables (mocked 501 / 403)', async ({ page }) => {
  const key = uniq('t404h')
  await seedRepos(m8Client(), [{ key }])
  await loginAs(page, 'admin')

  await page.route('**/api/v1/replications', (route) =>
    route.fulfill({ status: 501, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: 'replication is not configured on this instance' }] }) }),
  )
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
  await expect(page.locator('[data-testid="repl-degraded"]')).toBeVisible()
  await expect(page.locator('[data-testid="repl-list"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="repl-empty"]')).toHaveCount(0)
  await page.unroute('**/api/v1/replications')

  await page.route('**/api/v1/replications', (route) =>
    route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: 'forbidden' }] }) }),
  )
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
  await expect(page.locator('[data-testid="repl-denied"]')).toBeVisible()
  await expect(page.locator('[data-testid="repl-list"]')).toHaveCount(0)
})

// ---- 8. axe 双主题：节 + 内嵌表单开态（含预留位组） ---------------------------

test('axe: replications section + inline form clean in both themes', async ({ page }, testInfo) => {
  const key = uniq('t404i')
  await seedRepos(m8Client(), [{ key }])
  await loginAs(page, 'admin')

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="repl-empty"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="form-section-replications"]' })

    await page.click('[data-testid="repl-create"]')
    await expect(page.locator('[data-testid="repl-form"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="form-section-replications"]' })
  }
})
