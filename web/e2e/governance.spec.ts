import { expect, test } from '@playwright/test'

// T-102 探针：治理组三页全链——审计（过滤 + keyset 分页 + REST 对账）、
// GC（dry-run → type-to-confirm apply → 复核 0 候选 + gc.run 审计落账）、
// 配额（水位三态 + 行内编辑上限 REST 对账 + 413 语义）；非 admin 403
// 收敛（L1 导航隐藏 / L2 无权限卡）。全程监听服务端 5xx（票面：零 5xx）。
//
// 运行前提：已 `make console && make build` 的真二进制在前台 serve，
// BASE 指向它（同 T-98/T-99 探针约定）；ADMIN_PW 默认 password（PRD §4）。
// 对账腿走 page.evaluate fetch（同源携带 session cookie）。
//
// 遗留（见工作日志）：非 admin 探针用户无法经 API 清理（M4 无用户删除
// 端点），以唯一名留在实例中；GC 409 互斥需要真实并发持有者（export
// CLI），浏览器面无法稳定构造，归 T-103（W25b 真机时序）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'

// 串行执行：GC（graceHours=0 的 dry-run/apply）与配额/审计造数在同一实例
// 上互踩——并发上传的 blob 在节点提交前是瞬时 GC 候选，apply 也会真实
// 删文件。实例级维护操作天然要求静默实例（W25 同款前提）。
test.describe.configure({ mode: 'serial' })

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: import('@playwright/test').Page, user = ADMIN, pw = ADMIN_PW) {
  await page.fill('[data-testid="login-username"]', user)
  await page.fill('[data-testid="login-password"]', pw)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** 同源 fetch（携带 session cookie）；返回 {status, text}。
 *  body 为字符串时原样字节直发（内容面 PUT 的 quota 断言依赖精确字节，
 *  JSON.stringify 会给字符串加一对引号——8192 变 8194）；对象走 JSON。 */
async function api(
  page: import('@playwright/test').Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; text: string }> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const isString = typeof body === 'string'
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined && !isString ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? (isString ? body : JSON.stringify(body)) : undefined,
      })
      return { status: res.status, text: await res.text() }
    },
    { method, path, body },
  )
}

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 服务端 5xx 监听（票面 AC：零 5xx） */
function watchServerErrors(page: import('@playwright/test').Page): string[] {
  const bad: string[] = []
  page.on('response', (r) => {
    if (r.status() >= 500) bad.push(`${r.status()} ${r.url()}`)
  })
  return bad
}

/** T-172 D-4 放行项：GC 页挂载 MigrationPanel，每 5s 轮询
 *  GET /api/v1/storage/migration；未配置 S3 迁移的实例按 T-164 既定语义
 *  返 501「migration is not configured」（UI 降级为 migration-unconfigured
 *  提示，非服务端故障）——「零 5xx」账面对该预期态放行（仅 GC 用例会
 *  触发：MigrationPanel 只挂在 GCPage）。 */
const MIGRATION_501 = /^501 \S+\/binflow\/api\/v1\/storage\/migration$/

