import { expect, test } from '@playwright/test'

// T-99 探针：仓库创建 → 列表 → 详情 → 编辑（governance 字段往返）→
// 危险区删除（含非空仓 deleteContent 双段流）全流程；remote 凭据不回显；
// virtual 成员序与 defaultDeploymentRepo；表单门控零写请求。
//
// T-240 操作流迁移：三步向导 → 单页分区表单（进页弹包类型网格
// pkg-grid-*，选定即关闭）；步骤按钮 form-next/prev 退役，门控断言改
// form-submit 禁用态；列表类型筛选 select 退役（三 Tab 子路由承载，
// virtual 仓行在 /admin/repositories/virtual Tab）；详情 governance 卡
// 自本票起在「配置」Tab（repo-tab-config 先点，锚不变）。
//
// 运行前提：已 `make console && make build` 的真二进制在前台 serve，
// BASE 指向它（同 T-98 探针约定）；ADMIN_PW 默认 password（PRD §4）。
// 对账腿走 page.evaluate fetch（同源携带 session cookie——内容面与
// 管理面同凭据，ux R10 / CE-03），即「curl 单查对账」的浏览器等价。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: import('@playwright/test').Page) {
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** 同源 fetch（携带 session cookie）；返回 {status, text} */
async function api(
  page: import('@playwright/test').Page,
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

/** 唯一 key（规则 [a-z][a-z0-9-]{1,62}） */
function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

test('local generic full lifecycle: create with governance -> list -> edit roundtrip -> delete with content', async ({ page }) => {
  const key = uniq('t99a')
  await page.goto('/binflow/ui/admin/repositories/new')
  await login(page)

  // 进页即弹包类型网格（T-240 向导第 0 步）：选 Generic 即选定关闭
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await page.click('[data-testid="pkg-grid-item-generic"]')
  await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="form-rclass-local"]')).toBeChecked()

  // 单页分区表单：key 实时校验 + 描述 + governance（同一页）
  await page.fill('[data-testid="form-key"]', key)
  await expect(page.locator('[data-testid="form-key-ok"]')).toBeVisible()
  await page.fill('[data-testid="form-description"]', 'T-99 lifecycle probe')
  await page.fill('[data-testid="form-quota"]', '1048576')
  await page.fill('[data-testid="form-includes"]', '**/*')
  await page.fill('[data-testid="form-excludes"]', 'tmp/**')
  await page.click('[data-testid="form-submit"]')

  // 创建成功：toast（服务端文案）+ 跳详情
  await expect(page.locator('[data-testid="toast"]')).toContainText(`Successfully created repository '${key}'`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${key}$`))
  await expect(page.locator('[data-testid="repo-detail-page"] .key')).toHaveText(key)
  // governance 卡在「配置」Tab（T-240 三 Tab 化；锚不变）
  await page.click('[data-testid="repo-tab-config"]')
  await expect(page.locator('[data-testid="repo-governance-card"]')).toContainText('1048576')
  await page.click('[data-testid="repo-tab-summary"]')
  await expect(page.locator('[data-testid="repo-usage-card"]')).toBeVisible()

  // API 对账：packageType / governance 字段回显（AC①：建后 curl 单查对账）
  const created = await api(page, 'GET', `/api/repositories/${key}`)
  expect(created.status).toBe(200)
  const createdJson = JSON.parse(created.text)
  expect(createdJson.packageType).toBe('generic')
  expect(createdJson.rclass).toBe('local')
  expect(createdJson.configuration.quotaBytes).toBe(1048576)
  expect(createdJson.configuration.includesPattern).toBe('**/*')
  expect(createdJson.configuration.excludesPattern).toBe('tmp/**')

  // 列表：行可见 + 过滤（T-240：local 仓行在 local Tab 子路由）
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toBeVisible()

  // review B1：行内拷贝不触发行导航（隔离层），且剪贴板拿到完整 key
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.click(`[data-testid="repos-row-${key}"] .copy-btn`)
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)
  const clip = await page.evaluate(() => navigator.clipboard.readText())
  expect(clip).toBe(key)

  await page.fill('[data-testid="repos-filter-key"]', key)
  await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toBeVisible()
  await page.fill('[data-testid="repos-filter-key"]', 'definitely-no-such-repo')
  await expect(page.locator('[data-testid="repos-empty-filtered"]')).toBeVisible()

  // 编辑：rclass/packageType 锁定 + quota 修改往返（全量替换保全；单页无步骤）
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await expect(page.locator('[data-testid="form-rclass-local"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-package-generic"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-quota"]')).toHaveValue('1048576')
  await expect(page.locator('[data-testid="form-excludes"]')).toHaveValue('tmp/**')
  await page.fill('[data-testid="form-quota"]', '2097152')
  await page.fill('[data-testid="form-includes"]', 'release/**')
  await page.click('[data-testid="form-submit"]')
  await expect(page.locator('[data-testid="toast"]')).toContainText('update successfully')
  const updated = await api(page, 'GET', `/api/repositories/${key}`)
  const updatedJson = JSON.parse(updated.text)
  expect(updatedJson.configuration.quotaBytes).toBe(2097152)
  expect(updatedJson.configuration.includesPattern).toBe('release/**')
  expect(updatedJson.configuration.excludesPattern).toBe('tmp/**') // 未动的字段保全

  // 上传制品（内容面同凭据）：治理 pattern 门生效——includes=release/**
  // 时 acme/ 被拒（409 双 pattern），release/ 放行
  const rejected = await api(page, 'PUT', `/${key}/acme/app.bin`, 't99-probe-payload')
  expect(rejected.status).toBe(409)
  expect(rejected.text).toContain('includesPattern')
  const put = await api(page, 'PUT', `/${key}/release/app.bin`, 't99-probe-payload')
  expect(put.status).toBe(201)

  await page.goto(`/binflow/ui/admin/repositories/${key}`)
  await page.click('[data-testid="repo-delete-button"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()

  // 输错 key：确认不可用；输入正确 key 但不勾 deleteContent
  await page.fill('[data-testid="repo-delete-confirm-key"]', 'wrong-key')
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeDisabled()
  await page.fill('[data-testid="repo-delete-confirm-key"]', key)
  await page.click('[data-testid="confirm-accept"]')

  // 非空仓：400 原因（含制品数）带回对话框呈现，deleteContent 已预勾选。
  // 节点数口径（T-172 D-3 / ADR-0016 目录实体化）：release/app.bin 落库
  // 时 putNode 材料化 release/ folder 行——1 文件 + 1 祖先目录 = 恰 2 nodes
  await expect(page.locator('[data-testid="repo-delete-reason"]')).toContainText('holds 2 node(s)')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await page.fill('[data-testid="repo-delete-confirm-key"]', key)
  await page.click('[data-testid="confirm-accept"]')

  await expect(page.locator('[data-testid="toast"]')).toContainText('deleted successfully')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)
  await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toHaveCount(0)

  // 内容路径 404 断言（AC②）
  const gone = await api(page, 'GET', `/${key}/release/app.bin`)
  expect(gone.status).toBe(404)
})

test('remote maven: url roundtrip, password never echoed, empty delete', async ({ page }) => {
  const key = uniq('t99b')
  await page.goto('/binflow/ui/admin/repositories/new')
  await login(page)

  // 网格（local 默认）先选 Maven；再切 Remote——docker 组合自 T-431 起可选
  //（服务端 T-392 已开 remote 格；FE 门随 virtual 开禁一并退役）
  await page.click('[data-testid="pkg-grid-item-maven"]')
  await page.click('[data-testid="form-rclass-remote"]')
  await expect(page.locator('[data-testid="form-package-docker"]')).toBeEnabled()

  await page.fill('[data-testid="form-key"]', key)
  await page.fill('[data-testid="form-url"]', 'https://repo1.maven.org/maven2')
  await page.fill('[data-testid="form-username"]', 'dev')
  await page.fill('[data-testid="form-password"]', 's3cret-t99')
  await page.click('[data-testid="form-submit"]')

  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${key}$`))
  await expect(page.locator('[data-testid="repo-remote-card"]')).toContainText('repo1.maven.org')

  // API 对账：url/username 回显；密码绝不出现在任何回显面（NFR-S14）
  const got = await api(page, 'GET', `/api/repositories/${key}`)
  expect(got.status).toBe(200)
  expect(got.text).not.toContain('s3cret-t99')
  const json = JSON.parse(got.text)
  expect(json.rclass).toBe('remote')
  expect(json.packageType).toBe('maven')
  expect(json.configuration.url).toBe('https://repo1.maven.org/maven2')
  expect(json.configuration.username).toBe('dev')

  // 编辑：url 预填（remote 更新必带 url，否则服务端 400）——单页直达
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await expect(page.locator('[data-testid="form-url"]')).toHaveValue('https://repo1.maven.org/maven2')
  await expect(page.locator('[data-testid="form-password"]')).toHaveValue('')

  // 空仓删除：不勾 deleteContent 直接成功
  await page.goto(`/binflow/ui/admin/repositories/${key}`)
  await page.click('[data-testid="repo-delete-button"]')
  await page.fill('[data-testid="repo-delete-confirm-key"]', key)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]')).toContainText('deleted successfully')
})

