import type { Page } from '@playwright/test'
import {
  M8_ROLE_USERS,
  adminCredential,
  converge,
  countTreeNodes,
  ensureReadGrant,
  ensureUser,
  makeClient,
  plannedNodeCount,
  roleFixturesFromEnv,
  seedAll,
  seedRepos,
  seedTree,
} from '../../../scripts/seed-m8.mjs'

// T-232: seeding surface for M8 specs. The implementation lives in
// web/scripts/seed-m8.mjs (also the standalone CLI); this module re-exports it
// for the TS world and adds the two in-spec idioms:
//   m8Client()    admin REST client from the same env the harness uses
//   sessionApi()  same-origin fetch riding the PAGE's session cookie — the
//                 reconcile idiom every existing spec uses (rbac.spec V12+)

export {
  M8_ROLE_USERS,
  adminCredential,
  converge,
  countTreeNodes,
  ensureReadGrant,
  ensureUser,
  makeClient,
  plannedNodeCount,
  roleFixturesFromEnv,
  seedAll,
  seedRepos,
  seedTree,
}

/** Admin client pointed at the same base the Playwright harness targets
 * (config default http://127.0.0.1:8080, BASE overridable). */
export function m8Client() {
  const roles = roleFixturesFromEnv()
  return makeClient({ base: process.env.BASE ?? 'http://127.0.0.1:8080', username: roles.admin.name, password: roles.admin.password })
}

/** Same-origin fetch carrying the page's session cookie. Returns
 * { status, text, json } — json null for plain-text/empty bodies. */
export async function sessionApi(
  page: Page,
  method: string,
  path: string,
  body?: unknown,
): Promise<{ status: number; text: string; json: unknown }> {
  const r = await page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(`/binflow${path}`, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : body,
      })
      return { status: res.status, text: await res.text() }
    },
    { method, path, body },
  )
  let json: unknown = null
  try {
    json = JSON.parse(r.text)
  } catch {
    // plain-text or empty body — the management plane has both shapes
  }
  return { ...r, json }
}
