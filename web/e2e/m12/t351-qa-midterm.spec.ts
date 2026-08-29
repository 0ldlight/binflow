import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

// T-351 QA mid-term authoring (the QA UI-automation capability's first run,
// 2026-08-29 user directive — Playwright is the house stack). Target: the
// REAL-STACK trash legs T-352's mock probe cannot reach (its own header
// registers the gap: the shared harness is a community instance where the
// trashcan slot is locked, so browse/restore/empty only ever ran against
// mocks). This spec runs the same anchors against whichever LIVE form $BASE
// points at, self-gating per leg on the license tier:
//
//   community form → the locked posture leg (trash-locked, zero list)
//   pro form       → capture-by-REST → UI browse → five-tuple detail →
//                    restore roundtrip → artifact back + audit picker's
//                    M12 vocabulary (cleanup.run / trash.restore visible)
//
// Anchors only (console-ux testid discipline); four-state assertions on the
// states the real stack can reach. No product code changes — spec only.

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PW = process.env.ADMIN_PW ?? 'password'
const TRASH_PAGE = '/binflow/ui/admin/governance/trash'
const AUDIT_PAGE = '/binflow/ui/admin/governance/audit'

async function login(page: Page) {
  await page.goto('/binflow/ui/')
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
}

/** Same-origin REST helper (session cookie rides the browser context). */
async function rest(page: Page, method: string, path: string, body?: string) {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(new URL(path, window.location.origin).toString(), {
        method,
        headers: body === undefined ? {} : { 'Content-Type': 'application/octet-stream' },
        body,
      })
      const text = await res.text()
      return { status: res.status, text }
    },
    { method, path, body },
  )
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary')
})

/** The per-leg tier probe: pro legs only run against an unlocked instance. */
async function tier(request: import('@playwright/test').APIRequestContext): Promise<string> {
  const res = await request.get('/binflow/api/system/license', {
    headers: { Authorization: `Basic ${btoa(`${ADMIN}:${ADMIN_PW}`)}` },
  })
  if (!res.ok()) return 'community'
  const doc = (await res.json()) as { tier?: string }
  return doc.tier ?? 'community'
}

test.describe('T-351 QA real-stack trash legs (self-gating on license tier)', () => {
  test('community form: the locked posture card, no browse surface', async ({ page, request }) => {
    test.skip((await tier(request)) !== 'community', 'pro-form leg — run against an unlocked instance')
    await login(page)
    await page.goto(TRASH_PAGE)
    await expect(page.locator('[data-testid="trash-locked"]')).toBeVisible()
    // The locked posture renders no list and fires no browse request.
    await expect(page.locator('[data-testid="trash-list"]')).toHaveCount(0)
  })

  test('pro form: capture → browse → five-tuple detail → restore roundtrip', async ({ page, request }) => {
    test.skip((await tier(request)) !== 'pro', 'community-form leg — run against a locked instance')

    // Login first — the seed REST calls ride the session cookie. Clean the
    // subtree so earlier runs' leftovers can't multiply the file row.
    await login(page)
    await rest(page, 'DELETE', '/binflow/api/trash/clean/generic-t351').catch(() => undefined)
    const path = `qa-t351/restore-roundtrip/ui-${Date.now()}.txt`
    const up = await rest(page, 'PUT', `/binflow/generic-t351/${path}`, 't351 ui restore fixture')
    test.skip(up.status !== 201 && up.status !== 200, `seed upload failed (${up.status}) — create generic-t351 first`)
    const del = await rest(page, 'DELETE', `/binflow/generic-t351/${path}`)
    expect(del.status).toBe(204)
    await page.goto(TRASH_PAGE)
    // The can's root lists the original repository as a folder; drill in.
    // (Folder rows select on row-click; the DRILL is the name link inside.)
    await expect(page.locator('[data-testid="trash-list"]')).toBeVisible()
    const drill = (row: string) =>
      page.locator(`[data-testid="trash-row-${row}"] a, [data-testid="trash-row-${row}"] button`).first().click()
    await drill('generic-t351')
    await drill('generic-t351/qa-t351')
    await drill('generic-t351/qa-t351/restore-roundtrip')
    const row = page.locator(`[data-testid^="trash-row-generic-t351/qa-t351/restore-roundtrip/ui-"]`)
    await expect(row).toHaveCount(1)
    await row.click()
    // Five-tuple detail: the delete bookkeeping the restore depends on.
    const detail = page.locator('[data-testid="trash-detail"]')
    await expect(detail).toBeVisible()
    await expect(detail).toContainText('trash.originalRepository')
    await expect(detail).toContainText('generic-t351')
    await expect(detail).toContainText('trash.originalPath')
    await expect(detail).toContainText(path)
    await expect(detail).toContainText('trash.deletedBy')

    // Restore in place (empty to → the five-tuple's original location).
    await page.click('[data-testid^="trash-restore-generic-t351/"]')
    await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
    await page.click('[data-testid="confirm-accept"]')
    // The success surface is a transient toast; the roundtrip's authority is
    // the REST readback below (the L16 wire), so no brittle toast wait here.

    // The artifact is back, byte-identical (REST readback).
    const back = await rest(page, 'GET', `/binflow/generic-t351/${path}`)
    expect(back.status).toBe(200)
    expect(back.text).toBe('t351 ui restore fixture')
  })

  test('pro form: audit picker carries the M12 vocabulary (cleanup.run / trash.restore)', async ({ page, request }) => {
    test.skip((await tier(request)) !== 'pro', 'community-form leg — run against a locked instance')
    await login(page)
    await page.goto(AUDIT_PAGE)
    await expect(page.locator('[data-testid="audit-page"]')).toBeVisible()
    await page.click('[data-testid="audit-filter-action"]')
    // The 54-word mirror (T-352+353): a NATIVE select — the M12 additions
    // must be selectable options.
    const options = page.locator('[data-testid="audit-filter-action"] option')
    await expect(options.filter({ hasText: 'trash.restore' })).toHaveCount(1)
    await expect(options.filter({ hasText: 'cleanup.run' })).toHaveCount(1)
    const count = await options.count()
    expect(count).toBeGreaterThanOrEqual(54)
  })
})
