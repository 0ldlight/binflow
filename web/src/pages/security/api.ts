// 安全域 API（T-101；契约源 internal/httpapi/security.go + groups.go +
// permissions.go，T-97 定案形态）：
//
// - users：GET 列表简形态 {name,uri,realm} / GET {name} 全量回显
//   （email/admin/groups，无口令字段）；PUT {name} = create-or-replace
//   （201 无 body 两态——口令必填，适合建用户）；POST {name} = 部分更新
//   （email/password/admin/groups 指针语义：absent = 保持，groups:[] = 清空
//   ——组员维护与口令重置走这条，不需旧口令，admin 路由门）。
// - groups：GET/PUT(201 建 / 200 更新 描述)/POST(仅描述)/DELETE（被
//   permission target 引用 → 409，message 列 target 名；成功文案纯文本）。
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
}

export interface UserDetail {
  name: string
  email: string
  admin: boolean
  /** M7 闭集角色回显（snake 三值；与 admin 布尔一致：admin ⇔ adminRole=admin） */
  adminRole: string
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

// ---- permission targets（E-24 / SE-07；M7 动作集扩 manage，T-217） ----

/** 动作集四值：r/w/d + m（manage = 仓库级 admin 派生位，ADR-0026 决策 3）。
 *  wire 全词形（read/write/delete/manage）；GET 回显按本数组序（r/w/d/m）。 */
export type PermAction = 'read' | 'write' | 'delete' | 'manage'
export const PERM_ACTIONS: PermAction[] = ['read', 'write', 'delete', 'manage']

export interface PermPrincipals {
  users: Record<string, PermAction[]>
  groups: Record<string, PermAction[]>
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
// 四动作视图」。对用户 = 直接行 + 经所属组行（Artifactory User Permissions
// Tab 的 Applied To 语义）；对组 = 该组的行。纯前端计算，零新端点。

export interface PrincipalGrantRow {
  /** permission target 名 */
  target: string
  /** 授权途径：'direct'（主体直接在 principals 上）或组名（经该组） */
  sources: string[]
  /** 各途径动作的并集（r/w/d/m 序） */
  actions: PermAction[]
}

/** 组的授权行（§6.10 编辑态组权限矩阵；manage 徽章数据源同此） */
export function grantsOfGroup(targets: PermissionTarget[], group: string): PrincipalGrantRow[] {
  const rows: PrincipalGrantRow[] = []
  for (const t of targets) {
    const actions = t.principals.groups[group]
    if (!actions || actions.length === 0) continue
    rows.push({ target: t.name, sources: ['direct'], actions })
  }
  return rows
}

/** 用户的授权行：直接 + 经组（Artifactory Applied To 形态） */
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
      for (const a of direct) actions.add(a)
    }
    for (const g of userGroups) {
      const via = t.principals.groups[g]
      if (via && via.length > 0) {
        sources.push(g)
        for (const a of via) actions.add(a)
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
