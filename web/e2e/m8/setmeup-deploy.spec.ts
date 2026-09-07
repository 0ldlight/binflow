import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from './support/a11y'
import { expectCopied, grantClipboard } from './support/clipboard'
import { loginAs } from './support/roles'
import { m8Client, seedRepos, sessionApi } from './support/seed'

// T-242 Set Me Up / Deploy 对话框族（console-m8 §4.1/§4.2 + §7.4 step-up 融合
// + §8 T-231 债券）。断言口径 = e2e/m8/README §2：锚断言 + 操作流对照 +
// sessionApi 对账；错误文案断言 ADR-0027 决策 5 逐字（error_description）。
//
// T-382 迁移（D1 抽屉化，console-artifactory-parity v1.1 实测参数）：Set Me Up
// 壳 = 居中 Dialog → 右侧 Drawer（anchor right + temporary），宽 50vw 档
// （clamp 480~800，1280 视口 = 640px——非旧票 480 固定档）、全高、右上 X +
// Esc/遮罩关闭；Tab = Configure/Deploy/Resolve 三枚；底栏 = 左返回链接 +
// 右 Done 主按钮；步 0 = 包型药丸。铸币/step-up/OIDC 续铸行为语义断言全量
// 保留（smu-* 锚族零改名——壳替换不动锚）。
//
// 三入口（树工具栏 / 仓库列表行 / 仓库详情头）在本 spec 内全部走通；
// step-up 腿两层：(a) mock 拦截腿（verbatim ADR 错误体 + 第三次放行真铸，
// 任何实例可跑）；(b) 真实 armed 实例腿（BINFLOW_AUTH__TOKEN_STEP_UP=true
// 的实例全链——默认实例自动 skip）。

const ADR_STEP_UP_REQUIRED = JSON.stringify({
  error: 'step_up_required',
  error_description: 'step-up authentication required to mint a token',
})
const ADR_STEP_UP_INVALID = JSON.stringify({
  error: 'step_up_invalid',
  error_description: 'step-up credential rejected, expired, or already used',
})

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 实例内已有仓库的包类型集合（网格「只列已有仓库的包类型」的对照源） */
async function livePackageTypes(page: Page): Promise<Set<string>> {
  const res = await sessionApi(page, 'GET', '/api/repositories')
  expect(res.status).toBe(200)
  const repos = res.json as { packageType: string }[]
  return new Set(repos.map((r) => r.packageType))
}

// ---- Set Me Up：仓库上下文直达 + 铸币 + 命令块 + Tab + 剪贴板（admin 腿） ----

/** D1 抽屉几何断言（T-382，v1.1 实测参数）：右缘贴齐视口右缘、50vw 档
 *  （clamp 480~800——1280 视口 = 640px，非旧 480 固定档）、全高。先等
 *  Slide 过渡收敛（transform none）——入场动画中途取 box 会读到滑入途
 *  中的 x（宽度恒定、x 在动）。 */
async function expectDrawerGeometry(page: Page, drawer: ReturnType<Page['locator']>): Promise<void> {
  const vw = page.viewportSize()?.width ?? 1280
  const vh = page.viewportSize()?.height ?? 720
  await expect(drawer).toHaveCSS('transform', 'none')
  const box = await drawer.boundingBox()
  expect(box, 'drawer paper has a box').toBeTruthy()
  expect(box!.width).toBeCloseTo(Math.min(Math.max(480, vw / 2), 800), 0)
  expect(box!.x + box!.width).toBeCloseTo(vw, 0) // anchor right：右缘贴齐
  expect(box!.height).toBeCloseTo(vh, 0) // 全高
}

