#!/usr/bin/env node
// BinFlow M8 e2e seed (T-232): repositories + role users + a >=10,000-node
// directory tree, all through the REAL REST/content plane so the fixture is
// itself a workout of the exact wire the console rides.
//
//   users   PUT /binflow/api/security/users/{name}      (201 create / 200 replace)
//   repos   PUT /binflow/api/repositories/{key}          (200)
//   grants  POST /binflow/api/v1/permissions             (create-if-absent)
//   tree    PUT /binflow/{repo}/{path}                   (201, ancestors
//           materialize — T-128 — so one deep PUT yields a whole chain of
//           folder nodes)
//   count   GET /binflow/api/storage/{repo}/{dir}?list&deep=1 (files[] + the
//           queried folder itself)
//
// Tree plan (deterministic, so re-runs converge on the same nodes):
//   perf/                  root                          1 node
//   perf/w00..w49/         wide layer, 20 files each  1'050 nodes / 1'000 PUTs
//   perf/c000..c119/       120 chains x 76 nodes      9'240 nodes /   120 PUTs
//                                                      --------------------
//                                                       10'291 nodes / 1'120 PUTs
// The wide layer exercises one-level expand (the tree UI's lazy load); the
// deep chains pile up node count cheaply (ancestors materialize per PUT).
// Longest path ~318 chars, under the 512-char node-path limit (validate.go).
//
// Standalone (the QA / dev entrypoint — same binary-freshness discipline as
// scripts/m7-resume-probe.sh: rebuild when any Go input is newer than bin/):
//   node scripts/seed-m8.mjs [--base http://127.0.0.1:8080]
//                            [--admin admin] [--password password]
//                            [--repo m8-perf-local] [--skip-tree] [--plan-only]
//
// From specs (web/e2e/m8): the module exports the pieces so tests seed
// exactly what their leg needs; see e2e/m8/support/seed.ts.

// `fetch` is a Node >= 18 global; the eslint globals block for scripts/ does
// not declare it, so reach through globalThis to stay lint-clean without
// touching the shared config (area discipline). Buffer comes in as an
// explicit node: import for the same reason.
import { Buffer } from 'node:buffer'

const httpFetch = globalThis.fetch

const PERM_REPO_FALLBACK = 'm8-perf-local'

/** Three-role fixtures (ADR-0026 closed set). admin is the INSTANCE account
 * (env-driven, never provisioned here); the other two are created
 * idempotently — same name, same body, PUT converges. */
export const M8_ROLE_USERS = {
  admin: {
    name: 'admin',
    password: 'password',
    adminRole: 'admin',
    provisioned: false,
  },
  user: {
    name: 'm8-e2e-user',
    password: 'm8-e2e-user-pass',
    adminRole: 'user',
    provisioned: true,
  },
  readonly_admin: {
    name: 'm8-e2e-readonly',
    password: 'm8-e2e-readonly-pass',
    adminRole: 'readonly_admin',
    provisioned: true,
  },
}

/** Apply env overrides for the instance admin (the same ADMIN_USER/ADMIN_PW
 * convention every existing e2e spec uses). */
export function roleFixturesFromEnv(env = process.env) {
  return {
    ...M8_ROLE_USERS,
    admin: {
      ...M8_ROLE_USERS.admin,
      name: env.ADMIN_USER ?? M8_ROLE_USERS.admin.name,
      password: env.ADMIN_PW ?? M8_ROLE_USERS.admin.password,
    },
  }
}

export function makeClient({ base, username, password }) {
  const root = String(base ?? 'http://127.0.0.1:8080').replace(/\/+$/, '')
  const auth = Buffer.from(`${username}:${password}`).toString('base64')
  /** One REST call; non-2xx throws with status + body for actionable logs. */
  async function request(method, path, { body, headers = {}, raw } = {}) {
    const res = await httpFetch(`${root}${path}`, {
      method,
      headers: {
        Authorization: `Basic ${auth}`,
        ...(body !== undefined && !raw ? { 'Content-Type': 'application/json' } : {}),
        ...headers,
      },
      body: body === undefined ? undefined : raw ? body : JSON.stringify(body),
    })
    const text = await res.text()
    if (!res.ok) {
      throw new Error(`seed: ${method} ${path} -> ${res.status}: ${text.slice(0, 300)}`)
    }
    return { status: res.status, text }
  }
  return { base: root, request }
}

