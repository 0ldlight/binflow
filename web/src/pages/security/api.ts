// 安全域 API（T-101；契约源 internal/httpapi/security.go + groups.go +
// permissions.go，T-97 定案形态；M9 ADR-0030 E2~E5 加宽/新增面随 T-251/252
// 落地，本文件 T-257 消费）：
//
// - users：GET 列表 **E2 加宽形态** {name,uri,realm,source,email,adminRole,
//   enabled,groups}（enabled 恒渲染；groups 空成员集 = [] 恒非 null）——
//   users 页列表与 groups 页成员推导的单请求数据源（FR-78 N+1 退役）；
//   GET {name} 全量回显 + **E3 enabled**（DB 行事实，读侧闭环 T-208 写侧）；
//   PUT {name} = create-or-replace（201 无 body 两态——口令必填，适合建
//   用户）；POST {name} = 部分更新（email/password/admin/groups 指针语义：
//   absent = 保持，groups:[] = 清空——组员维护与口令重置走这条，不需旧
//   口令，admin 路由门）；DELETE {name} = **E4**（200 纯文本成功文案；
//   404 User not found 纯文本——重复删除是确定性 404〔有意非幂等，ADR-0030
//   §14.1 E4 pre-probe 形态〕；内置 admin / 最后一个 admin / 自删 = 400
//   纯文本护栏；级联：组员/授权/token/会话同事务删除，审计保留）。
// - groups：GET/PUT(201 建 / 200 更新 描述)/DELETE（被 permission target
//   引用 → 409，message 列 target 名；成功文案纯文本）；GET {name} 的
//   **E5 ?includeUsers=true** 增 userNames[]（恒渲染，空组 = []；字面
//   `true` 才开，其余拼法按 off 处理）——组编辑器穿梭的按需单组读。
//   groups 列表不加宽（ADR-0030 K19：membersCount/groups[] 双物化面必
//   漂移；成员汇总单源 = user_groups 行 → E2 users.groups 投影 + E5 按需）。
// - permissions：GET 列表（principals.users/groups 双栏回显）/ POST
//   （create-or-replace，201）/ DELETE {name}（204）。无单查端点——编辑器
//   从列表过滤。
//
// 错误体：用户/组/权限管理面是纯文本层（api 层统一收进 ApiError.message，
// 三种格式不暴露给组件，console-ux §1.3/§5.1）。

import { apiJSON, apiText } from '../../lib/api'
import type { AdminRole } from '../../lib/api'

// ---- users（E-19 / SE-05/06） ----

export interface UserListItem {
  name: string
  uri: string
  realm: string
  /** E2 加宽（T-251，恒渲染）：列表行全量事实——users 页单请求渲染的依据 */
  source: string
  email: string
  /** snake 三值闭集；与 admin 布尔一致（admin ⇔ adminRole=admin） */
  adminRole: string
  /** DB 行事实恒渲染（T-208 写侧的读侧闭环）——Status 列真值 */
  enabled: boolean
  /** 成员集，空 = [] 恒非 null（groups 页成员计数的客户端推导源，K19） */
  groups: string[]
}

export interface UserDetail {
  name: string
  email: string
  admin: boolean
  /** M7 闭集角色回显（snake 三值；与 admin 布尔一致：admin ⇔ adminRole=admin） */
  adminRole: string
  /** E3 回显（T-251，恒渲染）：DB 行事实——编辑器 enabled 控件回显驱动 */
  enabled: boolean
  groups: string[]
  lastLoggedIn?: string
  realm: string
  profileUpdatable: boolean
  internalPasswordDisabled: boolean
  disableUIAccess: boolean
}

/** PUT 体（create-or-replace：口令必填，admin 位与组员全量替换）。
 *  adminRole/enabled 为可选增补位（服务端 userCreateBody 实存）：
 *  adminRole 携带时须与 admin 布尔一致（resolveCreateRole 对校验）。 */
export interface UserReplaceBody {
  name: string
  email: string
  password: string
  admin: boolean
  adminRole?: AdminRole
  enabled?: boolean
  groups: string[]
}

/** POST 体（部分更新：仅携带要改的字段——指针语义的前端侧落法）。
 *  adminRole（M7）：单独携带时是完整的角色陈述；与 admin 布尔同时携带
 *  必须一致（admin=true ⇔ adminRole=admin），否则服务端 400——UI 角色下拉
 *  只走 adminRole 通道，永不与布尔混发。类型收紧为闭集 AdminRole 是
 *  T-218 尾债②收口（wire 不变，编译期挡住拼写错误）。enabled（T-208
 *  seam）：指针语义，携带即写入，absent 保持。 */
