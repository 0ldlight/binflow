// License & Add-ons 管理页（M10 T-288——P3 新栈重写）。两张卡：
// - License 状态卡：档位徽章（三色）/ licensee / 有效期倒计时（负值 = 已
//   过期 N 天）；未装 = community 地板说明。admin：文本域贴文档装载 +
//   卸载（prompt 输入 UNINSTALL 强确认）。
// - Addons 矩阵表：Id/名称/Kind 徽章/最低档位/三态（已解锁 · 锁定需N档 ·
//   配置熔断）；锁定/禁用行灰显 + ⊘。
// 门：GET 双端点 = CapSystemRead；POST/DELETE = CapSystemWrite（仅全量
// admin；readonly_admin 403 兜底）。普通 user → 页面级 L2。
// 锚族原样：license-page/license-card/license-tier/license-licensee/
// license-floor/license-readonly-note/license-doc-input/license-install(-
// error)?/license-uninstall/addons-card/addons-table/addons-row-<id>/
// addons-tier-<id>/addons-state-<id>。
import { useState } from 'react'
import type { ReactNode } from 'react'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { AlertBox } from '@/components/layout/bits'
import { PkgIcon } from '@/components/PkgIcon'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { TextArea } from '@/components/layout/fields'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, canAdminWrite, errText, isReadOnlyAdmin } from '@/lib/api'
import {
  getAddons,
  getLicense,
  installLicense,
  isDisabledByConfig,
  normalizeTier,
  tierBadgeClass,
  uninstallLicense,
} from '@/lib/addons'
import type { AddonRow, LicenseStatus } from '@/lib/addons'
import { useAsync } from '@/lib/useAsync'

import './license.css'
import { tr } from '@/i18n'

const t = tr('admin')

