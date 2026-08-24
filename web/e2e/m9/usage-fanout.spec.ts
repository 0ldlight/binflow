import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { sessionApi } from '../m8/support/seed'
import { m9Client, repoKeys, seedM9, usageSeedBody, userPassword } from './support/seed'

// T-258 (PRD FR-79 fanout, N-sequence "~171 -> <= 3"): the repos page "used"
// column hydrates from ONE batch request — GET /api/v1/storage/usage?include=
// counts (E1, T-253) — retiring the per-repo usage/{repo} fanout. Legs:
//
//   admin      hard whole-page cap: a cold full-page load of the repos route
//              issues <= 3 /binflow/api/** requests (whoami probe + repo list
//              + the ONE usage batch), per-repo usage form appears nowhere,
//              sort/filter (pure client state) re-fire nothing, every seeded
//              row hydrates non-"—" and reconciles against the API (sampled),
//              axe on the hydrated table.
//   readonly   readonly_admin sees the same full-set hydration (E1: admin and
//              readonly_admin both get the unfiltered 50) under the same cap.
//   user       u8 (read on 10 of the 50): the repos page itself is the L2
//              no-permission card (GET /api/repositories is CapRepoRead) and
//              the hydration fires NO request (nothing to hydrate); the
//              visible-set difference is asserted as-is at the wire the UI
//              consumes — u8's batch returns exactly its readable ten.
//
// Seeding: seedM9 in beforeAll — idempotent (PUT-replace + identical bytes
// re-PUT = delta 0), so this spec is self-sufficient on a fresh instance and
// convergent on a shared one. Request counting uses the in-page network layer
// (page.on('request')) over the /binflow/api/ prefix — the same counter form
// the parallel T-257 leg rides; kept in-file (m9 specs stay area-isolated
// until a shared helper ticket says otherwise).

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test.beforeAll(async () => {
  await seedM9(m9Client())
})

/** Whole-page management-plane request log: every /binflow/api/** request the
 * PAGE issues (static assets and content-plane paths outside /api/ excluded). */
function trackApi(page: Page) {
  const calls: string[] = []
  page.on('request', (r) => {
    const url = r.url()
    const i = url.indexOf('/binflow/api/')
    if (i !== -1) calls.push(`${r.method()} ${url.slice(i + '/binflow'.length)}`)
  })
  return {
    calls,
    matching: (re: RegExp) => calls.filter((c) => re.test(c)),
  }
}

/** 1024-base byte formatter — oracle twin of src/lib/format.ts formatBytes
 * (e2e cannot import src/; rules kept in lockstep, see repositories.css era). */
function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const digits = v >= 100 || i === 0 ? 0 : 1
  return `${v.toFixed(digits)} ${units[i]}`
}

/** Direct login (the m8 loginAs covers its three fixtures only; u8 is an m9
 * seed identity). Same anchors, same shell-wait contract. */
async function loginDirect(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/binflow/ui/')
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** Cold whole-page load of the repos route and its request log: navigates
 * first (lets the login landing page's own fetches finish), THEN attaches the
 * counter and reloads — the counter sees exactly one cold first-screen. */
async function coldLoadRepos(page: Page, tab = 'local') {
  await page.goto(`/binflow/ui/admin/repositories/${tab}`)
  await expect(page.locator('[data-testid="repos-table"]')).toBeVisible()
  const t = trackApi(page)
  await page.reload()
  await expect(page.locator('[data-testid="repos-table"]')).toBeVisible()
  return t
}

/** All hydrated used-cells on the current page ({key, text} for m9 rows). */
async function usageCells(page: Page): Promise<{ key: string; text: string }[]> {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLElement>('[data-testid^="repos-usage-m9-r"]')).map((el) => ({
      key: (el.dataset.testid ?? '').slice('repos-usage-'.length),
      text: (el.textContent ?? '').trim(),
    })),
  )
}

/** The fanout contract itself (PRD NFR-P39 "repos 列表页首屏 XHR ≤ 3" — the
 * page's DATA plane): repo list 1 + usage batch 1, the retired per-repo form
 * absent, ceiling 3. The ambient shell bootstrap (whoami probe + version,
 * both module-cached and carried by EVERY route's first screen) is outside
 * the budget but pinned here as an exact allowlist — no unknown /api/**
 * request may sneak onto the page either. */
