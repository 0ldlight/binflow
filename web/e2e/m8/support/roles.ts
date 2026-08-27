import { expect } from '@playwright/test'
import type { Page } from '@playwright/test'
import { converge, ensureReadGrant, ensureUser, m8Client, roleFixturesFromEnv, seedRepos } from './seed'

// T-232: three-role session helper (ADR-0026 closed set). loginAs(role) is
// the ONE login path M8 specs use — anchors come from the frozen console-ux
// §10 inventory (login-username / login-password / login-submit / app-nav /
// session-user), which ADR-0029 decision 3 keeps stable across the IA rework:
// anchors do not follow routes.
//
// Provisioning is idempotent (same body PUT converges) — but only ONCE THE
// ROW EXISTS. On a fresh instance N parallel workers calling loginAs at once
// first-create the same rows concurrently and the server's create-or-replace
// PUT is a read-modify-write, so the losers surface the UNIQUE collision as a
// 5xx (T-297's D-9 evidence: "one 201 creates, the rest replace" held only
// for the serialized case). T-326 D-9①: every step rides converge(), whose
// retry converges on the winner's row. Set M8_SKIP_PROVISION=1 on a
// known-seeded instance to skip the REST leg entirely.

export type M8Role = keyof ReturnType<typeof roleFixturesFromEnv>

/** Provision (idempotently) the role fixtures, the perf repo and the plain
 * user's read grant on it. Repo first: the grant target references it. */
export async function provisionRoles(): Promise<void> {
  if (process.env.M8_SKIP_PROVISION === '1') return
  const roles = roleFixturesFromEnv()
  const client = m8Client()
  for (const role of ['user', 'readonly_admin'] as const) {
    await converge(() => ensureUser(client, roles[role]))
  }
  await converge(() => seedRepos(client, [{ key: 'm8-perf-local' }]))
  await converge(() => ensureReadGrant(client, roles.user.name, 'm8-perf-local'))
}

export interface RoleSession {
  role: M8Role
  username: string
  password: string
}

/**
 * Log `role` in through the real login page and wait for the shell.
 * Keyboard discipline note: this helper uses fill/click for brevity — specs
 * that assert KEYBOARD reachability (the smoke leg) drive the same anchors
 * with page.keyboard directly instead.
 */
export async function loginAs(page: Page, role: M8Role): Promise<RoleSession> {
  await provisionRoles()
  const fixture = roleFixturesFromEnv()[role]

  await page.goto('/binflow/ui/')
  await expect(page.locator('[data-testid="login-page"]')).toBeVisible()
  await page.fill('[data-testid="login-username"]', fixture.name)
  await page.fill('[data-testid="login-password"]', fixture.password)
  await page.click('[data-testid="login-submit"]')

  // The shell contract every role shares: nav mounted + the session box
  // echoes the identity (server truth, not the form's echo).
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()
  await expect(page.locator('[data-testid="session-user"]')).toHaveText(fixture.name)
  if (role === 'readonly_admin') {
    await expect(page.locator('[data-testid="session-readonly-badge"]')).toBeVisible()
  } else {
    await expect(page.locator('[data-testid="session-readonly-badge"]')).toHaveCount(0)
  }
  return { role, username: fixture.name, password: fixture.password }
}