test('setmeup: repo context opens the client drawer directly; mint + token-embedded commands + copy', async ({
  page,
}) => {
  test.setTimeout(120_000)
  test.skip(!(await grantClipboard(page)), 'clipboard legs are chromium-only (the supported matrix)')
  const key = uniq('m8smu')
  await seedRepos(m8Client(), [{ key }, { key: `${key}-npm`, packageType: 'npm' }])

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/artifacts')
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await page.click('[data-testid="tree-setmeup"]')

  // 选中仓库 → 直达主面板（reverse §4.1），仓库下拉预选当前仓
  const dialog = page.locator('[data-testid="smu-dialog"]')
  await expect(dialog).toBeVisible()
  await expect(page.locator('[data-testid="smu-grid"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)
  await expect(dialog).toContainText('配置 Generic 客户端')

  // T-382 抽屉形态：右侧 50vw 档 + 全高（v1.1 实测收口）
  await expectDrawerGeometry(page, dialog)

  // Tab 三枚（Configure / Deploy / Resolve——v1.1 实测收口）
  for (const t of ['configure', 'deploy', 'resolve']) {
    await expect(page.locator(`[data-testid="smu-tab-${t}"]`)).toBeVisible()
  }
  await expect(dialog.locator('[role="tab"]')).toHaveCount(3)

  // 焦点陷阱（Drawer 形态复测）：连打 Tab 十次焦点仍在抽屉内（键盘 leg
  // 在 Deploy Dialog 上有同款断言——T-382 壳换 Drawer 后此处复测）
  for (let i = 0; i < 10; i++) await page.keyboard.press('Tab')
  const trappedIn = await page.evaluate(() =>
    !!document.activeElement?.closest('[data-testid="smu-dialog"]'),
  )
  expect(trappedIn, 'focus stays trapped inside the drawer').toBe(true)

  // 铸币（admin 会话 = ADR-0027 决策 1 豁免臂，无二次口令）
  await page.click('[data-testid="smu-generate"]')
  const panel = page.locator('[data-testid="smu-token-panel"]')
  await expect(panel).toBeVisible()
  await expect(panel).toContainText('关闭抽屉后不可再查看')
  const token = (await page.locator('[data-testid="smu-token"]').textContent()) ?? ''
  expect(token.length).toBeGreaterThan(20)

  // 命令块回填真实凭据：配置面占位符退场（generic 的配置侧不含凭据也
  // 不含命令块——T-382 三分后 Configure 无 generic 配置步；token 明文进
  // 部署侧 curl -u）
  await expect(page.locator('[data-testid="smu-pane-configure"]')).not.toContainText('<TOKEN 或口令>')

  // 剪贴板断言：令牌全值（console-ux §7.3——拷贝不截断）
  await expectCopied(page, page.locator('button[aria-label="复制 API Token"]'), token)

  // Tab 切换（配置 → 部署）：部署侧命令含仓库 key + token 明文（回填凭据）
  await page.click('[data-testid="smu-tab-deploy"]')
  await expect(page.locator('[data-testid="smu-pane-deploy"]')).toBeVisible()
  await expect(page.locator('[data-testid="smu-cmd-dep-generic-0"] pre')).toContainText(token)
  await expect(page.locator('[data-testid="smu-pane-deploy"]')).toContainText(key)

  // Tab 切换（部署 → 解析，T-382 第三枚）：generic 解析侧 = curl 下载与校验
  await page.click('[data-testid="smu-tab-resolve"]')
  await expect(page.locator('[data-testid="smu-pane-resolve"]')).toBeVisible()
  await expect(page.locator('[data-testid="smu-cmd-res-generic-0"] pre')).toContainText(
    `/binflow/${key}/acme/app.tar.gz`,
  )
  // 命令块 pre 不折行（overflow-x:auto——mono 命令横向滚动，P2 原则）
  await expect(page.locator('[data-testid="smu-cmd-res-generic-0"] pre')).toHaveCSS(
    'overflow-x',
    'auto',
  )

  // 对账（§2.6）：UI 说铸了 token——让同源 Bearer 臂复核管理面可达
  const bearer = await page.evaluate(async (t) => {
    const r = await fetch('/binflow/api/repositories', { headers: { Authorization: `Bearer ${t}` } })
    return r.status
  }, token)
  expect(bearer).toBe(200)

  // 底栏形态（v1.1 实测）：左返回链接 + 右 Done 主按钮；Done 关闭 + 回焦
  // 启动元素（L02——tree-setmeup）
  await expect(page.locator('[data-testid="smu-back"]')).toBeVisible()
  await page.click('[data-testid="smu-done"]')
  await expect(page.locator('[data-testid="smu-dialog"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="tree-setmeup"]')).toBeFocused()
})

// ---- Set Me Up：包类型药丸（无仓库上下文入口；只列已有仓库的包类型） ----

test('setmeup grid: package types = union of existing repos; back link returns to pills; X closes', async ({
  page,
}) => {
  const key = uniq('m8grid')
  await seedRepos(m8Client(), [{ key }, { key: `${key}-npmpkg`, packageType: 'npm' }])

  await loginAs(page, 'admin')
  // T-492（B-3.2）：/artifacts 进入即自动选中首仓库——无仓库上下文的根态
  // （步 0 药丸的前提）经「带选中进入 → 侧栏导航回根」重建（回根不重复
  // 自动选中）
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await page.click('[data-testid="app-nav"] a.nav-item:text-is("制品")')
  await page.click('[data-testid="tree-setmeup"]')

  await expect(page.locator('[data-testid="smu-grid"]')).toBeVisible()
  // 药丸集合 = 实例内已有仓库的包类型并集（对齐 reverse §4.1）
  const types = await livePackageTypes(page)
  for (const pt of ['generic', 'docker', 'maven', 'npm', 'pypi']) {
    const want = types.has(pt) ? 1 : 0
    await expect(page.locator(`[data-testid="smu-grid-item-${pt}"]`)).toHaveCount(want)
  }

  // 选 npm → 主面板；下拉只列 npm 仓
  await page.click('[data-testid="smu-grid-item-npm"]')
  await expect(page.locator('[data-testid="smu-repo"]')).toBeVisible()
  const picked = await page.locator('[data-testid="smu-repo"]').inputValue()
  const npmKeys = (await sessionApi(page, 'GET', '/api/repositories?packageType=npm'))
  expect(npmKeys.status).toBe(200)
  const npmKeyList = (npmKeys.json as { key: string }[]).map((r) => r.key)
  expect(npmKeyList.length).toBeGreaterThan(0)
  expect(npmKeyList).toContain(picked)
  for (const opt of await page.locator('[data-testid="smu-repo"] option').evaluateAll((els) =>
    els.map((e) => (e as HTMLOptionElement).value),
  )) {
    expect(npmKeyList).toContain(opt)
  }

  // 「选择不同的包类型」返回药丸（reverse §4.1 返回链接——T-382 起在底栏左）
  await page.click('[data-testid="smu-back"]')
  await expect(page.locator('[data-testid="smu-grid-item-generic"]')).toBeVisible()
  // 右上 X 关闭（抽屉族通用规格，§4；smu-close 随 T-382 复役）
  await page.click('[data-testid="smu-close"]')
  await expect(page.locator('[data-testid="smu-dialog"]')).toHaveCount(0)
})

// ---- Set Me Up：step-up 内联腿（mock 拦截：ADR 逐字错误体 + 第三次真铸） ----

test('setmeup step-up: 401 step_up_required -> inline password form; invalid -> verbatim ADR text; retry mints', async ({
  page,
}) => {
  const key = uniq('m8step')
  await seedRepos(m8Client(), [{ key }])

  // readonly_admin：非 admin web session 臂（step-up 的作用域）+ 可列仓库
  const sess = await loginAs(page, 'readonly_admin')
  await page.goto('/binflow/ui/artifacts')
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await page.click('[data-testid="tree-setmeup"]')
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)

  // 拦截铸币端点：① 401 step_up_required（ADR 逐字）② 错口令 401
  // step_up_invalid（ADR 逐字）③ 放行到真实服务端（默认实例 step-up 关闭
  // → 200 真铸；armed 实例携正确 step_up_password 同样 200）
  const bodies: unknown[] = []
  let calls = 0
  await page.route('**/api/security/token', async (route) => {
    calls += 1
    try {
      bodies.push(route.request().postDataJSON())
    } catch {
      bodies.push(null)
    }
    if (calls === 1) {
      return route.fulfill({ status: 401, contentType: 'application/json', body: ADR_STEP_UP_REQUIRED })
    }
    if (calls === 2) {
      return route.fulfill({ status: 401, contentType: 'application/json', body: ADR_STEP_UP_INVALID })
    }
    return route.continue()
  })

  // ① 首次铸币 → 401 required → 对话框内联口令框（不弹第二层）+ 聚焦
  await page.click('[data-testid="smu-generate"]')
  const stepUp = page.locator('[data-testid="smu-stepup"]')
  await expect(stepUp).toBeVisible()
  await expect(page.locator('[data-testid="smu-password"]')).toBeFocused()
  await expect(page.locator('[data-testid="smu-token-panel"]')).toHaveCount(0)

  // ② 错误口令 → 401 invalid → 内联错误 = error_description 逐字
  await page.fill('[data-testid="smu-password"]', 'definitely-wrong-password')
  await page.click('[data-testid="smu-password-submit"]')
  await expect(page.locator('[data-testid="smu-password-error"]')).toHaveText(
    'step-up credential rejected, expired, or already used',
  )
  await expect(stepUp).toBeVisible() // 内联呈现，仍在原对话框
  await expect(page.locator('[data-testid="smu-stepup"]')).toHaveCount(1)

  // ③ 正确口令 → 续铸成功（请求体携带 step_up_password；token 面板出现）
  await page.fill('[data-testid="smu-password"]', sess.password)
  await page.click('[data-testid="smu-password-submit"]')
  await expect(page.locator('[data-testid="smu-token-panel"]')).toBeVisible()
  expect(calls).toBe(3)
  const third = bodies[2] as { step_up_password?: string; expires_in?: number }
  expect(third.step_up_password).toBe(sess.password)
  expect(third.expires_in).toBe(86400)
})

// ---- Set Me Up：真实 armed 实例腿（BINFLOW_AUTH__TOKEN_STEP_UP=true 才跑） ----

test('setmeup step-up (real armed instance): full chain against the live gate', async ({ page }) => {
  const key = uniq('m8armed')
  await seedRepos(m8Client(), [{ key }])

  const sess = await loginAs(page, 'readonly_admin')
  // 探针：armed 实例的非 admin session 铸币答 401 step_up_required；默认
  // 实例（step-up 关闭）直接 200 → skip（mock 腿已覆盖默认实例）
  const probe = await sessionApi(page, 'POST', '/api/security/token', {
    grant_type: 'client_credentials',
    expires_in: 3600,
  })
  test.skip(
    probe.status !== 401 || (probe.json as { error?: string })?.error !== 'step_up_required',
    'instance not armed with auth.token_step_up (covered by the mocked leg)',
  )

  await page.goto('/binflow/ui/artifacts')
  await page.click(`[data-testid="tree-repo-${key}"]`)
  await page.click('[data-testid="tree-setmeup"]')
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)

  await page.click('[data-testid="smu-generate"]')
  await expect(page.locator('[data-testid="smu-stepup"]')).toBeVisible()

  // 真服务端错口令 → ADR-0027 决策 5 逐字（服务端原文，不由 UI 改写）
  await page.fill('[data-testid="smu-password"]', 'wrong-password-for-probe')
  await page.click('[data-testid="smu-password-submit"]')
  await expect(page.locator('[data-testid="smu-password-error"]')).toHaveText(
    'step-up credential rejected, expired, or already used',
  )

  // 正确口令 → 真铸成功
  await page.fill('[data-testid="smu-password"]', sess.password)
  await page.click('[data-testid="smu-password-submit"]')
  await expect(page.locator('[data-testid="smu-token-panel"]')).toBeVisible()
})

