import { expect, test } from '@playwright/test'
import { expectA11yClean } from './support/a11y'
import { expectCopied, grantClipboard } from './support/clipboard'
import { loginAs } from './support/roles'
import { countTreeNodes, m8Client, seedAll, seedRepos } from './support/seed'
import { measureFirstInteractive, measureTreeExpand, recordTiming } from './support/timing'

// T-232 helper self-tests: every M8 base helper proves itself in one minimal
// leg against the REAL local-filestore stack (binary-freshness discipline:
// build inputs newer than bin/ force `make build` first — the
// scripts/m7-resume-probe.sh guard). Legs are independent (unique entities
// or idempotent seeds) so fullyParallel stays safe.

const PERM_REPO = 'm8-perf-local'

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

// ---- seedRepos + seedTree: the >=10,000-node REST-materialized tree ------

test('seed: repos + tree materialize >=10,000 nodes over the real REST plane', async () => {
  test.setTimeout(300_000)
  const client = m8Client()
  const r = await seedAll(client, { repoKey: PERM_REPO })

  // All three planes answered; users/grant/repo converged idempotently.
  expect(r.users.map((u) => u.role).sort()).toEqual(['readonly_admin', 'user'])
  expect(['created', 'present']).toContain(r.grant)
  expect(r.repos.map((x) => x.key)).toContain(PERM_REPO)

  // THE number: deep-list verification >= 10,000 nodes under perf/.
  expect(r.tree).not.toBeNull()
  expect(r.tree!.verified).toBeGreaterThanOrEqual(10_000)
  // Deterministic plan: a clean repo converges on the exact node count.
  expect(r.tree!.verified).toBe(r.tree!.planned)

  // Spot checks through the management read plane (not just the count):
  // one wide file, one deep chain seed, both addressable by path.
  const wide = await client.request('GET', `/binflow/api/storage/${PERM_REPO}/perf/w00/f000.txt`)
  expect(wide.status).toBe(200)
  const deep = await client.request(
    'GET',
    `/binflow/api/storage/${PERM_REPO}/perf/c000/${Array.from({ length: 75 }, (_, i) => `d${String(i).padStart(2, '0')}`).join('/')}/seed.txt`,
  )
  expect(deep.status).toBe(200)

  // countTreeNodes is independently callable (the re-verify path for later
  // legs that only need the number).
  expect(await countTreeNodes(client, PERM_REPO, 'perf')).toBe(r.tree!.verified)
})

// ---- loginAs: three roles ----------------------------------------------

for (const role of ['admin', 'user', 'readonly_admin'] as const) {
  test(`loginAs(${role}) reaches the shell with the right session markers`, async ({ page }) => {
    const s = await loginAs(page, role)
    expect(s.username).toBeTruthy()
    await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
    await expect(page.locator('[data-testid="session-user"]')).toHaveText(s.username)
    if (role === 'readonly_admin') {
      await expect(page.locator('[data-testid="session-readonly-badge"]')).toBeVisible()
    } else {
      await expect(page.locator('[data-testid="session-readonly-badge"]')).toHaveCount(0)
    }
  })
}

// ---- clipboard ----------------------------------------------------------

test('clipboard: UI copy lands the FULL value on the OS clipboard', async ({ page }) => {
  test.skip(!(await grantClipboard(page)), 'clipboard legs are chromium-only (the supported matrix)')

  const key = uniq('m8clip')
  await seedRepos(m8Client(), [{ key }])
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toBeVisible()

  await expectCopied(page, page.locator(`[data-testid="repos-row-${key}"] button[aria-label="复制 仓库 key ${key}"]`), key)
})

// ---- axe-core -----------------------------------------------------------

test('axe: login page scans clean at serious/critical impact', async ({ page }, testInfo) => {
  await page.goto('/binflow/ui/')
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="login-page"]' })
})

// ---- timing -------------------------------------------------------------

test('timing: first-interactive + tree-expand collect numbers on a seeded level', async ({ page }, testInfo) => {
  const key = uniq('m8time')
  const client = m8Client()
  await seedRepos(client, [{ key }])
  // A small deterministic level: perf/w00..w05, three files each (24 nodes).
  for (let w = 0; w < 6; w++) {
    for (let f = 0; f < 3; f++) {
      await client.request('PUT', `/binflow/${key}/perf/w${String(w).padStart(2, '0')}/f${f}.txt`, {
        raw: true,
        headers: { 'Content-Type': 'application/octet-stream' },
        body: `t ${w} ${f}\n`,
      })
    }
  }

  const first = await measureFirstInteractive(page, '/binflow/ui/', '[data-testid="login-page"]')
  await recordTiming(testInfo, 'first-interactive', first)

  await loginAs(page, 'admin')
  await page.goto(`/binflow/ui/artifacts/${key}`)
  const root = page.locator('[data-testid="tree-node-perf"]')
  await expect(root).toBeVisible()
  const expand = await measureTreeExpand(page, root, `[data-testid^="tree-node-perf/"]`)
  await recordTiming(testInfo, 'tree-expand', expand)

  // Numbers are recorded, not gated (ADR-0029: interaction assertions only);
  // but the measurement itself must have seen the level: 6 child dirs.
  expect(expand.childRows).toBe(6)
  expect(expand.expandMs).toBeGreaterThan(0)
  expect(first.readyMs).toBeGreaterThan(0)
})
