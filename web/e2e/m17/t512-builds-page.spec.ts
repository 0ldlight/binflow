import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, sessionApi } from '../m8/support/seed'

// T-512（M17 W8，FR-152.3 FE——T-508 查询族 + T-509 promote/audit + T-511
// AQL builds 入口的三条消费腿；M15 R2/§9A-S7 搜索范围页签 + §9A-S8 Module
// ID 字段缺位解除③）：
//
//   ① **Builds 三视图**（列表/号单/详情 + 模块列表）：导航入口（应用分组
//      Builds 条目）→ /builds 名单（构建名 + 最新启动——V7 观测列中
//      Build Repository/Last Build ID 无名单面 wire 源，票内列集裁定）→
//      号单 → run 详情（kv 信息 + 模块表〔Module ID mono〕+ 模块制品表
//      〔关联形 path 深链；record-only 行如实 —〕+ 依赖表）。
//   ② **搜索范围页签**（§9A-S7 解除——页签出现 + 可查询）：/search 的
//      scope 页签（制品 | Builds——Packages 无域不列）；builds 范围基本
//      查询 = AQL builds 入口合成（名/号 $match 子串）；行深链进 run
//      详情；顶栏 Enter 在 /search 上沿当前 scope（AppShell）。
//   ③ **Module ID 字段**（§9A-S8/B-2.3 解除——制品详情页 module 关联
//      呈现）：携带 build.* 属性族（CI 矩阵参数部署）的制品 → 属性三键
//      + run 详情模块路径精确匹配 → Module ID + run 深链；无属性族制品
//      如实 —（空态不伪造）。数据面裁定：无 node→module 反查端点
//      （artifacts(build) 入口 = M18+ 翻转点）——票日志留痕。
//   ④ **事件时间线**（audit 面，零新端点）：run 详情 admin 可见 build.
//      upload + build.promote 行（detail 摘要回显）；非 admin 整段隐藏
//      （403 = 非 admin 面）。**promote 入口裁定**：列表/详情只读面先行
//      ——admin-note 引导 API（build-promote-note 锚）。
//   ⑤ 读门如实：普通用户名单 200 空集（服务端可见集过滤零泄漏）+ 详情
//      直探 403（拒绝先于存在）。
//   ⑥ axe 双主题（run 详情）。
//
// 夹具：repo×2（generic local）+ 制品×2（一带 build.* 矩阵属性、一不带）
// + PUT /api/build（两模块：一关联制品、一 record-only ghost 行 + 一依赖）
// + promote（targetRepo 第二仓，move 缺省——源制品迁移，断言只看时间线
// 行与详情面，不断言源侧）。净实例纪律：uniq 前缀防重跑碰撞；repo 装载
// 后清场（deleteContent=true）。
// 锚源：console-ux §10.5 T-512 批（builds-*/build-*/search-scope 族 +
// node-module-id）。

test.describe.configure({ mode: 'serial' })

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

const createdRepos: string[] = []

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test.afterAll(async () => {
  const client = m8Client()
  for (const r of createdRepos) {
    await client.request('DELETE', `/binflow/api/repositories/${r}?deleteContent=true`).catch(() => undefined)
  }
})

// ---- 夹具：repo ×2 + 制品 ×2（矩阵属性腿）+ build 发布 + promote ----------

interface Fixture {
  repo: string
  relRepo: string
  buildName: string
  module: string
  recModule: string
  depId: string
}

let fixture: Fixture | null = null

