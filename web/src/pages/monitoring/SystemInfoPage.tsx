// 系统信息页（console-m8 §6.19——P3 新栈重写；T-459 归位服务节点组，
// 路由 /admin/monitoring/system-info；页根锚 = settings（冻结锚，路由
// 迁移不改锚名））：
// - 实例信息：版本 / 修订 / 产品（GET /api/system/version 开放端点）+
//   发行（许可）行——静态产品定位，不伪造许可数据。
// - 健康卡：/api/v1/health 子系统行（403 驱动 L3 隐藏——settings-health
//   锚沿 T-98 冻结口径）。
// - 契约冻结注记：Server Name / Base URL / 匿名读开关 / 数据目录 / 日志
//   级别无查询端点——不展示、不伪造；Logo / Custom Message 写入口不建。
// 锚族原样：settings/settings-instance/settings-version/settings-license/
// settings-health(-<name>)?。
import { useAuth } from '@/app/AuthContext'
import { ErrorCard, StateSkeleton } from '@/components/layout/states'
import { getHealth, isReadOnlyAdmin } from '@/lib/api'
import type { HealthInfo, SubsystemStatus } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import { useVersion } from '@/lib/useVersion'
import { tr } from '@/i18n'

const t = tr('monitoring')

function SubsystemRow({ name, st }: { name: string; st: SubsystemStatus }) {
  const ok = st.status === 'ok'
  return (
    <div className="kv" data-testid={`settings-health-${name}`}>
      <span className="k">
        <span className={`status-dot ${ok ? 'ok' : 'err'}`} aria-hidden="true" />
        {name}
      </span>
      <span className={ok ? 'text-2' : ''} title={st.detail ?? ''}>
        {ok ? 'ok' : (st.detail ?? st.status)}
      </span>
    </div>
  )
}

function HealthSection() {
  const health = useAsync<HealthInfo>(getHealth, [])
  if (health.status === 'forbidden') return null
  return (
    <section className="card section" data-testid="settings-health">
      <h3 className="mb-2 text-dense font-semibold">{t('健康')}</h3>
      {health.status === 'loading' && <StateSkeleton lines={4} />}
      {health.status === 'error' && health.error && <ErrorCard error={health.error} onRetry={health.reload} />}
      {health.status === 'ok' && health.data && (
        <>
          <div className="kv">
            <span className="k">{t('总体')}</span>
            <span>
              <span className={`status-dot ${health.data.status === 'ok' ? 'ok' : 'err'}`} aria-hidden="true" />{' '}
              <span className="font-mono" lang="en">{health.data.status}</span>
            </span>
          </div>
          <SubsystemRow name="storage" st={health.data.storage} />
          <SubsystemRow name="metadata" st={health.data.metadata} />
          <SubsystemRow name="registry" st={health.data.registry} />
        </>
      )}
    </section>
  )
}

export default function SystemInfoPage() {
  const version = useVersion()
  const { session } = useAuth()

  return (
    <div data-testid="settings">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('系统信息')}</h2>
        <span className="text-aux text-2">{t('只读展示（配置面经实例 YAML 管理，无控制台写端点）')}</span>
      </div>

      <section className="card section" data-testid="settings-instance">
        <h3 className="mb-2 text-dense font-semibold">{t('实例信息')}</h3>
        <div className="kv">
          <span className="k">{t('产品')}</span>
          <span className="font-mono" lang="en">{version?.product ?? '—'}</span>
        </div>
        <div className="kv">
          <span className="k">{t('版本')}</span>
          <span className="font-mono" lang="en" data-testid="settings-version">
            {version ? `v${version.version}` : '—'}
          </span>
        </div>
        <div className="kv">
          <span className="k">{t('修订')}</span>
          <span className="font-mono" lang="en">{version?.revision || '—'}</span>
        </div>
        <div className="kv">
          <span className="k">{t('发行（许可）')}</span>
          <span data-testid="settings-license">{t('单二进制制品仓库 · 自包含发行')}</span>
        </div>
        <div className="kv">
          <span className="k">{t('当前用户')}</span>
          <span>
            {session?.username}
            {/* admin 布尔是角色镜像：readonly_admin 为 false——先判只读再判 admin */}
            {session && isReadOnlyAdmin(session) ? t('（readonly_admin）') : session?.admin ? t('（admin）') : ''}
          </span>
        </div>
        <p className="field-hint mb-0">
          {t('Server Name / Base URL / 匿名读开关 / 数据目录 / 日志级别无查询端点（契约冻结）， 不展示、不伪造；控制台不含配置写入口（Logo / Custom Message 不建）。')}
        </p>
      </section>

      <HealthSection />
    </div>
  )
}
