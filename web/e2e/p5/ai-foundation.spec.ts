import { test, expect } from '@playwright/test'
import type { Page } from '@playwright/test'

import { loginAs } from '../m8/support/roles'
import { expectA11yClean } from '../m8/support/a11y'

// FE-P5：AI 基座（frontend-rewrite-architecture §1 AI 行 + §10 P5 门 +
// 总令 §十五 Context-aware Copilot）。
//
// 六腿：
//  ① 三入口开合：顶栏 ai-open 钮 / ⌘(Ctrl)+J 快捷键 / palette-ai 条目
//    （P4 占位 → P5 接线）；Esc 关闭；焦点圈进 drawer。
//  ② 上下文徽章：Explorer 路由（repoKey 形）/ builds 坐标 / 仓库列表
//    域名形；发送后用户气泡首帧 ai-msg-context-badge 注入坐标。
//  ③ 消息渲染分层：echo markdown（strong/行内码）· 存储演示 tool-call
//    （ToolCallCard 参数折叠 + ToolResultCard 假表格）· fenced 代码块
//    （mono + 拷贝钮）· markdown 表格。
//  ④ confirm 闭环：创建仓库 → ConfirmCard 参数表 + [取消][创建仓库]
//    双钮 → 确认（已确认 + 假成功文案）→ 取消臂（已取消文案）。
//  ⑤ 零网络断言：AI 交互全程对 /binflow/api 与 /binflow/event 面零请求
//    （route 拦截计数——mock provider 组件契约的 e2e 面）。
//  ⑥ axe 双主题（drawer 开合态）。
//
// 锚源：console-ux §10.9 P5 批（ai-* 族）。键盘纪律：开合一律按键驱动。
test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function openDrawer(page: Page) {
  await page.keyboard.press('Control+j')
  await expect(page.locator('[data-testid="ai-drawer"]')).toBeVisible()
  await expect(page.locator('[data-testid="ai-input"]')).toBeVisible()
}

async function send(page: Page, text: string) {
  await page.fill('[data-testid="ai-input"]', text)
  await page.press('[data-testid="ai-input"]', 'Enter')
}

/** 点击发送钮的发送变体（ai-send 锚消费腿） */
async function sendByButton(page: Page, text: string) {
  await page.fill('[data-testid="ai-input"]', text)
  await page.click('[data-testid="ai-send"]')
}

test('ai: three entries open the drawer, Esc closes, focus stays inside', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()

  // 入口一：顶栏 AI 钮
  await page.click('[data-testid="ai-open"]')
  await expect(page.locator('[data-testid="ai-drawer"]')).toBeVisible()
  await expect(page.locator('[data-testid="ai-input"]')).toBeVisible()
  // 焦点圈进（Radix FocusScope——activeElement 落 drawer 内）
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.closest('[data-testid="ai-drawer"]') !== null))
    .toBe(true)

  // Esc 关闭（焦点回归——回焦到开 drawer 前的焦点位）
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="ai-drawer"]')).toHaveCount(0)

  // 入口二：⌘/Ctrl+J 快捷键（toggle 往返）
  await page.keyboard.press('Control+j')
  await expect(page.locator('[data-testid="ai-drawer"]')).toBeVisible()
  await page.keyboard.press('Control+j')
  await expect(page.locator('[data-testid="ai-drawer"]')).toHaveCount(0)

  // 入口三：palette AI 条目（P4 占位 → P5 接线：可激活且开 drawer）
  await page.keyboard.press('Control+k')
  const aiEntry = page.locator('[data-testid="palette-ai"]')
  await expect(aiEntry).toBeVisible()
  await expect(aiEntry).toBeEnabled()
  await aiEntry.click()
  await expect(page.locator('[data-testid="palette-root"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="ai-drawer"]')).toBeVisible()
  // 关闭钮锚
  await page.click('[data-testid="ai-close"]')
  await expect(page.locator('[data-testid="ai-drawer"]')).toHaveCount(0)
})

