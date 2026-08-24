import { expect, test } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { sessionApi } from '../m8/support/seed'
import { m9Client, m9Targets, repoKeys, seedM9, userPassword } from './support/seed'

// T-259 (FR-79 / ADR-0030 E6 + PRD N07): the m-holder console reachability —
// the permissions pages consume `GET /api/v1/permissions?filter=manage` (T-254)
// and the T-241 L2 boundary card RETIRES for manage holders (kept for plain
// users). Legs, all on the seed-m9 fixtures (u9: manage on t-in via
// m9-r00/m9-r01; t-out: u9 read-only on m9-r02/m9-r03 — outside the coverage):
//
//   u9 list     the fetch path itself: cold load issues exactly ONE
//               ?filter=manage request (never the frozen no-filter 403 form),
//               the table is exactly [t-in] (t-out hidden — server-side info
//               isolation, zero leakage), the manage-holder note renders, the
//               create entry stays L4-admin-only. axe.
//   u9 editor   full-field hydration from the filtered list (name locked, repo
//               chips, patterns, manage matrix cell), name-entry surfaces (the
//               catalogs are 403 for u9: manual repo entry in the dialog +
//               manual principal entry), the coverage boundary AS the editor
//               lives it — adding out-of-coverage m9-r02 saves into the verbatim
//               403 'manage coverage' form-error, the recovered in-coverage edit
//               saves 201 (family 4), and the seed body is converged back.
//   u9 delete   a coverage-in target created by u9's own POST (family 4 wire
//               arm) is deleted through the editor danger zone; the wire replay
//               of DELETE t-out stays 403.
//   u8 user     the L2 convergence RETAINED for a plain user: filter=manage is
//               403 (empty coverage — same bytes as no-filter by design), the
//               page renders the friendly no-coverage empty state, the table
//               never mounts, and the editor deep link carries the boundary
//               note. axe.
//
// Serial on purpose: leg 1 asserts the list is EXACTLY [t-in], which holds only
// while no other leg has a coverage-in target alive (the delete leg creates and
// removes t259-own); legs converge their own fixtures (leftover delete + seed
// body re-POST) so a failed prior run cannot poison a re-run. Request counting
// is the in-file page.on('request') tracker (T-258 precedent — m9 specs stay
// area-isolated until a shared helper ticket says otherwise).

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test.beforeAll(async () => {
  // Full seed-m9 slice: the four coverage targets ride the 50-repo fixture
  // (idempotent PUTs + create-if-absent targets — convergent on a shared
  // instance).
  await seedM9(m9Client())
})

/** /binflow/api/** request log (T-258 form): what the PAGE rides, ambient
 * shell bootstrap included, so unknown new requests fail the allowlists. */
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

/** Direct login through the real login page (u9/u8 are seed identities, not
 * m8 role fixtures). Same anchors, same shell-wait contract as loginAs. */
