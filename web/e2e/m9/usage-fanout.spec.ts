import { test } from '@playwright/test'

// T-250 skeleton (M9 B1): this placeholder reserves the usage-fanout slot in
// the m9 playwright project; the body lands with its tickets. Listed via
// `npx playwright test --project=m9 --list` — no assertions yet, by design.
//
// Fills with:
//   T-253 (E1) GET /api/v1/storage/usage batch endpoint — seed on
//     web/scripts/seed-m9.mjs (50 repos / 20 users / 10 groups; u8 holds
//     read on 10 of the 50 via t-u8-r — the response-subset leg, PRD N06)
//   T-258 repos list page hydrates the used-bytes column in <= 3 first-screen
//     requests (PRD: management-plane fanout, ~171 -> <= 3, request counting)

test.skip('usage fanout: batch usage endpoint + repos page single-request hydration (fills with T-253/T-258)', () => {
  // Placeholder body intentionally empty — T-250 lands only the skeleton.
})
