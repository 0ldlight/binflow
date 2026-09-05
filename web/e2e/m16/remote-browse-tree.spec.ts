import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { appendFileSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { createServer } from 'node:http'
import { gzipSync } from 'node:zlib'

import { expectA11yClean } from '../m8/support/a11y'
import { loginAs } from '../m8/support/roles'
import { m8Client } from '../m8/support/seed'

// T-461（M16 B13，FR-147.3——FE 远端浏览树消费：listRemoteFolderItems
// 可选档 on/off 双态 + 未缓存路径回源 + 降级呈现）。消费面：
//
//   ① 可选档开关（AC1）：仓库表单 Advanced 段 `form-list-remote-folder-items`
//      复选（off 默认——diff=0；仅批 1 型 helm/debian/rpm 呈现——引擎
//      BrowseSupported 同集，true 于其它包型服务端按名 400，generic/maven
//      的 HTML 抓取 BinFlow 明确不做〔remote-browsing.md §6〕不建禁用占位）
//      + 仓库详情 `repo-remote-browse` 回显 + 编辑回显 + flip-off round trip
//      （POINTER 语义——显式 false 恒提交）。wire 腿 = D-T456-1 修复后的
//      transport 指针字段（本 spec 即其端到端消费面：若 BE 腿未合入，
//      PUT→GET echo 断言红 = 「候 BE 合入复验」留痕，不强绿）。
//   ② 树消费双态（AC2）：off → remote 树仅缓存行（charts/ 等派生臂缺席
//      ——既有断言零回归）；on → 树展开含未缓存远端目录（helm index 全树
//      臂 / deb·rpm 元数据臂）+ 点击未缓存路径触发回源拉取 + 下载计数
//      联动（?stats 面 T-438 单源——uniq 仓新路径，断言确定性首计数）+
//      virtual 树含 remote 成员行（§8.5 扩面消费）。
//   ③ 上游停机降级（AC3）：缓存行可用（listing 不整树塌）+ 已渲染的派生
//      行点击 → 详情面板远端错误态 `node-remote-error`。wire note
//      （FolderInfo.remoteDegraded——T-448 §5-2 缝，httpapi 渲染腿在途）
//      以 route 注入契约形验证 FE 消费（`tree-remote-degraded` 横幅 +
//      缓存行仍在）——BE 腿合入后该注入腿自然退役为 live 断言。
//   ④ axe 双主题：树页（开档仓选中 + 降级横幅在场）+ 表单 Advanced 步。
//
// 夹具纪律：spec 内自建上游夹具（node http —— helm index.yaml 全树 /
// rpm repodata+primary / deb dists 元数据三臂语料）+ 临时端口 bind(0)；
// 仓库全 uniq key + afterAll 删除；pro license 经 bin/bf 自铸安装
// （t366 先例——helm/debian/rpm 建仓门；实例已 pro 则零动作、不可铸则
// skip 留痕，铸装则 afterAll 卸载还原）。锚源 = console-ux §10.5 T-461 批。

const BASE = process.env.BASE ?? 'http://127.0.0.1:8080'
// 仓根（cwd = web/——t366 同款：license 开发钥与 bin/bf 相对仓根寻址）
const ROOT = resolve(process.cwd(), '..')

// ---- 上游夹具语料（三臂） ----------------------------------------------------

const HELM_INDEX = `apiVersion: v1
entries:
  nginx:
    - name: nginx
      version: 1.0.0
      urls:
        - charts/nginx-1.0.0.tgz
    - name: nginx
      version: 1.1.0
      urls:
        - charts/nginx-1.1.0.tgz
  redis:
    - name: redis
      version: 2.0.0
      urls:
        - charts/redis-2.0.0.tgz
`

const RPM_REPOMD = `<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <revision>t461</revision>
  <data type="primary">
    <location href="repodata/primary.xml.gz"/>
    <checksum type="sha256">0000000000000000000000000000000000000000000000000000000000000000</checksum>
  </data>
</repomd>
`

const RPM_PRIMARY = gzipSync(
  Buffer.from(
    `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/primary" packages="1">
  <package type="rpm">
    <name>hello</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="1.0" rel="1"/>
    <location href="RPMS/x86_64/hello-1.0-1.x86_64.rpm"/>
  </package>
</metadata>
`,
  ),
)

const DEB_RELEASE = `Origin: t461 fixture
Suite: stable
Codename: stable
Architectures: amd64
Components: main
Checksums-Sha256:
 d41d8cd98f00b204e9800998ecf8427e 55 main/binary-amd64/Packages
 d41d8cd98f00b204e9800998ecf8427e 120 main/binary-amd64/Packages.gz
`

const DEB_PACKAGES = `Package: hello
Version: 1.0
Architecture: amd64
Filename: pool/main/h/hello/hello_1.0_amd64.deb
Size: 1000
`

/** 三臂语料全集（键 = 上游相对路径） */
const FIXTURE_FILES: Record<string, Buffer | string> = {
  'index.yaml': HELM_INDEX,
  'charts/nginx-1.0.0.tgz': 'FAKE-TGZ-nginx-1.0.0',
  'charts/nginx-1.1.0.tgz': 'FAKE-TGZ-nginx-1.1.0',
  'charts/redis-2.0.0.tgz': 'FAKE-TGZ-redis-2.0.0',
  'repodata/repomd.xml': RPM_REPOMD,
  'repodata/primary.xml.gz': RPM_PRIMARY,
  'dists/stable/Release': DEB_RELEASE,
  'dists/stable/main/binary-amd64/Packages': DEB_PACKAGES,
  'dists/stable/main/binary-amd64/Packages.gz': gzipSync(Buffer.from(DEB_PACKAGES)),
}

interface FixtureServer {
  url: string
  close: () => Promise<void>
}

/** 一个上游夹具服务器（bind(0) 临时端口——零端口冲突；降级腿用私实例可杀） */
async function startFixture(): Promise<FixtureServer> {
  const server = createServer((req, res) => {
    const rel = decodeURIComponent((req.url ?? '').split('?')[0]).replace(/^\/+/, '')
    const body = FIXTURE_FILES[rel]
    if (body === undefined) {
      res.writeHead(404, { 'content-type': 'text/plain' })
      res.end(`not found: ${rel}`)
      return
    }
    res.writeHead(200, { 'content-type': 'application/octet-stream' })
    res.end(body)
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const addr = server.address()
  // string 分支 = IPC 管道名（listen(0) on tcp 不触发，类型收窄需要）
  if (addr === null || typeof addr === 'string') throw new Error('fixture address unavailable')
  return {
    url: `http://127.0.0.1:${addr.port}`,
    // closeAllConnections：上游客户端（Go 引擎）的 keep-alive 连接会让
    // 裸 close() 等到 drain——降级腿需要「立即真死」
    close: () =>
      new Promise<void>((resolve) => {
        server.close(() => resolve())
        server.closeAllConnections()
      }),
  }
}

// ---- 实例状态（license 门 + 仓库台账） --------------------------------------

const createdKeys: string[] = []
let sharedFixture: FixtureServer | null = null
let licenseInstalledByUs = false
let licenseBlocked = ''

// ---- pro license 租约（并行 worker 协调） -----------------------------------
//
// 实例态纪律：本 spec 需要 pro（batch-1 建仓门）但不得把 pro 留给实例
//（community 假设的既有腿会翻红——m8 trash-locked 实证）。多 worker 并行
// 时 beforeAll/afterAll 各跑一份：以实例端口为键的租约文件计数活跃
// worker，末位 worker（计数归零）才卸载 license。

const leaseFile = `/tmp/t461-license-lease-${new URL(BASE).port}.txt`

/** 本 worker 登记租约——无条件（实例已 pro 的 worker 也要登记：否则装
 *  license 的 worker 会在其结束时就地卸载，砸到仍在跑腿的兄弟 worker） */
function acquireLicenseLease(): void {
  try {
    appendFileSync(leaseFile, `${process.pid}\n`)
  } catch {
    // 租约协调是尽力而为——失败退化为「谁装谁卸」（单 worker 即常态路径）
  }
}

/** 本 worker 装过 license：随租约文件携带 installed 标记（跨 worker 传递） */
function markLicenseInstalled(): void {
  try {
    appendFileSync(leaseFile, 'installed\n')
  } catch {
    // 同上——尽力而为
  }
}

/** 归还租约；末位 worker（无其余登记）且文件携带 installed 标记时卸载
 *  license 还原实例态（实例本就 pro 的运行不携带标记——零误删） */
async function releaseLicenseLease(): Promise<void> {
  let remaining = 1
  let weInstalled = licenseInstalledByUs
  try {
    const all = readFileSync(leaseFile, 'utf8').split('\n').filter((l) => l.trim() !== '')
    const others = all.filter((l) => l !== String(process.pid) && l !== 'installed')
    weInstalled = weInstalled || all.includes('installed')
    remaining = others.length
    if (remaining === 0) rmSync(leaseFile, { force: true })
    else writeFileSync(leaseFile, `${others.join('\n')}${weInstalled ? '\ninstalled' : ''}\n`)
  } catch {
    // 读不到 = 无并发记录，按末位处理
    remaining = 0
  }
  if (remaining === 0 && weInstalled) {
    await rest('DELETE', '/binflow/api/system/license').catch(() => undefined)
  }
}

/** admin REST 便捷封装（makeClient.request 的动词面——非 2xx 抛错带状态） */
async function rest(method: 'GET' | 'PUT' | 'POST' | 'DELETE', path: string, body?: unknown): Promise<void> {
  await m8Client().request(method, path, body !== undefined ? { body } : undefined)
}

test.beforeAll(async () => {
  sharedFixture = await startFixture()
  // helm/debian/rpm 建仓走 license 门（ADR-0033）：实例已 pro 则零动作；
  // community 则用仓库开发钥自铸 2 天 pro（t366 先例），afterAll 卸载还原。
  // 并行 worker 竞态：本文件 fullyParallel 下多 worker 各跑一份
  // beforeAll/afterAll——worker A 卸载时 worker B 可能仍在跑腿（建仓 400）。
  // 租约计数文件协调：每 worker 登记，末位 worker 才卸载。
  const probe = await m8Client().probeGet('/binflow/api/system/license')
  const alreadyPro = probe.status === 200 && (JSON.parse(probe.text).tier ?? '') === 'pro'
  acquireLicenseLease()
  if (alreadyPro) return
  try {
    const doc = execFileSync(
      join(ROOT, 'bin', 'bf'),
      ['license', 'issue', '--licensee', 'T-461 e2e', '--tier', 'pro', '--days', '2'],
      { cwd: ROOT, encoding: 'utf8' },
    ).trim()
    await m8Client().request('POST', '/binflow/api/system/license', {
      raw: true,
      body: doc,
      headers: { 'content-type': 'text/plain' },
    })
    licenseInstalledByUs = true
    markLicenseInstalled()
  } catch (err) {
    licenseBlocked = `pro license unavailable for batch-1 package types (${String(err).slice(0, 160)})`
  }
})

test.afterAll(async () => {
  for (const key of createdKeys) {
    await rest('DELETE', `/binflow/api/repositories/${key}?deleteContent=true`).catch(() => undefined)
  }
  if (licenseInstalledByUs) {
    await releaseLicenseLease()
  }
  await sharedFixture?.close()
})

test.beforeEach(async ({ request }) => {
  const probe = await request.get('/binflow/ui/')
  test.skip(probe.status() === 404, 'console segment not mounted by this binary yet')
  test.skip(!!licenseBlocked, licenseBlocked)
})

function uniq(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** 建一个批 1 型 remote 仓（REST 面——表单 UI 面单独腿走 ①） */
async function createRemote(pkg: 'helm' | 'debian' | 'rpm', url: string, flag: boolean): Promise<string> {
  const key = uniq(`t461-${pkg}`)
  await rest('PUT', `/binflow/api/repositories/${key}`, {
    rclass: 'remote',
    packageType: pkg,
    url,
    allowPrivateUpstream: true,
    listRemoteFolderItems: flag,
  })
  createdKeys.push(key)
  return key
}

/** 内容面 GET 拉一个路径（回源 pull-through——缓存 seeding） */
async function pullThrough(path: string): Promise<void> {
  await rest('GET', `/binflow${path}`)
}

async function login(page: Page) {
  await loginAs(page, 'admin')
}

// ---------------------------------------------------------------------------
// ① 可选档开关：表单（默认 off + 批 1 型门）+ 详情/编辑回显 + flip-off ------
// ---------------------------------------------------------------------------

test('flag: advanced-step checkbox off by default, batch-1 gated; round trip on/off echoes in detail', async ({
  page,
}) => {
  const key = await createRemote('helm', sharedFixture!.url, false)
  await login(page)

  // 编辑态：Advanced 步——批 1 型控件在场、默认 off；零改动 Save 禁用
  //（dirty-gating 既有语义不回归）
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await page.click('[data-testid="form-step-advanced"]')
  const box = page.locator('[data-testid="form-list-remote-folder-items"]')
  await expect(box).toBeVisible()
  await expect(box).not.toBeChecked()
  await expect(page.locator('[data-testid="form-submit"]')).toBeDisabled()

  // 开档保存（D-T456-1 wire 腿端到端）→ 详情回显「开启」
  await box.check()
  await expect(page.locator('[data-testid="form-submit"]')).toBeEnabled()
  await page.click('[data-testid="form-submit"]')
  await expect(page.locator('[data-testid="repo-detail-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-remote-browse"]')).toContainText('开启')

  // 编辑回显 on → flip-off round trip（显式 false 恒提交）→ 详情「关闭」
  await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
  await page.click('[data-testid="form-step-advanced"]')
  await expect(page.locator('[data-testid="form-list-remote-folder-items"]')).toBeChecked()
  await page.uncheck('[data-testid="form-list-remote-folder-items"]')
  await page.click('[data-testid="form-submit"]')
  await expect(page.locator('[data-testid="repo-detail-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="repo-remote-browse"]')).toContainText('关闭')

  // 非批 1 型（generic remote）：控件不呈现（不建禁用占位——不伪造将支持）
  const genericKey = uniq('t461-generic')
  await rest('PUT', `/binflow/api/repositories/${genericKey}`, {
    rclass: 'remote',
    packageType: 'generic',
    url: sharedFixture!.url,
    allowPrivateUpstream: true,
  })
  createdKeys.push(genericKey)
  await page.goto(`/binflow/ui/admin/repositories/${genericKey}/edit`)
  await page.click('[data-testid="form-step-advanced"]')
  await expect(page.locator('[data-testid="form-list-remote-folder-items"]')).toHaveCount(0)
})

// ---------------------------------------------------------------------------
// ② 树消费双态：off = 仅缓存行；on = helm 全树 + deb/rpm 元数据臂 + 回源 ----
// ---------------------------------------------------------------------------

test('tree off-state: remote tree carries cached rows only (diff = 0)', async ({ page }) => {
  const key = await createRemote('helm', sharedFixture!.url, false)
  // 缓存 seeding：拉 index.yaml（落地缓存行——off 态树上唯一可见面）
  await pullThrough(`/${key}/index.yaml`)
  await login(page)

  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="tree-row-index.yaml"]')).toBeVisible()
  // 派生臂缺席：charts/ 不出现（off 不回源枚举）
  await expect(page.locator('[data-testid="tree-node-charts"]')).toHaveCount(0)
  // 双态注记：off 口径文案
  await expect(page.locator('[data-testid="tree-remote-note"]')).toContainText('已缓存内容')
})

test('tree on-state: helm full-tree arm, uncached click pulls through with stats linkage', async ({ page }) => {
  const key = await createRemote('helm', sharedFixture!.url, true)
  await login(page)

  await page.goto(`/binflow/ui/artifacts/${key}`)
  // 根：charts/（派生目录）+ index.yaml（派生文件行）——index.yaml 全树臂
  //（children 表 = 选中仓根的权威呈现面；左树分支需手动展开不参与断言）
  await expect(page.locator('[data-testid="tree-row-charts"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-index.yaml"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-remote-note"]')).toContainText('远端浏览已开启')

  // 展开未缓存远端目录（URL 选择目录——左树祖先链自动展开 + 表收窄）：
  // charts/ 下 tgz 派生行（「远端」标记 + '—' 列）
  await page.goto(`/binflow/ui/artifacts/${key}/charts`)
  await expect(page.locator('[data-testid="tree-leaf-charts/nginx-1.1.0.tgz"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-nginx-1.0.0.tgz"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-nginx-1.0.0.tgz"] [data-testid="tree-row-uncached"]')).toBeVisible()

  // 点击未缓存路径 → 回源拉取：详情计数联动（?stats 面 T-438 单源——
  // item-info GET 即计数探针；不断言绝对值〔NodeDetail 的 useAsync 随父
  // 渲染重复发射 item GET，绝对值非确定——票内登记〕，断言动作后 > 0）
  await page.click('[data-testid="tree-leaf-charts/nginx-1.1.0.tgz"]')
  await expect(page.locator('[data-testid="node-detail"]')).toContainText('nginx-1.1.0.tgz')
  const downloads = page.locator('[data-testid="node-downloads"]')
  await expect
    .poll(async () => Number((await downloads.textContent())?.trim()), { timeout: 10_000 })
    .toBeGreaterThan(0)

  // 回源已落地：刷新后该行不再是「远端」派生形态（size/sha 自纠）
  await page.reload()
  await expect(page.locator('[data-testid="tree-row-nginx-1.1.0.tgz"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-nginx-1.1.0.tgz"] [data-testid="tree-row-uncached"]')).toHaveCount(0)
})

test('tree on-state: rpm and deb metadata arms enumerate; virtual carries remote member rows', async ({ page }) => {
  const helmKey = await createRemote('helm', sharedFixture!.url, true)
  const rpmKey = await createRemote('rpm', sharedFixture!.url, true)
  const debKey = await createRemote('debian', sharedFixture!.url, true)
  // deb 枚举基座 = 缓存内 suite 集（browseDebSuites）：先拉 Release 落缓存
  await pullThrough(`/${debKey}/dists/stable/Release`)

  // virtual（§8.5 扩面）：flag-on remote 成员的派生行进 virtual 树
  const virtKey = uniq('t461-virt')
  await rest('PUT', `/binflow/api/repositories/${virtKey}`, {
    rclass: 'virtual',
    packageType: 'helm',
    repositories: [helmKey],
  })
  createdKeys.push(virtKey)
  await login(page)

  // rpm：repodata + RPMS 两臂（repomd.xml location 全集 + primary 的
  // package location——RPMS/x86_64/ 为派生目录链）
  await page.goto(`/binflow/ui/artifacts/${rpmKey}`)
  await expect(page.locator('[data-testid="tree-row-repodata"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-RPMS"]')).toBeVisible()
  await page.goto(`/binflow/ui/artifacts/${rpmKey}/RPMS`)
  await expect(page.locator('[data-testid="tree-node-RPMS/x86_64"]')).toBeVisible()

  // deb：dists + pool 两臂（Release checksums 节 + Packages Filename 节）
  await page.goto(`/binflow/ui/artifacts/${debKey}`)
  await expect(page.locator('[data-testid="tree-row-dists"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-pool"]')).toBeVisible()
  await page.goto(`/binflow/ui/artifacts/${debKey}/pool`)
  await expect(page.locator('[data-testid="tree-node-pool/main"]')).toBeVisible()

  // virtual：remote 成员的 charts/ 派生臂 + 文件行同树呈现
  await page.goto(`/binflow/ui/artifacts/${virtKey}`)
  await expect(page.locator('[data-testid="tree-row-charts"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-index.yaml"]')).toBeVisible()
  await page.goto(`/binflow/ui/artifacts/${virtKey}/charts`)
  await expect(page.locator('[data-testid="tree-leaf-charts/nginx-1.0.0.tgz"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ③ 上游停机降级：缓存行可用 + 派生行点击错误态 + wire-note 消费（注入） -----
// ---------------------------------------------------------------------------

test('degradation: upstream outage keeps cached rows, derived-row click errors, note banner renders', async ({
  page,
}) => {
  // 本腿私有夹具（可杀）：seed 缓存后停上游——降级面
  const fixture = await startFixture()
  const key = await createRemote('helm', fixture.url, true)
  await pullThrough(`/${key}/index.yaml`)
  await login(page)

  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="tree-row-charts"]')).toBeVisible()
  await page.goto(`/binflow/ui/artifacts/${key}/charts`)
  await expect(page.locator('[data-testid="tree-leaf-charts/nginx-1.1.0.tgz"]')).toBeVisible()

  // 停上游（真死——close + closeAllConnections，非「大概没人听」）。注意
  // 枚举快照仍按 metadata TTL 续命（引擎 §4 语义：parsed tree 的进程内
  // 缓存不因上游死亡失效）——派生行在场，但点击即触回源链失败：
  await fixture.close()
  await page.click('[data-testid="tree-leaf-charts/nginx-1.1.0.tgz"]')
  await expect(page.locator('[data-testid="node-remote-error"]')).toBeVisible()

  // 换 url 到死端口（bind 后 close 的端口）：枚举快照签名失效（url 变
  // 更即失效——browseSignature）→ 刷新后派生臂消失，缓存行仍可用
  // （listing 不整树塌——§4-1 的 FE 可见面）
  const dead = await startFixture()
  const deadPort = dead.url
  await dead.close()
  await rest('POST', `/binflow/api/repositories/${key}`, {
    rclass: 'remote',
    packageType: 'helm',
    url: deadPort,
    allowPrivateUpstream: true,
    listRemoteFolderItems: true,
  })
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="tree-row-index.yaml"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-row-charts"]')).toHaveCount(0)

  // wire-note 消费腿（契约形注入——BE 渲染腿在途）：FolderInfo 携带
  // remoteDegraded 时横幅呈现 + 缓存行仍在（T-448 §5-2 缝的 FE 侧定案）
  await page.route(`**/api/storage/${key}`, async (route) => {
    const res = await route.fetch()
    const body = (await res.json()) as Record<string, unknown>
    body.remoteDegraded = "remote enumeration unavailable: upstream 'index.yaml': connection failed"
    await route.fulfill({ response: res, json: body })
  })
  await page.goto(`/binflow/ui/artifacts/${key}`)
  await expect(page.locator('[data-testid="tree-remote-degraded"]')).toBeVisible()
  await expect(page.locator('[data-testid="tree-remote-degraded"]')).toContainText('已缓存条目仍可用')
  await expect(page.locator('[data-testid="tree-row-index.yaml"]')).toBeVisible()
})

// ---------------------------------------------------------------------------
// ④ axe 双主题：树页（开档 + 降级横幅在场）+ 表单 Advanced 步 ---------------
// ---------------------------------------------------------------------------

test('a11y: remote-browse tree and form advanced step in both themes', async ({ page }, testInfo) => {
  const key = await createRemote('helm', sharedFixture!.url, true)
  await login(page)

  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((t) => localStorage.setItem('binflow-console-theme', t), theme)
    // 树页：开档仓选中 + 降级横幅在场（注入契约形）+ 派生行标记
    await page.route(`**/api/storage/${key}`, async (route) => {
      const res = await route.fetch()
      const body = (await res.json()) as Record<string, unknown>
      body.remoteDegraded = 'remote enumeration unavailable: assumed offline'
      await route.fulfill({ response: res, json: body })
    })
    await page.goto(`/binflow/ui/artifacts/${key}`)
    await expect(page.locator('[data-testid="tree-remote-degraded"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="tree-page"]' })

    // 表单 Advanced 步：可选档复选 + TTL 族在场
    await page.goto(`/binflow/ui/admin/repositories/${key}/edit`)
    await page.click('[data-testid="form-step-advanced"]')
    await expect(page.locator('[data-testid="form-list-remote-folder-items"]')).toBeVisible()
    await expectA11yClean(page, testInfo, { include: '[data-testid="repo-form-page"]' })
  }
})
