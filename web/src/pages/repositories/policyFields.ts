// deb/rpm/helm 索引引擎策略键的字段册（M12 T-353，FR-113.2/113.5 FE 腿）。
//
// REST 透传已就位（T-327R 的 deb/rpm 六+四键 + T-329 D-E 的 helm 强制布局
// 对——internal/httpapi/repositories.go repoConfig 的扁平 POINTER 字段，仅
// LOCAL 臂收集），本册只加表单面：数据驱动单渲染器消费（authconfig/
// sections.ts 的字段册先例），呈现位在仓库表单「高级」分区（local × 对应
// 包类型才渲染）。
//
// wire 键 = adapter 探针的 Artifactory 扁平拼写（无子对象）。存储是
// package-type-agnostic 的（debian 键存进 rpm 仓也只是被忽略——caller-owned
// blob 姿态），但读取方只有对应 adapter——表单按包类型收窄呈现，不提供假入口。
//
// 全量替换语义下的提交纪律（与既有表单一致）：
// - check/number 键**恒提交**（POINTER 语义：显式 false/0 ≠ 缺省——
//   calculateYumMetadata 的 flip-off 更新必须过 round trip，否则 RP-2 的
//   409 分支永远关不掉）；编辑态从 GET configuration 回显逐键预填。
// - text 空串 = 剔除（adapter 归一回默认）；byHash 是 K45 定案的值域闭集
//   （ALL/SHA256/NONE，T-346 起服务端 400 终裁）。
// - 锚 = `form-<wire>`（建仓表单 form-${k} 生成器族的键扩容——console-ux
//   §10.5 T-353 批；与远程参数四键同口径）。

export type PolicyPkg = 'debian' | 'rpm' | 'helm'

export type PolicyKind = 'check' | 'number' | 'text' | 'select'

export interface PolicyFieldDef {
  /** wire 键（= 锚动态段 + FormState.policy 的键） */
  wire: string
  label: string
  kind: PolicyKind
  /** select 的闭集值域 */
  options?: readonly string[]
  hint?: string
  placeholder?: string
  /** number 的产品默认（呈现于 hint；表单初始值也用它） */
  dflt?: number
  /** text 的产品默认（hint 呈现；初始输入留空 = 剔除归默认） */
  textDflt?: string
}

/** deb 索引策略（debian.md §3~§5） */
const DEB: PolicyFieldDef[] = [
  {
    wire: 'byHash',
    label: 'byHash（by-hash 索引策略）',
    kind: 'select',
    options: ['NONE', 'SHA256', 'ALL'],
    hint: '值域闭集 NONE / SHA256 / ALL（K45 定案，T-346 起服务端 400 终裁）；缺省 NONE。',
  },
  {
    wire: 'optionalIndexCompressionFormats',
    label: 'optionalIndexCompressionFormats（附加索引压缩格式）',
    kind: 'text',
    placeholder: 'xz, lzma',
    hint: '逗号分隔；空 = 不生成附加压缩索引。',
  },
  {
    wire: 'debianDefaultArchitectures',
    label: 'debianDefaultArchitectures（强制架构族）',
    kind: 'text',
    placeholder: 'i386,amd64',
    hint: 'Release 强制声明的架构族（TL-4）；空 = 产品默认 i386,amd64，"none" 显式退出。',
  },
  {
    wire: 'historyCycles',
    label: 'historyCycles（by-hash 世代保留数）',
    kind: 'number',
    dflt: 3,
    hint: 'by-hash 索引保留的世代数；非负整数，默认 3。',
  },
  {
    wire: 'origin',
    label: 'origin（Release Origin）',
    kind: 'text',
    placeholder: '（默认 = 仓库 key）',
    hint: 'Release 文件的 Origin 字段；空 = 仓库 key。',
  },
  {
    wire: 'label',
    label: 'label（Release Label）',
    kind: 'text',
    placeholder: '（默认 = 仓库 key）',
    hint: 'Release 文件的 Label 字段；空 = 仓库 key。',
  },
]

/** rpm 索引策略（rpm.md §2.4/§3.2/§4） */
const RPM: PolicyFieldDef[] = [
  {
    wire: 'calculateYumMetadata',
    label: 'calculateYumMetadata（RP-2 显式开启）',
    kind: 'check',
    hint: '产品默认 false：yum 元数据按需/显式生成——勾选后部署即计算 repodata。',
  },
  {
    wire: 'yumRootDepth',
    label: 'yumRootDepth（repodata 根深度）',
    kind: 'number',
    dflt: 0,
    hint: 'repodata 生成起算的目录深度；非负整数，默认 0 = 仓库根。',
  },
  {
    wire: 'enableFileListsIndexing',
    label: 'enableFileListsIndexing（filelists 索引）',
    kind: 'check',
    hint: '产品默认 false；开启后 repodata 含 filelists 索引（体积换按文件检索）。',
  },
  {
    wire: 'yumGroupFileNames',
    label: 'yumGroupFileNames（comps 组文件清单）',
    kind: 'text',
    placeholder: 'comps.xml',
    hint: '逗号分隔的 comps 组文件名；默认 comps.xml。',
  },
]

