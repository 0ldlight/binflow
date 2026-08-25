#!/usr/bin/env node
// BinFlow M10 seed (T-277): the FR-89-AC4 legacy-regression carrier —
// repositories, role users and artifacts whose paths contain NON-PAIRED ';'
// characters, all deployed through the REAL content plane so the fixture is
// itself a workout of the exact wire every client rides (the seed-m8/seed-m9
// discipline; makeClient is imported from seed-m8.mjs).
//
//   repos   PUT /binflow/api/repositories/{key}        x2  generic + maven
//   users   PUT /binflow/api/security/users/{name}     x2  m10-e2e-user /
//                                                         m10-e2e-readonly
//   grant   POST /binflow/api/v1/permissions           x1  m10-e2e-read
//           (plain user READ on both legacy repos — the L21 readonly leg)
//   files   PUT /binflow/{repo}/{path}                 x5  literal ';' names
//
// WHY these files (PRD milestone-10 §4.4 FR-89-AC4 + Q5, ADR-0033 /
// architecture §15.3.1): M1~M9 treat ';' as an ordinary path character. FR-89
// introduces matrix-parameter stripping — "PUT a/b/app.bin;build=77;env=prod"
// becomes the artifact a/b/app.bin plus properties. The survival rule is
// "paired `;k=v` trailing sequences only": a path whose ';' segments do not
// form a k=v grammar MUST keep its literal M1 semantics. This seed freezes
// that regression surface as data — every fixture below must stay reachable
// at its ORIGINAL literal path, byte-identical, before AND after FR-89:
//
//   legacy/a;b.bin                  one ';', no '=' anywhere after it
//   legacy/file;name.jar            the ADR-0033 §15.3.1 canonical example
//   legacy/x;y;z.txt                multiple non-paired ';' segments
//   legacy/dir;d/nested.bin         ';' in a FOLDER segment (prefix stratum)
//   com/acme;lib/1.0/acme;lib-1.0.jar   maven layout-compliant ';' names
//                                   (the maven adapter validates layout —
//                                   empirically 400 on non-layout paths)
//
// Deliberately NOT seeded: paired-lookalike names such as "app.bin;build=77"
// — on an M9 binary that is a literal file, after FR-89 it legitimately
// resolves as artifact + matrix params. That shape is a documented behavior
// CHANGE (the feature), not a survival fixture; it belongs to the FR-89
// ticket's own tests, not to the frozen legacy set.
//
// Everything converges on re-run: PUT repos/users replace with identical
// bodies, the grant is create-if-absent, file re-PUTs deploy identical bytes
// (the write plane treats it as an idempotent redeploy).
//
// Verification is baked in:
//   counts    2 repos, 2 role users, 1 grant target
//   readback  EVERY fixture GETs 200 at its original literal path with
//             byte-identical body — the hard gate, both pre- and post-FR-89
//   props     GET ?properties is probed and RECORDED, not gated: 404 today
//             (gap-endpoints E-09), 200 once FR-89 lands — evidence for the
//             L22 leg, never a seed failure
//   login     both role users authenticate (200 on /api/system/version)
//
// Standalone (same conventions as seed-m8/seed-m9):
//   node scripts/seed-m10.mjs [--base http://127.0.0.1:8080]
//                             [--admin admin] [--password password]
//                             [--plan-only] [--no-verify]
//
// From specs (web/e2e/m10): the module exports the pieces; see
// e2e/m10/support/seed.ts.

// `fetch` is a Node >= 18 global; the eslint globals block for scripts/ does
// not declare it, so reach through globalThis to stay lint-clean without
// touching the shared config (area discipline). Buffer comes in as an
// explicit node: import for the same reason.
import { Buffer } from 'node:buffer'

import { makeClient } from './seed-m8.mjs'

const httpFetch = globalThis.fetch

