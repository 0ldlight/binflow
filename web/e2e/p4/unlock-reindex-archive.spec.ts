import { execFileSync } from 'node:child_process'
import { readFileSync, rmSync, appendFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { test, expect } from '@playwright/test'

import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// FE-P4 A5/A6：解锁面收尾——reindex 入口（audit §2.17 首次 UI 化）与
// archive 下载（api/archive/download 首次 UI 化）。
//
// 实例态纪律（t461 先例）：helm 建仓与 archive 下载均走 pro 门——实例已
// pro 则零动作；community 则 bin/bf 自铸 2 天 pro（租约文件协调并行
// worker，末位卸载还原）；不可铸则 skip 留痕。
//
// 三腿：
//  ① 负向：generic 仓详情无 reindex 卡（不伪造四型外的入口）。
//  ② reindex：helm local 仓详情 → repo-reindex-card 在场 → run → toast
//    + 结果行（200 文案或 501 如实呈现——本实例服务面为准）。
//  ③ archive：树页目录右键「下载归档」→ 下载事件（zip 落盘）+
//    toast；多选工具条恰一目录时 tree-bulk-archive 在场。
//
// 锚源：console-ux §10.9 P4 批（repo-reindex-* / tree-bulk-archive /
// tree-context-archive——tree-context-<id> 族成员）。
const BASE = process.env.BASE ?? 'http://127.0.0.1:8080'
const ROOT = resolve(process.cwd(), '..')

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

const marker = uniq('p4unlock')
const helmKey = `${marker}-helm`
const genericKey = `${marker}-generic`

const createdKeys: string[] = [helmKey, genericKey]
let licenseInstalledByUs = false
let licenseBlocked = ''
let archiveBlocked = ''
const leaseFile = `/tmp/p4-license-lease-${new URL(BASE).port}.txt`

/** 进程存活探测（lease 计数的陈尸防御：历次运行遗留的死 pid 会被计入
 *  others 令 remaining 永不归零——license 永不卸载的实例态泄漏形态） */
function pruneDeadPids(all: string[]): string[] {
  return all.filter((l) => {
    const n = Number(l)
    if (!Number.isInteger(n)) return true // 'installed' 标记不是 pid
    try {
      process.kill(n, 0)
      return true
    } catch {
      return false
    }
  })
}

function readLease(): string[] {
  try {
    return pruneDeadPids(readFileSync(leaseFile, 'utf8').split('\n').filter((l) => l.trim() !== ''))
  } catch {
    return []
  }
}

function acquireLicenseLease(): void {
  try {
    const kept = readLease()
    rmSync(leaseFile, { force: true })
    appendFileSync(leaseFile, [...kept, `${process.pid}`].join('\n') + '\n')
  } catch {
    // 尽力而为——失败退化为「谁装谁卸」
  }
}

async function releaseLicenseLease(): Promise<void> {
  let remaining = 1
  let weInstalled = licenseInstalledByUs
  try {
    const all = readLease()
    const others = all.filter((l) => l !== String(process.pid) && l !== 'installed')
    weInstalled = weInstalled || all.includes('installed')
    remaining = others.length
    if (remaining === 0) rmSync(leaseFile, { force: true })
  } catch {
    // 无租约文件 = 单 worker 常态
  }
  if (remaining === 0 && weInstalled) {
    await m8Client().request('DELETE', '/binflow/api/system/license').catch(() => undefined)
  }
}

test.beforeAll(async () => {
  const client = m8Client()
  // pro 租约（helm 建仓门 + archive 的 repo-operations 槽位）
  const probe = await client.probeGet('/binflow/api/system/license')
  const alreadyPro = probe.status === 200 && (JSON.parse(probe.text).tier ?? '') === 'pro'
  acquireLicenseLease()
  if (!alreadyPro) {
    try {
      const doc = execFileSync(
        join(ROOT, 'bin', 'bf'),
        // addons 槽位两族：repo-operations（archive 门）+ helm（建仓门——
        // allowlist 语义：未列名的包型 400「not named in the allowlist」）
        ['license', 'issue', '--licensee', 'FE-P4 e2e', '--tier', 'pro', '--days', '2', '--addons', 'repo-operations,helm'],
        { cwd: ROOT, encoding: 'utf8' },
      ).trim()
      await client.request('POST', '/binflow/api/system/license', {
        raw: true,
        body: doc,
        headers: { 'content-type': 'text/plain' },
      })
      licenseInstalledByUs = true
      appendFileSync(leaseFile, 'installed\n')
    } catch (err) {
      licenseBlocked = `pro license unavailable (${String(err).slice(0, 160)})`
    }
  }
  await client
    .request('PUT', `/binflow/api/repositories/${helmKey}`, { body: { rclass: 'local', packageType: 'helm' } })
    .catch(() => undefined)
  await client
    .request('PUT', `/binflow/api/repositories/${genericKey}`, { body: { rclass: 'local', packageType: 'generic' } })
    .catch(() => undefined)
  await client
    .request('PUT', `/binflow/${genericKey}/p4dir/nested/file.bin`, {
      raw: true,
      body: 'p4-unlock-bytes',
      headers: { 'content-type': 'application/octet-stream' },
    })
    .catch(() => undefined)
  // folder_download 总闸探针（M13 T-368：缺省 off——实例未开闸则 archive
  // 腿如实 skip；开启需 binflow.yaml folder_download.enabled，重启生效）。
  // 探针打在已 seed 的目录上（建仓/内容之后——空目录是另一档 posture）
  try {
    await client.request('GET', `/binflow/api/archive/download/${genericKey}/p4dir/nested?archiveType=zip`)
  } catch (err) {
    archiveBlocked = `folder_download master switch off or archive face unavailable (${String(err).slice(0, 140)})`
  }
})

test.afterAll(async () => {
  for (const key of createdKeys) {
    await m8Client()
      .request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
      .catch(() => undefined)
  }
  if (licenseInstalledByUs) await releaseLicenseLease()
})

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  test.skip(!!licenseBlocked, licenseBlocked)
})

