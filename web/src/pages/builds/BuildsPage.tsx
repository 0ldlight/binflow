import { Link, useParams, useSearchParams } from 'react-router-dom'

import Button from '@mui/material/Button'
import Chip from '@mui/material/Chip'
import Paper from '@mui/material/Paper'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Typography from '@mui/material/Typography'

import { CopyButton } from '../../components/CopyButton'
import { EmptyState } from '../../components/EmptyState'
import { ErrorCard } from '../../components/ErrorCard'
import { Skeleton } from '../../components/Skeleton'
import { useAsync } from '../../lib/useAsync'
import { tr } from '../../i18n'
import {
  DEFAULT_BUILD_REPO,
  fetchBuildRunEvents,
  getBuildRun,
  listBuildNames,
  listBuildNumbers,
} from './api'
import type { BuildInfo } from './api'

const t = tr('builds')

// Builds 页（T-512 / FR-152.3——build 域 FE 只读查询面；BundlesPage 三视图
// 先例）：
//
// 形态定案（票内留痕，活体不可用——以 docs/reverse/build-info.md §6 档位
// 核验节转引的 t226 观测〔V7〕+ 官方 REST 参考为书面锚）：
// - **导航挂靠**：应用模式「应用」分组（制品之后、Release Bundles 之前——
//   Artifactory 应用域族 Packages/Builds/Artifacts/Release Bundles 的相对
//   序，BinFlow 无 Packages 域跳过；专域名词对位 Access Tokens / Release
//   Bundles 先例不译）；图标 = Material build（扳手——CI 构建语义）。
// - **三视图一组件**（URL 即状态）：/builds（名单 GET /api/build）→
//   /builds/:name（号单 GET /api/build/{name}，started 倒序）→
//   /builds/:name/:number（详情 GET /api/build/{name}/{number}；?started=
//   消歧同名同号多 run）。
// - **列集裁定**：V7 观测列 = Build Name / Build Repository / Last Build
//   ID / Last Build Time——BinFlow 名单面 wire 只有 name+lastStarted（一行
//   = (name, build_repo)，无 repo/号字段；号在号单面）——Build Repository
//   / Last Build ID 无源不伪造，列集 = 构建名 + 最新启动（登记态差异，
//   parity 册 §9A-S7 注记随票回写）。
// - **读门语义如实**：名单面服务端可见集过滤（无授予用户 200 空集走
//   空态——零泄漏）；号单面空可见集 = 404（与不存在同形，build-not-found
//   承载）；详情面拒绝先于存在（403 = 读门拒绝即答案，build-denied 承载
//   ——BundlesPage 同款分立）。
// - **promote 入口裁定（票内）**：ux 册对 Builds 页无承载定案（本页为
//   M17 新面）——列表/详情只读面先行留痕：admin-note 引导 API（POST
//   /api/build/promote/{name}/{number}，body §2.4 全字段）；控制台动作面
//   归后续票，不伪造表单。
// - **事件时间线**（AC2）：消费 audit 面（零新端点）——build.upload/
//   append/promote/delete 四 run 级词（retention 为名级窗口事件不可归属
//   单 run，不进 run 时间线）；admin 面，非 admin 403 整段隐藏。
// - mono + 一键拷贝：module id / 制品 sha256 / CI URL；record-only 制品行
//   （path 空 = 行存不冒领关联）如实无链接呈现。

/** Java 规范启动形（2026-09-06T21:22:30.067+0000）→ 展示形（空格替 T，
 *  偏移保真）；不可解析原样返回（审计友好，不伪造） */
function fmtStarted(v: string): string {
  return v ? v.replace('T', ' ') : '—'
}

/** audit RFC3339 → 秒精度 UTC 形（mono 中立——监控页同款） */
function fmtAuditTime(v: string): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

/** run 级 audit 词 → 中文动作名（词面 mono 保真另呈；模块级 t() 求值点
 *  按 locale reload 重求值——APP_NAV 同款） */
const ACTION_LABEL: Record<string, string> = {
  'build.upload': t('发布'),
  'build.append': t('追加合并'),
  'build.promote': t('晋升'),
  'build.delete': t('删除'),
}