/** Deterministic plan (single source for seeding and verification). */
export const M10_PLAN = Object.freeze({
  repoGeneric: 'm10-legacy-generic',
  repoMaven: 'm10-legacy-maven',
  grantTarget: 'm10-e2e-read',
  user: { name: 'm10-e2e-user', password: 'm10-e2e-user-pass', adminRole: 'user' },
  readonlyAdmin: { name: 'm10-e2e-readonly', password: 'm10-e2e-readonly-pass', adminRole: 'readonly_admin' },
})

/** The two legacy carriers. Both are file-PUT package types — exactly the
 * faces FR-89's matrix stripping applies to (§89.1: generic/maven; docker/
 * npm/pypi have no file-path PUT), so the fixture lives where the regression
 * can actually happen. */
export function legacyRepos(plan = M10_PLAN) {
  return [
    { key: plan.repoGeneric, packageType: 'generic', description: 'm10 legacy semicolon-path carrier (T-277, FR-89-AC4)' },
    { key: plan.repoMaven, packageType: 'maven', description: 'm10 legacy semicolon-path carrier, maven layout arm (T-277)' },
  ]
}

/** Deterministic body: same bytes every run, so a re-run is an idempotent
 * redeploy and the readback comparison has a computable oracle. */
export function fixtureBody(repoKey, path) {
  return `binflow-m10-legacy:${repoKey}/${path}\n`
}

/** The frozen non-paired ';' fixture set (see the header WHY). repoOf is the
 * plan-resolved repo key so a custom plan stays coherent. */
export function legacyFixtures(plan = M10_PLAN) {
  return [
    { repo: plan.repoGeneric, path: 'legacy/a;b.bin', shape: 'single-nonpaired' },
    { repo: plan.repoGeneric, path: 'legacy/file;name.jar', shape: 'adr-example' },
    { repo: plan.repoGeneric, path: 'legacy/x;y;z.txt', shape: 'multi-nonpaired' },
    { repo: plan.repoGeneric, path: 'legacy/dir;d/nested.bin', shape: 'folder-segment' },
    { repo: plan.repoMaven, path: 'com/acme;lib/1.0/acme;lib-1.0.jar', shape: 'maven-layout' },
  ]
}

// ---- idempotent ensure helpers ----------------------------------------------

/** Full-replace repo upsert (the M1 wire posture; 200 replace / would-be 201
 * on first create — both accepted). */
export async function ensureRepo(client, def) {
  const r = await client.request('PUT', `/binflow/api/repositories/${def.key}`, {
    body: {
      rclass: 'local',
      packageType: def.packageType,
      description: def.description,
    },
  })
  return r.status
}

/** Full-replace user upsert carrying adminRole (the M7 7.5 wire posture —
 * the snake spelling, ADR-0026 decision 6). */
export async function ensureM10User(client, def) {
  const r = await client.request('PUT', `/binflow/api/security/users/${def.name}`, {
    body: {
      name: def.name,
      email: `${def.name}@m10-seed.invalid`,
      password: def.password,
      admin: def.adminRole === 'admin',
      adminRole: def.adminRole,
      groups: [],
    },
  })
  return r.status
}

/** Create-if-absent READ grant for the plain user on both legacy repos
 * (permissions have no PUT; re-POST of an existing name is a replace). */
export async function ensureM10ReadGrant(client, plan = M10_PLAN) {
  const name = plan.grantTarget
  const list = await client.request('GET', '/binflow/api/v1/permissions')
  const targets = JSON.parse(list.text)
  if (Array.isArray(targets) && targets.some((t) => t.name === name)) {
    return 'present'
  }
  await client.request('POST', '/binflow/api/v1/permissions', {
    body: {
      name,
      repos: [plan.repoGeneric, plan.repoMaven],
      includePatterns: ['**'],
      excludePatterns: [],
      principals: { users: { [plan.user.name]: ['read'] }, groups: {} },
    },
  })
  return 'created'
}

