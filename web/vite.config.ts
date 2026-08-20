import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// BinFlow console build (ADR-0014 decision 4 + T-108 errata R1).
//
// base is the SPA's mount segment /binflow/ui/** (PRD FR-23): the React
// router basename in src/main.tsx MUST stay in sync with this value, and the
// Go handler in internal/console mounts the same segment plus the shared
// fingerprinted-asset mount /binflow/assets/** (assets deliberately live
// OUTSIDE the ui segment so the segment-internal history fallback never has
// to special-case file paths).
//
// scripts/relink-assets.mjs, run right after this build, rewrites the
// /binflow/ui/assets/ URLs emitted into index.html and CSS onto that shared
// /binflow/assets/ mount (see the script header for the why-and-upgrade).
export default defineConfig({
  base: '/binflow/ui/',
  plugins: [react()],
  build: {
    outDir: 'dist',
    sourcemap: false,
    target: 'es2020',
  },
  server: {
    port: 5173,
    // Dev proxy: the dev server front-ends a locally running binflow-server
    // (API + auth stay on the real binary; only the SPA is served live).
    proxy: {
      '/binflow/api': 'http://127.0.0.1:8080',
      '/binflow/assets': 'http://127.0.0.1:8080',
    },
  },
})