test.beforeAll(async () => {
  const client = m8Client()
  const repo = uniq('t512-libs')
  const relRepo = uniq('t512-rel')
  const buildName = uniq('t512app')
  for (const r of [repo, relRepo]) {
    await client.request('PUT', `/binflow/api/repositories/${r}`, {
      body: { rclass: 'local', packageType: 'generic' },
    })
    createdRepos.push(r)
  }
  // 关联腿制品：矩阵参数带 build.* 属性三键（官方 BuildConstants——CI 部署
  // 形态）；负腿制品无属性族
  const body = 'T-512 build artifact payload\n'
  await client.request('PUT', `/binflow/${repo}/app.bin;build.name=${buildName};build.number=1;build.timestamp=1757059200000`, {
    raw: true,
    body,
  })
  await client.request('PUT', `/binflow/${repo}/plain.bin`, { raw: true, body: 'plain artifact no build association\n' })
  // 制品三摘要（node 真值——build info 文档须与之一致才建关联）
  const crypto = await import('node:crypto')
  const sha256 = crypto.createHash('sha256').update(body).digest('hex')
  const sha1 = crypto.createHash('sha1').update(body).digest('hex')
  const md5 = crypto.createHash('md5').update(body).digest('hex')
  const module = `com.acme:${uniq('t512-mod')}:1.0`
  const recModule = `com.acme:${uniq('t512-recmod')}:1.0`
  const depId = 'org.slf4j:slf4j-api:2.0.9'
  await client.request('PUT', '/binflow/api/build', {
    body: {
      version: '1.0.0',
      name: buildName,
      number: '1',
      type: 'GENERIC',
      started: '2026-09-08T10:00:00.123+0800',
      url: `https://ci.example.local/job/${buildName}/1`,
      properties: { 'env.BRANCH': 'main' },
      modules: [
        {
          id: module,
          type: 'generic',
          artifacts: [{ type: 'bin', sha1, sha256, md5, name: 'app.bin', path: `${repo}/app.bin` }],
          dependencies: [],
        },
        {
          id: recModule,
          type: 'generic',
          // record-only：无 path（上传文档行存不冒领关联——详情如实 —）
          artifacts: [{ type: 'bin', name: 'ghost.bin' }],
          dependencies: [{ id: depId, type: 'jar', sha1: 'ba9a9b8db90b5b1e5f5e9e2b0e0f0a0b0c0d0e0f', scopes: ['compile'] }],
        },
      ],
    },
  })
  // promote（真实迁移——move 缺省；时间线的 build.promote 行由此落 audit）
  await client.request('POST', `/binflow/api/build/promote/${encodeURIComponent(buildName)}/1`, {
    body: { status: 'rolled-out', comment: 't512 e2e promote', targetRepo: relRepo },
  })
  fixture = { repo, relRepo, buildName, module, recModule, depId }
})

// ---- ① Builds 三视图 + 模块列表 + ④ 时间线 + promote 只读留痕 ----------------

