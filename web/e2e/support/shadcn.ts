/** shadcn/Radix Select 测试辅助（生产选择器不再是原生 <select>）。 */
import { expect, type Page } from '@playwright/test'

export async function selectShadcn(page: Page, selector: string, value: string | number): Promise<void> {
  await page.locator(selector).click()
  const wire = value === '' ? '' : String(value)
  await page.locator('[role="option"][data-value="' + wire + '"]').first().click()
}

export async function expectSelectValue(page: Page, selector: string, value: string | number): Promise<void> {
  await expect(page.locator(selector)).toHaveAttribute('data-value', value === '' ? '' : String(value))
}

export async function selectOptionValues(page: Page, selector: string): Promise<string[]> {
  await page.locator(selector).click()
  const values = await page.locator('[role="option"]').evaluateAll((els) =>
    els.map((el) => el.getAttribute('data-value') ?? ''),
  )
  await page.keyboard.press('Escape')
  return values
}
