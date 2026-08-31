import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { existsSync } from 'node:fs'
import { join } from 'node:path'

// T-366 fill (M13 FR-115.5 FE 腿, L09-FE 段): the webhook subscription
// console page (/admin/governance/webhooks — 治理分组第七页). Full-mock
// probe off dist/ (trash-can.spec.ts 的同款基座): the shared harness runs a
// community instance where the webhook slot locks every write verb at the
// server, so the CRUD/test surface only ever runs against mocks here; the
// REAL pro-instance legs live in t366-consumer.spec.ts (self-booted
// ephemeral server + script receiver).
//
// 覆盖: ① 空态 → 新建对话框（66 型分组下拉/校验/secret 三态/criteria
// 五键）→ POST 体对账 → 行呈现; ② 编辑（key 锁定、secret 留空 = 剔除键
// ——哨兵绝不回传, PUT 体网络层断言）; ③ 行内试发（wh-test-last 结果面）
// + 草稿试发（wh-test-result）; ④ 启停开关 PUT; ⑤ 删除 danger 确认;
// ⑥ 详情抽屉 + 投递记录（状态/耗时/重试计数 + 载荷快照 mono/copy;
// 空态 = 排障环语义如实）; ⑦ readonly_admin 只读臂 + 零写请求反断言;
// ⑧ community 槽锁定提示 + 写 403 表单呈现; ⑨ 列表 5xx 错误态 + 重试;
// ⑩ axe 双主题（页面 + Dialog + Drawer 开启态）serious/critical = 0。

const DIST = join(process.cwd(), 'dist')

test.beforeEach(() => {
  test.skip(!existsSync(join(DIST, 'index.html')), 'console not built — run `npm run build` first')
})

const PAGE = '/binflow/ui/admin/governance/webhooks'

/** 订阅回显 fixture（model.go SubscriptionView 逐字字段名） */
function subView(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    key: 'ci-hook',
    project_key: '',
    description: 'CI 触发器',
    enabled: true,
    event_filter: {
      domain: 'artifact',
      event_types: ['deployed'],
      criteria: { anyLocal: true, repoKeys: [], includePatterns: [], excludePatterns: [] },
    },
    handlers: [
      {
        handler_type: 'webhook',
        url: 'http://127.0.0.1:9990/hook',
        proxy: '',
        secret: '********',
        use_secret_for_signing: true,
        custom_http_headers: [],
        http_headers: [],
        secrets: [],
      },
    ],
    debug: false,
    ...over,
  }
}

/** 排障记录 fixture（service.go TroubleshootingRecord §7 schema） */
function record(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    timestamp: 1756550400000,
    elapsed_millis: 42,
    errors: [],
    request: {
      method: 'POST',
      url: 'http://127.0.0.1:9990/hook',
      headers: { 'X-JFrog-Event-Auth': ['********'] },
      payload: JSON.stringify({
        domain: 'artifact',
        event_type: 'deployed',
        data: { repo_key: 'libs', path: 'com/acme/1.0/a.jar', name: 'a.jar', sha256: 'ab'.repeat(32), size: 9 },
        subscription_key: 'ci-hook',
        jpd_origin: 'http://127.0.0.1:8080',
        source: 'binflow/binflow@01HTEST',
        userContext: { id: 'admin', isToken: false, realm: 'internal' },
      }),
      retries_attempted: 0,
    },
    response: { status: 200, headers: {}, body: '{"ok":true}' },
    event: {
      id: '01HTEST000000000000000000X',
      subscription_key: 'ci-hook',
      domain: 'artifact',
      event_type: 'deployed',
      data: {},
      source: 'binflow/binflow@01HTEST',
    },
    ...over,
  }
}

interface MockOpts {
  role?: 'admin' | 'readonly_admin'
  /** webhook 槽行（null = addons 403——按未门控呈现）；缺省解锁 */
  slot?: { enabled: boolean; minTier?: string } | null
  /** 初始订阅集（缺省一条 ci-hook） */
  subs?: Record<string, unknown>[]
  /** 列表 GET 的应答（缺省 200 存量集） */
  listStatus?: number
  /** 写动词应答（缺省成功形） */
  onWrite?: (method: string, path: string) => { status: number; body?: unknown }
  /** 排障记录应答 */
  onRecords?: (subscription: string) => Record<string, unknown>[]
}

/**
 * 全 mock 基座：会话/version/addons（webhook 槽）+ /event/api/v1 七端点族。
 * 返回写动词请求的记录器（body 对账腿消费）。
 */