test('ai: context badge follows route (repo / build coords / domain-only)', async ({ page }) => {
  await loginAs(page, 'admin')

  // Explorer 坐标形：repoKey（m8-perf-local 由角色夹具播种）
  await page.goto('/binflow/ui/artifacts/m8-perf-local')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()
  await openDrawer(page)
  const badge = page.locator('[data-testid="ai-context-badge"]')
  await expect(badge).toBeVisible()
  await expect(badge).toContainText('制品浏览器')
  await expect(badge).toContainText('m8-perf-local')

  // 发送后首帧注入：用户气泡坐标徽章 + 回显「当前在」（坐标走 markdown
  // 行内码——textContent 断言不含反引号记号）
  await send(page, '你好')
  await expect(page.locator('[data-testid="ai-msg-user-0"]')).toBeVisible()
  await expect(page.locator('[data-testid="ai-msg-context-badge"]')).toHaveText('m8-perf-local')
  const reply = page.locator('[data-testid="ai-msg-assistant-1"]')
  await expect(reply).toContainText('当前在')
  await expect(reply.locator('code').first()).toHaveText('m8-perf-local')

  // builds 坐标形：name/number 链（goto 全页重载——drawer 闩重置，重开）
  await page.goto('/binflow/ui/builds/my-build/42')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()
  await openDrawer(page)
  await expect(page.locator('[data-testid="ai-context-badge"]')).toContainText('my-build/42')

  // 域名形（无坐标）：仓库列表页只显域标签
  await page.goto('/binflow/ui/admin/repositories/local')
  await openDrawer(page)
  await expect(page.locator('[data-testid="ai-context-badge"]')).toContainText('仓库管理')
  await expect(page.locator('[data-testid="ai-context-badge"]')).not.toContainText('/') // 无坐标链
})

test('ai: message rendering layers (markdown / tool cards / code block / table)', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()

  // 空态（四态之一）：欢迎 + 两条建议 chip
  await openDrawer(page)
  await expect(page.locator('[data-testid="ai-empty"]')).toBeVisible()
  await page.click('[data-testid="ai-empty-suggest-storage"]')
  await expect(page.locator('[data-testid="ai-input"]')).toHaveValue('查一下存储占用')

  // 发送 → 存储演示（tool-call + 内联结果）；消息流容器在场
  await page.press('[data-testid="ai-input"]', 'Enter')
  await expect(page.locator('[data-testid="ai-thread"]')).toBeVisible()
  const assistant = page.locator('[data-testid="ai-msg-assistant-1"]')
  await expect(assistant).toBeVisible()

  // ToolCallCard：name 徽标 + 参数折叠展开
  const toolCall = assistant.locator('[data-testid="ai-tool-call"]')
  await expect(toolCall).toBeVisible()
  await expect(toolCall).toContainText('query_storage_usage')
  await toolCall.locator('summary').click()
  await expect(assistant.locator('[data-testid="ai-tool-call-args"]')).toBeVisible()
  await expect(assistant.locator('[data-testid="ai-tool-call-args"]')).toContainText('scope')

  // ToolResultCard：结果表格（docker-prod 假行）
  const toolResult = assistant.locator('[data-testid="ai-tool-result"]')
  await expect(toolResult).toBeVisible()
  await expect(assistant.locator('[data-testid="ai-tool-result-table"]')).toContainText('docker-prod')

  // fenced 代码块（mono + 拷贝钮）与 markdown 表格
  await expect(assistant.locator('[data-testid="ai-code-block"]')).toBeVisible()
  await expect(assistant.locator('[data-testid="ai-code-block"]')).toContainText('items.find')
  await expect(assistant.locator('[data-testid="ai-copy-code"]')).toBeVisible()
  await expect(assistant.locator('[data-testid="ai-md-table"]')).toBeVisible()

  // echo 臂 markdown：粗体渲染（<strong>；行内码断言在 badge 腿——dashboard
  // 无坐标，echo 模板不含行内码）；本腿走 ai-send 钮（锚消费）
  await sendByButton(page, '你好')
  const echo = page.locator('[data-testid="ai-msg-assistant-3"]')
  await expect(echo).toBeVisible()
  await expect(echo.locator('strong', { hasText: '本地演示响应' })).toBeVisible()

  // 错误态（四态之错误）：模拟错误 → 错误卡 + 详情 + 重试（确定性再抛）
  await send(page, '模拟错误')
  const errCard = page.locator('[data-testid="ai-msg-assistant-5"] [data-testid="ai-error"]')
  await expect(errCard).toBeVisible()
  await expect(errCard).toContainText('响应生成失败')
  await page.click('[data-testid="ai-msg-assistant-5"] [data-testid="ai-error-retry"]')
  await expect(page.locator('[data-testid="ai-msg-assistant-5"] [data-testid="ai-error"]')).toBeVisible()
})

