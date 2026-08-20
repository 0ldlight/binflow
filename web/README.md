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
