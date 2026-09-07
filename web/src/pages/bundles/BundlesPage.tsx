import { Link, useParams } from 'react-router-dom'

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
import { Pager, useClientPager } from '../../components/Pager'
import { Skeleton } from '../../components/Skeleton'
import { useAsync } from '../../lib/useAsync'
import { formatBytes } from '../../lib/format'
import { tr } from '../../i18n'
import {
  getBundleDescriptor,
  headBundleChecksum,
  listBundleNames,
  listBundleVersions,
} from './api'
import type { BundleVersionRow } from './api'

const t = tr('bundles')

// Release Bundles 页（T-514 / FR-153.3——bundle 列表/详情 FE 最小面）。
//
// 形态定案（票内留痕）：
// - **导航挂靠**：应用模式「应用」分组第三条目（制品之后）——console-ui
//   §1.1 应用域（Artifactory 展开族 Packages/Builds/Artifacts 的同域；
//   Release Bundles 属 Artifactory 应用域 /ui/repobundles，册面 OSS 7.84
//   无 Distribution 不列该条目，官方文档归属为据）。BinFlow 无 Builds 页
//   （FR-152 FE 在途），Packages 无面——本页是应用域 bundle 侧首件。
// - **三视图一组件**（ArtifactsBrowser 先例——路由 /bundles、/bundles/
//   :name、/bundles/:name/:version 同挂载点，URL 即状态）：
//   ① 名单（GET /release/bundles）② 版本单（同前缀 +/{name}）③ 描述符
//   （+/{name}/{version}；HEAD 校验和同址探针）。
// - **读门语义如实**（T-513 实测 wire 形）：名单面按会话可见集过滤（无
//   授予普通用户 200 空集走 EmptyState——零 403 oracle）；版本/描述符面对
//   无读门会话是明确 403（canRead 拒绝即答案，build 族姿态），404 = 系统
//   读权限持有者的真缺（或可见名零版本）。**契约注记**：T-513 报告 §1.4
//   表述「版本面空可见集 → 404」与实态不符（实态 403——见票日志「契约
//   漂移」），FE 按实态实现。
// - **创建面缺位登记**：POST /api/release/bundle（显式清单形，pro 槽位
//   门）无控制台入口——列表页注记引导 API，创建腿归后续票（不伪造表单）。
// - mono + 一键拷贝：signature（清单身份摘要占位）/ HEAD 描述符校验和 /
//   清单行 sha256；pending 行（sha256=''）如实呈现 —，不伪造成校验值。
// - 清单大表：客户端页窗（useClientPager——T-451 共享控件）。

/** RFC3339 UTC → 秒精度 UTC 形（mono 中立——监控页同款） */
function fmtUTC(v: string): string {
  return v ? v.replace('T', ' ').replace(/(\.\d+)?Z$/, ' UTC') : '—'
}

/** 状态徽章：COMPLETE = 终态（success）/ INPROGRESS = 清单尚有缺位
 *  （warning——T-513 容忍缺位政策的实义态）/ 其余词如实回显（闭集外
 *  不存在的词走 default——未来状态机扩词零破相）。 */
function StateBadge({ state }: { state: string }) {
  const color: 'success' | 'warning' | 'default' =
    state === 'COMPLETE' ? 'success' : state === 'INPROGRESS' ? 'warning' : 'default'
  return (
    <Chip
      size="small"
      variant="outlined"
      color={color}
      label={state}
      className="badge"
      data-testid="bundle-state"
      sx={{ fontFamily: 'var(--bf-mono)' }}
    />
  )
}

/** 404 承载（404 = 系统读权限持有者的真缺/可见名零版本；无读门会话在
 *  版本/描述符面得到 403（见 BundleDenied），两不混同） */
function BundleNotFound({ name }: { name?: string }) {
  return (
    <EmptyState
      testid="bundle-not-found"
      message={name ? t('Release Bundle {name} 不存在', { name: name }) : t('Release Bundle 不存在')}
      hint={t('名单面按会话可见集过滤（空集如实）；版本/描述符面对无读门会话是明确的 403。读门 = 系统读权限 ∨ Any Distribution 通道（按 bundle 名授予）。')}
      action={
        <Button variant="outlined" size="small" component={Link} to="/bundles">{t('← 返回 bundle 列表')}</Button>
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
        <Button variant="outlined" size="small" component={Link} to={backTo ?? '/bundles'}>{backTo ? t('← 返回版本列表') : t('← 返回 bundle 列表')}</Button>
      }
    />
  )
}

// ---- 视图 ①：bundle 名单 ------------------------------------------------------

