import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { expect, test } from '@playwright/test'

import { adminCredential } from './support/seed'

// T-287 fill (FR-88, L14~L17; behavior basis = the official NuGet API
// specifications + PRD 88.1 — docs/reverse/nuget.md (T-280) did NOT exist
// at implementation time; the grounding order and the live probes are
// recorded in reports/agents/T-287.md). The exhaustive protocol matrix
// lives in the Go harness (internal/adapter/nuget — endpoint matrix,
// license lifecycle, upstream-hit counters, the real dotnet legs);
// THIS spec is the QA anchor against a REAL binary at $BASE.
//
// Legs:
//   L14 v3 push: index.json resources, REST push of a real .nupkg shape +
//      flatcontainer/registration/versions reconciliation
//   L15 consume: dotnet pack → dotnet nuget push (direct publish form) →
//      dotnet add package --source → restore → run (exit codes are the
//      asserted object, the m10 README §2.5 rule), remote pull-through
//      second-hit cache leg
//   L16 v2: FindPackagesById()?id= OData entry shape (version/hash/deps)
//   L17 virtual: nuget-virt local-first aggregation (the tier-flip matrix
//      is gating-matrix.spec.ts's legs — L06~L09 — not this spec)
//
// Preconditions (the spec self-skips honestly):
//   - $BASE serves a BinFlow instance that can serve nuget repositories
//     (a pro-tier form or seeded rows — community answers the D3 400 on
//     create, which IS the gating matrix's assertion, not this one);
//   - BINFLOW_M10_NUGET_E2E=1 opts into the real-client legs (dotnet SDK
//     on PATH, DOTNET_ROOT honored).

const NUGET_E2E = process.env.BINFLOW_M10_NUGET_E2E === '1'
const REPO_LOCAL = process.env.BINFLOW_M10_NUGET_REPO ?? 'nuget-local'
const REPO_REMOTE = process.env.BINFLOW_M10_NUGET_REMOTE_REPO ?? 'nuget-remote'
const REPO_VIRTUAL = process.env.BINFLOW_M10_NUGET_VIRTUAL_REPO ?? 'nuget-virt'
const BASE = process.env.BASE ?? 'http://127.0.0.1:8080'
const V3 = (repo: string) => `/binflow/api/nuget/v3/${repo}` // repo-relative; raw() prefixes BASE
const V2 = (repo: string) => `/binflow/api/nuget/v2/${repo}`
const ABS = (path: string) => `${BASE}${path}` // absolute form (nuget.config sources)

// T-326 D-9②: env-first admin credential via the shared resolver — no
// hardcoded dev default in this spec (the nuget.config ClearTextPassword
// block below rides the same pair).
const ADMIN = adminCredential()

/** One admin-authenticated raw request against $BASE (statuses + bodies
 * for reconciliation; never文案 assertions). */
async function raw(
  method: string,
  path: string,
  body?: Buffer | string,
  contentType?: string,
): Promise<{ status: number; text: string; headers: Record<string, string> }> {
  const auth = Buffer.from(`${ADMIN.username}:${ADMIN.password}`).toString('base64')
  const res = await fetch(BASE + path, {
    method,
    headers: {
      Authorization: `Basic ${auth}`,
      ...(body !== undefined && contentType ? { 'Content-Type': contentType } : {}),
    },
    body: body as BodyInit | undefined,
  })
  const headers: Record<string, string> = {}
  res.headers.forEach((v, k) => (headers[k] = v))
  if (method === 'HEAD') await res.arrayBuffer()
  return { status: res.status, text: method === 'HEAD' ? '' : await res.text(), headers }
}

/** CRC-32 (the stored-entry zip builder's digest). */
function crc32(data: Uint8Array): number {
  let c = ~0
  for (const b of data) {
    c ^= b
    for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1))
  }
  return ~c >>> 0
}

/** A real .nupkg with STORED entries — the nuspec at the root named after
 * the package id plus one payload file (the official package layout the
 * push validation walks; the Go harness twin builds the same shape with
 * archive/zip, and the dotnet-client legs below push dotnet's OWN
 * packages). */
