import { expect, test } from '@playwright/test'

import {
  M10_PLAN,
  fixtureBody,
  legacyFixtures,
  legacyRepos,
  seedM10,
  verifyM10,
} from './support/seed'
import { m10Client } from './support/seed'

// T-286 fill (M10 B4, FR-89 BE): the properties-system legs L18/L19/L20/
// L22 (L21 is the FE ticket's Properties Tab). Assertion posture per
// e2e/m10/README §2.3 — deploy-and-readback accounting against the REAL
// content/REST plane; no UI, no pixel claims.
//
// Grammar anchors (ADR-0033 / architecture §15.3):
//   - matrix peel: paired ";k=v" trailing sequence strips; non-paired ';'
//     keeps the M1 literal path (decision 11.39)
//   - GET body: {"properties":{...}} (§15.3.3's table shape — the README
//     §2.3 inline example shows the unwrapped map; §15.3 is the authority
//     and this spec asserts the wrapped form)
//   - PUT merge: same-key value-set replace, other keys kept (§11.40)
//   - DELETE: selective keys, `*` wildcard, `properties=*` all; 204s are
//     idempotent

const BASE = process.env.BASE ?? 'http://127.0.0.1:8080'

/** Authenticated raw fetch — the legs NEED non-2xx statuses, so unlike
 * makeClient this never throws on them. */
async function call(
  method: string,
  path: string,
  { user = 'admin', password = process.env.ADMIN_PW ?? 'password', body }: {
    user?: string
    password?: string
    body?: string
  } = {},
) {
  const auth = Buffer.from(`${user}:${password}`).toString('base64')
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers: {
      Authorization: `Basic ${auth}`,
      ...(body !== undefined ? { 'Content-Type': 'application/octet-stream' } : {}),
    },
    body,
  })
  return { status: res.status, text: await res.text() }
}

test.beforeAll(async () => {
  // The seed is the FR-89-AC4 carrier: two legacy repos, role users, the
  // read grant and the five non-paired ';' fixtures — all through the real
  // plane, idempotent on re-run.
  await seedM10(m10Client())
})

test('L18 — matrix-parameter deploy strips and stores properties', async () => {
  const repo = M10_PLAN.repoGeneric
  const r = await call('PUT', `/binflow/${repo}/ci/app.bin;build=77;env=prod`, { body: 'l18-bytes' })
  expect(r.status).toBe(201)

  // The artifact is addressable at the CLEAN path with the exact bytes.
  const clean = await call('GET', `/binflow/${repo}/ci/app.bin`)
  expect(clean.status).toBe(200)
  expect(clean.text).toBe('l18-bytes')

  // The properties read back (§15.3's wrapped body shape).
  const props = await call('GET', `/binflow/api/storage/${repo}/ci/app.bin?properties=build,env`)
  expect(props.status).toBe(200)
  expect(JSON.parse(props.text)).toEqual({
    properties: { build: ['77'], env: ['prod'] },
  })

  // The .info detail body renders the same set additively (FR-89-AC1).
  const info = await call('GET', `/binflow/api/storage/${repo}/ci/app.bin`)
  expect(info.status).toBe(200)
  expect(JSON.parse(info.text).properties).toEqual({ build: ['77'], env: ['prod'] })
})