function expectSingleShotFanout(t: { calls: string[]; matching: (re: RegExp) => string[] }) {
  const fanout = t.matching(/\/api\/(repositories|v1\/storage\/usage)/)
  expect(
    t.matching(/GET \/api\/repositories\?type=local$/),
    'repo list: exactly one request',
  ).toHaveLength(1)
  expect(
    t.matching(/GET \/api\/v1\/storage\/usage\?include=counts$/),
    'usage batch: exactly one request',
  ).toHaveLength(1)
  expect(
    t.matching(/\/api\/v1\/storage\/usage\//),
    'retired per-repo usage/{repo} form must not appear',
  ).toHaveLength(0)
  expect(
    fanout.length,
    `page data-plane XHR cap <= 3 (got: ${fanout.join(' | ')})`,
  ).toBeLessThanOrEqual(3)
  const ambient = t.calls.filter((c) => !/\/api\/(repositories|v1\/storage\/usage)/.test(c))
  expect(
    [...ambient].sort(),
    `ambient shell bootstrap pinned to the known pair (got: ${ambient.join(' | ')})`,
  ).toEqual(['GET /api/system/version', 'GET /api/v1/session'])
}

// ---- admin: single-shot hydration + hard cap + zero refetch on sort/filter ----

test('admin: used column hydrates from ONE usage batch — whole-page cap <= 3, sort/filter re-fire nothing, values reconcile, axe clean', async ({
  page,
}, testInfo: TestInfo) => {
  test.setTimeout(90_000)
  await loginAs(page, 'admin')
  const keys = repoKeys()

  // Cold first-screen: wait for hydration on the first seeded row (the batch
  // drives every cell, so one settled cell means the batch landed).
  const t = await coldLoadRepos(page)
  await expect(page.locator(`[data-testid="repos-usage-${keys[0]}"]`)).toHaveText(
    fmtBytes(usageSeedBody(keys[0]).length),
  )
  expectSingleShotFanout(t)

  // Every seeded row hydrated non-"—" (50 rows on the seed fixture).
  const cells = await usageCells(page)
  expect(cells.length, 'all 50 seeded rows carry a used-cell anchor').toBeGreaterThanOrEqual(50)
  expect(
    cells.filter((c) => c.text === '—'),
    'no row left on the loading/error placeholder',
  ).toEqual([])

  // Sort + filter are pure client state: zero additional requests.
  const before = t.calls.length
  await page.click('[data-testid="repos-sort-key"]') // none -> asc
  await expect(page.locator('[data-testid^="repos-row-m9-r"]').first()).toHaveAttribute(
    'data-testid',
    `repos-row-${keys[0]}`,
  )
  await page.click('[data-testid="repos-sort-key"]') // asc -> desc
  await expect(page.locator('[data-testid^="repos-row-m9-r"]').first()).toHaveAttribute(
    'data-testid',
    `repos-row-${keys[49]}`,
  )
  await page.fill('[data-testid="repos-filter-key"]', 'm9-r4')
  await expect(page.locator('[data-testid^="repos-row-m9-r"]')).toHaveCount(10)
  await page.fill('[data-testid="repos-filter-key"]', '')
  await expect(page.locator('[data-testid^="repos-row-m9-r"]')).toHaveCount(50)
  await page.waitForTimeout(300)
  expect(t.calls.length, `sort/filter must not re-fire (before: ${before}, after: ${t.calls.length})`).toBe(before)

  // Reconciliation (sampled): page cell text vs the API's own batch body —
  // first / middle / last seeded rows.
  const api = await sessionApi(page, 'GET', '/api/v1/storage/usage')
  expect(api.status).toBe(200)
  const byKey = new Map(
    (api.json as { repo: string; usedBytes: number }[]).map((r) => [r.repo, r]),
  )
  for (const key of [keys[0], keys[24], keys[49]]) {
    const row = byKey.get(key)
    expect(row, `${key} present in the API batch body`).toBeTruthy()
    expect(row?.usedBytes, `${key} usedBytes = deterministic seed body length`).toBe(
      usageSeedBody(key).length,
    )
    await expect(page.locator(`[data-testid="repos-usage-${key}"]`)).toHaveText(
      fmtBytes(row?.usedBytes ?? -1),
    )
  }

  await expectA11yClean(page, testInfo, { include: '[data-testid="repos-page"]' })
})

// ---- readonly_admin: same full-set hydration under the same cap ----------------

test('readonly_admin: full 50-row hydration from one batch (E1 unfiltered view for readonly), cap <= 3', async ({
  page,
}) => {
  test.setTimeout(90_000)
  await loginAs(page, 'readonly_admin')

  const t = await coldLoadRepos(page)
  await expect(page.locator('[data-testid="repos-readonly-note"]')).toBeVisible()
  const keys = repoKeys()
  await expect(page.locator(`[data-testid="repos-usage-${keys[10]}"]`)).toHaveText(
    fmtBytes(usageSeedBody(keys[10]).length),
  )
  expectSingleShotFanout(t)

  // Visible set as-is at the wire the UI consumes: readonly gets the full 50
  // (E1: admin/readonly_admin unfiltered) — the contrast arm of the u8 leg.
  const api = await sessionApi(page, 'GET', '/api/v1/storage/usage')
  expect(api.status).toBe(200)
  const got = (api.json as { repo: string }[]).map((r) => r.repo)
  expect(keys.filter((k) => !got.includes(k)), 'every seeded repo in readonly batch').toEqual([])
})

// ---- user (u8): L2 no-permission card + zero hydration requests + subset wire ----

test('user u8: repos page is the L2 no-permission card, hydration fires nothing; visible set = exactly its readable ten', async ({
  page,
}, testInfo: TestInfo) => {
  test.setTimeout(90_000)
  await loginDirect(page, 'u8', userPassword('u8'))

  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('无权限查看仓库列表')
  await expect(page.locator('[data-testid="repos-table"]')).toHaveCount(0)

  // Cold reload under the counter: the gated hydration must stay silent (the
  // failed list leaves nothing to hydrate — no orphan usage request), and the
  // whole cold screen is pinned: ambient pair + the single denied list call.
  const t = trackApi(page)
  await page.reload()
  await expect(page.locator('[data-testid="empty-state"]')).toContainText('无权限查看仓库列表')
  expect(
    t.matching(/\/api\/v1\/storage\/usage/),
    'no usage request may fire when the list itself is forbidden',
  ).toEqual([])
  expect([...t.calls].sort(), `cold screen pinned (got: ${t.calls.join(' | ')})`).toEqual([
    'GET /api/repositories?type=local',
    'GET /api/system/version',
    'GET /api/v1/session',
  ])

  // Visible-set difference, as-is: u8's own batch (the wire a user-visible
  // repos view would consume) answers 200 with exactly the granted ten.
  const api = await sessionApi(page, 'GET', '/api/v1/storage/usage')
  expect(api.status).toBe(200)
  const rows = api.json as { repo: string; usedBytes: number }[]
  expect(rows.map((r) => r.repo)).toEqual(repoKeys().slice(0, 10))
  for (const r of rows) {
    expect(r.usedBytes).toBe(usageSeedBody(r.repo).length)
  }

  await expectA11yClean(page, testInfo, { include: '[data-testid="repos-page"]' })
})

// ---- four states: loading placeholder / error grey + tooltip + retry ----------
//
// The batch endpoint is route-mocked through its three behaviors (hang ->
// fail -> pass-through): loading pins the "—" placeholder while the row data
// is already on screen, the error pins the grey retry BUTTON (keyboard-
// reachable, tooltip carries the server message), and the retry click both
// recovers the value and proves the row-navigation isolation (clicking retry
// must NOT open the repo detail row).

test('four states: batch hang -> placeholder —, forced 500 -> grey retry button with server message, retry recovers without row navigation', async ({
  page,
}, testInfo: TestInfo) => {
  test.setTimeout(90_000)
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator('[data-testid="repos-table"]')).toBeVisible()

  const keys = repoKeys()
  const cell = page.locator(`[data-testid="repos-usage-${keys[0]}"]`)
  const failMessage = 't258 forced usage failure'
  let phase: 'hang' | 'fail' | 'pass' = 'hang'
  let release: (() => void) | undefined
  await page.route('**/api/v1/storage/usage*', async (route) => {
    if (phase === 'hang') {
      await new Promise<void>((r) => {
        release = r
      })
      release = undefined
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: failMessage }] }) })
      return
    }
    if (phase === 'fail') {
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ errors: [{ message: failMessage }] }) })
      return
    }
    await route.continue()
  })

  // Loading: the hung batch leaves the column on the "—" placeholder while
  // the table itself is fully rendered.
  phase = 'hang'
  await page.reload()
  await expect(page.locator('[data-testid="repos-table"]')).toBeVisible()
  await expect(cell).toHaveText('—')

  // Error: release the hang as a 500 -> grey retry button, tooltip carries
  // the server message verbatim (the E-01 envelope's extracted message).
  phase = 'fail'
  release?.()
  await expect(cell).toHaveRole('button')
  await expect(cell).toHaveText('—')
  await expect(cell).toHaveAttribute('title', new RegExp(failMessage))
  await expectA11yClean(page, testInfo, { include: '[data-testid="repos-page"]' })

  // Retry: click passes through to the real server -> value recovers; the
  // click must not bubble into the row's navigation (URL unchanged).
  phase = 'pass'
  await cell.click()
  await expect(cell).toHaveText(fmtBytes(usageSeedBody(keys[0]).length))
  expect(page.url()).toMatch(/\/binflow\/ui\/admin\/repositories\/local$/)

  await page.unroute('**/api/v1/storage/usage*')
})
