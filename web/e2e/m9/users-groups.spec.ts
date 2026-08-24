import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { sessionApi } from '../m8/support/seed'
import { ensureGroup, ensureM9User, groupNames, m9Client, userNames } from './support/seed'

// T-257 (FR-78 / PRD N01~N04): the users/groups pages consume the M9 widened
// echo — the N+1 retirement. The legs assert the NETWORK contract (request
// counting against the 20-user seed), not just the pixels:
//
//   N+1 retirement   users page = exactly 1 security GET (the E2 list); zero
//                    per-user GET /security/users/{name} fanout (T-237's
//                    listUsers + 20x getUser is dead). Groups page = 3 data
//                    GETs (groups list + E2 users + permission targets),
//                    still zero fanout; the group editor adds exactly one
//                    E5 ?includeUsers=true read.
//   Status truth     E2 enabled drives the list Status column; E3 drives the
//                    editor checkbox (the knownEnabled local-echo hack and
//                    its "no echo" hint are gone).
//   Delete chain     E4 full leg: typed-name strong confirm + irreversible
//                    wording, cascade reflected in the E5 group view, 404
//                    verbatim on re-delete (deterministic, intentionally NOT
//                    idempotent — M9 review ruling), self/built-in guards
//                    (UI pre-disabled + server 400 verbatim), readonly 403.
//   Two-view single  E2-derived member count vs E5 userNames vs the editor's
//   source           selected column — the same user_groups rows through two
//                    server views (ADR-0030 K19).
//
// Provisioning rides seed-m9's ensure helpers (idempotent PUTs) so the spec
// needs only the users/groups slice, not the 50-repo fanout fixture. Note the
// replace arm PRESERVES a stored enabled=false when the body omits it, so the
// disable leg re-converges u7 explicitly instead of relying on re-seeding.

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** GET request tracker for /binflow/api/** minus app-shell chrome: the
 *  /v1/session bootstrap probe (auth re-check on every document load) and
 *  /system/version (useVersion's module-cached license line, fetched once per
 *  boot). Neither is page data — the N01 budget counts what the PAGE rides. */