test('L19 — REST read/write/delete family', async () => {
  const repo = M10_PLAN.repoGeneric
  const base = `/binflow/api/storage/${repo}/l19/app.bin`
  const dep = await call('PUT', `/binflow/${repo}/l19/app.bin;build=77;build2=x`, { body: 'l19' })
  expect(dep.status).toBe(201)

  // PUT with the comma pair grammar (PRD's exact acceptance shape).
  expect((await call('PUT', `${base}?properties=qa=passed,owner=team-a`)).status).toBe(204)
  let props = JSON.parse((await call('GET', `${base}?properties`)).text).properties
  expect(props.qa).toEqual(['passed'])
  expect(props.owner).toEqual(['team-a'])
  expect(props.build).toEqual(['77'])

  // Merge: same-key value-set replace (multi-value via continuation),
  // other keys kept.
  expect((await call('PUT', `${base}?properties=qa=failed,v2`)).status).toBe(204)
  props = JSON.parse((await call('GET', `${base}?properties=qa,owner`)).text).properties
  expect(props.qa).toEqual(['failed', 'v2'])
  expect(props.owner).toEqual(['team-a'])

  // Wildcard read.
  const wild = JSON.parse((await call('GET', `${base}?properties=build*`)).text).properties
  expect(Object.keys(wild).sort()).toEqual(['build', 'build2'])

  // atomic=true: a missing named key is the 404.
  expect((await call('GET', `${base}?properties=build,nope&atomic=true`)).status).toBe(404)
  expect((await call('GET', `${base}?properties=build,qa&atomic=true`)).status).toBe(200)

  // Folder + recursive=1: the folder row and every node under it.
  const folder = `/binflow/api/storage/${repo}/l19`
  expect((await call('PUT', `/binflow/${repo}/l19/sub/inner.bin`, { body: 'inner' })).status).toBe(201)
  expect((await call('PUT', `${folder}?properties=release=done&recursive=1`)).status).toBe(204)
  for (const p of [folder, `${base}`, `${folder}/sub/inner.bin`]) {
    const got = JSON.parse((await call('GET', `${p}?properties=release`)).text).properties
    expect(got.release, `${p} must carry release`).toEqual(['done'])
  }
  expect((await call('DELETE', `${folder}?properties=release&recursive=1`)).status).toBe(204)

  // Selective delete keeps siblings; delete-all empties; idempotent.
  expect((await call('DELETE', `${base}?properties=qa`)).status).toBe(204)
  props = JSON.parse((await call('GET', `${base}?properties`)).text).properties
  expect(props.qa).toBeUndefined()
  expect(props.owner).toEqual(['team-a'])
  expect((await call('DELETE', `${base}?properties=*`)).status).toBe(204)
  expect((await call('DELETE', `${base}?properties=*`)).status).toBe(204)
  expect(JSON.parse((await call('GET', `${base}?properties`)).text).properties).toEqual({})
})

test('L20 — validation arms (matrix + REST, 400s)', async () => {
  const repo = M10_PLAN.repoGeneric

  // Matrix arm: the illegal key refuses the deploy outright and the
  // artifact never lands.
  const bad = await call('PUT', `/binflow/${repo}/l20/refused.bin;bad%20key=1`, { body: 'x' })
  expect(bad.status).toBe(400)
  expect((await call('GET', `/binflow/${repo}/l20/refused.bin`)).status).toBe(404)

  // REST arm: illegal key on the write plane.
  const dep = await call('PUT', `/binflow/${repo}/l20/app.bin`, { body: 'l20' })
  expect(dep.status).toBe(201)
  expect((await call('PUT', `/binflow/api/storage/${repo}/l20/app.bin?properties=bad+key=1`)).status).toBe(400)
  // Empty delete spec is the reference plane's explicit 400.
  expect((await call('DELETE', `/binflow/api/storage/${repo}/l20/app.bin?properties=`)).status).toBe(400)
  // Unknown node is the 404 envelope.
  expect((await call('GET', `/binflow/api/storage/${repo}/l20/nope.bin?properties=k`)).status).toBe(404)
})

test('L22 — legacy semicolon regression (seed-m10 fixtures, FR-89-AC4)', async () => {
  // verifyM10's readback leg IS the hard gate: every fixture GETs 200 at
  // its ORIGINAL literal path, byte-identical, post-FR-89.
  const v = await verifyM10(m10Client())
  expect(v.problems, v.problems.join('; ')).toEqual([])
  for (const row of v.evidence.readback) {
    expect(row.status).toBe(200)
    expect(row.bytes).toBe(true)
  }
  // The E-09 flip landed: ?properties now answers 200 (404 was the
  // pre-FR-89 posture the seed recorded as evidence).
  expect(v.evidence.propertiesPosture.status).toBe(200)

  // Direct re-assertion of every fixture's literal readback (belt and
  // braces — the README §2.3 idiom verbatim).
  for (const fx of legacyFixtures()) {
    const r = await call('GET', `/binflow/${fx.repo}/${fx.path}`)
    expect(r.status).toBe(200)
    expect(r.text).toBe(fixtureBody(fx.repo, fx.path))
  }
  // Both legacy repos exist and the plane stays reachable.
  expect((await call('GET', '/binflow/api/repositories')).status).toBe(200)
  expect(legacyRepos().length).toBe(2)

  // A paired-lookalike name is the feature, not legacy surface: the
  // deploy strips and the suffix re-GET re-peels to the same node.
  const repo = M10_PLAN.repoGeneric
  expect((await call('PUT', `/binflow/${repo}/legacy/app.bin;build=77`, { body: 'paired' })).status).toBe(201)
  const again = await call('GET', `/binflow/${repo}/legacy/app.bin;build=77`)
  expect(again.status).toBe(200)
  expect(again.text).toBe('paired')
})
