// 认证配置三段的字段册（T-307，FR-92 FE 腿）。
//
// 字段名/wire 名/分组/顺序对齐 docs/reverse/auth-integration.md v2（§6 页面
// 形态表 + §1.1/§1.2/§3.1 字段表）；OAuth(OIDC) 段是 T-305 落地的 BinFlow
// C 级 issuer 发现式 wire（snake_case），非 Artifactory 多 provider 模型
// （漂移随 T-305 登记，FE 按契约实态渲染）。
//
// 数据驱动单渲染器消费本册：PUT 是**全量替换**（服务端 canonicalize 时缺省
// 键落默认值），所以非 secret 字段必须逐字段回传——册子里每个字段都会进
// payload；唯一例外是 secret（空 = 自 payload 剔除 = 保持库存值，哨兵回传
// 是 400 红线）。

import type { AuthSection } from '../../../lib/api'
import { AUTH_SECRET_SENTINEL } from '../../../lib/api'
import { tr } from '../../../i18n'

const t = tr('admin')

export type FieldKind = 'text' | 'secret' | 'number' | 'check' | 'textarea' | 'list' | 'locked'

export interface FieldDef {
  /** data-testid（authcfg-<proto>-<name>，console-ux §10.5 T-307 批） */
  anchor: string
  /** 呈现标签（术语保留英文原词，§1.2 命名对齐） */
  label: string
  /** wire 路径（点号一级嵌套：LDAP search 子对象） */
  wire: string
  kind: FieldKind
  /** 输入/展示等宽（URL/DN/过滤器/claim 一律 mono——P2 同族标识符） */
  mono?: boolean
  placeholder?: string
  /** 输入下方常驻说明 */
  hint?: string
  /** check：doc 缺键时的表单初始（段默认值；SAML {} 空态路径） */
  wireDefault?: boolean
  /** check：wire 与呈现语义反向（SAML noAutoUserCreation，§3.4 命名陷阱） */
  invert?: boolean
  /** textarea 行数 */
  rows?: number
  /** 撑满整行（证书/URL 类长值） */
  full?: boolean
  /** locked：固定回传值（单段模型 key 恒 "ldap"） */
  fixed?: string
  /** secret：GET 哨兵 → 「已设置」提示行的独立锚（console-ux T-307 批） */
  setAnchor?: string
}

export interface GroupDef {
  title: string
  hint?: string
  fields: FieldDef[]
}

export interface SectionDef {
  id: AuthSection
  tab: string
  head: string
  /** 测试连接块是否带 §1.6 测试信封（testUsername/testPassword） */
  testCreds: boolean
  groups: GroupDef[]
}

// ---- LDAP（§1.1/§1.2 字段序；BinFlow 运行时扩展 = T-305 C 级增补） ----------