// ---- Deploy 对话框：拖拽上传 + T-231 特殊字符路径（% / 空格 / 中文） ----

test('deploy dialog: drag-drop upload with special-char filenames; encoded echo; checksum badge; API reconcile', async ({
  page,
}) => {
  const key = uniq('m8dep')
  await seedRepos(m8Client(), [{ key }])
  const names = ['50%off.bin', 'hello world.txt', '中文包.tar.gz']

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/artifacts')
  await page.click('[data-testid="tree-deploy"]')

  const dialog = page.locator('[data-testid="deploy-dialog"]')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('部署 Deploy')
  await page.selectOption('[data-testid="deploy-repo"]', key)
  // 包类型只读回显（§4.2 字段序）
  await expect(dialog).toContainText('Generic')

  // 拖拽投放（Chromium DataTransfer 合成 drop 事件——真实拖拽路径）
  await page.locator('[data-testid="deploy-drop"]').evaluate((el, files) => {
    const dt = new DataTransfer()
    for (const [name, body] of files) {
      dt.items.add(new File([body], name, { type: 'application/octet-stream' }))
    }
    el.dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true, cancelable: true }))
  }, names.map((n) => [n, `t231-body-${n}\n`] as [string, string]))

  // 行入队（hashing）+「部署」启泵（显式提交步，§4.2）
  for (const n of names) await expect(page.locator(`[data-testid="deploy-row-${n}"]`)).toBeVisible()
  await page.click('[data-testid="deploy-submit"]')

  // 201 + checksum 比对徽标（成功态：§3.2 矩阵 Deploy 行）
  for (const n of names) {
    await expect(page.locator(`[data-testid="deploy-verify-${n}"]`)).toHaveText('✓ checksum 一致', {
      timeout: 30_000,
    })
  }

  // T-231 编码回显：% → %25、空格 → %20、中文 → UTF-8 percent（只读 mono）
  await expect(page.locator('[data-testid="deploy-echo-50%off.bin"]')).toContainText('50%25off.bin')
  await expect(page.locator('[data-testid="deploy-echo-hello world.txt"]')).toContainText('hello%20world.txt')
  await expect(page.locator('[data-testid="deploy-echo-中文包.tar.gz"]')).toContainText(
    '%E4%B8%AD%E6%96%87%E5%8C%85.tar.gz',
  )

  // 对账（§2.6）：树面按原名可见（服务端原值，不二次编解码）
  const ls = await sessionApi(page, 'GET', `/api/storage/${key}`)
  expect(ls.status).toBe(200)
  const children = ((ls.json as { children?: { uri: string }[] }).children ?? []).map((c) => c.uri)
  for (const n of names) expect(children).toContain(`/${n}`)

  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toHaveCount(0)
})

