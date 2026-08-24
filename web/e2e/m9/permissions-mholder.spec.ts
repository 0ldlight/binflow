import { test } from '@playwright/test'

// T-250 skeleton (M9 B1): this placeholder reserves the permissions-mholder
// slot in the m9 playwright project; the body lands with its tickets. Listed
// via `npx playwright test --project=m9 --list` — no assertions yet, by design.
//
// Fills with:
//   T-254 (E9/E6) auth.ManageCoverage seam + GET /api/v1/permissions
//     ?filter=manage — u9 (seed: manage on t-in via m9-r00/m9-r01, read-only
//     t-out) lists t-in with zero t-out leakage; the no-filter branch stays
//     403 for u9 (closed set unchanged — PRD N07, the additive-only gate)
//   T-259 m-holder console reachability — u9 deep-links the permission list
//     (page renders, not the L2 boundary card) and edits t-in; t-out
//     POST/DELETE stay 403 (the T-241 B1 leg re-run)

test.skip('permissions-mholder: filter=manage coverage list + console editor reachability (fills with T-254/T-259)', () => {
  // Placeholder body intentionally empty — T-250 lands only the skeleton.
})