async function installMocks(page: Page, opts: MockOpts = {}) {
  const writes: { method: string; path: string; body?: string }[] = []
  const reads: { method: string; path: string }[] = []
  let subs: Record<string, unknown>[] = opts.subs ?? [subView()]

  // 兜底先注册（后注册者优先）：未覆盖端点一律 404，不漏到真实网络
  await page.route('**/binflow/api/**', (route) =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: 'unmocked endpoint' }] }) }),
  )
  await page.route('**/binflow/event/**', (route) =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify('unmocked event endpoint') }),
  )

  await page.route('**/binflow/api/v1/session', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        username: opts.role === 'readonly_admin' ? 'ro-admin' : 'admin',
        admin: true,
        ...(opts.role ? { adminRole: opts.role } : {}),
      }),
    }),
  )
  await page.route('**/binflow/api/system/version', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ version: '6.0.0-t366', revision: 'e2e', product: 'BinFlow' }) }),
  )
  await page.route('**/binflow/api/v1/addons', (route) => {
    if (opts.slot === null) {
      return route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: 'forbidden' }] }) })
    }
    const row = opts.slot ?? { enabled: true, minTier: 'pro' }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: 'webhook',
          kind: 'feature',
          minTier: row.minTier ?? 'pro',
          enabled: row.enabled,
          reason: row.enabled ? '' : `requires ${row.minTier ?? 'pro'}`,
          displayName: 'Webhooks',
          description: 'Unified event subscriptions.',
        },
      ]),
    })
  })

  await page.route('**/binflow/event/api/v1/troubleshooting**', (route) => {
    const url = new URL(route.request().url())
    const key = url.searchParams.get('subscription') ?? ''
    reads.push({ method: 'GET', path: `troubleshooting?${key}` })
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(opts.onRecords?.(key) ?? []) })
  })

  await page.route('**/binflow/event/api/v1/subscriptions/**', (route) => {
    const method = route.request().method()
    const key = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '')
    if (method === 'GET') {
      reads.push({ method, path: `get ${key}` })
      const found = subs.find((s) => s.key === key)
      if (!found) return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify('Subscription not found') })
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(found) })
    }
    writes.push({ method, path: `${method} ${key}`, body: route.request().postData() ?? undefined })
    const reply =
      opts.onWrite?.(method, `/subscriptions/${key}`) ??
      (method === 'PUT'
        ? { status: 204 }
        : method === 'DELETE'
          ? { status: 204 }
          : { status: 201, body: subView() })
    if (reply.status < 300 && method === 'PUT') {
      const body = route.request().postDataJSON() as Record<string, unknown>
      subs = subs.map((s) => {
        if (s.key !== key) return s
        // 服务端语义保真：PUT 体省略 secret = 保持库存密文（哨兵仍在回显里）
        const inHandlers = (body.handlers as Record<string, unknown>[] | undefined)?.[0]
        if (inHandlers && !('secret' in inHandlers)) {
          const storedSecret = (s.handlers as Record<string, unknown>[])[0]?.secret
          if (storedSecret !== undefined) inHandlers.secret = storedSecret
        }
        return { ...s, ...body }
      })
    }
    if (reply.status < 300 && method === 'DELETE') subs = subs.filter((s) => s.key !== key)
    return route.fulfill({
      status: reply.status,
      contentType: 'application/json',
      body: reply.status === 204 ? '' : JSON.stringify(reply.body ?? {}),
    })
  })

  await page.route('**/binflow/event/api/v1/subscriptions', (route) => {
    const method = route.request().method()
    if (method === 'GET') {
      reads.push({ method, path: 'list' })
      if (opts.listStatus && opts.listStatus !== 200) {
        return route.fulfill({ status: opts.listStatus, contentType: 'application/json', body: JSON.stringify('store busy') })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(subs) })
    }
    writes.push({ method: 'POST', path: 'create', body: route.request().postData() ?? undefined })
    const reply = opts.onWrite?.('POST', '/subscriptions') ?? { status: 201, body: subView() }
    if (reply.status < 300) {
      const body = (reply.body ?? subView()) as Record<string, unknown>
      subs = [...subs.filter((s) => s.key !== body.key), body]
    }
    return route.fulfill({ status: reply.status, contentType: 'application/json', body: JSON.stringify(reply.body ?? {}) })
  })

  // NOTE: registered LAST so it outranks the {key} glob (Playwright resolves
  // route precedence most-recent-first; "test" is a literal, not a key).
  await page.route('**/binflow/event/api/v1/subscriptions/test', (route) => {
    writes.push({ method: 'POST', path: 'test', body: route.request().postData() ?? undefined })
    const reply = opts.onWrite?.('POST', '/subscriptions/test') ?? {
      status: 200,
      body: { message: 'Test successful', ok: true, attempt: { status_code: 200, elapsed_millis: 37 } },
    }
    return route.fulfill({ status: reply.status, contentType: 'application/json', body: JSON.stringify(reply.body) })
  })

  return { writes, reads }
}