test('audit: filters, keyset load-more, path client-filter, REST parity', async ({ page }) => {
  // 负载余量（T-172 D-2 同族的 e2e 面）：105 次造数 PUT + 多段过滤/分页
  // 腿在并行负载下逼近默认 30s——本机两连超时（31.0s，造数腿独占 ~20s），
  // 加倍消化调度抖动；断言本体不变（T-159 曾记同点位顺序性 flake）
  test.setTimeout(60_000)
  const errors = watchServerErrors(page)
  const key = uniq('t102a')
  await page.goto('/binflow/ui/audit')
  await login(page)

  // 造数：建仓 + 105 个 deploy（首屏 100 + 第二页）
  const created = await api(page, 'PUT', `/api/repositories/${key}`, {
    rclass: 'local',
    packageType: 'generic',
  })
  expect(created.status).toBeLessThan(300)
  await page.evaluate(
    async ({ key }) => {
      for (let i = 0; i < 105; i++) {
        const res = await fetch(`/binflow/${key}/probe/file-${i}.bin`, {
          method: 'PUT',
          body: `t102-payload-${i}`,
        })
        if (res.status !== 201) throw new Error(`seed PUT ${i} -> ${res.status}`)
      }
    },
    { key },
  )

  // REST 对账基线：同过滤全量（repo 精确匹配）——审计落账在请求路径上
  // 同步完成，poll 只是给慢盘留余量
  let total = 0
  await expect
    .poll(
      async () => {
        const r = await api(page, 'GET', `/api/v1/audit?repo=${key}&limit=1000`)
        total = r.status === 200 ? ((JSON.parse(r.text).events ?? []).length as number) : 0
        return total
      },
      { timeout: 10_000 },
    )
    .toBeGreaterThanOrEqual(106) // 105 deploy + repo.create

  // UI：repo 过滤（350ms 防抖）→ 首屏 100 行 + 「加载更多」。
  // 防抖窗口内未过滤首页也是 100 行（造数后最新事件本就属于本仓），
  // 先等防抖落地，再以「无本仓外行」钉死过滤态，消除同数歧义。
  await page.fill('[data-testid="audit-filter-repo"]', key)
  await page.waitForTimeout(700)
  await expect(page.locator('[data-testid="audit-table"] tbody tr')).toHaveCount(100, {
    timeout: 10_000,
  })
  await expect(
    page.locator(`[data-testid="audit-table"] tbody tr:not(:has-text("${key}/"))`),
  ).toHaveCount(0)
  await expect(page.locator('[data-testid="audit-more"]')).toBeVisible()

  // B1 回归腿（review 修复的晚到响应竞态）：拦截带 cursor 的第 2 页请求
  // 挂起，期间切换过滤（action=deploy）→ 防抖首页重拉落地后放行旧第 2 页
  // ——守卫必须丢弃：列表不得被旧过滤页污染、nextCursor 不得被旧游标覆写
  // （判据 = 后续「加载更多」在新过滤下正确续页到 105，而非旧链的 106）。
  let releaseP2: (() => void) | null = null
  await page.route('**/api/v1/audit*', async (route) => {
    const url = new URL(route.request().url())
    if (url.searchParams.has('cursor')) {
      await new Promise<void>((resolve) => {
        releaseP2 = resolve
      })
    }
    await route.continue()
  })
  await page.click('[data-testid="audit-more"]') // 第 2 页在飞（挂起，未出浏览器）
  await page.selectOption('[data-testid="audit-filter-action"]', 'deploy') // 350ms 防抖后首页重拉
  await page.waitForTimeout(700) // 新过滤首页（100 行 deploy）已落地
  expect(releaseP2).toBeTruthy()
  // TS CFA 不追踪路由闭包内的赋值（142 行原收窄为 null → TS2349）：
  // 断言后经持有者别名放行，语义不变（review B2 修法①）
  const release2 = releaseP2 as (() => void) | null
  release2?.() // 放行旧过滤的第 2 页
  await page.waitForTimeout(400)
  await expect(page.locator('[data-testid="audit-table"] tbody tr')).toHaveCount(100) // 未被旧页污染
  await page.unroute('**/api/v1/audit*') // 先撤拦截——后续合法的带 cursor 请求不得再被挂起
  // 新过滤下正确续页：deploy 105 条 → 第 2 页 +5（若游标被旧链覆写会到 106）
  const restDeploy = JSON.parse(
    (await api(page, 'GET', `/api/v1/audit?repo=${key}&action=deploy&limit=1000`)).text,
  ).events.length as number
  expect(restDeploy).toBe(105)
  await page.click('[data-testid="audit-more"]')
  await expect(page.locator('[data-testid="audit-table"] tbody tr')).toHaveCount(restDeploy)

  // 回全量（repo-only）并核对 keyset 末页终止
  await page.selectOption('[data-testid="audit-filter-action"]', '')
  await page.waitForTimeout(700)
  while ((await page.locator('[data-testid="audit-more"]').count()) > 0) {
    await page.click('[data-testid="audit-more"]')
  }
  await expect(page.locator('[data-testid="audit-table"] tbody tr')).toHaveCount(total)
  await expect(page.locator('[data-testid="audit-more"]')).toHaveCount(0) // 末页游标为空
  await expect(page.locator('[data-testid="audit-count"]')).toContainText(`已加载 ${total} 条`)

  // path 过滤：仅已加载集（客户端子串，§6.3 兜底）——上一段已回全量
  await page.fill('[data-testid="audit-filter-path"]', 'file-1')
  const filtered = await page.locator('[data-testid="audit-table"] tbody tr').count()
  expect(filtered).toBeGreaterThan(0)
  expect(filtered).toBeLessThan(total) // file-1* 子串（file-1、file-10..19、file-100..105）
  await expect(page.locator('[data-testid="audit-count"]')).toContainText('路径过滤仅作用于已加载集')

  // 时间窗：until 设在过去 → 当前过滤下为空（含「清除过滤」出口）
  await page.fill('[data-testid="audit-filter-until"]', '2000-01-01T00:00')
  await expect(page.locator('[data-testid="audit-empty-filtered"]')).toBeVisible({ timeout: 10_000 })
  // since 在远古 + until 清空 → 恢复全量
  await page.fill('[data-testid="audit-filter-since"]', '2000-01-01T00:00')
  await page.fill('[data-testid="audit-filter-until"]', '')
  await expect(page.locator('[data-testid="audit-table"] tbody tr').first()).toBeVisible({
    timeout: 10_000,
  })
  // 行为原样 mono + 对象列拷贝按钮在
  await expect(page.locator('[data-testid="audit-row-0"] td').nth(2)).toHaveText('deploy')

  // 收尾
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
  expect(errors).toEqual([])
})