const LDAP: SectionDef = {
  id: 'ldap',
  tab: 'LDAP',
  head: t('LDAP 目录认证（用户名口令链的外部优先源）'),
  testCreds: true,
  groups: [
    {
      title: t('常规'),
      fields: [
        { anchor: 'authcfg-ldap-enabled', label: t('Enabled（启用这套设置）'), wire: 'enabled', kind: 'check', wireDefault: true },
        { anchor: 'authcfg-ldap-key', label: t('Settings Name（设置名）'), wire: 'key', kind: 'locked', fixed: 'ldap', hint: t('单段模型：每实例一套 LDAP 设置，key 固定 ldap，不可改名。') },
        { anchor: 'authcfg-ldap-url', label: t('LDAP URL（含 base DN）'), wire: 'ldapUrl', kind: 'text', mono: true, full: true, placeholder: 'ldap://host:389/dc=example,dc=com', hint: t('URL 的路径段就是搜索 base DN（§1.1 #3）；ldap(s):// 之外会被拒绝。') },
        { anchor: 'authcfg-ldap-autocreate', label: t('Auto Create System Users（首次登录自动建用户）'), wire: 'autoCreateUser', kind: 'check', wireDefault: true },
        { anchor: 'authcfg-ldap-allowprofile', label: 'Allow Created Users Access To Profile Page', wire: 'allowUserToAccessProfile', kind: 'check' },
        { anchor: 'authcfg-ldap-paging', label: t('Used Page Results（分页搜索）'), wire: 'pagingSupportEnabled', kind: 'check', wireDefault: true },
        { anchor: 'authcfg-ldap-userdn', label: t('User DN Pattern（直接绑定模板）'), wire: 'userDnPattern', kind: 'text', mono: true, full: true, placeholder: 'uid={0},ou=people,dc=example,dc=com', hint: t('{0} 运行时替换为用户名；与搜索模式互斥——AD/Entra 建议留空走搜索模式。') },
        { anchor: 'authcfg-ldap-emailattr', label: 'Email Attribute', wire: 'emailAttribute', kind: 'text', mono: true, placeholder: 'mail', hint: t('自动创建/更新用户时的 email 来源属性，每次登录比对更新。') },
      ],
    },
    {
      title: t('搜索模式（Search）'),
      hint: t('userDnPattern 留空时按 Search Filter + Search Base 定位用户 DN 再绑定验密；Manager DN 留空 = 匿名只读绑定。'),
      fields: [
        { anchor: 'authcfg-ldap-search-filter', label: t('Search Filter（RFC 2254）'), wire: 'search.searchFilter', kind: 'text', mono: true, full: true, placeholder: '(uid={0})', hint: t('{0} = 用户名；AD 用 (sAMAccountName={0})。') },
        { anchor: 'authcfg-ldap-search-base', label: t('Search Base（相对 URL base DN）'), wire: 'search.searchBase', kind: 'text', mono: true, full: true, placeholder: 'ou=people', hint: t('留空 = 只用 URL 里的 base DN；支持 | 分隔多 base（官方口径）。') },
        { anchor: 'authcfg-ldap-poisoning', label: t('Secure LDAP Search（投毒防护）'), wire: 'ldapPoisoningProtection', kind: 'check', wireDefault: true, hint: t('搜索输入过滤（wire 在段顶层，呈现归搜索组——§6 字段序）。') },
        { anchor: 'authcfg-ldap-search-subtree', label: t('Search Sub Tree（递归子树）'), wire: 'search.searchSubTree', kind: 'check', wireDefault: true },
        { anchor: 'authcfg-ldap-manager-dn', label: t('Manager DN（搜索绑定账号）'), wire: 'search.managerDn', kind: 'text', mono: true, full: true, placeholder: 'cn=admin,dc=example,dc=com', hint: t('执行用户搜索的管理员 DN；留空则匿名只读绑定。') },
        { anchor: 'authcfg-ldap-manager-pw', label: t('Manager Password（搜索绑定密码）'), wire: 'search.managerPassword', kind: 'secret', full: true, setAnchor: 'authcfg-ldap-manager-set' },
      ],
    },
    {
      title: t('BinFlow 运行时扩展（无 Artifactory 对应项）'),
      hint: t('T-305 C 级增补：连接姿态与组/角色映射，随本面一起保存。'),
      fields: [
        { anchor: 'authcfg-ldap-starttls', label: t('StartTLS（明文端口上升级 TLS）'), wire: 'startTls', kind: 'check' },
        { anchor: 'authcfg-ldap-skiptls', label: t('跳过 TLS 证书校验（skipTlsVerify）'), wire: 'skipTlsVerify', kind: 'check', hint: t('仅排障用：接受任意服务端证书。') },
        { anchor: 'authcfg-ldap-group-basedn', label: t('组搜索 Base DN'), wire: 'groupBaseDn', kind: 'text', mono: true },
        { anchor: 'authcfg-ldap-group-filter', label: t('组过滤器'), wire: 'groupFilter', kind: 'text', mono: true, placeholder: '(objectClass=group)', hint: t('{0} = 用户名（组属主判定）。') },
        { anchor: 'authcfg-ldap-group-nameattr', label: t('组名属性'), wire: 'groupNameAttribute', kind: 'text', mono: true, placeholder: 'cn' },
        { anchor: 'authcfg-ldap-admin-group', label: t('admin 角色映射组'), wire: 'adminGroup', kind: 'text', mono: true },
        { anchor: 'authcfg-ldap-readonly-group', label: t('readonly_admin 角色映射组'), wire: 'readOnlyGroup', kind: 'text', mono: true },
        { anchor: 'authcfg-ldap-poolsize', label: t('连接池大小'), wire: 'poolSize', kind: 'number', placeholder: '5', hint: t('默认 5；非正数归一为默认。') },
      ],
    },
  ],
}

// ---- OAuth / OIDC（BinFlow issuer 发现式 wire——T-305 漂移 1） ---------------

