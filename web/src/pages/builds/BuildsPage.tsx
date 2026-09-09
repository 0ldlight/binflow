// Builds 页（T-512 / FR-152.3——P3 新栈重写 + promote/retention 写面解锁
// （capability matrix 未列域 builds 行：「promote/retention 写面——API 在，
// 旧 FE 明文无 UI」））：
// - 三视图一组件（URL 即状态）：/builds（名单）→ /builds/:name（号单，
//   started 倒序）→ /builds/:name/:number（详情；?started= 消歧）。
// - 读门语义如实：号单空可见集 = 404（零泄漏）；详情面 403/404 分立承载。
// - **promote 对话框（解锁）**：run 详情头动作——status-only / 目标仓迁移
//   两臂（targetRepo 空 = 只翻状态）；dryRun 预演先行；messages[] 原文流
//   呈现（error|warning|info 语义色）。
// - **retention 对话框（解锁）**：号单头动作——四字段窗口（count /
//   minimumBuildDate / 排除号列表 / deleteBuildArtifacts），async 缺省
//   （200 应答自已验证计划——文案明示）。
// - 事件时间线（audit 面）：build.upload/append/promote/delete 四 run 级词；
//   admin 面，非 admin 403 整段隐藏。
// - mono + 一键拷贝：Module ID / 制品 sha256 / CI URL；record-only 制品行
//   （path 空）如实无链接。
// 锚族原样：builds-page/builds-empty/builds-table/builds-row-<name>/
// build-runs-page/build-runs-table/build-run-row-<n>/build-detail-page/
// build-detail-info/build-statuses/build-status-row-<i>/
// build-status-current/build-modules/build-module-row-<i>/
// build-artifacts/build-artifact-row-<i>/build-dependencies/
// build-dependency-row-<i>/build-timeline/build-timeline-row-<i>/
// build-not-found/build-denied/build-promote-note。
// 新锚（日志登记诉求）：build-promote/build-promote-dialog/
// build-retention-dialog。
import { useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { canAdminWrite } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import { tr } from '@/i18n'
import PromoteDialog from './PromoteDialog'
import RetentionDialog from './RetentionDialog'
import {
  DEFAULT_BUILD_REPO,
  fetchBuildRunEvents,
  getBuildRun,
  listBuildNames,
  listBuildNumbers,
} from './api'
import type { BuildInfo } from './api'

const t = tr('builds')

/** Java 规范启动形 → 展示形（空格替 T，偏移保真）；不可解析原样返回 */
function fmtStarted(v: string): string {
  return v ? v.replace('T', ' ') : '—'
}

/** audit RFC3339 → 秒精度 UTC 形 */
function fmtAuditTime(v: string): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

/** run 级 audit 词 → 中文动作名（词面 mono 保真另呈） */
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
        <ButtonAsChild variant="outline" size="sm">
          <Link to="/builds">{t('← 返回构建列表')}</Link>
        </ButtonAsChild>
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
        <ButtonAsChild variant="outline" size="sm">
          <Link to="/builds">{t('← 返回构建列表')}</Link>
        </ButtonAsChild>
      }
    />
  )
}

// ---- 视图 ①：构建名单 --------------------------------------------------------

