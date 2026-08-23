// Maven GAV → layout 路径的生成与前端预检（T-244 共享层归位：原居
// pages/artifacts/UploadDialog，DeployDialog 经由 components → pages 的
// 反向依赖消费——T-242 seam 注记登记）。与服务端 layout 解析同口径：
// 「至少 3 层目录 + 文件名 = module-version[-classifier].ext」。
//
// 预检失败（error 非空）时调用方不得发起写请求（FR-24-AC5：零写请求）。

export interface GavForm {
  groupId: string
  artifactId: string
  version: string
  classifier: string
  packaging: string
}

/** maven layout 路径生成 + 前端预检（与服务端 Parse 同口径） */
export function mavenTarget(f: GavForm): { dir: string; file: string; error: string | null } {
  const g = f.groupId.trim()
  const a = f.artifactId.trim()
  const v = f.version.trim()
  const c = f.classifier.trim()
  const p = f.packaging.trim() || 'jar'
  if (g === '') return { dir: '', file: '', error: 'groupId 不能为空' }
  if (g.includes('/')) return { dir: '', file: '', error: 'groupId 用点分隔，不能包含 /' }
  if (g.split('.').some((seg) => seg === '')) return { dir: '', file: '', error: 'groupId 有空段（连续点）' }
  if (a === '') return { dir: '', file: '', error: 'artifactId 不能为空' }
  if (a.includes('/') || a.includes(':')) return { dir: '', file: '', error: 'artifactId 不能包含 / 或 :' }
  if (v === '') return { dir: '', file: '', error: 'version 不能为空' }
  if (v === '-SNAPSHOT') return { dir: '', file: '', error: 'version 不能是裸 -SNAPSHOT' }
  if (v.includes('/')) return { dir: '', file: '', error: 'version 不能包含 /' }
  if (c.includes('/') || c.includes('.')) return { dir: '', file: '', error: 'classifier 不能包含 / 或 .' }
  if (p.includes('/') || p.includes('.')) return { dir: '', file: '', error: 'packaging 不能包含 / 或 .' }
  const file = `${a}-${v}${c ? `-${c}` : ''}.${p}`
  const dir = `${g.split('.').join('/')}/${a}/${v}`
  return { dir, file, error: null }
}
