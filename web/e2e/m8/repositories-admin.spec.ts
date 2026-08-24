import { expect, test } from '@playwright/test'

import { expectA11yClean } from './support/a11y'
import { loginAs } from './support/roles'
import { m8Client, sessionApi } from './support/seed'

// T-240（FR-73 / console-m8 §6.6~§6.8 / UI-08/09）：仓库管理域重排的
// 交互断言——三 Tab 列表 / 包类型网格建仓向导 / 单页分区表单 / 详情三
// Tab（概要/配置/Replications）+ quota 行内编辑（CanManageRepo 语义）/
// 删仓强确认（输入 key + deleteContent 两段流）/ readonly 与 m-holder 腿
// （T-218 仓库域债随本票收口）。
//
// 断言口径 = ADR-0029 决策 3（交互断言制；锚 = data-testid，不随路由改名）。
// 锚源：console-ux §10.2 仓库组冻结锚（repos-*/repo-*/form-*）+ 本票新增
// （repos-tab-*/repos-sort-*/repos-delete-<key>/repos-pager/pkg-grid*/
// form-{reset,cancel}/repo-form-readonly-note/repo-tab-*/repo-quota-*/
// repo-repl-{degraded,goto}/repo-edit-link/repo-goto-tree/
// repo-manage-note/repo-detail-readonly-note）。退役：repos-filter-{type,
// package}（Tab 化）、form-prev/form-next（单页分区化）——§10 已回写 v1.6。
//
// 门语义（router.go / rbac.go 实测）：GET /api/repositories = CapRepoRead
// （admin + readonly_admin 全量，普通 user 403）；GET /{key} = CanManageRepo
// 读臂（普通 user 持该仓 m 动作才过）；POST /{key} = 写臂（readonly 拒）；
// DELETE / PUT-create-臂 = CapRepoWrite（仅全量 admin）。
//
// 运行前提与 m8 README §1 相同：make console && make build 后真二进制前台
// serve，BASE 指向它；角色种子经 loginAs 幂等预备。

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
})

// ---- 1. 建仓向导全链（网格 → 分区表单 → 创建 → 对账） --------------------

test('admin: package-type grid wizard full chain (?rclass= preset, combo gating, create + reconcile)', async ({
  page,
}, testInfo) => {
  await loginAs(page, 'admin')
  const key = uniq('t240rz')

  // Quick 建仓入口形态：?rclass=remote 预选仓型；网格 docker 项禁用（FR-15-AC7）
  await page.goto('/binflow/ui/admin/repositories/new?rclass=remote')
  await expect(page.locator('[data-testid="pkg-grid"]')).toBeVisible()
  await expect(page.locator('[data-testid="pkg-grid-item-docker"]')).toBeDisabled()
  await expectA11yClean(page, testInfo, { include: '[data-testid="pkg-grid"]' })

  // 键盘腿：focus + Enter 选定 Maven（网格项是 button——Enter 原生激活）
  await page.focus('[data-testid="pkg-grid-item-maven"]')
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="pkg-grid"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="form-rclass-remote"]')).toBeChecked()
  await expect(page.locator('[data-testid="form-package-maven"]')).toBeChecked()
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled() // 必填未满足禁用

  await page.fill('[data-testid="form-key"]', key)
  await page.fill('[data-testid="form-url"]', 'https://repo1.maven.org/maven2')
  await page.fill('[data-testid="form-description"]', 't240 wizard probe')
  await expect(page.locator('[data-testid="form-submit"]')).toBeEnabled()
  await page.click('[data-testid="form-submit"]')

  await expect(page).toHaveURL(new RegExp(`/binflow/ui/admin/repositories/${key}$`))
  await expect(page.locator('[data-testid="toast"]')).toContainText(`Successfully created repository '${key}'`)
  await expect(page.locator('[data-testid="repo-detail-page"] .key')).toHaveText(key)
  // remote 上游卡在「概要」Tab（锚不变）
  await expect(page.locator('[data-testid="repo-remote-card"]')).toContainText('repo1.maven.org')

  // API 对账（UI 说的话让 API 复核）
  const got = await sessionApi(page, 'GET', `/api/repositories/${key}`)
  expect(got.status).toBe(200)
  expect((got.json as { rclass: string; packageType: string }).rclass).toBe('remote')
  expect((got.json as { rclass: string; packageType: string }).packageType).toBe('maven')

  // 网格取消腿：再次进新建页，取消 = 退回对应 Tab（不产生写请求）
  const writes: string[] = []
  page.on('request', (r) => {
    if (/\/binflow\/api\/repositories\//.test(r.url()) && (r.method() === 'PUT' || r.method() === 'POST')) {
      writes.push(`${r.method()} ${r.url()}`)
    }
  })
  await page.goto('/binflow/ui/admin/repositories/new')
  await page.click('[data-testid="pkg-grid-cancel"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)
  expect(writes).toHaveLength(0)

  // 收尾
  await m8Client().request('DELETE', `/binflow/api/repositories/${key}`)
})