function trackApiGets(page: Page): { data: () => string[]; userFanout: () => string[] } {
  const seen: string[] = []
  page.on('request', (r) => {
    if (r.method() !== 'GET') return
    const url = new URL(r.url())
    if (!url.pathname.startsWith('/binflow/api/')) return
    if (url.pathname === '/binflow/api/v1/session') return
    if (url.pathname === '/binflow/api/system/version') return
    seen.push(`${url.pathname}${url.search}`)
  })
  return {
    data: () => [...seen],
    // the retired N+1: per-user detail reads driven by list rendering
    userFanout: () => seen.filter((s) => /^\/binflow\/api\/security\/users\/[^/]+$/.test(s)),
  }
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test.beforeAll(async () => {
  // users/groups slice of seed-m9 (idempotent; groups before users —
  // membership rides the user body). 20 users / 10 groups, 2 per group.
  // Concurrency-safe under fullyParallel: every worker PUTs the identical
  // body, and the replace arm PRESERVES a stored enabled flag the body omits
  // (so a mid-run worker re-seed cannot un-flip N02's disable leg).
  const client = m9Client()
  for (const g of groupNames()) await ensureGroup(client, g)
  for (const u of userNames()) await ensureM9User(client, u)
})

test('N01: users page — single E2 request, zero per-user fanout, Status truth from the list echo', async ({
  page,
}, testInfo) => {
  await loginAs(page, 'admin')

  const api = trackApiGets(page)
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-row-u20"]')).toBeVisible() // all 20 seeded rows rendered

  // N+1 retirement: exactly ONE security GET (the E2 widened list) and ZERO
  // per-user detail reads. Budget <= 3 data GETs total (PRD N01 wording);
  // actual = 1.
  const data = api.data()
  expect(data.filter((p) => p === '/binflow/api/security/users')).toHaveLength(1)
  expect(api.userFanout()).toEqual([])
  expect(data.length).toBeLessThanOrEqual(3)

  // The widened echo renders WITHOUT the fanout: email / groups chips / role /
  // Status all from the single list payload (seed: u1 -> m9-g01).
  const row = page.locator('[data-testid="user-row-u1"]')
  await expect(row).toContainText('u1@m9-seed.invalid')
  await expect(row.locator('.badge', { hasText: 'm9-g01' })).toBeVisible()
  await expect(row.locator('.badge', { hasText: 'user' })).toBeVisible()
  await expect(page.locator('[data-testid="user-status-u1"]')).toHaveText('启用')

  // Status column sorts (asc = disabled first under the numeric projection)
  await page.click('[data-testid="users-sort-status"]')
  await expect(page.locator('[data-testid="users-sort-status"]')).toHaveAttribute('aria-sort', 'ascending')

  await expectA11yClean(page, testInfo, { include: '[data-testid="users-page"]' })
})

test('N01: groups page — members from the E2 projection, editor seeds from E5, two views agree', async ({ page }) => {
  await loginAs(page, 'admin')

  const api = trackApiGets(page)
  await page.goto('/binflow/ui/admin/security/groups')
  await expect(page.locator('[data-testid="groups-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-row-m9-g10"]')).toBeVisible()

  // 3 data GETs, user-count independent: groups list + E2 users + permission
  // targets. Zero per-user fanout (T-237's scanMembership did 1 + 20).
  const data = api.data()
  expect(data.filter((p) => p === '/binflow/api/security/groups')).toHaveLength(1)
  expect(data.filter((p) => p === '/binflow/api/security/users')).toHaveLength(1)
  expect(api.userFanout()).toEqual([])
  expect(data.length).toBeLessThanOrEqual(3)

  // E2 view: member count column + hover roster (seed: 2 users per group)
  await expect(page.locator('[data-testid="group-members-m9-g01"]')).toHaveText('2')
  await expect(page.locator('[data-testid="group-members-m9-g01"]')).toHaveAttribute('title', 'u1, u11')

  // E5 view: opening the editor adds EXACTLY one includeUsers read; the
  // selected column = server userNames, the user list is NOT re-fetched.
  await page.click('[data-testid="group-edit-m9-g01"]')
  await expect(page.locator('[data-testid="group-form-members"]')).toBeVisible()
  await expect(page.locator('[data-testid="group-form-members"] [data-testid="transfer-selected"]')).toContainText('u1')
  await expect(page.locator('[data-testid="group-form-members"] [data-testid="transfer-selected"]')).toContainText('u11')
  const afterEditor = api.data()
  expect(afterEditor.filter((p) => p === '/binflow/api/security/groups/m9-g01?includeUsers=true')).toHaveLength(1)
  expect(afterEditor.filter((p) => p === '/binflow/api/security/users')).toHaveLength(1) // still the single E2 read
  expect(afterEditor.filter((p) => p === '/binflow/api/security/groups')).toHaveLength(1) // list, not re-read

  // cross-view: E2 count (2) === E5 selected roster size === seed plan
  const selectedCount = await page
    .locator('[data-testid="group-form-members"] [data-testid="transfer-selected"] .transfer-item')
    .count()
  expect(selectedCount).toBe(2)
})

test('N02: Status truth — E3 echo drives the editor; list badge flips without any fanout', async ({ page }) => {
  await loginAs(page, 'admin')
  // converge u7 to enabled first (replace preserves stored enabled, so a
  // failed prior run may have left it disabled)
  expect((await sessionApi(page, 'POST', '/api/security/users/u7', { enabled: true })).status).toBe(200)

  // editor BEFORE the flip: checkbox checked from the E3 echo (the retired
  // knownEnabled hack defaulted to checked with a "no echo" hint — the hint
  // text itself is gone)
  await page.goto('/binflow/ui/admin/security/users/u7')
  await expect(page.locator('[data-testid="user-form-enabled"]')).toBeChecked()
  await expect(page.locator('[data-testid="user-form"]')).not.toContainText('暂无 enabled 回显')

  // flip disabled out-of-band (the partial-update arm), then read the pages
  expect((await sessionApi(page, 'POST', '/api/security/users/u7', { enabled: false })).status).toBe(200)

  const api = trackApiGets(page)
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="user-row-u7"]')).toBeVisible()
  await expect(page.locator('[data-testid="user-status-u7"]')).toHaveText('禁用')
  await expect(page.locator('[data-testid="user-status-u1"]')).toHaveText('启用')
  expect(api.userFanout()).toEqual([]) // the badge is the LIST echo, not a detail read

  // editor AFTER the flip: unchecked from the DB-row fact (E3), facts row echoes
  await page.goto('/binflow/ui/admin/security/users/u7')
  await expect(page.locator('[data-testid="user-form-enabled"]')).not.toBeChecked()
  await expect(page.locator('[data-testid="user-facts"] .status-pill.status-off')).toHaveText('禁用')

  // converge u7 back (in-test, NOT in afterAll — a per-worker afterAll races
  // this test's disable under fullyParallel and un-flips it mid-assert)
  expect((await sessionApi(page, 'POST', '/api/security/users/u7', { enabled: true })).status).toBe(200)
})