const OAUTH: SectionDef = {
  id: 'oauth',
  tab: 'OAuth (OIDC)',
  head: t('OAuth / OIDC SSO（issuer 发现式单段配置）'),
  testCreds: false,
  groups: [
    {
      title: t('常规'),
      hint: t('BinFlow 为 issuer 发现式模型：填 IdP 的 issuer URL，端点经 discovery 文档解析（Artifactory 的多 provider / authUrl / tokenUrl 面无对应，defaultNpm 不承载）。'),
      fields: [
        { anchor: 'authcfg-oauth-enabled', label: t('Enable OAuth（启用 OIDC SSO）'), wire: 'enabled', kind: 'check' },
        { anchor: 'authcfg-oauth-issuer', label: 'Issuer URL', wire: 'issuer_url', kind: 'text', mono: true, full: true, placeholder: 'https://idp.example.com/realms/main', hint: t('启用时保存前会做真实 discovery 探测（写路径验证）——不可达的 issuer 会被拒绝。') },
        { anchor: 'authcfg-oauth-client-id', label: 'Client ID', wire: 'client_id', kind: 'text', mono: true },
        { anchor: 'authcfg-oauth-client-secret', label: 'Client Secret', wire: 'client_secret', kind: 'secret', full: true, setAnchor: 'authcfg-oauth-secret-set' },
        { anchor: 'authcfg-oauth-redirect', label: 'Redirect URL', wire: 'redirect_url', kind: 'text', mono: true, full: true, placeholder: 'https://registry.example.com/binflow/ui/login' },
        { anchor: 'authcfg-oauth-scopes', label: t('Scopes（逗号分隔）'), wire: 'scopes', kind: 'list', mono: true, full: true, placeholder: 'openid, profile, email' },
        { anchor: 'authcfg-oauth-user-claim', label: t('用户 claim'), wire: 'user_claim', kind: 'text', mono: true, placeholder: 'preferred_username' },
        { anchor: 'authcfg-oauth-group-claim', label: t('组 claim'), wire: 'group_claim', kind: 'text', mono: true, placeholder: 'groups' },
        { anchor: 'authcfg-oauth-admin-group', label: t('admin 角色映射组'), wire: 'admin_group', kind: 'text', mono: true },
        { anchor: 'authcfg-oauth-readonly-group', label: t('readonly_admin 角色映射组'), wire: 'readonly_group', kind: 'text', mono: true },
        { anchor: 'authcfg-oauth-autocreate', label: t('Auto Create System Users（persistUsers 对应位，全局摊平语义）'), wire: 'auto_create_users', kind: 'check', wireDefault: true },
      ],
    },
  ],
}

// ---- SAML（§3.1 13 字段 verbatim，§6 呈现序） ---------------------------------

const SAML: SectionDef = {
  id: 'saml',
  tab: 'SAML SSO',
  head: t('SAML SSO（单配置；IdP 证书是公开材料，无脱敏字段）'),
  testCreds: false,
  groups: [
    {
      title: t('常规'),
      fields: [
        { anchor: 'authcfg-saml-enabled', label: 'Enable SAML Integration', wire: 'enableIntegration', kind: 'check' },
        { anchor: 'authcfg-saml-encrypted', label: 'Use Encrypted Assertion', wire: 'useEncryptedAssertion', kind: 'check' },
        { anchor: 'authcfg-saml-spname', label: t('SAML Service Provider Name（entityID）'), wire: 'serviceProviderName', kind: 'text', mono: true, hint: t('须配 Custom Base URL（7.98.7 起缺失会 500——官方 Breaking Change）。') },
        { anchor: 'authcfg-saml-login-url', label: 'SAML Login URL', wire: 'loginUrl', kind: 'text', mono: true, full: true, placeholder: 'https://idp.example.com/sso' },
        { anchor: 'authcfg-saml-logout-url', label: 'SAML Logout URL', wire: 'logoutUrl', kind: 'text', mono: true, full: true, placeholder: 'https://idp.example.com/logout', hint: t('可填 {baseUrl} 动态回跳（多节点建议）。') },
        { anchor: 'authcfg-saml-cert', label: t('SAML Certificate（IdP X.509 PEM）'), wire: 'certificate', kind: 'textarea', mono: true, rows: 5, full: true, placeholder: '-----BEGIN CERTIFICATE-----', hint: t('启用集成时保存前会解析证书——非 PEM CERTIFICATE 块会被拒绝。') },
        { anchor: 'authcfg-saml-syncgroups', label: 'Auto Associate Groups', wire: 'syncGroups', kind: 'check' },
        { anchor: 'authcfg-saml-groupattr', label: t('Group Attribute（大小写敏感）'), wire: 'groupAttribute', kind: 'text', mono: true, placeholder: 'memberOf' },
        { anchor: 'authcfg-saml-emailattr', label: 'Email Attribute', wire: 'emailAttribute', kind: 'text', mono: true, placeholder: 'mail' },
        { anchor: 'authcfg-saml-autocreate', label: t('Auto Create Users（wire = noAutoUserCreation，反语义：勾选 = 自动创建，默认不创建）'), wire: 'noAutoUserCreation', kind: 'check', invert: true, wireDefault: true },
        { anchor: 'authcfg-saml-allowprofile', label: 'Allow Created Users Access To Profile Page', wire: 'allowUserToAccessProfile', kind: 'check' },
        { anchor: 'authcfg-saml-autoredirect', label: 'Auto Redirect Login Link to SAML Login', wire: 'autoRedirect', kind: 'check', hint: t('仅会话过期时生效；手动登出不触发。') },
        { anchor: 'authcfg-saml-verify-audience', label: t('Verify Audience Restriction（受众校验，默认开）'), wire: 'verifyAudienceRestriction', kind: 'check', wireDefault: true },
      ],
    },
  ],
}

