import { expect } from '@playwright/test'
import type { Locator, Page } from '@playwright/test'

// T-232: clipboard assertion helper (ADR-0029 decision 3 — "点击路径可达" is
// an INTERACTION assertion, and copy buttons are part of the M8 operation
// flows: checksum copy in the artifact detail, repo-key copy, command blocks
// in Set Me Up). Chromium grants clipboard-read/clipboard-write to the
// context, so the assertion reads back what the UI actually wrote — the
// full value, never the truncated display form (console-ux §7.3).

/** Grant clipboard permissions for this context (Chromium only — the
 * supported matrix; other engines make copy legs skip). */
export async function grantClipboard(page: Page): Promise<boolean> {
  const caps = page.context().browser()?.browserType().name() ?? ''
  if (caps !== 'chromium') return false
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  return true
}

/** Click `trigger` (a copy control) and assert the OS clipboard holds exactly
 * `expected`. The read happens in the page AFTER the click, so async
 * writeText implementations are covered by Playwright's await semantics. */
export async function expectCopied(page: Page, trigger: Locator, expected: string): Promise<void> {
  if (!(await grantClipboard(page))) {
    throw new Error('clipboard assertions require chromium (the supported matrix)')
  }
  await trigger.click()
  await expect
    .poll(async () =>
      page.evaluate(async () => {
        try {
          return await navigator.clipboard.readText()
        } catch {
          return null // permission not effective yet — retry within the poll
        }
      }),
    )
    .toBe(expected)
}