/** Idempotent user upsert (full replace body — the M7 7.5 wire posture). */
export async function ensureUser(client, { name, password, adminRole = 'user' }) {
  const r = await client.request('PUT', `/binflow/api/security/users/${name}`, {
    body: {
      name,
      email: `${name}@m8-e2e.invalid`,
      password,
      admin: adminRole === 'admin',
      adminRole,
      groups: [],
    },
  })
  return r.status
}

/** Read grant for the plain-user fixture on the perf repo (so non-admin tree
 * legs have a visible subtree — the §2.2 deep-link posture). Create-if-absent:
 * permissions have no PUT, and re-POST of an existing name is a conflict. */
export async function ensureReadGrant(client, userName, repoKey) {
  const name = 'm8-e2e-read'
  const list = await client.request('GET', '/binflow/api/v1/permissions')
  const targets = JSON.parse(list.text)
  if (Array.isArray(targets) && targets.some((t) => t.name === name)) {
    return 'present'
  }
  await client.request('POST', '/binflow/api/v1/permissions', {
    body: {
      name,
      repos: [repoKey ?? PERM_REPO_FALLBACK],
      includePatterns: ['**'],
      excludePatterns: [],
      principals: { users: { [userName]: ['read'] }, groups: {} },
    },
  })
  return 'created'
}

/** Seed repositories. defs: [{ key, rclass='local', packageType='generic',
 * description }]. Accepts 200 (replace) and 201 (create). */
export async function seedRepos(client, defs) {
  const out = []
  for (const d of defs) {
    const key = d.key ?? PERM_REPO_FALLBACK
    const r = await client.request('PUT', `/binflow/api/repositories/${key}`, {
      body: {
        rclass: d.rclass ?? 'local',
        packageType: d.packageType ?? 'generic',
        description: d.description ?? 'm8 e2e fixture (T-232)',
      },
    })
    out.push({ key, status: r.status })
  }
  return out
}

// ---- the >=10,000-node tree -------------------------------------------------

export const TREE_PLAN = {
  root: 'perf',
  wideDirs: 50,
  wideFilesPerDir: 20,
  chains: 120,
  chainDepth: 75,
}

/** Deterministic plan math (single source for both seeding and verification).
 * Each chain contributes its own root folder too: c-folder + chainDepth dirs
 * + 1 file = chainDepth + 2 nodes. */
export function plannedNodeCount(p = TREE_PLAN) {
  const wide = p.wideDirs * (1 + p.wideFilesPerDir)
  const deep = p.chains * (p.chainDepth + 2)
  return 1 + wide + deep
}

function contentFor(path) {
  return `m8-seed ${path}\n`
}

/** Deploy one artifact; ancestors materialize server-side. */
async function putFile(client, repoKey, path) {
  await client.request('PUT', `/binflow/${repoKey}/${path}`, {
    raw: true,
    headers: { 'Content-Type': 'application/octet-stream' },
    body: contentFor(path),
  })
}

/** Run an async job list with bounded concurrency (SQLite-friendly pool). */
async function pool(jobs, limit) {
  let next = 0
  const workers = Array.from({ length: Math.min(limit, jobs.length) }, async () => {
    for (;;) {
      const i = next++
      if (i >= jobs.length) return
      await jobs[i]()
    }
  })
  await Promise.all(workers)
}

/** Seed the perf tree. Returns { planned, verified, skipped } — verified is
 * the deep-list count (+1 for the queried folder itself) and MUST be
 * >= minNodes. Idempotent shortcut: an already-verified tree (e.g. the CLI
 * ran before the suite) is recounted and skipped instead of re-PUT. */