// ---- 2. 三 Tab 列表形态 ---------------------------------------------------

test('admin: three-tab subroutes, per-type rows, column sort, count + pager, filter empty, axe', async ({
  page,
}, testInfo) => {
  const client = m8Client()
  const local = uniq('t240l')
  const remote = uniq('t240r')
  const virtual = uniq('t240v')
  await client.request('PUT', `/binflow/api/repositories/${local}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  await client.request('PUT', `/binflow/api/repositories/${remote}`, {
    body: { rclass: 'remote', packageType: 'maven', url: 'https://repo1.maven.org/maven2' },
  })
  await client.request('PUT', `/binflow/api/repositories/${virtual}`, {
    body: { rclass: 'virtual', packageType: 'generic', repositories: [local, remote] },
  })

  await loginAs(page, 'admin')

  // local Tab：本 Tab 行可见、virtual 行不可见（Tab = 类型过滤）
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator('[data-testid="repos-page"]')).toBeVisible()
  await expect(page.locator(`[data-testid="repos-row-${local}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="repos-row-${remote}"]`)).toHaveCount(0)
  await expect(page.locator('[data-testid="repos-create"]')).toBeVisible()
  await expect(page.locator('[data-testid="repos-readonly-note"]')).toHaveCount(0)

  // remote / virtual Tab 子路由 + aria-current
  await page.click('[data-testid="repos-tab-remote"]')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/remote$/)
  await expect(page.locator(`[data-testid="repos-row-${remote}"]`)).toBeVisible()
  await expect(page.locator(`[data-testid="repos-row-${local}"]`)).toHaveCount(0)
  await expect(page.locator('[data-testid="repos-tab-remote"]')).toHaveAttribute('aria-current', 'page')
  await page.click('[data-testid="repos-tab-virtual"]')
  await expect(page.locator(`[data-testid="repos-row-${virtual}"]`)).toBeVisible()
  // virtual 行 = 成员浮层（沿 T-99 形态）
  await page.click(`[data-testid="repos-row-${virtual}"] .member-pop summary`)
  await expect(page.locator(`[data-testid="repos-row-${virtual}"] .member-pop .pop`)).toContainText(local)

  // 列头排序（key：asc → desc → none；aria-sort 三态）
  await page.goto('/binflow/ui/admin/repositories/local')
  await page.click('[data-testid="repos-sort-key"]')
  await expect(page.locator('[data-testid="repos-sort-key"]')).toHaveAttribute('aria-sort', 'ascending')
  await page.click('[data-testid="repos-sort-key"]')
  await expect(page.locator('[data-testid="repos-sort-key"]')).toHaveAttribute('aria-sort', 'descending')
  const keysDesc = await page.locator('[data-testid="repos-table"] tbody .row-link').allTextContents()
  expect([...keysDesc].sort().reverse()).toEqual(keysDesc)
  await page.click('[data-testid="repos-sort-key"]')
  await expect(page.locator('[data-testid="repos-sort-key"]')).toHaveAttribute('aria-sort', 'none')

  // 计数标题 + 底部计数行（Artifactory "<N> Repositories" / "Showing a – b from c" 形态）
  await expect(page.locator('[data-testid="repos-count"]')).toContainText('个仓库')
  await expect(page.locator('[data-testid="repos-pager"]')).toContainText(/显示 1 – \d+ /)

  // 行链接键盘腿：focus + Enter 进详情
  await page.locator(`[data-testid="repos-row-${local}"] a.row-link`).focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid="repo-detail-page"]')).toBeVisible()

  // key 过滤：命中 → 计数行带过滤标注；无命中 → 过滤空态 + 清除
  await page.goto('/binflow/ui/admin/repositories/local')
  await page.fill('[data-testid="repos-filter-key"]', local)
  await expect(page.locator(`[data-testid="repos-row-${local}"]`)).toBeVisible()
  await expect(page.locator('[data-testid="repos-pager"]')).toContainText('过滤')
  await page.fill('[data-testid="repos-filter-key"]', 'definitely-no-such-repo')
  await expect(page.locator('[data-testid="repos-empty-filtered"]')).toBeVisible()
  await page.locator('[data-testid="repos-empty-filtered"] button', { hasText: '清除过滤' }).click()
  await expect(page.locator(`[data-testid="repos-row-${local}"]`)).toBeVisible()

  await expectA11yClean(page, testInfo, { include: '[data-testid="repos-page"]' })

  // 收尾
  for (const k of [virtual, remote, local]) {
    await client.request('DELETE', `/binflow/api/repositories/${k}`)
  }
})

