import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client, seedRepos, sessionApi } from '../m8/support/seed'

// T-439（M16 批次② 首票，FR-143.1/.2——表单三段结构 + 字段域补齐）：
//
//   ① 三段步进（B-2.5）：Basic | Advanced | Replications 步进条
//      （Artifactory 7.161.20 活体实测形态——jf-steps 三步条；7.84 审计
//      材料同构）。编辑态 × local 三段；建仓态两段（仓尚不存在，复制配置
//      无载体——POST /v1/replications 的 source_repo 前置校验必 400）。
//      非活跃步整步卸载（house 口径：锚计数 0，非 CSS 隐藏）。
//   ② 复制配置迁第三步（M6 能力语义零变化）：?section=replications 深链
//      直落第三步——仓列表 Run 动作与详情指针的既有落点零改造。
//   ③ 字段域八域表驱动（B-1.5 + B-3.12，Q8 冻结「全补」口径的 as-built
//      落地）：**活体实证两档**——本票 scratch 实例 PUT→GET 对账证明四域
//      decode-only 静默丢弃（repoLayoutRef/blackedOut/maxUniqueSnapshots/
//      archiveBrowsingEnabled——transport 解码不 400，但 configJSON 不转发，
//      GET 回显缺失）+ 三域无解码位（environments/notes/
//      suppressPomConsistencyChecks）→ 七域**预留位**（恒禁用 + 零提交，
//      R3 两档定案先例）；一域**实字段**（forceConanAuthentication——
//      local × conan，T-355A 全收 + adapter 行为消费；community 实例
//      license 门控不可选，条件腿自跳过）。
//   ④ 表单 footer（B-3.11/Q9 冻结）：重置钮移除（M1 锚点 Cancel +
//      Create/Save 两钮）；baseline 态随之退役。
//   ⑤ payload 净度（网络层对账）：七预留 wire 键零提交。
//   ⑥ API 漂移钉（tripwire）：PUT 带 blackedOut 族 → 200 但 GET 回显缺失
//      ——把 PRD「API 已收全」证据（repositories.go L55-61 仅解码层）的
//      契约漂移钉进断言；BE 承接票落地日本腿翻红即升级提示。
//   ⑦ axe 双主题：建仓两步 + 编辑第三步全形态 serious/critical = 0。
//
// 夹具纪律：Playwright 栈自建（仓库 PUT/seed），零外部状态；锚源 =
// console-ux §10.5 T-439 批（form-step-* 三枚 + 预留位族 + form-force-auth，
// v1.33）。

const RESERVED_WIRE_KEYS = [
  'repoLayoutRef',
  'blackedOut',
  'maxUniqueSnapshots',
  'archiveBrowsingEnabled',
  'environments',
  'notes',
  'suppressPomConsistencyChecks',
] as const

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

async function step(page: Page, id: 'basic' | 'advanced' | 'replications'): Promise<void> {
  await page.click(`[data-testid="form-step-${id}"]`)
}

/** 八域表驱动描述（AC1「字段域表驱动 spec」的表）：
 *  - kind: input = 预留位文本域（disabled + 「预留位」占位）；check = 预留位
 *    复选（disabled）；live-check = 实字段复选（可交互、进 body）。
 *  - fixture: 断言腿的仓型/包型前提（maven 域两枚与 conan 域一枚收窄呈现）。 */
interface Domain {
  name: string
  wire: string
  anchor: string
  kind: 'input' | 'check' | 'live-check'
  stepId: 'basic' | 'advanced'
  section: string
  fixture: { pkg?: string; remote?: boolean }
}