/** axe：serious（含 critical）= 0。扫描前把鼠标挪离交互区——点击开 Dialog
 *  后指针停在打开点，MUI action.hover 悬停底（≈4% 黑罩）会让其下文字的
 *  对比度假性跌破 4.5（hover 态不是本断言的对象）。 */
async function expectAxeClean(page: Page, label: string) {
  await page.mouse.move(1, 1)
  await page.waitForTimeout(100)
  const results = await new AxeBuilder({ page }).analyze()
  const serious = results.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')
  const detail = serious.map((v) => `${v.id}(${v.impact}): ${v.nodes.length} nodes`).join(', ')
  expect(serious, `${label} — ${detail}`).toEqual([])
}

async function openPage(page: Page) {
  await page.goto(PAGE)
  await expect(page.locator('[data-testid="wh-page"]')).toBeVisible()
}

// ---- ① 空态 + 新建全链 ----------------------------------------------------

test('wh: empty state → create dialog (closed-set, criteria, secret) → row', async ({ page }) => {
  const io = await installMocks(page, { subs: [] })
  await openPage(page)

  await expect(page.locator('[data-testid="wh-empty"]')).toBeVisible()
  await page.click('[data-testid="wh-create"]')
  await expect(page.locator('[data-testid="wh-dialog"]')).toBeVisible()

  // 校验：坏 key → helperText；submit 禁用
  await page.fill('[data-testid="wh-form-key"]', '9bad key')
  await expect(page.locator('text=key 须字母开头')).toBeVisible()
  expect(await page.locator('[data-testid="wh-form-submit"]').isDisabled()).toBe(true)
  await page.fill('[data-testid="wh-form-key"]', 'ci-hook')
  // 默认域 artifact + deployed 预选；勾 moved
  await page.click('[data-testid="wh-form-type-moved"]')
  // 默认 anyLocal 已勾 → 无空范围警示
  await expect(page.locator('[data-testid="wh-form-scope-warn"]')).toHaveCount(0)
  await page.fill('[data-testid="wh-form-url"]', 'http://127.0.0.1:9990/hook')
  await page.fill('[data-testid="wh-form-secret"]', 's3cret')
  await page.check('[data-testid="wh-form-sign"]')
  await page.check('[data-testid="wh-form-enabled"]')
  await page.check('[data-testid="wh-form-any-remote"]')
  await page.check('[data-testid="wh-form-debug"]')
  await page.fill('[data-testid="wh-form-repos"]', 'libs, libs2')
  await page.fill('[data-testid="wh-form-include"]', 'com/**')
  await page.fill('[data-testid="wh-form-exclude"]', '**/*.tmp')

  await page.click('[data-testid="wh-form-submit"]')

  // POST 体对账：wire 形（域/多事件型/criteria 五键/secret 明文/签名态/启用）
  await expect.poll(() => io.writes.length).toBe(1)
  const body = JSON.parse(io.writes[0]!.body!) as Record<string, unknown>
  const ef = body.event_filter as Record<string, unknown>
  const h0 = (body.handlers as Record<string, unknown>[])[0]!
  expect(body.key).toBe('ci-hook')
  expect(body.enabled).toBe(true)
  expect(ef.domain).toBe('artifact')
  expect(ef.event_types).toEqual(['deployed', 'moved'])
  expect(ef.criteria).toEqual({
    anyLocal: true,
    anyRemote: true,
    repoKeys: ['libs', 'libs2'],
    includePatterns: ['com/**'],
    excludePatterns: ['**/*.tmp'],
  })
  expect(h0.url).toBe('http://127.0.0.1:9990/hook')
  expect(h0.secret).toBe('s3cret')
  expect(h0.use_secret_for_signing).toBe(true)
  expect(body.debug).toBe(true)

  // 201 后列表刷新出行
  await expect(page.locator('[data-testid="wh-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="wh-row-ci-hook"]')).toBeVisible()
  await expect(page.locator('[data-testid="wh-count"]')).toContainText('1 个订阅')
  // 刷新腿（列表重读）
  await page.click('[data-testid="wh-refresh"]')
  await expect(page.locator('[data-testid="wh-row-ci-hook"]')).toBeVisible()
})

