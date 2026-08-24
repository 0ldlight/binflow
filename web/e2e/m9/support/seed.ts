import {
  M9_PLAN,
  ensureGroup,
  ensureM9User,
  ensureTarget,
  groupNames,
  groupsForUser,
  m9Targets,
  repoKeys,
  seedM9,
  userNames,
  userPassword,
  usageSeedBody,
  verifyM9,
} from '../../../scripts/seed-m9.mjs'
import { makeClient } from '../../../scripts/seed-m8.mjs'

// T-250: seeding surface for M9 specs. The implementation lives in
// web/scripts/seed-m9.mjs (also the standalone CLI); this module re-exports it
// for the TS world and adds the in-spec idiom the M8 suite established:
//   m9Client()  admin REST client from the same env the harness uses
// (BASE / ADMIN_USER / ADMIN_PW — the smoke.sh convention).
// usageSeedBody re-exported since T-258 (the usage-fanout spec computes its
// expected usedBytes oracle from the same deterministic body the seed PUTs).

export {
  M9_PLAN,
  ensureGroup,
  ensureM9User,
  ensureTarget,
  groupNames,
  groupsForUser,
  m9Targets,
  repoKeys,
  seedM9,
  userNames,
  userPassword,
  usageSeedBody,
  verifyM9,
}

/** Admin client pointed at the same base the Playwright harness targets
 * (config default http://127.0.0.1:8080, BASE overridable). */
export function m9Client() {
  return makeClient({
    base: process.env.BASE ?? 'http://127.0.0.1:8080',
    username: process.env.ADMIN_USER ?? 'admin',
    password: process.env.ADMIN_PW ?? process.env.ADMIN_PASSWORD ?? 'password',
  })
}