test('N03: delete user — typed-name strong confirm, cascade in the E5 view, 404 verbatim, guards', async ({ page }) => {
  await loginAs(page, 'admin')
  const victim = uniq('t257v')
  // dedicated group (NOT m9-g01): the parallel N01 groups leg counts m9-g01's
  // seeded roster — a transient victim there would race its member assertions
  const cascadeGroup = uniq('t257cg')
  expect((await sessionApi(page, 'PUT', `/api/security/groups/${cascadeGroup}`, { name: cascadeGroup, description: 't257 cascade' })).status).toBe(201)

  // fixture: the victim's only membership rides the cascade group
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${victim}`, {
        name: victim,
        email: `${victim}@example.com`,
        password: 't257-pw-del',
        admin: false,
        adminRole: 'user',
        groups: [cascadeGroup],
      })
    ).status,
  ).toBe(201)

  // strong confirm: irreversible wording + typed-name gate (wrong name keeps
  // the button disabled; cancel leaves the row intact)
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator(`[data-testid="user-row-${victim}"]`)).toBeVisible()
  await page.click(`[data-testid="user-delete-${victim}"]`)
  await expect(page.locator('[data-testid="confirm-dialog"]')).toContainText('不可恢复')
  await page.fill('[data-testid="user-delete-confirm-name"]', `${victim}x`)
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeDisabled()
  await page.click('[data-testid="confirm-cancel"]')
  await expect(page.locator('[data-testid="confirm-dialog"]')).toHaveCount(0)
  await expect(page.locator(`[data-testid="user-row-${victim}"]`)).toBeVisible()

  // confirm with the exact name -> server text toast + the row leaves the
  // refreshed list (no manual reload)
  await page.click(`[data-testid="user-delete-${victim}"]`)
  await page.fill('[data-testid="user-delete-confirm-name"]', victim)
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeEnabled()
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: 'removed successfully' })).toBeVisible({ timeout: 8000 })
  await expect(page.locator(`[data-testid="user-row-${victim}"]`)).toHaveCount(0)

  // cascade: GET 404 verbatim (text body — the DELETE-404 family shape), the
  // E5 group view no longer lists the victim (its only membership)
  const gone = await sessionApi(page, 'GET', `/api/security/users/${victim}`)
  expect(gone.status).toBe(404)
  expect(gone.text).toBe('User not found')
  const e5 = await sessionApi(page, 'GET', `/api/security/groups/${cascadeGroup}?includeUsers=true`)
  expect(e5.status).toBe(200)
  expect((e5.json as { userNames: string[] }).userNames).toEqual([])

  // re-delete: deterministic 404, intentionally NOT idempotent (M9 review
  // ruling) — the text body is the family's plain-text shape
  const again = await sessionApi(page, 'DELETE', `/api/security/users/${victim}`)
  expect(again.status).toBe(404)
  expect(again.text).toBe('User not found')

  // guards: self / built-in rows are pre-disabled (server 400 remains the
  // arbiter); the built-in message is verbatim when replayed on the wire
  const me = 'admin'
  await expect(page.locator(`[data-testid="user-delete-${me}"]`)).toBeDisabled()
  const builtin = await sessionApi(page, 'DELETE', `/api/security/users/${me}`)
  expect(builtin.status).toBe(400)
  expect(builtin.text).toBe('Cannot delete the built-in admin user.')

  // a NON-built-in admin deleting ITSELF: the self-guard message verbatim
  // (the last-admin 400 needs a zero-admin instance — server-test territory,
  // t251_users_domain_test.go; not reachable here without invasive surgery)
  const second = uniq('t257adm')
  const client = m9Client()
  expect(
    (
      await client.request('PUT', `/binflow/api/security/users/${second}`, {
        body: {
          name: second,
          email: `${second}@example.com`,
          password: 't257-pw-adm',
          admin: true,
          adminRole: 'admin',
          groups: [],
        },
      })
    ).status,
  ).toBe(201)
  const ctx = await page.context().browser()!.newContext()
  const selfPage = await ctx.newPage()
  await selfPage.goto('/binflow/ui/')
  await selfPage.fill('[data-testid="login-username"]', second)
  await selfPage.fill('[data-testid="login-password"]', 't257-pw-adm')
  await selfPage.click('[data-testid="login-submit"]')
  await expect(selfPage.locator('[data-testid="session-user"]')).toHaveText(second)
  const selfDel = await sessionApi(selfPage, 'DELETE', `/api/security/users/${second}`)
  expect(selfDel.status).toBe(400)
  expect(selfDel.text).toBe('Cannot delete the current authenticated user.')
  await ctx.close()
  // cleanup: with the built-in admin still seated, the second admin is not
  // last — the delete succeeds; the cascade group is empty and unreferenced
  const cleanup = await client.request('DELETE', `/binflow/api/security/users/${second}`)
  expect(cleanup.status).toBe(200)
  expect((await client.request('DELETE', `/binflow/api/security/groups/${cascadeGroup}`)).status).toBe(200)
})

test('N03: delete from the editor danger zone navigates back; self-guard disables the zone', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  const victim = uniq('t257e')
  expect(
    (
      await sessionApi(page, 'PUT', `/api/security/users/${victim}`, {
        name: victim,
        email: `${victim}@example.com`,
        password: 't257-pw-ed',
        admin: false,
        groups: [],
      })
    ).status,
  ).toBe(201)

  // editor danger zone: same strong confirm; success returns to the list
  await page.goto(`/binflow/ui/admin/security/users/${victim}`)
  await expect(page.locator('[data-testid="user-danger-zone"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="user-detail-page"]' })
  await page.click('[data-testid="user-delete"]')
  await page.fill('[data-testid="user-delete-confirm-name"]', victim)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: 'removed successfully' })).toBeVisible({ timeout: 8000 })
  await expect(page).toHaveURL(/\/admin\/security\/users$/)
  await expect(page.locator(`[data-testid="user-row-${victim}"]`)).toHaveCount(0)

  // the editor's own self-guard: admin editing the built-in admin sees the
  // zone button disabled (same two guardrails as the list row)
  await page.goto('/binflow/ui/admin/security/users/admin')
  await expect(page.locator('[data-testid="user-danger-zone"] [data-testid="user-delete"]')).toBeDisabled()
})

test('N03: readonly_admin — no delete entry (L4), write replay stays 403 server-side', async ({ page }) => {
  const fresh = uniq('t257ro')
  const client = m9Client()
  await client.request('PUT', `/binflow/api/security/users/${fresh}`, {
    body: { name: fresh, email: `${fresh}@example.com`, password: 't257-pw-ro', admin: false, groups: [] },
  })
  const ro = await loginAs(page, 'readonly_admin')

  // L4: the admin-gated action column never renders; the editor shows no
  // danger zone (server 403 remains the arbiter)
  await page.goto('/binflow/ui/admin/security/users')
  await expect(page.locator('[data-testid="users-table"]')).toBeVisible()
  await expect(page.locator('[data-testid="users-table"] [data-testid^="user-delete-"]')).toHaveCount(0)
  await page.goto(`/binflow/ui/admin/security/users/${ro.username}`)
  await expect(page.locator('[data-testid="user-danger-zone"]')).toHaveCount(0)

  const replay = await sessionApi(page, 'DELETE', `/api/security/users/${fresh}`)
  expect(replay.status).toBe(403)
  await client.request('DELETE', `/binflow/api/security/users/${fresh}`) // converge
})

// (the membership WRITE arm — group editor toggling a member — stays covered
// by the m8 suite e2e/m8/users-groups.spec.ts; the M9 legs above cover the
// new single-source data plane and the E4 delete surface.)
