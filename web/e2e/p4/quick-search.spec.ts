import { test, expect } from '@playwright/test'

import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// FE-P4 A2：顶栏全局搜索快速结果下拉（artifactsearch/quick 首次 UI 化——
// architecture §8「API 在而未用」解锁面）。
//
// 三腿：
//  ① 快速结果：种子仓 + 特异名制品 → 顶栏输入 ≥2 字符（300ms 防抖）→
//    topbar-search-quick 段呈现命中行（repo/path）→ 点击深链树页
//    （/artifacts/{repo}/{path}——文件末段即选中态）。
//  ② 键盘语义不破：Enter 仍是提交搜索（/search?q=——recent 提交通道），
//    两段 Esc 收下拉/清空失焦（fr82 既有契约维持）。
//  ③ 门控：<2 字符不触发（无快速段）；零命中不呈现快速段（仅最近词区）。
//
// 锚源：console-ux §10.9 P4 批（topbar-search-quick / topbar-search-quick-item-<i>）。
// 种子纪律：uniq key + afterAll 删除；REST 幂等 converge。

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

const marker = uniq('p4quick')

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
    .request('PUT', `/binflow/${marker}-local/p4/quick-probe.bin`, {
      raw: true,
      body: 'p4-quick-bytes',
      headers: { 'content-type': 'application/octet-stream' },
    })
    .catch(() => undefined)
})

test.afterAll(async () => {
  await m8Client()
    .request('DELETE', `/binflow/api/repositories/${marker}-local?deleteContent=true`)
    .catch(() => undefined)
})

test('quick results: type ≥2 chars, hit row deep-links the tree page', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')

  await page.focus('[data-testid="topbar-search"]')
  await page.keyboard.type(marker.slice(2, 8)) // ≥2 字符的特异片段
  await expect(page.locator('[data-testid="topbar-search-quick"]')).toBeVisible({ timeout: 10_000 })
  const hit = page.locator('[data-testid="topbar-search-quick-item-0"]')
  await expect(hit).toContainText(`${marker}-local/`)
  await expect(hit).toContainText('quick-probe.bin')

  // 点击深链：树页 + 文件末段选中态
  await hit.click()
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/artifacts/${marker}-local/p4/quick-probe\\.bin$`))
  await expect(page.locator('[data-testid="tree-page"]')).toBeVisible()
})

test('quick results: Enter still submits search; short term shows no quick section', async ({ page }) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/dashboard')

  // 单字符（<2）不触发快速段；最近词区照常（聚焦恒渲染）
  await page.focus('[data-testid="topbar-search"]')
  await page.keyboard.type('p')
  await page.waitForTimeout(600) // 防抖窗 + 余量
  await expect(page.locator('[data-testid="topbar-search-quick"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="topbar-search-recent"]')).toBeVisible()

  // Enter = 提交搜索（recent 通道语义不变）
  await page.keyboard.type('4quick-submit-probe')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/binflow\/ui\/search\?q=/)
})
