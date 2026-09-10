// Release Bundles 页（T-514 / FR-153.3——P3 新栈重写 + 创建面解锁（capability
// matrix 未列域 bundles 行：「POST create」））：
// - 三视图一组件（URL 即状态）：/bundles（名单）→ /bundles/:name（版本单）
//   → /bundles/:name/:version（描述符 + HEAD 校验和探针）。
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
import { Link, useParams } from 'react-router-dom'

import { useAuth } from '@/app/AuthContext'
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
  listBundleNames,
  listBundleVersions,
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
    <span className="badge" data-testid="bundle-state" data-variant={variant}>
      {state}
    </span>
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
        <span className="text-aux text-2">{t('版本化发布记录（名 → 版本 → 描述符 + 创建面）')}</span>
        {adminWrite && (
          <Button size="sm" className="ml-auto" data-testid="bundle-create" onClick={() => setCreateOpen(true)}>
            {t('＋ 创建 Bundle')}
          </Button>
        )}
      </div>

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
          <table className="w-full text-dense">
            <thead>
              <tr className="border-b border-border text-left text-aux text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">{t('Bundle')}</th>
              </tr>
            </thead>
            <tbody>
              {(names.data?.bundles ?? []).map((b) => (
                <tr key={b.name} className="border-b border-border/60 hover:bg-accent" data-testid={`bundles-row-${b.name}`}>
                  <td className="px-3 py-1.5">
                    <Link className="row-link font-mono text-primary hover:underline" lang="en" to={`/bundles/${encodeURIComponent(b.name)}`}>
                      {b.name}
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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

      {versions.status === 'loading' && <StateSkeleton lines={5} />}
      {versions.status === 'error' && versions.error && !notFound && (
        <ErrorCard error={versions.error} onRetry={versions.reload} />
      )}
      {notFound && <BundleNotFound name={name} />}
      {versions.status === 'ok' && (
        <section className="card section" data-testid="bundle-versions-table">
          <table className="w-full text-dense">
            <thead>
              <tr className="border-b border-border text-left text-aux text-muted-foreground">
                <th scope="col" className="px-3 py-2 font-medium">{t('版本')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('状态')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('创建时间')}</th>
              </tr>
            </thead>
            <tbody>
              {(versions.data?.versions ?? []).map((v: BundleVersionRow) => (
                <tr key={v.version} className="border-b border-border/60 hover:bg-accent" data-testid={`bundle-version-row-${v.version}`}>
                  <td className="px-3 py-1.5">
                    <Link
                      className="row-link font-mono text-primary hover:underline"
                      lang="en"
                      to={`/bundles/${encodeURIComponent(name)}/${encodeURIComponent(v.version)}`}
                    >
                      {v.version}
                    </Link>
                  </td>
                  <td className="px-3 py-1.5">
                    <StateBadge state={v.state} />
                  </td>
                  <td className="px-3 py-1.5 font-mono" lang="en">{fmtUTC(v.created)}</td>
                </tr>
              ))}
            </tbody>
          </table>
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
            <table className="w-full text-dense">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-3 py-2 font-medium">{t('仓库')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('路径')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">sha256</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('大小')}</th>
                </tr>
              </thead>
              <tbody>
                {pageRows.map((a, i) => (
                  <tr key={`${a.repo}/${a.path}`} className="border-b border-border/60 hover:bg-accent" data-testid={`bundle-artifact-row-${pager.from + i}`}>
                    <td className="px-3 py-1.5 font-mono" lang="en">{a.repo}</td>
                    <td className="px-3 py-1.5 font-mono" lang="en">{a.path}</td>
                    <td className="px-3 py-1.5">
                      {a.sha256 ? (
                        <>
                          <span className="font-mono" lang="en">{a.sha256.slice(0, 12)}…</span>
                          <CopyButton value={a.sha256} label={t('sha256')} />
                        </>
                      ) : (
                        // pending 行（sha256='' = 制品尚未快照）：如实 —
                        <span className="text-muted-foreground" title={t('清单引用的制品尚不在本实例（pending 行——bundle 因此 INPROGRESS）')}>—</span>
                      )}
                    </td>
                    <td className="px-3 py-1.5 font-mono" lang="en">{a.size > 0 ? formatBytes(a.size) : '—'}</td>
                  </tr>
                ))}
                {artifacts.length === 0 && (
                  <tr>
                    <td colSpan={4} className="px-3 py-2 text-muted-foreground">{t('（空清单）')}</td>
                  </tr>
                )}
              </tbody>
            </table>
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

// 三视图一组件（URL 即状态：/bundles → :name → :name/:version）
export default function BundlesPage() {
  const { name, version } = useParams<{ name?: string; version?: string }>()
  if (name === undefined) return <BundleNamesView />
  if (version === undefined) return <BundleVersionsView name={name} />
  return <BundleDetailView name={name} version={version} />
}
