import type { Locator, Page, TestInfo } from '@playwright/test'

// T-232: timing collection helpers (record-and-report, NOT a pass/fail gate —
// ADR-0029 decision 3 keeps M8 acceptance on interaction assertions; numbers
// are collected for trend comparison across milestones, the T-104/T-222
// precedent of record-only perf legs).
//
//   measureFirstInteractive  首屏可交互: navigation start -> ready anchor
//                            visible, plus the browser's own DCL/load marks.
//   measureTreeExpand        树层展开耗时: keyboard-expand a collapsed tree
//                            node -> first child row visible (the lazy
//                            one-level load of console-m8 §3.3 C2).

export interface FirstInteractive {
  /** Wall ms from just before goto() to the ready anchor being visible. */
  readyMs: number
  /** performance.getEntriesByType('navigation') marks, ms since nav start. */
  dclMs: number
  loadMs: number
  /** Longest task per the Long Tasks API when available (else -1). */
  longestTaskMs: number
}

/** Navigate cold (fresh context recommended) and time until `readySelector`
 * (a data-testid anchor) is visible. */
export async function measureFirstInteractive(page: Page, url: string, readySelector: string): Promise<FirstInteractive> {
  const t0 = Date.now()
  await page.goto(url, { waitUntil: 'domcontentloaded' })
  await page.waitForSelector(readySelector, { state: 'visible' })
  const readyMs = Date.now() - t0
  const nav = await page.evaluate(() => {
    const n = performance.getEntriesByType('navigation')[0] as PerformanceNavigationTiming | undefined
    return { dclMs: n ? Math.round(n.domContentLoadedEventEnd) : -1, loadMs: n ? Math.round(n.loadEventEnd) : -1 }
  })
  const longestTaskMs = await page.evaluate(
    () =>
      new Promise<number>((resolve) => {
        const entries = performance.getEntriesByType('longtask')
        if (entries.length > 0) {
          resolve(Math.round(Math.max(...entries.map((e) => e.duration))))
          return
        }
        // none recorded yet: watch briefly, then report -1 (absence)
        const po = new PerformanceObserver(() => {
          const all = performance.getEntriesByType('longtask')
          if (all.length > 0) resolve(Math.round(Math.max(...all.map((e) => e.duration))))
          else resolve(-1)
          po.disconnect()
        })
        po.observe({ entryTypes: ['longtask'] })
        setTimeout(() => {
          po.disconnect()
          resolve(-1)
        }, 50)
      }),
  )
  return { readyMs, ...nav, longestTaskMs }
}

export interface ExpandTiming {
  /** Wall ms from the expand key press to the first child row being visible. */
  expandMs: number
  /** Children rows rendered when the level settled (the fetched fan-out). */
  childRows: number
}

/** Keyboard-expand (ArrowRight, console-ux §8 tree semantics) a collapsed
 * `node` and time until its first child row appears. `childPrefixSelector`
 * selects the children layer, e.g. `[data-testid^="tree-node-perf/"]`. */
export async function measureTreeExpand(
  page: Page,
  node: Locator,
  childPrefixSelector: string,
): Promise<ExpandTiming> {
  await node.focus()
  const t0 = Date.now()
  await page.keyboard.press('ArrowRight')
  const children = page.locator(childPrefixSelector)
  await children.first().waitFor({ state: 'visible' })
  const expandMs = Date.now() - t0
  // let the level finish rendering whatever it fetched before counting
  await page.waitForTimeout(150)
  const childRows = await children.count()
  return { expandMs, childRows }
}

/** Attach a timing sample to the test output (JSON, one array entry per
 * call) and mirror it to stdout for quick reading. */
export async function recordTiming<T extends object>(
  testInfo: TestInfo,
  name: string,
  sample: T,
): Promise<void> {
  const line = JSON.stringify({ metric: name, ...sample })
  console.log(`[m8-timing] ${line}`)
  await testInfo.attach(`timing-${name}`, { body: `${line}\n`, contentType: 'application/json' })
}
