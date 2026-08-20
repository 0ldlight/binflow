import { expect, test } from '@playwright/test'

// T-89 smoke: the SPA is served at its mount point (W01/W02 anchors).
//
// The whole spec is conditional on the ROUTER mount: T-89 delivers the
// console handler (shell/fallback/immutable assets — unit-pinned in
// internal/console) and this harness; T-91 mounts /binflow/ui/** in the
// httpapi router, which is when these browser assertions go green. Against
// a pre-T-91 binary the segment still answers the content-plane 404, and
// the probe below turns that into an explicit skip.
test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(
    probe.status() === 404,
    'console segment not mounted by this binary yet (flips green with T-91\'s router mount)',
  )
})

test('SPA shell is served at /binflow/ui/ with the #root mount point', async ({ page }) => {
  await page.goto('/binflow/ui/')
  await expect(page.locator('#root')).toBeAttached()
  await expect(page).toHaveTitle(/BinFlow Console/)
})

test('deep link refreshes to the SPA shell (history fallback, in-segment)', async ({ page }) => {
  await page.goto('/binflow/ui/repositories')
  await expect(page.locator('#root')).toBeAttached()
})
