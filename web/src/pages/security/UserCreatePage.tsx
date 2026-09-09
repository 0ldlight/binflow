// 用户创建路由页（T-453 / FR-145.1——P3 新栈重写）：
// /admin/security/users/new 深链整页表单，对位 Artifactory 7.161.20
// /ui/admin/management/users/new：页脚 Cancel | Reset（初始禁置）| Save
// （初始禁置）；字段序 = User Name / Email Address / 角色（三值枚举）/
// Password / Retype Password / 相关组穿梭。
//
// 能力位三旗（FR-145.3）= 预留位恒禁用 + 零提交（BE 未承接，t453 漂移钉）。
// 管理位候裁臂：7.161 双布尔 vs BinFlow 三值枚举（ADR-0026 维持枚举，
// 附注不建双布尔）。readonly_admin：L4 深链防御（控件禁用 + 注记）。
// 锚族原样：user-create-page/user-form(-section-*|-name|-email|-role|
// -enabled|-password(2)?|-groups|-group-<name>|-reserved-caps|-profile-
// updatable|-disable-ui|-disable-internal-password|-error|-cancel|-reset|
// -submit)/user-create-readonly-note。
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { AlertBox, CheckRow } from '@/components/layout/bits'
import { StateSkeleton } from '@/components/layout/states'
import { TextInput, NativeSelect } from '@/components/layout/fields'
import { TransferBox } from '@/components/layout/transfer-box'
import { toast } from '@/lib/toast'
import { ADMIN_ROLES, ApiError, errText, isReadOnlyAdmin } from '@/lib/api'
import type { AdminRole } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import './security.css'
import { createUser, listGroups, validateUserName } from './api'
import { tr } from '@/i18n'

const t = tr('security')

interface CreateState {
  name: string
  email: string
  password: string
  password2: string
  role: AdminRole
  enabled: boolean
  groups: string[]
}

const CREATE_INITIAL: CreateState = {
  name: '',
  email: '',
  password: '',
  password2: '',
  role: 'user',
  enabled: true,
  groups: [],
}

/** 预留位组题头与三旗说明（BE 未承接域的如实呈现——锚逐字面内联） */
const RESERVED_CAP_TITLE = t('能力位（Can Update Profile / Disable UI Access / Disable Internal Password）——预留位')

/** 预留位复选（恒禁用 + 零提交——现值如实呈现 BE 默认态） */
function ReservedCapChecks() {
  return (
    <div className="field" data-testid="user-form-reserved-caps">
      <p className="field-hint">{RESERVED_CAP_TITLE}</p>
      <div>
        <CheckRow checked disabled label={t('Can Update Profile（可更新档案）')} testid="user-form-profile-updatable" />
        <p className="field-hint">
          {t('预留位——后端创建/更新端点未承接该域（GET 回显恒 true、零行为联动），恒禁用、零提交；Artifactory 语义 = 取消勾选后用户不能自助修改档案。承接落地时解禁（漂移钉见 t453 spec）。')}
        </p>
      </div>
      <div>
        <CheckRow checked={false} disabled label={t('Disable UI Access（禁用 UI 访问）')} testid="user-form-disable-ui" />
        <p className="field-hint">{t('预留位——同上未承接。语义（登录被拒臂）：勾选后该用户 UI 登录被拒，API / Token 面不受影响。')}</p>
      </div>
      <div>
        <CheckRow checked={false} disabled label={t('Disable Internal Password Login（禁用内部口令登录）')} testid="user-form-disable-internal-password" />
        <p className="field-hint">{t('预留位——同上未承接。语义（密码改道臂）：勾选后内部口令登录停用，认证走外部 IdP（LDAP/OIDC）。')}</p>
      </div>
    </div>
  )
}