export const SECTIONS: Record<AuthSection, SectionDef> = { ldap: LDAP, oauth: OAUTH, saml: SAML }
export const SECTION_ORDER: AuthSection[] = ['ldap', 'oauth', 'saml']

/** 该段全部 secret 字段（wire 路径） */
export function secretWires(def: SectionDef): string[] {
  return def.groups.flatMap((g) => g.fields.filter((f) => f.kind === 'secret').map((f) => f.wire))
}

// ---- 表单态 ⇄ wire doc --------------------------------------------------------

export type FormState = Record<string, string | boolean>

/** 点号一级路径读（doc 是服务端回显，未知形态容错为 undefined） */
export function getPath(doc: unknown, wire: string): unknown {
  const segs = wire.split('.')
  let cur: unknown = doc
  for (const s of segs) {
    if (cur === null || typeof cur !== 'object') return undefined
    cur = (cur as Record<string, unknown>)[s]
  }
  return cur
}

function setPath(out: Record<string, unknown>, wire: string, value: unknown): void {
  const segs = wire.split('.')
  if (segs.length === 1) {
    out[segs[0]] = value
    return
  }
  const parent = (out[segs[0]] ??= {}) as Record<string, unknown>
  parent[segs[1]] = value
}

/** GET doc → 表单态（secret 一律空——「留空保持不变」；check 走正语义） */
export function initForm(def: SectionDef, doc: unknown): FormState {
  const form: FormState = {}
  for (const g of def.groups) {
    for (const f of g.fields) {
      switch (f.kind) {
        case 'check': {
          const raw = getPath(doc, f.wire)
          const base = raw === undefined ? !!f.wireDefault : raw === true
          form[f.wire] = f.invert ? !base : base
          break
        }
        case 'secret':
          form[f.wire] = ''
          break
        case 'locked':
          form[f.wire] = f.fixed ?? ''
          break
        case 'list': {
          const v = getPath(doc, f.wire)
          form[f.wire] = Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string').join(', ') : ''
          break
        }
        case 'number': {
          const v = getPath(doc, f.wire)
          form[f.wire] = typeof v === 'number' ? String(v) : ''
          break
        }
        default:
          form[f.wire] = typeof getPath(doc, f.wire) === 'string' ? (getPath(doc, f.wire) as string) : ''
      }
    }
  }
  return form
}

/**
 * 表单态 → PUT payload。**secret 空值自 payload 剔除**（write-only keep
 * 语义；回传哨兵会被 400 拒——红线，网络层有断言）。非 secret 字段逐字段
 * 回传（PUT 全量替换，缺省键会被服务端归一成默认值，漏发=丢配置）。
 * number 非法/非正数剔除（服务端归一默认）；list 空集剔除。
 */
export function buildPayload(def: SectionDef, form: FormState): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const g of def.groups) {
    for (const f of g.fields) {
      const v = form[f.wire]
      switch (f.kind) {
        case 'check':
          setPath(out, f.wire, f.invert ? v !== true : v === true)
          break
        case 'locked':
          if (f.fixed !== undefined) setPath(out, f.wire, f.fixed)
          break
        case 'number': {
          const n = Number(v)
          if (Number.isFinite(n) && n > 0) setPath(out, f.wire, Math.trunc(n))
          break
        }
        case 'list': {
          const arr = String(v ?? '')
            .split(',')
            .map((s) => s.trim())
            .filter((s) => s !== '')
          if (arr.length > 0) setPath(out, f.wire, arr)
          break
        }
        case 'secret': {
          const s = String(v ?? '')
          if (s !== '') setPath(out, f.wire, s)
          break
        }
        default:
          setPath(out, f.wire, String(v ?? ''))
      }
    }
  }
  return out
}

/** GET 回显里该 wire 是否携带哨兵（= 库存已设置 secret） */
export function secretIsSet(doc: unknown, wire: string): boolean {
  return getPath(doc, wire) === AUTH_SECRET_SENTINEL
}

/** SAML 的锚定空态：GET 返回 {}（§3.2——doc 无任何键 = 从未保存） */
export function isEmptySectionDoc(doc: unknown): boolean {
  return (
    doc !== null &&
    typeof doc === 'object' &&
    !Array.isArray(doc) &&
    Object.keys(doc as Record<string, unknown>).length === 0
  )
}
