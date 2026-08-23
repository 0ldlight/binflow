import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { provisionRoles } from './support/roles'
import { roleFixturesFromEnv } from './support/seed'

// T-232 M8 smoke: every role logs in, the shell renders, logout revokes —
// the WHOLE flow driven by page.keyboard (no page.fill / page.click anywhere:
// reachability is the assertion, FR-75's premise leg). Anchors are the frozen
// console-ux §10 set; ADR-0029 decision 3 keeps them route-independent, so
// this spec survives the T-235 IA rework unchanged.
//
// Keyboard idiom: page.focus() positions on a control deterministically (the
// end state of tab traversal), then EVERY activation is a key press. The
// login leg does not even need focus(): the username field is autofocused,
// and Tab order carries the rest — asserted below, not assumed.

const roles = ['admin', 'user', 'readonly_admin'] as const

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  await provisionRoles()
})

/** The full keyboard leg for one role. Shared by the three role tests so the
 * operation-flow table in the README maps 1:1 onto this code. */
async function keyboardLoginShellLogout(page: Page, role: (typeof roles)[number]) {
  const fixture = roleFixturesFromEnv()[role]

  // Step 1 — visit the console root; the route guard bounces to /login.
  await page.goto('/binflow/ui/')
  await expect(page).toHaveURL(/\/binflow\/ui\/login/)
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()

  // Step 2 — username field receives focus on its own (autofocus); type.
  await expect(page.locator('[data-testid="login-username"]')).toBeFocused()
  await page.keyboard.type(fixture.name)

  // Step 3 — Tab reaches the password field; type.
  await page.keyboard.press('Tab')
  await expect(page.locator('[data-testid="login-password"]')).toBeFocused()
  await page.keyboard.type(fixture.password)

  // Step 4 — Enter submits the form (the only submit control in the card).
  await page.keyboard.press('Enter')

  // Step 5 — the shell renders for EVERY role: nav + identity echo.
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
  await expect(page.locator('[data-testid="session-user"]')).toHaveText(fixture.name)
  if (role === 'readonly_admin') {
    await expect(page.locator('[data-testid="session-readonly-badge"]')).toBeVisible()
  } else {
    await expect(page.locator('[data-testid="session-readonly-badge"]')).toHaveCount(0)
  }

  // Step 6 — open the session menu from the keyboard.
  await page.focus('[data-testid="session-toggle"]')
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="logout-button"]')).toBeVisible()

  // Step 7 — activate logout; the danger confirm traps focus (cancel is the
  // safety default) — Esc cancels first, proving keyboard escape works.
  await page.focus('[data-testid="logout-button"]')
  await page.keyboard.press('Enter')
  const dialog = page.locator('[data-testid="confirm-dialog"]')
  await expect(dialog).toBeVisible()
  await expect(page.locator('[data-testid="confirm-cancel"]')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible() // still logged in

  // Step 8 — log out for real: reopen, confirm via Tab + Enter.
  await page.focus('[data-testid="session-toggle"]')
  await page.keyboard.press('Enter')
  await page.focus('[data-testid="logout-button"]')
  await page.keyboard.press('Enter')
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Tab') // cancel -> accept
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeFocused()
  await page.keyboard.press('Enter')

  // Step 9 — back on /login; the session is revoked server-side, so a
  // protected deep link bounces to the guard again (auth-shell precedent).
  await expect(page).toHaveURL(/\/login/)
  await page.goto('/binflow/ui/repositories')
  await expect(page).toHaveURL(/\/login\?return=/)
}

for (const role of roles) {
  test(`keyboard smoke: ${role} login -> shell -> logout (page.keyboard only)`, async ({ page }) => {
    await keyboardLoginShellLogout(page, role)
  })
}
