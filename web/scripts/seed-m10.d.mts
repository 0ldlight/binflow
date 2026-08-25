// Type surface of scripts/seed-m10.mjs for the e2e TS world (tsconfig includes
// e2e/, strict). Hand-written declaration next to the implementation so the
// .mjs itself stays plain Node ESM (the scripts/ eslint block) — the
// seed-m8/seed-m9 .d.mts discipline.

import type { SeedClient } from './seed-m8.mjs'

export interface M10Plan {
  readonly repoGeneric: string
  readonly repoMaven: string
  readonly grantTarget: string
  readonly user: { readonly name: string; readonly password: string; readonly adminRole: string }
  readonly readonlyAdmin: { readonly name: string; readonly password: string; readonly adminRole: string }
}

export interface RepoDef {
  key: string
  packageType: string
  description: string
}

export interface LegacyFixture {
  repo: string
  path: string
  shape: string
}

export interface SeedM10Result {
  base: string
  repos: { key: string; status: number }[]
  users: { role: string; name: string; status: number }[]
  grant: 'created' | 'present'
  files: string[]
  elapsedMs: number
}

export interface VerifyM10Result {
  ok: boolean
  problems: string[]
  evidence: {
    repos: { key: string; present: boolean }[]
    users: { name: string; present: boolean }[]
    grant: boolean
    readback: { path: string; shape: string; status: number; bytes: boolean }[]
    userReadback: { path: string; status: number }
    propertiesPosture: { path: string; status: number }
  }
}

export declare const M10_PLAN: M10Plan
export declare function legacyRepos(plan?: M10Plan): RepoDef[]
export declare function fixtureBody(repoKey: string, path: string): string
export declare function legacyFixtures(plan?: M10Plan): LegacyFixture[]
export declare function ensureRepo(client: SeedClient, def: RepoDef): Promise<number>
export declare function ensureM10User(
  client: SeedClient,
  def: { name: string; password: string; adminRole: string },
): Promise<number>
export declare function ensureM10ReadGrant(client: SeedClient, plan?: M10Plan): Promise<'created' | 'present'>
export declare function seedM10(client: SeedClient, opts?: { plan?: M10Plan }): Promise<SeedM10Result>
export declare function verifyM10(client: SeedClient, opts?: { plan?: M10Plan; adminPassword?: string }): Promise<VerifyM10Result>