// ---- ①b 域切换：13 域分组 + 非托管域 criteria 说明 + 空范围警示 ----------

test('wh: domain closed set — dormant marks, unmanaged criteria note, empty-scope warn', async ({ page }) => {
  const io = await installMocks(page, { subs: [] })
  await openPage(page)

  await page.click('[data-testid="wh-empty-create"]')
  await expect(page.locator('[data-testid="wh-dialog"]')).toBeVisible()

  // 切到 curation 域：事件型是官方标题拼写（非 snake）+ 休眠标注；
  // criteria 五键不在该域 → 说明行呈现
  await page.selectOption('[data-testid="wh-form-domain"]', 'curation')
  await expect(page.locator('[data-testid="wh-form-type-Package was blocked by Curation"]')).toBeVisible()
  await expect(page.locator('[data-testid="wh-dialog"]')).toContainText('（休眠）')
  await expect(page.locator('[data-testid="wh-form-criteria-note"]')).toBeVisible()

  // 回 artifact 域：勾掉 anyLocal 且 repoKeys 为空 → 空范围警示（合法但不命中）
  await page.selectOption('[data-testid="wh-form-domain"]', 'artifact')
  await page.uncheck('[data-testid="wh-form-any-local"]')
  await expect(page.locator('[data-testid="wh-form-scope-warn"]')).toBeVisible()
  // 休眠域也允许创建（服务端校验绿——注册休眠语义）；放弃不提交
  expect(io.writes).toEqual([])
})

// ---- ② 编辑：key 锁定 + secret 留空 = 剔除键（哨兵不回传）/ 勾清除 = 擦除 ---

test('wh: edit dialog — key locked, secret preserved by omission, wipe sends empty string', async ({ page }) => {
  const io = await installMocks(page)
  await openPage(page)

  await page.click('[data-testid="wh-edit-ci-hook"]')
  await expect(page.locator('[data-testid="wh-dialog"]')).toBeVisible()
  // key 锁定（编辑态 disabled）
  await expect(page.locator('[data-testid="wh-form-key"]')).toBeDisabled()
  // secret 只写不回读：placeholder 提示「留空保持不变」；清除钮可用（已设置态）
  await expect(page.locator('[data-testid="wh-form-secret"]')).toHaveAttribute('placeholder', '已设置——留空保持不变')
  await expect(page.locator('[data-testid="wh-form-secret-clear"]')).toBeEnabled()
  await page.fill('[data-testid="wh-form-description"]', 'CI 触发器（改）')
  await page.click('[data-testid="wh-form-submit"]')

  await expect.poll(() => io.writes.map((w) => w.path)).toContain('PUT ci-hook')
  const put = JSON.parse(io.writes.find((w) => w.path === 'PUT ci-hook')!.body!) as Record<string, unknown>
  expect(put.key).toBe('ci-hook')
  expect(put.description).toBe('CI 触发器（改）')
  // 哨兵绝不回传：未动 secret → 键整个剔除（= 保持库存密文）
  const h0 = (put.handlers as Record<string, unknown>[])[0]!
  expect('secret' in h0).toBe(false)
  expect(h0.url).toBe('http://127.0.0.1:9990/hook')

  // 勾「清除已存 secret」→ PUT 体 secret = ""（擦除三态的第三态）
  await page.click('[data-testid="wh-edit-ci-hook"]')
  await page.check('[data-testid="wh-form-secret-clear"]')
  await page.click('[data-testid="wh-form-submit"]')
  await expect.poll(() => io.writes.filter((w) => w.path === 'PUT ci-hook').length).toBe(2)
  const wipe = JSON.parse(io.writes.filter((w) => w.path === 'PUT ci-hook')[1]!.body!) as Record<string, unknown>
  expect((wipe.handlers as Record<string, unknown>[])[0]!.secret).toBe('')
})

// ---- ③ 行内试发 + 草稿试发 --------------------------------------------------

test('wh: row test outcome panel + draft test inside dialog', async ({ page }) => {
  await installMocks(page, { onRecords: () => [] })
  await openPage(page)

  await page.click('[data-testid="wh-test-ci-hook"]')
  const last = page.locator('[data-testid="wh-test-last"]')
  await expect(last).toBeVisible()
  await expect(last).toContainText('HTTP 200')
  await expect(last).toContainText('37ms')

  // 草稿试发（对话框内——吃当前表单体）
  await page.click('[data-testid="wh-edit-ci-hook"]')
  await page.click('[data-testid="wh-form-test"]')
  const res = page.locator('[data-testid="wh-test-result"]')
  await expect(res).toBeVisible()
  await expect(res).toContainText('Test successful')
})

