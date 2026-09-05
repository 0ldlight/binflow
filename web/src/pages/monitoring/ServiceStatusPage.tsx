import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Paper from '@mui/material/Paper'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Typography from '@mui/material/Typography'

import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { getHealth, getSchedules } from '../../lib/api'
import type { HealthInfo, SubsystemStatus } from '../../lib/api'
import { useAsync } from '../../lib/useAsync'
import { useVersion } from '../../lib/useVersion'
import { tr } from '../../i18n'

const t = tr('monitoring')

// 服务状态页（T-459 / FR-145.5——监控组三页之二；7.161.20 活体形态 =
// /ui/admin/monitoring/service-status：总体 Online 徽标 + 服务卡〔URL +
// 节点数〕+ 节点卡〔URL/Version/Uptime〕；探针 reports/agents/t459-probe/）。
//
// BinFlow 载体（零新端点——AC4「对位既有 metrics/health 端点」）：
// - 总体 + 子系统：GET /api/v1/health（status/version/storage/metadata/
//   registry——system:read）；版本三值另与 GET /api/system/version 同源
//   对账（useVersion 模块级缓存）。
// - 实例 URL：浏览器在看的真实地址（window.location.origin + /binflow）
//   ——呈现事实源，非服务端回显。
// - 节点模型：单二进制 = 恒单节点（7.161 的节点列表是 HA 拓扑面，BinFlow
//   无集群域——不伪造多节点表）。
// - 调度服务：GET /api/v1/system/schedules（T-450 台账投影——maintenance/
//   backup/replication 三域的 cron/next-run/last-run/状态）。这是
//   「服务」在 BinFlow 语义下最贴近的运行面。
// - 缺位登记（不伪造）：Uptime 无端点（health/version 均不含进程启动
//   时间）——status-uptime-gap 如实注记；根级 /metrics 是 Prometheus
//   抓取面（Basic-auth 族，cookie 结构性不达——/healthz 同款「console 不
//   消费」口径，console-ux §3.6.2）。
// - 四态：loading 骨架 / health 403 → L2 无权限卡（管理面语义）/
//   error 错误卡 + 重试 / ok。readonly_admin 读面全通。
//
// T-462 落地注记：调度表仍是只读投影；cron 字段的编辑面在维护（GC）页
// 与备份页（本页不改写——读面姿态不变）。

/** 调度域中文名（wire 域值不翻译进排障列，标签给中文语境） */
const DOMAIN_LABEL: Record<string, string> = {
  maintenance: t('维护'),
  backup: t('备份'),
  replication: t('复制'),
}

/** RFC3339 UTC 串 → 人类可读（秒精度保留——next-run 排障要精确到秒） */
function fmtRFC3339(v: string): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

function SubsystemRow({ name, st }: { name: string; st: SubsystemStatus }) {
  const ok = st.status === 'ok'
  return (
    <TableRow data-testid={`status-sys-${name}`}>
      <TableCell>
        <span className={`status-dot ${ok ? 'ok' : 'err'}`} aria-hidden="true" />{' '}
        <span className="mono" lang="en">
          {name}
        </span>
      </TableCell>
      <TableCell className="mono" lang="en">
        {st.status}
      </TableCell>
      <TableCell sx={{ whiteSpace: 'normal', wordBreak: 'break-word' }} title={st.detail ?? ''}>
        {ok ? <span className="text-muted">—</span> : (st.detail ?? st.status)}
      </TableCell>
    </TableRow>
  )
}

