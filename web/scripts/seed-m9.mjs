#!/usr/bin/env node
// BinFlow M9 seed (T-250): the FR-78/FR-79 data foundation — 50 repositories,
// 20 users, 10 groups and the four coverage fixtures, all through the REAL
// REST plane so every leg tests against the exact wire the console rides
// (the seed-m8.mjs discipline; makeClient is imported from it).
//
//   repos   PUT /binflow/api/repositories/{key}       x50  m9-r00..m9-r49
//   groups  PUT /binflow/api/security/groups/{name}   x10  m9-g01..m9-g10
//   users   PUT /binflow/api/security/users/{name}    x20  u1..u20 (2 per group)
//   targets POST /binflow/api/v1/permissions          x4   create-if-absent
//
// Coverage fixtures (ADR-0030 / PRD FR-79 AC2+AC4+AC5, E9's group arm):
//   t-u8-r  u8 READ on 10 of the 50 repos — the E1 usage-filter leg: u8's
//           response must be a subset of its readable repos (N06 negative
//           grep: the other 40 keys appear nowhere).
//   t-in    u9 MANAGE, repos m9-r00/m9-r01 — inside u9's coverage: the E6
//           filter=manage leg lists it and u9 may edit it (N07).
//   t-out   u9 READ ONLY, repos m9-r02/m9-r03 — outside the coverage: zero
//           appearance in the filtered list, POST/DELETE stay 403.
//   t-grp   GROUP m9-g01 (u1, u11) manage — the E9 ManageCoverage group arm.
//
// Passwords follow the PRD N-sequence skeleton (u9 -> pw-u9-123). Everything
// converges on re-run: PUT repos/groups/users replace with identical bodies,
// targets are create-if-absent (permissions have no PUT).
//
// Verification is baked in (admin read plane + two live probes against the
// M8-tail authorization semantics):
//   counts    repositories >= 50 (all m9 keys present), 20 users, 10 groups,
//             4 targets
//   u8 probe  content GET on a granted repo answers 404 (authorized, file
//             missing) while a non-granted repo answers 403
//   u9 probe  POST /api/v1/permissions with t-in's body (repos inside the
//             coverage) is a 2xx create-or-replace; the same POST with
//             t-out's body is the family-4 403 — the manage bit is real
//
// Standalone (same conventions as seed-m8.mjs):
//   node scripts/seed-m9.mjs [--base http://127.0.0.1:8080]
//                            [--admin admin] [--password password]
//                            [--plan-only] [--no-verify]
//
// From specs (web/e2e/m9): the module exports the pieces; see
// e2e/m9/support/seed.ts.

// `fetch` is a Node >= 18 global; the eslint globals block for scripts/ does
// not declare it, so reach through globalThis to stay lint-clean without
// touching the shared config (area discipline). Buffer comes in as an
// explicit node: import for the same reason.
import { Buffer } from 'node:buffer'

import { makeClient } from './seed-m8.mjs'

const httpFetch = globalThis.fetch

/** Deterministic plan (single source for seeding and verification). */
export const M9_PLAN = Object.freeze({
  repos: 50,
  users: 20,
  groups: 10,
  repoPrefix: 'm9-r',
  groupPrefix: 'm9-g',
  userPrefix: 'u',
})

export function repoKeys(plan = M9_PLAN) {
  return Array.from({ length: plan.repos }, (_, i) => `${plan.repoPrefix}${String(i).padStart(2, '0')}`)
}

export function groupNames(plan = M9_PLAN) {
  return Array.from({ length: plan.groups }, (_, i) => `${plan.groupPrefix}${String(i + 1).padStart(2, '0')}`)
}

export function userNames(plan = M9_PLAN) {
  return Array.from({ length: plan.users }, (_, i) => `${plan.userPrefix}${i + 1}`)
}

/** PRD N-sequence password convention (u9 -> pw-u9-123). */
export function userPassword(name) {
  return `pw-${name}-123`
}

/** Deterministic spread: 2 users per group (u1,u11 -> m9-g01 ... u10,u20 -> m9-g10). */
export function groupsForUser(name, plan = M9_PLAN) {
  const n = Number(name.slice(plan.userPrefix.length))
  if (!Number.isInteger(n) || n < 1 || n > plan.users) {
    throw new Error(`seed-m9: '${name}' is not a seeded user name`)
  }
  return [`${plan.groupPrefix}${String(((n - 1) % plan.groups) + 1).padStart(2, '0')}`]
}

/** The four coverage targets. repos reference plan-indexed keys so a custom
 * plan stays coherent; principals use the wire verbs (read/write/delete/
 * manage — permissions.go's accepted set). */