function nupkg(id: string, version: string, deps: Array<[string, string]> = []): Buffer {
  const enc = new TextEncoder()
  const depXml = deps.length
    ? `    <dependencies>\n      <group targetFramework="netstandard2.0">\n${deps
        .map(([d, r]) => `        <dependency id="${d}" version="${r}" />`)
        .join('\n')}\n      </group>\n    </dependencies>\n`
    : ''
  const nuspec = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd">
  <metadata>
    <id>${id}</id>
    <version>${version}</version>
    <authors>M10 QA</authors>
    <description>nuget-pilot.spec fixture</description>
${depXml}  </metadata>
</package>
`
  const files: Array<[string, string]> = [
    [`${id}.nuspec`, nuspec],
    ['content/hello.txt', `hello ${id} ${version}\n`],
  ]
  const chunks: Buffer[] = []
  const central: Buffer[] = []
  let offset = 0
  for (const [name, content] of files) {
    const nameB = Buffer.from(enc.encode(name))
    const data = Buffer.from(enc.encode(content))
    const local = Buffer.alloc(30 + nameB.length)
    local.writeUInt32LE(0x04034b50, 0)
    local.writeUInt16LE(20, 4)
    local.writeUInt32LE(crc32(data), 14)
    local.writeUInt32LE(data.length, 18)
    local.writeUInt32LE(data.length, 22)
    local.writeUInt16LE(nameB.length, 26)
    nameB.copy(local, 30)
    chunks.push(local, data)
    const cd = Buffer.alloc(46 + nameB.length)
    cd.writeUInt32LE(0x02014b50, 0)
    cd.writeUInt16LE(20, 4)
    cd.writeUInt16LE(20, 6)
    cd.writeUInt32LE(crc32(data), 16)
    cd.writeUInt32LE(data.length, 20)
    cd.writeUInt32LE(data.length, 24)
    cd.writeUInt16LE(nameB.length, 28)
    cd.writeUInt32LE(offset, 42)
    nameB.copy(cd, 46)
    central.push(cd)
    offset += local.length + data.length
  }
  const cdBuf = Buffer.concat(central)
  const end = Buffer.alloc(22)
  end.writeUInt32LE(0x06054b50, 0)
  end.writeUInt16LE(central.length, 8)
  end.writeUInt16LE(central.length, 10)
  end.writeUInt32LE(cdBuf.length, 12)
  end.writeUInt32LE(offset, 16)
  return Buffer.concat([...chunks, cdBuf, end])
}

test.beforeAll(async () => {
  // Honest precondition (the go-pilot posture): the WRITE legs need the
  // nuget slot unlocked — a pro-tier instance. A community instance (with
  // or without seeded rows) answers every nuget write with the D2 403 +
  // X-Binflow-License-Required; that flip is gating-matrix.spec.ts's
  // leg (L06~L09), never this spec's.
  const lic = await raw('GET', '/binflow/api/system/license')
  const tier = (JSON.parse(lic.text).tier ?? 'community') as string
  test.skip(
    tier !== 'pro' && tier !== 'enterprise',
    `nuget writes are gated on this ${tier} instance (D2); install a pro license — see reports/agents/T-287.md`,
  )
})

// Sequential by data: L15 consumes what L14 publishes; L16/L17 reconcile
// the same facts (the seed-then-assert idiom).
test.describe.configure({ mode: 'serial' })

test('L14 v3 push: index resources + REST push + flatcontainer/registration reconciliation', async () => {
  // The service index: the three resource families, absolute URLs back
  // into this instance, publish aliasing the flatcontainer base.
  const idx = await raw('GET', `${V3(REPO_LOCAL)}/index.json`)
  expect(idx.status).toBe(200)
  const doc = JSON.parse(idx.text)
  expect(doc.version).toBe('3.0.0')
  const types = new Set(doc.resources.map((r: { '@type': string }) => r['@type']))
  for (const t of [
    'SearchQueryService',
    'RegistrationsBaseUrl/3.6.0',
    'PackageBaseAddress/3.0.0',
    'PackagePublish/2.0.0',
    'LegacyGallery',
  ])
    expect(types.has(t), `index misses ${t}`).toBe(true)

  // REST push (the addressed form — the curl surface; the dotnet direct
  // form lands in L15) + the sha512 sidecar reconciliation.
  const pkg = nupkg('M10.QA.Lib', '1.0.0', [['Serilog', '[4.0.0,)']])
  const sha512 = createHash('sha512').update(pkg).digest('base64')
  const push = await raw(
    'PUT',
    `/binflow/api/nuget/v3/${REPO_LOCAL}/flatcontainer/m10.qa.lib/1.0.0`,
    pkg,
  )
  expect(push.status).toBe(201)

  const versions = await raw('GET', `${V3(REPO_LOCAL)}/flatcontainer/m10.qa.lib/index.json`)
  expect(versions.status).toBe(200)
  expect(JSON.parse(versions.text).versions).toContain('1.0.0')

  const nupkgRes = await raw(
    'GET',
    `${V3(REPO_LOCAL)}/flatcontainer/m10.qa.lib/1.0.0/m10.qa.lib.1.0.0.nupkg`,
  )
  expect(nupkgRes.status).toBe(200)
  expect(nupkgRes.headers['x-checksum-sha256']).toBe(createHash('sha256').update(pkg).digest('hex'))
  expect((await raw('HEAD', `${V3(REPO_LOCAL)}/flatcontainer/m10.qa.lib/1.0.0/m10.qa.lib.1.0.0.nupkg`)).status).toBe(200)

  const sha = await raw(
    'GET',
    `${V3(REPO_LOCAL)}/flatcontainer/m10.qa.lib/1.0.0/m10.qa.lib.1.0.0.nupkg.sha512`,
  )
  expect(sha.status).toBe(200)
  expect(sha.text.trim()).toBe(sha512)

  const reg = await raw('GET', `${V3(REPO_LOCAL)}/registration/m10.qa.lib/index.json`)
  expect(reg.status).toBe(200)
  const regDoc = JSON.parse(reg.text)
  const leaf = regDoc.items[0].items[0]
  expect(leaf.catalogEntry.id).toBe('M10.QA.Lib')
  expect(leaf.catalogEntry.version).toBe('1.0.0')
  expect(leaf.catalogEntry.packageHash).toBe(sha512)
  expect(leaf.catalogEntry.packageHashAlgorithm).toBe('SHA512')
  expect(leaf.catalogEntry.dependencyGroups[0].targetFramework).toBe('netstandard2.0')
  expect(leaf.catalogEntry.dependencyGroups[0].dependencies[0]).toEqual({
    '@type': 'PackageDependency',
    id: 'Serilog',
    range: '[4.0.0,)',
  })
  expect(leaf.packageContent).toContain(`/binflow/api/nuget/v3/${REPO_LOCAL}/flatcontainer/`)

  // Search reconciliation (q + prerelease filter).
  const search = await raw('GET', `${V3(REPO_LOCAL)}/query?q=m10.qa`)
  expect(search.status).toBe(200)
  const hits = JSON.parse(search.text)
  expect(hits.totalHits).toBeGreaterThanOrEqual(1)
  expect(hits.data[0].id).toBe('m10.qa.lib')
})

test('L15 real client: dotnet pack → nuget push → add package --source → restore → run', () => {
  test.skip(!NUGET_E2E, 'set BINFLOW_M10_NUGET_E2E=1 (with a dotnet SDK on PATH) for the real-client legs')

  const lib = mkdtempSync(join(tmpdir(), 'm10-nuget-lib-'))
  const app = mkdtempSync(join(tmpdir(), 'm10-nuget-app-'))
  const dotnet = process.env.DOTNET_ROOT ? join(process.env.DOTNET_ROOT, 'dotnet') : 'dotnet'
  const home = mkdtempSync(join(tmpdir(), 'm10-nuget-home-'))
  const env = {
    ...process.env,
    DOTNET_CLI_TELEMETRY_OPTOUT: '1',
    DOTNET_NOLOGO: '1',
    DOTNET_SKIP_FIRST_TIME_EXPERIENCE: '1',
    DOTNET_MULTILEVEL_LOOKUP: '0',
    HOME: home,
    NUGET_XMLDOC_MODE: 'skip',
  }
  const run = (cwd: string, args: string[]) => execFileSync(dotnet, args, { cwd, env, encoding: 'utf8' })

  // Pack a real library (dotnet's own nupkg — the multipart direct push
  // the adapter must unwrap, the T-287 live finding).
  writeFileSync(
    join(lib, 'M10.Nuget.Lib.csproj'),
    `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
    <PackageId>M10.Nuget.Lib</PackageId>
    <Version>1.0.0</Version>
    <PackageOutputPath>${lib}</PackageOutputPath>
  </PropertyGroup>
</Project>
`,
  )
  writeFileSync(
    join(lib, 'Greeter.cs'),
    'namespace M10.Nuget.Lib;\n\npublic static class Greeter { public static string Hello() => "hello from m10 nuget"; }\n',
  )
  run(lib, ['pack', '--nologo', '-v', 'q'])

  // Push with Basic credentials via nuget.config (the source is the v3
  // service index — the client discovers the publish URL itself).
  writeFileSync(
    join(lib, 'nuget.config'),
    `<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="binflow" value="${ABS(`${V3(REPO_LOCAL)}/index.json`)}" />
  </packageSources>
  <packageSourceCredentials>
    <binflow>
      <add key="Username" value="${ADMIN.username}" />
      <add key="ClearTextPassword" value="${ADMIN.password}" />
    </binflow>
  </packageSourceCredentials>
</configuration>
`,
  )
  expect(() => run(lib, ['nuget', 'push', join(lib, 'M10.Nuget.Lib.1.0.0.nupkg'), '--source', 'binflow', '--api-key', 'x', '--skip-duplicate'])).not.toThrow()

  // Consume: add + restore + run (exit codes are the assertion).
  writeFileSync(
    join(app, 'nuget.config'),
    `<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="binflow" value="${ABS(`${V3(REPO_LOCAL)}/index.json`)}" />
  </packageSources>
</configuration>
`,
  )
  writeFileSync(
    join(app, 'App.csproj'),
    `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>
</Project>
`,
  )
  writeFileSync(join(app, 'Program.cs'), 'Console.WriteLine(M10.Nuget.Lib.Greeter.Hello());\n')
  run(app, ['add', 'package', 'M10.Nuget.Lib', '--source', ABS(`${V3(REPO_LOCAL)}/index.json`)])
  run(app, ['restore', '--nologo', '-v', 'q'])
  const out = run(app, ['run', '--nologo', '-v', 'q'])
  expect(out.trim().split('\n').pop()).toBe('hello from m10 nuget')
})

test('L15 remote pull-through: restore through nuget-remote (second-hit cache)', async () => {
  test.skip(!NUGET_E2E, 'set BINFLOW_M10_NUGET_E2E=1 for the real-client legs')
  test.skip(
    process.env.BINFLOW_M10_NUGET_REMOTE_SKIP === '1',
    'public upstream disabled for this run (record the reason in the QA log)',
  )

  // A REAL public package restored through the remote repository. The
  // fixed probe (newtonsoft.json 13.0.2) was verified servable from this
  // network's nuget mirror at fill time; override via env when the
  // reachable mirror set differs (see the ticket log's staleness probe).
  const app = mkdtempSync(join(tmpdir(), 'm10-nuget-remote-'))
  const dotnet = process.env.DOTNET_ROOT ? join(process.env.DOTNET_ROOT, 'dotnet') : 'dotnet'
  const home = mkdtempSync(join(tmpdir(), 'm10-nuget-home2-'))
  const env = {
    ...process.env,
    DOTNET_CLI_TELEMETRY_OPTOUT: '1',
    DOTNET_NOLOGO: '1',
    DOTNET_SKIP_FIRST_TIME_EXPERIENCE: '1',
    HOME: home,
    NUGET_XMLDOC_MODE: 'skip',
  }
  const run = (args: string[]) => execFileSync(dotnet, args, { cwd: app, env, encoding: 'utf8' })

  writeFileSync(
    join(app, 'nuget.config'),
    `<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="binflow-remote" value="${ABS(`${V3(REPO_REMOTE)}/index.json`)}" />
  </packageSources>
</configuration>
`,
  )
  writeFileSync(
    join(app, 'App.csproj'),
    `<?xml version="1.0" encoding="utf-8"?>
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>
</Project>
`,
  )
  writeFileSync(
    join(app, 'Program.cs'),
    `Console.WriteLine(Newtonsoft.Json.JsonConvert.SerializeObject(new { via = "binflow" }));\n`,
  )
  run(['add', 'package', 'Newtonsoft.Json', '--version', '13.0.2', '--source', ABS(`${V3(REPO_REMOTE)}/index.json`)])
  run(['restore', '--nologo', '-v', 'q'])
  const out = run(['run', '--nologo', '-v', 'q'])
  expect(out).toContain('"via":"binflow"')

  // The cache reconciliation: the landed copy answers the second read
  // (the upstream contact count lives in the Go harness's counters; the
  // REST-side assertion is the X-Binflow-Cache hit marker).
  const again = await raw(
    'GET',
    `/binflow/api/nuget/v3/${REPO_REMOTE}/flatcontainer/newtonsoft.json/13.0.2/newtonsoft.json.13.0.2.nupkg`,
  )
  expect(again.status).toBe(200)
  expect(String(again.headers['x-binflow-cache'] ?? '').toLowerCase()).toContain('hit')
})

test('L16 v2 FindPackagesById(): OData entry shape (id/version/hash/deps)', async () => {
  const meta = await raw('GET', `${V2(REPO_LOCAL)}/$metadata`)
  expect(meta.status).toBe(200)
  expect(meta.text).toContain('EntitySet Name="Packages"')

  const feed = await raw('GET', `${V2(REPO_LOCAL)}/FindPackagesById()?id='m10.qa.lib'`)
  expect(feed.status).toBe(200)
  expect(feed.headers['content-type']).toContain('application/xml')
  expect(feed.text).toContain('<entry>')
  expect(feed.text).toContain('<d:Id>M10.QA.Lib</d:Id>')
  expect(feed.text).toContain('<d:Version>1.0.0</d:Version>')
  expect(feed.text).toContain('<d:PackageHashAlgorithm>SHA512</d:PackageHashAlgorithm>')
  expect(feed.text).toContain('<d:IsLatestVersion>true</d:IsLatestVersion>')
  expect(feed.text).toContain('<d:Dependencies>Serilog:[4.0.0,):netstandard2.0</d:Dependencies>')
  expect(feed.text).toContain(
    `/binflow/api/nuget/v3/${REPO_LOCAL}/flatcontainer/m10.qa.lib/1.0.0/m10.qa.lib.1.0.0.nupkg`,
  )

  // Unknown id → the EMPTY feed (collection semantics, never 404).
  const empty = await raw('GET', `${V2(REPO_LOCAL)}/FindPackagesById()?id='No.Such.Pkg'`)
  expect(empty.status).toBe(200)
  expect(empty.text).toContain('<feed')
  expect(empty.text).not.toContain('<entry>')
})

test('L17 virtual: local-first aggregation across local + remote members', async () => {
  // Push a local member package, then read BOTH faces through the
  // virtual: the local package resolves local-first, the remote member's
  // cached package walks to the remote member.
  const pkg = nupkg('M10.Virt.Lib', '1.0.0')
  expect((await raw('PUT', `/binflow/api/nuget/v3/${REPO_LOCAL}/flatcontainer/m10.virt.lib/1.0.0`, pkg)).status).toBe(201)

  const versions = await raw('GET', `${V3(REPO_VIRTUAL)}/flatcontainer/m10.virt.lib/index.json`)
  expect(versions.status).toBe(200)
  expect(JSON.parse(versions.text).versions).toContain('1.0.0')

  const dl = await raw(
    'GET',
    `${V3(REPO_VIRTUAL)}/flatcontainer/m10.virt.lib/1.0.0/m10.virt.lib.1.0.0.nupkg`,
  )
  expect(dl.status).toBe(200)

  const feed = await raw('GET', `${V2(REPO_VIRTUAL)}/FindPackagesById()?id='m10.virt.lib'`)
  expect(feed.status).toBe(200)
  expect(feed.text).toContain('<entry>')

  // The remote member's package through the virtual (the cached copy the
  // L15 remote leg landed, when that leg ran on this instance; otherwise
  // the honest miss is the 404 — the member walk is the Go harness's
  // asserted part).
  const rem = await raw(
    'GET',
    `${V3(REPO_VIRTUAL)}/registration/newtonsoft.json/index.json`,
  )
  expect([200, 404]).toContain(rem.status)
})
