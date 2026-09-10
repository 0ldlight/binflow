import { test, expect } from '@playwright/test'

import { loginAs } from '../m8/support/roles'
import { expectA11yClean } from '../m8/support/a11y'
import { m8Client } from '../m8/support/seed'

// FE-P4 B 段：MUI 残留四件迁新栈壳的迁移回归（Radix Dialog sheet/Dialog +
// Tailwind——行为契约逐条保真，锚族零改名）。
//
// 四腿：
//  ① SetMe Up（Radix 右向 sheet）：树页入口开 → smu-dialog 在场 + 焦点圈进
//    （activeElement 落面板内）→ Tab 三枚键盘方向键 → 完成 → 关闭 + 回焦
//    启动钮（FocusScope 卸载回焦）。
//  ② Deploy（Radix Dialog）：候选仓回显 + 拖拽区在场 + axe（开态）。
//  ③ Properties（新栈表单族）：文件节点属性页签 → 常显表单加键值 → 行呈现
//    → 行内删除过危险确认（ConfirmDialog 的 Radix 化同测）。
//  ④ Replications（新栈内嵌节）：仓编辑页 ?section=replications 直落 →
//    列表/降级/表单三态其一呈现 + 表单 Test/提交钮在场（真 CRUD 归
//    t404 spec 的 mock 面——本 spec 只锁迁移后的形态与锚）。
//
// 锚源：console-ux §10 既有 smu-* / deploy-* / node-props-* / repl-* /
// confirm-* 全族（§10.9 P4 批 MUI 清场留痕行）。
function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

const marker = uniq('p4mui')

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test.beforeAll(async () => {
  const client = m8Client()
  await client
    .request('PUT', `/binflow/api/repositories/${marker}-local`, { body: { rclass: 'local', packageType: 'generic' } })
    .catch(() => undefined)
  await client
    .request('PUT', `/binflow/${marker}-local/p4/migration-probe.bin`, {
      raw: true,
      body: 'p4-mui-bytes',
      headers: { 'content-type': 'application/octet-stream' },
    })
    .catch(() => undefined)
})

test.afterAll(async () => {
  await m8Client()
    .request('DELETE', `/binflow/api/repositories/${marker}-local?deleteContent=true`)
    .catch(() => undefined)
})

test('setmeup: Radix sheet opens from tree header, tabs keyboard-navigate, close returns focus', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${marker}-local`)

  await page.click('[data-testid="tree-setmeup"]')
  const dialog = page.locator('[data-testid="smu-dialog"]')
  await expect(dialog).toBeVisible()
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(`${marker}-local`)
  // 焦点圈进（Radix FocusScope——activeElement 落面板内）
  await expect
    .poll(() => page.evaluate(() => !!document.activeElement?.closest('[data-testid="smu-dialog"]')))
    .toBe(true)

  // Tab 三枚：方向键循环 + aria-selected
  await page.focus('[data-testid="smu-tab-configure"]')
  await page.keyboard.press('ArrowRight')
  await expect(page.locator('[data-testid="smu-tab-deploy"]')).toBeFocused()
  await expect(page.locator('[data-testid="smu-tab-deploy"]')).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('ArrowRight')
  await expect(page.locator('[data-testid="smu-tab-resolve"]')).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(page.locator('[data-testid="smu-tab-deploy"]')).toBeFocused()

  // 完成关闭 + 回焦启动钮
  await page.click('[data-testid="smu-done"]')
  await expect(dialog).toHaveCount(0)
  await expect(page.locator('[data-testid="tree-setmeup"]')).toBeFocused()
})

test('deploy: Radix dialog shows candidates and dropzone, axe clean', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${marker}-local`)

  await page.click('[data-testid="tree-deploy"]')
  const dialog = page.locator('[data-testid="deploy-dialog"]')
  await expect(dialog).toBeVisible()
  await expect(page.locator('[data-testid="deploy-repo"]')).toHaveValue(`${marker}-local`)
  await expect(page.locator('[data-testid="deploy-drop"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="deploy-dialog"]' })

  await page.click('[data-testid="deploy-close"]')
  await expect(dialog).toHaveCount(0)
})

test('properties: new-stack form adds and deletes a property via danger confirm', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/properties/${marker}-local/p4/migration-probe.bin`)

  await expect(page.locator('[data-testid="node-props"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-props-empty"]')).toBeVisible()

  // 常显表单：键值 + Add（表单族 = .field 原生输入）
  await page.fill('[data-testid="node-props-key-input"]', 'p4mig')
  await page.fill('[data-testid="node-props-values-input-p4mig"]', 'alpha, beta')
  await expect(page.locator('[data-testid="node-props-add"]')).toBeEnabled()
  await page.click('[data-testid="node-props-add"]')
  const row = page.locator('[data-testid="node-props-row-p4mig"]')
  await expect(row).toBeVisible()
  await expect(row).toContainText('alpha, beta')

  // 行内删除过危险确认：先取消（行仍在），再确认（行消失）
  await page.click('[data-testid="node-props-delete-p4mig"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await page.click('[data-testid="confirm-cancel"]')
  await expect(row).toBeVisible()
  await page.click('[data-testid="node-props-delete-p4mig"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="node-props-empty"]')).toBeVisible()
})

test('replications: new-stack inline section renders via ?section deep link', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/admin/repositories/${marker}-local/edit?section=replications`)

  const section = page.locator('[data-testid="form-section-replications"]')
  await expect(section).toBeVisible()

  // 三态其一：空态引导（repl-empty + 建配置钮）/ 列表 / 降级注记——本实例
  // 复制端点在场（replication.spec 先例），seed 仓无配置 → 空态
  const empty = page.locator('[data-testid="repl-empty"]')
  const list = page.locator('[data-testid="repl-list"]')
  const degraded = page.locator('[data-testid="repl-degraded"]')
  await expect(empty.or(list).or(degraded).first()).toBeVisible()

  // 空态臂：建配置钮开内嵌表单（字段族 + 预留位恒禁用 + Test/提交）
  if (await empty.isVisible()) {
    await page.click('[data-testid="repl-create"]')
    await expect(page.locator('[data-testid="repl-form"]')).toBeVisible()
    await expect(page.locator('[data-testid="repl-form-name"]')).toBeVisible()
    await expect(page.locator('[data-testid="repl-form-url"]')).toBeVisible()
    await expect(page.locator('[data-testid="repl-form-event"]')).toBeDisabled()
    await expect(page.locator('[data-testid="repl-test"]')).toBeVisible()
    await expect(page.locator('[data-testid="repl-form-submit"]')).toBeDisabled()
    await page.click('[data-testid="repl-form-cancel"]')
    await expect(page.locator('[data-testid="repl-form"]')).toHaveCount(0)
  }
})
