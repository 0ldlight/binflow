// Flat config (eslint 9). Kept deliberately small: JS recommended,
// typescript-eslint recommended, react-hooks. The console's lint gate runs in
// CI via `npm run lint` (T-89 AC2); styling rules land with the design
// system, not the scaffold.
import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'node_modules', 'playwright-report', 'test-results'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  // react-hooks v7 (T-432 seg 1): the manual plugins/rules block is replaced
  // by the plugin's own flat config. v7 ships exactly two flat tiers and BOTH
  // enable the compiler-derived rules at error — the BOARD's fallback tier
  // (flat.recommended) is not a milder set in v7 (delta vs recommended-latest
  // is only void-use-memo), so "downgrade to recommended" cannot clear the
  // pre-existing findings the way it could in v6.
  //
  // Ratchet (ticket clause 4, 37 findings > 30 budget): the five rules that
  // fire on existing INTENTIONAL patterns are demoted to 'warn' — lint stays
  // green (CI runs plain `eslint .`, warnings pass) while every finding stays
  // visible as the cleanup backlog. These are behavior-sensitive refactors
  // (latest-value ref mirrors with documented 401-race fixes, MUI anchorEl
  // refs, uncontrolled-holder escape hatches) that do NOT belong in a pure
  // toolchain ticket whose e2e gate expects zero behavior change:
  //   set-state-in-effect 25 · refs 8 · immutability 2 · purity 1 ·
  //   preserve-manual-memoization 1
  // All other compiler-derived rules (set-state-in-render, use-memo,
  // void-use-memo, static-components, error-boundaries, ...) stay at 'error'
  // — zero existing violations, enforced from day one.
  reactHooks.configs.flat['recommended-latest'],
  {
    files: ['**/*.{ts,tsx}'],
    rules: {
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/refs': 'warn',
      'react-hooks/immutability': 'warn',
      'react-hooks/purity': 'warn',
      'react-hooks/preserve-manual-memoization': 'warn',
    },
  },
  {
    // Build scripts are plain Node ESM: declare the handful of Node globals
    // they use instead of pulling the `globals` package for two names.
    files: ['scripts/**/*.mjs'],
    languageOptions: {
      globals: { URL: 'readonly', console: 'readonly', process: 'readonly' },
    },
  },
  {
    // e2e auxiliary processes (the T-366 script receiver): same posture as
    // the build scripts — plain Node ESM spawned by a spec, not bundled.
    files: ['e2e/**/*.mjs'],
    languageOptions: {
      globals: {
        URL: 'readonly',
        console: 'readonly',
        process: 'readonly',
        Buffer: 'readonly',
        setTimeout: 'readonly',
      },
    },
  },
)