export function m9Targets(keys = repoKeys()) {
  const target = (name, repos, principals) => ({
    name,
    body: {
      name,
      repos,
      includePatterns: ['**'],
      excludePatterns: [],
      principals,
    },
  })
  return [
    target('t-u8-r', keys.slice(0, 10), { users: { u8: ['read'] }, groups: {} }),
    target('t-in', [keys[0], keys[1]], { users: { u9: ['manage'] }, groups: {} }),
    target('t-out', [keys[2], keys[3]], { users: { u9: ['read'] }, groups: {} }),
    target('t-grp', [keys[4], keys[5]], { users: {}, groups: { 'm9-g01': ['manage'] } }),
  ]
}

// ---- idempotent ensure helpers ----------------------------------------------

export async function ensureGroup(client, name, description) {
  const r = await client.request('PUT', `/binflow/api/security/groups/${name}`, {
    body: { name, description: description ?? `m9 seed group ${name} (T-250)` },
  })
  return r.status
}

/** Full-replace user upsert carrying the group membership (the M7 7.5 wire
 * posture; groups exist before their members — see seedM9's ordering). */
export async function ensureM9User(client, name, groups) {
  const r = await client.request('PUT', `/binflow/api/security/users/${name}`, {
    body: {
      name,
      email: `${name}@m9-seed.invalid`,
      password: userPassword(name),
      admin: false,
      adminRole: 'user',
      groups: groups ?? groupsForUser(name),
    },
  })
  return r.status
}

/** Create-if-absent permission target (permissions have no PUT; re-POST of
 * an existing name is a replace — m9Targets bodies are converged already). */
export async function ensureTarget(client, def) {
  const list = await client.request('GET', '/binflow/api/v1/permissions')
  const targets = JSON.parse(list.text)
  if (Array.isArray(targets) && targets.some((t) => t.name === def.name)) {
    return 'present'
  }
  await client.request('POST', '/binflow/api/v1/permissions', { body: def.body })
  return 'created'
}

// ---- the seed ----------------------------------------------------------------

/** Order matters: repos before targets (a target naming an unknown repository
 * is a 400), groups before users (membership rides the user body), users
 * before targets (principal validation demands known users/groups). */
export async function seedM9(client, { plan = M9_PLAN } = {}) {
  const started = Date.now()
  const keys = repoKeys(plan)
  const repos = []
  for (const key of keys) {
    const r = await client.request('PUT', `/binflow/api/repositories/${key}`, {
      body: {
        rclass: 'local',
        packageType: 'generic',
        description: `m9 seed repository ${key} (T-250 fanout fixture)`,
      },
    })
    repos.push({ key, status: r.status })
  }
  const groups = []
  for (const g of groupNames(plan)) {
    groups.push({ name: g, status: await ensureGroup(client, g) })
  }
  const users = []
  for (const u of userNames(plan)) {
    users.push({ name: u, status: await ensureM9User(client, u) })
  }
  const targets = []
  for (const t of m9Targets(keys)) {
    targets.push({ name: t.name, status: await ensureTarget(client, t) })
  }
  return {
    base: client.base,
    repos,
    groups,
    users,
    targets,
    elapsedMs: Date.now() - started,
  }
}

// ---- verification ------------------------------------------------------------

/** Non-throwing status probe (makeClient throws on non-2xx by design; probes
 * NEED the denied statuses). */
