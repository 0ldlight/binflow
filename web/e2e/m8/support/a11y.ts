import AxeBuilder from '@axe-core/playwright'
import { expect } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'

// T-232: axe-core accessibility scan helper. ADR-0029 decision 3 keeps
// "样式断言 = computed style 对自有 token 的存在性校验" as the ONLY style-level
// gate; axe is the complementary STRUCTURAL accessibility gate (labels, roles,
// focus order primitives, contrast via computed tokens) and asserts behavior
// semantics, not pixels — consistent with the interaction-assertion regime.
//
// Defaults: serious + critical only, WCAG 2.x A/AA rules. Specs that want the
// full picture pass impact: null; violations are ALWAYS attached to the test
// output so a failure reads as a report, not a mystery.

export interface A11yOptions {
  /** Selector to scope the scan (default: full page). */
  include?: string
  /** Impacts to fail on (default serious + critical). null = all. */
  impact?: Array<'minor' | 'moderate' | 'serious' | 'critical'> | null
  /** Extra axe tags beyond the WCAG A/AA baseline. */
  tags?: string[]
}

const BASE_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice']

export async function scanA11y(page: Page, opts: A11yOptions = {}) {
  let builder = new AxeBuilder({ page }).withTags(opts.tags ?? BASE_TAGS)
  if (opts.include) builder = builder.include(opts.include)
  return builder.analyze()
}

/** Assert no violations at the chosen impact levels; attach the full report
 * (passes summary + violation details) to the test output either way. */
export async function expectA11yClean(page: Page, testInfo: TestInfo, opts: A11yOptions = {}): Promise<void> {
  const results = await scanA11y(page, opts)
  const impacts = opts.impact ?? ['serious', 'critical']
  const failing = results.violations.filter((v) => impacts === null || impacts.includes(v.impact ?? 'minor'))
  const report = JSON.stringify(
    {
      url: page.url(),
      impacts: impacts ?? 'all',
      violations: results.violations.map((v) => ({
        id: v.id,
        impact: v.impact,
        help: v.help,
        nodes: v.nodes.slice(0, 5).map((n) => n.target),
      })),
      passes: results.passes.length,
      incomplete: results.incomplete.length,
    },
    null,
    2,
  )
  await testInfo.attach('a11y-report', { body: report, contentType: 'application/json' })
  expect(
    failing.map((v) => `${v.id}(${v.impact})`),
    'accessibility violations — full report in the a11y-report attachment',
  ).toEqual([])
}