// ---- ④ 启停开关（PUT 全量体） ----------------------------------------------

test('wh: enable toggle issues full-body PUT', async ({ page }) => {
  const io = await installMocks(page)
  await openPage(page)

  await page.click('[data-testid="wh-toggle-ci-hook"]')
  await expect.poll(() => io.writes.map((w) => w.path)).toContain('PUT ci-hook')
  const put = JSON.parse(io.writes.find((w) => w.path === 'PUT ci-hook')!.body!) as Record<string, unknown>
  expect(put.enabled).toBe(false) // 行内开关翻转
  expect((put.event_filter as Record<string, unknown>).domain).toBe('artifact')
  expect(((put.handlers as Record<string, unknown>[])[0]!).handler_type).toBe('webhook')
})

// ---- ⑤ 删除 danger 确认 ----------------------------------------------------

test('wh: delete via danger confirm removes the row', async ({ page }) => {
  const io = await installMocks(page)
  await openPage(page)

  await page.click('[data-testid="wh-delete-ci-hook"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await page.click('[data-testid="confirm-accept"]')
  await expect.poll(() => io.writes.map((w) => w.path)).toContain('DELETE ci-hook')
  await expect(page.locator('[data-testid="wh-row-ci-hook"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="wh-empty"]')).toBeVisible()
})

// ---- ⑥ 详情抽屉 + 投递记录 --------------------------------------------------

test('wh: drawer detail + delivery records (status/elapsed/retries + payload)', async ({ page }) => {
  await installMocks(page, {
    subs: [
      subView(),
      subView({
        key: 'quiet-hook',
        description: '',
        enabled: false,
        event_filter: { domain: 'docker', event_types: ['pushed'], criteria: { anyLocal: true } },
        handlers: [
          {
            handler_type: 'webhook',
            url: 'http://127.0.0.1:9990/quiet',
            proxy: '',
            use_secret_for_signing: false,
            custom_http_headers: [],
            http_headers: [],
            secrets: [],
          },
        ],
      }),
    ],
    onRecords: (key) =>
      key === 'ci-hook'
        ? [
            record(),
            record({
              timestamp: 1756550410000,
              elapsed_millis: 15,
              errors: ['receiver answered 500'],
              request: { method: 'POST', url: 'http://127.0.0.1:9990/hook', headers: {}, payload: '{}', retries_attempted: 2 },
              response: { status: 500, headers: {}, body: 'boom' },
            }),
          ]
        : [],
  })
  await openPage(page)

  await page.click('[data-testid="wh-open-ci-hook"]')
  const drawer = page.locator('[data-testid="wh-drawer"]')
  await expect(drawer).toBeVisible()
  // 详情面：criteria mono、secret 已设置（write-only 掩码态）
  await expect(page.locator('[data-testid="wh-drawer-criteria"]')).toContainText('anyLocal')
  await expect(drawer).toContainText('已设置（write-only')

  // 记录表：两条（delivered + 500 重试）——状态/耗时/重试计数逐列
  const row0 = page.locator('[data-testid="wh-record-0"]')
  await expect(row0).toBeVisible()
  await expect(row0).toContainText('200 已送达')
  await expect(row0).toContainText('42ms')
  await expect(page.locator('[data-testid="wh-record-1"]')).toContainText('500')
  await expect(page.locator('[data-testid="wh-record-1"]')).toContainText('2')

  // 载荷快照展开：七字段信封 mono 呈现
  await row0.click()
  const payload = page.locator('[data-testid="wh-record-payload-0"]')
  await expect(payload).toBeVisible()
  await expect(payload).toContainText('"subscription_key"')
  await expect(payload).toContainText('"userContext"')

  // 刷新腿（排障环重读）
  await page.click('[data-testid="wh-records-refresh"]')
  await expect(page.locator('[data-testid="wh-records-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="wh-record-0"]')).toBeVisible()

  await page.click('[data-testid="wh-drawer-close"]')
  await expect(drawer).toHaveCount(0)

  // 空记录腿：无失败且未开 debug 的订阅 → 空态如实（空列表 ≠ 无投递）
  await page.click('[data-testid="wh-open-quiet-hook"]')
  await expect(page.locator('[data-testid="wh-records-empty"]')).toBeVisible()
  await page.click('[data-testid="wh-drawer-close"]')
})

// ---- ⑦ readonly_admin 只读臂（零写请求反断言） ------------------------------

test('wh: readonly_admin — reads visible, write controls disabled, zero write requests', async ({ page }) => {
  const io = await installMocks(page, { role: 'readonly_admin' })
  await openPage(page)

  await expect(page.locator('[data-testid="wh-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="wh-create"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="wh-row-ci-hook"]')).toBeVisible()
  // 详情抽屉可用（读面）
  await page.click('[data-testid="wh-open-ci-hook"]')
  await expect(page.locator('[data-testid="wh-drawer"]')).toBeVisible()
  await page.click('[data-testid="wh-drawer-close"]')
  // 写动作全部禁用
  for (const t of ['wh-test-ci-hook', 'wh-edit-ci-hook', 'wh-delete-ci-hook']) {
    await expect(page.locator(`[data-testid="${t}"]`)).toBeDisabled()
  }
  await expect(page.locator('[data-testid="wh-toggle-ci-hook"]')).toBeDisabled()
  // 反断言：全程零写动词
  expect(io.writes).toEqual([])
})

// ---- ⑧ community 槽锁定提示 + 写 403 呈现 -----------------------------------

test('wh: locked slot note + write refusal surfaces the 403', async ({ page }) => {
  await installMocks(page, {
    slot: { enabled: false, minTier: 'pro' },
    subs: [],
    onWrite: () => ({ status: 403, body: 'webhook requires a pro license' }),
  })
  await openPage(page)

  await expect(page.locator('[data-testid="wh-locked-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="wh-locked-note"]')).toContainText('pro')
  // 读面仍开放（D1）：空列表正常呈现，不是错误态
  await expect(page.locator('[data-testid="wh-empty"]')).toBeVisible()

  // 写被服务端 403 终裁：表单错误行呈现 403 语境
  await page.click('[data-testid="wh-create"]')
  await page.fill('[data-testid="wh-form-key"]', 'ci-hook')
  await page.fill('[data-testid="wh-form-url"]', 'http://127.0.0.1:9990/hook')
  await page.click('[data-testid="wh-form-submit"]')
  const err = page.locator('[data-testid="wh-form-error"]')
  await expect(err).toBeVisible()
  await expect(err).toContainText('403')
})

// ---- ⑨ 列表错误态 + 重试 ----------------------------------------------------

test('wh: list failure — error card + retry recovers', async ({ page }) => {
  let failing = true
  const io = await installMocks(page, {})
  // 覆盖列表路由（后注册者优先）：失败一次后恢复
  await page.route('**/binflow/event/api/v1/subscriptions', (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    if (failing) {
      failing = false
      return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify('storage is busy') })
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([subView()]) })
  })
  await openPage(page)

  await expect(page.locator('[data-testid="error-card"]')).toBeVisible()
  // 重试钮无 testid（ErrorCard 公共组件——不在本票 area）：按角色名定位
  await page.click('[data-testid="error-card"] button:has-text("重试")')
  // 重试后行出现 = 第二次 GET 成功（错误态未清空旧数据的腿在复制页）
  await expect(page.locator('[data-testid="wh-row-ci-hook"]')).toBeVisible()
  expect(io.writes).toEqual([])
})

// ---- ⑩ axe 双主题（页面 + Dialog + Drawer 开启态） ---------------------------

test('wh: axe both themes — page, dialog and drawer serious/critical = 0', async ({ page }) => {
  await installMocks(page, {
    onRecords: () => [record(), record({ errors: ['send failed: x'], response: { status: 0, headers: {}, body: '' } })],
  })
  for (const theme of ['light', 'dark'] as const) {
    await page.goto('/binflow/ui/login')
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    await openPage(page)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expectAxeClean(page, `wh-page ${theme}`)

    await page.click('[data-testid="wh-create"]')
    await expect(page.locator('[data-testid="wh-dialog"]')).toBeVisible()
    await expectAxeClean(page, `wh-dialog ${theme}`)
    await page.click('[data-testid="wh-form-cancel"]')

    await page.click('[data-testid="wh-open-ci-hook"]')
    await expect(page.locator('[data-testid="wh-record-0"]')).toBeVisible()
    await expectAxeClean(page, `wh-drawer ${theme}`)
    await page.click('[data-testid="wh-drawer-close"]')
  }
})
