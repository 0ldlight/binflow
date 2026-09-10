// 服务状态页（T-459 / FR-145.5——P3 新栈重写；7.161 对位形态）：
// - 总体 + 子系统：GET /api/v1/health；版本三值与 GET /api/system/version
//   同源对账（useVersion 模块级缓存）。
// - 实例 URL = window.location.origin + /binflow（呈现事实源）。
// - 单二进制恒单节点（不伪造多节点表）。
// - 调度服务：GET /api/v1/system/schedules（台账只读投影；cron 编辑面归
//   维护/备份页）。
// - 缺位登记：Uptime 无端点（status-uptime-gap 如实注记）；/metrics 是
//   Prometheus 抓取面，控制台不消费。
// 锚族原样：status-page/status-overall/status-badge/status-version/
// status-url/status-nodes/status-uptime-gap/status-sys(-<name>)?/
// status-schedules/status-sched-<i>/status-refresh。
import { Badge } from '@/components/layout/bits'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { getHealth, getSchedules } from '@/lib/api'
import type { HealthInfo, SubsystemStatus } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import { useVersion } from '@/lib/useVersion'
import { tr } from '@/i18n'

const t = tr('monitoring')

/** 调度域中文名（wire 域值不翻译进排障列，标签给中文语境） */
const DOMAIN_LABEL: Record<string, string> = {
  maintenance: t('维护'),
  backup: t('备份'),
  replication: t('复制'),
}

/** RFC3339 UTC 串 → 人类可读（秒精度保留） */
function fmtRFC3339(v: string): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

function SubsystemRow({ name, st }: { name: string; st: SubsystemStatus }) {
  const ok = st.status === 'ok'
  return (
    <tr data-testid={`status-sys-${name}`} className="border-b border-border/60">
      <td className="px-3 py-1.5">
        <span className={`status-dot ${ok ? 'ok' : 'err'}`} aria-hidden="true" />{' '}
        <span className="font-mono" lang="en">{name}</span>
      </td>
      <td className="px-3 py-1.5 font-mono" lang="en">{st.status}</td>
      <td className="break-words px-3 py-1.5" title={st.detail ?? ''}>
        {ok ? <span className="text-muted-foreground">—</span> : (st.detail ?? st.status)}
      </td>
    </tr>
  )
}

