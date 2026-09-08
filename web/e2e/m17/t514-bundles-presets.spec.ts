import { execFileSync } from 'node:child_process'
import { join, resolve } from 'node:path'

import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, sessionApi } from '../m8/support/seed'

// T-514（M17 W7，FR-153.2/.3 FE 面——T-513 查询族 + T-491 通配桶 BE 的
// 消费腿；B-2.16 预置桶缺位解除 + bundle 列表/详情新面）：
//
//   ① **三预置桶同场勾选**（FR-153.2 / B-2.16）：两步弹窗选仓步的
//      Any Local / Any Remote / Any Distribution 预置行（console-ui §3.8
//      活体形态——与具体仓库同场可勾选、可混列）；提交 payload 的
//      repos[] = wire 字面（ANY LOCAL/ANY REMOTE/ANY DISTRIBUTION，网络层
//      断言）；GET 回显逐字；再开弹窗水合勾选态（回显→勾选回填）。
//   ② **Any Distribution 授予面覆盖断言**（AC2 探针）：授予 read on
//      ANY DISTRIBUTION 的用户在 /bundles 可见被授权 bundle（名→版本→
//      描述符三级钻取）；无授予用户名单空集（服务端可见集过滤零泄漏）
//      且版本面/描述符单查均 403（拒绝即答案——T-513 报告「版本面 404」与实态不符，本票契约漂移登记）。
//   ③ **bundle 列表/详情渲染**（FR-153.3）：导航入口（应用分组 Release
//      Bundles 条目）+ 名单/版本单/描述符三视图 + signature / HEAD 校验和
//      / 清单行 sha256（mono+拷贝）；pending 行（sha256 缺席）如实呈现。
//   ④ axe 双主题（三视图抽样：名单 + 描述符）。
//
// 实例态纪律：bundle 创建（POST /api/release/bundle）走 release-bundle
// 槽位门（pro 暂行）——pro license 经 bin/bf 自铸安装（t461 先例），本文件
// serial（单 worker）故 beforeAll 装 / afterAll 卸即可，无需租约计数。
// **bundle 无删除端点**（T-513 最小面清单不可变——E-26）——本 spec 创建的
// bundle 在实例上长存（uniq 名不与重跑碰撞；测试实例为一次性数据目录）。
// 锚源：console-ux §10.5 T-514 批（bundles-* / bundle-* 族 +
// perm-buckets-note；perm-repo-pick-<key> 冻结族扩桶字面成员）。

// 仓根（cwd = web/——license 开发钥与 bin/bf 相对仓根寻址，t461 同款）
const ROOT = resolve(process.cwd(), '..')

test.describe.configure({ mode: 'serial' })

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 任意用户走真实登录页（rbac.spec 同款——loginAs 只覆盖 M8 角色夹具）；
 *  先等登录页可见再填（cookie 清除后的 SPA 引导重定向不与 fill 竞态） */
async function login(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/binflow/ui/')
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
  await expect(page.locator('[data-testid="session-user"]')).toHaveText(username)
}

const createdUsers: string[] = []
const createdTargets: string[] = []
const createdRepos: string[] = []
let licenseInstalledByUs = false

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

// ---- pro license（bundle 创建门；serial 单 worker——装/卸各一次） ----------

async function installProLicense(): Promise<boolean> {
  const client = m8Client()
  const probe = await client.probeGet('/binflow/api/system/license')
  if (probe.status === 200 && (JSON.parse(probe.text).tier ?? '') === 'pro') return false
  try {
    const doc = execFileSync(
      join(ROOT, 'bin', 'bf'),
      ['license', 'issue', '--licensee', 'T-514 e2e', '--tier', 'pro', '--days', '2'],
      { cwd: ROOT, encoding: 'utf8' },
    ).trim()
    await client.request('POST', '/binflow/api/system/license', {
      raw: true,
      body: doc,
      headers: { 'content-type': 'text/plain' },
    })
    return true
  } catch (err) {
    throw new Error(`pro license unavailable (bundle create face gated): ${String(err).slice(0, 200)}`)
  }
}

test.beforeAll(async () => {
  licenseInstalledByUs = await installProLicense()
})

