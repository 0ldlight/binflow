import { execSync } from 'node:child_process'
import { expect, test } from '@playwright/test'

// T-104 QA 补充断言（conductor 派单：W 序列浏览器面未覆盖缝隙）：
// ① W10 docker 腿：UI 表单建 docker-ui-local（local+docker）→ REST 对账
//    packageType==docker（FR-24-AC1 原文仓型；既有 spec 只盖 generic/maven）
// ② docker 真客户端播种（dind 内 docker push，T-103 同款手法）→ W12c docker
//    树可见 manifest node（FR-25-AC4 P1：统一路径树呈现——特化视图为 T-100
//    遗留 1 的 P1 登记项）→ W11 非空仓两段删除 + 内容面 404
// ③ W10b：UI 编辑 remote url → REST 回显新值；password 永不回显
// ④ W34 actor 过滤腿：UI 行集 == REST ?actor= 同过滤结果集（抽样对账）
// ⑤ W23b UI 腿：审计页 DOM 零秘密（token 明文 / 错误口令字面量）
//
// 运行前提：make console && make build 的真二进制前台 serve，BASE 指向它；
// docker 腿额外需要本机 docker CLI + 名为 t104-dind 的 dind 容器
// （--insecure-registry 指向本实例；QA 手册见 T-104 报告）。

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'
const DOCKER_REPO = 'docker-ui-local'
const DIND = process.env.QA_DIND ?? 't104-dind'

type Page = import('@playwright/test').Page

test.describe.configure({ mode: 'serial' })

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

async function login(page: Page, user = ADMIN, pw = ADMIN_PW) {
  await page.fill('[data-testid="login-username"]', user)
  await page.fill('[data-testid="login-password"]', pw)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

async function api(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; text: string }> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      })
      return { status: res.status, text: await res.text() }
    },
    { method, path, body },
  )
}

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

function watchServerErrors(page: Page): string[] {
  const bad: string[] = []
  page.on('response', (r) => {
    if (r.status() >= 500) bad.push(`${r.status()} ${r.url()}`)
  })
  return bad
}

/** dind 可用性（docker CLI + 容器在跑）；不可用则整段 skip 并记录降级 */
function dindAvailable(): boolean {
  try {
    execSync('docker info > /dev/null 2>&1', { stdio: 'pipe' })
    execSync(`docker inspect ${DIND} > /dev/null 2>&1`, { stdio: 'pipe' })
    return true
  } catch {
    return false
  }
}

test('W10: UI form creates docker-ui-local (local+docker); REST packageType reconciles', async ({ page }) => {
  const errors = watchServerErrors(page)
  await page.goto('/binflow/ui/')
  await login(page)
  // 自愈收口：前次运行残留先清（幂等）
  await api(page, 'DELETE', `/api/repositories/${DOCKER_REPO}?deleteContent=true`)

  await page.goto('/binflow/ui/repositories/new')
  // 步骤 1：local（默认）+ docker
  await expect(page.locator('[data-testid="form-rclass-local"]')).toBeChecked()
  await page.click('[data-testid="form-package-docker"]')
  await page.click('[data-testid="form-next"]')
  await page.fill('[data-testid="form-key"]', DOCKER_REPO)
  await expect(page.locator('[data-testid="form-key-ok"]')).toBeVisible()
  await page.click('[data-testid="form-next"]')
  await page.click('[data-testid="form-submit"]')

  await expect(page).toHaveURL(new RegExp(`/binflow/ui/repositories/${DOCKER_REPO}$`))

  // UI 列表行可见
  await page.goto('/binflow/ui/repositories')
  await expect(page.locator(`[data-testid="repos-row-${DOCKER_REPO}"]`)).toBeVisible()

  // REST 对账（AC①：curl GET packageType=="docker" 的浏览器等价腿）
  const got = await api(page, 'GET', `/api/repositories/${DOCKER_REPO}`)
  expect(got.status).toBe(200)
  const json = JSON.parse(got.text)
  expect(json.packageType).toBe('docker')
  expect(json.rclass).toBe('local')

  // P6：docker 仓不出上传入口，以接入命令块替代（tree-commands）
  await page.goto(`/binflow/ui/repositories/${DOCKER_REPO}/tree`)
  await expect(page.locator('[data-testid="tree-commands"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-upload"]')).toHaveCount(0)
  expect(errors).toEqual([])
})

test('seed docker-ui-local via real docker client (dind push)', async () => {
  test.skip(!dindAvailable(), `docker CLI / dind container ${DIND} unavailable — record as degraded`)
  const out = execSync(
    `docker exec ${DIND} sh -c "docker tag t104img:v1 host.docker.internal:${process.env.QA_PORT ?? '18120'}/${DOCKER_REPO}/t104img:v1 && docker push host.docker.internal:${process.env.QA_PORT ?? '18120'}/${DOCKER_REPO}/t104img:v1"`,
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], timeout: 120_000 },
  )
  // 真客户端成功形态：digest 落盘回执
  expect(out).toMatch(/digest: sha256:[0-9a-f]{64}/)
})

