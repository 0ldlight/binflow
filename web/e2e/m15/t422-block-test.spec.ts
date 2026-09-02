import { expect, test } from '@playwright/test'

import { loginAs } from '../m8/support/roles'
import { makeClient, m8Client, seedRepos } from '../m8/support/seed'

// T-422（M15 B7，FR-138.2/138.3）——复制包 B 第二面的真栈交互断言：
// 表单「测试连接」（repl-test / repl-test-result）+ 治理页全局封锁双开关
// （repl-global-block / repl-block-push / repl-block-pull）。T-422 交付时
// 本机无起栈窗口（报告 §4 诚实注明），本 spec 为 QA 中期（T-421）补腿，
// 消费 anchor-audit dead 桶中的五个新锚（console-ux v1.30）。
//
// 断言面：
//   ① 表单 Test（创建态 = 草稿面 POST /v1/replications/test，§9.2-C-9）：
//      不可达目标 → 内联判定块（repl-test-result）锚定 unknown host 文案 +
//      「未触达目标」；零副作用（不落任何配置行）。网络层对账 = 请求确实
//      打在 id 无关草稿面上。
//   ② 自实例拒绝（§9.3 语义位）：target = 本实例 origin → 锚定文案
//      "Cannot replicate to the same instance: ..."。
//   ③ 真实第二实例成功腿（BASE2 门控——净双实例编排时才有）：锚定成功串
//      "tested successfully" +「目标可达且凭据被接受」+ HTTP 200 应答回显。
//   ④ 全局封锁卡：两方向独立翻转（§9.2-B-3 四变体锚定文案逐字经 toast）
//      + REST GET 官方 camelCase 键复核 + 封锁下配置面照常（UI-API 不受
//      门，t226 实证语义）+ 方向独立性（blockPull=on 时 push run 触发照常
//      200——空仓 scheduled:0）。
//   ⑤ readonly_admin：封锁卡只读呈现（开关禁用）。
//
// 并行纪律注记：blockPush=on 会令 t404 列表臂的 ▶ 真触发吃 409（全量
// chromium 4 workers 并行），故 push 方向的封锁窗压到「toast 即翻回」的
// 最小形态；「封锁下配置面照常 / run 触发」两腿全部放在 blockPull 窗内
// 执行（blockPull 不门 push 轨——这本身就是要钉的方向独立性）。
//
// 锚源：console-ux §10.5 v1.30（repl-test / repl-test-result /
// repl-global-block / repl-block-push / repl-block-pull——既有 repl-* 零改名）。

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 治理页封锁卡的 REST 真值读（admin Basic；官方 camelCase 键，§9.1-B） */
async function blockFlags(): Promise<{ push: boolean; pull: boolean }> {
  const res = await m8Client().request('GET', '/binflow/api/v1/system/replications')
  const body = JSON.parse(res.text) as Record<string, boolean>
  return { push: body.blockPushReplications === true, pull: body.blockPullReplications === true }
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

/** 把封锁态清零（幂等前置——spec 重跑/前序崩溃残留不误读初态） */
async function ensureUnblocked() {
  const f = await blockFlags()
  if (f.push || f.pull) {
    await m8Client().request('POST', '/binflow/api/v1/system/replications/unblock?push=true&pull=true')
  }
}

// ---- 1. 草稿面 Test：不可达目标 → 内联锚定文案 + 零副作用 -------------------

test('admin: form test-connection (draft face) — unreachable target fails inline, nothing persisted', async ({
  page,
}) => {
  const key = uniq('t422a')
  await seedRepos(m8Client(), [{ key }])

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await page.click('[data-testid="repl-create"]')
  await page.fill('[data-testid="repl-form-url"]', 'https://t422-nohost.invalid')
  await page.fill('[data-testid="repl-form-target-repo"]', `${key}-dr`)

  // 网络层对账：草稿面（id 无关）承载本次探测
  const posted = page.waitForRequest(
    (r) => r.method() === 'POST' && r.url().endsWith('/api/v1/replications/test'),
  )
  await page.click('[data-testid="repl-test"]')
  const req = await posted
  expect(req.url()).not.toMatch(/\/replications\/\d+\/test$/)

  // 内联判定块（非 toast、非表单错误）：锚定 unknown host 文案 + 未触达
  const verdict = page.locator('[data-testid="repl-test-result"]')
  await expect(verdict).toBeVisible({ timeout: 15_000 })
  await expect(verdict).toContainText("unknown host 't422-nohost.invalid'")
  await expect(verdict).toContainText('探测未通过')
  await expect(verdict).toContainText('未触达目标')
  await expect(page.locator('[data-testid="repl-form-error"]')).toHaveCount(0)

  // 零副作用（§9.2-C 探测不落盘）：该仓零配置行
  const list = await m8Client().request('GET', '/binflow/api/v1/replications')
  const rows = (JSON.parse(list.text) as Array<Record<string, unknown>>).filter(
    (c) => c.source_repo === key,
  )
  expect(rows).toHaveLength(0)
})

// ---- 2. 自实例拒绝：target = 本实例 origin -----------------------------------

test('admin: form test-connection — self-instance candidate refused with the anchored wording', async ({
  page,
}) => {
  const key = uniq('t422b')
  await seedRepos(m8Client(), [{ key }])

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await page.click('[data-testid="repl-create"]')
  const origin = await page.evaluate(() => window.location.origin)
  await page.fill('[data-testid="repl-form-url"]', origin)
  await page.fill('[data-testid="repl-form-target-repo"]', key)

  await page.click('[data-testid="repl-test"]')
  const verdict = page.locator('[data-testid="repl-test-result"]')
  await expect(verdict).toBeVisible({ timeout: 15_000 })
  await expect(verdict).toContainText('Cannot replicate to the same instance')
  await expect(verdict).toContainText(origin)
})

// ---- 3. 真实第二实例成功腿（BASE2 门控——双实例编排时启用） -------------------

test('admin: form test-connection — live second instance passes (requires BASE2)', async ({
  page,
}) => {
  test.skip(!process.env.BASE2, 'BASE2 not set — run with the two-instance harness to enable this leg')
  const key = uniq('t422c')
  await seedRepos(m8Client(), [{ key }])
  // 目标侧（BASE2）自备一个 local 仓 + admin 凭据（探测 GET storage 带 Basic）
  const b2 = makeClient({
    base: process.env.BASE2!,
    username: 'admin',
    password: process.env.ADMIN_PW2 ?? 'password',
  })
  await seedRepos(b2, [{ key: `${key}-dr` }])

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await page.click('[data-testid="repl-create"]')
  await page.fill('[data-testid="repl-form-url"]', process.env.BASE2!)
  await page.fill('[data-testid="repl-form-target-repo"]', `${key}-dr`)
  await page.fill('[data-testid="repl-form-username"]', 'admin')
  await page.fill('[data-testid="repl-form-password"]', process.env.ADMIN_PW2 ?? 'password')

  await page.click('[data-testid="repl-test"]')
  const verdict = page.locator('[data-testid="repl-test-result"]')
  await expect(verdict).toBeVisible({ timeout: 15_000 })
  await expect(verdict).toContainText('tested successfully')
  await expect(verdict).toContainText('目标可达且凭据被接受')
  await expect(verdict).toContainText('HTTP 200')
})

// ---- 4. 全局封锁卡：双方向翻转 + REST 复核 + 封锁下配置面照常 ----------------

test('admin: global block card — both directions flip with anchored toasts; config plane stays open; run trigger direction-independent', async ({
  page,
}) => {
  await ensureUnblocked()
  const key = uniq('t422d')
  await seedRepos(m8Client(), [{ key }])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/governance/replication')
  const card = page.locator('[data-testid="repl-global-block"]')
  await expect(card).toBeVisible()
  const pushSwitch = page.locator('[data-testid="repl-block-push"]')
  const pullSwitch = page.locator('[data-testid="repl-block-pull"]')
  await expect(pushSwitch).toBeEnabled()
  await expect(pullSwitch).toBeEnabled()
  await expect(pushSwitch).not.toBeChecked()
  await expect(pullSwitch).not.toBeChecked()

  // push 方向：toast 即翻回（最小窗——并行纪律注记，文件头）
  await pushSwitch.click()
  await expect(page.locator('[data-testid="toast"]').last()).toContainText(
    'Successfully blocked all push replications, no push replication will be triggered.',
  )
  await expect(pushSwitch).toBeChecked()
  expect((await blockFlags()).push).toBe(true)
  await pushSwitch.click()
  await expect(page.locator('[data-testid="toast"]').last()).toContainText(
    'Successfully unblocked all push replications.',
  )
  await expect(pushSwitch).not.toBeChecked()
  expect((await blockFlags()).push).toBe(false)

  // pull 方向：本窗内同时钉「封锁不拦配置面 + push run 不受 blockPull 门」
  await pullSwitch.click()
  await expect(page.locator('[data-testid="toast"]').last()).toContainText(
    'Successfully blocked all pull replications, no pull replication will be triggered.',
  )
  await expect(pullSwitch).toBeChecked()
  expect((await blockFlags()).pull).toBe(true)

  // UI-API 不受门（t226 实证语义）：封锁期配置面照常写
  const name = uniq('t422cfg').toLowerCase()
  const created = await seedConfig(key, name)
  expect(created.status).toBe(201)
  // 方向独立性：blockPull=on 时空仓 push run 触发照常 200（scheduled:0）
  const id = (JSON.parse(created.text) as Record<string, unknown>).id as number
  const run = await m8Client().request('POST', `/binflow/api/v1/replications/${id}/run`)
  expect(run.status).toBe(200)
  expect((JSON.parse(run.text) as Record<string, unknown>).scheduled).toBe(0)

  await pullSwitch.click()
  await expect(page.locator('[data-testid="toast"]').last()).toContainText(
    'Successfully unblocked all pull replications.',
  )
  expect((await blockFlags()).pull).toBe(false)
})

// ---- 5. readonly_admin：封锁卡只读呈现 ---------------------------------------

test('readonly_admin: global block card renders read-only — both switches disabled', async ({
  page,
}) => {
  await ensureUnblocked()
  await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/admin/governance/replication')
  await expect(page.locator('[data-testid="repl-global-block"]')).toBeVisible()
  await expect(page.locator('[data-testid="repl-block-push"]')).toBeDisabled()
  await expect(page.locator('[data-testid="repl-block-pull"]')).toBeDisabled()
  // 只读会话注记（R8 卡的 readonly 提示行）
  await expect(page.locator('[data-testid="repl-global-block"]')).toContainText('只读管理员')
})