test('reindex: generic repo hides the card, helm repo runs it', async ({ page }) => {
  await loginAs(page, 'admin')

  // 负向：generic 仓无 reindex 卡（四型外不伪造入口）
  await page.goto(`/binflow/ui/admin/repositories/${genericKey}`)
  await expect(page.locator('[data-testid="repo-detail-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-reindex-card"]')).toHaveCount(0)

  // helm 仓：高级动作卡 + 发起（toast + 结果行——服务面文案为准）
  await page.goto(`/binflow/ui/admin/repositories/${helmKey}`)
  await expect(page.locator('[data-testid="repo-reindex-card"]')).toBeVisible()
  await page.click('[data-testid="repo-reindex-run"]')
  await expect(page.locator('[data-testid="toast"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('[data-testid="repo-reindex-result"]')).toBeVisible()
})

test('archive: folder context menu downloads a zip; bulk button appears for single-folder selection', async ({ page }) => {
  // 实例闸（license 之外的 folder_download 总闸，M13 T-368 缺省 off）——
  // 未开闸实例如实 skip（开闸需 binflow.yaml folder_download.enabled 重启）
  test.skip(!!archiveBlocked, archiveBlocked)
  // 视口加高：树页 children 网格在短视口（Playwright 缺省 720p）下 flex 高度
  // 链塌缩（tree-list 被压到 ~0——单行网格尤甚，布局债登记日志 Risks）；
  // 1600x900 与操作台常用档一致
  await page.setViewportSize({ width: 1600, height: 900 })
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${genericKey}/p4dir`)

  // 目录行右键 → 下载归档（zip 落盘事件 + toast）——先等 children 面板
  // 离开加载态（骨架消散 + 行可见），AG Grid 布局稳定后再操作
  const dirRow = page.locator('[data-testid="tree-row-nested"]')
  await page.waitForSelector('[data-testid="skeleton"]', { state: 'detached', timeout: 20_000 }).catch(() => undefined)
  await expect(dirRow).toBeVisible()
  await dirRow.click({ button: 'right' })
  const archiveItem = page.locator('[data-testid="tree-context-archive"]')
  await expect(archiveItem).toBeVisible()
  const downloadPromise = page.waitForEvent('download', { timeout: 30_000 })
  await archiveItem.click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toMatch(/nested.*\.zip$/)
  await expect(page.locator('[data-testid="toast"]')).toBeVisible({ timeout: 10_000 })

  // 多选工具条：勾选唯一的目录行（选择列在独立单元格——定位按行容器再入
  // .ag-selection-checkbox，而非 name 单元格 span 内）
  const row = page.locator('.ag-row').filter({ hasText: 'nested' }).first()
  await row.locator('.ag-selection-checkbox').first().click()
  await expect(page.locator('[data-testid="tree-bulk-archive"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-bulk-archive"]')).toBeEnabled()
})