test('W12c + W11: docker tree shows manifest node; non-empty delete two-stage; content 404', async ({ page }) => {
  const errors = watchServerErrors(page)
  await page.goto('/binflow/ui/')
  await login(page)

  // 播种缺失守卫（seed 例被 skip 时本例同步降级，不留假绿）——v2 tags 面
  // 是播种成功的权威信号（storage 树的 manifests/ 是隐式目录，恒 404）
  const seeded = await page.evaluate(
    async ({ repo, auth }) => {
      const res = await fetch(`/v2/${repo}/t104img/tags/list`, {
        headers: { Authorization: `Basic ${btoa(auth)}` },
      })
      return res.status
    },
    { repo: DOCKER_REPO, auth: `${ADMIN}:${ADMIN_PW}` },
  )
  test.skip(seeded !== 200, 'docker seed missing (dind leg skipped) — record as degraded')

  // W12c（P1，统一路径树形态）：docker 仓树可见 image 目录与 manifest node
  await page.goto(`/binflow/ui/repositories/${DOCKER_REPO}/tree`)
  await expect(page.locator('[data-testid="tree-row-t104img"]')).toBeVisible({ timeout: 15_000 })
  await page.click('[data-testid="tree-row-t104img"]')
  await expect(page.locator('[data-testid="tree-row-manifests"]')).toBeVisible({ timeout: 15_000 })
  await page.click('[data-testid="tree-row-manifests"]')
  await page.waitForSelector('[data-testid="tree-list"] tbody tr', { timeout: 15_000 })
  const rowIds = await page.locator('[data-testid="tree-list"] tbody tr').evaluateAll((trs) =>
    trs.map((tr) => (tr as HTMLElement).getAttribute('data-testid') ?? ''),
  )
  // manifest node = <image>/manifests/<64hex>（manifestNodePath）；行 testid 取段名
  const hexRows = rowIds.filter((id) => /^tree-row-[0-9a-f]{64}$/.test(id))
  expect(hexRows.length).toBeGreaterThanOrEqual(1)

  // W11 两段流（非空仓）：不勾 deleteContent → 400 原因可见；勾选 → 成功
  await page.goto(`/binflow/ui/repositories/${DOCKER_REPO}`)
  await page.click('[data-testid="repo-delete-button"]')
  await page.fill('[data-testid="repo-delete-confirm-key"]', DOCKER_REPO)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="repo-delete-reason"]')).toContainText('node(s)')
  expect((await api(page, 'GET', `/api/repositories/${DOCKER_REPO}`)).status).toBe(200) // 仓仍在

  await page.fill('[data-testid="repo-delete-confirm-key"]', DOCKER_REPO)
  await page.check('[data-testid="repo-delete-content"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]')).toContainText('deleted successfully')

  // 内容面 404（W11 收口）：storage 树与 v2 tags 双腿
  const gone = await api(page, 'GET', `/api/storage/${DOCKER_REPO}/t104img/manifests`)
  expect(gone.status).toBe(404)
  const tags = await page.evaluate(
    async ({ repo, auth }) => {
      const res = await fetch(`/v2/${repo}/t104img/tags/list`, {
        headers: { Authorization: `Basic ${btoa(auth)}` },
      })
      return res.status
    },
    { repo: DOCKER_REPO, auth: `${ADMIN}:${ADMIN_PW}` },
  )
  expect(tags).toBe(404)
  expect(errors).toEqual([])
})

test('W10b: UI edits remote url; REST round-trips new value; password never echoed', async ({ page }) => {
  const errors = watchServerErrors(page)
  const key = uniq('t104rm')
  const secret = 't104-remote-secret'
  const url1 = 'https://mirror-a.example.com/upstream'
  const url2 = 'https://mirror-b.example.com/upstream'
  await page.goto('/binflow/ui/')
  await login(page)

  await page.goto('/binflow/ui/repositories/new')
  await page.click('[data-testid="form-rclass-remote"]')
  await page.click('[data-testid="form-package-generic"]')
  await page.click('[data-testid="form-next"]')
  await page.fill('[data-testid="form-key"]', key)
  await page.fill('[data-testid="form-url"]', url1)
  await page.fill('[data-testid="form-username"]', 'ci')
  await page.fill('[data-testid="form-password"]', secret)
  await page.click('[data-testid="form-next"]')
  await page.click('[data-testid="form-submit"]')
  await expect(page).toHaveURL(new RegExp(`/binflow/ui/repositories/${key}$`))

  let got = await api(page, 'GET', `/api/repositories/${key}`)
  expect(got.status).toBe(200)
  expect(got.text).not.toContain(secret)
  expect(JSON.parse(got.text).configuration.url).toBe(url1)

  // W10b：UI 改 url → 保存 → REST 单查回显新值
  await page.goto(`/binflow/ui/repositories/${key}/settings`)
  await page.click('[data-testid="form-next"]')
  await expect(page.locator('[data-testid="form-url"]')).toHaveValue(url1)
  await page.fill('[data-testid="form-url"]', url2)
  await page.click('[data-testid="form-next"]')
  await page.click('[data-testid="form-submit"]')
  await expect(page.locator('[data-testid="toast"]')).toContainText('update successfully')

  got = await api(page, 'GET', `/api/repositories/${key}`)
  expect(JSON.parse(got.text).configuration.url).toBe(url2)
  expect(got.text).not.toContain(secret) // 改前后 password 均不回显

  await api(page, 'DELETE', `/api/repositories/${key}`)
  expect(errors).toEqual([])
})