test('gc: dry-run -> typed confirm apply -> zero candidates after, gc.run audited', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t102g')
  await page.goto('/binflow/ui/governance/gc')
  await login(page)

  // 造孤儿：上传后删节点（blob 留存）。默认 grace 24h 内不是候选——
  // 走 graceHours=0（无宽限窗口）才能命中（W24 手法）。
  await api(page, 'PUT', `/api/repositories/${key}`, { rclass: 'local', packageType: 'generic' })
  const put = await api(page, 'PUT', `/${key}/orphan.bin`, 't102-gc-orphan-payload')
  expect(put.status).toBe(201)
  const del = await api(page, 'DELETE', `/${key}/orphan.bin`)
  expect(del.status).toBeLessThan(300)

  // 概况卡到达（与仪表盘同源）
  await expect(page.locator('[data-testid="gc-stats"]')).toBeVisible()
  await expect(page.locator('[data-testid="gc-stats"]')).toContainText('blob')

  // T-172 D-4：同页 MigrationPanel 探测 /api/v1/storage/migration 得 501
  // （未配置 S3 迁移，T-164 既定语义）→ 面板降级为一句提示而非错误态
  await expect(page.locator('[data-testid="migration-unconfigured"]')).toBeVisible()

  // 无 dry-run 时 apply 不可用（ADR-0015 勘误①：先审后执行）
  await expect(page.locator('[data-testid="gc-apply"]')).toBeDisabled()

  // 高级项：graceHours=0 → dry-run 至少 1 候选
  await page.click('[data-testid="gc-danger-zone"] summary')
  await page.fill('[data-testid="gc-grace-hours"]', '0')
  await page.click('[data-testid="gc-dryrun"]')
  await expect(page.locator('[data-testid="gc-result"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="gc-result"]')).toContainText('候选 blob')
  const dryText = await page.locator('[data-testid="gc-result"]').innerText()
  const dryCount = Number(dryText.match(/(\d+)\s*项/)?.[1] ?? 0)
  expect(dryCount).toBeGreaterThanOrEqual(1)

  // grace 参数变更 → 试运行结果过期，apply 关回
  await page.fill('[data-testid="gc-grace-hours"]', '1')
  await expect(page.locator('[data-testid="gc-apply"]')).toBeDisabled()
  await page.fill('[data-testid="gc-grace-hours"]', '0')
  await expect(page.locator('[data-testid="gc-apply"]')).toBeEnabled()

  // apply 二次确认：输错不可用；输入 YES 执行
  await page.click('[data-testid="gc-apply"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await page.fill('[data-testid="gc-confirm-text"]', 'yes-wrong')
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeDisabled()
  await page.fill('[data-testid="gc-confirm-text"]', 'YES')
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeEnabled()
  await page.click('[data-testid="confirm-accept"]')

  await expect(page.locator('[data-testid="gc-result"]')).toContainText('实际回收', { timeout: 15_000 })
  await expect(page.locator('[data-testid="toast"]')).toContainText('GC 完成')
  const appliedText = await page.locator('[data-testid="gc-result"]').innerText()
  const appliedDeleted = Number(appliedText.match(/(\d+)\s*项/)?.[1] ?? -1)
  expect(appliedDeleted).toBe(dryCount)

  // curl stats 对账腿（浏览器等价）：gc.run 落审计且 deletedCount 一致
  const auditRes = await api(page, 'GET', `/api/v1/audit?action=gc.run&limit=1`)
  expect(auditRes.status).toBe(200)
  const gcEvent = JSON.parse(auditRes.text).events[0]
  expect(gcEvent.detail.deletedCount).toBe(appliedDeleted)
  expect(gcEvent.detail.graceHours).toBe(0)

  // 再 dry-run：0 候选（绿色空态——好消息）
  await page.click('[data-testid="gc-dryrun"]')
  await expect(page.locator('[data-testid="gc-empty-ok"]')).toBeVisible({ timeout: 15_000 })

  // 收尾
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
  // 零 5xx 账面：放行 MigrationPanel 的 501-not-configured 预期态（D-4，
  // 见 MIGRATION_501 注释——上面已正向断言其降级呈现）
  expect(errors.filter((e) => !MIGRATION_501.test(e))).toEqual([])
})