function BundleNamesView() {
  const names = useAsync(listBundleNames, [])
  return (
    <div data-testid="bundles-page">
      <div className="page-header">
        <h2>Release Bundles</h2>
        <span className="text-2" style={{ fontSize: 'var(--bf-fs-aux)' }}>{t('版本化发布记录（名 → 版本 → 描述符，只读查询面）')}        </span>
      </div>
      <p className="admin-note">{t('ⓘ 创建走 API：POST /api/release/bundle（显式清单形——name / version / artifacts[]，release-bundle 槽位门）；控制台创建面归后续票，本页不伪造表单。')}        </p>

      {names.status === 'loading' && <Skeleton lines={5} />}
      {names.status === 'error' && names.error && <ErrorCard error={names.error} onRetry={names.reload} />}
      {names.status === 'ok' && (names.data?.bundles.length ?? 0) === 0 && (
        <EmptyState
          testid="bundles-empty"
          illustration
          message={t('暂无可见的 Release Bundle')}
          hint={t('服务端按会话可见集过滤（系统读权限 ∨ Any Distribution 通道授予面）——空集如实呈现；管理员可经 API 创建（POST /api/release/bundle）。')}
        />
      )}
      {names.status === 'ok' && (names.data?.bundles.length ?? 0) > 0 && (
        <Paper component="section" className="card section" elevation={1} data-testid="bundles-table">
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">{t('Bundle')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(names.data?.bundles ?? []).map((b) => (
                <TableRow key={b.name} hover data-testid={`bundles-row-${b.name}`}>
                  <TableCell>
                    <Link className="row-link mono" lang="en" to={`/bundles/${encodeURIComponent(b.name)}`}>
                      {b.name}
                    </Link>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Paper>
      )}
    </div>
  )
}

// ---- 视图 ②：版本单 ----------------------------------------------------------