export async function seedTree(client, repoKey, { minNodes = 10_000, concurrency = 6, plan = TREE_PLAN } = {}) {
  const planned = plannedNodeCount(plan)
  if (planned < minNodes) {
    throw new Error(`seed: plan yields ${planned} nodes < required ${minNodes}`)
  }
  let existing = 0
  try {
    existing = await countTreeNodes(client, repoKey, plan.root)
  } catch {
    existing = 0 // no tree yet (404) — the normal fresh path
  }
  if (existing >= planned) {
    return { planned, verified: existing, skipped: true }
  }

  const jobs = []
  for (let w = 0; w < plan.wideDirs; w++) {
    for (let f = 0; f < plan.wideFilesPerDir; f++) {
      const path = `${plan.root}/w${String(w).padStart(2, '0')}/f${String(f).padStart(3, '0')}.txt`
      jobs.push(() => putFile(client, repoKey, path))
    }
  }
  const dirs = Array.from({ length: plan.chainDepth }, (_, i) => `d${String(i).padStart(2, '0')}`)
  for (let c = 0; c < plan.chains; c++) {
    const path = `${plan.root}/c${String(c).padStart(3, '0')}/${dirs.join('/')}/seed.txt`
    jobs.push(() => putFile(client, repoKey, path))
  }
  await pool(jobs, concurrency)

  const verified = await countTreeNodes(client, repoKey, plan.root)
  return { planned, verified, skipped: false }
}

/** Deep-list node count below (and including) `path`. */
export async function countTreeNodes(client, repoKey, path) {
  const r = await client.request('GET', `/binflow/api/storage/${repoKey}/${path}?list&deep=1`)
  const body = JSON.parse(r.text)
  if (!Array.isArray(body.files)) {
    throw new Error(`seed: unexpected ?list shape for ${repoKey}/${path}`)
  }
  return body.files.length + 1 // + the queried folder row itself
}

/** Everything the smoke/perf legs need: role users, repos, read grant, tree.
 * Order matters: the grant's target references the repo row, so repos go
 * first (a 400 "unknown repository" otherwise). */
export async function seedAll(client, { repoKey = PERM_REPO_FALLBACK, skipTree = false } = {}) {
  const started = Date.now()
  const users = []
  for (const role of ['user', 'readonly_admin']) {
    const u = M8_ROLE_USERS[role]
    users.push({ role, name: u.name, status: await ensureUser(client, u) })
  }
  const repos = await seedRepos(client, [{ key: repoKey }])
  const grant = await ensureReadGrant(client, M8_ROLE_USERS.user.name, repoKey)
  const treeStarted = Date.now()
  const tree = skipTree ? null : await seedTree(client, repoKey)
  return {
    base: client.base,
    repoKey,
    users,
    grant,
    repos,
    tree,
    treeMs: tree ? Date.now() - treeStarted : 0,
    elapsedMs: Date.now() - started,
  }
}

// ---- CLI --------------------------------------------------------------------

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`)
  if (i === -1) return fallback
  const v = process.argv[i + 1]
  return v && !v.startsWith('--') ? v : true
}

if (process.argv[1] && process.argv[1].endsWith('seed-m8.mjs')) {
  const base = arg('base', '')
  const admin = arg('admin', '')
  const password = arg('password', '')
  const repoKey = arg('repo', '') || PERM_REPO_FALLBACK
  const planOnly = Boolean(arg('plan-only', false))
  const skipTree = Boolean(arg('skip-tree', false))

  if (planOnly) {
    console.log(JSON.stringify({ plan: TREE_PLAN, plannedNodes: plannedNodeCount() }, null, 2))
    process.exit(0)
  }

  const env = process.env
  const client = makeClient({
    base: typeof base === 'string' && base ? base : env.BASE || 'http://127.0.0.1:8080',
    username: typeof admin === 'string' && admin ? admin : env.ADMIN_USER || M8_ROLE_USERS.admin.name,
    password: typeof password === 'string' && password ? password : env.ADMIN_PW || M8_ROLE_USERS.admin.password,
  })
  seedAll(client, { repoKey, skipTree })
    .then((r) => {
      if (r.tree) {
        console.log(
          `seed-m8: tree verified ${r.tree.verified} nodes (planned ${r.tree.planned}) in ${Math.round(r.treeMs / 100) / 10}s`,
        )
      }
      console.log(JSON.stringify({ ...r, tree: r.tree ? { verified: r.tree.verified, planned: r.tree.planned } : null }))
      if (r.tree && r.tree.verified < 10_000) {
        console.error('seed-m8: VERIFICATION FAILED — fewer than 10,000 nodes')
        process.exit(1)
      }
    })
    .catch((e) => {
      console.error(`seed-m8: ${e.message}`)
      process.exit(1)
    })
}