/** 有效期/倒计时的组合呈现（perpetual / 未装 = 简化行） */
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
        <span className="font-mono" lang="en">{st.expiresAt || '—'}</span>
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
    const typed = await confirm.prompt({
      title: t('卸载 license'),
      description: (
        <p>
          {t('将卸载当前 license（')}<b className="font-mono" lang="en">{normalizeTier(st.tier)}</b> {t('档')}
          {st.licensee ? t('，被授权方 {v1}', { v1: st.licensee }) : ''}{t('）。卸载后实例降回')}{' '}
          <b>{t('community 地板')}</b>{t('：门控槽位（含其包型仓的建仓/写入）即刻关闭，既有制品读不受影响 （降级不劫持数据）。')}
          <br />{t('输入')} <b className="font-mono" lang="en">UNINSTALL</b> {t('以确认。')}
        </p>
      ),
      placeholder: 'UNINSTALL',
      mono: true,
      danger: true,
      confirmLabel: t('卸载 license'),
      cancelLabel: t('取消'),
      anchor: 'license-uninstall-confirm',
      validate: (v) => (v === 'UNINSTALL' ? null : t('需输入 UNINSTALL')),
    })
    if (typed !== 'UNINSTALL') return
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
      {state.status === 'loading' && <StateSkeleton lines={5} />}
      {state.status === 'error' && state.error && <ErrorCard error={state.error} onRetry={state.reload} />}
      {state.status === 'ok' && state.data && (
        <>
          <div className="kv">
            <span className="k">{t('当前档位')}</span>
            <span>
              <span className={`badge ${tierBadgeClass(state.data.tier)}`} data-testid="license-tier" lang="en">
                {normalizeTier(state.data.tier)}
              </span>
              <span className="ml-2 text-2">
                {state.data.licensed ? t('已授权') : t('未安装 license')}
              </span>
            </span>
          </div>
          {state.data.licensed && (
            <>
              <div className="kv">
                <span className="k">{t('被授权方')}</span>
                <span className="font-mono" lang="en" data-testid="license-licensee">
                  {state.data.licensee || '—'}
                </span>
              </div>
              <div className="kv">
                <span className="k">License ID</span>
                <span className="font-mono" lang="en">{state.data.licenseId || '—'}</span>
              </div>
              <div className="kv">
                <span className="k">{t('签发时间')}</span>
                <span className="font-mono" lang="en">{state.data.issuedAt || '—'}</span>
              </div>
            </>
          )}
          {expiryBlock(state.data)}
          {!state.data.licensed && (
            <p className="field-hint" data-testid="license-floor">
              {t('未安装 license：实例按')} <b>{t('community 地板')}</b>{t('运行——五核心包型与基础能力恒解锁， 门控槽位呈锁定态。下方矩阵即全槽位实时求值（与门控执行同源）。')}
            </p>
          )}

          {readOnly && (
            <p className="license-note" data-testid="license-readonly-note">
              {t('ⓘ 只读管理员视角：license 安装/卸载是管理面写操作（system:write，仅全量 admin）；本页只读呈现，服务端 403 兜底。')}
            </p>
          )}

          {admin && (
            <div className="license-install-block">
              <div className="field mb-0">
                <label htmlFor="license-doc">{t('安装 license 文档')}</label>
                <TextArea
                  id="license-doc"
                  mono
                  lang="en"
                  rows={5}
                  className="max-w-[720px]"
                  value={doc}
                  onChange={(e) => setDoc(e.target.value)}
                  placeholder={t('粘贴 license 文档全文（.lic）——验签失败会被原样拒绝，当前 license 不受影响')}
                  data-testid="license-doc-input"
                  spellCheck={false}
                />
                <p className="field-hint">{t('装载即刻生效（进程内原子切换，无撕裂）；GET 永不回显文档原文（NFR-S52）。')}</p>
              </div>
              {installError && (
                <AlertBox severity="error" testid="license-install-error">
                  <div className="font-medium">{t('装载被拒（HTTP')} {installError.status || t('网络')}{t('）')}</div>
                  <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{installError.message}</div>
                </AlertBox>
              )}
              <div className="form-actions mt-3">
                <Button
                  size="sm"
                  disabled={doc.trim() === '' || busy}
                  data-testid="license-install"
                  onClick={() => void doInstall()}
                >
                  {busy ? t('处理中…') : t('装载 license')}
                </Button>
                {state.data.licensed && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="border-destructive/50 text-destructive hover:bg-destructive/10"
                    disabled={busy}
                    data-testid="license-uninstall"
                    onClick={() => void doUninstall(state.data!)}
                  >
                    {t('卸载 license')}
                  </Button>
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
 *  trashcan/webhook 两 feature 槽 → 自有 brand 标；其余 feature 槽不在
 *  30 枚集内——无图标（注记豁免）。 */
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
    <tr className={`${rowClass} border-b border-border/60 hover:bg-accent`} data-testid={`addons-row-${row.id}`}>
      <td className="px-3 py-1.5 font-mono" lang="en">{row.id}</td>
      <td className="px-3 py-1.5">
        {/* 包型身份位走 brand 版；锁定/禁用行的置灰由 .is-locked/
            .is-disabled 行级 opacity 承载（license.css 沿用） */}
        <span className="inline-flex items-center gap-1.5">
          {iconId && <PkgIcon id={iconId} variant="brand" size={18} />}
          {row.displayName}
        </span>
      </td>
      <td className="px-3 py-1.5">
        <span className="badge neutral mono" lang="en">{row.kind}</span>
      </td>
      <td className="px-3 py-1.5" data-testid={`addons-tier-${row.id}`}>
        {tier === 'community' ? (
          <span className="text-2" title={t('community 地板：无 license 也解锁')}>—</span>
        ) : (
          <span className={`badge ${tierBadgeClass(tier)}`} lang="en">{tier}</span>
        )}
      </td>
      <td className="px-3 py-1.5" data-testid={`addons-state-${row.id}`}>
        {row.enabled ? (
          <>
            <span className="status-dot ok" aria-hidden="true" />{t('已解锁')}
          </>
        ) : disabledCfg ? (
          <>
            <span className="badge warning">{t('⊘ 已禁用')}</span>
            <span className="ml-1.5 text-2" title={row.reason ?? ''}>{t('配置熔断（addons.disabled）')}</span>
          </>
        ) : (
          <>
            <span aria-hidden="true">⊘</span> {t('锁定')}
            <span className="ml-1.5 text-2" title={row.reason ?? ''}>{t('需要')} {tier} {t('档')}</span>
          </>
        )}
      </td>
    </tr>
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
          <h2 className="text-lg font-semibold">License &amp; Add-ons</h2>
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
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">License &amp; Add-ons</h2>
        <span className="text-aux text-2">{t('档位 × addon 解锁矩阵（实时求值，与门控执行同源）')}</span>
      </div>

      <LicenseCard rev={rev} onChanged={bump} />

      <section className="card section" data-testid="addons-card">
        <h3>Add-ons</h3>
        {addons.status === 'loading' && <StateSkeleton lines={6} />}
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
            <table className="addons-table w-full text-dense" data-testid="addons-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-3 py-2 font-medium">ID</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('名称')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('类型')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('最低档位')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('状态')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <AddonTableRow key={row.id} row={row} />
                ))}
              </tbody>
            </table>
            <p className="field-hint" style={{ marginBottom: 0 }}>
              {t('Enabled 由 license 档位与 addons.disabled 配置决定，不可手动切换；锁定槽位在装对应档位 license 后即刻解锁。')}
            </p>
          </>
        )}
      </section>
    </div>
  )
}
