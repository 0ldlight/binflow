// GPG 签名密钥对管理页（P3 解锁面——capability matrix 未列域表 keypair 行：
// 「API 10 op 全备，无 UI」→ 解锁）。契约 = internal/httpapi/keypair.go。
//
// 页面形态（对位 Artifactory Security > Signing Keys）：
// - 密钥对列表：pairName / alias / pairType / algorithm / updatedAt /
//   updatedBy / 关联仓库（chips + 解除）/ 操作（查看公钥 · 校验 · 删除）；
// - 生成对话框（BinFlow-native 服务端 keygen：pairName/alias/passphrase/
//   keyBits 2048|4096/UID 三件——passphrase write-only 不回显）；
// - 导入对话框（armored 私钥+公钥+passphrase；vault 字面不构造）；
// - 仓库关联卡：选仓 × 选对 → associate（text body）；解除按 chip 行内；
// - 删除强确认 = 输入 pairName；in-use 400 服务端原文如实呈现。
// 私钥/口令永不出现在响应（结构上无字段）；四态齐备（503 未装配平面 →
// 错误卡原文）。
// 锚族（新锚——日志登记诉求）：keypair-page/keypair-table/keypair-row-<n>/
// keypair-create(-generate|-import)/keypair-generate-dialog/
// keypair-import-dialog/keypair-public-dialog/keypair-delete-<name>/
// keypair-verify-<name>/keypair-assoc-repo/keypair-assoc-pair/
// keypair-assoc-go/keypair-assoc-remove-<repo>/keypair-empty/
// keypair-readonly-note。
import { useState } from 'react'

import { useAuth } from '@/app/AuthContext'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertBox } from '@/components/layout/bits'
import { CopyButton } from '@/components/layout/copy-button'
import { EmptyState, ErrorCard, StateSkeleton } from '@/components/layout/states'
import { TextInput, TextArea, NativeSelect } from '@/components/layout/fields'
import { useConfirm } from '@/app/providers'
import { toast } from '@/lib/toast'
import { ApiError, errText, isReadOnlyAdmin, canAdminWrite } from '@/lib/api'
import { useAsync } from '@/lib/useAsync'
import './security.css'
import {
  associateKeypair,
  deleteKeypair,
  disassociateKeypair,
  generateKeypair,
  importKeypair,
  listKeypairs,
  verifyKeypair,
} from './keypair'
import type { KeypairSummary } from './keypair'
import { getRepositories } from '@/lib/api'
import { tr } from '@/i18n'

const t = tr('security')

const KEY_BITS_OPTIONS = [2048, 4096]

