import { selectShadcn } from '../support/shadcn'

import { expect, test } from '@playwright/test'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-514 consumption face. The permission editor must retain the three wire
// buckets alongside concrete repositories, while Release Lifecycle exposes only
// faces the current backend can honestly service. In particular, no browser
// “create bundle” control is fabricated: the assembly endpoint is AQL-only and
// the signed store chain is not available.

test.describe.configure({ mode: 'serial' })

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

const createdUsers: string[] = []
const createdTargets: string[] = []
const createdRepos: string[] = []

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

test.afterAll(async () => {
  const client = m8Client()
  for (const target of createdTargets) {
    await client.request('DELETE', `/binflow/api/v1/permissions/${target}`).catch(() => undefined)
  }
  for (const user of createdUsers) {
    await client.request('DELETE', `/binflow/api/security/users/${user}`).catch(() => undefined)
  }
  for (const repo of createdRepos) {
    await client.request('DELETE', `/binflow/api/repositories/${repo}?deleteContent=true`).catch(() => undefined)
  }
})

async function seedRepoWithArtifact(repo: string, path: string): Promise<void> {
  const client = m8Client()
  expect(
    (await client.request('PUT', `/binflow/api/repositories/${repo}`, {
      body: { rclass: 'local', packageType: 'generic' },
    })).status,
  ).toBe(200)
  createdRepos.push(repo)
  expect(
    (await client.request('PUT', `/binflow/${repo}/${path}`, {
      raw: true,
      headers: { 'Content-Type': 'application/octet-stream' },
      body: `t514 artifact bytes ${repo}`,
    })).status,
  ).toBe(201)
}

async function createUser(name: string, password: string): Promise<void> {
  const client = m8Client()
  expect(
    (await client.request('PUT', `/binflow/api/security/users/${name}`, {
      body: { name, email: `${name}@example.com`, password, admin: false, groups: [] },
    })).status,
  ).toBe(201)
  createdUsers.push(name)
}

test('permission buckets and concrete repositories coexist on the wire', async ({ page }) => {
  await loginAs(page, 'admin')
  const repo = uniq('t514pr')
  const user = uniq('t514pu')
  const target = uniq('t514pt')
  await seedRepoWithArtifact(repo, 'rel/one.bin')
  await createUser(user, 't514-preset-pw')

  await page.goto('/binflow/ui/admin/security/permissions/new')
  await page.fill('[data-testid="perm-form-name"]', target)
  await page.click('[data-testid="perm-repo-add"]')
  await expect(page.locator('[data-testid="perm-buckets-note"]')).toBeVisible()
  for (const label of ['Any Local', 'Any Remote', 'Any Distribution']) {
    await expect(page.locator('[data-testid="transfer-available"]')).toContainText(label)
  }
  await page.check('[data-testid="perm-repo-pick-ANY LOCAL"]')
  await page.check('[data-testid="perm-repo-pick-ANY REMOTE"]')
  await page.check('[data-testid="perm-repo-pick-ANY DISTRIBUTION"]')
  await page.check(`[data-testid="perm-repo-pick-${repo}"]`)
  await page.click('[data-testid="perm-res-step-2"]')
  await page.click('[data-testid="perm-res-ok"]')
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText('ANY DISTRIBUTION')
  await expect(page.locator('[data-testid="perm-repos"]')).toContainText(repo)

  await selectShadcn(page, '[data-testid="perm-add-user"]', user)
  await page.getByRole('button', { name: '添加用户' }).click()
  await page.check(`[data-testid="perm-matrix-cell-user-${user}-read"]`)
  const postedRepos: string[] = []
  page.on('request', (req) => {
    if (req.method() === 'POST' && req.url().includes('/api/v1/permissions')) {
      postedRepos.push(...((req.postDataJSON() as { repos?: string[] }).repos ?? []))
    }
  })
  await page.click('[data-testid="perm-save"]')
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="toast"]').filter({ hasText: `permission target ${target} 已保存` })).toBeVisible()
  expect([...postedRepos].sort()).toEqual(['ANY DISTRIBUTION', 'ANY LOCAL', 'ANY REMOTE', repo].sort())
  createdTargets.push(target)
})

test('release lifecycle remains a real read face without a fabricated write entry', async ({ page }, testInfo) => {
  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/artifacts')
  const navEntry = page.locator('[data-testid="nav-entry-release-lifecycle"]')
  await expect(navEntry).toBeVisible()
  await navEntry.click()
  await expect(page).toHaveURL('/binflow/ui/release-lifecycle')
  await expect(page.locator('[data-testid="bundles-page"] h2')).toHaveText('发布生命周期')
  await expect(page.locator('[data-testid="bundle-create"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="bundle-create-empty"]')).toHaveCount(0)

  const bogus = uniq('t514-no-match')
  await page.fill('[data-testid="bundles-search"]', bogus)
  await expect(page.locator('[data-testid="bundles-empty"], [data-testid="bundles-empty-filtered"]')).toBeVisible()
  await expectA11yClean(page, testInfo, { include: '[data-testid="bundles-page"]' })
})

test('plain user sees the same read-only release lifecycle entry', async ({ page }) => {
  await loginAs(page, 'user')
  await page.goto('/binflow/ui/release-lifecycle')
  await expect(page.locator('[data-testid="bundles-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="bundle-create"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="bundles-search"]')).toBeEnabled()
})

test('unknown release bundle version keeps the honest not-found face', async ({ page }) => {
  await loginAs(page, 'admin')
  const bogus = uniq('t514-nope')
  await page.goto(`/binflow/ui/release-bundles/${encodeURIComponent(bogus)}`)
  const honestFace = page.locator('[data-testid="bundle-not-found"], [data-testid="bundle-denied"]')
  await expect(honestFace).toBeVisible()
})
