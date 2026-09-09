// Release Bundle 创建对话框（P3 解锁面——POST /api/release/bundle 显式清单
// 形的控制台承载；契约 = internal/httpapi bundle.go bundleWireRequest）。
// - 字段：name / version / artifacts[]（repo/path 行编辑；sha256 可选钉——
//   与节点复算值相左即 400 拒绝，不静默快照错钉）。
// - 冲突三态：202 新建 / 200 同清单续建（resume）/ 409 异清单或已完成
//  （flat 体 "Bundle already exists"——服务端原文如实呈现）。
// - 门：sys:write + release-bundle 槽（pro+）——服务端终裁，错误原文行内。
// - aql/signature/uuid 官方通道被 400 点名拒绝——FE 不构造这些字段。
// 锚族（新锚——日志登记诉求）：bundle-create-dialog/bundle-create-name/
// bundle-create-version/bundle-create-add/bundle-create-row-<i>/
// bundle-create-repo-<i>/bundle-create-path-<i>/bundle-create-sha-<i>/
// bundle-create-remove-<i>/bundle-create-cancel/bundle-create-submit。
import { useState } from 'react'
import { Link } from 'react-router-dom'

import { Button, ButtonAsChild } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertBox } from '@/components/layout/bits'
import { TextInput } from '@/components/layout/fields'
import { toast } from '@/lib/toast'
import { ApiError, errText } from '@/lib/api'
import { tr } from '@/i18n'
import { createBundle } from './api'
import type { BundleManifestItem } from './api'

const t = tr('bundles')

const ROW_INITIAL: BundleManifestItem = { repo: '', path: '', sha256: '' }

