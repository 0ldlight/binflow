import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Suspense, lazy } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import './styles.css'

// Route table. Every screen is React.lazy — the lazy seam is deliberate
// (ADR-0014): each console page ships as its own fingerprinted chunk under
// /binflow/assets/, so later tickets add screens without touching this
// file's build shape. basename MUST equal vite's base (see vite.config.ts).
const Home = lazy(() => import('./pages/home'))

function RouteFallback() {
  return <div className="route-fallback">Loading…</div>
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter basename="/binflow/ui">
      <Suspense fallback={<RouteFallback />}>
        <Routes>
          <Route path="/" element={<Home />} />
        </Routes>
      </Suspense>
    </BrowserRouter>
  </StrictMode>,
)