// ---- 三入口可达（树工具栏已由上腿覆盖；此处：列表行 + 详情头） ----

test('entry points: repositories list row and repo detail header open both dialogs', async ({ page }) => {
  const key = uniq('m8ent')
  await seedRepos(m8Client(), [{ key }])

  await loginAs(page, 'admin')

  // 仓库列表行：Set Me Up（行导航不触发——按钮 stopPropagation）
  await page.goto('/binflow/ui/admin/repositories/local')
  await page.click(`[data-testid="repos-setmeup-${key}"]`)
  await expect(page.locator('[data-testid="smu-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)
  await expect(page).toHaveURL(/\/admin\/repositories\/local$/) // 未跳详情页
  // 遮罩关闭（抽屉族通用规格）：点抽屉外左侧遮罩 + 回焦启动元素（L02）
  const vw = page.viewportSize()?.width ?? 1280
  const vh = page.viewportSize()?.height ?? 720
  await page.mouse.click(Math.round(vw * 0.25), Math.round(vh / 2))
  await expect(page.locator('[data-testid="smu-dialog"]')).toHaveCount(0)
  await expect(page.locator(`[data-testid="repos-setmeup-${key}"]`)).toBeFocused()

  // 仓库列表行：部署（generic local 行有 Deploy 入口）
  await page.click(`[data-testid="repos-deploy-${key}"]`)
  await expect(page.locator('[data-testid="deploy-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="deploy-repo"]')).toHaveValue(key)
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toHaveCount(0)

  // 仓库详情头：Set Me Up + Deploy
  await page.goto(`/binflow/ui/admin/repositories/${key}`)
  await page.click('[data-testid="repo-setmeup"]')
  await expect(page.locator('[data-testid="smu-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)
  await page.keyboard.press('Escape')
  await page.click('[data-testid="repo-deploy"]')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="deploy-repo"]')).toHaveValue(key)
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-testid="deploy-dialog"]')).toHaveCount(0)
})

// ---- axe 结构可达性（serious/critical = 0 门；§9）----------------------------

// T-344C：对话框换 MUI 后入场带过渡动画——axe 在动画中途采样会把半透明
// 栈算进对比度（实测 4.48 < 4.5 假阳性）。扫描前等过渡收敛（终态断言语义
// 不变，只是不再对动画帧求值）。T-382 起 Set Me Up 壳 = Drawer（paper 走
// Slide，不透明度恒 1——本等待退化为终态在场断言，Deploy Dialog 的 Fade
// 仍由同款等待覆盖）。
async function settleDialog(page: Page, scope: string): Promise<void> {
  await expect(page.locator(scope)).toHaveCSS('opacity', '1')
}

test('axe: setmeup drawer (pills + main) clean in both themes; deploy dialog clean', async ({ page }, testInfo) => {
  const key = uniq('m8axe')
  await seedRepos(m8Client(), [{ key }, { key: `${key}-npmpkg`, packageType: 'npm' }])

  await loginAs(page, 'admin')

  // T-382：抽屉双主题扫描（a11y-sweep 同款 localStorage 姿势——路由面扫
  // 不到抽屉态，抽屉的 serious=0 门在本 spec 承载）
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    // T-492（B-3.2）：/artifacts 进入即自动选中首仓库——无仓库上下文的根态
    // （步 0 药丸前提）经「带选中进入 → 侧栏导航回根」重建
    await page.goto(`/binflow/ui/artifacts/${key}`)
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await page.click('[data-testid="app-nav"] a.nav-item:text-is("制品")')

    // 药丸态（步 0）
    await page.click('[data-testid="tree-setmeup"]')
    await expect(page.locator('[data-testid="smu-grid-item-generic"]')).toBeVisible()
    await settleDialog(page, '[data-testid="smu-dialog"]')
    await expectA11yClean(page, testInfo, { include: '[data-testid="smu-dialog"]' })
    await page.keyboard.press('Escape')

    // 主面板（含铸币区 + 三 Tab 命令块；Resolve 为 T-382 新增面）
    await page.click(`[data-testid="tree-repo-${key}"]`)
    await page.click('[data-testid="tree-setmeup"]')
    await expect(page.locator('[data-testid="smu-repo"]')).toHaveValue(key)
    await page.click('[data-testid="smu-tab-resolve"]')
    await expect(page.locator('[data-testid="smu-pane-resolve"]')).toBeVisible()
    await settleDialog(page, '[data-testid="smu-dialog"]')
    await expectA11yClean(page, testInfo, { include: '[data-testid="smu-dialog"]' })
    await page.keyboard.press('Escape')
  }

  // Deploy 对话框（含拖拽区 + GAV 回显面隐藏——generic 仓；居中 Dialog
  // 形态不变——决策项 C 暂行，单主题腿维持）
  await page.click('[data-testid="tree-deploy"]')
  await expect(page.locator('[data-testid="deploy-drop"]')).toBeVisible()
  await settleDialog(page, '[data-testid="deploy-dialog"]')
  await expectA11yClean(page, testInfo, { include: '[data-testid="deploy-dialog"]' })
})
