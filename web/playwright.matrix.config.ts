import { defineConfig, devices } from '@playwright/test'

// T-104 browser-matrix harness: the base playwright.config.ts pins the
// supported matrix (chromium, per FR-33-AC1); this overlay adds the
// WebKit/Firefox observation projects (FR-33-AC4 — P2, record not gate).
// Scoped to the cross-browser chain spec so the full suite never silently
// widens its engine matrix:
//
//   npx playwright test e2e/t104-crossbrowser.spec.ts --config playwright.matrix.config.ts
export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: process.env.BASE ?? 'http://127.0.0.1:8080',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
  ],
})
