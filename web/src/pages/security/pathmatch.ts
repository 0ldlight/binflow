// 权限 pattern 判定（console-ux §4.9[2]/§9-R8：模式测试器与服务端
// internal/auth/pathmatch.go 必须同源）。
//
// 本文件是 pathmatch.go 的逐语义 TypeScript 移植：
//   - pattern "" / "**" / "**/*" 匹配一切路径；
//   - '*' 跨段内任意字符（不跨 '/'）；'**' 跨任意段（Ant 回溯）；
//   - 以 '/**' 结尾的 pattern 匹配目录自身与其下一切；
//   - 目录前缀规则（Ant matchStart）仅在路径本身是目录（尾 '/'）时生效
//     ——文件路径必须整路径全匹配（B-2 修复语义）；
//   - 大小写敏感；段两侧空白在比较前裁剪（Ant token trimming）。
//
// 同源性由两道闸保证（无后端端点，ux R8 定案方案）：
//   1. pathmatch.fixtures.ts 由 scripts/gen-pathmatch-fixtures.mjs 从
//      internal/auth/pathmatch_test.go 的 table-driven 用例生成（漂移即红）；
//   2. e2e/pathmatch-parity.spec.ts 对全部 fixtures 断言本实现逐条一致。
//
// evaluatePath 在匹配之上叠加 authorizer.targetCovers 的组合规则：
// 空 includes = 匹配全部；任一 exclude 命中一票否决（exclude 优先）。

export interface PatternVerdict {
  pattern: string
  hit: boolean
}

export interface PathEvaluation {
  /** 每个 include pattern 的命中明细（§4.9「逐条命中明细」） */
  includes: PatternVerdict[]
  /** 每个 exclude pattern 的命中明细 */
  excludes: PatternVerdict[]
  /** includes 为空时的「匹配全部」语义由 includesEmpty 表达 */
  includesEmpty: boolean
  /** 命中的第一条 include（includesEmpty 时为 null） */
  includedBy: string | null
  /** 命中的第一条 exclude（一票否决项） */
  excludedBy: string | null
  /** 最终判定（exclude 优先） */
  match: boolean
}

function splitSegments(s: string): string[] {
  const segs: string[] = []
  for (const raw of s.split('/')) {
    const seg = raw.trim()
    if (seg !== '') segs.push(seg)
  }
  return segs
}

/** 段内 '*' 通配：首尾字面量锚定 + 中间字面量按序出现（Ant 段语义） */
function wildcardEqual(pattern: string, s: string): boolean {
  const parts = pattern.split('*')
  if (parts.length === 1) return pattern === s
  if (!s.startsWith(parts[0])) return false
  s = s.slice(parts[0].length)
  const last = parts[parts.length - 1]
  if (!s.endsWith(last)) return false
  s = s.slice(0, s.length - last.length)
  for (const mid of parts.slice(1, -1)) {
    if (mid === '') continue
    const idx = s.indexOf(mid)
    if (idx < 0) return false
    s = s.slice(idx + mid.length)
  }
  return true
}

function segmentMatch(pattern: string, segment: string): boolean {
  if (!pattern.includes('*')) return pattern === segment
  return wildcardEqual(pattern, segment)
}

/** '**' 回溯匹配（与 pathmatch.go segsMatch 逐行对应） */
function segsMatch(pSegs: string[], tSegs: string[], isFolder: boolean): boolean {
  let pi = 0
  let ti = 0
  let lastPlain = false
  while (pi < pSegs.length) {
    const seg = pSegs[pi]
    if (seg === '**') {
      const rest = pSegs.slice(pi + 1)
      for (let skip = ti; skip <= tSegs.length; skip++) {
        if (segsMatch(rest, tSegs.slice(skip), isFolder)) return true
      }
      return false
    }
    if (ti >= tSegs.length) return false
    if (!segmentMatch(seg, tSegs[ti])) return false
    lastPlain = !seg.includes('*')
    pi++
    ti++
  }
  if (ti === tSegs.length) return true
  // pattern 耗尽而路径还有余段：目录前缀规则只对目录路径生效
  return isFolder && lastPlain && pi > 0
}

/** 单 pattern 匹配（pathMatcher.match 移植；path 为 repo 相对路径，无前导 '/'） */
export function pathMatch(pattern: string, path: string): boolean {
  if (pattern === '' || pattern === '**' || pattern === '**/*') return true
  path = path.trim()
  if (path === '') return false
  const isFolder = path.endsWith('/')
  return segsMatch(splitSegments(pattern), splitSegments(path), isFolder)
}

export function matchesAny(patterns: string[], path: string): boolean {
  return patterns.some((p) => pathMatch(p, path))
}

/**
 * 模式测试器判定（§4.9 线框）：
 * 空 includes = 匹配全部路径（auth-model §4 / targetCovers 同款）；
 * 任一 exclude 命中即否决（exclude 优先）。
 */
export function evaluatePath(includes: string[], excludes: string[], path: string): PathEvaluation {
  const inc = includes.map((pattern) => ({ pattern, hit: pathMatch(pattern, path) }))
  const exc = excludes.map((pattern) => ({ pattern, hit: pathMatch(pattern, path) }))
  const includesEmpty = includes.length === 0
  const hitInc = inc.find((v) => v.hit) ?? null
  const hitExc = exc.find((v) => v.hit) ?? null
  const included = includesEmpty || hitInc !== null
  const match = included && hitExc === null
  return {
    includes: inc,
    excludes: exc,
    includesEmpty,
    includedBy: hitInc?.pattern ?? null,
    excludedBy: hitExc?.pattern ?? null,
    match,
  }
}