async function probeStatus(base, username, password, method, path, body) {
  const auth = Buffer.from(`${username}:${password}`).toString('base64')
  const res = await httpFetch(`${base}${path}`, {
    method,
    headers: {
      Authorization: `Basic ${auth}`,
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  await res.arrayBuffer()
  return res.status
}

/** Assert the seed took and the coverage fixtures behave (M8-tail semantics).
 * Returns { ok, problems, evidence } — problems [] means green. */
export async function verifyM9(client, { plan = M9_PLAN } = {}) {
  const problems = []
  const evidence = {}
  const keys = repoKeys(plan)

  const repoList = JSON.parse((await client.request('GET', '/binflow/api/repositories')).text)
  evidence.repoCount = Array.isArray(repoList) ? repoList.length : -1
  const present = new Set((Array.isArray(repoList) ? repoList : []).map((r) => r.key))
  const missing = keys.filter((k) => !present.has(k))
  if (evidence.repoCount < plan.repos) {
    problems.push(`repositories list has ${evidence.repoCount} rows, want >= ${plan.repos}`)
  }
  if (missing.length > 0) {
    problems.push(`missing seeded repositories: ${missing.join(', ')}`)
  }

  const userList = JSON.parse((await client.request('GET', '/binflow/api/security/users')).text)
  evidence.userCount = Array.isArray(userList) ? userList.filter((u) => userNames(plan).includes(u.name)).length : -1
  if (evidence.userCount !== plan.users) {
    problems.push(`seeded users present: ${evidence.userCount}, want ${plan.users}`)
  }

  const groupList = JSON.parse((await client.request('GET', '/binflow/api/security/groups')).text)
  evidence.groupCount = Array.isArray(groupList) ? groupList.filter((g) => groupNames(plan).includes(g.name)).length : -1
  if (evidence.groupCount !== plan.groups) {
    problems.push(`seeded groups present: ${evidence.groupCount}, want ${plan.groups}`)
  }

  const permList = JSON.parse((await client.request('GET', '/binflow/api/v1/permissions')).text)
  const targetNames = m9Targets(keys).map((t) => t.name)
  evidence.targets = Array.isArray(permList) ? targetNames.filter((n) => permList.some((t) => t.name === n)) : []
  if (evidence.targets.length !== targetNames.length) {
    problems.push(`seeded targets present: ${evidence.targets.join(', ') || 'none'}, want ${targetNames.join(', ')}`)
  }

  // u8 partial-read probe (content plane): granted repo passes authz and
  // fails the lookup (404); non-granted repo is denied (403).
  evidence.u8 = {
    granted: await probeStatus(client.base, 'u8', userPassword('u8'), 'GET', `/binflow/${keys[0]}/seed-probe.txt`),
    denied: await probeStatus(client.base, 'u8', userPassword('u8'), 'GET', `/binflow/${keys[49]}/seed-probe.txt`),
  }
  if (evidence.u8.granted !== 404) {
    problems.push(`u8 granted-repo probe want 404 (authorized, missing), got ${evidence.u8.granted}`)
  }
  if (evidence.u8.denied !== 403) {
    problems.push(`u8 non-granted-repo probe want 403, got ${evidence.u8.denied}`)
  }

  // u9 m-coverage probe (management plane, family-4 arm — M8 tail): POST with
  // t-in's body (repos inside the coverage) replaces it 2xx; POST with t-out's
  // body (repos outside) is the coverage 403 before any mutation.
  const defs = Object.fromEntries(m9Targets(keys).map((t) => [t.name, t]))
  evidence.u9 = {
    covered: await probeStatus(client.base, 'u9', userPassword('u9'), 'POST', '/binflow/api/v1/permissions', defs['t-in'].body),
    uncovered: await probeStatus(client.base, 'u9', userPassword('u9'), 'POST', '/binflow/api/v1/permissions', defs['t-out'].body),
  }
  if (evidence.u9.covered < 200 || evidence.u9.covered > 299) {
    problems.push(`u9 t-in POST (inside coverage) want 2xx, got ${evidence.u9.covered}`)
  }
  if (evidence.u9.uncovered !== 403) {
    problems.push(`u9 t-out POST (outside coverage) want 403, got ${evidence.u9.uncovered}`)
  }

  return { ok: problems.length === 0, problems, evidence }
}

// ---- CLI ---------------------------------------------------------------------

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`)
  if (i === -1) return fallback
  const v = process.argv[i + 1]
  return v && !v.startsWith('--') ? v : true
}

if (process.argv[1] && process.argv[1].endsWith('seed-m9.mjs')) {
  const base = arg('base', '')
  const admin = arg('admin', '')
  const password = arg('password', '')
  const planOnly = Boolean(arg('plan-only', false))
  const noVerify = Boolean(arg('no-verify', false))

  if (planOnly) {
    console.log(
      JSON.stringify(
        {
          plan: M9_PLAN,
          repoKeys: repoKeys().length,
          users: userNames(),
          groups: groupNames(),
          targets: m9Targets().map((t) => ({ name: t.name, repos: t.body.repos, principals: t.body.principals })),
        },
        null,
        2,
      ),
    )
    process.exit(0)
  }

  const env = process.env
  const client = makeClient({
    base: typeof base === 'string' && base ? base : env.BASE || 'http://127.0.0.1:8080',
    username: typeof admin === 'string' && admin ? admin : env.ADMIN_USER || 'admin',
    password: typeof password === 'string' && password ? password : env.ADMIN_PW || env.ADMIN_PASSWORD || 'password',
  })

  seedM9(client)
    .then(async (r) => {
      console.log(
        `seed-m9: ${r.repos.length} repos / ${r.users.length} users / ${r.groups.length} groups / ${r.targets.length} targets in ${Math.round(r.elapsedMs / 100) / 10}s (${r.base})`,
      )
      const summary = {
        base: r.base,
        repos: r.repos.length,
        users: r.users.length,
        groups: r.groups.length,
        targets: r.targets,
      }
      if (noVerify) {
        console.log(JSON.stringify(summary))
        return
      }
      const v = await verifyM9(client)
      console.log(JSON.stringify({ ...summary, verify: v }))
      if (!v.ok) {
        console.error(`seed-m9: VERIFICATION FAILED — ${v.problems.length} problem(s)`)
        for (const p of v.problems) console.error(`  - ${p}`)
        process.exit(1)
      }
      console.log('seed-m9: verification green (counts + u8 partial-read + u9 m-coverage probes)')
    })
    .catch((e) => {
      console.error(`seed-m9: ${e.message}`)
      process.exit(1)
    })
}
