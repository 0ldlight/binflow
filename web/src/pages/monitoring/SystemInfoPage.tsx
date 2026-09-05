import Paper from '@mui/material/Paper'
import Typography from '@mui/material/Typography'

import { useAuth } from '../../app/AuthContext'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { getHealth, isReadOnlyAdmin } from '../../lib/api'
import type { HealthInfo, SubsystemStatus } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import { useVersion } from '../../lib/useVersion'

// 系统信息页（console-m8 §6.19 / §1.2「General → Settings 重塑」，T-238——
// /admin/general/settings 落真身；原设置页的改密块归 /profile，T-239 拆分；
// **T-459（FR-145.5 / parity B-1.11）归位服务节点组**：路由迁
// /admin/monitoring/system-info——监控组三页 Logs/Status/Info 之一；旧
// /admin/general/settings 深链经路由表一次性 replace 折入，锚零改名）：
//
// - 实例信息：版本 / 修订 / 产品（GET /api/system/version 开放端点）+
//   发行（许可）行——BinFlow 无许可证端点，静态产品定位（与侧栏许可行
//   同源文案），不伪造许可数据。
// - 健康卡：/api/v1/health 子系统行（storage / metadata / registry），403
//   驱动 L3 隐藏（settings-health 锚沿 T-98 冻结口径，非 admin 不渲染）。
//   T-459 起运行面主承载移 Service Status 页（/admin/monitoring/status），
//   本卡保留为实例信息的健康摘要（锚冻结，不拆）。
// - 契约冻结注记：Server Name / Base URL / 匿名读开关 / 数据目录 / 日志
//   级别无查询端点（M4 缺口未补）——不展示、不伪造；Logo / Custom Message
//   写入口不建（§1.2）。
// - 页根锚 = settings（console-ux §10.5 冻结锚，路由迁移不改锚名）。
// T-344 批 D：残面换装——.card → Paper（类名留 DOM，:not shim 排除旧配方）、
// h3 → Typography subtitle2；kv 行族是布局 utility，原样保留。

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
    <Paper component="section" className="card section" elevation={1} data-testid="settings-health">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
        健康
      </Typography>
      {health.status === 'loading' && <Skeleton lines={4} />}
      {health.status === 'error' && health.error && <ErrorCard error={health.error} onRetry={health.reload} />}
      {health.status === 'ok' && health.data && (
        <>
          <div className="kv">
            <span className="k">总体</span>
            <span>
              <span className={`status-dot ${health.data.status === 'ok' ? 'ok' : 'err'}`} aria-hidden="true" />{' '}
              <span className="mono" lang="en">
                {health.data.status}
              </span>
            </span>
          </div>
          <SubsystemRow name="storage" st={health.data.storage} />
          <SubsystemRow name="metadata" st={health.data.metadata} />
          <SubsystemRow name="registry" st={health.data.registry} />
        </>
      )}
    </Paper>
  )
}

export default function SystemInfoPage() {
  const version = useVersion()
  const { session } = useAuth()

  return (
    <div data-testid="settings">
      <div className="page-header">
        <h2>系统信息</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>
          只读展示（配置面经实例 YAML 管理，无控制台写端点）
        </span>
      </div>

      <Paper component="section" className="card section" elevation={1} data-testid="settings-instance">
        <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>
          实例信息
        </Typography>
        <div className="kv">
          <span className="k">产品</span>
          <span className="mono" lang="en">
            {version?.product ?? '—'}
          </span>
        </div>
        <div className="kv">
          <span className="k">版本</span>
          <span className="mono" lang="en" data-testid="settings-version">
            {version ? `v${version.version}` : '—'}
          </span>
        </div>
        <div className="kv">
          <span className="k">修订</span>
          <span className="mono" lang="en">
            {version?.revision || '—'}
          </span>
        </div>
        <div className="kv">
          <span className="k">发行（许可）</span>
          <span data-testid="settings-license">单二进制制品仓库 · 自包含发行</span>
        </div>
        <div className="kv">
          <span className="k">当前用户</span>
          <span>
            {session?.username}
            {/* admin 布尔是角色镜像：readonly_admin 为 false——先判只读再判 admin */}
            {session && isReadOnlyAdmin(session) ? '（readonly_admin）' : session?.admin ? '（admin）' : ''}
          </span>
        </div>
        <p className="field-hint" style={{ marginBottom: 0 }}>
          Server Name / Base URL / 匿名读开关 / 数据目录 / 日志级别无查询端点（契约冻结），
          不展示、不伪造；控制台不含配置写入口（Logo / Custom Message 不建）。
        </p>
      </Paper>

      <HealthSection />
    </div>
  )
}
