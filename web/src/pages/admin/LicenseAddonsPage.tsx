import { useState } from 'react'
import type { ReactNode } from 'react'

import { useAuth } from '../../app/AuthContext'
import { useToast } from '../../app/ToastContext'
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
        <span className="k">有效期</span>
        <span>{st.licensed ? '永久（perpetual）' : '—'}</span>
      </div>
    )
  }
  const d = st.daysToExpiry
  return (
    <>
      <div className="kv">
        <span className="k">有效期至</span>
        <span className="mono" lang="en">
          {st.expiresAt || '—'}
        </span>
      </div>
      <div className="kv">
        <span className="k">倒计时</span>
        <span>
          {d === null
            ? '—'
            : d >= 0
              ? `剩 ${d} 天`
              : `已过期 ${-d} 天（D6 无宽限：已降级 community，读不劫持）`}
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
      toast.success(`License 已装载（${normalizeTier(st.tier)} 档，即刻生效）`)
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
        <p>
          将卸载当前 license（<b className="mono" lang="en">{normalizeTier(st.tier)}</b> 档
          {st.licensee ? `，被授权方 ${st.licensee}` : ''}）。卸载后实例降回{' '}
          <b>community 地板</b>：门控槽位（含其包型仓的建仓/写入）即刻关闭，既有制品读不受影响
          （降级不劫持数据）。
        </p>
        <div className="field" style={{ maxWidth: 'none', marginBottom: 0 }}>
          <label htmlFor="lic-uninstall-confirm">
            输入 <b className="mono" lang="en">UNINSTALL</b> 以确认：
          </label>
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
      title: '卸载 license',
      body,
      danger: true,
      confirmLabel: '卸载 license',
      confirmDisabled: () => holder.typed !== 'UNINSTALL',
    })
    if (!ok) return
    setBusy(true)
    try {
      const text = await uninstallLicense()
      toast.success(text) // 服务端文案原样（"License removed successfully."）
      onChanged()
    } catch (err) {
      toast.error(`卸载失败：${errText(err)}`)
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
            <span className="k">当前档位</span>
            <span>
              <span className={tierBadgeClass(state.data.tier)} data-testid="license-tier" lang="en">
                {normalizeTier(state.data.tier)}
              </span>
              <span className="text-2" style={{ marginLeft: 8 }}>
                {state.data.licensed ? '已授权' : '未安装 license'}
              </span>
            </span>
          </div>
          {state.data.licensed && (
            <>
              <div className="kv">
                <span className="k">被授权方</span>
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
                <span className="k">签发时间</span>
                <span className="mono" lang="en">
                  {state.data.issuedAt || '—'}
                </span>
              </div>
            </>
          )}
          {expiryBlock(state.data)}
          {!state.data.licensed && (
            <p className="field-hint" data-testid="license-floor">
              未安装 license：实例按 <b>community 地板</b>运行——五核心包型与基础能力恒解锁，
              门控槽位呈锁定态。下方矩阵即全槽位实时求值（与门控执行同源）。
            </p>
          )}

          {readOnly && (
            <p className="license-note" data-testid="license-readonly-note">
              ⓘ 只读管理员视角：license 安装/卸载是管理面写操作（system:write，仅全量
              admin）；本页只读呈现，服务端 403 兜底。
            </p>
          )}

          {admin && (
            <div className="license-install-block">
              <div className="field" style={{ marginBottom: 0 }}>
                <label htmlFor="license-doc">安装 license 文档</label>
                <textarea
                  id="license-doc"
                  className="mono-input"
                  rows={5}
                  value={doc}
                  onChange={(e) => setDoc(e.target.value)}
                  placeholder="粘贴 license 文档全文（.lic）——验签失败会被原样拒绝，当前 license 不受影响"
                  data-testid="license-doc-input"
                  lang="en"
                  spellCheck={false}
                />
                <p className="field-hint">
                  装载即刻生效（进程内原子切换，无撕裂）；GET 永不回显文档原文（NFR-S52）。
                </p>
              </div>
              {installError && (
                <div className="form-error" data-testid="license-install-error" role="alert">
                  <div className="headline">装载被拒（HTTP {installError.status || '网络'}）</div>
                  <div className="raw" lang="en">
                    {installError.message}
                  </div>
                </div>
              )}
              <div className="form-actions" style={{ marginTop: 12 }}>
                <button
                  type="button"
                  className="btn primary"
                  disabled={doc.trim() === '' || busy}
                  data-testid="license-install"
                  onClick={() => void doInstall()}
                >
                  {busy ? '处理中…' : '装载 license'}
                </button>
                {state.data.licensed && (
                  <button
                    type="button"
                    className="btn danger"
                    disabled={busy}
                    data-testid="license-uninstall"
                    onClick={() => void doUninstall(state.data!)}
                  >
                    卸载 license
                  </button>
                )}
              </div>
            </div>
          )}
        </>
      )}
    </section>
  )
}

