// GENERATED FILE — 不要手改。
// 事实源：internal/auth/pathmatch_test.go（TestPathMatcherMatch 表）。
// 再生成：cd web && node scripts/gen-pathmatch-fixtures.mjs
// 漂移检查：cd web && node scripts/gen-pathmatch-fixtures.mjs --check（CI 挂钩归 T-89 面）
// 消费者：e2e/pathmatch-parity.spec.ts（前端 pathmatch.ts 与 Go 判定逐条一致的断言锚）。

export interface PathMatchFixture {
  pattern: string
  path: string
  want: boolean
}

export const pathMatchFixtures: PathMatchFixture[] = [
  { pattern: "", path: "a/b.bin", want: true },
  { pattern: "**", path: "a/b.bin", want: true },
  { pattern: "**", path: "", want: true },
  { pattern: "**/*", path: "a/b.bin", want: true },
  { pattern: "**/*", path: "a.bin", want: true },
  { pattern: "ci-out/**", path: "ci-out", want: true },
  { pattern: "ci-out/**", path: "ci-out/", want: true },
  { pattern: "ci-out/**", path: "ci-out/y.bin", want: true },
  { pattern: "ci-out/**", path: "ci-out/sub/deep/z.bin", want: true },
  { pattern: "ci-out/**", path: "ci-outside.bin", want: false },
  { pattern: "ci-out/**", path: "other/y.bin", want: false },
  { pattern: "ci-out", path: "ci-out/", want: true },
  { pattern: "ci-out", path: "ci-out/y.bin", want: false },
  { pattern: "ci-out", path: "ci-out/sub/", want: true },
  { pattern: "ci-out", path: "ci-out/sub/z.bin", want: false },
  { pattern: "ci-out", path: "other/y.bin", want: false },
  { pattern: "a/*/c", path: "a/b/c/d", want: false },
  { pattern: "a/*/c", path: "a/b/c/", want: true },
  { pattern: "acme/artifact.bin", path: "acme/artifact.bin/evil", want: false },
  { pattern: "acme/artifact.bin", path: "acme/artifact.bin/evil/", want: true },
  { pattern: "**/release", path: "x/release/inner", want: false },
  { pattern: "**/release", path: "x/release/inner/", want: true },
  { pattern: "*.bin", path: "a.bin", want: true },
  { pattern: "*.bin", path: "a/b.bin", want: false },
  { pattern: "ci-*", path: "ci-out", want: true },
  { pattern: "ci-*", path: "ci", want: false },
  { pattern: "a/*/c", path: "a/b/c", want: true },
  { pattern: "a/*/c", path: "a/b/d/c", want: false },
  { pattern: "a/**/c", path: "a/c", want: true },
  { pattern: "a/**/c", path: "a/x/c", want: true },
  { pattern: "a/**/c", path: "a/x/y/c", want: true },
  { pattern: "a/**/c", path: "a/x/d", want: false },
  { pattern: "acme/artifact.bin", path: "acme/artifact.bin", want: true },
  { pattern: "acme/artifact.bin", path: "acme/artifact2.bin", want: false },
  { pattern: "/ci-out/**", path: "ci-out/a.bin", want: true },
  { pattern: " ci-out/** ", path: "ci-out/a.bin", want: true },
]
