import { useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { useConfirm } from '../../components/ConfirmDialog'
import { useToast } from '../../app/ToastContext'
import { StateSkeleton } from '@/components/layout/states'
import { ApiError, deleteNodeProperties, errText, getNodeProperties, putNodeProperties } from '../../lib/api'
import { PROPS_COPY } from './detailCopy'
import { useAsync } from '../../lib/useAsync'
import { tr } from '../../i18n'

const t = tr('artifacts')

// 制品详情 · Properties 页签（T-291 M10 首发；T-447 / FR-144.4 解剖翻正）。
//
// 交互解剖按 Artifactory 7.161.20 活体实证对齐（B-2.9 翻正——T-447 活体
// 取证：example-repo-local Properties 页签 = 常显 Property name / Property
// value 输入 + Add Property 钮 + 网格 Search 过滤 + 行选网格）：
//   - **常显表单**：Property/Value 两输入 + Add 钮常驻（隐藏「+ 新增属性」
//     旗标表单与逐行 ✎ 编辑行退役——B-2.9 两处偏差形态一并收口）；
//     同名键 Add = 该键值集整体替换（§11.40 合并律——键锁定的行内编辑
//     退役后，改值 = 同键重 Add，语义对用户可见〔replaceHint〕）
//   - **网格搜索**：键/值子串过滤既有属性网格（客户端过滤，清空恢复）
//   - **行内删除与 E1 统一**（Q2 出口①——收紧不倒退）：删除钮在行内，
//     但走 ConfirmDialog 危险确认（T-291 期的「轻交互无确认」退役）
//   - **Property|Property Set 分段 = K68 候裁臂**：不建（Property Set 须
//     BE 属性集小域扩列另立票；本票不做缺位登记——裁做时再入册）
//   - wire 形态 = §15.3.3 的 map<string,string[]>（值以逗号分隔呈现，
//     如实逐值呈现非拍平）；校验与服务端同口径（props.go 闭集），非法
//     即时反馈、Add 禁用——零坏请求出浏览器
//   - 权限姿态沿树页先例（W12d）：readonly_admin 预收敛禁用；普通用户
//     保留写入口，服务端 403 行内呈现（opError 信封文案）
// 四态：loading（Skeleton）/ 空（无属性引导，常显表单仍在）/ 错误
// （Alert + 重试）/ 数据。
//
// FE-P4 MUI 清场：TextField/Button/Tooltip/Table/Alert/IconButton →
// Tailwind + `.field` 族 + 原生 table（新栈 idiom）。锚族原样（node-props /
// -key-input / -values-input-<key> / -add / -search / -table / -row-<key> /
// -delete-<key> / -empty / -error / -search-empty）；校验/确认/toast 语义
// 零改点（useConfirm/useToast 仍是既有 API）。

/** 键闭集（与服务端 ValidatePropKey 同口径） */
const KEY_RE = /^[A-Za-z][A-Za-z0-9_.-]{0,63}$/
// eslint-disable-next-line no-control-regex -- 与服务端 isPropControlByte 同口径（C0 + DEL）
const CTRL_RE = /[\x00-\x1f\x7f]/

const MAX_KEYS = 64
const MAX_VALUES = 32
const MAX_VALUE_BYTES = 1024

function validateKey(key: string): string | null {
  if (!KEY_RE.test(key)) return t('键须匹配 [A-Za-z][A-Za-z0-9_.-]{0,63}（字母开头，≤64 字符）')
  return null
}

/** 逗号分隔的多值输入 → 值集合（即时校验，错误文案与服务端规则同源） */
function parseValues(text: string): { values: string[]; error: string | null } {
  const values = text
    .split(',')
    .map((v) => v.trim())
    .filter((v) => v !== '')
  if (values.length === 0) return { values, error: t('至少一个非空值（服务端拒绝空值）') }
  if (values.length > MAX_VALUES) return { values, error: t('单键最多 {MAX_VALUES} 个值', { MAX_VALUES: MAX_VALUES }) }
  const seen = new Set<string>()
  for (const v of values) {
    if (v.length > MAX_VALUE_BYTES) return { values, error: t('值超过 {MAX_VALUE_BYTES} 字节：{v1}…', { MAX_VALUE_BYTES: MAX_VALUE_BYTES, v1: v.slice(0, 24) }) }
    if (CTRL_RE.test(v)) return { values, error: t('值不能包含控制字符') }
    if (seen.has(v)) return { values, error: t('重复值：{v}（存储模型是值集合）', { v: v }) }
    seen.add(v)
  }
  return { values, error: null }
}

export default function PropertiesTab({
  repoKey,
  path,
  canWrite,
}: {
  repoKey: string
  path: string
  /** 写姿态（readonly_admin 预收敛禁用；普通用户 true，服务端 403 兜底） */
  canWrite: boolean
}) {
  const toast = useToast()
  const confirm = useConfirm()
  const propsQ = useAsync(() => getNodeProperties(repoKey, path), [repoKey, path])
  const rows: [string, string[]][] = useMemo(
    () =>
      propsQ.status === 'ok'
        ? Object.entries(propsQ.data ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
        : [],
    [propsQ.status, propsQ.data],
  )

  // 常显表单态（键/值输入驻留——Add 成功后清空；不做行内草稿行）
  const [newKey, setNewKey] = useState('')
  const [valuesText, setValuesText] = useState('')
  const [search, setSearch] = useState('')
  const [opError, setOpError] = useState<{ status: number; message: string } | null>(null)
  const [busy, setBusy] = useState(false)

  // ---- 即时校验（Add 钮禁用的依据） ----
  const trimmedKey = newKey.trim()
  const valuesAnchorSuffix = trimmedKey || 'new'
  const keyExists = rows.some(([k]) => k === trimmedKey)
  const keyError =
    trimmedKey === ''
      ? null
      : validateKey(trimmedKey)
  const keyCountError = trimmedKey !== '' && !keyExists && rows.length >= MAX_KEYS ? t('节点最多 {MAX_KEYS} 个属性键', { MAX_KEYS: MAX_KEYS }) : null
  const { error: valuesError } = parseValues(valuesText)
  const keyMissing = trimmedKey === ''
  const invalid = !!keyError || !!keyCountError || !!valuesError || keyMissing

  // ---- 网格搜索（键/值子串，客户端过滤；大小写不敏感） ----
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (q === '') return rows
    return rows.filter(([k, vs]) => k.toLowerCase().includes(q) || vs.some((v) => v.toLowerCase().includes(q)))
  }, [rows, search])

  // ---- 写操作（Add = PUT 单键 merge / 删除 = DELETE 单键，过危险确认） ----
  const add = async () => {
    if (invalid || busy) return
    const { values } = parseValues(valuesText)
    setBusy(true)
    setOpError(null)
    try {
      // PUT 单键 = 同名键值集整体替换、他键保留（§11.40）——常显表单下
      // 的「改值」即同键重 Add（replaceHint 语义可见）
      await putNodeProperties(repoKey, path, { [trimmedKey]: values })
      toast.success(keyExists ? t('已替换属性 {trimmedKey} 的值集', { trimmedKey: trimmedKey }) : t('已添加属性 {trimmedKey}', { trimmedKey: trimmedKey }))
      setNewKey('')
      setValuesText('')
      propsQ.reload()
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0
      setOpError({ status, message: errText(err) })
    } finally {
      setBusy(false)
    }
  }

  const remove = async (key: string) => {
    // E1 统一（Q2 出口①）：行内删除件必须过危险确认——浏览器面（属性行）
    // 的轻交互直删退役
    const ok = await confirm({
      title: PROPS_COPY.deleteTitle,
      danger: true,
      confirmLabel: t('删除'),
      body: (
        <p>
          {PROPS_COPY.deleteLead} <b className="mono" lang="en">{key}</b> {PROPS_COPY.deleteTrail}
        </p>
      ),
    })
    if (!ok) return
    setBusy(true)
    setOpError(null)
    try {
      await deleteNodeProperties(repoKey, path, [key])
      toast.success(t('已删除属性 {key}', { key: key }))
      propsQ.reload()
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0
      setOpError({ status, message: errText(err) })
    } finally {
      setBusy(false)
    }
  }

  const onFieldKeys = (e: React.KeyboardEvent<HTMLInputElement>) => {
    // 常显表单无「取消编辑」态——Enter 提交、Esc 只清输入
    if (e.key === 'Enter') {
      e.preventDefault()
      void add()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      setNewKey('')
      setValuesText('')
    }
  }

  const readonlyTitle = t('只读管理员不可写（服务端 403 兜底）')
  const searchEmpty = search.trim() !== '' && filtered.length === 0

  // ---- 四态 ----
  if (propsQ.status === 'loading') return <div data-testid="node-props"><StateSkeleton lines={3} /></div>
  if (propsQ.status === 'error' || propsQ.status === 'forbidden') {
    return (
      <div data-testid="node-props">
        <div
          data-testid="node-props-error"
          role="alert"
          className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense"
        >
          <span className="text-destructive">{t('属性加载失败（HTTP')} {propsQ.error?.status ?? 0}{t('）——')}{propsQ.error?.message ?? t('网络错误')}</span>
          <Button variant="outline" size="sm" onClick={propsQ.reload}>{t('重试')}</Button>
        </div>
      </div>
    )
  }

  return (
    <div data-testid="node-props">
      {/* 常显表单（B-2.9 翻正——7.161.20 活体：Property name / Property
          value 两输入 + Add 常驻；同名键 = 整体替换其值集） */}
      <div className="flex flex-wrap items-start gap-2" style={{ marginBottom: 6 }}>
        <div className="field" style={{ width: 220, marginBottom: 0 }}>
          <label htmlFor="node-props-key-input">Property</label>
          <input
            id="node-props-key-input"
            className="w-full"
            placeholder={PROPS_COPY.keyPlaceholder}
            value={newKey}
            onChange={(e) => setNewKey(e.target.value)}
            onKeyDown={onFieldKeys}
            disabled={!canWrite || busy}
            data-testid="node-props-key-input"
            spellCheck={false}
            autoComplete="off"
            aria-invalid={!!keyError || !!keyCountError}
          />
          {(keyError || keyCountError) ? (
            <p className="field-error" role="alert">{keyError ?? keyCountError}</p>
          ) : (
            <p className="field-hint">{keyExists ? PROPS_COPY.replaceHint : ' '}</p>
          )}
        </div>
        <div className="field" style={{ width: 280, marginBottom: 0 }}>
          <label htmlFor={`node-props-values-input-${valuesAnchorSuffix}`}>Value</label>
          <input
            // 后缀 = 已敲键或 new（家族 node-props-values-input-<key>）——
            // 模板串内不得内联引号（对账器值类正则按引号截断，锚家族会
            // 从 src 侧隐形——T-447 复刻 T-291 教训，先算后拼）
            data-testid={`node-props-values-input-${valuesAnchorSuffix}`}
            className="w-full"
            placeholder={PROPS_COPY.valuePlaceholder}
            value={valuesText}
            onChange={(e) => setValuesText(e.target.value)}
            onKeyDown={onFieldKeys}
            disabled={!canWrite || busy}
            spellCheck={false}
            autoComplete="off"
            aria-invalid={!!valuesError}
          />
          {valuesError ? (
            <p className="field-error" role="alert">{valuesError}</p>
          ) : (
            <p className="field-hint">{t('多值以逗号分隔，如 v1, v2')}</p>
          )}
        </div>
        <span title={!canWrite ? readonlyTitle : rows.length >= MAX_KEYS ? t('节点最多 {MAX_KEYS} 个属性键', { MAX_KEYS: MAX_KEYS }) : PROPS_COPY.replaceHint}>
          <Button
            variant="outline"
            size="sm"
            className="mt-6"
            data-testid="node-props-add"
            disabled={!canWrite || busy || invalid}
            onClick={() => void add()}
          >
            {PROPS_COPY.addLabel}
          </Button>
        </span>
      </div>

      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <span className="text-dense text-muted-foreground">{t('属性 ·')} {rows.length} {t('个键')}{search.trim() !== '' ? t('（匹配 {v1}）', { v1: filtered.length }) : ''}</span>
        {/* 网格搜索（B-2.9 解剖要素——键/值子串过滤；清空恢复全量） */}
        <div className="field" style={{ width: 220, marginBottom: 0 }}>
          <input
            data-testid="node-props-search"
            className="w-full"
            aria-label={PROPS_COPY.searchLabel}
            placeholder={PROPS_COPY.searchPlaceholder}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            autoComplete="off"
          />
          {search !== '' && (
            <button
              type="button"
              aria-label={t('清除属性搜索')}
              onClick={() => setSearch('')}
              className="mt-0.5 self-start text-aux text-muted-foreground hover:text-foreground"
            >
              ✕
            </button>
          )}
        </div>
      </div>

      {rows.length === 0 ? (
        <div data-testid="node-props-empty" className="py-6 text-center">
          <p className="text-dense text-muted-foreground">{t('此节点尚无属性')}</p>
          <p className="mt-1 text-aux text-muted-foreground">{t('部署时以矩阵参数（PUT …;key=value）附带，或用上方表单添加；属性用于检索与治理。')}</p>
        </div>
      ) : searchEmpty ? (
        <div className="py-6 text-center">
          <p className="text-dense text-muted-foreground" data-testid="node-props-search-empty">{t('没有匹配「')}{search.trim()}{t('」的属性')}</p>
        </div>
      ) : (
        <table data-testid="node-props-table" aria-label={t('制品属性')} className="w-full border-collapse text-dense">
          <thead>
            <tr className="border-b border-border text-left text-aux text-muted-foreground">
              <th className="w-[34%] px-2 py-1.5 font-medium">{t('键')}</th>
              <th className="px-2 py-1.5 font-medium">{t('值（多值以逗号分隔）')}</th>
              <th className="w-[72px] px-2 py-1.5 text-right font-medium">{t('操作')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map(([key, values]) => (
              <tr key={key} data-testid={`node-props-row-${key}`} className="border-b border-border/60">
                <th scope="row" className="px-2 py-1.5 text-left font-mono font-normal">
                  {key}
                </th>
                <td className="px-2 py-1.5 font-mono">{values.join(', ')}</td>
                <td className="whitespace-nowrap px-2 py-1.5 text-right">
                  <button
                    type="button"
                    aria-label={t('删除属性 {key}', { key: key })}
                    data-testid={`node-props-delete-${key}`}
                    title={!canWrite ? readonlyTitle : t('删除该属性（DELETE 单键——过危险确认，E1 统一）')}
                    disabled={!canWrite || busy}
                    onClick={() => void remove(key)}
                    className="rounded-sm p-1 text-muted-foreground hover:bg-surface-2 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50"
                  >
                    <span aria-hidden="true">🗑</span>
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {opError && (
        <div
          data-testid="node-props-error"
          role="alert"
          className="mt-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-dense"
        >
          <div className="flex items-center justify-between gap-2">
            <span>{t('属性写入失败（HTTP')} {opError.status}{t('）：')}<span lang="en">{opError.message}</span></span>
            <button
              type="button"
              aria-label={t('关闭错误提示')}
              onClick={() => setOpError(null)}
              className="rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
            >
              <span aria-hidden="true">✕</span>
            </button>
          </div>
          {opError.status === 403 && (
            <div>{t('当前会话没有该路径的写权限（write 动作）——权限按 permission target 的路径 pattern 授予，请联系管理员。')}</div>
          )}
        </div>
      )}

      <p className="mt-2 text-aux text-muted-foreground">
        {PROPS_COPY.footnote}
      </p>
    </div>
  )
}
