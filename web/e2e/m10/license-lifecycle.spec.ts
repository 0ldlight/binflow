import { test } from '@playwright/test'

// T-277 skeleton (M10 B0): this placeholder reserves the license-lifecycle
// slot in the m10 playwright project; the bodies land with their tickets.
// Listed via `npx playwright test --project=m10 --list` — no assertions yet,
// by design.
//
// Legs (PRD milestone-10 §5.5; wire paths per ADR-0032 §15.1.4 — /api/system/
// license, the PRD §4.1 spelling /api/v1/system/license lost the divergence
// ruling routed to T-293):
//   L01 keygen/issue self-check + tamper probe — `bf license generate/inspect`
//      (fills with the license core ticket; keygen toolchain = T-281)
//   L02 install/query/uninstall closed loop + audit license.install/delete
//      (fills with the license REST ticket)
//   L03 anti-forgery: tampered doc 400 / test-key doc 400 under the default
//      verify key / Artifactory path /api/system/licenses honest 400-404 (LC-02)
//   L04 no-license default: GET answers the community/none posture, five
//      package types smoke-green (the invariant this skeleton gates elsewhere)
//   L05 expiry: past-grace doc degrades to community while reads stay 200;
//      in-grace doc keeps the tier + binflow_license_expiry_days visible
//
// Multi-tier posture legs that need no browser run in scripts/m10-tier-matrix.sh
// (make test-m10-matrix); this spec carries the REST/lifecycle assertions.

test.skip('license lifecycle: keygen/install/query/uninstall/expiry (fills with the FR-84 tickets; keygen T-281)', () => {
  // Placeholder body intentionally empty — T-277 lands only the skeleton.
})
