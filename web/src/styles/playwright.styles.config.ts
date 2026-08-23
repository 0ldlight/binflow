import { defineConfig, devices } from '@playwright/test'

// T-234 临时冒烟配置（area 纪律：styles 票不碰 web/e2e/ 目录结构——
// 那是 T-232 的地盘）。T-232 的 web/e2e/m8/ 基座合入后，同目录的
// theme-smoke.spec.ts 迁移过去并入其助手体系，本配置随之删除。
//
// 约定沿现役 playwright.config.ts：不自启服务，先
// `make console && make build && ./bin/binflow-server serve`（或把 BASE
// 指向已运行实例）再 `npx playwright test --config src/styles/playwright.styles.config.ts`。
export default defineConfig({
  testDir: '.',
  timeout: 30_000,
  fullyParallel: true,
  reporter: 'list',
  use: {
    baseURL: process.env.BASE ?? 'http://127.0.0.1:8080',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