export interface UserUpdateBody {
  name?: string
  email?: string
  password?: string
  admin?: boolean
  adminRole?: AdminRole
  groups?: string[]
  enabled?: boolean
}

export function listUsers(): Promise<UserListItem[]> {
  return apiJSON<UserListItem[]>('/security/users')
}

export function getUser(name: string): Promise<UserDetail> {
  return apiJSON<UserDetail>(`/security/users/${encodeURIComponent(name)}`)
}

/** 创建（PUT /{name}，201 无 body 两态——重名即整体替换，本函数仅建新用） */
export function createUser(name: string, body: UserReplaceBody): Promise<string> {
  return apiText(`/security/users/${encodeURIComponent(name)}`, { method: 'PUT', body })
}

/** 部分更新（POST /{name}，200 无 body） */
export function updateUser(name: string, body: UserUpdateBody): Promise<string> {
  return apiText(`/security/users/${encodeURIComponent(name)}`, { method: 'POST', body })
}

/** 删除用户（E4，DELETE /{name}，200 纯文本 `The user: '<name>' has been
 *  removed successfully.`）。护栏 400/404 均纯文本体经 ApiError.message
 *  原样上浮：未知名/已被他人删除 → 404 `User not found`（重复删除是有意的
 *  确定性 404，非幂等——ADR-0030 §14.1 E4）；内置 admin / 最后一个 admin /
 *  自删 → 400 各自文案（调用方如实呈现，UI 侧仅对自删/内置预禁用入口）。 */
export function deleteUser(name: string): Promise<string> {
  return apiText(`/security/users/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

// ---- groups（SE-01~04） ----

export interface GroupListItem {
  name: string
  uri: string
  description: string
}

export function listGroups(): Promise<GroupListItem[]> {
  return apiJSON<GroupListItem[]>('/security/groups')
}

export function getGroup(name: string): Promise<GroupListItem> {
  return apiJSON<GroupListItem>(`/security/groups/${encodeURIComponent(name)}`)
}

/** E5 带参形态（T-252）：`?includeUsers=true` 增 `userNames`（恒渲染，空组
 *  = [] 恒非 null；服务端仅字面 `true` 开，其余拼法按 off 处理——本函数恒
 *  开）。组编辑器成员穿梭的按需单组读（ADR-0030 §14.1 E5：组编辑器穿梭
 *  = E5；users 页 Groups 列 = E2）。未知组 404 `Group not found`。 */
export interface GroupDetailWithUsers extends GroupListItem {
  userNames: string[]
}

export function getGroupWithUsers(name: string): Promise<GroupDetailWithUsers> {
  return apiJSON<GroupDetailWithUsers>(
    `/security/groups/${encodeURIComponent(name)}?includeUsers=true`,
  )
}

/** 创建或更新描述（PUT：新建 201 / 已存在 200，均无 body） */
export function putGroup(name: string, description: string): Promise<string> {
  return apiText(`/security/groups/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: { name, description },
  })
}