export default function CreateBundleDialog({
  onClose,
  onDone,
}: {
  onClose: () => void
  onDone: () => void
}) {
  const [name, setName] = useState('')
  const [version, setVersion] = useState('')
  const [rows, setRows] = useState<BundleManifestItem[]>([{ ...ROW_INITIAL }])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  /** 最近的创建回执（bundle_path 深链——续建/新建同形） */
  const [created, setCreated] = useState<{ path: string; resumed: boolean } | null>(null)

  const validRows = rows.filter((r) => r.repo.trim() !== '' && r.path.trim() !== '')
  const canSubmit =
    !busy && name.trim() !== '' && version.trim() !== '' && validRows.length > 0 &&
    rows.every((r) => (r.repo.trim() === '' && r.path.trim() === '' && r.sha256.trim() === '') || (r.repo.trim() !== '' && r.path.trim() !== ''))

  const setRow = (i: number, patch: Partial<BundleManifestItem>) => {
    setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))
  }

  const submit = async () => {
    if (!canSubmit) return
    setBusy(true)
    setError(null)
    try {
      const res = await createBundle(name.trim(), version.trim(), validRows)
      setCreated({ path: res.bundle_path, resumed: false })
      toast.success(t('Bundle {v1} 已提交（清单 {v2} 项）', { v1: `${name.trim()}/${version.trim()}`, v2: validRows.length }))
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-[680px]" data-testid="bundle-create-dialog">
        <DialogHeader>
          <DialogTitle>{t('创建 Release Bundle（显式清单）')}</DialogTitle>
        </DialogHeader>
        {created ? (
          <div className="flex flex-col gap-3">
            <AlertBox severity="success">
              <div>
                {t('已提交（202 新建 / 200 同清单续建——服务端按清单摘要判定）。描述符地址：')}
                <span className="font-mono" lang="en">{created.path}</span>
              </div>
            </AlertBox>
            <p className="field-hint">
              {t('清单引用的制品尚不在本实例时，bundle 处于 INPROGRESS（pending 行）——制品落地后自动补齐（同一清单再提交 = 续建）。')}
            </p>
            <div className="flex gap-2">
              <ButtonAsChild size="sm">
                <Link to={`/bundles/${encodeURIComponent(name.trim())}/${encodeURIComponent(version.trim())}`}>
                  {t('查看描述符 →')}
                </Link>
              </ButtonAsChild>
              <Button variant="outline" size="sm" onClick={onDone}>{t('返回列表')}</Button>
            </div>
          </div>
        ) : (
          <>
            <div className="flex flex-col gap-3">
              <div className="grid grid-cols-2 gap-3">
                <div className="field">
                  <label htmlFor="bc-name">{t('Bundle 名 *')}</label>
                  <TextInput
                    id="bc-name"
                    mono
                    lang="en"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="my-release"
                    data-testid="bundle-create-name"
                  />
                </div>
                <div className="field">
                  <label htmlFor="bc-version">{t('版本 *')}</label>
                  <TextInput
                    id="bc-version"
                    mono
                    lang="en"
                    value={version}
                    onChange={(e) => setVersion(e.target.value)}
                    placeholder="1.0.0"
                    data-testid="bundle-create-version"
                  />
                </div>
              </div>
              <div className="field">
                <label>{t('制品清单（artifacts——显式行集）')} *</label>
                {rows.map((r, i) => (
                  <div key={i} className="mb-1 grid grid-cols-[1fr_2fr_1fr_auto] items-center gap-2" data-testid={`bundle-create-row-${i}`}>
                    <TextInput
                      mono
                      lang="en"
                      value={r.repo}
                      onChange={(e) => setRow(i, { repo: e.target.value })}
                      placeholder="libs-release"
                      aria-label={t('第 {v1} 行仓库', { v1: i + 1 })}
                      data-testid={`bundle-create-repo-${i}`}
                    />
                    <TextInput
                      mono
                      lang="en"
                      value={r.path}
                      onChange={(e) => setRow(i, { path: e.target.value })}
                      placeholder="com/example/app/1.0/app-1.0.jar"
                      aria-label={t('第 {v1} 行路径', { v1: i + 1 })}
                      data-testid={`bundle-create-path-${i}`}
                    />
                    <TextInput
                      mono
                      lang="en"
                      value={r.sha256}
                      onChange={(e) => setRow(i, { sha256: e.target.value })}
                      placeholder={t('sha256（可选钉）')}
                      aria-label={t('第 {v1} 行 sha256', { v1: i + 1 })}
                      data-testid={`bundle-create-sha-${i}`}
                    />
                    <button
                      type="button"
                      className="grid size-7 place-items-center rounded-sm border border-border text-muted-foreground hover:text-destructive disabled:opacity-40"
                      aria-label={t('移除第 {v1} 行', { v1: i + 1 })}
                      disabled={rows.length === 1}
                      onClick={() => setRows((prev) => prev.filter((_, idx) => idx !== i))}
                      data-testid={`bundle-create-remove-${i}`}
                    >
                      ✕
                    </button>
                  </div>
                ))}
                <div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setRows((prev) => [...prev, { ...ROW_INITIAL }])}
                    data-testid="bundle-create-add"
                  >
                    {t('＋ 加一行')}
                  </Button>
                </div>
                <p className="field-hint">
                  {t('sha256 可选：填了则与节点复算值比对（相左即 400 拒绝——错钉不静默快照）；repo/path 引用本实例 local 仓的制品路径。')}
                </p>
              </div>
              {error && (
                <AlertBox severity="error">
                  <div className="font-medium">{t('创建被拒（HTTP')} {error.status || t('网络')}{t('）')}</div>
                  <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{error.message}</div>
                  {error.status === 409 && (
                    <div className="mt-1 text-2">{t('同名同版本已存在且清单不同（或已完成）——改版本号或核对清单。')}</div>
                  )}
                </AlertBox>
              )}
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={onClose} data-testid="bundle-create-cancel">{t('取消')}</Button>
              <Button disabled={!canSubmit} onClick={() => void submit()} data-testid="bundle-create-submit">
                {busy ? t('提交中…') : t('创建 Bundle')}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