test('form gating: docker combo open since T-431, key/url precheck, zero write requests', async ({ page }) => {
  await page.goto('/binflow/ui/admin/repositories/new')
  await login(page)

  const writes: string[] = []
  page.on('request', (r) => {
    if (/\/binflow\/api\/repositories\//.test(r.url()) && (r.method() === 'PUT' || r.method() === 'POST')) {
      writes.push(`${r.method()} ${r.url()}`)
    }
  })

  await page.click('[data-testid="pkg-grid-item-generic"]')

  // Remote × Docker 组合自 T-431 起可选（组合门退役；license 门控槽位不在此面）
  await page.click('[data-testid="form-rclass-remote"]')
  await expect(page.locator('[data-testid="form-package-docker"]')).toBeEnabled()

  // key 预检：非法字符 / 保留段（门控断言 = form-submit 禁用——T-240 单页化）
  await page.fill('[data-testid="form-key"]', 'Bad_Key')
  await expect(page.locator('[data-testid="form-key-error"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="form-key"]', 'api')
  await expect(page.locator('[data-testid="form-key-error"]')).toContainText('保留段')
  await page.fill('[data-testid="form-key"]', 't99-gate-probe')

  // remote 缺 url：门控拦下（FR-24-AC5：零写请求）
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="form-url"]', 'ftp://example.com/x')
  await expect(page.locator('[data-testid="form-url-error"]')).toContainText('http')
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()
  await page.fill('[data-testid="form-url"]', '')

  await page.waitForTimeout(300)
  expect(writes).toHaveLength(0)
})

