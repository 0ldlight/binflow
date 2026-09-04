import { useState } from 'react'
import type { ComponentPropsWithoutRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControlLabel from '@mui/material/FormControlLabel'
import Select from '@mui/material/Select'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { Skeleton } from '../../components/Skeleton'
import { ADMIN_ROLES, ApiError, errText, isReadOnlyAdmin } from '../../lib/api'
import type { AdminRole } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import './security.css'
import { TransferBox } from './TransferBox'
import { createUser, listGroups, validateUserName } from './api'

// 用户创建路由页（T-453 / FR-145.1，断言反转④——Q5 出口①路由化）：
// /admin/security/users/new 深链整页表单，对位 Artifactory 7.161.20 实测
// /ui/admin/management/users/new（2026-09-04 活体复核：整页路由表单，页脚
// Cancel | Reset〔初始禁置〕| Save〔初始禁置〕；字段序 = User Name /
// Email Address / 管理位双布尔 / 能力位三旗 / Password / Retype Password）。
// UsersPage 列表内联展开卡随本票退役（B-2.15 / parity 册 E5 注销）。
//
// 能力位三旗（FR-145.3）：Can Update Profile / Disable UI Access /
// Disable Internal Password Login——后端 create（PUT userCreateBody）与
// partial-update（POST userUpdateBody）均未承接三域（GET 回显恒
// profileUpdatable=true / 其余 false，行为联动零落点），按 T-439 预留位
// 两档纪律落**恒禁用 + 零提交**（漂移钉 = t453 spec 腿：PUT 携带三域 201
// 而 GET 回显不动——BE 承接落地日本腿翻红即提示转正）。语义如实标注：
// Disable UI Access = 登录被拒臂；Disable Internal Password = 密码改道臂。
//
// 管理位候裁臂：7.161 表单 = Administer Platform + Manage Resources 双
// 布尔（另 Platform Auditor / Manage Webhook）；BinFlow = 三值枚举
// adminRole（ADR-0026 暂行维持枚举 + 差异登记附注，候裁挂附注不建双布尔）。
//
// Retype Password：7.161 实测创建表单在场（T-384 期「创建态无 Retype」
// 定案随本票翻案——活体证据 reports/agents/t453-probe/s-users-new.dom.json）。
//
// readonly_admin：L4 深链防御——只读呈现（控件禁用 + 注记；服务端 403 兜底）。

/** 预留位组题头（能力位三旗——BE 未承接域的如实呈现） */
const RESERVED_CAP_TITLE = '能力位（Can Update Profile / Disable UI Access / Disable Internal Password）——预留位'
const CAP_PROFILE_LABEL = 'Can Update Profile（可更新档案）'
const CAP_PROFILE_HINT =
  '预留位——后端创建/更新端点未承接该域（GET 回显恒 true、零行为联动），恒禁用、零提交；Artifactory 语义 = 取消勾选后用户不能自助修改档案。承接落地时解禁（漂移钉见 t453 spec）。'
const CAP_DISABLE_UI_LABEL = 'Disable UI Access（禁用 UI 访问）'
const CAP_DISABLE_UI_HINT =
  '预留位——同上未承接。语义（登录被拒臂）：勾选后该用户 UI 登录被拒，API / Token 面不受影响。'
const CAP_DISABLE_PW_LABEL = 'Disable Internal Password Login（禁用内部口令登录）'
const CAP_DISABLE_PW_HINT =
  '预留位——同上未承接。语义（密码改道臂）：勾选后内部口令登录停用，认证走外部 IdP（LDAP/OIDC）。'

/** 管理位候裁臂附注（双布尔 vs 枚举——ADR-0026 暂行维持枚举） */
const ROLE_PARITY_NOTE =
  '候裁臂：Artifactory 7.161 此处为 Administer Platform + Manage Resources 双布尔（另有 Platform Auditor / Manage Webhook）；BinFlow 按 ADR-0026 暂行维持三值枚举（readonly_admin 无双布尔对位），差异登记候裁——双布尔不建不伪造。'

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

/** 预留位复选（T-439 ReservedCheck 同款）：恒禁用 + 零提交——现值如实
 *  呈现 BE 回显/默认态（profileUpdatable 恒 true / 两 disable 恒 false）。
 *  data-testid 逐字面内联（anchor-audit 的 src 扫描口径——prop 透传形
 *  不可见），三锚对账器可断。 */
function ReservedCapChecks() {
  return (
    <div className="field" data-testid="user-form-reserved-caps">
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
        {RESERVED_CAP_TITLE}
      </Typography>
      <div>
        <FormControlLabel
          className="check-row"
          disabled
          control={
            <Checkbox
              size="small"
              checked
              slotProps={{ input: { 'data-testid': 'user-form-profile-updatable' } as ComponentPropsWithoutRef<'input'> }}
            />
          }
          label={CAP_PROFILE_LABEL}
        />
        <p className="field-hint">{CAP_PROFILE_HINT}</p>
      </div>
      <div>
        <FormControlLabel
          className="check-row"
          disabled
          control={
            <Checkbox
              size="small"
              slotProps={{ input: { 'data-testid': 'user-form-disable-ui' } as ComponentPropsWithoutRef<'input'> }}
            />
          }
          label={CAP_DISABLE_UI_LABEL}
        />
        <p className="field-hint">{CAP_DISABLE_UI_HINT}</p>
      </div>
      <div>
        <FormControlLabel
          className="check-row"
          disabled
          control={
            <Checkbox
              size="small"
              slotProps={{
                input: { 'data-testid': 'user-form-disable-internal-password' } as ComponentPropsWithoutRef<'input'>,
              }}
            />
          }
          label={CAP_DISABLE_PW_LABEL}
        />
        <p className="field-hint">{CAP_DISABLE_PW_HINT}</p>
      </div>
    </div>
  )
}

export default function UserCreatePage() {
  const { session } = useAuth()
  const navigate = useNavigate()
  const readOnly = isReadOnlyAdmin(session)
  const toast = useToast()
  // 组清单随表单挂载取数（路由页即页面级——老内联卡的「先开页再带外建组」
  // 竞窗随路由化消解，页面进入即取）
  const groups = useAsync(listGroups, [])
  const [f, setF] = useState<CreateState>(CREATE_INITIAL)
  const [touched, setTouched] = useState({ name: false, email: false, password: false })
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState<ApiError | null>(null)

  // blur 触发校验（§4.7：必填空给「请填写此字段」语义）；必填未满足时
  // Save 置灰（7.161 活体：Save 初始 disabled）。Retype 不一致同置灰。
  const nameErr = touched.name || f.name.trim() !== '' ? validateUserName(f.name.trim()) : null
  const emailErr = touched.email && f.email.trim() === '' ? '请填写此字段' : null
  const passErr = touched.password && f.password === '' ? '请填写此字段' : null
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
      toast.success(`用户 ${f.name.trim()} 已创建`)
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
      <div className="page-header">
        <h2>新建用户</h2>
        <Button variant="outlined" size="small" component={Link} to="/admin/security/users">
          ← 返回列表
        </Button>
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="user-create-readonly-note">
          ⓘ 只读管理员（readonly_admin）视角：用户创建是管理面写操作，本页为只读呈现
          （服务端 403 兜底，UI 不代持判定）。
        </p>
      )}

      <section className="card inline-form" data-testid="user-form" aria-label="新建用户">
        {/* T-384 节锚（v1.20 批）随路由化迁本页：创建表单四节——M3 表单结构
            parity 断言载体（载体迁移零改名） */}
        <div className="form-section" data-testid="user-form-section-settings">
          <h4>用户设置</h4>
          <div className="field">
            <label htmlFor="uf-name">用户名 *</label>
            <TextField
              id="uf-name"
              size="small"
              value={f.name}
              disabled={readOnly}
              onBlur={() => setTouched((p) => ({ ...p, name: true }))}
              onChange={(e) => setF((p) => ({ ...p, name: e.target.value }))}
              placeholder="bob"
              error={!!nameErr}
              sx={{ width: 320 }}
              slotProps={{ htmlInput: { className: 'mono-input', 'data-testid': 'user-form-name', lang: 'en' } }}
            />
            {nameErr ? (
              <p className="field-error" role="alert">
                {nameErr}
              </p>
            ) : (
              <p className="field-hint">全小写；服务端终裁（保留名拒绝）。</p>
            )}
          </div>
          <div className="field">
            <label htmlFor="uf-email">Email *</label>
            <TextField
              id="uf-email"
              size="small"
              type="email"
              value={f.email}
              disabled={readOnly}
              onBlur={() => setTouched((p) => ({ ...p, email: true }))}
              onChange={(e) => setF((p) => ({ ...p, email: e.target.value }))}
              placeholder="bob@example.com"
              error={!!emailErr}
              sx={{ width: 320 }}
              slotProps={{ htmlInput: { 'data-testid': 'user-form-email' } }}
            />
            {emailErr && (
              <p className="field-error" role="alert">
                {emailErr}
              </p>
            )}
          </div>
          <div className="field" style={{ maxWidth: 480 }}>
            <label htmlFor="uf-role">角色（三值闭集，M7 FR-66）</label>
            <TextField
              id="uf-role"
              select
              size="small"
              value={f.role}
              disabled={readOnly}
              onChange={(e) => setF((p) => ({ ...p, role: e.target.value as AdminRole }))}
              sx={{ width: 320 }}
              slotProps={{
                select: {
                  native: true,
                  inputProps: { 'data-testid': 'user-form-role' } as ComponentPropsWithoutRef<'select'>,
                } as ComponentPropsWithoutRef<typeof Select>,
              }}
            >
              {ADMIN_ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </TextField>
            <p className="field-hint">user=按 permission target 授权；readonly_admin=管理面只读；admin=管理面全权。</p>
            <p className="field-hint" data-testid="user-form-role-parity-note">
              {ROLE_PARITY_NOTE}
            </p>
          </div>
        </div>
        <div className="form-section" data-testid="user-form-section-options">
          <h4>选项</h4>
          <FormControlLabel
            className="check-row"
            control={
              <Checkbox
                size="small"
                checked={f.enabled}
                disabled={readOnly}
                onChange={(e) => setF((p) => ({ ...p, enabled: e.target.checked }))}
                slotProps={{ input: { 'data-testid': 'user-form-enabled' } as ComponentPropsWithoutRef<'input'> }}
              />
            }
            label="启用（取消勾选 = 禁用账号——禁用后登录与写面全部拒绝）"
          />
          {/* T-453 / FR-145.3：能力位三旗预留位组（BE 未承接——恒禁用 + 零提交） */}
          <ReservedCapChecks />
        </div>
        <div className="form-section" data-testid="user-form-section-password">
          <h4>口令</h4>
          <div className="field">
            <label htmlFor="uf-pass">初始口令 *</label>
            <TextField
              id="uf-pass"
              size="small"
              type="password"
              autoComplete="new-password"
              value={f.password}
              disabled={readOnly}
              onBlur={() => setTouched((p) => ({ ...p, password: true }))}
              onChange={(e) => setF((p) => ({ ...p, password: e.target.value }))}
              error={!!passErr}
              sx={{ width: 320 }}
              slotProps={{ htmlInput: { 'data-testid': 'user-form-password' } }}
            />
            {passErr && (
              <p className="field-error" role="alert">
                {passErr}
              </p>
            )}
          </div>
          <div className="field">
            <label htmlFor="uf-pass2">确认口令 *</label>
            <TextField
              id="uf-pass2"
              size="small"
              type="password"
              autoComplete="new-password"
              placeholder="（再输入一次）"
              value={f.password2}
              disabled={readOnly}
              onChange={(e) => setF((p) => ({ ...p, password2: e.target.value }))}
              error={passMismatch}
              sx={{ width: 320 }}
              slotProps={{ htmlInput: { 'data-testid': 'user-form-password2' } }}
            />
            {passMismatch && (
              <p className="field-error" role="alert">
                两次输入的口令不一致
              </p>
            )}
          </div>
        </div>
        <div className="form-section" data-testid="user-form-section-groups">
          <h4>相关组</h4>
          <p className="field-hint">勾选即加入（右列）；保存后即时生效——移出组即失去该组授权，无需重登。</p>
          {groups.status === 'loading' && <Skeleton lines={2} />}
          {groups.status === 'ok' && (
            <div data-testid="user-form-groups">
              <TransferBox
                items={(groups.data ?? []).map((g) => ({ name: g.name, note: g.description || undefined }))}
                selected={f.groups}
                disabled={readOnly}
                onToggle={(name, next) =>
                  setF((p) => ({ ...p, groups: next ? [...p.groups, name] : p.groups.filter((x) => x !== name) }))
                }
                availableLabel="可选组"
                selectedLabel="已选组"
                itemTestid={(name) => `user-form-group-${name}`}
              />
            </div>
          )}
          {groups.status === 'ok' && (groups.data ?? []).length === 0 && (
            <p className="field-hint">实例还没有组——先到「组」页创建。</p>
          )}
          {groups.status !== 'loading' && groups.status !== 'ok' && (
            <p className="field-hint">组列表不可用（{groups.error?.message}）；可先建用户，稍后在编辑页入组。</p>
          )}
        </div>
        {serverError && (
          <Alert severity="error" data-testid="user-form-error">
            <div className="headline">创建失败（HTTP {serverError.status || '网络'}）</div>
            <div className="raw" lang="en">
              {serverError.message}
            </div>
          </Alert>
        )}
        <div className="form-actions">
          {/* 页脚三联（7.161 活体：Cancel 最左 / Reset〔初始禁置〕/ Save 右）——
              用户/组表单按 7.161 保留 Reset（与 T-439 建仓表单移除重置钮的
              Q9 处置为页面级差异化配置，差异留痕见 parity 册） */}
          <Button
            variant="outlined"
            size="small"
            component={Link}
            to="/admin/security/users"
            data-testid="user-form-cancel"
          >
            取消
          </Button>
          <Button
            variant="outlined"
            size="small"
            disabled={!dirty || submitting}
            data-testid="user-form-reset"
            onClick={() => setF(CREATE_INITIAL)}
          >
            重置
          </Button>
          <Button
            variant="contained"
            size="small"
            disabled={!canSubmit}
            title={readOnly ? '只读管理员：用户创建是管理面写操作（服务端 403）' : undefined}
            onClick={() => void submit()}
            data-testid="user-form-submit"
          >
            {submitting ? '创建中…' : '创建用户'}
          </Button>
        </div>
      </section>
    </div>
  )
}
