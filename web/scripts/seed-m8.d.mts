// Type surface of scripts/seed-m8.mjs for the e2e TS world (tsconfig includes
// e2e/, strict). Hand-written declaration next to the implementation so the
// .mjs itself stays plain Node ESM (the scripts/ eslint block).

export interface RoleFixture {
  name: string
  password: string
  adminRole: 'admin' | 'user' | 'readonly_admin'
  provisioned: boolean
}

export interface RoleFixtures {
  admin: RoleFixture
  user: RoleFixture
  readonly_admin: RoleFixture
}

export interface SeedClient {
  base: string
  request(
    method: string,
    path: string,
    opts?: { body?: unknown; headers?: Record<string, string>; raw?: boolean },
  ): Promise<{ status: number; text: string }>
  /** Non-throwing authenticated GET riding the client's own credential
   * (verify legs observe non-2xx statuses; T-326 D-9②). */
  probeGet(path: string): Promise<{ status: number; text: string }>
}

export interface AdminCredential {
  username: string
  password: string
}

export interface TreePlan {
  root: string
  wideDirs: number
  wideFilesPerDir: number
  chains: number
  chainDepth: number
}

export interface RepoDef {
  key?: string
  rclass?: string
  packageType?: string
  description?: string
}

export interface SeedAllResult {
  base: string
  repoKey: string
  users: { role: string; name: string; status: number }[]
  grant: 'created' | 'present'
  repos: { key: string; status: number }[]
  tree: { planned: number; verified: number } | null
  treeMs: number
  elapsedMs: number
}

export declare const M8_ROLE_USERS: RoleFixtures
export declare const TREE_PLAN: TreePlan

export declare function roleFixturesFromEnv(env?: Record<string, string | undefined>): RoleFixtures
export declare function adminCredential(env?: Record<string, string | undefined>): AdminCredential
export declare function converge<T>(fn: () => Promise<T>, opts?: { attempts?: number; baseDelayMs?: number }): Promise<T>
export declare function makeClient(opts: { base?: string; username: string; password: string }): SeedClient
export declare function ensureUser(
  client: SeedClient,
  user: { name: string; password: string; adminRole?: string },
): Promise<number>
export declare function ensureReadGrant(client: SeedClient, userName: string, repoKey?: string): Promise<'created' | 'present'>
export declare function seedRepos(client: SeedClient, defs: RepoDef[]): Promise<{ key: string; status: number }[]>
export declare function plannedNodeCount(plan?: TreePlan): number
export declare function seedTree(
  client: SeedClient,
  repoKey: string,
  opts?: { minNodes?: number; concurrency?: number; plan?: TreePlan },
): Promise<{ planned: number; verified: number }>
export declare function countTreeNodes(client: SeedClient, repoKey: string, path: string): Promise<number>
export declare function seedAll(
  client: SeedClient,
  opts?: { repoKey?: string; skipTree?: boolean },
): Promise<SeedAllResult>