// ---- 3. 删仓强确认（列表行入口；非空仓 deleteContent 两段流） ----------------

test('admin: row-level delete strong-confirm (typed key, deleteContent two-stage, reconcile)', async ({ page }) => {
  const client = m8Client()
  const key = uniq('t240del')
  await client.request('PUT', `/binflow/api/repositories/${key}`, {
    body: { rclass: 'local', packageType: 'generic' },
  })
  // 非空：1 文件 + 1 祖先目录 = 2 nodes（ADR-0016 目录实体化）
  await client.request('PUT', `/binflow/${key}/d/app.bin`, {
    raw: true,
    headers: { 'Content-Type': 'application/octet-stream' },
    body: 't240-del',
  })

  await loginAs(page, 'admin')
  await page.goto('/binflow/ui/admin/repositories/local')
  await page.click(`[data-testid="repos-delete-${key}"]`)
  await expect(page.locator('[data-testid="confirm-dialog"]')).toBeVisible()
  await expect(page.locator('[data-testid="confirm-dialog"]')).toContainText('没有撤销')

  // 输错 key：确认不可用（P5 强确认）
  await page.fill('[data-testid="repo-delete-confirm-key"]', 'wrong-key')
  await expect(page.locator('[data-testid="confirm-accept"]')).toBeDisabled()

  // 正确 key 但不勾 deleteContent → 400 原因（含 node 数）带回 + 预勾选
  await page.fill('[data-testid="repo-delete-confirm-key"]', key)
  await page.click('[data-testid="confirm-accept"]')
  await expect(page.locator('[data-testid="repo-delete-reason"]')).toContainText('holds 2 node(s)')
  await expect(page.locator('[data-testid="repo-delete-content"]')).toBeChecked()
  await page.fill('[data-testid="repo-delete-confirm-key"]', key)
  await page.click('[data-testid="confirm-accept"]')

  await expect(page.locator('[data-testid="toast"]')).toContainText('deleted successfully')
  await expect(page).toHaveURL(/\/binflow\/ui\/admin\/repositories\/local$/)
  await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toHaveCount(0)
  expect((await sessionApi(page, 'GET', `/api/repositories/${key}`)).status).toBe(404)
})

// ---- 4. readonly_admin 腿（读面全量 + 写面禁用 + T-218 文案收口） ------------