export default function UserCreatePage() {
  const { session } = useAuth()
  const navigate = useNavigate()
  const readOnly = isReadOnlyAdmin(session)
  // 组清单随表单挂载取数（路由页进入即取——无「先开页再带外建组」竞窗）
  const groups = useAsync(listGroups, [])
  const [f, setF] = useState<CreateState>(CREATE_INITIAL)
  const [touched, setTouched] = useState({ name: false, email: false, password: false })
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  // blur 触发校验（§4.7：必填空给「请填写此字段」语义）；必填未满足时
  // Save 置灰（7.161 活体：Save 初始 disabled）。Retype 不一致同置灰。
  const nameErr = touched.name || f.name.trim() !== '' ? validateUserName(f.name.trim()) : null
  const emailErr = touched.email && f.email.trim() === '' ? t('请填写此字段') : null
  const passErr = touched.password && f.password === '' ? t('请填写此字段') : null
  const passMismatch = f.password !== f.password2
  const canSubmit =
    f.name.trim() !== '' &&
    validateUserName(f.name.trim()) === null &&
    f.email.trim() !== '' &&
    f.password !== '' &&
    !passMismatch &&
    !submitting &&
    !readOnly
  const dirty = f !== CREATE_INITIAL

  const submit = async () => {
    setServerError(null)
    setSubmitting(true)
    try {
      await createUser(f.name.trim(), {
        name: f.name.trim(),
        email: f.email.trim(),
        password: f.password,
        // 角色走一致对（admin 布尔 = adminRole===admin——resolveCreateRole
        // 对校验要求，非「混发」：编辑臂才只走 adminRole）
        admin: f.role === 'admin',
        adminRole: f.role,
        enabled: f.enabled,
        groups: f.groups,
      })
      toast.success(t('用户 {v1} 已创建', { v1: f.name.trim() }))
      // 创建-列表-编辑闭环：保存后回列表（7.161 同姿）
      navigate('/admin/security/users')
    } catch (err) {
      // 服务端 400 文案原样行内（email/口令缺失、混合大小写、未知组）
      setServerError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div data-testid="user-create-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('新建用户')}</h2>
        <ButtonAsChild variant="outline" size="sm" className="ml-auto">
          <Link to="/admin/security/users">{t('← 返回列表')}</Link>
        </ButtonAsChild>
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="user-create-readonly-note">
          {t('ⓘ 只读管理员（readonly_admin）视角：用户创建是管理面写操作，本页为只读呈现 （服务端 403 兜底，UI 不代持判定）。')}
        </p>
      )}

      <section className="card inline-form" data-testid="user-form" aria-label={t('新建用户')}>
        <div className="form-section" data-testid="user-form-section-settings">
          <h4>{t('用户设置')}</h4>
          <div className="field">
            <label htmlFor="uf-name">{t('用户名 *')}</label>
            <TextInput
              id="uf-name"
              mono
              lang="en"
              value={f.name}
              disabled={readOnly}
              onBlur={() => setTouched((p) => ({ ...p, name: true }))}
              onChange={(e) => setF((p) => ({ ...p, name: e.target.value }))}
              placeholder="bob"
              aria-invalid={!!nameErr || undefined}
              data-testid="user-form-name"
            />
            {nameErr ? (
              <p className="field-error" role="alert">{nameErr}</p>
            ) : (
              <p className="field-hint">{t('全小写；服务端终裁（保留名拒绝）。')}</p>
            )}
          </div>
          <div className="field">
            <label htmlFor="uf-email">Email *</label>
            <TextInput
              id="uf-email"
              type="email"
              value={f.email}
              disabled={readOnly}
              onBlur={() => setTouched((p) => ({ ...p, email: true }))}
              onChange={(e) => setF((p) => ({ ...p, email: e.target.value }))}
              placeholder="bob@example.com"
              aria-invalid={!!emailErr || undefined}
              data-testid="user-form-email"
            />
            {emailErr && <p className="field-error" role="alert">{emailErr}</p>}
          </div>
          <div className="field max-w-[480px]">
            <label htmlFor="uf-role">{t('角色（三值闭集，M7 FR-66）')}</label>
            <NativeSelect
              id="uf-role"
              value={f.role}
              disabled={readOnly}
              onChange={(e) => setF((p) => ({ ...p, role: e.target.value as AdminRole }))}
              options={ADMIN_ROLES.map((r) => ({ value: r, label: r }))}
              data-testid="user-form-role"
            />
            <p className="field-hint">{t('user=按 permission target 授权；readonly_admin=管理面只读；admin=管理面全权。')}</p>
            <p className="field-hint" data-testid="user-form-role-parity-note">
              {t('候裁臂：Artifactory 7.161 此处为 Administer Platform + Manage Resources 双布尔（另有 Platform Auditor / Manage Webhook）；BinFlow 按 ADR-0026 暂行维持三值枚举（readonly_admin 无双布尔对位），差异登记候裁——双布尔不建不伪造。')}
            </p>
          </div>
        </div>
        <div className="form-section" data-testid="user-form-section-options">
          <h4>{t('选项')}</h4>
          <CheckRow
            checked={f.enabled}
            disabled={readOnly}
            onChange={(next) => setF((p) => ({ ...p, enabled: next }))}
            label={t('启用（取消勾选 = 禁用账号——禁用后登录与写面全部拒绝）')}
            testid="user-form-enabled"
          />
          <ReservedCapChecks />
        </div>
        <div className="form-section" data-testid="user-form-section-password">
          <h4>{t('口令')}</h4>
          <div className="field">
            <label htmlFor="uf-pass">{t('初始口令 *')}</label>
            <TextInput
              id="uf-pass"
              type="password"
              autoComplete="new-password"
              value={f.password}
              disabled={readOnly}
              onBlur={() => setTouched((p) => ({ ...p, password: true }))}
              onChange={(e) => setF((p) => ({ ...p, password: e.target.value }))}
              aria-invalid={!!passErr || undefined}
              data-testid="user-form-password"
            />
            {passErr && <p className="field-error" role="alert">{passErr}</p>}
          </div>
          <div className="field">
            <label htmlFor="uf-pass2">{t('确认口令 *')}</label>
            <TextInput
              id="uf-pass2"
              type="password"
              autoComplete="new-password"
              placeholder={t('（再输入一次）')}
              value={f.password2}
              disabled={readOnly}
              onChange={(e) => setF((p) => ({ ...p, password2: e.target.value }))}
              aria-invalid={passMismatch || undefined}
              data-testid="user-form-password2"
            />
            {passMismatch && <p className="field-error" role="alert">{t('两次输入的口令不一致')}</p>}
          </div>
        </div>
        <div className="form-section" data-testid="user-form-section-groups">
          <h4>{t('相关组')}</h4>
          <p className="field-hint">{t('勾选即加入（右列）；保存后即时生效——移出组即失去该组授权，无需重登。')}</p>
          {groups.status === 'loading' && <StateSkeleton lines={2} />}
          {groups.status === 'ok' && (
            <div data-testid="user-form-groups">
              <TransferBox
                items={(groups.data ?? []).map((g) => ({ name: g.name, note: g.description || undefined }))}
                selected={f.groups}
                disabled={readOnly}
                onToggle={(name, next) =>
                  setF((p) => ({ ...p, groups: next ? [...p.groups, name] : p.groups.filter((x) => x !== name) }))
                }
                availableLabel={t('可选组')}
                selectedLabel={t('已选组')}
                itemTestid={(name) => `user-form-group-${name}`}
              />
            </div>
          )}
          {groups.status === 'ok' && (groups.data ?? []).length === 0 && (
            <p className="field-hint">{t('实例还没有组——先到「组」页创建。')}</p>
          )}
          {groups.status !== 'loading' && groups.status !== 'ok' && (
            <p className="field-hint">{t('组列表不可用（')}{groups.error?.message}{t('）；可先建用户，稍后在编辑页入组。')}</p>
          )}
        </div>
        {serverError && (
          <AlertBox severity="error" testid="user-form-error">
            <div className="font-medium">{t('创建失败（HTTP')} {serverError.status || t('网络')}{t('）')}</div>
            <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{serverError.message}</div>
          </AlertBox>
        )}
        <div className="form-actions">
          {/* 页脚三联（7.161 活体：Cancel 最左 / Reset〔初始禁置〕/ Save 右） */}
          <Button variant="outline" size="sm" data-testid="user-form-cancel" onClick={() => navigate('/admin/security/users')}>
            {t('取消')}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!dirty || submitting}
            data-testid="user-form-reset"
            onClick={() => setF(CREATE_INITIAL)}
          >
            {t('重置')}
          </Button>
          <Button
            size="sm"
            disabled={!canSubmit}
            title={readOnly ? t('只读管理员：用户创建是管理面写操作（服务端 403）') : undefined}
            onClick={() => void submit()}
            data-testid="user-form-submit"
          >
            {submitting ? t('创建中…') : t('创建用户')}
          </Button>
        </div>
      </section>
    </div>
  )
}
