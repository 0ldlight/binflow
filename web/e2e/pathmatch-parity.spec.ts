import { expect, test } from '@playwright/test'

// T-101 AC②：权限模式测试器的判定向量与 internal/auth pathmatch 同源
//（console-ux §9-R8 定案方案——无后端端点，走 fixtures）。
//
// fixtures 由 web/scripts/gen-pathmatch-fixtures.mjs 从 internal/auth/
// pathmatch_test.go 的 table-driven 用例生成（--check 漂移即红）；本 spec
// 对前端实现逐条断言。纯 Node 断言（无浏览器/后端依赖，Playwright 直跑
// TS import）。

import { evaluatePath, matchesAny, pathMatch } from '../src/pages/security/pathmatch'
import { pathMatchFixtures } from '../src/pages/security/pathmatch.fixtures'
import { parseReferencedTargets } from '../src/pages/security/api'

test('frontend path matcher is fixture-parity with internal/auth pathmatch_test.go', () => {
  expect(pathMatchFixtures.length).toBeGreaterThanOrEqual(36)
  for (const f of pathMatchFixtures) {
    const got = pathMatch(f.pattern, f.path)
    if (got !== f.want) {
      throw new Error(`pathMatch(${JSON.stringify(f.pattern)}, ${JSON.stringify(f.path)}) = ${got}, want ${f.want}`)
    }
  }
})

test('matchesAny mirrors pathmatch.go semantics (empty list matches nothing)', () => {
  expect(matchesAny(['a/**', 'b/**'], 'a/x.bin')).toBe(true)
  expect(matchesAny(['a/**', 'b/**'], 'b/x.bin')).toBe(true)
  expect(matchesAny(['a/**'], 'c/x.bin')).toBe(false)
  expect(matchesAny([], 'a/x.bin')).toBe(false)
})

test('evaluatePath composes like authorizer.targetCovers: empty includes = all, exclude wins', () => {
  // exclude 优先（§4.9 线框的示例行）
  const hit = evaluatePath(['ci-out/**'], ['ci-out/tmp/**'], 'ci-out/builds/42/app.bin')
  expect(hit.match).toBe(true)
  expect(hit.includedBy).toBe('ci-out/**')
  expect(hit.excludedBy).toBeNull()

  const excluded = evaluatePath(['ci-out/**'], ['ci-out/tmp/**'], 'ci-out/tmp/x.bin')
  expect(excluded.match).toBe(false)
  expect(excluded.includedBy).toBe('ci-out/**')
  expect(excluded.excludedBy).toBe('ci-out/tmp/**') // exclude 一票否决

  // 空 includes = 匹配全部（auth-model §4）
  expect(evaluatePath([], [], 'anything/here.bin').match).toBe(true)
  // 注意 `**/tmp` 不匹配 `a/tmp/x`（** 后仍需整段 tmp 对齐且路径须耗尽），
  // 排除 tmp 下一切要用 `**/tmp/**`——Ant 回溯语义，正是测试器要可见化的点
  expect(evaluatePath([], ['**/tmp'], 'a/tmp/x').match).toBe(true)
  expect(evaluatePath([], ['**/tmp/**'], 'a/tmp/x').match).toBe(false)

  // 无 include 命中
  expect(evaluatePath(['release/*'], [], 'qa/x.bin').match).toBe(false)

  // 目录前缀规则：文件路径不因「模式恰好命名其祖先目录」获得匹配（B-2）
  expect(evaluatePath(['ci-out'], [], 'ci-out/y.bin').match).toBe(false)
  expect(evaluatePath(['ci-out'], [], 'ci-out/').match).toBe(true)
})

// review B1 回归：409 文案解析不得在含点 target 名上截断（服务端句式
// 尾部锚定；reviewer 三组实测向量 + 服务端原文整句）。
test('parseReferencedTargets keeps dotted target names whole (review B1)', () => {
  const srv = (targets: string) =>
    `Cannot delete group 'qa': it is referenced by permission target(s): ${targets}. Remove the group from those targets first.`
  expect(parseReferencedTargets(srv('t1, t2'))).toEqual(['t1', 't2'])
  expect(parseReferencedTargets(srv('qa.build'))).toEqual(['qa.build'])
  expect(parseReferencedTargets(srv('qa.build, plain'))).toEqual(['qa.build', 'plain'])
  expect(parseReferencedTargets(srv('a.b.c, d.e, f'))).toEqual(['a.b.c', 'd.e', 'f'])
  // 非匹配句式回退空数组（调用方呈现原始 message）
  expect(parseReferencedTargets('some other error shape')).toEqual([])
})