async function loginDirect(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/binflow/ui/')
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await page.fill('[data-testid="login-username"]', username)
  await page.fill('[data-testid="login-password"]', password)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

test.describe.serial('m-holder permissions reachability (T-259)', () => {
  const OWN = 't259-own' // the coverage-in target the delete leg rides

  test('u9 list: exactly [t-in] via ONE filter=manage request; t-out hidden; L4 create absent; axe', async ({
    page,
  }, testInfo: TestInfo) => {
    test.setTimeout(90_000)
    await loginDirect(page, 'u9', userPassword('u9'))
    // converge a leftover OWN from a failed prior run (404 when absent — both fine)
    await sessionApi(page, 'DELETE', `/api/v1/permissions/${OWN}`)

    // Cold load under the counter: navigate first, attach, reload — the
    // counter sees exactly one cold first-screen.
    await page.goto('/binflow/ui/admin/security/permissions')
    await expect(page.locator('[data-testid="perms-table"]')).toBeVisible()
    const t = trackApi(page)
    await page.reload()
    await expect(page.locator('[data-testid="perms-table"]')).toBeVisible()

    // The fetch path (E6): exactly one ?filter=manage; the frozen no-filter
    // form (the 403 the page used to ride) appears nowhere.
    expect(t.matching(/GET \/api\/v1\/permissions\?filter=manage$/)).toHaveLength(1)
    expect(t.matching(/GET \/api\/v1\/permissions$/), 'the no-filter form must not be ridden').toEqual([])
    const ambient = t.calls.filter((c) => !/\/api\/v1\/permissions\?filter=manage$/.test(c))
    expect([...ambient].sort(), `ambient shell bootstrap pinned (got: ${ambient.join(' | ')})`).toEqual([
      'GET /api/system/version',
      'GET /api/v1/session',
    ])

    // The covered set as the server computes it: exactly [t-in]. t-out (u9's
    // read-only grant) and the partial/unrelated targets appear nowhere.
    const names = await page.locator('[data-testid="perms-table"] tbody .row-link').allTextContents()
    expect(names).toEqual(['t-in'])
    await expect(page.locator('[data-testid="perm-row-t-out"]')).toHaveCount(0)
    await expect(page.locator('[data-testid="perms-page"]')).not.toContainText('t-out')

    // m-holder posture: the manage note renders, the count line is
    // coverage-scoped, the create entry stays admin-only (L4).
    await expect(page.locator('[data-testid="perms-manage-note"]')).toBeVisible()
    await expect(page.locator('[data-testid="perms-count"]')).toHaveText('管理范围内的权限 target： 1')
    await expect(page.locator('[data-testid="perms-create"]')).toHaveCount(0)

    // Wire contrast (frozen branch): u9's no-filter GET stays 403 — the page
    // no longer rides it, but the closed set itself is unchanged (N07).
    const nf = await sessionApi(page, 'GET', '/api/v1/permissions')
    expect(nf.status).toBe(403)

    await expectA11yClean(page, testInfo, { include: '[data-testid="perms-page"]' })
  })

  test('u9 editor: full-field hydration, name-entry surfaces, coverage-out save 403 verbatim, in-coverage save 201', async ({
    page,
  }) => {
    test.setTimeout(90_000)
    await loginDirect(page, 'u9', userPassword('u9'))
    const [r00, r01, r02] = repoKeys()

    // Enter through the list item (the m-holder console path)
    await page.goto('/binflow/ui/admin/security/permissions')
    await page.click('[data-testid="perm-row-t-in"] a.row-link')
    await expect(page.locator('[data-testid="perm-editor-page"]')).toBeVisible()

    // Full-field hydration from the filtered list (E6 entry = full list shape)
    await expect(page.locator('[data-testid="perm-form-name"]')).toHaveValue('t-in')
    await expect(page.locator('[data-testid="perm-form-name"]')).toBeDisabled()
    await expect(page.locator('[data-testid="perm-repos"]')).toContainText(r00)
    await expect(page.locator('[data-testid="perm-repos"]')).toContainText(r01)
    await expect(page.locator('[data-testid="perm-patterns-summary"]')).toContainText('**')
    await expect(page.locator('[data-testid="perm-matrix-cell-user-u9-manage"]')).toBeChecked()
    await expect(page.locator('[data-testid="perm-editor-manage-note"]')).toBeVisible()

    // —— Coverage boundary AS the editor lives it: add t-out's repo (outside
    // the coverage) via manual entry (the catalog is 403 for u9) ——
    await page.click('[data-testid="perm-repo-add"]')
    await expect(page.locator('[data-testid="perm-res-dialog"]')).toBeVisible()
    // the name-entry dialog face: catalog unavailable, manual entry present
    await expect(page.locator('[data-testid="perm-repo-entry-input"]')).toBeVisible()
    await page.fill('[data-testid="perm-repo-entry-input"]', r02)
    await page.click('[data-testid="perm-repo-entry-add"]')
    await expect(page.locator('[data-testid="perm-res-repos"] [data-testid="transfer-selected"]')).toContainText(r02)
    await page.click('[data-testid="perm-res-next"]')
    await page.click('[data-testid="perm-res-ok"]')
    await expect(page.locator('[data-testid="perm-repos"]')).toContainText(r02)

    // Save -> server is the arbiter: 403 with the family-4 verbatim text
    await page.click('[data-testid="perm-save"]')
    await page.click('[data-testid="confirm-accept"]')
    await expect(page.locator('[data-testid="form-error"]')).toBeVisible({ timeout: 8000 })
    await expect(page.locator('[data-testid="form-error"]')).toContainText('403')
    await expect(page.locator('[data-testid="form-error"]')).toContainText('manage coverage')
    // the failed save left the target untouched (wire reconciliation)
    const after403 = await sessionApi(page, 'GET', '/api/v1/permissions?filter=manage')
    const tin403 = (after403.json as { name: string; repos: string[] }[]).find((x) => x.name === 't-in')
    expect(tin403?.repos).toEqual([r00, r01])

    // —— Recovery + the name-entry principal face: drop the out-of-coverage
    // repo, add a principal by manual name entry (user catalog 403), save 201 ——
    await page.click(`[data-testid="perm-repo-remove-${r02}"]`)
    await expect(page.locator('[data-testid="perm-repos"]')).not.toContainText(r02)
    await expect(page.locator('[data-testid="perm-add-user"]')).toHaveRole('textbox')
    await page.fill('[data-testid="perm-add-user"]', 'u10')
    await page.getByRole('button', { name: '添加用户' }).click()
    await page.check('[data-testid="perm-matrix-cell-user-u10-read"]')
    await page.check('[data-testid="perm-matrix-cell-user-u9-write"]')
    await page.click('[data-testid="perm-save"]')
    const diff = page.locator('[data-testid="perm-diff"]')
    await expect(diff).toContainText('授予用户 u9 write')
    await expect(diff).toContainText('授予用户 u10 read')
    await page.click('[data-testid="confirm-accept"]')
    await expect(
      page.locator('[data-testid="toast"]').filter({ hasText: 'permission target t-in 已保存' }),
    ).toBeVisible({ timeout: 8000 })
    await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/permissions$/)

    // Wire reconciliation: the 201 landed the two grants (family-4 arm)
    const got = await sessionApi(page, 'GET', '/api/v1/permissions?filter=manage')
    const tin = (got.json as { name: string; repos: string[]; principals: { users: Record<string, string[]> } }[]).find(
      (x) => x.name === 't-in',
    )
    expect(tin?.repos).toEqual([r00, r01])
    expect([...(tin?.principals.users.u9 ?? [])].sort()).toEqual(['manage', 'write'])
    expect(tin?.principals.users.u10).toEqual(['read'])

    // Converge t-in back to the seed body (u9's own POST — coverage-in 2xx)
    const seedBody = m9Targets().find((x) => x.name === 't-in')!.body
    const conv = await sessionApi(page, 'POST', '/api/v1/permissions', seedBody)
    expect(conv.status).toBe(201)
  })

  test('u9 delete: coverage-in target removed through the danger zone; t-out DELETE replay stays 403', async ({
    page,
  }) => {
    test.setTimeout(90_000)
    await loginDirect(page, 'u9', userPassword('u9'))
    // converge t-in first (a failed prior editor leg may have left grants)
    const seedBody = m9Targets().find((x) => x.name === 't-in')!.body
    expect((await sessionApi(page, 'POST', '/api/v1/permissions', seedBody)).status).toBe(201)

    // Fixture via u9's own family-4 wire arm: repos inside the coverage
    const [r00] = repoKeys()
    const created = await sessionApi(page, 'POST', '/api/v1/permissions', {
      name: OWN,
      repos: [r00],
      includePatterns: ['**'],
      excludePatterns: [],
      principals: { users: {}, groups: {} },
    })
    expect(created.status).toBe(201)

    // The list now carries it (fully covered by construction)
    await page.goto('/binflow/ui/admin/security/permissions')
    await expect(page.locator(`[data-testid="perm-row-${OWN}"]`)).toBeVisible()
    await page.click(`[data-testid="perm-row-${OWN}"] a.row-link`)
    await expect(page.locator('[data-testid="perm-danger-zone"]')).toBeVisible()
    await page.click('[data-testid="perm-delete-button"]')
    await page.click('[data-testid="confirm-accept"]')
    await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${OWN} 已删除` })).toBeVisible({
      timeout: 8000,
    })
    await expect(page).toHaveURL(/\/binflow\/ui\/admin\/security\/permissions$/)
    await expect(page.locator(`[data-testid="perm-row-${OWN}"]`)).toHaveCount(0)

    // The wire boundary the editor cannot even reach: t-out DELETE stays 403
    const out = await sessionApi(page, 'DELETE', '/api/v1/permissions/t-out')
    expect(out.status).toBe(403)
    expect(out.text).toContain('manage coverage')
  })

  test('u8 (no manage): L2 convergence retained — friendly no-coverage empty state, zero table, editor boundary note; axe', async ({
    page,
  }, testInfo: TestInfo) => {
    test.setTimeout(90_000)
    await loginDirect(page, 'u8', userPassword('u8'))

    // The list: 403 (empty coverage — byte-identical to no-filter by design)
    // converges to the friendly no-coverage empty state; the table never mounts
    await page.goto('/binflow/ui/admin/security/permissions')
    await expect(page.locator('[data-testid="perms-page"] [data-testid="empty-state"]')).toContainText(
      '无管理范围内的权限目标',
    )
    await expect(page.locator('[data-testid="perms-page"]')).toContainText('manage 覆盖集')
    await expect(page.locator('[data-testid="perms-table"]')).toHaveCount(0)

    // Cold screen pinned: exactly the one denied filter=manage read + the
    // ambient shell pair — the page data plane rides nothing else
    const t = trackApi(page)
    await page.reload()
    await expect(page.locator('[data-testid="perms-page"] [data-testid="empty-state"]')).toBeVisible()
    expect(t.matching(/GET \/api\/v1\/permissions\?filter=manage$/)).toHaveLength(1)
    expect(t.matching(/GET \/api\/v1\/permissions$/)).toEqual([])
    const ambient = t.calls.filter((c) => !/\/api\/v1\/permissions\?filter=manage$/.test(c))
    expect([...ambient].sort(), `ambient pinned (got: ${ambient.join(' | ')})`).toEqual([
      'GET /api/system/version',
      'GET /api/v1/session',
    ])

    // axe on the friendly no-coverage empty state (before leaving the route)
    await expectA11yClean(page, testInfo, { include: '[data-testid="perms-page"]' })

    // The editor deep link keeps the boundary note (coverage-empty arm)
    await page.goto('/binflow/ui/admin/security/permissions/t-in')
    await expect(page.locator('[data-testid="perm-editor-page"] [data-testid="empty-state"]')).toBeVisible()
    await expect(page.locator('[data-testid="perm-editor-page"]')).toContainText('manage 覆盖集')
  })
})