export default function KeypairPage() {
  const { session } = useAuth()
  const readOnly = isReadOnlyAdmin(session)
  const admin = canAdminWrite(session)
  const confirm = useConfirm()

  const list = useAsync(listKeypairs, [])
  const repos = useAsync(getRepositories, [])
  const rows: KeypairSummary[] = list.data ?? []

  const [dialog, setDialog] = useState<'generate' | 'import' | null>(null)
  const [publicKey, setPublicKey] = useState<KeypairSummary | null>(null)
  const [verifying, setVerifying] = useState<string | null>(null)
  // 仓库关联卡
  const [assocRepo, setAssocRepo] = useState('')
  const [assocPair, setAssocPair] = useState('')
  const [assocBusy, setAssocBusy] = useState(false)

  const reload = list.reload

  const doVerify = async (pairName: string) => {
    setVerifying(pairName)
    try {
      const text = await verifyKeypair({ pairName })
      toast.success(text) // "Key was verified."
    } catch (err) {
      toast.error(t('校验失败：{v1}', { v1: errText(err) }))
    } finally {
      setVerifying(null)
    }
  }

  const doDelete = async (pair: KeypairSummary) => {
    const typed = await confirm.prompt({
      title: t('删除密钥对'),
      description: (
        <>
          {t('确定要删除密钥对')} <b className="font-mono" lang="en">{pair.pairName}</b> {t('吗？')}
          {pair.repositories.length > 0 && (
            <>
              {' '}{t('该对正被')} {pair.repositories.length} {t('个仓库关联（服务端会以 400 拒绝并列出引用）——先解除全部关联。')}
            </>
          )}
        </>
      ),
      placeholder: pair.pairName,
      mono: true,
      danger: true,
      confirmLabel: t('删除'),
      anchor: 'keypair-delete-confirm-name',
      validate: (v) => (v === pair.pairName ? null : t('输入与密钥对名不一致')),
    })
    if (typed === null || typed !== pair.pairName) return
    try {
      const text = await deleteKeypair(pair.pairName)
      toast.success(text) // "OK"
      reload()
    } catch (err) {
      // in-use 400（列 repo 名）服务端原文如实呈现
      toast.error(t('删除失败：{v1}', { v1: errText(err) }))
    }
  }

  const doAssociate = async () => {
    if (assocRepo === '' || assocPair === '') return
    setAssocBusy(true)
    try {
      await associateKeypair(assocRepo, assocPair)
      toast.success(t('已关联 {pair} → {repo}', { pair: assocPair, repo: assocRepo }))
      setAssocRepo('')
      setAssocPair('')
      reload()
    } catch (err) {
      toast.error(t('关联失败：{v1}', { v1: errText(err) }))
    } finally {
      setAssocBusy(false)
    }
  }

  const doDisassociate = async (pairName: string, repoKey: string) => {
    const ok = await confirm.confirm({
      title: t('解除关联 {repo}', { repo: repoKey }),
      description: t('解除后该仓库的签名/校验停用（密钥对本体保留）。'),
      confirmLabel: t('解除'),
      danger: true,
    })
    if (!ok) return
    try {
      await disassociateKeypair(repoKey, pairName)
      toast.success(t('已解除 {repo} 与 {pair} 的关联', { repo: repoKey, pair: pairName }))
      reload()
    } catch (err) {
      toast.error(t('解除失败：{v1}', { v1: errText(err) }))
    }
  }

  const pairOptions = ['', ...rows.map((r) => r.pairName)]
  const repoOptions = ['', ...(repos.data ?? []).map((r) => r.key)]

  return (
    <div data-testid="keypair-page">
      <div className="page-header flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">{t('签名密钥（GPG Key Pairs）')}</h2>
        <span className="text-aux text-2">{t('实例级签名密钥对面（导入 / 服务端生成 / 校验 / 仓库关联）——私钥与口令永不回显')}</span>
        <span className="ml-auto flex gap-2">
          <Button variant="outline" size="sm" data-testid="keypair-create-import" disabled={!admin || readOnly} onClick={() => setDialog('import')}>
            {t('导入密钥对')}
          </Button>
          <Button size="sm" data-testid="keypair-create-generate" disabled={!admin || readOnly} onClick={() => setDialog('generate')}>
            {t('生成密钥对')}
          </Button>
        </span>
      </div>

      {readOnly && (
        <p className="admin-note" data-testid="keypair-readonly-note">
          {t('ⓘ 只读管理员（readonly_admin）视角：签名密钥只读；导入/生成/删除/关联是管理面写操作（服务端 403 兜底）。')}
        </p>
      )}

      {list.status === 'loading' && <StateSkeleton lines={5} />}
      {list.status === 'error' && list.error && <ErrorCard error={list.error} onRetry={reload} />}
      {list.status === 'forbidden' && list.error && (
        <EmptyState message={t('无权限访问签名密钥')} hint={t('签名密钥面是 security:read 管理面读端点（admin / readonly_admin）。')} />
      )}
      {list.status === 'ok' &&
        (rows.length === 0 ? (
          <EmptyState
            illustration
            message={t('还没有签名密钥对')}
            testid="keypair-empty"
            hint={t('「生成密钥对」在服务端产生新对（BinFlow 原生）；「导入密钥对」装载既有 armored 材料一一对应。')}
            action={
              admin ? (
                <Button size="sm" data-testid="keypair-create-generate-empty" onClick={() => setDialog('generate')}>
                  {t('生成第一对')}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <>
            <table className="w-full text-dense" data-testid="keypair-table">
              <thead>
                <tr className="border-b border-border text-left text-aux text-muted-foreground">
                  <th scope="col" className="px-3 py-2 font-medium">{t('密钥对名')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">alias</th>
                  <th scope="col" className="px-3 py-2 font-medium">type</th>
                  <th scope="col" className="px-3 py-2 font-medium">algorithm</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('关联仓库')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('更新')}</th>
                  <th scope="col" className="px-3 py-2 font-medium">{t('操作')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr key={r.pairName} data-testid={`keypair-row-${i}`} className="border-b border-border/60 hover:bg-accent">
                    <td className="px-3 py-1.5">
                      <span className="font-mono" lang="en">{r.pairName}</span>{' '}
                      <CopyButton value={r.pairName} label={t('密钥对名 {v1}', { v1: r.pairName })} />
                    </td>
                    <td className="px-3 py-1.5"><span className="font-mono" lang="en">{r.alias || '—'}</span></td>
                    <td className="px-3 py-1.5">{r.pairType || '—'}</td>
                    <td className="px-3 py-1.5"><span className="font-mono" lang="en">{r.algorithm || '—'}</span></td>
                    <td className="px-3 py-1.5">
                      {r.repositories.length === 0 ? (
                        <span className="text-muted-foreground">—</span>
                      ) : (
                        <span className="sec-chips">
                          {r.repositories.map((repo) => (
                            <span key={repo} className="pattern-chip !mb-0">
                              <span className="val" lang="en">{repo}</span>
                              {admin && !readOnly && (
                                <button
                                  type="button"
                                  aria-label={t('解除 {repo} 关联', { repo: repo })}
                                  onClick={() => void doDisassociate(r.pairName, repo)}
                                  data-testid={`keypair-assoc-remove-${repo}`}
                                >
                                  ✕
                                </button>
                              )}
                            </span>
                          ))}
                        </span>
                      )}
                    </td>
                    <td className="px-3 py-1.5">
                      <span className="text-2" title={`${r.updatedAt} · ${r.updatedBy}`}>
                        {r.updatedAt ? r.updatedAt.replace('T', ' ').slice(0, 19) : '—'}
                      </span>
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5">
                      <Button variant="outline" size="sm" className="h-7" onClick={() => setPublicKey(r)}>
                        {t('公钥')}
                      </Button>{' '}
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7"
                        disabled={verifying === r.pairName}
                        onClick={() => void doVerify(r.pairName)}
                        data-testid={`keypair-verify-${r.pairName}`}
                      >
                        {verifying === r.pairName ? t('校验中…') : t('校验')}
                      </Button>{' '}
                      {admin && (
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7 border-destructive/50 text-destructive hover:bg-destructive/10"
                          onClick={() => void doDelete(r)}
                          data-testid={`keypair-delete-${r.pairName}`}
                        >
                          {t('删除')}
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>

            {/* 仓库关联卡（POST /v2/repositories/{repoKey}/keyPairs——body 为 pair 名纯文本） */}
            {admin && !readOnly && (
              <section className="card mt-4" data-testid="keypair-assoc">
                <h3>{t('关联到仓库')}</h3>
                <p className="field-hint">
                  {t('仓库关联后，该仓库的签名/校验使用此密钥对（Artifactory 7.19 关联面：仓库单槽——再关联即替换）。')}
                </p>
                <div className="flex flex-wrap items-center gap-2">
                  <NativeSelect
                    value={assocRepo}
                    onChange={(e) => setAssocRepo(e.target.value)}
                    className="w-[220px]"
                    aria-label={t('选择仓库')}
                    options={repoOptions.map((r) => ({ value: r, label: r === '' ? t('选择仓库…') : r }))}
                    data-testid="keypair-assoc-repo"
                  />
                  <NativeSelect
                    value={assocPair}
                    onChange={(e) => setAssocPair(e.target.value)}
                    className="w-[220px]"
                    aria-label={t('选择密钥对')}
                    options={pairOptions.map((p) => ({ value: p, label: p === '' ? t('选择密钥对…') : p }))}
                    data-testid="keypair-assoc-pair"
                  />
                  <Button
                    size="sm"
                    disabled={assocRepo === '' || assocPair === '' || assocBusy}
                    onClick={() => void doAssociate()}
                    data-testid="keypair-assoc-go"
                  >
                    {assocBusy ? t('关联中…') : t('关联')}
                  </Button>
                </div>
                {repos.status === 'error' && (
                  <p className="field-hint">{t('仓库列表不可用（')}{repos.error?.message}{t('）。')}</p>
                )}
              </section>
            )}
          </>
        ))}

      {dialog === 'generate' && (
        <GenerateDialog onClose={() => setDialog(null)} onDone={() => { setDialog(null); reload() }} />
      )}
      {dialog === 'import' && (
        <ImportDialog onClose={() => setDialog(null)} onDone={() => { setDialog(null); reload() }} />
      )}
      {publicKey && (
        <Dialog open onOpenChange={(open) => { if (!open) setPublicKey(null) }}>
          <DialogContent className="sm:max-w-[640px]" data-testid="keypair-public-dialog">
            <DialogHeader>
              <DialogTitle>
                {t('公钥 ·')} <span className="font-mono" lang="en">{publicKey.pairName}</span>
              </DialogTitle>
            </DialogHeader>
            <pre lang="en" className="max-h-[50vh] overflow-auto rounded-md border border-border bg-surface-2 p-3 font-mono text-aux">
              {publicKey.publicKey}
            </pre>
            <DialogFooter className="justify-start">
              <CopyButton value={publicKey.publicKey} label={t('armored 公钥')} />
              <span className="text-aux text-muted-foreground">{t('公钥是公开材料（分发给消费方校验签名）；私钥永不离开服务端。')}</span>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

/** 服务端生成对话框（BinFlow-native keygen——passphrase write-only） */
function GenerateDialog({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [f, setF] = useState({ pairName: '', alias: '', passphrase: '', passphrase2: '', keyBits: 2048, uidName: '', uidComment: '', uidEmail: '' })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const mismatch = f.passphrase !== '' && f.passphrase !== f.passphrase2
  const canSubmit = f.pairName.trim() !== '' && !mismatch && f.passphrase !== '' && !busy

  const submit = async () => {
    setBusy(true)
    setError(null)
    try {
      const s = await generateKeypair({
        pairName: f.pairName.trim(),
        alias: f.alias.trim(),
        passphrase: f.passphrase,
        keyBits: f.keyBits,
        uidName: f.uidName.trim(),
        uidComment: f.uidComment.trim(),
        uidEmail: f.uidEmail.trim(),
      })
      toast.success(t('密钥对 {v1} 已生成（{v2} 位）', { v1: s.pairName, v2: f.keyBits }))
      onDone()
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-[520px]" data-testid="keypair-generate-dialog">
        <DialogHeader>
          <DialogTitle>{t('生成签名密钥对（服务端）')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="field">
            <label htmlFor="kp-name">{t('密钥对名 *')}</label>
            <TextInput id="kp-name" mono lang="en" value={f.pairName} onChange={(e) => setF((p) => ({ ...p, pairName: e.target.value }))} placeholder="release-signing" data-testid="keypair-generate-name" />
            <p className="field-hint">{t('同名已存在 → 409（服务端终裁）。')}</p>
          </div>
          <div className="field">
            <label htmlFor="kp-alias">alias</label>
            <TextInput id="kp-alias" mono lang="en" value={f.alias} onChange={(e) => setF((p) => ({ ...p, alias: e.target.value }))} data-testid="keypair-generate-alias" />
          </div>
          <div className="field">
            <label htmlFor="kp-bits">{t('密钥长度')}</label>
            <NativeSelect
              id="kp-bits"
              value={String(f.keyBits)}
              onChange={(e) => setF((p) => ({ ...p, keyBits: Number(e.target.value) }))}
              options={KEY_BITS_OPTIONS.map((b) => ({ value: String(b), label: `${b} ${t('位')}` }))}
              data-testid="keypair-generate-bits"
            />
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="field">
              <label htmlFor="kp-uid-name">UID name</label>
              <TextInput id="kp-uid-name" mono lang="en" value={f.uidName} onChange={(e) => setF((p) => ({ ...p, uidName: e.target.value }))} data-testid="keypair-generate-uid-name" />
            </div>
            <div className="field">
              <label htmlFor="kp-uid-comment">UID comment</label>
              <TextInput id="kp-uid-comment" mono lang="en" value={f.uidComment} onChange={(e) => setF((p) => ({ ...p, uidComment: e.target.value }))} data-testid="keypair-generate-uid-comment" />
            </div>
            <div className="field">
              <label htmlFor="kp-uid-email">UID email</label>
              <TextInput id="kp-uid-email" mono lang="en" type="email" value={f.uidEmail} onChange={(e) => setF((p) => ({ ...p, uidEmail: e.target.value }))} data-testid="keypair-generate-uid-email" />
            </div>
          </div>
          <div className="field">
            <label htmlFor="kp-pass">{t('口令（保护私钥——永不回显）')} *</label>
            <TextInput id="kp-pass" type="password" autoComplete="new-password" value={f.passphrase} onChange={(e) => setF((p) => ({ ...p, passphrase: e.target.value }))} data-testid="keypair-generate-passphrase" />
          </div>
          <div className="field">
            <label htmlFor="kp-pass2">{t('确认口令')}</label>
            <TextInput
              id="kp-pass2"
              type="password"
              autoComplete="new-password"
              value={f.passphrase2}
              onChange={(e) => setF((p) => ({ ...p, passphrase2: e.target.value }))}
              aria-invalid={mismatch || undefined}
              data-testid="keypair-generate-passphrase2"
            />
            {mismatch && <p className="field-error" role="alert">{t('两次输入的口令不一致')}</p>}
          </div>
          {error && (
            <AlertBox severity="error">
              <div className="font-medium">{t('生成失败（HTTP')} {error.status || t('网络')}{t('）')}</div>
              <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{error.message}</div>
            </AlertBox>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} data-testid="keypair-generate-cancel">{t('取消')}</Button>
          <Button disabled={!canSubmit} onClick={() => void submit()} data-testid="keypair-generate-submit">
            {busy ? t('生成中…') : t('生成')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** 导入对话框（armored 材料；vault 字面不构造——400 红线在服务端） */
function ImportDialog({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [f, setF] = useState({ pairName: '', pairType: 'GPG', alias: '', privateKey: '', publicKey: '', passphrase: '' })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const canSubmit = f.pairName.trim() !== '' && f.privateKey.trim() !== '' && f.publicKey.trim() !== '' && !busy

  const submit = async () => {
    setBusy(true)
    setError(null)
    try {
      const s = await importKeypair({
        pairName: f.pairName.trim(),
        pairType: f.pairType,
        alias: f.alias.trim(),
        privateKey: f.privateKey,
        publicKey: f.publicKey,
        passphrase: f.passphrase,
      })
      toast.success(t('密钥对 {v1} 已导入', { v1: s.pairName }))
      onDone()
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, errText(err)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="sm:max-w-[620px]" data-testid="keypair-import-dialog">
        <DialogHeader>
          <DialogTitle>{t('导入签名密钥对')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="field">
              <label htmlFor="ki-name">{t('密钥对名 *')}</label>
              <TextInput id="ki-name" mono lang="en" value={f.pairName} onChange={(e) => setF((p) => ({ ...p, pairName: e.target.value }))} data-testid="keypair-import-name" />
            </div>
            <div className="field">
              <label htmlFor="ki-type">pairType</label>
              <TextInput id="ki-type" mono lang="en" value={f.pairType} onChange={(e) => setF((p) => ({ ...p, pairType: e.target.value }))} data-testid="keypair-import-type" />
            </div>
          </div>
          <div className="field">
            <label htmlFor="ki-alias">alias</label>
            <TextInput id="ki-alias" mono lang="en" value={f.alias} onChange={(e) => setF((p) => ({ ...p, alias: e.target.value }))} data-testid="keypair-import-alias" />
          </div>
          <div className="field">
            <label htmlFor="ki-priv">{t('armored 私钥（BEGIN PRIVATE KEY / BEGIN PGP PRIVATE KEY BLOCK）')} *</label>
            <TextArea
              id="ki-priv"
              mono
              lang="en"
              rows={5}
              className="max-w-none"
              value={f.privateKey}
              onChange={(e) => setF((p) => ({ ...p, privateKey: e.target.value }))}
              data-testid="keypair-import-private"
            />
            <p className="field-hint">{t('仅经 HTTPS 提交至本实例并密封存储；vaultKey / vaultPublicKey 字面被服务端 400 拒绝（不构造）。')}</p>
          </div>
          <div className="field">
            <label htmlFor="ki-pub">{t('armored 公钥')} *</label>
            <TextArea
              id="ki-pub"
              mono
              lang="en"
              rows={4}
              className="max-w-none"
              value={f.publicKey}
              onChange={(e) => setF((p) => ({ ...p, publicKey: e.target.value }))}
              data-testid="keypair-import-public"
            />
          </div>
          <div className="field">
            <label htmlFor="ki-pass">{t('口令（私钥保护口令——可选）')}</label>
            <TextInput id="ki-pass" type="password" autoComplete="new-password" value={f.passphrase} onChange={(e) => setF((p) => ({ ...p, passphrase: e.target.value }))} data-testid="keypair-import-passphrase" />
          </div>
          {error && (
            <AlertBox severity="error">
              <div className="font-medium">{t('导入失败（HTTP')} {error.status || t('网络')}{t('）')}</div>
              <div className="mt-1 break-all font-mono text-aux opacity-90" lang="en">{error.message}</div>
            </AlertBox>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} data-testid="keypair-import-cancel">{t('取消')}</Button>
          <Button disabled={!canSubmit} onClick={() => void submit()} data-testid="keypair-import-submit">
            {busy ? t('导入中…') : t('导入')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
