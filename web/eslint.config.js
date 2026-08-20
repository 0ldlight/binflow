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
  {
    files: ['**/*.{ts,tsx}'],
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
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
)
