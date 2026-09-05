import { useState } from 'react'
import type { ReactNode } from 'react'

import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
import { PkgIcon } from '../../components/PkgIcon'
import { useConfirm } from '../../components/ConfirmDialog'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin } from '../../lib/api'
import {
  getAddons,
  getLicense,
  installLicense,
  isDisabledByConfig,
  normalizeTier,
  tierBadgeClass,
  uninstallLicense,
} from '../../lib/addons'
import type { AddonRow, LicenseStatus } from '../../lib/addons'
import { useAsync } from '../../lib/useAsync'

import './license.css'
import { tr } from '../../i18n'

const t = tr('admin')

/** 档位徽章的 MUI Chip color（tier-pro→info / tier-enterprise→warning /
 *  community = default——mui-native-visual §3.2 映射；tierBadgeClass 类名
 *  组合续挂 DOM） */
const TIER_COLOR: Record<string, 'info' | 'warning' | 'default'> = {
  pro: 'info',
  enterprise: 'warning',
  community: 'default',
}

// License & Add-ons 管理页（M10 T-288，FR-84 FE 腿 + FR-86-AC5；console-m8
// 管理模式「常规」分组）。两张卡：
//
// - License 状态卡：当前档位徽章（三色）/ licensee / 有效期 / 过期倒计时
//   （daysToExpiry 负值 = 已过期 N 天——D6 无宽限，降级即时生效）；未装
//   license = community 地板说明（license-floor）。admin：文本域贴文档装载
//   （400 验签拒绝原文呈现——LICENSE_EXPIRED/LICENSE_INVALID 两 wire 码）+
//   卸载（输入 UNINSTALL 强确认——降级不劫持数据，但门控面即时关闭）。
// - Addons 矩阵表：GET /api/v1/addons 装配序全槽位——Id/名称/Kind 徽章
//   （package-type|feature，mono wire 值）/ 最低档位（地板 = 「—」无徽章）/
//   Enabled 只读态（license 决定，无手动开关：已解锁 / 锁定（需要 N 档）/
//   已禁用（addons.disabled 熔断）——锁定/禁用行灰显 + ⊘）。
//
// 门（router.go）：GET 双端点 = CapSystemRead（admin/readonly_admin），
// POST/DELETE = CapSystemWrite（仅全量 admin；readonly_admin 403 兜底，
// UI 不渲染写入口）。普通 user 双 GET 403 → 页面级 L2 无权限卡（管理面
// 门，导航本就不可达——AC5 三角色腿）。
//
// 契约注记：槽位三态由 enabled+reason 合成（§15.2.5），reason 是呈现面
// （m10 README §2.2——矩阵断言只对 id/minTier/enabled）；档位闭集外的
// tier 值归一为 community 呈现（不放大）。

/** 有效期/倒计时的组合呈现（perpetual / 未装 = 简化行）。licensed 形态才有
 *  倒计时文本——community 形态（默认测试形态）不可达，故不设锚（免死锚，
 *  锚册 v1.10 注记）。 */