test('readonly_admin: full list visible, write entries gone; detail/config read-only; edit form disabled + note; replay 403', async ({
  page,
}, testInfo) => {
  const client = m8Client()
  const key = uniq('t240ro')
  await client.request('PUT', `/binflow/api/repositories/${key}`, {
    body: { rclass: 'local', packageType: 'generic', quotaBytes: 10240 },
  })

  await loginAs(page, 'readonly_admin')

  // 列表：全量清单可见（CapRepoRead，T-236 定案）+ 只读注记 + 无写入口
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator(`[data-testid="repos-row-${key}"]`)).toBeVisible()
  await expect(page.locator('[data-testid="repos-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="repos-create"]')).toHaveCount(0)
  await expect(page.locator(`[data-testid="repos-delete-${key}"]`)).toHaveCount(0)
  // 浏览器部署入口预收敛（T-266，tree-deploy 语义平移）：禁用态 + tooltip
  // 说明——不再是「可点开、上传后 403 行内兜底」的迟到收敛
  const rowDeploy = page.locator(`[data-testid="repos-deploy-${key}"]`)
  await expect(rowDeploy).toBeDisabled()
  await expect(rowDeploy).toHaveAttribute('title', '只读管理员不可写（服务端 403 兜底）')

  // 详情：三 Tab 可见；配置 Tab quota 行内编辑禁用；无危险区（删除 = CapRepoWrite）
  await page.goto(`/binflow/ui/admin/repositories/${key}`)
  await expect(page.locator('[data-testid="repo-detail-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-detail-readonly-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-danger-zone"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="repo-edit-link"]')).toHaveCount(0)
  // 详情头 Deploy 同款预收敛（T-266）：禁用 + tooltip + 禁用钮不可触发
  // （disabled 控件非可激活态——DOM click 也不产生 click 激活，对话框不开）
  const detailDeploy = page.locator('[data-testid="repo-deploy"]')
  await expect(detailDeploy).toBeDisabled()
  await expect(detailDeploy).toHaveAttribute('title', '只读管理员不可写（服务端 403 兜底）')
  await detailDeploy.evaluate((el) => (el as HTMLButtonElement).click())
  await expect(page.locator('[data-testid="deploy-dialog"]')).toHaveCount(0)
  await page.click('[data-testid="repo-tab-config"]')
  await expect(page.locator('[data-testid="repo-governance-card"]')).toContainText('10240')
  await expect(page.locator('[data-testid="repo-quota-input"]')).toBeDisabled()
  await expect(page.locator('[data-testid="repo-quota-save"]')).toBeDisabled()
  // Replications Tab：降级位指向全局复制页（OSS 同款降级语义）
  await page.click('[data-testid="repo-tab-replications"]')
  await expect(page.locator('[data-testid="repo-repl-degraded"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-repl-goto"]')).toHaveAttribute('href', '/binflow/ui/admin/governance/replication')
  await expectA11yClean(page, testInfo, { include: '[data-testid="repo-detail-page"]' })

  // 编辑表单：全字段禁用 + 保存禁用 + 只读注记（T-218 债收口：文案走
  // CanManageRepo write 语义，不再是错位的 repo:write 全局门措辞）
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await expect(page.locator('[data-testid="form-key"]')).toHaveCount(0) // 编辑态 key 锁定展示
  await expect(page.locator('[data-testid="form-description"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-quota"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()
  await expect(page.locator('[data-testid="form-reset"]')).toBeVisible()
  const note = page.locator('[data-testid="repo-form-readonly-note"]')
  await expect(note).toBeVisible()
  await expect(note).toContainText('CanManageRepo')
  await expect(note).not.toContainText('repo:write')

  // 服务端兜底：同一会话重放写请求——403（UI 只是呈现层，无绕过）
  const w = await sessionApi(page, 'POST', `/api/repositories/${key}`, {
    rclass: 'local',
    packageType: 'generic',
    quotaBytes: 20480,
  })
  expect(w.status).toBe(403)
  const r = await sessionApi(page, 'GET', `/api/repositories/${key}`)
  expect(r.status).toBe(200) // 读面存活（只读态不是 403 姿态）

  // 双保险第二臂（T-266）：内容写面（Deploy 的真实上传臂 PUT
  // /binflow/{repo}/{path}）兜底仍在——预收敛禁用只是呈现层收敛
  const put = await sessionApi(page, 'PUT', `/${key}/e2e/ro-probe.bin`, 'probe\n')
  expect(put.status).toBe(403)
  // 未落任何内容（无绕过）：读面复核该路径不存在（artifacts-tree gone 臂同款）
  expect((await sessionApi(page, 'GET', `/api/storage/${key}/e2e/ro-probe.bin`)).status).toBe(404)

  // 收尾
  await client.request('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`)
})

// ---- 5. m-holder 腿（覆盖集内可编辑、集外 403 收敛） ------------------------

test('m-holder: covered repo editable (quota inline + editor), uncovered converges L2, list 403, no delete entry', async ({
  page,
}) => {
  const client = m8Client()
  const covered = uniq('t240mg')
  const other = uniq('t240mo')
  for (const k of [covered, other]) {
    await client.request('PUT', `/binflow/api/repositories/${k}`, {
      body: { rclass: 'local', packageType: 'generic', quotaBytes: 10240 },
    })
  }
  // loginAs 先行：幂等预备 m8-e2e-user（manage 授予的 target 引用它）
  const s = await loginAs(page, 'user')
  // manage 授予（m 动作 = 仓库配置派生权，M7 §7.2）：只盖 covered
  expect(
    (
      await client.request('POST', '/binflow/api/v1/permissions', {
        body: {
          name: uniq('t240mg'),
          repos: [covered],
          includePatterns: ['**'],
          excludePatterns: [],
          principals: { users: { 'm8-e2e-user': ['read', 'manage'] }, groups: {} },
        },
      })
    ).status,
  ).toBe(201)

  // 列表：CapRepoRead 403 → L2（普通 user 无全量清单）
  await page.goto('/binflow/ui/admin/repositories/local')
  await expect(page.locator('[data-testid="repos-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="repos-empty-filtered"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="repos-table"]')).toHaveCount(0)
  await expect(page.locator('[data-testid="repos-create"]')).toHaveCount(0)
  await expect(page.locator('.empty-state').first()).toContainText('无权限')

  // 覆盖集内详情：可达（GET 走 CanManageRepo 读臂）+ 身份注记 + quota 可编辑
  await page.goto(`/binflow/ui/admin/repositories/${covered}`)
  await expect(page.locator('[data-testid="repo-detail-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-manage-note"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-danger-zone"]')).toHaveCount(0) // 删除 = CapRepoWrite
  await expect(page.locator('[data-testid="repo-edit-link"]')).toBeVisible()
  await page.click('[data-testid="repo-tab-config"]')
  await expect(page.locator('[data-testid="repo-quota-input"]')).toBeEnabled()
  await page.fill('[data-testid="repo-quota-input"]', '40960')
  await page.click('[data-testid="repo-quota-save"]')
  await expect(page.locator('[data-testid="toast"]')).toContainText('update successfully')
  // API 对账：quota 落库且其它字段保全（全量替换语义）
  const got = await sessionApi(page, 'GET', `/api/repositories/${covered}`)
  expect((got.json as { configuration: { quotaBytes: number } }).configuration.quotaBytes).toBe(40960)

  // 覆盖集内编辑器：单页表单可用（服务端 CanManageRepo 写臂放行）
  await page.goto(`/binflow/ui/admin/repositories/${covered}/edit`)
  await expect(page.locator('[data-testid="form-description"]')).toBeEnabled()
  await expect(page.locator('[data-testid="form-submit"]')).toBeEnabled()
  await page.fill('[data-testid="form-description"]', 'updated by m-holder')
  await page.click('[data-testid="form-submit"]')
  await expect(page.locator('[data-testid="toast"]')).toContainText('update successfully')

  // 覆盖集外详情/编辑：GET 403 → L2 无权限卡（§7.10：UI 不自行判定覆盖集）
  await page.goto(`/binflow/ui/admin/repositories/${other}`)
  await expect(page.locator('[data-testid="repo-detail-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-detail-page"] .key')).toHaveCount(0)
  await expect(page.locator('[data-testid="repo-detail-page"] .empty-state')).toContainText('无权限')
  await expect(page.locator('[data-testid="repo-detail-page"] .empty-state')).toContainText('manage')

  // 服务端兜底：覆盖集外写重放 403、创建臂 403（FR-65 V08 边界）
  const wOut = await sessionApi(page, 'POST', `/api/repositories/${other}`, {
    rclass: 'local',
    packageType: 'generic',
  })
  expect(wOut.status).toBe(403)
  const wCreate = await sessionApi(page, 'PUT', `/api/repositories/${uniq('nope')}`, {
    rclass: 'local',
    packageType: 'generic',
  })
  expect(wCreate.status).toBe(403)

  // 收尾（admin 面）
  await client.request('DELETE', `/binflow/api/repositories/${covered}?deleteContent=true`)
  await client.request('DELETE', `/binflow/api/repositories/${other}?deleteContent=true`)
  expect(s.username).toBeTruthy()
})
