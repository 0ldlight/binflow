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
// Two projects: `chromium` = the M1~M8 suite (everything outside e2e/m9/),
// `m9` = the M9 milestone legs (T-250 skeleton; select with --project=m9).
// A bare `npx playwright test` runs both — every spec exactly once.
export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  fullyParallel: true,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: process.env.BASE ?? 'http://127.0.0.1:8080',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
      // The M9 suite (T-250) lives in its own project so in-flight milestone
      // legs run selectively (`npx playwright test --project=m9`); the base
      // project keeps the M1~M8 suite unchanged and never double-runs the
      // m9/ directory.
      testIgnore: /(^|\/)m9\//,
    },
    {
      name: 'm9',
      use: { ...devices['Desktop Chrome'] },
      testMatch: /(^|\/)m9\/.*\.spec\.ts$/,
    },
  ],
})
