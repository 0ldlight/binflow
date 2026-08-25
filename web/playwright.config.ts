import { defineConfig, devices } from '@playwright/test'

// Playwright harness (T-89 AC2). Chromium only (the console's supported
// matrix), baseURL from $BASE so the same spec runs against a local dev
// binary (default) or a QA instance — the smoke.sh convention (T-16).
//
// The harness does NOT autostart the server: `make build && ./bin/binflow-server serve`
// first (or point BASE at an already-running instance), then
// `cd web && npx playwright test`. CI wiring for the browsers lands with the
// QA tickets; this config is the stable seam.
//
// Three projects: `chromium` = the M1~M8 suite (everything outside e2e/m9/
// and e2e/m10/), `m9` = the M9 milestone legs (T-250 skeleton), `m10` = the
// M10 milestone legs (T-277 skeleton; select with --project=m10).
// A bare `npx playwright test` runs all three — every spec exactly once.
//
// Concurrency (T-268, FR-80-AC2/AC3): default parallelism is RESTORED — the
// T-232 --workers=1 stopgap is retired. Its root cause (gc graceHours=0
// racing parallel writers) is fixed by ADR-0031 and gated in CI by the GC
// stress step (ci.yml "GC concurrency stress"); that step and this parallel
// posture are coupled — see the note there. The workers count itself is
// pinned in the config body (AC floor 4; rationale inline).
export default defineConfig({
  testDir: './e2e',
  // Per-test budget is the CONCURRENCY-era 180s (T-268): the serial-era 30s
  // assumed an exclusive machine. axe legs run axe-core inside page.evaluate —
  // under default parallelism (8 chromium workers sharing the box) they
  // measured 33s+ per scan and blew both the 30s default and one spec's
  // self-set 90s (T-263 registered the flake; T-268 reproduced it 2/2 rounds
  // before this raise). The axe-heavy sweep legs keep their own 300s
  // (a11y-sweep/helpers) and keyboard keeps an explicit 180s. A real hang now
  // costs 3 minutes in one worker, not a false red under load.
  timeout: 180_000,
  fullyParallel: true,
  // Restored default concurrency, pinned at the AC floor (FR-80-AC2 ">=4
  // workers", T-268). The bare `npx playwright test` needs no --workers flag;
  // 4 is deterministic across machines (Playwright's cpu/2 default would give
  // 2 on a 4-vCPU box — below the floor — and 8 on this dev box, where it
  // STARVED the CPU-bound axe legs: this suite is not the I/O-bound web suite
  // cpu/2 assumes; axe-core runs in-page, and 8 chromium workers on 8 physical
  // cores pushed the 52-scan sweep past 8 minutes — evidence rounds in
  // reports/agents/T-268.md). 4 workers = every axe leg inside budget with
  // 3 consecutive green rounds.
  workers: 4,
  // Fail-fast BASE ownership probe (T-266 registered leftover, landed T-268):
  // verifies $BASE is a live BinFlow instance BEFORE any worker starts, so a
  // server that failed to boot cannot silently redirect the whole parallel
  // suite at whoever owns the port.
  globalSetup: './e2e/support/base-probe.ts',
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: process.env.BASE ?? 'http://127.0.0.1:8080',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
      // The M9 suite (T-250) and the M10 suite (T-277) live in their own
      // projects so in-flight milestone legs run selectively
      // (`npx playwright test --project=m9` / `--project=m10`); the base
      // project keeps the M1~M8 suite unchanged and never double-runs the
      // m9/ or m10/ directories.
      testIgnore: /(^|\/)(m9|m10)\//,
    },
    {
      name: 'm9',
      use: { ...devices['Desktop Chrome'] },
      testMatch: /(^|\/)m9\/.*\.spec\.ts$/,
    },
    {
      name: 'm10',
      use: { ...devices['Desktop Chrome'] },
      testMatch: /(^|\/)m10\/.*\.spec\.ts$/,
    },
  ],
})
