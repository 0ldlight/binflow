import { fileURLToPath } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// BinFlow console build (ADR-0014 decision 4 + T-108 errata R1).
//
// base is the SPA's mount segment /binflow/ui/** (PRD FR-23): the React
// router basename (src/main.tsx today; src/app/config UI_BASE for the
// rewrite foundation) MUST stay in sync with this value, and the Go handler
// in internal/console mounts the same segment plus the shared
// fingerprinted-asset mount /binflow/assets/** (assets deliberately live
// OUTSIDE the ui segment so the segment-internal history fallback never has
// to special-case file paths).
//
// scripts/relink-assets.mjs, run right after this build, rewrites the
// /binflow/ui/assets/ URLs emitted into index.html and CSS onto that shared
// /binflow/assets/ mount (see the script header for the why-and-upgrade).
//
// Frontend rewrite P1 additions (docs/design/frontend-rewrite-architecture):
// - @tailwindcss/vite: compiles the new styles layer (src/styles/tw/) when
//   it enters the module graph. The old MUI app's CSS is untouched — the
//   plugin only processes CSS that imports tailwindcss, so the legacy build
//   output is byte-identical until P2 wires the new entry.
// - '@' alias mirrors tsconfig paths (shadcn components.json convention).
export default defineConfig({
  base: '/binflow/ui/',
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    target: 'es2020',
  },
  server: {
    port: 5173,
    // Dev proxy: the dev server front-ends a locally running binflow-server
    // (API + auth stay on the real binary; only the SPA is served live).
    //
    // Whole /binflow prefix → 8080 (architecture §9: fixes the dev blind
    // spot for the event face /binflow/event/api/v1 and the content face
    // /binflow/{repo}/{path}, which previously 404'd against the dev
    // server). The SPA's own segment is carved back out via bypass so vite
    // keeps serving the live bundle and HMR: /binflow/ui/** stays local.
    // Note: a content repo literally named "ui" is unreachable in dev
    // (shadowed by the SPA segment); prod embed is unaffected.
    proxy: {
      '/binflow': {
        target: 'http://127.0.0.1:8080',
        bypass: (req) => {
          const url = req.url ?? ''
          if (url === '/binflow/ui' || url.startsWith('/binflow/ui/') || url.startsWith('/binflow/ui?')) {
            return url
          }
        },
      },
    },
  },
})
