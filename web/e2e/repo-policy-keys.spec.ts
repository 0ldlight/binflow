import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { existsSync } from 'node:fs'
import { join } from 'node:path'

import { expectA11yClean } from './m8/support/a11y'

// T-353 fill (FR-113.2/113.5 FE 腿, L29-FE): the repository form's
// deb/rpm/helm policy keys (advanced section, field-registry driven —
// REST 已透传 by T-327R/T-329 D-E, 本票只加表单面). Full-mock probe off
// dist/(replication.spec.ts 同款基座): deb/rpm/helm 是 pro 门控包型,
// community 真栈建不出这三型仓——表单面归 mock; 断言 = 提交体逐键对账 +
// 重开回显(L29-FE: 设 → 保存 → 重开回显) + POINTER 语义(flip-off 恒提交
// 显式 false) + axe 双主题.
//
// 锚 = form-<wire> 生成器族(console-ux §10.5 T-353 批 12 键)。

const DIST = join(process.cwd(), 'dist')

test.beforeEach(() => {
  test.skip(!existsSync(join(DIST, 'index.html')), 'console not built — run `npm run build` first')
})

/** 门控包型槽(deb/rpm/helm 解锁形——真栈 community 的对偶面) */
function pkgSlot(id: string): Record<string, unknown> {
  return {
    id,
    kind: 'package-type',
    minTier: 'pro',
    enabled: true,
    reason: '',
    displayName: id === 'debian' ? 'Debian' : id === 'rpm' ? 'RPM' : 'Helm',
    description: `${id} packages`,
  }
}

/** 编辑态回显体(GET /api/repositories/{key}) */
function repoDetail(key: string, packageType: string, configuration: Record<string, unknown>): Record<string, unknown> {
  return { key, rclass: 'local', packageType, description: 'e2e', url: '', configuration }
}

interface FormMockOpts {
  adminRole?: string
  /** GET /api/repositories/{key} 的应答表(key → configuration); 未列出的 key 404 */
  details?: Record<string, Record<string, unknown>>
  /** 写动词(PUT/POST)成功与否; 请求体推入返回的数组 */
  writes?: unknown[]
}

async function installMocks(page: Page, opts: FormMockOpts = {}): Promise<void> {
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
        username: 'admin',
        admin: true,
        ...(opts.adminRole ? { adminRole: opts.adminRole } : {}),
      }),
    }),
  )
  await page.route('**/binflow/api/system/version', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ version: '6.0.0-t353', revision: 'e2e', product: 'BinFlow' }),
    }),
  )
  await page.route('**/binflow/api/v1/addons', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([pkgSlot('debian'), pkgSlot('rpm'), pkgSlot('helm')]),
    }),
  )
  // virtual 成员候选(建仓面常驻请求)
  await page.route('**/binflow/api/repositories', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) }),
  )
  await page.route(/\/binflow\/api\/repositories\/[^/]+$/, (route) => {
    const key = new URL(route.request().url()).pathname.split('/').pop() ?? ''
    if (route.request().method() === 'GET') {
      const hit = opts.details?.[key]
      if (!hit) {
        return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: 'not found' }] }) })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(hit) })
    }
    // PUT(创建)/POST(更新): 成功纯文本 + 请求体留痕
    opts.writes?.push({ method: route.request().method(), key, body: JSON.parse(route.request().postData() ?? '{}') })
    return route.fulfill({ status: 200, contentType: 'text/plain', body: `Successfully ${route.request().method() === 'PUT' ? 'created' : 'updated'} repository '${key}'\n` })
  })

  await page.route('**/binflow/assets/**', (route) => {
    const name = new URL(route.request().url()).pathname.replace('/binflow/assets/', '')
    return route.fulfill({ path: join(DIST, 'assets', name) })
  })
  await page.route('**/binflow/ui/**', (route) => route.fulfill({ path: join(DIST, 'index.html') }))
}

/** T-439 步进感知归位（本 spec 系 T-439 翻新遗漏的 straggler——策略键节
 *  自 T-439 起分驻 Advanced 步，T-441 复跑发现；断言语义不弱化，只补步进
 *  导航。同款先例：m8/repositories-admin 的 T-431 漏翻新腿在 T-439 顺车归位） */
async function gotoAdvanced(page: import('@playwright/test').Page): Promise<void> {
  await page.click('[data-testid="form-step-advanced"]')
}

// ---------------------------------------------------------------------------
// ① deb 编辑器: 设策略键(byHash/historyCycles/origin)→ 保存 → 提交体对账
//    (空 text 剔除; 全量替换语义下 governance 键随行)
// ---------------------------------------------------------------------------

