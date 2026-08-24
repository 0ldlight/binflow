import { test } from '@playwright/test'

// T-250 skeleton (M9 B1): this placeholder reserves the oidc-stepup slot in
// the m9 playwright project; the body lands with its ticket. Listed via
// `npx playwright test --project=m9 --list` — no assertions yet, by design.
//
// Fills with:
//   T-260 (FR-81) OIDC step-up console leg — the mint-grant consumption face
//     over the ADR-0027 contract. Needs the armed instance posture
//     (BINFLOW_AUTH__TOKEN_STEP_UP=true + a mock IdP, the T-242 §1.5-4 probe
//     pattern): grant single-use replay -> 401 step_up_invalid, TTL <= 3600,
//     no grant plaintext in the access log (PRD N10/N11, NFR-S51).

test.skip('oidc-stepup: console consumes a single-use step-up grant on the armed instance (fills with T-260)', () => {
  // Placeholder body intentionally empty — T-250 lands only the skeleton.
})