test('ai: confirm flow closes the loop (accept then cancel)', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()
  await openDrawer(page)

  // 创建仓库 → ConfirmCard（参数表 + 双钮）；首条走空态建议 chip（锚消费）
  await page.click('[data-testid="ai-empty-suggest-create"]')
  await page.press('[data-testid="ai-input"]', 'Enter')
  const confirm = page.locator('[data-testid="ai-confirm"]')
  await expect(confirm).toBeVisible()
  await expect(page.locator('[data-testid="ai-confirm-params"]')).toContainText('repoKey')
  await expect(page.locator('[data-testid="ai-confirm-params"]')).toContainText('demo-local')
  await expect(page.locator('[data-testid="ai-confirm-cancel"]')).toBeVisible()
  await expect(page.locator('[data-testid="ai-confirm-accept"]')).toBeVisible()

  // 确认 → 结构化结果 + 假成功文案（重入臂；repoKey 走行内码——textContent 形）
  await page.click('[data-testid="ai-confirm-accept"]')
  await expect(page.locator('[data-testid="ai-confirm-result"]')).toContainText('已确认：demo-local')
  await expect(page.locator('[data-testid="ai-msg-assistant-1"]')).toContainText('仓库 demo-local 已创建')

  // 取消臂：新一次确认流 → 取消 → 已取消文案（scope 到本条消息——
  // 上一轮的 ai-confirm-result 仍在场）
  await send(page, '帮我创建仓库')
  await expect(page.locator('[data-testid="ai-msg-assistant-3"] [data-testid="ai-confirm"]')).toBeVisible()
  await page.locator('[data-testid="ai-msg-assistant-3"] [data-testid="ai-confirm-cancel"]').click()
  await expect(page.locator('[data-testid="ai-msg-assistant-3"] [data-testid="ai-confirm-result"]')).toContainText('已取消')
  await expect(page.locator('[data-testid="ai-msg-assistant-3"]')).toContainText('已取消创建仓库')
})

test('ai: zero network during the whole AI exchange', async ({ page }) => {
  // mock provider 组件契约的 e2e 面：AI 交互全程零请求（/binflow/api 与
  // /binflow/event 面 route 计数；应用自身的载入请求在计数窗前收口）
  let hits = 0
  await page.route(/\/binflow\/(api|event)\//, (route) => {
    hits += 1
    return route.continue()
  })

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()
  // 载入收口（版本/仓库清单等应用请求落地后再开计数窗）
  await page.waitForTimeout(1_000)
  hits = 0

  await openDrawer(page)
  await send(page, '查一下存储占用')
  await expect(page.locator('[data-testid="ai-tool-result"]')).toBeVisible()
  await send(page, '帮我创建仓库')
  await expect(page.locator('[data-testid="ai-confirm"]')).toBeVisible()
  await page.click('[data-testid="ai-confirm-accept"]')
  await expect(page.locator('[data-testid="ai-confirm-result"]')).toContainText('已确认')
  await page.waitForTimeout(500)

  expect(hits, 'AI 面不得发出任何 /binflow/api 或 /binflow/event 请求（mock provider 零网络契约）').toBe(0)
})

test('ai: axe clean with the drawer open, both themes', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()
  await openDrawer(page)
  // 消息流有内容后再扫（渲染分层在场）
  await send(page, '查一下存储占用')
  await expect(page.locator('[data-testid="ai-tool-result"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="ai-drawer"]' })

  // 暗色复扫（reload 后 runtime 重置——重开 + 重发再扫）
  await page.keyboard.press('Escape')
  await page.evaluate(() => {
    localStorage.setItem('binflow-console-theme', 'dark')
  })
  await page.reload()
  await expect(page.locator('[data-testid="topbar-search"]')).toBeVisible()
  await openDrawer(page)
  await send(page, '查一下存储占用')
  await expect(page.locator('[data-testid="ai-tool-result"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="ai-drawer"]' })
})
