// T-268 BASE ownership probe (T-266 registered leftover #3). Wired as the
// harness globalSetup: before ANY worker starts, verify $BASE answers as a
// BinFlow instance. Guards the accident class where the intended server
// failed to start (port already taken, wrong BINFLOW_SERVER__LISTEN) and the
// suite then silently hammers whatever else owns the port — under the
// restored default parallelism that misfire would hit an innocent instance
// with 8 concurrent workers instead of one.
//
// Identity check = /binflow/api/system/version (E-03, anonymous 200) whose
// body pins product:"BinFlow" (PRD Q4: honest identity, never an emulated
// Artifactory version) — an arbitrary web server or a non-BinFlow registry
// cannot satisfy it. The resolved version/revision is echoed into the run
// log so stale-binary incidents (T-266 §3.1) are visible at a glance.
//
// Failure mode = throw from globalSetup: Playwright aborts the whole run
// before scheduling tests, with this message as the first line of output.
const DEFAULT_BASE = 'http://127.0.0.1:8080'
const PROBE_TIMEOUT_MS = 5_000

type VersionBody = { product?: unknown; version?: unknown; revision?: unknown }

export default async function baseProbe(): Promise<void> {
  const base = (process.env.BASE ?? DEFAULT_BASE).replace(/\/+$/, '')
  const url = `${base}/binflow/api/system/version`

  let status = -1
  let body: VersionBody | null = null
  try {
    const res = await fetch(url, {
      headers: { Accept: 'application/json' },
      signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
    })
    status = res.status
    body = (await res.json().catch(() => null)) as VersionBody | null
  } catch (err) {
    throw new Error(
      `base-probe: ${url} unreachable (${(err as Error).name}: ${(err as Error).message}). ` +
        'The harness does NOT autostart the server — start it first ' +
        '(make build && ./bin/binflow-server serve) or point BASE at the intended instance.',
    )
  }

  if (status !== 200 || body?.product !== 'BinFlow') {
    throw new Error(
      `base-probe: ${url} is not a BinFlow instance ` +
        `(HTTP ${status}, product=${JSON.stringify(body?.product ?? null)}) — ` +
        'BASE points at the wrong target. Refusing to run the suite against it.',
    )
  }

  // One-line run banner — the harness log's only channel for the probe.
  console.log(
    `base-probe: BASE ${base} → BinFlow ${String(body.version)} (rev ${String(body.revision)}) — ownership verified`,
  )
}
