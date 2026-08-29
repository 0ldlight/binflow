# BinFlow Console (web/)

The web console's build domain (ADR-0014 decision 4 + the T-108 errata):
vite + React + TypeScript, mounted at `/binflow/ui/**` with fingerprinted
assets on the shared `/binflow/assets/**` mount.

## Commands

| Command | What it does |
|---|---|
| `make console` (repo root) | `npm ci` (lockfile-pinned) → `vite build` → copy `dist/` into `internal/console/dist` → report the gzipped SPA payload (5MB budget, W37). Rebuild the binary afterwards to embed the fresh bundle. |
| `npm run dev` | vite dev server (port 5173) proxying `/binflow/api` and `/binflow/assets` to a local `binflow-server` on :8080. |
| `npm run typecheck` | `tsc --noEmit` (CI gate). |
| `npm run lint` | eslint flat config (CI gate). |
| `npm run e2e` | Playwright (Chromium); `BASE=http://host:port npm run e2e` targets a running server. Browsers: `npx playwright install chromium`. |

## Layout

- `src/` — the SPA. `main.tsx` holds the router (`basename=/binflow/ui`, must
  equal vite's `base`); every screen is `React.lazy` (the route-chunk seam:
  later tickets add screens without touching the build shape).
- `scripts/relink-assets.mjs` — runs after `vite build`; rewrites the emitted
  `/binflow/ui/assets/` references onto `/binflow/assets/` (the shared mount
  keeps the ui segment pure: every in-segment path is the shell, so the
  Go-side history fallback stays dumb). See its header for the upgrade path.
- `e2e/` — Playwright specs. The smoke spec skips with a reason until the
  router mounts the segment (T-91); the console handler itself is
  fallback-complete (unit-pinned in `internal/console`).
- `../internal/console/dist/` — the embed target. `placeholder.html` is
  COMMITTED so a node-less checkout builds; everything `make console` copies
  there is gitignored.

## Notes

- This directory is NOT part of the Go module: `make test`/`make vet`
  filter `/web/` out of the package list because `node_modules` vendors
  third-party Go files. The JS/TS gates are eslint + tsc (same CI).
- Direct runtime dependencies are deliberately minimal (react,
  react-dom, react-router-dom); changes to that list go through the
  architect (ADR-0014 decision 4's third rule).

## E2E flake protocol (T-350 / FR-113.6, K46)

The load-flake family (axe-sweep and polling-budget legs straggling under
colocated load — T-263/T-268/T-300/T-327/T-329 evidence rounds) is adjudicated
by ENVIRONMENT, not by timeout raises:

- **CI is the authority.** The `e2e` job in `.github/workflows/ci.yml`
  (main pushes) runs the whole three-project suite on a dedicated runner
  (`--workers=2`) against a freshly booted, freshly seeded instance. A red
  there is a product/test finding; a green there is the authoritative green.
- **Local runs use "serial rerun green = pass".** On a shared dev machine
  (parallel agents, builds, other e2e instances), a budget red that passes
  `npx playwright test --workers=1 <spec>` in isolation is an isolation
  finding — log it, don't chase it, don't raise the timeout (raises mask the
  starvation signal the budget exists to surface).
- **The fallback (not landed, by design):** if a CI runner ever proves
  unavailable, the silence-window spec fix is the next lever — a
  `page.waitForLoadState('networkidle')` quiet window between `loginAs` and
  tracker attachment (the N01 straddle shape, T-327 §7). K46 took
  "runner first, silence window as fallback"; the runner landed, so the spec
  surgery stays holstered.

