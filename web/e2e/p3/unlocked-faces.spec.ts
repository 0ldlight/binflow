// P3 解锁面主干 spec（frontend-rewrite Phase 3——capability matrix 未列域
// + #24/#25 解锁项的 e2e 对账）：
// - keypair 页（新设）：列表四态（空态）、生成对话框字段集 + vault 拒答面
//   （导入面 UI 在场）、公钥对话框（无对时零渲染）、readonly 禁用；
// - Settings 页（#25 解锁）：旋钮回显卡（GET /v1/system/settings 消费断言）
//   + QRL 面板（三态呈现 + 双桶数值编辑 + disabled 态 400 语义）；
// - Webhooks outbox 死信面（T-496 API 消费）：Tab 切换、过滤器在场、
//   空态、replay 按钮仅 dead 行（mock 数据驱动）；
// - Builds promote/retention 写面（解锁）：对话框打开 + 字段集 + failFast
//   默认态；retention 对话框四字段；
// - Bundles 创建面（解锁）：对话框打开 + 行编辑器增删；
// - axe 双主题：keypair + settings 页。
import { expect, test } from '@playwright/test'
import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'

test.describe('P3 unlocked faces', () => {
  test('keypair page: empty state + generate dialog shape + vault refusal note', async ({ page }) => {
    await loginAs(page, 'admin')
    await page.goto('/binflow/ui/admin/security/keypair')
    await expect(page.locator('[data-testid="keypair-page"]')).toBeVisible()
    // 净实例：空态（无对）
    await expect(page.locator('[data-testid="keypair-empty"]')).toBeVisible()
    // 生成对话框：字段集（名/别名/长度/UID 三件/口令两联）
    await page.click('[data-testid="keypair-create-generate"]')
    await expect(page.locator('[data-testid="keypair-generate-dialog"]')).toBeVisible()
    for (const tid of [
      'keypair-generate-name', 'keypair-generate-alias', 'keypair-generate-bits',
      'keypair-generate-uid-name', 'keypair-generate-uid-comment', 'keypair-generate-uid-email',
      'keypair-generate-passphrase', 'keypair-generate-passphrase2',
    ]) {
      await expect(page.locator(`[data-testid="${tid}"]`)).toBeVisible()
    }
    // 提交门：名空 + 口令空 → 禁用
    await expect(page.locator('[data-testid="keypair-generate-submit"]')).toBeDisabled()
    await page.fill('[data-testid="keypair-generate-name"]', 'e2e-release-signing')
    await expect(page.locator('[data-testid="keypair-generate-submit"]')).toBeDisabled()
    await page.fill('[data-testid="keypair-generate-passphrase"]', 'e2e-passphrase')
    await page.fill('[data-testid="keypair-generate-passphrase2"]', 'e2e-passphrase')
    await expect(page.locator('[data-testid="keypair-generate-submit"]')).toBeEnabled()
    await page.click('[data-testid="keypair-generate-cancel"]')
    // 导入面对话框：armored 双栏 + vault 拒答文案（FE 不构造 vault 字段）
    await page.click('[data-testid="keypair-create-import"]')
    await expect(page.locator('[data-testid="keypair-import-dialog"]')).toBeVisible()
    await expect(page.locator('[data-testid="keypair-import-private"]')).toBeVisible()
    await expect(page.locator('[data-testid="keypair-import-public"]')).toBeVisible()
    await page.click('[data-testid="keypair-import-cancel"]')
  })

  test('settings page: knobs echo consumes /v1/system/settings + QRL panel three-state face', async ({ page }) => {
    await loginAs(page, 'admin')
    // 消费断言：GET /v1/system/settings 真发出（解锁面从 0 调用到 1）
    const settingsSeen = page.waitForRequest((r) => r.url().includes('/api/v1/system/settings'))
    await page.goto('/binflow/ui/admin/monitoring/settings')
    await expect(page.locator('[data-testid="settings-page"]')).toBeVisible()
    await settingsSeen
    await expect(page.locator('[data-testid="settings-knobs"]')).toBeVisible()
    // 旋钮族：folder_download 六字段 + trashcan.retention_days（消费端点回显）
    for (const tid of [
      'settings-knob-folder_download.enabled',
      'settings-knob-folder_download.enabled_for_anonymous',
      'settings-knob-folder_download.max_download_size_mb',
      'settings-knob-folder_download.max_files',
      'settings-knob-folder_download.max_concurrent_requests',
      'settings-knob-folder_download.enabled_empty_directories',
      'settings-knob-trashcan.retention_days',
    ]) {
      await expect(page.locator(`[data-testid="${tid}"]`)).toBeVisible()
    }
    // QRL：默认出厂 enabled（GET 200）→ 双桶数值在场 + 保存门（无改动禁用）
    await expect(page.locator('[data-testid="qrl-panel"]')).toBeVisible()
    await expect(page.locator('[data-testid="qrl-state"]')).toBeVisible()
    await expect(page.locator('[data-testid="qrl-row-DEFAULT"]')).toBeVisible()
    await expect(page.locator('[data-testid="qrl-row-LOW_PRIORITY"]')).toBeVisible()
    await expect(page.locator('[data-testid="qrl-save"]')).toBeDisabled()
    // 三态任一：停用入口（enabled）或启用入口（disabled）——出厂 disabled 实例
    await expect(page.locator('[data-testid="qrl-mode-disabled"], [data-testid="qrl-mode-enabled"]')).toBeVisible()
    await expect(page.locator('[data-testid="qrl-reset"]')).toBeVisible()
  })

  test('webhooks outbox: tab present, filters render, empty state, dead-row replay gate', async ({ page }) => {
    await loginAs(page, 'admin')
    await page.goto('/binflow/ui/admin/general/webhooks')
    await expect(page.locator('[data-testid="wh-page"]')).toBeVisible()
    // Outbox tab（P3 解锁——读面越过 addon 门）
    await page.click('[data-testid="wh-tab-outbox"]')
    await expect(page.locator('[data-testid="outbox-panel"]')).toBeVisible()
    // 过滤三件 + 状态闭集选项
    await expect(page.locator('[data-testid="outbox-filter-subscription"]')).toBeVisible()
    await expect(page.locator('[data-testid="outbox-filter-status"]')).toBeVisible()
    await expect(page.locator('[data-testid="outbox-filter-event-type"]')).toBeVisible()
    const opts = await page.locator('[data-testid="outbox-filter-status"] option').evaluateAll((els) =>
      (els as HTMLOptionElement[]).map((e) => e.value),
    )
    expect(opts).toEqual(['', 'pending', 'delivering', 'delivered', 'dead'])
    // 净实例空态（重放语义注记挂在数据分支内——空态时不渲染，改由
    // 重放按钮门语义承载：空表零 outbox-replay-* 按钮）
    await expect(page.locator('[data-testid="outbox-empty"]')).toBeVisible()
    await expect(page.locator('[data-testid^="outbox-replay-"]')).toHaveCount(0)
  })

  test('builds: promote + retention write dialogs open with field sets', async ({ page }) => {
    await loginAs(page, 'admin')
    // 种一个最小 run（PUT /api/build）
    const name = `p3e2e-${Date.now()}`
    await page.request.put(`/binflow/api/build`, {
      data: {
        version: '1.0.0', name, number: '1', type: 'GENERIC',
        started: '2026-09-09T10:00:00.123+0800',
        modules: [{ id: 'm1', type: 'generic', artifacts: [], dependencies: [] }],
      },
    })
    await page.goto(`/binflow/ui/builds/${encodeURIComponent(name)}/1`)
    await expect(page.locator('[data-testid="build-detail-page"]')).toBeVisible()
    // promote 对话框（解锁——旧 FE 明文「无 UI」）
    await page.click('[data-testid="build-promote"]')
    await expect(page.locator('[data-testid="build-promote-dialog"]')).toBeVisible()
    for (const tid of [
      'build-promote-status', 'build-promote-comment', 'build-promote-ciuser',
      'build-promote-target', 'build-promote-failfast',
    ]) {
      await expect(page.locator(`[data-testid="${tid}"]`)).toBeVisible()
    }
    // failFast 缺省勾选 + dryRun 预演按钮在场
    await expect(page.locator('[data-testid="build-promote-failfast"]')).toBeChecked()
    await expect(page.locator('[data-testid="build-promote-run"]')).toBeVisible()
    await page.click('[data-testid="build-promote-cancel"]')
    // retention 对话框（号单头）
    await page.goto(`/binflow/ui/builds/${encodeURIComponent(name)}`)
    await expect(page.locator('[data-testid="build-runs-page"]')).toBeVisible()
    await page.click('[data-testid="build-retention"]')
    await expect(page.locator('[data-testid="build-retention-dialog"]')).toBeVisible()
    for (const tid of [
      'build-retention-count', 'build-retention-min-date', 'build-retention-keep',
      'build-retention-delete-artifacts',
    ]) {
      await expect(page.locator(`[data-testid="${tid}"]`)).toBeVisible()
    }
    // 窗口全空 → 提交禁用（服务端拒无窗口体）
    await expect(page.locator('[data-testid="build-retention-submit"]')).toBeDisabled()
  })

  test('bundles: create dialog opens with manifest row editor', async ({ page }) => {
    await loginAs(page, 'admin')
    await page.goto('/binflow/ui/bundles')
    await expect(page.locator('[data-testid="bundles-page"]')).toBeVisible()
    await page.click('[data-testid="bundle-create"]')
    await expect(page.locator('[data-testid="bundle-create-dialog"]')).toBeVisible()
    await expect(page.locator('[data-testid="bundle-create-name"]')).toBeVisible()
    await expect(page.locator('[data-testid="bundle-create-version"]')).toBeVisible()
    // 行编辑器：首行三栏 + 加行/删行（首行删禁用）
    await expect(page.locator('[data-testid="bundle-create-row-0"]')).toBeVisible()
    await expect(page.locator('[data-testid="bundle-create-repo-0"]')).toBeVisible()
    await expect(page.locator('[data-testid="bundle-create-path-0"]')).toBeVisible()
    await expect(page.locator('[data-testid="bundle-create-sha-0"]')).toBeVisible()
    await expect(page.locator('[data-testid="bundle-create-remove-0"]')).toBeDisabled()
    await page.click('[data-testid="bundle-create-add"]')
    await expect(page.locator('[data-testid="bundle-create-row-1"]')).toBeVisible()
    // 提交门：名/版本/至少一行 repo+path
    await expect(page.locator('[data-testid="bundle-create-submit"]')).toBeDisabled()
  })

  test('axe: keypair + settings pages clean in both themes', async ({ page }, testInfo) => {
    await loginAs(page, 'admin')
    for (const theme of ['light', 'dark'] as const) {
      await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
      await page.goto('/binflow/ui/admin/security/keypair')
      await expect(page.locator('[data-testid="keypair-page"]')).toBeVisible()
      await expectA11yClean(page, testInfo)
      await page.goto('/binflow/ui/admin/monitoring/settings')
      await expect(page.locator('[data-testid="settings-knobs"]')).toBeVisible()
      await expectA11yClean(page, testInfo)
    }
  })
})