test.afterAll(async () => {
  const client = m8Client()
  for (const t of createdTargets) {
    await client.request('DELETE', `/binflow/api/v1/permissions/${t}`).catch(() => undefined)
  }
  for (const u of createdUsers) {
    await client.request('DELETE', `/binflow/api/security/users/${u}`).catch(() => undefined)
  }
  for (const r of createdRepos) {
    await client.request('DELETE', `/binflow/api/repositories/${r}?deleteContent=true`).catch(() => undefined)
  }
  if (licenseInstalledByUs) {
    await client.request('DELETE', '/binflow/api/system/license').catch(() => undefined)
  }
})

/** 建夹具：local 仓 + 一个真实制品（COMPLETE bundle 的快照行） */
async function seedRepoWithArtifact(repo: string, path: string): Promise<void> {
  const client = m8Client()
  expect((await client.request('PUT', `/binflow/api/repositories/${repo}`, { body: { rclass: 'local', packageType: 'generic' } })).status).toBe(200)
  createdRepos.push(repo)
  expect(
    (
      await client.request('PUT', `/binflow/${repo}/${path}`, {
        raw: true,
        headers: { 'Content-Type': 'application/octet-stream' },
        body: `t514 artifact bytes ${repo}`,
      })
    ).status,
  ).toBe(201)
}

async function createUser(name: string, password: string): Promise<void> {
  const client = m8Client()
  expect(
    (
      await client.request('PUT', `/binflow/api/security/users/${name}`, {
        body: { name, email: `${name}@example.com`, password, admin: false, groups: [] },
      })
    ).status,
  ).toBe(201)
  createdUsers.push(name)
}