export default function ServiceStatusPage() {
  const version = useVersion()
  const health = useAsync<HealthInfo>(getHealth, [])
  const schedules = useAsync(getSchedules, [])
  // 手动刷新 = 两读面一起重取（各自 useAsync 的 reload——游标/缓存自持）
  const doRefresh = () => {
    health.reload()
    schedules.reload()
  }

  const ok = health.status === 'ok' && health.data?.status === 'ok'
  const instanceUrl = `${window.location.origin}/binflow`

  return (
    <div data-testid="status-page">
      <div className="page-header">
        <h2>{t('服务状态')}</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{t('实例健康、子系统与调度服务运行面（只读）')}        </span>
      </div>

      {health.status === 'loading' && <Skeleton lines={6} />}
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
          <Paper component="section" className="card section" elevation={1} data-testid="status-overall">
            <div className="kv">
              <span className="k">{t('总体状态')}</span>
              <span>
                <span className={`status-dot ${ok ? 'ok' : 'err'}`} aria-hidden="true" />{' '}
                <span className="mono" lang="en" data-testid="status-badge">
                  {health.data.status}
                </span>
              </span>
            </div>
            <div className="kv">
              <span className="k">{t('产品 / 版本')}</span>
              <span className="mono" lang="en" data-testid="status-version">
                {version ? `${version.product} v${version.version}` : '—'}
                {version?.revision ? ` (${version.revision.slice(0, 12)})` : ''}
              </span>
            </div>
            <div className="kv">
              <span className="k">{t('实例 URL')}</span>
              <span className="mono" lang="en" data-testid="status-url">
                {instanceUrl}
              </span>
            </div>
            <div className="kv">
              <span className="k">{t('节点')}</span>
              <span data-testid="status-nodes">{t('单节点（单二进制部署）')}</span>
            </div>
            <div className="kv">
              <span className="k">{t('运行时长')}</span>
              <span className="text-2" data-testid="status-uptime-gap">{t('无查询端点，不呈现')}              </span>
            </div>
            <p className="field-hint" style={{ marginBottom: 0 }}>{t('运行时长（Uptime）无 REST 端点（health / version 均不含进程启动时间）——如实缺位不伪造； 指标抓取走根级')} <span className="mono" lang="en">/metrics</span>{t('（Prometheus 面，控制台不消费）。')}            </p>
          </Paper>

          {/* 子系统表（storage / metadata / registry——health 子卡） */}
          <Paper component="section" className="card section" elevation={1} data-testid="status-sys">
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>{t('子系统')}            </Typography>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell component="th" scope="col">{t('子系统')}</TableCell>
                  <TableCell component="th" scope="col">{t('状态')}</TableCell>
                  <TableCell component="th" scope="col">{t('详情')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                <SubsystemRow name="storage" st={health.data.storage} />
                <SubsystemRow name="metadata" st={health.data.metadata} />
                <SubsystemRow name="registry" st={health.data.registry} />
              </TableBody>
            </Table>
          </Paper>

          {/* 调度服务（T-450 台账只读投影；cron 编辑面归 T-462） */}
          <Paper component="section" className="card section" elevation={1} data-testid="status-schedules">
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1.5 }}>{t('调度服务')}            </Typography>
            {schedules.status === 'loading' && <Skeleton lines={3} />}
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
              <Table>
                <TableHead>
                  <TableRow>
                    <TableCell component="th" scope="col">{t('任务')}</TableCell>
                    <TableCell component="th" scope="col">{t('域')}</TableCell>
                    <TableCell component="th" scope="col">cron</TableCell>
                    <TableCell component="th" scope="col">{t('下次运行')}</TableCell>
                    <TableCell component="th" scope="col">{t('上次运行 / 结果')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {schedules.data!.schedules.map((s, i) => (
                    <TableRow key={`${s.domain}:${s.key}`} data-testid={`status-sched-${i}`} hover>
                      <TableCell className="mono" lang="en">
                        {s.key}
                      </TableCell>
                      <TableCell>{DOMAIN_LABEL[s.domain] ?? s.domain}</TableCell>
                      <TableCell className="mono" lang="en">
                        {s.cronExp || '—'}
                      </TableCell>
                      <TableCell>
                        {s.enabled ? (
                          <span className="mono" lang="en" title={s.nextRun}>
                            {fmtRFC3339(s.nextRun)}
                          </span>
                        ) : (
                          <Chip size="small" className="badge neutral" label={t('已停用')} />
                        )}
                      </TableCell>
                      <TableCell>
                        {s.lastRun ? (
                          <>
                            <span className="mono" lang="en">
                              {fmtRFC3339(s.lastRun)}
                            </span>{' '}
                            {s.lastStatus && (
                              <span className="text-2" lang="en" title={s.lastError || undefined}>{t('（')}{s.lastStatus}
                                {s.lastError ? t('：{v1}', { v1: s.lastError }) : ''}{t('）')}                              </span>
                            )}
                          </>
                        ) : (
                          <span className="text-muted">{t('未运行')}</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            <p className="field-hint" style={{ marginBottom: 0 }}>{t('调度台账为只读投影（cron 配置在维护 / 备份页编辑）；「上次运行」时间与结果来自 台账行，未跑过的任务如实标注。')}            </p>
          </Paper>

          <p style={{ marginTop: 'var(--bf-sp-2)' }}>
            <Button variant="outlined" size="small" onClick={doRefresh} data-testid="status-refresh">{t('刷新')}            </Button>
          </p>
        </>
      )}
    </div>
  )
}