function expiryBlock(st: LicenseStatus): ReactNode {
  if (!st.licensed || st.perpetual) {
    return (
      <div className="kv">
        <span className="k">{t('有效期')}</span>
        <span>{st.licensed ? t('永久（perpetual）') : '—'}</span>
      </div>
    )
  }
  const d = st.daysToExpiry
  return (
    <>
      <div className="kv">
        <span className="k">{t('有效期至')}</span>
        <span className="mono" lang="en">
          {st.expiresAt || '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">{t('倒计时')}</span>
        <span>
          {d === null
            ? '—'
            : d >= 0
              ? t('剩 {d} 天', { d: d })
              : t('已过期 {v1} 天（D6 无宽限：已降级 community，读不劫持）', { v1: -d })}
        </span>
      </div>
    </>
  )
}

function LicenseCard({ rev, onChanged }: { rev: number; onChanged: () => void }) {
  const { session } = useAuth()
  const toast = useToast()
  const confirm = useConfirm()
  const admin = canAdminWrite(session)
  const readOnly = isReadOnlyAdmin(session)

  const state = useAsync(getLicense, [rev])
  const [doc, setDoc] = useState('')
  const [busy, setBusy] = useState(false)
  const [installError, setInstallError] = useState<ApiError | null>(null)

  const doInstall = async () => {
    setInstallError(null)
    setBusy(true)
    try {
      const st = await installLicense(doc)
      toast.success(t('License 已装载（{v1} 档，即刻生效）', { v1: normalizeTier(st.tier) }))
      setDoc('')
      onChanged()
    } catch (err) {
      // 验签拒绝 400（LICENSE_EXPIRED/LICENSE_INVALID 两 wire 码）与 5xx
      // 持久化失败如实呈现原文——错误体不含内部校验细节（FR-84-AC3）
      setInstallError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  const doUninstall = async (st: LicenseStatus) => {
    const holder = { typed: '' }
    const body: ReactNode = (
      <>
        <p>{t('将卸载当前 license（')}<b className="mono" lang="en">{normalizeTier(st.tier)}</b> {t('档')}          {st.licensee ? t('，被授权方 {v1}', { v1: st.licensee }) : ''}{t('）。卸载后实例降回')}{' '}
          <b>{t('community 地板')}</b>{t('：门控槽位（含其包型仓的建仓/写入）即刻关闭，既有制品读不受影响 （降级不劫持数据）。')}        </p>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0 }}>
          <label htmlFor="lic-uninstall-confirm">{t('输入')} <b className="mono" lang="en">UNINSTALL</b> {t('以确认：')}          </label>
          <input
            id="lic-uninstall-confirm"
            className="confirm-input"
            autoComplete="off"
            onChange={(e) => {
              holder.typed = e.target.value
            }}
            lang="en"
          />
        </div>
      </>
    )
    const ok = await confirm({
      title: t('卸载 license'),
      body,
      danger: true,
      confirmLabel: t('卸载 license'),
      confirmDisabled: () => holder.typed !== 'UNINSTALL',
    })
    if (!ok) return
    setBusy(true)
    try {
      const text = await uninstallLicense()
      toast.success(text) // 服务端文案原样（"License removed successfully."）
      onChanged()
    } catch (err) {
      toast.error(t('卸载失败：{v1}', { v1: errText(err) }))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card section" data-testid="license-card">
      <h3>License</h3>
      {state.status === 'loading' && <Skeleton lines={5} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'ok' && state.data && (
        <>
          <div className="kv">
            <span className="k">{t('当前档位')}</span>
            <span>
              <Chip
                size="small"
                variant="outlined"
                color={TIER_COLOR[normalizeTier(state.data.tier)] ?? 'default'}
                className={tierBadgeClass(state.data.tier)}
                label={normalizeTier(state.data.tier)}
                data-testid="license-tier"
                lang="en"
              />
              <span className="text-2" style={{ marginLeft: 8 }}>
                {state.data.licensed ? t('已授权') : t('未安装 license')}
              </span>
            </span>
          </div>
          {state.data.licensed && (
            <>
              <div className="kv">
                <span className="k">{t('被授权方')}</span>
                <span className="mono" lang="en" data-testid="license-licensee">
                  {state.data.licensee || '—'}
                </span>
              </div>
              <div className="kv">
                <span className="k">License ID</span>
                <span className="mono" lang="en">
                  {state.data.licenseId || '—'}
                </span>
              </div>
              <div className="kv">
                <span className="k">{t('签发时间')}</span>
                <span className="mono" lang="en">
                  {state.data.issuedAt || '—'}
                </span>
              </div>
            </>
          )}
          {expiryBlock(state.data)}
          {!state.data.licensed && (
            <p className="field-hint" data-testid="license-floor">{t('未安装 license：实例按')} <b>{t('community 地板')}</b>{t('运行——五核心包型与基础能力恒解锁， 门控槽位呈锁定态。下方矩阵即全槽位实时求值（与门控执行同源）。')}            </p>
          )}

          {readOnly && (
            <p className="license-note" data-testid="license-readonly-note">{t('ⓘ 只读管理员视角：license 安装/卸载是管理面写操作（system:write，仅全量 admin）；本页只读呈现，服务端 403 兜底。')}            </p>
          )}

          {admin && (
            <div className="license-install-block">
              <div className="field" style={{ marginBottom: 0 }}>
                <label htmlFor="license-doc">{t('安装 license 文档')}</label>
                <TextField
                  id="license-doc"
                  size="small"
                  multiline
                  minRows={5}
                  value={doc}
                  onChange={(e) => setDoc(e.target.value)}
                  placeholder={t('粘贴 license 文档全文（.lic）——验签失败会被原样拒绝，当前 license 不受影响')}
                  sx={{ width: '100%', maxWidth: 720 }}
                  slotProps={{
                    htmlInput: {
                      'data-testid': 'license-doc-input',
                      lang: 'en',
                      className: 'mono-input',
                      spellCheck: false,
                    },
                  }}
                />
                <p className="field-hint">{t('装载即刻生效（进程内原子切换，无撕裂）；GET 永不回显文档原文（NFR-S52）。')}                </p>
              </div>
              {installError && (
                <Alert severity="error" data-testid="license-install-error">
                  <div className="headline">{t('装载被拒（HTTP')} {installError.status || t('网络')}{t('）')}</div>
                  <div className="raw" lang="en">
                    {installError.message}
                  </div>
                </Alert>
              )}
              <div className="form-actions" style={{ marginTop: 12 }}>
                <Button
                  variant="contained"
                  size="small"
                  disabled={doc.trim() === '' || busy}
                  data-testid="license-install"
                  onClick={() => void doInstall()}
                >
                  {busy ? t('处理中…') : t('装载 license')}
                </Button>
                {state.data.licensed && (
                  <Button
                    variant="outlined"
                    color="error"
                    size="small"
                   
                    disabled={busy}
                    data-testid="license-uninstall"
                    onClick={() => void doUninstall(state.data!)}
                  >{t('卸载 license')}                  </Button>
                )}
              </div>
            </div>
          )}
        </>
      )}
    </section>
  )
}

/** 矩阵行图标（T-390 / FR-127 AC2-④）：包型槽 → 对应包型 brand 标；
 *  trashcan/webhook 两 feature 槽 → 自有 brand 标；其余 feature 槽
 *  （properties/ha/repo-operations/xray-integration）不在 30 枚集内——
 *  无图标（注记豁免，见 T-390 日志）。 */
function addonIconId(row: AddonRow): string | null {
  if (row.kind === 'package-type') return row.id
  if (row.id === 'trashcan' || row.id === 'webhook') return row.id
  return null
}

function AddonTableRow({ row }: { row: AddonRow }) {
  const tier = normalizeTier(row.minTier)
  const disabledCfg = isDisabledByConfig(row)
  const rowClass = row.enabled ? '' : disabledCfg ? 'is-disabled' : 'is-locked'
  const iconId = addonIconId(row)
  return (
    <TableRow className={rowClass} data-testid={`addons-row-${row.id}`} hover>
      <TableCell className="mono" lang="en">
        {row.id}
      </TableCell>
      <TableCell>
        {/* 包型身份位走 brand 版（AC2-④）；锁定/禁用行的置灰由既有
            .is-locked/.is-disabled 行级 opacity 承载（沿用，不另设图标态） */}
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          {iconId && <PkgIcon id={iconId} variant="brand" size={18} />}
          {row.displayName}
        </span>
      </TableCell>
      <TableCell>
        <Chip size="small" className="badge neutral mono" label={row.kind} sx={{ fontFamily: 'var(--bf-mono)' }} lang="en" />
      </TableCell>
      <TableCell data-testid={`addons-tier-${row.id}`}>
        {tier === 'community' ? (
          <span className="text-2" title={t('community 地板：无 license 也解锁')}>
            —
          </span>
        ) : (
          <Chip size="small" variant="outlined" color={TIER_COLOR[tier] ?? 'default'} className={tierBadgeClass(tier)} label={tier} lang="en" />
        )}
      </TableCell>
      <TableCell data-testid={`addons-state-${row.id}`}>
        {row.enabled ? (
          <>
            <span className="status-dot ok" aria-hidden="true" />{t('已解锁')}          </>
        ) : disabledCfg ? (
          <>
            <Chip size="small" variant="outlined" color="warning" className="badge warning" label={t('⊘ 已禁用')} />
            <span className="text-2" style={{ marginLeft: 6 }} title={row.reason ?? ''}>{t('配置熔断（addons.disabled）')}            </span>
          </>
        ) : (
          <>
            <span aria-hidden="true">⊘</span> {t('锁定')}            <span className="text-2" style={{ marginLeft: 6 }} title={row.reason ?? ''}>{t('需要')} {tier} {t('档')}            </span>
          </>
        )}
      </TableCell>
    </TableRow>
  )
}

export default function LicenseAddonsPage() {
  const [rev, setRev] = useState(0)
  const license = useAsync(getLicense, [rev])
  const addons = useAsync(getAddons, [rev])
  const bump = () => setRev((r) => r + 1)

  // 页面级 L2：普通 user 双 GET 403（管理面门——导航不可达，直链收敛）
  if (license.status === 'forbidden') {
    return (
      <div data-testid="license-page">
        <div className="page-header">
          <h2>License &amp; Add-ons</h2>
        </div>
        <EmptyState
          message={t('无权限访问 License &amp; Add-ons')}
          hint={t('license 与 addon 状态属于管理面（system:read，需 admin / readonly_admin）。')}
        />
      </div>
    )
  }

  const rows = addons.data ?? []
  return (
    <div data-testid="license-page">
      <div className="page-header">
        <h2>License &amp; Add-ons</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{t('档位 × addon 解锁矩阵（实时求值，与门控执行同源）')}        </span>
      </div>

      <LicenseCard rev={rev} onChanged={bump} />

      <section className="card section" data-testid="addons-card">
        <h3>Add-ons</h3>
        {addons.status === 'loading' && <Skeleton lines={6} />}
        {addons.status === 'error' && addons.error && <ErrorCard error={addons.error} onRetry={addons.reload} />}
        {addons.status === 'forbidden' && (
          <EmptyState message={t('无权限读取 addon 清单')} hint={t('GET /api/v1/addons 需要管理面读权限。')} />
        )}
        {addons.status === 'ok' && rows.length === 0 && (
          <EmptyState
            message={t('此实例未装配 addon 注册表')}
            hint={t('GET /api/v1/addons 为空数组——该组装形态的自我描述（pre-M10 单元栈），非错误。')}
          />
        )}
        {addons.status === 'ok' && rows.length > 0 && (
          <>
            <Table className="addons-table" data-testid="addons-table">
              <TableHead>
                <TableRow>
                  <TableCell component="th" scope="col">ID</TableCell>
                  <TableCell component="th" scope="col">{t('名称')}</TableCell>
                  <TableCell component="th" scope="col">{t('类型')}</TableCell>
                  <TableCell component="th" scope="col">{t('最低档位')}</TableCell>
                  <TableCell component="th" scope="col">{t('状态')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((row) => (
                  <AddonTableRow key={row.id} row={row} />
                ))}
              </TableBody>
            </Table>
            <p className="field-hint" style={{ marginBottom: 0 }}>{t('Enabled 由 license 档位与 addons.disabled 配置决定，不可手动切换；锁定槽位在装对应档位 license 后即刻解锁。')}            </p>
          </>
        )}
      </section>
    </div>
  )
}