export default function ServiceStatusPage() {
  const version = useVersion()
  const health = useAsync<HealthInfo>(getHealth, [])
  const schedules = useAsync(getSchedules, [])
  // 手动刷新 = 两读面一起重取
  const doRefresh = () => {
    health.reload()
    schedules.reload()
  }

  const ok = health.status === 'ok' && health.data?.status === 'ok'
  const instanceUrl = `${window.location.origin}/binflow`

  return (
    <div data-testid="status-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('服务状态')}</h2>
        <span className="text-aux text-2">{t('实例健康、子系统与调度服务运行面（只读）')}</span>
      </div>

      {health.status === 'loading' && <StateSkeleton lines={6} />}
      {health.status === 'forbidden' && health.error && (
        <EmptyState
          message={t('无权限查看服务状态')}
          hint={t('健康端点为管理员视图（GET /api/v1/health 仅 admin / readonly_admin——system:read）。')}
        />
      )}
      {health.status === 'error' && health.error && <ErrorCard error={health.error} onRetry={health.reload} />}

      {health.status === 'ok' && health.data && (
        <>
          {/* 总体卡（7.161 对位：Online 徽标 + 服务卡） */}
          <section className="card section" data-testid="status-overall">
            <div className="kv">
              <span className="k">{t('总体状态')}</span>
              <span>
                <span className={`status-dot ${ok ? 'ok' : 'err'}`} aria-hidden="true" />{' '}
                <span className="font-mono" lang="en" data-testid="status-badge">
                  {health.data.status}
                </span>
              </span>
            </div>
            <div className="kv">
              <span className="k">{t('产品 / 版本')}</span>
              <span className="font-mono" lang="en" data-testid="status-version">
                {version ? `${version.product} v${version.version}` : '—'}
                {version?.revision ? ` (${version.revision.slice(0, 12)})` : ''}
              </span>
            </div>
            <div className="kv">
              <span className="k">{t('实例 URL')}</span>
              <span className="font-mono" lang="en" data-testid="status-url">
                {instanceUrl}
              </span>
            </div>
            <div className="kv">
              <span className="k">{t('节点')}</span>
              <span data-testid="status-nodes">{t('单节点（单二进制部署）')}</span>
            </div>
            <div className="kv">
              <span className="k">{t('运行时长')}</span>
              <span className="text-2" data-testid="status-uptime-gap">{t('无查询端点，不呈现')}</span>
            </div>
            <p className="field-hint mb-0">
              {t('运行时长（Uptime）无 REST 端点（health / version 均不含进程启动时间）——如实缺位不伪造； 指标抓取走根级')} <span className="font-mono" lang="en">/metrics</span>{t('（Prometheus 面，控制台不消费）。')}
            </p>
          </section>

          {/* 子系统表（storage / metadata / registry） */}
          <section className="card section" data-testid="status-sys">
            <h3 className="mb-2 text-dense font-semibold">{t('子系统')}</h3>
            <table className="w-full text-dense">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-3 py-2 font-medium">{t('子系统')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('状态')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('详情')}</th>
                </tr>
              </thead>
              <tbody>
                <SubsystemRow name="storage" st={health.data.storage} />
                <SubsystemRow name="metadata" st={health.data.metadata} />
                <SubsystemRow name="registry" st={health.data.registry} />
              </tbody>
            </table>
          </section>

          {/* 调度服务（T-450 台账只读投影） */}
          <section className="card section" data-testid="status-schedules">
            <h3 className="mb-2 text-dense font-semibold">{t('调度服务')}</h3>
            {schedules.status === 'loading' && <StateSkeleton lines={3} />}
            {schedules.status === 'error' && schedules.error && (
              <ErrorCard error={schedules.error} onRetry={schedules.reload} />
            )}
            {schedules.status === 'ok' && (schedules.data?.schedules.length ?? 0) === 0 && (
              <EmptyState
                message={t('未配置定时任务')}
                hint={t('GC / 清理 / 备份等 cron 调度在配置落库后出现在这里（GET /api/v1/system/schedules）。')}
              />
            )}
            {schedules.status === 'ok' && (schedules.data?.schedules.length ?? 0) > 0 && (
              <table className="w-full text-dense">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-3 py-2 font-medium">{t('任务')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('域')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">cron</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('下次运行')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('上次运行 / 结果')}</th>
                  </tr>
                </thead>
                <tbody>
                  {schedules.data!.schedules.map((s, i) => (
                    <tr key={`${s.domain}:${s.key}`} data-testid={`status-sched-${i}`} className="border-b border-border/60 hover:bg-accent">
                      <td className="px-3 py-1.5 font-mono" lang="en">{s.key}</td>
                      <td className="px-3 py-1.5">{DOMAIN_LABEL[s.domain] ?? s.domain}</td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{s.cronExp || '—'}</td>
                      <td className="px-3 py-1.5">
                        {s.enabled ? (
                          <span className="font-mono" lang="en" title={s.nextRun}>
                            {fmtRFC3339(s.nextRun)}
                          </span>
                        ) : (
                          <Badge>{t('已停用')}</Badge>
                        )}
                      </td>
                      <td className="px-3 py-1.5">
                        {s.lastRun ? (
                          <>
                            <span className="font-mono" lang="en">{fmtRFC3339(s.lastRun)}</span>{' '}
                            {s.lastStatus && (
                              <span className="text-2" lang="en" title={s.lastError || undefined}>
                                {t('（')}{s.lastStatus}{s.lastError ? t('：{v1}', { v1: s.lastError }) : ''}{t('）')}
                              </span>
                            )}
                          </>
                        ) : (
                          <span className="text-muted-foreground">{t('未运行')}</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            <p className="field-hint mb-0">{t('调度台账为只读投影（cron 配置在维护 / 备份页编辑）；「上次运行」时间与结果来自 台账行，未跑过的任务如实标注。')}</p>
          </section>

          <p className="mt-2">
            <Button variant="outline" size="sm" onClick={doRefresh} data-testid="status-refresh">{t('刷新')}</Button>
          </p>
        </>
      )}
    </div>
  )
}