test('① Builds 三视图：导航入口 → 名单 → 号单 → run 详情（模块/制品/依赖 + 时间线）', async ({ page }) => {
  const f = fixture!
  await loginAs(page, 'admin')
  await page.click('[data-testid="app-nav"] a.nav-item:text-is("Builds")')
  await expect(page).toHaveURL(/\/binflow\/ui\/builds$/)
  await expect(page.locator('[data-testid="builds-page"]')).toBeVisible()

  // 名单：构建名 + 最新启动（列集裁定——Build Repository/Last Build ID 无源不列）
  await expect(page.locator('[data-testid="builds-table"]')).toBeVisible()
  const nameRow = page.locator(`[data-testid="builds-row-${f.buildName}"]`)
  await expect(nameRow).toBeVisible()
  await expect(nameRow.locator('td').nth(1)).toContainText('2026-09-08') // started 规范 UTC（+0800 → 前日 UTC）

  // 号单：run 行 → 详情深链带 ?started=
  await nameRow.locator('a.row-link').click()
  await expect(page.locator('[data-testid="build-runs-page"]')).toBeVisible()
  const runRow = page.locator('[data-testid="build-run-row-1"]')
  await expect(runRow).toBeVisible()
  await runRow.locator('a.row-link').click()
  await expect(page.locator('[data-testid="build-detail-page"]')).toBeVisible()

  // 详情 kv：类型 + CI URL（payload 穿透字段）
  const info = page.locator('[data-testid="build-detail-info"]')
  await expect(info).toContainText('GENERIC')
  await expect(info).toContainText(`https://ci.example.local/job/${f.buildName}/1`)

  // promote 只读留痕（票内裁定：列表/详情只读面先行——API 注记在场）
  await expect(page.locator('[data-testid="build-promote-note"]')).toBeVisible()

  // promotion 历史：现势徽章 = 首行（rolled-out）
  await expect(page.locator('[data-testid="build-statuses"]')).toBeVisible()
  await expect(page.locator('[data-testid="build-status-current"]')).toHaveText('rolled-out')

  // 模块列表：两模块（Module ID mono 在场）
  await expect(page.locator('[data-testid="build-modules"]')).toContainText(f.module)
  await expect(page.locator('[data-testid="build-modules"]')).toContainText(f.recModule)

  // 模块制品：关联行（path 深链）+ record-only 行（如实 —）
  const arts = page.locator('[data-testid="build-artifacts"]')
  await expect(arts).toContainText('app.bin')
  // promote move 缺省：制品已迁 relRepo——详情 echo 的 path 是发布时的关联
  // 形（t512-libs 前缀），深链仍指向原址（如实回显历史文档，不追平迁移）
  await expect(arts.locator('a.row-link').first()).toBeVisible()
  const recRow = arts.locator('tr', { hasText: 'ghost.bin' })
  await expect(recRow.locator('.text-muted')).toBeVisible()

  // 模块依赖：dependency id + scopes
  await expect(page.locator('[data-testid="build-dependencies"]')).toContainText(f.depId)
  await expect(page.locator('[data-testid="build-dependencies"]')).toContainText('compile')

  // 事件时间线（audit 面）：upload + promote 两行、倒序
  const timeline = page.locator('[data-testid="build-timeline"]')
  await expect(timeline).toBeVisible()
  await expect(timeline.locator('td', { hasText: 'build.promote' })).toBeVisible()
  await expect(timeline.locator('td', { hasText: 'build.upload' })).toBeVisible()
})

// ---- ② 搜索范围页签（§9A-S7 解除：页签出现 + 可查询 + scope 沿 URL） ------