test('① three preset buckets check together with a real repo; wire payload, echo, hydrate', async ({ page }) => {
  await loginAs(page, 'admin')
  const repo = uniq('t514pr')
  const user = uniq('t514pu')
  const target = uniq('t514pt')
  await seedRepoWithArtifact(repo, 'rel/one.bin')
  await createUser(user, 't514-preset-pw')

  await page.goto('/binflow/ui/admin/security/permissions/new')
  await page.fill('[data-testid="perm-form-name"]', target)

  // 两步弹窗：三预置桶 + 真实仓同场勾选（B-2.16 缺位解除的本体）
  await page.click('[data-testid="perm-repo-add"]')
  await expect(page.locator('[data-testid="perm-buckets-note"]')).toBeVisible()
  // 预置行展示名 = 活体拼写（Any Local / Any Remote / Any Distribution），
  // 锚键 = wire 字面（perm-repo-pick-ANY LOCAL 等——冻结族扩成员）
  await expect(page.locator('[data-testid="transfer-available"]')).toContainText('Any Local')
  await expect(page.locator('[data-testid="transfer-available"]')).toContainText('Any Remote')
  await expect(page.locator('[data-testid="transfer-available"]')).toContainText('Any Distribution')
  await page.check('[data-testid="perm-repo-pick-ANY LOCAL"]')
  await page.check('[data-testid="perm-repo-pick-ANY REMOTE"]')
  await page.check('[data-testid="perm-repo-pick-ANY DISTRIBUTION"]')
  await page.check(`[data-testid="perm-repo-pick-${repo}"]`)
  // 右列（已选）四条同场
  await expect(page.locator('[data-testid="transfer-selected"]')).toContainText('Any Local')
  await expect(page.locator('[data-testid="transfer-selected"]')).toContainText('Any Remote')
  await expect(page.locator('[data-testid="transfer-selected"]')).toContainText('Any Distribution')
  await expect(page.locator('[data-testid="transfer-selected"]')).toContainText(repo)
  // 进第 2 步（OK 钮在步 2 footer——步 1 是 Next）→ 直接确定（模式不动）
  await page.click('[data-testid="perm-res-step-2"]')
  await page.click('[data-testid="perm-res-ok"]')
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText('ANY DISTRIBUTION')
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText(repo)

  // 授予 read → 保存（网络层断言 repos[] = 四条：桶字面 + 真仓键）
  await page.selectOption('[data-testid="perm-add-user"]', user)
  await page.getByRole('button', { name: '添加用户' }).click()
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-read"]`)
  const postedRepos: string[] = []
  page.on('request', (req) => {
    if (req.method() === 'POST' && req.url().includes('/api/v1/permissions')) {
      postedRepos.push(...((req.postDataJSON() as { repos?: string[] }).repos ?? []))
    }
  })
  await page.click('[data-testid="perm-save"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible({ timeout: 8000 })
  expect([...postedRepos].sort()).toEqual(['ANY DISTRIBUTION', 'ANY LOCAL', 'ANY REMOTE', repo].sort())

  // GET 回显逐字（T-491：wire 收三字面、echo 原样）
  const got = await sessionApi(page, 'GET', '/api/v1/permissions')
  expect(got.status).toBe(200)
  const echo = (got.json as { name: string; repos: string[] }[]).find((x) => x.name === target)
  expect(echo?.repos.sort()).toEqual(['ANY DISTRIBUTION', 'ANY LOCAL', 'ANY REMOTE', repo].sort())

  // 水合：再开弹窗，桶与仓的勾选态如实回填
  await page.goto(`/binflow/ui/admin/security/permissions/${target}`)
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText('ANY LOCAL')
  await page.click('[data-testid="perm-repo-add"]')
  await expect(page.locator('[data-testid="perm-repo-pick-ANY LOCAL"]')).toBeChecked()
  await expect(page.locator('[data-testid="perm-repo-pick-ANY REMOTE"]')).toBeChecked()
  await expect(page.locator('[data-testid="perm-repo-pick-ANY DISTRIBUTION"]')).toBeChecked()
  await expect(page.locator(`[data-testid="perm-repo-pick-${repo}"]`)).toBeChecked()
  // 勾选可撤销（桶不是单向门）
  await page.uncheck('[data-testid="perm-repo-pick-ANY REMOTE"]')
  await page.check('[data-testid="perm-repo-pick-ANY REMOTE"]')
  await page.click('[data-testid="perm-res-cancel"]')
})

test('② Any Distribution grant coverage: authorized user sees the bundle, ungranted sees empty + 404', async ({ page }, testInfo) => {
  // bundle 夹具（pro 门内）：真实制品行（快照 COMPLETE）+ pending 行混排
  const admin = m8Client()
  const repo = uniq('t514gr')
  const bundle = uniq('t514bundle')
  await seedRepoWithArtifact(repo, 'rel/snap.bin')
  expect(
    (
      await admin.request('POST', '/binflow/api/release/bundle', {
        body: {
          name: bundle,
          version: '1.0.0',
          artifacts: [
            { repo, path: 'rel/snap.bin' },
            { repo, path: 'rel/pending.bin' }, // 不在本实例 → pending 行（INPROGRESS 实义）
          ],
        },
      })
    ).status,
  ).toBe(202)

  // 授予面：user-granted read on ANY DISTRIBUTION（API 面——UI 面腿①已证）
  const granted = uniq('t514gr-u1')
  const stranger = uniq('t514gr-u2')
  const target = uniq('t514gr-t')
  await createUser(granted, 't514-grant-pw')
  await createUser(stranger, 't514-grant-pw')
  expect(
    (
      await admin.request('POST', '/binflow/api/v1/permissions', {
        body: {
          name: target,
          repos: ['ANY DISTRIBUTION'],
          includePatterns: ['**'],
          excludePatterns: [],
          principals: { users: { [granted]: ['read'] }, groups: {} },
        },
      })
    ).status,
  ).toBe(201)
  createdTargets.push(target)

  // 被授权用户：名单 → 版本 → 描述符三级可见（授予面覆盖探针·正向）
  await login(page, granted, 't514-grant-pw')
  await page.goto('/binflow/ui/bundles')
  await expect(page.locator(`[data-testid="bundles-row-${bundle}"]`)).toBeVisible()
  await page.click(`[data-testid="bundles-row-${bundle}"] a`)
  await expect(page.locator('[data-testid="bundle-versions-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="bundle-version-row-1.0.0"]')).toBeVisible()
  await expect(page.locator('[data-testid="bundle-version-row-1.0.0"]')).toContainText('INPROGRESS')
  await page.click('[data-testid="bundle-version-row-1.0.0"] a')
  await expect(page.locator('[data-testid="bundle-detail-page"]')).toBeVisible()
  // 描述符字段：签名摘要（mono）+ 创建者行（admin 经 REST 建）+ 清单两行
  // ——items 读序 = (repo_key, path)：pending 行先、快照行后
  await expect(page.locator('[data-testid="bundle-detail-signature"]')).not.toContainText('—')
  await expect(page.locator('[data-testid="bundle-detail-info"]')).toContainText('admin')
  const artifactRows = page.locator('[data-testid="bundle-artifacts"] tbody tr')
  await expect(artifactRows).toHaveCount(2)
  await expect(artifactRows.nth(0)).toContainText('rel/pending.bin')
  await expect(artifactRows.nth(0)).toContainText('—') // pending 行：sha256 缺席如实 —
  await expect(artifactRows.nth(1)).toContainText('rel/snap.bin')
  await expect(artifactRows.nth(1)).not.toContainText('—') // 快照行：sha256 + size 在场
  // HEAD 校验和（E5 面）：64 hex mono
  await expect(page.locator('[data-testid="bundle-checksum"]')).toHaveText(/^[0-9a-f]{64}$/)
  await expectA11yClean(page, testInfo, { include: '[data-testid="bundle-detail-page"]' })
  // 深色主题复扫（③补名单面双主题——此处描述符面双主题）
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expectA11yClean(page, testInfo, { include: '[data-testid="bundle-detail-page"]' })

  // 未授予用户：名单空集（可见集过滤——零 403 oracle）+ 版本面/描述符面
  // 均 403（canRead 拒绝即答案——名单面唯一走过滤的零泄漏面；404 归④的
  // admin 真缺臂）。
  // 同页换会话：清 cookie 走登录页（rbac V13 的 newContext 同义面）
  await page.context().clearCookies()
  await login(page, stranger, 't514-grant-pw')
  await page.goto('/binflow/ui/bundles')
  await expect(page.locator('[data-testid="bundles-empty"]')).toBeVisible()
  const versionsFace = await sessionApi(page, 'GET', `/api/release/bundles/${encodeURIComponent(bundle)}`)
  expect(versionsFace.status).toBe(403)
  const direct = await sessionApi(page, 'GET', `/api/release/bundles/${encodeURIComponent(bundle)}/1.0.0`)
  expect(direct.status).toBe(403)
  // 描述符面 403 的 UI 承载（bundle-denied——拒绝即答案，不与 404 混同）
  await page.goto(`/binflow/ui/bundles/${encodeURIComponent(bundle)}/1.0.0`)
  await expect(page.locator('[data-testid="bundle-denied"]')).toBeVisible()
})

test('③ admin render: nav entry + list/detail views + axe (light/dark)', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  // 导航入口：应用分组 Release Bundles 条目（T-514 挂靠——应用域）
  await page.goto('/binflow/ui/artifacts')
  const navEntry = page.locator('[data-testid="app-nav"] a', { hasText: 'Release Bundles' })
  await expect(navEntry).toBeVisible()
  await navEntry.click()
  await expect(page).toHaveURL(/\/binflow\/ui\/bundles$/)

  // 名单视图：表头 + 本票夹具行（②建的 bundle——serial 序保证在场）
  await expect(page.locator('[data-testid="bundles-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="bundles-table"] th', { hasText: 'Bundle' })).toHaveCount(1)
  await expectA11yClean(page, testInfo, { include: '[data-testid="bundles-page"]' })

  // 深色主题再扫一腿（②已在默认主题扫过描述符视图——此处补名单面）
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expectA11yClean(page, testInfo, { include: '[data-testid="bundles-page"]' })
})

test('④ versions face 404 zero-leak wording (unknown name)', async ({ page }) => {
  await loginAs(page, 'admin')
  const bogus = uniq('t514-nope')
  await page.goto(`/binflow/ui/bundles/${encodeURIComponent(bogus)}`)
  await expect(page.locator('[data-testid="bundle-not-found"]')).toBeVisible()
  await expect(page.locator('[data-testid="bundle-not-found"]')).toContainText(bogus)
})
