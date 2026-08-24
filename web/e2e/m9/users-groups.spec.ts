import { test } from '@playwright/test'

// T-250 skeleton (M9 B1): this placeholder reserves the users-groups slot in
// the m9 playwright project; the body lands with its tickets. Listed via
// `npx playwright test --project=m9 --list` — no assertions yet, by design.
//
// Fills with:
//   T-251 (E2/E3/E4) users domain endpoints — widened list echo (email /
//     adminRole / enabled / groups), per-user enabled echo, DELETE cascade
//     with the 400 guardrails (PRD N01/N02/N03; u9 is the fixture the PRD
//     N-sequence rides, password pw-u9-123 from the seed)
//   T-252 (E5) group members ?includeUsers=true (PRD N04)
//   T-257 users/groups pages consume the widened echo — the N+1 retirement:
//     <= 2 requests per page on the 20 users / 10 groups seed

test.skip('users-groups: widened echo, DELETE guardrails, includeUsers fanout (fills with T-251/T-252/T-257)', () => {
  // Placeholder body intentionally empty — T-250 lands only the skeleton.
})