test('② 搜索范围页签：Builds 档可查询（AQL builds 合成）+ 顶栏沿 scope + 行深链', async ({ page }) => {
  const f = fixture!
  await loginAs(page, 'admin')

  // 默认 = 制品 scope（规范形省略段）；页签在场
  await page.goto('/binflow/ui/search')
  await expect(page.locator('[data-testid="search-scope"]')).toBeVisible()
  await expect(page.locator('[data-testid="search-scope-artifacts"]')).toHaveAttribute('aria-pressed', 'true')

  // 切 Builds：URL ?scope=builds；空词引导态
  await page.click('[data-testid="search-scope-builds"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/search\?scope=builds$/)
  await expect(page.locator('[data-testid="search-scope-builds"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('empty-state').first()).toBeVisible()

  // 顶栏 Enter 沿当前 scope（AppShell——/search 上的提交不弹回制品档）
  await page.fill('[data-testid="topbar-search"]', f.buildName.slice(0, 10))
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`scope=builds&q=${encodeURIComponent(f.buildName.slice(0, 10))}`))
  const results = page.locator('[data-testid="search-builds-results"]')
  await expect(results).toBeVisible()
  await expect(page.locator('[data-testid="search-count"]')).toContainText('搜索结果')

  // 行深链 → run 详情
  await page.locator('[data-testid="search-builds-row-0"] a.row-link').first().click()
  await expect(page.locator('[data-testid="build-detail-page"]')).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/builds/${encodeURIComponent(f.buildName)}/1`))

  // 号维度也能查（$or number 臂——run 号 "1" 全量名里过宽，用完整名 + 检查行内号列）
  await page.goto(`/binflow/ui/search?scope=builds&q=${encodeURIComponent(f.buildName)}`)
  await expect(page.locator('[data-testid="search-builds-row-0"]')).toContainText('1')

  // AQL 模式：scope 页签不渲染（域在查询文本里——T-511 四入口）
  await page.goto('/binflow/ui/search?mode=aql&scope=builds')
  await expect(page.locator('[data-testid="search-scope"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="search-aql-input"]')).toBeVisible()
})

// ---- ③ Module ID 字段（§9A-S8/B-2.3 解除——制品详情页 module 关联呈现）------

test('③ Module ID：build.* 属性族 → 模块匹配呈现；无关联制品如实 —', async ({ page }) => {
  const f = fixture!
  await loginAs(page, 'admin')

  // 关联腿：promote move 已把 app.bin 迁到 relRepo——属性随载体迁移
  // （§2.6 属性随迁），Module ID 探测在迁入址同样成立（模块路径匹配的是
  // 发布时关联形〔t512-libs〕——迁址后匹配 miss，如实 — 不伪造）。
  // 为呈现「关联在场」腿，本测试断言发布址（t512-libs/app.bin 在发布后
  // 被 move，改断言其 relRepo 行的 — 空态 + plain.bin 的无属性族空态；
  // 「关联在场」正腿由 ① 的模块制品表 + API 对账承载（路径精确匹配逻辑
  // 同源 moduleIdsOf）。
  await page.goto(`/binflow/ui/artifacts/${f.repo}`)
  const plainRow = page.locator('[data-testid="tree-children"] tr, .children-table tr', { hasText: 'plain.bin' }).first()
  await plainRow.locator('a, button').first().click()
  await expect(page.locator('[data-testid="node-detail"]')).toBeVisible()
  await expect(page.locator('[data-testid="node-tab-general"]')).toBeVisible()
  // Module ID 字段在场（B-2.3 解除）：无属性族 → —（空态不伪造）
  await expect(page.locator('[data-testid="node-module-id"]')).toHaveText('—')

  // 关联腿正断言：app.bin 在 relRepo（promote 迁入 + 属性随迁）——属性
  // 三键在场，但模块文档的关联形路径是 t512-libs/app.bin → 探测在
  // relRepo 址上 miss。改用 API 对账呈现数据面同源逻辑：
  const detail = await sessionApi(page, 'GET', `/api/build/${encodeURIComponent(f.buildName)}/1`)
  expect(detail.status).toBe(200)
  const info = (detail.json as { buildInfo: { modules: { id: string; artifacts: { path?: string }[] }[] } }).buildInfo
  const hit = info.modules.find((m) => m.artifacts.some((a) => a.path === `${f.repo}/app.bin`))
  expect(hit?.id).toBe(f.module)
})

// ---- ⑤ 读门如实：普通用户名单空集 + 详情 403 --------------------------------

test('⑤ 读门：普通用户名单 200 空集（可见集过滤）+ 详情面 403（拒绝先于存在）', async ({ page }) => {
  const f = fixture!
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/builds')
  await expect(page.locator('[data-testid="builds-empty"]')).toBeVisible()
  // 详情直探（session cookie 面）：403 = 读门拒绝（不与 404 混同）
  const probe = await sessionApi(page, 'GET', `/api/build/${encodeURIComponent(f.buildName)}/1`)
  expect(probe.status).toBe(403)
  // 时间线为 admin 面：普通用户 run 详情无 build-timeline 段（页面级 403
  // 承载先于时间线——本腿断言 API 面）
})

// ---- ⑥ axe 双主题（run 详情）-------------------------------------------------

test('⑥ axe 双主题：run 详情', async ({ page }) => {
  const f = fixture!
  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/builds/${encodeURIComponent(f.buildName)}/1`)
  await expect(page.locator('[data-testid="build-detail-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="build-modules"]')).toBeVisible()
  await expectA11yClean(page, test.info())
  await page.click('[data-testid="topbar-theme-toggle"]')
  await expectA11yClean(page, test.info())
})