function BundleVersionsView({ name }: { name: string }) {
  const versions = useAsync(() => listBundleVersions(name), [name])
  const notFound = versions.status === 'error' && versions.error?.status === 404

  // 403 分支（无读门会话——与描述符面同姿态：拒绝即答案）
  if (versions.status === 'forbidden') {
    return (
      <div data-testid="bundle-versions-page">
        <div className="page-header">
          <h2>
            Release Bundles / <span className="mono" lang="en">{name}</span>
          </h2>
          <Button variant="outlined" size="small" component={Link} to="/bundles">{t('← 返回 bundle 列表')}</Button>
        </div>
        <BundleDenied />
      </div>
    )
  }

  return (
    <div data-testid="bundle-versions-page">
      <div className="page-header">
        <h2>
          Release Bundles / <span className="mono" lang="en">{name}</span>
        </h2>
        <Button variant="outlined" size="small" component={Link} to="/bundles">{t('← 返回 bundle 列表')}</Button>
      </div>

      {versions.status === 'loading' && <Skeleton lines={5} />}
      {versions.status === 'error' && versions.error && !notFound && (
        <ErrorCard error={versions.error} onRetry={versions.reload} />
      )}
      {notFound && <BundleNotFound name={name} />}
      {versions.status === 'ok' && (
        <Paper component="section" className="card section" elevation={1} data-testid="bundle-versions-table">
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell component="th" scope="col">{t('版本')}</TableCell>
                <TableCell component="th" scope="col">{t('状态')}</TableCell>
                <TableCell component="th" scope="col">{t('创建时间')}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(versions.data?.versions ?? []).map((v: BundleVersionRow) => (
                <TableRow key={v.version} hover data-testid={`bundle-version-row-${v.version}`}>
                  <TableCell>
                    <Link
                      className="row-link mono"
                      lang="en"
                      to={`/bundles/${encodeURIComponent(name)}/${encodeURIComponent(v.version)}`}
                    >
                      {v.version}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <StateBadge state={v.state} />
                  </TableCell>
                  <TableCell className="mono" lang="en">{fmtUTC(v.created)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Paper>
      )}
    </div>
  )
}

// ---- 视图 ③：描述符 ----------------------------------------------------------

/** HEAD 校验和行（E5 锚定面）：与描述符 GET 同址的无 body 探针——描述符
 *  JSON 字节的 sha256。失败/缺头如实呈现 —（不静默吞行）。 */
function BundleChecksumRow({ name, version }: { name: string; version: string }) {
  const probe = useAsync(() => headBundleChecksum(name, version), [name, version])
  const value = probe.status === 'ok' ? probe.data : ''
  return (
    <div className="kv">
      <span className="k">{t('描述符校验和（HEAD）')}</span>
      <span className="mono" lang="en" data-testid="bundle-checksum">
        {value || '—'}
      </span>
      {value && <CopyButton value={value} label={t('描述符校验和')} />}
    </div>
  )
}

/** 描述符页头（forbidden/404/ok 三分支共用——名/版本面包屑 + 返回链） */
function BundleDetailHeader({ name, version }: { name: string; version: string }) {
  return (
    <div className="page-header">
      <h2>
        Release Bundles / <span className="mono" lang="en">{name}</span> /{' '}
        <span className="mono" lang="en">{version}</span>
      </h2>
      <Button
        variant="outlined"
        size="small"
        component={Link}
        to={`/bundles/${encodeURIComponent(name)}`}
      >{t('← 返回版本列表')}        </Button>
    </div>
  )
}

function BundleDetailView({ name, version }: { name: string; version: string }) {
  const detail = useAsync(() => getBundleDescriptor(name, version), [name, version])
  const notFound = detail.status === 'error' && detail.error?.status === 404
  const artifacts = detail.data?.info.artifacts ?? []
  const pager = useClientPager(artifacts.length, `${name}/${version}|${artifacts.length}`)
  const pageRows = pager.slice(artifacts)

  // 描述符面 403 分支在前（useAsync 的 forbidden 态——拒绝即答案，不与
  // 404 的「不存在」混同）
  if (detail.status === 'forbidden') {
    return (
      <div data-testid="bundle-detail-page">
        <BundleDetailHeader name={name} version={version} />
        <BundleDenied backTo={`/bundles/${encodeURIComponent(name)}`} />
      </div>
    )
  }

  return (
    <div data-testid="bundle-detail-page">
      <BundleDetailHeader name={name} version={version} />

      {detail.status === 'loading' && <Skeleton lines={8} />}
      {detail.status === 'error' && detail.error && !notFound && (
        <ErrorCard error={detail.error} onRetry={detail.reload} />
      )}
      {notFound && <BundleNotFound name={name} />}

      {detail.status === 'ok' && detail.data && (
        <>
          <Paper component="section" className="card section" elevation={1} data-testid="bundle-detail-info">
            <div className="kv">
              <span className="k">{t('状态')}</span>
              <StateBadge state={detail.data.info.status} />
            </div>
            <div className="kv">
              <span className="k">{t('创建时间')}</span>
              <span className="mono" lang="en">{fmtUTC(detail.data.info.created)}</span>
            </div>
            <div className="kv">
              <span className="k">{t('创建者')}</span>
              <span className="mono" lang="en">{detail.data.info.created_by || '—'}</span>
            </div>
            <div className="kv">
              <span className="k">{t('类型')}</span>
              <span className="mono" lang="en">{detail.data.info.type}</span>
            </div>
            <div className="kv">
              <span className="k" title={t('清单身份集摘要占位（排序后的 repo/path 行集 sha256——签名缺席适配，E6 语义）')}>{t('签名（清单摘要）')}</span>
              <span className="mono" lang="en" data-testid="bundle-detail-signature">
                {detail.data.info.signature || '—'}
              </span>
              {detail.data.info.signature && (
                <CopyButton value={detail.data.info.signature} label={t('签名（清单摘要）')} />
              )}
            </div>
            <BundleChecksumRow name={name} version={version} />
          </Paper>

          <Paper component="section" className="card section" elevation={1} data-testid="bundle-artifacts">
            <Typography variant="subtitle2" component="h3" sx={{ mb: 1 }}>
              {t('制品清单（{v1} 项）', { v1: artifacts.length })}
            </Typography>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell component="th" scope="col">{t('仓库')}</TableCell>
                  <TableCell component="th" scope="col">{t('路径')}</TableCell>
                  <TableCell component="th" scope="col">{t('sha256')}</TableCell>
                  <TableCell component="th" scope="col">{t('大小')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {pageRows.map((a, i) => (
                  <TableRow key={`${a.repo}/${a.path}`} hover data-testid={`bundle-artifact-row-${pager.from + i}`}>
                    <TableCell className="mono" lang="en">{a.repo}</TableCell>
                    <TableCell className="mono" lang="en">{a.path}</TableCell>
                    <TableCell>
                      {a.sha256 ? (
                        <>
                          <span className="mono" lang="en">{a.sha256.slice(0, 12)}…</span>
                          <CopyButton value={a.sha256} label={t('sha256')} />
                        </>
                      ) : (
                        // pending 行（sha256='' = 制品尚未快照——T-513 容忍
                        // 缺位政策）：如实呈现 —，不伪造成校验值
                        <span className="text-muted" title={t('清单引用的制品尚不在本实例（pending 行——bundle 因此 INPROGRESS）')}>—</span>
                      )}
                    </TableCell>
                    <TableCell className="mono" lang="en">{a.size > 0 ? formatBytes(a.size) : '—'}</TableCell>
                  </TableRow>
                ))}
                {artifacts.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={4} className="text-muted">{t('（空清单）')}</TableCell>
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
          </Paper>
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
