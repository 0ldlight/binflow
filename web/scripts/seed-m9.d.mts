// Type surface of scripts/seed-m9.mjs for the e2e TS world (tsconfig includes
// e2e/, strict). Hand-written declaration next to the implementation so the
// .mjs itself stays plain Node ESM (the scripts/ eslint block) — the
// seed-m8.d.mts discipline.

import type { SeedClient } from './seed-m8.mjs'

export interface M9Plan {
  readonly repos: number
  readonly users: number
  readonly groups: number
  readonly repoPrefix: string
  readonly groupPrefix: string
  readonly userPrefix: string
}

export interface PermissionPrincipals {
  users: Record<string, string[]>
  groups: Record<string, string[]>
}

export interface TargetDef {
  name: string
  body: {
    name: string
    repos: string[]
    includePatterns: string[]
    excludePatterns: string[]
    principals: PermissionPrincipals
  }
}

export interface SeedM9Result {
  base: string
  repos: { key: string; status: number }[]
  groups: { name: string; status: number }[]
  users: { name: string; status: number }[]
  targets: { name: string; status: 'created' | 'present' }[]
  elapsedMs: number
}

export interface VerifyM9Result {
  ok: boolean
  problems: string[]
  evidence: {
    repoCount: number
    userCount: number
    groupCount: number
    targets: string[]
    u8: { granted: number; denied: number }
    u9: { covered: number; uncovered: number }
  }
}

export declare const M9_PLAN: M9Plan
export declare function repoKeys(plan?: M9Plan): string[]
export declare function groupNames(plan?: M9Plan): string[]
export declare function userNames(plan?: M9Plan): string[]
export declare function userPassword(name: string): string
export declare function groupsForUser(name: string, plan?: M9Plan): string[]
export declare function m9Targets(keys?: string[]): TargetDef[]
export declare function ensureGroup(client: SeedClient, name: string, description?: string): Promise<number>
export declare function ensureM9User(client: SeedClient, name: string, groups?: string[]): Promise<number>
export declare function ensureTarget(client: SeedClient, def: TargetDef): Promise<'created' | 'present'>
export declare function seedM9(client: SeedClient, opts?: { plan?: M9Plan }): Promise<SeedM9Result>
export declare function verifyM9(client: SeedClient, opts?: { plan?: M9Plan }): Promise<VerifyM9Result>
// T-253 usage-content exports (consumed by the T-258 usage-fanout spec)
export declare const USAGE_SEED_PATH: string
export declare function usageSeedBody(key: string): string
export declare function seedUsageContent(client: SeedClient, keys: string[]): Promise<number>