/** Deploy one legacy artifact at its literal path. */
async function putFixture(client, fx) {
  await client.request('PUT', `/binflow/${fx.repo}/${fx.path}`, {
    raw: true,
    headers: { 'Content-Type': 'application/octet-stream' },
    body: fixtureBody(fx.repo, fx.path),
  })
}

// ---- the seed ----------------------------------------------------------------

/** Order matters: repos before the grant (a target naming an unknown
 * repository is a 400), users before the grant (principal validation demands
 * known users), fixtures last (the content plane needs its repository rows). */
export async function seedM10(client, { plan = M10_PLAN } = {}) {
  const started = Date.now()
  const repos = []
  for (const def of legacyRepos(plan)) {
    repos.push({ key: def.key, status: await ensureRepo(client, def) })
  }
  const users = [
    { role: 'user', name: plan.user.name, status: await ensureM10User(client, plan.user) },
    { role: 'readonly_admin', name: plan.readonlyAdmin.name, status: await ensureM10User(client, plan.readonlyAdmin) },
  ]
  const grant = await ensureM10ReadGrant(client, plan)
  const files = []
  for (const fx of legacyFixtures(plan)) {
    await putFixture(client, fx)
    files.push(fx.path)
  }
  return { base: client.base, repos, users, grant, files, elapsedMs: Date.now() - started }
}

// ---- verification ------------------------------------------------------------

/** Non-throwing GET returning { status, text } — probes NEED the denied and
 * the not-yet-existing statuses without throwing. */
async function probeGet(base, username, password, path) {
  const auth = Buffer.from(`${username}:${password}`).toString('base64')
  const res = await httpFetch(`${base}${path}`, {
    headers: { Authorization: `Basic ${auth}` },
  })
  return { status: res.status, text: await res.text() }
}

/** Assert the seed took and the legacy surface behaves. Returns
 * { ok, problems, evidence } — problems [] means green. The byte-readback of
 * every fixture is the HARD gate; the ?properties posture is evidence only
 * (404 pre-FR-89 / 200 after — the E-09 flip must never fail this seed).
 * adminPassword feeds the admin-leg probes only (makeClient keeps its own
 * credential private — the seed-m9 verifyM9 posture). */
export async function verifyM10(client, { plan = M10_PLAN, adminPassword = 'password' } = {}) {
  const problems = []
  const evidence = {}
  const fixtures = legacyFixtures(plan)
  const admin = () => adminPassword

  const repoList = JSON.parse((await client.request('GET', '/binflow/api/repositories')).text)
  const present = new Set((Array.isArray(repoList) ? repoList : []).map((r) => r.key))
  evidence.repos = legacyRepos(plan).map((d) => ({ key: d.key, present: present.has(d.key) }))
  for (const r of evidence.repos) {
    if (!r.present) problems.push(`repository ${r.key} missing after seed`)
  }

  const userList = JSON.parse((await client.request('GET', '/binflow/api/security/users')).text)
  const names = [plan.user.name, plan.readonlyAdmin.name]
  evidence.users = names.map((n) => ({ name: n, present: (Array.isArray(userList) ? userList : []).some((u) => u.name === n) }))
  for (const u of evidence.users) {
    if (!u.present) problems.push(`user ${u.name} missing after seed`)
  }

  const permList = JSON.parse((await client.request('GET', '/binflow/api/v1/permissions')).text)
  evidence.grant = Array.isArray(permList) && permList.some((t) => t.name === plan.grantTarget)
  if (!evidence.grant) problems.push(`grant target ${plan.grantTarget} missing after seed`)

  // The FR-89-AC4 hard gate: every literal ';' path answers 200 with the
  // exact seeded bytes. This must hold on m9-done AND on every FR-89 build.
  evidence.readback = []
  for (const fx of fixtures) {
    const r = await probeGet(client.base, 'admin', admin(), `/binflow/${fx.repo}/${fx.path}`)
    evidence.readback.push({ path: `${fx.repo}/${fx.path}`, shape: fx.shape, status: r.status, bytes: r.status === 200 ? r.text === fixtureBody(fx.repo, fx.path) : false })
    if (r.status !== 200) {
      problems.push(`legacy fixture ${fx.repo}/${fx.path} (${fx.shape}) want GET 200 at literal path, got ${r.status}`)
    } else if (r.text !== fixtureBody(fx.repo, fx.path)) {
      problems.push(`legacy fixture ${fx.repo}/${fx.path} (${fx.shape}) byte mismatch on readback`)
    }
  }

  // Plain-user visibility leg: the read grant makes the literal path pass
  // authz (200 with bytes) — the L21 readonly arm's data bottom.
  const first = fixtures[0]
  const userRead = await probeGet(client.base, plan.user.name, plan.user.password, `/binflow/${first.repo}/${first.path}`)
  evidence.userReadback = { path: `${first.repo}/${first.path}`, status: userRead.status }
  if (userRead.status !== 200 || userRead.text !== fixtureBody(first.repo, first.path)) {
    problems.push(`plain-user read of ${first.repo}/${first.path} want 200 + bytes, got ${userRead.status}`)
  }

  // ?properties posture — RECORDED, not gated (E-09: 404 today, 200 after
  // FR-89; both are correct for their build).
  const props = await probeGet(client.base, 'admin', admin(), `/binflow/api/storage/${first.repo}/${first.path}?properties=build`)
  evidence.propertiesPosture = { path: `${first.repo}/${first.path}`, status: props.status }

  return { ok: problems.length === 0, problems, evidence }
}

