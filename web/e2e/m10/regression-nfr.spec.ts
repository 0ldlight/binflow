import { test } from '@playwright/test'

// T-277 skeleton (M10 B0): regression + NFR closing slot.
//
// Legs (PRD §5.5):
//   L28 M1~M9 full P0 rerun on the NO-LICENSE instance + server contract
//      change surface 100% attributed to M10 exemption tickets
//      (git diff m9-done..HEAD -- internal/ cmd/)
//   L29 NFR: gate hot-path P95 (vs the M7 6.8ms / M8 0.606ms / M9 archived
//      baselines, <10% drift), license switch <=1s, 100-concurrent
//      go mod download zero 5xx, matrix-param PUT throughput drift <10%
//   L30 cold start <2s / idle RSS <100MB (license load + registry + props
//      table must not break the baseline)
//
// The no-license==m9-done machine proof ALREADY runs today (T-277):
//   make test-m10-invariant
// (RBAC contract baseline EXPECT=1 + the m10 community posture). L28's full
// P0 rerun is the QA closing pass over the same invariant.

test.skip('regression + NFR close: M1~M9 P0 rerun (no license) + gate hot-path/switch/concurrency/cold-start (fills with the QA closing ticket)', () => {
  // Placeholder body intentionally empty — T-277 lands only the skeleton.
})
