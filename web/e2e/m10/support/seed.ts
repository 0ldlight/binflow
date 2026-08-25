import {
  M10_PLAN,
  ensureM10ReadGrant,
  ensureM10User,
  ensureRepo,
  fixtureBody,
  legacyFixtures,
  legacyRepos,
  seedM10,
  verifyM10,
} from '../../../scripts/seed-m10.mjs'
import { makeClient } from '../../../scripts/seed-m8.mjs'

// T-277: seeding surface for M10 specs. The implementation lives in
// web/scripts/seed-m10.mjs (also the standalone CLI); this module re-exports
// it for the TS world and adds the in-spec idiom the M8/M9 suites established:
//   m10Client()  admin REST client from the same env the harness uses
// (BASE / ADMIN_USER / ADMIN_PW — the smoke.sh convention).
// The legacy ';' fixtures are the FR-89-AC4 regression carrier: specs assert
// literal-path byte readback against fixtureBody(repo, path).

export {
  M10_PLAN,
  ensureM10ReadGrant,
  ensureM10User,
  ensureRepo,
  fixtureBody,
  legacyFixtures,
  legacyRepos,
  seedM10,
  verifyM10,
}

/** Admin client pointed at the same base the Playwright harness targets
 * (config default http://127.0.0.1:8080, BASE overridable). */
export function m10Client() {
  return makeClient({
    base: process.env.BASE ?? 'http://127.0.0.1:8080',
    username: process.env.ADMIN_USER ?? 'admin',
    password: process.env.ADMIN_PW ?? process.env.ADMIN_PASSWORD ?? 'password',
  })
}
