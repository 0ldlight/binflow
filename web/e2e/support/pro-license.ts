import { appendFileSync, readFileSync, rmSync, writeFileSync } from 'node:fs'

import { m8Client } from '../m8/support/seed'

// Shared pro-license lease for specs that flip the SHARED test instance to
// pro (repo-creation / bundle / archive gates) and must restore it before
// the run moves on — community-posture legs elsewhere in the suite red the
// moment a pro license lingers (the L025-2 "54-red family": the former
// per-spec lease files let every worker ACQUIRE but only the installing
// worker RELEASE, so the holder count never reached zero and the license
// stayed on for the rest of the run).
//
// Contract (t461 / p4 / t514 consumers):
//   beforeAll: proLeaseAcquire(BASE) — UNCONDITIONAL, before the probe:
//     workers that find the instance already pro must still hold a lease,
//     or the installer's release sees phantom holders and never unloads.
//   install:  proLeaseMarkInstalled(BASE) once THIS worker installed — the
//     marker rides the file so the LAST worker out (not necessarily the
//     installer) knows an unload is owed.
//   afterAll: await proLeaseRelease(BASE, licenseInstalledByUs) —
//     UNCONDITIONAL: the non-installing workers' releases are exactly what
//     lets the count reach zero. Last one out unloads (and only then, and
//     only if anyone installed) — instance restored to its pre-run state.
//
// One file per instance PORT, shared across consumer specs: t461 unloading
// "its" license while p4/t514 legs are mid-flight (separate per-spec files
// cannot see each other) is the same poison in the other direction.
// Best-effort by design (comments at the former call sites said so too):
// a failed fs op degrades to install-and-unload-per-worker.

const BASE_DEFAULT = 'http://127.0.0.1:8080'

function leaseFile(base: string): string {
  return `/tmp/binflow-e2e-pro-lease-${new URL(base).port}.txt`
}

/** Dead-pid pruning: stale lines from crashed runs must not pin the license
 *  forever (p4's evolution of the t461 original, kept here as the one home). */
function pruneDeadPids(all: string[]): string[] {
  return all.filter((l) => {
    const n = Number(l)
    if (!Number.isInteger(n)) return true // 'installed' marker is not a pid
    try {
      process.kill(n, 0)
      return true
    } catch {
      return false
    }
  })
}

function readLease(base: string): string[] {
  try {
    return pruneDeadPids(
      readFileSync(leaseFile(base), 'utf8').split('\n').filter((l) => l.trim() !== ''),
    )
  } catch {
    return []
  }
}

export function proLeaseAcquire(base = process.env.BASE ?? BASE_DEFAULT): void {
  try {
    const kept = readLease(base)
    rmSync(leaseFile(base), { force: true })
    writeFileSync(leaseFile(base), [...kept, String(process.pid)].join('\n') + '\n')
  } catch {
    // best effort — failure degrades to install-and-unload-per-worker
  }
}

export function proLeaseMarkInstalled(base = process.env.BASE ?? BASE_DEFAULT): void {
  try {
    appendFileSync(leaseFile(base), 'installed\n')
  } catch {
    // same best-effort posture
  }
}

export async function proLeaseRelease(
  base = process.env.BASE ?? BASE_DEFAULT,
  installedByThisWorker = false,
): Promise<void> {
  let remaining = 1
  let installed = installedByThisWorker
  try {
    const all = readLease(base)
    const others = all.filter((l) => l !== String(process.pid) && l !== 'installed')
    installed = installed || all.includes('installed')
    remaining = others.length
    if (remaining === 0) rmSync(leaseFile(base), { force: true })
    else writeFileSync(leaseFile(base), `${others.join('\n')}${installed ? '\ninstalled' : ''}\n`)
  } catch {
    // unreadable = no concurrent record — treat as last worker out
    remaining = 0
  }
  if (remaining === 0 && installed) {
    await m8Client().request('DELETE', '/binflow/api/system/license').catch(() => undefined)
  }
}