function AddonTableRow({ row }: { row: AddonRow }) {
  const tier = normalizeTier(row.minTier)
  const disabledCfg = isDisabledByConfig(row)
  const rowClass = row.enabled ? '' : disabledCfg ? 'is-disabled' : 'is-locked'
  return (
    <tr className={rowClass} data-testid={`addons-row-${row.id}`}>
      <td className="mono" lang="en">
        {row.id}
      </td>
      <td>{row.displayName}</td>
      <td>
        <span className="badge neutral mono" lang="en">
          {row.kind}
        </span>
      </td>
      <td data-testid={`addons-tier-${row.id}`}>
        {tier === 'community' ? (
          <span className="text-2" title="community 地板：无 license 也解锁">
            —
          </span>
        ) : (
          <span className={tierBadgeClass(tier)} lang="en">
            {tier}
          </span>
        )}
      </td>
      <td data-testid={`addons-state-${row.id}`}>
        {row.enabled ? (
          <>
            <span className="status-dot ok" aria-hidden="true" />
            已解锁
          </>
        ) : disabledCfg ? (
          <>
            <span className="badge warning">⊘ 已禁用</span>
            <span className="text-2" style={{ marginLeft: 6 }} title={row.reason ?? ''}>
              配置熔断（addons.disabled）
            </span>
          </>
        ) : (
          <>
            <span aria-hidden="true">⊘</span> 锁定
            <span className="text-2" style={{ marginLeft: 6 }} title={row.reason ?? ''}>
              需要 {tier} 档
            </span>
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
          <h2>License &amp; Add-ons</h2>
        </div>
        <EmptyState
          message="无权限访问 License &amp; Add-ons"
          hint="license 与 addon 状态属于管理面（system:read，需 admin / readonly_admin）。"
        />
      </div>
    )
  }

  const rows = addons.data ?? []
  return (
    <div data-testid="license-page">
      <div className="page-header">
        <h2>License &amp; Add-ons</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          档位 × addon 解锁矩阵（实时求值，与门控执行同源）
        </span>
      </div>

      <LicenseCard rev={rev} onChanged={bump} />

      <section className="card section" data-testid="addons-card">
        <h3>Add-ons</h3>
        {addons.status === 'loading' && <Skeleton lines={6} />}
        {addons.status === 'error' && addons.error && <ErrorCard error={addons.error} onRetry={addons.reload} />}
        {addons.status === 'forbidden' && (
          <EmptyState message="无权限读取 addon 清单" hint="GET /api/v1/addons 需要管理面读权限。" />
        )}
        {addons.status === 'ok' && rows.length === 0 && (
          <EmptyState
            message="此实例未装配 addon 注册表"
            hint="GET /api/v1/addons 为空数组——该组装形态的自我描述（pre-M10 单元栈），非错误。"
          />
        )}
        {addons.status === 'ok' && rows.length > 0 && (
          <>
            <table className="table addons-table" data-testid="addons-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>名称</th>
                  <th>类型</th>
                  <th>最低档位</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <AddonTableRow key={row.id} row={row} />
                ))}
              </tbody>
            </table>
            <p className="field-hint" style={{ marginBottom: 0 }}>
              Enabled 由 license 档位与 addons.disabled 配置决定，不可手动切换；锁定槽位在装对应档位
              license 后即刻解锁。
            </p>
          </>
        )}
      </section>
    </div>
  )
}