function BuildNamesView() {
  const names = useAsync(listBuildNames, [])
  return (
    <div data-testid="builds-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">Builds</h2>
        <span className="text-aux text-2">{t('CI 构建记录（构建名 → run 号 → run 详情；promote / 保留策略写面）')}</span>
      </div>
      {names.status === 'loading' && <StateSkeleton lines={5} />}
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
        <section className="card section" data-testid="builds-table">
          <table className="w-full text-dense">
            <thead>
              <tr className="border-b border-border text-left text-aux text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">{t('构建名')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('最新启动')}</th>
              </tr>
            </thead>
            <tbody>
              {(names.data?.builds ?? []).map((b) => {
                const name = b.uri.replace(/^\//, '')
                return (
                  <tr key={`${name}|${b.lastStarted}`} className="border-b border-border/60 hover:bg-accent" data-testid={`builds-row-${name}`}>
                    <td className="px-3 py-1.5">
                      <Link className="row-link font-mono text-primary hover:underline" lang="en" to={`/builds/${encodeURIComponent(name)}`}>
                        {name}
                      </Link>
                    </td>
                    <td className="px-3 py-1.5 font-mono" lang="en">{fmtStarted(b.lastStarted)}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </section>
      )}
    </div>
  )
}

// ---- 视图 ②：run 号单 --------------------------------------------------------

function BuildNumbersView({ name }: { name: string }) {
  const { session } = useAuth()
  const adminWrite = canAdminWrite(session)
  const numbers = useAsync(() => listBuildNumbers(name), [name])
  const notFound = numbers.status === 'error' && numbers.error?.status === 404
  const [retentionOpen, setRetentionOpen] = useState(false)

  return (
    <div data-testid="build-runs-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">
          Builds / <span className="font-mono" lang="en">{name}</span>
        </h2>
        <span className="ml-auto flex gap-2">
          {adminWrite && (
            <Button variant="outline" size="sm" data-testid="build-retention" onClick={() => setRetentionOpen(true)}>
              {t('保留策略…')}
            </Button>
          )}
          <ButtonAsChild variant="outline" size="sm">
            <Link to="/builds">{t('← 返回构建列表')}</Link>
          </ButtonAsChild>
        </span>
      </div>

      {numbers.status === 'loading' && <StateSkeleton lines={5} />}
      {numbers.status === 'error' && numbers.error && !notFound && (
        <ErrorCard error={numbers.error} onRetry={numbers.reload} />
      )}
      {notFound && <BuildNotFound name={name} />}
      {numbers.status === 'ok' && (
        <section className="card section" data-testid="build-runs-table">
          <table className="w-full text-dense">
            <thead>
              <tr className="border-b border-border text-left text-aux text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">{t('run 号')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('启动时间')}</th>
              </tr>
            </thead>
            <tbody>
              {(numbers.data?.buildsNumbers ?? []).map((n) => {
                const number = n.uri.replace(/^\//, '')
                return (
                  <tr key={`${number}|${n.started}`} className="border-b border-border/60 hover:bg-accent" data-testid={`build-run-row-${number}`}>
                    <td className="px-3 py-1.5">
                      <Link
                        className="row-link font-mono text-primary hover:underline"
                        lang="en"
                        to={`/builds/${encodeURIComponent(name)}/${encodeURIComponent(number)}?started=${encodeURIComponent(n.started)}`}
                      >
                        {number}
                      </Link>
                    </td>
                    <td className="px-3 py-1.5 font-mono" lang="en">{fmtStarted(n.started)}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </section>
      )}
      {retentionOpen && (
        <RetentionDialog name={name} onClose={() => setRetentionOpen(false)} onDone={() => { setRetentionOpen(false); numbers.reload() }} />
      )}
    </div>
  )
}

// ---- 视图 ③：run 详情 --------------------------------------------------------

/** promotion 历史（statuses[]——六元组；首行 = 现势〔最新行=现势，§2.4〕） */
function BuildStatuses({ statuses }: { statuses: BuildInfo['statuses'] }) {
  if (!statuses || statuses.length === 0) return null
  return (
    <section className="card section" data-testid="build-statuses">
      <h3 className="mb-2 text-dense font-semibold">{t('promotion 历史（{v1} 条）', { v1: statuses.length })}</h3>
      <table className="w-full text-dense">
        <thead>
          <tr className="border-b border-border text-left text-aux text-muted-foreground">
            <th scope="col" className="px-3 py-2 font-medium">{t('状态')}</th>
            <th scope="col" className="px-3 py-2 font-medium">{t('时间')}</th>
            <th scope="col" className="px-3 py-2 font-medium">{t('目标仓')}</th>
            <th scope="col" className="px-3 py-2 font-medium">comment</th>
            <th scope="col" className="px-3 py-2 font-medium">ciUser</th>
            <th scope="col" className="px-3 py-2 font-medium">user</th>
          </tr>
        </thead>
        <tbody>
          {statuses.map((s, i) => (
            <tr key={`${s.timestamp}|${i}`} className="border-b border-border/60 hover:bg-accent" data-testid={`build-status-row-${i}`}>
              <td className="px-3 py-1.5">
                {i === 0 ? (
                  <span className="badge neutral mono" data-testid="build-status-current">{s.status}</span>
                ) : (
                  <span className="font-mono" lang="en">{s.status}</span>
                )}
              </td>
              <td className="px-3 py-1.5 font-mono" lang="en">{fmtStarted(s.timestamp)}</td>
              <td className="px-3 py-1.5 font-mono" lang="en">{s.repository || '—'}</td>
              <td className="px-3 py-1.5">{s.comment || '—'}</td>
              <td className="px-3 py-1.5 font-mono" lang="en">{s.ciUser || '—'}</td>
              <td className="px-3 py-1.5 font-mono" lang="en">{s.user || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
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
    <section className="card section" data-testid="build-timeline">
      <h3 className="mb-2 text-dense font-semibold">{t('事件时间线（audit）')}</h3>
      {events.status === 'loading' && <StateSkeleton lines={2} />}
      {events.status === 'error' && events.error && (
        <p className="text-2" title={events.error.message}>{t('时间线不可用（HTTP')} {events.error.status}{t('）')}</p>
      )}
      {events.status === 'ok' && rows.length === 0 && (
        <p className="text-2">{t('本 run 无 audit 事件行（retention 是名级窗口事件，不归属单个 run）。')}</p>
      )}
      {events.status === 'ok' && rows.length > 0 && (
        <table className="w-full text-dense">
          <thead>
            <tr className="border-b border-border text-left text-aux text-muted-foreground">
              <th scope="col" className="px-3 py-2 font-medium">{t('动作')}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t('时间')}</th>
              <th scope="col" className="px-3 py-2 font-medium">{t('操作者')}</th>
              <th scope="col" className="px-3 py-2 font-medium">detail</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((e, i) => (
              <tr key={e.id} className="border-b border-border/60 hover:bg-accent" data-testid={`build-timeline-row-${i}`}>
                <td className="px-3 py-1.5">
                  <span className="font-mono" lang="en" title={ACTION_LABEL[e.action] ?? ''}>{e.action}</span>
                </td>
                <td className="px-3 py-1.5 font-mono" lang="en">{fmtAuditTime(e.time)}</td>
                <td className="px-3 py-1.5 font-mono" lang="en">{e.actor}</td>
                <td className="px-3 py-1.5">
                  <span className="break-all font-mono text-aux text-2" lang="en">
                    {Object.entries(e.detail)
                      .map(([k, v]) => `${k}=${String(v)}`)
                      .join(' · ')}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function BuildDetailView({ name, number }: { name: string; number: string }) {
  const { session } = useAuth()
  const adminWrite = canAdminWrite(session)
  const [params] = useSearchParams()
  const started = params.get('started') ?? undefined
  const detail = useAsync(
    () => getBuildRun(name, number, { started }),
    [name, number, started ?? ''],
  )
  const [promoteOpen, setPromoteOpen] = useState(false)
  const notFound = detail.status === 'error' && detail.error?.status === 404
  const info = detail.data?.buildInfo
  const modules = info?.modules ?? []
  const artifacts = modules.flatMap((m) => m.artifacts.map((a) => ({ module: m.id, a })))
  const dependencies = modules.flatMap((m) => m.dependencies.map((d) => ({ module: m.id, d })))
  const props = Object.entries(info?.properties ?? {})

  const header = (
    <div className="page-header flex flex-wrap items-center gap-2">
      <h2 className="text-lg font-semibold">
        Builds / <span className="font-mono" lang="en">{name}</span> / <span className="font-mono" lang="en">{number}</span>
      </h2>
      <span className="ml-auto flex gap-2">
        {adminWrite && info && (
          <Button size="sm" data-testid="build-promote" onClick={() => setPromoteOpen(true)}>
            {t('晋升（promote）…')}
          </Button>
        )}
        <ButtonAsChild variant="outline" size="sm">
          <Link to={`/builds/${encodeURIComponent(name)}`}>{t('← 返回 run 列表')}</Link>
        </ButtonAsChild>
      </span>
    </div>
  )

  if (detail.status === 'forbidden') {
    return (
      <div data-testid="build-detail-page">
        {header}
        <BuildDenied />
      </div>
    )
  }

  return (
    <div data-testid="build-detail-page">
      {header}
      <p className="admin-note" data-testid="build-promote-note">{t('ⓘ 发布走 API：PUT /api/build（body = build info JSON，name/number 在 body）；模块追加 POST /api/build/append/{name}/{number}（204）。本页「晋升」按钮即 POST /api/build/promote（P3 解锁——dryRun 预演先行）。')}</p>

      {detail.status === 'loading' && <StateSkeleton lines={8} />}
      {detail.status === 'error' && detail.error && !notFound && (
        <ErrorCard error={detail.error} onRetry={detail.reload} />
      )}
      {notFound && <BuildNotFound name={name} />}

      {detail.status === 'ok' && info && (
        <>
          <section className="card section" data-testid="build-detail-info">
            <div className="kv">
              <span className="k">{t('构建名')}</span>
              <span className="font-mono" lang="en">{info.name}</span>
            </div>
            <div className="kv">
              <span className="k">{t('run 号')}</span>
              <span className="font-mono" lang="en">{info.number}</span>
            </div>
            <div className="kv">
              <span className="k">{t('启动时间')}</span>
              <span className="font-mono" lang="en">{fmtStarted(info.started)}</span>
            </div>
            <div className="kv">
              <span className="k">{t('类型')}</span>
              <span className="font-mono" lang="en">{info.type || '—'}</span>
            </div>
            {typeof info.url === 'string' && info.url !== '' && (
              <div className="kv">
                <span className="k">CI URL</span>
                <span className="break-all font-mono" lang="en">
                  {info.url} <CopyButton value={info.url} label="CI URL" />
                </span>
              </div>
            )}
            {props.length > 0 && (
              <div className="kv">
                <span className="k">{t('属性（{v1} 项）', { v1: props.length })}</span>
                <span className="break-all font-mono" lang="en">
                  {props.map(([k, v], i) => (
                    <span key={k}>
                      {i > 0 && ' · '}{k}={v}
                    </span>
                  ))}
                </span>
              </div>
            )}
          </section>

          <BuildStatuses statuses={info.statuses} />

          {/* 模块列表（build-info.md §3.1 module 字段集） */}
          <section className="card section" data-testid="build-modules">
            <h3 className="mb-2 text-dense font-semibold">{t('模块（{v1} 个）', { v1: modules.length })}</h3>
            {modules.length === 0 ? (
              <p className="text-2">{t('（无模块——append 可按 module id 增量并入）')}</p>
            ) : (
              <table className="w-full text-dense">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-3 py-2 font-medium">Module ID</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('类型')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('制品数')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('依赖数')}</th>
                  </tr>
                </thead>
                <tbody>
                  {modules.map((m, i) => (
                    <tr key={m.id} className="border-b border-border/60 hover:bg-accent" data-testid={`build-module-row-${i}`}>
                      <td className="px-3 py-1.5">
                        <span className="font-mono" lang="en">{m.id}</span>
                        <CopyButton value={m.id} label="Module ID" />
                      </td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{m.type || '—'}</td>
                      <td className="px-3 py-1.5 font-mono">{m.artifacts.length}</td>
                      <td className="px-3 py-1.5 font-mono">{m.dependencies.length}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>

          {artifacts.length > 0 && (
            <section className="card section" data-testid="build-artifacts">
              <h3 className="mb-2 text-dense font-semibold">{t('模块制品（{v1} 项）', { v1: artifacts.length })}</h3>
              <table className="w-full text-dense">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-3 py-2 font-medium">Module ID</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('名称')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('路径')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">sha256</th>
                  </tr>
                </thead>
                <tbody>
                  {artifacts.map(({ module, a }, i) => {
                    // 关联形 path = "<repo>/<path>"——首段拆 (repo, path) 挂
                    // 跨仓树深链；record-only 行（path 空）如实无链接
                    const repo = a.path?.includes('/') ? a.path.split('/')[0] : ''
                    const rest = a.path && repo ? a.path.slice(repo.length + 1) : ''
                    return (
                      <tr key={`${module}|${a.path || a.name}|${i}`} className="border-b border-border/60 hover:bg-accent" data-testid={`build-artifact-row-${i}`}>
                        <td className="px-3 py-1.5 font-mono" lang="en">{module}</td>
                        <td className="px-3 py-1.5 font-mono" lang="en">{a.name || '—'}</td>
                        <td className="px-3 py-1.5">
                          {repo && rest ? (
                            <Link
                              className="row-link font-mono text-primary hover:underline"
                              lang="en"
                              to={`/artifacts/${[repo, ...rest.split('/')].map((s) => encodeURIComponent(s)).join('/')}`}
                            >
                              {a.path}
                            </Link>
                          ) : (
                            <span className="text-muted-foreground" title={t('record-only 行：上传文档的路径未解析到本实例节点（无 repo 段/节点缺/sha256 相左）——行存不冒领关联')}>—</span>
                          )}
                        </td>
                        <td className="px-3 py-1.5">
                          {a.sha256 ? (
                            <>
                              <span className="font-mono" lang="en">{a.sha256.slice(0, 12)}…</span>
                              <CopyButton value={a.sha256} label="sha256" />
                            </>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </section>
          )}

          {dependencies.length > 0 && (
            <section className="card section" data-testid="build-dependencies">
              <h3 className="mb-2 text-dense font-semibold">{t('模块依赖（{v1} 项）', { v1: dependencies.length })}</h3>
              <table className="w-full text-dense">
                <thead>
                  <tr className="border-b border-border text-left text-aux text-muted-foreground">
                    <th scope="col" className="px-3 py-2 font-medium">Module ID</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('依赖')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('类型')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">scopes</th>
                    <th scope="col" className="px-3 py-2 font-medium">sha1</th>
                  </tr>
                </thead>
                <tbody>
                  {dependencies.map(({ module, d }, i) => (
                    <tr key={`${module}|${d.id}|${i}`} className="border-b border-border/60 hover:bg-accent" data-testid={`build-dependency-row-${i}`}>
                      <td className="px-3 py-1.5 font-mono" lang="en">{module}</td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{d.id}</td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{d.type || '—'}</td>
                      <td className="px-3 py-1.5 font-mono" lang="en">{(d.scopes ?? []).join(',') || '—'}</td>
                      <td className="px-3 py-1.5">
                        {d.sha1 ? (
                          <>
                            <span className="font-mono" lang="en">{d.sha1.slice(0, 12)}…</span>
                            <CopyButton value={d.sha1} label="sha1" />
                          </>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          )}

          <BuildTimeline name={name} number={number} started={info.started} />
        </>
      )}

      {promoteOpen && info && (
        <PromoteDialog
          name={name}
          number={number}
          started={info.started}
          onClose={() => setPromoteOpen(false)}
          onDone={detail.reload}
        />
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