/** helm 强制布局对（helm.md §4.3/S5——T-329 D-E 透传） */
const HELM: PolicyFieldDef[] = [
  {
    wire: 'forceMetadataNameVersion',
    label: 'Enforce Chart Name and Version（forceMetadataNameVersion）',
    kind: 'check',
    hint: '产品默认 false：强制上传路径与 Chart 元数据的 name/version 一致，否则拒绝。',
  },
  {
    wire: 'forceNonDuplicateChart',
    label: 'Prevent Duplicate Chart Paths（forceNonDuplicateChart）',
    kind: 'check',
    hint: '产品默认 false：禁止同路径重复上传同版本 Chart。',
  },
]

export const POLICY_FIELDS: Record<PolicyPkg, PolicyFieldDef[]> = {
  debian: DEB,
  rpm: RPM,
  helm: HELM,
}

/** 该包类型是否携带策略键（表单呈现位与提交位共用判定） */
export function policyPkg(packageType: string): PolicyPkg | null {
  if (packageType === 'debian' || packageType === 'rpm' || packageType === 'helm') return packageType
  return null
}

/** 表单态：键 = wire，值 = check 布尔 / 其余字符串 */
export type PolicyForm = Record<string, string | boolean>

/** 创建态初始（check = 产品默认 false；select/number/text = 空或默认拼写） */
export function initialPolicyForm(pkg: PolicyPkg): PolicyForm {
  const form: PolicyForm = {}
  for (const f of POLICY_FIELDS[pkg]) {
    if (f.kind === 'check') form[f.wire] = false
    else if (f.kind === 'select') form[f.wire] = f.options?.[0] ?? ''
    else form[f.wire] = ''
  }
  return form
}

/** GET configuration 回显 → 表单态（逐键预填；缺键 = 表单默认拼写） */
export function prefillPolicyForm(pkg: PolicyPkg, cfg: Record<string, unknown> | undefined): PolicyForm {
  const form = initialPolicyForm(pkg)
  for (const f of POLICY_FIELDS[pkg]) {
    const v = cfg?.[f.wire]
    if (f.kind === 'check') form[f.wire] = v === true
    else if (f.kind === 'number') form[f.wire] = typeof v === 'number' ? String(v) : ''
    else if (f.kind === 'select') form[f.wire] = typeof v === 'string' && v !== '' ? v : (f.options?.[0] ?? '')
    else if (Array.isArray(v)) form[f.wire] = v.filter((x): x is string => typeof x === 'string').join(', ')
    else form[f.wire] = typeof v === 'string' ? v : ''
  }
  return form
}

/** number 字段的输入校验：空或非负整数合法 */
export function policyNumberValid(pkg: PolicyPkg, form: PolicyForm): boolean {
  return POLICY_FIELDS[pkg].every((f) => {
    if (f.kind !== 'number') return true
    const v = String(form[f.wire] ?? '').trim()
    return v === '' || /^\d+$/.test(v)
  })
}

/**
 * 表单态 → 请求体键。check 恒提交（POINTER 语义，flip-off 必须过 round
 * trip）；number 合法非空才提交（空 = 剔除归默认）；text 空串剔除；
 * optionalIndexCompressionFormats 逗号切数组。
 */
export function policyBodyEntries(pkg: PolicyPkg, form: PolicyForm): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of POLICY_FIELDS[pkg]) {
    const v = form[f.wire]
    switch (f.kind) {
      case 'check':
        out[f.wire] = v === true
        break
      case 'number': {
        const t = String(v ?? '').trim()
        if (t !== '' && /^\d+$/.test(t)) out[f.wire] = Number(t)
        break
      }
      case 'select': {
        const t = String(v ?? '').trim()
        if (t !== '') out[f.wire] = t
        break
      }
      default: {
        if (f.wire === 'optionalIndexCompressionFormats') {
          const arr = String(v ?? '')
            .split(',')
            .map((s) => s.trim())
            .filter((s) => s !== '')
          if (arr.length > 0) out[f.wire] = arr
          break
        }
        const t = String(v ?? '').trim()
        if (t !== '') out[f.wire] = t
        break
      }
    }
  }
  return out
}
