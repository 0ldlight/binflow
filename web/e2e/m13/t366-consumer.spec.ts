import { test, expect } from '@playwright/test'
import { spawn, execFileSync } from 'node:child_process'
import { createHash, createHmac, randomBytes } from 'node:crypto'
import net from 'node:net'
import { mkdtempSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

// T-366 consumer leg (M13 FR-115.7 / L08 / AC-2): the REAL pipeline against
// a REAL binary — a self-booted ephemeral PRO instance (license injected;
// the shared $BASE harness is community where every write verb 403s) plus
// the script receiver t366-receiver.mjs (fault injection: 500 / timeout).
//
// Legs:
//   UI    create subscription through the real console dialog (signed secret)
//   PIPE  artifact PUT → receiver gets the 7-field envelope, HMAC-SHA256 hex
//         verified over the EXACT wire bytes; troubleshooting ring carries
//         the delivered record (status / elapsed / retries=0); the artifact
//         PUT itself stays fast (emit never blocks the main path)
//   TEST  POST /subscriptions/test direct send — plaintext-secret mode puts
//         the secret itself in X-JFrog-Event-Auth (official dual-state)
//   RETRY receiver 500,500 → 200: delivered on attempt 3 (fixed 10s waits),
//         ring records retries_attempted 0,1,2
//   DEAD  receiver always 500: 5 attempts (first counted) then dead —
//         5 ring records, retries_attempted 0..4
//   HANG  timeout fault via the test endpoint: outcome ok=false, send-failure
//         error, elapsed ≥ ~30s (the whole-attempt budget)
//   DRAW  the console drawer shows the dead subscription's record chain
//
// Preconditions (the suite self-skips honestly, the m10-pilot convention):
//   - `make console && make build` has produced bin/binflow-server with the
//     T-366 console embedded (the UI legs settle on wh-* anchors);
//   - a pro license document: $T366_LICENSE_FILE, or auto-minted via
//     bin/bf + the repo-root signing key when present (the m10-tier-matrix
//     --license-dir injection posture: documents are INJECTED, never
//     generated here — the dev checkout's gitignored key is the source);
//   - the receiver binds the machine's GLOBAL IPv6 (the SSRF guard keeps its
//     default posture: loopback/RFC1918 targets stay refused — that refusal
//     is asserted as a leg — while the own-GUA address is admitted and the
//     traffic never leaves the host). The webhook.allow_private_target knob
//     that would admit loopback is currently UNREACHABLE in config loading
//     (internal/config/load.go never wired the section — registered as
//     drift in the ticket log), so the GUA is the honest on-host route.

import { networkInterfaces } from 'node:os'

const ROOT = resolve(process.cwd(), '..')
const BIN = join(ROOT, 'bin', 'binflow-server')
const BF = join(ROOT, 'bin', 'bf')
const KEY = join(ROOT, 'binflow-license-private.pem')
const ADMIN = 'admin'
const ADMIN_PW = 't366-consumer-pw'
const RECEIVER = join(ROOT, 'web', 'e2e', 'm13', 't366-receiver.mjs')

/** A global (non-link-local, non-ULA, non-loopback) IPv6 of this machine —
 *  the only locally-routable address class the SSRF guard admits by default. */
function ownGlobalV6(): string | null {
  for (const ifaces of Object.values(networkInterfaces())) {
    for (const i of ifaces ?? []) {
      if (i.family !== 'IPv6' || i.internal) continue
      const a = i.address.toLowerCase()
      if (a.startsWith('fe80:') || a.startsWith('fc') || a.startsWith('fd') || a === '::1') continue
      if (/^[23][0-9a-f]{3}:/.test(a)) return i.address
    }
  }
  return null
}

/** The pro license document (env-injected or minted from the dev key). */
function licenseDoc(): string | null {
  const fromEnv = process.env.T366_LICENSE_FILE
  if (fromEnv && existsSync(fromEnv)) return readFileSync(fromEnv, 'utf8').trim()
  if (existsSync(BF) && existsSync(KEY)) {
    try {
      return execFileSync(BF, ['license', 'issue', '--licensee', 'T-366 consumer e2e', '--tier', 'pro', '--days', '2'], { cwd: ROOT, encoding: 'utf8' }).trim()
    } catch {
      return null
    }
  }
  return null
}

const license = licenseDoc()
const gua = ownGlobalV6()
const canRun = existsSync(BIN) && license !== null && gua !== null
const skipReason = !existsSync(BIN)
  ? 'bin/binflow-server not built — run `make console && make build`'
  : license === null
    ? 'no pro license material (set T366_LICENSE_FILE or provide bin/bf + the repo signing key) — the consumer legs need an unlocked webhook slot'
    : 'no global IPv6 on this machine (the SSRF guard refuses loopback/RFC1918 by default and the allow_private_target knob is unreachable in config — see the ticket log drift entry)'

test.skip(!canRun, skipReason)

// ---- the ephemeral stack (serial: one server, one receiver, shared state) ---

test.describe.configure({ mode: 'serial' })

const state: {
  base?: string
  auth?: string
  recv?: { port: number; logs: (path: string) => Promise<RecvEntry[]> }
} = {}
let serverProc: ReturnType<typeof spawn> | null = null
let recvProc: ReturnType<typeof spawn> | null = null
let tmp = ''

interface RecvEntry {
  ts: number
  method: string
  headers: Record<string, string>
  raw: string
  body: {
    domain: string
    event_type: string
    data: Record<string, unknown>
    subscription_key: string
    jpd_origin: string
    source: string
    userContext: { id: string; isToken: boolean; realm: string }
  }
}

/** deadline poller */
async function until<T>(fn: () => Promise<T | undefined>, timeoutMs: number, what: string): Promise<T> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const v = await fn()
    if (v !== undefined) return v
    if (Date.now() > deadline) throw new Error(`timeout waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 500))
  }
}

/** REST helper over the ephemeral server (Basic auth — the smoke.sh posture) */
async function rest(method: string, path: string, body?: string | Record<string, unknown>): Promise<{ status: number; text: string }> {
  const isJson = typeof body === 'object'
  const res = await fetch(`${state.base}${path}`, {
    method,
    headers: {
      ...(state.auth ? { Authorization: state.auth } : {}),
      ...(body !== undefined && isJson ? { 'Content-Type': 'application/json' } : body !== undefined ? { 'Content-Type': 'text/plain;charset=utf-8' } : {}),
    },
    body: body === undefined ? undefined : isJson ? JSON.stringify(body) : (body as string),
  })
  return { status: res.status, text: await res.text() }
}

/** one subscription body (the wire shape webhook.md §2 prescribes) */
function subBody(key: string, url: string, over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    key,
    project_key: '',
    description: `t366 consumer leg ${key}`,
    enabled: true,
    event_filter: {
      domain: 'artifact',
      event_types: ['deployed'],
      criteria: { anyLocal: true, includePatterns: [`${key}/**`] },
    },
    handlers: [{ handler_type: 'webhook', url, use_secret_for_signing: false, custom_http_headers: [] }],
    debug: true,
    ...over,
  }
}

test.beforeAll(async () => {
  // 1. receiver on an ephemeral port, bound to the own global IPv6 (admitted
  //    by the SSRF guard's default posture; traffic stays on the host)
  recvProc = spawn(process.execPath, [RECEIVER, '0', gua!], { stdio: ['ignore', 'pipe', 'inherit'] })
  const recvPort = await new Promise<number>((resolvePort, reject) => {
    const t = setTimeout(() => reject(new Error('receiver never reported its port')), 10_000)
    recvProc!.stdout!.on('data', (d: Buffer) => {
      const m = /RECV (\d+)/.exec(d.toString())
      if (m) {
        clearTimeout(t)
        resolvePort(Number(m[1]))
      }
    })
  })
  await until(async () => {
    try {
      const r = await fetch(`http://[${gua}]:${recvPort}/__health`)
      return r.ok ? true : undefined
    } catch {
      return undefined
    }
  }, 10_000, 'receiver health')

  // 2. ephemeral pro server (SSRF posture relaxed for the loopback receiver —
  //    webhook.allow_private_target, ADR-0041 decision 6's operator knob)
  tmp = mkdtempSync(join(tmpdir(), 'binflow-t366-'))
  const port = await new Promise<number>((res) => {
    const s = net.createServer()
    s.listen(0, '127.0.0.1', () => {
      const p = (s.address() as { port: number }).port
      s.close(() => res(p))
    })
  })
  state.base = `http://127.0.0.1:${port}`
  state.auth = `Basic ${Buffer.from(`${ADMIN}:${ADMIN_PW}`).toString('base64')}`
  state.recv = {
    port: recvPort,
    logs: async (path: string) => (await (await fetch(`http://[${gua!}]:${recvPort}/__log?path=${encodeURIComponent(path)}`)).json()) as RecvEntry[],
  }
  const cfg = join(tmp, 'binflow.yaml')
  writeFileSync(cfg, `server:\n  listen: 127.0.0.1:${port}\n  base_url: ${state.base}\nstorage:\n  data_dir: ${join(tmp, 'data')}\n`)
  const credsKey = randomBytes(32).toString('base64')
  serverProc = spawn(BIN, ['serve', '-c', cfg], {
    cwd: ROOT,
    env: {
      ...process.env,
      BINFLOW_ADMIN_PASSWORD: ADMIN_PW,
      // NOTE: no webhook.allow_private_target here — the knob is currently
      // unreachable in config loading (registered as drift); the receiver's
      // GUA address needs no relaxation and the DEFAULT SSRF posture is one
      // of the asserted legs.
      BINFLOW_REMOTE_CREDENTIALS_KEY: credsKey,
      BINFLOW_LOGGING__LEVEL: 'info',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  serverProc.stdout!.on('data', (d: Buffer) => process.stdout.write(`[srv] ${d}`))
  serverProc.stderr!.on('data', (d: Buffer) => process.stderr.write(`[srv] ${d}`))
  await until(async () => {
    try {
      const r = await fetch(`${state.base}/binflow/api/system/ping`)
      return r.ok ? true : undefined
    } catch {
      return undefined
    }
  }, 30_000, 'server ping')

  // 3. inject the pro license (write verbs need the webhook slot unlocked)
  const lic = await rest('POST', '/binflow/api/system/license', license!)
  expect(lic.status, `license install failed: ${lic.text}`).toBe(201)
  const tier = JSON.parse((await rest('GET', '/binflow/api/system/license')).text) as { tier: string }
  expect(tier.tier).toBe('pro')

  // 4. one local repo feeds every leg's artifact PUT
  const repo = await rest('PUT', '/binflow/api/repositories/t366-local', { rclass: 'local', packageType: 'generic', description: 't366 consumer' })
  expect(repo.status, `repo create failed: ${repo.text}`).toBe(200)
})

test.afterAll(() => {
  if (serverProc) {
    serverProc.kill('SIGTERM')
    serverProc = null
  }
  if (recvProc) {
    recvProc.kill('SIGTERM')
    recvProc = null
  }
  if (tmp) {
    try {
      rmSync(tmp, { recursive: true, force: true })
    } catch {
      // best-effort cleanup
    }
  }
})

const SIGNED_SECRET = 't366-signing-secret'

// ---- UI leg: create the signed subscription through the real console -------

test('consumer UI: create signed subscription via the console dialog', async ({ page }) => {
  await page.goto(`${state.base}/binflow/ui/`)
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()

  await page.goto(`${state.base}/binflow/ui/admin/governance/webhooks`)
  await expect(page.locator('[data-testid="wh-page"]')).toBeVisible()
  await expect(page.locator('[data-testid="wh-empty"]')).toBeVisible()

  await page.click('[data-testid="wh-create"]')
  await expect(page.locator('[data-testid="wh-dialog"]')).toBeVisible()
  await page.fill('[data-testid="wh-form-key"]', 'uileg')
  // 默认域 artifact + deployed 预选 + anyLocal；路径 pattern 隔离本腿事件
  await page.fill('[data-testid="wh-form-include"]', 'uileg/**')
  await page.fill('[data-testid="wh-form-url"]', `http://[${gua}]:${state.recv!.port}/ok`)
  await page.fill('[data-testid="wh-form-secret"]', SIGNED_SECRET)
  await page.check('[data-testid="wh-form-sign"]')
  await page.check('[data-testid="wh-form-debug"]')
  await page.check('[data-testid="wh-form-enabled"]')
  await page.click('[data-testid="wh-form-submit"]')

  await expect(page.locator('[data-testid="wh-row-uileg"]')).toBeVisible()
})

// ---- PIPE leg: artifact PUT → signed envelope at the receiver + ring record -

test('consumer PIPE: PUT triggers signed 7-field envelope; delivered record in the ring', async () => {
  const body = `t366 pipe leg ${Date.now()}\n`
  const t0 = Date.now()
  const up = await rest('PUT', '/binflow/t366-local/uileg/a.bin', body)
  const dur = Date.now() - t0
  expect(up.status, `artifact PUT failed: ${up.text}`).toBe(201)
  // NFR-P58 主路径零阻塞面：emit 是同步小事务——PUT 秒回（粗上界 2s）
  expect(dur, 'artifact PUT blocked on the emit path').toBeLessThan(2000)

  const entry = await until(async () => (await state.recv!.logs('/ok'))[0], 15_000, 'receiver /ok entry')
  const env = entry.body
  // §4 七字段（BinFlow 取文档全集的既定立场）
  expect(env.domain).toBe('artifact')
  expect(env.event_type).toBe('deployed')
  expect(env.subscription_key).toBe('uileg')
  expect(env.jpd_origin).toBe(state.base)
  expect(env.source).toMatch(/^binflow\/binflow@[0-9A-Z]{26}$/)
  expect(env.userContext).toEqual({ id: 'admin', isToken: false, realm: 'internal' })
  const data = env.data as Record<string, unknown>
  expect(data.repo_key).toBe('t366-local')
  expect(data.path).toBe('uileg/a.bin')
  expect(data.name).toBe('a.bin')
  expect(data.sha256).toBe(createHash('sha256').update(body).digest('hex')) // sha256 of the exact PUT bytes
  expect(data.size).toBe(body.length)

  // HMAC-SHA256 hex over the EXACT wire bytes（openssl sha256 -hmac 兼容形态）
  const wire = Buffer.from(entry.raw, 'latin1')
  const sig = entry.headers['x-jfrog-event-auth']
  expect(typeof sig).toBe('string')
  expect(sig).toBe(createHmac('sha256', SIGNED_SECRET).update(wire).digest('hex'))

  // 排障环：debug 订阅的成功投递也入记录（状态/耗时/重试计数）
  const rec = await until(async () => {
    const recs = (JSON.parse(
      (await rest('GET', '/binflow/event/api/v1/troubleshooting?subscription=uileg')).text,
    )) as { response: { status: number }; elapsed_millis: number; request: { retries_attempted: number } }[]
    return recs.find((r) => r.response.status === 200)
  }, 15_000, 'delivered ring record')
  expect(rec.elapsed_millis).toBeGreaterThan(0)
  expect(rec.request.retries_attempted).toBe(0)
})

// ---- TEST leg: the synchronous draft send, plaintext-secret dual-state -----

test('consumer TEST: direct send carries the plaintext secret in X-JFrog-Event-Auth', async () => {
  const res = await rest('POST', '/binflow/event/api/v1/subscriptions/test', {
    ...subBody('draftprobe', `http://[${gua}]:${state.recv!.port}/ok`, { enabled: false }),
    handlers: [{ handler_type: 'webhook', url: `http://[${gua}]:${state.recv!.port}/ok`, secret: 't366-plain-secret', use_secret_for_signing: false, custom_http_headers: [] }],
  })
  expect(res.status).toBe(200)
  const outcome = JSON.parse(res.text) as { ok: boolean; message: string; attempt: { status_code: number; elapsed_millis: number } }
  expect(outcome.ok).toBe(true)
  expect(outcome.attempt.status_code).toBe(200)
  expect(outcome.attempt.elapsed_millis).toBeGreaterThan(0)

  const entries = await state.recv!.logs('/ok')
  const last = entries[entries.length - 1]!
  expect(last.headers['x-jfrog-event-auth']).toBe('t366-plain-secret') // 官方默认态：secret 明文直传
})

// ---- SSRF leg: the default posture refuses loopback targets（§5.4 / L09）---

test('consumer SSRF: loopback target refused by the default posture', async () => {
  const url = `http://127.0.0.1:${state.recv!.port}/ok`
  const res = await rest('POST', '/binflow/event/api/v1/subscriptions/test', {
    ...subBody('ssrfprobe', url, { enabled: false }),
    handlers: [{ handler_type: 'webhook', url, use_secret_for_signing: false, custom_http_headers: [] }],
  })
  expect(res.status).toBe(200)
  const outcome = JSON.parse(res.text) as { ok: boolean; attempt: { status_code: number; error?: string } }
  expect(outcome.ok).toBe(false)
  expect(outcome.attempt.status_code).toBe(0)
  expect(outcome.attempt.error ?? '').toContain('target rejected')
  expect(outcome.attempt.error ?? '').toContain('loopback')
})

// ---- RETRY leg: 500,500 → 200（固定 10s 间隔，首试计入） --------------------

test('consumer RETRY: 500,500 → delivered on attempt 3 with retries 0,1,2 recorded', async () => {
  const url = `http://[${gua}]:${state.recv!.port}/flaky`
  const created = await rest('POST', '/binflow/event/api/v1/subscriptions', subBody('retryleg', url))
  expect(created.status).toBe(201)
  const up = await rest('PUT', '/binflow/t366-local/retryleg/a.bin', 'retry leg\n')
  expect(up.status).toBe(201)

  // 接收器见 3 次到达（t0 / +10s / +20s——固定间隔，无退避曲线）
  await until(async () => ((await state.recv!.logs('/flaky')).length >= 3 ? true : undefined), 45_000, '3 flaky arrivals')
  const arrivals = (await state.recv!.logs('/flaky')).map((e) => e.ts)
  expect(arrivals[1]! - arrivals[0]!).toBeGreaterThanOrEqual(9_000)
  expect(arrivals[2]! - arrivals[1]!).toBeGreaterThanOrEqual(9_000)

  // 环记录：两次失败（retries 0,1）+ 一次成功（retries 2）
  const recs = await until(async () => {
    const rs = (JSON.parse(
      (await rest('GET', '/binflow/event/api/v1/troubleshooting?subscription=retryleg')).text,
    )) as { response: { status: number }; errors: string[]; request: { retries_attempted: number } }[]
    const success = rs.find((r) => r.response.status === 200)
    return rs.length >= 3 && success ? rs : undefined
  }, 45_000, 'retry chain in the ring')
  const byRetries = [...recs].sort((a, b) => a.request.retries_attempted - b.request.retries_attempted)
  expect(byRetries.map((r) => r.request.retries_attempted)).toEqual([0, 1, 2])
  expect(byRetries[0]!.response.status).toBe(500)
  expect(byRetries[0]!.errors[0]).toContain('500')
})

// ---- DEAD leg: always-500 → 5 attempts（首试计入）→ dead 可查 --------------

test('consumer DEAD: persistent 500 exhausts the budget — 5 ring records, retries 0..4', async () => {
  test.setTimeout(120_000)
  const url = `http://[${gua}]:${state.recv!.port}/fail500`
  const created = await rest('POST', '/binflow/event/api/v1/subscriptions', subBody('deadleg', url))
  expect(created.status).toBe(201)
  const up = await rest('PUT', '/binflow/t366-local/deadleg/a.bin', 'dead leg\n')
  expect(up.status).toBe(201)

  // 5 次尝试（t0,+10,+20,+30,+40）；每次失败必录 → 环上 5 条
  const recs = await until(async () => {
    const rs = (JSON.parse(
      (await rest('GET', '/binflow/event/api/v1/troubleshooting?subscription=deadleg')).text,
    )) as { response: { status: number }; errors: string[]; request: { retries_attempted: number } }[]
    return rs.length >= 5 ? rs : undefined
  }, 75_000, '5 dead-leg records')
  expect(recs).toHaveLength(5)
  const retries = recs.map((r) => r.request.retries_attempted).sort((a, b) => a - b)
  expect(retries).toEqual([0, 1, 2, 3, 4])
  for (const r of recs) {
    expect(r.response.status).toBe(500)
    expect(r.errors[0]).toContain('500')
  }
  const arrivals = (await state.recv!.logs('/fail500')).map((e) => e.ts)
  expect(arrivals).toHaveLength(5)
})

// ---- HANG leg: timeout fault via the synchronous test endpoint --------------

test('consumer HANG: unresponsive receiver aborts at the 30s whole-attempt budget', async () => {
  test.setTimeout(90_000)
  const url = `http://[${gua}]:${state.recv!.port}/hang`
  const res = await rest('POST', '/binflow/event/api/v1/subscriptions/test', {
    ...subBody('hangprobe', url, { enabled: false }),
    handlers: [{ handler_type: 'webhook', url, use_secret_for_signing: false, custom_http_headers: [] }],
  })
  expect(res.status).toBe(200)
  const outcome = JSON.parse(res.text) as { ok: boolean; attempt: { status_code: number; elapsed_millis: number; error?: string } }
  expect(outcome.ok).toBe(false)
  expect(outcome.attempt.status_code).toBe(0)
  expect(outcome.attempt.error ?? '').toContain('send failed')
  expect(outcome.attempt.elapsed_millis).toBeGreaterThanOrEqual(25_000) // 30s 预算（含余量下界）
})

// ---- DRAWER leg: the console shows the dead chain (AC1 × AC2 集成） ---------

test('consumer DRAWER: console drawer renders the dead subscription record chain', async ({ page }) => {
  await page.goto(`${state.base}/binflow/ui/`)
  await page.fill('[data-testid="login-username"]', ADMIN)
  await page.fill('[data-testid="login-password"]', ADMIN_PW)
  await page.click('[data-testid="login-submit"]')
  await expect(page.locator('[data-testid="app-nav"]')).toBeVisible()

  await page.goto(`${state.base}/binflow/ui/admin/governance/webhooks`)
  await expect(page.locator('[data-testid="wh-row-deadleg"]')).toBeVisible()
  await page.click('[data-testid="wh-open-deadleg"]')
  await expect(page.locator('[data-testid="wh-drawer"]')).toBeVisible()
  const row0 = page.locator('[data-testid="wh-record-0"]')
  await expect(row0).toBeVisible()
  await expect(row0).toContainText('500')
  // 载荷快照可展开（mono + 拷贝基元在页）
  await row0.click()
  await expect(page.locator('[data-testid="wh-record-payload-0"]')).toContainText('"subscription_key"')
})