const DOMAINS: Domain[] = [
  { name: 'repoLayoutRef', wire: 'repoLayoutRef', anchor: 'form-repo-layout', kind: 'input', stepId: 'basic', section: 'form-section-general', fixture: {} },
  { name: 'environments(Stage)', wire: 'environments', anchor: 'form-environments', kind: 'input', stepId: 'basic', section: 'form-section-general', fixture: {} },
  { name: 'internal-description(notes)', wire: 'notes', anchor: 'form-internal-description', kind: 'input', stepId: 'basic', section: 'form-section-general', fixture: {} },
  { name: 'blackedOut', wire: 'blackedOut', anchor: 'form-blacked-out', kind: 'check', stepId: 'advanced', section: 'form-section-advanced', fixture: {} },
  { name: 'archiveBrowsingEnabled', wire: 'archiveBrowsingEnabled', anchor: 'form-archive-browsing', kind: 'check', stepId: 'advanced', section: 'form-section-advanced', fixture: {} },
  { name: 'maxUniqueSnapshots', wire: 'maxUniqueSnapshots', anchor: 'form-max-unique-snapshots', kind: 'input', stepId: 'advanced', section: 'form-section-policy', fixture: { pkg: 'maven' } },
  { name: 'suppressPomConsistencyChecks', wire: 'suppressPomConsistencyChecks', anchor: 'form-suppress-pom', kind: 'check', stepId: 'advanced', section: 'form-section-policy', fixture: { pkg: 'maven' } },
  { name: 'forceConanAuthentication', wire: 'forceConanAuthentication', anchor: 'form-force-auth', kind: 'live-check', stepId: 'advanced', section: 'form-section-advanced', fixture: { pkg: 'conan' } },
]

/** 域落点断言（表驱动核心）：控件在正确的步/节、正确的禁用/使能形态 */
async function expectDomainAnchor(page: Page, d: Domain): Promise<void> {
  const loc = page.locator(`[data-testid="${d.anchor}"]`)
  await expect(loc, `${d.name} anchor`).toBeVisible()
  if (d.kind === 'input') {
    await expect(loc, `${d.name} reserved input disabled`).toBeDisabled()
    await expect(loc).toHaveValue('')
  } else if (d.kind === 'check') {
    await expect(loc, `${d.name} reserved check disabled`).toBeDisabled()
    await expect(loc).not.toBeChecked()
  } else {
    await expect(loc, `${d.name} live check enabled`).toBeEnabled()
  }
}

// ---------------------------------------------------------------------------
// ① 三段步进 + 深链 + 复制配置迁第三步（B-2.5 / FR-143.1）
// ---------------------------------------------------------------------------