/** 删除（200 纯文本；被 permission target 引用 → 409 message 列 target 名） */
export function deleteGroup(name: string): Promise<string> {
  return apiText(`/security/groups/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

/**
 * 从 409 文案中提取引用 target 名（服务端句式：
 * "Cannot delete group 'x': it is referenced by permission target(s): a, b.
 *  Remove the group from those targets first."）。解析失败回空数组——
 * 调用方回退呈现原始 message。
 *
 * 尾部锚定完整固定后缀（review B1）：target 名无字符集限制（permissions.go
 * 仅校验非空），含点名（如 `qa.build`）在任意句点截断的旧写法会把
 * `qa.build, plain` 解析成 `['qa']`——多 target 场景整段丢名，冲突面板
 * 链接直达 404。锚定 `". Remove the group from those targets first."$"
 * 后捕获组只能停在名单真正的结尾。
 */
export function parseReferencedTargets(message: string): string[] {
  const m = /permission target\(s\):\s*(.+?)\.\s+Remove the group from those targets first\.$/.exec(message)
  if (!m) return []
  return m[1]
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '')
}

// ---- permission targets（E-24 / SE-07；M7 动作集扩 manage，T-217； ----
// ---- T-455 五列动词域：annotate 独立位 + write→deploy-cache 双层词表） ----

/**
 * 动作集五值（T-455，parity B-1.6 翻正——Q7）：UI 矩阵列序 =
 * read / annotate / write / delete / manage（7.161.20 活体列序对位：
 * Repositories 资源型 Read / Annotate / Deploy/Cache / Delete/Overwrite /
 * Manage；BinFlow 列头用正名单词形 + tooltip 注 7.161 标签，探针证据
 * reports/agents/t455-probe/）。
 *
 * wire 双层（T-444 / ADR-0044 K68.2）：GET 回显**正名单单形**
 * read / deploy-cache / annotate / delete / manage；PUT 收五词 + `write`
 * 别名（= deploy-cache，**不附带 annotate**——拆分语义，零提权）。本类型的
 * 'write' 是 FE 展示/勾选域词：水合归一（deploy-cache→write）与保存序列化
 * （write→deploy-cache）各自单点收口在 normalizePermActions / wireActions。
 * annotate = 属性写位（properties PUT/DELETE 门），不隐含内容写；write =
 * 部署位，不携带 annotate——两列独立勾选（活体联动形态在 0 仓参照实例不可
 * 观测，矩阵门在资源型+行选定之后——登记不伪造；BinFlow 语义独立位列）。
 */
export type PermAction = 'read' | 'annotate' | 'write' | 'delete' | 'manage'
export const PERM_ACTIONS: PermAction[] = ['read', 'annotate', 'write', 'delete', 'manage']

/** wire 动作词（GET 回显正名单 + PUT 收词超集含 write 别名）。
 *  GET 侧 principals 值的诚实类型（'deploy-cache' 不在 PermAction 里）。 */
export type WireAction = 'read' | 'write' | 'deploy-cache' | 'annotate' | 'delete' | 'manage'

/** wire 回显正名单序（GET echo 按此序；wireActions 输出同序，往返稳定） */
const WIRE_ACTION_ORDER: readonly WireAction[] = ['read', 'deploy-cache', 'annotate', 'delete', 'manage']

/** wire → UI 词归一：'deploy-cache' → 'write'（'write' 别名同收），闭集外
 *  丢弃、去重、保序。T-444 兼容窗的展示面收口点——编辑器水合与授权汇总
 *  （grantsOf*）都过这里，勾选渲染不再直接 includes('write') 漏 deploy-cache。 */
export function normalizePermActions(raw: readonly string[]): PermAction[] {
  const out: PermAction[] = []
  for (const w of raw) {
    const a: PermAction | null =
      w === 'write' || w === 'deploy-cache' ? 'write' : (PERM_ACTIONS as readonly string[]).includes(w) ? (w as PermAction) : null
    if (a !== null && !out.includes(a)) out.push(a)
  }
  return out
}

/** UI → wire 词序列化：'write' → 正名单 'deploy-cache'（别名收词两形等效，
 *  发正名单形与 GET 回显同形——diff/快照比较稳定）；正名单序输出。 */
export function wireActions(actions: readonly PermAction[]): WireAction[] {
  const set = new Set(actions)
  return WIRE_ACTION_ORDER.filter((w) => (w === 'deploy-cache' ? set.has('write') : set.has(w as PermAction)))
}

export interface PermPrincipals {
  users: Record<string, WireAction[]>
  groups: Record<string, WireAction[]>
}

export interface PermissionTarget {
  name: string
  repos: string[]
  includePatterns: string[]
  excludePatterns: string[]
  principals: PermPrincipals
}

export interface PermissionTargetBody extends PermissionTarget {
  /** 新建/替换同名 target（POST /v1/permissions 是唯一的写端点） */
  name: string
}

export function listPermissionTargets(): Promise<PermissionTarget[]> {
  return apiJSON<PermissionTarget[]>('/v1/permissions')
}

/** E6 `GET /api/v1/permissions?filter=manage`（T-254 / ADR-0030 §14.1.6）：
 *  m-holder（普通 user 持 manage）的可达读臂——族 4 写臂（覆盖集内 POST/
 *  DELETE）的读臂对称补全。**条目字段与全量列表一致**（name/repos/
 *  patterns/principals 全渲染——编辑器注水面）。服务端三分支：
 *  admin/readonly_admin → 全量（与无 filter 响应等价）；普通用户且 manage
 *  覆盖集非空 → 仅 repos ⊆ 覆盖集 的 target 子集（**部分覆盖的 target
 *  隐藏**——B1 替换臂必败不诱导；覆盖集外元数据零出现，NFR-S49）；
 *  覆盖集为空 → 403（与无 filter 同形同文案，零新增可区分面）——调用方
 *  （列表页/编辑器）以 403 收敛 L2，即「无 manage / 覆盖集空」的普通 user
 *  形态；200 空数组 = 覆盖集非空但无完全落入的 target（友好空态）。 */
export function listPermissionTargetsManaged(): Promise<PermissionTarget[]> {
  return apiJSON<PermissionTarget[]>('/v1/permissions?filter=manage')
}

/** 保存（POST /v1/permissions，create-or-replace——201 无 body） */
export function savePermissionTarget(body: PermissionTargetBody): Promise<string> {
  return apiText('/v1/permissions', { method: 'POST', body })
}

export function deletePermissionTarget(name: string): Promise<void> {
  return apiJSON<void>(`/v1/permissions/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

// ---- 名称规则（前端预检，服务端终裁；与 groups.go validateGroupName 同口径） ----

/**
 * 组名规则：非空 / ≤64 / ^[a-z][a-z0-9._-]*$ / 保留字 {anonymous,_system_}。
 * 返回错误文案；null = 通过。
 */
export function validateGroupName(name: string): string | null {
  if (name === '') return '组名不能为空'
  if (name === 'anonymous' || name === '_system_') return `「${name}」是保留名`
  if (name.length > 64) return '组名最长 64 个字符'
  if (!/^[a-z][a-z0-9._-]*$/.test(name)) {
    return '组名需以小写字母开头，其后为小写字母 / 数字 / 点 / 下划线 / 连字符'
  }
  return null
}

/** 用户名规则（服务端：全小写 + 非保留 + 非空；email/口令必填在提交链校验） */
export function validateUserName(name: string): string | null {
  if (name === '') return '用户名不能为空'
  if (name === '_system_') return `「${name}」是保留名`
  if (name !== name.toLowerCase()) return '用户名必须全小写（服务端拒绝混合大小写拼写）'
  if (/\s/.test(name)) return '用户名不能包含空白字符'
  return null
}

// ---- 主体授权汇总（T-237；console-m8 §6.9[5]/§6.10 组权限矩阵）----
// 只读汇总：把 permission targets 列表折叠成「某主体在每个 target 上的
// 五动作视图」（T-455 起含 annotate）。对用户 = 直接行 + 经所属组行
// （Artifactory User Permissions Tab 的 Applied To 语义）；对组 = 该组的行。
// 纯前端计算，零新端点。

export interface PrincipalGrantRow {
  /** permission target 名 */
  target: string
  /** 授权途径：'direct'（主体直接在 principals 上）或组名（经该组） */
  sources: string[]
  /** 各途径动作的并集（UI 五词域，r/a/w/d/m 序——归一见 normalizePermActions） */
  actions: PermAction[]
}

/** 组的授权行（§6.10 编辑态组权限矩阵；manage 徽章数据源同此）。wire 词
 *  （deploy-cache/write）经 normalizePermActions 归一为 UI 五词域。 */
export function grantsOfGroup(targets: PermissionTarget[], group: string): PrincipalGrantRow[] {
  const rows: PrincipalGrantRow[] = []
  for (const t of targets) {
    const actions = t.principals.groups[group]
    if (!actions || actions.length === 0) continue
    rows.push({ target: t.name, sources: ['direct'], actions: normalizePermActions(actions) })
  }
  return rows
}

/** 用户的授权行：直接 + 经组（Artifactory Applied To 形态）；并集在 UI 词域 */
export function grantsOfUser(
  targets: PermissionTarget[],
  user: string,
  userGroups: readonly string[],
): PrincipalGrantRow[] {
  const rows: PrincipalGrantRow[] = []
  for (const t of targets) {
    const sources: string[] = []
    const actions = new Set<PermAction>()
    const direct = t.principals.users[user]
    if (direct && direct.length > 0) {
      sources.push('direct')
      for (const a of normalizePermActions(direct)) actions.add(a)
    }
    for (const g of userGroups) {
      const via = t.principals.groups[g]
      if (via && via.length > 0) {
        sources.push(g)
        for (const a of normalizePermActions(via)) actions.add(a)
      }
    }
    if (sources.length === 0) continue
    rows.push({ target: t.name, sources, actions: PERM_ACTIONS.filter((a) => actions.has(a)) })
  }
  return rows
}

/** 主体是否在任何 target 上持有 manage——组页 adminPrivileges 徽章的数据源。
 *  BinFlow 组模型无 Artifactory 的 adminPrivileges 布尔（rbac-model §5
 *  有意不跟进）；manage（仓库配置派生权，ADR-0026）是其最小诚实同构。 */
export function holdsManage(rows: readonly PrincipalGrantRow[]): boolean {
  return rows.some((r) => r.actions.includes('manage'))
}