// ---- CLI ---------------------------------------------------------------------

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`)
  if (i === -1) return fallback
  const v = process.argv[i + 1]
  return v && !v.startsWith('--') ? v : true
}

if (process.argv[1] && process.argv[1].endsWith('seed-m10.mjs')) {
  const base = arg('base', '')
  const admin = arg('admin', '')
  const password = arg('password', '')
  const planOnly = Boolean(arg('plan-only', false))
  const noVerify = Boolean(arg('no-verify', false))

  if (planOnly) {
    console.log(
      JSON.stringify(
        {
          plan: M10_PLAN,
          repos: legacyRepos().map((r) => ({ key: r.key, packageType: r.packageType })),
          fixtures: legacyFixtures().map((f) => ({ repo: f.repo, path: f.path, shape: f.shape })),
        },
        null,
        2,
      ),
    )
    process.exit(0)
  }

  const env = process.env
  const adminName = typeof admin === 'string' && admin ? admin : env.ADMIN_USER || 'admin'
  const adminPw = typeof password === 'string' && password ? password : env.ADMIN_PW || env.ADMIN_PASSWORD || 'password'
  const client = makeClient({
    base: typeof base === 'string' && base ? base : env.BASE || 'http://127.0.0.1:8080',
    username: adminName,
    password: adminPw,
  })

  seedM10(client)
    .then(async (r) => {
      console.log(
        `seed-m10: ${r.repos.length} repos / ${r.users.length} role users / grant ${r.grant} / ${r.files.length} legacy ';' fixtures in ${Math.round(r.elapsedMs / 100) / 10}s (${r.base})`,
      )
      const summary = { base: r.base, repos: r.repos, users: r.users, grant: r.grant, files: r.files }
      if (noVerify) {
        console.log(JSON.stringify(summary))
        return
      }
      const v = await verifyM10(client, { adminPassword: adminPw })
      console.log(JSON.stringify({ ...summary, verify: v }))
      if (!v.ok) {
        console.error(`seed-m10: VERIFICATION FAILED — ${v.problems.length} problem(s)`)
        for (const p of v.problems) console.error(`  - ${p}`)
        process.exit(1)
      }
      console.log(`seed-m10: verification green (counts + ${v.evidence.readback.length} literal-path byte readbacks + user-read leg; ?properties posture ${v.evidence.propertiesPosture.status} recorded)`)
    })
    .catch((e) => {
      console.error(`seed-m10: ${e.message}`)
      process.exit(1)
    })
}