test('quotas: water levels warn/full, inline edit roundtrip, 413 at ceiling', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t102q')
  await page.goto('/binflow/ui/governance/quotas')
  await login(page)

  // quota 10240B：8KB（80% 黄）→ +2KB（100% 红）→ 再写 413。
  // 列表/用量在页面挂载时拉取（无轮询）——每次 API 侧变更后 reload 对账。
  await api(page, 'PUT', `/api/repositories/${key}`, {
    rclass: 'local',
    packageType: 'generic',
    quotaBytes: 10240,
  })
  const p1 = await api(page, 'PUT', `/${key}/a.bin`, 'a'.repeat(8 * 1024))
  expect(p1.status).toBe(201)

  await page.reload()
  await expect(page.locator(`[data-testid="quota-row-${key}"]`)).toBeVisible({ timeout: 10_000 })
  await expect(page.locator(`[data-testid="quota-bar-${key}"] .pct`)).toHaveText(/^80% 高$/)
  await expect(page.locator(`[data-testid="quota-bar-${key}"] .water-bar`)).toHaveClass(/warn/)

  const p2 = await api(page, 'PUT', `/${key}/b.bin`, 'b'.repeat(2 * 1024))
  expect(p2.status).toBe(201)
  await page.reload()
  await expect(page.locator(`[data-testid="quota-bar-${key}"] .pct`)).toHaveText(/^100% 满$/, {
    timeout: 10_000,
  })
  await expect(page.locator(`[data-testid="quota-bar-${key}"] .water-bar`)).toHaveClass(/full/)

  // 超限：413 + message 双值（quota exceeded）
  const p3 = await api(page, 'PUT', `/${key}/c.bin`, 'c'.repeat(1024))
  expect(p3.status).toBe(413)
  expect(p3.text).toContain('quota exceeded')

  // 行内编辑上限：10240 → 40960 → REST 对账 + 水位回落
  await page.click(`[data-testid="quota-edit-${key}"]`)
  await page.fill(`[data-testid="quota-input-${key}"]`, '40960')
  await page.click(`[data-testid="quota-save-${key}"]`)
  await expect(page.locator('[data-testid="toast"]')).toContainText('update successfully')
  const usage = JSON.parse((await api(page, 'GET', `/api/v1/storage/usage/${key}`)).text)
  expect(usage.quotaBytes).toBe(40960)
  expect(usage.usedBytes).toBe(10240)
  await expect(page.locator(`[data-testid="quota-bar-${key}"] .pct`)).toHaveText(/^25%$/, {
    timeout: 10_000,
  })

  // 跳转仓库设置可达
  await page.click(`[data-testid="quota-row-${key}"] a:has-text("仓库设置")`)
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${key}/edit$`))
  await expect(page.locator('[data-testid="repo-form-page"]')).toBeVisible()

  // 收尾
  await api(page, 'DELETE', `/api/repositories/${key}?deleteContent=true`)
  expect(errors).toEqual([])
})

test('non-admin: governance nav hidden (L1), deep links show no-access card (L2)', async ({
  page,
  browser,
}) => {
  const errors = watchServerErrors(page)
  const viewer = uniq('t102view')
  // 管理面造数走 admin 页（page.evaluate 的相对 fetch 需先落在 http 文档上）
  await page.goto('/binflow/ui/')
  await login(page)
  // 非 admin 探针用户（M4 无用户删除端点——唯一名留存，见工作日志遗留）
  const created = await api(page, 'PUT', `/api/security/users/${viewer}`, {
    name: viewer,
    email: `${viewer}@example.com`,
    password: 't102-viewer-pw',
    admin: false,
  })
  expect(created.status).toBeLessThan(300)

  const ctx = await browser.newContext()
  const p2 = await ctx.newPage()
  const errors2 = watchServerErrors(p2)
  await p2.goto('/binflow/ui/audit')
  await login(p2, viewer, 't102-viewer-pw')

  // L1：治理分组（含审计/GC/备份/配额）与安全分组整体隐藏
  await expect(p2.locator('.app-nav', { hasText: '治理' })).toHaveCount(0)
  await expect(p2.locator('.app-nav .nav-item', { hasText: '审计日志' })).toHaveCount(0)

  // L2：直链渲染页面壳 + 单张无权限卡（不留空白壳）
  await expect(p2.locator('[data-testid="audit-page"] [data-testid="empty-state"]')).toBeVisible()
  await expect(p2.locator('[data-testid="audit-page"]')).toContainText('无权限查看审计日志')
  await p2.goto('/binflow/ui/governance/gc')
  await expect(p2.locator('[data-testid="gc-page"]')).toContainText('无权限查看存储概况')
  // L4：写入口（危险区）不渲染
  await expect(p2.locator('[data-testid="gc-danger-zone"]')).toHaveCount(0)
  await p2.goto('/binflow/ui/governance/quotas')
  await expect(p2.locator('[data-testid="quotas-page"]')).toContainText('无权限查看配额')

  await ctx.close()
  expect(errors).toEqual([])
  expect(errors2).toEqual([])
})