test('W34: audit page actor filter matches REST result set', async ({ page }) => {
  const errors = watchServerErrors(page)
  const user = uniq('w34probe')
  const pw = 'w34-probe-pw'
  await page.goto('/binflow/ui/')
  await login(page)
  expect(
    (
      await api(page, 'PUT', `/api/security/users/${user}`, {
        name: user,
        email: `${user}@example.com`,
        password: pw,
        admin: false,
        groups: [],
      })
    ).status,
  ).toBeLessThan(300)

  // 该 actor 的独立事件源：3 次 UI 登录（login.success ×3，事件即落）
  for (let i = 0; i < 3; i++) {
    const ctx = await page.context().browser()!.newContext()
    const p = await ctx.newPage()
    await p.goto('/binflow/ui/')
    await login(p, user, pw)
    await ctx.close()
  }

  // REST 基线
  const rest = await api(page, 'GET', `/api/v1/audit?actor=${user}&limit=1000`)
  expect(rest.status).toBe(200)
  const events = JSON.parse(rest.text).events as { actor: string; action: string }[]
  expect(events.length).toBeGreaterThanOrEqual(3)
  expect(events.every((e) => e.actor === user)).toBe(true)

  // UI：actor 过滤（防抖）→ 行集与 REST 一致
  await page.goto('/binflow/ui/audit')
  await expect(page.locator('[data-testid="audit-table"] tbody tr').first()).toBeVisible()
  await page.fill('[data-testid="audit-filter-actor"]', user)
  await page.waitForTimeout(700)
  const rows = page.locator('[data-testid="audit-table"] tbody tr')
  await expect(rows).toHaveCount(events.length, { timeout: 10_000 })
  await expect(rows.first()).toContainText(user)
  expect(errors).toEqual([])
})

test('W23b UI leg: audit page DOM carries no token plaintext / password literal', async ({ page }) => {
  const errors = watchServerErrors(page)
  const wrongPw = 't104-wrong-pw-literal'
  await page.goto('/binflow/ui/')
  await login(page)

  // 秘密源 1：错口令登录（auth.failed 落审计；detail 不得携带尝试值）
  const bad = await page.evaluate(
    async (pw) => {
      const res = await fetch('/binflow/api/v1/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: 'admin', password: pw }),
      })
      return res.status
    },
    wrongPw,
  )
  expect(bad).toBe(401)
  // 秘密源 2：真签 token。注意（T-104 实测登记）：token 签发**不落审计**
  // （PRD FR-29 词表把 token.issue 列为 M1 既有——实现无此路径，缺陷另报）；
  // 此处只断言 token 明文不进审计页 DOM（含未过滤首屏全量行）
  const tok = await api(page, 'POST', '/api/security/token', { grant_type: 'client_credentials' })
  expect(tok.status).toBeLessThan(300)
  const token = JSON.parse(tok.text).access_token as string
  expect(token.length).toBeGreaterThan(20)

  // UI：审计页按 action 收窄到 auth.failed（行确实被渲染，脱敏断言不空转；
  // 词汇对齐 T-187：服务端自 M6 起认证失败只发 auth.failed 一词）
  await page.goto('/binflow/ui/audit')
  await page.selectOption('[data-testid="audit-filter-action"]', 'auth.failed')
  await page.waitForTimeout(700)
  await expect(page.locator('[data-testid="audit-table"] tbody tr').first()).toContainText('auth.failed')
  const loginDom = await page.locator('[data-testid="audit-page"]').innerText()
  expect(loginDom).not.toContain(wrongPw)

  // 未过滤首屏（最近 100 行）：token 明文与错误口令字面量均零命中
  await page.selectOption('[data-testid="audit-filter-action"]', '')
  await page.waitForTimeout(700)
  await expect(page.locator('[data-testid="audit-table"] tbody tr').first()).toBeVisible()
  const fullDom = await page.locator('[data-testid="audit-page"]').innerText()
  expect(fullDom).not.toContain(token)
  expect(fullDom).not.toContain(wrongPw)
  expect(errors).toEqual([])
})