test('deb editor: set policy keys and save — transport body matches the registry', async ({ page }) => {
  const writes: { method: string; key: string; body: Record<string, unknown> }[] = []
  await installMocks(page, {
    details: {
      'deb-local': repoDetail('deb-local', 'debian', {
        byHash: 'SHA256',
        historyCycles: 5,
        includesPattern: '',
        excludesPattern: '',
        quotaBytes: 0,
      }),
    },
    writes,
  })
  await page.goto('/binflow/ui/admin/repositories/deb-local/edit')

  // 高级分区(Advanced 步): deb 六键呈现(字段册驱动), 回显预填
  await expect(page.locator('[data-testid="repo-form-page"]')).toBeVisible()
  await gotoAdvanced(page)
  await expect(page.getByText('Deb 索引策略——索引引擎策略键')).toBeVisible()
  await expect(page.locator('[data-testid="form-byHash"]')).toHaveValue('SHA256')
  await expect(page.locator('[data-testid="form-historyCycles"]')).toHaveValue('5')
  await expect(page.locator('[data-testid="form-optionalIndexCompressionFormats"]')).toHaveCount(1)
  await expect(page.locator('[data-testid="form-debianDefaultArchitectures"]')).toHaveCount(1)
  await expect(page.locator('[data-testid="form-origin"]')).toHaveCount(1)
  await expect(page.locator('[data-testid="form-label"]')).toHaveCount(1)

  // 改键: byHash=ALL / historyCycles=7 / origin 文本
  await page.locator('[data-testid="form-byHash"]').selectOption('ALL')
  await page.locator('[data-testid="form-historyCycles"]').fill('7')
  await page.locator('[data-testid="form-origin"]').fill('BinFlow CI')
  await page.locator('[data-testid="form-submit"]').click()

  expect(writes).toHaveLength(1)
  expect(writes[0].method).toBe('POST')
  const body = writes[0].body
  expect(body.byHash).toBe('ALL')
  expect(body.historyCycles).toBe(7)
  expect(body.origin).toBe('BinFlow CI')
  // 空 text 剔除(归 adapter 默认); POINTER 无 boolean 键(deb 族纯 text/number/select)
  expect(body.optionalIndexCompressionFormats).toBeUndefined()
  expect(body.debianDefaultArchitectures).toBeUndefined()
  expect(body.label).toBeUndefined()
  // 全量替换语义: governance 键随行(includesPattern 空 = 恢复默认 **/*)
  expect(body.rclass).toBe('local')
  expect(body.packageType).toBe('debian')
})

// ---------------------------------------------------------------------------
// ② L29-FE 正面: 设 → 保存 → 重开回显(deb)
// ---------------------------------------------------------------------------

test('deb editor round-trip: saved config echoes back on reopen', async ({ page }) => {
  const writes: { method: string; key: string; body: Record<string, unknown> }[] = []
  const cfgByRev: Record<string, Record<string, unknown>> = {
    'deb-rt': repoDetail('deb-rt', 'debian', {}),
  }
  await installMocks(page, { details: cfgByRev, writes })
  await page.goto('/binflow/ui/admin/repositories/deb-rt/edit')

  // 默认拼写: byHash 闭集首值 NONE, historyCycles 空(= 归默认 3)
  await gotoAdvanced(page)
  await expect(page.locator('[data-testid="form-byHash"]')).toHaveValue('NONE')
  await page.locator('[data-testid="form-byHash"]').selectOption('SHA256')
  await page.locator('[data-testid="form-historyCycles"]').fill('9')
  await page.locator('[data-testid="form-submit"]').click()
  expect(writes).toHaveLength(1)

  // 重开: GET 回显已保存配置(模拟服务端 canonical 存储后的 GET)
  cfgByRev['deb-rt'] = repoDetail('deb-rt', 'debian', { byHash: 'SHA256', historyCycles: 9 })
  await page.goto('/binflow/ui/admin/repositories/deb-rt/edit')
  await gotoAdvanced(page)
  await expect(page.locator('[data-testid="form-byHash"]')).toHaveValue('SHA256')
  await expect(page.locator('[data-testid="form-historyCycles"]')).toHaveValue('9')
})

// ---------------------------------------------------------------------------
// ③ rpm 创建面: 网格选型 → RP-2 开关 + 根深度; POINTER 语义——显式 false
//    恒随行(flip-off 过 round trip 的前提)
// ---------------------------------------------------------------------------

test('rpm create: RP-2 opt-in and root depth ride the PUT; explicit false survives', async ({ page }) => {
  const writes: { method: string; key: string; body: Record<string, unknown> }[] = []
  await installMocks(page, { writes })

  await page.goto('/binflow/ui/admin/repositories/new?rclass=local')
  await page.locator('[data-testid="pkg-grid-item-rpm"]').click()
  await page.locator('[data-testid="form-key"]').fill('rpm-local')
  await gotoAdvanced(page)
  // deb 族字段不得出现在 rpm 仓(字段册按包类型收窄)
  await expect(page.locator('[data-testid="form-byHash"]')).toHaveCount(0)
  await expect(page.getByText('RPM 索引策略——索引引擎策略键')).toBeVisible()

  await page.locator('[data-testid="form-calculateYumMetadata"]').check()
  await page.locator('[data-testid="form-yumRootDepth"]').fill('2')
  await page.locator('[data-testid="form-submit"]').click()

  expect(writes).toHaveLength(1)
  expect(writes[0].method).toBe('PUT')
  const body = writes[0].body
  expect(body.calculateYumMetadata).toBe(true)
  expect(body.yumRootDepth).toBe(2)
  // POINTER 语义: 未勾的布尔键显式 false 随行(不是缺省)
  expect(body.enableFileListsIndexing).toBe(false)
  // 空 text 剔除(归默认 comps.xml)
  expect(body.yumGroupFileNames).toBeUndefined()
  expect(body.byHash).toBeUndefined() // deb 键不串型
})

