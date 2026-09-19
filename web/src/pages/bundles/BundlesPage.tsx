import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
// Release Bundles 页（T-514 / FR-153.3——P3 新栈重写 + 创建面解锁（capability
// matrix 未列域 bundles 行：「POST create」））：
// - URL 即状态：/bundles 与 /bundles/source（source 名单）→ /bundles/:name
//   → /bundles/:name/:version；/bundles/target → /bundles/target/:name →
//   /bundles/target/:name/:version（Release Lifecycle v2 target/history 面）。
// - 读门语义如实（T-513 实测）：名单面按会话可见集过滤（空集走空态）；
//   版本/描述符面对无读门会话 403（canRead 拒绝即答案），404 = 真缺。
// - **创建对话框（解锁）**：POST /api/release/bundle 显式清单形——name/
//   version/artifacts[]（repo/path/sha256 行编辑）；202 新建 / 200 同清单
//   续建 / 409 异清单（服务端 flat 体原文呈现）；release-bundle 槽门（pro+）
//   服务端终裁。
// - mono + 一键拷贝：signature / HEAD 校验和 / 清单行 sha256；pending 行
//   （sha256=''）如实 —。
// 锚族原样：bundles-page/bundles-empty/bundles-table/bundles-row-<name>/
// bundle-versions-page/bundle-versions-table/bundle-version-row-<v>/
// bundle-state/bundle-detail-page/bundle-detail-info/bundle-detail-signature/
// bundle-checksum/bundle-artifacts/bundle-artifact-row-<i>/
// bundle-not-found/bundle-denied。
// 新锚（日志登记诉求）：bundle-create/bundle-create-dialog。
import { useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
import { Badge } from '@/components/ui/badge'
import { Button, ButtonAsChild } from '@/components/ui/button'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { Pager, useClientPager } from '@/components/layout/pager'
import { useAsync } from '@/lib/useAsync'
import { formatBytes } from '@/lib/format'
import { tr } from '@/i18n'
import CreateBundleDialog from './CreateBundleDialog'
import {
  getBundleDescriptor,
  headBundleChecksum,
  getTargetBundleRecord,
  getTargetBundleStatus,
  listBundleNames,
  listBundleVersions,
  listTargetBundleVersions,
  listTargetBundles,
} from './api'
import type { BundleVersionRow } from './api'

const t = tr('bundles')

/** RFC3339 UTC → 秒精度 UTC 形 */
function fmtUTC(v: string): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

/** 状态徽章：COMPLETE = 终态（success）/ INPROGRESS = 清单有缺位（warning）/
 *  其余词如实回显（闭集外走 neutral——未来状态机扩词零破相）。 */
function StateBadge({ state }: { state: string }) {
  const variant = state === 'COMPLETE' ? 'success' : state === 'INPROGRESS' ? 'warning' : 'neutral'
  return (
    <Badge variant="tint-info" data-testid="bundle-state" data-variant={variant}>
      {state}
    </Badge>
  )
}

/** 404 承载 */
function BundleNotFound({ name }: { name?: string }) {
  return (
    <EmptyState
      testid="bundle-not-found"
      message={name ? t('Release Bundle {name} 不存在', { name: name }) : t('Release Bundle 不存在')}
      hint={t('名单面按会话可见集过滤（空集如实）；版本/描述符面对无读门会话是明确的 403。读门 = 系统读权限 ∨ Any Distribution 通道（按 bundle 名授予）。')}
      action={
        <ButtonAsChild variant="outline" size="sm">
          <Link to="/bundles">{t('← 返回 bundle 列表')}</Link>
        </ButtonAsChild>
      }
    />
  )
}

/** 403 承载（版本/描述符面——拒绝即答案，不与 404 混同） */
function BundleDenied({ backTo }: { backTo?: string }) {
  return (
    <EmptyState
      testid="bundle-denied"
      message={t('无权限查看此 Release Bundle')}
      hint={t('读门 = 系统读权限 ∨ Any Distribution 通道（按 bundle 名授予）——403 即读门拒绝（拒绝即答案，不与不存在混同）。')}
      action={
        <ButtonAsChild variant="outline" size="sm">
          <Link to={backTo ?? '/bundles'}>{backTo ? t('← 返回版本列表') : t('← 返回 bundle 列表')}</Link>
        </ButtonAsChild>
      }
    />
  )
}

// ---- 视图 ①：bundle 名单 ------------------------------------------------------

function BundleNamesView() {
  const { session } = useAuth()
  const adminWrite = session?.admin === true
  const names = useAsync(listBundleNames, [])
  const [createOpen, setCreateOpen] = useState(false)

  return (
    <div data-testid="bundles-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">Release Bundles</h2>
        <span className="text-aux text-muted-foreground">{t('版本化发布记录（名 → 版本 → 描述符 + 创建面）')}</span>
        {adminWrite && (
          <Button size="sm" className="ml-auto" data-testid="bundle-create" onClick={() => setCreateOpen(true)}>
            {t('＋ 创建 Bundle')}
          </Button>
        )}
      </div>
      <BundleModeTabs mode="source" />

      {names.status === 'loading' && <StateSkeleton lines={5} />}
      {names.status === 'error' && names.error && <ErrorCard error={names.error} onRetry={names.reload} />}
      {names.status === 'ok' && (names.data?.bundles.length ?? 0) === 0 && (
        <EmptyState
          testid="bundles-empty"
          illustration
          message={t('暂无可见的 Release Bundle')}
          hint={t('服务端按会话可见集过滤（系统读权限 ∨ Any Distribution 通道授予面）——空集如实呈现。')}
          action={
            adminWrite ? (
              <Button size="sm" data-testid="bundle-create-empty" onClick={() => setCreateOpen(true)}>
                {t('创建第一个 Bundle')}
              </Button>
            ) : undefined
          }
        />
      )}
      {names.status === 'ok' && (names.data?.bundles.length ?? 0) > 0 && (
        <section className="card section" data-testid="bundles-table">
          <Table className="w-full text-dense">
            <TableHeader>
              <TableRow className="border-b border-border text-left text-aux text-muted-foreground">
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('Bundle')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(names.data?.bundles ?? []).map((b) => (
                <TableRow key={b.name} className="border-b border-border/60 hover:bg-accent" data-testid={`bundles-row-${b.name}`}>
                  <TableCell className="px-3 py-1.5">
                    <Link className="row-link font-mono text-primary hover:underline" lang="en" to={`/bundles/${encodeURIComponent(b.name)}`}>
                      {b.name}
                    </Link>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>
      )}
      {createOpen && (
        <CreateBundleDialog onClose={() => setCreateOpen(false)} onDone={() => { setCreateOpen(false); names.reload() }} />
      )}
    </div>
  )
}

// ---- 视图 ②：版本单 ----------------------------------------------------------

function BundleVersionsView({ name }: { name: string }) {
  const versions = useAsync(() => listBundleVersions(name), [name])
  const notFound = versions.status === 'error' && versions.error?.status === 404

  // 403 分支（无读门会话——拒绝即答案）
  if (versions.status === 'forbidden') {
    return (
      <div data-testid="bundle-versions-page">
        <div className="page-header flex flex-wrap items-center gap-2">
          <h2 className="text-lg font-semibold">
            Release Bundles / <span className="font-mono" lang="en">{name}</span>
          </h2>
          <ButtonAsChild variant="outline" size="sm" className="ml-auto">
            <Link to="/bundles">{t('← 返回 bundle 列表')}</Link>
          </ButtonAsChild>
        </div>
        <BundleDenied />
      </div>
    )
  }

  return (
    <div data-testid="bundle-versions-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">
          Release Bundles / <span className="font-mono" lang="en">{name}</span>
        </h2>
        <ButtonAsChild variant="outline" size="sm" className="ml-auto">
          <Link to="/bundles">{t('← 返回 bundle 列表')}</Link>
        </ButtonAsChild>
      </div>
      <BundleModeTabs mode="source" />

      {versions.status === 'loading' && <StateSkeleton lines={5} />}
      {versions.status === 'error' && versions.error && !notFound && (
        <ErrorCard error={versions.error} onRetry={versions.reload} />
      )}
      {notFound && <BundleNotFound name={name} />}
      {versions.status === 'ok' && (
        <section className="card section" data-testid="bundle-versions-table">
          <Table className="w-full text-dense">
            <TableHeader>
              <TableRow className="border-b border-border text-left text-aux text-muted-foreground">
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('版本')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('状态')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('创建时间')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('历史')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(versions.data?.versions ?? []).map((v: BundleVersionRow) => (
                <TableRow key={v.version} className="border-b border-border/60 hover:bg-accent" data-testid={`bundle-version-row-${v.version}`}>
                  <TableCell className="px-3 py-1.5">
                    <Link
                      className="row-link font-mono text-primary hover:underline"
                      lang="en"
                      to={`/bundles/${encodeURIComponent(name)}/${encodeURIComponent(v.version)}`}
                    >
                      {v.version}
                    </Link>
                  </TableCell>
                  <TableCell className="px-3 py-1.5">
                    <StateBadge state={v.state} />
                  </TableCell>
                  <TableCell className="px-3 py-1.5 font-mono" lang="en">{fmtUTC(v.created)}</TableCell>
                  <TableCell className="px-3 py-1.5">
                    <Link className="row-link" lang="en" to={'/bundles/target/' + encodeURIComponent(name) + '/' + encodeURIComponent(v.version)} data-testid={'bundle-history-' + v.version}>
                      {t('查看 target 历史')}
                    </Link>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>
      )}
    </div>
  )
}

// ---- 视图 ③：描述符 ----------------------------------------------------------

/** HEAD 校验和行（E5 锚定面）：描述符 JSON 字节的 sha256 */
function BundleChecksumRow({ name, version }: { name: string; version: string }) {
  const probe = useAsync(() => headBundleChecksum(name, version), [name, version])
  const value = probe.status === 'ok' ? probe.data : ''
  return (
    <div className="kv">
      <span className="k">{t('描述符校验和（HEAD）')}</span>
      <span className="font-mono" lang="en" data-testid="bundle-checksum">
        {value || '—'}
      </span>
      {value && <CopyButton value={value} label={t('描述符校验和')} />}
    </div>
  )
}

function BundleDetailView({ name, version }: { name: string; version: string }) {
  const detail = useAsync(() => getBundleDescriptor(name, version), [name, version])
  const notFound = detail.status === 'error' && detail.error?.status === 404
  const artifacts = detail.data?.info.artifacts ?? []
  const pager = useClientPager(artifacts.length, `${name}/${version}|${artifacts.length}`)
  const pageRows = pager.slice(artifacts)

  // 描述符面 403 分支在前（拒绝即答案，不与 404 的「不存在」混同）
  if (detail.status === 'forbidden') {
    return (
      <div data-testid="bundle-detail-page">
        <div className="page-header flex flex-wrap items-center gap-2">
          <h2 className="text-lg font-semibold">
            Release Bundles / <span className="font-mono" lang="en">{name}</span> /{' '}
            <span className="font-mono" lang="en">{version}</span>
          </h2>
          <ButtonAsChild variant="outline" size="sm" className="ml-auto">
            <Link to={`/bundles/${encodeURIComponent(name)}`}>{t('← 返回版本列表')}</Link>
          </ButtonAsChild>
        </div>
        <BundleDenied backTo={`/bundles/${encodeURIComponent(name)}`} />
      </div>
    )
  }

  return (
    <div data-testid="bundle-detail-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">
          Release Bundles / <span className="font-mono" lang="en">{name}</span> /{' '}
          <span className="font-mono" lang="en">{version}</span>
        </h2>
        <ButtonAsChild variant="outline" size="sm" className="ml-auto">
          <Link to={`/bundles/${encodeURIComponent(name)}`}>{t('← 返回版本列表')}</Link>
        </ButtonAsChild>
      </div>

      {detail.status === 'loading' && <StateSkeleton lines={8} />}
      {detail.status === 'error' && detail.error && !notFound && (
        <ErrorCard error={detail.error} onRetry={detail.reload} />
      )}
      {notFound && <BundleNotFound name={name} />}

      {detail.status === 'ok' && detail.data && (
        <>
          <section className="card section" data-testid="bundle-detail-info">
            <div className="kv">
              <span className="k">{t('状态')}</span>
              <StateBadge state={detail.data.info.status} />
            </div>
            <div className="kv">
              <span className="k">{t('创建时间')}</span>
              <span className="font-mono" lang="en">{fmtUTC(detail.data.info.created)}</span>
            </div>
            <div className="kv">
              <span className="k">{t('创建者')}</span>
              <span className="font-mono" lang="en">{detail.data.info.created_by || '—'}</span>
            </div>
            <div className="kv">
              <span className="k">{t('类型')}</span>
              <span className="font-mono" lang="en">{detail.data.info.type}</span>
            </div>
            <div className="kv">
              <span className="k" title={t('清单身份集摘要占位（排序后的 repo/path 行集 sha256——签名缺席适配，E6 语义）')}>{t('签名（清单摘要）')}</span>
              <span className="font-mono" lang="en" data-testid="bundle-detail-signature">
                {detail.data.info.signature || '—'}
              </span>
              {detail.data.info.signature && (
                <CopyButton value={detail.data.info.signature} label={t('签名（清单摘要）')} />
              )}
            </div>
            <BundleChecksumRow name={name} version={version} />
          </section>

          <section className="card section" data-testid="bundle-artifacts">
            <h3 className="mb-2 text-dense font-semibold">{t('制品清单（{v1} 项）', { v1: artifacts.length })}</h3>
            <Table className="w-full text-dense">
              <TableHeader>
                <TableRow className="border-b border-border text-left text-aux text-muted-foreground">
                  <TableHead scope="col" className="px-3 py-2 font-medium">{t('仓库')}</TableHead>
                  <TableHead scope="col" className="px-3 py-2 font-medium">{t('路径')}</TableHead>
                  <TableHead scope="col" className="px-3 py-2 font-medium">sha256</TableHead>
                  <TableHead scope="col" className="px-3 py-2 font-medium">{t('大小')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pageRows.map((a, i) => (
                  <TableRow key={`${a.repo}/${a.path}`} className="border-b border-border/60 hover:bg-accent" data-testid={`bundle-artifact-row-${pager.from + i}`}>
                    <TableCell className="px-3 py-1.5 font-mono" lang="en">{a.repo}</TableCell>
                    <TableCell className="px-3 py-1.5 font-mono" lang="en">{a.path}</TableCell>
                    <TableCell className="px-3 py-1.5">
                      {a.sha256 ? (
                        <>
                          <span className="font-mono" lang="en">{a.sha256.slice(0, 12)}…</span>
                          <CopyButton value={a.sha256} label={t('sha256')} />
                        </>
                      ) : (
                        // pending 行（sha256='' = 制品尚未快照）：如实 —
                        <span className="text-muted-foreground" title={t('清单引用的制品尚不在本实例（pending 行——bundle 因此 INPROGRESS）')}>—</span>
                      )}
                    </TableCell>
                    <TableCell className="px-3 py-1.5 font-mono" lang="en">{a.size > 0 ? formatBytes(a.size) : '—'}</TableCell>
                  </TableRow>
                ))}
                {artifacts.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={4} className="px-3 py-2 text-muted-foreground">{t('（空清单）')}</TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
            {artifacts.length > 0 && (
              <Pager
                page={pager.page}
                pageCount={pager.pageCount}
                onPageChange={pager.setPage}
                from={pager.from}
                to={pager.to}
                total={artifacts.length}
                pageSize={pager.size}
                onPageSizeChange={pager.setSize}
              />
            )}
          </section>
        </>
      )}
    </div>
  )
}



// ---- Release Lifecycle target/history companion views ------------------------

function BundleModeTabs({ mode }: { mode: 'source' | 'target' }) {
  return (
    <nav className="flex flex-wrap items-center gap-1" aria-label={t('Release Bundle 视图')} data-testid="bundle-mode-tabs">
      <ButtonAsChild variant={mode === 'source' ? 'secondary' : 'ghost'} size="sm">
        <Link to="/bundles" data-testid="bundle-tab-source">{t('Source')}</Link>
      </ButtonAsChild>
      <ButtonAsChild variant={mode === 'target' ? 'secondary' : 'ghost'} size="sm">
        <Link to="/bundles/target" data-testid="bundle-tab-target">{t('Target')}</Link>
      </ButtonAsChild>
    </nav>
  )
}

function TargetBundlesView() {
  const targets = useAsync(listTargetBundles, [])
  const rows = targets.data?.release_bundles ?? []

  return (
    <div data-testid="target-bundles-page" className="flex flex-col gap-3">
      <div className="page-header flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-semibold">{t('Release Bundles · Target')}</h2>
        <span className="text-aux text-muted-foreground">{t('Distribution received 面（v2 只读）')}</span>
      </div>
      <BundleModeTabs mode="target" />

      {targets.status === 'loading' && <StateSkeleton lines={5} />}
      {targets.status === 'error' && targets.error && <ErrorCard error={targets.error} onRetry={targets.reload} />}
      {targets.status === 'forbidden' && <BundleDenied backTo="/bundles" />}
      {targets.status === 'ok' && rows.length === 0 && (
        <EmptyState
          testid="target-bundles-empty"
          illustration
          message={t('暂无 Received Release Bundle')}
          hint={t('BinFlow 当前是 source-only 实例；Distribution v2 received 面如实返回空集。')}
        />
      )}
      {targets.status === 'ok' && rows.length > 0 && (
        <section className="card section" data-testid="target-bundles-table">
          <Table className="w-full text-dense">
            <TableHeader>
              <TableRow className="border-b border-border text-left text-aux text-muted-foreground">
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('名称')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('项目')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('版本数')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('最新版本')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('日期')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('接收时间')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => {
                const name = row.name ?? ''
                return (
                  <TableRow key={name} data-testid={'target-bundle-row-' + name}>
                    <TableCell className="px-3 py-1.5">
                      <Link className="row-link font-mono" lang="en" to={'/bundles/target/' + encodeURIComponent(name)}>{name}</Link>
                    </TableCell>
                    <TableCell className="px-3 py-1.5 font-mono" lang="en">{row.project_key ?? '—'}</TableCell>
                    <TableCell className="px-3 py-1.5">{targets.data?.total ?? rows.length}</TableCell>
                    <TableCell className="px-3 py-1.5 font-mono" lang="en">{row.release_bundle_version ?? row.version ?? '—'}</TableCell>
                    <TableCell className="px-3 py-1.5 font-mono" lang="en">{fmtUTC(row.created ?? '')}</TableCell>
                    <TableCell className="px-3 py-1.5 font-mono" lang="en">{fmtUTC(row.received_at ?? '')}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </section>
      )}
    </div>
  )
}

function TargetBundleVersionsView({ name }: { name: string }) {
  const versions = useAsync(() => listTargetBundleVersions(name), [name])
  const rows = versions.data?.versions ?? []

  return (
    <div data-testid="target-versions-page" className="flex flex-col gap-3">
      <div className="page-header flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-semibold">{t('Target /')} <span className="font-mono" lang="en">{name}</span></h2>
        <ButtonAsChild variant="outline" size="sm" className="ml-auto">
          <Link to="/bundles/target">{t('← 返回 Target 列表')}</Link>
        </ButtonAsChild>
      </div>
      <BundleModeTabs mode="target" />

      {versions.status === 'loading' && <StateSkeleton lines={4} />}
      {versions.status === 'error' && versions.error && <ErrorCard error={versions.error} onRetry={versions.reload} />}
      {versions.status === 'forbidden' && <BundleDenied backTo="/bundles/target" />}
      {versions.status === 'ok' && rows.length === 0 && (
        <EmptyState
          testid="target-versions-empty"
          illustration
          message={t('该 Bundle 暂无 received 版本')}
          hint={t('v2 received 版本面对 source-only BinFlow 实例返回空集。')}
        />
      )}
      {versions.status === 'ok' && rows.length > 0 && (
        <section className="card section" data-testid="target-versions-table">
          <Table className="w-full text-dense">
            <TableHeader>
              <TableRow className="border-b border-border text-left text-aux text-muted-foreground">
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('版本')}</TableHead>
                <TableHead scope="col" className="px-3 py-2 font-medium">{t('接收时间')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => {
                const version = row.release_bundle_version ?? row.version ?? ''
                return (
                  <TableRow key={version} data-testid={'target-version-row-' + version}>
                    <TableCell className="px-3 py-1.5">
                      <Link className="row-link font-mono" lang="en" to={'/bundles/target/' + encodeURIComponent(name) + '/' + encodeURIComponent(version)}>{version}</Link>
                    </TableCell>
                    <TableCell className="px-3 py-1.5 font-mono" lang="en">{fmtUTC(row.received_at ?? row.created ?? '')}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </section>
      )}
    </div>
  )
}

function TargetHistoryView({ name, version }: { name: string; version: string }) {
  const record = useAsync(() => getTargetBundleRecord(name, version), [name, version])
  const status = useAsync(() => getTargetBundleStatus(name, version), [name, version])
  const recordMissing = record.status === 'error' && record.error?.status === 404
  const statusMissing = status.status === 'error' && status.error?.status === 404
  const missing = recordMissing && (status.status === 'loading' || statusMissing)

  return (
    <div data-testid="target-history-page" className="flex flex-col gap-3">
      <div className="page-header flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-semibold">
          {t('Target 历史 /')} <span className="font-mono" lang="en">{name}</span> / <span className="font-mono" lang="en">{version}</span>
        </h2>
        <ButtonAsChild variant="outline" size="sm" className="ml-auto">
          <Link to={'/bundles/target/' + encodeURIComponent(name)}>{t('← 返回版本列表')}</Link>
        </ButtonAsChild>
      </div>
      <BundleModeTabs mode="target" />

      {record.status === 'loading' && <StateSkeleton lines={5} />}
      {!missing && record.status === 'error' && record.error && <ErrorCard error={record.error} onRetry={record.reload} />}
      {missing && (
        <EmptyState
          testid="target-history-empty"
          message={t('该版本没有 target 历史记录')}
          hint={t('BinFlow 是 source-only 实例；v2 records/statuses 面如实返回 404，不伪造 Distribution 历史事件。')}
        />
      )}
      {record.status === 'ok' && (
        <section className="card section" data-testid="target-history-record">
          <div className="kv"><span className="k">{t('记录')}</span><span className="font-mono" lang="en">{record.data?.release_bundle?.name ?? name}</span></div>
          <div className="kv"><span className="k">{t('状态')}</span><span className="font-mono" lang="en">{status.data?.status ?? record.data?.status ?? '—'}</span></div>
        </section>
      )}
      {!missing && status.status === 'error' && status.error && <ErrorCard error={status.error} onRetry={status.reload} />}
    </div>
  )
}

// URL router：source 三视图 + v2 target/history 三视图（显式路由优先）。
export default function BundlesPage() {
  const { name, version } = useParams<{ name?: string; version?: string }>()
  const { pathname } = useLocation()
  const isTarget = pathname.startsWith('/bundles/target')

  if (isTarget) {
    if (name === undefined) return <TargetBundlesView />
    if (version === undefined) return <TargetBundleVersionsView name={name} />
    return <TargetHistoryView name={name} version={version} />
  }
  if (name === 'source' && version === undefined) return <BundleNamesView />
  if (name === undefined) return <BundleNamesView />
  if (version === undefined) return <BundleVersionsView name={name} />
  return <BundleDetailView name={name} version={version} />
}