test('virtual: member order roundtrip, defaultDeploymentRepo, server 400 inline', async ({ page }) => {
  const m1 = uniq('t99v1')
  const m2 = uniq('t99v2')
  await page.goto('/binflow/ui/')
  await login(page)
  // 成员仓走 API 准备（表单 SUT 是 virtual 本身）
  await api(page, 'PUT', `/api/repositories/${m1}`, { rclass: 'local', packageType: 'generic' })
  await api(page, 'PUT', `/api/repositories/${m2}`, {
    rclass: 'local',
    packageType: 'generic',
    quotaBytes: 1024,
  })

  const vkey = uniq('t99v')
  await page.goto('/binflow/ui/admin/repositories/new')
  await page.click('[data-testid="pkg-grid-item-generic"]')
  await page.click('[data-testid="form-rclass-virtual"]')
  await expect(page.locator('[data-testid="form-package-generic"]')).toBeChecked()
  await page.fill('[data-testid="form-key"]', vkey)
  await page.check(`[data-testid="form-member-${m1}"]`)
  await page.check(`[data-testid="form-member-${m2}"]`)
  // 调序：把 m2 上移到首位
  await page.click('[data-testid="member-up-1"]')
  await expect(page.locator('[data-testid="form-member-order"] .chip-item').first()).toContainText(m2)
  await page.selectOption('[data-testid="form-default-deploy"]', m1)
  await page.click('[data-testid="form-submit"]')

  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${vkey}$`))
  await expect(page.locator('[data-testid="repo-virtual-card"]')).toBeVisible()

  // API 对账：成员序 + defaultDeploymentRepo（写路由只接受 local 成员）
  const got = await api(page, 'GET', `/api/repositories/${vkey}`)
  const json = JSON.parse(got.text)
  expect(json.configuration.repositories).toEqual([m2, m1])
  expect(json.configuration.defaultDeploymentRepo).toBe(m1)

  // review B1：列表成员浮层可开、内容可读，且点击不触发行导航
  //（T-240：virtual 仓行在 virtual Tab 子路由）
  await page.goto('/binflow/ui/admin/repositories/virtual')
  await page.click(`[data-testid="repos-row-${vkey}"] .member-pop summary`)
  const pop = page.locator(`[data-testid="repos-row-${vkey}"] .member-pop .pop`)
  await expect(pop).toBeVisible()
  await expect(pop).toContainText(m2)
  await expect(pop).toContainText(m1)
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/virtual$/)

  // review B2：取消 defaultDeploymentRepo 所指成员 → select 联动回「（未配置）」，提交不再吃 400
  await page.goto(`/binflow/ui/admin/repositories/${vkey}/edit`)
  await expect(page.locator(`[data-testid="form-member-${m1}"]`)).toBeChecked()
  await expect(page.locator('[data-testid="form-default-deploy"]')).toHaveValue(m1)
  await page.uncheck(`[data-testid="form-member-${m1}"]`)
  await expect(page.locator('[data-testid="form-default-deploy"]')).toHaveValue('')
  await page.click('[data-testid="form-submit"]')
  await expect(page.locator('[data-testid="toast"]')).toContainText('update successfully')
  const after = await api(page, 'GET', `/api/repositories/${vkey}`)
  const afterJson = JSON.parse(after.text)
  expect(afterJson.configuration.repositories).toEqual([m2])
  expect(afterJson.configuration.defaultDeploymentRepo).toBeUndefined()

  // 服务端 400 行内回显：编辑态成员在表单打开后被外部删除 → 保存被服务端拒
  await page.goto(`/binflow/ui/admin/repositories/${vkey}/edit`)
  await expect(page.locator(`[data-testid="form-member-${m2}"]`)).toBeChecked()
  await api(page, 'DELETE', `/api/repositories/${m2}`)
  await page.click('[data-testid="form-submit"]')
  await expect(page.locator('[data-testid="form-error"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-error"]')).toContainText(m2)

  // 收尾
  await api(page, 'DELETE', `/api/repositories/${vkey}`)
  await api(page, 'DELETE', `/api/repositories/${m1}`)
})