// ---------------------------------------------------------------------------
// ④ helm 编辑器: 强制布局对的 flip-off(预填 true → 摘勾 → 显式 false)
// ---------------------------------------------------------------------------

test('helm editor: enforce-layout pair round-trips an explicit false on flip-off', async ({ page }) => {
  const writes: { method: string; key: string; body: Record<string, unknown> }[] = []
  await installMocks(page, {
    details: {
      'helm-local': repoDetail('helm-local', 'helm', {
        forceMetadataNameVersion: true,
        forceNonDuplicateChart: false,
      }),
    },
    writes,
  })
  await page.goto('/binflow/ui/admin/repositories/helm-local/edit')
  await gotoAdvanced(page)

  await expect(page.getByText('Helm 强制布局——索引引擎策略键')).toBeVisible()
  await expect(page.locator('[data-testid="form-forceMetadataNameVersion"]')).toBeChecked()
  await expect(page.locator('[data-testid="form-forceNonDuplicateChart"]')).not.toBeChecked()

  await page.locator('[data-testid="form-forceMetadataNameVersion"]').uncheck()
  await page.locator('[data-testid="form-forceNonDuplicateChart"]').check()
  await page.locator('[data-testid="form-submit"]').click()

  expect(writes).toHaveLength(1)
  expect(writes[0].body.forceMetadataNameVersion).toBe(false)
  expect(writes[0].body.forceNonDuplicateChart).toBe(true)
})

// ---------------------------------------------------------------------------
// ⑤ readonly_admin: 策略键控件全禁用 + 表单只读注记(反断言面)
// ---------------------------------------------------------------------------

test('readonly_admin: policy fields disabled with the form readonly note', async ({ page }) => {
  await installMocks(page, {
    adminRole: 'readonly_admin',
    details: { 'deb-ro': repoDetail('deb-ro', 'debian', { byHash: 'ALL' }) },
  })
  await page.goto('/binflow/ui/admin/repositories/deb-ro/edit')
  await gotoAdvanced(page)

  await expect(page.locator('[data-testid="repo-form-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-byHash"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-historyCycles"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-origin"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()
})

// ---------------------------------------------------------------------------
// ⑥ axe 双主题: deb 编辑器(策略键全列展开)与新网格项的结构性可达
// ---------------------------------------------------------------------------

test('a11y: deb policy form is clean in both themes', async ({ page }, testInfo) => {
  await installMocks(page, {
    details: { 'deb-a11y': repoDetail('deb-a11y', 'debian', { byHash: 'ALL', historyCycles: 3 }) },
  })
  for (const theme of ['light', 'dark'] as const) {
    await page.goto('/binflow/ui/artifacts')
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await page.goto('/binflow/ui/admin/repositories/deb-a11y/edit')
    await gotoAdvanced(page)
    await expect(page.locator('[data-testid="form-byHash"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repo-form-page"]' })
  }
})

// ---------------------------------------------------------------------------
// ⑦ 审计动作选择器: 全量词表镜像(internal/audit Actions()——T-346 54 枚 +
//    T-446/T-450 调度三域九词 = 63 枚, 逐枚对照 = 选项计数 + 新词表抽查;
//    选择器与断言源 GE-02)
// ---------------------------------------------------------------------------

test('audit action picker carries the full vocabulary (54 T-346 + 9 scheduler actions)', async ({ page }) => {
  await installMocks(page)
  await page.route('**/binflow/api/v1/audit**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ events: [], nextCursor: '' }) }),
  )
  await page.goto('/binflow/ui/admin/governance/audit')

  const select = page.locator('[data-testid="audit-filter-action"]')
  await expect(select).toBeVisible()
  // 63 动作 + 1 个「动作：全部」占位
  await expect(select.locator('option')).toHaveCount(64)
  // 新词表抽查: T-346 的各族 + 补漏的 cleanup.run(值原样 mono, 不翻译)
  // + T-446/T-450 调度三域 set/run/fail 九词
  for (const action of [
    'cleanup.run',
    'props.write',
    'replication.push.failed',
    'keypair.associate',
    'auth.config.samlkey.regenerate',
    'license.addon.denied',
    'artifact.explode',
    'trash.restore',
    'trash.empty',
    'trash.clean',
    'trash.retention',
    'storage.replay.drained',
    'maintenance.schedule.set',
    'maintenance.schedule.run',
    'maintenance.schedule.fail',
    'backup.schedule.set',
    'backup.schedule.run',
    'backup.schedule.fail',
    'replication.schedule.set',
    'replication.schedule.run',
    'replication.schedule.fail',
  ]) {
    await expect(select.locator(`option[value="${action}"]`)).toHaveCount(1)
  }
})