test('stepper: Basic|Advanced|Replications three segments; steps swap by unmount; create form is two segments', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const key = uniq('t439s')

  // 建仓态：两段（无 Replications 步——仓尚不存在）
  await page.goto('/binflow/ui/admin/repositories/new')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await page.click('[data-testid="pkg-grid-item-generic"]')
  await expect(page.locator('[data-testid="form-step-basic"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-step-advanced"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-step-replications"]')).toHaveCount(0)

  // 步进切换 = 整步卸载（锚计数 0，非 CSS 隐藏）：基础步只见常规节
  await expect(page.locator('[data-testid="form-section-general"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-section-advanced"]')).toHaveCount(0)
  await step(page, 'advanced')
  await expect(page.locator('[data-testid="form-section-advanced"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-section-general"]')).toHaveCount(0)
  await step(page, 'basic')
  await expect(page.locator('[data-testid="form-section-general"]')).toBeVisible()

  // 编辑态 × local：三段 + 深链 ?section=replications 直落第三步
  await seedRepos(m8Client(), [{ key }])
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await expect(page.locator('[data-testid="form-step-replications"]')).toBeVisible()
  await expect(page.locator('[data-testid="form-step-replications"]')).toHaveAttribute('aria-label', 'Step 3 of 3: Replications')
  await expect(page.locator('[data-testid="form-section-replications"]')).toHaveCount(0) // 未切步不渲染
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
  await expect(page.locator('[data-testid="form-section-replications"]')).toBeVisible()
  await expect(page.locator('[data-testid="repl-empty"]')).toBeVisible()

  // remote 编辑态：仍两段（复制 push 源 = local，ADR-0021）
  const rt = uniq('t439rt')
  await m8Client().request('PUT', `/binflow/api/repositories/${rt}`, {
    body: { rclass: 'remote', packageType: 'maven', url: 'https://repo1.maven.org/maven2' },
  })
  await page.goto(`/binflow/ui/admin/repositories/${rt}/edit`)
  await expect(page.locator('[data-testid="form-step-replications"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ② 字段域八域表驱动（B-1.5 + B-3.12）——预留位形态 + 实字段条件腿 ----------
// ---------------------------------------------------------------------------

test('field domains: seven reserved (disabled, zero-commit posture) across correct step/section; table-driven', async ({
  page,
}) => {
  await loginAs(page, 'admin')

  // generic local 建仓态：五域（basic 三 + advanced 两）+ maven 两域（policy 节）
  await page.goto('/binflow/ui/admin/repositories/new')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await page.click('[data-testid="pkg-grid-item-maven"]')

  for (const d of DOMAINS.filter((x) => x.fixture.pkg !== 'conan')) {
    if (d.stepId === 'advanced') await step(page, 'advanced')
    else await step(page, 'basic')
    // 节归属（maven 域两枚在 policy 节内）
    const section = page.locator(`[data-testid="${d.section}"]`)
    await expect(section).toBeVisible()
    await expectDomainAnchor(page, d)
    if (d.kind === 'input') {
      // 预留位如实标注（R3 先例同款文案契约）
      await expect(section).toContainText('预留位')
    }
    await step(page, 'basic') // 复位到 basic 供下一域判步
  }

  // 预留组根锚（两枚）：basic 与 advanced 各一组
  await expect(page.locator('[data-testid="form-reserved-basic"]')).toBeVisible()
  await step(page, 'advanced')
  await expect(page.locator('[data-testid="form-reserved-advanced"]')).toBeVisible()
})

test('field domains: forceConanAuthentication live round-trip (conditional — conan slot availability)', async ({
  page,
}) => {
  await loginAs(page, 'admin')

  // conan 是 license 门控槽位（community 地板不可建）——条件腿自跳过
  // （PRD 条件票纪律：未触发不构成 DoD 缺口；pro 实例上此腿全跑）
  const probe = await sessionApi(page, 'GET', '/api/v1/addons')
  if (probe.status !== 200) test.skip(true, 'addons API unavailable')
  const rows = (Array.isArray(probe.json) ? probe.json : []) as { id: string; kind: string; enabled: boolean }[]
  const conan = rows.find((s) => s.id === 'conan' && s.kind === 'package-type')
  if (!conan || !conan.enabled) test.skip(true, 'conan slot not enabled on this instance (community)')

  const key = uniq('t439conan')
  await m8Client().request('PUT', `/binflow/api/repositories/${key}`, {
    body: { rclass: 'local', packageType: 'conan', forceConanAuthentication: true },
  })
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await step(page, 'advanced')
  const d = DOMAINS.find((x) => x.wire === 'forceConanAuthentication')!
  await expectDomainAnchor(page, d)
  // 字面锚定（表驱动模板之外的直引——锚册消费口径）
  await expect(page.locator('[data-testid="form-force-auth"]')).toBeChecked() // 回显
  // flip-off 过 round trip（POINTER 语义——显式 false 必须过提交）
  const posted = page.waitForRequest((r) => r.method() === 'POST' && /\/api\/repositories\//.test(r.url()))
  await page.locator(`[data-testid="${d.anchor}"]`).click()
  await page.click('[data-testid="form-submit"]')
  const body = JSON.parse((await posted).postData() ?? '{}') as Record<string, unknown>
  expect(body.forceConanAuthentication).toBe(false)
  const got = await sessionApi(page, 'GET', `/api/repositories/${key}`)
  const cfg = ((got.json as { configuration?: Record<string, unknown> }).configuration ?? {})
  expect(cfg.forceConanAuthentication).toBe(false)

  await m8Client().request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
})

// ---------------------------------------------------------------------------
// ③ footer 重置钮移除（B-3.11/Q9）+ payload 净度（网络层） --------------------
// ---------------------------------------------------------------------------

test('footer: reset removed (Cancel + Create/Save only); reserved wire keys never ride the create body', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const key = uniq('t439f')

  await page.goto('/binflow/ui/admin/repositories/new')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await page.click('[data-testid="pkg-grid-item-generic"]')
  await page.fill('[data-testid="form-key"]', key)
  await page.fill('[data-testid="form-description"]', 't439 footer+purity probe')

  // 重置钮移除（M1 锚点两钮）；取消/提交在场
  await expect(page.locator('[data-testid="form-reset"]')).toHaveCount(0)
  await expect(page.locator('.form-actions button')).toHaveCount(2)

  // payload 净度：PUT body 键集 = wire 闭集——七预留 wire 键零提交
  const put = page.waitForRequest((r) => r.method() === 'PUT' && r.url().includes(`/api/repositories/${key}`))
  await page.click('[data-testid="form-submit"]')
  const body = JSON.parse((await put).postData() ?? '{}') as Record<string, unknown>
  for (const k of RESERVED_WIRE_KEYS) expect(body, `reserved key ${k} must not ride`).not.toHaveProperty(k)
  expect(body.rclass).toBe('local')
  expect(body.packageType).toBe('generic')

  await expect(page.locator('[data-testid="toast"]')).toContainText(`Successfully created repository '${key}'`)
  await m8Client().request('DELETE', `/binflow/api/repositories/${key}`)
})

// ---------------------------------------------------------------------------
// ④ API 漂移钉（tripwire）：decode-only 静默丢弃的契约漂移钉进断言 -------------
// ---------------------------------------------------------------------------

test('api drift pin: PUT accepts the four B-1.5 fields (200) but GET echo misses them — decode-only drop', async ({
  page,
}) => {
  await loginAs(page, 'admin')
  const key = uniq('t439d')
  const made = await sessionApi(page, 'PUT', `/api/repositories/${key}`, {
    rclass: 'local', packageType: 'maven',
    repoLayoutRef: 'maven-2-default', blackedOut: true, maxUniqueSnapshots: 7, archiveBrowsingEnabled: true,
    includesPattern: '**/*.jar', // 对照组：configJSON 转发的字段
  })
  expect(made.status).toBe(200) // 解码层不拒（PRD「已收」证据即止于此）

  const got = await sessionApi(page, 'GET', `/api/repositories/${key}`)
  expect(got.status).toBe(200)
  const cfg = ((got.json as { configuration?: Record<string, unknown> }).configuration ?? {})
  // 对照组回显（机制证明——configJSON 转发链活着）
  expect(cfg.includesPattern).toBe('**/*.jar')
  // 四域静默丢弃（decode-only）：BE 承接票（configJSON 转发 + 行为联动）落地
  // 后本腿翻红——即升级提示：预留位转正 + 本 spec 三链腿改提交-回显-行为。
  for (const k of ['repoLayoutRef', 'blackedOut', 'maxUniqueSnapshots', 'archiveBrowsingEnabled'] as const) {
    expect(cfg, `drift pin: ${k} currently dropped by configJSON`).not.toHaveProperty(k)
  }

  await m8Client().request('DELETE', `/binflow/api/repositories/${key}`)
})

// ---------------------------------------------------------------------------
// ⑤ axe 双主题：建仓两步 + 编辑第三步全形态 -----------------------------------
// ---------------------------------------------------------------------------

test('axe: stepper form clean in both themes (create basic+advanced, edit replications step)', async (
  { page },
  testInfo: TestInfo,
) => {
  test.setTimeout(300_000) // 4 面 × 双主题；axe 在默认并发下实测 30s+/面
  await loginAs(page, 'admin')
  const key = uniq('t439ax')
  await seedRepos(m8Client(), [{ key }])

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)

    await page.goto('/binflow/ui/admin/repositories/new')
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
    await page.click('[data-testid="pkg-grid-item-maven"]')
    await expect(page.locator('[data-testid="form-reserved-basic"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repo-form-page"]' })
    await step(page, 'advanced')
    await expect(page.locator('[data-testid="form-reserved-advanced"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repo-form-page"]' })

    await page.goto(`/binflow/ui/admin/repositories/${key}/edit?section=replications`)
    await expect(page.locator('[data-testid="form-section-replications"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repo-form-page"]' })
  }
})