/** 404 承载（号单面 = 空可见集与不存在同形——零泄漏法；详情面 = 真缺 run） */
function BuildNotFound({ name }: { name?: string }) {
  return (
    <EmptyState
      testid="build-not-found"
      message={name ? t('构建 {name} 不存在（或当前会话不可见）', { name: name }) : t('构建不存在')}
      hint={t('号单面按会话可见集过滤——不可读的构建名与不存在的名同形（零泄漏）；详情面 404 = run 真缺（?started= 消歧同名同号多 run）。')}
      action={
        <Button variant="outlined" size="small" component={Link} to="/builds">{t('← 返回构建列表')}</Button>
      }
    />
  )
}

/** 403 承载（详情面——读门拒绝即答案，不与 404 混同） */
function BuildDenied() {
  return (
    <EmptyState
      testid="build-denied"
      message={t('无权限查看此构建')}
      hint={t('读门 = r(buildRepo, buildName) 镜像——403 即读门拒绝（拒绝即答案，不与不存在混同）。')}
      action={
        <Button variant="outlined" size="small" component={Link} to="/builds">{t('← 返回构建列表')}</Button>
      }
    />
  )
}

// ---- 视图 ①：构建名单 --------------------------------------------------------

function BuildNamesView() {
  const names = useAsync(listBuildNames, [])
  return (
    <div data-testid="builds-page">
      <div className="page-header">
        <h2>Builds</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{t('CI 构建记录（构建名 → run 号 → run 详情，只读查询面）')}        </span>
      </div>
      <p className="admin-note">{t('ⓘ 发布走 API：PUT /api/build（body = build info JSON，name/number 在 body）；promote 走 POST /api/build/promote/{name}/{number}——控制台动作面归后续票，本页不伪造表单。')}        </p>

      {names.status === 'loading' && <Skeleton lines={5} />}
      {names.status === 'error' && names.error && <ErrorCard error={names.error} onRetry={names.reload} />}
      {names.status === 'ok' && (names.data?.builds.length ?? 0) === 0 && (
        <EmptyState
          testid="builds-empty"
          illustration
          message={t('暂无可见的构建')}
          hint={t('服务端按会话可见集过滤（r(buildRepo, buildName) 授予面）——空集如实呈现；CI 经 PUT /api/build 发布（jf rt build-publish 同形）。')}
        />
      )}
      {names.status === 'ok' && (names.data?.builds.length ?? 0) > 0 && (
        <Paper component="section" className="card section" elevation={1} data-testid="builds-table">
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">{t('构建名')}</TableCell>
                <TableCell component="th" scope="col">{t('最新启动')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(names.data?.builds ?? []).map((b) => {
                const name = b.uri.replace(/^\//, '')
                return (
                  <TableRow key={`${name}|${b.lastStarted}`} hover data-testid={`builds-row-${name}`}>
                    <TableCell>
                      <Link className="row-link mono" lang="en" to={`/builds/${encodeURIComponent(name)}`}>
                        {name}
                      </Link>
                    </TableCell>
                    <TableCell className="mono" lang="en">{fmtStarted(b.lastStarted)}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </Paper>
      )}
    </div>
  )
}

// ---- 视图 ②：run 号单 --------------------------------------------------------

function BuildNumbersView({ name }: { name: string }) {
  const numbers = useAsync(() => listBuildNumbers(name), [name])
  const notFound = numbers.status === 'error' && numbers.error?.status === 404

  return (
    <div data-testid="build-runs-page">
      <div className="page-header">
        <h2>
          Builds / <span className="mono" lang="en">{name}</span>
        </h2>
        <Button variant="outlined" size="small" component={Link} to="/builds">{t('← 返回构建列表')}</Button>
      </div>

      {numbers.status === 'loading' && <Skeleton lines={5} />}
      {numbers.status === 'error' && numbers.error && !notFound && (
        <ErrorCard error={numbers.error} onRetry={numbers.reload} />
      )}
      {notFound && <BuildNotFound name={name} />}
      {numbers.status === 'ok' && (
        <Paper component="section" className="card section" elevation={1} data-testid="build-runs-table">
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">{t('run 号')}</TableCell>
                <TableCell component="th" scope="col">{t('启动时间')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(numbers.data?.buildsNumbers ?? []).map((n) => {
                const number = n.uri.replace(/^\//, '')
                return (
                  <TableRow key={`${number}|${n.started}`} hover data-testid={`build-run-row-${number}`}>
                    <TableCell>
                      <Link
                        className="row-link mono"
                        lang="en"
                        to={`/builds/${encodeURIComponent(name)}/${encodeURIComponent(number)}?started=${encodeURIComponent(n.started)}`}
                      >
                        {number}
                      </Link>
                    </TableCell>
                    <TableCell className="mono" lang="en">{fmtStarted(n.started)}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </Paper>
      )}
    </div>
  )
}

// ---- 视图 ③：run 详情 --------------------------------------------------------

/** promotion 历史（statuses[]——六元组；首行 = 现势〔最新行=现势，§2.4〕） */
function BuildStatuses({ statuses }: { statuses: BuildInfo['statuses'] }) {
  if (!statuses || statuses.length === 0) return null
  return (
    <Paper component="section" className="card section" elevation={1} data-testid="build-statuses">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1 }}>
        {t('promotion 历史（{v1} 条）', { v1: statuses.length })}
      </Typography>
      <Table size="small">
        <TableHead>
          <TableRow>
            <TableCell component="th" scope="col">{t('状态')}</TableCell>
            <TableCell component="th" scope="col">{t('时间')}</TableCell>
            <TableCell component="th" scope="col">{t('目标仓')}</TableCell>
            <TableCell component="th" scope="col">comment</TableCell>
            <TableCell component="th" scope="col">ciUser</TableCell>
            <TableCell component="th" scope="col">user</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {statuses.map((s, i) => (
            <TableRow key={`${s.timestamp}|${i}`} hover data-testid={`build-status-row-${i}`}>
              <TableCell>
                {i === 0 ? (
                  <Chip size="small" variant="outlined" label={s.status} data-testid="build-status-current" sx={{ fontFamily: 'var(--bf-mono)' }} />
                ) : (
                  <span className="mono" lang="en">{s.status}</span>
                )}
              </TableCell>
              <TableCell className="mono" lang="en">{fmtStarted(s.timestamp)}</TableCell>
              <TableCell className="mono" lang="en">{s.repository || '—'}</TableCell>
              <TableCell>{s.comment || '—'}</TableCell>
              <TableCell className="mono" lang="en">{s.ciUser || '—'}</TableCell>
              <TableCell className="mono" lang="en">{s.user || '—'}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Paper>
  )
}

/** build 事件时间线（audit 面——admin；403 整段隐藏不泄漏存在性） */
function BuildTimeline({ name, number, started }: { name: string; number: string; started?: string }) {
  const events = useAsync(
    () => fetchBuildRunEvents(DEFAULT_BUILD_REPO, name, number, started),
    [name, number, started ?? ''],
  )
  const rows = events.data ?? []
  if (events.status === 'forbidden') return null
  return (
    <Paper component="section" className="card section" elevation={1} data-testid="build-timeline">
      <Typography variant="subtitle2" component="h3" sx={{ mb: 1 }}>
        {t('事件时间线（audit）')}
      </Typography>
      {events.status === 'loading' && <Skeleton lines={2} />}
      {events.status === 'error' && events.error && (
        <p className="text-2" title={events.error.message}>{t('时间线不可用（HTTP')} {events.error.status}{t('）')}</p>
      )}
      {events.status === 'ok' && rows.length === 0 && (
        <p className="text-2">{t('本 run 无 audit 事件行（retention 是名级窗口事件，不归属单个 run）。')}</p>
      )}
      {events.status === 'ok' && rows.length > 0 && (
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell component="th" scope="col">{t('动作')}</TableCell>
              <TableCell component="th" scope="col">{t('时间')}</TableCell>
              <TableCell component="th" scope="col">{t('操作者')}</TableCell>
              <TableCell component="th" scope="col">detail</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((e, i) => (
              <TableRow key={e.id} hover data-testid={`build-timeline-row-${i}`}>
                <TableCell>
                  <span className="mono" lang="en" title={ACTION_LABEL[e.action] ?? ''}>{e.action}</span>
                </TableCell>
                <TableCell className="mono" lang="en">{fmtAuditTime(e.time)}</TableCell>
                <TableCell className="mono" lang="en">{e.actor}</TableCell>
                <TableCell>
                  <span className="mono text-2" lang="en" style={{ wordBreak: 'break-all' }}>
                    {Object.entries(e.detail)
                      .map(([k, v]) => `${k}=${String(v)}`)
                      .join(' · ')}
                  </span>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Paper>
  )
}

function BuildDetailView({ name, number }: { name: string; number: string }) {
  const [params] = useSearchParams()
  const started = params.get('started') ?? undefined
  const detail = useAsync(
    () => getBuildRun(name, number, { started }),
    [name, number, started ?? ''],
  )
  const notFound = detail.status === 'error' && detail.error?.status === 404
  const info = detail.data?.buildInfo
  const modules = info?.modules ?? []
  const artifacts = modules.flatMap((m) => m.artifacts.map((a) => ({ module: m.id, a })))
  const dependencies = modules.flatMap((m) => m.dependencies.map((d) => ({ module: m.id, d })))
  const props = Object.entries(info?.properties ?? {})

  if (detail.status === 'forbidden') {
    return (
      <div data-testid="build-detail-page">
        <div className="page-header">
          <h2>
            Builds / <span className="mono" lang="en">{name}</span> / <span className="mono" lang="en">{number}</span>
          </h2>
          <Button variant="outlined" size="small" component={Link} to={`/builds/${encodeURIComponent(name)}`}>{t('← 返回 run 列表')}</Button>
        </div>
        <BuildDenied />
      </div>
    )
  }

  return (
    <div data-testid="build-detail-page">
      <div className="page-header">
        <h2>
          Builds / <span className="mono" lang="en">{name}</span> / <span className="mono" lang="en">{number}</span>
        </h2>
        <Button variant="outlined" size="small" component={Link} to={`/builds/${encodeURIComponent(name)}`}>{t('← 返回 run 列表')}</Button>
      </div>
      <p className="admin-note" data-testid="build-promote-note">{t('ⓘ promote 走 API：POST /api/build/promote/{name}/{number}（body：status/comment/ciUser/timestamp/dryRun/sourceRepo/targetRepo/copy/artifacts/dependencies/scopes/properties/failFast）——ux 册无承载定案，控制台动作面归后续票。')}        </p>

      {detail.status === 'loading' && <Skeleton lines={8} />}
      {detail.status === 'error' && detail.error && !notFound && (
        <ErrorCard error={detail.error} onRetry={detail.reload} />
      )}
      {notFound && <BuildNotFound name={name} />}

      {detail.status === 'ok' && info && (
        <>
          <Paper component="section" className="card section" elevation={1} data-testid="build-detail-info">
            <div className="kv">
              <span className="k">{t('构建名')}</span>
              <span className="mono" lang="en">{info.name}</span>
            </div>
            <div className="kv">
              <span className="k">{t('run 号')}</span>
              <span className="mono" lang="en">{info.number}</span>
            </div>
            <div className="kv">
              <span className="k">{t('启动时间')}</span>
              <span className="mono" lang="en">{fmtStarted(info.started)}</span>
            </div>
            <div className="kv">
              <span className="k">{t('类型')}</span>
              <span className="mono" lang="en">{info.type || '—'}</span>
            </div>
            {typeof info.url === 'string' && info.url !== '' && (
              <div className="kv">
                <span className="k">CI URL</span>
                <span className="mono" lang="en" style={{ wordBreak: 'break-all' }}>
                  {info.url} <CopyButton value={info.url} label="CI URL" />
                </span>
              </div>
            )}
            {props.length > 0 && (
              <div className="kv">
                <span className="k">{t('属性（{v1} 项）', { v1: props.length })}</span>
                <span className="mono" lang="en" style={{ wordBreak: 'break-all' }}>
                  {props.map(([k, v], i) => (
                    <span key={k}>
                      {i > 0 && ' · '}{k}={v}
                    </span>
                  ))}
                </span>
              </div>
            )}
          </Paper>

          <BuildStatuses statuses={info.statuses} />

          {/* 模块列表（断言地基照 build-info.md §3.1 module 字段集） */}
          <Paper component="section" className="card section" elevation={1} data-testid="build-modules">
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1 }}>
              {t('模块（{v1} 个）', { v1: modules.length })}
            </Typography>
            {modules.length === 0 ? (
              <p className="text-2">{t('（无模块——append 可按 module id 增量并入）')}</p>
            ) : (
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell component="th" scope="col">Module ID</TableCell>
                    <TableCell component="th" scope="col">{t('类型')}</TableCell>
                    <TableCell component="th" scope="col">{t('制品数')}</TableCell>
                    <TableCell component="th" scope="col">{t('依赖数')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {modules.map((m, i) => (
                    <TableRow key={m.id} hover data-testid={`build-module-row-${i}`}>
                      <TableCell>
                        <span className="mono" lang="en">{m.id}</span>
                        <CopyButton value={m.id} label="Module ID" />
                      </TableCell>
                      <TableCell className="mono" lang="en">{m.type || '—'}</TableCell>
                      <TableCell className="mono">{m.artifacts.length}</TableCell>
                      <TableCell className="mono">{m.dependencies.length}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </Paper>

          {artifacts.length > 0 && (
            <Paper component="section" className="card section" elevation={1} data-testid="build-artifacts">
              <Typography variant="subtitle2" component="h3" sx={{ mb: 1 }}>
                {t('模块制品（{v1} 项）', { v1: artifacts.length })}
              </Typography>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell component="th" scope="col">Module ID</TableCell>
                    <TableCell component="th" scope="col">{t('名称')}</TableCell>
                    <TableCell component="th" scope="col">{t('路径')}</TableCell>
                    <TableCell component="th" scope="col">sha256</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {artifacts.map(({ module, a }, i) => {
                    // 关联形 path = "<repo>/<path>"——首段拆 (repo, path) 挂
                    // 跨仓树深链；record-only 行（path 空 = 行存不冒领关联）
                    // 如实无链接
                    const repo = a.path?.includes('/') ? a.path.split('/')[0] : ''
                    const rest = a.path && repo ? a.path.slice(repo.length + 1) : ''
                    return (
                      <TableRow key={`${module}|${a.path || a.name}|${i}`} hover data-testid={`build-artifact-row-${i}`}>
                        <TableCell className="mono" lang="en">{module}</TableCell>
                        <TableCell className="mono" lang="en">{a.name || '—'}</TableCell>
                        <TableCell>
                          {repo && rest ? (
                            <Link
                              className="row-link mono"
                              lang="en"
                              to={`/artifacts/${[repo, ...rest.split('/')].map((s) => encodeURIComponent(s)).join('/')}`}
                            >
                              {a.path}
                            </Link>
                          ) : (
                            <span className="text-muted" title={t('record-only 行：上传文档的路径未解析到本实例节点（无 repo 段/节点缺/sha256 相左）——行存不冒领关联')}>—</span>
                          )}
                        </TableCell>
                        <TableCell>
                          {a.sha256 ? (
                            <>
                              <span className="mono" lang="en">{a.sha256.slice(0, 12)}…</span>
                              <CopyButton value={a.sha256} label="sha256" />
                            </>
                          ) : (
                            <span className="text-muted">—</span>
                          )}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </Paper>
          )}

          {dependencies.length > 0 && (
            <Paper component="section" className="card section" elevation={1} data-testid="build-dependencies">
              <Typography variant="subtitle2" component="h3" sx={{ mb: 1 }}>
                {t('模块依赖（{v1} 项）', { v1: dependencies.length })}
              </Typography>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell component="th" scope="col">Module ID</TableCell>
                    <TableCell component="th" scope="col">{t('依赖')}</TableCell>
                    <TableCell component="th" scope="col">{t('类型')}</TableCell>
                    <TableCell component="th" scope="col">scopes</TableCell>
                    <TableCell component="th" scope="col">sha1</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {dependencies.map(({ module, d }, i) => (
                    <TableRow key={`${module}|${d.id}|${i}`} hover data-testid={`build-dependency-row-${i}`}>
                      <TableCell className="mono" lang="en">{module}</TableCell>
                      <TableCell className="mono" lang="en">{d.id}</TableCell>
                      <TableCell className="mono" lang="en">{d.type || '—'}</TableCell>
                      <TableCell className="mono" lang="en">{(d.scopes ?? []).join(',') || '—'}</TableCell>
                      <TableCell>
                        {d.sha1 ? (
                          <>
                            <span className="mono" lang="en">{d.sha1.slice(0, 12)}…</span>
                            <CopyButton value={d.sha1} label="sha1" />
                          </>
                        ) : (
                          <span className="text-muted">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Paper>
          )}

          <BuildTimeline name={name} number={number} started={info.started} />
        </>
      )}
    </div>
  )
}

// 三视图一组件（URL 即状态：/builds → :name → :name/:number）
export default function BuildsPage() {
  const { name, number } = useParams<{ name?: string; number?: string }>()
  if (name === undefined) return <BuildNamesView />
  if (number === undefined) return <BuildNumbersView name={name} />
  return <BuildDetailView name={name} number={number} />
}
